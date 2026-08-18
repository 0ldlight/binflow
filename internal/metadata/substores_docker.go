package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ---- DockerStore (schema 002_docker, architecture section 6 final DDL) ----

type dockerStore struct{ db *sql.DB }

var _ DockerStore = (*dockerStore)(nil)

func (s *dockerStore) PutManifest(ctx context.Context, m *DockerManifest) error {
	const stmt = `INSERT INTO docker_manifests
		(repo_key, image, digest, media_type, size, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (repo_key, image, digest) DO UPDATE SET
			media_type = excluded.media_type,
			size = excluded.size,
			created_by = excluded.created_by,
			created_at = excluded.created_at`
	if _, err := s.db.ExecContext(ctx, stmt,
		m.RepoKey, m.Image, m.Digest, m.MediaType, m.Size, m.CreatedBy, m.CreatedAt); err != nil {
		return wrapExec("docker manifests put", m.RepoKey+"/"+m.Image, err)
	}
	return nil
}

func (s *dockerStore) GetManifest(ctx context.Context, repoKey, image, digest string) (*DockerManifest, error) {
	const stmt = `SELECT repo_key, image, digest, media_type, size, created_by, created_at
		FROM docker_manifests WHERE repo_key = ? AND image = ? AND digest = ?`
	row := s.db.QueryRowContext(ctx, stmt, repoKey, image, digest)
	m := &DockerManifest{}
	err := row.Scan(&m.RepoKey, &m.Image, &m.Digest, &m.MediaType, &m.Size, &m.CreatedBy, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("docker manifests get %s/%s/%s: %w", repoKey, image, digest, ErrManifestNotFound)
	}
	if err != nil {
		return nil, wrapExec("docker manifests get", repoKey+"/"+image, err)
	}
	return m, nil
}

