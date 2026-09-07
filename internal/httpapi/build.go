// The build-info REST family (M17 T-508, FR-152.2 / ADR-0045 decision 6 +
// Errata ①②④, wire frozen by docs/reverse/build-info.md §1/§2): the
// BODY-carried upload PUT /api/build (the pre-errata path-segment skeleton
// is VOIDED — PUT /api/build/{name}/{number} has no route and keeps the
// E-26 404), the names/numbers/detail query ladder whose URIs echo the
// relative form ("/<name>", "/<number>" — official examples), and the
// append face POST /api/build/append/{name}/{number} whose success is 204
// EMPTY and whose missing-parent answer is the spec's verbatim
// "Build-Info not found".
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
// is out of the M17 face).

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/build"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// buildMaxBodyBytes caps the upload/append document (the family's wire-size
// floor: build info is KB-scale CI metadata; the reference's own manifest
// ceiling exists precisely because an unbounded JSON body is a DoS, and a
// document this large is malformed in spirit).
const buildMaxBodyBytes = 32 << 20

// buildDetailURI is the family's self-addressing prefix (relative URIs, the
// official echo form).
const buildDetailURI = "/api/build/"

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

// handleBuildUpload serves PUT /api/build: the full-document save. 200 with
// an EMPTY body on success (the frozen success shape).
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
	w.WriteHeader(http.StatusOK) // empty body, the official success form
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

// handleBuildList serves GET /api/build: every build NAME the caller may
// read, one row per (name, build_repo), uri "/<name>" relative — the
// server-side visible-set filter already ran (zero leakage, NFR-S80's
// third arm). Builds is never null: a fresh or fully-filtered view is [].
func (s *Server) handleBuildList(w http.ResponseWriter, r *http.Request) {
	if s.buildsUnavailable(w) {
		return
	}
	if refuseBuildProjects(w, r) {
		return
	}
	rows, err := s.builds.ListBuildNames(r.Context(), principalFrom(r.Context()))
	if err != nil {
		s.writeBuildError(w, err)
		return
	}
	body := struct {
		URI    string           `json:"uri"`
		Builds []buildNameEntry `json:"builds"`
	}{URI: "/api/build", Builds: make([]buildNameEntry, 0, len(rows))}
	for _, row := range rows {
		body.Builds = append(body.Builds, buildNameEntry{
			URI: "/" + row.Name, LastStarted: row.LastStarted})
	}
	s.countBuildsGet("names")
	writeJSONBody(w, http.StatusOK, body)
}

// handleBuildNumbers serves GET /api/build/{buildName}: every run of one
// name the caller may read, newest first, uri "/<number>". An empty visible
// set answers the family's 404 — a name the caller cannot read anywhere is
// indistinguishable from a name that does not exist (the zero-leak law; a
// 200-with-empty-list would confirm the name).
func (s *Server) handleBuildNumbers(w http.ResponseWriter, r *http.Request, name string) {
	if s.buildsUnavailable(w) {
		return
	}
	if refuseBuildProjects(w, r) {
		return
	}
	rows, err := s.builds.ListBuildNumbers(r.Context(), principalFrom(r.Context()), name)
	if err != nil {
		s.writeBuildError(w, err)
		return
	}
	if len(rows) == 0 {
		writeError(w, http.StatusNotFound, "Build-Info not found")
		return
	}
	body := struct {
		URI           string             `json:"uri"`
		BuildsNumbers []buildNumberEntry `json:"buildsNumbers"`
	}{URI: buildDetailURI + url.PathEscape(name), BuildsNumbers: make([]buildNumberEntry, 0, len(rows))}
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
// payload document with the normalized truth overlaid — name, number,
// started, type, modules, properties and, once promotions exist, statuses[]
// (the actual echo field the reference schema omits, §1's medium-confidence
// note).
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
	c := build.Coordinate{
		Name: name, Number: number,
		Started: q.Get("started"), Repo: q.Get("buildRepo"),
	}
	detail, err := s.builds.GetBuildDetail(r.Context(), principalFrom(r.Context()), c)
	if err != nil {
		s.writeBuildError(w, err)
		return
	}
	s.countBuildsGet("detail")
	writeJSONBody(w, http.StatusOK, map[string]any{
		"uri":       buildDetailURI + url.PathEscape(name) + "/" + url.PathEscape(number),
		"buildInfo": s.renderBuildInfo(detail),
	})
}

// handleBuildAppend serves POST /api/build/append/{buildName}/
// {buildNumber}: the module-array merge. 204 EMPTY on success; the parent's
// absence is the spec's verbatim 404 "Build-Info not found"; a denied
// caller meets 403 before the parent is even looked up (no oracle).
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
// vcs, issues, ... — survives from it), then the normalized truth overlaid
// (modules and properties from the store, so appends are visible; the
// canonical started; statuses[] when promotions exist). An unparsable
// archive degrades to the minimal envelope — the read never fails over the
// echo's decoration.
func (s *Server) renderBuildInfo(d *build.Detail) map[string]any {
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
	doc["started"] = d.Build.Started
	if d.Build.Type != "" {
		doc["type"] = d.Build.Type
	} else {
		delete(doc, "type")
	}
	doc["modules"] = renderBuildModules(d.Modules)
	props := make(map[string]any, len(d.Properties))
	for _, p := range d.Properties {
		props[p.Name] = p.Value
	}
	doc["properties"] = props
	if len(d.Promotions) > 0 {
		statuses := make([]map[string]any, 0, len(d.Promotions))
		for _, p := range d.Promotions {
			statuses = append(statuses, map[string]any{
				"status":     p.Status,
				"timestamp":  p.PromotedAt,
				"comment":    p.Comment,
				"repository": p.TargetRepo,
				"ciUser":     p.CiUser,
				"user":       p.PromotedBy,
			})
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
