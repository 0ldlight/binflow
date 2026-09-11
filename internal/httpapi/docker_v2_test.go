package httpapi_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/docker"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// netDial and bufioReader are thin aliases so the raw-TCP probes read at
// a glance next to their /binflow twin in routes_test.go.
func netDial(h *harness) (net.Conn, error) {
	return net.Dial("tcp", h.srv.Listener.Addr().String())
}

func bufioReader(c net.Conn) *bufio.Reader { return bufio.NewReader(c) }

// The T-33 foundation suite: the /v2 root-level exception (ADR-0010),
// name resolution, the spec error envelope, the ping/challenge pair
// (D04) and the traversal defenses (NFR-S11). Everything runs against the
// real router + middleware chain through the standard harness.

// seedDockerRepo creates a docker-typed repository row directly through
// the store: repo.Service still rejects package_type=docker until T-35
// lands, and the /v2 plane only needs the ROW (its gate reads
// PackageType), not the service's create path.
func seedDockerRepo(t *testing.T, h *harness, key string) {
	t.Helper()
	if err := h.md.Repos().Create(t.Context(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeLocal, PackageType: "docker",
	}); err != nil {
		t.Fatalf("seed docker repo %s: %v", key, err)
	}
}

// specErrorBody mirrors the registry error schema for assertions.
type specErrorBody struct {
	Errors []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Detail  any    `json:"detail"`
	} `json:"errors"`
}

// decodeSpecError parses a /v2 response body as the registry error schema.
func decodeSpecError(t *testing.T, body string) specErrorBody {
	t.Helper()
	var eb specErrorBody
	if err := json.Unmarshal([]byte(body), &eb); err != nil {
		t.Fatalf("body %q is not the registry spec error schema: %v", body, err)
	}
	if len(eb.Errors) != 1 {
		t.Fatalf("body %q: want exactly one error entry, got %d", body, len(eb.Errors))
	}
	return eb
}

// assertV2Headers pins the two headers every /v2 response must carry:
// the api-version advertisement (docker-registry.md section 0, high
// confidence: Artifactory enforces it everywhere) and a JSON content type.
func assertV2Headers(t *testing.T, resp *http.Response) {
	t.Helper()
	if got := resp.Header.Get("Docker-Distribution-Api-Version"); got != "registry/2.0" {
		t.Fatalf("Docker-Distribution-Api-Version = %q, want registry/2.0", got)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}

// TestV2PingChallengesUnauthenticated (DE-01, D44-1/PRD C6 errata): every
// UNAUTHENTICATED ping — on an anonymous-open instance too — answers 401 +
// the Bearer challenge. Ping-caching clients (docker daemon,
// containers/image) authenticate only against the challenge the ping
// cached, so a challenge-free 200 made them skip credentials forever (push
// 401 loops with zero token requests; a wrong-password login "succeeded"
// without the server seeing a single authenticated request). Anonymous
// access now flows through the anonymous token; an authenticated principal
// still gets the 200 {} probe.
func TestV2PingChallengesUnauthenticated(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/v2", "/v2/"} {
		resp := h.do(http.MethodGet, path, "", "", nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s anonymous status = %d; body=%s", path, resp.StatusCode, body)
		}
		assertV2Headers(t, resp)
		ch := resp.Header.Get("WWW-Authenticate")
		want := fmt.Sprintf(`Bearer realm="%s/v2/token",service="%s"`,
			h.srv.URL, strings.TrimPrefix(h.srv.URL, "http://"))
		if ch != want {
			t.Fatalf("%s WWW-Authenticate =\n  %q\nwant\n  %q", path, ch, want)
		}
	}
	// Authenticated probe: 200 {} (the success the daemon caches).
	resp := h.do(http.MethodGet, "/v2/", adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK || body != "{}" {
		t.Fatalf("authenticated ping = %d %q", resp.StatusCode, body)
	}
	assertV2Headers(t, resp)
}

// TestV2PingAnonymousClosed (D04 second half, ADR-0010 clause 4): with
// anonymous access off, the same probe answers 401 + the Bearer challenge
// whose realm is the adapter's own /v2/token endpoint and whose service is
// the request's host echo (L000-B C01). The PRD v1.0 wording
// (realm=/binflow/api/security/token) was
// superseded by ADR-0010 and written back in PRD v1.1 (T-32 risk R1).
func TestV2PingAnonymousClosed(t *testing.T) {
	h := newHarnessCfg(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = false }, nil)

	resp := h.do(http.MethodGet, "/v2/", "", "", nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
	}
	assertV2Headers(t, resp)
	eb := decodeSpecError(t, body)
	if eb.Errors[0].Code != "UNAUTHORIZED" {
		t.Fatalf("error code = %q, want UNAUTHORIZED", eb.Errors[0].Code)
	}
	ch := resp.Header.Get("WWW-Authenticate")
	want := fmt.Sprintf(`Bearer realm="%s/v2/token",service="%s"`,
		h.srv.URL, strings.TrimPrefix(h.srv.URL, "http://"))
	if ch != want {
		t.Fatalf("WWW-Authenticate =\n  %q\nwant\n  %q", ch, want)
	}
	// An authenticated principal gets the 200 even on a closed instance.
	resp = h.do(http.MethodGet, "/v2/", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated ping on closed instance = %d", resp.StatusCode)
	}
}

