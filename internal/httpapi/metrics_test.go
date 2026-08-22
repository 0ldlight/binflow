// /metrics endpoint integration tests (T-163 AC 2/AC 3): anonymous posture,
// the require_auth gate's two states, the four metric families' exposition,
// scrape-time snapshots, HTTP middleware counting and the replication
// presence rule (FR-61-AC4). The pure path-normalization table lives in
// metrics_internal_test.go (unexported function).

package httpapi_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/console"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
	"github.com/lzwzzy/binflow/internal/replication"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// metricsHarness is a real-stack harness (the newHarnessCfg shape) plus the
// two T-163 Deps entries the shared harness predates: the metrics registry
// and, optionally, a replication store.
type metricsHarness struct {
	t       *testing.T
	url     string
	md      metadata.Store
	dataDir string
}

// get issues a GET (with optional Basic credentials) and returns status,
// headers and body.
func (h *metricsHarness) get(path, user, pass string) (int, http.Header, string) {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.url+path, nil)
	if err != nil {
		h.t.Fatalf("build request %s: %v", path, err)
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("read body %s: %v", path, err)
	}
	return resp.StatusCode, resp.Header, string(body)
}

// newMetricsHarness assembles the stack. requireAuth flips
// metrics.require_auth; repl (nil allowed) is the replication store seam;
// withMetrics=false leaves Deps.Metrics unwired (the pre-M6 posture).
func newMetricsHarness(t *testing.T, requireAuth bool, repl replication.Store, withMetrics bool) *metricsHarness {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()

	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	cfg.Metrics.RequireAuth = requireAuth

	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	svc := repo.New(st, md, authSvc, audit.New(md, true))
	genericHandler := generic.New(svc, md.Blobs())

	var reg *metrics.Registry
	if withMetrics {
		reg = metrics.NewRegistry()
	}
	s := httpapi.New(httpapi.Deps{
		Config:      cfg,
		Auth:        authSvc,
		Authz:       authSvc,
		Metadata:    md,
		Repos:       md.Repos(),
		ReposSvc:    svc,
		Passwords:   authSvc,
		Tokens:      authSvc,
		GC:          st,
		DataDir:     dataDir,
		Console:     console.Handler(),
		Adapters:    []adapter.Handler{genericHandler},
		Metrics:     reg,
		Replication: repl,
	}, nil)

	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &metricsHarness{t: t, url: ts.URL, md: md, dataDir: dataDir}
}

// fakeReplicationStore stubs replication.Store for the presence/status
// assertions (only ListConfigs/Status carry data).
type fakeReplicationStore struct {
	configs []*replication.ReplicationConfig
	status  map[int64]*replication.ConfigStatus
}

func (f *fakeReplicationStore) CreateConfig(context.Context, *replication.ReplicationConfig) (int64, error) {
	return 0, nil
}
func (f *fakeReplicationStore) GetConfig(context.Context, int64) (*replication.ReplicationConfig, error) {
	return nil, replication.ErrConfigNotFound
}
func (f *fakeReplicationStore) UpdateConfig(context.Context, *replication.ReplicationConfig) error {
	return nil
}
func (f *fakeReplicationStore) DeleteConfig(context.Context, int64) error { return nil }
func (f *fakeReplicationStore) ListConfigs(context.Context) ([]*replication.ReplicationConfig, error) {
	return f.configs, nil
}
func (f *fakeReplicationStore) CreateTask(context.Context, *replication.ReplicationTask) (int64, error) {
	return 0, nil
}
func (f *fakeReplicationStore) GetTask(context.Context, int64) (*replication.ReplicationTask, error) {
	return nil, replication.ErrTaskNotFound
}
func (f *fakeReplicationStore) UpdateTask(context.Context, *replication.ReplicationTask) error {
	return nil
}
func (f *fakeReplicationStore) DeleteTask(context.Context, int64) error { return nil }
func (f *fakeReplicationStore) ListTasks(context.Context, int64, int) ([]*replication.ReplicationTask, error) {
	return nil, nil
}
func (f *fakeReplicationStore) Status(_ context.Context, id int64) (*replication.ConfigStatus, error) {
	if st, ok := f.status[id]; ok {
		return st, nil
	}
	return &replication.ConfigStatus{}, nil
}

