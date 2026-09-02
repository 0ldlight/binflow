package remote

// T-423 (FR-139.2, T-377 D1): the busy retry budget on the remote
// cache-fill write path. The land() chain writes six metadata rows per
// fetched object (the session-row pair inside the storage engine plus the
// blob, node and cache-state upserts); under a 24-worker first-pull storm
// any of them can starve past the store's busy_timeout. These tests
// inject busy-class failures at every site and pin three behaviors: the
// fetch completes within the budget, an exhausted budget keeps the
// retryable verdict, and a busy failure is never blamed on the upstream
// (no assumed-offline mark).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// remoteBusyErr builds a store-shaped busy-class error (see the storage
// tests' twin: the classifier contract is errors.Is-based).
func remoteBusyErr(label string) error {
	return fmt.Errorf("metadata: %s: %w: database is locked (5) (SQLITE_BUSY)",
		label, metadata.ErrStoreBusy)
}

// flakyBlobStore fails the first N Puts busy-class, then delegates.
type flakyBlobStore struct {
	metadata.BlobStore
	fail, calls atomic.Int64
}

func (f *flakyBlobStore) Put(ctx context.Context, b *metadata.Blob) error {
	f.calls.Add(1)
	if f.fail.Add(-1) >= 0 {
		return remoteBusyErr("blobs put")
	}
	return f.BlobStore.Put(ctx, b)
}

// flakyNodeStore fails the first N Puts busy-class, then delegates.
type flakyNodeStore struct {
	metadata.NodeStore
	fail, calls atomic.Int64
}

func (f *flakyNodeStore) Put(ctx context.Context, n *metadata.Node) error {
	f.calls.Add(1)
	if f.fail.Add(-1) >= 0 {
		return remoteBusyErr("nodes put")
	}
	return f.NodeStore.Put(ctx, n)
}

// flakyRemoteStore fails the first N PutCaches busy-class, then delegates.
type flakyRemoteStore struct {
	metadata.RemoteStore
	fail, calls atomic.Int64
}

func (f *flakyRemoteStore) PutCache(ctx context.Context, e *metadata.RemoteCacheEntry) error {
	f.calls.Add(1)
	if f.fail.Add(-1) >= 0 {
		return remoteBusyErr("remote-cache put")
	}
	return f.RemoteStore.PutCache(ctx, e)
}

// flakySessions fails the first N creates/set-states busy-class, then
// delegates (the storage package's own twin covers its side; this one
// feeds the engine the remote test assembles).
type flakySessions struct {
	metadata.UploadSessionStore
	createFail, createCalls atomic.Int64
	setFail, setCalls       atomic.Int64
}

func (f *flakySessions) Create(ctx context.Context, u *metadata.UploadSession) error {
	f.createCalls.Add(1)
	if f.createFail.Add(-1) >= 0 {
		return remoteBusyErr("upload-sessions create")
	}
	return f.UploadSessionStore.Create(ctx, u)
}

func (f *flakySessions) SetState(ctx context.Context, id, state string) error {
	f.setCalls.Add(1)
	if f.setFail.Add(-1) >= 0 {
		return remoteBusyErr("upload-sessions set-state")
	}
	return f.UploadSessionStore.SetState(ctx, id, state)
}

// busyWrapMD routes selected sub-stores through the flaky wrappers while
// every other method delegates to the wrapped real store.
type busyWrapMD struct {
	metadata.Store
	sess   *flakySessions
	blobs  *flakyBlobStore
	nodes  *flakyNodeStore
	remote *flakyRemoteStore
}

func (m *busyWrapMD) UploadSessions() metadata.UploadSessionStore { return m.sess }
func (m *busyWrapMD) Blobs() metadata.BlobStore                   { return m.blobs }
func (m *busyWrapMD) Nodes() metadata.NodeStore                   { return m.nodes }
func (m *busyWrapMD) Remote() metadata.RemoteStore                { return m.remote }

// fastBusyRetry keeps the ladder semantics with no waiting.
func fastBusyRetry() storage.BusyRetryPolicy {
	return storage.BusyRetryPolicy{Attempts: 5, Backoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond}
}

