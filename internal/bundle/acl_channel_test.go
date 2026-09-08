// The read faces' dual gate over the REAL permission plane (M17 T-513,
// ADR-0046 decision 3 + Errata ④ E8): the capability arm (readonly_admin
// reads through CapSystemRead, the plain user does not) and the Any
// Distribution pseudo-key channel — a target whose repos[] names the
// bucket literal grants read BY BUNDLE NAME, includes/excludes applying
// on the name exactly as T-491 froze, the bucket never leaking to
// ungranted principals, and the write face staying capability-gated for
// everyone (NFR-S81's zero-leak probes).

package bundle_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/bundle"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// putBundleChannelTarget installs one permission target whose repos[]
// carries the ANY DISTRIBUTION bucket literal, granting read to one user.
func putBundleChannelTarget(t *testing.T, st metadata.Store, name string, includes, excludes []string, user string) {
	t.Helper()
	mk := func(l []string) string {
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatalf("marshal patterns: %v", err)
		}
		return string(b)
	}
	now := metadata.Now()
	target := &metadata.PermissionTarget{
		Name: name, Repos: mk([]string{auth.BucketAnyDistribution}),
		Includes: mk(includes), Excludes: mk(excludes),
		CreatedAt: now, UpdatedAt: now,
	}
	principals := []*metadata.PermissionPrincipal{{
		TargetName: name, Principal: user, PrincipalType: "user", CanRead: true,
	}}
	if err := st.Permissions().PutTarget(context.Background(), target, principals); err != nil {
		t.Fatalf("put channel target %q: %v", name, err)
	}
}