// TestV2PingBaseURLOverride: server.base_url wins over the request Host
// when the challenge realm is built (ADR-0010 clause 4's consequence
// bullet: "server.base_url 语义不变（realm 生成用它）").
func TestV2PingBaseURLOverride(t *testing.T) {
	h := newHarnessCfg(t, func(c *mutatedConfig) {
		c.Security.AnonymousAccess = false
		c.Server.BaseURL = "https://registry.example.com"
	}, nil)
	resp := h.do(http.MethodGet, "/v2/", "", "", nil, nil)
	mustGet(t, resp)
	// base_url drives the REALM; the service still echoes the request's
	// host (C01: the service is what the client addressed, not the config).
	want := fmt.Sprintf(`Bearer realm="https://registry.example.com/v2/token",service="%s"`,
		strings.TrimPrefix(h.srv.URL, "http://"))
	if got := resp.Header.Get("WWW-Authenticate"); got != want {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
	}
}

// TestV2NameResolution (ADR-0010 clause 3): the name splits first segment
// = repo key, remainder = image; a single-segment name has no split and
// answers the spec 404; a missing or non-docker repository answers 404
// NAME_UNKNOWN with the spec body.
func TestV2NameResolution(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "team1")
	seedRepo(t, h, "generic-local")

	tests := []struct {
		name    string
		method  string
		path    string
		status  int
		code    string // empty: any spec body accepted (foundation 404s)
		message string // substring asserted when non-empty
	}{
		{
			// T-39: the manifest route serves its protocol answer now — an
			// unknown tag is MANIFEST_UNKNOWN (the name resolved fine).
			name:   "repo with image resolves onto the manifest plane",
			method: http.MethodGet,
			path:   "/v2/team1/app/manifests/latest",
			status: http.StatusNotFound,
			code:   "MANIFEST_UNKNOWN",
		},
		{
			// Same resolution with a nested image name; the malformed digest
			// reference answers the protocol's DIGEST_INVALID.
			name:    "nested image name resolves the same repo key",
			method:  http.MethodGet,
			path:    "/v2/team1/acme/app/manifests/sha256:abc",
			status:  http.StatusBadRequest,
			code:    "DIGEST_INVALID",
			message: "invalid docker digest",
		},
		{
			name:   "single-segment name has no repository split",
			method: http.MethodGet,
			path:   "/v2/ubuntu/manifests/latest",
			status: http.StatusNotFound,
		},
		{
			name:    "missing repository is NAME_UNKNOWN",
			method:  http.MethodGet,
			path:    "/v2/no-such-repo/app/manifests/latest",
			status:  http.StatusNotFound,
			code:    "NAME_UNKNOWN",
			message: "no-such-repo",
		},
		{
			name:    "non-docker repository is NAME_UNKNOWN too (indistinguishable)",
			method:  http.MethodGet,
			path:    "/v2/generic-local/app/manifests/latest",
			status:  http.StatusNotFound,
			code:    "NAME_UNKNOWN",
			message: "generic-local",
		},
		{
			name:    "referrers route answers the DE-15 404",
			method:  http.MethodGet,
			path:    "/v2/team1/app/referrers/sha256:abc",
			status:  http.StatusNotFound,
			message: "not implemented",
		},
		{
			name:   "undefined route shape answers the DE-16 404",
			method: http.MethodGet,
			path:   "/v2/foo/bar/baz/qux",
			status: http.StatusNotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.do(tc.method, tc.path, "", "", nil, nil)
			body := mustGet(t, resp)
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, tc.status, body)
			}
			assertV2Headers(t, resp)
			eb := decodeSpecError(t, body)
			if tc.code != "" && eb.Errors[0].Code != tc.code {
				t.Fatalf("error code = %q, want %q", eb.Errors[0].Code, tc.code)
			}
			if tc.message != "" && !strings.Contains(eb.Errors[0].Message, tc.message) {
				t.Fatalf("message %q does not contain %q", eb.Errors[0].Message, tc.message)
			}
		})
	}
}

