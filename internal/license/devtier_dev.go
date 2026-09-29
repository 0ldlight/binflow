//go:build dev

package license

import (
	"log/slog"
	"os"
)

// DevTierEnvVar is the environment variable the dev-build scratch bypass
// honors (T-573 / BIN-55). Values are the tier closed set's wire spellings
// (community/pro/enterprise). This file compiles ONLY under -tags dev; the
// default build's arm (devtier_prod.go) never reads the variable.
const DevTierEnvVar = "BINFLOW_DEV_TIER"

// resolveDevTier is the dev-build arm of the T-573 bypass: read
// BINFLOW_DEV_TIER once at Manager construction and hand State() the
// override. Unset/empty = no override (the honest floor). An out-of-set
// value fails safe to community with ONE warn line — the same direction as
// Tier.String and every D1~D7 degradation: an override never widens past a
// spellable tier, and a typo never bricks the scratch startup.
//
// Dev builds are local-scratch only (BIN-55): no CI lane passes -tags dev,
// and the artifacts are never released. No license document is forged —
// only the effective-tier readout moves; Install still demands a genuinely
// verifiable ed25519 document.
func resolveDevTier(log *slog.Logger) devTierOverride {
	v := os.Getenv(DevTierEnvVar)
	if v == "" {
		return devTierOverride{}
	}
	if t, ok := ParseTier(v); ok {
		return devTierOverride{active: true, tier: t}
	}
	log.Warn("license: "+DevTierEnvVar+" is not a tier in the closed set (community|pro|enterprise); failing safe to community",
		"value", v)
	return devTierOverride{active: true, tier: TierCommunity}
}