// newBusyEnv assembles a fetchEnv whose storage engine and remote engine
// both see the busy-injecting metadata wrappers.
func newBusyEnv(t *testing.T) (*fetchEnv, *busyWrapMD) {
	t.Helper()
	ctx := context.Background()
	store, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	wrap := &busyWrapMD{
		Store:  store,
		sess:   &flakySessions{UploadSessionStore: store.UploadSessions()},
		blobs:  &flakyBlobStore{BlobStore: store.Blobs()},
		nodes:  &flakyNodeStore{NodeStore: store.Nodes()},
		remote: &flakyRemoteStore{RemoteStore: store.Remote()},
	}
	st, err := storage.OpenEngine(t.TempDir(), storage.Options{
		Sessions:  wrap.sess,
		BusyRetry: fastBusyRetry(),
	})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	state := &upstreamState{phase: "ok", files: map[string]string{}}
	hits := &atomic.Int64{}
	srv := newUpstreamServer(t, state, hits)
	clk := &fakeClock{now: time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)}
	eng, err := NewEngine(st, wrap, EngineOptions{Now: clk.Now, Logger: silentLogger, BusyRetry: fastBusyRetry()})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	e := &fetchEnv{st: st, md: store, eng: eng, clk: clk, srv: srv, hits: hits}
	e.state = state
	e.createRemote(t, "generic-remote", "generic", nil)
	return e, wrap
}

