package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	moderncsqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// sqliteDriverName is the database/sql driver registered by modernc.org/sqlite.
const sqliteDriverName = "sqlite"

// BusyTimeoutMs is the per-connection busy_timeout budget (T-54, up from
// T-10's 5000). WAL serializes writers; a waiter retries inside this budget
// before SQLITE_BUSY escapes. The budget must exceed the worst WALL-CLOCK
// span a lock holder can stay scheduled-away or fsync-blocked: under a
// whole-repo `go test -race` fan-out (or a production fsync storm) observed
// request spans reached ~14s with the old 5s budget, i.e. the holder was
// descheduled well past 5s and the waiter's busy wait expired (the T-54
// reproduction: blobs put → SQLITE_BUSY → push 500). 15s covers the observed
// worst case with margin. Waiting cannot deadlock here: no store
// transaction upgrades a read to a write (every tx opens ON a write
// statement), so a waiter only ever waits for a finite holder.
const BusyTimeoutMs = 15000

// Options configures Open.
type Options struct {
	// Driver selects the dialect. M1 supports "sqlite"; "postgres" returns
	// errPostgresDisabled (PRD FR-3-AC10).
	Driver string
	// DSN is the sqlite file path. When empty, Path is used.
	DSN string
	// Path is the sqlite file path used when DSN is empty. When both are
	// empty the caller is expected to have resolved data_dir/binflow.db;
	// Open fails on an empty final path rather than touching the cwd.
	Path string
	// AdminPassword seeds the admin account when the users table has no such
	// row. When empty, the documented default "password" is used (ADR-0009;
	// upper layers warn on the default value).
	AdminPassword string
}

// errPostgresDisabled marks the M1 postgres gap. The message is user-facing
// log wording (FR-3-AC10).
var errPostgresDisabled = errors.New("postgres support is not enabled in this build (M1 ships SQLite only)")

// Open opens (creating if needed) the metadata database, applies pending
// migrations and seeds the admin user. Reopening the same file is idempotent:
// applied migrations are skipped and the admin seed only fires when no admin
// row exists.
func Open(ctx context.Context, opts Options) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(opts.Driver)) {
	case "", "sqlite":
	case "postgres":
		return nil, fmt.Errorf("metadata: driver %q: %w", opts.Driver, errPostgresDisabled)
	default:
		return nil, fmt.Errorf("metadata: unknown driver %q (supported: sqlite)", opts.Driver)
	}

	path := opts.DSN
	if path == "" {
		path = opts.Path
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("metadata: empty sqlite database path")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("metadata: creating database directory %s: %w", dir, err)
		}
	}

	db, err := sql.Open(sqliteDriverName, dsn(path))
	if err != nil {
		return nil, fmt.Errorf("metadata: opening sqlite %s: %w", path, err)
	}
	// ADR-0007 pool budget: NumCPU connections. Per-connection PRAGMAs
	// (foreign_keys, busy_timeout, case_sensitive_like) ride in the DSN so
	// they hold on every pooled connection; journal_mode=WAL is a database
	// file property and is applied once via setupSQLite below.
	db.SetMaxOpenConns(NumCPUConcurrency())
	db.SetMaxIdleConns(NumCPUConcurrency())
	db.SetConnMaxLifetime(0)

	if err := setupSQLite(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrate(ctx, db, Now()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := seedAdmin(ctx, db, opts.AdminPassword); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &sqliteStore{db: db, path: path}, nil
}

// dsn builds the driver DSN in file:...?_pragma=... form.
//
// foreign_keys, busy_timeout and case_sensitive_like are per-connection
// PRAGMAs, so they must be in the DSN: modernc.org/sqlite applies the
// _pragma list inside newConn on every connection the pool creates. Exec'ing
// them once via db.ExecContext would only configure whichever single
// connection happened to serve that call — with a pool larger than one that
// leaves FK enforcement silently off on later connections (review B2).
// case_sensitive_like=ON makes the LIKE arm of prefix queries binary
// sensitive (SQLite LIKE is ASCII case-insensitive by default); without it
// DeleteByPrefix("LIB") would silently delete "lib/..." (review B1).
func dsn(path string) string {
	base := path
	if !strings.HasPrefix(path, "file:") && path != ":memory:" {
		base = "file:" + url.PathEscape(path)
	}
	sep := "?"
	if idx := strings.Index(base, "?"); idx >= 0 {
		sep = "&"
	}
	// Order matters only for readability; the driver sorts the _pragma list
	// itself (busy_timeout first).
	return base + sep +
		"_pragma=foreign_keys(1)" +
		"&_pragma=busy_timeout(" + strconv.Itoa(BusyTimeoutMs) + ")" +
		"&_pragma=case_sensitive_like(1)" +
		"&_pragma=journal_mode(WAL)"
}

// isSQLiteBusy classifies a driver-level error as busy-class contention
// (SQLITE_BUSY: another connection holds the write lock past our
// busy_timeout; SQLITE_LOCKED: table-level contention with shared-cache
// style access). Typed against *sqlite.Error — the driver's Code() carries
// the real result code, which string matching could only guess at.
func isSQLiteBusy(err error) bool {
	var se *moderncsqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	return se.Code() == sqlite3.SQLITE_BUSY || se.Code() == sqlite3.SQLITE_LOCKED
}

// setupSQLite verifies connectivity and asserts that the DSN PRAGMAs are
// actually in force (defense in depth: a DSN regression must fail loudly at
// startup, not the first time a prefix query misbehaves).
//
// case_sensitive_like is a flag pragma with no readable value, so it is
// asserted behaviorally instead: 'A' LIKE 'a' is true only when LIKE is
// case-insensitive.
func setupSQLite(ctx context.Context, db *sql.DB) error {
	pragmas := []struct{ name, want string }{
		{"journal_mode", "wal"},
		{"foreign_keys", "1"},
		{"busy_timeout", strconv.Itoa(BusyTimeoutMs)},
	}
	for _, p := range pragmas {
		var got string
		if err := db.QueryRowContext(ctx, "PRAGMA "+p.name).Scan(&got); err != nil {
			return fmt.Errorf("metadata: PRAGMA %s: %w", p.name, err)
		}
		if got != p.want {
			return fmt.Errorf("metadata: PRAGMA %s = %q, want %q (check the DSN in store.go)", p.name, got, p.want)
		}
	}
	var likeCaseInsensitive bool
	if err := db.QueryRowContext(ctx, `SELECT 'A' LIKE 'a'`).Scan(&likeCaseInsensitive); err != nil {
		return fmt.Errorf("metadata: probing case_sensitive_like: %w", err)
	}
	if likeCaseInsensitive {
		return fmt.Errorf("metadata: LIKE is case-insensitive; case_sensitive_like is not in force (check the DSN in store.go)")
	}
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("metadata: sqlite ping: %w", err)
	}
	return nil
}

