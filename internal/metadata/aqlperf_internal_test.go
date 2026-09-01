package metadata

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"
	"time"
)

// T-411 AC3: the ten-thousand-node performance legs (NFR-P67 pre-leg,
// local SSD + WAL — the store's DSN default). The corpus extends the M10
// property fixture along the three axes the ticket names: multiple
// repositories, multiple depths, multiple properties.
//
//   item-domain budget:      P95 <= 500ms
//   property-join budget:    P95 <= 800ms
//
// The IR below is hand-built in the exact shapes PlanQuery emits for the
// corresponding AQL (the equivalence is pinned by the snapshot tables in
// internal/search/plan_test.go and the chain tests); what this leg
// measures is the compiled SQL execution, which is where the budget lives.
//
// Re-run: scripts/m15-aql-perf.sh (or go test ./internal/metadata
// -run TestT411Perf -count=1 -v). -short skips the corpus and the legs.

const (
	perfRepos     = 6    // repositories
	perfFiles     = 2000 // file nodes per repository -> 12,000 total
	perfBlobs     = 1500 // shared ledger rows
	perfRuns      = 30   // measured runs per shape
	perfBatchRows = 100  // rows per multi-row INSERT (parameter budget)
)

// perfBudgetItem / perfBudgetProp are the AC3 budgets in milliseconds.
const (
	perfBudgetItem = 500 * time.Millisecond
	perfBudgetProp = 800 * time.Millisecond
)

// seedAqlPerfCorpus lands the deterministic corpus with batched raw
// inserts (the store API's per-row transactions would measure argon-grade
// fsync patience, not query latency).
func seedAqlPerfCorpus(t *testing.T, st Store) {
	t.Helper()
	db := st.(*sqliteStore).db
	start := time.Now()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	const ts = "2026-01-01T00:00:00Z"
	for r := 0; r < perfRepos; r++ {
		exec(`INSERT INTO repositories (repo_key, type, package_type, description, config, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?)`, fmt.Sprintf("perf-r%d", r), "local", "generic", "", "{}", ts, ts)
	}
	// Blobs: 1500 shared contents, every thousandth carries a sha1 probe.
	for startIdx := 0; startIdx < perfBlobs; startIdx += perfBatchRows {
		end := min(startIdx+perfBatchRows, perfBlobs)
		var sb strings.Builder
		sb.WriteString(`INSERT INTO blobs (sha256, sha1, md5, size, created_at) VALUES `)
		var args []any
		for i := startIdx; i < end; i++ {
			if len(args) > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("(?,?,?,?,?)")
			args = append(args,
				fmt.Sprintf("%064d", i), fmt.Sprintf("perf-sha1-%06d", i),
				fmt.Sprintf("perf-md5-%06d", i), i*13, ts)
		}
		exec(sb.String(), args...)
	}
	marker := strings.Repeat("0", 64)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	users := []string{"alice", "bob", "carol"}

	// Folder marker rows: one chain l0/ .. l{d}/ per repo plus flat dirs.
	type folder struct{ repo, path string }
	var folders []folder
	for r := 0; r < perfRepos; r++ {
		chain := ""
		for d := 0; d < 5; d++ {
			chain += fmt.Sprintf("l%d/", d)
			folders = append(folders, folder{fmt.Sprintf("perf-r%d", r), chain})
		}
		for f := 0; f < 50; f++ {
			folders = append(folders, folder{fmt.Sprintf("perf-r%d", r), fmt.Sprintf("flat-%02d/", f)})
		}
	}
	for startIdx := 0; startIdx < len(folders); startIdx += perfBatchRows {
		end := min(startIdx+perfBatchRows, len(folders))
		var sb strings.Builder
		sb.WriteString(`INSERT INTO nodes (repo_key, path, sha256, size, mime, created_by, created_at, updated_at) VALUES `)
		var args []any
		for _, f := range folders[startIdx:end] {
			if len(args) > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("(?,?,?,?,?,?,?,?)")
			args = append(args, f.repo, f.path, marker, 0, "inode/directory", "admin", ts, ts)
		}
		exec(sb.String(), args...)
	}

	// File nodes and their properties, built in one deterministic pass.
	type prop struct{ repo, path, name, value string }
	insertNodes := func(rows []struct {
		repo, path, sha string
		size            int64
		by, at          string
	}) {
		for startIdx := 0; startIdx < len(rows); startIdx += perfBatchRows {
			end := min(startIdx+perfBatchRows, len(rows))
			var sb strings.Builder
			sb.WriteString(`INSERT INTO nodes (repo_key, path, sha256, size, mime, created_by, created_at, updated_at) VALUES `)
			var args []any
			for _, n := range rows[startIdx:end] {
				if len(args) > 0 {
					sb.WriteString(",")
				}
				sb.WriteString("(?,?,?,?,?,?,?,?)")
				args = append(args, n.repo, n.path, n.sha, n.size, "application/octet-stream", n.by, n.at, n.at)
			}
			exec(sb.String(), args...)
		}
	}
	insertProps := func(rows []prop) {
		for startIdx := 0; startIdx < len(rows); startIdx += perfBatchRows {
			end := min(startIdx+perfBatchRows, len(rows))
			var sb strings.Builder
			sb.WriteString(`INSERT INTO node_props (repo_key, path, name, value) VALUES `)
			var args []any
			for _, p := range rows[startIdx:end] {
				if len(args) > 0 {
					sb.WriteString(",")
				}
				sb.WriteString("(?,?,?,?)")
				args = append(args, p.repo, p.path, p.name, p.value)
			}
			exec(sb.String(), args...)
		}
	}
	var nodes []struct {
		repo, path, sha string
		size            int64
		by, at          string
	}
	var props []prop
	for r := 0; r < perfRepos; r++ {
		repo := fmt.Sprintf("perf-r%d", r)
		for i := 0; i < perfFiles; i++ {
			depth := i % 5
			parts := make([]string, depth)
			for d := 0; d < depth; d++ {
				parts[d] = fmt.Sprintf("l%d", d)
			}
			path := strings.Join(append(parts, fmt.Sprintf("file-r%d-%04d.bin", r, i)), "/")
			createdAt := base.Add(time.Duration(i) * time.Hour).UTC().Format(time.RFC3339)
			nodes = append(nodes, struct {
				repo, path, sha string
				size            int64
				by, at          string
			}{repo, path, fmt.Sprintf("%064d", i%perfBlobs), int64(1000 + i%997), users[i%3], createdAt})
			// Every node carries license; even i add env and build.
			license := "MIT"
			if i%2 == 0 {
				license = "Apache-2.0"
			}
			props = append(props, prop{repo, path, "license", license})
			if i%2 == 0 {
				env := []string{"prod", "dev", "stage"}[i%3]
				props = append(props, prop{repo, path, "env", env})
				props = append(props, prop{repo, path, "build", fmt.Sprintf("b%02d", i/100)})
			}
		}
	}
	insertNodes(nodes)
	insertProps(props)
	t.Logf("corpus: %d repos, %d files, %d folder rows, %d blob rows, %d property rows seeded in %s",
		perfRepos, len(nodes), len(folders), perfBlobs, len(props), time.Since(start).Round(time.Millisecond))
}

