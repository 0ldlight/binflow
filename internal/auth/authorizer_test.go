package auth_test

import (
	"encoding/json"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

func targetOf(name string, repos, includes, excludes []string) *metadata.PermissionTarget {
	mk := func(l []string) string {
		if l == nil {
			l = []string{}
		}
		b, _ := json.Marshal(l)
		return string(b)
	}
	now := metadata.Now()
	return &metadata.PermissionTarget{
		Name: name, Repos: mk(repos), Includes: mk(includes), Excludes: mk(excludes),
		CreatedAt: now, UpdatedAt: now,
	}
}

func groupRow(target, principal string) []*metadata.PermissionPrincipal {
	return []*metadata.PermissionPrincipal{{
		TargetName: target, Principal: principal, PrincipalType: "group",
		CanRead: true, CanWrite: true, CanDelete: true,
	}}
}

// AC 3: the full permission matrix — admin / authorized / unauthorized /
// anonymous x r/w/d x anonymous-read on/off — plus pattern semantics inside
// the Authorizer (exclude priority, empty includes, repo scoping,
// case-insensitive usernames).
func TestCanPermissionMatrix(t *testing.T) {
	const (
		repoA   = "libs-release"
		repoB   = "other-repo"
		inPath  = "ci-out/y.bin"      // covered by the include pattern
		outPath = "elsewhere/z.bin"   // outside the include pattern
		exPath  = "ci-out/secret.key" // hit by the exclude pattern
	)

	setup := func(t *testing.T) *fixture {
		t.Helper()
		f := newFixture(t, true)
		f.putTarget("ci-out-rw", []string{repoA},
			[]string{"ci-out/**"}, []string{"ci-out/secret.key"},
			"ci-bot", true /*read*/, true /*write*/, false /*delete*/)
		return f
	}

	admin := &auth.Principal{Name: "admin", Admin: true}
	ci := &auth.Principal{Name: "ci-bot"}
	other := &auth.Principal{Name: otherUser}

	tests := []struct {
		name string
		p    *auth.Principal
		repo string
		path string
		act  string
		want bool
	}{
		// admin passes everything, everywhere
		{"admin read anywhere", admin, repoA, outPath, auth.ActionRead, true},
		{"admin write anywhere", admin, repoA, outPath, auth.ActionWrite, true},
		{"admin delete anywhere", admin, repoA, exPath, auth.ActionDelete, true},
		{"admin other repo", admin, repoB, "x", auth.ActionDelete, true},

		// authorized principal inside the granted path
		{"ci read in scope", ci, repoA, inPath, auth.ActionRead, true},
		{"ci write in scope", ci, repoA, inPath, auth.ActionWrite, true},
		{"ci delete denied (no d)", ci, repoA, inPath, auth.ActionDelete, false},
		// exclude beats include, for every granted action
		{"ci read on excluded path", ci, repoA, exPath, auth.ActionRead, false},
		{"ci write on excluded path", ci, repoA, exPath, auth.ActionWrite, false},
		// outside the include pattern
		{"ci read out of scope", ci, repoA, outPath, auth.ActionRead, false},
		{"ci write out of scope", ci, repoA, outPath, auth.ActionWrite, false},
		// different repo listed nowhere
		{"ci other repo", ci, repoB, inPath, auth.ActionRead, false},

		// unauthorized principal
		{"other read", other, repoA, inPath, auth.ActionRead, false},
		{"other write", other, repoA, inPath, auth.ActionWrite, false},
		{"other delete", other, repoA, inPath, auth.ActionDelete, false},

		// anonymous with the flag ON (fixture): read-only
		{"anon read flag-on", nil, repoA, inPath, auth.ActionRead, true},
		{"anon write flag-on", nil, repoA, inPath, auth.ActionWrite, false},
		{"anon delete flag-on", nil, repoA, inPath, auth.ActionDelete, false},

		// unknown action denies
		{"ci bogus action", ci, repoA, inPath, "x", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := setup(t)
			if got := f.svc.Can(f.ctx, tt.p, tt.repo, tt.path, tt.act); got != tt.want {
				t.Fatalf("Can(%+v, %s, %s, %s) = %v, want %v",
					tt.p, tt.repo, tt.path, tt.act, got, tt.want)
			}
		})
	}
}