// TestV2ErrorEnvelopeIsolation (NFR-S10): /v2 failures render the registry
// schema, never the /binflow errors[] envelope — including the ping
// endpoint's method failure (reached with credentials: an unauthenticated
// ping challenges first since D44-1). The status field of the /binflow
// envelope is the discriminator: the registry schema has no "status"
// member.
func TestV2ErrorEnvelopeIsolation(t *testing.T) {
	h := newHarness(t)

	resp := h.do(http.MethodPost, "/v2/", adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /v2/ status = %d; body=%s", resp.StatusCode, body)
	}
	assertV2Headers(t, resp)
	if strings.Contains(body, `"status"`) {
		t.Fatalf("/v2 error body carries the /binflow envelope's status field: %s", body)
	}
	eb := decodeSpecError(t, body)
	if eb.Errors[0].Code != "UNSUPPORTED" {
		t.Fatalf("error code = %q, want UNSUPPORTED", eb.Errors[0].Code)
	}
}

// TestV2TraversalDefense (NFR-S11, D24 variants): dot-segment and
// percent-encoded traversal spellings under /v2 answer 400/404 with the
// spec body and never touch anything outside the data directory. The
// requests go over the vanilla client here (the Go client refuses dot
// segments; the raw-TCP proof for the SAME rule is the generic plane's
// TestDotSegmentReachesAdapterUnnormalized, and the router path is
// shared).
func TestV2TraversalDefense(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "team1")

	tests := []struct {
		name   string
		path   string
		status int
	}{
		{"encoded dot-dot in repo segment", "/v2/team1/%2e%2e/etc/passwd/manifests/latest", http.StatusBadRequest},
		{"decoded dot-dot in image segment", "/v2/team1/../etc/passwd/manifests/latest", http.StatusBadRequest},
		{"dot segment inside image", "/v2/team1/acme/./app/manifests/latest", http.StatusBadRequest},
		{"double slash in name", "/v2/team1//app/manifests/latest", http.StatusBadRequest},
		// Review B2: the repo-key slot is inside the defense — these were
		// 404s before the fix.
		{"dot-dot in repo key slot", "/v2/../etc/passwd", http.StatusBadRequest},
		{"dot in repo key slot with route", "/v2/./x/manifests/latest", http.StatusBadRequest},
		{"dot-dot repo key with route tail", "/v2/../binflow/app/manifests/latest", http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.do(http.MethodGet, tc.path, "", "", nil, nil)
			body := mustGet(t, resp)
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, tc.status, body)
			}
			assertV2Headers(t, resp)
			decodeSpecError(t, body)
		})
	}

	// Encoded slash ("%2F") inside the first segment: after decoding the
	// path names TWO segments ("team1" / "secret"), which is a legal
	// repoKey/image split — the request proceeds as the repo-gate 404,
	// exactly like the generic plane's twin (TestEncodedSlashACLConsistent):
	// the decoded form can never smuggle past the ACL, it just addresses
	// a repository that does not exist. Encoded-slash traversal inside
	// the IMAGE part is rejected by the dot-segment walk above when it
	// decodes to "../", and plain "%2F" images are ordinary name bytes.
	t.Run("encoded slash decodes to a legal two-segment name (repo-gate 404)", func(t *testing.T) {
		// "team1%2Fsecret" decodes to segments team1/secret: repoKey
		// "team1" exists, so this specific spelling proceeds to the
		// foundation 404; with an ABSENT repo key the same mechanism hits
		// the NAME_UNKNOWN gate. Both are 404 — the decoded form can
		// never smuggle a path past the ACL, it just names repositories.
		resp := h.do(http.MethodGet, "/v2/team1%2Fsecret/manifests/latest", "", "", nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%s", resp.StatusCode, body)
		}
		decodeSpecError(t, body)

		resp = h.do(http.MethodGet, "/v2/absent%2Fsecret/manifests/latest", "", "", nil, nil)
		body = mustGet(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("absent-repo status = %d, want 404; body=%s", resp.StatusCode, body)
		}
		eb := decodeSpecError(t, body)
		if eb.Errors[0].Code != "NAME_UNKNOWN" {
			t.Fatalf("error code = %q, want NAME_UNKNOWN", eb.Errors[0].Code)
		}
	})
}

