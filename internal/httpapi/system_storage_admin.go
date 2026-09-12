package httpapi

// The /api/system/storage maintenance family (LOOP 003 / docs/reverse/
// storage/prune-gc-admin.md E1-E10 — the Artifactory StorageResource
// plane, mounted under the /binflow prefix like the rest of the compatible
// API): prune/start|stop|status (202 async job + persisted 26-field
// status report), gc (200 synchronous text/plain dot stream), optimize
// (202 empty), compress (200 stream over the metadata VACUUM), backup
// (immediate-schedule trigger), size/info (read faces) and exportds
// (the deprecated constant failure).
//
// The kernels are the existing engines — no second storage path: prune and
// the gc stream ride storage.Prune (the GCSweep gates reused per shard
// directory), compress rides RunMetadataCompress, backup rides the
// scheduler's backup runner through the Deps seam. The REST additions are
// the async job frame (the prune manager), the persisted report schema
// and the wire forms.
//
// Divergences against the spec that are BinFlow's honest mapping (the
// diff report carries them):
//   - path prefix: /binflow/api/system/storage/** (no root mirror, E-26);
//   - the prune grace is the configured storage.gc_grace_hours window
//     (Artifactory's own eligibility threshold is UNKNOWN — spec §3.4 —
//     and BinFlow's grace is the standing conservative window);
//   - a report persisted as "running" by a process that then died is
//     rewritten "stopped" on first load (restart recovery; unspecified in
//     the source behavior).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// BackupRunner schedules one backup fire — the seam the scheduler's
// backup-domain runner (cmd's backupRunner) satisfies structurally: the
// SAME kernel a cron fire rides, wired into Deps so the REST trigger is
// never a second executor.
type BackupRunner interface {
	Run(ctx context.Context, key string) error
}

// ---- the persisted prune report (spec §2.2 — 26 fields / 4 objects) ----

// pruneReportState is the persisted half of the status report: the last
// run's snapshot. Artifactory lands it in the configs store
// (key=STORAGE_PRUNE_REPORT); BinFlow's honest equivalent is one JSON
// document in the data directory, written atomically (temp + fsync +
// rename), surviving restarts.
type pruneReportState struct {
	Status                 string         `json:"status"` // running|stopped|finished|error
	DryRun                 bool           `json:"dryRun"`
	StartedAt              time.Time      `json:"startedAt"`
	LastUpdated            time.Time      `json:"lastUpdated"`
	Progress               int            `json:"progress"` // directories enumerated this run (of 256)
	TotalBinariesProcessed int64          `json:"totalBinariesProcessed"`
	TotalBinariesCleaned   int64          `json:"totalBinariesCleaned"`
	TotalBytesCleaned      int64          `json:"totalBytesCleaned"`
	LastDir                *pruneDirState `json:"lastHandledDirectory"`
}

// pruneDirState is one directory's slice of the persisted report.
type pruneDirState struct {
	Name              string    `json:"name"`
	Status            string    `json:"status"` // finished|stopped
	BinariesProcessed int64     `json:"binariesProcessed"`
	BinariesCleaned   int64     `json:"binariesCleaned"`
	BytesCleaned      int64     `json:"bytesCleaned"`
	StartedAt         time.Time `json:"startedAt"`
	FinishedAt        time.Time `json:"finishedAt"`
}

// pruneTimingView is the six-field timing block (top level and
// lastHandledDirectory share the structure — spec §2.2).
type pruneTimingView struct {
	StartedAtMillis   int64  `json:"startedAtMillis"`
	StartedAt         string `json:"startedAt"`
	DurationMillis    int64  `json:"durationMillis"`
	Duration          string `json:"duration"`
	LastUpdatedMillis int64  `json:"lastUpdatedMillis"`
	LastUpdated       string `json:"lastUpdated"`
}

