package metadata

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// T-411 AC1/AC2 over the query seam: IR→parameterized-SQL snapshot table
// (the storage leg of the AST→SQL chain — the planner leg lives in
// internal/search/plan_test.go and the two are pinned to the same IR by
// the end-to-end chain test), the zero-concatenation injection red line,
// idx_node_props_name consumption (the M10 redemption), and the executor's
// behavioral contracts on seeded rows.

// aqlSelectPrefix is the constant projection prefix of every compiled
// query: identity pair, the four derived columns, then the storage
// columns. It is pinned literally so any drift in the derivation
// expressions (verified against live SQLite semantics) fails loudly.
const aqlSelectPrefix = "SELECT nodes.repo_key, nodes.path, " +
	"substr(nodes.path, 1, length(rtrim(nodes.path, replace(nodes.path, '/', ''))) - 1), " +
	"substr(nodes.path, length(rtrim(nodes.path, replace(nodes.path, '/', ''))) + 1), " +
	"CASE WHEN nodes.path LIKE '%/' THEN 'folder' ELSE 'file' END, " +
	"length(nodes.path) - length(replace(nodes.path, '/', '')) + (CASE WHEN nodes.path LIKE '%/' THEN 0 ELSE 1 END), " +
	"nodes.size, nodes.sha256, nodes.created_by, nodes.created_at, nodes.updated_at"

// aqlBlobsTail is what the lazy ledger join adds to the projection.
const aqlBlobsTail = ", b.sha1, b.md5 FROM nodes JOIN blobs b ON b.sha256 = nodes.sha256"

// directPropJoin is the unique-predicate join shape (name+value equality
// addressing the node_props primary key exactly).
func directPropJoin(alias string) string {
	return " JOIN node_props " + alias + " ON " + alias + ".repo_key = nodes.repo_key AND " + alias +
		".path = nodes.path AND " + alias + ".name = ? AND " + alias + ".value = ?"
}

// distinctPropJoin is the non-unique name-leading join shape (one per
// statement).
func distinctPropJoin(alias, body string) string {
	return " JOIN (SELECT DISTINCT repo_key, path FROM node_props WHERE " + body +
		") " + alias + " ON " + alias + ".repo_key = nodes.repo_key AND " + alias + ".path = nodes.path"
}

