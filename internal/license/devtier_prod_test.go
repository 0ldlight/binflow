//go:build !dev

package license_test

// T-573 / BIN-55 — the default-build pin: the BINFLOW_DEV_TIER environment
// variable must be COMPLETELY ignored without the dev build tag — not read,
// not parsed, not logged. Default-build entitlement behavior is
// byte-identical to the pre-T-573 binary for every value of the variable.

import (
	"context"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/license"
)

// TestDevTierEnvIgnoredInDefaultBuild walks the variable's value space
// (both spellable tiers and garbage) and asserts the community floor and
// the locked gated package types hold — the honest unlicensed instance.
func TestDevTierEnvIgnoredInDefaultBuild(t *testing.T) {
	for _, v := range []string{"enterprise", "pro", "community", "platinum", " "} {
		t.Run("env="+v, func(t *testing.T) {
			t.Setenv("BINFLOW_DEV_TIER", v)
			m := newTestManager(t, nil, &fakeStore{}, nil)
			st := m.State()
			if st.Tier != license.TierCommunity || st.Licensed {
				t.Fatalf("State = %+v, want the unlicensed community floor (env %q)", st, v)
			}
			for _, pt := range []string{"cargo", "debian", "nuget", "rpm", "helm"} {
				if m.PackageTypeAvailable(context.Background(), pt, license.TierPro) {
					t.Errorf("PackageTypeAvailable(%q, pro) = true in a default build (env %q)", pt, v)
				}
			}
			if strings.Contains(m.log.String(), "BINFLOW_DEV_TIER") {
				t.Errorf("default build logged the variable — it must never be read (env %q):\n%s", v, m.log.String())
			}
		})
	}
}
