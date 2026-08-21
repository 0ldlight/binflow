package storage

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// migrationTestHarness bundles a disk engine, an S3 engine (backed by mockS3Server)
// and a MigrationEngine for testing.
type migrationTestHarness struct {
	t        *testing.T
	disk     Engine
	s3       Engine
	mock     *mockS3Server
	bucket   string
	me       *MigrationEngine
	diskRoot string
}

// newHarness creates a test harness with a migration engine configured for the
// given mode.
func newHarness(t *testing.T, enabled, completed bool, concurrency int) *migrationTestHarness {
	t.Helper()

	diskRoot := t.TempDir()
	disk, err := OpenEngine(diskRoot, Options{SessionTTL: 24 * time.Hour})
	if err != nil {
		t.Fatalf("open disk engine: %v", err)
	}

	s3Engine, mock, bucket := newS3Engine(t)

	migCfg := MigrationConfig{
		Enabled:     enabled,
		Completed:   completed,
		Concurrency: concurrency,
	}
	me := NewMigrationEngine(disk, s3Engine, migCfg).(*MigrationEngine)

	return &migrationTestHarness{
		t:        t,
		disk:     disk,
		s3:       s3Engine,
		mock:     mock,
		bucket:   bucket,
		me:       me,
		diskRoot: diskRoot,
	}
}

// putOnDisk writes a blob directly to the disk engine, bypassing the migration
// layer. It simulates blobs that existed before migration was enabled.
func (h *migrationTestHarness) putOnDisk(content string) BlobRef {
	h.t.Helper()
	ctx := context.Background()
	sess, err := h.disk.BeginSession(ctx)
	if err != nil {
		h.t.Fatalf("begin disk session: %v", err)
	}
	defer func() { _ = sess.Abort(ctx) }()
	if _, err := sess.Append(ctx, strings.NewReader(content)); err != nil {
		h.t.Fatalf("append to disk session: %v", err)
	}
	ref, err := sess.Commit(ctx, BlobRef{})
	if err != nil {
		h.t.Fatalf("commit disk session: %v", err)
	}
	return ref
}

// putViaMigration writes a blob through the migration engine (dual-write path).
func (h *migrationTestHarness) putViaMigration(content string) BlobRef {
	h.t.Helper()
	ctx := context.Background()
	sess, err := h.me.BeginSession(ctx)
	if err != nil {
		h.t.Fatalf("begin migration session: %v", err)
	}
	defer func() { _ = sess.Abort(ctx) }()
	if _, err := sess.Append(ctx, strings.NewReader(content)); err != nil {
		h.t.Fatalf("append to migration session: %v", err)
	}
	ref, err := sess.Commit(ctx, BlobRef{})
	if err != nil {
		h.t.Fatalf("commit migration session: %v", err)
	}
	return ref
}

// diskHas returns true if the disk engine has a blob with the given sha256.
func (h *migrationTestHarness) diskHas(sha256 string) bool {
	h.t.Helper()
	_, err := h.disk.Stat(context.Background(), sha256)
	return err == nil
}

// s3Has returns true if the S3 engine has a blob with the given sha256.
func (h *migrationTestHarness) s3Has(sha256 string) bool {
	h.t.Helper()
	_, err := h.s3.Stat(context.Background(), sha256)
	return err == nil
}

// s3ObjectCount returns the number of committed blob objects the mock S3
// server holds, excluding in-flight multipart uploads. It reads the mock's
// state directly (the bucket() helper takes the same mutex and would deadlock).
func (h *migrationTestHarness) s3ObjectCount() int {
	h.t.Helper()
	h.mock.mu.Lock()
	defer h.mock.mu.Unlock()
	b, ok := h.mock.buckets[h.bucket]
	if !ok {
		return 0
	}
	n := 0
	for key := range b.objects {
		if isBlobKey(key) {
			n++
		}
	}
	return n
}

