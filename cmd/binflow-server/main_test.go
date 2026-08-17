package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// TestRunHelpMatrix covers the argument-dispatch surface (AC 5): no
// arguments and every help spelling print usage and exit 0, --version
// prints the banner, unknown commands fail with a pointed message.
func TestRunHelpMatrix(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no args prints usage", args: nil},
		{name: "long help flag", args: []string{"--help"}},
		{name: "short help flag", args: []string{"-h"}},
		{name: "help command", args: []string{"help"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run(tt.args, &stdout, &stderr); err != nil {
				t.Fatalf("run(%v) error = %v, want nil", tt.args, err)
			}
			if got := stdout.String(); !strings.Contains(got, "Usage:") {
				t.Errorf("stdout = %q, want it to contain usage", got)
			}
		})
	}
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(--version) error = %v, want nil", err)
	}
	if got := stdout.String(); !strings.Contains(got, "binflow-server "+version) {
		t.Errorf("stdout = %q, want it to contain the version banner", got)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"frobnicate"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("run(frobnicate) error = %v, want unknown command", err)
	}
}

// TestServeMissingConfigFileFailFast: -c pointing at a nonexistent file is
// an operator statement; the server must refuse to boot rather than fall
// back to defaults (AC 5).
func TestServeMissingConfigFileFailFast(t *testing.T) {
	var stdout, stderr bytes.Buffer
	missing := filepath.Join(t.TempDir(), "no-such-binflow.yaml")
	err := run([]string{"serve", "-c", missing}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(serve -c <missing>) error = nil, want a config error")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error = %v, want it to name the missing path %s", err, missing)
	}
}

// TestServeFlagErrors: flag mistakes and stray positionals are usage
// errors, not server boots.
func TestServeFlagErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown flag", args: []string{"serve", "--frob"}, want: "flag provided but not defined"},
		{name: "stray positional", args: []string{"serve", "extra.yaml"}, want: "unexpected argument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tt.args, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("run(%v) error = %v, want it to contain %q", tt.args, err, tt.want)
			}
		})
	}
}

// TestGCFlagMatrix covers the gc subcommand's argument surface (AC 2/5):
// dry-run default, --apply, --grace-days override, and the rejections. The
// data directory is empty, so every run reports zero candidates — the flag
// plumbing is the subject here; TestGCTwoModesOnRealStore covers behavior.
func TestGCFlagMatrix(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, map[string]string{
		"BINFLOW_HOME":              "",
		"BINFLOW_ADMIN_PASSWORD":    "test-admin-pw",
		"BINFLOW_STORAGE__DATA_DIR": filepath.Join(dir, "data"),
	})
	restore := chdirTemp(t)
	defer restore()

	tests := []struct {
		name    string
		args    []string
		wantErr string // "" means success
		wantOut []string
	}{
		{
			name:    "dry-run is the default posture",
			args:    []string{"gc"},
			wantOut: []string{"mode=dry-run", "candidates=0"},
		},
		{
			name:    "apply is opt-in",
			args:    []string{"gc", "--apply"},
			wantOut: []string{"mode=apply", "candidates=0"},
		},
		{
			name:    "grace-days override is echoed",
			args:    []string{"gc", "--grace-days", "2"},
			wantOut: []string{"mode=dry-run", "grace=48h0m0s"},
		},
		{
			name:    "negative grace is rejected",
			args:    []string{"gc", "--grace-days", "-1"},
			wantErr: "must be a positive integer",
		},
		{
			name:    "stray positional is rejected",
			args:    []string{"gc", "frob"},
			wantErr: "unexpected argument",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tt.args, &stdout, &stderr)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("run(%v) error = %v, want nil (stderr: %s)", tt.args, err, stderr.String())
				}
				for _, want := range tt.wantOut {
					if !strings.Contains(stderr.String(), want) {
						t.Errorf("gc output = %q, want it to contain %q", stderr.String(), want)
					}
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("run(%v) error = %v, want it to contain %q", tt.args, err, tt.wantErr)
			}
		})
	}
}

