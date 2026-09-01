package config

import (
	"strings"
	"time"
)

// Defaults (architecture section 8): behavior parameters ship safe defaults;
// only paths and ports need explicit attention.
const (
	// DefaultListen is the default server listen address.
	DefaultListen = ":8080"
	// DefaultDataDir is the default storage data directory.
	DefaultDataDir = "./data"
	// DefaultDriver is the default metadata driver.
	DefaultDriver = DriverSQLite
	// DefaultAnonymousAccess mirrors ADR-0009: content GET/HEAD is anonymously
	// readable unless explicitly disabled.
	DefaultAnonymousAccess = true
	// DefaultGracefulTimeout bounds in-flight work at shutdown.
	DefaultGracefulTimeout = 30 * time.Second
	// DefaultTTL applies to both upload session expiry and GC grace.
	DefaultTTL = 24 * time.Hour
	// DefaultGCHoldTTL and MinGCHoldTTL bound
	// storage.gc_hold_ttl_seconds ([M9] ADR-0031 / architecture section 8:
	// default 600, floor 60). The floor is enforced at boot — a hold TTL
	// shorter than the Commit-to-metadata-commit window would silently
	// re-open the W-1 race the hold set exists to close. The resolved Config
	// carries 0 for "unset" (the sentinel-default pattern of
	// auth.hash_concurrency): the storage engine maps 0 to the default, so
	// the two spellings of "not configured" stay one value.
	DefaultGCHoldTTL = 600 * time.Second
	MinGCHoldTTL     = 60 * time.Second
	// DefaultArgon2MemoryMB is the argon2id m=64MB parameter.
	DefaultArgon2MemoryMB = 64
	// DefaultTokenTTL is the default API token lifetime (30 days).
	DefaultTokenTTL = 720 * time.Hour
	// DefaultTokenNonAdminMaxTTL is the cap on the lifetime a non-admin
	// caller may request on POST /api/security/token (auth.token_nonadmin_max_ttl,
	// PRD M6 v1.2 Q11 guardrail 2 / K9). 365 days, the value Artifactory ships
	// as access.token.non.admin.max.expires.in.
	DefaultTokenNonAdminMaxTTL = 365 * 24 * time.Hour
	// DefaultAuditEnabled turns audit logging on.
	DefaultAuditEnabled = true
	// DefaultLogLevel and DefaultLogFormat shape structured logging.
	DefaultLogLevel  = "info"
	DefaultLogFormat = "json"
	// DefaultConsoleSessionTTL is the web console session lifetime
	// (console.session_ttl_hours, PRD FR-23 / ADR-0014 erratum 3).
	DefaultConsoleSessionTTL = 24 * time.Hour
	// DefaultStorageBackend is the default storage backend.
	DefaultStorageBackend = StorageBackendDisk
	// DefaultS3UploadPartSize is the default S3 multipart upload part size (5 MiB).
	DefaultS3UploadPartSize = int64(5 * 1024 * 1024)
	// DefaultS3UploadConcurrency is the default number of concurrent S3 upload parts.
	DefaultS3UploadConcurrency = 4
	// DefaultMigrationConcurrency is the default number of background migration goroutines.
	DefaultMigrationConcurrency = 5
	// DefaultLDAPPoolSize is the default idle LDAP connection pool size
	// (auth.ldap.pool_size; mirrors internal/auth LDAPConfig.defaults).
	DefaultLDAPPoolSize = 5
	// DefaultTokenStepUpGrantTTL is the default lifetime of one OIDC mint
	// grant (auth.token_step_up_grant_ttl_seconds, ADR-0027 decision 4):
	// 300 seconds — long enough for a browser to carry the grant from the
	// re-auth redirect into the mint request, short enough that the
	// single-use window closes quickly. The value domain is
	// [MinTokenStepUpGrantTTL, MaxTokenStepUpGrantTTL], enforced at boot.
	DefaultTokenStepUpGrantTTL = 300 * time.Second
	// MinTokenStepUpGrantTTL and MaxTokenStepUpGrantTTL bound
	// auth.token_step_up_grant_ttl_seconds (ADR-0027: domain [60, 3600]
	// seconds). Out-of-domain values refuse the boot regardless of the
	// auth.token_step_up switch — an operator spelling the key has stated
	// intent and a silently-clamped grant window would be a security
	// posture they never chose.
	MinTokenStepUpGrantTTL = 60 * time.Second
	MaxTokenStepUpGrantTTL = 3600 * time.Second
	// DefaultAllowPrivateTarget is the default for replication.allow_private_target
	// (T-210 / ADR-0025 decision 4): true keeps the pre-config behavior where the
	// replication engine's DenyPrivateTargets defaulted to false — every realistic
	// replication target resolves to a private host (datacenter-to-datacenter,
	// same-host). This key is the explicit operator spelling of that SSRF surface:
	// setting it false flips DenyPrivateTargets to true and rejects private target
	// URLs, while the guard still enforces scheme/host sanity, per-hop re-screening
	// and DNS-rebinding pinning (ADR-0021). The default is deliberately permissive
	// so existing deployments that replicate to private targets do not change.
	DefaultAllowPrivateTarget = true
	// The folder_download limit defaults (M13 T-368 / FR-118.1) are the
	// repo-operations.md section 2.1 spec column — the same numbers
	// internal/repo's DefaultFolderDownloadConfig ships, spelled here so
	// the loader's default and the consumer's default stay independently
	// checkable (the T-368 httpapi leg pins the equality; an unconfigured
	// boot is byte-for-byte the M12 as-built).
	DefaultFolderDownloadMaxSizeMb             int64 = 1024
	DefaultFolderDownloadMaxFiles              int   = 5000
	DefaultFolderDownloadMaxConcurrentRequests int   = 10
	// DefaultTrashcanRetentionDays (M13 T-368 / FR-118.2) is the M12
	// as-built retention window: config.xml trashcanConfig's
	// retentionPeriodDays=14 (storage-layout.md section 5), the value the
	// retention cron ran on before the knob existed — the default-unchanged
	// red line. Mirrors repo.TrashDefaultRetentionDays (this package cannot
	// import internal/repo; the equality is pinned by the T-368 httpapi leg).
	DefaultTrashcanRetentionDays = 14
)

