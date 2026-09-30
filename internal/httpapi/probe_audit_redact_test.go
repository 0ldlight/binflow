package httpapi_test

// T-624 (BIN-108) — probe audit detail "url" userinfo redaction (T-617
// residual site 19: repositories_probe.go rendered the draft override — or
// the stored upstream URL — into the audit row verbatim). Negative-test hard
// gate: an upstream URL carrying userinfo (placeholder credentials
// ulogin-t624/FAKECRED-T624, never real ones) must not reach the audit detail
// in the clear, on BOTH arms — the draft body's url override and the
// stored-configuration fallback written through the real PUT face. Ruling
// (c): redact only, never reject or strip (userinfo-only URLs are a live
// authentication source).

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

const (
	t624User = "ulogin-t624"
	t624Pass = "FAKECRED-T624"
	// An unroutable loopback target: the probe answers the transport refusal
	// verdict (ok:false at 400) and STILL writes its audit row — the handler
	// records the event for every completed probe, pass or fail.
	t624RawURL    = "http://" + t624User + ":" + t624Pass + "@127.0.0.1:1"
	t624CleanHost = "http://127.0.0.1:1"
)

// auditDetailOf returns the single repository.remote.test audit row's detail.
func auditDetailOf(t *testing.T, h *harness) string {
	t.Helper()
	events, err := h.md.Audits().Query(context.Background(),
		metadata.AuditQuery{Action: "repository.remote.test", Limit: 10})
	if err != nil {
		t.Fatalf("query audit rows: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("audit rows = %d, want 1 (one probe, one row)", len(events))
	}
	return events[0].Detail
}

// assertNoFakeCred fails on any clear-text credential residue.
func assertNoFakeCred(t *testing.T, face, s string) {
	t.Helper()
	for _, leak := range []string{t624User, t624Pass} {
		if strings.Contains(s, leak) {
			t.Fatalf("%s leaks credential %q: %s", face, leak, s)
		}
	}
}

// TestProbeAuditRedactsDraftURLUserinfo walks the draft arm: the test body's
// url override carries userinfo straight into the audit target.
func TestProbeAuditRedactsDraftURLUserinfo(t *testing.T) {
	f := newProbeFixture(t)
	status, res := f.post(t, adminUser, adminPass, `{"url":"`+t624RawURL+`"}`)
	if status != http.StatusBadRequest || res.OK {
		t.Fatalf("userinfo draft probe = (%d, %+v), want the transport-refusal verdict", status, res)
	}
	assertNoFakeCred(t, "verdict message", res.Message)
	detail := auditDetailOf(t, f.harness)
	assertNoFakeCred(t, "audit detail", detail)
	if !strings.Contains(detail, `"url":"`+t624CleanHost+`"`) {
		t.Fatalf("audit detail = %s, want the redacted url %q", detail, t624CleanHost)
	}
}

// TestProbeAuditRedactsStoredURLUserinfo walks the stored arm the way the
// ticket spells it: PUT the remote repository WITH the userinfo URL through
// the real REST face, then probe with an empty body — the audit target falls
// back to the stored configuration's URL.
func TestProbeAuditRedactsStoredURLUserinfo(t *testing.T) {
	h := newHarness(t)
	resp := putRepo(t, h, "t624-store-r",
		`{"rclass":"remote","packageType":"generic","url":"`+t624RawURL+`","allowPrivateUpstream":true}`)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create remote with userinfo url = %d, body = %s", resp.StatusCode, body)
	}
	presp := h.do(http.MethodPost, "/binflow/api/repositories/t624-store-r/test",
		adminUser, adminPass, nil, nil)
	pbody := mustGet(t, presp)
	defer presp.Body.Close() //nolint:errcheck // drained by mustGet
	if presp.StatusCode != http.StatusBadRequest || strings.Contains(pbody, t624Pass) {
		t.Fatalf("stored-config probe = (%d, %s), want the transport-refusal verdict without credentials",
			presp.StatusCode, pbody)
	}
	detail := auditDetailOf(t, h)
	assertNoFakeCred(t, "audit detail", detail)
	if !strings.Contains(detail, `"url":"`+t624CleanHost+`"`) {
		t.Fatalf("audit detail = %s, want the redacted url %q", detail, t624CleanHost)
	}
}