// TestGCTwoModesOnRealStore drives gc end to end against a real data
// directory (AC 2): a blob present on disk and referenced by no node is
// listed by the dry run once its mtime is aged past the grace window, and
// --apply physically removes both the file and the ledger row. A referenced
// blob (node row present) survives --apply.
func TestGCTwoModesOnRealStore(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	withEnv(t, map[string]string{
		"BINFLOW_HOME":                    "",
		"BINFLOW_ADMIN_PASSWORD":          "test-admin-pw",
		"BINFLOW_STORAGE__DATA_DIR":       dataDir,
		"BINFLOW_STORAGE__GC_GRACE_HOURS": "24",
	})
	restore := chdirTemp(t)
	defer restore()

	ctx := context.Background()
	md, err := metadata.Open(ctx, metadata.Options{
		Driver:        "sqlite",
		DSN:           filepath.Join(dataDir, "binflow.db"),
		AdminPassword: "test-admin-pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}

	seedBlob := func(body string, age time.Duration, referenced bool) string {
		t.Helper()
		sum := sha256.Sum256([]byte(body))
		sha := hex.EncodeToString(sum[:])
		blobPath := filepath.Join(dataDir, "blobs", sha[:2], sha)
		if err := os.MkdirAll(filepath.Dir(blobPath), 0o700); err != nil {
			t.Fatalf("mkdir blob shard: %v", err)
		}
		if err := os.WriteFile(blobPath, []byte(body), 0o600); err != nil {
			t.Fatalf("write blob: %v", err)
		}
		past := time.Now().Add(-age)
		if err := os.Chtimes(blobPath, past, past); err != nil {
			t.Fatalf("backdate blob mtime: %v", err)
		}
		now := metadata.Now()
		if err := md.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: int64(len(body)), CreatedAt: now}); err != nil {
			t.Fatalf("blobs put: %v", err)
		}
		if referenced {
			if err := md.Repos().Create(ctx, &metadata.Repo{
				RepoKey: "gc-repo", Type: "local", PackageType: "generic", CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				t.Fatalf("repo create: %v", err)
			}
			if err := md.Nodes().Put(ctx, &metadata.Node{
				RepoKey: "gc-repo", Path: "kept.bin", Sha256: sha, Size: int64(len(body)), CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				t.Fatalf("node put: %v", err)
			}
		}
		return sha
	}

	unref := seedBlob("gc-candidate-body", 72*time.Hour, false)
	ref := seedBlob("gc-referenced-body", 72*time.Hour, true)
	if err := md.Close(); err != nil {
		t.Fatalf("metadata close: %v", err)
	}

	// Dry run: the unreferenced blob is listed, nothing is deleted, both
	// ledger rows stay.
	var stdout, stderr bytes.Buffer
	if err := run([]string{"gc"}, &stdout, &stderr); err != nil {
		t.Fatalf("gc dry-run: %v\noutput:\n%s", err, stderr.String())
	}
	out := stderr.String()
	if !strings.Contains(out, "mode=dry-run") || !strings.Contains(out, unref) {
		t.Fatalf("gc dry-run output = %q, want mode=dry-run and the candidate %s", out, unref)
	}
	if strings.Contains(out, ref) {
		t.Fatalf("gc dry-run listed the REFERENCED blob %s — mark set is broken", ref)
	}
	for _, sha := range []string{unref, ref} {
		p := filepath.Join(dataDir, "blobs", sha[:2], sha)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("dry-run must not delete %s: %v", sha, err)
		}
	}

	// Apply: the unreferenced blob is physically gone and its ledger row
	// dropped; the referenced blob survives on disk.
	stdout.Reset()
	stderr.Reset()
	if err := run([]string{"gc", "--apply"}, &stdout, &stderr); err != nil {
		t.Fatalf("gc --apply: %v\noutput:\n%s", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dataDir, "blobs", unref[:2], unref)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unreferenced blob after --apply: %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "blobs", ref[:2], ref)); err != nil {
		t.Fatalf("referenced blob must survive --apply: %v", err)
	}

	md2, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", DSN: filepath.Join(dataDir, "binflow.db")})
	if err != nil {
		t.Fatalf("reopen metadata: %v", err)
	}
	defer func() { _ = md2.Close() }()
	if _, err := md2.Blobs().Get(ctx, ref); err != nil {
		t.Fatalf("referenced ledger row must survive: %v", err)
	}
	if _, err := md2.Blobs().Get(ctx, unref); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("unreferenced ledger row after --apply: %v, want ErrNotFound", err)
	}
}

// TestServePostgresRefusedToBoot (AC 1, FR-3-AC10): metadata.driver=postgres
// fails startup with a nonzero exit and the greppable message, before any
// file is opened.
func TestServePostgresRefusedToBoot(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, map[string]string{
		"BINFLOW_HOME":              "",
		"BINFLOW_ADMIN_PASSWORD":    "test-admin-pw",
		"BINFLOW_STORAGE__DATA_DIR": filepath.Join(dir, "data"),
		"BINFLOW_METADATA__DRIVER":  "postgres",
		"BINFLOW_METADATA__DSN":     "postgres://u:p@localhost:5432/binflow",
	})
	restore := chdirTemp(t)
	defer restore()

	var stdout, stderr bytes.Buffer
	err := run([]string{"serve"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(serve) with env postgres driver error = nil, want refusal")
	}
	if !errors.Is(err, errPostgresDisabled) {
		t.Fatalf("error = %v, want errPostgresDisabled in the chain", err)
	}
	if !strings.Contains(stderr.String(), "Postgres support is not enabled") {
		t.Errorf("stderr = %q, want the FR-3-AC10 message", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "binflow.db")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("postgres refusal must not create a database, stat err = %v", err)
	}
}

