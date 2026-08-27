package conan

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The license-gating integration legs (the T-285/T-287/T-294 verification
// form, through the conan adapter's own surface):
//
//   - D3: community tier (no document) refuses the conan repository
//     create with the configuration-plane 400 naming the addon and tier;
//   - a pro document installed through the REAL license.Manager (test
//     keypair through the constructor seam) flips create to 200 and the
//     upload to 201;
//   - uninstall returns the write face to the gated refusal (403 +
//     X-Binflow-License-Required) while reads keep serving (D1).

// newLicensedStack assembles the gated stack.
func newLicensedStack(t *testing.T) (*stack, *licenseKeys) {
	t.Helper()
	keys := newLicenseKeys(t)
	s := newStackOpt(t, stackOptions{addons: newAddonsSeam(), keys: keys, anonymous: true})
	if s.license == nil {
		t.Fatal("licensed stack built without a Manager")
	}
	return s, keys
}

// createConanRepo issues the REST repository create (the D3 surface).
func (s *stack) createConanRepo(t *testing.T, key, rclass string) (int, string) {
	t.Helper()
	body := fmt.Sprintf(`{"key":%q,"rclass":%q,"packageType":"conan"}`, key, rclass)
	status, respBody, _ := s.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass,
		strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	return status, respBody
}

// TestGateD3CommunityRefusesCreate: no document installed → the conan
// create answers the D3 400 naming the addon and the tier gap.
func TestGateD3CommunityRefusesCreate(t *testing.T) {
	s, _ := newLicensedStack(t)
	status, body := s.createConanRepo(t, "conan-local", "local")
	if status != http.StatusBadRequest {
		t.Fatalf("community conan create status = %d, want 400 (body %s)", status, body)
	}
	for _, token := range []string{"conan", "pro"} {
		if !strings.Contains(body, token) {
			t.Errorf("D3 body %q does not name %q", body, token)
		}
	}
}

// TestGateProLifecycle: install pro → create 200 + upload 201; uninstall
// → upload 403 with the X-Binflow-License-Required marker, reads keep
// serving (D1 + D2), create returns to the D3 400.
func TestGateProLifecycle(t *testing.T) {
	s, keys := newLicensedStack(t)
	ctx := context.Background()

	if status, _ := s.createConanRepo(t, "conan-local", "local"); status != http.StatusBadRequest {
		t.Fatalf("pre-install create status = %d, want 400", status)
	}

	if _, err := s.license.Install(ctx, proDoc(t, keys)); err != nil {
		t.Fatalf("install pro: %v", err)
	}
	if st := s.license.State(); !st.Licensed || st.Tier != license.TierPro {
		t.Fatalf("licensed state = %+v, want pro", st)
	}
	if status, body := s.createConanRepo(t, "conan-local", "local"); status != http.StatusOK {
		t.Fatalf("pro create status = %d, want 200 (body %s)", status, body)
	}

	r := ref{name: "gated", version: "0.1.0", user: "u", channel: "c"}
	if status, body, _ := s.putRecipeFile("conan-local", r, fixtureRev(1), "conanfile.py", []byte("x")); status != http.StatusCreated {
		t.Fatalf("pro upload status = %d, want 201 (body %s)", status, body)
	}

	// Uninstall: writes gated, reads keep serving (D1 + D2).
	if err := s.license.Uninstall(ctx); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	status, body, hdr := s.putRecipeFile("conan-local", r, fixtureRev(2), "conanfile.py", []byte("y"))
	if status != http.StatusForbidden {
		t.Fatalf("post-uninstall upload status = %d, want 403 (body %s)", status, body)
	}
	if got := hdr.Get("X-Binflow-License-Required"); got != "conan" {
		t.Errorf("X-Binflow-License-Required = %q, want conan", got)
	}
	status, gotBody, _ := s.get(v2("conan-local", "gated/0.1.0/u/c/revisions/"+fixtureRev(1)+"/files/conanfile.py"))
	if status != http.StatusOK || gotBody != "x" {
		t.Errorf("post-uninstall read = (%d, %q), want the served copy", status, gotBody)
	}
	status, gotBody, _ = s.get(v2("conan-local", "gated/0.1.0/u/c/latest"))
	if status != http.StatusOK {
		t.Errorf("post-uninstall latest = (%d, %s), want D1 keep-serving", status, gotBody)
	}
	if status, b2 := s.createConanRepo(t, "conan-other", "local"); status != http.StatusBadRequest {
		t.Errorf("post-uninstall create status = %d, want 400 (body %s)", status, b2)
	}
}

// TestProviderRegistration: the metadata provider classifies the index
// documents as regenerable metadata and the content trees as content, and
// maps the storage grammar onto the upstream v2 wire grammar (the remote
// hop's translation facet).
func TestProviderRegistration(t *testing.T) {
	if _, ok := parseRouteProbe(); !ok {
		t.Fatal("provider probe failed")
	}
	p := provider{}
	if got := p.Classify("u/n/1.0/c/index.json"); got != "metadata" {
		t.Errorf("Classify(index.json) = %v", got)
	}
	if got := p.Classify("u/n/1.0/c/abc/.timestamp"); got != "metadata" {
		t.Errorf("Classify(.timestamp) = %v", got)
	}
	if got := p.Classify("u/n/1.0/c/abc/.files.json"); got != "metadata" {
		t.Errorf("Classify(.files.json) = %v", got)
	}
	if got := p.Classify("u/n/1.0/c/abc/.search.json"); got != "metadata" {
		t.Errorf("Classify(.search.json) = %v", got)
	}
	if got := p.Classify("u/n/1.0/c/abc/export/conanfile.py"); got != "content" {
		t.Errorf("Classify(export file) = %v", got)
	}
	if got := p.Classify("u/n/1.0/c/abc/package/pid/prev/conan_package.tgz"); got != "content" {
		t.Errorf("Classify(package file) = %v", got)
	}
	if name, ok := p.PackageName("u/n/1.0/c/abc/export/conanfile.py"); !ok || name != "u/n/1.0/c" {
		t.Errorf("PackageName = (%q, %v)", name, ok)
	}
	if _, ok := p.PackageName("stray.txt"); ok {
		t.Error("PackageName accepted a non-coordinate path")
	}
}

// TestProviderUpstreamPath pins the storage -> upstream wire grammar: the
// coordinate order swaps, the index documents become their revisions
// endpoints, and the two markers become the listing/search endpoints.
func TestProviderUpstreamPath(t *testing.T) {
	rrev, pid, prev := "ab"+strings.Repeat("0", 62), "cd"+strings.Repeat("0", 38), "ef"+strings.Repeat("0", 62)
	cases := []struct{ storage, wire string }{
		// recipe plane
		{"myuser/hello/1.0/stable/index.json", "v2/conans/hello/1.0/myuser/stable/revisions"},
		{"myuser/hello/1.0/stable/" + rrev + "/.files.json", "v2/conans/hello/1.0/myuser/stable/revisions/" + rrev + "/files"},
		{"myuser/hello/1.0/stable/" + rrev + "/.search.json", "v2/conans/hello/1.0/myuser/stable/revisions/" + rrev + "/search"},
		{"myuser/hello/1.0/stable/" + rrev + "/export/conanfile.py", "v2/conans/hello/1.0/myuser/stable/revisions/" + rrev + "/files/conanfile.py"},
		{"myuser/hello/1.0/stable/" + rrev + "/export/sub/dir/patch.diff", "v2/conans/hello/1.0/myuser/stable/revisions/" + rrev + "/files/sub/dir/patch.diff"},
		// package plane
		{"myuser/hello/1.0/stable/" + rrev + "/package/" + pid + "/index.json",
			"v2/conans/hello/1.0/myuser/stable/revisions/" + rrev + "/packages/" + pid + "/revisions"},
		{"myuser/hello/1.0/stable/" + rrev + "/package/" + pid + "/" + prev + "/.files.json",
			"v2/conans/hello/1.0/myuser/stable/revisions/" + rrev + "/packages/" + pid + "/revisions/" + prev + "/files"},
		{"myuser/hello/1.0/stable/" + rrev + "/package/" + pid + "/" + prev + "/conan_package.tgz",
			"v2/conans/hello/1.0/myuser/stable/revisions/" + rrev + "/packages/" + pid + "/revisions/" + prev + "/files/conan_package.tgz"},
		// unknown shapes pass through (the generic posture)
		{"stray.txt", "stray.txt"},
		{"myuser/hello/1.0/stable/" + rrev + "/.timestamp", "myuser/hello/1.0/stable/" + rrev + "/.timestamp"},
		{"u/n/1.0/c/XYZ/not-a-revision/export/f", "u/n/1.0/c/XYZ/not-a-revision/export/f"},
	}
	p := provider{}
	for _, tc := range cases {
		if got := p.UpstreamPath(tc.storage); got != tc.wire {
			t.Errorf("UpstreamPath(%q) = %q, want %q", tc.storage, got, tc.wire)
		}
	}
}

// parseRouteProbe is a compile-time canary for the route grammar's
// availability (the provider legs above ride the same package).
func parseRouteProbe() (route, bool) { return parseRoute("v2/conans/search") }

// TestRepoTypesServedClasses: the adapter's declared classes (spec section
// 7 — local in full, remote pull-through, virtual aggregation).
func TestRepoTypesServedClasses(t *testing.T) {
	h := New(nil, nil, nil, nil, Options{})
	types := h.RepoTypes()
	want := []string{repo.TypeLocal, repo.TypeRemote, repo.TypeVirtual}
	if len(types) != len(want) {
		t.Fatalf("RepoTypes = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Errorf("RepoTypes[%d] = %q, want %q", i, types[i], want[i])
		}
	}
}
