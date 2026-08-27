package repo

// The unused-cleanup engine (T-324, PRD FR-102.2 / LC-29): the cron-driven
// remote-cache reclamation policy over the storage GC kernel. The M10 field
// unusedArtifactsCleanupPeriodHours (remote_configs.unused_cleanup_period_hours,
// persisted since T-290) becomes effective here — 0 stays off (the product
// default, repo-semantics section 7.1), and a positive period drives a pass
// that removes cached artifacts a remote repository has not served and has
// not (re)fetched within the window.
//
// One run is three legs, in a fixed order, under ONE data-directory
// maintenance lock (the gc/export/import family, ADR-0015 erratum ③):
//
//	session leg   expired upload sessions (rows + temp state) through the
//	              engine's SessionSweeper capability — the zero-orphan
//	              guarantee's session half.
//	policy leg    per remote repo with a positive period: the FILE nodes
//	              neither downloaded (audit trail) nor (re)landed
//	              (updated_at) since the cutoff lose their node row and
//	              their remote_cache validator row.
//	gc leg        one GCSweep apply over the whole store with the ADR-0031
//	              gates (hold set + single-point Live) reclaims the blobs
//	              the policy leg orphaned — plus any other orphans — and
//	              the blobs-ledger rows of the deleted checksums go with
//	              them (the same teardown POST /api/v1/system/gc performs).
//
// The "unused" oracle is the audit trail: every served artifact leaves a
// download event (repo service records one per GET), and the keep set of a
// remote repo is every path downloaded since the cutoff from itself OR from
// any virtual repository it is a member of (virtual GETs audit under the
// VIRTUAL key — a member artifact served through the aggregate must not be
// reaped). With audit disabled there IS no oracle — the downloads leave no
// trace and the engine cannot tell "unused" from "heavily used but never
// re-fetched" — so the policy leg refuses to run and says so in the report;
// the session and gc legs still run (neither depends on the oracle).
//
// Zero-orphan guarantee (the ticket's acceptance core): a completed run
// leaves no residue behind its deletions —
//
//	index    node rows and remote_cache validator rows of every cleaned
//	         path are dropped together (node_props cascades through its FK,
//	         the 013 "same statement" contract); docker index rows cannot
//	         orphan because remote docker repositories do not exist (the
//	         M3 class matrix, FR-15-AC7).
//	blob     every blob the policy leg orphaned is deleted from blobs/ and
//	         the blobs ledger by the gc leg unless it is still referenced
//	         (dedup across repos keeps it — correct, not residue) or inside
//	         the grace window (the mtime safety margin; the next run's gc
//	         leg reclaims it, reported as gracePending).
//	session  expired upload-session rows and their temp files are gone
//	         (the session leg).
//
// Ordering inside one policy deletion is node row first, validator row
// second: a crash between the two leaves a validator row without a node,
// which is INERT (the fetch path consults a validator only after the node
// hit) and self-heals on the next fetch's PutCache upsert. The reverse
// order could leave a live node whose validator vanished — also safe, but
// the chosen order keeps the dangerous half (a node) for the shorter
// window.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// ErrCleanupRunning is returned when a second RunOnce is attempted while
// one is in flight: cleanup runs are never concurrent with each other (the
// data-directory lock would serialize cross-process contenders anyway; this
// closes the same-process REST-vs-cron race the lock cannot see).
var ErrCleanupRunning = errors.New("cleanup: a run is already in progress")

// Cleanup triggers recorded in the run report and the audit detail.
const (
	CleanupTriggerCron   = "cron"
	CleanupTriggerManual = "manual"
)

// ActorCleanup is the audit actor scheduled runs record (the CLI governance
// precedent records "admin"; a cron run has no human behind it, and
// "anonymous" would misreport a scheduled maintenance action as unauthenticated
// access).
const ActorCleanup = "system-cleanup"

// defaultCleanupTick is the cron cadence: one pass per hour. The per-repo
// period stays the policy threshold; the tick is only how often the engine
// looks. Every pass is idempotent and cheap when nothing aged out (the audit
// walk is an indexed keyset scan bounded by the window's event count).
const defaultCleanupTick = time.Hour

