package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	yaml "go.yaml.in/yaml/v3"
)

// raw mirrors the YAML file schema (architecture section 8). Every scalar is a
// pointer so "absent" is distinguishable from "explicitly zero": absent fields
// fall back to defaults, explicit fields win, and the anonymous-access alias
// needs to know whether it was set at all. Unknown keys are rejected by the
// strict decoder, not silently dropped.
type raw struct {
	Server *struct {
		Listen                 *string  `yaml:"listen"`
		BaseURL                *string  `yaml:"base_url"`
		CORSOrigins            []string `yaml:"cors_origins"`
		GracefulTimeoutSeconds *int     `yaml:"graceful_timeout_seconds"`
	} `yaml:"server"`
	Storage *struct {
		DataDir         *string `yaml:"data_dir"`
		SessionTTLHours *int    `yaml:"session_ttl_hours"`
		GCGraceHours    *int    `yaml:"gc_grace_hours"`
	} `yaml:"storage"`
	Metadata *struct {
		Driver *string `yaml:"driver"`
		DSN    *string `yaml:"dsn"`
	} `yaml:"metadata"`
	Auth *struct {
		Argon2MemoryMB       *int  `yaml:"argon2_memory_mb"`
		TokenDefaultTTLHours *int  `yaml:"token_default_ttl_hours"`
		AnonymousRead        *bool `yaml:"anonymous_read"`
	} `yaml:"auth"`
	Security *struct {
		AnonymousAccess *bool `yaml:"anonymous_access"`
	} `yaml:"security"`
	Audit *struct {
		Enabled *bool `yaml:"enabled"`
	} `yaml:"audit"`
	Logging *struct {
		Level  *string `yaml:"level"`
		Format *string `yaml:"format"`
	} `yaml:"logging"`
}

// Load reads the YAML file at path, applies BINFLOW_-prefixed environment
// overrides, fills defaults, and validates the result. Any problem — missing
// file, malformed YAML, unknown key, bad enum, unwritable data_dir — is a
// returned error; the caller must refuse to start on it (fail-fast).
func Load(path string) (*Config, error) {
	src, err := os.ReadFile(path) // operator-provided config path by design (G304 excluded globally)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	r, err := decodeRaw(src)
	if err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return build(r, environ())
}

// decodeRaw unmarshals YAML strictly: unknown keys and duplicate keys are
// errors, the document must be a mapping (a null document counts as empty),
// and the stream must hold exactly one document — a second document would
// otherwise bypass both the secret scan and strict decoding (review B1).
func decodeRaw(src []byte) (*raw, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(src, &root); err != nil {
		return nil, err
	}
	r := &raw{}
	if len(root.Content) == 0 { // empty file: everything falls back to defaults
		return r, nil
	}
	doc := root.Content[0]
	isNull := doc.Tag == "!!null"
	if !isNull && doc.Kind != yaml.MappingNode {
		return nil, errors.New("config file must be a YAML mapping")
	}
	if !isNull {
		if err := rejectSecrets(doc); err != nil {
			return nil, err
		}
	}
	dec := yaml.NewDecoder(bytes.NewReader(src))
	dec.KnownFields(true)
	if err := dec.Decode(r); err != nil {
		return nil, err
	}
	// The strict decoder only consumed the first document; require the stream
	// to end there. Without this, anything after a "---" separator (secrets,
	// unknown keys, overriding values) would be silently dropped while still
	// looking like a successfully loaded config.
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("config file must contain exactly one YAML document")
	}
	return r, nil
}