// pruneReportView is the E3 response body: the field set and order of the
// live instance's finished/running/stopped reports.
type pruneReportView struct {
	Status               string          `json:"status"`
	DryRun               bool            `json:"dryRun"`
	Timing               pruneTimingView `json:"timing"`
	Progress             string          `json:"progress"`
	Report               pruneTotalsView `json:"report"`
	LastHandledDirectory *pruneDirView   `json:"lastHandledDirectory"`
}

// pruneTotalsView is the three-field report block.
type pruneTotalsView struct {
	TotalBinariesProcessed int64 `json:"totalBinariesProcessed"`
	TotalBinariesCleaned   int64 `json:"totalBinariesCleaned"`
	TotalBytesCleaned      int64 `json:"totalBytesCleaned"`
}

// pruneDirView is the five-field lastHandledDirectory block plus its
// embedded timing.
type pruneDirView struct {
	Name              string          `json:"name"`
	Status            string          `json:"status"`
	BinariesProcessed int64           `json:"binariesProcessed"`
	BinariesCleaned   int64           `json:"binariesCleaned"`
	BytesCleaned      int64           `json:"bytesCleaned"`
	Timing            pruneTimingView `json:"timing"`
}

// pruneClock renders the no-timezone local timestamp (yyyy-MM-dd'T'HH:mm:ss,
// spec §2.2's observed format).
func pruneClock(t time.Time) string { return t.Format("2006-01-02T15:04:05") }

// pruneDuration renders the HH:mm:ss.SSS form.
func pruneDuration(d time.Duration) string {
	d = d.Truncate(time.Millisecond)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	ms := int(d.Milliseconds()) % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}

func pruneTimingViewOf(started, updated time.Time) pruneTimingView {
	return pruneTimingView{
		StartedAtMillis:   started.UnixMilli(),
		StartedAt:         pruneClock(started),
		DurationMillis:    updated.Sub(started).Milliseconds(),
		Duration:          pruneDuration(updated.Sub(started)),
		LastUpdatedMillis: updated.UnixMilli(),
		LastUpdated:       pruneClock(updated),
	}
}

// renderPruneReport projects the persisted state onto the wire schema.
func renderPruneReport(st *pruneReportState) pruneReportView {
	v := pruneReportView{
		Status:   st.Status,
		DryRun:   st.DryRun,
		Timing:   pruneTimingViewOf(st.StartedAt, st.LastUpdated),
		Progress: fmt.Sprintf("%d of %d", st.Progress, storage.PruneShardCount),
		Report: pruneTotalsView{
			TotalBinariesProcessed: st.TotalBinariesProcessed,
			TotalBinariesCleaned:   st.TotalBinariesCleaned,
			TotalBytesCleaned:      st.TotalBytesCleaned,
		},
	}
	if st.LastDir != nil {
		v.LastHandledDirectory = &pruneDirView{
			Name:              st.LastDir.Name,
			Status:            st.LastDir.Status,
			BinariesProcessed: st.LastDir.BinariesProcessed,
			BinariesCleaned:   st.LastDir.BinariesCleaned,
			BytesCleaned:      st.LastDir.BytesCleaned,
			Timing:            pruneTimingViewOf(st.LastDir.StartedAt, st.LastDir.FinishedAt),
		}
	}
	return v
}

// ---- the prune manager: single-flight job, stop marker, persistence ----

// pruneManager owns the PUD job's process-side state: the single-flight
// flag (a second start answers the spec's 412), the stop marker (stop
// requests land even while idle, E4) and the persisted report.
type pruneManager struct {
	mu      sync.Mutex
	loaded  bool
	running bool
	stopReq bool
	state   *pruneReportState

	path string
	log  interface {
		WarnContext(ctx context.Context, msg string, args ...any)
	}
	now func() time.Time
}

func newPruneManager(dataDir string, log interface {
	WarnContext(ctx context.Context, msg string, args ...any)
}) *pruneManager {
	// Single-process assumption (review N4): the report, the single-flight
	// flag and the stop marker are per-process state — two BinFlow
	// processes sharing one data directory sit outside the standing
	// deployment boundary (the datalock still serializes their maintenance
	// passes; neither sees the other's marker or running flag).
	return &pruneManager{
		path: filepath.Join(dataDir, "prune_report.json"),
		log:  log,
		now:  time.Now,
	}
}

