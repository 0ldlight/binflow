// The build-info REST family (M17 T-508 → L023-2A realignment, FR-152.2 /
// ADR-0045 decision 6 + Errata ①②④, wire frozen by docs/reverse/
// build-info.md §1/§2/§11): the BODY-carried upload PUT /api/build (the
// pre-errata path-segment skeleton is VOIDED — PUT /api/build/{name}/
// {number} has no route and keeps the E-26 404) whose success is 204 with
// the X-Checksum-Sha256 header (§11.1-E1), the names/numbers/detail query
// ladder whose top-level URIs echo the ABSOLUTE form with the ?buildRepo=
// query string (§11.3) and whose empty answers are the spec's verbatim
// 404s (E2), the append face POST /api/build/append/{name}/{number} whose
// success is 204 EMPTY and whose missing-parent answer is E4's verbatim
// "The build <name>:<number> is not found", and the run deletion face
// DELETE /api/build/{name} with §11.7's E6 wording.
//
// Route gates demand authentication only (the official RolesAllowed
// admin,user posture): every face's real decision is the build-domain
// allow() mirror on (buildRepo, buildName), which is body- or
// path-dependent, so the handlers own it — the permissions family-4
// precedent. The BinFlow-native parameter rulings this file owns: ?project=
// and ?projectKey= are refused (no projects domain — silently landing a
// project-scoped document on the default build repo would be the dishonest
// alternative), ?buildRepo= addresses a custom logical key on every face
// (the officially-sanctioned parameter name, extended to the faces the
// reference serves through ?project=), and ?diff= is refused (Builds Diff
// is out of the M17 face — its response shape stays an open item,
// build-info.md §9 #5).

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/build"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/webhook"
)

// buildMaxBodyBytes caps the upload/append document (the family's wire-size
// floor: build info is KB-scale CI metadata; the reference's own manifest
// ceiling exists precisely because an unbounded JSON body is a DoS, and a
// document this large is malformed in spirit).
const buildMaxBodyBytes = 32 << 20

// buildWebhookEmitter adapts the build domain's Emit facet (build.emit,
// T-510 / ADR-0045 decision 7) onto the unified-event bus: the four
// payload facts ride their webhook.Event carriers (Name/Repo the base,
// Number/Started the build extras) and the principal projects onto the
// envelope's userContext the same way the repository domain's
// hookActorOf does. The plane is the ONE bus instance the assembly
// holds — same-source by construction.
func buildWebhookEmitter(plane WebhookPlane) build.WebhookEmitter {
	return func(ctx context.Context, e build.WebhookEvent) {
		plane.Emit(ctx, webhook.Event{
			Domain: webhook.DomainBuild, Type: e.Type,
			Name: e.Name, Repo: e.Repo,
			BuildNumber:  e.Number,
			BuildStarted: e.Started,
			Actor:        buildHookActorOf(e.Principal),
		})
	}
}

// buildHookActorOf projects a build-domain principal onto the outbound
// envelope's userContext triple (repo/api.go's hookActorOf shape — the
// anonymous fallback included; duplicated because that helper is
// deliberately unexported on the repository domain's file).
func buildHookActorOf(p *build.Principal) webhook.Actor {
	if p == nil {
		return webhook.Actor{ID: "anonymous", Realm: webhook.RealmFor("")}
	}
	return webhook.Actor{
		ID:      p.Name,
		IsToken: p.TokenID > 0,
		Realm:   webhook.RealmFor(string(p.Source)),
	}
}

// refuseBuildProjects answers the honest 400 for the project family the
// platform does not carry (report bool: true = answered, stop).
func refuseBuildProjects(w http.ResponseWriter, r *http.Request) bool {
	q := r.URL.Query()
	for _, name := range []string{"project", "projectKey"} {
		if strings.TrimSpace(q.Get(name)) != "" {
			writeError(w, http.StatusBadRequest,
				"projects are not supported in BinFlow; address a custom build repository with ?buildRepo=")
			return true
		}
	}
	return false
}

