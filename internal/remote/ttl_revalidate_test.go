package remote

// The cache-v2 §5.1/§5.3 legs: the per-package-type content-TTL default
// (docker/helmoci 21600s, explicit values untouched) and the upstream
// conditional-GET revalidation arm (metadata class, stored validators).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
)

func TestResolveContentTTLSecondsPackageDefaults(t *testing.T) {
	tests := []struct {
		name        string
		explicit    int64
		packageType string
		want        int64
	}{
		{name: "explicit docker value wins", explicit: 7200, packageType: "docker", want: 7200},
		{name: "unset docker takes 21600", explicit: 0, packageType: "docker", want: 21600},
		{name: "unset helmoci takes 21600", explicit: 0, packageType: "helmoci", want: 21600},
		{name: "unset generic keeps 7200", explicit: 0, packageType: "generic", want: 7200},
		{name: "unset npm keeps 7200", explicit: 0, packageType: "npm", want: 7200},
		{name: "negative resolves as unset", explicit: -5, packageType: "maven", want: 7200},
		{name: "unknown type keeps 7200", explicit: 0, packageType: "", want: 7200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveContentTTLSeconds(tt.explicit, tt.packageType); got != tt.want {
				t.Fatalf("ResolveContentTTLSeconds(%d, %q) = %d, want %d",
					tt.explicit, tt.packageType, got, tt.want)
			}
		})
	}
}

func TestFetchContentTTLPackageDefaultResolution(t *testing.T) {
	// An unset docker row resolves its content window to 21600s at load; an
	// explicit 7200 row passes through untouched (存量不回改).
	e := newFetchEnv(t, func(row *metadata.Repo, cfg *metadata.RemoteConfig) {
		row.PackageType = "docker"
		cfg.ContentTTLSeconds = 0
	})
	e.state.files["/layer.bin"] = "layer-bytes"
	readAll(t, mustFetch(t, e, "layer.bin"))
	row, err := e.md.Remote().GetCache(context.Background(), "generic-remote", "layer.bin")
	if err != nil {
		t.Fatalf("cache row: %v", err)
	}
	// 2026-08-19T12:00:00Z + 21600s.
	if !hasRFC3339Prefix(row.ExpiresAt, "2026-08-19T18:00:00") {
		t.Fatalf("docker unset-row expiry = %s, want +21600s", row.ExpiresAt)
	}

	e2 := newFetchEnv(t, func(row *metadata.Repo, cfg *metadata.RemoteConfig) {
		row.PackageType = "docker"
		cfg.ContentTTLSeconds = 7200 // the standing explicit write
	})
	e2.state.files["/layer.bin"] = "layer-bytes"
	readAll(t, mustFetch(t, e2, "layer.bin"))
	row2, err := e2.md.Remote().GetCache(context.Background(), "generic-remote", "layer.bin")
	if err != nil {
		t.Fatalf("cache row: %v", err)
	}
	if !hasRFC3339Prefix(row2.ExpiresAt, "2026-08-19T14:00:00") {
		t.Fatalf("docker explicit-row expiry = %s, want +7200s (explicit untouched)", row2.ExpiresAt)
	}
}

func hasRFC3339Prefix(got, wantUTC string) bool {
	loc, _ := time.LoadLocation("UTC")
	w, err := time.ParseInLocation(time.RFC3339, wantUTC+"Z", loc)
	return err == nil && got == w.UTC().Format(time.RFC3339)
}

// condUpstream answers 304 when the request's If-None-Match matches the
// live body's ETag, else 200 with the live body; it records the validators
// it saw.
type condUpstream struct {
	etag atomic.Value // string
	body atomic.Value // string
	inm  atomic.Value // last If-None-Match seen ("" when absent)
}

func (c *condUpstream) serve(w http.ResponseWriter, r *http.Request) {
	inm := r.Header.Get("If-None-Match")
	c.inm.Store(inm)
	body, _ := c.body.Load().(string)
	etag, _ := c.etag.Load().(string)
	if inm != "" && inm == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	_, _ = w.Write([]byte(body))
}