func TestCompileNodeQuerySnapshot(t *testing.T) {
	tests := []struct {
		name     string
		query    NodeQuery
		wantSQL  string
		wantArgs []any
	}{
		{
			name:     "match all files (planner shape of items.find({}))",
			query:    NodeQuery{Where: &QueryAnd{Children: []NodePredicate{&QueryFolderTest{}}}},
			wantSQL:  aqlSelectPrefix + " FROM nodes WHERE (nodes.path NOT LIKE '%/') ORDER BY nodes.repo_key, nodes.path",
			wantArgs: nil,
		},
		{
			name: "repo equality with window",
			query: NodeQuery{
				Where: &QueryAnd{Children: []NodePredicate{
					&QueryCompare{Field: QueryRepo, Op: QueryEq, Value: "libs"},
					&QueryFolderTest{},
				}},
				Offset: 5, HasOffset: true, Limit: 10, HasLimit: true,
			},
			wantSQL:  aqlSelectPrefix + " FROM nodes WHERE (nodes.repo_key = ? AND nodes.path NOT LIKE '%/') ORDER BY nodes.repo_key, nodes.path LIMIT ? OFFSET ?",
			wantArgs: []any{"libs", int64(10), int64(5)},
		},
		{
			name: "offset without limit uses sqlite no-limit spelling",
			query: NodeQuery{
				Where:  &QueryAnd{Children: []NodePredicate{&QueryFolderTest{}}},
				Offset: 20, HasOffset: true,
			},
			wantSQL:  aqlSelectPrefix + " FROM nodes WHERE (nodes.path NOT LIKE '%/') ORDER BY nodes.repo_key, nodes.path LIMIT -1 OFFSET ?",
			wantArgs: []any{int64(20)},
		},
		{
			name:  "name wildcard match escapes",
			query: NodeQuery{Where: &QueryCompare{Field: QueryName, Op: QueryLike, Value: "%.jar"}},
			wantSQL: aqlSelectPrefix + " FROM nodes WHERE " + aqlNameExpr +
				" LIKE ? ESCAPE '\\' ORDER BY nodes.repo_key, nodes.path",
			wantArgs: []any{"%.jar"},
		},
		{
			name:  "path compares the derived parent directory",
			query: NodeQuery{Where: &QueryCompare{Field: QueryPath, Op: QueryEq, Value: "org/foo"}},
			wantSQL: aqlSelectPrefix + " FROM nodes WHERE " + aqlParentExpr +
				" = ? ORDER BY nodes.repo_key, nodes.path",
			wantArgs: []any{"org/foo"},
		},
		{
			name:     "folder test positive",
			query:    NodeQuery{Where: &QueryFolderTest{Folder: true}},
			wantSQL:  aqlSelectPrefix + " FROM nodes WHERE nodes.path LIKE '%/' ORDER BY nodes.repo_key, nodes.path",
			wantArgs: nil,
		},
		{
			name:     "negated folder test (type $ne folder)",
			query:    NodeQuery{Where: &QueryNot{Child: &QueryFolderTest{Folder: true}}},
			wantSQL:  aqlSelectPrefix + " FROM nodes WHERE (NOT nodes.path LIKE '%/') ORDER BY nodes.repo_key, nodes.path",
			wantArgs: nil,
		},
		{
			name:     "unsatisfiable predicate",
			query:    NodeQuery{Where: &QueryFalse{}},
			wantSQL:  aqlSelectPrefix + " FROM nodes WHERE 1 = 0 ORDER BY nodes.repo_key, nodes.path",
			wantArgs: nil,
		},
		{
			name:     "depth inequality carries int64 arg",
			query:    NodeQuery{Where: &QueryCompare{Field: QueryDepth, Op: QueryGte, Value: int64(3)}},
			wantSQL:  aqlSelectPrefix + " FROM nodes WHERE " + aqlDepthExpr + " >= ? ORDER BY nodes.repo_key, nodes.path",
			wantArgs: []any{int64(3)},
		},
		{
			name:     "date comparison normalizes both sides",
			query:    NodeQuery{Where: &QueryCompare{Field: QueryCreated, Op: QueryGt, Value: "2026-08-01T00:00:00Z"}},
			wantSQL:  aqlSelectPrefix + " FROM nodes WHERE julianday(nodes.created_at) > julianday(?) ORDER BY nodes.repo_key, nodes.path",
			wantArgs: []any{"2026-08-01T00:00:00Z"},
		},
		{
			name: "modified and updated share the updated_at column",
			query: NodeQuery{Where: &QueryOr{Children: []NodePredicate{
				&QueryCompare{Field: QueryModified, Op: QueryLt, Value: "2026-01-01T00:00:00Z"},
				&QueryCompare{Field: QueryUpdated, Op: QueryGte, Value: "2026-09-01T00:00:00Z"},
			}}},
			wantSQL:  aqlSelectPrefix + " FROM nodes WHERE (julianday(nodes.updated_at) < julianday(?) OR julianday(nodes.updated_at) >= julianday(?)) ORDER BY nodes.repo_key, nodes.path",
			wantArgs: []any{"2026-01-01T00:00:00Z", "2026-09-01T00:00:00Z"},
		},
		{
			name:     "sha1 filter pulls the blobs ledger in lazily",
			query:    NodeQuery{Where: &QueryCompare{Field: QuerySha1, Op: QueryEq, Value: "8ddcdd9c896b4f0f27101e79ed114466bf7860a8"}},
			wantSQL:  aqlSelectPrefix + aqlBlobsTail + " WHERE b.sha1 = ? ORDER BY nodes.repo_key, nodes.path",
			wantArgs: []any{"8ddcdd9c896b4f0f27101e79ed114466bf7860a8"},
		},
		{
			name: "unique property predicate becomes a direct base-table join",
			query: NodeQuery{Where: &QueryPropExists{Conds: []QueryPropCond{
				{OnValue: false, Op: QueryEq, Value: "license"},
				{OnValue: true, Op: QueryEq, Value: "Apache-2.0"},
			}}},
			wantSQL: aqlSelectPrefix + " FROM nodes" + directPropJoin("np0") +
				" WHERE 1 = 1 ORDER BY nodes.repo_key, nodes.path",
			wantArgs: []any{"license", "Apache-2.0"},
		},
		{
			name: "key existence rides the one DISTINCT join slot",
			query: NodeQuery{Where: &QueryPropExists{Conds: []QueryPropCond{
				{OnValue: false, Op: QueryEq, Value: "license"},
			}}},
			wantSQL: aqlSelectPrefix + " FROM nodes" +
				distinctPropJoin("np0", "node_props.name = ?") +
				" WHERE 1 = 1 ORDER BY nodes.repo_key, nodes.path",
			wantArgs: []any{"license"},
		},
		{
			name: "unique joins direct, non-unique name-leading joins distinct, value-only stays EXISTS",
			query: NodeQuery{Where: &QueryAnd{Children: []NodePredicate{
				&QueryPropExists{Conds: []QueryPropCond{
					{OnValue: false, Op: QueryEq, Value: "env"},
					{OnValue: true, Op: QueryEq, Value: "prod"},
				}},
				&QueryCompare{Field: QueryRepo, Op: QueryEq, Value: "libs"},
				&QueryPropSame{Conds: []QueryPropCond{
					{OnValue: false, Op: QueryEq, Value: "build"},
					{OnValue: true, Op: QueryLike, Value: "b1%"},
				}},
				&QueryPropExists{Conds: []QueryPropCond{
					{OnValue: false, Op: QueryEq, Value: "team"},
					{OnValue: true, Op: QueryLike, Value: "core%"},
				}},
			}}},
			wantSQL: aqlSelectPrefix + " FROM nodes" +
				directPropJoin("np0") +
				distinctPropJoin("np1", "node_props.name = ? AND node_props.value LIKE ? ESCAPE '\\'") +
				" WHERE (nodes.repo_key = ?" +
				" AND EXISTS (SELECT 1 FROM node_props np WHERE np.repo_key = nodes.repo_key AND np.path = nodes.path" +
				" AND np.name = ? AND np.value LIKE ? ESCAPE '\\'))" +
				" ORDER BY nodes.repo_key, nodes.path",
			wantArgs: []any{"env", "prod", "build", "b1%", "libs", "team", "core%"},
		},
		{
			name: "property predicate under a disjunction stays a correlated EXISTS",
			query: NodeQuery{Where: &QueryOr{Children: []NodePredicate{
				&QueryPropExists{Conds: []QueryPropCond{{OnValue: true, Op: QueryEq, Value: "v"}}},
				&QueryCompare{Field: QueryRepo, Op: QueryEq, Value: "libs"},
			}}},
			wantSQL: aqlSelectPrefix + " FROM nodes WHERE (EXISTS (SELECT 1 FROM node_props np WHERE " +
				"np.repo_key = nodes.repo_key AND np.path = nodes.path AND np.value = ?) OR nodes.repo_key = ?) " +
				"ORDER BY nodes.repo_key, nodes.path",
			wantArgs: []any{"v", "libs"},
		},
		{
			name:  "empty property condition list is the any-property test",
			query: NodeQuery{Where: &QueryPropExists{}},
			wantSQL: aqlSelectPrefix + " FROM nodes WHERE EXISTS (SELECT 1 FROM node_props np WHERE " +
				"np.repo_key = nodes.repo_key AND np.path = nodes.path) ORDER BY nodes.repo_key, nodes.path",
			wantArgs: nil,
		},
		{
			name: "user sort keys precede the constant tiebreaker",
			query: NodeQuery{
				Where: &QueryAnd{Children: []NodePredicate{&QueryFolderTest{}}},
				Sort: []NodeSort{
					{Field: QueryCreated, Asc: false},
					{Field: QueryName, Asc: true},
				},
			},
			wantSQL: aqlSelectPrefix + " FROM nodes WHERE (nodes.path NOT LIKE '%/') ORDER BY " +
				"julianday(nodes.created_at) DESC, " + aqlNameExpr + " ASC, nodes.repo_key, nodes.path",
			wantArgs: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs, err := compileNodeQuery(tt.query)
			if err != nil {
				t.Fatalf("compileNodeQuery: %v", err)
			}
			if gotSQL != tt.wantSQL {
				t.Errorf("sql:\n got %s\nwant %s", gotSQL, tt.wantSQL)
			}
			if fmt.Sprint(gotArgs) != fmt.Sprint(tt.wantArgs) {
				t.Errorf("args:\n got %#v\nwant %#v", gotArgs, tt.wantArgs)
			}
		})
	}
}

