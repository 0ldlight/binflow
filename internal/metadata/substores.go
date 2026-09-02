package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ---- RepoStore ----

type repoStore struct{ db *sql.DB }

func (s *repoStore) Create(ctx context.Context, r *Repo) error {
	const stmt = `INSERT INTO repositories
		(repo_key, type, package_type, description, config, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, stmt,
		r.RepoKey, r.Type, r.PackageType, r.Description, r.Config, r.CreatedAt, r.UpdatedAt); err != nil {
		return wrapExec("repositories create", r.RepoKey, err)
	}
	return nil
}

func (s *repoStore) Update(ctx context.Context, r *Repo) error {
	const stmt = `UPDATE repositories
		SET type = ?, package_type = ?, description = ?, config = ?, updated_at = ?
		WHERE repo_key = ?`
	res, err := s.db.ExecContext(ctx, stmt,
		r.Type, r.PackageType, r.Description, r.Config, r.UpdatedAt, r.RepoKey)
	if err != nil {
		return wrapExec("repositories update", r.RepoKey, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("repositories update rows", r.RepoKey, err)
	} else if n == 0 {
		return fmt.Errorf("repositories update %s: %w", r.RepoKey, ErrRepoNotFound)
	}
	return nil
}

func (s *repoStore) Get(ctx context.Context, repoKey string) (*Repo, error) {
	const stmt = `SELECT repo_key, type, package_type, description, config, created_at, updated_at
		FROM repositories WHERE repo_key = ?`
	row := s.db.QueryRowContext(ctx, stmt, repoKey)
	r := &Repo{}
	err := row.Scan(&r.RepoKey, &r.Type, &r.PackageType, &r.Description, &r.Config, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("repositories get %s: %w", repoKey, ErrRepoNotFound)
	}
	if err != nil {
		return nil, wrapExec("repositories get", repoKey, err)
	}
	return r, nil
}

func (s *repoStore) Delete(ctx context.Context, repoKey string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM repositories WHERE repo_key = ?`, repoKey)
	if err != nil {
		return wrapExec("repositories delete", repoKey, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("repositories delete rows", repoKey, err)
	} else if n == 0 {
		return fmt.Errorf("repositories delete %s: %w", repoKey, ErrRepoNotFound)
	}
	return nil
}

