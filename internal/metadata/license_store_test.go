package metadata_test

// T-279 AC 2 (migration 012): the licenses table exists in both dialect
// files, the migrator applies 012 idempotently, and the LicenseStore
// sub-store honors the single-row contract — Get absent = (nil, nil),
// Put replaces atomically (perpetual NULL round-trip included), Delete is
// idempotent.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// TestLicenseMigration012BothDialects: the ADR-0007 lockstep rule for this
// migration — the sqlite dialect (the embedded one) and the postgres
// dialect (the lockstep file) both exist and both carry the licenses table
// idempotently.
func TestLicenseMigration012BothDialects(t *testing.T) {
	for _, path := range []string{
		"migrations/sqlite/012_license.sql",
		"migrations/postgres/012_license.sql",
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("dialect file %s: %v", path, err)
		}
		if !strings.Contains(string(b), "CREATE TABLE IF NOT EXISTS licenses") {
			t.Fatalf("%s does not create the licenses table idempotently", path)
		}
	}
}

// TestLicenseMigrationApplied: a fresh open lands at least version 012
// (the licenses migration) in the schema_migrations ledger.
func TestLicenseMigrationApplied(t *testing.T) {
	md, _ := openLicenseStore(t)
	var v int
	if err := rawQueryInt(md, `SELECT MAX(version) FROM schema_migrations`, &v); err != nil {
		t.Fatalf("reading schema version: %v", err)
	}
	if v < 12 {
		t.Fatalf("schema version = %d, want >= 12 (012_license applied)", v)
	}
}

func openLicenseStore(t *testing.T) (metadata.Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	md, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "binflow.db"),
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })
	return md, ctx
}

func TestLicenseStoreRoundtrip(t *testing.T) {
	md, ctx := openLicenseStore(t)
	ls := md.Licenses()

	// No license installed: (nil, nil) — the community floor, not an error.
	rec, err := ls.GetLicense(ctx)
	if err != nil || rec != nil {
		t.Fatalf("GetLicense on empty table = (%v, %v), want (nil, nil)", rec, err)
	}

	first := &metadata.LicenseRecord{
		LicenseID: "lic-1", Tier: "pro", Licensee: "Acme Corp",
		Doc: "cHJlbQ.c2ln", IssuedAt: "2026-08-01T00:00:00Z",
		NotBefore: "2026-08-01T00:00:00Z", ExpiresAt: "2027-08-01T00:00:00Z",
		InstalledAt: "2026-08-25T00:00:00Z",
	}
	if err := ls.PutLicense(ctx, first); err != nil {
		t.Fatalf("PutLicense: %v", err)
	}
	got, err := ls.GetLicense(ctx)
	if err != nil {
		t.Fatalf("GetLicense: %v", err)
	}
	if *got != *first {
		t.Fatalf("roundtrip mismatch: %+v want %+v", got, first)
	}

	// Replace: the single row swaps whole.
	second := &metadata.LicenseRecord{
		LicenseID: "lic-2", Tier: "enterprise", Licensee: "Globex",
		Doc: "ZW50ZXI.c2lnMg", IssuedAt: "2026-08-20T00:00:00Z",
		NotBefore: "2026-08-20T00:00:00Z", ExpiresAt: "", // perpetual: NULL round-trip
		InstalledAt: "2026-08-26T00:00:00Z",
	}
	if err := ls.PutLicense(ctx, second); err != nil {
		t.Fatalf("PutLicense replace: %v", err)
	}
	got, err = ls.GetLicense(ctx)
	if err != nil {
		t.Fatalf("GetLicense after replace: %v", err)
	}
	if got.LicenseID != "lic-2" || got.ExpiresAt != "" {
		t.Fatalf("replace/perpetual roundtrip wrong: %+v", got)
	}

	// Exactly one row: the single-license model is structural.
	var n int
	if err := rawQueryInt(md, `SELECT COUNT(*) FROM licenses`, &n); err != nil {
		t.Fatalf("count licenses: %v", err)
	}
	if n != 1 {
		t.Fatalf("licenses holds %d rows, want exactly 1", n)
	}

	// Delete is idempotent.
	if err := ls.DeleteLicense(ctx); err != nil {
		t.Fatalf("DeleteLicense: %v", err)
	}
	if err := ls.DeleteLicense(ctx); err != nil {
		t.Fatalf("idempotent DeleteLicense: %v", err)
	}
	if rec, err := ls.GetLicense(ctx); err != nil || rec != nil {
		t.Fatalf("GetLicense after delete = (%v, %v), want (nil, nil)", rec, err)
	}
}

// TestLicenseMigrationIdempotent: reopening the database applies nothing
// new (ADR-0007) and keeps the stored document verbatim — the re-verify
// fact source must survive restarts byte for byte.
func TestLicenseMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.db")
	ctx := context.Background()

	md1, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: path})
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	doc := "ZG9jLXRleHQ.c2lnLXRleHQ"
	if err := md1.Licenses().PutLicense(ctx, &metadata.LicenseRecord{
		LicenseID: "lic-m", Tier: "pro", Licensee: "Acme Corp", Doc: doc,
		IssuedAt: "2026-08-01T00:00:00Z", NotBefore: "2026-08-01T00:00:00Z",
		ExpiresAt: "2027-08-01T00:00:00Z", InstalledAt: "2026-08-25T00:00:00Z",
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := md1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	md2, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: path})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = md2.Close() }()
	rec, err := md2.Licenses().GetLicense(ctx)
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if rec == nil || rec.Doc != doc {
		t.Fatalf("doc did not survive the restart verbatim: %+v", rec)
	}
}

// rawQueryInt runs a scalar int query over the store's own connection (the
// COUNT(*) structural assertions above).
func rawQueryInt(md metadata.Store, query string, out *int) error {
	type rawConn interface {
		RawConn(ctx context.Context, fn func(*sql.Conn) error) error
	}
	rc, ok := md.(rawConn)
	if !ok {
		return nil // diagnostic seam absent: skip the structural count
	}
	return rc.RawConn(context.Background(), func(c *sql.Conn) error {
		return c.QueryRowContext(context.Background(), query).Scan(out)
	})
}
