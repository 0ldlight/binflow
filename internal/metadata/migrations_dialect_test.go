// T-519: dialect behavior of the migration machinery — version-set parity
// between the sqlite and postgres sets (ADR-0007 lockstep), dialect
// directory selection, and the ?/placeholder rebind.
package metadata

import (
	"os"
	"testing"
)

func TestMigrationVersionParity(t *testing.T) {
	sqlite := loadMigrations(migrationsFS)
	postgres := loadDialectMigrations(migrationsFS, migrationsDir(dialectPostgres))
	if len(sqlite) == 0 {
		t.Fatal("sqlite migration set is empty")
	}
	if len(sqlite) != len(postgres) {
		t.Fatalf("dialect sets differ in size: sqlite %d vs postgres %d", len(sqlite), len(postgres))
	}
	seen := map[int]string{}
	for _, m := range postgres {
		seen[m.version] = m.name
	}
	for _, m := range sqlite {
		got, ok := seen[m.version]
		if !ok {
			t.Errorf("version %03d (%s) has no postgres file", m.version, m.name)
			continue
		}
		if got != m.name {
			t.Errorf("version %03d name mismatch: sqlite %q vs postgres %q", m.version, m.name, got)
		}
	}
}

func TestMigrationsDirPerDialect(t *testing.T) {
	tests := []struct {
		d    dialect
		want string
	}{
		{dialectSQLite, "migrations/sqlite"},
		{dialectPostgres, "migrations/postgres"},
	}
	for _, tt := range tests {
		if got := migrationsDir(tt.d); got != tt.want {
			t.Errorf("migrationsDir(%q) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestMigrationsDirMissingSetPanics(t *testing.T) {
	// A packaging regression (embed pattern stops matching a directory) must
	// fail loudly at load, not apply an empty set silently.
	defer func() {
		if recover() == nil {
			t.Fatal("loadDialectMigrations on a missing directory must panic")
		}
	}()
	tmp := t.TempDir()
	_ = loadDialectMigrations(os.DirFS(tmp), "migrations/nosuchdir")
}

func TestRebindPostgres(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"plain", "INSERT INTO t (a, b) VALUES (?, ?)", "INSERT INTO t (a, b) VALUES ($1, $2)"},
		{"none", "SELECT 1", "SELECT 1"},
		{"string literal untouched", "SELECT * FROM t WHERE a = '?' AND b = ?", "SELECT * FROM t WHERE a = '?' AND b = $1"},
		{"escaped quote", `SELECT '?' WHERE b = ?`, `SELECT '?' WHERE b = $1`},
		{"quoted identifier", `SELECT "col?" FROM t WHERE a = ?`, `SELECT "col?" FROM t WHERE a = $1`},
		{"line comment", "SELECT 1 -- a ? comment\n, ? FROM t", "SELECT 1 -- a ? comment\n, $1 FROM t"},
		{"block comment", "SELECT /* ? */ ? FROM t", "SELECT /* ? */ $1 FROM t"},
		{"dollar quote", "SELECT $$a ? body$$, ? FROM t", "SELECT $$a ? body$$, $1 FROM t"},
		{"tagged dollar quote", "SELECT $fn$? $ body$fn$, ? FROM t", "SELECT $fn$? $ body$fn$, $1 FROM t"},
		{"dollar not a quote", "SELECT cost$2, ? FROM t", "SELECT cost$2, $1 FROM t"},
		{"seed statement", "INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", "INSERT INTO schema_migrations (version, applied_at) VALUES ($1, $2)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rebind(dialectPostgres, tt.query); got != tt.want {
				t.Errorf("rebind(postgres, %q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestRebindSQLitePassthrough(t *testing.T) {
	// sqlite understands ? natively; the statement must come back verbatim.
	for _, q := range []string{
		"SELECT ? FROM t WHERE a = ?",
		"INSERT INTO t VALUES (?, ?, ?)",
		"SELECT '?'",
	} {
		if got := rebind(dialectSQLite, q); got != q {
			t.Errorf("rebind(sqlite, %q) = %q, want verbatim", q, got)
		}
	}
}
