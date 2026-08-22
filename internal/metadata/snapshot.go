package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
)

// This file is the backup/restore face of the SQLite store (FR-32/GE-07/08,
// ADR-0015 decision 3/4): the online snapshot writer and the read-only
// snapshot inspectors. Everything here treats a snapshot file as an artifact
// — the writer must leave a plain, self-contained database file, and the
// inspectors must never modify one.

// VacuumInto writes a transactionally consistent snapshot of this database
// to dst using SQLite's VACUUM INTO (the online backup API face: the source
// keeps serving reads and writes through the copy, WAL included). dst must
// not exist — SQLite refuses to overwrite, and a half-written target is
// never silently retried. The output is a plain rollback-journal database
// (no -wal/-shm companions), which is exactly the shape a backup artifact
// wants: one file, byte-hashable, restorable by copy.
//
// Export runs this BEFORE copying blobs (ADR-0015: the order is a hard rule
// — snapshot first means blobs that land later can only be surplus, never
// dangling references).
func (s *sqliteStore) VacuumInto(ctx context.Context, dst string) error {
	dst = strings.TrimSpace(dst)
	if dst == "" {
		return fmt.Errorf("metadata: vacuum into: destination path is empty")
	}
	// The destination is a bound parameter, not string-interpolated SQL.
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		return fmt.Errorf("metadata: vacuum into %s: %w", dst, err)
	}
	return nil
}

// dsnSnapshotRO builds the read-only DSN for inspecting a snapshot file.
// mode=ro is honored by SQLite's own URI parser (the driver opens with
// SQLITE_OPEN_URI), so the connection is read-only at the file level — an
// inspector bug cannot scribble on an artifact. journal_mode is deliberately
// NOT set: converting an artifact to WAL would mutate it and strand -wal
// companions next to it.
func dsnSnapshotRO(path string) string {
	base := path
	if !strings.HasPrefix(path, "file:") {
		base = "file:" + url.PathEscape(path)
	}
	return base + "?mode=ro&_pragma=busy_timeout(" + strconv.Itoa(BusyTimeoutMs) + ")"
}

// openSnapshotRO opens a snapshot database file read-only. The caller owns
// closing the handle.
func openSnapshotRO(path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("metadata: snapshot: path is empty")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("metadata: snapshot %s: %w", path, err)
	}
	db, err := sql.Open(sqliteDriverName, dsnSnapshotRO(path))
	if err != nil {
		return nil, fmt.Errorf("metadata: snapshot: open %s: %w", path, err)
	}
	return db, nil
}

