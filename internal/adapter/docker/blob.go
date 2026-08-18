package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The blob read/cancel plane (DE-06/DE-07/DE-14) plus the shared helpers of
// the blob domain: digest parsing, the layout paths, and the empty-layer
// special case.

// Protocol-owned header names of the blob domain.
const (
	hdrChecksumSha256 = "X-Checksum-Sha256"
	hdrChecksumSha1   = "X-Checksum-Sha1"
	hdrChecksumMd5    = "X-Checksum-Md5"

	hdrContentDigest = "Docker-Content-Digest"

	mimeOctetStream = "application/octet-stream"

	// blobsTail is the route tail prefix of the blob read plane.
	blobsTail = "blobs/"
)

// emptyLayerDigestHex is the canonical docker empty layer: the 32-byte gzip
// of an empty tar that `docker push` SKIPS uploading (the client knows the
// registry can synthesize it — it is the highest-frequency digest in the
// ecosystem). GET/HEAD answer the fixed bytes without touching the store,
// and the manifest validation chain treats it as always-present
// (docker-registry.md 2.3, "规格照抄" 4).
const emptyLayerDigestHex = "a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"

// emptyLayerBytes is the exact 32-byte artifact (hex below spells it; the
// test pins its sha256 to the constant above so a transcription typo can
// never ship).
var emptyLayerBytes = mustHex("1f8b080000096e8800ff621805a360148c5800080000ffff2eafb5ef00040000")

// emptyLayerAncillary carries the synthesized layer's other digests so the
// X-Checksum family is complete on the synthesized HEAD (computed at init
// from the same bytes, never hand-copied).
var emptyLayerAncillary = emptyLayerSums{
	sha1: sumSha1(emptyLayerBytes),
	md5:  sumMd5(emptyLayerBytes),
}

// serveBlob routes /v2/<name>/blobs/<digest> (the read plane; DELETE is the
// deliberate 405, DE-14).
func (h *Handler) serveBlob(w http.ResponseWriter, r *http.Request, ref nameRef, tail string) {
	digestParam, ok := strings.CutPrefix(tail, blobsTail)
	if !ok || digestParam == "" {
		writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported, "unknown blob route "+tail, nil)
		return
	}
	if r.Method == http.MethodDelete {
		// DE-14/D13d: single-blob DELETE is deliberately unsupported — the
		// physical reclamation path is GC only (ADR-0006's safety floor).
		w.Header().Set("Allow", "GET, HEAD")
		writeSpecError(w, http.StatusMethodNotAllowed, ErrCodeUnsupported,
			"blob deletion is not supported; unreferenced blobs are reclaimed by garbage collection", nil)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeSpecError(w, http.StatusMethodNotAllowed, ErrCodeUnsupported,
			"method "+r.Method+" is not supported on blobs", nil)
		return
	}

	hex, err := parseDigestParam(digestParam)
	if err != nil {
		writeSpecError(w, http.StatusBadRequest, ErrCodeDigestInvalid,
			fmt.Sprintf("digest %q is not a valid sha256 digest", digestParam),
			map[string]string{"digest": digestParam})
		return
	}

	// Empty-layer synthesis: the registry answers the canonical bytes without
	// a store round trip (the client never uploaded them).
	if hex == emptyLayerDigestHex {
		h.serveEmptyLayer(w, r)
		return
	}

	p := principalOf(r)
	path := blobNodePath(ref.image, hex)
	rc, node, err := h.svc.Get(r.Context(), p, ref.repoKey, path)
	if err != nil {
		h.writeBlobReadError(w, r, err, ref, hex)
		return
	}
	defer rc.Close() //nolint:errcheck // read-only fd

	blob := storage.BlobRef{Sha256: hex, Size: node.Size}
	if row := h.ledgerRow(r.Context(), hex); row != nil {
		if row.Sha1 != "" {
			blob.Sha1 = row.Sha1
		}
		if row.Md5 != "" {
			blob.Md5 = row.Md5
		}
	}
	h.serveBlobBody(w, r, rc, blob)
}

