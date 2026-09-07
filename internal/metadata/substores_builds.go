package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// buildStore implements BuildStore over the 024 build-info table family
// (M17 T-507, ADR-0045 decision 2 + Errata). A dumb ledger in the
// scheduleStore tradition: rows round-trip as text/ints, the only
// interpretation is the four-tuple addressing (empty repo normalizes to
// DefaultBuildRepo; empty started on GetBuild resolves the latest run).
// The original JSON document lives in builds.payload; nothing here touches
// storage/nodes except the REAL build_artifacts association, whose NULL
// shape is the record-only artifact (ADR-0045 axis 2-A).
type buildStore struct{ db *sql.DB }

// buildCoordsWhere is the four-tuple predicate every run-addressed
// statement shares.
const buildCoordsWhere = `build_name = ? AND build_number = ? AND started = ? AND build_repo = ?`

// buildSelectCols is the builds header projection.
const buildSelectCols = `build_name, build_number, started, build_repo, build_type, payload,
	created_by, created_at, updated_by, updated_at`

// normalizeBuildRepo maps ” onto the default logical key; every other
// spelling passes through untouched (custom build repos are caller data).
func normalizeBuildRepo(repo string) string {
	if repo == "" {
		return DefaultBuildRepo
	}
	return repo
}

// buildUpsertStmt is PutBuild: an upsert by the four-tuple that keeps the
// ORIGINAL created_at/created_by on conflict (the PUT overwrite face is a
// re-publication of one run's header, never a re-creation).
const buildUpsertStmt = `INSERT INTO builds
	(build_name, build_number, started, build_repo, build_type, payload,
	 created_by, created_at, updated_by, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT (build_name, build_number, started, build_repo) DO UPDATE SET
		build_type = excluded.build_type,
		payload    = excluded.payload,
		updated_by = excluded.updated_by,
		updated_at = excluded.updated_at`

// PutBuild implements BuildStore.PutBuild.
func (s *buildStore) PutBuild(ctx context.Context, b *Build) error {
	repo := normalizeBuildRepo(b.Repo)
	if _, err := s.db.ExecContext(ctx, buildUpsertStmt,
		b.Name, b.Number, b.Started, repo, b.Type, b.Payload,
		b.CreatedBy, b.CreatedAt, b.UpdatedBy, b.UpdatedAt); err != nil {
		return wrapExec("builds put", b.Name+"#"+b.Number, err)
	}
	return nil
}

// scanBuild reads one header row.
func scanBuild(scan func(...any) error) (*Build, error) {
	b := &Build{}
	if err := scan(&b.Name, &b.Number, &b.Started, &b.Repo, &b.Type, &b.Payload,
		&b.CreatedBy, &b.CreatedAt, &b.UpdatedBy, &b.UpdatedAt); err != nil {
		return nil, err
	}
	return b, nil
}

// GetBuild implements BuildStore.GetBuild: the exact four-tuple row, or —
// when started is empty — the LATEST run of (name, number, repo) by
// started DESC (the single-build GET face's default ruling).
func (s *buildStore) GetBuild(ctx context.Context, name, number, started, repo string) (*Build, error) {
	repo = normalizeBuildRepo(repo)
	stmt := `SELECT ` + buildSelectCols + ` FROM builds
		WHERE build_name = ? AND build_number = ? AND build_repo = ?`
	args := []any{name, number, repo}
	if started == "" {
		stmt += ` ORDER BY started DESC LIMIT 1`
	} else {
		stmt += ` AND started = ?`
		args = append(args, started)
	}
	row := s.db.QueryRowContext(ctx, stmt, args...)
	b, err := scanBuild(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("builds get %s#%s: %w", name, number, ErrBuildNotFound)
	}
	if err != nil {
		return nil, wrapExec("builds get", name+"#"+number, err)
	}
	return b, nil
}

