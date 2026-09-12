package httpapi_test

// The /api/system/storage admin plane's behavior tests (LOOP 003 /
// prune-gc-admin.md): the PUD lifecycle (202/412 arms, stop marker,
// persisted 26-field report across restarts), the dry-run polarity (D6 —
// the request's default DELETES), the gc dot stream with its 409
// concurrency arm, and the wire forms of optimize/compress/backup/size/
// info/exportds. Async arms ride a gated prune wrapper — the controlled
// job frame the 202 plane needs (park the pass, assert mid-flight, then
// release).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// gatingPruner parks every Prune call until released — the injected wait
// the async tests control job timing with. firstDir signals once the walk
// has settled its first directory (mid-walk stop tests wait on it).
// Everything else delegates to the real engine.
type gatingPruner struct {
	storage.Engine
	entered  chan struct{}
	release  chan struct{}
	firstDir chan struct{}
	once     sync.Once
	dirOnce  sync.Once
}

func (g *gatingPruner) Prune(ctx context.Context, opts storage.PruneOptions) (*storage.PruneOutcome, error) {
	g.once.Do(func() { close(g.entered) })
	<-g.release
	inner := opts.Observe
	opts.Observe = func(s storage.PruneDirStats) {
		if inner != nil {
			inner(s)
		}
		g.dirOnce.Do(func() { close(g.firstDir) })
	}
	return g.Engine.(storage.Pruner).Prune(ctx, opts)
}

// gatingHarness builds the standard harness with the engine wrapped in a
// gating pruner (the injected wait the async arms are controlled through).
func gatingHarness(t *testing.T) (*harness, *gatingPruner) {
	t.Helper()
	gate := &gatingPruner{
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
		firstDir: make(chan struct{}),
	}
	h := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		gate.Engine = d.GC.(storage.Engine)
		d.GC = gate
	}, nil)
	return h, gate
}

// adminJSON issues an admin request and returns status, Content-Type and
// the decoded JSON object (nil when the body is not a JSON object).
func adminJSON(t *testing.T, h interface {
	do(method, path, user, pass string, body []byte, hdr map[string]string) *http.Response
}, method, path string, body []byte) (int, string, map[string]any) {
	t.Helper()
	resp := h.do(method, path, adminUser, adminPass, body, nil)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, path, err)
	}
	var obj map[string]any
	_ = json.Unmarshal(raw, &obj)
	return resp.StatusCode, resp.Header.Get("Content-Type"), obj
}

// eventually polls cond until it holds or the deadline passes.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// seedOrphanBlob plants one backdated, unreferenced blob in the harness's
// data dir and returns its sha256.
func seedOrphanBlob(t *testing.T, dataDir, body string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	sha := hex.EncodeToString(sum[:])
	path := filepath.Join(dataDir, "blobs", sha[:2], sha)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir shard: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write blob: %v", err)
	}
	past := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatalf("backdate blob: %v", err)
	}
	return sha
}

// pruneStatus fetches and decodes the status report (nil when 412).
func pruneStatus(t *testing.T, h interface {
	do(method, path, user, pass string, body []byte, hdr map[string]string) *http.Response
}) (int, map[string]any) {
	t.Helper()
	code, _, obj := adminJSON(t, h, http.MethodGet, "/binflow/api/system/storage/prune/status", nil)
	return code, obj
}

