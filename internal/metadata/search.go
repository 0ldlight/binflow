package metadata

import (
	"context"
	"fmt"
	"strings"
)

// NodeSearcher is the read-only search extension of NodeStore (T-92, FR-26 /
// SR-01/SR-02; the gavc/prop/pattern arms of FR-134 ride the same seam,
// T-417). It is deliberately a SEPARATE interface rather than extra
// NodeStore methods: the M4 metadata wave extends the store family in
// parallel tickets (T-90's governance sub-stores), and a standalone seam in
// its own file keeps those change surfaces from colliding. The concrete
// nodeStore satisfies it; consumers reach it by asserting
// md.Nodes().(NodeSearcher) and treat a failed assertion as "search is not
// available on this store" (the production sqlite store always satisfies it).
//
// Contract notes shared by every method:
//   - repos narrows the query SQL-side (the IN predicate): nil or empty means
//     every repository. Unknown keys simply match nothing — the caller's
//     400-vs-empty decision is not this layer's business (same posture as
//     RepoStore filters).
//   - folder rows (the trailing-slash marker nodes) are never returned:
//     search answers artifacts, and the folder markers all share one sentinel
//     blob that would otherwise flood checksum results.
//   - rows come back ordered by (repo_key, path) so callers and tests see a
//     stable sequence across engines.
//   - limit > 0 caps the row count SQL-side (the K63 cap+1 probe the legacy
//     searches ride, T-417); 0 is unbounded (the T-92 pair's posture).
type NodeSearcher interface {
	// SearchByName returns every file node whose repo-relative path contains
	// name as a literal, case-INsensitive substring (K64's calibration,
	// aql.md section 0-5: official "part of file name" wording + live probe
	// + decompiled caseInsensitive default — BinFlow's former
	// case-sensitive reading was the drift, corrected in T-417). LIKE
	// wildcards inside name (% _ \) are escaped so the caller's fragment
	// always means itself; the fold is ASCII (SQLite's LOWER is ASCII-only),
	// which covers the alphabet real artifact names vary by. name must be
	// non-empty — the caller owns that validation.
	SearchByName(ctx context.Context, name string, repos []string) ([]*Node, error)
	// SearchByChecksum resolves the given digests onto blob rows and returns
	// every file node referencing them. Each digest is independent: sha256
	// addresses blobs directly, sha1/md5 resolve through the blobs ledger
	// first (a miss on one digest does not fail the others — the union of all
	// resolved blobs wins). All three may be empty, in which case no blob is
	// addressed and the answer is an empty slice; format validation is the
	// caller's (repo layer) contract.
	SearchByChecksum(ctx context.Context, sha256, sha1, md5 string, repos []string) ([]*Node, error)
	// SearchByPath returns every file node whose repo-relative path starts
	// with f.Prefix and contains each f.Infixes entry as a literal substring
	// (FR-134.1, the gavc arm's path translation — a maven coordinate set
	// composes into literal path pieces upstream). Matching is
	// case-sensitive; LIKE wildcards inside the pieces are escaped so they
	// always mean themselves. An empty piece applies no conjunct — a fully
	// empty PathFilter matches every file node, and the caller owns that
	// validation.
	SearchByPath(ctx context.Context, f PathFilter, limit int, repos []string) ([]*Node, error)
	// SearchByProps returns every file node carrying ALL the given property
	// constraints (FR-134.2, the prop arm): a PropFilter with an empty Value
	// is the key's bare existence (any value), a non-empty one pins the
	// value. conds must be non-empty — the caller owns that validation.
	SearchByProps(ctx context.Context, conds []PropFilter, limit int, repos []string) ([]*Node, error)
	// SearchByPattern returns every file node whose repository key and
	// repo-relative path match repoLike/pathLike (FR-134.3, the pattern
	// arm). Both arrive as PRE-TRANSLATED SQL LIKE patterns — the single
	// wildcard translator in internal/search (match.go) did the '*'-/'?'-to-
	// '%''_'-and-escape pass at the endpoint; this layer only appends the
	// ESCAPE clause and never translates a second time (ADR-0043 pt 1/3,
	// "防三个译者"). Either pattern may be empty (no conjunct).
	SearchByPattern(ctx context.Context, repoLike, pathLike string, limit int) ([]*Node, error)
}

// PathFilter is the literal path-constraint family of the gavc search: a
// leading path prefix plus infix substrings, all literal (case-sensitive;
// LIKE wildcard bytes escape to themselves).
type PathFilter struct {
	// Prefix matches the head of the repo-relative path ("" = unconstrained).
	Prefix string
	// Infixes each match anywhere inside the path (nil/"" entries apply no
	// conjunct).
	Infixes []string
}

