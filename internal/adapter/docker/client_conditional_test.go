package docker

// L004-1 + LOOP 005 D1 (live reference :8082, Artifactory-pro 7.161.20 —
// the overturn of L000-B E3-2's "If-None-Match 恒不被消费"; the D1 verdict
// reports/compatibility/L004-304-ping-diff.md §2): the remote read face's
// CLIENT conditional matrix. A GET against a valid cached copy answers 304
// when the client's validator matches, and the two faces evaluate
// preconditions IDENTICALLY — the If-None-Match comparison is by VALUE,
// quote- and weak-prefix-insensitive (the unquoted sha1 matches), and any
// present If-None-Match — matching or not — seals the If-Modified-Since
// arm; only the 304 RESPONSE shapes differ (the manifest face answers a
// BARE 304, the blob face one carrying the FULL artifact face). HEAD is
// never conditional on either face, and the upstream-revalidation arm (an
// upstream 304 on the expired tag path) slides the window and serves 200
// full with the REVALIDATED marker instead.
//
// D1 history: the first implementation parsed the manifest face's
// If-None-Match as a quoted-tag-only list and DROPPED unquoted spellings
// onto the date arm — live-falsified by the M2/M2c/M2d arms of the
// 18-arm matrix (the reference matches unquoted values and seals the date
// arm on ANY present If-None-Match); the falsified expectations were
// corrected with the fix in the same change.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
)

// TestClientConditionalManifestMatrix: the manifest face's precondition
// table, driven over a HIT copy of the standard fixture — one leg per
// observed reference behavior (capture a_mf_inm304.h + the L004-1 probe
// batch).
func TestClientConditionalManifestMatrix(t *testing.T) {
	down, _, _, manifest, _, _ := newDockerRemoteFixture(t)
	path := "/v2/docker-remote/up-local/myapp/manifests/1.0"
	if code, body, _ := down.get(path, acceptManifests); code != http.StatusOK || body != string(manifest) {
		t.Fatalf("warm-up pull = (%d, %d bytes), want the cached copy", code, len(body))
	}
	etag := sumSha1(manifest)
	lm := func() string {
		_, _, hdr := down.get(path, nil)
		return hdr.Get("Last-Modified")
	}()

	cases := []struct {
		name string
		hdr  map[string]string
		want int
	}{
		{"quoted etag matches", map[string]string{"If-None-Match": `"` + etag + `"`}, http.StatusNotModified},
		{"weak quoted etag matches", map[string]string{"If-None-Match": `W/"` + etag + `"`}, http.StatusNotModified},
		{"unquoted etag matches by value (D1: M2/M2d)", map[string]string{"If-None-Match": etag}, http.StatusNotModified},
		{"star does not match and seals the date arm", map[string]string{"If-None-Match": "*"}, http.StatusOK},
		{"quoted digest-form etag does not match", map[string]string{"If-None-Match": `"sha256:` + sha256Hex(manifest) + `"`}, http.StatusOK},
		{"quoted mismatch blocks the matching date arm", map[string]string{
			"If-None-Match": `"deadbeef"`, "If-Modified-Since": lm}, http.StatusOK},
		{"unquoted mismatch also seals the date arm (D1: M2c)", map[string]string{
			"If-None-Match": "deadbeef", "If-Modified-Since": lm}, http.StatusOK},
		{"date arm matches Last-Modified", map[string]string{"If-Modified-Since": lm}, http.StatusNotModified},
		{"date arm older than Last-Modified", map[string]string{
			"If-Modified-Since": "Thu, 01 Jan 2020 00:00:00 GMT"}, http.StatusOK},
		{"future date trivially matches", map[string]string{
			"If-Modified-Since": "Fri, 11 Sep 2027 04:25:03 GMT"}, http.StatusNotModified},
		{"unquoted etag match with the date arm riding along (D1: M6)", map[string]string{
			"If-None-Match": etag, "If-Modified-Since": lm}, http.StatusNotModified},
		{"no preconditions", nil, http.StatusOK},
	}
	for _, tc := range cases {
		code, body, hdr := down.get(path, withAccept(tc.hdr))
		if code != tc.want {
			t.Errorf("%s: GET = %d, want %d", tc.name, code, tc.want)
			continue
		}
		if tc.want == http.StatusNotModified {
			if body != "" {
				t.Errorf("%s: 304 carries a body: %q", tc.name, body)
			}
			// The manifest 304 is BARE (capture a_mf_inm304.h): api-version
			// and the cache marker alone — none of the validators, no
			// Content-Length, no Docker-Content-Digest.
			for _, absent := range []string{"Etag", "Last-Modified", hdrContentDigest, "Content-Length", hdrChecksumSha1} {
				if got := hdr.Get(absent); got != "" {
					t.Errorf("%s: 304 carries %s = %q, want it absent (the bare manifest 304)", tc.name, absent, got)
				}
			}
			if got := hdr.Get(remote.HdrCacheState); got != remote.CacheHit {
				t.Errorf("%s: 304 cache marker = %q, want HIT", tc.name, got)
			}
		}
	}

	// HEAD never answers 304 — the live capture's HEAD + matching quoted
	// etag answers the full 200 head set.
	req := httptest.NewRequest(http.MethodHead, path, nil)
	for k, v := range acceptManifests {
		req.Header.Set(k, v)
	}
	req.Header.Set("If-None-Match", `"`+etag+`"`)
	rec := httptest.NewRecorder()
	down.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD with matching INM = %d, want 200 (HEAD is never conditional)", rec.Code)
	}
}

