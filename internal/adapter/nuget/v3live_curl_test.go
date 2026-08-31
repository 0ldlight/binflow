package nuget

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// TestV3LiveCurlMatrix drives the REAL curl binary against the REAL
// assembled stack over a real socket — the ticket's client leg (no dotnet
// SDK or nuget.exe in this environment; the T-337 curl posture). The v3
// matrix: the three repository classes' search / download / registration
// faces, the dynamic service-index resolution (an upstream whose @id
// spellings are NOT nuget.org's), the semver2 shape, and the
// upstream-deleted-still-cached comparison. Every command and its answer
// is t.Log'd — the ticket log's verbatim record. Environment-gated:
// BINFLOW_T341_CURL_E2E=1.
func TestV3LiveCurlMatrix(t *testing.T) {
	if os.Getenv("BINFLOW_T341_CURL_E2E") != "1" {
		t.Skip("set BINFLOW_T341_CURL_E2E=1 (with curl on PATH) to run the v3 real-client matrix")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skipf("curl unavailable: %v", err)
	}

	// The stack: local + remote(fake upstream with a NON-nuget.org @id
	// spelling — the dynamic-resolution leg) + virtual(local+remote).
	s := newStack(t)
	s.seedRepo(t, "live-local", repo.TypeLocal)
	s.seedRepo(t, "live-remote", repo.TypeRemote)
	s.seedRepo(t, "live-virt", repo.TypeVirtual)

	const (
		regPath  = "custom/registration-hive"
		flatPath = "custom/packages"
	)
	pkg := buildNupkg(t, "Live.V3", "1.0.0", flatDeps("none"))
	pkgLocal := buildNupkg(t, "Local.Pkg", "1.0.0", flatDeps("none"))
	deleted := &atomic.Bool{}
	up := newFakeUpstream(t)
	up.serve(t, "/v3/index.json", []byte(fmt.Sprintf(
		`{"version":"3.0.0","resources":[`+
			`{"@id":"%s/%s/","@type":"RegistrationsBaseUrl"},`+
			`{"@id":"%s/%s/","@type":"RegistrationsBaseUrl/3.6.0"},`+
			`{"@id":"%s/%s/","@type":"PackageBaseAddress/3.0.0"},`+
			`{"@id":"%s/search","@type":"SearchQueryService"}]}`,
		up.srv.URL, regPath, up.srv.URL, regPath, up.srv.URL, flatPath, up.srv.URL)), "application/json")
	up.serve(t, "/search", []byte(fmt.Sprintf(
		`{"totalHits":1,"data":[{"id":"Live.V3","version":"1.0.0",`+
			`"versions":[{"version":"1.0.0"}],`+
			`"registration":"%s/%s/live.v3/index.json"}]}`,
		up.srv.URL, regPath)), "application/json")
	up.srv.Config.Handler.(*http.ServeMux).HandleFunc("/"+regPath+"/live.v3/index.json", func(w http.ResponseWriter, _ *http.Request) {
		if deleted.Load() {
			http.NotFound(w, nil)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(upstreamRegistrationAt(up.srv.URL, regPath, flatPath, "live.v3", "Live.V3", "1.0.0", pkg)) //nolint:gosec // live fixture
	})
	up.serve(t, "/"+flatPath+"/live.v3/index.json", []byte(`{"versions":["1.0.0"]}`), "application/json")
	up.serve(t, "/"+flatPath+"/live.v3/1.0.0/live.v3.1.0.0.nupkg", pkg.body, "application/octet-stream")
	s.seedRemoteConfig(t, "live-remote", up.srv.URL)
	s.seedVirtualMembers(t, "live-virt", "live-local", "live-remote")

	// The local fixture lands through the Go seam (curl's binary leg is
	// the read faces below).
	if status, body, _ := s.put(pushPath("live-local", "local.pkg", "1.0.0"), pkgLocal.body, nil); status != 201 {
		t.Fatalf("local push: %d %s", status, body)
	}

	base := s.srv.URL
	req := func(method, path string, extra ...string) (int, string) {
		t.Helper()
		args := []string{"-sS", "--compressed", "-m", "20", "-X", method, "-o", "/tmp/t341-body", "-w", "%{http_code}", base + path}
		args = append(args, extra...)
		t.Logf("$ curl %s", strings.Join(args, " "))
		code, err := exec.Command("curl", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("curl %s %s: %v", method, path, err)
		}
		body, rerr := os.ReadFile("/tmp/t341-body")
		if rerr != nil {
			t.Fatalf("read body: %v", rerr)
		}
		status := 0
		_, _ = fmt.Sscanf(string(code), "%d", &status) //nolint:errcheck // curl's -w %{http_code} is always digits
		t.Logf("=> %d %.200s", status, strings.TrimSpace(string(body)))
		return status, string(body)
	}

	// LOCAL: the three faces + the service index rows (section 9.1).
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v3/live-local/query"); status != 200 || !strings.Contains(body, "local.pkg") {
		t.Errorf("local search = %d", status)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v3/live-local/registration/local.pkg/index.json"); status != 200 || !strings.Contains(body, `"Local.Pkg"`) {
		t.Errorf("local registration = %d", status)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v3/live-local/registration/local.pkg/1.0.0.json"); status != 200 || !strings.Contains(body, `"Local.Pkg"`) {
		t.Errorf("local single-version leaf = %d", status)
	}
	if status, _ := req(http.MethodGet, "/binflow/api/nuget/v3/live-local/registration/local.pkg/9.9.9.json"); status != 404 {
		t.Errorf("local leaf miss = %d, want 404", status)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v3/live-local/flatcontainer/local.pkg/1.0.0/local.pkg.1.0.0.nupkg"); status != 200 || body != string(pkgLocal.body) {
		t.Errorf("local download = %d (len %d)", status, len(body))
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v3/live-local/index.json"); status != 200 ||
		!strings.Contains(body, "registration-semver2/") || !strings.Contains(body, "RegistrationsBaseUrl/3.6.0") {
		t.Errorf("service index = %d %.160s", status, body)
	}

	// The Q6 duplicate-arm matrix (nuget.md section 5.4, K59 — ruled
	// 2026-08-31 = align on 409; T-401): the DIRECT push form over the
	// real socket, four arms — ②a different bytes + w-only → 409 with the
	// official wording, ②b SAME bytes + w-only → 409 (the flip), ③ the
	// delete right → overwrite 201, ④ fresh package → 201.
	dupPkg := buildNupkg(t, "Live.Dup", "1.0.0", flatDeps("none"))
	dupAlt := buildNupkg(t, "Live.Dup", "1.0.0", flatDeps("Serilog", "4.0.0"))
	freshPkg := buildNupkg(t, "Live.Fresh", "1.0.0", flatDeps("none"))
	if err := os.WriteFile("/tmp/t401-dup.nupkg", dupPkg.body, 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.WriteFile("/tmp/t401-alt.nupkg", dupAlt.body, 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.WriteFile("/tmp/t401-fresh.nupkg", freshPkg.body, 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	s.seedUser(t, "livewriter", "livewriterpass")
	s.seedGrant(t, "t401-writer", "livewriter", "live-local", true, true, false)
	pushBase := "/binflow/api/nuget/v3/live-local/flatcontainer"
	// The seed (admin holds d) — also arm ④'s spelling for a fresh id.
	if status, body := req(http.MethodPut, pushBase, "-u", adminUser+":"+adminPass, "--data-binary", "@/tmp/t401-dup.nupkg"); status != 201 {
		t.Fatalf("Q6 seed push = %d %s", status, body)
	}
	// Arm ②a: different bytes, w-only.
	if status, body := req(http.MethodPut, pushBase, "-u", "livewriter:livewriterpass", "--data-binary", "@/tmp/t401-alt.nupkg"); status != 409 ||
		strings.TrimSpace(body) != msgPushDuplicate {
		t.Errorf("Q6 arm2a = (%d, %q), want 409 + the official wording", status, body)
	}
	// Arm ②b: SAME bytes, w-only — the Q6 flip's own leg.
	if status, body := req(http.MethodPut, pushBase, "-u", "livewriter:livewriterpass", "--data-binary", "@/tmp/t401-dup.nupkg"); status != 409 ||
		strings.TrimSpace(body) != msgPushDuplicate {
		t.Errorf("Q6 arm2b = (%d, %q), want 409 + the official wording", status, body)
	}
	// Arm ③: the delete right overwrites (different bytes, 201).
	if status, _ := req(http.MethodPut, pushBase, "-u", adminUser+":"+adminPass, "--data-binary", "@/tmp/t401-alt.nupkg"); status != 201 {
		t.Errorf("Q6 arm3 = %d, want the overwrite 201", status)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v3/live-local/flatcontainer/live.dup/1.0.0/live.dup.1.0.0.nupkg"); status != 200 || body != string(dupAlt.body) {
		t.Errorf("Q6 arm3 download = %d (len %d), want the overwritten bytes", status, len(body))
	}
	// Arm ④: a fresh package lands on w alone.
	if status, _ := req(http.MethodPut, pushBase, "-u", "livewriter:livewriterpass", "--data-binary", "@/tmp/t401-fresh.nupkg"); status != 201 {
		t.Errorf("Q6 arm4 = %d, want 201", status)
	}

	// REMOTE: search (live upstream proxy, URLs re-anchored), registration
	// (section 9.4: packageContent → the v2 Download face), the semver2
	// shape, the versions document and the package through the custom @id
	// spellings.
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v3/live-remote/query?q=live"); status != 200 ||
		!strings.Contains(body, `"Live.V3"`) ||
		!strings.Contains(body, "/binflow/api/nuget/v3/live-remote/registration/live.v3/index.json") {
		t.Errorf("remote search = %d %.200s", status, body)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v3/live-remote/query?q=live&semVerLevel=2.0.0"); status != 200 ||
		!strings.Contains(body, "/binflow/api/nuget/v3/live-remote/registration-semver2/live.v3/index.json") {
		t.Errorf("remote semVerLevel search = %d %.200s", status, body)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v3/live-remote/registration/live.v3/index.json"); status != 200 ||
		strings.Contains(body, up.srv.URL) ||
		!strings.Contains(body, "/binflow/api/nuget/v2/live-remote/Download/live.v3/1.0.0") {
		t.Errorf("remote registration = %d %.200s", status, body)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v3/live-remote/registration-semver2/live.v3/index.json"); status != 200 || !strings.Contains(body, `"Live.V3"`) {
		t.Errorf("remote semver2 registration = %d", status)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v3/live-remote/flatcontainer/live.v3/index.json"); status != 200 || !strings.Contains(body, `"1.0.0"`) {
		t.Errorf("remote versions = %d", status)
	}
	// The v2 Download face the rewritten packageContent cites actually serves.
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v2/live-remote/Download/live.v3/1.0.0"); status != 200 || body != string(pkg.body) {
		t.Errorf("remote v2 Download (the packageContent target) = %d (len %d)", status, len(body))
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v3/live-remote/flatcontainer/live.v3/1.0.0/live.v3.1.0.0.nupkg"); status != 200 || body != string(pkg.body) {
		t.Errorf("remote download = %d (len %d)", status, len(body))
	}

	// VIRTUAL: the merged search (local facts + the remote member's live
	// upstream), both registration families, the download.
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v3/live-virt/query"); status != 200 ||
		!strings.Contains(body, "local.pkg") || !strings.Contains(body, `"Live.V3"`) ||
		!strings.Contains(body, "/binflow/api/nuget/v3/live-virt/registration/live.v3/index.json") {
		t.Errorf("virtual search = %d %.240s", status, body)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v3/live-virt/registration/live.v3/index.json"); status != 200 || !strings.Contains(body, `"Live.V3"`) {
		t.Errorf("virtual remote registration = %d", status)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v3/live-virt/flatcontainer/live.v3/1.0.0/live.v3.1.0.0.nupkg"); status != 200 || body != string(pkg.body) {
		t.Errorf("virtual remote download = %d (len %d)", status, len(body))
	}

	// The upstream-deleted-still-cached comparison: kill the registration
	// upstream; the cached copy keeps serving inside the TTL.
	if status, _ := req(http.MethodGet, "/binflow/api/nuget/v3/live-remote/registration/live.v3/index.json"); status != 200 {
		t.Fatalf("pre-deletion registration = %d", status)
	}
	deleted.Store(true)
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v3/live-remote/registration/live.v3/index.json"); status != 200 || !strings.Contains(body, `"Live.V3"`) {
		t.Errorf("post-deletion registration = %d, want the cached 200 copy", status)
	}

	t.Log("V3 LIVE CURL MATRIX COMPLETE — search/download/registration over local/remote/virtual + dynamic spellings + semver2 + upstream-deletion")
}
