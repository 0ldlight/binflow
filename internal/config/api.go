package config

import "time"

// Config is the immutable, fully resolved server configuration. Load fills in
// every default before returning it, so consumers never see zero values for
// behavior parameters (architecture section 8: "unlisted fields get no
// default", listed fields always do).
type Config struct {
	Server      ServerConfig
	Storage     StorageConfig
	Metadata    MetadataConfig
	Auth        AuthConfig
	Security    SecurityConfig
	Audit       AuditConfig
	Logging     LoggingConfig
	Console     ConsoleConfig
	Metrics     MetricsConfig
	Replication ReplicationConfig
	// Addons is the addon circuit-breaker section (M10 T-283, ADR-0032 /
	// architecture section 15.5 — the artifactory.addons.disabled behavior
	// pattern in BinFlow's own spelling).
	Addons AddonsConfig

	// AdminPassword carries BINFLOW_ADMIN_PASSWORD (empty when unset). It is
	// env-only: the YAML schema rejects any key that looks like a secret.
	AdminPassword string

	// StartupWarnings collects non-fatal configuration findings the loader
	// wants on the operator's console at boot (T-306, ADR-0036): the
	// compatibility-window hints of the binstore coexistence branches and the
	// ignored chain-scoped env leftovers. The loader itself never logs — the
	// cmd assembly drains this list through the real logger right after
	// construction, so the format and destination stay in one place.
	StartupWarnings []string
}

// AddonsConfig is the addons.disabled plane (M10 T-283, ADR-0032 / section
// 15.5): the operator's global breaker over the addon slots, including the
// five core package types (a core id in the list is a degradation knob —
// WARNed by the license Manager, honored anyway). Restart-effective: the
// value is consumed once at assembly (license.Manager's disabled set), so
// removing an entry and restarting restores everything; nothing is ever
// deleted by the breaker.
type AddonsConfig struct {
	// Disabled is the CSV of addon ids to switch off ("" = nothing
	// disabled). CSV, not a YAML list: one string spells the whole set in
	// every surface (YAML scalar and BINFLOW_ADDONS__DISABLED), matching
	// Artifactory's addons.disabled form. Values are trimmed by the
	// consumer; addon ids are case-sensitive registry keys.
	Disabled string
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
	DataDir    string        // root for blobs/, sessions/, binflow.db (disk only)
	Backend    string        // "disk" (default) | "s3" — storage backend selection
	SessionTTL time.Duration // expired upload sessions are reaped at startup
	GCGrace    time.Duration // unreferenced blobs younger than this survive GC
	// GCHoldTTL is how long an unreleased in-flight GC hold protects a blob
	// ([M9] ADR-0031: storage.gc_hold_ttl_seconds — the TTL backstop for a
	// Commit whose metadata rows never landed, crash or unwired consumer).
	// 0 = sentinel-default (the engine applies DefaultGCHoldTTL, 600s); a
	// configured value below MinGCHoldTTL (60s) refuses the boot.
	GCHoldTTL time.Duration
	S3        S3Config        // S3 backend configuration (only used when Backend=s3)
	Migration MigrationConfig // disk-to-S3 migration configuration
	// Chain is the resolved provider chain's self-describing label (T-306,
	// ADR-0036 decision 7): the ordered provider types, the chain-level
	// migration mode, and which source declared them. For a chain that came
	// from a binstore.yaml this is the DECLARED chain (a bypass chain keeps
	// its pre-declared s3 member visible); for the embedded spelling it is
	// the ASSEMBLED chain — what the boot actually runs. BinFlow has no
	// template layer, so there is no template name to report: the joined
	// provider list IS the storage-type label (the binarystore-2 decision
	// recorded in binstore.go; Artifactory's explicit-chain template value
	// was never located).
	Chain StorageChain
}

