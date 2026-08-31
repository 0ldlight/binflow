package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The K60-1 gate exemption (T-394, docs/reverse/npm.md §2): the npm legacy
// login PUT carries its credential in the body (couch convention — no
// Authorization header), so the content plane's write gate must stand down
// for exactly the "-/user/org.couchdb.user:<name>" family on npm
// repositories. Two assertion families live here:
//
//   - the exemption works: the anonymous body arm becomes reachable, mints
//     a TokenRegistry token, is an idempotent re-mint (K60-6's
//     never-409 invariant), and the minted token authenticates;
//   - the predicate does not leak: every other write surface — publish,
//     dist-tags, the same family on a non-npm repository or an unknown
//     repo key, non-PUT verbs, non-couch ids — keeps the standard write
//     gate's 401.

// couchLoginBody is the document npm 10.x PUTs (live capture, K60 §2.1):
// only name/password are consumed server-side.
func couchLoginBody(name, password string) []byte {
	return []byte(fmt.Sprintf(
		`{"_id":"org.couchdb.user:%s","name":%q,"password":%q,"type":"user","roles":[],"date":"2026-08-31T00:00:00.000Z"}`,
		name, name, password))
}

// loginResponse is the K60 §2.2 success contract: npm reads only the
// token; id/rev are informational.
type loginResponse struct {
	OK    bool   `json:"ok"`
	ID    string `json:"id"`
	Token string `json:"token"`
}

func postCouchLogin(s *harness, path string, body []byte) *http.Response {
	return s.do(http.MethodPut, path, "", "", body, map[string]string{
		"Content-Type": "application/json",
	})
}

