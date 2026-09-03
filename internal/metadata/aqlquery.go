package metadata

import (
	"context"
	"fmt"
	"strings"
)

// NodeQueryer is the read-only AQL execution seam over nodes (T-411,
// FR-133.2 / ADR-0043 pt 1). The AST→IR compiler in internal/search lowers
// a parsed query into a NodeQuery and this seam turns the IR into
// dialect-parameterized SQL. Like NodeSearcher it is a separate interface
// asserted off NodeStore (md.Nodes().(NodeQueryer)); the production sqlite
// store always satisfies it.
//
// Contract notes:
//   - SQL text is assembled from package-constant fragments only; every
//     caller-controlled value rides in args (NFR-S74: the injection surface
//     is closed structurally, not by escaping).
//   - Rows are ordered by the query's sort keys with the constant
//     (repo_key, path) tiebreaker appended, so paging is deterministic
//     (ADR-0043 pt 3); a query without sort keys is ordered by the
//     tiebreaker alone.
//   - Folder marker rows (trailing-slash paths, ADR-0016) take part only
//     when the query asks for them: the search planner appends an implicit
//     file predicate to every query without a type condition (aql.md §2.2,
//     live evidence v09), and an explicit type condition governs itself.
//   - The blobs ledger is joined lazily: only queries whose criteria or
//     projection reference sha1/md5 pay for it (ADR-0043 pt 3, axis 7-A).
type NodeQueryer interface {
	// QueryNodes executes one compiled query. An empty predicate matches
	// every row (the planner's implicit file predicate is the caller's to
	// add); q.Where == nil is treated as match-all.
	QueryNodes(ctx context.Context, q NodeQuery) ([]*NodeQueryRow, error)
}

// The concrete nodeStore carries the AQL query extension.
var _ NodeQueryer = (*nodeStore)(nil)

// QueryField is the closed-set key naming one storage-backed projection of
// the nodes row (ADR-0043 pt 3). The planner maps registry-resolved AQL
// field names onto these keys; the compiler maps keys onto SQL expressions.
// type has no key of its own: it lowers onto the folder test predicate
// because nodes has no type column (the trailing-slash marker convention).
type QueryField string

// The query field closure (ADR-0043 pt 3 plus the aql.md §10 mapping):
// modified and updated share the updated_at column (single-column dual
// meaning — BinFlow has no separate modification timestamp); sha1 and md5
// live in the blobs ledger behind the lazy join. The statistics triple
// (M16 T-440, aql.md §14.1) rides the four per-node counting columns T-438
// landed — the same single counting channel the ?stats face reads, never a
// second source.
const (
	QueryRepo      QueryField = "repo"
	QueryPath      QueryField = "path" // AQL path = the parent directory (aql.md §2.2/§10)
	QueryName      QueryField = "name"
	QueryDepth     QueryField = "depth"
	QuerySize      QueryField = "size"
	QueryCreated   QueryField = "created"
	QueryModified  QueryField = "modified"
	QueryUpdated   QueryField = "updated"
	QueryCreatedBy QueryField = "created_by"
	QuerySha256    QueryField = "sha256"
	QuerySha1      QueryField = "sha1"
	QueryMd5       QueryField = "md5"
	// QueryDownloaded is the statistics domain's downloaded field:
	// nodes.last_downloaded_at, whose structural zero '' is the wire-side
	// null (never downloaded — aql.md §14.1 zero-value semantics).
	QueryDownloaded QueryField = "downloaded"
	// QueryDownloads is the statistics domain's downloads counter:
	// nodes.download_count, whose structural zero 0 is the wire-side null.
	QueryDownloads QueryField = "downloads"
	// QueryDownloadedBy is the statistics domain's downloaded_by field:
	// nodes.last_downloaded_by, '' = never.
	QueryDownloadedBy QueryField = "downloaded_by"
)

// QueryOp is the comparator vocabulary of the compiled predicate tree.
// Date bounds and wildcard patterns are already resolved by the planner:
// Like carries a translated SQL pattern (match.go's single translator).
type QueryOp string

