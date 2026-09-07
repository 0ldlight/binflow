// The build ACL face's probes (M17 T-507, FR-152.1 / ADR-0045 decision 4,
// NFR-S80): allow() same-source with the path position carrying the build
// NAME, the three unauthorized arms (query 403, detail zero-appearance,
// list zero-leak), the role arms (admin bypass, readonly_admin's r-always,
// nil-authorizer fail-closed, anonymous denial) and the wildcard-bucket
// exclusion (the ANY family never implicitly covers the build domain).

package build_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/build"
	"github.com/lzwzzy/binflow/internal/metadata"
)

const (
	probeStampA = "2026-09-07T10:00:00.000+0000"
	probeStampB = "2026-09-07T11:30:00.000+0000"
)

// probeWorld is the full same-source assembly: one metadata store feeds
// BOTH the real auth.Service and the build.Service over it — the decision
// and the data share one source, which is what every probe below asserts
// against (targets written to the store must move the build answers).
type probeWorld struct {
	store metadata.Store
	svc   *build.Service
	t     *testing.T
}

func newProbeWorld(t *testing.T) *probeWorld {
	t.Helper()
	ctx := context.Background()
	st, err := metadata.Open(ctx, metadata.Options{
		Path:          filepath.Join(t.TempDir(), "binflow.db"),
		AdminPassword: "pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	az := auth.NewFromStore(st, false) // anonymous_access off
	w := &probeWorld{store: st, svc: build.New(st.Builds(), az), t: t}

	mk := func(l ...string) string {
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(b)
	}
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	target := func(name string, repos, includes, excludes []string, rows ...*metadata.PermissionPrincipal) {
		t.Helper()
		if err := st.Permissions().PutTarget(ctx, &metadata.PermissionTarget{
			Name: name, Repos: mk(repos...), Includes: mk(includes...),
			Excludes: mk(excludes...), CreatedAt: now, UpdatedAt: now,
		}, rows); err != nil {
			t.Fatalf("PutTarget %s: %v", name, err)
		}
	}

	// bob reads build names matching pub-* under the default build repo
	// (the path position IS the build name — pattern-by-name is the whole
	// point of the ACL face). carol reads everything except sec*.
	// mallory holds ANY LOCAL — the wildcard bucket that must NOT reach
	// the build domain.
	target("pub-ci", []string{metadata.DefaultBuildRepo}, []string{"pub-*"}, nil,
		&metadata.PermissionPrincipal{TargetName: "pub-ci", Principal: "bob", PrincipalType: "user", CanRead: true})
	target("ci-wide", []string{metadata.DefaultBuildRepo}, []string{"**"}, []string{"sec*"},
		&metadata.PermissionPrincipal{TargetName: "ci-wide", Principal: "carol", PrincipalType: "user", CanRead: true})
	target("any-local-reader", []string{auth.BucketAnyLocal}, nil, nil,
		&metadata.PermissionPrincipal{TargetName: "any-local-reader", Principal: "mallory", PrincipalType: "user", CanRead: true})

	// Three runs: a visible one, an excluded-name one, and the same
	// visible name under a DIFFERENT build repo (the per-repo filter arm).
	w.seedRun(&metadata.Build{Name: "pub-1", Number: "5", Started: probeStampA, Payload: `{"run":"pub-1#5"}`})
	w.seedRun(&metadata.Build{Name: "secret", Number: "9", Started: probeStampA, Payload: `{"run":"secret#9"}`})
	w.seedRun(&metadata.Build{Name: "pub-1", Number: "7", Started: probeStampB, Repo: "team-build-info", Payload: `{"run":"team"}`})
	return w
}

func (w *probeWorld) seedRun(b *metadata.Build) {
	w.t.Helper()
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	b.CreatedBy, b.CreatedAt, b.UpdatedBy, b.UpdatedAt = "ci", now, "ci", now
	if err := w.store.Builds().PutBuild(context.Background(), b); err != nil {
		w.t.Fatalf("seed run %s#%s: %v", b.Name, b.Number, err)
	}
}

var (
	bobPrincipal    = &auth.Principal{Name: "bob", Role: auth.RoleUser}
	carolPrincipal  = &auth.Principal{Name: "carol", Role: auth.RoleUser}
	malloryNeighbor = &auth.Principal{Name: "mallory", Role: auth.RoleUser}
	evePrincipal    = &auth.Principal{Name: "eve", Role: auth.RoleUser} // no row anywhere
	// adminPrincipal carries the DERIVED Admin mirror alongside the role —
	// exactly how the authenticator builds real principals; the allow
	// mirror's first line reads that field (repo/service.go's shape).
	adminPrincipal = &auth.Principal{Name: "root", Role: auth.RoleAdmin, Admin: true}
	readerAdmin    = &auth.Principal{Name: "auditor", Role: auth.RoleReadOnlyAdmin}
)

// TestBuildACLQueryForbiddenArm: the first NFR-S80 arm — a user with no
// grant on the build domain gets ErrForbidden (the 403 face's service
// form) on the single-build query. The denial precedes existence: a
// DENIED principal cannot even learn whether a build exists (missing
// coordinates answer forbidden, not not-found — no existence oracle).
func TestBuildACLQueryForbiddenArm(t *testing.T) {
	w := newProbeWorld(t)
	ctx := context.Background()

	if _, err := w.svc.GetBuild(ctx, evePrincipal, build.Coordinate{Name: "pub-1", Number: "5"}); !errors.Is(err, build.ErrForbidden) {
		t.Errorf("eve get(pub-1#5) = %v, want ErrForbidden", err)
	}
	if _, err := w.svc.GetBuild(ctx, evePrincipal, build.Coordinate{Name: "ghost", Number: "1"}); !errors.Is(err, build.ErrForbidden) {
		t.Errorf("eve get(missing) = %v, want ErrForbidden — denial precedes existence, no oracle", err)
	}
	if w.svc.CanRead(ctx, evePrincipal, metadata.DefaultBuildRepo, "pub-1") {
		t.Error("eve CanRead = true, want false (no grant anywhere)")
	}
}

// TestBuildACLDetailZeroAppearanceArm: the second arm — a build the
// principal cannot read NEVER appears through the detail face: the row
// exists (admin sees it), but bob's pattern (pub-*) and carol's exclude
// (sec*) both answer forbidden with no field of the row in the error.
func TestBuildACLDetailZeroAppearanceArm(t *testing.T) {
	w := newProbeWorld(t)
	ctx := context.Background()

	// The row demonstrably exists — the admin arm reads it.
	got, err := w.svc.GetBuild(ctx, adminPrincipal, build.Coordinate{Name: "secret", Number: "9"})
	if err != nil {
		t.Fatalf("admin get(secret#9): %v", err)
	}
	if got.Payload != `{"run":"secret#9"}` {
		t.Fatalf("admin got payload %q, want the seeded run", got.Payload)
	}

	for name, p := range map[string]*auth.Principal{"bob": bobPrincipal, "carol": carolPrincipal} {
		_, err := w.svc.GetBuild(ctx, p, build.Coordinate{Name: "secret", Number: "9"})
		if !errors.Is(err, build.ErrForbidden) || errors.Is(err, metadata.ErrBuildNotFound) {
			// forbidden, and NOT a not-found masquerade on the same error
			t.Errorf("%s get(secret#9) = %v, want ErrForbidden without a not-found alias (zero appearance)", name, err)
		}
	}

	// The positive controls: bob and carol read their granted names.
	for name, p := range map[string]*auth.Principal{"bob": bobPrincipal, "carol": carolPrincipal} {
		got, err := w.svc.GetBuild(ctx, p, build.Coordinate{Name: "pub-1", Number: "5"})
		if err != nil {
			t.Errorf("%s get(pub-1#5) = %v, want the visible run", name, err)
			continue
		}
		if got.Payload != `{"run":"pub-1#5"}` {
			t.Errorf("%s get(pub-1#5) payload = %q", name, got.Payload)
		}
	}

	// started='' rides the gate too: the latest-run address of a denied
	// name is still forbidden.
	if _, err := w.svc.GetBuild(ctx, bobPrincipal, build.Coordinate{Name: "secret", Number: "9", Started: probeStampA}); !errors.Is(err, build.ErrForbidden) {
		t.Errorf("bob get(secret#9 exact started) = %v, want ErrForbidden", err)
	}
}

// TestBuildACLListZeroLeakArm: the third arm — the listing faces run the
// server-side visible-set filter: every row is evaluated against
// allow(r) over its own (build_repo, build_name) BEFORE it joins the
// answer. secret never leaks to bob/carol, and the same-name run under
// team-build-info stays invisible to them (their target lists only the
// default key) — while the admin sees all three rows.
func TestBuildACLListZeroLeakArm(t *testing.T) {
	w := newProbeWorld(t)
	ctx := context.Background()

	names, err := w.svc.ListBuildNames(ctx, bobPrincipal)
	if err != nil {
		t.Fatalf("bob list names: %v", err)
	}
	if len(names) != 1 || names[0].Name != "pub-1" || names[0].Repo != metadata.DefaultBuildRepo {
		t.Fatalf("bob names = %+v, want exactly pub-1@default (secret and the team run filtered)", names)
	}
	names, err = w.svc.ListBuildNames(ctx, evePrincipal)
	if err != nil || len(names) != 0 {
		t.Fatalf("eve names = %+v (%v), want empty", names, err)
	}
	names, err = w.svc.ListBuildNames(ctx, adminPrincipal)
	if err != nil || len(names) != 3 {
		t.Fatalf("admin names = %+v (%v), want all three rows", names, err)
	}

	// The numbers face: an invisible name answers EMPTY for bob (zero
	// leak), the visible name answers only the default repo's run — the
	// team-build-info run of the SAME name is filtered by its own repo.
	secretNumbers, err := w.svc.ListBuildNumbers(ctx, bobPrincipal, "secret")
	if err != nil || len(secretNumbers) != 0 {
		t.Fatalf("bob numbers(secret) = %+v (%v), want empty (zero leak)", secretNumbers, err)
	}
	pubNumbers, err := w.svc.ListBuildNumbers(ctx, bobPrincipal, "pub-1")
	if err != nil || len(pubNumbers) != 1 || pubNumbers[0].Repo != metadata.DefaultBuildRepo || pubNumbers[0].Number != "5" {
		t.Fatalf("bob numbers(pub-1) = %+v (%v), want only run 5 of the default repo", pubNumbers, err)
	}
	allNumbers, err := w.svc.ListBuildNumbers(ctx, adminPrincipal, "pub-1")
	if err != nil || len(allNumbers) != 2 {
		t.Fatalf("admin numbers(pub-1) = %+v (%v), want both repos' runs", allNumbers, err)
	}
}

// TestBuildACLRolesAndFailClosed: the mirror's own arms — admin bypass in
// the mirror (even with no authorizer wired: p.Admin short-circuits
// first), nil authorizer fail-closed for everyone else, readonly_admin's
// r-always content-plane semantics reaching the build domain through the
// SAME auth.Service, and the anonymous denial (anonymous_access off).
func TestBuildACLRolesAndFailClosed(t *testing.T) {
	w := newProbeWorld(t)
	ctx := context.Background()

	// readonly_admin: targets are short-circuited, r is always granted —
	// the build domain inherits the semantics through the shared source.
	got, err := w.svc.GetBuild(ctx, readerAdmin, build.Coordinate{Name: "secret", Number: "9"})
	if err != nil {
		t.Fatalf("readonly_admin get(secret#9) = %v, want r-always granted", err)
	}
	if got.Name != "secret" {
		t.Fatalf("readonly_admin got %+v, want the secret run", got)
	}
	// The write face of the same role is hard-denied — pinned at the
	// projection level (the write faces land in T-508 on this mirror).
	if !w.svc.CanRead(ctx, readerAdmin, "any-repo-never-listed", "any-name") {
		t.Error("readonly_admin CanRead = false, want true (r is role-granted, targets never consulted)")
	}

	// The nil-authorizer build: fail-closed for users, open for admin
	// (the mirror's first line is the admin short-circuit, the repo
	// service's order verbatim).
	closed := build.New(w.store.Builds(), nil)
	if _, err := closed.GetBuild(ctx, bobPrincipal, build.Coordinate{Name: "pub-1", Number: "5"}); !errors.Is(err, build.ErrForbidden) {
		t.Errorf("nil-az bob get = %v, want ErrForbidden (fail closed)", err)
	}
	if _, err := closed.GetBuild(ctx, adminPrincipal, build.Coordinate{Name: "secret", Number: "9"}); err != nil {
		t.Errorf("nil-az admin get = %v, want the admin short-circuit ahead of the nil check", err)
	}

	// Anonymous (nil principal): anonymous_access off denies the read.
	if _, err := w.svc.GetBuild(ctx, nil, build.Coordinate{Name: "pub-1", Number: "5"}); !errors.Is(err, build.ErrForbidden) {
		t.Errorf("anonymous get = %v, want ErrForbidden (anonymous_access off)", err)
	}
}

// TestBuildACLWildcardBucketExcluded: the ANY LOCAL bucket grant must not
// reach the build domain — the build_repo logical key is no repository
// row, so no class resolves and the bucket arm never fires: exact-key
// targets only (ADR-0045 Errata ④㋑ direction: the ANY family never
// IMPLICITLY covers buildinfo; a wildcard grant must name the key).
func TestBuildACLWildcardBucketExcluded(t *testing.T) {
	w := newProbeWorld(t)
	ctx := context.Background()

	// Control: mallory's ANY LOCAL grant works on a real local repository
	// (the class resolves), so the probe below failing means the exclusion
	// — not a broken grant.
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if err := w.store.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "libs-release", Type: "local", PackageType: "generic",
		Config: "{}", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	az := auth.NewFromStore(w.store, false)
	if !az.Can(ctx, malloryNeighbor, "libs-release", "anything", auth.ActionRead) {
		t.Fatal("control: mallory ANY LOCAL read on a real local repo = false, grant broken")
	}

	// The exclusion: the same grant does not read the build domain.
	if w.svc.CanRead(ctx, malloryNeighbor, metadata.DefaultBuildRepo, "pub-1") {
		t.Error("ANY LOCAL grant reached the build domain, want excluded (no repositories row -> no class -> no bucket)")
	}
	if _, err := w.svc.GetBuild(ctx, malloryNeighbor, build.Coordinate{Name: "pub-1", Number: "5"}); !errors.Is(err, build.ErrForbidden) {
		t.Errorf("mallory get(pub-1#5) = %v, want ErrForbidden (bucket excluded)", err)
	}
	names, err := w.svc.ListBuildNames(ctx, malloryNeighbor)
	if err != nil || len(names) != 0 {
		t.Fatalf("mallory names = %+v (%v), want empty (bucket excluded from the visible set)", names, err)
	}
}

// TestBuildServiceCoordinateValidation: the hard input floor of the
// coordinate — the 400 family's service form (distinct from both
// forbidden and not-found), table-driven.
func TestBuildServiceCoordinateValidation(t *testing.T) {
	w := newProbeWorld(t)
	ctx := context.Background()

	cases := []struct {
		name string
		c    build.Coordinate
	}{
		{"empty name", build.Coordinate{Number: "5"}},
		{"name with slash", build.Coordinate{Name: "a/b", Number: "5"}},
		{"name with newline", build.Coordinate{Name: "a\nb", Number: "5"}},
		{"oversized name", build.Coordinate{Name: string(make([]byte, 257)), Number: "5"}},
		{"empty number", build.Coordinate{Name: "pub-1"}},
		{"oversized number", build.Coordinate{Name: "pub-1", Number: string(make([]byte, 129))}},
		{"control started", build.Coordinate{Name: "pub-1", Number: "5", Started: "2026-09-07\t10:00"}},
	}
	for _, tc := range cases {
		if _, err := w.svc.GetBuild(ctx, adminPrincipal, tc.c); !errors.Is(err, build.ErrInvalidCoordinate) {
			t.Errorf("%s: get = %v, want ErrInvalidCoordinate", tc.name, err)
		}
	}
	if err := build.ValidateBuildName("no/slash"); !errors.Is(err, build.ErrInvalidCoordinate) {
		t.Errorf("ValidateBuildName(slash) = %v, want ErrInvalidCoordinate", err)
	}
	if err := build.ValidateBuildName("fine-name.1"); err != nil {
		t.Errorf("ValidateBuildName(fine) = %v, want nil", err)
	}

	// A valid but absent coordinate for the admin answers the honest
	// not-found (wrapped), NOT invalid and NOT forbidden — the three-way
	// distinction the httpapi face maps onto 400/404/403.
	_, err := w.svc.GetBuild(ctx, adminPrincipal, build.Coordinate{Name: "fine-name.1", Number: "404"})
	if !errors.Is(err, metadata.ErrBuildNotFound) || errors.Is(err, build.ErrInvalidCoordinate) || errors.Is(err, build.ErrForbidden) {
		t.Errorf("admin get(valid missing) = %v, want wrapped ErrBuildNotFound only", err)
	}

	// The list face validates its bare name the same way.
	if _, err := w.svc.ListBuildNumbers(ctx, adminPrincipal, "bad/name"); !errors.Is(err, build.ErrInvalidCoordinate) {
		t.Errorf("numbers(bad name) = %v, want ErrInvalidCoordinate", err)
	}
}

// TestBuildServiceLatestRunAddress: started=” resolves the latest run of
// (name, number, repo) behind the gate — the single-build GET default
// the T-508 face rides.
func TestBuildServiceLatestRunAddress(t *testing.T) {
	w := newProbeWorld(t)
	ctx := context.Background()

	// Two runs of pub-1#5 (same name+number, different started stamps).
	w.seedRun(&metadata.Build{Name: "pub-1", Number: "5", Started: probeStampB, Payload: `{"run":"latest"}`})

	got, err := w.svc.GetBuild(ctx, bobPrincipal, build.Coordinate{Name: "pub-1", Number: "5"})
	if err != nil {
		t.Fatalf("bob get latest: %v", err)
	}
	if got.Started != probeStampB || got.Payload != `{"run":"latest"}` {
		t.Fatalf("latest address = started %q payload %q, want the probeStampB run", got.Started, got.Payload)
	}
	got, err = w.svc.GetBuild(ctx, bobPrincipal, build.Coordinate{Name: "pub-1", Number: "5", Started: probeStampA})
	if err != nil || got.Payload != `{"run":"pub-1#5"}` {
		t.Fatalf("exact address = %+v (%v), want the original run", got, err)
	}

	// The names projection reports the group's lastStarted — both runs of
	// pub-1#5 plus the seeded team run collapse correctly for the admin.
	names, err := w.svc.ListBuildNames(ctx, adminPrincipal)
	if err != nil {
		t.Fatalf("admin names: %v", err)
	}
	for _, n := range names {
		if n.Repo != metadata.DefaultBuildRepo {
			continue
		}
		if n.Name == "pub-1" && n.LastStarted != probeStampB {
			t.Errorf("pub-1 lastStarted = %q, want %q (the newer run)", n.LastStarted, probeStampB)
		}
	}
}