// Metadata driver enum (architecture section 8; postgres M1 enum-only).
const (
	DriverSQLite   = "sqlite"
	DriverPostgres = "postgres"
)

// SecretEnvVar is the name of the admin bootstrap password environment
// variable. It is deliberately excluded from the YAML schema: secrets never
// enter the config file (ADR-0009).
const SecretEnvVar = "BINFLOW_ADMIN_PASSWORD" //nolint:gosec // identifier of an env variable, not a credential value

// S3SecretEnvVar is the name of the S3 secret access key environment variable.
// Like the admin password, it is env-only and never appears in YAML.
const S3SecretEnvVar = "BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY" //nolint:gosec // identifier of an env variable, not a credential value

// OIDCClientSecretEnvVar is the name of the OIDC client secret environment
// variable (auth.oidc, M6/ADR-0020). Env-only, never in YAML.
const OIDCClientSecretEnvVar = "BINFLOW_AUTH_OIDC_CLIENT_SECRET" //nolint:gosec // identifier of an env variable, not a credential value

// LDAPBindPasswordEnvVar is the name of the LDAP bind password environment
// variable (auth.ldap, M6/ADR-0020). Env-only, never in YAML.
const LDAPBindPasswordEnvVar = "BINFLOW_AUTH_LDAP_BIND_PASSWORD" //nolint:gosec // identifier of an env variable, not a credential value

// allowedDrivers is the accepted metadata.driver enum.
func allowedDrivers() map[string]bool {
	return map[string]bool{DriverSQLite: true, DriverPostgres: true}
}

// allowedLogLevels is the accepted logging.level enum.
func allowedLogLevels() map[string]bool {
	return map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
}

// allowedLogFormats is the accepted logging.format enum.
func allowedLogFormats() map[string]bool {
	return map[string]bool{"json": true, "console": true}
}

