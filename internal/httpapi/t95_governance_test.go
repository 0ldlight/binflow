package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/config"
)

// T-95: the governance plane over real HTTP (W12a/W26/W26b/W27, FR-24-AC4
// and FR-31/GE-05/GE-06) — the REST repository fields, the content-plane
// 409/404/413 renderings the adapters produce VERBATIM from the service's
// StatusErrors (zero adapter changes, the single-choke-point contract), and
// the usage endpoint with its admin-or-read gate.

// t95CreateRepo PUTs one repository configuration through /api/repositories.
func t95CreateRepo(t *testing.T, h *harness, key, body string) *http.Response {
	t.Helper()
	resp := h.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass, []byte(body), nil)
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		t.Fatalf("create repo %s: status %d (%s)", key, resp.StatusCode, mustGet(t, resp))
	}
	return resp
}

// t95Upload PUTs content through the content plane, returning the response
// (the caller owns the body).
func t95Upload(h *harness, repo, path, content string) *http.Response {
	return h.do(http.MethodPut, "/binflow/"+repo+"/"+path, adminUser, adminPass, []byte(content), nil)
}

// usageBodyT95 is the GE-06 response shape.
type usageBodyT95 struct {
	Repo       string `json:"repo"`
	UsedBytes  int64  `json:"usedBytes"`
	QuotaBytes int64  `json:"quotaBytes"`
}