// rejectSecrets scans the YAML mapping for secret-looking keys: at the top
// level and inside the security/auth/metadata sections. Secrets never enter
// the config file (ADR-0009): the admin password is BINFLOW_ADMIN_PASSWORD,
// env-only. Secret-ish spellings inside known sections would also be caught
// by strict decoding; this pass gives them a pointed error message instead
// of a bare "field not found".
func rejectSecrets(m *yaml.Node) error {
	for i := 0; i+1 < len(m.Content); i += 2 {
		keyNode, valNode := m.Content[i], m.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			continue
		}
		section := strings.ToLower(keyNode.Value)
		if isSecretYAMLKey("", section) {
			return secretYAMLErr(keyNode.Value)
		}
		if valNode.Kind != yaml.MappingNode {
			continue
		}
		if section != "security" && section != "auth" && section != "metadata" {
			continue
		}
		for j := 0; j+1 < len(valNode.Content); j += 2 {
			nested := valNode.Content[j]
			if nested.Kind != yaml.ScalarNode {
				continue
			}
			key := strings.ToLower(nested.Value)
			if isSecretYAMLKey(section, key) {
				return secretYAMLErr(section + "." + nested.Value)
			}
		}
	}
	return nil
}

// secretYAMLErr formats the "secret in YAML" error with the env escape hatch.
func secretYAMLErr(path string) error {
	return fmt.Errorf(
		"config: key %q looks like a secret; secrets must not be written into the YAML file — use the environment variable %s instead",
		path, SecretEnvVar)
}

// build resolves raw YAML plus environment into a Config with defaults
// applied. Env wins over YAML; YAML wins over defaults.
func build(r *raw, env map[string]string) (*Config, error) {
	c := defaults()

	if r.Server != nil {
		if r.Server.Listen != nil {
			c.Server.Listen = *r.Server.Listen
		}
		if r.Server.BaseURL != nil {
			c.Server.BaseURL = *r.Server.BaseURL
		}
		if r.Server.CORSOrigins != nil {
			c.Server.CORSOrigins = r.Server.CORSOrigins
		}
		if r.Server.GracefulTimeoutSeconds != nil {
			c.Server.GracefulTimeout = time.Duration(*r.Server.GracefulTimeoutSeconds) * time.Second
		}
	}
	if r.Storage != nil {
		if r.Storage.DataDir != nil {
			c.Storage.DataDir = *r.Storage.DataDir
		}
		if r.Storage.SessionTTLHours != nil {
			c.Storage.SessionTTL = time.Duration(*r.Storage.SessionTTLHours) * time.Hour
		}
		if r.Storage.GCGraceHours != nil {
			c.Storage.GCGrace = time.Duration(*r.Storage.GCGraceHours) * time.Hour
		}
	}
	if r.Metadata != nil {
		if r.Metadata.Driver != nil {
			c.Metadata.Driver = *r.Metadata.Driver
		}
		if r.Metadata.DSN != nil {
			c.Metadata.DSN = *r.Metadata.DSN
		}
	}
	if r.Auth != nil {
		if r.Auth.Argon2MemoryMB != nil {
			c.Auth.Argon2MemoryMB = *r.Auth.Argon2MemoryMB
		}
		if r.Auth.TokenDefaultTTLHours != nil {
			c.Auth.TokenDefaultTTL = time.Duration(*r.Auth.TokenDefaultTTLHours) * time.Hour
		}
	}
	if r.Audit != nil && r.Audit.Enabled != nil {
		c.Audit.Enabled = *r.Audit.Enabled
	}
	if r.Logging != nil {
		if r.Logging.Level != nil {
			c.Logging.Level = *r.Logging.Level
		}
		if r.Logging.Format != nil {
			c.Logging.Format = *r.Logging.Format
		}
	}

	// The anonymous toggle has two equivalent keys; resolve them with a
	// conflict check before env overrides apply on top of the merged value.
	anon, err := resolveAnonymous(r, DefaultAnonymousAccess)
	if err != nil {
		return nil, err
	}
	c.Security.AnonymousAccess = anon

	if err := applyEnv(c, env); err != nil {
		return nil, err
	}
	// AdminPassword is assigned inside applyEnv/setEnvValue together with
	// every other env value, so case-insensitive spellings behave uniformly
	// (review B3): reading env[SecretEnvVar] here with exact casing would let
	// binflow_admin_password silently vanish and the deployment fall back to
	// the documented default password.

	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// defaults returns the fully populated default configuration (architecture
// section 8): listen :8080, data_dir ./data, driver sqlite, anonymous_access
// true, 24h session TTL / GC grace, 30d token TTL, argon2 64MB, audit on,
// info/json logging.
func defaults() *Config {
	return &Config{
		Server: ServerConfig{
			Listen:          DefaultListen,
			BaseURL:         "",
			CORSOrigins:     []string{},
			GracefulTimeout: DefaultGracefulTimeout,
		},
		Storage: StorageConfig{
			DataDir:    DefaultDataDir,
			SessionTTL: DefaultTTL,
			GCGrace:    DefaultTTL,
		},
		Metadata: MetadataConfig{Driver: DefaultDriver, DSN: ""},
		Auth: AuthConfig{
			Argon2MemoryMB:  DefaultArgon2MemoryMB,
			TokenDefaultTTL: DefaultTokenTTL,
		},
		Security: SecurityConfig{AnonymousAccess: DefaultAnonymousAccess},
		Audit:    AuditConfig{Enabled: DefaultAuditEnabled},
		Logging:  LoggingConfig{Level: DefaultLogLevel, Format: DefaultLogFormat},
	}
}

// resolveAnonymous merges security.anonymous_access (primary, PRD spelling)
// with the architecture §8 alias auth.anonymous_read: unset falls back to def,
// a single set key wins, both set to the same value is fine, both set with
// different values is an error naming both keys.
func resolveAnonymous(r *raw, def bool) (bool, error) {
	var primary, alias *bool
	if r.Security != nil {
		primary = r.Security.AnonymousAccess
	}
	if r.Auth != nil {
		alias = r.Auth.AnonymousRead
	}
	switch {
	case primary == nil && alias == nil:
		return def, nil
	case primary != nil && alias == nil:
		return *primary, nil
	case primary == nil && alias != nil:
		return *alias, nil
	case *primary == *alias:
		return *primary, nil
	default:
		return false, errors.New(
			"config: security.anonymous_access and auth.anonymous_read are both set with different values; " +
				"they are equivalent — keep only security.anonymous_access")
	}
}

// environ snapshots the process environment into a map. Later entries win,
// matching os.Getenv semantics for duplicated keys.
func environ() map[string]string {
	out := map[string]string{}
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		out[name] = value
	}
	return out
}

