package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/lzwzzy/binflow/internal/build"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/search"
)

// The build-family search faces (M17 T-511, FR-152.3 / aql.md §15.4):
//
//	POST /binflow/api/search/buildArtifacts   body = BuildArtifactsRequest
//	GET  /binflow/api/search/dependency?sha1=&sha256=&buildRepo=&project=
//
// plus the assembly half of the three AQL entries: the BuildSearcher
// adapter that feeds builds/modules/dependencies.find (search.BuildSearcher
// is the consumer-side seam; THIS file is the assembly point T-507's
// boundary ruling names — build never imports search, the facets ride
// Deps). Wire copy is verbatim from aql.md §15.4 / build-info.md §4
// (decompiled, medium confidence — the BinFlow posture keeps the product's
// spelling including the "your" typo, parity over pedantry).

// msgBuildArtifacts* are the three 400s of the buildArtifacts search,
// verbatim (aql.md §15.4 — the third keeps the product's "your" typo).
const (
	msgBuildArtifactsNoName    = "Cannot search without build name."
	msgBuildArtifactsNoNumber  = "Cannot search without build number or build status."
	msgBuildArtifactsBothGiven = "Cannot search with both build number and build status parameters, please omit build number if your are looking for latest build by status or omit build status to search for specific build version."
)

// buildArtifactsNoResults renders the 404 copy's two tails: the number arm
// echoes the REQUESTED number (the LATEST sentinel included), the status
// arm the requested status (aql.md §15.4).
func buildArtifactsNoResults(name, number, status string) string {
	switch {
	case number != "":
		return fmt.Sprintf("Could not find any build artifacts for build '%s' number '%s'", name, number)
	default:
		return fmt.Sprintf("Could not find any build artifacts for build '%s' status '%s'", name, status)
	}
}

// buildArtifactsBodyCap bounds the request body (a search request is KB
// scale; the cap keeps an unbounded read off the endpoint).
const buildArtifactsBodyCap = 1 << 20

// buildMappingRule is one mappings[] entry (aql.md §15.4): input is a
// regular expression matched against the artifact path, output the
// replacement template ($1-style group tokens).
type buildMappingRule struct {
	Input  string `json:"input"`
	Output string `json:"output"`
}

// buildArtifactsRequest is the endpoint's body (the official
// BuildArtifactsRequest schema minus the archive fields, aql.md §15.4 /
// build-info.md §2.7).
type buildArtifactsRequest struct {
	BuildName   string             `json:"buildName"`
	BuildNumber string             `json:"buildNumber"`
	BuildStatus string             `json:"buildStatus"`
	Repos       []string           `json:"repos"`
	Mappings    []buildMappingRule `json:"mappings"`
}

// ---- the BuildSearcher assembly (the AQL three entries' data plane) ----

// buildSearchAdapter feeds the AQL engine's build family over the record
// plane: enumeration rides the BuildStore's published faces (the dumb
// ledger — complete and unfiltered, the division the engine's weave owns),
// the row-level ACL is the build.Service CanRead projection (the SAME
// allow() mirror the build REST family runs). Run enumeration is
// coords-then-header (ListBuildNames/ListBuildNumbers/GetBuild): the M17
// store exposes no whole-plane query face, and the single-node corpus the
// entries address (NFR-P80's scale) keeps the walk inside the P95 budget —
// a SQL pushdown face is the natural M18+ follow-up when a corpus outgrows
// it.
type buildSearchAdapter struct {
	store metadata.BuildStore
	svc   *build.Service
}

// newBuildSearchAdapter assembles the adapter over the two faces the
// server already holds.
func newBuildSearchAdapter(store metadata.BuildStore, svc *build.Service) *buildSearchAdapter {
	return &buildSearchAdapter{store: store, svc: svc}
}

// buildRunCoord addresses one run (the four-tuple minus the derivables).
type buildRunCoord struct {
	name    string
	number  string
	started string
	repo    string
}

// buildRunCoords enumerates every run's coordinates: distinct names first
// (ListBuildNames lists one row per (name, repo) — a name with runs in two
// build repos appears twice, the walk dedupes), then every run per name
// (ListBuildNumbers spans the build repos, newest started first).
func (a *buildSearchAdapter) buildRunCoords(ctx context.Context) ([]buildRunCoord, error) {
	names, err := a.store.ListBuildNames(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("build search: listing build names: %w", err)
	}
	var coords []buildRunCoord
	lastName := ""
	for _, n := range names {
		if n.Name == lastName {
			continue
		}
		lastName = n.Name
		numbers, err := a.store.ListBuildNumbers(ctx, n.Name, "")
		if err != nil {
			return nil, fmt.Errorf("build search: listing runs of %s: %w", n.Name, err)
		}
		for _, bn := range numbers {
			coords = append(coords, buildRunCoord{
				name: n.Name, number: bn.Number, started: bn.Started, repo: bn.Repo,
			})
		}
	}
	return coords, nil
}