// cleanupAuditPageSize is the download-oracle page size: large enough that
// one page usually covers a window, small enough to keep keyset pages short.
const cleanupAuditPageSize = 500

// CleanupSweeper is the storage face the engine drives: the same GCSweep
// kernel POST /api/v1/system/gc runs (the engine's in-flight hold set is
// directly visible because this engine lives in the serve process).
type CleanupSweeper interface {
	GCSweep(ctx context.Context, m storage.GCMarker, grace time.Duration, apply bool) ([]string, error)
}

// CleanupOptions carries the engine's collaborators. Store, Engine, Audit and
// DataDir are required; the rest have product defaults.
type CleanupOptions struct {
	// Store is the metadata store (repos, nodes, remote configs, virtual
	// members, the blobs ledger).
	Store metadata.Store
	// Engine is the storage engine's sweep face.
	Engine CleanupSweeper
	// Audit is the shared audit logger: its Query facet is the download
	// oracle, its append facet records cleanup.run.
	Audit audit.Logger
	// AuditEnabled mirrors config.Audit.Enabled — the oracle's honesty gate
	// (see the package comment's audit-disabled posture).
	AuditEnabled bool
	// DataDir is storage.data_dir — the maintenance lock's home.
	DataDir string
	// Grace is the blob grace window of the gc leg (storage.gc_grace_hours
	// at assembly). Zero falls back to storage's own default.
	Grace time.Duration
	// TickEvery is the cron cadence (default 1h; tests shrink it).
	TickEvery time.Duration
	// Now is the clock (tests inject; production UTC wall time).
	Now func() time.Time
	// Log is the logger (slog.Default() when nil).
	Log *slog.Logger
}

// cleanupRepoReport is one repository's slice of the run report.
type cleanupRepoReport struct {
	Repo        string `json:"repo"`
	PeriodHours int64  `json:"periodHours"`
	Cutoff      string `json:"cutoff,omitempty"` // RFC3339; empty when skipped
	// KeptByUse counts nodes the download oracle protected.
	KeptByUse int `json:"keptByUse"`
	// Candidates is the node count the policy marked (dry-run and apply
	// alike); Deleted is what actually went (apply only).
	Candidates int    `json:"candidates"`
	Deleted    int    `json:"deleted"`
	Bytes      int64  `json:"bytes"`
	Skipped    string `json:"skipped,omitempty"` // refusal reason (audit off, ...)
	Error      string `json:"error,omitempty"`
}

// CleanupReport is one completed (or refused) run. It is the POST response
// body, the GET status snapshot and the audit detail payload in one shape.
type CleanupReport struct {
	Trigger    string              `json:"trigger"`
	Apply      bool                `json:"apply"`
	StartedAt  string              `json:"startedAt"`
	FinishedAt string              `json:"finishedAt"`
	Repos      []cleanupRepoReport `json:"repos"`
	// GracePending is the gc leg's candidate count minus its delete count:
	// blobs the ADR-0031 gates or the grace window kept this run (the next
	// run's gc leg reclaims them; conservative, never lost).
	GracePending int `json:"gracePending"`
	GCDeleted    int `json:"gcDeleted"`
	// SessionsSwept is expired upload-session rows reclaimed.
	SessionsSwept int `json:"sessionsSwept"`
	// Cumulative counters since engine start (the /metrics gauges' source).
	ObjectsCleaned int64  `json:"objectsCleaned"`
	BytesReclaimed int64  `json:"bytesReclaimed"`
	OK             bool   `json:"ok"`
	Error          string `json:"error,omitempty"`
}

// CleanupRunOptions scopes one manual run. Trigger defaults to manual, apply
// defaults to false (the dry-run-is-the-default posture the gc face set,
// ADR-0015 erratum ①), Repo empty means every policy-carrying remote repo.
type CleanupRunOptions struct {
	Trigger string
	Apply   bool
	Repo    string
	Actor   string
}

