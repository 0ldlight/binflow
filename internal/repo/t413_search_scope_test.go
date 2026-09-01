package repo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// T-413 AC1 (the repo side of the ACL weave): SearchScope classifies the
// caller's readable repository set from the SAME permission tables auth.Can
// evaluates, and CanRead is the public projection of the content-plane read
// decision. The engine-level zero-leak leg over this seam lives in
// internal/search/engine_test.go; this file pins the classification matrix
// and the same-source invariants.

// scopeEnv builds the classification playground: two local repositories
// (open for alice through a pattern-free target, path-scoped through an
// includes target), one remote, one virtual, plus a group-granted local.
func scopeEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	ctx := context.Background()
	create := func(key, typ string) {
		t.Helper()
		if err := e.md.Repos().Create(ctx, &metadata.Repo{
			RepoKey: key, Type: typ, PackageType: repo.PackageGeneric, Config: "{}",
			CreatedAt: "2026-09-01T00:00:00Z", UpdatedAt: "2026-09-01T00:00:00Z",
		}); err != nil {
			t.Fatalf("create repo %s: %v", key, err)
		}
	}
	create("local-a", repo.TypeLocal)
	create("local-b", repo.TypeLocal)
	create("local-c", repo.TypeLocal)
	create("remote-cache", repo.TypeRemote)
	create("virtual-x", repo.TypeVirtual)

	putTarget := func(name, repos, includes string, principals ...*metadata.PermissionPrincipal) {
		t.Helper()
		if err := e.md.Permissions().PutTarget(ctx, &metadata.PermissionTarget{
			Name: name, Repos: repos, Includes: includes, Excludes: "[]",
		}, principals); err != nil {
			t.Fatalf("put target %s: %v", name, err)
		}
	}
	row := func(target, principal, typ string) *metadata.PermissionPrincipal {
		return &metadata.PermissionPrincipal{TargetName: target, Principal: principal,
			PrincipalType: typ, CanRead: true}
	}
	putTarget("t-open", `["local-a"]`, "[]", row("t-open", "alice", "user"))
	putTarget("t-path", `["local-b"]`, `["pub/**"]`, row("t-path", "alice", "user"))
	putTarget("t-group", `["local-c"]`, "[]", row("t-group", "devs", "group"))
	putTarget("t-write-only", `["remote-cache"]`, "[]",
		&metadata.PermissionPrincipal{TargetName: "t-write-only", Principal: "alice",
			PrincipalType: "user", CanRead: false, CanWrite: true})
	return e
}

// scopeKeys renders a scope for assertion.
func scopeKeys(t *testing.T, scope []repo.ReadScope) map[string]bool {
	t.Helper()
	out := make(map[string]bool, len(scope))
	for _, s := range scope {
		out[s.Repo+"|"+boolStr(s.PathScoped)] = true
	}
	return out
}

func boolStr(b bool) string {
	if b {
		return "scoped"
	}
	return "open"
}

// TestSearchScopeMatrix pins the classification table: role universes,
// pattern-free vs pattern-carrying targets, the group arm, the write-only
// non-grant, virtual exclusion and the anonymous gate.
func TestSearchScopeMatrix(t *testing.T) {
	e := scopeEnv(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		p       *repo.Principal
		wantErr error
		want    map[string]bool
	}{
		{
			name: "admin reads every local and remote repository unrestricted",
			p:    admin(),
			want: map[string]bool{
				"local-a|open": true, "local-b|open": true, "local-c|open": true,
				"remote-cache|open": true,
			},
		},
		{
			name: "readonly admin reads globally without target rows",
			p:    &repo.Principal{Name: "watcher", Role: auth.RoleReadOnlyAdmin},
			want: map[string]bool{
				"local-a|open": true, "local-b|open": true, "local-c|open": true,
				"remote-cache|open": true,
			},
		},
		{
			name: "pattern-free target classifies open, includes target path-scoped",
			p:    &repo.Principal{Name: "alice", Groups: []string{}},
			want: map[string]bool{"local-a|open": true, "local-b|scoped": true},
		},
		{
			name: "group rows grant through the SE-07 union",
			p:    &repo.Principal{Name: "dave", Groups: []string{"devs"}},
			want: map[string]bool{"local-c|open": true},
		},
		{
			name: "zero-grant user gets the empty scope, not an error",
			p:    &repo.Principal{Name: "bob"},
			want: map[string]bool{},
		},
		{
			name:    "anonymous on a closed instance meets the search gate",
			p:       nil,
			wantErr: repo.ErrForbidden,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope, err := e.svc.SearchScope(ctx, tt.p)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("SearchScope err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("SearchScope: %v", err)
			}
			got := scopeKeys(t, scope)
			if len(got) != len(tt.want) {
				t.Fatalf("SearchScope = %v, want exactly %v", got, tt.want)
			}
			for k := range tt.want {
				if !got[k] {
					t.Fatalf("SearchScope = %v, missing %q (want %v)", got, k, tt.want)
				}
			}
		})
	}

	// The virtual repository never enters any scope, admin included —
	// asserted by the want tables above (virtual-x absent everywhere).
	// A write-only row grants no read: remote-cache is absent for alice
	// (the t-write-only arm) and present for no one but the role universes.
}