// Runs implements search.BuildSearcher.
func (a *buildSearchAdapter) Runs(ctx context.Context) ([]*search.BuildRun, error) {
	coords, err := a.buildRunCoords(ctx)
	if err != nil {
		return nil, err
	}
	runs := make([]*search.BuildRun, 0, len(coords))
	for _, c := range coords {
		b, err := a.store.GetBuild(ctx, c.name, c.number, c.started, c.repo)
		if err != nil {
			return nil, fmt.Errorf("build search: run %s#%s: %w", c.name, c.number, err)
		}
		runs = append(runs, &search.BuildRun{
			Name: b.Name, Number: b.Number, Started: b.Started, Repo: b.Repo,
			URL:       buildPayloadURL(b.Payload),
			CreatedAt: b.CreatedAt, CreatedBy: b.CreatedBy,
			UpdatedAt: b.UpdatedAt, UpdatedBy: b.UpdatedBy,
		})
	}
	return runs, nil
}

// Modules implements search.BuildSearcher.
func (a *buildSearchAdapter) Modules(ctx context.Context) ([]*search.BuildModuleRow, error) {
	coords, err := a.buildRunCoords(ctx)
	if err != nil {
		return nil, err
	}
	var out []*search.BuildModuleRow
	for _, c := range coords {
		mods, err := a.store.ListModules(ctx, c.name, c.number, c.started, c.repo)
		if err != nil {
			return nil, fmt.Errorf("build search: modules of %s#%s: %w", c.name, c.number, err)
		}
		for _, m := range mods {
			row := &search.BuildModuleRow{
				Repo: c.repo, Name: c.name, Number: c.number, Started: c.started,
				ModuleID: m.ID,
			}
			for _, d := range m.Dependencies {
				row.Dependencies = append(row.Dependencies, &search.BuildDependencyRow{
					ID: d.ID, Type: d.Type, Scopes: d.Scopes,
					Sha1: d.Sha1, Sha256: d.Sha256, Md5: d.Md5, Seq: d.Seq,
				})
			}
			out = append(out, row)
		}
	}
	return out, nil
}

// CanReadBuild implements search.BuildSearcher: the same r(buildRepo,
// buildName) decision the build REST read faces run.
func (a *buildSearchAdapter) CanReadBuild(ctx context.Context, p *repo.Principal, buildRepo, buildName string) bool {
	return a.svc.CanRead(ctx, p, buildRepo, buildName)
}

// buildPayloadURL extracts the CI server URL from the archived build
// document (” when the payload carries none — the field renders as an
// empty string, never a fabricated value).
func buildPayloadURL(payload string) string {
	if payload == "" {
		return ""
	}
	var doc struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		return ""
	}
	return doc.URL
}

// ---- POST /api/search/buildArtifacts ----

