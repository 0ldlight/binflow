package docker

// M14 T-392 (FR-129): the DOCKER remote pull-through over the real /v2
// plane. The remote data chain itself is family-shared (T-363 built it for
// helmoci; T-365 widened the read use cases) — this file pins the DELTA:
// a package_type=docker, class=remote repository row, created through the
// real service (the community posture — the core-five slot needs no
// license), reaches the same pull-through: the self-referential BinFlow
// upstream round trip (MISS, byte identity, the frozen-upstream HIT), the
// degradation matrix, the RE-05 write refusal and the Bearer dance. The
// real docker CLIENT legs run in the ticket's live validation (dind).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// remotePullStack is one catalogStack plus a header/body-capable request
// helper (the remote legs carry Accept headers the plain serve() cannot).
type remotePullStack struct {
	*catalogStack
}

// serveReq runs one request against the stack's handler with the admin
// principal in the context and optional headers/body.
func (rs *remotePullStack) serveReq(method, path string, body io.Reader, hdr map[string]string) (int, string, http.Header) {
	rs.t.Helper()
	req := httptest.NewRequest(method, path, body)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	req = req.WithContext(adapter.WithPrincipal(req.Context(), rs.admin))
	rec := httptest.NewRecorder()
	rs.h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), rec.Header()
}

// get runs an anonymous GET (the pull-through read face is anonymous-open
// on this stack, the docker login-less pull shape).
func (rs *remotePullStack) get(path string, hdr map[string]string) (int, string, http.Header) {
	rs.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	rs.h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), rec.Header()
}

// acceptManifests is the Accept set a docker daemon sends for manifest
// negotiation (schema2 + OCI single image, enough for these legs).
var acceptManifests = map[string]string{
	"Accept": strings.Join([]string{
		mediaTypeDockerManifest,
		mediaTypeOCIManifest,
		"application/vnd.docker.distribution.manifest.v1+prettyjws",
	}, ", "),
}

// seedDockerRemoteRepo creates the remote docker repository through the
// REAL service (the FR-129 admission — community posture, the harness
// carries no license and no gate) pointing at base.
func (rs *remotePullStack) seedDockerRemoteRepo(t *testing.T, key, base string) {
	t.Helper()
	_, err := rs.svc.CreateRepo(context.Background(), rs.admin, &metadata.Repo{
		RepoKey: key, Type: repo.TypeRemote, PackageType: repo.PackageDocker,
		Config: fmt.Sprintf(`{"url":%q,"allowPrivateUpstream":true}`, base),
	})
	if err != nil {
		t.Fatalf("create remote docker repository %s: %v", key, err)
	}
}

// newDockerRemoteFixture assembles the self-referential pair: an upstream
// stack with a docker local repository holding one pushed image, served
// behind a counting server, and a downstream stack whose docker-remote
// repository points at it.
func newDockerRemoteFixture(t *testing.T) (down *remotePullStack, upSrv *httptest.Server, hits *atomic.Int64, manifest, cfg, layer []byte) {
	t.Helper()
	up := &remotePullStack{newCatalogStack(t, true)}
	up.seedDockerRepo("up-local")

	layer = []byte("t392-layer-bytes-for-the-pull-through-roundtrip")
	cfg = []byte(`{"architecture":"amd64","os":"linux","created":"2026-08-31T00:00:00Z"}`)
	layerDgst, cfgDgst := "sha256:"+sha256Hex(layer), "sha256:"+sha256Hex(cfg)
	manifest = []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"`+mediaTypeDockerManifest+`",`+
			`"config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"%s","size":%d},`+
			`"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","digest":"%s","size":%d}]}`,
		cfgDgst, len(cfg), layerDgst, len(layer)))
	if code, body, _ := up.serveReq(http.MethodPost, "/v2/up-local/myapp/blobs/uploads/?digest="+layerDgst,
		strings.NewReader(string(layer)), nil); code != http.StatusCreated {
		t.Fatalf("layer push = (%d, %s), want 201", code, body)
	}
	if code, body, _ := up.serveReq(http.MethodPost, "/v2/up-local/myapp/blobs/uploads/?digest="+cfgDgst,
		strings.NewReader(string(cfg)), nil); code != http.StatusCreated {
		t.Fatalf("config push = (%d, %s), want 201", code, body)
	}
	if code, body, _ := up.serveReq(http.MethodPut, "/v2/up-local/myapp/manifests/1.0",
		strings.NewReader(string(manifest)), map[string]string{"Content-Type": mediaTypeDockerManifest}); code != http.StatusCreated {
		t.Fatalf("upstream manifest PUT = (%d, %s), want 201", code, body)
	}

	hits = &atomic.Int64{}
	upSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		r = r.WithContext(adapter.WithPrincipal(r.Context(), up.admin))
		up.h.ServeHTTP(w, r)
	}))
	t.Cleanup(upSrv.Close)

	down = &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "docker-remote", upSrv.URL+"/v2/up-local")
	return down, upSrv, hits, manifest, cfg, layer
}

