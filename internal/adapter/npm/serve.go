package npm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// GET/HEAD /<name> (packument), GET /<name>/<version>, GET/HEAD tarball,
// GET the domain root. Downloads inherit the M1 header set (X-Checksum-*
// triple, ETag = sha1 unquoted, Last-Modified, Accept-Ranges, Range 206/416,
// conditional 304) — the same contract the generic adapter serves, restated
// locally because adapter packages share no unexported code (area rule).

// serveRoot answers the domain-root connectivity probe: 200 with an empty
// body (spec section 0).
func (h *Handler) serveRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "GET and HEAD only on the npm domain root")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// servePackumentRoute dispatches GET/HEAD (packument) vs PUT (publish) on
// the package address.
func (h *Handler) servePackumentRoute(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *Principal, repoKey, name string) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		h.servePackument(ctx, w, r, p, repoKey, name)
	case http.MethodPut:
		h.servePublish(ctx, w, r, p, repoKey, name)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT")
		writeError(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on npm package documents")
	}
}

// servePackument renders the packument: ETag/X-Checksum-Sha1 = the STORED
// document's sha1 (spec section 2.4), If-None-Match -> 304 with the same
// headers, dist.tarball rewritten to this registry, SLIM negotiated by
// Accept (P1), _attachments never returned.
func (h *Handler) servePackument(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *Principal, repoKey, name string) {
	if err := validatePackageName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	doc, node, err := h.loadPackument(ctx, p, repoKey, name)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) {
			writeError(w, http.StatusNotFound, fmt.Sprintf(msgPackNotFound, name))
			return
		}
		h.writeServiceError(w, err)
		return
	}

	slim := acceptsSLIM(r.Header.Get("Accept"))
	body, ct, err := renderPackument(doc, name, requestScheme(r), r.Host, h.opts.BaseURL, repoKey, slim)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render packument: "+err.Error())
		return
	}

	etag := h.packumentSHA1(r.Context(), node, body)
	lastMod := parseNodeTime(node.UpdatedAt)
	if lastMod.IsZero() {
		lastMod = parseNodeTime(node.CreatedAt)
	}
	hdr := w.Header()
	hdr.Set("ETag", etag)
	hdr.Set(hdrChecksumSha1, etag)
	hdr.Set("Content-Type", ct)
	if !lastMod.IsZero() {
		hdr.Set("Last-Modified", lastMod.UTC().Format(http.TimeFormat))
	}
	if etagMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// serveTagOrVersion handles the one-segment tail: GET/HEAD reads one
// version's manifest; PUT is the legacy dist-tag spelling (pre-npm-8
// clients, spec endpoint table).
func (h *Handler) serveTagOrVersion(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *Principal, repoKey, name, tail string) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		h.serveVersion(ctx, w, r, p, repoKey, name, tail)
	case http.MethodPut:
		h.serveLegacyTagPut(ctx, w, r, p, repoKey, name, tail)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT")
		writeError(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported here")
	}
}

// serveVersion returns one version's manifest with its tarball rewritten.
func (h *Handler) serveVersion(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *Principal, repoKey, name, version string) {
	doc, _, err := h.loadPackument(ctx, p, repoKey, name)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) {
			writeError(w, http.StatusNotFound, fmt.Sprintf(msgPackNotFound, name))
			return
		}
		h.writeServiceError(w, err)
		return
	}
	m := mapOf(versionsOf(doc)[version])
	if m == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("version %s of package '%s' not found", version, name))
		return
	}
	out := copyManifest(m)
	if dist := distOf(out); dist != nil {
		dist["tarball"] = packumentURLPrefix(requestScheme(r), r.Host, h.opts.BaseURL, repoKey) +
			escapePathSegments(versionDistTarball(name, version, dist))
	}
	body, err := encodeDoc(out)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render version: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// serveTarball streams one tarball (GET/HEAD) with the M1 header set and the
// single-range contract.
func (h *Handler) serveTarball(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *Principal, repoKey, name, file string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "tarballs are read-only")
		return
	}
	if !isTarballFilename(file) {
		writeError(w, http.StatusNotFound, "not a tarball path: "+name+"/-/"+file)
		return
	}
	rc, node, err := h.svc.Get(ctx, p, repoKey, name+"/"+tarballDir+file)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	defer rc.Close() //nolint:errcheck // read-only fd
	h.streamNode(w, r, rc, node)
}

// streamNode renders a node download: checksum headers from the ledger,
// ETag = sha1 unquoted, Last-Modified, conditional 304, single Range 206 /
// unsatisfiable 416. Shared by tarballs (the packument renders its own
// body, so it does not stream through here).
func (h *Handler) streamNode(w http.ResponseWriter, r *http.Request, rc io.ReadSeekCloser, node *metadata.Node) {
	sums := h.digestsOf(r.Context(), node.Sha256)
	lastMod := parseNodeTime(node.UpdatedAt)
	if lastMod.IsZero() {
		lastMod = parseNodeTime(node.CreatedAt)
	}
	hdr := w.Header()
	if sums.sha256 != "" {
		hdr.Set(hdrChecksumSha256, sums.sha256)
	}
	if sums.sha1 != "" {
		hdr.Set(hdrChecksumSha1, sums.sha1)
		hdr.Set("ETag", sums.sha1)
	}
	if sums.md5 != "" {
		hdr.Set(hdrChecksumMd5, sums.md5)
	}
	if !lastMod.IsZero() {
		hdr.Set("Last-Modified", lastMod.UTC().Format(http.TimeFormat))
	}
	hdr.Set("Accept-Ranges", "bytes")
	hdr.Set("Content-Type", "application/octet-stream")

	if etagMatch(r.Header.Get("If-None-Match"), sums.sha1) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	rng, malformed, ignore := parseByteRange(r.Header.Get("Range"), node.Size)
	switch {
	case malformed:
		hdr.Set("Content-Range", "bytes */"+strconv.FormatInt(node.Size, 10))
		hdr.Del("Content-Length")
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	case !ignore && rng != nil:
		if _, err := rc.Seek(rng.start, io.SeekStart); err != nil {
			writeError(w, http.StatusInternalServerError, "seek blob for range: "+err.Error())
			return
		}
		hdr.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rng.start, rng.end, node.Size))
		hdr.Set("Content-Length", strconv.FormatInt(rng.end-rng.start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.CopyN(w, rc, rng.end-rng.start+1)
		return
	}

	hdr.Set("Content-Length", strconv.FormatInt(node.Size, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, rc) // client aborts surface as short writes, not errors
}

// writeJSONBody emits one JSON success payload.
func writeJSONBody(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(body)
}

// acceptsSLIM reports whether the client negotiated the install-v1 variant.
func acceptsSLIM(accept string) bool {
	for _, part := range strings.Split(accept, ",") {
		if strings.HasPrefix(strings.TrimSpace(part), contentTypeSLIM) {
			return true
		}
	}
	return false
}

// requestScheme resolves http vs https (X-Forwarded-Proto honored — the
// docker realm precedent).
func requestScheme(r *http.Request) string {
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return strings.TrimSpace(strings.Split(proto, ",")[0])
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// parseNodeTime parses an RFC3339 node timestamp, zero on failure.
func parseNodeTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
