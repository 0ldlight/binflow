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
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/storage"
)

// service is the local + remote implementation of Service (virtual member
// resolution lands with T-71 and keeps the M2 refusal until then).
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
	return &service{st: st, md: md, az: az, au: au, nowFn: now, remoteEng: eng, cipher: eng.Cipher()}
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

// requireAuthenticated rejects anonymous principals for write-path
// operations (ADR-0009: writes are never anonymous, whatever the
// anonymous-access switch says).
func requireAuthenticated(p *Principal) error {
	if p == nil {
		return fmt.Errorf("write operation: %w", ErrUnauthorized)
	}
	return nil
}

// requireAdmin gates admin-plane operations (repository CRUD in M1).
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
// refuses them with RE-05's 405 — so only virtual keeps the interim refusal
// (the resolver lands with T-71).
func (s *service) loadLocalRepo(ctx context.Context, repoKey string) (*metadata.Repo, error) {
	r, err := s.loadRepoRow(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	if r.Type != TypeLocal {
		return nil, fmt.Errorf("%w: %s repositories are not served by the local content plane (the virtual resolver lands with T-71)",
			ErrRepoTypeNotSupported, r.Type)
	}
	return r, nil
}

// refuseNonLocalWrite answers the write plane's refusal for non-local
// classes BEFORE any body is drained or permission pair is evaluated: the
// method itself is invalid on these targets, whatever the caller's grants.
// remote is read-only — 405 + Allow: GET (RE-05, FR-20-AC9) — and virtual
// keeps the interim refusal until T-71's write routing.
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
		return fmt.Errorf("%w: %s repositories are not served by the local content plane (the virtual resolver lands with T-71)",
			ErrRepoTypeNotSupported, row.Type)
	}
	return nil
}

