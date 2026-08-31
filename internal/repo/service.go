package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/storage"
	"github.com/lzwzzy/binflow/internal/webhook"
)

// service is the local + remote + virtual implementation of Service (the
// virtual resolver lives in virtual.go — T-71).
type service struct {
	st    storage.Engine
	md    metadata.Store
	az    Authorizer
	au    AuditLogger
	nowFn func() time.Time
	// remoteEng is the M3 pull-through engine (architecture section 5.4);
	// Get/Delete on a remote repository dispatch to it.
	remoteEng RemoteFetcher
	// cipher seals remote repository passwords (nil when no master key is
	// configured — passwords are then dropped at write time, never stored
	// unprotected; ADR-0012 decision 4).
	cipher *remote.Cipher
	// search is the metadata store's NodeSearcher seam (T-92), resolved once
	// at assembly: the production sqlite store always provides it, and nil
	// (a store without the seam) makes the search use cases answer
	// ErrSearchUnavailable instead of touching a nil interface.
	search metadata.NodeSearcher
	// repl is the push-replication enqueue seam (M6, ADR-0021): wired by
	// AttachReplicator after New, before the first request is served. nil
	// (replication not configured, the M1~M5 default) makes the Put-tail
	// hook a no-op.
	repl Replicator
	// pkgGate is the addon-plane package-type verdict seam (M10 T-283,
	// ADR-0032 weave point 2): wired by AttachPackageTypeGate after New.
	// nil keeps the static five-type enum as the whole legality check —
	// the pre-M10 posture, byte-identical M9 behavior (invariant 1).
	pkgGate PackageTypeGate
	// cmObserver is the copy-side index-recompute seam (M12 T-339,
	// repo-operations section 1.4's "copy triggers the async metadata
	// recalculation over the candidate directories"): wired by
	// AttachCopyMoveObserver after New. nil (the default, and every stack
	// until the adapters' reindex kernels are wired at cmd assembly)
	// keeps the trigger a no-op.
	cmObserver CopyMoveObserver
	// hooks is the unified-event webhook seam (M13 T-362, ADR-0041
	// decision 1): wired by AttachWebhookEmitter after New. nil (every
	// pre-M13 stack and the unwired test posture) keeps every emit below
	// a no-op — the mutation tails' behavior is byte-identical without
	// the bus (FR-114 AC6's zero-regression posture).
	hooks WebhookEmitter
	// folderCfg/folderSlots are the folder-download configuration and its
	// concurrency semaphore (M12 T-343, repo-operations section 2.1):
	// installed by ConfigureFolderDownload (assembly/tests), spec defaults
	// otherwise.
	folderMu    sync.Mutex
	folderCfg   FolderDownloadConfig
	folderSlots chan struct{}
	// trashCfg/trashGate/trashRepoSeen are the trash-can state (M12 T-345,
	// FR-106): the ZERO config is disabled — the M11 hard-delete posture
	// every pre-T-345 stack keeps. ConfigureTrash installs it, the gate is
	// the license seam (nil = unlocked), and trashRepoSeen memoizes the
	// built-in repository row's first materialization.
	trashCfg      TrashConfig
	trashGate     TrashGate
	trashRepoSeen atomic.Bool
}

// newService wires the collaborators; New is the public constructor with the
// default clock (tests inject a controllable one).
//
// The remote engine is assembled HERE (architecture section 5.4: the
// dispatch lives inside repo.Service.Get, so neither cmd nor the adapters
// ever see the engine). Its constructor error is the ADR-0012 startup
// invariant — at-rest credentials without a master key — which must fail the
// PROCESS, not the first request: the panic surfaces through cmd assembly as
// a non-zero exit naming BINFLOW_REMOTE_CREDENTIALS_KEY (FR-15-AC9-2), the
// same startup-invariant posture as adapter.Register's panics. The
// alternative (an error-returning constructor variant that only cmd calls)
// was rejected because it leaves every other repo.New caller silently
// without remote support; flagged in the T-66 report for review.
func newService(st storage.Engine, md metadata.Store, az Authorizer, au AuditLogger, now func() time.Time) *service {
	eng, err := remote.NewEngine(st, md, remote.EngineOptions{Now: now, Logger: slog.Default()})
	if err != nil {
		panic(fmt.Sprintf("repo: remote engine startup check failed: %v", err))
	}
	// The search seam rides the same store handle; see the struct field.
	search, _ := md.Nodes().(metadata.NodeSearcher)
	svc := &service{st: st, md: md, az: az, au: au, nowFn: now, remoteEng: eng, cipher: eng.Cipher(), search: search}
	// The folder-download face starts at the §2.1 spec defaults (enabled
	// off); ConfigureFolderDownload replaces them at assembly time.
	svc.setFolderDownloadConfig(defaultFolderDownloadConfig)
	return svc
}

var _ Service = (*service)(nil)

// now returns the current UTC time formatted the metadata layer expects
// (RFC3339 text, ADR-0007).
func (s *service) now() string { return s.nowFn().Format(time.RFC3339) }

// actor resolves the audit actor name for a principal (nil = anonymous).
func actor(p *Principal) string {
	if p == nil {
		return "anonymous"
	}
	return p.Name
}

// validateRepoTypeDyn is validateRepoType with the T-283 dynamic overlay
// (architecture section 15.1.5 weave point 2): with a package-type gate
// wired, a REGISTRY-KNOWN slot extends the legal package-type set — the
// static enum's "must be one of generic, docker, maven, npm, pypi" no longer
// rejects the assembled pilot types (T-282 leftover 2) — while the static
// five keep their own class rulings (virtual docker stays refused; remote
// docker opened by T-392 onto the /v2 remote seam) and a
// known-but-locked slot answers the D3 refusal. A nil gate, or a value the
// registry does not know, keeps validateRepoType verbatim.
//
// p and repoKey feed the D3 refusal's audit row; the read paths never come
// here (D1 — the content plane's license gate is the HTTP verb face's,
// never this service's).
func (s *service) validateRepoTypeDyn(ctx context.Context, p *Principal, repoKey, rclass, packageType string) error {
	if s.pkgGate == nil {
		return validateRepoType(rclass, packageType)
	}
	v := s.pkgGate.Verdict(ctx, packageType)
	if !v.Known {
		return validateRepoType(rclass, packageType)
	}
	if err := validateRclass(rclass); err != nil {
		return err
	}
	if knownPackageTypes[packageType] && !supportedPackageTypes[rclass][packageType] {
		return errClassNotSupported(rclass, packageType)
	}
	if !v.Unlocked {
		return s.denyPackageType(ctx, p, repoKey, "", packageType, v.Refusal)
	}
	return nil
}

// denyPackageType answers the D3 refusal and records the license.addon.denied
// audit row (PRD FR-85.4: config-plane refusals are audited one row each).
// refusal is the verdict's pointed clause (e.g. `license tier 'community' <
// 'pro'`); member, when non-empty, names the virtual-member face of the
// refusal (FR-85.1④) so the message says whose package type locked it.
func (s *service) denyPackageType(ctx context.Context, p *Principal, repoKey, member, packageType, refusal string) error {
	detail, err := json.Marshal(map[string]string{"addon": packageType, "refusal": refusal})
	if err != nil {
		detail = []byte("{}") // unreachable: a flat string map always marshals
	}
	s.audit(ctx, AuditEvent{
		Actor:  actor(p),
		Action: AuditActionAddonDenied,
		Repo:   repoKey,
		Detail: string(detail),
	})
	if member != "" {
		return fmt.Errorf("%w on this instance: virtual repository member '%s' uses package type '%s' (%s)",
			ErrPackageTypeNotAvailable, member, packageType, refusal)
	}
	return fmt.Errorf("%w on this instance: package type '%s' is not available (%s)",
		ErrPackageTypeNotAvailable, packageType, refusal)
}

// requireAuthenticated rejects anonymous principals for write-path
// operations (ADR-0009: writes are never anonymous, whatever the
// anonymous-access switch says).
func requireAuthenticated(p *Principal) error {
	if p == nil {
		return fmt.Errorf("write operation: %w", ErrUnauthorized)
	}
	return nil
}

// requireAdmin gates the admin-plane operations that keep a service-level
// backstop after T-217 (FR-65/ADR-0026 decision 3): CreateRepo and UpdateRepo
// now trust the httpapi management gates (family 6's CapRepoWrite branch and
// family 7's repoManage write gate respectively — the m action reaches those
// routes by design), while DeleteRepo keeps this second door: repository
// deletion is the destructive extreme of family 6, stays global-admin-only,
// and no behavior depends on relaxing it.
func requireAdmin(p *Principal) error {
	if err := requireAuthenticated(p); err != nil {
		return err
	}
	if !p.Admin {
		return fmt.Errorf("admin privileges required: %w", ErrForbidden)
	}
	return nil
}

// allow consults the injected authorizer. A nil authorizer fails closed:
// only admin principals pass, which is the correct M1 fallback until T-11's
// implementation is wired in.
func (s *service) allow(ctx context.Context, p *Principal, repoKey, path, action string) bool {
	if p != nil && p.Admin {
		return true
	}
	if s.az == nil {
		return false
	}
	return s.az.Can(ctx, p, repoKey, path, action)
}

// audit appends one event best-effort (architecture section 3.5, technical
// debt #4): a logging failure is logged, never propagated.
func (s *service) audit(ctx context.Context, e AuditEvent) {
	if s.au == nil {
		return
	}
	if e.Time == "" {
		e.Time = s.now()
	}
	if err := s.au.Append(ctx, e); err != nil {
		slog.WarnContext(ctx, "repo: audit append failed", "action", e.Action, "repo", e.Repo, "path", e.Path, "error", err)
	}
}

// emitHook is the webhook seam's nil-safe tail (M13 T-362): every wiring
// site calls this beside its audit row — same address, same best-effort
// contract, one line each (ADR-0041 decision 1's "与既有 audit 并列同址").
func (s *service) emitHook(ctx context.Context, e webhook.Event) {
	if s.hooks == nil {
		return
	}
	s.hooks.Emit(ctx, e)
}

// loadRepoRow resolves repoKey into its repository row of ANY class; the
// class-specific branches decide what to do with it. Unknown keys map to
// ErrRepoNotFound so the metadata-layer sentinel never leaks to HTTP callers.
func (s *service) loadRepoRow(ctx context.Context, repoKey string) (*metadata.Repo, error) {
	r, err := s.md.Repos().Get(ctx, repoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return nil, fmt.Errorf("repo %q: %w", repoKey, ErrRepoNotFound)
		}
		return nil, fmt.Errorf("repo %q: %w", repoKey, err)
	}
	return r, nil
}

