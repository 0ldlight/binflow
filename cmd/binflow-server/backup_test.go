package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" // direct driver access: corrupting backup artifacts on purpose (W31 fixtures)

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// shaOf is the reference digest for fixtures.
func shaOf(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// seedInstance builds a real instance: generic repo with two referenced
// nodes, a docker repo whose refs edge holds a third blob, a ledger-only
// orphan blob on disk, and one live web session. Blob files carry a stale
// mtime so mtime preservation is observable (W32). Returns the three live
// checksums.
func seedInstance(t *testing.T, dataDir string) (nodeA, nodeB, refsOnly string) {
	t.Helper()
	ctx := context.Background()
	md, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite", DSN: filepath.Join(dataDir, "binflow.db"), AdminPassword: "test-admin-pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = md.Close() }()

	now := metadata.Now()
	nodeA, nodeB, refsOnly = shaOf("node-a-body"), shaOf("node-b-body"), shaOf("refs-only-body")
	for _, r := range []struct{ key, ptype string }{{"gen", "generic"}, {"dock", "docker"}} {
		if err := md.Repos().Create(ctx, &metadata.Repo{
			RepoKey: r.key, Type: "local", PackageType: r.ptype, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("repo create %s: %v", r.key, err)
		}
	}
	stamp := time.Now().Add(-48 * time.Hour).UTC()
	writeBlob := func(sha, body string) {
		t.Helper()
		path := filepath.Join(dataDir, "blobs", sha[:2], sha)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir shard: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write blob: %v", err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatalf("backdate blob: %v", err)
		}
		if err := md.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: int64(len(body)), CreatedAt: now}); err != nil {
			t.Fatalf("blobs put: %v", err)
		}
	}
	for _, pair := range []struct{ sha, body string }{
		{nodeA, "node-a-body"}, {nodeB, "node-b-body"}, {refsOnly, "refs-only-body"},
		{shaOf("ledger-orphan"), "ledger-orphan"},
	} {
		writeBlob(pair.sha, pair.body)
	}
	for _, n := range []*metadata.Node{
		{RepoKey: "gen", Path: "a.bin", Sha256: nodeA, Size: 12, CreatedAt: now, UpdatedAt: now},
		{RepoKey: "gen", Path: "b.bin", Sha256: nodeB, Size: 12, CreatedAt: now, UpdatedAt: now},
	} {
		if err := md.Nodes().Put(ctx, n); err != nil {
			t.Fatalf("node put: %v", err)
		}
	}
	digest := shaOf("manifest-body")
	if err := md.Docker().PutManifest(ctx, &metadata.DockerManifest{
		RepoKey: "dock", Image: "app", Digest: digest,
		MediaType: "application/vnd.docker.distribution.manifest.v2+json",
		CreatedAt: now, CreatedBy: "admin",
	}); err != nil {
		t.Fatalf("put manifest: %v", err)
	}
	if err := md.Docker().PutRefs(ctx, "dock", "app", digest, []*metadata.DockerRef{
		{RepoKey: "dock", Image: "app", ManifestDigest: digest, BlobDigest: refsOnly, ChildMediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip"},
	}); err != nil {
		t.Fatalf("put refs: %v", err)
	}
	if err := md.WebSessions().Create(ctx, &metadata.WebSession{
		IDHash: shaOf("live-session-id"), Username: "admin",
		CreatedAt: now, ExpiresAt: metadata.NeverExpires,
	}); err != nil {
		t.Fatalf("web session create: %v", err)
	}
	return nodeA, nodeB, refsOnly
}

// backupEnv isolates the CLI runs: env-based config, empty cwd, and paths
// under one temp root.
func backupEnv(t *testing.T) (root, dataDir string) {
	t.Helper()
	root = t.TempDir()
	dataDir = filepath.Join(root, "data")
	withEnv(t, map[string]string{
		"BINFLOW_HOME":              "",
		"BINFLOW_ADMIN_PASSWORD":    "test-admin-pw",
		"BINFLOW_STORAGE__DATA_DIR": dataDir,
	})
	restore := chdirTemp(t)
	t.Cleanup(restore)
	return root, dataDir
}

// runCLI executes a subcommand through the same dispatcher main uses.
func runCLI(t *testing.T, args ...string) error {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(args, &stdout, &stderr)
	if err != nil {
		t.Logf("cli %v failed as expected-ish: %v\nstderr:\n%s", args, err, stderr.String())
	}
	return err
}

// findAuditAction reports whether the store's audit trail carries an event
// with the given action and actor.
func findAuditAction(t *testing.T, dbPath, action, actor string) bool {
	t.Helper()
	md, err := metadata.Open(context.Background(), metadata.Options{Driver: "sqlite", DSN: dbPath, AdminPassword: "test-admin-pw"})
	if err != nil {
		t.Fatalf("open %s for audit check: %v", dbPath, err)
	}
	defer func() { _ = md.Close() }()
	events, err := md.Audits().List(context.Background(), "", actor, 100)
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	for _, e := range events {
		if e.Action == action {
			return true
		}
	}
	return false
}

