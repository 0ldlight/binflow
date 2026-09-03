package metadata_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The schedules ledger's five faces (021, M16 T-446 / ADR-0044 decision 2):
// upsert semantics, the not-found sentinel, the list projections and the
// due predicate the scheduler's tick rides on — plus the 021 CHECKs.

func putScheduleRow(t *testing.T, st metadata.Store, sc *metadata.Schedule) {
	t.Helper()
	if err := st.Schedules().Put(context.Background(), sc); err != nil {
		t.Fatalf("schedules put %s/%s: %v", sc.Domain, sc.Key, err)
	}
}

func TestSchedulesStorePutGetUpserts(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 3, 7, 0, 0, 0, time.UTC).Format(time.RFC3339)

	putScheduleRow(t, st, &metadata.Schedule{
		Domain: "backup", Key: "daily",
		CronExpr: "0 0 2 ? * MON-FRI", Enabled: true, NextRunAt: "2026-09-04T02:00:00Z",
		CreatedAt: now, CreatedBy: "admin", UpdatedAt: now, UpdatedBy: "admin",
	})
	got, err := st.Schedules().Get(ctx, "backup", "daily")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CronExpr != "0 0 2 ? * MON-FRI" || !got.Enabled || got.NextRunAt != "2026-09-04T02:00:00Z" {
		t.Errorf("get = %+v, want the stored row", got)
	}

	// Upsert replaces the mutable columns but keeps the creation stamp.
	putScheduleRow(t, st, &metadata.Schedule{
		Domain: "backup", Key: "daily",
		CronExpr: "0 0 3 ? * SAT", Enabled: false, NextRunAt: "",
		LastRunAt: "2026-09-05T03:00:00Z", LastStatus: "ok",
		CreatedAt: "1999-01-01T00:00:00Z", CreatedBy: "nobody", // must NOT overwrite
		UpdatedAt: now, UpdatedBy: "scheduler",
	})
	got, err = st.Schedules().Get(ctx, "backup", "daily")
	if err != nil {
		t.Fatalf("get after upsert: %v", err)
	}
	if got.CronExpr != "0 0 3 ? * SAT" || got.Enabled || got.LastStatus != "ok" {
		t.Errorf("upserted row = %+v, want the replacement columns", got)
	}
	if got.CreatedAt != now || got.CreatedBy != "admin" {
		t.Errorf("created_at/by = %q/%q, want the ORIGINAL creation stamp kept", got.CreatedAt, got.CreatedBy)
	}

	if _, err := st.Schedules().Get(ctx, "backup", "nope"); !errors.Is(err, metadata.ErrScheduleNotFound) {
		t.Errorf("get(missing) = %v, want ErrScheduleNotFound", err)
	}
}

func TestSchedulesStoreDelete(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 3, 7, 0, 0, 0, time.UTC).Format(time.RFC3339)
	putScheduleRow(t, st, &metadata.Schedule{
		Domain: "replication", Key: "cfg-1", CronExpr: "0 * * ? * *",
		CreatedAt: now, CreatedBy: "admin", UpdatedAt: now, UpdatedBy: "admin",
	})
	if err := st.Schedules().Delete(ctx, "replication", "cfg-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// "No row = not scheduled": deleting again is the not-found sentinel
	// (the config surface's clear-cronExp path only deletes what is there).
	if err := st.Schedules().Delete(ctx, "replication", "cfg-1"); !errors.Is(err, metadata.ErrScheduleNotFound) {
		t.Errorf("delete(missing) = %v, want ErrScheduleNotFound", err)
	}
}