// loadLocalRepo resolves repoKey and asserts it is a local repository the
// service can operate on. Remote repositories are NO LONGER refused here —
// Get/Delete dispatch to the remote engine (T-66) and the write plane
// refuses them with RE-05's 405 — and virtual repositories resolve through
// their members on the read plane (T-71); only the aggregate LIST keeps the
// refusal (FR-21-AC8 is P2).
func (s *service) loadLocalRepo(ctx context.Context, repoKey string) (*metadata.Repo, error) {
	r, err := s.loadRepoRow(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	if r.Type != TypeLocal {
		if r.Type == TypeVirtual {
			return nil, fmt.Errorf("%w: virtual repositories resolve through their members on the read plane; aggregate listing is deferred (FR-21-AC8, P2)",
				ErrRepoTypeNotSupported)
		}
		return nil, fmt.Errorf("%w: %s repositories are not served by the local content plane", ErrRepoTypeNotSupported, r.Type)
	}
	return r, nil
}

// refuseNonLocalWrite answers the write plane's refusal for non-local
// classes BEFORE any body is drained or permission pair is evaluated: the
// method itself is invalid on these targets, whatever the caller's grants.
// remote is read-only — 405 + Allow: GET (RE-05, FR-20-AC9). Virtual is
// normally routed BEFORE this gate (routeVirtualWrite — un-routed virtuals
// answer the C5 405 there); the arm below is the safety net for a caller
// that forgets the routing step, so a virtual can never silently fall into
// the local write plane.
func refuseNonLocalWrite(row *metadata.Repo) error {
	switch row.Type {
	case TypeRemote:
		return &StatusError{
			Code: http.StatusMethodNotAllowed,
			Message: fmt.Sprintf(
				"Remote repository '%s' is a read-only proxy cache; deployments to remote repositories are not accepted.", row.RepoKey),
			Header: http.Header{"Allow": []string{http.MethodGet}},
			cause:  fmt.Errorf("%w: remote repositories are read-only", ErrRepoTypeNotSupported),
		}
	case TypeVirtual:
		return refuseVirtualWrite(row.RepoKey)
	}
	return nil
}

// isV2PlaneFamily reports the registry-v2 package-type family (HL-3):
// docker itself plus helmoci, whose repositories the docker /v2 plane
// serves through the same use cases (one stack, two package types — the
// helmoci adapter is only the registration shell).
func isV2PlaneFamily(packageType string) bool {
	return packageType == PackageDocker || packageType == PackageHelmOCI
}

// loadLocalDockerRepo resolves repoKey and asserts it is a local
// registry-v2 family repository (docker or helmoci, HL-3) — the /v2 use
// cases refuse to serve any other package type, even another local one
// (the plane would otherwise index generic content under a registry name).
func (s *service) loadLocalDockerRepo(ctx context.Context, repoKey string) (*metadata.Repo, error) {
	r, err := s.loadLocalRepo(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	if !isV2PlaneFamily(r.PackageType) {
		return nil, fmt.Errorf("repo %q: %w: package type is %q, not one of the registry v2 family (%s, %s)",
			repoKey, ErrRepoTypeNotSupported, r.PackageType, PackageDocker, PackageHelmOCI)
	}
	return r, nil
}

// validateDockerImage checks the image relative name: it becomes the leading
// segments of every docker node path, so the generic node-path rules apply
// verbatim (empty/dot segments, slashes, length). "" is not an image.
func validateDockerImage(image string) error {
	if image == "" {
		return fmt.Errorf("%w: image name is empty", ErrInvalidImage)
	}
	if err := validateNodePath(image); err != nil {
		return fmt.Errorf("image %q: %w", image, ErrInvalidImage)
	}
	return nil
}

// ---- Content use cases ----

// Get implements Service.Get. Addressing a folder node yields
// (nil, node, ErrIsFolder): folder rows carry metadata but no streamable
// body (their sha256 is the shared empty-marker sentinel).
//
// M3 (T-66, architecture section 5.4): a REMOTE repository dispatches to the
// pull-through engine AFTER the read gate — an unauthorized principal must
// not be able to aim BinFlow at upstream URLs. The returned reader carries
// the fetch's response hints (X-BinFlow-Cache, X-Binflow-Upstream-Error)
// for the serving adapter's structural probe; a *remote.FetchError renders
// verbatim as a *StatusError.
//
// M3 (T-71, ADR-0013): a VIRTUAL repository dispatches to the two-bucket
// member resolver (virtual.go) behind the same read gate; the winning
// member's reader is wrapped with X-BinFlow-Resolved-From on top of
// whatever hints it already carried.
func (s *service) Get(ctx context.Context, p *Principal, repoKey, path string) (io.ReadSeekCloser, *metadata.Node, error) {
	if err := validateNodePath(path); err != nil {
		return nil, nil, err
	}
	row, err := s.loadRepoRow(ctx, repoKey)
	if err != nil {
		return nil, nil, err
	}
	if !s.allow(ctx, p, repoKey, path, ActionRead) {
		// Distinguish the anonymous challenge from the authenticated denial
		// so httpapi can answer 401 vs 403 (rest-api section 1.4).
		if p == nil {
			return nil, nil, fmt.Errorf("read %s/%s: %w", repoKey, path, ErrUnauthorized)
		}
		return nil, nil, fmt.Errorf("read %s/%s: %w", repoKey, path, ErrForbidden)
	}
	switch row.Type {
	case TypeRemote:
		// T-406 (parity): the browser's FOLDER face on a remote repository.
		// Folders are never upstream resources (remote_cache validators exist
		// only for content/metadata kinds), so the cache rows are the whole
		// truth: a cached folder row serves directly, and a folder probe that
		// misses but has cached CHILDREN is materialized on read — the cache
		// plane already writes on read, and pre-T-406 landings carry file
		// rows without their ancestor folders. A childless folder probe is an
		// honest 404-shaped miss (no upstream round trip). Content paths keep
		// the engine's TTL-classed pull-through below.
		if isFolderNode(path) {
			if n, nerr := s.md.Nodes().Get(ctx, repoKey, path); nerr == nil && n.Sha256 == emptyFolderSHA {
				return nil, n, fmt.Errorf("get %s/%s: %w", repoKey, path, ErrIsFolder)
			}
			if p != nil {
				dir := strings.TrimSuffix(path, "/")
				if kids, kerr := s.md.Nodes().ListByPrefix(ctx, repoKey, dir); kerr == nil && len(kids) > 0 {
					if n, ferr := s.putFolderRow(ctx, p, repoKey, path, folderMime); ferr == nil {
						return nil, n, fmt.Errorf("get %s/%s: %w", repoKey, path, ErrIsFolder)
					}
				}
			}
			return nil, nil, fmt.Errorf("node %s/%s: %w", repoKey, path, ErrNodeNotFound)
		}
		return s.getRemote(ctx, p, repoKey, path)
	case TypeVirtual:
		return s.getVirtual(ctx, p, repoKey, path)
	case TypeLocal:
	default:
		return nil, nil, fmt.Errorf("%w: %s repositories are not served by the local content plane",
			ErrRepoTypeNotSupported, row.Type)
	}
	// Governance pattern gate, download arm (T-95/W12a, FR-24-AC4): a path
	// the repository's patterns refuse answers the SAME error as a missing
	// node — the converged dual-value ruling (download 404 / upload 409) —
	// so the refusal is indistinguishable from "never was here". The default
	// configuration short-circuits; remote/virtual reads stay ungated in M4
	// (spec-pending, see the T-95 report).
	if gov := parseGovernance(row.Config); !gov.allowsPath(path) {
		return nil, nil, fmt.Errorf("node %s/%s: %w", repoKey, path, ErrNodeNotFound)
	}
	n, err := s.md.Nodes().Get(ctx, repoKey, path)
	if err != nil {
		if errors.Is(err, metadata.ErrNodeNotFound) {
			return nil, nil, fmt.Errorf("node %s/%s: %w", repoKey, path, ErrNodeNotFound)
		}
		return nil, nil, fmt.Errorf("node %s/%s: %w", repoKey, path, err)
	}
	if n.Sha256 == emptyFolderSHA {
		// Folder node: metadata only, no body to stream. The node comes
		// back with the sentinel so adapters can branch on it.
		return nil, n, fmt.Errorf("get %s/%s: %w", repoKey, path, ErrIsFolder)
	}
	rc, _, err := s.st.Open(ctx, n.Sha256)
	if err != nil {
		// blob-first ordering plus FK make this a "blob row exists, file
		// gone" repair case, not a normal miss.
		return nil, nil, fmt.Errorf("open blob %s for %s/%s: %w", n.Sha256, repoKey, path, err)
	}
	// Engine.Open returns io.ReadCloser (ADR-0019); the DiskEngine's concrete
	// value is *os.File which also implements io.ReadSeekCloser. The Service
	// interface promises Seek — type-assert the concrete value.
	seekable, ok := rc.(io.ReadSeekCloser)
	if !ok {
		_ = rc.Close()
		return nil, nil, fmt.Errorf("open blob %s for %s/%s: storage backend does not support Seek", n.Sha256, repoKey, path)
	}
	s.audit(ctx, AuditEvent{Actor: actor(p), Action: AuditActionDownload, Repo: repoKey, Path: path})
	return seekable, n, nil
}

// getRemote is the remote branch of Get: the RE-04 six-step pull-through
// (negative cache, TTL-classed local copy, guarded upstream fetch with the
// stale-while-error downgrade). The engine's *FetchError maps verbatim onto
// a *StatusError — the unfound family wraps ErrNodeNotFound so every older
// mapping (httpapi's /api/storage included) keeps answering 404 — and
// anything else is an honest 500.
func (s *service) getRemote(ctx context.Context, p *Principal, repoKey, path string) (io.ReadSeekCloser, *metadata.Node, error) {
	if s.remoteEng == nil {
		return nil, nil, fmt.Errorf("%w: remote repositories have no engine wired", ErrRepoTypeNotSupported)
	}
	res, err := s.remoteEng.Fetch(ctx, repoKey, path)
	if err != nil {
		var fe *remote.FetchError
		if errors.As(err, &fe) {
			var cause error
			if fe.Unfound {
				cause = fmt.Errorf("node %s/%s: %w", repoKey, path, ErrNodeNotFound)
			}
			return nil, nil, &StatusError{Code: fe.Status, Message: fe.Message, cause: cause}
		}
		return nil, nil, fmt.Errorf("remote fetch %s/%s: %w", repoKey, path, err)
	}
	// [M9] ADR-0031 (the remote pull-through release, the fifth landing
	// path): a MISS is the only cache state that landed a blob in THIS call —
	// land()'s session Commit registered the in-flight GC hold — and the node
	// row referencing it is committed by the time Fetch returns, so the
	// hold's job is done. HIT/STALE served an already-referenced copy and own
	// no hold; releasing there would strip a concurrent lander's refcount.
	if res.CacheState == remote.CacheMiss && res.Node != nil {
		s.releaseGCHold(ctx, res.Node.Sha256)
		// Webhook seam: artifact/cached fires on the miss-and-land arm only
		// (webhook.md 3.1 — HIT/STALE served a copy this call did not
		// fetch).
		s.emitHook(ctx, webhook.Event{
			Domain: webhook.DomainArtifact, Type: webhook.TypeArtifactCached,
			Repo: repoKey, Path: path, Sha256: res.Node.Sha256, Size: res.Node.Size,
			Actor: hookActorOf(p),
		})
	}
	s.audit(ctx, AuditEvent{Actor: actor(p), Action: AuditActionDownload, Repo: repoKey, Path: path})
	return res.Body, res.Node, nil
}

// Put implements Service.Put: the zero-options PutWithOptions (the
// exemption is the maven metadata family's, nothing else's).
func (s *service) Put(ctx context.Context, p *Principal, repoKey, path string, body io.Reader, expect storage.BlobRef, mime string) (*metadata.Node, error) {
	return s.PutWithOptions(ctx, p, repoKey, path, body, expect, mime, PutOptions{})
}

// PutWithOptions implements Service.PutWithOptions: Put plus the
// regenerable-content exemption (see PutOptions). Ordering is the
// correctness core (architecture sections 3.2/3.3): the physical blob
// commits FIRST, then the metadata writes land blob-first (blobs row, then
// node row) so the nodes.sha256 → blobs.sha256 FK backs the invariant. A
// failure after Commit leaves an unreferenced blob — GC's grace period is
// the designed recovery path — and never a node without its blob.
func (s *service) PutWithOptions(ctx context.Context, p *Principal, repoKey, path string, body io.Reader, expect storage.BlobRef, mime string, opts PutOptions) (*metadata.Node, error) {
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	if err := validateNodePath(path); err != nil {
		return nil, err
	}
	if err := guardSystemRepo(repoKey, "deploying content"); err != nil {
		return nil, err
	}
	// Deploy properties are validated before anything is gated or drained:
	// an illegal matrix set must die as a 400 with zero side effects (the
	// atomic-rejection posture every other input shape upholds).
	if err := propSetGuard(repoKey, path, opts.Properties); err != nil {
		return nil, err
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	// The write plane's repository resolution (T-71): a virtual repository
	// swaps its addressing onto the configured local deployment member BEFORE
	// the body is drained or any permission is evaluated — the 405 of an
	// un-routed virtual answers here, and a routed write runs the target
	// member's semantics from this point on.
	repoKey, row, err := s.resolveWriteRepo(ctx, repoKey)
	if err != nil {
		return nil, err
	}

	// Governance pattern gate (T-95/W12a, FR-24-AC4): runs on the TARGET
	// repository (a routed virtual write is the member's write — the same
	// repository the quota meters) and before the body is drained, so a
	// refused pattern never costs a transfer. The default configuration
	// short-circuits inside the gate; unconfigured repositories are on the
	// M1~M3 path verbatim.
	gov := parseGovernance(row.Config)
	if !gov.allowsPath(path) {
		return nil, gov.rejectPut(repoKey, path)
	}

	folder := isFolderNode(path)
	if !folder {
		// The permission pair (repo-semantics section 3) runs before the
		// body is committed: an existing node whose checksum equals the
		// client-declared sha256 is an idempotent retransmit — it succeeds
		// with neither the deploy nor the overwrite check; a different
		// checksum requires delete permission on the old node (unless the
		// freely-rewritable exemption lifts it, PutOptions).
		idempotent, existing, err := s.authorizeContentPut(ctx, p, repoKey, path, expect.Sha256, opts.SkipOverwriteCheck)
		if err != nil {
			return nil, err
		}
		committed, err := s.commitBlob(ctx, body, expect)
		if err != nil {
			return nil, err
		}
		// Quota gate, streaming arm (GE-05/W26): the size is only known once
		// the session committed, so the check runs post-landing — a refusal
		// never writes the node (the blob stays unreferenced for GC, the
		// designed residue), which is the atomic-rejection contract.
		//
		// replaced keys on ANY existing row, idempotent or not (review B1):
		// a declared-checksum retransmit holds existing.Size == incoming
		// (same sha256 = same bytes), so its delta is 0 and checkQuota's
		// delta<=0 arm exempts it — gating `replaced` on !idempotent turned
		// the exemption into a full-size pre-check delta and 413'd every
		// checksum-declared retransmit at the ceiling.
		replaced := int64(0)
		if existing != nil {
			replaced = existing.Size
		}
		if err := s.checkQuota(ctx, p, gov, repoKey, path, committed.Size, replaced); err != nil {
			return nil, err
		}
		n, err := s.putNode(ctx, p, repoKey, path, false, committed, mime, opts.Properties)
		if err != nil {
			return nil, err
		}
		// [M9] ADR-0031: the node row is durably committed, so the hold
		// commitBlob's session Commit registered is released here (release is
		// acceleration; ordering after the metadata commit is the W-1 soundness
		// requirement). A failure on any earlier step returns WITHOUT
		// releasing — the unreferenced blob stays protected until the TTL
		// backstop, the conservative failure direction the ADR registered.
		s.releaseGCHold(ctx, committed.Sha256)
		s.audit(ctx, AuditEvent{
			Actor: p.Name, Action: AuditActionDeploy, Repo: repoKey, Path: path,
			Detail: fmt.Sprintf(`{"sha256":%q,"size":%d,"idempotent":%t}`, n.Sha256, n.Size, idempotent),
		})
		// Webhook seam: artifact/deployed on the file-deploy tail (webhook.md
		// 3.1; the idempotent-retransmit arm keeps firing — at-least-once is
		// the delivery contract and dedup belongs to the receiver, section 5.6).
		s.emitHook(ctx, webhook.Event{
			Domain: webhook.DomainArtifact, Type: webhook.TypeArtifactDeployed,
			Repo: repoKey, Path: path, Sha256: n.Sha256, Size: n.Size,
			Actor: hookActorOf(p),
		})
		// Push-replication hook (M6, ADR-0021 decision 2): the artifact is
		// landed and audited, the chain is complete — enqueue the event on a
		// detached context and move on. Folder deploys take the branch below
		// and carry no blob, so only file nodes replicate.
		s.notifyReplicator(ctx, repoKey, path, n.Sha256)
		return n, nil
	}

	// Folder deploy (trailing slash, rest-api section 1.1): an empty marker
	// node with no blob of its own. The permission gate runs BEFORE the body
	// is drained (T-12 review M3): an unauthorized principal must not make
	// the server read and discard an arbitrary-length request. The body must
	// be empty — silently discarding bytes the client believes it uploaded
	// would corrupt the caller's accounting.
	if !s.allow(ctx, p, repoKey, path, ActionWrite) {
		return nil, fmt.Errorf("write %s/%s: %w", repoKey, path, ErrForbidden)
	}
	nRead, readErr := countReader(body)
	if nRead != 0 || readErr != nil {
		// A read error on a must-be-empty body is treated as a violation:
		// "0 bytes then error" must not pass as an empty body.
		return nil, fmt.Errorf("folder deploy %s/%s: %w: body must be empty", repoKey, path, ErrInvalidPath)
	}
	n, err := s.putNode(ctx, p, repoKey, path, true, storage.BlobRef{}, mime, opts.Properties)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, AuditEvent{
		Actor: p.Name, Action: AuditActionDeploy, Repo: repoKey, Path: path,
		Detail: `{"folder":true}`,
	})
	return n, nil
}

// notifyReplicator fires the push-replication enqueue seam off the request
// path (AC ①: 非阻塞 — an upload must never fail or slow because of
// replication). The goroutine runs Enqueue on a context detached from the
// request's cancellation (the response may be written and the request ctx
// dropped before the task row lands) but inheriting its values, and the
// recover shield keeps a replication panic from taking the process down.
func (s *service) notifyReplicator(ctx context.Context, repoKey, path, sha256 string) {
	if s.repl == nil || sha256 == "" {
		return
	}
	repl := s.repl // snapshot: the hook owns its own reference from here on
	detached := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if v := recover(); v != nil {
				slog.WarnContext(detached, "repo: replication enqueue panicked",
					"repo", repoKey, "path", path, "panic", v)
			}
		}()
		repl.Enqueue(detached, repoKey, path, sha256)
	}()
}

