package remote

// T-367 (FR-117): the two engine-side seams of the ticket — the absolute-URL
// fetch (FetchAbsolute: the helm _external dependency face's landing hop)
// and the chartsBaseUrl base override (the heterogeneous-base upstream:
// content-class hops go to the charts base, the repo-root index keeps the
// repository URL).

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// t367Env is one engine plus TWO loopback servers (the repository upstream
// and the divergent charts base / external dependency host) with
// independent request counters and an Authorization probe.
type t367Env struct {
	st  storage.Engine
	md  metadata.Store
	eng *Engine
	clk *fakeClock

	up       *httptest.Server
	upHits   *atomic.Int64
	upAuth   *atomic.Int64 // requests that carried an Authorization header
	base     *httptest.Server
	baseHits *atomic.Int64
	baseAuth *atomic.Int64
	files    map[string]string // served by BOTH servers (path-keyed)
}

// newT367Env assembles the two-server environment. Both servers serve the
// same files map and count Authorization-bearing requests, so a test can
// prove both WHERE a hop went and WHETHER credentials rode it.
func newT367Env(t *testing.T) *t367Env {
	t.Helper()
	ctx := context.Background()
	key := bytes.Repeat([]byte{11}, 32)
	t.Setenv(CredentialsEnvVar, base64.StdEncoding.EncodeToString(key))
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

	e := &t367Env{st: st, md: md, files: map[string]string{}}
	mkServer := func(hits, auth *atomic.Int64) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			if r.Header.Get("Authorization") != "" {
				auth.Add(1)
			}
			body, ok := e.files[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte(body))
		}))
	}
	e.upHits, e.upAuth = &atomic.Int64{}, &atomic.Int64{}
	e.baseHits, e.baseAuth = &atomic.Int64{}, &atomic.Int64{}
	e.up = mkServer(e.upHits, e.upAuth)
	e.base = mkServer(e.baseHits, e.baseAuth)
	t.Cleanup(e.up.Close)
	t.Cleanup(e.base.Close)

	e.clk = &fakeClock{now: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)}
	eng, err := NewEngine(st, md, EngineOptions{Now: e.clk.Now, Logger: silentLogger, Key: key})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	e.eng = eng
	return e
}

// createT367Remote writes one remote repository row (canonical config JSON
// spelling included) plus its remote_configs row.
func (e *t367Env) createT367Remote(t *testing.T, key, packageType, configTail string, creds bool) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	user := ""
	if creds {
		user = `"username":"jfrog","password":"x",`
	}
	row := &metadata.Repo{
		RepoKey: key, Type: "remote", PackageType: packageType,
		Config: `{"url":"` + e.up.URL + `",` + user +
			`"retrievalCachePeriodSecs":7200,"missedRetrievalCachePeriodSecs":1800,` +
			`"socketTimeoutSecs":15,"assumedOfflinePeriodSecs":300,"hardFail":false,` +
			`"allowPrivateUpstream":true,"priorityResolution":false` + configTail + `}`,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := e.md.Repos().Create(context.Background(), row); err != nil {
		t.Fatalf("create repo %s: %v", key, err)
	}
	if err := e.md.Remote().CreateConfig(context.Background(), &metadata.RemoteConfig{
		RepoKey: key, URL: e.up.URL,
		Username: func() string {
			if creds {
				return "jfrog"
			}
			return ""
		}(),
		Password:             encryptedPassword(t, "s3cret"),
		ContentTTLSeconds:    7200,
		MetadataTTLSeconds:   600,
		AllowPrivateUpstream: true,
	}); err != nil {
		t.Fatalf("create remote config %s: %v", key, err)
	}
}

// encryptedPassword seals one plaintext under the engine's own test key so
// the credential path is live (the startup pass would refuse a bare
// plaintext).
func encryptedPassword(t *testing.T, plain string) string {
	t.Helper()
	cipher, err := NewCipher(bytes.Repeat([]byte{11}, 32))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	enc, err := cipher.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	return enc
}

