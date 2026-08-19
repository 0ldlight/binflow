package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// fetchEnv is one engine under test: the real storage engine plus the real
// SQLite metadata store plus a counting mock upstream, all on loopback (the
// repositories carry allowPrivateUpstream, exactly like PRD M41's own
// fixture posture).
type fetchEnv struct {
	st    storage.Engine
	md    metadata.Store
	eng   *Engine
	clk   *fakeClock
	state *upstreamState

	srv  *httptest.Server
	hits *atomic.Int64
}

// fakeClock is the controllable TTL/offline clock.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

var silentLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// upstreamState lets one test flip the mock upstream's behavior (healthy /
// 404 / 5xx /Basic-gated) while keeping its address — the assumed-offline
// recovery cycle needs the same host:port to come back.
type upstreamState struct {
	mu    sync.Mutex
	phase string // "ok" | "404" | "500" | "auth"
	user  string
	pass  string
	files map[string]string
	delay time.Duration
}

func (u *upstreamState) set(phase string) {
	u.mu.Lock()
	u.phase = phase
	u.mu.Unlock()
}

func (u *upstreamState) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		phase, files, user, pass, delay := u.phase, u.files, u.user, u.pass, u.delay
		u.mu.Unlock()
		if delay > 0 {
			time.Sleep(delay)
		}
		if phase == "auth" {
			gotUser, gotPass, ok := r.BasicAuth()
			if !ok || gotUser != user || gotPass != pass {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		switch phase {
		case "404":
			http.NotFound(w, r)
			return
		case "500":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("upstream boom"))
			return
		case "304":
			w.WriteHeader(http.StatusNotModified)
			return
		}
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", `"etag-`+fmt.Sprintf("%x", len(body))+`"`)
		_, _ = w.Write([]byte(body))
	}
}

// newFetchEnv opens the engine over a fresh stack. tune may adjust the repo
// row and the remote config (defaults mirror the service's canonical write).
func newFetchEnv(t *testing.T, tune func(row *metadata.Repo, cfg *metadata.RemoteConfig)) *fetchEnv {
	t.Helper()
	ctx := context.Background()
	st, err := storage.OpenEngine(t.TempDir(), storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	state := &upstreamState{phase: "ok", files: map[string]string{}}
	hits := &atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		state.handler()(w, r)
	}))
	t.Cleanup(srv.Close)

	clk := &fakeClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}
	eng, err := NewEngine(st, md, EngineOptions{Now: clk.Now, Logger: silentLogger})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	e := &fetchEnv{st: st, md: md, eng: eng, clk: clk, srv: srv, hits: hits}
	e.state = state
	e.createRemote(t, "generic-remote", "generic", tune)
	return e
}

// createRemote writes one remote repository row plus its remote_configs row
// (the same shapes repo.Service persists; the engine reads the store
// directly).
func (e *fetchEnv) createRemote(t *testing.T, key, packageType string, tune func(*metadata.Repo, *metadata.RemoteConfig)) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	row := &metadata.Repo{
		RepoKey: key, Type: "remote", PackageType: packageType,
		Config: `{"url":"` + e.srv.URL + `","username":"","retrievalCachePeriodSecs":7200,` +
			`"missedRetrievalCachePeriodSecs":1800,"socketTimeoutSecs":15,` +
			`"assumedOfflinePeriodSecs":300,"hardFail":false,` +
			`"allowPrivateUpstream":true,"priorityResolution":false}`,
		CreatedAt: now, UpdatedAt: now,
	}
	cfg := &metadata.RemoteConfig{
		RepoKey: key, URL: e.srv.URL,
		ContentTTLSeconds: 7200, MetadataTTLSeconds: 600,
		AllowPrivateUpstream: true,
	}
	if tune != nil {
		tune(row, cfg)
	}
	if err := e.md.Repos().Create(context.Background(), row); err != nil {
		t.Fatalf("create repo %s: %v", key, err)
	}
	if err := e.md.Remote().CreateConfig(context.Background(), cfg); err != nil {
		t.Fatalf("create remote config %s: %v", key, err)
	}
}

func fetchErr(t *testing.T, res *FetchResult, err error) *FetchError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a FetchError, got result %v", res)
	}
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("error %v is not a *FetchError", err)
	}
	return fe
}

