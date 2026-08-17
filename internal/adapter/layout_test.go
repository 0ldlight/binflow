package adapter

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func layoutReq(t *testing.T, raw string) *http.Request {
	t.Helper()
	// Server-side parsing: http.Server populates URL.Path (decoded) and
	// RawPath (escaped, when it differs). ParseRequestURI reproduces that;
	// inputs it refuses (odd characters) fall back to a hand-built URL
	// whose Path is the raw string (Layout then decodes EscapedPath, which
	// escapes Path itself).
	if u, err := url.ParseRequestURI("/" + raw); err == nil {
		u.Path = strings.TrimPrefix(u.Path, "/")
		return &http.Request{Method: http.MethodGet, URL: u}
	}
	return &http.Request{Method: http.MethodGet, URL: &url.URL{Path: raw}}
}

func TestLayoutTable(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantKey string
		wantRel string
		wantErr bool
	}{
		{name: "plain", path: "generic-local/acme/a.bin", wantKey: "generic-local", wantRel: "acme/a.bin"},
		{name: "deep", path: "r/a/b/c/d.tar.gz", wantKey: "r", wantRel: "a/b/c/d.tar.gz"},
		{name: "folder trailing slash", path: "r/acme/", wantKey: "r", wantRel: "acme/"},
		{name: "single segment", path: "r", wantErr: true},
		{name: "empty", path: "", wantErr: true},
		{name: "bare slash", path: "/", wantErr: true},
		{name: "empty key", path: "/a.bin", wantErr: true},
		{name: "empty rel", path: "r/", wantErr: true},
		{name: "double slash", path: "r/a//b", wantErr: true},
		{name: "leading dot segment", path: "r/./a", wantErr: true},
		{name: "dot escape", path: "r/a/../../etc/passwd", wantErr: true},
		{name: "encoded dot-dot", path: "r/a/%2e%2e/b", wantErr: true},
		{name: "encoded double-dot uppercase", path: "r/%2E%2E/b", wantErr: true},
		{name: "encoded dot", path: "r/%2e/a", wantErr: true},
		{name: "dot as final segment", path: "r/a/..", wantErr: true},
		{name: "backslash", path: `r/a\b`, wantErr: true},
		{name: "reserved api", path: "api/repositories", wantErr: true},
		{name: "reserved v2", path: "v2/blobs/uploads", wantErr: true},
		{name: "oversize key", path: "r" + string(make([]byte, 0)) + "/a", wantKey: "r", wantRel: "a"},
		{name: "oversize path", path: "r/" + longSeg(600), wantErr: true},
		{name: "path at limit", path: "r/" + longSeg(400) + "/" + longSeg(100), wantKey: "r"},
		{name: "malformed pct stays opaque", path: "r/%zz", wantKey: "r", wantRel: "%zz"}, // PathUnescape rejects; Layout keeps raw
		{name: "pct-encoded slash decodes to separator", path: "r/a%2Fb", wantKey: "r", wantRel: "a/b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, rel, err := Layout(layoutReq(t, tt.path))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Layout(%q) = (%q,%q,nil), want error", tt.path, key, rel)
				}
				if !errors.Is(err, ErrBadRequestPath) {
					t.Fatalf("Layout(%q) err = %v, want wrap of ErrBadRequestPath", tt.path, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Layout(%q) err = %v", tt.path, err)
			}
			if key != tt.wantKey || (tt.wantRel != "" && rel != tt.wantRel) {
				t.Fatalf("Layout(%q) = (%q,%q), want key %q rel %q", tt.path, key, rel, tt.wantKey, tt.wantRel)
			}
		})
	}
}

func longSeg(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

func TestLayoutDecodeBeforeValidation(t *testing.T) {
	// %2e%2e decodes to ".." and must be judged as such (FR-4-AC10/NFR-S4):
	// decode-then-validate, never validate-then-decode.
	if _, _, err := Layout(layoutReq(t, "generic-local/a/%2e%2e/%2e%2e/etc/passwd")); !errors.Is(err, ErrBadRequestPath) {
		t.Fatalf("encoded traversal accepted: %v", err)
	}
	// A legal percent-encoded filename survives decoding intact.
	key, rel, err := Layout(layoutReq(t, "generic-local/a%20b/c%2Bd.bin"))
	if err != nil {
		t.Fatalf("legal encoded path rejected: %v", err)
	}
	if key != "generic-local" || rel != "a b/c+d.bin" {
		t.Fatalf("decoded path = %q/%q", key, rel)
	}
	// %2e%2e must decode into ".." before validation, not after.
	if _, _, err := Layout(layoutReq(t, "generic-local/%2e%2e/secret")); !errors.Is(err, ErrBadRequestPath) {
		t.Fatalf("encoded leading traversal accepted: %v", err)
	}
	// A literal "%2e%2e" that decodes fine but sits mid-segment is just a
	// filename; only a full dot segment is a traversal.
	if k, r2, err := Layout(layoutReq(t, "generic-local/file%2e%2etail.bin")); err != nil || k != "generic-local" || r2 != "file..tail.bin" {
		t.Fatalf("benign embedded dots rejected: key=%q rel=%q err=%v", k, r2, err)
	}
}