// TestServeAssembledStackPing (AC 5): the serve assembly chain (config ->
// metadata -> storage -> auth/audit -> repo.Service -> generic adapter ->
// httpapi) answers /binflow/api/system/ping over a real listener, and the
// default-password WARN fires on a default boot (ADR-0009).
func TestServeAssembledStackPing(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	withEnv(t, map[string]string{
		"BINFLOW_HOME":              "",
		"BINFLOW_STORAGE__DATA_DIR": dataDir,
		"BINFLOW_ADMIN_PASSWORD":    "",
	})
	restore := chdirTemp(t)
	defer restore()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg, err := loadServeConfig("")
	if err != nil {
		t.Fatalf("loadServeConfig: %v", err)
	}
	stack, err := openStack(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	defer stack.close(logger)
	warnDefaultAdminPassword(context.Background(), stack, logger)

	if !strings.Contains(logBuf.String(), "default password") {
		t.Errorf("startup log = %q, want the ADR-0009 default-password WARN", logBuf.String())
	}

	srv := newAssembledServer(cfg, stack, logger)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/binflow/api/system/ping")
	if err != nil {
		t.Fatalf("GET ping: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read ping body: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "OK" {
		t.Fatalf("ping = %d %q, want 200 OK", resp.StatusCode, string(body))
	}
}

// TestServeAdminPasswordEnvOverridesDefault boots with
// BINFLOW_ADMIN_PASSWORD set: the WARN must NOT fire, and admin
// authenticates with the provided password.
func TestServeAdminPasswordEnvOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, map[string]string{
		"BINFLOW_HOME":              "",
		"BINFLOW_STORAGE__DATA_DIR": filepath.Join(dir, "data"),
		"BINFLOW_ADMIN_PASSWORD":    "not-the-default",
	})
	restore := chdirTemp(t)
	defer restore()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	cfg, err := loadServeConfig("")
	if err != nil {
		t.Fatalf("loadServeConfig: %v", err)
	}
	stack, err := openStack(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	defer stack.close(logger)
	warnDefaultAdminPassword(context.Background(), stack, logger)

	if strings.Contains(logBuf.String(), "default password") {
		t.Errorf("startup log = %q, want no default-password WARN for an env-provided password", logBuf.String())
	}
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "not-the-default")
	p, err := stack.authSvc.Authenticate(context.Background(), req)
	if err != nil || p == nil || !p.Admin {
		t.Fatalf("admin auth with env password: principal=%v err=%v, want admin principal", p, err)
	}
}

// TestServeHomeResolution (AC 1): with no config file and BINFLOW_HOME set,
// the data directory lands under $BINFLOW_HOME/data; an explicit
// BINFLOW_DATA_DIR outranks the HOME-derived default.
func TestServeHomeResolution(t *testing.T) {
	home := t.TempDir()
	explicit := t.TempDir()
	withEnv(t, map[string]string{
		"BINFLOW_HOME":              home,
		"BINFLOW_ADMIN_PASSWORD":    "test-admin-pw",
		"BINFLOW_STORAGE__DATA_DIR": "",
	})
	restore := chdirTemp(t)
	defer restore()

	cfg, err := loadServeConfig("")
	if err != nil {
		t.Fatalf("loadServeConfig (defaults): %v", err)
	}
	if want := filepath.Join(home, "data"); cfg.Storage.DataDir != want {
		t.Fatalf("DataDir = %q, want HOME-derived %q", cfg.Storage.DataDir, want)
	}

	t.Setenv("BINFLOW_STORAGE__DATA_DIR", explicit)
	cfg, err = loadServeConfig("")
	if err != nil {
		t.Fatalf("loadServeConfig (explicit): %v", err)
	}
	if cfg.Storage.DataDir != explicit {
		t.Fatalf("DataDir = %q, want explicit %q (DATA_DIR outranks HOME)", cfg.Storage.DataDir, explicit)
	}
}

