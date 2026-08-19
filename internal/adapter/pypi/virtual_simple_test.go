package pypi

// T-72: the virtual simple-index collection matrix (FR-21-AC5). Every case
// runs the FULL httpapi stack (real dispatch seam, real service, real
// engine) against a counting mock upstream, the same posture as the T-82
// render tests.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// pypiVirtualFixture seeds a local member (pyv-a), a remote member behind
// the mock upstream and a virtual over them; the upstream serves both
// HTML and JSON project pages plus the files they reference.
type pypiVirtualFixture struct {
	s        *stack
	upstream *httptest.Server
}

func newPyPIVirtualFixture(t *testing.T, virtualConfig string, upstreamJSON bool) *pypiVirtualFixture {
	t.Helper()
	s := newStackCfg(t, nil)
	ctx := context.Background()

	f := &pypiVirtualFixture{s: s}
	upSdist := "upstream-sdist"
	upWheel := "upstream-wheel"
	f.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/simple/mix-pkg/" && upstreamJSON:
			w.Header().Set("Content-Type", simpleJSONCT)
			_, _ = w.Write([]byte(`{"meta":{"api-version":"2.0"},"name":"mix-pkg","files":[` +
				`{"filename":"mix_pkg-2.0.0.tar.gz","url":"../../packages/mix-pkg/2.0.0/mix_pkg-2.0.0.tar.gz#sha256=aa","hashes":{"sha256":"aa"},"size":3}]}`))
		case r.URL.Path == "/simple/mix-pkg/":
			w.Header().Set("Content-Type", simpleHTMLMediaType)
			_, _ = w.Write([]byte(indexHead +
				`<a href="../../packages/mix-pkg/2.0.0/mix_pkg-2.0.0.tar.gz#sha256=aa">mix_pkg-2.0.0.tar.gz</a>` + "\n" +
				indexFoot))
		case r.URL.Path == "/packages/mix-pkg/2.0.0/mix_pkg-2.0.0.tar.gz":
			_, _ = w.Write([]byte(upSdist))
		case r.URL.Path == "/simple/up-only/":
			w.Header().Set("Content-Type", simpleHTMLMediaType)
			_, _ = w.Write([]byte(indexHead +
				`<a href="../../packages/up-only/1.0.0/up_only-1.0.0-py3-none-any.whl#sha256=bb">up_only-1.0.0-py3-none-any.whl</a>` + "\n" +
				indexFoot))
		case r.URL.Path == "/packages/up-only/1.0.0/up_only-1.0.0-py3-none-any.whl":
			_, _ = w.Write([]byte(upWheel))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.upstream.Close)

	rows := []*metadata.Repo{
		{RepoKey: "pyv-a", Type: repo.TypeLocal, PackageType: repo.PackagePypi},
		{RepoKey: "pyv-rem", Type: repo.TypeRemote, PackageType: repo.PackagePypi,
			Config: `{"url":"` + f.upstream.URL + `","allowPrivateUpstream":true}`},
		{RepoKey: "pyv-virt", Type: repo.TypeVirtual, PackageType: repo.PackagePypi, Config: virtualConfig},
	}
	for _, row := range rows {
		if _, err := s.svc.CreateRepo(ctx, &repo.Principal{Name: "admin", Admin: true}, row); err != nil {
			t.Fatalf("CreateRepo(%s): %v", row.RepoKey, err)
		}
	}
	return f
}

