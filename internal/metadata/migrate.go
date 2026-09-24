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

// migrationsFS embeds both dialect migration sets (ADR-0007). Version files
// are named NNN_description.sql; they apply in ascending order, each in one
// transaction, and are recorded in schema_migrations. The two dialect
// directories carry the same version numbers in lockstep; the parity of the
// sets is pinned by the migrations dialect test (both sides must ship one
// file per version).
//
//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationsFS embed.FS

// dialect names the SQL dialect of a migration set (ADR-0007 lockstep: same
// version numbers, same schema_migrations semantics, dialect-local SQL).
type dialect string

const (
	dialectSQLite   dialect = "sqlite"
	dialectPostgres dialect = "postgres"
)

// migrationsDir is the embedded directory of a dialect's migration set.
func migrationsDir(d dialect) string { return "migrations/" + string(d) }

// migration is one parsed versioned SQL file.
type migration struct {
	version int
	name    string
	body    string
}

// loadMigrations parses and returns the embedded SQLite migrations (the
// dialect every pre-postgres caller means) sorted by version ascending.
func loadMigrations(fsys fs.FS) []migration {
	return loadDialectMigrations(fsys, migrationsDir(dialectSQLite))
}

// loadDialectMigrations parses and returns one dialect's embedded migration
// set sorted by version ascending. Duplicate version numbers are a packaging
// error and panic early at startup rather than corrupting a database later.
func loadDialectMigrations(fsys fs.FS, dir string) []migration {
	entries, err := fs.ReadDir(fsys, dir)
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
		body, err := fs.ReadFile(fsys, dir+"/"+e.Name())
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

// migrate applies every pending migration of the given dialect, each inside
// its own transaction. It is idempotent: applied versions are skipped, so
// reopening a database re-runs nothing (ADR-0007).
func migrate(ctx context.Context, db *sql.DB, now string, d dialect) error {
	migs := loadDialectMigrations(migrationsFS, migrationsDir(d))
	current, err := CurrentVersion(ctx, db)
	if err != nil {
		return err
	}
	for _, m := range migs {
		if m.version <= current {
			continue
		}
		if err := applyMigration(ctx, db, m, now, d); err != nil {
			return fmt.Errorf("metadata: migration %03d_%s: %w", m.version, m.name, err)
		}
	}
	return nil
}

// applyMigration runs one migration's statements and its bookkeeping insert
// in a single transaction: either the schema change and the version row land
// together, or neither does.
func applyMigration(ctx context.Context, db *sql.DB, m migration, now string, d dialect) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	if _, err := tx.ExecContext(ctx, m.body); err != nil {
		return fmt.Errorf("exec %03d_%s.sql: %w", m.version, m.name, err)
	}
	if _, err := tx.ExecContext(ctx, rebind(d,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
	), m.version, now); err != nil {
		return fmt.Errorf("recording version %d: %w", m.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// rebind rewrites the ? placeholders of a dialect-common SQL statement into
// the dialect's wire form. Postgres's extended protocol has no ? placeholder
// (pgx passes query text through verbatim), so every ? becomes $n in
// positional order; sqlite understands both spellings and keeps the original
// text. Text outside statements is respected: single-quoted strings (with
// the doubled-quote escape),
// quoted identifiers (""), -- line comments, /* block comments */ and
// dollar-quoted bodies ($$...$$, $tag$...$tag$) pass through untouched.
func rebind(d dialect, query string) string {
	if d != dialectPostgres {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	for i := 0; i < len(query); {
		c := query[i]
		switch {
		case c == '\'' || c == '"': // quoted literal / identifier, '' escape
			quote := c
			j := i + 1
			for j < len(query) {
				if query[j] == quote {
					if j+1 < len(query) && query[j+1] == quote {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			b.WriteString(query[i:min(j, len(query))])
			i = j
		case c == '-' && i+1 < len(query) && query[i+1] == '-':
			if j := strings.IndexByte(query[i:], '\n'); j >= 0 {
				b.WriteString(query[i : i+j+1])
				i += j + 1
			} else {
				b.WriteString(query[i:])
				i = len(query)
			}
		case c == '/' && i+1 < len(query) && query[i+1] == '*':
			if j := strings.Index(query[i+2:], "*/"); j >= 0 {
				b.WriteString(query[i : i+2+j+2])
				i += 2 + j + 2
			} else {
				b.WriteString(query[i:])
				i = len(query)
			}
		case c == '$' && i+1 < len(query) && dollarQuoteEnd(query, i) > i:
			end := dollarQuoteEnd(query, i)
			b.WriteString(query[i:end])
			i = end
		case c == '?':
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// dollarQuoteEnd returns the index just past the dollar-quoted body starting
// at query[i] ($$...$$ or $tag$...$tag$), or i when query[i] does not open a
// dollar quote.
func dollarQuoteEnd(query string, i int) int {
	tagEnd := strings.IndexByte(query[i+1:], '$')
	if tagEnd < 0 {
		return i
	}
	tagEnd += i + 1 // index of the closing $ of the opening delimiter
	tag := query[i : tagEnd+1]
	if !isDollarTag(tag) {
		return i
	}
	if j := strings.Index(query[tagEnd+1:], tag); j >= 0 {
		return tagEnd + 1 + j + len(tag)
	}
	return len(query) // unterminated: treat the rest as body
}

// isDollarTag reports whether tag is a well-formed dollar-quote delimiter
// ($$ or $[A-Za-z_][A-Za-z0-9_]*$).
func isDollarTag(tag string) bool {
	if len(tag) < 2 || tag[0] != '$' || tag[len(tag)-1] != '$' {
		return false
	}
	for _, c := range tag[1 : len(tag)-1] {
		switch {
		case c == '_', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}
