package repo_test

// T-511 (the T-491 leave-behind it inherited): SearchScope expands the
// wildcard bucket literals of a target's repos[] onto the repository
// population they name — the same class coverage repoListed evaluates per
// key, so the AQL one-shot scope and the per-row Can decision cannot
// disagree. Before the fix the literal matched no real repository key and
// an ANY LOCAL read grant left the user's AQL scope empty (under-report,
// fail-closed direction).

import (
	"context"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// bucketEnv seeds the class playground: two locals, one remote, one
// virtual.
func bucketEnv(t *testing.T) *env {
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
	create("loc-a", repo.TypeLocal)
	create("loc-b", repo.TypeLocal)
	create("rem-a", repo.TypeRemote)
	create("virt-a", repo.TypeVirtual)
	return e
}

// putBucketTarget grants p read through one repos[] listing.
func putBucketTarget(t *testing.T, e *env, name, repos, principal string) {
	t.Helper()
	if err := e.md.Permissions().PutTarget(context.Background(), &metadata.PermissionTarget{
		Name: name, Repos: repos, Includes: "[]", Excludes: "[]",
	}, []*metadata.PermissionPrincipal{{
		TargetName: name, Principal: principal, PrincipalType: "user", CanRead: true,
	}}); err != nil {
		t.Fatalf("put target %s: %v", name, err)
	}
}

// TestSearchScopeExpandsWildcardBuckets is the fix's pin: ANY LOCAL covers
// every local repository, ANY REMOTE every remote, virtual stays out of
// both, and ANY DISTRIBUTION contributes no repository row (the bundle
// pseudo-key channel).
func TestSearchScopeExpandsWildcardBuckets(t *testing.T) {
	e := bucketEnv(t)
	ctx := context.Background()

	tests := []struct {
		name      string
		repos     string
		principal string // distinct per case: grants union across targets
		want      map[string]bool
	}{
		{
			name:      "ANY LOCAL covers both locals, open (pattern-free target)",
			repos:     `["` + auth.BucketAnyLocal + `"]`,
			principal: "alice",
			want:      map[string]bool{"loc-a|open": true, "loc-b|open": true},
		},
		{
			name:      "ANY REMOTE covers the remote only",
			repos:     `["` + auth.BucketAnyRemote + `"]`,
			principal: "bob",
			want:      map[string]bool{"rem-a|open": true},
		},
		{
			name:      "ANY DISTRIBUTION names no repository",
			repos:     `["` + auth.BucketAnyDistribution + `"]`,
			principal: "carol",
			want:      map[string]bool{},
		},
		{
			name:      "bucket and exact key disjoin in one target",
			repos:     `["loc-a", "` + auth.BucketAnyRemote + `"]`,
			principal: "dave",
			want:      map[string]bool{"loc-a|open": true, "rem-a|open": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			putBucketTarget(t, e, "t-"+tt.name, tt.repos, tt.principal)
			scope, err := e.svc.SearchScope(ctx, &repo.Principal{Name: tt.principal})
			if err != nil {
				t.Fatalf("SearchScope: %v", err)
			}
			got := scopeKeys(t, scope)
			if len(got) != len(tt.want) {
				t.Fatalf("SearchScope = %v, want exactly %v", got, tt.want)
			}
			for k := range tt.want {
				if !got[k] {
					t.Fatalf("SearchScope = %v, missing %q", got, k)
				}
			}
		})
	}
}

// TestSearchScopeBucketMatchesCan pins the same-source property on the
// bucket arm: every repository the expansion put in scope is readable at
// every path through the REAL auth.Service's Can (the per-row second
// stage) — the expansion cannot outrun (or trail) the decision plane.
func TestSearchScopeBucketMatchesCan(t *testing.T) {
	e := bucketEnv(t)
	ctx := context.Background()
	putBucketTarget(t, e, "t-any-local", `["`+auth.BucketAnyLocal+`"]`, "alice")

	realAz := auth.NewFromStore(e.md, false)
	svc := newServiceWithClock(e.st, e.md, realAz, nil, func() time.Time { return time.Now().UTC() })
	aliceP := &repo.Principal{Name: "alice"}
	scope, err := svc.SearchScope(ctx, aliceP)
	if err != nil {
		t.Fatalf("SearchScope: %v", err)
	}
	if len(scope) != 2 {
		t.Fatalf("scope = %v, want both locals", scope)
	}
	for _, s := range scope {
		for _, path := range []string{"root.bin", "deep/nested/x.bin"} {
			if !svc.CanRead(ctx, aliceP, s.Repo, path) {
				t.Fatalf("scope repo %s refused at %q — bucket expansion drifted from Can", s.Repo, path)
			}
		}
	}
	// And the repositories NO bucket names stay refused.
	if svc.CanRead(ctx, aliceP, "rem-a", "x.bin") || svc.CanRead(ctx, aliceP, "virt-a", "x.bin") {
		t.Fatal("ANY LOCAL leaked onto the remote or the virtual repository")
	}
}