// registerT367HelmProvider enters the helm metadata provider shape this
// package's tests need (the REAL registration lives in the helm adapter,
// which cannot be imported here — the cycle through repo). The
// classification mirrors internal/adapter/helm/provider.go: the repo-root
// index is the regenerable document, everything else content.
var registerT367HelmProvider sync.Once

type t367HelmProvider struct{}

func (t367HelmProvider) Protocol() string { return "helm" }
func (t367HelmProvider) Classify(p string) adapter.MetadataKind {
	if p == "index.yaml" || strings.HasSuffix(p, "/index.yaml") {
		return adapter.KindMetadata
	}
	return adapter.KindContent
}
func (t367HelmProvider) PackageName(string) (string, bool)   { return "", false }
func (t367HelmProvider) Versions() adapter.VersionComparator { return nil }

func TestT367ChartsBaseSameHostOverride(t *testing.T) {
	registerT367HelmProvider.Do(func() { adapter.RegisterMetadata(t367HelmProvider{}) })
	e := newT367Env(t)
	e.files["/index.yaml"] = "apiVersion: v1\nentries: {}\n"
	e.files["/mirror/het-1.0.0.tgz"] = "chart-bytes"
	// Same host, different path: the charts live under /mirror on the
	// upstream host. The credentialed client is reused (same origin).
	base := e.up.URL + "/mirror"
	e.createT367Remote(t, "helm-r", "helm", fmt.Sprintf(`,"chartsBaseUrl":%q`, base), true)

	res, err := e.eng.Fetch(context.Background(), "helm-r", "het-1.0.0.tgz")
	if err != nil {
		t.Fatalf("Fetch chart: %v", err)
	}
	if body := readAllT367(t, res); body != "chart-bytes" {
		t.Fatalf("chart body = %q", body)
	}
	// The hop went to the BASE path, not the repository root: the base
	// server is the same host, so the counter rose once for /mirror/het-…
	// and the repository URL answered the index below.
	if got := e.upHits.Load(); got != 1 {
		t.Errorf("upstream hits = %d, want 1 (the charts hop only)", got)
	}
	// The index (metadata class) KEEPS the repository URL — never the base.
	if _, err = e.eng.Fetch(context.Background(), "helm-r", "index.yaml"); err != nil {
		t.Fatalf("Fetch index: %v", err)
	}
	if got := e.upHits.Load(); got != 2 {
		t.Errorf("upstream hits after index = %d, want 2", got)
	}
	if res, err = e.eng.Fetch(context.Background(), "helm-r", "het-1.0.0.tgz"); err != nil {
		t.Fatalf("second Fetch chart: %v", err)
	}
	_ = res.Body.Close()
	if got := e.upHits.Load(); got != 2 {
		t.Errorf("upstream hits after the cached re-read = %d, want 2 (HIT, zero egress)", got)
	}
}

func TestT367ChartsBaseCrossHostDropsCredentials(t *testing.T) {
	registerT367HelmProvider.Do(func() { adapter.RegisterMetadata(t367HelmProvider{}) })
	e := newT367Env(t)
	e.files["/index.yaml"] = "apiVersion: v1\n"
	e.files["/charts/het-1.0.0.tgz"] = "chart-bytes"
	e.createT367Remote(t, "helm-r", "helm", fmt.Sprintf(`,"chartsBaseUrl":%q`, e.base.URL+"/charts"), true)

	if _, err := e.eng.Fetch(context.Background(), "helm-r", "het-1.0.0.tgz"); err != nil {
		t.Fatalf("Fetch chart: %v", err)
	}
	if got := e.baseHits.Load(); got != 1 {
		t.Fatalf("charts-base hits = %d, want 1", got)
	}
	if got := e.upHits.Load(); got != 0 {
		t.Errorf("repository-upstream hits = %d, want 0 (the base served the chart)", got)
	}
	// The credential NEVER rode to the charts host…
	if got := e.baseAuth.Load(); got != 0 {
		t.Errorf("charts-base requests carrying Authorization = %d, want 0", got)
	}
	// …while the repository's own hop (the index) still presents it.
	if _, err := e.eng.Fetch(context.Background(), "helm-r", "index.yaml"); err != nil {
		t.Fatalf("Fetch index: %v", err)
	}
	if got := e.upAuth.Load(); got != 1 {
		t.Errorf("repository-upstream authenticated requests = %d, want 1", got)
	}
}

