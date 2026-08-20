package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/config"
)

// errorBody decodes the errors[] envelope (E-01).
type errorBody struct {
	Errors []struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
	} `json:"errors"`
}

func decodeError(t *testing.T, resp *http.Response) errorBody {
	t.Helper()
	body := mustGet(t, resp)
	var eb errorBody
	if err := json.Unmarshal([]byte(body), &eb); err != nil {
		t.Fatalf("body %q is not the errors[] envelope: %v", body, err)
	}
	if len(eb.Errors) == 0 {
		t.Fatalf("body %q has no errors entries", body)
	}
	return eb
}

// mustGet reads and closes the body, returning it as a string.
func mustGet(t *testing.T, resp *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if closeErr != nil {
		t.Fatalf("close body: %v", closeErr)
	}
	return string(body)
}

// TestMiddlewareOrder pins the chain through its observable invariants:
// the response carries X-Request-Id (requestID outermost), exactly one
// access log line is emitted per request (accessLog second), and a
// panicking handler still gets its line with a 500 (accessLog outside
// recover). The authenticator/authorizer pair is asserted by the
// auth-matrix tests, which need the real dependencies.
func TestMiddlewareOrder(t *testing.T) {
	h := newHarness(t)
	h.resetLogs()

	req := httptest.NewRequest(http.MethodGet, "/binflow/", nil)
	rec := httptest.NewRecorder()
	h.srv.Config.Handler.ServeHTTP(rec, req) //nolint:errcheck // probing the assembled handler directly
	// The console seam is T-89's embedded console: /binflow/ answers the
	// CE-01 301 to /binflow/ui/ (the M1 placeholder 200 is terminated).
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("console redirect status = %d, want 301", rec.Code)
	}
	if got := rec.Header().Get("X-Request-Id"); got == "" {
		t.Fatal("X-Request-Id header missing on the response")
	}
	accessLines := 0
	for _, l := range strings.Split(h.logs(), "\n") {
		if strings.Contains(l, "access") && strings.Contains(l, "path=/binflow/") {
			accessLines++
		}
	}
	if accessLines != 1 {
		t.Fatalf("got %d access log lines, want exactly 1; logs:\n%s", accessLines, h.logs())
	}
}

// TestRequestIDUnique: repeated requests never share an id.
func TestRequestIDUnique(t *testing.T) {
	h := newHarness(t)
	seen := make(map[string]bool)
	for i := 0; i < 32; i++ {
		resp := h.do(http.MethodGet, "/binflow/api/system/ping", "", "", nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("ping status = %d", resp.StatusCode)
		}
		id := resp.Header.Get("X-Request-Id")
		if id == "" {
			t.Fatal("empty X-Request-Id")
		}
		if seen[id] {
			t.Fatalf("duplicate request id %q", id)
		}
		seen[id] = true
		defer func() { _ = resp.Body.Close() }()
	}
}

// TestAccessLogFields: one line per request carrying method, path,
// status, duration_ms, remote_addr, user (anonymous for unauthenticated)
// and the byte counts — and never the Authorization header or its value
// (NFR-S3).
func TestAccessLogFields(t *testing.T) {
	h := newHarness(t)
	h.resetLogs()

	resp := h.do(http.MethodGet, "/binflow/api/system/ping", "", "", nil, nil)
	defer func() { _ = resp.Body.Close() }()
	resp = h.do(http.MethodGet, "/binflow/api/system/version", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()

	logs := h.logs()
	for _, want := range []string{
		"method=GET",
		"path=/binflow/api/system/ping",
		"status=200",
		"duration_ms=",
		"remote_addr=",
		"user=anonymous",
		"user=admin",
		"bytes_in=0",
		"bytes_out=2",
		"request_id=",
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("access log missing %q; logs:\n%s", want, logs)
		}
	}
	// NFR-S3: neither the cleartext pair nor the base64 header value may
	// appear in the log stream.
	if strings.Contains(logs, "admin:password") || strings.Contains(logs, "YWRtaW46cGFzc3dvcmQ=") {
		t.Errorf("access log leaks credentials; logs:\n%s", logs)
	}
	if strings.Contains(logs, "Authorization=") {
		t.Errorf("access log records the Authorization header; logs:\n%s", logs)
	}
}

// TestRecoverPanicTo500: a panicking handler answers the envelope 500,
// logs the panic, and still produces its access log line.
func TestRecoverPanicTo500(t *testing.T) {
	s := newPanicConsoleServer(t)
	req := httptest.NewRequest(http.MethodGet, "/binflow/", nil)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var eb errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &eb); err != nil || len(eb.Errors) == 0 {
		t.Fatalf("panic response is not the errors[] envelope: %q", rec.Body.String())
	}
	if !strings.Contains(s.logs(), "panic recovered") {
		t.Fatalf("no panic log line; logs:\n%s", s.logs())
	}
	if !strings.Contains(s.logs(), "access") {
		t.Fatalf("panicking request produced no access log line; logs:\n%s", s.logs())
	}
}

// TestCORSMiddleware: configured origins echo with allow headers; an
// unconfigured origin gets no allow header; preflight answers 204.
func TestCORSMiddleware(t *testing.T) {
	h := newHarnessCfg(t, func(c *config.Config) {
		c.Server.CORSOrigins = []string{"https://console.example"}
	}, nil)

	// Allowed origin.
	resp := h.do(http.MethodGet, "/binflow/api/system/ping", "", "", nil,
		map[string]string{"Origin": "https://console.example"})
	defer func() { _ = resp.Body.Close() }()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://console.example" {
		t.Fatalf("allow-origin = %q", got)
	}

	// Foreign origin: no allow header (same-origin default posture).
	resp = h.do(http.MethodGet, "/binflow/api/system/ping", "", "", nil,
		map[string]string{"Origin": "https://evil.example"})
	defer func() { _ = resp.Body.Close() }()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("foreign origin got allow-origin %q", got)
	}

	// Preflight.
	req, err := http.NewRequest(http.MethodOptions, h.srv.URL+"/binflow/api/system/ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://console.example")
	req.Header.Set("Access-Control-Request-Method", "PUT")
	preflight, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = preflight.Body.Close()
	if preflight.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", preflight.StatusCode)
	}
	if got := preflight.Header.Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Authorization") {
		t.Fatalf("preflight allow-headers = %q, missing auth surface", got)
	}
}