// PutFromBlob implements Service.PutFromBlob: the checksum-deploy use case
// (zero-transfer deploy against an existing blob, rest-api.md section 1.3)
// expressed as a Service method so adapters never open blobs themselves
// (architecture section 5.1: the streaming-detail exception is "extend
// repo.Service", not "bypass it").
//
// Validation order mirrors Put: authentication, path shape, repo/local
// check, then the permission pair (idempotent retransmit vs overwrite) —
// the blob is never even opened for an unauthorized caller. The blob is
// addressed by sha256, or since T-73 by a sha1-only declaration resolved
// through the ledger's sha1 index before the permission pair (see the
// inline comment). The blob is then verified in BOTH dimensions before any
// metadata write:
//
//   - filestore (storage.Open): the physical content must exist and its
//     size backs the node row;
//   - blobs ledger (metadata.Blobs.Get): the digest record must exist.
//
// The ledger row is the digest source of truth: sha1/md5 (and the size of
// record) come from it, never from the client's claim — a client cannot
// smuggle ancillary digests into the ledger by declaring them here. A
// physical blob without a ledger row (crash-window residue) is
// ErrOrphanBlob: the node is refused rather than materialized with a
// digest record that would stay incomplete forever (BlobStore.Put is
// DO-NOTHING on conflict and never back-fills).
//
// [M9] ADR-0031 note: this path runs NO session Commit — the blob it
// references was committed by an earlier call whose hold was already
// released with ITS metadata — so it also calls no ReleaseGCHold. The hold
// set is refcounted, not owner-tagged: a release here would decrement a
// hold owned by a CONCURRENT in-flight upload of identical bytes and strip
// its W-1 protection. This path's freshness protection is mechanism A
// alone (the apply-phase Live recheck sees the node row the moment it
// commits).
func (s *service) PutFromBlob(ctx context.Context, p *Principal, repoKey, path string, ref storage.BlobRef, mime string) (*metadata.Node, error) {
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	if err := validateNodePath(path); err != nil {
		return nil, err
	}
	if err := guardSystemRepo(repoKey, "deploying content"); err != nil {
		return nil, err
	}
	if isFolderNode(path) {
		return nil, fmt.Errorf("folder deploy %s/%s: %w: checksum deploy targets files only", repoKey, path, ErrInvalidPath)
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	// The write plane's repository resolution (T-71) — same contract as Put:
	// un-routed virtuals answer the C5 405 here, routed ones land in the
	// target member.
	repoKey, repoRow, err := s.resolveWriteRepo(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	// Governance gates (T-95): the pattern refusal before anything is
	// opened; the quota pre-check below runs once the size is known (the
	// filestore probe), so a refused checksum-deploy writes nothing at all —
	// the same atomic-rejection contract as Put's streaming arm.
	gov := parseGovernance(repoRow.Config)
	if !gov.allowsPath(path) {
		return nil, gov.rejectPut(repoKey, path)
	}

	// The same permission pair as Put, keyed on the addressing digest. The
	// primary key is the client-declared sha256; since T-73 a sha1-ONLY
	// declaration addresses the blob through the ledger's sha1 index
	// (idx_blobs_sha1 — the maven ecosystem's dominant algorithm: mvn/wagon
	// CI callers exist whose only declared digest is the sha1). Resolution
	// runs BEFORE the permission pair so the idempotent-redeploy probe and
	// every step below key on the RESOLVED sha256; a sha1 miss is the C15b
	// miss (404), indistinguishable from an unknown sha256 by design.
	if ref.Sha256 == "" {
		if ref.Sha1 == "" {
			return nil, fmt.Errorf("checksum deploy %s/%s: %w: sha256 or sha1 is required", repoKey, path, ErrInvalidPath)
		}
		blobRow, err := s.md.Blobs().GetBySha1(ctx, ref.Sha1)
		if err != nil {
			if errors.Is(err, metadata.ErrNotFound) {
				return nil, fmt.Errorf("checksum deploy %s/%s: %w", repoKey, path, ErrNodeNotFound)
			}
			return nil, fmt.Errorf("ledger blob by sha1 %s: %w", ref.Sha1, err)
		}
		ref.Sha256 = blobRow.Sha256
	}
	idempotent, existing, err := s.authorizeContentPut(ctx, p, repoKey, path, ref.Sha256, false)
	if err != nil {
		return nil, err
	}

	// Filestore check: the physical content must be present.
	rc, physRef, err := s.st.Open(ctx, ref.Sha256)
	if err != nil {
		if errors.Is(err, storage.ErrBlobNotFound) {
			return nil, fmt.Errorf("checksum deploy %s/%s: %w", repoKey, path, ErrNodeNotFound)
		}
		return nil, fmt.Errorf("open blob %s for checksum deploy %s/%s: %w", ref.Sha256, repoKey, path, err)
	}
	defer rc.Close() //nolint:errcheck // read-only fd, size already taken

	// Ledger check: the digest record must be present; its sha1/md5/size are
	// authoritative. An orphan physical blob refuses the deploy.
	row, err := s.md.Blobs().Get(ctx, ref.Sha256)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			return nil, fmt.Errorf("checksum deploy %s/%s: blob %s: %w", repoKey, path, ref.Sha256, ErrOrphanBlob)
		}
		return nil, fmt.Errorf("ledger blob %s: %w", ref.Sha256, err)
	}
	committed := storage.BlobRef{
		Sha256: row.Sha256,
		Sha1:   row.Sha1,
		Md5:    row.Md5,
		Size:   physRef.Size, // the file's own size; the ledger row agrees for healthy blobs
	}
	if row.Sha256 == "" {
		committed.Sha256 = ref.Sha256
	}

	// Quota pre-check (GE-05): the size is known BEFORE any metadata write,
	// so this arm refuses with zero writes — a checksum-deploy ("秒传") is
	// quota-bound like any other landing (NFR-S23). replaced keys on any
	// existing row (review B1): a re-announcement of the same blob at the
	// same path is delta 0 and exempt, exactly like the streaming arm.
	replaced := int64(0)
	if existing != nil {
		replaced = existing.Size
	}
	if err := s.checkQuota(ctx, p, gov, repoKey, path, committed.Size, replaced); err != nil {
		return nil, err
	}

	n, err := s.putNode(ctx, p, repoKey, path, false, committed, mime, nil)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, AuditEvent{
		Actor: p.Name, Action: AuditActionDeploy, Repo: repoKey, Path: path,
		Detail: fmt.Sprintf(`{"sha256":%q,"size":%d,"idempotent":%t,"checksumDeployed":true}`, n.Sha256, n.Size, idempotent),
	})
	// Webhook seam: artifact/deployed on the checksum-deploy tail (the
	// zero-transfer deploy is a deploy, webhook.md 3.1's PUT 部署链).
	s.emitHook(ctx, webhook.Event{
		Domain: webhook.DomainArtifact, Type: webhook.TypeArtifactDeployed,
		Repo: repoKey, Path: path, Sha256: n.Sha256, Size: n.Size,
		Actor: hookActorOf(p),
	})
	return n, nil
}

