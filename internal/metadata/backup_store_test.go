package metadata_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The backup payload store's four faces (022, M16 T-450 / ADR-0044
// decisions 2 and 5): upsert semantics, the not-found sentinel, the list
// projection — the cron half of the entity lives in the schedules ledger,
// this store carries only what the export carrier reads at fire time.

func putBackupRow(t *testing.T, st metadata.Store, b *metadata.Backup) {
	t.Helper()
	if err := st.Backups().Put(context.Background(), b); err != nil {
		t.Fatalf("backups put %s: %v", b.Key, err)
	}
}

func TestBackupStorePutGetUpserts(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	putBackupRow(t, st, &metadata.Backup{
		Key: "nightly", Enabled: true, ExportDir: "/var/backups/binflow",
		CreatedAt: "2026-09-03T07:00:00Z", CreatedBy: "admin",
		UpdatedAt: "2026-09-03T07:00:00Z", UpdatedBy: "admin",
	})
	got, err := st.Backups().Get(ctx, "nightly")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Enabled || got.ExportDir != "/var/backups/binflow" {
		t.Errorf("get = %+v, want the stored row", got)
	}

	// Upsert replaces the mutable columns but keeps the creation stamp.
	putBackupRow(t, st, &metadata.Backup{
		Key: "nightly", Enabled: false, ExportDir: "/srv/backup",
		CreatedAt: "1999-01-01T00:00:00Z", CreatedBy: "nobody", // must NOT overwrite
		UpdatedAt: "2026-09-04T07:00:00Z", UpdatedBy: "admin",
	})
	got, err = st.Backups().Get(ctx, "nightly")
	if err != nil {
		t.Fatalf("get after upsert: %v", err)
	}
	if got.Enabled || got.ExportDir != "/srv/backup" {
		t.Errorf("upserted row = %+v, want the replacement columns", got)
	}
	if got.CreatedAt != "2026-09-03T07:00:00Z" || got.CreatedBy != "admin" {
		t.Errorf("created_at/by = %q/%q, want the ORIGINAL creation stamp kept", got.CreatedAt, got.CreatedBy)
	}

	if _, err := st.Backups().Get(ctx, "nope"); !errors.Is(err, metadata.ErrBackupNotFound) {
		t.Errorf("get(missing) = %v, want ErrBackupNotFound", err)
	}
}

func TestBackupStoreDeleteAndList(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putBackupRow(t, st, &metadata.Backup{Key: "daily", ExportDir: "/a"})
	putBackupRow(t, st, &metadata.Backup{Key: "weekly", ExportDir: "/b"})

	rows, err := st.Backups().List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 || rows[0].Key != "daily" || rows[1].Key != "weekly" {
		t.Fatalf("list = %+v, want daily,weekly key-ordered", rows)
	}

	if err := st.Backups().Delete(ctx, "daily"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := st.Backups().Delete(ctx, "daily"); !errors.Is(err, metadata.ErrBackupNotFound) {
		t.Errorf("delete(missing) = %v, want ErrBackupNotFound", err)
	}
	rows, err = st.Backups().List(ctx)
	if err != nil || len(rows) != 1 || rows[0].Key != "weekly" {
		t.Errorf("list after delete = %v (%v), want the weekly row alone", rows, err)
	}
}
