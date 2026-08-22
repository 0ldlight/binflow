package main

// T-201 tests for the engine-aware backup faces (T-173 D-2): under
// storage.backend=s3 the data directory carries no blobs/ tree, so
//
//	export must stream every snapshot-referenced blob OUT of the engine
//	(the previous unconditional tree copy shipped nothing and died at the
//	manifest boundary with "dangling reference"), and
//
//	import must upload the artifact's blobs back INTO the engine (the tree
// copy restored files an S3 instance never reads).
//
// The mock bucket rides s3_stack_test.go's s3Mock (extended here with the
// GET/ListObjectsV2 subset the engine's read paths exercise).

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// s3BackupEnv is backupEnv plus the S3 environment legs: env-only config
// (backend=s3 against the mock bucket). The secret rides the documented
// env-only name exactly like the serve assembly.
func s3BackupEnv(t *testing.T, endpoint, bucket string) (root, dataDir string) {
	t.Helper()
	root = t.TempDir()
	dataDir = filepath.Join(root, "data")
	withEnv(t, map[string]string{
		"BINFLOW_HOME":                        "",
		"BINFLOW_ADMIN_PASSWORD":              "test-admin-pw",
		"BINFLOW_STORAGE__DATA_DIR":           dataDir,
		"BINFLOW_STORAGE__BACKEND":            config.StorageBackendS3,
		"BINFLOW_STORAGE__S3__BUCKET":         bucket,
		"BINFLOW_STORAGE__S3__REGION":         "us-east-1",
		"BINFLOW_STORAGE__S3__ENDPOINT":       endpoint,
		"BINFLOW_STORAGE__S3__ACCESS_KEY_ID":  "test-access-key",
		"BINFLOW_STORAGE__S3__USE_PATH_STYLE": "true",
		config.S3SecretEnvVar:                 "test-secret",
	})
	restore := chdirTemp(t)
	t.Cleanup(restore)
	return root, dataDir
}

// seedS3Instance builds a live S3-backed source instance's blobs: two
// blobs uploaded through the engine's session path (the keys land at
// blobs/<xx>/<sha> in the mock bucket) plus the sha of one body never
// uploaded — the dangling-reference fixture the export must refuse.
// Returns the two live checksums and the dangling one. Node rows are the
// caller's business (each test shapes its own live set).
func seedS3Instance(t *testing.T, cfg *config.Config) (liveA, liveB, dangling string) {
	t.Helper()
	ctx := context.Background()

	// The S3 engine path never touches upload_sessions, but openStorageEngine
	// now accepts the metadata store seam; open a throwaway store on the
	// shared data-dir database (the tests optionally reuse the same file).
	md, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite", DSN: sqlitePath(cfg), AdminPassword: "test-admin-pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = md.Close() }()

	st, err := openStorageEngine(ctx, cfg, testSlogLogger(t), md)
	if err != nil {
		t.Fatalf("openStorageEngine: %v", err)
	}
	defer func() { _ = st.Close() }()

	liveA = putBlobViaSession(ctx, t, st, "s3-node-a-body")
	liveB = putBlobViaSession(ctx, t, st, "s3-node-b-body")
	dangling = shaOf("never-uploaded-body")
	return liveA, liveB, dangling
}

// seedNodeWithLedger writes one node row plus its blobs-ledger row (nodes
// carry a FK into the ledger — the shape repo.Service's landed-upload path
// guarantees and the raw SQL seed must reproduce).
func seedNodeWithLedger(ctx context.Context, t *testing.T, md metadata.Store, n *metadata.Node) {
	t.Helper()
	if err := md.Blobs().Put(ctx, &metadata.Blob{Sha256: n.Sha256, Size: n.Size, CreatedAt: n.CreatedAt}); err != nil {
		t.Fatalf("blobs put %s: %v", n.Sha256, err)
	}
	if err := md.Nodes().Put(ctx, n); err != nil {
		t.Fatalf("node put %s: %v", n.Path, err)
	}
}

