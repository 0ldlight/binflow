package cargo

// T-476's product-side evidence leg: the REAL curl binary against the
// REAL assembled stack over a real socket — the publish body (the wire's
// own [u32 LE][json][u32 LE][.crate] framing) rides a file through
// --data-binary, the family's common incident shape. The production
// wiring (Options.SpoolDir = <dataDir>/staging) must answer the official
// 200 + empty warnings — including under a poisoned server-side TMPDIR,
// the read-only-rootfs topology. Environment-gated:
// BINFLOW_T476_CURL_E2E=1.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

func TestPublishLiveCurlUnderReadOnlyServerTemp(t *testing.T) {
	if os.Getenv("BINFLOW_T476_CURL_E2E") != "1" {
		t.Skip("set BINFLOW_T476_CURL_E2E=1 (with curl on PATH) to run the T-476 real-client leg")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skipf("curl unavailable: %v", err)
	}

	s := newStack(t) // default spoolDir = <dataDir>/staging, the cmd posture
	s.seedRepo(t, "t476-cargo-local", repo.TypeLocal)
	pushURL := s.srv.URL + repoPath("t476-cargo-local") + "/api/v1/crates/new"

	// publish runs the framed PUT through real curl (-w %{http_code} rides
	// along for the assertion). The in-process stack means this test
	// process IS the server, so the TMPDIR poison below lands server-side.
	publish := func(fixture string) (string, string) {
		t.Helper()
		args := []string{"-sS", "-u", adminUser + ":" + adminPass, "-X", "PUT",
			"--data-binary", "@" + fixture, "-w", "\n%{http_code}", pushURL}
		t.Logf("$ curl %s", strings.Join(args, " "))
		out, err := exec.Command("curl", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("curl PUT: %v (%s)", err, out)
		}
		parts := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
		body, code := parts[0], "200"
		if len(parts) == 2 {
			body, code = parts[0], strings.TrimSpace(parts[1])
		}
		t.Logf("=> %s %.200s", code, body)
		return code, body
	}

	dir := t.TempDir()
	fresh := filepath.Join(dir, "t476-fresh.cargo")
	if err := os.WriteFile(fresh, publishBody(`{"name":"t476curl","vers":"0.1.0"}`, []byte("t476 crate bytes")), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if code, body := publish(fresh); code != "200" || strings.Contains(body, "Failed to publish with error") {
		t.Fatalf("curl publish = (%s, %.200s), want the official 200 + empty warnings", code, body)
	}

	// The incident family's own topology: poison the server-side TMPDIR,
	// then publish a second, fresh crate through the same command form.
	t.Setenv("TMPDIR", mustReadOnlyDir(t))
	robust := filepath.Join(dir, "t476-robust.cargo")
	if err := os.WriteFile(robust, publishBody(`{"name":"t476robust","vers":"0.1.0"}`, []byte("robust crate bytes")), 0o644); err != nil {
		t.Fatalf("fixture 2: %v", err)
	}
	if code, body := publish(robust); code != "200" || strings.Contains(body, "Failed to publish with error") {
		t.Fatalf("curl publish under a read-only server TMPDIR = (%s, %.200s), want 200 + empty warnings", code, body)
	}
	t.Log("T476 CARGO LIVE CURL COMPLETE — the framed publish answers the official 200, including under the incident's read-only server TMPDIR")
}
