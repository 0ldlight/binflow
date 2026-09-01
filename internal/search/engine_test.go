package search

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// T-413: the engine entry — ACL weave, K63 gate, window/truncation,
// obfuscation. Two fixture families:
//
//   - the func family (funcACL/funcQueryer): pure-mechanics pins — the woven
//     IR, the cap+1 probe, the gate's non-blocking rejection, the timeout
//     mapping, the empty-scope short-circuit;
//   - the real family (newRealStack): storage + metadata + auth.Service +
//     repo.Service over temp dirs — the AC1 zero-leak leg through the very
//     seams production wires (the same allow() source T-92 runs).

// ---- fixtures: the func family ----

// funcACL scripts the two-stage weave.
type funcACL struct {
	scope    []repo.ReadScope
	scopeErr error
	canRead  func(repoKey, path string) bool
}

func (f *funcACL) SearchScope(context.Context, *repo.Principal) ([]repo.ReadScope, error) {
	return f.scope, f.scopeErr
}

func (f *funcACL) CanRead(_ context.Context, _ *repo.Principal, repoKey, path string) bool {
	if f.canRead == nil {
		return true
	}
	return f.canRead(repoKey, path)
}

// funcQueryer records and routes query execution.
type funcQueryer struct {
	mu    sync.Mutex
	calls int
	last  metadata.NodeQuery
	run   func(ctx context.Context, q metadata.NodeQuery) ([]*metadata.NodeQueryRow, error)
}

func (f *funcQueryer) QueryNodes(ctx context.Context, q metadata.NodeQuery) ([]*metadata.NodeQueryRow, error) {
	f.mu.Lock()
	f.calls++
	f.last = q
	run := f.run
	f.mu.Unlock()
	if run == nil {
		return nil, nil
	}
	return run(ctx, q)
}

func (f *funcQueryer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *funcQueryer) snapshot() metadata.NodeQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

// rowsOf builds result rows for the canned arms.
func rowsOf(ids ...string) []*metadata.NodeQueryRow {
	out := make([]*metadata.NodeQueryRow, 0, len(ids))
	for _, id := range ids {
		parts := strings.SplitN(id, "/", 2)
		out = append(out, &metadata.NodeQueryRow{
			RepoKey: parts[0], Path: parts[1], Name: parts[1], Type: "file", CreatedBy: "admin",
		})
	}
	return out
}

// ---- fixtures: the real family ----

// realStack is the production-shaped assembly: the real storage engine,
// sqlite metadata store, auth.Service authorizer and repo.Service, over
// temp dirs (the fakes_test.go env shape, rebuilt here because the search
// package must wire its own engine against the public constructors).
type realStack struct {
	md  metadata.Store
	svc repo.Service
	eng *Engine
}

const seedTS = "2026-09-01T10:00:00Z"

func newRealStack(t *testing.T, anonymousRead bool) *realStack {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := storage.OpenEngine(dir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: filepath.Join(dir, "binflow.db")})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })
	az := auth.NewFromStore(md, anonymousRead)
	svc := repo.NewWithClock(st, md, az, nil, func() time.Time { return time.Now().UTC() })
	q, ok := md.Nodes().(metadata.NodeQueryer)
	if !ok {
		t.Fatalf("the sqlite store does not carry metadata.NodeQueryer")
	}
	return &realStack{md: md, svc: svc, eng: NewEngine(EngineOptions{Nodes: q, ACL: svc})}
}

// seedBlob lands the one shared content row every seeded node references.
func seedBlob(t *testing.T, md metadata.Store) string {
	t.Helper()
	sha := strings.Repeat("0", 64)
	if err := md.Blobs().Put(context.Background(), &metadata.Blob{
		Sha256: sha, Size: 1, CreatedAt: seedTS,
	}); err != nil {
		t.Fatalf("seed blob: %v", err)
	}
	return sha
}

// seedRepo lands a registry row of the given class.
func seedRepo(t *testing.T, md metadata.Store, key, typ string) {
	t.Helper()
	if err := md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: typ, PackageType: "generic", Config: "{}",
		CreatedAt: seedTS, UpdatedAt: seedTS,
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
}