// TestV2TraversalDefenseRawTCP: dot-segment requests reach the /v2 route
// VERBATIM — no net/http cleanPath redirect in front of it (the T-14
// lesson: ServeMux redirects un-normalized paths even through a lone
// catch-all; the /v2 exception must inherit the same no-normalization
// posture the /binflow dispatcher has).
func TestV2TraversalDefenseRawTCP(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{
		"/v2/team1/a/../../etc/passwd",
		"/v2/./team1/app/manifests/latest",
	} {
		t.Run(path, func(t *testing.T) {
			probeRawV2(t, h, path)
		})
	}
}

// probeRawV2 sends one un-normalized GET over raw TCP and asserts a 400 or
// 404 spec-body answer — never a 3xx redirect.
func probeRawV2(t *testing.T, h *harness, path string) {
	t.Helper()
	conn, err := netDial(h)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: t\r\nConnection: close\r\n\r\n", path) //nolint:errcheck // test probe; write failure fails the read below
	resp, err := http.ReadResponse(bufioReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		t.Fatalf("redirect %d to %q — normalization leaked in front of /v2",
			resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
		body := mustGet(t, resp)
		t.Fatalf("status = %d, want 400/404; body=%s", resp.StatusCode, body)
	}
}

// TestV2RootNotUnderBinflow (ADR-0010 clause 2): /binflow/v2/** keeps the
// E-26 envelope 404 — no double mount — while the root spelling serves
// the registry plane. A client spelling the wrong prefix gets the honest
// envelope, a registry client gets the spec body.
func TestV2RootNotUnderBinflow(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "team1")

	resp := h.do(http.MethodGet, "/binflow/v2/team1/app/manifests/latest", "", "", nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/binflow/v2 status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, `"status"`) {
		t.Fatalf("/binflow/v2 body is not the envelope: %s", body)
	}
	if resp.Header.Get("Docker-Distribution-Api-Version") != "" {
		t.Fatal("/binflow/v2 response carries the registry advertisement header")
	}
}