// readBuildBody reads the capped request document.
func readBuildBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, buildMaxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest,
			"build info body could not be read (max "+strconv.Itoa(buildMaxBodyBytes>>20)+"MiB): "+err.Error())
		return nil, false
	}
	return raw, true
}

// writeBuildError maps the service faces onto the wire: the 400 family
// carries the validator's own wording (pinned verbatim by tests — the
// reference exposes no frozen 400 samples, §9 #6), 403 the wrapped
// forbidden text (operations.go's passthrough posture), 404 the spec's
// verbatim "Build-Info not found" (the one frozen wording — uniform across
// the family so the numbers face's zero-leak 404 and the detail face's
// absent-run 404 read identically). A *repo.StatusError renders VERBATIM
// first (the promote faces' carrier refusals — the four-adapter seam
// posture) and the promote 503 arm answers the carrier-less unit stack.
func (s *Server) writeBuildError(w http.ResponseWriter, err error) {
	var se *repo.StatusError
	if errors.As(err, &se) {
		writeError(w, se.Code, se.Message)
		return
	}
	switch {
	case errors.Is(err, build.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, build.ErrInvalidBuildInfo), errors.Is(err, build.ErrInvalidCoordinate):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, metadata.ErrBuildNotFound):
		writeError(w, http.StatusNotFound, "Build-Info not found")
	case errors.Is(err, build.ErrPromoteUnavailable):
		writeError(w, http.StatusServiceUnavailable, "build promotion is not available on this instance")
	default:
		s.log.Error("httpapi: build operation failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "build operation failed")
	}
}

// handleBuildUpload serves PUT /api/build: the full-document save. 204
// No Content with the X-Checksum-Sha256 response header on success (§11.1
// -E1 — the checksum of the stored build JSON manifest, not the request
// bytes: the server re-serializes with its own rewrites first).
func (s *Server) handleBuildUpload(w http.ResponseWriter, r *http.Request) {
	if s.buildsUnavailable(w) {
		return
	}
	if refuseBuildProjects(w, r) {
		return
	}
	raw, ok := readBuildBody(w, r)
	if !ok {
		return
	}
	start := time.Now()
	res, err := s.builds.Upload(r.Context(), principalFrom(r.Context()), raw, r.URL.Query().Get("buildRepo"))
	if err != nil {
		s.writeBuildError(w, err)
		return
	}
	outcome := "created"
	if !res.Created {
		outcome = "replaced"
	}
	s.observeBuildsPut("upload", outcome, time.Since(start))
	w.Header().Set("X-Checksum-Sha256", res.Checksum)
	w.WriteHeader(http.StatusNoContent) // empty body, the frozen success form (E1)
}

// buildNameEntry and buildNumberEntry are the list faces' row shapes
// (relative URIs, the official echo form).
type buildNameEntry struct {
	URI         string `json:"uri"`
	LastStarted string `json:"lastStarted"`
}

type buildNumberEntry struct {
	URI     string `json:"uri"`
	Started string `json:"started"`
}

// buildRepoQuery resolves the family's ?buildRepo= parameter: absent or
// empty lands on the default logical key (the reference's build_repo
// default, §0 soft-seam ②), a custom key passes through.
func buildRepoQuery(r *http.Request) string {
	if v := r.URL.Query().Get("buildRepo"); v != "" {
		return v
	}
	return metadata.DefaultBuildRepo
}

// buildFamilyURI renders the family's top-level self-address: the ABSOLUTE
// URL plus the ?buildRepo= query string (§11.3's live shape —
// http://<host>/binflow/api/build[...]?buildRepo=<repo>).
func buildFamilyURI(r *http.Request, repo, tail string) string {
	return contextURL(r) + "/api/build" + tail + "?buildRepo=" + url.QueryEscape(repo)
}

