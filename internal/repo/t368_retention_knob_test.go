package repo_test

// T-368 (FR-118.2): trashcan.retention_days as a knob — the retention
// cron consumes the configured window instead of the fixed spec 14. The
// M12 base (TestTrashRetentionWindow) pinned the 13/15-day arms of the
// default window; this leg pins the SHORT window the config key makes
// expressible (retention_days=1: inside the day nothing goes, past it
// the purge runs with its audit row) plus the zero-value sentinel arm
// (0 maps onto the spec 14, the loader's documented YAML-0 semantics).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// t368RetentionRunOnce runs one pass with the knob-spelled window and
// fails on anything but success.
func t368RetentionRunOnce(t *testing.T, e *env, days int) *repo.TrashRetentionReport {
	t.Helper()
	eng, err := repo.NewTrashEngine(repo.TrashEngineOptions{
		Store: e.md, Audit: e.au, RetentionDays: days, Now: e.clk.Now,
	})
	if err != nil {
		t.Fatalf("NewTrashEngine(%dd): %v", days, err)
	}
	rep, err := eng.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce(%dd): %v", days, err)
	}
	return rep
}

// TestT368RetentionDaysShortWindow: the retention_days=1 clock fixture —
// the purge follows the configured window, not the spec 14: a 23-hour
// entry survives, a 25-hour entry goes, and the pass records its
// trash.retention audit row under the scheduled actor.
func TestT368RetentionDaysShortWindow(t *testing.T) {
	e := newEnv(t)
	trashEnable(t, e, 1)
	mustCreateRepo(t, e, "libs")
	put(t, e, admin(), "libs", "old/a.bin", "aaa")
	put(t, e, admin(), "libs", "new/b.bin", "bbb")
	// old/ is captured at t0; new/ two hours later, so at every pass
	// below the two sit on opposite sides of the one-day window.
	if err := e.svc.Delete(context.Background(), admin(), "libs", "old/"); err != nil {
		t.Fatalf("Delete old: %v", err)
	}
	e.clk.Advance(2 * time.Hour)
	if err := e.svc.Delete(context.Background(), admin(), "libs", "new/"); err != nil {
		t.Fatalf("Delete new: %v", err)
	}
	e.clk.Advance(23 * time.Hour) // now = t0+25h: old/ is 25h, new/ is 23h

	// First pass: exactly the over-window entry goes; the 23-hour one
	// stays — the boundary is the CONFIGURED day, not the spec fortnight
	// (under the default 14 both would survive).
	rep := t368RetentionRunOnce(t, e, 1)
	if rep.Files != 1 {
		t.Fatalf("1-day pass = %+v, want exactly the 25h-old entry purged", rep)
	}
	if _, err := e.md.Nodes().Get(context.Background(), repo.TrashRepoKey, "libs/old/a.bin"); !errors.Is(err, metadata.ErrNodeNotFound) {
		t.Fatalf("expired 25h entry survived: %v", err)
	}
	if _, err := e.md.Nodes().Get(context.Background(), repo.TrashRepoKey, "libs/new/b.bin"); err != nil {
		t.Fatalf("23h entry purged inside the 1-day window: %v", err)
	}

	// Two more hours: the second entry crosses and goes too.
	e.clk.Advance(2 * time.Hour) // new/ is now 25h
	rep = t368RetentionRunOnce(t, e, 1)
	if rep.Files != 1 {
		t.Fatalf("second pass = %+v, want the remaining entry purged", rep)
	}
	if _, err := e.md.Nodes().Get(context.Background(), repo.TrashRepoKey, "libs/new/b.bin"); !errors.Is(err, metadata.ErrNodeNotFound) {
		t.Fatalf("second entry survived its window: %v", err)
	}

	// The scheduled pass audited itself (the M12 cron posture, unchanged
	// by the knob).
	var audited bool
	for _, ev := range trashAuditEvents(e.au) {
		if ev.Action == repo.AuditActionTrashRetention && ev.Actor == repo.ActorTrashRetention {
			audited = true
		}
	}
	if !audited {
		t.Fatalf("no trash.retention audit row for the knob-driven pass")
	}
}

// TestT368RetentionDaysZeroSentinel: retention_days=0 (the loader's
// documented YAML-0 spelling of "default") maps onto the spec 14 at the
// engine — a 2-day-old entry survives, the default-14 regression shape.
func TestT368RetentionDaysZeroSentinel(t *testing.T) {
	e := newEnv(t)
	trashEnable(t, e, 0)
	mustCreateRepo(t, e, "libs")
	put(t, e, admin(), "libs", "x/a.bin", "aaa")
	if err := e.svc.Delete(context.Background(), admin(), "libs", "x/"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	e.clk.Advance(2 * 24 * time.Hour)
	rep := t368RetentionRunOnce(t, e, 0)
	if rep.Files != 0 {
		t.Fatalf("zero-sentinel pass purged a 2-day entry (want the 14-day window): %+v", rep)
	}
	// And the capture itself rode the same fallback (trashEnable's <= 0
	// arm): the node is in the can.
	if _, err := e.md.Nodes().Get(context.Background(), repo.TrashRepoKey, "libs/x/a.bin"); err != nil {
		t.Fatalf("captured entry missing: %v", err)
	}
}
