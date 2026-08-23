package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Validate checks every fail-fast rule (architecture section 8). Load calls
// it before returning; it is exported so assembly code (cmd) can re-check a
// Config it constructed by hand. Pure value checks run first; the only side
// effect (creating and probing storage.data_dir) runs last, so a config that
// fails for any other reason leaves no directory behind.
//
// Rules:
//   - server.listen parses as host:port with a numeric in-range port
//   - server.base_url, when set, is an absolute http(s) URL
//   - metadata.driver is one of sqlite|postgres (postgres is enum pass-through
//     only in M1; refusing to boot on it is cmd's job, FR-3-AC10 / T-16)
//   - metadata.dsn is validated per driver (postgres: URL; sqlite: optional
//     file path, no :memory:)
//   - durations and argon2 memory are positive
//   - logging.level and logging.format are within their enums
//   - storage.data_dir can be created and written
func (c *Config) Validate() error {
	if err := validatePort(c.Server.Listen); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := validateBaseURL(c.Server.BaseURL); err != nil {
		return err
	}
	if !allowedDrivers()[c.Metadata.Driver] {
		return fmt.Errorf("config: metadata.driver: unknown driver %q (want %q or %q)",
			c.Metadata.Driver, DriverSQLite, DriverPostgres)
	}
	if err := validateDSN(c.Metadata.Driver, c.Metadata.DSN); err != nil {
		return err
	}
	if c.Storage.SessionTTL <= 0 {
		return fmt.Errorf("config: storage.session_ttl_hours must be positive, got %s", c.Storage.SessionTTL)
	}
	if c.Storage.GCGrace <= 0 {
		return fmt.Errorf("config: storage.gc_grace_hours must be positive, got %s", c.Storage.GCGrace)
	}
	if c.Server.GracefulTimeout <= 0 {
		return fmt.Errorf("config: server.graceful_timeout_seconds must be positive, got %s", c.Server.GracefulTimeout)
	}
	if c.Auth.Argon2MemoryMB <= 0 {
		return fmt.Errorf("config: auth.argon2_memory_mb must be positive, got %d", c.Auth.Argon2MemoryMB)
	}
	// hash_concurrency is sentinel-defaulted: 0 keeps the auth service's
	// derived gate limit, so only negatives are nonsense. A non-positive
	// override would panic the WithHashConcurrency builder at assembly —
	// refusing the boot here keeps that panic a never-reached assertion.
	if c.Auth.HashConcurrency < 0 {
		return fmt.Errorf("config: auth.hash_concurrency must be a positive integer when set (0 = derived default), got %d",
			c.Auth.HashConcurrency)
	}
	if c.Auth.TokenDefaultTTL <= 0 {
		return fmt.Errorf("config: auth.token_default_ttl_hours must be positive, got %s", c.Auth.TokenDefaultTTL)
	}
	// A zero/negative cap would brick every non-admin token create (the Q11
	// guardrail demands 0 < ttl <= cap), so refuse the boot instead: raise the
	// cap or keep the default. Admins are never bound by it, but the value
	// still gates the self-service plane.
	if c.Auth.TokenNonAdminMaxTTL <= 0 {
		return fmt.Errorf("config: auth.token_nonadmin_max_ttl must be positive (seconds), got %s", c.Auth.TokenNonAdminMaxTTL)
	}
	// M7 step-up (ADR-0027 decision 4/6): the mint-grant TTL's domain is
	// [60, 3600] seconds, enforced UNCONDITIONALLY — with the switch on it
	// bounds the single-use re-auth window, and with the switch off the
	// operator has still spelled an intent a silent clamp would betray
	// (the strict-schema posture T-179 established for every auth key).
	if c.Auth.TokenStepUpGrantTTL < MinTokenStepUpGrantTTL || c.Auth.TokenStepUpGrantTTL > MaxTokenStepUpGrantTTL {
		return fmt.Errorf("config: auth.token_step_up_grant_ttl_seconds must be within [%d, %d] seconds, got %s",
			int(MinTokenStepUpGrantTTL.Seconds()), int(MaxTokenStepUpGrantTTL.Seconds()), c.Auth.TokenStepUpGrantTTL)
	}
	if c.Console.SessionTTL <= 0 {
		return fmt.Errorf("config: console.session_ttl_hours must be positive, got %s", c.Console.SessionTTL)
	}
	if !allowedLogLevels()[c.Logging.Level] {
		return fmt.Errorf("config: logging.level: unknown level %q (want debug, info, warn or error)", c.Logging.Level)
	}
	if !allowedLogFormats()[c.Logging.Format] {
		return fmt.Errorf("config: logging.format: unknown format %q (want json or console)", c.Logging.Format)
	}
	// Backend enum: disk (default) | s3.
	if c.Storage.Backend != StorageBackendDisk && c.Storage.Backend != StorageBackendS3 {
		return fmt.Errorf("config: storage.backend: unknown backend %q (want %q or %q)",
			c.Storage.Backend, StorageBackendDisk, StorageBackendS3)
	}
	// When backend=s3, require bucket, region, endpoint, access_key_id, and
	// secret_access_key; validate upload_part_size and upload_concurrency
	// are positive.
	if c.Storage.Backend == StorageBackendS3 {
		if c.Storage.S3.Bucket == "" {
			return fmt.Errorf("config: storage.s3.bucket is required when backend=s3")
		}
		if c.Storage.S3.Region == "" {
			return fmt.Errorf("config: storage.s3.region is required when backend=s3")
		}
		if c.Storage.S3.Endpoint == "" {
			return fmt.Errorf("config: storage.s3.endpoint is required when backend=s3")
		}
		if c.Storage.S3.AccessKeyID == "" {
			return fmt.Errorf("config: storage.s3.access_key_id is required when backend=s3")
		}
		if c.Storage.S3.SecretAccessKey == "" {
			return fmt.Errorf("config: storage.s3.secret_access_key is required when backend=s3 (set BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY)")
		}
		if c.Storage.S3.UploadPartSize <= 0 {
			return fmt.Errorf("config: storage.s3.upload_part_size must be positive, got %d", c.Storage.S3.UploadPartSize)
		}
		if c.Storage.S3.UploadConcurrency <= 0 {
			return fmt.Errorf("config: storage.s3.upload_concurrency must be positive, got %d", c.Storage.S3.UploadConcurrency)
		}
	}
	// Migration: when enabled, require concurrency >= 1.
	if c.Storage.Migration.Enabled {
		if c.Storage.Migration.Concurrency <= 0 {
			return fmt.Errorf("config: storage.migration.concurrency must be positive when migration is enabled, got %d", c.Storage.Migration.Concurrency)
		}
	}
	// auth.oidc (M6, ADR-0020): when enabled, the three wiring fields are
	// required and the two URLs must be absolute http(s). The client secret
	// is deliberately NOT required here — public (PKCE-only) clients are
	// legal, and the provider constructor is the deeper authority.
	if c.Auth.OIDC.Enabled {
		if c.Auth.OIDC.IssuerURL == "" {
			return fmt.Errorf("config: auth.oidc.issuer_url is required when auth.oidc.enabled=true")
		}
		if c.Auth.OIDC.ClientID == "" {
			return fmt.Errorf("config: auth.oidc.client_id is required when auth.oidc.enabled=true")
		}
		if c.Auth.OIDC.RedirectURL == "" {
			return fmt.Errorf("config: auth.oidc.redirect_url is required when auth.oidc.enabled=true")
		}
		if err := validateHTTPURL("auth.oidc.issuer_url", c.Auth.OIDC.IssuerURL); err != nil {
			return err
		}
		if err := validateHTTPURL("auth.oidc.redirect_url", c.Auth.OIDC.RedirectURL); err != nil {
			return err
		}
	}
	// auth.ldap (M6, ADR-0020): when enabled, url and base_dn are required,
	// the URL must carry the ldap/ldaps scheme, and the pool size must be
	// positive.
	if c.Auth.LDAP.Enabled {
		if c.Auth.LDAP.URL == "" {
			return fmt.Errorf("config: auth.ldap.url is required when auth.ldap.enabled=true")
		}
		if c.Auth.LDAP.BaseDN == "" {
			return fmt.Errorf("config: auth.ldap.base_dn is required when auth.ldap.enabled=true")
		}
		if err := validateLDAPURL(c.Auth.LDAP.URL); err != nil {
			return err
		}
		if c.Auth.LDAP.PoolSize <= 0 {
			return fmt.Errorf("config: auth.ldap.pool_size must be positive when auth.ldap.enabled=true, got %d", c.Auth.LDAP.PoolSize)
		}
	}
	// Filesystem probe last: everything above is pure, this creates the dir.
	// Only probe the data_dir when using disk backend; S3 backends do not need
	// a local data directory.
	if c.Storage.Backend == StorageBackendDisk || c.Storage.Backend == "" {
		if err := ensureDataDir(c.Storage.DataDir); err != nil {
			return fmt.Errorf("config: %w", err)
		}
	}
	return nil
}