// CleanupStats is the engine's cumulative snapshot (the /metrics gauges read
// this at scrape time, the replTasks precedent).
type CleanupStats struct {
	Runs           int64  `json:"runs"`
	ObjectsCleaned int64  `json:"objectsCleaned"`
	BytesReclaimed int64  `json:"bytesReclaimed"`
	LastFinishedAt string `json:"lastFinishedAt"`
	LastOK         bool   `json:"lastOk"`
}

// CleanupEngine is the unused-cleanup engine. Safe for concurrent use;
// RunOnce serializes runs, Run is the cron loop.
type CleanupEngine struct {
	opts    CleanupOptions
	log     *slog.Logger
	now     func() time.Time
	record  audit.Recorder
	running atomic.Bool
	runs    atomic.Int64
	objects atomic.Int64
	bytes   atomic.Int64
	lastMu  sync.RWMutex
	lastOK  bool
	lastAt  string
	last    atomic.Pointer[CleanupReport]
}

// NewCleanupEngine validates the options and builds the engine.
func NewCleanupEngine(opts CleanupOptions) (*CleanupEngine, error) {
	if opts.Store == nil {
		return nil, errors.New("repo: cleanup: metadata store is required")
	}
	if opts.Engine == nil {
		return nil, errors.New("repo: cleanup: storage sweep face is required")
	}
	if opts.Audit == nil {
		return nil, errors.New("repo: cleanup: audit logger is required")
	}
	if opts.DataDir == "" {
		return nil, errors.New("repo: cleanup: data directory is required")
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	tick := opts.TickEvery
	if tick <= 0 {
		tick = defaultCleanupTick
	}
	opts.Log = log
	opts.Now = now
	opts.TickEvery = tick
	return &CleanupEngine{opts: opts, log: log, now: now, record: audit.BestEffort(opts.Audit)}, nil
}

// TickEvery reports the cron cadence (the status face shows it).
func (e *CleanupEngine) TickEvery() time.Duration { return e.opts.TickEvery }

// AuditEnabled reports whether the download oracle is armed.
func (e *CleanupEngine) AuditEnabled() bool { return e.opts.AuditEnabled }

// Stats returns the cumulative snapshot.
func (e *CleanupEngine) Stats() CleanupStats {
	e.lastMu.RLock()
	defer e.lastMu.RUnlock()
	return CleanupStats{
		Runs:           e.runs.Load(),
		ObjectsCleaned: e.objects.Load(),
		BytesReclaimed: e.bytes.Load(),
		LastFinishedAt: e.lastAt,
		LastOK:         e.lastOK,
	}
}

// LastReport returns the last completed run's report (nil before the first).
func (e *CleanupEngine) LastReport() *CleanupReport { return e.last.Load() }

// Run is the cron loop: one RunOnce (apply) per tick until ctx ends. A run
// error is logged, never fatal — the next tick retries.
func (e *CleanupEngine) Run(ctx context.Context) {
	t := time.NewTicker(e.opts.TickEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			opts := CleanupRunOptions{Trigger: CleanupTriggerCron, Apply: true, Actor: ActorCleanup}
			if _, err := e.RunOnce(ctx, opts); err != nil {
				e.log.WarnContext(ctx, "repo: cleanup: scheduled run failed", "error", err.Error())
			}
		}
	}
}

