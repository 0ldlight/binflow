package httpapi_test

// T-201 regression tests for the S3 defect package A (T-173 D-3/D-1):
//
//   - D-3  POST /api/v1/storage/migration/start must hand the migration a
//     context DETACHED from the request — net/http cancels r.Context() the
//     moment the handler returns, which killed every REST-triggered
//     migration at "list disk blobs" before the fix.
//   - D-1  /api/v1/storage/stats must size an engine-backed instance
//     through Deps.BlobInventory instead of walking a data-dir blobs/ tree
//     that does not exist under backend=s3; the same seam feeds the
//     /metrics storage-byte gauge and the GC candidate sizing.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
	"github.com/lzwzzy/binflow/internal/storage"
)

// fakeMigrationStart captures the context StartMigration received and
// reports, asynchronously, whether it got canceled — the D-3 observable.
type fakeMigrationStart struct {
	started chan struct{}
	outcome chan string // "canceled" or "alive"
}

func newFakeMigrationStart() *fakeMigrationStart {
	return &fakeMigrationStart{
		started: make(chan struct{}),
		outcome: make(chan string, 1),
	}
}

func (f *fakeMigrationStart) StartMigration(ctx context.Context) error {
	close(f.started)
	go func() {
		select {
		case <-ctx.Done():
			f.outcome <- "canceled"
		case <-time.After(300 * time.Millisecond):
			f.outcome <- "alive"
		}
	}()
	return nil
}

func (f *fakeMigrationStart) StatusView() *storage.MigrationStatusView {
	return &storage.MigrationStatusView{}
}

// errMigrationStart refuses every start (the 409 arm of the route).
type errMigrationStart struct{}

func (errMigrationStart) StartMigration(context.Context) error {
	return errors.New("storage: migration: migration is not enabled")
}
func (errMigrationStart) StatusView() *storage.MigrationStatusView {
	return &storage.MigrationStatusView{}
}

