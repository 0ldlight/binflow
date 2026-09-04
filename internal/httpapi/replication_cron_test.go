package httpapi_test

// The replication config plane's cron arm (M16 T-450, FR-150.4 /
// replication.md §2.1 cronExp — the M15 Q5 reversal's landing): the
// create/update/delete wiring of the schedules ledger row behind the
// family's existing CRUD, the single-state clear, the enabled-switch mirror
// (one flip parks the event track AND the schedule), the delete linkage and
// the anchor's 400 family (cron-scheduling.md §3 — "Invalid cronExp"; the
// "cronExp is required" arm never fires on BinFlow's optional-cron face, a
// registered intentional difference).

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

func replCronCreate(t *testing.T, h *replHarness, name, cron string) (int, map[string]any) {
	t.Helper()
	body := `{"name": "` + name + `", "source_repo": "libs-release", "target_url": "http://target.example", "target_repo": "replica"`
	if cron != "" {
		body += `, "cron_exp": "` + cron + `"`
	}
	body += "}"
	resp, raw := h.do(http.MethodPost, "/binflow/api/v1/replications", "admin", "password", body)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("create %s decode: %v (%s)", name, err, raw)
	}
	return resp.StatusCode, parsed
}

func TestReplicationConfigCronFieldLifecycle(t *testing.T) {
	h := newReplHarness(t, false)
	ctx := context.Background()

	// Create WITH a cron: the row lands in the same request, the response
	// echoes the arm with the next-run projection.
	code, created := replCronCreate(t, h, "cronned", "0 0/30 * * * ?")
	if code != http.StatusCreated {
		t.Fatalf("create with cron = %d (%v), want 201", code, created)
	}
	if created["cron_exp"] != "0 0/30 * * * ?" {
		t.Errorf("create echo cron_exp = %v, want the expression", created["cron_exp"])
	}
	if next, _ := created["next_schedule_sync"].(string); next == "" {
		t.Error("create echo next_schedule_sync empty, want the computed trigger")
	}
	id := int64(created["id"].(float64))
	row, err := h.md.Schedules().Get(ctx, "replication", "1")
	if err != nil || row == nil {
		t.Fatalf("schedule row after create: %v", err)
	}
	if row.CronExpr != "0 0/30 * * * ?" || !row.Enabled || row.NextRunAt == "" {
		t.Errorf("schedule row = %+v, want armed", row)
	}

	// Create WITHOUT a cron: no row (the config rides the event track).
	code, plain := replCronCreate(t, h, "plain", "")
	if code != http.StatusCreated {
		t.Fatalf("create without cron = %d, want 201", code)
	}
	if plain["cron_exp"] != "" || plain["next_schedule_sync"] != "" {
		t.Errorf("plain create echo = %v, want empty cron fields", plain)
	}
	plainID := int64(plain["id"].(float64))
	if row, err := h.md.Schedules().Get(ctx, "replication", strconv.FormatInt(plainID, 10)); err == nil {
		t.Errorf("plain create landed a schedule row: %+v", row)
	}

	// PUT the cron onto the plain config (the by-id face).
	resp, raw := h.do(http.MethodPut, "/binflow/api/v1/replications/2", "admin", "password",
		`{"cron_exp": "0 0 12 1/1 * ? *"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT cron = %d (%s), want 200", resp.StatusCode, raw)
	}
	var updated map[string]any
	_ = json.Unmarshal([]byte(raw), &updated)
	if updated["cron_exp"] != "0 0 12 1/1 * ? *" || updated["next_schedule_sync"] == "" {
		t.Errorf("PUT echo = %v, want the seven-field expression armed", updated)
	}

	// The enabled switch mirrors into the ledger: disabling parks the
	// schedule (enabled=false, next=''), re-enabling re-arms from now.
	resp, _ = h.do(http.MethodPut, "/binflow/api/v1/replications/2", "admin", "password", `{"enabled": false}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT enabled=false = %d, want 200", resp.StatusCode)
	}
	row, err = h.md.Schedules().Get(ctx, "replication", "2")
	if err != nil || row.Enabled || row.NextRunAt != "" {
		t.Errorf("schedule after disable = %+v (%v), want parked", row, err)
	}
	resp, _ = h.do(http.MethodPut, "/binflow/api/v1/replications/2", "admin", "password", `{"enabled": true}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT enabled=true = %d, want 200", resp.StatusCode)
	}
	row, err = h.md.Schedules().Get(ctx, "replication", "2")
	if err != nil || !row.Enabled || row.NextRunAt == "" {
		t.Errorf("schedule after re-enable = %+v (%v), want re-armed", row, err)
	}

	// The clear arm: an explicit empty cron deletes the row (single-state
	// law) and leaves the config fully usable.
	resp, _ = h.do(http.MethodPut, "/binflow/api/v1/replications/2", "admin", "password", `{"cron_exp": ""}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT clear = %d, want 200", resp.StatusCode)
	}
	if row, err := h.md.Schedules().Get(ctx, "replication", "2"); err == nil {
		t.Errorf("schedule row survived the clear: %+v", row)
	}

	// The delete linkage: the first config's row dies with its config.
	resp, _ = h.do(http.MethodDelete, "/binflow/api/v1/replications/cronned", "admin", "password", "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204", resp.StatusCode)
	}
	if row, err := h.md.Schedules().Get(ctx, "replication", strconv.FormatInt(id, 10)); err == nil {
		t.Errorf("schedule row outlived its config: %+v", row)
	}

	// The audit word: one replication.schedule.set per CRON arm (the
	// enabled-only flips stay the family's own config.update word).
	events, err := h.md.Audits().Query(ctx, metadata.AuditQuery{Action: "replication.schedule.set", Limit: 10})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("replication.schedule.set rows = %d, want 3 (create set, PUT set, clear)", len(events))
	}
}