// NumCPUConcurrency reports the connection budget ADR-0007 assigns to SQLite
// (MaxOpenConns = NumCPU).
func NumCPUConcurrency() int { return runtime.NumCPU() }

// Now returns the current time as RFC3339 UTC text, the timestamp format of
// every metadata column (ADR-0007).
func Now() string { return time.Now().UTC().Format(time.RFC3339) }

// NeverExpires is the expires_at sentinel meaning "no expiry".
const NeverExpires = "9999-12-31T00:00:00Z"

// FolderMarkerSHA is the sha256 sentinel every folder node row carries
// (repo.Service's folder deploy — a trailing-slash path with an empty body,
// rest-api.md section 1.1). A folder row has no content of its own: the
// sentinel exists to satisfy the NOT NULL + FK pair on nodes.sha256 against
// ONE shared blobs-ledger marker row (size 0), so folder paths at every
// parent level reuse it (marker semantics, not content). The writer lives in
// internal/repo (emptyFolderSHA aliases this constant — one spelling, two
// packages).
//
// The sentinel can never name a physical blob: SHA-256 output is never 64
// zero bytes, and a checksum-deploy that declares it is refused because no
// filestore object backs it. Consumers deriving a set of PHYSICAL blob
// references from node rows — the backup manifest boundary (SnapshotChecksums)
// and the GC mark walkers (cmd/binflow-server and httpapi liveChecksumSet) —
// must exclude it: treating it as a blob reference fails closed on a file the
// filestore can never carry (T-124, from T-106 QA D-106-1).
const FolderMarkerSHA = "0000000000000000000000000000000000000000000000000000000000000000"

// defaultAdminPassword is the documented evaluation default (ADR-0009). Upper
// layers must warn when it is in effect; this package just uses it.
const defaultAdminPassword = "password"

