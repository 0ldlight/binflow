package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/search"
)

// The legacy old-search doors (M15 T-417, FR-134 / aql.md section 8; the
// usage member M16 T-440, FR-148.1 / aql.md section 14.2):
//
//	GET /binflow/api/search/gavc?g=&a=&v=&c=&repos=   FR-134.1
//	GET /binflow/api/search/prop?props=k[=v][&repos=] FR-134.2
//	GET /binflow/api/search/pattern?pattern=<repo>:<path-glob>  FR-134.3
//	GET /binflow/api/search/usage?notUsedSince=<epoch-ms>       FR-148.1
//
// The first three doors share the T-92 pair's every convention (search.go):
// the {"results":[FileInfo...]} envelope through writeSearchResults, the
// route gate left open so the use case owns the anonymous-channel decision
// (a closed instance answers the spec's 403, not a masking 401 challenge),
// the error family through writeSearchError (query-shape problems are the
// E-01 400) — plus the two M15 additions: the K63 row ceiling (the endpoint
// sources search.ResultCap, passes cap+1 down and reads the truncation off
// the extra row, one ceiling and one truncation header shared with AQL) and
// the wildcard translation for the pattern arm, which happens HERE — at the
// endpoint — through match.go's single kernel (internal/search imports
// internal/repo, so the service cannot; one translator, no drift, the
// ADR-0043 pt 1/3 rule).
//
// Empty-set family (aql.md section 0-4, live-verified): gavc/prop/pattern
// answer 200 + "results":[] on a miss; usage belongs to the 404 family
// (v8n: {"errors":[{404,"No results found."}]}) and carries its own five-
// field row shape — the two sub-families do not share an envelope.

// legacySearcher is the service capability face (consumer-side interface,
// asserted off Deps.ReposSvc — the copyMoveRunner/archiveFamilyRunner
// precedent: the big Service interface stays untouched so hand-written test
// fakes that implement it method by method do not break; the concrete
// service carries the face, repo.LegacySearchService).
type legacySearcher interface {
	SearchGavc(ctx context.Context, p *repo.Principal, q repo.GavcQuery, limit int, repos []string) ([]*metadata.Node, error)
	SearchProps(ctx context.Context, p *repo.Principal, conds []repo.PropCond, limit int, repos []string) ([]*metadata.Node, error)
	SearchPattern(ctx context.Context, p *repo.Principal, repoLike, pathLike string, limit int) ([]*metadata.Node, error)
}

// legacySearchSvc resolves the face once per request. The miss arm is the
// honest 503 (a unit stack or an assembly gap — the ErrSearchUnavailable
// wording family), never a panic on a nil method set.
func (s *Server) legacySearchSvc(w http.ResponseWriter, r *http.Request) (legacySearcher, bool) {
	svc, ok := s.deps.ReposSvc.(legacySearcher)
	if !ok {
		s.log.ErrorContext(r.Context(), "httpapi: legacy search endpoint reached but the service carries no search face")
		writeError(w, http.StatusServiceUnavailable, "search is not available on this instance")
		return nil, false
	}
	return svc, true
}