func TestStoragePruneLifecycleSubmitStopFinish(t *testing.T) {
	h, gate := gatingHarness(t)

	// Never ran: both read and stop answer the 412 information bodies.
	code, ct, obj := adminJSON(t, h, http.MethodGet, "/binflow/api/system/storage/prune/status", nil)
	if code != http.StatusPreconditionFailed || obj["info"] != "No Prune task found" {
		t.Fatalf("status before any run = %d %v", code, obj)
	}
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("412 content type = %q", ct)
	}
	code, _, obj = adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/prune/stop", nil)
	if code != http.StatusPreconditionFailed || obj["info"] != "No running Prune task found" {
		t.Fatalf("stop before any run = %d %v", code, obj)
	}

	// Submit a dry run: 202 with the submission information body.
	code, ct, obj = adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/prune/start",
		[]byte(`{"dryRun":true}`))
	if code != http.StatusAccepted {
		t.Fatalf("start = %d, want 202", code)
	}
	if obj["info"] != "Pruning Unreferenced Data task has been submitted" {
		t.Fatalf("start info = %v", obj["info"])
	}
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("start content type = %q", ct)
	}

	// A second start while the task runs answers the 412 mutex arm.
	<-gate.entered // the task is parked inside its pass
	code, _, obj = adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/prune/start", nil)
	if code != http.StatusPreconditionFailed || obj["info"] != "Pruning Unreferenced Data task cannot be started" {
		t.Fatalf("concurrent start = %d %v", code, obj)
	}

	// Mid-flight status: running with the dryRun echo.
	code, obj = pruneStatus(t, h)
	if code != http.StatusOK || obj["status"] != "running" || obj["dryRun"] != true {
		t.Fatalf("mid-flight status = %d %v", code, obj)
	}

	// Release the pass: the run finishes on its own (the stop arm has its
	// own test — a stop requested here would land stopped instead).
	close(gate.release)
	eventually(t, "finished status", func() bool {
		code, obj = pruneStatus(t, h)
		return code == http.StatusOK && obj["status"] == "finished"
	})

	// The 26-field schema: 6 top-level keys, timing 6, report 3,
	// lastHandledDirectory 5 + its own timing 6.
	if len(obj) != 6 {
		t.Fatalf("top-level keys = %d (%v), want 6", len(obj), storageAdminKeysOf(obj))
	}
	timing, ok := obj["timing"].(map[string]any)
	if !ok || len(timing) != 6 {
		t.Fatalf("timing keys = %v", obj["timing"])
	}
	report, ok := obj["report"].(map[string]any)
	if !ok || len(report) != 3 {
		t.Fatalf("report keys = %v", obj["report"])
	}
	lastDir, ok := obj["lastHandledDirectory"].(map[string]any)
	if !ok || len(lastDir) != 6 {
		t.Fatalf("lastHandledDirectory keys = %v", obj["lastHandledDirectory"])
	}
	innerTiming, ok := lastDir["timing"].(map[string]any)
	if !ok || len(innerTiming) != 6 {
		t.Fatalf("lastHandledDirectory.timing keys = %v", lastDir["timing"])
	}
	if obj["progress"] != "256 of 256" {
		t.Fatalf("progress = %v, want 256 of 256", obj["progress"])
	}
	if lastDir["name"] != "ff" || lastDir["status"] != "finished" {
		t.Fatalf("last dir = %v/%v", lastDir["name"], lastDir["status"])
	}
	if _, ok := timing["durationMillis"].(float64); !ok {
		t.Fatalf("durationMillis missing: %v", timing)
	}
	// The per-directory timing must be sane too: the engine's deferred
	// FinishedAt write lands on the returned stats (the named-return
	// contract) — a zero FinishedAt once rendered absurd negative
	// durations.
	if dirTiming, ok := lastDir["timing"].(map[string]any); !ok || dirTiming["durationMillis"].(float64) < 0 {
		t.Fatalf("lastHandledDirectory.timing = %v", lastDir["timing"])
	}
	if s, _ := obj["status"].(string); s == "" {
		t.Fatal("status missing")
	}
}

func storageAdminKeysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestStoragePruneStoppedRunLandsStoppedTerminal(t *testing.T) {
	h, gate := gatingHarness(t)

	if code, _, obj := adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/prune/start", nil); code != http.StatusAccepted {
		t.Fatalf("start = %d %v", code, obj)
	}
	<-gate.entered
	close(gate.release)
	// The stop marker lands while the walk is already moving: the task
	// consumes it at the next directory boundary.
	<-gate.firstDir
	code, _, obj := adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/prune/stop", nil)
	if code != http.StatusAccepted || obj["info"] != "Prune task stop request submitted" {
		t.Fatalf("stop = %d %v", code, obj)
	}
	eventually(t, "stopped terminal status", func() bool {
		code, obj := pruneStatus(t, h)
		return code == http.StatusOK && obj["status"] == "stopped"
	})
	_, obj = pruneStatus(t, h)
	if lastDir, ok := obj["lastHandledDirectory"].(map[string]any); !ok || lastDir["status"] != "stopped" {
		t.Fatalf("stopped run's last dir = %v", obj["lastHandledDirectory"])
	}
	if prog, _ := obj["progress"].(string); prog == "256 of 256" {
		t.Fatalf("stopped run's progress = %q, want an early position", prog)
	}
}

