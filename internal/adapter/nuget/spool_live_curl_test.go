package nuget

// T-476's product-side evidence leg: the REAL curl binary against the
// REAL assembled stack over a real socket, in the UAT incident's exact
// command shape —
//
//	curl -u admin:password -X PUT --data-binary @dummy.nupkg \
//	    http://<host>/binflow/api/nuget/v3/<repo>/flatcontainer
//
// which the read-only-rootfs UAT answered "spool upload: open
// /tmp/binflow-nuget-865178418.nupkg: read-only file system" on a bare
// 500. The production wiring (Options.SpoolDir = <dataDir>/staging, the
// cmd assembly's own shape) must answer 201 — including under a poisoned
// server-side TMPDIR, the incident's own topology. Every command and its
// answer is t.Log'd. Environment-gated: BINFLOW_T476_CURL_E2E=1.

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

func TestPushLiveCurlUnderReadOnlyServerTemp(t *testing.T) {
	if os.Getenv("BINFLOW_T476_CURL_E2E") != "1" {
		t.Skip("set BINFLOW_T476_CURL_E2E=1 (with curl on PATH) to run the T-476 real-client leg")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skipf("curl unavailable: %v", err)
	}

	s := newStack(t) // default spoolDir = <dataDir>/staging, the cmd posture
	s.seedRepo(t, "t476-nuget-local", repo.TypeLocal)
	pushURL := s.srv.URL + apiPath("t476-nuget-local") + "/" + segFlat

	// push runs the incident's exact command form (-w %{http_code} rides
	// along for the assertion; exec.Command passes the URL verbatim, no
	// shell). The in-process stack means this test process IS the server,
	// so the TMPDIR poison below lands on the server side.
	push := func(fixture string) string {
		t.Helper()
		args := []string{"-sS", "-u", adminUser + ":" + adminPass, "-X", "PUT",
			"--data-binary", "@" + fixture, "-o", os.DevNull, "-w", "%{http_code}", pushURL}
		t.Logf("$ curl %s", strings.Join(args, " "))
		code, err := exec.Command("curl", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("curl PUT: %v (%s)", err, code)
		}
		t.Logf("=> %s", strings.TrimSpace(string(code)))
		return strings.TrimSpace(string(code))
	}

	pkg := buildNupkg(t, "T476.Dummy", "1.0.0", flatDeps("none"))
	fixture := filepath.Join(t.TempDir(), "dummy.nupkg")
	if err := os.WriteFile(fixture, pkg.body, 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if code := push(fixture); code != "201" {
		t.Fatalf("curl PUT (the iron-evidence form) = %s, want 201 (the pre-T-476 answer was the bare 500)", code)
	}

	// The incident's own topology: poison the server-side TMPDIR, then
	// push a second, fresh package through the same command form.
	t.Setenv("TMPDIR", mustReadOnlyDir(t))
	pkg2 := buildNupkg(t, "T476.Robust", "1.0.0", flatDeps("none"))
	if err := os.WriteFile(fixture, pkg2.body, 0o644); err != nil {
		t.Fatalf("fixture 2: %v", err)
	}
	if code := push(fixture); code != "201" {
		t.Fatalf("curl PUT under a read-only server TMPDIR = %s, want 201", code)
	}
	if status, body, _ := s.get(packagePath("t476-nuget-local", "t476.robust", "1.0.0", "nupkg")); status != http.StatusOK || string(body) != string(pkg2.body) {
		t.Fatalf("package GET after the poisoned-TMPDIR push = %d (len %d, want %d stored bytes)", status, len(body), len(pkg2.body))
	}
	t.Log("T476 NUGET LIVE CURL COMPLETE — the iron-evidence PUT answers 201, including under the incident's read-only server TMPDIR")
}
