package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ---- UserStore ----

type userStore struct{ db *sql.DB }

func (s *userStore) Create(ctx context.Context, u *User) error {
	const stmt = `INSERT INTO users (username, password_hash, is_admin, enabled, email, created_at, updated_at, provider, provider_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, stmt,
		u.Username, u.PasswordHash, boolToInt(u.IsAdmin), boolToInt(u.Enabled), u.Email, u.CreatedAt, u.UpdatedAt, u.Provider, u.ProviderID); err != nil {
		return wrapExec("users create", u.Username, err)
	}
	return nil
}

func (s *userStore) Get(ctx context.Context, username string) (*User, error) {
	const stmt = `SELECT username, password_hash, is_admin, enabled, email, created_at, updated_at, provider, provider_id
		FROM users WHERE username = ?`
	return scanUser(s.db.QueryRowContext(ctx, stmt, username), username)
}

func (s *userStore) GetByPasswordHash(ctx context.Context, passwordHash string) (*User, error) {
	const stmt = `SELECT username, password_hash, is_admin, enabled, email, created_at, updated_at, provider, provider_id
		FROM users WHERE password_hash = ? LIMIT 1`
	return scanUser(s.db.QueryRowContext(ctx, stmt, passwordHash), passwordHash)
}

func scanUser(row *sql.Row, key string) (*User, error) {
	u := &User{}
	var isAdmin, enabled int
	err := row.Scan(&u.Username, &u.PasswordHash, &isAdmin, &enabled, &u.Email, &u.CreatedAt, &u.UpdatedAt, &u.Provider, &u.ProviderID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("users get %s: %w", key, ErrUserNotFound)
	}
	if err != nil {
		return nil, wrapExec("users get", key, err)
	}
	u.IsAdmin = isAdmin != 0
	u.Enabled = enabled != 0
	return u, nil
}

func (s *userStore) UpdatePassword(ctx context.Context, username, passwordHash string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE username = ?`,
		passwordHash, Now(), username)
	if err != nil {
		return wrapExec("users update-password", username, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("users update-password rows", username, err)
	} else if n == 0 {
		return fmt.Errorf("users update-password %s: %w", username, ErrUserNotFound)
	}
	return nil
}

// UpdateEmail sets the 004 email column (FR-27-AC8). Same shape as
// UpdatePassword; blank stays a valid stored value (the blank->400 rule is a
// service-layer validation, T-97 SE-05).
func (s *userStore) UpdateEmail(ctx context.Context, username, email string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET email = ?, updated_at = ? WHERE username = ?`,
		email, Now(), username)
	if err != nil {
		return wrapExec("users update-email", username, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("users update-email rows", username, err)
	} else if n == 0 {
		return fmt.Errorf("users update-email %s: %w", username, ErrUserNotFound)
	}
	return nil
}

// UpdateProfile refreshes the mutable non-credential columns (email, admin
// flag) in one statement (T-97: the replace/partial-update bodies of
// /api/security/users/{name}).
func (s *userStore) UpdateProfile(ctx context.Context, username, email string, isAdmin bool) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET email = ?, is_admin = ?, updated_at = ? WHERE username = ?`,
		email, boolToInt(isAdmin), Now(), username)
	if err != nil {
		return wrapExec("users update-profile", username, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("users update-profile rows", username, err)
	} else if n == 0 {
		return fmt.Errorf("users update-profile %s: %w", username, ErrUserNotFound)
	}
	return nil
}

func (s *userStore) SetEnabled(ctx context.Context, username string, enabled bool) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET enabled = ?, updated_at = ? WHERE username = ?`,
		boolToInt(enabled), Now(), username)
	if err != nil {
		return wrapExec("users set-enabled", username, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("users set-enabled rows", username, err)
	} else if n == 0 {
		return fmt.Errorf("users set-enabled %s: %w", username, ErrUserNotFound)
	}
	return nil
}

func (s *userStore) Delete(ctx context.Context, username string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE username = ?`, username)
	if err != nil {
		return wrapExec("users delete", username, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("users delete rows", username, err)
	} else if n == 0 {
		return fmt.Errorf("users delete %s: %w", username, ErrUserNotFound)
	}
	return nil
}