// p95Of runs fn `runs` times and returns the 95th percentile duration.
func p95Of(t *testing.T, runs int, fn func() (int, error)) (time.Duration, int) {
	t.Helper()
	durations := make([]time.Duration, 0, runs)
	rows := 0
	for i := 0; i < runs; i++ {
		start := time.Now()
		n, err := fn()
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		rows = n
		durations = append(durations, time.Since(start))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	idx := int(math.Ceil(0.95*float64(runs))) - 1
	return durations[idx], rows
}

// perfQuery executes one compiled shape through the public seam.
func perfQuery(ctx context.Context, q NodeQueryer, nq NodeQuery) (int, error) {
	rows, err := q.QueryNodes(ctx, nq)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func TestT411PerfItemDomain(t *testing.T) {
	if testing.Short() {
		t.Skip("perf leg skipped in -short")
	}
	if raceDetectorOn {
		t.Skip("perf leg skipped under -race: detector bookkeeping skews wall-clock budgets")
	}
	st := openTest(t)
	seedAqlPerfCorpus(t, st)
	q := st.Nodes().(NodeQueryer)
	ctx := context.Background()

	shapes := []struct {
		name   string
		query  NodeQuery
		minRow int
	}{
		{
			name: "repo equality plus date window, sorted",
			query: NodeQuery{
				Where: &QueryAnd{Children: []NodePredicate{
					&QueryCompare{Field: QueryRepo, Op: QueryEq, Value: "perf-r0"},
					&QueryCompare{Field: QueryCreated, Op: QueryGte, Value: "2026-02-01T00:00:00Z"},
					&QueryCompare{Field: QueryCreated, Op: QueryLt, Value: "2026-03-01T00:00:00Z"},
					&QueryFolderTest{},
				}},
				Sort:  []NodeSort{{Field: QueryCreated, Asc: false}},
				Limit: 100, HasLimit: true,
			},
			minRow: 1,
		},
		{
			name: "whole-corpus name scan at the K63 cap",
			query: NodeQuery{
				Where: &QueryAnd{Children: []NodePredicate{
					&QueryCompare{Field: QueryName, Op: QueryLike, Value: "%file-r_-09%"},
					&QueryFolderTest{},
				}},
				Limit: 1000, HasLimit: true,
			},
			// 100 matching names per repository (i in 0900..0999).
			minRow: 600,
		},
		{
			name: "match-all with derived-column sort",
			query: NodeQuery{
				Where: &QueryAnd{Children: []NodePredicate{&QueryFolderTest{}}},
				Sort:  []NodeSort{{Field: QueryName, Asc: true}},
				Limit: 1000, HasLimit: true,
			},
			minRow: 1000,
		},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			p95, rows := p95Of(t, perfRuns, func() (int, error) {
				return perfQuery(ctx, q, shape.query)
			})
			t.Logf("P95 = %v over %d runs (%d rows)", p95, perfRuns, rows)
			if rows < shape.minRow {
				t.Fatalf("rows = %d, want >= %d (shape lost selectivity)", rows, shape.minRow)
			}
			if p95 > perfBudgetItem {
				t.Fatalf("item-domain P95 %v exceeds the %v budget", p95, perfBudgetItem)
			}
		})
	}
}