// The comparator closure. Values arrive in QueryCompare.Value /
// QueryPropCond.Value as string or int64.
const (
	QueryEq      QueryOp = "eq"
	QueryNe      QueryOp = "ne"
	QueryGt      QueryOp = "gt"
	QueryGte     QueryOp = "gte"
	QueryLt      QueryOp = "lt"
	QueryLte     QueryOp = "lte"
	QueryLike    QueryOp = "like"
	QueryNotLike QueryOp = "notlike"
)

// NodePredicate is the sealed predicate tree of one query. Type-switch
// over QueryAnd, QueryOr, QueryNot, QueryFalse, QueryFolderTest,
// QueryCompare, QueryPropExists and QueryPropSame.
type NodePredicate interface{ isNodePredicate() }

// QueryAnd is a conjunction; an empty Children list matches every row.
type QueryAnd struct{ Children []NodePredicate }

// QueryOr is a disjunction of one or more children.
type QueryOr struct{ Children []NodePredicate }

// QueryNot negates its child.
type QueryNot struct{ Child NodePredicate }

// QueryFalse is the unsatisfiable predicate (e.g. {"type":{"$ne":"any"}} —
// every node is either a file or a folder, so "not any" matches nothing;
// also the expansion of a memberless virtual repository under $eq).
type QueryFalse struct{}

// QueryFolderTest constrains the derived node type: folder rows are the
// trailing-slash marker rows (ADR-0016). Folder=false selects files only.
type QueryFolderTest struct{ Folder bool }

// QueryCompare is one comparator over a storage field. Date fields
// (created/modified/updated) are normalized on both sides by the compiler;
// every other field compares its column expression directly.
type QueryCompare struct {
	Field QueryField
	Op    QueryOp
	Value any // string or int64
}

// QueryPropCond is one constraint arm of a property predicate: on the
// property name (OnValue=false) or its value (OnValue=true).
type QueryPropCond struct {
	OnValue bool
	Op      QueryOp
	Value   string
}

// QueryPropExists requires any node_props row of the node (row-level
// any-of — a multi-valued property matches when any of its values does,
// ADR-0043 pt 3) to satisfy every condition. An empty Conds list is the
// bare "node carries any property" test.
type QueryPropExists struct{ Conds []QueryPropCond }

// QueryPropSame is $msp (aql.md §2.3): ONE node_props row — the same
// property instance — must satisfy every condition, unlike a plain And of
// property conditions where each condition may be met by a different
// value of the same key.
type QueryPropSame struct{ Conds []QueryPropCond }

// QueryZero is the statistics domain's null-literal test (aql.md §14.1):
// the field sits at its STRUCTURAL zero — download_count = 0,
// last_downloaded_at = ” / last_downloaded_by = ” — which is what the
// wire-side {"$eq":null} denotes on a stats field. Negate is the $ne arm.
// The zero spellings live in one place (zeroValueSQL) so the wire and the
// storage conventions cannot drift.
type QueryZero struct {
	Field  QueryField
	Negate bool
}

func (*QueryAnd) isNodePredicate()        {}
func (*QueryOr) isNodePredicate()         {}
func (*QueryNot) isNodePredicate()        {}
func (*QueryFalse) isNodePredicate()      {}
func (*QueryFolderTest) isNodePredicate() {}
func (*QueryCompare) isNodePredicate()    {}
func (*QueryPropExists) isNodePredicate() {}
func (*QueryPropSame) isNodePredicate()   {}
func (*QueryZero) isNodePredicate()       {}

// NodeSort is one sort key; the compiler appends the constant
// (repo_key, path) tiebreaker after the user keys.
type NodeSort struct {
	Field QueryField
	Asc   bool
}

// NodeQuery is the dialect-neutral compiled form of one AQL items query
// (the IR of ADR-0043 pt 3's two-stage pipeline). Fields is the storage
// projection; Props lists property projection keys where "" or "*" is the
// catch-all (metadata fills NodeQueryRow.Props for every row, empty map
// included, so callers need not distinguish "no properties" from "not
// asked").
type NodeQuery struct {
	Where     NodePredicate
	Sort      []NodeSort
	Offset    int64
	HasOffset bool
	Limit     int64
	HasLimit  bool
	Fields    []QueryField
	Props     []string
}

