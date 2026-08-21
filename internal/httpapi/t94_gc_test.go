package httpapi_test

// T-94: the managed GC face (FR-30/GE-03). W24 (dry-run reports and never
// touches data), W25 (apply frees exactly the reported candidates; a blob
// kept alive by a second referencing path survives) and the W25b GC side
// (the data-directory maintenance lock answers 409 while an export or
// another gc holds it). FR-30-AC5's audit leg asserts gc.run lands with
// the candidate counts; the CLI-side audit + M2 O3 zero-regression run on
// the real machine (see reports/agents/T-94.md).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// t94Response decodes the GE-03 body.
type t94Response struct {
	CandidateCount int   `json:"candidateCount"`
	CandidateBytes int64 `json:"candidateBytes"`
	DeletedCount   int   `json:"deletedCount"`
}

// t94GC POSTs the gc endpoint with the given JSON body ("" = no body) and
// returns the status, the decoded body (on 200) and the raw text.
func t94GC(t *testing.T, h *harness, body string, user, pass string) (int, t94Response, string) {
	t.Helper()
	var payload []byte
	if body != "" {
		payload = []byte(body)
	}
	resp := h.do(http.MethodPost, "/binflow/api/v1/system/gc", user, pass, payload,
		map[string]string{"Content-Type": "application/json"})
	raw := mustGet(t, resp)
	var out t94Response
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("gc body is not the GE-03 shape: %v\n%s", err, raw)
		}
	}
	return resp.StatusCode, out, raw
}