// RunOnce executes one three-leg run under the maintenance lock. The
// returned report is always non-nil on success (a refused or failed run
// carries its reason inside); ErrCleanupRunning and the lock refusal
// (wrapping storage.ErrDataLockHeld) are the two error outcomes.
func (e *CleanupEngine) RunOnce(ctx context.Context, opts CleanupRunOptions) (*CleanupReport, error) {
	if opts.Trigger == "" {
		opts.Trigger = CleanupTriggerManual
	}
	if opts.Actor == "" {
		opts.Actor = ActorCleanup
	}
	if !e.running.CompareAndSwap(false, true) {
		return nil, ErrCleanupRunning
	}
	defer e.running.Store(false)

	lock, err := storage.AcquireDataLock(e.opts.DataDir, storage.DataLockOpCleanup)
	if err != nil {
		if errors.Is(err, storage.ErrDataLockHeld) {
			return nil, fmt.Errorf("repo: cleanup: %w", err)
		}
		return nil, fmt.Errorf("repo: cleanup: acquiring the data directory lock: %w", err)
	}
	defer func() { _ = lock.Release() }()

	// The run detaches from the caller's cancellation exactly like the gc
	// face (T-94 review N1): a REST client hang-up or a cron ctx winding
	// down mid-apply must not strand a half-executed policy pass — the
	// maintenance lock is held, the deletions are partial-safe but the
	// report and audit row must still land.
	runCtx := context.WithoutCancel(ctx)
	started := e.now()
	rep := &CleanupReport{
		Trigger:   opts.Trigger,
		Apply:     opts.Apply,
		StartedAt: started.Format(time.RFC3339),
		OK:        true,
	}
	// firstErr keeps the typed handle behind rep.Error's sentence so the
	// returned error stays errors.Is-matchable (the REST face branches on
	// ErrRepoNotFound et al.).
	var firstErr error

	// Session leg: expired upload sessions (rows + temp state).
	if sweeper, ok := e.opts.Engine.(storage.SessionSweeper); ok {
		n, serr := sweeper.SweepExpiredSessions(runCtx)
		if serr != nil {
			rep.OK = false
			rep.Error = fmt.Sprintf("session sweep: %v", serr)
			firstErr = serr
			e.log.WarnContext(runCtx, "repo: cleanup: session sweep failed", "error", serr.Error())
		} else {
			rep.SessionsSwept = n
		}
	}

	// Policy leg. The repository listing happens here (once) so the keep
	// sets walk virtual membership without re-listing per repo.
	rows, rerr := e.opts.Store.Repos().List(runCtx)
	if rerr != nil {
		rep.OK = false
		rep.Error = fmt.Sprintf("policy: listing repositories: %v", rerr)
		firstErr = rerr
		e.log.WarnContext(runCtx, "repo: cleanup: policy leg failed", "error", rerr.Error())
	} else {
		if perr := e.runPolicy(runCtx, opts, started, rep, rows); perr != nil {
			rep.OK = false
			if firstErr == nil {
				firstErr = perr
			}
			if rep.Error == "" {
				rep.Error = perr.Error()
			}
		}
	}

	// GC leg — always driven (apply) in a cron run and in a manual apply
	// run: reclaiming what the policy leg orphaned IS the zero-orphan
	// guarantee's blob half. A manual dry run keeps it dry (candidates
	// only) so the report shows what an apply would free. Apply runs take
	// the same two-pass shape the REST gc face takes: the dry pass sizes
	// the candidate set (so gracePending is honest), the apply pass frees.
	marker := &CleanupMarker{Ctx: runCtx, MD: e.opts.Store}
	candidates, gerr := e.opts.Engine.GCSweep(runCtx, marker, e.opts.Grace, false)
	if gerr != nil {
		rep.OK = false
		if rep.Error == "" {
			rep.Error = fmt.Sprintf("gc sweep: %v", gerr)
		}
		e.log.WarnContext(runCtx, "repo: cleanup: gc dry pass failed", "error", gerr.Error())
	}
	var deleted []string
	if opts.Apply {
		deleted, gerr = e.opts.Engine.GCSweep(runCtx, marker, e.opts.Grace, true)
		if gerr != nil {
			rep.OK = false
			if firstErr == nil {
				firstErr = gerr
			}
			if rep.Error == "" {
				rep.Error = fmt.Sprintf("gc sweep: %v", gerr)
			}
			e.log.WarnContext(runCtx, "repo: cleanup: gc apply pass failed", "error", gerr.Error())
		}
		rep.GCDeleted = len(deleted)
		rep.GracePending = len(candidates) - len(deleted)
		if rep.GracePending < 0 {
			rep.GracePending = 0 // a racing writer's blob landed between the passes
		}
		// The blobs-ledger teardown the REST/CLI gc faces perform: a row
		// whose physical file is gone must not survive as a phantom.
		for _, sha := range deleted {
			if err := e.opts.Store.Blobs().Delete(runCtx, sha); err != nil && !errors.Is(err, metadata.ErrNotFound) {
				e.log.WarnContext(runCtx, "repo: cleanup: dropping blobs ledger row failed",
					"sha256", sha, "error", err.Error())
			}
		}
	}

	rep.FinishedAt = e.now().Format(time.RFC3339)

	// Cumulative counters + last-run snapshot (dry runs change nothing, so
	// they must not move the counters either).
	var policyObjects int64
	var policyBytes int64
	for _, r := range rep.Repos {
		policyObjects += int64(r.Deleted)
		policyBytes += r.Bytes
	}
	if opts.Apply && rep.OK {
		e.runs.Add(1)
		e.objects.Add(policyObjects)
		e.bytes.Add(policyBytes)
	}
	e.lastMu.Lock()
	e.lastAt = rep.FinishedAt
	e.lastOK = rep.OK
	e.lastMu.Unlock()
	e.last.Store(rep)

	// Audit row: best-effort by the audit contract, but never skipped for a
	// shape problem — a report that fails to marshal records "{}".
	detail, derr := json.Marshal(rep)
	if derr != nil {
		e.log.WarnContext(runCtx, "repo: cleanup: audit detail marshal failed", "error", derr.Error())
		detail = []byte("{}")
	}
	e.record.Record(runCtx, audit.Event{
		Actor:  opts.Actor,
		Action: audit.ActionCleanupRun,
		Detail: string(detail),
	})

	mode := "dry-run"
	if opts.Apply {
		mode = "apply"
	}
	e.log.InfoContext(runCtx, "repo: cleanup: run complete",
		"mode", mode,
		"trigger", rep.Trigger,
		"repos", len(rep.Repos),
		"deleted", policyObjects,
		"bytes", policyBytes,
		"sessions_swept", rep.SessionsSwept,
		"gc_deleted", rep.GCDeleted,
		"ok", rep.OK,
	)
	if !rep.OK {
		if firstErr != nil {
			return rep, fmt.Errorf("repo: cleanup: %w", firstErr)
		}
		return rep, errors.New("repo: cleanup: " + rep.Error)
	}
	return rep, nil
}

