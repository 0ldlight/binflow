package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// Search use cases (T-92, FR-26 / SR-01/SR-02). The SQL mechanics live in
// metadata.NodeSearcher; this layer owns the parts the store cannot judge:
// query validation (K2's provisional semantics), the anonymous-channel gate
// and the ACL visibility filter.
//
// ACL posture (NFR-S24, zero leak): a result node is visible exactly when
// the caller could GET it — the same Authorizer.Can(read) decision the
// content plane runs, evaluated per node path. Admin principals skip the
// filter (they see everything); the repos filter narrows the SQL predicate
// BEFORE rows come back, and the per-node check removes whatever path-level
// grants still hide. A user holding no readable path anywhere simply gets an
// empty result set, never an error — the PRD allows predicate pushdown or
// post-filtering; this is the post-filter arm with SQL-side repo narrowing.

// ChecksumQuery is the SR-02 addressing triple. At least one field must be
// set; values are bare lowercase hex (the use case normalizes case and
// rejects wrong shapes before any store access).
type ChecksumQuery struct {
	Sha256 string
	Sha1   string
	Md5    string
}

// maxSearchRepos caps the repos csv filter. The placeholder fan-out of an
// unbounded IN list is a denial-of-service vector against the SQL engine
// budget, and no legitimate caller names thousands of repositories; the cap
// answers 400 (ErrInvalidSearchQuery) well before the engine limit.
const maxSearchRepos = 1000

// SearchArtifacts implements Service.SearchArtifacts (SR-01, K2 provisional:
// literal case-sensitive path substring, SQL LIKE). repos narrows the
// candidate repositories; nil or empty means every repository the caller can
// read.
func (s *service) SearchArtifacts(ctx context.Context, p *Principal, name string, repos []string) ([]*metadata.Node, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: artifact search requires a non-empty name", ErrInvalidSearchQuery)
	}
	if err := validateReposFilter(repos); err != nil {
		return nil, err
	}
	if err := s.searchGate(ctx, p); err != nil {
		return nil, err
	}
	ns, err := s.searcher()
	if err != nil {
		return nil, err
	}
	nodes, err := ns.SearchByName(ctx, name, repos)
	if err != nil {
		return nil, fmt.Errorf("search artifacts by name %q: %w", name, err)
	}
	return s.filterVisible(ctx, p, nodes), nil
}

// SearchChecksum implements Service.SearchChecksum (SR-02). Every digest is
// resolved independently onto blob rows and the result is the union of the
// nodes referencing them, de-duplicated by (repo, path); at least one digest
// must be present and each present one must be well-formed bare hex of its
// algorithm's length.
func (s *service) SearchChecksum(ctx context.Context, p *Principal, q ChecksumQuery, repos []string) ([]*metadata.Node, error) {
	normalized, err := normalizeChecksumQuery(q)
	if err != nil {
		return nil, err
	}
	if err := validateReposFilter(repos); err != nil {
		return nil, err
	}
	if err := s.searchGate(ctx, p); err != nil {
		return nil, err
	}
	ns, err := s.searcher()
	if err != nil {
		return nil, err
	}
	nodes, err := ns.SearchByChecksum(ctx, normalized.Sha256, normalized.Sha1, normalized.Md5, repos)
	if err != nil {
		return nil, fmt.Errorf("search by checksum: %w", err)
	}
	return s.filterVisible(ctx, p, nodes), nil
}

// searcher resolves the metadata search seam. The production sqlite store
// always satisfies metadata.NodeSearcher; the defensive error keeps a store
// without the seam (a test double, a future engine) answering an honest 500
// instead of panicking on a nil interface call.
func (s *service) searcher() (metadata.NodeSearcher, error) {
	if s.search == nil {
		return nil, fmt.Errorf("%w: the configured metadata store does not support search", ErrSearchUnavailable)
	}
	return s.search, nil
}

// searchGate applies the anonymous-channel rule (O4 boundary, PRD FR-26):
// anonymous callers ride the anonymous_access switch exactly like a content
// read — open instance passes, closed instance denies with 403 (rest-api.md
// section 4's unauthenticated status; the 401 challenge family stays with
// the management plane). Authenticated callers proceed and are filtered per
// node.
func (s *service) searchGate(ctx context.Context, p *Principal) error {
	if p == nil && !s.allow(ctx, nil, "", "", ActionRead) {
		return fmt.Errorf("search: %w: anonymous access is disabled on this instance", ErrForbidden)
	}
	return nil
}

