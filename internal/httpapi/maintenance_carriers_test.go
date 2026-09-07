package httpapi_test

// The gc-cron-gap three carriers' configuration CRUD on the maintenance
// face (M17 T-495, FR-158): the quota / compress / prune slots ride the
// same six-slot projection, the same PUT write law (single-state clear,
// pre-validation, disabled shape) and the same maintenance.schedule.set
// audit word as the T-450 three — the honest gap T-462 registered ("Quota/
// Compress/Prune 无载体不伪造") closes on the CONFIG plane here; the fire
// plane's proofs (carrier kernels + the scheduled dispatch) live in
// cmd/binflow-server's wiring tests.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// gapSlotKeys are the three T-495 slot keys, in the projection's order.
var gapSlotKeys = []string{"quota", "compress", "prune"}

// TestMaintenanceGapSlotsConfigCRUD: the three new slots' cron arms land,
// echo with a computed next-run, persist, keep the disabled shape, clear
// to the single unscheduled state, and write one maintenance.schedule.set
// audit row per landed arm — the T-450 contract, extended verbatim.
func TestMaintenanceGapSlotsConfigCRUD(t *testing.T) {
	h := newHarness(t)

	// Set all three in one PUT: each echoes enabled with a future next-run.
	resp := h.do(http.MethodPut, "/binflow/api/v1/system/maintenance", "admin", adminPass,
		[]byte(`{"quota": {"cronExp": "0 15 3 * * ?"}, "compress": {"cronExp": "0 0 4 ? * SUN"}, "prune": {"cronExp": "0 30 1 * * ?"}}`), nil)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT three gap slots = %d (%s), want 200", resp.StatusCode, raw)
	}
	slots := maintBody(t, resp)
	nextBySlot := map[string]string{}
	for _, key := range gapSlotKeys {
		s := slots[key]
		if s["cronExp"] == "" || s["enabled"] != true {
			t.Fatalf("slot %s after PUT = %v, want the stored schedule", key, s)
		}
		next, _ := s["nextRun"].(string)
		if next == "" {
			t.Fatalf("slot %s nextRun empty, want the computed trigger", key)
		}
		nextBySlot[key] = next
	}

	// Persisted: a second GET reads the same rows (and the projection
	// carries exactly the three ledger rows).
	resp = h.do(http.MethodGet, "/binflow/api/v1/system/maintenance", "admin", adminPass, nil, nil)
	slots = maintBody(t, resp)
	for _, key := range gapSlotKeys {
		if slots[key]["nextRun"] != nextBySlot[key] {
			t.Errorf("slot %s nextRun drifted between reads: %v vs %v",
				key, slots[key]["nextRun"], nextBySlot[key])
		}
	}
	sresp := h.do(http.MethodGet, "/binflow/api/v1/system/schedules?domain=maintenance", "admin", adminPass, nil, nil)
	sraw, _ := io.ReadAll(sresp.Body)
	_ = sresp.Body.Close()
	var proj struct {
		Schedules []map[string]any `json:"schedules"`
	}
	if err := json.Unmarshal(sraw, &proj); err != nil {
		t.Fatalf("decode schedules projection: %v (%s)", err, sraw)
	}
	if len(proj.Schedules) != 3 {
		t.Fatalf("maintenance ledger rows = %v, want exactly the three gap rows", proj.Schedules)
	}

	// The disabled shape: cron kept, enabled false, next-run ''.
	resp = h.do(http.MethodPut, "/binflow/api/v1/system/maintenance", "admin", adminPass,
		[]byte(`{"compress": {"cronExp": "0 0 4 ? * SUN", "enabled": false}}`), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT compress disabled = %d, want 200", resp.StatusCode)
	}
	slots = maintBody(t, resp)
	if c := slots["compress"]; c["cronExp"] != "0 0 4 ? * SUN" || c["enabled"] != false || c["nextRun"] != "" {
		t.Errorf("disabled compress slot = %v, want cron kept / enabled false / nextRun ''", c)
	}

	// The clear arm deletes the row (single-state law): one slot cleared,
	// the other two untouched.
	resp = h.do(http.MethodPut, "/binflow/api/v1/system/maintenance", "admin", adminPass,
		[]byte(`{"prune": {"cronExp": ""}}`), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT clear prune = %d, want 200", resp.StatusCode)
	}
	slots = maintBody(t, resp)
	if p := slots["prune"]; p["cronExp"] != "" || p["enabled"] != false {
		t.Errorf("cleared prune slot = %v, want unscheduled", p)
	}
	if slots["quota"]["nextRun"] != nextBySlot["quota"] {
		t.Errorf("quota slot drifted while prune cleared: %v", slots["quota"])
	}

	// The audit word: one maintenance.schedule.set row per landed arm —
	// 3 (the set) + 1 (the disabled re-set) + 1 (the clear) = 5.
	events, err := h.md.Audits().Query(context.Background(), metadata.AuditQuery{Action: "maintenance.schedule.set", Limit: 20})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("maintenance.schedule.set rows = %d, want 5 (3 set + 1 disabled + 1 clear)", len(events))
	}
}

// TestMaintenanceGapSlotsValidationFamily: a bad expression on a gap slot
// arm is the anchor's 400 naming the slot, and nothing half-lands; a mixed
// PUT with one good and one bad gap arm refuses WHOLE (the pre-validation
// law) — the quota/compress/prune arms answer exactly the family the T-450
// slots do.
func TestMaintenanceGapSlotsValidationFamily(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"garbage quota expression", `{"quota": {"cronExp": "every night"}}`, "Invalid cronExp"},
		{"out-of-range compress minute", `{"compress": {"cronExp": "0 77 4 * * ?"}}`, "Invalid cronExp"},
		{"both day fields prune", `{"prune": {"cronExp": "0 0 2 1 * MON"}}`, "exactly one"},
	} {
		resp := h.do(http.MethodPut, "/binflow/api/v1/system/maintenance", "admin", adminPass, []byte(tc.body), nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: PUT = %d, want 400", tc.name, resp.StatusCode)
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if !strings.Contains(string(raw), tc.want) {
			t.Errorf("%s: 400 body %q lacks %q", tc.name, raw, tc.want)
		}
	}

	// The all-arms-validate-first law: a valid quota arm beside a garbage
	// compress arm refuses whole — the quota slot must NOT half-land.
	resp := h.do(http.MethodPut, "/binflow/api/v1/system/maintenance", "admin", adminPass,
		[]byte(`{"quota": {"cronExp": "0 15 3 * * ?"}, "compress": {"cronExp": "nope"}}`), nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("mixed PUT = %d, want the 400 of the bad arm", resp.StatusCode)
	}
	_ = resp.Body.Close()
	rows, err := h.md.Schedules().List(context.Background(), "maintenance")
	if err != nil || len(rows) != 0 {
		t.Errorf("maintenance rows after refusals = %v (%v), want none", rows, err)
	}
}
