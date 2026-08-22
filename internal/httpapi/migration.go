package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/lzwzzy/binflow/internal/storage"
)

// MigrationStarter is the seam the storage migration engine plugs into HTTP
// handlers. The signatures are the engine's own method set verbatim — since
// T-180 the StatusView return is the concrete *storage.MigrationStatusView
// (httpapi already depends on storage for the GC seam and the data lock),
// so *storage.MigrationEngine satisfies this interface directly and cmd no
// longer bridges it through an adapter (the T-178 leftover this collapsed).
type MigrationStarter interface {
	StartMigration(ctx context.Context) error
	StatusView() *storage.MigrationStatusView
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
//
// The migration context is DETACHED from the request (T-201, T-173 D-3):
// net/http cancels r.Context() the moment this handler returns, so handing
// it to StartMigration killed the background run at "list disk blobs" the
// instant the 202 left the wire — a REST-triggered migration could never
// migrate a single blob (H12/H14 were unreachable through the product
// path). WithoutCancel keeps every request value and drops only the
// cancellation, the same ruling the GC sweep ctx (T-94 N1) and the
// replication enqueue (T-162) already follow. Lifecycle is not lost: the
// migration engine derives its own cancellable context from the one passed
// here and cancels it on Close, so graceful shutdown still stops the run
// (and the run is restart-safe by design).
func (s *Server) handleMigrationStart(w http.ResponseWriter, r *http.Request) {
	if s.deps.Migration == nil {
		writeError(w, http.StatusNotImplemented, "migration is not configured")
		return
	}
	if err := s.deps.Migration.StartMigration(context.WithoutCancel(r.Context())); err != nil {
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