// TestCompileNodeQueryRejectsUnknownParts pins the defensive arms: the
// planner cannot produce them, a hand-built IR gets an honest error.
func TestCompileNodeQueryRejectsUnknownParts(t *testing.T) {
	if _, _, err := compileNodeQuery(NodeQuery{Where: &QueryCompare{Field: "bogus", Op: QueryEq, Value: "x"}}); err == nil {
		t.Fatal("unknown field must fail")
	}
	if _, _, err := compileNodeQuery(NodeQuery{Where: &QueryCompare{Field: QueryRepo, Op: "bogus", Value: "x"}}); err == nil {
		t.Fatal("unknown op must fail")
	}
	if _, _, err := compileNodeQuery(NodeQuery{Where: &QueryOr{}}); err == nil {
		t.Fatal("empty disjunction must fail")
	}
	if _, _, err := compileNodeQuery(NodeQuery{Where: fakePredicate{}}); err == nil {
		t.Fatal("unknown predicate must fail")
	}
}

// hostileValues is the AC2 injection corpus: quotes, statement
// terminators, comment openers, SQL wildcards, control bytes and unicode.
var hostileValues = []string{
	`x' OR '1'='1`,
	`'); DROP TABLE nodes; --`,
	`" UNION SELECT password_hash FROM users --`,
	`/* block */ -- line`,
	`a%b_c\d`,
	`日本語/資料-01.txt`,
	"line\nbreak\ttab\x00nul",
	`*/';`,
}

