package httpapi_test

// T-93: the audit query plane (FR-29, GE-01/GE-02; W22/W22b/W23/W23b/W39).
// The endpoint is GET /binflow/api/v1/audit — admin only, envelope-shaped,
// keyset-paginated — and it is the ONLY verb routed under that path: the
// append-only rule (W39) is the absence of any write route.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
)

// t93Event decodes one events[] entry. Detail is a map[string]any on
// purpose: unmarshalling a non-object detail (the stored string form)
// fails the decode, so the "detail is an object" contract (GE-01) is
// asserted structurally on every read.
type t93Event struct {
	ID     int64          `json:"id"`
	Time   string         `json:"time"`
	Actor  string         `json:"actor"`
	Action string         `json:"action"`
	Repo   string         `json:"repo"`
	Path   string         `json:"path"`
	Detail map[string]any `json:"detail"`
}

// t93Page is the GE-01 envelope.
type t93Page struct {
	Events     []t93Event `json:"events"`
	NextCursor string     `json:"nextCursor"`
}

// t93GetAudit fetches the endpoint as admin and returns the status, the
// decoded page (on 200) and the raw body (for the W23b greps).
func t93GetAudit(t *testing.T, h *harness, query string) (int, t93Page, string) {
	t.Helper()
	if query != "" && !strings.HasPrefix(query, "?") {
		t.Fatalf("query %q must start with '?'", query)
	}
	resp := h.do(http.MethodGet, "/binflow/api/v1/audit"+query, adminUser, adminPass, nil, nil)
	raw := mustGet(t, resp)
	var page t93Page
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &page); err != nil {
			t.Fatalf("audit body is not the GE-01 envelope: %v\n%s", err, raw)
		}
	}
	return resp.StatusCode, page, raw
}

// t93Seed appends events through the same audit.Logger chain the server
// uses (Redact included), with explicit Times for deterministic windows.
func t93Seed(t *testing.T, h *harness, events ...audit.Event) {
	t.Helper()
	lg := audit.New(h.md, true)
	for _, e := range events {
		if err := lg.Append(context.Background(), e); err != nil {
			t.Fatalf("seed append %s/%s: %v", e.Time, e.Action, err)
		}
	}
}

// t93Fixture seeds six events with strictly ordered times; identity is the
// Time string.
func t93Fixture() []audit.Event {
	return []audit.Event{
		{Time: "2026-08-19T11:00:00Z", Actor: "jane", Action: audit.ActionDeploy, Repo: "generic-local", Path: "a.bin", Detail: `{"size":128}`},
		{Time: "2026-08-19T11:00:01Z", Actor: "ci-bot", Action: audit.ActionDelete, Repo: "generic-local", Path: "b.bin"},
		{Time: "2026-08-19T11:00:02Z", Actor: "jane", Action: audit.ActionLoginFail},
		{Time: "2026-08-19T11:00:03Z", Actor: "admin", Action: audit.ActionRepoCreate, Repo: "generic-local"},
		{Time: "2026-08-19T11:00:04Z", Actor: "admin", Action: audit.ActionGroupCreate},
		{Time: "2026-08-19T11:00:05Z", Actor: "ci-bot", Action: audit.ActionQuotaExceeded, Repo: "generic-local", Path: "big.bin"},
	}
}