// TestSearchScopeMatchesAuth pins the same-source property (ADR-0043 pt 4):
// with the REAL auth.Service wired as the authorizer over the same store,
// every repository SearchScope classifies open is readable at every path
// auth.Can can name, and every path-scoped repository has both readable and
// refused paths under the same decision — the restatement in
// scopeTargetReads cannot have drifted from auth's rowCoversPrincipal.
func TestSearchScopeMatchesAuth(t *testing.T) {
	e := scopeEnv(t)
	ctx := context.Background()

	put(t, e, admin(), "local-a", "any/where.bin", "a")
	put(t, e, admin(), "local-b", "pub/inside.bin", "b")
	put(t, e, admin(), "local-b", "priv/hidden.bin", "b2")
	put(t, e, admin(), "local-c", "dev/asset.bin", "c")

	realAz := auth.NewFromStore(e.md, false)
	svc := newServiceWithClock(e.st, e.md, realAz, nil, func() time.Time { return time.Now().UTC() })

	aliceP := &repo.Principal{Name: "ALICE"} // case-folded on purpose: the
	// user row is lowercase; auth matches names case-insensitively and the
	// restated rule must too.
	scope, err := svc.SearchScope(ctx, aliceP)
	if err != nil {
		t.Fatalf("SearchScope: %v", err)
	}
	for _, s := range scope {
		switch s.Repo {
		case "local-a":
			if s.PathScoped {
				t.Fatalf("local-a classified PathScoped, want open")
			}
			// An open repository must be readable at EVERY path — the
			// classification's core promise (no row check can add
			// information).
			for _, path := range []string{"any/where.bin", "deeper/x/y.bin", "root.bin"} {
				if !svc.CanRead(ctx, aliceP, "local-a", path) {
					t.Fatalf("open repo local-a refused at %q — classification drifted from allow()", path)
				}
			}
		case "local-b":
			if !s.PathScoped {
				t.Fatalf("local-b classified open, want PathScoped")
			}
			if !svc.CanRead(ctx, aliceP, "local-b", "pub/inside.bin") {
				t.Fatalf("local-b pub/ refused — CanRead and the includes pattern disagree")
			}
			if svc.CanRead(ctx, aliceP, "local-b", "priv/hidden.bin") {
				t.Fatalf("local-b priv/ accepted — CanRead leaked past the includes pattern")
			}
		default:
			t.Fatalf("unexpected repo %q in alice's scope", s.Repo)
		}
	}
	// The zero-grant arm and the write-only arm hold under the real
	// authorizer too: CanRead must refuse what the scope left out.
	if svc.CanRead(ctx, &repo.Principal{Name: "bob"}, "local-a", "any/where.bin") {
		t.Fatalf("bob reads local-a through CanRead but holds no scope row")
	}
	if svc.CanRead(ctx, aliceP, "remote-cache", "cached/path.bin") {
		t.Fatalf("alice reads remote-cache with a write-only row — read bit leaked")
	}
}
