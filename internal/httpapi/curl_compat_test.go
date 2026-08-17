package httpapi_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The curl black-box suite reproduces the PRD's acceptance commands against
// the full httpapi stack: C03/C04/C05/C06/C10/C16/C19 (repository and item
// info), C20 (change password), C21a/C21c (token form create / revoke XOR
// and idempotence) and C22a (the PUT user route). curl is the "real client"
// for this management/REST surface; skipping is honest when the binary is
// unavailable (CI images must install curl).

func curlCompatPath(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl not available on PATH; compatible-REST client test skipped")
	}
	return p
}

// curlRun runs the real curl binary against the harness server and returns
// (stdout+stderr, exit code).
func curlRun(t *testing.T, serverURL string, args ...string) (string, int) {
	t.Helper()
	full := append([]string{"-s", "--retry", "0"}, args...)
	full = append(full, serverURL)
	cmd := exec.Command(curlCompatPath(t), full...) //nolint:gosec // test-only client invocation
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	code := 0
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("curl run: %v (%s)", err, out)
	}
	return string(out), code
}

// curlStatus returns just the HTTP status of a curl invocation.
func curlStatus(t *testing.T, serverURL string, args ...string) string {
	t.Helper()
	args = append(args, "-o", "/dev/null", "-w", "%{http_code}")
	out, code := curlRun(t, serverURL, args...)
	if code != 0 {
		t.Fatalf("curl exited %d: %s", code, out)
	}
	return strings.TrimSpace(out)
}

// TestCurlCompatReposAndStorage: C03 -> C04 -> C05 -> C06 -> C10 -> C16 ->
// C19 with the real client, exact status and body-wording assertions.
func TestCurlCompatReposAndStorage(t *testing.T) {
	h := newHarness(t)
	base := h.srv.URL + "/binflow"
	admin := adminUser + ":" + adminPass

	t.Run("C03 create is 200 with the plain-text wording", func(t *testing.T) {
		out, code := curlRun(t, base+"/api/repositories/generic-local",
			"-u", admin, "-X", "PUT", "-H", "Content-Type: application/json",
			"-d", `{"rclass":"local","packageType":"generic","description":"M1 QA"}`)
		if code != 0 {
			t.Fatalf("curl exit %d: %s", code, out)
		}
		if strings.TrimSpace(out) != "Successfully created repository 'generic-local'" {
			t.Fatalf("body = %q", out)
		}
	})

	t.Run("C04 invalid key is 400", func(t *testing.T) {
		if got := curlStatus(t, base+"/api/repositories/Bad_Key!",
			"-u", admin, "-X", "PUT", "-H", "Content-Type: application/json",
			"-d", `{"rclass":"local","packageType":"generic"}`); got != "400" {
			t.Fatalf("status = %s, want 400", got)
		}
	})

	t.Run("C05 list exposes the key via jq-style grep", func(t *testing.T) {
		out, code := curlRun(t, base+"/api/repositories", "-u", admin)
		if code != 0 || !strings.Contains(out, `"generic-local"`) {
			t.Fatalf("list = %q (exit %d)", out, code)
		}
	})

	t.Run("C06 single repo config", func(t *testing.T) {
		out, code := curlRun(t, base+"/api/repositories/generic-local", "-u", admin)
		if code != 0 || !strings.Contains(out, `"rclass": "local"`) || !strings.Contains(out, `"packageType": "generic"`) {
			t.Fatalf("config = %q (exit %d)", out, code)
		}
	})

	// Seed one artifact + folder for the info endpoints.
	if got := curlStatus(t, base+"/generic-local/acme/", "-u", admin, "-X", "PUT"); got != "201" {
		t.Fatalf("mkdir status = %s", got)
	}
	dir := t.TempDir()
	artifact := filepath.Join(dir, "artifact.bin")
	body := "the-curl-artifact"
	if err := writeTestFile(artifact, body); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if got := curlStatus(t, base+"/generic-local/acme/artifact.bin",
		"-u", admin, "-T", artifact); got != "201" {
		t.Fatalf("upload status = %s", got)
	}

	t.Run("C10 item info carries the sha256", func(t *testing.T) {
		out, code := curlRun(t, base+"/api/storage/generic-local/acme/artifact.bin", "-u", admin)
		if code != 0 {
			t.Fatalf("exit %d: %s", code, out)
		}
		want := fmt.Sprintf(`"sha256": "%s"`, sha256Hex([]byte(body)))
		if !strings.Contains(out, want) {
			t.Fatalf("body missing %s: %s", want, out)
		}
		if want := fmt.Sprintf(`"size": "%d"`, len(body)); !strings.Contains(out, want) {
			t.Fatalf("size not the string form of %d: %s", len(body), out)
		}
	})

	t.Run("C16 folder children", func(t *testing.T) {
		out, code := curlRun(t, base+"/api/storage/generic-local/acme", "-u", admin)
		if code != 0 || !strings.Contains(out, `"children"`) {
			t.Fatalf("folder info = %q (exit %d)", out, code)
		}
	})

	t.Run("C19 delete ladder", func(t *testing.T) {
		if got := curlStatus(t, base+"/api/repositories/generic-local", "-u", admin, "-X", "DELETE"); got != "400" {
			t.Fatalf("non-empty delete = %s, want 400", got)
		}
		if got := curlStatus(t, base+"/api/repositories/generic-local?deleteContent=true",
			"-u", admin, "-X", "DELETE"); got == "" || got[0] != '2' {
			t.Fatalf("forced delete = %s, want 2xx", got)
		}
	})
}