// TestClientConditionalBlobMatrix: the blob face's precondition table —
// the same by-value, quote-insensitive token comparison the manifest face
// runs post-D1 (the faces differ only in the 304 response shape), any
// present If-None-Match blocking the date arm, and the 304 keeping the
// FULL artifact face.
func TestClientConditionalBlobMatrix(t *testing.T) {
	down, _, _, manifest, cfg, _ := newDockerRemoteFixture(t)
	// Pull the manifest first: the chain gate (ADR-0047) admits an uncached
	// blob only when a manifest chain named it.
	if code, _, _ := down.get("/v2/docker-remote/up-local/myapp/manifests/1.0", acceptManifests); code != http.StatusOK {
		t.Fatalf("warm-up manifest pull = %d, want 200", code)
	}
	_ = manifest
	path := "/v2/docker-remote/up-local/myapp/blobs/sha256:" + sha256Hex(cfg)
	if code, body, _ := down.get(path, nil); code != http.StatusOK || body != string(cfg) {
		t.Fatalf("warm-up blob pull = (%d, %d bytes), want the cached copy", code, len(body))
	}
	etag := sumSha1(cfg)
	_, _, hdr := down.get(path, nil)
	lm := hdr.Get("Last-Modified")

	cases := []struct {
		name string
		hdr  map[string]string
		want int
	}{
		{"quoted etag matches", map[string]string{"If-None-Match": `"` + etag + `"`}, http.StatusNotModified},
		{"unquoted etag ALSO matches (quote-insensitive)", map[string]string{"If-None-Match": etag}, http.StatusNotModified},
		{"weak quoted etag matches", map[string]string{"If-None-Match": `W/"` + etag + `"`}, http.StatusNotModified},
		{"star does not match", map[string]string{"If-None-Match": "*"}, http.StatusOK},
		{"digest-form etag does not match", map[string]string{
			"If-None-Match": `"sha256:` + sha256Hex(cfg) + `"`}, http.StatusOK},
		{"mismatch blocks the matching date arm", map[string]string{
			"If-None-Match": `"deadbeef"`, "If-Modified-Since": lm}, http.StatusOK},
		{"unquoted mismatch blocks the date arm too", map[string]string{
			"If-None-Match": "deadbeef", "If-Modified-Since": lm}, http.StatusOK},
		{"date arm matches Last-Modified", map[string]string{"If-Modified-Since": lm}, http.StatusNotModified},
	}
	for _, tc := range cases {
		code, body, hdr := down.get(path, tc.hdr)
		if code != tc.want {
			t.Errorf("%s: GET = %d, want %d", tc.name, code, tc.want)
			continue
		}
		if tc.want == http.StatusNotModified {
			if body != "" {
				t.Errorf("%s: 304 carries a body: %q", tc.name, body)
			}
			// The blob 304 keeps the FULL face (capture: Etag,
			// Last-Modified, the checksum family, the digest, the filename
			// pair) — the manifest face's bare 304 is not this face.
			for _, present := range []string{"Etag", "Last-Modified", hdrContentDigest, hdrChecksumSha1, hdrFilename} {
				if got := hdr.Get(present); got == "" {
					t.Errorf("%s: blob 304 is missing %s (the full-face 304)", tc.name, present)
				}
			}
			if got := hdr.Get(hdrContentDisposition); !strings.Contains(got, "sha256__") {
				t.Errorf("%s: blob 304 disposition = %q, want the sha256__ filename face", tc.name, got)
			}
		}
	}

	// HEAD never conditional.
	req := httptest.NewRequest(http.MethodHead, path, nil)
	req.Header.Set("If-None-Match", `"`+etag+`"`)
	rec := httptest.NewRecorder()
	down.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD with matching INM = %d, want 200", rec.Code)
	}
}

