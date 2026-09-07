package main

// The cron scheduler's cmd-side wiring proofs (M16 T-450, ADR-0044
// decisions 6 and 8): the maintenance runner's slot dispatch over the REAL
// assembled stack (the REST gc kernel + CleanupEngine), the backup runner
// driving the shared export kernel into timestamped artifacts, the
// tombstone/park guards, and the startScheduler assembly firing one landed
// schedule end to end (Kick → carrier → engine run-state write-back →
// schedule.run audit word).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/scheduler"
	"github.com/lzwzzy/binflow/internal/storage"
)

// TestSchedulerMaintenanceRunnerDispatch pins the maintenance carrier's
// slot law over the real stack: gc rides the REST gc kernel, both cleanup
// slots ride CleanupEngine.RunOnce (the ADR decision-8① two-family full
// pass), the T-495 three ride their carrier kernels (the quota threshold
// check, the metadata VACUUM rebuild, the prune dry-run), an unknown slot
// is the closed-set error naming all six.
func TestSchedulerMaintenanceRunnerDispatch(t *testing.T) {
	cfg := configDefaults()
	cfg.Storage.DataDir = t.TempDir()
	logger, _ := captureLogger(t)
	st, err := openStack(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	defer st.close(logger)

	ctx := context.Background()
	if _, err := st.svc.CreateRepo(ctx, &repo.Principal{Name: "admin", Admin: true}, &metadata.Repo{
		RepoKey: "sched-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	m := &maintenanceRunner{srv: httpapi.New(httpapi.Deps{
		Config: cfg, Auth: st.authSvc, Authz: st.authSvc, Metadata: st.md,
		Repos: st.md.Repos(), ReposSvc: st.svc, Passwords: st.authSvc, Tokens: st.authSvc,
		GC: st.st, DataDir: cfg.Storage.DataDir,
	}, logger), cleanup: st.cleanupEng, log: logger}
	for _, slot := range []string{
		"gc", "cleanup-unused-cache", "cleanup-virtual",
		"quota", "compress", "prune",
	} {
		if err := m.Run(ctx, slot); err != nil {
			t.Errorf("Run(%s) = %v, want nil (the slot's carrier fired)", slot, err)
		}
	}
	err = m.Run(ctx, "not-a-slot")
	if err == nil || !strings.Contains(err.Error(), "no carrier for slot") {
		t.Errorf("Run(unknown) = %v, want the closed-set error", err)
	}
	if err == nil || !strings.Contains(err.Error(), "quota, compress, prune") {
		t.Errorf("closed-set error must name the T-495 three: %v", err)
	}
}

