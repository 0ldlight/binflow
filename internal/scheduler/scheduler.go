// Package scheduler is the cron scheduling domain (M16 T-446, FR-150.1/.2
// / ADR-0044): the Quartz-subset expression parser and next-run calculator
// (cron.go, next.go — pure functions), the schedules-ledger-driven Run
// loop, the per-domain Runner registration face and the misfire guards.
//
// COEXISTENCE WITH THE EVENT-DRIVEN ENGINES (ADR-0044 decision 8 — the
// assertion-reversal-⑦ boundary, declared here because this package is its
// structural guarantee):
//
//   - The scheduler fires ONLY full-type jobs: the closed set of Runners
//     the assembly registers (GC pass, cleanup passes, backup export,
//     replication full sync). It has no incremental face and never will —
//     incremental push remains the event-driven engines' exclusive domain
//     (repo.Service's Enqueue tail, the webhook Bus), and this package
//     imports neither the repo content-write path nor the Bus.
//   - The replication/webhook ENGINE FILES are untouched (the diff=0 hard
//     AC): a full sync rides the target domain's own exported carrier
//     (TriggerFullSync), never a second executor, never a new queue.
//   - Zero duplicate delivery holds at the CONTENT layer (the L48
//     assertion wording): task rows may coexist in one window (a scheduled
//     full-sync row plus event-driven incremental rows), convergence is
//     the carriers' idempotence (target-side sha256 hits transfer no
//     bytes). The scheduler neither suppresses nor serializes against the
//     event track — two tracks, one convergent target state.
//
// MISFIRE GUARDS (ADR-0044 decision 9): parse-time closed-set rejection
// and never-reachable refusal live in cron.go/next.go; run-time guards —
// per-domain concurrency 1 (the single dispatch loop is serial, the
// strongest form of the cap), missed-window collapse (each due row fires
// ONCE, next recomputed from now — a multi-window outage is one make-up
// shot, never a catch-up storm), and clock-rollback protection (a row
// whose last_run is in the future of the clock is skipped with a WARN and
// re-armed).
package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
)

// Runner executes ONE full-type job of a domain, identified by key
// (ADR-0044 decision 6's verbatim contract). Implementations are the SAME
// carrier the domain's manual face uses (TriggerFullSync / the RunOnce
// family) — idempotent-convergent; the scheduler never builds a second
// executor.
type Runner interface {
	// Run executes ONE full-type job of the domain, identified by key.
	// Idempotent-convergent: the SAME carrier the domain's manual face
	// uses; the scheduler never builds a second executor.
	Run(ctx context.Context, key string) error
}

// AuditSink is the consumer-side audit seam (the replication engine's
// shape, structurally satisfied by audit.Logger and audit.Recorder).
// Append failures are logged and swallowed — auditing must never fail a
// scheduled run.
type AuditSink interface {
	Append(ctx context.Context, e audit.Event) error
}

// Engine constants (ADR-0044 decision 11: internal constants, zero new
// config keys).
const (
	// DefaultTickInterval is the dispatch tick — the anchor's ±minute
	// firing precision (NFR-P74).
	DefaultTickInterval = time.Minute
	// errorTextLimit truncates last_error and the fail audit detail (the
	// replication engine's family value).
	errorTextLimit = 512
	// kickQueueSize bounds the manual-kick queue: a full queue is a
	// misconfiguration (nobody should kick that fast), refused loudly.
	kickQueueSize = 32
	// stateWriteTimeout bounds the detached context of the post-run
	// write-back: shutdown must still be able to land the run state.
	stateWriteTimeout = 10 * time.Second
	// auditActor is the audit trail's actor for engine-emitted words.
	auditActor = "scheduler"
)

// Metric family names (ADR-0044 decision 10; ADR-0022 posture — the shared
// expvar-free registry, registered here by the engine that owns them).
const (
	metricFiresTotal    = "binflow_scheduler_fires_total"
	metricFailuresTotal = "binflow_scheduler_failures_total"
)

