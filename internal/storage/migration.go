package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

// MigrationEngine is a disk-to-S3 online migration wrapper. It implements the
// Engine interface and operates in one of three modes:
//
//   - Bypass (migration disabled or completed): delegates all operations to the
//     active backend (disk or S3) and exposes no migration surface.
//   - Dual-write (migration enabled, not completed): writes go to both disk and
//     S3; reads check S3 first with disk fallback; deletes go to both.
//   - Migration-complete (completed flag set): delegates to S3 only; the
//     background migration goroutine has finished.
//
// The background migration is initiated by StartMigration(ctx) and copies
// existing blobs from the local DiskEngine to the S3Engine using SkipIfExists
// idempotent semantics. It is restart-safe: on restart it re-lists both
// sources and copies only what is missing from S3.
//
// MigrationEngine is safe for concurrent use.
type MigrationEngine struct {
	disk   Engine
	s3     Engine
	config MigrationConfig

	// Background migration state
	migMu     sync.Mutex
	migCtx    context.Context
	migCancel context.CancelFunc
	migDone   chan struct{}
	statusGrd statusGuard // protects the migration status snapshot

	mu     sync.RWMutex
	closed bool
}

// MigrationConfig holds the migration behaviour parameters.
type MigrationConfig struct {
	// Enabled toggles dual-write mode and background migration.
	Enabled bool
	// Completed signals that the background migration has finished. When both
	// Enabled and Completed are true, the engine delegates to S3 only.
	Completed bool
	// Concurrency bounds the number of goroutines the background migration
	// uses to copy blobs from disk to S3. Must be >= 1 when Enabled is true.
	Concurrency int
}

