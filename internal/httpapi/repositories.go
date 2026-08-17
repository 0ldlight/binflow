package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// Artifactory-compatible repository CRUD (rest-api.md section 2; PRD
// E-04..E-08). All routes demand authentication; repository mutations are
// additionally admin-only — enforced by the route gates in router.go, not
// re-checked here (the terminal handlers run only after the gate passed).
//
// Success bodies for PUT/DELETE are plain text (spec wording); failures use
// the errors[] envelope (the repository plane belongs to the generic error
// layer, PRD section 5.1 three-format split).

// repoListItem is one GET /api/repositories entry (rest-api.md section 2,
// RepoDetails subset): type is lowercase, url is the context URL of the repo.
type repoListItem struct {
	Key           string `json:"key"`
	Description   string `json:"description"`
	Type          string `json:"type"`
	PackageType   string `json:"packageType"`
	URL           string `json:"url"`
	Configuration any    `json:"configuration,omitempty"`
}

// repoConfig is the single-repository configuration body (GET and PUT share
// the shape; rclass echoes the BinFlow row's type).
type repoConfig struct {
	Key                     string `json:"key"`
	RClass                  string `json:"rclass"`
	PackageType             string `json:"packageType"`
	Description             string `json:"description"`
	URL                     string `json:"url"`
	Notes                   string `json:"notes,omitempty"`
	IncludesPattern         string `json:"includesPattern,omitempty"`
	ExcludesPattern         string `json:"excludesPattern,omitempty"`
	RepoLayoutRef           string `json:"repoLayoutRef,omitempty"`
	BlackedOut              *bool  `json:"blackedOut,omitempty"`
	HandleReleases          *bool  `json:"handleReleases,omitempty"`
	HandleSnapshots         *bool  `json:"handleSnapshots,omitempty"`
	SnapshotVersionBehavior string `json:"snapshotVersionBehavior,omitempty"`
	MaxUniqueSnapshots      int    `json:"maxUniqueSnapshots,omitempty"`
	ChecksumPolicyType      string `json:"checksumPolicyType,omitempty"`
	ArchiveBrowsingEnabled  *bool  `json:"archiveBrowsingEnabled,omitempty"`
}

// repoTypeOrder ranks types for the list ordering: type ascending, then key
// ascending (rest-api.md section 2). M1 has local only; the full ordering
// table keeps the contract stable the day remote/virtual land.
var repoTypeOrder = map[string]int{
	repo.TypeLocal:   0,
	repo.TypeRemote:  1,
	repo.TypeVirtual: 2,
}

// writeText emits a plain-text success body (repo-management plane).
func writeText(w http.ResponseWriter, status int, body string) {
	writePlainText(w, status, body)
}

// writePlainText is the single plain-text emitter: the nosniff guard keeps
// browsers from content-sniffing these bodies as HTML (gosec G705 posture —
// several of these messages interpolate client-supplied identifiers).
func writePlainText(w http.ResponseWriter, status int, body string) {
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body)) //nolint:gosec // G705: body is operator-controlled plain text under nosniff + text/plain
}