// NodeQueryRow is one result row. RepoKey and Path are the identity
// columns (the storage path — the AQL path/name split is a projection of
// it, carried by ParentPath and Name). Sha1/Md5 stay empty unless the
// query pulled the blobs ledger in; the three statistics members stay at
// their structural zeros unless the projection asked for the counting
// columns (M16 T-440 — the single source the ?stats face shares).
type NodeQueryRow struct {
	RepoKey    string
	Path       string // storage path, repo-relative
	ParentPath string // the AQL path field: storage path minus the last segment ('' at root)
	Name       string // last path segment ('' for folder marker rows, ADR-0043 pt 3)
	Type       string // "file" | "folder"
	Depth      int64  // segment count, root children = 1 (aql.md §2.2 "根 folder 起")
	Size       int64
	Sha256     string
	Sha1       string
	Md5        string
	CreatedBy  string
	CreatedAt  string
	UpdatedAt  string
	// DownloadCount/LastDownloadedAt/LastDownloadedBy carry the counting
	// columns when the query projected them (QueryDownloads & family);
	// last_downloaded_* keep their '' = never spelling — the wire-side null
	// rendering is the search package's business, not the store's.
	DownloadCount    int64
	LastDownloadedAt string
	LastDownloadedBy string
	Props            map[string][]string // property projections; nil when none were requested
}

// Derived column expressions (ADR-0043 pt 3 derivation axis A: SQL
// expressions, zero migrations, SQLite/PG same-form functions — rtrim,
// replace, length, substr and CASE behave identically in both dialects).
// The name/parent split rides the rtrim trick: replace(path,'/',”) is the
// character set of every non-slash path byte, so rtrim eats exactly the
// last segment and leaves the prefix through the final slash (folder rows
// keep their trailing slash, so their name arm is the empty string).
const (
	aqlParentExpr = "substr(nodes.path, 1, length(rtrim(nodes.path, replace(nodes.path, '/', ''))) - 1)"
	aqlNameExpr   = "substr(nodes.path, length(rtrim(nodes.path, replace(nodes.path, '/', ''))) + 1)"
	aqlTypeExpr   = "CASE WHEN nodes.path LIKE '%/' THEN 'folder' ELSE 'file' END"
	aqlDepthExpr  = "length(nodes.path) - length(replace(nodes.path, '/', '')) + (CASE WHEN nodes.path LIKE '%/' THEN 0 ELSE 1 END)"
)

// aqlPropBatchRows is how many result rows ride one property-fetch
// statement (two parameters per row: 800 bound parameters per batch, under
// the 999 floor of old SQLite builds).
const aqlPropBatchRows = 400

