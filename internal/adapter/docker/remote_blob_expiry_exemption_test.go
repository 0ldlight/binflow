package docker

// LOOP 008 L008-2 ticket 1 (the D3 ruling, conductor LOOP 007 2026-09-12;
// evidence reports/compatibility/L007-d3-virtual-probe.md §1 — the
// known-divergence entry docker/remote-blob-expired-revalidation closed as
// BUG-align): the retrieval window EXEMPTS the digest-addressed blob face.
// Content-addressed bytes are immutable — a standing copy landed with the
// digest ENFORCED at commit is already the checksum-verified answer — so
// the expired arm serves the standing copy LOCALLY: zero upstream contact,
// the cache row untouched (the reference's posture across all four probe
// arms, 1×/3×TTL, INM, 10×TTL and a real docker pull). The MUTABLE manifest
// face keeps its window (an expired tag still revalidates upstream), and
// the negative-cache row keeps its own missedTTL semantics (pinned by the
// cold-miss files, unchanged here).

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
)

// TestRemoteBlobExpiryServesStandingCopy: the direct face's expired-arm
// table — every client-visible shape of an expired-window VERIFIED blob
// answers from the standing copy with the upstream counter frozen, while
// the manifest control arm still rides the window and re-contacts the
// upstream.
func TestRemoteBlobExpiryServesStandingCopy(t *testing.T) {
	down, _, hits, manifest, cfg, _ := newDockerRemoteFixture(t)

	// Warm: the manifest names the config blob (the ADR-0047 chain gate),
	// then the blob itself lands.
	if code, body, _ := down.get("/v2/docker-remote/up-local/myapp/manifests/1.0", acceptManifests); code != http.StatusOK || body != string(manifest) {
		t.Fatalf("warm manifest pull = (%d, %d bytes), want the proxied copy", code, len(body))
	}
	blobPath := "/v2/docker-remote/up-local/myapp/blobs/sha256:" + sha256Hex(cfg)
	if code, body, _ := down.get(blobPath, nil); code != http.StatusOK || body != string(cfg) {
		t.Fatalf("warm blob pull = (%d, %d bytes), want the proxied copy", code, len(body))
	}
	warm := hits.Load()

	// Expire ONLY the blob's retrieval window (the row-level clock) — the
	// manifest row stays fresh so the control arm measures the blob face
	// alone.
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if err := down.md.Remote().PutCache(context.Background(), &metadata.RemoteCacheEntry{
		RepoKey: "docker-remote", Path: "up-local/myapp/blobs/" + sha256Hex(cfg),
		Kind: metadata.RemoteCacheKindContent, FetchedAt: past, ExpiresAt: past,
	}); err != nil {
		t.Fatalf("expire the blob window: %v", err)
	}

	cases := []struct {
		name       string
		method     string
		hdr        map[string]string
		want       int
		wantMarker string
	}{
		{"expired GET serves the standing copy", http.MethodGet, nil, http.StatusOK, remote.CacheHit},
		{"expired GET x2 stays local", http.MethodGet, nil, http.StatusOK, remote.CacheHit},
		{"expired GET x3 stays local", http.MethodGet, nil, http.StatusOK, remote.CacheHit},
		{"expired INM answers a local 304", http.MethodGet,
			map[string]string{"If-None-Match": `"` + sumSha1(cfg) + `"`}, http.StatusNotModified, remote.CacheHit},
		{"expired HEAD serves the standing copy", http.MethodHead, nil, http.StatusOK, remote.CacheHit},
	}
	for _, tc := range cases {
		code, body, hdr := down.serveReq(tc.method, blobPath, nil, tc.hdr)
		if code != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, code, tc.want)
			continue
		}
		if tc.want == http.StatusOK && tc.method == http.MethodGet && body != string(cfg) {
			t.Errorf("%s body = %d bytes, want the standing copy verbatim", tc.name, len(body))
		}
		if got := hdr.Get(remote.HdrCacheState); got != tc.wantMarker {
			t.Errorf("%s cache marker = %q, want %q", tc.name, got, tc.wantMarker)
		}
	}
	if got := hits.Load(); got != warm {
		t.Fatalf("expired blob arms contacted the upstream: %d -> %d (the window exemption must freeze the counter)", warm, got)
	}

	// The manifest CONTROL arm — the window still gates the mutable face:
	// an expired tag re-opens the upstream conversation (the revalidation
	// hop; the exact REVALIDATED/MISS marker is TestManifestRevalidation-
	// Upstream304's pin, this arm only asserts the window still fires).
	if err := down.md.Remote().PutCache(context.Background(), &metadata.RemoteCacheEntry{
		RepoKey: "docker-remote", Path: "up-local/myapp/manifests/" + sha256Hex(manifest),
		Kind: metadata.RemoteCacheKindContent, FetchedAt: past, ExpiresAt: past,
	}); err != nil {
		t.Fatalf("expire the manifest window: %v", err)
	}
	code, body, _ := down.get("/v2/docker-remote/up-local/myapp/manifests/1.0", acceptManifests)
	if code != http.StatusOK || body != string(manifest) {
		t.Fatalf("expired manifest pull = (%d, %d bytes), want the refreshed copy", code, len(body))
	}
	if got := hits.Load(); got == warm {
		t.Fatal("the expired manifest arm never contacted the upstream — the window must keep gating the manifest face")
	}
}