// load reads the persisted report once. A report left "running" by a dead
// process is rewritten "stopped": the owning task cannot exist anymore
// (restart recovery — BinFlow-side rule, the source behavior is
// unspecified for mid-run restarts).
func (m *pruneManager) load() {
	if m.loaded {
		return
	}
	m.loaded = true
	raw, err := os.ReadFile(m.path)
	if err != nil {
		return // never ran: no report, the 412 "No Prune task found" state
	}
	var st pruneReportState
	if err := json.Unmarshal(raw, &st); err != nil {
		m.log.WarnContext(context.Background(), "httpapi: prune report unreadable; ignoring",
			"path", m.path, "error", err.Error())
		return
	}
	if st.Status == "running" {
		st.Status = "stopped"
		m.persist(&st, true)
	}
	m.state = &st
}

// persist writes the report atomically: temp file + rename, so a
// half-written report is never visible. durable additionally fsyncs the
// file and its directory — the terminal writes (finish, load-time
// recovery) pay it; the 256 per-directory progress updates ride the
// rename alone (their loss to a crash is one stale progress tick, the
// final write re-establishes the truth; fsync-per-directory measured
// ~170ms/dir on the dev host, a 40s+ run for a walk that is otherwise
// sub-second).
func (m *pruneManager) persist(st *pruneReportState, durable bool) {
	raw, err := json.Marshal(st)
	if err != nil {
		m.log.WarnContext(context.Background(), "httpapi: prune report marshal failed", "error", err.Error())
		return
	}
	tmp := m.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // G304: tmp is this manager's own data-dir path
	if err != nil {
		m.log.WarnContext(context.Background(), "httpapi: prune report write failed", "error", err.Error())
		return
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return
	}
	if durable {
		if err := f.Sync(); err != nil {
			_ = f.Close()
			return
		}
	}
	if err := f.Close(); err != nil {
		return
	}
	if err := os.Rename(tmp, m.path); err != nil {
		return
	}
	ok = true
	if durable {
		if d, derr := os.Open(filepath.Dir(m.path)); derr == nil {
			_ = d.Sync()
			_ = d.Close()
		}
	}
}

// snapshot returns a deep copy of the last report (nil when prune never
// ran): the running job mutates the live state under the same mutex, and
// the render path must read a stable copy — returning the pointer would
// race every mid-flight status read (spec §3.2-2's running-poll shape).
func (m *pruneManager) snapshot() *pruneReportState {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	if m.state == nil {
		return nil
	}
	cp := *m.state
	if m.state.LastDir != nil {
		dir := *m.state.LastDir
		cp.LastDir = &dir
	}
	return &cp
}

// requestStop sets the stop marker. It reports whether a historical
// report exists — the spec's discriminator between the accepted 202 and
// the 412 "No running Prune task found".
func (m *pruneManager) requestStop() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	if m.state == nil {
		return false
	}
	m.stopReq = true
	return true
}

// stopRequested is the engine's stop probe.
func (m *pruneManager) stopRequested() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopReq
}

// begin claims the single-flight slot and lands the initial running
// report. started=false means a task is already running (the 412 arm).
func (m *pruneManager) begin(dryRun bool, startFrom string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	if m.running {
		return false
	}
	now := m.now()
	m.running = true
	m.stopReq = false // a leftover idle-stop marker dies with the new run
	m.state = &pruneReportState{
		Status:      "running",
		DryRun:      dryRun,
		StartedAt:   now,
		LastUpdated: now,
	}
	if startFrom != "" {
		// The resume point indexes the run's first processed directory
		// into the 256-name enumeration (dirs before it are skipped but
		// counted — spec §3.2-5).
		m.state.Progress = shardIndexOf(startFrom)
	}
	m.persist(m.state, false)
	return true
}