func loadManifestFor(t *testing.T, backupDir string) *storage.Manifest {
	t.Helper()
	m, err := storage.LoadManifest(storage.ManifestPath(backupDir))
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	return m
}

// TestExportFlagMatrix covers the export/import argument surface.
func TestExportFlagMatrix(t *testing.T) {
	backupEnv(t)
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "export without --output", args: []string{"export"}, wantErr: "--output is required"},
		{name: "export --tar is P2 backlog", args: []string{"export", "--output", "x", "--tar"}, wantErr: "not implemented in M4"},
		{name: "export stray positional", args: []string{"export", "extra"}, wantErr: "unexpected argument"},
		{name: "import without --input", args: []string{"import"}, wantErr: "--input is required"},
		{name: "import bad --verify", args: []string{"import", "--input", "x", "--verify", "paranoid"}, wantErr: "spot or full"},
		{name: "import stray positional", args: []string{"import", "extra"}, wantErr: "unexpected argument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runCLI(t, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("run(%v) error = %v, want it to contain %q", tt.args, err, tt.wantErr)
			}
		})
	}
}

// TestUsageListsBackupCommands: --help names both subcommands (the operator
// discovery surface).
func TestUsageListsBackupCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(--help): %v", err)
	}
	for _, want := range []string{"export", "import", "--verify", "mtime"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("usage output missing %q:\n%s", want, stdout.String())
		}
	}
}

// TestExportOnlineArtifacts (W28/W28b): the export command's artifact shape
// — manifest fields, snapshot hash, 0700 perms, every referenced blob
// present, mtime preserved, ledger orphans excluded, window uploads not
// referenced, export.run audited.
func TestExportOnlineArtifacts(t *testing.T) {
	root, dataDir := backupEnv(t)
	nodeA, nodeB, refsOnly := seedInstance(t, dataDir)
	out := filepath.Join(root, "bk")

	if err := runCLI(t, "export", "--output", out); err != nil {
		t.Fatalf("export: %v", err)
	}

	// Layout: manifest.json + metadata.db + blobs/.
	for _, name := range []string{"manifest.json", "metadata.db", "blobs"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("artifact %s missing: %v", name, err)
		}
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat output dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("output dir mode = %o, want 0700 (NFR-S22)", perm)
	}
	dbInfo, err := os.Stat(filepath.Join(out, "metadata.db"))
	if err != nil {
		t.Fatalf("stat snapshot db: %v", err)
	}
	if perm := dbInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("snapshot db mode = %o, want 0600 (secret-grade artifact regardless of umask)", perm)
	}

	m := loadManifestFor(t, out)
	if m.FormatVersion != storage.BackupFormatVersion || m.CreatedAt == "" || m.BinflowVersion == "" || m.GraceNote == "" {
		t.Fatalf("manifest header fields incomplete: %+v", m)
	}
	wantSet := map[string]bool{nodeA: true, nodeB: true, refsOnly: true}
	if m.BlobCount != 3 || len(m.Blobs) != 3 {
		t.Fatalf("manifest blobCount = %d, want 3 (nodes ∪ docker_refs, not the ledger)", m.BlobCount)
	}
	var total int64
	for _, b := range m.Blobs {
		if !wantSet[b.Sha256] {
			t.Fatalf("manifest references unexpected blob %s", b.Sha256)
		}
		total += b.Size
		// W28b: every referenced blob exists in the artifact...
		p, err := storage.BlobPath(out, b.Sha256)
		if err != nil {
			t.Fatalf("BlobPath: %v", err)
		}
		dst, err := os.Stat(p)
		if err != nil {
			t.Fatalf("referenced blob %s missing from the artifact: %v", b.Sha256, err)
		}
		// ...with its mtime intact (W32).
		src, err := os.Stat(filepath.Join(dataDir, "blobs", b.Sha256[:2], b.Sha256))
		if err != nil {
			t.Fatalf("source blob stat: %v", err)
		}
		if !dst.ModTime().Equal(src.ModTime()) {
			t.Fatalf("blob %s mtime drifted: artifact %v vs source %v", b.Sha256, dst.ModTime(), src.ModTime())
		}
	}
	if m.TotalBytes != total {
		t.Fatalf("manifest totalBytes = %d, want %d", m.TotalBytes, total)
	}

	// metadata.sha256 matches the shipped file byte for byte.
	sum, _, err := storage.HashFile(filepath.Join(out, "metadata.db"))
	if err != nil {
		t.Fatalf("hash snapshot: %v", err)
	}
	if sum != m.Metadata.Sha256 {
		t.Fatalf("metadata.sha256 = %s, actual = %s", m.Metadata.Sha256, sum)
	}
	if m.Metadata.File != "metadata.db" {
		t.Fatalf("metadata.file = %q, want metadata.db", m.Metadata.File)
	}

	// The ledger-orphan blob rides along as surplus (allowed by the window
	// semantics) but is not referenced.
	manifestBytes, err := os.ReadFile(storage.ManifestPath(out))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if strings.Contains(string(manifestBytes), shaOf("ledger-orphan")) {
		t.Fatal("unreferenced ledger orphan is referenced by the manifest")
	}

	// W28b window semantics: an upload AFTER the export is not in the manifest.
	ctx := context.Background()
	md, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite", DSN: filepath.Join(dataDir, "binflow.db"), AdminPassword: "test-admin-pw",
	})
	if err != nil {
		t.Fatalf("reopen source: %v", err)
	}
	newSha := shaOf("post-export-upload")
	path := filepath.Join(dataDir, "blobs", newSha[:2], newSha)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("post-export-upload"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := md.Blobs().Put(ctx, &metadata.Blob{Sha256: newSha, Size: 19, CreatedAt: metadata.Now()}); err != nil {
		t.Fatalf("blobs put: %v", err)
	}
	if err := md.Nodes().Put(ctx, &metadata.Node{
		RepoKey: "gen", Path: "later.bin", Sha256: newSha, Size: 19, CreatedAt: metadata.Now(), UpdatedAt: metadata.Now(),
	}); err != nil {
		t.Fatalf("node put: %v", err)
	}
	if err := md.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	manifestBytes, err = os.ReadFile(storage.ManifestPath(out))
	if err != nil {
		t.Fatalf("reread manifest: %v", err)
	}
	if strings.Contains(string(manifestBytes), newSha) {
		t.Fatal("post-export upload leaked into the manifest — the snapshot boundary is broken")
	}

	// export.run lands in the SOURCE instance's audit trail.
	if !findAuditAction(t, filepath.Join(dataDir, "binflow.db"), "export.run", "admin") {
		t.Fatal("export.run audit event missing from the source instance")
	}
}

