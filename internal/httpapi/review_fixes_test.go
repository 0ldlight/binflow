package httpapi_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/httpapi"
)

// chainWithAccessLog assembles the same head httpapi uses in production
// (requestID -> accessLog -> recover) around a test terminal handler,
// capturing the access-log output. It bypasses CORS and the auth pair,
// which the B3/M1 assertions do not exercise.
func chainWithAccessLog(t *testing.T, sink io.Writer, terminal http.Handler) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(sink, nil))
	return httpapi.ChainHeadForTest(logger, terminal)
}

// ---- B1: /readyz must fail when the data directory is not writable ----

// TestReadyzFailsOnReadOnlyDataDir drives /readyz against a data directory
// the process cannot write into: the probe must answer 503 (review B1 — a
// read-only instance must stop receiving traffic), while /healthz stays
// 200 (liveness is about the process, not the dependencies).
func TestReadyzFailsOnReadOnlyDataDir(t *testing.T) {
	roDir := t.TempDir()
	// Build a subdirectory the test then strips write permission from;
	// root ignores mode bits, so skip under root where the probe would
	// still succeed and the assertion would be wrong.
	roSub := filepath.Join(roDir, "ro")
	if err := os.Mkdir(roSub, 0o555); err != nil {
		t.Fatalf("mkdir ro: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roSub, 0o755) })
	if amRoot() {
		t.Skip("running as root: mode bits are not enforced, probe would succeed")
	}

	h := newHarnessCfg(t, func(*mutatedConfig) {}, nil)
	// Point the server's DataDir at the read-only subdirectory by
	// rebuilding a harness-equivalent server with the overridden probe
	// root. The harness builds its own dir, so drive the assembled
	// handler directly with a swapped Deps.DataDir through a fresh server.
	s := h.rebuildWithDataDir(t, roSub)

	resp := s.do(http.MethodGet, "/readyz")
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/readyz status = %d, want 503; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "storage not ready") {
		t.Fatalf("/readyz body = %q, want the storage-not-ready detail", body)
	}

	resp = s.do(http.MethodGet, "/healthz")
	if body := mustGet(t, resp); resp.StatusCode != http.StatusOK || body != "OK" {
		t.Fatalf("/healthz = %d %q, want 200 OK", resp.StatusCode, body)
	}
}

// amRoot reports whether the test runs with uid 0 (mode bits ignored).
func amRoot() bool { return os.Geteuid() == 0 }

// rebuiltServer drives a second harness server with an overridden DataDir
// (the B1 probe target) while sharing the metadata store.
type rebuiltServer struct{ ts *httptest.Server }

func (b *rebuiltServer) do(method, path string) *http.Response {
	req, err := http.NewRequest(method, b.ts.URL+path, nil)
	if err != nil {
		panic(err)
	}
	resp, err := b.ts.Client().Do(req)
	if err != nil {
		panic(err)
	}
	return resp
}

// ---- B2: the dispatch table carries exactly one ping route ----

// TestPingRouteOnceAndMethodCoverage pins the B2 fix: system/ping is
// routed by a single case; the surrounding surface behaves as designed
// (GET passes, other verbs fall to E-26②'s not-implemented 404, which is
// the documented M1 posture — a 405+Allow refinement is T-15's call).
func TestPingRouteOnceAndMethodCoverage(t *testing.T) {
	h := newHarness(t)

	resp := h.do(http.MethodGet, "/binflow/api/system/ping", "", "", nil, nil)
	if body := mustGet(t, resp); resp.StatusCode != http.StatusOK || body != "OK" {
		t.Fatalf("GET ping = %d %q", resp.StatusCode, body)
	}

	for _, method := range []string{http.MethodPost, http.MethodPut} {
		resp := h.do(method, "/binflow/api/system/ping", "", "", nil, nil)
		eb := decodeError(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s ping status = %d, want 404 (E-26②)", method, resp.StatusCode)
		}
		if !strings.Contains(eb.Errors[0].Message, "not implemented") {
			t.Fatalf("%s ping message = %q", method, eb.Errors[0].Message)
		}
	}
}

// ---- B3: the access log records the trailing error, not the 200 ----

// TestStatusRecorderRecordsLateWriteHeader drives a handler that writes
// body bytes first and calls WriteHeader(500) afterwards: the access log
// must record 500 (B3), not the 200 the wire committed.
func TestStatusRecorderRecordsLateWriteHeader(t *testing.T) {
	var logged strings.Builder
	late := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("partial-bytes"))
		w.WriteHeader(http.StatusInternalServerError)
	})
	handler := chainWithAccessLog(t, &logged, late)

	req := httptest.NewRequest(http.MethodGet, "/binflow/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !strings.Contains(logged.String(), "status=500") {
		t.Fatalf("access log did not record the late 500; log=%q", logged.String())
	}
	if got := rec.Body.String(); got != "partial-bytes" {
		t.Fatalf("body = %q", got)
	}
}

