package repo

// The trash-can domain (M12 T-345, PRD FR-106 / LC-39; the behavior anchors
// are docs/reverse/storage-layout.md section 5, docs/reverse/inv-1-core.md
// section B and docs/reverse/repo-semantics.md section 4 — the "trash can"
// row of the full feature matrix). Deleting an artifact stops being a hard
// delete: the unified content-plane delete seam (service.Delete) captures
// the node tree into the built-in `auto-trashcan` local repository first,
// marks it with the trash property five-tuple, and only then drops the
// original rows. Restore is the reverse move; empty/clean are the manual
// purges; the retention cron is the age-driven purge.
//
// The capture and restore legs ride the T-339 copy/move pipeline verbatim
// (FR-105.5: "删除 = move to auto-trashcan" — BinFlow spells it copy +
// source-delete so the delete seam keeps its exact M11 error surface) under
// the `_system_` internal identity: the pipeline's permission checks and
// copy observer are bypassed for internal runs, mirroring Artifactory's
// self-exemption of the trash chain (repo-operations.md section 1.2's
// SystemIdentity seam, mid confidence — code-only evidence, now pinned by
// the tests below).
//
// Storage layout (storage-layout.md section 5, high confidence): a captured
// node lives at `auto-trashcan/<originalRepoKey>/<originalNodePath>` — the
// path itself encodes the provenance structurally, the properties carry it
// redundantly per node (so a subtree restore of any child works even after
// property drift). The property set:
//
//	trash.time                   epoch milliseconds of the delete
//	trash.deletedBy              the deleting principal's name
//	trash.originalRepository     the source repository key
//	trash.originalRepositoryType the source rclass ("local" — only local
//	                              content is captured; remote cache
//	                              invalidation and repo teardown are not
//	                              artifact deletes, registered)
//	trash.originalPath           THIS node's original repo-relative path
//
// Locally-generated content never enters the trash (the regenerable
// families the index kernels rewrite on every deploy would fill the can
// with noise): the skip set is documented on trashSkipPath.
//
// Retention (config.xml trashcanConfig, high confidence): the default is
// enabled with a 14-day retention; the cron purges FILE nodes whose
// trash.time predates the cutoff and prunes folder rows whose subtree
// holds no file anymore. Blob reclamation is deliberately NOT the trash
// engine's business — purged trash nodes orphan their blobs exactly like a
// plain delete does, and the standing GC (or the cleanup engine's gc leg)
// reclaims them. That is also the unused-cleanup immunity argument: the
// cleanup policy leg only walks REMOTE repositories (auto-trashcan is
// local), and every GC mark walks LiveChecksumSet, which includes the
// trash repo's nodes — trash content is referenced content, so neither
// engine can reap it (pinned by tests).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// TrashRepoKey is the built-in trash-can repository key (Artifactory's
// spelling, storage-layout.md section 5 / console-ui.md's tree node).
const TrashRepoKey = "auto-trashcan"

// SystemActorName is the reserved internal identity the trash chain (and
// any future internal writer) runs as — Artifactory's `_system_`
// self-exemption precedent. The name is reserved against user creation on
// the users plane (httpapi's reserved-name set), so an audit row or a
// property naming it can never be confused with a human account.
const SystemActorName = "_system_"

// The trash property five-tuple (storage-layout.md section 5). trash.time
// is epoch milliseconds rendered as decimal text.
const (
	PropTrashTime                   = "trash.time"
	PropTrashDeletedBy              = "trash.deletedBy"
	PropTrashOriginalRepository     = "trash.originalRepository"
	PropTrashOriginalRepositoryType = "trash.originalRepositoryType"
	PropTrashOriginalPath           = "trash.originalPath"
)

// trashPropKeys is the closed key set (the restore strip and the retention
// sweep share it).
var trashPropKeys = []string{
	PropTrashTime, PropTrashDeletedBy, PropTrashOriginalRepository,
	PropTrashOriginalRepositoryType, PropTrashOriginalPath,
}

// Audit actions of the family (the props.*/artifact.* precedent: spelled
// under the domain facade; joining audit.Actions()' picker list is the
// audit owner's one-liner).
const (
	AuditActionTrashRestore   = "trash.restore"
	AuditActionTrashEmpty     = "trash.empty"
	AuditActionTrashClean     = "trash.clean"
	AuditActionTrashRetention = "trash.retention"
)