// runPolicy is the policy leg: one report row per policy-carrying remote
// repository (or the one named repo), each with its own keep-set walk and
// deletion pass. allRows is the repository listing the caller already
// fetched (the keep set walks virtual members off it).
func (e *CleanupEngine) runPolicy(ctx context.Context, opts CleanupRunOptions, now time.Time, rep *CleanupReport, allRows []*metadata.Repo) error {
	if opts.Repo != "" {
		var row *metadata.Repo
		for _, r := range allRows {
			if r.RepoKey == opts.Repo {
				row = r
				break
			}
		}
		if row == nil {
			return fmt.Errorf("policy: repository %s: %w", opts.Repo, ErrRepoNotFound)
		}
		r, err := e.cleanupRepo(ctx, row, allRows, now, opts.Apply)
		if err != nil {
			return err
		}
		rep.Repos = append(rep.Repos, *r)
		return nil
	}

	var firstErr error
	for _, row := range allRows {
		if row.Type != TypeRemote {
			continue
		}
		r, err := e.cleanupRepo(ctx, row, allRows, now, opts.Apply)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		rep.Repos = append(rep.Repos, *r)
	}
	sort.Slice(rep.Repos, func(i, j int) bool { return rep.Repos[i].Repo < rep.Repos[j].Repo })
	return firstErr
}