// handleBuildList serves GET /api/build: every build NAME the caller may
// read under the addressed build_repo, one row per name, uri "/<name>"
// relative — the server-side visible-set filter already ran (zero
// leakage, NFR-S80's third arm). A fresh or fully-filtered view is the
// family's empty state: the spec's verbatim 404 "No builds were found"
// (§11.1-E2 — the reference answers 404, not 200-with-[]).
func (s *Server) handleBuildList(w http.ResponseWriter, r *http.Request) {
	if s.buildsUnavailable(w) {
		return
	}
	if refuseBuildProjects(w, r) {
		return
	}
	repo := buildRepoQuery(r)
	rows, err := s.builds.ListBuildNamesRepo(r.Context(), principalFrom(r.Context()), repo)
	if err != nil {
		s.writeBuildError(w, err)
		return
	}
	if len(rows) == 0 {
		writeError(w, http.StatusNotFound, "No builds were found")
		return
	}
	body := struct {
		URI    string           `json:"uri"`
		Builds []buildNameEntry `json:"builds"`
	}{URI: buildFamilyURI(r, repo, ""), Builds: make([]buildNameEntry, 0, len(rows))}
	for _, row := range rows {
		body.Builds = append(body.Builds, buildNameEntry{
			URI: "/" + row.Name, LastStarted: row.LastStarted})
	}
	s.countBuildsGet("names")
	writeJSONBody(w, http.StatusOK, body)
}

// handleBuildNumbers serves GET /api/build/{buildName}: every run of one
// name the caller may read, started DESC (strict newest-first — §11.8),
// uri "/<number>" relative. An empty visible set answers the spec's
// verbatim 404 "No build was found for build name: <name>" — a name the
// caller cannot read is indistinguishable from a name that does not
// exist (the zero-leak law; a 200-with-empty-list would confirm the
// name). A same-number multi-run stack answers that number once per run
// (§11.3's live shape).
func (s *Server) handleBuildNumbers(w http.ResponseWriter, r *http.Request, name string) {
	if s.buildsUnavailable(w) {
		return
	}
	if refuseBuildProjects(w, r) {
		return
	}
	repo := buildRepoQuery(r)
	rows, err := s.builds.ListBuildNumbers(r.Context(), principalFrom(r.Context()), name, repo)
	if err != nil {
		s.writeBuildError(w, err)
		return
	}
	if len(rows) == 0 {
		writeError(w, http.StatusNotFound, "No build was found for build name: "+name)
		return
	}
	body := struct {
		URI           string             `json:"uri"`
		BuildsNumbers []buildNumberEntry `json:"buildsNumbers"`
	}{URI: buildFamilyURI(r, repo, "/"+url.PathEscape(name)), BuildsNumbers: make([]buildNumberEntry, 0, len(rows))}
	for _, row := range rows {
		body.BuildsNumbers = append(body.BuildsNumbers, buildNumberEntry{
			URI: "/" + row.Number, Started: row.Started})
	}
	s.countBuildsGet("numbers")
	writeJSONBody(w, http.StatusOK, body)
}

// handleBuildGet serves GET /api/build/{buildName}/{buildNumber}: one run's
// detail. ?started= disambiguates same-name-same-number runs (normalized
// through the same gate the upload ran, so the caller may re-use its
// original literal; absent = the latest run). The body echoes the archived
// payload document with the interpreted truth overlaid — name, number,
// type, modules, properties and, once promotions exist, statuses[] — while
// the payload's own `started` literal stands VERBATIM (§11.3: the detail
// face echoes the original timezone, unlike the list faces' UTC
// normalization). ?slim=true strips the heavy segments (modules [],
// properties null — the jf CLI consumption shape). A miss answers the
// spec's verbatim 404 with the addressed coordinates.
func (s *Server) handleBuildGet(w http.ResponseWriter, r *http.Request, name, number string) {
	if s.buildsUnavailable(w) {
		return
	}
	if refuseBuildProjects(w, r) {
		return
	}
	q := r.URL.Query()
	if strings.TrimSpace(q.Get("diff")) != "" {
		writeError(w, http.StatusBadRequest,
			"the diff parameter is not supported in BinFlow (Builds Diff is outside the M17 scope)")
		return
	}
	startedLit := q.Get("started")
	c := build.Coordinate{
		Name: name, Number: number,
		Started: startedLit, Repo: q.Get("buildRepo"),
	}
	detail, err := s.builds.GetBuildDetail(r.Context(), principalFrom(r.Context()), c)
	if err != nil {
		if errors.Is(err, metadata.ErrBuildNotFound) {
			msg := "No build was found for build name: " + name + ", build number: " + number
			if startedLit != "" {
				msg += ", build started: " + startedLit
			}
			writeError(w, http.StatusNotFound, msg)
			return
		}
		s.writeBuildError(w, err)
		return
	}
	s.countBuildsGet("detail")
	writeJSONBody(w, http.StatusOK, map[string]any{
		"uri":       buildFamilyURI(r, detail.Build.Repo, "/"+url.PathEscape(name)+"/"+url.PathEscape(number)),
		"buildInfo": s.renderBuildInfo(detail, slimBuildQuery(q)),
	})
}