// TestV2MiddlewareChainShared (ADR-0010 clause 1: "进同一 middleware 链"):
// /v2 requests get a request id, an access-log line and panic recovery —
// the observability contract of every product route.
func TestV2MiddlewareChainShared(t *testing.T) {
	h := newHarness(t)
	h.resetLogs()

	resp := h.do(http.MethodGet, "/v2/", "", "", nil, nil)
	mustGet(t, resp)
	if resp.Header.Get("X-Request-Id") == "" {
		t.Fatal("/v2 response carries no X-Request-Id")
	}
	logs := h.logs()
	if !strings.Contains(logs, "path=/v2/") {
		t.Fatalf("no access-log line for /v2/; logs:\n%s", logs)
	}
}

// TestV2HealthRegistryField (PRD section 6.3, add-only): the /api/v1/health
// body grows a registry subsystem reporting ok when the docker adapter is
// mounted.
func TestV2HealthRegistryField(t *testing.T) {
	h := newHarness(t)
	resp := h.do(http.MethodGet, "/binflow/api/v1/health", adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	var health struct {
		Status   string                  `json:"status"`
		Storage  struct{ Status string } `json:"storage"`
		Metadata struct{ Status string } `json:"metadata"`
		Registry struct{ Status string } `json:"registry"`
	}
	if err := json.Unmarshal([]byte(body), &health); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if health.Registry.Status != "ok" {
		t.Fatalf("registry subsystem = %q, want ok; body=%s", health.Registry.Status, body)
	}
	if health.Status != "ok" || health.Metadata.Status != "ok" {
		t.Fatalf("legacy fields degraded: status=%s metadata=%s", health.Status, health.Metadata.Status)
	}
}

// ---- T-33 review fixes (B1/B2/B3/N4/N5) ----

// statusFormEntry mirrors Artifactory's generic error model entry for
// assertions (the ping face's refused-credential body).
type statusFormEntry struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// statusFormEnvelope is the {"errors":[{status,message}]} pretty body.
type statusFormEnvelope struct {
	Errors []statusFormEntry `json:"errors"`
}

// assertPingRefusedFace pins the PING route's refused-credential arm
// (L003-2, evidence reports/compatibility/L003-remote-face-diff.md ③ +
// captures /tmp/l0022/a_pingbad.h): 401 + `Basic realm="Artifactory
// Realm"` + application/json;charset=ISO-8859-1 + the generic error
// model's pretty "Bad Credentials". The bad-basic arm is verbatim parity
// with the reference; BinFlow renders the same form for every refused
// credential class (the reference's live stale-bearer wording — "Props
// Authentication Token not found" — is a measured message-level delta
// left to a follow-up; the challenge shape and envelope are aligned).
func assertPingRefusedFace(t *testing.T, resp *http.Response, body string) {
	t.Helper()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
	}
	if ch := resp.Header.Get("WWW-Authenticate"); ch != `Basic realm="Artifactory Realm"` {
		t.Fatalf("challenge = %q, want the Basic realm form", ch)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json;charset=ISO-8859-1" {
		t.Fatalf("Content-Type = %q, want the ping face charset spelling", ct)
	}
	var eb statusFormEnvelope
	if err := json.Unmarshal([]byte(body), &eb); err != nil {
		t.Fatalf("body %q is not the generic error model: %v", body, err)
	}
	if len(eb.Errors) != 1 || eb.Errors[0].Status != http.StatusUnauthorized ||
		eb.Errors[0].Message != "Bad Credentials" {
		t.Fatalf("body = %q, want the pretty Bad Credentials status form", body)
	}
}

