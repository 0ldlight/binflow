// License REST plane (M10 T-279, ADR-0032 / architecture section 15.1.4):
// the install/query/uninstall surface over license.Manager.
//
//	GET    /binflow/api/system/license   CapSystemRead
//	POST   /binflow/api/system/license   CapSystemWrite  body = document text
//	DELETE /binflow/api/system/license   CapSystemWrite
//
// Shape rulings (ADR over PRD, per the T-293 registered divergences):
//   - the path is /api/system/license (SINGULAR — single instance, single
//     document; Artifactory's plural + activate/licenseChanged is HA
//     multi-license semantics, deliberately not emulated);
//   - a successful install answers 201 (a resource came into force);
//   - D6 has no grace period (the PRD Q6 30-day grace was not adopted);
//   - the GET body follows the section 15.1.4 field list verbatim: no
//     "source" field (licensed carries the fact), and NO document echo —
//     neither the payload segment nor the signature ever rides a response
//     or a log line (NFR-S52).
//
// Errors use the errors[] envelope. Verification failures carry the two
// wire codes of D7 inside the message: LICENSE_EXPIRED for the time-window
// family, LICENSE_INVALID for everything else, with no internal check
// detail (FR-84-AC3).

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// licenseMaxBodyBytes caps the POST body: a v1 document is a few hundred
// bytes, so 64 KiB is orders of magnitude of headroom while still a bound
// (an unbounded read of "the license" is a DoS surface with no upside).
const licenseMaxBodyBytes = 64 << 10

// licenseRemovedText is the DELETE response body (section 15.1.4).
const licenseRemovedText = "License removed successfully."

// LicenseManager is the entitlement seam behind the three endpoints (the
// Manager's management-plane facet; the gate facet AddonEnabled is consumed
// by the T-283 weave points, not here).
type LicenseManager interface {
	Install(ctx context.Context, doc string) (license.State, error)
	Uninstall(ctx context.Context) error
	State() license.State
}

// licenseStatusBody is the GET/POST-success body (section 15.1.4). Every
// time field is RFC3339 UTC or empty; daysToExpiry is null for a perpetual
// license or the unlicensed floor and otherwise the whole days remaining
// (negative once past expiry — "expired N days ago" is operator signal).
// addons is the DOCUMENT's explicit allowlist (null = tier-wide unlock):
// the effective per-addon view is GET /api/v1/addons (T-282), which joins
// this state with the registry.
type licenseStatusBody struct {
	Licensed     bool            `json:"licensed"`
	Tier         string          `json:"tier"`
	LicenseID    string          `json:"licenseId"`
	Licensee     string          `json:"licensee"`
	IssuedAt     string          `json:"issuedAt"`
	NotBefore    string          `json:"notBefore"`
	ExpiresAt    string          `json:"expiresAt"`
	Perpetual    bool            `json:"perpetual"`
	DaysToExpiry *int            `json:"daysToExpiry"`
	Addons       []string        `json:"addons"`
	Limits       json.RawMessage `json:"limits"`
}

// handleLicenseGet answers the effective state. A stack assembled without
// the license collaborator still answers the FLOOR state honestly (the
// Docs-handler precedent: product self-description, not an error) — only
// the mutating verbs demand the manager.
func (s *Server) handleLicenseGet(w http.ResponseWriter, _ *http.Request) {
	st := license.State{Tier: license.TierCommunity, Perpetual: true}
	if s.deps.License != nil {
		st = s.deps.License.State()
	}
	writeJSONBody(w, http.StatusOK, licenseStatusFromState(st))
}

// handleLicenseInstall verifies and installs the posted document. The full
// chain must pass before anything is replaced (D7): a refusal answers 400
// and leaves the current license exactly in force.
func (s *Server) handleLicenseInstall(w http.ResponseWriter, r *http.Request) {
	if s.deps.License == nil {
		writeError(w, http.StatusServiceUnavailable, "license manager is not available on this instance")
		return
	}
	body := http.MaxBytesReader(w, r.Body, licenseMaxBodyBytes)
	raw, err := io.ReadAll(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "license document body could not be read (max 64 KiB)")
		return
	}
	doc := string(raw)

	st, err := s.deps.License.Install(r.Context(), doc)
	if err != nil {
		if !license.IsVerificationError(err) {
			// A persistence failure is not a document verdict: no
			// license.invalid event, no LICENSE_* code — the 5xx store
			// family (busy first, then the honest 500).
			s.log.ErrorContext(r.Context(), "httpapi: license install persist failed",
				"error", err.Error())
			if metadata.IsStoreBusy(err) {
				writeError(w, http.StatusServiceUnavailable, "license store is busy, retry")
				return
			}
			writeError(w, http.StatusInternalServerError, "license install failed")
			return
		}
		// The two wire codes of D7, and nothing internal (FR-84-AC3):
		// the time-window family is operator-actionable, every other
		// failure collapses to LICENSE_INVALID.
		var msg string
		switch {
		case errors.Is(err, license.ErrExpired):
			msg = "license install rejected: LICENSE_EXPIRED — document is expired"
		case errors.Is(err, license.ErrNotYetValid):
			msg = "license install rejected: LICENSE_EXPIRED — document is not valid yet"
		default:
			msg = "license install rejected: LICENSE_INVALID — verification failed"
		}
		s.log.WarnContext(r.Context(), "httpapi: license install rejected",
			"reason", licenseReasonClass(err))
		s.audit.Record(r.Context(), audit.Event{
			Actor:  actorName(r),
			Action: license.ActionInvalid,
			Detail: licenseInstallRejectDetail(err),
		})
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	s.log.InfoContext(r.Context(), "httpapi: license installed", "tier", st.Tier.String())
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actorName(r),
		Action: license.ActionInstall,
		Detail: licenseTransitionDetail(st),
	})
	writeJSONBody(w, http.StatusCreated, licenseStatusFromState(st))
}