// Defaults returns a Config with every field at its documented default. It is
// the same value Load falls back to and doubles as living documentation of
// the default set (architecture section 8).
func Defaults() *Config { return defaults() }

// validateHTTPURL checks that a config URL field is an absolute http(s) URL
// (the shape OIDC discovery and the login redirect demand).
func validateHTTPURL(key, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("config: %s: must be an absolute http(s) URL, got %q", key, raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("config: %s: scheme must be http or https, got %q", key, u.Scheme)
	}
	return nil
}

// validateLDAPURL checks that auth.ldap.url carries the ldap/ldaps scheme
// with a host (go-ldap's DialURL contract; a bare hostname would silently
// default to port 389 and the wrong TLS posture).
func validateLDAPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("config: auth.ldap.url: must be an ldap(s) URL like ldap://host:389, got %q", raw)
	}
	if u.Scheme != "ldap" && u.Scheme != "ldaps" {
		return fmt.Errorf("config: auth.ldap.url: scheme must be ldap or ldaps, got %q", u.Scheme)
	}
	return nil
}

// validateBaseURL: empty means "derive from request" and is fine; otherwise
// it must be an absolute http(s) URL without query or fragment.
func validateBaseURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("config: server.base_url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("config: server.base_url: scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("config: server.base_url: host is required, got %q", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("config: server.base_url: must not contain query or fragment, got %q", raw)
	}
	return nil
}

