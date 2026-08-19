package pypi

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/storage"
)

// adminPrincipal is the seeded evaluation admin, for direct Service calls.
func adminPrincipal() *auth.Principal { return &auth.Principal{Name: adminUser, Admin: true} }

// zeroRef declares no client digests (the service computes its own).
func zeroRef() storage.BlobRef { return storage.BlobRef{} }

// sha1Hex is the ETag source digest for the download contract.
func sha1Hex(b []byte) string {
	sum := sha1.Sum(b) //nolint:gosec // the M1 download contract's ETag, not a security choice
	return hex.EncodeToString(sum[:])
}

// TestDownloadBothEntrances pins PE-03's two-entrance rule: the packages/
// mount the index hrefs target and the bare content path address the SAME
// node with the same body and the same M1 header contract.
func TestDownloadBothEntrances(t *testing.T) {
	s := newStack(t)
	content := []byte("distribution body")
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0-py3-none-any.whl", content)

	paths := []string{
		"/binflow/api/pypi/pypi-local/packages/demo-pkg/1.0.0/demo_pkg-1.0.0-py3-none-any.whl",
		"/binflow/pypi-local/demo-pkg/1.0.0/demo_pkg-1.0.0-py3-none-any.whl",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			status, body, hdr := s.get(path)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200", status)
			}
			if body != string(content) {
				t.Fatalf("body = %q, want the stored bytes", body)
			}
			if got := hdr.Get("X-Checksum-Sha256"); got != sha256Hex(content) {
				t.Fatalf("X-Checksum-Sha256 = %q, want %q", got, sha256Hex(content))
			}
			if got := hdr.Get("ETag"); got != sha1Hex(content) {
				t.Fatalf("ETag = %q, want the unquoted sha1 %q", got, sha1Hex(content))
			}
			if got := hdr.Get("X-Checksum-Md5"); got != md5Hex(content) {
				t.Fatalf("X-Checksum-Md5 = %q, want %q", got, md5Hex(content))
			}
			if got := hdr.Get("Accept-Ranges"); got != "bytes" {
				t.Fatalf("Accept-Ranges = %q, want bytes", got)
			}
			if got := hdr.Get("Content-Type"); got != "application/zip" {
				t.Fatalf("Content-Type = %q, want the stored wheel mime", got)
			}
			if got := hdr.Get("Content-Length"); got != strconv.Itoa(len(content)) {
				t.Fatalf("Content-Length = %q, want %d", got, len(content))
			}
		})
	}
}

// TestDownloadNotFound pins the 404 wording family: unknown file, unknown
// project folder, and the bare packages/ segment.
func TestDownloadNotFound(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0.tar.gz", []byte("x"))
	for _, path := range []string{
		"/binflow/api/pypi/pypi-local/packages/demo-pkg/1.0.0/no-such-file.whl",
		"/binflow/api/pypi/pypi-local/packages/",
		"/binflow/api/pypi/pypi-local/packages",
		"/binflow/api/pypi/pypi-local/demo-pkg/1.0.0/",
		"/binflow/api/pypi/pypi-local/unknown-proj/1.0.0/x.whl",
	} {
		status, body, _ := s.get(path)
		if status != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404 (body %s)", path, status, body)
		}
		if !strings.Contains(body, "Failed to find the requested resource") {
			t.Fatalf("%s: body %q lacks the download 404 wording", path, body)
		}
	}
}

// TestDownloadRange pins the M1 range contract on distribution paths:
// single-range 206 slices, suffix ranges, unsatisfiable 416 with the
// bytes */total form, and multi-range ignoring to a full 200.
func TestDownloadRange(t *testing.T) {
	s := newStack(t)
	content := []byte("0123456789")
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0.tar.gz", content)
	path := "/binflow/api/pypi/pypi-local/packages/demo-pkg/1.0.0/demo_pkg-1.0.0.tar.gz"

	tests := []struct {
		name        string
		rangeHeader string
		wantStatus  int
		wantBody    string
		wantRange   string
	}{
		{"slice", "bytes=2-5", http.StatusPartialContent, "2345", "bytes 2-5/10"},
		{"open end", "bytes=7-", http.StatusPartialContent, "789", "bytes 7-9/10"},
		{"suffix", "bytes=-3", http.StatusPartialContent, "789", "bytes 7-9/10"},
		{"clamp", "bytes=3-999", http.StatusPartialContent, "3456789", "bytes 3-9/10"},
		{"unsatisfiable", "bytes=10-11", http.StatusRequestedRangeNotSatisfiable, "", "bytes */10"},
		{"malformed", "bytes=8-2", http.StatusRequestedRangeNotSatisfiable, "", "bytes */10"},
		{"multi-range ignored", "bytes=0-1,3-4", http.StatusOK, string(content), ""},
		{"non-bytes ignored", "items=0-1", http.StatusOK, string(content), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := s.do(http.MethodGet, path, "", "", nil, map[string]string{"Range": tc.rangeHeader})
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if got := resp.Header.Get("Content-Range"); got != tc.wantRange {
				t.Fatalf("Content-Range = %q, want %q", got, tc.wantRange)
			}
			if tc.wantBody != "" && readAll(t, resp) != tc.wantBody {
				t.Fatalf("body = %q, want %q", readAll(t, resp), tc.wantBody)
			}
		})
	}
}

