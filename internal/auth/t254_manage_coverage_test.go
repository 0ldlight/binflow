package auth_test

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// T-254 (M9 E9, ADR-0030 / architecture section 14.1.9): auth.ManageCoverage
// — the single decision point behind E6's filter branch and the family-4
// write arms. Under test: the universe sentinel for admin, the empty
// non-universe set for readonly_admin (the role holds no m anywhere), the
// user-direct + group union for plain users, the fail-closed posture on
// every failure shape, and the equivalence with Can(repo, "", "m") that
// makes the write-arm convergence behavior-preserving.

// t254PutTarget writes one target with its principal rows straight through
// the store (the unit seam — the wire's own validation is irrelevant here,
// and hand-shaped rows are exactly what the fail-closed legs need).
func t254PutTarget(t *testing.T, st metadata.Store, name, repos string, rows []*metadata.PermissionPrincipal) {
	t.Helper()
	err := st.Permissions().PutTarget(context.Background(), &metadata.PermissionTarget{
		Name: name, Repos: repos, Includes: "[]", Excludes: "[]",
		CreatedAt: "2026-08-24T00:00:00Z", UpdatedAt: "2026-08-24T00:00:00Z",
	}, rows)
	if err != nil {
		t.Fatalf("PutTarget(%s): %v", name, err)
	}
}

