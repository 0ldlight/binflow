// L026-6 (D08-R02): the release-bundle transaction family — the v2-signing
// entrance's ERROR face, wire-frozen by release-bundle.md §10.3 (p38-p43)
// and §1 rows 3-6. BinFlow carries no Distribution signing keys, so the
// family's terminal arm for a well-formed JWS is the reference's signature
// validation refusal; the 201 BundleTransactionResponse / the happy
// three-stage state machine stay UNKNOWN (the wire evidence ends at the
// parse arm too — the probe instance had no signing chain either).

package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode"
)

// The family's frozen copy (release-bundle.md §1/§10.3 — do not reword).
const (
	// msgJWSParsePrefix wraps the parse failure on the transaction/open
	// faces (p38/p39); the store face reuses the bare suffix only (p48 —
	// same cause, different code and wrapper, the two-port asymmetry).
	msgJWSParsePrefix = "Failed to parse JWS. "
	// msgJWSMissingDelimiters is the jose library's copy for a token with
	// no part delimiters (p38/p39/p48).
	msgJWSMissingDelimiters = "Invalid serialized unsecured/JWS/JWE object: Missing part delimiters"
	// msgRBSignatureInvalid is the decompiled signature-validation refusal
	// (§1 row 3's first 400) — the terminal arm of every signing entrance
	// on an instance without Distribution keys.
	msgRBSignatureInvalid = "Failed validating release bundle signature"
	// msgRBTransactionNotFound is the close/status family's not-found copy
	// with the client's transaction path echoed verbatim (p41-p43).
	msgRBTransactionNotFound = "Release bundle %s not found"
)

// jwsFormError validates the compact-serialization FORM only: a JWS is
// three dot-separated base64url segments (a JWE five). The probed evidence
// covers exactly one malformed shape — no delimiters at all (p38/p39/p48);
// every other non-well-formed spelling reuses the same frozen copy rather
// than inventing jose internals the wire never captured (UNKNOWN family
// reuse, registered in the ticket report). "" means the form is well-formed
// — nothing about the signature is claimed.
func jwsFormError(jws string) string {
	if !strings.Contains(jws, ".") {
		return msgJWSMissingDelimiters
	}
	parts := strings.Split(jws, ".")
	if len(parts) != 3 && len(parts) != 5 {
		return msgJWSMissingDelimiters
	}
	for _, p := range parts {
		if _, err := base64.RawURLEncoding.DecodeString(p); err != nil {
			return msgJWSMissingDelimiters
		}
	}
	return ""
}

// jacksonUnrecognizedToken renders the reference's Jackson parse-error copy
// for a body whose leading token JSON refuses (p40/p46, byte-frozen). The
// probed bodies both began with a bare word ("not json"); a body whose
// first character is structural (a malformed "{...") fails differently on
// Jackson and was never probed — ok=false sends the caller to the
// platform's own honest 400 wording instead of a fabricated copy.
func jacksonUnrecognizedToken(body []byte) (string, bool) {
	start := 0
	for start < len(body) && isJSONSpace(body[start]) {
		start++
	}
	if start >= len(body) || !isUnrecognizedTokenStart(rune(body[start])) {
		return "", false
	}
	end := start
	for end < len(body) && !isJSONDelimiter(body[end]) {
		end++
	}
	token := string(body[start:end])
	return "Unrecognized token '" + token + "': was expecting (JSON String, Number, Array, Object or token 'null', 'true' or 'false')" +
		"\n at [Source: REDACTED (`StreamReadFeature.INCLUDE_SOURCE_IN_LOCATION` disabled); line: 1, column: " +
		strconv.Itoa(start+1) + "]", true
}

// isUnrecognizedTokenStart reports whether c can begin a Jackson
// "unrecognized token" (a bare word — letters and the JS-ish sigils;
// digits, '-', '.' open JSON numbers and never reach this arm).
func isUnrecognizedTokenStart(c rune) bool {
	return unicode.IsLetter(c) || c == '_' || c == '$'
}

// isJSONSpace mirrors Jackson's whitespace set at the token level.
func isJSONSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

// isJSONDelimiter reports whether b ends an unrecognized-token run
// (structural characters, commas, colons and whitespace all delimit).
func isJSONDelimiter(b byte) bool {
	if isJSONSpace(b) {
		return true
	}
	switch b {
	case '{', '}', '[', ']', ',', ':':
		return true
	}
	return false
}