// TestCompileZeroConcatenationInjection is the injection red line
// (NFR-S74): the statement text is a pure function of the query SHAPE —
// for a fixed shape, hostile and benign values compile to byte-identical
// SQL, and the value reaches the statement only as a parameter.
func TestCompileZeroConcatenationInjection(t *testing.T) {
	const benign = "benign-value"
	shapes := []struct {
		name  string
		build func(v string) NodeQuery
	}{
		{"string equality", func(v string) NodeQuery {
			return NodeQuery{Where: &QueryCompare{Field: QueryRepo, Op: QueryEq, Value: v}}
		}},
		{"string inequality", func(v string) NodeQuery {
			return NodeQuery{Where: &QueryCompare{Field: QueryName, Op: QueryNe, Value: v}}
		}},
		{"wildcard match", func(v string) NodeQuery {
			return NodeQuery{Where: &QueryCompare{Field: QueryPath, Op: QueryLike, Value: LikePatternForTest(v)}}
		}},
		{"date bound", func(v string) NodeQuery {
			return NodeQuery{Where: &QueryCompare{Field: QueryCreated, Op: QueryGt, Value: v}}
		}},
		{"checksum via ledger join", func(v string) NodeQuery {
			return NodeQuery{Where: &QueryCompare{Field: QuerySha1, Op: QueryEq, Value: v}}
		}},
		{"property value", func(v string) NodeQuery {
			return NodeQuery{Where: &QueryPropExists{Conds: []QueryPropCond{
				{OnValue: false, Op: QueryEq, Value: v},
				{OnValue: true, Op: QueryEq, Value: v},
			}}}
		}},
		{"property value under disjunction", func(v string) NodeQuery {
			return NodeQuery{Where: &QueryOr{Children: []NodePredicate{
				&QueryPropExists{Conds: []QueryPropCond{{OnValue: true, Op: QueryEq, Value: v}}},
				&QueryCompare{Field: QueryRepo, Op: QueryEq, Value: v},
			}}}
		}},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			baseSQL, _, err := compileNodeQuery(shape.build(benign))
			if err != nil {
				t.Fatalf("benign compile: %v", err)
			}
			for _, hostile := range hostileValues {
				gotSQL, gotArgs, err := compileNodeQuery(shape.build(hostile))
				if err != nil {
					t.Fatalf("hostile compile (%q): %v", hostile, err)
				}
				if gotSQL != baseSQL {
					t.Errorf("hostile value %q changed the statement text:\n got %s\nbase %s", hostile, gotSQL, baseSQL)
				}
				if strings.Contains(gotSQL, hostile) {
					t.Errorf("hostile value %q leaked into the statement text", hostile)
				}
				found := false
				for _, a := range gotArgs {
					if s, ok := a.(string); ok && (s == hostile || s == LikePatternForTest(hostile)) {
						found = true
					}
				}
				if !found {
					t.Errorf("hostile value %q not carried in args (%#v)", hostile, gotArgs)
				}
			}
		})
	}
}

