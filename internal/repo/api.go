package repo

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// Sentinel errors. Errors returned by Service wrap one of these; callers match
// with errors.Is and map to HTTP statuses at the httpapi layer.
var (
	// ErrRepoNotFound: the repository key does not exist (404).
	ErrRepoNotFound = errors.New("repository not found")
	// ErrRepoExists: a repository with the same key already exists (400/409).
	ErrRepoExists = errors.New("repository already exists")
	// ErrInvalidRepoKey: the key does not match [a-z][a-z0-9-]{1,62}
	// (PRD FR-3-AC4).
	ErrInvalidRepoKey = errors.New("invalid repository key")
	// ErrReservedRepoKey: the key collides with a routing segment reserved by
	// ADR-0008 ("api", "v2").
	ErrReservedRepoKey = errors.New("repository key is reserved")
	// ErrInvalidRepoType: unknown rclass, or an attempt to change it.
	ErrInvalidRepoType = errors.New("invalid repository type")
	// ErrRepoTypeNotSupported: remote/virtual repositories land in M3; M1
	// implements local only. httpapi translates this to a 400-shaped response.
	ErrRepoTypeNotSupported = errors.New("repository type not supported in M1")
	// ErrPackageTypeNotSupported: M1 repositories are generic only.
	ErrPackageTypeNotSupported = errors.New("package type not supported in M1")
	// ErrInvalidRepoConfig: the config blob is not valid JSON.
	ErrInvalidRepoConfig = errors.New("invalid repository config")
	// ErrRepoNotEmpty: DeleteRepo on a non-empty repository without
	// deleteContent (message names the deleteContent flag, FR-3-AC5).
	ErrRepoNotEmpty = errors.New("repository is not empty")
	// ErrNodeNotFound: no node at the path (idempotent 404 semantics,
	// repo-semantics section 4).
	ErrNodeNotFound = errors.New("node not found")
	// ErrIsFolder: Get addressed a folder node (trailing-slash path). The
	// node metadata is still returned; adapters use this to render
	// FolderInfo instead of streaming a body.
	ErrIsFolder = errors.New("node is a folder")
	// ErrOrphanBlob: PutFromBlob was handed a blob that exists physically
	// but has no blobs-ledger row (crash-window residue awaiting GC).
	// Materializing a node from it would freeze an incomplete digest record
	// (the ledger row is the sha1/md5 source of truth and is never
	// back-filled), so the operation refuses; the caller maps this to a
	// plain "content not found" (404), not a 500.
	ErrOrphanBlob = errors.New("blob exists in the filestore but not in the ledger")
	// ErrInvalidPath: malformed artifact path (empty segment, dot segment,
	// absolute path, too long, ...).
	ErrInvalidPath = errors.New("invalid artifact path")
	// ErrUnauthorized: anonymous principal where authentication is required
	// (401 challenge).
	ErrUnauthorized = errors.New("authentication required")
	// ErrForbidden: authenticated principal without the required grant (403).
	ErrForbidden = errors.New("permission denied")
)

// Repository types and package types (architecture section 6 DDL).
const (
	TypeLocal   = "local"
	TypeRemote  = "remote"
	TypeVirtual = "virtual"

	PackageGeneric = "generic"
)

// Reserved repo keys (ADR-0008): they collide with /binflow routing segments.
var reservedRepoKeys = map[string]bool{
	"api": true,
	"v2":  true,
}

// Authorization actions (architecture section 3.4). Aliased from auth so
// repo and auth can never drift apart on the action vocabulary.
const (
	ActionRead   = auth.ActionRead
	ActionWrite  = auth.ActionWrite
	ActionDelete = auth.ActionDelete
)

// Audit actions emitted by this package (architecture section 3.5),
// aliased from the audit package's vocabulary.
const (
	AuditActionDeploy     = audit.ActionDeploy
	AuditActionDownload   = audit.ActionDownload
	AuditActionDelete     = audit.ActionDelete
	AuditActionRepoCreate = audit.ActionRepoCreate
	AuditActionRepoUpdate = audit.ActionRepoUpdate
	AuditActionRepoDelete = audit.ActionRepoDelete
)

// Principal is the caller identity (architecture section 3.4). It is the
// auth package's own type, aliased here so both sides of the boundary share
// one declaration while the section 3.3 signatures stay readable. nil means
// anonymous (ADR-0009).
type Principal = auth.Principal

// Authorizer is the consumer-side ACL seam (architecture section 3.4). The
// auth package's implementation satisfies it structurally. nil inside New
// means "fail closed": only admin principals pass — the correct posture when
// no authorizer is wired.
type Authorizer interface {
	// Can reports whether p may perform action (r|w|d) on repoKey/path.
	// Anonymous policy (anonymous_access, ADR-0009) is the implementation's
	// decision, not this package's.
	Can(ctx context.Context, p *Principal, repoKey, path, action string) bool
}

// AuditEvent is one best-effort audit record (architecture section 3.5),
// aliased to the audit package's canonical type. Time and Actor are stamped
// by the injected logger when empty; this package fills Actor itself.
type AuditEvent = audit.Event