// slimBuildQuery parses the ?slim= flag (true/1 — anything else or absent
// is the full document; the reference pins only slim=true).
func slimBuildQuery(q url.Values) bool {
	v := q.Get("slim")
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	return err == nil && b
}

// handleBuildAppend serves POST /api/build/append/{buildName}/
// {buildNumber}: the module-array merge. 204 EMPTY on success; the parent's
// absence is the spec's verbatim 404 "The build <name>:<number> is not
// found" (§11.1-E4 — the openapi's "Build-Info not found" wording is
// voided); a denied caller meets 403 before the parent is even looked up
// (no oracle).
func (s *Server) handleBuildAppend(w http.ResponseWriter, r *http.Request, name, number string) {
	if s.buildsUnavailable(w) {
		return
	}
	if refuseBuildProjects(w, r) {
		return
	}
	raw, ok := readBuildBody(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	start := time.Now()
	_, err := s.builds.Append(r.Context(), principalFrom(r.Context()), build.Coordinate{
		Name: name, Number: number,
		Started: q.Get("started"), Repo: q.Get("buildRepo"),
	}, raw)
	if err != nil {
		if errors.Is(err, metadata.ErrBuildNotFound) {
			writeError(w, http.StatusNotFound,
				"The build "+name+":"+number+" is not found") // E4 verbatim
			return
		}
		s.writeBuildError(w, err)
		return
	}
	s.observeBuildsPut("append", "merged", time.Since(start))
	w.WriteHeader(http.StatusNoContent) // empty, the frozen success shape
}

// buildsUnavailable renders the honest 503 of a metadata-less unit stack
// (the aql/search posture).
func (s *Server) buildsUnavailable(w http.ResponseWriter) bool {
	if s.builds == nil {
		writeError(w, http.StatusServiceUnavailable, "build info is not available on this instance")
		return true
	}
	return false
}

// handleBuildPromote serves POST /api/build/promote/{buildName}/
// {buildNumber} (M17 T-509, FR-152.2 — build-info.md §1/§2.4): 200 with the
// messages[] stream ({level: error|warning|info, message} — partial failures
// under failFast=false ride the same 200), the promotion history row behind
// it, and the promote metric family's outcome arm. ?started= disambiguates
// same-name-same-number runs like every run-addressed face.
func (s *Server) handleBuildPromote(w http.ResponseWriter, r *http.Request, name, number string) {
	if s.buildsUnavailable(w) {
		return
	}
	if refuseBuildProjects(w, r) {
		return
	}
	raw, ok := readBuildBody(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	start := time.Now()
	res, err := s.builds.Promote(r.Context(), principalFrom(r.Context()), build.Coordinate{
		Name: name, Number: number,
		Started: q.Get("started"), Repo: q.Get("buildRepo"),
	}, raw)
	if err != nil {
		s.writeBuildError(w, err)
		return
	}
	outcome := "promoted"
	switch {
	case res.DryRun:
		outcome = "dry-run"
	case res.StatusOnly:
		outcome = "status-only"
	}
	s.observeBuildsPromote(outcome, time.Since(start))
	if res.Messages == nil {
		res.Messages = []build.PromotionMessage{}
	}
	writeJSONBody(w, http.StatusOK, struct {
		Messages []build.PromotionMessage `json:"messages"`
	}{Messages: res.Messages})
}

// handleBuildRetention serves POST /api/build/retention/{buildName}
// (build-info.md §1/§2.5): the four-field window body, ?async= (default
// TRUE — the official posture; the execution moves off the request path, the
// 200 answers from the VALIDATED plan). async=false runs the window inline:
// the ladder (403/404/400) answers synchronously, a mid-execution store
// fault answers the honest 500 — success stays the official bare 200.
func (s *Server) handleBuildRetention(w http.ResponseWriter, r *http.Request, name string) {
	if s.buildsUnavailable(w) {
		return
	}
	if refuseBuildProjects(w, r) {
		return
	}
	raw, ok := readBuildBody(w, r)
	if !ok {
		return
	}
	var req build.RetentionRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, http.StatusBadRequest,
			"retention body is not valid JSON: "+err.Error())
		return
	}
	q := r.URL.Query()
	p := principalFrom(r.Context())
	plan, err := s.builds.PrepareRetention(r.Context(), p, name, q.Get("buildRepo"), req)
	if err != nil {
		s.writeBuildError(w, err)
		return
	}
	async := true
	if v := q.Get("async"); v != "" {
		if b, perr := strconv.ParseBool(v); perr == nil {
			async = b
		} else {
			writeError(w, http.StatusBadRequest,
				"the async parameter must be a boolean (true/false), got "+strconv.Quote(v))
			return
		}
	}
	if async {
		// The official default: the window runs detached (validation already
		// answered; a background failure is logged, never a silent loss).
		detached := context.WithoutCancel(r.Context())
		go func() {
			defer func() {
				if v := recover(); v != nil {
					s.log.Error("httpapi: build retention panicked",
						"build", name, "panic", v)
				}
			}()
			if _, err := plan.Execute(detached, s.builds, p); err != nil {
				s.log.Error("httpapi: build retention execution failed",
					"build", name, "error", err.Error())
			}
		}()
		w.WriteHeader(http.StatusOK)
		return
	}
	if _, err := plan.Execute(r.Context(), s.builds, p); err != nil {
		s.writeBuildError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK) // empty body, the undocumented official success form
}

