package pypi

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// reqWithPath builds a request whose EscapedPath is exactly the given
// spelling (the router contract: the escaped form reaches Layout
// verbatim). url.Parse is used so Path carries the DECODED form and
// RawPath the escaped one — the same pair net/http hands the router.
func reqWithPath(t *testing.T, escaped string) *http.Request {
	t.Helper()
	u, err := url.Parse(escaped)
	if err != nil {
		// Control bytes and other spellings Parse refuses still reach the
		// layout in production through the raw-TCP path; the decoded form
		// is irrelevant there because the rejection fires before any
		// segment analysis.
		u = &url.URL{Path: escaped}
	}
	return &http.Request{Method: http.MethodGet, URL: u}
}

// TestLayoutMatrix pins the PyPI layout split: the bare repository root is
// a legal address (twine POSTs to .../api/pypi/<repo> with no trailing
// slash), protocol segments are ordinary segments, and the shared artifact
// path defenses (dot segments, double slashes, controls, reserved keys,
// length caps) reject with ErrBadRequestPath.
func TestLayoutMatrix(t *testing.T) {
	h := New(nil, nil, nil, nil)
	tests := []struct {
		name    string
		escaped string
		repo    string
		rel     string
		wantErr bool
	}{
		{"bare repo root", "/pypi-local", "pypi-local", "", false},
		{"root with slash", "/pypi-local/", "pypi-local", "", false},
		{"simple root", "/pypi-local/simple/", "pypi-local", "simple/", false},
		{"simple no slash", "/pypi-local/simple", "pypi-local", "simple", false},
		{"project page", "/pypi-local/simple/demo-pkg/", "pypi-local", "simple/demo-pkg/", false},
		{"packages path", "/pypi-local/packages/Demo/1.0/Demo-1.0.whl", "pypi-local", "packages/Demo/1.0/Demo-1.0.whl", false},
		{"bare content path", "/pypi-local/Demo/1.0/Demo-1.0.whl", "pypi-local", "Demo/1.0/Demo-1.0.whl", false},
		{"legacy json path", "/pypi-local/pypi/demo/json", "pypi-local", "pypi/demo/json", false},
		{"encoded project name", "/pypi-local/simple/demo%2Dpkg/", "pypi-local", "simple/demo-pkg/", false},
		{"empty path", "/", "", "", true},
		{"reserved repo key api", "/api/simple/x", "", "", true},
		{"reserved repo key v2", "/v2/x", "", "", true},
		{"dot segment", "/pypi-local/a/../b", "", "", true},
		{"encoded dot segment", "/pypi-local/simple/%2e%2e/", "", "", true},
		{"double slash", "/pypi-local//simple/", "", "", true},
		{"trailing double slash", "/pypi-local/simple//", "", "", true},
		{"control byte", "/pypi-local/simple/de\x00mo/", "", "", true},
		{"backslash", `/pypi-local/simple\demo`, "", "", true},
		{"repo key too long", "/" + strings.Repeat("a", 64) + "/x", "", "", true},
		{"path too long", "/pypi-local/" + strings.Repeat("a", 513), "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repoKey, rel, err := h.Layout(reqWithPath(t, tc.escaped))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Layout(%q) = %q, %q; want an error", tc.escaped, repoKey, rel)
				}
				if !strings.Contains(err.Error(), "bad request path") {
					t.Fatalf("error %q does not carry the ErrBadRequestPath context", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Layout(%q): %v", tc.escaped, err)
			}
			if repoKey != tc.repo || rel != tc.rel {
				t.Fatalf("Layout(%q) = (%q, %q); want (%q, %q)", tc.escaped, repoKey, rel, tc.repo, tc.rel)
			}
		})
	}
}

// TestRoutingMatrix pins the protocol's verb map through the real stack:
// the domain root answers GET/HEAD (probe) and POST (upload); every other
// route family is read-only with an Allow header on 405.
func TestRoutingMatrix(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0-py3-none-any.whl", []byte("wheel-bytes"))

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantAllow  string
	}{
		{"root probe", http.MethodGet, "/binflow/api/pypi/pypi-local/", http.StatusOK, ""},
		{"root probe HEAD", http.MethodHead, "/binflow/api/pypi/pypi-local/", http.StatusOK, ""},
		{"root probe no slash", http.MethodGet, "/binflow/api/pypi/pypi-local", http.StatusOK, ""},
		{"root probe bare mount", http.MethodGet, "/binflow/pypi-local/", http.StatusOK, ""},
		{"root PUT", http.MethodPut, "/binflow/api/pypi/pypi-local/", http.StatusMethodNotAllowed, "GET, HEAD, POST"},
		{"root DELETE", http.MethodDelete, "/binflow/api/pypi/pypi-local/", http.StatusMethodNotAllowed, "GET, HEAD, POST"},
		{"simple POST", http.MethodPost, "/binflow/api/pypi/pypi-local/simple/", http.StatusMethodNotAllowed, "GET, HEAD"},
		{"simple project DELETE", http.MethodDelete, "/binflow/api/pypi/pypi-local/simple/demo-pkg/", http.StatusMethodNotAllowed, "GET, HEAD"},
		{"packages PUT", http.MethodPut, "/binflow/api/pypi/pypi-local/packages/demo-pkg/1.0.0/demo_pkg-1.0.0-py3-none-any.whl", http.StatusMethodNotAllowed, "GET, HEAD"},
		{"packages DELETE", http.MethodDelete, "/binflow/api/pypi/pypi-local/packages/demo-pkg/1.0.0/demo_pkg-1.0.0-py3-none-any.whl", http.StatusMethodNotAllowed, "GET, HEAD"},
		{"bare content PUT", http.MethodPut, "/binflow/pypi-local/demo-pkg/1.0.0/demo_pkg-1.0.0-py3-none-any.whl", http.StatusMethodNotAllowed, "GET, HEAD"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := s.do(tc.method, tc.path, adminUser, adminPass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if got := resp.Header.Get("Allow"); tc.wantAllow != "" && got != tc.wantAllow {
				t.Fatalf("Allow = %q, want %q", got, tc.wantAllow)
			}
		})
	}
}

