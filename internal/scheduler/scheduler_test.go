package scheduler_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
	"github.com/lzwzzy/binflow/internal/scheduler"
)

// The engine's behavior tests (M16 T-446 AC1/AC2): the Run loop over the
// REAL schedules ledger (a real SQLite metadata store, so the 021 migration
// and the due predicate ride along), the Kick equivalence seam, the three
// run-time guards and the audit/metrics facets.

// ---- test scaffolding ----

func openLedger(t *testing.T) metadata.Store {
	t.Helper()
	st, err := metadata.Open(context.Background(), metadata.Options{
		Driver: "sqlite", Path: filepath.Join(t.TempDir(), "binflow.db"), AdminPassword: "it-admin-pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// fakeClock is a controllable now(): every read advances the clock by one
// step, so runOne's start/ran stamps and the sweep's due boundary are all
// deterministic without sleeps.
type fakeClock struct {
	mu   sync.Mutex
	base time.Time
	step time.Duration
	n    int
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.base.Add(time.Duration(c.n) * c.step)
	c.n++
	return t
}

func (c *fakeClock) at(i int) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.base.Add(time.Duration(i) * c.step)
}

// recRunner records every fire; block, when non-nil, parks each Run until
// closed — the per-domain-serial test's gate.
type recRunner struct {
	mu       sync.Mutex
	calls    []string
	err      error
	block    chan struct{}
	inFlight atomic.Int32
	maxSeen  atomic.Int32
}

func (r *recRunner) Run(_ context.Context, key string) error {
	cur := r.inFlight.Add(1)
	for {
		seen := r.maxSeen.Load()
		if cur <= seen || r.maxSeen.CompareAndSwap(seen, cur) {
			break
		}
	}
	if r.block != nil {
		<-r.block
	}
	r.inFlight.Add(-1)
	r.mu.Lock()
	r.calls = append(r.calls, key)
	r.mu.Unlock()
	return r.err
}

func (r *recRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// auditSink collects the engine's audit words.
type auditSink struct {
	mu     sync.Mutex
	events []audit.Event
}

func (s *auditSink) Append(_ context.Context, e audit.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}

func (s *auditSink) actions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.events))
	for _, e := range s.events {
		out = append(out, e.Action)
	}
	return out
}

func putSchedule(t *testing.T, st metadata.Store, sc *metadata.Schedule) {
	t.Helper()
	if err := st.Schedules().Put(context.Background(), sc); err != nil {
		t.Fatalf("schedules put: %v", err)
	}
}

