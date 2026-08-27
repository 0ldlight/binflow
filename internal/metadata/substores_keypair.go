package metadata

import (
	"context"
	"database/sql"
	"errors"
)

// gpgKeypairStore implements GpgKeypairStore over the 016 gpg_keypairs
// table (M11 T-319, ADR-0038 decision 2). One row per pair, keyed by the
// Artifactory wire identifier (pairName). The store never interprets the
// sealed columns — sealing is the service layer's contract (private_key_enc
// and passphrase_enc arrive already 'enc:v1:'-sealed) — and no method here
// returns them to a caller that could echo them: GetKeypair/ListKeypairs
// return the full row to the trusted keypair service only, which strips the
// sealed columns before anything crosses a REST boundary.
type gpgKeypairStore struct{ db *sql.DB }

// GetKeypair returns the pair's row or (nil, nil) when no pair carries the
// name.
func (s *gpgKeypairStore) GetKeypair(ctx context.Context, pairName string) (*GpgKeypairRecord, error) {
	const stmt = `SELECT pair_name, pair_type, alias, public_key, private_key_enc,
		passphrase_enc, algorithm, created_at, updated_at, updated_by
		FROM gpg_keypairs WHERE pair_name = ?`
	row := s.db.QueryRowContext(ctx, stmt, pairName)
	rec := &GpgKeypairRecord{}
	err := row.Scan(&rec.PairName, &rec.PairType, &rec.Alias, &rec.PublicKey,
		&rec.PrivateKeyEnc, &rec.PassphraseEnc, &rec.Algorithm,
		&rec.CreatedAt, &rec.UpdatedAt, &rec.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, wrapExec("gpg_keypairs get", pairName, err)
	}
	return rec, nil
}

// PutKeypair replaces the pair's row atomically (one UPSERT — create,
// update and rotation share the integral-replacement contract).
func (s *gpgKeypairStore) PutKeypair(ctx context.Context, rec *GpgKeypairRecord) error {
	const stmt = `INSERT INTO gpg_keypairs (pair_name, pair_type, alias, public_key,
		private_key_enc, passphrase_enc, algorithm, created_at, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(pair_name) DO UPDATE SET pair_type = excluded.pair_type,
			alias = excluded.alias, public_key = excluded.public_key,
			private_key_enc = excluded.private_key_enc,
			passphrase_enc = excluded.passphrase_enc, algorithm = excluded.algorithm,
			created_at = excluded.created_at, updated_at = excluded.updated_at,
			updated_by = excluded.updated_by`
	if _, err := s.db.ExecContext(ctx, stmt, rec.PairName, rec.PairType, rec.Alias,
		rec.PublicKey, rec.PrivateKeyEnc, rec.PassphraseEnc, rec.Algorithm,
		rec.CreatedAt, rec.UpdatedAt, rec.UpdatedBy); err != nil {
		return wrapExec("gpg_keypairs put", rec.PairName, err)
	}
	return nil
}

// DeleteKeypair removes the pair's row (idempotent: a missing name is a
// no-op — the service layer owns the not-found / in-use refusals).
func (s *gpgKeypairStore) DeleteKeypair(ctx context.Context, pairName string) error {
	const stmt = `DELETE FROM gpg_keypairs WHERE pair_name = ?`
	if _, err := s.db.ExecContext(ctx, stmt, pairName); err != nil {
		return wrapExec("gpg_keypairs delete", pairName, err)
	}
	return nil
}

// ListKeypairs returns every pair row, ordered by name.
func (s *gpgKeypairStore) ListKeypairs(ctx context.Context) ([]*GpgKeypairRecord, error) {
	const stmt = `SELECT pair_name, pair_type, alias, public_key, private_key_enc,
		passphrase_enc, algorithm, created_at, updated_at, updated_by
		FROM gpg_keypairs ORDER BY pair_name`
	rows, err := s.db.QueryContext(ctx, stmt)
	if err != nil {
		return nil, wrapExec("gpg_keypairs list", "", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*GpgKeypairRecord
	for rows.Next() {
		rec := &GpgKeypairRecord{}
		if err := rows.Scan(&rec.PairName, &rec.PairType, &rec.Alias, &rec.PublicKey,
			&rec.PrivateKeyEnc, &rec.PassphraseEnc, &rec.Algorithm,
			&rec.CreatedAt, &rec.UpdatedAt, &rec.UpdatedBy); err != nil {
			return nil, wrapExec("gpg_keypairs list scan", "", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("gpg_keypairs list", "", err)
	}
	return out, nil
}