// TestT392RemotePullThroughChain: the full proxy round trip — manifest by
// tag (MISS, byte-identical, the digest header), the config/layer blobs,
// the SECOND round trip entirely from the cache (HIT markers, the upstream
// counter frozen — AC1's assertion), the cached tags/list and catalog rows,
// by-digest resolution, and the RE-05 write refusal.
func TestT392RemotePullThroughChain(t *testing.T) {
	down, _, hits, manifest, cfg, layer := newDockerRemoteFixture(t)

	code, body, hdr := down.get("/v2/docker-remote/myapp/manifests/1.0", acceptManifests)
	if code != http.StatusOK {
		t.Fatalf("remote manifest GET = (%d, %s), want 200", code, body)
	}
	if body != string(manifest) {
		t.Fatalf("remote manifest body drifts: %d bytes, want the upstream %d", len(body), len(manifest))
	}
	if got := hdr.Get("Docker-Content-Digest"); got != "sha256:"+sha256Hex(manifest) {
		t.Errorf("Docker-Content-Digest = %q, want the measured digest", got)
	}
	if got := hdr.Get("Content-Type"); got != mediaTypeDockerManifest {
		t.Errorf("Content-Type = %q, want the upstream media type", got)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("first manifest X-BinFlow-Cache = %q, want MISS", got)
	}
	if hits.Load() == 0 {
		t.Fatal("the first pull never reached the upstream")
	}

	// The blobs: proxied byte-identical (streamed landing, AC1's digest
	// identity extends to every layer byte).
	for _, blob := range [][]byte{cfg, layer} {
		code, body, hdr = down.get("/v2/docker-remote/myapp/blobs/sha256:"+sha256Hex(blob), nil)
		if code != http.StatusOK || body != string(blob) {
			t.Fatalf("remote blob GET = (%d, %d bytes), want (200, %d bytes)", code, len(body), len(blob))
		}
		if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
			t.Errorf("first blob X-BinFlow-Cache = %q, want MISS", got)
		}
	}

	// The second round trip: everything serves from the cache — the
	// upstream counter must not move (AC1's 上游访问计数不增).
	afterFirst := hits.Load()
	code, body, hdr = down.get("/v2/docker-remote/myapp/manifests/1.0", acceptManifests)
	if code != http.StatusOK || body != string(manifest) {
		t.Fatalf("second manifest GET = (%d, %d bytes), want the cached copy", code, len(body))
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "HIT" {
		t.Errorf("second manifest X-BinFlow-Cache = %q, want HIT", got)
	}
	for _, blob := range [][]byte{cfg, layer} {
		code, body, _ = down.get("/v2/docker-remote/myapp/blobs/sha256:"+sha256Hex(blob), nil)
		if code != http.StatusOK || body != string(blob) {
			t.Fatalf("second blob GET = (%d, %d bytes), want the cached copy", code, len(body))
		}
	}
	if hits.Load() != afterFirst {
		t.Fatalf("cached round trip contacted the upstream: %d -> %d hits", afterFirst, hits.Load())
	}

	// The cached tag rows back the tags/list face; the catalog names the
	// cached image; by-digest resolves over the cached state (HIT).
	code, body, _ = down.get("/v2/docker-remote/myapp/tags/list", nil)
	if code != http.StatusOK || !strings.Contains(body, `"1.0"`) {
		t.Errorf("remote tags/list = (%d, %s), want 200 naming 1.0", code, body)
	}
	code, body, _ = down.serveReq(http.MethodGet, "/v2/_catalog", nil, nil)
	if code != http.StatusOK || !strings.Contains(body, "docker-remote/myapp") {
		t.Errorf("catalog = (%d, %s), want the remote's cached image", code, body)
	}
	code, body, hdr = down.get("/v2/docker-remote/myapp/manifests/sha256:"+sha256Hex(manifest), acceptManifests)
	if code != http.StatusOK || body != string(manifest) {
		t.Fatalf("manifest by digest = (%d, %d bytes), want the cached copy", code, len(body))
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "HIT" {
		t.Errorf("by-digest X-BinFlow-Cache = %q, want HIT over the cached state", got)
	}
}

// TestT392RemoteWriteRefusal: every write verb against the docker remote
// row answers RE-05's 405 + Allow: GET — the read-only proxy contract,
// answered before any upload session is minted.
func TestT392RemoteWriteRefusal(t *testing.T) {
	down, _, _, manifest, _, _ := newDockerRemoteFixture(t)

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodPut, "/v2/docker-remote/myapp/manifests/2.0"},
		{http.MethodPost, "/v2/docker-remote/myapp/blobs/uploads/"},
		{http.MethodDelete, "/v2/docker-remote/myapp/manifests/sha256:" + sha256Hex(manifest)},
	} {
		code, body, hdr := down.serveReq(tc.method, tc.path, strings.NewReader(`{}`),
			map[string]string{"Content-Type": mediaTypeDockerManifest})
		if code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s = (%d, %s), want 405", tc.method, tc.path, code, body)
		}
		if got := hdr.Get("Allow"); got != http.MethodGet {
			t.Errorf("%s %s Allow = %q, want GET", tc.method, tc.path, got)
		}
		if !strings.Contains(body, "read-only proxy cache") {
			t.Errorf("%s %s body %q lacks the RE-05 wording", tc.method, tc.path, body)
		}
	}
}