// LikePatternForTest mirrors what the planner produces for a $match value:
// the translated LIKE pattern. It lives here so the metadata-side injection
// table exercises the escaped-pattern arm with the real byte shape the
// search kernel emits (the kernel itself is tested in internal/search).
func LikePatternForTest(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		switch c := p[i]; c {
		case '*':
			b.WriteByte('%')
		case '?':
			b.WriteByte('_')
		case '%', '_', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// TestAQLIndexConsumption pins the access paths (AC2, M10 redemption):
// the hoisted property join must SEEK through idx_node_props_name — never
// full-scan node_props — the nested EXISTS arm plans through the
// node_props primary key, and the lazy blobs join seeks the ledger.
func TestAQLIndexConsumption(t *testing.T) {
	st := openTest(t)
	db := st.(*sqliteStore).db
	ctx := context.Background()
	putRepo(t, st, "libs")
	const ts0 = "2026-08-01T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO blobs (sha256, sha1, md5, size, created_at) VALUES (?,?,?,?,?)`,
		strings.Repeat("a", 64), "sha1-aaa", "md5-aaa", 10, ts0); err != nil {
		t.Fatalf("seed blob: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO nodes (repo_key, path, sha256, size, mime, created_by, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, "libs", "org/lib/1.0.jar", strings.Repeat("a", 64), 10, "application/octet-stream", "u1", ts0, ts0); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	if err := st.NodeProps().Merge(ctx, "libs", "org/lib/1.0.jar", map[string][]string{"license": {"Apache-2.0"}}); err != nil {
		t.Fatalf("merge props: %v", err)
	}

	cases := []struct {
		name  string
		query NodeQuery
		want  string
		bad   string
	}{
		{
			name: "direct property join seeks idx_node_props_name",
			query: NodeQuery{Where: &QueryPropExists{Conds: []QueryPropCond{
				{OnValue: false, Op: QueryEq, Value: "license"},
				{OnValue: true, Op: QueryEq, Value: "Apache-2.0"},
			}}},
			want: "SEARCH np0 USING INDEX idx_node_props_name (name=? AND value=?)",
			bad:  "SCAN np0",
		},
		{
			name: "key-existence distinct join seeks the name arm",
			query: NodeQuery{Where: &QueryPropExists{Conds: []QueryPropCond{
				{OnValue: false, Op: QueryEq, Value: "license"},
			}}},
			want: "SEARCH node_props USING INDEX idx_node_props_name (name=?)",
			bad:  "SCAN node_props",
		},
		{
			name: "nested EXISTS arm probes the node_props primary key",
			query: NodeQuery{Where: &QueryOr{Children: []NodePredicate{
				&QueryPropExists{Conds: []QueryPropCond{{OnValue: true, Op: QueryEq, Value: "Apache-2.0"}}},
				&QueryCompare{Field: QueryRepo, Op: QueryEq, Value: "other"},
			}}},
			want: "SEARCH np USING COVERING INDEX sqlite_autoindex_node_props_1",
			bad:  "SCAN np",
		},
		{
			// The sha1-keyed join rides idx_blobs_sha1 — the T-73 seam —
			// then reaches nodes through idx_nodes_blob; no full scans.
			name:  "lazy blobs join seeks the ledger through idx_blobs_sha1",
			query: NodeQuery{Where: &QueryCompare{Field: QuerySha1, Op: QueryEq, Value: "sha1-aaa"}},
			want:  "SEARCH b USING INDEX idx_blobs_sha1 (sha1=?)",
			bad:   "SCAN b",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sqlText, args, err := compileNodeQuery(tc.query)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			plans := planOf(t, db, sqlText, args...)
			t.Logf("plan: %v", plans)
			if !planContains(plans, tc.want) {
				t.Fatalf("plan %v does not contain %q", plans, tc.want)
			}
			if planContains(plans, tc.bad) {
				t.Fatalf("plan %v contains forbidden %q", plans, tc.bad)
			}
		})
	}
}

// fakePredicate pins the compiler's honest rejection of hand-built IR.
type fakePredicate struct{}

func (fakePredicate) isNodePredicate() {}

// TestQueryNodesBehavior exercises the executor on seeded rows: derived
// columns, date normalization, the any-of property semantics, the MSP
// same-instance rule, paging determinism, property projection and the
// lazy ledger join.
func TestQueryNodesBehavior(t *testing.T) {
	st := openTest(t)
	db := st.(*sqliteStore).db
	ctx := context.Background()
	putRepo(t, st, "libs")
	putRepo(t, st, "libs2")
	const ts = "2026-08-01T00:00:00Z"
	raw := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	// Distinct blobs so the ledger join discriminates.
	raw(`INSERT INTO blobs (sha256, sha1, md5, size, created_at) VALUES (?,?,?,?,?)`,
		strings.Repeat("a", 64), "sha1-aaa", "md5-aaa", 10, ts)
	raw(`INSERT INTO blobs (sha256, sha1, md5, size, created_at) VALUES (?,?,?,?,?)`,
		strings.Repeat("b", 64), "sha1-bbb", "md5-bbb", 20, ts)
	// The folder-marker sentinel blob every trailing-slash row references.
	raw(`INSERT INTO blobs (sha256, sha1, md5, size, created_at) VALUES (?,?,?,?,?)`,
		FolderMarkerSHA, "", "", 0, ts)
	// Files at several depths, one unicode name, one fractional-second
	// timestamp, plus folder marker rows.
	rows := []struct{ repo, path, sha, by, at string }{
		{"libs", "root.bin", strings.Repeat("a", 64), "alice", "2026-08-01T10:00:00Z"},
		{"libs", "org/lib-1.0.jar", strings.Repeat("a", 64), "alice", "2026-08-02T10:00:00Z"},
		{"libs", "org/sub/lib-2.0.jar", strings.Repeat("b", 64), "bob", "2026-08-03T10:00:30.500Z"},
		{"libs", "日本語/資料-01.txt", strings.Repeat("a", 64), "alice", "2026-08-04T10:00:00Z"},
		{"libs2", "other/nested/deep/x.bin", strings.Repeat("b", 64), "carol", "2026-07-31T10:00:00Z"},
		{"libs", "org/", strings.Repeat("0", 64), "alice", ts},
		{"libs", "org/sub/", strings.Repeat("0", 64), "alice", ts},
	}
	for _, r := range rows {
		raw(`INSERT INTO nodes (repo_key, path, sha256, size, mime, created_by, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?)`, r.repo, r.path, r.sha, 5, "application/octet-stream", r.by, r.at, r.at)
	}
	// env is multi-valued on the unicode node: the any-of row semantics
	// and the MSP same-instance rule both discriminate on it.
	if err := st.NodeProps().Merge(ctx, "libs", "org/lib-1.0.jar", map[string][]string{
		"license": {"Apache-2.0"},
	}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if err := st.NodeProps().Merge(ctx, "libs", "日本語/資料-01.txt", map[string][]string{
		"env": {"prod", "dev"},
	}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	q := func(nq NodeQuery) []*NodeQueryRow {
		t.Helper()
		got, err := st.Nodes().(NodeQueryer).QueryNodes(ctx, nq)
		if err != nil {
			t.Fatalf("QueryNodes: %v", err)
		}
		return got
	}

	t.Run("derived columns", func(t *testing.T) {
		got := q(NodeQuery{Where: &QueryCompare{Field: QueryRepo, Op: QueryEq, Value: "libs"}, Limit: 100, HasLimit: true})
		byPath := map[string]*NodeQueryRow{}
		for _, r := range got {
			byPath[r.Path] = r
		}
		want := map[string][4]any{ // parent, name, type, depth
			"root.bin":            {"", "root.bin", "file", int64(1)},
			"org/lib-1.0.jar":     {"org", "lib-1.0.jar", "file", int64(2)},
			"org/sub/lib-2.0.jar": {"org/sub", "lib-2.0.jar", "file", int64(3)},
			"日本語/資料-01.txt":       {"日本語", "資料-01.txt", "file", int64(2)},
			"org/":                {"org", "", "folder", int64(1)},
			"org/sub/":            {"org/sub", "", "folder", int64(2)},
		}
		if len(got) != len(want) {
			t.Fatalf("got %d rows, want %d: %v", len(got), len(want), got)
		}
		for path, w := range want {
			r := byPath[path]
			if r == nil {
				t.Fatalf("missing row %s", path)
			}
			if r.ParentPath != w[0] || r.Name != w[1] || r.Type != w[2] || r.Depth != w[3] {
				t.Errorf("%s = (parent=%q name=%q type=%q depth=%d), want %v", path, r.ParentPath, r.Name, r.Type, r.Depth, w)
			}
		}
	})

	t.Run("date comparison normalizes fractional seconds", func(t *testing.T) {
		// Bare text comparison orders '.5' BEFORE 'Z', so the stored
		// .500 variant would sort before its own plain second and both
		// bounds below would answer wrong; julianday keeps chronology.
		afterPlain := q(NodeQuery{Where: &QueryAnd{Children: []NodePredicate{
			&QueryCompare{Field: QueryCreated, Op: QueryGt, Value: "2026-08-03T10:00:30Z"},
			&QueryFolderTest{},
		}}})
		if len(afterPlain) != 2 || afterPlain[0].Path != "org/sub/lib-2.0.jar" || afterPlain[1].Path != "日本語/資料-01.txt" {
			t.Fatalf("fractional .500 row must follow its plain second: got %v", afterPlain)
		}
		afterFrac := q(NodeQuery{Where: &QueryAnd{Children: []NodePredicate{
			&QueryCompare{Field: QueryCreated, Op: QueryGt, Value: "2026-08-03T10:00:30.500Z"},
			&QueryFolderTest{},
		}}})
		if len(afterFrac) != 1 || afterFrac[0].Path != "日本語/資料-01.txt" {
			t.Fatalf("strict bound on the exact fraction: got %v", afterFrac)
		}
	})

	t.Run("property any-of vs msp same-instance", func(t *testing.T) {
		env := func(conds ...QueryPropCond) []QueryPropCond { return conds }
		anyOf := q(NodeQuery{Where: &QueryAnd{Children: []NodePredicate{
			&QueryPropExists{Conds: env(
				QueryPropCond{OnValue: false, Op: QueryEq, Value: "env"},
				QueryPropCond{OnValue: true, Op: QueryEq, Value: "prod"},
			)},
			&QueryPropExists{Conds: env(
				QueryPropCond{OnValue: false, Op: QueryEq, Value: "env"},
				QueryPropCond{OnValue: true, Op: QueryEq, Value: "dev"},
			)},
			&QueryFolderTest{},
		}}})
		if len(anyOf) != 1 || anyOf[0].Path != "日本語/資料-01.txt" {
			t.Fatalf("any-of over the multi-valued env: got %v", anyOf)
		}
		same := q(NodeQuery{Where: &QueryAnd{Children: []NodePredicate{
			&QueryPropSame{Conds: env(
				QueryPropCond{OnValue: false, Op: QueryEq, Value: "env"},
				QueryPropCond{OnValue: true, Op: QueryEq, Value: "prod"},
				QueryPropCond{OnValue: false, Op: QueryEq, Value: "env"},
				QueryPropCond{OnValue: true, Op: QueryEq, Value: "dev"},
			)},
			&QueryFolderTest{},
		}}})
		if len(same) != 0 {
			t.Fatalf("$msp must require ONE property instance (prod and dev are different rows): got %v", same)
		}
	})

	t.Run("sort window and tiebreaker", func(t *testing.T) {
		all := q(NodeQuery{
			Where: &QueryAnd{Children: []NodePredicate{&QueryFolderTest{}}},
			Sort:  []NodeSort{{Field: QueryCreated, Asc: true}},
		})
		var paths []string
		for _, r := range all {
			paths = append(paths, r.Path)
		}
		if len(all) != 5 {
			t.Fatalf("file rows = %d, want 5 (%v)", len(all), paths)
		}
		for i := 1; i < len(all); i++ {
			if all[i].CreatedAt < all[i-1].CreatedAt {
				t.Fatalf("not sorted by created: %v", paths)
			}
		}
		page2 := q(NodeQuery{
			Where:  &QueryAnd{Children: []NodePredicate{&QueryFolderTest{}}},
			Sort:   []NodeSort{{Field: QueryCreated, Asc: true}},
			Offset: 2, HasOffset: true,
			Limit: 2, HasLimit: true,
		})
		if len(page2) != 2 || page2[0].Path != all[2].Path || page2[1].Path != all[3].Path {
			t.Fatalf("offset/limit page mismatch: %v vs %v", page2, all[2:4])
		}
	})

	t.Run("property projection fills ordered values", func(t *testing.T) {
		got := q(NodeQuery{
			Where: &QueryCompare{Field: QueryPath, Op: QueryEq, Value: "日本語"},
			Props: []string{"env"},
		})
		if len(got) != 1 {
			t.Fatalf("parent-dir criteria: %v", got)
		}
		if fmt.Sprint(got[0].Props["env"]) != "[dev prod]" {
			t.Fatalf("props = %v, want [dev prod] (PK value-arm lexical order)", got[0].Props)
		}
		all := q(NodeQuery{
			Where: &QueryCompare{Field: QueryPath, Op: QueryEq, Value: "org"},
			Props: []string{"*"},
		})
		// Both the file and the folder marker live under parent "org"; the
		// marker carries nothing, the file carries its one key.
		if len(all) != 2 {
			t.Fatalf("catch-all rows = %d, want 2", len(all))
		}
		var fileProps map[string][]string
		for _, r := range all {
			if r.Type == "file" {
				fileProps = r.Props
			}
		}
		if fmt.Sprint(fileProps) != "map[license:[Apache-2.0]]" {
			t.Fatalf("catch-all props = %v, want map[license:[Apache-2.0]]", fileProps)
		}
		// Rows without the requested key still carry the (empty) map.
		none := q(NodeQuery{
			Where: &QueryCompare{Field: QueryPath, Op: QueryEq, Value: "org/sub"},
			Props: []string{"license"},
		})
		if len(none) != 2 {
			t.Fatalf("rows under parent org/sub = %d, want 2", len(none))
		}
		for _, r := range none {
			if r.Props == nil || len(r.Props) != 0 {
				t.Fatalf("props for keyless node %s = %v, want empty non-nil map", r.Path, r.Props)
			}
		}
	})

	t.Run("lazy ledger join fills checksums", func(t *testing.T) {
		got := q(NodeQuery{Where: &QueryCompare{Field: QuerySha1, Op: QueryEq, Value: "sha1-bbb"}})
		if len(got) != 2 {
			t.Fatalf("sha1 rows = %d, want 2 (%v)", len(got), got)
		}
		for _, r := range got {
			if r.Sha1 != "sha1-bbb" || r.Md5 != "md5-bbb" {
				t.Fatalf("checksums not filled: %+v", r)
			}
		}
		// Without a ledger reference the columns stay empty (both the
		// file and the folder marker under parent "org").
		plain := q(NodeQuery{Where: &QueryCompare{Field: QueryPath, Op: QueryEq, Value: "org"}})
		if len(plain) != 2 {
			t.Fatalf("rows under parent org = %d, want 2", len(plain))
		}
		for _, r := range plain {
			if r.Sha1 != "" || r.Md5 != "" {
				t.Fatalf("ledger must stay lazy: %+v", r)
			}
		}
	})

	t.Run("nil where matches everything", func(t *testing.T) {
		got := q(NodeQuery{})
		if len(got) != 7 {
			t.Fatalf("match-all rows = %d, want 7", len(got))
		}
	})
}
