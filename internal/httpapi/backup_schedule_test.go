package httpapi_test

// The backup configuration plane's REST contract (M16 T-450, FR-150.3 /
// ADR-0044 decision 7②): the CRUD chain over the 022 payload table + the
// 021 ledger's domain='backup' rows — upsert-by-key both PUT spellings,
// the list/detail projections with the cron echo, the past nextBackupTime
// refusal, the single-state clear (payload survives, schedule row dies),
// the delete linkage (both rows die) and the capability gates.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

func backupBodyOf(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode backup body %s: %v", raw, err)
	}
	return body
}

func TestBackupScheduleCRUDChain(t *testing.T) {
	h := newHarness(t)
	exportDir := t.TempDir()

	// The fresh list is empty (nothing preseeded — the 022 posture).
	resp := h.do(http.MethodGet, "/binflow/api/v1/system/backups", "admin", adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET list = %d, want 200", resp.StatusCode)
	}
	list := backupBodyOf(t, resp)
	if backups, ok := list["backups"].([]any); !ok || len(backups) != 0 {
		t.Fatalf("fresh backups list = %v, want []", list)
	}

	// PUT (the body-key form, the official single-PUT shape): payload row
	// + schedule row land together, the answer carries the projections.
	resp = h.do(http.MethodPut, "/binflow/api/v1/system/backups", "admin", adminPass, []byte(`{
		"backupKey": "nightly",
		"cronExp": "0 0 2 ? * MON-FRI",
		"exportPath": "`+exportDir+`"
	}`), map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT nightly = %d (%s), want 200", resp.StatusCode, raw)
	}
	body := backupBodyOf(t, resp)
	if body["backupKey"] != "nightly" || body["cronExp"] != "0 0 2 ? * MON-FRI" || body["enabled"] != true {
		t.Fatalf("PUT body = %v, want the stored schedule", body)
	}
	if next, _ := body["nextScheduleBackup"].(string); next == "" {
		t.Error("nextScheduleBackup empty, want the computed trigger")
	}

	// The {key} GET and the list both read the same state.
	resp = h.do(http.MethodGet, "/binflow/api/v1/system/backups/nightly", "admin", adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET {key} = %d, want 200", resp.StatusCode)
	}
	body = backupBodyOf(t, resp)
	if body["exportPath"] != exportDir {
		t.Errorf("exportPath = %v, want %s", body["exportPath"], exportDir)
	}
	resp = h.do(http.MethodGet, "/binflow/api/v1/system/backups", "admin", adminPass, nil, nil)
	list = backupBodyOf(t, resp)
	if backups, _ := list["backups"].([]any); len(backups) != 1 {
		t.Fatalf("list after create = %v, want one row", list)
	}

	// The by-key PUT alias updates the payload.
	resp = h.do(http.MethodPut, "/binflow/api/v1/system/backups/nightly", "admin", adminPass,
		[]byte(`{"backupKey": "ignored-body-key", "cronExp": "0 0 2 ? * SAT", "exportPath": "`+exportDir+`"}`),
		map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT {key} = %d, want 200", resp.StatusCode)
	}
	body = backupBodyOf(t, resp)
	if body["backupKey"] != "nightly" || body["cronExp"] != "0 0 2 ? * SAT" {
		t.Errorf("by-key update = %v, want the path key and the new cron", body)
	}

	// The past nextBackupTime refusal (ADR-0044 decision 9②).
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	resp = h.do(http.MethodPut, "/binflow/api/v1/system/backups", "admin", adminPass,
		[]byte(`{"backupKey": "nightly", "cronExp": "0 0 2 ? * SAT", "nextBackupTime": "`+past+`", "exportPath": "`+exportDir+`"}`),
		map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("past nextBackupTime = %d, want 400", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(raw), "not in the future") {
		t.Errorf("past nextBackupTime body %q lacks the refusal", raw)
	}

	// A future nextBackupTime is accepted and stored verbatim.
	future := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
	resp = h.do(http.MethodPut, "/binflow/api/v1/system/backups", "admin", adminPass,
		[]byte(`{"backupKey": "nightly", "cronExp": "0 0 2 ? * SAT", "nextBackupTime": "`+future+`", "exportPath": "`+exportDir+`"}`),
		map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("future nextBackupTime = %d, want 200", resp.StatusCode)
	}
	body = backupBodyOf(t, resp)
	if body["nextScheduleBackup"] != future {
		t.Errorf("nextScheduleBackup = %v, want the explicit stamp %s", body["nextScheduleBackup"], future)
	}

	// The single-state clear: an empty cronExp keeps the payload row and
	// removes the schedule row.
	resp = h.do(http.MethodPut, "/binflow/api/v1/system/backups/nightly", "admin", adminPass,
		[]byte(`{"cronExp": "", "exportPath": "`+exportDir+`"}`),
		map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear cron = %d, want 200", resp.StatusCode)
	}
	body = backupBodyOf(t, resp)
	if body["cronExp"] != "" || body["nextScheduleBackup"] != "" || body["enabled"] != true {
		t.Errorf("cleared backup = %v, want payload alive / schedule gone", body)
	}
	if row, _ := h.md.Schedules().Get(context.Background(), "backup", "nightly"); row != nil {
		t.Errorf("schedule row survived the clear: %+v", row)
	}

	// The audit word: one backup.schedule.set per landed arm.
	events, err := h.md.Audits().Query(context.Background(), metadata.AuditQuery{Action: "backup.schedule.set", Limit: 10})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(events) < 4 {
		t.Fatalf("backup.schedule.set rows = %d, want >= 4 (create, update, explicit-next, clear)", len(events))
	}

	// DELETE drops BOTH rows; the second delete is the honest 404.
	resp = h.do(http.MethodDelete, "/binflow/api/v1/system/backups/nightly", "admin", adminPass, nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = h.do(http.MethodGet, "/binflow/api/v1/system/backups/nightly", "admin", adminPass, nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after DELETE = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = h.do(http.MethodDelete, "/binflow/api/v1/system/backups/nightly", "admin", adminPass, nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("second DELETE = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestBackupScheduleValidationAndGates(t *testing.T) {
	h := newHarness(t)
	seedLicenseUser(t, h.md, "roat", "roat-pw", "readonly_admin")
	dir := t.TempDir()

	cases := []struct {
		name string
		body string
		want string
	}{
		{"missing exportPath", `{"backupKey": "b1", "cronExp": "0 0 2 ? * MON"}`, "exportPath is required"},
		{"relative exportPath", `{"backupKey": "b1", "cronExp": "0 0 2 ? * MON", "exportPath": "backups"}`, "absolute server path"},
		{"dotdot exportPath", `{"backupKey": "b1", "cronExp": "0 0 2 ? * MON", "exportPath": "/var/../etc"}`, "'..'"},
		{"bad key charset", `{"backupKey": "nightly/evil", "cronExp": "0 0 2 ? * MON", "exportPath": "` + dir + `"}`, "backupKey"},
		{"invalid cron", `{"backupKey": "b1", "cronExp": "0 0 2 ? * MON-FRI-SAT", "exportPath": "` + dir + `"}`, "Invalid cronExp"},
	}
	for _, tc := range cases {
		resp := h.do(http.MethodPut, "/binflow/api/v1/system/backups", "admin", adminPass,
			[]byte(tc.body), map[string]string{"Content-Type": "application/json"})
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: PUT = %d, want 400", tc.name, resp.StatusCode)
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if !strings.Contains(string(raw), tc.want) {
			t.Errorf("%s: 400 body %q lacks %q", tc.name, raw, tc.want)
		}
	}

	// The gates: readonly_admin reads, never writes; anonymous 401s.
	resp := h.do(http.MethodGet, "/binflow/api/v1/system/backups", "roat", "roat-pw", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readonly list = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = h.do(http.MethodPut, "/binflow/api/v1/system/backups", "roat", "roat-pw",
		[]byte(`{"backupKey": "b1", "cronExp": "0 0 2 ? * MON", "exportPath": "`+dir+`"}`),
		map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("readonly PUT = %d, want 403", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = h.do(http.MethodDelete, "/binflow/api/v1/system/backups/b1", "roat", "roat-pw", nil, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("readonly DELETE = %d, want 403", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = h.do(http.MethodGet, "/binflow/api/v1/system/backups", "", "", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Nothing landed through the refused writes.
	rows, err := h.md.Backups().List(context.Background())
	if err != nil || len(rows) != 0 {
		t.Errorf("backups after refusals = %v (%v), want none", rows, err)
	}
}
