// T-491 acceptance surface (M17 W2, FR-156.2 / M16 B-2.16; ADR-0046
// point 3 + Errata ④ E8): the preset wildcard buckets of the permission
// plane — ANY LOCAL / ANY REMOTE class coverage (new repositories included
// the moment they exist), the ANY DISTRIBUTION pseudo-key channel, the
// zero-verb-expansion invariant of the five-column matrix, the manage
// plane's literal coverage, and the fail-closed postures (inert without a
// class source; the bucket-row read failing denies).

package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// seedRepoRow plants one repositories-table row straight through the store
// (the classifier reads exactly this table; the repo service's own
// validation is irrelevant to the class answer).
func seedRepoRow(t *testing.T, f *fixture, key, class string) {
	t.Helper()
	now := metadata.Now()
	if err := f.st.Repos().Create(f.ctx, &metadata.Repo{
		RepoKey: key, Type: class, PackageType: "generic",
		Config: "{}", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
}

// putBucketTarget installs one target whose repos[] carries the bucket
// literal, with the FULL five-bit principal row (the fixture's putTarget
// predates m/a).
func putBucketTarget(t *testing.T, f *fixture, name string, repos []string, principal string, bits auth.PrincipalBits) {
	t.Helper()
	if err := f.st.Permissions().PutTarget(f.ctx,
		targetOf(name, repos, nil, nil),
		[]*metadata.PermissionPrincipal{{
			TargetName: name, Principal: principal, PrincipalType: "user",
			CanRead: bits.Read, CanWrite: bits.Write, CanDelete: bits.Delete,
			CanManage: bits.Manage, CanAnnotate: bits.Annotate,
		}}); err != nil {
		t.Fatalf("put bucket target %q: %v", name, err)
	}
}

// TestWildcardBucketClassCoverage: ANY LOCAL covers every local repository
// — including one created AFTER the grant (the wildcard-effectiveness
// probe) — and nothing else: remote and virtual repositories stay outside,
// ungranted principals stay denied, and pattern semantics ride unchanged
// (excludes carve out of the bucket target exactly as out of an exact one).
func TestWildcardBucketClassCoverage(t *testing.T) {
	f := newFixture(t, false)
	seedRepoRow(t, f, "libs-local", "local")
	seedRepoRow(t, f, "hub-remote", "remote")
	seedRepoRow(t, f, "agg-virtual", "virtual")

	putBucketTarget(t, f, "any-local-read", []string{auth.BucketAnyLocal},
		"ci-bot", auth.PrincipalBits{Read: true})

	ci := &auth.Principal{Name: "ci-bot"}
	other := &auth.Principal{Name: otherUser}

	// The grant covers the existing local repository, read only.
	if !f.svc.Can(f.ctx, ci, "libs-local", "a/b.bin", auth.ActionRead) {
		t.Fatal("ANY LOCAL read grant must cover an existing local repository")
	}
	if f.svc.Can(f.ctx, ci, "libs-local", "a/b.bin", auth.ActionWrite) {
		t.Fatal("read-only grant must not write (zero verb expansion)")
	}

	// A repository that does not exist: no class answer, no bucket — the
	// wildcard never covers a key it cannot classify.
	if f.svc.Can(f.ctx, ci, "never-created", "a.bin", auth.ActionRead) {
		t.Fatal("a key with no repository row must stay uncovered")
	}

	// A local repository created AFTER the grant joins automatically — the
	// class is resolved per evaluation, no re-grant.
	seedRepoRow(t, f, "late-local", "local")
	if !f.svc.Can(f.ctx, ci, "late-local", "x.bin", auth.ActionRead) {
		t.Fatal("a newly created local repository must be covered by ANY LOCAL immediately")
	}

	// Class scoping: the remote and virtual repositories are NOT covered.
	if f.svc.Can(f.ctx, ci, "hub-remote", "a.bin", auth.ActionRead) {
		t.Fatal("ANY LOCAL must not cover a remote repository")
	}
	if f.svc.Can(f.ctx, ci, "agg-virtual", "a.bin", auth.ActionRead) {
		t.Fatal("ANY LOCAL must not cover a virtual repository (no preset names the class)")
	}

	// Ungranted principals stay denied everywhere the bucket covers.
	for _, repo := range []string{"libs-local", "late-local"} {
		if f.svc.Can(f.ctx, other, repo, "a.bin", auth.ActionRead) {
			t.Fatalf("ungranted principal must stay denied on %s", repo)
		}
	}
}

// TestWildcardBucketRemoteMirror: ANY REMOTE is the same rule for the
// remote class — and the two buckets do not bleed into each other.
func TestWildcardBucketRemoteMirror(t *testing.T) {
	f := newFixture(t, false)
	seedRepoRow(t, f, "libs-local", "local")
	seedRepoRow(t, f, "hub-remote", "remote")

	putBucketTarget(t, f, "any-remote-read", []string{auth.BucketAnyRemote},
		"ci-bot", auth.PrincipalBits{Read: true})

	ci := &auth.Principal{Name: "ci-bot"}
	if !f.svc.Can(f.ctx, ci, "hub-remote", "npm/lodash/-/lodash-4.0.0.tgz", auth.ActionRead) {
		t.Fatal("ANY REMOTE read grant must cover the remote repository")
	}
	if f.svc.Can(f.ctx, ci, "libs-local", "a.bin", auth.ActionRead) {
		t.Fatal("ANY REMOTE must not cover a local repository")
	}
}

// TestWildcardBucketPatternSemantics: include/exclude patterns apply to a
// bucket target exactly as to an exact-key one — the bucket widens only
// the repos listing, never the path plane.
func TestWildcardBucketPatternSemantics(t *testing.T) {
	f := newFixture(t, false)
	seedRepoRow(t, f, "libs-local", "local")

	// includes/excludes ride through targetOf; nil includes = everything.
	if err := f.st.Permissions().PutTarget(f.ctx,
		targetOf("any-local-pattern", []string{auth.BucketAnyLocal}, []string{"ci/**"}, []string{"ci/secret.key"}),
		[]*metadata.PermissionPrincipal{{
			TargetName: "any-local-pattern", Principal: "ci-bot", PrincipalType: "user",
			CanRead: true, CanWrite: true, CanDelete: true,
		}}); err != nil {
		t.Fatalf("put pattern bucket target: %v", err)
	}
	ci := &auth.Principal{Name: "ci-bot"}
	if !f.svc.Can(f.ctx, ci, "libs-local", "ci/out.bin", auth.ActionRead) {
		t.Fatal("include hit under the bucket must allow")
	}
	if f.svc.Can(f.ctx, ci, "libs-local", "ci/secret.key", auth.ActionRead) {
		t.Fatal("exclude hit under the bucket must deny")
	}
	if f.svc.Can(f.ctx, ci, "libs-local", "elsewhere/x.bin", auth.ActionRead) {
		t.Fatal("include miss under the bucket must deny")
	}
}

// TestWildcardBucketVerbZeroExpansion: AC2's probe matrix. Five grants,
// each carrying exactly ONE verb through ANY LOCAL — every granted verb
// answers true on a local repository and the other four answer false. The
// bucket widens repo coverage only; the five columns stay independent (and
// m implies none of r/w/d/a re-pins ADR-0026 decision 3 under the bucket).
func TestWildcardBucketVerbZeroExpansion(t *testing.T) {
	f := newFixture(t, false)
	seedRepoRow(t, f, "libs-local", "local")

	verbs := []struct {
		name string
		act  string
		bits auth.PrincipalBits
		user string
	}{
		{"read", auth.ActionRead, auth.PrincipalBits{Read: true}, "v-read"},
		{"write", auth.ActionWrite, auth.PrincipalBits{Write: true}, "v-write"},
		{"delete", auth.ActionDelete, auth.PrincipalBits{Delete: true}, "v-delete"},
		{"manage", auth.ActionManage, auth.PrincipalBits{Manage: true}, "v-manage"},
		{"annotate", auth.ActionAnnotate, auth.PrincipalBits{Annotate: true}, "v-annotate"},
	}
	for _, v := range verbs {
		f.createUser(v.user, v.user+"-pw", false)
		putBucketTarget(t, f, "only-"+v.name, []string{auth.BucketAnyLocal}, v.user, v.bits)
	}

	for _, granted := range verbs {
		p := &auth.Principal{Name: granted.user}
		for _, probe := range verbs {
			want := granted.name == probe.name
			if got := f.svc.Can(f.ctx, p, "libs-local", "a.bin", probe.act); got != want {
				t.Fatalf("grant of only %s: Can(%s) = %v, want %v (zero verb expansion violated)",
					granted.name, probe.name, got, want)
			}
		}
	}
}

// TestWildcardBucketDistributionPseudoKey: the ANY DISTRIBUTION channel of
// ADR-0046 point 3 — callers pass the bucket literal itself in Can's
// repoKey position, path carries the bundle name, and evaluation is
// EXACT: the pseudo-key is granted only by a target that names it, the
// ANY family never implicitly covers the bundle domain (E8), and patterns
// apply by bundle name.
func TestWildcardBucketDistributionPseudoKey(t *testing.T) {
	f := newFixture(t, false)
	// A local repository exists and a local-bucket grant is live: neither
	// may reach the pseudo-key.
	seedRepoRow(t, f, "libs-local", "local")
	putBucketTarget(t, f, "any-local-read", []string{auth.BucketAnyLocal},
		"ci-bot", auth.PrincipalBits{Read: true})

	// The distribution grant: read on bundles whose name matches rel-*.
	if err := f.st.Permissions().PutTarget(f.ctx,
		targetOf("dist-release", []string{auth.BucketAnyDistribution}, []string{"rel-*"}, nil),
		[]*metadata.PermissionPrincipal{{
			TargetName: "dist-release", Principal: "ci-bot", PrincipalType: "user",
			CanRead: true,
		}}); err != nil {
		t.Fatalf("put distribution target: %v", err)
	}

	ci := &auth.Principal{Name: "ci-bot"}
	other := &auth.Principal{Name: otherUser}

	if !f.svc.Can(f.ctx, ci, auth.BucketAnyDistribution, "rel-2026.9", auth.ActionRead) {
		t.Fatal("the pseudo-key channel must answer read for a bundle name the target covers")
	}
	if f.svc.Can(f.ctx, ci, auth.BucketAnyDistribution, "prod-9", auth.ActionRead) {
		t.Fatal("include patterns must apply on the bundle-name path plane")
	}
	if f.svc.Can(f.ctx, ci, auth.BucketAnyDistribution, "rel-2026.9", auth.ActionWrite) {
		t.Fatal("read-only distribution grant must not write (zero verb expansion)")
	}
	if f.svc.Can(f.ctx, other, auth.BucketAnyDistribution, "rel-2026.9", auth.ActionRead) {
		t.Fatal("ungranted principal must stay denied on the pseudo-key")
	}

	// E8: the local bucket grant must NOT implicitly cover the bundle
	// domain — grant both buckets to separate users on one target to also
	// prove the co-existence (naming both literal entries in one repos[]).
	if f.svc.Can(f.ctx, &auth.Principal{Name: "v-read"}, auth.BucketAnyDistribution, "rel-2026.9", auth.ActionRead) {
		t.Fatal("ANY LOCAL must not implicitly grant the ANY DISTRIBUTION pseudo-key")
	}
	if err := f.st.Permissions().PutTarget(f.ctx,
		targetOf("both-buckets", []string{auth.BucketAnyLocal, auth.BucketAnyDistribution}, nil, nil),
		[]*metadata.PermissionPrincipal{{
			TargetName: "both-buckets", Principal: "other", PrincipalType: "user",
			CanRead: true,
		}}); err != nil {
		t.Fatalf("put both-buckets target: %v", err)
	}
	if !f.svc.Can(f.ctx, other, auth.BucketAnyDistribution, "any-bundle", auth.ActionRead) {
		t.Fatal("a target naming BOTH literals must grant the pseudo-key through its ANY DISTRIBUTION entry")
	}
	if !f.svc.Can(f.ctx, other, "libs-local", "a.bin", auth.ActionRead) {
		t.Fatal("the same target's ANY LOCAL entry must keep covering local repositories")
	}
}

// TestWildcardBucketManagePlane: a manage-only grant through ANY LOCAL is
// repo-level admin on every local repository (present and future), implies
// no content verb, and ManageCoverage carries the bucket LITERAL — the
// documented non-expansion (rbac.go's T-491 note).
func TestWildcardBucketManagePlane(t *testing.T) {
	f := newFixture(t, false)
	seedRepoRow(t, f, "libs-local", "local")
	putBucketTarget(t, f, "any-local-manage", []string{auth.BucketAnyLocal},
		"ci-bot", auth.PrincipalBits{Manage: true})

	ci := &auth.Principal{Name: "ci-bot"}

	// m holds on the existing repo and on one created later; r/w/d/a do not.
	if !f.svc.Can(f.ctx, ci, "libs-local", "", auth.ActionManage) {
		t.Fatal("ANY LOCAL manage grant must hold on the local repository")
	}
	seedRepoRow(t, f, "late-local", "local")
	if !f.svc.Can(f.ctx, ci, "late-local", "", auth.ActionManage) {
		t.Fatal("a newly created local repository joins the manage coverage immediately")
	}
	for _, act := range []string{auth.ActionRead, auth.ActionWrite, auth.ActionDelete, auth.ActionAnnotate} {
		if f.svc.Can(f.ctx, ci, "libs-local", "a.bin", act) {
			t.Fatalf("manage-only grant must not carry %s (no privilege chain)", act)
		}
	}

	// The single-repo management facet rides Can's m answer.
	if !f.svc.CanManageRepo(f.ctx, ci, "libs-local", true) {
		t.Fatal("CanManageRepo write arm must pass for the wildcard manage holder")
	}

	// Coverage keeps the literal, never the expansion.
	coverage, universe := f.svc.ManageCoverage(f.ctx, ci)
	if universe {
		t.Fatal("a plain user's coverage must not be the universe sentinel")
	}
	if _, ok := coverage[auth.BucketAnyLocal]; !ok {
		t.Fatal("the bucket literal must appear in the manage coverage set")
	}
	for _, key := range []string{"libs-local", "late-local"} {
		if _, ok := coverage[key]; ok {
			t.Fatalf("coverage must stay literal (no expansion to %s)", key)
		}
	}

	// The ?permissions view agrees: the holder shows with Manage only.
	users, groups, err := f.svc.ItemPrincipals(f.ctx, "libs-local", "a.bin")
	if err != nil {
		t.Fatalf("ItemPrincipals: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("no group principals expected, got %v", groups)
	}
	bits, ok := users["ci-bot"]
	if !ok {
		t.Fatal("the wildcard manage holder must appear in the ?permissions view")
	}
	if !bits.Manage || bits.Read || bits.Write || bits.Delete || bits.Annotate {
		t.Fatalf("view bits = %+v, want Manage only (view agrees with Can)", bits)
	}
}

// TestWildcardBucketInertWithoutClassifier: a service with no repository
// class source keeps the pre-T-491 exact-only behavior — the bucket
// literal matches only as an exact repoKey (the pseudo-key symmetry), and
// real repositories are covered only by exact listings.
func TestWildcardBucketInertWithoutClassifier(t *testing.T) {
	f := newFixture(t, false)
	seedRepoRow(t, f, "libs-local", "local")
	putBucketTarget(t, f, "any-local-read", []string{auth.BucketAnyLocal},
		"ci-bot", auth.PrincipalBits{Read: true})
	putBucketTarget(t, f, "exact-read", []string{"libs-local"},
		"other", auth.PrincipalBits{Read: true})

	inert := f.svc.WithRepoClass(nil)

	// The bucket grant degrades to exact-only: no local repository is
	// covered by it...
	if inert.Can(f.ctx, &auth.Principal{Name: "ci-bot"}, "libs-local", "a.bin", auth.ActionRead) {
		t.Fatal("without a class source the bucket must stay inert (exact-only)")
	}
	// ...but the literal as repoKey still matches exactly (the pseudo-key
	// arm needs no classifier by design).
	if !inert.Can(f.ctx, &auth.Principal{Name: "ci-bot"}, auth.BucketAnyLocal, "a.bin", auth.ActionRead) {
		t.Fatal("the bucket literal as repoKey must keep exact-match semantics")
	}
	// Exact grants are untouched.
	if !inert.Can(f.ctx, &auth.Principal{Name: "other"}, "libs-local", "a.bin", auth.ActionRead) {
		t.Fatal("exact-key grants must keep working with the classifier unwired")
	}
}

// bucketFailingPerms answers the repo-key read healthily and fails only
// the bucket read — the partial-data posture: Can must deny, never answer
// from half the grant tables.
type bucketFailingPerms struct {
	repoKey string
	failKey string
}

func (p bucketFailingPerms) ListTargets(context.Context) ([]auth.Target, error) {
	return []auth.Target{{
		Name: "t", Repos: `["` + p.repoKey + `","` + p.failKey + `"]`,
		Includes: `["**"]`, Excludes: `[]`,
	}}, nil
}

func (p bucketFailingPerms) PrincipalsFor(_ context.Context, repoKey string) ([]auth.PermissionRow, error) {
	if repoKey == p.failKey {
		return nil, errors.New("boom: bucket query failed")
	}
	return []auth.PermissionRow{{
		TargetName: "t", Principal: "ci-bot", PrincipalType: "user", CanRead: true,
	}}, nil
}

// TestWildcardBucketFailsClosedOnBucketRead: with a live exact grant in
// hand, the bucket-row read failing denies the whole evaluation — the same
// fail-closed posture as the first PrincipalsFor (a broken permission
// table must never open access, and partial data is not a subset of truth).
func TestWildcardBucketFailsClosedOnBucketRead(t *testing.T) {
	f := newFixture(t, false)
	seedRepoRow(t, f, "libs-local", "local")
	svc := f.svc.WithPermissions(bucketFailingPerms{repoKey: "libs-local", failKey: auth.BucketAnyLocal})

	if svc.Can(f.ctx, &auth.Principal{Name: "ci-bot"}, "libs-local", "a.bin", auth.ActionRead) {
		t.Fatal("Can granted despite the bucket-row read failing; must deny (fail closed)")
	}
}
