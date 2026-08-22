// T-212 acceptance surface (M7, ADR-0026 as finalized by T-214): the closed
// role set, the six-capability management plane and the repo-scoped m action.
// The matrices are exhaustive by design — a closed set is enumerable, which is
// the whole review argument for closed sets (ADR-0026 rationale).

package auth_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// putTargetManage installs one named target whose principal row carries the
// manage bit (plus optional r/w/d) — the m-plane sibling of fixture.putTarget.
func putTargetManage(t *testing.T, f *fixture, name string, repos []string, principal string, read, write, del, manage bool) {
	t.Helper()
	mk := func(list []string) string {
		if list == nil {
			list = []string{}
		}
		b, err := json.Marshal(list)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(b)
	}
	now := metadata.Now()
	if err := f.st.Permissions().PutTarget(f.ctx,
		&metadata.PermissionTarget{
			Name: name, Repos: mk(repos), Includes: mk(nil), Excludes: mk(nil),
			CreatedAt: now, UpdatedAt: now,
		},
		[]*metadata.PermissionPrincipal{{
			TargetName: name, Principal: principal, PrincipalType: "user",
			CanRead: read, CanWrite: write, CanDelete: del, CanManage: manage,
		}}); err != nil {
		t.Fatalf("put target %q: %v", name, err)
	}
}

// rolePrincipal builds a principal whose role is explicit (the way the
// authentication arms fill it since T-212).
func rolePrincipal(name string, role auth.Role) *auth.Principal {
	return &auth.Principal{Name: name, Role: role, Admin: role == auth.RoleAdmin}
}

