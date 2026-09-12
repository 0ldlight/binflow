package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/search"
)

// The UI search family (M16 T-452, FR-148.3 / aql.md §14.5 — inv-2 §1.C's
// four UI-tree resources, wire confidence medium, live arm V-m pending):
//
//	POST /binflow/api/artifactsearch/quick|gavc|checksum|trash
//	GET  /binflow/api/artifactsearch/pkg/{type}        the option set
//	POST /binflow/api/artifactsearch/pkg/{type}        criteria-array search
//	POST /binflow/api/artifactsearch/pkg/tonative      criteria -> AQL text
//	*    /binflow/api/stashResults[/**]                10 operations, below
//	POST /binflow/api/packagesSearch/leadFile|artifacts
//	POST /binflow/api/syntax-search
//
// Mounting ruling (the registered divergence): Artifactory serves the four
// resources under its UI REST tree (/artifactory/ui/api/<subpath>, exact
// prefix itself unconfirmed — V-m); BinFlow has no ui/api REST segment (the
// console's /binflow/ui is the SPA shell), so the family re-homes onto
// /binflow/api/<subpath> — the SAML key family's precedent (T-331). The
// registered subpaths are kept verbatim.
//
// Every member of the family is authenticated-only (@RolesAllowed admin,
// user → any non-anonymous principal; anonymous answers the 401 challenge).
//
// The BinFlow operation map (medium-confidence wire models pinned by test,
// per the house rule for medium-confidence rows):
//
//   - quick/gavc/checksum/trash reuse the legacy doors' service kernels and
//     answer the same {"results":[FileInfo]} envelope (the E-09 superset
//     row — the registered family posture; trash narrows to the trashcan
//     repository key).
//   - pkg/{type} GET answers the repositories of that package type (the
//     BinFlow reading of the "option set"); POST takes the anchor's
//     criteria-model ARRAY and answers the name matches inside the type's
//     repositories.
//   - pkg/tonative converts one criteria model into the equivalent AQL
//     text ({"query": "items.find(...)"}) — the anchor's "UI 检索条件 → AQL
//     转换".
//   - deleteArtifact is deliberately NOT routed (the anchor's own note: a
//     delete verb on the search page; BinFlow's search plane is read-only —
//     the gap is registered, the E-26 404 answers).
//   - stashResults: the anchor's factory posture — the master switch ships
//     OFF and BinFlow ships no enable carrier (the Smart Searches pro-tier
//     ruling, aql.md §14.5's note): every one of the ten operations answers
//     the verbatim 404 copy.
//   - packagesSearch resolves by repository path (BinFlow has no packages
//     index — the per-protocol lead-file semantics are the registered gap);
//     a miss answers the anchor's 404 with an EMPTY body.
//   - syntax-search posts AQL text and answers the UI envelope; a syntax
//     error is the 400 errors[] envelope (the anchor's ErrorResponse).

// msgStashDisabled is the stash family's verbatim off-copy (aql.md §14.5).
const msgStashDisabled = "Stash search results endpoint is disabled"

// uiSearchBodyMaxBytes caps the JSON request bodies of the family.
const uiSearchBodyMaxBytes = 1 << 20

// uiArtifactSearcher is the consumer-side seam over the name/checksum
// kernels (the legacySearcher precedent — asserted off Deps.ReposSvc so
// hand-written Service fakes keep compiling method by method).
type uiArtifactSearcher interface {
	SearchArtifacts(ctx context.Context, p *repo.Principal, name string, repos []string) ([]*metadata.Node, error)
	SearchChecksum(ctx context.Context, p *repo.Principal, q repo.ChecksumQuery, repos []string) ([]*metadata.Node, error)
}

