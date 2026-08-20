// Package console serves the embedded web console (ADR-0014 as amended by
// the T-108 errata, PRD FR-23): a React SPA mounted at /binflow/ui/** with
// its fingerprinted build output on the shared /binflow/assets/** mount.
//
// The SPA is built from web/ by `make console` (vite build -> copy into
// dist/) and embedded at compile time; a committed placeholder shell keeps
// the embed complete on a fresh clone, so `make build` needs no node
// toolchain (the placeholder strategy — see dist/placeholder.html).
package console

import (
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

// distFS embeds the console build output. dist/placeholder.html is COMMITTED
// so the pattern always matches and a node-less checkout builds (ADR-0002:
// the binary always carries a console); dist/index.html and dist/assets/
// arrive from `make console` and are gitignored.
//
//go:embed dist
var distFS embed.FS

// The mount contract (PRD FR-23 / ADR-0014 T-108 errata).
const (
	// uiSegment is the SPA's exclusive segment: /binflow/ui and everything
	// under /binflow/ui/ serve the shell (history fallback — the router owns
	// every other /binflow/<repo>/<path> spelling, and this handler never
	// touches those).
	uiSegment = "/binflow/ui"
	// assetMount serves the fingerprinted build output. It lives OUTSIDE the
	// ui segment on purpose: with hashed, immutable files at their own mount
	// the segment-internal fallback can stay dumb — every path under
	// /binflow/ui/ is the shell, no file special-casing.
	assetMount = "/binflow/assets"

	shellPath        = "index.html"       // inside distFS
	unbuiltShellPath = "placeholder.html" // inside distFS, committed
	assetDir         = "assets"           // inside distFS
)

// Cache policies (PRD FR-23-AC3/W02): the shell is negotiated on every load;
// fingerprinted assets are content-addressed and therefore infinitely
// cacheable.
const (
	shellCache  = "no-cache"
	assetCache  = "public, max-age=31536000, immutable"
	indexHTMLCT = "text/html; charset=utf-8"
)

// Handler returns the console's HTTP surface. It answers exactly three
// shapes and nothing else — the mount redirect, the ui segment, the asset
// mount — so mounting it can never shadow the content plane:
//
//	GET /binflow, /binflow/        -> 301 /binflow/ui/   (CE-01)
//	GET /binflow/ui/**             -> SPA shell, no-cache (history fallback)
//	GET /binflow/assets/<hash>.js  -> immutable when present, 404 otherwise
//	anything else                  -> 404 (out of segment; /binflow/<repo>/…
//	                                  stays the router/content plane's)
//
// The httpapi router binds this handler at the /binflow root seam (Deps
// .Console) and T-91 mounts the ui/assets segments onto the same handler.
func Handler() http.Handler {
	return http.HandlerFunc(serve)
}

// Dist returns the embedded build tree (read-only). It is the test seam for
// asserting what `make console` actually embedded; production code serves
// through Handler.
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Unreachable: the embed directive guarantees the dist directory.
		panic("console: embedded dist: " + err.Error())
	}
	return sub
}

func serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "console serves GET and HEAD only", http.StatusMethodNotAllowed)
		return
	}
	// Judge the CLEANED path: a /binflow/ui/../<repo> spelling must not
	// sneak past the segment prefix (NFR-S18 posture; the router's own
	// defenses sit upstream, this is the handler's backstop).
	p := path.Clean("/" + r.URL.EscapedPath())

	switch {
	case p == "/binflow" || p == "/binflow/":
		http.Redirect(w, r, uiSegment+"/", http.StatusMovedPermanently)
	case p == uiSegment || p == uiSegment+"/" || strings.HasPrefix(p, uiSegment+"/"):
		serveShell(w, r)
	case strings.HasPrefix(p, assetMount+"/"):
		serveAsset(w, r, strings.TrimPrefix(p, assetMount+"/"))
	default:
		http.NotFound(w, r)
	}
}

// serveShell answers everything in the ui segment with the SPA shell
// (segment-internal history fallback: /binflow/ui/repositories is the app's
// route, not a file). When `make console` has not run (fresh clone, no
// node), the committed placeholder shell is served instead — same shape,
// same cache policy, honest content.
func serveShell(w http.ResponseWriter, r *http.Request) {
	name := shellPath
	if _, err := fs.Stat(distFS, path.Join("dist", shellPath)); err != nil {
		name = unbuiltShellPath
	}
	body, err := distFS.ReadFile(path.Join("dist", name))
	if err != nil {
		// Unreachable: placeholder.html is committed and always embedded.
		http.Error(w, "console shell missing", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", indexHTMLCT)
	h.Set("Cache-Control", shellCache)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// serveAsset answers /binflow/assets/<name> from dist/assets/. Names are
// content-fingerprinted by the build, so a hit is served immutable and a
// miss is a plain 404 (an unbuilt or long-gone hash — never a fallback).
func serveAsset(w http.ResponseWriter, r *http.Request, name string) {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	full := path.Join("dist", assetDir, name)
	f, err := distFS.Open(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // read-only embedded file
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "console asset unreadable", http.StatusInternalServerError)
		return
	}
	ct := mime.TypeByExtension(path.Ext(name))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", assetCache)
	// Zero modtime: embedded bytes have no meaningful mtime, so no
	// Last-Modified/conditional dance — immutability is the whole policy.
	http.ServeContent(w, r, name, time.Time{}, rs)
}