// handleSearchBuildArtifacts serves the endpoint (aql.md §15.4): resolve
// the run (number — LATEST resolves the newest — XOR status), collect its
// node-associated artifacts, narrow by repos[], rewrite by mappings[] and
// answer downloadUri rows; an empty hit set is the verbatim 404. The
// caller needs r(buildRepo, buildName) on the build AND read on every
// artifact's repository — a URI is an address, not a free leak.
func (s *Server) handleSearchBuildArtifacts(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil {
		w.Header().Set("WWW-Authenticate", basicChallenge)
		writeError(w, http.StatusUnauthorized, msgAQLAuthRequired)
		return
	}
	if s.builds == nil {
		s.log.ErrorContext(r.Context(), "httpapi: buildArtifacts search reached but the build domain is not assembled")
		writeError(w, http.StatusServiceUnavailable, "search is not available on this instance")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, buildArtifactsBodyCap+1))
	if err != nil || len(body) > buildArtifactsBodyCap {
		writeError(w, http.StatusBadRequest, msgAQLBadRequestBody)
		return
	}
	var req buildArtifactsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, msgAQLBadRequestBody)
		return
	}
	name := req.BuildName
	if name == "" {
		writeError(w, http.StatusBadRequest, msgBuildArtifactsNoName)
		return
	}
	number, status := req.BuildNumber, req.BuildStatus
	if number == "" && status == "" {
		writeError(w, http.StatusBadRequest, msgBuildArtifactsNoNumber)
		return
	}
	if number != "" && status != "" {
		writeError(w, http.StatusBadRequest, msgBuildArtifactsBothGiven)
		return
	}

	// The build-domain gate first — denial carries no existence
	// information (the family's no-oracle posture).
	if !s.builds.CanRead(r.Context(), p, metadata.DefaultBuildRepo, name) {
		err := fmt.Errorf("build artifacts search %s: %w", name, build.ErrForbidden)
		writeError(w, http.StatusForbidden, err.Error())
		return
	}

	mappings, ok := compileBuildMappings(w, req.Mappings)
	if !ok {
		return // 400 already written
	}
	repos := req.Repos
	repoSet := make(map[string]bool, len(repos))
	for _, key := range repos {
		repoSet[key] = true
	}

	ctx := r.Context()
	run, ok := s.resolveBuildArtifactsRun(ctx, w, name, number, status)
	if !ok {
		return // 404 already written
	}

	mods, err := s.deps.Metadata.Builds().ListModules(ctx, run.name, run.number, run.started, run.repo)
	if err != nil {
		s.log.ErrorContext(ctx, "httpapi: buildArtifacts search modules read failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "Build artifacts search failed")
		return
	}

	base := requestBase(r)
	truncated := false
	rows := make([]map[string]string, 0, 16)
	for _, m := range mods {
		for _, art := range m.Artifacts {
			if art.RepoKey == "" || art.Path == "" {
				continue // record-only line: no node association, no URI
			}
			if len(repoSet) > 0 && !repoSet[art.RepoKey] {
				continue
			}
			// The artifact address is content-plane data: the same read
			// decision a download runs gates it (admin short-circuits
			// inside the mirror).
			if s.deps.ReposSvc != nil && !s.deps.ReposSvc.CanRead(ctx, p, art.RepoKey, art.Path) {
				continue
			}
			path := art.Path
			for _, mrule := range mappings {
				if mrule.re.MatchString(path) {
					path = mrule.re.ReplaceAllString(path, mrule.output)
					break
				}
			}
			if len(rows) >= search.ResultCap {
				truncated = true
				break
			}
			rows = append(rows, map[string]string{"downloadUri": downloadURI(base, art.RepoKey, path)})
		}
	}
	if len(rows) == 0 {
		writeError(w, http.StatusNotFound, buildArtifactsNoResults(name, number, status))
		return
	}
	if truncated {
		w.Header().Set(search.TruncatedHeader, "true")
	}
	writeJSONBody(w, http.StatusOK, resultsEnvelope{Results: rows})
}

// compiledBuildMapping is one mappings[] rule with its input compiled.
type compiledBuildMapping struct {
	re     *regexp.Regexp
	output string
}

// compileBuildMappings compiles the mappings[] inputs. An illegal
// expression answers the E-01 400 naming it (the official face registers
// no copy for the arm; the family's clear-400 posture applies).
func compileBuildMappings(w http.ResponseWriter, rules []buildMappingRule) ([]compiledBuildMapping, bool) {
	out := make([]compiledBuildMapping, 0, len(rules))
	for _, rule := range rules {
		if rule.Input == "" {
			continue
		}
		re, err := regexp.Compile(rule.Input)
		if err != nil {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("Invalid mapping pattern %q: %v", rule.Input, err))
			return nil, false
		}
		out = append(out, compiledBuildMapping{re: re, output: rule.Output})
	}
	return out, true
}

