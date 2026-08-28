package storage

// Fail-open machinery for the dual-write MigrationEngine (ADR-0040, T-338).
//
// The steady state is unchanged: dual-write commits synchronously to disk AND
// S3. When the S3 arm fails on ANY operation (Begin/Append/Commit/Open/Stat —
// binary judgment, no error taxonomy: the S3-compatible ecosystem's error
// shapes are not reliable enough to classify), a process-internal circuit
// breaker opens a failure window:
//
//   - writes: BeginSession takes the disk-only fast path (no S3 dial, so no
//     per-PUT timeout penalty); sessions already mid-flight degrade to
//     disk-only at whichever arm point failed; Commit = disk commit (the
//     ADR-0006 protocol, untouched) + enqueue + SUCCESS. A committed disk
//     blob is NEVER rolled back because S3 failed (the migration.go reverse-
//     delete clause is abolished).
//   - reads: Open/Stat read disk directly (disk is the superset — the
//     migration direction is disk→S3 and disk copies are never auto-deleted,
//     so the fallback has no holes and read-your-writes holds).
//
// The debt is recorded in a durable replay queue (<data>/replay-queue/
// <sha256>.json, temp+fsync+rename atomic enqueue, per-sha filenames make
// enqueue idempotent). A drain worker copies queued blobs to S3 with bounded
// concurrency (migration.concurrency); its first successful copy is the
// half-open probe that closes the window, and after the queue empties a
// diskList×s3Set existence diff reconciles anything the queue missed (≤3
// convergence rounds, then a permanent WARN). GC and Delete keep failing
// honestly — fail-open covers the data plane only; the governance plane is
// retryable by its callers.
//
// Observability: the engine owns the counters behind the six replay metric
// families (ReplayStats — the cleanup.Stats precedent: /metrics reads them at
// scrape time, a scrape can never miss or double-count) and exposes the two
// audit facts (window open/close, drained) through the event callback the
// assembly layer registers (SetReplayEvents) — this package never imports
// audit (layering). The WARN/INFO lines (first window error, permanent
// failures, boot watermark) go to slog.Default, which the server assembly
// installs as the process logger.
//
// The breaker and the queue are dual-write-only organs: bypass and completed
// modes execute none of this code (zero new code paths there), and an
// assembly configured completed with a non-empty replay queue must refuse to
// boot (CheckCompletedReplayQueue — the assembly's fail-fast gate).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// replayQueueDirName is the replay queue directory directly under the data
// dir (the disk engine's root): <data>/replay-queue/<sha256>.json. The
// directory is engine-private DERIVED state — worst case (lost or corrupted)
// the idempotent migration re-scan rebuilds the diff — and is therefore not
// part of the version-compatibility promise (the ADR-0006 transient face).
const replayQueueDirName = "replay-queue"

// replayEntryVersion is the on-disk queue entry shape version. A field
// addition must bump it (the state.json posture).
const replayEntryVersion = 1

// Drain-loop constants (engine-internal — the binstore.yaml schema is frozen,
// ADR-0036; these are not configuration).
const (
	// replayBackoffStart is the initial inter-pass backoff of the drain loop.
	replayBackoffStart = time.Second
	// replayBackoffMax caps the exponential backoff.
	replayBackoffMax = 5 * time.Minute
	// replayPermanentAttempts marks an entry permanently failed after this
	// many consecutive failed passes. The entry is NOT deleted (no dead-letter
	// deletion): it stays queued until the S3 side is fixed and a later pass
	// drains it; the accounting (WARN + metric) fires once per entry.
	replayPermanentAttempts = 16
	// replayReconcileMaxRounds bounds the re-enqueue convergence loop after a
	// drained queue still shows missing blobs in the diskList×s3Set diff.
	replayReconcileMaxRounds = 3
)

// replayEntry is the persisted queue record.
type replayEntry struct {
	Version    int       `json:"version"`
	Sha256     string    `json:"sha256"`
	EnqueuedAt time.Time `json:"enqueued_at"`
}