// newUpstreamServer starts the counting mock upstream (newFetchEnv's
// shape, split out so the busy env can assemble its own stack).
func newUpstreamServer(t *testing.T, state *upstreamState, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		state.handler()(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestLandRetriesBusyRowsAtEverySiteAndCompletesFetch injects one busy
// failure at each of the five write sites on the cache-fill path and pins
// the budgeted outcome: the fetch answers a MISS with the upstream's exact
// bytes, every retry ladder fired, and all three rows landed in the real
// store.
func TestLandRetriesBusyRowsAtEverySiteAndCompletesFetch(t *testing.T) {
	e, wrap := newBusyEnv(t)
	ctx := context.Background()
	const path = "blob.bin"
	e.state.files["/"+path] = "cache-fill-payload"

	wrap.sess.createFail.Store(1)
	wrap.sess.setFail.Store(1)
	wrap.blobs.fail.Store(1)
	wrap.nodes.fail.Store(1)
	wrap.remote.fail.Store(1)

	res, err := e.eng.Fetch(ctx, "generic-remote", path)
	if err != nil {
		t.Fatalf("Fetch under busy rows: %v", err)
	}
	defer res.Body.Close() //nolint:errcheck // test cleanup
	if res.CacheState != CacheMiss || !res.HasCopy {
		t.Fatalf("cache state = %q hasCopy=%t, want MISS with a copy", res.CacheState, res.HasCopy)
	}
	body, _ := io.ReadAll(res.Body)
	if string(body) != "cache-fill-payload" {
		t.Fatalf("served bytes = %q, want the upstream payload", body)
	}
	for _, site := range []struct {
		name string
		got  int64
	}{
		{"session create", wrap.sess.createCalls.Load()},
		{"session set-state", wrap.sess.setCalls.Load()},
		{"blob row", wrap.blobs.calls.Load()},
		{"node row", wrap.nodes.calls.Load()},
		{"cache-state row", wrap.remote.calls.Load()},
	} {
		if site.got < 2 {
			t.Fatalf("%s retried %d times, want the retry ladder to fire", site.name, site.got)
		}
	}
	node, err := e.md.Nodes().Get(ctx, "generic-remote", path)
	if err != nil {
		t.Fatalf("node row missing after the budgeted landing: %v", err)
	}
	if node.Size != int64(len("cache-fill-payload")) {
		t.Fatalf("node size = %d, want %d", node.Size, len("cache-fill-payload"))
	}
	if entry, err := e.md.Remote().GetCache(ctx, "generic-remote", path); err != nil {
		t.Fatalf("cache-state row missing after the budgeted landing: %v", err)
	} else if entry.Kind != metadata.RemoteCacheKindContent {
		t.Fatalf("cache-state kind = %q, want content", entry.Kind)
	}
}

// TestLandBusyAppendFailureIsBlamedLocallyNotUpstream pins the honesty
// rule: a busy-class failure while persisting the session state is a LOCAL
// fault — the fetch error keeps the retryable sentinel, carries no
// upstream-body marker, and the repository is NOT marked assumed offline
// (the next different path still contacts the upstream).
func TestLandBusyAppendFailureIsBlamedLocallyNotUpstream(t *testing.T) {
	e, wrap := newBusyEnv(t)
	ctx := context.Background()
	e.state.files["/a.bin"] = "aaa"
	e.state.files["/b.bin"] = "bbb"

	// A session-state persist that stays busy past a one-attempt budget:
	// Append fails busy on the first fetch.
	strict := storage.BusyRetryPolicy{Attempts: 1}
	wrap.sess.setFail.Store(1 << 30)
	e.eng.busyRetry = strict

	_, err := e.eng.Fetch(ctx, "generic-remote", "a.bin")
	if !metadata.IsStoreBusy(err) {
		t.Fatalf("err = %v, want the busy verdict on the local write path", err)
	}
	if errors.Is(err, errUpstreamBody) {
		t.Fatalf("busy failure mis-blamed on the upstream body: %v", err)
	}

	// Restore a working budget; the next fetch of a DIFFERENT path must
	// reach the upstream (an offline mark from the mis-blamed failure
	// would freeze upstream contacts inside the window).
	e.eng.busyRetry = fastBusyRetry()
	wrap.sess.setFail.Store(0)
	hitsBefore := e.hits.Load()
	res, err := e.eng.Fetch(ctx, "generic-remote", "b.bin")
	if err != nil {
		t.Fatalf("follow-up fetch: %v", err)
	}
	defer res.Body.Close() //nolint:errcheck // test cleanup
	if got := e.hits.Load(); got <= hitsBefore {
		t.Fatalf("upstream contacts %d -> %d: the busy failure marked the repository offline", hitsBefore, got)
	}
}

// TestLandBusyExhaustionKeepsRetryableVerdict pins the honest tail: when
// the whole budget is spent the fetch fails with the busy sentinel intact
// — the upper layer's 503-retry classification (T-54) still sees it.
func TestLandBusyExhaustionKeepsRetryableVerdict(t *testing.T) {
	e, wrap := newBusyEnv(t)
	ctx := context.Background()
	e.state.files["/x.bin"] = "xxx"
	wrap.blobs.fail.Store(1 << 30)

	_, err := e.eng.Fetch(ctx, "generic-remote", "x.bin")
	if !metadata.IsStoreBusy(err) {
		t.Fatalf("err = %v, want the busy sentinel past the exhausted budget", err)
	}
	if got := wrap.blobs.calls.Load(); got != 5 {
		t.Fatalf("blob-row executions = %d, want 5 (the whole budget)", got)
	}
}

// TestLandConcurrentCacheFillsStayConsistent is the package-level leg of
// the T-377 D1 shape: many DIFFERENT paths landing concurrently (the
// singleflight tests cover the same-path stampede). Every fetch must
// complete and every node row must be queryable afterwards.
func TestLandConcurrentCacheFillsStayConsistent(t *testing.T) {
	e, _ := newBusyEnv(t)
	ctx := context.Background()
	const workers, perWorker = 24, 5
	for w := 0; w < workers; w++ {
		for i := 0; i < perWorker; i++ {
			e.state.files[fmt.Sprintf("/w%02d/i%d.bin", w, i)] = fmt.Sprintf("payload-%02d-%d", w, i)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, workers*perWorker)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				path := fmt.Sprintf("w%02d/i%d.bin", w, i)
				res, err := e.eng.Fetch(ctx, "generic-remote", path)
				if err != nil {
					errs <- fmt.Errorf("%s: %w", path, err)
					continue
				}
				body, rerr := io.ReadAll(res.Body)
				_ = res.Body.Close()
				if rerr != nil {
					errs <- fmt.Errorf("%s: read: %w", path, rerr)
					continue
				}
				if want := fmt.Sprintf("payload-%02d-%d", w, i); string(body) != want {
					errs <- fmt.Errorf("%s: bytes %q, want %q", path, body, want)
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	nodes, err := e.md.Nodes().ListByPrefix(ctx, "generic-remote", "")
	if err != nil {
		t.Fatalf("ListByPrefix: %v", err)
	}
	if len(nodes) != workers*perWorker {
		t.Fatalf("landed nodes = %d, want %d", len(nodes), workers*perWorker)
	}
}
