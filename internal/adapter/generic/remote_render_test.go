package generic_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// T-66 rendering contract: the adapter stays class-agnostic while the
// service's remote outcomes reach the wire verbatim — a *repo.StatusError
// renders its own status/message/headers (RE-05's 405 + Allow, RE-04's 404
// wordings) and a hinted body stream adds X-BinFlow-Cache /
// X-Binflow-Upstream-Error. One end-to-end pass over a real remote
// repository and a real mock upstream proves both seams at once.

func TestRemoteOutcomesRenderThroughHandler(t *testing.T) {
	ctx := context.Background()
	dataDir, dbDir := t.TempDir(), t.TempDir()
	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	defer st.Close() //nolint:errcheck // teardown of the harness engine
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dbDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer md.Close() //nolint:errcheck // teardown of the harness store

	clk := &rclock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}
	svc := repo.NewWithClock(st, md, &allowAll{}, nil, clk.Now)

	hits := &atomic.Int64{}
	files := map[string]string{"/dir/up.bin": "upstream-bytes"}
	var offline atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if offline.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if b, ok := files[r.URL.Path]; ok {
			_, _ = w.Write([]byte(b))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(upstream.Close)

	if _, err := svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "generic-remote", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
		Config: `{"url":"` + upstream.URL + `","allowPrivateUpstream":true}`,
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	h := genericHandler(t, svc, md)

	// MISS: the first pull-through carries the cache header.
	res := h(t, http.MethodGet, "/binflow/generic-remote/dir/up.bin")
	if res.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("X-BinFlow-Cache"); got != "MISS" {
		t.Fatalf("X-BinFlow-Cache = %q, want MISS", got)
	}
	if res.Body.String() != "upstream-bytes" {
		t.Fatalf("body = %q", res.Body.String())
	}

	// HIT: the second serve is local (upstream counter frozen).
	res2 := h(t, http.MethodGet, "/binflow/generic-remote/dir/up.bin")
	if got := res2.Header().Get("X-BinFlow-Cache"); got != "HIT" {
		t.Fatalf("second X-BinFlow-Cache = %q, want HIT", got)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1", got)
	}

	// 405 + Allow on PUT (RE-05).
	res3 := h(t, http.MethodPut, "/binflow/generic-remote/x.bin")
	if res3.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT status = %d, want 405", res3.Code)
	}
	if got := res3.Header().Get("Allow"); got != "GET" {
		t.Fatalf("Allow = %q, want GET", got)
	}
	if !strings.Contains(res3.Body.String(), "read-only") {
		t.Fatalf("PUT body = %s", res3.Body.String())
	}

	// The exact 404 wording of the checksum sidecar (M45 equality).
	res4 := h(t, http.MethodGet, "/binflow/generic-remote/dir/up.bin.sha1")
	if res4.Code != http.StatusNotFound {
		t.Fatalf("sidecar status = %d", res4.Code)
	}
	if !strings.Contains(res4.Body.String(), "Checksums are not downloadable.") {
		t.Fatalf("sidecar body = %s", res4.Body.String())
	}

	// STALE + X-Binflow-Upstream-Error when the upstream faults and only an
	// expired copy remains (M44-1).
	clk.advance(7201 * time.Second)
	offline.Store(true)
	_ = h(t, http.MethodGet, "/binflow/generic-remote/dir/up.bin") // the faulting contact
	hitsAtFault := hits.Load()
	res5 := h(t, http.MethodGet, "/binflow/generic-remote/dir/up.bin")
	if res5.Code != http.StatusOK {
		t.Fatalf("stale serve status = %d", res5.Code)
	}
	if got := res5.Header().Get("X-BinFlow-Cache"); got != "STALE" {
		t.Fatalf("stale X-BinFlow-Cache = %q", got)
	}
	if res5.Header().Get("X-Binflow-Upstream-Error") == "" {
		t.Fatalf("stale serve must carry X-Binflow-Upstream-Error")
	}
	if res5.Body.String() != "upstream-bytes" {
		t.Fatalf("stale body = %q", res5.Body.String())
	}
	if got := hits.Load(); got != hitsAtFault {
		t.Fatalf("upstream hits inside the offline window = %d, want %d", got, hitsAtFault)
	}

	// No cached copy + no hardFail: the 404 names the offline state.
	res6 := h(t, http.MethodGet, "/binflow/generic-remote/uncached.bin")
	if res6.Code != http.StatusNotFound {
		t.Fatalf("offline uncached status = %d, want 404", res6.Code)
	}
	if !strings.Contains(res6.Body.String(), "assumed offline") {
		t.Fatalf("offline message = %s", res6.Body.String())
	}
}

// rclock is the remote-rendering test's controllable clock.
type rclock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *rclock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *rclock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// genericHandler mounts the real handler once and returns a request driver.
// The driver receives the FULL product path and strips /binflow itself —
// the mount contract httpapi's dispatchContent upholds (the adapter sees the
// repo key first, never the product prefix).
func genericHandler(t *testing.T, svc repo.Service, md metadata.Store) func(t *testing.T, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	handler := generic.New(svc, md.Blobs())
	return func(t *testing.T, method, target string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, strings.TrimPrefix(target, "/binflow"), strings.NewReader("payload"))
		req = req.WithContext(adapter.WithPrincipal(req.Context(), admin()))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
}
