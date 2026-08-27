package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The Helm management plane (M11 T-309, ADR-0034's first clause): the two
// reindex endpoints JFrog documents, mounted on the /binflow/api segment
// through the dispatchAPI EXPLICIT-route family — a handler plus its
// routeAuth gate, NOT the apiProtocolMounts content alias (the two lists
// carry different semantics: management = CanManageRepo, content =
// rewrite + adapter dispatch).
//
//	helmReindexer is the seam: the mounted classic-helm adapter's own
//	methods, type-asserted off the dispatch map (the assembly already
//	passed the handler in Deps.Adapters; a management route reaching for
//	its protocol's engine through the adapter interface keeps httpapi free
//	of any adapter-package import).
type helmReindexer interface {
	// ReindexRepository rebuilds the whole-repo index (the caller wraps
	// it in the async posture).
	ReindexRepository(ctx context.Context, p *repo.Principal, repoKey string) error
	// ReindexPath rebuilds one path's slice synchronously.
	ReindexPath(ctx context.Context, p *repo.Principal, repoKey, path string) error
}

// helmReindex resolves the mounted adapter's reindex seam; ok is false
// when no helm adapter (or one without the seam) is mounted.
func (s *Server) helmReindex() (helmReindexer, bool) {
	h, ok := s.adapters["helm"]
	if !ok {
		return nil, false
	}
	ri, ok := h.(helmReindexer)
	return ri, ok
}

// handleHelmReindex serves POST /binflow/api/helm/{repoKey}/reindex —
// the whole-repository rebuild, dispatched ASYNC (JFrog's Calculate Helm
// Chart Index semantics: the 200 acknowledges the scheduling, the rebuild
// lands in the background; the completion is observable as the new
// index.yaml).
func (s *Server) handleHelmReindex(w http.ResponseWriter, r *http.Request, repoKey string) {
	ri, ok := s.helmReindex()
	if !ok {
		writePlainText(w, http.StatusNotImplemented, "helm repositories are not served by this instance")
		return
	}
	if !s.checkHelmReindexTarget(w, r, repoKey) {
		return
	}
	p := principalFrom(r.Context())
	//nolint:gosec // G118: the goroutine deliberately detaches from the
	// request's lifetime — the async posture must survive the client
	// hanging up, and the per-repo index lock plus the whole-file rewrite
	// keep an aborted run's retry sound.
	go func() {
		if err := ri.ReindexRepository(context.Background(), p, repoKey); err != nil {
			s.log.Error("helm: repository reindex failed", "repo", repoKey, "error", err.Error())
		}
	}()
	writePlainText(w, http.StatusOK,
		"Helm chart index calculation for repository '"+repoKey+"' has been scheduled.")
}

// handleHelmReindexPath serves POST /binflow/api/helm/{repoKey}/reindex/{path}
// — the partial, SYNCHRONOUS rebuild (JFrog's Helm Charts Partial
// Re-Indexing): the response returns after the path's entries are
// recomputed.
func (s *Server) handleHelmReindexPath(w http.ResponseWriter, r *http.Request, repoKey, nodePath string) {
	ri, ok := s.helmReindex()
	if !ok {
		writePlainText(w, http.StatusNotImplemented, "helm repositories are not served by this instance")
		return
	}
	if !s.checkHelmReindexTarget(w, r, repoKey) {
		return
	}
	if err := ri.ReindexPath(r.Context(), principalFrom(r.Context()), repoKey, nodePath); err != nil {
		switch {
		case errors.Is(err, repo.ErrNodeNotFound):
			writeError(w, http.StatusNotFound,
				"Failed to find the path '"+nodePath+"' specified in the reindex request.")
		case errors.Is(err, repo.ErrRepoNotFound), errors.Is(err, metadata.ErrRepoNotFound):
			writeError(w, http.StatusNotFound,
				"Failed to find the repository '"+repoKey+"' specified in the request.")
		default:
			s.log.Error("helm: path reindex failed", "repo", repoKey, "path", nodePath, "error", err.Error())
			writeError(w, http.StatusInternalServerError, "helm reindex failed: "+err.Error())
		}
		return
	}
	writePlainText(w, http.StatusOK,
		"Helm chart index calculation for repository '"+repoKey+"' path '"+nodePath+"' completed.")
}

// checkHelmReindexTarget validates the repository row: a helm LOCAL
// repository (the reindex family serves local only — JFrog's
// local/federated set; a remote/virtual row answers the 400, a missing
// row the 404 envelope). false means the request was refused.
func (s *Server) checkHelmReindexTarget(w http.ResponseWriter, r *http.Request, repoKey string) bool {
	row, err := s.deps.Repos.Get(r.Context(), repoKey)
	if err != nil {
		s.writeRepoLookupError(w, repoKey, err)
		return false
	}
	if row.PackageType != "helm" {
		writeError(w, http.StatusBadRequest,
			"Repository '"+repoKey+"' is a "+row.PackageType+" repository; the helm reindex family serves helm repositories.")
		return false
	}
	if row.Type != repo.TypeLocal {
		writeError(w, http.StatusBadRequest,
			"Repository '"+repoKey+"' is a "+row.Type+" repository; the helm reindex family serves local repositories only.")
		return false
	}
	return true
}

// splitHelmReindexPath extracts the repo-relative node path off the
// reindex/{path...} tail, percent-decoded; ok is false for an empty or
// undecodable tail.
func splitHelmReindexPath(tail string) (string, bool) {
	raw := strings.TrimPrefix(tail, "reindex/")
	if raw == "" {
		return "", false
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil || decoded == "" || strings.Contains(decoded, "..") {
		return "", false
	}
	return strings.TrimPrefix(decoded, "/"), true
}