// seedFile lands a file node (or a folder marker when the path ends in '/').
func seedFile(t *testing.T, md metadata.Store, sha, repoKey, path string) {
	t.Helper()
	if err := md.Nodes().Put(context.Background(), &metadata.Node{
		RepoKey: repoKey, Path: path, Sha256: sha, Size: 42,
		Mime: "application/octet-stream", CreatedBy: "admin",
		CreatedAt: seedTS, UpdatedAt: seedTS,
	}); err != nil {
		t.Fatalf("seed node %s/%s: %v", repoKey, path, err)
	}
}

// seedProps annotates one seeded node.
func seedProps(t *testing.T, md metadata.Store, repoKey, path string, props map[string][]string) {
	t.Helper()
	if err := md.NodeProps().Merge(context.Background(), repoKey, path, props); err != nil {
		t.Fatalf("seed props %s/%s: %v", repoKey, path, err)
	}
}

// seedTarget lands one permission target with its principal rows.
func seedTarget(t *testing.T, md metadata.Store, name, repos, includes string,
	rows ...*metadata.PermissionPrincipal) {
	t.Helper()
	for _, r := range rows {
		r.TargetName = name
	}
	if err := md.Permissions().PutTarget(context.Background(), &metadata.PermissionTarget{
		Name: name, Repos: repos, Includes: includes, Excludes: "[]",
	}, rows); err != nil {
		t.Fatalf("seed target %s: %v", name, err)
	}
}

// readRow is the universal read principal row.
func readRow(principal, typ string) *metadata.PermissionPrincipal {
	return &metadata.PermissionPrincipal{Principal: principal, PrincipalType: typ, CanRead: true}
}

// hitsOf renders rows as "<repo>/<path>".
func hitsOf(rows []*metadata.NodeQueryRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.RepoKey+"/"+r.Path)
	}
	return out
}

// ---- AC1: the two-stage weave over the real stack ----

// aclEnv is the dual-user zero-leak playground: alice reads alpha
// unrestricted and beta only under pub/**; bob reads gamma only; nobody but
// the role universes reads vault; the remote repository is cached but
// ungranted.
func aclEnv(t *testing.T) *realStack {
	t.Helper()
	rs := newRealStack(t, false)
	sha := seedBlob(t, rs.md)
	for key, paths := range map[string][]string{
		"alpha": {"pub/x.bin", "secret/y.bin"},
		"beta":  {"pub/ok.bin", "hidden/no.bin"},
		"gamma": {"z.bin"},
		"vault": {"top.bin"},
		"rem":   {"cached/remote.bin"},
	} {
		seedRepo(t, rs.md, key, repo.TypeLocal)
		for _, p := range paths {
			seedFile(t, rs.md, sha, key, p)
		}
	}
	seedTarget(t, rs.md, "t-alpha", `["alpha"]`, "[]", readRow("alice", "user"))
	seedTarget(t, rs.md, "t-beta", `["beta"]`, `["pub/**"]`, readRow("alice", "user"))
	seedTarget(t, rs.md, "t-gamma", `["gamma"]`, "[]", readRow("bob", "user"))
	return rs
}

