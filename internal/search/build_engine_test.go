package search

// T-511 AC3 legs over the engine: the build-family entries ride the same
// K63 plane (gate/deadline/cap), the ACL weave is the r(buildRepo,
// buildName) row filter with zero unauthorized-row appearance, identities
// obfuscate for non-admin callers, and the window/truncation contract
// matches the items family.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/repo"
)

// fakeBuildSearcher scripts the record plane: two build names (pub-app the
// readable one, secret-core the forbidden one), one run each, two modules
// per run with two dependencies apiece.
type fakeBuildSearcher struct {
	runs      []*BuildRun
	modules   []*BuildModuleRow
	canRead   func(buildRepo, buildName string) bool
	readCalls atomic.Int64
}

func (f *fakeBuildSearcher) Runs(context.Context) ([]*BuildRun, error) {
	return f.runs, nil
}

func (f *fakeBuildSearcher) Modules(context.Context) ([]*BuildModuleRow, error) {
	return f.modules, nil
}

func (f *fakeBuildSearcher) CanReadBuild(_ context.Context, p *repo.Principal, buildRepo, buildName string) bool {
	f.readCalls.Add(1)
	// The real mirror (build.Service.allow) short-circuits admins; the
	// fake keeps the same evaluation chain.
	if p != nil && p.Admin {
		return true
	}
	if f.canRead == nil {
		return true
	}
	return f.canRead(buildRepo, buildName)
}

// newBuildFixture seeds the two-name world: pub-app#51 (2026-09-05) and
// secret-core#9 (2026-09-06), one module pair each, every module carrying
// two dependencies. canRead defaults to "pub-app only".
func newBuildFixture(t *testing.T) *fakeBuildSearcher {
	t.Helper()
	mk := func(name, number, started string) *BuildRun {
		return &BuildRun{
			Name: name, Number: number, Started: started,
			Repo: "artifactory-build-info", URL: "https://ci/job/" + name + "/" + number,
			CreatedAt: started, CreatedBy: "jenkins", UpdatedAt: started, UpdatedBy: "jenkins",
		}
	}
	pub := mk("pub-app", "51", "2026-09-05T10:00:00.000+0000")
	sec := mk("secret-core", "9", "2026-09-06T10:00:00.000+0000")
	mods := []*BuildModuleRow{
		{Repo: pub.Repo, Name: pub.Name, Number: pub.Number, Started: pub.Started, ModuleID: "com.example:api:1.0",
			Dependencies: []*BuildDependencyRow{
				{ID: "junit:junit:4.13", Type: "jar", Scopes: "test", Sha1: "dd11", Sha256: "ee22"},
				{ID: "org:lib:2.0", Type: "jar", Scopes: "compile", Sha1: "ff33", Sha256: "aa44"},
			}},
		{Repo: pub.Repo, Name: pub.Name, Number: pub.Number, Started: pub.Started, ModuleID: "com.example:web:1.0",
			Dependencies: []*BuildDependencyRow{
				{ID: "org:lib:2.0", Type: "jar", Scopes: "compile", Sha1: "ff33", Sha256: "aa44"},
			}},
		{Repo: sec.Repo, Name: sec.Name, Number: sec.Number, Started: sec.Started, ModuleID: "com.example:core:3.0",
			Dependencies: []*BuildDependencyRow{
				{ID: "org:secret:1.0", Type: "jar", Scopes: "compile", Sha1: "abab", Sha256: "cdcd"},
			}},
	}
	return &fakeBuildSearcher{
		runs:    []*BuildRun{pub, sec},
		modules: mods,
		canRead: func(_, buildName string) bool { return buildName == "pub-app" },
	}
}

// newBuildEngine assembles the engine over the fixture with a nil node
// queryer — the build family never touches it.
func newBuildEngine(t *testing.T, s *fakeBuildSearcher) *Engine {
	t.Helper()
	return NewEngine(EngineOptions{
		Builds: s,
		Now:    func() time.Time { return time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC) },
	})
}