// TestDownloadConditional pins the conditional half of the M1 contract on
// distribution paths: If-None-Match (weak) and If-Modified-Since both
// answer 304 with no body.
func TestDownloadConditional(t *testing.T) {
	s := newStack(t)
	content := []byte("conditional body")
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0.tar.gz", content)
	path := "/binflow/api/pypi/pypi-local/packages/demo-pkg/1.0.0/demo_pkg-1.0.0.tar.gz"

	etag := `"` + sha1Hex(content) + `"`
	for _, tc := range []struct {
		name  string
		hdr   map[string]string
		match bool
	}{
		{"weak etag", map[string]string{"If-None-Match": "W/" + etag}, true},
		{"etag miss", map[string]string{"If-None-Match": `"nope"`}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := s.do(http.MethodGet, path, "", "", nil, tc.hdr)
			defer func() { _ = resp.Body.Close() }()
			want := http.StatusOK
			if tc.match {
				want = http.StatusNotModified
			}
			if resp.StatusCode != want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, want)
			}
		})
	}

	// If-Modified-Since with a future date.
	_, _, hdr := s.get(path)
	lastMod := hdr.Get("Last-Modified")
	if lastMod == "" {
		t.Fatal("download carries no Last-Modified")
	}
	resp := s.do(http.MethodGet, path, "", "", nil, map[string]string{"If-Modified-Since": lastMod})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("If-Modified-Since with the served Last-Modified: status = %d, want 304", resp.StatusCode)
	}
}

// TestDownloadHead pins HEAD parity: same headers, no body.
func TestDownloadHead(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0.tar.gz", []byte("head-body"))
	resp := s.do(http.MethodHead, "/binflow/api/pypi/pypi-local/packages/demo-pkg/1.0.0/demo_pkg-1.0.0.tar.gz",
		"", "", nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.ContentLength != int64(len("head-body")) {
		t.Fatalf("Content-Length = %d, want 9", resp.ContentLength)
	}
}

// TestDownloadUnknownRepo pins the repo-not-found wording through the
// protocol mount (the router resolves the row before the adapter, so this
// doubles as the seam's unknown-repo contract).
func TestDownloadUnknownRepo(t *testing.T) {
	s := newStack(t)
	status, body, _ := s.get("/binflow/api/pypi/no-such-repo/simple/x/")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
	if !strings.Contains(body, "Failed to find the repository 'no-such-repo'") {
		t.Fatalf("body %q lacks the repo 404 wording", body)
	}
}

// TestSeedViaServiceListing proves the index only counts 3-segment FILE
// nodes: a deeper node seeded through the service (the generic plane can
// still address a pypi repository's namespace) never surfaces as a project
// entry, and a folder-only project stays 404.
func TestSeedViaServiceListing(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	_, err := s.svc.Put(ctx, adminPrincipal(), "pypi-local",
		"nested/deeper/still/file.txt", strings.NewReader("x"),
		zeroRef(), "text/plain")
	if err != nil {
		t.Fatalf("seed nested node: %v", err)
	}

	_, body, _ := s.get("/binflow/api/pypi/pypi-local/simple/")
	if strings.Contains(body, "nested") {
		t.Fatalf("root index lists a non-package node:\n%s", body)
	}
	status, _, _ := s.get("/binflow/api/pypi/pypi-local/simple/nested/")
	if status != http.StatusNotFound {
		t.Fatalf("folder-only project: status = %d, want 404", status)
	}
}
