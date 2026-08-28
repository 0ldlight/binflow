package httpapi

// POST/GET /binflow/api/v1/system/cleanup — the unused-cleanup engine's
// REST face (T-324, PRD FR-102.2 / LC-29). The engine itself lives in
// internal/repo (cleanup.go); this file is only the management plane:
// the manual trigger with the gc family's dry-run-default posture
// (ADR-0015 erratum ①), the live status view (the GET the gc face
// recorded as P2 debt — here the engine's own last-run snapshot makes it
// cheap), and the audit actor handoff. Synchronous execution follows the
// gc face's PRD Q6 interim reading: one POST runs the three legs to
// completion and answers with the report.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// ReplayStatsSource is the fail-open engine's stats face (M12 T-338,
// ADR-0040's observability clause): the seven replay series refresh at
// scrape time. It lives beside CleanupEngine — the consumer-side
// narrow-interface precedent.
type ReplayStatsSource interface {
	ReplayStats() storage.ReplayStats
}

// CleanupEngine is the consumer-side seam over repo.CleanupEngine the
// router + handlers need: one scoped or full run, and the read faces. cmd
// wires the real engine; the test harness wires the same. Assemblies
// without one leave it nil and POST answers 503 rather than pretending a
// run happened.
type CleanupEngine interface {
	RunOnce(ctx context.Context, opts repo.CleanupRunOptions) (*repo.CleanupReport, error)
	LastReport() *repo.CleanupReport
	Stats() repo.CleanupStats
	TickEvery() time.Duration
	AuditEnabled() bool
}

// cleanupRequestBody is the trigger body {"apply":bool,"repo":string?}.
// apply defaults to false (the dry-run default posture); repo scopes the
// policy leg to one repository (the session and gc legs stay whole-store —
// they are maintenance, not policy).
type cleanupRequestBody struct {
	Apply bool   `json:"apply"`
	Repo  string `json:"repo"`
}

// cleanupStatus is the GET response: schedule, cumulative counters, the
// last run's report and the policy summary per remote repository.
type cleanupStatus struct {
	Enabled      bool                `json:"enabled"`
	AuditEnabled bool                `json:"auditEnabled"`
	TickEvery    string              `json:"tickEvery"`
	Stats        repo.CleanupStats   `json:"stats"`
	LastRun      *repo.CleanupReport `json:"lastRun"`
	Repos        []cleanupPolicyRow  `json:"repos"`
}

// cleanupPolicyRow is one remote repository's effective policy.
type cleanupPolicyRow struct {
	Repo        string `json:"repo"`
	PeriodHours int64  `json:"periodHours"`
	Off         bool   `json:"off"`
}

// handleSystemCleanupPOST serves POST /binflow/api/v1/system/cleanup. The
// route gate already demanded an authenticated system:write principal; the
// handler re-checks (it hands the actor to the audit trail).
func (s *Server) handleSystemCleanupPOST(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "cleanup requires an administrator account")
		return
	}
	if s.deps.Cleanup == nil {
		writeError(w, http.StatusServiceUnavailable, "cleanup is not available on this instance")
		return
	}

	var body cleanupRequestBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		// EOF alone is an empty body: the dry-run default posture.
		writeError(w, http.StatusBadRequest, "request body is not valid cleanup request JSON: "+err.Error())
		return
	}
	if body.Repo != "" {
		if err := validateRepoKeyShape(body.Repo); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	rep, err := s.deps.Cleanup.RunOnce(r.Context(), repo.CleanupRunOptions{
		Trigger: repo.CleanupTriggerManual,
		Apply:   body.Apply,
		Repo:    body.Repo,
		Actor:   p.Name,
	})
	if err != nil {
		if errors.Is(err, repo.ErrCleanupRunning) {
			writeError(w, http.StatusConflict, "cleanup rejected: "+err.Error())
			return
		}
		if errors.Is(err, storage.ErrDataLockHeld) {
			writeError(w, http.StatusConflict, s.cleanupLockRefusedMessage(err))
			return
		}
		if errors.Is(err, repo.ErrRepoNotFound) {
			// A scoped run naming an unknown repository: the honest 404
			// (the run's harmless legs still executed and its audit row
			// still landed — the report is in the trail).
			writeError(w, http.StatusNotFound, "cleanup: "+err.Error())
			return
		}
		if rep != nil {
			// The run completed with per-leg failures: the report IS the
			// answer (200 with ok=false), the same honest-divergence
			// reading the gc face gives racing writers.
			s.writeCleanupReport(w, rep)
			return
		}
		s.log.ErrorContext(r.Context(), "httpapi: cleanup run failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "cleanup: "+err.Error())
		return
	}
	s.writeCleanupReport(w, rep)
}

// writeCleanupReport renders one run report.
func (s *Server) writeCleanupReport(w http.ResponseWriter, rep *repo.CleanupReport) {
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render cleanup report: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// handleSystemCleanupGET serves GET /binflow/api/v1/system/cleanup: the
// engine's live status (schedule, counters, last report, per-repo policy).
func (s *Server) handleSystemCleanupGET(w http.ResponseWriter, r *http.Request) {
	if s.deps.Cleanup == nil {
		writeError(w, http.StatusServiceUnavailable, "cleanup is not available on this instance")
		return
	}
	st := cleanupStatus{
		Enabled:      false,
		AuditEnabled: s.deps.Cleanup.AuditEnabled(),
		TickEvery:    s.deps.Cleanup.TickEvery().String(),
		Stats:        s.deps.Cleanup.Stats(),
		LastRun:      s.deps.Cleanup.LastReport(),
	}
	rows, err := s.deps.Metadata.Repos().List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cleanup: listing repositories: "+err.Error())
		return
	}
	for _, row := range rows {
		if row.Type != repo.TypeRemote {
			continue
		}
		prow := cleanupPolicyRow{Repo: row.RepoKey}
		if cfg, cerr := s.deps.Metadata.Remote().GetConfig(r.Context(), row.RepoKey); cerr == nil {
			prow.PeriodHours = cfg.UnusedCleanupPeriodHours
			prow.Off = cfg.UnusedCleanupPeriodHours <= 0
		} else {
			prow.Off = true
		}
		if !prow.Off {
			st.Enabled = true
		}
		st.Repos = append(st.Repos, prow)
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render cleanup status: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// validateRepoKeyShape refuses a scoped run whose repo key cannot be a
// repo key (the cheap charset check; existence is the engine's ErrRepoNotFound).
func validateRepoKeyShape(key string) error {
	if len(key) < 2 || len(key) > 64 {
		return fmt.Errorf("repo must be 2-64 characters, got %d", len(key))
	}
	return nil
}

// cleanupLockRefusedMessage shapes the 409 body off the lock file's holder
// record (the gc face's T-96 N1/N3 posture, never parsed out of the error
// string).
func (s *Server) cleanupLockRefusedMessage(err error) string {
	var because string
	switch storage.HolderOp(storage.LockHolder(s.deps.DataDir)) {
	case storage.DataLockOpExport:
		because = "export in progress; "
	case storage.DataLockOpGC:
		because = "a gc run is in progress; "
	case storage.DataLockOpImport:
		because = "an import is in progress; "
	case storage.DataLockOpCleanup:
		because = "another cleanup run is in progress; "
	default:
		because = "another maintenance operation is in progress; "
	}
	return "cleanup rejected: " + because + err.Error()
}
