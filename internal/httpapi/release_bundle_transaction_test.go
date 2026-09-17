// L026-6 (D08-R02): the v2-signing transaction family's wire legs (p38-p43
// + p54, release-bundle.md §10.3/§10.7) — the jose wrapper's prefixed
// parse arm, the open face's Jackson and parse arms, both close faces'
// path-echo not-found copy, the status face's admin door with its bare
// "Forbidden" envelope, and the signature-validation terminal arm a
// well-formed JWS honestly reaches.

package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/license"
)

// TestBundleTransactionErrorArms: the family's probed error ladder.
func TestBundleTransactionErrorArms(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	admin := adminP()

	tests := []struct {
		name   string
		method string
		rest   string
		body   string
		want   int
		word   string
	}{
		// p38: the jose wrapper prefixes the parse copy.
		{"wrapper parse", http.MethodPost, "release/bundle/transaction", "not-a-jws",
			400, "Failed to parse JWS. Invalid serialized unsecured/JWS/JWE object: Missing part delimiters"},
		// p39: the open face's JSON body with a garbage JWS — same copy,
		// same prefix.
		{"open parse", http.MethodPost, "release/bundle/transaction/open", `{"signedJwsBundle":"garbage"}`,
			400, "Failed to parse JWS. Invalid serialized unsecured/JWS/JWE object: Missing part delimiters"},
		// p40: a non-JSON body answers the Jackson copy byte-for-byte.
		{"open jackson", http.MethodPost, "release/bundle/transaction/open", "not json",
			400, "Unrecognized token 'not': was expecting (JSON String, Number, Array, Object or token 'null', 'true' or 'false')"},
		// p41: the sync close consumes the transaction id, echoes the rest.
		{"close echo", http.MethodPost, "release/bundle/transaction/close/nonexistent/tx/path", "",
			400, "Release bundle tx/path not found"},
		// p42: the async close — same face, same echo.
		{"async close echo", http.MethodPost, "release/bundle/transaction/async/close/nonexistent/tx/path", "",
			400, "Release bundle tx/path not found"},
		// p43: the status face — same echo again.
		{"status echo", http.MethodGet, "release/bundle/transaction/async/close/status/nonexistent/tx/path", "",
			400, "Release bundle tx/path not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body, _ := st.do(t, tt.method, tt.rest, tt.body, admin)
			if code != tt.want {
				t.Fatalf("%s %s = %d %s, want %d", tt.method, tt.rest, code, body, tt.want)
			}
			if !strings.Contains(body, tt.word) {
				t.Errorf("body %q must contain %q", body, tt.word)
			}
		})
	}

	// A WELL-FORMED JWS (three base64url segments) passes the form check
	// and lands on the honest signature-validation refusal — the family's
	// terminal arm on an instance without Distribution keys.
	wellFormed := "eyJhbGciOiJQUzI1NiJ9.eyJzdWIiOiJyZWxlYXNlLWJ1bmRsZSJ9.dGVzdF9zaWduYXR1cmU"
	code, body, _ := st.do(t, http.MethodPost, "release/bundle/transaction", wellFormed, admin)
	if code != 400 || !strings.Contains(body, "Failed validating release bundle signature") {
		t.Fatalf("well-formed wrapper JWS = %d %s, want the signature 400", code, body)
	}
	code, body, _ = st.do(t, http.MethodPost, "release/bundle/transaction/open",
		`{"signedJwsBundle":"`+wellFormed+`"}`, admin)
	if code != 400 || !strings.Contains(body, "Failed validating release bundle signature") {
		t.Fatalf("well-formed open JWS = %d %s, want the signature 400", code, body)
	}
}

// TestBundleTransactionStatusAdminGate: p54 — a non-admin caller meets the
// family's BARE "Forbidden" envelope (not the platform manage-gate
// wording) BEFORE any path evaluation.
func TestBundleTransactionStatusAdminGate(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	code, body, _ := st.do(t, http.MethodGet,
		"release/bundle/transaction/async/close/status/nonexistent/tx/path", "",
		&auth.Principal{Name: "dev"})
	if code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, want 403", code)
	}
	if !strings.Contains(body, `"message": "Forbidden"`) || strings.Contains(body, "administrator") {
		t.Fatalf("non-admin status body = %s, want the bare Forbidden envelope", body)
	}
}

// TestBundleTransactionAddonGate: the family's write entrances sit behind
// the feature gate — community answers the license 403 with the header.
func TestBundleTransactionAddonGate(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierCommunity, licensed: true})
	for _, rest := range []string{
		"release/bundle/transaction",
		"release/bundle/transaction/open",
		"release/bundle/transaction/close/x",
	} {
		code, _, hdr := st.do(t, http.MethodPost, rest, "x", adminP())
		if code != http.StatusForbidden || hdr.Get("X-Binflow-License-Required") != BundleAddonID {
			t.Fatalf("community %s = %d, want the gated 403 + header", rest, code)
		}
	}
}
