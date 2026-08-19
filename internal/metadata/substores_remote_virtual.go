package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ---- RemoteStore (schema 001 remote_configs widened by 003, remote_cache
// new in 003; architecture section 6 final DDL) ----

type remoteStore struct{ db *sql.DB }

var _ RemoteStore = (*remoteStore)(nil)

func (s *remoteStore) CreateConfig(ctx context.Context, c *RemoteConfig) error {
	const stmt = `INSERT INTO remote_configs
		(repo_key, url, username, password, content_ttl_seconds, metadata_ttl_seconds, allow_private_upstream, blocked_out)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, stmt,
		c.RepoKey, c.URL, c.Username, c.Password,
		c.ContentTTLSeconds, c.MetadataTTLSeconds,
		boolToInt(c.AllowPrivateUpstream), boolToInt(c.BlockedOut)); err != nil {
		return wrapExec("remote configs create", c.RepoKey, err)
	}
	return nil
}

func (s *remoteStore) UpdateConfig(ctx context.Context, c *RemoteConfig) error {
	const stmt = `UPDATE remote_configs
		SET url = ?, username = ?, password = ?,
		    content_ttl_seconds = ?, metadata_ttl_seconds = ?,
		    allow_private_upstream = ?, blocked_out = ?
		WHERE repo_key = ?`
	res, err := s.db.ExecContext(ctx, stmt,
		c.URL, c.Username, c.Password,
		c.ContentTTLSeconds, c.MetadataTTLSeconds,
		boolToInt(c.AllowPrivateUpstream), boolToInt(c.BlockedOut), c.RepoKey)
	if err != nil {
		return wrapExec("remote configs update", c.RepoKey, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("remote configs update rows", c.RepoKey, err)
	} else if n == 0 {
		return fmt.Errorf("remote configs update %s: %w", c.RepoKey, ErrRemoteConfigNotFound)
	}
	return nil
}

func (s *remoteStore) GetConfig(ctx context.Context, repoKey string) (*RemoteConfig, error) {
	const stmt = `SELECT repo_key, url, username, password,
			content_ttl_seconds, metadata_ttl_seconds, allow_private_upstream, blocked_out
		FROM remote_configs WHERE repo_key = ?`
	row := s.db.QueryRowContext(ctx, stmt, repoKey)
	c := &RemoteConfig{}
	var allowPrivate, blockedOut int
	err := row.Scan(&c.RepoKey, &c.URL, &c.Username, &c.Password,
		&c.ContentTTLSeconds, &c.MetadataTTLSeconds, &allowPrivate, &blockedOut)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("remote configs get %s: %w", repoKey, ErrRemoteConfigNotFound)
	}
	if err != nil {
		return nil, wrapExec("remote configs get", repoKey, err)
	}
	c.AllowPrivateUpstream = allowPrivate != 0
	c.BlockedOut = blockedOut != 0
	return c, nil
}

func (s *remoteStore) DeleteConfig(ctx context.Context, repoKey string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM remote_configs WHERE repo_key = ?`, repoKey)
	if err != nil {
		return wrapExec("remote configs delete", repoKey, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("remote configs delete rows", repoKey, err)
	} else if n == 0 {
		return fmt.Errorf("remote configs delete %s: %w", repoKey, ErrRemoteConfigNotFound)
	}
	return nil
}

// ---- remote_cache ----

func (s *remoteStore) PutCache(ctx context.Context, e *RemoteCacheEntry) error {
	const stmt = `INSERT INTO remote_cache
		(repo_key, path, etag, last_modified, fetched_at, expires_at, kind)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (repo_key, path) DO UPDATE SET
			etag = excluded.etag,
			last_modified = excluded.last_modified,
			fetched_at = excluded.fetched_at,
			expires_at = excluded.expires_at,
			kind = excluded.kind`
	if _, err := s.db.ExecContext(ctx, stmt,
		e.RepoKey, e.Path, e.ETag, e.LastModified, e.FetchedAt, e.ExpiresAt, e.Kind); err != nil {
		return wrapExec("remote cache put", e.RepoKey+"/"+e.Path, err)
	}
	return nil
}

func (s *remoteStore) GetCache(ctx context.Context, repoKey, path string) (*RemoteCacheEntry, error) {
	const stmt = `SELECT repo_key, path, etag, last_modified, fetched_at, expires_at, kind
		FROM remote_cache WHERE repo_key = ? AND path = ?`
	row := s.db.QueryRowContext(ctx, stmt, repoKey, path)
	e := &RemoteCacheEntry{}
	err := row.Scan(&e.RepoKey, &e.Path, &e.ETag, &e.LastModified, &e.FetchedAt, &e.ExpiresAt, &e.Kind)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("remote cache get %s/%s: %w", repoKey, path, ErrRemoteCacheNotFound)
	}
	if err != nil {
		return nil, wrapExec("remote cache get", repoKey+"/"+path, err)
	}
	return e, nil
}

