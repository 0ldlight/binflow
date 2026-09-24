// T-519: postgres store integration. Gated on BINFLOW_TEST_POSTGRES_DSN —
// the leg is NOT_RUN (four-state discipline: skip is not a pass) unless the
// environment hands the test a live postgres instance, e.g.
//
//	docker run -e POSTGRES_PASSWORD=test -p 5432:5432 postgres:17-alpine
//	BINFLOW_TEST_POSTGRES_DSN='postgres://postgres:test@127.0.0.1:5432/postgres?sslmode=disable' \
//	  go test ./internal/metadata/ -run TestStorePostgres -count=1
//
// The DSN must point at a throwaway database: each run applies the full
// migration chain and seeds the admin user (reopen runs are idempotent).
package metadata

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

// postgresTestDSN returns the gate DSN, or skips with the NOT_RUN marker
// printed into the test log (the report leg records NOT_RUN, not PASS).
func postgresTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("BINFLOW_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Logf("NOT_RUN: BINFLOW_TEST_POSTGRES_DSN not set")
		t.Skip("BINFLOW_TEST_POSTGRES_DSN not set")
		return ""
	}
	return dsn
}

// testPostgresOpen opens with a bounded wait: CI containers can still be
// settling when the suite starts.
func testPostgresOpen(t *testing.T, dsn string) (*sqlStore, *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := Open(ctx, Options{Driver: "postgres", DSN: dsn})
	if err != nil {
		t.Fatalf("Open(postgres): %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st.(*sqlStore), st.(*sqlStore).db
}

func TestStorePostgresOpenMigratesAndSeeds(t *testing.T) {
	dsn := postgresTestDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, db := testPostgresOpen(t, dsn)

	// The full postgres migration chain applied: the version ledger matches
	// the embedded set one-to-one (ADR-0007 lockstep — same count, same max
	// version as the sqlite side).
	migs := loadDialectMigrations(migrationsFS, migrationsDir(dialectPostgres))
	var n, maxVersion int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(version) FROM schema_migrations`).Scan(&n, &maxVersion); err != nil {
		t.Fatalf("reading schema_migrations: %v", err)
	}
	if n != len(migs) || maxVersion != migs[len(migs)-1].version {
		t.Fatalf("schema_migrations = (%d rows, max %d), want (%d rows, max %d)", n, maxVersion, len(migs), migs[len(migs)-1].version)
	}

	// Spot-check tables from the head and the tail of the chain (001 and the
	// 02x additions) — dialect evidence that the SERIAL/timestamp bodies ran.
	for _, table := range []string{"repositories", "nodes", "tokens", "webhook_subscriptions", "schedules", "backups", "bundles", "bundle_items"} {
		var exists bool
		if err := db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`, table).Scan(&exists); err != nil {
			t.Fatalf("probing table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s missing after migrations", table)
		}
	}

	// The admin seed landed (seedAdmin rides the rebound placeholders).
	var role string
	var hash string
	var isAdmin int
	if err := db.QueryRowContext(ctx,
		`SELECT role, password_hash, is_admin FROM users WHERE username = 'admin'`).Scan(&role, &hash, &isAdmin); err != nil {
		t.Fatalf("reading seeded admin: %v", err)
	}
	if role != "admin" || isAdmin != 1 {
		t.Fatalf("admin row = (role %q, is_admin %d), want (admin, 1)", role, isAdmin)
	}
	if !VerifyPassword(defaultAdminPassword, hash) {
		t.Error("seeded admin password does not verify against the documented default")
	}
}

func TestStorePostgresReopenIdempotent(t *testing.T) {
	dsn := postgresTestDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st, db := testPostgresOpen(t, dsn)
	var before int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&before); err != nil {
		t.Fatalf("counting schema_migrations: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("closing first store: %v", err)
	}

	_, db2 := testPostgresOpen(t, dsn)
	var after int
	if err := db2.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&after); err != nil {
		t.Fatalf("counting schema_migrations after reopen: %v", err)
	}
	if after != before {
		t.Fatalf("reopen added %d migration rows (want 0; idempotence broke)", after-before)
	}
	// The admin seed must not fire twice either (the guard short-circuits
	// before the UNIQUE constraint ever sees a duplicate).
	var admins int
	if err := db2.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username = 'admin'`).Scan(&admins); err != nil {
		t.Fatalf("counting admin rows: %v", err)
	}
	if admins != 1 {
		t.Fatalf("admin rows after reopen = %d, want 1", admins)
	}
}

func TestStorePostgresDBPathNeverLeaksDSN(t *testing.T) {
	dsn := postgresTestDSN(t)
	st, _ := testPostgresOpen(t, dsn)
	if got := st.DBPath(); got != "postgres" {
		t.Fatalf("DBPath() = %q, want the redacted label %q (a DSN carries credentials)", got, "postgres")
	}
}
