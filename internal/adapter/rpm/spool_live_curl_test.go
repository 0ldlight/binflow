package rpm

// T-476's product-side evidence leg: the REAL curl binary against the
// REAL assembled stack over a real socket — the plain PUT <path>.rpm,
// the family's common incident shape. The production wiring
// (Options.SpoolDir = <dataDir>/staging) must answer 201 — including
// under a poisoned server-side TMPDIR, the read-only-rootfs topology.
// Environment-gated: BINFLOW_T476_CURL_E2E=1.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

func TestRpmPutLiveCurlUnderReadOnlyServerTemp(t *testing.T) {
	if os.Getenv("BINFLOW_T476_CURL_E2E") != "1" {
		t.Skip("set BINFLOW_T476_CURL_E2E=1 (with curl on PATH) to run the T-476 real-client leg")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skipf("curl unavailable: %v", err)
	}

	s := newStack(t) // default spoolDir = <dataDir>/staging, the cmd posture
	s.seedRepo(t, "t476-rpm-local", repo.TypeLocal, "{}")

	// put runs the package PUT through real curl (-w %{http_code} rides
	// along for the assertion). The in-process stack means this test
	// process IS the server, so the TMPDIR poison below lands server-side.
	put := func(fixture, path string) string {
		t.Helper()
		args := []string{"-sS", "-u", adminUser + ":" + adminPass, "-X", "PUT",
			"--data-binary", "@" + fixture, "-o", os.DevNull, "-w", "%{http_code}",
			s.srv.URL + path}
		t.Logf("$ curl %s", strings.Join(args, " "))
		code, err := exec.Command("curl", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("curl PUT: %v (%s)", err, code)
		}
		t.Logf("=> %s", strings.TrimSpace(string(code)))
		return strings.TrimSpace(string(code))
	}

	dir := t.TempDir()
	fresh := filepath.Join(dir, "t476-fresh.rpm")
	if err := os.WriteFile(fresh, pkgFixture("t476curl", "1.0.0", "1", "noarch"), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if code := put(fresh, "/binflow/t476-rpm-local/t476curl-1.0.0-1.noarch.rpm"); code != "201" {
		t.Fatalf("curl rpm PUT = %s, want 201 (the pre-T-476 family answer was the bare 500)", code)
	}

	// The incident family's own topology: poison the server-side TMPDIR,
	// then PUT a second, fresh package through the same command form.
	t.Setenv("TMPDIR", mustReadOnlyDir(t))
	robust := filepath.Join(dir, "t476-robust.rpm")
	if err := os.WriteFile(robust, pkgFixture("t476robust", "1.0.0", "1", "noarch"), 0o644); err != nil {
		t.Fatalf("fixture 2: %v", err)
	}
	if code := put(robust, "/binflow/t476-rpm-local/t476robust-1.0.0-1.noarch.rpm"); code != "201" {
		t.Fatalf("curl rpm PUT under a read-only server TMPDIR = %s, want 201", code)
	}
	t.Log("T476 RPM LIVE CURL COMPLETE — the package PUT answers 201, including under the incident's read-only server TMPDIR")
}
