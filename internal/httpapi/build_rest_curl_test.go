// The build REST family's curl contract (M17 T-508, FR-152.2 AC3): the
// real-client arm of the suite — the L-line shape of the acceptance
// commands (PUT /binflow/api/build with the document on stdin's spot, the
// query ladder, the append merge) plus the error surface's verbatim
// statuses, all through the actual curl binary.

package httpapi_test

import (
	"strings"
	"testing"
)

// TestCurlCompatBuildFamily walks the upload/append/query ladder with the
// real client: 200-empty upload, echo with modules and dependencies, the
// 204 merge whose GET shows the union, the list faces, and the error
// surface (400 malformed, the verbatim 404, the 403 write arm).
func TestCurlCompatBuildFamily(t *testing.T) {
	h := newBuildHarness(t)
	base := h.srv.URL + "/binflow"
	admin := adminUser + ":" + adminPass

	t.Run("upload answers 200 empty", func(t *testing.T) {
		out, code := curlRun(t, base+"/api/build",
			"-u", admin, "-X", "PUT", "-H", "Content-Type: application/json",
			"-d", buildRESTDoc)
		if code != 0 {
			t.Fatalf("curl exit %d: %s", code, out)
		}
		if strings.TrimSpace(out) != "" {
			t.Fatalf("upload body = %q, want empty", out)
		}
	})

	t.Run("detail echoes modules and dependencies", func(t *testing.T) {
		out, code := curlRun(t, base+"/api/build/pub-app/51", "-u", admin)
		if code != 0 {
			t.Fatalf("curl exit %d: %s", code, out)
		}
		for _, want := range []string{
			`"uri": "/api/build/pub-app/51"`,
			`"id": "com.example:api:1.0"`,
			`"name": "api-1.0.jar"`,
			`"id": "junit:junit:4.13"`,
			`"scopes": [`,
			`"test"`,
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("detail missing %s: %s", want, out)
			}
		}
	})

	t.Run("append merges by module id (204, union on GET)", func(t *testing.T) {
		body := `[{"id":"com.example:api:1.0","dependencies":[{"type":"jar","sha1":"22","id":"org:lib:2.0"}]}]`
		if got := curlStatus(t, base+"/api/build/append/pub-app/51",
			"-u", admin, "-X", "POST", "-H", "Content-Type: application/json",
			"-d", body); got != "204" {
			t.Fatalf("append = %s, want 204", got)
		}
		out, code := curlRun(t, base+"/api/build/pub-app/51", "-u", admin)
		if code != 0 {
			t.Fatalf("curl exit %d: %s", code, out)
		}
		if !strings.Contains(out, `"id": "org:lib:2.0"`) {
			t.Fatalf("merged dependency missing: %s", out)
		}
		if !strings.Contains(out, `"id": "junit:junit:4.13"`) {
			t.Fatalf("original dependency overwritten (merge law broken): %s", out)
		}
	})

	t.Run("list faces carry the relative URIs", func(t *testing.T) {
		out, code := curlRun(t, base+"/api/build", "-u", admin)
		if code != 0 || !strings.Contains(out, `"uri": "/pub-app"`) ||
			!strings.Contains(out, `"lastStarted": "2026-09-07T10:00:00.000+0000"`) {
			t.Fatalf("names list = %q (exit %d)", out, code)
		}
		out, code = curlRun(t, base+"/api/build/pub-app", "-u", admin)
		if code != 0 || !strings.Contains(out, `"buildsNumbers"`) ||
			!strings.Contains(out, `"uri": "/51"`) {
			t.Fatalf("numbers face = %q (exit %d)", out, code)
		}
	})

	t.Run("error surface is honest", func(t *testing.T) {
		// Malformed document: 400 with the self-frozen wording.
		if got := curlStatus(t, base+"/api/build",
			"-u", admin, "-X", "PUT", "-H", "Content-Type: application/json",
			"-d", `{"name": "pub-app",`); got != "400" {
			t.Fatalf("malformed upload = %s, want 400", got)
		}
		// Missing parent: the spec's VERBATIM 404 wording.
		out, code := curlRun(t, base+"/api/build/append/pub-app/77",
			"-u", admin, "-X", "POST", "-H", "Content-Type: application/json",
			"-d", `[]`)
		if code != 0 || !strings.Contains(out, `"status": 404`) ||
			!strings.Contains(out, "Build-Info not found") {
			t.Fatalf("missing-parent append = %q (exit %d), want the verbatim 404", out, code)
		}
		// Ungranted write: 403 (denial precedes existence).
		if got := curlStatus(t, base+"/api/build",
			"-u", "eve:pw", "-X", "PUT", "-H", "Content-Type: application/json",
			"-d", buildRESTDoc); got != "403" {
			t.Fatalf("ungranted upload = %s, want 403", got)
		}
		// Anonymous: the 401 challenge.
		if got := curlStatus(t, base+"/api/build/pub-app/51"); got != "401" {
			t.Fatalf("anonymous detail = %s, want 401", got)
		}
	})
}
