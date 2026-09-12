package docker

// LOOP 005 C14 (the L004-2 verdict, reports/compatibility/
// L004-negative-cache-probe.md — ADR-0048's Errata clause fired, the
// manifest cold-miss negative cache is a BUG gap closed by alignment):
// the manifest face's COLD miss (no local rows) records a
// reference-keyed miss row on the upstream 404 and answers repeated asks
// locally inside the missedRetrievalCachePeriodSecs window, re-asking the
// upstream once it expires — the blob face's own posture (B2), extended to
// the reference forms the blob face cannot have: the tag (M-a, keyed under
// the image's tags/ namespace) and the digest (the standing arm's own
// manifest node path). The unknown-IMAGE form (M-b) rides the same
// reference-keyed memory: repeats answer locally, with the first ask still
// querying the upstream — the reference's zero-first-contact oracle is not
// locally computable without refusing images the cache has never served
// (which would kill pull-through), a residual documented in the ticket
// report for the conductor to adjudicate.
//
// The upstream counters are the L004-2 probe's own instrument: one ask
// counted at the upstream, repeats frozen, the miss row visible in the
// remote cache table.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// TestManifestColdMissNegativeCache: the cold-miss memory over one remote
// repository — tag miss (M-a) with its missedTTL expiry and re-ask, the
// digest miss, the unknown-image form (M-b), the HEAD+GET pull shape, and
// the pull-through escape (a tag the upstream HAS still serves through the
// same repository after misses were recorded).
func TestManifestColdMissNegativeCache(t *testing.T) {
	known := []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"%s","config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"sha256:%s","size":2}}`,
		mediaTypeDockerManifest, sha256Hex([]byte("{}"))))
	knownDgst := "sha256:" + sha256Hex(known)

	var hits atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/up/known/manifests/1.0" {
			hits.Add(1)
			w.Header().Set("Content-Type", mediaTypeDockerManifest)
			w.Header().Set("Docker-Content-Digest", knownDgst)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(known)
			return
		}
		hits.Add(1)
		w.WriteHeader(http.StatusNotFound) // every other reference misses upstream
	}))
	t.Cleanup(up.Close)

	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "docker-remote", up.URL)
	repoKey := "docker-remote"

	assert404 := func(t *testing.T, path string) {
		t.Helper()
		code, body, _ := down.get(path, acceptManifests)
		if code != http.StatusNotFound {
			t.Fatalf("GET %s = (%d, %s), want the unfound family", path, code, body)
		}
		var eb specErrorBody
		if err := json.Unmarshal([]byte(body), &eb); err != nil || len(eb.Errors) != 1 ||
			eb.Errors[0].Code != ErrCodeManifestUnknown {
			t.Fatalf("GET %s body %q: want one %s entry (%v)", path, body, ErrCodeManifestUnknown, err)
		}
	}

	t.Run("tag miss answers locally until the row expires", func(t *testing.T) {
		path := "/v2/docker-remote/up/known/manifests/l004miss"
		assert404(t, path)
		if got := hits.Load(); got != 1 {
			t.Fatalf("first tag miss: upstream contacts = %d, want 1", got)
		}
		assert404(t, path)
		assert404(t, path)
		if got := hits.Load(); got != 1 {
			t.Fatalf("repeat tag misses: upstream contacts = %d, want the count frozen at 1", got)
		}

		row, err := down.md.Remote().GetCache(context.Background(), repoKey, "up/known/tags/l004miss")
		if err != nil || row == nil {
			t.Fatalf("miss row up/known/tags/l004miss: (%v, %v), want the negative record", row, err)
		}
		if row.Kind != "negative" {
			t.Errorf("miss row kind = %q, want %q", row.Kind, "negative")
		}

		// Expire the window (the missedTTL clock): the next ask re-queries
		// the upstream and re-records the miss.
		past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
		if err := down.md.Remote().PutCache(context.Background(), &metadata.RemoteCacheEntry{
			RepoKey: repoKey, Path: "up/known/tags/l004miss", Kind: "negative", FetchedAt: past, ExpiresAt: past,
		}); err != nil {
			t.Fatalf("expire the miss row: %v", err)
		}
		assert404(t, path)
		if got := hits.Load(); got != 2 {
			t.Fatalf("post-expiry tag miss: upstream contacts = %d, want the re-ask (2)", got)
		}
		assert404(t, path)
		if got := hits.Load(); got != 2 {
			t.Fatalf("post-expiry repeat: upstream contacts = %d, want frozen at 2", got)
		}
	})

	t.Run("digest miss keys the manifest node path", func(t *testing.T) {
		path := "/v2/docker-remote/up/known/manifests/sha256:" + sha256Hex([]byte("absent-manifest"))
		before := hits.Load()
		assert404(t, path)
		assert404(t, path)
		if got := hits.Load() - before; got != 1 {
			t.Fatalf("digest miss x2: upstream contacts = %d, want 1", got)
		}
		if row, err := down.md.Remote().GetCache(context.Background(), repoKey,
			"up/known/manifests/"+sha256Hex([]byte("absent-manifest"))); err != nil || row == nil || row.Kind != "negative" {
			t.Fatalf("digest miss row: (%v, %v), want the negative record at the manifest node path", row, err)
		}
	})

	t.Run("unknown image repeats answer locally (M-b)", func(t *testing.T) {
		path := "/v2/docker-remote/up/absent-img/manifests/latest"
		before := hits.Load()
		assert404(t, path)
		if got := hits.Load() - before; got != 1 {
			t.Fatalf("first unknown-image ask: upstream contacts = %d, want 1 (the pull-through-preserving first ask)", got)
		}
		assert404(t, path)
		assert404(t, path)
		if got := hits.Load() - before; got != 1 {
			t.Fatalf("unknown-image repeats: upstream contacts = %d, want the count frozen at 1", got)
		}
	})

	t.Run("HEAD records the miss the GET then answers locally", func(t *testing.T) {
		before := hits.Load()
		code, _, _ := down.serveReq(http.MethodHead, "/v2/docker-remote/up/known/manifests/head-miss", nil, acceptManifests)
		if code != http.StatusNotFound {
			t.Fatalf("HEAD miss = %d, want 404", code)
		}
		assert404(t, "/v2/docker-remote/up/known/manifests/head-miss")
		if got := hits.Load() - before; got != 1 {
			t.Fatalf("HEAD+GET pull shape: upstream contacts = %d, want 1 (the row the HEAD wrote)", got)
		}
	})

	t.Run("a tag the upstream has still pulls through", func(t *testing.T) {
		before := hits.Load()
		code, body, hdr := down.get("/v2/docker-remote/up/known/manifests/1.0", acceptManifests)
		if code != http.StatusOK || body != string(known) {
			t.Fatalf("existing tag pull = (%d, %d bytes), want the proxied copy", code, len(body))
		}
		if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
			t.Errorf("existing tag first pull marker = %q, want MISS", got)
		}
		if got := hits.Load() - before; got != 1 {
			t.Fatalf("existing tag pull: upstream contacts = %d, want 1", got)
		}
		// And the second round trip is a HIT with the upstream frozen — the
		// miss memory did not leak onto other references.
		code, body, hdr = down.get("/v2/docker-remote/up/known/manifests/1.0", acceptManifests)
		if code != http.StatusOK || body != string(known) {
			t.Fatalf("existing tag second pull = (%d, %d bytes), want the cached copy", code, len(body))
		}
		if got := hdr.Get("X-BinFlow-Cache"); got != "HIT" {
			t.Errorf("existing tag second pull marker = %q, want HIT", got)
		}
		if got := hits.Load() - before; got != 1 {
			t.Fatalf("existing tag second pull: upstream contacts = %d, want frozen at 1", got)
		}
	})
}
