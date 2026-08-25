package addons

import (
	"context"
	"fmt"

	"github.com/lzwzzy/binflow/internal/license"
)

// Gate is the license.Manager's entitlement facet as this package consumes
// it (consumer-side interface; *license.Manager satisfies it). The primitives
// keep license free of an addons import — ADR-0033's dependency direction.
type Gate interface {
	// AddonEnabled is license.Manager's single evaluation point of the
	// tier x addon unlock matrix (section 15.2.3).
	AddonEnabled(ctx context.Context, id string, minTier license.Tier) bool
}

// Evaluator is the status view's facet of the Manager: the gate above plus
// the state snapshot the REASON derivation reads (effective tier and the
// document's explicit addon allowlist). *license.Manager satisfies it.
type Evaluator interface {
	Gate
	State() license.State
}

// Status is one slot's evaluated row — the domain form GET /api/v1/addons
// projects onto the wire. Enabled and Reason come from the SAME evaluation
// the enforcement gates consult (Evaluator.AddonEnabled), so the visible
// matrix and the enforced matrix cannot diverge (section 15.2.5's
// single-source ruling, the ?permissions precedent).
type Status struct {
	Addon  Addon
	Enable bool
	Reason string
}

// Reason spellings (section 15.2.5): an unlocked slot carries an empty
// reason; the three locked shapes name their cause. A disabled-config hit
// wins the derivation because it wins the Manager's own evaluation order.
const (
	reasonDisabled  = "disabled by configuration"
	reasonAllowlist = "not named in the license addon allowlist"
)

// Statuses evaluates every slot against ev, in assembly order. A nil ev is
// the honest community floor (no license collaborator mounted — unit stacks:
// every floor slot unlocked, every gated slot locked with the tier reason);
// nothing is ever reported disabled by configuration without a Manager that
// actually consumed the addons.disabled CSV.
func (r *Registry) Statuses(ctx context.Context, ev Evaluator) []Status {
	out := make([]Status, 0, len(r.ordered))
	for _, a := range r.ordered {
		out = append(out, r.statusOf(ctx, ev, a))
	}
	return out
}

// StatusOf evaluates one slot (the single-addon spelling consumers that
// already hold an Addon use; ok is false for an unknown id).
func (r *Registry) StatusOf(ctx context.Context, ev Evaluator, id string) (Status, bool) {
	a, ok := r.byID[id]
	if !ok {
		return Status{}, false
	}
	return r.statusOf(ctx, ev, a), true
}

// statusOf joins one descriptor with the evaluator. The reason mirrors
// license.Manager.AddonEnabled's evaluation ORDER exactly: the disabled
// config first, then the tier floor, then the allowlist — so the stated
// reason is always the deciding clause, not a plausible one.
//
// The disabled probe is AddonEnabled at the floor tier: the Manager tests
// the disabled set before anything else and independent of tier, so a floor
// probe returning false names "disabled by configuration" and nothing else.
func (r *Registry) statusOf(ctx context.Context, ev Evaluator, a Addon) Status {
	st := Status{Addon: a}
	if ev == nil {
		st.Enable = a.MinTier <= license.TierCommunity
		if !st.Enable {
			st.Reason = tierReason(license.State{Tier: license.TierCommunity}, a)
		}
		return st
	}
	st.Enable = ev.AddonEnabled(ctx, a.ID, a.MinTier)
	if st.Enable {
		return st
	}
	if !ev.AddonEnabled(ctx, a.ID, license.TierCommunity) {
		st.Reason = reasonDisabled
		return st
	}
	state := ev.State()
	if state.Tier < a.MinTier {
		st.Reason = tierReason(state, a)
		return st
	}
	st.Reason = reasonAllowlist
	return st
}

// tierReason renders the insufficient-tier clause (section 15.2.5's example
// spelling: "license tier community < pro").
func tierReason(state license.State, a Addon) string {
	return fmt.Sprintf("license tier %s < %s", state.Tier, a.MinTier)
}

// PackageTypeAvailable is weave point 2's predicate (section 15.1.5): a
// repository of packageType may be created on this instance exactly when a
// package-type slot is registered for it AND the license evaluation unlocks
// it (license.Manager.PackageTypeAvailable's spelling over registry-resolved
// id and tier — the mapping is registry data, the verdict is the Manager's).
// T-283 wires this into repo.Service's validation chain through the
// consumer-side seam; this function is the single spelling both sides share.
//
// A nil gate reads as the community floor: the five core slots answer true,
// every gated slot false — the pre-license instance's honest answer.
func (r *Registry) PackageTypeAvailable(ctx context.Context, gate Gate, packageType string) bool {
	a, ok := r.byPackage[packageType]
	if !ok {
		return false
	}
	if gate == nil {
		return a.MinTier <= license.TierCommunity
	}
	return gate.AddonEnabled(ctx, a.ID, a.MinTier)
}