// validateDSN checks the DSN shape per driver. sqlite: empty means
// data_dir/binflow.db (resolved by the consumer); otherwise a plain file path
// only — SQLite URI spellings (file::memory:, file:x?mode=…) are rejected
// wholesale because an in-memory database would silently evaporate on restart
// (and split into per-connection private databases), and URI query parameters
// are a foot-gun the driver layer should not own. postgres: a postgres:// or
// postgresql:// URL, pass-through only in M1.
func validateDSN(driver, dsn string) error {
	if dsn == "" {
		return nil
	}
	switch driver {
	case DriverSQLite:
		// Whitelist semantics (review B2): a legal sqlite DSN is exactly
		// "empty" or "plain path". Everything with a scheme-ish prefix or a
		// query string — file::memory:, file:test.db, file::memory:?cache=shared,
		// :memory: — is rejected; matching spellings one by one would only
		// catch the ones we remembered.
		if strings.Contains(dsn, "?") || strings.Contains(dsn, ":") {
			return fmt.Errorf(
				"config: metadata.dsn: sqlite DSN must be a plain file path (no URI scheme, no query parameters); in-memory databases are not supported because data would not survive a restart, got %q",
				dsn)
		}
		return nil
	case DriverPostgres:
		u, err := url.Parse(dsn)
		if err != nil {
			// url.Error quotes the raw input, which may carry credentials.
			return errors.New("config: metadata.dsn: unparseable postgres URL")
		}
		if u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("config: metadata.dsn: postgres requires a URL like postgres://user@host:5432/binflow, got %s", redactDSN(dsn))
		}
		if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
			return fmt.Errorf("config: metadata.dsn: postgres DSN must start with postgres:// or postgresql://, got %s", redactDSN(dsn))
		}
		return nil
	default:
		return fmt.Errorf("config: metadata.dsn: cannot validate DSN for unknown driver %q", driver)
	}
}

// redactDSN strips the password from a DSN before it goes into an error
// message: Load errors end up in startup logs and a DSN legitimately carries
// credentials (review M1; the package's own NFR-S3 red line). Unparseable
// input is never echoed back raw.
func redactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "<unparseable DSN>"
	}
	if u.User != nil {
		u.User = url.User(u.User.Username()) // keep user, drop password
	}
	return u.String()
}
