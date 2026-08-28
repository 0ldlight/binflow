package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The copy/move REST family (M12 T-339, FR-105.1; the behavior spec is
// docs/reverse/repo-operations.md sections 0/1). Two endpoints, synchronous
// 200 + messages[] streaming — there is deliberately NO 202/async task form
// (the spec's section 7 terminal ruling):
//
//	POST /binflow/api/copy/{srcRepo}[/{srcPath}]?to=/{targetRepo}[/{targetPath}]
//	POST /binflow/api/move/{srcRepo}[/{srcPath}]?to=/{targetRepo}[/{targetPath}]
//
// Query parameters (section 0's table): to (REQUIRED), dry (0/1, default 0),
// failFast (0/1, default 0), suppressLayouts (0/1, default 1 — parsed for
// wire parity; BinFlow has no cross-layout translation, the registered
// divergence), atomic (code-only evidence, mid confidence — registered, no
// behavior: BinFlow's per-item pipeline already matches the atomic=false
// partial-success posture).
//
// The Q4 final ruling (BOARD M12, user 2026-08-28): the operations family
// carries Artifactory's pro entitlement posture, gated through the addon
// three-seam precedent of T-282/283 — the community shape is the D4 403 +
// X-Binflow-License-Required header (license_gate.go's closed set), NOT
// Artifactory's own 400 text/plain addon refusal (BinFlow's uniform gate
// rendering; the divergence is deliberate and registered). The slot is the
// feature-kind "repo-operations" descriptor — the whole family (copy/move
// now, the archive trio of T-343 next) rides ONE entitlement, mirroring the
// spec's section 6 finding that Artifactory lists the entire family at pro.

// addonRepoOperations is the operations family's feature slot, single-sourced
// from the addons constructor (FR-86-AC2: one spelling of the id/tier pair).
var addonRepoOperations = addons.RepoOperations()

// copyMoveRunner is the service capability face (consumer-side interface,
// asserted off Deps.ReposSvc — the big Service interface stays untouched so
// the hand-written adapter test fakes that implement it method by method do
// not break).
type copyMoveRunner interface {
	CopyOrMove(ctx context.Context, p *repo.Principal, req repo.CopyMoveRequest) (*repo.CopyMoveResult, error)
}

// handleCopyMove serves POST /binflow/api/{copy,move}/**: the license gate
// (after the route's authentication door — RBAC precedes license, the T-283
// ruling), then the request parse, then the service pipeline, then the
// messages[] rendering. The route passed op ("copy"/"move") and rest — the
// raw, still-escaped remainder after /api/{op}/.
func (s *Server) handleCopyMove(w http.ResponseWriter, r *http.Request, op, rest string) {
	// The entitlement gate is the handler's first line (weave 3,
	// license_gate.go) — community answers the D4 403 shape.
	if !s.RequireAddon(w, r, addonRepoOperations.ID, addonRepoOperations.MinTier) {
		return
	}

	svc, ok := s.deps.ReposSvc.(copyMoveRunner)
	if !ok {
		// A ReposSvc without the operations face: a unit stack or an
		// assembly gap — the honest 501, the deb/helm management-face
		// posture for a missing collaborator.
		writeError(w, http.StatusNotImplemented, "copy/move is not available on this instance")
		return
	}

	// ---- stage 1 (the wire half): path and query parse ----
	// Strip BOTH spellings of the op prefix so the bare /api/copy (an
	// empty source key) reaches the service's §1.1 400, not a bogus
	// "copy" repository key.
	stripped := strings.TrimPrefix(strings.TrimPrefix(rest, op), "/")
	seg, tail, _ := strings.Cut(stripped, "/")
	srcRepo, err := url.PathUnescape(seg)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid source repository key escaping")
		return
	}
	srcPath, err := url.PathUnescape(tail)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid source path escaping")
		return
	}
	if srcRepo == "" {
		// §1.1's FIRST 400 — the source key precedes the target pair.
		writeError(w, http.StatusBadRequest, "Source repository key is empty")
		return
	}

	q := r.URL.Query()
	to := q.Get("to")
	if strings.TrimSpace(to) == "" {
		// Section 1.1's verbatim 400 (the missing "to").
		writeError(w, http.StatusBadRequest, "Target repository key is empty")
		return
	}
	trimmed := strings.TrimPrefix(to, "/")
	tgtSeg, tgtTail, _ := strings.Cut(trimmed, "/")
	tgtRepo, err := url.PathUnescape(tgtSeg)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid target repository key escaping")
		return
	}
	// The trailing-slash signal of the explicit into-directory target rides
	// the raw spelling (section 1.4): keep it on the request's TargetPath.
	tgtPath := tgtTail

	req := repo.CopyMoveRequest{
		Op:              op,
		SrcRepo:         srcRepo,
		SrcPath:         srcPath,
		TargetRepo:      tgtRepo,
		TargetPath:      tgtPath,
		DryRun:          isTruthyFlag(q.Get("dry")),
		FailFast:        isTruthyFlag(q.Get("failFast")),
		SuppressLayouts: !isFalsyOrZero(q.Get("suppressLayouts")), // default 1
	}

	res, err := svc.CopyOrMove(r.Context(), principalFrom(r.Context()), req)
	if err != nil {
		s.writeCopyMoveError(w, err)
		return
	}
	s.writeCopyMoveResult(w, res)
}

// isFalsyOrZero reads the default-on flag spelling: absent, "1" or "true"
// means ON; "0"/"false" turns it off (suppressLayouts' default is 1).
func isFalsyOrZero(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false":
		return true
	}
	return false
}

// copyMoveResultBody is the wire shape of section 1.5: messages[] with
// level+message only (the status codes drive the HTTP status, they are not
// per-entry body fields — V-3's level casing is the logback enum spelling).
type copyMoveResultBody struct {
	Messages []copyMoveMessageBody `json:"messages"`
}

type copyMoveMessageBody struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// contentTypeCopyOrMove is the documented media type of the family
// (application/vnd.org.jfrog.artifactory.storage.CopyOrMoveResult+json;
// application/json is the tolerated alternate — BinFlow serves the specific
// type).
const contentTypeCopyOrMove = "application/vnd.org.jfrog.artifactory.storage.CopyOrMoveResult+json"

// writeCopyMoveResult renders the aggregated messages[] with the §1.5 status
// decision (the last error's code, the 409 fallback, else 200).
func (s *Server) writeCopyMoveResult(w http.ResponseWriter, res *repo.CopyMoveResult) {
	body := copyMoveResultBody{Messages: make([]copyMoveMessageBody, 0, len(res.Messages))}
	for _, m := range res.Messages {
		body.Messages = append(body.Messages, copyMoveMessageBody{Level: m.Level, Message: m.Message})
	}
	out, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render copy/move result: "+err.Error())
		return
	}
	h := w.Header()
	h.Set("Content-Type", contentTypeCopyOrMove)
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(res.HTTPStatus)
	_, _ = w.Write(out)
}

// writeCopyMoveError maps the pipeline's FATAL refusals: the service's
// *StatusError already carries the exact rendering (the spec's verbatim
// precheck messages); the wrapped sentinals keep their /api/storage-family
// mapping; everything else is an honest 500.
func (s *Server) writeCopyMoveError(w http.ResponseWriter, err error) {
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
	case errors.Is(err, repo.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, repo.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", basicChallenge)
		writeError(w, http.StatusUnauthorized, "authentication required")
	default:
		s.log.Error("httpapi: copy/move service failure", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "copy/move operation failed")
	}
}