// cleanupRepo runs the policy pass for one remote repository row.
func (e *CleanupEngine) cleanupRepo(ctx context.Context, row *metadata.Repo, allRows []*metadata.Repo, now time.Time, apply bool) (*cleanupRepoReport, error) {
	r := &cleanupRepoReport{Repo: row.RepoKey}
	if row.Type != TypeRemote {
		r.Skipped = "not a remote repository"
		return r, nil
	}
	cfg, err := e.opts.Store.Remote().GetConfig(ctx, row.RepoKey)
	if err != nil {
		// A remote repo without a config row is pre-003 residue the fetcher
		// would also refuse; skipping (not failing) keeps the run going.
		if errors.Is(err, metadata.ErrRemoteConfigNotFound) {
			r.Skipped = "no remote config row"
			return r, nil
		}
		return nil, fmt.Errorf("policy: remote config %s: %w", row.RepoKey, err)
	}
	r.PeriodHours = cfg.UnusedCleanupPeriodHours
	if cfg.UnusedCleanupPeriodHours <= 0 {
		r.Skipped = "unusedArtifactsCleanupPeriodHours is 0 (off)"
		return r, nil
	}
	if !e.opts.AuditEnabled {
		// The oracle is unarmed (see the package comment): refuse the
		// deletion, say why, keep the rest of the run intact.
		r.Skipped = "audit is disabled — the download oracle has no trail to read"
		return r, nil
	}

	cutoff := now.Add(-time.Duration(cfg.UnusedCleanupPeriodHours) * time.Hour)
	r.Cutoff = cutoff.Format(time.RFC3339)

	keep, err := e.keepSet(ctx, row.RepoKey, allRows, cutoff)
	if err != nil {
		return nil, fmt.Errorf("policy: keep set %s: %w", row.RepoKey, err)
	}

	nodes, err := e.opts.Store.Nodes().ListByPrefix(ctx, row.RepoKey, "")
	if err != nil {
		return nil, fmt.Errorf("policy: listing nodes of %s: %w", row.RepoKey, err)
	}
	for _, n := range nodes {
		if n.Sha256 == "" || n.Sha256 == metadata.FolderMarkerSHA {
			continue // folder markers and residue carry no physical blob
		}
		if _, used := keep[n.Path]; used {
			r.KeptByUse++
			continue
		}
		updated, perr := time.Parse(time.RFC3339, n.UpdatedAt)
		if perr != nil {
			// An unparseable clock is a conservative keep, never a delete.
			r.KeptByUse++
			continue
		}
		if updated.After(cutoff) {
			continue // (re)landed inside the window: the fetch path itself is use
		}
		r.Candidates++
		r.Bytes += n.Size
		if !apply {
			continue
		}
		// Node row first, validator row second (the package comment's
		// crash-window argument).
		if err := e.opts.Store.Nodes().Delete(ctx, row.RepoKey, n.Path); err != nil {
			if errors.Is(err, metadata.ErrNodeNotFound) {
				continue // raced a concurrent delete: already gone, not an error
			}
			r.Error = fmt.Sprintf("delete node %s: %v", n.Path, err)
			continue
		}
		if err := e.opts.Store.Remote().DeleteCache(ctx, row.RepoKey, n.Path); err != nil &&
			!errors.Is(err, metadata.ErrRemoteCacheNotFound) {
			// Inert residue at worst (see the package comment); the deletion
			// itself stands.
			e.log.WarnContext(ctx, "repo: cleanup: dropping remote cache row failed",
				"repo", row.RepoKey, "path", n.Path, "error", err.Error())
		}
		r.Deleted++
	}
	return r, nil
}

// keepSet builds the download oracle's keep set for one remote repo: every
// path with a download event since cutoff, from the repo itself plus every
// virtual repo it is a member of (virtual GETs audit under the virtual key).
func (e *CleanupEngine) keepSet(ctx context.Context, repoKey string, allRows []*metadata.Repo, cutoff time.Time) (map[string]struct{}, error) {
	var virtuals []string
	for _, row := range allRows {
		if row.Type != TypeVirtual {
			continue
		}
		members, err := e.opts.Store.Virtual().ListMembers(ctx, row.RepoKey)
		if err != nil {
			return nil, fmt.Errorf("listing members of %s: %w", row.RepoKey, err)
		}
		for _, m := range members {
			if m.MemberRepo == repoKey {
				virtuals = append(virtuals, row.RepoKey)
				break
			}
		}
	}
	keep := map[string]struct{}{}
	since := cutoff.UTC().Format(time.RFC3339)
	sources := append([]string{repoKey}, virtuals...)
	for _, src := range sources {
		if err := e.walkDownloads(ctx, src, since, keep); err != nil {
			return nil, fmt.Errorf("download walk %s: %w", src, err)
		}
	}
	return keep, nil
}

