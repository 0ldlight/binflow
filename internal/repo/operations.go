package repo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The copy/move operations family (M12 T-339, FR-105.1; the behavior spec is
// docs/reverse/repo-operations.md section 1 — every clause below cites it).
// One five-stage pipeline serves both verbs:
//
//	parse     addressing normalization (op, paths, flags, the empty-source
//	          warning of section 1.1)
//	precheck  the target-qualification table of section 1.2 BEFORE any
//	          per-item work (class refusals, same-path, the .jfrog block)
//	validate  the per-item chain of section 1.3, in its priority order, run
//	          over the whole source tree (folder rows included)
//	transfer  the zero-copy landing of section 1.4 (blob reference reuse
//	          through the ledger, metadata/property carry, move = copy + the
//	          per-file source delete, unix-style target adjustment)
//	assemble  the messages[] aggregation of section 1.5 (errors first, then
//	          warnings; the status rule) plus the audit row and the copy-side
//	          index-recompute trigger
//
// dry=1 runs stages 1-3 and the counting half of 5 with ZERO side effects;
// failFast=1 stops the walk on the first warning or error (section 1.1).
//
// There is deliberately NO 202/async task form (spec section 7's explicit
// conclusion): the endpoint answers synchronously with the aggregated
// messages stream.

// Operation verbs (the Op field of CopyMoveRequest).
const (
	OpCopy = "copy"
	OpMove = "move"
)

// Audit actions of the family (the props.* precedent of T-286: spelled here
// under the domain facade; joining audit.Actions()' picker list is the audit
// owner's one-liner). Detail carries the source/target addressing and the
// artifact/folder counts.
const (
	AuditActionCopy = "artifact.copy"
	AuditActionMove = "artifact.move"
)

// CopyMoveMessage is one entry of the response's messages[] (section 1.5).
// Level is the logback enum name spelling (V-3: code evidence says the
// uppercase rendering); Status carries the entry's HTTP code for the §1.5
// status aggregation and stays OUT of the wire rendering (the documented
// body shape is level+message only).
type CopyMoveMessage struct {
	Level   string // "ERROR" | "WARN" | "INFO"
	Status  int    // 0 = carries no code
	Message string
}

// CopyMoveResult is the pipeline's aggregate: the message stream, the
// artifact/folder counts of the summary line and the §1.5 status decision.
type CopyMoveResult struct {
	Messages  []CopyMoveMessage
	Artifacts int
	Folders   int
	DryRun    bool
	// HTTPStatus is section 1.5's decision: the LAST error entry's code
	// when one is present, 409 for code-less errors, 200 otherwise
	// (warnings included).
	HTTPStatus int
}

// SystemInternalPathPrefixes is the explicit system-path set of the copy/move
// target block (FR-97.1's DB-2 name registry): Artifactory's
// artifactory.move.copy.block.internal.metadata.target.enabled default-on
// gate, spelled as one list so the trash-can restore chain (T-345) and any
// future internal writer share a single definition. A target path whose
// FIRST segment is one of these answers the 404 "Internal metadata request
// blocked" (repo-operations section 1.2, mid confidence — code-only
// evidence).
var SystemInternalPathPrefixes = []string{".jfrog"}

// CopyMoveRequest is the parsed addressing of one operation. The REST face
// builds it from the path and query; in-process callers (the future
// trash-can engine) may build it directly.
type CopyMoveRequest struct {
	// Op is OpCopy or OpMove.
	Op string
	// SrcRepo/SrcPath address the source. SrcPath "" is the repository
	// root (section 1.1's warning + root-tree semantics).
	SrcRepo string
	SrcPath string
	// TargetRepo/TargetPath address the destination ("to" on the wire).
	// TargetPath "" is the target repository root.
	TargetRepo string
	TargetPath string
	// DryRun runs the full validation chain with zero side effects.
	DryRun bool
	// FailFast stops the walk on the first warning or error.
	FailFast bool
	// SuppressLayouts is parsed for wire parity only: BinFlow has no
	// cross-layout path translation (the LayoutsCoreAddon feature, spec
	// section 1.4, mid confidence), so every spelling behaves as
	// suppressLayouts=1 — a REGISTERED divergence, not a silent ignore.
	SuppressLayouts bool
	// SystemIdentity is the _system_-style internal exemption of section
	// 1.2: the remote/virtual target refusals do not apply. In-process
	// seam ONLY (the trash-can restore chain's future consumer); the REST
	// face never sets it.
	SystemIdentity bool
}

// CopyMoveService is the operations-family capability face of the concrete
// Service implementation. It is deliberately NOT part of the big Service
// interface: consumers reach it by assertion (httpapi) or by concrete type
// (the trash-can engine), so the interface addition cannot break the
// hand-written adapter test fakes that implement Service method by method.
type CopyMoveService interface {
	CopyOrMove(ctx context.Context, p *Principal, req CopyMoveRequest) (*CopyMoveResult, error)
}

// CopyMoveObserver is the copy-side index-recompute seam (section 1.4:
// "copy 触发 Maven 元数据重算（异步任务，按候选目录集合）；move 不触发"). The
// per-protocol consumers (maven's metadata calculator, the deb/conan reindex
// kernels) are wired at cmd assembly — repo never imports the adapters
// (architecture section 2's direction rule, the Replicator seam's precedent).
//
// AfterCopyMove reports one COMPLETED non-dry copy: the target repository
// and the deduplicated, sorted parent directories (trailing-slash folder
// spellings) of every landed FILE node — the candidate-directory set the
// spec names. Implementations must be safe for concurrent use; the pipeline
// fires them OFF the request path (detached context, recover-shielded), so
// an observer can never fail or slow the operation that triggered it.
type CopyMoveObserver interface {
	AfterCopyMove(ctx context.Context, op, targetRepo string, dirs []string)
}

// CopyOrMove implements CopyMoveService: the five-stage pipeline. The error
// return is for FATAL precheck refusals (already *StatusError with the exact
// client rendering) and infrastructure faults; per-item problems are messages
// inside the result, never errors.
func (s *service) CopyOrMove(ctx context.Context, p *Principal, req CopyMoveRequest) (*CopyMoveResult, error) {
	// ---- stage 1: parse ----
	pl := &cmPipeline{
		svc: s, p: p, op: req.Op, dry: req.DryRun, failFast: req.FailFast,
		srcRepo: req.SrcRepo, srcPath: strings.Trim(req.SrcPath, "/"),
		tgtRepo: req.TargetRepo, tgtPath: strings.Trim(req.TargetPath, "/"),
		// The raw spellings keep the trailing-slash signal (the explicit
		// into-directory target of section 1.4) that Trim above drops.
		tgtSlash:  strings.HasSuffix(req.TargetPath, "/"),
		srcSlash:  strings.HasSuffix(req.SrcPath, "/"),
		allowMemo: map[cmAllowKey]bool{},
	}
	if err := pl.parse(); err != nil {
		return nil, err
	}

	// ---- stage 2: precheck ----
	if err := pl.precheck(ctx, req.SystemIdentity); err != nil {
		return nil, err
	}

	// ---- stages 3+4: resolve the source plan, then walk (validate each
	// item; transfer it when not dry) ----
	if err := pl.resolveAndWalk(ctx); err != nil {
		return nil, err
	}

	// ---- stage 5: assemble ----
	return pl.assemble(ctx), nil
}

// cmAllowKey memoizes one authorizer verdict inside a single operation: Can
// is a pure function of (principal, repo, path, action) and the principal is
// fixed for the whole walk, so a tree of N items costs at most one store
// round trip per distinct (repo, path, action) — the N×3 verdicts a
// ten-thousand-node tree needs never become N×3 permission queries.
type cmAllowKey struct {
	repo, path, action string
}

// cmPipeline carries one operation's state across the five stages.
type cmPipeline struct {
	svc      *service
	p        *Principal
	op       string
	dry      bool
	failFast bool

	srcRepo  string // the repo key the CALLER addressed (authorization key)
	srcPath  string // normalized (no leading/trailing slash; "" = root)
	srcSlash bool   // the caller spelled the source with a trailing slash
	srcEff   string // the repo rows are read from (virtual resolved onto a member)

	tgtRepo  string
	tgtPath  string
	tgtSlash bool

	// resolved by resolveAndWalk:
	srcDir  string // trailing-slash folder root ("" = repository root)
	srcIsFd bool   // the source item is a FILE
	tgtRoot string // the effective target root (file path, or dir with trailing slash)

	srcIsRemote bool
	tgtGov      governance

	msgs     []CopyMoveMessage
	files    int
	folders  int
	landedFD []string // landed FILE target paths (the observer's candidate dirs)
	hasErr   bool
	hasWarn  bool

	allowMemo map[cmAllowKey]bool

	// transfer bookkeeping (move pruning + the empty-target sweep):
	movedRows  []*metadata.Node // source rows deleted by a move (files)
	srcFolders []string         // source folder rows of the walk, root first
	keptSrc    map[string]bool  // source rows that did NOT move (failures)
	createdTgt []string         // target folder rows this transfer created
}

// parse is stage 1: parameter shape (the §1.1 messages that are request-level
// 400s) and op validity. The empty-source-path WARNING lives here too — it
// does not fail the request (the root tree is the source).
func (pl *cmPipeline) parse() error {
	if pl.op != OpCopy && pl.op != OpMove {
		return &StatusError{
			Code:    http.StatusBadRequest,
			Message: "Unknown operation '" + pl.op + "'",
			cause:   fmt.Errorf("%w: copy/move op must be copy or move", ErrInvalidPath),
		}
	}
	if pl.srcRepo == "" {
		// Section 1.1's verbatim 400.
		return &StatusError{
			Code: http.StatusBadRequest, Message: "Source repository key is empty",
			cause: fmt.Errorf("%w: source repository key is empty", ErrInvalidPath),
		}
	}
	if pl.tgtRepo == "" {
		// Section 1.1's verbatim 400 (the missing/malformed "to").
		return &StatusError{
			Code: http.StatusBadRequest, Message: "Target repository key is empty",
			cause: fmt.Errorf("%w: target repository key is empty", ErrInvalidPath),
		}
	}
	if pl.srcPath == "" && (pl.srcSlash || pl.srcRepo != "") {
		// Section 1.1's warning: an explicitly empty source path means the
		// repository root; the operation continues.
		pl.warn(0, "Source repository path is empty, path set to root")
	}
	return nil
}

// cmRepoPath renders the "src=/a/b, target=/c/d" clause the §1.2 not-found
// message carries.
func (pl *cmPipeline) cmRepoPath(repo, path string) string { return repo + "/" + path }

// precheck is stage 2 (section 1.2, before any per-item validation): source
// and target rows exist, the target is a local repository, source and target
// are not the same RepoPath, and the target does not address internal
// metadata. Every refusal is the spec's verbatim message as a *StatusError.
func (pl *cmPipeline) precheck(ctx context.Context, systemIdentity bool) error {
	s := pl.svc
	// Source row (any class — the class decides the read strategy later).
	srcRow, err := s.loadRepoRow(ctx, pl.srcRepo)
	if err != nil {
		if errors.Is(err, ErrRepoNotFound) {
			return &StatusError{
				Code: http.StatusBadRequest,
				Message: fmt.Sprintf("Could not calculate repo path from src=%s, target=%s: repository %s not found",
					pl.cmRepoPath(pl.srcRepo, pl.srcPath), pl.cmRepoPath(pl.tgtRepo, pl.tgtPath), pl.srcRepo),
				cause: err,
			}
		}
		return err
	}
	// Target row.
	tgtRow, err := s.loadRepoRow(ctx, pl.tgtRepo)
	if err != nil {
		if errors.Is(err, ErrRepoNotFound) {
			return &StatusError{
				Code: http.StatusBadRequest,
				Message: fmt.Sprintf("Could not calculate repo path from src=%s, target=%s: repository %s not found",
					pl.cmRepoPath(pl.srcRepo, pl.srcPath), pl.cmRepoPath(pl.tgtRepo, pl.tgtPath), pl.tgtRepo),
				cause: err,
			}
		}
		return err
	}
	// Class refusals (section 1.2, verbatim; BinFlow has no separate cache
	// rclass — the remote cache lives in the remote's own namespace, so the
	// "cache repository" arm is unreachable by construction and lives on in
	// the registration only).
	if !systemIdentity {
		switch tgtRow.Type {
		case TypeRemote:
			return &StatusError{
				Code: http.StatusBadRequest,
				Message: fmt.Sprintf(
					"Target repository %s is a remote repository. copy/move to remote repositories is not allowed.", pl.tgtRepo),
				cause: fmt.Errorf("%w: copy/move to remote repositories is not allowed", ErrRepoTypeNotSupported),
			}
		case TypeVirtual:
			return &StatusError{
				Code: http.StatusBadRequest,
				Message: fmt.Sprintf(
					"Target repository %s is a virtual repository. copy/move to virtual repositories is not allowed.", pl.tgtRepo),
				cause: fmt.Errorf("%w: copy/move to virtual repositories is not allowed", ErrRepoTypeNotSupported),
			}
		}
	}
	// The internal-metadata target block (section 1.2): .jfrog and .jfrog/**
	// are internal metadata — 404, not 403, per the default-on switch.
	if cmTargetsInternalPath(pl.tgtPath) {
		return &StatusError{
			Code:    http.StatusNotFound,
			Message: "Internal metadata request blocked",
			cause:   fmt.Errorf("%w: the target addresses internal metadata", ErrInvalidPath),
		}
	}
	// Same RepoPath (section 1.2): folder-vs-file spellings of one path are
	// the same destination when the item kinds agree.
	if pl.srcRepo == pl.tgtRepo && pl.srcPath == pl.tgtPath && pl.srcSlash == pl.tgtSlash {
		verb := pl.op
		return &StatusError{
			Code: http.StatusBadRequest,
			Message: fmt.Sprintf("Skipping %s %s: Destination and source are the same",
				verb, pl.cmRepoPath(pl.srcRepo, pl.srcPath)),
			cause: fmt.Errorf("%w: destination and source are the same", ErrInvalidPath),
		}
	}
	pl.tgtGov = parseGovernance(tgtRow.Config)
	pl.srcIsRemote = srcRow.Type == TypeRemote

	// Move FROM a virtual repository would need the source delete to
	// propagate through the virtual — BinFlow's standing RE-08 refusal
	// (deletes never propagate; the existing DELETE plane's ruling). The
	// copy verb keeps the resolution path below.
	if srcRow.Type == TypeVirtual && pl.op == OpMove {
		return &StatusError{
			Code: http.StatusBadRequest,
			Message: fmt.Sprintf(
				"Deletes are not propagated through the virtual repository '%s'; move the artifact from its member repository directly.", pl.srcRepo),
			cause: fmt.Errorf("%w: move from a virtual repository is not supported", ErrRepoTypeNotSupported),
		}
	}
	return nil
}

// cmTargetsInternalPath reports whether path's first segment is one of the
// SystemInternalPathPrefixes ("" — the repository root — never is).
func cmTargetsInternalPath(path string) bool {
	if path == "" {
		return false
	}
	seg, _, _ := strings.Cut(path, "/")
	for _, p := range SystemInternalPathPrefixes {
		if seg == p {
			return true
		}
	}
	return false
}

// allow is the memoized s.allow of the walk (see cmAllowKey). Read verdicts
// key on the repo the CALLER addressed (srcRepo); write/delete verdicts on
// the target — the same keys the per-item chain names.
func (pl *cmPipeline) allow(ctx context.Context, repo, path, action string) bool {
	k := cmAllowKey{repo: repo, path: path, action: action}
	if v, ok := pl.allowMemo[k]; ok {
		return v
	}
	v := pl.svc.allow(ctx, pl.p, repo, path, action)
	pl.allowMemo[k] = v
	return v
}

// resolveAndWalk is stages 3 and 4: resolve the effective source (local /
// remote cache / virtual member routing), compute the unix-adjusted target
// root, then walk the source tree item by item.
func (pl *cmPipeline) resolveAndWalk(ctx context.Context) error {
	if err := pl.resolveSource(ctx); err != nil {
		return err
	}
	if err := pl.resolveTargetRoot(ctx); err != nil {
		return err
	}
	return pl.walk(ctx)
}

// resolveSource pins the effective source repository and the source item:
//
//   - local: the addressed repository, node row probe;
//   - remote: the repository's own cache namespace (ADR-0012 — cache nodes
//     land in the remote's namespace). A FILE miss fetches through the
//     pull-through engine first (the landing copy, then the node reference);
//     a FOLDER walks the cached subtree only — upstream enumeration is not
//     part of the remote plane (registered divergence: the spec is silent on
//     remote sources, BinFlow copies what is cached);
//   - virtual: two-bucket member routing (the first member holding the item
//     wins, VirtualMemberOrder's order). FILE resolution consults local
//     holdings and remote CACHE rows; FOLDER resolution takes the first
//     member holding the folder row and copies its subtree. Aggregate
//     (multi-member union) folder copies are NOT supported (registered —
//     the spec carries no virtual-source semantics).
//
// Read authorization always keys on the repo the caller addressed (the
// getVirtual posture: members are resolution internals).
func (pl *cmPipeline) resolveSource(ctx context.Context) error {
	s := pl.svc
	pl.srcEff = pl.srcRepo
	srcRow, err := s.loadRepoRow(ctx, pl.srcRepo)
	if err != nil {
		return err
	}

	if srcRow.Type == TypeVirtual {
		member, err := pl.resolveVirtualSource(ctx)
		if err != nil {
			return err
		}
		pl.srcEff = member
		mRow, err := s.loadRepoRow(ctx, member)
		if err != nil {
			return err
		}
		pl.srcIsRemote = mRow.Type == TypeRemote
	} else {
		pl.srcIsRemote = srcRow.Type == TypeRemote
	}

	// The read gate on the ADDRESSED key, before any store walk (the same
	// ordering as Get: an unauthorized principal must not aim the engine).
	if !pl.allow(ctx, pl.srcRepo, pl.srcPath, ActionRead) {
		return &StatusError{
			Code: http.StatusForbidden,
			Message: fmt.Sprintf("User doesn't have permissions to read '%s'. Needs read permissions.",
				pl.cmRepoPath(pl.srcRepo, pl.srcPath)),
			cause: fmt.Errorf("read %s/%s: %w", pl.srcRepo, pl.srcPath, ErrForbidden),
		}
	}

	// Root source: the whole-repository tree.
	if pl.srcPath == "" {
		pl.srcDir, pl.srcIsFd = "", false
		return nil
	}

	// File probe first, then the folder spelling (section 1.1: a directory
	// addressed without its trailing slash gets it back).
	if n, err := s.md.Nodes().Get(ctx, pl.srcEff, pl.srcPath); err == nil {
		pl.srcDir, pl.srcIsFd = "", true
		_ = n // the row itself is re-read by the walk's single-item arm
		return nil
	} else if !errors.Is(err, metadata.ErrNodeNotFound) {
		return fmt.Errorf("source probe %s/%s: %w", pl.srcEff, pl.srcPath, err)
	}
	folderPath := pl.srcPath + "/"
	if n, err := s.md.Nodes().Get(ctx, pl.srcEff, folderPath); err == nil {
		_ = n
		pl.srcDir, pl.srcIsFd = folderPath, false
		return nil
	} else if !errors.Is(err, metadata.ErrNodeNotFound) {
		return fmt.Errorf("source probe %s/%s: %w", pl.srcEff, folderPath, err)
	}

	// Miss. A remote source's FILE gets the landing fetch (cache, stale
	// downgrade, guarded upstream — the full FR-20 chain), then a re-probe.
	if pl.srcIsRemote && !pl.srcSlash {
		if err := pl.fetchRemoteIntoCache(ctx, pl.srcPath); err != nil {
			return err
		}
		if _, err := s.md.Nodes().Get(ctx, pl.srcEff, pl.srcPath); err == nil {
			pl.srcDir, pl.srcIsFd = "", true
			return nil
		}
	}
	// Section 1.2's source-item miss: the documented 400 (V-2 rules the
	// code's warning branch out for BinFlow; registered).
	return &StatusError{
		Code:    http.StatusBadRequest,
		Message: fmt.Sprintf("Could not find item at %s", pl.cmRepoPath(pl.srcRepo, pl.srcPath)),
		cause:   fmt.Errorf("node %s/%s: %w", pl.srcRepo, pl.srcPath, ErrNodeNotFound),
	}
}

// resolveVirtualSource routes a virtual source onto its holding member:
// two-bucket order, first member holding the FILE row (or the remote cache
// row) wins for a file source; first member holding the FOLDER row wins for
// a folder source. A miss answers the same 400 as a plain missing item.
func (pl *cmPipeline) resolveVirtualSource(ctx context.Context) (string, error) {
	s := pl.svc
	order, err := s.virtualMemberOrder(ctx, pl.srcRepo)
	if err != nil {
		return "", err
	}
	folderPath := pl.srcPath + "/"
	for _, m := range order {
		if pl.srcPath != "" {
			fileHit := false
			if _, err := s.md.Nodes().Get(ctx, m.key, pl.srcPath); err == nil {
				fileHit = true
			} else if !errors.Is(err, metadata.ErrNodeNotFound) {
				return "", fmt.Errorf("virtual member probe %s/%s: %w", m.key, pl.srcPath, err)
			}
			if fileHit {
				return m.key, nil
			}
		}
		if _, err := s.md.Nodes().Get(ctx, m.key, folderPath); err == nil {
			pl.srcPath = folderPath
			pl.srcSlash = true
			return m.key, nil
		} else if !errors.Is(err, metadata.ErrNodeNotFound) {
			return "", fmt.Errorf("virtual member probe %s/%s: %w", m.key, folderPath, err)
		}
	}
	return "", &StatusError{
		Code:    http.StatusBadRequest,
		Message: fmt.Sprintf("Could not find item at %s", pl.cmRepoPath(pl.srcRepo, pl.srcPath)),
		cause:   fmt.Errorf("node %s/%s: %w", pl.srcRepo, pl.srcPath, ErrNodeNotFound),
	}
}

// fetchRemoteIntoCache lands one remote path through the pull-through engine
// so the copy references a LOCAL blob (the "landing copy" for remote
// sources). An unfound upstream maps onto the source-miss 400 (V-2's
// documented posture); every other engine failure keeps its exact rendering
// (a *remote.FetchError rides through as a *StatusError, the getRemote
// mapping verbatim).
func (pl *cmPipeline) fetchRemoteIntoCache(ctx context.Context, path string) error {
	res, err := pl.svc.remoteEng.Fetch(ctx, pl.srcEff, path)
	if err != nil {
		var fe *remote.FetchError
		if errors.As(err, &fe) {
			if fe.Unfound {
				return &StatusError{
					Code:    http.StatusBadRequest,
					Message: fmt.Sprintf("Could not find item at %s", pl.cmRepoPath(pl.srcRepo, pl.srcPath)),
					cause:   fmt.Errorf("node %s/%s: %w", pl.srcRepo, pl.srcPath, ErrNodeNotFound),
				}
			}
			return &StatusError{Code: fe.Status, Message: fe.Message}
		}
		return fmt.Errorf("remote fetch %s/%s: %w", pl.srcEff, path, err)
	}
	if res != nil && res.Body != nil {
		_ = res.Body.Close() //nolint:errcheck // the landing stays cached; the stream is not consumed here
	}
	return nil
}

// cmJoin joins a repo-relative directory and a name without the empty-segment
// traps: the repository root ("") takes the name bare.
func cmJoin(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// resolveTargetRoot computes the unix-adjusted effective target of section
// 1.4 (the REST path arm, default on):
//
//   - FILE source: an existing FILE target is overwritten in place; an
//     existing FOLDER target (or a trailing-slash target, or the repository
//     root) receives the file UNDER it (<dir>/<source name>); a missing
//     target is the file's new path verbatim.
//   - FOLDER source: an existing FILE target is the fatal folder-under-file
//     400; an existing FOLDER target (or a trailing slash) receives the
//     folder UNDER it; a missing target renames the folder to it.
func (pl *cmPipeline) resolveTargetRoot(ctx context.Context) error {
	s := pl.svc
	base := cmBaseName(pl.srcPath)

	if pl.srcIsFd {
		tgt := pl.tgtPath
		if pl.tgtPath == "" || pl.tgtSlash {
			tgt = cmJoin(pl.tgtPath, base) // root / explicit directory target
		} else if cmHasFolderRow(ctx, s, pl.tgtRepo, pl.tgtPath) {
			tgt = cmJoin(pl.tgtPath, base)
		}
		pl.tgtRoot = tgt
		return nil
	}

	// Folder source. base "" (the whole-repository copy) always addresses
	// the target path itself.
	dirUnder := func() string {
		switch {
		case base == "" && pl.tgtPath == "":
			return "" // root to root: identical paths
		case base == "":
			return pl.tgtPath + "/"
		default:
			return cmJoin(pl.tgtPath, base) + "/"
		}
	}
	switch {
	case pl.tgtPath == "" || pl.tgtSlash:
		pl.tgtRoot = dirUnder()
	case cmHasFileRow(ctx, s, pl.tgtRepo, pl.tgtPath):
		return &StatusError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Can't move folder under file '%s'.", pl.cmRepoPath(pl.tgtRepo, pl.tgtPath)),
			cause:   fmt.Errorf("%w: the target addresses a file", ErrInvalidPath),
		}
	case cmHasFolderRow(ctx, s, pl.tgtRepo, pl.tgtPath):
		pl.tgtRoot = dirUnder()
	default:
		pl.tgtRoot = pl.tgtPath + "/"
	}
	return nil
}

// cmHasFileRow / cmHasFolderRow probe one repository path's node kind.
func cmHasFileRow(ctx context.Context, s *service, repo, path string) bool {
	if path == "" {
		return false
	}
	_, err := s.md.Nodes().Get(ctx, repo, path)
	return err == nil
}

func cmHasFolderRow(ctx context.Context, s *service, repo, path string) bool {
	if path == "" {
		return false // the repository root always behaves as a directory
	}
	_, err := s.md.Nodes().Get(ctx, repo, path+"/")
	return err == nil
}

// cmBaseName is the last path segment of a repo-relative path ("" for the
// repository root).
func cmBaseName(path string) string {
	path = strings.TrimSuffix(path, "/")
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// walk is the per-item loop: enumerate the source tree, run the section 1.3
// chain per item, transfer (stage 4) when not dry, and honor failFast.
func (pl *cmPipeline) walk(ctx context.Context) error {
	s := pl.svc
	var rows []*metadata.Node
	if pl.srcIsFd {
		n, err := s.md.Nodes().Get(ctx, pl.srcEff, pl.srcPath)
		if err != nil {
			return fmt.Errorf("source %s/%s: %w", pl.srcEff, pl.srcPath, err)
		}
		rows = []*metadata.Node{n}
	} else {
		dir := strings.TrimSuffix(pl.srcDir, "/")
		all, err := s.md.Nodes().ListByPrefix(ctx, pl.srcEff, dir)
		if err != nil {
			return fmt.Errorf("list %s/%s: %w", pl.srcEff, pl.srcDir, err)
		}
		for _, n := range all {
			// The B1/B2 prefix lesson: the subtree arm is dir+"/", and the
			// folder row itself is the exact srcDir spelling. The
			// whole-repository source (srcDir "") takes every row.
			if pl.srcDir == "" || n.Path == pl.srcDir || strings.HasPrefix(n.Path, dir+"/") {
				rows = append(rows, n)
			}
		}
	}
	pl.keptSrc = map[string]bool{}

	for _, n := range rows {
		item := &cmItem{src: n}
		item.tgt = pl.mapTarget(n.Path)
		item.folder = isFolderNode(n.Path)
		pl.srcFolders = append(pl.srcFolders, n.Path) // root-first on the sorted walk
		if err := pl.processItem(ctx, item); err != nil {
			return err
		}
		if pl.failFast && (pl.hasErr || pl.hasWarn) {
			// Section 1.1: failFast=1 stops on the FIRST warning or error.
			return nil
		}
	}

	if pl.op == OpMove && !pl.dry {
		if err := pl.finishMove(ctx); err != nil {
			return err
		}
	}
	if !pl.dry {
		// §1.4's orphan rule rides BOTH verbs: a target folder the
		// transfer created whose every child was refused is swept.
		if err := pl.sweepEmptyTargets(ctx); err != nil {
			return err
		}
	}
	return nil
}

// cmItem is one walk entry: the source row plus its derived target.
type cmItem struct {
	src    *metadata.Node
	tgt    string
	folder bool
}

// mapTarget maps a source path onto its target spelling: the file target is
// the resolved root; a tree child is the target dir root plus the child's
// path relative to the source dir root.
func (pl *cmPipeline) mapTarget(srcPath string) string {
	if pl.srcIsFd {
		return pl.tgtRoot
	}
	if pl.srcDir == "" {
		return srcPath // whole-repository copy: identical paths
	}
	return pl.tgtRoot + strings.TrimPrefix(srcPath, pl.srcDir)
}

// processItem runs the section 1.3 chain (in its priority order) and, when
// every gate passes and the run is live, transfers the item (stage 4).
func (pl *cmPipeline) processItem(ctx context.Context, item *cmItem) error {
	s := pl.svc
	src := item.src
	srcRef := pl.cmRepoPath(pl.srcRepo, src.Path)
	tgtRef := pl.cmRepoPath(pl.tgtRepo, item.tgt)

	// #1 source read (per item, per the spec's tree-recursion rule).
	if !pl.allow(ctx, pl.srcRepo, src.Path, ActionRead) {
		pl.itemError(item, http.StatusForbidden,
			fmt.Sprintf("User doesn't have permissions to read '%s'. Needs read permissions.", srcRef))
		return nil
	}
	// #3 target include/exclude patterns (section 1.3; the copy/move family
	// keeps Artifactory's 403 here — BinFlow's own upload 409 divergence is
	// the content plane's, registered).
	if !pl.tgtGov.allowsPath(strings.TrimSuffix(item.tgt, "/")) {
		pl.itemError(item, http.StatusForbidden,
			fmt.Sprintf("The repository '%s' rejected the path '%s' due to a conflict with its include/exclude patterns.",
				pl.tgtRepo, tgtRef))
		return nil
	}
	// #4 move's source-delete grant (per item).
	if pl.op == OpMove && !pl.allow(ctx, pl.srcRepo, src.Path, ActionDelete) {
		pl.itemError(item, http.StatusForbidden,
			fmt.Sprintf("User doesn't have permissions to move '%s'. Needs delete permissions.", srcRef))
		return nil
	}

	// Target existence probes drive #5/#6/#7.
	tgtFile := cmHasFileRow(ctx, s, pl.tgtRepo, item.folderOpPath())
	tgtFolder := cmHasFolderRow(ctx, s, pl.tgtRepo, strings.TrimSuffix(item.tgt, "/"))
	switch {
	case item.folder && tgtFile:
		// #6: a folder cannot land under a file.
		pl.itemError(item, http.StatusBadRequest,
			fmt.Sprintf("Can't move folder under file '%s'.", pl.cmRepoPath(pl.tgtRepo, item.folderOpPath())))
		return nil
	case !item.folder && tgtFile:
		// #5: overriding an existing file needs DELETE on it — the spec's
		// deliberate 401 (not 403).
		if !pl.allow(ctx, pl.tgtRepo, item.tgt, ActionDelete) {
			pl.itemError(item, http.StatusUnauthorized,
				fmt.Sprintf("User doesn't have permissions to override '%s'. Needs delete permissions.", tgtRef))
			return nil
		}
	case !item.folder && !tgtFile && !tgtFolder:
		// #7: creating the fresh node needs WRITE.
		if !pl.allow(ctx, pl.tgtRepo, item.tgt, ActionWrite) {
			pl.itemError(item, http.StatusForbidden,
				fmt.Sprintf("User doesn't have permissions to create '%s'. Needs write permissions.", tgtRef))
			return nil
		}
	case item.folder && !tgtFile && !tgtFolder:
		// The folder twin of #7 (the chain runs on directories too).
		if !pl.allow(ctx, pl.tgtRepo, item.tgt, ActionWrite) {
			pl.itemError(item, http.StatusForbidden,
				fmt.Sprintf("User doesn't have permissions to create '%s'. Needs write permissions.", tgtRef))
			return nil
		}
	}

	if pl.dry {
		pl.count(item)
		return nil
	}

	// Quota (BinFlow extension, W26 parity: every landing is quota-bound;
	// the spec's §1.3 chain has no quota row — registered): files only,
	// folders are size 0.
	if !item.folder {
		replaced := int64(0)
		if tgtFile {
			if ex, err := s.md.Nodes().Get(ctx, pl.tgtRepo, item.tgt); err == nil {
				replaced = ex.Size
			}
		}
		if err := s.checkQuota(ctx, pl.p, pl.tgtGov, pl.tgtRepo, item.tgt, src.Size, replaced); err != nil {
			code := http.StatusInsufficientStorage
			var se *StatusError
			if errors.As(err, &se) && se.Code > 0 {
				code = se.Code
			}
			pl.itemError(item, code, err.Error())
			return nil
		}
	}

	if err := pl.transfer(ctx, item, tgtFile); err != nil {
		// An infrastructure fault on one item is an item-level ERROR (the
		// walk continues unless failFast), never a 500 for the whole
		// operation — the aggregate contract of section 1.5.
		pl.itemError(item, http.StatusInternalServerError,
			fmt.Sprintf("Could not %s '%s' to '%s': %v", pl.op, srcRef, tgtRef, err))
		return nil
	}
	pl.count(item)
	return nil
}

// folderOpPath is the path a folder item occupies when probed as a file row
// (folder targets are spelled with the trailing slash; the file-collision
// probe addresses the trimmed spelling).
func (it *cmItem) folderOpPath() string {
	if it.folder {
		return strings.TrimSuffix(it.tgt, "/")
	}
	return it.tgt
}

// count advances the summary counters (files/folders; the dry run counts the
// items that PASSED the chain — the "were copied" forecast).
func (pl *cmPipeline) count(item *cmItem) {
	if item.folder {
		pl.folders++
	} else {
		pl.files++
		pl.landedFD = append(pl.landedFD, item.tgt)
	}
}

// itemError records one item-level failure and marks the source row kept.
func (pl *cmPipeline) itemError(item *cmItem, status int, msg string) {
	pl.msgs = append(pl.msgs, CopyMoveMessage{Level: "ERROR", Status: status, Message: msg})
	pl.hasErr = true
	pl.keptSrc[item.src.Path] = true
}

// warn records one warning-level message.
func (pl *cmPipeline) warn(status int, msg string) {
	pl.msgs = append(pl.msgs, CopyMoveMessage{Level: "WARN", Status: status, Message: msg})
	pl.hasWarn = true
}

// transfer lands one item (stage 4): the node row with the SOURCE's
// metadata (created/createdBy/modified — section 1.4's carry rule), the
// ledger-backed blob reference (zero copy), the full property set, and — for
// move — the per-file source delete. tgtFile says the target file row
// pre-existed (the override arm: old properties drop first, delete-then-copy
// semantics).
func (pl *cmPipeline) transfer(ctx context.Context, item *cmItem, tgtFile bool) error {
	s := pl.svc
	src := item.src
	props, err := s.md.NodeProps().List(ctx, pl.srcEff, src.Path)
	if err != nil {
		return fmt.Errorf("source properties: %w", err)
	}

	if item.folder {
		existed := cmHasFolderRow(ctx, s, pl.tgtRepo, strings.TrimSuffix(item.tgt, "/"))
		if err := pl.writeFolder(ctx, src, item.tgt, props, existed); err != nil {
			return err
		}
		if !existed {
			pl.createdTgt = append(pl.createdTgt, item.tgt)
		}
	} else {
		// The digest triple rides the ledger row (sha1/md5 are its
		// authority — the same rule PutFromBlob follows).
		blob, err := s.md.Blobs().Get(ctx, src.Sha256)
		if err != nil {
			return fmt.Errorf("ledger blob %s: %w", src.Sha256, err)
		}
		ref := storage.BlobRef{Sha256: src.Sha256, Size: src.Size}
		if blob != nil {
			ref.Sha1, ref.Md5 = blob.Sha1, blob.Md5
			if blob.Size > 0 {
				ref.Size = blob.Size
			}
		}
		if tgtFile {
			// Delete-then-copy (section 1.4): the overridden file's
			// properties do not survive a copy over it.
			if err := s.md.NodeProps().Delete(ctx, pl.tgtRepo, item.tgt, nil); err != nil {
				return fmt.Errorf("target properties reset: %w", err)
			}
		}
		if err := pl.writeFile(ctx, src, item.tgt, ref, props); err != nil {
			return err
		}
	}

	// move = copy + the per-file source delete (section 1.4); folder rows
	// go at the walk's end, bottom-up, only when empty (finishMove).
	if pl.op == OpMove && !item.folder {
		if err := s.md.Usage().DeleteNodeWithUsage(ctx, pl.srcEff, src.Path, s.now()); err != nil {
			return fmt.Errorf("source delete: %w", err)
		}
		pl.movedRows = append(pl.movedRows, src)
	}
	return nil
}

// writeFile lands one file node with the source's identity fields (section
// 1.4's metadata carry; BinFlow has no modified_by column — registered), the
// ancestors materialized first (ADR-0016) and the properties merged onto the
// fresh row.
func (pl *cmPipeline) writeFile(ctx context.Context, src *metadata.Node, tgt string, ref storage.BlobRef, props map[string][]string) error {
	s := pl.svc
	if err := s.materializeAncestors(ctx, pl.p, pl.tgtRepo, tgt); err != nil {
		return err
	}
	if err := s.md.Blobs().Put(ctx, &metadata.Blob{
		Sha256: ref.Sha256, Sha1: ref.Sha1, Md5: ref.Md5, Size: ref.Size, CreatedAt: s.now(),
	}); err != nil {
		return fmt.Errorf("blob row %s: %w", ref.Sha256, err)
	}
	now := s.now()
	n := &metadata.Node{
		RepoKey: pl.tgtRepo, Path: tgt, Sha256: ref.Sha256, Size: ref.Size,
		Mime: src.Mime, CreatedBy: src.CreatedBy, CreatedAt: src.CreatedAt, UpdatedAt: now,
	}
	if n.CreatedBy == "" || n.CreatedAt == "" {
		// Degenerate source rows (hand-seeded fixtures) still get an
		// honest identity.
		n.CreatedBy, n.CreatedAt = pl.p.Name, now
	}
	// The source's modified stamp rides along (the carry rule); it loses to
	// the operation's own moment only when it is empty.
	if src.UpdatedAt != "" {
		n.UpdatedAt = src.UpdatedAt
	}
	if err := s.md.Usage().PutNodeWithUsage(ctx, n, now); err != nil {
		return fmt.Errorf("node %s/%s: %w", pl.tgtRepo, tgt, err)
	}
	return s.applyDeployProps(ctx, pl.tgtRepo, tgt, props)
}

// writeFolder lands one folder node with the source folder's identity fields
// and properties. existed says the target folder row pre-existed (the
// merge-into case keeps its created identity, mirroring putFolderRow).
func (pl *cmPipeline) writeFolder(ctx context.Context, src *metadata.Node, tgt string, props map[string][]string, existed bool) error {
	s := pl.svc
	if err := s.materializeAncestors(ctx, pl.p, pl.tgtRepo, tgt); err != nil {
		return err
	}
	if err := s.ensureFolderLedger(ctx); err != nil {
		return err
	}
	now := s.now()
	n := &metadata.Node{
		RepoKey: pl.tgtRepo, Path: tgt, Sha256: emptyFolderSHA, Size: 0,
		Mime: folderMime, CreatedBy: src.CreatedBy, CreatedAt: src.CreatedAt, UpdatedAt: now,
	}
	if n.CreatedBy == "" || n.CreatedAt == "" {
		n.CreatedBy, n.CreatedAt = pl.p.Name, now
	}
	if src.UpdatedAt != "" {
		n.UpdatedAt = src.UpdatedAt
	}
	if existed {
		// Merge-into: the standing folder keeps its created identity (the
		// putFolderRow posture) — only the fields a copy can refresh move.
		if ex, err := s.md.Nodes().Get(ctx, pl.tgtRepo, tgt); err == nil {
			n.CreatedBy, n.CreatedAt = ex.CreatedBy, ex.CreatedAt
		}
	}
	if err := s.md.Usage().PutNodeWithUsage(ctx, n, now); err != nil {
		return fmt.Errorf("node %s/%s: %w", pl.tgtRepo, tgt, err)
	}
	return s.applyDeployProps(ctx, pl.tgtRepo, tgt, props)
}

// finishMove prunes the source side after a live move: folder rows
// deepest-first, each only when its subtree is fully gone (a kept row — an
// item that failed — pins its whole ancestor chain), then the empty-parent
// sweep above the moved root.
func (pl *cmPipeline) finishMove(ctx context.Context) error {
	s := pl.svc
	if !pl.srcIsFd && pl.srcDir != "" {
		// Occupancy: every kept row pins all of its ancestor folders.
		occupied := map[string]bool{}
		for path := range pl.keptSrc {
			for f := parentPrefix(path); f != ""; f = parentPrefix(f) {
				occupied[f] = true
			}
		}
		// Deepest-first deletion of the walk's folder rows.
		folders := make([]string, 0, len(pl.srcFolders))
		for _, f := range pl.srcFolders {
			if isFolderNode(f) {
				folders = append(folders, f)
			}
		}
		sort.Sort(sort.Reverse(sort.StringSlice(folders)))
		for _, f := range folders {
			if occupied[f] {
				continue
			}
			if err := s.md.Usage().DeleteNodeWithUsage(ctx, pl.srcEff, f, s.now()); err != nil {
				if errors.Is(err, metadata.ErrNodeNotFound) {
					continue
				}
				return fmt.Errorf("source folder delete %s/%s: %w", pl.srcEff, f, err)
			}
		}
		if err := s.pruneEmptyParents(ctx, pl.srcEff, pl.srcDir); err != nil {
			return err
		}
	} else if pl.srcIsFd {
		if err := s.pruneEmptyParents(ctx, pl.srcEff, pl.srcPath); err != nil {
			return err
		}
	}
	return nil
}

// sweepEmptyTargets is §1.4's orphan rule: target folders this transfer
// CREATED but that ended up with no children (every child rejected or
// skipped) are removed — no empty directories are left behind. It runs for
// copy AND move alike, after the walk (and after finishMove's source
// pruning, so a move's own deletes cannot confuse the emptiness probe).
func (pl *cmPipeline) sweepEmptyTargets(ctx context.Context) error {
	s := pl.svc
	if len(pl.createdTgt) == 0 || !pl.hasErr {
		return nil // nothing created, or everything landed
	}
	sort.Sort(sort.Reverse(sort.StringSlice(pl.createdTgt))) // deepest first
	for _, f := range pl.createdTgt {
		children, err := s.md.Nodes().ListByPrefix(ctx, pl.tgtRepo, strings.TrimSuffix(f, "/"))
		if err != nil {
			return fmt.Errorf("target sweep %s/%s: %w", pl.tgtRepo, f, err)
		}
		empty := true
		for _, c := range children {
			if c.Path != f {
				empty = false
				break
			}
		}
		if empty {
			if err := s.md.Usage().DeleteNodeWithUsage(ctx, pl.tgtRepo, f, s.now()); err != nil {
				return fmt.Errorf("target sweep %s/%s: %w", pl.tgtRepo, f, err)
			}
			pl.folders--
		}
	}
	return nil
}

// assemble is stage 5: the summary line, the §1.5 ordering (errors first,
// warnings after), the status decision, the audit row and the copy-side
// observer trigger.
func (pl *cmPipeline) assemble(ctx context.Context) *CopyMoveResult {
	verb, pp := pl.op+"ing", "copied"
	if pl.op == OpMove {
		pp = "moved"
	}
	dryPrefix := ""
	if pl.dry {
		dryPrefix = "Dry run for "
	}
	summary := fmt.Sprintf("%s%s %s to %s completed successfully, %d artifacts and %d folders were %s",
		dryPrefix, verb, pl.cmRepoPath(pl.srcRepo, pl.srcPathDisplay()), pl.cmRepoPath(pl.tgtRepo, pl.tgtRootDisplay()), pl.files, pl.folders, pp)

	out := &CopyMoveResult{Artifacts: pl.files, Folders: pl.folders, DryRun: pl.dry, HTTPStatus: http.StatusOK}
	for _, m := range pl.msgs {
		if m.Level == "ERROR" {
			out.Messages = append(out.Messages, m)
		}
	}
	lastErrStatus := 0
	for _, m := range out.Messages {
		if m.Status > 0 {
			lastErrStatus = m.Status
		}
	}
	for _, m := range pl.msgs {
		if m.Level != "ERROR" {
			out.Messages = append(out.Messages, m)
		}
	}
	if len(out.Messages) == 0 || !pl.hasErr && !pl.hasWarn {
		// The clean success body is the single INFO summary (section 1.5);
		// a warnings-only run keeps its warnings AND the summary.
		out.Messages = append(out.Messages, CopyMoveMessage{Level: "INFO", Message: summary})
	} else if !pl.hasErr {
		out.Messages = append(out.Messages, CopyMoveMessage{Level: "INFO", Message: summary})
	}
	if pl.hasErr {
		if lastErrStatus > 0 {
			out.HTTPStatus = lastErrStatus
		} else {
			out.HTTPStatus = http.StatusConflict // §1.5's fallback
		}
	}

	// Audit (one op-level row; the family bypasses Put's per-item deploy
	// audits by design — registered) and the observer trigger (copy only,
	// live runs only, files only).
	action := AuditActionCopy
	if pl.op == OpMove {
		action = AuditActionMove
	}
	pl.svc.audit(ctx, AuditEvent{
		Actor:  actor(pl.p),
		Action: action,
		Repo:   pl.tgtRepo,
		Path:   pl.tgtRootDisplay(),
		Detail: fmt.Sprintf(`{"src":%q,"artifacts":%d,"folders":%d,"dry":%t}`,
			pl.cmRepoPath(pl.srcRepo, pl.srcPathDisplay()), pl.files, pl.folders, pl.dry),
	})
	if pl.op == OpCopy && !pl.dry && len(pl.landedFD) > 0 {
		pl.svc.notifyCopyObservers(ctx, pl.tgtRepo, pl.landedFD)
	}
	return out
}

// srcPathDisplay / tgtRootDisplay render the addressing the summary line
// carries (folder roots keep their trailing slash, the storage spelling).
func (pl *cmPipeline) srcPathDisplay() string {
	if pl.srcIsFd {
		return pl.srcPath
	}
	if pl.srcDir == "" {
		return ""
	}
	return pl.srcDir
}

func (pl *cmPipeline) tgtRootDisplay() string {
	if pl.srcIsFd {
		return pl.tgtRoot
	}
	if pl.tgtRoot == "" {
		return ""
	}
	return strings.TrimSuffix(pl.tgtRoot, "/")
}

// notifyCopyObservers fires the index-recompute seam off the request path
// (the notifyReplicator contract: detached context, recover shield, never
// fails or slows the trigger).
func (s *service) notifyCopyObservers(ctx context.Context, targetRepo string, landedFiles []string) {
	if s.cmObserver == nil {
		return
	}
	obs := s.cmObserver
	// The candidate-directory set: deduped, sorted parent folders.
	seen := map[string]bool{}
	dirs := make([]string, 0, len(landedFiles))
	for _, p := range landedFiles {
		d := parentPrefix(p)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		dirs = append(dirs, d)
	}
	if len(dirs) == 0 {
		return
	}
	sort.Strings(dirs)
	detached := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if v := recover(); v != nil {
				slog.WarnContext(detached, "repo: copy observer panicked",
					"repo", targetRepo, "panic", v)
			}
		}()
		obs.AfterCopyMove(detached, OpCopy, targetRepo, dirs)
	}()
}

// Compile-time pin: the concrete service carries the capability face.
var _ CopyMoveService = (*service)(nil)