func TestSchedulesStoreListProjections(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 3, 7, 0, 0, 0, time.UTC).Format(time.RFC3339)
	rows := []*metadata.Schedule{
		{Domain: "replication", Key: "cfg-2", CronExpr: "0 * * ? * *",
			CreatedAt: now, CreatedBy: "a", UpdatedAt: now, UpdatedBy: "a"},
		{Domain: "backup", Key: "weekly", CronExpr: "0 0 2 ? * SAT",
			CreatedAt: now, CreatedBy: "a", UpdatedAt: now, UpdatedBy: "a"},
		{Domain: "maintenance", Key: "gc", CronExpr: "0 0 /4 * * ?",
			CreatedAt: now, CreatedBy: "a", UpdatedAt: now, UpdatedBy: "a"},
	}
	for _, r := range rows {
		putScheduleRow(t, st, r)
	}
	all, err := st.Schedules().List(ctx, "")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("list all = %d rows, want 3", len(all))
	}
	// Ordered by (domain, key): backup, maintenance, replication.
	if all[0].Domain != "backup" || all[1].Domain != "maintenance" || all[2].Domain != "replication" {
		t.Errorf("list all order = %s,%s,%s, want domain-ordered", all[0].Domain, all[1].Domain, all[2].Domain)
	}
	one, err := st.Schedules().List(ctx, "backup")
	if err != nil || len(one) != 1 || one[0].Key != "weekly" {
		t.Errorf("list(backup) = %v (%v), want the single weekly row", one, err)
	}
}

func TestSchedulesStoreListDuePredicate(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 3, 7, 0, 0, 0, time.UTC)
	stamp := now.Format(time.RFC3339)
	rows := []*metadata.Schedule{
		// due: enabled, next_run in the past.
		{Domain: "backup", Key: "due", Enabled: true, NextRunAt: "2026-09-03T06:00:00Z",
			CronExpr: "0 * * ? * *", CreatedAt: stamp, CreatedBy: "a", UpdatedAt: stamp, UpdatedBy: "a"},
		// not due: the future.
		{Domain: "backup", Key: "future", Enabled: true, NextRunAt: "2026-09-04T02:00:00Z",
			CronExpr: "0 0 2 ? * MON-FRI", CreatedAt: stamp, CreatedBy: "a", UpdatedAt: stamp, UpdatedBy: "a"},
		// not due: disabled rows carry '' — which sorts FIRST and must
		// never match the <= now comparison.
		{Domain: "backup", Key: "disabled", Enabled: false, NextRunAt: "",
			CronExpr: "0 * * ? * *", CreatedAt: stamp, CreatedBy: "a", UpdatedAt: stamp, UpdatedBy: "a"},
		// due: the boundary is inclusive — next_run exactly at now fires.
		{Domain: "replication", Key: "edge", Enabled: true, NextRunAt: stamp,
			CronExpr: "0 * * ? * *", CreatedAt: stamp, CreatedBy: "a", UpdatedAt: stamp, UpdatedBy: "a"},
	}
	for _, r := range rows {
		putScheduleRow(t, st, r)
	}
	due, err := st.Schedules().ListDue(ctx, stamp)
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	if len(due) != 2 {
		t.Fatalf("list due = %d rows, want 2 (past + the inclusive now edge)", len(due))
	}
	if due[0].Key != "due" || due[1].Key != "edge" {
		t.Errorf("due rows = %s,%s, want due,edge in next_run order", due[0].Key, due[1].Key)
	}
}

func TestSchedulesStoreDomainCheckConstraint(t *testing.T) {
	st := open(t)
	now := time.Date(2026, 9, 3, 7, 0, 0, 0, time.UTC).Format(time.RFC3339)
	// The 021 CHECK is the closed-set guard at the storage edge: a domain
	// outside maintenance|backup|replication never lands.
	err := st.Schedules().Put(context.Background(), &metadata.Schedule{
		Domain: "observability", Key: "x", CronExpr: "0 * * ? * *",
		CreatedAt: now, CreatedBy: "a", UpdatedAt: now, UpdatedBy: "a",
	})
	if err == nil {
		t.Fatal("put(unknown domain) = nil error, want the CHECK constraint refusal")
	}
	// Same for the last_status closed set.
	err = st.Schedules().Put(context.Background(), &metadata.Schedule{
		Domain: "backup", Key: "x", CronExpr: "0 * * ? * *", LastStatus: "exploded",
		CreatedAt: now, CreatedBy: "a", UpdatedAt: now, UpdatedBy: "a",
	})
	if err == nil {
		t.Fatal("put(bad last_status) = nil error, want the CHECK constraint refusal")
	}
}