// TestEngineACLWeaveZeroLeak is AC1's server-side leg: the readable repo set
// becomes the repo_key predicate (stage one) and path-scoped rows clear
// CanRead (stage two) — a repository outside the caller's scope can never
// surface, and the same-repo hidden arm drops at the row re-check.
func TestEngineACLWeaveZeroLeak(t *testing.T) {
	rs := aclEnv(t)
	ctx := context.Background()

	tests := []struct {
		name string
		p    *repo.Principal
		err  error
		want []string
	}{
		{"alice: open target whole-repo, includes target only the granted arm",
			&repo.Principal{Name: "alice"}, nil,
			[]string{"alpha/pub/x.bin", "alpha/secret/y.bin", "beta/pub/ok.bin"}},
		{"bob: only his own repository",
			&repo.Principal{Name: "bob"}, nil, []string{"gamma/z.bin"}},
		{"admin: every repository incl. vault and the remote cache",
			&repo.Principal{Name: "root", Admin: true}, nil,
			[]string{
				"alpha/pub/x.bin", "alpha/secret/y.bin", "beta/hidden/no.bin", "beta/pub/ok.bin",
				"gamma/z.bin", "rem/cached/remote.bin", "vault/top.bin",
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := rs.eng.Run(ctx, tt.p, `items.find({})`)
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("Run err = %v, want %v", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			got := hitsOf(res.Rows)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("rows = %v\nwant    %v", got, tt.want)
			}
			// The explicit zero-leak assertion: no row may name a repository
			// outside the caller's readable set, whatever the predicate did.
			for _, r := range res.Rows {
				switch tt.p.Name {
				case "alice":
					if r.RepoKey != "alpha" && r.RepoKey != "beta" {
						t.Errorf("leak: alice saw %s/%s", r.RepoKey, r.Path)
					}
				case "bob":
					if r.RepoKey != "gamma" {
						t.Errorf("leak: bob saw %s/%s", r.RepoKey, r.Path)
					}
				}
			}
		})
	}

	// The anonymous closed-instance gate rides the same seam as T-92.
	if _, err := rs.eng.Run(ctx, nil, `items.find({})`); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("anonymous Run err = %v, want repo.ErrForbidden", err)
	}

	// Obfuscation (aql.md §6 / ADR-0043 Errata ⑧): non-admin rows carry the
	// literal, admin rows the original.
	ali, err := rs.eng.Run(ctx, &repo.Principal{Name: "alice"}, `items.find({"repo":"alpha"})`)
	if err != nil {
		t.Fatalf("alice alpha run: %v", err)
	}
	for _, r := range ali.Rows {
		if r.CreatedBy != "unknown" {
			t.Fatalf("alice created_by = %q, want the obfuscated literal", r.CreatedBy)
		}
	}
	adm, err := rs.eng.Run(ctx, &repo.Principal{Name: "root", Admin: true}, `items.find({"repo":"alpha"})`)
	if err != nil {
		t.Fatalf("admin alpha run: %v", err)
	}
	for _, r := range adm.Rows {
		if r.CreatedBy != "admin" {
			t.Fatalf("admin created_by = %q, want the original value", r.CreatedBy)
		}
	}
}

// ---- the mechanics: weave, window, short-circuit, gate, timeout ----