// SnapshotChecksums returns the live checksum set of a snapshot file: every
// sha256 referenced by a node row UNION every blob_digest referenced by a
// docker_refs row — the GC mark shape (architecture sections 4.4 and 11.12),
// computed from the SNAPSHOT so the manifest boundary is exactly what the
// snapshot can serve, not what the live store happens to reference at some
// other moment.
//
// Folder node rows are not blob references: they carry the FolderMarkerSHA
// sentinel over one shared ledger row with no physical file behind it, so the
// nodes half excludes the sentinel (T-124, from T-106 QA D-106-1 — including
// it made export refuse the whole instance as a dangling reference). The
// exclusion is by VALUE, not by path shape: the sentinel is the folder
// contract the whole read plane keys on (repo.Service.Get and the virtual
// member probe both branch on it), and no physical blob can ever collide
// with it — SHA-256 output is never 64 zero bytes.
//
// The query is SQL-direct (not the store walk the gc CLI uses) on purpose:
// a snapshot is an artifact being inspected, not a live store to open
// through Open (which would run migrations and seed against it).
func SnapshotChecksums(ctx context.Context, path string) (map[string]struct{}, error) {
	db, err := openSnapshotRO(path)
	if err != nil {
		return nil, err
	}
	defer db.Close() //nolint:errcheck // read-only inspector
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT sha256 FROM nodes WHERE sha256 != '' AND sha256 != ?
		UNION
		SELECT DISTINCT blob_digest FROM docker_refs WHERE blob_digest != ''`, FolderMarkerSHA)
	if err != nil {
		return nil, fmt.Errorf("metadata: snapshot %s: live checksums: %w", path, err)
	}
	defer rows.Close() //nolint:errcheck // read-only inspector
	set := map[string]struct{}{}
	for rows.Next() {
		var sum string
		if err := rows.Scan(&sum); err != nil {
			return nil, fmt.Errorf("metadata: snapshot %s: live checksums: %w", path, err)
		}
		set[sum] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metadata: snapshot %s: live checksums: %w", path, err)
	}
	return set, nil
}

// SnapshotSchemaVersion returns a snapshot file's highest recorded
// schema_migrations version (0 when the database carries no migrations at
// all). Import compares it against LatestSchemaVersion and refuses a
// snapshot from a NEWER build: the migration chain only points forward
// (ADR-0015 decision 4).
func SnapshotSchemaVersion(ctx context.Context, path string) (int, error) {
	db, err := openSnapshotRO(path)
	if err != nil {
		return 0, err
	}
	defer db.Close() //nolint:errcheck // read-only inspector
	// Absent table = fresh/legacy file with no recorded migrations.
	var name string
	if err := db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("metadata: snapshot %s: schema_migrations probe: %w", path, err)
	}
	var version sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		return 0, fmt.Errorf("metadata: snapshot %s: schema_migrations read: %w", path, err)
	}
	return int(version.Int64), nil
}

// latestSchemaVersion caches LatestSchemaVersion's answer (the embedded
// migration set is immutable for the life of the process).
var (
	latestSchemaVersionOnce sync.Once
	latestSchemaVersion     int
)

// LatestSchemaVersion reports the highest migration version this build
// carries — the ceiling an imported snapshot's schema_migrations must not
// exceed.
func LatestSchemaVersion() int {
	latestSchemaVersionOnce.Do(func() {
		migs := loadMigrations(migrationsFS)
		if len(migs) > 0 {
			latestSchemaVersion = migs[len(migs)-1].version
		}
	})
	return latestSchemaVersion
}

// PurgeTransientFromSnapshot removes runtime-only rows from a freshly
// written snapshot: web_sessions and upload_sessions never ride a backup
// (architecture section 11 item 19 — sessions are per-instance runtime state,
// restored users simply log in again; carrying them would also make old
// browser cookies authenticate against the restored instance, and an
// upload_sessions row without its uploads/<uuid>/data temp file is an orphan).
// remote_cache deliberately STAYS: its validator rows describe cached blobs
// that travel with blobs/.
//
// The purge opens the artifact read-write without the WAL pragma (a plain
// rollback-journal transaction leaves no companion files behind), runs after
// VacuumInto and before the artifact is hashed.
func PurgeTransientFromSnapshot(ctx context.Context, path string) error {
	if path == "" {
		return fmt.Errorf("metadata: purge snapshot: path is empty")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("metadata: purge snapshot %s: %w", path, err)
	}
	// Same shape as dsn() minus journal_mode: no WAL conversion of an artifact.
	base := path
	if !strings.HasPrefix(path, "file:") {
		base = "file:" + url.PathEscape(path)
	}
	db, err := sql.Open(sqliteDriverName, base+"?_pragma=busy_timeout("+strconv.Itoa(BusyTimeoutMs)+")&_pragma=foreign_keys(1)")
	if err != nil {
		return fmt.Errorf("metadata: purge snapshot: open %s: %w", path, err)
	}
	defer db.Close() //nolint:errcheck // purge finished or failed; either way the handle is dropped

	// Each transient table is probed independently: a snapshot predating the
	// table (web_sessions predates ADR-0014, upload_sessions predates T-209's
	// migration 010) carries nothing to purge and must not fail the export.
	// The statements are literal (no SQL concatenation): the table names are a
	// fixed allowlist, never caller input (gosec G202).
	type purgeTarget struct{ table, stmt string }
	for _, t := range []purgeTarget{
		{"web_sessions", `DELETE FROM web_sessions`},
		{"upload_sessions", `DELETE FROM upload_sessions`},
	} {
		var name string
		err = db.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, t.table).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			continue // snapshot predates the table: nothing to purge
		}
		if err != nil {
			return fmt.Errorf("metadata: purge snapshot %s: %s probe: %w", path, t.table, err)
		}
		if _, err := db.ExecContext(ctx, t.stmt); err != nil {
			return fmt.Errorf("metadata: purge snapshot %s: %s: %w", path, t.table, err)
		}
	}
	return nil
}