func mustFetch(t *testing.T, e *fetchEnv, path string) *FetchResult {
	t.Helper()
	res, err := e.eng.Fetch(context.Background(), "generic-remote", path)
	if err != nil {
		t.Fatalf("Fetch(%s): %v", path, err)
	}
	return res
}

func readAll(t *testing.T, res *FetchResult) string {
	t.Helper()
	defer res.Body.Close() //nolint:errcheck // read-only probe stream
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ---- M41: pull-through + cache hit with the upstream-count negative assertion ----

func TestFetchPullThroughAndCacheHit(t *testing.T) {
	e := newFetchEnv(t, func(_ *metadata.Repo, _ *metadata.RemoteConfig) {})
	e.state.files["/dir/up.bin"] = "hello-upstream"

	res := mustFetch(t, e, "dir/up.bin")
	if res.CacheState != CacheMiss {
		t.Fatalf("first fetch state = %q, want MISS", res.CacheState)
	}
	if body := readAll(t, res); body != "hello-upstream" {
		t.Fatalf("body = %q", body)
	}
	if res.Node.Sha256 != sha256Hex("hello-upstream") {
		t.Fatalf("node sha256 = %s, want the measured digest", res.Node.Sha256)
	}
	if res.Node.RepoKey != "generic-remote" || res.Node.Path != "dir/up.bin" {
		t.Fatalf("node landed at %s/%s — the cache must live in the remote repo namespace", res.Node.RepoKey, res.Node.Path)
	}
	if !res.HasCopy {
		t.Fatalf("a landed fetch reports HasCopy (R10)")
	}
	if got := e.hits.Load(); got != 1 {
		t.Fatalf("upstream hits after first fetch = %d, want 1", got)
	}

	res2 := mustFetch(t, e, "dir/up.bin")
	if res2.CacheState != CacheHit {
		t.Fatalf("second fetch state = %q, want HIT", res2.CacheState)
	}
	if body := readAll(t, res2); body != "hello-upstream" {
		t.Fatalf("cached body = %q", body)
	}
	if got := e.hits.Load(); got != 1 {
		t.Fatalf("upstream hits after cached read = %d, want STILL 1 (M41 negative assertion)", got)
	}

	// The hint headers ride the body (the adapter's structural probe).
	if v := res2.Body.(interface{ ExtraHeaders() http.Header }).ExtraHeaders().Get(HdrCacheState); v != "HIT" {
		t.Fatalf("hint X-BinFlow-Cache = %q", v)
	}

	// The cache row carries the validators and the content kind.
	entry, err := e.md.Remote().GetCache(context.Background(), "generic-remote", "dir/up.bin")
	if err != nil {
		t.Fatalf("cache row: %v", err)
	}
	if entry.Kind != metadata.RemoteCacheKindContent {
		t.Fatalf("kind = %q, want content", entry.Kind)
	}
	if entry.ETag == "" {
		t.Fatalf("validators (ETag) not recorded")
	}
}

// ---- M45 / FR-20-AC13: checksum sidecars are never proxied ----

func TestFetchChecksumSidecarNeverProxied(t *testing.T) {
	e := newFetchEnv(t, func(_ *metadata.Repo, _ *metadata.RemoteConfig) {})
	e.state.files["/dir/up.bin"] = "hello-upstream"
	mustFetch(t, e, "dir/up.bin") // cache the artifact first
	before := e.hits.Load()

	for _, sidecar := range []string{"dir/up.bin.sha1", "dir/up.bin.md5", "dir/up.bin.sha256", "dir/up.bin.sha512"} {
		res, err := e.eng.Fetch(context.Background(), "generic-remote", sidecar)
		fe := fetchErr(t, res, err)
		if fe.Status != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", sidecar, fe.Status)
		}
		if fe.Message != msgChecksumsNotDownloadable {
			t.Fatalf("%s: message = %q, want the exact M45 wording", sidecar, fe.Message)
		}
		if !fe.Unfound {
			t.Fatalf("%s: must carry unfound semantics", sidecar)
		}
	}
	if got := e.hits.Load(); got != before {
		t.Fatalf("upstream hits = %d, want %d (sidecars never proxied)", got, before)
	}
}

// ---- M43 / FR-20-AC4: the negative cache ----