// resolveBuildArtifactsRun resolves the target run: the number arm (the
// LATEST sentinel takes the newest run of the name; a concrete number its
// latest run) or the status arm (the newest run whose CURRENT promotion
// status matches — the history's newest row, the store's ordering). A miss
// answers the verbatim 404; the bool reports the response written.
func (s *Server) resolveBuildArtifactsRun(ctx context.Context, w http.ResponseWriter, name, number, status string) (buildRunCoord, bool) {
	store := s.deps.Metadata.Builds()
	notFound := func() (buildRunCoord, bool) {
		writeError(w, http.StatusNotFound, buildArtifactsNoResults(name, number, status))
		return buildRunCoord{}, false
	}
	if status == "" {
		if number == "LATEST" {
			numbers, err := store.ListBuildNumbers(ctx, name, metadata.DefaultBuildRepo)
			if err != nil {
				s.log.ErrorContext(ctx, "httpapi: buildArtifacts search numbers read failed", "error", err.Error())
				writeError(w, http.StatusInternalServerError, "Build artifacts search failed")
				return buildRunCoord{}, false
			}
			if len(numbers) == 0 {
				return notFound()
			}
			latest := numbers[0] // started DESC: the newest run
			return buildRunCoord{name: name, number: latest.Number, started: latest.Started, repo: latest.Repo}, true
		}
		b, err := store.GetBuild(ctx, name, number, "", metadata.DefaultBuildRepo)
		if err != nil {
			if errors.Is(err, metadata.ErrBuildNotFound) {
				return notFound()
			}
			s.log.ErrorContext(ctx, "httpapi: buildArtifacts search build read failed", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "Build artifacts search failed")
			return buildRunCoord{}, false
		}
		return buildRunCoord{name: b.Name, number: b.Number, started: b.Started, repo: b.Repo}, true
	}
	// The status arm: the newest run whose current status matches.
	numbers, err := store.ListBuildNumbers(ctx, name, metadata.DefaultBuildRepo)
	if err != nil {
		s.log.ErrorContext(ctx, "httpapi: buildArtifacts search numbers read failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "Build artifacts search failed")
		return buildRunCoord{}, false
	}
	for _, bn := range numbers {
		promos, err := store.ListPromotions(ctx, name, bn.Number, bn.Started, metadata.DefaultBuildRepo)
		if err != nil {
			s.log.ErrorContext(ctx, "httpapi: buildArtifacts search promotions read failed", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "Build artifacts search failed")
			return buildRunCoord{}, false
		}
		current := ""
		if len(promos) > 0 {
			current = promos[0].Status // promoted_at DESC: the newest row
		}
		if current == status {
			return buildRunCoord{name: name, number: bn.Number, started: bn.Started, repo: bn.Repo}, true
		}
	}
	return notFound()
}

// ---- GET /api/search/dependency ----

// handleSearchDependency serves the endpoint (aql.md §15.4): the checksum
// reverse lookup — which builds' dependency segments name the artifact.
// Rows are build URIs (the build API form), deduped per (name, number)
// with the newest run first; the ACL gate is r(buildRepo, buildName) per
// run, memoized per name. buildRepo narrows to one build repository
// (default the preferred-build-info key); project is accepted and ignored
// (BinFlow runs the single-project posture).
func (s *Server) handleSearchDependency(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil {
		w.Header().Set("WWW-Authenticate", basicChallenge)
		writeError(w, http.StatusUnauthorized, msgAQLAuthRequired)
		return
	}
	if s.builds == nil {
		s.log.ErrorContext(r.Context(), "httpapi: dependency search reached but the build domain is not assembled")
		writeError(w, http.StatusServiceUnavailable, "search is not available on this instance")
		return
	}
	q := r.URL.Query()
	sha1 := strings.ToLower(strings.TrimSpace(q.Get("sha1")))
	sha256 := strings.ToLower(strings.TrimSpace(q.Get("sha256")))
	if sha1 == "" && sha256 == "" {
		writeError(w, http.StatusBadRequest,
			"Dependency search requires at least one of sha1 or sha256.")
		return
	}
	if sha1 != "" && (len(sha1) != 40 || !isLowerHex(sha1)) {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("Dependency search sha1 %q must be 40 hex characters.", q.Get("sha1")))
		return
	}
	if sha256 != "" && (len(sha256) != 64 || !isLowerHex(sha256)) {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("Dependency search sha256 %q must be 64 hex characters.", q.Get("sha256")))
		return
	}
	buildRepo := strings.TrimSpace(q.Get("buildRepo"))
	if buildRepo == "" {
		buildRepo = metadata.DefaultBuildRepo // the preferred-build-info default
	}

	ctx := r.Context()
	store := s.deps.Metadata.Builds()
	names, err := store.ListBuildNames(ctx, buildRepo)
	if err != nil {
		s.log.ErrorContext(ctx, "httpapi: dependency search names read failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "Dependency search failed")
		return
	}

	base := requestBase(r)
	seen := make(map[string]bool, 8)
	rows := make([]map[string]string, 0, 8)
	for _, n := range names {
		if !s.builds.CanRead(ctx, p, n.Repo, n.Name) {
			continue // the run set of an unreadable name is not the caller's
		}
		numbers, err := store.ListBuildNumbers(ctx, n.Name, buildRepo)
		if err != nil {
			s.log.ErrorContext(ctx, "httpapi: dependency search numbers read failed", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "Dependency search failed")
			return
		}
		for _, bn := range numbers {
			if seen[n.Name+"/"+bn.Number] {
				continue // dedupe per build: the newest run already answered
			}
			mods, err := store.ListModules(ctx, n.Name, bn.Number, bn.Started, bn.Repo)
			if err != nil {
				s.log.ErrorContext(ctx, "httpapi: dependency search modules read failed", "error", err.Error())
				writeError(w, http.StatusInternalServerError, "Dependency search failed")
				return
			}
			for _, m := range mods {
				for _, d := range m.Dependencies {
					hit := (sha1 != "" && strings.EqualFold(d.Sha1, sha1)) ||
						(sha256 != "" && strings.EqualFold(d.Sha256, sha256))
					if !hit {
						continue
					}
					seen[n.Name+"/"+bn.Number] = true
					if len(rows) >= search.ResultCap {
						w.Header().Set(search.TruncatedHeader, "true")
						writeJSONBody(w, http.StatusOK, resultsEnvelope{Results: rows})
						return
					}
					rows = append(rows, map[string]string{
						"uri": buildURI(base, n.Name, bn.Number),
					})
					break
				}
				if seen[n.Name+"/"+bn.Number] {
					break
				}
			}
		}
	}
	// The empty set keeps the family's dominant arm (200 + results:[]) —
	// the 404-empty posture is the usage/buildArtifacts subfamily's, and
	// no live sample registers which side dependency belongs to (logged
	// for the reverse-engineer's ledger).
	writeJSONBody(w, http.StatusOK, resultsEnvelope{Results: rows})
}

