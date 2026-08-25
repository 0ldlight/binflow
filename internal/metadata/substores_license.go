package metadata

import (
	"context"
	"database/sql"
	"errors"
)

// licenseStore implements LicenseStore over the 012 licenses table. The
// single-row model is enforced by the schema (id CHECK-pinned to 1) and by
// PutLicense's one-transaction replace.
type licenseStore struct{ db *sql.DB }

// GetLicense returns the stored row or (nil, nil) when no license is
// installed.
func (s *licenseStore) GetLicense(ctx context.Context) (*LicenseRecord, error) {
	const stmt = `SELECT license_id, tier, licensee, doc, issued_at, not_before, expires_at, installed_at
		FROM licenses WHERE id = 1`
	row := s.db.QueryRowContext(ctx, stmt)
	rec := &LicenseRecord{}
	var expires sql.NullString
	err := row.Scan(&rec.LicenseID, &rec.Tier, &rec.Licensee, &rec.Doc,
		&rec.IssuedAt, &rec.NotBefore, &expires, &rec.InstalledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, wrapExec("licenses get", "", err)
	}
	rec.ExpiresAt = expires.String
	return rec, nil
}

// PutLicense replaces the single row in one transaction: the DELETE and the
// INSERT land together or not at all, so a failed replace never strands an
// empty table under a live high-tier state (the D7 install contract —
// architecture section 15.1.2's "新验通过才替换").
func (s *licenseStore) PutLicense(ctx context.Context, rec *LicenseRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapExec("licenses put begin", "", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	if _, err := tx.ExecContext(ctx, `DELETE FROM licenses WHERE id = 1`); err != nil {
		return wrapExec("licenses put delete", "", err)
	}
	var expires any
	if rec.ExpiresAt != "" {
		expires = rec.ExpiresAt
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO licenses
		(id, license_id, tier, licensee, doc, issued_at, not_before, expires_at, installed_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.LicenseID, rec.Tier, rec.Licensee, rec.Doc,
		rec.IssuedAt, rec.NotBefore, expires, rec.InstalledAt); err != nil {
		return wrapExec("licenses put insert", rec.LicenseID, err)
	}
	if err := tx.Commit(); err != nil {
		return wrapExec("licenses put commit", rec.LicenseID, err)
	}
	return nil
}

// DeleteLicense removes the row; an empty table is a success (idempotent
// uninstall keeps the community floor).
func (s *licenseStore) DeleteLicense(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM licenses WHERE id = 1`); err != nil {
		return wrapExec("licenses delete", "", err)
	}
	return nil
}
