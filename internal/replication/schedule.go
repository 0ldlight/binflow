package replication

// The cron-scheduled full-sync carrier (M16 T-450, FR-150.4 / ADR-0044
// decisions 6 and 8③): the replication domain's Runner. A scheduled fire
// rides TriggerFullSync — the SAME carrier the manual Replicate Now face
// (POST /api/v1/replications/{id}/run) uses — never a second executor and
// never a new queue; the tasks it appends are the rows the event track's
// worker drains, and convergence is the target-side sha256 idempotent hit
// (the L48 content-layer zero-duplicate-delivery posture).
//
// The type deliberately satisfies the scheduler engine's Runner contract
// STRUCTURALLY (Run(ctx, key string) error) without importing
// internal/scheduler: the dependency direction the ADR pins — the engine
// consumes domain carriers, domains never reach back into the engine. The
// ledger key is the config id in its TEXT form (ADR-0044 decision 2).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// ScheduleRunner is the replication domain's cron carrier: one Run fires one
// config's full reconciliation. Build with NewScheduleRunner and register it
// under the replication domain at assembly time.
type ScheduleRunner struct {
	store  Store
	sched  metadata.ScheduleStore
	engine *Engine
	log    *slog.Logger
}

// NewScheduleRunner assembles the runner. store and engine are the SAME
// instances the REST faces and the worker loop use (a second engine would
// double-claim the task ledger); sched is the schedules ledger the runner's
// tombstone/park arms keep honest.
func NewScheduleRunner(store Store, sched metadata.ScheduleStore, engine *Engine) *ScheduleRunner {
	return &ScheduleRunner{store: store, sched: sched, engine: engine, log: slog.Default()}
}

// Run executes ONE scheduled full sync of the config named by key (the
// config id, TEXT form). The arms, in order:
//
//   - config gone (deleted through the REST face whose ledger-row teardown
//     raced or failed): the orphan ledger row is tombstone-cleaned and the
//     fire is a no-op success — "no config = nothing to schedule".
//   - config disabled: the ledger row is parked (enabled=0, next=”) to
//     mirror the config's own switch and the fire is a no-op success — the
//     family's one-switch-parks-both-tracks posture.
//   - push blocked (the global brake, §9.2-B-4): a SKIP, not a failure —
//     the anchored posture blocks the cron track and the event track
//     together; the fire lands ok with a WARN naming the brake.
//   - otherwise: TriggerFullSync — seed the pending rows, wake the worker,
//     return. The push itself is the worker loop's business (async seeding,
//     §9.2-A-3); a scheduled fire that seeded nothing (empty repository) is
//     a successful no-op.
func (r *ScheduleRunner) Run(ctx context.Context, key string) error {
	id, err := strconv.ParseInt(key, 10, 64)
	if err != nil || id < 1 {
		return fmt.Errorf("replication: schedule key %q is not a config id", key)
	}
	cfg, err := r.store.GetConfig(ctx, id)
	if errors.Is(err, ErrConfigNotFound) {
		r.tombstone(ctx, key)
		return nil
	}
	if err != nil {
		return fmt.Errorf("replication: schedule %s: reading config: %w", key, err)
	}
	if !cfg.Enabled {
		return r.park(ctx, key)
	}
	res, err := r.engine.TriggerFullSync(ctx, cfg)
	switch {
	case err == nil:
		r.log.InfoContext(ctx, "replication: scheduled full sync seeded",
			"config", cfg.Name, "id", cfg.ID,
			"scheduled", res.Scheduled, "capped", res.Capped)
		return nil
	case errors.Is(err, ErrPushBlocked):
		// The brake blocks both tracks (§9.2-B-4): the fire is a skip, not
		// a failure — the ledger keeps its cron and fires again next
		// period; unblocking resumes without any reconfiguration.
		r.log.WarnContext(ctx, "replication: scheduled full sync skipped: push replication is blocked",
			"config", cfg.Name, "id", cfg.ID)
		return nil
	case errors.Is(err, ErrTriggerDisabled):
		// A raced enabled flip between the read above and the trigger: the
		// park arm's outcome, arrived at honestly.
		return r.park(ctx, key)
	case errors.Is(err, ErrNoMetaSeam):
		// An assembly fault is a genuine failure — the schedule's fail
		// state and the schedule.fail audit word are the honest record.
		return fmt.Errorf("replication: schedule %s: %w", key, err)
	default:
		return fmt.Errorf("replication: schedule %s: triggering full sync of %s: %w", key, cfg.Name, err)
	}
}

// tombstone removes an orphan ledger row (its config is gone — the REST
// face's delete normally removed it first). Best-effort: the row re-fires
// and re-cleans if the delete fails.
func (r *ScheduleRunner) tombstone(ctx context.Context, key string) {
	if err := r.sched.Delete(ctx, "replication", key); err != nil && !errors.Is(err, metadata.ErrScheduleNotFound) {
		r.log.WarnContext(ctx, "replication: schedule row outlived its config (tombstone failed)",
			"key", key, "error", err.Error())
		return
	}
	r.log.InfoContext(ctx, "replication: schedule row outlived its config (tombstoned)", "key", key)
}

// park mirrors a disabled config into its ledger row (enabled=0, next=”
// — the DDL's unscheduled shape, cron kept so re-enabling recomputes from
// now) and answers nil: a parked schedule fired and correctly did nothing.
func (r *ScheduleRunner) park(ctx context.Context, key string) error {
	row, err := r.sched.Get(ctx, "replication", key)
	if errors.Is(err, metadata.ErrScheduleNotFound) {
		return nil // nothing scheduled, nothing to park
	}
	if err != nil {
		return fmt.Errorf("replication: schedule %s: reading ledger row: %w", key, err)
	}
	row.Enabled = false
	row.NextRunAt = ""
	row.UpdatedAt = metadata.Now()
	row.UpdatedBy = "scheduler"
	if err := r.sched.Put(ctx, row); err != nil {
		return fmt.Errorf("replication: schedule %s: parking disabled config: %w", key, err)
	}
	r.log.InfoContext(ctx, "replication: config disabled — schedule parked", "key", key)
	return nil
}