// PropFilter is one property constraint of the prop search: Key must exist
// on the node; a non-empty Value additionally pins the value (the official
// key-without-value "*" form is Value == "").
type PropFilter struct {
	Key   string
	Value string
}

// The shared nodeStore carries the search extension (see the interface
// comment for why this is not part of NodeStore itself).
var _ NodeSearcher = (*nodeStore)(nil)

// likeLiteral escapes a caller fragment's LIKE-wildcard bytes so the
// fragment always means itself inside a LIKE pattern (the same replacer
// likePrefix uses).
func likeLiteral(frag string) string {
	repl := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return repl.Replace(frag)
}

// likeSubstringFolded is the K64-calibrated name-search pattern: the
// fragment folds to lowercase before escaping, and the caller wraps the
// column in LOWER() so both sides compare folded regardless of the store's
// case_sensitive_like pragma (store.go DSN). The fold is ASCII — SQLite's
// LOWER knows no Unicode case tables — which covers the alphabet artifact
// names actually vary by (a drift registered with K64's write-up).
func likeSubstringFolded(frag string) string {
	return "%" + likeLiteral(strings.ToLower(frag)) + "%"
}

// nodeQuery runs the shared search SELECT with extra conjuncts. conds are
// static SQL fragments chosen by the callers (the ONLY dynamic piece is the
// placeholder count of the IN lists); every caller-supplied VALUE rides in
// args — the assembly below never interpolates data, so G202's
// concatenation signal is a false positive here by construction. limit > 0
// caps the row count SQL-side (the K63 +1 probe arm).
func (s *nodeStore) nodeQuery(ctx context.Context, conds []string, args []any, limit int) ([]*Node, error) {
	q := `SELECT repo_key, path, sha256, size, mime, created_by, created_at, updated_at
		FROM nodes WHERE path NOT LIKE '%/'`
	for _, c := range conds {
		q += " AND (" + c + ")" //nolint:gosec // G202: fragments are package-constant shapes; values stay parameterized in args
	}
	q += ` ORDER BY repo_key, path`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, wrapExec("nodes search", "", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*Node
	for rows.Next() {
		n := &Node{}
		if err := rows.Scan(&n.RepoKey, &n.Path, &n.Sha256, &n.Size, &n.Mime, &n.CreatedBy, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, wrapExec("nodes search scan", "", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("nodes search rows", "", err)
	}
	return out, nil
}

// repoFilter appends the repos IN predicate when the list narrows anything.
// It returns the conjunct ("" when no narrowing applies) plus the args to
// hand nodeQuery; callers keep their own args order stable by appending this
// fragment first or last consistently.
func repoFilter(repos []string) (cond string, args []any) {
	if len(repos) == 0 {
		return "", nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(repos)), ",")
	args = make([]any, 0, len(repos))
	for _, r := range repos {
		args = append(args, r)
	}
	return "repo_key IN (" + placeholders + ")", args
}

// SearchByName implements NodeSearcher (K64: case-insensitive literal
// substring — LOWER on both sides, the fragment folded by
// likeSubstringFolded).
func (s *nodeStore) SearchByName(ctx context.Context, name string, repos []string) ([]*Node, error) {
	var conds []string
	var args []any
	if rc, rargs := repoFilter(repos); rc != "" {
		conds = append(conds, rc)
		args = append(args, rargs...)
	}
	conds = append(conds, `LOWER(path) LIKE ? ESCAPE '\'`)
	args = append(args, likeSubstringFolded(name))
	return s.nodeQuery(ctx, conds, args, 0)
}

// SearchByChecksum implements NodeSearcher. Digest resolution rides the
// blobs ledger: sha256 keys blobs directly, sha1 through idx_blobs_sha1 (the
// T-73 seam), md5 through a plain ledger scan (no md5 index exists — the
// ledger is one row per unique content, so the scan is bounded by content
// count, and the NFR-P17 index inventory stays the architect's call).
func (s *nodeStore) SearchByChecksum(ctx context.Context, sha256, sha1, md5 string, repos []string) ([]*Node, error) {
	keys := map[string]bool{}
	if sha256 != "" {
		keys[sha256] = true
	}
	// The auxiliary digests resolve through the ledger; each column gets its
	// own literal statement (no string-built SQL — gosec G201 posture).
	aux := []struct {
		col, val, stmt string
	}{
		{"sha1", sha1, `SELECT sha256 FROM blobs WHERE sha1 = ?`},
		{"md5", md5, `SELECT sha256 FROM blobs WHERE md5 = ?`},
	}
	for _, a := range aux {
		if a.val == "" {
			continue
		}
		rows, err := s.db.QueryContext(ctx, a.stmt, a.val)
		if err != nil {
			return nil, wrapExec("blobs resolve by "+a.col, a.val, err)
		}
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				_ = rows.Close()
				return nil, wrapExec("blobs resolve by "+a.col+" scan", a.val, err)
			}
			if k != "" {
				keys[k] = true
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, wrapExec("blobs resolve by "+a.col+" rows", a.val, err)
		}
		_ = rows.Close()
	}
	if len(keys) == 0 {
		return nil, nil
	}
	keyList := make([]string, 0, len(keys))
	for k := range keys {
		keyList = append(keyList, k)
	}
	var conds []string
	var args []any
	if rc, rargs := repoFilter(repos); rc != "" {
		conds = append(conds, rc)
		args = append(args, rargs...)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keyList)), ",")
	conds = append(conds, "sha256 IN ("+placeholders+")")
	for _, k := range keyList {
		args = append(args, k)
	}
	nodes, err := s.nodeQuery(ctx, conds, args, 0)
	if err != nil {
		return nil, fmt.Errorf("checksum search: %w", err)
	}
	return nodes, nil
}