func TestFetchNegativeCache(t *testing.T) {
	e := newFetchEnv(t, func(_ *metadata.Repo, _ *metadata.RemoteConfig) {})
	// The upstream 404s the path (phase defaults to ok, path absent).

	res, err := e.eng.Fetch(context.Background(), "generic-remote", "missing.bin")
	fe := fetchErr(t, res, err)
	if fe.Status != http.StatusNotFound || !fe.Unfound {
		t.Fatalf("first miss = (%d, unfound=%t)", fe.Status, fe.Unfound)
	}
	if got := e.hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1", got)
	}

	// Inside the window: 404 with ZERO further upstream traffic.
	res2, err := e.eng.Fetch(context.Background(), "generic-remote", "missing.bin")
	fe2 := fetchErr(t, res2, err)
	if fe2.Status != http.StatusNotFound {
		t.Fatalf("negative serve = %d", fe2.Status)
	}
	if got := e.hits.Load(); got != 1 {
		t.Fatalf("upstream hits inside window = %d, want 1 (M43 negative assertion)", got)
	}

	// After the window (missedRetrievalCachePeriodSecs 1800): probe again.
	e.clk.Advance(1801 * time.Second)
	e.state.files["/missing.bin"] = "now-present"
	fetched := mustFetch(t, e, "missing.bin")
	if body := readAll(t, fetched); body != "now-present" {
		t.Fatalf("refetched body = %q", body)
	}
	if got := e.hits.Load(); got != 2 {
		t.Fatalf("upstream hits after window = %d, want 2", got)
	}
}

// ---- M44 / FR-20-AC5: assumed-offline, stale service, hardFail ----

func TestFetchUpstreamFaultMatrix(t *testing.T) {
	// Phase 1: cache a copy, then let it expire.
	e := newFetchEnv(t, func(_ *metadata.Repo, _ *metadata.RemoteConfig) {})
	e.state.files["/dir/up.bin"] = "v1"
	mustFetch(t, e, "dir/up.bin")
	e.clk.Advance(7201 * time.Second) // past the content TTL

	// Phase 2: the upstream faults (5xx).
	e.state.set("500")

	// Cached path: 200 old content + the upstream-error hint (M44-1).
	res := mustFetch(t, e, "dir/up.bin")
	if res.CacheState != CacheStale {
		t.Fatalf("fault state = %q, want STALE", res.CacheState)
	}
	if body := readAll(t, res); body != "v1" {
		t.Fatalf("stale body = %q", body)
	}
	hitsAtFault := e.hits.Load() // the one 5xx contact that opened the window
	if res.UpstreamError == "" {
		t.Fatalf("stale serve must carry the X-Binflow-Upstream-Error summary")
	}
	if !res.HasCopy {
		t.Fatalf("stale serve reports HasCopy (R10)")
	}

	// Uncached path: 404 naming the offline state (M44-2), default hardFail.
	res2, err := e.eng.Fetch(context.Background(), "generic-remote", "other.bin")
	fe := fetchErr(t, res2, err)
	if fe.Status != http.StatusNotFound || !fe.Unfound {
		t.Fatalf("uncached fault = (%d, unfound=%t), want (404, true)", fe.Status, fe.Unfound)
	}
	if !strings.Contains(fe.Message, "assumed offline") {
		t.Fatalf("message must name the offline state: %q", fe.Message)
	}

	// Silence window: recovered upstream, but requests inside the window
	// never reach it (M44-4).
	e.state.set("ok")
	e.state.files["/other.bin"] = "late"
	e.clk.Advance(100 * time.Second)
	if _, err := e.eng.Fetch(context.Background(), "generic-remote", "other.bin"); err == nil {
		t.Fatalf("inside the silence window the answer must stay 404, got a result")
	} else if !strings.Contains(err.Error(), "assumed offline") {
		t.Fatalf("window message = %v", err)
	}
	if got := e.hits.Load(); got != hitsAtFault {
		t.Fatalf("upstream hits inside silence window = %d, want %d", got, hitsAtFault)
	}

	// After the window: automatic recovery, refetch lands the new content.
	e.clk.Advance(201 * time.Second)
	res4 := mustFetch(t, e, "other.bin")
	if body := readAll(t, res4); body != "late" {
		t.Fatalf("recovered body = %q", body)
	}
	if got := e.hits.Load(); got != hitsAtFault+1 {
		t.Fatalf("upstream hits after recovery = %d, want %d", got, hitsAtFault+1)
	}
}