// TestT394NpmLegacyLoginBodyCredentialArm pins the exemption's positive
// face: anonymous PUTs on the couch user-document family reach the npm
// adapter's body-credential arm on BOTH content entrances (the /api/npm
// mount npm clients address, and the bare content mount), the minted token
// authenticates downstream, and re-login mints again — never a 409.
func TestT394NpmLegacyLoginBodyCredentialArm(t *testing.T) {
	s := newNpmStack(t)

	tests := []struct {
		name string
		path string
	}{
		{"api mount (npm client spelling)", "/binflow/api/npm/npm-local/-/user/org.couchdb.user:admin"},
		{"bare content mount", "/binflow/npm-local/-/user/org.couchdb.user:admin"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := postCouchLogin(s, tc.path, couchLoginBody("admin", adminPass))
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusCreated {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want 201 (body %s)", resp.StatusCode, body)
			}
			var lr loginResponse
			if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
				t.Fatalf("decode login response: %v", err)
			}
			if !lr.OK || lr.Token == "" {
				t.Fatalf("login response = %+v, want ok=true and a non-empty token", lr)
			}
			if lr.ID != "org.couchdb.user:admin" {
				t.Fatalf("id = %q, want org.couchdb.user:admin", lr.ID)
			}

			// The minted token authenticates on the same plane (the login
			// full chain's gate-level leg; the client-level chain runs in
			// the ticket's live verification).
			resp = s.do(http.MethodGet, "/binflow/api/npm/npm-local/-/whoami", "", "", nil,
				map[string]string{"Authorization": "Bearer " + lr.Token})
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("whoami with minted token: status = %d, want 200", resp.StatusCode)
			}
			var wa struct {
				Username string `json:"username"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&wa); err != nil {
				t.Fatalf("decode whoami: %v", err)
			}
			if wa.Username != "admin" {
				t.Fatalf("whoami username = %q, want admin", wa.Username)
			}

			// K60-6 invariant: login is an idempotent re-mint. A second PUT
			// must answer 201 again — a 409 would send npm into the rev
			// dance BinFlow deliberately does not serve.
			resp = postCouchLogin(s, tc.path, couchLoginBody("admin", adminPass))
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("re-login status = %d, want 201 (never 409)", resp.StatusCode)
			}
		})
	}
}

// TestT394NpmLoginCredentialArms pins the adapter's own verdicts once the
// gate lets the request through: a present-but-wrong password and a
// credential-less body are 401s with the Basic challenge, never a silent
// anonymous mint or a 500.
func TestT394NpmLoginCredentialArms(t *testing.T) {
	s := newNpmStack(t)
	path := "/binflow/api/npm/npm-local/-/user/org.couchdb.user:admin"

	tests := []struct {
		name string
		body []byte
	}{
		{"wrong password", couchLoginBody("admin", "not-the-password")},
		{"empty body (no name/password)", []byte(`{}`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := postCouchLogin(s, path, tc.body)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
			if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, "Basic") {
				t.Fatalf("WWW-Authenticate = %q, want a Basic challenge", got)
			}
		})
	}

	// The header arm that was already green (T-374 live: Basic -> 201)
	// stays green through the exempted route.
	resp := s.do(http.MethodPut, path, adminUser, adminPass, couchLoginBody("admin", adminPass), nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("header-authenticated login status = %d, want 201", resp.StatusCode)
	}
}

// TestT394NpmLoginExemptionDoesNotLeak is the strict side: every write
// face the exemption must NOT open keeps the standard gate's 401 for an
// anonymous caller — package publishes, the dist-tags family, non-couch
// or malformed couch spellings, non-PUT verbs, unknown repo keys, and —
// the load-bearing row — the very same couch path family on a non-npm
// repository, where the generic adapter would otherwise store the body as
// a node.
func TestT394NpmLoginExemptionDoesNotLeak(t *testing.T) {
	s := newNpmStack(t)
	// A generic repository beside the npm one: same request shape, other
	// content plane.
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: "generic-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("seed generic-local: %v", err)
	}

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"package publish keeps the write gate", http.MethodPut,
			"/binflow/api/npm/npm-local/leak-probe-pkg", http.StatusUnauthorized},
		{"scoped package publish keeps the write gate", http.MethodPut,
			"/binflow/api/npm/npm-local/@leak%2Fprobe-pkg", http.StatusUnauthorized},
		{"dist-tags collection write keeps the write gate", http.MethodPut,
			"/binflow/api/npm/npm-local/-/package/leak-probe-pkg/dist-tags", http.StatusUnauthorized},
		{"non-couch user id keeps the write gate", http.MethodPut,
			"/binflow/api/npm/npm-local/-/user/notcouch:admin", http.StatusUnauthorized},
		{"bare couch prefix without a name keeps the write gate", http.MethodPut,
			"/binflow/api/npm/npm-local/-/user/org.couchdb.user:", http.StatusUnauthorized},
		{"couch family with a trailing stranger segment keeps the write gate", http.MethodPut,
			"/binflow/api/npm/npm-local/-/user/org.couchdb.user:admin/stranger", http.StatusUnauthorized},
		{"DELETE on the couch family keeps the gate (PUT-only exemption)", http.MethodDelete,
			"/binflow/api/npm/npm-local/-/user/org.couchdb.user:admin", http.StatusUnauthorized},
		{"couch family on a generic repository keeps the write gate", http.MethodPut,
			"/binflow/generic-local/-/user/org.couchdb.user:admin", http.StatusUnauthorized},
		{"couch family under the npm mount on a generic repository keeps the write gate", http.MethodPut,
			"/binflow/api/npm/generic-local/-/user/org.couchdb.user:admin", http.StatusUnauthorized},
		{"couch family on an unknown repo key keeps the write gate", http.MethodPut,
			"/binflow/api/npm/no-such-repo/-/user/org.couchdb.user:admin", http.StatusUnauthorized},
		{"the web login entry keeps its anonymous 401 (K60-4 posture)", http.MethodPost,
			"/binflow/api/npm/npm-local/-/v1/login", http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := s.do(tc.method, tc.path, "", "", []byte(`{}`), nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusUnauthorized {
				if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, "Basic") {
					t.Fatalf("WWW-Authenticate = %q, want a Basic challenge", got)
				}
			}
		})
	}
}

// TestT394NpmSessionReadPlaneUntouched pins the K60 maintain face: the
// exemption touches no read arm — anonymous whoami still meets the
// adapter's 401 + Basic challenge, anonymous ping stays 200.
func TestT394NpmSessionReadPlaneUntouched(t *testing.T) {
	s := newNpmStack(t)

	t.Run("whoami anonymous stays 401 with challenge", func(t *testing.T) {
		resp := s.do(http.MethodGet, "/binflow/api/npm/npm-local/-/whoami", "", "", nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
		if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, "Basic") {
			t.Fatalf("WWW-Authenticate = %q, want a Basic challenge", got)
		}
	})

	t.Run("ping anonymous stays 200", func(t *testing.T) {
		resp := s.do(http.MethodGet, "/binflow/api/npm/npm-local/-/ping", "", "", nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})
}