// The anonymous flag off flips only the anonymous-read row; everything
// else behaves identically (FR-5-AC13: with anonymous_access=false the
// read permission of real users becomes observable).
func TestCanAnonymousFlagOff(t *testing.T) {
	f := newFixture(t, false)
	f.putTarget("t", []string{"r"}, []string{"ci/**"}, nil, "ci-bot", true, true, false)

	ci := &auth.Principal{Name: "ci-bot"}
	other := &auth.Principal{Name: otherUser}

	if f.svc.Can(f.ctx, nil, "r", "ci/a.bin", auth.ActionRead) {
		t.Fatal("anonymous read must be denied when the flag is off")
	}
	if f.svc.Can(f.ctx, nil, "r", "ci/a.bin", auth.ActionWrite) {
		t.Fatal("anonymous write is always denied")
	}
	if !f.svc.Can(f.ctx, ci, "r", "ci/a.bin", auth.ActionRead) {
		t.Fatal("granted user read survives the flag change")
	}
	if f.svc.Can(f.ctx, other, "r", "ci/a.bin", auth.ActionRead) {
		t.Fatal("ungranted user read stays denied (read differentiation becomes observable)")
	}
}

// Pattern semantics through Can: empty includes = everything; exclude
// priority; multiple targets union; multiple principals.
func TestCanPatternSemantics(t *testing.T) {
	t.Run("empty includes means everything", func(t *testing.T) {
		f := newFixture(t, false)
		f.putTarget("wide", []string{"r"}, nil, nil, "ci-bot", true, true, true)
		ci := &auth.Principal{Name: "ci-bot"}
		for _, p := range []string{"a.bin", "deep/x/y.bin"} {
			for _, a := range []string{auth.ActionRead, auth.ActionWrite, auth.ActionDelete} {
				if !f.svc.Can(f.ctx, ci, "r", p, a) {
					t.Fatalf("empty includes must grant %s on %s", a, p)
				}
			}
		}
	})

	t.Run("exclude scopes its own target only", func(t *testing.T) {
		f := newFixture(t, false)
		// Two targets for the same user: one grants everything, another
		// carves an exclude out of its own scope. Permission targets union:
		// a path excluded in one target but included in another is allowed
		// by the other (auth-model.md section 4: "excludes take priority
		// over includes" governs pattern evaluation inside one target).
		f.putTarget("allow", []string{"r"}, []string{"**"}, nil, "ci-bot", true, true, true)
		f.putTarget("deny-corner", []string{"r"}, []string{"**"}, []string{"priv/**"}, "ci-bot", true, true, true)
		ci := &auth.Principal{Name: "ci-bot"}
		if !f.svc.Can(f.ctx, ci, "r", "priv/k.bin", auth.ActionRead) {
			t.Fatal("the unrestricted target still grants the excluded path (targets union)")
		}
		if !f.svc.Can(f.ctx, ci, "r", "open/k.bin", auth.ActionRead) {
			t.Fatal("non-excluded path must stay allowed")
		}
	})

	t.Run("exclude wins inside one target", func(t *testing.T) {
		f := newFixture(t, false)
		f.putTarget("carved", []string{"r"}, []string{"**"}, []string{"priv/**"}, "ci-bot", true, true, true)
		ci := &auth.Principal{Name: "ci-bot"}
		if f.svc.Can(f.ctx, ci, "r", "priv/k.bin", auth.ActionRead) {
			t.Fatal("exclude hit must deny inside its own target")
		}
		if !f.svc.Can(f.ctx, ci, "r", "open/k.bin", auth.ActionRead) {
			t.Fatal("include hit without exclude must allow")
		}
	})

	t.Run("repo scoping", func(t *testing.T) {
		f := newFixture(t, false)
		f.putTarget("one-repo", []string{"r1"}, []string{"**"}, nil, "ci-bot", true, true, true)
		ci := &auth.Principal{Name: "ci-bot"}
		if !f.svc.Can(f.ctx, ci, "r1", "a", auth.ActionRead) {
			t.Fatal("listed repo grants")
		}
		if f.svc.Can(f.ctx, ci, "r2", "a", auth.ActionRead) {
			t.Fatal("unlisted repo denies")
		}
	})

	t.Run("username case-insensitive", func(t *testing.T) {
		f := newFixture(t, false)
		f.putTarget("ci", []string{"r"}, []string{"**"}, nil, "ci-bot", true, false, false)
		if !f.svc.Can(f.ctx, &auth.Principal{Name: "CI-BOT"}, "r", "a", auth.ActionRead) {
			t.Fatal("principal comparison must ignore case (Artifactory lowercases usernames)")
		}
	})

	t.Run("groups ignored in M1", func(t *testing.T) {
		f := newFixture(t, false)
		// A group-typed principal row for a name must not authorize a user
		// of that name (M1 has no groups; non-user rows are skipped rather
		// than misapplied).
		if err := f.st.Permissions().PutTarget(f.ctx,
			targetOf("grp", []string{"r"}, []string{"**"}, nil),
			groupRow("grp", "ci-bot")); err != nil {
			t.Fatalf("put group row: %v", err)
		}
		if f.svc.Can(f.ctx, &auth.Principal{Name: "ci-bot"}, "r", "a", auth.ActionRead) {
			t.Fatal("group-typed row must not authorize a user principal in M1")
		}
	})
}
