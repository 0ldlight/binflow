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

// ErrRemoteConfigNotFound is returned by RemoteStore methods for missing
// remote_configs rows.
var ErrRemoteConfigNotFound = errors.New("metadata: remote config not found")

// ErrRemoteCacheNotFound is returned by RemoteStore cache methods for
// missing remote_cache rows.
var ErrRemoteCacheNotFound = errors.New("metadata: remote cache entry not found")

// ErrStoreBusy marks TRANSIENT write contention (SQLITE_BUSY/SQLITE_LOCKED
// surfacing past the busy_timeout budget — T-54's F1 flake: a whole-repo
// parallel test load or an fsync storm can starve the WAL writer longer than
// the timeout, and the waiter's error is "locked", not "broken"). Every
// store error passes wrapExec, which attaches this sentinel when the driver
// error is busy-class; upper layers answer retryable (HTTP 503), never
// permanent-failure.
var ErrStoreBusy = errors.New("metadata: store busy")

// IsStoreBusy reports whether err (or anything it wraps) is busy-class store
// contention — the retryable signal of ErrStoreBusy.
func IsStoreBusy(err error) bool { return errors.Is(err, ErrStoreBusy) }

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

// RemoteConfig is one remote proxy repository configuration row (001 table,
// widened by 003: dual cache TTL, SSRF exemption flag, blocked_out rename).
// The 001 cache_ttl_seconds column is legacy, superseded by the dual TTL and
// intentionally not surfaced.
//
// Password is opaque to this store: from T-66 on it carries
// 'enc:v1:<b64(nonce+ciphertext)>' AES-256-GCM text under env
// BINFLOW_REMOTE_CREDENTIALS_KEY (ADR-0012); the store neither inspects nor
// transforms it, and callers must never log it.
type RemoteConfig struct {
	RepoKey              string
	URL                  string
	Username             string
	Password             string
	ContentTTLSeconds    int64 // artifact cache TTL (DDL default 86400)
	MetadataTTLSeconds   int64 // metadata cache TTL (DDL default 600)
	AllowPrivateUpstream bool  // SSRF chain exemption (admin-set, audited; ADR-0012)
	BlockedOut           bool  // manual mask (ADR-0012 decision 2)
}

// Remote cache entry kinds (ADR-0012 TTL split): artifacts carry the long
// content TTL; regenerable metadata (maven-metadata.xml, npm packument, PyPI
// simple pages) carries the short metadata TTL.
const (
	RemoteCacheKindContent  = "content"
	RemoteCacheKindMetadata = "metadata"
)

// RemoteCacheEntry is one row of remote_cache (003): the conditional
// revalidation state (ETag / Last-Modified) and TTL clocks of one cached
// (repo, path) pair. The cached bytes themselves are an ordinary blob plus
// node in the remote repo's namespace — this table only tracks validators so
// the local artifact surface (nodes/blobs) stays untouched.
type RemoteCacheEntry struct {
	RepoKey      string
	Path         string
	ETag         string
	LastModified string
	FetchedAt    string // RFC3339 UTC
	ExpiresAt    string // RFC3339 UTC; lexicographic order == chronological order for Now()-style values
	Kind         string // RemoteCacheKindContent | RemoteCacheKindMetadata
}

// VirtualMember is one row of virtual_members (001 table). The position
// semantics are ADR-0013's as amended by the T-79 linkage record (PRD C3):
// resolution is two-bucket — priorityResolution-marked members first, then
// the rest — and position records declaration order within each bucket
// (local is no longer unconditionally first). Member-level flags such as
// priorityResolution live in the repositories.config JSON, not in this table.
type VirtualMember struct {
	VirtualRepo string
	MemberRepo  string
	Position    int64
}