// walkDownloads collects the paths of every download event of one source
// repo since the cutoff into keep, walking the audit keyset pagination to
// exhaustion. Events repeat across pages only on cursor drift; a path set
// makes re-observation harmless.
func (e *CleanupEngine) walkDownloads(ctx context.Context, repoKey, since string, keep map[string]struct{}) error {
	cursor := ""
	for {
		page, err := e.opts.Audit.Query(ctx, audit.Filter{
			Repo:   repoKey,
			Action: audit.ActionDownload,
			Since:  since,
			Limit:  cleanupAuditPageSize,
			Cursor: cursor,
		})
		if err != nil {
			return err
		}
		for _, ev := range page.Events {
			if ev.Path != "" {
				keep[ev.Path] = struct{}{}
			}
		}
		if page.NextCursor == "" {
			return nil
		}
		cursor = page.NextCursor
	}
}

// CleanupMarker is the storage.GCMarker of the cleanup engine's gc leg: the
// same liveChecksumSet walk + single-point IsReferenced probe the REST gc
// face runs (ADR-0031 decision 3 / architecture section 14.2 point 3).
type CleanupMarker struct {
	Ctx context.Context
	MD  metadata.Store
}

// Mark implements storage.GCMarker (the liveChecksumSet snapshot).
func (m *CleanupMarker) Mark() (map[string]struct{}, error) {
	set, err := LiveChecksumSet(m.Ctx, m.MD)
	if err != nil {
		return nil, fmt.Errorf("cleanup: referenced set: %w", err)
	}
	return set, nil
}

// Live implements storage.GCMarker (the single-point recheck).
func (m *CleanupMarker) Live(sha256 string) (bool, error) {
	live, err := m.MD.IsReferenced(m.Ctx, sha256)
	if err != nil {
		return false, fmt.Errorf("cleanup: reference recheck %s: %w", sha256, err)
	}
	return live, nil
}

// LiveChecksumSet computes the GC mark set: every sha256 referenced by any
// node row UNION every blob_digest referenced by any docker_refs row
// (architecture sections 4.4 and 11.12). This is the ONE in-tree copy of the
// walk (T-324): httpapi's gc face delegates here; cmd's CLI gc keeps its own
// (main.go is conductor-owned — the dedup there rides the T-324 wiring).
// Folder marker rows are skipped (T-124): the shared sentinel has no
// physical file behind it, so the mark set stays exactly "physical blob
// shas".
func LiveChecksumSet(ctx context.Context, md metadata.Store) (map[string]struct{}, error) {
	set := map[string]struct{}{}

	repos, err := md.Repos().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing repositories: %w", err)
	}
	for _, r := range repos {
		nodes, err := md.Nodes().ListByPrefix(ctx, r.RepoKey, "")
		if err != nil {
			return nil, fmt.Errorf("listing nodes of %s: %w", r.RepoKey, err)
		}
		for _, n := range nodes {
			if n.Sha256 != "" && n.Sha256 != metadata.FolderMarkerSHA {
				set[n.Sha256] = struct{}{}
			}
		}

		images, err := md.Docker().ListImages(ctx, r.RepoKey, "", 0)
		if err != nil {
			return nil, fmt.Errorf("listing docker images of %s: %w", r.RepoKey, err)
		}
		for _, image := range images {
			manifests, err := md.Docker().ListManifestsByImage(ctx, r.RepoKey, image)
			if err != nil {
				return nil, fmt.Errorf("listing manifests of %s/%s: %w", r.RepoKey, image, err)
			}
			for _, m := range manifests {
				refs, err := md.Docker().ListRefsByManifest(ctx, r.RepoKey, image, m.Digest)
				if err != nil {
					return nil, fmt.Errorf("listing refs of %s/%s@%s: %w", r.RepoKey, image, m.Digest, err)
				}
				for _, ref := range refs {
					if ref.BlobDigest != "" {
						set[ref.BlobDigest] = struct{}{}
					}
				}
			}
		}
	}
	return set, nil
}
