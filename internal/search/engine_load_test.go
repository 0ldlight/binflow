package search

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// T-413 AC2's window arm and AC3's mixed-load leg, both over the real
// sqlite store (the func family cannot exercise SQL-side windows).

// bigEnv seeds one open repository with n file nodes at zero-padded paths
// (lexical order == numeric order under the (repo, path) tiebreaker).
func bigEnv(t *testing.T, n int, repoKey string) (*Engine, metadata.Store) {
	t.Helper()
	rs := newRealStack(t, false)
	sha := seedBlob(t, rs.md)
	seedRepo(t, rs.md, repoKey, repo.TypeLocal)
	for i := 0; i < n; i++ {
		seedFile(t, rs.md, sha, repoKey, fmt.Sprintf("d/%04d.bin", i))
	}
	eng := NewEngine(EngineOptions{Nodes: mustQueryer(t, rs.md), ACL: &funcACL{
		scope: []repo.ReadScope{{Repo: repoKey}},
	}})
	return eng, rs.md
}

// TestEngineWindowAndPaging pins the K63 window semantics: the cap+1 probe,
// the truncation marker as the honest upper bound on the RAW row sequence,
// and offset/limit paging reaching the whole set (AC2: "offset/limit 分页
// 可达全量").
func TestEngineWindowAndPaging(t *testing.T) {
	const total = 2505
	eng, _ := bigEnv(t, total, "big")
	ctx := context.Background()
	caller := &repo.Principal{Name: "alice"}

	pages := []struct {
		query        string
		wantRows     int
		wantTrunc    bool
		wantFirst    string
		wantLast     string
		wantEchoLim  int64
		wantEchoHas  bool
		wantEchoOff  int64
		wantEchoHasO bool
		walk         bool // the offset-walk pages must be pairwise disjoint
	}{
		{`items.find({}).offset(0)`, 1000, true, "d/0000.bin", "d/0999.bin", 0, false, 0, true, true},
		{`items.find({}).offset(1000)`, 1000, true, "d/1000.bin", "d/1999.bin", 0, false, 1000, true, true},
		{`items.find({}).offset(2000)`, 505, false, "d/2000.bin", "d/2504.bin", 0, false, 2000, true, true},
		// Windows re-reading the head (an effective cap or a tighter limit
		// from offset 0) legitimately revisit rows — they pin the echo and
		// the effective-cap arms, not the walk.
		{`items.find({}).limit(5000)`, 1000, true, "d/0000.bin", "d/0999.bin", 5000, true, 0, false, false},
		{`items.find({}).limit(3)`, 3, true, "d/0000.bin", "d/0002.bin", 3, true, 0, false, false},
	}
	seen := map[string]bool{}
	for _, pg := range pages {
		t.Run(pg.query, func(t *testing.T) {
			res, err := eng.Run(ctx, caller, pg.query)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(res.Rows) != pg.wantRows {
				t.Fatalf("rows = %d, want %d", len(res.Rows), pg.wantRows)
			}
			if res.Truncated != pg.wantTrunc {
				t.Fatalf("truncated = %t, want %t", res.Truncated, pg.wantTrunc)
			}
			if hitsOf(res.Rows)[0] != "big/"+pg.wantFirst || hitsOf(res.Rows)[len(res.Rows)-1] != "big/"+pg.wantLast {
				t.Fatalf("page bounds = [%s, %s], want [%s, %s]",
					hitsOf(res.Rows)[0], hitsOf(res.Rows)[len(res.Rows)-1], pg.wantFirst, pg.wantLast)
			}
			if res.Limit != pg.wantEchoLim || res.HasLimit != pg.wantEchoHas ||
				res.Offset != pg.wantEchoOff || res.HasOffset != pg.wantEchoHasO {
				t.Fatalf("echo window = (%d,%t,%d,%t), want (%d,%t,%d,%t)",
					res.Limit, res.HasLimit, res.Offset, res.HasOffset,
					pg.wantEchoLim, pg.wantEchoHas, pg.wantEchoOff, pg.wantEchoHasO)
			}
			for _, h := range hitsOf(res.Rows) {
				if !pg.walk {
					continue
				}
				if seen[h] {
					t.Fatalf("page walk revisited %s — pages are not disjoint", h)
				}
				seen[h] = true
			}
		})
	}
	// The union of the offset-walked pages is the whole corpus: paging
	// reaches the full set behind the cap.
	if len(seen) != total {
		t.Fatalf("page walk covered %d distinct rows, want %d", len(seen), total)
	}

	// A window with nothing past it reports no truncation; past the end is
	// an honest empty page.
	res, err := eng.Run(ctx, caller, `items.find({"name":{"$eq":"0007.bin"}})`)
	if err != nil {
		t.Fatalf("exact-name run: %v", err)
	}
	if len(res.Rows) != 1 || res.Truncated {
		t.Fatalf("exact-name = %d rows, truncated %t — want 1 row, no truncation", len(res.Rows), res.Truncated)
	}
	res, err = eng.Run(ctx, caller, `items.find({}).offset(2505)`)
	if err != nil {
		t.Fatalf("past-end run: %v", err)
	}
	if len(res.Rows) != 0 || res.Truncated {
		t.Fatalf("past-end = %d rows, truncated %t — want an honest empty page", len(res.Rows), res.Truncated)
	}
}