// writeJSONBody emits an indented JSON success body.
func writeJSONBody(w http.ResponseWriter, status int, v any) {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render response: "+err.Error())
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writePlainError emits the plain-text error body of the user/permission
// management plane (PRD section 5.1 three-format split, layer 2).
func writePlainError(w http.ResponseWriter, status int, message string) {
	writePlainText(w, status, message)
}

// handleRepoList serves GET /api/repositories (E-04): every repository the
// caller can see (M1: all of them — the read filter needs per-repo ACL data
// M1 does not carry; the route itself already requires authentication),
// ordered type-then-key, Cache-Control: no-store (rest-api.md section 2).
func (s *Server) handleRepoList(w http.ResponseWriter, r *http.Request) {
	repos, err := s.deps.ReposSvc.ListRepos(r.Context(), principalFrom(r.Context()))
	if err != nil {
		s.writeRepoSvcError(w, err)
		return
	}
	items := make([]repoListItem, 0, len(repos))
	for _, row := range repos {
		items = append(items, s.repoListItemOf(r, row))
	}
	sort.SliceStable(items, func(i, j int) bool {
		ti, tj := repoTypeOrder[items[i].Type], repoTypeOrder[items[j].Type]
		if ti != tj {
			return ti < tj
		}
		return items[i].Key < items[j].Key
	})
	w.Header().Set("Cache-Control", "no-store")
	writeJSONBody(w, http.StatusOK, items)
}

// repoListItemOf projects one metadata row onto the wire shape.
func (s *Server) repoListItemOf(r *http.Request, row *metadata.Repo) repoListItem {
	return repoListItem{
		Key:         row.RepoKey,
		Description: row.Description,
		Type:        row.Type,
		PackageType: row.PackageType,
		URL:         requestBase(r) + "/" + row.RepoKey,
	}
}

// requestBase is scheme://host as the request presented it (URLs inside
// bodies are derived from the request, never from a configured base in M1;
// config.Server.BaseURL wiring lands with the console milestone).
func requestBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// handleRepoGet serves GET /api/repositories/{key} (E-05): the full config
// body; unknown key -> 404 with the spec's plain wording wrapped in the
// envelope (BinFlow keeps the envelope for the repository plane, E-01).
func (s *Server) handleRepoGet(w http.ResponseWriter, r *http.Request, key string) {
	row, err := s.deps.ReposSvc.GetRepo(r.Context(), principalFrom(r.Context()), key)
	if err != nil {
		s.writeRepoSvcError(w, err)
		return
	}
	writeJSONBody(w, http.StatusOK, s.repoConfigOf(r, row))
}

// repoConfigOf projects one row onto the configuration body.
func (s *Server) repoConfigOf(r *http.Request, row *metadata.Repo) repoConfig {
	return repoConfig{
		Key:         row.RepoKey,
		RClass:      row.Type,
		PackageType: row.PackageType,
		Description: row.Description,
		URL:         requestBase(r) + "/" + row.RepoKey,
	}
}

// handleRepoPut serves PUT /api/repositories/{key} (E-06/E-07): create, or
// update when the key exists — both answer 200 plain text (PRD v1.3
// calibration R2; the create-vs-update wording distinction is kept so
// scripts can log accurately). remote/virtual rclass -> 400 (E-07, M1).
func (s *Server) handleRepoPut(w http.ResponseWriter, r *http.Request, key string) {
	var body repoConfig
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "request body is not valid repository configuration JSON: "+err.Error())
		return
	}
	// PRD v1.3: BinFlow does not distinguish the body/path key mismatch
	// cases (400 create / 409 update) — one uniform 400.
	if body.Key != "" && body.Key != key {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"repository key in body %q does not match the request path %q", body.Key, key))
		return
	}

	p := principalFrom(r.Context())
	stored := &metadata.Repo{
		RepoKey: key, Type: body.RClass, PackageType: body.PackageType,
		Description: body.Description,
	}

	// Update path first: an existing key makes this an update regardless of
	// the body's completeness (PUT is the documented M1 update spelling too).
	current, getErr := s.deps.ReposSvc.GetRepo(r.Context(), p, key)
	switch {
	case getErr == nil:
		if stored.Type == "" {
			stored.Type = current.Type
		}
		if stored.PackageType == "" {
			stored.PackageType = current.PackageType
		}
		if _, err := s.deps.ReposSvc.UpdateRepo(r.Context(), p, stored); err != nil {
			s.writeRepoSvcError(w, err)
			return
		}
		writeText(w, http.StatusOK, fmt.Sprintf("Repository %s update successfully.\n", key))
		return
	case !errors.Is(getErr, repo.ErrRepoNotFound):
		s.writeRepoSvcError(w, getErr)
		return
	}

	created, err := s.deps.ReposSvc.CreateRepo(r.Context(), p, stored)
	if err != nil {
		s.writeRepoSvcError(w, err)
		return
	}
	writeText(w, http.StatusOK, fmt.Sprintf("Successfully created repository '%s'\n", created.RepoKey))
}

// handleRepoPost serves POST /api/repositories/{key} (update spelling,
// rest-api.md section 2): 404 when the key is unknown.
func (s *Server) handleRepoPost(w http.ResponseWriter, r *http.Request, key string) {
	var body repoConfig
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "request body is not valid repository configuration JSON: "+err.Error())
		return
	}
	p := principalFrom(r.Context())
	current, err := s.deps.ReposSvc.GetRepo(r.Context(), p, key)
	if err != nil {
		s.writeRepoSvcError(w, err)
		return
	}
	rclass := body.RClass
	if rclass == "" {
		rclass = current.Type
	}
	packageType := body.PackageType
	if packageType == "" {
		packageType = current.PackageType
	}
	if _, err := s.deps.ReposSvc.UpdateRepo(r.Context(), p, &metadata.Repo{
		RepoKey: key, Type: rclass, PackageType: packageType,
		Description: body.Description,
	}); err != nil {
		s.writeRepoSvcError(w, err)
		return
	}
	writeText(w, http.StatusOK, fmt.Sprintf("Repository %s update successfully.\n", key))
}

// handleRepoDelete serves DELETE /api/repositories/{key} (E-08): empty
// repositories delete directly; non-empty ones demand ?deleteContent=true
// (the 400 message names the flag, FR-3-AC5). Success is a 200 plain-text
// report (rest-api.md section 2).
func (s *Server) handleRepoDelete(w http.ResponseWriter, r *http.Request, key string) {
	deleteContent := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("deleteContent")), "true")
	if err := s.deps.ReposSvc.DeleteRepo(r.Context(), principalFrom(r.Context()), key, deleteContent); err != nil {
		s.writeRepoSvcError(w, err)
		return
	}
	writeText(w, http.StatusOK, fmt.Sprintf("Repository %s deleted successfully.\n", key))
}

// writeRepoSvcError maps repo.Service sentinels onto envelope statuses.
func (s *Server) writeRepoSvcError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repo.ErrRepoNotFound):
		writeError(w, http.StatusNotFound, "Repository does not exist: "+err.Error())
	case errors.Is(err, repo.ErrInvalidRepoKey), errors.Is(err, repo.ErrReservedRepoKey),
		errors.Is(err, repo.ErrInvalidRepoConfig), errors.Is(err, repo.ErrInvalidRepoType):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrRepoTypeNotSupported):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrRepoExists):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrRepoNotEmpty):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", basicChallenge)
		writeError(w, http.StatusUnauthorized, "authentication required")
	case errors.Is(err, repo.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		s.log.Error("httpapi: repository service failure", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "repository operation failed")
	}
}
