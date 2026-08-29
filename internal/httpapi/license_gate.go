// The entitlement gate weaves (M10 T-283, ADR-0032 / architecture section
// 15.1.5): the license verdict consulted at the three EXISTING decision
// seams — the content write verbs (weave 1), the repo configuration plane
// (weave 2, inside repo.Service through the PackageTypeGate seam) and the
// feature-addon handler first line (weave 3, RequireAddon). NOT a
// middleware: the verdict is data-, verb- and state-dependent, and the /v2
// root exception would walk around any prefix map (a gate that cannot catch
// the bypasses is not a gate) — the section 7.2 chain order is untouched,
// every gate sits strictly AFTER the RBAC door.
//
// Degradation shapes rendered here (ADR-0032's closed set):
//
//	D2  gated write verb  -> 403, errors[] envelope
//	     `license required: addon '<id>' needs tier '<t>' (current: <tier|none>)`
//	     + response header X-Binflow-License-Required: <addonID> (the
//	     programmable marker; the dispatch note's "license.addon.gated 头"
//	     spelling is read as THIS header — the ADR is the authority).
//	D1  reads NEVER consult the gate — data is not hostage to entitlement,
//	     and SERVER-INTERNAL writes (the remote pull-through's landing, the
//	     metadata calculators) are exempt by construction: the gate is the
//	     HTTP verb face's, nothing behind it re-asks the question.
//	D4  feature addons gate through RequireAddon, the same 403 form.

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/repo"
)

// headerLicenseRequired is D2's programmable marker (ADR-0032): the response
// header a denied gated write carries so console and CI can branch on the
// entitlement refusal without parsing the envelope. The value is the addon
// id. The addons.disabled breaker shape deliberately does NOT set it —
// installing a license would not clear that refusal, and a header named
// "license required" must never lie about the remedy.
const headerLicenseRequired = "X-Binflow-License-Required"

// The addon-gate counter's decision label values (metrics.go consumes).
const (
	gateDecisionAllow = "allow"
	gateDecisionDeny  = "deny"
)

// addonAllowed consults the license facet for one slot. A missing facet (no
// license collaborator mounted, or a bare fake without the gate arm — every
// pre-M10 unit stack) evaluates the COMMUNITY FLOOR: floor slots pass, gated
// slots refuse — the honest answer of an unlicensed instance, and the same
// evaluation addons.Registry.PackageTypeAvailable gives a nil gate.
func (s *Server) addonAllowed(ctx context.Context, id string, minTier license.Tier) bool {
	if s.addonsEval != nil {
		return s.addonsEval.AddonEnabled(ctx, id, minTier)
	}
	return minTier <= license.TierCommunity
}

// addonDisabled probes the addons.disabled breaker shape (view.go's
// statusOf probe): a slot the floor tier would unlock but the Manager still
// refuses is switched off by configuration. With no facet mounted nothing is
// ever disabled.
func (s *Server) addonDisabled(ctx context.Context, id string) bool {
	if s.addonsEval == nil {
		return false
	}
	return !s.addonsEval.AddonEnabled(ctx, id, license.TierCommunity)
}

// currentTier renders the D2 message's "(current: …)" clause: the effective
// tier of a licensed instance, "none" when no document is in force.
func (s *Server) currentTier() string {
	if s.addonsEval != nil {
		if st := s.addonsEval.State(); st.Licensed {
			return st.Tier.String()
		}
	}
	return "none"
}

// gateAddonWrite is weave point 1 (section 15.1.5's dispatchContent inner):
// the content-plane license gate, consulted ONLY on write verbs and only
// for a package type the assembled registry carries a slot for. It runs
// after the repository row lookup (an unauthorized principal was already
// stopped at the RBAC door; the gate never leaks row existence) and before
// the adapter dispatch. false means the request was refused.
func (s *Server) gateAddonWrite(w http.ResponseWriter, r *http.Request, repoKey, packageType string) bool {
	if s.deps.Addons == nil {
		return true // pre-M10 unit stack: no slots, no question
	}
	a, ok := s.deps.Addons.ForPackageType(packageType)
	if !ok {
		// No slot serves the type: not an addon question. Unreachable for
		// mounted adapters (New's assembly guard), reachable for a seeded
		// row of an unassembled type — the adapter-missing 501 owns it.
		return true
	}
	if s.addonAllowed(r.Context(), a.ID, a.MinTier) {
		s.countAddonGate(a.ID, gateDecisionAllow)
		return true
	}
	s.countAddonGate(a.ID, gateDecisionDeny)
	s.denyAddon(w, r, a.ID, a.MinTier, repoKey, contentAuditPath(r, repoKey))
	return false
}

