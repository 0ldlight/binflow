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
	if c.Auth.TokenDefaultTTL <= 0 {
		return fmt.Errorf("config: auth.token_default_ttl_hours must be positive, got %s", c.Auth.TokenDefaultTTL)
	}
	if !allowedLogLevels()[c.Logging.Level] {
		return fmt.Errorf("config: logging.level: unknown level %q (want debug, info, warn or error)", c.Logging.Level)
	}
	if !allowedLogFormats()[c.Logging.Format] {
		return fmt.Errorf("config: logging.format: unknown format %q (want json or console)", c.Logging.Format)
	}
	// Filesystem probe last: everything above is pure, this creates the dir.
	if err := ensureDataDir(c.Storage.DataDir); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}

// Defaults returns a Config with every field at its documented default. It is
// the same value Load falls back to and doubles as living documentation of
// the default set (architecture section 8).
func Defaults() *Config { return defaults() }

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
