package maven

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// T-82: the two T-66/T-71 seams on the maven transfer plane, over one
// end-to-end virtual fixture (local member first, remote member second,
// counting mock upstream):
//
//   - a *repo.StatusError renders verbatim — the RE-08 DELETE refusal's own
//     405 + Allow: GET (the T-71 bug: the adapter swallowed it into the
//     ErrRepoTypeNotSupported arm's 400),
//   - a hinted body stream contributes X-BinFlow-Resolved-From (and the
//     remote member's X-BinFlow-Cache beneath it) to artifact and sidecar
//     responses alike.
func TestVirtualRenderSeams(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	hits := &atomic.Int64{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/com/acme/up/2.0.0/up-2.0.0.jar" {
			_, _ = w.Write([]byte("upstream-jar-bytes"))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(upstream.Close)

	for _, row := range []*metadata.Repo{
		{RepoKey: "mv-loc", Type: repo.TypeLocal, PackageType: Protocol, Config: "{}"},
		{RepoKey: "mv-rem", Type: repo.TypeRemote, PackageType: Protocol,
			Config: `{"url":"` + upstream.URL + `","allowPrivateUpstream":true}`},
		{RepoKey: "mv-virt", Type: repo.TypeVirtual, PackageType: Protocol,
			Config: `{"repositories":["mv-loc","mv-rem"]}`},
	} {
		if _, err := hs.svc.CreateRepo(ctx, &repo.Principal{Name: "admin", Admin: true}, row); err != nil {
			t.Fatalf("CreateRepo(%s): %v", row.RepoKey, err)
		}
	}

	localJar := "com/acme/lib/1.0.0/lib-1.0.0.jar"
	if resp := hs.serve(http.MethodPut, "/mv-loc/"+localJar, jarBytes, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed local member = %d (%s)", resp.StatusCode, drain(t, resp))
	}

	// Local-member hit through the virtual: Resolved-From names the member;
	// a local hit carries no cache header (nothing was fetched).
	resp := hs.serve(http.MethodGet, "/mv-virt/"+localJar, nil, nil, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("virtual GET local member = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	if got := resp.Header.Get(repo.HdrResolvedFrom); got != "mv-loc" {
		t.Errorf("local hit Resolved-From = %q, want mv-loc", got)
	}
	if got := resp.Header.Get("X-BinFlow-Cache"); got != "" {
		t.Errorf("local hit carries X-BinFlow-Cache %q, want none", got)
	}
	if body := drain(t, resp); string(body) != string(jarBytes) {
		t.Errorf("local hit body = %q", body)
	}

	// Remote-member pull-through: both hints on one response.
	resp = hs.serve(http.MethodGet, "/mv-virt/com/acme/up/2.0.0/up-2.0.0.jar", nil, nil, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("virtual GET remote member = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	if got := resp.Header.Get(repo.HdrResolvedFrom); got != "mv-rem" {
		t.Errorf("remote hit Resolved-From = %q, want mv-rem", got)
	}
	if got := resp.Header.Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("remote hit X-BinFlow-Cache = %q, want MISS", got)
	}
	if body := drain(t, resp); string(body) != "upstream-jar-bytes" {
		t.Errorf("remote hit body = %q", body)
	}

	// Repeat: the member's cached copy serves (HIT), the upstream stays
	// frozen — the header merge is not a one-shot artifact of the MISS path.
	resp = hs.serve(http.MethodGet, "/mv-virt/com/acme/up/2.0.0/up-2.0.0.jar", nil, nil, true)
	if got := resp.Header.Get("X-BinFlow-Cache"); got != "HIT" {
		t.Errorf("repeat X-BinFlow-Cache = %q, want HIT", got)
	}
	if got := resp.Header.Get(repo.HdrResolvedFrom); got != "mv-rem" {
		t.Errorf("repeat Resolved-From = %q, want mv-rem", got)
	}
	drain(t, resp)
	if got := hits.Load(); got != 1 {
		t.Errorf("upstream hits = %d, want 1", got)
	}

	// The checksum sidecar face carries the same resolution hint as its
	// target (the computed body never streams, but the member question is
	// the same one operators ask).
	resp = hs.serve(http.MethodGet, "/mv-virt/"+localJar+".sha1", nil, nil, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("virtual sidecar GET = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	if got := resp.Header.Get(repo.HdrResolvedFrom); got != "mv-loc" {
		t.Errorf("sidecar Resolved-From = %q, want mv-loc", got)
	}
	drain(t, resp)

	// DELETE through the virtual repository: the RE-08 refusal's own 405 +
	// Allow: GET with the C5 wording — the exact case T-71 measured as a
	// 400 on this face (the StatusError fell into the adapter's
	// non-PUT/POST arm).
	resp = hs.serve(http.MethodDelete, "/mv-virt/"+localJar, nil, nil, true)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("virtual DELETE = %d, want 405 (%s)", resp.StatusCode, drain(t, resp))
	}
	if got := resp.Header.Get("Allow"); got != "GET" {
		t.Errorf("virtual DELETE Allow = %q, want GET", got)
	}
	if msg := string(drain(t, resp)); !strings.Contains(msg,
		"No local repository was configured as local deployment repository for the (mv-virt) virtual repository.") {
		t.Errorf("virtual DELETE body = %s", msg)
	}

	// The member's node survived: deletes never propagate through the
	// virtual resolution.
	if resp := hs.serve(http.MethodGet, "/mv-loc/"+localJar, nil, nil, true); resp.StatusCode != http.StatusOK {
		t.Fatalf("member GET after refused delete = %d", resp.StatusCode)
	}
}
