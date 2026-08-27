package helm

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// layoutReq builds one adapter-shaped request (the goproxy test posture).
func layoutReq(raw string) *http.Request {
	if u, err := url.ParseRequestURI("/" + raw); err == nil {
		u.Path = strings.TrimPrefix(u.Path, "/")
		return &http.Request{Method: http.MethodGet, URL: u}
	}
	return &http.Request{Method: http.MethodGet, URL: &url.URL{Path: raw}}
}

// The wire-grammar route table (helm.md section 2) — table-driven over
// parseRoute, including the reserved-prefix precedence and the shared
// path validation.
func TestParseRouteTable(t *testing.T) {
	tests := []struct {
		rel  string
		want routeKind
	}{
		{"", kindRoot},
		{"index.yaml", kindIndex},
		{"mychart-0.1.0.tgz", kindChart},
		{"sub/mychart-0.1.0.tgz", kindChart},
		{"mychart-0.1.0.tgz.prov", kindProv},
		{"sub/mychart-0.1.0.tgz.prov", kindProv},
		{"mychart-0.1.0.tar.gz", kindTarGz},
		{"README.md", kindBareContent},
		{"notes.txt", kindBareContent},
		{"_external", kindExternal},
		{"_external/https/example.com/x.tgz", kindExternal},
		{"_transitive/https/example.com/y.tgz", kindTransitive},
		// The prefix families win over suffix matches.
		{"_external/https/example.com/pkg.tar.gz", kindExternal},
		{".index/sha/x/index.yaml", kindBareContent}, // the virtual cache root stays addressable as storage
	}
	for _, tt := range tests {
		if got := parseRoute(tt.rel); got.kind != tt.want {
			t.Errorf("parseRoute(%q) = %v, want %v", tt.rel, got.kind, tt.want)
		}
	}
}

func TestLayoutValidation(t *testing.T) {
	for _, bad := range []string{
		"helm-local/../etc/passwd",
		"helm-local/a//b",
		"helm-local/a\\b",
		"api/x",
		"v2/x",
	} {
		if _, _, err := layout(layoutReq(bad)); err == nil {
			t.Errorf("layout(%q) accepted an illegal path", bad)
		}
	}
	if key, rel, err := layout(layoutReq("helm-local/sub/x.tgz")); err != nil || key != "helm-local" || rel != "sub/x.tgz" {
		t.Fatalf("layout decode = (%q,%q,%v)", key, rel, err)
	}
	// Percent-encoded dot-dot is decoded before validation.
	if _, _, err := layout(layoutReq("helm-local/%2e%2e/x")); err == nil {
		t.Error("layout accepted an encoded dot-dot escape")
	}
}
