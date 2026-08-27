package httpapi_test

// T-324 REST face: POST/GET /binflow/api/v1/system/cleanup over the real
// harness stack (real engine, real metadata, real audit logger — the
// depsMutate seam injects the same repo.CleanupEngine cmd will wire).
// Pins the dry-run default, the apply outcome with the zero-orphan shape,
// the GET status plane, the 503/403/400 postures and the cleanup.run audit
// row the query plane can filter on.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// t324Harness builds the harness with the cleanup engine wired into Deps.
func t324Harness(t *testing.T) *harness {
	t.Helper()
	return t324HarnessMetrics(t, false)
}

// t324HarnessMetrics optionally wires the metrics registry so the scrape
// plane can be asserted alongside the trigger plane.
func t324HarnessMetrics(t *testing.T, withMetrics bool) *harness {
	t.Helper()
	return newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		if withMetrics {
			d.Metrics = metrics.NewRegistry()
		}
		eng, err := repo.NewCleanupEngine(repo.CleanupOptions{
			Store:        d.Metadata,
			Engine:       d.GC,
			Audit:        audit.New(d.Metadata, true),
			AuditEnabled: true,
			DataDir:      d.DataDir,
			Grace:        time.Nanosecond,
			TickEvery:    time.Hour,
		})
		if err != nil {
			t.Fatalf("NewCleanupEngine: %v", err)
		}
		d.Cleanup = eng
	}, [][2]string{{"alice", "pw"}})
}

// t324POST drives the trigger endpoint.
func t324POST(t *testing.T, h *harness, body, user, pass string) (int, map[string]any) {
	t.Helper()
	var payload []byte
	if body != "" {
		payload = []byte(body)
	}
	resp := h.do(http.MethodPost, "/binflow/api/v1/system/cleanup", user, pass, payload,
		map[string]string{"Content-Type": "application/json"})
	defer func() { _ = resp.Body.Close() }()
	raw := mustGet(t, resp)
	var out map[string]any
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("cleanup body is not JSON: %v\n%s", err, raw)
		}
	}
	return resp.StatusCode, out
}

// t324GET drives the status endpoint.
func t324GET(t *testing.T, h *harness, user, pass string) (int, map[string]any) {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/v1/system/cleanup", user, pass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	raw := mustGet(t, resp)
	var out map[string]any
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("cleanup status is not JSON: %v\n%s", err, raw)
		}
	}
	return resp.StatusCode, out
}

// t324RepoRow fetches one repo's report row out of the decoded response.
func t324RepoRow(t *testing.T, body map[string]any, key string) map[string]any {
	t.Helper()
	rows, ok := body["repos"].([]any)
	if !ok {
		t.Fatalf("response repos missing: %v", body)
	}
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok && m["repo"] == key {
			return m
		}
	}
	t.Fatalf("repo %s not in response: %v", key, body)
	return nil
}

// t324AuditEvent is one cleanup.run row as the audit query plane returns it.
type t324AuditEvent struct {
	Actor  string         `json:"actor"`
	Action string         `json:"action"`
	Detail map[string]any `json:"detail"`
}

