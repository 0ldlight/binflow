package main

// T-349 (FR-113.5): the default-boot storage-chain ordering, pinned at the
// serve seam. The config-package unit legs live in
// internal/config/binstore_test.go (TestBuildDefaultConfig); these legs pin
// the ORDER as loadServeConfig boots it — the resolution an operator of the
// bare-binary/compose-default form actually meets.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/config"
)

// t349DefaultBootEnv is the incomplete S3 chain-key set (missing
// ACCESS_KEY_ID): enough to look like an S3 declaration, never enough to
// assemble one. The secret rides the always-effective env var.
func t349DefaultBootEnv(dir string) map[string]string {
	return map[string]string{
		"BINFLOW_HOME":                       "",
		"BINFLOW_STORAGE__DATA_DIR":          filepath.Join(dir, "data"),
		"BINFLOW_STORAGE__BACKEND":           "s3",
		"BINFLOW_STORAGE__S3__BUCKET":        "bkt",
		"BINFLOW_STORAGE__S3__REGION":        "us-east-1",
		"BINFLOW_STORAGE__S3__ENDPOINT":      "http://127.0.0.1:9000",
		config.S3SecretEnvVar:                "test-secret",
		"BINFLOW_STORAGE__S3__ACCESS_KEY_ID": "", // deliberately incomplete
	}
}

// TestT349EnvOnlyIncompleteChainRefuses: with no binflow.yaml and no
// binstore.yaml anywhere, the incomplete chain-key env set refuses the boot
// at config-load time, naming the missing key — the fail-fast the AC pins.
func TestT349EnvOnlyIncompleteChainRefuses(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, t349DefaultBootEnv(dir))
	restore := chdirTemp(t)
	defer restore()

	var buf bytes.Buffer
	err := runServe(nil, &buf)
	if err == nil {
		t.Fatal("runServe booted with an incomplete S3 env chain, want refusal")
	}
	if msg := err.Error(); !strings.Contains(msg, "storage.s3.access_key_id is required when backend=s3") {
		t.Errorf("refusal %q does not name the missing key", msg)
	}
}

// TestT349BinstoreOutranksIncompleteEnvChain: the same incomplete env set
// with a binstore.yaml present boots FROM THE FILE — the chain-scoped env
// keys are ignored + warned (complete sets always were; T-325 registered
// the inconsistency, T-349 closes it) — and the chain label names the file.
func TestT349BinstoreOutranksIncompleteEnvChain(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.BinstoreFileName),
		[]byte("version: 1\nchain:\n  - type: filestore\n"), 0o600); err != nil {
		t.Fatalf("writing binstore.yaml: %v", err)
	}
	withEnv(t, t349DefaultBootEnv(dir))
	t.Chdir(dir) // ./binstore.yaml is the lookup position under test

	cfg, err := loadServeConfig("")
	if err != nil {
		t.Fatalf("loadServeConfig: %v (binstore.yaml owns the chain; the incomplete env set must be ignored, not fatal)", err)
	}
	if cfg.Storage.Backend != config.StorageBackendDisk {
		t.Errorf("backend = %q, want the file's disk", cfg.Storage.Backend)
	}
	if !strings.HasSuffix(cfg.Storage.Chain.Source, config.BinstoreFileName) {
		t.Errorf("chain source = %q, want the binstore.yaml path", cfg.Storage.Chain.Source)
	}
	found := false
	for _, w := range cfg.StartupWarnings {
		if strings.Contains(w, "BINFLOW_STORAGE__BACKEND is set but ignored") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing the ignored-env warning, warnings = %v", cfg.StartupWarnings)
	}
}

// TestT349BrokenBinstoreErrorPrecedesEnvRefusal: a broken binstore.yaml AND
// the incomplete env set in one boot — the FILE's error wins; the
// env-spelled missing-key complaint never fires. The file owns the chain,
// so the file's diagnostics own the boot (the T-349 ordering).
func TestT349BrokenBinstoreErrorPrecedesEnvRefusal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.BinstoreFileName),
		[]byte("version: 1\nchain:\n  - type: cache-fs\n"), 0o600); err != nil {
		t.Fatalf("writing binstore.yaml: %v", err)
	}
	withEnv(t, t349DefaultBootEnv(dir))
	t.Chdir(dir) // ./binstore.yaml is the lookup position under test

	var buf bytes.Buffer
	err := runServe(nil, &buf)
	if err == nil {
		t.Fatal("runServe booted with a broken binstore.yaml, want refusal")
	}
	msg := err.Error()
	if !strings.Contains(msg, "cache-fs") || !strings.Contains(msg, "reserved") {
		t.Errorf("refusal %q is not the binstore.yaml error", msg)
	}
	if strings.Contains(msg, "access_key_id") {
		t.Errorf("refusal %q leaks the env-spelled complaint the file outranks", msg)
	}
}
