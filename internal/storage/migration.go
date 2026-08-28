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
// In dual-write mode the engine additionally carries the ADR-0040 fail-open
// machinery: an S3 failure window degrades writes to disk-only + replay queue
// (never blocking or rolling back the disk commit) and reads to disk directly;
// recovery drains the queue and reconciles the disk×S3 existence diff. See
// failopen.go. Bypass and completed modes never touch that state.
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

	// fo is the dual-write fail-open state (ADR-0040); inert otherwise.
	fo failOpenState

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
	Running bool `json:"running"`
	Done    bool `json:"done"`
	// Total mirrors MigrationStatus.Total: the blobs this run must copy
	// (on disk, not yet on S3) — the denominator of the console progress
	// bar, not the instance's whole blob count.
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
	// Total is the number of blobs THIS RUN must still copy: those found on
	// disk but not yet on S3 when the run's scan finished (len(missing)). It
	// is not the whole on-disk blob count — blobs already present on S3
	// count toward Skipped instead, and Total shrinks to 0 across restarts
	// as the idempotent re-scan skips what earlier runs copied.
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
//
// In dual-write mode it also arms the fail-open machinery (ADR-0040): the
// durable replay queue is loaded from <data>/replay-queue and — when entries
// survived a restart — the drain worker starts immediately (the boot
// watermark INFO line reports the depth).
func NewMigrationEngine(disk Engine, s3 Engine, cfg MigrationConfig) Engine {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 5
	}
	e := &MigrationEngine{
		disk:   disk,
		s3:     s3,
		config: cfg,
	}
	if cfg.Enabled && !cfg.Completed {
		e.initFailOpen()
	}
	return e
}

