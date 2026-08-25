package license

// Tier is the license tier closed set (ADR-0032 decision "档位模型 B"):
// community < pro < enterprise, a total order carried by the int value.
// Adding a tier is an architecture change with a new ADR, never a data edit
// — the matrix is code, not configuration.
type Tier int

const (
	// TierCommunity is the floor: every M1~M9 capability, the five core
	// package types included, with no license installed at all. An
	// unlicensed, expired, invalid or insufficient license always degrades
	// to this tier (D1~D7).
	TierCommunity Tier = iota
	// TierPro is the mid tier (gated package types and mid-tier addons).
	TierPro
	// TierEnterprise is the top tier (HA-class capabilities).
	TierEnterprise
)

// tierNames maps every Tier to its wire spelling. The map is the single
// spelling source for String and ParseTier.
var tierNames = map[Tier]string{
	TierCommunity:  "community",
	TierPro:        "pro",
	TierEnterprise: "enterprise",
}

// String returns the wire spelling ("community" | "pro" | "enterprise").
// An out-of-set value renders as "community" — the fail-safe direction:
// rendering never widens an entitlement.
func (t Tier) String() string {
	if name, ok := tierNames[t]; ok {
		return name
	}
	return tierNames[TierCommunity]
}

// ParseTier resolves a wire spelling onto the closed set. ok is false for
// anything outside the set — callers treat that as an invalid document,
// never as an unknown-high-tier guess.
func ParseTier(s string) (t Tier, ok bool) {
	for tier, name := range tierNames {
		if name == s {
			return tier, true
		}
	}
	return TierCommunity, false
}

// Tiers returns the closed set in ascending order (the QA matrix's tier
// axis; ADR-0026's enumerable-closed-set philosophy).
func Tiers() []Tier { return []Tier{TierCommunity, TierPro, TierEnterprise} }
