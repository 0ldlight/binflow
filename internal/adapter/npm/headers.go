package npm

import (
	"context"
	"crypto/sha1" //nolint:gosec // G401: npm ETag/X-Checksum-Sha1 protocol digest, never a security primitive
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// Download header set and the two conditional mechanics (weak If-None-Match,
// single byte range). The generic adapter owns the same rules unexported;
// adapter packages share no unexported code (area rule), so the npm plane
// restates them — behavior-identical, kept deliberately compact.

// Protocol-facing header names (M1 download contract).
const (
	hdrChecksumSha256 = "X-Checksum-Sha256"
	hdrChecksumSha1   = "X-Checksum-Sha1"
	hdrChecksumMd5    = "X-Checksum-Md5"
)

// readerHints copies the structural ExtraHeaders() hints off a body stream
// (the generic adapter's T-66 seam, restated locally — adapter packages share
// no unexported code, the area rule): a remote member's fetch carries
// X-BinFlow-Cache / X-Binflow-Upstream-Error, a virtual resolution's reader
// adds X-BinFlow-Resolved-From on top of those (T-71). A plain local blob
// carries none; nil means "nothing to say".
func readerHints(body io.Reader) http.Header {
	extra, ok := body.(interface{ ExtraHeaders() http.Header })
	if !ok {
		return nil
	}
	h := http.Header{}
	for k, vv := range extra.ExtraHeaders() {
		h[k] = append([]string(nil), vv...)
	}
	return h
}

// applyHints merges collected reader hints into the response headers.
func applyHints(dst http.Header, hints http.Header) {
	for k, vv := range hints {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// digestTripleLedger is the sha256-keyed triple of one blob (sha256 from the
// node, sha1/md5 from the blobs ledger — ADR-0006 keeps no sidecar files).
type digestTripleLedger struct{ sha256, sha1, md5 string }

// digestsOf resolves a node's digest triple; a ledger miss degrades to the
// node's sha256 alone (the download keeps serving while consistency tooling
// notices the gap).
func (h *Handler) digestsOf(ctx context.Context, sha256 string) digestTripleLedger {
	t := digestTripleLedger{sha256: sha256}
	if h.ledger == nil || sha256 == "" {
		return t
	}
	b, err := h.ledger.Get(ctx, sha256)
	if err != nil || b == nil {
		return t
	}
	t.sha1, t.md5 = b.Sha1, b.Md5
	return t
}

// packumentSHA1 is the packument response's ETag: the STORED document's sha1
// (spec section 2.4 — the official registry spec leaves metadata ETags
// undefined; Artifactory pins the document file). The ledger row carries it;
// without a ledger the served body's sha1 stands in (identical for a
// freshly-written document whose rendering is byte-stable modulo the tarball
// rewrite, and never wrong enough to break npm's cache semantics).
func (h *Handler) packumentSHA1(ctx context.Context, node *metadata.Node, served []byte) string {
	if h.ledger != nil && node != nil && node.Sha256 != "" {
		if b, err := h.ledger.Get(ctx, node.Sha256); err == nil && b != nil && b.Sha1 != "" {
			return b.Sha1
		}
	}
	sum := sha1.Sum(served) //nolint:gosec // G401: the packument ETag is the spec-pinned sha1
	return hex.EncodeToString(sum[:])
}

// etagMatch implements weak comparison over a comma-separated entity-tag
// list. BinFlow's stored form is an unquoted sha1; npm's make-fetch-happen
// sends quoted tags, so bare/"quoted"/W/"weak" all normalize before
// comparing; "*" matches any existing representation (RFC 9110 13.1.2).
func etagMatch(header, etag string) bool {
	if etag == "" || header == "" {
		return false
	}
	for _, tag := range strings.Split(header, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" {
			return true
		}
		if normalizeETag(tag) == etag {
			return true
		}
	}
	return false
}

// normalizeETag strips W/ and DQUOTEs (token shaping of an opaque digest).
func normalizeETag(tag string) string {
	if len(tag) >= 2 && tag[0:2] == "W/" {
		tag = tag[2:]
	}
	if len(tag) >= 2 && tag[0] == '"' && tag[len(tag)-1] == '"' {
		tag = tag[1 : len(tag)-1]
	}
	return strings.ToLower(strings.TrimSpace(tag))
}

// byteRange is an inclusive interval within the entity.
type byteRange struct{ start, end int64 }

// parseByteRange evaluates one Range header against total: the single
// satisfiable interval, malformed=true (answer 416), or ignore=true (serve
// the full body — absent header, non-bytes unit, multi-range set). The npm
// clients never range-fetch, but the M1 download contract is uniform.
func parseByteRange(header string, total int64) (rng *byteRange, malformed, ignore bool) {
	spec := strings.TrimSpace(header)
	if spec == "" {
		return nil, false, true
	}
	eq := strings.IndexByte(spec, '=')
	if eq < 0 {
		return nil, false, true
	}
	if !strings.EqualFold(strings.TrimSpace(spec[:eq]), "bytes") {
		return nil, false, true
	}
	set := strings.TrimSpace(spec[eq+1:])
	if set == "" || strings.ContainsRune(set, ',') {
		return nil, false, true
	}
	dash := strings.IndexByte(set, '-')
	if dash < 0 {
		return nil, true, false
	}
	first, last := set[:dash], set[dash+1:]
	if strings.ContainsAny(first+last, " \t") || (first == "" && last == "") {
		return nil, true, false
	}
	switch {
	case first == "": // suffix bytes=-N
		n, err := strconv.ParseInt(last, 10, 64)
		if err != nil || n <= 0 || total == 0 {
			return nil, true, false
		}
		if n > total {
			n = total
		}
		return &byteRange{start: total - n, end: total - 1}, false, false
	case last == "": // open bytes=N-
		start, err := strconv.ParseInt(first, 10, 64)
		if err != nil || start < 0 || total == 0 || start >= total {
			return nil, true, false
		}
		return &byteRange{start: start, end: total - 1}, false, false
	default:
		start, err1 := strconv.ParseInt(first, 10, 64)
		end, err2 := strconv.ParseInt(last, 10, 64)
		if err1 != nil || err2 != nil || start < 0 || start > end || total == 0 || start >= total {
			return nil, true, false
		}
		if end >= total {
			end = total - 1
		}
		return &byteRange{start: start, end: end}, false, false
	}
}