// TestEngineWeavesScopeAndProbe pins the IR contract of the weave: the
// scope lands as a top-level AND conjunct (a repo_key disjunction) so the
// compiler's property-join hoisting keeps working, and the window carries
// the cap+1 probe with the engine's effective limit.
func TestEngineWeavesScopeAndProbe(t *testing.T) {
	q := &funcQueryer{run: func(context.Context, metadata.NodeQuery) ([]*metadata.NodeQueryRow, error) {
		return nil, nil
	}}
	eng := NewEngine(EngineOptions{
		Nodes: q,
		ACL: &funcACL{scope: []repo.ReadScope{
			{Repo: "r1"}, {Repo: "r2", PathScoped: true},
		}},
	})
	res, err := eng.Run(context.Background(), &repo.Principal{Name: "alice"}, `items.find({"@team":"x"})`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := q.snapshot()
	and, ok := got.Where.(*metadata.QueryAnd)
	if !ok {
		t.Fatalf("woven Where = %T, want *metadata.QueryAnd", got.Where)
	}
	var group *metadata.QueryOr
	for _, ch := range and.Children {
		if or, isOr := ch.(*metadata.QueryOr); isOr {
			group = or
		}
	}
	if group == nil {
		t.Fatalf("woven Where has no scope disjunction: %#v", and.Children)
	}
	keys := make([]string, 0, len(group.Children))
	for _, ch := range group.Children {
		cmp, ok := ch.(*metadata.QueryCompare)
		if !ok || cmp.Field != metadata.QueryRepo || cmp.Op != metadata.QueryEq {
			t.Fatalf("scope arm = %#v, want repo eq comparators", ch)
		}
		keys = append(keys, cmp.Value.(string))
	}
	if strings.Join(keys, ",") != "r1,r2" {
		t.Fatalf("scope repos = %v, want [r1 r2]", keys)
	}
	if !got.HasLimit || got.Limit != ResultCap+1 {
		t.Fatalf("woven window = (%d, %t), want the cap+1 probe (%d, true)", got.Limit, got.HasLimit, ResultCap+1)
	}
	// The echoed window stays the query's own (absent here).
	if res.HasLimit || res.HasOffset {
		t.Fatalf("echo window = (%d,%t,%d,%t), want absent", res.Limit, res.HasLimit, res.Offset, res.HasOffset)
	}
}

// TestEnginePathScopedRecheck pins stage two on its own: rows of a
// PathScoped repository drop when CanRead refuses; rows of open
// repositories never consult it.
func TestEnginePathScopedRecheck(t *testing.T) {
	var checked []string
	var mu sync.Mutex
	q := &funcQueryer{run: func(context.Context, metadata.NodeQuery) ([]*metadata.NodeQueryRow, error) {
		return rowsOf("open/ok.bin", "scoped/pub/ok.bin", "scoped/hidden/denied.bin"), nil
	}}
	eng := NewEngine(EngineOptions{Nodes: q, ACL: &funcACL{
		scope: []repo.ReadScope{{Repo: "open"}, {Repo: "scoped", PathScoped: true}},
		canRead: func(repoKey, path string) bool {
			mu.Lock()
			checked = append(checked, repoKey+"/"+path)
			mu.Unlock()
			return !strings.Contains(path, "hidden")
		},
	}})
	res, err := eng.Run(context.Background(), &repo.Principal{Name: "alice"}, `items.find({})`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := hitsOf(res.Rows)
	if strings.Join(got, ",") != "open/ok.bin,scoped/pub/ok.bin" {
		t.Fatalf("rows = %v, want the open row and the granted scoped row only", got)
	}
	// Only the scoped repository's rows were checked.
	if strings.Join(checked, ",") != "scoped/pub/ok.bin,scoped/hidden/denied.bin" {
		t.Fatalf("CanRead consulted %v, want only the scoped rows", checked)
	}
}

// TestEngineEmptyScopeShortCircuit pins the empty-scope arm: no SQL at all,
// the plan still echoed, no error (ADR-0043 pt 4).
func TestEngineEmptyScopeShortCircuit(t *testing.T) {
	for _, tc := range []struct {
		name string
		acl  ACL
	}{
		{"zero-grant principal", &funcACL{scope: nil}},
		{"no ACL wired (fails closed)", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &funcQueryer{}
			eng := NewEngine(EngineOptions{Nodes: q, ACL: tc.acl})
			res, err := eng.Run(context.Background(), &repo.Principal{Name: "bob"}, `items.find({})`)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if q.count() != 0 {
				t.Fatalf("executed %d queries on an empty scope, want 0", q.count())
			}
			if len(res.Rows) != 0 || res.Plan == nil {
				t.Fatalf("result = %d rows, plan %v — want 0 rows with the plan echoed", len(res.Rows), res.Plan)
			}
		})
	}
}

// TestEngineGateBusy is AC2's concurrency arm: the fifth concurrent query
// meets ErrResourceBusy (the 429 family) without queueing; the gate frees
// as the blocked queries drain.
func TestEngineGateBusy(t *testing.T) {
	inside := make(chan struct{}, maxConcurrent+1)
	release := make(chan struct{})
	q := &funcQueryer{run: func(context.Context, metadata.NodeQuery) ([]*metadata.NodeQueryRow, error) {
		inside <- struct{}{}
		<-release
		return rowsOf("r/a.bin"), nil
	}}
	eng := NewEngine(EngineOptions{Nodes: q, ACL: &funcACL{scope: []repo.ReadScope{{Repo: "r"}}}})

	var wg sync.WaitGroup
	for i := 0; i < maxConcurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := eng.Run(context.Background(), &repo.Principal{Name: "alice"}, `items.find({})`); err != nil {
				t.Errorf("blocked holder failed: %v", err)
			}
		}()
	}
	// Wait until all four holders sit inside the queryer.
	for i := 0; i < maxConcurrent; i++ {
		<-inside
	}

	if _, err := eng.Run(context.Background(), &repo.Principal{Name: "alice"}, `items.find({})`); !errors.Is(err, ErrResourceBusy) {
		t.Fatalf("fifth concurrent Run err = %v, want ErrResourceBusy", err)
	}
	close(release)
	wg.Wait()

	// The gate is drained: a fresh query runs.
	if _, err := eng.Run(context.Background(), &repo.Principal{Name: "alice"}, `items.find({})`); err != nil {
		t.Fatalf("Run after drain: %v", err)
	}
}