// TestVirtualBlobExpiryServesStandingCopy: the walk's member-scoped twin —
// an expired-window verified blob on a remote MEMBER serves through the
// virtual with the upstream frozen and the winning member named.
func TestVirtualBlobExpiryServesStandingCopy(t *testing.T) {
	cfgBytes := []byte("{}")
	cfgDgst := "sha256:" + sha256Hex(cfgBytes)
	manifest := []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"%s","config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"%s","size":%d}}`,
		mediaTypeDockerManifest, cfgDgst, len(cfgBytes)))

	var hits atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/v2/up/known/manifests/1.0":
			w.Header().Set("Content-Type", mediaTypeDockerManifest)
			w.Header().Set("Docker-Content-Digest", "sha256:"+sha256Hex(manifest))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(manifest)
		case "/v2/up/known/blobs/" + cfgDgst:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(cfgBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(up.Close)

	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "virt-remote", up.URL)
	down.seedDockerVirtualRepo(t, "docker-virt", "virt-remote")

	// Warm through the virtual: the manifest lands the member's chain rows,
	// the blob lands the member's cache.
	if code, body, _ := down.get("/v2/docker-virt/up/known/manifests/1.0", acceptManifests); code != http.StatusOK || body != string(manifest) {
		t.Fatalf("warm virtual manifest pull = (%d, %d bytes)", code, len(body))
	}
	blobPath := "/v2/docker-virt/up/known/blobs/" + cfgDgst
	if code, body, _ := down.get(blobPath, nil); code != http.StatusOK || body != string(cfgBytes) {
		t.Fatalf("warm virtual blob pull = (%d, %d bytes)", code, len(body))
	}
	warm := hits.Load()

	// Expire the MEMBER's blob window; every re-ask through the virtual
	// must answer from the member's standing copy.
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if err := down.md.Remote().PutCache(context.Background(), &metadata.RemoteCacheEntry{
		RepoKey: "virt-remote", Path: "up/known/blobs/" + sha256Hex(cfgBytes),
		Kind: metadata.RemoteCacheKindContent, FetchedAt: past, ExpiresAt: past,
	}); err != nil {
		t.Fatalf("expire the member's blob window: %v", err)
	}

	cases := []struct {
		name string
		hdr  map[string]string
		want int
	}{
		{"expired virtual GET serves the standing copy", nil, http.StatusOK},
		{"expired virtual GET x2 stays local", nil, http.StatusOK},
		{"expired virtual INM answers a local 304", map[string]string{"If-None-Match": `"` + sumSha1(cfgBytes) + `"`}, http.StatusNotModified},
	}
	for _, tc := range cases {
		code, body, hdr := down.get(blobPath, tc.hdr)
		if code != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, code, tc.want)
			continue
		}
		if tc.want == http.StatusOK && body != string(cfgBytes) {
			t.Errorf("%s body = %d bytes, want the standing copy verbatim", tc.name, len(body))
		}
		if got := hdr.Get(remote.HdrCacheState); got != remote.CacheHit {
			t.Errorf("%s cache marker = %q, want %q", tc.name, got, remote.CacheHit)
		}
		if got := hdr.Get("X-BinFlow-Resolved-From"); got != "virt-remote" {
			t.Errorf("%s resolved-from = %q, want the winning member", tc.name, got)
		}
	}
	if got := hits.Load(); got != warm {
		t.Fatalf("expired virtual blob arms contacted the upstream: %d -> %d", warm, got)
	}
}