// filterVisible keeps the nodes the principal may read. Admin sees
// everything; every other principal — anonymous included — is judged per
// node path through the same Authorizer.Can(read) call a download runs, so
// the search surface can never reveal a row the content plane would refuse.
func (s *service) filterVisible(ctx context.Context, p *Principal, nodes []*metadata.Node) []*metadata.Node {
	if p != nil && p.Admin {
		return nodes
	}
	out := make([]*metadata.Node, 0, len(nodes))
	for _, n := range nodes {
		if s.allow(ctx, p, n.RepoKey, n.Path, ActionRead) {
			out = append(out, n)
		}
	}
	return out
}

// SearchScope implements Service.SearchScope (T-413, FR-133.2 /
// ADR-0043 pt 4): the collection-level first stage of the AQL read weave.
// The repository set comes from the registry (virtual excluded — nodes
// carry no virtual rows, aql.md §7), and the read classification from the
// SAME permission tables auth.Can evaluates: a target whose principal rows
// address p with the read bit puts its whole repos list in scope, and a
// target with no include/exclude patterns additionally marks those repos
// pattern-free (PathScoped=false — targetCovers with empty patterns covers
// every path, so no row check can add information). Pattern-carrying
// targets never narrow here: their path semantics stay in auth's matcher
// and reach the rows through CanRead, the single source (the axis 3-C
// double-truth-source rejection of ADR-0043).
//
// Roles that Can short-circuits to global read — admin, readonly-admin,
// and anonymous on an open instance (the searchGate above already refused
// the closed-instance arm) — take the universe without consulting the
// permission tables, mirroring the decision order of auth.Can.
//
// Failure posture: store errors propagate (the engine maps them to the
// 500 class — an honest failure, never a widened scope); a malformed
// target row is skipped fail-closed, exactly the posture auth.Can takes.
func (s *service) SearchScope(ctx context.Context, p *Principal) ([]ReadScope, error) {
	if err := s.searchGate(ctx, p); err != nil {
		return nil, err
	}
	repos, err := s.md.Repos().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("search scope: listing repositories: %w", err)
	}
	slices.SortFunc(repos, func(a, b *metadata.Repo) int {
		return strings.Compare(a.RepoKey, b.RepoKey)
	})

	if p == nil || p.Admin || p.EffectiveRole() == auth.RoleReadOnlyAdmin {
		scope := make([]ReadScope, 0, len(repos))
		for _, r := range repos {
			if r.Type == TypeVirtual {
				continue
			}
			scope = append(scope, ReadScope{Repo: r.RepoKey})
		}
		return scope, nil
	}

	targets, rows, err := s.readGrantTables(ctx)
	if err != nil {
		return nil, err
	}
	covered := make(map[string]bool)
	patternFree := make(map[string]bool)
	for _, t := range targets {
		repoKeys, includes, excludes, err := decodeScopeTarget(t)
		if err != nil {
			// Fail closed on the malformed row, mirroring auth.Can's skip.
			continue
		}
		if !scopeTargetReads(rows[t.Name], p) {
			continue
		}
		free := len(includes) == 0 && len(excludes) == 0
		for _, key := range repoKeys {
			covered[key] = true
			if free {
				patternFree[key] = true
			}
		}
	}
	var scope []ReadScope
	for _, r := range repos {
		if r.Type == TypeVirtual {
			continue
		}
		switch {
		case patternFree[r.RepoKey]:
			scope = append(scope, ReadScope{Repo: r.RepoKey, PathScoped: false})
		case covered[r.RepoKey]:
			scope = append(scope, ReadScope{Repo: r.RepoKey, PathScoped: true})
		}
	}
	return scope, nil
}

// CanRead implements Service.CanRead: the row-level second stage of the AQL
// read weave — the public read-only projection of the same allow(read)
// decision the content plane and filterVisible run (ADR-0043 pt 4).
func (s *service) CanRead(ctx context.Context, p *Principal, repoKey, path string) bool {
	return s.allow(ctx, p, repoKey, path, ActionRead)
}