// TestV2RejectedCredentialRendersSpecBody (review B1; re-anchored by
// L003-2): a presented-but-refused credential answers the registry plane
// on every /v2 route — never the /binflow envelope. The two routes have
// DIFFERENT verified faces (L003-remote-face-diff.md ②/③): the PING route
// answers the reference's own refused-credential arm — Basic realm +
// pretty "Bad Credentials" (capture a_pingbad.h) — while every resource
// route keeps the Bearer re-challenge + spec body (a docker client
// mid-negotiation on a resource must not meet a Basic challenge). Both
// anonymous modes are covered on both routes.
func TestV2RejectedCredentialRendersSpecBody(t *testing.T) {
	badBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:wrong"))
	cases := []struct {
		name string
		open bool
		hdr  map[string]string
	}{
		{name: "bad basic, anonymous open", open: true, hdr: map[string]string{"Authorization": badBasic}},
		{name: "bad basic, anonymous closed", open: false, hdr: map[string]string{"Authorization": badBasic}},
		{name: "stale bearer, anonymous open", open: true, hdr: map[string]string{"Authorization": "Bearer not-a-real-token"}},
		{name: "stale bearer, anonymous closed", open: false, hdr: map[string]string{"Authorization": "Bearer not-a-real-token"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarnessCfg(t, func(c *mutatedConfig) {
				c.Security.AnonymousAccess = tc.open
			}, nil)
			// The ping face: the reference's refused-credential arm.
			resp := h.do(http.MethodGet, "/v2/", "", "", nil, tc.hdr)
			assertPingRefusedFace(t, resp, mustGet(t, resp))

			// Every other route: the Bearer re-challenge + spec body.
			path := "/v2/somerepo/app/manifests/latest"
			resp = h.do(http.MethodGet, path, "", "", nil, tc.hdr)
			body := mustGet(t, resp)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("%s status = %d; body=%s", path, resp.StatusCode, body)
			}
			assertV2Headers(t, resp)
			if strings.Contains(body, `"status"`) {
				t.Fatalf("%s body carries the generic status form: %s", path, body)
			}
			eb := decodeSpecError(t, body)
			if eb.Errors[0].Code != "UNAUTHORIZED" {
				t.Fatalf("%s code = %q, want UNAUTHORIZED", path, eb.Errors[0].Code)
			}
			ch := resp.Header.Get("WWW-Authenticate")
			if !strings.HasPrefix(ch, `Bearer realm="`) || !strings.Contains(ch, `/v2/token`) {
				t.Fatalf("%s challenge = %q, want Bearer realm=.../v2/token", path, ch)
			}
			if strings.Contains(ch, `Basic realm`) {
				t.Fatalf("%s challenge carries a Basic scheme: %q", path, ch)
			}
		})
	}
}

// TestV2ExpiredBearerRendersSpecBody (review B1, the docker renewal path;
// re-anchored by L003-2): a token that WAS valid and has since expired
// still lands on the registry-plane 401 — on the PING route that is the
// refused-credential face (Basic realm + pretty status form, the
// reference's own arm), on resource routes the Bearer re-challenge
// (covered by TestV2RejectedCredentialRendersSpecBody's resource leg).
// The row is seeded with a past ExpiresAt because Issue() treats
// non-positive TTL as "never expires".
func TestV2ExpiredBearerRendersSpecBody(t *testing.T) {
	h := newHarness(t)
	plaintext := "expired-token-plaintext-" + strconv.Itoa(os.Getpid())
	digest := sha256.Sum256([]byte(plaintext))
	if _, err := h.md.Tokens().Create(t.Context(), &metadata.Token{
		Username:    adminUser,
		TokenSHA256: hex.EncodeToString(digest[:]),
		ExpiresAt:   "2000-01-01T00:00:00Z",
		CreatedAt:   "2000-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed expired token row: %v", err)
	}

	resp := h.do(http.MethodGet, "/v2/", "", "", nil,
		map[string]string{"Authorization": "Bearer " + plaintext})
	assertPingRefusedFace(t, resp, mustGet(t, resp))
}

