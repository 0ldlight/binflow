package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/search"
)

// The D03 quick-win search doors (L024-3A, aql.md §16.2–§16.5 — the wire
// contract the L024-1 session froze):
//
//	GET /binflow/api/search/versions?g=&a=&v=&repos=       §16.2 (D03-R10)
//	GET /binflow/api/search/latestVersion?g=&a=&v=&repos=  §16.3 (D03-R11)
//	GET /binflow/api/versions/{repoKey}/{path}?listFiles=  §16.4 (D03-R12)
//	GET /binflow/api/search/badChecksum?type=&repos=        §16.5-R13
//
// versions/latestVersion share one kernel (repo.SearchVersions — the same
// getArtifactVersions single source Artifactory runs) and differ only in
// envelope: the versions row is the two-key thin {"version","integration"}
// new→old, latestVersion is text/plain with the bare version string. The
// empty-set verdict is the 404 family's "Unable to find artifact versions"
// (live-verbatim), and §16.2's execution order holds: the v-pattern filter
// runs only AFTER that verdict, so a filtered-to-empty page stays 200 [].

// The versions family's verbatim copies (aql.md §16.2/§16.3).
const (
	msgNoArtifactVersions        = "Unable to find artifact versions"     // empty set, live-verbatim
	msgLatestReleaseNotFound     = "Latest release version not found"     // v absent + no release (decompiled, medium)
	msgLatestIntegrationNotFound = "Latest integration version not found" // wildcard miss (decompiled, medium)
)

// badChecksum's verbatim 400 copies (aql.md §16.5-R13, live) plus the
// official response cap.
const (
	msgNoChecksumType = "No checksum type defined"
	badChecksumRowCap = 10000
)

// checksumAuditor is the badChecksum kernel seam (asserted off
// Deps.ReposSvc, the legacySearcher precedent — repo.ChecksumAuditService).
type checksumAuditor interface {
	AuditChecksums(ctx context.Context, p *repo.Principal, typ string, limit int, repos []string) ([]repo.ChecksumMismatch, error)
}

// versionsSearcher is the versions kernel seam (the same precedent —
// repo.VersionSearchService).
type versionsSearcher interface {
	SearchVersions(ctx context.Context, p *repo.Principal, g, a string, limit int, repos []string) ([]repo.ArtifactVersion, error)
}

// versionSearchParams reads and validates the shared g/a pair; the bool is
// false once the 400 has been written.
func versionSearchParams(w http.ResponseWriter, r *http.Request) (g, a string, ok bool) {
	q := r.URL.Query()
	g, a = q.Get("g"), q.Get("a")
	if strings.TrimSpace(g) == "" || strings.TrimSpace(a) == "" {
		writeError(w, http.StatusBadRequest, "Version search requires non-empty 'g' and 'a' query parameters.")
		return "", "", false
	}
	return g, a, true
}

// handleSearchVersions serves GET /api/search/versions (§16.2): g and a
// both required; v an optional wildcard pattern filtered AFTER the
// empty-set verdict (filtered-empty stays 200 []); repos narrows; remote is
// accepted and ignored (no remote search plane — the C-layer reading,
// registered in the ticket log).
func (s *Server) handleSearchVersions(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.deps.ReposSvc.(versionsSearcher)
	if !ok {
		s.log.ErrorContext(r.Context(), "httpapi: version search endpoint reached but the service carries no version face")
		writeError(w, http.StatusServiceUnavailable, "search is not available on this instance")
		return
	}
	g, a, ok := versionSearchParams(w, r)
	if !ok {
		return
	}
	versions, err := svc.SearchVersions(r.Context(), principalFrom(r.Context()), g, a,
		search.ResultCap+1, searchReposFilter(r.URL.Query().Get("repos")))
	if err != nil {
		s.writeSearchError(w, err)
		return
	}
	if len(versions) == 0 {
		writeError(w, http.StatusNotFound, msgNoArtifactVersions)
		return
	}
	// The v filter runs after the empty-set verdict (§16.2): a miss here is
	// 200 with results [], never the 404.
	rows := make([]versionRow, 0, len(versions))
	for _, v := range versions {
		if pattern := r.URL.Query().Get("v"); pattern != "" && !search.MatchesPattern(pattern, v.Value) {
			continue
		}
		rows = append(rows, versionRow{Version: v.Value, Integration: v.Integration})
	}
	writeJSONBody(w, http.StatusOK, versionsResults{Results: rows})
}

// versionRow is the two-key thin row (§16.2: no uri).
type versionRow struct {
	Version     string `json:"version"`
	Integration bool   `json:"integration"`
}

type versionsResults struct {
	Results []versionRow `json:"results"`
}