// newT201Server builds the minimal admin-capable stack the routes under
// test need: real auth (admin/password, the documented seed), real sqlite
// metadata, and whatever extra Deps the test injects.
func newT201Server(t *testing.T, build func(*httpapi.Deps, *config.Config)) *httptest.Server {
	t.Helper()
	md, err := metadata.Open(context.Background(), metadata.Options{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "t201.db"),
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	authSvc := auth.NewFromStore(md, false)
	deps := httpapi.Deps{
		Config:   cfg,
		Auth:     authSvc,
		Authz:    authSvc,
		Metadata: md,
		Repos:    md.Repos(),
		DataDir:  cfg.Storage.DataDir,
	}
	if build != nil {
		build(&deps, cfg)
	}
	s := httpapi.New(deps, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// doAdmin issues an admin-authenticated request against ts.
func doAdmin(t *testing.T, ts *httptest.Server, method, path string, body string) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, rdr)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	req.SetBasicAuth(adminUser, adminPass)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// TestMigrationStartSurvivesRequestReturn is the D-3 regression: the 202
// leaves the wire, the request context dies with the connection, and the
// migration context handed to the engine must NOT. The fake starter holds
// the captured context for 300ms — with the pre-fix r.Context() the server
// cancels it within milliseconds of the response and the probe observes
// "canceled"; the fixed WithoutCancel context keeps it "alive".
func TestMigrationStartSurvivesRequestReturn(t *testing.T) {
	starter := newFakeMigrationStart()
	ts := newT201Server(t, func(d *httpapi.Deps, _ *config.Config) {
		d.Migration = starter
	})

	resp := doAdmin(t, ts, http.MethodPost, "/binflow/api/v1/storage/migration/start", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST migration/start status = %d, want 202 (body: %s)", resp.StatusCode, body)
	}
	// The response is fully received and the handler has returned; from
	// here on the request context's lifespan is over.
	select {
	case <-starter.started:
	case <-time.After(2 * time.Second):
		t.Fatal("StartMigration was never called")
	}
	select {
	case got := <-starter.outcome:
		if got != "alive" {
			t.Fatalf("migration context outcome = %q, want \"alive\" — the request's death canceled the background migration (T-173 D-3)", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("migration context probe never resolved")
	}
}

// TestMigrationStartRefusalStillMapsTo409 keeps the error arm pinned: a
// starter that refuses (migration not enabled) answers 409 with the
// engine's own message, unchanged by the D-3 fix.
func TestMigrationStartRefusalStillMapsTo409(t *testing.T) {
	ts := newT201Server(t, func(d *httpapi.Deps, _ *config.Config) {
		d.Migration = errMigrationStart{}
	})
	resp := doAdmin(t, ts, http.MethodPost, "/binflow/api/v1/storage/migration/start", "")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body: %s)", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "migration is not enabled") {
		t.Fatalf("body %q, want the engine's refusal message", body)
	}
}

// fakeInventory is the Deps.BlobInventory stub: a fixed sha→stat table and
// an optional failure.
type fakeInventory struct {
	stats map[string]storage.BlobStat
	err   error
}

func (f *fakeInventory) BlobStats(context.Context) (map[string]storage.BlobStat, error) {
	return f.stats, f.err
}

// fakeGCEngine answers one fixed candidate list for every pass.
type fakeGCEngine struct {
	candidates []string
}

func (f *fakeGCEngine) GC(_ context.Context, _ func() (map[string]struct{}, error), _ time.Duration, _ bool) ([]string, error) {
	return f.candidates, nil
}

// TestStorageStatsS3Branch (D-1): with the inventory seam wired, the stats
// endpoint reports the bucket's bytes as physical_bytes and never touches
// the (nonexistent) data-dir blobs tree — the pre-fix behavior was a
// permanent 500 under backend=s3. The table also pins the error arm and
// the disk regression (a seam-less stack with no blobs/ tree keeps the M1
// walk and its error shape).
func TestStorageStatsS3Branch(t *testing.T) {
	big := strings.Repeat("x", 4096)

	cases := []struct {
		name       string
		build      func(*httpapi.Deps, *config.Config)
		wantStatus int
		wantBody   string
		notBody    string
	}{
		{
			name: "s3 inventory sizes the bucket",
			build: func(d *httpapi.Deps, cfg *config.Config) {
				cfg.Storage.Backend = config.StorageBackendS3
				d.BlobInventory = &fakeInventory{stats: map[string]storage.BlobStat{
					sha256Hex([]byte(big)):               {Size: 4096, LastModified: time.Now()},
					sha256Hex([]byte("small")):           {Size: 5, LastModified: time.Now()},
					sha256Hex([]byte("not-a-blob-key-")): {Size: 100, LastModified: time.Now()},
				}}
			},
			wantStatus: http.StatusOK,
			wantBody:   `"physical_bytes": 4201`,
			notBody:    "size of blobs dir",
		},
		{
			name: "s3 inventory failure surfaces as 500",
			build: func(d *httpapi.Deps, cfg *config.Config) {
				cfg.Storage.Backend = config.StorageBackendS3
				d.BlobInventory = &fakeInventory{err: errors.New("list objects: bucket gone")}
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   "size blobs through the storage engine",
			notBody:    "size of blobs dir",
		},
		{
			name: "seam-less stack keeps the disk walk",
			build: func(*httpapi.Deps, *config.Config) {
				// No seam and no blobs/ tree in the synthetic data dir:
				// the M1 walk's own error shape, proving the branch is
				// keyed on the seam and nothing else.
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   "size of blobs dir",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := newT201Server(t, tc.build)
			resp := doAdmin(t, ts, http.MethodGet, "/binflow/api/v1/storage/stats", "")
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", resp.StatusCode, tc.wantStatus, body)
			}
			if !strings.Contains(string(body), tc.wantBody) {
				t.Errorf("body %q, want it to contain %q", body, tc.wantBody)
			}
			if tc.notBody != "" && strings.Contains(string(body), tc.notBody) {
				t.Errorf("body %q must not contain %q", body, tc.notBody)
			}
		})
	}
}

// TestGCSizingThroughInventory (D-1, GC leg): with the seam wired,
// candidateBytes comes from the engine inventory — the pre-fix disk walk
// could only log "gc candidate sizing incomplete" under S3. The fake GC
// reports one candidate the inventory sizes at 4096.
func TestGCSizingThroughInventory(t *testing.T) {
	sha := sha256Hex([]byte("gc-candidate-body"))
	ts := newT201Server(t, func(d *httpapi.Deps, cfg *config.Config) {
		cfg.Storage.Backend = config.StorageBackendS3
		d.BlobInventory = &fakeInventory{stats: map[string]storage.BlobStat{
			sha: {Size: 4096, LastModified: time.Now().Add(-48 * time.Hour)},
		}}
		d.GC = &fakeGCEngine{candidates: []string{sha}}
	})
	resp := doAdmin(t, ts, http.MethodPost, "/binflow/api/v1/system/gc", `{}`)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST gc status = %d, want 200 (body: %s)", resp.StatusCode, body)
	}
	var got struct {
		CandidateCount int   `json:"candidateCount"`
		CandidateBytes int64 `json:"candidateBytes"`
		DeletedCount   int   `json:"deletedCount"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode gc response %q: %v", body, err)
	}
	if got.CandidateCount != 1 || got.CandidateBytes != 4096 {
		t.Fatalf("gc response = %+v, want candidateCount=1 candidateBytes=4096 (sized through the engine inventory)", got)
	}
}

// TestMetricsStorageBytesThroughInventory (D-1, /metrics leg): the
// storage-byte gauge on an s3-labeled, seam-wired stack carries the
// bucket's physical bytes (the pre-fix approximation summed repo_usage
// logical bytes instead; an empty instance reported 0 either way, so the
// assertion uses a nonzero inventory).
func TestMetricsStorageBytesThroughInventory(t *testing.T) {
	ts := newT201Server(t, func(d *httpapi.Deps, cfg *config.Config) {
		cfg.Storage.Backend = config.StorageBackendS3
		d.BlobInventory = &fakeInventory{stats: map[string]storage.BlobStat{
			sha256Hex([]byte("metrics-a")): {Size: 300, LastModified: time.Now()},
			sha256Hex([]byte("metrics-b")): {Size: 200, LastModified: time.Now()},
		}}
		d.Metrics = metrics.NewRegistry()
	})
	resp := doAdmin(t, ts, http.MethodGet, "/metrics", "")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics status = %d, want 200 (body: %s)", resp.StatusCode, body)
	}
	if want := `binflow_storage_blob_bytes{engine="s3"} 500`; !strings.Contains(string(body), want) {
		t.Errorf("body missing %q (physical bucket bytes through the inventory seam)\nbody:\n%s", want, body)
	}
}