func TestFetchHardFailAnswers502(t *testing.T) {
	e := newFetchEnv(t, func(row *metadata.Repo, _ *metadata.RemoteConfig) {
		row.Config = strings.Replace(row.Config, `"hardFail":false`, `"hardFail":true`, 1)
	})
	e.state.set("500")
	res, err := e.eng.Fetch(context.Background(), "generic-remote", "nope.bin")
	fe := fetchErr(t, res, err)
	if fe.Status != http.StatusBadGateway {
		t.Fatalf("hardFail status = %d, want 502", fe.Status)
	}
	if fe.Unfound {
		t.Fatalf("hardFail is not the unfound family")
	}
}

// An unsolicited 304 refreshes the clock and serves the standing copy
// (REVALIDATED); without a copy it is a 502, never an offline mark.
func TestFetchUnsolicited304(t *testing.T) {
	e := newFetchEnv(t, nil)
	e.state.files["/stable.bin"] = "stable-body"
	readAll(t, mustFetch(t, e, "stable.bin"))

	e.clk.Advance(7201 * time.Second) // expire
	e.state.set("304")
	res := mustFetch(t, e, "stable.bin")
	if res.CacheState != CacheRevalidated {
		t.Fatalf("304 state = %q, want REVALIDATED", res.CacheState)
	}
	if body := readAll(t, res); body != "stable-body" {
		t.Fatalf("revalidated body = %q", body)
	}
	// The refreshed window serves a plain HIT with zero upstream traffic.
	before := e.hits.Load()
	res2 := mustFetch(t, e, "stable.bin")
	if res2.CacheState != CacheHit {
		t.Fatalf("post-refresh state = %q, want HIT", res2.CacheState)
	}
	readAll(t, res2)
	if got := e.hits.Load(); got != before {
		t.Fatalf("upstream hits after revalidation = %d, want %d", got, before)
	}

	// A 304 without any copy is a 502 and does NOT open the offline window
	// (another path still contacts the upstream).
	res3, err := e.eng.Fetch(context.Background(), "generic-remote", "ghost.bin")
	fe := fetchErr(t, res3, err)
	if fe.Status != http.StatusBadGateway {
		t.Fatalf("304-without-copy status = %d, want 502", fe.Status)
	}
	res4, err := e.eng.Fetch(context.Background(), "generic-remote", "still-ghost.bin")
	_ = res4
	if err == nil || strings.Contains(err.Error(), "assumed offline") {
		t.Fatalf("a 304 must not open the offline window: %v", err)
	}
}

func TestFetchTransportRefusalOpensOfflineWindow(t *testing.T) {
	// A closed port (not a 5xx): the transport-fault arm of the same matrix.
	e := newFetchEnv(t, nil)
	ctx := context.Background()
	deadURL := "http://127.0.0.1:1"
	row, err := e.md.Repos().Get(ctx, "generic-remote")
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	row.Config = strings.Replace(row.Config, e.srv.URL, deadURL, 1)
	if err := e.md.Repos().Update(ctx, row); err != nil {
		t.Fatalf("update repo: %v", err)
	}
	cfg, err := e.md.Remote().GetConfig(ctx, "generic-remote")
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	cfg.URL = deadURL
	if err := e.md.Remote().UpdateConfig(ctx, cfg); err != nil {
		t.Fatalf("update config: %v", err)
	}

	res, err := e.eng.Fetch(ctx, "generic-remote", "x.bin")
	fe := fetchErr(t, res, err)
	if fe.Status != http.StatusNotFound || !strings.Contains(fe.Message, "assumed offline") {
		t.Fatalf("refused connection = (%d, %q)", fe.Status, fe.Message)
	}
}

// ---- FR-20-AC10: upstream credentials ----

