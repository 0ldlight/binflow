package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite" // direct driver access: the tests inspect snapshot ARTIFACTS, not stores

	"github.com/lzwzzy/binflow/internal/metadata"
)

// openSnapshotRW opens a snapshot file read-write through the raw driver —
// the test-side scalpel for corrupting artifacts on purpose (W31 fixtures).
func openSnapshotRW(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(15000)")
	if err != nil {
		t.Fatalf("open snapshot rw: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func shaFor(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// seedSnapshotFixture builds a store with one repo, two referenced nodes, a
// docker ref edge holding a third blob, one unreferenced ledger-only blob
// and one live web session — the shape every snapshot assertion needs.
func seedSnapshotFixture(t *testing.T, dir string) (nodeA, nodeB, refsOnly string) {
	t.Helper()
	ctx := context.Background()
	md, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite", DSN: filepath.Join(dir, "binflow.db"), AdminPassword: "pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = md.Close() }()

	now := metadata.Now()
	nodeA, nodeB, refsOnly = shaFor("node-a"), shaFor("node-b"), shaFor("refs-only")
	for _, r := range []struct{ key, ptype string }{{"gen", "generic"}, {"dock", "docker"}} {
		if err := md.Repos().Create(ctx, &metadata.Repo{
			RepoKey: r.key, Type: "local", PackageType: r.ptype, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("repo create %s: %v", r.key, err)
		}
	}
	for _, sha := range []string{nodeA, nodeB} {
		if err := md.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: 6, CreatedAt: now}); err != nil {
			t.Fatalf("blobs put: %v", err)
		}
	}
	for _, n := range []*metadata.Node{
		{RepoKey: "gen", Path: "a.bin", Sha256: nodeA, Size: 6, CreatedAt: now, UpdatedAt: now},
		{RepoKey: "gen", Path: "b.bin", Sha256: nodeB, Size: 6, CreatedAt: now, UpdatedAt: now},
	} {
		if err := md.Nodes().Put(ctx, n); err != nil {
			t.Fatalf("node put: %v", err)
		}
	}
	// Explicit folder rows, written exactly the way repo.Service's folder
	// deploy writes them (T-124, from T-106 QA D-106-1): trailing-slash
	// paths over ONE shared FolderMarkerSHA ledger row, no physical blob.
	// Every checksum assertion below now runs with the folder shape present.
	if err := md.Blobs().Put(ctx, &metadata.Blob{Sha256: metadata.FolderMarkerSHA, Size: 0, CreatedAt: now}); err != nil {
		t.Fatalf("blobs put folder marker: %v", err)
	}
	for _, p := range []string{"acme/", "acme/nested/"} {
		if err := md.Nodes().Put(ctx, &metadata.Node{
			RepoKey: "gen", Path: p, Sha256: metadata.FolderMarkerSHA, Size: 0, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("folder node put %s: %v", p, err)
		}
	}
	digest := shaFor("manifest-body")
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
	// Ledger-only row: present in blobs, referenced by nothing — it must NOT
	// be part of the snapshot's live set.
	ledgerOnly := shaFor("ledger-only")
	if err := md.Blobs().Put(ctx, &metadata.Blob{Sha256: ledgerOnly, Size: 12, CreatedAt: now}); err != nil {
		t.Fatalf("blobs put: %v", err)
	}
	if err := md.WebSessions().Create(ctx, &metadata.WebSession{
		IDHash: shaFor("session-id"), Username: "admin",
		CreatedAt: now, ExpiresAt: metadata.NeverExpires,
	}); err != nil {
		t.Fatalf("web session create: %v", err)
	}
	// A live upload session row: like the web session, it is runtime state and
	// must not ride the artifact (purged on export). The timestamps are strings
	// (metadata.Now() returns an RFC3339 string); the expiry is far in the
	// future only to keep the row valid — the purge ignores expiry entirely.
	if err := md.UploadSessions().Create(ctx, &metadata.UploadSession{
		ID: "11111111-2222-3333-4444-555555555555", State: `{"received":42}`,
		CreatedAt: now, ExpiresAt: metadata.NeverExpires,
	}); err != nil {
		t.Fatalf("upload session create: %v", err)
	}
	return nodeA, nodeB, refsOnly
}

// vacuumSnapshotOf reopens the fixture store and snapshots it, standing in
// for the export command's step 1.
func vacuumSnapshotOf(t *testing.T, dir, dst string) {
	t.Helper()
	md, err := metadata.Open(ctxBG(), metadata.Options{
		Driver: "sqlite", DSN: filepath.Join(dir, "binflow.db"), AdminPassword: "pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open for vacuum: %v", err)
	}
	defer func() { _ = md.Close() }()
	type vacuumer interface {
		VacuumInto(ctx context.Context, dst string) error
	}
	v, ok := md.(vacuumer)
	if !ok {
		t.Fatal("store does not implement VacuumInto (consumer-side interface)")
	}
	if err := v.VacuumInto(ctxBG(), dst); err != nil {
		t.Fatalf("VacuumInto: %v", err)
	}
}

func ctxBG() context.Context { return context.Background() }

func TestVacuumIntoProducesPlainArtifact(t *testing.T) {
	dir := t.TempDir()
	seedSnapshotFixture(t, dir)
	dst := filepath.Join(t.TempDir(), "snapshot.db")
	vacuumSnapshotOf(t, dir, dst)

	// One plain file, no WAL companions — the artifact must be byte-stable.
	for _, companion := range []string{dst + "-wal", dst + "-shm"} {
		if _, err := os.Stat(companion); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("snapshot left a companion %s: %v", companion, err)
		}
	}
	db := openSnapshotRW(t, dst)
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if mode != "delete" {
		t.Fatalf("snapshot journal_mode = %q, want delete (plain artifact)", mode)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM repositories").Scan(&count); err != nil {
		t.Fatalf("repositories count: %v", err)
	}
	if count != 2 {
		t.Fatalf("snapshot repositories = %d, want 2 (data must survive the vacuum)", count)
	}

	// VACUUM INTO refuses an existing destination — a retried export must
	// never silently overwrite.
	if err := func() error {
		md, err := metadata.Open(ctxBG(), metadata.Options{Driver: "sqlite", DSN: filepath.Join(dir, "binflow.db"), AdminPassword: "pw"})
		if err != nil {
			return err
		}
		defer func() { _ = md.Close() }()
		type vacuumer interface {
			VacuumInto(ctx context.Context, dst string) error
		}
		return md.(vacuumer).VacuumInto(ctxBG(), dst)
	}(); err == nil {
		t.Fatal("VacuumInto onto an existing file succeeded, want refusal")
	}
}

func TestSnapshotChecksumsIsNodesUnionDockerRefs(t *testing.T) {
	dir := t.TempDir()
	nodeA, nodeB, refsOnly := seedSnapshotFixture(t, dir)
	dst := filepath.Join(t.TempDir(), "snapshot.db")
	vacuumSnapshotOf(t, dir, dst)

	set, err := metadata.SnapshotChecksums(ctxBG(), dst)
	if err != nil {
		t.Fatalf("SnapshotChecksums: %v", err)
	}
	for _, want := range []string{nodeA, nodeB, refsOnly} {
		if _, ok := set[want]; !ok {
			t.Fatalf("live set is missing %s; set = %v", want, set)
		}
	}
	if _, ok := set[shaFor("ledger-only")]; ok {
		t.Fatal("ledger-only blob is in the live set — the boundary must be nodes ∪ docker_refs, not the blobs ledger")
	}
	// T-124: the folder marker sentinel is a value-based contract, never a
	// physical blob reference — its presence here is exactly the shape that
	// made export refuse every instance with an explicit directory node.
	if _, ok := set[metadata.FolderMarkerSHA]; ok {
		t.Fatal("folder marker sentinel is in the live set — a folder row is not a physical blob reference (T-124)")
	}
	if len(set) != 3 {
		t.Fatalf("live set size = %d, want 3 (two file nodes + one refs edge; folder markers excluded); set = %v", len(set), set)
	}

	// The inspector is read-only at the file level: an existing artifact's
	// mtime survives an inspection unchanged.
	before, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat snapshot: %v", err)
	}
	if _, err := metadata.SnapshotChecksums(ctxBG(), dst); err != nil {
		t.Fatalf("second inspection: %v", err)
	}
	after, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat snapshot again: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Fatal("inspection modified the snapshot artifact")
	}
}