// ---- M1: a mid-stream panic must not append the 500 envelope ----

// TestRecoverMidStreamPanicNoAppend drives a handler that writes half a
// body and then panics: the response bytes must end with the partial body
// (no JSON envelope grafted on), and the access log must still show the
// request with status 500 (review M1).
func TestRecoverMidStreamPanicNoAppend(t *testing.T) {
	var logged strings.Builder
	mid := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ARTIFACT-PREFIX-BYTES"))
		panic("mid-stream failure")
	})
	handler := chainWithAccessLog(t, &logged, mid)

	req := httptest.NewRequest(http.MethodGet, "/binflow/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Body.String(); got != "ARTIFACT-PREFIX-BYTES" {
		t.Fatalf("response body = %q, want the partial body only (no envelope appended)", got)
	}
	if !strings.Contains(logged.String(), "panic recovered") {
		t.Fatalf("no panic log line; log=%q", logged.String())
	}
	if !strings.Contains(logged.String(), "status=500") {
		t.Fatalf("access log did not record 500 for the mid-stream panic; log=%q", logged.String())
	}
}

// TestRecoverBeforeWriteStillEnvelopes: the un-committed panic path keeps
// the original behavior — envelope 500, no stray body bytes.
func TestRecoverBeforeWriteStillEnvelopes(t *testing.T) {
	var logged strings.Builder
	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("pre-write") })
	handler := chainWithAccessLog(t, &logged, boom)

	req := httptest.NewRequest(http.MethodGet, "/binflow/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var eb struct {
		Errors []struct {
			Status  int    `json:"status"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &eb); err != nil || len(eb.Errors) != 1 {
		t.Fatalf("body = %q, want the errors[] envelope", rec.Body.String())
	}
}

// ---- M2: encoded repo keys route; ACL keys off the decoded string ----

// TestEncodedRepoKeyRoutes verifies the M2 fix from the functional side:
// a repository key whose characters the client percent-encodes
// ("generic%2Dlocal" for "generic-local") routes to the adapter and
// serves content — the authorization, repo-row lookup and layout now key
// off the same decoded string.
func TestEncodedRepoKeyRoutes(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	resp := h.do(http.MethodPut, "/binflow/generic%2Dlocal/acme/enc.bin", adminUser, adminPass,
		[]byte("encoded-key-body"), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT via encoded key status = %d, want 201; body=%s",
			resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()

	resp = h.do(http.MethodGet, "/binflow/generic%2Dlocal/acme/enc.bin", "", "", nil, nil)
	if body := mustGet(t, resp); resp.StatusCode != http.StatusOK || body != "encoded-key-body" {
		t.Fatalf("GET via encoded key = %d %q", resp.StatusCode, body)
	}

	// The encoded reserved segment is NOT decoded into a route: /%61pi is
	// "api" after decoding, and the dispatch must treat it as the reserved
	// segment (content-path 404, never the API surface).
	resp = h.do(http.MethodGet, "/binflow/%61pi/system/ping", "", "", nil, nil)
	eb := decodeError(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /binflow/%%61pi/... status = %d, want 404", resp.StatusCode)
	}
	if !strings.Contains(eb.Errors[0].Message, "not implemented") &&
		!strings.Contains(eb.Errors[0].Message, "Failed to find the repository") {
		t.Fatalf("message = %q", eb.Errors[0].Message)
	}
}

// TestEncodedSlashACLConsistent verifies the M2 security note: an encoded
// slash inside the first segment cannot smuggle a path past the ACL —
// after decoding, "%2F" is a slash, the segment is not a legal repo key,
// and the request fails closed with a 404 rather than routing anywhere.
func TestEncodedSlashACLConsistent(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	resp := h.do(http.MethodGet, "/binflow/generic-local/priv%2Fsecret.bin", "", "", nil, nil)
	eb := decodeError(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (encoded slash cannot name a node)", resp.StatusCode)
	}
	if !strings.Contains(eb.Errors[0].Message, "Failed to find the requested resource") &&
		!strings.Contains(eb.Errors[0].Message, "not implemented") {
		t.Fatalf("message = %q", eb.Errors[0].Message)
	}
}
