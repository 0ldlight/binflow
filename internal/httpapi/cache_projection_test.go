package httpapi_test

// T-530 (F1): the <K>-cache projection face on the wire
// (remote-cache-projection.md section 2.1): GET /<remote>-cache/<path>
// serves a landed copy byte-exact, a miss is the ordinary 404 with zero
// upstream contact, the /api/storage read face resolves, the projection
// key never appears in the repository list, and no write verb gets through.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/config"
)

// countingUpstream serves one fixed path and counts requests.
func countingUpstream(t *testing.T, path, body string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == path {
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestCacheProjectionWireChain: the full direct-download face.
func TestCacheProjectionWireChain(t *testing.T) {
	h := newHarness(t)
	srv, hits := countingUpstream(t, "/cached/it.bin", "wire-up-bytes")
	// allowPrivateUpstream: the counting upstream is a loopback listener —
	// the SSRF chain's own exemption knob (the repo-layer suites ride the
	// same flag).
	if status, body := putRepoStatus(t, h, "rem",
		`{"rclass":"remote","packageType":"generic","allowPrivateUpstream":true,"url":"`+srv.URL+`"}`); status != http.StatusOK {
		t.Fatalf("create remote: %d %s", status, body)
	}

	// Warm the remote through its own key (the only face that pulls).
	resp := h.do(http.MethodGet, "/binflow/rem/cached/it.bin", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("warm GET = %d", resp.StatusCode)
	}
	drain(resp)
	warm := hits.Load()

	getBody := func(path string) (int, string) {
		resp := h.do(http.MethodGet, path, adminUser, adminPass, nil, nil)
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode, string(b)
	}

	// The projection serves the standing copy byte-exact.
	code, body := getBody("/binflow/rem-cache/cached/it.bin")
	if code != http.StatusOK || body != "wire-up-bytes" {
		t.Fatalf("projection GET = %d %q, want 200 %q", code, body, "wire-up-bytes")
	}
	if got := hits.Load(); got != warm {
		t.Fatalf("projection GET hit the upstream %d times, want 0", got-warm)
	}

	// A never-fetched path: the ordinary 404, zero upstream.
	code, _ = getBody("/binflow/rem-cache/never.bin")
	if code != http.StatusNotFound {
		t.Fatalf("projection miss = %d, want 404", code)
	}
	if got := hits.Load(); got != warm {
		t.Fatalf("projection miss hit the upstream %d times, want 0 (a miss never pulls)", got-warm)
	}

	// The /api/storage read face resolves the projection key.
	code, body = getBody("/binflow/api/storage/rem-cache/cached/it.bin")
	if code != http.StatusOK {
		t.Fatalf("api/storage projection = %d %s, want 200", code, body)
	}
	var item struct {
		Repo string `json:"repo"`
	}
	if err := json.Unmarshal([]byte(body), &item); err != nil || item.Repo != "rem-cache" {
		t.Fatalf("api/storage body = %s, want repo %q (err %v)", body, "rem-cache", err)
	}

	// The repository list carries no projection entity.
	code, body = getBody("/binflow/api/repositories")
	if code != http.StatusOK {
		t.Fatalf("repository list = %d", code)
	}
	if strings.Contains(body, `"rem-cache"`) {
		t.Fatalf("repository list carries the projection key: %s", body)
	}

	// Writes through the projection key fall to the standard chain: the
	// repository lookup misses the (nonexistent) projection row.
	resp = h.do(http.MethodPut, "/binflow/rem-cache/up.bin", adminUser, adminPass, []byte("x"), nil)
	putBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(putBody), "Failed to find the repository 'rem-cache'") {
		t.Fatalf("projection PUT = %d %s, want the unknown-repository 404", resp.StatusCode, string(putBody))
	}
}

// TestCacheProjectionWireFacesThatStayStandard: spellings that must NOT get
// the projection treatment — a parent that is not a remote, an anonymous
// caller (anonymous off on this stack), and the direct remote key still
// pulling.
func TestCacheProjectionWireFacesThatStayStandard(t *testing.T) {
	// Anonymous access OFF for the 401 leg (the default stack follows
	// ADR-0009 and allows anonymous content reads).
	h := newHarnessCfg(t, func(c *config.Config) { c.Security.AnonymousAccess = false }, nil)
	srv, hits := countingUpstream(t, "/cached/it.bin", "wire-up-bytes")
	if status, body := putRepoStatus(t, h, "rem",
		`{"rclass":"remote","packageType":"generic","allowPrivateUpstream":true,"url":"`+srv.URL+`"}`); status != http.StatusOK {
		t.Fatalf("create remote: %d %s", status, body)
	}
	if status, body := putRepoStatus(t, h, "loc",
		`{"rclass":"local","packageType":"generic"}`); status != http.StatusOK {
		t.Fatalf("create local: %d %s", status, body)
	}

	// A local parent has no projection: the standard unknown-repo 404.
	resp := h.do(http.MethodGet, "/binflow/loc-cache/x.bin", adminUser, adminPass, nil, nil)
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(b), "Failed to find the repository 'loc-cache'") {
		t.Fatalf("local -cache GET = %d %s, want the standard 404", resp.StatusCode, string(b))
	}

	// Anonymous (anonymous access off): the content-plane challenge. The
	// intercept sits before enforce on purpose, so the challenge is the
	// SERVICE gate's 401 rendered by the adapter face.
	resp = h.do(http.MethodGet, "/binflow/rem-cache/cached/it.bin", "", "", nil, nil)
	drain(resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous projection GET = %d, want 401", resp.StatusCode)
	}

	// The remote key itself keeps pulling (the only face that contacts the
	// upstream).
	resp = h.do(http.MethodGet, "/binflow/rem/cached/it.bin", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remote GET = %d", resp.StatusCode)
	}
	drain(resp)
	if got := hits.Load(); got != 1 {
		t.Fatalf("remote GET upstream hits = %d, want 1", got)
	}
}