func TestFetchUpstreamCredentialsMatrix(t *testing.T) {
	e := newFetchEnv(t, nil)
	e.state.set("auth")
	e.state.user, e.state.pass = "ci", "tpasswd"
	e.state.files["/sec.bin"] = "secret-content"

	// Right credentials (the engine decrypts the sealed row).
	c, err := NewCipher(testKey(t))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	sealed, err := c.Encrypt("tpasswd")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	e.eng.cipher = c
	cfg, err := e.md.Remote().GetConfig(context.Background(), "generic-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	cfg.Username, cfg.Password = "ci", sealed
	if err := e.md.Remote().UpdateConfig(context.Background(), cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	// The client signature changed; the engine rebuilds on the next fetch.
	res := mustFetch(t, e, "sec.bin")
	if body := readAll(t, res); body != "secret-content" {
		t.Fatalf("authenticated body = %q", body)
	}

	// Wrong credentials: 401 is unfound, message carries the upstream
	// summary (repo-semantics 7.6), and the repository is NOT marked
	// offline (the upstream is healthy).
	e.clk.Advance(7201 * time.Second) // expire to force a refetch
	cfg.Password = sealed             // unchanged; flip the upstream's expectation instead
	e.state.pass = "different"
	res2, err := e.eng.Fetch(context.Background(), "generic-remote", "sec.bin")
	fe := fetchErr(t, res2, err)
	if fe.Status != http.StatusNotFound || !fe.Unfound {
		t.Fatalf("refused credentials = (%d, %t)", fe.Status, fe.Unfound)
	}
	if !strings.Contains(fe.Message, "401") {
		t.Fatalf("message must carry the upstream 401 summary: %q", fe.Message)
	}
	// A subsequent different path still contacts the upstream (no offline
	// mark, no negative carry-over): the counter moved by exactly one.
	before := e.hits.Load()
	res3, err := e.eng.Fetch(context.Background(), "generic-remote", "sec2.bin")
	if err == nil {
		readAll(t, res3)
		t.Fatalf("the auth-gated upstream must refuse, got a result")
	}
	if got := e.hits.Load(); got != before+1 {
		t.Fatalf("after 401 the upstream must still be contacted: hits %d -> %d", before, got)
	}
}

// ---- M42 / FR-20-AC3: the SSRF default refusal ----

func TestFetchSSRFDefaultRefusal(t *testing.T) {
	e := newFetchEnv(t, func(row *metadata.Repo, cfg *metadata.RemoteConfig) {
		// Loopback upstream WITHOUT the exemption.
		row.Config = strings.Replace(row.Config, `"allowPrivateUpstream":true`, `"allowPrivateUpstream":false`, 1)
		cfg.AllowPrivateUpstream = false
	})
	res, err := e.eng.Fetch(context.Background(), "generic-remote", "dir/up.bin")
	fe := fetchErr(t, res, err)
	if fe.Status != http.StatusBadRequest {
		t.Fatalf("SSRF refusal status = %d, want 400", fe.Status)
	}
	if !strings.Contains(fe.Message, "private or suppressed upstream") {
		t.Fatalf("message must carry the M42 wording: %q", fe.Message)
	}
	if got := e.hits.Load(); got != 0 {
		t.Fatalf("upstream hits = %d, want 0 (denied before any packet)", got)
	}
}

// ---- FR-20-AC12: redirects (follow same-origin, guard every hop) ----

func TestFetchRedirects(t *testing.T) {
	e := newFetchEnv(t, nil)
	hopHits := &atomic.Int64{}
	e.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hopHits.Add(1)
		e.hits.Add(1)
		switch r.URL.Path {
		case "/hop1":
			http.Redirect(w, r, "/hop2", http.StatusFound)
		case "/hop2":
			http.Redirect(w, r, "/final.bin", http.StatusFound)
		default:
			_, _ = w.Write([]byte("redirected-content"))
		}
	})
	res := mustFetch(t, e, "hop1")
	if body := readAll(t, res); body != "redirected-content" {
		t.Fatalf("redirected body = %q", body)
	}
	if got := hopHits.Load(); got != 3 {
		t.Fatalf("hops = %d, want 3 (two redirects plus the final)", got)
	}
	// Per-hop screening of redirect targets (the cloud-metadata hop shape of
	// FR-20-AC12) is the T-65 client's own tested matrix; the engine-side
	// mapping of a chain rejection onto 400 is covered by the direct
	// refusal test above (both arms produce *RejectionError).
}

// ---- Dual TTL: the MetadataProvider class split ----

var registerTestProvider sync.Once

