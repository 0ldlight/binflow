package helmoci

// M13 T-363 (FR-116.1): the HelmOCI remote pull-through over the real /v2
// plane — a self-referential BinFlow upstream (a helmoci LOCAL repository
// with a pushed chart) proxied through a helmoci REMOTE repository, plus
// the degradation matrix (upstream deleted/gone), the write refusal and
// the class-gate legs. The helm/docker CLIENT legs run in the ticket's
// live validation; this file pins the server-side plane.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// countingWrap serves one handler behind a request counter — the
// self-referential upstream with the "上游访问计数不增" assertion surface.
func countingWrap(h http.Handler) (*httptest.Server, *atomic.Int64) {
	hits := &atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h.ServeHTTP(w, r)
	}))
	return srv, hits
}

// remoteStack is one downstream stack with a seeded helmoci remote
// repository pointing at base (the upstream's distribution API root).
func (s *stack) seedRemoteRepo(t *testing.T, key, url string) {
	t.Helper()
	s.seedRepo(t, key, repo.TypeRemote, Protocol)
	if err := s.md.Remote().CreateConfig(context.Background(), &metadata.RemoteConfig{
		RepoKey: key, URL: url, ContentTTLSeconds: 7200, MetadataTTLSeconds: 600,
		AllowPrivateUpstream: true,
	}); err != nil {
		t.Fatalf("seed remote config %s: %v", key, err)
	}
}

// upstreamChart pushes one chart into the UPSTREAM stack's local helmoci
// repository and returns (manifest, config, chart).
func upstreamChart(t *testing.T, up *stack, version string) (manifest, cfg, chart []byte) {
	t.Helper()
	cfg = []byte(fmt.Sprintf(`{"name":"mychart","version":%q,"apiVersion":"v2"}`, version))
	chart = []byte("the upstream chart tgz body " + version)
	up.pushBlob(t, "mychart", cfg, "")
	up.pushBlob(t, "mychart", chart, "")
	manifest = helmManifestBody(cfg, chart, nil)
	status, body, _ := up.put("/v2/helmoci-local/mychart/manifests/"+version, manifest,
		map[string]string{"Content-Type": mtOCIManifest})
	if status != http.StatusCreated {
		t.Fatalf("upstream manifest PUT status = %d (body %s)", status, body)
	}
	return manifest, cfg, chart
}

// newRemoteFixture assembles the self-referential pair: an upstream stack
// with the chart pushed and a downstream stack whose helmoci-remote points
// at the (counted) upstream API root. The wrapped server is returned so a
// degradation leg can take the upstream away mid-test.
func newRemoteFixture(t *testing.T) (down, up *stack, wrapped *httptest.Server, hits *atomic.Int64, manifest, cfg, chart []byte) {
	t.Helper()
	up = newStack(t)
	up.seedRepo(t, "helmoci-local", repo.TypeLocal, Protocol)
	manifest, cfg, chart = upstreamChart(t, up, "0.1.0")
	wrapped, hits = countingWrap(up.srv.Config.Handler)
	t.Cleanup(wrapped.Close)

	down = newStack(t)
	// The upstream URL is the REGISTRY ROOT; the wire path carries /v2/
	// itself (L000-F). The upstream's repoKey rides the image namespace
	// — helmoci-local/mychart, the library/hello-world shape.
	down.seedRemoteRepo(t, "helmoci-remote", wrapped.URL)
	return down, up, wrapped, hits, manifest, cfg, chart
}