// TestExportRefusesWhenLocked (W25b, export side): with the data-directory
// maintenance lock held by a GC run, export fails fast.
func TestExportRefusesWhenLocked(t *testing.T) {
	root, dataDir := backupEnv(t)
	seedInstance(t, dataDir)

	lock, err := storage.AcquireDataLock(dataDir, "gc")
	if err != nil {
		t.Fatalf("hold lock as gc: %v", err)
	}
	defer func() { _ = lock.Release() }()

	err = runCLI(t, "export", "--output", filepath.Join(root, "bk"))
	if !errors.Is(err, storage.ErrDataLockHeld) {
		t.Fatalf("export under gc error = %v, want ErrDataLockHeld", err)
	}
	if !strings.Contains(err.Error(), "op=gc") {
		t.Fatalf("error = %v, want the holder record for diagnostics", err)
	}
	// The refused run must not have left artifacts behind.
	if _, err := os.Stat(filepath.Join(root, "bk", "metadata.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused export left a snapshot behind: %v", err)
	}
}

// TestGCRefusesWhenExportHoldsLock (W25b, gc side): the CLI gc consumes the
// same primitive — while export holds the lock, gc exits non-zero.
func TestGCRefusesWhenExportHoldsLock(t *testing.T) {
	root, dataDir := backupEnv(t)
	seedInstance(t, dataDir)

	lock, err := storage.AcquireDataLock(dataDir, "export")
	if err != nil {
		t.Fatalf("hold lock as export: %v", err)
	}
	defer func() { _ = lock.Release() }()

	err = runCLI(t, "gc")
	if !errors.Is(err, storage.ErrDataLockHeld) {
		t.Fatalf("gc under export error = %v, want ErrDataLockHeld", err)
	}
	_ = root
}

// TestExportDanglingReference: a node whose blob file is gone from the
// filestore is source corruption — export refuses instead of shipping a
// backup it knows is broken, and cleans its partial output.
func TestExportDanglingReference(t *testing.T) {
	root, dataDir := backupEnv(t)
	nodeA, _, _ := seedInstance(t, dataDir)
	if err := os.Remove(filepath.Join(dataDir, "blobs", nodeA[:2], nodeA)); err != nil {
		t.Fatalf("remove blob: %v", err)
	}

	out := filepath.Join(root, "bk")
	err := runCLI(t, "export", "--output", out)
	if err == nil || !strings.Contains(err.Error(), "dangling reference") {
		t.Fatalf("export error = %v, want a dangling-reference refusal", err)
	}
	for _, name := range []string{"metadata.db", "blobs", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(out, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed export left %s behind (looks like a usable backup)", name)
		}
	}
}