// TestMetricsEndpointAnonymous (FR-61-AC1): default posture is anonymous,
// 200, the Prometheus 0.0.4 content type, and the four families' TYPE/HELP
// lines — with the replication family ABSENT while its Deps is unwired
// (FR-61-AC4's "not exposed at zero" leg).
func TestMetricsEndpointAnonymous(t *testing.T) {
	h := newMetricsHarness(t, false, nil, true)

	code, hdr, body := h.get("/metrics", "", "")
	if code != http.StatusOK {
		t.Fatalf("GET /metrics status = %d, want 200", code)
	}
	if ct := hdr.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Fatalf("Content-Type = %q, want text/plain; version=0.0.4...", ct)
	}
	for _, want := range []string{
		"# HELP binflow_http_requests_total ",
		"# TYPE binflow_http_requests_total counter",
		"# TYPE binflow_http_request_duration_seconds histogram",
		"# HELP binflow_http_requests_in_flight ",
		`binflow_storage_blobs{engine="disk"} 0`,
		`binflow_storage_blob_bytes{engine="disk"} 0`,
		`binflow_auth_logins_total{source="local"} 0`,
		`binflow_auth_logins_total{source="oidc"} 0`,
		`binflow_auth_logins_total{source="ldap"} 0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
	if strings.Contains(body, "binflow_replication_tasks") {
		t.Error("body contains binflow_replication_tasks, want the family unexposed while replication is not wired")
	}
}

// TestMetricsRequireAuth (FR-61-AC3): metrics.require_auth=true flips the
// endpoint to credential-gated — anonymous 401 with the Basic challenge,
// any authenticated principal 200.
func TestMetricsRequireAuth(t *testing.T) {
	h := newMetricsHarness(t, true, nil, true)

	code, hdr, _ := h.get("/metrics", "", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /metrics status = %d, want 401", code)
	}
	if wa := hdr.Get("WWW-Authenticate"); !strings.Contains(wa, "Basic") {
		t.Fatalf("WWW-Authenticate = %q, want a Basic challenge", wa)
	}

	code, _, body := h.get("/metrics", adminUser, adminPass)
	if code != http.StatusOK {
		t.Fatalf("authenticated GET /metrics status = %d, want 200", code)
	}
	if !strings.Contains(body, "binflow_http_requests_total") {
		t.Error("authenticated body missing binflow_http_requests_total")
	}
}

// TestMetricsNotConfigured: a stack assembled without Deps.Metrics keeps the
// route but answers 503 honestly (never an empty exposition, never a panic).
func TestMetricsNotConfigured(t *testing.T) {
	h := newMetricsHarness(t, false, nil, false)
	code, _, _ := h.get("/metrics", "", "")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("GET /metrics on a metrics-less stack = %d, want 503", code)
	}
}

// TestMetricsMethodNotAllowed: the scrape endpoint is GET/HEAD only.
func TestMetricsMethodNotAllowed(t *testing.T) {
	h := newMetricsHarness(t, false, nil, true)
	req, err := http.NewRequest(http.MethodPost, h.url+"/metrics", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /metrics = %d, want 405", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); !strings.Contains(allow, "GET") {
		t.Fatalf("Allow = %q, want it to name GET", allow)
	}
}

// TestMetricsHTTPCounting: the middleware counts by method, normalized
// route and status; the duration histogram accumulates observations. Two
// scrapes: the counting runs after the inner handler renders, so the first
// scrape's own request only shows up in the second one — and the in-flight
// gauge self-observes as 1 while the scrape is being served.
func TestMetricsHTTPCounting(t *testing.T) {
	h := newMetricsHarness(t, false, nil, true)
	h.get("/binflow/api/system/ping", "", "") // 200
	h.get("/binflow/api/system/nope", "", "") // 404 (envelope)
	h.get("/metrics", "", "")                 // warm the self-series

	_, _, body := h.get("/metrics", "", "")
	for _, want := range []string{
		`binflow_http_requests_total{method="GET",path="/binflow/api/system/ping",status="200"} 1`,
		`binflow_http_requests_total{method="GET",path="/binflow/api/system/nope",status="404"} 1`,
		`binflow_http_requests_total{method="GET",path="/metrics",status="200"} 1`,
		"binflow_http_requests_in_flight 1",
		`binflow_http_request_duration_seconds_count{method="GET",path="/binflow/api/system/ping"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\nbody:\n%s", want, body)
		}
	}
}

// TestMetricsPathCardinality (FR-61-AC2 leg): many distinct raw paths
// collapse to a bounded set of normalized route templates.
func TestMetricsPathCardinality(t *testing.T) {
	h := newMetricsHarness(t, false, nil, true)
	for _, p := range []string{
		"/binflow/libs/a/b/c.jar", "/binflow/libs/x/y/z.tgz",
		"/binflow/api/storage/libs/a/b/c.jar", "/binflow/api/storage/other/z.bin",
		"/binflow/api/repositories/libs", "/binflow/api/repositories/other",
		"/binflow/api/security/users/alice", "/binflow/api/security/users/bob",
		"/healthz", "/readyz", "/metrics",
	} {
		h.get(p, "", "")
	}
	_, _, body := h.get("/metrics", "", "")
	paths := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "binflow_http_requests_total{") {
			continue
		}
		if i := strings.Index(line, `path="`); i >= 0 {
			rest := line[i+len(`path="`):]
			if end := strings.IndexByte(rest, '"'); end >= 0 {
				paths[rest[:end]] = true
			}
		}
	}
	// 11 raw paths above collapse to 7 templates (+ /metrics itself = 8).
	if len(paths) > 9 {
		t.Fatalf("normalized path label cardinality = %d (values %v), want <= 9", len(paths), paths)
	}
}