func TestStoragePruneStartFromDirectoryMessageAndProgress(t *testing.T) {
	h := newHarness(t)
	code, _, obj := adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/prune/start",
		[]byte(`{"dryRun":true,"startFromDirectory":"80"}`))
	if code != http.StatusAccepted {
		t.Fatalf("start = %d, want 202", code)
	}
	if obj["info"] != "Prune Unreferenced Data task resumes from directory 80" {
		t.Fatalf("resume info = %v", obj["info"])
	}
	eventually(t, "finished resume run", func() bool {
		code, obj := pruneStatus(t, h)
		return code == http.StatusOK && obj["status"] == "finished"
	})
	_, obj = pruneStatus(t, h)
	if obj["progress"] != "256 of 256" {
		t.Fatalf("resume progress = %v, want 256 of 256 (denominator invariant)", obj["progress"])
	}
}

func TestStoragePruneDefaultPolarityDeletes(t *testing.T) {
	// D6: an ABSENT dryRun field means the run DELETES — the Artifactory
	// polarity, opposite the legacy /api/v1/system/gc apply default.
	h := newHarness(t)
	sha := seedOrphanBlob(t, h.dataDir, "polarity orphan payload")

	if code, _, obj := adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/prune/start", nil); code != http.StatusAccepted {
		t.Fatalf("start = %d %v", code, obj)
	}
	eventually(t, "finished apply run", func() bool {
		code, obj := pruneStatus(t, h)
		return code == http.StatusOK && obj["status"] == "finished"
	})
	if _, err := os.Stat(filepath.Join(h.dataDir, "blobs", sha[:2], sha)); !os.IsNotExist(err) {
		t.Fatalf("orphan survived a default (deleting) prune run: %v", err)
	}
	_, obj := pruneStatus(t, h)
	if obj["dryRun"] != false {
		t.Fatalf("dryRun echo = %v, want false", obj["dryRun"])
	}
	rep := obj["report"].(map[string]any)
	if rep["totalBinariesCleaned"] != float64(1) {
		t.Fatalf("cleaned = %v, want 1", rep["totalBinariesCleaned"])
	}
}

func TestStoragePruneReportPersistsAcrossRestart(t *testing.T) {
	h := newHarness(t)
	if code, _, obj := adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/prune/start",
		[]byte(`{"dryRun":true}`)); code != http.StatusAccepted {
		t.Fatalf("start = %d %v", code, obj)
	}
	eventually(t, "finished run", func() bool {
		code, obj := pruneStatus(t, h)
		return code == http.StatusOK && obj["status"] == "finished"
	})
	_, before := pruneStatus(t, h)

	// A fresh server over the same data dir serves the same report.
	rebuilt := h.rebuildWithDataDir(t, h.dataDir)
	req, err := http.NewRequest(http.MethodGet, rebuilt.ts.URL+"/binflow/api/system/storage/prune/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth(adminUser, adminPass)
	resp, err := rebuilt.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("status after restart: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status after restart = %d, want 200", resp.StatusCode)
	}
	var after map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&after)
	if after["status"] != before["status"] || after["dryRun"] != before["dryRun"] ||
		after["progress"] != before["progress"] {
		t.Fatalf("report drifted across restart: %v vs %v", after, before)
	}
}

