package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeConfig writes a YAML body to a temp file and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "binflow.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// loadWithEnv loads a config body with extra environment variables applied
// for the duration of the test. The working directory moves to a temp dir so
// the default relative data_dir ("./data") never dirties the package tree.
func loadWithEnv(t *testing.T, body string, env map[string]string) (*Config, error) {
	t.Helper()
	t.Chdir(t.TempDir())
	for name, value := range env {
		t.Setenv(name, value)
	}
	return Load(writeConfig(t, body))
}

// mustLoad loads a config body that is expected to succeed.
func mustLoad(t *testing.T, body string, env map[string]string) *Config {
	t.Helper()
	c, err := loadWithEnv(t, body, env)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	return c
}

func TestLoadDefaultsOnEmptyFile(t *testing.T) {
	c := mustLoad(t, "", nil)

	if got, want := c.Server.Listen, DefaultListen; got != want {
		t.Errorf("Listen = %q, want %q", got, want)
	}
	if got, want := c.Storage.DataDir, DefaultDataDir; got != want {
		t.Errorf("DataDir = %q, want %q", got, want)
	}
	if got, want := c.Metadata.Driver, DriverSQLite; got != want {
		t.Errorf("Driver = %q, want %q", got, want)
	}
	if got, want := c.Metadata.DSN, ""; got != want {
		t.Errorf("DSN = %q, want %q", got, want)
	}
	if !c.Security.AnonymousAccess {
		t.Errorf("AnonymousAccess = false, want true (ADR-0009)")
	}
	if got, want := c.Storage.SessionTTL, 24*time.Hour; got != want {
		t.Errorf("SessionTTL = %s, want %s", got, want)
	}
	if got, want := c.Storage.GCGrace, 24*time.Hour; got != want {
		t.Errorf("GCGrace = %s, want %s", got, want)
	}
	if got, want := c.Server.GracefulTimeout, 30*time.Second; got != want {
		t.Errorf("GracefulTimeout = %s, want %s", got, want)
	}
	if got, want := c.Auth.Argon2MemoryMB, 64; got != want {
		t.Errorf("Argon2MemoryMB = %d, want %d", got, want)
	}
	if got, want := c.Auth.TokenDefaultTTL, 720*time.Hour; got != want {
		t.Errorf("TokenDefaultTTL = %s, want %s", got, want)
	}
	if !c.Audit.Enabled {
		t.Errorf("Audit.Enabled = false, want true")
	}
	if got, want := c.Logging.Level, "info"; got != want {
		t.Errorf("Level = %q, want %q", got, want)
	}
	if got, want := c.Logging.Format, "json"; got != want {
		t.Errorf("Format = %q, want %q", got, want)
	}
	if len(c.Server.CORSOrigins) != 0 {
		t.Errorf("CORSOrigins = %v, want empty", c.Server.CORSOrigins)
	}
	if c.AdminPassword != "" {
		t.Errorf("AdminPassword = %q, want empty when env unset", c.AdminPassword)
	}
}

func TestDefaultsMatchesEmptyLoad(t *testing.T) {
	t.Chdir(t.TempDir()) // Validate() probes the default data_dir
	d := Defaults()
	if got, want := d.Server.Listen, ":8080"; got != want {
		t.Errorf("Defaults().Listen = %q, want %q", got, want)
	}
	if !d.Security.AnonymousAccess || d.Metadata.Driver != DriverSQLite ||
		d.Storage.SessionTTL != 24*time.Hour || d.Storage.GCGrace != 24*time.Hour {
		t.Errorf("Defaults() = %+v, want the documented default set", d)
	}
}

