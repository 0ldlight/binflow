package httpapi

// The Debian management plane (M11 T-310, ADR-0034 / debian.md section
// 4.3): POST /binflow/api/deb/reindex/{repoKey}?async=0|1 — the Calculate
// Debian Index family, mounted on the /binflow/api segment through the
// dispatchAPI EXPLICIT-route family (a handler plus its routeAuth gate,
// NOT an apiProtocolMounts content alias — the same posture /api/yum
// established). Every response body is text/plain (the endpoint family's
// own contract, not the errors[] envelope).
//
// The branch matrix (section 4.3, verbatim wordings where the spec pins
// them):
//
//	repoKey blank          → 400 "Target repository key cannot be blank"
//	anonymous              → 401 (the route's required gate)
//	no MANAGE permission   → 403 (the route's repoManage gate)
//	repo missing           → 404 "Unable to find repository '<key>'."
//	non-debian package type→ 400 "Repository '<key>' doesn't handle debian
//	                          requests."
//	virtual/remote class   → 400 (the remote pull-through and the virtual
//	                          stanza aggregation land with T-314)
//	async=1                → 202 the accepted wording (scheduled in
//	                          background)
//	async=0                → 200 the same wording (synchronous completion)
//
// X-GPG-PASSPHRASE is accepted and ignored in this release: unsigned
// mode (DB-1 — the instance keypair system is K-1, not yet landed); the
// stale Release.gpg/InRelease cleanup the unsigned posture demands runs
// inside every recompute.
//
// The debPUT chain is the AUTOMATIC face (unlike yum's RP-2 opt-in): a
// repository upload already recomputes the affected distributions in the
// background, so this endpoint's role is the whole-repository sweep —
// after property edits through the REST properties family (section 2.3:
// coordinate property changes re-trigger the index), a version upgrade,
// or a repair.

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// debReindexer is the seam: the mounted deb adapter's own method,
// type-asserted off the dispatch map (the /api/yum seam's posture — a
// management route reaching for its protocol's engine through the
// adapter interface keeps httpapi free of any adapter-package import).
type debReindexer interface {
	// ReindexRepository recomputes every distribution's index tree (the
	// caller wraps it in the async posture when the request says async).
	ReindexRepository(ctx context.Context, p *repo.Principal, repoKey string) error
}

// debReindex resolves the mounted adapter's reindex seam; ok is false
// when no deb adapter (or one without the seam) is mounted.
func (s *Server) debReindex() (debReindexer, bool) {
	h, ok := s.adapters["debian"]
	if !ok {
		return nil, false
	}
	ri, ok := h.(debReindexer)
	return ri, ok
}

// The pinned wordings (debian.md sections 3.2 / 4.3).
const (
	msgDebBlankKey      = "Target repository key cannot be blank"
	msgDebAccepted      = "Debian index calculation for repository '%s' accepted."
	msgDebRepoUnfind    = "Unable to find repository '%s'."
	msgDebNotDebian     = "Repository '%s' doesn't handle debian requests."
	msgDebClassUnserved = "Repository '%s' is a %s repository; the remote pull-through and the virtual stanza aggregation land with their own BinFlow ticket (local repositories are served here)."
)

// handleDebReindexBlankKey answers POST /binflow/api/deb/reindex[/] —
// the blank repository key branch (400, the pinned wording).
func (s *Server) handleDebReindexBlankKey(w http.ResponseWriter, _ *http.Request) {
	writePlainText(w, http.StatusBadRequest, msgDebBlankKey)
}

// handleDebReindex serves POST /binflow/api/deb/reindex/{repoKey}.
func (s *Server) handleDebReindex(w http.ResponseWriter, r *http.Request, repoKey string) {
	ri, ok := s.debReindex()
	if !ok {
		writeError(w, http.StatusNotImplemented, "debian repositories are not served by this instance")
		return
	}
	row, err := s.deps.Repos.Get(r.Context(), repoKey)
	if err != nil {
		writePlainText(w, http.StatusNotFound, fmt.Sprintf(msgDebRepoUnfind, repoKey))
		return
	}
	if row.PackageType != "debian" {
		writePlainText(w, http.StatusBadRequest, fmt.Sprintf(msgDebNotDebian, repoKey))
		return
	}
	if row.Type != repo.TypeLocal {
		writePlainText(w, http.StatusBadRequest, fmt.Sprintf(msgDebClassUnserved, repoKey, row.Type))
		return
	}
	// async: absent or 0 = synchronous; 1 = asynchronous; anything else is
	// a malformed request.
	async := r.URL.Query().Get("async")
	switch async {
	case "", "0", "1":
	default:
		writeError(w, http.StatusBadRequest, "async must be 0 or 1")
		return
	}
	if async == "1" {
		p := principalFrom(r.Context())
		//nolint:gosec // G118: the recompute deliberately detaches from
		// the request's lifetime — the async posture must survive the
		// client hanging up, and the per-repo index lock serializes runs.
		go func() {
			if err := ri.ReindexRepository(context.Background(), p, repoKey); err != nil {
				s.log.Error("deb: async index calculation failed", "repo", repoKey, "error", err.Error())
			}
		}()
		writePlainText(w, http.StatusAccepted, fmt.Sprintf(msgDebAccepted, repoKey))
		return
	}
	p := principalFrom(r.Context())
	if err := ri.ReindexRepository(r.Context(), p, repoKey); err != nil {
		switch {
		case errors.Is(err, repo.ErrRepoNotFound), errors.Is(err, metadata.ErrRepoNotFound):
			writePlainText(w, http.StatusNotFound, fmt.Sprintf(msgDebRepoUnfind, repoKey))
		default:
			s.log.Error("deb: index calculation failed", "repo", repoKey, "error", err.Error())
			writeError(w, http.StatusInternalServerError, "debian index calculation failed: "+err.Error())
		}
		return
	}
	writePlainText(w, http.StatusOK, fmt.Sprintf(msgDebAccepted, repoKey))
}
