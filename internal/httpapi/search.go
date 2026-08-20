package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// Artifactory-compatible artifact search (rest-api.md section 4; PRD M4
// FR-26 / SR-01/SR-02, T-92). Two entrances open the M1 E-26 search domain:
//
//	GET /binflow/api/search/artifact?name=<frag>&repos=<csv>   SR-01 (W14)
//	GET /binflow/api/search/checksum?sha256=&sha1=&md5=&repos= SR-02 (W15)
//
// Both answer 200 {"results":[FileInfo...]} — the E-09 field set verbatim
// (storage.go's fileInfoOf) — and every other family member
// (props/users/artifactory/pattern/badge/...) stays on the E-26 404: the
// M4 search domain deliberately opens exactly these two doors (SR-04, an
// intentional incompatibility — property and user search are M5+ scope).
//
// Matching semantics are K2's provisional ruling: a literal, case-sensitive
// path substring via SQL LIKE (`*` wildcards and gavc are P2); the checksum
// arm is exact digest matching (rest-api.md section 4, high confidence).
//
// Read authorization rides the content-plane decision inside the service use
// case (per-node read ACL, admin sees all, anonymous follows the
// anonymous_access channel): the route gate itself stays open like
// /api/storage's, so the closed-instance anonymous caller meets the spec's
// 403 instead of a 401 challenge that would mask it.

// searchResults is the {"results":[FileInfo...]} envelope of both search
// entrances. results is always a JSON array — an empty search answers [] ,
// never null (W16's `.results | length` probe).
type searchResults struct {
	Results []fileInfoBody `json:"results"`
}

// handleSearchArtifact serves GET /api/search/artifact (SR-01). name is
// required — a missing or blank value answers the E-01 400; repos optionally
// narrows the search to a comma-separated repository list (unknown keys
// simply match nothing, the E-04 filter posture).
func (s *Server) handleSearchArtifact(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name := q.Get("name")
	if strings.TrimSpace(name) == "" {
		writeError(w, http.StatusBadRequest, "Artifact search requires a non-empty 'name' query parameter.")
		return
	}
	repos := searchReposFilter(q.Get("repos"))
	p := principalFrom(r.Context())
	nodes, err := s.deps.ReposSvc.SearchArtifacts(r.Context(), p, name, repos)
	if err != nil {
		s.writeSearchError(w, err)
		return
	}
	s.writeSearchResults(w, r, nodes)
}

// handleSearchChecksum serves GET /api/search/checksum (SR-02). At least one
// of sha256/sha1/md5 must be present and each present digest must be bare
// hex of its algorithm's length — anything else answers the E-01 400
// (rest-api.md section 4). repos narrows like the artifact arm.
func (s *Server) handleSearchChecksum(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := repo.ChecksumQuery{
		Sha256: q.Get("sha256"),
		Sha1:   q.Get("sha1"),
		Md5:    q.Get("md5"),
	}
	repos := searchReposFilter(q.Get("repos"))
	p := principalFrom(r.Context())
	nodes, err := s.deps.ReposSvc.SearchChecksum(r.Context(), p, query, repos)
	if err != nil {
		s.writeSearchError(w, err)
		return
	}
	s.writeSearchResults(w, r, nodes)
}

// searchReposFilter splits the repos csv. An absent or blank parameter means
// "every repository"; blank entries are dropped so "a,,b" behaves like "a,b".
func searchReposFilter(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// writeSearchResults renders the results envelope through the E-09 FileInfo
// projection.
func (s *Server) writeSearchResults(w http.ResponseWriter, r *http.Request, nodes []*metadata.Node) {
	base := requestBase(r)
	results := make([]fileInfoBody, 0, len(nodes))
	for _, n := range nodes {
		results = append(results, s.fileInfoOf(r.Context(), base, n.RepoKey, n))
	}
	writeJSONBody(w, http.StatusOK, searchResults{Results: results})
}

// writeSearchError maps the search use cases' failures: query-shape problems
// answer 400 (E-01), the anonymous-closed denial keeps the content-plane 403
// mapping, and everything else falls through the storage plane's mapper
// (404/500 with logging).
func (s *Server) writeSearchError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repo.ErrInvalidSearchQuery):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrSearchUnavailable):
		s.log.Error("httpapi: search unavailable on the configured store", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "search is not available on this instance")
	default:
		s.writeStorageError(w, err)
	}
}