// ErrUnknownDomain marks Register/Kick calls naming a domain outside the
// closed set, and ErrDuplicateDomain a second Register of a live domain
// (both assembly-time errors per ADR-0044 decision 6).
var (
	ErrUnknownDomain   = errors.New("scheduler: unknown domain (closed set: maintenance, backup, replication)")
	ErrDuplicateDomain = errors.New("scheduler: domain already registered")
	// ErrKickQueueFull: the manual-kick queue is saturated.
	ErrKickQueueFull = errors.New("scheduler: kick queue full")
)

// Options assembles the engine. The zero value beyond Store is the
// documented defaults; every field is a test or assembly seam.
type Options struct {
	// Store is required — the schedules ledger (metadata.ScheduleStore).
	Store metadata.ScheduleStore
	// Now stamps row times (UTC). nil = time.Now().UTC().
	Now func() time.Time
	// Logger receives the WARN/INFO lines. nil = slog.Default().
	Logger *slog.Logger
	// TickInterval is the dispatch cadence (<= 0 = DefaultTickInterval).
	// A seam for tests, not a knob.
	TickInterval time.Duration
	// Registry, when non-nil, receives the two scheduler metric families.
	// Registration errors surface at assembly (an honest startup failure,
	// not a silent metrics gap — the webhook dispatcher posture).
	Registry *metrics.Registry
	// Audit receives the run/fail words. nil = no auditing.
	Audit AuditSink
}

// Scheduler is the cron dispatch engine. Build with New, Register the
// domains' runners (assembly time), then run Run in a dedicated goroutine
// until the context is canceled. A Scheduler must not be copied after New.
type Scheduler struct {
	store metadata.ScheduleStore
	now   func() time.Time
	log   *slog.Logger
	tick  time.Duration
	audit AuditSink

	mu      sync.RWMutex
	runners map[string]Runner

	kicks chan kick

	mFires *metrics.Counter
	mFails *metrics.Counter
}

// kick is one manual-trigger request through the same dispatch path the
// tick uses (the test-acceleration seam).
type kick struct {
	domain, key string
}

