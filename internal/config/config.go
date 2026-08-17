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
	// DefaultArgon2MemoryMB is the argon2id m=64MB parameter.
	DefaultArgon2MemoryMB = 64
	// DefaultTokenTTL is the default API token lifetime (30 days).
	DefaultTokenTTL = 720 * time.Hour
	// DefaultAuditEnabled turns audit logging on.
	DefaultAuditEnabled = true
	// DefaultLogLevel and DefaultLogFormat shape structured logging.
	DefaultLogLevel  = "info"
	DefaultLogFormat = "json"
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
		"master_key", "masterkey", "admin_token", "admintoken":
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
	case "SECURITY_ANONYMOUS_ACCESS":
		return []string{"security", "anonymous_access"}, envBool, true
	case "DATA_DIR": // convenience alias for storage.data_dir
		return []string{"storage", "data_dir"}, envString, true
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
	case "metadata.driver":
		return parts, envString, true
	case "metadata.dsn":
		return parts, envString, true
	case "auth.argon2_memory_mb":
		return parts, envIntPos, true
	case "auth.token_default_ttl_hours":
		return parts, envIntPos, true
	case "security.anonymous_access", "auth.anonymous_read":
		return parts, envBool, true
	case "audit.enabled":
		return parts, envBool, true
	case "logging.level", "logging.format":
		return parts, envString, true
	}
	return nil, envUnsupported, false
}
