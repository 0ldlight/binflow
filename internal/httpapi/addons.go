// Addon registry REST plane (M10 T-282, ADR-0033 / architecture section
// 15.2.5): the status visibility surface over the assembled addons.Registry
// joined with the license Manager.
//
//	GET /binflow/api/v1/addons   CapSystemRead
//
// The body is a bare array (the governance family's convention), one row per
// slot in ASSEMBLY order, carrying id/kind/minTier/enabled/reason/
// displayName/description. enabled and reason come from the SAME evaluation
// the enforcement gates consult (license.Manager.AddonEnabled through
// addons.Registry.Statuses) — visibility and execution cannot diverge (the
// ?permissions-view precedent). The PRD's state triple
// (unlocked/locked/disabled) rides enabled+reason: a disabled-config hit
// answers enabled=false with reason "disabled by configuration".
//
// Every other verb on the path falls to the E-26 404 (router.go); a stack
// assembled without the registry answers 200 with an EMPTY array — the
// product self-description of that assembly, the Docs-handler precedent —
// while a registry mounted without the license collaborator evaluates
// everything on the community floor.

package httpapi

import (
	"net/http"
)

// addonWire is one slot's wire row (section 15.2.5's field list; minTier
// renders through license.Tier.String, reason is omitted when empty).
type addonWire struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	MinTier     string `json:"minTier"`
	Enabled     bool   `json:"enabled"`
	Reason      string `json:"reason,omitempty"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

// handleAddonsList answers the full slot manifest with live evaluation.
func (s *Server) handleAddonsList(w http.ResponseWriter, r *http.Request) {
	if s.deps.Addons == nil {
		writeJSONBody(w, http.StatusOK, []addonWire{})
		return
	}
	rows := s.deps.Addons.Statuses(r.Context(), s.addonsEval)
	body := make([]addonWire, 0, len(rows))
	for _, row := range rows {
		body = append(body, addonWire{
			ID:          row.Addon.ID,
			Kind:        string(row.Addon.Kind),
			MinTier:     row.Addon.MinTier.String(),
			Enabled:     row.Enable,
			Reason:      row.Reason,
			DisplayName: row.Addon.DisplayName,
			Description: row.Addon.Description,
		})
	}
	writeJSONBody(w, http.StatusOK, body)
}

// addonPackageTypes renders the registry's package-type ids (the repo-create
// known-set's error wording — dynamic, so a newly assembled slot needs no
// message edit either).
func (s *Server) addonPackageTypes() []string {
	slots := s.deps.Addons.PackageTypeAddons()
	out := make([]string, 0, len(slots))
	for _, a := range slots {
		out = append(out, a.PackageType)
	}
	return out
}

// checkAddonPackageType is the repo-create plane's dynamic legality check
// (PRD FR-86.5's enum half): with the registry mounted, the set of legal
// package types IS the registry's package-type slot set — a newly registered
// slot is creatable the moment it is assembled, no branch in this file. The
// UNLOCK half of the gate (a known-but-locked slot's D3 refusal) is T-283's
// weave and deliberately absent here. false means the request was refused.
func (s *Server) checkAddonPackageType(w http.ResponseWriter, packageType string) bool {
	if s.deps.Addons == nil {
		return true // pre-M10 unit stack: repo.Service's static enum stays the only check
	}
	if _, known := s.deps.Addons.ForPackageType(packageType); known {
		return true
	}
	writeError(w, http.StatusBadRequest, unknownPackageTypeMessage(packageType, s.addonPackageTypes()))
	return false
}