// handleBuildDelete serves DELETE /api/build/{buildName}: the run
// deletion face (§11.7 — L023-2A's D07-R04 arm). ?buildNumbers= carries
// the CSV of numbers, ?artifacts=1 additionally deletes the associated
// artifact nodes, ?deleteAll=1 drops every run of the name. The 200
// wording is E6's verbatim text/plain: "have" (not the official page's
// stale "has"), the Warning segment for numbers no run answered, the
// trailing newline; deleteAll has its own no-period sentence. The 404s
// are two-branch: a missing name and an all-missing numbers list are
// different answers.
func (s *Server) handleBuildDelete(w http.ResponseWriter, r *http.Request, name string) {
	if s.buildsUnavailable(w) {
		return
	}
	if refuseBuildProjects(w, r) {
		return
	}
	q := r.URL.Query()
	deleteAll, err := buildFlagQuery(q, "deleteAll")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	artifacts, err := buildFlagQuery(q, "artifacts")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var numbers []string
	for _, n := range strings.Split(q.Get("buildNumbers"), ",") {
		if n != "" {
			numbers = append(numbers, n)
		}
	}
	if !deleteAll && len(numbers) == 0 {
		writeError(w, http.StatusBadRequest, "Please provide at least one build number to delete")
		return
	}
	res, err := s.builds.DeleteRuns(r.Context(), principalFrom(r.Context()),
		name, q.Get("buildRepo"), numbers, deleteAll, artifacts)
	if err != nil {
		switch {
		case errors.Is(err, build.ErrBuildNumbersNotFound):
			writeError(w, http.StatusNotFound, "Unable to find the given build numbers")
		case errors.Is(err, metadata.ErrBuildNotFound):
			writeError(w, http.StatusNotFound, "Unable to find build '"+name+"'")
		default:
			s.writeBuildError(w, err)
		}
		return
	}
	if deleteAll {
		writeText(w, http.StatusOK,
			"All builds '"+name+"' under '"+res.Repo+"' have been deleted successfully") // E6: no trailing period
		return
	}
	var sb strings.Builder
	sb.WriteString("The following builds have been deleted successfully: ")
	sb.WriteString(quoteJoin(res.Deleted))
	sb.WriteString(".\n")
	if len(res.Missing) > 0 {
		sb.WriteString("Warning - the following builds could not be removed: ")
		sb.WriteString(quoteJoin(res.Missing))
		sb.WriteString(".\n")
	}
	writeText(w, http.StatusOK, sb.String())
}

