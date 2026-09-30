package maven

// T-597 / BIN-79 (ledger generic/remote-deploy-refusal-form, arm d): the
// remote-repo checksum sidecar GET/HEAD retraction. The a-priori 404
// "Checksums are not downloadable." is gone — the suffix-stripped SOURCE
// rides the ordinary pull-through chain, and the landed copy answers its
// computed digest. Live A anchors (7.161.26, /tmp/t597 probe legs
// m-side-sha1-first / m-side-sha1-inwin): the upstream-fault 404 cites the
// source path and the source's upstream URL in the retrieval-error form;
// the requests that follow inside the assumed-offline window answer the
// offline form. The reachable-upstream 200 form is live-unprobed (NOT_RUN)
// and follows the same source resolution.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestRemoteSidecarBacksourceChain pins the read face end to end against a
// real upstream: the sidecar resolves the SOURCE, the first contact lands
// the copy, every later sidecar (any algorithm, GET and HEAD) reads the
// ledger — zero further upstream packets.
func TestRemoteSidecarBacksourceChain(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/up/junit/junit/4.13.2/junit-4.13.2.jar" {
			_, _ = w.Write(jarBytes)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(upstream.Close)

	// The harness's seeded row points at the screened loopback; rewire it
	// to the live fixture with the NFR-S13 exemption (the admin-set flag
	// is the legitimate way to aim the chain at a private test upstream).
	if err := hs.md.Remote().UpdateConfig(ctx, &metadata.RemoteConfig{
		RepoKey: "maven-remote", URL: upstream.URL + "/up", AllowPrivateUpstream: true,
	}); err != nil {
		t.Fatalf("rewire upstream: %v", err)
	}

	s1, md5Hex, s256 := digests(jarBytes)
	for _, tt := range []struct {
		algo string
		want string
	}{
		{"sha1", s1},
		{"md5", md5Hex},
		{"sha256", s256},
	} {
		resp := hs.serve(http.MethodGet, "/maven-remote/junit/junit/4.13.2/junit-4.13.2.jar."+tt.algo, nil, nil, true)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("sidecar GET .%s = %d, want 200 (computed digest of the landed source)", tt.algo, resp.StatusCode)
		}
		if got := string(drain(t, resp)); got != tt.want {
			t.Fatalf("sidecar .%s body = %s, want %s", tt.algo, got, tt.want)
		}
	}
	// The SOURCE landed once; the md5/sha256 legs and the two legs below
	// are pure ledger reads.
	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1 (every sidecar after the source lands is a HIT)", got)
	}

	// HEAD renders the same face: the status and headers answer, the body
	// stays suppressed (writeSidecarBody's HEAD arm).
	head := hs.serve(http.MethodHead, "/maven-remote/junit/junit/4.13.2/junit-4.13.2.jar.sha1", nil, nil, true)
	if head.StatusCode != http.StatusOK {
		t.Fatalf("sidecar HEAD = %d, want 200", head.StatusCode)
	}
	if head.ContentLength != int64(len(s1)) {
		t.Fatalf("sidecar HEAD Content-Length = %d, want %d", head.ContentLength, len(s1))
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream hits after HEAD = %d, want 1", got)
	}
}

// TestRemoteSidecarUpstreamFaultExternalization pins the fault arms against
// an unreachable upstream (the private-upstream exemption on, so the chain
// itself — not the NFR-S13 screening — answers): the FIRST contact carries
// the retrieval error citing the source path and the source's upstream
// URL; the requests inside the assumed-offline window that follows answer
// the offline form. Neither form may cite the terminal .sha1 spelling.
func TestRemoteSidecarUpstreamFaultExternalization(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()
	admin := &auth.Principal{Name: "admin", Admin: true}

	// A fresh remote repository whose upstream is a dead port: the chain
	// is exercised from a clean slate (no offline window, no negative
	// cache, no local copy).
	if _, err := hs.svc.CreateRepo(ctx, admin, &metadata.Repo{
		RepoKey: "maven-remote-dead", Type: repo.TypeRemote, PackageType: Protocol,
		Config: `{"url":"http://127.0.0.1:1/dead","allowPrivateUpstream":true}`,
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	const src = "com/acme/x/1.0.0/x-1.0.0.jar"
	const upURL = "http://127.0.0.1:1/dead/" + src

	// First contact: the retrieval-error form (live-A family, probe
	// m-side-sha1-first).
	resp := hs.serve(http.MethodGet, "/maven-remote-dead/"+src+".sha1", nil, nil, true)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("first sidecar GET = %d, want 404", resp.StatusCode)
	}
	first := string(drain(t, resp))
	for _, want := range []string{
		"Error in getting information for '" + src + "'",
		"Failed retrieving resource from " + upURL,
		"Path: 'maven-remote-dead:" + src + "'",
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("first-fault message misses %q: %s", want, first)
		}
	}
	if strings.Contains(first, ".sha1") {
		t.Fatalf("first-fault message cites the sidecar spelling: %s", first)
	}

	// Second contact: still the retrieval form — T-619/BIN-101 opens the
	// offline window on the second consecutive transport fault.
	resp = hs.serve(http.MethodGet, "/maven-remote-dead/"+src+".sha1", nil, nil, true)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("second-fault sidecar GET = %d, want 404", resp.StatusCode)
	}
	if second := string(drain(t, resp)); !strings.Contains(second, "Failed retrieving resource from") {
		t.Fatalf("second-fault message is not the retrieval form: %s", second)
	}

	// Inside the assumed-offline window: the offline form (probe
	// m-side-sha1-inwin).
	resp = hs.serve(http.MethodGet, "/maven-remote-dead/"+src+".sha1", nil, nil, true)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("in-window sidecar GET = %d, want 404", resp.StatusCode)
	}
	inwin := string(drain(t, resp))
	for _, want := range []string{
		"is assumed offline, 'maven-remote-dead:" + src + "' is not found at '" + src + "'",
		"Path: 'maven-remote-dead:" + src + "'",
	} {
		if !strings.Contains(inwin, want) {
			t.Fatalf("in-window message misses %q: %s", want, inwin)
		}
	}
	if strings.Contains(inwin, "Failed retrieving resource from") {
		t.Fatalf("in-window message re-externalizes the retrieval error: %s", inwin)
	}

	// HEAD answers the same status on the same face.
	head := hs.serve(http.MethodHead, "/maven-remote-dead/"+src+".md5", nil, nil, true)
	if head.StatusCode != http.StatusNotFound {
		t.Fatalf("in-window sidecar HEAD = %d, want 404", head.StatusCode)
	}
	_ = drain(t, head)
}
