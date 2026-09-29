//go:build dev

package license_test

// T-573 / BIN-55 — the -tags dev scratch bypass, dev-build arm: the
// BINFLOW_DEV_TIER environment variable overrides the Manager's effective
// tier readout (fail-safe to community on an out-of-set value, one WARN
// line), unlocking the gated package types for the pro-scratch
// differential runs. Compiled only under -tags dev.

import (
	"context"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/license"
)

// TestDevTierOverrideState pins the readout: every spellable value lands,
// an unspellable one fails safe to community, an empty variable is no
// override at all.
func TestDevTierOverrideState(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want license.Tier
	}{
		{"community override", "community", license.TierCommunity},
		{"pro override", "pro", license.TierPro},
		{"enterprise override", "enterprise", license.TierEnterprise},
		{"out-of-set value fails safe to community", "platinum", license.TierCommunity},
		{"empty value is no override", "", license.TierCommunity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BINFLOW_DEV_TIER", tc.env)
			m := newTestManager(t, nil, &fakeStore{}, nil)
			if got := m.State().Tier; got != tc.want {
				t.Fatalf("State().Tier = %v, want %v (env %q)", got, tc.want, tc.env)
			}
		})
	}
}

// TestDevTierInvalidValueLogsOnce pins the single WARN line of the
// fail-safe arm (operator input value only — never a document or key).
func TestDevTierInvalidValueLogsOnce(t *testing.T) {
	t.Setenv("BINFLOW_DEV_TIER", "not-a-tier")
	m := newTestManager(t, nil, &fakeStore{}, nil)
	if got := m.State().Tier; got != license.TierCommunity {
		t.Fatalf("State().Tier = %v, want community under an invalid override", got)
	}
	if n := strings.Count(m.log.String(), "BINFLOW_DEV_TIER"); n != 1 {
		t.Fatalf("WARN lines naming the variable = %d, want exactly 1:\n%s", n, m.log.String())
	}
}

// TestDevTierUnlocksGatedRepoTypes is the five-family repo-create leg at
// the gate predicate (the live scratch curl legs ride the same single
// evaluation point): cargo/debian/nuget/rpm/helm are pro slots, an
// enterprise override unlocks every verdict — including the enterprise
// feature floor (ha) — while an unset variable keeps them locked.
func TestDevTierUnlocksGatedRepoTypes(t *testing.T) {
	gated := []string{"cargo", "debian", "nuget", "rpm", "helm"}
	ctx := context.Background()

	t.Run("enterprise override unlocks", func(t *testing.T) {
		t.Setenv("BINFLOW_DEV_TIER", "enterprise")
		m := newTestManager(t, nil, &fakeStore{}, nil)
		for _, pt := range gated {
			if !m.PackageTypeAvailable(ctx, pt, license.TierPro) {
				t.Errorf("PackageTypeAvailable(%q, pro) = false under the enterprise dev override", pt)
			}
		}
		if !m.AddonEnabled(ctx, "ha", license.TierEnterprise) {
			t.Error("AddonEnabled(ha, enterprise) = false under the enterprise dev override")
		}
	})

	t.Run("pro override covers the pro floor but not enterprise", func(t *testing.T) {
		t.Setenv("BINFLOW_DEV_TIER", "pro")
		m := newTestManager(t, nil, &fakeStore{}, nil)
		for _, pt := range gated {
			if !m.PackageTypeAvailable(ctx, pt, license.TierPro) {
				t.Errorf("PackageTypeAvailable(%q, pro) = false under the pro dev override", pt)
			}
		}
		if m.AddonEnabled(ctx, "ha", license.TierEnterprise) {
			t.Error("AddonEnabled(ha, enterprise) = true under the pro dev override")
		}
	})

	t.Run("unset stays locked", func(t *testing.T) {
		t.Setenv("BINFLOW_DEV_TIER", "")
		m := newTestManager(t, nil, &fakeStore{}, nil)
		for _, pt := range gated {
			if m.PackageTypeAvailable(ctx, pt, license.TierPro) {
				t.Errorf("PackageTypeAvailable(%q, pro) = true with no override", pt)
			}
		}
	})
}
