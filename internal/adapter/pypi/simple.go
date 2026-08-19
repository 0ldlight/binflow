package pypi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/repo"
)

// PEP media types (PEP 691): the JSON simple form is negotiated ONLY by an
// explicit Accept of the JSON media type — the default stays HTML, matching
// Artifactory's default-off switch (PRD FR-19: "BinFlow 亦默认 HTML").
const (
	simpleJSONMediaType = "application/vnd.pypi.simple.v1+json"
	simpleHTMLMediaType = "text/html; charset=utf-8"
	simpleJSONCT        = "application/vnd.pypi.simple.v1+json; charset=utf-8"
)

// indexHead is the fixed document head every simple page shares (PEP 629's
// api-version=2 declaration; maven-npm-pypi.md section 3.2 pins the exact
// Artifactory spelling for BOTH the repository-level and project-level
// pages).
const indexHead = "<!DOCTYPE html>\n<html><head><title>Simple Index</title><meta name=\"api-version\" value=\"2\" /></head><body>\n"

// indexFoot closes the document.
const indexFoot = "</body></html>\n"

// indexEntry is one downloadable file on a project page.
type indexEntry struct {
	filename string // final path segment, the anchor text
	path     string // full repo-relative node path (<name>/<version>/<filename>)
	sha256   string
	size     int64
}

// serveSimple routes the simple-index family:
//
//	/simple          -> 302 to /simple/          (trailing-slash redirect)
//	/simple/         -> repository-level project index (P2)
//	/simple/<name>   -> 302 to /simple/<name>/   (PEP 503 clients expect it)
//	/simple/<name>/  -> the project page (HTML, or JSON via Accept)
//	/simple/<name>/<anything> -> 404 (reserved endpoint, no semantics)
func (h *Handler) serveSimple(w http.ResponseWriter, r *http.Request, repoKey, rel, tail string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed,
			fmt.Sprintf("method %s is not supported on the simple index", r.Method))
		return
	}

	if rel == segSimple {
		// "/simple" without the trailing slash: same redirect as the
		// project-level form.
		redirectTrailingSlash(w, segSimple+"/")
		return
	}
	if tail == "" || tail == "/" {
		h.serveSimpleRoot(w, r, repoKey)
		return
	}

	trimmed := strings.TrimSuffix(tail, "/")
	if strings.Contains(trimmed, "/") {
		// /simple/<name>/<version> and anything deeper: a reserved endpoint
		// with no implementation semantics — always 404
		// (maven-npm-pypi.md section 3.1, high confidence).
		writeError(w, http.StatusNotFound,
			"the simple index has no per-version pages in BinFlow (reserved endpoint, always 404)")
		return
	}
	if trimmed == "" {
		writeError(w, http.StatusBadRequest, "the project name in the simple index path is empty")
		return
	}
	if !strings.HasSuffix(tail, "/") {
		// No trailing slash: 302 to the same name with one (the client's
		// spelling is preserved, PEP 503 clients follow redirects).
		redirectTrailingSlash(w, escapePathSegment(trimmed)+"/")
		return
	}

	h.serveProjectPage(w, r, repoKey, trimmed)
}

// redirectTrailingSlash answers the 302 with a RELATIVE Location. Relative
// is the only correct form here: the /binflow/api/pypi seam rewrites the
// URL before this handler runs, so an absolute-path Location would resolve
// against the server root and leave the protocol mount; a relative one
// resolves against whatever spelling the client actually used
// (maven-npm-pypi.md section 3.1: no-trailing-slash GET -> 302).
func redirectTrailingSlash(w http.ResponseWriter, location string) {
	w.Header().Set("Location", location)
	w.WriteHeader(http.StatusFound)
}

