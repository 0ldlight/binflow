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

// Store is the dialect-neutral entry to all metadata state. Migrations run
// automatically inside Open (ADR-0007); they are not part of this interface.
// Implementations must be safe for concurrent use (SQLite relies on WAL plus
// database/sql pool serialization, see store.go).
type Store interface {
	// Repos/Nodes/Blobs/Users/Tokens/Permissions/Audits return the
	// sub-stores sharing the same underlying handle.
	Repos() RepoStore
	Nodes() NodeStore
	Blobs() BlobStore
	Users() UserStore
	Tokens() TokenStore
	Permissions() PermissionStore
	Audits() AuditStore
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