// ReplayStats is the scrape-time snapshot behind the six replay metric
// families (ADR-0040 point 8): queue depth, drained/copied total, failed
// totals by outcome, read fallbacks served, and the open-window gauge.
type ReplayStats struct {
	// QueueDepth is the number of entries currently queued (gauge).
	QueueDepth int64
	// DrainedTotal counts blobs confirmed on S3 by the drain loop (counter).
	DrainedTotal int64
	// FailedRetry counts copy attempts that failed and will be retried
	// (counter, outcome=retry).
	FailedRetry int64
	// FailedPermanent counts entries that exhausted replayPermanentAttempts
	// consecutive passes (counter, outcome=permanent). The entries stay
	// queued; this is accounting, not eviction.
	FailedPermanent int64
	// SourceGone counts entries dropped because the source blob disappeared
	// from disk before the copy (GC reclaimed it — an unreferenced blob's S3
	// copy is not owed) (counter, outcome=source_gone).
	SourceGone int64
	// ReadFallbackTotal counts reads served from disk after S3 was tried and
	// missed or errored (counter). In-window direct-disk reads do not count —
	// no fallback happened.
	ReadFallbackTotal int64
	// WindowOpen reports whether the failure window is open (gauge 0/1).
	WindowOpen bool
}

// ReplayEventKind enumerates the engine facts the assembly layer mirrors into
// the audit trail (storage.replay.window / storage.replay.drained).
type ReplayEventKind string

const (
	// ReplayEventWindow reports a failure-window transition. FirstError
	// carries the S3-side error chain of the opening failure (empty on
	// close).
	ReplayEventWindow ReplayEventKind = "window"
	// ReplayEventDrained reports a drain episode that converged: the queue
	// emptied and the reconcile diff closed (or exhausted its rounds).
	ReplayEventDrained ReplayEventKind = "drained"
)

// ReplayEvent is one observable fail-open fact. The assembly facet maps it to
// the audit vocabulary; the engine emits it synchronously — callbacks must
// not block on I/O.
type ReplayEvent struct {
	Kind ReplayEventKind
	// Phase is "open" or "close" for window events.
	Phase string
	// FirstError is the S3 error chain that opened the window (window/open).
	FirstError string
	// Drained / SourceGone / PermanentFailed are this episode's totals
	// (drained event).
	Drained         int64
	SourceGone      int64
	PermanentFailed int64
	// ReconcileMissing is the blob count still missing from S3 after the
	// reconcile rounds ran out (drained event; 0 = converged clean).
	ReconcileMissing int
	// Rounds is the number of reconcile re-enqueue rounds this episode ran.
	Rounds int
}

// ReplayQueueDir returns the replay queue directory under dataDir (the same
// layout NewMigrationEngine derives from the disk engine's root). Exported
// for the assembly's completed-mode fail-fast gate and for operators.
func ReplayQueueDir(dataDir string) string {
	return filepath.Join(dataDir, replayQueueDirName)
}

// ErrCompletedWithReplayQueue is returned (wrapped) by
// CheckCompletedReplayQueue when the instance is assembled completed while
// the replay queue still holds undelivered blobs: declaring the migration
// finished while data never reached S3 is a visibility incident (the exact
// reverse of the divergence ADR-0036 guards against).
var ErrCompletedWithReplayQueue = errors.New("storage: replay queue not empty under completed migration")