func TestLoadFullYAML(t *testing.T) {
	c := mustLoad(t, `
server:
  listen: "127.0.0.1:9090"
  base_url: "https://artifacts.example.com/binflow"
  cors_origins: ["https://console.example.com", "https://ci.example.com"]
  graceful_timeout_seconds: 45
storage:
  data_dir: "`+t.TempDir()+`"
  session_ttl_hours: 12
  gc_grace_hours: 48
metadata:
  driver: sqlite
  dsn: "`+filepath.Join(t.TempDir(), "test.db")+`"
auth:
  argon2_memory_mb: 128
  token_default_ttl_hours: 24
security:
  anonymous_access: false
audit:
  enabled: false
logging:
  level: debug
  format: console
`, nil)

	if got, want := c.Server.Listen, "127.0.0.1:9090"; got != want {
		t.Errorf("Listen = %q, want %q", got, want)
	}
	if got, want := c.Server.BaseURL, "https://artifacts.example.com/binflow"; got != want {
		t.Errorf("BaseURL = %q, want %q", got, want)
	}
	if got, want := len(c.Server.CORSOrigins), 2; got != want {
		t.Errorf("CORSOrigins len = %d, want %d", got, want)
	}
	if got, want := c.Server.GracefulTimeout, 45*time.Second; got != want {
		t.Errorf("GracefulTimeout = %s, want %s", got, want)
	}
	if got, want := c.Storage.SessionTTL, 12*time.Hour; got != want {
		t.Errorf("SessionTTL = %s, want %s", got, want)
	}
	if got, want := c.Storage.GCGrace, 48*time.Hour; got != want {
		t.Errorf("GCGrace = %s, want %s", got, want)
	}
	if got, want := c.Metadata.DSN, filepath.Join(filepath.Dir(c.Metadata.DSN), "test.db"); got != want {
		t.Errorf("DSN = %q, want %q", got, want)
	}
	if got, want := c.Auth.Argon2MemoryMB, 128; got != want {
		t.Errorf("Argon2MemoryMB = %d, want %d", got, want)
	}
	if got, want := c.Auth.TokenDefaultTTL, 24*time.Hour; got != want {
		t.Errorf("TokenDefaultTTL = %s, want %s", got, want)
	}
	if c.Security.AnonymousAccess {
		t.Errorf("AnonymousAccess = true, want false")
	}
	if c.Audit.Enabled {
		t.Errorf("Audit.Enabled = true, want false")
	}
	if got, want := c.Logging.Level, "debug"; got != want {
		t.Errorf("Level = %q, want %q", got, want)
	}
	if got, want := c.Logging.Format, "console"; got != want {
		t.Errorf("Format = %q, want %q", got, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("Load(missing) error = nil, want error")
	}
	if !strings.Contains(err.Error(), "nope.yaml") {
		t.Errorf("error %q should mention the file path", err)
	}
	var perr *os.PathError
	if !errors.As(err, &perr) {
		t.Errorf("error %v should wrap *os.PathError", err)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "tabs", body: "server:\n\tlisten: :8080\n", want: "found character that cannot start any token"},
		{name: "unclosed quote", body: "server:\n  listen: \":8080\n", want: "found unexpected end of stream"},
		{name: "not a mapping", body: "- just\n- a\n- list\n", want: "must be a YAML mapping"},
		{name: "duplicate top key", body: "server:\n  listen: :1\nserver:\n  listen: :2\n", want: "already defined"},
		{name: "duplicate nested key", body: "logging:\n  level: info\n  level: debug\n", want: "already defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWithEnv(t, tt.body, nil)
			if err == nil {
				t.Fatalf("Load() error = nil, want one containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestLoadUnknownKeysRejected(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "unknown top-level", body: "securty:\n  anonymous_access: false\n", want: "field securty not found"},
		{name: "unknown nested", body: "logging:\n  lvl: debug\n", want: "field lvl not found"},
		{name: "unknown section key", body: "storage:\n  datadir: /tmp\n", want: "field datadir not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWithEnv(t, tt.body, nil)
			if err == nil {
				t.Fatalf("Load() error = nil, want one containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestLoadSecretsRejectedInYAML(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "top-level admin_password", body: "admin_password: hunter2\n"},
		{name: "top-level password", body: "password: hunter2\n"},
		{name: "top-level master_key", body: "master_key: cafebabe\n"},
		{name: "security.admin_password", body: "security:\n  admin_password: hunter2\n"},
		{name: "auth.password", body: "auth:\n  password: hunter2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWithEnv(t, tt.body, nil)
			if err == nil {
				t.Fatal("Load() error = nil, want secret-in-YAML error")
			}
			if !strings.Contains(err.Error(), "secret") {
				t.Errorf("error = %q, want it to mention secrets", err)
			}
			if !strings.Contains(err.Error(), SecretEnvVar) {
				t.Errorf("error = %q, want it to point at %s", err, SecretEnvVar)
			}
		})
	}
}

func TestLoadAdminPasswordEnvOnly(t *testing.T) {
	c := mustLoad(t, "", map[string]string{SecretEnvVar: "s3cr3t!"})
	if got, want := c.AdminPassword, "s3cr3t!"; got != want {
		t.Errorf("AdminPassword = %q, want %q", got, want)
	}
}

func TestLoadAnonymousAlias(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    bool
		wantErr bool
	}{
		{name: "primary only true", body: "security:\n  anonymous_access: true\n", want: true},
		{name: "primary only false", body: "security:\n  anonymous_access: false\n", want: false},
		{name: "alias only true", body: "auth:\n  anonymous_read: true\n", want: true},
		{name: "alias only false", body: "auth:\n  anonymous_read: false\n", want: false},
		{name: "both agree true", body: "security:\n  anonymous_access: true\nauth:\n  anonymous_read: true\n", want: true},
		{name: "both agree false", body: "security:\n  anonymous_access: false\nauth:\n  anonymous_read: false\n", want: false},
		{name: "conflict", body: "security:\n  anonymous_access: true\nauth:\n  anonymous_read: false\n", wantErr: true},
		{name: "conflict reversed", body: "security:\n  anonymous_access: false\nauth:\n  anonymous_read: true\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := loadWithEnv(t, tt.body, nil)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Load() error = nil, want alias conflict error")
				}
				for _, want := range []string{"anonymous_access", "anonymous_read"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error = %q, want it to mention %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}
			if got := c.Security.AnonymousAccess; got != tt.want {
				t.Errorf("AnonymousAccess = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	base := "server:\n  listen: :9000\nstorage:\n  data_dir: " + t.TempDir() + "\n"
	c := mustLoad(t, base, map[string]string{
		"BINFLOW_SERVER__LISTEN":            ":7070",
		"BINFLOW_METADATA__DRIVER":          "postgres",
		"BINFLOW_METADATA__DSN":             "postgres://binflow@example.db.local:5432/binflow",
		"BINFLOW_STORAGE__DATA_DIR":         t.TempDir(),
		"BINFLOW_SECURITY_ANONYMOUS_ACCESS": "false",
		"BINFLOW_AUTH__ARGON2_MEMORY_MB":    "256",
		"BINFLOW_LOGGING__LEVEL":            "warn",
	})
	if got, want := c.Server.Listen, ":7070"; got != want {
		t.Errorf("Listen = %q, want env value %q", got, want)
	}
	if got, want := c.Metadata.Driver, "postgres"; got != want {
		t.Errorf("Driver = %q, want %q", got, want)
	}
	if got, want := c.Metadata.DSN, "postgres://binflow@example.db.local:5432/binflow"; got != want {
		t.Errorf("DSN = %q, want %q", got, want)
	}
	if c.Security.AnonymousAccess {
		t.Errorf("AnonymousAccess = true, want false via env")
	}
	if got, want := c.Auth.Argon2MemoryMB, 256; got != want {
		t.Errorf("Argon2MemoryMB = %d, want %d", got, want)
	}
	if got, want := c.Logging.Level, "warn"; got != want {
		t.Errorf("Level = %q, want %q", got, want)
	}
}

func TestLoadEnvWinsOverYAML(t *testing.T) {
	c := mustLoad(t, "storage:\n  data_dir: "+t.TempDir()+"\nsecurity:\n  anonymous_access: true\n",
		map[string]string{"BINFLOW_SECURITY_ANONYMOUS_ACCESS": "false"})
	if c.Security.AnonymousAccess {
		t.Errorf("AnonymousAccess = true, want env override false")
	}
}

func TestLoadEnvAliasSpelling(t *testing.T) {
	c := mustLoad(t, "", map[string]string{"BINFLOW_AUTH__ANONYMOUS_READ": "false"})
	if c.Security.AnonymousAccess {
		t.Errorf("AnonymousAccess = true, want alias env override false")
	}
}

func TestLoadEnvCaseInsensitive(t *testing.T) {
	c := mustLoad(t, "", map[string]string{"binflow_logging__level": "DEBUG"})
	if got, want := c.Logging.Level, "debug"; got != want {
		t.Errorf("Level = %q, want %q (variable names are case-insensitive, enum values normalized)", got, want)
	}
	c2 := mustLoad(t, "", map[string]string{"Binflow_Metadata__Driver": "Postgres"})
	if got, want := c2.Metadata.Driver, "postgres"; got != want {
		t.Errorf("Driver = %q, want %q", got, want)
	}
}

func TestLoadEnvBoolSpellings(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "1", want: true},
		{value: "true", want: true},
		{value: "TRUE", want: true},
		{value: "yes", want: true},
		{value: "on", want: true},
		{value: "0", want: false},
		{value: "false", want: false},
		{value: "no", want: false},
		{value: "off", want: false},
		{value: "", want: false},
	}
	for _, tt := range tests {
		t.Run("anonymous_access="+tt.value, func(t *testing.T) {
			c := mustLoad(t, "", map[string]string{"BINFLOW_SECURITY_ANONYMOUS_ACCESS": tt.value})
			if got := c.Security.AnonymousAccess; got != tt.want {
				t.Errorf("AnonymousAccess = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadEnvInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "bad bool",
			env:  map[string]string{"BINFLOW_SECURITY_ANONYMOUS_ACCESS": "maybe"},
			want: `invalid boolean "maybe"`,
		},
		{
			name: "bad int",
			env:  map[string]string{"BINFLOW_AUTH__ARGON2_MEMORY_MB": "lots"},
			want: `invalid syntax`,
		},
		{
			name: "zero duration",
			env:  map[string]string{"BINFLOW_STORAGE__SESSION_TTL_HOURS": "0"},
			want: "positive",
		},
		{
			name: "negative duration",
			env:  map[string]string{"BINFLOW_STORAGE__GC_GRACE_HOURS": "-3"},
			want: "positive",
		},
		{
			name: "bad driver enum",
			env:  map[string]string{"BINFLOW_METADATA__DRIVER": "mysql"},
			want: `unknown driver "mysql"`,
		},
		{
			name: "bad logging level",
			env:  map[string]string{"BINFLOW_LOGGING__LEVEL": "verbose"},
			want: `unknown level "verbose"`,
		},
		{
			name: "bad logging format",
			env:  map[string]string{"BINFLOW_LOGGING__FORMAT": "syslog"},
			want: `unknown format "syslog"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWithEnv(t, "", tt.env)
			if err == nil {
				t.Fatalf("Load() error = nil, want one containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestLoadEnvUnknownRejected(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{name: "unknown section", env: map[string]string{"BINFLOW_SERVERS__LISTEN": ":1"}},
		{name: "unknown key", env: map[string]string{"BINFLOW_LOGGING__VERBOSITY": "3"}},
		{name: "list value attempt", env: map[string]string{"BINFLOW_SERVER__CORS_ORIGINS": "a,b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWithEnv(t, "", tt.env)
			if err == nil {
				t.Fatal("Load() error = nil, want unknown-variable error")
			}
			if !strings.Contains(err.Error(), "unknown BINFLOW_") {
				t.Errorf("error = %q, want unknown-variable error", err)
			}
		})
	}
}

func TestLoadEnvReservedIgnored(t *testing.T) {
	c := mustLoad(t, "", map[string]string{"BINFLOW_HOME": "/somewhere"})
	if got, want := c.Storage.DataDir, DefaultDataDir; got != want {
		t.Errorf("DataDir = %q, want %q (BINFLOW_HOME belongs to cmd)", got, want)
	}
}

func TestValidateRules(t *testing.T) {
	good := func(dir string) *Config {
		c := Defaults()
		c.Storage.DataDir = dir
		return c
	}
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name:    "listen missing port",
			mutate:  func(c *Config) { c.Server.Listen = "localhost" },
			wantErr: "missing port",
		},
		{
			name:    "listen scheme",
			mutate:  func(c *Config) { c.Server.Listen = "http://0.0.0.0:8080" },
			wantErr: "scheme",
		},
		{
			name:    "listen non-numeric port",
			mutate:  func(c *Config) { c.Server.Listen = ":http" },
			wantErr: "non-numeric port",
		},
		{
			name:    "listen port out of range",
			mutate:  func(c *Config) { c.Server.Listen = ":70000" },
			wantErr: "out of range",
		},
		{
			name:   "ephemeral port ok",
			mutate: func(c *Config) { c.Server.Listen = ":0" },
		},
		{
			name: "base_url bad scheme",
			mutate: func(c *Config) {
				c.Server.BaseURL = "ftp://example.com/binflow"
			},
			wantErr: "scheme",
		},
		{
			name: "base_url no host",
			mutate: func(c *Config) {
				c.Server.BaseURL = "http:///binflow"
			},
			wantErr: "host is required",
		},
		{
			name: "driver unknown",
			mutate: func(c *Config) {
				c.Metadata.Driver = "mysql"
			},
			wantErr: `unknown driver "mysql"`,
		},
		{
			name: "session ttl zero",
			mutate: func(c *Config) {
				c.Storage.SessionTTL = 0
			},
			wantErr: "session_ttl_hours must be positive",
		},
		{
			name: "gc grace negative",
			mutate: func(c *Config) {
				c.Storage.GCGrace = -time.Hour
			},
			wantErr: "gc_grace_hours must be positive",
		},
		{
			name: "graceful timeout zero",
			mutate: func(c *Config) {
				c.Server.GracefulTimeout = 0
			},
			wantErr: "graceful_timeout_seconds must be positive",
		},
		{
			name: "argon2 memory zero",
			mutate: func(c *Config) {
				c.Auth.Argon2MemoryMB = 0
			},
			wantErr: "argon2_memory_mb must be positive",
		},
		{
			name: "token ttl zero",
			mutate: func(c *Config) {
				c.Auth.TokenDefaultTTL = 0
			},
			wantErr: "token_default_ttl_hours must be positive",
		},
		{
			name: "log level bad",
			mutate: func(c *Config) {
				c.Logging.Level = "trace"
			},
			wantErr: `unknown level "trace"`,
		},
		{
			name: "log format bad",
			mutate: func(c *Config) {
				c.Logging.Format = "logfmt"
			},
			wantErr: `unknown format "logfmt"`,
		},
		{
			name: "sqlite :memory: dsn",
			mutate: func(c *Config) {
				c.Metadata.DSN = ":memory:"
			},
			wantErr: ":memory:",
		},
		{
			// Review B2: the bare ":memory:" guard must not be bypassable by
			// SQLite URI spellings without a query string.
			name: "sqlite file::memory: uri (review B2)",
			mutate: func(c *Config) {
				c.Metadata.DSN = "file::memory:"
			},
			wantErr: "plain file path",
		},
		{
			name: "sqlite file:test.db uri (review B2)",
			mutate: func(c *Config) {
				c.Metadata.DSN = "file:test.db"
			},
			wantErr: "plain file path",
		},
		{
			name: "sqlite file::memory:?cache=shared uri (review B2)",
			mutate: func(c *Config) {
				c.Metadata.DSN = "file::memory:?cache=shared"
			},
			wantErr: "plain file path",
		},
		{
			name: "postgres dsn not url",
			mutate: func(c *Config) {
				c.Metadata.Driver = DriverPostgres
				c.Metadata.DSN = "host=db user=binflow"
			},
			wantErr: "postgres",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			c := good(dir)
			tt.mutate(c)
			err := c.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() error = nil, want one containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePostgresEnumPassThrough(t *testing.T) {
	t.Chdir(t.TempDir())
	c := Defaults()
	c.Metadata.Driver = DriverPostgres
	c.Metadata.DSN = "postgres://binflow@example.db:5432/binflow?sslmode=disable"
	c.Storage.DataDir = t.TempDir()
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() with postgres driver error = %v, want nil (startup refusal is cmd's job, FR-3-AC10)", err)
	}
}

func TestValidateDataDirFailures(t *testing.T) {
	if runtimeIsWindows() {
		t.Skip("permission semantics differ on windows")
	}
	dir := t.TempDir()
	filePath := filepath.Join(dir, "plain-file")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		dataDir string
	}{
		{name: "data_dir under a plain file", dataDir: filepath.Join(filePath, "nested")},
		{name: "data_dir read-only parent", dataDir: readonlyDir(t, dir) + "/child"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Defaults()
			c.Storage.DataDir = tt.dataDir
			err := c.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want data_dir failure")
			}
			if !strings.Contains(err.Error(), "data_dir") {
				t.Errorf("error = %q, want it to mention data_dir", err)
			}
		})
	}
}

// readonlyDir drops write permission on a fresh copy and restores nothing
// (t.TempDir cleans up regardless).
func readonlyDir(t *testing.T, parent string) string {
	t.Helper()
	dir := filepath.Join(parent, "ro")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runtimeIsWindows() bool { return os.PathSeparator == '\\' }

func TestValidateDataDirCreatedAndWritable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "deep", "nested")
	c := Defaults()
	c.Storage.DataDir = dir
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat(data_dir): %v", err)
	}
	if !info.IsDir() {
		t.Errorf("data_dir %s is not a directory", dir)
	}
	// Probe must not linger.
	if _, err := os.Stat(filepath.Join(dir, ".config-write-probe")); !os.IsNotExist(err) {
		t.Errorf("write probe left behind in %s", dir)
	}
}

func TestLoadYAMLValidationFailures(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "bad listen in yaml",
			body:    "server:\n  listen: not-an-address\n",
			wantErr: "missing port",
		},
		{
			name:    "bad driver in yaml",
			body:    "metadata:\n  driver: oracle\n",
			wantErr: `unknown driver "oracle"`,
		},
		{
			name:    "bad log level in yaml",
			body:    "logging:\n  level: fatal\n",
			wantErr: `unknown level "fatal"`,
		},
		{
			name:    "zero ttl in yaml",
			body:    "storage:\n  session_ttl_hours: 0\n",
			wantErr: "session_ttl_hours must be positive",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWithEnv(t, tt.body, nil)
			if err == nil {
				t.Fatalf("Load() error = nil, want one containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestErrorMessagesPrefixedAndWrapped(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "gone.yaml"))
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.HasPrefix(err.Error(), "config:") {
		t.Errorf("error %q should start with config:", err)
	}
	// go.yaml.in/yaml/v3 errors must be wrapped so callers can inspect them.
	if !strings.Contains(fmt.Errorf("wrapped: %w", err).Error(), "gone.yaml") {
		t.Error("wrapped error lost the file path")
	}
}

func TestLoadEnvValueErrorNamesVariable(t *testing.T) {
	_, err := loadWithEnv(t, "", map[string]string{"BINFLOW_LOGGING__LEVEL": "loud"})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "BINFLOW_LOGGING__LEVEL") {
		t.Errorf("error %q should name the offending variable", err)
	}
}

// TestLoadRejectsMultiDocumentYAML pins review B1: everything after a
// document separator must not be silently dropped, whatever it carries —
// secrets, unknown keys, or values that would override doc 1.
func TestLoadRejectsMultiDocumentYAML(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "secret in doc 2",
			body:    "server:\n  listen: :9000\n---\nserver:\n  admin_password: hunter2\n",
			wantErr: "exactly one YAML document",
		},
		{
			name:    "override in doc 2",
			body:    "server:\n  listen: :9000\n---\nserver:\n  listen: :9999\n",
			wantErr: "exactly one YAML document",
		},
		{
			name:    "unknown key in doc 2",
			body:    "server:\n  listen: :9000\n---\nsecurty:\n  anonymous_access: false\n",
			wantErr: "exactly one YAML document",
		},
		{
			name:    "top-level secret in doc 2",
			body:    "server:\n  listen: :9000\n---\nadmin_password: top\n",
			wantErr: "exactly one YAML document",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWithEnv(t, tt.body, nil)
			if err == nil {
				t.Fatalf("Load() error = nil, want one containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestLoadNullDocumentActsAsEmpty pins review n2: a null document ("---" or
// "null") means "nothing configured", same as an empty file.
func TestLoadNullDocumentActsAsEmpty(t *testing.T) {
	for _, body := range []string{"---\n", "--- null\n", "null\n"} {
		c, err := loadWithEnv(t, body, nil)
		if err != nil {
			t.Fatalf("Load(%q) error = %v, want nil", body, err)
		}
		if got, want := c.Server.Listen, DefaultListen; got != want {
			t.Errorf("Load(%q).Listen = %q, want default %q", body, got, want)
		}
	}
}

// TestLoadAdminPasswordCaseVariants pins review B3: the admin password
// variable is matched case-insensitively like every other BINFLOW_ variable,
// so a lowercase spelling cannot silently fall back to the documented
// default password.
func TestLoadAdminPasswordCaseVariants(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "lowercase spelling works",
			env:  map[string]string{"binflow_admin_password": "T0pSecret"},
			want: "T0pSecret",
		},
		{
			name: "mixed-case spelling works",
			env:  map[string]string{"Binflow_Admin_Password": "MiXeD"},
			want: "MiXeD",
		},
		{
			name: "empty value counts as unset",
			env:  map[string]string{"BINFLOW_ADMIN_PASSWORD": ""},
			want: "",
		},
		{
			name: "several variants: sorted walk, last one wins",
			env: map[string]string{
				"binflow_admin_password": "lowercase",
				"BINFLOW_ADMIN_PASSWORD": "UPPERCASE",
			},
			// C-locale sort puts uppercase first, so the lowercase spelling
			// is applied last and wins; the point is that the outcome is
			// deterministic, not which one it is.
			want: "lowercase",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := loadWithEnv(t, "", tt.env)
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}
			if got := c.AdminPassword; got != tt.want {
				t.Errorf("AdminPassword = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPostgresDSNErrorDoesNotLeakPassword pins review M1: DSN validation
// errors must never echo credentials back into startup logs.
func TestPostgresDSNErrorDoesNotLeakPassword(t *testing.T) {
	const secret = "SuperSecret"
	tests := []struct {
		name string
		c    *Config
	}{
		{
			name: "missing host",
			c: &Config{
				Server:  ServerConfig{Listen: ":8080", GracefulTimeout: time.Second, CORSOrigins: []string{}},
				Storage: StorageConfig{DataDir: t.TempDir(), SessionTTL: time.Hour, GCGrace: time.Hour},
				Metadata: MetadataConfig{
					Driver: DriverPostgres,
					DSN:    "postgres://admin:" + secret + "@/binflow",
				},
				Auth:     AuthConfig{Argon2MemoryMB: 64, TokenDefaultTTL: time.Hour},
				Security: SecurityConfig{AnonymousAccess: true},
				Audit:    AuditConfig{Enabled: true},
				Logging:  LoggingConfig{Level: "info", Format: "json"},
			},
		},
		{
			name: "wrong scheme",
			c: &Config{
				Server:  ServerConfig{Listen: ":8080", GracefulTimeout: time.Second, CORSOrigins: []string{}},
				Storage: StorageConfig{DataDir: t.TempDir(), SessionTTL: time.Hour, GCGrace: time.Hour},
				Metadata: MetadataConfig{
					Driver: DriverPostgres,
					DSN:    "postgres+x://admin:" + secret + "@db:5432/binflow",
				},
				Auth:     AuthConfig{Argon2MemoryMB: 64, TokenDefaultTTL: time.Hour},
				Security: SecurityConfig{AnonymousAccess: true},
				Audit:    AuditConfig{Enabled: true},
				Logging:  LoggingConfig{Level: "info", Format: "json"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.c.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want DSN error")
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error %q leaks the password %q", err, secret)
			}
			if strings.Contains(err.Error(), "admin:"+secret) {
				t.Errorf("error %q leaks user:password", err)
			}
		})
	}
}

// TestValidateFailureLeavesNoDataDir pins review m4: the pure checks run
// before the filesystem probe, so a config rejected for a bad value must not
// leave a freshly created directory behind.
func TestValidateFailureLeavesNoDataDir(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "should-not-exist")

	c := Defaults()
	c.Server.Listen = "not-an-address" // fails before any fs probe
	c.Storage.DataDir = target
	if err := c.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want listen failure")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("data_dir %s was created despite validation failure", target)
	}
}

// TestLoadExplicitZeroValues pins review m5: the raw-pointer → Duration/int →
// validation chain on keys that previously were only covered via direct
// Config mutation.
func TestLoadExplicitZeroValues(t *testing.T) {
	t.Run("yaml graceful_timeout_seconds 0", func(t *testing.T) {
		_, err := loadWithEnv(t, "server:\n  graceful_timeout_seconds: 0\n", nil)
		if err == nil || !strings.Contains(err.Error(), "graceful_timeout_seconds must be positive") {
			t.Errorf("error = %v, want graceful_timeout_seconds positivity error", err)
		}
	})
	t.Run("yaml argon2_memory_mb 0", func(t *testing.T) {
		_, err := loadWithEnv(t, "auth:\n  argon2_memory_mb: 0\n", nil)
		if err == nil || !strings.Contains(err.Error(), "argon2_memory_mb must be positive") {
			t.Errorf("error = %v, want argon2_memory_mb positivity error", err)
		}
	})
	t.Run("env graceful timeout 0", func(t *testing.T) {
		_, err := loadWithEnv(t, "", map[string]string{"BINFLOW_SERVER__GRACEFUL_TIMEOUT_SECONDS": "0"})
		if err == nil || !strings.Contains(err.Error(), "BINFLOW_SERVER__GRACEFUL_TIMEOUT_SECONDS") {
			t.Errorf("error = %v, want it to name the variable", err)
		}
	})
	t.Run("env token ttl 0", func(t *testing.T) {
		_, err := loadWithEnv(t, "", map[string]string{"BINFLOW_AUTH__TOKEN_DEFAULT_TTL_HOURS": "0"})
		if err == nil || !strings.Contains(err.Error(), "BINFLOW_AUTH__TOKEN_DEFAULT_TTL_HOURS") {
			t.Errorf("error = %v, want it to name the variable", err)
		}
	})
	t.Run("yaml positive values accepted", func(t *testing.T) {
		c := mustLoad(t, "server:\n  graceful_timeout_seconds: 5\nauth:\n  argon2_memory_mb: 32\n", nil)
		if got, want := c.Server.GracefulTimeout, 5*time.Second; got != want {
			t.Errorf("GracefulTimeout = %s, want %s", got, want)
		}
		if got, want := c.Auth.Argon2MemoryMB, 32; got != want {
			t.Errorf("Argon2MemoryMB = %d, want %d", got, want)
		}
	})
}

// ---- S3 configuration tests (T-152) ----

func TestLoadS3Defaults(t *testing.T) {
	c := mustLoad(t, "", nil)
	if got, want := c.Storage.Backend, StorageBackendDisk; got != want {
		t.Errorf("Backend = %q, want %q", got, want)
	}
	if got, want := c.Storage.S3.UploadPartSize, DefaultS3UploadPartSize; got != want {
		t.Errorf("S3.UploadPartSize = %d, want %d", got, want)
	}
	if got, want := c.Storage.S3.UploadConcurrency, DefaultS3UploadConcurrency; got != want {
		t.Errorf("S3.UploadConcurrency = %d, want %d", got, want)
	}
	if c.Storage.S3.Bucket != "" {
		t.Errorf("S3.Bucket = %q, want empty", c.Storage.S3.Bucket)
	}
	if c.Storage.S3.SecretAccessKey != "" {
		t.Errorf("S3.SecretAccessKey = %q, want empty", c.Storage.S3.SecretAccessKey)
	}
}

func TestLoadS3FullYAML(t *testing.T) {
	c := mustLoad(t, `
storage:
  backend: s3
  s3:
    bucket: my-bucket
    region: us-east-1
    endpoint: https://s3.amazonaws.com
    access_key_id: AKIAIOSFODNN7EXAMPLE
    use_path_style: true
    upload_part_size: 10485760
    upload_concurrency: 8
    bucket_prefix: binflow-prod
`, map[string]string{
		"BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	})
	if got, want := c.Storage.Backend, "s3"; got != want {
		t.Errorf("Backend = %q, want %q", got, want)
	}
	if got, want := c.Storage.S3.Bucket, "my-bucket"; got != want {
		t.Errorf("S3.Bucket = %q, want %q", got, want)
	}
	if got, want := c.Storage.S3.Region, "us-east-1"; got != want {
		t.Errorf("S3.Region = %q, want %q", got, want)
	}
	if got, want := c.Storage.S3.Endpoint, "https://s3.amazonaws.com"; got != want {
		t.Errorf("S3.Endpoint = %q, want %q", got, want)
	}
	if got, want := c.Storage.S3.AccessKeyID, "AKIAIOSFODNN7EXAMPLE"; got != want {
		t.Errorf("S3.AccessKeyID = %q, want %q", got, want)
	}
	if got, want := c.Storage.S3.SecretAccessKey, "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"; got != want {
		t.Errorf("S3.SecretAccessKey = %q, want %q", got, want)
	}
	if !c.Storage.S3.UsePathStyle {
		t.Errorf("S3.UsePathStyle = false, want true")
	}
	if got, want := c.Storage.S3.UploadPartSize, int64(10485760); got != want {
		t.Errorf("S3.UploadPartSize = %d, want %d", got, want)
	}
	if got, want := c.Storage.S3.UploadConcurrency, 8; got != want {
		t.Errorf("S3.UploadConcurrency = %d, want %d", got, want)
	}
	if got, want := c.Storage.S3.BucketPrefix, "binflow-prod"; got != want {
		t.Errorf("S3.BucketPrefix = %q, want %q", got, want)
	}
}

func TestLoadS3EnvOverrides(t *testing.T) {
	base := "storage:\n  backend: s3\n  s3:\n    bucket: yaml-bucket\n    region: yaml-region\n    endpoint: https://yaml.example.com\n    access_key_id: YAMLKEY\n"
	c := mustLoad(t, base, map[string]string{
		"BINFLOW_STORAGE__BACKEND":                "s3",
		"BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY":    "env-secret-key",
		"BINFLOW_STORAGE__S3__BUCKET":             "env-bucket",
		"BINFLOW_STORAGE__S3__REGION":             "env-region",
		"BINFLOW_STORAGE__S3__ENDPOINT":           "https://env.example.com",
		"BINFLOW_STORAGE__S3__ACCESS_KEY_ID":      "ENVKEY",
		"BINFLOW_STORAGE__S3__USE_PATH_STYLE":     "true",
		"BINFLOW_STORAGE__S3__UPLOAD_PART_SIZE":   "20971520",
		"BINFLOW_STORAGE__S3__UPLOAD_CONCURRENCY": "16",
		"BINFLOW_STORAGE__S3__BUCKET_PREFIX":      "env-prefix",
	})
	if got, want := c.Storage.Backend, "s3"; got != want {
		t.Errorf("Backend = %q, want %q", got, want)
	}
	if got, want := c.Storage.S3.Bucket, "env-bucket"; got != want {
		t.Errorf("S3.Bucket = %q, want env value %q", got, want)
	}
	if got, want := c.Storage.S3.Region, "env-region"; got != want {
		t.Errorf("S3.Region = %q, want env value %q", got, want)
	}
	if got, want := c.Storage.S3.Endpoint, "https://env.example.com"; got != want {
		t.Errorf("S3.Endpoint = %q, want env value %q", got, want)
	}
	if got, want := c.Storage.S3.AccessKeyID, "ENVKEY"; got != want {
		t.Errorf("S3.AccessKeyID = %q, want env value %q", got, want)
	}
	if got, want := c.Storage.S3.SecretAccessKey, "env-secret-key"; got != want {
		t.Errorf("S3.SecretAccessKey = %q, want env value %q", got, want)
	}
	if !c.Storage.S3.UsePathStyle {
		t.Errorf("S3.UsePathStyle = false, want env override true")
	}
	if got, want := c.Storage.S3.UploadPartSize, int64(20971520); got != want {
		t.Errorf("S3.UploadPartSize = %d, want %d", got, want)
	}
	if got, want := c.Storage.S3.UploadConcurrency, 16; got != want {
		t.Errorf("S3.UploadConcurrency = %d, want %d", got, want)
	}
	if got, want := c.Storage.S3.BucketPrefix, "env-prefix"; got != want {
		t.Errorf("S3.BucketPrefix = %q, want env value %q", got, want)
	}
}

func TestLoadS3SecretAccessKeyEnvOnly(t *testing.T) {
	// Without the env var, S3 should fail validation (missing secret_access_key).
	_, err := loadWithEnv(t, `
storage:
  backend: s3
  s3:
    bucket: my-bucket
    region: us-east-1
    endpoint: https://s3.amazonaws.com
    access_key_id: AKIAIOSFODNN7EXAMPLE
`, nil)
	if err == nil {
		t.Fatal("Load() error = nil, want secret_access_key required error")
	}
	if !strings.Contains(err.Error(), "secret_access_key") {
		t.Errorf("error = %q, want it to mention secret_access_key", err)
	}

	// With the env var, it should succeed.
	c, err := loadWithEnv(t, `
storage:
  backend: s3
  s3:
    bucket: my-bucket
    region: us-east-1
    endpoint: https://s3.amazonaws.com
    access_key_id: AKIAIOSFODNN7EXAMPLE
`, map[string]string{"BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY": "secret!"})
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if got, want := c.Storage.S3.SecretAccessKey, "secret!"; got != want {
		t.Errorf("S3.SecretAccessKey = %q, want %q", got, want)
	}
}

func TestLoadS3SecretAccessKeyCaseInsensitive(t *testing.T) {
	c, err := loadWithEnv(t, `
storage:
  backend: s3
  s3:
    bucket: my-bucket
    region: us-east-1
    endpoint: https://s3.amazonaws.com
    access_key_id: AKIAIOSFODNN7EXAMPLE
`, map[string]string{"binflow_storage_s3_secret_access_key": "lowercase-secret"})
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if got, want := c.Storage.S3.SecretAccessKey, "lowercase-secret"; got != want {
		t.Errorf("S3.SecretAccessKey = %q, want %q", got, want)
	}
}

func TestLoadS3SecretAccessKeyRejectedInYAML(t *testing.T) {
	_, err := loadWithEnv(t, `
storage:
  backend: s3
  s3:
    bucket: my-bucket
    region: us-east-1
    endpoint: https://s3.amazonaws.com
    access_key_id: AKIAIOSFODNN7EXAMPLE
    secret_access_key: hunter2
`, nil)
	if err == nil {
		t.Fatal("Load() error = nil, want secret-access-key-rejected-in-YAML error")
	}
	if !strings.Contains(err.Error(), "secret") {
		t.Errorf("error = %q, want it to mention secrets", err)
	}
	if !strings.Contains(err.Error(), "secret_access_key") {
		t.Errorf("error = %q, want it to mention secret_access_key", err)
	}
}

func TestValidateS3BackendMissingFields(t *testing.T) {
	type testCase struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}
	tests := []testCase{
		{
			name:    "missing bucket",
			mutate:  func(c *Config) { c.Storage.S3.Bucket = "" },
			wantErr: "storage.s3.bucket is required",
		},
		{
			name:    "missing region",
			mutate:  func(c *Config) { c.Storage.S3.Region = "" },
			wantErr: "storage.s3.region is required",
		},
		{
			name:    "missing endpoint",
			mutate:  func(c *Config) { c.Storage.S3.Endpoint = "" },
			wantErr: "storage.s3.endpoint is required",
		},
		{
			name:    "missing access_key_id",
			mutate:  func(c *Config) { c.Storage.S3.AccessKeyID = "" },
			wantErr: "storage.s3.access_key_id is required",
		},
		{
			name:    "missing secret_access_key",
			mutate:  func(c *Config) { c.Storage.S3.SecretAccessKey = "" },
			wantErr: "storage.s3.secret_access_key is required",
		},
		{
			name:    "zero upload_part_size",
			mutate:  func(c *Config) { c.Storage.S3.UploadPartSize = 0 },
			wantErr: "storage.s3.upload_part_size must be positive",
		},
		{
			name:    "negative upload_part_size",
			mutate:  func(c *Config) { c.Storage.S3.UploadPartSize = -1 },
			wantErr: "storage.s3.upload_part_size must be positive",
		},
		{
			name:    "zero upload_concurrency",
			mutate:  func(c *Config) { c.Storage.S3.UploadConcurrency = 0 },
			wantErr: "storage.s3.upload_concurrency must be positive",
		},
		{
			name:    "negative upload_concurrency",
			mutate:  func(c *Config) { c.Storage.S3.UploadConcurrency = -1 },
			wantErr: "storage.s3.upload_concurrency must be positive",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validS3Config()
			tt.mutate(c)
			err := c.Validate()
			if err == nil {
				t.Fatalf("Validate() error = nil, want one containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateS3BackendValid(t *testing.T) {
	c := validS3Config()
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestValidateBackendUnknown(t *testing.T) {
	c := Defaults()
	c.Storage.Backend = "nfs"
	c.Storage.DataDir = t.TempDir()
	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want unknown backend error")
	}
	if !strings.Contains(err.Error(), "unknown backend") {
		t.Errorf("error = %q, want unknown backend error", err)
	}
	if !strings.Contains(err.Error(), "nfs") {
		t.Errorf("error = %q, want it to mention the bad value", err)
	}
}

func TestValidateS3SkipsDataDirCreation(t *testing.T) {
	// When backend=s3, Validate() should not require a writable data_dir.
	// Use a non-existent directory path to prove it's skipped.
	c := validS3Config()
	c.Storage.DataDir = "/nonexistent/should-not-be-created"
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil (S3 backend should skip data_dir creation)", err)
	}
}

// validS3Config returns a Config with S3 backend fully configured for validation tests.
func validS3Config() *Config {
	c := Defaults()
	c.Storage.Backend = "s3"
	c.Storage.S3.Bucket = "test-bucket"
	c.Storage.S3.Region = "us-east-1"
	c.Storage.S3.Endpoint = "https://s3.amazonaws.com"
	c.Storage.S3.AccessKeyID = "AKIAIOSFODNN7EXAMPLE"
	c.Storage.S3.SecretAccessKey = "secret"
	c.Storage.S3.UploadPartSize = DefaultS3UploadPartSize
	c.Storage.S3.UploadConcurrency = DefaultS3UploadConcurrency
	return c
}