// TestAuditQueryW22 is the query face (W22): shape, filters, closed-open
// window, limit bounds and the admin-only gate.
func TestAuditQueryW22(t *testing.T) {
	// jane is a real non-admin account: the 403 leg needs an authenticated
	// non-admin (a wrong password would be the 401 of a rejected
	// credential, a different plane of the matrix).
	h := newHarnessCfg(t, nil, [][2]string{{"jane", "jane-pw"}})
	t93Seed(t, h, t93Fixture()...)

	// Default query: 200, newest-first, full GE-01 field set.
	status, page, _ := t93GetAudit(t, h, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if len(page.Events) != 6 {
		t.Fatalf("events = %d, want 6", len(page.Events))
	}
	for i, e := range page.Events {
		if e.ID <= 0 {
			t.Fatalf("event %d has id %d, want a positive row id", i, e.ID)
		}
		if _, err := time.Parse(time.RFC3339, e.Time); err != nil {
			t.Fatalf("time %q is not RFC3339: %v", e.Time, err)
		}
		if e.Detail == nil {
			t.Fatalf("event %d detail is null, want an object", i)
		}
	}
	if page.Events[0].Time != "2026-08-19T11:00:05Z" {
		t.Fatalf("first event time = %s, want the newest (11:00:05)", page.Events[0].Time)
	}
	first := page.Events[5] // the deploy event carries a detail object
	if first.Detail["size"] != float64(128) {
		t.Fatalf("deploy detail = %v, want size:128 rendered as an object", first.Detail)
	}
	if first.Repo != "generic-local" || first.Path != "a.bin" || first.Actor != "jane" {
		t.Fatalf("deploy event projection = %+v", first)
	}

	// Filter matrix on the endpoint surface.
	status, byActor, _ := t93GetAudit(t, h, "?actor=jane")
	if status != http.StatusOK || len(byActor.Events) != 2 {
		t.Fatalf("actor=jane: %d events (%d), want 2", len(byActor.Events), status)
	}
	for _, e := range byActor.Events {
		if e.Actor != "jane" {
			t.Fatalf("actor filter leaked %q", e.Actor)
		}
	}
	_, byAction, _ := t93GetAudit(t, h, "?action=login.failed")
	if len(byAction.Events) < 1 {
		t.Fatalf("action=login.failed returned no events")
	}
	_, byRepo, _ := t93GetAudit(t, h, "?repo=generic-local")
	if len(byRepo.Events) != 4 {
		t.Fatalf("repo=generic-local = %d events, want 4", len(byRepo.Events))
	}

	// Closed-open window with bounds exactly ON fixture events: since
	// includes 03, until excludes 05.
	_, window, _ := t93GetAudit(t, h,
		"?since=2026-08-19T11:00:03Z&until=2026-08-19T11:00:05Z")
	if len(window.Events) != 2 ||
		window.Events[0].Time != "2026-08-19T11:00:04Z" ||
		window.Events[1].Time != "2026-08-19T11:00:03Z" {
		t.Fatalf("window = %v, want [04 03]", t93Times(window))
	}
	// An offset spelling of the same instant narrows identically (UTC
	// normalization, T-90 review note; %2B is the URL-encoded '+').
	_, offset, _ := t93GetAudit(t, h,
		"?since=2026-08-19T13:00:03%2B02:00&until=2026-08-19T11:00:05Z")
	if len(offset.Events) != len(window.Events) {
		t.Fatalf("offset since returned %d events, plain form %d", len(offset.Events), len(window.Events))
	}

	// Parameter validation: limit cap, non-numeric limit, bad timestamps,
	// foreign cursor — every one is the E-01 400 envelope.
	for _, q := range []string{
		"?limit=1001", "?limit=0", "?limit=-1", "?limit=abc",
		"?since=2026-08-19", "?since=yesterday",
		"?until=not-a-time",
		"?cursor=garbage",
	} {
		resp := h.do(http.MethodGet, "/binflow/api/v1/audit"+q, adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", q, resp.StatusCode)
		}
		eb := decodeError(t, resp)
		if eb.Errors[0].Status != http.StatusBadRequest {
			t.Fatalf("%s: envelope status = %d, want 400", q, eb.Errors[0].Status)
		}
	}
	// The cap itself is accepted.
	if status, _, _ := t93GetAudit(t, h, "?limit=1000"); status != http.StatusOK {
		t.Fatalf("limit=1000: status = %d, want 200", status)
	}

	// Admin-only gate: non-admin 403, anonymous 401 (challenge header on
	// the anonymous leg).
	resp := h.do(http.MethodGet, "/binflow/api/v1/audit", "jane", "jane-pw", nil, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, want 403", resp.StatusCode)
	}
	anon := h.do(http.MethodGet, "/binflow/api/v1/audit", "", "", nil, nil)
	if anon.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", anon.StatusCode)
	}
	if anon.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("anonymous 401 lacks the Basic challenge")
	}
	_ = mustGet(t, resp)
	_ = mustGet(t, anon)
}

func t93Times(p t93Page) []string {
	out := make([]string, 0, len(p.Events))
	for _, e := range p.Events {
		out = append(out, e.Time)
	}
	return out
}

// TestAuditPaginationW22b is FR-29-AC6: limit=2 plus two nextCursor follows
// — the three pages' union equals the unlimited query exactly (no gaps, no
// repeats), and the terminal page carries no cursor.
func TestAuditPaginationW22b(t *testing.T) {
	h := newHarness(t)
	t93Seed(t, h, t93Fixture()...)

	_, full, _ := t93GetAudit(t, h, "")
	if len(full.Events) != 6 {
		t.Fatalf("full query = %d events, want 6", len(full.Events))
	}

	var walked []t93Event
	cursor := ""
	pages := 0
	for {
		q := "?limit=2"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		status, page, _ := t93GetAudit(t, h, q)
		if status != http.StatusOK {
			t.Fatalf("page %d: status %d", pages+1, status)
		}
		walked = append(walked, page.Events...)
		pages++
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
		if pages > 6 {
			t.Fatalf("pagination did not terminate")
		}
	}
	if pages != 3 {
		t.Fatalf("pages = %d, want 3 (6 events at limit=2)", pages)
	}
	if len(walked) != len(full.Events) {
		t.Fatalf("walked %d events, full query has %d", len(walked), len(full.Events))
	}
	seen := map[int64]bool{}
	for i, e := range walked {
		if seen[e.ID] {
			t.Fatalf("event id %d repeated across pages", e.ID)
		}
		seen[e.ID] = true
		if e.ID != full.Events[i].ID {
			t.Fatalf("page walk diverges from full order at %d: %d vs %d", i, e.ID, full.Events[i].ID)
		}
	}
	// A cursor is URL-safe to echo back verbatim (the test did exactly that).
}