// t94Stats reads the whole-instance blob count.
func t94Stats(t *testing.T, h *harness) int64 {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/v1/storage/stats", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("storage stats: status %d (%s)", resp.StatusCode, mustGet(t, resp))
	}
	var stats struct {
		Blobs int64 `json:"blobs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("storage stats body: %v", err)
	}
	return stats.Blobs
}

// t94Put uploads one generic artifact and asserts the status.
func t94Put(t *testing.T, h *harness, repo, path, content string, want int) {
	t.Helper()
	resp := h.do(http.MethodPut, "/binflow/"+repo+"/"+path, adminUser, adminPass, []byte(content), nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != want {
		t.Fatalf("PUT %s/%s: status %d, want %d (%s)", repo, path, resp.StatusCode, want, mustGet(t, resp))
	}
}

// t94Delete removes one node through the content plane.
func t94Delete(t *testing.T, h *harness, repo, path string) {
	t.Helper()
	resp := h.do(http.MethodDelete, "/binflow/"+repo+"/"+path, adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE %s/%s: status %d (%s)", repo, path, resp.StatusCode, mustGet(t, resp))
	}
}

// TestT94GCDryRunAndApply walks the W24/W25 sequence against the real
// stack: a deduplicated blob kept alive by its second referencing path
// survives the apply (keep.jar), the true orphan is freed exactly as
// reported (stats and blobs ledger both drop by the same amount) and a
// follow-up dry-run sees zero candidates.
func TestT94GCDryRunAndApply(t *testing.T) {
	h := newHarness(t)
	createRepoHTTP(t, h, "generic-local")

	orphan := strings.Repeat("T-94 orphan payload\n", 64) // blob Y: freed
	keeper := strings.Repeat("T-94 keeper payload\n", 96) // blob X: survives
	t94Put(t, h, "generic-local", "gc/b.jar", orphan, http.StatusCreated)
	t94Put(t, h, "generic-local", "gc/a.jar", keeper, http.StatusCreated)
	// Same content: dedups onto the same physical blob, so deleting a.jar
	// below leaves the blob referenced by keep.jar (W25's survivor).
	t94Put(t, h, "generic-local", "gc/keep.jar", keeper, http.StatusCreated)
	t94Delete(t, h, "generic-local", "gc/a.jar")
	t94Delete(t, h, "generic-local", "gc/b.jar")

	before := t94Stats(t, h)
	// T-128 (ADR-0016): the folder marker blob (sha256=64×'0', size=0)
	// is also counted — 2 content blobs (dedup collapsed a.jar/keep.jar)
	// + 1 folder marker = 3. GC must never touch the folder marker.
	if before != 3 {
		t.Fatalf("stats before gc: %d blobs, want 3 (2 content + folder marker)", before)
	}

	// W24: dry-run reports the orphan, touches nothing.
	status, dry, raw := t94GC(t, h, `{"apply":false,"graceHours":0}`, adminUser, adminPass)
	if status != http.StatusOK || dry.CandidateCount != 1 || dry.DeletedCount != 0 ||
		dry.CandidateBytes != int64(len(orphan)) {
		t.Fatalf("dry-run: status %d body %s", status, raw)
	}
	if after := t94Stats(t, h); after != before {
		t.Fatalf("dry-run changed stats: %d -> %d", before, after)
	}

	// W25: apply frees the orphan; the keeper's blob survives because
	// keep.jar still references it.
	status, applied, raw := t94GC(t, h, `{"apply":true,"graceHours":0}`, adminUser, adminPass)
	if status != http.StatusOK || applied.CandidateCount != 1 ||
		applied.DeletedCount != applied.CandidateCount ||
		applied.CandidateBytes != int64(len(orphan)) {
		t.Fatalf("apply: status %d body %s", status, raw)
	}
	if after := t94Stats(t, h); after != before-int64(applied.DeletedCount) {
		t.Fatalf("stats after apply: %d, want %d", after, before-int64(applied.DeletedCount))
	}
	resp := h.do(http.MethodGet, "/binflow/generic-local/gc/keep.jar", adminUser, adminPass, nil, nil)
	if body := mustGet(t, resp); resp.StatusCode != http.StatusOK || body != keeper {
		t.Fatalf("referenced blob did not survive: status %d", resp.StatusCode)
	}

	// The blobs ledger row died with the physical file (the CLI run's
	// teardown; a phantom row would survive as a "blob" in stats).
	if rows, err := h.md.Blobs().Count(context.Background()); err != nil || rows != 2 {
		t.Fatalf("blobs ledger after apply: %d rows (err %v), want 2 (keeper content + folder marker)", rows, err)
	}

	// Post-apply dry-run: zero candidates.
	if status, again, raw := t94GC(t, h, `{"apply":false,"graceHours":0}`, adminUser, adminPass); status != http.StatusOK ||
		again.CandidateCount != 0 || again.DeletedCount != 0 || again.CandidateBytes != 0 {
		t.Fatalf("post-apply dry-run: status %d body %s", status, raw)
	}
}

// TestT94GCGraceDefault pins the graceHours pointer semantics: ABSENT uses
// storage.gc_grace_hours (here nudged to a no-window value through the
// config mutation seam), while an explicit 0 is the no-window request —
// and a fresh orphan stays inside the default 24h configured grace.
func TestT94GCGraceDefault(t *testing.T) {
	h := newHarnessCfg(t, func(c *mutatedConfig) {
		c.Storage.GCGrace = time.Nanosecond
	}, nil)
	createRepoHTTP(t, h, "generic-local")
	t94Put(t, h, "generic-local", "g/a.jar", "fresh orphan", http.StatusCreated)
	t94Delete(t, h, "generic-local", "g/a.jar")

	// Absent graceHours -> configured (no-window) grace: the orphan is
	// visible, and the request defaults to the dry-run posture.
	if status, out, raw := t94GC(t, h, `{}`, adminUser, adminPass); status != http.StatusOK ||
		out.CandidateCount != 1 || out.DeletedCount != 0 {
		t.Fatalf("absent graceHours: status %d body %s", status, raw)
	}
	if status, out, raw := t94GC(t, h, "", adminUser, adminPass); status != http.StatusOK ||
		out.CandidateCount != 1 || out.DeletedCount != 0 {
		t.Fatalf("empty body (dry-run default): status %d body %s", status, raw)
	}

	// Same shape on an untuned instance: a fresh orphan sits inside the
	// default 24h grace and is NOT a candidate.
	h24 := newHarness(t)
	createRepoHTTP(t, h24, "generic-local")
	t94Put(t, h24, "generic-local", "g/a.jar", "fresh orphan", http.StatusCreated)
	t94Delete(t, h24, "generic-local", "g/a.jar")
	if status, out, raw := t94GC(t, h24, `{}`, adminUser, adminPass); status != http.StatusOK ||
		out.CandidateCount != 0 {
		t.Fatalf("default-grace instance: status %d body %s", status, raw)
	}
}

// TestT94GCRequestBodyValidation walks the request-shape matrix: the admin
// gate, JSON/type/range validation, and the verbs that have no route (the
// GET status endpoint is a recorded P2 debt — its absence is E-26).
func TestT94GCRequestBodyValidation(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"jane", "jane-pass"}})

	cases := []struct {
		name    string
		method  string
		body    string
		user    string
		pass    string
		want    int
		wantSub string
	}{
		{"non-admin refused", http.MethodPost, `{}`, "jane", "jane-pass", http.StatusForbidden, "administrator"},
		{"anonymous challenged", http.MethodPost, `{}`, "", "", http.StatusUnauthorized, ""},
		{"negative graceHours", http.MethodPost, `{"graceHours":-1}`, "admin", adminPass, http.StatusBadRequest, "graceHours"},
		{"overflowing graceHours", http.MethodPost, `{"graceHours":876001}`, "admin", adminPass, http.StatusBadRequest, "graceHours"},
		{"string graceHours", http.MethodPost, `{"graceHours":"0"}`, "admin", adminPass, http.StatusBadRequest, "graceHours"},
		{"malformed json", http.MethodPost, `{"apply":`, "admin", adminPass, http.StatusBadRequest, "not valid gc request JSON"},
		{"get has no route", http.MethodGet, "", "admin", adminPass, http.StatusNotFound, "not implemented"},
		{"delete has no route", http.MethodDelete, "", "admin", adminPass, http.StatusNotFound, "not implemented"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var payload []byte
			if tc.body != "" {
				payload = []byte(tc.body)
			}
			resp := h.do(tc.method, "/binflow/api/v1/system/gc", tc.user, tc.pass, payload,
				map[string]string{"Content-Type": "application/json"})
			raw := mustGet(t, resp)
			if resp.StatusCode != tc.want {
				t.Fatalf("status %d, want %d (%s)", resp.StatusCode, tc.want, raw)
			}
			if tc.wantSub != "" && !strings.Contains(raw, tc.wantSub) {
				t.Fatalf("body %q does not contain %q", raw, tc.wantSub)
			}
		})
	}
}

// TestT94GCLockMutualExclusion drives the W25b GC side: while the shared
// data-directory maintenance lock is held by an export (or another gc —
// REST or CLI, same primitive), the endpoint answers 409 with the
// ErrDataLockHeld sentence and the holder diagnostics, and never runs a
// sweep. The holder-word mapping follows the storage op vocabulary
// (T-96 architecture review N1..N3): export names the PRD's literal
// "export in progress", gc names itself, an unreadable record falls back
// to the generic maintenance wording without guessing.
func TestT94GCLockMutualExclusion(t *testing.T) {
	h := newHarness(t)
	createRepoHTTP(t, h, "generic-local")
	t94Put(t, h, "generic-local", "gc/a.jar", "payload", http.StatusCreated)
	t94Delete(t, h, "generic-local", "gc/a.jar")
	before := t94Stats(t, h)

	cases := []struct {
		name    string
		op      string
		wantSub []string
	}{
		{
			name: "export holds the lock",
			op:   storage.DataLockOpExport,
			wantSub: []string{
				"export in progress",
				"data directory is locked by another maintenance operation",
				"op=export",
			},
		},
		{
			name: "another gc holds the lock",
			op:   storage.DataLockOpGC,
			wantSub: []string{
				"another gc run is in progress",
				"data directory is locked by another maintenance operation",
				"op=gc",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lock, err := storage.AcquireDataLock(h.dataDir, tc.op)
			if err != nil {
				t.Fatalf("hold lock as %s: %v", tc.op, err)
			}
			defer func() { _ = lock.Release() }()

			// Even an apply must be refused before any sweep work.
			status, _, raw := t94GC(t, h, `{"apply":true,"graceHours":0}`, adminUser, adminPass)
			if status != http.StatusConflict {
				t.Fatalf("status %d, want 409 (%s)", status, raw)
			}
			for _, sub := range tc.wantSub {
				if !strings.Contains(raw, sub) {
					t.Fatalf("409 body %q does not contain %q", raw, sub)
				}
			}
			if after := t94Stats(t, h); after != before {
				t.Fatalf("a refused gc still changed stats: %d -> %d", before, after)
			}
		})
	}

	// The unknown-holder branch (T-96 review N3: the Windows byte-range
	// lock makes the record unreadable; a blanked record stands in): the
	// refusal must not guess "export" — the generic maintenance wording
	// carries it.
	lock, err := storage.AcquireDataLock(h.dataDir, storage.DataLockOpGC)
	if err != nil {
		t.Fatalf("hold lock: %v", err)
	}
	if err := os.Truncate(filepath.Join(h.dataDir, ".maintenance.lock"), 0); err != nil {
		t.Fatalf("blank holder record: %v", err)
	}
	status, _, raw := t94GC(t, h, `{"apply":true,"graceHours":0}`, adminUser, adminPass)
	if status != http.StatusConflict ||
		!strings.Contains(raw, "another maintenance operation is in progress") ||
		strings.Contains(raw, "export in progress") {
		t.Fatalf("unknown-holder 409: status %d body %s", status, raw)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// TestT94GCDockerRefsMarkSet pins the docker half of the mark set: a blob
// referenced only through docker_refs (a config or layer digest with no
// node row) is NOT a candidate, while an unreferenced on-disk blob is —
// the walk must union nodes with docker_refs (architecture 11.12).
func TestT94GCDockerRefsMarkSet(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	body := func(name string) string { return "docker blob " + name }
	digest := func(content string) string {
		sum := sha256.Sum256([]byte(content))
		return hex.EncodeToString(sum[:])
	}
	seedBlob := func(content string) string {
		sha := digest(content)
		path, err := storage.BlobPath(h.dataDir, sha)
		if err != nil {
			t.Fatalf("blob path: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("shard dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("seed blob: %v", err)
		}
		return sha
	}

	// A docker repository (the management API helper spells generic only,
	// so the row goes in through the store seam the /v2 tests also use).
	if err := h.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "docker-local", Type: "local", PackageType: "docker",
	}); err != nil {
		t.Fatalf("create docker repo: %v", err)
	}
	kept := seedBlob(body("kept"))
	dead := seedBlob(body("dead"))
	manifestDigest := digest("manifest:" + kept)
	if err := h.md.Docker().PutManifest(ctx, &metadata.DockerManifest{
		RepoKey: "docker-local", Image: "app", Digest: manifestDigest, Size: 1,
	}); err != nil {
		t.Fatalf("put manifest: %v", err)
	}
	if err := h.md.Docker().PutRefs(ctx, "docker-local", "app", manifestDigest,
		[]*metadata.DockerRef{{
			RepoKey: "docker-local", Image: "app",
			ManifestDigest: manifestDigest, BlobDigest: kept,
		}}); err != nil {
		t.Fatalf("put refs: %v", err)
	}

	status, out, raw := t94GC(t, h, `{"apply":false,"graceHours":0}`, adminUser, adminPass)
	if status != http.StatusOK || out.CandidateCount != 1 || out.CandidateBytes != int64(len(body("dead"))) {
		t.Fatalf("docker mark set: status %d body %s (kept=%s dead=%s)", status, raw, kept, dead)
	}
	// The dead file is gone after an apply; the docker-referenced one stays.
	if status, applied, raw := t94GC(t, h, `{"apply":true,"graceHours":0}`, adminUser, adminPass); status != http.StatusOK ||
		applied.DeletedCount != 1 {
		t.Fatalf("docker mark apply: status %d body %s", status, raw)
	}
	if _, err := os.Stat(filepath.Join(h.dataDir, "blobs", kept[:2], kept)); err != nil {
		t.Fatalf("docker-referenced blob was swept: %v", err)
	}
}

// t94AuditEvent is one gc.run row as the audit query plane returns it.
type t94AuditEvent struct {
	Actor  string         `json:"actor"`
	Action string         `json:"action"`
	Detail map[string]any `json:"detail"`
}

// TestT94GCAuditTrail asserts the gc.run events (FR-30-AC5): one per run,
// actor = the administering principal, detail carrying the candidate
// count/bytes pair the audit history surface is built on, and graceHours
// null exactly when the request left it absent.
func TestT94GCAuditTrail(t *testing.T) {
	h := newHarness(t)
	createRepoHTTP(t, h, "generic-local")
	t94Put(t, h, "generic-local", "g/a.jar", "payload", http.StatusCreated)
	t94Delete(t, h, "generic-local", "g/a.jar")

	if status, _, raw := t94GC(t, h, `{"apply":false,"graceHours":0}`, adminUser, adminPass); status != http.StatusOK {
		t.Fatalf("dry-run: %d (%s)", status, raw)
	}
	if status, _, raw := t94GC(t, h, `{"apply":true}`, adminUser, adminPass); status != http.StatusOK {
		t.Fatalf("apply: %d (%s)", status, raw)
	}

	resp := h.do(http.MethodGet, "/binflow/api/v1/audit?action=gc.run&limit=10", adminUser, adminPass, nil, nil)
	raw := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit query: %d (%s)", resp.StatusCode, raw)
	}
	var page struct {
		Events []t94AuditEvent `json:"events"`
	}
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		t.Fatalf("audit body: %v\n%s", err, raw)
	}
	if len(page.Events) != 2 {
		t.Fatalf("gc.run events: %d, want 2\n%s", len(page.Events), raw)
	}
	// Newest first: the apply run (absent graceHours -> null), then the
	// dry-run (explicit 0).
	applyRun, dryRun := page.Events[0], page.Events[1]
	for _, tc := range []struct {
		name      string
		ev        t94AuditEvent
		wantApply bool
		wantGrace any
	}{
		{"apply run", applyRun, true, nil},
		{"dry-run", dryRun, false, float64(0)},
	} {
		if tc.ev.Actor != "admin" || tc.ev.Action != "gc.run" {
			t.Fatalf("%s event: actor=%s action=%s", tc.name, tc.ev.Actor, tc.ev.Action)
		}
		if got, ok := tc.ev.Detail["apply"].(bool); !ok || got != tc.wantApply {
			t.Fatalf("%s detail apply = %v, want %v (%v)", tc.name, tc.ev.Detail["apply"], tc.wantApply, tc.ev.Detail)
		}
		if got := tc.ev.Detail["graceHours"]; got != tc.wantGrace {
			t.Fatalf("%s detail graceHours = %v (%T), want %v", tc.name, got, got, tc.wantGrace)
		}
		for _, key := range []string{"candidateCount", "candidateBytes", "deletedCount"} {
			if _, ok := tc.ev.Detail[key]; !ok {
				t.Fatalf("%s detail misses %q: %v", tc.name, key, tc.ev.Detail)
			}
		}
	}
	if got, ok := dryRun.Detail["candidateCount"].(float64); !ok || got != 1 {
		t.Fatalf("dry-run candidateCount = %v, want 1", dryRun.Detail["candidateCount"])
	}
	if got, ok := dryRun.Detail["candidateBytes"].(float64); !ok || got != float64(len("payload")) {
		t.Fatalf("dry-run candidateBytes = %v, want %d", dryRun.Detail["candidateBytes"], len("payload"))
	}
	// The structured completion log is the operator's live signal.
	if !strings.Contains(h.logs(), "httpapi: gc run complete") {
		t.Fatalf("gc completion log line missing:\n%s", h.logs())
	}
}

// TestT94GCGateWithoutEngine pins the degraded-assembly branch: a stack
// assembled without the GC seam (rebuildWithDataDir wires no engine) gets
// an honest 503 from an authenticated admin, never a fake run.
func TestT94GCGateWithoutEngine(t *testing.T) {
	h := newHarness(t)
	s := h.rebuildWithDataDir(t, h.dataDir)
	req, err := http.NewRequest(http.MethodPost, s.ts.URL+"/binflow/api/v1/system/gc", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.SetBasicAuth(adminUser, adminPass)
	resp, err := s.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw := mustGet(t, resp)
	if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(raw, "not available") {
		t.Fatalf("status %d, want 503 (%s)", resp.StatusCode, raw)
	}
}
