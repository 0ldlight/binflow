package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/lzwzzy/binflow/internal/search"
)

// The query rate limiter's admin REST face (M16 T-452, FR-148.2 / aql.md
// §14.4 — the whole section is the decompiled single source, official
// reference has no page; confidence medium, live arm V-l pending):
//
//	GET    /binflow/api/v1/system/query_rate_limiter/config
//	POST   /binflow/api/v1/system/query_rate_limiter/config
//	DELETE /binflow/api/v1/system/query_rate_limiter/config
//
// Wire, verbatim from the anchor: GET answers
// {"rlSettings":[{"rlType":"DEFAULT",...},{"rlType":"LOW_PRIORITY",...}]};
// POST is a merge-write answering the plain-text success copy; DELETE
// restores the factory state with its own copy. While the limiter is
// DISABLED every one of the three answers 400 plain text "Query rate
// limiter is disabled" — the anchor's factory posture.
//
// Two registered BinFlow mappings (in-ticket rulings the anchor explicitly
// leaves to T-452):
//
//   - THE ENABLE CARRIER. Artifactory flips the tri-state through the
//     restart-effective system.properties keys; BinFlow has no properties
//     plane (and this ticket's area adds no config keys), so the state
//     rides the persisted-by-REST document itself: a POST may carry a
//     "mode" field ("disabled" | "enabled" | "simulation"). The disabled
//     400 arm still applies to every request that does not carry an
//     explicit mode — the one deliberate wire extension, everything else
//     verbatim.
//   - THE ADMIN DOOR. Artifactory pins RolesAllowed(admin); BinFlow maps it
//     onto the v1-system-plane capability family — GET rides system:read,
//     POST/DELETE ride system:write (the license/settings family posture).
//     A plain user meets 403 on all three; the readonly_admin — a
//     BinFlow-native role — may read (registered divergence).
//
// The QRL throttles by DELAY and never rejects: the K63 gate's 429/408
// behavior is untouched in every state (aql.md §14.4's orthogonality note,
// pinned by TestQRLK63GateConsistency).
//
// PERSISTENCE NOTE (registered gap): Artifactory backs the custom config
// with the DB ("DB 有则回 DB 值"); BinFlow's document is process-lifetime
// (this ticket's area owns no metadata schema — a restart returns to the
// factory state). A DB-backed follow-up needs a migration + substore.

// The verbatim plain-text copies (aql.md §14.4's table).
const (
	msgQRLDisabled        = "Query rate limiter is disabled"
	msgQRLUpdated         = "Query rate limiter configuration was updated successfully"
	msgQRLDeleted         = "Query rate limiter configuration was deleted successfully"
	qrlConfigMaxBodyBytes = 1 << 20
)

// qrlConfigBody is the POST face: the official rlSettings array plus the
// BinFlow mode carrier (see the file comment). Both halves optional — the
// merge keeps whatever the request does not state.
type qrlConfigBody struct {
	Mode       *string             `json:"mode"`
	RLSettings []search.QRLSetting `json:"rlSettings"`
}

// qrlSettingsBody is the GET face: exactly the anchor's shape, nothing else.
type qrlSettingsBody struct {
	RLSettings []search.QRLSetting `json:"rlSettings"`
}

// writeQRLDisabledText answers the disabled arm's 400 plain-text copy.
func writeQRLDisabledText(w http.ResponseWriter) {
	writePlainText(w, http.StatusBadRequest, msgQRLDisabled)
}

// handleQRLConfigGet serves GET: disabled answers the verbatim 400; an
// active limiter answers the effective settings (defaults when nothing was
// ever written — the anchor's "DB 有则回 DB 值，无则回默认值" reading).
func (s *Server) handleQRLConfigGet(w http.ResponseWriter, _ *http.Request) {
	if s.qrl.Mode() == search.QRLModeDisabled {
		writeQRLDisabledText(w)
		return
	}
	writeJSONBody(w, http.StatusOK, qrlSettingsBody{RLSettings: s.qrl.Settings()})
}

// handleQRLConfigPost serves the merge-write. A request without a mode
// field on a disabled limiter keeps the anchor's 400 arm; an explicit mode
// is the enable/disable/simulation carrier (the file comment's ruling).
// Validation failures answer the errors[] 400 (the management-plane
// posture); success answers the verbatim plain-text copy.
func (s *Server) handleQRLConfigPost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, qrlConfigMaxBodyBytes+1))
	if err != nil || len(body) > qrlConfigMaxBodyBytes {
		writeError(w, http.StatusBadRequest, "Query rate limiter configuration body is unreadable or oversized.")
		return
	}
	var req qrlConfigBody
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "Query rate limiter configuration body is not valid JSON: "+err.Error())
			return
		}
	}
	if req.Mode == nil || *req.Mode == "" {
		if s.qrl.Mode() == search.QRLModeDisabled {
			writeQRLDisabledText(w)
			return
		}
	} else if err := search.ValidateQRLMode(*req.Mode); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := search.QRLConfig{Settings: req.RLSettings}
	if req.Mode != nil && *req.Mode != "" {
		cfg.Mode = search.QRLMode(*req.Mode)
	}
	if err := s.qrl.Configure(cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.log.InfoContext(r.Context(), "httpapi: query rate limiter configuration updated",
		"mode", s.qrl.Mode())
	writePlainText(w, http.StatusOK, msgQRLUpdated)
}

// handleQRLConfigDelete serves the reset verb: back to the factory state
// (disabled, default buckets), answering the verbatim copy. The anchor
// keeps the disabled 400 arm here too — deleting an already-disabled
// limiter is the same "feature off" verdict.
func (s *Server) handleQRLConfigDelete(w http.ResponseWriter, r *http.Request) {
	if s.qrl.Mode() == search.QRLModeDisabled {
		writeQRLDisabledText(w)
		return
	}
	s.qrl.Reset()
	s.log.InfoContext(r.Context(), "httpapi: query rate limiter configuration reset to factory state")
	writePlainText(w, http.StatusOK, msgQRLDeleted)
}