// TestBuildEngineRowsAndWindows drives the three entrances through Run:
// row shapes per entry, sort, the offset/limit window and the
// truncation/cap arm.
func TestBuildEngineRowsAndWindows(t *testing.T) {
	s := newBuildFixture(t)
	e := newBuildEngine(t, s)
	admin := &repo.Principal{Name: "admin", Admin: true}
	ctx := context.Background()

	res, err := e.Run(ctx, admin, `builds.find({})`)
	if err != nil {
		t.Fatalf("builds.find: %v", err)
	}
	if res.EntryDomain != "builds" || len(res.BuildRows) != 2 || len(res.Rows) != 0 {
		t.Fatalf("builds result: entry=%s rows=%d itemRows=%d", res.EntryDomain, len(res.BuildRows), len(res.Rows))
	}
	if got := res.BuildRows[0].Name; got != "pub-app" {
		t.Fatalf("natural order broken: first row = %s", got)
	}

	// Sort desc by started flips the order.
	res, err = e.Run(ctx, admin, `builds.find({}).sort({"$desc":["started"]})`)
	if err != nil {
		t.Fatalf("builds.find sorted: %v", err)
	}
	if res.BuildRows[0].Name != "secret-core" {
		t.Fatalf("desc sort: first = %s, want secret-core", res.BuildRows[0].Name)
	}

	// The window: offset+limit over the sorted rows.
	res, err = e.Run(ctx, admin, `builds.find({}).sort({"$desc":["started"]}).offset(1).limit(1)`)
	if err != nil {
		t.Fatalf("windowed: %v", err)
	}
	if len(res.BuildRows) != 1 || res.BuildRows[0].Name != "pub-app" {
		t.Fatalf("window rows = %v", res.BuildRows)
	}
	if res.Offset != 1 || !res.HasOffset || res.Limit != 1 || !res.HasLimit {
		t.Fatalf("window echoes: %+v", res)
	}

	// modules: two readable (admin sees all three).
	res, err = e.Run(ctx, admin, `modules.find({})`)
	if err != nil {
		t.Fatalf("modules.find: %v", err)
	}
	if len(res.BuildRows) != 3 {
		t.Fatalf("modules rows = %d, want 3", len(res.BuildRows))
	}

	// dependencies: four lines across the three modules.
	res, err = e.Run(ctx, admin, `dependencies.find({})`)
	if err != nil {
		t.Fatalf("dependencies.find: %v", err)
	}
	if len(res.BuildRows) != 4 {
		t.Fatalf("dependencies rows = %d, want 4", len(res.BuildRows))
	}

	// Criteria: only the junit dependency.
	res, err = e.Run(ctx, admin, `dependencies.find({"name":{"$match":"junit:*"}})`)
	if err != nil {
		t.Fatalf("dependencies criteria: %v", err)
	}
	if len(res.BuildRows) != 1 || res.BuildRows[0].DepID != "junit:junit:4.13" {
		t.Fatalf("criteria rows = %v", res.BuildRows)
	}
}

// TestBuildEngineCapAndTruncation pins the K63 row ceiling on the build
// family: rows past the cap are trimmed with Truncated set (the cap+1
// probe's materialized-plane equivalent).
func TestBuildEngineCapAndTruncation(t *testing.T) {
	const extra = ResultCap + 5
	s := newBuildFixture(t)
	var runs []*BuildRun
	for i := 0; i < extra; i++ {
		runs = append(runs, &BuildRun{
			Name: "pub-app", Number: strings.Repeat("1", 1) + time.Duration(i).String(),
			Started: "2026-09-05T10:00:00.000+0000", Repo: "artifactory-build-info",
		})
	}
	s.runs = runs
	s.canRead = func(string, string) bool { return true }
	e := newBuildEngine(t, s)
	admin := &repo.Principal{Name: "admin", Admin: true}

	res, err := e.Run(context.Background(), admin, `builds.find({"name":"pub-app"})`)
	if err != nil {
		t.Fatalf("builds.find: %v", err)
	}
	if len(res.BuildRows) != ResultCap || !res.Truncated {
		t.Fatalf("rows = %d truncated = %v, want %d/true", len(res.BuildRows), res.Truncated, ResultCap)
	}
	// The query's own tighter limit windows the same raw set — and the
	// flag still fires (the items family's rule: rows past the effective
	// window set Truncated, the query's own limit included).
	res, err = e.Run(context.Background(), admin, `builds.find({"name":"pub-app"}).limit(10)`)
	if err != nil {
		t.Fatalf("builds.find limited: %v", err)
	}
	if len(res.BuildRows) != 10 || !res.Truncated {
		t.Fatalf("limited rows = %d truncated = %v, want 10/true", len(res.BuildRows), res.Truncated)
	}
}