func TestStoragePruneRunningReportRecoveredAsStopped(t *testing.T) {
	h := newHarness(t)
	started := time.Now()
	state := fmt.Sprintf(`{"status":"running","dryRun":false,"startedAt":%q,"lastUpdated":%q,"progress":7,`+
		`"totalBinariesProcessed":1,"totalBinariesCleaned":0,"totalBytesCleaned":0,"lastHandledDirectory":null}`,
		started.UTC().Format(time.RFC3339), started.UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(h.dataDir, "prune_report.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	rebuilt := h.rebuildWithDataDir(t, h.dataDir)
	req, err := http.NewRequest(http.MethodGet, rebuilt.ts.URL+"/binflow/api/system/storage/prune/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth(adminUser, adminPass)
	resp, err := rebuilt.ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var obj map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&obj)
	if obj["status"] != "stopped" {
		t.Fatalf("orphaned running report after restart = %v, want stopped", obj["status"])
	}
}

func TestStorageGCStreamFormAndConcurrentRefusal(t *testing.T) {
	h, gate := gatingHarness(t)

	type result struct {
		resp *http.Response
		body string
	}
	first := make(chan result, 1)
	go func() {
		resp := h.do(http.MethodPost, "/binflow/api/system/storage/gc", adminUser, adminPass, nil, nil)
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		first <- result{resp: resp, body: string(raw)}
	}()
	<-gate.entered // the gc holds the maintenance lock inside its pass

	// A concurrent manual gc is refused with 409, never queued.
	code, _, _ := adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/gc", nil)
	if code != http.StatusConflict {
		t.Fatalf("concurrent gc = %d, want 409", code)
	}

	close(gate.release)
	r := <-first
	if r.resp.StatusCode != http.StatusOK {
		t.Fatalf("gc status = %d, want 200", r.resp.StatusCode)
	}
	if ct := r.resp.Header.Get("Content-Type"); ct != "text/plain;charset=utf-8" {
		t.Fatalf("gc content type = %q", ct)
	}
	if dots := strings.Count(r.body, "."); dots != storage.PruneShardCount {
		t.Fatalf("gc body dots = %d, want %d (one per shard directory)", dots, storage.PruneShardCount)
	}
	if nl := strings.Count(r.body, "\n"); nl != storage.PruneShardCount/80 {
		t.Fatalf("gc body newlines = %d, want %d (one every 80 dots)", nl, storage.PruneShardCount/80)
	}
	if strings.Contains(r.body, "{") {
		t.Fatalf("gc body must carry no JSON: %q", r.body[:min(60, len(r.body))])
	}
}

func TestStorageOptimizeForm(t *testing.T) {
	h := newHarness(t)
	resp := h.do(http.MethodPost, "/binflow/api/system/storage/optimize", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("optimize = %d, want 202", resp.StatusCode)
	}
	if len(raw) != 0 {
		t.Fatalf("optimize body = %q, want empty", raw)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		t.Fatalf("optimize content type = %q, want none", ct)
	}
}

func TestStorageCompressStreamForm(t *testing.T) {
	h := newHarness(t)
	resp := h.do(http.MethodPost, "/binflow/api/system/storage/compress", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("compress = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain;charset=utf-8" {
		t.Fatalf("compress content type = %q", ct)
	}
	if !strings.Contains(string(raw), "internal database:") {
		t.Fatalf("compress body = %q, want the before/after line", raw)
	}
}

func TestStorageBackupArms(t *testing.T) {
	var mu sync.Mutex
	var ran []string
	h := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		d.BackupRunner = fakeBackupRunner(func(_ context.Context, key string) error {
			mu.Lock()
			ran = append(ran, key)
			mu.Unlock()
			return nil
		})
	}, nil)

	// Unknown key: the live 500 errors-envelope arm.
	code, _, obj := adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/backup?key=nope", nil)
	if code != http.StatusInternalServerError {
		t.Fatalf("backup unknown key = %d, want 500", code)
	}
	errs, ok := obj["errors"].([]any)
	if !ok || len(errs) != 1 {
		t.Fatalf("backup errors envelope = %v", obj)
	}
	entry := errs[0].(map[string]any)
	if entry["status"] != float64(500) || entry["message"] != "No backup identified with key 'nope'" {
		t.Fatalf("backup error entry = %v", entry)
	}

	// Disabled backup: the decompile's 500 arm.
	if err := h.md.Backups().Put(context.Background(), &metadata.Backup{
		Key: "off", Enabled: false, ExportDir: t.TempDir(), CreatedAt: "2026-09-11T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	code, _, _ = adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/backup?key=off", nil)
	if code != http.StatusInternalServerError {
		t.Fatalf("backup disabled = %d, want 500", code)
	}

	// Enabled backup: 200, the runner fires detached.
	if err := h.md.Backups().Put(context.Background(), &metadata.Backup{
		Key: "nightly", Enabled: true, ExportDir: t.TempDir(), CreatedAt: "2026-09-11T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	code, _, _ = adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/backup?key=nightly", nil)
	if code != http.StatusOK {
		t.Fatalf("backup enabled = %d, want 200", code)
	}
	eventually(t, "backup runner fire", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(ran) == 1 && ran[0] == "nightly"
	})
}

// fakeBackupRunner adapts a function onto the BackupRunner seam.
func fakeBackupRunner(fn func(ctx context.Context, key string) error) httpapi.BackupRunner {
	return backupRunnerFunc(fn)
}

type backupRunnerFunc func(ctx context.Context, key string) error

func (f backupRunnerFunc) Run(ctx context.Context, key string) error { return f(ctx, key) }

func TestStorageSizeAndInfoForms(t *testing.T) {
	h := newHarness(t)
	a := seedOrphanBlob(t, h.dataDir, "size-body-a")
	b := seedOrphanBlob(t, h.dataDir, "size-body-b")
	var want int64
	for _, body := range []string{"size-body-a", "size-body-b"} {
		want += int64(len(body))
	}
	_ = a
	_ = b

	resp := h.do(http.MethodGet, "/binflow/api/system/storage/size", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("size = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if strings.TrimSpace(string(raw)) != fmt.Sprint(want) {
		t.Fatalf("size = %q, want %d", raw, want)
	}

	resp2 := h.do(http.MethodGet, "/binflow/api/system/storage/info", adminUser, adminPass, nil, nil)
	defer func() { _ = resp2.Body.Close() }()
	raw2, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode != http.StatusOK || resp2.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("info = %d %q", resp2.StatusCode, resp2.Header.Get("Content-Type"))
	}
	var info struct {
		Data struct {
			BaseDataDir         string `json:"baseDataDir"`
			BinariesDir         string `json:"binariesDir"`
			UsageSpace          string `json:"usageSpace"`
			FreeSpace           string `json:"freeSpace"`
			TempDir             string `json:"tempDir"`
			UsageSpaceInPercent string `json:"usageSpaceInPercent"`
			ID                  string `json:"id"`
			TotalSpace          string `json:"totalSpace"`
			FreeSpaceInPercent  string `json:"freeSpaceInPercent"`
			FileStoreDir        string `json:"fileStoreDir"`
			Type                string `json:"type"`
		} `json:"data"`
		SubBinaryTreeElements []struct{} `json:"subBinaryTreeElements"`
	}
	if err := json.Unmarshal(raw2, &info); err != nil {
		t.Fatalf("info body: %v", err)
	}
	if info.Data.Type != "file-system" || info.Data.ID != "file-system" || info.Data.FileStoreDir != "blobs" {
		t.Fatalf("info provider = %+v", info.Data)
	}
	if !strings.HasSuffix(info.Data.BinariesDir, "blobs") || info.Data.BaseDataDir == "" {
		t.Fatalf("info dirs = %+v", info.Data)
	}
	if len(info.SubBinaryTreeElements) != 0 {
		t.Fatalf("info subBinaryTreeElements = %d, want 0", len(info.SubBinaryTreeElements))
	}
}

func TestStorageExportdsConstantFailure(t *testing.T) {
	h := newHarness(t)
	code, _, obj := adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/exportds?to=/tmp/x", nil)
	if code != http.StatusInternalServerError {
		t.Fatalf("exportds = %d, want 500", code)
	}
	if errs, ok := obj["errors"].([]any); !ok || len(errs) != 1 {
		t.Fatalf("exportds envelope = %v", obj)
	} else if e := errs[0].(map[string]any); e["message"] != "Export data is no longer supported" {
		t.Fatalf("exportds message = %v", e["message"])
	}
	// The failure is constant: a second call fails identically.
	code2, _, _ := adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/exportds?to=/tmp/y", nil)
	if code2 != http.StatusInternalServerError {
		t.Fatalf("second exportds = %d, want 500", code2)
	}
}

func TestStorageAdminPlaneAuthGates(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"alice", "alicepw"}})
	base := "/binflow/api/system/storage"

	// Anonymous: the family's 401 challenge.
	resp := h.do(http.MethodPost, base+"/prune/start", "", "", nil, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous start = %d, want 401", resp.StatusCode)
	}

	// Non-admin: 403 on the write faces, 403 on the read face.
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodPost, base + "/prune/start"},
		{http.MethodPost, base + "/gc"},
		{http.MethodPost, base + "/optimize"},
		{http.MethodGet, base + "/prune/status"},
		{http.MethodGet, base + "/size"},
	} {
		resp := h.do(tc.method, tc.path, "alice", "alicepw", nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("alice %s %s = %d, want 403", tc.method, tc.path, resp.StatusCode)
		}
	}
}