// uiArtifactSvc resolves the face once per request (the honest 503 miss
// arm, the legacySearchSvc posture).
func (s *Server) uiArtifactSvc(w http.ResponseWriter, r *http.Request) (uiArtifactSearcher, bool) {
	svc, ok := s.deps.ReposSvc.(uiArtifactSearcher)
	if !ok {
		s.log.ErrorContext(r.Context(), "httpapi: UI search endpoint reached but the service carries no search face")
		writeError(w, http.StatusServiceUnavailable, "search is not available on this instance")
		return nil, false
	}
	return svc, true
}

// ---- artifactsearch ----

// uiQuickBody is the quick door's criteria model.
type uiQuickBody struct {
	SearchTerm string   `json:"searchTerm"`
	Repos      []string `json:"repos"`
}

// uiGavcBody is the gavc door's criteria model (the legacy g/a/v/c names).
type uiGavcBody struct {
	Group      string   `json:"g"`
	Artifact   string   `json:"a"`
	Version    string   `json:"v"`
	Classifier string   `json:"c"`
	Repos      []string `json:"repos"`
}

// uiChecksumBody is the checksum door's criteria model.
type uiChecksumBody struct {
	Sha256 string   `json:"sha256"`
	Sha1   string   `json:"sha1"`
	Md5    string   `json:"md5"`
	Repos  []string `json:"repos"`
}

// uiCriterion is ONE entry of the pkg doors' criteria-model array.
type uiCriterion struct {
	Name  string   `json:"name"`
	Repos []string `json:"repos"`
}

// decodeUIBody decodes one JSON body of the family into v.
func decodeUIBody(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, uiSearchBodyMaxBytes+1))
	if err != nil || len(body) > uiSearchBodyMaxBytes {
		writeError(w, http.StatusBadRequest, "Request body is unreadable or oversized.")
		return false
	}
	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, "Request body is required.")
		return false
	}
	if err := json.Unmarshal(body, v); err != nil {
		writeError(w, http.StatusBadRequest, "Request body is not valid JSON: "+err.Error())
		return false
	}
	return true
}

// handleUIArtifactSearchQuick serves POST artifactsearch/quick: the name
// fragment through the legacy artifact kernel (K64 semantics).
func (s *Server) handleUIArtifactSearchQuick(w http.ResponseWriter, r *http.Request) {
	var req uiQuickBody
	if !decodeUIBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.SearchTerm) == "" {
		writeError(w, http.StatusBadRequest, "Quick search requires a non-empty 'searchTerm'.")
		return
	}
	svc, ok := s.uiArtifactSvc(w, r)
	if !ok {
		return
	}
	p := principalFrom(r.Context())
	start := time.Now()
	nodes, err := svc.SearchArtifacts(r.Context(), p, req.SearchTerm, req.Repos)
	s.observeLegacySearch(time.Since(start))
	if err != nil {
		s.writeSearchError(w, err)
		return
	}
	s.writeLegacySearchResults(w, r, nodes)
}

// handleUIArtifactSearchGavc serves POST artifactsearch/gavc.
func (s *Server) handleUIArtifactSearchGavc(w http.ResponseWriter, r *http.Request) {
	var req uiGavcBody
	if !decodeUIBody(w, r, &req) {
		return
	}
	if req.Group == "" && req.Artifact == "" && req.Version == "" && req.Classifier == "" {
		writeError(w, http.StatusBadRequest, "GAVC search requires at least one coordinate.")
		return
	}
	svc, ok := s.legacySearchSvc(w, r)
	if !ok {
		return
	}
	p := principalFrom(r.Context())
	start := time.Now()
	nodes, err := svc.SearchGavc(r.Context(), p, repo.GavcQuery{
		Group: req.Group, Artifact: req.Artifact, Version: req.Version, Classifier: req.Classifier,
	}, search.ResultCap+1, req.Repos)
	s.observeLegacySearch(time.Since(start))
	if err != nil {
		s.writeSearchError(w, err)
		return
	}
	s.writeLegacySearchResults(w, r, nodes)
}