// TestV2RevokedBearerRendersSpecBody (review B1 + D23 preview; re-anchored
// by L003-2): a revoked token's Bearer request also lands on the
// registry-plane 401 — the ping route's refused-credential face.
func TestV2RevokedBearerRendersSpecBody(t *testing.T) {
	h := newHarness(t)
	tok, err := h.authSvc.Issue(t.Context(), adminUser, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := h.authSvc.Revoke(t.Context(), tok.AccessToken); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	resp := h.do(http.MethodGet, "/v2/", "", "", nil,
		map[string]string{"Authorization": "Bearer " + tok.AccessToken})
	assertPingRefusedFace(t, resp, mustGet(t, resp))
}

// TestV2PanicRendersSpecBody (review B1 same-family): a panicking /v2
// handler is recovered into the registry-plane 500 — spec body plus the
// api-version header, never the errors[] envelope. The panicking adapter
// rides the full production chain through the same Deps seam.
func TestV2PanicRendersSpecBody(t *testing.T) {
	h := newHarness(t)
	inner := docker.New(h.svc, docker.NewStaticRepoLookup(nil), nil, nil, nil,
		docker.Options{AnonymousAccess: true}, nil)
	s := httpapi.New(httpapi.Deps{
		Config:   config.Defaults(),
		Auth:     h.authSvc,
		Authz:    h.authSvc,
		Metadata: h.md,
		Repos:    h.md.Repos(),
		Adapters: []adapter.Handler{panicV2Adapter{inner}},
	}, nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/v2/team1/app/manifests/latest")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), `"status"`) {
		t.Fatalf("/v2 panic body carries the /binflow envelope: %s", body)
	}
	if got := resp.Header.Get("Docker-Distribution-Api-Version"); got != "registry/2.0" {
		t.Fatalf("api-version header = %q", got)
	}
	if !strings.Contains(string(body), `"code":"UNKNOWN"`) {
		t.Fatalf("body = %s, want spec UNKNOWN code", body)
	}

	// The same stack's /binflow panic keeps the envelope form (plane split
	// both ways — this is the pre-existing behavior, asserted so the split
	// cannot silently collapse).
	s2 := httpapi.New(httpapi.Deps{
		Config:   config.Defaults(),
		Auth:     h.authSvc,
		Authz:    h.authSvc,
		Metadata: h.md,
		Repos:    h.md.Repos(),
		Console:  http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("binflow boom") }),
	}, nil)
	ts2 := httptest.NewServer(s2.Handler())
	defer ts2.Close()
	resp2, err := ts2.Client().Get(ts2.URL + "/binflow/")
	if err != nil {
		t.Fatalf("get binflow: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	body2, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode != http.StatusInternalServerError {
		t.Fatalf("/binflow panic status = %d", resp2.StatusCode)
	}
	if !strings.Contains(string(body2), `"status"`) {
		t.Fatalf("/binflow panic body lost the envelope: %s", body2)
	}
}

// panicV2Adapter wraps the docker adapter and panics on every request,
// preserving the Protocol key so dispatch reaches it.
type panicV2Adapter struct{ inner *docker.Handler }

func (p panicV2Adapter) Protocol() string    { return p.inner.Protocol() }
func (p panicV2Adapter) RepoTypes() []string { return p.inner.RepoTypes() }
func (p panicV2Adapter) Layout(r *http.Request) (string, string, error) {
	return p.inner.Layout(r)
}
func (p panicV2Adapter) ServeHTTP(http.ResponseWriter, *http.Request) { panic("v2 boom") }

// TestV2RepoLookupFailureIs500 (review B2): a genuine repository-lookup
// error is a spec-body 500 (UNKNOWN) with an ERROR log — never a
// NAME_UNKNOWN 404 that would read as "image missing" to docker clients.
func TestV2RepoLookupFailureIs500(t *testing.T) {
	h := newHarness(t)
	lines, logger, mu := newCapturingLogger()
	handler := docker.New(h.svc, failingRepoLookup{}, h.authSvc, h.authSvc, h.md.Users(),
		docker.Options{AnonymousAccess: true}, logger)
	s := httpapi.New(httpapi.Deps{
		Config:   config.Defaults(),
		Auth:     h.authSvc,
		Authz:    h.authSvc,
		Metadata: h.md,
		Repos:    h.md.Repos(),
		Adapters: []adapter.Handler{handler},
	}, logger)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/v2/team1/app/manifests/latest")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Docker-Distribution-Api-Version"); got != "registry/2.0" {
		t.Fatalf("api-version header = %q", got)
	}
	var eb specErrorBody
	if err := json.Unmarshal(body, &eb); err != nil || len(eb.Errors) != 1 || eb.Errors[0].Code != "UNKNOWN" {
		t.Fatalf("body = %s, want one spec UNKNOWN entry", body)
	}
	mu.Lock()
	logged := strings.Join(*lines, "\n")
	mu.Unlock()
	if !strings.Contains(logged, "repository lookup failed") {
		t.Fatalf("no ERROR log for the lookup failure; logs:\n%s", logged)
	}
}