// TestExportOutputDirGuards: a non-empty output directory is refused; an
// output path inside the data directory is refused.
func TestExportOutputDirGuards(t *testing.T) {
	root, dataDir := backupEnv(t)
	seedInstance(t, dataDir)

	used := filepath.Join(root, "used")
	if err := os.MkdirAll(used, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(used, "stale.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := runCLI(t, "export", "--output", used); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("export into used dir error = %v, want refusal", err)
	}

	inside := filepath.Join(dataDir, "bk")
	if err := runCLI(t, "export", "--output", inside); err == nil || !strings.Contains(err.Error(), "inside the data directory") {
		t.Fatalf("export into data dir error = %v, want refusal", err)
	}
}

// TestImportRoundtripFullState (W30/W30b/W32): restore into an empty data
// dir; repos, nodes, docker refs, users, the admin password hash and the
// audit history all survive; web_sessions do not; blob mtimes are
// identical; import.run is recorded in the restored instance.
func TestImportRoundtripFullState(t *testing.T) {
	root, dataDir := backupEnv(t)
	nodeA, nodeB, refsOnly := seedInstance(t, dataDir)

	// A pre-existing audit history must survive the roundtrip. (The
	// export.run event itself is appended to the SOURCE after the snapshot
	// is sealed, so it stays in the source instance's trail — pinned in
	// TestExportOnlineArtifacts.)
	ctxSeed := context.Background()
	srcMD, err := metadata.Open(ctxSeed, metadata.Options{Driver: "sqlite", DSN: filepath.Join(dataDir, "binflow.db"), AdminPassword: "test-admin-pw"})
	if err != nil {
		t.Fatalf("open source for audit seed: %v", err)
	}
	if err := srcMD.Audits().Append(ctxSeed, &metadata.AuditEvent{
		Time: metadata.Now(), Actor: "admin", Action: "deploy", RepoKey: "gen", Path: "a.bin", Detail: "{}",
	}); err != nil {
		t.Fatalf("seed audit event: %v", err)
	}
	if err := srcMD.Close(); err != nil {
		t.Fatalf("close source: %v", err)
	}

	backup := filepath.Join(root, "bk")
	if err := runCLI(t, "export", "--output", backup); err != nil {
		t.Fatalf("export: %v", err)
	}

	// Fresh target data dir via env switch.
	freshData := filepath.Join(root, "fresh-data")
	t.Setenv("BINFLOW_STORAGE__DATA_DIR", freshData)
	if err := runCLI(t, "import", "--input", backup, "--verify", "full"); err != nil {
		t.Fatalf("import: %v", err)
	}

	restoredDB := filepath.Join(freshData, "binflow.db")
	if _, err := os.Stat(restoredDB); err != nil {
		t.Fatalf("restored db missing: %v", err)
	}
	// W32: restored blob mtimes equal the source's.
	for _, sha := range []string{nodeA, nodeB, refsOnly, shaOf("ledger-orphan")} {
		src, err := os.Stat(filepath.Join(dataDir, "blobs", sha[:2], sha))
		if err != nil {
			t.Fatalf("source stat %s: %v", sha, err)
		}
		dst, err := os.Stat(filepath.Join(freshData, "blobs", sha[:2], sha))
		if err != nil {
			t.Fatalf("restored stat %s: %v", sha, err)
		}
		if !src.ModTime().Equal(dst.ModTime()) {
			t.Fatalf("blob %s: source mtime %v != restored %v (grace clock reset)", sha, src.ModTime(), dst.ModTime())
		}
	}

	ctx := context.Background()
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", DSN: restoredDB, AdminPassword: "test-admin-pw"})
	if err != nil {
		t.Fatalf("open restored store: %v", err)
	}
	defer func() { _ = md.Close() }()

	repos, err := md.Repos().List(ctx)
	if err != nil || len(repos) != 2 {
		t.Fatalf("restored repos = %v (err %v), want the two seeded repositories", repos, err)
	}
	for _, p := range []string{"a.bin", "b.bin"} {
		if _, err := md.Nodes().Get(ctx, "gen", p); err != nil {
			t.Fatalf("restored node %s: %v", p, err)
		}
	}
	images, err := md.Docker().ListImages(ctx, "dock", "", 0)
	if err != nil || len(images) != 1 {
		t.Fatalf("restored docker images = %v (err %v)", images, err)
	}
	// users survive (password hash included — admin still authenticates via
	// the seeded hash; the restore must not reseed anything).
	if _, err := md.Users().Get(ctx, "admin"); err != nil {
		t.Fatalf("restored admin user: %v", err)
	}
	// web_sessions never ride the backup (architecture 11.19).
	if _, err := md.WebSessions().GetBySHA256(ctx, shaOf("live-session-id")); !errors.Is(err, metadata.ErrWebSessionNotFound) {
		t.Fatalf("restored web session lookup err = %v, want ErrWebSessionNotFound (sessions must not survive)", err)
	}
	// The export-time audit history is queryable in the restored instance.
	events, err := md.Audits().List(ctx, "", "admin", 100)
	if err != nil {
		t.Fatalf("restored audit list: %v", err)
	}
	sawDeploy, sawImport := false, false
	for _, e := range events {
		switch e.Action {
		case "deploy":
			sawDeploy = true // pre-existing history survived
		case "import.run":
			sawImport = true // recorded by the restore itself
		}
	}
	if !sawDeploy {
		t.Fatal("pre-existing audit history did not survive the roundtrip")
	}
	if !sawImport {
		t.Fatal("import.run not recorded in the restored instance")
	}
}

// TestImportCorruptionMatrix (W31 + cross-version): every corrupted backup
// and every dirty target fails fast, with the target data dir left empty.
func TestImportCorruptionMatrix(t *testing.T) {
	buildBackup := func(t *testing.T) (backup string, nodeShas []string) {
		t.Helper()
		root, dataDir := backupEnv(t)
		nodeA, nodeB, refsOnly := seedInstance(t, dataDir)
		backup = filepath.Join(root, "bk")
		if err := runCLI(t, "export", "--output", backup); err != nil {
			t.Fatalf("export: %v", err)
		}
		return backup, []string{nodeA, nodeB, refsOnly}
	}

	tests := []struct {
		name    string
		corrupt func(t *testing.T, backup string, shas []string)
		wantErr string
	}{
		{
			name: "referenced blob deleted from the backup",
			corrupt: func(t *testing.T, backup string, shas []string) {
				p := filepath.Join(backup, "blobs", shas[0][:2], shas[0])
				if err := os.Remove(p); err != nil {
					t.Fatalf("remove blob: %v", err)
				}
			},
			wantErr: "is missing from the backup",
		},
		{
			name: "manifest blobCount tampered (jq .blobCount += 1)",
			corrupt: func(t *testing.T, backup string, _ []string) {
				rewriteManifest(t, backup, func(m map[string]any) { m["blobCount"] = m["blobCount"].(float64) + 1 })
			},
			wantErr: "blobCount",
		},
		{
			name: "manifest totalBytes tampered",
			corrupt: func(t *testing.T, backup string, _ []string) {
				rewriteManifest(t, backup, func(m map[string]any) { m["totalBytes"] = m["totalBytes"].(float64) + 1 })
			},
			wantErr: "totalBytes",
		},
		{
			name: "metadata.db bytes flipped (sha mismatch)",
			corrupt: func(t *testing.T, backup string, _ []string) {
				p := filepath.Join(backup, "metadata.db")
				b, err := os.ReadFile(p)
				if err != nil {
					t.Fatalf("read db: %v", err)
				}
				b[len(b)-1] ^= 0xFF
				if err := os.WriteFile(p, b, 0o600); err != nil {
					t.Fatalf("write db: %v", err)
				}
			},
			wantErr: "hashes to",
		},
		{
			name: "blob truncated (size mismatch caught by the full size pass)",
			corrupt: func(t *testing.T, backup string, shas []string) {
				p := filepath.Join(backup, "blobs", shas[1][:2], shas[1])
				if err := os.WriteFile(p, []byte("short"), 0o600); err != nil {
					t.Fatalf("write blob: %v", err)
				}
			},
			wantErr: "has size",
		},
		{
			name: "snapshot schema newer than this build",
			corrupt: func(t *testing.T, backup string, _ []string) {
				db, err := sql.Open("sqlite", "file:"+filepath.Join(backup, "metadata.db"))
				if err != nil {
					t.Fatalf("open snapshot: %v", err)
				}
				if _, err := db.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", 9999, metadata.Now()); err != nil {
					t.Fatalf("bump version: %v", err)
				}
				if err := db.Close(); err != nil {
					t.Fatalf("close: %v", err)
				}
				// Re-pin the manifest hash so the schema check is what fires.
				sum, _, err := storage.HashFile(filepath.Join(backup, "metadata.db"))
				if err != nil {
					t.Fatalf("hash: %v", err)
				}
				rewriteManifest(t, backup, func(m map[string]any) {
					meta := m["metadata"].(map[string]any)
					meta["sha256"] = sum
				})
			},
			wantErr: "migration chain is not reachable",
		},
		{
			name: "manifest formatVersion from the future",
			corrupt: func(t *testing.T, backup string, _ []string) {
				rewriteManifest(t, backup, func(m map[string]any) { m["formatVersion"] = 99 })
			},
			wantErr: "formatVersion",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backup, shas := buildBackup(t)
			tt.corrupt(t, backup, shas)

			fresh := filepath.Join(filepath.Dir(backup), "fresh")
			t.Setenv("BINFLOW_STORAGE__DATA_DIR", fresh)
			err := runCLI(t, "import", "--input", backup)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("import error = %v, want it to contain %q", err, tt.wantErr)
			}
			assertDirEffectivelyEmpty(t, fresh)
		})
	}

	// The dirty-target refusal happens before the backup is even read.
	t.Run("non-empty target data dir", func(t *testing.T) {
		backup, _ := buildBackup(t)
		dirty := filepath.Join(filepath.Dir(backup), "dirty")
		if err := os.MkdirAll(filepath.Join(dirty, "blobs"), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		t.Setenv("BINFLOW_STORAGE__DATA_DIR", dirty)
		err := runCLI(t, "import", "--input", backup)
		if err == nil || !strings.Contains(err.Error(), "not empty") {
			t.Fatalf("import error = %v, want not-empty refusal", err)
		}
	})
}

