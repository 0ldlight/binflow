package nuget

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestLiveCurlMatrix drives the REAL curl binary against the REAL assembled
// stack over a real socket — the environment's client leg (no dotnet SDK or
// nuget.exe here; the ticket instruction's curl fallback, the M11 conan
// posture). Every command and its answer is t.Log'd — the ticket log's
// verbatim record. Environment-gated like the T-287 dotnet matrix so CI
// stays green without curl: BINFLOW_T337_CURL_E2E=1.
func TestLiveCurlMatrix(t *testing.T) {
	if os.Getenv("BINFLOW_T337_CURL_E2E") != "1" {
		t.Skip("set BINFLOW_T337_CURL_E2E=1 (with curl on PATH) to run the real-client matrix")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skipf("curl unavailable: %v", err)
	}

	// The stack: local + remote(fake upstream) + virtual(local+remote).
	s := newStack(t)
	s.seedRepo(t, "live-local", repo.TypeLocal)
	s.seedRepo(t, "live-remote", repo.TypeRemote)
	s.seedRepo(t, "live-virt", repo.TypeVirtual)
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: "live-prio", Type: repo.TypeLocal, PackageType: Protocol,
		Config: `{"priorityResolution":true}`,
	}); err != nil {
		t.Fatalf("seed priority member: %v", err)
	}
	up := newV2Upstream(t, func(w http.ResponseWriter, _ *http.Request, res string) {
		if strings.HasPrefix(res, "api/v2/Search()") {
			w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
			_, _ = w.Write([]byte(upstreamV2Feed("Up.Lib", "7.0.0"))) //nolint:gosec // live fixture
			return
		}
		http.NotFound(w, nil)
	})
	s.seedRemoteConfig(t, "live-remote", up.srv.URL)
	s.seedVirtualMembers(t, "live-virt", "live-prio", "live-remote")

	base := s.srv.URL
	req := func(method, path string, extra ...string) (int, string) {
		t.Helper()
		args := []string{"-sS", "--compressed", "-m", "20", "-X", method, "-o", "/tmp/t337-body", "-w", "%{http_code}", base + path}
		args = append(args, extra...)
		t.Logf("$ curl %s", strings.Join(args, " "))
		code, err := exec.Command("curl", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("curl %s %s: %v", method, path, err)
		}
		body, rerr := os.ReadFile("/tmp/t337-body")
		if rerr != nil {
			t.Fatalf("read body: %v", rerr)
		}
		status := 0
		_, _ = fmt.Sscanf(string(code), "%d", &status) //nolint:errcheck // curl's -w %{http_code} is always digits
		t.Logf("=> %d %.160s", status, strings.TrimSpace(string(body)))
		return status, string(body)
	}

	pkgA := buildNupkg(t, "Live.A", "1.0.0", flatDeps("none"))
	pkgB := buildNupkg(t, "Live.A", "1.1.0", flatDeps("none"))
	if err := os.WriteFile("/tmp/t337-a.nupkg", pkgA.body, 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.WriteFile("/tmp/t337-b.nupkg", pkgB.body, 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	auth := []string{"-u", adminUser + ":" + adminPass}

	// #17: publish root (curl raw octet-stream — the nuget.exe-era client
	// spelling) — the canonical 1.0.0 into live-local, and the same package
	// into the virtual's local member for the merge leg.
	for _, target := range []string{"live-local", "live-prio"} {
		if status, body := req(http.MethodPut, "/binflow/"+target+"/v2", append(auth, "--data-binary", "@/tmp/t337-a.nupkg", "-H", "Content-Type: application/octet-stream")...); status != 201 {
			t.Fatalf("#17 publish root (%s) = %d %s", target, status, body)
		}
	}
	// #18: publish with a path prefix.
	if status, body := req(http.MethodPut, "/binflow/live-local/v2/team/live.a/1.1.0", append(auth, "--data-binary", "@/tmp/t337-b.nupkg")...); status != 201 {
		t.Fatalf("#18 publish prefix = %d %s", status, body)
	}

	// The read faces. #1's base root rides the content-plane spelling — the
	// api-mount rewrite currently refuses the empty rest (the registered
	// httpapi gap, pinned below).
	if status, body := req(http.MethodGet, "/binflow/live-local/v2/"); status != 200 || !strings.Contains(body, "<service") {
		t.Errorf("#1 service doc = %d", status)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v2/live-local/"); status != 200 || !strings.Contains(body, "<service") {
		t.Errorf("#1 api-mount base root = %d, want 200 + the service doc (the empty-rest allowance landed with the ticket)", status)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v2/live-local/$metadata"); status != 200 || !strings.Contains(body, "V2FeedPackage") {
		t.Errorf("#2 $metadata = %d", status)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v2/live-local/Search()?searchTerm='live'"); status != 200 || !strings.Contains(body, "Live.A") {
		t.Errorf("#3 Search = %d", status)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v2/live-local/Search()/$count"); status != 200 || strings.TrimSpace(body) != "1" {
		t.Errorf("#4 Search/$count = (%d, %q)", status, body)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v2/live-local/FindPackagesById()?id='Live.A'"); status != 200 || !strings.Contains(body, "1.1.0") {
		t.Errorf("#5 FindPackagesById = %d", status)
	}
	if status, body := req(http.MethodGet, `/binflow/api/nuget/v2/live-local/FindPackagesById()/$count?id=%27Live.A%27`); status != 200 || strings.TrimSpace(body) != "2" {
		t.Errorf("#6 FindPackagesById/$count = (%d, %q)", status, body)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v2/live-local/Packages()"); status != 200 || !strings.Contains(body, "1.0.0") {
		t.Errorf("#7 Packages = %d", status)
	}
	if status, body := req(http.MethodGet, `/binflow/api/nuget/v2/live-local/Packages(Id=%27Live.A%27,Version=%271.0.0%27)`); status != 200 || !strings.Contains(body, "<entry>") {
		t.Errorf("#8 single entry = %d", status)
	}
	if status, body := req(http.MethodGet, `/binflow/api/nuget/v2/live-local/Packages(Id=%27Live.A%27)/Id`); status != 200 || !strings.Contains(body, "<d:Id ") {
		t.Errorf("#9 Id projection = %d", status)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v2/live-local/Packages()/$count"); status != 200 || strings.TrimSpace(body) != "2" {
		t.Errorf("#10 Packages/$count = (%d, %q)", status, body)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v2/live-local/GetUpdates()/?packageIds='Live.A'&versions='1.0.0'&includeAllVersions=true"); status != 200 || !strings.Contains(body, "1.1.0") {
		t.Errorf("#11 GetUpdates = %d", status)
	}
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v2/live-local/GetUpdates()/$count?packageIds='Live.A'&versions='1.0.0'"); status != 200 || strings.TrimSpace(body) != "1" {
		t.Errorf("#12 GetUpdates/$count = (%d, %q)", status, body)
	}
	// #13: the $batch wire (multipart/mixed with the application/http part).
	batch := "--batch_337\r\nContent-Type: application/http\r\nContent-Transfer-Encoding: binary\r\n\r\nGET /api/v2/live-local/Packages()/$count HTTP/1.1\r\nHost: binflow\r\n\r\n--batch_337--\r\n"
	if status, body := req(http.MethodPost, "/binflow/api/nuget/v2/live-local/$batch", append(auth, "-H", "Content-Type: multipart/mixed; boundary=batch_337", "--data-binary", batch)...); status != 202 || !strings.Contains(body, "batchresponse_") {
		t.Errorf("#13 $batch = %d", status)
	}
	// #14: protocol download (both the canonical and the prefixed landing).
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v2/live-local/Download/live.a/1.0.0"); status != 200 || body != string(pkgA.body) {
		t.Errorf("#14 Download = %d (len %d)", status, len(body))
	}
	if status, _ := req(http.MethodGet, "/binflow/api/nuget/v2/live-local/Download/live.a/1.1.0"); status != 200 {
		t.Errorf("#14 Download (prefixed) = %d", status)
	}
	// #15: the bare .nupkg face.
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v2/live-local/live.a/1.0.0/live.a.1.0.0.nupkg"); status != 200 || body != string(pkgA.body) {
		t.Errorf("#15 bare nupkg = %d", status)
	}
	// #16: the v2 delete — the prefixed landing first, then the canonical.
	if status, body := req(http.MethodDelete, "/binflow/api/nuget/v2/live-local/live.a/1.1.0", auth...); status != 200 || !strings.Contains(body, "Successfully removed") {
		t.Errorf("#16 delete = %d %s", status, body)
	}
	if status, _ := req(http.MethodGet, "/binflow/api/nuget/v2/live-local/Download/live.a/1.1.0"); status != 404 {
		t.Errorf("post-delete download = %d", status)
	}

	// The remote leg: the proxied search re-anchors the upstream feed.
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v2/live-remote/Search()"); status != 200 || !strings.Contains(body, "/Download/up.lib/7.0.0") {
		t.Errorf("remote Search = %d %.120s", status, body)
	}
	// The virtual leg: local member first, remote member surfaces.
	if status, body := req(http.MethodGet, "/binflow/api/nuget/v2/live-virt/Search()"); status != 200 || !strings.Contains(body, "Live.A") || !strings.Contains(body, "Up.Lib") {
		t.Errorf("virtual Search = %d %.160s", status, body)
	}

	t.Log("LIVE CURL MATRIX COMPLETE — 18 endpoints over local/remote/virtual")
}