// ErrSystemRepo marks an operation refused because it addresses the
// built-in system repository (the trash can) through a user plane — the
// REST face maps it through the *StatusError it arrives in.
var ErrSystemRepo = errors.New("system repository")

// TrashDefaultRetentionDays is the spec default (14, config.xml
// trashcanConfig.retentionPeriodDays).
const TrashDefaultRetentionDays = 14

// SystemPrincipal returns the internal system identity: the `_system_`
// actor with the admin role, so every allow() check the pipeline runs
// passes by the role short-circuit (the Authorizer is never consulted —
// the identity is the exemption, not a grant). The REST planes can never
// produce this principal: authentication resolves real users only, and the
// name is reserved on the users plane.
func SystemPrincipal() *Principal {
	return &Principal{Name: SystemActorName, Role: auth.RoleAdmin, Admin: true}
}

// TrashConfig is the trash-can feature configuration (the BinFlow mapping
// of config.xml's trashcanConfig, config-formats.md's "trashcan" row). The
// ZERO value is DISABLED — every pre-T-345 stack (and every test that
// never opts in) keeps the M11 hard-delete semantics byte-for-byte, the
// AC6 zero-regression posture.
type TrashConfig struct {
	// Enabled turns the delete-seam capture on (runtime gate permitting).
	Enabled bool
	// RetentionDays is the retention window; <= 0 falls back to
	// TrashDefaultRetentionDays at read sites.
	RetentionDays int
}

// DefaultTrashConfig is the spec default: enabled, 14 days.
func DefaultTrashConfig() TrashConfig {
	return TrashConfig{Enabled: true, RetentionDays: TrashDefaultRetentionDays}
}

// TrashGate is the license-plane seam of the feature (the PackageTypeGate
// precedent: repo never imports license — the assembly adapter answers).
// nil means UNLOCKED (the unwired unit-test posture); the production
// assembly wires the trashcan addon slot's evaluation so a community
// instance keeps the M11 hard-delete behavior and the REST family answers
// the D4 403 (AC5's two forms).
type TrashGate interface {
	// Unlocked reports whether the trash-can slot may act right now.
	// Implementations must be safe for concurrent use (an atomic read).
	Unlocked(ctx context.Context) bool
}

// ConfigureTrash installs the feature configuration (the
// ConfigureFolderDownload precedent: the constructor signature stays
// stable, assembly calls this before the first request). A non-concrete
// Service is skipped with a WARN.
func ConfigureTrash(s Service, cfg TrashConfig) {
	impl, ok := s.(*service)
	if !ok {
		slog.Warn("repo: ConfigureTrash: service is not the concrete implementation; trash can stays disabled")
		return
	}
	impl.trashCfg = cfg
}

// AttachTrashGate wires the license-plane seam (the AttachReplicator
// precedent). Call after ConfigureTrash at assembly time.
func AttachTrashGate(s Service, g TrashGate) {
	impl, ok := s.(*service)
	if !ok {
		slog.Warn("repo: AttachTrashGate: service is not the concrete implementation; gate not wired")
		return
	}
	impl.trashGate = g
}

// ---- capture (the delete-seam half) ----

// trashActive is the runtime decision: configuration on AND (no gate or
// the gate unlocked). It is consulted once per eligible delete.
func (s *service) trashActive(ctx context.Context) bool {
	if !s.trashCfg.Enabled {
		return false
	}
	if s.trashGate == nil {
		return true
	}
	return s.trashGate.Unlocked(ctx)
}