// TestCurlCompatSecurity: C20 (change password), C21 (token form create,
// revoke XOR and idempotence), C22a (PUT user route) — each against a fresh
// harness because C20 rotates the admin credential.
func TestCurlCompatSecurity(t *testing.T) {
	t.Run("C20 change password and old-password invalidation", func(t *testing.T) {
		h := newHarness(t)
		base := h.srv.URL + "/binflow"
		if got := curlStatus(t, base+"/api/security/password",
			"-u", adminUser+":"+adminPass, "-X", "PUT", "-H", "Content-Type: application/json",
			"-d", `{"oldPassword":"password","newPassword":"n3w-pw"}`); got != "200" {
			t.Fatalf("change = %s", got)
		}
		if got := curlStatus(t, base+"/api/repositories", "-u", adminUser+":"+adminPass); got != "401" {
			t.Fatalf("old password = %s, want 401", got)
		}
		if got := curlStatus(t, base+"/api/repositories", "-u", adminUser+":n3w-pw"); got != "200" {
			t.Fatalf("new password = %s, want 200", got)
		}
		// Alias route.
		if got := curlStatus(t, base+"/api/security/users/authorization/changePassword",
			"-u", adminUser+":n3w-pw", "-X", "POST", "-H", "Content-Type: application/json",
			"-d", `{"userName":"admin","oldPassword":"n3w-pw","newPassword1":"n3w-pw2","newPassword2":"n3w-pw2"}`); got != "200" {
			t.Fatalf("alias change = %s", got)
		}
	})

	t.Run("C21 token form create, use, revoke, idempotence, XOR", func(t *testing.T) {
		h := newHarness(t)
		base := h.srv.URL + "/binflow"
		admin := adminUser + ":" + adminPass

		out, code := curlRun(t, base+"/api/security/token",
			"-u", admin, "-X", "POST", "-d", "grant_type=client_credentials")
		if code != 0 {
			t.Fatalf("create exit %d: %s", code, out)
		}
		for _, want := range []string{`"access_token"`, `"token_type": "Bearer"`, `"scope"`, `"token_id"`} {
			if !strings.Contains(out, want) {
				t.Fatalf("create body missing %s: %s", want, out)
			}
		}
		token := jsonFieldString(t, out, "access_token")
		if token == "" {
			t.Fatalf("no access_token: %s", out)
		}

		// The token authenticates (Basic password slot, C21b).
		if got := curlStatus(t, base+"/api/repositories", "-u", adminUser+":"+token); got != "200" {
			t.Fatalf("token use = %s", got)
		}

		// Revoke by value: 200 "Token revoked"; the token then fails.
		out, code = curlRun(t, base+"/api/security/token/revoke",
			"-u", admin, "-X", "POST", "-d", "token="+token)
		if code != 0 || strings.TrimSpace(out) != "Token revoked" {
			t.Fatalf("revoke = %q (exit %d)", out, code)
		}
		if got := curlStatus(t, base+"/api/repositories", "-u", adminUser+":"+token); got != "401" {
			t.Fatalf("revoked token = %s, want 401", got)
		}

		// Repeat revoke: 200 "Token not found".
		out, code = curlRun(t, base+"/api/security/token/revoke",
			"-u", admin, "-X", "POST", "-d", "token="+token)
		if code != 0 || strings.TrimSpace(out) != "Token not found" {
			t.Fatalf("repeat revoke = %q (exit %d)", out, code)
		}

		// XOR rule: both -> 400, neither -> 400.
		if got := curlStatus(t, base+"/api/security/token/revoke",
			"-u", admin, "-X", "POST", "-d", "token=x&token_id=1"); got != "400" {
			t.Fatalf("XOR violation = %s, want 400", got)
		}
		if got := curlStatus(t, base+"/api/security/token/revoke",
			"-u", admin, "-X", "POST", "-d", ""); got != "400" {
			t.Fatalf("empty revoke = %s, want 400", got)
		}
	})

	t.Run("C22a PUT user route, list, then ungranted write 403", func(t *testing.T) {
		h := newHarness(t)
		base := h.srv.URL + "/binflow"
		admin := adminUser + ":" + adminPass

		if got := curlStatus(t, base+"/api/security/users/ci-bot",
			"-u", admin, "-X", "PUT", "-H", "Content-Type: application/json",
			"-d", `{"name":"ci-bot","email":"ci@example.com","password":"ci-pw","admin":false}`); got != "201" {
			t.Fatalf("create user = %s", got)
		}
		out, code := curlRun(t, base+"/api/security/users", "-u", admin)
		if code != 0 || !strings.Contains(out, `"ci-bot"`) || !strings.Contains(out, `"realm": "internal"`) {
			t.Fatalf("users = %q (exit %d)", out, code)
		}
		if strings.Contains(strings.ToLower(out), `"password"`) {
			t.Fatalf("user list leaks a password field: %s", out)
		}
		// The missing-email collection POST is 400 (same chain).
		if got := curlStatus(t, base+"/api/security/users",
			"-u", admin, "-X", "POST", "-H", "Content-Type: application/json",
			"-d", `{"name":"ci-bot-2","password":"ci-pw2"}`); got != "400" {
			t.Fatalf("missing email = %s, want 400", got)
		}
		// Ungranted write is 403.
		if got := curlStatus(t, base+"/generic-local/x.bin",
			"-u", "ci-bot:ci-pw", "-X", "PUT", "-d", "x"); got != "403" {
			// generic-local does not exist on this harness: the write would
			// be a repo 404. Create it first to make the assertion honest.
			if got2 := curlStatus(t, base+"/api/repositories/generic-local",
				"-u", admin, "-X", "PUT", "-H", "Content-Type: application/json",
				"-d", `{"rclass":"local","packageType":"generic"}`); got2 != "200" {
				t.Fatalf("seed repo = %s", got2)
			}
			if got := curlStatus(t, base+"/generic-local/x.bin",
				"-u", "ci-bot:ci-pw", "-X", "PUT", "-d", "x"); got != "403" {
				t.Fatalf("ungranted write = %s, want 403", got)
			}
		}
	})
}

// jsonFieldString extracts one top-level string field from a JSON document
// (a jq stand-in for tests that cannot depend on jq being installed).
func jsonFieldString(t *testing.T, doc, field string) string {
	t.Helper()
	// Cheap and adequate for these fixed-shape bodies: find "field": "value".
	needle := `"` + field + `": "`
	i := strings.Index(doc, needle)
	if i < 0 {
		return ""
	}
	rest := doc[i+len(needle):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// writeTestFile deposits content at path (curl -T needs a real file).
func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600) //nolint:gosec // test fixture under t.TempDir
}
