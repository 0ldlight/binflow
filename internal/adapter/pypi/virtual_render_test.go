package pypi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// T-82: the two T-66/T-71 seams on the pypi transfer plane, over one
// end-to-end virtual fixture (local member first, remote member second,
// counting mock upstream), driven through the FULL httpapi chain:
//
//   - a hinted body stream contributes X-BinFlow-Resolved-From (and the
//     remote member's X-BinFlow-Cache beneath it) to packages/ downloads,
//   - a *repo.StatusError renders verbatim — a remote member fault (the
//     SSRF refusal) propagates through the virtual as its own 400, not the
//     default arm's 500 (the T-71 finding).
//
// The PyPI protocol itself defines no DELETE route (uploads are POST on the
// repository root; every content address is GET/HEAD only), so the
// service's RE-08 delete refusal is unreachable by construction here — the
// DELETE leg asserts the face's honest method-gate 405 instead.
func TestVirtualRenderSeams(t *testing.T) {
	s := newStackCfg(t, nil)
	ctx := context.Background()

	hits := &atomic.Int64{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/up-pkg/1.0.0/up_pkg-1.0.0.tar.gz" {
			_, _ = w.Write([]byte("upstream-sdist"))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(upstream.Close)

	for _, row := range []*metadata.Repo{
		{RepoKey: "pyv-loc", Type: repo.TypeLocal, PackageType: repo.PackagePypi},
		{RepoKey: "pyv-rem", Type: repo.TypeRemote, PackageType: repo.PackagePypi,
			Config: `{"url":"` + upstream.URL + `","allowPrivateUpstream":true}`},
		{RepoKey: "pyv-virt", Type: repo.TypeVirtual, PackageType: repo.PackagePypi,
			Config: `{"repositories":["pyv-loc","pyv-rem"]}`},
	} {
		if _, err := s.svc.CreateRepo(ctx, &repo.Principal{Name: "admin", Admin: true}, row); err != nil {
			t.Fatalf("CreateRepo(%s): %v", row.RepoKey, err)
		}
	}

	// Seed the local member through the twine-shaped upload face.
	if status, body := s.upload("/binflow/api/pypi/pyv-loc", map[string]string{
		":action": "file_upload", "name": "loc-pkg", "version": "1.0.0",
	}, "loc_pkg-1.0.0.tar.gz", []byte("local-sdist")); status != http.StatusOK {
		t.Fatalf("seed upload = %d, body %s", status, body)
	}

	// packages/ via the virtual, local member: Resolved-From names the
	// member; a local hit carries no cache header.
	status, body, hdr := s.get("/binflow/api/pypi/pyv-virt/packages/loc-pkg/1.0.0/loc_pkg-1.0.0.tar.gz")
	if status != http.StatusOK {
		t.Fatalf("virtual GET local member = %d, body %s", status, body)
	}
	if got := hdr.Get(repo.HdrResolvedFrom); got != "pyv-loc" {
		t.Errorf("local hit Resolved-From = %q, want pyv-loc", got)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "" {
		t.Errorf("local hit carries X-BinFlow-Cache %q, want none", got)
	}
	if body != "local-sdist" {
		t.Errorf("local hit body = %q", body)
	}

	// packages/ via the virtual, remote member: both hints on one response.
	status, body, hdr = s.get("/binflow/api/pypi/pyv-virt/packages/up-pkg/1.0.0/up_pkg-1.0.0.tar.gz")
	if status != http.StatusOK {
		t.Fatalf("virtual GET remote member = %d, body %s", status, body)
	}
	if got := hdr.Get(repo.HdrResolvedFrom); got != "pyv-rem" {
		t.Errorf("remote hit Resolved-From = %q, want pyv-rem", got)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("remote hit X-BinFlow-Cache = %q, want MISS", got)
	}
	if body != "upstream-sdist" {
		t.Errorf("remote hit body = %q", body)
	}

	// Repeat: the member's cached copy serves (HIT), the upstream frozen.
	status, body, hdr = s.get("/binflow/api/pypi/pyv-virt/packages/up-pkg/1.0.0/up_pkg-1.0.0.tar.gz")
	if status != http.StatusOK {
		t.Fatalf("repeat virtual GET = %d", status)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "HIT" {
		t.Errorf("repeat X-BinFlow-Cache = %q, want HIT", got)
	}
	if body != "upstream-sdist" {
		t.Errorf("repeat body = %q", body)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("upstream hits = %d, want 1", got)
	}

	// DELETE on the same address: the PyPI protocol has no delete route, so
	// the face's own method gate answers the 405 — the artifact is not
	// deletable through the virtual by any spelling.
	resp := s.do(http.MethodDelete,
		"/binflow/api/pypi/pyv-virt/packages/loc-pkg/1.0.0/loc_pkg-1.0.0.tar.gz",
		adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("virtual DELETE = %d, want 405", resp.StatusCode)
	}
	if got := resp.Header.Get("Allow"); got != "GET, HEAD" {
		t.Errorf("virtual DELETE Allow = %q, want GET, HEAD", got)
	}
	_ = resp.Body.Close()

	// The local member's node survived the refused delete.
	if status, _, _ = s.get("/binflow/api/pypi/pyv-loc/packages/loc-pkg/1.0.0/loc_pkg-1.0.0.tar.gz"); status != http.StatusOK {
		t.Fatalf("member GET after refused delete = %d", status)
	}

	// StatusError rendering: a virtual whose only member is an SSRF-refusing
	// remote (loopback upstream, no exemption) propagates the engine's own
	// 400 verdict — before T-82 the pypi table answered a flat 500.
	if _, err := s.svc.CreateRepo(ctx, &repo.Principal{Name: "admin", Admin: true}, &metadata.Repo{
		RepoKey: "pyv-ssrf", Type: repo.TypeRemote, PackageType: repo.PackagePypi,
		Config: `{"url":"` + upstream.URL + `"}`,
	}); err != nil {
		t.Fatalf("CreateRepo(pyv-ssrf): %v", err)
	}
	if _, err := s.svc.CreateRepo(ctx, &repo.Principal{Name: "admin", Admin: true}, &metadata.Repo{
		RepoKey: "pyv-fault", Type: repo.TypeVirtual, PackageType: repo.PackagePypi,
		Config: `{"repositories":["pyv-ssrf"]}`,
	}); err != nil {
		t.Fatalf("CreateRepo(pyv-fault): %v", err)
	}
	faultResp := s.do(http.MethodGet,
		"/binflow/api/pypi/pyv-fault/packages/x/1.0.0/x-1.0.0.tar.gz", "", "", nil, nil)
	defer func() { _ = faultResp.Body.Close() }()
	if faultResp.StatusCode != http.StatusBadRequest {
		faultBody, _ := io.ReadAll(faultResp.Body)
		t.Fatalf("virtual member-fault GET = %d, want 400; body %s", faultResp.StatusCode, faultBody)
	}
	faultBody, _ := io.ReadAll(faultResp.Body)
	if !strings.Contains(string(faultBody), "suppressed upstream") {
		t.Errorf("member-fault body = %s, want the engine's SSRF wording", faultBody)
	}
}
