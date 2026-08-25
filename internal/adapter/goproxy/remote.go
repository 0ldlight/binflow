package goproxy

import (
	"context"
	"io"
	"net/http"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The remote-repository face (goproxy.md section 6.2): every version-file
// GET walks repo.Service's pull-through engine (svc.Get dispatches on the
// class; the engine's upstream hop applies this protocol's re-escaping
// through the provider facet in provider.go). What lives HERE are the two
// proxy-only list endpoints: the upstream document is cached under the
// internal marker paths (section 3.2) and served verbatim, so the engine's
// TTL classes, negative cache, assumed-offline downgrade and stale service
// all apply unmodified.
//
// PUT on a remote repository never reaches an adapter branch: the service
// answers RE-05's read-only refusal (the BinFlow-wide remote write
// posture, which section 6.2 defers to).

// serveRemoteList serves GET @v/list off the pull-through chain: the
// upstream list body, cached at <module>/@v/.versionList (the metadata TTL
// class — the marker is in the expirable set, section 3.2/S4) and passed
// through byte-for-byte (S7: no rewrite of the upstream document).
func (h *Handler) serveRemoteList(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey string, t target) {
	rc, node, err := h.svc.Get(ctx, p, repoKey, versionListMarker(t.module))
	if err != nil {
		// Upstream unfound/negative-cache/offline all arrive here as the
		// unfound family — the official 404 that lets the go command fall
		// through to the next GOPROXY entry (section 6.2's "list/@latest
		// read failures are 404, never 5xx").
		h.writeError(w, err, repoKey, t.module)
		return
	}
	h.serveNode(ctx, w, r, node, rc, "text/plain; charset=utf-8")
}

// serveRemoteLatest serves GET @latest off the pull-through chain: the
// upstream @latest JSON cached at <module>@latest.latest (the
// reverse-engineered marker spelling — no '/' before "@latest", section
// 3.2/S3) and served verbatim.
func (h *Handler) serveRemoteLatest(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey string, t target) {
	rc, node, err := h.svc.Get(ctx, p, repoKey, latestMarker(t.module))
	if err != nil {
		h.writeError(w, err, repoKey, t.module)
		return
	}
	h.serveNode(ctx, w, r, node, rc, "application/json")
}

// upstreamBody reads one member document fully (the list/latest bodies are
// small; the engine already buffered the metadata class under its 64MB cap).
func upstreamBody(rc io.ReadSeekCloser) ([]byte, error) {
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	return io.ReadAll(rc)
}
