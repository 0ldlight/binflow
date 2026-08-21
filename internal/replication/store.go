package replication

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// SQLiteStore is the SQLite implementation of Store over the schema created
// by 009_replication.sql (dialect conventions of 001_init.sql, ADR-0007:
// RFC3339 UTC text timestamps, booleans as INTEGER 0/1, no RETURNING). The
// database handle comes from the metadata layer — migrations, PRAGMAs
// (foreign_keys among them) and the connection pool are owned there; this
// store only issues SQL.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore wraps a migrated metadata database handle. The caller keeps
// ownership of the handle (closing it stays with the metadata store).
func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{db: db} }

var _ Store = (*SQLiteStore)(nil)

// boolToInt maps a Go bool onto the 0/1 INTEGER convention of the dialect.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isUniqueViolation reports whether err is a SQLite uniqueness failure; the
// modernc driver spells it "UNIQUE constraint failed". Used to normalize the
// replications.name collision onto ErrDuplicateName without leaking driver
// wording upward (the same approach as repo's create-race classification).
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// wrapErr attaches context and — for busy-class contention — the
// metadata.ErrStoreBusy retryable signal, so upper layers answer 503-retry
// exactly like every other store (T-54 contract).
func wrapErr(label, key string, err error) error {
	if err == nil {
		return nil
	}
	if metadata.IsStoreBusy(err) {
		if key == "" {
			return fmt.Errorf("replication: %s: %w: %w", label, metadata.ErrStoreBusy, err)
		}
		return fmt.Errorf("replication: %s (%s): %w: %w", label, key, metadata.ErrStoreBusy, err)
	}
	if key == "" {
		return fmt.Errorf("replication: %s: %w", label, err)
	}
	return fmt.Errorf("replication: %s (%s): %w", label, key, err)
}

// ---- ReplicationConfig CRUD (replications) ----

const configColumns = `id, name, source_repo, target_url, target_repo, target_username,
	target_password_enc, max_bandwidth_bytes_per_sec, max_items_per_push, enabled,
	created_at, updated_at`

// CreateConfig implements Store.
func (s *SQLiteStore) CreateConfig(ctx context.Context, c *ReplicationConfig) (int64, error) {
	const stmt = `INSERT INTO replications
		(name, source_repo, target_url, target_repo, target_username, target_password_enc,
		 max_bandwidth_bytes_per_sec, max_items_per_push, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := s.db.ExecContext(ctx, stmt,
		c.Name, c.SourceRepo, c.TargetURL, c.TargetRepo, c.TargetUsername, c.TargetPasswordEnc,
		c.MaxBandwidthBytesPerSec, c.MaxItemsPerPush, boolToInt(c.Enabled), c.CreatedAt, c.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, fmt.Errorf("replication: config create %s: %w", c.Name, ErrDuplicateName)
		}
		return 0, wrapErr("config create", c.Name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, wrapErr("config create id", c.Name, err)
	}
	return id, nil
}

// GetConfig implements Store.
func (s *SQLiteStore) GetConfig(ctx context.Context, id int64) (*ReplicationConfig, error) {
	const stmt = `SELECT ` + configColumns + ` FROM replications WHERE id = ?`
	c, err := scanConfig(s.db.QueryRowContext(ctx, stmt, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("replication: config get %d: %w", id, ErrConfigNotFound)
	}
	if err != nil {
		return nil, wrapErr("config get", fmt.Sprint(id), err)
	}
	return c, nil
}

// UpdateConfig implements Store.
func (s *SQLiteStore) UpdateConfig(ctx context.Context, c *ReplicationConfig) error {
	const stmt = `UPDATE replications
		SET name = ?, source_repo = ?, target_url = ?, target_repo = ?,
		    target_username = ?, target_password_enc = ?,
		    max_bandwidth_bytes_per_sec = ?, max_items_per_push = ?,
		    enabled = ?, updated_at = ?
		WHERE id = ?`
	res, err := s.db.ExecContext(ctx, stmt,
		c.Name, c.SourceRepo, c.TargetURL, c.TargetRepo,
		c.TargetUsername, c.TargetPasswordEnc,
		c.MaxBandwidthBytesPerSec, c.MaxItemsPerPush,
		boolToInt(c.Enabled), c.UpdatedAt, c.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("replication: config update %d: %w", c.ID, ErrDuplicateName)
		}
		return wrapErr("config update", fmt.Sprint(c.ID), err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapErr("config update rows", fmt.Sprint(c.ID), err)
	} else if n == 0 {
		return fmt.Errorf("replication: config update %d: %w", c.ID, ErrConfigNotFound)
	}
	return nil
}

// DeleteConfig implements Store.
func (s *SQLiteStore) DeleteConfig(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM replications WHERE id = ?`, id)
	if err != nil {
		return wrapErr("config delete", fmt.Sprint(id), err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapErr("config delete rows", fmt.Sprint(id), err)
	} else if n == 0 {
		return fmt.Errorf("replication: config delete %d: %w", id, ErrConfigNotFound)
	}
	return nil
}

