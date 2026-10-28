// T-630: the bare-root homepage face (wave-1 differential vs Artifactory
// 7.161.26). GET / serves the console shell straight from the root — 200
// text/html, byte-identical to /binflow/ui/, no redirect leg — while
// /binflow and /binflow/ answer 302 with an ABSOLUTE Location derived from
// the request (Host header, X-Forwarded-Proto-aware scheme). Every other
// prefix-less path keeps the E-26① hint 404, and /binflow/ui/ stays a 200.

package httpapi_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRootHomepage(t *testing.T) {
	h := newHarness(t)

	// Alignment oracle: the ui-segment shell, fetched through the same
	// assembled chain the table below drives.
	uiRec := httptest.NewRecorder()
	uiReq := httptest.NewRequest(http.MethodGet, "/binflow/ui/", nil)
	h.srv.Config.Handler.ServeHTTP(uiRec, uiReq) //nolint:errcheck // probing the assembled handler directly
	if uiRec.Code != http.StatusOK {
		t.Fatalf("/binflow/ui/ regression baseline = %d, want 200", uiRec.Code)
	}
	uiBody := uiRec.Body.Bytes()

	tests := []struct {
		name       string
		method     string // default GET
		path       string
		host       string // request Host override; "" keeps httptest's example.com
		xfp        string // X-Forwarded-Proto header value; "" sends none
		wantStatus int
		wantCT     string // exact Content-Type; "" skips the assertion
		wantBody   []byte // exact body; nil skips the assertion
		wantLoc    string // exact Location; "" skips the assertion
		wantInBody string // substring assertion; "" skips it
	}{
		{
			name:       "bare root serves the ui shell: 200 text/html, same bytes",
			path:       "/",
			wantStatus: http.StatusOK,
			wantCT:     "text/html; charset=utf-8",
			wantBody:   uiBody,
		},
		{
			name:       "mount root redirects 302 to the absolute ui URL",
			path:       "/binflow/",
			wantStatus: http.StatusFound,
			wantLoc:    "http://example.com/binflow/ui/",
		},
		{
			name:       "mount root without the trailing slash redirects the same way",
			path:       "/binflow",
			wantStatus: http.StatusFound,
			wantLoc:    "http://example.com/binflow/ui/",
		},
		{
			name:       "Location host comes from the request Host header",
			path:       "/binflow/",
			host:       "artifacts.example.com:8443",
			wantStatus: http.StatusFound,
			wantLoc:    "http://artifacts.example.com:8443/binflow/ui/",
		},
		{
			name:       "Location scheme honors X-Forwarded-Proto behind a TLS terminator",
			path:       "/binflow/",
			host:       "artifacts.example.com",
			xfp:        "https",
			wantStatus: http.StatusFound,
			wantLoc:    "https://artifacts.example.com/binflow/ui/",
		},
		{
			name:       "ui segment regression: the shell still serves 200 html",
			path:       "/binflow/ui/",
			wantStatus: http.StatusOK,
			wantCT:     "text/html; charset=utf-8",
		},
		{
			name:       "another prefix-less path keeps the hint 404",
			path:       "/favicon.ico",
			wantStatus: http.StatusNotFound,
			wantInBody: "no root mirror",
		},
		{
			name:       "non-GET on the root keeps the hint 404 (no homepage mirror)",
			method:     http.MethodPost,
			path:       "/",
			wantStatus: http.StatusNotFound,
			wantInBody: "no root mirror",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tt.path, nil)
			if tt.host != "" {
				req.Host = tt.host
			}
			if tt.xfp != "" {
				req.Header.Set("X-Forwarded-Proto", tt.xfp)
			}
			rec := httptest.NewRecorder()
			h.srv.Config.Handler.ServeHTTP(rec, req) //nolint:errcheck // probing the assembled handler directly

			if rec.Code != tt.wantStatus {
				t.Fatalf("GET %s status = %d, want %d (body: %.200s)",
					tt.path, rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantCT != "" {
				if got := rec.Header().Get("Content-Type"); got != tt.wantCT {
					t.Fatalf("GET %s Content-Type = %q, want %q", tt.path, got, tt.wantCT)
				}
			}
			if tt.wantBody != nil && !bytes.Equal(rec.Body.Bytes(), tt.wantBody) {
				t.Fatalf("GET %s body differs from /binflow/ui/ (root %d bytes, ui %d bytes)",
					tt.path, rec.Body.Len(), len(tt.wantBody))
			}
			if tt.wantLoc != "" {
				if got := rec.Header().Get("Location"); got != tt.wantLoc {
					t.Fatalf("GET %s Location = %q, want %q", tt.path, got, tt.wantLoc)
				}
			}
			if tt.wantInBody != "" && !strings.Contains(rec.Body.String(), tt.wantInBody) {
				t.Fatalf("GET %s body has no %q substring: %.200s", tt.path, tt.wantInBody, rec.Body.String())
			}
		})
	}
}