func (s *userStore) List(ctx context.Context) ([]*User, error) {
	const stmt = `SELECT username, password_hash, is_admin, enabled, email, created_at, updated_at, provider, provider_id
		FROM users ORDER BY username`
	rows, err := s.db.QueryContext(ctx, stmt)
	if err != nil {
		return nil, wrapExec("users list", "", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*User
	for rows.Next() {
		u := &User{}
		var isAdmin, enabled int
		if err := rows.Scan(&u.Username, &u.PasswordHash, &isAdmin, &enabled, &u.Email, &u.CreatedAt, &u.UpdatedAt, &u.Provider, &u.ProviderID); err != nil {
			return nil, wrapExec("users list scan", "", err)
		}
		u.IsAdmin = isAdmin != 0
		u.Enabled = enabled != 0
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("users list rows", "", err)
	}
	return out, nil
}

// ---- TokenStore ----

type tokenStore struct{ db *sql.DB }

func (s *tokenStore) Create(ctx context.Context, t *Token) (int64, error) {
	const stmt = `INSERT INTO tokens (username, token_sha256, expires_at, created_at, last_used_at)
		VALUES (?, ?, ?, ?, ?)`
	res, err := s.db.ExecContext(ctx, stmt, t.Username, t.TokenSHA256, t.ExpiresAt, t.CreatedAt, t.LastUsedAt)
	if err != nil {
		return 0, wrapExec("tokens create", t.Username, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, wrapExec("tokens create id", t.Username, err)
	}
	return id, nil
}

func (s *tokenStore) Get(ctx context.Context, id int64) (*Token, error) {
	const stmt = `SELECT id, username, token_sha256, expires_at, created_at, last_used_at
		FROM tokens WHERE id = ?`
	return scanToken(s.db.QueryRowContext(ctx, stmt, id), "id")
}

func (s *tokenStore) GetBySHA256(ctx context.Context, sha256 string) (*Token, error) {
	const stmt = `SELECT id, username, token_sha256, expires_at, created_at, last_used_at
		FROM tokens WHERE token_sha256 = ?`
	return scanToken(s.db.QueryRowContext(ctx, stmt, sha256), sha256)
}

func scanToken(row *sql.Row, key string) (*Token, error) {
	t := &Token{}
	err := row.Scan(&t.ID, &t.Username, &t.TokenSHA256, &t.ExpiresAt, &t.CreatedAt, &t.LastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("tokens get %s: %w", key, ErrTokenNotFound)
	}
	if err != nil {
		return nil, wrapExec("tokens get", key, err)
	}
	return t, nil
}

func (s *tokenStore) Touch(ctx context.Context, id int64, lastUsedAt string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE tokens SET last_used_at = ? WHERE id = ?`, lastUsedAt, id)
	if err != nil {
		return wrapExec("tokens touch", "", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("tokens touch rows", "", err)
	} else if n == 0 {
		return fmt.Errorf("tokens touch %d: %w", id, ErrTokenNotFound)
	}
	return nil
}

func (s *tokenStore) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tokens WHERE id = ?`, id)
	if err != nil {
		return wrapExec("tokens delete", "", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("tokens delete rows", "", err)
	} else if n == 0 {
		return fmt.Errorf("tokens delete %d: %w", id, ErrTokenNotFound)
	}
	return nil
}

func (s *tokenStore) ListByUsername(ctx context.Context, username string) ([]*Token, error) {
	const stmt = `SELECT id, username, token_sha256, expires_at, created_at, last_used_at
		FROM tokens WHERE username = ? ORDER BY id`
	rows, err := s.db.QueryContext(ctx, stmt, username)
	if err != nil {
		return nil, wrapExec("tokens list-by-username", username, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*Token
	for rows.Next() {
		t := &Token{}
		if err := rows.Scan(&t.ID, &t.Username, &t.TokenSHA256, &t.ExpiresAt, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, wrapExec("tokens list-by-username scan", username, err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("tokens list-by-username rows", username, err)
	}
	return out, nil
}

// ---- PermissionStore ----

type permissionStore struct{ db *sql.DB }

func (s *permissionStore) PutTarget(ctx context.Context, t *PermissionTarget, principals []*PermissionPrincipal) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapExec("permission put-target begin", t.Name, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO permission_targets (name, repos, includes, excludes, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (name) DO UPDATE SET
			repos = excluded.repos,
			includes = excluded.includes,
			excludes = excluded.excludes,
			updated_at = excluded.updated_at`,
		t.Name, t.Repos, t.Includes, t.Excludes, t.CreatedAt, t.UpdatedAt); err != nil {
		return wrapExec("permission put-target", t.Name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM permission_principals WHERE target_name = ?`, t.Name); err != nil {
		return wrapExec("permission put-target clear principals", t.Name, err)
	}
	const insertPrincipal = `INSERT INTO permission_principals
		(target_name, principal, principal_type, can_read, can_write, can_delete)
		VALUES (?, ?, ?, ?, ?, ?)`
	for _, p := range principals {
		if _, err := tx.ExecContext(ctx, insertPrincipal,
			t.Name, p.Principal, p.PrincipalType,
			boolToInt(p.CanRead), boolToInt(p.CanWrite), boolToInt(p.CanDelete)); err != nil {
			return wrapExec("permission put-target principal "+p.Principal, t.Name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return wrapExec("permission put-target commit", t.Name, err)
	}
	return nil
}

func (s *permissionStore) GetTarget(ctx context.Context, name string) (*PermissionTarget, []*PermissionPrincipal, error) {
	t := &PermissionTarget{}
	var err error
	if t.Repos, t.Includes, t.Excludes, err = func() (string, string, string, error) {
		row := s.db.QueryRowContext(ctx,
			`SELECT name, repos, includes, excludes, created_at, updated_at FROM permission_targets WHERE name = ?`, name)
		var repos, includes, excludes string
		if err := row.Scan(&t.Name, &repos, &includes, &excludes, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return "", "", "", err
		}
		return repos, includes, excludes, nil
	}(); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, fmt.Errorf("permission get %s: %w", name, ErrNotFound)
		}
		return nil, nil, wrapExec("permission get", name, err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, target_name, principal, principal_type, can_read, can_write, can_delete
		FROM permission_principals WHERE target_name = ? ORDER BY principal`, name)
	if err != nil {
		return nil, nil, wrapExec("permission get principals", name, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*PermissionPrincipal
	for rows.Next() {
		p := &PermissionPrincipal{}
		var canRead, canWrite, canDelete int
		if err := rows.Scan(&p.ID, &p.TargetName, &p.Principal, &p.PrincipalType, &canRead, &canWrite, &canDelete); err != nil {
			return nil, nil, wrapExec("permission get principals scan", name, err)
		}
		p.CanRead, p.CanWrite, p.CanDelete = canRead != 0, canWrite != 0, canDelete != 0
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, wrapExec("permission get principals rows", name, err)
	}
	return t, out, nil
}

func (s *permissionStore) DeleteTarget(ctx context.Context, name string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapExec("permission delete begin", name, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM permission_targets WHERE name = ?`, name); err != nil {
		return wrapExec("permission delete", name, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM permission_principals WHERE target_name = ?`, name); err != nil {
		return wrapExec("permission delete principals", name, err)
	}
	if err := tx.Commit(); err != nil {
		return wrapExec("permission delete commit", name, err)
	}
	return nil
}

func (s *permissionStore) ListTargets(ctx context.Context) ([]*PermissionTarget, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, repos, includes, excludes, created_at, updated_at FROM permission_targets ORDER BY name`)
	if err != nil {
		return nil, wrapExec("permission list", "", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*PermissionTarget
	for rows.Next() {
		t := &PermissionTarget{}
		if err := rows.Scan(&t.Name, &t.Repos, &t.Includes, &t.Excludes, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, wrapExec("permission list scan", "", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("permission list rows", "", err)
	}
	return out, nil
}

func (s *permissionStore) PrincipalsFor(ctx context.Context, repoKey string) ([]*PermissionPrincipal, error) {
	// repos is a JSON array of repo keys; the LIKE match over the quoted key
	// is exact enough for M1 (keys cannot contain quotes or JSON specials).
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id, p.target_name, p.principal, p.principal_type, p.can_read, p.can_write, p.can_delete
		FROM permission_principals p
		JOIN permission_targets t ON t.name = p.target_name
		WHERE t.repos LIKE ? ESCAPE '\'
		ORDER BY p.target_name, p.principal`,
		`%"`+escapeLike(repoKey)+`"%`)
	if err != nil {
		return nil, wrapExec("permission principals-for", repoKey, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*PermissionPrincipal
	for rows.Next() {
		p := &PermissionPrincipal{}
		var canRead, canWrite, canDelete int
		if err := rows.Scan(&p.ID, &p.TargetName, &p.Principal, &p.PrincipalType, &canRead, &canWrite, &canDelete); err != nil {
			return nil, wrapExec("permission principals-for scan", repoKey, err)
		}
		p.CanRead, p.CanWrite, p.CanDelete = canRead != 0, canWrite != 0, canDelete != 0
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("permission principals-for rows", repoKey, err)
	}
	return out, nil
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// GroupReferences returns the names of every permission target carrying a
// group principal row for the named group (T-97, SE-04's delete guard),
// ordered by target name. Only principal_type='group' rows match: a user
// row spelling the same name references no group.
func (s *permissionStore) GroupReferences(ctx context.Context, group string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.name FROM permission_targets t
		JOIN permission_principals p ON p.target_name = t.name
		WHERE p.principal_type = 'group' AND p.principal = ?
		ORDER BY t.name`, group)
	if err != nil {
		return nil, wrapExec("permission group-references", group, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, wrapExec("permission group-references scan", group, err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("permission group-references rows", group, err)
	}
	return out, nil
}

// ---- AuditStore ----

type auditStore struct{ db *sql.DB }

func (s *auditStore) Append(ctx context.Context, e *AuditEvent) error {
	const stmt = `INSERT INTO audit_events (time, actor, action, repo_key, path, detail)
		VALUES (?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, stmt, e.Time, e.Actor, e.Action, e.RepoKey, e.Path, e.Detail); err != nil {
		return wrapExec("audit append", e.Action, err)
	}
	return nil
}

func (s *auditStore) List(ctx context.Context, repoKey, actor string, limit int) ([]*AuditEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id, time, actor, action, repo_key, path, detail FROM audit_events`
	var conds []string
	var args []any
	if repoKey != "" {
		conds = append(conds, "repo_key = ?")
		args = append(args, repoKey)
	}
	if actor != "" {
		conds = append(conds, "actor = ?")
		args = append(args, actor)
	}
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ") //nolint:gosec // conds are static literals, values are bound args
	}
	q += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, wrapExec("audit list", "", err)
	}
	return scanAuditEvents(rows, "audit list")
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