// denyAddon renders the uniform gated refusal (D2/D4's 403), records the
// license.addon.denied audit row and is the single spelling of the wire
// form — the content face, the /v2 face and RequireAddon all land here, so
// the envelope, the header and the audit detail cannot drift apart.
func (s *Server) denyAddon(w http.ResponseWriter, r *http.Request, id string, minTier license.Tier, repoKey, nodePath string) {
	detail, err := json.Marshal(map[string]string{
		"addon": id,
		"tier":  s.currentTier(),
		"need":  minTier.String(),
	})
	if err != nil {
		detail = []byte("{}") // unreachable: a flat string map always marshals
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actorName(r),
		Action: repo.AuditActionAddonDenied,
		Repo:   repoKey,
		Path:   nodePath,
		Detail: string(detail),
	})
	if s.addonDisabled(r.Context(), id) {
		// PRD 85.3's breaker shape: name the knob and the recovery path;
		// restart-effective by design.
		writeError(w, http.StatusForbidden, fmt.Sprintf(
			"addon '%s' is disabled by configuration (addons.disabled); remove the entry and restart to restore it", id))
		return
	}
	w.Header().Set(headerLicenseRequired, id)
	if st := s.licenseState(); st.Licensed && st.Tier >= minTier {
		// The tier is sufficient but the document's explicit addon
		// allowlist narrowed the grant (section 15.2.3's third clause) —
		// saying "needs tier" here would be a lie the operator cannot act
		// on.
		writeError(w, http.StatusForbidden, fmt.Sprintf(
			"license required: addon '%s' is not named in the license addon allowlist (current: %s)", id, st.Tier.String()))
		return
	}
	writeError(w, http.StatusForbidden, fmt.Sprintf(
		"license required: addon '%s' needs tier '%s' (current: %s)", id, minTier.String(), s.currentTier()))
}

// licenseState is the snapshot read helper (the community floor for a
// facet-less stack — an unlicensed instance's honest state).
func (s *Server) licenseState() license.State {
	if s.addonsEval != nil {
		return s.addonsEval.State()
	}
	return license.State{Tier: license.TierCommunity, Perpetual: true}
}

// RequireAddon is weave point 3 — the feature-addon REST handler's first
// line (section 15.1.5's sketch: `if !license.Require(w, r, "ha",
// license.TierEnterprise) { return }`). M10 ships the seam only: the
// properties feature's own gate lands with T-286 and the first feature
// bodies (ha, xray-integration) are M11+ — but every future consumer gates
// through THIS method so the 403 form stays the closed set's D4, identical
// to the data plane's D2. It answers whether the handler may proceed and
// renders the refusal itself.
func (s *Server) RequireAddon(w http.ResponseWriter, r *http.Request, id string, minTier license.Tier) bool {
	if s.addonAllowed(r.Context(), id, minTier) {
		s.countAddonGate(id, gateDecisionAllow)
		return true
	}
	s.countAddonGate(id, gateDecisionDeny)
	s.denyAddon(w, r, id, minTier, "", "")
	return false
}

// contentAuditPath derives the repo-relative node path of a content request
// for the audit row (the /binflow/<repoKey>/<path> spelling the adapter
// receives after prefix stripping; approximate by construction — audit is
// an observer, never a router).
func contentAuditPath(r *http.Request, repoKey string) string {
	rest := strings.TrimPrefix(r.URL.Path, prefix+"/"+repoKey)
	return strings.TrimPrefix(rest, "/")
}

// gateV2Write is the /v2 root exception's arm of weave 1 (ADR-0010 clause
// 1: the registry plane bypasses the /binflow prefix, so the gate must live
// in the /v2 dispatch itself or the docker slot would be ungated — the
// "middleware cannot catch the bypasses" argument pointed at this exact
// seam). Reads pass (D1); the token endpoint is an AUTH plane, never a
// content write; single-segment paths are the adapter's 404 shape and skip
// the question. The gate consults the REPOSITORY ROW's package type, not a
// hardcoded "docker" (T-342/HL-3): the plane serves the registry-v2 family,
// and a helmoci repository's writes must gate on the helmoci slot — with
// the row shape, a docker repository keeps the docker floor slot verbatim
// (identical verdicts, identical audit labels) while the family's gated
// members get their own. A row the lookup cannot resolve passes through:
// the adapter's own gate answers the spec-body 404/500, and the license
// question never leaks repository existence (the same ordering the RBAC
// door upstream upholds).
func (s *Server) gateV2Write(w http.ResponseWriter, r *http.Request, escapedPath string) bool {
	switch r.Method {
	case http.MethodPut, http.MethodPost, http.MethodPatch, http.MethodDelete:
	default:
		return true // D1: the registry plane's reads pass
	}
	if escapedPath == "/v2/token" {
		return true // the auth plane: POST /v2/token is a login, not a write
	}
	if s.deps.Addons == nil {
		return true
	}
	rest := strings.TrimPrefix(escapedPath, "/v2/")
	repoKey, tail, _ := strings.Cut(rest, "/")
	if repoKey == "" || tail == "" {
		return true
	}
	row, err := s.deps.Repos.Get(r.Context(), repoKey)
	if err != nil || row == nil {
		// A miss or a lookup fault is not an entitlement verdict: the
		// adapter's repo gate renders the honest spec body right behind
		// this point (NAME_UNKNOWN for the miss, the 500 for the fault).
		return true
	}
	return s.gateAddonWrite(w, r, repoKey, row.PackageType)
}
