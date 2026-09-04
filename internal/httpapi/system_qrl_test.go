package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metrics"
	"github.com/lzwzzy/binflow/internal/search"
)

// metricValue reads one bare gauge sample out of a Prometheus text body.
func metricValue(t *testing.T, body, line string) float64 {
	t.Helper()
	for _, l := range strings.Split(body, "\n") {
		if !strings.HasPrefix(l, line+" ") {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(l, line)), 64)
		if err != nil {
			t.Fatalf("metric line %q: %v", l, err)
		}
		return v
	}
	t.Fatalf("metric line %q not found in:\n%s", line, body)
	return 0
}

// T-452 (FR-148.2 / aql.md §14.4): the QRL admin REST face over the real
// stack — the three operations' verbatim copies, the disabled-400 factory
// arm, the tri-state carrier, the admin door, the K72 readout/gate
// consistency, and the metrics sampling presentation.

// qrlDo issues one QRL config request and decodes status + body.
func qrlDo(t *testing.T, h *harness, method, user, pass string, body []byte) (int, string, string) {
	t.Helper()
	resp := h.do(method, "/binflow/api/v1/system/query_rate_limiter/config", user, pass, body, nil)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, mustGet(t, resp), resp.Header.Get("Content-Type")
}

// qrlEnable flips the limiter through the REST carrier.
func qrlEnable(t *testing.T, h *harness, mode string, settings string) {
	t.Helper()
	body := []byte(`{"mode":"` + mode + `"`)
	if settings != "" {
		body = []byte(`{"mode":"` + mode + `","rlSettings":[` + settings + `]}`)
	}
	status, respBody, _ := qrlDo(t, h, http.MethodPost, adminUser, adminPass, body)
	if status != http.StatusOK {
		t.Fatalf("enable %s = %d %s", mode, status, respBody)
	}
}