func TestT367ChartsBaseNonHelmIgnored(t *testing.T) {
	// A hand-mangled row: the config layer refuses chartsBaseUrl on non-helm
	// remotes, so the engine's package-type gate is the defense in depth —
	// the field must not redirect a generic repository's fetches.
	e := newT367Env(t)
	e.files["/a-1.0.0.jar"] = "jar-bytes"
	e.createT367Remote(t, "maven-r", "maven", fmt.Sprintf(`,"chartsBaseUrl":%q`, e.base.URL), false)

	if _, err := e.eng.Fetch(context.Background(), "maven-r", "a-1.0.0.jar"); err != nil {
		t.Fatalf("Fetch jar: %v", err)
	}
	if got := e.baseHits.Load(); got != 0 {
		t.Errorf("charts-base hits = %d, want 0 (the gate held)", got)
	}
	if got := e.upHits.Load(); got != 1 {
		t.Errorf("repository-upstream hits = %d, want 1", got)
	}
}

func TestT367FetchAbsoluteLandsAndCaches(t *testing.T) {
	e := newT367Env(t)
	dep := "dependency-chart-bytes"
	e.createT367Remote(t, "helm-r", "helm", "", true)

	// Serve the dependency on the SECOND server (the third-party host).
	depHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.baseHits.Add(1)
		if r.Header.Get("Authorization") != "" {
			e.baseAuth.Add(1)
		}
		if r.URL.Path == "/deps/dep-1.0.0.tgz" {
			_, _ = io.WriteString(w, dep)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(depHost.Close)

	path := "_external/http/" + strings.TrimPrefix(depHost.URL, "http://") + "/deps/dep-1.0.0.tgz"
	res, err := e.eng.FetchAbsolute(context.Background(), "helm-r", path, depHost.URL+"/deps/dep-1.0.0.tgz")
	if err != nil {
		t.Fatalf("FetchAbsolute: %v", err)
	}
	if res.CacheState != CacheMiss || res.Node == nil || res.Node.Path != path {
		t.Fatalf("first FetchAbsolute = state %q, node %v, want MISS at the folded path", res.CacheState, res.Node)
	}
	if body := readAllT367(t, res); body != dep {
		t.Fatalf("dependency body = %q", body)
	}
	// The landing is a real node: the row stands in the repository's own
	// namespace at the folded path.
	if n, nerr := e.md.Nodes().Get(context.Background(), "helm-r", path); nerr != nil || n == nil || n.Sha256 != res.Node.Sha256 {
		t.Fatalf("landed node row = (%v, %v), want the folded-path copy", n, nerr)
	}
	// Credentials never ride the face.
	if got := e.baseAuth.Load(); got != 0 {
		t.Errorf("third-party requests carrying Authorization = %d, want 0", got)
	}
	// The second pull is a local HIT: zero further egress.
	if res, err = e.eng.FetchAbsolute(context.Background(), "helm-r", path, depHost.URL+"/deps/dep-1.0.0.tgz"); err != nil || res.CacheState != CacheHit {
		t.Fatalf("second FetchAbsolute = (%v, state %q), want HIT", err, cacheStateOf(res))
	}
	_ = res.Body.Close()
	if got := e.baseHits.Load(); got != 1 {
		t.Errorf("third-party hits = %d, want 1 (cached)", got)
	}
}

func TestT367FetchAbsoluteNegativeCache(t *testing.T) {
	e := newT367Env(t)
	e.createT367Remote(t, "helm-r", "helm", "", false)
	const path = "_external/http/dep.example.com/absent-1.0.0.tgz"
	target := e.up.URL + "/absent-1.0.0.tgz"

	res, err := e.eng.FetchAbsolute(context.Background(), "helm-r", path, target)
	fe := fetchErrT367(t, res, err)
	if fe.Status != http.StatusNotFound || !fe.Unfound {
		t.Fatalf("absent dependency = (%d, unfound %t), want the 404 family", fe.Status, fe.Unfound)
	}
	// The negative window: the second call answers unfound with ZERO
	// further third-party packets.
	res, err = e.eng.FetchAbsolute(context.Background(), "helm-r", path, target)
	fe = fetchErrT367(t, res, err)
	if fe.Status != http.StatusNotFound {
		t.Fatalf("negative-window dependency = %d, want 404", fe.Status)
	}
	if got := e.upHits.Load(); got != 1 {
		t.Errorf("third-party hits = %d, want 1 (negative cache)", got)
	}
}

func TestT367FetchAbsoluteFaultsNeverMarkOffline(t *testing.T) {
	e := newT367Env(t)
	e.files["/local-copy.tgz"] = "repo-chart"
	e.createT367Remote(t, "helm-r", "helm", "", false)

	// The third-party target is a dead port (connection refused — the
	// transport-fault family that would otherwise mark the repository
	// assumed offline).
	const (
		path   = "_external/http/dead.example.com/x-1.0.0.tgz"
		deadTg = "http://127.0.0.1:1/x-1.0.0.tgz"
	)
	if _, err := e.eng.FetchAbsolute(context.Background(), "helm-r", path, deadTg); err == nil {
		t.Fatal("dead external target must fail")
	}
	// The repository's OWN upstream was never implicated: a normal fetch on
	// the same repository still contacts upstream and succeeds (no assumed
	// offline window was opened by the third-party fault).
	res, err := e.eng.Fetch(context.Background(), "helm-r", "local-copy.tgz")
	if err != nil {
		t.Fatalf("Fetch after the external fault: %v (the offline window must not have opened)", err)
	}
	if body := readAllT367(t, res); body != "repo-chart" {
		t.Fatalf("repo chart body = %q", body)
	}
}

func TestT367FetchAbsoluteStaleServesExpiredCopy(t *testing.T) {
	e := newT367Env(t)
	e.createT367Remote(t, "helm-r", "helm", "", false)
	phase := &atomic.Int64{} // 0 = serve, 1 = 500
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if phase.Load() == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, "dep-v1")
	}))
	t.Cleanup(srv.Close)
	const path = "_external/http/stale.example.com/dep-1.0.0.tgz"

	res, err := e.eng.FetchAbsolute(context.Background(), "helm-r", path, srv.URL+"/dep-1.0.0.tgz")
	if err != nil {
		t.Fatalf("FetchAbsolute: %v", err)
	}
	_ = readAllT367(t, res)
	e.clk.Advance(3 * time.Hour) // past the content TTL
	phase.Store(1)

	res, err = e.eng.FetchAbsolute(context.Background(), "helm-r", path, srv.URL+"/dep-1.0.0.tgz")
	if err != nil {
		t.Fatalf("FetchAbsolute during the fault: %v", err)
	}
	if res.CacheState != CacheStale || res.UpstreamError == "" {
		t.Fatalf("faulted re-fetch = state %q, upstreamError %q, want STALE with the marker", res.CacheState, res.UpstreamError)
	}
	if body := readAllT367(t, res); body != "dep-v1" {
		t.Fatalf("stale body = %q, want the expired copy", body)
	}
}

func TestT367FetchAbsoluteRejectsBadTargets(t *testing.T) {
	e := newT367Env(t)
	e.createT367Remote(t, "helm-r", "helm", "", false)
	for _, target := range []string{"", "ftp://example.com/x.tgz", "/relative/path.tgz", "http://"} {
		if _, err := e.eng.FetchAbsolute(context.Background(), "helm-r", "_external/http/x", target); err == nil {
			t.Errorf("FetchAbsolute(%q) must refuse, got nil", target)
		}
	}
}

// readAllT367 drains one result body.
func readAllT367(t *testing.T, res *FetchResult) string {
	t.Helper()
	defer res.Body.Close() //nolint:errcheck // read-only probe stream
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// fetchErrT367 asserts a FetchError and returns it.
func fetchErrT367(t *testing.T, res *FetchResult, err error) *FetchError {
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