// TestVirtualSimpleMergeMatrix: the same project spread over a local and a
// remote member collects BOTH members' entries into one page (sorted,
// deduplicated), each entry's href resolving through the virtual's
// packages/ mount whichever member holds it.
func TestVirtualSimpleMergeMatrix(t *testing.T) {
	f := newPyPIVirtualFixture(t, `{"repositories":["pyv-a","pyv-rem"]}`, false)

	// The local member holds 1.0.0; the remote member's page adds 2.0.0.
	if status, body := f.s.upload("/binflow/api/pypi/pyv-a", map[string]string{
		":action": "file_upload", "name": "mix-pkg", "version": "1.0.0",
	}, "mix_pkg-1.0.0.tar.gz", []byte("local-sdist")); status != http.StatusOK {
		t.Fatalf("seed upload = %d, body %s", status, body)
	}

	status, body, hdr := f.s.get("/binflow/api/pypi/pyv-virt/simple/mix-pkg/")
	if status != http.StatusOK {
		t.Fatalf("virtual project page = %d, body %s", status, body)
	}
	// The remote member's entry href carries the upstream's own packages/
	// prefix in its BinFlow path (the member's cache mirrors the upstream
	// layout), so it reads one "packages/" deeper than the local entry's.
	for _, want := range []string{
		`>mix_pkg-1.0.0.tar.gz</a>`,
		`>mix_pkg-2.0.0.tar.gz</a>`,
		`../../packages/packages/mix-pkg/2.0.0/mix_pkg-2.0.0.tar.gz#sha256=aa`,
		`<meta name="api-version" value="2" />`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("merged page missing %q:\n%s", want, body)
		}
	}
	// Sorted by filename: 1.0.0 precedes 2.0.0.
	if strings.Index(body, "mix_pkg-1.0.0.tar.gz") > strings.Index(body, "mix_pkg-2.0.0.tar.gz") {
		t.Errorf("merged entries not sorted by filename:\n%s", body)
	}
	if hdr.Get("Content-Type") != simpleHTMLMediaType {
		t.Errorf("merged page content type = %q", hdr.Get("Content-Type"))
	}

	// Both entries download through the virtual: the local member's copy
	// first-hit, the remote member's pulled through and cached.
	status, lbody, _ := f.s.get("/binflow/api/pypi/pyv-virt/packages/mix-pkg/1.0.0/mix_pkg-1.0.0.tar.gz")
	if status != http.StatusOK || lbody != "local-sdist" {
		t.Fatalf("local entry via virtual = %d %q", status, lbody)
	}
	status, rbody, rhdr := f.s.get("/binflow/api/pypi/pyv-virt/packages/packages/mix-pkg/2.0.0/mix_pkg-2.0.0.tar.gz")
	if status != http.StatusOK || rbody != "upstream-sdist" {
		t.Fatalf("remote entry via virtual = %d %q", status, rbody)
	}
	if got := rhdr.Get(repo.HdrResolvedFrom); got != "pyv-rem" {
		t.Errorf("remote entry Resolved-From = %q, want pyv-rem", got)
	}

	// The project only the remote member knows: same collection, one
	// contributor.
	status, body, _ = f.s.get("/binflow/api/pypi/pyv-virt/simple/up-only/")
	if status != http.StatusOK || !strings.Contains(body, "up_only-1.0.0-py3-none-any.whl") {
		t.Fatalf("remote-only project page = %d (%s)", status, body)
	}

	// Unknown project: the honest 404, both members walked.
	if status, _, _ = f.s.get("/binflow/api/pypi/pyv-virt/simple/ghost/"); status != http.StatusNotFound {
		t.Fatalf("unknown project through virtual = %d, want 404", status)
	}
}

// TestVirtualSimpleJSONFallback: a JSON-requesting client gets JSON only
// when every contributing member can serve it — one HTML-only member flips
// the WHOLE page to HTML (the spec's whole-response fallback rule).
func TestVirtualSimpleJSONFallback(t *testing.T) {
	f := newPyPIVirtualFixture(t, `{"repositories":["pyv-a","pyv-rem"]}`, false)
	if status, body := f.s.upload("/binflow/api/pypi/pyv-a", map[string]string{
		":action": "file_upload", "name": "mix-pkg", "version": "1.0.0",
	}, "mix_pkg-1.0.0.tar.gz", []byte("local-sdist")); status != http.StatusOK {
		t.Fatalf("seed upload = %d, body %s", status, body)
	}
	jsonAccept := map[string]string{"Accept": simpleJSONMediaType}

	// The mixed virtual: the remote member is HTML-only -> HTML.
	resp := f.s.do(http.MethodGet, "/binflow/api/pypi/pyv-virt/simple/mix-pkg/", "", "", nil, jsonAccept)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fallback page = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != simpleHTMLMediaType {
		t.Errorf("mixed page content type = %q, want the HTML fallback", ct)
	}
	_ = resp.Body.Close()

	// A local-only virtual: JSON negotiates normally.
	if _, err := f.s.svc.CreateRepo(context.Background(), &repo.Principal{Name: "admin", Admin: true}, &metadata.Repo{
		RepoKey: "pyv-loconly", Type: repo.TypeVirtual, PackageType: repo.PackagePypi,
		Config: `{"repositories":["pyv-a"]}`,
	}); err != nil {
		t.Fatalf("CreateRepo(pyv-loconly): %v", err)
	}
	resp = f.s.do(http.MethodGet, "/binflow/api/pypi/pyv-loconly/simple/mix-pkg/", "", "", nil, jsonAccept)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("local-only JSON page = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != simpleJSONCT {
		t.Errorf("local-only JSON content type = %q", ct)
	}
	_ = resp.Body.Close()
}

