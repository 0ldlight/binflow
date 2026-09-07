package httpapi

// GET/PUT /binflow/api/v1/system/maintenance — the maintenance plane's cron
// configuration face (M16 T-450, FR-150.3 / ADR-0044 decision 7①): the six
// maintenance cron slots of the Artifactory maintenance page
// (console-ui.md §3.9 — Garbage Collection / Cleanup Unused Cached
// Artifacts / Cleanup Virtual Repositories, plus the M17 T-495 gc-cron-gap
// three: Quota / Compress Internal Database / Prune Unreferenced Data),
// each backed by one row of the 021 schedules ledger under
// domain='maintenance'.
//
// BinFlow's honest mapping of the two cleanup families is the ADR-0044
// decision-8① closed set: "Cleanup 两族全量 pass (RunOnce)" — the
// repo.CleanupEngine full pass IS the two-family carrier (the unused-cache
// policy leg plus the session/gc legs), so BOTH cleanup slots dispatch the
// same RunOnce at fire time. There is deliberately no virtual-only pass
// (BinFlow virtual repositories aggregate; they hold no cache of their own
// to clean) — a registered C-layer difference, not a fabricated one.
//
// The T-495 three (FR-158): the quota / compress / prune slots ride the
// carrier kernels in maintenance_carriers.go — quota is the threshold
// check + alert trail (the per-repo quotaBytes ceilings the storage-quota
// page owns; enforcement is out of scope here), compress is the metadata VACUUM
// rebuild, prune is the gc engine's dry-run form. The Artifactory page's
// instance-level quota percentage pair has no BinFlow carrier and stays
// uncarried (T-462's FE note registered the thresholds' home as the
// storage-quota page — this slot schedules the check, it does not add a
// second threshold source).
//
// The manual faces stay exactly as they are (the AC's "手动 dry-run/apply
// 并存维持"): POST /api/v1/system/gc and POST/GET /api/v1/system/cleanup
// are untouched; "Run Now" IS those routes.

import (
	"net/http"
	"strings"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/scheduler"
)

// maintenanceSlots is the maintenance slot closed set (the 021 ledger's
// in-domain keys, ADR-0044 decision 2's DDL comment), in the order the GET
// projection renders. The T-495 extension (quota/compress/prune) is the
// carrier-set erratum registered in the ticket's report: ADR-0044
// decision 1's incremental-registration discipline (a new consumer is a
// Register/dispatch arm plus this list edit — never a schema change; the
// ADR-0046 K75 insights 4th-domain precedent lifted onto in-domain
// carriers), so the 021 CHECK and the ledger schema are untouched.
var maintenanceSlots = []string{
	"gc",
	"cleanup-unused-cache",
	"cleanup-virtual",
	"quota",
	"compress",
	"prune",
}

// maintenanceSlotStatus is one slot's GET projection: the ledger row's
// full state, with the absent row rendered as the unscheduled default
// ("no row = not scheduled" — ADR-0044 decision 2's single-state law).
type maintenanceSlotStatus struct {
	Key        string `json:"key"`
	CronExp    string `json:"cronExp"`
	Enabled    bool   `json:"enabled"`
	NextRun    string `json:"nextRun"`
	LastRun    string `json:"lastRun"`
	LastStatus string `json:"lastStatus"`
	LastError  string `json:"lastError"`
}

// maintenancePutSlotBody is one slot's PUT arm. cronExp empty (the only
// way to express it) DELETES the ledger row — the single-state semantics;
// a non-empty expression is validated (Parse + a reachable future trigger,
// ADR-0044 decision 4) and stored with next_run computed from now. Enabled
// defaults to true when the arm omits it (a schedule you just typed is
// meant to be live).
type maintenancePutSlotBody struct {
	CronExp string `json:"cronExp"`
	Enabled *bool  `json:"enabled"`
}

// maintenancePutBody is the PUT body: every slot is optional; an absent
// slot is untouched. The shape is closed over the six maintenance slots —
// the T-450 three plus the T-495 gc-cron-gap carriers.
type maintenancePutBody struct {
	GC                 *maintenancePutSlotBody `json:"gc"`
	CleanupUnusedCache *maintenancePutSlotBody `json:"cleanup-unused-cache"`
	CleanupVirtual     *maintenancePutSlotBody `json:"cleanup-virtual"`
	Quota              *maintenancePutSlotBody `json:"quota"`
	Compress           *maintenancePutSlotBody `json:"compress"`
	Prune              *maintenancePutSlotBody `json:"prune"`
}

