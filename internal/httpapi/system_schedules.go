package httpapi

// GET /api/v1/system/schedules — the read-only projection of the cron
// schedules ledger (M16 T-450, FR-150.3 / ADR-0044 decision 7④): every
// row's expression, next-run, last-run and status, optionally narrowed by
// ?domain= (the FE's next-run countdown / failure / disabled rendering,
// T-462's consuming face). This file also carries the writer helpers the
// three configuration faces (maintenance, backups, replications cronExp)
// share — the validation family, the single-state row law and the 400/500
// mapping.

import (
	"errors"
	"net/http"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/scheduler"
)

// nowUTC is the scheduling faces' clock: UTC second-resolution, the same
// reading metadata.Now() stores and the scheduler engine computes from.
func nowUTC() time.Time { return time.Now().UTC() }

// boolText renders a bool for the audit detail (the fmt.Sprint family the
// replication handlers use).
func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// cronError is the shared invalid-expression 400 payload of the three
// consuming faces: the anchor's "Invalid cronExp" wording
// (cron-scheduling.md §3) plus the slot/config the expression was for and
// the engine's pointed reason.
type cronError struct {
	key, cron, reason string
}

func (e *cronError) Error() string {
	return "Invalid cronExp " + e.cron + " for " + e.key + ": " + e.reason
}

// pastNextTimeError marks an explicit next-run timestamp (the backup
// face's writable nextBackupTime) that is not in the future — ADR-0044
// decision 9②'s past-time refusal.
type pastNextTimeError struct{ at string }

func (e *pastNextTimeError) Error() string {
	return "nextBackupTime " + e.at + " is not in the future; pick a time after now or omit the field"
}

// scheduleStatus is one ledger row's read shape — the projection body's
// element and the cron echo every config face's response embeds.
type scheduleStatus struct {
	Domain     string `json:"domain"`
	Key        string `json:"key"`
	CronExp    string `json:"cronExp"`
	Enabled    bool   `json:"enabled"`
	NextRun    string `json:"nextRun"`
	LastRun    string `json:"lastRun"`
	LastStatus string `json:"lastStatus"`
	LastError  string `json:"lastError"`
}

// validateCronArm is writeScheduleCron's validation half, called separately
// by faces whose payload write must not half-land behind a refused
// expression (the backup face writes the 022 row first): the same law,
// applied before anything is stored. nil = the arm is storable.
func validateCronArm(key, cron, explicitNext string) error {
	if cron == "" {
		return nil
	}
	if _, verr := scheduler.ValidateExpr(cron, nowUTC()); verr != nil {
		reason := verr.Error()
		if errors.Is(verr, scheduler.ErrUnreachable) {
			reason = "the expression has no future trigger"
		}
		return &cronError{key: key, cron: cron, reason: reason}
	}
	if explicitNext != "" {
		at, perr := time.Parse(time.RFC3339, explicitNext)
		if perr != nil {
			return &cronError{key: key, cron: cron,
				reason: "nextBackupTime " + explicitNext + " is not an RFC3339 timestamp"}
		}
		if cerr := scheduler.CheckNextRunAt(at.UTC(), nowUTC()); cerr != nil {
			return &pastNextTimeError{at: explicitNext}
		}
	}
	return nil
}