// PutLandedBlob implements Service.PutLandedBlob (architecture section
// 11.13, the M3 debt closure): the blob is already committed in the
// filestore, the caller holds the storage session's digest triple, and this
// method lands the metadata — blobs row first, then the node — with ONE O(1)
// Open as the presence/size probe and zero re-reading of the bytes. The
// docker finalize path used to stream the landed blob back through Put (an
// O(size) re-read plus a second digest pass per push); this variant is the
// use case that delete-the-workaround was waiting for (the future
// maven/npm chunked uploads share it).
//
// The contract difference against PutFromBlob: the ledger row does NOT need
// to pre-exist — this method WRITES it (blob-first, per architecture section
// 3.2; Blobs.Put's ON CONFLICT DO NOTHING keeps any pre-existing row the
// sha1/md5 authority, which is exactly right: identical bytes cannot
// disagree on digests). ref should therefore carry the session-computed
// sha1/md5; a ref without them materializes a row they will never be
// back-filled into (the permanence PutFromBlob's ErrOrphanBlob guard exists
// to prevent on ITS path — here the caller owns a completed Commit, not a
// scavenged orphan).
func (s *service) PutLandedBlob(ctx context.Context, p *Principal, repoKey, path string, ref storage.BlobRef, mime string) (*metadata.Node, error) {
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	if err := validateNodePath(path); err != nil {
		return nil, err
	}
	if err := guardSystemRepo(repoKey, "deploying content"); err != nil {
		return nil, err
	}
	if isFolderNode(path) {
		return nil, fmt.Errorf("landed blob %s/%s: %w: folder paths have no blob of their own", repoKey, path, ErrInvalidPath)
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	// The write plane's repository resolution (T-71) — same contract as Put:
	// un-routed virtuals answer the C5 405 here, routed ones land in the
	// target member.
	repoKey, repoRow, err := s.resolveWriteRepo(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	// Governance gates (T-95): pattern refusal up front; the quota pre-check
	// runs once the O(1) size probe below has the number. This is the arm
	// docker's upload finalize and mount ride (W26c/W26-AC5): a refused
	// finalize writes neither the ledger row nor the node, so the registry
	// surface keeps zero residue of the refused push.
	gov := parseGovernance(repoRow.Config)
	if !gov.allowsPath(path) {
		return nil, gov.rejectPut(repoKey, path)
	}
	if ref.Sha256 == "" {
		return nil, fmt.Errorf("landed blob %s/%s: %w: sha256 is required", repoKey, path, ErrInvalidPath)
	}
	idempotent, existing, err := s.authorizeContentPut(ctx, p, repoKey, path, ref.Sha256, false)
	if err != nil {
		return nil, err
	}

	// O(1) presence probe: the OPEN (never a read) proves the physical blob
	// exists and yields the file's own size for the node row. A miss here is
	// a caller-or-store inconsistency — the contract hands over a COMMITTED
	// ref — so it surfaces as a plain error, not ErrNodeNotFound (which
	// would mask an internal fault as a client-addressable 404).
	rc, phys, err := s.st.Open(ctx, ref.Sha256)
	if err != nil {
		return nil, fmt.Errorf("open landed blob %s for %s/%s: %w", ref.Sha256, repoKey, path, err)
	}
	_ = rc.Close() //nolint:errcheck // read-only fd; presence and size are already taken

	// Quota pre-check (GE-05), size known before any metadata write.
	// replaced keys on any existing row (review B1): a same-digest
	// re-finalize is delta 0 and exempt, exactly like the streaming arm.
	replaced := int64(0)
	if existing != nil {
		replaced = existing.Size
	}
	if err := s.checkQuota(ctx, p, gov, repoKey, path, phys.Size, replaced); err != nil {
		return nil, err
	}

	committed := storage.BlobRef{Sha256: ref.Sha256, Sha1: ref.Sha1, Md5: ref.Md5, Size: phys.Size}
	n, err := s.putNode(ctx, p, repoKey, path, false, committed, mime, nil)
	if err != nil {
		return nil, err
	}
	// [M9] ADR-0031 (the landed-blob release — the docker layer/config
	// finalize and pypi upload path): the hold this call's caller acquired
	// with its session Commit is released once the ledger + node rows above
	// are durably committed. The acquire belongs to the adapter's Commit, the
	// release to the service that knows when the metadata landed — the exact
	// pairing ADR-0031 prescribes for the double-node docker finalize.
	s.releaseGCHold(ctx, ref.Sha256)
	s.audit(ctx, AuditEvent{
		Actor: p.Name, Action: AuditActionDeploy, Repo: repoKey, Path: path,
		Detail: fmt.Sprintf(`{"sha256":%q,"size":%d,"idempotent":%t,"landedBlob":true}`, n.Sha256, n.Size, idempotent),
	})
	// Push-replication hook (T-195 D4): the landed-blob chain is how docker
	// layer/config finalizes and pypi uploads land — without this tail those
	// artifacts never enqueued a replication task (T-175 D4: zero tasks for a
	// twine upload). Same contract as PutWithOptions: detached, non-blocking,
	// never fails the caller.
	s.notifyReplicator(ctx, repoKey, path, n.Sha256)
	return n, nil
}

// isIdempotentRedeploy reports whether the target node already holds the
// client-declared sha256 (repo-semantics section 3: same checksum = an
// idempotent retransmit that skips both the overwrite check and the deploy
// permission check). The second return is the existing node, if any, so the
// caller can run the overwrite (delete-permission) check without a second
// query.
func (s *service) isIdempotentRedeploy(ctx context.Context, repoKey, path, declaredSha256 string) (bool, *metadata.Node, error) {
	existing, err := s.md.Nodes().Get(ctx, repoKey, path)
	if err != nil {
		if errors.Is(err, metadata.ErrNodeNotFound) {
			return false, nil, nil
		}
		return false, nil, fmt.Errorf("node %s/%s: %w", repoKey, path, err)
	}
	return existing.Sha256 != "" && existing.Sha256 == declaredSha256, existing, nil
}

// authorizeContentPut is the permission pair every content-landing use case
// runs BEFORE touching bytes or rows (repo-semantics section 3): a node
// already holding the declared sha256 is an idempotent retransmit that skips
// both gates; otherwise overwriting an existing node additionally requires
// delete on it, and every landing requires write. skipOverwrite lifts the
// delete half for the freely-rewritable family (PutOptions, repo-semantics
// section 3's sidecar/metadata exemption) — the write grant stays. The pair
// lives in one place so Put, PutFromBlob and PutLandedBlob can never drift
// apart on the ordering — the security-relevant part is that the gates run
// before the body is drained or the blob is opened.
//
// The existing node (nil when the path is fresh) rides along since T-95: the
// quota gate needs the size of the row this write REPLACES, and the probe
// already read it.
func (s *service) authorizeContentPut(ctx context.Context, p *Principal, repoKey, path, declaredSha256 string, skipOverwrite bool) (idempotent bool, existing *metadata.Node, err error) {
	idempotent, existing, err = s.isIdempotentRedeploy(ctx, repoKey, path, declaredSha256)
	if err != nil {
		return false, nil, err
	}
	if idempotent {
		return true, existing, nil
	}
	if existing != nil && !skipOverwrite && !s.allow(ctx, p, repoKey, path, ActionDelete) {
		return false, nil, fmt.Errorf(
			"overwrite %s/%s: %w: user %q needs DELETE permission on the existing node",
			repoKey, path, ErrForbidden, p.Name)
	}
	if !s.allow(ctx, p, repoKey, path, ActionWrite) {
		return false, nil, fmt.Errorf("write %s/%s: %w", repoKey, path, ErrForbidden)
	}
	return false, existing, nil
}

// putNode's storage contract (architecture sections 3.2/3.3): the blobs row
// first, then the node row that references it; the nodes.sha256 FK is the
// crash backstop. Both writes happen only after the physical blob exists.
//
// Since T-95 the node write rides Usage().PutNodeWithUsage — the node row
// and the repo_usage logical-bytes counter land in ONE transaction
// (GE-05/W26b's exact accounting); the call is otherwise NodeStore.Put
// verbatim.
//
// Folder nodes (path with trailing slash) have no content of their own. The
// emptyFolderSHA sentinel satisfies the NOT NULL + FK pair on nodes.sha256
// with a dedicated blobs row — folder paths across every parent level share
// it (marker semantics, not content: two folders with the same name never
// collide, their nodes rows stay keyed by (repo_key, path)). The value lives
// in metadata.FolderMarkerSHA (T-124) so this writer and the value-based
// consumers — the snapshot manifest boundary and the GC mark walkers — share
// one spelling at compile time.
const emptyFolderSHA = metadata.FolderMarkerSHA

// ensureFolderLedger writes the shared empty-folder blob row. It runs before
// any folder node write for the same blob-first reason as content uploads.
func (s *service) ensureFolderLedger(ctx context.Context) error {
	return s.md.Blobs().Put(ctx, &metadata.Blob{
		Sha256: emptyFolderSHA, Size: 0, CreatedAt: s.now(),
	})
}

// putNode persists blob + node in the mandated order (architecture sections
// 3.2/3.3) and lands the deploy-time properties (M10 T-286): the props
// parameter is the matrix set the adapter peeled off the PUT path, applied
// to the TARGET node only (materialized ancestors are derived state and
// carry none) with the store's merge semantics — same-key value-set
// replace, other keys kept. The application sits in the shared tail of the
// whole landing family (Put/PutWithOptions/PutFromBlob/PutLandedBlob,
// section 15.3.1's "全族同链"), so no landing path can bypass it; callers
// without matrix props pass nil and write nothing.
func (s *service) putNode(ctx context.Context, p *Principal, repoKey, path string, folder bool, ref storage.BlobRef, mime string, props map[string][]string) (*metadata.Node, error) {
	// Ancestors first (ADR-0016): every ancestor directory row lands BEFORE
	// the target row, for file and folder targets alike. A crash past this
	// point can only leave benign empty folder rows behind — never a file
	// row whose parent directory row is missing.
	if err := s.materializeAncestors(ctx, p, repoKey, path); err != nil {
		return nil, err
	}
	if folder {
		n, err := s.putFolderRow(ctx, p, repoKey, path, mime)
		if err != nil {
			return nil, err
		}
		return n, s.applyDeployProps(ctx, repoKey, path, props)
	}

	existing, err := s.md.Nodes().Get(ctx, repoKey, path)
	switch {
	case err == nil:
		if existing.Sha256 == ref.Sha256 {
			// Same content (or the same folder marker): refresh the
			// modified-side fields, keep created/createdBy (repo-semantics
			// section 3).
			existing.Mime = mime
			existing.UpdatedAt = s.now()
			if err := s.md.Usage().PutNodeWithUsage(ctx, existing, s.now()); err != nil {
				return nil, fmt.Errorf("idempotent redeploy %s/%s: %w", repoKey, path, err)
			}
			return existing, s.applyDeployProps(ctx, repoKey, path, props)
		}
	case errors.Is(err, metadata.ErrNodeNotFound):
		// new node below
	default:
		return nil, fmt.Errorf("node %s/%s: %w", repoKey, path, err)
	}

	// Blob-first (architecture section 3.2, T-25 a-case ruling): the blobs
	// row (idempotent, ON CONFLICT DO NOTHING) lands before the node row
	// that references it; the FK is the crash backstop. A failure here
	// leaves at most an unreferenced blob — no node, nothing dangling.
	if err := s.md.Blobs().Put(ctx, &metadata.Blob{
		Sha256: ref.Sha256, Sha1: ref.Sha1, Md5: ref.Md5, Size: ref.Size, CreatedAt: s.now(),
	}); err != nil {
		return nil, fmt.Errorf("blob row %s: %w", ref.Sha256, err)
	}

	now := s.now()
	n := &metadata.Node{
		RepoKey: repoKey, Path: path, Sha256: ref.Sha256, Size: ref.Size, Mime: mime,
		CreatedBy: p.Name, CreatedAt: now, UpdatedAt: now,
	}
	if existing != nil {
		// Overwrite keeps the first deployment's created/createdBy and
		// updates the modified-side fields (repo-semantics section 3).
		n.CreatedBy = existing.CreatedBy
		n.CreatedAt = existing.CreatedAt
	}
	if err := s.md.Usage().PutNodeWithUsage(ctx, n, s.now()); err != nil {
		return nil, fmt.Errorf("node %s/%s: %w", repoKey, path, err)
	}
	return n, s.applyDeployProps(ctx, repoKey, path, props)
}

// applyDeployProps lands a deploy's matrix properties on the node that just
// committed. nil/empty is the common no-op (every non-matrix deploy). A
// store failure fails the deploy's outcome honestly: the node (and blob)
// are already durable — the same residue posture a post-landing quota
// refusal leaves, with GC owning the blob side — but the caller learns the
// annotation did not land instead of silently losing it.
func (s *service) applyDeployProps(ctx context.Context, repoKey, path string, props map[string][]string) error {
	if len(props) == 0 {
		return nil
	}
	if err := s.md.NodeProps().Merge(ctx, repoKey, path, props); err != nil {
		return fmt.Errorf("deploy properties %s/%s: %w", repoKey, path, err)
	}
	return nil
}

// folderMime is the mime column materialized ancestor folder rows carry. The
// FolderInfo render never reads a folder's mime (folders have no content
// type); the fixed default keeps ancestor rows uniform whatever the target
// write's declared type happens to be.
const folderMime = "application/octet-stream"

// materializeAncestors lands one folder row per ancestor directory of path,
// outermost first (ADR-0016, the directory-materialization invariant; the
// 007 migration backfills databases written before it). Each write reuses
// the folder arm verbatim — the same semantics an explicit mkdir gets — so
// an already-materialized ancestor is an idempotent refresh (updated_at
// follows its descendants, created/created_by preserved) and a fresh one is
// a new size-0 row keyed by the shared FolderMarkerSHA sentinel.
//
// Ancestors are DERIVED state and pass none of the target write's gates: no
// permission check, no governance pattern, no audit event, quota/usage delta
// 0 (folder size is 0). Legality is inherited from the target write, which
// every caller has already fully gated by the time putNode runs.
//
// The remote pull-through engine does NOT come through here — it writes
// Nodes().Put directly and materializes nothing (architecture section 11
// debt 20; M4 has no remote directory-browsing surface, so the gap is
// unobservable).
func (s *service) materializeAncestors(ctx context.Context, p *Principal, repoKey, path string) error {
	for _, dir := range ancestorDirs(path) {
		if _, err := s.putFolderRow(ctx, p, repoKey, dir, folderMime); err != nil {
			return fmt.Errorf("materialize ancestor %s/%s: %w", repoKey, dir, err)
		}
	}
	return nil
}

// putFolderRow is putNode's folder arm, extracted so the target write and
// materializeAncestors share one spelling: probe the row, then either the
// idempotent refresh (same marker sha: update mime + updated_at, keep
// created/created_by) or the blob-first fresh write (sentinel ledger row,
// then the size-0 node row through Usage().PutNodeWithUsage). A pre-existing
// non-marker row at a folder path cannot be produced by any writer; should
// one appear, it is treated exactly like an overwrite (provenance kept,
// marker written), mirroring the file arm.
func (s *service) putFolderRow(ctx context.Context, p *Principal, repoKey, folderPath, mime string) (*metadata.Node, error) {
	existing, err := s.md.Nodes().Get(ctx, repoKey, folderPath)
	switch {
	case err == nil:
		if existing.Sha256 == emptyFolderSHA {
			existing.Mime = mime
			existing.UpdatedAt = s.now()
			if err := s.md.Usage().PutNodeWithUsage(ctx, existing, s.now()); err != nil {
				return nil, fmt.Errorf("idempotent folder redeploy %s/%s: %w", repoKey, folderPath, err)
			}
			return existing, nil
		}
	case errors.Is(err, metadata.ErrNodeNotFound):
		// new row below
	default:
		return nil, fmt.Errorf("node %s/%s: %w", repoKey, folderPath, err)
	}

	// The shared empty-folder blob row must precede any folder node write
	// (same blob-first reason as content uploads; the nodes.sha256 FK is the
	// crash backstop).
	if err := s.ensureFolderLedger(ctx); err != nil {
		return nil, fmt.Errorf("folder ledger row: %w", err)
	}
	now := s.now()
	n := &metadata.Node{
		RepoKey: repoKey, Path: folderPath, Sha256: emptyFolderSHA, Size: 0, Mime: mime,
		CreatedBy: p.Name, CreatedAt: now, UpdatedAt: now,
	}
	if existing != nil {
		n.CreatedBy = existing.CreatedBy
		n.CreatedAt = existing.CreatedAt
	}
	if err := s.md.Usage().PutNodeWithUsage(ctx, n, s.now()); err != nil {
		return nil, fmt.Errorf("node %s/%s: %w", repoKey, folderPath, err)
	}
	return n, nil
}

// commitBlob runs the upload session: Append while the three digests are
// computed, then Commit against the client-declared digests. Any failure
// aborts the session; Abort is safe on every path (including Commit failure
// — the session is finalized either way).
func (s *service) commitBlob(ctx context.Context, body io.Reader, expect storage.BlobRef) (storage.BlobRef, error) {
	sess, err := s.st.BeginSession(ctx)
	if err != nil {
		return storage.BlobRef{}, fmt.Errorf("begin upload session: %w", err)
	}
	if _, err := sess.Append(ctx, body); err != nil {
		_ = sess.Abort(context.Background())
		return storage.BlobRef{}, fmt.Errorf("append upload %s: %w", sess.ID(), err)
	}
	ref, err := sess.Commit(ctx, expect)
	if err != nil {
		// Commit failure already finalized the session; Abort is idempotent
		// and covers the theoretical not-finalized path.
		_ = sess.Abort(context.Background())
		return storage.BlobRef{}, fmt.Errorf("commit upload %s: %w", sess.ID(), err)
	}
	return ref, nil
}

// releaseGCHold drops the in-flight GC hold a successful session Commit
// registered for sha256 ([M9] ADR-0031, architecture section 14.2 point 1).
// Every caller runs it strictly AFTER the metadata rows referencing the blob
// have committed — releasing earlier would re-open the W-1 window the hold
// exists to close. The engine contract makes ReleaseGCHold a no-op that
// cannot fail the caller's operation (unknown sha, double release and
// post-TTL release are all nil); the WARN exists so a future engine that
// does report something stays observable instead of silently swallowed.
//
// Of the five ADR-0031 landing paths, exactly the ones whose call CHAINS own
// a session Commit release here: Put (its own commitBlob), PutLandedBlob and
// the remote pull-through (the adapter's / engine's Commit). PutFromBlob and
// PutManifest deliberately do NOT: see their doc comments — a release
// without a matching acquire would strip a concurrent uploader's hold
// (holdSet is refcounted, not owner-tagged).
func (s *service) releaseGCHold(ctx context.Context, sha256 string) {
	if sha256 == "" || sha256 == emptyFolderSHA {
		return // folder markers ride no physical blob; nothing was acquired
	}
	if err := s.st.ReleaseGCHold(sha256); err != nil {
		slog.WarnContext(ctx, "repo: release gc hold failed", "sha256", sha256, "error", err.Error())
	}
}

// countReader drains r counting bytes; used only for the must-be-empty
// folder-deploy body.
func countReader(r io.Reader) (int64, error) {
	if r == nil {
		return 0, nil
	}
	discarded, err := io.Copy(io.Discard, r)
	return discarded, err
}

// isUniqueViolation reports whether err is a driver-level uniqueness
// constraint failure (the modernc sqlite message spells it "UNIQUE
// constraint failed"). Used to normalize the create-race path onto the
// ErrRepoExists sentinel without leaking driver details upward.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// Delete implements Service.Delete: node references only, never blobs.
//
// M3 (T-66, RE-06): a REMOTE repository delete drops the LOCAL cache only —
// the node row(s) and the remote_cache entry, never anything upstream (the
// six-step order makes the next GET refetch). The permission gate is the
// same delete grant as local; a path with nothing cached answers the
// idempotent 404.
//
// M3 (T-71): a VIRTUAL repository delete is ALWAYS the 405 — deletes do not
// propagate through the member resolution (see refuseVirtualDelete).
//
// M12 (T-345, FR-106): a LOCAL repository delete is trash-captured first
// when the feature is on (ConfigureTrash + the license gate): the node tree
// is copied into auto-trashcan under the system identity and marked with
// the trash five-tuple BEFORE any source row drops. A capture failure
// aborts the delete (fail-closed); locally-generated index families are
// skipped (trashSkipPath); the zero configuration keeps the M11 hard
// delete byte-for-byte.
func (s *service) Delete(ctx context.Context, p *Principal, repoKey, path string) error {
	if err := requireAuthenticated(p); err != nil {
		return err
	}
	if err := validateNodePath(path); err != nil {
		return err
	}
	if err := guardSystemRepo(repoKey, "deleting content"); err != nil {
		return err
	}
	row, err := s.loadRepoRow(ctx, repoKey)
	if err != nil {
		return err
	}
	if row.Type == TypeRemote {
		return s.deleteRemoteCache(ctx, p, repoKey, path)
	}
	if row.Type == TypeVirtual {
		// T-71: deletes never propagate through a virtual repository —
		// cache deletes belong to the remote member itself (RE-06), artifact
		// deletes to the member that holds them. Even a configured write
		// route does not change this (BinFlow's deliberate incompatibility,
		// PRD section 2.2 / RE-08).
		return refuseVirtualDelete(row.RepoKey, virtualWriteRouted(row.Config))
	}
	if !s.allow(ctx, p, repoKey, path, ActionDelete) {
		return fmt.Errorf("delete %s/%s: %w", repoKey, path, ErrForbidden)
	}

	// The trash capture (T-345): only LOCAL artifact content, only when
	// the feature is live, never the regenerable index families.
	captured := false
	if s.trashActive(ctx) && !trashSkipPath(path) {
		if err := s.captureIntoTrash(ctx, p, repoKey, path); err != nil {
			return err
		}
		captured = true
	}

	nodes := s.md.Nodes()
	if isFolderNode(path) {
		// Directory delete (repo-semantics section 4): the folder row and
		// everything under it go, then empty folder rows up the chain are
		// pruned. DeleteByPrefix("d") matches the exact row plus the "d/%"
		// subtree (folder row itself and every child). The trailing-slash
		// spelling must NOT be passed: likePrefix("d/") builds "d//%",
		// matching nothing (same root cause as review B1/B2).
		//
		// M2 (review): a same-named FILE "d" legally coexisting with the
		// folder "d/" must be spared — the delete targets the DIRECTORY, not
		// an unrelated file whose name lacks the slash. The store's prefix
		// query cannot express that distinction, so the rows are listed
		// (slash-stripped prefix: "d/" would build the dead "d//%" arm) and
		// filtered in process: the folder row itself and everything beneath
		// it go, one exact delete per row (small N; SQLite serializes).
		//
		// Permission note (T-12 review M4): the delete grant is checked once
		// for the folder path, not per descendant. M1's path-prefix ACLs
		// make this equivalent for typical grants; a user authorized on d/
		// but not on a nested d/sub/ target can delete d/sub/ content. M1
		// accepts this simplification; per-node recursion is an M2+ option.
		dir := strings.TrimSuffix(path, "/")
		candidates, err := nodes.ListByPrefix(ctx, repoKey, dir)
		if err != nil {
			return fmt.Errorf("delete folder %s/%s: %w", repoKey, path, err)
		}
		var removed int64
		for _, v := range candidates {
			if v.Path != path && !strings.HasPrefix(v.Path, dir+"/") {
				continue // exact-arm same-named FILE: not part of the directory
			}
			if err := s.md.Usage().DeleteNodeWithUsage(ctx, repoKey, v.Path, s.now()); err != nil {
				if errors.Is(err, metadata.ErrNodeNotFound) {
					continue // concurrent delete of the same row
				}
				return fmt.Errorf("delete folder %s/%s: %w", repoKey, path, err)
			}
			removed++
		}
		if removed == 0 {
			return fmt.Errorf("node %s/%s: %w", repoKey, path, ErrNodeNotFound)
		}
		if err := s.pruneEmptyParents(ctx, repoKey, path); err != nil {
			return err
		}
		s.audit(ctx, AuditEvent{
			Actor: p.Name, Action: AuditActionDelete, Repo: repoKey, Path: path,
			Detail: fmt.Sprintf(`{"removed":%d,"trash":%t}`, removed, captured),
		})
		return nil
	}

	doomed, err := nodes.Get(ctx, repoKey, path)
	if err != nil {
		if errors.Is(err, metadata.ErrNodeNotFound) {
			return fmt.Errorf("node %s/%s: %w", repoKey, path, ErrNodeNotFound)
		}
		return fmt.Errorf("node %s/%s: %w", repoKey, path, err)
	}
	// Usage-aware delete (T-95/W27): the counter falls back with the node —
	// deleting under a full quota frees room for the next write.
	if err := s.md.Usage().DeleteNodeWithUsage(ctx, repoKey, path, s.now()); err != nil {
		return fmt.Errorf("delete node %s/%s: %w", repoKey, path, err)
	}
	// Deleting a leaf may empty its parent folders; prune them so listings
	// show directories that actually contain something.
	if err := s.pruneEmptyParents(ctx, repoKey, path); err != nil {
		return err
	}
	if captured {
		s.audit(ctx, AuditEvent{Actor: p.Name, Action: AuditActionDelete, Repo: repoKey, Path: path,
			Detail: `{"trash":true}`})
	} else {
		s.audit(ctx, AuditEvent{Actor: p.Name, Action: AuditActionDelete, Repo: repoKey, Path: path})
	}
	// Webhook seam: artifact/deleted on the unified delete tail (webhook.md
	// 3.1; the trash capture is the SAME delete seen through the safety
	// net — one event, never two, the trash chain's own copy/move runs are
	// system-identity and stay silent below).
	s.emitHook(ctx, webhook.Event{
		Domain: webhook.DomainArtifact, Type: webhook.TypeArtifactDeleted,
		Repo: repoKey, Path: path, Sha256: doomed.Sha256, Size: doomed.Size,
		Actor: hookActorOf(p),
	})
	return nil
}

// deleteRemoteCache is the remote branch of Delete (RE-06): permission,
// then the engine's local-cache invalidation. The engine drops node rows
// and cache entries without any upstream contact; "nothing was cached"
// maps onto the same idempotent ErrNodeNotFound the local plane answers.
func (s *service) deleteRemoteCache(ctx context.Context, p *Principal, repoKey, path string) error {
	if !s.allow(ctx, p, repoKey, path, ActionDelete) {
		return fmt.Errorf("delete %s/%s: %w", repoKey, path, ErrForbidden)
	}
	if s.remoteEng == nil {
		return fmt.Errorf("%w: remote repositories have no engine wired", ErrRepoTypeNotSupported)
	}
	existed, err := s.remoteEng.Invalidate(ctx, repoKey, path)
	if err != nil {
		return err
	}
	if !existed {
		return fmt.Errorf("node %s/%s: %w", repoKey, path, ErrNodeNotFound)
	}
	s.audit(ctx, AuditEvent{
		Actor: p.Name, Action: AuditActionDelete, Repo: repoKey, Path: path,
		Detail: `{"remoteCache":true}`,
	})
	return nil
}

// pruneEmptyParents walks from path's parent upward, deleting folder rows
// that no longer have any child. The folder row itself is deleted last, so a
// crash midway can only over-retain folders, never over-delete files.
//
// The live-child check queries with the slash-stripped form: folder rows are
// stored with a trailing slash, but ListByPrefix("d/") would build a "d//%"
// subtree arm that matches nothing (T-12 review B1 — the check was blind and
// folders with surviving children were deleted). A same-named FILE under the
// stripped prefix ("d" without slash) counts as a live child and stops the
// prune, which is the safe direction.
func (s *service) pruneEmptyParents(ctx context.Context, repoKey, path string) error {
	folder := parentPrefix(path)
	deleted := map[string]bool{}
	for folder != "" {
		children, err := s.md.Nodes().ListByPrefix(ctx, repoKey, strings.TrimSuffix(folder, "/"))
		if err != nil {
			return fmt.Errorf("prune %s/%s: %w", repoKey, folder, err)
		}
		for _, c := range children {
			if c.Path == folder || deleted[c.Path] || hasDeletedPrefix(c.Path, deleted) {
				continue
			}
			return nil // a live child remains: keep the folder chain
		}
		// The folder is empty (only its own row / already-deleted children
		// remain) — record and delete it, then check its parent.
		if err := s.md.Nodes().Delete(ctx, repoKey, folder); err != nil {
			if errors.Is(err, metadata.ErrNodeNotFound) {
				folder = parentPrefix(folder)
				continue
			}
			return fmt.Errorf("prune %s/%s: %w", repoKey, folder, err)
		}
		deleted[folder] = true
		folder = parentPrefix(folder)
	}
	return nil
}

// hasDeletedPrefix reports whether p was removed as part of an
// already-pruned subtree prefix.
func hasDeletedPrefix(p string, deleted map[string]bool) bool {
	for d := range deleted {
		if len(p) > len(d) && p[:len(d)] == d {
			return true
		}
	}
	return false
}

// List implements Service.List. The prefix is normalized before use, so
// "d" and "d/" are equivalent (both return the folder row and every node
// beneath it); without the strip, ListByPrefix("d/") would build a "d//%"
// subtree arm matching nothing (T-12 review B2).
func (s *service) List(ctx context.Context, p *Principal, repoKey, prefix string) ([]*metadata.Node, error) {
	if prefix != "" {
		prefix = strings.TrimSuffix(prefix, "/")
		if prefix == "" { // was exactly "/"
			return nil, fmt.Errorf("%w: the repository root is not a listable prefix", ErrInvalidPath)
		}
		if err := validateNodePath(prefix); err != nil {
			return nil, err
		}
	}
	// T-406 (parity): remote repositories list their CACHE — pull-through
	// landings are ordinary node rows under the remote key (remote_cache
	// holds only validators), so the browser shows cached content like
	// Artifactory's remote-cache FolderInfo (rest-api.md section 3: FileInfo
	// carries remoteUrl on remote-cache rows); the upstream is never probed
	// from the listing face. Virtual aggregate listing keeps its refusal
	// (FR-21-AC8, P2) — the console renders a member-aware empty state.
	row, err := s.loadRepoRow(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	if row.Type == TypeVirtual {
		return nil, fmt.Errorf("%w: virtual repositories resolve through their members on the read plane; aggregate listing is deferred (FR-21-AC8, P2)",
			ErrRepoTypeNotSupported)
	}
	if !s.allow(ctx, p, repoKey, prefix, ActionRead) {
		if p == nil {
			return nil, fmt.Errorf("read %s/%s: %w", repoKey, prefix, ErrUnauthorized)
		}
		return nil, fmt.Errorf("read %s/%s: %w", repoKey, prefix, ErrForbidden)
	}
	nodes, err := s.md.Nodes().ListByPrefix(ctx, repoKey, prefix)
	if err != nil {
		return nil, fmt.Errorf("list %s/%s: %w", repoKey, prefix, err)
	}
	return nodes, nil
}

// ---- Docker use cases (M2, FR-7 through FR-9) ----

// dockerPermPath is the ACL path of an image: the trailing-slash folder form
// M1's path-prefix grants key on ("acme/team/app" -> "acme/team/app/"), so a
// grant on the folder covers every manifest/blob/tag beneath it — the same
// shape ADR-0010 clause 5 derives from the scope subject <repoKey>/<image>.
func dockerPermPath(image string) string { return image + "/" }

// dockerImageTypeOf maps a manifest media type onto the webhook envelope's
// image_type pair (webhook.md 3.3: "oci"/"docker") — the OCI media-type
// family is the discriminator, everything else (the application/vnd.docker.*
// family) is the docker spelling.
func dockerImageTypeOf(mediaType string) string {
	if strings.HasPrefix(mediaType, "application/vnd.oci.") {
		return "oci"
	}
	return "docker"
}

// PutManifest implements Service.PutManifest. Ordering follows the content
// Put contract: the manifest body's blob is already committed by the adapter
// (blobs row included — same checksums as the body), so this method's first
// metadata write is the NODE at the layout path; the index rows follow. A
// crash midway leaves an indexed-but-unwalkable manifest at worst, never a
// node without its blob.
//
// Permission pair: write on the image grants a publish; overwriting the node
// path of a DIFFERENT digest (impossible for a compliant push, possible for
// a forged internal call) additionally requires delete, mirroring Put.
//
// [M9] ADR-0031 note: no ReleaseGCHold here, by design. The manifest body's
// blob was committed by the adapter's step-1 svc.Put — WHICH ALREADY RELEASED
// its hold once the <image>/blobs/<hex> node row landed — and the layer
// blobs below were landed by earlier PutLandedBlob calls that released their
// own. This method owns zero acquires; releasing the manifest digest again
// would strip a concurrent repush's in-flight hold (holdSet is refcounted,
// not owner-tagged). The premise — every PutManifest caller lands the body
// through the blob plane first — is the service contract documented above
// and the only in-tree caller shape (the docker adapter's two-step).
func (s *service) PutManifest(ctx context.Context, p *Principal, repoKey, image, digest, tag, mediaType string, size int64, refs []*metadata.DockerRef) (*PutManifestResult, error) {
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	if err := validateDockerImage(image); err != nil {
		return nil, err
	}
	if err := validateDigest(digest); err != nil {
		return nil, err
	}
	if tag != "" {
		if err := validateTag(tag); err != nil {
			return nil, err
		}
	}
	if mediaType == "" {
		return nil, fmt.Errorf("manifest %s/%s@%s: %w: media type is empty", repoKey, image, digest, ErrInvalidManifest)
	}
	if size < 0 {
		return nil, fmt.Errorf("manifest %s/%s@%s: %w: negative size", repoKey, image, digest, ErrInvalidManifest)
	}
	for i, r := range refs {
		if r == nil {
			return nil, fmt.Errorf("manifest %s/%s@%s: %w: ref %d is nil", repoKey, image, digest, ErrInvalidManifest, i)
		}
		if err := validateDigest(r.BlobDigest); err != nil {
			return nil, fmt.Errorf("manifest %s/%s@%s ref %d: %w", repoKey, image, digest, i, err)
		}
	}
	dockerRow, err := s.loadLocalDockerRepo(ctx, repoKey)
	if err != nil {
		return nil, err
	}

	permPath := dockerPermPath(image)
	nodePath := dockerImageManifestPath(image, digest)

	// Governance gates (T-95): the pattern refusal on the manifest's LAYOUT
	// path (docker patterns are P2-observation ground — no W assertion
	// either way).
	//
	// NO quota check here, deliberately: the docker data model gives one
	// manifest body TWO node rows (<image>/blobs/<hex> landed by the
	// adapter's step-1 svc.Put, <image>/manifests/<hex> written by this
	// method), and the step-1 upload has ALREADY run the quota gate on
	// exactly these bytes. Re-checking here would demand the ceiling cover
	// the manifest twice — a push that fits would 413. The wire stays
	// bounded at every blob upload (layers through PutLandedBlob, the
	// manifest body through Put); the counter, counting node references,
	// does carry the manifest twice (a documented modeling artifact of the
	// two-node layout, see the T-95 report).
	gov := parseGovernance(dockerRow.Config)
	if !gov.allowsPath(nodePath) {
		return nil, gov.rejectPut(repoKey, nodePath)
	}

	// stored holds the existing manifest row on an idempotent republish (the
	// result must report the serving truth, not the caller's re-announcement).
	var stored *metadata.DockerManifest

	// Idempotent republish probe: the immutable body at the same digest.
	// A same-digest re-push re-runs the tag/refs writes (both upserts) and
	// refreshes the modified-side columns — and, like the generic Put's
	// retransmit row (repo-semantics section 3), it skips the write gate:
	// nothing observable changes except freshness.
	existing, err := s.md.Nodes().Get(ctx, repoKey, nodePath)
	idempotent := false
	switch {
	case err == nil:
		if existing.Sha256 == digest {
			idempotent = true
		} else {
			// The layout path is digest-keyed, so this can only be a forged
			// call or a hash collision; both refuse unless the caller also
			// holds delete on the image.
			if !s.allow(ctx, p, repoKey, permPath, ActionDelete) {
				return nil, fmt.Errorf(
					"overwrite %s/%s: %w: user %q needs DELETE permission on the existing manifest node",
					repoKey, nodePath, ErrForbidden, p.Name)
			}
		}
	case errors.Is(err, metadata.ErrNodeNotFound):
		// fresh publish
	default:
		return nil, fmt.Errorf("node %s/%s: %w", repoKey, nodePath, err)
	}

	if !idempotent {
		if !s.allow(ctx, p, repoKey, permPath, ActionWrite) {
			return nil, fmt.Errorf("write %s/%s: %w", repoKey, permPath, ErrForbidden)
		}
	}

	// The tag's previous pointer, for the result's TagRepointed report. Read
	// BEFORE any write so the value reflects the pre-call state even when the
	// writes below refresh it. The contract: non-empty only when this call
	// actually MOVES the tag (a fresh tag or a same-digest republish that
	// leaves it in place reports "").
	var repointedFrom string
	if tag != "" {
		if prev, err := s.md.Docker().GetTag(ctx, repoKey, image, tag); err == nil {
			if prev.Digest != digest {
				repointedFrom = prev.Digest
			}
		} else if !errors.Is(err, metadata.ErrTagNotFound) {
			return nil, fmt.Errorf("tag %s/%s:%s: %w", repoKey, image, tag, err)
		}
	}

	// Node first: every index row below is derivable from the store's blobs
	// plus this node (the crash-recovery order mirrors Put's blob-first
	// rule with the blob already committed).
	n, err := s.putNode(ctx, p, repoKey, nodePath, false,
		storage.BlobRef{Sha256: digest, Size: size}, mediaType, nil)
	if err != nil {
		return nil, err
	}

	dk := s.md.Docker()
	now := s.now()
	if !idempotent {
		// Fresh publish: the index row lands. On an idempotent republish the
		// row is deliberately LEFT ALONE — the manifest body is immutable
		// (same digest = same bytes), so re-announcing it must not move
		// created_by/media_type/size: a zero-grant caller re-announcing a
		// digest must not be able to drift the served Content-Type of an
		// existing manifest (T-35 review B2 — the upsert would have rewritten
		// all four columns; the node path already kept provenance, this
		// closes the index side of the asymmetry).
		if err := dk.PutManifest(ctx, &metadata.DockerManifest{
			RepoKey: repoKey, Image: image, Digest: digest,
			MediaType: mediaType, Size: size, CreatedBy: p.Name, CreatedAt: now,
		}); err != nil {
			return nil, fmt.Errorf("manifest row %s/%s@%s: %w", repoKey, image, digest, err)
		}
	} else if m, err := dk.GetManifest(ctx, repoKey, image, digest); err == nil {
		// The result reports the STORED manifest, not the caller's claim —
		// the row is the /v2 plane's serving truth.
		stored = m
	} else if !errors.Is(err, metadata.ErrManifestNotFound) {
		return nil, fmt.Errorf("manifest row %s/%s@%s: %w", repoKey, image, digest, err)
	}
	if tag != "" {
		if err := dk.PutTag(ctx, &metadata.DockerTag{
			RepoKey: repoKey, Image: image, Tag: tag, Digest: digest,
			UpdatedBy: p.Name, UpdatedAt: now,
		}); err != nil {
			return nil, fmt.Errorf("tag row %s/%s:%s: %w", repoKey, image, tag, err)
		}
	}
	// Refs replace the manifest's whole set atomically (an empty set clears).
	rows := make([]*metadata.DockerRef, len(refs))
	for i, r := range refs {
		rows[i] = &metadata.DockerRef{
			RepoKey: repoKey, Image: image, ManifestDigest: digest,
			BlobDigest: r.BlobDigest, ChildMediaType: r.ChildMediaType,
		}
	}
	if err := dk.PutRefs(ctx, repoKey, image, digest, rows); err != nil {
		return nil, fmt.Errorf("refs of %s/%s@%s: %w", repoKey, image, digest, err)
	}

	s.audit(ctx, AuditEvent{
		Actor: p.Name, Action: AuditActionDeploy, Repo: repoKey, Path: nodePath,
		Detail: fmt.Sprintf(`{"digest":%q,"tag":%q,"size":%d,"refs":%d,"idempotent":%t}`,
			digest, tag, size, len(rows), idempotent),
	})
	// Webhook seam: docker/pushed fires on the tag-publish tail (webhook.md
	// 3.3 — "new tag push"; digest-only publishes carry no tag row and stay
	// silent; the layer/config blobs landed through PutLandedBlob and never
	// fire the artifact domain, the /v2 plane's event is THIS one).
	if tag != "" {
		s.emitHook(ctx, webhook.Event{
			Domain: webhook.DomainDocker, Type: webhook.TypeDockerPushed,
			Repo: repoKey, Path: nodePath, Sha256: digest, Size: size,
			ImageName: image, Tag: tag, ImageType: dockerImageTypeOf(mediaType),
			Actor: hookActorOf(p),
		})
	}
	// Push-replication hook (T-195 D2): the manifest NODE task is what drives
	// the protocol-aware docker push plane (the /v2 manifest PUT that lands
	// the manifest row AND the tag pointers on the target); the body blob's
	// task fired from the adapter's step-1 Put above. A tag repoint re-fires
	// this hook, so re-pointed tags re-converge on the target.
	s.notifyReplicator(ctx, repoKey, nodePath, digest)
	// On an idempotent republish the result carries the STORED manifest row
	// (unchanged provenance and serving columns); on a fresh publish the row
	// just written. `stored` is only ever set on the idempotent path.
	reported := stored
	if reported == nil {
		reported = &metadata.DockerManifest{
			RepoKey: repoKey, Image: image, Digest: digest,
			MediaType: mediaType, Size: size, CreatedBy: p.Name, CreatedAt: now,
		}
	}
	return &PutManifestResult{Manifest: reported, Node: n, TagRepointed: repointedFrom}, nil
}

// ResolveManifest implements Service.ResolveManifest: the index row of one
// digest. The read gate is the image folder (r).
func (s *service) ResolveManifest(ctx context.Context, p *Principal, repoKey, image, digest string) (*metadata.DockerManifest, error) {
	if err := validateDockerImage(image); err != nil {
		return nil, err
	}
	if err := validateDigest(digest); err != nil {
		return nil, err
	}
	row, err := s.loadV2ReadRepo(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	if !s.allow(ctx, p, repoKey, dockerPermPath(image), ActionRead) {
		if p == nil {
			return nil, fmt.Errorf("read %s/%s: %w", repoKey, image, ErrUnauthorized)
		}
		return nil, fmt.Errorf("read %s/%s: %w", repoKey, image, ErrForbidden)
	}
	if row.Type == TypeVirtual {
		// T-365: the virtual arm walks the member order; the first member
		// whose manifest row answers wins.
		return s.resolveV2VirtualManifest(ctx, p, repoKey, image, digest)
	}
	m, err := s.md.Docker().GetManifest(ctx, repoKey, image, digest)
	if err != nil {
		if errors.Is(err, metadata.ErrManifestNotFound) {
			return nil, fmt.Errorf("manifest %s/%s@%s: %w", repoKey, image, digest, ErrManifestNotFound)
		}
		return nil, fmt.Errorf("manifest %s/%s@%s: %w", repoKey, image, digest, err)
	}
	s.audit(ctx, AuditEvent{Actor: actor(p), Action: AuditActionDownload, Repo: repoKey, Path: nodePathFor(image, digest)})
	return m, nil
}

// nodePathFor is the layout-path spelling used by audit records.
func nodePathFor(image, digest string) string { return dockerImageManifestPath(image, digest) }

// ResolveTag implements Service.ResolveTag.
func (s *service) ResolveTag(ctx context.Context, p *Principal, repoKey, image, tag string) (*metadata.DockerTag, error) {
	if err := validateDockerImage(image); err != nil {
		return nil, err
	}
	if err := validateTag(tag); err != nil {
		return nil, err
	}
	row, err := s.loadV2ReadRepo(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	if !s.allow(ctx, p, repoKey, dockerPermPath(image), ActionRead) {
		if p == nil {
			return nil, fmt.Errorf("read %s/%s: %w", repoKey, image, ErrUnauthorized)
		}
		return nil, fmt.Errorf("read %s/%s: %w", repoKey, image, ErrForbidden)
	}
	if row.Type == TypeVirtual {
		// T-365: the first member whose tag (and its manifest) answers wins.
		return s.resolveV2VirtualTag(ctx, p, repoKey, image, tag)
	}
	t, err := s.md.Docker().GetTag(ctx, repoKey, image, tag)
	if err != nil {
		if errors.Is(err, metadata.ErrTagNotFound) {
			return nil, fmt.Errorf("tag %s/%s:%s: %w", repoKey, image, tag, ErrTagNotFound)
		}
		return nil, fmt.Errorf("tag %s/%s:%s: %w", repoKey, image, tag, err)
	}
	s.audit(ctx, AuditEvent{Actor: actor(p), Action: AuditActionDownload, Repo: repoKey, Path: dockerImageManifestPath(image, t.Digest)})
	return t, nil
}

// ListTags implements Service.ListTags. The store returns the image's tags
// in tag order; the n/last slice applies the official pagination contract on
// top (docker-registry.md section 6: exclusive last cursor, n as the page
// size, n<=0 = full list).
func (s *service) ListTags(ctx context.Context, p *Principal, repoKey, image string, n int, last string) ([]*metadata.DockerTag, error) {
	if err := validateDockerImage(image); err != nil {
		return nil, err
	}
	if last != "" {
		if err := validateTag(last); err != nil {
			// The cursor shares the tag charset; the sentinel says what the
			// caller got wrong (a cursor, not a tag) so the /v2 plane picks
			// the pagination-400 family, not a tag-shaped error.
			return nil, fmt.Errorf("tags/list cursor %q: %w", last, ErrInvalidCursor)
		}
	}
	row, err := s.loadV2ReadRepo(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	if !s.allow(ctx, p, repoKey, dockerPermPath(image), ActionRead) {
		if p == nil {
			return nil, fmt.Errorf("read %s/%s: %w", repoKey, image, ErrUnauthorized)
		}
		return nil, fmt.Errorf("read %s/%s: %w", repoKey, image, ErrForbidden)
	}
	if row.Type == TypeVirtual {
		// T-365: the member tag UNION (first-seen on shared names), with
		// the same unknown-image / tagless-image distinction.
		return s.listV2VirtualTags(ctx, repoKey, image, n, last)
	}
	tags, err := s.md.Docker().ListTagsByImage(ctx, repoKey, image)
	if err != nil {
		return nil, fmt.Errorf("tags of %s/%s: %w", repoKey, image, err)
	}
	if len(tags) == 0 {
		// Distinguish "image unknown" from "image without tags": the former
		// is NAME_UNKNOWN, the latter an empty page (docker-registry.md
		// section 6, the two official shapes). The manifest row COUNT is the
		// discriminator — checking the error alone sent both states to 404
		// (T-52 fix, a T-35 defect T-40's implementer surfaced).
		rows, err := s.md.Docker().ListManifestsByImage(ctx, repoKey, image)
		if err != nil {
			return nil, fmt.Errorf("manifests of %s/%s: %w", repoKey, image, err)
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("image %s/%s: %w", repoKey, image, ErrImageNotFound)
		}
		// Manifests exist but no tag points at any of them: the api contract's
		// empty page. tags is the store's nil slice — len 0 — which is the
		// shape the adapter renders as "tags":null (PRD R4).
		return tags, nil
	}
	tags = sliceAfterCursor(tags, n, last, func(t *metadata.DockerTag) string { return t.Tag })
	return tags, nil
}

// ListImages implements Service.ListImages (the _catalog source): image
// names prefixed with the repository key, lexicographic, n/last sliced.
func (s *service) ListImages(ctx context.Context, p *Principal, repoKey string, n int, last string) ([]string, error) {
	row, err := s.loadV2ReadRepo(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	if !s.allow(ctx, p, repoKey, "", ActionRead) {
		if p == nil {
			return nil, fmt.Errorf("read %s: %w", repoKey, ErrUnauthorized)
		}
		return nil, fmt.Errorf("read %s: %w", repoKey, ErrForbidden)
	}
	// The exclusive cursor compares against the FULL "<repoKey>/<image>"
	// name: the caller's last came from a previous page of the same shape.
	after := ""
	if last != "" {
		if !strings.HasPrefix(last, repoKey+"/") {
			return nil, fmt.Errorf("catalog cursor %q: %w: must start with %q",
				last, ErrInvalidCursor, repoKey+"/")
		}
		after = strings.TrimPrefix(last, repoKey+"/")
	}
	if row.Type == TypeVirtual {
		// T-365: the union of the members' images, rendered under the
		// VIRTUAL key (the catalog names the addressed surface).
		return s.listV2VirtualImages(ctx, repoKey, n, after)
	}
	images, err := s.md.Docker().ListImages(ctx, repoKey, after, n)
	if err != nil {
		return nil, fmt.Errorf("catalog of %s: %w", repoKey, err)
	}
	out := make([]string, len(images))
	for i, img := range images {
		out[i] = repoKey + "/" + img
	}
	return out, nil
}

// sliceAfterCursor applies the official pagination slice to an ordered page:
// drop everything through the exclusive last cursor, then cap at n (n<=0 =
// no cap). Not finding the cursor is NOT an error — the official semantics
// are undefined there and an empty tail is the least surprising answer (a
// page edge that moved since the caller fetched it).
func sliceAfterCursor[T any](page []T, n int, last string, key func(T) string) []T {
	if last != "" {
		i := indexAfterCursor(page, last, key)
		if i < 0 {
			return nil // cursor beyond the tail: nothing follows it
		}
		page = page[i:]
	}
	if n > 0 && len(page) > n {
		page = page[:n]
	}
	return page
}

// indexAfterCursor returns the position just past the cursor's entry, or -1
// when the cursor is not on the page.
func indexAfterCursor[T any](page []T, last string, key func(T) string) int {
	for i, v := range page {
		if key(v) == last {
			return i + 1
		}
	}
	return -1
}

// DeleteManifest implements Service.DeleteManifest: permission plus node
// drop here, index row and its tag/ref cascade in the store's one
// transaction (architecture section 11.12 — reimplementing the cascade at
// this layer would break the same-transaction guarantee FR-9-AC6 tests).
// The referenced blobs are never touched; GC owns the bytes.
func (s *service) DeleteManifest(ctx context.Context, p *Principal, repoKey, image, digest string) error {
	if err := requireAuthenticated(p); err != nil {
		return err
	}
	if err := validateDockerImage(image); err != nil {
		return err
	}
	if err := validateDigest(digest); err != nil {
		return err
	}
	if _, err := s.loadLocalDockerRepo(ctx, repoKey); err != nil {
		return err
	}
	permPath := dockerPermPath(image)
	if !s.allow(ctx, p, repoKey, permPath, ActionDelete) {
		return fmt.Errorf("delete %s/%s: %w", repoKey, permPath, ErrForbidden)
	}

	// Resolve first: not-found must surface BEFORE the store's cascade runs.
	// The not-found branch also heals the one crash window this use case
	// has: if the store's transaction committed but the node delete below
	// never ran (process death between the two), the manifest row is gone
	// while its layout node lingers — a retry would otherwise answer 404
	// forever without ever cleaning the residue. A node whose sha256 IS the
	// digest is that residue by construction (the layout path is
	// digest-keyed and the index is the registry's authority); anything
	// else at the path is not ours to touch.
	doomedM, err := s.md.Docker().GetManifest(ctx, repoKey, image, digest)
	if err != nil {
		if !errors.Is(err, metadata.ErrManifestNotFound) {
			return fmt.Errorf("manifest %s/%s@%s: %w", repoKey, image, digest, err)
		}
		nodePath := dockerImageManifestPath(image, digest)
		if n, nerr := s.md.Nodes().Get(ctx, repoKey, nodePath); nerr == nil && n.Sha256 == digest {
			if err := s.md.Usage().DeleteNodeWithUsage(ctx, repoKey, nodePath, s.now()); err != nil && !errors.Is(err, metadata.ErrNodeNotFound) {
				return fmt.Errorf("heal node %s/%s: %w", repoKey, nodePath, err)
			}
			if err := s.pruneEmptyParents(ctx, repoKey, nodePath); err != nil {
				return err
			}
		}
		return fmt.Errorf("manifest %s/%s@%s: %w", repoKey, image, digest, ErrManifestNotFound)
	}

	// Index first, node second: the store's transaction is the atomic unit;
	// a node delete that follows can only orphan a blob (GC territory),
	// never leave a live node with no index row to walk back from.
	if err := s.md.Docker().DeleteManifest(ctx, repoKey, image, digest); err != nil {
		return fmt.Errorf("delete manifest %s/%s@%s: %w", repoKey, image, digest, err)
	}
	nodePath := dockerImageManifestPath(image, digest)
	if err := s.md.Usage().DeleteNodeWithUsage(ctx, repoKey, nodePath, s.now()); err != nil {
		if !errors.Is(err, metadata.ErrNodeNotFound) {
			return fmt.Errorf("delete node %s/%s: %w", repoKey, nodePath, err)
		}
	}
	if err := s.pruneEmptyParents(ctx, repoKey, nodePath); err != nil {
		return err
	}
	s.audit(ctx, AuditEvent{
		Actor: p.Name, Action: AuditActionDelete, Repo: repoKey, Path: nodePath,
		Detail: fmt.Sprintf(`{"digest":%q}`, digest),
	})
	// Webhook seam: docker/deleted on the registry delete tail (webhook.md
	// 3.3 — the digest-keyed delete cascades every tag the store held; the
	// tag field stays empty rather than inventing one).
	s.emitHook(ctx, webhook.Event{
		Domain: webhook.DomainDocker, Type: webhook.TypeDockerDeleted,
		Repo: repoKey, Path: nodePath, Sha256: digest, Size: doomedM.Size,
		ImageName: image, ImageType: dockerImageTypeOf(doomedM.MediaType),
		Actor: hookActorOf(p),
	})
	return nil
}

// DeleteRepoDocker implements Service.DeleteRepoDocker: the FIRST half of a
// docker repository's index teardown. It deletes each image's manifest and
// tag rows one image at a time (docker_manifests and docker_tags move
// together per image; the repositories-row FK cascade is only a backstop).
// The store's DeleteImage also clears that image's ref rows in the same
// transaction, so this half alone already handles every publish that
// happened BEFORE it. What it cannot guarantee — and why the refs sweep in
// DeleteRepo runs AFTER the repositories row is gone — is a PutRefs landing
// between this half and the row delete: that straggler has no FK, and only
// the post-delete DeleteRepoRefs collects it (T-35 review B1 — swept early,
// it would outlive the repository, pin its blob in GC's mark set forever,
// and leak into a same-named repository recreated later).
func (s *service) DeleteRepoDocker(ctx context.Context, repoKey string) (int64, error) {
	dk := s.md.Docker()
	images, err := dk.ListImages(ctx, repoKey, "", 0)
	if err != nil {
		return 0, fmt.Errorf("catalog of %q: %w", repoKey, err)
	}
	var removed int64
	for _, image := range images {
		n, err := dk.DeleteImage(ctx, repoKey, image)
		if err != nil {
			return 0, fmt.Errorf("docker teardown %q image %q: %w", repoKey, image, err)
		}
		removed += n
	}
	return removed, nil
}

// ---- Repository CRUD ----

// CreateRepo implements Service.CreateRepo. The M3 model (FR-15): the config
// blob is typed per rclass — local keeps the M1 passthrough contract, remote
// and virtual are validated, defaulted and canonicalized here — and the
// type-owned state lands right after the repositories row: the
// remote_configs row for remote (the fetcher's store; password EMPTY until
// the crypto chain of T-66, per the T-62 review's no-plaintext-window
// ruling) and the virtual_members list for virtual (positions = declaration
// order). Every validation runs BEFORE the first write so a refused create
// leaves no partial state.
func (s *service) CreateRepo(ctx context.Context, p *Principal, r *metadata.Repo) (*metadata.Repo, error) {
	// T-217 (FR-65, ADR-0026 decision 3 / architecture section 7.1 family 6):
	// repository creation is NOT delegated to manage holders, and the httpapi
	// create-arm branch (handleRepoPut's CapRepoWrite check) is the ONLY gate
	// on it — the pre-M7 service backstop was relaxed so the two doors cannot
	// drift apart. The discriminating pin is t215_create_arm_gate_test.go:
	// a manage holder whose ghost target lists the key being created must be
	// refused by THAT branch, not here. Authentication itself is still
	// non-negotiable (ADR-0009: writes are never anonymous).
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("%w: repository payload is nil", ErrInvalidRepoKey)
	}
	// T-345: the trash can's key is system-owned — a user create squatting
	// it would shadow the built-in.
	if err := guardSystemRepo(r.RepoKey, "creating it"); err != nil {
		return nil, err
	}
	if err := validateRepoKey(r.RepoKey); err != nil {
		return nil, err
	}
	// M10 T-283 (D3, weave point 2): the legality question rides the
	// dynamic overlay — registry-known slots extend the static enum, and a
	// known-but-locked slot refuses the create with the pointed clause.
	if err := s.validateRepoTypeDyn(ctx, p, r.RepoKey, r.Type, r.PackageType); err != nil {
		return nil, err
	}

	// Type-specific config: parse + validate + canonicalize. A blank config
	// parses as "{}" for remote/virtual so the "url is required" /
	// "repositories is required" refusals name the FIELD (M02b/M03) instead
	// of a JSON EOF.
	var (
		remote         *remoteConfig
		remotePassword string
		members        []string
	)
	config := r.Config
	if strings.TrimSpace(config) == "" {
		config = "{}"
	}
	var err error
	switch r.Type {
	case TypeLocal:
		if config, err = normalizeConfig(config); err != nil {
			return nil, err
		}
		if err := validateLocalConfig(config); err != nil {
			return nil, err
		}
		if err := s.validateLocalKeypairRef(ctx, r.PackageType, config); err != nil {
			return nil, err
		}
	case TypeRemote:
		if err := rejectKeypairRefOnNonLocal(r.Type, config); err != nil {
			return nil, err
		}
		rc, password, perr := parseRemoteConfig(config, r.PackageType)
		if perr != nil {
			return nil, perr
		}
		remote = &rc
		remotePassword = password
		if config, err = marshalConfig(rc); err != nil {
			return nil, err
		}
	case TypeVirtual:
		if err := rejectKeypairRefOnNonLocal(r.Type, config); err != nil {
			return nil, err
		}
		vc, perr := parseVirtualConfig(config)
		if perr != nil {
			return nil, perr
		}
		if verr := s.validateVirtualMembers(ctx, p, r.RepoKey, r.PackageType, vc); verr != nil {
			return nil, verr
		}
		members = vc.Repositories
		if config, err = marshalConfig(vc); err != nil {
			return nil, err
		}
	}

	if _, err := s.md.Repos().Get(ctx, r.RepoKey); err == nil {
		return nil, fmt.Errorf("%w: %q", ErrRepoExists, r.RepoKey)
	} else if !errors.Is(err, metadata.ErrRepoNotFound) {
		return nil, fmt.Errorf("repo %q: %w", r.RepoKey, err)
	}
	now := s.now()
	stored := &metadata.Repo{
		RepoKey: r.RepoKey, Type: r.Type, PackageType: r.PackageType,
		Description: r.Description, Config: config, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.md.Repos().Create(ctx, stored); err != nil {
		// The pre-check Get above can race a concurrent create of the same
		// key; the primary-key constraint then fails here. Map it onto the
		// same sentinel so the HTTP layer always sees ErrRepoExists (T-12
		// review nit).
		if errors.Is(err, metadata.ErrDuplicate) || isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: %q", ErrRepoExists, r.RepoKey)
		}
		return nil, fmt.Errorf("create repo %q: %w", r.RepoKey, err)
	}

	// Type-owned state, strictly after the repositories row (both tables FK
	// it). A failure here leaves the row without its config/members — the
	// retry heals through UpdateRepo (the config re-parse falls back to
	// CreateConfig when the row is missing), and nothing else can reference
	// the half-created repository meanwhile.
	switch {
	case remote != nil:
		if err := s.md.Remote().CreateConfig(ctx, &metadata.RemoteConfig{
			RepoKey:  r.RepoKey,
			URL:      remote.URL,
			Username: remote.Username,
			// The T-66 encryption chain: the accepted password lands as its
			// enc:v1 AES-256-GCM sealed form (ADR-0012 decision 4); with no
			// master key it is dropped with a WARN instead of stored
			// unprotected — the plaintext window the T-62 review closed
			// stays closed. The canonical config JSON never carries it, so
			// no echo path can leak it either (NFR-S14).
			Password: s.sealPassword(ctx, r.RepoKey, remotePassword),
			// The product default (7200, PRD C4/ADR-0012 errata two) — the
			// DDL's 86400 is a schema-level fallback for rows created
			// outside this service.
			ContentTTLSeconds:    remote.RetrievalCachePeriodSecs,
			MetadataTTLSeconds:   defaultMetadataTTLSeconds,
			AllowPrivateUpstream: remote.AllowPrivateUpstream,
			// T-290 (FR-90.2): the smart remote effective columns mirror the
			// canonical JSON the same way content_ttl_seconds mirrors
			// retrievalCachePeriodSecs — the fetcher reads the row, GET
			// echoes the JSON, both are written by this one call.
			SocketTimeoutMs:              remote.SocketTimeoutMillis,
			MetadataRetrievalTimeoutSecs: remote.MetadataRetrievalTimeoutSecs,
			UnusedCleanupPeriodHours:     remote.UnusedCleanupPeriodHours,
		}); err != nil {
			return nil, fmt.Errorf("remote config %q: %w", r.RepoKey, err)
		}
	case members != nil:
		if err := s.md.Virtual().SetMembers(ctx, r.RepoKey, members); err != nil {
			return nil, fmt.Errorf("virtual members %q: %w", r.RepoKey, err)
		}
	}

	detail := fmt.Sprintf(`{"type":%q,"packageType":%q}`, r.Type, r.PackageType)
	if remote != nil {
		// The SSRF exemption is security-relevant state: the create/update
		// audit trail always records its value (NFR-S14 point 5).
		detail = fmt.Sprintf(`{"type":%q,"packageType":%q,"allowPrivateUpstream":%t}`,
			r.Type, r.PackageType, remote.AllowPrivateUpstream)
	}
	s.audit(ctx, AuditEvent{
		Actor: p.Name, Action: AuditActionRepoCreate, Repo: r.RepoKey,
		Detail: detail,
	})
	return stored, nil
}

// validateVirtualMembers enforces the FR-15-AC4 member rules: every member
// exists, none is virtual (nested virtuals are a deliberate M3
// incompatibility, PRD section 2.2), none repeats, and the repository does
// not list itself (reachable on update — create runs before the row exists,
// but one shape serves both). defaultDeploymentRepo, when set, must be one
// of the members AND that member must be a LOCAL repository
// (repo-semantics section 8.2: the write route targets a local deployment
// repository; a remote member cannot accept deploys).
//
// M10 T-283 (FR-85.1④): a member whose package type's addon slot is locked
// refuses the create/update too — a virtual may not gain a gated member the
// instance cannot serve. Existing virtuals keep reading (D1: member
// resolution is a read path, never gated).
func (s *service) validateVirtualMembers(ctx context.Context, p *Principal, virtualKey, virtualPackageType string, cfg virtualConfig) error {
	seen := make(map[string]bool, len(cfg.Repositories))
	for _, m := range cfg.Repositories {
		if m == virtualKey {
			return fmt.Errorf("%w: virtual repository %q cannot list itself as a member", ErrInvalidRepoConfig, virtualKey)
		}
		if seen[m] {
			return fmt.Errorf("%w: virtual repository members list %q more than once", ErrInvalidRepoConfig, m)
		}
		seen[m] = true
	}
	memberRows := make([]*metadata.Repo, 0, len(cfg.Repositories))
	for _, m := range cfg.Repositories {
		row, err := s.md.Repos().Get(ctx, m)
		if err != nil {
			if errors.Is(err, metadata.ErrRepoNotFound) {
				return fmt.Errorf("%w: virtual repository member %q does not exist", ErrInvalidRepoConfig, m)
			}
			return fmt.Errorf("virtual member %q: %w", m, err)
		}
		memberRows = append(memberRows, row)
		if m == TrashRepoKey {
			// T-345: the trash can never aggregates into a virtual — a
			// member listing would expose captured (deleted) content to
			// every reader of the virtual key.
			return fmt.Errorf("%w: virtual repository member %q is the system trash can", ErrInvalidRepoConfig, m)
		}
		if row.Type == TypeVirtual {
			return fmt.Errorf(
				"%w: virtual repository member %q is itself virtual (nested virtual repositories are not supported)",
				ErrInvalidRepoConfig, m)
		}
		if cfg.DefaultDeploymentRepo != "" && m == cfg.DefaultDeploymentRepo && row.Type != TypeLocal {
			return fmt.Errorf(
				"%w: defaultDeploymentRepo %q must be a local repository member, not %s",
				ErrInvalidRepoConfig, cfg.DefaultDeploymentRepo, row.Type)
		}
		// The addon-plane member rule (FR-85.1④): only the REGISTRY-KNOWN
		// and locked shape refuses — an ungated member (the static five on
		// the floor) never meets the question, so pre-M10 stacks and
		// five-core virtuals are untouched.
		if s.pkgGate != nil {
			if v := s.pkgGate.Verdict(ctx, row.PackageType); v.Known && !v.Unlocked {
				return s.denyPackageType(ctx, p, virtualKey, m, row.PackageType, v.Refusal)
			}
		}
	}
	if err := validateHelmFamilyMix(virtualPackageType, memberRows); err != nil {
		return err
	}
	if err := validateV2MemberTypes(virtualPackageType, memberRows); err != nil {
		return err
	}
	if cfg.DefaultDeploymentRepo != "" && !seen[cfg.DefaultDeploymentRepo] {
		return fmt.Errorf(
			"%w: defaultDeploymentRepo %q is not a member of the virtual repository",
			ErrInvalidRepoConfig, cfg.DefaultDeploymentRepo)
	}
	return nil
}

// helmFamilyOf maps one package type onto the classic-Helm protocol
// family: "helm" (this repository) and "helmoci" (the registry-v2 face
// the docker adapter serves, HL-3) are the two spellings; everything else
// — "" included — is family-free.
func helmFamilyOf(packageType string) string {
	switch packageType {
	case PackageHelm, PackageHelmOCI:
		return packageType
	}
	return ""
}

// validateHelmFamilyMix enforces the Helm/HelmOCI no-mixing rule (helm.md
// section 8.2's closing note: the two protocol families cannot share one
// virtual repository — one serves index.yaml chart repos, the other the
// registry-v2 manifest plane; a mixed virtual would resolve one family's
// URLs through the other's grammar). The VIRTUAL's own package type joins
// the member set: a helm virtual with a helmoci member is the same mix.
// T-309 owns the check; the virtual implementation itself is T-313's.
func validateHelmFamilyMix(virtualPackageType string, memberRows []*metadata.Repo) error {
	var sawHelm, sawHelmOCI string
	check := func(packageType, owner string) {
		switch helmFamilyOf(packageType) {
		case PackageHelm:
			if sawHelm == "" {
				sawHelm = owner
			}
		case PackageHelmOCI:
			if sawHelmOCI == "" {
				sawHelmOCI = owner
			}
		}
	}
	if helmFamilyOf(virtualPackageType) != "" {
		check(virtualPackageType, "(the virtual repository itself)")
	}
	for _, row := range memberRows {
		check(row.PackageType, row.RepoKey)
	}
	if sawHelm != "" && sawHelmOCI != "" {
		return fmt.Errorf(
			"%w: virtual repository cannot mix the Helm and HelmOCI protocol families (helm repositories serve classic chart indexes, helmoci repositories serve the registry v2 plane); first helm member %q, first helmoci member %q",
			ErrInvalidRepoConfig, sawHelm, sawHelmOCI)
	}
	return nil
}

// validateV2MemberTypes enforces the registry-v2 same-type member rule
// (T-367's rider, closing the cross-ecosystem gap T-365's live pass pried
// open: a docker pull could resolve through a helmoci virtual because the
// /v2 plane is family-shared): a docker or helmoci virtual repository
// aggregates members of its OWN package type only — Artifactory's
// "virtual members must be of the virtual's package type" semantics on the
// one family where two package types share a wire plane. The refused
// wording follows the validateHelmFamilyMix shape. Non-v2 virtuals are out
// of the rider's scope (registered in the ticket report); the
// helm×helmoci mix keeps its own, earlier check.
func validateV2MemberTypes(virtualPackageType string, memberRows []*metadata.Repo) error {
	if !isV2PlaneFamily(virtualPackageType) {
		return nil
	}
	for _, row := range memberRows {
		if row.PackageType != virtualPackageType {
			return fmt.Errorf(
				"%w: virtual repository cannot mix the %s and %s package types (a %s virtual aggregates %s repositories only); member %q is a %s repository",
				ErrInvalidRepoConfig, virtualPackageType, row.PackageType,
				virtualPackageType, virtualPackageType, row.RepoKey, row.PackageType)
		}
	}
	return nil
}

// GetRepo implements Service.GetRepo. Remote rows get their config echoed
// through maskRemoteConfig — the canonical form never carries a password,
// but the read boundary enforces NFR-S14 against rows written by any other
// means too.
func (s *service) GetRepo(ctx context.Context, p *Principal, repoKey string) (*metadata.Repo, error) {
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	r, err := s.md.Repos().Get(ctx, repoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return nil, fmt.Errorf("repo %q: %w", repoKey, ErrRepoNotFound)
		}
		return nil, fmt.Errorf("repo %q: %w", repoKey, err)
	}
	if r.Type == TypeRemote {
		r.Config = maskRemoteConfig(r.Config)
	}
	return r, nil
}

// ListRepos implements Service.ListRepos.
func (s *service) ListRepos(ctx context.Context, p *Principal) ([]*metadata.Repo, error) {
	return s.ListReposFiltered(ctx, p, "", "")
}

// ListReposFiltered implements Service.ListReposFiltered (M04, FR-15-AC5):
// exact column matches, "" meaning "no filter on this axis", and unknown
// values matching nothing (rest-api.md section 2's empty-array-not-error
// contract). The repository count is small and the filtering is trivial;
// in-memory keeps one read path instead of a second query shape.
func (s *service) ListReposFiltered(ctx context.Context, p *Principal, repoType, packageType string) ([]*metadata.Repo, error) {
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	repos, err := s.md.Repos().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	out := make([]*metadata.Repo, 0, len(repos))
	for _, row := range repos {
		if repoType != "" && row.Type != repoType {
			continue
		}
		if packageType != "" && row.PackageType != packageType {
			continue
		}
		if row.Type == TypeRemote {
			row.Config = maskRemoteConfig(row.Config)
		}
		out = append(out, row)
	}
	return out, nil
}

// UpdateRepo implements Service.UpdateRepo: description and config only,
// type and package type are immutable (changing them would silently change
// every adapter routing decision). A PROVIDED config re-validates and
// rewrites the type-owned state (the remote_configs row / the virtual member
// list — full-replace semantics, the Artifactory PUT model); an absent
// config keeps it, so description-only updates never touch members or
// credentials.
func (s *service) UpdateRepo(ctx context.Context, p *Principal, r *metadata.Repo) (*metadata.Repo, error) {
	// T-217 (FR-65, ADR-0026 decision 3 / architecture section 7.1 family 7):
	// the single-repo configuration family's gate lives in httpapi — the
	// repoManage write route gate, which a manage holder passes through
	// Can(repo, "", m). The pre-M7 admin re-check here would veto exactly the
	// delegation that gate implements, so the service trusts it (authentication
	// is still demanded; ADR-0009).
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("%w: repository payload is nil", ErrInvalidRepoKey)
	}
	if err := guardSystemRepo(r.RepoKey, "reconfiguring it"); err != nil {
		return nil, err
	}
	if err := validateRepoKey(r.RepoKey); err != nil {
		return nil, err
	}
	current, err := s.md.Repos().Get(ctx, r.RepoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return nil, fmt.Errorf("repo %q: %w", r.RepoKey, ErrRepoNotFound)
		}
		return nil, fmt.Errorf("repo %q: %w", r.RepoKey, err)
	}
	if r.Type != "" && r.Type != current.Type {
		return nil, fmt.Errorf("%w: repository type is immutable (%q → %q)",
			ErrInvalidRepoType, current.Type, r.Type)
	}
	if r.PackageType != "" && r.PackageType != current.PackageType {
		return nil, fmt.Errorf("%w: package type is immutable (%q → %q)",
			ErrInvalidRepoType, current.PackageType, r.PackageType)
	}
	// M10 T-283 (D3's 改仓 half): the configuration write on a repository
	// whose (immutable) package type slot is locked refuses with the same
	// pointed clause as the create — description-only updates included, the
	// closed set's literal reading. The CONTENT of an existing repository
	// stays untouched by this (D1 owns the read plane; the write plane's
	// gate is httpapi's verb face).
	if err := s.validateRepoTypeDyn(ctx, p, current.RepoKey, current.Type, current.PackageType); err != nil {
		return nil, err
	}

	var (
		remote         *remoteConfig
		remotePassword string
		members        []string
		configSet      bool
		privateFrom    *bool
	)
	config := current.Config
	if r.Config != "" {
		configSet = true
		switch current.Type {
		case TypeLocal:
			if config, err = normalizeConfig(r.Config); err != nil {
				return nil, err
			}
			if err := validateLocalConfig(config); err != nil {
				return nil, err
			}
			if err := s.validateLocalKeypairRef(ctx, current.PackageType, config); err != nil {
				return nil, err
			}
		case TypeRemote:
			if err := rejectKeypairRefOnNonLocal(current.Type, r.Config); err != nil {
				return nil, err
			}
			rc, password, perr := parseRemoteConfig(r.Config, current.PackageType)
			if perr != nil {
				return nil, perr
			}
			remote = &rc
			remotePassword = password
			if config, err = marshalConfig(rc); err != nil {
				return nil, err
			}
			// The audit trail records the EXEMPTION'S transition, not just
			// its new value (NFR-S14 point 5: "变更留审计记录"). The old
			// value comes from the stored canonical form; a row that
			// somehow carries none counts as false.
			var old remoteConfig
			_ = json.Unmarshal([]byte(current.Config), &old)
			if old.AllowPrivateUpstream != rc.AllowPrivateUpstream {
				from := old.AllowPrivateUpstream
				privateFrom = &from
			}
		case TypeVirtual:
			if err := rejectKeypairRefOnNonLocal(current.Type, r.Config); err != nil {
				return nil, err
			}
			vc, perr := parseVirtualConfig(r.Config)
			if perr != nil {
				return nil, perr
			}
			if verr := s.validateVirtualMembers(ctx, p, current.RepoKey, current.PackageType, vc); verr != nil {
				return nil, verr
			}
			members = vc.Repositories
			if config, err = marshalConfig(vc); err != nil {
				return nil, err
			}
		}
	}
	current.Description = r.Description
	current.Config = config
	current.UpdatedAt = s.now()
	if err := s.md.Repos().Update(ctx, current); err != nil {
		return nil, fmt.Errorf("update repo %q: %w", r.RepoKey, err)
	}

	// Type-owned state, after the repositories row: the remote config row is
	// UPDATED in place (falling back to CreateConfig for a row lost to the
	// create-crash window — the update is the documented healer), the member
	// list replaced atomically by SetMembers.
	if remote != nil {
		row := &metadata.RemoteConfig{
			RepoKey: r.RepoKey,
			URL:     remote.URL, Username: remote.Username,
			// Full-replace semantics (the Artifactory PUT model, T-80's
			// ruling): the new body's password — sealed, or dropped with a
			// WARN when no master key is configured — replaces the stored
			// one; a body without a password clears it.
			Password:             s.sealPassword(ctx, r.RepoKey, remotePassword),
			ContentTTLSeconds:    remote.RetrievalCachePeriodSecs,
			MetadataTTLSeconds:   defaultMetadataTTLSeconds,
			AllowPrivateUpstream: remote.AllowPrivateUpstream,
			// T-290 (FR-90.2): same mirror contract as the create arm above.
			SocketTimeoutMs:              remote.SocketTimeoutMillis,
			MetadataRetrievalTimeoutSecs: remote.MetadataRetrievalTimeoutSecs,
			UnusedCleanupPeriodHours:     remote.UnusedCleanupPeriodHours,
		}
		if err := s.md.Remote().UpdateConfig(ctx, row); err != nil {
			if !errors.Is(err, metadata.ErrRemoteConfigNotFound) {
				return nil, fmt.Errorf("remote config %q: %w", r.RepoKey, err)
			}
			if err := s.md.Remote().CreateConfig(ctx, row); err != nil {
				return nil, fmt.Errorf("remote config %q: %w", r.RepoKey, err)
			}
		}
	}
	if members != nil {
		if err := s.md.Virtual().SetMembers(ctx, r.RepoKey, members); err != nil {
			return nil, fmt.Errorf("virtual members %q: %w", r.RepoKey, err)
		}
	}

	detail := fmt.Sprintf(`{"descriptionSet":%t,"configSet":%t}`, r.Description != "", configSet && config != "{}")
	if privateFrom != nil {
		detail = fmt.Sprintf(`{"descriptionSet":%t,"configSet":%t,"allowPrivateUpstream":{"from":%t,"to":%t}}`,
			r.Description != "", configSet && config != "{}", *privateFrom, remote.AllowPrivateUpstream)
	}
	s.audit(ctx, AuditEvent{
		Actor: p.Name, Action: AuditActionRepoUpdate, Repo: r.RepoKey,
		Detail: detail,
	})
	return current, nil
}

// sealPassword encrypts one upstream credential for at-rest storage
// (ADR-0012 decision 4, NFR-S14): with a master key configured the enc:v1
// AES-256-GCM form lands in the remote_configs row; without one the
// password is DROPPED with a WARN rather than stored unprotected — the
// plaintext window the T-62 review ordered closed stays closed, and the
// fetch then simply goes anonymous (every FR-15-AC9 scenario runs with the
// key present; the no-key create is a misconfiguration, not a contract).
func (s *service) sealPassword(ctx context.Context, repoKey, password string) string {
	if password == "" {
		return ""
	}
	if s.cipher == nil {
		slog.WarnContext(ctx, "repo: remote repository password dropped — no credentials master key configured",
			"repo", repoKey, "env", remote.CredentialsEnvVar)
		return ""
	}
	sealed, err := s.cipher.Encrypt(password)
	if err != nil {
		// A fresh random nonce and a validated key cannot fail here; if the
		// impossible happens, dropping is still the only safe answer.
		slog.WarnContext(ctx, "repo: remote repository password could not be sealed — dropped",
			"repo", repoKey, "error", err.Error())
		return ""
	}
	return sealed
}

// DeleteRepo implements Service.DeleteRepo. A non-empty repository without
// deleteContent fails with an error whose message names the flag
// (FR-3-AC5); with deleteContent every node goes first, then the row
// itself (the FK cascades nodes as a backstop; deleting them explicitly
// keeps the operation's blast radius observable). Docker repositories
// additionally count their manifest index as content and tear the three
// docker tables down before the row goes (FR-7-AC5).
func (s *service) DeleteRepo(ctx context.Context, p *Principal, repoKey string, deleteContent bool) error {
	// Family 6's other half stays double-doored ON PURPOSE (T-217): the route
	// gate is CapRepoWrite (admin-only, not delegated to m holders), and this
	// service-level backstop survives for the destructive extreme — every
	// production caller reaches DeleteRepo only through the gated httpapi
	// handler, so the second door is unobservable, pure defense in depth.
	if err := requireAdmin(p); err != nil {
		return err
	}
	if err := guardSystemRepo(repoKey, "deleting it"); err != nil {
		return err
	}
	repoRow, err := s.md.Repos().Get(ctx, repoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return fmt.Errorf("repo %q: %w", repoKey, ErrRepoNotFound)
		}
		return fmt.Errorf("repo %q: %w", repoKey, err)
	}
	// M10 T-283 (the ticket's 删仓同 ruling, D3's form): deleting a
	// repository whose package type slot is locked refuses with the same
	// clause — PRD 85.3's "数据与仓配置零删除" read literally (the breaker
	// must leave the configuration in place for the restart that restores
	// it). Reads of the repository's content are unaffected (D1).
	if err := s.validateRepoTypeDyn(ctx, p, repoRow.RepoKey, repoRow.Type, repoRow.PackageType); err != nil {
		return err
	}
	nodes, err := s.md.Nodes().ListByPrefix(ctx, repoKey, "")
	if err != nil {
		return fmt.Errorf("repo %q nodes: %w", repoKey, err)
	}
	// A registry-v2 family repository (docker or helmoci, HL-3) also "holds
	// content" through its manifest index — a repo whose nodes were pruned
	// but whose manifests remain (digest-only
	// pushes leave no folder rows, tags go under the image name) is not
	// empty either. Counting the catalog is the cheap sufficient probe.
	// (Tag rows cannot outlive their manifest: PutManifest writes the
	// manifest row before the tag, and the store's delete cascade removes
	// both in one transaction.)
	images := 0
	if isV2PlaneFamily(repoRow.PackageType) {
		rows, err := s.md.Docker().ListImages(ctx, repoKey, "", 0)
		if err != nil {
			return fmt.Errorf("repo %q docker catalog: %w", repoKey, err)
		}
		images = len(rows)
	}
	if len(nodes) > 0 && !deleteContent {
		return fmt.Errorf(
			"%w: %q holds %d node(s); retry with deleteContent=true to remove them",
			ErrRepoNotEmpty, repoKey, len(nodes))
	}
	if images > 0 && !deleteContent {
		return fmt.Errorf(
			"%w: %q holds %d docker image(s); retry with deleteContent=true to remove them",
			ErrRepoNotEmpty, repoKey, images)
	}
	if len(nodes) > 0 {
		if _, err := s.md.Nodes().DeleteByPrefix(ctx, repoKey, ""); err != nil {
			return fmt.Errorf("repo %q delete content: %w", repoKey, err)
		}
	}
	// Docker index teardown, first half (FR-7-AC5): manifests/tags per image.
	// This runs while the repositories row still exists, so a concurrent
	// publish racing this delete fails its node/manifest writes on the FK
	// the moment the row goes below.
	if _, err := s.DeleteRepoDocker(ctx, repoKey); err != nil {
		return fmt.Errorf("repo %q docker teardown: %w", repoKey, err)
	}
	// Remote repositories drop their cache-validator rows (FR-15-AC6):
	// remote_cache is DERIVED state, so unlike content nodes it never blocks
	// the delete — the purge runs on both the deleteContent and the
	// plain-empty path, and the count lands in the audit detail (the row FK
	// would cascade them anyway; the explicit call keeps the blast radius
	// observable, the same posture as the node deletes above). The
	// remote_configs row itself and virtual_members rows cascade through
	// their FKs (T-62's no-separate-teardown contract).
	cacheRows := int64(0)
	if repoRow.Type == TypeRemote {
		n, err := s.md.Remote().DeleteCacheByRepo(ctx, repoKey)
		if err != nil {
			return fmt.Errorf("repo %q remote cache teardown: %w", repoKey, err)
		}
		cacheRows = n
		// The engine's in-process state (outbound client pool, assumed
		// offline window) goes with the repository (T-66).
		if s.remoteEng != nil {
			s.remoteEng.Forget(repoKey)
		}
	}
	if err := s.md.Repos().Delete(ctx, repoKey); err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return fmt.Errorf("repo %q: %w", repoKey, ErrRepoNotFound)
		}
		return fmt.Errorf("delete repo %q: %w", repoKey, err)
	}
	// Second half, strictly AFTER the row is gone: docker_refs carries no
	// FK (architecture section 11.12), so its sweep must close the door the
	// FKs otherwise would — a concurrent PutManifest whose PutRefs landed
	// between the manifests/tags teardown and the repositories delete would
	// otherwise leave ref rows that no FK cascade can ever reach (permanent
	// GC pin + ghost references for a same-named recreate; T-35 review B1).
	// Running it last also means a failed repositories delete leaves refs
	// intact alongside the surviving row — a consistent, retryable state.
	// The sweep is a plain per-repo_key DELETE, so rows of a recreated
	// same-name repository cannot be hit: nothing can write through this
	// Service into that name until CreateRepo re-seeds it, which happens
	// strictly after this call returns.
	if _, err := s.md.Docker().DeleteRepoRefs(ctx, repoKey); err != nil {
		return fmt.Errorf("repo %q docker refs teardown: %w", repoKey, err)
	}
	detail := fmt.Sprintf(`{"deleteContent":%t,"removedNodes":%d}`, deleteContent, len(nodes))
	if repoRow.Type == TypeRemote {
		detail = fmt.Sprintf(`{"deleteContent":%t,"removedNodes":%d,"removedCacheRows":%d}`,
			deleteContent, len(nodes), cacheRows)
	}
	s.audit(ctx, AuditEvent{
		Actor: p.Name, Action: AuditActionRepoDelete, Repo: repoKey,
		Detail: detail,
	})
	return nil
}