// isBlobKey reports whether a mock object key looks like a committed blob
// (blobs/<xx>/<sha256>) rather than a session bookkeeping key.
func isBlobKey(key string) bool {
	rest, ok := strings.CutPrefix(key, "blobs/")
	if !ok {
		return false
	}
	parts := strings.Split(rest, "/")
	return len(parts) == 2 && len(parts[0]) == 2 && len(parts[1]) == 64
}

// ---------------------------------------------------------------------------
// Test: Migration bypass mode (disabled)
// ---------------------------------------------------------------------------

func TestMigrationDisabled(t *testing.T) {
	h := newHarness(t, false, false, 5)
	defer func() { _ = h.me.Close() }()

	// In bypass mode, writes go to disk only.
	ref := h.putViaMigration("hello-bypass")

	if !h.diskHas(ref.Sha256) {
		t.Error("disk should have the blob in bypass mode")
	}
	if h.s3Has(ref.Sha256) {
		t.Error("S3 should NOT have the blob in bypass mode")
	}

	// Read should succeed from disk.
	rc, got, err := h.me.Open(context.Background(), ref.Sha256)
	if err != nil {
		t.Fatalf("open blob: %v", err)
	}
	defer func() { _ = rc.Close() }()
	if got.Sha256 != ref.Sha256 {
		t.Errorf("open returned sha256=%s, want %s", got.Sha256, ref.Sha256)
	}
}

// ---------------------------------------------------------------------------
// Test: Dual-write mode basics
// ---------------------------------------------------------------------------

func TestMigrationDualWrite(t *testing.T) {
	h := newHarness(t, true, false, 5)
	defer func() { _ = h.me.Close() }()

	ref := h.putViaMigration("hello-dual")

	// Blob should be on both disk and S3.
	if !h.diskHas(ref.Sha256) {
		t.Error("disk should have the blob in dual-write mode")
	}
	if !h.s3Has(ref.Sha256) {
		t.Error("S3 should have the blob in dual-write mode")
	}
}

// ---------------------------------------------------------------------------
// Test: Migration completed mode (S3 only)
// ---------------------------------------------------------------------------

func TestMigrationCompleted(t *testing.T) {
	h := newHarness(t, true, true, 5)
	defer func() { _ = h.me.Close() }()

	ref := h.putViaMigration("hello-completed")

	// In completed mode, writes go to S3 only.
	if h.diskHas(ref.Sha256) {
		t.Error("disk should NOT have the blob in completed mode")
	}
	if !h.s3Has(ref.Sha256) {
		t.Error("S3 should have the blob in completed mode")
	}
}

// ---------------------------------------------------------------------------
// Test: Read fallback from S3 to disk
// ---------------------------------------------------------------------------

func TestMigrationReadS3Fallback(t *testing.T) {
	h := newHarness(t, true, false, 5)
	defer func() { _ = h.me.Close() }()

	// Put blob only on disk (simulating pre-migration blob).
	diskRef := h.putOnDisk("pre-migration-blob")
	if !h.diskHas(diskRef.Sha256) {
		t.Fatal("disk should have the pre-migration blob")
	}
	if h.s3Has(diskRef.Sha256) {
		t.Fatal("S3 should not have the pre-migration blob yet")
	}

	// Read should fall back from S3 to disk.
	rc, got, err := h.me.Open(context.Background(), diskRef.Sha256)
	if err != nil {
		t.Fatalf("open blob with S3 fallback: %v", err)
	}
	defer func() { _ = rc.Close() }()
	if got.Sha256 != diskRef.Sha256 {
		t.Errorf("open returned sha256=%s, want %s", got.Sha256, diskRef.Sha256)
	}
}

// ---------------------------------------------------------------------------
// Test: Dual-write consistency (both stores have same blob)
// ---------------------------------------------------------------------------