// TestBuildEngineACLZeroUnauthorizedAppearance is the AC3 probe: a
// non-admin caller holding pub-app only — every secret-core row is absent
// from builds, modules AND dependencies answers, and the memoization means
// one verdict per (repo, name), not one per row.
func TestBuildEngineACLZeroUnauthorizedAppearance(t *testing.T) {
	s := newBuildFixture(t)
	e := newBuildEngine(t, s)
	bob := &repo.Principal{Name: "bob"}
	ctx := context.Background()

	for _, q := range []string{
		`builds.find({})`,
		`builds.find({"name":"*"})`,
		`modules.find({})`,
		`dependencies.find({})`,
		`dependencies.find({"name":"org:secret:1.0"})`, // names the forbidden row directly
	} {
		res, err := e.Run(ctx, bob, q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		for _, r := range res.BuildRows {
			if r.Name == "secret-core" {
				t.Fatalf("%s: unauthorized run row appeared (%s#%s)", q, r.Name, r.Number)
			}
		}
	}
	// The identity triple the §6 anchor names (name/number/repo) renders
	// on the readable rows and carries no forbidden leakage.
	res, err := e.Run(ctx, bob, `builds.find({}).include("name","number","repo")`)
	if err != nil {
		t.Fatalf("include triple: %v", err)
	}
	if len(res.BuildRows) != 1 || res.BuildRows[0].Name != "pub-app" || res.BuildRows[0].Number != "51" ||
		res.BuildRows[0].Repo != "artifactory-build-info" {
		t.Fatalf("visible rows = %v", res.BuildRows)
	}
	// Memoization: 3 module rows of 2 names -> 2 CanReadBuild evaluations.
	s.readCalls.Store(0)
	if _, err := e.Run(ctx, bob, `dependencies.find({})`); err != nil {
		t.Fatalf("memo probe: %v", err)
	}
	if got := s.readCalls.Load(); got != 2 {
		t.Fatalf("CanReadBuild calls = %d, want 2 (memoized per run)", got)
	}
}

// TestBuildEngineObfuscation: created_by/modified_by mask to the literal
// unknown for non-admin callers; the admin keeps the real identities.
func TestBuildEngineObfuscation(t *testing.T) {
	s := newBuildFixture(t)
	e := newBuildEngine(t, s)
	ctx := context.Background()

	res, err := e.Run(ctx, &repo.Principal{Name: "bob"}, `builds.find({"name":"pub-app"})`)
	if err != nil {
		t.Fatalf("non-admin: %v", err)
	}
	for _, r := range res.BuildRows {
		if r.CreatedBy != obfuscatedUser || r.UpdatedBy != obfuscatedUser {
			t.Fatalf("identity leaked: created_by=%q modified_by=%q", r.CreatedBy, r.UpdatedBy)
		}
	}
	res, err = e.Run(ctx, &repo.Principal{Name: "admin", Admin: true}, `builds.find({"name":"pub-app"})`)
	if err != nil {
		t.Fatalf("admin: %v", err)
	}
	for _, r := range res.BuildRows {
		if r.CreatedBy != "jenkins" || r.UpdatedBy != "jenkins" {
			t.Fatalf("admin identities masked: %+v", r)
		}
	}
}

// TestBuildEngineSharesK63Gate: a build-family query occupies the same
// admission slots as an items query — maxConcurrent in flight, the next
// answers ErrResourceBusy.
func TestBuildEngineSharesK63Gate(t *testing.T) {
	s := newBuildFixture(t)
	// A slow CanReadBuild holds every admitted query inside the segment.
	block := make(chan struct{})
	s.canRead = func(string, string) bool {
		<-block
		return true
	}
	e := newBuildEngine(t, s)
	// A non-admin caller: the fake's admin short-circuit would bypass the
	// blocking arm.
	bob := &repo.Principal{Name: "bob"}

	var wg sync.WaitGroup
	var busy atomic.Int64
	for i := 0; i < maxConcurrent+1; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.Run(context.Background(), bob, `builds.find({})`)
			if errors.Is(err, ErrResourceBusy) {
				busy.Add(1)
			}
		}()
	}
	// Wait until the gate settles: exactly one rejection among
	// maxConcurrent+1 concurrent queries.
	deadline := time.Now().Add(5 * time.Second)
	for busy.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	close(block)
	wg.Wait()
	if busy.Load() != 1 {
		t.Fatalf("busy rejections = %d, want exactly 1 (shared gate)", busy.Load())
	}
}

// TestBuildEngineNoSearcher: the nil-seam posture is the honest
// ErrBuildSearchUnavailable — items queries still run (the empty-scope
// short-circuit of a nil ACL answers the honest empty set, the items
// family's own nil-seam posture).
func TestBuildEngineNoSearcher(t *testing.T) {
	e := NewEngine(EngineOptions{})
	if _, err := e.Run(context.Background(), &repo.Principal{Name: "admin", Admin: true}, `builds.find({})`); !errors.Is(err, ErrBuildSearchUnavailable) {
		t.Fatalf("err = %v, want ErrBuildSearchUnavailable", err)
	}
	res, err := e.Run(context.Background(), &repo.Principal{Name: "admin", Admin: true}, `items.find({})`)
	if err != nil || res == nil || len(res.Rows) != 0 || res.EntryDomain != "" {
		t.Fatalf("items result = %+v err = %v, want the honest empty set", res, err)
	}
}