// TestAnyDistributionChannelByname: the channel grants the read faces by
// NAME — includes admit matching names and refuse non-matching ones,
// excludes carve out, and the plain user without the grant sees nothing.
func TestAnyDistributionChannelByname(t *testing.T) {
	f := newBundleFixture(t)
	f.seedNode(t, "libs", "a.bin", "sha-a", 10)
	ctx := f.ctx

	// Three bundles: two matching the rel-* include pattern, one outside.
	for _, pair := range [][2]string{{"rel-q3", "1"}, {"rel-q4", "2"}, {"prod-annual", "1"}} {
		if _, err := f.svc.Create(ctx, adminP(), req(pair[0], pair[1], [2]string{"libs", "a.bin"})); err != nil {
			t.Fatalf("seed %s/%s: %v", pair[0], pair[1], err)
		}
	}

	// The channel grant: ANY DISTRIBUTION in repos[], includes ["rel-*"].
	putBundleChannelTarget(t, f.st, "rel-readers", []string{"rel-*"}, nil, "rel-bot")
	bot := &auth.Principal{Name: "rel-bot"}
	plain := &auth.Principal{Name: "nobody"}

	// Single read: the include admits by name, refuses the outsider, and
	// the ungranted principal is refused on both (ErrForbidden, never a
	// not-found masquerade).
	if _, err := f.svc.GetBundle(ctx, bot, "rel-q3", "1"); err != nil {
		t.Fatalf("channel read of rel-q3: %v", err)
	}
	if _, err := f.svc.GetBundle(ctx, bot, "prod-annual", "1"); !errors.Is(err, bundle.ErrForbidden) {
		t.Fatalf("channel read outside the include = %v, want ErrForbidden", err)
	}
	if _, err := f.svc.GetBundle(ctx, plain, "rel-q3", "1"); !errors.Is(err, bundle.ErrForbidden) {
		t.Fatalf("ungranted read = %v, want ErrForbidden", err)
	}

	// Names list: the visible set is server-side filtered — the bot sees
	// exactly the two matching names, the plain user none.
	names, err := f.svc.ListBundleNames(ctx, bot)
	if err != nil {
		t.Fatalf("channel list: %v", err)
	}
	if len(names) != 2 || names[0].Name != "rel-q3" || names[1].Name != "rel-q4" {
		t.Fatalf("channel list = %v, want the two rel-* names only (name order)", names)
	}
	names, err = f.svc.ListBundleNames(ctx, plain)
	if err != nil {
		t.Fatalf("plain list: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("ungranted list = %v, want the empty visible set", names)
	}

	// Versions list: the same name-scoped verdict (rel-q3 visible,
	// prod-annual refused).
	if rows, err := f.svc.ListBundleVersions(ctx, bot, "rel-q3"); err != nil || len(rows) != 1 {
		t.Fatalf("channel versions = %v (%v), want one row", rows, err)
	}
	if _, err := f.svc.ListBundleVersions(ctx, bot, "prod-annual"); !errors.Is(err, bundle.ErrForbidden) {
		t.Fatalf("channel versions outside the include = %v, want ErrForbidden", err)
	}
}

// TestAnyDistributionChannelExcludes: the exclude patterns of the SAME
// target carve names out of the bucket grant (the pattern plane applies
// verbatim to the pseudo-key channel).
func TestAnyDistributionChannelExcludes(t *testing.T) {
	f := newBundleFixture(t)
	f.seedNode(t, "libs", "a.bin", "sha-a", 10)
	for _, pair := range [][2]string{{"rel-open", "1"}, {"rel-secret", "1"}} {
		if _, err := f.svc.Create(f.ctx, adminP(), req(pair[0], pair[1], [2]string{"libs", "a.bin"})); err != nil {
			t.Fatalf("seed %s: %v", pair[0], err)
		}
	}
	putBundleChannelTarget(t, f.st, "rel-excluding", []string{"rel-*"}, []string{"rel-secret"}, "rel-bot")
	bot := &auth.Principal{Name: "rel-bot"}

	if _, err := f.svc.GetBundle(f.ctx, bot, "rel-open", "1"); err != nil {
		t.Fatalf("included name: %v", err)
	}
	if _, err := f.svc.GetBundle(f.ctx, bot, "rel-secret", "1"); !errors.Is(err, bundle.ErrForbidden) {
		t.Fatalf("excluded name = %v, want ErrForbidden", err)
	}
	names, err := f.svc.ListBundleNames(f.ctx, bot)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(names) != 1 || names[0].Name != "rel-open" {
		t.Fatalf("visible set = %v, want rel-open only (the exclude carves rel-secret)", names)
	}
}

// TestReadCapabilityArms: readonly_admin reads everything through
// CapSystemRead with NO permission row at all; the channel never widens
// into the WRITE face (the plain user who holds the bucket read grant
// still cannot create — zero verb expansion, the write arm is
// capability-only).
func TestReadCapabilityArms(t *testing.T) {
	f := newBundleFixture(t)
	f.seedNode(t, "libs", "a.bin", "sha-a", 10)
	if _, err := f.svc.Create(f.ctx, adminP(), req("rel-q3", "9", [2]string{"libs", "a.bin"})); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ro := &auth.Principal{Name: "auditor", Role: auth.RoleReadOnlyAdmin}
	if _, err := f.svc.GetBundle(f.ctx, ro, "rel-q3", "9"); err != nil {
		t.Fatalf("readonly_admin capability read: %v", err)
	}
	if names, err := f.svc.ListBundleNames(f.ctx, ro); err != nil || len(names) != 1 {
		t.Fatalf("readonly_admin list = %v (%v), want one row", names, err)
	}

	// The channel holder cannot write: the bucket grant is read-only by
	// construction here, and even so the create face consults ONLY
	// CapSystemWrite.
	putBundleChannelTarget(t, f.st, "rel-writers", nil, nil, "rel-bot")
	bot := &auth.Principal{Name: "rel-bot"}
	if _, err := f.svc.Create(f.ctx, bot, req("rel-q3", "10", [2]string{"libs", "a.bin"})); !errors.Is(err, bundle.ErrForbidden) {
		t.Fatalf("channel holder create = %v, want ErrForbidden (write is capability-gated)", err)
	}
	// The readonly_admin cannot write either (the role is read-only BY
	// ROLE — the short-circuit law).
	if _, err := f.svc.Create(f.ctx, ro, req("rel-q3", "11", [2]string{"libs", "a.bin"})); !errors.Is(err, bundle.ErrForbidden) {
		t.Fatalf("readonly_admin create = %v, want ErrForbidden", err)
	}
}

// TestReadGateFailClosed: a service without both seams denies every read
// except the admin short-circuit (nil caps + nil az = the unit-fake
// posture, fail closed).
func TestReadGateFailClosed(t *testing.T) {
	f := newBundleFixture(t)
	f.seedNode(t, "libs", "a.bin", "sha-a", 10)
	if _, err := f.svc.Create(f.ctx, adminP(), req("rel", "12", [2]string{"libs", "a.bin"})); err != nil {
		t.Fatalf("seed: %v", err)
	}
	bare := bundle.New(f.st.Bundles(), nil, nil)
	// admin still reads (the explicit short-circuit)…
	if _, err := bare.GetBundle(f.ctx, adminP(), "rel", "12"); err != nil {
		t.Fatalf("admin read through the bare service: %v", err)
	}
	// …everyone else does not.
	if _, err := bare.GetBundle(f.ctx, &auth.Principal{Name: "someone"}, "rel", "12"); !errors.Is(err, bundle.ErrForbidden) {
		t.Fatalf("seamless read = %v, want ErrForbidden", err)
	}
	if names, err := bare.ListBundleNames(f.ctx, &auth.Principal{Name: "someone"}); err != nil || len(names) != 0 {
		t.Fatalf("seamless list = %v (%v), want the empty visible set", names, err)
	}
}
