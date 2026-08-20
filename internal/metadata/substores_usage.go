package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ---- UsageStore (004 repo_usage; T-95/GE-05, ADR-0015 decision 2) ----
//
// The counter row and the node row move in ONE transaction on both write
// methods: a crash (or an injected failure) between the two would leave the
// quota number drifting from the node state it claims to measure, and every
// future enforcement decision would key on the wrong total.
//
// Locking discipline (the store's standing invariant, store.go's busy-timeout
// comment: "no store transaction upgrades a read to a write — every tx opens
// ON a write statement"): a deferred transaction that SELECTs the old size
// before writing would upgrade its lock mid-flight, and two such upgrades
// deadlock into an immediate SQLITE_BUSY the busy_timeout cannot wait out —
// exactly what the concurrent-write tests caught in review. So the delta is
// computed by a SCALAR SUBQUERY inside the first write statement: the
// statement opens the transaction holding the write lock, the subquery reads
// the pre-write row state under it, and no upgrade ever happens.

type usageStore struct{ db *sql.DB }

// nodeUpsertStmt is byte-for-byte NodeStore.Put's statement (the combined
// method must not drift from the plain one's upsert semantics).
const nodeUpsertStmt = `INSERT INTO nodes
	(repo_key, path, sha256, size, mime, created_by, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT (repo_key, path) DO UPDATE SET
		sha256 = excluded.sha256,
		size = excluded.size,
		mime = excluded.mime,
		created_by = excluded.created_by,
		created_at = excluded.created_at,
		updated_at = excluded.updated_at`

// usageAdjustPutStmt adjusts the counter by (incoming - current row size):
// the innermost SELECT reads the size of the node row this upsert REPLACES,
// under the write lock the statement itself holds. The outer COALESCE turns
// "no row" into 0. On conflict excluded.logical_bytes carries the delta.
const usageAdjustPutStmt = `INSERT INTO repo_usage (repo_key, logical_bytes, updated_at)
	VALUES (?, ? - (SELECT COALESCE((SELECT size FROM nodes WHERE repo_key = ? AND path = ?), 0)), ?)
	ON CONFLICT (repo_key) DO UPDATE SET
		logical_bytes = logical_bytes + excluded.logical_bytes,
		updated_at = excluded.updated_at`

// usageAdjustDeleteStmt subtracts the size of the row the accompanying
// DELETE is about to drop (the adjust runs first in the same transaction,
// so the subquery still sees the row; a rolled-back or empty delete rolls
// the adjust back with it).
const usageAdjustDeleteStmt = `INSERT INTO repo_usage (repo_key, logical_bytes, updated_at)
	VALUES (?, -(SELECT COALESCE(SUM(size), 0) FROM nodes WHERE repo_key = ? AND path = ?), ?)
	ON CONFLICT (repo_key) DO UPDATE SET
		logical_bytes = logical_bytes + excluded.logical_bytes,
		updated_at = excluded.updated_at`

func (s *usageStore) Get(ctx context.Context, repoKey string) (*RepoUsage, error) {
	const stmt = `SELECT repo_key, logical_bytes, updated_at FROM repo_usage WHERE repo_key = ?`
	u := &RepoUsage{}
	err := s.db.QueryRowContext(ctx, stmt, repoKey).Scan(&u.RepoKey, &u.LogicalBytes, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		// No row = nothing ever metered: the honest total is zero, not an
		// error (GE-06 renders it, the quota gate compares against it).
		return &RepoUsage{RepoKey: repoKey, LogicalBytes: 0, UpdatedAt: ""}, nil
	}
	if err != nil {
		return nil, wrapExec("repo_usage get", repoKey, err)
	}
	return u, nil
}

func (s *usageStore) PutNodeWithUsage(ctx context.Context, n *Node, updatedAt string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapExec("repo_usage put-node begin", n.RepoKey, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	// Counter first (the write that opens the transaction), node second:
	// both land or vanish together, and the delta's subquery sees the OLD
	// row because the node upsert has not run yet. An idempotent
	// same-content redeploy computes delta 0 — same sha256 means identical
	// bytes means identical size — and only refreshes updated_at.
	if _, err := tx.ExecContext(ctx, usageAdjustPutStmt,
		n.RepoKey, n.Size, n.RepoKey, n.Path, updatedAt); err != nil {
		return wrapExec("repo_usage put-node adjust", n.RepoKey, err)
	}
	if _, err := tx.ExecContext(ctx, nodeUpsertStmt,
		n.RepoKey, n.Path, n.Sha256, n.Size, n.Mime, n.CreatedBy, n.CreatedAt, n.UpdatedAt); err != nil {
		return wrapExec("repo_usage put-node upsert", n.RepoKey, err)
	}
	if err := tx.Commit(); err != nil {
		return wrapExec("repo_usage put-node commit", n.RepoKey, err)
	}
	return nil
}

func (s *usageStore) DeleteNodeWithUsage(ctx context.Context, repoKey, path, updatedAt string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapExec("repo_usage delete-node begin", repoKey, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	// The adjust's subquery sizes the row about to die; an absent row
	// contributes 0 and the delete below then reports the not-found.
	if _, err := tx.ExecContext(ctx, usageAdjustDeleteStmt,
		repoKey, repoKey, path, updatedAt); err != nil {
		return wrapExec("repo_usage delete-node adjust", repoKey, err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM nodes WHERE repo_key = ? AND path = ?`, repoKey, path)
	if err != nil {
		return wrapExec("repo_usage delete-node", repoKey, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("repo_usage delete-node rows", repoKey, err)
	} else if n == 0 {
		// Roll back the (zero-delta) adjust and answer NodeStore.Delete's
		// exact contract.
		return fmt.Errorf("nodes delete %s/%s: %w", repoKey, path, ErrNodeNotFound)
	}
	if err := tx.Commit(); err != nil {
		return wrapExec("repo_usage delete-node commit", repoKey, err)
	}
	return nil
}
