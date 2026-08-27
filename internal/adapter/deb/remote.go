package deb

// The remote-repository face (debian.md section 8's remote row, S9): an
// apt mirror pull-through. Every read rides repo.Service's engine —
// svc.Get on a remote repository runs the FR-20 chain (negative cache,
// TTL-classed copy through the deb provider's Classify split: everything
// under dists/ is the expirable metadata class, pool/ artifacts the
// immutable content class, guarded upstream fetch with the
// stale-while-error downgrade) — so this file owns almost nothing:
//
//   - GET/HEAD stream through the engine verbatim (the upstream's
//     Release/Packages/by-hash tree lands cached under the repository
//     key; the X-BinFlow-Cache hints ride the reader);
//   - PUT answers the read-only 405 IN-HANDLER (the repo package's own
//     RE-05 refusal, restated so the debPUT arm never spools and parses
//     a body the service is going to refuse anyway);
//   - DELETE rides the service's RE-06 arm — the remote-class DELETE is
//     CACHE EVICTION (drop the local copy, the next GET re-fetches),
//     which is section 4.3's evictItemInCache semantics on the standard
//     content plane. debian.md's coarse "PUT/DELETE 拒绝" (medium
//     confidence) predates the BinFlow-wide RE-06 ruling; the divergence
//     register carries the note.
//
// The S9 "path normalization" (locate /dists/ in the request path and
// re-anchor on the suffix) is an Artifactory-ism for upstreams whose URL
// does not name the archive root. BinFlow's remote model pins the
// ARCHIVE-ROOT posture instead — the upstream URL carries any nested
// prefix (http://deb.debian.org/debian) and the request path appends
// verbatim, exactly like every other storage-path remote (generic,
// conan) — so no rewriting happens here; registered in the report.
//
// The indexCached family (section 4.3: back-fill deb.* coordinate
// properties on cached .debs, the `<remote>-cache` key shape) is NOT
// implemented: BinFlow caches under the remote key itself (no hidden
// -cache repository), the remote serves the upstream's own indexes
// verbatim (no locally-rendered index to feed), and deb.metadata.*
// facts arrive through the search plane's parse-on-demand. Leftover
// with the analysis in the T-314 report.

import (
	"context"
	"fmt"
	"net/http"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// msgRemoteReadOnly restates the service's RE-05 wording (the conan
// posture: adapter packages restate rather than reach around the seam).
const msgRemoteReadOnly = "Remote repository '%s' is a read-only proxy cache; deployments to remote repositories are not accepted."

// refuseRemoteWrite answers any PUT on a remote repository with the
// read-only 405 BEFORE the body is drained (the debPUT arm's spool and
// control parse would be wasted work on a refusal the service pins
// anyway).
func refuseRemoteWrite(w http.ResponseWriter, repoKey string) {
	w.Header().Set("Allow", http.MethodGet)
	writeText(w, http.StatusMethodNotAllowed, fmt.Sprintf(msgRemoteReadOnly, repoKey))
}

// remoteContentType pins the served type per wire shape (the local
// arms' per-kind pinning, one table).
func remoteContentType(rt route, rel string) string {
	switch rt.kind {
	case kindIndex:
		return indexContentType(rel)
	case kindDeb:
		return "application/vnd.debian.binary-package"
	case kindDsc:
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

// serveRemote dispatches the content plane on a remote repository: the
// reads ride the engine, writes refuse (PUT) or evict (DELETE), and the
// root probe answers the class-independent 200.
func (h *Handler) serveRemote(ctx context.Context, w http.ResponseWriter, r *http.Request, repoKey, rel string, rt route) {
	if rt.kind == kindRoot {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			w.WriteHeader(http.StatusOK)
		default:
			h.methodNotAllowed(w, r, "GET, HEAD")
		}
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		h.serveStoredFile(ctx, w, r, repoKey, rel, remoteContentType(rt, rel))
	case http.MethodPut:
		refuseRemoteWrite(w, repoKey)
	case http.MethodDelete:
		// RE-06: drop the cached copy (node rows plus the engine's cache
		// validator entry) — the next GET re-fetches upstream. A
		// never-cached path answers the shared not-found (the local
		// plane's own DELETE-of-missing shape).
		p := adapter.PrincipalFrom(ctx)
		if err := h.svc.Delete(ctx, p, repoKey, rel); err != nil {
			h.writeError(w, err, repoKey, rel)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		h.methodNotAllowed(w, r, "GET, HEAD, PUT, DELETE")
	}
}