// observe folds one directory's stats into the running report.
func (m *pruneManager) observe(stats storage.PruneDirStats) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running || m.state == nil || m.state.Status != "running" {
		return
	}
	now := m.now()
	// Monotonic clamp (review N2): a resumed run begins at its startFrom
	// position while the walk's skipped-directory observations arrive with
	// indices below it (00..startFrom-1) — the raw assignment would drag
	// the progress numerator backwards for the whole skip span.
	m.state.Progress = max(m.state.Progress, stats.Index)
	m.state.TotalBinariesProcessed += stats.BinariesProcessed
	m.state.TotalBinariesCleaned += stats.BinariesCleaned
	m.state.TotalBytesCleaned += stats.BytesCleaned
	m.state.LastDir = &pruneDirState{
		Name:              stats.Name,
		Status:            "finished",
		BinariesProcessed: stats.BinariesProcessed,
		BinariesCleaned:   stats.BinariesCleaned,
		BytesCleaned:      stats.BytesCleaned,
		StartedAt:         stats.StartedAt,
		FinishedAt:        stats.FinishedAt,
	}
	m.state.LastUpdated = now
	m.persist(m.state, false)
}

// finish lands the terminal report (stopped/finished/error) and frees the
// slot.
func (m *pruneManager) finish(status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
	m.stopReq = false
	if m.state == nil {
		return
	}
	m.state.Status = status
	m.state.LastUpdated = m.now()
	if status == "stopped" && m.state.LastDir != nil {
		// The live instance's stopped shape: the landing directory is
		// rendered stopped (the terminal record, not a cleared one).
		m.state.LastDir.Status = "stopped"
	}
	m.persist(m.state, true)
}

// shardIndexOf maps a two-hex shard name onto its 1-based position in the
// 00..ff enumeration; 0 for anything else.
func shardIndexOf(name string) int {
	if len(name) != 2 {
		return 0
	}
	v, err := strconv.ParseUint(name, 16, 32)
	if err != nil {
		return 0
	}
	return int(v) + 1
}

// ---- shared wire helpers ----

// writeInfo answers the prune family's {"info":"…"} envelope (E2/E4).
func writeInfo(w http.ResponseWriter, status int, info string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Info string `json:"info"`
	}{info})
}

// dotStream is the §3.1 ImportExportStreamStatusHolder form: one '.' per
// progress event (a newline every 80), info lines verbatim + newline,
// errors as "\n<statusCode> : <message>" — flushed as written, and the
// status code pinned at 200 from the first byte. Write errors are
// swallowed: a hung-up client stops the stream, never the task.
type dotStream struct {
	w     http.ResponseWriter
	flush http.Flusher
	dots  int
}

func newDotStream(w http.ResponseWriter) *dotStream {
	w.Header().Set("Content-Type", "text/plain;charset=utf-8")
	w.WriteHeader(http.StatusOK)
	ds := &dotStream{w: w}
	if f, ok := w.(http.Flusher); ok {
		ds.flush = f
	}
	return ds
}

func (d *dotStream) write(s string) {
	_, _ = io.WriteString(d.w, s)
	if d.flush != nil {
		d.flush.Flush()
	}
}

func (d *dotStream) dot() {
	d.dots++
	if d.dots%80 == 0 {
		d.write(".\n")
		return
	}
	d.write(".")
}

func (d *dotStream) line(s string) { d.write(s + "\n") }

func (d *dotStream) errLine(code int, msg string) {
	d.write(fmt.Sprintf("\n%d : %s\n", code, msg))
}

// storagePruner resolves the prune capability off the wired GC engine.
func (s *Server) storagePruner() storage.Pruner {
	if s.deps.GC == nil {
		return nil
	}
	pr, _ := s.deps.GC.(storage.Pruner)
	return pr
}

// ---- E2: POST /api/system/storage/prune/start ----