// SearchByPath implements NodeSearcher: one leading-prefix conjunct plus one
// contains conjunct per infix, all literal through likeLiteral.
func (s *nodeStore) SearchByPath(ctx context.Context, f PathFilter, limit int, repos []string) ([]*Node, error) {
	var conds []string
	var args []any
	if rc, rargs := repoFilter(repos); rc != "" {
		conds = append(conds, rc)
		args = append(args, rargs...)
	}
	if f.Prefix != "" {
		conds = append(conds, `path LIKE ? ESCAPE '\'`)
		args = append(args, likeLiteral(f.Prefix)+"%")
	}
	for _, infx := range f.Infixes {
		if infx == "" {
			continue
		}
		conds = append(conds, `path LIKE ? ESCAPE '\'`)
		args = append(args, "%"+likeLiteral(infx)+"%")
	}
	nodes, err := s.nodeQuery(ctx, conds, args, limit)
	if err != nil {
		return nil, fmt.Errorf("gavc path search: %w", err)
	}
	return nodes, nil
}

// SearchByProps implements NodeSearcher: one correlated EXISTS per
// constraint over the node_props primary key (repo_key, path, name[, value])
// — the same access shape the AQL property predicates ride, the M10
// idx_node_props_name reservation's sibling consumer. The EXISTS probes one
// node_props row, so AND-across-constraints holds per node; multi-valued
// keys match when any value does.
func (s *nodeStore) SearchByProps(ctx context.Context, conds []PropFilter, limit int, repos []string) ([]*Node, error) {
	var sqlConds []string
	var args []any
	if rc, rargs := repoFilter(repos); rc != "" {
		sqlConds = append(sqlConds, rc)
		args = append(args, rargs...)
	}
	for _, c := range conds {
		exists := "EXISTS (SELECT 1 FROM node_props np WHERE np.repo_key = nodes.repo_key AND np.path = nodes.path AND np.name = ?"
		args = append(args, c.Key)
		if c.Value != "" {
			exists += " AND np.value = ?"
			args = append(args, c.Value)
		}
		sqlConds = append(sqlConds, exists+")") //nolint:gosec // G202: constant fragment; the key/value ride in args
	}
	nodes, err := s.nodeQuery(ctx, sqlConds, args, limit)
	if err != nil {
		return nil, fmt.Errorf("property search: %w", err)
	}
	return nodes, nil
}

// SearchByPattern implements NodeSearcher: the two pre-translated LIKE
// patterns with the ESCAPE clause appended (the translation itself happened
// once, in internal/search's kernel, at the endpoint).
func (s *nodeStore) SearchByPattern(ctx context.Context, repoLike, pathLike string, limit int) ([]*Node, error) {
	var conds []string
	var args []any
	if repoLike != "" {
		conds = append(conds, `repo_key LIKE ? ESCAPE '\'`)
		args = append(args, repoLike)
	}
	if pathLike != "" {
		conds = append(conds, `path LIKE ? ESCAPE '\'`)
		args = append(args, pathLike)
	}
	nodes, err := s.nodeQuery(ctx, conds, args, limit)
	if err != nil {
		return nil, fmt.Errorf("pattern search: %w", err)
	}
	return nodes, nil
}