// buildFlagQuery parses one 0|1 query flag (true/false also parse — the
// boolean spellings collapse; anything else is the honest 400).
func buildFlagQuery(q url.Values, name string) (bool, error) {
	v := q.Get(name)
	if v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("the %s parameter must be a boolean (0/1), got %s", name, strconv.Quote(v))
	}
	return b, nil
}

// quoteJoin renders the E6 deletion wording's entry list: 'a', 'b'.
func quoteJoin(entries []string) string {
	quoted := make([]string, 0, len(entries))
	for _, e := range entries {
		quoted = append(quoted, "'"+e+"'")
	}
	return strings.Join(quoted, ", ")
}

// splitBuildCoords splits the /api/build family's tail after prefix into
// the DECODED (name, number) pair: exactly one segment addresses the
// numbers face, exactly two the detail/append face; anything else (empty
// first segment, trailing slash, deeper tail) is not the family's grammar —
// routed=false leaves the E-26 404 to the caller. err reports a segment
// whose percent-escaping cannot decode (the honest 400 — build numbers
// officially carry special characters, so the decode is not cosmetic).
func splitBuildCoords(rest, prefix string) (name, number string, routed bool, err error) {
	parts := strings.Split(strings.TrimPrefix(rest, prefix), "/")
	if parts[0] == "" || (len(parts) == 2 && parts[1] == "") || len(parts) > 2 {
		return "", "", false, nil
	}
	if name, err = url.PathUnescape(parts[0]); err != nil {
		return "", "", true, err
	}
	if len(parts) == 2 {
		if number, err = url.PathUnescape(parts[1]); err != nil {
			return "", "", true, err
		}
	}
	return name, number, true, nil
}

// renderBuildInfo assembles the GET face's buildInfo document: the archived
// payload parsed as the base (every uninterpreted wire field — buildAgent,
// vcs, issues, ... — survives from it, INCLUDING its original `started`
// literal: §11.3's detail face echoes the uploaded timezone verbatim, so
// the canonical store literal is only the fallback), then the interpreted
// truth overlaid (name, number, type, modules and properties from the
// store, so appends are visible; statuses[] when promotions exist).
// durationMillis defaults to 0 when the document carried none (§3.1), and
// slim strips the heavy segments. An unparsable archive degrades to the
// minimal envelope — the read never fails over the echo's decoration.
func (s *Server) renderBuildInfo(d *build.Detail, slim bool) map[string]any {
	doc := map[string]any{}
	if d.Build.Payload != "" {
		var base map[string]any
		if err := json.Unmarshal([]byte(d.Build.Payload), &base); err != nil {
			s.log.Debug("httpapi: build payload archive is not JSON, echoing the normalized envelope only",
				"build", d.Build.Name+"#"+d.Build.Number)
		} else {
			doc = base
		}
	}
	doc["name"] = d.Build.Name
	doc["number"] = d.Build.Number
	if _, ok := doc["started"]; !ok {
		doc["started"] = d.Build.Started
	}
	if d.Build.Type != "" {
		doc["type"] = d.Build.Type
	} else {
		delete(doc, "type")
	}
	if slim {
		// §1 detail row: modules [] and properties null — the jf CLI's
		// lightweight consumption shape.
		doc["modules"] = []buildModuleEcho{}
		doc["properties"] = nil
	} else {
		doc["modules"] = renderBuildModules(d.Modules)
		props := make(map[string]any, len(d.Properties))
		for _, p := range d.Properties {
			props[p.Name] = p.Value
		}
		doc["properties"] = props
	}
	if _, ok := doc["durationMillis"]; !ok {
		doc["durationMillis"] = 0 // §3.1's default echo
	}
	if len(d.Promotions) > 0 {
		statuses := make([]map[string]any, 0, len(d.Promotions))
		for _, p := range d.Promotions {
			entry := map[string]any{
				"status":    p.Status,
				"timestamp": p.PromotedAt,
				"comment":   p.Comment,
				"user":      p.PromotedBy,
			}
			// §3.1's wire shape: timestampDate is the epoch-milliseconds
			// twin of timestamp (the schema-undocumented field the live
			// probe pinned); repository/ciUser are omitted whole when the
			// promotion carried none — nullable fields never ride as "".
			if t, err := time.Parse(time.RFC3339, p.PromotedAt); err == nil {
				entry["timestampDate"] = t.UnixMilli()
			}
			if p.TargetRepo != "" {
				entry["repository"] = p.TargetRepo
			}
			if p.CiUser != "" {
				entry["ciUser"] = p.CiUser
			}
			statuses = append(statuses, entry)
		}
		doc["statuses"] = statuses
	} else {
		delete(doc, "statuses")
	}
	return doc
}