// seedAdmin inserts the admin row if missing. The password comes from
// BINFLOW_ADMIN_PASSWORD via opts; absent that the documented default applies.
// An existing admin row is never overwritten. The hash is computed only when
// a seed will actually happen (argon2 costs ~50ms; no reason to pay it on
// every restart of an already-seeded database).
func seedAdmin(ctx context.Context, db *sql.DB, password string) error {
	if password == "" {
		password = defaultAdminPassword
	}
	var exists bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM users WHERE username = 'admin')`).Scan(&exists); err != nil {
		return fmt.Errorf("metadata: checking admin seed: %w", err)
	}
	if exists {
		return nil
	}
	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("metadata: hashing admin password: %w", err)
	}
	now := Now()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, is_admin, enabled, created_at, updated_at, provider, provider_id, role)
		 VALUES ('admin', ?, 1, 1, ?, ?, 'local', '', 'admin')`,
		hash, now, now); err != nil {
		return fmt.Errorf("metadata: seeding admin user: %w", err)
	}
	return nil
}

// sqliteStore implements Store over the pure-Go SQLite driver.
type sqliteStore struct {
	db   *sql.DB
	path string
}

var _ Store = (*sqliteStore)(nil)

// DBPath exposes the database file path for diagnostics (health output, test
// file-scan assertions).
func (s *sqliteStore) DBPath() string { return s.path }

// RawConn acquires one pooled connection and hands it to fn. It exists for
// diagnostics that must observe per-connection driver state (PRAGMA
// assertions); normal code paths must go through the Store sub-stores.
func (s *sqliteStore) RawConn(ctx context.Context, fn func(*sql.Conn) error) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("metadata: acquiring connection: %w", err)
	}
	defer func() { _ = conn.Close() }()
	return fn(conn)
}

func (s *sqliteStore) Repos() RepoStore             { return &repoStore{db: s.db} }
func (s *sqliteStore) Nodes() NodeStore             { return &nodeStore{db: s.db} }
func (s *sqliteStore) Blobs() BlobStore             { return &blobStore{db: s.db} }
func (s *sqliteStore) Users() UserStore             { return &userStore{db: s.db} }
func (s *sqliteStore) Tokens() TokenStore           { return &tokenStore{db: s.db} }
func (s *sqliteStore) Permissions() PermissionStore { return &permissionStore{db: s.db} }
func (s *sqliteStore) Audits() AuditStore           { return &auditStore{db: s.db} }
func (s *sqliteStore) Docker() DockerStore          { return &dockerStore{db: s.db} }
func (s *sqliteStore) Remote() RemoteStore          { return &remoteStore{db: s.db} }
func (s *sqliteStore) Virtual() VirtualStore        { return &virtualStore{db: s.db} }
func (s *sqliteStore) Groups() GroupStore           { return &groupStore{db: s.db} }
func (s *sqliteStore) WebSessions() WebSessionStore { return &webSessionStore{db: s.db} }
func (s *sqliteStore) UploadSessions() UploadSessionStore {
	return &uploadSessionStore{db: s.db}
}
func (s *sqliteStore) Usage() UsageStore        { return &usageStore{db: s.db} }
func (s *sqliteStore) Licenses() LicenseStore   { return &licenseStore{db: s.db} }
func (s *sqliteStore) NodeProps() NodePropStore { return &nodePropStore{db: s.db} }
func (s *sqliteStore) AuthConfigs() AuthConfigStore {
	return &authConfigStore{db: s.db}
}

func (s *sqliteStore) GpgKeypairs() GpgKeypairStore {
	return &gpgKeypairStore{db: s.db}
}

func (s *sqliteStore) Schedules() ScheduleStore {
	return &scheduleStore{db: s.db}
}

func (s *sqliteStore) Backups() BackupStore {
	return &backupStore{db: s.db}
}

func (s *sqliteStore) Builds() BuildStore {
	return &buildStore{db: s.db}
}

func (s *sqliteStore) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("metadata: ping: %w", err)
	}
	return nil
}

// IsReferenced answers "does any node row or docker ref row point at sha256
// RIGHT NOW" with one bounded query (ADR-0031 mechanism A / architecture
// section 14.2 point 3): it is the GCMarker.Live oracle the sweep's
// pre-delete recheck consults, so it must stay a single-point existence
// probe — never a rebuild of the referenced set. Both halves ride their
// dedicated indexes (idx_nodes_blob, idx_docker_refs_blob).
func (s *sqliteStore) IsReferenced(ctx context.Context, sha256 string) (bool, error) {
	const stmt = `SELECT EXISTS(SELECT 1 FROM nodes WHERE sha256 = ?)
		OR EXISTS(SELECT 1 FROM docker_refs WHERE blob_digest = ?)`
	var referenced bool
	if err := s.db.QueryRowContext(ctx, stmt, sha256, sha256).Scan(&referenced); err != nil {
		return false, wrapExec("nodes/docker_refs is-referenced", "", err)
	}
	return referenced, nil
}

func (s *sqliteStore) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("metadata: closing %s: %w", s.path, err)
	}
	return nil
}