// StorageChain is the blob engine's provider-chain label (T-306, ADR-0036):
// one INFO line of honest observability, credentials excluded by
// construction (the secret is env-only and never part of any chain source).
type StorageChain struct {
	// Providers is the provider type list, declared order (filestore, s3).
	Providers []string
	// Mode is the chain-level migration mode: "", "bypass", "dual-write" or
	// "completed". It is non-empty only for the two-member chain.
	Mode string
	// Source is either ChainSourceEmbedded ("embedded") or the absolute
	// path of the binstore.yaml that declared the chain.
	Source string
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

// AuthConfig carries password hashing and token lifetime parameters, plus
// the two external identity-provider sections (M6, ADR-0020): auth.oidc and
// auth.ldap. Both default to disabled — an unconfigured boot keeps the
// pre-M6 local-only posture byte-for-byte.
type AuthConfig struct {
	Argon2MemoryMB  int           // argon2id m parameter (64 = 64MB)
	TokenDefaultTTL time.Duration // default token lifetime
	// HashConcurrency caps how many argon2id derivations (password
	// verifies and hashes) run at once across the process (T-204 / T-192:
	// each in-flight derivation costs ~64 MiB of transient heap, so this
	// is the operator's memory/CPU ceiling for authentication storms).
	// Key auth.hash_concurrency. The resolved value is a SENTINEL-DEFAULT:
	// 0 (unset) keeps the auth service's derived default — GOMAXPROCS
	// clamped to [1, 16] — so an unconfigured boot changes nothing; a
	// positive value is passed to auth Service.WithHashConcurrency at the
	// cmd assembly point. Explicit YAML 0 counts as unset; negative values
	// fail validation, and the env override
	// BINFLOW_AUTH__HASH_CONCURRENCY demands a positive integer like every
	// other int key.
	HashConcurrency int
	// TokenNonAdminMaxTTL caps the lifetime a NON-ADMIN caller may request
	// on POST /api/security/token (PRD M6 v1.2 Q11 guardrail 2 / K9,
	// T-188/T-190; mirrors Artifactory's
	// access.token.non.admin.max.expires.in). Key auth.token_nonadmin_max_ttl,
	// value in SECONDS (the same unit expires_in speaks), default 365d.
	// Admins are not bound by it (they may mint never-expiring tokens).
	TokenNonAdminMaxTTL time.Duration
	// TokenStepUp arms the second-factor gate on POST /api/security/token
	// (M7 FR-68 / ADR-0027, default false — an unconfigured boot keeps the
	// Q11 posture byte-for-byte, and enterprise deployments are advised to
	// enable it). When true, a NON-ADMIN web-session caller must present a
	// second credential: step_up_password for local/LDAP legs (argon2
	// verify / LDAP re-bind) or step_up_grant for the OIDC leg (single-use
	// mint grant from a prompt=login re-authentication). Basic, Bearer,
	// admin-session and /v2/token arms are exempt.
	TokenStepUp bool
	// TokenStepUpGrantTTL is the lifetime of one OIDC mint grant, in
	// seconds (auth.token_step_up_grant_ttl_seconds, default 300, domain
	// [60, 3600] enforced at boot). Grants are single-use and bound to
	// {username, session}; the ledger is in-process (ADR-0027 decision 4).
	TokenStepUpGrantTTL time.Duration
	OIDC                OIDCConfig // auth.oidc (disabled by default)
	LDAP                LDAPConfig // auth.ldap (disabled by default)
}

// OIDCConfig is the auth.oidc section (M6, ADR-0020). Key names mirror
// internal/auth's OIDCConfig and the Helm chart's config.oidc block (T-170).
// client_secret is env-only (BINFLOW_AUTH_OIDC_CLIENT_SECRET) and never
// appears in the YAML file; the strict schema and the secret scan both
// reject it there.
type OIDCConfig struct {
	Enabled       bool     // arm the OIDC Bearer/login flow (default false)
	IssuerURL     string   // issuer URL for .well-known/openid-configuration discovery
	ClientID      string   // OAuth2 client ID registered at the provider
	ClientSecret  string   // env-only secret, resolved from BINFLOW_AUTH_OIDC_CLIENT_SECRET
	RedirectURL   string   // callback URL (…/binflow/api/v1/oidc/callback)
	Scopes        []string // requested scopes; empty = provider default [openid, profile, email]
	UserClaim     string   // username claim; empty = preferred_username
	GroupClaim    string   // group claim; empty = groups
	AdminGroup    string   // group whose members are granted admin; empty = no mapping
	ReadOnlyGroup string   // group whose members are granted readonly_admin (M7, ADR-0026 decision 6); empty = no mapping
}

// LDAPConfig is the auth.ldap section (M6, ADR-0020). Key names mirror
// internal/auth's LDAPConfig and the Helm chart's config.ldap block (T-170).
// bind_password is env-only (BINFLOW_AUTH_LDAP_BIND_PASSWORD) and never
// appears in the YAML file. LDAP authenticates only at the login endpoint
// (no Bearer arm, ADR-0020).
type LDAPConfig struct {
	Enabled       bool   // arm the LDAP login fallback (default false)
	URL           string // ldap://host:389 or ldaps://host:636
	BaseDN        string // search base DN (e.g. dc=example,dc=com)
	BindDN        string // service-account DN for user search; empty = direct user bind
	BindPassword  string // env-only secret, resolved from BINFLOW_AUTH_LDAP_BIND_PASSWORD
	UserFilter    string // user search filter template (%s = username); empty = (uid=%s)
	UserIDAttr    string // attribute mapping to the BinFlow username; empty = uid
	GroupFilter   string // group search filter template; empty = no group search
	GroupBaseDN   string // group search base DN; empty = base_dn (PRD FR-55)
	GroupNameAttr string // attribute holding the group name; empty = cn
	AdminGroup    string // DN of a group whose members are granted admin; empty = no mapping
	ReadOnlyGroup string // DN of a group whose members are granted readonly_admin (M7, ADR-0026); empty = no mapping
	PoolSize      int    // idle connection pool size (default 5)
	StartTLS      bool   // StartTLS on ldap:// connections (ignored for ldaps://, WARN when both)
	SkipTLSVerify bool   // skip TLS certificate verification (default false; evaluation only, WARN when enabled)
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

// MetricsConfig shapes the /metrics exposure (T-163, ADR-0022 / PRD FR-61).
// The section follows the T-179 config-section pattern (OIDC/LDAP); it
// carries no secret-valued keys, so the secret scan is not extended.
type MetricsConfig struct {
	// RequireAuth demands a valid credential on GET /metrics. Default false:
	// the endpoint rides the /healthz-family anonymous posture (ADR-0022
	// exposure rule), and deployments that must not expose operational
	// metrics either set this or restrict the path at the reverse proxy.
	RequireAuth bool
}

// ReplicationConfig carries the push-replication behavior flags (T-210,
// ADR-0025 decision 4). It holds the explicit operator spelling of the
// replication engine's SSRF posture, which previously lived only as an implicit
// default (EngineOptions.DenyPrivateTargets zero value = private targets
// allowed). Every scalar mirrors the engine option 1:1 so assembly (cmd)
// can map it without translation.
type ReplicationConfig struct {
	// AllowPrivateTarget permits replication target URLs that resolve to
	// private (RFC 1918 / loopback / link-local) hosts. Default true — the
	// engine's DenyPrivateTargets defaulted to false because every realistic
	// replication target (datacenter-to-datacenter, same-host) is a private
	// address. Setting it false is an explicit opt-in to the tighter posture
	// (DenyPrivateTargets=true): the engine then rejects private target URLs
	// while STILL enforcing scheme/host sanity, per-hop re-screening and
	// DNS-rebinding pinning (ADR-0021). This key is not a bypass of the guard;
	// it only toggles the private-address leg of the SSRF screening list.
	AllowPrivateTarget bool
}