// buildURI renders the build info form: <base>/binflow/api/build/<name>/
// <number> (the build REST family's own address, path-escaped per segment
// — build names and numbers are free-form).
func buildURI(base, name, number string) string {
	return base + "/binflow/api/build/" + url.PathEscape(name) + "/" + url.PathEscape(number)
}

// isLowerHex reports whether s is entirely lowercase hex digits.
func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return false
		}
	}
	return true
}

// resultsEnvelope is the {"results":[...]} family shape (the thin row map
// keeps each endpoint's key set literal at its call site).
type resultsEnvelope struct {
	Results []map[string]string `json:"results"`
}

// ---- the AQL build-row rendering (the three entries' transport half) ----

// writeAQLBuildResult renders the streaming envelope for a build-family
// result: the same prefix/range/suffix contract as the items form, rows
// through the plan's projection echo list over the build field registry.
func (s *Server) writeAQLBuildResult(w http.ResponseWriter, res *search.Result, compact bool) {
	fields := res.Plan.Output
	var b strings.Builder
	b.WriteString(aqlEnvelopePrefix)
	for i, row := range res.BuildRows {
		if i > 0 {
			b.WriteByte(',')
		}
		renderAQLBuildRow(&b, row, fields, compact)
	}
	b.WriteString(aqlEnvelopeRowsClose)
	renderAQLRange(&b, res, compact)
	b.WriteString(aqlEnvelopeSuffix)

	w.Header().Set("Content-Type", "application/json")
	if res.Truncated {
		w.Header().Set(search.TruncatedHeader, "true")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, b.String()) //nolint:errcheck // response body; a vanished client is not an error
}

// renderAQLBuildRow renders one build-family row: every member is a
// scalar — strings as stored, dates through the shared build-instant
// kernel in the ISO-milliseconds echo form (an unparseable value passes
// through verbatim rather than being faked, the aqlJSONDate rule).
func renderAQLBuildRow(b *strings.Builder, row *search.BuildRow, fields []search.OutputField, compact bool) {
	b.WriteByte('{')
	first := true
	emit := func(key, val string) {
		if !first {
			b.WriteByte(',')
		}
		first = false
		if compact {
			b.WriteString(aqlJSONString(key))
			b.WriteByte(':')
			b.WriteString(val)
			return
		}
		b.WriteString("\n  ")
		b.WriteString(aqlJSONString(key))
		b.WriteString(" : ")
		b.WriteString(val)
	}
	for _, f := range fields {
		emit(f.Key, buildRowJSON(f.Field, row))
	}
	if !compact {
		b.WriteString("\n")
	}
	b.WriteByte('}')
}

// buildRowJSON renders one projection member of a build row.
func buildRowJSON(id search.FieldID, row *search.BuildRow) string {
	v := search.BuildFieldValue(row, id)
	switch id {
	case search.FieldBuildStarted, search.FieldBuildCreated, search.FieldBuildModified:
		if t, ok := search.ParseBuildInstant(v); ok {
			return `"` + t.UTC().Format("2006-01-02T15:04:05.000Z") + `"`
		}
	}
	return aqlJSONString(v)
}
