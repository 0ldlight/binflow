package httpapi_test

// T-442 (FR-143.5) — the remote repository Test endpoint's REST wire:
// POST /api/repositories/{key}/test over the standard harness, against a
// loopback upstream. The verdict body (ok at 200, ok:false + the inline
// reason at 400), the draft-override arms, the 404/400/403 ladder, the
// audit row, and the probe's one-read-only-GET footprint.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// probeFixture is the harness plus a counting loopback upstream and a
// remote repository row pointing at it.
type probeFixture struct {
	*harness
	up       *httptest.Server
	upHits   *atomic.Int64
	authMode *atomic.Bool
}

// newProbeFixture assembles the stack. The upstream serves "/" at 200 and
// can be flipped into a Basic-auth gate for the refused arm.
func newProbeFixture(t *testing.T) *probeFixture {
	t.Helper()
	hits := &atomic.Int64{}
	authMode := &atomic.Bool{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if authMode.Load() {
			u, p, ok := r.BasicAuth()
			if !ok || u != "u" || p != "p" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, "bad credentials")
				return
			}
		}
		_, _ = io.WriteString(w, "upstream root")
	}))
	t.Cleanup(up.Close)

	h := newHarnessCfg(t, nil, [][2]string{{"dev", "dev-pw"}})
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := h.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "helm-r", Type: "remote", PackageType: "helm",
		Config: `{"url":"` + up.URL + `","username":"","retrievalCachePeriodSecs":7200,` +
			`"missedRetrievalCachePeriodSecs":1800,"socketTimeoutSecs":5,` +
			`"assumedOfflinePeriodSecs":300,"hardFail":false,"allowPrivateUpstream":true}`,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed remote repo: %v", err)
	}
	if err := h.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "libs", Type: "local", PackageType: "generic",
		Config: "{}", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed local repo: %v", err)
	}
	if err := h.md.Remote().CreateConfig(ctx, &metadata.RemoteConfig{
		RepoKey: "helm-r", URL: up.URL,
		ContentTTLSeconds: 7200, MetadataTTLSeconds: 600,
		AllowPrivateUpstream: true, // loopback upstream, the fixture posture
	}); err != nil {
		t.Fatalf("seed remote config: %v", err)
	}
	return &probeFixture{harness: h, up: up, upHits: hits, authMode: authMode}
}

// probeResponse is the verdict body.
type probeResponse struct {
	OK         bool   `json:"ok"`
	StatusCode int    `json:"status_code"`
	Message    string `json:"message"`
}

// post runs one test call and decodes the verdict body.
func (f *probeFixture) post(t *testing.T, user, pass, body string) (int, probeResponse) {
	t.Helper()
	var raw []byte
	if body != "" {
		raw = []byte(body)
	}
	resp := f.do(http.MethodPost, "/binflow/api/repositories/helm-r/test", user, pass, raw, nil)
	defer resp.Body.Close() //nolint:errcheck // drained below
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var out probeResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode verdict (%d, %q): %v", resp.StatusCode, data, err)
	}
	return resp.StatusCode, out
}