// serveBlobBody streams the opened blob with the docker read contract:
// Docker-Content-Digest, the checksum family, ETag (= sha1, matching the
// generic plane), Accept-Ranges and the M1 Range semantics (206 single
// slice / 416 unsatisfiable with "bytes */<total>", multi-range and other
// units ignored — FR-8-AC8 reuses the M1 conditional/range base).
func (h *Handler) serveBlobBody(w http.ResponseWriter, r *http.Request, rc io.ReadSeeker, blob storage.BlobRef) {
	hdr := w.Header()
	writeAPIVersionHdr(hdr)
	hdr.Set(hdrContentDigest, digestPrefixHex(blob.Sha256))
	if blob.Sha1 != "" {
		hdr.Set(hdrChecksumSha1, blob.Sha1)
		hdr.Set("ETag", blob.Sha1)
	}
	if blob.Md5 != "" {
		hdr.Set(hdrChecksumMd5, blob.Md5)
	}
	hdr.Set(hdrChecksumSha256, blob.Sha256)
	hdr.Set("Accept-Ranges", "bytes")
	hdr.Set("Content-Type", mimeOctetStream)

	parser := rangeParser{total: blob.Size}
	rng, malformed, ignore := parser.parseRange(r.Header.Get("Range"))
	if malformed {
		hdr.Set("Content-Range", "bytes */"+strconv.FormatInt(blob.Size, 10))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if !ignore && rng.length() > 0 {
		if _, err := rc.Seek(rng.start, io.SeekStart); err != nil {
			h.log.ErrorContext(r.Context(), "docker: seek blob for range", "error", err.Error())
			writeSpecError(w, http.StatusInternalServerError, ErrCodeUnknown, "seek blob for range: "+err.Error(), nil)
			return
		}
		hdr.Set("Content-Range", rng.contentRange(blob.Size))
		hdr.Set("Content-Length", strconv.FormatInt(rng.length(), 10))
		w.WriteHeader(http.StatusPartialContent)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.CopyN(w, rc, rng.length())
		return
	}
	hdr.Set("Content-Length", strconv.FormatInt(blob.Size, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, rc)
}

// serveEmptyLayer answers the synthesized 32-byte artifact. HEAD carries the
// full header family with Content-Length 32; GET streams the exact bytes.
func (h *Handler) serveEmptyLayer(w http.ResponseWriter, r *http.Request) {
	hdr := w.Header()
	writeAPIVersionHdr(hdr)
	hdr.Set(hdrContentDigest, digestPrefixHex(emptyLayerDigestHex))
	hdr.Set(hdrChecksumSha256, emptyLayerDigestHex)
	hdr.Set(hdrChecksumSha1, emptyLayerAncillary.sha1)
	hdr.Set(hdrChecksumMd5, emptyLayerAncillary.md5)
	hdr.Set("ETag", emptyLayerAncillary.sha1)
	hdr.Set("Accept-Ranges", "bytes")
	hdr.Set("Content-Type", mimeOctetStream)
	hdr.Set("Content-Length", strconv.Itoa(len(emptyLayerBytes)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(emptyLayerBytes)
}

// writeBlobReadError maps the service failures of the blob read path onto
// the spec error body.
func (h *Handler) writeBlobReadError(w http.ResponseWriter, r *http.Request, err error, ref nameRef, hex string) {
	switch {
	case errors.Is(err, repo.ErrNodeNotFound), errors.Is(err, repo.ErrIsFolder):
		writeSpecError(w, http.StatusNotFound, ErrCodeBlobUnknown,
			fmt.Sprintf("blob unknown to registry: %s", digestPrefixHex(hex)),
			map[string]string{"digest": digestPrefixHex(hex)})
	case errors.Is(err, repo.ErrUnauthorized):
		h.challenge(w, r, deriveChallengeScope(r.Method, ref))
	case errors.Is(err, repo.ErrForbidden):
		writeSpecError(w, http.StatusForbidden, ErrCodeDenied,
			"requested access to the resource is denied", nil)
	default:
		h.log.ErrorContext(r.Context(), "docker: blob read failed",
			"repo", ref.repoKey, "digest", hex, "error", err.Error())
		writeSpecError(w, http.StatusInternalServerError, ErrCodeUnknown,
			"blob read failed: "+err.Error(), nil)
	}
}

// ---- shared helpers of the blob domain ----

// parseDigestParam accepts "sha256:<64 lowercase hex>" (the spec's digest
// grammar; uppercase hex is tolerated and folded, mirroring the storage
// layer) and yields the bare hex. Every other algorithm or shape is an
// error the caller maps to DIGEST_INVALID (architecture 5.3 ruling 2: no
// algorithm conversion, M2 serves sha256 only).
func parseDigestParam(param string) (string, error) {
	algo, hexPart, found := strings.Cut(param, ":")
	if !found {
		return "", fmt.Errorf("digest %q carries no algorithm", param)
	}
	if algo != "sha256" {
		return "", fmt.Errorf("algorithm %q is not supported (sha256 only)", algo)
	}
	hexPart = strings.ToLower(hexPart)
	if len(hexPart) != 64 || !isAllHex(hexPart) {
		return "", fmt.Errorf("digest %q is not 64 hex characters", param)
	}
	return hexPart, nil
}

// isAllHex reports whether s is non-empty lowercase hex.
func isAllHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// parseUint64 parses a bare non-negative decimal (Content-Range anchors).
func parseUint64(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty range anchor")
	}
	var n int64
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("non-digit %q in range anchor", s[i])
		}
		if n > (1<<62)/10 {
			return 0, errors.New("range anchor overflow")
		}
		n = n*10 + int64(s[i]-'0')
	}
	return n, nil
}

// digestPrefixHex re-prefixes bare hex with the wire algorithm spelling.
func digestPrefixHex(hex string) string { return "sha256:" + hex }

// blobNodePath is the docker blob layout path (architecture section 6's
// node-layout convention): <image>/blobs/<hex>.
func blobNodePath(image, hex string) string { return image + "/blobs/" + hex }

// blobURL is the canonical blob read URL (relative, root-level).
func blobURL(repoKey, image, hex string) string {
	return "/v2/" + repoKey + "/" + image + "/blobs/" + digestPrefixHex(hex)
}

// uploadURL is the upload-session URL (relative, root-level).
func uploadURL(repoKey, image, id string) string {
	return "/v2/" + repoKey + "/" + image + "/blobs/uploads/" + id
}

// splitMountSource splits a mount's from parameter "<repoKey>/<image>" into
// its two halves. A bare single segment names only a repository key with no
// image (legal to address, nothing mountable — the caller degrades).
func splitMountSource(from string) (repoKey, image string) {
	key, rest, _ := strings.Cut(from, "/")
	return key, rest
}

// principalOf is the context principal (nil = anonymous).
func principalOf(r *http.Request) *Principal { return adapter.PrincipalFrom(r.Context()) }

// isDenied reports whether err is an authorization refusal.
func isDenied(err error) bool {
	return errors.Is(err, repo.ErrForbidden) || errors.Is(err, repo.ErrUnauthorized)
}

// isChecksumMismatch reports whether err is the storage layer's digest
// disagreement (Commit against an expected digest that did not match).
func isChecksumMismatch(err error) bool { return errors.Is(err, storage.ErrChecksumMismatch) }

// ledgerRow fetches the blob's ancillary digests from the metadata ledger
// (the sha1/md5 source of truth, ADR-0006); nil on any miss.
func (h *Handler) ledgerRow(ctx context.Context, hex string) *storage.BlobRef {
	if h.ledger == nil {
		return nil
	}
	row, err := h.ledger.Get(ctx, hex)
	if err != nil || row == nil {
		return nil
	}
	return &storage.BlobRef{
		Sha256: row.Sha256,
		Sha1:   row.Sha1,
		Md5:    row.Md5,
		Size:   row.Size,
	}
}

// blobPresent reports whether the source repository exposes the blob at the
// docker layout path for THIS principal (the mount reads it there, NFR-S12:
// a mount must not bypass the source's read ACL).
//
// The successful Get returns an OPEN reader over the blob (the real service
// hands back *os.File) — it must be closed here or every successful mount
// probe leaks one fd (review B3, both reviewers). A Close failure does not
// un-make the presence: the read-only fd's lifecycle, not the content, is
// what Close reports on.
func (h *Handler) blobPresent(ctx context.Context, p *Principal, repoKey, path string) bool {
	rc, _, err := h.svc.Get(ctx, p, repoKey, path)
	if err != nil {
		return false
	}
	_ = rc.Close() //nolint:errcheck // read-only fd; presence is already decided
	return true
}

// canMountFrom answers the source-read question of the mount: read on the
// source repo's image path. The route gate has already settled the
// destination; this is the NFR-S12 second question.
//
// The action MUST go through the scope->Can table (T-43 QA D1): passing the
// scope spelling ("pull") reaches Authorizer.Can, whose action domain is
// the single-letter r/w/d — rowAllows answers default:false to anything
// else, so every NON-admin mount silently degraded to a 202 upload grant
// (admins never noticed: Can short-circuits on p.Admin). canActions is the
// same table the route gate uses (handler.go), keeping the two read
// questions literally one question.
func (h *Handler) canMountFrom(ctx context.Context, p *Principal, repoKey, image string) bool {
	if h.authz == nil {
		return p == nil && h.opts.AnonymousAccess
	}
	return h.authz.Can(ctx, p, repoKey, image, canActions[scopeActionPull][0])
}

// emptyLayerSums carries the synthesized layer's ancillary digests.
type emptyLayerSums struct{ sha1, md5 string }