// trashSkipPath reports whether path is locally-generated content the
// capture must NOT take (regenerable index families — the trash can is for
// artifacts, not for index noise). The set is the enumerated families the
// BinFlow index kernels rewrite in place, anchored on repo-operations.md
// section 1.4's exclude.local.generated (mid confidence) and T-343's
// explode exclusion precedent:
//
//	.jfrog/**                        internal metadata (the shared
//	                                  SystemInternalPathPrefixes set)
//	dists/**                         the debian index engine's tree
//	**/repodata/**, _tmp_*/**        the rpm index engine's output and
//	                                  staging directories
//	**/maven-metadata.xml[.sha1...]  the maven metadata family
//
// Everything else — including every protocol artifact path — is captured.
// Registered divergence: Artifactory's own exclusion rides repo-layout
// metadata BinFlow does not carry; this closed set is BinFlow's spelling.
func trashSkipPath(path string) bool {
	seg, _, _ := strings.Cut(path, "/")
	for _, p := range SystemInternalPathPrefixes {
		if seg == p {
			return true
		}
	}
	if seg == "dists" || strings.HasPrefix(seg, "_tmp_") {
		return true
	}
	trimmed := strings.TrimSuffix(path, "/")
	for _, part := range strings.Split(trimmed, "/") {
		if part == "repodata" {
			return true
		}
	}
	base := seg
	if i := strings.LastIndexByte(trimmed, '/'); i >= 0 {
		base = trimmed[i+1:]
	}
	return base == "maven-metadata.xml" || strings.HasPrefix(base, "maven-metadata.xml.")
}

// captureIntoTrash copies the addressed node tree into the trash can and
// marks it. It runs BEFORE any source row is dropped: a capture failure
// aborts the delete (fail-closed — an artifact stays alive rather than
// being lost uncaptured). A path with nothing to delete is not an error
// (the delete's own arms answer the idempotent 404).
func (s *service) captureIntoTrash(ctx context.Context, p *Principal, repoKey, path string) error {
	// Existence probe, in the delete seam's own order: file row first,
	// then the folder spelling (a folder addressed without its trailing
	// slash gets it back).
	isFolder := isFolderNode(path)
	if isFolder {
		dir := strings.TrimSuffix(path, "/")
		rows, err := s.md.Nodes().ListByPrefix(ctx, repoKey, dir)
		if err != nil {
			return fmt.Errorf("trash probe %s/%s: %w", repoKey, path, err)
		}
		found := false
		for _, n := range rows {
			if n.Path == path || strings.HasPrefix(n.Path, dir+"/") {
				found = true
				break
			}
		}
		if !found {
			return nil // nothing under the folder spelling: nothing to capture
		}
	} else {
		_, err := s.md.Nodes().Get(ctx, repoKey, path)
		if errors.Is(err, metadata.ErrNodeNotFound) {
			return nil // the delete will answer the 404
		}
		if err != nil {
			return fmt.Errorf("trash probe %s/%s: %w", repoKey, path, err)
		}
	}

	if err := s.ensureTrashRepo(ctx); err != nil {
		return err
	}

	// The capture move: OpCopy (the source rows are the delete seam's to
	// drop, in its own order and error surface), the system identity, the
	// internal exemption flag, and the exact-path addressing (the unix
	// into-directory/rename semantics would mangle the
	// <repoKey>/<nodePath> layout). A re-capture of the same path hits the
	// pipeline's override arm (delete-then-copy) and lands identically.
	res, err := s.CopyOrMove(ctx, SystemPrincipal(), CopyMoveRequest{
		Op:             OpCopy,
		SrcRepo:        repoKey,
		SrcPath:        path,
		TargetRepo:     TrashRepoKey,
		TargetPath:     repoKey + "/" + strings.TrimSuffix(path, "/"),
		SystemIdentity: true,
		ExactPath:      true,
	})
	if err != nil {
		return fmt.Errorf("trash capture %s/%s: %w", repoKey, path, err)
	}
	if res.HTTPStatus != http.StatusOK {
		// Partial or refused capture (per-item errors land here): the
		// delete must NOT proceed — the uncaptured files would be lost.
		first := "capture reported errors"
		for _, m := range res.Messages {
			if m.Level == "ERROR" {
				first = m.Message
				break
			}
		}
		return &StatusError{
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Trash can capture failed, the artifact was not deleted: %s", first),
			cause:   fmt.Errorf("trash capture %s/%s: %w", repoKey, path, ErrSystemRepo),
		}
	}

	// Mark the captured tree. Per-node originalPath keeps every subtree
	// restorable on its own facts.
	epoch := strconv.FormatInt(s.nowFn().UnixMilli(), 10)
	who := actor(p)
	root := repoKey + "/" + strings.TrimSuffix(path, "/")
	rows, err := s.md.Nodes().ListByPrefix(ctx, TrashRepoKey, root)
	if err != nil {
		return fmt.Errorf("trash mark %s: %w", root, err)
	}
	for _, n := range rows {
		if !isFolder && n.Path != root {
			continue // a file capture marks exactly its one row
		}
		if isFolder && n.Path != root && n.Path != root+"/" && !strings.HasPrefix(n.Path, root+"/") {
			continue
		}
		orig := strings.TrimPrefix(n.Path, repoKey+"/")
		props := map[string][]string{
			PropTrashTime:                   {epoch},
			PropTrashDeletedBy:              {who},
			PropTrashOriginalRepository:     {repoKey},
			PropTrashOriginalRepositoryType: {TypeLocal},
			PropTrashOriginalPath:           {orig},
		}
		if err := s.md.NodeProps().Merge(ctx, TrashRepoKey, n.Path, props); err != nil {
			return fmt.Errorf("trash mark %s/%s: %w", TrashRepoKey, n.Path, err)
		}
	}
	return nil
}