// Store is the dialect-neutral entry to all metadata state. Migrations run
// automatically inside Open (ADR-0007); they are not part of this interface.
// Implementations must be safe for concurrent use (SQLite relies on WAL plus
// database/sql pool serialization, see store.go).
type Store interface {
	// Repos/Nodes/Blobs/Users/Tokens/Permissions/Audits/Docker/Remote/Virtual
	// return the sub-stores sharing the same underlying handle.
	Repos() RepoStore
	Nodes() NodeStore
	Blobs() BlobStore
	Users() UserStore
	Tokens() TokenStore
	Permissions() PermissionStore
	Audits() AuditStore
	Docker() DockerStore
	Remote() RemoteStore
	Virtual() VirtualStore
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
	// GetBySha1 resolves a blob row through its sha1 (idx_blobs_sha1, the
	// T-73 seam): the sha1-keyed checksum-deploy lookup the maven ecosystem
	// needs (mvn/wagon callers whose only declared digest is the sha1).
	// sha1 carries no schema uniqueness; the first row wins — two DIFFERENT
	// contents sharing a sha1 is cryptographically absurd, and identical
	// bytes are one row keyed by their common sha256. ErrNotFound when no
	// row carries the sha1 (the caller's miss, not a shape error).
	GetBySha1(ctx context.Context, sha1 string) (*Blob, error)
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

// RemoteStore is the remote proxy configuration and cache-validator state
// behind the pull-through fetcher (schema 001 remote_configs widened by 003,
// remote_cache new in 003; architecture section 6 final DDL).
//
// The config methods are CRUD over remote_configs rows; the repo row itself
// (type "remote") lives in RepoStore and both sides share the repo_key. The
// cache methods track one validator row per cached (repo, path) — the cached
// bytes are ordinary blobs/nodes and never pass through here.
type RemoteStore interface {
	// CreateConfig inserts one config row; the repo row must already exist
	// (FK to repositories.repo_key).
	CreateConfig(ctx context.Context, c *RemoteConfig) error
	// UpdateConfig refreshes every non-key column of an existing row;
	// ErrRemoteConfigNotFound when the repo has no config row.
	UpdateConfig(ctx context.Context, c *RemoteConfig) error
	// GetConfig returns ErrRemoteConfigNotFound when absent.
	GetConfig(ctx context.Context, repoKey string) (*RemoteConfig, error)
	// DeleteConfig removes the row; ErrRemoteConfigNotFound when absent.
	DeleteConfig(ctx context.Context, repoKey string) error

	// PutCache upserts by (repo_key, path): a re-fetch of the same path
	// refreshes validators and clocks, never conflicts.
	PutCache(ctx context.Context, e *RemoteCacheEntry) error
	// GetCache returns ErrRemoteCacheNotFound when absent.
	GetCache(ctx context.Context, repoKey, path string) (*RemoteCacheEntry, error)
	// DeleteCache drops one cached path's validator row (the RE-06
	// force-refresh move: delete the cache, the next GET re-fetches);
	// ErrRemoteCacheNotFound when absent.
	DeleteCache(ctx context.Context, repoKey, path string) error
	// DeleteCacheByRepo drops every validator row of one repository
	// (teardown and ?deleteContent=true cascades). Returns the number of
	// rows removed; deleting a repo with no cache rows returns 0.
	DeleteCacheByRepo(ctx context.Context, repoKey string) (int64, error)
	// ListExpiredCache returns entries with expires_at <= now, ordered by
	// expires_at (then repo_key, path for determinism) — the periodic sweep
	// candidates that idx_remote_cache_expiry serves. limit<=0 means all.
	ListExpiredCache(ctx context.Context, now string, limit int) ([]*RemoteCacheEntry, error)
}

// VirtualStore is the virtual repository member ledger (virtual_members,
// 001 table; position semantics per ADR-0013). Member-list CRUD is
// replace-shaped: SetMembers is the create/update operation (slice order
// becomes the position sequence) and SetMembers with an empty slice is the
// delete; ListMembers is the read. Deleting either repository row cascades
// the member rows through their FKs, so no separate teardown call exists.
type VirtualStore interface {
	// SetMembers atomically replaces the member list of one virtual repo:
	// positions are assigned 0..n-1 in slice order (declaration order). An
	// empty or nil slice clears the member set. The virtual repo and every
	// member repo must exist (FKs); duplicate members surface the primary
	// key constraint as an error — caller-side validation owns the 400.
	SetMembers(ctx context.Context, virtualRepo string, members []string) error
	// ListMembers returns the members of one virtual repo ordered by
	// position (ties, which hand-written rows could carry, break on
	// member_repo for determinism). An unknown or member-less repo returns
	// an empty slice, not an error.
	ListMembers(ctx context.Context, virtualRepo string) ([]*VirtualMember, error)
}
