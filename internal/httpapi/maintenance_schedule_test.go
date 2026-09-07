package httpapi_test

// The maintenance cron face's REST contract (M16 T-450, FR-150.3 /
// ADR-0044 decision 7①): the three-slot projection, the PUT write law
// (single-state clear, validation, disabled shape), the anchor's 400
// family, the schedule/set audit word, the capability gates — and the AC's
// "手动 dry-run/apply 并存维持": the manual gc/cleanup faces keep working
// untouched beside the cron configuration.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

func maintBody(t *testing.T, resp *http.Response) map[string]map[string]any {
	t.Helper()
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var body struct {
		Slots []map[string]any `json:"slots"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode maintenance body %s: %v", raw, err)
	}
	if len(body.Slots) != 6 {
		t.Fatalf("slots = %d (%s), want the six maintenance slots", len(body.Slots), raw)
	}
	out := map[string]map[string]any{}
	for _, s := range body.Slots {
		out[s["key"].(string)] = s
	}
	return out
}

func TestMaintenanceScheduleSlotsLifecycle(t *testing.T) {
	h := newHarness(t)

	// The fresh instance carries no rows: every slot renders unscheduled.
	resp := h.do(http.MethodGet, "/binflow/api/v1/system/maintenance", "admin", adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET = %d, want 200", resp.StatusCode)
	}
	slots := maintBody(t, resp)
	for _, key := range []string{"gc", "cleanup-unused-cache", "cleanup-virtual", "quota", "compress", "prune"} {
		s := slots[key]
		if s["cronExp"] != "" || s["enabled"] != false || s["nextRun"] != "" {
			t.Errorf("fresh slot %s = %v, want the unscheduled default", key, s)
		}
	}

	// Write the factory GC cadence: echoed, enabled, with a future next-run.
	resp = h.do(http.MethodPut, "/binflow/api/v1/system/maintenance", "admin", adminPass,
		[]byte(`{"gc": {"cronExp": "0 0 /4 * * ?"}}`), nil)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT gc = %d (%s), want 200", resp.StatusCode, raw)
	}
	slots = maintBody(t, resp)
	gc := slots["gc"]
	if gc["cronExp"] != "0 0 /4 * * ?" || gc["enabled"] != true {
		t.Fatalf("gc after PUT = %v, want the stored schedule", gc)
	}
	next, _ := gc["nextRun"].(string)
	if next == "" {
		t.Fatal("gc nextRun is empty, want the computed trigger")
	}

	// Persisted: a second GET reads the same row.
	resp = h.do(http.MethodGet, "/binflow/api/v1/system/maintenance", "admin", adminPass, nil, nil)
	slots = maintBody(t, resp)
	if slots["gc"]["nextRun"] != next {
		t.Errorf("nextRun drifted between reads: %v vs %v", slots["gc"]["nextRun"], next)
	}

	// The disabled shape keeps the cron, drops the next-run (the DDL's
	// single state for "configured but parked").
	resp = h.do(http.MethodPut, "/binflow/api/v1/system/maintenance", "admin", adminPass,
		[]byte(`{"cleanup-unused-cache": {"cronExp": "0 12 5 * * ?", "enabled": false}}`), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT cleanup disabled = %d, want 200", resp.StatusCode)
	}
	slots = maintBody(t, resp)
	cu := slots["cleanup-unused-cache"]
	if cu["cronExp"] != "0 12 5 * * ?" || cu["enabled"] != false || cu["nextRun"] != "" {
		t.Errorf("disabled cleanup slot = %v, want cron kept / enabled false / nextRun ''", cu)
	}

	// The clear arm deletes the row (the single-state law): the slot reads
	// unscheduled and the ledger projection carries no maintenance rows.
	resp = h.do(http.MethodPut, "/binflow/api/v1/system/maintenance", "admin", adminPass,
		[]byte(`{"cleanup-unused-cache": {"cronExp": ""}}`), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT clear = %d, want 200", resp.StatusCode)
	}
	slots = maintBody(t, resp)
	if slots["cleanup-unused-cache"]["cronExp"] != "" {
		t.Errorf("cleared slot = %v, want unscheduled", slots["cleanup-unused-cache"])
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
	if len(proj.Schedules) != 1 || proj.Schedules[0]["key"] != "gc" {
		t.Errorf("maintenance ledger rows = %v, want the gc row alone", proj.Schedules)
	}

	// The audit word: one maintenance.schedule.set row per landed arm.
	events, err := h.md.Audits().Query(context.Background(), metadata.AuditQuery{Action: "maintenance.schedule.set", Limit: 10})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("maintenance.schedule.set rows = %d, want 3 (gc set, cleanup set, cleanup cleared)", len(events))
	}
}

func TestMaintenanceScheduleValidationFamily(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name string
		body string
		want string
	}{
		{"garbage expression", `{"gc": {"cronExp": "not a cron"}}`, "Invalid cronExp"},
		{"five-field unix cron", `{"gc": {"cronExp": "0 * * * *"}}`, "5-field Unix cron"},
		{"both day fields", `{"gc": {"cronExp": "0 0 2 1 * MON"}}`, "exactly one"},
		{"out-of-range value", `{"gc": {"cronExp": "0 99 * ? * *"}}`, "Invalid cronExp"},
	}
	for _, tc := range cases {
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
	// No slot arm at all is the refused empty edit.
	resp := h.do(http.MethodPut, "/binflow/api/v1/system/maintenance", "admin", adminPass, []byte(`{}`), nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty PUT = %d, want 400", resp.StatusCode)
	}
	// A rejected write leaves nothing behind.
	rows, err := h.md.Schedules().List(context.Background(), "maintenance")
	if err != nil || len(rows) != 0 {
		t.Errorf("maintenance rows after refusals = %v (%v), want none", rows, err)
	}
}

func TestMaintenanceScheduleCapabilityGates(t *testing.T) {
	h := newHarness(t)
	seedLicenseUser(t, h.md, "roat", "roat-pw", "readonly_admin")

	// readonly_admin reads the schedule state, never writes it (T-214①).
	resp := h.do(http.MethodGet, "/binflow/api/v1/system/maintenance", "roat", "roat-pw", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readonly GET = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = h.do(http.MethodPut, "/binflow/api/v1/system/maintenance", "roat", "roat-pw",
		[]byte(`{"gc": {"cronExp": "0 0 /4 * * ?"}}`), nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("readonly PUT = %d, want 403", resp.StatusCode)
	}
	_ = resp.Body.Close()
	// T-495: the probe holds over the widened six-slot set — the new
	// carrier arms ride the same system:write gate, not a softer one.
	resp = h.do(http.MethodPut, "/binflow/api/v1/system/maintenance", "roat", "roat-pw",
		[]byte(`{"quota": {"cronExp": "0 15 3 * * ?"}, "compress": {"cronExp": "0 0 4 ? * SUN"}, "prune": {"cronExp": "0 30 1 * * ?"}}`), nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("readonly PUT gap slots = %d, want 403", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// The schedules projection rides the same read gate.
	resp = h.do(http.MethodGet, "/binflow/api/v1/system/schedules", "roat", "roat-pw", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readonly schedules GET = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Anonymous is the 401 of the route gate; an unknown domain filter is
	// the honest 400, not a silent empty list.
	resp = h.do(http.MethodGet, "/binflow/api/v1/system/maintenance", "", "", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous GET = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = h.do(http.MethodGet, "/binflow/api/v1/system/schedules?domain=nope", "admin", adminPass, nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("schedules unknown domain = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestMaintenanceManualFacesCoexistWithSchedule is the AC's "手动
// dry-run/apply 并存维持": with a gc schedule configured, the manual POST
// faces answer exactly as before (the schedule is configuration; the manual
// routes are the carriers "Run Now" rides).
func TestMaintenanceManualFacesCoexistWithSchedule(t *testing.T) {
	h := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		eng, err := repo.NewCleanupEngine(repo.CleanupOptions{
			Store:        d.Metadata,
			Engine:       d.GC,
			Audit:        audit.New(d.Metadata, true),
			AuditEnabled: true,
			DataDir:      d.DataDir,
			Grace:        time.Nanosecond,
			TickEvery:    time.Hour,
		})
		if err != nil {
			t.Fatalf("NewCleanupEngine: %v", err)
		}
		d.Cleanup = eng
	}, nil)
	resp := h.do(http.MethodPut, "/binflow/api/v1/system/maintenance", "admin", adminPass,
		[]byte(`{"gc": {"cronExp": "0 0 /4 * * ?"}, "cleanup-unused-cache": {"cronExp": "0 12 5 * * ?"}}`), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// The manual gc face: dry-run default posture, untouched semantics.
	resp = h.do(http.MethodPost, "/binflow/api/v1/system/gc", "admin", adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manual gc = %d, want 200 (dry-run default)", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var gcResp struct {
		DeletedCount int `json:"deletedCount"`
	}
	if err := json.Unmarshal(raw, &gcResp); err != nil {
		t.Fatalf("decode gc body: %v (%s)", err, raw)
	}
	if gcResp.DeletedCount != 0 {
		t.Errorf("dry-run deleted = %d, want 0", gcResp.DeletedCount)
	}

	// The manual cleanup face: same dry-run default.
	resp = h.do(http.MethodPost, "/binflow/api/v1/system/cleanup", "admin", adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manual cleanup = %d, want 200 (dry-run default)", resp.StatusCode)
	}
	raw, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var clean struct {
		Trigger string `json:"trigger"`
		Apply   bool   `json:"apply"`
	}
	if err := json.Unmarshal(raw, &clean); err != nil {
		t.Fatalf("decode cleanup body: %v (%s)", err, raw)
	}
	if clean.Trigger != "manual" || clean.Apply {
		t.Errorf("manual cleanup = trigger %q apply %v, want manual dry-run", clean.Trigger, clean.Apply)
	}
}