// TestEngineTimeoutWhileExecuting pins the deadline mapping with an
// arbitrarily slow query: the blocker honors the context, and the surfaced
// error is the 408-family sentinel — not the bare context error.
func TestEngineTimeoutWhileExecuting(t *testing.T) {
	q := &funcQueryer{run: func(ctx context.Context, _ metadata.NodeQuery) ([]*metadata.NodeQueryRow, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	eng := NewEngine(EngineOptions{Nodes: q, ACL: &funcACL{scope: []repo.ReadScope{{Repo: "r"}}}})
	eng.timeout = 30 * time.Millisecond

	_, err := eng.Run(context.Background(), &repo.Principal{Name: "alice"}, `items.find({})`)
	if err == nil {
		t.Fatal("slow query returned nil error, want ErrQueryTimeout")
	}
	if !errors.Is(err, ErrQueryTimeout) {
		t.Fatalf("slow query err = %v, want ErrQueryTimeout", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout leaked the bare context error: %v", err)
	}
}

// TestEngineSlowQueryFullScanTimeout is AC2's injected slow-query leg over
// the real store: a leading-wildcard name pattern (LIKE '%…%', the
// full-table scan shape) against the deadline answers the timeout family.
func TestEngineSlowQueryFullScanTimeout(t *testing.T) {
	rs := newRealStack(t, false)
	sha := seedBlob(t, rs.md)
	seedRepo(t, rs.md, "big", repo.TypeLocal)
	for i := 0; i < 400; i++ {
		seedFile(t, rs.md, sha, "big", fmt.Sprintf("d/%03d/pkg-name-%03d.bin", i, i))
	}
	eng := NewEngine(EngineOptions{Nodes: mustQueryer(t, rs.md), ACL: &funcACL{
		scope: []repo.ReadScope{{Repo: "big"}},
	}})
	eng.timeout = time.Nanosecond

	res, err := eng.Run(context.Background(), &repo.Principal{Name: "alice"},
		`items.find({"name":{"$match":"*pkg-name-399*"}})`)
	if !errors.Is(err, ErrQueryTimeout) {
		t.Fatalf("full-scan run = (%v rows, %v), want ErrQueryTimeout", len(res.Rows), err)
	}

	// The same query inside the budget returns the needle: the timeout is
	// the gate's verdict, not a broken plan.
	eng.timeout = 10 * time.Second
	res, err = eng.Run(context.Background(), &repo.Principal{Name: "alice"},
		`items.find({"name":{"$match":"*pkg-name-399*"}})`)
	if err != nil {
		t.Fatalf("full-scan run inside budget: %v", err)
	}
	if len(res.Rows) != 1 || res.Rows[0].Path != "d/399/pkg-name-399.bin" {
		t.Fatalf("full-scan rows = %v, want the single needle", hitsOf(res.Rows))
	}
}

// mustQueryer resolves the NodeQueryer seam off a store.
func mustQueryer(t *testing.T, md metadata.Store) metadata.NodeQueryer {
	t.Helper()
	q, ok := md.Nodes().(metadata.NodeQueryer)
	if !ok {
		t.Fatalf("the sqlite store does not carry metadata.NodeQueryer")
	}
	return q
}