// CheckCompletedReplayQueue is the assembly-time fail-fast gate (ADR-0040
// point 7): an instance configured storage.migration.completed=true must
// refuse to boot while <dataDir>/replay-queue holds entries. The error names
// both exits: switch back to dual-write and let the drain converge, or
// confirm the queue may be discarded and remove the directory. dataDir is the
// same root the disk engine was opened with (cfg.Storage.DataDir).
func CheckCompletedReplayQueue(dataDir string) error {
	entries, err := os.ReadDir(ReplayQueueDir(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("storage: check replay queue at %s: %w", ReplayQueueDir(dataDir), err)
	}
	n := 0
	for _, de := range entries {
		if de.IsDir() || strings.HasPrefix(de.Name(), ".") {
			continue
		}
		n++
	}
	if n == 0 {
		return nil
	}
	return fmt.Errorf("%w: %d entries in %s — switch storage.migration back to dual-write to drain, or confirm the queue may be discarded and remove the directory",
		ErrCompletedWithReplayQueue, n, ReplayQueueDir(dataDir))
}

// ---------------------------------------------------------------------------
// replayQueue — durable per-sha entry directory
// ---------------------------------------------------------------------------

// replayQueue is the durable replay queue. The on-disk truth is the
// <dir>/<sha256>.json files; index is the in-process mirror (single-instance
// premise, ADR-0040 §9 — no cross-process locking).
type replayQueue struct {
	dir string

	mu    sync.Mutex
	index map[string]struct{}
}

// openReplayQueue loads (or lazily creates) the queue at dir. Corrupt entries
// are salvaged by filename (the sha is the filename — derived state, worst
// case the migration re-scan rebuilds it); unparseable names are removed.
func openReplayQueue(dir string) (*replayQueue, error) {
	q := &replayQueue{dir: dir, index: make(map[string]struct{})}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("storage: replay queue: create %s: %w", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("storage: replay queue: scan %s: %w", dir, err)
	}
	for _, de := range entries {
		name := de.Name()
		if de.IsDir() || strings.HasPrefix(name, ".") {
			// Leftover temp files from an interrupted enqueue are not
			// entries; the atomic protocol means no half file was ever
			// visible under its final name.
			if !de.IsDir() && strings.HasPrefix(name, ".") {
				_ = os.Remove(filepath.Join(dir, name))
			}
			continue
		}
		sha := strings.TrimSuffix(name, ".json")
		if len(sha) != sha256HexLen || !isLowerHex(sha) {
			// Not an entry (foreign file): leave it alone — the queue
			// directory is engine-private, but deleting unknown files by
			// guesswork is worse than ignoring them.
			continue
		}
		q.index[sha] = struct{}{}
	}
	return q, nil
}

// isLowerHex reports whether s is entirely lowercase hexadecimal.
func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// enqueue adds sha to the queue: temp+fsync+rename(+dir fsync), the ADR-0006
// protocol. Idempotent — the per-sha filename means a repeat PUT of the same
// content does not grow the queue (the same dedup source as checksum
// addressing).
func (q *replayQueue) enqueue(sha256 string) error {
	q.mu.Lock()
	if _, ok := q.index[sha256]; ok {
		q.mu.Unlock()
		return nil
	}
	q.mu.Unlock()

	entry := replayEntry{Version: replayEntryVersion, Sha256: sha256, EnqueuedAt: time.Now().UTC()}
	blob, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("storage: replay queue: encode %s: %w", sha256, err)
	}
	final := q.path(sha256)
	tmp := filepath.Join(q.dir, ".tmp-"+sha256)
	if err := writeFileSync(tmp, blob, 0o600); err != nil {
		return fmt.Errorf("storage: replay queue: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("storage: replay queue: publish %s: %w", final, err)
	}
	if err := syncDir(q.dir); err != nil {
		return fmt.Errorf("storage: replay queue: sync %s: %w", q.dir, err)
	}

	q.mu.Lock()
	q.index[sha256] = struct{}{}
	q.mu.Unlock()
	return nil
}

// remove deletes sha's entry (only after the blob is confirmed on S3 or the
// source is gone — never as dead-letter eviction).
func (q *replayQueue) remove(sha256 string) {
	q.mu.Lock()
	delete(q.index, sha256)
	q.mu.Unlock()
	_ = os.Remove(q.path(sha256))
}

// snapshot returns the queued shas (a copy; order unspecified).
func (q *replayQueue) snapshot() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, 0, len(q.index))
	for sha := range q.index {
		out = append(out, sha)
	}
	return out
}