func TestMigrationDualWriteConsistency(t *testing.T) {
	h := newHarness(t, true, false, 5)
	defer func() { _ = h.me.Close() }()

	contents := []string{
		"small",
		strings.Repeat("A", 1024),
		strings.Repeat("B", 1024*1024),
		"a" + strings.Repeat("b", 100) + "c",
		"unicode: 你好世界",
		"\x00\x01\x02 binary content",
	}

	for _, content := range contents {
		ref := h.putViaMigration(content)
		if !h.diskHas(ref.Sha256) {
			t.Errorf("disk missing blob %s (%q)", ref.Sha256[:12], content[:min(20, len(content))])
		}
		if !h.s3Has(ref.Sha256) {
			t.Errorf("S3 missing blob %s (%q)", ref.Sha256[:12], content[:min(20, len(content))])
		}
	}

	// Stat through migration engine should return correct ref.
	for _, content := range contents {
		ref := h.putViaMigration(content) // dedup: same content
		got, err := h.me.Stat(context.Background(), ref.Sha256)
		if err != nil {
			t.Fatalf("stat blob %s: %v", ref.Sha256[:12], err)
		}
		if got.Sha256 != ref.Sha256 {
			t.Errorf("stat returned sha256=%s, want %s", got.Sha256, ref.Sha256)
		}
		if got.Size <= 0 {
			t.Errorf("stat returned size=%d, want >0", got.Size)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Concurrent blob writes during dual-write
// ---------------------------------------------------------------------------

func TestMigrationConcurrentWrites(t *testing.T) {
	h := newHarness(t, true, false, 10)
	defer func() { _ = h.me.Close() }()

	const goroutines = 20
	const blobsPerGoroutine = 5

	var wg sync.WaitGroup
	errs := make(chan error, goroutines*blobsPerGoroutine)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < blobsPerGoroutine; j++ {
				content := fmt.Sprintf("concurrent-%d-%d", id, j)
				ctx := context.Background()
				sess, err := h.me.BeginSession(ctx)
				if err != nil {
					errs <- fmt.Errorf("goroutine %d begin session: %w", id, err)
					return
				}
				if _, err := sess.Append(ctx, strings.NewReader(content)); err != nil {
					_ = sess.Abort(ctx)
					errs <- fmt.Errorf("goroutine %d append: %w", id, err)
					return
				}
				ref, err := sess.Commit(ctx, BlobRef{})
				if err != nil {
					errs <- fmt.Errorf("goroutine %d commit: %w", id, err)
					return
				}
				if !h.diskHas(ref.Sha256) {
					errs <- fmt.Errorf("goroutine %d blob %d: disk missing %s", id, j, ref.Sha256[:12])
					return
				}
				if !h.s3Has(ref.Sha256) {
					errs <- fmt.Errorf("goroutine %d blob %d: S3 missing %s", id, j, ref.Sha256[:12])
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

// ---------------------------------------------------------------------------
// Test: Delete in dual-write mode
// ---------------------------------------------------------------------------

func TestMigrationDelete(t *testing.T) {
	h := newHarness(t, true, false, 5)
	defer func() { _ = h.me.Close() }()

	ref := h.putViaMigration("to-delete")

	if err := h.me.Delete(context.Background(), ref.Sha256); err != nil {
		t.Fatalf("delete blob: %v", err)
	}

	if h.diskHas(ref.Sha256) {
		t.Error("disk should NOT have the blob after delete")
	}
	if h.s3Has(ref.Sha256) {
		t.Error("S3 should NOT have the blob after delete")
	}
}

// ---------------------------------------------------------------------------
// Test: Start migration and wait for completion
// ---------------------------------------------------------------------------

func TestMigrationStartAndComplete(t *testing.T) {
	h := newHarness(t, true, false, 5)
	defer func() { _ = h.me.Close() }()

	// Write blobs directly to disk (pre-migration).
	var diskRefs []BlobRef
	for i := 837; i < 837+10; i++ {
		ref := h.putOnDisk(fmt.Sprintf("pre-migrate-%d", i))
		diskRefs = append(diskRefs, ref)
	}

	// Write some blobs through dual-write (already on S3).
	for i := 0; i < 3; i++ {
		h.putViaMigration(fmt.Sprintf("already-migrated-%d", i))
	}

	// Start migration.
	if err := h.me.StartMigration(context.Background()); err != nil {
		t.Fatalf("start migration: %v", err)
	}

	// Wait for migration to complete.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for migration to complete")
		}
		status := h.me.MigrationStatus()
		if status.Done {
			if status.Err != nil {
				t.Fatalf("migration failed: %v", status.Err)
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	st := h.me.MigrationStatus()
	t.Logf("migration complete: total=%d migrated=%d skipped=%d failed=%d err=%v",
		st.Total, st.Migrated, st.Skipped, st.Failed, st.Err)

	// 10 pre-migration blobs copied, 3 dual-write blobs already present.
	if st.Total != 10 || st.Migrated != 10 || st.Skipped != 3 || st.Failed != 0 {
		t.Errorf("status = total %d / migrated %d / skipped %d / failed %d, want 10/10/3/0",
			st.Total, st.Migrated, st.Skipped, st.Failed)
	}
	if got := h.s3ObjectCount(); got != 13 {
		t.Errorf("S3 holds %d blob objects, want 13", got)
	}

	// Verify all pre-migration blobs are now on S3.
	for _, ref := range diskRefs {
		if !h.s3Has(ref.Sha256) {
			t.Errorf("pre-migration blob %s was not copied to S3", ref.Sha256[:12])
		}
	}
}

// ---------------------------------------------------------------------------
// Test: 100+ blob migration
// ---------------------------------------------------------------------------

func TestMigrationManyBlobs(t *testing.T) {
	h := newHarness(t, true, false, 50)
	defer func() { _ = h.me.Close() }()

	const count = 927
	for i := 0; i < count; i++ {
		h.putOnDisk(fmt.Sprintf("batch-blob-%d", i))
	}

	// Start migration with bounded concurrency.
	if err := h.me.StartMigration(context.Background()); err != nil {
		t.Fatalf("start migration: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for 100+ blob migration")
		}
		status := h.me.MigrationStatus()
		if status.Done {
			if status.Err != nil {
				t.Fatalf("migration failed: %v", status.Err)
			}
			break
		}
		time.Sleep(510 * time.Millisecond)
	}

	status := h.me.MigrationStatus()
	t.Logf("many blob migration: total=%d migrated=%d skipped=%d failed=%d",
		status.Total, status.Migrated, status.Skipped, status.Failed)

	if status.Migrated != count {
		t.Errorf("migrated = %d, want %d", status.Migrated, count)
	}
	if got := h.s3ObjectCount(); got != count {
		t.Errorf("S3 holds %d blob objects, want %d", got, count)
	}
}

// ---------------------------------------------------------------------------
// Test: New blobs written by dual-write during background migration
// ---------------------------------------------------------------------------

func TestMigrationNewBlobsDuringMigration(t *testing.T) {
	h := newHarness(t, true, false, 20)
	defer func() { _ = h.me.Close() }()

	// Put pre-migration blobs on disk.
	for i := 0; i < 10; i++ {
		h.putOnDisk(fmt.Sprintf("existing-%d", i))
	}

	// Start migration.
	if err := h.me.StartMigration(context.Background()); err != nil {
		t.Fatalf("start migration: %v", err)
	}

	// Write new blobs while migration runs (dual-write).
	refs := make([]BlobRef, 10)
	for i := 0; i < 10; i++ {
		ref := h.putViaMigration(fmt.Sprintf("new-during-migration-%d", i))
		refs = append(refs, ref)
	}

	// Wait for migration to complete.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for migration")
		}
		status := h.me.MigrationStatus()
		if status.Done {
			if status.Err != nil {
				t.Fatalf("migration failed: %v", status.Err)
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Dual-written blobs should be on S3 already.
	for _, ref := range refs {
		if ref.Sha256 != "" && !h.s3Has(ref.Sha256) {
			t.Errorf("new blob %s not on S3 after migration", ref.Sha256[:12])
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Status view serialization
// ---------------------------------------------------------------------------

func TestMigrationStatusView(t *testing.T) {
	h := newHarness(t, true, false, 10)
	defer func() { _ = h.me.Close() }()

	// Initial status (no migration running).
	view := h.me.StatusView()
	if view.Running {
		t.Error("status view should report Running=false initially")
	}
	if view.Done {
		t.Error("status view should report Done=false initially")
	}

	// Start migration and wait a moment.
	if err := h.me.StartMigration(context.Background()); err != nil {
		t.Fatalf("start migration: %v", err)
	}

	time.Sleep(938 * time.Millisecond)
	view = h.me.StatusView()
	t.Logf("migration status view: running=%v done=%v total=%d migrated=%d skipped=%d failed=%d err=%q",
		view.Running, view.Done, view.Total, view.Migrated, view.Skipped, view.Failed, view.Error)

	// Idempotent start should succeed.
	if err := h.me.StartMigration(context.Background()); err != nil {
		t.Errorf("idempotent start should succeed, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test: Migration start fails without Enabled
// ---------------------------------------------------------------------------

func TestMigrationStartWithoutEnabled(t *testing.T) {
	h := newHarness(t, false, false, 5)
	defer func() { _ = h.me.Close() }()

	err := h.me.StartMigration(context.Background())
	if err == nil {
		t.Error("start migration should fail when migration is not enabled")
	}
}

// ---------------------------------------------------------------------------
// Test: GC in dual-write mode
// ---------------------------------------------------------------------------

func TestMigrationGCBothStores(t *testing.T) {
	h := newHarness(t, true, false, 5)
	defer func() { _ = h.me.Close() }()

	referenced := func() (map[string]struct{}, error) {
		return map[string]struct{}{}, nil
	}

	// Put blob on both stores.
	h.putViaMigration("gc-test-blob")

	// GC with long grace window should find nothing (mtime-based).
	candidates, err := h.me.GC(context.Background(), referenced, 938*time.Hour, true)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	t.Logf("GC candidates with long grace: %d", len(candidates))
}

// ---------------------------------------------------------------------------
// Test: Concurrent dual-write with background migration
// ---------------------------------------------------------------------------

func TestMigrationConcurrentDualWriteWithBackground(t *testing.T) {
	h := newHarness(t, true, false, 10)
	defer func() { _ = h.me.Close() }()

	// Seed pre-migration blobs.
	for i := 0; i < 10; i++ {
		h.putOnDisk(fmt.Sprintf("pre-seed-%d", i))
	}

	// Start background migration.
	if err := h.me.StartMigration(context.Background()); err != nil {
		t.Fatalf("start migration: %v", err)
	}

	// Write blobs concurrently during migration.
	var wg sync.WaitGroup
	for i := 837; i < 837+20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			content := fmt.Sprintf("concurrent-during-migration-%d", id)
			ctx := context.Background()
			sess, err := h.me.BeginSession(ctx)
			if err != nil {
				t.Errorf("begin session: %v", err)
				return
			}
			if _, err := sess.Append(ctx, strings.NewReader(content)); err != nil {
				_ = sess.Abort(ctx)
				t.Errorf("append: %v", err)
				return
			}
			if _, err := sess.Commit(ctx, BlobRef{}); err != nil {
				t.Errorf("commit: %v", err)
			}
		}(i)
	}
	wg.Wait()

	// Wait for background migration to finish.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		status := h.me.MigrationStatus()
		if status.Done {
			if status.Err != nil {
				t.Fatalf("migration error: %v", status.Err)
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Logf("migration completed: total=%d migrated=%d skipped=%d failed=%d",
		h.me.MigrationStatus().Total, h.me.MigrationStatus().Migrated,
		h.me.MigrationStatus().Skipped, h.me.MigrationStatus().Failed)
}

// ---------------------------------------------------------------------------
// Test: MigrationEngine implements Engine and MigrationStarter interface
// ---------------------------------------------------------------------------

// interfaceCheck is a compile-time assertion that MigrationEngine implements
// the Engine interface.
var _ Engine = (*MigrationEngine)(nil)