// handleSearchLatestVersion serves GET /api/search/latestVersion (§16.3):
// text/plain with the bare version string. The three v arms: absent -> the
// latest release (first non-integration); wildcard -> the first pattern
// hit; non-wildcard -> the integration versions of that version line (the
// §16.3/V-ab mechanism reading — a release-only line answers the empty-set
// 404, the live-observed arm). exact is accepted and currently has no
// behavioral face of its own (no spec row pins one — registered in the
// ticket log).
func (s *Server) handleSearchLatestVersion(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.deps.ReposSvc.(versionsSearcher)
	if !ok {
		s.log.ErrorContext(r.Context(), "httpapi: version search endpoint reached but the service carries no version face")
		writeError(w, http.StatusServiceUnavailable, "search is not available on this instance")
		return
	}
	g, a, ok := versionSearchParams(w, r)
	if !ok {
		return
	}
	versions, err := svc.SearchVersions(r.Context(), principalFrom(r.Context()), g, a,
		search.ResultCap+1, searchReposFilter(r.URL.Query().Get("repos")))
	if err != nil {
		s.writeSearchError(w, err)
		return
	}
	if len(versions) == 0 {
		writeError(w, http.StatusNotFound, msgNoArtifactVersions)
		return
	}
	v := r.URL.Query().Get("v")
	switch {
	case v == "":
		for _, cand := range versions {
			if !cand.Integration {
				writePlainText(w, http.StatusOK, cand.Value)
				return
			}
		}
		writeError(w, http.StatusNotFound, msgLatestReleaseNotFound)
	case search.HasWildcard(v):
		for _, cand := range versions {
			if search.MatchesPattern(v, cand.Value) {
				writePlainText(w, http.StatusOK, cand.Value)
				return
			}
		}
		writeError(w, http.StatusNotFound, msgLatestIntegrationNotFound)
	default:
		// The version-line reading: the non-wildcard value names a line
		// whose INTEGRATION versions compete ("1.1" matches
		// "1.1-20260915.175736-1", the expansion of "1.1-SNAPSHOT", and
		// the unexpanded "1.1-SNAPSHOT" spelling itself).
		for _, cand := range versions {
			if cand.Integration && (cand.Value == v || strings.HasPrefix(cand.Value, v+"-")) {
				writePlainText(w, http.StatusOK, cand.Value)
				return
			}
		}
		writeError(w, http.StatusNotFound, msgNoArtifactVersions)
	}
}

// ---- GET /api/versions/{repoKey}/{path} (§16.4, D03-R12) ----

// msgVersionsByPropsNotFound is the endpoint's miss 404 copy, verbatim
// (aql.md §16.4, live: the bare "Not Found").
const msgVersionsByPropsNotFound = "Not Found"

