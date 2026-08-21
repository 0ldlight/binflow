package config

import "time"

// Config is the immutable, fully resolved server configuration. Load fills in
// every default before returning it, so consumers never see zero values for
// behavior parameters (architecture section 8: "unlisted fields get no
// default", listed fields always do).
type Config struct {
	Server   ServerConfig
	Storage  StorageConfig
	Metadata MetadataConfig
	Auth     AuthConfig
	Security SecurityConfig
	Audit    AuditConfig
	Logging  LoggingConfig
	Console  ConsoleConfig

	// AdminPassword carries BINFLOW_ADMIN_PASSWORD (empty when unset). It is
	// env-only: the YAML schema rejects any key that looks like a secret.
	AdminPassword string
}

// ServerConfig is the HTTP listener surface.
type ServerConfig struct {
	Listen      string   // host:port, default ":8080"
	BaseURL     string   // externally visible URL; empty = derive from request
	CORSOrigins []string // empty = same-origin policy
	// GracefulTimeout bounds the shutdown drain window (architecture 7.4).
	GracefulTimeout time.Duration
}

// StorageBackend is the enum for the storage backend selection.
const (
	StorageBackendDisk = "disk"
	StorageBackendS3   = "s3"
)

// StorageConfig is the blob engine surface.
type StorageConfig struct {
	DataDir    string          // root for blobs/, sessions/, binflow.db (disk only)
	Backend    string          // "disk" (default) | "s3" — storage backend selection
	SessionTTL time.Duration   // expired upload sessions are reaped at startup
	GCGrace    time.Duration   // unreferenced blobs younger than this survive GC
	S3         S3Config        // S3 backend configuration (only used when Backend=s3)
	Migration  MigrationConfig // disk-to-S3 migration configuration
}

// S3Config holds the S3-compatible object storage configuration. The
// secret_access_key field is env-only (BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY)
// and must never appear in the YAML file. The bucket_prefix is an optional
// key prefix under which all blobs are stored (e.g. "binflow-prod").
type S3Config struct {
	Bucket            string // S3 bucket name (required)
	Region            string // AWS region or MinIO-compatible region (required)
	Endpoint          string // S3-compatible endpoint URL (required; e.g. https://s3.amazonaws.com)
	AccessKeyID       string // access key ID (required)
	SecretAccessKey   string // secret access key (env-only, never in YAML)
	UsePathStyle      bool   // use path-style addressing (true for MinIO)
	UploadPartSize    int64  // multipart upload part size in bytes (default 5 MiB)
	UploadConcurrency int    // number of concurrent part uploads (default 4)
	BucketPrefix      string // optional key prefix for all stored objects
}

// MigrationConfig holds the disk-to-S3 online migration configuration.
// When enabled, the storage layer enters dual-write mode: all writes go to both
// disk and S3, reads check S3 first with disk fallback. A background goroutine
// copies existing blobs from disk to S3. When completed, the migration flag
// switches to single-write (S3-only).
type MigrationConfig struct {
	Enabled     bool // enable dual-write mode and background migration
	Completed   bool // set to true after all blobs have been migrated; restart-safe
	Concurrency int  // background migration goroutine count (default 5)
}

// MetadataConfig selects the SQL driver. M1 ships sqlite; postgres is accepted
// here for enum pass-through only (startup refusal is T-16, FR-3-AC10).
type MetadataConfig struct {
	Driver string // "sqlite" | "postgres"
	DSN    string // sqlite: file path (empty = data_dir/binflow.db); postgres: URL
}

// AuthConfig carries password hashing and token lifetime parameters.
type AuthConfig struct {
	Argon2MemoryMB  int           // argon2id m parameter (64 = 64MB)
	TokenDefaultTTL time.Duration // default token lifetime
}

// SecurityConfig is the access posture. AnonymousAccess only ever opens
// content GET/HEAD; writes and /binflow/api/** always require auth
// (ADR-0009).
type SecurityConfig struct {
	AnonymousAccess bool
}

// AuditConfig toggles best-effort audit event appends.
type AuditConfig struct {
	Enabled bool
}

// LoggingConfig shapes the structured stdout log stream.
type LoggingConfig struct {
	Level  string // debug|info|warn|error
	Format string // json|console
}

// ConsoleConfig shapes the web console session plane (PRD FR-23, ADR-0014 as
// amended by the T-108 errata). The console itself is embedded static output
// with no knobs; the only behavior parameter is the browser session lifetime.
type ConsoleConfig struct {
	// SessionTTL is the lifetime of one web_sessions row: the absolute cap
	// stamped into expires_at at login AND the idle window that the
	// last_used heartbeat refreshes — the sliding renewal never extends a
	// session past the cap (ADR-0014 decision 2, erratum 3). Resolved from
	// console.session_ttl_hours (primary key, default 24) with
	// console.session_ttl_seconds as an override key for test granularity;
	// when both are set, seconds wins (PRD v1.1 R4 dual-key resolution).
	SessionTTL time.Duration
}