// TestRemotePullThroughChain: the full proxy round trip — manifest by tag
// (MISS, byte-identical, digest header), the chart/config blobs, then the
// SECOND round trip serving entirely from the cache (HIT markers, the
// upstream counter frozen), the cached tags/list and catalog rows, and the
// by-digest resolution over the cached state.
func TestRemotePullThroughChain(t *testing.T) {
	down, _, _, hits, manifest, cfg, chart := newRemoteFixture(t)

	accept := map[string]string{"Accept": mtOCIManifest}
	status, body, hdr := down.do(http.MethodGet, "/v2/helmoci-remote/helmoci-local/mychart/manifests/0.1.0", "", "", nil, accept)
	if status != http.StatusOK {
		t.Fatalf("remote manifest GET status = %d, want 200 (body %s)", status, body)
	}
	if body != string(manifest) {
		t.Fatalf("remote manifest body drifts: %d bytes, want the upstream %d", len(body), len(manifest))
	}
	if got := hdr.Get("Docker-Content-Digest"); got != "sha256:"+sha256HexOf(manifest) {
		t.Errorf("Docker-Content-Digest = %q, want the measured digest", got)
	}
	if got := hdr.Get("Content-Type"); got != mtOCIManifest {
		t.Errorf("Content-Type = %q, want the upstream media type", got)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("first manifest X-BinFlow-Cache = %q, want MISS", got)
	}
	firstHits := hits.Load()
	if firstHits == 0 {
		t.Fatal("the first pull never reached the upstream")
	}

	// The blobs: proxied byte-identical (streamed landing).
	for _, blob := range [][]byte{cfg, chart} {
		status, body, hdr = down.get("/v2/helmoci-remote/helmoci-local/mychart/blobs/sha256:" + sha256HexOf(blob))
		if status != http.StatusOK || body != string(blob) {
			t.Fatalf("remote blob GET = (%d, %d bytes), want (200, %d bytes)", status, len(body), len(blob))
		}
		if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
			t.Errorf("first blob X-BinFlow-Cache = %q, want MISS", got)
		}
	}

	// The second round trip: everything serves from the cache — the
	// upstream counter must not move (AC1's assertion).
	afterBlobs := hits.Load()
	status, body, hdr = down.do(http.MethodGet, "/v2/helmoci-remote/helmoci-local/mychart/manifests/0.1.0", "", "", nil, accept)
	if status != http.StatusOK || body != string(manifest) {
		t.Fatalf("second manifest GET = (%d, %d bytes)", status, len(body))
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "HIT" {
		t.Errorf("second manifest X-BinFlow-Cache = %q, want HIT", got)
	}
	status, body, _ = down.get("/v2/helmoci-remote/helmoci-local/mychart/blobs/sha256:" + sha256HexOf(chart))
	if status != http.StatusOK || body != string(chart) {
		t.Fatalf("second blob GET = (%d, %d bytes)", status, len(body))
	}
	if hits.Load() != afterBlobs {
		t.Fatalf("cached round trip contacted the upstream: %d -> %d hits", afterBlobs, hits.Load())
	}

	// The cached tag rows back the tags/list face (helm's no-version pull
	// resolves through it) and the catalog names the cached image.
	status, body, _ = down.get("/v2/helmoci-remote/helmoci-local/mychart/tags/list")
	if status != http.StatusOK || !strings.Contains(body, `"0.1.0"`) {
		t.Errorf("remote tags/list = (%d, %s), want 200 naming 0.1.0", status, body)
	}
	status, body, _ = down.do(http.MethodGet, "/v2/_catalog", adminUser, adminPass, nil, nil)
	if status != http.StatusOK || !strings.Contains(body, "helmoci-remote/helmoci-local/mychart") {
		t.Errorf("catalog = (%d, %s), want the remote's cached image", status, body)
	}

	// By-digest resolution over the cached state (helm pull oci://…@sha256:).
	status, body, hdr = down.get("/v2/helmoci-remote/helmoci-local/mychart/manifests/sha256:" + sha256HexOf(manifest))
	if status != http.StatusOK || body != string(manifest) {
		t.Fatalf("remote manifest by digest = (%d, %d bytes)", status, len(body))
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "HIT" {
		t.Errorf("by-digest X-BinFlow-Cache = %q, want HIT over the cached state", got)
	}
}

// TestRemoteUpstreamDeletedCacheServes: deleting the chart UPSTREAM does
// not disturb the cached copy — the fresh window serves it (HIT, counter
// frozen), the upstream fact base is BinFlow's own.
func TestRemoteUpstreamDeletedCacheServes(t *testing.T) {
	down, up, _, hits, manifest, _, _ := newRemoteFixture(t)

	status, body, _ := down.do(http.MethodGet, "/v2/helmoci-remote/helmoci-local/mychart/manifests/0.1.0", "", "", nil,
		map[string]string{"Accept": mtOCIManifest})
	if status != http.StatusOK || body != string(manifest) {
		t.Fatalf("first pull = (%d, %d bytes)", status, len(body))
	}
	cached := hits.Load()

	// Delete upstream (the manifest row + nodes, the full teardown).
	if err := up.svc.DeleteManifest(context.Background(), &repo.Principal{Name: adminUser, Admin: true},
		"helmoci-local", "mychart", sha256HexOf(manifest)); err != nil {
		t.Fatalf("upstream delete: %v", err)
	}

	status, body, hdr := down.do(http.MethodGet, "/v2/helmoci-remote/helmoci-local/mychart/manifests/0.1.0", "", "", nil,
		map[string]string{"Accept": mtOCIManifest})
	if status != http.StatusOK || body != string(manifest) {
		t.Fatalf("post-delete pull = (%d, %d bytes), want the cached copy", status, len(body))
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "HIT" {
		t.Errorf("post-delete X-BinFlow-Cache = %q, want HIT (本地事实兜底)", got)
	}
	if hits.Load() != cached {
		t.Fatalf("post-delete pull contacted the upstream: %d -> %d", cached, hits.Load())
	}
}

// TestRemoteDegradationMatrix: the upstream gone (connection refused) —
// an EXPIRED cached copy serves STALE with the marker; an uncached
// reference answers the unfound family, never a naked 5xx (FR-116.5).
func TestRemoteDegradationMatrix(t *testing.T) {
	down, _, wrapped, _, manifest, _, _ := newRemoteFixture(t)

	status, body, _ := down.do(http.MethodGet, "/v2/helmoci-remote/helmoci-local/mychart/manifests/0.1.0", "", "", nil,
		map[string]string{"Accept": mtOCIManifest})
	if status != http.StatusOK {
		t.Fatalf("first pull status = %d (body %s)", status, body)
	}
	// Expire the cached manifest+blob windows (the row-level clock).
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	for _, path := range []string{
		"helmoci-local/mychart/manifests/" + sha256HexOf(manifest),
		"helmoci-local/mychart/blobs/" + sha256HexOf(manifest),
	} {
		if err := down.md.Remote().PutCache(context.Background(), &metadata.RemoteCacheEntry{
			RepoKey: "helmoci-remote", Path: path, Kind: metadata.RemoteCacheKindContent,
			FetchedAt: past, ExpiresAt: past,
		}); err != nil {
			t.Fatalf("expire %s: %v", path, err)
		}
	}
	// The upstream goes away entirely: close it, and point a second
	// repository at a closed port for the uncached legs.
	wrapped.Close()
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	closed.Close() // refused from here on
	down.seedRemoteRepo(t, "helmoci-remote-dead", closed.URL)

	// STALE serve of the expired copy, with the marker.
	status, body, hdr := down.do(http.MethodGet, "/v2/helmoci-remote/helmoci-local/mychart/manifests/0.1.0", "", "", nil,
		map[string]string{"Accept": mtOCIManifest})
	if status != http.StatusOK || body != string(manifest) {
		t.Fatalf("degraded pull = (%d, %d bytes), want the stale copy", status, len(body))
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "STALE" {
		t.Errorf("degraded X-BinFlow-Cache = %q, want STALE", got)
	}
	if got := hdr.Get("X-Binflow-Upstream-Error"); got == "" {
		t.Error("degraded serve carries no X-Binflow-Upstream-Error marker")
	}

	// Uncached against the dead upstream: the unfound family, zero 5xx.
	status, body, _ = down.get("/v2/helmoci-remote-dead/other/manifests/9.9.9")
	if status != http.StatusNotFound {
		t.Fatalf("uncached degraded pull status = %d, want 404 (body %s)", status, body)
	}
	if !strings.Contains(body, "MANIFEST_UNKNOWN") {
		t.Errorf("uncached degraded body %q is not the spec unfound shape", body)
	}
	status, body, _ = down.get("/v2/helmoci-remote-dead/other/blobs/sha256:" + sha256HexOf([]byte("x")))
	if status != http.StatusNotFound || !strings.Contains(body, "BLOB_UNKNOWN") {
		t.Errorf("uncached degraded blob = (%d, %s), want the 404 BLOB_UNKNOWN shape", status, body)
	}
}

// TestRemoteWriteRefusal: the remote plane's write refusal answers
// Artifactory's upload-rejection shape (L000-B C11, evidence E5-1..3) — 400
// with the generic error model's per-endpoint copy for the three OBSERVED
// verb families; an unobserved combination (a blob DELETE) keeps RE-05's
// standing 405 + Allow: GET.
func TestRemoteWriteRefusal(t *testing.T) {
	down, _, _, _, manifest, _, _ := newRemoteFixture(t)

	cases := []struct {
		method, path, wantBody string
	}{
		{http.MethodPut, "/v2/helmoci-remote/helmoci-local/mychart/manifests/0.2.0",
			"Unable to upload a manifest to a remote repository."},
		{http.MethodPost, "/v2/helmoci-remote/helmoci-local/mychart/blobs/uploads/",
			"Unable to upload blobs to a remote repository."},
		{http.MethodDelete, "/v2/helmoci-remote/helmoci-local/mychart/manifests/sha256:" + sha256HexOf(manifest),
			"Unable to delete a manifest from a remote repository."},
	}
	for _, tc := range cases {
		status, body, _ := down.do(tc.method, tc.path, adminUser, adminPass,
			strings.NewReader(string(manifest)), map[string]string{"Content-Type": mtOCIManifest})
		if status != http.StatusBadRequest {
			t.Fatalf("%s %s status = %d, want 400 (body %s)", tc.method, tc.path, status, body)
		}
		if !strings.Contains(body, tc.wantBody) {
			t.Errorf("%s %s body %q lacks the E5 copy %q", tc.method, tc.path, body, tc.wantBody)
		}
	}

	// An unobserved combination keeps the standing 405 + Allow: GET.
	status, body, hdr := down.do(http.MethodDelete,
		"/v2/helmoci-remote/helmoci-local/mychart/blobs/sha256:"+sha256HexOf(manifest),
		adminUser, adminPass, nil, nil)
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("blob DELETE status = %d, want the standing 405 (body %s)", status, body)
	}
	if got := hdr.Get("Allow"); got != http.MethodGet {
		t.Errorf("blob DELETE Allow = %q, want GET", got)
	}
}

// TestRemoteClassGates: the live create plane — helmoci REMOTE creation
// passes under the pro document (with the canonicalized url), docker REMOTE
// rides the same seam since T-392 (FR-129), and the unlicensed community
// tier keeps helmoci's D3 refusal.
func TestRemoteClassGates(t *testing.T) {
	s, keys := newLicensedStack(t)
	ctx := context.Background()

	// Community: the D3 refusal names helmoci and the tier.
	body := `{"key":"helmoci-remote","rclass":"remote","packageType":"helmoci","url":"http://127.0.0.1:9/v2/up"}`
	status, respBody, _ := s.do(http.MethodPut, "/binflow/api/repositories/helmoci-remote", adminUser, adminPass,
		strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if status != http.StatusBadRequest || !strings.Contains(respBody, "helmoci") {
		t.Fatalf("community helmoci remote create = (%d, %s), want the D3 400", status, respBody)
	}

	// Pro: the remote creation lands with its typed config.
	if _, err := s.license.Install(ctx, proDoc(t, keys)); err != nil {
		t.Fatalf("install pro: %v", err)
	}
	status, respBody, _ = s.do(http.MethodPut, "/binflow/api/repositories/helmoci-remote", adminUser, adminPass,
		strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if status != http.StatusOK {
		t.Fatalf("pro helmoci remote create = (%d, %s), want 200", status, respBody)
	}
	var created struct {
		Config json.RawMessage
	}
	if err := json.Unmarshal([]byte(respBody), &created); err != nil {
		t.Logf("create response is not JSON (%s) — asserting through the row instead", respBody)
	}
	row, err := s.md.Repos().Get(ctx, "helmoci-remote")
	if err != nil {
		t.Fatalf("row after create: %v", err)
	}
	if row.Type != repo.TypeRemote || row.PackageType != Protocol {
		t.Fatalf("row = (%s, %s), want (remote, helmoci)", row.Type, row.PackageType)
	}
	cfg, err := s.md.Remote().GetConfig(ctx, "helmoci-remote")
	if err != nil || cfg.URL != "http://127.0.0.1:9/v2/up" {
		t.Fatalf("remote config = (%v, %+v), want the typed url row", err, cfg)
	}

	// The docker package type rides the same /v2 remote seam since T-392
	// (FR-129): the create passes under the SAME pro document (docker's
	// slot is core-five, so it would also pass on community — pinned in
	// the repo package's own gate tests).
	dockerBody := `{"key":"docker-remote","rclass":"remote","packageType":"docker","url":"http://127.0.0.1:9/v2/up"}`
	status, respBody, _ = s.do(http.MethodPut, "/binflow/api/repositories/docker-remote", adminUser, adminPass,
		strings.NewReader(dockerBody), map[string]string{"Content-Type": "application/json"})
	if status != http.StatusOK {
		t.Fatalf("docker remote create = (%d, %s), want 200 (the seam admission)", status, respBody)
	}
}