// TestManifestRevalidationUpstream304: the expired tag path rides the
// upstream conditional hop (If-None-Match = the quoted digest etag); an
// upstream 304 slides the retrieval window (the next request is a HIT,
// zero upstream packets) and serves 200 full with the REVALIDATED marker —
// the client's own conditionals are NOT evaluated on this arm (the live
// reference's revalidation serve answers 200 full).
func TestManifestRevalidationUpstream304(t *testing.T) {
	manifest := []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"%s","config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"sha256:%s","size":2}}`,
		mediaTypeDockerManifest, sha256Hex([]byte("{}"))))
	dgst := "sha256:" + sha256Hex(manifest)
	validator := `"` + dgst + `"`

	var hops, conditional atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/manifests/1.0") {
			http.NotFound(w, r)
			return
		}
		hops.Add(1)
		if r.Header.Get("If-None-Match") != "" {
			conditional.Add(1)
		}
		if r.Header.Get("If-None-Match") == validator {
			w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", mediaTypeDockerManifest)
		w.Header().Set("Docker-Content-Digest", dgst)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(manifest)
	}))
	defer up.Close()

	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "reval-remote", up.URL)
	path := "/v2/reval-remote/myapp/manifests/1.0"

	// Cold pull (MISS): unconditional upstream 200, the copy lands.
	if code, body, _ := down.get(path, acceptManifests); code != http.StatusOK || body != string(manifest) {
		t.Fatalf("cold pull = (%d, %d bytes), want the proxied copy", code, len(body))
	}
	if conditional.Load() != 0 {
		t.Fatalf("cold pull rode a conditional hop (count %d)", conditional.Load())
	}

	// Expire the retrieval window (the row-level clock).
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if err := down.md.Remote().PutCache(context.Background(), &metadata.RemoteCacheEntry{
		RepoKey: "reval-remote", Path: "myapp/manifests/" + sha256Hex(manifest),
		Kind: metadata.RemoteCacheKindContent, FetchedAt: past, ExpiresAt: past,
	}); err != nil {
		t.Fatalf("expire the window: %v", err)
	}

	// Expired pull: the upstream sees the conditional hop, answers 304, and
	// the client gets the full 200 copy with the REVALIDATED marker — even
	// with a matching client INM (this arm does not evaluate it).
	code, body, hdr := down.get(path, withAccept(map[string]string{"If-None-Match": `"nope"`}))
	if code != http.StatusOK || body != string(manifest) {
		t.Fatalf("revalidated pull = (%d, %d bytes), want the full copy", code, len(body))
	}
	if got := hdr.Get(remote.HdrCacheState); got != remote.CacheRevalidated {
		t.Errorf("revalidated pull marker = %q, want REVALIDATED", got)
	}
	if conditional.Load() != 1 {
		t.Fatalf("expired pull did not ride the conditional hop (count %d)", conditional.Load())
	}

	// The window slid: the next pull is a HIT with zero upstream packets.
	after := hops.Load()
	if code, body, hdr = down.get(path, acceptManifests); code != http.StatusOK || body != string(manifest) {
		t.Fatalf("post-revalidation pull = (%d, %d bytes), want the cached copy", code, len(body))
	}
	if got := hdr.Get(remote.HdrCacheState); got != remote.CacheHit {
		t.Errorf("post-revalidation marker = %q, want HIT (the window slid)", got)
	}
	if hops.Load() != after {
		t.Fatalf("post-revalidation pull contacted the upstream: %d -> %d", after, hops.Load())
	}
}

// withAccept overlays the manifest Accept set on one probe's headers (the
// conditional legs ride real negotiation).
func withAccept(hdr map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range acceptManifests {
		out[k] = v
	}
	for k, v := range hdr {
		out[k] = v
	}
	return out
}
