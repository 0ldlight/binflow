package metadata

import (
	"context"
	"database/sql"
	"sort"
)

// nodePropStore implements NodePropStore over the 013 node_props table.
// The multi-value set semantics ride the primary key: one row per value,
// values ordered by the key's (name, value) arm; the merge law (same-key
// value-set replace, other keys kept — architecture section 11.40) is one
// transaction per call so a partial merge can never surface.
type nodePropStore struct{ db *sql.DB }

// List returns every property of one node as key -> ordered value set. A
// node without properties (or a node that does not exist — the caller
// owns that question) answers an empty map.
func (s *nodePropStore) List(ctx context.Context, repoKey, path string) (map[string][]string, error) {
	const stmt = `SELECT name, value FROM node_props WHERE repo_key = ? AND path = ? ORDER BY name, value`
	rows, err := s.db.QueryContext(ctx, stmt, repoKey, path)
	if err != nil {
		return nil, wrapExec("node_props list", repoKey, err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]string{}
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return nil, wrapExec("node_props list scan", repoKey, err)
		}
		out[name] = append(out[name], value)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("node_props list rows", repoKey, err)
	}
	return out, nil
}

// Merge applies the section-11.40 merge to one node in one transaction:
// per named key the value set is replaced (delete + insert), unnamed keys
// keep their rows. Key iteration is sorted so the statement sequence is
// deterministic (replay debugging, statement-order-sensitive test probes).
func (s *nodePropStore) Merge(ctx context.Context, repoKey, path string, props map[string][]string) error {
	if len(props) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapExec("node_props merge begin", repoKey, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM node_props WHERE repo_key = ? AND path = ? AND name = ?`,
			repoKey, path, key); err != nil {
			return wrapExec("node_props merge delete", repoKey+"/"+path, err)
		}
		// Values are stored in their given order semantically (a set), but
		// written sorted so the physical row order matches the primary key
		// order List reads back — deterministic whatever the caller passed.
		values := append([]string(nil), props[key]...)
		sort.Strings(values)
		for _, v := range values {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO node_props (repo_key, path, name, value) VALUES (?, ?, ?, ?)`,
				repoKey, path, key, v); err != nil {
				return wrapExec("node_props merge insert", repoKey+"/"+path, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return wrapExec("node_props merge commit", repoKey, err)
	}
	return nil
}

// Delete drops the named keys of one node (every key when keys is empty)
// in one transaction. Idempotent per the REST contract: keys the node does
// not carry simply match no rows.
func (s *nodePropStore) Delete(ctx context.Context, repoKey, path string, keys []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapExec("node_props delete begin", repoKey, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	if len(keys) == 0 {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM node_props WHERE repo_key = ? AND path = ?`, repoKey, path); err != nil {
			return wrapExec("node_props delete all", repoKey+"/"+path, err)
		}
	} else {
		sorted := append([]string(nil), keys...)
		sort.Strings(sorted)
		for _, key := range sorted {
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM node_props WHERE repo_key = ? AND path = ? AND name = ?`,
				repoKey, path, key); err != nil {
				return wrapExec("node_props delete key", repoKey+"/"+path, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return wrapExec("node_props delete commit", repoKey, err)
	}
	return nil
}