// TestParseRole pins the closed set and its wire spellings (T-214③: snake
// values only — kebab was deliberated and rejected).
func TestParseRole(t *testing.T) {
	tests := []struct {
		in   string
		want auth.Role
		ok   bool
	}{
		{"admin", auth.RoleAdmin, true},
		{"readonly_admin", auth.RoleReadOnlyAdmin, true},
		{"user", auth.RoleUser, true},
		{"read-only-admin", "", false}, // kebab: rejected by T-214③
		{"", "", false},
		{"superuser", "", false},
		{"ADMIN", "", false}, // closed set is case-sensitive
	}
	for _, tt := range tests {
		got, ok := auth.ParseRole(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("ParseRole(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

// TestPrincipalEffectiveRole pins the derivation rules: an explicit closed-set
// Role wins, a bare Admin flag folds to admin (hand-built principals keep
// working), everything else is user. Nil answers user — CanManage's nil gate
// is separate and also tested.
func TestPrincipalEffectiveRole(t *testing.T) {
	tests := []struct {
		name string
		p    *auth.Principal
		want auth.Role
	}{
		{"explicit admin", &auth.Principal{Role: auth.RoleAdmin}, auth.RoleAdmin},
		{"explicit readonly", &auth.Principal{Role: auth.RoleReadOnlyAdmin}, auth.RoleReadOnlyAdmin},
		{"explicit user", &auth.Principal{Role: auth.RoleUser}, auth.RoleUser},
		{"bare admin flag (hand-built)", &auth.Principal{Admin: true}, auth.RoleAdmin},
		{"empty principal", &auth.Principal{}, auth.RoleUser},
		{"garbage role string", &auth.Principal{Role: auth.Role("root")}, auth.RoleUser},
		{"role beats inconsistent admin flag", &auth.Principal{Role: auth.RoleReadOnlyAdmin, Admin: true}, auth.RoleReadOnlyAdmin},
		{"nil principal", nil, auth.RoleUser},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.EffectiveRole(); got != tt.want {
				t.Fatalf("EffectiveRole() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCanManageCapabilityMatrix is the full 3x6 management-plane matrix
// (ADR-0026 rationale: the closed set exists so this table is exhaustive)
// plus the two degenerate rows: anonymous and an unknown capability value.
func TestCanManageCapabilityMatrix(t *testing.T) {
	f := newFixture(t, false)
	readOnlyCaps := map[auth.ManagementCapability]bool{
		auth.CapSystemRead:   true,
		auth.CapSecurityRead: true,
		auth.CapRepoRead:     true,
	}
	for _, role := range []auth.Role{auth.RoleAdmin, auth.RoleReadOnlyAdmin, auth.RoleUser} {
		p := rolePrincipal("matrix-"+string(role), role)
		for _, cap := range auth.ManagementCapabilities {
			want := false
			switch role {
			case auth.RoleAdmin:
				want = true
			case auth.RoleReadOnlyAdmin:
				want = readOnlyCaps[cap]
			}
			t.Run(string(role)+" "+string(cap), func(t *testing.T) {
				if got := f.svc.CanManage(f.ctx, p, cap); got != want {
					t.Fatalf("CanManage(%s, %s) = %v, want %v", role, cap, got, want)
				}
			})
		}
	}
	// GC dry-run and every write capability deny readonly_admin — the
	// invariant behind T-214's "readonly roles never POST write routes"
	// ruling is the matrix above; this spot check names it for reviewers.
	ro := rolePrincipal("auditor", auth.RoleReadOnlyAdmin)
	for _, cap := range []auth.ManagementCapability{auth.CapSystemWrite, auth.CapSecurityWrite, auth.CapRepoWrite} {
		if f.svc.CanManage(f.ctx, ro, cap) {
			t.Fatalf("readonly_admin must not hold %s", cap)
		}
	}
	// Degenerate rows.
	if f.svc.CanManage(f.ctx, nil, auth.CapSystemRead) {
		t.Fatal("anonymous must not hold any capability (the 401 gate is the caller's; this layer answers false)")
	}
	if f.svc.CanManage(f.ctx, rolePrincipal("a", auth.RoleAdmin), auth.ManagementCapability("system:superuser")) {
		t.Fatal("unknown capability values deny for every role, admin included (fail closed on typos)")
	}
}

// TestCanManageRepoMatrix pins evaluation chain ③ for the two verb values
// across the role set: admin both, readonly_admin read-only, user = the m
// action (covered separately below), anonymous never.
func TestCanManageRepoMatrix(t *testing.T) {
	f := newFixture(t, false)
	tests := []struct {
		role  auth.Role
		write bool
		want  bool
	}{
		{auth.RoleAdmin, true, true},
		{auth.RoleAdmin, false, true},
		{auth.RoleReadOnlyAdmin, true, false},
		{auth.RoleReadOnlyAdmin, false, true},
		{auth.RoleUser, true, false}, // no target grants m in this fixture
		{auth.RoleUser, false, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.role)+" write="+boolStr(tt.write), func(t *testing.T) {
			p := rolePrincipal("repo-"+string(tt.role), tt.role)
			if got := f.svc.CanManageRepo(f.ctx, p, "some-repo", tt.write); got != tt.want {
				t.Fatalf("CanManageRepo(%s, write=%v) = %v, want %v", tt.role, tt.write, got, tt.want)
			}
		})
	}
	if f.svc.CanManageRepo(f.ctx, nil, "some-repo", false) {
		t.Fatal("anonymous must not manage repos")
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// TestCanManageAction is the m-action trio the ticket names: the empty grant
// set, the covering subset and the partial intersection — plus the two
// non-interference rules (m implies nothing on the path plane, the path plane
// implies no m) and the readonly short-circuit.
func TestCanManageAction(t *testing.T) {
	const (
		repoA = "libs-release"
		repoB = "other-repo"
	)

	t.Run("empty set: no targets at all", func(t *testing.T) {
		f := newFixture(t, false)
		u := &auth.Principal{Name: "plain"}
		for _, act := range []string{auth.ActionManage, auth.ActionRead} {
			if f.svc.Can(f.ctx, u, repoA, "a.bin", act) {
				t.Fatalf("action %s must deny with no targets", act)
			}
		}
		if f.svc.CanManageRepo(f.ctx, u, repoA, true) {
			t.Fatal("m-based repo management must deny with no targets")
		}
	})

	t.Run("subset: target lists the repo, row carries m", func(t *testing.T) {
		f := newFixture(t, false)
		putTargetManage(t, f, "repo-admin-t", []string{repoA}, "carol", false, false, false, true)
		carol := &auth.Principal{Name: "carol"}
		if !f.svc.Can(f.ctx, carol, repoA, "any/path.bin", auth.ActionManage) {
			t.Fatal("m must be granted when repos[] lists the repo and the row carries can_manage")
		}
		if !f.svc.CanManageRepo(f.ctx, carol, repoA, true) {
			t.Fatal("CanManageRepo(user, write) must follow the m grant")
		}
		if !f.svc.CanManageRepo(f.ctx, carol, repoA, false) {
			t.Fatal("CanManageRepo(user, read) must follow the m grant too")
		}
	})

	t.Run("partial intersection: m on one repo only", func(t *testing.T) {
		f := newFixture(t, false)
		putTargetManage(t, f, "half-coverage", []string{repoA}, "carol", true, false, false, true)
		carol := &auth.Principal{Name: "carol"}
		if !f.svc.Can(f.ctx, carol, repoA, "x", auth.ActionManage) {
			t.Fatal("covered repo must grant m")
		}
		if f.svc.Can(f.ctx, carol, repoB, "x", auth.ActionManage) {
			t.Fatal("uncovered repo must deny m (partial coverage is not global)")
		}
		if f.svc.CanManageRepo(f.ctx, carol, repoB, true) {
			t.Fatal("repo management outside the m coverage must deny")
		}
	})

	t.Run("includes and excludes never participate in m", func(t *testing.T) {
		f := newFixture(t, false)
		// One target whose path plane DENIES everything for carol (an
		// exclude hit), yet its row carries m on the listed repo: m is
		// repository-configuration power and has no path subdomain.
		mk := func(l []string) string {
			b, _ := json.Marshal(l)
			return string(b)
		}
		now := metadata.Now()
		if err := f.st.Permissions().PutTarget(f.ctx,
			&metadata.PermissionTarget{
				Name: repoA + "-path-denied", Repos: mk([]string{repoA}),
				Includes: mk([]string{"nothing/**"}), Excludes: mk([]string{"**"}),
				CreatedAt: now, UpdatedAt: now,
			},
			[]*metadata.PermissionPrincipal{{
				TargetName: repoA + "-path-denied", Principal: "carol", PrincipalType: "user",
				CanRead: true, CanManage: true,
			}}); err != nil {
			t.Fatalf("put target: %v", err)
		}
		carol := &auth.Principal{Name: "carol"}
		if f.svc.Can(f.ctx, carol, repoA, "a.bin", auth.ActionRead) {
			t.Fatal("path plane sanity: the exclude must deny r")
		}
		if !f.svc.Can(f.ctx, carol, repoA, "a.bin", auth.ActionManage) {
			t.Fatal("m must ignore includes/excludes and match on repos[] alone")
		}
	})

	t.Run("m implies no path-plane action", func(t *testing.T) {
		f := newFixture(t, false)
		putTargetManage(t, f, "m-only", []string{repoA}, "carol", false, false, false, true)
		carol := &auth.Principal{Name: "carol"}
		for _, act := range []string{auth.ActionRead, auth.ActionWrite, auth.ActionDelete} {
			if f.svc.Can(f.ctx, carol, repoA, "a.bin", act) {
				t.Fatalf("m must not imply %s (no privilege chain)", act)
			}
		}
	})

	t.Run("path-plane actions imply no m", func(t *testing.T) {
		f := newFixture(t, false)
		f.putTarget("rwd-t", []string{repoA}, []string{"**"}, nil, "carol", true, true, true)
		carol := &auth.Principal{Name: "carol"}
		if f.svc.Can(f.ctx, carol, repoA, "a.bin", auth.ActionManage) {
			t.Fatal("r/w/d must not imply m")
		}
	})

	t.Run("readonly_admin short-circuits the target plane", func(t *testing.T) {
		f := newFixture(t, false)
		// A w/d-granting target covering the auditor by group membership:
		// combination is INEFFECTIVE, not illegal (T-214①).
		if err := f.st.Permissions().PutTarget(f.ctx,
			targetOf("trap", []string{repoA}, []string{"**"}, nil),
			groupRow("trap", "auditors")); err != nil {
			t.Fatalf("put group target: %v", err)
		}
		auditor := &auth.Principal{Name: "auditor", Role: auth.RoleReadOnlyAdmin, Groups: []string{"auditors"}}
		if !f.svc.Can(f.ctx, auditor, repoA, "a.bin", auth.ActionRead) {
			t.Fatal("readonly_admin r is global (even with anonymous read off and no covering target)")
		}
		if !f.svc.Can(f.ctx, auditor, "repo-never-configured", "x/y.bin", auth.ActionRead) {
			t.Fatal("readonly_admin r must not depend on any target")
		}
		for _, act := range []string{auth.ActionWrite, auth.ActionDelete, auth.ActionManage} {
			if f.svc.Can(f.ctx, auditor, repoA, "a.bin", act) {
				t.Fatalf("readonly_admin %s must be denied even when a covering target carries it", act)
			}
		}
	})

	t.Run("anonymous never holds m", func(t *testing.T) {
		f := newFixture(t, true) // anonymous read on
		putTargetManage(t, f, "anon-t", []string{repoA}, "carol", false, false, false, true)
		if f.svc.Can(f.ctx, nil, repoA, "a.bin", auth.ActionManage) {
			t.Fatal("anonymous read must not extend to m")
		}
		if f.svc.Can(f.ctx, nil, repoA, "a.bin", auth.ActionWrite) {
			t.Fatal("anonymous read must not extend to w")
		}
	})
}

// TestVerifyRoleImmediateEffect pins the immediate-effect seam (ADR-0026
// decision 6, the T-208 enabled precedent): Verify re-reads the owner row's
// role on every call, so an existing token's management-plane permissions
// follow a role change with no re-issue — the V04 premise.
func TestVerifyRoleImmediateEffect(t *testing.T) {
	f := newFixture(t, false)
	tok, err := f.svc.Issue(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	p, err := f.svc.Verify(f.ctx, tok.AccessToken)
	if err != nil {
		t.Fatalf("Verify (user): %v", err)
	}
	if p.Role != auth.RoleUser {
		t.Fatalf("fresh token role = %q, want user", p.Role)
	}
	if f.svc.CanManage(f.ctx, p, auth.CapSystemRead) {
		t.Fatal("a plain user holds no management capability")
	}

	// Promote to readonly_admin behind the token's back.
	if err := f.st.Users().SetRole(f.ctx, "ci-bot", metadata.RoleReadOnlyAdmin); err != nil {
		t.Fatalf("SetRole(readonly_admin): %v", err)
	}
	p, err = f.svc.Verify(f.ctx, tok.AccessToken)
	if err != nil {
		t.Fatalf("Verify (readonly): %v", err)
	}
	if p.Role != auth.RoleReadOnlyAdmin || p.Admin {
		t.Fatalf("role after change = (%q, admin=%v), want (readonly_admin, false)", p.Role, p.Admin)
	}
	if !f.svc.CanManage(f.ctx, p, auth.CapSystemRead) {
		t.Fatal("the SAME token must immediately read the management plane")
	}
	if f.svc.CanManage(f.ctx, p, auth.CapSystemWrite) {
		t.Fatal("readonly_admin must not gain write capabilities")
	}
	if f.svc.Can(f.ctx, p, "libs-release", "a.bin", auth.ActionWrite) {
		t.Fatal("readonly_admin content plane must be read-only")
	}

	// And back down to user.
	if err := f.st.Users().SetRole(f.ctx, "ci-bot", metadata.RoleUser); err != nil {
		t.Fatalf("SetRole(user): %v", err)
	}
	p, err = f.svc.Verify(f.ctx, tok.AccessToken)
	if err != nil {
		t.Fatalf("Verify (demoted): %v", err)
	}
	if p.Role != auth.RoleUser || f.svc.CanManage(f.ctx, p, auth.CapSystemRead) {
		t.Fatal("demotion must take effect on the same token immediately")
	}
}

// TestSessionRoleImmediateEffect is the session-arm twin of the token test:
// the cookie re-resolves the owner row per request, role included.
func TestSessionRoleImmediateEffect(t *testing.T) {
	f := newFixture(t, false)
	sess, err := f.svc.IssueSession(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	r := req("", "")
	r.AddCookie(&http.Cookie{Name: auth.CookieSessionName, Value: sess.ID})
	p, err := f.svc.Authenticate(f.ctx, r)
	if err != nil {
		t.Fatalf("Authenticate (session): %v", err)
	}
	if p.Role != auth.RoleUser {
		t.Fatalf("session role = %q, want user", p.Role)
	}

	if err := f.st.Users().SetRole(f.ctx, "ci-bot", metadata.RoleReadOnlyAdmin); err != nil {
		t.Fatalf("SetRole: %v", err)
	}
	r2 := req("", "")
	r2.AddCookie(&http.Cookie{Name: auth.CookieSessionName, Value: sess.ID})
	p, err = f.svc.Authenticate(f.ctx, r2)
	if err != nil {
		t.Fatalf("Authenticate (session, promoted): %v", err)
	}
	if p.Role != auth.RoleReadOnlyAdmin {
		t.Fatalf("session role after change = %q, want readonly_admin", p.Role)
	}
	if !f.svc.CanManageRepo(f.ctx, p, "any-repo", false) {
		t.Fatal("readonly_admin session must read-manage repos")
	}
	if f.svc.CanManageRepo(f.ctx, p, "any-repo", true) {
		t.Fatal("readonly_admin session must not write-manage repos")
	}
}
