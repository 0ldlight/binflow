package httpapi

// The YUM management plane (M11 T-311, ADR-0034 / rpm.md section 3.2):
// POST /binflow/api/yum/{repoKey}?path=<path>&async=<0|1> — the Calculate
// YUM Repository Metadata family JFrog documents, mounted on the
// /binflow/api segment through the dispatchAPI EXPLICIT-route family (a
// handler plus its routeAuth gate, NOT the apiProtocolMounts content
// alias). Every response body is text/plain (the endpoint family's own
// contract, not the errors[] envelope).
//
// The branch matrix (section 3.2, verbatim wordings where the spec pins
// them):
//
//	repoKey blank          → 400 "Target repository key cannot be blank"
//	anonymous              → 401 (the route's required gate)
//	no MANAGE permission   → 403 (the route's repoManage gate)
//	repo missing / non-rpm → 404 "Unable to find repository '<key>'."
//	virtual/remote class   → 400 (the virtual 202/200 arms land with the
//	                          virtual aggregation's own ticket — registered
//	                          divergence, T-311 report)
//	async=1                → 202 "YUM metadata calculation for repository
//	                          '<key>' accepted." (scheduled in background)
//	async=0 + auto-calc on → 409 "Unable to perform immediate YUM metadata
//	                          calculation on a repository with auto-async
//	                          calculation enabled."
//	async=0 + auto-calc off→ 200 the 202 wording (synchronous completion)
//
// X-GPG-PASSPHRASE is accepted and ignored in this release: unsigned mode
// (RP-1/DB-1 — the instance keypair system is K-1, not yet landed); the
// stale repomd.xml.asc/.key cleanup the unsigned posture demands runs
// inside the recompute.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// yumReindexer is the seam: the mounted rpm adapter's own method,
// type-asserted off the dispatch map (the assembly already passed the
// handler in Deps.Adapters; a management route reaching for its
// protocol's engine through the adapter interface keeps httpapi free of
// any adapter-package import).
type yumReindexer interface {
	// ReindexRepository recomputes every yum root's repodata (the caller
	// wraps it in the async posture when the request says async).
	ReindexRepository(ctx context.Context, p *repo.Principal, repoKey string) error
}

// yumReindex resolves the mounted adapter's reindex seam; ok is false when
// no rpm adapter (or one without the seam) is mounted.
func (s *Server) yumReindex() (yumReindexer, bool) {
	h, ok := s.adapters["rpm"]
	if !ok {
		return nil, false
	}
	ri, ok := h.(yumReindexer)
	return ri, ok
}

// The pinned wordings (rpm.md section 3.2).
const (
	msgYumBlankKey      = "Target repository key cannot be blank"
	msgYumAccepted      = "YUM metadata calculation for repository '%s' accepted."
	msgYumAutoAsync     = "Unable to perform immediate YUM metadata calculation on a repository with auto-async calculation enabled."
	msgYumRepoUnfind    = "Unable to find repository '%s'."
	msgYumClassUnserved = "Repository '%s' is a %s repository; YUM metadata calculation on virtual repositories lands with the virtual aggregation's own BinFlow ticket (local repositories are served here)."
)

// handleYumReindexBlankKey answers POST /binflow/api/yum[/] — the blank
// repository key branch (400, the pinned wording).
func (s *Server) handleYumReindexBlankKey(w http.ResponseWriter, _ *http.Request) {
	writePlainText(w, http.StatusBadRequest, msgYumBlankKey)
}

// handleYumReindex serves POST /binflow/api/yum/{repoKey}.
func (s *Server) handleYumReindex(w http.ResponseWriter, r *http.Request, repoKey string) {
	ri, ok := s.yumReindex()
	if !ok {
		writeError(w, http.StatusNotImplemented, "rpm repositories are not served by this instance")
		return
	}
	row, err := s.deps.Repos.Get(r.Context(), repoKey)
	if err != nil {
		writePlainText(w, http.StatusNotFound, fmt.Sprintf(msgYumRepoUnfind, repoKey))
		return
	}
	if row.PackageType != "rpm" {
		writePlainText(w, http.StatusNotFound, fmt.Sprintf(msgYumRepoUnfind, repoKey))
		return
	}
	if row.Type != repo.TypeLocal {
		writePlainText(w, http.StatusBadRequest, fmt.Sprintf(msgYumClassUnserved, repoKey, row.Type))
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
				s.log.Error("rpm: async yum metadata calculation failed", "repo", repoKey, "error", err.Error())
			}
		}()
		writePlainText(w, http.StatusAccepted, fmt.Sprintf(msgYumAccepted, repoKey))
		return
	}
	if yumAutoCalcEnabled(row.Config) {
		writePlainText(w, http.StatusConflict, msgYumAutoAsync)
		return
	}
	p := principalFrom(r.Context())
	if err := ri.ReindexRepository(r.Context(), p, repoKey); err != nil {
		switch {
		case errors.Is(err, repo.ErrRepoNotFound), errors.Is(err, metadata.ErrRepoNotFound):
			writePlainText(w, http.StatusNotFound, fmt.Sprintf(msgYumRepoUnfind, repoKey))
		default:
			s.log.Error("rpm: yum metadata calculation failed", "repo", repoKey, "error", err.Error())
			writeError(w, http.StatusInternalServerError, "yum metadata calculation failed: "+err.Error())
		}
		return
	}
	writePlainText(w, http.StatusOK, fmt.Sprintf(msgYumAccepted, repoKey))
}

// yumAutoCalcEnabled reads calculateYumMetadata off the repository's rpm
// config section (RP-2's final ruling: the DEFAULT IS FALSE — the flag is
// the explicit opt-in).
func yumAutoCalcEnabled(config string) bool {
	var probe struct {
		CalculateYumMetadata bool `json:"calculateYumMetadata"`
	}
	if err := json.Unmarshal([]byte(config), &probe); err != nil {
		return false
	}
	return probe.CalculateYumMetadata
}
