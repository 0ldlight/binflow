package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// seedRepo creates a local generic repository through the real service
// (admin plane), keeping the test's HTTP surface honest.
func seedRepo(t *testing.T, h *harness, key string) {
	t.Helper()
	admin := &auth.Principal{Name: adminUser, Admin: true}
	if _, err := h.svc.CreateRepo(context.Background(), admin, &metadata.Repo{
		RepoKey: key, Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("CreateRepo %s: %v", key, err)
	}
}

// grantRead gives user read+write on key/includePattern through the real
// permission store (PUT target + principals row, E-24 shape).
func grant(t *testing.T, h *harness, name, key, pattern, user string, read, write, del bool) {
	t.Helper()
	target := `["` + key + `"]`
	includes := `["` + pattern + `"]`
	if err := h.md.Permissions().PutTarget(context.Background(), &metadata.PermissionTarget{
		Name: name, Repos: target, Includes: includes, Excludes: "[]",
	}, []*metadata.PermissionPrincipal{{
		TargetName: name, Principal: user, PrincipalType: "user",
		CanRead: read, CanWrite: write, CanDelete: del,
	}}); err != nil {
		t.Fatalf("PutTarget %s: %v", name, err)
	}
}

// TestAnonymousLayeringMatrix is the ADR-0009 decision table (FR-5-AC12):
// anonymous GET on content passes (default anonymous_access=true),
// anonymous PUT answers 401 with the Basic challenge, anonymous access
// to the management plane answers 401 as well.
func TestAnonymousLayeringMatrix(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	// Admin deposits one artifact for the anonymous reader.
	resp := h.do(http.MethodPut, "/binflow/generic-local/acme/v2.bin", adminUser, adminPass,
		[]byte("artifact-bytes"), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("admin PUT status = %d, want 201", resp.StatusCode)
	}
	defer func() { _ = resp.Body.Close() }()

	tests := []struct {
		name          string
		method        string
		path          string
		user, pass    string
		wantStatus    int
		wantChallenge bool
	}{
		{"anonymous content GET is open (default on)", http.MethodGet, "/binflow/generic-local/acme/v2.bin", "", "", http.StatusOK, false},
		{"anonymous content HEAD is open", http.MethodHead, "/binflow/generic-local/acme/v2.bin", "", "", http.StatusOK, false},
		{"anonymous content PUT is 401", http.MethodPut, "/binflow/generic-local/anon/x.bin", "", "", http.StatusUnauthorized, true},
		{"anonymous content DELETE is 401", http.MethodDelete, "/binflow/generic-local/acme/v2.bin", "", "", http.StatusUnauthorized, true},
		{"anonymous management API is 401", http.MethodGet, "/binflow/api/v1/health", "", "", http.StatusUnauthorized, true},
		{"anonymous storage stats is 401", http.MethodGet, "/binflow/api/v1/storage/stats", "", "", http.StatusUnauthorized, true},
		{"admin management API passes", http.MethodGet, "/binflow/api/v1/health", adminUser, adminPass, http.StatusOK, false},
		{"bad credential is 401", http.MethodGet, "/binflow/api/v1/health", adminUser, "wrong", http.StatusUnauthorized, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.do(tc.method, tc.path, tc.user, tc.pass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			chal := resp.Header.Get("WWW-Authenticate")
			if tc.wantChallenge && !strings.HasPrefix(chal, `Basic realm="`) {
				t.Fatalf("WWW-Authenticate = %q, want Basic realm challenge", chal)
			}
			if !tc.wantChallenge && chal != "" {
				t.Fatalf("unexpected challenge %q", chal)
			}
		})
	}
}

// TestAnonymousReadDisabled (FR-5-AC13): with anonymous_access=false the
// content GET closes too — 401 + challenge for anonymous, 200 for admin,
// 200 for a read-granted user and 403 for the same user outside the
// granted pattern.
func TestAnonymousReadDisabled(t *testing.T) {
	h := newHarnessCfg(t, func(c *config.Config) { c.Security.AnonymousAccess = false },
		[][2]string{{"ci-bot", "ci-pw"}})
	seedRepo(t, h, "generic-local")
	grant(t, h, "ci-out-r", "generic-local", "ci-out/**", "ci-bot", true, false, false)

	resp := h.do(http.MethodPut, "/binflow/generic-local/ci-out/x.bin", adminUser, adminPass, []byte("x"), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("admin PUT status = %d", resp.StatusCode)
	}
	defer func() { _ = resp.Body.Close() }()
	resp = h.do(http.MethodPut, "/binflow/generic-local/acme/v2.bin", adminUser, adminPass, []byte("y"), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("admin PUT 2 status = %d", resp.StatusCode)
	}
	defer func() { _ = resp.Body.Close() }()

	tests := []struct {
		name       string
		user, pass string
		path       string
		want       int
	}{
		{"anonymous GET now 401", "", "", "/binflow/generic-local/ci-out/x.bin", http.StatusUnauthorized},
		{"admin GET still 200", adminUser, adminPass, "/binflow/generic-local/ci-out/x.bin", http.StatusOK},
		{"granted user GET 200", "ci-bot", "ci-pw", "/binflow/generic-local/ci-out/x.bin", http.StatusOK},
		{"granted user outside pattern 403", "ci-bot", "ci-pw", "/binflow/generic-local/acme/v2.bin", http.StatusForbidden},
		{"granted user GET of missing file under grant is 404", "ci-bot", "ci-pw", "/binflow/generic-local/ci-out/never.bin", http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.do(http.MethodGet, tc.path, tc.user, tc.pass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
	// The write-denied case is a PUT, answered with the envelope 403.
	resp = h.do(http.MethodPut, "/binflow/generic-local/ci-out/z.bin", "ci-bot", "ci-pw", []byte("z"), nil)
	eb := decodeError(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("ci-bot PUT status = %d, want 403", resp.StatusCode)
	}
	if eb.Errors[0].Status != http.StatusForbidden {
		t.Fatalf("envelope status = %d, want 403", eb.Errors[0].Status)
	}
}

// TestAuthenticatedNoPermissionWrites403: with a credential but no grant,
// content writes answer 403 (not a 401 challenge — the identity is known,
// rest-api.md section 1.4).
func TestAuthenticatedNoPermissionWrites403(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"ci-bot", "ci-pw"}})
	seedRepo(t, h, "generic-local")

	resp := h.do(http.MethodPut, "/binflow/generic-local/other/z.bin", "ci-bot", "ci-pw", []byte("z"), nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	eb := decodeError(t, resp)
	if eb.Errors[0].Status != http.StatusForbidden {
		t.Fatalf("envelope status = %d", eb.Errors[0].Status)
	}
}
