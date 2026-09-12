package npm

import (
	"net/http"
	"strings"
	"testing"
)

// L012-1 wire alignment of the dist-tag error faces (curl matrix m05/m07/
// m08/m09, conductor rulings D3/D4/D5): the ghost-package 404 is the
// reference's bare "Not found", the bad-version 404 names the VERSION
// position, and a malformed tag body answers 400 with a neutral message that
// never leaks the JSON decoder's internals (the reference's own 500 on these
// cells is its flaw — the ruling keeps the stronger 400).

// TestDistTagWireErrorAlignment walks the four wording cells against a live
// document; every expectation is the reference cell verbatim.
func TestDistTagWireErrorAlignment(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "demo-pkg", "1.0.0", "1.1.0")

	cases := []struct {
		name       string
		method     string
		target     string
		body       string
		wantStatus int
		wantMsg    string
	}{
		{"ghost package GET is bare Not found (m05/D4)", http.MethodGet,
			"/npm-local/-/package/ghost-pkg/dist-tags", "", http.StatusNotFound, `"message": "Not found"`},
		{"bad version 404 names the version position (m07/D3)", http.MethodPut,
			"/npm-local/-/package/demo-pkg/dist-tags/novers", `"9.9.9"`, http.StatusNotFound,
			"npm package not found with name:demo-pkg, and version:9.9.9"},
		{"malformed body is a neutral 400 (m08/D5)", http.MethodPut,
			"/npm-local/-/package/demo-pkg/dist-tags/badbody", "not-a-json-string", http.StatusBadRequest,
			`"message": "invalid dist-tag body"`},
		{"object body is a neutral 400 (m09/D5)", http.MethodPut,
			"/npm-local/-/package/demo-pkg/dist-tags/badobj", `{"v":"1.0.0"}`, http.StatusBadRequest,
			`"message": "invalid dist-tag body"`},
		{"legacy spelling shares the neutral 400", http.MethodPut,
			"/npm-local/demo-pkg/badbody", "not-a-json-string", http.StatusBadRequest,
			`"message": "invalid dist-tag body"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := s.call(tc.method, tc.target, tc.body, adminPrincipal, nil)
			if rr.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rr.Code, tc.wantStatus, bodyOf(rr))
			}
			if !strings.Contains(bodyOf(rr), tc.wantMsg) {
				t.Fatalf("body = %s, want it to contain %s", bodyOf(rr), tc.wantMsg)
			}
			for _, leak := range []string{"invalid character", "cannot unmarshal", "unexpected end"} {
				if strings.Contains(bodyOf(rr), leak) {
					t.Fatalf("body = %s leaks decoder internals (%s)", bodyOf(rr), leak)
				}
			}
		})
	}
}

// TestDistTagsCacheControlHeader: the collection GET pins the reference's
// one-minute client freshness window (D7, the header npm's fetchTags caching
// rides).
func TestDistTagsCacheControlHeader(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "demo-pkg", "1.0.0")

	rr := s.call(http.MethodGet, "/npm-local/-/package/demo-pkg/dist-tags", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("ls status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if got := rr.Header().Get("Cache-Control"); got != "max-age=60" {
		t.Fatalf("Cache-Control = %q, want max-age=60", got)
	}
}