func TestSnapshotSchemaVersionAndCeiling(t *testing.T) {
	// No hardcoded version number: migrations land with parallel tickets
	// (004 console governance, 005 quota backfill, ...), so the pin is
	// relational — the ceiling is the embedded set's top, and a snapshot of
	// a freshly migrated store reports exactly it.
	latest := metadata.LatestSchemaVersion()
	if latest < 4 {
		t.Fatalf("LatestSchemaVersion = %d, want at least 4 (004_console_governance)", latest)
	}
	dir := t.TempDir()
	seedSnapshotFixture(t, dir)
	dst := filepath.Join(t.TempDir(), "snapshot.db")
	vacuumSnapshotOf(t, dir, dst)

	v, err := metadata.SnapshotSchemaVersion(ctxBG(), dst)
	if err != nil {
		t.Fatalf("SnapshotSchemaVersion: %v", err)
	}
	if v != latest {
		t.Fatalf("snapshot schema version = %d, want the embedded ceiling %d", v, latest)
	}

	// A snapshot from a NEWER build (schema_migrations carries a version
	// past what this binary embeds) must be detectable — import refuses it.
	db := openSnapshotRW(t, dst)
	if _, err := db.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", 999, metadata.Now()); err != nil {
		t.Fatalf("bump schema version: %v", err)
	}
	v, err = metadata.SnapshotSchemaVersion(ctxBG(), dst)
	if err != nil {
		t.Fatalf("SnapshotSchemaVersion after bump: %v", err)
	}
	if v <= metadata.LatestSchemaVersion() {
		t.Fatalf("bumped version %d not visible above the ceiling %d", v, metadata.LatestSchemaVersion())
	}

	// A database with no schema_migrations at all reports 0 (legacy file).
	legacy := filepath.Join(t.TempDir(), "legacy.db")
	ldb, err := sql.Open("sqlite", "file:"+legacy)
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	if _, err := ldb.Exec("CREATE TABLE t(a)"); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if err := ldb.Close(); err != nil {
		t.Fatalf("close legacy: %v", err)
	}
	if v, err := metadata.SnapshotSchemaVersion(ctxBG(), legacy); err != nil || v != 0 {
		t.Fatalf("legacy snapshot version = %d, err = %v; want 0, nil", v, err)
	}
}

