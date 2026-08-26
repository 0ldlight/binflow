package main

// T-306 (FR-93 / ADR-0036) assembly tests: the three chains a binstore.yaml
// can declare must round-trip a blob end to end — PUT through a full upload
// session, GET through the engine, sha256 reconciled against every store
// the chain owns (the M6 H-sequence reconciliation posture) — and the four
// fail-fast shapes must refuse the boot with usable errors at the real
// runServe seam.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/storage"
)

// t306Configs writes binflow.yaml (data_dir anchored in dir) plus the given
// binstore.yaml, and returns the loaded, validated Config.
func t306Configs(t *testing.T, mainExtra, binstore string) (*config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "binflow.yaml")
	main := "storage:\n  data_dir: " + filepath.Join(dir, "data") + "\n" + mainExtra
	if err := os.WriteFile(mainPath, []byte(main), 0o600); err != nil {
		t.Fatalf("writing binflow.yaml: %v", err)
	}
	if binstore != "" {
		if err := os.WriteFile(filepath.Join(dir, config.BinstoreFileName), []byte(binstore), 0o600); err != nil {
			t.Fatalf("writing binstore.yaml: %v", err)
		}
	}
	cfg, err := config.Load(mainPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg, mainPath
}

// t306ReadBack reconciles one blob: the engine's read stream must hash to
// the sha256 the session commit returned (the H-sequence posture: content
// is verified against its own checksum, not just returned).
func t306ReadBack(ctx context.Context, t *testing.T, st storage.Engine, sha, content string) {
	t.Helper()
	rc, ref, err := st.Open(ctx, sha)
	if err != nil {
		t.Fatalf("Open(%s): %v", sha, err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading blob back: %v", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != sha {
		t.Fatalf("read-back sha256 = %s, want %s", hex.EncodeToString(sum[:]), sha)
	}
	if string(data) != content {
		t.Fatalf("read-back content mismatch: %d bytes", len(data))
	}
	if ref.Sha256 != sha {
		t.Fatalf("BlobRef sha256 = %s, want %s", ref.Sha256, sha)
	}
}

const t306Content = "t-306 three-chain roundtrip payload — the quick brown blob jumps the storage chain"

// TestT306FilestoreChainRoundtrip: binstore.yaml [filestore] assembles the
// disk engine byte-identically to the embedded backend=disk spelling.
func TestT306FilestoreChainRoundtrip(t *testing.T) {
	cfg, mainPath := t306Configs(t, "", "version: 1\nchain:\n  - type: filestore\n")
	st := testStack(t, cfg) // testStack registers the teardown

	ctx := context.Background()
	sha := putBlobViaSession(ctx, t, st.st, t306Content)
	t306ReadBack(ctx, t, st.st, sha, t306Content)

	diskPath, err := storage.BlobPath(cfg.Storage.DataDir, sha)
	if err != nil {
		t.Fatalf("BlobPath: %v", err)
	}
	if data, err := os.ReadFile(diskPath); err != nil || string(data) != t306Content {
		t.Fatalf("disk copy at %s: %v", diskPath, err)
	}
	if _, ok := st.st.(*storage.MigrationEngine); ok {
		t.Error("a single-member chain must not wrap a MigrationEngine")
	}
	want := filepath.Join(filepath.Dir(mainPath), config.BinstoreFileName)
	if cfg.Storage.Chain.Source != want || cfg.Storage.Chain.Mode != "" {
		t.Errorf("chain label = %+v, want source %q mode \"\"", cfg.Storage.Chain, want)
	}
}

// TestT306S3ChainRoundtrip: binstore.yaml [s3] assembles the S3 engine —
// the blob lands in the bucket, never on disk, and reads hash back.
func TestT306S3ChainRoundtrip(t *testing.T) {
	mock := newS3Mock(t, "binflow")
	withEnv(t, map[string]string{config.S3SecretEnvVar: "test-secret"})
	binstore := "version: 1\nchain:\n" +
		"  - type: s3\n" +
		"    bucket: binflow\n" +
		"    region: us-east-1\n" +
		"    endpoint: " + mock.endpoint() + "\n" +
		"    access_key_id: test-access-key\n" +
		"    use_path_style: true\n"
	cfg, _ := t306Configs(t, "", binstore)
	if cfg.Storage.Backend != config.StorageBackendS3 {
		t.Fatalf("backend = %q, want s3", cfg.Storage.Backend)
	}
	st := testStack(t, cfg)

	ctx := context.Background()
	sha := putBlobViaSession(ctx, t, st.st, t306Content)
	t306ReadBack(ctx, t, st.st, sha, t306Content)

	if got := mock.object("binflow", blobKey(sha)); string(got) != t306Content {
		t.Fatalf("bucket object = %d bytes, want the uploaded content", len(got))
	}
	if _, err := os.Stat(filepath.Join(cfg.Storage.DataDir, "blobs")); !os.IsNotExist(err) {
		t.Errorf("s3 chain wrote a local blobs/ tree, stat err = %v", err)
	}
}

// TestT306DualWriteChainRoundtrip: binstore.yaml [filestore, s3] +
// mode=dual-write assembles the T-164 MigrationEngine — one upload lands on
// BOTH stores, reads reconcile, and the REST-facing starter seam is live.
func TestT306DualWriteChainRoundtrip(t *testing.T) {
	mock := newS3Mock(t, "binflow")
	withEnv(t, map[string]string{config.S3SecretEnvVar: "test-secret"})
	binstore := "version: 1\nchain:\n" +
		"  - type: filestore\n" +
		"  - type: s3\n" +
		"    bucket: binflow\n" +
		"    region: us-east-1\n" +
		"    endpoint: " + mock.endpoint() + "\n" +
		"    access_key_id: test-access-key\n" +
		"    use_path_style: true\n" +
		"migration:\n  mode: dual-write\n  concurrency: 3\n"
	cfg, _ := t306Configs(t, "", binstore)
	st := testStack(t, cfg)

	mig, ok := st.st.(*storage.MigrationEngine)
	if !ok {
		t.Fatalf("dual-write chain must assemble a MigrationEngine, got %T", st.st)
	}
	if view := mig.StatusView(); view.Running {
		t.Error("freshly booted dual-write reports a running migration, want idle")
	}

	ctx := context.Background()
	sha := putBlobViaSession(ctx, t, st.st, t306Content)
	t306ReadBack(ctx, t, st.st, sha, t306Content)

	if got := mock.object("binflow", blobKey(sha)); string(got) != t306Content {
		t.Fatalf("s3 copy of the dual-write blob: %d bytes, want the content", len(got))
	}
	diskPath, err := storage.BlobPath(cfg.Storage.DataDir, sha)
	if err != nil {
		t.Fatalf("BlobPath: %v", err)
	}
	if data, err := os.ReadFile(diskPath); err != nil || string(data) != t306Content {
		t.Fatalf("disk copy of the dual-write blob: %v", err)
	}
	if cfg.Storage.Migration.Concurrency != 3 {
		t.Errorf("declared migration concurrency = %d, want 3", cfg.Storage.Migration.Concurrency)
	}
}

// TestT306BypassChainAssemblesDisk: mode=bypass keeps only the filestore
// leg live — the S3 parameters are pre-declared but inert, so an upload
// lands on disk and the bucket stays empty.
func TestT306BypassChainAssemblesDisk(t *testing.T) {
	mock := newS3Mock(t, "binflow")
	withEnv(t, map[string]string{config.S3SecretEnvVar: "test-secret"})
	binstore := "version: 1\nchain:\n" +
		"  - type: filestore\n" +
		"  - type: s3\n" +
		"    bucket: binflow\n" +
		"    region: us-east-1\n" +
		"    endpoint: " + mock.endpoint() + "\n" +
		"    access_key_id: test-access-key\n" +
		"    use_path_style: true\n" +
		"migration:\n  mode: bypass\n"
	cfg, _ := t306Configs(t, "", binstore)
	if cfg.Storage.Backend != config.StorageBackendDisk {
		t.Fatalf("bypass must keep the disk backend, got %q", cfg.Storage.Backend)
	}
	st := testStack(t, cfg)

	ctx := context.Background()
	sha := putBlobViaSession(ctx, t, st.st, t306Content)
	t306ReadBack(ctx, t, st.st, sha, t306Content)

	if got := mock.object("binflow", blobKey(sha)); got != nil {
		t.Errorf("bypass chain wrote to the bucket, want the s3 member inert")
	}
	if cfg.Storage.Chain.Mode != config.MigrationModeBypass {
		t.Errorf("chain mode = %q, want bypass", cfg.Storage.Chain.Mode)
	}
}

// TestT306CompletedChainS3Only: mode=completed is the migration terminal
// state — S3 alone is the source of truth.
func TestT306CompletedChainS3Only(t *testing.T) {
	mock := newS3Mock(t, "binflow")
	withEnv(t, map[string]string{config.S3SecretEnvVar: "test-secret"})
	binstore := "version: 1\nchain:\n" +
		"  - type: filestore\n" +
		"  - type: s3\n" +
		"    bucket: binflow\n" +
		"    region: us-east-1\n" +
		"    endpoint: " + mock.endpoint() + "\n" +
		"    access_key_id: test-access-key\n" +
		"    use_path_style: true\n" +
		"migration:\n  mode: completed\n"
	cfg, _ := t306Configs(t, "", binstore)
	st := testStack(t, cfg)

	if _, ok := st.st.(*storage.MigrationEngine); ok {
		t.Fatal("completed chain must not wrap the engines: S3 alone is the source of truth")
	}
	ctx := context.Background()
	sha := putBlobViaSession(ctx, t, st.st, t306Content)
	t306ReadBack(ctx, t, st.st, sha, t306Content)
	if got := mock.object("binflow", blobKey(sha)); string(got) != t306Content {
		t.Fatalf("bucket object: %d bytes, want the content", len(got))
	}
	if _, err := os.Stat(filepath.Join(cfg.Storage.DataDir, "blobs")); !os.IsNotExist(err) {
		t.Errorf("completed chain wrote a local blobs/ tree, stat err = %v", err)
	}
}

// TestT306ServeRefusesReservedProvider: runServe with a binstore.yaml
// carrying a reserved provider name refuses to boot with the honest
// "reserved slot" message naming the file and line.
func TestT306ServeRefusesReservedProvider(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "binflow.yaml")
	if err := os.WriteFile(mainPath, []byte("storage:\n  data_dir: "+filepath.Join(dir, "data")+"\n"), 0o600); err != nil {
		t.Fatalf("writing binflow.yaml: %v", err)
	}
	binstore := filepath.Join(dir, config.BinstoreFileName)
	if err := os.WriteFile(binstore, []byte("version: 1\nchain:\n  - type: cache-fs\n"), 0o600); err != nil {
		t.Fatalf("writing binstore.yaml: %v", err)
	}

	var buf bytes.Buffer
	err := runServe([]string{"-c", mainPath}, &buf)
	if err == nil {
		t.Fatal("runServe succeeded, want refusal")
	}
	msg := err.Error()
	for _, want := range []string{binstore, `"cache-fs"`, "reserved slot", "line 3"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q\n  does not contain %q", msg, want)
		}
	}
}

