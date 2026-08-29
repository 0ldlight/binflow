package helmoci

// The helmoci slot's three-seam gating legs (AC4's server-side half — the
// M10 dual-run posture applied to the T-342 slot), through the /v2 plane
// and the repo-create plane exactly as the conductor dispatch describes:
//
//   - community tier (no document): the helmoci repository create refuses
//     with the D3 400 naming the addon and the tier gap, and a /v2 WRITE
//     against a seeded helmoci repository answers the D2 403 with the
//     X-Binflow-License-Required marker — the /v2 arm resolves the
//     repository row's package type, so the write gates on the HELMOCI
//     slot, not the docker floor slot;
//   - a pro document installed through the REAL license.Manager flips
//     create to 200 and the /v2 push chain to 201;
//   - uninstall returns the write face to the gated 403 while reads keep
//     serving (D1) and create returns to the D3 400.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/repo"
)

// gatedManifestPUT is one /v2 manifest write against a seeded repository
// (the license gate sits in the dispatch, ahead of the adapter, so the
// body need not be a valid manifest for the gate question).
func (s *stack) gatedManifestPUT(repoKey string) (int, string, http.Header) {
	return s.put("/v2/"+repoKey+"/mychart/manifests/0.1.0", []byte(`{"schemaVersion":2}`),
		map[string]string{"Content-Type": mtOCIManifest})
}

// TestGateCommunitySeam: no document → create 400 (D3) and the /v2 write
// 403 (D2) with the helmoci marker, while the /v2 read of seeded content
// keeps serving (D1).
func TestGateCommunitySeam(t *testing.T) {
	s, _ := newLicensedStack(t)

	status, body := s.createHelmOCIRepo(t, "helmoci-local", "local")
	if status != http.StatusBadRequest {
		t.Fatalf("community helmoci create status = %d, want 400 (body %s)", status, body)
	}
	for _, token := range []string{"helmoci", "pro"} {
		if !strings.Contains(body, token) {
			t.Errorf("D3 body %q does not name %q", body, token)
		}
	}

	// Seeded row (the D3 refusal must not depend on the row's absence):
	// writes refuse on the helmoci slot, reads pass.
	s.seedRepo(t, "helmoci-seeded", repo.TypeLocal, Protocol)
	cfg := []byte(`{"name":"mychart","version":"0.1.0","apiVersion":"v2"}`)
	chart := []byte("chart")
	manifest := helmManifestBody(cfg, chart, nil)
	s.put("/v2/helmoci-seeded/mychart/manifests/0.1.0", manifest,
		map[string]string{"Content-Type": mtOCIManifest}) // gated → 403, asserted below

	status, body, hdr := s.gatedManifestPUT("helmoci-seeded")
	if status != http.StatusForbidden {
		t.Fatalf("community /v2 manifest PUT status = %d, want 403 (body %s)", status, body)
	}
	if got := hdr.Get("X-Binflow-License-Required"); got != "helmoci" {
		t.Errorf("X-Binflow-License-Required = %q, want helmoci", got)
	}
	// The blob-upload initiation — the other write verb of the push chain —
	// refuses identically.
	status, body, hdr = s.post("/v2/helmoci-seeded/mychart/blobs/uploads/", nil, nil)
	if status != http.StatusForbidden || hdr.Get("X-Binflow-License-Required") != "helmoci" {
		t.Errorf("community blob POST = (%d, %s, %q), want the helmoci 403", status, body, hdr.Get("X-Binflow-License-Required"))
	}
	// D1: the ping and the read plane never consult the gate.
	status, body, _ = s.get("/v2/helmoci-seeded/mychart/manifests/0.1.0")
	if status == http.StatusForbidden {
		t.Fatalf("community manifest GET refused (%s) — reads must pass (D1)", body)
	}
}

