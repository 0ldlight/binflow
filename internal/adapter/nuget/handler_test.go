package nuget

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The endpoint matrix over the full middleware chain (the harness
// posture): every face of doc.go's layout table, happy paths and the
// error family the clients actually meet.

// TestServiceIndexResources: the index names the three resource families
// and the publish alias, with absolute URLs back into this instance.
func TestServiceIndexResources(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)

	status, body, _ := s.get(apiPath("ng-local") + "/index.json")
	if status != http.StatusOK {
		t.Fatalf("index status = %d, body %s", status, body)
	}
	var doc struct {
		Version   string `json:"version"`
		Resources []struct {
			ID   string `json:"@id"`
			Type string `json:"@type"`
		} `json:"resources"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("index body is not JSON: %v\n%s", err, body)
	}
	if doc.Version != "3.0.0" {
		t.Errorf("index version = %q, want 3.0.0", doc.Version)
	}
	wantTypes := map[string]bool{
		"SearchQueryService":         false,
		"SearchQueryService/3.5.0":   false,
		"RegistrationsBaseUrl":       false,
		"RegistrationsBaseUrl/3.6.0": false,
		"PackageBaseAddress/3.0.0":   false,
		"PackagePublish/2.0.0":       false,
		"LegacyGallery":              false,
	}
	ids := map[string]string{}
	for _, r := range doc.Resources {
		if _, ok := wantTypes[r.Type]; ok {
			wantTypes[r.Type] = true
			ids[r.Type] = r.ID
		}
	}
	for typ, seen := range wantTypes {
		if !seen {
			t.Errorf("index misses resource %s", typ)
		}
	}
	// The bases: trailing slash on the storage families, none on publish,
	// and every URL roots at this server's origin.
	if !strings.HasSuffix(ids["PackageBaseAddress/3.0.0"], "/api/nuget/v3/ng-local/flatcontainer/") {
		t.Errorf("flatcontainer base = %q", ids["PackageBaseAddress/3.0.0"])
	}
	if !strings.HasSuffix(ids["PackagePublish/2.0.0"], "/api/nuget/v3/ng-local/flatcontainer") ||
		strings.HasSuffix(ids["PackagePublish/2.0.0"], "flatcontainer/") {
		t.Errorf("publish base = %q (want the flatcontainer base without the trailing slash)", ids["PackagePublish/2.0.0"])
	}
}

// TestPushDownloadRoundtrip: the full local lifecycle — push (201), the
// three package files, the versions document, the registration, and the
// DELETE.
func TestPushDownloadRoundtrip(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)

	pkg := buildNupkg(t, "Demo.Pkg", "1.2.3", flatDeps("none"))
	status, body, _ := s.put(pushPath("ng-local", "demo.pkg", "1.2.3"), pkg.body, nil)
	if status != http.StatusCreated {
		t.Fatalf("push status = %d, body %s", status, body)
	}

	// The nupkg byte-for-byte.
	status, body, hdr := s.get(packagePath("ng-local", "demo.pkg", "1.2.3", "nupkg"))
	if status != http.StatusOK || body != string(pkg.body) {
		t.Fatalf("nupkg GET = %d (len %d, want %d)", status, len(body), len(pkg.body))
	}
	if ct := hdr.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("nupkg content-type = %q", ct)
	}

	// The .sha512 sidecar matches the measured digest.
	status, body, _ = s.get(packagePath("ng-local", "demo.pkg", "1.2.3", "sha512"))
	if status != http.StatusOK || body != pkg.sha512 {
		t.Fatalf("sha512 GET = (%d, %q), want %q", status, body, pkg.sha512)
	}

	// The nuspec sidecar is the extracted manifest.
	status, body, _ = s.get(packagePath("ng-local", "demo.pkg", "1.2.3", "nuspec"))
	if status != http.StatusOK || !strings.Contains(body, "<id>Demo.Pkg</id>") {
		t.Fatalf("nuspec GET = (%d, %q…)", status, firstLine(body))
	}

	// The versions document.
	status, body, _ = s.get(apiPath("ng-local") + "/flatcontainer/demo.pkg/index.json")
	if status != http.StatusOK {
		t.Fatalf("versions status = %d, body %s", status, body)
	}
	var vd versionsDocument
	if err := json.Unmarshal([]byte(body), &vd); err != nil {
		t.Fatalf("versions body: %v", err)
	}
	if len(vd.Versions) != 1 || vd.Versions[0] != "1.2.3" {
		t.Fatalf("versions = %v, want [1.2.3]", vd.Versions)
	}

	// The registration: one inline page, the leaf keyed by the normalized
	// lowercase spelling, the hash and the dependency group present.
	status, body, _ = s.get(apiPath("ng-local") + "/registration/demo.pkg/index.json")
	if status != http.StatusOK {
		t.Fatalf("registration status = %d, body %s", status, body)
	}
	var reg registrationIndex
	if err := json.Unmarshal([]byte(body), &reg); err != nil {
		t.Fatalf("registration body: %v", err)
	}
	if reg.Count != 1 || len(reg.Items) != 1 || len(reg.Items[0].Items) != 1 {
		t.Fatalf("registration shape: %s", body)
	}
	leaf := reg.Items[0].Items[0]
	if leaf.CatalogEntry.ID != "Demo.Pkg" || leaf.CatalogEntry.Version != "1.2.3" {
		t.Errorf("catalog id/version = %q/%q", leaf.CatalogEntry.ID, leaf.CatalogEntry.Version)
	}
	if leaf.CatalogEntry.PackageHash != pkg.sha512 {
		t.Errorf("packageHash = %q, want the measured digest", leaf.CatalogEntry.PackageHash)
	}
	if !strings.Contains(leaf.PackageContent, "/flatcontainer/demo.pkg/1.2.3/demo.pkg.1.2.3.nupkg") {
		t.Errorf("packageContent = %q", leaf.PackageContent)
	}

	// DELETE removes the version directory (the trio goes together).
	status, _, _ = s.delete(pushPath("ng-local", "demo.pkg", "1.2.3"))
	if status != http.StatusNoContent {
		t.Fatalf("delete status = %d", status)
	}
	status, _, _ = s.get(packagePath("ng-local", "demo.pkg", "1.2.3", "nupkg"))
	if status != http.StatusNotFound {
		t.Errorf("post-delete nupkg GET = %d, want 404", status)
	}
}

// TestPushValidationRefusals: the validation chain's 400 family — the
// nuspec identity mismatches and the not-a-zip body.
func TestPushValidationRefusals(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)

	cases := []struct {
		name    string
		id      string
		version string
		body    func() []byte
	}{
		{"not a zip", "bad.pkg", "1.0.0", func() []byte { return []byte("certainly not a zip") }},
		{"id mismatch", "other.pkg", "1.0.0", func() []byte { return buildNupkg(t, "Different.Pkg", "1.0.0", flatDeps("none")).body }},
		{"version mismatch", "demo.pkg", "2.0.0", func() []byte { return buildNupkg(t, "Demo.Pkg", "1.0.0", flatDeps("none")).body }},
	}
	for _, tc := range cases {
		status, body, _ := s.put(pushPath("ng-local", tc.id, tc.version), tc.body(), nil)
		if status != http.StatusBadRequest {
			t.Errorf("%s: status = %d (body %s), want 400", tc.name, status, body)
		}
	}
	// Nothing landed from any refused push.
	status, body, _ := s.get(apiPath("ng-local") + "/flatcontainer/demo.pkg/index.json")
	if status != http.StatusOK || strings.Count(body, "1.0.0") != 0 {
		t.Errorf("refused pushes leaked versions: (%d, %s)", status, body)
	}
}

// TestPushRemoteRefused: a push onto a REMOTE repository never reaches
// the validation chain — the service's read-only door owns it.
func TestPushRemoteRefused(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)
	pkg := buildNupkg(t, "Demo.Pkg", "1.0.0", flatDeps("none"))
	status, body, _ := s.put(pushPath("ng-remote", "demo.pkg", "1.0.0"), pkg.body, nil)
	if status != http.StatusMethodNotAllowed && status != http.StatusBadRequest {
		t.Fatalf("remote push status = %d (body %s), want the read-only refusal family", status, body)
	}
}

// TestVersionNormalization: a 3-part push target with a 2-part nuspec
// version normalizes to the same storage key on both faces.
func TestVersionNormalization(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)

	// nuspec says 1.2, the push target says 1.2.0 — the normalized key.
	pkg := buildNupkg(t, "Norm.Pkg", "1.2", flatDeps("none"))
	status, body, _ := s.put(pushPath("ng-local", "norm.pkg", "1.2.0"), pkg.body, nil)
	if status != http.StatusCreated {
		t.Fatalf("push status = %d, body %s", status, body)
	}
	status, body, _ = s.get(apiPath("ng-local") + "/flatcontainer/norm.pkg/index.json")
	if status != http.StatusOK || !strings.Contains(body, `"1.2.0"`) {
		t.Fatalf("normalized versions = (%d, %s)", status, body)
	}
}

// TestSearchFaces: the minimal query set — the empty q, the substring q,
// the prerelease filter and the take window.
func TestSearchFaces(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)

	for _, p := range []struct {
		id, version string
	}{
		{"Alpha.Lib", "1.0.0"},
		{"Alpha.Extra", "2.0.0-beta1"},
		{"Beta.Tool", "3.1.4"},
	} {
		pkg := buildNupkg(t, p.id, p.version, flatDeps("none"))
		if status, body, _ := s.put(pushPath("ng-local", lowerASCII(p.id), p.version), pkg.body, nil); status != http.StatusCreated {
			t.Fatalf("push %s: %d %s", p.id, status, body)
		}
	}

	get := func(q string) (int, searchResponse) {
		status, body, _ := s.get(apiPath("ng-local") + "/query" + q)
		var doc searchResponse
		if err := json.Unmarshal([]byte(body), &doc); err != nil && status == http.StatusOK {
			t.Fatalf("search body: %v (%s)", err, body)
		}
		return status, doc
	}

	// The default (stable-only) view: the beta-only package is filtered.
	status, doc := get("")
	if status != http.StatusOK || doc.TotalHits != 2 {
		t.Fatalf("empty q: (%d, total %d), want 2 stable hits", status, doc.TotalHits)
	}
	_, doc = get("?q=alpha")
	if doc.TotalHits != 1 {
		t.Fatalf("q=alpha stable total = %d, want 1", doc.TotalHits)
	}
	_, doc = get("?q=alpha&prerelease=true")
	if doc.TotalHits != 2 {
		t.Fatalf("q=alpha prerelease total = %d, want 2", doc.TotalHits)
	}
	// The stable-only view drops the beta-only package entirely; the
	// prerelease view surfaces it with the beta as its best version.
	_, doc = get("?q=extra")
	if doc.TotalHits != 0 {
		t.Fatalf("q=extra stable total = %d, want 0", doc.TotalHits)
	}
	_, doc = get("?q=extra&prerelease=true")
	if doc.TotalHits != 1 || len(doc.Data) != 1 || doc.Data[0].Version != "2.0.0-beta1" {
		t.Fatalf("q=extra prerelease = (total %d), want 1 with the beta best", doc.TotalHits)
	}
	_, doc = get("?take=1")
	if len(doc.Data) != 1 || doc.TotalHits != 2 {
		t.Errorf("take=1: data %d / total %d", len(doc.Data), doc.TotalHits)
	}
	_, doc = get("?prerelease=true&take=10")
	if doc.TotalHits != 3 {
		t.Errorf("prerelease total = %d, want 3", doc.TotalHits)
	}
}

// TestSha512Synthesis: a package landed through the BARE content plane
// (no push sidecars) still serves a correct .sha512 — the synthesis
// fallback off the stored blob.
func TestSha512Synthesis(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)

	pkg := buildNupkg(t, "Bare.Pkg", "1.0.0", flatDeps("none"))
	status, body, _ := s.put("/binflow/ng-local/bare.pkg/1.0.0/bare.pkg.1.0.0.nupkg", pkg.body, nil)
	if status != http.StatusCreated {
		t.Fatalf("bare PUT status = %d, body %s", status, body)
	}
	status, body, _ = s.get(packagePath("ng-local", "bare.pkg", "1.0.0", "sha512"))
	if status != http.StatusOK || body != pkg.sha512 {
		t.Fatalf("synthesized sha512 = (%d, %q), want %q", status, body, pkg.sha512)
	}
}

// TestErrorFamily: the unknown-path 404, the unknown repo 404 and the
// method refusals with Allow headers.
func TestErrorFamily(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)

	if status, _, _ := s.get(apiPath("ng-local") + "/nope"); status != http.StatusNotFound {
		t.Errorf("unknown v3 path = %d, want 404", status)
	}
	if status, _, _ := s.get(apiPath("ng-missing") + "/index.json"); status != http.StatusNotFound {
		t.Errorf("unknown repo = %d, want 404", status)
	}
	req, _ := http.NewRequest(http.MethodPost, s.srv.URL+apiPath("ng-local")+"/query", nil)
	req.SetBasicAuth(adminUser, adminPass)
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST query: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST query = %d, want 405", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); !strings.Contains(allow, "GET") {
		t.Errorf("POST query Allow = %q", allow)
	}
}

// TestTraversalRefused: dot-segment spellings die at the layout door
// (decoded form judged — the FR-4-AC10 family).
func TestTraversalRefused(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)

	for _, p := range []string{
		apiPath("ng-local") + "/flatcontainer/..%2F..%2Fetc/index.json",
		apiPath("ng-local") + "/flatcontainer/a/../b/index.json",
		apiPath("ng-local") + "/flatcontainer//demo.pkg/index.json",
	} {
		if status, _, _ := s.get(p); status != http.StatusBadRequest && status != http.StatusNotFound {
			t.Errorf("%s = %d, want the 400/404 refusal family", p, status)
		}
	}
}

// firstLine is the log-line helper.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