// TestT306ServeRefusesDivergentChain: the Q5 divergence refusal at the real
// serve seam — both source files named.
func TestT306ServeRefusesDivergentChain(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "binflow.yaml")
	main := "storage:\n  data_dir: " + filepath.Join(dir, "data") + "\n  backend: disk\n"
	if err := os.WriteFile(mainPath, []byte(main), 0o600); err != nil {
		t.Fatalf("writing binflow.yaml: %v", err)
	}
	binstore := filepath.Join(dir, config.BinstoreFileName)
	if err := os.WriteFile(binstore, []byte("version: 1\nchain:\n  - type: s3\n    bucket: bkt\n"), 0o600); err != nil {
		t.Fatalf("writing binstore.yaml: %v", err)
	}

	var buf bytes.Buffer
	err := runServe([]string{"-c", mainPath}, &buf)
	if err == nil {
		t.Fatal("runServe succeeded, want refusal")
	}
	msg := err.Error()
	for _, want := range []string{binstore, mainPath, "assemble differently"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q\n  does not contain %q", msg, want)
		}
	}
}

// TestT306LegacyEmbeddedZeroChange: the compatibility-window form — an
// embedded storage section with no binstore.yaml anywhere — resolves
// through loadServeConfig exactly as before T-306, plus the migration hint.
func TestT306LegacyEmbeddedZeroChange(t *testing.T) {
	mock := newS3Mock(t, "binflow")
	withEnv(t, map[string]string{config.S3SecretEnvVar: "test-secret"})
	dir := t.TempDir()
	main := "storage:\n" +
		"  data_dir: " + filepath.Join(dir, "data") + "\n" +
		"  backend: s3\n" +
		"  s3:\n" +
		"    bucket: binflow\n" +
		"    region: us-east-1\n" +
		"    endpoint: " + mock.endpoint() + "\n" +
		"    access_key_id: test-access-key\n" +
		"    use_path_style: true\n"
	if err := os.WriteFile(filepath.Join(dir, "binflow.yaml"), []byte(main), 0o600); err != nil {
		t.Fatalf("writing binflow.yaml: %v", err)
	}
	t.Chdir(dir)

	cfg, err := loadServeConfig("")
	if err != nil {
		t.Fatalf("loadServeConfig: %v", err)
	}
	if cfg.Storage.Backend != config.StorageBackendS3 || cfg.Storage.S3.Bucket != "binflow" {
		t.Fatalf("legacy embedded form must resolve unchanged, got backend=%q bucket=%q",
			cfg.Storage.Backend, cfg.Storage.S3.Bucket)
	}
	found := false
	for _, w := range cfg.StartupWarnings {
		if strings.Contains(w, "compatibility window") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing the branch-① migration hint, warnings = %v", cfg.StartupWarnings)
	}

	st := testStack(t, cfg)
	ctx := context.Background()
	sha := putBlobViaSession(ctx, t, st.st, t306Content)
	t306ReadBack(ctx, t, st.st, sha, t306Content)
}
