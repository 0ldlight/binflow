// L026-6 (D08-R05 companion): the v2 Release Lifecycle read plane's wire
// legs (p13-p26/p60, release-bundle.md §10.6) — the empty envelopes
// (names/received/records pagination), the received single GET's 恒 400
// validator copy, the two 404 record families with their store-key leaks
// (release-bundles-v2 and the -jfds twin), the audit family's seven-field
// validation and its 405, and the family catch-all.

package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/license"
)

// TestV2BundleReadFaces: the read plane's probed ladder.
func TestV2BundleReadFaces(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	admin := adminP()

	tests := []struct {
		name   string
		method string
		rest   string
		want   int
		word   string
	}{
		// The empty envelopes.
		{"names", http.MethodGet, "v2/release_bundle/names", 200, `"release_bundles": []`},
		{"received", http.MethodGet, "v2/release_bundle/received", 200, `"total": 0`},
		{"received versions", http.MethodGet, "v2/release_bundle/received/no-such", 200, `"versions": []`},
		{"records pagination", http.MethodGet, "v2/release_bundle/records/no-such", 200, `"limit": 1000`},
		// p20/p23: the received single GET is 恒 400 — the validator copy,
		// valid and invalid names alike.
		{"received single valid name", http.MethodGet, "v2/release_bundle/received/nosuchbundle/1.0", 400,
			"Bad request: [`Release Bundle name must begin with a {letter | _ | digit} and consist of {letters | _ | . | - | digits}`]"},
		{"received single hyphen name", http.MethodGet, "v2/release_bundle/received/no-such/1.0", 400,
			"Release Bundle name must begin with"},
		// p17/p24: the records single GET leaks the three-segment v2 layout
		// and the .evd evidence file name.
		{"records single", http.MethodGet, "v2/release_bundle/records/nosuchbundle/1.0", 404,
			"Path not found: release-bundles-v2/nosuchbundle/1.0/release-bundle.json.evd"},
		// p16/p25: the statuses face's record 404 over the plain store key.
		{"statuses", http.MethodGet, "v2/release_bundle/statuses/nosuchbundle/1.0.0-1", 404,
			"Record not found, repository: release-bundles-v2, name: nosuchbundle, version: 1.0.0-1"},
		// p60: the received DELETE leaks the -jfds twin store key.
		{"received delete", http.MethodDelete, "v2/release_bundle/received/nosuchbundle/1.0", 404,
			"Record not found, repository: release-bundles-v2-jfds, name: nosuchbundle, version: 1.0"},
		// p13 + the family catch-all: unrouted subpaths answer "Not Found".
		{"bare records", http.MethodGet, "v2/release_bundle/records", 404, `"message": "Not Found"`},
		{"unknown subpath", http.MethodGet, "v2/release_bundle/bogus", 404, `"message": "Not Found"`},
		{"deep tail", http.MethodGet, "v2/release_bundle/records/a/b/c", 404, `"message": "Not Found"`},
		{"statuses bare", http.MethodGet, "v2/release_bundle/statuses", 404, `"message": "Not Found"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body, _ := st.do(t, tt.method, tt.rest, "", admin)
			if code != tt.want {
				t.Fatalf("%s %s = %d %s, want %d", tt.method, tt.rest, code, body, tt.want)
			}
			if !strings.Contains(body, tt.word) {
				t.Errorf("body %q must contain %q", body, tt.word)
			}
			if strings.Contains(body, "null") {
				t.Errorf("body %q must not render null", body)
			}
		})
	}

	// The read faces demand authentication (the route door).
	if code, _, _ := st.do(t, http.MethodGet, "v2/release_bundle/names", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous v2 names = %d, want 401", code)
	}
}

// TestV2AuditFamily: the audit face — the seven-field validation copy in
// the probed order (p26), the 405 of every other verb (p22), and the
// honest E-26 refusal when the body passes validation (success shape
// UNKNOWN, registered).
func TestV2AuditFamily(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	admin := adminP()

	code, body, _ := st.do(t, http.MethodPost, "v2/audit", `{}`, admin)
	if code != http.StatusBadRequest {
		t.Fatalf("empty audit = %d %s, want 400", code, body)
	}
	want := "Bad request: [`'event_summary' must be non-null`, `'event_status' must be non-null`, " +
		"`'release_bundle_version' must be non-null`, `'subject_reference' must be non-null`, " +
		"`'release_bundle_name' must be non-null`, `'subject_type' must be non-null`, `'created_by' must be non-null`]"
	if !strings.Contains(body, want) {
		t.Errorf("audit validation = %s\nwant %s", body, want)
	}

	// A partially-filled body lists only its missing fields, same order.
	code, body, _ = st.do(t, http.MethodPost, "v2/audit", `{"event_summary":"s","created_by":"root"}`, admin)
	if code != http.StatusBadRequest ||
		!strings.Contains(body, "'event_status' must be non-null") ||
		strings.Contains(body, "'event_summary' must be non-null") ||
		strings.Contains(body, "'created_by' must be non-null") {
		t.Fatalf("partial audit = %d %s, want only the five missing fields", code, body)
	}

	// p22: GET answers the 405 (the family registers POST-only).
	if code, body, _ = st.do(t, http.MethodGet, "v2/audit?limit=1", "", admin); code != http.StatusMethodNotAllowed || !strings.Contains(body, "Method Not Allowed") {
		t.Fatalf("audit GET = %d %s, want the 405", code, body)
	}

	// A complete body: the success shape was never captured — the honest
	// E-26 refusal (registered UNKNOWN).
	code, body, _ = st.do(t, http.MethodPost, "v2/audit",
		`{"event_summary":"s","event_status":"DONE","release_bundle_version":"1.0","subject_reference":"r",`+
			`"release_bundle_name":"n","subject_type":"bundle","created_by":"root"}`, admin)
	if code != http.StatusNotFound || !strings.Contains(body, "not implemented") {
		t.Fatalf("complete audit = %d %s, want the E-26 404", code, body)
	}

	// The community gate (the audit POST is a write face of the family).
	stCommunity := newT513Stack(t, fakeLicenseEval{tier: license.TierCommunity, licensed: true})
	if code, _, _ = stCommunity.do(t, http.MethodPost, "v2/audit", `{}`, adminP()); code != http.StatusForbidden {
		t.Fatalf("community audit = %d, want the gated 403", code)
	}
	// … while the 405 arm is a routing fact — it fires for community too.
	if code, _, _ = stCommunity.do(t, http.MethodGet, "v2/audit", "", adminP()); code != http.StatusMethodNotAllowed {
		t.Fatalf("community audit GET = %d, want the 405 (routing fact)", code)
	}
}