// TestMetricsLoginCounter: a successful console login counts by source
// (local); a failed login counts nothing.
func TestMetricsLoginCounter(t *testing.T) {
	h := newMetricsHarness(t, false, nil, true)
	postLogin(t, h.url, adminUser, adminPass, http.StatusOK)
	postLogin(t, h.url, adminUser, "wrong", http.StatusUnauthorized)

	_, _, body := h.get("/metrics", "", "")
	if want := `binflow_auth_logins_total{source="local"} 1`; !strings.Contains(body, want) {
		t.Errorf("body missing %q (failed logins must not count)\nbody:\n%s", want, body)
	}
}

// TestMetricsStorageSnapshot: the scrape pulls the blob count from the
// ledger and the physical bytes from blobs/ (a landed blob moves both).
func TestMetricsStorageSnapshot(t *testing.T) {
	h := newMetricsHarness(t, false, nil, true)
	landTestBlob(t, h.md, h.dataDir)

	_, _, body := h.get("/metrics", "", "")
	for _, want := range []string{
		`binflow_storage_blobs{engine="disk"} 1`,
		`binflow_storage_blob_bytes{engine="disk"} 11`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\nbody:\n%s", want, body)
		}
	}
}

// TestMetricsNamingConvention (T-197 / D5): `_total` is reserved for counters
// — every gauge/histogram family in the exposition must drop the suffix, and
// the pre-rename storage names must be gone (the scrape surface changed, no
// alias is kept). This pins the convention end-to-end even though the
// registry also rejects such declarations at registration time.
func TestMetricsNamingConvention(t *testing.T) {
	h := newMetricsHarness(t, false, nil, true)
	_, _, body := h.get("/metrics", "", "")

	gaugesWithTotal := []string{}
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 || fields[0] != "#" || fields[1] != "TYPE" {
			continue
		}
		name, typ := fields[2], fields[3]
		switch typ {
		case "counter":
			if !strings.HasSuffix(name, "_total") {
				t.Errorf("counter family %q lacks the _total suffix", name)
			}
		case "gauge", "histogram":
			if strings.HasSuffix(name, "_total") {
				gaugesWithTotal = append(gaugesWithTotal, name)
			}
		}
	}
	if len(gaugesWithTotal) > 0 {
		t.Errorf("non-counter families carrying _total: %v", gaugesWithTotal)
	}
	for _, stale := range []string{"binflow_storage_blobs_total", "binflow_storage_blob_bytes_total"} {
		if strings.Contains(body, stale) {
			t.Errorf("body still exposes the pre-T-197 gauge name %q", stale)
		}
	}
}

// TestMetricsReplicationSnapshot (FR-61-AC4's enabled leg): a wired store
// exposes the task gauge with per-status values summed across configs.
func TestMetricsReplicationSnapshot(t *testing.T) {
	repl := &fakeReplicationStore{
		configs: []*replication.ReplicationConfig{
			{ID: 1, Name: "push-a", SourceRepo: "libs-release"},
			{ID: 2, Name: "push-b", SourceRepo: "libs-release"},
		},
		status: map[int64]*replication.ConfigStatus{
			1: {Pending: 2, Succeeded: 5, Failed: 1},
			2: {Succeeded: 3, Skipped: 4},
		},
	}
	h := newMetricsHarness(t, false, repl, true)

	_, _, body := h.get("/metrics", "", "")
	for _, want := range []string{
		"# TYPE binflow_replication_tasks gauge",
		`binflow_replication_tasks{status="pending"} 2`,
		`binflow_replication_tasks{status="in_progress"} 0`,
		`binflow_replication_tasks{status="success"} 8`,
		`binflow_replication_tasks{status="failed"} 1`,
		`binflow_replication_tasks{status="skipped"} 4`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\nbody:\n%s", want, body)
		}
	}
}

// postLogin issues the console login request.
func postLogin(t *testing.T, base, user, pass string, wantStatus int) {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, user, pass)
	resp, err := http.Post(base+"/binflow/api/v1/session", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != wantStatus {
		t.Fatalf("POST session status = %d, want %d", resp.StatusCode, wantStatus)
	}
}

// landTestBlob writes one physical blob plus its ledger row (the two halves
// of a landed artifact, sized 11 bytes).
func landTestBlob(t *testing.T, md metadata.Store, dataDir string) {
	t.Helper()
	sha := strings.Repeat("ab", 32) // 64 hex chars
	path, err := storage.BlobPath(dataDir, sha)
	if err != nil {
		t.Fatalf("blob path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir blob shard: %v", err)
	}
	if err := os.WriteFile(path, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("write blob: %v", err)
	}
	if err := md.Blobs().Put(context.Background(), &metadata.Blob{
		Sha256: sha, Size: 11, CreatedAt: "2026-08-22T00:00:00Z",
	}); err != nil {
		t.Fatalf("put blob row: %v", err)
	}
}