// TestAuditVocabularyW23 is GE-02: the PRD-named vocabulary actions are
// each queryable through the endpoint, and W23b — the full export greps
// clean for credential literals, a minted token plaintext and an
// Authorization header value (NFR-S3 redaction chain).
func TestAuditVocabularyW23(t *testing.T) {
	h := newHarness(t)

	// W23: deploy/delete/repo.create/group.create/gc.run/quota.exceeded
	// (the subset the PRD script names) each >= 1 and queryable. The
	// fixture covers deploy/delete/repo.create/group.create/quota.exceeded;
	// gc.run completes the set.
	t93Seed(t, h, append(t93Fixture(), audit.Event{
		Time: "2026-08-19T11:00:06Z", Actor: "admin", Action: audit.ActionGCRun,
	})...)
	for _, action := range []string{
		audit.ActionDeploy, audit.ActionDelete, audit.ActionRepoCreate,
		audit.ActionGroupCreate, audit.ActionGCRun, audit.ActionQuotaExceeded,
	} {
		status, page, _ := t93GetAudit(t, h, "?action="+action)
		if status != http.StatusOK || len(page.Events) < 1 {
			t.Fatalf("action=%s: %d events (%d), want >= 1", action, len(page.Events), status)
		}
		for _, e := range page.Events {
			if e.Action != action {
				t.Fatalf("action filter returned %q", e.Action)
			}
		}
	}

	// W23b: credentials riding an event's detail are redacted BEFORE
	// storage; the export surface renders only the masked forms.
	t93Seed(t, h, audit.Event{
		Time: "2026-08-19T11:00:07Z", Actor: "sloppy-op", Action: audit.ActionLoginFail,
		Detail: `{"password":"w23-literal-pw","Authorization":"Basic d232My1hdXRo","access_token":"w23-token-plaintext","note":"keep"}`,
	})
	// A freshly minted API token: its plaintext must never appear in the
	// export (it never enters any detail payload).
	mintResp := h.do(http.MethodPost, "/binflow/api/security/token",
		adminUser, adminPass, []byte("grant_type=client_credentials"), nil)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal([]byte(mustGet(t, mintResp)), &tok); err != nil || tok.AccessToken == "" {
		t.Fatalf("token mint failed: %v (%s)", err, tok.AccessToken)
	}

	status, _, raw := t93GetAudit(t, h, "?limit=1000")
	if status != http.StatusOK {
		t.Fatalf("full export status = %d", status)
	}
	for _, leak := range []string{
		"w23-literal-pw",
		"Basic d232My1hdXRo",
		"w23-token-plaintext",
		tok.AccessToken,
	} {
		if strings.Contains(raw, leak) {
			t.Fatalf("audit export leaks %q", leak)
		}
	}
	// Non-credential detail survives next to the masks (the "keep" note);
	// the exact indent shape is MarshalIndent's business, so match the
	// quoted token only.
	for _, masked := range []string{"[REDACTED]", `"keep"`} {
		if !strings.Contains(raw, masked) {
			t.Fatalf("audit export lost non-credential detail (%q missing)", masked)
		}
	}
}

// TestAuditAppendOnlyW39: no write surface exists under /api/v1/audit —
// PUT/DELETE/POST on the collection and on an item subpath all answer the
// E-26 404 (FR-29-AC4).
func TestAuditAppendOnlyW39(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPut, "/binflow/api/v1/audit"},
		{http.MethodDelete, "/binflow/api/v1/audit"},
		{http.MethodPost, "/binflow/api/v1/audit"},
		{http.MethodDelete, "/binflow/api/v1/audit/1"},
		{http.MethodPut, "/binflow/api/v1/audit/1"},
	} {
		resp := h.do(tc.method, tc.path, adminUser, adminPass, []byte("{}"), nil)
		body := decodeError(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s: status = %d, want 404", tc.method, tc.path, resp.StatusCode)
		}
		if !strings.Contains(body.Errors[0].Message, "not implemented") {
			t.Fatalf("%s %s: message = %q, want the E-26 wording", tc.method, tc.path, body.Errors[0].Message)
		}
	}
}