// applyEnv walks BINFLOW_-prefixed variables in deterministic order, applies
// the known ones, and collects the unknown ones into a single error.
func applyEnv(c *Config, env map[string]string) error {
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	sort.Strings(names)

	var unknown []string
	for _, name := range names {
		upper := strings.ToUpper(name)
		if !strings.HasPrefix(upper, "BINFLOW_") || upper == "BINFLOW_" {
			continue // not ours
		}
		if upper == "BINFLOW_HOME" {
			continue // reserved for cmd (T-16): config/data dir resolution
		}
		path, kind, ok := splitEnvKey(strings.TrimPrefix(upper, "BINFLOW_"))
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		if err := setEnvValue(c, path, kind, env[name], name); err != nil {
			return err
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf(
			"config: unknown BINFLOW_ environment variable(s) %s; keys map as BINFLOW_<SECTION>__<KEY>, e.g. BINFLOW_METADATA__DRIVER",
			strings.Join(unknown, ", "))
	}
	return nil
}

// setEnvValue writes one parsed env value into the Config tree.
func setEnvValue(c *Config, path []string, kind envKind, value, name string) error {
	where := strings.Join(path, ".")
	switch kind {
	case envSecret:
		// Env-only secret (ADR-0009); every spelling of the variable name
		// funnels through this same case-insensitive path (review B3). An
		// empty value counts as unset. If several case variants of the name
		// coexist in the environment, applyEnv's sorted (C-locale) walk makes
		// the lexicographically-last variant win deterministically.
		if value != "" {
			c.AdminPassword = value
		}
		return nil
	case envBool:
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("config: %s: %w", name, err)
		}
		switch where {
		case "security.anonymous_access", "auth.anonymous_read":
			c.Security.AnonymousAccess = b
		case "audit.enabled":
			c.Audit.Enabled = b
		default:
			return fmt.Errorf("config: internal: bool path %q not wired", where)
		}
		return nil
	case envString:
		switch where {
		case "server.listen":
			c.Server.Listen = value
		case "server.base_url":
			c.Server.BaseURL = value
		case "storage.data_dir":
			c.Storage.DataDir = value
		case "metadata.driver":
			d := strings.ToLower(value)
			if !allowedDrivers()[d] {
				return fmt.Errorf("config: %s: unknown driver %q (want %q or %q)", name, value, DriverSQLite, DriverPostgres)
			}
			c.Metadata.Driver = d
		case "metadata.dsn":
			c.Metadata.DSN = value
		case "logging.level":
			l := strings.ToLower(value)
			if !allowedLogLevels()[l] {
				return fmt.Errorf("config: %s: unknown level %q (want debug, info, warn or error)", name, value)
			}
			c.Logging.Level = l
		case "logging.format":
			f := strings.ToLower(value)
			if !allowedLogFormats()[f] {
				return fmt.Errorf("config: %s: unknown format %q (want json or console)", name, value)
			}
			c.Logging.Format = f
		default:
			return fmt.Errorf("config: internal: string path %q not wired", where)
		}
		return nil
	case envIntPos:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("config: %s: %w", name, err)
		}
		if n <= 0 {
			return fmt.Errorf("config: %s: must be a positive integer, got %q", name, value)
		}
		switch where {
		case "server.graceful_timeout_seconds":
			c.Server.GracefulTimeout = time.Duration(n) * time.Second
		case "storage.session_ttl_hours":
			c.Storage.SessionTTL = time.Duration(n) * time.Hour
		case "storage.gc_grace_hours":
			c.Storage.GCGrace = time.Duration(n) * time.Hour
		case "auth.argon2_memory_mb":
			c.Auth.Argon2MemoryMB = n
		case "auth.token_default_ttl_hours":
			c.Auth.TokenDefaultTTL = time.Duration(n) * time.Hour
		default:
			return fmt.Errorf("config: internal: int path %q not wired", where)
		}
		return nil
	default:
		return fmt.Errorf("config: internal: unhandled env kind for %s", name)
	}
}

