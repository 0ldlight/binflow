// T-444 acceptance surface (FR-146.1, ADR-0044 K68 / architecture section
// 25.6): the annotate action's closed-set seat — the verb × permission-bit
// matrix of Can, the split's orthogonality invariants (w ↛ a, m ↛ a,
// a ↛ w), the role gates (admin bypass, readonly_admin constant deny), and
// the effective-permission view's annotate bit.

package auth_test

import (
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// grantBits installs one target whose single user row carries the exact
// action bits named — the full-five spelling putTarget's r/w/d triple
// cannot express (manage rode along since T-217, annotate is new).
func grantBits(f *fixture, name string, read, write, del, manage, annotate bool) {
	f.t.Helper()
	now := metadata.Now()
	if err := f.st.Permissions().PutTarget(f.ctx,
		&metadata.PermissionTarget{
			Name: name, Repos: `["libs-release"]`,
			Includes: `["ci/**"]`, Excludes: `["ci/secret.key"]`,
			CreatedAt: now, UpdatedAt: now,
		},
		[]*metadata.PermissionPrincipal{{
			TargetName: name, Principal: "ci-bot", PrincipalType: "user",
			CanRead: read, CanWrite: write, CanDelete: del, CanManage: manage, CanAnnotate: annotate,
		}}); err != nil {
		f.t.Fatalf("PutTarget %s: %v", name, err)
	}
}

// TestCanAnnotateVerbBitMatrix is the closed set's exhaustive table: every
// action in {r,w,d,m,a} against the eight meaningful single/dual-bit grant
// shapes. The split's two load-bearing rows: a write-only row DENIES
// annotate (the pre-T-444 shadow is gone) and an annotate-only row denies
// write (annotate opens no content-byte face).
func TestCanAnnotateVerbBitMatrix(t *testing.T) {
	const inPath = "ci/y.bin"
	tests := []struct {
		name string
		// the grant row's bits
		read, write, del, manage, annotate bool
		// the expected Can answers, in r/w/d/m/a order
		want [5]bool
	}{
		{"no bits", false, false, false, false, false, [5]bool{false, false, false, false, false}},
		{"read only", true, false, false, false, false, [5]bool{true, false, false, false, false}},
		{"write only (the split: no annotate)", false, true, false, false, false, [5]bool{false, true, false, false, false}},
		{"annotate only (no content face)", false, false, false, false, true, [5]bool{false, false, false, false, true}},
		{"write+annotate (the backfilled shape)", false, true, false, false, true, [5]bool{false, true, false, false, true}},
		{"read+annotate (the annotator without deploy)", true, false, false, false, true, [5]bool{true, false, false, false, true}},
		{"manage only (no privilege chain)", false, false, false, true, false, [5]bool{false, false, false, true, false}},
		{"full five", true, true, true, true, true, [5]bool{true, true, true, true, true}},
	}
	actions := []string{auth.ActionRead, auth.ActionWrite, auth.ActionDelete, auth.ActionManage, auth.ActionAnnotate}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, false)
			grantBits(f, "matrix", tt.read, tt.write, tt.del, tt.manage, tt.annotate)
			ci := &auth.Principal{Name: "ci-bot"}
			for i, act := range actions {
				if got := f.svc.Can(f.ctx, ci, "libs-release", inPath, act); got != tt.want[i] {
					t.Errorf("Can(%s) with bits r=%v w=%v d=%v m=%v a=%v = %v, want %v",
						act, tt.read, tt.write, tt.del, tt.manage, tt.annotate, got, tt.want[i])
				}
			}
		})
	}
}

// TestAnnotatePathPlaneSemantics: annotate evaluates on the path plane like
// r/w/d — the include pattern must cover the path and an exclude hit wins —
// and is fenced per repository. (manage's repo-scoped match is the
// contrast: an excluded path still answers m.)
func TestAnnotatePathPlaneSemantics(t *testing.T) {
	f := newFixture(t, false)
	grantBits(f, "scoped", true, false, false, true, true)
	ci := &auth.Principal{Name: "ci-bot"}

	cases := []struct {
		repo, path string
		annotate   bool
	}{
		{"libs-release", "ci/y.bin", true},
		{"libs-release", "ci/secret.key", false}, // exclude beats include
		{"libs-release", "elsewhere/z.bin", false},
		{"other-repo", "ci/y.bin", false}, // repo fence
	}
	for _, tc := range cases {
		if got := f.svc.Can(f.ctx, ci, tc.repo, tc.path, auth.ActionAnnotate); got != tc.annotate {
			t.Errorf("Can(a) on %s/%s = %v, want %v", tc.repo, tc.path, got, tc.annotate)
		}
	}
	// The manage contrast: repo-scoped, ignores the exclude.
	if !f.svc.Can(f.ctx, ci, "libs-release", "ci/secret.key", auth.ActionManage) {
		t.Error("Can(m) on the excluded path must stay true (manage has no path plane)")
	}
}

// TestAnnotateRoleGates: admin passes annotate through the role bypass and
// readonly_admin denies it with the rest of the write family — the property
// plane is a write plane for the globally read-only role.
func TestAnnotateRoleGates(t *testing.T) {
	f := newFixture(t, true)
	admin := &auth.Principal{Name: "admin", Admin: true}
	ro := &auth.Principal{Name: "auditor", Role: auth.RoleReadOnlyAdmin}

	if !f.svc.Can(f.ctx, admin, "any-repo", "any/path.bin", auth.ActionAnnotate) {
		t.Error("admin must pass annotate (role bypass)")
	}
	if f.svc.Can(f.ctx, ro, "libs-release", "ci/y.bin", auth.ActionAnnotate) {
		t.Error("readonly_admin must deny annotate")
	}
	if !f.svc.Can(f.ctx, nil, "libs-release", "ci/y.bin", auth.ActionRead) {
		t.Error("anonymous read stays on with the flag (unchanged)")
	}
	if f.svc.Can(f.ctx, nil, "libs-release", "ci/y.bin", auth.ActionAnnotate) {
		t.Error("anonymous annotate must deny (read-only anonymous plane)")
	}
}

// TestItemPrincipalsAnnotateBit: the ?permissions view's PrincipalBits
// carries the annotate fact with the same coverage predicate Can applies.
func TestItemPrincipalsAnnotateBit(t *testing.T) {
	f := newFixture(t, false)
	grantBits(f, "view", true, true, false, false, true)

	users, _, err := f.svc.ItemPrincipals(f.ctx, "libs-release", "ci/y.bin")
	if err != nil {
		t.Fatalf("ItemPrincipals: %v", err)
	}
	b, ok := users["ci-bot"]
	if !ok {
		t.Fatal("ci-bot missing from the view")
	}
	if !b.Read || !b.Write || !b.Annotate || b.Delete || b.Manage {
		t.Fatalf("PrincipalBits = %+v, want r+w+a only", b)
	}
	// The uncovered path renders no entry (same predicate as Can).
	users, _, err = f.svc.ItemPrincipals(f.ctx, "libs-release", "elsewhere/z.bin")
	if err != nil {
		t.Fatalf("ItemPrincipals (elsewhere): %v", err)
	}
	if _, ok := users["ci-bot"]; ok {
		t.Fatal("ci-bot must not appear for an uncovered path")
	}
}