// failingRepoLookup always fails with a transport-flavored error (the B2
// scenario: DB busy/locked — distinct from not-found).
type failingRepoLookup struct{}

func (failingRepoLookup) Get(context.Context, string) (docker.RepoRow, error) {
	return nil, errors.New("database is locked")
}

// List fails the same way (T-40 added the catalog's enumeration seam).
func (failingRepoLookup) List(context.Context) ([]docker.RepoRow, error) {
	return nil, errors.New("database is locked")
}

// TestV2CatalogPlaceholder (review B3, updated for T-40): the catalog is
// implemented now, so /v2/_catalog and its query variants answer the real
// listing — and the original property holds: a repository row named
// "_catalog" can never capture the registry-level route (the seeded repo
// has no manifests, so it appears in nobody's catalog either).
func TestV2CatalogPlaceholder(t *testing.T) {
	h := newHarness(t)
	if err := h.md.Repos().Create(t.Context(), &metadata.Repo{
		RepoKey: "_catalog", Type: repo.TypeLocal, PackageType: "docker",
	}); err != nil {
		t.Fatalf("seed _catalog repo: %v", err)
	}
	for _, path := range []string{"/v2/_catalog", "/v2/_catalog?n=10"} {
		resp := h.do(http.MethodGet, path, "", "", nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d; body=%s", path, resp.StatusCode, body)
		}
		if !strings.Contains(body, `"repositories":[]`) {
			t.Fatalf("%s body = %q, want the empty catalog listing", path, body)
		}
	}
}

// TestV2MethodNotAllowedHasAllow (review N5): the 405 carries RFC 9110's
// mandatory Allow header (authenticated: the unauthenticated ping
// challenges before the method check since D44-1).
func TestV2MethodNotAllowedHasAllow(t *testing.T) {
	h := newHarness(t)
	resp := h.do(http.MethodPost, "/v2/", adminUser, adminPass, nil, nil)
	mustGet(t, resp)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow = %q, want \"GET, HEAD\"", allow)
	}
}

// TestV2UnavailableWithoutAdapter (review N4): when assembly mounts no
// docker adapter, /v2 answers the spec-body 404 (never an envelope, never
// a 500), and the health endpoint reports the registry subsystem degraded.
func TestV2UnavailableWithoutAdapter(t *testing.T) {
	md, err := metadata.Open(t.Context(), metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/n.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = md.Close() }()
	authSvc := auth.NewFromStore(md, true)
	s := httpapi.New(httpapi.Deps{
		Config:   config.Defaults(),
		Auth:     authSvc,
		Authz:    authSvc,
		Metadata: md,
		Repos:    md.Repos(),
	}, nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/v2/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Docker-Distribution-Api-Version"); got != "registry/2.0" {
		t.Fatalf("api-version header = %q", got)
	}
	if strings.Contains(string(body), `"status"`) {
		t.Fatalf("body carries the /binflow envelope: %s", body)
	}

	// Health reports the degraded registry subsystem (add-only field).
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/binflow/api/v1/health", nil)
	req.SetBasicAuth(adminUser, adminPass)
	hresp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	defer func() { _ = hresp.Body.Close() }()
	hbody, _ := io.ReadAll(hresp.Body)
	var health struct {
		Registry struct{ Status string } `json:"registry"`
	}
	if err := json.Unmarshal(hbody, &health); err != nil {
		t.Fatalf("health body %s: %v", hbody, err)
	}
	if health.Registry.Status != "error" {
		t.Fatalf("registry subsystem = %q, want error; body=%s", health.Registry.Status, hbody)
	}
}