// DeleteBuild implements BuildStore.DeleteBuild: the exact run leaves with
// its whole segment (the ON DELETE CASCADE chain modules -> artifacts/
// dependencies, plus properties and promotions).
func (s *buildStore) DeleteBuild(ctx context.Context, name, number, started, repo string) error {
	repo = normalizeBuildRepo(repo)
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM builds WHERE `+buildCoordsWhere, name, number, started, repo)
	if err != nil {
		return wrapExec("builds delete", name+"#"+number, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapExec("builds delete rows", name+"#"+number, err)
	} else if n == 0 {
		return fmt.Errorf("builds delete %s#%s: %w", name, number, ErrBuildNotFound)
	}
	return nil
}

// ListBuildNames implements BuildStore.ListBuildNames: one row per
// (build_name, build_repo) with the group's MAX(started) — the names
// face's source. repo = ” spans every build_repo (the ACL visible-set
// walk reads that shape).
func (s *buildStore) ListBuildNames(ctx context.Context, repo string) ([]*BuildName, error) {
	stmt := `SELECT build_name, build_repo, MAX(started) FROM builds`
	var args []any
	if repo != "" {
		stmt += ` WHERE build_repo = ?`
		args = append(args, repo)
	}
	stmt += ` GROUP BY build_name, build_repo ORDER BY build_name, build_repo`
	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, wrapExec("builds list names", repo, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*BuildName
	for rows.Next() {
		n := &BuildName{}
		if err := rows.Scan(&n.Name, &n.Repo, &n.LastStarted); err != nil {
			return nil, wrapExec("builds list names scan", repo, err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("builds list names rows", repo, err)
	}
	return out, nil
}

// ListBuildNumbers implements BuildStore.ListBuildNumbers: every run of
// one name, newest first (the "latest = take first" ruling), number DESC
// as the deterministic tiebreak.
func (s *buildStore) ListBuildNumbers(ctx context.Context, name, repo string) ([]*BuildNumber, error) {
	stmt := `SELECT build_number, started, build_repo FROM builds WHERE build_name = ?`
	args := []any{name}
	if repo != "" {
		stmt += ` AND build_repo = ?`
		args = append(args, repo)
	}
	stmt += ` ORDER BY started DESC, build_number DESC`
	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, wrapExec("builds list numbers", name, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*BuildNumber
	for rows.Next() {
		n := &BuildNumber{}
		if err := rows.Scan(&n.Number, &n.Started, &n.Repo); err != nil {
			return nil, wrapExec("builds list numbers scan", name, err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("builds list numbers rows", name, err)
	}
	return out, nil
}

// PutModules implements BuildStore.PutModules: one transaction that drops
// the previous segment (the modules delete cascades artifacts and
// dependencies) and inserts the new one whole. The parent FK rejects an
// orphan segment — the caller's not-found mapping.
func (s *buildStore) PutModules(ctx context.Context, name, number, started, repo string, modules []*BuildModule) error {
	repo = normalizeBuildRepo(repo)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapExec("builds modules begin", name+"#"+number, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM build_modules WHERE `+buildCoordsWhere,
		name, number, started, repo); err != nil {
		return wrapExec("builds modules clear", name+"#"+number, err)
	}
	const modStmt = `INSERT INTO build_modules
		(build_name, build_number, started, build_repo, module_id, module_type)
		VALUES (?, ?, ?, ?, ?, ?)`
	const artStmt = `INSERT INTO build_artifacts
		(build_name, build_number, started, build_repo, module_id, seq,
		 name, type, sha1, sha256, md5, repo_key, path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	const depStmt = `INSERT INTO build_dependencies
		(build_name, build_number, started, build_repo, module_id, seq,
		 dep_id, dep_type, scopes, sha1, sha256, md5)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for _, m := range modules {
		if _, err := tx.ExecContext(ctx, modStmt,
			name, number, started, repo, m.ID, m.Type); err != nil {
			return wrapExec("builds modules put", name+"#"+number+"/"+m.ID, err)
		}
		for _, a := range m.Artifacts {
			// '' association writes NULL: the record-only artifact never
			// claims a nodes row (MATCH SIMPLE exempts NULL from the FK).
			var nodeRepo, nodePath any
			if a.RepoKey != "" || a.Path != "" {
				nodeRepo, nodePath = a.RepoKey, a.Path
			}
			if _, err := tx.ExecContext(ctx, artStmt,
				name, number, started, repo, m.ID, a.Seq,
				a.Name, a.Type, a.Sha1, a.Sha256, a.Md5, nodeRepo, nodePath); err != nil {
				return wrapExec("build artifacts put", name+"#"+number+"/"+m.ID, err)
			}
		}
		for _, d := range m.Dependencies {
			if _, err := tx.ExecContext(ctx, depStmt,
				name, number, started, repo, m.ID, d.Seq,
				d.ID, d.Type, d.Scopes, d.Sha1, d.Sha256, d.Md5); err != nil {
				return wrapExec("build dependencies put", name+"#"+number+"/"+m.ID, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return wrapExec("builds modules commit", name+"#"+number, err)
	}
	return nil
}

// ListModules implements BuildStore.ListModules: three ordered reads
// stitched in memory (module_id, then seq inside each module).
func (s *buildStore) ListModules(ctx context.Context, name, number, started, repo string) ([]*BuildModule, error) {
	repo = normalizeBuildRepo(repo)
	coords := []any{name, number, started, repo}

	out, byID, err := s.listModuleRows(ctx, coords, name+"#"+number)
	if err != nil {
		return nil, err
	}
	if err := s.attachArtifacts(ctx, coords, name+"#"+number, byID); err != nil {
		return nil, err
	}
	if err := s.attachDependencies(ctx, coords, name+"#"+number, byID); err != nil {
		return nil, err
	}
	return out, nil
}

// listModuleRows reads the module rows ordered by module_id, returning
// both the ordered slice and the id index the attach passes stitch into.
func (s *buildStore) listModuleRows(ctx context.Context, coords []any, key string) ([]*BuildModule, map[string]*BuildModule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT module_id, module_type FROM build_modules WHERE `+buildCoordsWhere+`
		 ORDER BY module_id`, coords...)
	if err != nil {
		return nil, nil, wrapExec("builds modules list", key, err)
	}
	defer func() { _ = rows.Close() }()
	byID := make(map[string]*BuildModule)
	var out []*BuildModule
	for rows.Next() {
		m := &BuildModule{}
		if err := rows.Scan(&m.ID, &m.Type); err != nil {
			return nil, nil, wrapExec("builds modules list scan", key, err)
		}
		byID[m.ID] = m
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, wrapExec("builds modules list rows", key, err)
	}
	return out, byID, nil
}

// attachArtifacts appends each artifact row to its module (seq order); a
// NULL association reads back as ” — the record-only shape.
func (s *buildStore) attachArtifacts(ctx context.Context, coords []any, key string, byID map[string]*BuildModule) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT module_id, seq, name, type, sha1, sha256, md5, repo_key, path
		 FROM build_artifacts WHERE `+buildCoordsWhere+` ORDER BY module_id, seq`, coords...)
	if err != nil {
		return wrapExec("build artifacts list", key, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			mid                string
			a                  BuildArtifact
			nodeRepo, nodePath sql.NullString
		)
		if err := rows.Scan(&mid, &a.Seq, &a.Name, &a.Type, &a.Sha1, &a.Sha256, &a.Md5,
			&nodeRepo, &nodePath); err != nil {
			return wrapExec("build artifacts list scan", key, err)
		}
		a.RepoKey, a.Path = nodeRepo.String, nodePath.String
		if m, ok := byID[mid]; ok {
			m.Artifacts = append(m.Artifacts, &a)
		}
	}
	if err := rows.Err(); err != nil {
		return wrapExec("build artifacts list rows", key, err)
	}
	return nil
}

// attachDependencies appends each dependency row to its module (seq
// order).
func (s *buildStore) attachDependencies(ctx context.Context, coords []any, key string, byID map[string]*BuildModule) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT module_id, seq, dep_id, dep_type, scopes, sha1, sha256, md5
		 FROM build_dependencies WHERE `+buildCoordsWhere+` ORDER BY module_id, seq`, coords...)
	if err != nil {
		return wrapExec("build dependencies list", key, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var mid string
		d := &BuildDependency{}
		if err := rows.Scan(&mid, &d.Seq, &d.ID, &d.Type, &d.Scopes,
			&d.Sha1, &d.Sha256, &d.Md5); err != nil {
			return wrapExec("build dependencies list scan", key, err)
		}
		if m, ok := byID[mid]; ok {
			m.Dependencies = append(m.Dependencies, d)
		}
	}
	if err := rows.Err(); err != nil {
		return wrapExec("build dependencies list rows", key, err)
	}
	return nil
}

// PutProperties implements BuildStore.PutProperties: the previous set
// leaves whole, the new one lands — set semantics in one transaction.
func (s *buildStore) PutProperties(ctx context.Context, name, number, started, repo string, props []*BuildProperty) error {
	repo = normalizeBuildRepo(repo)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapExec("build props begin", name+"#"+number, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM build_properties WHERE `+buildCoordsWhere,
		name, number, started, repo); err != nil {
		return wrapExec("build props clear", name+"#"+number, err)
	}
	const stmt = `INSERT INTO build_properties
		(build_name, build_number, started, build_repo, name, value)
		VALUES (?, ?, ?, ?, ?, ?)`
	for _, p := range props {
		if _, err := tx.ExecContext(ctx, stmt, name, number, started, repo, p.Name, p.Value); err != nil {
			return wrapExec("build props put", name+"#"+number+"/"+p.Name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return wrapExec("build props commit", name+"#"+number, err)
	}
	return nil
}

// ListProperties implements BuildStore.ListProperties.
func (s *buildStore) ListProperties(ctx context.Context, name, number, started, repo string) ([]*BuildProperty, error) {
	repo = normalizeBuildRepo(repo)
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, value FROM build_properties WHERE `+buildCoordsWhere+` ORDER BY name`,
		name, number, started, repo)
	if err != nil {
		return nil, wrapExec("build props list", name+"#"+number, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*BuildProperty
	for rows.Next() {
		p := &BuildProperty{}
		if err := rows.Scan(&p.Name, &p.Value); err != nil {
			return nil, wrapExec("build props list scan", name+"#"+number, err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("build props list rows", name+"#"+number, err)
	}
	return out, nil
}

// AppendPromotion implements BuildStore.AppendPromotion: the one write the
// append-only history has. The row keeps its id forever — there is no
// update and no delete face by design (the current status is the newest
// row, never a mutated one).
func (s *buildStore) AppendPromotion(ctx context.Context, p *BuildPromotion) error {
	repo := normalizeBuildRepo(p.Repo)
	dryRun := int64(0)
	if p.DryRun {
		dryRun = 1
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO build_promotions
		(id, build_name, build_number, started, build_repo,
		 status, target_repo, ci_user, comment, dry_run, params_json, promoted_by, promoted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Number, p.Started, repo,
		p.Status, p.TargetRepo, p.CiUser, p.Comment, dryRun, p.ParamsJSON, p.PromotedBy, p.PromotedAt); err != nil {
		return wrapExec("build promotions append", p.Name+"#"+p.Number, err)
	}
	return nil
}

// ListPromotions implements BuildStore.ListPromotions: newest first, id
// DESC as the deterministic tiebreak — the current status is row one.
func (s *buildStore) ListPromotions(ctx context.Context, name, number, started, repo string) ([]*BuildPromotion, error) {
	repo = normalizeBuildRepo(repo)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, build_name, build_number, started, build_repo,
			status, target_repo, ci_user, comment, dry_run, params_json, promoted_by, promoted_at
		 FROM build_promotions WHERE `+buildCoordsWhere+`
		 ORDER BY promoted_at DESC, id DESC`,
		name, number, started, repo)
	if err != nil {
		return nil, wrapExec("build promotions list", name+"#"+number, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*BuildPromotion
	for rows.Next() {
		p := &BuildPromotion{}
		var dryRun int64
		if err := rows.Scan(&p.ID, &p.Name, &p.Number, &p.Started, &p.Repo,
			&p.Status, &p.TargetRepo, &p.CiUser, &p.Comment, &dryRun,
			&p.ParamsJSON, &p.PromotedBy, &p.PromotedAt); err != nil {
			return nil, wrapExec("build promotions list scan", name+"#"+number, err)
		}
		p.DryRun = dryRun != 0
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("build promotions list rows", name+"#"+number, err)
	}
	return out, nil
}