// New assembles the engine. Store is required; the rest fall back to the
// documented defaults.
func New(opts Options) (*Scheduler, error) {
	if opts.Store == nil {
		return nil, errors.New("scheduler: New: store is nil")
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	tick := opts.TickInterval
	if tick <= 0 {
		tick = DefaultTickInterval
	}
	s := &Scheduler{
		store:   opts.Store,
		now:     now,
		log:     log,
		tick:    tick,
		audit:   opts.Audit,
		runners: map[string]Runner{},
		kicks:   make(chan kick, kickQueueSize),
	}
	if opts.Registry != nil {
		fires, err := opts.Registry.NewCounter(metricFiresTotal,
			"Scheduled full-type jobs fired by the cron scheduler since process start, by domain.")
		if err != nil {
			return nil, fmt.Errorf("scheduler: New: %w", err)
		}
		fails, err := opts.Registry.NewCounter(metricFailuresTotal,
			"Scheduled full-type jobs that failed, by domain, since process start.")
		if err != nil {
			return nil, fmt.Errorf("scheduler: New: %w", err)
		}
		s.mFires, s.mFails = fires, fails
	}
	return s, nil
}

// Register adds one domain's runner to the closed set (ADR-0044 decision
// 6): an unknown domain, a duplicate registration or a nil runner is an
// assembly-time error, not a runtime surprise. Safe to call before Run;
// registering while the loop dispatches is not (assembly is sequential,
// like every engine's).
func (s *Scheduler) Register(domain string, r Runner) error {
	if !knownDomain(domain) {
		return fmt.Errorf("%w: %q", ErrUnknownDomain, domain)
	}
	if r == nil {
		return fmt.Errorf("scheduler: Register %s: runner is nil", domain)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.runners[domain]; dup {
		return fmt.Errorf("%w: %s", ErrDuplicateDomain, domain)
	}
	s.runners[domain] = r
	return nil
}

func knownDomain(domain string) bool {
	switch domain {
	case DomainMaintenance, DomainBackup, DomainReplication:
		return true
	}
	return false
}

func (s *Scheduler) runner(domain string) (Runner, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.runners[domain]
	return r, ok
}

// Kick manually fires one schedule through the SAME dispatch path a due
// tick uses (ADR-0044 decision 1's equivalence seam — test acceleration
// and the parity leg T-450 asserts). It validates synchronously (domain
// registered, row present), then queues the fire onto the loop; the row's
// next_run re-arms exactly as a due fire would.
func (s *Scheduler) Kick(ctx context.Context, domain, key string) error {
	if !knownDomain(domain) {
		return fmt.Errorf("%w: %q", ErrUnknownDomain, domain)
	}
	if _, ok := s.runner(domain); !ok {
		return fmt.Errorf("scheduler: Kick %s/%s: no runner registered for domain", domain, key)
	}
	if _, err := s.store.Get(ctx, domain, key); err != nil {
		return fmt.Errorf("scheduler: Kick %s/%s: %w", domain, key, err)
	}
	select {
	case s.kicks <- kick{domain: domain, key: key}:
		return nil
	default:
		return fmt.Errorf("scheduler: Kick %s/%s: %w", domain, key, ErrKickQueueFull)
	}
}

// Run is the dispatch loop: a boot pass (the missed-window collapse — every
// overdue row fires ONCE, next recomputed from now), then every kick and
// every tick sweeps the due rows. Dispatch is serial by design: one
// goroutine owns every fire, so per-domain concurrency is 1 structurally
// (the strongest form of ADR-0044 decision 6's cap — a stalled carrier
// backpressures everything, which is the intended posture of a single
// maintenance window). Run returns when ctx is canceled; a run already in
// flight aborts at its own context boundary, and an aborted run leaves no
// run state (the row stays due; the next boot's collapse re-fires it once).
func (s *Scheduler) Run(ctx context.Context) error {
	s.census(ctx)
	s.sweep(ctx)
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case k := <-s.kicks:
			s.dispatch(ctx, k.domain, k.key)
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

// census logs the one-line ledger watermark (ADR-0044 decision 10's boot
// INFO): enabled rows and the nearest next-run.
func (s *Scheduler) census(ctx context.Context) {
	rows, err := s.store.List(ctx, "")
	if err != nil {
		s.log.WarnContext(ctx, "scheduler: boot census failed", "error", err.Error())
		return
	}
	enabled, nearest := 0, ""
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		enabled++
		if row.NextRunAt != "" && (nearest == "" || row.NextRunAt < nearest) {
			nearest = row.NextRunAt
		}
	}
	s.log.InfoContext(ctx, "scheduler: started",
		"tick_interval", s.tick.String(),
		"registered_domains", s.registeredDomains(),
		"enabled_schedules", enabled,
		"nearest_next_run", nearest)
}

func (s *Scheduler) registeredDomains() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.runners))
	for d := range s.runners {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// sweep fires every due row once (the tick's whole job): the store's due
// predicate is next_run <= now, each fire re-arms from now, so a row that
// went overdue across many ticks still fires exactly once — the collapse.
func (s *Scheduler) sweep(ctx context.Context) {
	now := s.now()
	rows, err := s.store.ListDue(ctx, now.Format(time.RFC3339))
	if err != nil {
		if metadata.IsStoreBusy(err) {
			s.log.DebugContext(ctx, "scheduler: due scan busy", "error", err.Error())
		} else {
			s.log.WarnContext(ctx, "scheduler: due scan failed", "error", err.Error())
		}
		return
	}
	for _, row := range rows {
		s.dispatchRow(ctx, row, now)
	}
}

// dispatchRow fires one due ledger row, applying the run-time guards
// before handing the job to runOne (the shared fire path).
func (s *Scheduler) dispatchRow(ctx context.Context, row *metadata.Schedule, now time.Time) {
	// Clock-rollback guard (ADR-0044 decision 9): the clock sits before
	// the row's last run — firing again would double-fire inside one
	// schedule period. Skip with a WARN and re-arm from now so the row
	// does not nag every tick.
	if row.LastRunAt != "" {
		if last, err := time.Parse(time.RFC3339, row.LastRunAt); err == nil && now.Before(last) {
			s.log.WarnContext(ctx, "scheduler: clock rollback suspected — skipping and re-arming",
				"domain", row.Domain, "key", row.Key,
				"last_run", row.LastRunAt, "now", now.Format(time.RFC3339))
			s.rearm(ctx, row, now)
			return
		}
	}
	r, ok := s.runner(row.Domain)
	if !ok {
		// A ledger row for a domain nobody registered (assembly order or
		// a future incremental registration): WARN and re-arm from now —
		// one notice per due period, never a tight loop.
		s.log.WarnContext(ctx, "scheduler: no runner registered for scheduled domain — re-arming",
			"domain", row.Domain, "key", row.Key)
		s.rearm(ctx, row, now)
		return
	}
	s.runOne(ctx, row.Domain, row.Key, r, now)
}

// dispatch is the kick path: identical to a due fire minus the guards the
// kick's synchronous validation already covered (the row was present and
// the domain registered at Kick time; a row deleted since simply skips
// its write-back).
func (s *Scheduler) dispatch(ctx context.Context, domain, key string) {
	r, ok := s.runner(domain)
	if !ok {
		s.log.WarnContext(ctx, "scheduler: kick arrived for unregistered domain", "domain", domain, "key", key)
		return
	}
	s.runOne(ctx, domain, key, r, s.now())
}

// runOne executes one fire and lands the run state: the runner's outcome,
// the audit word, the metric and the re-armed next_run (recomputed from
// the CURRENT expression — the row is re-read after the run so a config
// edit that landed mid-run is never clobbered by a stale snapshot).
func (s *Scheduler) runOne(ctx context.Context, domain, key string, r Runner, start time.Time) {
	runErr := r.Run(ctx, key)
	if runErr != nil && errors.Is(runErr, context.Canceled) {
		// Shutdown abort: not a failure, not a success — leave the row
		// exactly as it was (still due; the next boot's collapse fires it).
		s.log.InfoContext(ctx, "scheduler: run aborted by shutdown",
			"domain", domain, "key", key, "error", runErr.Error())
		return
	}
	ran := s.now()
	ranStamp := ran.Format(time.RFC3339)

	// Write-back on a detached context: ctx may already be canceled while
	// the run state must still land (the replication engine's posture).
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stateWriteTimeout)
	defer cancel()
	fresh, err := s.store.Get(wctx, domain, key)
	if err != nil && !errors.Is(err, metadata.ErrScheduleNotFound) {
		s.log.WarnContext(ctx, "scheduler: post-run read failed", "domain", domain, "key", key, "error", err.Error())
	}

	status, errText := "ok", ""
	if runErr != nil {
		status, errText = "failed", truncateErr(runErr.Error())
	}
	if fresh != nil {
		fresh.LastRunAt = ranStamp
		fresh.LastStatus = status
		fresh.LastError = errText
		fresh.UpdatedAt = ranStamp
		fresh.UpdatedBy = auditActor
		if fresh.Enabled {
			if next, nerr := Next(fresh.CronExpr, ran); nerr != nil {
				// An expression that stopped reaching the future (a year
				// domain gone by, an operator-edited row): WARN and park
				// the row unscheduled — it will not fire.
				s.log.WarnContext(ctx, "scheduler: cannot re-arm schedule",
					"domain", domain, "key", key, "cron", fresh.CronExpr, "error", nerr.Error())
				fresh.NextRunAt = ""
			} else {
				fresh.NextRunAt = next.Format(time.RFC3339)
			}
		} else {
			fresh.NextRunAt = "" // disabled: '' = unscheduled, the DDL's single state
		}
		if perr := s.store.Put(wctx, fresh); perr != nil {
			s.log.WarnContext(ctx, "scheduler: run-state write-back failed",
				"domain", domain, "key", key, "error", perr.Error())
		}
	}

	s.count(domain, runErr == nil)
	if fresh == nil && err != nil {
		// The row vanished mid-run (config delete) and the read itself
		// errored — the audit below still records the fire.
		s.log.WarnContext(ctx, "scheduler: schedule row vanished mid-run",
			"domain", domain, "key", key, "error", err.Error())
	}
	s.auditRun(wctx, domain, key, ran, ran.Sub(start), status, errText)
}