// QueryNodes implements NodeQueryer.
func (s *nodeStore) QueryNodes(ctx context.Context, q NodeQuery) ([]*NodeQueryRow, error) {
	if q.Where == nil {
		q.Where = &QueryAnd{}
	}
	sqlText, args, err := compileNodeQuery(q)
	if err != nil {
		return nil, fmt.Errorf("metadata: compiling aql node query: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, wrapExec("aql nodes query", "", err)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		return nil, wrapExec("aql nodes query columns", "", err)
	}
	var out []*NodeQueryRow
	for rows.Next() {
		r := &NodeQueryRow{}
		dest := []any{&r.RepoKey, &r.Path, &r.ParentPath, &r.Name, &r.Type,
			&r.Depth, &r.Size, &r.Sha256, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt}
		switch len(cols) {
		case 16: // blobs ledger + the three counting columns
			dest = append(dest, &r.Sha1, &r.Md5,
				&r.DownloadCount, &r.LastDownloadedAt, &r.LastDownloadedBy)
		case 14: // the three counting columns (statistics projection, M16 T-440)
			dest = append(dest, &r.DownloadCount, &r.LastDownloadedAt, &r.LastDownloadedBy)
		case 13: // the blobs ledger joined for sha1/md5
			dest = append(dest, &r.Sha1, &r.Md5)
		case 11:
		default:
			return nil, fmt.Errorf("metadata: aql nodes query: unexpected column count %d", len(cols))
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, wrapExec("aql nodes query scan", "", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapExec("aql nodes query rows", "", err)
	}
	if len(q.Props) > 0 {
		if err := s.fetchQueryProps(ctx, out, q.Props); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// fetchQueryProps fills the property projection of every result row in
// bounded batches keyed on the (repo_key, path) row value — one statement
// per aqlPropBatchRows rows, never per row.
func (s *nodeStore) fetchQueryProps(ctx context.Context, rows []*NodeQueryRow, keys []string) error {
	all := false
	var specific []string
	seen := map[string]bool{}
	for _, k := range keys {
		if k == "" || k == "*" {
			all = true
			continue
		}
		if !seen[k] {
			seen[k] = true
			specific = append(specific, k)
		}
	}
	index := make(map[string]*NodeQueryRow, len(rows))
	for _, r := range rows {
		r.Props = map[string][]string{}
		index[r.RepoKey+"\x00"+r.Path] = r
	}
	for start := 0; start < len(rows); start += aqlPropBatchRows {
		end := min(start+aqlPropBatchRows, len(rows))
		batch := rows[start:end]
		var sb strings.Builder
		var args []any
		sb.WriteString("SELECT repo_key, path, name, value FROM node_props WHERE (repo_key, path) IN (VALUES (?,?)")
		args = append(args, batch[0].RepoKey, batch[0].Path)
		for _, r := range batch[1:] {
			sb.WriteString(",(?,?)") //nolint:gosec // G202: constant fragment; values ride in args
			args = append(args, r.RepoKey, r.Path)
		}
		sb.WriteString(")")
		if !all && len(specific) > 0 {
			sb.WriteString(" AND name IN (" + strings.TrimSuffix(strings.Repeat("?,", len(specific)), ",") + ")") //nolint:gosec // G202: placeholder count only, no data
			for _, k := range specific {
				args = append(args, k)
			}
		}
		sb.WriteString(" ORDER BY repo_key, path, name, value")
		propRows, err := s.db.QueryContext(ctx, sb.String(), args...)
		if err != nil {
			return wrapExec("aql props fetch", "", err)
		}
		for propRows.Next() {
			var rk, pth, name, value string
			if err := propRows.Scan(&rk, &pth, &name, &value); err != nil {
				_ = propRows.Close()
				return wrapExec("aql props fetch scan", "", err)
			}
			if r, ok := index[rk+"\x00"+pth]; ok && (all || seen[name]) {
				r.Props[name] = append(r.Props[name], value)
			}
		}
		if err := propRows.Err(); err != nil {
			_ = propRows.Close()
			return wrapExec("aql props fetch rows", "", err)
		}
		_ = propRows.Close()
	}
	return nil
}

// nodeQueryCompiler assembles one query. Everything it writes into the
// statement text comes from the constant fragments in this file; the only
// dynamic pieces are placeholder counts (IN lists) — caller values reach
// the statement exclusively through args, in the order the placeholders
// appear: joins first, then the WHERE tree left to right, then the window.
type nodeQueryCompiler struct {
	joins     []string
	joinArgs  []any
	whereArgs []any
	joinN     int
	needBlobs bool
	// distinctJoined caps the DISTINCT-subquery joins at one (see
	// whereSQL: two materialized subqueries in one statement plan
	// quadratically).
	distinctJoined bool
}

// compileNodeQuery renders the IR as parameterized SQL.
func compileNodeQuery(q NodeQuery) (string, []any, error) {
	c := &nodeQueryCompiler{}
	// The lazy ledger join triggers on EITHER side: a sha1/md5 comparator
	// sets the flag while the WHERE tree renders, a sha1/md5 projection
	// sets it up front (ADR-0043 pt 3 axis 7-A). The counting columns ride
	// the same lazy rule: only a statistics projection pays for them.
	needStats := false
	for _, f := range q.Fields {
		if f == QuerySha1 || f == QueryMd5 {
			c.needBlobs = true
		}
		if f == QueryDownloaded || f == QueryDownloads || f == QueryDownloadedBy {
			needStats = true
		}
	}
	whereSQL, err := c.whereSQL(q.Where)
	if err != nil {
		return "", nil, err
	}
	orderSQL, err := c.orderSQL(q.Sort)
	if err != nil {
		return "", nil, err
	}

	var sb strings.Builder
	sb.WriteString("SELECT nodes.repo_key, nodes.path, ")
	sb.WriteString(aqlParentExpr)
	sb.WriteString(", ")
	sb.WriteString(aqlNameExpr)
	sb.WriteString(", ")
	sb.WriteString(aqlTypeExpr)
	sb.WriteString(", ")
	sb.WriteString(aqlDepthExpr)
	sb.WriteString(", nodes.size, nodes.sha256, nodes.created_by, nodes.created_at, nodes.updated_at")
	if c.needBlobs {
		sb.WriteString(", b.sha1, b.md5")
	}
	if needStats {
		sb.WriteString(", nodes.download_count, nodes.last_downloaded_at, nodes.last_downloaded_by")
	}
	sb.WriteString(" FROM nodes")
	if c.needBlobs {
		sb.WriteString(" JOIN blobs b ON b.sha256 = nodes.sha256")
	}
	for _, j := range c.joins {
		sb.WriteString(j) //nolint:gosec // G202: fragments are package-constant shapes; values ride in args
	}
	sb.WriteString(" WHERE " + whereSQL) //nolint:gosec // G202: fragments are package-constant shapes; values ride in args
	sb.WriteString(orderSQL)
	args := make([]any, 0, len(c.joinArgs)+len(c.whereArgs)+2)
	args = append(args, c.joinArgs...)
	args = append(args, c.whereArgs...)
	// SQLite has no OFFSET without LIMIT; LIMIT -1 is its no-limit spelling
	// (the postgres arm would simply omit the clause).
	if q.HasLimit {
		sb.WriteString(" LIMIT ?")
		args = append(args, q.Limit)
	} else if q.HasOffset {
		sb.WriteString(" LIMIT -1")
	}
	if q.HasOffset {
		sb.WriteString(" OFFSET ?")
		args = append(args, q.Offset)
	}
	return sb.String(), args, nil
}

// whereSQL renders the predicate tree. Property predicates sitting as
// top-level conjuncts leave the WHERE tree as joins, in three classes
// (ADR-0043 pt 3 — the access path that consumes idx_node_props_name, the
// M10 reserved index; the class split exists because SQLite materializes
// a DISTINCT subquery as an index-less ephemeral table, and TWO of those
// in one statement degrade to a per-probe linear scan — the quadratic
// plan the perf leg caught at ~1.9s):
//
//   - unique predicates (a name equality AND a value equality among their
//     conditions): a DIRECT join on node_props. The four-column primary
//     key makes the match unique per node, so an inner join is a semi-join
//     and row multiplicity — LIMIT/OFFSET semantics — cannot change. The
//     join drives through idx_node_props_name.
//   - the FIRST remaining predicate with any name condition: ONE DISTINCT
//     subquery join (the name-only key-existence shape rides this arm).
//   - everything else: a correlated EXISTS probing the node_props primary
//     key (a covering probe per candidate row; measured well inside the
//     NFR-P67 property budget over the 12k-node corpus).
func (c *nodeQueryCompiler) whereSQL(p NodePredicate) (string, error) {
	if and, ok := p.(*QueryAnd); ok {
		var rest []NodePredicate
		for _, ch := range and.Children {
			done, err := c.hoist(ch)
			if err != nil {
				return "", err
			}
			if !done {
				rest = append(rest, ch)
			}
		}
		if len(rest) == 0 {
			return "1 = 1", nil
		}
		return c.group(rest, " AND ")
	}
	done, err := c.hoist(p)
	if err != nil {
		return "", err
	}
	if done {
		return "1 = 1", nil
	}
	return c.render(p)
}

// hoist moves one top-level conjunct into the join list when it qualifies,
// reporting whether it did. Only AND-position predicates reach this call
// path — anything under an Or/Not keeps its EXISTS rendering, which is
// position-correct by construction.
func (c *nodeQueryCompiler) hoist(p NodePredicate) (bool, error) {
	conds := propCondsOf(p)
	if conds == nil {
		return false, nil
	}
	if uniquePropConds(conds) {
		return true, c.addDirectPropJoin(conds)
	}
	if !c.distinctJoined && hasNameCond(conds) {
		c.distinctJoined = true
		return true, c.addDistinctPropJoin(conds)
	}
	return false, nil
}

// uniquePropConds reports whether the condition set pins one node_props
// row per node: a name equality and a value equality together address the
// four-column primary key exactly.
func uniquePropConds(conds []QueryPropCond) bool {
	var nameEq, valueEq bool
	for _, cd := range conds {
		switch {
		case !cd.OnValue && cd.Op == QueryEq:
			nameEq = true
		case cd.OnValue && cd.Op == QueryEq:
			valueEq = true
		}
	}
	return nameEq && valueEq
}

// hasNameCond reports whether any condition constrains the property name.
func hasNameCond(conds []QueryPropCond) bool {
	for _, cd := range conds {
		if !cd.OnValue {
			return true
		}
	}
	return false
}

// propCondsOf returns the condition list of a property predicate, or nil
// when p is not one.
func propCondsOf(p NodePredicate) []QueryPropCond {
	switch n := p.(type) {
	case *QueryPropExists:
		return n.Conds
	case *QueryPropSame:
		return n.Conds
	}
	return nil
}

// addDirectPropJoin renders a unique property predicate as a direct join
// on the node_props base table. The correlation arms come first, then the
// equality pair, then any remaining conditions — canonical order keeps the
// snapshot deterministic.
func (c *nodeQueryCompiler) addDirectPropJoin(conds []QueryPropCond) error {
	alias := fmt.Sprintf("np%d", c.joinN)
	c.joinN++
	var sb strings.Builder
	sb.WriteString(" JOIN node_props " + alias + " ON " + alias + ".repo_key = nodes.repo_key AND " + alias + ".path = nodes.path") //nolint:gosec // G202: alias is compiler-generated
	var nameEq, valueEq *QueryPropCond
	var rest []QueryPropCond
	for i := range conds {
		cd := conds[i]
		switch {
		case nameEq == nil && !cd.OnValue && cd.Op == QueryEq:
			nameEq = &conds[i]
		case valueEq == nil && cd.OnValue && cd.Op == QueryEq:
			valueEq = &conds[i]
		default:
			rest = append(rest, cd)
		}
	}
	sb.WriteString(" AND " + alias + ".name = ?")
	c.joinArgs = append(c.joinArgs, nameEq.Value)
	sb.WriteString(" AND " + alias + ".value = ?")
	c.joinArgs = append(c.joinArgs, valueEq.Value)
	for _, cd := range rest {
		col := alias + ".name"
		if cd.OnValue {
			col = alias + ".value"
		}
		frag, err := c.opFragment(cd.Op, col, false)
		if err != nil {
			return err
		}
		sb.WriteString(" AND " + frag)
		c.joinArgs = append(c.joinArgs, cd.Value)
	}
	c.joins = append(c.joins, sb.String())
	return nil
}

// addDistinctPropJoin renders one non-unique name-constrained predicate as
// a join over the DISTINCT (repo_key, path) subquery.
func (c *nodeQueryCompiler) addDistinctPropJoin(conds []QueryPropCond) error {
	alias := fmt.Sprintf("np%d", c.joinN)
	c.joinN++
	var sb strings.Builder
	sb.WriteString(" JOIN (SELECT DISTINCT repo_key, path FROM node_props WHERE ")
	body, err := c.condList(conds, "node_props.name", "node_props.value", &c.joinArgs)
	if err != nil {
		return err
	}
	sb.WriteString(body)
	sb.WriteString(") " + alias + " ON " + alias + ".repo_key = nodes.repo_key AND " + alias + ".path = nodes.path") //nolint:gosec // G202: alias is compiler-generated
	c.joins = append(c.joins, sb.String())
	return nil
}

// group renders children joined by sep inside parentheses (single children
// keep the parentheses: one canonical shape per tree node).
func (c *nodeQueryCompiler) group(children []NodePredicate, sep string) (string, error) {
	parts := make([]string, 0, len(children))
	for _, ch := range children {
		s, err := c.render(ch)
		if err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	return "(" + strings.Join(parts, sep) + ")", nil //nolint:gosec // G202: fragments are package-constant shapes; values ride in args
}

// render renders one predicate node in WHERE position.
func (c *nodeQueryCompiler) render(p NodePredicate) (string, error) {
	switch n := p.(type) {
	case *QueryAnd:
		if len(n.Children) == 0 {
			return "1 = 1", nil
		}
		return c.group(n.Children, " AND ")
	case *QueryOr:
		if len(n.Children) == 0 {
			return "", fmt.Errorf("metadata: aql compile: empty disjunction predicate")
		}
		return c.group(n.Children, " OR ")
	case *QueryNot:
		inner, err := c.render(n.Child)
		if err != nil {
			return "", err
		}
		return "(NOT " + inner + ")", nil //nolint:gosec // G202: fragments are package-constant shapes; values ride in args
	case *QueryFalse:
		return "1 = 0", nil
	case *QueryFolderTest:
		if n.Folder {
			return "nodes.path LIKE '%/'", nil
		}
		return "nodes.path NOT LIKE '%/'", nil
	case *QueryCompare:
		return c.renderCompare(n)
	case *QueryPropExists, *QueryPropSame:
		// The EXISTS probed row IS one node_props instance, so the MSP
		// same-instance semantics hold in this position for free; the
		// hoisted arm keeps them by keeping all conditions inside the one
		// DISTINCT subquery.
		return c.renderExists(propCondsOf(n))
	case *QueryZero:
		return c.renderZero(n)
	}
	return "", fmt.Errorf("metadata: aql compile: unknown predicate %T", p)
}

// renderZero renders the structural-zero test of one statistics field
// (QueryZero): the single place the wire-side null and the storage-side
// zero spellings meet. The RAW column is compared (never the julianday
// projection) — ” is a text value, not a date.
func (c *nodeQueryCompiler) renderZero(z *QueryZero) (string, error) {
	col, err := c.statsColumn(z.Field)
	if err != nil {
		return "", err
	}
	zero, err := statsZeroValue(z.Field)
	if err != nil {
		return "", err
	}
	c.whereArgs = append(c.whereArgs, zero)
	op := " = "
	if z.Negate {
		op = " <> "
	}
	return col + op + "?", nil //nolint:gosec // G202: constant fragment; the zero rides in args
}

// statsColumn maps a statistics query field onto its counting column and
// reports the column's structural zero (the value the wire renders as
// null, aql.md §14.1).
func (c *nodeQueryCompiler) statsColumn(f QueryField) (string, error) {
	switch f {
	case QueryDownloads:
		return "nodes.download_count", nil
	case QueryDownloaded:
		return "nodes.last_downloaded_at", nil
	case QueryDownloadedBy:
		return "nodes.last_downloaded_by", nil
	}
	return "", fmt.Errorf("metadata: aql compile: field %q has no zero-value form", f)
}

// statsZeroValue is the bound parameter of a QueryZero arm: the column's
// structural zero (0 for the counter, ” for the never-spellings).
func statsZeroValue(f QueryField) (any, error) {
	switch f {
	case QueryDownloads:
		return int64(0), nil
	case QueryDownloaded, QueryDownloadedBy:
		return "", nil
	}
	return nil, fmt.Errorf("metadata: aql compile: field %q has no zero-value form", f)
}

// renderExists renders a property predicate in non-hoistable position as a
// correlated EXISTS over one node_props row.
func (c *nodeQueryCompiler) renderExists(conds []QueryPropCond) (string, error) {
	var sb strings.Builder
	sb.WriteString("EXISTS (SELECT 1 FROM node_props np WHERE np.repo_key = nodes.repo_key AND np.path = nodes.path")
	if len(conds) > 0 {
		sb.WriteString(" AND ")
		body, err := c.condList(conds, "np.name", "np.value", &c.whereArgs)
		if err != nil {
			return "", err
		}
		sb.WriteString(body)
	}
	sb.WriteString(")")
	return sb.String(), nil
}

// condList renders property conditions against the given column spellings,
// appending each value to args in placeholder order.
func (c *nodeQueryCompiler) condList(conds []QueryPropCond, nameCol, valueCol string, args *[]any) (string, error) {
	parts := make([]string, 0, len(conds))
	for _, cd := range conds {
		col := nameCol
		if cd.OnValue {
			col = valueCol
		}
		frag, err := c.opFragment(cd.Op, col, false)
		if err != nil {
			return "", err
		}
		*args = append(*args, cd.Value)
		parts = append(parts, frag)
	}
	return strings.Join(parts, " AND "), nil //nolint:gosec // G202: fragments are package-constant shapes; values ride in args
}

// renderCompare renders one field comparator.
func (c *nodeQueryCompiler) renderCompare(cmp *QueryCompare) (string, error) {
	expr, date, err := c.fieldExpr(cmp.Field)
	if err != nil {
		return "", err
	}
	frag, err := c.opFragment(cmp.Op, expr, date)
	if err != nil {
		return "", err
	}
	c.whereArgs = append(c.whereArgs, cmp.Value)
	return frag, nil
}

// fieldExpr maps a query field onto its SQL expression. Date fields report
// date=true so their comparator wraps the bound parameter in the
// normalization function as well — RFC3339 TEXT comparison is unsound
// under mixed fractional-second and zone-offset spellings (ADR-0043 pt 3:
// SQLite julianday(x) on both sides; the postgres arm of this single point
// is x::timestamptz).
func (c *nodeQueryCompiler) fieldExpr(f QueryField) (expr string, date bool, err error) {
	switch f {
	case QueryRepo:
		return "nodes.repo_key", false, nil
	case QueryPath:
		return aqlParentExpr, false, nil
	case QueryName:
		return aqlNameExpr, false, nil
	case QueryDepth:
		return aqlDepthExpr, false, nil
	case QuerySize:
		return "nodes.size", false, nil
	case QueryCreated:
		return "julianday(nodes.created_at)", true, nil
	case QueryModified, QueryUpdated:
		return "julianday(nodes.updated_at)", true, nil
	case QueryCreatedBy:
		return "nodes.created_by", false, nil
	case QuerySha256:
		return "nodes.sha256", false, nil
	case QuerySha1:
		c.needBlobs = true
		return "b.sha1", false, nil
	case QueryMd5:
		c.needBlobs = true
		return "b.md5", false, nil
	case QueryDownloaded:
		// Date-normalized like created/updated: the never spelling ''
		// lands at julianday NULL, so every bare comparison excludes
		// never-downloaded rows — the wire-side rule the null literal's
		// QueryZero arm spells out (aql.md §14.1).
		return "julianday(nodes.last_downloaded_at)", true, nil
	case QueryDownloads:
		return "nodes.download_count", false, nil
	case QueryDownloadedBy:
		return "nodes.last_downloaded_by", false, nil
	}
	return "", false, fmt.Errorf("metadata: aql compile: unknown query field %q", f)
}

// opFragment renders "<expr> <op> <param>". Like patterns carry the
// ESCAPE '\' clause: the pattern argument itself is produced by the single
// wildcard translator in internal/search (match.go), which escapes the
// literal %, _ and \ bytes.
func (c *nodeQueryCompiler) opFragment(op QueryOp, expr string, date bool) (string, error) {
	switch op {
	case QueryLike:
		return expr + " LIKE ? ESCAPE '\\'", nil //nolint:gosec // G202: constant fragment; the pattern rides in args
	case QueryNotLike:
		return expr + " NOT LIKE ? ESCAPE '\\'", nil //nolint:gosec // G202: constant fragment; the pattern rides in args
	}
	var sqlop string
	switch op {
	case QueryEq:
		sqlop = "="
	case QueryNe:
		sqlop = "<>"
	case QueryGt:
		sqlop = ">"
	case QueryGte:
		sqlop = ">="
	case QueryLt:
		sqlop = "<"
	case QueryLte:
		sqlop = "<="
	default:
		return "", fmt.Errorf("metadata: aql compile: unknown query op %q", op)
	}
	if date {
		return expr + " " + sqlop + " julianday(?)", nil //nolint:gosec // G202: constant fragment; the bound date rides in args
	}
	return expr + " " + sqlop + " ?", nil //nolint:gosec // G202: constant fragment; values ride in args
}

// orderSQL renders the ORDER BY: user keys first, then the constant
// (repo_key, path) tiebreaker (page determinism, ADR-0043 pt 3).
func (c *nodeQueryCompiler) orderSQL(sorts []NodeSort) (string, error) {
	parts := make([]string, 0, len(sorts)+2)
	for _, s := range sorts {
		expr, _, err := c.fieldExpr(s.Field)
		if err != nil {
			return "", err
		}
		dir := " DESC"
		if s.Asc {
			dir = " ASC"
		}
		parts = append(parts, expr+dir)
	}
	parts = append(parts, "nodes.repo_key", "nodes.path")
	return " ORDER BY " + strings.Join(parts, ", "), nil //nolint:gosec // G202: fragments are package-constant shapes
}