func TestT254ManageCoverageRoleAndUnionShapes(t *testing.T) {
	f := newFixture(t, false)
	ctx := context.Background()
	st := f.st

	t254PutTarget(t, st, "t254-direct", `["r-dir-1","r-dir-2"]`, []*metadata.PermissionPrincipal{
		{TargetName: "t254-direct", Principal: "ci-bot", PrincipalType: "user", CanManage: true},
	})
	t254PutTarget(t, st, "t254-group", `["r-grp-1"]`, []*metadata.PermissionPrincipal{
		{TargetName: "t254-group", Principal: "app-admins", PrincipalType: "group", CanManage: true},
		{TargetName: "t254-group", Principal: "other", PrincipalType: "user", CanRead: true},
	})
	// A read-only target for a DIFFERENT group: its read row must not leak
	// coverage to that group's members.
	t254PutTarget(t, st, "t254-read-only", `["r-read-1"]`, []*metadata.PermissionPrincipal{
		{TargetName: "t254-read-only", Principal: "readers", PrincipalType: "group", CanRead: true},
	})

	cases := []struct {
		name        string
		p           *auth.Principal
		want        []string // nil means "the universe sentinel"
		wantUnivers bool
	}{
		{"nil principal is the empty deny", nil, []string{}, false},
		{"admin is the universe sentinel", &auth.Principal{Name: "root", Role: auth.RoleAdmin}, nil, true},
		{"admin via the derived Admin flag", &auth.Principal{Name: "root", Admin: true}, nil, true},
		{"readonly_admin holds no manage bit", &auth.Principal{Name: "aud", Role: auth.RoleReadOnlyAdmin}, []string{}, false},
		{"direct user m unions the whole target repos list",
			&auth.Principal{Name: "ci-bot"}, []string{"r-dir-1", "r-dir-2"}, false},
		{"the user-name match is case-insensitive (Can's rule)",
			&auth.Principal{Name: "CI-BOT"}, []string{"r-dir-1", "r-dir-2"}, false},
		{"group m unions alongside nothing else",
			&auth.Principal{Name: "someone", Groups: []string{"app-admins"}}, []string{"r-grp-1"}, false},
		{"group m and direct m union",
			&auth.Principal{Name: "ci-bot", Groups: []string{"app-admins"}},
			[]string{"r-dir-1", "r-dir-2", "r-grp-1"}, false},
		{"a group read row grants no coverage",
			&auth.Principal{Name: "reader", Groups: []string{"readers"}}, []string{}, false},
		{"a user row naming another user grants nothing",
			&auth.Principal{Name: "stranger"}, []string{}, false},
		{"a user row spelling a group name grants nothing (type-keyed)",
			&auth.Principal{Name: "app-admins"}, []string{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, universe := f.svc.ManageCoverage(ctx, tc.p)
			if universe != tc.wantUnivers {
				t.Fatalf("universe = %v, want %v", universe, tc.wantUnivers)
			}
			if universe {
				if got != nil {
					t.Fatalf("universe sentinel must carry a nil map, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("non-universe coverage must be a non-nil map")
			}
			var keys []string
			for k := range got {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if len(keys) != len(tc.want) {
				t.Fatalf("coverage = %v, want %v", keys, tc.want)
			}
			for i := range keys {
				if keys[i] != tc.want[i] {
					t.Fatalf("coverage = %v, want %v", keys, tc.want)
				}
			}
		})
	}
}

// TestT254ManageCoverageMatchesCan: the equivalence the family-4 write-arm
// convergence preserves — for a plain user, repo ∈ coverage iff
// Can(repo, "", "m"). Every cell of the (principal x repository) grid must
// agree, or the read face (E6) and the write arms (POST/DELETE) would
// disagree on what a holder covers.
func TestT254ManageCoverageMatchesCan(t *testing.T) {
	f := newFixture(t, false)
	ctx := context.Background()
	t254PutTarget(t, f.st, "t254-grid", `["r-in-1","r-in-2"]`, []*metadata.PermissionPrincipal{
		{TargetName: "t254-grid", Principal: "ci-bot", PrincipalType: "user", CanManage: true},
		{TargetName: "t254-grid", Principal: "app-admins", PrincipalType: "group", CanRead: true},
	})

	principals := []*auth.Principal{
		{Name: "ci-bot"},
		{Name: "other", Groups: []string{"app-admins"}},
		{Name: "stranger"},
	}
	repos := []string{"r-in-1", "r-in-2", "r-out-1"}
	for _, p := range principals {
		coverage, universe := f.svc.ManageCoverage(ctx, p)
		if universe {
			t.Fatalf("%s: plain user must never be the universe", p.Name)
		}
		for _, repo := range repos {
			_, inCoverage := coverage[repo]
			if want := f.svc.Can(ctx, p, repo, "", auth.ActionManage); want != inCoverage {
				t.Fatalf("%s x %s: Can(m)=%v but coverage membership=%v — the faces disagree",
					p.Name, repo, want, inCoverage)
			}
		}
	}
}

// t254CoveragePerms is the full fake over the permission plane: the two
// permissionSource methods plus the E9 rows facet, each with a failure knob.
type t254CoveragePerms struct {
	failRows    bool
	failTargets bool

	targets []auth.Target
	rows    []auth.PermissionRow
}

func (p *t254CoveragePerms) ListTargets(context.Context) ([]auth.Target, error) {
	if p.failTargets {
		return nil, errors.New("boom: targets table unavailable")
	}
	return p.targets, nil
}

func (p *t254CoveragePerms) PrincipalsFor(context.Context, string) ([]auth.PermissionRow, error) {
	return nil, nil
}

func (p *t254CoveragePerms) Principals(context.Context) ([]auth.PermissionRow, error) {
	if p.failRows {
		return nil, errors.New("boom: principals table unavailable")
	}
	return p.rows, nil
}

// TestT254ManageCoverageFailsClosed: every failure shape denies — the empty,
// non-universe set. Store errors must never widen access (Can's posture),
// a malformed target repos column skips that target, and a permission
// source without the rows facet (a unit fake predating the seam) denies
// rather than guessing.
func TestT254ManageCoverageFailsClosed(t *testing.T) {
	ctx := context.Background()
	holder := &auth.Principal{Name: "ci-bot", Groups: []string{"app-admins"}}
	live := &t254CoveragePerms{
		targets: []auth.Target{{Name: "t", Repos: `["r-live"]`, Includes: "[]", Excludes: "[]"}},
		rows: []auth.PermissionRow{
			{TargetName: "t", Principal: "ci-bot", PrincipalType: "user", CanManage: true},
		},
	}

	cases := []struct {
		name string
		perm *t254CoveragePerms
		want []string
	}{
		{"rows query failure denies", &t254CoveragePerms{failRows: true}, nil},
		{"target listing failure denies", &t254CoveragePerms{failTargets: true}, nil},
		{"a malformed target skips only itself", &t254CoveragePerms{
			targets: []auth.Target{
				{Name: "bad", Repos: `{not json`, Includes: "[]", Excludes: "[]"},
				{Name: "t", Repos: `["r-live"]`, Includes: "[]", Excludes: "[]"},
			},
			rows: []auth.PermissionRow{
				{TargetName: "bad", Principal: "ci-bot", PrincipalType: "user", CanManage: true},
				{TargetName: "t", Principal: "ci-bot", PrincipalType: "user", CanManage: true},
			},
		}, []string{"r-live"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newFixture(t, false).svc.WithPermissions(tc.perm)
			got, universe := svc.ManageCoverage(ctx, holder)
			if universe {
				t.Fatalf("failure must never produce the universe")
			}
			if got == nil {
				t.Fatalf("non-universe coverage must be a non-nil map")
			}
			if len(got) != len(tc.want) {
				t.Fatalf("coverage = %v, want %v", got, tc.want)
			}
			for _, want := range tc.want {
				if _, ok := got[want]; !ok {
					t.Fatalf("coverage = %v, want %v", got, tc.want)
				}
			}
		})
	}

	// Facet missing: failingPerms (failclosed_test.go's fake) implements
	// the two permissionSource methods and nothing else — the coverage
	// answer is the deny, not a panic or a pass.
	svc := newFixture(t, false).svc.WithPermissions(&failingPerms{})
	if got, universe := svc.ManageCoverage(ctx, holder); universe || len(got) != 0 {
		t.Fatalf("facet-less source: coverage = %v, universe %v; want empty deny", got, universe)
	}

	// Control: the same construction with the live source answers the
	// grant — the denials above come from the failure branches.
	svc = newFixture(t, false).svc.WithPermissions(live)
	got, universe := svc.ManageCoverage(ctx, holder)
	if universe || len(got) != 1 {
		t.Fatalf("live control: coverage = %v, universe %v; want exactly [r-live]", got, universe)
	}
	if _, ok := got["r-live"]; !ok {
		t.Fatalf("live control: coverage = %v, want r-live", got)
	}
}