// pruneRequestBody is the optional E2 body. The polarity is the
// Artifactory one (D6): dryRun defaults to FALSE — the task deletes — and
// true is the estimate mode. The spec's two unverified parameters
// (resume position, binaryOlderThanDays — V-3) have no key names and are
// deliberately absent.
type pruneRequestBody struct {
	DryRun             bool   `json:"dryRun"`
	StartFromDirectory string `json:"startFromDirectory"`
}

func (s *Server) handleStoragePruneStart(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "prune requires an administrator account")
		return
	}
	pr := s.storagePruner()
	if pr == nil {
		writeError(w, http.StatusServiceUnavailable, "prune is not available on this instance (the storage engine carries no prune face)")
		return
	}
	var body pruneRequestBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request body is not valid prune request JSON: "+err.Error())
		return
	}
	if body.StartFromDirectory != "" && !validShardNameASCII(body.StartFromDirectory) {
		writeError(w, http.StatusBadRequest, "startFromDirectory must be a two-character hex shard name (00..ff)")
		return
	}

	// Single-flight claim + the initial running report. A submission
	// failure answers the spec's 500 form.
	if !s.prune.begin(body.DryRun, body.StartFromDirectory) {
		writeInfo(w, http.StatusPreconditionFailed, "Pruning Unreferenced Data task cannot be started")
		return
	}

	// The job runs detached: the 202 has already answered, and a task
	// must outlive the requesting connection (the gc face's posture).
	// WithoutCancel keeps the request's values and drops its cancellation.
	go s.runPruneJob(context.WithoutCancel(r.Context()), pr, body.DryRun, body.StartFromDirectory)

	info := "Pruning Unreferenced Data task has been submitted"
	if body.StartFromDirectory != "" {
		info = fmt.Sprintf("Prune Unreferenced Data task resumes from directory %s", body.StartFromDirectory)
	}
	writeInfo(w, http.StatusAccepted, info)
}

// validShardNameASCII accepts exactly two lowercase hex characters.
func validShardNameASCII(name string) bool {
	if len(name) != 2 {
		return false
	}
	for i := 0; i < 2; i++ {
		c := name[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// runPruneJob is the background PUD task: one prune pass whose
// per-directory observations land in the persisted report, the stop
// marker is consumed between directories, and apply mode tears the
// blobs-ledger rows of its deletions down (the RunGC teardown). The
// caller hands the request context with cancellation stripped — the task
// must outlive the connection that submitted it.
func (s *Server) runPruneJob(ctx context.Context, pr storage.Pruner, dryRun bool, startFrom string) {
	mgr := s.prune
	status := "finished"
	out, err := pr.Prune(ctx, storage.PruneOptions{
		Marker:    gcMarker{ctx: ctx, md: s.deps.Metadata},
		Grace:     s.deps.Config.Storage.GCGrace,
		Apply:     !dryRun,
		StartFrom: startFrom,
		Stop:      mgr.stopRequested,
		Observe:   mgr.observe,
	})
	switch {
	case err != nil && out != nil && out.Stopped:
		status = "stopped" // a stopped pass with shard-level faults: the stop dominates
	case err != nil:
		status = "error"
		s.log.ErrorContext(ctx, "httpapi: prune task failed", "error", err.Error())
	case out.Stopped:
		status = "stopped"
	}
	if out != nil && !dryRun {
		for _, sha := range out.Deleted {
			if derr := s.deps.Metadata.Blobs().Delete(ctx, sha); derr != nil && !errors.Is(derr, metadata.ErrNotFound) {
				s.log.WarnContext(ctx, "httpapi: dropping blobs ledger row failed",
					"sha256", sha, "error", derr.Error())
			}
		}
	}
	mgr.finish(status)
	processed, cleaned, bytesCleaned := mgr.snapshotTotals()
	detail := fmt.Sprintf(`{"dryRun":%t,"applied":%t,"processed":%d,"cleaned":%d,"bytesCleaned":%d}`,
		dryRun, !dryRun, processed, cleaned, bytesCleaned)
	s.audit.Record(ctx, audit.Event{
		Actor:  "admin-api",
		Action: audit.ActionMaintenancePruneRun,
		Detail: detail,
	})
	s.log.InfoContext(ctx, "httpapi: prune task complete",
		"status", status, "dry_run", dryRun, "progress", fmt.Sprintf("%d of %d", mgr.snapshotProgress(), storage.PruneShardCount))
}

// snapshotTotals/snapshotProgress are the audit tail's read of the final
// report (zero when the pass produced none).
func (m *pruneManager) snapshotTotals() (int64, int64, int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return 0, 0, 0
	}
	return m.state.TotalBinariesProcessed, m.state.TotalBinariesCleaned, m.state.TotalBytesCleaned
}

func (m *pruneManager) snapshotProgress() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return 0
	}
	return m.state.Progress
}