// TestEngineMixedLoad is AC3 (NFR-P68's pre-leg): eight concurrent query
// shapes over the real store while a writer keeps landing nodes — the only
// permitted failures are the gate's own 429 rejections (never a 5xx), every
// result stays inside the cap, and the store keeps serving both planes
// until the writer's tail is queryable.
func TestEngineMixedLoad(t *testing.T) {
	rs := newRealStack(t, false)
	sha := seedBlob(t, rs.md)
	seedRepo(t, rs.md, "load-a", repo.TypeLocal)
	seedRepo(t, rs.md, "load-b", repo.TypeLocal)
	for key, n := range map[string]int{"load-a": 450, "load-b": 450} {
		seedFile(t, rs.md, sha, key, "d/")
		for i := 0; i < n; i++ {
			path := fmt.Sprintf("d/%03d.bin", i)
			seedFile(t, rs.md, sha, key, path)
			if i%10 == 0 {
				team := "platform"
				if i%20 == 0 {
					team = "infra"
				}
				seedProps(t, rs.md, key, path, map[string][]string{"team": {team}})
			}
		}
	}
	// A fixed clock puts the seeded timestamps inside the $last window.
	eng := NewEngine(EngineOptions{
		Nodes: mustQueryer(t, rs.md),
		ACL: &funcACL{scope: []repo.ReadScope{
			{Repo: "load-a"}, {Repo: "load-b"},
		}},
		Now: func() time.Time { return time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC) },
	})
	ctx := context.Background()
	caller := &repo.Principal{Name: "alice"}

	flavors := []string{
		`items.find({})`,
		`items.find({"name":{"$match":"*4?2*"}})`,
		`items.find({"@team":"platform"})`,
		`items.find({"$or":[{"repo":"load-a"},{"path":"d"}]})`,
		`items.find({"repo":"load-a"}).sort({"$asc":["name"]}).limit(50)`,
		`items.find({"created":{"$last":"60 minutes"}})`,
		`items.find({"type":"folder"})`,
		`items.find({"size":{"$gt":10}}).offset(20).limit(30)`,
	}

	const rounds = 15
	deadline := time.Now().Add(30 * time.Second)
	runFlavor := func(query string) {
		for i := 0; i < rounds; i++ {
			res, err := eng.Run(ctx, caller, query)
			// The gate's 429 is a legitimate busy verdict, never a failure
			// — the test client retries politely until its own deadline.
			for err != nil && errors.Is(err, ErrResourceBusy) && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
				res, err = eng.Run(ctx, caller, query)
			}
			if err != nil {
				t.Errorf("flavor %q: %v (only the busy gate may refuse)", query, err)
				return
			}
			if len(res.Rows) > ResultCap {
				t.Errorf("flavor %q returned %d rows past the cap", query, len(res.Rows))
				return
			}
		}
	}
	var wg sync.WaitGroup
	for _, flavor := range flavors {
		wg.Add(1)
		go func(query string) {
			defer wg.Done()
			runFlavor(query)
		}(flavor)
	}

	// The write plane runs beside the queries: 300 fresh nodes land while
	// the eight flavors hammer the read plane.
	const freshNodes = 300
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < freshNodes; i++ {
			seedFile(t, rs.md, sha, "load-a", fmt.Sprintf("w/%03d.bin", i))
		}
	}()
	wg.Wait()

	// The writer's tail is queryable: the corpus grew by exactly the fresh
	// nodes and the read plane sees them (AQL path is the PARENT directory,
	// aql.md §2.2 — the tail rows all live under "w").
	res, err := eng.Run(ctx, caller, `items.find({"path":{"$eq":"w"}})`)
	if err != nil {
		t.Fatalf("post-load run: %v", err)
	}
	if len(res.Rows) != freshNodes {
		t.Fatalf("fresh tail = %d rows, want %d", len(res.Rows), freshNodes)
	}
	if hits := hitsOf(res.Rows); !strings.HasPrefix(hits[0], "load-a/w/") {
		t.Fatalf("fresh tail order starts at %v", hits[0])
	}
}
