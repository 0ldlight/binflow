package metadata

import (
	"context"
	"database/sql"
	"errors"
)

// authConfigStore implements AuthConfigStore over the 015 auth_configs table
// (M11 T-305, ADR-0035 decision 2). One row per protocol section; PutAuthConfig
// is the section-granularity integral replacement (an UPSERT keyed on the
// section — the same replace-in-one-statement contract PutLicense carries for
// the single licenses row).
type authConfigStore struct{ db *sql.DB }

// GetAuthConfig returns the stored section row or (nil, nil) when the section
// has never been written.
func (s *authConfigStore) GetAuthConfig(ctx context.Context, section string) (*AuthConfigRecord, error) {
	const stmt = `SELECT doc, updated_at, updated_by FROM auth_configs WHERE section = ?`
	row := s.db.QueryRowContext(ctx, stmt, section)
	rec := &AuthConfigRecord{Section: section}
	err := row.Scan(&rec.Doc, &rec.UpdatedAt, &rec.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, wrapExec("auth_configs get", section, err)
	}
	return rec, nil
}

// PutAuthConfig replaces the section's row atomically (one UPSERT statement —
// no intermediate state between the old and the new descriptor).
func (s *authConfigStore) PutAuthConfig(ctx context.Context, rec *AuthConfigRecord) error {
	const stmt = `INSERT INTO auth_configs (section, doc, updated_at, updated_by)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(section) DO UPDATE SET doc = excluded.doc,
			updated_at = excluded.updated_at, updated_by = excluded.updated_by`
	if _, err := s.db.ExecContext(ctx, stmt, rec.Section, rec.Doc, rec.UpdatedAt, rec.UpdatedBy); err != nil {
		return wrapExec("auth_configs put", rec.Section, err)
	}
	return nil
}

// ListAuthConfigs returns every stored section row, ordered by section name.
func (s *authConfigStore) ListAuthConfigs(ctx context.Context) ([]*AuthConfigRecord, error) {
	const stmt = `SELECT section, doc, updated_at, updated_by FROM auth_configs ORDER BY section`
	rows, err := s.db.QueryContext(ctx, stmt)
	if err != nil {
		return nil, wrapExec("auth_configs list", "", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*AuthConfigRecord
	for rows.Next() {
		rec := &AuthConfigRecord{}
		if err := rows.Scan(&rec.Section, &rec.Doc, &rec.UpdatedAt, &rec.UpdatedBy); err != nil {
			return nil, wrapExec("auth_configs list scan", "", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("auth_configs list", "", err)
	}
	return out, nil
}