// serveSimpleRoot renders the repository-level index (P2 face): one anchor
// per distinct normalized project name, sorted. pip never fetches it; it is
// the browser/operator surface.
func (h *Handler) serveSimpleRoot(w http.ResponseWriter, r *http.Request, repoKey string) {
	names, err := h.projectNames(r.Context(), r, repoKey)
	if err != nil {
		h.writeServiceError(w, err, r.Method, repoKey, segSimple+"/")
		return
	}
	var b strings.Builder
	b.WriteString(indexHead)
	for _, n := range names {
		b.WriteString(`<a href="` + htmlEscape(n) + `/">` + htmlEscape(n) + "</a>\n")
	}
	b.WriteString(indexFoot)
	writeIndex(w, r, []byte(b.String()), simpleHTMLMediaType)
}

// serveProjectPage renders one project's page: every file of every stored
// spelling of the name (PEP 503 normalization is the lookup key, so
// Demo_Pkg, demo_pkg and demo-pkg share this page), entries sorted by
// filename, each anchored to the packages/ download URL with the sha256
// fragment pip self-verifies against. An unknown name is 404.
//
// Repository classes split the collection (T-72): a VIRTUAL repository
// merges every member's entries, a REMOTE repository serves its upstream
// page remapped onto this repository's packages/ mount (the M46/M47 face),
// only a LOCAL repository reads its own node facts.
func (h *Handler) serveProjectPage(w http.ResponseWriter, r *http.Request, repoKey, name string) {
	if h.repos != nil {
		switch row, err := h.repos.Get(r.Context(), repoKey); {
		case err == nil && row.Type == repo.TypeVirtual:
			h.serveVirtualProjectPage(w, r, repoKey, name)
			return
		case err == nil && row.Type == repo.TypeRemote:
			h.serveRemoteProjectPage(w, r, repoKey, name)
			return
		}
	}
	entries, err := h.projectEntries(r.Context(), r, repoKey, name)
	if err != nil {
		h.writeServiceError(w, err, r.Method, repoKey, segSimple+"/"+name+"/")
		return
	}
	if len(entries) == 0 {
		writeError(w, http.StatusNotFound,
			fmt.Sprintf("project '%s' not found in repository '%s'", normalizePackageName(name), repoKey))
		return
	}

	// One URL, two representations chosen by Accept: every response from
	// this page (both forms, 200 and 304 alike) must declare Vary: Accept
	// so no intermediary cache ever cross-serves the HTML and JSON forms
	// (T-70 review N2).
	w.Header().Set("Vary", "Accept")

	if wantsSimpleJSON(r) {
		writeSimpleJSON(w, r, name, entries)
		return
	}

	var b strings.Builder
	b.WriteString(indexHead)
	for _, e := range entries {
		href := "../../" + segPackages + "/" + escapePath(e.path) + "#sha256=" + e.sha256
		b.WriteString(`<a href="` + htmlEscape(href) + `">` + htmlEscape(e.filename) + "</a>\n")
	}
	b.WriteString(indexFoot)
	writeIndex(w, r, []byte(b.String()), simpleHTMLMediaType)
}

// writeSimpleJSON renders the PEP 691 JSON form of a project page. The url
// field carries the same ../../packages/...#sha256= spelling as the HTML
// href so both forms address identical targets.
func writeSimpleJSON(w http.ResponseWriter, r *http.Request, name string, entries []indexEntry) {
	type jsonFile struct {
		Filename string            `json:"filename"`
		URL      string            `json:"url"`
		Hashes   map[string]string `json:"hashes"`
		Size     int64             `json:"size"`
	}
	doc := struct {
		Meta  map[string]string `json:"meta"`
		Name  string            `json:"name"`
		Files []jsonFile        `json:"files"`
	}{
		Meta:  map[string]string{"api-version": "2.0"}, // PEP 691 spells it 2.0
		Name:  normalizePackageName(name),
		Files: make([]jsonFile, 0, len(entries)),
	}
	for _, e := range entries {
		doc.Files = append(doc.Files, jsonFile{
			Filename: e.filename,
			URL:      "../../" + segPackages + "/" + escapePath(e.path) + "#sha256=" + e.sha256,
			Hashes:   map[string]string{"sha256": e.sha256},
			Size:     e.size,
		})
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "rendering the JSON simple index: "+err.Error())
		return
	}
	body = append(body, '\n')
	writeIndex(w, r, body, simpleJSONCT)
}

