package httpapi_test

// system_logs_test.go pins the System Logs process-log tail face (M17
// T-493, FR-157③): the three capabilities (tail window / server-side
// substring filter / download arm), the parameter validation ladder, the
// capability gate (system:read — the audit read's posture), the E-26 family
// posture for foreign verbs, and the capture-through contract (a request's
// own access line lands in the tail).

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// logsPath is the endpoint under test.
const logsPath = "/binflow/api/v1/system/logs"

// seedReadOnlyAdmin creates a readonly_admin local account (the role that
// holds system:read without holding system:write — the gate's honest middle
// arm).
func seedReadOnlyAdmin(t *testing.T, h *harness, name, pass string) {
	t.Helper()
	hash, err := auth.HashPassword(pass)
	if err != nil {
		t.Fatalf("hash readonly admin password: %v", err)
	}
	if err := h.md.Users().Create(context.Background(), &metadata.User{
		Username: name, PasswordHash: hash, Role: "readonly_admin", Enabled: true,
	}); err != nil {
		t.Fatalf("seed readonly admin: %v", err)
	}
}

// getLogs fetches the JSON arm as (status, body).
func getLogs(t *testing.T, h *harness, query, user, pass string) (int, systemLogsBody) {
	t.Helper()
	path := logsPath
	if query != "" {
		path += "?" + query
	}
	resp := h.do(http.MethodGet, path, user, pass, nil, nil)
	defer drain(resp)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, systemLogsBody{}
	}
	var out systemLogsBody
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("logs body %q: %v", body, err)
	}
	return resp.StatusCode, out
}

// systemLogsBody mirrors the endpoint's JSON arm for the package-external
// decoder (kept local so the test asserts the WIRE shape, not the handler's
// struct).
type systemLogsBody struct {
	Lines       []string `json:"lines"`
	Count       int      `json:"count"`
	Held        int      `json:"held"`
	Capacity    int      `json:"capacity"`
	Truncated   bool     `json:"truncated"`
	GeneratedAt string   `json:"generatedAt"`
}

func TestSystemLogsGate(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"qa-bot", "qa-pw"}})
	seedReadOnlyAdmin(t, h, "ro-admin", "ro-pw")

	// Anonymous meets the 401 challenge (the management plane's posture).
	resp := h.do(http.MethodGet, logsPath, "", "", nil, nil)
	defer drain(resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", resp.StatusCode)
	}
	if chal := resp.Header.Get("WWW-Authenticate"); !strings.HasPrefix(chal, `Basic realm="`) {
		t.Fatalf("WWW-Authenticate = %q, want the Basic challenge", chal)
	}

	// A plain user 403s (no system:read).
	if st, _ := getLogs(t, h, "", "qa-bot", "qa-pw"); st != http.StatusForbidden {
		t.Fatalf("plain user status = %d, want 403", st)
	}

	// readonly_admin reads (the audit read's capability posture).
	if st, _ := getLogs(t, h, "", "ro-admin", "ro-pw"); st != http.StatusOK {
		t.Fatalf("readonly admin status = %d, want 200", st)
	}

	// admin reads.
	if st, _ := getLogs(t, h, "", adminUser, adminPass); st != http.StatusOK {
		t.Fatalf("admin status = %d, want 200", st)
	}
}

