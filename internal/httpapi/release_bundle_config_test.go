// L026-6 (D08-R05): the config and fat_manifest faces' wire legs (p06,
// p47, p53, p55 — release-bundle.md §10.6/§10.7) — the 720 factory
// default, the PUT's 202 copy and the value's round trip, the admin doors
// with their bare "Forbidden" envelope, and the fat-manifest path-shape
// rejection.

package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/license"
)

// TestBundleConfigFace: GET answers the factory default; PUT answers the
// frozen copy under 202 and the value round-trips; the PUT validation
// keeps the trust boundary honest (BinFlow-native arms — unprobed).
func TestBundleConfigFace(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	admin := adminP()

	// p06: the factory default.
	code, body, _ := st.do(t, http.MethodGet, "release/bundles/config", "", admin)
	if code != http.StatusOK || !strings.Contains(body, `"incompleteCleanupPeriodHours": 720`) {
		t.Fatalf("config GET = %d %s, want the 720 default", code, body)
	}

	// The PUT: 202 with the frozen copy (text/plain), then the round trip.
	code, body, hdr := st.do(t, http.MethodPut, "release/bundles/config", `{"incompleteCleanupPeriodHours":48}`, admin)
	if code != http.StatusAccepted || strings.TrimSpace(body) != "Successfully updated release bundles config" {
		t.Fatalf("config PUT = %d %q, want 202 with the frozen copy", code, body)
	}
	if ct := hdr.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("config PUT content type = %q, want text/plain (CT unprobed, platform posture)", ct)
	}
	code, body, _ = st.do(t, http.MethodGet, "release/bundles/config", "", admin)
	if code != http.StatusOK || !strings.Contains(body, `"incompleteCleanupPeriodHours": 48`) {
		t.Fatalf("config after PUT = %d %s, want 48", code, body)
	}

	// Validation: a negative value and a missing key refuse (BinFlow-native
	// arms, unprobed on the wire).
	for _, b := range []string{`{"incompleteCleanupPeriodHours":-1}`, `{}`, `not json`} {
		if code, body, _ = st.do(t, http.MethodPut, "release/bundles/config", b, admin); code != http.StatusBadRequest {
			t.Fatalf("config PUT %s = %d %s, want 400", b, code, body)
		}
	}

	// The PUT sits behind the feature gate (community 403 + header).
	stCommunity := newT513Stack(t, fakeLicenseEval{tier: license.TierCommunity, licensed: true})
	code, _, hdr = stCommunity.do(t, http.MethodPut, "release/bundles/config", `{"incompleteCleanupPeriodHours":1}`, adminP())
	if code != http.StatusForbidden || hdr.Get("X-Binflow-License-Required") != BundleAddonID {
		t.Fatalf("community config PUT = %d, want the gated 403 + header", code)
	}
}

// TestBundleConfigAdminGate: p53 — the config GET's non-admin answer is
// the family's bare "Forbidden" envelope.
func TestBundleConfigAdminGate(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	code, body, _ := st.do(t, http.MethodGet, "release/bundles/config", "", &auth.Principal{Name: "dev"})
	if code != http.StatusForbidden || !strings.Contains(body, `"message": "Forbidden"`) {
		t.Fatalf("non-admin config = %d %s, want the bare 403", code, body)
	}
}

// TestBundleFatManifest: the admin face's probed arms — the path-shape
// rejection echoes the caller's whole path (p47), the admin door renders
// the bare Forbidden envelope (p55), and a list.manifest.json path on an
// instance without v2 records answers the honest 404 (UNKNOWN sub-arm).
func TestBundleFatManifest(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	admin := adminP()

	code, body, _ := st.do(t, http.MethodGet, "release/fat_manifest_content/nonexistent/manifest.json", "", admin)
	if code != http.StatusBadRequest ||
		!strings.Contains(body, "Fat manifest content view is only allowed on list.manifest.json files. Got: [nonexistent/manifest.json]") {
		t.Fatalf("fat manifest reject = %d %s, want the p47 copy", code, body)
	}
	if code, _, _ = st.do(t, http.MethodGet, "release/fat_manifest_content/x/list.manifest.json", "", admin); code != http.StatusNotFound {
		t.Fatalf("fat manifest on an absent evidence file = %d, want the honest 404", code)
	}
	// p55: non-admin → the bare Forbidden envelope.
	if code, body, _ = st.do(t, http.MethodGet, "release/fat_manifest_content/x/list.manifest.json", "", &auth.Principal{Name: "dev"}); code != http.StatusForbidden || !strings.Contains(body, `"message": "Forbidden"`) {
		t.Fatalf("non-admin fat manifest = %d %s, want the bare 403", code, body)
	}
	// The addon gate rides behind the admin door.
	stCommunity := newT513Stack(t, fakeLicenseEval{tier: license.TierCommunity, licensed: true})
	if code, _, _ = stCommunity.do(t, http.MethodGet, "release/fat_manifest_content/x/manifest.json", "", adminP()); code != http.StatusForbidden {
		t.Fatalf("community fat manifest = %d, want the gated 403", code)
	}
}