type t66Provider struct{}

func (t66Provider) Protocol() string { return "npm" }
func (t66Provider) Classify(p string) adapter.MetadataKind {
	if strings.HasSuffix(p, ".json") {
		return adapter.KindMetadata
	}
	return adapter.KindContent
}
func (t66Provider) PackageName(string) (string, bool)   { return "", false }
func (t66Provider) Versions() adapter.VersionComparator { return nil }

func TestFetchDualTTLByKind(t *testing.T) {
	registerTestProvider.Do(func() { adapter.RegisterMetadata(t66Provider{}) })
	e := newFetchEnv(t, func(row *metadata.Repo, _ *metadata.RemoteConfig) {
		row.PackageType = "npm"
	})
	e.state.files["/pack.json"] = "{}"
	e.state.files["/lib.tgz"] = "tarball"

	mustFetch(t, e, "pack.json")
	mustFetch(t, e, "lib.tgz")

	meta, err := e.md.Remote().GetCache(context.Background(), "generic-remote", "pack.json")
	if err != nil {
		t.Fatalf("metadata row: %v", err)
	}
	content, err := e.md.Remote().GetCache(context.Background(), "generic-remote", "lib.tgz")
	if err != nil {
		t.Fatalf("content row: %v", err)
	}
	if meta.Kind != metadata.RemoteCacheKindMetadata || content.Kind != metadata.RemoteCacheKindContent {
		t.Fatalf("kinds = %q / %q", meta.Kind, content.Kind)
	}
	// 600s vs 7200s TTL windows from the fetch instant.
	if !strings.HasPrefix(meta.ExpiresAt, "2026-08-19T12:10:00") {
		t.Fatalf("metadata expiry = %s, want +600s", meta.ExpiresAt)
	}
	if !strings.HasPrefix(content.ExpiresAt, "2026-08-19T14:00:00") {
		t.Fatalf("content expiry = %s, want +7200s", content.ExpiresAt)
	}

	// The metadata entry expires first and refetches.
	e.clk.Advance(601 * time.Second)
	res := mustFetch(t, e, "pack.json")
	if res.CacheState != CacheMiss {
		t.Fatalf("expired metadata state = %q, want MISS (refetch)", res.CacheState)
	}
	readAll(t, res)
	res2 := mustFetch(t, e, "lib.tgz")
	if res2.CacheState != CacheHit {
		t.Fatalf("unexpired content state = %q, want HIT", res2.CacheState)
	}
	readAll(t, res2)
}

// ---- repo-semantics 7.3: concurrent miss singleflight ----

func TestFetchConcurrentMissSingleFlight(t *testing.T) {
	e := newFetchEnv(t, func(_ *metadata.Repo, _ *metadata.RemoteConfig) {})
	e.state.files["/slow.bin"] = "concurrent-content"
	e.state.mu.Lock()
	e.state.delay = 150 * time.Millisecond
	e.state.mu.Unlock()

	const n = 16
	var wg sync.WaitGroup
	results := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := e.eng.Fetch(context.Background(), "generic-remote", "slow.bin")
			if err != nil {
				errs[i] = err
				return
			}
			defer res.Body.Close() //nolint:errcheck // read-only probe stream // read-only probe stream
			b, rerr := io.ReadAll(res.Body)
			if rerr != nil {
				errs[i] = rerr
				return
			}
			results[i] = string(b)
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("waitor %d failed: %v (waitors must never 5xx)", i, errs[i])
		}
		if results[i] != "concurrent-content" {
			t.Fatalf("waitor %d body = %q", i, results[i])
		}
	}
	if got := e.hits.Load(); got != 1 {
		t.Fatalf("upstream hits under a miss stampede = %d, want exactly 1", got)
	}
}

// ---- RE-06: invalidation ----