// TestVirtualSimpleUpstreamJSONMember: an upstream serving the PEP 691 JSON
// form parses into the same entry set (the sniffer, not the Accept header,
// decides the member's format — the engine fetch carries none).
func TestVirtualSimpleUpstreamJSONMember(t *testing.T) {
	f := newPyPIVirtualFixture(t, `{"repositories":["pyv-rem"]}`, true)
	status, body, _ := f.s.get("/binflow/api/pypi/pyv-virt/simple/mix-pkg/")
	if status != http.StatusOK || !strings.Contains(body, "mix_pkg-2.0.0.tar.gz") {
		t.Fatalf("JSON upstream member page = %d (%s)", status, body)
	}
	status, rbody, _ := f.s.get("/binflow/api/pypi/pyv-virt/packages/packages/mix-pkg/2.0.0/mix_pkg-2.0.0.tar.gz")
	if status != http.StatusOK || rbody != "upstream-sdist" {
		t.Fatalf("JSON member entry download = %d %q", status, rbody)
	}
}

// TestVirtualSimpleMemberFaults: one member's classified failure does not
// block the others (the PRD rule); a collection with nothing gathered
// surfaces the remembered failure instead of masking it as 404.
func TestVirtualSimpleMemberFaults(t *testing.T) {
	ctx := context.Background()
	f := newPyPIVirtualFixture(t, `{"repositories":["pyv-a","pyv-rem"]}`, false)

	// The faulting member: no loopback exemption -> the engine's SSRF 400.
	// Both rows go through the SERVICE so the member ledger is written.
	if _, err := f.s.svc.CreateRepo(ctx, &repo.Principal{Name: "admin", Admin: true}, &metadata.Repo{
		RepoKey: "pyv-ssrf", Type: repo.TypeRemote, PackageType: repo.PackagePypi,
		Config: `{"url":"` + f.upstream.URL + `"}`,
	}); err != nil {
		t.Fatalf("seed pyv-ssrf: %v", err)
	}
	if _, err := f.s.svc.CreateRepo(ctx, &repo.Principal{Name: "admin", Admin: true}, &metadata.Repo{
		RepoKey: "pyv-fmix", Type: repo.TypeVirtual, PackageType: repo.PackagePypi,
		Config: `{"repositories":["pyv-a","pyv-ssrf"]}`,
	}); err != nil {
		t.Fatalf("seed pyv-fmix: %v", err)
	}
	if status, body := f.s.upload("/binflow/api/pypi/pyv-a", map[string]string{
		":action": "file_upload", "name": "mix-pkg", "version": "1.0.0",
	}, "mix_pkg-1.0.0.tar.gz", []byte("local-sdist")); status != http.StatusOK {
		t.Fatalf("seed upload = %d, body %s", status, body)
	}

	// The healthy local member still answers through the faulting one.
	if status, body, _ := f.s.get("/binflow/api/pypi/pyv-fmix/simple/mix-pkg/"); status != http.StatusOK ||
		!strings.Contains(body, "mix_pkg-1.0.0.tar.gz") {
		t.Fatalf("one member's fault blocked the page (status %d, %s)", status, body)
	}

	// Nothing gathered anywhere: the remembered failure is the answer.
	if status, body, _ := f.s.get("/binflow/api/pypi/pyv-fmix/simple/ghost/"); status != http.StatusBadRequest ||
		!strings.Contains(body, "suppressed upstream") {
		t.Fatalf("all-member-fault page = %d (%s), want the engine's 400", status, body)
	}
}

// TestRemoteRepositoryProjectPage: a BARE remote repository serves its
// upstream page remapped onto its own packages/ mount (the M46/M47 face
// T-70 left at the transitional refusal) — the single-member degenerate
// case of the same collection.
func TestRemoteRepositoryProjectPage(t *testing.T) {
	f := newPyPIVirtualFixture(t, `{"repositories":["pyv-a","pyv-rem"]}`, false)

	status, body, _ := f.s.get("/binflow/api/pypi/pyv-rem/simple/up-only/")
	if status != http.StatusOK {
		t.Fatalf("remote project page = %d (%s)", status, body)
	}
	if !strings.Contains(body, `../../packages/packages/up-only/1.0.0/up_only-1.0.0-py3-none-any.whl#sha256=bb`) {
		t.Errorf("remote page did not remap the upstream href:\n%s", body)
	}
	// The download pulls through the remote repository itself, at the
	// upstream-mirrored path the href names.
	status, rbody, hdr := f.s.get("/binflow/api/pypi/pyv-rem/packages/packages/up-only/1.0.0/up_only-1.0.0-py3-none-any.whl")
	if status != http.StatusOK || rbody != "upstream-wheel" {
		t.Fatalf("remote page entry download = %d %q", status, rbody)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("remote entry X-BinFlow-Cache = %q, want MISS", got)
	}

	// An upstream-unknown project is the plain 404.
	if status, _, _ = f.s.get("/binflow/api/pypi/pyv-rem/simple/ghost/"); status != http.StatusNotFound {
		t.Fatalf("remote unknown project = %d, want 404", status)
	}
}
