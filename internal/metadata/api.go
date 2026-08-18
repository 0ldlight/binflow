package metadata

import (
	"context"
	"errors"
)

// ErrNotFound is returned by Get-style methods when the requested row does not
// exist. Wrapping is allowed as long as errors.Is keeps working.
var ErrNotFound = errors.New("metadata: not found")

// ErrNodeNotFound is returned by NodeStore.Get/Delete for missing nodes.
var ErrNodeNotFound = errors.New("metadata: node not found")

// ErrRepoNotFound is returned by RepoStore methods for missing repositories.
var ErrRepoNotFound = errors.New("metadata: repository not found")

// ErrUserNotFound is returned by UserStore methods for missing users.
var ErrUserNotFound = errors.New("metadata: user not found")

// ErrTokenNotFound is returned by TokenStore methods for missing tokens.
var ErrTokenNotFound = errors.New("metadata: token not found")

// ErrDuplicate is reserved for callers that want a typed conflict signal;
// the M1 stores surface driver constraint errors wrapped (the repository
// layer maps them to HTTP 400/409).
var ErrDuplicate = errors.New("metadata: duplicate")

// ErrManifestNotFound is returned by DockerStore methods for missing
// docker_manifests rows.
var ErrManifestNotFound = errors.New("metadata: docker manifest not found")

// ErrTagNotFound is returned by DockerStore methods for missing docker_tags
// rows.
var ErrTagNotFound = errors.New("metadata: docker tag not found")

// Node is one artifact row (a repo-relative path pointing at a blob).
// Timestamps are RFC3339 UTC text (ADR-0007).
type Node struct {
	RepoKey   string
	Path      string // repo-relative, '/' separated, no leading '/'
	Sha256    string // -> blobs.sha256
	Size      int64
	Mime      string
	CreatedBy string // principal name
	CreatedAt string // RFC3339 UTC
	UpdatedAt string // RFC3339 UTC
}

// Repo is one repository configuration row.
type Repo struct {
	RepoKey     string
	Type        string // local | remote | virtual
	PackageType string // generic | docker | maven | npm | pypi
	Description string
	Config      string // JSON blob of type-specific config
	CreatedAt   string
	UpdatedAt   string
}

// Blob is one physical content-addressed object reference.
type Blob struct {
	Sha256    string // hex lowercase
	Sha1      string
	Md5       string
	Size      int64
	CreatedAt string
}

// User is one local account.
type User struct {
	Username     string
	PasswordHash string // argon2id PHC string; never round-trips through API responses
	IsAdmin      bool
	Enabled      bool
	CreatedAt    string
	UpdatedAt    string
}

// Token stores only sha256(plaintext); the plaintext is shown once at issue
// time (NFR-S2).
type Token struct {
	ID          int64
	Username    string
	TokenSHA256 string
	ExpiresAt   string // RFC3339; '9999-12-31T00:00:00Z' means never expires
	CreatedAt   string
	LastUsedAt  string // '' when never used
}

// PermissionTarget is a named permission target (PRD E-24): a set of repos,
// include/exclude path patterns and the principals bound to it.
type PermissionTarget struct {
	Name      string
	Repos     string // JSON array of repo keys
	Includes  string // JSON array of include patterns
	Excludes  string // JSON array of exclude patterns
	CreatedAt string
	UpdatedAt string
}

// PermissionPrincipal is one (target, user) grant row. Actions are the
// read/write/delete booleans; write covers upload but not delete.
type PermissionPrincipal struct {
	ID            int64
	TargetName    string
	Principal     string
	PrincipalType string // "user" (groups [M4])
	CanRead       bool
	CanWrite      bool
	CanDelete     bool
}

// AuditEvent is one append-only audit record (architecture section 3.5).
type AuditEvent struct {
	ID      int64
	Time    string
	Actor   string
	Action  string // deploy|delete|download|login.success|login.failed|repo.create|...
	RepoKey string
	Path    string
	Detail  string // JSON
}

// DockerManifest is one row of the manifest metadata table (002_docker). The
// manifest body itself is a plain blob plus a node in the docker layout
// ("<image>/manifests/<digest-hex>"); this row is the registry index.
type DockerManifest struct {
	RepoKey   string
	Image     string // repository-relative name, no repo key first segment, may contain '/'
	Digest    string // bare hex sha256, same keyspace as Blob.Sha256
	MediaType string
	Size      int64
	CreatedBy string
	CreatedAt string // RFC3339 UTC
}

// DockerTag is a tag-to-digest pointer. Tags are mutable: repointing is an
// update, not a conflict. Digests are logical references to
// DockerManifest.Digest — there is no DB-level FK (architecture 11.12).
type DockerTag struct {
	RepoKey   string
	Image     string
	Tag       string // charset [a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}, validated by the service layer
	Digest    string
	UpdatedBy string
	UpdatedAt string // RFC3339 UTC
}