func TestT411PerfPropertyJoin(t *testing.T) {
	if testing.Short() {
		t.Skip("perf leg skipped in -short")
	}
	if raceDetectorOn {
		t.Skip("perf leg skipped under -race: detector bookkeeping skews wall-clock budgets")
	}
	st := openTest(t)
	seedAqlPerfCorpus(t, st)
	q := st.Nodes().(NodeQueryer)
	ctx := context.Background()

	shapes := []struct {
		name   string
		query  NodeQuery
		minRow int
	}{
		{
			name: "two hoisted property joins with projection",
			query: NodeQuery{
				Where: &QueryAnd{Children: []NodePredicate{
					&QueryPropExists{Conds: []QueryPropCond{
						{OnValue: false, Op: QueryEq, Value: "license"},
						{OnValue: true, Op: QueryEq, Value: "Apache-2.0"},
					}},
					&QueryPropExists{Conds: []QueryPropCond{
						{OnValue: false, Op: QueryEq, Value: "env"},
						{OnValue: true, Op: QueryEq, Value: "prod"},
					}},
					&QueryFolderTest{},
				}},
				Sort:  []NodeSort{{Field: QueryCreated, Asc: true}},
				Limit: 1000, HasLimit: true,
				Props: []string{"license", "env", "build"},
			},
			// license=Apache-2.0 AND env=prod <=> i%6==0: 2000 nodes,
			// so the capped window is full.
			minRow: 1000,
		},
		{
			name: "$msp same-instance with projection",
			query: NodeQuery{
				Where: &QueryAnd{Children: []NodePredicate{
					&QueryPropSame{Conds: []QueryPropCond{
						{OnValue: false, Op: QueryEq, Value: "build"},
						{OnValue: true, Op: QueryLike, Value: "b0_%"},
						{OnValue: true, Op: QueryLt, Value: "b03"},
					}},
					&QueryFolderTest{},
				}},
				Limit: 1000, HasLimit: true,
				Props: []string{"*"},
			},
			// One property row (build in b00..b02) satisfies every
			// condition: even i below 300, six repositories.
			minRow: 900,
		},
		{
			name: "value-only EXISTS under a disjunction (non-hoistable worst case)",
			query: NodeQuery{
				Where: &QueryAnd{Children: []NodePredicate{
					&QueryOr{Children: []NodePredicate{
						&QueryPropExists{Conds: []QueryPropCond{{OnValue: true, Op: QueryEq, Value: "stage"}}},
						&QueryCompare{Field: QueryRepo, Op: QueryEq, Value: "perf-r5"},
					}},
					&QueryFolderTest{},
				}},
				Sort:  []NodeSort{{Field: QueryCreated, Asc: true}},
				Limit: 1000, HasLimit: true,
				Props: []string{"env"},
			},
			minRow: 1000,
		},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			p95, rows := p95Of(t, perfRuns, func() (int, error) {
				return perfQuery(ctx, q, shape.query)
			})
			t.Logf("P95 = %v over %d runs (%d rows)", p95, perfRuns, rows)
			if rows < shape.minRow {
				t.Fatalf("rows = %d, want >= %d (shape lost selectivity)", rows, shape.minRow)
			}
			if p95 > perfBudgetProp {
				t.Fatalf("property-join P95 %v exceeds the %v budget", p95, perfBudgetProp)
			}
		})
	}
}