// ListConfigs implements Store.
func (s *SQLiteStore) ListConfigs(ctx context.Context) ([]*ReplicationConfig, error) {
	const stmt = `SELECT ` + configColumns + ` FROM replications ORDER BY id`
	rows, err := s.db.QueryContext(ctx, stmt)
	if err != nil {
		return nil, wrapErr("config list", "", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*ReplicationConfig
	for rows.Next() {
		c, err := scanConfig(rows)
		if err != nil {
			return nil, wrapErr("config list scan", "", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr("config list rows", "", err)
	}
	return out, nil
}

// scanConfig reads one replications row off a single-row scanner or a rows
// iterator (both satisfy the Scan contract; sql.ErrNoRows passes through for
// the caller to map onto ErrConfigNotFound).
func scanConfig(scan interface{ Scan(dest ...any) error }) (*ReplicationConfig, error) {
	c := &ReplicationConfig{}
	var enabled int
	err := scan.Scan(&c.ID, &c.Name, &c.SourceRepo, &c.TargetURL, &c.TargetRepo,
		&c.TargetUsername, &c.TargetPasswordEnc,
		&c.MaxBandwidthBytesPerSec, &c.MaxItemsPerPush, &enabled,
		&c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.Enabled = enabled != 0
	return c, nil
}

// ---- ReplicationTask CRUD (replication_tasks) ----

const taskColumns = `id, replication_id, blob_sha256, node_path, status, attempts,
	last_error, created_at, completed_at`

// CreateTask implements Store (empty Status defaults to pending).
func (s *SQLiteStore) CreateTask(ctx context.Context, t *ReplicationTask) (int64, error) {
	status := t.Status
	if status == "" {
		status = TaskStatusPending
	}
	if !validTaskStatus(status) {
		return 0, fmt.Errorf("replication: task create %s: %w", status, ErrInvalidStatus)
	}
	const stmt = `INSERT INTO replication_tasks
		(replication_id, blob_sha256, node_path, status, attempts, last_error, created_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := s.db.ExecContext(ctx, stmt,
		t.ReplicationID, t.BlobSHA256, t.NodePath, status, t.Attempts, t.LastError,
		t.CreatedAt, t.CompletedAt)
	if err != nil {
		return 0, wrapErr("task create", t.NodePath, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, wrapErr("task create id", t.NodePath, err)
	}
	return id, nil
}

// GetTask implements Store.
func (s *SQLiteStore) GetTask(ctx context.Context, id int64) (*ReplicationTask, error) {
	const stmt = `SELECT ` + taskColumns + ` FROM replication_tasks WHERE id = ?`
	t, err := scanTask(s.db.QueryRowContext(ctx, stmt, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("replication: task get %d: %w", id, ErrTaskNotFound)
	}
	if err != nil {
		return nil, wrapErr("task get", fmt.Sprint(id), err)
	}
	return t, nil
}

// UpdateTask implements Store (identity columns are immutable).
func (s *SQLiteStore) UpdateTask(ctx context.Context, t *ReplicationTask) error {
	if !validTaskStatus(t.Status) {
		return fmt.Errorf("replication: task update %d: %w", t.ID, ErrInvalidStatus)
	}
	// Identity columns (replication_id, blob_sha256, node_path, created_at)
	// are immutable: a task row is one blob push attempt, and rewriting its
	// subject would silently corrupt the event history.
	const stmt = `UPDATE replication_tasks
		SET status = ?, attempts = ?, last_error = ?, completed_at = ?
		WHERE id = ?`
	res, err := s.db.ExecContext(ctx, stmt, t.Status, t.Attempts, t.LastError, t.CompletedAt, t.ID)
	if err != nil {
		return wrapErr("task update", fmt.Sprint(t.ID), err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapErr("task update rows", fmt.Sprint(t.ID), err)
	} else if n == 0 {
		return fmt.Errorf("replication: task update %d: %w", t.ID, ErrTaskNotFound)
	}
	return nil
}

// DeleteTask implements Store.
func (s *SQLiteStore) DeleteTask(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM replication_tasks WHERE id = ?`, id)
	if err != nil {
		return wrapErr("task delete", fmt.Sprint(id), err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapErr("task delete rows", fmt.Sprint(id), err)
	} else if n == 0 {
		return fmt.Errorf("replication: task delete %d: %w", id, ErrTaskNotFound)
	}
	return nil
}

// ListTasks implements Store.
func (s *SQLiteStore) ListTasks(ctx context.Context, replicationID int64, limit int) ([]*ReplicationTask, error) {
	if limit <= 0 {
		limit = 100
	}
	const stmt = `SELECT ` + taskColumns + `
		FROM replication_tasks WHERE replication_id = ?
		ORDER BY created_at DESC, id DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, stmt, replicationID, limit)
	if err != nil {
		return nil, wrapErr("task list", fmt.Sprint(replicationID), err)
	}
	defer func() { _ = rows.Close() }()
	var out []*ReplicationTask
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, wrapErr("task list scan", fmt.Sprint(replicationID), err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr("task list rows", fmt.Sprint(replicationID), err)
	}
	return out, nil
}

// ---- derived state ----

// Status implements Store.
func (s *SQLiteStore) Status(ctx context.Context, replicationID int64) (*ConfigStatus, error) {
	const stmt = `SELECT
		COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'in_progress' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'skipped' THEN 1 ELSE 0 END), 0),
		COALESCE(MAX(CASE WHEN status = 'success' THEN completed_at END), '')
		FROM replication_tasks WHERE replication_id = ?`
	st := &ConfigStatus{ReplicationID: replicationID}
	err := s.db.QueryRowContext(ctx, stmt, replicationID).Scan(
		&st.Pending, &st.InProgress, &st.Succeeded, &st.Failed, &st.Skipped, &st.LastSuccessAt)
	if err != nil {
		return nil, wrapErr("status", fmt.Sprint(replicationID), err)
	}
	return st, nil
}

// scanTask reads one replication_tasks row off a single-row scanner or a
// rows iterator (same contract as scanConfig).
func scanTask(scan interface{ Scan(dest ...any) error }) (*ReplicationTask, error) {
	t := &ReplicationTask{}
	err := scan.Scan(&t.ID, &t.ReplicationID, &t.BlobSHA256, &t.NodePath, &t.Status,
		&t.Attempts, &t.LastError, &t.CreatedAt, &t.CompletedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}
