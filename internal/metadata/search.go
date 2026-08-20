package metadata

import (
	"context"
	"fmt"
	"strings"
)

// NodeSearcher is the read-only search extension of NodeStore (T-92, FR-26 /
// SR-01/SR-02). It is deliberately a SEPARATE interface rather than extra
// NodeStore methods: the M4 metadata wave extends the store family in
// parallel tickets (T-90's governance sub-stores), and a standalone seam in
// its own file keeps those change surfaces from colliding. The concrete
// nodeStore satisfies it; consumers reach it by asserting
// md.Nodes().(NodeSearcher) and treat a failed assertion as "search is not
// available on this store" (the production sqlite store always satisfies it).
//
// Contract notes shared by both methods:
//   - repos narrows the query SQL-side (the IN predicate): nil or empty means
//     every repository. Unknown keys simply match nothing — the caller's
//     400-vs-empty decision is not this layer's business (same posture as
//     RepoStore filters).
//   - folder rows (the trailing-slash marker nodes) are never returned:
//     search answers artifacts, and the folder markers all share one sentinel
//     blob that would otherwise flood checksum results.
//   - rows come back ordered by (repo_key, path) so callers and tests see a
//     stable sequence across engines.
type NodeSearcher interface {
	// SearchByName returns every file node whose repo-relative path contains
	// name as a literal, case-sensitive substring (K2's provisional artifact
	// search semantics: path/filename substring via SQL LIKE; the `*`
	// wildcard family is P2). LIKE wildcards inside name (% _ \) are escaped
	// so the caller's fragment always means itself. name must be non-empty —
	// the caller owns that validation.
	SearchByName(ctx context.Context, name string, repos []string) ([]*Node, error)
	// SearchByChecksum resolves the given digests onto blob rows and returns
	// every file node referencing them. Each digest is independent: sha256
	// addresses blobs directly, sha1/md5 resolve through the blobs ledger
	// first (a miss on one digest does not fail the others — the union of all
	// resolved blobs wins). All three may be empty, in which case no blob is
	// addressed and the answer is an empty slice; format validation is the
	// caller's (repo layer) contract.
	SearchByChecksum(ctx context.Context, sha256, sha1, md5 string, repos []string) ([]*Node, error)
}

// The shared nodeStore carries the search extension (see the interface
// comment for why this is not part of NodeStore itself).
var _ NodeSearcher = (*nodeStore)(nil)

// likeSubstring escapes a caller fragment into a literal substring LIKE
// pattern. case_sensitive_like=1 (store.go DSN) makes the match binary
// sensitive; without the escaping a "%" in an artifact name would widen the
// match instead of naming the file (the same replacer likePrefix uses).
func likeSubstring(frag string) string {
	repl := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + repl.Replace(frag) + "%"
}

// nodeQuery runs the shared search SELECT with extra conjuncts. conds are
// static SQL fragments chosen by the callers (the ONLY dynamic piece is the
// placeholder count of the IN lists); every caller-supplied VALUE rides in
// args — the assembly below never interpolates data, so G202's
// concatenation signal is a false positive here by construction.
func (s *nodeStore) nodeQuery(ctx context.Context, conds []string, args []any) ([]*Node, error) {
	q := `SELECT repo_key, path, sha256, size, mime, created_by, created_at, updated_at
		FROM nodes WHERE path NOT LIKE '%/'`
	for _, c := range conds {
		q += " AND (" + c + ")" //nolint:gosec // G202: fragments are package-constant shapes; values stay parameterized in args
	}
	q += ` ORDER BY repo_key, path`
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

// SearchByName implements NodeSearcher.
func (s *nodeStore) SearchByName(ctx context.Context, name string, repos []string) ([]*Node, error) {
	var conds []string
	var args []any
	if rc, rargs := repoFilter(repos); rc != "" {
		conds = append(conds, rc)
		args = append(args, rargs...)
	}
	conds = append(conds, `path LIKE ? ESCAPE '\'`)
	args = append(args, likeSubstring(name))
	return s.nodeQuery(ctx, conds, args)
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
	nodes, err := s.nodeQuery(ctx, conds, args)
	if err != nil {
		return nil, fmt.Errorf("checksum search: %w", err)
	}
	return nodes, nil
}