func (s *repoStore) List(ctx context.Context) ([]*Repo, error) {
	const stmt = `SELECT repo_key, type, package_type, description, config, created_at, updated_at
		FROM repositories ORDER BY repo_key`
	rows, err := s.db.QueryContext(ctx, stmt)
	if err != nil {
		return nil, wrapExec("repositories list", "", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*Repo
	for rows.Next() {
		r := &Repo{}
		if err := rows.Scan(&r.RepoKey, &r.Type, &r.PackageType, &r.Description, &r.Config, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, wrapExec("repositories list scan", "", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("repositories list rows", "", err)
	}
	return out, nil
}

// ---- NodeStore ----

type nodeStore struct{ db *sql.DB }

func (s *nodeStore) Get(ctx context.Context, repoKey, path string) (*Node, error) {
	const stmt = `SELECT repo_key, path, sha256, size, mime, created_by, created_at, updated_at
		FROM nodes WHERE repo_key = ? AND path = ?`
	row := s.db.QueryRowContext(ctx, stmt, repoKey, path)
	n := &Node{}
	err := row.Scan(&n.RepoKey, &n.Path, &n.Sha256, &n.Size, &n.Mime, &n.CreatedBy, &n.CreatedAt, &n.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("nodes get %s/%s: %w", repoKey, path, ErrNodeNotFound)
	}
	if err != nil {
		return nil, wrapExec("nodes get", repoKey, err)
	}
	return n, nil
}

func (s *nodeStore) Put(ctx context.Context, n *Node) error {
	const stmt = `INSERT INTO nodes
		(repo_key, path, sha256, size, mime, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (repo_key, path) DO UPDATE SET
			sha256 = excluded.sha256,
			size = excluded.size,
			mime = excluded.mime,
			created_by = excluded.created_by,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at`
	if _, err := s.db.ExecContext(ctx, stmt,
		n.RepoKey, n.Path, n.Sha256, n.Size, n.Mime, n.CreatedBy, n.CreatedAt, n.UpdatedAt); err != nil {
		return wrapExec("nodes put", n.RepoKey, err)
	}
	return nil
}

func (s *nodeStore) Delete(ctx context.Context, repoKey, path string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE repo_key = ? AND path = ?`, repoKey, path)
	if err != nil {
		return wrapExec("nodes delete", repoKey, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("nodes delete rows", repoKey, err)
	} else if n == 0 {
		return fmt.Errorf("nodes delete %s/%s: %w", repoKey, path, ErrNodeNotFound)
	}
	return nil
}

func (s *nodeStore) DeleteByPrefix(ctx context.Context, repoKey, prefix string) (int64, error) {
	exact, subtree := likePrefix(prefix)
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM nodes WHERE repo_key = ? AND (path = ? OR path LIKE ? ESCAPE '\')`,
		repoKey, exact, subtree)
	if err != nil {
		return 0, wrapExec("nodes delete-by-prefix", repoKey, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, wrapExec("nodes delete-by-prefix rows", repoKey, err)
	}
	return n, nil
}

func (s *nodeStore) ListByPrefix(ctx context.Context, repoKey, prefix string) ([]*Node, error) {
	const stmt = `SELECT repo_key, path, sha256, size, mime, created_by, created_at, updated_at
		FROM nodes WHERE repo_key = ? AND (path = ? OR path LIKE ? ESCAPE '\') ORDER BY path`
	exact, subtree := likePrefix(prefix)
	rows, err := s.db.QueryContext(ctx, stmt, repoKey, exact, subtree)
	if err != nil {
		return nil, wrapExec("nodes list-by-prefix", repoKey, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*Node
	for rows.Next() {
		n := &Node{}
		if err := rows.Scan(&n.RepoKey, &n.Path, &n.Sha256, &n.Size, &n.Mime, &n.CreatedBy, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, wrapExec("nodes list-by-prefix scan", repoKey, err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("nodes list-by-prefix rows", repoKey, err)
	}
	return out, nil
}

// likePrefix builds a pair of LIKE patterns matching the prefix itself and
// every path under it: prefix "a" matches "a" and "a/..." but not "ab".
// The first return is the exact (escaped) path, the second the subtree
// pattern. LIKE wildcards inside the prefix are escaped so they match
// literally.
func likePrefix(prefix string) (exact, subtree string) {
	prefix = strings.TrimPrefix(prefix, "/")
	if prefix == "" {
		return "", "%"
	}
	repl := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return repl.Replace(prefix), repl.Replace(prefix) + "/%"
}

// CountDownload implements NodeStore.CountDownload (M16 T-438, ADR-0044
// K69): ONE statement, self-incrementing on both counters so concurrent
// landings never lose an increment. The folder-marker exclusion keeps
// folder rows at their structural zero (a folder carries no body, so no
// download face can land on it — the guard lives at the SQL edge, not at
// every caller's); a missing row updates nothing and reports no error.
func (s *nodeStore) CountDownload(ctx context.Context, repoKey, path, by, at string, remoteServed bool) error {
	remoteDelta := int64(0)
	if remoteServed {
		remoteDelta = 1
	}
	const stmt = `UPDATE nodes
		SET download_count = download_count + 1,
		    last_downloaded_at = ?,
		    last_downloaded_by = ?,
		    remote_download_count = remote_download_count + ?
		WHERE repo_key = ? AND path = ? AND sha256 <> ?`
	if _, err := s.db.ExecContext(ctx, stmt,
		at, by, remoteDelta, repoKey, path, FolderMarkerSHA); err != nil {
		return wrapExec("nodes count-download", repoKey, err)
	}
	return nil
}

// Stats implements NodeStore.Stats: the four counting columns of one node
// row, ErrNodeNotFound when absent.
func (s *nodeStore) Stats(ctx context.Context, repoKey, path string) (*NodeStats, error) {
	const stmt = `SELECT repo_key, path, download_count, last_downloaded_at, last_downloaded_by, remote_download_count
		FROM nodes WHERE repo_key = ? AND path = ?`
	row := s.db.QueryRowContext(ctx, stmt, repoKey, path)
	st := &NodeStats{}
	err := row.Scan(&st.RepoKey, &st.Path, &st.DownloadCount, &st.LastDownloadedAt, &st.LastDownloadedBy, &st.RemoteDownloadCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("nodes stats %s/%s: %w", repoKey, path, ErrNodeNotFound)
	}
	if err != nil {
		return nil, wrapExec("nodes stats", repoKey, err)
	}
	return st, nil
}

// ---- BlobStore ----

type blobStore struct{ db *sql.DB }

func (s *blobStore) Put(ctx context.Context, b *Blob) error {
	const stmt = `INSERT INTO blobs (sha256, sha1, md5, size, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (sha256) DO NOTHING`
	if _, err := s.db.ExecContext(ctx, stmt, b.Sha256, b.Sha1, b.Md5, b.Size, b.CreatedAt); err != nil {
		return wrapExec("blobs put", b.Sha256, err)
	}
	return nil
}

func (s *blobStore) Get(ctx context.Context, sha256 string) (*Blob, error) {
	const stmt = `SELECT sha256, sha1, md5, size, created_at FROM blobs WHERE sha256 = ?`
	row := s.db.QueryRowContext(ctx, stmt, sha256)
	b := &Blob{}
	err := row.Scan(&b.Sha256, &b.Sha1, &b.Md5, &b.Size, &b.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("blobs get %s: %w", sha256, ErrNotFound)
	}
	if err != nil {
		return nil, wrapExec("blobs get", sha256, err)
	}
	return b, nil
}

// GetBySha1 is the sha1-keyed twin of Get, backed by idx_blobs_sha1 (the
// T-62 seam T-73 consumes). LIMIT 1 keeps the answer deterministic should
// the column ever hold duplicates (see the interface comment). The empty
// string is refused outright: the shared folder-marker row carries an empty
// sha1, and a caller that failed its both-empty guard must not resolve it.
func (s *blobStore) GetBySha1(ctx context.Context, sha1 string) (*Blob, error) {
	if sha1 == "" {
		return nil, fmt.Errorf("blobs get by sha1: %w", ErrNotFound)
	}
	const stmt = `SELECT sha256, sha1, md5, size, created_at FROM blobs WHERE sha1 = ? LIMIT 1`
	row := s.db.QueryRowContext(ctx, stmt, sha1)
	b := &Blob{}
	err := row.Scan(&b.Sha256, &b.Sha1, &b.Md5, &b.Size, &b.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("blobs get by sha1 %s: %w", sha1, ErrNotFound)
	}
	if err != nil {
		return nil, wrapExec("blobs get by sha1", sha1, err)
	}
	return b, nil
}

func (s *blobStore) Delete(ctx context.Context, sha256 string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM blobs WHERE sha256 = ?`, sha256)
	if err != nil {
		return wrapExec("blobs delete", sha256, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("blobs delete rows", sha256, err)
	} else if n == 0 {
		return fmt.Errorf("blobs delete %s: %w", sha256, ErrNotFound)
	}
	return nil
}

func (s *blobStore) Count(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM blobs`).Scan(&n); err != nil {
		return 0, wrapExec("blobs count", "", err)
	}
	return n, nil
}

// FilterUnreferenced streams sha256 values of blobs that no node references.
// The GC mark phase consumes it while sweeping rows in batches, so pages are
// keyed on the last (created_at, sha256) pair rather than OFFSET — the sweep
// deletes rows between pages and OFFSET pagination would then skip blobs.
func (s *blobStore) FilterUnreferenced(ctx context.Context, pageSize int, fn func(sha256 string) error) error {
	if pageSize <= 0 {
		pageSize = 500
	}
	const stmt = `SELECT b.sha256, b.created_at FROM blobs b
		WHERE NOT EXISTS (SELECT 1 FROM nodes n WHERE n.sha256 = b.sha256)
		  AND (b.created_at > ? OR (b.created_at = ? AND b.sha256 > ?))
		ORDER BY b.created_at, b.sha256
		LIMIT ?`
	var lastCreatedAt, lastSha string
	for {
		rows, err := s.db.QueryContext(ctx, stmt, lastCreatedAt, lastCreatedAt, lastSha, pageSize)
		if err != nil {
			return wrapExec("blobs filter-unreferenced", "", err)
		}
		var batch []string
		for rows.Next() {
			var sha, createdAt string
			if err := rows.Scan(&sha, &createdAt); err != nil {
				_ = rows.Close()
				return wrapExec("blobs filter-unreferenced scan", "", err)
			}
			batch = append(batch, sha)
			lastCreatedAt, lastSha = createdAt, sha
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return wrapExec("blobs filter-unreferenced rows", "", err)
		}
		_ = rows.Close()
		if len(batch) == 0 {
			return nil
		}
		for _, sha := range batch {
			if err := fn(sha); err != nil {
				return err
			}
		}
		if len(batch) < pageSize {
			return nil
		}
	}
}

// wrapExec annotates a database error with the statement label and key.
func wrapExec(label, key string, err error) error {
	if err == nil {
		return nil
	}
	if isSQLiteBusy(err) {
		// Attach the retryable signal (T-54): busy-class contention that
		// outlived busy_timeout is transient by nature — upper layers must
		// answer 503-retry, not 500-broken. The driver error stays wrapped
		// for the log line.
		if key == "" {
			return fmt.Errorf("metadata: %s: %w: %w", label, ErrStoreBusy, err)
		}
		return fmt.Errorf("metadata: %s (%s): %w: %w", label, key, ErrStoreBusy, err)
	}
	if key == "" {
		return fmt.Errorf("metadata: %s: %w", label, err)
	}
	return fmt.Errorf("metadata: %s (%s): %w", label, key, err)
}