// ---- E4: POST /api/system/storage/prune/stop ----

func (s *Server) handleStoragePruneStop(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "prune requires an administrator account")
		return
	}
	if !s.prune.requestStop() {
		writeInfo(w, http.StatusPreconditionFailed, "No running Prune task found")
		return
	}
	writeInfo(w, http.StatusAccepted, "Prune task stop request submitted")
}

// ---- E3: GET /api/system/storage/prune/status ----

func (s *Server) handleStoragePruneStatus(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "prune status requires an administrator account")
		return
	}
	st := s.prune.snapshot()
	if st == nil {
		writeInfo(w, http.StatusPreconditionFailed, "No Prune task found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(renderPruneReport(st))
}

// ---- E1: POST /api/system/storage/gc (synchronous, dot stream) ----

func (s *Server) handleStorageGCStream(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "gc requires an administrator account")
		return
	}
	if s.deps.GC == nil {
		writeError(w, http.StatusServiceUnavailable, "gc is not available on this instance")
		return
	}
	pr := s.storagePruner()
	if pr == nil {
		writeError(w, http.StatusServiceUnavailable, "gc is not available on this instance (the storage engine carries no prune face)")
		return
	}
	// The same data-directory maintenance lock the REST/CLI gc faces hold:
	// a concurrent manual gc (or export) is refused, never queued.
	lock, err := storage.AcquireDataLock(s.deps.DataDir, storage.DataLockOpGC)
	if err != nil {
		if errors.Is(err, storage.ErrDataLockHeld) {
			writeError(w, http.StatusConflict, s.gcLockRefusedMessage(err))
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = lock.Release() }()

	ds := newDotStream(w)
	ctx := context.WithoutCancel(r.Context())
	out, perr := pr.Prune(ctx, storage.PruneOptions{
		Marker:  gcMarker{ctx: ctx, md: s.deps.Metadata},
		Grace:   s.deps.Config.Storage.GCGrace,
		Apply:   true,
		Observe: func(storage.PruneDirStats) { ds.dot() },
	})
	if perr != nil {
		// In-stream error form (§3.1-2): the code is already pinned at
		// 200; the error rides the body.
		ds.errLine(http.StatusInternalServerError, perr.Error())
	}
	if out != nil {
		// The blobs-ledger teardown every real deletion performs.
		for _, sha := range out.Deleted {
			if derr := s.deps.Metadata.Blobs().Delete(ctx, sha); derr != nil && !errors.Is(derr, metadata.ErrNotFound) {
				s.log.WarnContext(ctx, "httpapi: dropping blobs ledger row failed",
					"sha256", sha, "error", derr.Error())
			}
		}
		detail := fmt.Sprintf(`{"apply":true,"processed":%d,"cleaned":%d,"bytesCleaned":%d,"error":%t}`,
			out.Totals.BinariesProcessed, out.Totals.BinariesCleaned, out.Totals.BytesCleaned, perr != nil)
		s.audit.Record(ctx, audit.Event{
			Actor:  p.Name,
			Action: audit.ActionGCRun,
			Detail: detail,
		})
		s.log.InfoContext(ctx, "httpapi: storage gc stream complete",
			"processed", out.Totals.BinariesProcessed, "cleaned", out.Totals.BinariesCleaned,
			"bytes_cleaned", out.Totals.BytesCleaned, "error", fmt.Sprintf("%t", perr != nil))
	} else {
		// Mark-phase failure (review N6): the pass never reached a
		// directory, but the attempt still lands its gc.run row — an
		// operator reading the audit trail must see the failed run, not
		// silence.
		s.audit.Record(ctx, audit.Event{
			Actor:  p.Name,
			Action: audit.ActionGCRun,
			Detail: fmt.Sprintf(`{"apply":true,"processed":0,"cleaned":0,"bytesCleaned":0,"error":%t}`, perr != nil),
		})
		if perr != nil {
			s.log.ErrorContext(ctx, "httpapi: storage gc stream failed before walk", "error", perr.Error())
		}
	}
}