// TestExportS3StreamsBlobsFromEngine (D-2): export on a backend=s3
// instance succeeds, the artifact carries every referenced blob streamed
// from the bucket, and the local data directory never grows a blobs/ tree.
func TestExportS3StreamsBlobsFromEngine(t *testing.T) {
	mock := newS3Mock(t, "bk-src")
	root, dataDir := s3BackupEnv(t, mock.endpoint(), "bk-src")
	_ = root

	ctx := context.Background()
	cfg, err := loadServeConfig("")
	if err != nil {
		t.Fatalf("loadServeConfig: %v", err)
	}
	liveA, liveB, _ := seedS3Instance(t, cfg)

	// Reference the blobs from nodes (the manifest boundary is the
	// snapshot's live set, not the bucket contents).
	md, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite", DSN: filepath.Join(dataDir, "binflow.db"), AdminPassword: "test-admin-pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	now := metadata.Now()
	for _, r := range []string{"gen"} {
		if err := md.Repos().Create(ctx, &metadata.Repo{
			RepoKey: r, Type: "local", PackageType: "generic", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("repo create: %v", err)
		}
	}
	for _, n := range []*metadata.Node{
		{RepoKey: "gen", Path: "a.bin", Sha256: liveA, Size: 14, CreatedAt: now, UpdatedAt: now},
		{RepoKey: "gen", Path: "b.bin", Sha256: liveB, Size: 14, CreatedAt: now, UpdatedAt: now},
	} {
		seedNodeWithLedger(ctx, t, md, n)
	}
	if err := md.Close(); err != nil {
		t.Fatalf("md close: %v", err)
	}

	out := filepath.Join(t.TempDir(), "bk")
	// Backdate one object's LastModified so the export's mtime
	// preservation (object LastModified -> artifact file mtime, the
	// W28/W32 grace-clock contract on the S3 face) is observable.
	stamp := time.Now().Add(-48 * time.Hour).UTC()
	mock.setObjectLastModified("bk-src", blobKey(liveA), stamp)
	if err := runCLI(t, "export", "--output", out); err != nil {
		t.Fatalf("export (backend=s3): %v", err)
	}

	m := loadManifestFor(t, out)
	if m.BlobCount != 2 {
		t.Fatalf("manifest blobCount = %d, want 2 (m.Blobs: %+v)", m.BlobCount, m.Blobs)
	}
	want := map[string]string{liveA: "s3-node-a-body", liveB: "s3-node-b-body"}
	for sha, body := range want {
		p, err := storage.BlobPath(out, sha)
		if err != nil {
			t.Fatalf("BlobPath: %v", err)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("artifact blob %s missing: %v (the engine stream-out failed, T-173 D-2)", sha, err)
		}
		if string(data) != body {
			t.Fatalf("artifact blob %s = %q, want %q", sha, data, body)
		}
	}
	// W28/W32 on the engine face: the object's LastModified rides the
	// artifact as the file's mtime (second granularity — filesystem
	// mtime storage may round the nanoseconds RFC3339Nano carries).
	mtimePath, err := storage.BlobPath(out, liveA)
	if err != nil {
		t.Fatalf("BlobPath: %v", err)
	}
	info, err := os.Stat(mtimePath)
	if err != nil {
		t.Fatalf("stat artifact blob: %v", err)
	}
	if got, want := info.ModTime().Unix(), stamp.Unix(); got != want {
		t.Fatalf("artifact mtime = %d, want the bucket object's LastModified %d", got, want)
	}
	// The export must not fabricate a local blob tree on an S3 instance.
	if _, err := os.Stat(filepath.Join(dataDir, "blobs")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("export created a local blobs/ tree under backend=s3, stat err = %v", err)
	}
}

// TestExportS3DanglingReference (D-2 negative): a node referencing a blob
// the bucket does not carry is a dangling reference in the SOURCE instance
// — the export refuses instead of shipping a broken artifact (W28b).
func TestExportS3DanglingReference(t *testing.T) {
	mock := newS3Mock(t, "bk-dangling")
	_, dataDir := s3BackupEnv(t, mock.endpoint(), "bk-dangling")

	ctx := context.Background()
	cfg, err := loadServeConfig("")
	if err != nil {
		t.Fatalf("loadServeConfig: %v", err)
	}
	liveA, _, dangling := seedS3Instance(t, cfg)

	md, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite", DSN: filepath.Join(dataDir, "binflow.db"), AdminPassword: "test-admin-pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	now := metadata.Now()
	if err := md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "gen", Type: "local", PackageType: "generic", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("repo create: %v", err)
	}
	for _, n := range []*metadata.Node{
		{RepoKey: "gen", Path: "a.bin", Sha256: liveA, Size: 14, CreatedAt: now, UpdatedAt: now},
		{RepoKey: "gen", Path: "ghost.bin", Sha256: dangling, Size: 19, CreatedAt: now, UpdatedAt: now},
	} {
		seedNodeWithLedger(ctx, t, md, n)
	}
	if err := md.Close(); err != nil {
		t.Fatalf("md close: %v", err)
	}

	out := filepath.Join(t.TempDir(), "bk")
	err = runCLI(t, "export", "--output", out)
	if err == nil {
		t.Fatal("export succeeded with a dangling reference, want refusal")
	}
	if !strings.Contains(err.Error(), "dangling reference") {
		t.Fatalf("export error = %v, want it to name the dangling reference", err)
	}
}

