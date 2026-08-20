package auth_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// createGroup inserts one group row; failing hard keeps test worlds obvious.
func (f *fixture) createGroup(name string) {
	f.t.Helper()
	now := metadata.Now()
	if err := f.st.Groups().Create(f.ctx, &metadata.Group{
		Name: name, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		f.t.Fatalf("create group %q: %v", name, err)
	}
}

// addMember puts username into group (replace semantics per user).
func (f *fixture) addMember(group, username string) {
	f.t.Helper()
	if err := f.st.Groups().SetUserGroups(f.ctx, username, []string{group}); err != nil {
		f.t.Fatalf("set %s into %s: %v", username, group, err)
	}
}

// putGroupTarget installs a target granting actions to a GROUP principal.
func (f *fixture) putGroupTarget(name string, repos, includes, excludes []string, group string, read, write, del bool) {
	f.t.Helper()
	if err := f.st.Permissions().PutTarget(f.ctx,
		targetOf(name, repos, includes, excludes),
		[]*metadata.PermissionPrincipal{{
			TargetName: name, Principal: group, PrincipalType: "group",
			CanRead: read, CanWrite: write, CanDelete: del,
		}}); err != nil {
		f.t.Fatalf("put group target %q: %v", name, err)
	}
}

// authenticateBasic resolves a principal through the Basic arm (the shared
// fill path every arm funnels through).
func (f *fixture) authenticateBasic(user, pass string) *auth.Principal {
	f.t.Helper()
	p, err := f.svc.Authenticate(f.ctx, req(basic(user, pass), ""))
	if err != nil {
		f.t.Fatalf("authenticate %q: %v", user, err)
	}
	if p == nil {
		f.t.Fatalf("authenticate %q: anonymous", user)
	}
	return p
}

// brokenGroupSource models a membership lookup that always fails (the
// fail-closed leg of the fill).
type brokenGroupSource struct{}

func (brokenGroupSource) GroupsOfUser(context.Context, string) ([]string, error) {
	return nil, errors.New("membership store unavailable")
}

// SE-07: the membership fill lands on every authentication arm's principal.
func TestGroupsFilledAtAuthentication(t *testing.T) {
	f := newFixture(t, true)
	f.createGroup("devs")
	f.addMember("devs", "ci-bot")

	p := f.authenticateBasic("ci-bot", ciPW)
	if len(p.Groups) != 1 || p.Groups[0] != "devs" {
		t.Fatalf("basic principal groups = %v, want [devs]", p.Groups)
	}

	tok, err := f.svc.Issue(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	// API-key arm (Authenticate) and the direct Verify seam both fill.
	p, err = f.svc.Authenticate(f.ctx, req("", tok.AccessToken))
	if err != nil {
		t.Fatalf("api-key authenticate: %v", err)
	}
	if len(p.Groups) != 1 || p.Groups[0] != "devs" {
		t.Fatalf("api-key principal groups = %v, want [devs]", p.Groups)
	}
	p, err = f.svc.Verify(f.ctx, tok.AccessToken)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(p.Groups) != 1 || p.Groups[0] != "devs" {
		t.Fatalf("verify principal groups = %v, want [devs]", p.Groups)
	}

	// Group-less users carry the empty set, not an error.
	p = f.authenticateBasic(otherUser, "other-pw")
	if len(p.Groups) != 0 {
		t.Fatalf("group-less principal groups = %v, want empty", p.Groups)
	}
}

// SE-07 union: the effective grant set is the user's own rows UNION the
// rows of every group they belong to (W19/W19c).
func TestCanGroupUnion(t *testing.T) {
	const (
		repo  = "generic-local"
		incl  = "devs/**"
		inPth = "devs/w.bin"
	)
	setup := func(t *testing.T) *fixture {
		t.Helper()
		f := newFixture(t, false)
		f.createGroup("devs")
		f.addMember("devs", "ci-bot")
		// Group grants read+write on devs/** (W19's devs-rw).
		f.putGroupTarget("devs-rw", []string{repo}, []string{incl}, nil, "devs", true, true, false)
		return f
	}

	t.Run("member inherits the group grant", func(t *testing.T) {
		f := setup(t)
		p := f.authenticateBasic("ci-bot", ciPW)
		for _, tc := range []struct {
			action string
			want   bool
		}{
			{auth.ActionRead, true},
			{auth.ActionWrite, true},
			{auth.ActionDelete, false}, // group has no delete (W19's DELETE 403)
		} {
			if got := f.svc.Can(f.ctx, p, repo, inPth, tc.action); got != tc.want {
				t.Fatalf("member Can(%s) = %v, want %v", tc.action, got, tc.want)
			}
		}
		// Outside the pattern and outside the repo: denied both.
		if f.svc.Can(f.ctx, p, repo, "other/o.bin", auth.ActionWrite) {
			t.Fatal("write outside the include pattern must deny")
		}
		if f.svc.Can(f.ctx, p, "other-repo", inPth, auth.ActionWrite) {
			t.Fatal("write on an unlisted repo must deny")
		}
	})

	t.Run("non-member gets nothing from the group row", func(t *testing.T) {
		f := setup(t)
		p := f.authenticateBasic(otherUser, "other-pw")
		if f.svc.Can(f.ctx, p, repo, inPth, auth.ActionRead) {
			t.Fatal("non-member must not inherit")
		}
	})

	t.Run("user and group grants union (W19c)", func(t *testing.T) {
		f := setup(t)
		// Direct user grant: read only, on a wider path set.
		f.putTarget("user-direct", []string{repo}, []string{"**"}, nil, "ci-bot", true, false, false)
		p := f.authenticateBasic("ci-bot", ciPW)
		if !f.svc.Can(f.ctx, p, repo, inPth, auth.ActionRead) {
			t.Fatal("direct read grant must hold")
		}
		if !f.svc.Can(f.ctx, p, repo, inPth, auth.ActionWrite) {
			t.Fatal("group write grant must union in")
		}
		// The direct grant widens beyond the group's pattern.
		if !f.svc.Can(f.ctx, p, repo, "anywhere/x.bin", auth.ActionRead) {
			t.Fatal("direct read outside the group pattern must hold")
		}
		if f.svc.Can(f.ctx, p, repo, "anywhere/x.bin", auth.ActionWrite) {
			t.Fatal("group write must stay pattern-scoped")
		}
		// A delete granted by a second group union: one success suffices.
		f.createGroup("cleaners")
		f.st.Groups().SetUserGroups(f.ctx, "ci-bot", []string{"devs", "cleaners"}) //nolint:errcheck // fixture hard-fails elsewhere
		f.putGroupTarget("clean-d", []string{repo}, []string{"**"}, nil, "cleaners", false, false, true)
		p = f.authenticateBasic("ci-bot", ciPW)
		if !f.svc.Can(f.ctx, p, repo, inPth, auth.ActionDelete) {
			t.Fatal("delete via a second group must union in")
		}
	})

	t.Run("exclude priority holds inside group targets", func(t *testing.T) {
		f := newFixture(t, false)
		f.createGroup("devs")
		f.addMember("devs", "ci-bot")
		f.putGroupTarget("carved", []string{repo}, []string{"**"}, []string{"priv/**"}, "devs", true, true, true)
		p := f.authenticateBasic("ci-bot", ciPW)
		if f.svc.Can(f.ctx, p, repo, "priv/k.bin", auth.ActionRead) {
			t.Fatal("excluded path must deny through the group grant too")
		}
		if !f.svc.Can(f.ctx, p, repo, "open/k.bin", auth.ActionRead) {
			t.Fatal("non-excluded path must allow")
		}
	})

	t.Run("folder trailing-slash convention holds for group grants", func(t *testing.T) {
		f := newFixture(t, false)
		f.createGroup("devs")
		f.addMember("devs", "ci-bot")
		// Pattern "devs" names a directory: it covers the folder listing
		// "devs/" (matchStart under the isFolder gate) but not files below.
		f.putGroupTarget("folder", []string{"generic-local"}, []string{"devs"}, nil, "devs", true, false, false)
		p := f.authenticateBasic("ci-bot", ciPW)
		if !f.svc.Can(f.ctx, p, "generic-local", "devs/", auth.ActionRead) {
			t.Fatal("folder listing must be covered by the directory-naming pattern")
		}
		if f.svc.Can(f.ctx, p, "generic-local", "devs/a.bin", auth.ActionRead) {
			t.Fatal("a file below the directory must NOT be covered (use devs/**)")
		}
	})
}

// NFR-S25: membership changes take effect on the NEXT request — the fill is
// per-authentication, no cache, no window, no restart (W19b).
func TestGroupMembershipImmediateEffect(t *testing.T) {
	f := newFixture(t, false)
	f.createGroup("devs")
	f.putGroupTarget("devs-rw", []string{"generic-local"}, []string{"devs/**"}, nil, "devs", false, true, false)

	f.addMember("devs", "ci-bot")
	p := f.authenticateBasic("ci-bot", ciPW)
	if !f.svc.Can(f.ctx, p, "generic-local", "devs/w.bin", auth.ActionWrite) {
		t.Fatal("member write must hold before the removal")
	}

	// Remove from the group: the very next authentication sees it.
	f.st.Groups().SetUserGroups(f.ctx, "ci-bot", nil) //nolint:errcheck // fixture hard-fails elsewhere
	p = f.authenticateBasic("ci-bot", ciPW)
	if f.svc.Can(f.ctx, p, "generic-local", "devs/w2.bin", auth.ActionWrite) {
		t.Fatal("write must deny immediately after the removal (no window)")
	}
}

// Fail-closed fill: a broken membership source never opens group-derived
// grants; the credential itself stays valid (own grants keep working).
func TestGroupsFillFailsClosed(t *testing.T) {
	f := newFixture(t, false)
	f.createGroup("devs")
	f.addMember("devs", "ci-bot")
	f.putGroupTarget("devs-rw", []string{"generic-local"}, []string{"**"}, nil, "devs", true, true, true)
	f.putTarget("user-direct", []string{"generic-local"}, []string{"direct/**"}, nil, "ci-bot", true, false, false)

	svc := f.svc.WithGroups(brokenGroupSource{})
	p, err := svc.Authenticate(f.ctx, req(basic("ci-bot", ciPW), ""))
	if err != nil {
		t.Fatalf("authentication must survive a broken membership lookup: %v", err)
	}
	if len(p.Groups) != 0 {
		t.Fatalf("groups = %v, want empty on lookup failure", p.Groups)
	}
	if svc.Can(f.ctx, p, "generic-local", "devs/w.bin", auth.ActionWrite) {
		t.Fatal("group-derived grant must deny when the lookup failed")
	}
	if !svc.Can(f.ctx, p, "generic-local", "direct/x.bin", auth.ActionRead) {
		t.Fatal("the user's own grant must survive")
	}
}

// SE-08: ItemPrincipals renders exactly the covering grants, split into
// users/groups, with the same predicate Can applies.
func TestItemPrincipals(t *testing.T) {
	f := newFixture(t, false)
	f.createGroup("devs")
	f.createGroup("readers")
	f.addMember("devs", "ci-bot")
	f.putTarget("jane-direct", []string{"generic-local"}, []string{"devs/**"}, []string{"devs/secret.bin"}, "ci-bot", true, false, false)
	f.putGroupTarget("devs-rw", []string{"generic-local"}, []string{"devs/**"}, nil, "devs", true, true, false)
	f.putGroupTarget("readers-any", []string{"generic-local"}, []string{"**"}, nil, "readers", true, false, false)
	// A target scoped to another repo contributes nothing here.
	f.putGroupTarget("other-repo", []string{"elsewhere"}, []string{"**"}, nil, "readers", true, true, true)

	t.Run("union per principal, only covering targets", func(t *testing.T) {
		users, groups, err := f.svc.ItemPrincipals(f.ctx, "generic-local", "devs/w.bin")
		if err != nil {
			t.Fatalf("ItemPrincipals: %v", err)
		}
		if got := users["ci-bot"]; !got.Read || got.Write || got.Delete {
			t.Fatalf("users[ci-bot] = %+v, want read only", got)
		}
		if got := groups["devs"]; !got.Read || !got.Write || got.Delete {
			t.Fatalf("groups[devs] = %+v, want r+w", got)
		}
		if got := groups["readers"]; !got.Read || got.Write || got.Delete {
			t.Fatalf("groups[readers] = %+v, want read only", got)
		}
		if len(users) != 1 || len(groups) != 2 {
			t.Fatalf("unexpected view sizes: users=%v groups=%v", users, groups)
		}
	})

	t.Run("excluded path drops the excluding target only", func(t *testing.T) {
		users, groups, err := f.svc.ItemPrincipals(f.ctx, "generic-local", "devs/secret.bin")
		if err != nil {
			t.Fatalf("ItemPrincipals: %v", err)
		}
		if _, ok := users["ci-bot"]; ok {
			t.Fatal("the excluding target must not cover its excluded path")
		}
		if _, ok := groups["devs"]; !ok {
			t.Fatal("the unexcluded group target still covers")
		}
	})

	t.Run("uncovered path answers empty, never nil", func(t *testing.T) {
		users, groups, err := f.svc.ItemPrincipals(f.ctx, "unknown-repo", "x")
		if err != nil {
			t.Fatalf("ItemPrincipals: %v", err)
		}
		if users == nil || groups == nil || len(users) != 0 || len(groups) != 0 {
			t.Fatalf("view = %v %v, want empty non-nil maps", users, groups)
		}
	})

	t.Run("view agrees with Can on every action", func(t *testing.T) {
		// The same predicate, checked through both doors: for each principal
		// combination the view shows, an actual authorization question must
		// answer identically.
		const path = "devs/w.bin"
		users, groups, err := f.svc.ItemPrincipals(f.ctx, "generic-local", path)
		if err != nil {
			t.Fatalf("ItemPrincipals: %v", err)
		}
		bitFor := func(b auth.PrincipalBits, action string) bool {
			switch action {
			case auth.ActionRead:
				return b.Read
			case auth.ActionWrite:
				return b.Write
			case auth.ActionDelete:
				return b.Delete
			}
			return false
		}
		for _, action := range []string{auth.ActionRead, auth.ActionWrite, auth.ActionDelete} {
			// ci-bot is a member of devs: its effective bits are the union.
			var eff auth.PrincipalBits
			if b, ok := users["ci-bot"]; ok {
				eff.Read = eff.Read || b.Read
				eff.Write = eff.Write || b.Write
				eff.Delete = eff.Delete || b.Delete
			}
			if b, ok := groups["devs"]; ok {
				eff.Read = eff.Read || b.Read
				eff.Write = eff.Write || b.Write
				eff.Delete = eff.Delete || b.Delete
			}
			p := &auth.Principal{Name: "ci-bot", Groups: []string{"devs"}}
			if got := f.svc.Can(f.ctx, p, "generic-local", path, action); got != bitFor(eff, action) {
				t.Fatalf("action %s: Can=%v but view=%v (disagreement)", action, got, bitFor(eff, action))
			}
		}
	})
}

// A request-level sanity pin behind the fill: the http.Request-based entry
// keeps working unchanged (compile-and-behave guard for the arm refactor).
func TestAuthenticateArmsUnchanged(t *testing.T) {
	f := newFixture(t, true)
	if p, err := f.svc.Authenticate(f.ctx, req("", "")); err != nil || p != nil {
		t.Fatalf("no credential must stay anonymous: %v %+v", err, p)
	}
	r, err := http.NewRequest(http.MethodGet, "/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := f.svc.Authenticate(f.ctx, r); err != nil || p != nil {
		t.Fatalf("bare request must stay anonymous: %v %+v", err, p)
	}
}
