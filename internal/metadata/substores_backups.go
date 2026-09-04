package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// backupStore implements BackupStore over the 022 backups table (M16 T-450,
// ADR-0044 decisions 2 and 5): the payload half of a scheduled backup
// configuration, the same dumb-ledger contract as the schedules store.
type backupStore struct{ db *sql.DB }

// backupUpsertStmt is Put: a plain upsert by key that keeps the ORIGINAL
// created_at/created_by on conflict (a config edit is an update of an
// existing backup, never a re-creation).
const backupUpsertStmt = `INSERT INTO backups
	(key, enabled, export_dir, created_at, created_by, updated_at, updated_by)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT (key) DO UPDATE SET
		enabled     = excluded.enabled,
		export_dir  = excluded.export_dir,
		updated_at  = excluded.updated_at,
		updated_by  = excluded.updated_by`

const backupSelectCols = `key, enabled, export_dir, created_at, created_by, updated_at, updated_by`

// Put implements BackupStore.Put.
func (s *backupStore) Put(ctx context.Context, b *Backup) error {
	enabled := int64(0)
	if b.Enabled {
		enabled = 1
	}
	if _, err := s.db.ExecContext(ctx, backupUpsertStmt,
		b.Key, enabled, b.ExportDir,
		b.CreatedAt, b.CreatedBy, b.UpdatedAt, b.UpdatedBy); err != nil {
		return wrapExec("backups put", b.Key, err)
	}
	return nil
}

// Get implements BackupStore.Get.
func (s *backupStore) Get(ctx context.Context, key string) (*Backup, error) {
	const stmt = `SELECT ` + backupSelectCols + ` FROM backups WHERE key = ?`
	row := s.db.QueryRowContext(ctx, stmt, key)
	b := &Backup{}
	err := row.Scan(&b.Key, &b.Enabled, &b.ExportDir,
		&b.CreatedAt, &b.CreatedBy, &b.UpdatedAt, &b.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("backups get %s: %w", key, ErrBackupNotFound)
	}
	if err != nil {
		return nil, wrapExec("backups get", key, err)
	}
	return b, nil
}

// Delete implements BackupStore.Delete.
func (s *backupStore) Delete(ctx context.Context, key string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM backups WHERE key = ?`, key)
	if err != nil {
		return wrapExec("backups delete", key, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("backups delete rows", key, err)
	} else if n == 0 {
		return fmt.Errorf("backups delete %s: %w", key, ErrBackupNotFound)
	}
	return nil
}

// List implements BackupStore.List: every row ordered by key.
func (s *backupStore) List(ctx context.Context) ([]*Backup, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+backupSelectCols+` FROM backups ORDER BY key`)
	if err != nil {
		return nil, wrapExec("backups list", "", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*Backup
	for rows.Next() {
		b := &Backup{}
		if err := rows.Scan(&b.Key, &b.Enabled, &b.ExportDir,
			&b.CreatedAt, &b.CreatedBy, &b.UpdatedAt, &b.UpdatedBy); err != nil {
			return nil, wrapExec("backups list scan", "", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("backups list rows", "", err)
	}
	return out, nil
}
