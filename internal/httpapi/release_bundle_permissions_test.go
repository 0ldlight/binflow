// L026-6 (D08-R06): the family's permission matrix legs (p50-p63,
// release-bundle.md §10.7) — the plain user's read reach and validation
// reach, the anonymous 401 door across the family (BinFlow's platform-wide
// 401 wording divergence noted in the ticket report), ANY DISTRIBUTION's
// non-REST-entity ruling held (the single query stays the platform's 404
// "Not Found", never a fabricated preset row), and the distribution rclass
// REST-create refusal verbatim.

package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/license"
)

// TestReleaseBundleUserMatrix: p50/p51 — a plain user (no grants) reads the
// names face (200, user role allowed) and reaches the assembly face's
// validation arm (400, not 403). The admin faces (store/config/status/
// fat_manifest) refuse with the bare envelope — their own test files pin
// those arms.
func TestReleaseBundleUserMatrix(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	dev := &auth.Principal{Name: "dev"}

	code, body, _ := st.do(t, http.MethodGet, "release/bundles", "", dev)
	if code != http.StatusOK || !strings.Contains(body, `"bundles": {}`) {
		t.Fatalf("user list = %d %s, want 200 (p50)", code, body)
	}
	code, body, _ = st.do(t, http.MethodPost, "release/bundle", `{}`, dev)
	if code != http.StatusBadRequest || !strings.Contains(body, "Request is invalid. Missing AQL query") {
		t.Fatalf("user assembly = %d %s, want the validation 400 (p51)", code, body)
	}
}

// TestReleaseBundleAnonymousDoor: p56-p58 — anonymous meets the 401 door on
// every face of the family. BinFlow's 401 message is the platform's frozen
// "authentication required" (the reference's "Authentication is required"
// is a platform-wide divergence registered in the ticket report, not this
// family's to change).
func TestReleaseBundleAnonymousDoor(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	for _, tt := range []struct {
		method string
		rest   string
		body   string
	}{
		{http.MethodPost, "release/bundle", `{}`},
		{http.MethodGet, "release/bundles", ""},
		{http.MethodPut, "release/store", `{}`},
		{http.MethodGet, "release/bundles/config", ""},
		{http.MethodGet, "release/fat_manifest_content/x", ""},
		{http.MethodGet, "v2/release_bundle/names", ""},
	} {
		code, _, _ := st.do(t, tt.method, tt.rest, tt.body, nil)
		if code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s %s = %d, want 401", tt.method, tt.rest, code)
		}
	}
	// The OPTIONS preflight is the one unauthenticated verb (p11).
	if code, _, _ := st.do(t, http.MethodOptions, "release/store", "", nil); code != http.StatusOK {
		t.Fatalf("anonymous OPTIONS = %d, want 200 (preflight semantics)", code)
	}
}

// TestAnyDistributionNonRestEntity: §10.7's ruling — ANY DISTRIBUTION is an
// authorization-fallback constant, never a REST permission-target
// resource. The single query stays the platform's 404 "Not Found" (p62's
// aligned arm); the list face keeps its stored-targets shape (the presets
// divergence is pre-existing, registered — T-253/T-254 froze it).
func TestAnyDistributionNonRestEntity(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	admin := adminP()

	code, body, _ := st.do(t, http.MethodGet, "security/permissions/Any%20Distribution", "", admin)
	if code != http.StatusNotFound || !strings.Contains(body, `"message": "Not Found"`) {
		t.Fatalf("Any Distribution single query = %d %s, want the 404 (p62)", code, body)
	}
	// The channel itself still authorizes reads (the constant works inside
	// the fallback chain even though no REST row exists) — pinned by the
	// gate matrix in bundle_wire_test.go.
}

// TestDistributionRclassRefused: p63 — the distribution rclass is a real
// enum value the REST create face refuses, with the verbatim copy (the
// pre-check fires before the key-exists and package-type questions).
func TestDistributionRclassRefused(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	admin := adminP()

	code, body, _ := st.do(t, http.MethodPut, "repositories/l026r-d08-dist",
		`{"rclass":"distribution","url":"http://172.16.58.130:8082/distribution/api/v1","packageType":"generic"}`, admin)
	if code != http.StatusBadRequest {
		t.Fatalf("distribution create = %d %s, want 400", code, body)
	}
	if !strings.Contains(body, "Unsupported repository type 'distribution' or media type 'application/json'") {
		t.Fatalf("distribution copy = %s, want the p63 verbatim", body)
	}
	// The refusal precedes the key question: an EXISTING key with the
	// distribution rclass answers the same copy, not the exists-400.
	code, body, _ = st.do(t, http.MethodPut, "repositories/libs", `{"rclass":"distribution"}`, admin)
	if code != http.StatusBadRequest || !strings.Contains(body, "Unsupported repository type 'distribution'") {
		t.Fatalf("distribution on existing key = %d %s, want the same verbatim copy", code, body)
	}
	// And the ordinary rclass families keep their own wording.
	if code, body, _ = st.do(t, http.MethodPut, "repositories/zzz-bogus", `{"rclass":"federated"}`, admin); code == 0 || strings.Contains(body, "Unsupported repository type") {
		t.Fatalf("federated rclass = %d %s — the pre-check must not swallow the ordinary families", code, body)
	}
}
