package repo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// service is the M1 local-repository implementation of Service.
type service struct {
	st    storage.Engine
	md    metadata.Store
	az    Authorizer
	au    AuditLogger
	nowFn func() time.Time
}

// newService wires the collaborators; New is the public constructor with the
// default clock (tests inject a controllable one).
func newService(st storage.Engine, md metadata.Store, az Authorizer, au AuditLogger, now func() time.Time) *service {
	return &service{st: st, md: md, az: az, au: au, nowFn: now}
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

// loadLocalRepo resolves repoKey and asserts it is a local repository the
// service can operate on. Unknown keys map to ErrRepoNotFound so the
// metadata-layer sentinel never leaks to HTTP callers.
func (s *service) loadLocalRepo(ctx context.Context, repoKey string) (*metadata.Repo, error) {
	r, err := s.md.Repos().Get(ctx, repoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return nil, fmt.Errorf("repo %q: %w", repoKey, ErrRepoNotFound)
		}
		return nil, fmt.Errorf("repo %q: %w", repoKey, err)
	}
	if r.Type != TypeLocal {
		return nil, fmt.Errorf("%w: %s repositories are supported from M3", ErrRepoTypeNotSupported, r.Type)
	}
	return r, nil
}

// ---- Content use cases ----

// Get implements Service.Get. Addressing a folder node yields
// (nil, node, ErrIsFolder): folder rows carry metadata but no streamable
// body (their sha256 is the shared empty-marker sentinel).
func (s *service) Get(ctx context.Context, p *Principal, repoKey, path string) (io.ReadSeekCloser, *metadata.Node, error) {
	if err := validateNodePath(path); err != nil {
		return nil, nil, err
	}
	if _, err := s.loadLocalRepo(ctx, repoKey); err != nil {
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
	if _, err := s.loadLocalRepo(ctx, repoKey); err != nil {
		return nil, err
	}

	folder := isFolderNode(path)
	if !folder {
		// repo-semantics section 3: an existing node whose checksum equals
		// the client-declared sha256 is an idempotent retransmit — it must
		// succeed with neither the deploy nor the overwrite check.
		idempotent, existing, err := s.isIdempotentRedeploy(ctx, repoKey, path, expect.Sha256)
		if err != nil {
			return nil, err
		}
		if !idempotent {
			// Overwrite check FIRST (repo-semantics section 3): a different
			// checksum (or none declared) requires delete permission on the
			// old node, with the spec's wording. The plain write gate covers
			// brand-new paths.
			if existing != nil && !s.allow(ctx, p, repoKey, path, ActionDelete) {
				return nil, fmt.Errorf(
					"overwrite %s/%s: %w: user %q needs DELETE permission on the existing node",
					repoKey, path, ErrForbidden, p.Name)
			}
			if !s.allow(ctx, p, repoKey, path, ActionWrite) {
				return nil, fmt.Errorf("write %s/%s: %w", repoKey, path, ErrForbidden)
			}
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
func (s *service) Delete(ctx context.Context, p *Principal, repoKey, path string) error {
	if err := requireAuthenticated(p); err != nil {
		return err
	}
	if err := validateNodePath(path); err != nil {
		return err
	}
	if _, err := s.loadLocalRepo(ctx, repoKey); err != nil {
		return err
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

// ---- Repository CRUD ----

// CreateRepo implements Service.CreateRepo.
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
	config, err := normalizeConfig(r.Config)
	if err != nil {
		return nil, err
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
	s.audit(ctx, AuditEvent{
		Actor: p.Name, Action: AuditActionRepoCreate, Repo: r.RepoKey,
		Detail: fmt.Sprintf(`{"type":%q,"packageType":%q}`, r.Type, r.PackageType),
	})
	return stored, nil
}

// GetRepo implements Service.GetRepo.
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
	return r, nil
}

// ListRepos implements Service.ListRepos.
func (s *service) ListRepos(ctx context.Context, p *Principal) ([]*metadata.Repo, error) {
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	repos, err := s.md.Repos().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	return repos, nil
}

// UpdateRepo implements Service.UpdateRepo: description and config only,
// type and package type are immutable (changing them would silently change
// every adapter routing decision).
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
	config := current.Config
	if r.Config != "" {
		config, err = normalizeConfig(r.Config)
		if err != nil {
			return nil, err
		}
	}
	current.Description = r.Description
	current.Config = config
	current.UpdatedAt = s.now()
	if err := s.md.Repos().Update(ctx, current); err != nil {
		return nil, fmt.Errorf("update repo %q: %w", r.RepoKey, err)
	}
	s.audit(ctx, AuditEvent{
		Actor: p.Name, Action: AuditActionRepoUpdate, Repo: r.RepoKey,
		Detail: fmt.Sprintf(`{"descriptionSet":%t,"configSet":%t}`, r.Description != "", config != "{}"),
	})
	return current, nil
}

// DeleteRepo implements Service.DeleteRepo. A non-empty repository without
// deleteContent fails with an error whose message names the flag
// (FR-3-AC5); with deleteContent every node goes first, then the row
// itself (the FK cascades nodes as a backstop; deleting them explicitly
// keeps the operation's blast radius observable).
func (s *service) DeleteRepo(ctx context.Context, p *Principal, repoKey string, deleteContent bool) error {
	if err := requireAdmin(p); err != nil {
		return err
	}
	if _, err := s.md.Repos().Get(ctx, repoKey); err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return fmt.Errorf("repo %q: %w", repoKey, ErrRepoNotFound)
		}
		return fmt.Errorf("repo %q: %w", repoKey, err)
	}
	nodes, err := s.md.Nodes().ListByPrefix(ctx, repoKey, "")
	if err != nil {
		return fmt.Errorf("repo %q nodes: %w", repoKey, err)
	}
	if len(nodes) > 0 && !deleteContent {
		return fmt.Errorf(
			"%w: %q holds %d node(s); retry with deleteContent=true to remove them",
			ErrRepoNotEmpty, repoKey, len(nodes))
	}
	if len(nodes) > 0 {
		if _, err := s.md.Nodes().DeleteByPrefix(ctx, repoKey, ""); err != nil {
			return fmt.Errorf("repo %q delete content: %w", repoKey, err)
		}
	}
	if err := s.md.Repos().Delete(ctx, repoKey); err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return fmt.Errorf("repo %q: %w", repoKey, ErrRepoNotFound)
		}
		return fmt.Errorf("delete repo %q: %w", repoKey, err)
	}
	s.audit(ctx, AuditEvent{
		Actor: p.Name, Action: AuditActionRepoDelete, Repo: repoKey,
		Detail: fmt.Sprintf(`{"deleteContent":%t,"removedNodes":%d}`, deleteContent, len(nodes)),
	})
	return nil
}