func getSchedule(t *testing.T, st metadata.Store, domain, key string) *metadata.Schedule {
	t.Helper()
	sc, err := st.Schedules().Get(context.Background(), domain, key)
	if err != nil {
		t.Fatalf("schedules get %s/%s: %v", domain, key, err)
	}
	return sc
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// waitForRow waits for a durable row effect: the runner's return and the
// ledger's write-back are two steps, assertions must observe the second.
func waitForRow(t *testing.T, st metadata.Store, domain, key, what string, cond func(*metadata.Schedule) bool) {
	t.Helper()
	waitFor(t, what, func() bool {
		row, err := st.Schedules().Get(context.Background(), domain, key)
		return err == nil && cond(row)
	})
}

// ---- New / Register ----

func TestSchedulerNewRequiresStore(t *testing.T) {
	if _, err := scheduler.New(scheduler.Options{}); err == nil {
		t.Fatal("New without a store = nil error, want refusal")
	}
}

func TestSchedulerRegisterClosedDomainSet(t *testing.T) {
	st := openLedger(t)
	s, err := scheduler.New(scheduler.Options{Store: st.Schedules()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r := &recRunner{}
	if err := s.Register("observability", r); !errors.Is(err, scheduler.ErrUnknownDomain) {
		t.Errorf("Register(unknown) = %v, want ErrUnknownDomain", err)
	}
	if err := s.Register(scheduler.DomainBackup, nil); err == nil {
		t.Error("Register(nil runner) = nil error, want refusal")
	}
	for _, d := range scheduler.Domains() {
		if err := s.Register(d, r); err != nil {
			t.Fatalf("Register(%s): %v", d, err)
		}
	}
	if err := s.Register(scheduler.DomainBackup, r); !errors.Is(err, scheduler.ErrDuplicateDomain) {
		t.Errorf("Register(duplicate) = %v, want ErrDuplicateDomain", err)
	}
}

// ---- the fire path: due sweep and Kick are the same dispatch ----

func TestSchedulerFiresDueRowAndRearms(t *testing.T) {
	st := openLedger(t)
	clock := &fakeClock{base: thu, step: time.Second}
	// A row already overdue: daily 02:00, last window long gone.
	putSchedule(t, st, &metadata.Schedule{
		Domain: scheduler.DomainBackup, Key: "daily",
		CronExpr: "0 0 2 ? * *", Enabled: true,
		NextRunAt: clock.at(0).Add(-time.Hour).Format(time.RFC3339),
		CreatedAt: clock.at(0).Format(time.RFC3339), CreatedBy: "test",
		UpdatedAt: clock.at(0).Format(time.RFC3339), UpdatedBy: "test",
	})
	r := &recRunner{}
	s, err := scheduler.New(scheduler.Options{
		Store: st.Schedules(), Now: clock.now, TickInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Register(scheduler.DomainBackup, r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	waitFor(t, "the due row to fire", func() bool { return r.count() >= 1 })
	waitForRow(t, st, scheduler.DomainBackup, "daily", "the run state to land",
		func(row *metadata.Schedule) bool { return row.LastRunAt != "" })
	cancel()

	// The boot sweep fired it once and re-armed from the sweep's now.
	row := getSchedule(t, st, scheduler.DomainBackup, "daily")
	if row.LastRunAt == "" || row.LastStatus != "ok" || row.LastError != "" {
		t.Errorf("run state = %q/%q/%q, want a stamped ok", row.LastRunAt, row.LastStatus, row.LastError)
	}
	if row.UpdatedBy != "scheduler" {
		t.Errorf("updated_by = %q, want scheduler", row.UpdatedBy)
	}
	nextRun, err := time.Parse(time.RFC3339, row.NextRunAt)
	if err != nil {
		t.Fatalf("next_run_at %q: %v", row.NextRunAt, err)
	}
	if !nextRun.After(clock.at(0)) {
		t.Errorf("next_run_at %s not re-armed past the fire instant", row.NextRunAt)
	}
}

func TestSchedulerKickUsesTheDueDispatchPath(t *testing.T) {
	st := openLedger(t)
	clock := &fakeClock{base: thu, step: time.Second}
	next, err := scheduler.Next("0 0 2 ? * MON-FRI", clock.at(0))
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	putSchedule(t, st, &metadata.Schedule{
		Domain: scheduler.DomainMaintenance, Key: "gc",
		CronExpr: "0 0 2 ? * MON-FRI", Enabled: true,
		NextRunAt: next.Format(time.RFC3339), // far future: only a kick reaches it
		CreatedAt: clock.at(0).Format(time.RFC3339), CreatedBy: "test",
		UpdatedAt: clock.at(0).Format(time.RFC3339), UpdatedBy: "test",
	})
	r := &recRunner{}
	s, err := scheduler.New(scheduler.Options{
		Store: st.Schedules(), Now: clock.now, TickInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Register(scheduler.DomainMaintenance, r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	if err := s.Kick(ctx, scheduler.DomainMaintenance, "gc"); err != nil {
		t.Fatalf("Kick: %v", err)
	}
	waitFor(t, "the kicked row to fire", func() bool { return r.count() >= 1 })
	waitForRow(t, st, scheduler.DomainMaintenance, "gc", "the run state to land",
		func(row *metadata.Schedule) bool { return row.LastRunAt != "" && row.UpdatedBy == "scheduler" })
	cancel()

	// Same landing as a due fire: run state stamped, next re-armed — the
	// equivalence seam's whole contract.
	row := getSchedule(t, st, scheduler.DomainMaintenance, "gc")
	if row.LastStatus != "ok" {
		t.Errorf("last_status = %q, want ok", row.LastStatus)
	}
	rearmed, err := scheduler.Next("0 0 2 ? * MON-FRI", clock.at(0))
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if row.NextRunAt != rearmed.Format(time.RFC3339) {
		t.Errorf("next_run_at = %q, want the re-armed %q", row.NextRunAt, rearmed.Format(time.RFC3339))
	}
}

func TestSchedulerKickValidation(t *testing.T) {
	st := openLedger(t)
	s, err := scheduler.New(scheduler.Options{Store: st.Schedules()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := s.Kick(ctx, "observability", "x"); !errors.Is(err, scheduler.ErrUnknownDomain) {
		t.Errorf("Kick(unknown domain) = %v, want ErrUnknownDomain", err)
	}
	if err := s.Register(scheduler.DomainBackup, &recRunner{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := s.Kick(ctx, scheduler.DomainBackup, "missing"); !errors.Is(err, metadata.ErrScheduleNotFound) {
		t.Errorf("Kick(missing row) = %v, want ErrScheduleNotFound", err)
	}
	putSchedule(t, st, &metadata.Schedule{
		Domain: scheduler.DomainBackup, Key: "daily", CronExpr: "0 0 2 ? * *",
		CreatedAt: thu.Format(time.RFC3339), CreatedBy: "test",
		UpdatedAt: thu.Format(time.RFC3339), UpdatedBy: "test",
	})
	if err := s.Kick(ctx, scheduler.DomainReplication, "daily"); err == nil {
		t.Error("Kick(unregistered domain) = nil, want refusal")
	}
}

// ---- the run-time guards ----

// Missed-window collapse (ADR-0044 decision 5/9): a row that went overdue
// across MANY windows fires exactly once — next is recomputed from now,
// there is no catch-up.
func TestSchedulerMissedWindowsCollapseToOneFire(t *testing.T) {
	st := openLedger(t)
	clock := &fakeClock{base: thu, step: time.Second}
	// Every minute; the row is three windows overdue.
	putSchedule(t, st, &metadata.Schedule{
		Domain: scheduler.DomainBackup, Key: "daily",
		CronExpr: "0 * * ? * *", Enabled: true,
		NextRunAt: clock.at(0).Add(-3 * time.Minute).Format(time.RFC3339),
		CreatedAt: clock.at(0).Format(time.RFC3339), CreatedBy: "test",
		UpdatedAt: clock.at(0).Format(time.RFC3339), UpdatedBy: "test",
	})
	r := &recRunner{}
	s, err := scheduler.New(scheduler.Options{
		Store: st.Schedules(), Now: clock.now, TickInterval: 30 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Register(scheduler.DomainBackup, r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()
	waitFor(t, "the collapsed fire", func() bool { return r.count() >= 1 })
	time.Sleep(150 * time.Millisecond) // several more ticks elapse
	cancel()
	if n := r.count(); n != 1 {
		t.Errorf("fires = %d after several ticks over a multi-window-overdue row, want exactly 1 (collapse)", n)
	}
}

// Clock rollback (ADR-0044 decision 9): a due row whose last_run is in the
// clock's future is skipped with a WARN and re-armed — never double-fired.
func TestSchedulerClockRollbackSkipsAndRearms(t *testing.T) {
	st := openLedger(t)
	clock := &fakeClock{base: thu, step: time.Second}
	now := clock.at(0)
	putSchedule(t, st, &metadata.Schedule{
		Domain: scheduler.DomainReplication, Key: "cfg-1",
		CronExpr: "0 * * ? * *", Enabled: true,
		NextRunAt: now.Add(-time.Minute).Format(time.RFC3339), // due
		LastRunAt: now.Add(time.Hour).Format(time.RFC3339),    // ...but the clock sits before the last run
		CreatedAt: now.Format(time.RFC3339), CreatedBy: "test",
		UpdatedAt: now.Format(time.RFC3339), UpdatedBy: "test",
	})
	r := &recRunner{}
	s, err := scheduler.New(scheduler.Options{
		Store: st.Schedules(), Now: clock.now, TickInterval: 30 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Register(scheduler.DomainReplication, r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()
	time.Sleep(150 * time.Millisecond)
	cancel()
	if n := r.count(); n != 0 {
		t.Errorf("fires = %d under a rolled-back clock, want 0", n)
	}
	row := getSchedule(t, st, scheduler.DomainReplication, "cfg-1")
	if row.NextRunAt == "" || row.NextRunAt <= now.Format(time.RFC3339) {
		t.Errorf("next_run_at = %q, want re-armed into the future", row.NextRunAt)
	}
	if row.LastRunAt != now.Add(time.Hour).Format(time.RFC3339) {
		t.Errorf("last_run_at = %q, want the rollback guard to leave it untouched", row.LastRunAt)
	}
}

// Per-domain concurrency 1 (ADR-0044 decision 6/9): the serial loop never
// lets two fires of one domain overlap — the structural cap.
func TestSchedulerPerDomainConcurrencyIsOne(t *testing.T) {
	st := openLedger(t)
	clock := &fakeClock{base: thu, step: time.Second}
	now := clock.at(0)
	next, _ := scheduler.Next("0 0 2 ? * *", now)
	putSchedule(t, st, &metadata.Schedule{
		Domain: scheduler.DomainMaintenance, Key: "gc",
		CronExpr: "0 0 2 ? * *", Enabled: true,
		NextRunAt: next.Format(time.RFC3339),
		CreatedAt: now.Format(time.RFC3339), CreatedBy: "test",
		UpdatedAt: now.Format(time.RFC3339), UpdatedBy: "test",
	})
	block := make(chan struct{})
	r := &recRunner{block: block}
	s, err := scheduler.New(scheduler.Options{
		Store: st.Schedules(), Now: clock.now, TickInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Register(scheduler.DomainMaintenance, r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()
	if err := s.Kick(ctx, scheduler.DomainMaintenance, "gc"); err != nil {
		t.Fatalf("kick 1: %v", err)
	}
	if err := s.Kick(ctx, scheduler.DomainMaintenance, "gc"); err != nil {
		t.Fatalf("kick 2: %v", err)
	}
	waitFor(t, "the first fire to enter the runner", func() bool { return r.inFlight.Load() >= 1 })
	time.Sleep(50 * time.Millisecond) // the second kick is queued behind the parked first
	close(block)
	waitFor(t, "both fires to complete", func() bool { return r.count() >= 2 })
	cancel()
	if got := r.maxSeen.Load(); got != 1 {
		t.Errorf("max in-flight runner calls = %d, want 1 (per-domain serial)", got)
	}
}

// A disabled row never fires; ” next_run is not due (the DDL's single
// disabled state).
func TestSchedulerDisabledRowNeverFires(t *testing.T) {
	st := openLedger(t)
	clock := &fakeClock{base: thu, step: time.Second}
	putSchedule(t, st, &metadata.Schedule{
		Domain: scheduler.DomainBackup, Key: "paused",
		CronExpr: "0 * * ? * *", Enabled: false, NextRunAt: "",
		CreatedAt: clock.at(0).Format(time.RFC3339), CreatedBy: "test",
		UpdatedAt: clock.at(0).Format(time.RFC3339), UpdatedBy: "test",
	})
	r := &recRunner{}
	s, _ := scheduler.New(scheduler.Options{Store: st.Schedules(), Now: clock.now, TickInterval: 30 * time.Millisecond})
	if err := s.Register(scheduler.DomainBackup, r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()
	time.Sleep(120 * time.Millisecond)
	cancel()
	if n := r.count(); n != 0 {
		t.Errorf("fires = %d on a disabled row, want 0", n)
	}
}

// Failure landing: the runner's error becomes last_status=failed, a
// truncated last_error, the fail audit word and the failure metric — and
// the row still re-arms (one bad pass never kills the schedule).
func TestSchedulerFailureLandsAndRearms(t *testing.T) {
	st := openLedger(t)
	clock := &fakeClock{base: thu, step: time.Second}
	putSchedule(t, st, &metadata.Schedule{
		Domain: scheduler.DomainBackup, Key: "daily",
		CronExpr: "0 0 2 ? * *", Enabled: true,
		NextRunAt: clock.at(0).Add(-time.Minute).Format(time.RFC3339),
		CreatedAt: clock.at(0).Format(time.RFC3339), CreatedBy: "test",
		UpdatedAt: clock.at(0).Format(time.RFC3339), UpdatedBy: "test",
	})
	long := "boom: " + strings.Repeat("x", 900)
	r := &recRunner{err: errors.New(long)}
	sink := &auditSink{}
	reg := metrics.NewRegistry()
	s, _ := scheduler.New(scheduler.Options{
		Store: st.Schedules(), Now: clock.now,
		TickInterval: 50 * time.Millisecond, Audit: sink, Registry: reg,
	})
	if err := s.Register(scheduler.DomainBackup, r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()
	waitFor(t, "the failed fire", func() bool { return r.count() >= 1 })
	waitForRow(t, st, scheduler.DomainBackup, "daily", "the failed run state to land",
		func(row *metadata.Schedule) bool { return row.LastStatus == "failed" })
	cancel()

	row := getSchedule(t, st, scheduler.DomainBackup, "daily")
	if row.LastStatus != "failed" {
		t.Errorf("last_status = %q, want failed", row.LastStatus)
	}
	if len(row.LastError) > 512 || !strings.HasPrefix(row.LastError, "boom:") {
		t.Errorf("last_error = %q..., want the truncated summary", row.LastError[:32])
	}
	if row.NextRunAt == "" {
		t.Error("next_run_at empty after a failed run, want re-armed")
	}
	actions := sink.actions()
	if len(actions) != 1 || actions[0] != audit.ActionBackupScheduleFail {
		t.Errorf("audit actions = %v, want exactly [backup.schedule.fail]", actions)
	}
	if e := sink.events[0]; e.Actor != "scheduler" || !strings.Contains(e.Detail, `"key":"daily"`) {
		t.Errorf("fail event = %+v, want actor scheduler and the key in detail", e)
	}
	out := reg.Format()
	if !strings.Contains(out, `binflow_scheduler_fires_total{domain="backup"} 1`) {
		t.Errorf("fires metric missing:\n%s", out)
	}
	if !strings.Contains(out, `binflow_scheduler_failures_total{domain="backup"} 1`) {
		t.Errorf("failures metric missing:\n%s", out)
	}
}

// Success landing: the run audit word with the key and a duration, the
// fires metric — and the carrier's own audit words are the carriers' to
// emit (none here: the engine adds its layer, it does not replace theirs).
func TestSchedulerSuccessAuditsAndCounts(t *testing.T) {
	st := openLedger(t)
	clock := &fakeClock{base: thu, step: time.Second}
	next, _ := scheduler.Next("0 0 2 ? * *", clock.at(0))
	putSchedule(t, st, &metadata.Schedule{
		Domain: scheduler.DomainReplication, Key: "cfg-9",
		CronExpr: "0 0 2 ? * *", Enabled: true,
		NextRunAt: next.Format(time.RFC3339),
		CreatedAt: clock.at(0).Format(time.RFC3339), CreatedBy: "test",
		UpdatedAt: clock.at(0).Format(time.RFC3339), UpdatedBy: "test",
	})
	r := &recRunner{}
	sink := &auditSink{}
	reg := metrics.NewRegistry()
	s, _ := scheduler.New(scheduler.Options{
		Store: st.Schedules(), Now: clock.now, Audit: sink, Registry: reg,
	})
	if err := s.Register(scheduler.DomainReplication, r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()
	if err := s.Kick(ctx, scheduler.DomainReplication, "cfg-9"); err != nil {
		t.Fatalf("Kick: %v", err)
	}
	waitFor(t, "the fire", func() bool { return r.count() >= 1 })
	waitFor(t, "the run audit word to land", func() bool { return len(sink.actions()) >= 1 })
	cancel()
	actions := sink.actions()
	if len(actions) != 1 || actions[0] != audit.ActionReplicationScheduleRun {
		t.Fatalf("audit actions = %v, want exactly [replication.schedule.run]", actions)
	}
	if !strings.Contains(sink.events[0].Detail, "duration_ms") {
		t.Errorf("run detail = %s, want duration_ms", sink.events[0].Detail)
	}
	if out := reg.Format(); !strings.Contains(out, `binflow_scheduler_fires_total{domain="replication"} 1`) {
		t.Errorf("fires metric missing:\n%s", out)
	}
}

// A config edit landing mid-run is never clobbered: the write-back re-reads
// the row and re-arms off the CURRENT expression.
func TestSchedulerMidRunConfigEditSurvivesWriteBack(t *testing.T) {
	st := openLedger(t)
	clock := &fakeClock{base: thu, step: time.Second}
	next, _ := scheduler.Next("0 0 2 ? * *", clock.at(0))
	putSchedule(t, st, &metadata.Schedule{
		Domain: scheduler.DomainMaintenance, Key: "gc",
		CronExpr: "0 0 2 ? * *", Enabled: true,
		NextRunAt: next.Format(time.RFC3339),
		CreatedAt: clock.at(0).Format(time.RFC3339), CreatedBy: "test",
		UpdatedAt: clock.at(0).Format(time.RFC3339), UpdatedBy: "test",
	})
	block := make(chan struct{})
	r := &recRunner{block: block}
	s, _ := scheduler.New(scheduler.Options{Store: st.Schedules(), Now: clock.now})
	if err := s.Register(scheduler.DomainMaintenance, r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()
	if err := s.Kick(ctx, scheduler.DomainMaintenance, "gc"); err != nil {
		t.Fatalf("Kick: %v", err)
	}
	waitFor(t, "the run to park inside the runner", func() bool { return r.inFlight.Load() == 1 })

	// The operator rewrites the schedule while the job runs.
	newExpr := "0 30 4 * * ?"
	newNext, err := scheduler.Next(newExpr, clock.at(1))
	if err != nil {
		t.Fatalf("Next(new): %v", err)
	}
	putSchedule(t, st, &metadata.Schedule{
		Domain: scheduler.DomainMaintenance, Key: "gc",
		CronExpr: newExpr, Enabled: true, NextRunAt: newNext.Format(time.RFC3339),
		CreatedAt: clock.at(0).Format(time.RFC3339), CreatedBy: "test",
		UpdatedAt: clock.at(2).Format(time.RFC3339), UpdatedBy: "operator",
	})
	close(block)
	waitFor(t, "the write-back to land", func() bool {
		row, err := st.Schedules().Get(context.Background(), scheduler.DomainMaintenance, "gc")
		return err == nil && row.LastRunAt != "" && row.UpdatedBy == "scheduler"
	})
	cancel()

	row := getSchedule(t, st, scheduler.DomainMaintenance, "gc")
	if row.CronExpr != newExpr {
		t.Errorf("cron_expr = %q, want the operator's mid-run edit %q preserved", row.CronExpr, newExpr)
	}
	if row.NextRunAt != newNext.Format(time.RFC3339) {
		t.Errorf("next_run_at = %q, want re-arm off the new expression (%q)", row.NextRunAt, newNext.Format(time.RFC3339))
	}
	if row.LastStatus != "ok" {
		t.Errorf("last_status = %q, want ok (the run itself succeeded)", row.LastStatus)
	}
}

// Shutdown abort: a run canceled by ctx leaves no run state — the row stays
// due and the next boot's collapse re-fires it once (convergent by the
// carriers' idempotence).
func TestSchedulerShutdownAbortLeavesRowUntouched(t *testing.T) {
	st := openLedger(t)
	clock := &fakeClock{base: thu, step: time.Second}
	next, _ := scheduler.Next("0 0 2 ? * *", clock.at(0))
	putSchedule(t, st, &metadata.Schedule{
		Domain: scheduler.DomainMaintenance, Key: "gc",
		CronExpr: "0 0 2 ? * *", Enabled: true,
		NextRunAt: next.Format(time.RFC3339),
		CreatedAt: clock.at(0).Format(time.RFC3339), CreatedBy: "test",
		UpdatedAt: clock.at(0).Format(time.RFC3339), UpdatedBy: "test",
	})
	abort := make(chan struct{})
	r := &canceledRunner{abort: abort}
	s, _ := scheduler.New(scheduler.Options{Store: st.Schedules(), Now: clock.now})
	if err := s.Register(scheduler.DomainMaintenance, r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = s.Run(ctx); close(done) }()
	if err := s.Kick(ctx, scheduler.DomainMaintenance, "gc"); err != nil {
		t.Fatalf("Kick: %v", err)
	}
	waitFor(t, "the run to park", func() bool { return r.entered.Load() })
	cancel()     // shutdown first...
	close(abort) // ...then release the runner, which observes the canceled ctx
	<-done

	row := getSchedule(t, st, scheduler.DomainMaintenance, "gc")
	if row.LastRunAt != "" || row.LastStatus != "" || row.UpdatedBy == "scheduler" {
		t.Errorf("aborted run left state: last_run=%q status=%q updated_by=%q, want untouched",
			row.LastRunAt, row.LastStatus, row.UpdatedBy)
	}
}

// canceledRunner parks until released, then answers context.Canceled — the
// shutdown-abort shape of a long carrier.
type canceledRunner struct {
	abort   chan struct{}
	entered atomic.Bool
}

func (r *canceledRunner) Run(ctx context.Context, _ string) error {
	r.entered.Store(true)
	<-r.abort
	return ctx.Err()
}

// A due row of a domain nobody registered: WARN + re-arm (no tight loop,
// no fire) — the incremental-registration posture.
func TestSchedulerUnregisteredDomainRearmsWithoutFiring(t *testing.T) {
	st := openLedger(t)
	clock := &fakeClock{base: thu, step: time.Second}
	putSchedule(t, st, &metadata.Schedule{
		Domain: scheduler.DomainReplication, Key: "cfg-2",
		CronExpr: "0 * * ? * *", Enabled: true,
		NextRunAt: clock.at(0).Add(-time.Minute).Format(time.RFC3339),
		CreatedAt: clock.at(0).Format(time.RFC3339), CreatedBy: "test",
		UpdatedAt: clock.at(0).Format(time.RFC3339), UpdatedBy: "test",
	})
	r := &recRunner{}
	s, _ := scheduler.New(scheduler.Options{Store: st.Schedules(), Now: clock.now, TickInterval: 30 * time.Millisecond})
	// maintenance registered, replication deliberately NOT.
	if err := s.Register(scheduler.DomainMaintenance, r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()
	time.Sleep(120 * time.Millisecond)
	cancel()
	if n := r.count(); n != 0 {
		t.Errorf("fires = %d, want 0 (wrong-domain runner must not fire)", n)
	}
	row := getSchedule(t, st, scheduler.DomainReplication, "cfg-2")
	if row.NextRunAt == "" || row.NextRunAt <= clock.at(1).Format(time.RFC3339) {
		t.Errorf("next_run_at = %q, want re-armed past now", row.NextRunAt)
	}
}