func TestSystemLogsTailFilterAndValidation(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	// Drive real traffic so the ring holds real process lines: five uploads
	// (five access lines, each naming its path) after the harness boot.
	for i := 0; i < 5; i++ {
		putContent(t, h, "/binflow/generic-local/tail/piece-"+string(rune('a'+i))+".bin", "x")
	}

	// Default window: non-empty, ordered oldest→newest, count consistent.
	st, body := getLogs(t, h, "", adminUser, adminPass)
	if st != http.StatusOK {
		t.Fatalf("status = %d", st)
	}
	if body.Count != len(body.Lines) || body.Count == 0 {
		t.Fatalf("count=%d len(lines)=%d — an empty or inconsistent tail", body.Count, len(body.Lines))
	}
	if body.Capacity == 0 || body.GeneratedAt == "" {
		t.Fatalf("capacity/generatedAt not echoed: %+v", body)
	}

	// Server-side filter: the tail of the MATCHING stream only, and the
	// window cut applies to matches (limit=1 keeps the newest).
	st, body = getLogs(t, h, "filter=tail/piece-c&limit=10", adminUser, adminPass)
	if st != http.StatusOK || body.Count == 0 {
		t.Fatalf("filtered status=%d count=%d", st, body.Count)
	}
	for _, l := range body.Lines {
		if !strings.Contains(l, "tail/piece-c") {
			t.Fatalf("unfiltered line in the window: %q", l)
		}
	}
	_, narrow := getLogs(t, h, "filter=tail/piece&limit=1", adminUser, adminPass)
	if narrow.Count != 1 || !strings.Contains(narrow.Lines[0], "tail/") {
		t.Fatalf("limit-1 window = %v, want exactly the newest match", narrow.Lines)
	}
	// No match is an honest empty array, not an error.
	_, none := getLogs(t, h, "filter=definitely-not-a-line-xyz", adminUser, adminPass)
	if none.Count != 0 || none.Lines == nil {
		t.Fatalf("no-match body = %+v, want lines: []", none)
	}

	// Validation ladder: limit outside 1..1000 and filter over 256 chars.
	for _, tc := range []struct{ name, query string }{
		{"limit zero", "limit=0"},
		{"limit negative", "limit=-5"},
		{"limit over max", "limit=1001"},
		{"limit not a number", "limit=abc"},
		{"filter too long", "filter=" + strings.Repeat("x", 257)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.do(http.MethodGet, logsPath+"?"+tc.query, adminUser, adminPass, nil, nil)
			defer drain(resp)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			decodeError(t, resp)
		})
	}

	// The full limit range is legal at both edges.
	for _, edge := range []string{"limit=1", "limit=1000"} {
		if st, _ := getLogs(t, h, edge, adminUser, adminPass); st != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", edge, st)
		}
	}
}

func TestSystemLogsDownloadArm(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/dl/app.bin", "x")

	resp := h.do(http.MethodGet, logsPath+"?filter=dl/app.bin&download=1", adminUser, adminPass, nil, nil)
	defer drain(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body=%s", resp.StatusCode, mustGet(t, resp))
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="binflow-service.log"` {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q", ct)
	}
	body := mustGet(t, resp)
	if !strings.Contains(body, "dl/app.bin") {
		t.Fatalf("download body missing the filtered line: %q", body)
	}
	if strings.Contains(body, `"lines"`) {
		t.Fatalf("download arm answered the JSON shape: %q", body)
	}
	// Every downloaded line matches the filter (the window IS the download).
	for _, l := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		if l != "" && !strings.Contains(l, "dl/app.bin") {
			t.Fatalf("unfiltered line in the download: %q", l)
		}
	}
}

func TestSystemLogsCaptureThroughAccessLine(t *testing.T) {
	h := newHarness(t)

	// One distinctive request BEFORE the read: its access line must be in
	// the tail — the endpoint serves the process's own stream, captured
	// from the assembled logger onward.
	probe := h.do(http.MethodGet, "/binflow/api/system/ping", "", "", nil, nil)
	defer drain(probe)
	if probe.StatusCode != http.StatusOK {
		t.Fatalf("ping status = %d", probe.StatusCode)
	}

	st, body := getLogs(t, h, "filter=access", adminUser, adminPass)
	if st != http.StatusOK {
		t.Fatalf("status = %d", st)
	}
	found := false
	for _, l := range body.Lines {
		if strings.Contains(l, "/binflow/api/system/ping") && strings.Contains(l, "status=200") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the ping request's access line never reached the tail; lines=%v", body.Lines)
	}
}

func TestSystemLogsE26ForeignVerbs(t *testing.T) {
	h := newHarness(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		resp := h.do(method, logsPath, adminUser, adminPass, nil, nil)
		eb := decodeError(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status = %d, want the E-26 404", method, resp.StatusCode)
		}
		if !strings.Contains(eb.Errors[0].Message, "not implemented") {
			t.Fatalf("%s message = %q", method, eb.Errors[0].Message)
		}
	}
	// Sub-paths have no routes either.
	resp := h.do(http.MethodGet, logsPath+"/tail", adminUser, adminPass, nil, nil)
	defer drain(resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("sub-path status = %d, want 404", resp.StatusCode)
	}
}