// handleUIArtifactSearchChecksum serves POST artifactsearch/checksum.
func (s *Server) handleUIArtifactSearchChecksum(w http.ResponseWriter, r *http.Request) {
	var req uiChecksumBody
	if !decodeUIBody(w, r, &req) {
		return
	}
	if req.Sha256 == "" && req.Sha1 == "" && req.Md5 == "" {
		writeError(w, http.StatusBadRequest, "Checksum search requires at least one digest.")
		return
	}
	svc, ok := s.uiArtifactSvc(w, r)
	if !ok {
		return
	}
	p := principalFrom(r.Context())
	start := time.Now()
	nodes, err := svc.SearchChecksum(r.Context(), p, repo.ChecksumQuery{
		Sha256: req.Sha256, Sha1: req.Sha1, Md5: req.Md5,
	}, req.Repos)
	s.observeLegacySearch(time.Since(start))
	if err != nil {
		s.writeSearchError(w, err)
		return
	}
	s.writeLegacySearchResults(w, r, nodes)
}

// handleUIArtifactSearchTrash serves POST artifactsearch/trash: the quick
// kernel narrowed to the trashcan repository (the console tree's Trash Can
// node is the same key).
func (s *Server) handleUIArtifactSearchTrash(w http.ResponseWriter, r *http.Request) {
	var req uiQuickBody
	if !decodeUIBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.SearchTerm) == "" {
		writeError(w, http.StatusBadRequest, "Trash search requires a non-empty 'searchTerm'.")
		return
	}
	svc, ok := s.uiArtifactSvc(w, r)
	if !ok {
		return
	}
	p := principalFrom(r.Context())
	start := time.Now()
	nodes, err := svc.SearchArtifacts(r.Context(), p, req.SearchTerm, []string{repo.TrashRepoKey})
	s.observeLegacySearch(time.Since(start))
	if err != nil {
		s.writeSearchError(w, err)
		return
	}
	s.writeLegacySearchResults(w, r, nodes)
}

// uiReposOfPackageType lists the local repository keys of one package type
// through the management listing (admin/user plane; the keys only).
func (s *Server) uiReposOfPackageType(w http.ResponseWriter, r *http.Request, pkgType string) ([]string, bool) {
	rows, err := s.deps.ReposSvc.ListReposFiltered(r.Context(), principalFrom(r.Context()), "", pkgType)
	if err != nil {
		s.log.ErrorContext(r.Context(), "httpapi: UI search package-type listing failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "repository listing failed")
		return nil, false
	}
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Type == repo.TypeLocal {
			keys = append(keys, row.RepoKey)
		}
	}
	sort.Strings(keys)
	return keys, true
}

// handleUIArtifactSearchPkgGet serves GET artifactsearch/pkg/{type}: the
// option set — the repositories of that package type the UI may search
// within (the BinFlow reading of the anchor's "选项集", registered).
func (s *Server) handleUIArtifactSearchPkgGet(w http.ResponseWriter, r *http.Request, pkgType string) {
	keys, ok := s.uiReposOfPackageType(w, r, pkgType)
	if !ok {
		return
	}
	writeJSONBody(w, http.StatusOK, struct {
		Repos []string `json:"repos"`
	}{Repos: keys})
}