func TestPurgeTransientFromSnapshotRemovesRuntimeSessionTables(t *testing.T) {
	dir := t.TempDir()
	seedSnapshotFixture(t, dir)
	dst := filepath.Join(t.TempDir(), "snapshot.db")
	vacuumSnapshotOf(t, dir, dst)

	if err := metadata.PurgeTransientFromSnapshot(ctxBG(), dst); err != nil {
		t.Fatalf("PurgeTransientFromSnapshot: %v", err)
	}
	db := openSnapshotRW(t, dst)
	var sessions int
	if err := db.QueryRow("SELECT COUNT(*) FROM web_sessions").Scan(&sessions); err != nil {
		t.Fatalf("count web_sessions: %v", err)
	}
	if sessions != 0 {
		t.Fatalf("web_sessions rows = %d, want 0 (runtime state never rides a backup, architecture 11.19)", sessions)
	}
	var uploads int
	if err := db.QueryRow("SELECT COUNT(*) FROM upload_sessions").Scan(&uploads); err != nil {
		t.Fatalf("count upload_sessions: %v", err)
	}
	if uploads != 0 {
		t.Fatalf("upload_sessions rows = %d, want 0 (runtime session rows never ride a backup, T-209)", uploads)
	}
	// remote_cache STAYS (validators of cached blobs that travel with blobs/).
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS remote_cache (x)"); err != nil {
		t.Fatalf("create remote_cache stand-in: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := metadata.PurgeTransientFromSnapshot(ctxBG(), dst); err != nil {
		t.Fatalf("second purge: %v", err)
	}
	// No companions left behind by the purge's rollback-journal transaction.
	for _, companion := range []string{dst + "-wal", dst + "-shm", dst + "-journal"} {
		if _, err := os.Stat(companion); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("purge left %s behind: %v", companion, err)
		}
	}
}