// ensureTrashRepo materializes the built-in repository row on first
// capture. It writes the store directly (NOT CreateRepo): the built-in is
// assembly data, not a user configuration — no validation gate, no audit
// row, no permission principal. The per-service memo is safe because the
// user planes refuse every mutation of the row afterwards.
func (s *service) ensureTrashRepo(ctx context.Context) error {
	if s.trashRepoSeen.Load() {
		return nil
	}
	_, err := s.md.Repos().Get(ctx, TrashRepoKey)
	if err == nil {
		s.trashRepoSeen.Store(true)
		return nil
	}
	if !errors.Is(err, metadata.ErrRepoNotFound) {
		return fmt.Errorf("trash repo probe: %w", err)
	}
	now := s.now()
	if err := s.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey:     TrashRepoKey,
		Type:        TypeLocal,
		PackageType: PackageGeneric,
		Description: "System trash can (auto-managed; restore/empty through the trash REST family)",
		Config:      "{}",
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		// A concurrent first-capture racing us onto the INSERT answers
		// the unique-constraint error; the row exists either way.
		if _, gerr := s.md.Repos().Get(ctx, TrashRepoKey); gerr == nil {
			s.trashRepoSeen.Store(true)
			return nil
		}
		return fmt.Errorf("trash repo create: %w", err)
	}
	s.trashRepoSeen.Store(true)
	slog.InfoContext(ctx, "repo: trash can repository materialized", "repo", TrashRepoKey)
	return nil
}

// guardSystemRepo refuses a USER-plane mutation of the built-in trash
// repository: it is system-owned state, and the trash family is its only
// writer. Reads (browse, properties, item info) stay open — the standard
// content-plane ACL governs them, and no permission target can ever name
// the key (CreateRepo refuses it), so only admin/readonly_admin see in.
func guardSystemRepo(repoKey, verb string) error {
	if repoKey != TrashRepoKey {
		return nil
	}
	return &StatusError{
		Code: http.StatusBadRequest,
		Message: fmt.Sprintf(
			"The trash can ('%s') is a system repository; %s through the trash can REST family.", TrashRepoKey, verb),
		cause: fmt.Errorf("%s %s: %w", verb, TrashRepoKey, ErrSystemRepo),
	}
}

// ---- the user-facing capability face (the REST family's service) ----

// TrashRestoreRequest is the restore addressing. Path is trash-can-relative
// (what the browser sees under auto-trashcan). The target override pair
// mirrors the REST "to" parameter; empty TargetRepo restores to the
// captured location (properties first, path structure as the fallback).
// TransactionSize is the wire's batch knob — parsed and validated,
// semantically inert in BinFlow (the pipeline is per-item; registered
// divergence).
type TrashRestoreRequest struct {
	Path            string
	TargetRepo      string
	TargetPath      string
	TransactionSize int
}

// TrashSummary is the empty/clean purge report (the REST body and the
// audit detail in one shape).
type TrashSummary struct {
	Removed int64 `json:"removed"`
	Files   int64 `json:"files"`
	Folders int64 `json:"folders"`
	Bytes   int64 `json:"bytes"`
}