// handleUIArtifactSearchPkgPost serves POST artifactsearch/pkg/{type}: the
// criteria-model array, every entry searched by name fragment inside the
// type's repositories, the union answered in the family envelope.
func (s *Server) handleUIArtifactSearchPkgPost(w http.ResponseWriter, r *http.Request, pkgType string) {
	var criteria []uiCriterion
	if !decodeUIBody(w, r, &criteria) {
		return
	}
	svc, ok := s.uiArtifactSvc(w, r)
	if !ok {
		return
	}
	keys, ok := s.uiReposOfPackageType(w, r, pkgType)
	if !ok {
		return
	}
	if len(keys) == 0 {
		s.writeLegacySearchResults(w, r, nil)
		return
	}
	p := principalFrom(r.Context())
	seen := map[string]*metadata.Node{}
	for _, c := range criteria {
		if strings.TrimSpace(c.Name) == "" {
			continue
		}
		nodes, err := svc.SearchArtifacts(r.Context(), p, c.Name, keys)
		if err != nil {
			s.writeSearchError(w, err)
			return
		}
		for _, n := range nodes {
			seen[n.RepoKey+"/"+n.Path] = n
		}
	}
	merged := make([]*metadata.Node, 0, len(seen))
	for _, n := range seen {
		merged = append(merged, n)
	}
	s.writeLegacySearchResults(w, r, merged)
}

// handleUIArtifactSearchToNative serves POST artifactsearch/pkg/tonative:
// the UI criteria model rendered as the equivalent AQL text (a STRING is
// answered, never executed — the conversion face only).
func (s *Server) handleUIArtifactSearchToNative(w http.ResponseWriter, r *http.Request) {
	var criteria []uiCriterion
	if !decodeUIBody(w, r, &criteria) {
		return
	}
	patterns := make([]string, 0, len(criteria))
	for _, c := range criteria {
		if strings.TrimSpace(c.Name) == "" {
			continue
		}
		// The $match literal is JSON-escaped — a criteria value can never
		// break out of the quoted AQL string.
		patterns = append(patterns, `"name":{"$match":`+aqlJSONString("*"+c.Name+"*")+"}")
	}
	query := "items.find({" + strings.Join(patterns, ",") + "})"
	writeJSONBody(w, http.StatusOK, struct {
		Query string `json:"query"`
	}{Query: query})
}

// handleUIStashDisabled answers EVERY stashResults operation: the family
// ships in the anchor's factory-off posture and BinFlow carries no enable
// switch (the Smart Searches pro-tier ruling) — the verbatim 404 copy is
// the wire contract, distinguished from the E-26 not-implemented 404.
func handleUIStashDisabled(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, msgStashDisabled)
}

// ---- packagesSearch ----

// uiPackageBody is the packages doors' addressing model: one repository
// path (BinFlow has no packages index — the per-protocol lead-file
// semantics are the registered gap; the path-addressing subset is the
// honest core).
type uiPackageBody struct {
	RepoKey string `json:"repoKey"`
	Path    string `json:"path"`
}