// handleSearchGavc serves GET /api/search/gavc. Every coordinate is
// optional, at least one is required (the official rule, aql.md section
// 8.2); an illegal spelling answers the E-01 400 (the compatible subset of
// the M3 maven layout model — segment rules, no path separators inside a
// coordinate). repos narrows like the T-92 pair.
func (s *Server) handleSearchGavc(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.legacySearchSvc(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	query := repo.GavcQuery{
		Group:      q.Get("g"),
		Artifact:   q.Get("a"),
		Version:    q.Get("v"),
		Classifier: q.Get("c"),
	}
	repos := searchReposFilter(q.Get("repos"))
	p := principalFrom(r.Context())
	start := time.Now()
	nodes, err := svc.SearchGavc(r.Context(), p, query, search.ResultCap+1, repos)
	s.observeLegacySearch(time.Since(start))
	if err != nil {
		s.writeSearchError(w, err)
		return
	}
	s.writeLegacySearchResults(w, r, nodes)
}

// handleSearchProp serves GET /api/search/prop. Two parameter spellings
// share one constraint grammar (aql.md section 8.2, both live-verified):
// the documented props=k[=v] form, and the OFFICIAL form where any
// non-reserved query parameter names a property key (build.name=x). A key
// without a value — either spelling — is the official "*" arm: the key must
// exist, its value is unconstrained. repos is the one reserved name; at
// least one constraint is required.
func (s *Server) handleSearchProp(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.legacySearchSvc(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var conds []repo.PropCond
	for _, v := range q["props"] {
		key, value, _ := strings.Cut(v, "=")
		conds = append(conds, repo.PropCond{Key: key, Value: value})
	}
	for key, values := range q {
		if key == "props" || key == "repos" {
			continue
		}
		value := ""
		if len(values) > 0 {
			value = values[0]
		}
		conds = append(conds, repo.PropCond{Key: key, Value: value})
	}
	repos := searchReposFilter(q.Get("repos"))
	p := principalFrom(r.Context())
	start := time.Now()
	nodes, err := svc.SearchProps(r.Context(), p, conds, search.ResultCap+1, repos)
	s.observeLegacySearch(time.Since(start))
	if err != nil {
		s.writeSearchError(w, err)
		return
	}
	s.writeLegacySearchResults(w, r, nodes)
}

// handleSearchPattern serves GET /api/search/pattern: one
// <repo-glob>:<path-glob> value (aql.md section 8.2). Both halves are
// non-empty or the E-01 400 answers; the halves are then translated ONCE
// through match.go's kernel — the same translator AQL $match runs — so the
// two surfaces cannot drift ('*' and '?' cross segments here, SQL LIKE
// semantics; Artifactory's pattern search documents Ant-style globbing and
// the official notes even disclaim `**` — BinFlow's single-kernel reading
// is the registered divergence, aql.md section 8.2's alignment note).
func (s *Server) handleSearchPattern(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.legacySearchSvc(w, r)
	if !ok {
		return
	}
	pattern := strings.TrimSpace(r.URL.Query().Get("pattern"))
	if pattern == "" {
		writeError(w, http.StatusBadRequest, "Pattern search requires a non-empty 'pattern' query parameter.")
		return
	}
	repoGlob, pathGlob, found := strings.Cut(pattern, ":")
	repoGlob, pathGlob = strings.TrimSpace(repoGlob), strings.TrimSpace(pathGlob)
	if !found || repoGlob == "" || pathGlob == "" {
		writeError(w, http.StatusBadRequest, "Pattern search requires a '<repo-pattern>:<path-pattern>' value.")
		return
	}
	p := principalFrom(r.Context())
	start := time.Now()
	nodes, err := svc.SearchPattern(r.Context(), p,
		search.LikePattern(repoGlob), search.LikePattern(pathGlob), search.ResultCap+1)
	s.observeLegacySearch(time.Since(start))
	if err != nil {
		s.writeSearchError(w, err)
		return
	}
	s.writeLegacySearchResults(w, r, nodes)
}

// writeLegacySearchResults renders the shared envelope with the K63 ceiling:
// the service returned at most cap+1 rows, so a longer page means more raw
// rows existed — the page is trimmed to the cap and the same truncation
// header AQL sets (search.TruncatedHeader) marks it, one shape across both
// search planes.
func (s *Server) writeLegacySearchResults(w http.ResponseWriter, r *http.Request, nodes []*metadata.Node) {
	if len(nodes) > search.ResultCap {
		w.Header().Set(search.TruncatedHeader, "true")
		nodes = nodes[:search.ResultCap]
	}
	s.writeSearchResults(w, r, nodes)
}

// observeLegacySearch records one executed legacy query on the search
// family's counters — the same plane metric the AQL entrance feeds, with
// the plane label this ticket added (T-415's preseed note): plane="legacy".
// Metrics-less stacks count nothing and gate exactly the same.
func (s *Server) observeLegacySearch(d time.Duration) {
	if s.metrics == nil {
		return
	}
	s.metrics.searchQueries.Inc("plane", "legacy")
	s.metrics.searchDur.Observe(d.Seconds())
}

// ---- GET /api/search/usage (M16 T-440, FR-148.1 / aql.md §14.2) ----

// usageRunner is the consumer-side seam over the usage entrance of the T-413
// engine (RunUsage rides the very same gate, deadline and row cap a
// hand-written AQL query does — the K63 zero-exemption posture): asserted
// off the assembled engine so the scripted aqlRunner fakes of the transport
// legs keep compiling untouched.
type usageRunner interface {
	RunUsage(ctx context.Context, p *repo.Principal, q search.UsageQuery) (*search.Result, error)
}

// msgUsageNoResults is the 404 family's verbatim copy (aql.md §14.2, live
// v8n) — the endpoint's empty-set AND missing-parameter verdict: parameter
// missing answers the same 404, not a 400 (the decompiled quirk, §14.2).
const msgUsageNoResults = "No results found."

// usageEpochZero is the epoch-0 formatted instant a never-remote-downloaded
// row carries in remoteLastDownloaded (aql.md §14.2: epoch-0 formatted, not
// null — live v8m byte-for-byte). A null lastDownloaded renders the same
// way: LastDownloadRestResult formats null dates as epoch-0, the very rule
// that produces the remote arm's constant.
const usageEpochZero = "1970-01-01T00:00:00.000Z"

// usageResultRow is the five-field usage row (aql.md §14.2, live v8m):
// uri + the four download-statistics fields, nothing else. remoteDownload-
// Count/remoteLastDownloaded are the smart-remote dimension BinFlow has no
// source for (§14.1's mapping ruling: 恒 0/epoch-0, data never fabricated —
// the ?stats face's remoteDownloadCount is a different, BinFlow-native
// metric and the two coexist by design).
type usageResultRow struct {
	URI                  string `json:"uri"`
	DownloadCount        int64  `json:"downloadCount"`
	LastDownloaded       string `json:"lastDownloaded"`
	RemoteDownloadCount  int64  `json:"remoteDownloadCount"`
	RemoteLastDownloaded string `json:"remoteLastDownloaded"`
}

// usageResults is the endpoint's envelope.
type usageResults struct {
	Results []usageResultRow `json:"results"`
}

// handleSearchUsage serves GET /api/search/usage (aql.md §14.2):
// notUsedSince is required epoch milliseconds (last download strictly
// before it, never-downloaded included), createdBefore optional (defaults
// to notUsedSince per the official rule), repos an optional CSV narrowing.
// The hit set runs through the engine's fixed statistics-domain template —
// the spec's own one-source shape ("usage REST = statistics 域单源",
// §14.1) — so the download counts land on the very nodes columns the
// ?stats face reads (T-438's single counting channel, zero second source).
//
// Wire posture per §14.2: anonymous answers the 401 challenge (a
// privileged non-anonymous face, unlike the trio above); an empty hit set
// AND a missing notUsedSince both answer the verbatim 404 copy; the row
// ceiling is the K63 cap with the shared truncation header.
func (s *Server) handleSearchUsage(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil {
		w.Header().Set("WWW-Authenticate", basicChallenge)
		writeError(w, http.StatusUnauthorized, msgAQLAuthRequired)
		return
	}
	runner, ok := s.aql.(usageRunner)
	if s.aql == nil || !ok {
		s.log.ErrorContext(r.Context(), "httpapi: usage search endpoint reached but no search engine is assembled")
		writeError(w, http.StatusServiceUnavailable, "search is not available on this instance")
		return
	}
	q := r.URL.Query()
	raw := strings.TrimSpace(q.Get("notUsedSince"))
	if raw == "" {
		// The decompiled quirk (§14.2, medium confidence): a missing
		// parameter answers the empty-set 404 copy, not a 400.
		writeUsageNoResults(w)
		return
	}
	notUsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || notUsed < 0 {
		writeError(w, http.StatusBadRequest,
			"Usage search requires a non-negative 'notUsedSince' epoch-milliseconds value.")
		return
	}
	uq := search.UsageQuery{NotUsedSince: notUsed}
	if cb := strings.TrimSpace(q.Get("createdBefore")); cb != "" {
		before, err := strconv.ParseInt(cb, 10, 64)
		if err != nil || before < 0 {
			writeError(w, http.StatusBadRequest,
				"Usage search requires a non-negative 'createdBefore' epoch-milliseconds value.")
			return
		}
		uq.CreatedBefore = before
	}
	uq.Repos = searchReposFilter(q.Get("repos"))

	start := time.Now()
	res, err := runner.RunUsage(r.Context(), p, uq)
	s.observeLegacySearch(time.Since(start))
	if err != nil {
		s.writeAQLRunError(w, r, err) // the engine's families: 400/429/408/500
		return
	}
	if len(res.Rows) == 0 {
		writeUsageNoResults(w)
		return
	}
	if res.Truncated {
		w.Header().Set(search.TruncatedHeader, "true")
	}
	base := requestBase(r)
	rows := make([]usageResultRow, 0, len(res.Rows))
	for _, row := range res.Rows {
		last := usageEpochZero
		if row.LastDownloadedAt != "" {
			last = isoMillisUTC(row.LastDownloadedAt)
		}
		rows = append(rows, usageResultRow{
			URI:                  storageURI(base, row.RepoKey, row.Path),
			DownloadCount:        row.DownloadCount,
			LastDownloaded:       last,
			RemoteDownloadCount:  0,
			RemoteLastDownloaded: usageEpochZero,
		})
	}
	writeJSONBody(w, http.StatusOK, usageResults{Results: rows})
}

// writeUsageNoResults answers the 404 family's verbatim copy (live v8n).
func writeUsageNoResults(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, msgUsageNoResults)
}