// parseBool accepts the usual Go booleans plus the YAML/shell spellings
// yes/no/on/off; an empty value counts as false (BINFLOW_X= is BINFLOW_X=0).
func parseBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "0", "f", "false", "n", "no", "off", "":
		return false, nil
	}
	return false, fmt.Errorf("invalid boolean %q", value)
}

// ensureDataDir fails fast when the data directory cannot be created or is
// not writable: refusing boot is better than 500-ing on every upload later.
// The SQLite path lands in the same directory (T-10), so one probe covers it.
//
// The probe is best-effort, not a guarantee: permissions or mount state can
// change between this check and actual use (TOCTOU), and the fixed probe
// filename is predictable — treat this as boot-time fail-fast hygiene, never
// as a security boundary. Concurrent Load calls are harmless: the probe is
// create-then-remove, so racing writers at worst observe a stale-file error
// for a file that is about to disappear.
func ensureDataDir(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create data_dir %s: %w", dir, err)
	}
	probe := filepath.Join(dir, ".config-write-probe")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		return fmt.Errorf("data_dir %s not writable: %w", dir, err)
	}
	if err := os.Remove(probe); err != nil {
		return fmt.Errorf("data_dir %s: remove write probe: %w", dir, err)
	}
	return nil
}

// validatePort accepts ":8080", "0.0.0.0:8080", "localhost:8080" (and the
// ephemeral ":0" used by tests); it rejects scheme prefixes, missing ports,
// and non-numeric or out-of-range ports.
func validatePort(listen string) error {
	if strings.Contains(listen, "://") {
		return fmt.Errorf("listen %q must not contain a scheme", listen)
	}
	_, portStr, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("listen %q: %w", listen, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("listen %q: non-numeric port %q", listen, portStr)
	}
	if port < 0 || port > 65535 {
		return fmt.Errorf("listen %q: port %d out of range", listen, port)
	}
	return nil
}