// Usage implements Service.Usage (GE-06/W26b, FR-31): the repository's
// metered total against its configured ceiling, for the observability
// endpoint /api/v1/storage/usage/{repo}. The gate is the family-7 OR formula
// (architecture section 7.1, K11/T-214 P9): CanManageRepo(read) ∨ Can(r) —
// admin and readonly_admin pass through their role arms (the global read),
// a plain user passes with either a read grant OR the manage bit on the
// repository (a repo admin who may set quotaBytes may not be blind to the
// usage; before T-217 the manage arm was unreachable because no REST seam
// granted m). A principal with neither answers ErrForbidden.
func (s *service) Usage(ctx context.Context, p *Principal, repoKey string) (*UsageReport, error) {
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	row, err := s.loadRepoRow(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	if !s.allow(ctx, p, repoKey, "", ActionRead) && !s.allow(ctx, p, repoKey, "", ActionManage) {
		return nil, fmt.Errorf("read %s: %w", repoKey, ErrForbidden)
	}
	u, err := s.md.Usage().Get(ctx, repoKey)
	if err != nil {
		return nil, fmt.Errorf("usage %s: %w", repoKey, err)
	}
	quota := int64(0)
	if row.Type == TypeLocal {
		// quotaBytes is a local-repository field (the write plane's ceiling;
		// remote/virtual configs never carry it), but the probe is tolerant
		// for every class: a hand-seeded row answers its value, the rest 0.
		quota = parseGovernance(row.Config).quotaBytes
	}
	return &UsageReport{RepoKey: repoKey, UsedBytes: u.LogicalBytes, QuotaBytes: quota}, nil
}

// UsageBatch implements Service.UsageBatch (M9 E1, ADR-0030 / architecture
// section 14.1, FR-79.1): the set form of Usage behind
// GET /api/v1/storage/usage. One aggregate query feeds the whole response —
// the per-repo Usage().Get loop this replaces is the ~170-requests-per-page
// fan-out the endpoint exists to collapse. Visibility reuses Usage's exact
// family-7 OR decision per repository (allow(read) ∨ allow(m) at path ""),
// evaluated through the same allow seam, so a batch row can never appear
// where the single-repo endpoint would answer 403; the denied arm here is
// SILENT EXCLUSION rather than ErrForbidden because a set endpoint owes a
// filtered view to any legitimately authenticated caller (200 [] on an empty
// visibility set), while invisible repositories leak nothing — not their
// usage, not their existence.
func (s *service) UsageBatch(ctx context.Context, p *Principal, q UsageBatchQuery) ([]*UsageBatchReport, error) {
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	rows, err := s.md.Usage().List(ctx, q.IncludeCounts)
	if err != nil {
		return nil, fmt.Errorf("usage list: %w", err)
	}
	var named map[string]struct{}
	if q.Repos != nil {
		named = make(map[string]struct{}, len(q.Repos))
		for _, key := range q.Repos {
			named[key] = struct{}{}
		}
	}
	out := make([]*UsageBatchReport, 0, len(rows))
	for _, row := range rows {
		if named != nil {
			if _, ok := named[row.RepoKey]; !ok {
				continue
			}
		}
		// The family-7 OR gate, verbatim from Usage: read grant OR the
		// manage bit, admin/readonly_admin passing through the role arms
		// inside allow. Invisible rows are dropped before any projection.
		if !s.allow(ctx, p, row.RepoKey, "", ActionRead) && !s.allow(ctx, p, row.RepoKey, "", ActionManage) {
			continue
		}
		quota := int64(0)
		if row.Type == TypeLocal {
			// quotaBytes is a local-repository field (Usage's probe rule):
			// remote/virtual rows never carry a ceiling, hand-seeded local
			// rows answer whatever their config says.
			quota = parseGovernance(row.Config).quotaBytes
		}
		row2 := &UsageBatchReport{
			UsageReport: UsageReport{
				RepoKey:    row.RepoKey,
				UsedBytes:  row.UsedBytes,
				QuotaBytes: quota,
			},
		}
		if q.IncludeCounts {
			// The counts extras carry meaning only when asked for; a plain
			// query answers them zeroed (the handler then does not render
			// the fields at all — half-filled rows would be a lie two
			// layers deep).
			row2.NodeCount = row.NodeCount
			row2.UpdatedAt = row.UpdatedAt
		}
		out = append(out, row2)
	}
	return out, nil
}