// TestRepositoryTestEndpointVerdicts walks the pass/refused/unreachable
// verdicts and the probe's upstream footprint.
func TestRepositoryTestEndpointVerdicts(t *testing.T) {
	f := newProbeFixture(t)

	// Stored-config probe: pass at 200 with the success wording.
	status, res := f.post(t, adminUser, adminPass, "")
	if status != http.StatusOK || !res.OK || res.StatusCode != http.StatusOK ||
		!strings.Contains(res.Message, "tested successfully") {
		t.Fatalf("stored-config probe = (%d, %+v), want 200/ok/success", status, res)
	}
	if got := f.upHits.Load(); got != 1 {
		t.Fatalf("upstream contacts = %d, want 1 (one read-only GET)", got)
	}

	// Draft credentials against the gated upstream: wrong pair fails
	// inline at 400 with the upstream status in the reason.
	f.authMode.Store(true)
	status, res = f.post(t, adminUser, adminPass,
		`{"url":"`+f.up.URL+`","username":"u","password":"wrong"}`)
	if status != http.StatusBadRequest || res.OK ||
		!strings.Contains(res.Message, "returned error 401") {
		t.Fatalf("wrong-credential probe = (%d, %+v), want 400/401 inline", status, res)
	}
	// The correct pair passes before anything is saved (the create form's
	// test-before-save arm).
	status, res = f.post(t, adminUser, adminPass,
		`{"url":"`+f.up.URL+`","username":"u","password":"p"}`)
	if status != http.StatusOK || !res.OK {
		t.Fatalf("draft-pair probe = (%d, %+v), want 200/ok", status, res)
	}

	// Unreachable draft URL: the transport family, still the verdict body.
	status, res = f.post(t, adminUser, adminPass, `{"url":"http://127.0.0.1:1"}`)
	if status != http.StatusBadRequest || res.OK ||
		!strings.Contains(res.Message, "connection failed") {
		t.Fatalf("unreachable probe = (%d, %+v), want 400/transport", status, res)
	}
	if got := f.upHits.Load(); got != 3 {
		t.Fatalf("upstream contacts = %d, want 3 (one GET per probing verdict)", got)
	}
}

// TestRepositoryTestEndpointLadder walks the face faults: unknown repo 404,
// wrong class 400, malformed body 400, the manage gate's 403 and the
// anonymous 401 — plus the audit row.
func TestRepositoryTestEndpointLadder(t *testing.T) {
	f := newProbeFixture(t)

	resp := f.do(http.MethodPost, "/binflow/api/repositories/no-such-repo/test",
		adminUser, adminPass, nil, nil)
	resp.Body.Close() //nolint:errcheck // status-only probe
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown repo = %d, want 404", resp.StatusCode)
	}

	resp = f.do(http.MethodPost, "/binflow/api/repositories/libs/test",
		adminUser, adminPass, nil, nil)
	resp.Body.Close() //nolint:errcheck // status-only probe
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("local repo = %d, want 400", resp.StatusCode)
	}

	if status, _ := f.post(t, adminUser, adminPass, "{not json"); status != http.StatusBadRequest {
		t.Fatalf("malformed body = %d, want 400", status)
	}

	// The route gate: a plain user cannot aim the stored credential at
	// upstreams; anonymous is challenged.
	resp = f.do(http.MethodPost, "/binflow/api/repositories/helm-r/test",
		"dev", "dev-pw", nil, nil)
	resp.Body.Close() //nolint:errcheck // status-only probe
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin = %d, want 403", resp.StatusCode)
	}
	resp = f.do(http.MethodPost, "/binflow/api/repositories/helm-r/test", "", "", nil, nil)
	resp.Body.Close() //nolint:errcheck // status-only probe
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d, want 401", resp.StatusCode)
	}
	if got := f.upHits.Load(); got != 0 {
		t.Fatalf("upstream contacts across the ladder = %d, want 0", got)
	}

	// One passing probe leaves the audit row (actor, repo, url, verdict).
	if status, res := f.post(t, adminUser, adminPass, ""); status != http.StatusOK || !res.OK {
		t.Fatalf("audit-leg probe = (%d, %+v)", status, res)
	}
	events, err := f.md.Audits().Query(context.Background(),
		metadata.AuditQuery{Action: "repository.remote.test", Limit: 10})
	if err != nil || len(events) != 1 {
		t.Fatalf("audit rows = %d (%v), want 1", len(events), err)
	}
	if events[0].Actor != adminUser || !strings.Contains(events[0].Detail, `"repo":"helm-r"`) {
		t.Errorf("audit row = actor %q detail %q, want admin and the repo", events[0].Actor, events[0].Detail)
	}
}