// handleBundleTransaction serves POST /api/release/bundle/transaction — the
// application/jose wrapper that forwards to open (§1 row 3): the parse arm
// carries the "Failed to parse JWS. " prefix (p38), then the signature arm.
func (s *Server) handleBundleTransaction(w http.ResponseWriter, r *http.Request) {
	if !s.requireBundleAddon(w, r) {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bundleMaxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "release bundle body could not be read: "+err.Error())
		return
	}
	if msg := jwsFormError(strings.TrimSpace(string(raw))); msg != "" {
		writeError(w, http.StatusBadRequest, msgJWSParsePrefix+msg)
		return
	}
	// No Distribution signing keys exist on BinFlow: signature validation
	// is honestly failed. The 201 open response is UNKNOWN territory (the
	// wire never captured it either — §8 #5).
	writeError(w, http.StatusBadRequest, msgRBSignatureInvalid)
}

// handleBundleTransactionOpen serves POST /api/release/bundle/transaction/
// open: a JSON body {"signedJwsBundle": "<jws>"}. A non-JSON body answers
// the Jackson copy (p40); a garbage JWS the prefixed parse arm (p39).
func (s *Server) handleBundleTransactionOpen(w http.ResponseWriter, r *http.Request) {
	if !s.requireBundleAddon(w, r) {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bundleMaxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "release bundle body could not be read: "+err.Error())
		return
	}
	var wire struct {
		SignedJwsBundle string `json:"signedJwsBundle"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		if msg, ok := jacksonUnrecognizedToken(raw); ok {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		writeError(w, http.StatusBadRequest, "release bundle body is not valid JSON: "+err.Error())
		return
	}
	// A missing/empty signedJwsBundle is unprobed on the wire; it takes the
	// parse arm of the empty token (no delimiters), the jose library's own
	// answer for an empty string (UNKNOWN sub-arm, family-consistent).
	if msg := jwsFormError(strings.TrimSpace(wire.SignedJwsBundle)); msg != "" {
		writeError(w, http.StatusBadRequest, msgJWSParsePrefix+msg)
		return
	}
	writeError(w, http.StatusBadRequest, msgRBSignatureInvalid)
}

// splitTransactionPath splits the tail after a close-family prefix the way
// the reference's route does (p41-p43): the FIRST segment is consumed by
// the route and the REMAINDER is the transaction path echoed back —
// close/nonexistent/tx/path answers "Release bundle tx/path not found".
func splitTransactionPath(tail string) string {
	_, rest, _ := strings.Cut(tail, "/")
	return rest
}

// handleBundleTransactionClose serves both POST close faces (the sync close
// and the async close, §1 rows 4-5): no transaction can ever exist on
// BinFlow (opening one requires the signing chain), so the family's whole
// reachable behavior is the not-found 400 with the echoed path.
func (s *Server) handleBundleTransactionClose(w http.ResponseWriter, r *http.Request, tail string) {
	if !s.requireBundleAddon(w, r) {
		return
	}
	writeError(w, http.StatusBadRequest, fmt.Sprintf(msgRBTransactionNotFound, splitTransactionPath(tail)))
}

// handleBundleTransactionCloseStatus serves GET /api/release/bundle/
// transaction/async/close/status/{path}: the ADMIN-gated progress probe
// (§10.7 p54: the non-admin answer is the family's bare "Forbidden"
// envelope), then the same not-found copy (p43).
func (s *Server) handleBundleTransactionCloseStatus(w http.ResponseWriter, r *http.Request, tail string) {
	if !bundleAdminGate(w, r) {
		return
	}
	writeError(w, http.StatusBadRequest, fmt.Sprintf(msgRBTransactionNotFound, splitTransactionPath(tail)))
}

// bundleAdminGate renders the release-bundle family's admin door — the bare
// "Forbidden" errors envelope of §10.7 (p52-p55), NOT the platform's
// manage-gate wording. The wire evidence covers plain users; the
// readonly_admin arm is unprobed and stays on the literal admin check.
func bundleAdminGate(w http.ResponseWriter, r *http.Request) bool {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "Forbidden")
		return false
	}
	return true
}
