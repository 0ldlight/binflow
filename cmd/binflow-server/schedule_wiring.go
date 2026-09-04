package main

// The cron scheduler's assembly (M16 T-450, FR-150.3/.4 / ADR-0044 decision
// 6): build the engine over the 021 ledger, register the three consuming
// domains' carriers, run the loop for the server's lifetime. The carriers
// are the SAME ones the manual faces ride — maintenance/gc is the REST gc
// kernel (httpapi.Server.RunGC), the cleanup slots are CleanupEngine.RunOnce,
// backup is the export kernel the CLI shares (exportSnapshot), replication
// is TriggerFullSync through the domain's ScheduleRunner — never a second
// executor anywhere (the ADR's one-carrier law).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/replication"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/scheduler"
)

// schedulerActor is the actor name the carriers' own audit rows record for
// cron-triggered runs (the engine's schedule.run/fail words carry
// "scheduler" themselves; this is what the CARRIER rows say).
const schedulerActor = "scheduler"

// maintenanceRunner is the maintenance domain's carrier: one Run dispatches
// on the slot key. Both cleanup slots ride the SAME RunOnce full pass (the
// ADR decision-8① closed-set reading: the two Artifactory cleanup families
// are BinFlow's one three-leg pass — there is no virtual-only leg to fire).
type maintenanceRunner struct {
	srv     *httpapi.Server
	cleanup *repo.CleanupEngine
	log     *slog.Logger
}

// Run implements the scheduler's Runner contract (structurally — cmd hands
// this to Register; the interface lives with its consumer).
func (m *maintenanceRunner) Run(ctx context.Context, key string) error {
	switch key {
	case "gc":
		// The full pass: apply mode, the CONFIGURED grace (an explicit
		// graceHours is the REST face's per-request lever; a scheduled run
		// takes the standing policy — ADR-0044 decision 8①'s "grace 兜底
		// 不缩短").
		res, err := m.srv.RunGC(ctx, httpapi.GCRunRequest{Actor: schedulerActor, Apply: true})
		if err != nil {
			return fmt.Errorf("scheduled gc: %w", err)
		}
		m.log.InfoContext(ctx, "scheduled gc complete",
			"candidates", res.CandidateCount, "deleted", res.DeletedCount)
		return nil
	case "cleanup-unused-cache", "cleanup-virtual":
		rep, err := m.cleanup.RunOnce(ctx, repo.CleanupRunOptions{
			Trigger: repo.CleanupTriggerCron,
			Apply:   true,
			Actor:   repo.ActorCleanup,
		})
		if err != nil && rep == nil {
			return fmt.Errorf("scheduled cleanup (%s): %w", key, err)
		}
		if rep != nil && !rep.OK {
			return fmt.Errorf("scheduled cleanup (%s): %s", key, rep.Error)
		}
		m.log.InfoContext(ctx, "scheduled cleanup complete", "slot", key, "repos", len(rep.Repos))
		return nil
	default:
		// An unknown key cannot be created through the REST face (the slot
		// set is closed there); this is the hand-written-row guard.
		return fmt.Errorf("maintenance: no carrier for slot %q (closed set: gc, cleanup-unused-cache, cleanup-virtual)", key)
	}
}

// backupRunner is the backup domain's carrier: one Run exports the instance
// into the payload's directory under a timestamped sub-artifact — the SAME
// exportSnapshot kernel the CLI subcommand rides.
type backupRunner struct {
	cfg *config.Config
	md  metadata.Store
	log *slog.Logger
}

// Run implements the scheduler's Runner contract.
func (b *backupRunner) Run(ctx context.Context, key string) error {
	bk, err := b.md.Backups().Get(ctx, key)
	if errors.Is(err, metadata.ErrBackupNotFound) {
		// The payload row is gone (a raced delete whose ledger teardown
		// failed): tombstone the orphan schedule row and succeed — there is
		// nothing to back up.
		if derr := b.md.Schedules().Delete(ctx, scheduler.DomainBackup, key); derr != nil && !errors.Is(derr, metadata.ErrScheduleNotFound) {
			b.log.WarnContext(ctx, "backup schedule row outlived its payload (tombstone failed)",
				"key", key, "error", derr.Error())
			return nil
		}
		b.log.InfoContext(ctx, "backup schedule row outlived its payload (tombstoned)", "key", key)
		return nil
	}
	if err != nil {
		return fmt.Errorf("scheduled backup %s: reading payload: %w", key, err)
	}
	if !bk.Enabled {
		// A disabled payload never exports; the ledger row's enabled bit
		// mirrors it, so this is a raced flip — an honest no-op.
		b.log.InfoContext(ctx, "scheduled backup skipped: disabled", "key", key)
		return nil
	}
	// Sub-second suffix: two fires inside one clock second must never mix
	// artifacts (the kernel refuses a non-empty output directory).
	stampTime := time.Now().UTC()
	stamp := stampTime.Format("20060102T150405") + fmt.Sprintf("-%09dZ", stampTime.Nanosecond())
	out := filepath.Join(bk.ExportDir, key+"-"+stamp)
	// The export holds the data-directory maintenance lock; it must run to
	// its own conclusion even when the scheduler's context winds down (the
	// gc/cleanup carriers' WithoutCancel posture — a half-written backup
	// directory is exactly the artifact the kernel's cleanup defer exists
	// to prevent, and canceling mid-copy would rely on it).
	summary, err := exportSnapshot(context.WithoutCancel(ctx), b.cfg, b.log, out)
	if err != nil {
		return fmt.Errorf("scheduled backup %s: %w", key, err)
	}
	b.log.InfoContext(ctx, "scheduled backup complete",
		"key", key, "output", summary.Output,
		"blobs", summary.BlobCount, "bytes", summary.TotalBytes,
		"duration", summary.Duration.String())
	return nil
}

// startScheduler assembles the engine and its three domain registrations
// and launches the Run loop on the server's signal context (the
// licenseMgr/cleanupEng lifecycle family: no drain semantics — an in-flight
// carrier runs on its own detached context, next_run is already on disk and
// the next boot's collapse re-fires an interrupted window once).
func startScheduler(ctx context.Context, logger *slog.Logger, cfg *config.Config, stack *stack, srv *httpapi.Server) (*scheduler.Scheduler, error) {
	eng, err := scheduler.New(scheduler.Options{
		Store:    stack.md.Schedules(),
		Logger:   logger,
		Registry: stack.metricRegistry(),
		Audit:    stack.auditLog,
	})
	if err != nil {
		return nil, fmt.Errorf("scheduler: %w", err)
	}
	if err := eng.Register(scheduler.DomainMaintenance, &maintenanceRunner{
		srv: srv, cleanup: stack.cleanupEng, log: logger,
	}); err != nil {
		return nil, fmt.Errorf("scheduler: maintenance domain: %w", err)
	}
	if err := eng.Register(scheduler.DomainBackup, &backupRunner{
		cfg: cfg, md: stack.md, log: logger,
	}); err != nil {
		return nil, fmt.Errorf("scheduler: backup domain: %w", err)
	}
	if err := eng.Register(scheduler.DomainReplication,
		replication.NewScheduleRunner(stack.replStore, stack.md.Schedules(), stack.replEngine)); err != nil {
		return nil, fmt.Errorf("scheduler: replication domain: %w", err)
	}
	// Run returns nil when ctx ends (the engine's contract); the discard
	// is explicit for errcheck the way every lifecycle engine's exit is.
	go func() { _ = eng.Run(ctx) }()
	return eng, nil
}