// readGrantTables loads the permission targets and their principal rows in
// two queries (the N+1 PrincipalsFor-per-repo shape is what
// metadata.PermissionStore.Principals exists to avoid, M9 E9).
func (s *service) readGrantTables(ctx context.Context) ([]*metadata.PermissionTarget, map[string][]*metadata.PermissionPrincipal, error) {
	targets, err := s.md.Permissions().ListTargets(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("search scope: listing permission targets: %w", err)
	}
	rows, err := s.md.Permissions().Principals(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("search scope: listing permission principals: %w", err)
	}
	byTarget := make(map[string][]*metadata.PermissionPrincipal)
	for _, r := range rows {
		byTarget[r.TargetName] = append(byTarget[r.TargetName], r)
	}
	return targets, byTarget, nil
}

// decodeScopeTarget unpacks one target's JSON array columns. It reads the
// same stored strings auth's decodeTarget does; only the presence of the
// pattern arrays matters here — their matching semantics never leave auth.
func decodeScopeTarget(t *metadata.PermissionTarget) (repos, includes, excludes []string, err error) {
	if err = json.Unmarshal([]byte(t.Repos), &repos); err != nil {
		return nil, nil, nil, fmt.Errorf("target %q repos column: %w", t.Name, err)
	}
	if err = json.Unmarshal([]byte(t.Includes), &includes); err != nil {
		return nil, nil, nil, fmt.Errorf("target %q includes column: %w", t.Name, err)
	}
	if err = json.Unmarshal([]byte(t.Excludes), &excludes); err != nil {
		return nil, nil, nil, fmt.Errorf("target %q excludes column: %w", t.Name, err)
	}
	return repos, includes, excludes, nil
}

// scopeTargetReads reports whether any principal row of the target grants
// the principal read: the SE-07 union restated (auth's rowCoversPrincipal —
// internal/auth/groups.go — is unexported and the Authorizer seam cannot
// enumerate coverage, so this is the one deliberate restatement of the
// rule; keep the two in sync).
func scopeTargetReads(rows []*metadata.PermissionPrincipal, p *Principal) bool {
	for _, row := range rows {
		if !row.CanRead {
			continue
		}
		switch row.PrincipalType {
		case "user":
			if strings.EqualFold(row.Principal, p.Name) {
				return true
			}
		case "group":
			if slices.Contains(p.Groups, row.Principal) {
				return true
			}
		}
	}
	return false
}

// validateReposFilter rejects an oversized repos list before it reaches the
// SQL placeholder budget.
func validateReposFilter(repos []string) error {
	if len(repos) > maxSearchRepos {
		return fmt.Errorf("%w: repos filter lists %d repositories, at most %d are accepted",
			ErrInvalidSearchQuery, len(repos), maxSearchRepos)
	}
	return nil
}

// checksumShapes pairs each digest parameter with its algorithm length.
var checksumShapes = []struct {
	name  string
	value func(ChecksumQuery) string
	want  int
}{
	{"sha256", func(q ChecksumQuery) string { return q.Sha256 }, 64},
	{"sha1", func(q ChecksumQuery) string { return q.Sha1 }, 40},
	{"md5", func(q ChecksumQuery) string { return q.Md5 }, 32},
}

// normalizeChecksumQuery trims, lowercases and shape-checks every present
// digest, and answers ErrInvalidSearchQuery when none is present at all
// (SR-02: at least one value). Mixed-case hex is accepted — clients echo
// digests in whatever case they stored them — and normalized to the
// lowercase spelling every table column uses.
func normalizeChecksumQuery(q ChecksumQuery) (ChecksumQuery, error) {
	present := false
	for _, c := range checksumShapes {
		v := strings.ToLower(strings.TrimSpace(c.value(q)))
		if v == "" {
			continue
		}
		present = true
		if len(v) != c.want || !isHex(v) {
			return ChecksumQuery{}, fmt.Errorf("%w: %s checksum %q must be %d hex characters",
				ErrInvalidSearchQuery, c.name, v, c.want)
		}
		switch c.name {
		case "sha256":
			q.Sha256 = v
		case "sha1":
			q.Sha1 = v
		case "md5":
			q.Md5 = v
		}
	}
	if !present {
		return ChecksumQuery{}, fmt.Errorf("%w: checksum search requires at least one of sha256, sha1 or md5", ErrInvalidSearchQuery)
	}
	return q, nil
}

// isHex reports whether s is entirely lowercase hex digits (the caller
// lowercased it first).
func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}