// depth returns the current entry count.
func (q *replayQueue) depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.index)
}

func (q *replayQueue) path(sha256 string) string {
	return filepath.Join(q.dir, sha256+".json")
}

// ---------------------------------------------------------------------------
// Fail-open engine state (embedded in MigrationEngine)
// ---------------------------------------------------------------------------

// failOpenState is the dual-write-only fail-open machinery: breaker, queue,
// drain worker and the observability counters. The zero value is inert (bypass
// and completed modes never initialize it).
type failOpenState struct {
	queue *replayQueue // nil when the disk engine's root cannot be derived

	// window is the circuit breaker: closed (false) = steady dual-write,
	// open (true) = failure window. The drain loop's first successful copy is
	// the half-open probe that closes it; there is no timer — S3's own
	// responses are the only recovery signal.
	windowMu   sync.Mutex
	windowOpen bool
	firstError string

	// worker lifecycle
	workerMu     sync.Mutex
	workerRun    bool
	workerCancel context.CancelFunc

	// events is the assembly facet (nil = nobody listens; the engine's own
	// logging and counters are unconditional).
	eventsMu sync.RWMutex
	events   func(ReplayEvent)

	// counters (atomic — read by ReplayStats at scrape time, bumped on the
	// request path and by the drain worker)
	drainedTotal    atomic.Int64
	failedRetry     atomic.Int64
	failedPermanent atomic.Int64
	sourceGone      atomic.Int64
	readFallback    atomic.Int64

	// drainLoop holds the engine-internal retry parameters (NOT
	// configuration — the binstore.yaml schema is frozen). Tests shrink
	// them to keep the permanent-failure legs fast; production reads the
	// package constants.
	drainLoop struct {
		backoffStart    time.Duration
		backoffMax      time.Duration
		permanentTries  int
		reconcileRounds int
	}
}

// drainDefaults fills the retry parameters with the production constants.
func (f *failOpenState) drainDefaults() {
	f.drainLoop.backoffStart = replayBackoffStart
	f.drainLoop.backoffMax = replayBackoffMax
	f.drainLoop.permanentTries = replayPermanentAttempts
	f.drainLoop.reconcileRounds = replayReconcileMaxRounds
}

// initFailOpen wires the fail-open state for a dual-write engine: load the
// durable queue, log the boot watermark and schedule the drain when entries
// survived a restart (AC-B). Foreign disk engines (not *engine) get a nil
// queue: writes still fail open in-process, but the debt does not survive a
// restart — every real assembly opens the disk engine through OpenEngine.
func (e *MigrationEngine) initFailOpen() {
	e.fo.drainDefaults()
	if de, ok := e.disk.(*engine); ok {
		q, err := openReplayQueue(filepath.Join(de.root, replayQueueDirName))
		if err != nil {
			// The blob store opened; a queue that cannot even be scanned is
			// a disk-level anomaly — fail open anyway (the queue is derived
			// state), loudly.
			slog.Error("storage: replay queue unavailable; in-window writes will not persist replay debt",
				"dir", ReplayQueueDir(de.root), "error", err.Error())
		} else {
			e.fo.queue = q
		}
	}
	depth := 0
	if e.fo.queue != nil {
		depth = e.fo.queue.depth()
	}
	if depth > 0 {
		slog.Info("storage: migration dual-write replay queue watermark",
			"depth", depth, "drain", "scheduled")
		e.ensureReplayWorker()
	}
}

// windowIsOpen reports whether the failure window is open.
func (f *failOpenState) windowIsOpen() bool {
	f.windowMu.Lock()
	defer f.windowMu.Unlock()
	return f.windowOpen
}