// DockerRef is one (manifest, blob) reference edge: config and layer blobs a
// manifest uses. It is the second source of GC reference facts: blobs listed
// here must never be reclaimed even when no node row points at them.
type DockerRef struct {
	RepoKey        string
	Image          string
	ManifestDigest string // referencing side
	BlobDigest     string // referenced side
	ChildMediaType string // role: config vs layer media type, '' allowed
}

// Store is the dialect-neutral entry to all metadata state. Migrations run
// automatically inside Open (ADR-0007); they are not part of this interface.
// Implementations must be safe for concurrent use (SQLite relies on WAL plus
// database/sql pool serialization, see store.go).
type Store interface {
	// Repos/Nodes/Blobs/Users/Tokens/Permissions/Audits/Docker return the
	// sub-stores sharing the same underlying handle.
	Repos() RepoStore
	Nodes() NodeStore
	Blobs() BlobStore
	Users() UserStore
	Tokens() TokenStore
	Permissions() PermissionStore
	Audits() AuditStore
	Docker() DockerStore
	// Ping verifies liveness for health endpoints.
	Ping(ctx context.Context) error
	// Close releases the underlying handle.
	Close() error
}

// RepoStore is repository configuration CRUD.
type RepoStore interface {
	Create(ctx context.Context, r *Repo) error
	Update(ctx context.Context, r *Repo) error // upsert-by-key semantics on non-key fields
	Get(ctx context.Context, repoKey string) (*Repo, error)
	Delete(ctx context.Context, repoKey string) error // cascades nodes via FK
	List(ctx context.Context) ([]*Repo, error)
}

// NodeStore is artifact path metadata. Put is an upsert by (repo_key, path).
type NodeStore interface {
	// Get returns ErrNodeNotFound when absent.
	Get(ctx context.Context, repoKey, path string) (*Node, error)
	Put(ctx context.Context, n *Node) error
	Delete(ctx context.Context, repoKey, path string) error // drops the reference only, never the blob
	// DeleteByPrefix removes every node under repoKey/prefix (directory
	// recursion and repository teardown); returns the number of rows removed.
	DeleteByPrefix(ctx context.Context, repoKey, prefix string) (int64, error)
	// ListByPrefix returns nodes under repoKey whose path starts with prefix,
	// ordered by path.
	ListByPrefix(ctx context.Context, repoKey, prefix string) ([]*Node, error)
}

// BlobStore is the "ever existed" ledger of physical objects (ADR-0006: rows
// outlive node references so the GC can look up secondary checksums).
type BlobStore interface {
	// Put inserts or refreshes a blob row (caller commits the physical blob
	// first; see architecture 3.3 for the ordering invariant).
	Put(ctx context.Context, b *Blob) error
	Get(ctx context.Context, sha256 string) (*Blob, error)
	// Delete removes the row; only the GC calls this after sweep.
	Delete(ctx context.Context, sha256 string) error
	// Count returns the total number of blob rows.
	Count(ctx context.Context) (int64, error)
	// FilterUnreferenced streams sha256 values of blobs not referenced by any
	// node, oldest first, pageSize rows per query (never the whole table).
	// Pages are keyed on (created_at, sha256) instead of OFFSET so rows the
	// callback deletes between pages are not skipped.
	FilterUnreferenced(ctx context.Context, pageSize int, fn func(sha256 string) error) error
}

// UserStore is local account CRUD plus the two lookups auth needs.
type UserStore interface {
	Create(ctx context.Context, u *User) error
	Get(ctx context.Context, username string) (*User, error)
	GetByPasswordHash(ctx context.Context, passwordHash string) (*User, error)
	UpdatePassword(ctx context.Context, username, passwordHash string) error
	Delete(ctx context.Context, username string) error
	List(ctx context.Context) ([]*User, error)
}

// TokenStore is API token persistence; sha256(plaintext) is the only stored
// form of the secret.
type TokenStore interface {
	Create(ctx context.Context, t *Token) (int64, error) // returns the new id
	Get(ctx context.Context, id int64) (*Token, error)
	GetBySHA256(ctx context.Context, sha256 string) (*Token, error)
	Touch(ctx context.Context, id int64, lastUsedAt string) error
	Delete(ctx context.Context, id int64) error
	ListByUsername(ctx context.Context, username string) ([]*Token, error)
}

// PermissionStore manages named permission targets and their principals
// (PRD E-24; the target-replace operation is one transaction).
type PermissionStore interface {
	// PutTarget creates or wholly replaces target t and its principals.
	PutTarget(ctx context.Context, t *PermissionTarget, principals []*PermissionPrincipal) error
	GetTarget(ctx context.Context, name string) (*PermissionTarget, []*PermissionPrincipal, error)
	DeleteTarget(ctx context.Context, name string) error
	ListTargets(ctx context.Context) ([]*PermissionTarget, error)
	// PrincipalsFor returns every principal row of every target whose repos
	// JSON array lists repoKey (auth evaluates patterns in memory).
	PrincipalsFor(ctx context.Context, repoKey string) ([]*PermissionPrincipal, error)
}