// TrashService is the trash family's capability face (the CopyMoveService
// posture: NOT part of the big Service interface, reached by assertion, so
// the hand-written adapter test fakes stay untouched).
type TrashService interface {
	// TrashRestore moves one trash entry back out (the reverse move under
	// the system identity; the landed tree loses its trash.* properties).
	TrashRestore(ctx context.Context, p *Principal, req TrashRestoreRequest) (*CopyMoveResult, error)
	// TrashEmpty purges the whole can (every row; blobs are GC's business).
	TrashEmpty(ctx context.Context, p *Principal) (*TrashSummary, error)
	// TrashClean purges one addressed entry (file or folder subtree).
	TrashClean(ctx context.Context, p *Principal, path string) (*TrashSummary, error)
}

// Compile-time pin: the concrete service carries the capability face.
var _ TrashService = (*service)(nil)

// trashResolve probes one trash-can path in the delete seam's order: the
// file row first, then the folder spelling. isFolder reports which
// spelling hit.
func (s *service) trashResolve(ctx context.Context, path string) (*metadata.Node, bool, error) {
	if !isFolderNode(path) {
		if n, err := s.md.Nodes().Get(ctx, TrashRepoKey, path); err == nil {
			return n, false, nil
		} else if !errors.Is(err, metadata.ErrNodeNotFound) {
			return nil, false, fmt.Errorf("trash probe %s: %w", path, err)
		}
	}
	folder := path
	if !strings.HasSuffix(folder, "/") {
		folder += "/"
	}
	if n, err := s.md.Nodes().Get(ctx, TrashRepoKey, folder); err == nil {
		return n, true, nil
	} else if !errors.Is(err, metadata.ErrNodeNotFound) {
		return nil, false, fmt.Errorf("trash probe %s: %w", folder, err)
	}
	return nil, false, fmt.Errorf("node %s/%s: %w", TrashRepoKey, path, ErrNodeNotFound)
}

// trashProps reads one node's trash tuple (nil when unreadable).
func (s *service) trashProps(ctx context.Context, path string) map[string][]string {
	props, err := s.md.NodeProps().List(ctx, TrashRepoKey, path)
	if err != nil {
		return nil
	}
	return props
}

// TrashRestore implements TrashService: resolve the entry in the can,
// decide the destination (override > captured properties > the path
// structure), run the exact-path reverse move as the system identity,
// strip the trash markers off the landed tree, audit.
func (s *service) TrashRestore(ctx context.Context, p *Principal, req TrashRestoreRequest) (*CopyMoveResult, error) {
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	if err := validateNodePath(req.Path); err != nil {
		return nil, err
	}
	if req.TransactionSize < 0 {
		return nil, fmt.Errorf("%w: transaction-size must not be negative", ErrInvalidPath)
	}
	node, isFolder, err := s.trashResolve(ctx, req.Path)
	if err != nil {
		return nil, err
	}

	// Destination resolution: the explicit override first, then the
	// captured tuple, then the capture layout's structure.
	tgtRepo, tgtPath := req.TargetRepo, req.TargetPath
	if tgtRepo == "" {
		props := s.trashProps(ctx, node.Path)
		if v := props[PropTrashOriginalRepository]; len(v) == 1 && v[0] != "" {
			tgtRepo = v[0]
		}
		if v := props[PropTrashOriginalPath]; len(v) == 1 && v[0] != "" {
			tgtPath = v[0]
		}
		if tgtRepo == "" {
			tgtRepo, tgtPath, _ = strings.Cut(req.Path, "/")
		}
	}
	if tgtRepo == TrashRepoKey {
		return nil, &StatusError{
			Code:    http.StatusBadRequest,
			Message: "The trash can cannot be the restore destination.",
			cause:   fmt.Errorf("restore into %s: %w", TrashRepoKey, ErrSystemRepo),
		}
	}
	row, err := s.loadRepoRow(ctx, tgtRepo)
	if err != nil {
		return nil, err
	}
	if row.Type != TypeLocal {
		return nil, &StatusError{
			Code: http.StatusBadRequest,
			Message: fmt.Sprintf(
				"Restore target repository %s is a %s repository; the restore destination must be a local repository.",
				tgtRepo, row.Type),
			cause: fmt.Errorf("restore target %s: %w", tgtRepo, ErrRepoTypeNotSupported),
		}
	}

	// Exact-path spelling: a folder target keeps its trailing slash; an
	// empty target path restores the entry's basename into the target
	// repository's root.
	tgt := strings.Trim(tgtPath, "/")
	if tgt == "" {
		tgt = trashBaseName(strings.TrimSuffix(req.Path, "/"))
	}
	if isFolder || strings.HasSuffix(tgtPath, "/") {
		tgt += "/"
	}

	res, err := s.CopyOrMove(ctx, SystemPrincipal(), CopyMoveRequest{
		Op:             OpMove,
		SrcRepo:        TrashRepoKey,
		SrcPath:        node.Path,
		TargetRepo:     tgtRepo,
		TargetPath:     tgt,
		SystemIdentity: true,
		ExactPath:      true,
	})
	if err != nil {
		return nil, err
	}
	if res.HTTPStatus == http.StatusOK {
		// Strip the trash markers off the landed tree (属性复原: the
		// original properties rode along; the markers must not).
		root := strings.TrimSuffix(tgt, "/")
		rows, lerr := s.md.Nodes().ListByPrefix(ctx, tgtRepo, root)
		if lerr != nil {
			return res, fmt.Errorf("trash restore strip %s/%s: %w", tgtRepo, root, lerr)
		}
		for _, n := range rows {
			if n.Path != root && n.Path != root+"/" && !strings.HasPrefix(n.Path, root+"/") {
				continue
			}
			if serr := s.md.NodeProps().Delete(ctx, tgtRepo, n.Path, trashPropKeys); serr != nil {
				return res, fmt.Errorf("trash restore strip %s/%s: %w", tgtRepo, n.Path, serr)
			}
		}
	}
	s.audit(ctx, AuditEvent{
		Actor:  p.Name,
		Action: AuditActionTrashRestore,
		Repo:   tgtRepo,
		Path:   strings.TrimSuffix(tgt, "/"),
		Detail: fmt.Sprintf(`{"from":%q,"artifacts":%d,"folders":%d}`,
			TrashRepoKey+"/"+req.Path, res.Artifacts, res.Folders),
	})
	return res, nil
}