// handleVersionsByProps serves GET /api/versions/{repoKey}/{path}
// (§16.4): the latest version among the items whose lowercase "version"
// property competes, narrowed by the repoKey/path segments (either may be
// the _any wildcard) and by every non-reserved query parameter as a
// property filter. listFiles=1 adds the artifacts rows — rendered with the
// reference's trailing-comma quirk (§16.4/V-aa: the row object ends
// `,\n  }`, non-strict JSON, live byte-for-byte twice).
//
// Path-matching reading: a non-_any path is a directory-boundary prefix of
// the item's path (the official "path to the artifact folder" wording);
// no live sample of a literal path arm exists beyond _any — the
// differential leg owns the recalibration.
func (s *Server) handleVersionsByProps(w http.ResponseWriter, r *http.Request, repoKey, pathFilter string) {
	p := principalFrom(r.Context())
	if p == nil {
		w.Header().Set("WWW-Authenticate", basicChallenge)
		writeError(w, http.StatusUnauthorized, msgAQLAuthRequired)
		return
	}
	svc, ok := s.legacySearchSvc(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var conds []repo.PropCond
	for key, values := range q {
		if key == "listFiles" {
			continue
		}
		value := ""
		if len(values) > 0 {
			value = values[0]
		}
		conds = append(conds, repo.PropCond{Key: key, Value: value})
	}
	// The version source itself: every candidate must carry a "version"
	// property (lowercase — §16.4, official wording).
	conds = append(conds, repo.PropCond{Key: "version"})
	var repos []string
	if repoKey != "_any" {
		repos = []string{repoKey}
	}
	nodes, err := svc.SearchProps(r.Context(), p, conds, search.ResultCap+1, repos)
	if err != nil {
		s.writeSearchError(w, err)
		return
	}
	type candidate struct{ repo, path, version string }
	var hits []candidate
	for _, n := range nodes {
		if pathFilter != "_any" && n.Path != pathFilter && !strings.HasPrefix(n.Path, pathFilter+"/") {
			continue
		}
		props, err := s.deps.Metadata.NodeProps().List(r.Context(), n.RepoKey, n.Path)
		if err != nil {
			s.log.ErrorContext(r.Context(), "httpapi: versions-by-properties property read failed",
				"repo", n.RepoKey, "path", n.Path, "error", err.Error())
			writeError(w, http.StatusInternalServerError, "AQL query execution failed")
			return
		}
		values := props["version"]
		if len(values) == 0 {
			continue
		}
		hits = append(hits, candidate{repo: n.RepoKey, path: n.Path, version: values[0]})
	}
	if len(hits) == 0 {
		writeError(w, http.StatusNotFound, msgVersionsByPropsNotFound)
		return
	}
	latest := hits[0]
	for _, c := range hits[1:] {
		if repo.CompareVersions(c.version, latest.version) > 0 {
			latest = c
		}
	}
	var b strings.Builder
	b.WriteString("{\n  \"version\" : ")
	b.WriteString(aqlJSONString(latest.version))
	b.WriteString(",\n  \"artifacts\" : [")
	var rows []string
	if q.Get("listFiles") == "1" {
		for _, c := range hits {
			if c.version != latest.version {
				continue
			}
			// The trailing comma after the last member is the reference's
			// serialization quirk (§16.4, live bytes) — deliberately not
			// strict JSON; the multi-row separator extrapolates the
			// single-artifact sample (V-aa).
			rows = append(rows, "{\n    \"repo\" : "+aqlJSONString(c.repo)+
				",\n    \"path\" : "+aqlJSONString(c.path)+",\n  }")
		}
	}
	if len(rows) == 0 {
		b.WriteString(" ") // Jackson's empty-array form: [ ]
	} else {
		b.WriteString(" " + strings.Join(rows, ",") + " ")
	}
	b.WriteString("]\n}")
	writeRawBody(w, http.StatusOK, "application/json", b.String())
}

// ---- GET /api/search/badChecksum (§16.5-R13) ----

// handleSearchBadChecksum serves GET /api/search/badChecksum: the admin-only
// corruption report. The two 400 copies are live-verbatim; the hit row is
// the official+decompiled anchor shape (uri + the expected/actual pair of
// the requested type — V-z, recalibrate on a corruption differential).
func (s *Server) handleSearchBadChecksum(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil {
		w.Header().Set("WWW-Authenticate", basicChallenge)
		writeError(w, http.StatusUnauthorized, msgAQLAuthRequired)
		return
	}
	if !p.Admin {
		writeError(w, http.StatusForbidden, "Forbidden")
		return
	}
	q := r.URL.Query()
	typ := q.Get("type")
	if typ == "" {
		writeError(w, http.StatusBadRequest, msgNoChecksumType)
		return
	}
	if typ != "md5" && typ != "sha1" && typ != "sha256" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Checksum type: %s is not defined", typ))
		return
	}
	auditor, ok := s.deps.ReposSvc.(checksumAuditor)
	if !ok {
		s.log.ErrorContext(r.Context(), "httpapi: badChecksum endpoint reached but the service carries no audit face")
		writeError(w, http.StatusServiceUnavailable, "search is not available on this instance")
		return
	}
	hits, err := auditor.AuditChecksums(r.Context(), p, typ, badChecksumRowCap, searchReposFilter(q.Get("repos")))
	if err != nil {
		s.writeSearchError(w, err)
		return
	}
	base := requestBase(r)
	rows := make([]badChecksumRow, 0, len(hits))
	for _, hit := range hits {
		rows = append(rows, badChecksumRow{
			URI: storageURI(base, hit.RepoKey, hit.Path),
			Checksums: map[string]checksumPair{
				typ: {Expected: hit.Registered, Actual: hit.Actual},
			},
		})
	}
	writeJSONBody(w, http.StatusOK, badChecksumResults{Results: rows})
}

type checksumPair struct {
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type badChecksumRow struct {
	URI       string                  `json:"uri"`
	Checksums map[string]checksumPair `json:"checksums"`
}

type badChecksumResults struct {
	Results []badChecksumRow `json:"results"`
}

// writeRawBody emits a pre-rendered body verbatim — the /api/versions face
// alone needs it (its trailing-comma quirk is not representable through the
// JSON encoder). HTML escaping is a non-issue: values pass through
// aqlJSONString, the family's raw-string escaper.
func writeRawBody(w http.ResponseWriter, status int, contentType, body string) {
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body)) //nolint:gosec // G705: nosniff set; the body is operator-rendered JSON
}