// readScheduleRow is ScheduleStore.Get with the not-found sentinel folded
// into nil ("no row = not scheduled" — the reader never errors on absence).
func (s *Server) readScheduleRow(r *http.Request, domain, key string) (*metadata.Schedule, error) {
	row, err := s.deps.Metadata.Schedules().Get(r.Context(), domain, key)
	if errors.Is(err, metadata.ErrScheduleNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

// writeScheduleCron lands ONE schedule configuration (the shared writer of
// the three consuming faces, ADR-0044 decisions 2/4/9):
//
//   - cron == "" DELETES the row — the "no row = not scheduled" single
//     state; an absent row is not an error. Returns cleared=true.
//   - otherwise the expression is validated (Parse + a reachable future
//     trigger), next_run is the explicitNext timestamp when one is given
//     (checked against now — a past explicit next-run refuses) and the
//     computed next trigger otherwise, and the row is written PRESERVING
//     the creation stamp and the engine-owned run-state columns (a config
//     edit re-arms; it does not rewrite history). enabled=false parks the
//     row unscheduled (next=” — the DDL's disabled shape, the cron kept
//     so re-enabling recomputes from now).
//
// A *cronError or *pastNextTimeError return is the 400 family; every other
// error is a store fault (writeScheduleError maps both).
func (s *Server) writeScheduleCron(r *http.Request, domain, key, cron string, enabled bool, explicitNext, actor string) (cleared bool, nextRun string, err error) {
	ctx := r.Context()
	if cron == "" {
		if derr := s.deps.Metadata.Schedules().Delete(ctx, domain, key); derr != nil && !errors.Is(derr, metadata.ErrScheduleNotFound) {
			return false, "", derr
		}
		return true, "", nil
	}
	next, verr := scheduler.ValidateExpr(cron, nowUTC())
	if verr != nil {
		reason := verr.Error()
		if errors.Is(verr, scheduler.ErrUnreachable) {
			reason = "the expression has no future trigger"
		}
		return false, "", &cronError{key: key, cron: cron, reason: reason}
	}
	if explicitNext != "" {
		at, perr := time.Parse(time.RFC3339, explicitNext)
		if perr != nil {
			return false, "", &cronError{key: key, cron: cron,
				reason: "nextBackupTime " + explicitNext + " is not an RFC3339 timestamp"}
		}
		if cerr := scheduler.CheckNextRunAt(at.UTC(), nowUTC()); cerr != nil {
			return false, "", &pastNextTimeError{at: explicitNext}
		}
		next = at.UTC()
	}
	nextRun = ""
	if enabled {
		nextRun = next.UTC().Format(time.RFC3339)
	}
	now := metadata.Now()
	row := &metadata.Schedule{
		Domain: domain, Key: key,
		CronExpr: cron, Enabled: enabled, NextRunAt: nextRun,
		CreatedAt: now, CreatedBy: actor, UpdatedAt: now, UpdatedBy: actor,
	}
	if cur, gerr := s.deps.Metadata.Schedules().Get(ctx, domain, key); gerr == nil {
		row.CreatedAt, row.CreatedBy = cur.CreatedAt, cur.CreatedBy
		row.LastRunAt, row.LastStatus, row.LastError = cur.LastRunAt, cur.LastStatus, cur.LastError
	} else if !errors.Is(gerr, metadata.ErrScheduleNotFound) {
		return false, "", gerr
	}
	if perr := s.deps.Metadata.Schedules().Put(ctx, row); perr != nil {
		return false, "", perr
	}
	return false, nextRun, nil
}

// syncScheduleEnabled flips an EXISTING row's enabled bit (re-arming
// next_run from the row's current expression, ” when disabled) — the
// replication config face's enabled switch mirrors into the ledger so one
// flip parks the event track and the schedule together. No row: a no-op
// (nothing scheduled, nothing to park).
func (s *Server) syncScheduleEnabled(r *http.Request, domain, key string, enabled bool, actor string) error {
	ctx := r.Context()
	row, err := s.deps.Metadata.Schedules().Get(ctx, domain, key)
	if errors.Is(err, metadata.ErrScheduleNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	row.Enabled = enabled
	row.NextRunAt = ""
	if enabled {
		if next, nerr := scheduler.Next(row.CronExpr, nowUTC()); nerr == nil {
			row.NextRunAt = next.UTC().Format(time.RFC3339)
		} // unreachable-at-flip: leave '' (unscheduled), the engine's park
	}
	row.UpdatedAt = metadata.Now()
	row.UpdatedBy = actor
	return s.deps.Metadata.Schedules().Put(ctx, row)
}

// writeScheduleError maps a writeScheduleCron error onto the HTTP family:
// the 400 validation arms, 503 for a busy store (the family's retryable
// posture), 500 for every other store fault.
func (s *Server) writeScheduleError(w http.ResponseWriter, err error, face string) {
	var ce *cronError
	var pe *pastNextTimeError
	switch {
	case errors.As(err, &ce), errors.As(err, &pe):
		writeError(w, http.StatusBadRequest, err.Error())
	case metadata.IsStoreBusy(err):
		writeError(w, http.StatusServiceUnavailable, face+" store is busy, retry shortly: "+err.Error())
	default:
		writeError(w, http.StatusInternalServerError, face+": writing schedules: "+err.Error())
	}
}

// handleSystemSchedulesGET serves GET /api/v1/system/schedules?domain=:
// the ledger's read-only projection (every domain, or one closed-set
// domain). Gate = system:read — readonly_admin may see the schedule state
// (the addons-plane posture); writes only exist on the three config faces.
func (s *Server) handleSystemSchedulesGET(w http.ResponseWriter, r *http.Request) {
	domain := ""
	if raw, ok := r.URL.Query()["domain"]; ok {
		domain = raw[0]
		known := false
		for _, d := range scheduler.Domains() {
			if d == domain {
				known = true
				break
			}
		}
		if !known {
			writeError(w, http.StatusBadRequest,
				"domain must be one of maintenance, backup, replication (or omitted for every domain)")
			return
		}
	}
	rows, err := s.deps.Metadata.Schedules().List(r.Context(), domain)
	if err != nil {
		if metadata.IsStoreBusy(err) {
			writeError(w, http.StatusServiceUnavailable, "schedules store is busy, retry shortly: "+err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "schedules: listing: "+err.Error())
		return
	}
	out := make([]scheduleStatus, 0, len(rows))
	for _, row := range rows {
		out = append(out, scheduleStatus{
			Domain: row.Domain, Key: row.Key, CronExp: row.CronExpr,
			Enabled: row.Enabled && row.CronExpr != "",
			NextRun: row.NextRunAt, LastRun: row.LastRunAt,
			LastStatus: row.LastStatus, LastError: row.LastError,
		})
	}
	writeJSONBody(w, http.StatusOK, map[string]any{"schedules": out})
}