func TestStoragePruneBadBodyAndStartFrom(t *testing.T) {
	h := newHarness(t)
	code, _, obj := adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/prune/start",
		[]byte(`{"startFromDirectory":"zz"}`))
	if code != http.StatusBadRequest {
		t.Fatalf("start bad startFromDirectory = %d %v, want 400", code, obj)
	}
	code, _, _ = adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/prune/start",
		[]byte(`{not json`))
	if code != http.StatusBadRequest {
		t.Fatalf("start malformed body = %d, want 400", code)
	}
}

func TestStoragePruneStopIdleWithHistoryAccepted(t *testing.T) {
	// E4's idle arm (review N5): a historical report exists but no task is
	// running — the stop request still lands as 202 (the historical report
	// is the discriminator against the never-ran 412), and the leftover
	// marker must not stop the next run before its first directory.
	h := newHarness(t)
	if code, _, obj := adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/prune/start",
		[]byte(`{"dryRun":true}`)); code != http.StatusAccepted {
		t.Fatalf("start = %d %v", code, obj)
	}
	eventually(t, "finished run", func() bool {
		code, obj := pruneStatus(t, h)
		return code == http.StatusOK && obj["status"] == "finished"
	})

	code, _, obj := adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/prune/stop", nil)
	if code != http.StatusAccepted || obj["info"] != "Prune task stop request submitted" {
		t.Fatalf("idle stop with history = %d %v, want 202", code, obj)
	}

	// The idle marker dies with the next run (begin clears stopReq): the
	// fresh pass finishes, it does not land stopped.
	if code, _, _ := adminJSON(t, h, http.MethodPost, "/binflow/api/system/storage/prune/start",
		[]byte(`{"dryRun":true}`)); code != http.StatusAccepted {
		t.Fatalf("start after idle stop = %d, want 202", code)
	}
	eventually(t, "finished second run", func() bool {
		code, obj := pruneStatus(t, h)
		return code == http.StatusOK && obj["status"] == "finished"
	})
}