// DeleteManifest removes the manifest row and cascades its tags and refs in
// one transaction (architecture 11.12: no DB-level FK backs those edges, so
// the store owns the same-transaction cascade).
func (s *dockerStore) DeleteManifest(ctx context.Context, repoKey, image, digest string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapExec("docker manifests delete begin", repoKey+"/"+image, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	res, err := tx.ExecContext(ctx,
		`DELETE FROM docker_manifests WHERE repo_key = ? AND image = ? AND digest = ?`,
		repoKey, image, digest)
	if err != nil {
		return wrapExec("docker manifests delete", repoKey+"/"+image, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return wrapExec("docker manifests delete rows", repoKey+"/"+image, err)
	}
	if n == 0 {
		return fmt.Errorf("docker manifests delete %s/%s/%s: %w", repoKey, image, digest, ErrManifestNotFound)
	}
	for _, stmt := range []string{
		`DELETE FROM docker_tags WHERE repo_key = ? AND image = ? AND digest = ?`,
		`DELETE FROM docker_refs WHERE repo_key = ? AND image = ? AND manifest_digest = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, repoKey, image, digest); err != nil {
			return wrapExec("docker manifests delete cascade", repoKey+"/"+image, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return wrapExec("docker manifests delete commit", repoKey+"/"+image, err)
	}
	return nil
}

func (s *dockerStore) ListManifestsByImage(ctx context.Context, repoKey, image string) ([]*DockerManifest, error) {
	const stmt = `SELECT repo_key, image, digest, media_type, size, created_by, created_at
		FROM docker_manifests WHERE repo_key = ? AND image = ? ORDER BY digest`
	rows, err := s.db.QueryContext(ctx, stmt, repoKey, image)
	if err != nil {
		return nil, wrapExec("docker manifests list", repoKey+"/"+image, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*DockerManifest
	for rows.Next() {
		m := &DockerManifest{}
		if err := rows.Scan(&m.RepoKey, &m.Image, &m.Digest, &m.MediaType, &m.Size, &m.CreatedBy, &m.CreatedAt); err != nil {
			return nil, wrapExec("docker manifests list scan", repoKey+"/"+image, err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("docker manifests list rows", repoKey+"/"+image, err)
	}
	return out, nil
}

// ---- tags ----

func (s *dockerStore) PutTag(ctx context.Context, t *DockerTag) error {
	const stmt = `INSERT INTO docker_tags
		(repo_key, image, tag, digest, updated_by, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (repo_key, image, tag) DO UPDATE SET
			digest = excluded.digest,
			updated_by = excluded.updated_by,
			updated_at = excluded.updated_at`
	if _, err := s.db.ExecContext(ctx, stmt,
		t.RepoKey, t.Image, t.Tag, t.Digest, t.UpdatedBy, t.UpdatedAt); err != nil {
		return wrapExec("docker tags put", t.RepoKey+"/"+t.Image, err)
	}
	return nil
}

func (s *dockerStore) GetTag(ctx context.Context, repoKey, image, tag string) (*DockerTag, error) {
	const stmt = `SELECT repo_key, image, tag, digest, updated_by, updated_at
		FROM docker_tags WHERE repo_key = ? AND image = ? AND tag = ?`
	row := s.db.QueryRowContext(ctx, stmt, repoKey, image, tag)
	t := &DockerTag{}
	err := row.Scan(&t.RepoKey, &t.Image, &t.Tag, &t.Digest, &t.UpdatedBy, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("docker tags get %s/%s:%s: %w", repoKey, image, tag, ErrTagNotFound)
	}
	if err != nil {
		return nil, wrapExec("docker tags get", repoKey+"/"+image, err)
	}
	return t, nil
}

func (s *dockerStore) DeleteTag(ctx context.Context, repoKey, image, tag string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM docker_tags WHERE repo_key = ? AND image = ? AND tag = ?`,
		repoKey, image, tag)
	if err != nil {
		return wrapExec("docker tags delete", repoKey+"/"+image, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("docker tags delete rows", repoKey+"/"+image, err)
	} else if n == 0 {
		return fmt.Errorf("docker tags delete %s/%s:%s: %w", repoKey, image, tag, ErrTagNotFound)
	}
	return nil
}

func (s *dockerStore) ListTagsByImage(ctx context.Context, repoKey, image string) ([]*DockerTag, error) {
	const stmt = `SELECT repo_key, image, tag, digest, updated_by, updated_at
		FROM docker_tags WHERE repo_key = ? AND image = ? ORDER BY tag`
	rows, err := s.db.QueryContext(ctx, stmt, repoKey, image)
	if err != nil {
		return nil, wrapExec("docker tags list", repoKey+"/"+image, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*DockerTag
	for rows.Next() {
		t := &DockerTag{}
		if err := rows.Scan(&t.RepoKey, &t.Image, &t.Tag, &t.Digest, &t.UpdatedBy, &t.UpdatedAt); err != nil {
			return nil, wrapExec("docker tags list scan", repoKey+"/"+image, err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("docker tags list rows", repoKey+"/"+image, err)
	}
	return out, nil
}

// ---- refs ----

// PutRefs replaces the ref set of one manifest atomically. The DELETE +
// batched INSERT pair runs in a single transaction: a failure midway leaves
// the previous ledger intact. The insert is conflict-tolerant on purpose
// (T-39 review B1): the table keys one row per REFERENCED BLOB (the DDL's
// own comment), so a caller handing a set with duplicate blob digests
// collapses onto one row instead of tripping the PK and 500ing a legal
// publish — the adapter dedups too; this is the second layer.
func (s *dockerStore) PutRefs(ctx context.Context, repoKey, image, manifestDigest string, refs []*DockerRef) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapExec("docker refs put begin", repoKey+"/"+image, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM docker_refs WHERE repo_key = ? AND image = ? AND manifest_digest = ?`,
		repoKey, image, manifestDigest); err != nil {
		return wrapExec("docker refs put clear", repoKey+"/"+image, err)
	}
	const insert = `INSERT OR IGNORE INTO docker_refs
		(repo_key, image, manifest_digest, blob_digest, child_media_type)
		VALUES (?, ?, ?, ?, ?)`
	for _, r := range refs {
		if _, err := tx.ExecContext(ctx, insert,
			repoKey, image, manifestDigest, r.BlobDigest, r.ChildMediaType); err != nil {
			return wrapExec("docker refs put row "+r.BlobDigest, repoKey+"/"+image, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return wrapExec("docker refs put commit", repoKey+"/"+image, err)
	}
	return nil
}

func (s *dockerStore) ListRefsByManifest(ctx context.Context, repoKey, image, manifestDigest string) ([]*DockerRef, error) {
	const stmt = `SELECT repo_key, image, manifest_digest, blob_digest, child_media_type
		FROM docker_refs WHERE repo_key = ? AND image = ? AND manifest_digest = ?
		ORDER BY blob_digest`
	rows, err := s.db.QueryContext(ctx, stmt, repoKey, image, manifestDigest)
	if err != nil {
		return nil, wrapExec("docker refs list", repoKey+"/"+image, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*DockerRef
	for rows.Next() {
		r := &DockerRef{}
		if err := rows.Scan(&r.RepoKey, &r.Image, &r.ManifestDigest, &r.BlobDigest, &r.ChildMediaType); err != nil {
			return nil, wrapExec("docker refs list scan", repoKey+"/"+image, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("docker refs list rows", repoKey+"/"+image, err)
	}
	return out, nil
}

func (s *dockerStore) RefsByBlob(ctx context.Context, repoKey, blobDigest string) (bool, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM docker_refs WHERE repo_key = ? AND blob_digest = ?)`,
		repoKey, blobDigest).Scan(&exists); err != nil {
		return false, wrapExec("docker refs by-blob", repoKey, err)
	}
	return exists, nil
}

// ---- catalog / teardown ----

// ListImages is the /v2/_catalog source: DISTINCT image names with at least
// one manifest row. Pages are keyed on an exclusive after-cursor instead of
// OFFSET so catalog results stay stable while clients page concurrently.
func (s *dockerStore) ListImages(ctx context.Context, repoKey, after string, limit int) ([]string, error) {
	const stmt = `SELECT DISTINCT image FROM docker_manifests
		WHERE repo_key = ? AND image > ? ORDER BY image LIMIT ?`
	rows, err := s.db.QueryContext(ctx, stmt, repoKey, after, limitOrDefault(limit))
	if err != nil {
		return nil, wrapExec("docker images list", repoKey, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var image string
		if err := rows.Scan(&image); err != nil {
			return nil, wrapExec("docker images list scan", repoKey, err)
		}
		out = append(out, image)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("docker images list rows", repoKey, err)
	}
	return out, nil
}

// limitOrDefault maps limit<=0 to -1 (SQL "no limit").
func limitOrDefault(limit int) int {
	if limit <= 0 {
		return -1
	}
	return limit
}

// DeleteImage drops every index row of one image in a single transaction:
// the three tables must move together or the image is left half-deleted
// (which the registry surface would then serve as ghost tags).
func (s *dockerStore) DeleteImage(ctx context.Context, repoKey, image string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, wrapExec("docker image delete begin", repoKey+"/"+image, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	var manifests int64
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM docker_tags WHERE repo_key = ? AND image = ?`, repoKey, image); err != nil {
		return 0, wrapExec("docker image delete tags", repoKey+"/"+image, err)
	}
	if err != nil {
		return 0, wrapExec("docker image delete tags", repoKey+"/"+image, err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM docker_refs WHERE repo_key = ? AND image = ?`, repoKey, image); err != nil {
		return 0, wrapExec("docker image delete refs", repoKey+"/"+image, err)
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM docker_manifests WHERE repo_key = ? AND image = ?`, repoKey, image)
	if err != nil {
		return 0, wrapExec("docker image delete manifests", repoKey+"/"+image, err)
	}
	if manifests, err = res.RowsAffected(); err != nil {
		return 0, wrapExec("docker image delete rows", repoKey+"/"+image, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, wrapExec("docker image delete commit", repoKey+"/"+image, err)
	}
	return manifests, nil
}

// DeleteRepoRefs exists because docker_refs carries no FK to repositories
// (architecture 11.12): the repository teardown path must call it before the
// repositories row disappears, or ref rows linger as un-collectable garbage.
func (s *dockerStore) DeleteRepoRefs(ctx context.Context, repoKey string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM docker_refs WHERE repo_key = ?`, repoKey)
	if err != nil {
		return 0, wrapExec("docker repo refs delete", repoKey, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, wrapExec("docker repo refs delete rows", repoKey, err)
	}
	return n, nil
}