// TestT95PatternsW12a walks the W12a script: the configured repository
// refuses a .txt upload (409 naming the include pattern) and a secret/**
// upload (409 naming the exclude), serves the allowed jar, and an
// unconfigured repository keeps the M1~M3 behavior.
func TestT95PatternsW12a(t *testing.T) {
	h := newHarness(t)
	defer h.resetLogs()

	t95CreateRepo(t, h, "pattern-local",
		`{"rclass":"local","packageType":"generic","includesPattern":"**/*.jar","excludesPattern":"secret/**"}`)

	// Upload outside includes: 409, message names the pattern.
	resp := t95Upload(h, "pattern-local", "a/t.txt", "x")
	if body := mustGet(t, resp); resp.StatusCode != http.StatusConflict || !strings.Contains(body, "**/*.jar") {
		t.Fatalf(".txt upload: status %d body %s, want 409 naming the pattern", resp.StatusCode, body)
	}
	// Upload inside excludes (include hit or not): 409 naming the exclude.
	resp = t95Upload(h, "pattern-local", "secret/t.jar", "x")
	if body := mustGet(t, resp); resp.StatusCode != http.StatusConflict || !strings.Contains(body, "secret/**") {
		t.Fatalf("excluded upload: status %d body %s, want 409 naming the exclude", resp.StatusCode, body)
	}
	// The allowed jar lands and reads back.
	if resp := t95Upload(h, "pattern-local", "t.jar", "j"); resp.StatusCode != http.StatusCreated {
		t.Fatalf("allowed jar upload: status %d (%s)", resp.StatusCode, mustGet(t, resp))
	}
	resp = h.do(http.MethodGet, "/binflow/pattern-local/t.jar", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK || mustGet(t, resp) != "j" {
		t.Fatalf("allowed jar download: status %d", resp.StatusCode)
	}

	// Configuration round-trip: the stored blob echoes both patterns (and
	// quotaBytes rides the same passthrough for the FE).
	resp = h.do(http.MethodGet, "/binflow/api/repositories/pattern-local", adminUser, adminPass, nil, nil)
	var cfg struct {
		Configuration map[string]any `json:"configuration"`
	}
	if err := json.Unmarshal([]byte(mustGet(t, resp)), &cfg); err != nil {
		t.Fatalf("repo config body is not JSON: %v", err)
	}
	if got := cfg.Configuration["includesPattern"]; got != "**/*.jar" {
		t.Errorf("configuration.includesPattern = %v, want **/*.jar", got)
	}
	if got := cfg.Configuration["excludesPattern"]; got != "secret/**" {
		t.Errorf("configuration.excludesPattern = %v, want secret/**", got)
	}

	// The regression leg: an unconfigured repository answers the same
	// upload with the historical 201.
	t95CreateRepo(t, h, "plain-local", `{"rclass":"local","packageType":"generic"}`)
	if resp := t95Upload(h, "plain-local", "a/t.txt", "x"); resp.StatusCode != http.StatusCreated {
		t.Fatalf("default repo .txt upload: status %d, want 201", resp.StatusCode)
	}
}

// TestT95QuotaW26W26bW27 walks the quota script: the 800/1024 pair, the 413
// with used/quota in the body, the atomic refusal (path 404, counter and
// blob ledger untouched), the usage endpoint's exact accounting, and the
// delete-frees-room fallback.
func TestT95QuotaW26W26bW27(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	t95CreateRepo(t, h, "tiny", `{"rclass":"local","packageType":"generic","quotaBytes":1024}`)
	body800 := strings.Repeat("a", 800)

	// FR-24-AC6/W26 precondition: quotaBytes round-trips through the stored
	// configuration (the FE form and the curl 单查 read this echo).
	resp := h.do(http.MethodGet, "/binflow/api/repositories/tiny", adminUser, adminPass, nil, nil)
	var cfgQ struct {
		Configuration map[string]any `json:"configuration"`
	}
	if err := json.Unmarshal([]byte(mustGet(t, resp)), &cfgQ); err != nil {
		t.Fatalf("repo config body is not JSON: %v", err)
	}
	if got := cfgQ.Configuration["quotaBytes"]; got != float64(1024) {
		t.Errorf("configuration.quotaBytes = %v (%T), want 1024", got, got)
	}

	if resp := t95Upload(h, "tiny", "a.bin", body800); resp.StatusCode != http.StatusCreated {
		t.Fatalf("first 800B: status %d (%s)", resp.StatusCode, mustGet(t, resp))
	}
	blobsBefore, err := h.md.Blobs().Count(ctx)
	if err != nil {
		t.Fatalf("blob count: %v", err)
	}

	// The second 800B: 413 with the quota wording and both values.
	resp = t95Upload(h, "tiny", "b.bin", body800)
	if respBody := mustGet(t, resp); resp.StatusCode != http.StatusRequestEntityTooLarge ||
		!strings.Contains(respBody, "quota exceeded") ||
		!strings.Contains(respBody, "800") || !strings.Contains(respBody, "1024") {
		t.Fatalf("over-quota upload: status %d body %s, want 413 with quota exceeded + used/quota", resp.StatusCode, respBody)
	}

	// Atomicity (W26b): refused path 404, usage exactly 800, blob ledger
	// unchanged (the unreferenced physical file is GC's business).
	if resp := h.do(http.MethodGet, "/binflow/tiny/b.bin", adminUser, adminPass, nil, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("refused path GET: status %d, want 404", resp.StatusCode)
	}
	u := t95Usage(t, h, "tiny")
	if u.UsedBytes != 800 || u.QuotaBytes != 1024 {
		t.Fatalf("usage = %+v, want used 800 quota 1024", u)
	}
	blobsAfter, err := h.md.Blobs().Count(ctx)
	if err != nil {
		t.Fatalf("blob count after refusal: %v", err)
	}
	if blobsAfter != blobsBefore {
		t.Fatalf("blob ledger moved on refusal: %d -> %d", blobsBefore, blobsAfter)
	}

	// W27: reads and deletes stay unlimited, the counter falls, the
	// re-upload succeeds.
	if resp := h.do(http.MethodGet, "/binflow/tiny/a.bin", adminUser, adminPass, nil, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("read at the ceiling: status %d", resp.StatusCode)
	}
	if resp := h.do(http.MethodDelete, "/binflow/tiny/a.bin", adminUser, adminPass, nil, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete at the ceiling: status %d, want 204", resp.StatusCode)
	}
	if u := t95Usage(t, h, "tiny"); u.UsedBytes != 0 {
		t.Fatalf("usage after delete = %d, want 0", u.UsedBytes)
	}
	if resp := t95Upload(h, "tiny", "b.bin", body800); resp.StatusCode != http.StatusCreated {
		t.Fatalf("re-upload after fallback: status %d", resp.StatusCode)
	}

	// The quota.exceeded audit trail through the real REST path (GE-02/W23
	// vocabulary arrival; the WARN log leg is asserted at the service layer
	// where the recorder and the log both fire).
	var saw bool
	rows, err := h.md.Audits().List(context.Background(), "tiny", "", 100)
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	for _, ev := range rows {
		if ev.Action == "quota.exceeded" && ev.Path == "b.bin" {
			saw = true
			if !strings.Contains(ev.Detail, `"used":800`) || !strings.Contains(ev.Detail, `"quota":1024`) {
				t.Errorf("quota audit detail = %s, want used/quota values", ev.Detail)
			}
		}
	}
	if !saw {
		t.Fatal("no quota.exceeded audit event through the REST path")
	}

	// quotaBytes 0 / absent: unlimited, the M1~M3 behavior verbatim.
	t95CreateRepo(t, h, "free", `{"rclass":"local","packageType":"generic","quotaBytes":0}`)
	for _, p := range []string{"a.bin", "b.bin"} {
		if resp := t95Upload(h, "free", p, body800); resp.StatusCode != http.StatusCreated {
			t.Fatalf("unlimited upload %s: status %d", p, resp.StatusCode)
		}
	}
	if u := t95Usage(t, h, "free"); u.QuotaBytes != 0 || u.UsedBytes != 1600 {
		t.Fatalf("unlimited usage = %+v, want quota 0 used 1600", u)
	}

	// Config validation: a negative quotaBytes is a 400 at config time.
	resp = h.do(http.MethodPut, "/binflow/api/repositories/neg", adminUser, adminPass,
		[]byte(`{"rclass":"local","packageType":"generic","quotaBytes":-1}`), nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("negative quotaBytes: status %d, want 400", resp.StatusCode)
	}
}

// TestT95UsageEndpointGate: the GE-06 access rule over the wire — admin
// passes, a read-granted user passes, an ungranted authenticated user gets
// 403, anonymous 401, an unknown repository 404, and non-GET verbs have no
// route (the E-26 404).
func TestT95UsageEndpointGate(t *testing.T) {
	h := newHarnessCfg(t, func(c *config.Config) { c.Security.AnonymousAccess = false },
		[][2]string{{"bob", "bob-pw"}})
	t95CreateRepo(t, h, "tiny", `{"rclass":"local","packageType":"generic","quotaBytes":512}`)
	if resp := t95Upload(h, "tiny", "a.bin", strings.Repeat("a", 100)); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed upload: status %d", resp.StatusCode)
	}

	// Anonymous: the route's authentication gate (401 challenge).
	resp := h.do(http.MethodGet, "/binflow/api/v1/storage/usage/tiny", "", "", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous usage: status %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Authenticated without the grant: 403 (not a challenge).
	resp = h.do(http.MethodGet, "/binflow/api/v1/storage/usage/tiny", "bob", "bob-pw", nil, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("ungranted usage: status %d, want 403", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Grant read on the repository root: 201 with the exact body.
	perm := `{"name":"tiny-read","repos":["tiny"],"includePatterns":["**"],"principals":{"users":{"bob":["read"]}}}`
	if resp := h.do(http.MethodPost, "/binflow/api/v1/permissions", adminUser, adminPass, []byte(perm), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("permission create: status %d (%s)", resp.StatusCode, mustGet(t, resp))
	}
	resp = h.do(http.MethodGet, "/binflow/api/v1/storage/usage/tiny", "bob", "bob-pw", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read-granted usage: status %d (%s)", resp.StatusCode, mustGet(t, resp))
	}
	var u usageBodyT95
	if err := json.Unmarshal([]byte(mustGet(t, resp)), &u); err != nil {
		t.Fatalf("usage body is not JSON: %v", err)
	}
	if u.Repo != "tiny" || u.UsedBytes != 100 || u.QuotaBytes != 512 {
		t.Fatalf("usage body = %+v, want {tiny 100 512}", u)
	}

	// Admin sees it too; unknown repository 404; other verbs unrouted.
	if u := t95Usage(t, h, "tiny"); u.UsedBytes != 100 || u.QuotaBytes != 512 {
		t.Fatalf("admin usage = %+v, want {tiny 100 512}", u)
	}
	resp = h.do(http.MethodGet, "/binflow/api/v1/storage/usage/no-such-repo", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown repo usage: status %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = h.do(http.MethodPost, "/binflow/api/v1/storage/usage/tiny", adminUser, adminPass, []byte("{}"), nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("POST usage: status %d, want the unrouted 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// ---- helpers ----

// t95Usage reads and decodes the usage endpoint as admin.
func t95Usage(t *testing.T, h *harness, repo string) usageBodyT95 {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/v1/storage/usage/"+repo, adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("usage(%s): status %d (%s)", repo, resp.StatusCode, mustGet(t, resp))
	}
	var u usageBodyT95
	if err := json.Unmarshal([]byte(mustGet(t, resp)), &u); err != nil {
		t.Fatalf("usage(%s) body is not JSON: %v", repo, err)
	}
	return u
}
