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

// StorageConfig is the blob engine surface.
type StorageConfig struct {
	DataDir    string        // root for blobs/, sessions/, binflow.db
	SessionTTL time.Duration // expired upload sessions are reaped at startup
	GCGrace    time.Duration // unreferenced blobs younger than this survive GC
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
