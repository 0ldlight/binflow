package pypi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// serveDownload serves a distribution file (PE-03): the packages/ mount the
// index hrefs point at, and the bare content path — the same node's second
// entrance. The response is the M1 download contract (rest-api.md section
// 1.4) pip relies on for cache correctness: X-Checksum-* headers, ETag =
// the unquoted sha1, Last-Modified, Accept-Ranges, single-range 206/416 and
// conditional 304. The range/conditional machinery is the generic adapter's
// T-13 contract ported verbatim (per-protocol copy, the docker range.go
// precedent — extracting a shared package is an architect call, not a
// protocol ticket's).
func (h *Handler) serveDownload(w http.ResponseWriter, r *http.Request, repoKey, path string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed,
			fmt.Sprintf("method %s is not supported on PyPI distribution paths (uploads use POST on the repository root)", r.Method))
		return
	}
	if path == "" || path == "/" {
		writeError(w, http.StatusNotFound, notFoundMessage(repoKey, segPackages))
		return
	}

	ctx := r.Context()
	rc, node, err := h.svc.Get(ctx, adapter.PrincipalFrom(ctx), repoKey, path)
	if err != nil {
		if errors.Is(err, repo.ErrIsFolder) {
			// Folder rows carry no distribution body; the plain download
			// 404 is the honest answer (the generic adapter's rule).
			writeError(w, http.StatusNotFound, notFoundMessage(repoKey, path))
			return
		}
		h.writeServiceError(w, err, r.Method, repoKey, path)
		return
	}
	defer rc.Close() //nolint:errcheck // read-only fd

	sums := h.digestsOf(ctx, node)
	lastMod := parseRFC3339(node.UpdatedAt)
	if lastMod.IsZero() {
		lastMod = parseRFC3339(node.CreatedAt)
	}

	hdr := w.Header()
	if sums.sha256 != "" {
		hdr.Set("X-Checksum-Sha256", sums.sha256)
	}
	if sums.sha1 != "" {
		hdr.Set("X-Checksum-Sha1", sums.sha1)
	}
	if sums.md5 != "" {
		hdr.Set("X-Checksum-Md5", sums.md5)
	}
	if sums.sha1 != "" {
		hdr.Set("ETag", sums.sha1) // unquoted sha1, rest-api.md 1.4
	}
	if !lastMod.IsZero() {
		hdr.Set("Last-Modified", lastMod.UTC().Format(http.TimeFormat))
	}
	hdr.Set("Accept-Ranges", "bytes")
	hdr.Set("Content-Type", downloadContentType(node))

	// Conditional requests first: a fresh store answers 304 with no body.
	if evalConditional(r, sums.sha1, lastMod) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// Then Range: one satisfiable byte range slices the stream (206); an
	// unsatisfiable/malformed spec is a 416; anything BinFlow does not
	// implement (multi-range, other units) is ignored and serves the full
	// 200 body (FR-4-AC14: never 5xx).
	parser := httpRangeParser{total: node.Size}
	rng, malformed, ignore := parser.parseRange(r.Header.Get("Range"))
	switch {
	case malformed:
		hdr.Set("Content-Range", "bytes */"+strconv.FormatInt(node.Size, 10))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	case !ignore && rng.length() > 0:
		if _, err := rc.Seek(rng.start, io.SeekStart); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("seek blob for range: %v", err))
			return
		}
		hdr.Set("Content-Range", rng.contentRange(node.Size))
		hdr.Set("Content-Length", strconv.FormatInt(rng.length(), 10))
		w.WriteHeader(http.StatusPartialContent)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.CopyN(w, rc, rng.length())
		return
	}

	hdr.Set("Content-Length", strconv.FormatInt(node.Size, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, rc) // client aborts surface as short writes, not errors
}

// downloadContentType resolves the served Content-Type: the stored node mime
// (the upload's part Content-Type, or the octet-stream default) with a
// wheel/sdist extension mapping for nodes seeded by other means.
func downloadContentType(n *metadata.Node) string {
	if n.Mime != "" {
		return n.Mime
	}
	switch {
	case strings.HasSuffix(n.Path, ".whl"):
		return "application/zip" // a wheel IS a zip archive
	case strings.HasSuffix(n.Path, ".tar.gz"), strings.HasSuffix(n.Path, ".tgz"):
		return "application/gzip"
	default:
		return "application/octet-stream"
	}
}

// digestTriple is a node's three digests; sha1/md5 are ledger facts keyed by
// the blob (ADR-0006: no sidecar files).
type digestTriple struct{ sha256, sha1, md5 string }

// digestsOf resolves the node's three digests. sha256 lives on the node;
// sha1/md5 come from the blobs ledger. A ledger miss degrades to
// sha256-only — the download keeps serving (the generic adapter's rule).
func (h *Handler) digestsOf(ctx context.Context, node *metadata.Node) digestTriple {
	t := digestTriple{sha256: node.Sha256}
	if h.blobs == nil || node.Sha256 == "" {
		return t
	}
	b, err := h.blobs.Get(ctx, node.Sha256)
	if err != nil || b == nil {
		return t
	}
	t.sha1, t.md5 = b.Sha1, b.Md5
	return t
}

// parseRFC3339 parses an RFC3339 timestamp, returning zero on failure.
func parseRFC3339(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