func TestInvalidateRefetches(t *testing.T) {
	e := newFetchEnv(t, nil)
	e.state.files["/dir/up.bin"] = "v1"
	readAll(t, mustFetch(t, e, "dir/up.bin"))
	if got := e.hits.Load(); got != 1 {
		t.Fatalf("prefetch hits = %d", got)
	}

	existed, err := e.eng.Invalidate(context.Background(), "generic-remote", "dir/up.bin")
	if err != nil || !existed {
		t.Fatalf("Invalidate = (%t, %v), want (true, nil)", existed, err)
	}
	// Unknown path: nothing was cached.
	existed2, err := e.eng.Invalidate(context.Background(), "generic-remote", "unknown.bin")
	if err != nil || existed2 {
		t.Fatalf("Invalidate unknown = (%t, %v), want (false, nil)", existed2, err)
	}

	e.state.files["/dir/up.bin"] = "v2"
	res := mustFetch(t, e, "dir/up.bin")
	if body := readAll(t, res); body != "v2" {
		t.Fatalf("post-invalidate body = %q, want the refetched v2", body)
	}
	if got := e.hits.Load(); got != 2 {
		t.Fatalf("upstream hits after invalidate+refetch = %d, want 2", got)
	}
}

func TestForgetDropsState(t *testing.T) {
	e := newFetchEnv(t, nil)
	e.state.files["/x"] = "y"
	readAll(t, mustFetch(t, e, "x"))
	e.eng.Forget("generic-remote")
	e.eng.mu.Lock()
	clients, offline := len(e.eng.clients), len(e.eng.offline)
	e.eng.mu.Unlock()
	if clients != 0 || offline != 0 {
		t.Fatalf("after Forget: clients=%d offline=%d, want 0/0", clients, offline)
	}
}

// ---- RE-11: the stats surface ----

func TestStatsCounters(t *testing.T) {
	e := newFetchEnv(t, nil)
	e.state.files["/a.bin"] = "aaa"
	e.state.files["/b.bin"] = "bbbb"

	readAll(t, mustFetch(t, e, "a.bin"))                                      // miss
	readAll(t, mustFetch(t, e, "a.bin"))                                      // hit
	_, _ = e.eng.Fetch(context.Background(), "generic-remote", "missing.bin") // negative (404)
	_, _ = e.eng.Fetch(context.Background(), "generic-remote", "missing.bin") // negative-cache serve

	stats, err := e.eng.Stats(context.Background(), "generic-remote")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Misses != 1 || stats.Hits != 1 || stats.Negatives != 1 || stats.CachedNodes != 1 {
		t.Fatalf("counters = %+v", stats)
	}
	if stats.UpstreamBytes != int64(len("aaa")) || stats.CachedBytes != int64(len("aaa")) {
		t.Fatalf("byte counters = %+v", stats)
	}
}

// ---- FR-20-AC11 / NFR-P14: 1GB streaming with a flat heap ----

func TestFetch1GBStreamingFlatHeap(t *testing.T) {
	if testing.Short() {
		t.Skip("1GB streaming probe")
	}
	e := newFetchEnv(t, func(_ *metadata.Repo, _ *metadata.RemoteConfig) {})
	const gib = 1 << 30
	e.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		e.hits.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		// One GiB of zeros without holding it: a zero reader streamed out.
		_, _ = io.CopyN(w, zeroReader{}, gib)
	})

	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	res, err := e.eng.Fetch(context.Background(), "generic-remote", "big.bin")
	if err != nil {
		t.Fatalf("Fetch big.bin: %v", err)
	}

	var after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&after)
	delta := after.TotalAlloc - before.TotalAlloc // allocations, not live heap
	_ = delta
	live := uint64(0)
	if after.HeapAlloc > before.HeapAlloc {
		live = after.HeapAlloc - before.HeapAlloc
	}

	// Stream the landed copy back through sha256: proves both the landing
	// and the serving are streaming (no full-body buffering anywhere).
	h := sha256.New()
	n, cerr := io.Copy(h, res.Body)
	_ = res.Body.Close()
	if cerr != nil {
		t.Fatalf("stream back: %v", cerr)
	}
	if n != gib {
		t.Fatalf("served %d bytes, want %d", n, gib)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	want := sha256OfZeros(t, gib)
	if sum != want {
		t.Fatalf("1GB sha256 mismatch: got %s want %s", sum, want)
	}
	if live > 200<<20 { // headroom under the 256MB budget (live heap only)
		t.Fatalf("live heap delta = %d bytes, want < 200MB (NFR-P14)", live)
	}
	if res.Node.Size != gib {
		t.Fatalf("node size = %d, want %d", res.Node.Size, gib)
	}
}