// AuditLogger is the consumer-side audit seam, satisfied structurally by
// audit.Logger. Append failures never block business operations (technical
// debt ledger #4); this package swallows and logs them so the guarantee
// holds regardless of the injected implementation.
type AuditLogger interface {
	Append(ctx context.Context, e AuditEvent) error
}

// Service orchestrates the repository use cases (architecture section 3.3):
// artifact Get/Put/Delete/List for local repositories plus repository CRUD.
// M1 implements the local branch only; content operations on remote/virtual
// rows yield ErrRepoTypeNotSupported.
//
// Put's transaction boundary is the correctness core: storage Commit puts the
// physical blob in place first, then the metadata writes land blob-first
// (blobs row, then node row). A crash in between leaves an unreferenced blob
// for GC's grace period — never metadata pointing at a missing blob
// (architecture sections 3.2/3.3; the nodes.sha256 FK enforces the order).
type Service interface {
	// Get opens the node's blob for reading (caller closes) and returns its
	// metadata. Missing node → ErrNodeNotFound; missing repo → ErrRepoNotFound.
	Get(ctx context.Context, p *Principal, repoKey, path string) (io.ReadSeekCloser, *metadata.Node, error)
	// Put streams body into a storage session, commits it against the
	// client-declared digests in expect and persists the node. Overwrite
	// semantics follow repo-semantics section 3: same declared sha256 on an
	// existing node is an idempotent retransmit (no delete-permission check,
	// created/createdBy preserved); a different checksum requires delete
	// permission on the old node. A path with a trailing slash creates a
	// folder node (empty body required).
	Put(ctx context.Context, p *Principal, repoKey, path string, body io.Reader, expect storage.BlobRef, mime string) (*metadata.Node, error)
	// PutFromBlob creates (or idempotently re-creates) a node pointing at an
	// already-committed blob — the checksum-deploy use case (X-Checksum-Deploy,
	// rest-api.md section 1.3), reached through the Service so adapters never
	// touch storage directly (architecture section 5.1's exception clause:
	// "extend repo.Service, do not bypass"). ref.Sha256 must address a blob
	// that exists in BOTH the filestore and the blobs ledger: an orphan blob
	// (physical file present, ledger row missing — the crash-window residue
	// GC eventually collects) yields ErrOrphanBlob instead of silently
	// materializing a node whose ancillary digests would be lost forever.
	// The ledger row is the digest source of truth: sha1/md5 are read from
	// it, never re-derived from the client's claim. Permission, overwrite
	// and idempotent-retransmit semantics are identical to Put.
	PutFromBlob(ctx context.Context, p *Principal, repoKey, path string, ref storage.BlobRef, mime string) (*metadata.Node, error)
	// Delete removes the node reference only (the blob is GC's business).
	// A directory path deletes recursively and prunes folder rows left empty.
	// Missing path → ErrNodeNotFound (idempotent 404 semantics).
	Delete(ctx context.Context, p *Principal, repoKey, path string) error
	// List returns every node under prefix ("" = whole repository), ordered
	// by path. Folder nodes (path ending in "/") are included. The prefix is
	// normalized: "d" and "d/" are equivalent and both return the folder row
	// plus everything beneath it (T-12 review B2).
	List(ctx context.Context, p *Principal, repoKey, prefix string) ([]*metadata.Node, error)

	// CreateRepo validates and persists a new repository configuration.
	// Admin only in M1 (repository management is an admin-plane operation).
	CreateRepo(ctx context.Context, p *Principal, r *metadata.Repo) (*metadata.Repo, error)
	// GetRepo returns one repository configuration (any authenticated
	// principal).
	GetRepo(ctx context.Context, p *Principal, repoKey string) (*metadata.Repo, error)
	// ListRepos returns every repository ordered by key.
	ListRepos(ctx context.Context, p *Principal) ([]*metadata.Repo, error)
	// UpdateRepo updates description/config; type and package type are
	// immutable. Admin only.
	UpdateRepo(ctx context.Context, p *Principal, r *metadata.Repo) (*metadata.Repo, error)
	// DeleteRepo removes the repository. Non-empty repositories require
	// deleteContent=true (ErrRepoNotEmpty names the flag otherwise); with it,
	// every node is removed first. Admin only.
	DeleteRepo(ctx context.Context, p *Principal, repoKey string, deleteContent bool) error
}

// New builds the Service from its collaborator contracts (architecture
// section 3.3). st and md are required; az may be nil (admin-only mode); au
// may be nil (auditing disabled).
func New(st storage.Engine, md metadata.Store, az Authorizer, au AuditLogger) Service {
	return NewWithClock(st, md, az, au, func() time.Time { return time.Now().UTC() })
}

// NewWithClock is New with an injected clock (RFC3339 UTC timestamps come
// from it). Exported for integration tests that must advance time
// deterministically.
func NewWithClock(st storage.Engine, md metadata.Store, az Authorizer, au AuditLogger, now func() time.Time) Service {
	return newService(st, md, az, au, now)
}
