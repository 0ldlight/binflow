package httpapi

// The trash-can REST family (M12 T-345, FR-106.1 / inv-1-core.md section B,
// inv-2-surface.md section 1.A):
//
//	POST   /binflow/api/trash/empty                    empty the can
//	POST   /binflow/api/trash/restore/{path}?to=…      restore an entry
//	DELETE /binflow/api/trash/clean/{path}             purge one entry
//
// Browsing rides the standing storage face (GET /api/storage/auto-trashcan
// and its ?properties arm — the four-tuple assertion surface); the console
// tree node is the repository key itself (console-ui.md's Trash Can node).
// The wire's `to` and `transaction-size` restore parameters are parsed and
// validated; transaction-size is semantically inert in BinFlow (the
// pipeline is per-item — the registered divergence).
//
// The routes demand the system:write capability (admin; readonly_admin
// 403 — the destructive-management posture of the gc/cleanup family), and
// the license gate is each handler's first line (the Q3 interim ruling:
// the trashcan slot at pro; community answers the D4 403).

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/repo"
)

// addonTrashcan is the feature's slot, single-sourced from the addons
// constructor (FR-86-AC2).
var addonTrashcan = addons.Trashcan()

// trashRunner is the service capability face (consumer-side interface off
// Deps.ReposSvc — the copyMoveRunner posture).
type trashRunner interface {
	TrashRestore(ctx context.Context, p *repo.Principal, req repo.TrashRestoreRequest) (*repo.CopyMoveResult, error)
	TrashEmpty(ctx context.Context, p *repo.Principal) (*repo.TrashSummary, error)
	TrashClean(ctx context.Context, p *repo.Principal, path string) (*repo.TrashSummary, error)
}

// trashFace resolves the capability; the honest 501 when the service does
// not carry it (a unit stack or an assembly gap — the deb/helm
// management-face posture).
func (s *Server) trashFace(w http.ResponseWriter, r *http.Request) trashRunner {
	if !s.RequireAddon(w, r, addonTrashcan.ID, addonTrashcan.MinTier) {
		return nil
	}
	svc, ok := s.deps.ReposSvc.(trashRunner)
	if !ok {
		writeError(w, http.StatusNotImplemented, "the trash can is not available on this instance")
		return nil
	}
	return svc
}

// handleTrashEmpty serves POST /binflow/api/trash/empty.
func (s *Server) handleTrashEmpty(w http.ResponseWriter, r *http.Request) {
	svc := s.trashFace(w, r)
	if svc == nil {
		return
	}
	sum, err := svc.TrashEmpty(r.Context(), principalFrom(r.Context()))
	if err != nil {
		s.writeTrashError(w, err)
		return
	}
	writeJSONBody(w, http.StatusOK, sum)
}

// handleTrashRestore serves POST /binflow/api/trash/restore/{path}. The
// path is trash-can-relative; `to` is the optional /{repo}[/{path}]
// destination override; `transaction-size` the inert batch knob.
func (s *Server) handleTrashRestore(w http.ResponseWriter, r *http.Request, rest string) {
	svc := s.trashFace(w, r)
	if svc == nil {
		return
	}
	raw := strings.TrimPrefix(strings.TrimPrefix(rest, "trash/restore"), "/")
	path, err := url.PathUnescape(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid trash path escaping")
		return
	}
	if strings.TrimSpace(path) == "" {
		writeError(w, http.StatusBadRequest, "Trash path is empty")
		return
	}

	req := repo.TrashRestoreRequest{Path: path}
	q := r.URL.Query()
	if to := q.Get("to"); to != "" {
		trimmed := strings.TrimPrefix(to, "/")
		seg, tail, _ := strings.Cut(trimmed, "/")
		tgtRepo, terr := url.PathUnescape(seg)
		if terr != nil {
			writeError(w, http.StatusBadRequest, "invalid target repository key escaping")
			return
		}
		tgtPath, terr := url.PathUnescape(tail)
		if terr != nil {
			writeError(w, http.StatusBadRequest, "invalid target path escaping")
			return
		}
		req.TargetRepo, req.TargetPath = tgtRepo, tgtPath
	}
	if ts := q.Get("transaction-size"); ts != "" {
		n, perr := strconv.Atoi(ts)
		if perr != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "transaction-size must be a positive integer")
			return
		}
		req.TransactionSize = n
	}

	res, err := svc.TrashRestore(r.Context(), principalFrom(r.Context()), req)
	if err != nil {
		s.writeTrashError(w, err)
		return
	}
	s.writeCopyMoveResult(w, res)
}

// handleTrashClean serves DELETE /binflow/api/trash/clean/{path}.
func (s *Server) handleTrashClean(w http.ResponseWriter, r *http.Request, rest string) {
	svc := s.trashFace(w, r)
	if svc == nil {
		return
	}
	raw := strings.TrimPrefix(strings.TrimPrefix(rest, "trash/clean"), "/")
	path, err := url.PathUnescape(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid trash path escaping")
		return
	}
	if strings.TrimSpace(path) == "" {
		writeError(w, http.StatusBadRequest, "Trash path is empty")
		return
	}
	sum, err := svc.TrashClean(r.Context(), principalFrom(r.Context()), path)
	if err != nil {
		s.writeTrashError(w, err)
		return
	}
	writeJSONBody(w, http.StatusOK, sum)
}

// writeTrashError maps the family's service failures: *StatusError
// verbatim, the sentinel family through the storage-plane mapping, the
// rest an honest 500.
func (s *Server) writeTrashError(w http.ResponseWriter, err error) {
	var se *repo.StatusError
	if errors.As(err, &se) {
		writeError(w, se.Code, se.Message)
		return
	}
	switch {
	case errors.Is(err, repo.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrRepoNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, repo.ErrNodeNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, repo.ErrRepoTypeNotSupported):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, repo.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", basicChallenge)
		writeError(w, http.StatusUnauthorized, "authentication required")
	default:
		s.log.Error("httpapi: trash service failure", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "trash can operation failed")
	}
}