// trashBaseName is the last segment of a repo-relative path.
func trashBaseName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// TrashEmpty implements TrashService: purge every row of the can. The
// built-in repository row itself stays (the can persists, empty).
func (s *service) TrashEmpty(ctx context.Context, p *Principal) (*TrashSummary, error) {
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	sum, err := s.trashPurge(ctx, "")
	if err != nil {
		return nil, err
	}
	s.audit(ctx, AuditEvent{
		Actor:  p.Name,
		Action: AuditActionTrashEmpty,
		Repo:   TrashRepoKey,
		Detail: fmt.Sprintf(`{"files":%d,"folders":%d,"bytes":%d}`, sum.Files, sum.Folders, sum.Bytes),
	})
	return sum, nil
}

// TrashClean implements TrashService: purge one addressed entry.
func (s *service) TrashClean(ctx context.Context, p *Principal, path string) (*TrashSummary, error) {
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	if err := validateNodePath(path); err != nil {
		return nil, err
	}
	if _, _, err := s.trashResolve(ctx, path); err != nil {
		return nil, err
	}
	sum, err := s.trashPurge(ctx, path)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, AuditEvent{
		Actor:  p.Name,
		Action: AuditActionTrashClean,
		Repo:   TrashRepoKey,
		Path:   path,
		Detail: fmt.Sprintf(`{"files":%d,"folders":%d,"bytes":%d}`, sum.Files, sum.Folders, sum.Bytes),
	})
	return sum, nil
}

