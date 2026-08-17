package httpapi_test

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

// TestE26FullMatrix: every unmapped root path and every unimplemented
// /binflow/api endpoint answers 404 + the errors[] envelope with "not
// implemented" wording — never a 500, never an empty 200 (PRD E-26,
// C24).
func TestE26FullMatrix(t *testing.T) {
	h := newHarness(t)

	tests := []struct {
		path string
		hint bool // message must mention /binflow
	}{
		{"/v2/", false},
		{"/v2/_catalog", false},
		{"/artifactory/api/system/ping", true},
		{"/artifactory/libs-release-local/x.jar", true},
		{"/api/system/info", true},
		{"/whatever", true},
		{"/binflow/v2/", false},
		{"/binflow/v2/blobs/uploads", false},
		{"/binflow/api/npm/xx", false},
		{"/binflow/api/pypi/simple", false},
		{"/binflow/api/pypi-ui/packages", false},
		{"/binflow/api/search/artifact", false},
		{"/binflow/api/replication", false},
		{"/binflow/api/system/info", false},
		{"/binflow/api/system/configuration", false},
		{"/binflow/api/builds", false},
		{"/binflow/api", false},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			resp := h.do(http.MethodGet, tc.path, "", "", nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", resp.StatusCode)
			}
			eb := decodeError(t, resp)
			if eb.Errors[0].Status != http.StatusNotFound {
				t.Fatalf("envelope status = %d", eb.Errors[0].Status)
			}
			msg := eb.Errors[0].Message
			if !strings.Contains(strings.ToLower(msg), "not implemented") &&
				!strings.Contains(msg, "/binflow") {
				t.Fatalf("message %q carries neither the not-implemented wording nor the prefix hint", msg)
			}
			if tc.hint && !strings.Contains(msg, "/binflow") {
				t.Fatalf("message %q missing the /binflow prefix hint", msg)
			}
		})
	}
}

// TestDotSegmentReachesAdapterUnnormalized is the routing core of
// FR-4-AC10: a raw request line with dot segments must reach the adapter
// layout verbatim and come back as the adapter's 400 — never as net/http's
// cleanPath 3xx redirect. The Go client refuses to send such a request
// line, so the probe goes over raw TCP (with Basic auth inline so the
// request passes the write gate and dies at the layout defense).
func TestDotSegmentReachesAdapterUnnormalized(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	for _, path := range []string{
		"/binflow/generic-local/a/../../etc/passwd",
		"/binflow/generic-local/a/./b",
	} {
		t.Run(path, func(t *testing.T) { probeRawPut(t, h, path) })
	}

	// A leading double slash escapes neither as a redirect nor a silent
	// rewrite: the empty first segment misses every repository row and
	// answers the envelope 404 (empty-segment rule, FR-4-AC11 defense at
	// the dispatch boundary).
	t.Run("/binflow//generic-local//x", func(t *testing.T) {
		conn, err := net.Dial("tcp", h.srv.Listener.Addr().String())
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() { _ = conn.Close() }()
		_, _ = fmt.Fprintf(conn, "PUT /binflow//generic-local//x HTTP/1.1\r\nHost: t\r\nAuthorization: Basic %s\r\nContent-Length: 1\r\nConnection: close\r\n\r\nz", base64Admin())
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			t.Fatalf("redirect %d to %q — normalization leaked", resp.StatusCode, resp.Header.Get("Location"))
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		decodeError(t, resp)
	})
}

// probeRawPut sends one un-normalized PUT over raw TCP with admin
// credentials and asserts the adapter layout's 400 (never a redirect).
func probeRawPut(t *testing.T, h *harness, path string) {
	t.Helper()
	conn, err := net.Dial("tcp", h.srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = fmt.Fprintf(conn, "PUT %s HTTP/1.1\r\nHost: t\r\nAuthorization: Basic %s\r\nContent-Length: 1\r\nConnection: close\r\n\r\nz",
		path, base64Admin())
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		t.Fatalf("redirect %d to %q — cleanPath normalization leaked in front of the adapter",
			resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400 from the adapter layout; body=%s", resp.StatusCode, string(body))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("400 body content-type = %q", ct)
	}
	decodeError(t, resp)
}

// base64Admin is the Basic header value for the seeded evaluation admin.
func base64Admin() string {
	return base64.StdEncoding.EncodeToString([]byte(adminUser + ":" + adminPass))
}

// TestEncodedDotSegmentReachesAdapter: the percent-encoded traversal
// variant (%2e%2e) survives the router untouched as well.
func TestEncodedDotSegmentReachesAdapter(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	for _, path := range []string{
		"/binflow/generic-local/acme/%2e%2e/%2e%2e/etc/passwd",
		"/binflow/generic-local/a/..%2fb",
	} {
		resp := h.do(http.MethodPut, path, adminUser, adminPass, []byte("z"), nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400; body=%s", path, resp.StatusCode, body)
		}
		if !strings.Contains(body, `"errors"`) {
			t.Fatalf("%s: body %q is not the envelope", path, body)
		}
	}
}

// TestContentRoundtripThroughRouter: upload and download through the full
// router (prefix strip + principal seam + adapter dispatch) with real
// bytes, proving the dispatch path preserves the body, the checksum
// headers and the 201/200 contract.
func TestContentRoundtripThroughRouter(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	content := []byte("the artifact body")
	sum := sha256Hex(content)

	resp := h.do(http.MethodPut, "/binflow/generic-local/acme/artifact.bin", adminUser, adminPass,
		content, map[string]string{"X-Checksum-Sha256": sum})
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT status = %d; body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-Checksum-Sha256"); got != sum {
		t.Fatalf("response X-Checksum-Sha256 = %q, want %q", got, sum)
	}
	var created struct {
		Repo string `json:"repo"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("PUT body not FileInfo JSON: %v (%s)", err, body)
	}
	if created.Repo != "generic-local" || created.Path != "/acme/artifact.bin" {
		t.Fatalf("FileInfo repo/path = %q/%q", created.Repo, created.Path)
	}

	resp = h.do(http.MethodGet, "/binflow/generic-local/acme/artifact.bin", "", "", nil, nil)
	got := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d", resp.StatusCode)
	}
	if got != string(content) {
		t.Fatalf("GET body mismatch: %q", got)
	}
	if resp.Header.Get("X-Checksum-Sha256") != sum {
		t.Fatalf("GET X-Checksum-Sha256 = %q", resp.Header.Get("X-Checksum-Sha256"))
	}
}

// TestUnknownRepoIs404Envelope: a content path under a repo key with no
// repository row answers the spec-worded 404, envelope-shaped.
func TestUnknownRepoIs404Envelope(t *testing.T) {
	h := newHarness(t)
	resp := h.do(http.MethodGet, "/binflow/no-such-repo/x.bin", "", "", nil, nil)
	eb := decodeError(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if !strings.Contains(eb.Errors[0].Message, "Failed to find the repository 'no-such-repo'") {
		t.Fatalf("message = %q", eb.Errors[0].Message)
	}
}
