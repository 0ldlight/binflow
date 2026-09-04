package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ---- GroupStore (004, architecture section 6 final DDL) ----

type groupStore struct{ db *sql.DB }

func (s *groupStore) Create(ctx context.Context, g *Group) error {
	const stmt = `INSERT INTO groups (name, description, created_at, updated_at)
		VALUES (?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, stmt, g.Name, g.Description, g.CreatedAt, g.UpdatedAt); err != nil {
		return wrapExec("groups create", g.Name, err)
	}
	return nil
}

// Update refreshes the mutable columns (description) of the named group; the
// name is the key and never changes.
func (s *groupStore) Update(ctx context.Context, g *Group) error {
	const stmt = `UPDATE groups SET description = ?, updated_at = ? WHERE name = ?`
	res, err := s.db.ExecContext(ctx, stmt, g.Description, g.UpdatedAt, g.Name)
	if err != nil {
		return wrapExec("groups update", g.Name, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("groups update rows", g.Name, err)
	} else if n == 0 {
		return fmt.Errorf("groups update %s: %w", g.Name, ErrGroupNotFound)
	}
	return nil
}

func (s *groupStore) Get(ctx context.Context, name string) (*Group, error) {
	const stmt = `SELECT id, name, description, created_at, updated_at FROM groups WHERE name = ?`
	return scanGroup(s.db.QueryRowContext(ctx, stmt, name), name)
}

func scanGroup(row *sql.Row, key string) (*Group, error) {
	g := &Group{}
	err := row.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("groups get %s: %w", key, ErrGroupNotFound)
	}
	if err != nil {
		return nil, wrapExec("groups get", key, err)
	}
	return g, nil
}

// Delete drops the group row; user_groups rows cascade through the FK
// (architecture 6: membership teardown rides the cascade, no store-side
// cleanup needed).
func (s *groupStore) Delete(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM groups WHERE name = ?`, name)
	if err != nil {
		return wrapExec("groups delete", name, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("groups delete rows", name, err)
	} else if n == 0 {
		return fmt.Errorf("groups delete %s: %w", name, ErrGroupNotFound)
	}
	return nil
}

