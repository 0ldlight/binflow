package addons_test

// T-282 unlock matrix: Registry.Statuses joined with a REAL license.Manager
// (T-279's actual evaluation point, test keys per the ADR injection seam —
// not a fake gate): the full tier x slot table, the allowlist narrowing
// (floor immune), the addons.disabled circuit breaker, the nil-evaluator
// floor, and the PackageTypeAvailable seam including the "register a new
// package-type slot and it is creatable" leg (FR-86's mount-point proof).

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// ---- the license test doubles (this package's own copies; internal/license
// keeps its pair in its _test package) ----

type memStore struct {
	mu  sync.Mutex
	rec *metadata.LicenseRecord
}

func (s *memStore) GetLicense(context.Context) (*metadata.LicenseRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rec, nil
}

func (s *memStore) PutLicense(_ context.Context, rec *metadata.LicenseRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rec = rec
	return nil
}

func (s *memStore) DeleteLicense(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rec = nil
	return nil
}

type signKeys struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
	kid  string
}

func newSignKeys(t *testing.T) signKeys {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	return signKeys{priv: priv, pub: pub, kid: "addons-test-key"}
}

// sign renders a valid v1 document for tier; allow narrows the addons
// whitelist (nil = tier-wide unlock).
func (k signKeys) sign(t *testing.T, tier license.Tier, allow []string) string {
	t.Helper()
	now := time.Now().UTC()
	payload, err := json.Marshal(map[string]any{
		"typ": "binflow-license", "alg": "EdDSA", "kid": k.kid, "ver": 1,
		"licenseId": "t282-" + tier.String(), "licensee": "Acme Corp",
		"tier":      tier.String(),
		"issuedAt":  now.Add(-24 * time.Hour).Format(time.RFC3339),
		"notBefore": now.Add(-1 * time.Hour).Format(time.RFC3339),
		"expiresAt": now.Add(365 * 24 * time.Hour).Format(time.RFC3339),
		"addons":    allow,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(ed25519.Sign(k.priv, payload))
}

func newManager(t *testing.T, k signKeys, disabledCSV string) *license.Manager {
	t.Helper()
	m, err := license.New(license.Options{
		Store:       &memStore{},
		VerifyKeys:  map[string]ed25519.PublicKey{k.kid: k.pub},
		DisabledCSV: disabledCSV,
	})
	if err != nil {
		t.Fatalf("license.New: %v", err)
	}
	return m
}

// ---- the matrix ----

// unlockTable is the expected (enabled, reason) per slot id for one instance
// posture. Built from the PRD 86.2 tier marks: floor = five core +
// properties, pro adds the pilots, enterprise adds the placeholders; allow
// (non-nil) is the document's explicit addon allowlist; disabled is the
// addons.disabled config set (it outranks everything).
func unlockTable(tier license.Tier, disabled map[string]bool, allow []string) map[string][2]any {
	table := map[string][2]any{}
	for _, a := range addons.New(productionManifest()...).All() {
		var enabled bool
		switch {
		case a.MinTier == license.TierCommunity:
			enabled = true // floor: unlocked on every tier, allowlist-immune
		case tier >= a.MinTier:
			enabled = allow == nil || containsID(allow, a.ID)
		default:
			enabled = false
		}
		switch {
		case disabled[a.ID]:
			table[a.ID] = [2]any{false, "disabled by configuration"}
		case enabled:
			table[a.ID] = [2]any{true, ""}
		case tier >= a.MinTier: // tier suffices but the allowlist does not name it
			table[a.ID] = [2]any{false, "not named in the license addon allowlist"}
		default:
			table[a.ID] = [2]any{false, "license tier " + tier.String() + " < " + a.MinTier.String()}
		}
	}
	return table
}

func containsID(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func assertStatuses(t *testing.T, got []addons.Status, want map[string][2]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("status rows = %d, want %d", len(got), len(want))
	}
	for _, row := range got {
		w := want[row.Addon.ID]
		enabled, _ := w[0].(bool)
		reason, _ := w[1].(string)
		if row.Enable != enabled || row.Reason != reason {
			t.Fatalf("slot %s = (enabled %v, reason %q), want (enabled %v, reason %q)",
				row.Addon.ID, row.Enable, row.Reason, enabled, reason)
		}
	}
}

// TestStatusMatrixFullTable: tier x slot over the real Manager — no license
// (community floor), pro, enterprise, each against the complete manifest.
func TestStatusMatrixFullTable(t *testing.T) {
	ctx := context.Background()
	for _, tier := range license.Tiers() {
		t.Run("tier-"+tier.String(), func(t *testing.T) {
			k := newSignKeys(t)
			m := newManager(t, k, "")
			if tier != license.TierCommunity {
				// The community ROW of the matrix is the unlicensed floor;
				// installing a community document must not change it either.
				if _, err := m.Install(ctx, k.sign(t, tier, nil)); err != nil {
					t.Fatalf("install %s: %v", tier, err)
				}
			}
			r := addons.New(productionManifest()...)
			assertStatuses(t, r.Statuses(ctx, m), unlockTable(tier, nil, nil))
		})
	}
}

// TestStatusUnlicensedFloorMatchesCommunityDoc: invariant 1's display half —
// an unlicensed instance and an explicit community document render the SAME
// matrix (the floor is the floor however you reach it).
func TestStatusUnlicensedFloorMatchesCommunityDoc(t *testing.T) {
	ctx := context.Background()
	r := addons.New(productionManifest()...)
	k := newSignKeys(t)

	unlicensed := r.Statuses(ctx, newManager(t, k, ""))
	licensed := r.Statuses(ctx, mustInstall(t, newManager(t, k, ""), k.sign(t, license.TierCommunity, nil)))
	for i := range unlicensed {
		if unlicensed[i] != licensed[i] {
			t.Fatalf("community document changed the matrix at %s: %+v vs %+v",
				unlicensed[i].Addon.ID, unlicensed[i], licensed[i])
		}
	}
}

func mustInstall(t *testing.T, m *license.Manager, doc string) *license.Manager {
	t.Helper()
	if _, err := m.Install(context.Background(), doc); err != nil {
		t.Fatalf("install: %v", err)
	}
	return m
}

// TestStatusAllowlistNarrowing: an explicit allowlist narrows non-floor
// slots ONLY — the five core package types and properties stay unlocked
// (Manager's floor-immunity rule, seen through the status view).
func TestStatusAllowlistNarrowing(t *testing.T) {
	ctx := context.Background()
	k := newSignKeys(t)
	m := mustInstall(t, newManager(t, k, ""), k.sign(t, license.TierEnterprise, []string{"ha"}))

	r := addons.New(productionManifest()...)
	assertStatuses(t, r.Statuses(ctx, m), unlockTable(license.TierEnterprise, nil, []string{"ha"}))

	// The two shapes the compact table cannot spell: the named slot itself
	// stays unlocked, and every floor slot ignores the allowlist.
	if row, ok := r.StatusOf(ctx, m, "ha"); !ok || !row.Enable || row.Reason != "" {
		t.Fatalf("allowlisted ha = %+v, want unlocked with no reason", row)
	}
	for _, id := range []string{"generic", "docker", "maven", "npm", "pypi", "properties"} {
		if row, _ := r.StatusOf(ctx, m, id); !row.Enable {
			t.Fatalf("allowlist constrained the floor slot %s: %+v", id, row)
		}
	}
}

// TestStatusDisabledConfig: the circuit breaker outranks tier AND floor —
// npm (a five-core slot) reports disabled by configuration, the gated go
// slot too, and untouched slots keep their tier verdicts. The reason
// derivation follows the Manager's evaluation order (disabled first), so a
// gated-and-disabled slot says "disabled", not "tier".
func TestStatusDisabledConfig(t *testing.T) {
	ctx := context.Background()
	k := newSignKeys(t)
	m := mustInstall(t, newManager(t, k, "npm,go"), k.sign(t, license.TierPro, nil))

	r := addons.New(productionManifest()...)
	assertStatuses(t, r.Statuses(ctx, m), unlockTable(license.TierPro, map[string]bool{"npm": true, "go": true}, nil))

	if row, _ := r.StatusOf(ctx, m, "npm"); row.Reason != "disabled by configuration" {
		t.Fatalf("npm reason = %q, want the config breaker named", row.Reason)
	}
	if row, _ := r.StatusOf(ctx, m, "go"); row.Enable || row.Reason != "disabled by configuration" {
		t.Fatalf("gated+disabled go = %+v, want disabled-by-config to win the reason", row)
	}
}

// TestStatusNilEvaluator: a stack assembled without the license collaborator
// still renders the honest floor (nothing "disabled by configuration" —
// no Manager consumed any CSV).
func TestStatusNilEvaluator(t *testing.T) {
	r := addons.New(productionManifest()...)
	assertStatuses(t, r.Statuses(context.Background(), nil), unlockTable(license.TierCommunity, nil, nil))
}

// TestPackageTypeAvailableSeam: weave point 2's single spelling — the
// registry resolves packageType -> (id, tier), the Manager decides. The
// mount-point leg registers a NEW package-type slot ("conan", pro) that
// exists nowhere else in the tree and proves it becomes available the
// moment it is assembled (FR-86-AC2: no scattered if-branches to touch).
func TestPackageTypeAvailableSeam(t *testing.T) {
	ctx := context.Background()
	k := newSignKeys(t)
	floor := newManager(t, k, "")
	pro := mustInstall(t, newManager(t, k, ""), k.sign(t, license.TierPro, nil))

	// The production manifest.
	r := addons.New(productionManifest()...)
	for _, pt := range []string{"generic", "docker", "maven", "npm", "pypi"} {
		if !r.PackageTypeAvailable(ctx, floor, pt) {
			t.Fatalf("five-core type %s unavailable on the floor (retro-fit broken)", pt)
		}
	}
	for _, pt := range []string{"go", "nuget", "cargo"} {
		if r.PackageTypeAvailable(ctx, floor, pt) {
			t.Fatalf("gated type %s available on the floor", pt)
		}
		if !r.PackageTypeAvailable(ctx, pro, pt) {
			t.Fatalf("gated type %s unavailable under a pro license", pt)
		}
	}
	if r.PackageTypeAvailable(ctx, floor, "bogus") || r.PackageTypeAvailable(ctx, pro, "bogus") {
		t.Fatal("unknown package type answered available")
	}
	// The disabled breaker reaches the repo-create spelling too.
	kDis := newSignKeys(t)
	mDis := mustInstall(t, newManager(t, kDis, "npm"), kDis.sign(t, license.TierPro, nil))
	if r.PackageTypeAvailable(ctx, mDis, "npm") {
		t.Fatal("disabled npm still creatable")
	}

	// The mount-point leg: a brand-new slot, assembled and nothing else.
	conan := addons.Addon{
		ID: "conan", Kind: addons.KindPackageType, MinTier: license.TierPro,
		PackageType: "conan", DisplayName: "Conan", Description: "C/C++ packages",
	}
	r2 := addons.New(productionManifest()[0], conan) // any assembly carrying it
	if r2.PackageTypeAvailable(ctx, floor, "conan") {
		t.Fatal("new pro slot available on the floor")
	}
	if !r2.PackageTypeAvailable(ctx, pro, "conan") {
		t.Fatal("new pro slot NOT available under a pro license — registering a package-type addon must be the whole mount")
	}
}
