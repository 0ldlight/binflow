package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// scheduleStore implements ScheduleStore over the 021 schedules table (M16
// T-446, ADR-0044 decision 2). A dumb ledger: every column round-trips as
// text, the only interpretation is the due predicate (enabled, non-empty
// next_run_at, lexicographic RFC3339 comparison — the webhook deliveries
// queue-predicate shape over idx_schedules_due).
type scheduleStore struct{ db *sql.DB }

// scheduleUpsertStmt is Put: a plain upsert by (domain, key) that keeps the
// ORIGINAL created_at/created_by on conflict (a run-state write-back or a
// config edit is an update of an existing schedule, never a re-creation).
const scheduleUpsertStmt = `INSERT INTO schedules
	(domain, key, cron_expr, enabled, next_run_at, last_run_at, last_status, last_error,
	 created_at, created_by, updated_at, updated_by)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT (domain, key) DO UPDATE SET
		cron_expr   = excluded.cron_expr,
		enabled     = excluded.enabled,
		next_run_at = excluded.next_run_at,
		last_run_at = excluded.last_run_at,
		last_status = excluded.last_status,
		last_error  = excluded.last_error,
		updated_at  = excluded.updated_at,
		updated_by  = excluded.updated_by`

const scheduleSelectCols = `domain, key, cron_expr, enabled, next_run_at, last_run_at, last_status, last_error,
	created_at, created_by, updated_at, updated_by`

// Put implements ScheduleStore.Put.
func (s *scheduleStore) Put(ctx context.Context, sc *Schedule) error {
	enabled := int64(0)
	if sc.Enabled {
		enabled = 1
	}
	if _, err := s.db.ExecContext(ctx, scheduleUpsertStmt,
		sc.Domain, sc.Key, sc.CronExpr, enabled, sc.NextRunAt,
		sc.LastRunAt, sc.LastStatus, sc.LastError,
		sc.CreatedAt, sc.CreatedBy, sc.UpdatedAt, sc.UpdatedBy); err != nil {
		return wrapExec("schedules put", sc.Domain+"/"+sc.Key, err)
	}
	return nil
}

// Get implements ScheduleStore.Get.
func (s *scheduleStore) Get(ctx context.Context, domain, key string) (*Schedule, error) {
	const stmt = `SELECT ` + scheduleSelectCols + ` FROM schedules WHERE domain = ? AND key = ?`
	row := s.db.QueryRowContext(ctx, stmt, domain, key)
	sc := &Schedule{}
	err := row.Scan(&sc.Domain, &sc.Key, &sc.CronExpr, &sc.Enabled,
		&sc.NextRunAt, &sc.LastRunAt, &sc.LastStatus, &sc.LastError,
		&sc.CreatedAt, &sc.CreatedBy, &sc.UpdatedAt, &sc.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("schedules get %s/%s: %w", domain, key, ErrScheduleNotFound)
	}
	if err != nil {
		return nil, wrapExec("schedules get", domain+"/"+key, err)
	}
	return sc, nil
}

// Delete implements ScheduleStore.Delete.
func (s *scheduleStore) Delete(ctx context.Context, domain, key string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM schedules WHERE domain = ? AND key = ?`, domain, key)
	if err != nil {
		return wrapExec("schedules delete", domain+"/"+key, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("schedules delete rows", domain+"/"+key, err)
	} else if n == 0 {
		return fmt.Errorf("schedules delete %s/%s: %w", domain, key, ErrScheduleNotFound)
	}
	return nil
}

// List implements ScheduleStore.List: one domain ordered by key, or every
// domain ordered by (domain, key).
func (s *scheduleStore) List(ctx context.Context, domain string) ([]*Schedule, error) {
	var (
		stmt string
		args []any
	)
	if domain == "" {
		stmt = `SELECT ` + scheduleSelectCols + ` FROM schedules ORDER BY domain, key`
	} else {
		stmt = `SELECT ` + scheduleSelectCols + ` FROM schedules WHERE domain = ? ORDER BY key`
		args = []any{domain}
	}
	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, wrapExec("schedules list", domain, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*Schedule
	for rows.Next() {
		sc := &Schedule{}
		if err := rows.Scan(&sc.Domain, &sc.Key, &sc.CronExpr, &sc.Enabled,
			&sc.NextRunAt, &sc.LastRunAt, &sc.LastStatus, &sc.LastError,
			&sc.CreatedAt, &sc.CreatedBy, &sc.UpdatedAt, &sc.UpdatedBy); err != nil {
			return nil, wrapExec("schedules list scan", domain, err)
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("schedules list rows", domain, err)
	}
	return out, nil
}

// ListDue implements ScheduleStore.ListDue: the tick query. next_run_at <>
// ” keeps disabled-shaped rows (” sorts first and would otherwise always
// be due); the comparison is lexicographic on RFC3339 UTC text, which is
// chronological over canonical second-precision stamps (the audit cursor's
// standing argument).
func (s *scheduleStore) ListDue(ctx context.Context, now string) ([]*Schedule, error) {
	const stmt = `SELECT ` + scheduleSelectCols + `
		FROM schedules
		WHERE enabled = 1 AND next_run_at <> '' AND next_run_at <= ?
		ORDER BY next_run_at, domain, key`
	rows, err := s.db.QueryContext(ctx, stmt, now)
	if err != nil {
		return nil, wrapExec("schedules list-due", "", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*Schedule
	for rows.Next() {
		sc := &Schedule{}
		if err := rows.Scan(&sc.Domain, &sc.Key, &sc.CronExpr, &sc.Enabled,
			&sc.NextRunAt, &sc.LastRunAt, &sc.LastStatus, &sc.LastError,
			&sc.CreatedAt, &sc.CreatedBy, &sc.UpdatedAt, &sc.UpdatedBy); err != nil {
			return nil, wrapExec("schedules list-due scan", "", err)
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("schedules list-due rows", "", err)
	}
	return out, nil
}