func TestFetchMetadataRevalidationConditional304(t *testing.T) {
	registerTestProvider.Do(func() { adapter.RegisterMetadata(t66Provider{}) }) // .json = metadata, else content

	up := &condUpstream{}
	up.body.Store("manifest-v1")
	up.etag.Store(`"etag-v1"`)
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		up.serve(w, r)
	}))
	t.Cleanup(srv.Close)

	e := newFetchEnv(t, func(row *metadata.Repo, cfg *metadata.RemoteConfig) {
		row.PackageType = "npm" // the test provider classifies index.json as metadata
		cfg.URL = srv.URL
	})

	// First fetch: full 200, validator archived.
	if body := readAll(t, mustFetch(t, e, "index.json")); body != "manifest-v1" {
		t.Fatalf("first body = %q", body)
	}

	// Expire the metadata window. The upstream's tag is UNCHANGED (same
	// body, same ETag): the revalidation arm must carry If-None-Match with
	// the ARCHIVED ETag and the upstream's 304 lands as REVALIDATED with
	// the cached bytes.
	e.clk.Advance(601 * time.Second)

	res := mustFetch(t, e, "index.json")
	if res.CacheState != CacheRevalidated {
		t.Fatalf("revalidation state = %q, want REVALIDATED", res.CacheState)
	}
	if body := readAll(t, res); body != "manifest-v1" {
		t.Fatalf("revalidated body = %q, want the cached manifest-v1", body)
	}
	if seen, _ := up.inm.Load().(string); seen != `"etag-v1"` {
		t.Fatalf("upstream If-None-Match = %q, want the archived etag-v1", seen)
	}

	// The slid window serves HIT with zero upstream traffic.
	before := hits.Load()
	res2 := mustFetch(t, e, "index.json")
	if res2.CacheState != CacheHit {
		t.Fatalf("post-revalidation state = %q, want HIT", res2.CacheState)
	}
	readAll(t, res2)
	if got := hits.Load(); got != before {
		t.Fatalf("upstream hits after revalidation = %d, want %d", got, before)
	}

	// When the upstream DOES move (new body, new ETag), the same arm's
	// stale validator mismatches: the 200 replaces the copy wholesale.
	e.clk.Advance(601 * time.Second)
	up.body.Store("manifest-v2")
	up.etag.Store(`"etag-v2"`)
	res3 := mustFetch(t, e, "index.json")
	if res3.CacheState != CacheMiss {
		t.Fatalf("moved-upstream state = %q, want MISS (full replacement)", res3.CacheState)
	}
	if body := readAll(t, res3); body != "manifest-v2" {
		t.Fatalf("replaced body = %q, want manifest-v2", body)
	}
}

func TestFetchContentRefetchStaysUnconditional(t *testing.T) {
	// The conditional arm is metadata-class ONLY: an expired CONTENT-class
	// copy refetches unconditionally (immutable, checksum-addressed — the
	// window governs resolution, never a digest hit).
	registerTestProvider.Do(func() { adapter.RegisterMetadata(t66Provider{}) }) // .json = metadata, else content

	up := &condUpstream{}
	up.body.Store("blob-v1")
	up.etag.Store(`"etag-b1"`)
	srv := httptest.NewServer(http.HandlerFunc(up.serve))
	t.Cleanup(srv.Close)

	e := newFetchEnv(t, func(row *metadata.Repo, cfg *metadata.RemoteConfig) {
		row.PackageType = "npm"
		cfg.URL = srv.URL
	})
	if body := readAll(t, mustFetch(t, e, "pkg-1.0.0.tgz")); body != "blob-v1" {
		t.Fatalf("first body = %q", body)
	}
	e.clk.Advance(7201 * time.Second) // expire the content window
	res := mustFetch(t, e, "pkg-1.0.0.tgz")
	if res.CacheState != CacheMiss {
		t.Fatalf("expired content state = %q, want MISS", res.CacheState)
	}
	if seen, _ := up.inm.Load().(string); seen != "" {
		t.Fatalf("content-class refetch sent If-None-Match %q, want none", seen)
	}
	readAll(t, res)
}