func TestReplicationConfigCronValidationAndGates(t *testing.T) {
	h := newReplHarness(t, false)
	ctx := context.Background()

	// The anchor's 400 family: an invalid expression refuses the create
	// BEFORE the config row lands (nothing half-stored).
	resp, raw := h.do(http.MethodPost, "/binflow/api/v1/replications", "admin", "password",
		`{"name": "bad-cron", "source_repo": "libs-release", "target_url": "http://target.example", "target_repo": "replica", "cron_exp": "0 0 2 ? * MON-FRI-SAT"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create invalid cron = %d (%s), want 400", resp.StatusCode, raw)
	}
	if !strings.Contains(raw, "Invalid cronExp") {
		t.Errorf("400 body %q lacks the anchor's Invalid cronExp wording", raw)
	}
	if cfgs, err := h.replStore.ListConfigs(ctx); err != nil || len(cfgs) != 0 {
		t.Errorf("configs after refused create = %v (%v), want none", cfgs, err)
	}

	// The refused PUT likewise changes nothing.
	code, _ := replCronCreate(t, h, "good", "")
	if code != http.StatusCreated {
		t.Fatalf("create = %d, want 201", code)
	}
	resp, raw = h.do(http.MethodPut, "/binflow/api/v1/replications/1", "admin", "password",
		`{"cron_exp": "not a cron"}`)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(raw, "Invalid cronExp") {
		t.Fatalf("PUT invalid cron = %d (%s), want 400 Invalid cronExp", resp.StatusCode, raw)
	}
	if row, err := h.md.Schedules().Get(ctx, "replication", "1"); err == nil && row != nil {
		t.Errorf("refused PUT landed a schedule row: %+v", row)
	}

	// The empty edit is still the family's refused shape.
	resp, _ = h.do(http.MethodPut, "/binflow/api/v1/replications/1", "admin", "password", `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty PUT = %d, want 400", resp.StatusCode)
	}

	// The non-admin gate: the cron arm rides the family's own face.
	resp, _ = h.do(http.MethodPut, "/binflow/api/v1/replications/1", "dev", "dev-pw",
		`{"cron_exp": "0 0/5 * * * ?"}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin PUT cron = %d, want 403", resp.StatusCode)
	}

	// The list projection carries the cron fields for every config.
	resp, raw = h.do(http.MethodGet, "/binflow/api/v1/replications", "admin", "password", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list = %d, want 200", resp.StatusCode)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(raw), &list); err != nil || len(list) != 1 {
		t.Fatalf("list decode: %v (%s)", err, raw)
	}
	if _, ok := list[0]["cron_exp"]; !ok {
		t.Errorf("list row %v lacks the cron_exp field", list[0])
	}
}