// count bumps the two metric families (nil registry = count nothing, gate
// exactly the same — the addon-gate posture).
func (s *Scheduler) count(domain string, ok bool) {
	if s.mFires != nil {
		s.mFires.Inc("domain", domain)
	}
	if !ok && s.mFails != nil {
		s.mFails.Inc("domain", domain)
	}
}

// auditRun records the run/fail word (ADR-0044 decision 10): the engine's
// view of one fired job, ON TOP of the carrier's own audit trail. Detail
// carries the key plus duration_ms on success / the truncated error on
// failure. Best-effort: failures log, never propagate.
func (s *Scheduler) auditRun(ctx context.Context, domain, key string, ran time.Time, took time.Duration, status, errText string) {
	if s.audit == nil {
		return
	}
	action, detail := domain+".schedule.run", map[string]any{"key": key}
	if status == "failed" {
		action = domain + ".schedule.fail"
		detail["error"] = errText
	} else {
		detail["duration_ms"] = took.Milliseconds()
	}
	b, err := json.Marshal(detail)
	if err != nil {
		b = []byte(`{"key":"?"}`) // unreachable: flat scalars always marshal
	}
	if err := s.audit.Append(ctx, audit.Event{
		Time:   ran.Format(time.RFC3339),
		Actor:  auditActor,
		Action: action,
		Detail: string(b),
	}); err != nil {
		s.log.WarnContext(ctx, "scheduler: audit append failed (best-effort, continuing)",
			"action", action, "error", err.Error())
	}
}

