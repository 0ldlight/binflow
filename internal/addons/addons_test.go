package addons_test

// T-282 registry behavior: the production manifest's shape (11 slots, kind
// split, stable order), the assembly-time panic family, and the five-core
// retro-fit invariant (floor tier => never gated). The unlock matrix lives
// in view_test.go over a REAL license.Manager.

import (
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/license"
)

// productionManifest is the exact slice cmd/binflow-server assembles (kept
// in sync by TestCmdAssemblyMatchesProductionManifest on the cmd side).
func productionManifest() []addons.Addon {
	return []addons.Addon{
		addons.Generic(), addons.Docker(), addons.Maven(), addons.Npm(), addons.Pypi(),
		addons.Go(), addons.NuGet(), addons.Cargo(),
		addons.Properties(), addons.RepoOperations(), addons.Trashcan(), addons.HA(), addons.XrayIntegration(),
	}
}

func TestManifestShape(t *testing.T) {
	r := addons.New(productionManifest()...)

	if got := r.Len(); got < 10 {
		t.Fatalf("manifest carries %d slots, want >= 10 (FR-86-AC1)", got)
	}
	if len(r.All()) != r.Len() {
		t.Fatalf("All() = %d, Len() = %d", len(r.All()), r.Len())
	}
	if len(r.PackageTypeAddons()) != 8 || len(r.FeatureAddons()) != 5 {
		t.Fatalf("kind split wrong: %d package-type / %d feature",
			len(r.PackageTypeAddons()), len(r.FeatureAddons()))
	}

	// Stable assembly order: the response array is diffable across builds.
	wantOrder := []string{
		"generic", "docker", "maven", "npm", "pypi",
		"go", "nuget", "cargo",
		"properties", "repo-operations", "trashcan", "ha", "xray-integration",
	}
	for i, id := range wantOrder {
		if r.All()[i].ID != id {
			t.Fatalf("slot %d = %q, want %q", i, r.All()[i].ID, id)
		}
	}

	// Tier marks per PRD 86.2: five core + properties on the floor, the
	// pilots at pro, the enterprise placeholders at ent.
	for _, a := range r.All() {
		var want license.Tier
		switch a.ID {
		case "generic", "docker", "maven", "npm", "pypi", "properties":
			want = license.TierCommunity
		case "go", "nuget", "cargo", "repo-operations", "trashcan":
			want = license.TierPro
		case "ha", "xray-integration":
			want = license.TierEnterprise
		default:
			t.Fatalf("unexpected slot %q in the production manifest", a.ID)
		}
		if a.MinTier != want {
			t.Fatalf("slot %s MinTier = %s, want %s", a.ID, a.MinTier, want)
		}
		if a.DisplayName == "" || a.Description == "" {
			t.Fatalf("slot %s lacks console metadata", a.ID)
		}
		// The enterprise placeholders' descriptions must carry the M11+
		// reservation note (PRD 86.2/AC3).
		if a.MinTier == license.TierEnterprise && !strings.Contains(a.Description, "M11+") {
			t.Fatalf("enterprise slot %s description lacks the M11+ reservation note: %q", a.ID, a.Description)
		}
	}
}

// TestRetrofitFiveCoreNeverGated: the retro-fit's zero-behavior-change
// invariant at the registry level — every five-core slot sits on the floor,
// so the gated derivation (ok && MinTier > community) is false for all of
// them on every instance (architecture invariant 1).
func TestRetrofitFiveCoreNeverGated(t *testing.T) {
	r := addons.New(productionManifest()...)
	for _, pt := range []string{"generic", "docker", "maven", "npm", "pypi"} {
		a, ok := r.ForPackageType(pt)
		if !ok {
			t.Fatalf("five-core slot %s missing from the manifest (the §15.2.4 panic case, defused)", pt)
		}
		if a.MinTier != license.TierCommunity {
			t.Fatalf("five-core slot %s MinTier = %s, want community (retro-fit floor)", pt, a.MinTier)
		}
		if gated := ok && a.MinTier > license.TierCommunity; gated {
			t.Fatalf("five-core slot %s is gated — the retro-fit must never gate them", pt)
		}
	}
}

func TestLookups(t *testing.T) {
	r := addons.New(productionManifest()...)
	if a, ok := r.ForPackageType("go"); !ok || a.ID != "go" || a.Kind != addons.KindPackageType {
		t.Fatalf("ForPackageType(go) = (%+v, %v)", a, ok)
	}
	if a, ok := r.ForPackageType("ha"); ok {
		t.Fatalf("ForPackageType answered a feature slot: %+v", a)
	}
	if _, ok := r.ByID("ha"); !ok {
		t.Fatal("ByID(ha) missing")
	}
	if _, ok := r.ByID("nope"); ok {
		t.Fatal("ByID(nope) answered")
	}

	// The returned slices are copies: a caller mutation must not poison the
	// registry (the manifest is read-only after assembly).
	all := r.All()
	all[0].ID = "mutated"
	if again, _ := r.ByID("generic"); again.ID != "generic" {
		t.Fatalf("All() leaked internal storage: %+v", again)
	}
}

func TestNewPanics(t *testing.T) {
	base := addons.Generic()
	cases := []struct {
		name string
		item addons.Addon
		want string
	}{
		{"empty id", addons.Addon{Kind: addons.KindFeature, MinTier: license.TierCommunity, DisplayName: "x"}, "empty ID"},
		{"empty display name", addons.Addon{ID: "f", Kind: addons.KindFeature, MinTier: license.TierCommunity}, "DisplayName"},
		{"unknown kind", addons.Addon{ID: "f", Kind: "other", MinTier: license.TierCommunity, DisplayName: "x"}, "unknown Kind"},
		{"package-type without PackageType", addons.Addon{ID: "g", Kind: addons.KindPackageType, MinTier: license.TierCommunity, DisplayName: "x"}, "PackageType"},
		{
			"package-type id mismatch",
			addons.Addon{ID: "g", Kind: addons.KindPackageType, MinTier: license.TierCommunity, PackageType: "golang", DisplayName: "x"},
			"must equal the ID",
		},
		{
			"feature with PackageType",
			addons.Addon{ID: "f", Kind: addons.KindFeature, MinTier: license.TierCommunity, PackageType: "f", DisplayName: "x"},
			"leave PackageType empty",
		},
		{
			"tier above the closed set",
			addons.Addon{ID: "f", Kind: addons.KindFeature, MinTier: license.TierEnterprise + 1, DisplayName: "x"},
			"closed tier set",
		},
		{
			"tier below the closed set",
			addons.Addon{ID: "f", Kind: addons.KindFeature, MinTier: license.TierCommunity - 1, DisplayName: "x"},
			"closed tier set",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// panicMessage returns "" when New does not panic, so one loop
			// pins both the panic itself and its reason.
			got := panicMessage(func() { _ = addons.New(base, tc.item) })
			if got == "" {
				t.Fatalf("New accepted the malformed %q descriptor", tc.name)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("panic = %q, want it to contain %q", got, tc.want)
			}
		})
	}
	// Duplicate id — the one collision a registry can actually catch.
	got := panicMessage(func() { _ = addons.New(base, addons.Generic()) })
	if !strings.Contains(got, "duplicate addon id") {
		t.Fatalf("duplicate panic = %q", got)
	}
}

func panicMessage(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg, _ = r.(string)
		}
	}()
	f()
	return ""
}
