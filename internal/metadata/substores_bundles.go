package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	moderncsqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// bundleStore implements BundleStore over the 025 release-bundle table
// family (M17 T-513, ADR-0046 decision 1 + Errata ①⑤). A dumb ledger in
// the buildStore tradition: rows round-trip as text/ints, the only
// interpretation is the (name, version) pair addressing and the
// UNIQUE-key fact InsertBundle surfaces as ErrBundleExists — the conflict
// tri-state's evaluation is the domain service's, never the store's.
// Nothing here touches storage/nodes (the record-plane ruling) and no
// repository row is ever created.
type bundleStore struct{ db *sql.DB }

// bundleSelectCols is the bundles header projection.
const bundleSelectCols = `bundle_name, bundle_version, state, signature, description,
	created_by, created_at, updated_by, updated_at`

// isSQLiteUnique classifies a driver-level error as a UNIQUE-constraint
// refusal (the pair key or the item identity index): the typed-code check
// isSQLiteBusy pairs with — string matching could only guess at.
func isSQLiteUnique(err error) bool {
	var se *moderncsqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	return se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE ||
		se.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
}

// InsertBundle implements BundleStore.InsertBundle: the pair row and its
// whole manifest segment in one transaction. An existing pair answers
// ErrBundleExists — the insert is never an upsert (manifest immutability).
func (s *bundleStore) InsertBundle(ctx context.Context, b *Bundle, items []*BundleItem) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapExec("bundles insert begin", b.Name+"/"+b.Version, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	if _, err := tx.ExecContext(ctx, `INSERT INTO bundles
		(bundle_name, bundle_version, state, signature, description,
		 created_by, created_at, updated_by, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.Name, b.Version, b.State, b.Signature, b.Description,
		b.CreatedBy, b.CreatedAt, b.UpdatedBy, b.UpdatedAt); err != nil {
		if isSQLiteUnique(err) {
			return fmt.Errorf("bundles insert %s/%s: %w", b.Name, b.Version, ErrBundleExists)
		}
		return wrapExec("bundles insert", b.Name+"/"+b.Version, err)
	}
	if err := insertBundleItems(ctx, tx, b.Name, b.Version, items); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return wrapExec("bundles insert commit", b.Name+"/"+b.Version, err)
	}
	return nil
}

// insertBundleItems writes the manifest segment inside an open
// transaction.
func insertBundleItems(ctx context.Context, tx *sql.Tx, name, version string, items []*BundleItem) error {
	const stmt = `INSERT INTO bundle_items
		(id, bundle_name, bundle_version, repo_key, path, sha256, size, added_at, added_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for _, it := range items {
		if _, err := tx.ExecContext(ctx, stmt,
			it.ID, name, version, it.RepoKey, it.Path,
			it.Sha256, it.Size, it.AddedAt, it.AddedBy); err != nil {
			return wrapExec("bundle items put", it.RepoKey+"/"+it.Path, err)
		}
	}
	return nil
}

// GetBundle implements BundleStore.GetBundle: the exact pair row.
func (s *bundleStore) GetBundle(ctx context.Context, name, version string) (*Bundle, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+bundleSelectCols+` FROM bundles WHERE bundle_name = ? AND bundle_version = ?`,
		name, version)
	b := &Bundle{}
	if err := row.Scan(&b.Name, &b.Version, &b.State, &b.Signature, &b.Description,
		&b.CreatedBy, &b.CreatedAt, &b.UpdatedBy, &b.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("bundles get %s/%s: %w", name, version, ErrBundleNotFound)
		}
		return nil, wrapExec("bundles get", name+"/"+version, err)
	}
	return b, nil
}

// ListBundleNames implements BundleStore.ListBundleNames: one row per
// name — COUNT(*) and MAX(created_at) of the group — ordered by name.
func (s *bundleStore) ListBundleNames(ctx context.Context) ([]*BundleName, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT bundle_name, COUNT(*), MAX(created_at) FROM bundles GROUP BY bundle_name ORDER BY bundle_name`)
	if err != nil {
		return nil, wrapExec("bundles list names", "", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*BundleName
	for rows.Next() {
		n := &BundleName{}
		if err := rows.Scan(&n.Name, &n.Versions, &n.LastCreated); err != nil {
			return nil, wrapExec("bundles list names scan", "", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("bundles list names rows", "", err)
	}
	return out, nil
}

// ListBundleVersions implements BundleStore.ListBundleVersions: every
// version of one name, newest first, version DESC as the deterministic
// tiebreak.
func (s *bundleStore) ListBundleVersions(ctx context.Context, name string) ([]*BundleVersion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT bundle_version, state, created_at FROM bundles
		 WHERE bundle_name = ? ORDER BY created_at DESC, bundle_version DESC`, name)
	if err != nil {
		return nil, wrapExec("bundles list versions", name, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*BundleVersion
	for rows.Next() {
		v := &BundleVersion{}
		if err := rows.Scan(&v.Version, &v.State, &v.Created); err != nil {
			return nil, wrapExec("bundles list versions scan", name, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("bundles list versions rows", name, err)
	}
	return out, nil
}

// ReplaceItems implements BundleStore.ReplaceItems: the resume arm's one
// write — the segment leaves whole, the new one lands, the row's state and
// updated_* move in the SAME transaction (a state flip without its
// snapshots, or snapshots without the flip, would be a readable lie). The
// parent FK rejects an orphan segment: a missing pair answers
// ErrBundleNotFound.
func (s *bundleStore) ReplaceItems(ctx context.Context, name, version, state string, items []*BundleItem, updatedBy, updatedAt string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapExec("bundle items begin", name+"/"+version, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	res, err := tx.ExecContext(ctx,
		`UPDATE bundles SET state = ?, updated_by = ?, updated_at = ?
		 WHERE bundle_name = ? AND bundle_version = ?`,
		state, updatedBy, updatedAt, name, version)
	if err != nil {
		return wrapExec("bundle items state", name+"/"+version, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("bundle items state rows", name+"/"+version, err)
	} else if n == 0 {
		return fmt.Errorf("bundle items state %s/%s: %w", name, version, ErrBundleNotFound)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM bundle_items WHERE bundle_name = ? AND bundle_version = ?`, name, version); err != nil {
		return wrapExec("bundle items clear", name+"/"+version, err)
	}
	if err := insertBundleItems(ctx, tx, name, version, items); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return wrapExec("bundle items commit", name+"/"+version, err)
	}
	return nil
}

// ListItems implements BundleStore.ListItems: the manifest segment ordered
// by (repo_key, path).
func (s *bundleStore) ListItems(ctx context.Context, name, version string) ([]*BundleItem, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_key, path, sha256, size, added_at, added_by FROM bundle_items
		 WHERE bundle_name = ? AND bundle_version = ? ORDER BY repo_key, path`, name, version)
	if err != nil {
		return nil, wrapExec("bundle items list", name+"/"+version, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*BundleItem
	for rows.Next() {
		it := &BundleItem{}
		if err := rows.Scan(&it.ID, &it.RepoKey, &it.Path, &it.Sha256, &it.Size,
			&it.AddedAt, &it.AddedBy); err != nil {
			return nil, wrapExec("bundle items list scan", name+"/"+version, err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("bundle items list rows", name+"/"+version, err)
	}
	return out, nil
}