// TestT324RESTDryRunDefaultAndApply is the full REST chain: a remote repo
// with a period, one stale and one used cached artifact, the dry-run
// default, then the apply that removes exactly the stale one — node and
// blob and ledger row all gone (the zero-orphan shape at the REST plane).
func TestT324RESTDryRunDefaultAndApply(t *testing.T) {
	h := t324Harness(t)
	ctx := context.Background()

	if _, err := h.svc.CreateRepo(ctx, t324Admin(), &metadata.Repo{
		RepoKey: "rest-cache", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
		Config: `{"url":"https://up.example.org","unusedArtifactsCleanupPeriodHours":24}`,
	}); err != nil {
		t.Fatalf("CreateRepo(remote): %v", err)
	}
	stale := t324SeedCacheNode(t, h, "rest-cache", "stale.bin", "legacy-bytes", -48*time.Hour)
	t324SeedCacheNode(t, h, "rest-cache", "kept.bin", "kept-bytes", -48*time.Hour)
	// kept.bin stays: a download event inside the window.
	if err := audit.New(h.md, true).Append(ctx, audit.Event{
		Action: audit.ActionDownload, Repo: "rest-cache", Path: "kept.bin", Actor: "admin",
	}); err != nil {
		t.Fatalf("seed download event: %v", err)
	}

	// POST with no body: the dry-run default.
	st, body := t324POST(t, h, "", adminUser, adminPass)
	if st != http.StatusOK {
		t.Fatalf("dry POST status = %d", st)
	}
	if body["apply"] != false {
		t.Fatalf("default run apply = %v, want false", body["apply"])
	}
	row := t324RepoRow(t, body, "rest-cache")
	if row["candidates"].(float64) != 1 || row["deleted"].(float64) != 0 {
		t.Fatalf("dry row = %v, want 1 candidate 0 deleted", row)
	}
	if t324NodeOf(t, h, "rest-cache", "stale.bin") == nil {
		t.Fatal("dry run removed the node")
	}

	// Apply.
	st, body = t324POST(t, h, `{"apply":true}`, adminUser, adminPass)
	if st != http.StatusOK {
		t.Fatalf("apply POST status = %d", st)
	}
	row = t324RepoRow(t, body, "rest-cache")
	if row["deleted"].(float64) != 1 {
		t.Fatalf("apply row = %v, want 1 deleted", row)
	}
	if t324NodeOf(t, h, "rest-cache", "stale.bin") != nil {
		t.Fatal("apply left the stale node behind")
	}
	if t324NodeOf(t, h, "rest-cache", "kept.bin") == nil {
		t.Fatal("apply reaped the used node")
	}
	// Zero-orphan, blob half, through the physical walk the gc face's
	// tests use: the stale blob is gone file AND ledger row.
	blobPath, err := storage.BlobPath(h.dataDir, stale.Sha256)
	if err != nil {
		t.Fatalf("BlobPath: %v", err)
	}
	if _, statErr := os.Stat(blobPath); statErr == nil {
		t.Fatal("stale blob file survived the apply run")
	}
	if _, err := h.md.Blobs().Get(ctx, stale.Sha256); err == nil {
		t.Fatal("stale blobs-ledger row survived the apply run")
	}

	// GET status: schedule + counters + last run + per-repo policy.
	st, status := t324GET(t, h, adminUser, adminPass)
	if st != http.StatusOK {
		t.Fatalf("GET status = %d", st)
	}
	if status["enabled"] != true {
		t.Fatalf("status enabled = %v, want true", status["enabled"])
	}
	if status["auditEnabled"] != true {
		t.Fatalf("status auditEnabled = %v", status["auditEnabled"])
	}
	stats := status["stats"].(map[string]any)
	if stats["objectsCleaned"].(float64) != 1 {
		t.Fatalf("stats = %v, want 1 object cleaned", stats)
	}
	if status["lastRun"] == nil {
		t.Fatal("status lastRun missing")
	}
	rows := status["repos"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["periodHours"].(float64) != 24 {
		t.Fatalf("status repos = %v", rows)
	}

	// The audit trail carries one row per run, filterable through the
	// query plane (the gc.run posture, FR-30-AC5's cleanup analogue).
	resp := h.do(http.MethodGet, "/binflow/api/v1/audit?action=cleanup.run&limit=10", adminUser, adminPass, nil, nil)
	raw := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit query: %d (%s)", resp.StatusCode, raw)
	}
	var page struct {
		Events []t324AuditEvent `json:"events"`
	}
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		t.Fatalf("audit body: %v\n%s", err, raw)
	}
	if len(page.Events) != 2 {
		t.Fatalf("cleanup.run events: %d, want 2 (dry + apply)\n%s", len(page.Events), raw)
	}
	// Newest first: the apply run, then the dry run; actor is the
	// administering principal, detail carries the report shape.
	for i, wantApply := range []bool{true, false} {
		ev := page.Events[i]
		if ev.Actor != "admin" || ev.Action != "cleanup.run" {
			t.Fatalf("event %d: actor=%s action=%s", i, ev.Actor, ev.Action)
		}
		if got := ev.Detail["apply"].(bool); got != wantApply {
			t.Fatalf("event %d detail apply = %v, want %v", i, got, wantApply)
		}
		if _, ok := ev.Detail["repos"]; !ok {
			t.Fatalf("event %d detail misses repos: %v", i, ev.Detail)
		}
	}

	// The structured completion log is the operator's live signal. The
	// engine logs through its own slog.Default unless the assembler injects
	// the harness logger; the repo-package tests carry the line evidence
	// (see reports/agents/T-324.md), so the REST plane asserts the
	// engine-independent facts only.
}

// TestT324RESTPostures: 403 non-admin, 400 bad body, E-26 on unknown
// shapes, and the honest 503 on an unwired stack (both verbs).
func TestT324RESTPostures(t *testing.T) {
	h := t324Harness(t)

	if st, _ := t324POST(t, h, "", "alice", "pw"); st != http.StatusForbidden {
		t.Fatalf("non-admin POST = %d, want 403", st)
	}
	resp := h.do(http.MethodPost, "/binflow/api/v1/system/cleanup", adminUser, adminPass,
		[]byte(`{not json`), map[string]string{"Content-Type": "application/json"})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad body = %d, want 400", resp.StatusCode)
	}
	resp = h.do(http.MethodPost, "/binflow/api/v1/system/cleanup/extra", adminUser, adminPass, nil, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown shape = %d, want 404", resp.StatusCode)
	}

	bare := newHarness(t)
	if st, _ := t324POST(t, bare, "", adminUser, adminPass); st != http.StatusServiceUnavailable {
		t.Fatalf("unwired POST = %d, want 503", st)
	}
	if st, _ := t324GET(t, bare, adminUser, adminPass); st != http.StatusServiceUnavailable {
		t.Fatalf("unwired GET = %d, want 503", st)
	}
}