// trashPurge removes rows of the can: the whole can when path is "", one
// file row / folder subtree otherwise. Files first, then folder rows
// deepest-first, so a crash midway leaves only over-retained folders,
// never dangling children.
func (s *service) trashPurge(ctx context.Context, path string) (*TrashSummary, error) {
	sum := &TrashSummary{}
	rows, err := s.md.Nodes().ListByPrefix(ctx, TrashRepoKey, strings.TrimSuffix(path, "/"))
	if err != nil {
		return nil, fmt.Errorf("trash purge %s: %w", path, err)
	}
	dir := strings.TrimSuffix(path, "/")
	var folders []string
	for _, n := range rows {
		if path != "" && n.Path != path && n.Path != path+"/" && !strings.HasPrefix(n.Path, dir+"/") {
			continue
		}
		if isFolderNode(n.Path) {
			folders = append(folders, n.Path)
			continue
		}
		if err := s.md.Usage().DeleteNodeWithUsage(ctx, TrashRepoKey, n.Path, s.now()); err != nil {
			if errors.Is(err, metadata.ErrNodeNotFound) {
				continue
			}
			return nil, fmt.Errorf("trash purge %s: %w", n.Path, err)
		}
		sum.Files++
		sum.Bytes += n.Size
	}
	sort.Sort(sort.Reverse(sort.StringSlice(folders)))
	for _, f := range folders {
		if err := s.md.Nodes().Delete(ctx, TrashRepoKey, f); err != nil && !errors.Is(err, metadata.ErrNodeNotFound) {
			return nil, fmt.Errorf("trash purge %s: %w", f, err)
		}
		sum.Folders++
	}
	sum.Removed = sum.Files + sum.Folders
	return sum, nil
}

// ---- the retention engine (TrashGCHelper's BinFlow spelling) ----

// ActorTrashRetention is the audit actor scheduled retention runs record
// (the ActorCleanup precedent: a cron run has no human behind it).
const ActorTrashRetention = "system-trash"

// defaultTrashTick is the retention cron's cadence (the cleanup engine's
// hourly precedent; every pass is idempotent and cheap on an empty can).
const defaultTrashTick = time.Hour

// TrashEngine is the retention cron: one pass per tick purging FILE nodes
// whose trash.time (fallback: the row's updated_at) predates the retention
// cutoff, then the folder rows whose subtree no longer holds a file. Safe
// for concurrent use; RunOnce serializes passes. No data-directory lock:
// unlike the cleanup engine there is no gc leg to coordinate — purged rows
// orphan their blobs exactly like plain deletes and the standing GC
// reclaims them.
type TrashEngine struct {
	store   metadata.Store
	au      AuditLogger
	gate    TrashGate
	days    int
	tick    time.Duration
	now     func() time.Time
	running atomic.Bool
}

// TrashEngineOptions carries the engine's collaborators. Store and Audit
// are required; RetentionDays <= 0 falls back to the spec default.
type TrashEngineOptions struct {
	Store metadata.Store
	Audit AuditLogger
	// Gate is the license seam; nil means unlocked (test posture).
	Gate TrashGate
	// RetentionDays is the retention window.
	RetentionDays int
	// TickEvery is the cron cadence (default 1h).
	TickEvery time.Duration
	// Now is the clock (tests inject).
	Now func() time.Time
}

// NewTrashEngine builds the retention engine.
func NewTrashEngine(opts TrashEngineOptions) (*TrashEngine, error) {
	if opts.Store == nil {
		return nil, errors.New("repo: trash retention: metadata store is required")
	}
	if opts.Audit == nil {
		return nil, errors.New("repo: trash retention: audit logger is required")
	}
	if opts.RetentionDays <= 0 {
		opts.RetentionDays = TrashDefaultRetentionDays
	}
	if opts.TickEvery <= 0 {
		opts.TickEvery = defaultTrashTick
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	return &TrashEngine{
		store: opts.Store, au: opts.Audit, gate: opts.Gate,
		days: opts.RetentionDays, tick: opts.TickEvery, now: opts.Now,
	}, nil
}

// Run is the cron loop (the CleanupEngine.Run posture: a run error is
// logged, never fatal — the next tick retries).
func (e *TrashEngine) Run(ctx context.Context) {
	t := time.NewTicker(e.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := e.RunOnce(ctx); err != nil {
				slog.WarnContext(ctx, "repo: trash retention run failed", "error", err.Error())
			}
		}
	}
}

// TrashRetentionReport is one pass's outcome (the audit detail payload).
type TrashRetentionReport struct {
	Trigger    string `json:"trigger"`
	Skipped    string `json:"skipped,omitempty"`
	Files      int64  `json:"files"`
	Folders    int64  `json:"folders"`
	Bytes      int64  `json:"bytes"`
	FinishedAt string `json:"finishedAt"`
}