// tripWindow opens the failure window (idempotent within a window). The first
// error of the window is kept for the WARN line and the audit event — the
// error chain names the S3 side (permission typos and wrong buckets must stay
// visible even while reads keep succeeding from disk).
func (e *MigrationEngine) tripWindow(err error) {
	e.fo.windowMu.Lock()
	if e.fo.windowOpen {
		e.fo.windowMu.Unlock()
		return
	}
	e.fo.windowOpen = true
	e.fo.firstError = err.Error()
	e.fo.windowMu.Unlock()

	slog.Warn("storage: s3 arm failed; opening fail-open window (writes go disk-only + replay queue, reads go disk)",
		"first_error", e.fo.firstError)
	e.emitReplayEvent(ReplayEvent{Kind: ReplayEventWindow, Phase: "open", FirstError: e.fo.firstError})
}

// closeWindow closes the failure window after the drain loop's half-open
// probe (its first successful copy) proved S3 reachable again.
func (e *MigrationEngine) closeWindow() {
	e.fo.windowMu.Lock()
	if !e.fo.windowOpen {
		e.fo.windowMu.Unlock()
		return
	}
	e.fo.windowOpen = false
	e.fo.windowMu.Unlock()

	slog.Info("storage: s3 arm recovered; closing fail-open window, draining replay queue")
	e.emitReplayEvent(ReplayEvent{Kind: ReplayEventWindow, Phase: "close"})
}

// SetReplayEvents registers the assembly facet receiving the audit facts
// (storage.replay.window / storage.replay.drained). Pass nil to detach. The
// callback fires synchronously on request paths and drain passes — it must
// not block.
func (e *MigrationEngine) SetReplayEvents(fn func(ReplayEvent)) {
	e.fo.eventsMu.Lock()
	e.fo.events = fn
	e.fo.eventsMu.Unlock()
}

func (e *MigrationEngine) emitReplayEvent(ev ReplayEvent) {
	e.fo.eventsMu.RLock()
	fn := e.fo.events
	e.fo.eventsMu.RUnlock()
	if fn != nil {
		fn(ev)
	}
}

// ReplayStats returns the fail-open observability snapshot (dual-write only;
// the zero value otherwise). The assembly registers the six metric families
// and reads this at scrape time.
func (e *MigrationEngine) ReplayStats() ReplayStats {
	return ReplayStats{
		QueueDepth:        int64(e.fo.queueDepth()),
		DrainedTotal:      e.fo.drainedTotal.Load(),
		FailedRetry:       e.fo.failedRetry.Load(),
		FailedPermanent:   e.fo.failedPermanent.Load(),
		SourceGone:        e.fo.sourceGone.Load(),
		ReadFallbackTotal: e.fo.readFallback.Load(),
		WindowOpen:        e.fo.windowIsOpen(),
	}
}

func (f *failOpenState) queueDepth() int {
	if f.queue == nil {
		return 0
	}
	return f.queue.depth()
}

// enqueueReplay records replay debt for sha. Called from the Commit paths
// (degraded sessions and failed S3 commits) — never blocks the caller's
// success: a queue write failure is logged at ERROR (the blob is safe on
// disk; the debt resurfaces in the next drain episode's reconcile).
func (e *MigrationEngine) enqueueReplay(sha256 string) {
	if e.fo.queue == nil {
		// No durable queue (foreign disk engine): the debt is still owed —
		// account it as a retry so the depth gauge is honest about
		// in-process backlog and the reconcile pass of any later episode
		// re-derives it.
		e.fo.failedRetry.Add(1)
		return
	}
	if err := e.fo.queue.enqueue(sha256); err != nil {
		slog.Error("storage: replay queue enqueue failed; blob is safe on disk, reconcile will re-derive the debt",
			"sha256", sha256, "error", err.Error())
		return
	}
	e.ensureReplayWorker()
}