// TestT392RemoteDegradationMatrix: the upstream gone (connection refused)
// — an EXPIRED cached copy serves STALE with the marker; an uncached
// reference answers the unfound family, never a naked 5xx (AC2's 降级).
func TestT392RemoteDegradationMatrix(t *testing.T) {
	down, upSrv, deadSrv, _, manifest, cfg, layer := newDockerRemoteFixtureWithDead(t)

	code, body, _ := down.get("/v2/docker-remote/myapp/manifests/1.0", acceptManifests)
	if code != http.StatusOK {
		t.Fatalf("first pull = (%d, %s), want 200", code, body)
	}
	// Expire the cached manifest + blob windows (the row-level clock).
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	for _, path := range []string{
		"myapp/manifests/" + sha256Hex(manifest),
		"myapp/blobs/" + sha256Hex(manifest),
		"myapp/blobs/" + sha256Hex(cfg),
		"myapp/blobs/" + sha256Hex(layer),
	} {
		if err := down.md.Remote().PutCache(context.Background(), &metadata.RemoteCacheEntry{
			RepoKey: "docker-remote", Path: path, Kind: metadata.RemoteCacheKindContent,
			FetchedAt: past, ExpiresAt: past,
		}); err != nil {
			t.Fatalf("expire %s: %v", path, err)
		}
	}
	upSrv.Close()

	// STALE serve of the expired copy, with the marker (已缓存可拉).
	code, body, hdr := down.get("/v2/docker-remote/myapp/manifests/1.0", acceptManifests)
	if code != http.StatusOK || body != string(manifest) {
		t.Fatalf("degraded pull = (%d, %d bytes), want the stale copy", code, len(body))
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "STALE" {
		t.Errorf("degraded X-BinFlow-Cache = %q, want STALE", got)
	}
	if got := hdr.Get("X-Binflow-Upstream-Error"); got == "" {
		t.Error("degraded serve carries no X-Binflow-Upstream-Error marker")
	}

	// Uncached against the dead upstream: the unfound family, zero 5xx
	// (未缓存零 5xx).
	down.seedDockerRemoteRepo(t, "docker-remote-dead", deadSrv.URL+"/v2/nothing")
	code, body, _ = down.get("/v2/docker-remote-dead/otherimg/manifests/9.9.9", acceptManifests)
	if code != http.StatusNotFound || !strings.Contains(body, "MANIFEST_UNKNOWN") {
		t.Errorf("uncached degraded manifest = (%d, %s), want the 404 unfound shape", code, body)
	}
	code, body, _ = down.get("/v2/docker-remote-dead/otherimg/blobs/sha256:"+sha256Hex([]byte("x")), nil)
	if code != http.StatusNotFound || !strings.Contains(body, "BLOB_UNKNOWN") {
		t.Errorf("uncached degraded blob = (%d, %s), want the 404 BLOB_UNKNOWN shape", code, body)
	}
}

// newDockerRemoteFixtureWithDead adds a second, already-closed server the
// degradation legs point a fresh remote at (connection-refused upstream).
func newDockerRemoteFixtureWithDead(t *testing.T) (down *remotePullStack, upSrv, deadSrv *httptest.Server, hits *atomic.Int64, manifest, cfg, layer []byte) {
	t.Helper()
	down, upSrv, hits, manifest, cfg, layer = newDockerRemoteFixture(t)
	deadSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	deadSrv.Close() // refused from here on
	return down, upSrv, deadSrv, hits, manifest, cfg, layer
}

// TestT392RemoteBearerChain: the upstream authentication chain over a
// docker remote row — the 401 with the WWW-Authenticate Bearer challenge,
// the token exchange at the realm, the authorized refetch (exactly one
// round of each), the landing and the cache HIT afterwards (AC2).
func TestT392RemoteBearerChain(t *testing.T) {
	manifest := []byte(`{"schemaVersion":2,"mediaType":"` + mediaTypeDockerManifest +
		`","config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"sha256:` +
		strings.Repeat("0a", 32) + `","size":7},"layers":[]}`)

	var challenged, exchanged, served atomic.Int64
	var bearerSrv *httptest.Server
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			exchanged.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"t392-upstream-token","expires_in":300}`))
		case r.Header.Get("Authorization") != "Bearer t392-upstream-token":
			challenged.Add(1)
			w.Header().Set("WWW-Authenticate",
				fmt.Sprintf(`Bearer realm=%q,service="t392-upstream"`, bearerSrv.URL+"/token"))
			writeSpecError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "authentication required", nil)
		default:
			served.Add(1)
			w.Header().Set("Content-Type", mediaTypeDockerManifest)
			w.Header().Set("Docker-Content-Digest", "sha256:"+sha256Hex(manifest))
			_, _ = w.Write(manifest)
		}
	})
	bearerSrv = httptest.NewServer(mux)
	t.Cleanup(bearerSrv.Close)

	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "docker-remote", bearerSrv.URL+"/v2/upstream")

	code, body, hdr := down.get("/v2/docker-remote/myapp/manifests/1.0", acceptManifests)
	if code != http.StatusOK {
		t.Fatalf("bearer pull = (%d, %s), want 200 through the dance", code, body)
	}
	if body != string(manifest) {
		t.Fatalf("bearer body drifts: %d bytes, want %d", len(body), len(manifest))
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("bearer pull X-BinFlow-Cache = %q, want MISS", got)
	}
	if challenged.Load() != 1 || exchanged.Load() != 1 || served.Load() != 1 {
		t.Fatalf("the dance counts = (401 %d, exchange %d, serve %d), want one of each",
			challenged.Load(), exchanged.Load(), served.Load())
	}

	// The second pull serves from the cache — no further dance rounds.
	code, body, hdr = down.get("/v2/docker-remote/myapp/manifests/1.0", acceptManifests)
	if code != http.StatusOK || body != string(manifest) {
		t.Fatalf("post-dance pull = (%d, %d bytes), want the cached copy", code, len(body))
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "HIT" {
		t.Errorf("post-dance X-BinFlow-Cache = %q, want HIT", got)
	}
	if challenged.Load() != 1 || exchanged.Load() != 1 || served.Load() != 1 {
		t.Fatalf("the cached pull re-ran the dance: (401 %d, exchange %d, serve %d)",
			challenged.Load(), exchanged.Load(), served.Load())
	}
}

// TestT392RemotePlaneRefusesNonRemoteClasses: the routing gate around the
// remote branch — a LOCAL docker row keeps the PUSH plane (the 202 upload
// start, never the remote 405), and a non-v2-family row stays NAME_UNKNOWN.
func TestT392RemotePlaneRefusesNonRemoteClasses(t *testing.T) {
	down, _, _, _, _, _ := newDockerRemoteFixture(t)

	down.seedDockerRepo("plain-local")
	code, body, _ := down.serveReq(http.MethodPost, "/v2/plain-local/other/blobs/uploads/", nil, nil)
	if code != http.StatusAccepted {
		t.Fatalf("local upload start = (%d, %s), want 202 (the local plane)", code, body)
	}

	if _, err := down.svc.CreateRepo(context.Background(), down.admin, &metadata.Repo{
		RepoKey: "gen-local", Type: repo.TypeLocal, PackageType: "generic",
	}); err != nil {
		t.Fatalf("seed generic repo: %v", err)
	}
	code, body, _ = down.get("/v2/gen-local/any/manifests/1.0", acceptManifests)
	if code != http.StatusNotFound || !strings.Contains(body, "NAME_UNKNOWN") {
		t.Errorf("generic row on the /v2 plane = (%d, %s), want the NAME_UNKNOWN 404", code, body)
	}
}

// TestT392RemoteUpstreamFactsDecrypted: the RemoteUpstream seam over a
// docker row answers the adapter's session facts (the URL/token/TTL
// resolution the pull-through rides) — the family check admits docker.
func TestT392RemoteUpstreamFactsDecrypted(t *testing.T) {
	down, upSrv, _, _, _, _ := newDockerRemoteFixture(t)
	plane, ok := down.svc.(repo.RemoteV2Plane)
	if !ok {
		t.Fatal("the concrete service does not implement RemoteV2Plane")
	}
	facts, err := plane.RemoteUpstream(context.Background(), down.admin, "docker-remote")
	if err != nil {
		t.Fatalf("RemoteUpstream on docker remote: %v", err)
	}
	if facts.URL != upSrv.URL+"/v2/up-local" {
		t.Fatalf("facts URL = %q, want the upstream root", facts.URL)
	}
	if !facts.AllowPrivateUpstream || facts.ContentTTLSeconds <= 0 || facts.MissedTTLSeconds <= 0 {
		t.Fatalf("facts = %+v, want the private exemption and positive TTLs", facts)
	}
	if _, err := plane.RemoteUpstream(context.Background(), down.admin, "up-local-missing"); err == nil ||
		!errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("RemoteUpstream on a missing key = %v, want ErrRepoNotFound", err)
	}
}