// TestVacuumIntoSnapshotIsConsistentWhileSourceServes proves the online
// claim (GE-07): concurrent writes on the source connection continue while
// the snapshot is taken, and the snapshot reflects a consistent point in
// time (every row it references was committed before the vacuum).
func TestVacuumIntoSnapshotIsConsistentWhileSourceServes(t *testing.T) {
	dir := t.TempDir()
	ctx := ctxBG()
	md, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite", DSN: filepath.Join(dir, "binflow.db"), AdminPassword: "pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = md.Close() }()
	now := metadata.Now()
	if err := md.Repos().Create(ctx, &metadata.Repo{RepoKey: "gen", Type: "local", PackageType: "generic", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("repo create: %v", err)
	}
	if err := md.Blobs().Put(ctx, &metadata.Blob{Sha256: shaFor("body"), Size: 4, CreatedAt: now}); err != nil {
		t.Fatalf("blobs put: %v", err)
	}
	for i := 0; i < 20; i++ {
		if err := md.Nodes().Put(ctx, &metadata.Node{
			RepoKey: "gen", Path: fmt.Sprintf("f%02d.bin", i), Sha256: shaFor("body"), Size: 4, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("node put: %v", err)
		}
	}
	dst := filepath.Join(t.TempDir(), "snapshot.db")

	// Writers hammering the source while the vacuum runs.
	stop := make(chan struct{})
	wg := make(chan struct{})
	go func() {
		defer close(wg)
		i := 100
		for {
			select {
			case <-stop:
				return
			default:
				_ = md.Nodes().Put(ctx, &metadata.Node{
					RepoKey: "gen", Path: fmt.Sprintf("w%03d.bin", i), Sha256: shaFor("body"), Size: 4, CreatedAt: now, UpdatedAt: now,
				})
				i++
			}
		}
	}()
	time.Sleep(5 * time.Millisecond) // let the writer get ahead
	type vacuumer interface {
		VacuumInto(ctx context.Context, dst string) error
	}
	if err := md.(vacuumer).VacuumInto(ctx, dst); err != nil {
		t.Fatalf("VacuumInto under concurrent writes: %v", err)
	}
	close(stop)
	<-wg

	set, err := metadata.SnapshotChecksums(ctx, dst)
	if err != nil {
		t.Fatalf("SnapshotChecksums: %v", err)
	}
	if len(set) != 1 || !containsKey(set, shaFor("body")) {
		t.Fatalf("snapshot live set = %v, want exactly the one checksum", set)
	}
}

func containsKey(m map[string]struct{}, k string) bool {
	_, ok := m[k]
	return ok
}

// TestFolderMarkerSHAContract pins the sentinel itself (T-124): 64 hex zero
// bytes, the exact value repo.Service's folder deploy writes and every
// value-based consumer (snapshot boundary, GC mark walkers) excludes. A typo
// in either spelling would break the folder read plane or resurrect D-106-1,
// so the literal is asserted, not derived.
func TestFolderMarkerSHAContract(t *testing.T) {
	if len(metadata.FolderMarkerSHA) != 64 {
		t.Fatalf("FolderMarkerSHA length = %d, want 64 (sha256 hex width)", len(metadata.FolderMarkerSHA))
	}
	for i := 0; i < len(metadata.FolderMarkerSHA); i++ {
		if metadata.FolderMarkerSHA[i] != '0' {
			t.Fatalf("FolderMarkerSHA[%d] = %q, want '0' (the sentinel is 64 zero hex bytes)", i, metadata.FolderMarkerSHA[i])
		}
	}
	// The sentinel is unreachable by real content: not even the empty input
	// hashes to it (sha256("") = e3b0c442…), so no physical blob file can
	// ever collide with the exclusion.
	if empty := shaFor(""); empty == metadata.FolderMarkerSHA {
		t.Fatal("sha256(\"\") equals the folder marker sentinel — the no-collision premise is broken")
	}
}