func (s *groupStore) List(ctx context.Context) ([]*Group, error) {
	const stmt = `SELECT id, name, description, created_at, updated_at FROM groups ORDER BY name`
	rows, err := s.db.QueryContext(ctx, stmt)
	if err != nil {
		return nil, wrapExec("groups list", "", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*Group
	for rows.Next() {
		g := &Group{}
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, wrapExec("groups list scan", "", err)
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("groups list rows", "", err)
	}
	return out, nil
}

// SetUserGroups atomically replaces the user's membership set. Names are
// resolved to ids up front so a missing group fails with ErrGroupNotFound
// (carrying the name) instead of a raw FK constraint error — the caller maps
// that to the 400 wording of SE-06. Duplicate names in the list are
// deduplicated; an empty list clears the membership.
func (s *groupStore) SetUserGroups(ctx context.Context, username string, groupNames []string) error {
	ids, err := s.resolveGroupIDs(ctx, groupNames)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapExec("groups set-user-groups begin", username, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_groups WHERE username = ?`, username); err != nil {
		return wrapExec("groups set-user-groups clear", username, err)
	}
	const insert = `INSERT INTO user_groups (group_id, username) VALUES (?, ?)`
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, insert, id, username); err != nil {
			return wrapExec("groups set-user-groups member", username, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return wrapExec("groups set-user-groups commit", username, err)
	}
	return nil
}

// resolveGroupIDs maps names to group ids, rejecting unknown names. The
// result preserves input order with duplicates removed (stable for tests).
func (s *groupStore) resolveGroupIDs(ctx context.Context, names []string) ([]int64, error) {
	if len(names) == 0 {
		return nil, nil
	}
	// Groups are few (an instance-wide handful); one full scan beats an
	// N-way IN round-trip and keeps the parameter count bounded.
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM groups`)
	if err != nil {
		return nil, wrapExec("groups resolve", "", err)
	}
	defer func() { _ = rows.Close() }()
	idByName := map[string]int64{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, wrapExec("groups resolve scan", "", err)
		}
		idByName[name] = id
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("groups resolve rows", "", err)
	}
	seen := map[string]bool{}
	var ids []int64
	for _, name := range names {
		if seen[name] {
			continue
		}
		id, ok := idByName[name]
		if !ok {
			return nil, fmt.Errorf("groups resolve %s: %w", name, ErrGroupNotFound)
		}
		seen[name] = true
		ids = append(ids, id)
	}
	return ids, nil
}

// GroupsOfUser is the authentication-time join (architecture 3.4): the
// Principal.Groups fill. Unknown or group-less users return an empty slice.
func (s *groupStore) GroupsOfUser(ctx context.Context, username string) ([]*Group, error) {
	const stmt = `SELECT g.id, g.name, g.description, g.created_at, g.updated_at
		FROM groups g JOIN user_groups ug ON ug.group_id = g.id
		WHERE ug.username = ? ORDER BY g.name`
	rows, err := s.db.QueryContext(ctx, stmt, username)
	if err != nil {
		return nil, wrapExec("groups of-user", username, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*Group
	for rows.Next() {
		g := &Group{}
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, wrapExec("groups of-user scan", username, err)
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("groups of-user rows", username, err)
	}
	return out, nil
}

// MembershipsByUser implements GroupStore.MembershipsByUser (M9, ADR-0030
// E2): the whole membership table as one username -> group-names map, each
// set ordered by group name. Users without memberships have no map entry —
// the caller renders [] (the wire contract pins empty as [], never null).
func (s *groupStore) MembershipsByUser(ctx context.Context) (map[string][]string, error) {
	const stmt = `SELECT ug.username, g.name
		FROM user_groups ug JOIN groups g ON g.id = ug.group_id
		ORDER BY ug.username, g.name`
	rows, err := s.db.QueryContext(ctx, stmt)
	if err != nil {
		return nil, wrapExec("groups memberships-by-user", "", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]string{}
	for rows.Next() {
		var username, group string
		if err := rows.Scan(&username, &group); err != nil {
			return nil, wrapExec("groups memberships-by-user scan", "", err)
		}
		out[username] = append(out[username], group)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("groups memberships-by-user rows", "", err)
	}
	return out, nil
}

// MembershipsByGroup implements GroupStore.MembershipsByGroup (M9, ADR-0030
// E5): one group's member set in ONE join — the group-side mirror of
// MembershipsByUser (section 14.1 pins the data source as a user_groups
// single-group query, so the statement filters by name instead of walking
// the whole table). Usernames come back ordered; a member-less group (or a
// name with no row — the handler's Get already answered that 404) yields a
// nil slice, not an error — the caller renders [].
func (s *groupStore) MembershipsByGroup(ctx context.Context, group string) ([]string, error) {
	const stmt = `SELECT ug.username
		FROM user_groups ug JOIN groups g ON g.id = ug.group_id
		WHERE g.name = ? ORDER BY ug.username`
	rows, err := s.db.QueryContext(ctx, stmt, group)
	if err != nil {
		return nil, wrapExec("groups memberships-by-group", group, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var username string
		if err := rows.Scan(&username); err != nil {
			return nil, wrapExec("groups memberships-by-group scan", group, err)
		}
		out = append(out, username)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("groups memberships-by-group rows", group, err)
	}
	return out, nil
}

// ---- WebSessionStore (004, ADR-0014) ----

type webSessionStore struct{ db *sql.DB }

func (s *webSessionStore) Create(ctx context.Context, w *WebSession) error {
	const stmt = `INSERT INTO web_sessions (id_hash, username, created_at, expires_at, last_used_at, revoked_at)
		VALUES (?, ?, ?, ?, ?, '')`
	if _, err := s.db.ExecContext(ctx, stmt,
		w.IDHash, w.Username, w.CreatedAt, w.ExpiresAt, w.LastUsedAt); err != nil {
		return wrapExec("web-sessions create", w.Username, err)
	}
	return nil
}

func (s *webSessionStore) GetBySHA256(ctx context.Context, idHash string) (*WebSession, error) {
	const stmt = `SELECT id_hash, username, created_at, expires_at, last_used_at, revoked_at
		FROM web_sessions WHERE id_hash = ?`
	w := &WebSession{}
	err := s.db.QueryRowContext(ctx, stmt, idHash).
		Scan(&w.IDHash, &w.Username, &w.CreatedAt, &w.ExpiresAt, &w.LastUsedAt, &w.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("web-sessions get %s: %w", shortHash(idHash), ErrWebSessionNotFound)
	}
	if err != nil {
		return nil, wrapExec("web-sessions get", shortHash(idHash), err)
	}
	return w, nil
}

func (s *webSessionStore) Touch(ctx context.Context, idHash, lastUsedAt string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE web_sessions SET last_used_at = ? WHERE id_hash = ?`, lastUsedAt, idHash)
	if err != nil {
		return wrapExec("web-sessions touch", shortHash(idHash), err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("web-sessions touch rows", shortHash(idHash), err)
	} else if n == 0 {
		return fmt.Errorf("web-sessions touch %s: %w", shortHash(idHash), ErrWebSessionNotFound)
	}
	return nil
}

func (s *webSessionStore) Revoke(ctx context.Context, idHash, revokedAt string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE web_sessions SET revoked_at = ? WHERE id_hash = ?`, revokedAt, idHash)
	if err != nil {
		return wrapExec("web-sessions revoke", shortHash(idHash), err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("web-sessions revoke rows", shortHash(idHash), err)
	} else if n == 0 {
		return fmt.Errorf("web-sessions revoke %s: %w", shortHash(idHash), ErrWebSessionNotFound)
	}
	return nil
}

// ListSweepable returns expired or revoked rows — the startup sweep
// candidates (architecture 11 item 18; the sessions/ directory pattern).
func (s *webSessionStore) ListSweepable(ctx context.Context, now string, limit int) ([]*WebSession, error) {
	q := `SELECT id_hash, username, created_at, expires_at, last_used_at, revoked_at
		FROM web_sessions WHERE expires_at <= ? OR revoked_at != ''
		ORDER BY expires_at, id_hash`
	var args []any
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, now, limit)
	} else {
		args = append(args, now)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, wrapExec("web-sessions sweepable", "", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*WebSession
	for rows.Next() {
		w := &WebSession{}
		if err := rows.Scan(&w.IDHash, &w.Username, &w.CreatedAt, &w.ExpiresAt, &w.LastUsedAt, &w.RevokedAt); err != nil {
			return nil, wrapExec("web-sessions sweepable scan", "", err)
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("web-sessions sweepable rows", "", err)
	}
	return out, nil
}

func (s *webSessionStore) Delete(ctx context.Context, idHash string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM web_sessions WHERE id_hash = ?`, idHash)
	if err != nil {
		return wrapExec("web-sessions delete", shortHash(idHash), err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("web-sessions delete rows", shortHash(idHash), err)
	} else if n == 0 {
		return fmt.Errorf("web-sessions delete %s: %w", shortHash(idHash), ErrWebSessionNotFound)
	}
	return nil
}

// shortHash narrows a session id hash for error text and logs: the hash is
// not secret material (it is the stored form), but errors travel further
// than they should and 16 hex chars identify a row for diagnostics.
func shortHash(h string) string {
	if len(h) > 16 {
		return h[:16] + "..."
	}
	return h
}

// ---- AuditStore.Query (GE-01) ----

// auditCursorSeparator joins the cursor tuple; RFC3339 UTC timestamps never
// contain it, so splitting on the first occurrence is unambiguous.
const auditCursorSeparator = "|"

// parseAuditCursor decodes the opaque keyset cursor "<time>|<id>". The empty
// cursor (first page) decodes to ok=false; anything else that does not match
// the shape is ErrInvalidCursor.
func parseAuditCursor(cursor string) (time string, id int64, ok bool, err error) {
	if cursor == "" {
		return "", 0, false, nil
	}
	timePart, idPart, found := strings.Cut(cursor, auditCursorSeparator)
	if !found || timePart == "" {
		return "", 0, false, fmt.Errorf("metadata: audit cursor %q: %w", cursor, ErrInvalidCursor)
	}
	id, err = strconv.ParseInt(idPart, 10, 64)
	if err != nil || id <= 0 {
		return "", 0, false, fmt.Errorf("metadata: audit cursor %q: %w", cursor, ErrInvalidCursor)
	}
	return timePart, id, true, nil
}

// Query is the full-parameter audit read (GE-01). Ordering is (time DESC,
// id DESC): newest first with id as the deterministic tiebreaker, which is
// exactly the reverse of every (column, time) composite index's physical
// order — the 004 indexes serve the filter and the ordering without a sort
// and without a full table scan (asserted by TestAuditQueryPlanNoFullScan).
func (s *auditStore) Query(ctx context.Context, q AuditQuery) ([]*AuditEvent, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	cursorTime, cursorID, hasCursor, err := parseAuditCursor(q.Cursor)
	if err != nil {
		return nil, err
	}

	query := `SELECT id, time, actor, action, repo_key, path, detail FROM audit_events`
	var conds []string
	var args []any
	if q.RepoKey != "" {
		conds = append(conds, "repo_key = ?")
		args = append(args, q.RepoKey)
	}
	if q.Actor != "" {
		conds = append(conds, "actor = ?")
		args = append(args, q.Actor)
	}
	if q.Action != "" {
		conds = append(conds, "action = ?")
		args = append(args, q.Action)
	}
	// Closed-open window (PRD GE-01): Since inclusive, Until exclusive.
	// RFC3339 UTC text compares chronologically.
	if q.Since != "" {
		conds = append(conds, "time >= ?")
		args = append(args, q.Since)
	}
	if q.Until != "" {
		conds = append(conds, "time < ?")
		args = append(args, q.Until)
	}
	if hasCursor {
		// Keyset predicate: everything strictly after the cursor row in
		// (time DESC, id DESC) order.
		conds = append(conds, "(time < ? OR (time = ? AND id < ?))")
		args = append(args, cursorTime, cursorTime, cursorID)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ") //nolint:gosec // conds are static literals, values are bound args
	}
	query += " ORDER BY time DESC, id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapExec("audit query", "", err)
	}
	return scanAuditEvents(rows, "audit query")
}

// scanAuditEvents drains an audit_events rows cursor into events.
func scanAuditEvents(rows *sql.Rows, label string) ([]*AuditEvent, error) {
	defer func() { _ = rows.Close() }()
	var out []*AuditEvent
	for rows.Next() {
		e := &AuditEvent{}
		if err := rows.Scan(&e.ID, &e.Time, &e.Actor, &e.Action, &e.RepoKey, &e.Path, &e.Detail); err != nil {
			return nil, wrapExec(label+" scan", "", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec(label+" rows", "", err)
	}
	return out, nil
}

// LastActionTimes is the per-actor aggregation face (FR-146.3, M16): for
// every actor with at least one event of the named action, the RFC3339
// time of their most recent such event — ONE GROUP BY statement (the
// SQL stays inside the SQLite/Postgres common subset, ADR-0007). The
// audit log is append-only, so MAX(time) never decreases between calls;
// stale actors (users deleted after their last login) stay in the map and
// are simply never looked up by the consumers. The 004
// idx_audit_action(action,time) index serves the equality filter.
func (s *auditStore) LastActionTimes(ctx context.Context, action string) (map[string]string, error) {
	const stmt = `SELECT actor, MAX(time) FROM audit_events WHERE action = ? GROUP BY actor`
	rows, err := s.db.QueryContext(ctx, stmt, action)
	if err != nil {
		return nil, wrapExec("audit last action times", action, err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]string)
	for rows.Next() {
		var actor, last string
		if err := rows.Scan(&actor, &last); err != nil {
			return nil, wrapExec("audit last action times scan", action, err)
		}
		out[actor] = last
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("audit last action times rows", action, err)
	}
	return out, nil
}