// ---- E6: POST /api/system/storage/optimize (202, empty body) ----

// handleStorageOptimize answers the sharding balancer's manual trigger.
// BinFlow runs a single-provider filestore — there is nothing to balance,
// so the trigger is accepted and executed as the no-op it honestly is
// (202 with an empty body and no Content-Type, the live form).
func (s *Server) handleStorageOptimize(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "optimize requires an administrator account")
		return
	}
	s.log.InfoContext(r.Context(), "httpapi: storage optimize accepted (single-provider filestore: no-op)")
	w.WriteHeader(http.StatusAccepted)
}

// ---- E5: POST /api/system/storage/compress (stream, code pinned 200) ----

// handleStorageCompress streams one metadata rebuild. The kernel is the
// maintenance/compress carrier (RunMetadataCompress — the store's VACUUM
// face); a store without that face answers in-stream (the code stays 200,
// the §3.1-2 pinning rule — the PostgreSQL-compress refusal shape).
func (s *Server) handleStorageCompress(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "compress requires an administrator account")
		return
	}
	ds := newDotStream(w)
	ds.line("compressing the internal database")
	res, err := s.RunMetadataCompress(r.Context(), CompressRequest{Actor: p.Name, RemoteAddr: r.RemoteAddr})
	if err != nil {
		ds.errLine(http.StatusInternalServerError, err.Error())
		return
	}
	ds.line(fmt.Sprintf("internal database: %d -> %d bytes (reclaimed %d)",
		res.BeforeBytes, res.AfterBytes, res.ReclaimedBytes))
}

// ---- E7: POST /api/system/storage/backup?key=<key> ----

// handleStorageBackup immediately schedules one configured backup. The
// error arms follow the live evidence: an unknown key answers the errors
// envelope at 500 ("No backup identified with key 'X'"); a disabled one
// the 500 the decompile records. The success arm schedules through the
// BackupRunner seam (the scheduler's runner — the same kernel a cron fire
// rides) and answers 200 with the stream holder's content type; the
// export itself runs detached (the spec's "schedule, do not wait" form).
func (s *Server) handleStorageBackup(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "backup requires an administrator account")
		return
	}
	key := r.URL.Query().Get("key")
	bk, err := s.deps.Metadata.Backups().Get(r.Context(), key)
	if err != nil {
		switch {
		case errors.Is(err, metadata.ErrBackupNotFound):
			writeError(w, http.StatusInternalServerError,
				fmt.Sprintf("No backup identified with key '%s'", key))
		case metadata.IsStoreBusy(err):
			writeError(w, http.StatusServiceUnavailable, "backups store is busy, retry shortly: "+err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "backups: reading: "+err.Error())
		}
		return
	}
	if !bk.Enabled {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("backup '%s' is disabled", key))
		return
	}
	if s.deps.BackupRunner == nil {
		writeError(w, http.StatusServiceUnavailable,
			"backup scheduling is not available on this instance (no backup runner wired)")
		return
	}
	runner := s.deps.BackupRunner
	actor := p.Name
	storedKey := bk.Key // the store's spelling, not the query echo (taint)
	go func() {
		ctx := context.WithoutCancel(r.Context())
		if rerr := runner.Run(ctx, storedKey); rerr != nil {
			s.log.ErrorContext(ctx, "httpapi: scheduled backup failed", "key", storedKey, "error", rerr.Error())
			return
		}
		s.log.InfoContext(ctx, "httpapi: backup complete", "key", storedKey, "actor", actor)
	}()
	w.Header().Set("Content-Type", "text/plain;charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, fmt.Sprintf("backup '%s' scheduled\n", storedKey))
}