// MigrationStatusView is the JSON-serializable summary of migration progress.
// It is a snapshot of MigrationStatus with error and times converted to strings.
type MigrationStatusView struct {
	Running    bool   `json:"running"`
	Done       bool   `json:"done"`
	Total      int64  `json:"total"`
	Migrated   int64  `json:"migrated"`
	Skipped    int64  `json:"skipped"`
	Failed     int64  `json:"failed"`
	Error      string `json:"error,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// MigrationStatus is the live progress of a background migration run.
type MigrationStatus struct {
	// Running is true while a migration is in progress.
	Running bool
	// Total is the number of blobs on disk when the migration started.
	Total int64
	// Migrated is the number of blobs successfully copied to S3 so far.
	Migrated int64
	// Skipped is the number of blobs that already existed on S3.
	Skipped int64
	// Failed is the number of blobs that could not be copied.
	Failed int64
	// Done is true when the migration has completed (success or failure).
	Done bool
	// Err is the first error encountered during migration (nil if successful).
	Err error
	// StartedAt is when the migration began.
	StartedAt time.Time
	// FinishedAt is when the migration ended (zero if still running).
	FinishedAt time.Time
}

// statusGuard provides concurrently safe access to the migration status.
type statusGuard struct {
	mu sync.Mutex
	s  *MigrationStatus
}

func (g *statusGuard) loadSnapshot() *MigrationStatus {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.s == nil {
		return &MigrationStatus{}
	}
	cp := *g.s
	return &cp
}

func (g *statusGuard) storeSnapshot(s *MigrationStatus) {
	g.mu.Lock()
	defer g.mu.Unlock()
	cp := *s
	g.s = &cp
}

// updateSnapshotAtomically calls fn with the current live status (held under
// the mutex) and stores the result. fn must not retain the pointer.
func (g *statusGuard) updateSnapshotAtomically(fn func(*MigrationStatus)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.s == nil {
		g.s = &MigrationStatus{}
	}
	fn(g.s)
	// Store a copy so consumers always get a snapshot.
	cp := *g.s
	g.s = &cp
}

// NewMigrationEngine wraps the given disk and S3 engines with migration logic.
// When migration is disabled, it delegates to the disk engine only.
// When migration is enabled, it enters dual-write mode.
// When migration is completed, it delegates to S3 only.
func NewMigrationEngine(disk Engine, s3 Engine, cfg MigrationConfig) Engine {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 5
	}
	return &MigrationEngine{
		disk:   disk,
		s3:     s3,
		config: cfg,
	}
}

// activeEngine returns the engine(s) to use for the current operation.
// Dual-write writes to both; reads check S3 first with disk fallback.
func (e *MigrationEngine) activeEngine(read bool) (primary, fallback Engine) {
	e.mu.RLock()
	cfg := e.config
	e.mu.RUnlock()

	if cfg.Completed {
		// Migration complete: S3 is the source of truth.
		if read {
			return e.s3, nil
		}
		return e.s3, nil
	}
	if cfg.Enabled {
		// Dual-write mode: writes go to both, reads check S3 first.
		if read {
			return e.s3, e.disk
		}
		return e.disk, e.s3 // writes: primary=disk, fallback=S3 (both written)
	}
	// Bypass: disk only.
	return e.disk, nil
}

func (e *MigrationEngine) checkOpen() error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed {
		return ErrEngineClosed
	}
	return nil
}

// ---------------------------------------------------------------------------
// Engine interface
// ---------------------------------------------------------------------------

// BeginSession creates a new upload session. In dual-write mode, it creates
// sessions on both disk and S3 so that all data flows to both backends.
// When migration is disabled or completed, it delegates to the active engine.
func (e *MigrationEngine) BeginSession(ctx context.Context) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("storage: migration: begin session: %w", err)
	}
	if err := e.checkOpen(); err != nil {
		return nil, fmt.Errorf("storage: migration: begin session: %w", err)
	}

	e.mu.RLock()
	cfg := e.config
	e.mu.RUnlock()

	if cfg.Enabled && !cfg.Completed {
		// Dual-write mode: create sessions on both engines.
		diskSess, err := e.disk.BeginSession(ctx)
		if err != nil {
			return nil, fmt.Errorf("storage: migration: begin disk session: %w", err)
		}
		s3Sess, err := e.s3.BeginSession(ctx)
		if err != nil {
			_ = diskSess.Abort(ctx)
			return nil, fmt.Errorf("storage: migration: begin S3 session: %w", err)
		}
		return &migrationSession{
			disk: diskSess,
			s3:   s3Sess,
			eng:  e,
		}, nil
	}

	primary, _ := e.activeEngine(false)
	return primary.BeginSession(ctx)
}

// ResumeSession delegates to the primary engine.
func (e *MigrationEngine) ResumeSession(ctx context.Context, id string) (Session, error) {
	primary, _ := e.activeEngine(false)
	return primary.ResumeSession(ctx, id)
}

// Open reads a blob. In dual-write mode, it checks S3 first and falls back to
// disk if the blob is not yet on S3.
func (e *MigrationEngine) Open(ctx context.Context, sha256 string) (io.ReadCloser, BlobRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, BlobRef{}, fmt.Errorf("storage: migration: open blob: %w", err)
	}

	primary, fallback := e.activeEngine(true)

	rc, ref, err := primary.Open(ctx, sha256)
	if err == nil {
		return rc, ref, nil
	}

	if fallback == nil || !errors.Is(err, ErrBlobNotFound) {
		return nil, BlobRef{}, err
	}

	// Fallback to disk.
	return fallback.Open(ctx, sha256)
}

// Stat verifies a blob. In dual-write mode, it checks S3 first with disk
// fallback.
func (e *MigrationEngine) Stat(ctx context.Context, sha256 string) (BlobRef, error) {
	if err := ctx.Err(); err != nil {
		return BlobRef{}, fmt.Errorf("storage: migration: stat blob: %w", err)
	}

	primary, fallback := e.activeEngine(true)

	ref, err := primary.Stat(ctx, sha256)
	if err == nil {
		return ref, nil
	}

	if fallback == nil || !errors.Is(err, ErrBlobNotFound) {
		return BlobRef{}, err
	}

	return fallback.Stat(ctx, sha256)
}

// Delete removes a blob from both backends in dual-write mode.
func (e *MigrationEngine) Delete(ctx context.Context, sha256 string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("storage: migration: delete blob: %w", err)
	}
	if err := e.checkOpen(); err != nil {
		return fmt.Errorf("storage: migration: delete blob %s: %w", sha256, err)
	}

	e.mu.RLock()
	cfg := e.config
	e.mu.RUnlock()

	if cfg.Completed {
		return e.s3.Delete(ctx, sha256)
	}

	if cfg.Enabled {
		// Delete from both; prefer S3 error if both fail.
		errS3 := e.s3.Delete(ctx, sha256)
		errDisk := e.disk.Delete(ctx, sha256)
		if errS3 != nil && !errors.Is(errS3, ErrBlobNotFound) {
			return errS3
		}
		if errDisk != nil && !errors.Is(errDisk, ErrBlobNotFound) {
			return errDisk
		}
		return nil
	}

	return e.disk.Delete(ctx, sha256)
}

// GC runs mark-sweep on the active backend(s). In dual-write mode, it runs on
// both.
func (e *MigrationEngine) GC(ctx context.Context, referenced func() (map[string]struct{}, error), grace time.Duration, apply bool) ([]string, error) {
	if err := e.checkOpen(); err != nil {
		return nil, fmt.Errorf("storage: migration: gc: %w", err)
	}

	e.mu.RLock()
	cfg := e.config
	e.mu.RUnlock()

	if cfg.Completed {
		return e.s3.GC(ctx, referenced, grace, apply)
	}

	if cfg.Enabled {
		// Run GC on both backends; merge results.
		diskCandidates, errDisk := e.disk.GC(ctx, referenced, grace, apply)
		s3Candidates, errS3 := e.s3.GC(ctx, referenced, grace, apply)

		merged := mergeCandidates(diskCandidates, s3Candidates)
		if errDisk != nil {
			return merged, errDisk
		}
		if errS3 != nil {
			return merged, errS3
		}
		return merged, nil
	}

	return e.disk.GC(ctx, referenced, grace, apply)
}

// Close shuts down both engines.
func (e *MigrationEngine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.mu.Unlock()

	// Stop any running migration.
	e.migMu.Lock()
	if e.migCancel != nil {
		e.migCancel()
	}
	e.migMu.Unlock()

	var firstErr error
	if err := e.disk.Close(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("storage: migration: close disk: %w", err)
	}
	if err := e.s3.Close(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("storage: migration: close s3: %w", err)
	}
	return firstErr
}

// ---------------------------------------------------------------------------
// Background migration
// ---------------------------------------------------------------------------

// StartMigration initiates a background migration from disk to S3. It is
// idempotent: if a migration is already running, it returns nil and the
// caller can query MigrationStatus(). The migration copies blobs that exist
// on disk but not on S3 using bounded concurrency.
func (e *MigrationEngine) StartMigration(ctx context.Context) error {
	e.migMu.Lock()
	defer e.migMu.Unlock()

	if e.migCancel != nil {
		// Migration is already running.
		return nil
	}

	e.mu.RLock()
	enabled := e.config.Enabled
	e.mu.RUnlock()

	if !enabled {
		return errors.New("storage: migration: migration is not enabled")
	}

	migCtx, cancel := context.WithCancel(ctx)
	e.migCtx = migCtx
	e.migCancel = cancel
	e.migDone = make(chan struct{})

	e.mu.RLock()
	concurrency := e.config.Concurrency
	e.mu.RUnlock()
	if concurrency <= 0 {
		concurrency = 5
	}

	status := &MigrationStatus{
		Running:   true,
		StartedAt: time.Now(),
	}
	e.statusGrd.storeSnapshot(status)

	go e.runMigration(migCtx, concurrency, status)

	return nil
}

// MigrationStatus returns the live status of the background migration.
func (e *MigrationEngine) MigrationStatus() *MigrationStatus {
	return e.statusGrd.loadSnapshot()
}

// StatusView returns a JSON-serializable snapshot of the migration progress.
// It is safe for concurrent use and can be called from any goroutine.
func (e *MigrationEngine) StatusView() *MigrationStatusView {
	s := e.statusGrd.loadSnapshot()
	v := &MigrationStatusView{
		Running:  s.Running,
		Done:     s.Done,
		Total:    s.Total,
		Migrated: s.Migrated,
		Skipped:  s.Skipped,
		Failed:   s.Failed,
	}
	if s.Err != nil {
		v.Error = s.Err.Error()
	}
	if !s.StartedAt.IsZero() {
		v.StartedAt = s.StartedAt.Format(time.RFC3339)
	}
	if !s.FinishedAt.IsZero() {
		v.FinishedAt = s.FinishedAt.Format(time.RFC3339)
	}
	return v
}

// runMigration is the background goroutine that copies blobs from disk to S3.
// All status updates go through statusGuard to ensure readers see consistent
// snapshots. The status parameter is only used for the initial Running/StartedAt
// fields which are set before the goroutine starts.
func (e *MigrationEngine) runMigration(ctx context.Context, concurrency int, _ *MigrationStatus) {
	finish := func(err error) {
		e.statusGrd.updateSnapshotAtomically(func(s *MigrationStatus) {
			s.Running = false
			s.Done = true
			s.FinishedAt = time.Now()
			if err != nil {
				s.Err = err
			}
		})
		e.migMu.Lock()
		if e.migCancel != nil {
			e.migCancel()
			e.migCancel = nil
		}
		close(e.migDone)
		e.migMu.Unlock()
	}

	defer func() {
		if r := recover(); r != nil {
			finish(fmt.Errorf("panic: %v", r))
		}
	}()

	// List blobs from both backends.
	diskList, err := e.diskList(ctx)
	if err != nil {
		finish(fmt.Errorf("list disk blobs: %w", err))
		return
	}

	s3Set, err := e.s3Set(ctx)
	if err != nil {
		finish(fmt.Errorf("list S3 blobs: %w", err))
		return
	}

	// Compute missing blobs (on disk but not on S3).
	var missing []string
	for _, sha := range diskList {
		if _, ok := s3Set[sha]; !ok {
			missing = append(missing, sha)
		}
	}

	e.statusGrd.updateSnapshotAtomically(func(s *MigrationStatus) {
		s.Total = int64(len(missing))
		s.Skipped = int64(len(diskList) - len(missing))
	})

	if len(missing) == 0 {
		finish(nil)
		return
	}

	// Copy missing blobs with bounded concurrency.
	jobs := make(chan string, len(missing))
	for _, sha := range missing {
		jobs <- sha
	}
	close(jobs)

	var wg sync.WaitGroup
	var failedMu sync.Mutex
	var firstErr error

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sha := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}

				if err := e.copyBlob(ctx, sha); err != nil {
					e.statusGrd.updateSnapshotAtomically(func(s *MigrationStatus) {
						s.Failed++
					})
					failedMu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("copy blob %s: %w", sha, err)
					}
					failedMu.Unlock()
				} else {
					e.statusGrd.updateSnapshotAtomically(func(s *MigrationStatus) {
						s.Migrated++
					})
				}
			}
		}()
	}

	wg.Wait()
	finish(firstErr)
}

// copyBlob copies a single blob from disk to S3. It uses the Engine interface
// (Open on disk, then a session on S3) to stream the blob without buffering
// it entirely in memory.
func (e *MigrationEngine) copyBlob(ctx context.Context, sha256 string) error {
	// Check if blob already exists on S3 (idempotent skip).
	// Use Open + Close instead of Stat because Stat does a full hash-verification
	// read, and in some backends the "not found" error wrapping differs.
	rc, _, err := e.s3.Open(ctx, sha256)
	if err == nil {
		_ = rc.Close()
		return nil
	}
	if !errors.Is(err, ErrBlobNotFound) {
		return err
	}

	// Open the blob from disk.
	rc, _, err = e.disk.Open(ctx, sha256)
	if err != nil {
		return fmt.Errorf("open from disk: %w", err)
	}
	defer func() { _ = rc.Close() }()

	// Write to S3 via a session.
	sess, err := e.s3.BeginSession(ctx)
	if err != nil {
		return fmt.Errorf("begin S3 session: %w", err)
	}
	defer sess.Abort(ctx) //nolint:errcheck // cleanup

	if _, err := sess.Append(ctx, rc); err != nil {
		return fmt.Errorf("append to S3 session: %w", err)
	}

	_, err = sess.Commit(ctx, BlobRef{Sha256: sha256})
	if err != nil {
		return fmt.Errorf("commit to S3: %w", err)
	}

	return nil
}

// diskList returns all blob sha256 values on disk.
func (e *MigrationEngine) diskList(ctx context.Context) ([]string, error) {
	// Use the disk engine's GC with an empty referenced set and no grace period
	// to get all blobs. Passing 1 nanosecond effectively skips the grace period
	// (a zero grace triggers DefaultGCGrace of 24h per the Engine contract).
	allDisk := func() (map[string]struct{}, error) {
		return map[string]struct{}{}, nil
	}
	candidates, err := e.disk.GC(ctx, allDisk, 1, false)
	if err != nil {
		return nil, err
	}
	return candidates, nil
}

// s3Set returns a set of all blob sha256 values on S3.
func (e *MigrationEngine) s3Set(ctx context.Context) (map[string]struct{}, error) {
	allS3 := func() (map[string]struct{}, error) {
		return map[string]struct{}{}, nil
	}
	candidates, err := e.s3.GC(ctx, allS3, 1, false)
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(candidates))
	for _, sha := range candidates {
		set[sha] = struct{}{}
	}
	return set, nil
}

// mergeCandidates returns the union of two sorted sha256 candidate lists.
func mergeCandidates(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for _, s := range a {
		seen[s] = struct{}{}
	}
	for _, s := range b {
		seen[s] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for s := range seen {
		result = append(result, s)
	}
	sort.Strings(result)
	return result
}

// ---------------------------------------------------------------------------
// migrationSession — dual-write session wrapper
// ---------------------------------------------------------------------------

// migrationSession wraps a disk session and an S3 session so that Appends flow
// to both backends and Commit finalizes both atomically. It implements the
// Session interface and is used by MigrationEngine in dual-write mode.
type migrationSession struct {
	disk Session
	s3   Session
	eng  *MigrationEngine
}

// ID returns the disk session's ID — the caller needs a single identifier and
// the disk session is the primary because its ID is used for ResumeSession
// (which always delegates to disk).
func (s *migrationSession) ID() string {
	return s.disk.ID()
}

// Append streams r into both the disk and S3 sessions. The reader is consumed
// once via io.TeeReader so memory usage is constant regardless of blob size.
// If either backend fails, both sessions are poisoned and the error is returned.
func (s *migrationSession) Append(ctx context.Context, r io.Reader) (written int64, err error) {
	// Use a pipe to tee the data to both sessions concurrently.
	pr, pw := io.Pipe()
	tee := io.TeeReader(r, pw)

	// Read from tee into disk, which writes to S3 via the pipe.
	type result struct {
		w   int64
		err error
	}
	ch := make(chan result, 1)

	go func() {
		w, err := s.s3.Append(ctx, pr)
		ch <- result{w, err}
	}()

	diskWritten, diskErr := s.disk.Append(ctx, tee)
	// Close the pipe writer — if the disk append succeeded, the S3 reader
	// has everything it needs; if it failed, closing pw signals the S3 goroutine
	// to stop. The close error carries no extra information beyond diskErr.
	_ = pw.Close()

	res := <-ch

	if diskErr != nil {
		// Disk failed — the S3 goroutine gets an error from the pipe too.
		// Abort the S3 session to clean up partial multipart upload.
		_ = s.s3.Abort(ctx)
		return 0, diskErr
	}
	if res.err != nil {
		// S3 failed — abort the disk session to clean up.
		_ = s.disk.Abort(ctx)
		return 0, res.err
	}

	// Both sides must agree on the cumulative offset.
	if diskWritten != res.w {
		_ = s.disk.Abort(ctx)
		_ = s.s3.Abort(ctx)
		return 0, fmt.Errorf("storage: migration: session %s: disk and S3 offsets disagree: disk=%d s3=%d", s.ID(), diskWritten, res.w)
	}

	return diskWritten, nil
}

// Commit finalizes both sessions. It commits the disk session first to get the
// authoritative BlobRef (which carries the sha256), then commits the S3 session
// with the same expected ref. If the S3 commit fails, the disk blob is cleaned
// up to maintain consistency: the caller sees an error as if the commit never
// happened.
func (s *migrationSession) Commit(ctx context.Context, expect BlobRef) (BlobRef, error) {
	ref, err := s.disk.Commit(ctx, expect)
	if err != nil {
		_ = s.s3.Abort(ctx)
		return BlobRef{}, err
	}

	// Commit to S3 with the exact sha256 we got from disk.
	_, err = s.s3.Commit(ctx, BlobRef{Sha256: ref.Sha256})
	if err != nil {
		// S3 commit failed — clean up disk to maintain consistency.
		_ = s.eng.disk.Delete(context.Background(), ref.Sha256)
		return BlobRef{}, fmt.Errorf("storage: migration: s3 commit failed (disk blob rolled back): %w", err)
	}

	return ref, nil
}

// Abort discards both sessions. It is idempotent and nil-receiver safe.
func (s *migrationSession) Abort(ctx context.Context) error {
	if s == nil {
		return nil
	}
	errDisk := s.disk.Abort(ctx)
	errS3 := s.s3.Abort(ctx)
	if errDisk != nil {
		return errDisk
	}
	return errS3
}