// loadLocalDockerRepo resolves repoKey and asserts it is a local DOCKER
// repository — the docker use cases refuse to serve any other package type,
// even another local one (the /v2 plane would otherwise index generic
// content under a registry name).
func (s *service) loadLocalDockerRepo(ctx context.Context, repoKey string) (*metadata.Repo, error) {
	r, err := s.loadLocalRepo(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	if r.PackageType != PackageDocker {
		return nil, fmt.Errorf("repo %q: %w: package type is %q, not %q",
			repoKey, ErrRepoTypeNotSupported, r.PackageType, PackageDocker)
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
	if row.Type == TypeRemote {
		return s.getRemote(ctx, p, repoKey, path)
	}
	if row.Type != TypeLocal {
		return nil, nil, fmt.Errorf("%w: %s repositories are not served by the local content plane (the virtual resolver lands with T-71)",
			ErrRepoTypeNotSupported, row.Type)
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
	s.audit(ctx, AuditEvent{Actor: actor(p), Action: AuditActionDownload, Repo: repoKey, Path: path})
	return rc, n, nil
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
	s.audit(ctx, AuditEvent{Actor: actor(p), Action: AuditActionDownload, Repo: repoKey, Path: path})
	return res.Body, res.Node, nil
}

// Put implements Service.Put. Ordering is the correctness core (architecture
// sections 3.2/3.3): the physical blob commits FIRST, then the metadata
// writes land blob-first (blobs row, then node row) so the
// nodes.sha256 → blobs.sha256 FK backs the invariant. A failure after Commit
// leaves an unreferenced blob — GC's grace period is the designed recovery
// path — and never a node without its blob.
func (s *service) Put(ctx context.Context, p *Principal, repoKey, path string, body io.Reader, expect storage.BlobRef, mime string) (*metadata.Node, error) {
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	if err := validateNodePath(path); err != nil {
		return nil, err
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	if row, err := s.loadRepoRow(ctx, repoKey); err != nil {
		return nil, err
	} else if err := refuseNonLocalWrite(row); err != nil {
		return nil, err
	}

	folder := isFolderNode(path)
	if !folder {
		// The permission pair (repo-semantics section 3) runs before the
		// body is committed: an existing node whose checksum equals the
		// client-declared sha256 is an idempotent retransmit — it succeeds
		// with neither the deploy nor the overwrite check; a different
		// checksum requires delete permission on the old node.
		idempotent, err := s.authorizeContentPut(ctx, p, repoKey, path, expect.Sha256)
		if err != nil {
			return nil, err
		}
		committed, err := s.commitBlob(ctx, body, expect)
		if err != nil {
			return nil, err
		}
		n, err := s.putNode(ctx, p, repoKey, path, false, committed, mime)
		if err != nil {
			return nil, err
		}
		s.audit(ctx, AuditEvent{
			Actor: p.Name, Action: AuditActionDeploy, Repo: repoKey, Path: path,
			Detail: fmt.Sprintf(`{"sha256":%q,"size":%d,"idempotent":%t}`, n.Sha256, n.Size, idempotent),
		})
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
	n, err := s.putNode(ctx, p, repoKey, path, true, storage.BlobRef{}, mime)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, AuditEvent{
		Actor: p.Name, Action: AuditActionDeploy, Repo: repoKey, Path: path,
		Detail: `{"folder":true}`,
	})
	return n, nil
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
// then verified in BOTH dimensions before any metadata write:
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
func (s *service) PutFromBlob(ctx context.Context, p *Principal, repoKey, path string, ref storage.BlobRef, mime string) (*metadata.Node, error) {
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	if err := validateNodePath(path); err != nil {
		return nil, err
	}
	if isFolderNode(path) {
		return nil, fmt.Errorf("folder deploy %s/%s: %w: checksum deploy targets files only", repoKey, path, ErrInvalidPath)
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	if row, err := s.loadRepoRow(ctx, repoKey); err != nil {
		return nil, err
	} else if err := refuseNonLocalWrite(row); err != nil {
		return nil, err
	}

	// The same permission pair as Put, keyed on the client-declared sha256
	// (which PutFromBlob requires to be set — it IS the addressing key here).
	if ref.Sha256 == "" {
		return nil, fmt.Errorf("checksum deploy %s/%s: %w: sha256 is required", repoKey, path, ErrInvalidPath)
	}
	idempotent, err := s.authorizeContentPut(ctx, p, repoKey, path, ref.Sha256)
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

	n, err := s.putNode(ctx, p, repoKey, path, false, committed, mime)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, AuditEvent{
		Actor: p.Name, Action: AuditActionDeploy, Repo: repoKey, Path: path,
		Detail: fmt.Sprintf(`{"sha256":%q,"size":%d,"idempotent":%t,"checksumDeployed":true}`, n.Sha256, n.Size, idempotent),
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
	if isFolderNode(path) {
		return nil, fmt.Errorf("landed blob %s/%s: %w: folder paths have no blob of their own", repoKey, path, ErrInvalidPath)
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	if row, err := s.loadRepoRow(ctx, repoKey); err != nil {
		return nil, err
	} else if err := refuseNonLocalWrite(row); err != nil {
		return nil, err
	}
	if ref.Sha256 == "" {
		return nil, fmt.Errorf("landed blob %s/%s: %w: sha256 is required", repoKey, path, ErrInvalidPath)
	}
	idempotent, err := s.authorizeContentPut(ctx, p, repoKey, path, ref.Sha256)
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

	committed := storage.BlobRef{Sha256: ref.Sha256, Sha1: ref.Sha1, Md5: ref.Md5, Size: phys.Size}
	n, err := s.putNode(ctx, p, repoKey, path, false, committed, mime)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, AuditEvent{
		Actor: p.Name, Action: AuditActionDeploy, Repo: repoKey, Path: path,
		Detail: fmt.Sprintf(`{"sha256":%q,"size":%d,"idempotent":%t,"landedBlob":true}`, n.Sha256, n.Size, idempotent),
	})
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
// delete on it, and every landing requires write. The pair lives in one
// place so Put, PutFromBlob and PutLandedBlob can never drift apart on the
// ordering — the security-relevant part is that the gates run before the
// body is drained or the blob is opened.
func (s *service) authorizeContentPut(ctx context.Context, p *Principal, repoKey, path, declaredSha256 string) (idempotent bool, err error) {
	idempotent, existing, err := s.isIdempotentRedeploy(ctx, repoKey, path, declaredSha256)
	if err != nil {
		return false, err
	}
	if idempotent {
		return true, nil
	}
	if existing != nil && !s.allow(ctx, p, repoKey, path, ActionDelete) {
		return false, fmt.Errorf(
			"overwrite %s/%s: %w: user %q needs DELETE permission on the existing node",
			repoKey, path, ErrForbidden, p.Name)
	}
	if !s.allow(ctx, p, repoKey, path, ActionWrite) {
		return false, fmt.Errorf("write %s/%s: %w", repoKey, path, ErrForbidden)
	}
	return false, nil
}

// putNode persists blob + node in the mandated order (architecture sections
// 3.2/3.3): the blobs row first, then the node row that references it; the
// nodes.sha256 FK is the crash backstop. Both writes happen only after the
// physical blob exists.
//
// Folder nodes (path with trailing slash) have no content of their own. The
// emptyFolderSHA sentinel satisfies the NOT NULL + FK pair on nodes.sha256
// with a dedicated blobs row — folder paths across every parent level share
// it (marker semantics, not content: two folders with the same name never
// collide, their nodes rows stay keyed by (repo_key, path)).
const emptyFolderSHA = "0000000000000000000000000000000000000000000000000000000000000000"

// ensureFolderLedger writes the shared empty-folder blob row. It runs before
// any folder node write for the same blob-first reason as content uploads.
func (s *service) ensureFolderLedger(ctx context.Context) error {
	return s.md.Blobs().Put(ctx, &metadata.Blob{
		Sha256: emptyFolderSHA, Size: 0, CreatedAt: s.now(),
	})
}

func (s *service) putNode(ctx context.Context, p *Principal, repoKey, path string, folder bool, ref storage.BlobRef, mime string) (*metadata.Node, error) {
	sha256 := ref.Sha256
	size := ref.Size
	if folder {
		sha256 = emptyFolderSHA
		size = 0
	}

	existing, err := s.md.Nodes().Get(ctx, repoKey, path)
	switch {
	case err == nil:
		if existing.Sha256 == sha256 {
			// Same content (or the same folder marker): refresh the
			// modified-side fields, keep created/createdBy (repo-semantics
			// section 3).
			existing.Mime = mime
			existing.UpdatedAt = s.now()
			if err := s.md.Nodes().Put(ctx, existing); err != nil {
				return nil, fmt.Errorf("idempotent redeploy %s/%s: %w", repoKey, path, err)
			}
			return existing, nil
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
	if folder {
		if err := s.ensureFolderLedger(ctx); err != nil {
			return nil, fmt.Errorf("folder ledger row: %w", err)
		}
	} else {
		if err := s.md.Blobs().Put(ctx, &metadata.Blob{
			Sha256: ref.Sha256, Sha1: ref.Sha1, Md5: ref.Md5, Size: ref.Size, CreatedAt: s.now(),
		}); err != nil {
			return nil, fmt.Errorf("blob row %s: %w", ref.Sha256, err)
		}
	}

	now := s.now()
	n := &metadata.Node{
		RepoKey: repoKey, Path: path, Sha256: sha256, Size: size, Mime: mime,
		CreatedBy: p.Name, CreatedAt: now, UpdatedAt: now,
	}
	if existing != nil {
		// Overwrite keeps the first deployment's created/createdBy and
		// updates the modified-side fields (repo-semantics section 3).
		n.CreatedBy = existing.CreatedBy
		n.CreatedAt = existing.CreatedAt
	}
	if err := s.md.Nodes().Put(ctx, n); err != nil {
		return nil, fmt.Errorf("node %s/%s: %w", repoKey, path, err)
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
func (s *service) Delete(ctx context.Context, p *Principal, repoKey, path string) error {
	if err := requireAuthenticated(p); err != nil {
		return err
	}
	if err := validateNodePath(path); err != nil {
		return err
	}
	row, err := s.loadRepoRow(ctx, repoKey)
	if err != nil {
		return err
	}
	if row.Type == TypeRemote {
		return s.deleteRemoteCache(ctx, p, repoKey, path)
	}
	if row.Type != TypeLocal {
		return fmt.Errorf("%w: %s repositories are not served by the local content plane (the virtual resolver lands with T-71)",
			ErrRepoTypeNotSupported, row.Type)
	}
	if !s.allow(ctx, p, repoKey, path, ActionDelete) {
		return fmt.Errorf("delete %s/%s: %w", repoKey, path, ErrForbidden)
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
			if err := nodes.Delete(ctx, repoKey, v.Path); err != nil {
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
			Detail: fmt.Sprintf(`{"removed":%d}`, removed),
		})
		return nil
	}

	if _, err := nodes.Get(ctx, repoKey, path); err != nil {
		if errors.Is(err, metadata.ErrNodeNotFound) {
			return fmt.Errorf("node %s/%s: %w", repoKey, path, ErrNodeNotFound)
		}
		return fmt.Errorf("node %s/%s: %w", repoKey, path, err)
	}
	if err := nodes.Delete(ctx, repoKey, path); err != nil {
		return fmt.Errorf("delete node %s/%s: %w", repoKey, path, err)
	}
	// Deleting a leaf may empty its parent folders; prune them so listings
	// show directories that actually contain something.
	if err := s.pruneEmptyParents(ctx, repoKey, path); err != nil {
		return err
	}
	s.audit(ctx, AuditEvent{Actor: p.Name, Action: AuditActionDelete, Repo: repoKey, Path: path})
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
	if _, err := s.loadLocalRepo(ctx, repoKey); err != nil {
		return nil, err
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
	if _, err := s.loadLocalDockerRepo(ctx, repoKey); err != nil {
		return nil, err
	}

	permPath := dockerPermPath(image)
	nodePath := dockerImageManifestPath(image, digest)

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
		storage.BlobRef{Sha256: digest, Size: size}, mediaType)
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
	if _, err := s.loadLocalDockerRepo(ctx, repoKey); err != nil {
		return nil, err
	}
	if !s.allow(ctx, p, repoKey, dockerPermPath(image), ActionRead) {
		if p == nil {
			return nil, fmt.Errorf("read %s/%s: %w", repoKey, image, ErrUnauthorized)
		}
		return nil, fmt.Errorf("read %s/%s: %w", repoKey, image, ErrForbidden)
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
	if _, err := s.loadLocalDockerRepo(ctx, repoKey); err != nil {
		return nil, err
	}
	if !s.allow(ctx, p, repoKey, dockerPermPath(image), ActionRead) {
		if p == nil {
			return nil, fmt.Errorf("read %s/%s: %w", repoKey, image, ErrUnauthorized)
		}
		return nil, fmt.Errorf("read %s/%s: %w", repoKey, image, ErrForbidden)
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
	if _, err := s.loadLocalDockerRepo(ctx, repoKey); err != nil {
		return nil, err
	}
	if !s.allow(ctx, p, repoKey, dockerPermPath(image), ActionRead) {
		if p == nil {
			return nil, fmt.Errorf("read %s/%s: %w", repoKey, image, ErrUnauthorized)
		}
		return nil, fmt.Errorf("read %s/%s: %w", repoKey, image, ErrForbidden)
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
	if _, err := s.loadLocalDockerRepo(ctx, repoKey); err != nil {
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
	if _, err := s.md.Docker().GetManifest(ctx, repoKey, image, digest); err != nil {
		if !errors.Is(err, metadata.ErrManifestNotFound) {
			return fmt.Errorf("manifest %s/%s@%s: %w", repoKey, image, digest, err)
		}
		nodePath := dockerImageManifestPath(image, digest)
		if n, nerr := s.md.Nodes().Get(ctx, repoKey, nodePath); nerr == nil && n.Sha256 == digest {
			if err := s.md.Nodes().Delete(ctx, repoKey, nodePath); err != nil && !errors.Is(err, metadata.ErrNodeNotFound) {
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
	if err := s.md.Nodes().Delete(ctx, repoKey, nodePath); err != nil {
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
	if err := requireAdmin(p); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("%w: repository payload is nil", ErrInvalidRepoKey)
	}
	if err := validateRepoKey(r.RepoKey); err != nil {
		return nil, err
	}
	if err := validateRepoType(r.Type, r.PackageType); err != nil {
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
	case TypeRemote:
		rc, password, perr := parseRemoteConfig(config)
		if perr != nil {
			return nil, perr
		}
		remote = &rc
		remotePassword = password
		if config, err = marshalConfig(rc); err != nil {
			return nil, err
		}
	case TypeVirtual:
		vc, perr := parseVirtualConfig(config)
		if perr != nil {
			return nil, perr
		}
		if verr := s.validateVirtualMembers(ctx, r.RepoKey, vc); verr != nil {
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
func (s *service) validateVirtualMembers(ctx context.Context, virtualKey string, cfg virtualConfig) error {
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
	for _, m := range cfg.Repositories {
		row, err := s.md.Repos().Get(ctx, m)
		if err != nil {
			if errors.Is(err, metadata.ErrRepoNotFound) {
				return fmt.Errorf("%w: virtual repository member %q does not exist", ErrInvalidRepoConfig, m)
			}
			return fmt.Errorf("virtual member %q: %w", m, err)
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
	}
	if cfg.DefaultDeploymentRepo != "" && !seen[cfg.DefaultDeploymentRepo] {
		return fmt.Errorf(
			"%w: defaultDeploymentRepo %q is not a member of the virtual repository",
			ErrInvalidRepoConfig, cfg.DefaultDeploymentRepo)
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
	if err := requireAdmin(p); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("%w: repository payload is nil", ErrInvalidRepoKey)
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
		case TypeRemote:
			rc, password, perr := parseRemoteConfig(r.Config)
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
			vc, perr := parseVirtualConfig(r.Config)
			if perr != nil {
				return nil, perr
			}
			if verr := s.validateVirtualMembers(ctx, current.RepoKey, vc); verr != nil {
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
	if err := requireAdmin(p); err != nil {
		return err
	}
	repoRow, err := s.md.Repos().Get(ctx, repoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return fmt.Errorf("repo %q: %w", repoKey, ErrRepoNotFound)
		}
		return fmt.Errorf("repo %q: %w", repoKey, err)
	}
	nodes, err := s.md.Nodes().ListByPrefix(ctx, repoKey, "")
	if err != nil {
		return fmt.Errorf("repo %q nodes: %w", repoKey, err)
	}
	// A docker repository also "holds content" through its manifest index —
	// a repo whose nodes were pruned but whose manifests remain (digest-only
	// pushes leave no folder rows, tags go under the image name) is not
	// empty either. Counting the catalog is the cheap sufficient probe.
	// (Tag rows cannot outlive their manifest: PutManifest writes the
	// manifest row before the tag, and the store's delete cascade removes
	// both in one transaction.)
	images := 0
	if repoRow.PackageType == PackageDocker {
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