// TestSpotVersusFullVerification pins the P0/P1 integrity split: the default
// spot pass rehashes the first 100 (sorted) manifest blobs; --verify full
// rehashes every one. Corruption inside the sample is caught either way;
// corruption beyond it only by full.
func TestSpotVersusFullVerification(t *testing.T) {
	root, dataDir := backupEnv(t)
	ctx := context.Background()

	// 101 referenced blobs: the spot sample is the first 100 sorted.
	md, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite", DSN: filepath.Join(dataDir, "binflow.db"), AdminPassword: "test-admin-pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	now := metadata.Now()
	if err := md.Repos().Create(ctx, &metadata.Repo{RepoKey: "gen", Type: "local", PackageType: "generic", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("repo create: %v", err)
	}
	var shas []string
	for i := 0; i < 101; i++ {
		body := fmt.Sprintf("blob-%03d-body", i)
		sha := shaOf(body)
		shas = append(shas, sha)
		path := filepath.Join(dataDir, "blobs", sha[:2], sha)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := md.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: int64(len(body)), CreatedAt: now}); err != nil {
			t.Fatalf("blobs put: %v", err)
		}
		if err := md.Nodes().Put(ctx, &metadata.Node{
			RepoKey: "gen", Path: fmt.Sprintf("f%03d.bin", i), Sha256: sha, Size: int64(len(body)), CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("node put: %v", err)
		}
	}
	if err := md.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Sorted order decides the sample: first = lowest sha (in the sample),
	// last = highest sha (beyond it).
	sorted := append([]string(nil), shas...)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	inSample := sorted[0]

	backup := filepath.Join(root, "bk")
	if err := runCLI(t, "export", "--output", backup); err != nil {
		t.Fatalf("export: %v", err)
	}

	corrupt := func(sha string) {
		t.Helper()
		p := filepath.Join(backup, "blobs", sha[:2], sha)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read blob: %v", err)
		}
		for i := range b {
			b[i] ^= 0xFF // same length, different content: passes the size pass
		}
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatalf("write blob: %v", err)
		}
	}

	// Corruption INSIDE the sample: the default spot pass catches it.
	corrupt(inSample)
	fresh := filepath.Join(root, "fresh1")
	t.Setenv("BINFLOW_STORAGE__DATA_DIR", fresh)
	if err := runCLI(t, "import", "--input", backup); err == nil || !strings.Contains(err.Error(), "content hashes to") {
		t.Fatalf("spot import of in-sample corruption error = %v, want a hash refusal", err)
	}
	assertDirEffectivelyEmpty(t, fresh)

	// Corruption BEYOND the sample: spot passes (documented P0 posture),
	// full refuses.
	backup2 := filepath.Join(root, "bk2")
	t.Setenv("BINFLOW_STORAGE__DATA_DIR", dataDir)
	if err := runCLI(t, "export", "--output", backup2); err != nil {
		t.Fatalf("re-export: %v", err)
	}
	corruptLast := sorted[len(sorted)-1]
	p := filepath.Join(backup2, "blobs", corruptLast[:2], corruptLast)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for i := range b {
		b[i] ^= 0xFF
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	fresh2 := filepath.Join(root, "fresh2")
	t.Setenv("BINFLOW_STORAGE__DATA_DIR", fresh2)
	if err := runCLI(t, "import", "--input", backup2); err != nil {
		t.Fatalf("spot import of beyond-sample corruption failed = %v, want the documented spot pass", err)
	}
	assertDirEffectivelyEmpty(t, filepath.Join(root, "fresh3"))
	fresh3 := filepath.Join(root, "fresh3")
	t.Setenv("BINFLOW_STORAGE__DATA_DIR", fresh3)
	if err := runCLI(t, "import", "--input", backup2, "--verify", "full"); err == nil || !strings.Contains(err.Error(), "content hashes to") {
		t.Fatalf("full import of beyond-sample corruption error = %v, want a hash refusal", err)
	}
	assertDirEffectivelyEmpty(t, fresh3)
}