// TestSchedulerBackupRunnerExports pins the backup domain's carrier: the
// 022 payload drives the shared export kernel into one timestamped
// artifact per fire under the configured directory, a vanished payload
// tombstones its schedule row, and a disabled payload never exports.
func TestSchedulerBackupRunnerExports(t *testing.T) {
	cfg := configDefaults()
	dataDir := t.TempDir()
	cfg.Storage.DataDir = dataDir
	logger, _ := captureLogger(t)
	st, err := openStack(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	defer st.close(logger)

	ctx := context.Background()
	admin := &repo.Principal{Name: "admin", Admin: true}
	if _, err := st.svc.CreateRepo(ctx, admin, &metadata.Repo{
		RepoKey: "bk-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if _, err := st.svc.Put(ctx, admin, "bk-local", "org/app.bin",
		strings.NewReader("schedule-wiring-artifact"), storage.BlobRef{}, "application/octet-stream"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	exportRoot := t.TempDir()
	now := metadata.Now()
	if err := st.md.Backups().Put(ctx, &metadata.Backup{
		Key: "nightly", Enabled: true, ExportDir: exportRoot,
		CreatedAt: now, CreatedBy: "admin", UpdatedAt: now, UpdatedBy: "admin",
	}); err != nil {
		t.Fatalf("backups put: %v", err)
	}
	if err := st.md.Schedules().Put(ctx, &metadata.Schedule{
		Domain: scheduler.DomainBackup, Key: "nightly", CronExpr: "0 0 2 ? * MON-FRI",
		Enabled: true, NextRunAt: "2099-01-01T00:00:00Z",
		CreatedAt: now, CreatedBy: "admin", UpdatedAt: now, UpdatedBy: "admin",
	}); err != nil {
		t.Fatalf("schedules put: %v", err)
	}

	b := &backupRunner{cfg: cfg, md: st.md, log: logger}
	if err := b.Run(ctx, "nightly"); err != nil {
		t.Fatalf("backup Run: %v", err)
	}
	entries, err := os.ReadDir(exportRoot)
	if err != nil || len(entries) != 1 {
		t.Fatalf("export root after one fire = %v (%v), want one timestamped artifact", entries, err)
	}
	artifact := filepath.Join(exportRoot, entries[0].Name())
	if !strings.HasPrefix(entries[0].Name(), "nightly-") {
		t.Errorf("artifact dir = %s, want the nightly-<stamp> shape", entries[0].Name())
	}
	for _, name := range []string{"manifest.json", "metadata.db", "blobs"} {
		if _, err := os.Stat(filepath.Join(artifact, name)); err != nil {
			t.Errorf("artifact %s missing: %v", name, err)
		}
	}

	// A second fire lands a SECOND timestamped artifact (never mixes) —
	// the sub-second suffix keeps two same-second fires distinct.
	if err := b.Run(ctx, "nightly"); err != nil {
		t.Fatalf("second backup Run: %v", err)
	}
	if entries, _ = os.ReadDir(exportRoot); len(entries) != 2 {
		t.Errorf("export root after two fires = %d entries, want 2", len(entries))
	}

	// A disabled payload is an honest no-op (no third artifact).
	if err := st.md.Backups().Put(ctx, &metadata.Backup{
		Key: "nightly", Enabled: false, ExportDir: exportRoot,
		CreatedAt: now, CreatedBy: "admin", UpdatedAt: now, UpdatedBy: "admin",
	}); err != nil {
		t.Fatalf("backups put disabled: %v", err)
	}
	if err := b.Run(ctx, "nightly"); err != nil {
		t.Fatalf("disabled backup Run: %v", err)
	}
	if entries, _ = os.ReadDir(exportRoot); len(entries) != 2 {
		t.Errorf("export root after the disabled fire = %d entries, want 2", len(entries))
	}

	// A vanished payload tombstones the orphan schedule row.
	if err := st.md.Backups().Delete(ctx, "nightly"); err != nil {
		t.Fatalf("backups delete: %v", err)
	}
	if err := b.Run(ctx, "nightly"); err != nil {
		t.Fatalf("orphan backup Run: %v", err)
	}
	if row, gerr := st.md.Schedules().Get(ctx, scheduler.DomainBackup, "nightly"); gerr == nil {
		t.Errorf("orphan schedule row survived the tombstone: %+v", row)
	}
}

// TestSchedulerGapCarriersFireOnSchedule pins the T-495 three's FIRE plane
// (FR-158's 到点断言): landed schedule rows under the maintenance domain
// fire through the engine's Kick seam (the same dispatch path a due tick
// uses), and each fire lands its layered trail — the engine's
// maintenance.schedule.run row, the carrier's own word
// (maintenance.quota.check / maintenance.compress.run /
// maintenance.prune.run), the quota WARN line, and the prune slot's
// DRY-RUN law: the unreferenced blob is REPORTED (a gc.run row with
// apply:false and a candidate in the prune detail) but the file SURVIVES
// (the gc slot stays the deletion surface).
func TestSchedulerGapCarriersFireOnSchedule(t *testing.T) {
	cfg := configDefaults()
	cfg.Storage.DataDir = t.TempDir()
	// A ~zero grace so the seeded orphan is prunable-candidate NOW (the
	// scheduled passes take the standing policy — this makes the policy
	// permissive for the test's clock).
	cfg.Storage.GCGrace = time.Nanosecond
	logger, logText := captureLogger(t)
	st, err := openStack(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	defer st.close(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httpapi.New(httpapi.Deps{
		Config: cfg, Auth: st.authSvc, Authz: st.authSvc, Metadata: st.md,
		Repos: st.md.Repos(), ReposSvc: st.svc, Passwords: st.authSvc, Tokens: st.authSvc,
		GC: st.st, DataDir: cfg.Storage.DataDir,
	}, logger)
	eng, err := startScheduler(ctx, logger, cfg, st, srv)
	if err != nil {
		t.Fatalf("startScheduler: %v", err)
	}

	// One local repository with an 18-byte artifact, then a 10-byte quota
	// tightened OVER it — the quota carrier's threshold fixture (content
	// first, ceiling second: the upload gate would refuse an 18-byte Put
	// into a 10-byte repo, and a tightened quota is exactly the violation
	// the scheduled check exists to surface).
	admin := &repo.Principal{Name: "admin", Admin: true}
	if _, err := st.svc.CreateRepo(ctx, admin, &metadata.Repo{
		RepoKey: "gap-quota", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if _, err := st.svc.Put(ctx, admin, "gap-quota", "big.bin",
		strings.NewReader("0123456789ABCDEFGH"), storage.BlobRef{}, "application/octet-stream"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := st.svc.UpdateRepo(ctx, admin, &metadata.Repo{
		RepoKey: "gap-quota", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: `{"quotaBytes":10}`,
	}); err != nil {
		t.Fatalf("UpdateRepo (quota tighten): %v", err)
	}

	// One unreferenced blob file under blobs/ — the prune carrier's
	// candidate fixture (no node row points at it, the ~zero grace makes
	// it collectable).
	orphan := sha256Hex("prune-orphan-body")
	orphanPath, perr := storage.BlobPath(cfg.Storage.DataDir, orphan)
	if perr != nil {
		t.Fatalf("BlobPath: %v", perr)
	}
	if err := os.MkdirAll(filepath.Dir(orphanPath), 0o700); err != nil {
		t.Fatalf("orphan dir: %v", err)
	}
	if err := os.WriteFile(orphanPath, []byte("prune-orphan-body"), 0o600); err != nil {
		t.Fatalf("orphan file: %v", err)
	}

	// Land the three schedule rows and fire each through the Kick seam.
	now := metadata.Now()
	for _, slot := range []string{"quota", "compress", "prune"} {
		if err := st.md.Schedules().Put(ctx, &metadata.Schedule{
			Domain: scheduler.DomainMaintenance, Key: slot, CronExpr: "0 0 /4 * * ?",
			Enabled: true, NextRunAt: "2099-01-01T00:00:00Z",
			CreatedAt: now, CreatedBy: "admin", UpdatedAt: now, UpdatedBy: "admin",
		}); err != nil {
			t.Fatalf("schedules put %s: %v", slot, err)
		}
		if err := eng.Kick(ctx, scheduler.DomainMaintenance, slot); err != nil {
			t.Fatalf("Kick %s: %v", slot, err)
		}
	}

	// Poll until all three rows carry the fired/ok state.
	deadline := time.Now().Add(60 * time.Second)
	for {
		done := true
		for _, slot := range []string{"quota", "compress", "prune"} {
			row, gerr := st.md.Schedules().Get(ctx, scheduler.DomainMaintenance, slot)
			if gerr != nil || row.LastRunAt == "" || row.LastStatus != "ok" {
				done = false
				break
			}
		}
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("gap schedule rows did not reach fired/ok before the deadline")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The layered trail: the engine's schedule.run rows (3) plus each
	// carrier's own word (one row each).
	waitForAudit := func(action string, want int, check func(string) bool) {
		t.Helper()
		dl := time.Now().Add(60 * time.Second)
		for {
			events, qerr := st.md.Audits().Query(ctx, metadata.AuditQuery{Action: action, Limit: 10})
			if qerr == nil && len(events) == want && (check == nil || check(events[0].Detail)) {
				return
			}
			if time.Now().After(dl) {
				t.Fatalf("%s rows = %v (%v), want %d", action, events, qerr, want)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitForAudit("maintenance.schedule.run", 3, nil)
	waitForAudit("maintenance.quota.check", 1, func(d string) bool {
		return strings.Contains(d, `"repo":"gap-quota"`) && strings.Contains(d, `"overQuota":1`)
	})
	waitForAudit("maintenance.compress.run", 1, nil)
	waitForAudit("maintenance.prune.run", 1, func(d string) bool {
		return strings.Contains(d, `"candidateCount":1`) && strings.Contains(d, `"applied":false`)
	})

	// The quota WARN line named the over-quota repository.
	if out := logText(); !strings.Contains(out, "repository over its quota") || !strings.Contains(out, "gap-quota") {
		t.Errorf("captured log lacks the quota WARN line for gap-quota:\n%s", out)
	}

	// The prune fire's own sweep trail: a gc.run row in its dry-run shape.
	gcEvents, qerr := st.md.Audits().Query(ctx, metadata.AuditQuery{Action: "gc.run", Limit: 10})
	if qerr != nil || len(gcEvents) != 1 || !strings.Contains(gcEvents[0].Detail, `"apply":false`) {
		t.Fatalf("gc.run rows = %v (%v), want one apply:false row from the prune fire", gcEvents, qerr)
	}

	// THE DRY-RUN LAW: the orphan blob file SURVIVED the prune fire — the
	// prune slot reports, it never deletes (the gc slot owns deletion).
	if _, serr := os.Stat(orphanPath); serr != nil {
		t.Fatalf("prune fire deleted the orphan %s — the carrier must be dry-run: %v", orphanPath, serr)
	}
}

// sha256Hex is the orphan fixture's digest helper.
func sha256Hex(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// TestStartSchedulerFiresLandedSchedule pins the ASSEMBLY: startScheduler
// over the real stack registers the three domains and its loop drives one
// landed maintenance schedule end to end (Kick — the same dispatch path a
// due tick uses — fires the gc carrier, the engine lands the run state and
// the schedule.run audit word).
func TestStartSchedulerFiresLandedSchedule(t *testing.T) {
	cfg := configDefaults()
	cfg.Storage.DataDir = t.TempDir()
	logger, _ := captureLogger(t)
	st, err := openStack(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	defer st.close(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The maintenance runner needs only the gc kernel's server — the same
	// minimal Deps shape (newAssembledServer is the package's ONE-per-
	// process full assembly; the registry is process-global, T-168).
	srv := httpapi.New(httpapi.Deps{
		Config: cfg, Auth: st.authSvc, Authz: st.authSvc, Metadata: st.md,
		Repos: st.md.Repos(), ReposSvc: st.svc, Passwords: st.authSvc, Tokens: st.authSvc,
		GC: st.st, DataDir: cfg.Storage.DataDir,
	}, logger)
	eng, err := startScheduler(ctx, logger, cfg, st, srv)
	if err != nil {
		t.Fatalf("startScheduler: %v", err)
	}

	now := metadata.Now()
	if err := st.md.Schedules().Put(ctx, &metadata.Schedule{
		Domain: scheduler.DomainMaintenance, Key: "gc", CronExpr: "0 0 /4 * * ?",
		Enabled: true, NextRunAt: "2099-01-01T00:00:00Z",
		CreatedAt: now, CreatedBy: "admin", UpdatedAt: now, UpdatedBy: "admin",
	}); err != nil {
		t.Fatalf("schedules put: %v", err)
	}
	if err := eng.Kick(ctx, scheduler.DomainMaintenance, "gc"); err != nil {
		t.Fatalf("Kick: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		row, gerr := st.md.Schedules().Get(ctx, scheduler.DomainMaintenance, "gc")
		if gerr == nil && row.LastRunAt != "" && row.LastStatus == "ok" && row.NextRunAt != "2099-01-01T00:00:00Z" {
			break // fired, recorded, re-armed from the expression
		}
		if time.Now().After(deadline) {
			t.Fatalf("schedule row after Kick = %+v (%v), want fired+re-armed", row, gerr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The audit word lands AFTER the ledger write-back (runOne's order), so
	// it polls too — under the full-suite's parallel -race load the gap
	// between the two writes exceeds any immediate check.
	deadline = time.Now().Add(30 * time.Second)
	var events []*metadata.AuditEvent
	for {
		var qerr error
		events, qerr = st.md.Audits().Query(ctx, metadata.AuditQuery{Action: "maintenance.schedule.run", Limit: 10})
		if qerr == nil && len(events) == 1 && events[0].Actor == "scheduler" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("maintenance.schedule.run rows = %v (%v), want one scheduler-actor row", events, qerr)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// THE FAIL ARM of the audit triple (调度/执行/失败): a hand-written row
	// for a slot no carrier exists for fires, fails, and lands the
	// maintenance.schedule.fail word with the truncated reason in the
	// ledger's last_error.
	if err := st.md.Schedules().Put(ctx, &metadata.Schedule{
		Domain: scheduler.DomainMaintenance, Key: "bogus-slot", CronExpr: "0 0 /4 * * ?",
		Enabled: true, NextRunAt: "2099-01-01T00:00:00Z",
		CreatedAt: now, CreatedBy: "admin", UpdatedAt: now, UpdatedBy: "admin",
	}); err != nil {
		t.Fatalf("schedules put bogus: %v", err)
	}
	if err := eng.Kick(ctx, scheduler.DomainMaintenance, "bogus-slot"); err != nil {
		t.Fatalf("Kick bogus: %v", err)
	}
	deadline = time.Now().Add(30 * time.Second)
	for {
		row, gerr := st.md.Schedules().Get(ctx, scheduler.DomainMaintenance, "bogus-slot")
		if gerr == nil && row.LastStatus == "failed" && strings.Contains(row.LastError, "no carrier for slot") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("bogus schedule row = %+v (%v), want failed + the closed-set reason", row, gerr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	deadline = time.Now().Add(30 * time.Second)
	var failEvents []*metadata.AuditEvent
	for {
		var qerr error
		failEvents, qerr = st.md.Audits().Query(ctx, metadata.AuditQuery{Action: "maintenance.schedule.fail", Limit: 10})
		if qerr == nil && len(failEvents) == 1 && failEvents[0].Actor == "scheduler" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("maintenance.schedule.fail rows = %v (%v), want one scheduler-actor row", failEvents, qerr)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The BACKUP domain through the same engine (the audit triple's run arm
	// on the second domain): a payload + schedule row fire one export and
	// land backup.schedule.run.
	exportRoot := t.TempDir()
	if err := st.md.Backups().Put(ctx, &metadata.Backup{
		Key: "wiring-nightly", Enabled: true, ExportDir: exportRoot,
		CreatedAt: now, CreatedBy: "admin", UpdatedAt: now, UpdatedBy: "admin",
	}); err != nil {
		t.Fatalf("backups put: %v", err)
	}
	if err := st.md.Schedules().Put(ctx, &metadata.Schedule{
		Domain: scheduler.DomainBackup, Key: "wiring-nightly", CronExpr: "0 0 2 ? * MON-FRI",
		Enabled: true, NextRunAt: "2099-01-01T00:00:00Z",
		CreatedAt: now, CreatedBy: "admin", UpdatedAt: now, UpdatedBy: "admin",
	}); err != nil {
		t.Fatalf("schedules put backup: %v", err)
	}
	if err := eng.Kick(ctx, scheduler.DomainBackup, "wiring-nightly"); err != nil {
		t.Fatalf("Kick backup: %v", err)
	}
	// Poll the run word (written after the export completes — the artifact
	// directory exists from the kernel's first step, so it is no completion
	// signal), then assert the artifact shape.
	deadline = time.Now().Add(60 * time.Second)
	var bkEvents []*metadata.AuditEvent
	for {
		var qerr error
		bkEvents, qerr = st.md.Audits().Query(ctx, metadata.AuditQuery{Action: "backup.schedule.run", Limit: 10})
		if qerr == nil && len(bkEvents) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("backup.schedule.run rows = %v (%v), want one row", bkEvents, qerr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if bkEvents[0].Actor != "scheduler" {
		t.Fatalf("backup.schedule.run actor = %q, want scheduler", bkEvents[0].Actor)
	}
	entries, rerr := os.ReadDir(exportRoot)
	if rerr != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "wiring-nightly-") {
		t.Fatalf("export root after the backup fire = %v (%v), want one timestamped artifact", entries, rerr)
	}
	if _, serr := os.Stat(filepath.Join(exportRoot, entries[0].Name(), "manifest.json")); serr != nil {
		t.Fatalf("backup artifact manifest missing: %v", serr)
	}
}
