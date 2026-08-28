package cargo

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The remote-repository face (spec section 8's remote row, T-316): a
// sparse pull-through proxy in front of one upstream registry URL.
//
//	GET  index/config.json         synthesized self-pointing (dl/api cite
//	                               THIS remote repository, so every
//	                               download crosses the cache) + a
//	                               best-effort fetch of the upstream's own
//	                               config, cached verbatim as
//	                               config.original.json (S4's translation
//	                               chain, preserved upstream form)
//	GET  index/{pkgPath}           svc.Get → the FR-20 engine (negative
//	                               cache, dual TTL, guarded upstream hop
//	                               through provider.UpstreamPath, MISS
//	                               landing, stale-while-error)
//	GET  v1/crates/{n}/{v}/download  the same engine chain at the .crate
//	                               storage path
//	GET  api/v1/crates?q=…         the upstream search endpoint, proxied
//	                               verbatim and CACHED under the
//	                               query-keyed marker path (the engine's
//	                               TTL/negative machinery applies per
//	                               query); an upstream fault answers the
//	                               CG-2 class-8 conflict face (409)
//	PUT  api/v1/crates/new & the   the read-only refusals (RE-05, before
//	     yank family                  any body is drained)
//	bare content                    GET pulls through; PUT answers the
//	                               service's 405; DELETE is RE-06 cache
//	                               eviction
//
// The upstream must speak the cargo sparse wire grammar under its
// configured URL (another BinFlow, an Artifactory cargo face, or any
// single-host sparse-compatible registry): index/config.json,
// index/{pkgPath}, v1/crates/{n}/{v}/download, api/v1/crates. The public
// crates.io splits its index and download planes across two hosts, which
// a pure-path upstream hop cannot address — proxying crates.io directly
// requires a BinFlow/Artifactory remote of it in front (the deviation
// register's entry, spec section 8's BinFlow row).

// msgRemoteWrite is the remote write refusal's body (the repo package's
// RE-05 wording, restated so it rides the cargo errors envelope without
// an import; the conan/deb posture).
func msgRemoteWrite(repoKey string) string {
	return fmt.Sprintf("Remote repository '%s' is a read-only proxy cache; deployments to remote repositories are not accepted.", repoKey)
}

// serveRemote dispatches one request on a remote repository. The read
// planes reuse the local arms verbatim — svc.Get on a remote repository
// IS the pull-through chain, so the index/download/bare readers and the
// synthesized config need no remote twin of their own.
func (h *Handler) serveRemote(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey string, rt route) {
	switch rt.kind {
	case kindRoot:
		// The repository-root probe (spec section 2), shared with local.
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			w.WriteHeader(http.StatusOK)
		default:
			w.Header().Set("Allow", "GET, HEAD")
			writeEnvelope(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the repository root")
		}
	case kindGitFace:
		// Sparse only, shared with local: the deprecation wording.
		writeEnvelope(w, http.StatusNotFound, msgGitDeprecated)

	case kindConfig:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.fetchOriginalConfig(ctx, p, repoKey)
			h.serveConfig(w, r, repoKey)
		case http.MethodPut, http.MethodDelete, http.MethodPost:
			writeEnvelope(w, http.StatusForbidden,
				"'index/config.json' is server-generated (the synthesized sparse entry document); direct writes are not permitted")
		default:
			w.Header().Set("Allow", "GET, HEAD")
			writeEnvelope(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on config.json")
		}

	case kindIndexFile:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.serveIndexFile(ctx, w, r, p, repoKey, rt.pkgPath)
		case http.MethodPut, http.MethodPost:
			// The derived write family dies at the read-only door: the
			// shared arm would land nothing (svc refuses), so the refusal
			// answers directly with the RE-05 wording.
			w.Header().Set("Allow", "GET, HEAD, DELETE")
			writeEnvelope(w, http.StatusMethodNotAllowed, msgRemoteWrite(repoKey))
		case http.MethodDelete:
			// RE-06 like the bare face: DELETE on a remote repository
			// evicts the cached copy (204/404), never anything upstream.
			if err := h.svc.Delete(ctx, p, repoKey, segIndex+"/"+rt.pkgPath); err != nil {
				h.writeError(w, err, repoKey, segIndex+"/"+rt.pkgPath)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Header().Set("Allow", "GET, HEAD, DELETE")
			writeEnvelope(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on index files")
		}

	case kindDownload:
		if !h.requireMethod(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveDownload(ctx, w, r, p, repoKey, rt)

	case kindSearch:
		if !h.requireMethod(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveRemoteSearch(ctx, w, r, p, repoKey)

	case kindPublish, kindYank, kindUnyank:
		// The protocol write family refuses before any body drains
		// (spec section 8: publish/yank/unyank 不路由 — the BinFlow-wide
		// remote write posture).
		w.Header().Set("Allow", http.MethodGet)
		writeEnvelope(w, http.StatusMethodNotAllowed, msgRemoteWrite(repoKey))

	case kindBareContent:
		// The bare face's own arms carry the class: GET pulls through the
		// engine, PUT answers the service's 405, DELETE is RE-06.
		h.serveBareContent(ctx, w, r, p, repoKey, rt.path)

	default:
		writeEnvelope(w, http.StatusNotFound, "not found")
	}
}

// fetchOriginalConfig pulls the upstream's own config.json through the
// engine so it lands (and TTL-serves) as config.original.json at the
// repository root — S4's preserved-upstream-form copy. Best-effort by
// contract: the synthesized document this request actually serves never
// depends on it, so an unfetchable upstream (offline window, refused
// credentials, a plain 404) must not break a client's first hop.
func (h *Handler) fetchOriginalConfig(ctx context.Context, p *repo.Principal, repoKey string) {
	rc, _, err := h.svc.Get(ctx, p, repoKey, fileOriginalConfig)
	if err != nil {
		return // best-effort: the engine's own log line names the fault
	}
	_ = rc.Close() //nolint:errcheck // read-only fd; the fetch was the point
}

// serveRemoteSearch proxies GET api/v1/crates?q=…&per_page=…: the VERBATIM
// query string keys the cache marker (query order, unknown parameters and
// repeats all preserved byte-for-byte), the engine fetches the upstream
// search endpoint through the marker's UpstreamPath translation, and the
// landed body serves as-is — the upstream's search contract IS the
// response. The unfound family keeps its honest 404; the engine's
// hard-fault answers (hardFail's 502, the over-cap 502) map onto CG-2
// class 8's conflict face (409), the anchor table's "search 上游异常 →
// 409 + errors 信封". The DEFAULT upstream-fault posture is the engine's
// own — assumed-offline downgrade to the unfound 404 — which diverges
// from Artifactory's blanket 409 and is registered as such (spec section
// 8's deviation register): the FR-20 chain is BinFlow-wide remote
// behavior, not this endpoint's to override by message sniffing.
func (h *Handler) serveRemoteSearch(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey string) {
	marker := searchCachePath(r.URL.RawQuery)
	rc, node, err := h.svc.Get(ctx, p, repoKey, marker)
	if err != nil {
		var se *repo.StatusError
		if errors.As(err, &se) && se.Code >= 500 {
			writeEnvelope(w, http.StatusConflict, "search upstream failed: "+se.Message)
			return
		}
		h.writeError(w, err, repoKey, marker)
		return
	}
	h.serveNode(ctx, w, r, node, rc, "application/json")
}