// RunOnce executes one retention pass. A locked gate (or a missing
// repository row — the trash can never used on this instance) reports the
// skip, never an error.
func (e *TrashEngine) RunOnce(ctx context.Context) (*TrashRetentionReport, error) {
	if !e.running.CompareAndSwap(false, true) {
		return nil, errors.New("repo: trash retention: a run is already in progress")
	}
	defer e.running.Store(false)

	rep := &TrashRetentionReport{Trigger: "cron", FinishedAt: e.now().Format(time.RFC3339)}
	if e.gate != nil && !e.gate.Unlocked(ctx) {
		rep.Skipped = "trash can locked (license)"
		return rep, nil
	}
	if _, err := e.store.Repos().Get(ctx, TrashRepoKey); err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			rep.Skipped = "trash can not in use (no repository row)"
			return rep, nil
		}
		return rep, fmt.Errorf("repo: trash retention: %w", err)
	}

	cutoff := e.now().Add(-time.Duration(e.days) * 24 * time.Hour)
	rows, err := e.store.Nodes().ListByPrefix(ctx, TrashRepoKey, "")
	if err != nil {
		return rep, fmt.Errorf("repo: trash retention: listing: %w", err)
	}
	var folders []string
	for _, n := range rows {
		if isFolderNode(n.Path) {
			folders = append(folders, n.Path)
			continue
		}
		if !trashExpired(ctx, e.store, n, cutoff) {
			continue
		}
		if err := e.store.Usage().DeleteNodeWithUsage(ctx, TrashRepoKey, n.Path, e.now().Format(time.RFC3339)); err != nil {
			if errors.Is(err, metadata.ErrNodeNotFound) {
				continue
			}
			return rep, fmt.Errorf("repo: trash retention: delete %s: %w", n.Path, err)
		}
		rep.Files++
		rep.Bytes += n.Size
	}
	// Folder rows whose subtree no longer holds a file (a live file
	// anywhere beneath pins the whole chain).
	for _, f := range folders {
		children, cerr := e.store.Nodes().ListByPrefix(ctx, TrashRepoKey, strings.TrimSuffix(f, "/"))
		if cerr != nil {
			return rep, fmt.Errorf("repo: trash retention: folder probe %s: %w", f, cerr)
		}
		hasFile := false
		for _, c := range children {
			if strings.HasPrefix(c.Path, f) && !isFolderNode(c.Path) {
				hasFile = true
				break
			}
		}
		if hasFile {
			continue
		}
		if err := e.store.Nodes().Delete(ctx, TrashRepoKey, f); err != nil && !errors.Is(err, metadata.ErrNodeNotFound) {
			return rep, fmt.Errorf("repo: trash retention: folder delete %s: %w", f, err)
		}
		rep.Folders++
	}

	e.auditRow(ctx, rep)
	return rep, nil
}

// trashExpired reads one node's delete moment: trash.time (epoch ms)
// first, the row's updated_at as the degraded fallback (a crash between
// the capture move and the property marking leaves an unmarked row — the
// conservative DELETE, never a silent immortal: the fallback keeps the
// retention effective on the row's own age).
func trashExpired(ctx context.Context, store metadata.Store, n *metadata.Node, cutoff time.Time) bool {
	props, err := store.NodeProps().List(ctx, TrashRepoKey, n.Path)
	if err == nil {
		if v := props[PropTrashTime]; len(v) == 1 {
			if ms, perr := strconv.ParseInt(v[0], 10, 64); perr == nil {
				return time.UnixMilli(ms).Before(cutoff)
			}
		}
	}
	updated, perr := time.Parse(time.RFC3339, n.UpdatedAt)
	if perr != nil {
		return false // unparseable: keep (the conservative direction)
	}
	return updated.Before(cutoff)
}

// auditRow records the pass (best-effort by the audit contract).
func (e *TrashEngine) auditRow(ctx context.Context, rep *TrashRetentionReport) {
	if e.au == nil {
		return
	}
	if err := e.au.Append(ctx, AuditEvent{
		Time:   e.now().Format(time.RFC3339),
		Actor:  ActorTrashRetention,
		Action: AuditActionTrashRetention,
		Repo:   TrashRepoKey,
		Detail: fmt.Sprintf(`{"files":%d,"folders":%d,"bytes":%d,"skipped":%q}`,
			rep.Files, rep.Folders, rep.Bytes, rep.Skipped),
	}); err != nil {
		slog.WarnContext(ctx, "repo: trash retention audit append failed", "error", err.Error())
	}
}
