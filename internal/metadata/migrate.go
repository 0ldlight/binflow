package metadata

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// migrationsFS embeds the SQLite dialect migration set (ADR-0007). Version
// files are named NNN_description.sql; they apply in ascending order, each in
// one transaction, and are recorded in schema_migrations. The postgres
// directory ships a placeholder README only (PRD FR-3-AC10).
//
//go:embed migrations/sqlite/*.sql
var migrationsFS embed.FS

// migration is one parsed versioned SQL file.
type migration struct {
	version int
	name    string
	body    string
}

// loadMigrations parses and returns the embedded migrations sorted by version
// ascending. Duplicate version numbers are a packaging error and panic early
// at startup rather than corrupting a database later.
func loadMigrations(fsys fs.FS) []migration {
	entries, err := fs.ReadDir(fsys, "migrations/sqlite")
	if err != nil {
		panic(fmt.Sprintf("metadata: embedded migrations unreadable: %v", err))
	}
	var migs []migration
	seen := map[int]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		version, name, ok := parseMigrationName(e.Name())
		if !ok {
			panic(fmt.Sprintf("metadata: migration file %q is not named NNN_description.sql", e.Name()))
		}
		if prev, dup := seen[version]; dup {
			panic(fmt.Sprintf("metadata: duplicate migration version %03d: %q and %q", version, prev, e.Name()))
		}
		seen[version] = e.Name()
		body, err := fs.ReadFile(fsys, "migrations/sqlite/"+e.Name())
		if err != nil {
			panic(fmt.Sprintf("metadata: reading migration %q: %v", e.Name(), err))
		}
		migs = append(migs, migration{version: version, name: name, body: string(body)})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	return migs
}

// parseMigrationName splits "001_init.sql" into (1, "init", true).
func parseMigrationName(filename string) (version int, name string, ok bool) {
	if !strings.HasSuffix(filename, ".sql") {
		return 0, "", false
	}
	base := strings.TrimSuffix(filename, ".sql")
	underscore := strings.IndexByte(base, '_')
	if underscore <= 0 {
		return 0, "", false
	}
	version, err := strconv.Atoi(base[:underscore])
	if err != nil || version <= 0 {
		return 0, "", false
	}
	return version, base[underscore+1:], true
}

// CurrentVersion reports the highest applied migration version. A database
// with no schema_migrations table (fresh file) reports 0.
func CurrentVersion(ctx context.Context, db *sql.DB) (int, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return 0, fmt.Errorf("metadata: creating schema_migrations: %w", err)
	}
	var current sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&current); err != nil {
		return 0, fmt.Errorf("metadata: reading schema_migrations: %w", err)
	}
	return int(current.Int64), nil
}

// migrate applies every pending migration, each inside its own transaction.
// It is idempotent: applied versions are skipped, so reopening a database
// re-runs nothing (ADR-0007).
func migrate(ctx context.Context, db *sql.DB, now string) error {
	migs := loadMigrations(migrationsFS)
	current, err := CurrentVersion(ctx, db)
	if err != nil {
		return err
	}
	for _, m := range migs {
		if m.version <= current {
			continue
		}
		if err := applyMigration(ctx, db, m, now); err != nil {
			return fmt.Errorf("metadata: migration %03d_%s: %w", m.version, m.name, err)
		}
	}
	return nil
}

// applyMigration runs one migration's statements and its bookkeeping insert
// in a single transaction: either the schema change and the version row land
// together, or neither does.
func applyMigration(ctx context.Context, db *sql.DB, m migration, now string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	if _, err := tx.ExecContext(ctx, m.body); err != nil {
		return fmt.Errorf("exec %03d_%s.sql: %w", m.version, m.name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		m.version, now,
	); err != nil {
		return fmt.Errorf("recording version %d: %w", m.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