// TestQRLRESTThreeOpsAndFactoryArm pins the face's wire (§14.4): the
// factory disabled 400 (verbatim, plain text) on all three verbs, the
// enable carrier, the GET readout's two-type shape, the merge-write, the
// DELETE reset, and the validation 400s.
func TestQRLRESTThreeOpsAndFactoryArm(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"plain", "plain-pw"}})

	// The factory state: every operation answers the verbatim plain-text
	// 400 (the anchor's disabled posture).
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		status, body, ctype := qrlDo(t, h, method, adminUser, adminPass, nil)
		if status != http.StatusBadRequest || body != "Query rate limiter is disabled" {
			t.Fatalf("%s factory = %d %q, want the verbatim 400 copy", method, status, body)
		}
		if !strings.HasPrefix(ctype, "text/plain") {
			t.Fatalf("%s factory content-type = %q, want text/plain", method, ctype)
		}
	}

	// A POST without a mode on the disabled limiter keeps the 400 arm.
	status, body, _ := qrlDo(t, h, http.MethodPost, adminUser, adminPass,
		[]byte(`{"rlSettings":[{"rlType":"DEFAULT","permitsPerTimeFrame":9,"timeFrameMillis":1000,"timeQuota":1000}]}`))
	if status != http.StatusBadRequest || body != "Query rate limiter is disabled" {
		t.Fatalf("modeless POST on disabled = %d %q", status, body)
	}

	// The enable carrier: POST with mode=enabled answers the updated copy.
	status, body, ctype := qrlDo(t, h, http.MethodPost, adminUser, adminPass,
		[]byte(`{"mode":"enabled"}`))
	if status != http.StatusOK || body != "Query rate limiter configuration was updated successfully" {
		t.Fatalf("enable = %d %q", status, body)
	}
	if !strings.HasPrefix(ctype, "text/plain") {
		t.Fatalf("enable content-type = %q", ctype)
	}

	// GET answers the two-type readout whose numbers ARE the K63 trio
	// (K72: the REST face can never disagree with the enforced gate).
	k := search.K63Gate()
	status, body, ctype = qrlDo(t, h, http.MethodGet, adminUser, adminPass, nil)
	if status != http.StatusOK || !strings.HasPrefix(ctype, "application/json") {
		t.Fatalf("enabled GET = %d %q %s", status, body, ctype)
	}
	var readout struct {
		RLSettings []search.QRLSetting `json:"rlSettings"`
	}
	if err := json.Unmarshal([]byte(body), &readout); err != nil {
		t.Fatalf("readout %q: %v", body, err)
	}
	if len(readout.RLSettings) != 2 {
		t.Fatalf("readout = %s, want the two-type shape", body)
	}
	for i, want := range []search.QRLSetting{
		{RLType: "DEFAULT", PermitsPerTimeFrame: k.Concurrency, TimeFrameMillis: k.TimeoutMillis, TimeQuota: k.RowCap},
		{RLType: "LOW_PRIORITY", PermitsPerTimeFrame: k.Concurrency, TimeFrameMillis: k.TimeoutMillis, TimeQuota: k.RowCap},
	} {
		if readout.RLSettings[i] != want {
			t.Fatalf("readout[%d] = %+v, want the K63 trio %+v (K72)", i, readout.RLSettings[i], want)
		}
	}

	// The merge-write: one bucket narrowed, the other untouched.
	status, body, _ = qrlDo(t, h, http.MethodPost, adminUser, adminPass,
		[]byte(`{"rlSettings":[{"rlType":"DEFAULT","permitsPerTimeFrame":7,"timeFrameMillis":5000,"timeQuota":500}]}`))
	if status != http.StatusOK || body != "Query rate limiter configuration was updated successfully" {
		t.Fatalf("merge = %d %q", status, body)
	}
	status, body, _ = qrlDo(t, h, http.MethodGet, adminUser, adminPass, nil)
	if status != http.StatusOK {
		t.Fatalf("merged readout status = %d body=%s", status, body)
	}
	readout = struct {
		RLSettings []search.QRLSetting `json:"rlSettings"`
	}{}
	if err := json.Unmarshal([]byte(body), &readout); err != nil {
		t.Fatalf("merged readout %q: %v", body, err)
	}
	wantMerged := []search.QRLSetting{
		{RLType: "DEFAULT", PermitsPerTimeFrame: 7, TimeFrameMillis: 5000, TimeQuota: 500},
		{RLType: "LOW_PRIORITY", PermitsPerTimeFrame: k.Concurrency, TimeFrameMillis: k.TimeoutMillis, TimeQuota: k.RowCap},
	}
	if len(readout.RLSettings) != 2 {
		t.Fatalf("merged readout = %s, want the two-type shape", body)
	}
	for i := range wantMerged {
		if readout.RLSettings[i] != wantMerged[i] {
			t.Fatalf("merged readout[%d] = %+v, want %+v (the merge keeps the untouched bucket)", i, readout.RLSettings[i], wantMerged[i])
		}
	}

	// Validation 400s: the errors[] envelope (the management-plane posture).
	for _, bad := range []string{
		`{"mode":"paused"}`,
		`{"rlSettings":[{"rlType":"SYSTEM","permitsPerTimeFrame":1,"timeFrameMillis":1,"timeQuota":1}]}`,
		`{"rlSettings":[{"rlType":"DEFAULT","permitsPerTimeFrame":0,"timeFrameMillis":1000,"timeQuota":1000}]}`,
		`not json`,
	} {
		status, body, ctype = qrlDo(t, h, http.MethodPost, adminUser, adminPass, []byte(bad))
		if status != http.StatusBadRequest || !strings.HasPrefix(ctype, "application/json") {
			t.Fatalf("bad POST %q = %d %q %s, want the envelope 400", bad, status, body, ctype)
		}
		if !strings.Contains(body, `"errors"`) {
			t.Fatalf("bad POST %q body = %s, want the errors[] envelope", bad, body)
		}
	}

	// DELETE: the reset copy, then the factory 400 arm again.
	status, body, _ = qrlDo(t, h, http.MethodDelete, adminUser, adminPass, nil)
	if status != http.StatusOK || body != "Query rate limiter configuration was deleted successfully" {
		t.Fatalf("delete = %d %q", status, body)
	}
	if status, body, _ = qrlDo(t, h, http.MethodGet, adminUser, adminPass, nil); status != http.StatusBadRequest {
		t.Fatalf("GET after reset = %d %s, want the disabled 400", status, body)
	}

	// The admin door: a plain user meets 403 on all three; anonymous meets
	// the 401 challenge.
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		resp := h.do(method, "/binflow/api/v1/system/query_rate_limiter/config", "plain", "plain-pw", nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("plain user %s = %d, want 403 (the admin door)", method, resp.StatusCode)
		}
		resp = h.do(method, "/binflow/api/v1/system/query_rate_limiter/config", "", "", nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s = %d, want 401", method, resp.StatusCode)
		}
	}

	// Foreign verbs keep the E-26 404.
	resp := h.do(http.MethodPut, "/binflow/api/v1/system/query_rate_limiter/config", adminUser, adminPass, nil, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("PUT = %d, want the E-26 404", resp.StatusCode)
	}
}

