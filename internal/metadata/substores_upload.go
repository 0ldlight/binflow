package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// uploadSessionStore persists local-filestore upload sessions (010, T-209).
// It mirrors the webSessionStore shape: rows are engine-owned, state is opaque
// JSON the metadata layer never parses.
type uploadSessionStore struct{ db *sql.DB }

func (s *uploadSessionStore) Create(ctx context.Context, u *UploadSession) error {
	const stmt = `INSERT INTO upload_sessions (id, state, created_at, expires_at)
		VALUES (?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, stmt, u.ID, u.State, u.CreatedAt, u.ExpiresAt); err != nil {
		return wrapExec("upload-sessions create", u.ID, err)
	}
	return nil
}

func (s *uploadSessionStore) Get(ctx context.Context, id string) (*UploadSession, error) {
	const stmt = `SELECT id, state, created_at, expires_at
		FROM upload_sessions WHERE id = ?`
	u := &UploadSession{}
	err := s.db.QueryRowContext(ctx, stmt, id).
		Scan(&u.ID, &u.State, &u.CreatedAt, &u.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("upload-sessions get %s: %w", id, ErrUploadSessionNotFound)
	}
	if err != nil {
		return nil, wrapExec("upload-sessions get", id, err)
	}
	return u, nil
}

func (s *uploadSessionStore) SetState(ctx context.Context, id, state string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE upload_sessions SET state = ? WHERE id = ?`, state, id)
	if err != nil {
		return wrapExec("upload-sessions set-state", id, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("upload-sessions set-state rows", id, err)
	} else if n == 0 {
		return fmt.Errorf("upload-sessions set-state %s: %w", id, ErrUploadSessionNotFound)
	}
	return nil
}

func (s *uploadSessionStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM upload_sessions WHERE id = ?`, id)
	if err != nil {
		return wrapExec("upload-sessions delete", id, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("upload-sessions delete rows", id, err)
	} else if n == 0 {
		return fmt.Errorf("upload-sessions delete %s: %w", id, ErrUploadSessionNotFound)
	}
	return nil
}

// ListExpired returns rows whose expires_at has passed, ordered by expires_at
// then id for deterministic startup-sweep batches.
func (s *uploadSessionStore) ListExpired(ctx context.Context, now string, limit int) ([]*UploadSession, error) {
	q := `SELECT id, state, created_at, expires_at
		FROM upload_sessions WHERE expires_at <= ?
		ORDER BY expires_at, id`
	var args []any
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, now, limit)
	} else {
		args = append(args, now)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, wrapExec("upload-sessions list-expired", "", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*UploadSession
	for rows.Next() {
		u := &UploadSession{}
		if err := rows.Scan(&u.ID, &u.State, &u.CreatedAt, &u.ExpiresAt); err != nil {
			return nil, wrapExec("upload-sessions list-expired scan", "", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("upload-sessions list-expired rows", "", err)
	}
	return out, nil
}