// projectEntries collects the index entries of one project: every FILE node
// whose first path segment normalizes to the requested name. Folder marker
// rows and non-<name>/<version>/<filename> shapes are skipped; the walk is
// a whole-repository listing filtered in memory — the index has no hidden
// sidecar files by design (maven-npm-pypi.md section 4.5 recommends exactly
// this reconstruction from artifact facts).
func (h *Handler) projectEntries(ctx context.Context, r *http.Request, repoKey, name string) ([]indexEntry, error) {
	nodes, err := h.svc.List(ctx, adapter.PrincipalFrom(r.Context()), repoKey, "")
	if err != nil {
		return nil, err
	}
	want := normalizePackageName(name)
	entries := make([]indexEntry, 0, 4)
	for _, n := range nodes {
		if strings.HasSuffix(n.Path, "/") {
			continue // folder marker rows carry no file
		}
		segs := strings.Split(n.Path, "/")
		if len(segs) != 3 || segs[0] == "" || segs[1] == "" || segs[2] == "" {
			continue // not a <name>/<version>/<filename> storage shape
		}
		if normalizePackageName(segs[0]) != want {
			continue
		}
		entries = append(entries, indexEntry{filename: segs[2], path: n.Path, sha256: n.Sha256, size: n.Size})
	}
	// Sorted by filename, ties by full path — deterministic output keeps
	// the ETag stable (maven-npm-pypi.md section 3.2: entries are emitted
	// in filename order).
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].filename != entries[j].filename {
			return entries[i].filename < entries[j].filename
		}
		return entries[i].path < entries[j].path
	})
	return entries, nil
}

// projectNames lists the distinct normalized project names of the
// repository (the root index), sorted.
func (h *Handler) projectNames(ctx context.Context, r *http.Request, repoKey string) ([]string, error) {
	nodes, err := h.svc.List(ctx, adapter.PrincipalFrom(r.Context()), repoKey, "")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, n := range nodes {
		if strings.HasSuffix(n.Path, "/") {
			continue
		}
		segs := strings.Split(n.Path, "/")
		if len(segs) != 3 || segs[0] == "" || segs[1] == "" || segs[2] == "" {
			continue
		}
		seen[normalizePackageName(segs[0])] = true
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// writeIndex emits a rendered index page with its stable ETag and the
// If-None-Match 304 short-circuit. The ETag is the sha256 of the exact body
// bytes — an opaque, content-stable value, which is all PEP 503 demands
// (the client never parses it; maven-npm-pypi.md section 3.2, and the PRD's
// "any stable content hash" ruling).
func writeIndex(w http.ResponseWriter, r *http.Request, body []byte, contentType string) {
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:]) + `"`
	hdr := w.Header()
	hdr.Set("Content-Type", contentType)
	hdr.Set("ETag", etag)
	if etagMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	hdr.Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// wantsSimpleJSON applies the PEP 691 negotiation: JSON only when the
// client explicitly names the JSON media type in Accept. BinFlow keeps the
// Artifactory default (HTML) for every other spelling — a bare Accept: */*
// or text/html stays HTML (PRD FR-19).
func wantsSimpleJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), simpleJSONMediaType)
}

// escapePath percent-encodes each segment of a repo-relative path for a URL
// (href) while keeping the segment separators.
func escapePath(rel string) string {
	segs := strings.Split(rel, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// escapePathSegment escapes one path segment (the 302 Location target).
func escapePathSegment(seg string) string { return url.PathEscape(seg) }

// htmlEscape escapes text destined for HTML content or a double-quoted
// attribute value (maven-npm-pypi.md section 3.2: values are HTML-escaped;
// a stored filename like "a&b<c>.whl" must never inject markup).
func htmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#39;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
