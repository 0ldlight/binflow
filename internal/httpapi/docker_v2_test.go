package httpapi_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"

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

// TestV2PingAnonymousOpen (D04, DE-01): with anonymous access on (the
// default), /v2 and /v2/ answer 200 {} to anonymous GET — the probe docker
// login performs before anything else.
func TestV2PingAnonymousOpen(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/v2", "/v2/"} {
		resp := h.do(http.MethodGet, path, "", "", nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d; body=%s", path, resp.StatusCode, body)
		}
		if body != "{}" {
			t.Fatalf("%s body = %q, want {}", path, body)
		}
		assertV2Headers(t, resp)
	}
}

// TestV2PingAnonymousClosed (D04 second half, ADR-0010 clause 4): with
// anonymous access off, the same probe answers 401 + the Bearer challenge
// whose realm is the adapter's own /v2/token endpoint and whose service is
// "binflow". The PRD v1.0 wording (realm=/binflow/api/security/token) was
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
	want := fmt.Sprintf(`Bearer realm="%s/v2/token",service="binflow"`, h.srv.URL)
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
	want := `Bearer realm="https://registry.example.com/v2/token",service="binflow"`
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
			name:    "repo with image falls through to the foundation 404",
			method:  http.MethodGet,
			path:    "/v2/team1/app/manifests/latest",
			status:  http.StatusNotFound,
			message: "not implemented",
		},
		{
			name:    "nested image name resolves the same repo key",
			method:  http.MethodGet,
			path:    "/v2/team1/acme/app/manifests/sha256:abc",
			status:  http.StatusNotFound,
			message: "not implemented",
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
// endpoint's method failure. The status field of the /binflow envelope is
// the discriminator: the registry schema has no "status" member.
func TestV2ErrorEnvelopeIsolation(t *testing.T) {
	h := newHarness(t)

	resp := h.do(http.MethodPost, "/v2/", "", "", nil, nil)
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