// The module echo rows (the wire shapes of build-info.md §3.1: modules,
// artifacts with the association-form path, dependencies with scopes[]).
type buildModuleEcho struct {
	ID           string                `json:"id"`
	Type         string                `json:"type,omitempty"`
	Artifacts    []buildArtifactEcho   `json:"artifacts"`
	Dependencies []buildDependencyEcho `json:"dependencies"`
}

type buildArtifactEcho struct {
	Type   string `json:"type,omitempty"`
	Sha1   string `json:"sha1,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
	Md5    string `json:"md5,omitempty"`
	Name   string `json:"name,omitempty"`
	Path   string `json:"path,omitempty"`
}

type buildDependencyEcho struct {
	Type   string   `json:"type,omitempty"`
	Sha1   string   `json:"sha1,omitempty"`
	Sha256 string   `json:"sha256,omitempty"`
	Md5    string   `json:"md5,omitempty"`
	ID     string   `json:"id,omitempty"`
	Scopes []string `json:"scopes,omitempty"`
}

// renderBuildModules projects the store's module segment onto the wire echo.
// Arrays are never null (an empty segment echoes []); the artifact path is
// the association form "<repo>/<path>" — a record-only row carries no path
// (the 024 design: the wire path IS the association).
func renderBuildModules(modules []*metadata.BuildModule) []buildModuleEcho {
	out := make([]buildModuleEcho, 0, len(modules))
	for _, m := range modules {
		echo := buildModuleEcho{
			ID:           m.ID,
			Type:         m.Type,
			Artifacts:    make([]buildArtifactEcho, 0, len(m.Artifacts)),
			Dependencies: make([]buildDependencyEcho, 0, len(m.Dependencies)),
		}
		for _, a := range m.Artifacts {
			path := ""
			if a.RepoKey != "" {
				path = a.RepoKey + "/" + a.Path
			}
			echo.Artifacts = append(echo.Artifacts, buildArtifactEcho{
				Type: a.Type, Sha1: a.Sha1, Sha256: a.Sha256, Md5: a.Md5,
				Name: a.Name, Path: path,
			})
		}
		for _, dep := range m.Dependencies {
			var scopes []string
			if dep.Scopes != "" {
				scopes = strings.Split(dep.Scopes, ",")
			}
			echo.Dependencies = append(echo.Dependencies, buildDependencyEcho{
				Type: dep.Type, Sha1: dep.Sha1, Sha256: dep.Sha256, Md5: dep.Md5,
				ID: dep.ID, Scopes: scopes,
			})
		}
		out = append(out, echo)
	}
	return out
}