// markFailingPruner wraps the real engine with a Prune that dies in its
// Mark phase (out==nil, no directory ever walked) — the review N6 arm.
type markFailingPruner struct {
	storage.Engine
}

func (f *markFailingPruner) Prune(context.Context, storage.PruneOptions) (*storage.PruneOutcome, error) {
	return nil, errors.New("mark phase failed: referenced set unavailable")
}

func TestStorageGCStreamMarkFailureAudited(t *testing.T) {
	// Review N6: a gc stream whose Prune dies before the walk (out==nil)
	// must still land its gc.run audit row — an operator reading the
	// trail would otherwise see silence for a run that was attempted.
	h := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		d.GC = &markFailingPruner{Engine: d.GC.(storage.Engine)}
	}, nil)
	resp := h.do(http.MethodPost, "/binflow/api/system/storage/gc", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read gc body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("gc mark failure status = %d, want 200 (pinned)", resp.StatusCode)
	}
	if !strings.Contains(string(raw), "\n500 : mark phase failed") {
		t.Fatalf("gc mark failure body = %q, want the in-stream error line", raw)
	}
	eventually(t, "gc.run audit row for the failed attempt", func() bool {
		page, qerr := audit.New(h.md, true).Query(context.Background(),
			audit.Filter{Action: audit.ActionGCRun, Limit: 10})
		if qerr != nil || len(page.Events) == 0 {
			return false
		}
		ev := page.Events[len(page.Events)-1]
		return ev.Actor == adminUser && strings.Contains(ev.Detail, `"error":true`)
	})
}