// isSecretYAMLKey reports whether a lower-cased YAML key denotes a secret.
// section is the lowercase top-level section ("", "security", "auth", ...).
// Known secret spellings are rejected wherever they appear, so a misplaced
// password fails fast with a pointed message instead of silently not working.
// metadata.dsn is special: it is a legitimate (non-secret-graded) config key
// inside this package, but see the package doc for its logging red line.
func isSecretYAMLKey(section, key string) bool {
	switch key {
	case "admin_password", "adminpassword", "password", "secret",
		"master_key", "masterkey", "admin_token", "admintoken",
		"client_secret", "clientsecret", "bind_password", "bindpassword":
		return true
	case "dsn":
		// A DSN may embed credentials; only reject the obvious misspelling at
		// the top level (metadata.dsn is the one legal spelling).
		return section == ""
	}
	return false
}

// envKind classifies an overridable config scalar for env parsing.
type envKind int

const (
	envUnsupported envKind = iota // list-valued or unknown: rejected
	envSecret                     // BINFLOW_ADMIN_PASSWORD: env-only secret
	envBool
	envString
	envIntPos // positive integer (durations arrive as *_hours/_seconds)
)

// splitEnvKey maps an environment variable name (BINFLOW_ prefix already
// stripped) to a config path. It returns ok=false for anything that is not a
// known overridable scalar, including list-valued keys and reserved names.
func splitEnvKey(upper string) (path []string, kind envKind, ok bool) {
	switch upper {
	case "ADMIN_PASSWORD":
		return nil, envSecret, true
	case "STORAGE_S3_SECRET_ACCESS_KEY":
		return nil, envSecret, true
	case "AUTH_OIDC_CLIENT_SECRET":
		return nil, envSecret, true
	case "AUTH_LDAP_BIND_PASSWORD":
		return nil, envSecret, true
	case "SECURITY_ANONYMOUS_ACCESS":
		return []string{"security", "anonymous_access"}, envBool, true
	case "DATA_DIR": // convenience alias for storage.data_dir
		return []string{"storage", "data_dir"}, envString, true
	case "AUTH_TOKEN_NONADMIN_MAX_TTL":
		// K9's documented single-underscore spelling; the generic
		// BINFLOW_AUTH__TOKEN_NONADMIN_MAX_TTL form maps through the "__"
		// path below.
		return []string{"auth", "token_nonadmin_max_ttl"}, envIntPos, true
	case "AUTH_TOKEN_STEP_UP":
		// M7 (ADR-0027 decision 6): the step-up switch's documented
		// single-underscore spelling; the generic
		// BINFLOW_AUTH__TOKEN_STEP_UP form maps through the "__" path below.
		return []string{"auth", "token_step_up"}, envBool, true
	case "AUTH_TOKEN_STEP_UP_GRANT_TTL_SECONDS":
		// Same dual-spelling rule for the mint-grant TTL (ADR-0027
		// decision 6); the domain check lives in Validate.
		return []string{"auth", "token_step_up_grant_ttl_seconds"}, envIntPos, true
	case "REPLICATION_ALLOW_PRIVATE_TARGET":
		// T-210's documented single-underscore spelling; the generic
		// BINFLOW_REPLICATION__ALLOW_PRIVATE_TARGET form maps through the "__"
		// path below.
		return []string{"replication", "allow_private_target"}, envBool, true
	case "REPLICATION_BLOCK_PUSH", "REPLICATION_BLOCK_PULL":
		// M15 T-422 (FR-138.3, §9.2-B): the global block brake's boot
		// carriers, same single-underscore reachability as the SSRF twin
		// above; the generic BINFLOW_REPLICATION__BLOCK_* form maps through
		// the "__" path below.
		return []string{"replication", strings.ToLower(strings.TrimPrefix(upper, "REPLICATION_"))}, envBool, true
	case "WEBHOOK_ALLOW_PRIVATE_TARGET":
		// M13 (ADR-0041 decision 6): the webhook SSRF toggle's
		// single-underscore spelling, mirroring the replication twin above;
		// the generic BINFLOW_WEBHOOK__ALLOW_PRIVATE_TARGET form maps
		// through the "__" path below.
		return []string{"webhook", "allow_private_target"}, envBool, true
	case "AUTH_OIDC_READONLY_GROUP":
		// M7 (ADR-0026 decision 4): the documented single-underscore
		// spelling; the generic BINFLOW_AUTH__OIDC__READONLY_GROUP form maps
		// through the "__" path below.
		return []string{"auth", "oidc", "readonly_group"}, envString, true
	case "AUTH_LDAP_READONLY_GROUP":
		return []string{"auth", "ldap", "readonly_group"}, envString, true
	}
	parts := strings.Split(upper, "__")
	for i, p := range parts {
		parts[i] = strings.ToLower(p)
	}
	switch strings.Join(parts, ".") {
	case "server.listen", "server.base_url":
		return parts, envString, true
	case "server.graceful_timeout_seconds":
		return parts, envIntPos, true
	case "server.cors_origins":
		return nil, envUnsupported, false // lists are YAML-only
	case "storage.data_dir":
		return parts, envString, true
	case "storage.session_ttl_hours", "storage.gc_grace_hours":
		return parts, envIntPos, true
	case "storage.gc_hold_ttl_seconds":
		// [M9] ADR-0031: same reachability as the YAML key; the 60s floor is
		// Validate's (an out-of-domain override refuses the boot).
		return parts, envIntPos, true
	case "storage.backend":
		return parts, envString, true
	case "storage.s3.bucket", "storage.s3.region", "storage.s3.endpoint",
		"storage.s3.access_key_id", "storage.s3.bucket_prefix":
		return parts, envString, true
	case "storage.s3.use_path_style":
		return parts, envBool, true
	case "storage.s3.upload_part_size":
		return parts, envIntPos, true
	case "storage.s3.upload_concurrency":
		return parts, envIntPos, true
	case "storage.migration.enabled", "storage.migration.completed":
		return parts, envBool, true
	case "storage.migration.concurrency":
		return parts, envIntPos, true
	case "metadata.driver":
		return parts, envString, true
	case "metadata.dsn":
		return parts, envString, true
	case "auth.oidc.readonly_group", "auth.ldap.readonly_group":
		// M7 (ADR-0026 decision 6): the federated read-only-admin group
		// mapping, same reachability as its YAML key.
		return parts, envString, true
	case "auth.argon2_memory_mb":
		return parts, envIntPos, true
	case "auth.hash_concurrency":
		return parts, envIntPos, true
	case "auth.token_default_ttl_hours":
		return parts, envIntPos, true
	case "auth.token_nonadmin_max_ttl":
		return parts, envIntPos, true
	case "auth.token_step_up":
		// M7 (ADR-0027): the generic BINFLOW_AUTH__TOKEN_STEP_UP spelling.
		return parts, envBool, true
	case "auth.token_step_up_grant_ttl_seconds":
		// M7 (ADR-0027): the generic
		// BINFLOW_AUTH__TOKEN_STEP_UP_GRANT_TTL_SECONDS spelling.
		return parts, envIntPos, true
	case "console.session_ttl_hours", "console.session_ttl_seconds":
		return parts, envIntPos, true
	case "metrics.require_auth":
		return parts, envBool, true
	case "replication.allow_private_target", "webhook.allow_private_target",
		"replication.block_push", "replication.block_pull":
		return parts, envBool, true
	case "folder_download.enabled", "folder_download.enabled_for_anonymous",
		"folder_download.enabled_empty_directories":
		// M13 T-368 / FR-118.1: the folder_download switches, generic
		// double-underscore form (BINFLOW_FOLDER_DOWNLOAD__ENABLED…).
		return parts, envBool, true
	case "folder_download.max_download_size_mb", "folder_download.max_files",
		"folder_download.max_concurrent_requests":
		// The three limits; positive integers (the YAML spelling alone can
		// express 0 = unlimited, the hash_concurrency rule).
		return parts, envIntPos, true
	case "trashcan.retention_days":
		// M13 T-368 / FR-118.2: the retention window (BINFLOW_TRASHCAN__RETENTION_DAYS);
		// 0-as-default is a YAML-only spelling for the same reason.
		return parts, envIntPos, true
	case "addons.disabled":
		// M10 T-283 (ADR-0032 / section 15.5): the circuit-breaker CSV, same
		// reachability as its YAML key (BINFLOW_ADDONS__DISABLED).
		return parts, envString, true
	case "security.anonymous_access", "auth.anonymous_read":
		return parts, envBool, true
	case "audit.enabled":
		return parts, envBool, true
	case "logging.level", "logging.format":
		return parts, envString, true
	}
	return nil, envUnsupported, false
}