func (s *remoteStore) DeleteCache(ctx context.Context, repoKey, path string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM remote_cache WHERE repo_key = ? AND path = ?`, repoKey, path)
	if err != nil {
		return wrapExec("remote cache delete", repoKey+"/"+path, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("remote cache delete rows", repoKey+"/"+path, err)
	} else if n == 0 {
		return fmt.Errorf("remote cache delete %s/%s: %w", repoKey, path, ErrRemoteCacheNotFound)
	}
	return nil
}

func (s *remoteStore) DeleteCacheByRepo(ctx context.Context, repoKey string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM remote_cache WHERE repo_key = ?`, repoKey)
	if err != nil {
		return 0, wrapExec("remote cache delete-by-repo", repoKey, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, wrapExec("remote cache delete-by-repo rows", repoKey, err)
	}
	return n, nil
}

func (s *remoteStore) ListExpiredCache(ctx context.Context, now string, limit int) ([]*RemoteCacheEntry, error) {
	const stmt = `SELECT repo_key, path, etag, last_modified, fetched_at, expires_at, kind
		FROM remote_cache WHERE expires_at <= ?
		ORDER BY expires_at, repo_key, path LIMIT ?`
	rows, err := s.db.QueryContext(ctx, stmt, now, limitOrDefault(limit))
	if err != nil {
		return nil, wrapExec("remote cache expired", "", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*RemoteCacheEntry
	for rows.Next() {
		e := &RemoteCacheEntry{}
		if err := rows.Scan(&e.RepoKey, &e.Path, &e.ETag, &e.LastModified, &e.FetchedAt, &e.ExpiresAt, &e.Kind); err != nil {
			return nil, wrapExec("remote cache expired scan", "", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("remote cache expired rows", "", err)
	}
	return out, nil
}

// ---- VirtualStore (virtual_members, 001 table; ADR-0013 position
// semantics) ----

type virtualStore struct{ db *sql.DB }

var _ VirtualStore = (*virtualStore)(nil)

// SetMembers replaces the member list atomically: the DELETE + positional
// INSERT batch runs in one transaction, so readers never observe a virtual
// repo with half of its old and half of its new members (T-71 computes the
// resolution order per request — it must always see one coherent list).
func (s *virtualStore) SetMembers(ctx context.Context, virtualRepo string, members []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapExec("virtual members set begin", virtualRepo, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM virtual_members WHERE virtual_repo = ?`, virtualRepo); err != nil {
		return wrapExec("virtual members set clear", virtualRepo, err)
	}
	const insert = `INSERT INTO virtual_members (virtual_repo, member_repo, position)
		VALUES (?, ?, ?)`
	for i, m := range members {
		if _, err := tx.ExecContext(ctx, insert, virtualRepo, m, i); err != nil {
			return wrapExec("virtual members set row "+m, virtualRepo, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return wrapExec("virtual members set commit", virtualRepo, err)
	}
	return nil
}

func (s *virtualStore) ListMembers(ctx context.Context, virtualRepo string) ([]*VirtualMember, error) {
	const stmt = `SELECT virtual_repo, member_repo, position
		FROM virtual_members WHERE virtual_repo = ?
		ORDER BY position, member_repo`
	rows, err := s.db.QueryContext(ctx, stmt, virtualRepo)
	if err != nil {
		return nil, wrapExec("virtual members list", virtualRepo, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*VirtualMember
	for rows.Next() {
		m := &VirtualMember{}
		if err := rows.Scan(&m.VirtualRepo, &m.MemberRepo, &m.Position); err != nil {
			return nil, wrapExec("virtual members list scan", virtualRepo, err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("virtual members list rows", virtualRepo, err)
	}
	return out, nil
}