// isClosed reports the engine's closed flag (fail-open worker gate).
func (e *MigrationEngine) isClosed() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.closed
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
//
// ADR-0040 fail-open: while the failure window is open the session takes the
// disk-only fast path (no S3 dial — no per-PUT timeout penalty) and its Commit
// enqueues the replay debt. When the S3 arm fails to begin in the steady
// state, the session degrades the same way instead of failing the PUT: the
// disk arm is the floor, and the queue catches S3 up after recovery.
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
		if e.fo.windowIsOpen() {
			// Failure window: disk-only fast path, debt enqueued at Commit.
			return &migrationSession{disk: diskSess, eng: e}, nil
		}
		s3Sess, err := e.s3.BeginSession(ctx)
		if err != nil {
			// S3 arm failed to begin: open the window and degrade this
			// session to disk-only — the PUT proceeds (fail-open), never
			// 500s with zero disk bytes.
			e.tripWindow(err)
			return &migrationSession{disk: diskSess, eng: e}, nil
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
// disk. ADR-0040: the fallback condition is ANY S3 error (a miss is normal
// migration lag; an errored S3 additionally trips the failure window), and
// while the window is open reads go straight to disk — the superset, so the
// fallback has no holes and read-your-writes holds.
func (e *MigrationEngine) Open(ctx context.Context, sha256 string) (io.ReadCloser, BlobRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, BlobRef{}, fmt.Errorf("storage: migration: open blob: %w", err)
	}

	e.mu.RLock()
	cfg := e.config
	e.mu.RUnlock()

	if cfg.Enabled && !cfg.Completed {
		if e.fo.windowIsOpen() {
			return e.disk.Open(ctx, sha256)
		}
		rc, ref, err := e.s3.Open(ctx, sha256)
		if err == nil {
			return rc, ref, nil
		}
		if !errors.Is(err, ErrBlobNotFound) {
			e.tripWindow(err)
		}
		e.fo.readFallback.Add(1)
		return e.disk.Open(ctx, sha256)
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
// fallback — the mirror of Open's ADR-0040 posture (the checksum-deploy /
// X-Checksum-Deploy path rides Stat).
func (e *MigrationEngine) Stat(ctx context.Context, sha256 string) (BlobRef, error) {
	if err := ctx.Err(); err != nil {
		return BlobRef{}, fmt.Errorf("storage: migration: stat blob: %w", err)
	}

	e.mu.RLock()
	cfg := e.config
	e.mu.RUnlock()

	if cfg.Enabled && !cfg.Completed {
		if e.fo.windowIsOpen() {
			return e.disk.Stat(ctx, sha256)
		}
		ref, err := e.s3.Stat(ctx, sha256)
		if err == nil {
			return ref, nil
		}
		if !errors.Is(err, ErrBlobNotFound) {
			e.tripWindow(err)
		}
		e.fo.readFallback.Add(1)
		return e.disk.Stat(ctx, sha256)
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

// GC implements the legacy face: adapt and delegate to GCSweep (ADR-0031).
func (e *MigrationEngine) GC(ctx context.Context, referenced func() (map[string]struct{}, error), grace time.Duration, apply bool) ([]string, error) {
	if referenced == nil {
		return nil, errors.New("storage: migration: gc: referenced callback is nil")
	}
	return e.GCSweep(ctx, ReferencedFunc(referenced), grace, apply)
}

// GCSweep runs mark-sweep on the active backend(s). In dual-write mode it
// runs on both, passing the SAME marker down — m.Mark() is therefore invoked
// once per backend pass (plus the apply-phase refresh for legacy-form
// markers); the GCMarker contract allows multiple calls. Each backend
// applies its own hold set and delete gates: dual-write sessions register
// the sha on both engines at Commit, and each engine's sweep protects what
// it would delete (see ReleaseGCHold below).
func (e *MigrationEngine) GCSweep(ctx context.Context, m GCMarker, grace time.Duration, apply bool) ([]string, error) {
	if err := e.checkOpen(); err != nil {
		return nil, fmt.Errorf("storage: migration: gc: %w", err)
	}

	e.mu.RLock()
	cfg := e.config
	e.mu.RUnlock()

	if cfg.Completed {
		return e.s3.GCSweep(ctx, m, grace, apply)
	}

	if cfg.Enabled {
		// Run GC on both backends; merge results.
		diskCandidates, errDisk := e.disk.GCSweep(ctx, m, grace, apply)
		s3Candidates, errS3 := e.s3.GCSweep(ctx, m, grace, apply)

		merged := mergeCandidates(diskCandidates, s3Candidates)
		if errDisk != nil {
			return merged, errDisk
		}
		if errS3 != nil {
			return merged, errS3
		}
		return merged, nil
	}

	return e.disk.GCSweep(ctx, m, grace, apply)
}

// SweepExpiredSessions implements SessionSweeper (T-324): the open-time
// reclamation rerun on the maintenance clock, fanned out with GCSweep's
// mode shape — completed mode delegates to S3, dual-write runs BOTH
// backends (each sweeps its own rows and backend state; the count sums the
// session rows), bypass mode the disk engine alone. The wrapped engines
// are interface-typed, so the capability is asserted per backend and a
// backend without it is a LOUD error, never a silent skip.
func (e *MigrationEngine) SweepExpiredSessions(ctx context.Context) (int, error) {
	sweepOf := func(name string, eng Engine) (int, error) {
		sw, ok := eng.(SessionSweeper)
		if !ok {
			return 0, fmt.Errorf("storage: migration: sweep sessions: %s backend lacks the SessionSweeper capability", name)
		}
		return sw.SweepExpiredSessions(ctx)
	}
	if err := e.checkOpen(); err != nil {
		return 0, fmt.Errorf("storage: migration: sweep sessions: %w", err)
	}

	e.mu.RLock()
	cfg := e.config
	e.mu.RUnlock()

	if cfg.Completed {
		return sweepOf("s3", e.s3)
	}
	if cfg.Enabled {
		diskN, errDisk := sweepOf("disk", e.disk)
		s3N, errS3 := sweepOf("s3", e.s3)
		if errDisk != nil {
			return diskN + s3N, fmt.Errorf("storage: migration: sweep sessions (disk): %w", errDisk)
		}
		if errS3 != nil {
			return diskN + s3N, fmt.Errorf("storage: migration: sweep sessions (s3): %w", errS3)
		}
		return diskN + s3N, nil
	}
	return sweepOf("disk", e.disk)
}

// ReleaseGCHold implements Engine.ReleaseGCHold across both wrapped engines.
// A dual-write Commit registers the sha on disk AND S3 (two independent hold
// sets), so the release must reach both; in bypass and completed modes one
// side releases an unknown sha, which is a no-op. The disk engine's own
// release is infallible, so in practice this never errors — the wrap keeps
// the seam honest for future backends.
func (e *MigrationEngine) ReleaseGCHold(sha256 string) error {
	var firstErr error
	if err := e.disk.ReleaseGCHold(sha256); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("storage: migration: release gc hold (disk): %w", err)
	}
	if err := e.s3.ReleaseGCHold(sha256); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("storage: migration: release gc hold (s3): %w", err)
	}
	return firstErr
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

	// Stop the fail-open drain worker (not waited for: the replay queue is
	// durable, an interrupted pass resumes on the next boot — ADR-0028's
	// close posture takes no new obligation here).
	e.stopReplayWorker()

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

// diskList returns all blob sha256 values on disk, bypassing every GC
// candidacy gate (holds included — the migration inventory is a diff input,
// not a reclamation decision; a sha held by an in-flight upload must still
// be copied if it predates dual-write, and the restart re-scan catches
// anything a live hold defers).
func (e *MigrationEngine) diskList(ctx context.Context) ([]string, error) {
	return listAllBlobs(ctx, e.disk)
}

// s3Set returns a set of all blob sha256 values on S3 (gates bypassed, same
// as diskList).
func (e *MigrationEngine) s3Set(ctx context.Context) (map[string]struct{}, error) {
	candidates, err := listAllBlobs(ctx, e.s3)
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(candidates))
	for _, sha := range candidates {
		set[sha] = struct{}{}
	}
	return set, nil
}

// listAllBlobs enumerates every blob in eng's store with the hold gate and
// the grace window disabled. The concrete engines expose an internal
// gcList; a foreign Engine implementation falls back to the legacy GC face
// with an empty set and a sub-second grace, where holds still apply — the
// conservative direction for an assembly this package does not know.
func listAllBlobs(ctx context.Context, eng Engine) ([]string, error) {
	switch e := eng.(type) {
	case *engine:
		return e.gcList(ctx)
	case *S3Engine:
		return e.gcList(ctx)
	default:
		return eng.GC(ctx, func() (map[string]struct{}, error) {
			return map[string]struct{}{}, nil
		}, time.Nanosecond, false)
	}
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
// to both backends and Commit finalizes both. It implements the Session
// interface and is used by MigrationEngine in dual-write mode.
//
// ADR-0040 fail-open: an S3 arm that fails at ANY point of the session's life
// (begin, mid-Append, commit) is aborted and abandoned — the session degrades
// to disk-only (s3 == nil) and keeps going; Commit then enqueues the replay
// debt and succeeds. The disk arm is the floor: only a disk failure fails the
// caller, honestly.
type migrationSession struct {
	disk Session
	s3   Session // nil once degraded to disk-only
	eng  *MigrationEngine
}

// ID returns the disk session's ID — the caller needs a single identifier and
// the disk session is the primary because its ID is used for ResumeSession
// (which always delegates to disk).
func (s *migrationSession) ID() string {
	return s.disk.ID()
}

// Offset returns the disk session's offset: the disk side is the primary (its
// ID names the session, and ResumeSession delegates to disk), and Append
// already enforces that both backends agree on the cumulative offset, so the
// disk value is the pair's shared truth.
func (s *migrationSession) Offset() int64 {
	return s.disk.Offset()
}

// Append streams r into the session. In the steady state the reader is
// consumed once via io.TeeReader into both backends so memory usage is
// constant regardless of blob size. A failed S3 arm aborts and detaches —
// the disk side keeps streaming and the session degrades to disk-only
// (fail-open); a failed disk arm poisons the session and returns the error
// (the floor).
func (s *migrationSession) Append(ctx context.Context, r io.Reader) (written int64, err error) {
	if s.s3 == nil {
		// Degraded (window fast path, failed S3 begin, or an earlier append
		// detached the arm): plain disk append.
		return s.disk.Append(ctx, r)
	}

	// Use a pipe to tee the data to both sessions concurrently.
	pr, pw := io.Pipe()
	feed := &detachWriter{w: pw}
	tee := io.TeeReader(r, feed)

	// Read from tee into disk, which writes to S3 via the pipe.
	type result struct {
		w   int64
		err error
	}
	ch := make(chan result, 1)

	go func() {
		w, err := s.s3.Append(ctx, pr)
		// Whether the S3 arm succeeded or failed, the pipe is finished:
		// detaching first stops the tee from feeding it any further bytes,
		// and the close releases a write that is already blocked inside the
		// pipe (an abandoned reader would otherwise deadlock it).
		feed.detach()
		_ = pw.Close()
		ch <- result{w, err}
	}()

	diskWritten, diskErr := s.disk.Append(ctx, tee)
	// Close the pipe writer — if the disk append succeeded, the S3 reader
	// has everything it needs; if it failed, closing pw signals the S3 goroutine
	// to stop. The close error carries no extra information beyond diskErr.
	_ = pw.Close()

	res := <-ch

	if diskErr != nil {
		// Disk failed — the floor. The S3 goroutine gets an error from the
		// pipe too; abort its session to clean up partial multipart upload.
		_ = s.s3.Abort(ctx)
		return 0, diskErr
	}
	if res.err != nil {
		// S3 arm failed mid-flight: abort it, open the failure window and
		// degrade to disk-only — this Append SUCCEEDS on the disk bytes
		// already written; the replay queue carries the S3 debt.
		_ = s.s3.Abort(ctx)
		s.s3 = nil
		s.eng.tripWindow(res.err)
		return diskWritten, nil
	}

	// Both sides must agree on the cumulative offset. A disagreement means
	// the S3 copy cannot be trusted — degrade the same way (abort the S3
	// arm, keep the disk truth; the queued re-copy re-reads disk).
	if diskWritten != res.w {
		_ = s.s3.Abort(ctx)
		s.s3 = nil
		s.eng.tripWindow(fmt.Errorf("storage: migration: session %s: disk and S3 offsets disagree: disk=%d s3=%d", s.ID(), diskWritten, res.w))
		return diskWritten, nil
	}

	return diskWritten, nil
}

// detachWriter wraps the pipe the tee feeds: once the S3 arm is gone, writes
// become no-ops so the disk side of the tee never observes the S3 arm's
// death. Fail-open means the S3 arm's problems must never fail the disk
// append.
type detachWriter struct {
	mu   sync.Mutex
	dead bool
	w    io.Writer
}

func (d *detachWriter) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.dead {
		return len(p), nil
	}
	if _, err := d.w.Write(p); err != nil {
		// The pipe broke underneath us (the S3 reader vanished mid-write).
		// Swallow: the S3 goroutine reports the real error through ch.
		d.dead = true
	}
	return len(p), nil
}

func (d *detachWriter) detach() {
	d.mu.Lock()
	d.dead = true
	d.mu.Unlock()
}

// Commit finalizes the session: disk first (the ADR-0006 commit protocol,
// untouched — that blob is the caller's success), then S3 with the exact
// sha256 disk produced. ADR-0040: an S3 commit failure NEVER rolls the disk
// blob back — the debt is enqueued, the failure window opens and the caller
// sees success (the reverse-delete clause is abolished). Only a disk commit
// failure fails the caller. A degraded session commits disk-only and
// enqueues.
func (s *migrationSession) Commit(ctx context.Context, expect BlobRef) (BlobRef, error) {
	ref, err := s.disk.Commit(ctx, expect)
	if err != nil {
		if s.s3 != nil {
			_ = s.s3.Abort(ctx)
		}
		return BlobRef{}, err
	}

	if s.s3 == nil {
		// Degraded session: the blob is safe on disk; queue the replay debt.
		s.eng.enqueueReplay(ref.Sha256)
		return ref, nil
	}

	// Commit to S3 with the exact sha256 we got from disk.
	if _, err := s.s3.Commit(ctx, BlobRef{Sha256: ref.Sha256}); err != nil {
		s.eng.tripWindow(err)
		s.eng.enqueueReplay(ref.Sha256)
		return ref, nil
	}

	return ref, nil
}

// Abort discards both sessions. It is idempotent and nil-receiver safe.
func (s *migrationSession) Abort(ctx context.Context) error {
	if s == nil {
		return nil
	}
	errDisk := s.disk.Abort(ctx)
	if s.s3 != nil {
		if errS3 := s.s3.Abort(ctx); errDisk == nil {
			errDisk = errS3
		}
	}
	return errDisk
}