// TestExportImportS3Roundtrip (D-2 + AC 4): export an S3 instance, import
// the artifact into a FRESH data directory pointing at a FRESH bucket, and
// verify the restore: db rows back, blobs live in the new bucket (readable
// through the engine), no local blobs tree.
func TestExportImportS3Roundtrip(t *testing.T) {
	mock := newS3Mock(t, "rt-src", "rt-dst")
	root, dataDir := s3BackupEnv(t, mock.endpoint(), "rt-src")

	ctx := context.Background()
	cfg, err := loadServeConfig("")
	if err != nil {
		t.Fatalf("loadServeConfig: %v", err)
	}
	liveA, liveB, _ := seedS3Instance(t, cfg)

	md, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite", DSN: filepath.Join(dataDir, "binflow.db"), AdminPassword: "test-admin-pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	now := metadata.Now()
	if err := md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "gen", Type: "local", PackageType: "generic", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("repo create: %v", err)
	}
	for _, n := range []*metadata.Node{
		{RepoKey: "gen", Path: "a.bin", Sha256: liveA, Size: 14, CreatedAt: now, UpdatedAt: now},
		{RepoKey: "gen", Path: "b.bin", Sha256: liveB, Size: 14, CreatedAt: now, UpdatedAt: now},
	} {
		seedNodeWithLedger(ctx, t, md, n)
	}
	if err := md.Close(); err != nil {
		t.Fatalf("md close: %v", err)
	}

	out := filepath.Join(root, "bk")
	if err := runCLI(t, "export", "--output", out); err != nil {
		t.Fatalf("export: %v", err)
	}

	// Retarget the environment at a fresh data dir + fresh bucket (the
	// disaster-recovery shape: new machine, new bucket, one artifact).
	dstData := filepath.Join(root, "data-restored")
	withEnv(t, map[string]string{
		"BINFLOW_STORAGE__DATA_DIR":   dstData,
		"BINFLOW_STORAGE__S3__BUCKET": "rt-dst",
	})
	if err := runCLI(t, "import", "--input", out, "--verify", "full"); err != nil {
		t.Fatalf("import (backend=s3): %v", err)
	}

	// The restored metadata carries the nodes (and the import.run audit).
	restored, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite", DSN: filepath.Join(dstData, "binflow.db"), AdminPassword: "test-admin-pw",
	})
	if err != nil {
		t.Fatalf("open restored metadata: %v", err)
	}
	defer func() { _ = restored.Close() }()
	nodes, err := restored.Nodes().ListByPrefix(ctx, "gen", "")
	if err != nil {
		t.Fatalf("list restored nodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("restored node count = %d, want 2", len(nodes))
	}

	// The blobs live in the TARGET bucket and read back byte-identical
	// through the engine (the symmetric restore the tree copy could not
	// deliver on an S3 instance).
	withEnv(t, map[string]string{"BINFLOW_STORAGE__DATA_DIR": t.TempDir()})
	cfgDst, err := loadServeConfig("")
	if err != nil {
		t.Fatalf("loadServeConfig (dst): %v", err)
	}
	stDst, err := openStorageEngine(ctx, cfgDst, testSlogLogger(t), restored)
	if err != nil {
		t.Fatalf("openStorageEngine (dst): %v", err)
	}
	defer func() { _ = stDst.Close() }()
	for sha, body := range map[string]string{liveA: "s3-node-a-body", liveB: "s3-node-b-body"} {
		rc, ref, err := stDst.Open(ctx, sha)
		if err != nil {
			t.Fatalf("open restored blob %s from the target bucket: %v", sha, err)
		}
		got, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read restored blob %s: %v", sha, err)
		}
		if int64(len(got)) != ref.Size || string(got) != body {
			t.Fatalf("restored blob %s = %q (size %d), want %q (%d bytes)", sha, got, len(got), body, ref.Size)
		}
	}
	// The inventory seam reports both blobs with their sizes.
	inv, ok := stDst.(blobInventory)
	if !ok {
		t.Fatalf("engine %T does not carry the inventory seam", stDst)
	}
	stats, err := inv.BlobStats(ctx)
	if err != nil {
		t.Fatalf("BlobStats: %v", err)
	}
	if len(stats) != 2 || stats[liveA].Size != 14 || stats[liveB].Size != 14 {
		t.Fatalf("inventory = %+v, want both blobs at 14 bytes", stats)
	}
	// No local blob tree on the restored S3 instance either.
	if _, err := os.Stat(filepath.Join(dstData, "blobs")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("import created a local blobs/ tree under backend=s3, stat err = %v", err)
	}
}

// TestEngineBackedBlobStoreBranch pins the backup-face selector: pure s3
// and completed-migration stacks are engine-backed; disk and dual-write
// (migration enabled, not completed) keep the data-dir tree copy.
func TestEngineBackedBlobStoreBranch(t *testing.T) {
	base := func(mut func(*config.Config)) *config.Config {
		cfg := configDefaults()
		cfg.Storage.DataDir = t.TempDir()
		mut(cfg)
		return cfg
	}
	cases := []struct {
		name string
		mut  func(*config.Config)
		want bool
	}{
		{"disk", func(*config.Config) {}, false},
		{"s3 pure", func(c *config.Config) { c.Storage.Backend = config.StorageBackendS3 }, true},
		{"s3 dual-write", func(c *config.Config) {
			c.Storage.Backend = config.StorageBackendS3
			c.Storage.Migration.Enabled = true
			c.Storage.Migration.Completed = false
		}, false},
		{"s3 migration completed", func(c *config.Config) {
			c.Storage.Backend = config.StorageBackendS3
			c.Storage.Migration.Enabled = true
			c.Storage.Migration.Completed = true
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := engineBackedBlobStore(base(tc.mut)); got != tc.want {
				t.Fatalf("engineBackedBlobStore = %v, want %v", got, tc.want)
			}
		})
	}
}