// handleLicenseDelete uninstalls: the community floor takes over
// immediately, existing artifacts stay readable (D1) and gated write faces
// close (D2/D3 — T-283's weave). Idempotent: deleting an unlicensed
// instance keeps the 200.
func (s *Server) handleLicenseDelete(w http.ResponseWriter, r *http.Request) {
	if s.deps.License == nil {
		writeError(w, http.StatusServiceUnavailable, "license manager is not available on this instance")
		return
	}
	prev := s.deps.License.State()
	if err := s.deps.License.Uninstall(r.Context()); err != nil {
		s.log.ErrorContext(r.Context(), "httpapi: license uninstall failed", "error", err.Error())
		if metadata.IsStoreBusy(err) {
			writeError(w, http.StatusServiceUnavailable, "license store is busy, retry")
			return
		}
		writeError(w, http.StatusInternalServerError, "license uninstall failed")
		return
	}
	if prev.Licensed {
		// The uninstall of an unlicensed instance is a no-op floor-keeper:
		// audited only when a license actually left.
		s.audit.Record(r.Context(), audit.Event{
			Actor:  actorName(r),
			Action: license.ActionDelete,
			Detail: licenseTransitionDetail(prev),
		})
	}
	s.log.InfoContext(r.Context(), "httpapi: license removed")
	writePlainText(w, http.StatusOK, licenseRemovedText)
}

// licenseStatusFromState projects the Manager snapshot onto the wire body.
func licenseStatusFromState(st license.State) licenseStatusBody {
	b := licenseStatusBody{
		Licensed:  st.Licensed,
		Tier:      st.Tier.String(),
		LicenseID: st.LicenseID,
		Licensee:  st.Licensee,
		Perpetual: st.Perpetual,
		Addons:    st.AddonAllowlist,
		Limits:    st.Limits,
	}
	if !st.IssuedAt.IsZero() {
		b.IssuedAt = st.IssuedAt.Format(time.RFC3339)
	}
	if !st.NotBefore.IsZero() {
		b.NotBefore = st.NotBefore.Format(time.RFC3339)
	}
	if !st.ExpiresAt.IsZero() {
		b.ExpiresAt = st.ExpiresAt.Format(time.RFC3339)
		days := int(time.Until(st.ExpiresAt).Hours() / 24)
		b.DaysToExpiry = &days
	}
	return b
}

// licenseReasonClass is the wire-safe failure category for logs (the audit
// actor for these events comes from the shared actorName helper).
func licenseReasonClass(err error) string {
	var msg string
	switch {
	case errors.Is(err, license.ErrExpired):
		msg = "expired"
	case errors.Is(err, license.ErrNotYetValid):
		msg = "not_yet_valid"
	case errors.Is(err, license.ErrBadDocument):
		msg = "malformed"
	case errors.Is(err, license.ErrBadHeader):
		msg = "header"
	case errors.Is(err, license.ErrUnknownKey):
		msg = "unknown_key"
	case errors.Is(err, license.ErrBadSignature):
		msg = "signature"
	default:
		msg = "invalid"
	}
	return msg
}

// licenseInstallRejectDetail builds the license.invalid detail for a
// refused install: {reason} only — the rejected document's own fields are
// untrusted input and never echo into the trail.
func licenseInstallRejectDetail(err error) string {
	return fmt.Sprintf(`{"reason":%q}`, licenseReasonClass(err))
}

// licenseTransitionDetail builds the install/delete detail: tier, licensee,
// licenseId and the expiry — the audit trail is the one place the licensee
// is allowed to persist (NFR-S52 restricts LOGS; the trail is the security
// record and ADR-0032 names these fields for it).
func licenseTransitionDetail(st license.State) string {
	d := map[string]string{
		"tier":      st.Tier.String(),
		"licensee":  st.Licensee,
		"licenseId": st.LicenseID,
	}
	if st.Perpetual {
		d["expiresAt"] = "perpetual"
	} else if !st.ExpiresAt.IsZero() {
		d["expiresAt"] = st.ExpiresAt.Format(time.RFC3339)
	}
	b, err := json.Marshal(d)
	if err != nil {
		return "{}" // unreachable: a flat string map always marshals
	}
	return string(b)
}