// rearm recomputes one row's next_run from now WITHOUT firing (the
// clock-rollback and unregistered-domain guards' landing): the collapse's
// write side, on the row's own current expression.
func (s *Scheduler) rearm(ctx context.Context, row *metadata.Schedule, now time.Time) {
	stamp := now.Format(time.RFC3339)
	fresh := row
	if cur, err := s.store.Get(ctx, row.Domain, row.Key); err == nil {
		fresh = cur // config may have moved since the scan
	} else if !errors.Is(err, metadata.ErrScheduleNotFound) {
		s.log.WarnContext(ctx, "scheduler: re-arm read failed",
			"domain", row.Domain, "key", row.Key, "error", err.Error())
		return
	}
	if next, err := Next(fresh.CronExpr, now); err == nil {
		fresh.NextRunAt = next.Format(time.RFC3339)
	} else {
		fresh.NextRunAt = ""
	}
	fresh.UpdatedAt = stamp
	fresh.UpdatedBy = auditActor
	if err := s.store.Put(ctx, fresh); err != nil {
		s.log.WarnContext(ctx, "scheduler: re-arm write failed",
			"domain", row.Domain, "key", row.Key, "error", err.Error())
	}
}

// truncateErr clips an error text to the ledger's summary budget.
func truncateErr(msg string) string {
	if len(msg) <= errorTextLimit {
		return msg
	}
	return msg[:errorTextLimit]
}