// AuditStore appends and queries audit events. Append is best-effort upstream
// (architecture 3.5); the store itself reports errors honestly.
type AuditStore interface {
	Append(ctx context.Context, e *AuditEvent) error
	// List returns events newest-first, at most limit rows, optionally
	// filtered by repo and actor.
	List(ctx context.Context, repoKey, actor string, limit int) ([]*AuditEvent, error)
}

// DockerStore is the registry index behind the docker adapter /v2 surface
// (architecture 5.3 mapping table, schema 002_docker). The manifest body and
// its blobs remain ordinary blobs/nodes; this store tracks only the
// manifests/tags/refs index rows and the repository-level _catalog facts.
//
// Callers that must mutate several tables atomically (manifest delete with
// its cascade, a full image teardown) use DeleteImage and DeleteManifest —
// multi-statement operations are single transactions inside this store. The
// fine-grained methods trade atomicity for composability and are meant for
// single-row operations.
type DockerStore interface {
	// PutManifest upserts by (repo_key, image, digest): re-pushing the same
	// manifest refreshes metadata columns and never conflicts.
	PutManifest(ctx context.Context, m *DockerManifest) error
	// GetManifest returns ErrManifestNotFound when absent.
	GetManifest(ctx context.Context, repoKey, image, digest string) (*DockerManifest, error)
	// DeleteManifest removes the manifest row and cascades: every tag
	// pointing at it and every ref it issued is dropped in the same
	// transaction (architecture 11.12 consistency contract). Returns
	// ErrManifestNotFound when the manifest row is absent; the cascade is
	// idempotent when rows are already gone.
	DeleteManifest(ctx context.Context, repoKey, image, digest string) error
	// ListManifestsByImage returns manifests of one image ordered by digest.
	ListManifestsByImage(ctx context.Context, repoKey, image string) ([]*DockerManifest, error)

	// PutTag upserts by (repo_key, image, tag): re-pushing a tag repoints it
	// at the new digest (updated_by/updated_at refresh).
	PutTag(ctx context.Context, t *DockerTag) error
	// GetTag returns ErrTagNotFound when absent.
	GetTag(ctx context.Context, repoKey, image, tag string) (*DockerTag, error)
	// DeleteTag removes one tag row; ErrTagNotFound when absent.
	DeleteTag(ctx context.Context, repoKey, image, tag string) error
	// ListTagsByImage returns the image's tags ordered by tag.
	ListTagsByImage(ctx context.Context, repoKey, image string) ([]*DockerTag, error)

	// PutRefs inserts the ref rows of one manifest, replacing any previous
	// set for that (repo_key, image, manifest_digest) atomically (a manifest
	// body is immutable, so re-push writes the same edges; replace keeps the
	// ledger exact without diffing). An empty refs slice clears the set.
	PutRefs(ctx context.Context, repoKey, image, manifestDigest string, refs []*DockerRef) error
	// ListRefsByManifest returns the ref rows of one manifest ordered by
	// blob_digest.
	ListRefsByManifest(ctx context.Context, repoKey, image, manifestDigest string) ([]*DockerRef, error)
	// RefsByBlob reports whether any manifest still references the blob
	// (repo-key scoped — the blob-delete precheck that idx_docker_refs_blob
	// serves, mirroring idx_nodes_blob).
	RefsByBlob(ctx context.Context, repoKey, blobDigest string) (bool, error)

	// ListImages returns repository image names ("<image>") that have at
	// least one manifest row, lexicographically ordered (the /v2/_catalog
	// source: docker_manifests DISTINCT). Pagination is keyset-based with an
	// exclusive cursor, limit<=0 meaning "all".
	ListImages(ctx context.Context, repoKey, after string, limit int) ([]string, error)

	// DeleteImage drops every manifest/tag/ref row of (repo_key, image) in
	// one transaction. Row removal is idempotent; it reports how many
	// manifest rows were removed. Deleting an image that never existed is
	// not an error (it returns 0) — the caller owns the 404 decision.
	DeleteImage(ctx context.Context, repoKey, image string) (manifestsRemoved int64, err error)

	// DeleteRepoRefs drops every docker_refs row of a repository. Repository
	// deletion cascades docker_manifests/docker_tags through their FKs, but
	// docker_refs has no DB-level FK (architecture 11.12): the caller must
	// invoke this in the same logical teardown before the repo row goes.
	DeleteRepoRefs(ctx context.Context, repoKey string) (int64, error)
}