// TestLegacyJSONAPIAlways404 pins PE-04/M58: the warehouse legacy JSON API
// (/pypi/<name>/json and per-version shape) answers the E-26-family
// not-implemented 404 on every method — BinFlow deliberately does not host
// it, and the probe must be able to tell.
func TestLegacyJSONAPIAlways404(t *testing.T) {
	s := newStack(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/binflow/api/pypi/pypi-local/pypi/demo-pkg/json"},
		{http.MethodGet, "/binflow/api/pypi/pypi-local/pypi/demo-pkg/1.0.0/json"},
		{http.MethodPost, "/binflow/api/pypi/pypi-local/pypi/demo-pkg/json"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			status, body, _ := s.get(tc.path)
			if status != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", status)
			}
			if !strings.Contains(body, "not implemented") {
				t.Fatalf("body %q lacks the not-implemented wording", body)
			}
		})
	}
}

// TestProtocolSegmentsShadowBarePaths documents the reserved-prefix rule:
// a project literally named "packages"/"simple"/"pypi" is stored and
// indexed normally, and downloads through the packages/ mount work — only
// its BARE content entrance is shadowed by the protocol prefix (the same
// namespace collision every prefixed simple-index implementation has).
func TestProtocolSegmentsShadowBarePaths(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "packages", "1.0.0", "packages-1.0.0.tar.gz", []byte("pk"))

	status, body, _ := s.get("/binflow/api/pypi/pypi-local/simple/packages/")
	if status != http.StatusOK {
		t.Fatalf("simple page status = %d, want 200", status)
	}
	if !strings.Contains(body, "packages-1.0.0.tar.gz") {
		t.Fatalf("simple page lacks the file entry: %s", body)
	}

	status, body, _ = s.get("/binflow/api/pypi/pypi-local/packages/packages/1.0.0/packages-1.0.0.tar.gz")
	if status != http.StatusOK || body != "pk" {
		t.Fatalf("packages-mount download = %d %q, want 200 %q", status, body, "pk")
	}

	// The bare entrance is shadowed: the first segment routes to the
	// packages/ family and the remainder (1.0.0/...) is not a file.
	status, _, _ = s.get("/binflow/pypi-local/packages/1.0.0/packages-1.0.0.tar.gz")
	if status != http.StatusNotFound {
		t.Fatalf("shadowed bare path status = %d, want 404 (documented reservation)", status)
	}
}

// TestRepoTypesDeclaresAllClasses pins the declarative class set: every M3
// repository class carries package_type=pypi rows and dispatches here
// (FR-15-AC1) — the remote/virtual engines live behind repo.Service.
func TestRepoTypesDeclaresAllClasses(t *testing.T) {
	got := New(nil, nil, nil, nil).RepoTypes()
	want := []string{repo.TypeLocal, repo.TypeRemote, repo.TypeVirtual}
	if len(got) != len(want) {
		t.Fatalf("RepoTypes() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("RepoTypes() = %v, want %v", got, want)
		}
	}
}

// fakeService satisfies repo.Service minimally for the Register wiring
// test (the registries only store the pointer; nothing is invoked).
type fakeService struct{ repo.Service }

// TestRegisterWiring pins the T-63 review N2 contract: the single Register
// call enters BOTH process-wide registries under one literal, so the
// handler's dispatch key and the metadata provider's protocol can never
// drift. It runs exactly once per test binary (the registry panics on
// duplicates — the assembly-bug contract under test).
func TestRegisterWiring(t *testing.T) {
	h := Register(fakeService{}, fakeClassReader{}, nil, nil)
	if h.Protocol() != Protocol {
		t.Fatalf("handler protocol = %q, want %q", h.Protocol(), Protocol)
	}
	if got, ok := adapter.ForRepoType(Protocol); !ok || got != adapter.Handler(h) {
		t.Fatalf("ForRepoType(%q) did not resolve to the registered handler", Protocol)
	}
	p, ok := adapter.ForProtocol(Protocol)
	if !ok {
		t.Fatalf("ForProtocol(%q) found no provider", Protocol)
	}
	if p.Protocol() != Protocol {
		t.Fatalf("provider protocol = %q, want %q", p.Protocol(), Protocol)
	}
}

// fakeClassReader satisfies repo.ClassReader for the wiring test.
type fakeClassReader struct{}

func (fakeClassReader) Get(_ context.Context, _ string) (*metadata.Repo, error) {
	return nil, metadata.ErrRepoNotFound
}

// compile-time pins of the seams the package consumes.
var (
	_ repo.ClassReader = fakeClassReader{}
	_ BlobLedger       = metadata.BlobStore(nil)
)