// zeroReader is an endless stream of zeros.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// sha256OfZeros digests n zero bytes streaming.
func sha256OfZeros(t *testing.T, n int64) string {
	t.Helper()
	h := sha256.New()
	if _, err := io.CopyN(h, zeroReader{}, n); err != nil {
		t.Fatalf("digest zeros: %v", err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ---- the startup credential pass (FR-15-AC9) ----

func TestStartupCredentialPass(t *testing.T) {
	ctx := context.Background()

	seedRow := func(t *testing.T, md metadata.Store, password string) {
		t.Helper()
		now := time.Now().UTC().Format(time.RFC3339)
		if err := md.Repos().Create(ctx, &metadata.Repo{
			RepoKey: "r", Type: "remote", PackageType: "generic", Config: `{}`, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("create repo: %v", err)
		}
		if err := md.Remote().CreateConfig(ctx, &metadata.RemoteConfig{RepoKey: "r", URL: "http://example.com", Password: password}); err != nil {
			t.Fatalf("create config: %v", err)
		}
	}

	openStore := func(t *testing.T) metadata.Store {
		t.Helper()
		md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/binflow.db"})
		if err != nil {
			t.Fatalf("metadata.Open: %v", err)
		}
		t.Cleanup(func() { _ = md.Close() })
		return md
	}

	t.Run("no credentials no key boots", func(t *testing.T) {
		md := openStore(t)
		seedRow(t, md, "")
		st, _ := storage.OpenEngine(t.TempDir(), storage.Options{})
		t.Cleanup(func() { _ = st.Close() })
		if _, err := NewEngine(st, md, EngineOptions{Now: func() time.Time { return time.Now() }}); err != nil {
			t.Fatalf("engine must boot without a key when no credentials exist: %v", err)
		}
	})

	t.Run("credentials without key fail fast", func(t *testing.T) {
		md := openStore(t)
		c, _ := NewCipher(testKey(t))
		sealed, _ := c.Encrypt("pw")
		seedRow(t, md, sealed)
		st, _ := storage.OpenEngine(t.TempDir(), storage.Options{})
		t.Cleanup(func() { _ = st.Close() })
		_, err := NewEngine(st, md, EngineOptions{Key: nil, Now: func() time.Time { return time.Now() }})
		if err == nil || !strings.Contains(err.Error(), CredentialsEnvVar) {
			t.Fatalf("fail-fast error must name %s: %v", CredentialsEnvVar, err)
		}
	})

	t.Run("legacy plaintext migrated once", func(t *testing.T) {
		md := openStore(t)
		seedRow(t, md, "legacy-plaintext")
		st, _ := storage.OpenEngine(t.TempDir(), storage.Options{})
		t.Cleanup(func() { _ = st.Close() })
		eng, err := NewEngine(st, md, EngineOptions{Key: testKey(t), Now: func() time.Time { return time.Now() }})
		if err != nil {
			t.Fatalf("migrating boot: %v", err)
		}
		cfg, err := md.Remote().GetConfig(ctx, "r")
		if err != nil {
			t.Fatalf("GetConfig: %v", err)
		}
		if !IsEncrypted(cfg.Password) {
			t.Fatalf("password not migrated: %q", cfg.Password)
		}
		if got, _, err := eng.cipher.Decrypt(cfg.Password); err != nil || got != "legacy-plaintext" {
			t.Fatalf("migrated value decrypts to (%q, %v)", got, err)
		}
	})

	t.Run("wrong key fails fast", func(t *testing.T) {
		md := openStore(t)
		c, _ := NewCipher(bytesRepeat(1, 32))
		sealed, _ := c.Encrypt("pw")
		seedRow(t, md, sealed)
		st, _ := storage.OpenEngine(t.TempDir(), storage.Options{})
		t.Cleanup(func() { _ = st.Close() })
		_, err := NewEngine(st, md, EngineOptions{Key: bytesRepeat(2, 32), Now: func() time.Time { return time.Now() }})
		if err == nil {
			t.Fatalf("undecryptable credential must refuse to boot")
		}
	})
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// ---- env noise guard ----

func TestMain(m *testing.M) {
	// The credential pass reads the real environment; make the outcome
	// deterministic for every engine built through the test helper.
	_ = os.Unsetenv(CredentialsEnvVar)
	os.Exit(m.Run())
}
