// L026-6 (D08-R03): the Distribution store face's wire legs (p11/p44-p49
// + p48b, release-bundle.md §10.4/§10.7) — the ordered arm chain (admin
// door, projectKey passthrough, the null-JWS 500 leak, the INVALID_RB_REPO
// double-key envelope, the default system-repo provisioning side effect,
// the bare JWS-parse 500, the signature 400), the CORS preflight, the
// auto-created repository's shape, and its list-hidden/single-visible
// posture on the repositories plane.

package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/bundle"
	"github.com/lzwzzy/binflow/internal/license"
)

// TestBundleStoreArmChain: §10.4's ordered ladder, first failure shorting.
func TestBundleStoreArmChain(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	admin := adminP()

	// Arm 1 (§10.7 p52): the non-admin caller meets the bare Forbidden
	// envelope before anything else.
	code, body, _ := st.do(t, http.MethodPut, "release/store", `{}`, &auth.Principal{Name: "dev"})
	if code != http.StatusForbidden || !strings.Contains(body, `"message": "Forbidden"`) {
		t.Fatalf("non-admin store = %d %s, want the bare 403", code, body)
	}

	// Arm 2 (p49): a projectKey names a project BinFlow never carries —
	// the Access passthrough copy with the caller's key in backticks.
	code, body, _ = st.do(t, http.MethodPut, "release/store?projectKey=l026nope",
		`{"signedJwsBundle":"garbage"}`, admin)
	if code != http.StatusNotFound ||
		!strings.Contains(body, "HTTP response status 404:Failed to execute add project resource with error Could not find project `l026nope`") {
		t.Fatalf("projectKey arm = %d %s, want the passthrough 404", code, body)
	}

	// Arm 3 (p46): a non-JSON body answers the Jackson copy.
	code, body, _ = st.do(t, http.MethodPut, "release/store", "not json", admin)
	if code != http.StatusBadRequest || !strings.Contains(body, "Unrecognized token 'not'") {
		t.Fatalf("jackson arm = %d %s, want the 400 copy", code, body)
	}

	// Arm 4 (p45): a body without signedJwsBundle 500s — the reference's
	// null-annotation leak, not a 400.
	code, body, _ = st.do(t, http.MethodPut, "release/store", `{}`, admin)
	if code != http.StatusInternalServerError || !strings.Contains(body, "jwsString is marked non-null but is null") {
		t.Fatalf("null JWS arm = %d %s, want the 500 leak copy", code, body)
	}

	// Arm 5 (p44): an explicit storingRepo that is not a release-bundle
	// repository answers the DOUBLE-KEY envelope.
	code, body, hdr := st.do(t, http.MethodPut, "release/store",
		`{"signedJwsBundle":"garbage","storingRepo":"libs","artifactMapping":{}}`, admin)
	if code != http.StatusBadRequest {
		t.Fatalf("invalid storingRepo = %d %s, want 400", code, body)
	}
	for _, want := range []string{
		`"reason": "INVALID_RB_REPO"`, `"message": "Invalid release bundle repository"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("double-key body %q must contain %q", body, want)
		}
	}
	if ct := hdr.Get("Content-Type"); ct != "application/json" {
		t.Errorf("double-key content type = %q, want application/json", ct)
	}
	// An unknown storingRepo key answers the same envelope (sub-arm
	// unprobed, family-consistent).
	code, body, _ = st.do(t, http.MethodPut, "release/store",
		`{"signedJwsBundle":"garbage","storingRepo":"no-such-repo"}`, admin)
	if code != http.StatusBadRequest || !strings.Contains(body, "INVALID_RB_REPO") {
		t.Fatalf("unknown storingRepo = %d %s, want the same envelope", code, body)
	}

	// Arm 6 (p48): the default storing repository resolves to
	// release-bundles, the provisioning side effect fires, and a garbage
	// JWS answers the BARE 500 (no "Failed to parse JWS. " prefix — the
	// two-port asymmetry).
	code, body, _ = st.do(t, http.MethodPut, "release/store", `{"signedJwsBundle":"garbage"}`, admin)
	if code != http.StatusInternalServerError ||
		!strings.Contains(body, "Invalid serialized unsecured/JWS/JWE object: Missing part delimiters") {
		t.Fatalf("default repo garbage JWS = %d %s, want the bare 500", code, body)
	}
	if strings.Contains(body, "Failed to parse JWS.") {
		t.Errorf("store arm must NOT carry the transaction family's prefix: %s", body)
	}

	// The side effect (p48b): the system repository exists with the wire
	// shape, answers its single GET, and stays out of the LIST.
	ctx := context.Background()
	row, err := st.md.Repos().Get(ctx, bundle.SystemRepoKey)
	if err != nil {
		t.Fatalf("system repo after store: %v", err)
	}
	if row.Type != "local" || row.PackageType != bundle.PackageTypeReleaseBundles {
		t.Fatalf("system row = %+v, want local/%s", row, bundle.PackageTypeReleaseBundles)
	}
	code, body, _ = st.do(t, http.MethodGet, "repositories/"+bundle.SystemRepoKey, "", admin)
	if code != http.StatusOK {
		t.Fatalf("single GET of the system repo = %d %s, want 200 (p48b)", code, body)
	}
	code, body, _ = st.do(t, http.MethodGet, "repositories", "", admin)
	if code != http.StatusOK || strings.Contains(body, bundle.SystemRepoKey) {
		t.Fatalf("list = %d …, want release-bundles hidden (p48b)", code)
	}
	if !strings.Contains(body, `"libs"`) {
		t.Fatalf("list lost the ordinary repositories: %s", body)
	}

	// Arm 7: a WELL-FORMED JWS lands on the signature-validation refusal —
	// the terminal arm without Distribution keys (the 202/200/409
	// tri-state behind it is unreachable, registered UNKNOWN).
	code, body, _ = st.do(t, http.MethodPut, "release/store",
		`{"signedJwsBundle":"eyJhbGciOiJQUzI1NiJ9.eyJzdWIiOiJyZWxlYXNlLWJ1bmRsZSJ9.dGVzdF9zaWduYXR1cmU"}`, admin)
	if code != http.StatusBadRequest || !strings.Contains(body, "Failed validating release bundle signature") {
		t.Fatalf("well-formed JWS = %d %s, want the signature 400", code, body)
	}

	// The addon gate sits behind the admin door: community admin answers
	// the license 403 with the header.
	stCommunity := newT513Stack(t, fakeLicenseEval{tier: license.TierCommunity, licensed: true})
	code, _, hdr = stCommunity.do(t, http.MethodPut, "release/store", `{"signedJwsBundle":"garbage"}`, adminP())
	if code != http.StatusForbidden || hdr.Get("X-Binflow-License-Required") != BundleAddonID {
		t.Fatalf("community store = %d, want the gated 403 + header", code)
	}
}

// TestBundleStoreOptions: p11 — the CORS preflight is unauthenticated,
// answers 200 text/plain "OPTIONS, PUT" and the Allow header.
func TestBundleStoreOptions(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	code, body, hdr := st.do(t, http.MethodOptions, "release/store", "", nil)
	if code != http.StatusOK {
		t.Fatalf("OPTIONS = %d, want 200", code)
	}
	if strings.TrimSpace(body) != "OPTIONS, PUT" {
		t.Fatalf("OPTIONS body = %q, want %q", body, "OPTIONS, PUT")
	}
	if allow := hdr.Get("Allow"); allow != "OPTIONS,PUT" {
		t.Fatalf("Allow = %q, want OPTIONS,PUT", allow)
	}
}