// rewriteManifest loads the backup's manifest as generic JSON, mutates it
// and writes it back — the jq-shaped tamper the W31 playbook prescribes.
func rewriteManifest(t *testing.T, backup string, mutate func(m map[string]any)) {
	t.Helper()
	path := storage.ManifestPath(backup)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	mutate(m)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// assertDirEffectivelyEmpty pins the no-half-restore guarantee: after a
// failed import the target holds nothing but (at most) the maintenance lock
// file — no db, no blobs, no sessions.
func assertDirEffectivelyEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.Name() == ".maintenance.lock" {
			continue // runtime lock residue, never restored state
		}
		t.Fatalf("dir %s still holds %q after a failed import — a half-restore must never exist", dir, e.Name())
	}
}

// TestImportAuditedAndIdempotent: import.run lands in the restored instance,
// and importing the same backup twice into fresh directories yields
// byte-identical blob trees (the restore is a pure function of the backup).
func TestImportAuditedAndIdempotent(t *testing.T) {
	root, dataDir := backupEnv(t)
	seedInstance(t, dataDir)
	backup := filepath.Join(root, "bk")
	if err := runCLI(t, "export", "--output", backup); err != nil {
		t.Fatalf("export: %v", err)
	}

	first := filepath.Join(root, "r1")
	t.Setenv("BINFLOW_STORAGE__DATA_DIR", first)
	if err := runCLI(t, "import", "--input", backup); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if !findAuditAction(t, filepath.Join(first, "binflow.db"), "import.run", "admin") {
		t.Fatal("import.run audit event missing from the restored instance")
	}

	second := filepath.Join(root, "r2")
	t.Setenv("BINFLOW_STORAGE__DATA_DIR", second)
	if err := runCLI(t, "import", "--input", backup); err != nil {
		t.Fatalf("second import: %v", err)
	}
	// Both restores carry the same files with the same mtimes.
	a := blobTreeFingerprint(t, first)
	b := blobTreeFingerprint(t, second)
	if a != b {
		aLines, bLines := strings.Split(a, "\n"), strings.Split(b, "\n")
		for i := range aLines {
			if i >= len(bLines) || aLines[i] != bLines[i] {
				t.Fatalf("two imports of the same backup differ at line %d:\n%q\n%q", i, aLines[i], firstOrNil(bLines, i))
			}
		}
		t.Fatalf("two imports differ in length: %d vs %d", len(aLines), len(bLines))
	}
}

