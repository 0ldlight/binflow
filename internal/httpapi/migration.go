package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
)

// MigrationStarter is the seam the storage migration engine plugs into HTTP
// handlers. It is defined here so httpapi does not import storage.
type MigrationStarter interface {
	StartMigration(ctx context.Context) error
	StatusView() any
}

// handleMigrationStatus answers GET /binflow/api/v1/storage/migration with the
// current migration progress.
func (s *Server) handleMigrationStatus(w http.ResponseWriter, _ *http.Request) {
	if s.deps.Migration == nil {
		writeError(w, http.StatusNotImplemented, "migration is not configured")
		return
	}
	status := s.deps.Migration.StatusView()
	body, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render migration status: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// handleMigrationStart answers POST /binflow/api/v1/storage/migration/start by
// initiating a background migration run. It is idempotent: calling it while a
// migration is already running returns the current status without error.
func (s *Server) handleMigrationStart(w http.ResponseWriter, r *http.Request) {
	if s.deps.Migration == nil {
		writeError(w, http.StatusNotImplemented, "migration is not configured")
		return
	}
	if err := s.deps.Migration.StartMigration(r.Context()); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	status := s.deps.Migration.StatusView()
	body, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render migration status: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write(body)
}
