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

// ErrLastAdmin is returned by UserStore.DeleteCascade when the guarded
// delete refused to remove the last admin-role account (T-273): the census
// rides the cascade transaction itself, so two concurrent deletes can never
// strand the instance admin-less. The auth layer maps it onto its own
// last-admin delete sentinel (the 400 the wire answers).
var ErrLastAdmin = errors.New("metadata: deleting the last admin-role user is refused")

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

// ErrGroupNotFound is returned by GroupStore methods for missing groups.
var ErrGroupNotFound = errors.New("metadata: group not found")

// ErrWebSessionNotFound is returned by WebSessionStore methods for missing
// web_sessions rows.
var ErrWebSessionNotFound = errors.New("metadata: web session not found")

// ErrUploadSessionNotFound is returned by UploadSessionStore methods for
// missing upload_sessions rows.
var ErrUploadSessionNotFound = errors.New("metadata: upload session not found")

// ErrInvalidCursor is returned by AuditStore.Query when the cursor parameter
// cannot be decoded; the HTTP layer maps it to 400 (GE-01).
var ErrInvalidCursor = errors.New("metadata: invalid cursor")

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

// Role spellings of the users.role column (011 widening, M7 ADR-0026). The
// closed set is defined by the auth package (auth.Role*); the metadata layer
// stores and returns the text verbatim and does not interpret it. Empty (rows
// from callers predating 011, hand-built test fixtures) means "derive from
// IsAdmin", which Create applies so the is_admin mirror never drifts on insert.
const (
	RoleAdmin         = "admin"
	RoleReadOnlyAdmin = "readonly_admin"
	RoleUser          = "user"
)

