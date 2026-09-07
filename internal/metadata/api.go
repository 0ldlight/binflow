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

// ErrBuildNotFound is returned by BuildStore Get/Delete for missing build
// runs (the four-tuple lookup found no row).
var ErrBuildNotFound = errors.New("metadata: build not found")

// ErrBundleNotFound is returned by BundleStore reads for missing bundles
// (the (name, version) pair has no row).
var ErrBundleNotFound = errors.New("metadata: bundle not found")

// ErrBundleExists is returned by BundleStore's InsertBundle when the pair
// (bundle_name, bundle_version) already carries a row — the UNIQUE-key fact
// the release-bundle conflict tri-state evaluates (ADR-0046 Errata ② E6).
var ErrBundleExists = errors.New("metadata: bundle already exists")

// ErrScheduleNotFound is returned by ScheduleStore Get/Delete for missing
// schedules rows (the "no row = not scheduled" single-state semantics of
// ADR-0044 decision 2: absence is the unscheduled state, callers render it
// as such rather than erroring on the READ side).
var ErrScheduleNotFound = errors.New("metadata: schedule not found")

// ErrBackupNotFound is returned by BackupStore Get/Delete for missing
// backups rows.
var ErrBackupNotFound = errors.New("metadata: backup not found")

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

// NodeStats is the per-node download statistics projection of the nodes
// table's four counting columns (M16 T-438, ADR-0044 K69 — the download
// plane's SINGLE counting channel: the ?stats wire face and the usage
// domain read this shape and nothing else counts). It deliberately does
// not extend Node: the content-plane Node shape is frozen (FileInfo zero
// change, architecture 25.5) and statistics are a read-side projection.
type NodeStats struct {
	RepoKey             string
	Path                string
	DownloadCount       int64  // three-arm criterion: direct, via virtual (member row), remote-serving
	LastDownloadedAt    string // RFC3339 UTC, "" = never downloaded
	LastDownloadedBy    string // principal name, "anonymous" for unauthenticated, "" = never
	RemoteDownloadCount int64  // downloads a REMOTE repository served; structurally 0 on local rows
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
	// DeployScope (017 widening, M12 T-349 / FR-113.3): '' keeps the
	// historical unrestricted meaning; any other value is a JSON document
	// narrowing the token to one landing operation, parsed and enforced by
	// the auth verifier (unparseable values fail closed there).
	DeployScope string
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
// implies none of r/w/d. CanAnnotate (023 widening, M16 T-444 / ADR-0044
// K68) is the property-write bit — the ?properties family's PUT/DELETE gate;
// a path-plane action like r/w/d, implying and implied by none of them
// (migration 023 backfilled it onto every can_write row, so pre-split
// grants keep the property-write face they had).
type PermissionPrincipal struct {
	ID            int64
	TargetName    string
	Principal     string
	PrincipalType string // "user" (groups [M4])
	CanRead       bool
	CanWrite      bool
	CanDelete     bool
	CanManage     bool
	CanAnnotate   bool
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

	// Smart remote effective fields (014, T-290 / FR-90.2). Zero means
	// "unset" everywhere: the fetcher falls back to the legacy
	// repositories.config JSON field and then the product default, so
	// pre-014 rows keep their exact behavior.
	SocketTimeoutMs              int64 // upstream IO timeout, ms granularity (0 = legacy secs path)
	MetadataRetrievalTimeoutSecs int64 // per-repo metadata singleflight wait cap (0 = 60s)
	UnusedCleanupPeriodHours     int64 // unused-artifact cleanup period (0 = off; engine is M11)
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

// AuthConfigRecord is one protocol section of the authentication
// configuration descriptor (015, M11 T-305 / ADR-0035 decision 2). Doc is the
// section's JSON text under the write path's strict schema; secrets inside
// Doc are always 'enc:v1:'-sealed by the auth ConfigManager before they
// reach this layer — the store never interprets the payload.
type AuthConfigRecord struct {
	Section   string // closed set: 'ldap' | 'oidc' | 'saml'
	Doc       string // section JSON (secrets sealed)
	UpdatedAt string // RFC3339 UTC
	UpdatedBy string // principal name of the last writer
}

// AuthConfigStore is the section-granularity authentication-configuration
// seam (015). PutAuthConfig is an integral replacement — no field-level
// merge; disabling a section is the enabled flag inside the doc, not a row
// delete.
type AuthConfigStore interface {
	GetAuthConfig(ctx context.Context, section string) (*AuthConfigRecord, error)
	PutAuthConfig(ctx context.Context, rec *AuthConfigRecord) error
	ListAuthConfigs(ctx context.Context) ([]*AuthConfigRecord, error)
}

// GpgKeypairRecord is one instance-level GPG signing key pair (016, M11
// T-319, ADR-0038 decision 2). PairName is the Artifactory wire identifier
// (KeyPairInput.pairName) and the table's primary key in one. PublicKey is
// the armored public-key block verbatim (a public artifact); PrivateKeyEnc
// and PassphraseEnc are ALWAYS 'enc:v1:'-sealed (the armored private-key
// block as imported/generated, sealed whole under the instance master key)
// — plaintext key material never lands in this table, and neither sealed
// column is ever echoed by any read path (the private key and the
// passphrase never leave the store).
type GpgKeypairRecord struct {
	PairName      string // PK; [a-zA-Z][a-zA-Z0-9_-]{0,63} (validated upstream)
	PairType      string // closed set: 'GPG' (M11; the RSA PEM family is out of scope)
	Alias         string // display alias, echoed in KeyPairSummary
	PublicKey     string // armored public-key block (plaintext by design)
	PrivateKeyEnc string // enc:v1-sealed armored private-key block
	PassphraseEnc string // enc:v1-sealed passphrase ('' sealed is legal: no-passphrase keys)
	Algorithm     string // key material summary, e.g. 'RSA-4096' or 'Ed25519'
	CreatedAt     string // RFC3339 UTC
	UpdatedAt     string // RFC3339 UTC
	UpdatedBy     string // principal name of the last writer
}

// GpgKeypairStore is the instance keypair seam (016). PutKeypair is the
// integral replacement of one pair (create, update and rotation all land
// here — there is no field-level merge); DeleteKeypair removes the row
// (the service layer owns the in-use guard that refuses while any
// repository references the pair).
type GpgKeypairStore interface {
	GetKeypair(ctx context.Context, pairName string) (*GpgKeypairRecord, error)
	PutKeypair(ctx context.Context, rec *GpgKeypairRecord) error
	DeleteKeypair(ctx context.Context, pairName string) error
	ListKeypairs(ctx context.Context) ([]*GpgKeypairRecord, error)
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
	// Groups/WebSessions/Builds return the sub-stores sharing the same
	// underlying handle.
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
	NodeProps() NodePropStore
	AuthConfigs() AuthConfigStore
	GpgKeypairs() GpgKeypairStore
	Schedules() ScheduleStore
	Backups() BackupStore
	Builds() BuildStore
	Bundles() BundleStore
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
	// CountDownload atomically records one download against a node row (the
	// SQL-side self-increment is exact under SQLite's single writer and
	// Postgres row locks alike). by is the downloader's principal spelling
	// ("anonymous" for unauthenticated access), at the RFC3339 UTC landing
	// instant; remoteServed additionally bumps remote_download_count —
	// callers set it when the counted row lives in a remote repository, so
	// local rows keep the column at its structural zero. Folder rows are
	// excluded mechanically (the shared folder marker carries no body), and
	// a missing row is a silent no-op: counting keeps the audit row's
	// best-effort posture, so a node deleted between the serve and this
	// bookkeeping is history, not an error.
	CountDownload(ctx context.Context, repoKey, path, by, at string, remoteServed bool) error
	// Stats returns one node's download statistics projection (the four
	// counting columns); ErrNodeNotFound when the row is absent.
	Stats(ctx context.Context, repoKey, path string) (*NodeStats, error)
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
	// LastActionTimes is the per-actor aggregation face (FR-146.3, M16):
	// for every actor with at least one event of the named action it
	// returns the RFC3339 time of their most recent such event, from ONE
	// GROUP BY query — the users-list projection derives every row's
	// lastLoggedIn with a single statement instead of a per-user walk
	// (the N+1 shape the MembershipsByUser widening killed for groups).
	// The 004 idx_audit_action(action,time) index serves the filter. An
	// empty log returns an empty map, never nil.
	LastActionTimes(ctx context.Context, action string) (map[string]string, error)
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

// NodePropStore is the artifact-properties persistence seam (013, M10
// T-286 / ADR-0033, architecture section 15.3.2). Consumers are the deploy
// chain (repo.Service's PutOptions.Properties tail) and the ?properties
// REST family (httpapi). The store keeps the SET semantics — one row per
// (repo, path, key, value) — and the merge law of section 11.40 (same-key
// value-set replace, other keys kept); legality (charset, sizes,
// cardinality) is repo.ValidatePropSet's question and never re-checked
// here. Rows cascade on node and repository deletes through the 013 FKs,
// so the store carries no delete-cascade method of its own.
type NodePropStore interface {
	// List returns every property of one node: key -> value set, values
	// ordered (the primary key's value arm gives a deterministic order). A
	// node without properties answers an empty map, not an error — the
	// caller owns the node's existence question.
	List(ctx context.Context, repoKey, path string) (map[string][]string, error)
	// Merge applies the section-11.40 merge to ONE node in a single
	// transaction: every key in props has its value set REPLACED, keys not
	// named keep their rows, other nodes are untouched. An empty props map
	// is a no-op. The node must exist (the composite FK refuses orphan
	// annotations; callers resolve the 404 first).
	Merge(ctx context.Context, repoKey, path string, props map[string][]string) error
	// Delete drops the named keys of one node in a single transaction. A
	// nil/empty keys slice (the REST `properties=*` form) drops every key
	// of the node; naming keys it does not carry is not an error
	// (delete is idempotent — the caller answers 204 either way).
	Delete(ctx context.Context, repoKey, path string, keys []string) error
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

// Schedule is one row of the unified cron schedules ledger (021, M16
// T-446 / ADR-0044 decision 2): the full-type job one consuming plane
// (domain) wants fired on a Quartz-subset cron expression. The store is a
// dumb ledger — expression legality, next-run computation and dispatch are
// internal/scheduler's; the REST projection (GET /api/v1/system/schedules)
// and the three config surfaces are T-450's.
type Schedule struct {
	Domain   string // closed set: maintenance | backup | replication (021 CHECK)
	Key      string // in-domain entity key (job slot name, backup key, replication id)
	CronExpr string
	Enabled  bool
	// NextRunAt is RFC3339 UTC, '' = unscheduled (the disabled state).
	NextRunAt  string
	LastRunAt  string // RFC3339 UTC, '' = never ran
	LastStatus string // '' | ok | failed (021 CHECK)
	LastError  string // failure summary, already truncated by the writer
	CreatedAt  string
	CreatedBy  string
	UpdatedAt  string
	UpdatedBy  string
}

// ScheduleStore is the schedules ledger persistence seam (021, M16 T-446 /
// ADR-0044 decisions 1 and 2): exactly the five faces the scheduler engine
// and the config surfaces consume. The store validates nothing beyond the
// 021 CHECKs — the scheduler package owns expression parsing, next-run
// computation and the run-state write-back law (Put replaces the run-state
// columns too, so the engine's post-run re-arm is one full-row write).
type ScheduleStore interface {
	// Put upserts by (domain, key): an existing row keeps its
	// created_at/created_by, every other column is replaced.
	Put(ctx context.Context, s *Schedule) error
	// Get returns the row or wraps ErrScheduleNotFound.
	Get(ctx context.Context, domain, key string) (*Schedule, error)
	// Delete removes the row ("no row = not scheduled"; the config surface
	// drops it when the cronExp is cleared or the entity dies). Wraps
	// ErrScheduleNotFound when absent.
	Delete(ctx context.Context, domain, key string) error
	// List returns the rows of one domain ordered by key, or of every
	// domain ordered by (domain, key) when domain is "". The read
	// projection and the boot census read here.
	List(ctx context.Context, domain string) ([]*Schedule, error)
	// ListDue returns the enabled rows with a next_run_at at or before now
	// (RFC3339 UTC text; '' never matches), ordered by next_run_at — the
	// tick query's only call, served by idx_schedules_due.
	ListDue(ctx context.Context, now string) ([]*Schedule, error)
}

// Backup is one row of backups (022, M16 T-450 / ADR-0044 decisions 2 and
// 5): the PAYLOAD half of a scheduled backup configuration. The cron half
// lives in the schedules ledger under domain='backup' with the same key —
// this row is what the export carrier reads at fire time (the enabled bit
// and the server path the artifacts land under). Artifactory descriptor
// fields without a BinFlow carrier (repository subsets, incremental,
// retention rotation, zip archival, mail-on-error) are deliberately absent.
type Backup struct {
	Key       string // equals the schedules row key under domain='backup'
	Enabled   bool
	ExportDir string // server path; one timestamped subdirectory per fire
	CreatedAt string
	CreatedBy string
	UpdatedAt string
	UpdatedBy string
}

// BackupStore is the backup payload persistence seam (022, M16 T-450):
// the same dumb-ledger contract as ScheduleStore — the store validates
// nothing beyond the schema, the REST face owns key legality and the
// cron/next-run law.
type BackupStore interface {
	// Put upserts by key: an existing row keeps its created_at/created_by,
	// every other column is replaced.
	Put(ctx context.Context, b *Backup) error
	// Get returns the row or wraps ErrBackupNotFound.
	Get(ctx context.Context, key string) (*Backup, error)
	// Delete removes the row. Wraps ErrBackupNotFound when absent.
	Delete(ctx context.Context, key string) error
	// List returns every row ordered by key.
	List(ctx context.Context) ([]*Backup, error)
}

// DefaultBuildRepo is the build_repo logical key's default spelling (024,
// M17 T-507 / ADR-0045 Errata ②: product schema DDL DEFAULT + webhook.md
// section 3.4 sample + getPreferredBuildRepo fallback, triple-sourced). It
// is an AUTHORIZATION-domain key, not a repository: no repositories row is
// required and none is ever auto-created; a real repository of the same
// key does not interfere (build data bypasses nodes). BuildStore writes
// normalize an empty Repo to this value.
const DefaultBuildRepo = "artifactory-build-info"

// Build is one build run header row of builds (024, M17 T-507 /
// ADR-0045 decision 2): the CI-reported RECORD of one run — name, number,
// started stamp, owning build_repo and the archived original document.
// The wire fields beyond this header (buildAgent, agent, vcs, issues,
// licenseControl, url, durationMillis, ...) live in Payload verbatim; the
// normalized child tables carry only the queryable segments (modules,
// artifacts, dependencies, properties).
type Build struct {
	Name    string // build_name
	Number  string // build_number, CI free form ("51", "1.0.0", special chars allowed)
	Started string // wire literal yyyy-MM-dd'T'HH:mm:ss.SSSZ; run identity element 3
	Repo    string // build_repo logical ACL key; '' normalizes to DefaultBuildRepo on write
	Type    string // wire `type` (MAVEN|GRADLE|ANT|IVY|GENERIC); '' allowed
	// Payload is the archived original build info JSON ('' = none). It is
	// the GET face's echo source and stays byte-for-byte what arrived.
	Payload   string
	CreatedBy string
	CreatedAt string
	UpdatedBy string
	UpdatedAt string
}

// BuildName is one row of the names projection: the latest run's started
// stamp per (build_name, build_repo) — the GET /api/build face's source.
type BuildName struct {
	Name        string
	Repo        string
	LastStarted string // MAX(started) of the group, wire literal
}

// BuildNumber is one row of the numbers projection: every run of one
// build name, newest first — the GET /api/build/{name} face's source.
type BuildNumber struct {
	Number  string
	Started string // wire literal
	Repo    string
}

// BuildModule is one row of build_modules with its nested segment rows.
// ID is the Module ID field (T-512's consumption): the append face's merge
// key ("same id = same module") and the "<child-name>/<child-number>"
// reference form of aggregate builds. Artifacts and Dependencies keep the
// wire array order in Seq.
type BuildModule struct {
	ID   string // module_id
	Type string
	// Artifacts are the module's produced artifacts; a row with empty
	// RepoKey/Path is record-only (no nodes association).
	Artifacts []*BuildArtifact
	// Dependencies are the module's consumed inputs; they never resolve to
	// nodes (dependencies are not artifacts).
	Dependencies []*BuildDependency
}

// BuildArtifact is one row of build_artifacts. The wire fields (name,
// type, sha1, sha256, md5) are stored verbatim; the nodes association is
// the (RepoKey, Path) pair — the wire `path`'s repo segment split — and is
// REAL: the FK to nodes(repo_key, path) holds it. Empty RepoKey/Path means
// no association (stored NULL): the artifact line without a resolvable
// checksum/node link is a record, and the record outlives the node (ON
// DELETE SET NULL keeps the row when the node leaves).
type BuildArtifact struct {
	Seq    int64 // wire array order
	Name   string
	Type   string
	Sha1   string
	Sha256 string
	Md5    string
	// RepoKey/Path are the resolved nodes association ('' = record-only).
	RepoKey string
	Path    string
}

// BuildDependency is one row of build_dependencies. ID is the wire `id`
// whole coordinate ("g:a:v:c" et al — no name/version split, the layouts
// disagree and the payload keeps the full document); Scopes is the wire
// scopes[] joined with ','. Checksums are stored but never resolve to
// nodes: a dependency names what a build CONSUMED, existence is not
// required (inv-4 D1).
type BuildDependency struct {
	Seq    int64
	ID     string
	Type   string
	Scopes string
	Sha1   string
	Sha256 string
	Md5    string
}

// BuildPromotion is one row of build_promotions: the append-only history
// of a run's promotions (six-tuple status/timestamp/comment/repository/
// ciUser/user plus the archived request). Status is a FREE string — no
// closed set, staged/rolled-up/released are convention not protocol
// (ADR-0045 Errata ②); the run's current status is the newest row
// (max promoted_at). The writer mints ID (uuid).
type BuildPromotion struct {
	ID         string
	Name       string // build_name
	Number     string // build_number
	Started    string // run identity element 3
	Repo       string // build_repo
	Status     string // free string
	TargetRepo string // the promotion's target repository
	CiUser     string
	Comment    string
	DryRun     bool
	ParamsJSON string // archived promotion request
	PromotedBy string
	PromotedAt string // RFC3339 UTC
}

// BuildProperty is one row of build_properties: map semantics, one value
// per name (the wire `properties` object's entries).
type BuildProperty struct {
	Name  string
	Value string
}

// BuildStore is the build-info table family's persistence seam (024, M17
// T-507 / ADR-0045 decision 2): a dumb ledger in the ScheduleStore
// tradition — the store validates nothing beyond the schema's own
// constraints (FKs, CHECKs, the four-tuple key); coordinate legality,
// started-format parsing and the overwrite/merge laws are the consuming
// build service's (T-508/T-509). All reads and writes address a run by
// the four-tuple; an empty started in GetBuild means "the latest run of
// (name, number, repo)" and an empty repo normalizes to DefaultBuildRepo.
// Implementations must be safe for concurrent use.
type BuildStore interface {
	// PutBuild upserts one run header by the four-tuple: an existing row
	// keeps its created_at/created_by, every other column (type, payload,
	// updated_*) is replaced. Child segments are untouched — PutModules/
	// PutProperties own theirs.
	PutBuild(ctx context.Context, b *Build) error
	// GetBuild returns the run row. started = '' resolves the LATEST run
	// of (name, number, repo) by started DESC (the single-build GET face's
	// default); otherwise the exact run. Wraps ErrBuildNotFound.
	GetBuild(ctx context.Context, name, number, started, repo string) (*Build, error)
	// DeleteBuild removes the exact run and cascades its whole segment
	// (modules, artifacts, dependencies, properties, promotions). The
	// started coordinate is REQUIRED — no latest-run resolution on a
	// delete. Wraps ErrBuildNotFound when absent.
	DeleteBuild(ctx context.Context, name, number, started, repo string) error
	// ListBuildNames returns one row per (build_name, build_repo) with the
	// group's MAX(started), ordered by (name, repo). repo = '' spans every
	// build_repo; otherwise only that key's builds.
	ListBuildNames(ctx context.Context, repo string) ([]*BuildName, error)
	// ListBuildNumbers returns every run of one build name ordered by
	// started DESC (newest first — the "latest = take first" ruling), then
	// number DESC for determinism. repo = '' spans every build_repo.
	ListBuildNumbers(ctx context.Context, name, repo string) ([]*BuildNumber, error)
	// PutModules atomically replaces the run's module segment (modules,
	// their artifacts and their dependencies — one transaction, the
	// previous segment leaves by cascade). The parent run row must exist:
	// the FK rejects an orphan segment, the caller maps that to its
	// not-found semantics.
	PutModules(ctx context.Context, name, number, started, repo string, modules []*BuildModule) error
	// ListModules returns the run's module segment — modules ordered by
	// module_id, artifacts and dependencies by seq.
	ListModules(ctx context.Context, name, number, started, repo string) ([]*BuildModule, error)
	// PutProperties atomically replaces the run's property set (set
	// semantics: the previous set leaves whole).
	PutProperties(ctx context.Context, name, number, started, repo string, props []*BuildProperty) error
	// ListProperties returns the run's properties ordered by name.
	ListProperties(ctx context.Context, name, number, started, repo string) ([]*BuildProperty, error)
	// AppendPromotion appends one history row. There is deliberately NO
	// update or delete face: the history is append-only and the current
	// status is ListPromotions' first row.
	AppendPromotion(ctx context.Context, p *BuildPromotion) error
	// ListPromotions returns the run's history ordered by promoted_at DESC
	// (then id DESC for determinism) — the current status is row one.
	ListPromotions(ctx context.Context, name, number, started, repo string) ([]*BuildPromotion, error)
}

// BundleStore is the release-bundle record persistence seam (025, M17
// T-513 / ADR-0046 decision 1): the same dumb-ledger contract as
// BuildStore — the store validates nothing beyond the schema (the state
// CHECK and the pair/uniqueness keys), name/version legality and the
// conflict tri-state's interpretation live in the domain service. Rows
// round-trip as text/ints; the manifest segment is written whole.
type BundleStore interface {
	// InsertBundle writes a NEW bundle row with its whole manifest
	// segment in one transaction. A row already sitting at
	// (bundle_name, bundle_version) answers ErrBundleExists (the
	// UNIQUE-key fact; the caller evaluates the tri-state) — there is
	// deliberately no upsert: the manifest is immutable.
	InsertBundle(ctx context.Context, b *Bundle, items []*BundleItem) error
	// GetBundle returns the pair's row or wraps ErrBundleNotFound.
	GetBundle(ctx context.Context, name, version string) (*Bundle, error)
	// ListBundleNames returns one row per bundle name — version count
	// and newest created_at — ordered by name.
	ListBundleNames(ctx context.Context) ([]*BundleName, error)
	// ListBundleVersions returns every version of one name ordered by
	// created_at DESC (then version DESC for determinism).
	ListBundleVersions(ctx context.Context, name string) ([]*BundleVersion, error)
	// ReplaceItems atomically replaces the manifest segment and sets the
	// row's state/updated_* in the same transaction (the resume arm's
	// one write: the identity set is unchanged — same digest — only
	// snapshots and the derived state move). The parent row must exist:
	// a missing pair answers ErrBundleNotFound.
	ReplaceItems(ctx context.Context, name, version, state string, items []*BundleItem, updatedBy, updatedAt string) error
	// ListItems returns the manifest segment ordered by (repo_key, path)
	// — pending rows (sha256 '') included, they are the INPROGRESS face.
	ListItems(ctx context.Context, name, version string) ([]*BundleItem, error)
}

// Bundle is one row of bundles (025, M17 T-513 / ADR-0046): the versioned
// release RECORD — name, version, state and the signature placeholder.
// State values are the domain's closed set (COMPLETE/INPROGRESS); Signature
// carries the content digest (no signing chain exists in the minimal face —
// the digest IS the "same signature" comparison of the conflict tri-state);
// Description is the ADR skeleton's zero-cost BinFlow-native column (no M17
// wire face writes it). The type dimension (SOURCE/TARGET) is a code
// constant of the domain, not a column: the single instance keeps SOURCE
// records only (architecture §26.2).
type Bundle struct {
	Name        string // bundle_name
	Version     string // bundle_version, free-form (no SemVer enforcement)
	State       string // COMPLETE | INPROGRESS
	Signature   string // "sha256:<hex>" content digest of the item-identity set
	Description string
	CreatedBy   string
	CreatedAt   string
	UpdatedBy   string
	UpdatedAt   string
}

// BundleName is one row of the names projection: the name's version count
// and newest created_at — the GET /api/release/bundles face's source.
type BundleName struct {
	Name        string
	Versions    int
	LastCreated string
}

// BundleVersion is one row of the versions projection: every version of
// one name, newest first.
type BundleVersion struct {
	Version string
	State   string
	Created string
}

// BundleItem is one row of bundle_items: the manifest line as a NODES
// TIME-POINT SNAPSHOT. RepoKey/Path are the identity (the digest's input);
// Sha256/Size are frozen at resolution time (”/0 = PENDING — the artifact
// is not on this instance yet, the bundle's INPROGRESS face); the snapshot
// never re-resolves once set (a bundle is a release-moment record).
type BundleItem struct {
	ID      string // uuid minted by the writer
	RepoKey string
	Path    string
	Sha256  string
	Size    int64
	AddedAt string
	AddedBy string
}