// TestGateProLifecycle: install pro → create 200 + the push chain 201;
// uninstall → the write face returns to the gated 403 while the pushed
// manifest keeps serving, and create returns to the D3 400.
func TestGateProLifecycle(t *testing.T) {
	s, keys := newLicensedStack(t)
	ctx := context.Background()

	if status, _ := s.createHelmOCIRepo(t, "helmoci-local", "local"); status != http.StatusBadRequest {
		t.Fatalf("pre-install create status = %d, want 400", status)
	}
	if _, err := s.license.Install(ctx, proDoc(t, keys)); err != nil {
		t.Fatalf("install pro: %v", err)
	}
	if st := s.license.State(); !st.Licensed || st.Tier != license.TierPro {
		t.Fatalf("licensed state = %+v, want pro", st)
	}
	if status, body := s.createHelmOCIRepo(t, "helmoci-local", "local"); status != http.StatusOK {
		t.Fatalf("pro create status = %d, want 200 (body %s)", status, body)
	}

	// The full push chain under the pro document: blobs, then the manifest.
	cfg := []byte(`{"name":"gated","version":"0.1.0","apiVersion":"v2"}`)
	chart := []byte("the gated chart")
	s.pushBlob(t, "gated", cfg, "")
	s.pushBlob(t, "gated", chart, "")
	manifest := helmManifestBody(cfg, chart, nil)
	status, body, _ := s.put("/v2/helmoci-local/gated/manifests/0.1.0", manifest,
		map[string]string{"Content-Type": mtOCIManifest})
	if status != http.StatusCreated {
		t.Fatalf("pro manifest PUT status = %d, want 201 (body %s)", status, body)
	}

	// Uninstall: writes gated, reads keep serving (D1 + D2).
	if err := s.license.Uninstall(ctx); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	status, body, hdr := s.gatedManifestPUT("helmoci-local")
	if status != http.StatusForbidden {
		t.Fatalf("post-uninstall manifest PUT status = %d, want 403 (body %s)", status, body)
	}
	if got := hdr.Get("X-Binflow-License-Required"); got != "helmoci" {
		t.Errorf("X-Binflow-License-Required = %q, want helmoci", got)
	}
	status, body, _ = s.get("/v2/helmoci-local/gated/manifests/0.1.0")
	if status != http.StatusOK || body != string(manifest) {
		t.Errorf("post-uninstall manifest GET = (%d, %d bytes), want the served copy (D1)", status, len(body))
	}
	if status, b2 := s.createHelmOCIRepo(t, "helmoci-other", "local"); status != http.StatusBadRequest {
		t.Errorf("post-uninstall create status = %d, want 400 (body %s)", status, b2)
	}
}

// TestGateDockerFloorUnchangedOnV2: the same /v2 write gate keeps its
// verdict for a DOCKER repository — the floor slot never refuses on tier
// (the row-resolved package type must not perturb the docker plane's
// gating, AC3's zero-regression clause at this seam).
func TestGateDockerFloorUnchangedOnV2(t *testing.T) {
	s, keys := newLicensedStack(t)
	ctx := context.Background()
	s.seedRepo(t, "docker-local", repo.TypeLocal, repo.PackageDocker)

	// Community (no document): the docker write passes the gate (floor
	// slot) and proceeds into the adapter's own validation — any status
	// other than the gated 403 proves the gate did not fire.
	status, body, hdr := s.put("/v2/docker-local/app/manifests/v1", []byte(`{"schemaVersion":2}`),
		map[string]string{"Content-Type": mtOCIManifest})
	if status == http.StatusForbidden {
		t.Fatalf("docker manifest PUT gated on community (%s %q) — the docker slot is a floor slot", body, hdr.Get("X-Binflow-License-Required"))
	}
	if got := hdr.Get("X-Binflow-License-Required"); got != "" {
		t.Errorf("docker write carries X-Binflow-License-Required = %q, want none", got)
	}

	// And under an expired pro document the docker plane still does not
	// consult the tier (only the addons.disabled breaker can refuse it —
	// not exercised here).
	if _, err := s.license.Install(ctx, proDoc(t, keys)); err != nil {
		t.Fatalf("install pro: %v", err)
	}
	s.seedRepo(t, "docker-two", repo.TypeLocal, repo.PackageDocker)
	status, _, hdr = s.put("/v2/docker-two/app/manifests/v1", []byte(`{"schemaVersion":2}`),
		map[string]string{"Content-Type": mtOCIManifest})
	if status == http.StatusForbidden || hdr.Get("X-Binflow-License-Required") != "" {
		t.Errorf("docker write under pro gated (%d, %q)", status, hdr.Get("X-Binflow-License-Required"))
	}
}