// TestServeHomeConfigFallback: ./binflow.yaml absent but
// $BINFLOW_HOME/binflow.yaml present -> the HOME config is loaded.
func TestServeHomeConfigFallback(t *testing.T) {
	home := t.TempDir()
	cfgBody := "server:\n  listen: :0\nstorage:\n  data_dir: " + filepath.Join(home, "from-home") + "\n"
	if err := os.WriteFile(filepath.Join(home, "binflow.yaml"), []byte(cfgBody), 0o600); err != nil {
		t.Fatalf("write home config: %v", err)
	}
	withEnv(t, map[string]string{
		"BINFLOW_HOME":           home,
		"BINFLOW_ADMIN_PASSWORD": "test-admin-pw",
	})
	restore := chdirTemp(t)
	defer restore()

	cfg, err := loadServeConfig("")
	if err != nil {
		t.Fatalf("loadServeConfig: %v", err)
	}
	if cfg.Server.Listen != ":0" {
		t.Fatalf("Listen = %q, want the HOME config's :0", cfg.Server.Listen)
	}
	if cfg.Storage.DataDir != filepath.Join(home, "from-home") {
		t.Fatalf("DataDir = %q, want the HOME config's explicit dir", cfg.Storage.DataDir)
	}
}

// TestWarnDefaultPasswordAfterRotation: an admin whose password was rotated
// away from the default does not trigger the WARN.
func TestWarnDefaultPasswordAfterRotation(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, map[string]string{
		"BINFLOW_HOME":              "",
		"BINFLOW_STORAGE__DATA_DIR": filepath.Join(dir, "data"),
		"BINFLOW_ADMIN_PASSWORD":    "test-admin-pw",
	})
	restore := chdirTemp(t)
	defer restore()

	cfg, err := loadServeConfig("")
	if err != nil {
		t.Fatalf("loadServeConfig: %v", err)
	}
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	stack, err := openStack(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	defer stack.close(logger)

	if err := stack.authSvc.ChangePassword(context.Background(), "admin", "test-admin-pw", "rotated-pw"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	logBuf.Reset()
	warnDefaultAdminPassword(context.Background(), stack, logger)
	if strings.Contains(logBuf.String(), "default password") {
		t.Errorf("log = %q, want no WARN after rotation", logBuf.String())
	}
}

// TestDefaultPasswordLiteralAuthenticates pins the WARN predicate to the
// documented default (ADR-0009): the literal here must authenticate against
// the hash metadata actually seeds when BINFLOW_ADMIN_PASSWORD is unset.
func TestDefaultPasswordLiteralAuthenticates(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, map[string]string{
		"BINFLOW_HOME":              "",
		"BINFLOW_ADMIN_PASSWORD":    "",
		"BINFLOW_STORAGE__DATA_DIR": filepath.Join(dir, "data"),
	})
	restore := chdirTemp(t)
	defer restore()

	md, err := metadata.Open(context.Background(), metadata.Options{
		Driver: "sqlite", DSN: filepath.Join(dir, "data", "binflow.db"),
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = md.Close() }()
	u, err := md.Users().Get(context.Background(), "admin")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	if !auth.VerifyPassword(defaultAdminPassword, u.PasswordHash) {
		t.Fatalf("the pinned literal %q does not authenticate against the seeded admin hash", defaultAdminPassword)
	}
}

// TestPostgresRefusedBeforeOpen is the unit twin of
// TestServePostgresRefusedToBoot: openStack's gate fires before metadata or
// storage open, with the sentinel in the chain.
func TestPostgresRefusedBeforeOpen(t *testing.T) {
	cfg := configDefaults()
	cfg.Metadata.Driver = "postgres"
	cfg.Metadata.DSN = "postgres://user:pw@localhost:5432/binflow"
	cfg.Storage.DataDir = t.TempDir()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	_, err := openStack(context.Background(), cfg, logger)
	if !errors.Is(err, errPostgresDisabled) {
		t.Fatalf("openStack(postgres) error = %v, want errPostgresDisabled", err)
	}
	if !strings.Contains(logBuf.String(), "Postgres support is not enabled") {
		t.Errorf("log = %q, want the FR-3-AC10 message", logBuf.String())
	}
}

// ---- helpers ----

// withEnv sets environment variables for the test and restores the previous
// values on cleanup (empty value unsets).
func withEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		old, had := os.LookupEnv(k)
		if v == "" {
			if err := os.Unsetenv(k); err != nil {
				t.Fatalf("unset %s: %v", k, err)
			}
		} else if err := os.Setenv(k, v); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
		t.Cleanup(func() {
			if !had {
				_ = os.Unsetenv(k)
				return
			}
			_ = os.Setenv(k, old)
		})
	}
}

// chdirTemp moves into a fresh empty directory (so the ./binflow.yaml and
// ./data default lookups are isolated) and returns the restore func.
func chdirTemp(t *testing.T) func() {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir %s: %v", tmp, err)
	}
	return func() {
		if err := os.Chdir(wd); err != nil {
			t.Errorf("chdir back %s: %v", wd, err)
		}
	}
}