func firstOrNil(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// blobTreeFingerprint renders name+size+mtime for every blob, sorted.
func blobTreeFingerprint(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.Walk(filepath.Join(root, "blobs"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s %d %d", rel, info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return strings.Join(lines, "\n")
}

// ---- correctness review fixes (B1/B2) ----

// TestImportHoldsDataLockAgainstGC (B1): import is a data-directory-wide
// maintenance operation — a concurrent gc holding the lock must refuse it
// entry, which is only possible if import itself acquires the lock.
func TestImportHoldsDataLockAgainstGC(t *testing.T) {
	root, dataDir := backupEnv(t)
	seedInstance(t, dataDir)
	backup := filepath.Join(root, "bk")
	if err := runCLI(t, "export", "--output", backup); err != nil {
		t.Fatalf("export: %v", err)
	}

	fresh := filepath.Join(root, "fresh")
	t.Setenv("BINFLOW_STORAGE__DATA_DIR", fresh)

	// A gc (or any maintenance operation) holding the lock refuses import.
	lock, err := storage.AcquireDataLock(fresh, storage.DataLockOpGC)
	if err != nil {
		t.Fatalf("hold lock as gc: %v", err)
	}
	err = runCLI(t, "import", "--input", backup)
	if !errors.Is(err, storage.ErrDataLockHeld) {
		t.Fatalf("import under gc error = %v, want ErrDataLockHeld", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	// With the lock free, the same import succeeds — the guard is the lock,
	// not a broken command.
	if err := runCLI(t, "import", "--input", backup); err != nil {
		t.Fatalf("import after release: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fresh, "binflow.db")); err != nil {
		t.Fatalf("restored db missing: %v", err)
	}
}

// TestClearDirContentsPreservesLockFile (B1): the no-half-restore cleanup
// must skip the maintenance lock file — it runs while import HOLDS the lock,
// and unlinking a held lock file lets the next acquirer lock a fresh inode.
func TestClearDirContentsPreservesLockFile(t *testing.T) {
	dir := t.TempDir()
	lock, err := storage.AcquireDataLock(dir, storage.DataLockOpImport)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer func() { _ = lock.Release() }()

	// Half-restored residue of every kind, plus the live lock file.
	for _, name := range []string{
		"binflow.db", "blobs", "sessions", "stray.txt",
		filepath.Join("blobs", "ab"),
	} {
		if err := os.MkdirAll(filepath.Join(filepath.Dir(filepath.Join(dir, name))), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "binflow.db"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write db: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "blobs", "ab"), 0o700); err != nil {
		t.Fatalf("mkdir blobs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "blobs", "ab", strings.Repeat("a", 64)), []byte("x"), 0o600); err != nil {
		t.Fatalf("write blob: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write stray: %v", err)
	}

	clearDirContents(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != storage.MaintenanceLockName {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("dir holds %v after clearDirContents, want only %q (the held lock file must survive)", names, storage.MaintenanceLockName)
	}
	// The surviving lock is still held and still functional: a contender
	// keeps being refused, release still works.
	if _, err := storage.AcquireDataLock(dir, storage.DataLockOpGC); !errors.Is(err, storage.ErrDataLockHeld) {
		t.Fatalf("second acquire under surviving lock = %v, want ErrDataLockHeld", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release after clear: %v", err)
	}
}

// TestImportRefusesNonSQLiteDriver (B2a): a postgres config must be refused
// before any path math — otherwise the URL-shaped dsn is treated as a file
// path and garbage directories appear outside the data directory.
func TestImportRefusesNonSQLiteDriver(t *testing.T) {
	root, dataDir := backupEnv(t)
	seedInstance(t, dataDir)
	backup := filepath.Join(root, "bk")
	if err := runCLI(t, "export", "--output", backup); err != nil {
		t.Fatalf("export: %v", err)
	}

	fresh := filepath.Join(root, "fresh")
	t.Setenv("BINFLOW_STORAGE__DATA_DIR", fresh)
	t.Setenv("BINFLOW_METADATA__DRIVER", "postgres")
	t.Setenv("BINFLOW_METADATA__DSN", "postgres://user:pw@localhost:5431/binflow")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	err = runCLI(t, "import", "--input", backup)
	if err == nil || !strings.Contains(err.Error(), "metadata.driver") || !strings.Contains(err.Error(), "sqlite") {
		t.Fatalf("import with postgres driver error = %v, want a driver refusal naming sqlite", err)
	}
	// Nothing anywhere: no fresh target, no garbage path tree under the CWD.
	assertDirEffectivelyEmpty(t, fresh)
	if _, err := os.Stat(filepath.Join(cwd, "postgres:")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("postgres URL leaked a path tree into the working directory: %v", err)
	}
}

// TestImportGuardsOutOfDataDirDSN (B2b): an explicit metadata dsn outside
// the data directory is allowed only when it does not exist (no silent
// truncation of another database), and a failed write phase removes what it
// wrote there (no residue outside the data directory).
func TestImportGuardsOutOfDataDirDSN(t *testing.T) {
	root, dataDir := backupEnv(t)
	seedInstance(t, dataDir)
	backup := filepath.Join(root, "bk")
	if err := runCLI(t, "export", "--output", backup); err != nil {
		t.Fatalf("export: %v", err)
	}

	fresh := filepath.Join(root, "fresh")
	externalDB := filepath.Join(root, "elsewhere", "restored.db")
	t.Setenv("BINFLOW_STORAGE__DATA_DIR", fresh)
	t.Setenv("BINFLOW_METADATA__DSN", externalDB)

	// An EXISTING external database is refused (ADR-0015 decision 4: empty
	// instance only; no O_TRUNC of an operator file).
	if err := os.MkdirAll(filepath.Dir(externalDB), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(externalDB, []byte("precious existing database"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	err := runCLI(t, "import", "--input", backup)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("import over existing external db error = %v, want refusal", err)
	}
	if got, err := os.ReadFile(externalDB); err != nil || string(got) != "precious existing database" {
		t.Fatalf("existing external db was touched: %q, %v", got, err)
	}

	// The write-phase rollback contract (residue never escapes the data
	// dir, the held lock survives) is pinned at unit level — an end-to-end
	// write failure mid-copy cannot be forced deterministically without a
	// race, and the unit form tests exactly the B2 concern.
	t.Run("rollback removes out-of-bounds residue and keeps the live lock", func(t *testing.T) {
		target := filepath.Join(root, "fresh-rollback")
		if err := os.MkdirAll(filepath.Join(target, "blobs", "ab"), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(target, "blobs", "ab", strings.Repeat("a", 64)), []byte("x"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := os.WriteFile(filepath.Join(target, "binflow.db"), []byte("partial"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		lock, err := storage.AcquireDataLock(target, storage.DataLockOpImport)
		if err != nil {
			t.Fatalf("acquire (writes the lock residue): %v", err)
		}
		ext := filepath.Join(root, "elsewhere", "partial-restored.db")
		if err := os.WriteFile(ext, []byte("partial external"), 0o600); err != nil {
			t.Fatalf("write external: %v", err)
		}

		cleanupFailedImport(target, ext, true)

		assertDirEffectivelyEmpty(t, target)
		if _, err := os.Stat(ext); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("out-of-bounds db survived the rollback: %v", err)
		}
		if err := lock.Release(); err != nil {
			t.Fatalf("release: %v", err)
		}
	})

	// And the happy path with an external dsn: db lands at the configured
	// point, blobs in the data dir (the operator file having been moved
	// away, the landing point is free again).
	if err := os.Remove(externalDB); err != nil {
		t.Fatalf("remove the operator file: %v", err)
	}
	if err := runCLI(t, "import", "--input", backup); err != nil {
		t.Fatalf("import with external dsn: %v", err)
	}
	if _, err := os.Stat(externalDB); err != nil {
		t.Fatalf("restored external db missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fresh, "blobs")); err != nil {
		t.Fatalf("restored blobs missing in data dir: %v", err)
	}
}