// ---- E8: GET /api/system/storage/size ----

// handleStorageSize answers the filestore's blob bytes (the live body is
// the bare number, text/plain). The walk is O(blob count), memory-flat.
func (s *Server) handleStorageSize(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "storage size requires an administrator account")
		return
	}
	blobsDir := filepath.Join(s.deps.DataDir, "blobs")
	var total int64
	_ = filepath.WalkDir(blobsDir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil // an absent dir sizes zero; the walk is advisory
		}
		if info, ierr := d.Info(); ierr == nil {
			total += info.Size()
		}
		return nil
	})
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, strconv.FormatInt(total, 10))
}

// ---- E9: GET /api/system/storage/info ----

// storageInfoView is the binary-provider projection the live instance
// serves (text/plain, chunked; every numeric field stringified). BinFlow's
// mapping: the single file-system provider over the data directory —
// binariesDir is the blobs/ shard root, tempDir the engine's uploads
// staging, usage/free/total the hosting volume's statfs numbers (the live
// instance's usageSpace+freeSpace==totalSpace identity).
type storageInfoView struct {
	Data struct {
		BaseDataDir         string `json:"baseDataDir"`
		BinariesDir         string `json:"binariesDir"`
		UsageSpace          string `json:"usageSpace"`
		FreeSpace           string `json:"freeSpace"`
		TempDir             string `json:"tempDir"`
		UsageSpaceInPercent string `json:"usageSpaceInPercent"`
		ID                  string `json:"id"`
		TotalSpace          string `json:"totalSpace"`
		FreeSpaceInPercent  string `json:"freeSpaceInPercent"`
		FileStoreDir        string `json:"fileStoreDir"`
		Type                string `json:"type"`
	} `json:"data"`
	SubBinaryTreeElements []struct{} `json:"subBinaryTreeElements"`
}

func (s *Server) handleStorageInfo(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "storage info requires an administrator account")
		return
	}
	var v storageInfoView
	v.Data.BaseDataDir = s.deps.DataDir
	v.Data.BinariesDir = filepath.Join(s.deps.DataDir, "blobs")
	v.Data.TempDir = "uploads"
	v.Data.ID = "file-system"
	v.Data.FileStoreDir = "blobs"
	v.Data.Type = "file-system"
	total, free := filestoreSpace(filepath.Join(s.deps.DataDir, "blobs"))
	used := total - free
	if used < 0 {
		used = 0
	}
	v.Data.UsageSpace = strconv.FormatInt(used, 10)
	v.Data.FreeSpace = strconv.FormatInt(free, 10)
	v.Data.TotalSpace = strconv.FormatInt(total, 10)
	if total > 0 {
		v.Data.UsageSpaceInPercent = strconv.FormatInt(used*100/total, 10)
		v.Data.FreeSpaceInPercent = strconv.FormatInt(free*100/total, 10)
	} else {
		v.Data.UsageSpaceInPercent = "0"
		v.Data.FreeSpaceInPercent = "0"
	}
	v.SubBinaryTreeElements = []struct{}{}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// ---- E10: POST /api/system/storage/exportds (deprecated, constant failure) ----

// handleStorageExportds is the @Deprecated export face: it always fails
// ("Export data is no longer supported"), the uncaught-exception 500 the
// decompile records. BinFlow's export lives on the CLI (ADR-0015).
func (s *Server) handleStorageExportds(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusInternalServerError, "Export data is no longer supported")
}