// maintenanceProjection renders every slot's state — the GET body and the
// PUT response in one shape.
func (s *Server) maintenanceProjection(r *http.Request) ([]maintenanceSlotStatus, error) {
	out := make([]maintenanceSlotStatus, 0, len(maintenanceSlots))
	for _, key := range maintenanceSlots {
		st := maintenanceSlotStatus{Key: key}
		row, err := s.readScheduleRow(r, scheduler.DomainMaintenance, key)
		if err != nil {
			return nil, err
		}
		if row != nil {
			st.CronExp = row.CronExpr
			st.Enabled = row.Enabled && row.CronExpr != ""
			st.NextRun = row.NextRunAt
			st.LastRun = row.LastRunAt
			st.LastStatus = row.LastStatus
			st.LastError = row.LastError
		}
		out = append(out, st)
	}
	return out, nil
}

// handleSystemMaintenanceGET serves GET /api/v1/system/maintenance: the
// three slots' schedule state (the FE's cron + next-run columns).
func (s *Server) handleSystemMaintenanceGET(w http.ResponseWriter, r *http.Request) {
	slots, err := s.maintenanceProjection(r)
	if err != nil {
		if metadata.IsStoreBusy(err) {
			writeError(w, http.StatusServiceUnavailable, "schedules store is busy, retry shortly: "+err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "maintenance: reading schedules: "+err.Error())
		return
	}
	writeJSONBody(w, http.StatusOK, map[string]any{"slots": slots})
}

// handleSystemMaintenancePUT serves PUT /api/v1/system/maintenance: write
// one or more slots' cron configuration. The route gate demanded
// system:write (admin-only in the closed role set, ADR-0026); the handler
// re-checks because it writes the actor into the audit trail.
func (s *Server) handleSystemMaintenancePUT(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "maintenance configuration requires an administrator account")
		return
	}
	var body maintenancePutBody
	if err := decodeJSONBodyOf(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	arms := map[string]*maintenancePutSlotBody{
		"gc":                   body.GC,
		"cleanup-unused-cache": body.CleanupUnusedCache,
		"cleanup-virtual":      body.CleanupVirtual,
		"quota":                body.Quota,
		"compress":             body.Compress,
		"prune":                body.Prune,
	}
	// All arms validate BEFORE any lands: a two-arm PUT with one bad
	// expression must not half-land (the backup face's law, applied to the
	// multi-slot body).
	for _, key := range maintenanceSlots {
		if arm := arms[key]; arm != nil {
			if verr := validateCronArm(key, strings.TrimSpace(arm.CronExp), ""); verr != nil {
				s.writeScheduleError(w, verr, "maintenance")
				return
			}
		}
	}
	touched := 0
	for _, key := range maintenanceSlots {
		arm := arms[key]
		if arm == nil {
			continue
		}
		enabled := true
		if arm.Enabled != nil {
			enabled = *arm.Enabled
		}
		cron := strings.TrimSpace(arm.CronExp)
		cleared, nextRun, err := s.writeScheduleCron(r, scheduler.DomainMaintenance, key,
			cron, enabled, "", p.Name)
		if err != nil {
			s.writeScheduleError(w, err, "maintenance")
			return
		}
		detail := auditDetail("key", key, "action", "cleared")
		if !cleared {
			detail = auditDetail("key", key, "action", "set",
				"cronExp", cron, "next_run", nextRun, "enabled", boolText(enabled))
		}
		s.audit.Record(r.Context(), audit.Event{
			Actor:  p.Name,
			Action: audit.ActionMaintenanceScheduleSet,
			Detail: detail,
		})
		touched++
	}
	if touched == 0 {
		writeError(w, http.StatusBadRequest,
			"maintenance body carries no slot arm (gc, cleanup-unused-cache, cleanup-virtual, quota, compress, prune)")
		return
	}
	slots, err := s.maintenanceProjection(r)
	if err != nil {
		if metadata.IsStoreBusy(err) {
			writeError(w, http.StatusServiceUnavailable, "schedules store is busy, retry shortly: "+err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "maintenance: reading schedules: "+err.Error())
		return
	}
	writeJSONBody(w, http.StatusOK, map[string]any{"slots": slots})
}