// uiPackageRow is the packages doors' row.
type uiPackageRow struct {
	URI     string `json:"uri"`
	RepoKey string `json:"repoKey"`
	Path    string `json:"path"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Created string `json:"created"`
}

// parseUIPackage decodes and validates the addressing model.
func parseUIPackage(w http.ResponseWriter, r *http.Request) (uiPackageBody, bool) {
	var req uiPackageBody
	if !decodeUIBody(w, r, &req) {
		return req, false
	}
	req.Path = strings.Trim(req.Path, "/")
	if req.RepoKey == "" || req.Path == "" {
		writeError(w, http.StatusBadRequest, "Package search requires 'repoKey' and 'path'.")
		return req, false
	}
	if strings.Contains(req.Path, "..") {
		writeError(w, http.StatusBadRequest, "Package search requires a path without dot segments.")
		return req, false
	}
	return req, true
}

// writeUIPackage404Empty is the anchor's miss arm: 404 with an EMPTY body
// (packagesSearch's registered verbatim posture — no envelope).
func writeUIPackage404Empty(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
}

// uiPackageRowOf projects one node onto the row.
func uiPackageRowOf(r *http.Request, n *metadata.Node) uiPackageRow {
	return uiPackageRow{
		URI:     storageURI(requestBase(r), n.RepoKey, n.Path),
		RepoKey: n.RepoKey,
		Path:    "/" + n.Path,
		Name:    n.Path[strings.LastIndex(n.Path, "/")+1:],
		Size:    n.Size,
		Created: isoMillisUTC(n.CreatedAt),
	}
}

// handleUIPackagesLeadFile serves POST packagesSearch/leadFile: the
// addressed path's FILE node — a folder is not a lead file and answers the
// empty 404 (no per-protocol index to elect one). Resolves through the
// metadata faces' non-counting storageNode (L011-1): a packages lookup is
// not a download and must not feed the counters.
func (s *Server) handleUIPackagesLeadFile(w http.ResponseWriter, r *http.Request) {
	req, ok := parseUIPackage(w, r)
	if !ok {
		return
	}
	node, err := s.storageNode(r, principalFrom(r.Context()), req.RepoKey, req.Path)
	if err != nil || node == nil || isFolderPath(node.Path) {
		writeUIPackage404Empty(w)
		return
	}
	writeJSONBody(w, http.StatusOK, uiPackageRowOf(r, node))
}

// handleUIPackagesArtifacts serves POST packagesSearch/artifacts: the file
// nodes at or under the addressed path.
func (s *Server) handleUIPackagesArtifacts(w http.ResponseWriter, r *http.Request) {
	req, ok := parseUIPackage(w, r)
	if !ok {
		return
	}
	nodes, err := s.deps.ReposSvc.List(r.Context(), principalFrom(r.Context()), req.RepoKey, req.Path)
	if err != nil || len(nodes) == 0 {
		writeUIPackage404Empty(w)
		return
	}
	rows := make([]uiPackageRow, 0, len(nodes))
	for _, n := range nodes {
		if !isFolderPath(n.Path) {
			rows = append(rows, uiPackageRowOf(r, n))
		}
	}
	if len(rows) == 0 {
		writeUIPackage404Empty(w)
		return
	}
	writeJSONBody(w, http.StatusOK, struct {
		Results []uiPackageRow `json:"results"`
	}{Results: rows})
}

// ---- syntax-search ----

// handleUISyntaxSearch serves POST syntax-search: the AQL text body (the
// /api/search/aql spelling — text/plain body, ?query= fallback) answered
// as the UI envelope: {"results":[<compact row objects>]}. The rows project
// through the query's own include list (the engine's projection echo), so
// the face answers exactly what the search page would render.
func (s *Server) handleUISyntaxSearch(w http.ResponseWriter, r *http.Request) {
	if s.aql == nil {
		s.log.ErrorContext(r.Context(), "httpapi: syntax-search reached but no search engine is assembled")
		writeError(w, http.StatusServiceUnavailable, "search is not available on this instance")
		return
	}
	query, ok := readAQLQuery(w, r)
	if !ok {
		return
	}
	if query == "" {
		writeError(w, http.StatusBadRequest, msgAQLBadRequestBody)
		return
	}
	ctx, expanded := withAQLExpandedVirtuals(r.Context())
	start := time.Now()
	res, err := s.aql.Run(ctx, principalFrom(r.Context()), query)
	s.observeAQLQuery(time.Since(start), query)
	if err != nil {
		// The anchor's error arm: a syntax error is the 400 errors[]
		// envelope (the ErrorResponse) — writeAQLRunError renders exactly
		// the QueryError family, the 429/408 gates ride with it.
		s.writeAQLRunError(w, r, err)
		return
	}
	// The virtual-repository reverse map, resolved once when the projection
	// (or the query's own virtual keys) need it — the aql face's rule.
	var rev map[string][]string
	virtualQueried := len(*expanded) > 0
	for _, f := range res.Plan.Output {
		if f.Kind == search.OutputVirtualRepos {
			virtualQueried = true
		}
	}
	if virtualQueried && s.aqlVirtual != nil {
		rev, err = s.aqlVirtual.reverse(r.Context())
		if err != nil {
			s.log.ErrorContext(r.Context(), "httpapi: syntax-search virtual index failed", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "AQL query execution failed")
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if res.Truncated {
		w.Header().Set(search.TruncatedHeader, "true")
	}
	w.WriteHeader(http.StatusOK)
	rows := make([]map[string]any, 0, len(res.Rows))
	for _, row := range res.Rows {
		rows = append(rows, uiSyntaxRow(row, res.Plan.Output, rev))
	}
	_ = json.NewEncoder(w).Encode(struct {
		Results []map[string]any `json:"results"`
	}{Results: rows}) //nolint:errcheck // response body; a vanished client is not an error
}

// uiSyntaxRow projects one row through the query's output list into the
// compact object form (native types; dates in the family's ISO form).
func uiSyntaxRow(row *metadata.NodeQueryRow, fields []search.OutputField, rev map[string][]string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f.Kind {
		case search.OutputProp:
			props := map[string][]string{}
			for k, vs := range row.Props {
				props[k] = vs
			}
			out["properties"] = props
		case search.OutputStat:
			out["stats"] = []map[string]any{uiStatMembers(row, fields)}
		case search.OutputVirtualRepos:
			out["virtual_repos"] = aqlVirtualsOf(rev, row.RepoKey)
		default:
			if k, v, ok := uiItemMember(f.Field, row); ok {
				out[k] = v
			}
		}
	}
	return out
}

// uiStatMembers collects the projected statistics members of one row.
func uiStatMembers(row *metadata.NodeQueryRow, fields []search.OutputField) map[string]any {
	out := map[string]any{}
	for _, f := range fields {
		if f.Kind != search.OutputStat {
			continue
		}
		switch f.Field {
		case search.FieldStatDownloaded:
			if row.LastDownloadedAt != "" {
				out["downloaded"] = isoMillisUTC(row.LastDownloadedAt)
			} else {
				out["downloaded"] = nil
			}
		case search.FieldStatDownloads:
			out["downloads"] = row.DownloadCount
		case search.FieldStatDownloadedBy:
			if row.LastDownloadedBy == "" {
				out["downloaded_by"] = nil
			} else {
				out["downloaded_by"] = row.LastDownloadedBy
			}
		case search.FieldStatRemoteDownloads:
			out["remote_downloads"] = 0
		case search.FieldStatRemoteDownloaded, search.FieldStatRemoteDownloadedBy,
			search.FieldStatRemoteOrigin, search.FieldStatRemotePath:
			out[strings.TrimPrefix(string(f.Field), "stat.")] = nil
		}
	}
	return out
}

// uiItemMember projects one storage-backed item field.
func uiItemMember(f search.FieldID, row *metadata.NodeQueryRow) (string, any, bool) {
	switch f {
	case search.FieldRepo:
		return "repo", row.RepoKey, true
	case search.FieldPath:
		return "path", row.ParentPath, true
	case search.FieldName:
		return "name", row.Name, true
	case search.FieldType:
		return "type", row.Type, true
	case search.FieldSize:
		return "size", row.Size, true
	case search.FieldCreated:
		return "created", isoMillisUTC(row.CreatedAt), true
	case search.FieldModified:
		return "modified", isoMillisUTC(row.UpdatedAt), true
	case search.FieldUpdated:
		return "updated", isoMillisUTC(row.UpdatedAt), true
	case search.FieldCreatedBy:
		return "created_by", row.CreatedBy, true
	case search.FieldModifiedBy:
		return "modified_by", row.CreatedBy, true
	case search.FieldDepth:
		return "depth", row.Depth, true
	case search.FieldActualMD5:
		return "actual_md5", row.Md5, true
	case search.FieldActualSHA1:
		return "actual_sha1", row.Sha1, true
	case search.FieldSha256:
		return "sha256", row.Sha256, true
	case search.FieldOriginalMD5:
		return "original_md5", row.Md5, true
	case search.FieldOriginalSHA1:
		return "original_sha1", row.Sha1, true
	}
	return "", nil, false
}