// User is one local account.
type User struct {
	Username     string
	PasswordHash string // argon2id PHC string; never round-trips through API responses
	IsAdmin      bool   // compatibility mirror of Role == "admin" (ADR-0026 decision 6; removal M8)
	Enabled      bool
	CreatedAt    string
	UpdatedAt    string
	Email        string // 004 widening (FR-27-AC8): '' when unset; blank-vs-shape validation is a service-layer concern
	Provider     string // 008 widening (M6, ADR-0020): 'local', 'oidc', or 'ldap'; DEFAULT 'local'
	ProviderID   string // 008 widening: stable ID from the identity provider (OIDC sub or LDAP DN); DEFAULT ''
	Role         string // 011 widening (M7, ADR-0026): 'admin' | 'readonly_admin' | 'user'; '' on Create derives from IsAdmin
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
// read/write/delete booleans; write covers upload but not delete. CanManage
// (011 widening, M7 ADR-0026) is the repo-scoped admin bit: it matches on the
// target's repos list only — includes/excludes never apply to it, and it
// implies none of r/w/d.
type PermissionPrincipal struct {
	ID            int64
	TargetName    string
	Principal     string
	PrincipalType string // "user" (groups [M4])
	CanRead       bool
	CanWrite      bool
	CanDelete     bool
	CanManage     bool
}

// AuditEvent is one append-only audit record (architecture section 3.5).
type AuditEvent struct {
	ID      int64
	Time    string
	Actor   string
	Action  string // deploy|delete|download|login.success|auth.failed|repo.create|... (T-187: auth.failed replaced login.failed)
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

// Group is one user group row (004, groups table). Groups are the
// principal_type='group' side of permission_principals: authorization unions
// the user's group grants with the user's own grants (T-97).
type Group struct {
	ID          int64
	Name        string // unique; validated by the service layer (charset/reserved words)
	Description string
	CreatedAt   string
	UpdatedAt   string
}

// WebSession is one browser session row (004, web_sessions; ADR-0014). The
// session id plaintext only ever travels in the binflow_session cookie —
// this row stores sha256(plaintext), the same rule as tokens.
type WebSession struct {
	IDHash     string // sha256(session id plaintext), primary key
	Username   string
	CreatedAt  string
	ExpiresAt  string // absolute TTL cap; the sliding window never extends past it
	LastUsedAt string // '' when never used
	RevokedAt  string // '' while live; logout sets it (replayed cookies must fail)
}

// RepoUsage is one repo_usage row (004, ADR-0015 decision 2): the logical
// byte total of a repository's node references, maintained in the same
// transaction as node insert/delete (T-95/GE-05). A repository without a row
// (freshly created, never written to) has a zero total, not an error.
type RepoUsage struct {
	RepoKey      string
	LogicalBytes int64
	UpdatedAt    string
}

// UsageRow is one row of the E1 batch aggregate (M9, ADR-0030 / architecture
// section 14.1 E1, FR-79.1): repository identity and config moment joined
// with the metered total, the single-query data source behind
// GET /api/v1/storage/usage. UsedBytes repeats RepoUsage.LogicalBytes under
// the endpoint's spelling; a repository without a repo_usage row answers 0
// (the Get single-repo semantics, set-shaped). NodeCount is filled only when
// the caller asked for counts (the ?include=counts arm) and counts FILE nodes
// only — folder sentinel rows (FolderMarkerSHA) are directory markers, not
// artifacts, and never inflate the count.
type UsageRow struct {
	RepoKey   string
	Type      string // repository class ('local' | 'remote' | 'virtual')
	Config    string // the raw config blob (quotaBytes lives here for local)
	UpdatedAt string // repositories.updated_at — the CONFIG change moment
	UsedBytes int64
	NodeCount int64 // 0 unless the caller requested counts
}

// UploadSession is one upload_sessions row (010, T-209): the persisted state
// of a local-filestore upload session. The disk storage engine owns the
// lifecycle; State is opaque JSON owned by the engine (the metadata layer
// never parses it), so the engine may widen the JSON shape without a
// metadata migration.
type UploadSession struct {
	ID        string // session uuid, primary key
	State     string // opaque JSON owned by the engine (created_at/received/version)
	CreatedAt string // RFC3339 UTC
	ExpiresAt string // RFC3339 UTC; startup sweep deletes rows past this
}

// LicenseRecord is the single licenses row (012, M10 T-279 / ADR-0032).
// The single-license model is the table's contract: id is CHECK-pinned to
// 1 and PutLicense replaces the row atomically. Doc carries the document
// text verbatim — it is the fact source the license.Manager re-verifies
// (startup and daily ticker) — while the remaining columns are derived
// projections for query/render. ExpiresAt is ” for a perpetual
// (community-tier) license, mirroring the column's NULL. The tier value is
// the license package's closed-set spelling stored verbatim; this layer
// does not interpret it.
type LicenseRecord struct {
	LicenseID   string
	Tier        string
	Licensee    string
	Doc         string // the exact two-segment document text
	IssuedAt    string // RFC3339 UTC
	NotBefore   string // RFC3339 UTC
	ExpiresAt   string // RFC3339 UTC; '' = perpetual
	InstalledAt string // RFC3339 UTC
}

// AuditQuery is the full-parameter audit filter (GE-01, M4). Every field is
// optional; the zero query returns the newest events. Since and Until are
// RFC3339 UTC text forming a closed-open interval on the event time
// (Since <= time < Until — text comparison is chronological for RFC3339 UTC
// values). Cursor is the opaque keyset cursor of the previous page's last
// row; results are newest-first ordered (time DESC, id DESC) so every filter
// shape is served by the (column, time) composite indexes without a full
// table scan.
type AuditQuery struct {
	RepoKey string
	Actor   string
	Action  string
	Since   string // RFC3339; inclusive lower bound
	Until   string // RFC3339; exclusive upper bound
	Limit   int    // <=0 means 100 (mirrors List)
	Cursor  string // "" means first page; "<RFC3339>|<id>" afterwards
}

// Store is the dialect-neutral entry to all metadata state. Migrations run
// automatically inside Open (ADR-0007); they are not part of this interface.
// Implementations must be safe for concurrent use (SQLite relies on WAL plus
// database/sql pool serialization, see store.go).
type Store interface {
	// Repos/Nodes/Blobs/Users/Tokens/Permissions/Audits/Docker/Remote/Virtual/
	// Groups/WebSessions return the sub-stores sharing the same underlying
	// handle.
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
	Groups() GroupStore
	WebSessions() WebSessionStore
	UploadSessions() UploadSessionStore
	Usage() UsageStore
	Licenses() LicenseStore
	// IsReferenced reports whether any node row or docker ref row currently
	// points at sha256 ([M9] ADR-0031 mechanism A): the single-point Live
	// oracle behind the GC sweep's pre-delete recheck. It spans two sub-stores
	// (nodes ∪ docker_refs, architecture sections 4.4 and 11.12), which is why
	// it lives on the aggregate instead of either sub-store. Callers must not
	// use it to rebuild the referenced set — Mark-shaped walks keep their own
	// listing form; this method is the bounded freshness probe and nothing
	// else.
	IsReferenced(ctx context.Context, sha256 string) (bool, error)
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
	// UpdateEmail sets the 004 email column (FR-27-AC8); ErrUserNotFound when
	// the user does not exist.
	UpdateEmail(ctx context.Context, username, email string) error
	// UpdateProfile refreshes the mutable non-credential columns (email,
	// admin flag) of one account in a single statement; ErrUserNotFound when
	// the user does not exist. The password hash keeps its own seam
	// (UpdatePassword) because it carries a freshly derived argon2id value
	// while this method serves the create-or-replace and partial-update
	// bodies of /api/security/users/{name} (T-97 SE-05/06).
	UpdateProfile(ctx context.Context, username, email string, isAdmin bool) error
	// SetEnabled flips the enabled flag of one account (T-208, ADR-0025
	// decision 6): disabling an account invalidates its existing sessions and
	// tokens immediately because Verify re-resolves the owner row per request.
	// ErrUserNotFound when the user does not exist.
	SetEnabled(ctx context.Context, username string, enabled bool) error
	// SetRole writes the 011 role column of one account and maintains the
	// is_admin compatibility mirror in the SAME statement (role = 'admin' <=>
	// is_admin = 1, ADR-0026 decision 6). The role value is stored verbatim;
	// closed-set validation is the service layer's concern. ErrUserNotFound
	// when the user does not exist. Consumers: the idp_sync authoritative
	// rewrite (every provider authentication) and the adminRole wire field.
	SetRole(ctx context.Context, username string, role string) error
	// DeleteCascade removes the account and every dependent row in ONE
	// transaction (M9, ADR-0030 E4): the permission_principals rows naming
	// the account as a USER principal go first (permission_principals has no
	// DB-level FK to users, so the ACE strip is explicit — gap-endpoints
	// section 2.2 step 2), then the users row itself, whose foreign keys
	// cascade user_groups, tokens and web_sessions (verify re-resolves the
	// owner row per request, so the drop is an immediate credential kill,
	// ADR-0025 guardrail 3). audit_events rows are actor-named text and
	// deliberately survive.
	//
	// The users-row delete is census-guarded in the same statement (T-273):
	// it only removes an admin-role row while another admin-role row would
	// survive, so the last admin can never be deleted no matter what the
	// caller checked beforehand. ErrUserNotFound when the account does not
	// exist and ErrLastAdmin when the census predicate refused the row; both
	// probes ride the same transaction, so either refusal leaves zero side
	// effects (an orphan user-typed ACE row of the same name is NOT stripped
	// in those cases).
	DeleteCascade(ctx context.Context, username string) error
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
	// Principals returns every principal row of every target in ONE query
	// (M9 E9, T-254): the unkeyed companion of PrincipalsFor. auth's
	// ManageCoverage walks all targets without a repo key in hand, and the
	// only alternative over the existing seams — PrincipalsFor per distinct
	// repo — would scale the query count with the repository count (the
	// N+1 the coverage evaluation is gated against). Rows whose target row
	// disappeared are excluded by the same JOIN PrincipalsFor uses.
	Principals(ctx context.Context) ([]*PermissionPrincipal, error)
	// GroupReferences returns the names of every permission target carrying
	// a group principal row for the named group (T-97, SE-04's delete
	// guard): deleting a referenced group would silently strip its members
	// of every grant, so the service layer answers 409 listing these names.
	// User-typed rows naming the same string are irrelevant and excluded.
	GroupReferences(ctx context.Context, group string) ([]string, error)
}

// AuditStore appends and queries audit events. Append is best-effort upstream
// (architecture 3.5); the store itself reports errors honestly.
type AuditStore interface {
	Append(ctx context.Context, e *AuditEvent) error
	// List returns events newest-first, at most limit rows, optionally
	// filtered by repo and actor.
	List(ctx context.Context, repoKey, actor string, limit int) ([]*AuditEvent, error)
	// Query is the full-parameter face (GE-01, M4): repo/actor/action
	// equality filters, a closed-open Since/Until time window and keyset
	// cursor pagination, newest-first (time DESC, id DESC). The next cursor
	// after a page is derived from the last returned row
	// ("<time>|<id>"); a malformed cursor returns an error wrapping
	// ErrInvalidCursor. Every filter shape is served by the 004 composite
	// indexes (actor,time)/(action,time) or the 001 (repo_key,time)/time
	// indexes — never a full table scan.
	Query(ctx context.Context, q AuditQuery) ([]*AuditEvent, error)
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

// GroupStore is user-group CRUD and membership (schema 004; architecture
// section 6 final DDL). Groups carry no admin bit by design (FR-27-AC9): an
// admin-flagged member grants nothing — the authorization effect of a group
// is exactly the union of its permission_principals grants (T-97).
//
// Membership rows live in user_groups and are maintained per user:
// SetUserGroups is the single write path (the groups[] field of
// PUT/POST /api/security/users/{name}). Deleting a group row cascades its
// memberships; deleting a user cascades theirs — both via FKs.
type GroupStore interface {
	// Create inserts a group; the name is the caller-visible key (charset
	// and reserved-word rules are validated by the service layer, T-97).
	Create(ctx context.Context, g *Group) error
	// Update refreshes description/updated_at of the named group;
	// ErrGroupNotFound when absent.
	Update(ctx context.Context, g *Group) error
	// Get returns ErrGroupNotFound when absent.
	Get(ctx context.Context, name string) (*Group, error)
	// Delete removes the group and cascades its user_groups rows (FK).
	// Whether a group referenced by a permission target may be deleted is a
	// service-layer decision (409, PRD K3) — the store itself carries no
	// target reference.
	Delete(ctx context.Context, name string) error
	// List returns every group ordered by name.
	List(ctx context.Context) ([]*Group, error)
	// SetUserGroups atomically replaces the user's memberships with the
	// named groups: existing rows not in the list are released, new ones are
	// upserted (re-applying the same list is a no-op). An empty list clears
	// the membership. Every named group must exist and so must the user
	// (FKs); a missing group surfaces as ErrGroupNotFound wrapped with its
	// name so the caller can answer the 400 wording.
	SetUserGroups(ctx context.Context, username string, groupNames []string) error
	// GroupsOfUser resolves the groups the user belongs to, ordered by name
	// — the authentication-time fill of Principal.Groups (architecture 3.4).
	// An unknown or group-less user returns an empty slice, not an error.
	GroupsOfUser(ctx context.Context, username string) ([]*Group, error)
	// MembershipsByUser returns every user's group-name set in ONE query
	// (M9, ADR-0030 E2 — the groups[] widening of GET /api/security/users;
	// one aggregated walk replaces the per-user N+1 the users page used to
	// pay). Users without memberships simply have no map entry; the caller
	// renders []. Names within one user's set are ordered by group name.
	MembershipsByUser(ctx context.Context) (map[string][]string, error)
	// MembershipsByGroup resolves ONE group's member usernames in a single
	// JOIN statement (M9, ADR-0030 E5 — the userNames[] of GET
	// /api/security/groups/{name}?includeUsers=true), the group-side mirror
	// of MembershipsByUser's family: the whole member set is one aggregated
	// query whatever its size, never a per-user walk. Usernames come back
	// ordered; a member-less group returns an empty slice, not an error —
	// existence is Get's question, which the caller runs first for the 404.
	MembershipsByGroup(ctx context.Context, group string) ([]string, error)
}

// WebSessionStore is browser-session persistence (schema 004, ADR-0014:
// server-side sessions keyed by sha256(id); storage mirrors the tokens rule).
// Expiry and revocation are row facts the caller evaluates — GetBySHA256
// returns expired and revoked rows alike so the caller can distinguish
// "unknown cookie" from "logged out" and rate answers accordingly.
type WebSessionStore interface {
	// Create inserts one session row; id_hash is unique by primary key.
	Create(ctx context.Context, s *WebSession) error
	// GetBySHA256 resolves a session by its id hash; ErrWebSessionNotFound
	// when no row carries it.
	GetBySHA256(ctx context.Context, idHash string) (*WebSession, error)
	// Touch records last_used_at (the sliding-window heartbeat);
	// ErrWebSessionNotFound when absent.
	Touch(ctx context.Context, idHash, lastUsedAt string) error
	// Revoke marks the session logged out (idempotent — re-revoking a
	// revoked row just refreshes the timestamp); ErrWebSessionNotFound when
	// absent.
	Revoke(ctx context.Context, idHash, revokedAt string) error
	// ListSweepable returns expired (expires_at <= now) or revoked rows —
	// the startup sweep candidates (architecture 11 item 18), ordered by
	// expires_at then id_hash for determinism. limit<=0 means all.
	ListSweepable(ctx context.Context, now string, limit int) ([]*WebSession, error)
	// Delete removes one row; ErrWebSessionNotFound when absent.
	Delete(ctx context.Context, idHash string) error
}

// UploadSessionStore is the persistence seam over upload_sessions (010, T-209)
// for the local-filestore upload session. The storage engine owns the rows:
// it creates one on BeginSession, re-reads it on ResumeSession and deletes it
// on Commit/Abort/expiry. State is opaque to the metadata layer.
type UploadSessionStore interface {
	// Create inserts one session row; ErrDuplicate-class constraint errors are
	// wrapped by the driver (a fresh uuid colliding is a retryable rarity).
	Create(ctx context.Context, s *UploadSession) error
	// Get resolves a session by id; ErrUploadSessionNotFound when absent.
	Get(ctx context.Context, id string) (*UploadSession, error)
	// SetState replaces the opaque state blob for an existing session;
	// ErrUploadSessionNotFound when absent.
	SetState(ctx context.Context, id, state string) error
	// Delete removes a session row. Idempotent: deleting an absent row reports
	// ErrUploadSessionNotFound so the caller can distinguish replayed cleanup
	// from a missing session.
	Delete(ctx context.Context, id string) error
	// ListExpired returns the expired (expires_at <= now) rows, ordered by
	// expires_at then id for determinism — the startup sweep candidates.
	// limit<=0 means all.
	ListExpired(ctx context.Context, now string, limit int) ([]*UploadSession, error)
}

// LicenseStore is the single-row licenses persistence seam (012, M10
// T-279 / ADR-0032). The license.Manager is the consumer; the metadata
// layer stores and returns rows without interpreting tier spellings or the
// document text.
type LicenseStore interface {
	// GetLicense returns the stored license row, or (nil, nil) when no
	// license is installed (the community floor — an absent row is a
	// state, not an error).
	GetLicense(ctx context.Context) (*LicenseRecord, error)
	// PutLicense atomically replaces the single row: delete + insert in
	// ONE transaction, so a replacement either lands whole or leaves the
	// previous license fully in force (the D7 install contract).
	PutLicense(ctx context.Context, rec *LicenseRecord) error
	// DeleteLicense removes the row. Idempotent: deleting an empty table
	// succeeds (the floor is already in force).
	DeleteLicense(ctx context.Context) error
}

// UsageStore is the quota accounting seam over repo_usage (004; ADR-0015
// decision 2, consumed by repo.Service's write gates — T-95/GE-05). The two
// write methods are the SAME-TRANSACTION variants of the node operations:
// the node row and the logical_bytes counter land or vanish together, which
// is the invariant a quota number may be enforced against. Plain
// NodeStore.Put/Delete (the remote proxy cache path among others) never
// touch the counter — pull-through landing is deliberately unmetered (PRD
// section 7 Q2).
type UsageStore interface {
	// Get returns the repository's usage row; a repository without a row
	// answers a zero total, not an error (an empty repository uses 0 bytes).
	Get(ctx context.Context, repoKey string) (*RepoUsage, error)
	// List returns the usage aggregate of EVERY repository in one query (M9,
	// ADR-0030 / architecture section 14.1 E1): repositories LEFT JOIN
	// repo_usage ordered by repo_key — the batch endpoint's data source. One
	// statement (two with counts), never N per-repo round trips: the E1
	// fan-out collapse exists precisely because per-repo Usage().Get loops
	// are the ~170-requests-per-page posture the endpoint retires. Repos
	// without a repo_usage row answer UsedBytes 0 (Get's zero-total
	// semantics, set-shaped). includeCounts adds the per-repo FILE node
	// count (folder sentinel rows excluded); callers that will not render
	// counts pass false and skip the aggregation entirely (the zero-cost
	// default path).
	List(ctx context.Context, includeCounts bool) ([]UsageRow, error)
	// PutNodeWithUsage upserts the node exactly like NodeStore.Put and, in
	// the SAME transaction, adjusts repo_usage by the size difference against
	// the row the upsert replaces (new node: +size; overwrite: +new-old;
	// idempotent same-content redeploy: +0 — same sha256 implies same size).
	PutNodeWithUsage(ctx context.Context, n *Node, updatedAt string) error
	// DeleteNodeWithUsage drops one node row exactly like NodeStore.Delete
	// (ErrNodeNotFound when absent) and, in the SAME transaction, subtracts
	// its size from repo_usage. Folder rows contribute their stored size
	// (always 0 for the folder marker).
	DeleteNodeWithUsage(ctx context.Context, repoKey, path, updatedAt string) error
}