// TestQRLGateConsistencyOnLiveQueries is K72's behavior half over the real
// stack: the limiter ACTIVE (simulation — the observation mode never
// delays) changes nothing about the query planes' answers — the K63 gate,
// deadline and row cap govern exactly as before, and the mode flips are
// visible on the face.
func TestQRLGateConsistencyOnLiveQueries(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	deposit(t, h, "generic-local", "acme/artifact.bin", "qrl-bytes")

	for _, mode := range []string{"enabled", "simulation"} {
		qrlEnable(t, h, mode, `{"rlType":"DEFAULT","permitsPerTimeFrame":100,"timeFrameMillis":10000,"timeQuota":1000000}`)
		// AQL answers exactly as the disabled factory would.
		status, body := aqlDo(t, h, `items.find({"repo":"generic-local"}).include("name")`, adminUser, adminPass, "")
		if status != http.StatusOK || !strings.Contains(body, "artifact.bin") {
			t.Fatalf("aql under %s = %d %s — the rate plane must not bend the query answers", mode, status, body)
		}
		// The legacy plane too.
		status, body = t452DatesDo(t, h, "creation?from=0", adminUser, adminPass)
		if status != http.StatusOK {
			t.Fatalf("creation under %s = %d %s", mode, status, body)
		}
	}
	// Back to the factory state for the rest of the process.
	qrlDo(t, h, http.MethodDelete, adminUser, adminPass, nil)
}

// TestQRLMetricsSampling pins the metrics presentation (§14.4's 指标 job,
// mapped onto scrape-time sampling): the mode gauge, the window counters
// one scrape reads after real queries, and the throttle INFO line with the
// anchor's copy when a window saw slowdown.
func TestQRLMetricsSampling(t *testing.T) {
	h := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		d.Metrics = metrics.NewRegistry()
	}, nil)
	seedRepo(t, h, "generic-local")
	deposit(t, h, "generic-local", "acme/artifact.bin", "qrl-metrics-bytes")

	// Factory state: the mode gauge reads the disabled ordinal and nothing
	// was sampled.
	m := h.do(http.MethodGet, "/metrics", "", "", nil, nil)
	body := mustGet(t, m)
	_ = m.Body.Close()
	if !strings.Contains(body, "binflow_qrl_mode 0") {
		t.Fatalf("factory metrics missing the disabled mode gauge:\n%s", body)
	}

	// Simulation with a one-permit budget: the first query takes the
	// permit, the second is recorded as would-be-throttled — both admitted.
	qrlEnable(t, h, "simulation", `{"rlType":"DEFAULT","permitsPerTimeFrame":1,"timeFrameMillis":60000,"timeQuota":60000}`)
	for i := 0; i < 2; i++ {
		status, respBody := aqlDo(t, h, `items.find({"repo":"generic-local"}).include("name")`, adminUser, adminPass, "")
		if status != http.StatusOK {
			t.Fatalf("aql %d under simulation = %d %s", i, status, respBody)
		}
	}

	h.resetLogs()
	m = h.do(http.MethodGet, "/metrics", "", "", nil, nil)
	body = mustGet(t, m)
	_ = m.Body.Close()
	for _, want := range []string{
		"binflow_qrl_mode 2", // simulation ordinal
		`binflow_qrl_window_queries{type="DEFAULT"} 2`,
		`binflow_qrl_window_permits{type="DEFAULT"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("sampled metrics missing %q\nbody:\n%s", want, body)
		}
	}
	// The slowdown gauge: the second query's would-be delay is the rest of
	// the 60s frame (a few hundred ms short of the full frame — the two
	// HTTP round trips age the window), so assert a large-but-sub-frame
	// value rather than the exact constant.
	slowed := metricValue(t, body, `binflow_qrl_slowed_down_millis{type="DEFAULT"}`)
	if slowed < 50000 || slowed > 60000 {
		t.Fatalf("slowed_down_millis = %f, want the near-full frame remainder", slowed)
	}
	// The throttle INFO line: the anchor's copy with the placeholders
	// filled (1 permit per 60000 ms frame, some % of the time).
	if !strings.Contains(h.logs(), "Throttling has been applied (") ||
		!strings.Contains(h.logs(), "reached the set limit (1 per 60000 ms)") {
		t.Fatalf("throttle INFO line missing from the scrape log:\n%s", h.logs())
	}

	// The window reset: the next scrape samples a fresh (empty) window.
	m = h.do(http.MethodGet, "/metrics", "", "", nil, nil)
	body = mustGet(t, m)
	_ = m.Body.Close()
	if !strings.Contains(body, `binflow_qrl_window_queries{type="DEFAULT"} 0`) {
		t.Fatalf("second scrape did not reset the window:\n%s", body)
	}
}