// ensureReplayWorker starts the drain worker when it is not already running
// and the engine is not closed.
func (e *MigrationEngine) ensureReplayWorker() {
	e.fo.workerMu.Lock()
	defer e.fo.workerMu.Unlock()
	if e.fo.workerRun {
		return
	}
	if e.isClosed() {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.fo.workerCancel = cancel
	e.fo.workerRun = true
	go e.replayWorker(ctx)
}

// stopReplayWorker cancels the drain worker (Close). Not waited for: the
// queue is durable, an interrupted pass resumes on the next boot.
func (e *MigrationEngine) stopReplayWorker() {
	e.fo.workerMu.Lock()
	defer e.fo.workerMu.Unlock()
	if e.fo.workerCancel != nil {
		e.fo.workerCancel()
		e.fo.workerCancel = nil
	}
}

// replayWorker drains the queue until it converges, then exits (restarted by
// the next enqueue). Pass structure:
//
//	entries → copy/confirm each (bounded concurrency) → all settled →
//	reconcile diskList×s3Set → missing re-enqueued (≤3 rounds) → converged →
//	drained event, exit.
//
// Failures back off exponentially (1s ×2, cap 5min), reset by any success;
// after replayPermanentAttempts consecutive failed passes an entry's failure
// is accounted permanent (WARN + counter, entry retained for retry).
func (e *MigrationEngine) replayWorker(ctx context.Context) {
	defer func() {
		e.fo.workerMu.Lock()
		e.fo.workerRun = false
		e.fo.workerCancel = nil
		e.fo.workerMu.Unlock()
	}()

	// Episode counters (the drained event's detail).
	var epDrained, epSourceGone, epPermanent int64
	attempts := make(map[string]int) // consecutive failed passes per entry
	permanentMarked := make(map[string]bool)
	rounds := 0 // reconcile rounds this episode
	backoff := e.fo.drainLoop.backoffStart

	for {
		if ctx.Err() != nil {
			return
		}
		entries := e.fo.queueSnapshot()
		if len(entries) == 0 {
			// Queue drained — reconcile (existence diff, the inventory path).
			missing, err := e.reconcileMissing(ctx)
			if err != nil {
				// S3 unreachable mid-reconcile: behave like a failed pass.
				e.tripWindow(err)
				if !e.sleepBackoff(ctx, backoff) {
					return
				}
				backoff = min(backoff*2, e.fo.drainLoop.backoffMax)
				continue
			}
			if len(missing) == 0 {
				e.emitReplayEvent(ReplayEvent{
					Kind:             ReplayEventDrained,
					Drained:          epDrained,
					SourceGone:       epSourceGone,
					PermanentFailed:  epPermanent,
					ReconcileMissing: 0,
					Rounds:           rounds,
				})
				slog.Info("storage: replay queue drained and reconciled",
					"drained", epDrained, "source_gone", epSourceGone,
					"permanent_failed", epPermanent, "rounds", rounds)
				return
			}
			rounds++
			if rounds > e.fo.drainLoop.reconcileRounds {
				// Convergence exhausted: permanent accounting + WARN; the
				// human hammer is the idempotent migration re-scan.
				e.fo.failedPermanent.Add(int64(len(missing)))
				epPermanent += int64(len(missing))
				slog.Warn("storage: replay reconcile did not converge; blobs still missing from s3 after max rounds (POST /api/v1/storage/migration/start re-scans idempotently)",
					"missing", len(missing), "rounds", rounds-1)
				e.emitReplayEvent(ReplayEvent{
					Kind:             ReplayEventDrained,
					Drained:          epDrained,
					SourceGone:       epSourceGone,
					PermanentFailed:  epPermanent,
					ReconcileMissing: len(missing),
					Rounds:           rounds - 1,
				})
				return
			}
			// Re-enqueue the diff and converge (both catch-up channels are
			// "copy only what S3 lacks" — idempotent with the background
			// migration scan).
			for _, sha := range missing {
				if e.fo.queue != nil {
					if err := e.fo.queue.enqueue(sha); err != nil {
						slog.Error("storage: replay queue enqueue failed during reconcile", "sha256", sha, "error", err.Error())
					}
				}
			}
			continue
		}

		succeeded, failed := e.drainPass(ctx, entries, attempts, permanentMarked, &epDrained, &epSourceGone, &epPermanent)
		if failed > 0 {
			if !e.sleepBackoff(ctx, backoff) {
				return
			}
			if succeeded > 0 {
				backoff = e.fo.drainLoop.backoffStart // any copy success resets
			} else {
				backoff = min(backoff*2, e.fo.drainLoop.backoffMax)
			}
		} else if succeeded > 0 {
			backoff = e.fo.drainLoop.backoffStart
		}
	}
}

// drainPass processes one snapshot of queue entries with bounded concurrency.
// attempts/permanentMarked carry the per-entry consecutive-failure state; the
// episode counters are updated in place. Returns the pass's succeeded and
// failed entry counts (the backoff policy inputs).
func (e *MigrationEngine) drainPass(ctx context.Context, entries []string, attempts map[string]int, permanentMarked map[string]bool, epDrained, epSourceGone, epPermanent *int64) (succeeded, failed int) {
	var mu sync.Mutex // guards attempts/permanentMarked, the episode counters and the return values
	var wg sync.WaitGroup
	jobs := make(chan string)

	workers := e.config.Concurrency
	if workers <= 0 {
		workers = 5
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sha := range jobs {
				if ctx.Err() != nil {
					return
				}
				err := e.copyBlob(ctx, sha)
				mu.Lock()
				switch {
				case err == nil:
					// The half-open probe succeeded: close the window on the
					// first proof of S3 reachability in this episode.
					e.closeWindow()
					// Count BEFORE removing: the removal (a queue-mutex
					// critical section) is the settle point an observer can
					// wait on — a depth of 0 must always imply the counters
					// have already landed.
					e.fo.drainedTotal.Add(1)
					if e.fo.queue != nil {
						e.fo.queue.remove(sha)
					}
					delete(attempts, sha)
					(*epDrained)++
					succeeded++
				case errors.Is(err, ErrBlobNotFound):
					// Source gone (GC reclaimed an unreferenced blob — its
					// S3 copy is not owed). Drop the entry — counter first,
					// removal last (the settle point; see above).
					e.fo.sourceGone.Add(1)
					if e.fo.queue != nil {
						e.fo.queue.remove(sha)
					}
					delete(attempts, sha)
					(*epSourceGone)++
				default:
					// S3-side failure: retry with backoff; permanent
					// accounting at the threshold (entry retained).
					e.fo.failedRetry.Add(1)
					e.tripWindow(err)
					attempts[sha]++
					n := attempts[sha]
					if n >= e.fo.drainLoop.permanentTries && !permanentMarked[sha] {
						permanentMarked[sha] = true
						e.fo.failedPermanent.Add(1)
						(*epPermanent)++
						slog.Warn("storage: replay entry exhausted consecutive attempts; retaining for retry until s3 recovers",
							"sha256", sha, "attempts", n, "last_error", err.Error())
					}
					failed++
				}
				mu.Unlock()
			}
		}()
	}
	for _, sha := range entries {
		select {
		case jobs <- sha:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return succeeded, failed
		}
	}
	close(jobs)
	wg.Wait()
	return succeeded, failed
}

// sleepBackoff waits d, reporting false when the worker should exit (engine
// closed or context done).
func (e *MigrationEngine) sleepBackoff(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// reconcileMissing computes the existence diff (disk × S3, gates bypassed —
// the inventory path the background migration already uses): blobs on disk
// that S3 still lacks, including anything a lost queue entry forgot.
func (e *MigrationEngine) reconcileMissing(ctx context.Context) ([]string, error) {
	diskList, err := e.diskList(ctx)
	if err != nil {
		return nil, fmt.Errorf("reconcile list disk: %w", err)
	}
	s3Set, err := e.s3Set(ctx)
	if err != nil {
		return nil, fmt.Errorf("reconcile list s3: %w", err)
	}
	var missing []string
	for _, sha := range diskList {
		if _, ok := s3Set[sha]; !ok {
			missing = append(missing, sha)
		}
	}
	return missing, nil
}

func (f *failOpenState) queueSnapshot() []string {
	if f.queue == nil {
		return nil
	}
	return f.queue.snapshot()
}