// TestT324RESTScopedRun: the repo body field scopes the policy leg; an
// unknown repo answers the engine's wrapped not-found error.
func TestT324RESTScopedRun(t *testing.T) {
	h := t324Harness(t)
	ctx := context.Background()
	if _, err := h.svc.CreateRepo(ctx, t324Admin(), &metadata.Repo{
		RepoKey: "scoped-cache", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
		Config: `{"url":"https://up.example.org","unusedArtifactsCleanupPeriodHours":24}`,
	}); err != nil {
		t.Fatalf("CreateRepo(remote): %v", err)
	}
	st, body := t324POST(t, h, `{"repo":"scoped-cache"}`, adminUser, adminPass)
	if st != http.StatusOK {
		t.Fatalf("scoped POST = %d", st)
	}
	t324RepoRow(t, body, "scoped-cache")

	st, _ = t324POST(t, h, `{"repo":"no-such"}`, adminUser, adminPass)
	if st != http.StatusNotFound {
		t.Fatalf("unknown repo POST = %d, want 404", st)
	}
}

// ---- fixtures ----

// t324Admin is the harness admin as a repo principal.
func t324Admin() *repo.Principal { return &repo.Principal{Name: adminUser, Admin: true} }

// t324SeedCacheNode lands one cache-shaped artifact through the REAL
// engine session path (blob file + ledger row) plus a node row aged by age
// and a remote_cache validator row — the pull-through landing's end state,
// minus the upstream hop the repo-package tests already own.
func t324SeedCacheNode(t *testing.T, h *harness, repoKey, path, content string, age time.Duration) *metadata.Node {
	t.Helper()
	ctx := context.Background()
	sess, err := h.st.BeginSession(ctx)
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if _, err := sess.Append(ctx, strings.NewReader(content)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	ref, err := sess.Commit(ctx, storage.BlobRef{})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	// The service's landing path releases the GC hold once the node row is
	// committed (ADR-0031's R step); this fixture commits directly through
	// the session, so it performs the same release after its own metadata
	// writes below — done at function end once the rows stand.
	defer func() { _ = h.st.ReleaseGCHold(ref.Sha256) }()
	now := time.Now().UTC()
	aged := now.Add(age)
	stamp := aged.Format(time.RFC3339)
	if err := h.md.Blobs().Put(ctx, &metadata.Blob{
		Sha256: ref.Sha256, Sha1: ref.Sha1, Md5: ref.Md5, Size: ref.Size, CreatedAt: stamp,
	}); err != nil {
		t.Fatalf("Blobs().Put: %v", err)
	}
	node := &metadata.Node{
		RepoKey: repoKey, Path: path, Sha256: ref.Sha256, Size: ref.Size,
		Mime: "application/octet-stream", CreatedBy: "remote-proxy",
		CreatedAt: stamp, UpdatedAt: stamp,
	}
	if err := h.md.Nodes().Put(ctx, node); err != nil {
		t.Fatalf("Nodes().Put: %v", err)
	}
	if err := h.md.Remote().PutCache(ctx, &metadata.RemoteCacheEntry{
		RepoKey: repoKey, Path: path, Kind: metadata.RemoteCacheKindContent,
		FetchedAt: stamp, ExpiresAt: stamp,
	}); err != nil {
		t.Fatalf("Remote().PutCache: %v", err)
	}
	return node
}

// t324NodeOf returns the node row or nil.
func t324NodeOf(t *testing.T, h *harness, repoKey, path string) *metadata.Node {
	t.Helper()
	n, err := h.md.Nodes().Get(context.Background(), repoKey, path)
	if err == nil {
		return n
	}
	if strings.Contains(err.Error(), "not found") {
		return nil
	}
	t.Fatalf("Nodes().Get %s/%s: %v", repoKey, path, err)
	return nil
}

// TestT324MetricsGauges: the FR-102.2 metrics row — cumulative objects and
// bytes families exposed on /metrics once a run moved counters (the
// scrape-time gauge snapshot, replTasks precedent).
func TestT324MetricsGauges(t *testing.T) {
	h := t324HarnessMetrics(t, true)
	ctx := context.Background()
	if _, err := h.svc.CreateRepo(ctx, t324Admin(), &metadata.Repo{
		RepoKey: "metrics-cache", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
		Config: `{"url":"https://up.example.org","unusedArtifactsCleanupPeriodHours":24}`,
	}); err != nil {
		t.Fatalf("CreateRepo(remote): %v", err)
	}
	t324SeedCacheNode(t, h, "metrics-cache", "gone.bin", "metrics-bytes-0123456789", -48*time.Hour)

	if st, body := t324POST(t, h, `{"apply":true}`, adminUser, adminPass); st != http.StatusOK {
		t.Fatalf("apply POST = %d (%v)", st, body)
	}

	resp := h.do(http.MethodGet, "/metrics", "", "", nil, nil)
	defer func() { _ = resp.Body.Close() }()
	raw := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics scrape = %d (%s)", resp.StatusCode, raw)
	}
	for _, want := range []string{
		"binflow_cleanup_objects 1",
		"binflow_cleanup_bytes 24", // len("metrics-bytes-0123456789")
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("metrics body misses %q:\n%s", want, raw)
		}
	}
}
