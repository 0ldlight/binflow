// Package docs serves the embedded help documentation site (ADR-0011, PRD
// FR-41/DC-01): a Docusaurus static build mounted at /binflow/docs/**.
//
// The site is built from docs/user by `make docs` (docusaurus build ->
// copy into dist/) and embedded at compile time — the same shape as the
// console's web/ -> internal/console seam (ADR-0011 delivery clause). A
// committed placeholder shell keeps the embed complete on a fresh clone,
// so `make build` needs no node toolchain (the T-89 placeholder strategy;
// see dist/placeholder.html).
//
// The handler is deliberately simpler than the console's: a docs site is a
// plain multi-page static tree (every doc compiles to <route>/index.html),
// so there is no SPA history fallback and no out-of-segment asset mount —
// everything the browser loads, including the local search index, lives
// inside the segment (NFR-S29 zero-CDN posture).
package docs

import (
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// distFS embeds the docs site build output. dist/placeholder.html is
// COMMITTED so the pattern always matches and a node-less checkout builds;
// dist/index.html, dist/404.html and dist/assets/ arrive from `make docs`
// and are gitignored (same convention as internal/console/dist).
//
//go:embed dist
var distFS embed.FS

// The mount contract (PRD FR-41 / ADR-0011; the repo key "docs" has been
// reserved since the ADR-0008 T-108 union, so the segment can never shadow
// a repository).
const (
	// segment is the site's exclusive segment: /binflow/docs and everything
	// under /binflow/docs/. Content paths /binflow/<repo>/<path> never
	// reach this handler (the router dispatches the segment first).
	segment = "/binflow/docs"

	indexPath        = "index.html"       // inside distFS
	unbuiltIndexPath = "placeholder.html" // inside distFS, committed
	notFoundPath     = "404.html"         // inside distFS, from the build
)

// Cache policies (PRD FR-41-AC2): HTML, the search index and the sitemap
// change with the embedded binary, so they negotiate on every load; the
// build output under assets/ is content-fingerprinted (hashes in filenames)
// and therefore infinitely cacheable.
const (
	pageCache  = "no-cache"
	assetCache = "public, max-age=31536000, immutable"
)

// Handler returns the docs site's HTTP surface. It answers GET/HEAD only,
// consults no credential of any kind — the help center is product
// self-description and stays readable anonymously even on
// anonymous_access=false instances (PRD FR-41, the /healthz posture) — and
// never routes outside its segment:
//
//	GET /binflow/docs          -> 301 /binflow/docs/   (canonical form)
//	GET /binflow/docs/         -> index.html (placeholder shell when unbuilt)
//	GET /binflow/docs/<p>      -> dist/<p>, or <p>/index.html for a directory
//	GET /binflow/docs/assets/**-> immutable when present, 404 otherwise
//	miss                         -> dist/404.html with status 404
//	anything else               -> 404 (out of segment; /binflow/<repo>/…
//	                               stays the router/content plane's)
//
// The httpapi router binds this handler at the /binflow root seam
// (Deps.Docs; nil defaults to it — see server.New).
func Handler() http.Handler {
	return http.HandlerFunc(serve)
}

// Dist returns the embedded build tree (read-only). It is the test seam for
// asserting what `make docs` actually embedded; production code serves
// through Handler.
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Unreachable: the embed directive guarantees the dist directory.
		panic("docs: embedded dist: " + err.Error())
	}
	return sub
}

func serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "docs serves GET and HEAD only", http.StatusMethodNotAllowed)
		return
	}
	// Canonical form first, judged on the RAW spelling: path.Clean erases
	// the trailing slash, so the segment root's own slashed spelling would
	// become the bare segment and trigger a self-redirect loop. Handle both
	// canonical forms before Clean. The site's asset and search-index links
	// are root-absolute against baseUrl /binflow/docs/, and relative links
	// inside pages assume the trailing slash.
	if raw := r.URL.EscapedPath(); raw == segment {
		http.Redirect(w, r, segment+"/", http.StatusMovedPermanently)
		return
	} else if raw == segment+"/" {
		serveIndex(w, r)
		return
	}
	// Judge the CLEANED path (the console handler's backstop habit): a
	// /binflow/docs/../<repo> spelling must not sneak past the segment
	// prefix (NFR-S18 posture; the router's own defenses sit upstream).
	p := path.Clean("/" + r.URL.EscapedPath())

	if p == segment {
		// A dot-segment spelling of the segment root (e.g. /binflow/docs/.)
		// — canonicalize the same way.
		http.Redirect(w, r, segment+"/", http.StatusMovedPermanently)
		return
	}
	if !strings.HasPrefix(p, segment+"/") {
		http.NotFound(w, r)
		return
	}
	rel := strings.TrimPrefix(p, segment+"/")
	if rel == "" {
		serveIndex(w, r)
		return
	}
	// The segment tail arrived escaped; file names in the build output are
	// ASCII-safe hashes and slugs, but unescape anyway so an encoded
	// spelling resolves the same file as the raw one.
	if decoded, err := url.PathUnescape(rel); err == nil {
		rel = decoded
	}
	servePath(w, r, path.Clean(rel))
}

// serveIndex answers the segment root with the site shell. When `make
// docs` has not run (fresh clone, no node), the committed placeholder
// shell is served instead — same shape, same cache policy, honest content.
func serveIndex(w http.ResponseWriter, r *http.Request) {
	name := indexPath
	if _, err := fs.Stat(distFS, path.Join("dist", indexPath)); err != nil {
		name = unbuiltIndexPath
	}
	body, err := distFS.ReadFile(path.Join("dist", name))
	if err != nil {
		// Unreachable: placeholder.html is committed and always embedded.
		http.Error(w, "docs shell missing", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", pageCache)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// servePath answers /binflow/docs/<rel> from dist/<rel>. A directory tail
// resolves to its index.html (Docusaurus compiles every page that way, so
// /binflow/docs/integrations/maven/ keeps working); a miss renders the
// build's own 404 page under status 404 when available.
func servePath(w http.ResponseWriter, r *http.Request, rel string) {
	if rel == "" || rel == "." || strings.HasPrefix(rel, "../") || rel == ".." {
		http.NotFound(w, r)
		return
	}
	name := rel
	if info, err := fs.Stat(distFS, path.Join("dist", rel)); err == nil && info.IsDir() {
		name = path.Join(rel, indexPath)
	}
	f, err := distFS.Open(path.Join("dist", name))
	if err != nil {
		serveNotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // read-only embedded file
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "docs file unreadable", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", contentType(name))
	// Fingerprinted build output is immutable; everything else (HTML, the
	// search index, the sitemap) revalidates on every load.
	if strings.HasPrefix(name, "assets/") {
		h.Set("Cache-Control", assetCache)
	} else {
		h.Set("Cache-Control", pageCache)
	}
	// Zero modtime: embedded bytes have no meaningful mtime, so no
	// Last-Modified/conditional dance — fingerprinting is the whole policy.
	http.ServeContent(w, r, path.Base(name), time.Time{}, rs)
}

// serveNotFound renders the build's 404 page under status 404; without a
// build (placeholder-only embed) it falls back to the plain 404.
func serveNotFound(w http.ResponseWriter, r *http.Request) {
	body, err := distFS.ReadFile(path.Join("dist", notFoundPath))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", pageCache)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusNotFound)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// contentType maps the build's file extensions explicitly: charset=utf-8
// on text shapes is load-bearing (the pages are Chinese — a bare
// text/html from the host's mime table would garble them), and .js/.svg
// answers must survive a bare mime database.
func contentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".xml":
		return "application/xml; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".woff2":
		return "font/woff2"
	}
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}
