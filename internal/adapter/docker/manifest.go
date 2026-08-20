package docker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The manifest domain (FR-9/DE-08/DE-09/DE-10): PUT with the structural
// validation chain and the reference-integrity gate, GET/HEAD by tag and by
// digest with Accept negotiation, DELETE by digest only.

// manifestMaxBytes is the manifest body ceiling (PRD section 6.5 ruling ③:
// 4MB, enough for even a very large index; anything bigger is abuse, not a
// manifest). A body over the limit answers 400 MANIFEST_INVALID.
const manifestMaxBytes = 4 << 20

// manifestsTail is the route tail prefix of the manifest plane.
const manifestsTail = "manifests/"

// digestPrefix is the wire spelling of the only algorithm M2 serves
// (architecture section 5.3 ruling 2).
const digestPrefix = "sha256:"

// Media types the structural parser recognizes by name. The list drives
// PARSING only — the stored Content-Type is the client's own header value
// verbatim (PRD FR-9: pass-through, no whitelist — Helm OCI configs and
// future artifact types depend on that; architecture section 5.3's
// four-type whitelist was superseded, see T-32 risk R3).
const (
	mediaTypeDockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
	mediaTypeDockerList     = "application/vnd.docker.distribution.manifest.list.v2+json"
	mediaTypeOCIManifest    = "application/vnd.oci.image.manifest.v1+json"
	mediaTypeOCIIndex       = "application/vnd.oci.image.index.v1+json"
	// mediaTypeDockerSchema1 and ...Signed are the legacy signed manifests:
	// refused outright (interim ruling — M2 does not support schema1).
	mediaTypeDockerSchema1       = "application/vnd.docker.distribution.manifest.v1+json"
	mediaTypeDockerSchema1Signed = "application/vnd.docker.distribution.manifest.v1+prettyjws"
)

// manifestRef is one descriptor inside a manifest: config, layer or index
// child. Only digest and mediaType are load-bearing in M2 (sizes are
// advisory; every other field survives verbatim in the stored body — the
// adapter never re-serializes a manifest).
type manifestRef struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
}

// imageManifest is the schema2/OCI single-image shape (one JSON vocabulary
// covers both families; the docker and OCI fields overlap).
type imageManifest struct {
	SchemaVersion int           `json:"schemaVersion"`
	MediaType     string        `json:"mediaType"`
	Config        *manifestRef  `json:"config"`
	Layers        []manifestRef `json:"layers"`
	Subject       *manifestRef  `json:"subject"`
}

// indexManifest is the manifest-list / OCI-index shape.
type indexManifest struct {
	SchemaVersion int           `json:"schemaVersion"`
	MediaType     string        `json:"mediaType"`
	Manifests     []manifestRef `json:"manifests"`
	Subject       *manifestRef  `json:"subject"`
}

// parsedManifest is the structural parse outcome: the descriptors the
// reference gate must verify, and the OCI 1.1 subject digest when present.
type parsedManifest struct {
	refs    []manifestRef
	subject string // digest of the OCI subject (wire form), "" when absent
}

// serveManifest routes /v2/<name>/manifests/<ref> after the repo and
// authorization gates have passed (T-37: the gates sit ahead of every
// content handler, so the verb bodies only implement protocol). tail is
// "manifests/<ref>" already percent-decoded (parseV2Name decodes the whole
// path); the ref is judged as digest-or-tag, never spliced into a path.
func (h *Handler) serveManifest(w http.ResponseWriter, r *http.Request, ref nameRef, tail string) {
	reference, ok := strings.CutPrefix(tail, manifestsTail)
	if !ok || reference == "" {
		writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported, "unknown manifest route "+tail, nil)
		return
	}
	switch r.Method {
	case http.MethodPut:
		h.putManifest(w, r, ref, reference)
	case http.MethodGet, http.MethodHead:
		h.getManifest(w, r, ref, reference)
	case http.MethodDelete:
		h.deleteManifest(w, r, ref, reference)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT, DELETE")
		writeSpecError(w, http.StatusMethodNotAllowed, ErrCodeUnsupported,
			"method "+r.Method+" is not supported on manifests", nil)
	}
}

// putManifest implements DE-08. Chain order (every failure answers before
// any state changes):
//
//  1. reference shape — a "sha256:" ref must be a valid digest, anything
//     else a legal tag (FR-9-AC4's mismatch is judged later, against the
//     body);
//  2. bounded body read (4MB, section 6.5 ruling ③);
//  3. structural parse keyed on Content-Type (schema1 refused; unknown
//     types judged by body shape — pass-through family);
//  4. by-digest reference vs computed sha256 → DIGEST_INVALID;
//  5. reference integrity — every config/layer (image) or child manifest
//     (index, lazy: presence only) digest must be served by THIS repository
//     at its layout path, the canonical empty layer exempt (T-38) →
//     MANIFEST_BLOB_UNKNOWN;
//  6. land: svc.Put commits the body as a blob and writes the
//     node/ledger rows (mime = the manifest's Content-Type, FR-7-AC4),
//     svc.PutManifest writes the index row, the tag pointer and the ref
//     ledger;
//  7. 201 + Location + Docker-Content-Digest (+ OCI-Subject, docker-
//     registry.md section 3 step 10).
func (h *Handler) putManifest(w http.ResponseWriter, r *http.Request, ref nameRef, reference string) {
	isDigestRef := strings.HasPrefix(reference, digestPrefix)
	var wantHex string
	if isDigestRef {
		hexPart, err := parseDigestParam(reference)
		if err != nil {
			writeSpecError(w, http.StatusBadRequest, ErrCodeDigestInvalid,
				fmt.Sprintf("manifest reference %q is not a valid sha256 digest", reference),
				map[string]string{"digest": reference})
			return
		}
		wantHex = hexPart
	} else if err := validateManifestTag(reference); err != nil {
		writeSpecError(w, http.StatusBadRequest, ErrCodeManifestInvalid,
			fmt.Sprintf("manifest reference %q is neither a digest nor a legal tag: %v", reference, err), nil)
		return
	}

	// Bounded read: the limit covers chunked bodies too (MaxBytesReader
	// counts, it does not trust Content-Length).
	payload, err := readManifestBody(w, r)
	if err != nil {
		if errors.Is(err, errManifestTooLarge) {
			writeSpecError(w, http.StatusBadRequest, ErrCodeManifestInvalid,
				fmt.Sprintf("manifest body exceeds the %d byte limit", manifestMaxBytes), nil)
			return
		}
		h.log.ErrorContext(r.Context(), "docker: read manifest body", "error", err.Error())
		writeSpecError(w, http.StatusInternalServerError, ErrCodeUnknown,
			"read manifest body: "+err.Error(), nil)
		return
	}

	// Structural parse: the request Content-Type selects the schema family
	// (docker-registry.md section 3 step 5) and is stored verbatim.
	contentType := mediaTypeOfHeader(r.Header.Get("Content-Type"))
	parsed, err := parseManifest(contentType, payload)
	if err != nil {
		writeSpecError(w, http.StatusBadRequest, ErrCodeManifestInvalid, err.Error(), nil)
		return
	}

	// The manifest's identity is the sha256 of its exact bytes.
	dgst := sha256HexOf(payload)
	if wantHex != "" && wantHex != dgst {
		writeSpecError(w, http.StatusBadRequest, ErrCodeDigestInvalid,
			fmt.Sprintf("manifest reference %s does not match the body digest %s", reference, digestPrefix+dgst),
			map[string]string{"digest": reference})
		return
	}

	// Reference integrity (architecture section 5.3 validation chain ②/③).
	p := principalOf(r)
	for _, mr := range parsed.refs {
		hexPart, derr := parseDigestParam(mr.Digest)
		if derr != nil {
			writeSpecError(w, http.StatusBadRequest, ErrCodeManifestInvalid,
				fmt.Sprintf("manifest descriptor digest %q is not a valid sha256 digest", mr.Digest), nil)
			return
		}
		if hexPart == emptyLayerDigestHex {
			continue // synthesized by the registry, never uploaded (T-38)
		}
		present, perr := h.childPresent(r.Context(), p, ref, mr, hexPart)
		if perr != nil {
			h.writeManifestReadError(w, r, perr, ref, mr.Digest)
			return
		}
		if !present {
			writeSpecError(w, http.StatusBadRequest, ErrCodeManifestBlobUnknown,
				fmt.Sprintf("manifest blob unknown to registry: %s", mr.Digest),
				map[string]string{"digest": mr.Digest})
			return
		}
	}

	// Land the manifest in the two steps the service contract prescribes
	// (PutManifest doc: "the adapter uploads the body through the normal
	// blob path first").
	//
	// Step 1 — the body rides the normal blob plane: svc.Put streams it
	// into the service's own session (Commit dedups a same-digest
	// republish; the ledger row keeps the real sha1/md5 of the manifest
	// bytes) and writes the node at <image>/blobs/<hex>. Manifests ARE
	// blobs on the wire — the spec serves them at /v2/<name>/blobs/<digest>
	// too, and the blob node is the reference-integrity anchor a later
	// index push probes.
	if _, err := h.svc.Put(r.Context(), p, ref.repoKey, blobNodePath(ref.image, dgst),
		bytes.NewReader(payload), storage.BlobRef{Sha256: dgst}, mimeOctetStream); err != nil {
		h.writeManifestPutError(w, r, err, ref, dgst)
		return
	}

	// Step 2 — the docker use case: the node at <image>/manifests/<hex>
	// (mime = the manifest's Content-Type, FR-7-AC4), the manifests index
	// row, the tag pointer ("" = digest-only push; Q4: a same-tag re-push
	// REPOINTS) and the ref ledger. The two node paths are distinct, so
	// PutManifest's idempotent probe (keyed on the MANIFEST path) only
	// fires on a genuine same-digest manifest republish.
	tag := ""
	if !isDigestRef {
		tag = reference
	}
	// The ref ledger is one row per REFERENCED BLOB (the DDL's own
	// comment), and docker_refs keys it (repo, image, manifest, blob) — so
	// duplicate descriptors inside one manifest (the same layer twice,
	// config==layer, an index naming one child twice, even the empty layer
	// listed twice) collapse onto a single row. Deduping HERE keeps
	// PutRefs's INSERT set duplicate-free; the store's insert is
	// conflict-tolerant too (defense in depth, review B1) so a second
	// writer racing the same conclusion cannot 500 a legal push.
	refs := make([]*metadata.DockerRef, 0, len(parsed.refs))
	seen := make(map[string]struct{}, len(parsed.refs))
	for _, mr := range parsed.refs {
		hexPart, _ := parseDigestParam(mr.Digest)
		if _, dup := seen[hexPart]; dup {
			continue
		}
		seen[hexPart] = struct{}{}
		refs = append(refs, &metadata.DockerRef{BlobDigest: hexPart, ChildMediaType: mr.MediaType})
	}
	if _, err := h.svc.PutManifest(r.Context(), p, ref.repoKey, ref.image, dgst, tag,
		contentType, int64(len(payload)), refs); err != nil {
		h.writeManifestPutError(w, r, err, ref, dgst)
		return
	}

	hdr := w.Header()
	writeAPIVersionHdr(hdr)
	hdr.Set("Location", manifestURL(ref.repoKey, ref.image, dgst))
	hdr.Set(hdrContentDigest, digestPrefix+dgst)
	hdr.Set("Content-Length", "0")
	if parsed.subject != "" {
		hdr.Set("OCI-Subject", parsed.subject)
	}
	w.WriteHeader(http.StatusCreated)
}

// getManifest implements DE-09: by-tag and by-digest resolution, the exact
// stored body (bit-for-bit — a re-serialization would move the digest),
// Accept negotiation without schema conversion (docker-registry.md section
// 4 calibration: an Accept set that excludes the stored type is
// MANIFEST_UNKNOWN, never a schema2->1 downcast), Docker-Content-Digest
// and the stored Content-Type. HEAD carries the headers only.
func (h *Handler) getManifest(w http.ResponseWriter, r *http.Request, ref nameRef, reference string) {
	dgst, mediaType, size, err := h.resolveManifestRef(r, ref, reference)
	if err != nil {
		h.writeManifestReadError(w, r, err, ref, reference)
		return
	}
	if !acceptAllows(r.Header.Values("Accept"), mediaType) {
		writeSpecError(w, http.StatusNotFound, ErrCodeManifestUnknown,
			fmt.Sprintf("manifest %s is not available in an accepted media type (stored: %s)", reference, mediaType),
			map[string]string{"mediaType": mediaType})
		return
	}
	rc, _, err := h.svc.Get(r.Context(), principalOf(r), ref.repoKey, manifestNodePath(ref.image, dgst))
	if err != nil {
		h.writeManifestReadError(w, r, err, ref, reference)
		return
	}
	defer rc.Close() //nolint:errcheck // read-only fd

	hdr := w.Header()
	writeAPIVersionHdr(hdr)
	hdr.Set(hdrContentDigest, digestPrefix+dgst)
	hdr.Set("Content-Type", mediaType)
	hdr.Set("Content-Length", fmt.Sprint(size))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, rc)
}

// resolveManifestRef maps the reference onto the manifest's identity and
// serving metadata. A digest reference resolves directly; anything else is
// a tag whose CURRENT pointer decides (FR-9-AC2: after a tag moves, the
// old manifest stays reachable by its digest). The manifest ROW is the
// serving truth — Content-Type and size come from it, never the caller.
func (h *Handler) resolveManifestRef(r *http.Request, ref nameRef, reference string) (dgst, mediaType string, size int64, err error) {
	p := principalOf(r)
	if strings.HasPrefix(reference, digestPrefix) {
		hexPart, perr := parseDigestParam(reference)
		if perr != nil {
			return "", "", 0, fmt.Errorf("manifest reference %q: %w", reference, repo.ErrInvalidDigest)
		}
		m, merr := h.svc.ResolveManifest(r.Context(), p, ref.repoKey, ref.image, hexPart)
		if merr != nil {
			return "", "", 0, merr
		}
		return hexPart, m.MediaType, m.Size, nil
	}
	if terr := validateManifestTag(reference); terr != nil {
		return "", "", 0, fmt.Errorf("manifest reference %q: %w", reference, repo.ErrInvalidTag)
	}
	t, terr := h.svc.ResolveTag(r.Context(), p, ref.repoKey, ref.image, reference)
	if terr != nil {
		return "", "", 0, terr
	}
	if t.Digest == "" {
		// The tag row outlived its manifest (crash residue the cascade
		// normally prevents): the tag addresses nothing.
		return "", "", 0, fmt.Errorf("tag %s: %w", reference, repo.ErrManifestNotFound)
	}
	m, merr := h.svc.ResolveManifest(r.Context(), p, ref.repoKey, ref.image, t.Digest)
	if merr != nil {
		return "", "", 0, merr
	}
	return t.Digest, m.MediaType, m.Size, nil
}

// deleteManifest implements DE-10: by-digest only. A tag reference answers
// the deliberate 405 UNSUPPORTED (official spec; PRD v1.1 ruling R2) and
// the tag itself is untouched — deleting "by tag" is expressed as deleting
// the pointed-at manifest by digest, whose cascade clears the tag.
func (h *Handler) deleteManifest(w http.ResponseWriter, r *http.Request, ref nameRef, reference string) {
	if !strings.HasPrefix(reference, digestPrefix) {
		w.Header().Set("Allow", "GET, HEAD, PUT, DELETE")
		writeSpecError(w, http.StatusMethodNotAllowed, ErrCodeUnsupported,
			"manifest deletion by tag is not supported; delete by digest — the tag disappears with the cascade", nil)
		return
	}
	hexPart, err := parseDigestParam(reference)
	if err != nil {
		writeSpecError(w, http.StatusBadRequest, ErrCodeDigestInvalid,
			fmt.Sprintf("manifest reference %q is not a valid sha256 digest", reference),
			map[string]string{"digest": reference})
		return
	}
	// svc.DeleteManifest drops the node plus the index row, and the store
	// cascades tags/refs in the same transaction (FR-9-AC6). Referenced
	// blobs are never touched — GC owns the bytes.
	if derr := h.svc.DeleteManifest(r.Context(), principalOf(r), ref.repoKey, ref.image, hexPart); derr != nil {
		h.writeManifestDeleteError(w, r, derr, ref, reference)
		return
	}
	hdr := w.Header()
	writeAPIVersionHdr(hdr)
	hdr.Set("Content-Length", "0")
	w.WriteHeader(http.StatusAccepted)
}

// childPresent reports whether one referenced descriptor is served by this
// repository at its layout path. Index children (descriptor media types
// that name manifest/index families) live under manifests/; config and
// layers live under blobs/. The lazy rule (architecture validation chain
// ③): presence only, the child is never re-parsed. A probe failure that is
// not a plain miss (permission, store fault) propagates so the caller
// renders the honest status instead of a bogus MANIFEST_BLOB_UNKNOWN.
func (h *Handler) childPresent(ctx context.Context, p *Principal, ref nameRef, mr manifestRef, hexPart string) (bool, error) {
	path := blobNodePath(ref.image, hexPart)
	if isManifestFamilyMediaType(mr.MediaType) {
		path = manifestNodePath(ref.image, hexPart)
	}
	rc, _, err := h.svc.Get(ctx, p, ref.repoKey, path)
	switch {
	case err == nil:
		_ = rc.Close() //nolint:errcheck // read-only fd; presence already decided
		return true, nil
	case errors.Is(err, repo.ErrNodeNotFound), errors.Is(err, repo.ErrIsFolder):
		return false, nil
	default:
		return false, err
	}
}

// isManifestFamilyMediaType reports whether a descriptor's media type names
// a manifest (an index child) rather than a blob: the digest spelling alone
// cannot decide which layout path holds it.
func isManifestFamilyMediaType(mediaType string) bool {
	switch mediaType {
	case mediaTypeDockerManifest, mediaTypeOCIManifest, mediaTypeDockerList, mediaTypeOCIIndex:
		return true
	}
	return false
}

// ---- error mapping ----

// writeManifestPutError maps the service failures of the publish path.
//
// A *repo.StatusError renders VERBATIM first (T-111, the four-adapter
// seam): the governance verdicts that live inside svc.Put (step 1 of the
// two-step landing) and svc.PutManifest (step 2) — quota 413, pattern 409 —
// plus the virtual/remote write 405s reach the wire with their own status
// instead of the 500 UNKNOWN this mapper answered before. Auth-class
// refusals never arrive as a StatusError on this plane (they are plain
// sentinels, and the arms below keep the token challenge), so placing the
// verbatim arm ahead of the switch is safe.
func (h *Handler) writeManifestPutError(w http.ResponseWriter, r *http.Request, err error, ref nameRef, dgst string) {
	var se *repo.StatusError
	if errors.As(err, &se) {
		h.log.WarnContext(r.Context(), "docker: manifest publish refused",
			"repo", ref.repoKey, "digest", dgst, "status", se.Code, "error", se.Message)
		writeVerbatimStatusError(w, se)
		return
	}
	switch {
	case errors.Is(err, repo.ErrUnauthorized):
		h.challenge(w, r, deriveChallengeScope(r.Method, ref))
	case errors.Is(err, repo.ErrForbidden), isDenied(err):
		writeSpecError(w, http.StatusForbidden, ErrCodeDenied,
			"requested access to the resource is denied", nil)
	case errors.Is(err, repo.ErrInvalidManifest), errors.Is(err, repo.ErrInvalidImage),
		errors.Is(err, repo.ErrInvalidTag):
		writeSpecError(w, http.StatusBadRequest, ErrCodeManifestInvalid, err.Error(), nil)
	case errors.Is(err, repo.ErrInvalidDigest):
		writeSpecError(w, http.StatusBadRequest, ErrCodeDigestInvalid, err.Error(), nil)
	case errors.Is(err, storage.ErrEngineClosed), metadata.IsStoreBusy(err):
		// Engine shutdown and busy-class metadata contention (T-54) share the
		// retryable class: 503 UNAVAILABLE; the busy arm adds Retry-After.
		if metadata.IsStoreBusy(err) {
			w.Header().Set("Retry-After", "1")
			h.log.WarnContext(r.Context(), "docker: manifest publish busy (transient, retry)",
				"repo", ref.repoKey, "digest", dgst, "error", err.Error())
		}
		writeSpecError(w, http.StatusServiceUnavailable, ErrCodeUnavailable, err.Error(), nil)
	default:
		h.log.ErrorContext(r.Context(), "docker: manifest publish failed",
			"repo", ref.repoKey, "digest", dgst, "error", err.Error())
		writeSpecError(w, http.StatusInternalServerError, ErrCodeUnknown,
			"manifest publish failed: "+err.Error(), nil)
	}
}

// writeManifestReadError maps the resolution/read/delete-query failures of
// the read plane.
func (h *Handler) writeManifestReadError(w http.ResponseWriter, r *http.Request, err error, ref nameRef, reference string) {
	switch {
	case errors.Is(err, repo.ErrManifestNotFound), errors.Is(err, repo.ErrTagNotFound),
		errors.Is(err, repo.ErrNodeNotFound):
		writeSpecError(w, http.StatusNotFound, ErrCodeManifestUnknown,
			fmt.Sprintf("manifest unknown to registry: %s", reference),
			map[string]string{"reference": reference})
	case errors.Is(err, repo.ErrUnauthorized):
		h.challenge(w, r, deriveChallengeScope(r.Method, ref))
	case errors.Is(err, repo.ErrForbidden):
		writeSpecError(w, http.StatusForbidden, ErrCodeDenied,
			"requested access to the resource is denied", nil)
	case errors.Is(err, repo.ErrInvalidDigest):
		writeSpecError(w, http.StatusBadRequest, ErrCodeDigestInvalid, err.Error(), nil)
	case errors.Is(err, repo.ErrInvalidTag):
		writeSpecError(w, http.StatusBadRequest, ErrCodeManifestInvalid, err.Error(), nil)
	default:
		h.log.ErrorContext(r.Context(), "docker: manifest read failed",
			"repo", ref.repoKey, "reference", reference, "error", err.Error())
		writeSpecError(w, http.StatusInternalServerError, ErrCodeUnknown,
			"manifest read failed: "+err.Error(), nil)
	}
}

// writeManifestDeleteError maps the delete failures.
func (h *Handler) writeManifestDeleteError(w http.ResponseWriter, r *http.Request, err error, ref nameRef, reference string) {
	switch {
	case errors.Is(err, repo.ErrManifestNotFound):
		writeSpecError(w, http.StatusNotFound, ErrCodeManifestUnknown,
			fmt.Sprintf("manifest unknown to registry: %s", reference),
			map[string]string{"reference": reference})
	case errors.Is(err, repo.ErrUnauthorized):
		h.challenge(w, r, deriveChallengeScope(r.Method, ref))
	case errors.Is(err, repo.ErrForbidden):
		writeSpecError(w, http.StatusForbidden, ErrCodeDenied,
			"requested access to the resource is denied", nil)
	case errors.Is(err, repo.ErrInvalidDigest):
		writeSpecError(w, http.StatusBadRequest, ErrCodeDigestInvalid, err.Error(), nil)
	default:
		h.log.ErrorContext(r.Context(), "docker: manifest delete failed",
			"repo", ref.repoKey, "reference", reference, "error", err.Error())
		writeSpecError(w, http.StatusInternalServerError, ErrCodeUnknown,
			"manifest delete failed: "+err.Error(), nil)
	}
}

// ---- structural parsing ----

// readManifestBody drains the request body bounded by manifestMaxBytes.
// errManifestTooLarge is the dedicated sentinel for the limit (MaxBytesReader
// closes the connection after the overrun; the 400 still goes out on it).
var errManifestTooLarge = errors.New("manifest body exceeds the size limit")

func readManifestBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	bounded := http.MaxBytesReader(w, r.Body, manifestMaxBytes)
	payload, err := io.ReadAll(bounded)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return nil, errManifestTooLarge
		}
		return nil, err
	}
	return payload, nil
}

// mediaTypeOfHeader strips parameters ("; charset=utf-8") from a
// Content-Type header value.
func mediaTypeOfHeader(v string) string {
	return strings.TrimSpace(strings.SplitN(v, ";", 2)[0])
}

// parseManifest structurally validates one manifest body against its
// announced Content-Type and extracts the descriptors the reference gate
// verifies. Rules (interim ruling set):
//
//   - a schema1 Content-Type is refused (M2 does not serve schema1);
//   - a missing Content-Type is refused (the stored type IS the client's
//     value; there is nothing to serve without one);
//   - the four known manifest/index types parse through their dedicated
//     shapes (index: schemaVersion + manifests[]; image: schemaVersion +
//     config.digest + layers[].digest);
//   - every OTHER Content-Type is judged by the BODY's shape (pass-through
//     family: manifests[] present -> index semantics; config/layers
//     present -> image semantics; neither -> refuse — a body BinFlow
//     cannot reason about would leave the validation chain guessing).
//
// The body is never re-serialized; unknown fields ride along verbatim.
func parseManifest(contentType string, body []byte) (*parsedManifest, error) {
	if contentType == "" {
		return nil, errors.New("manifest Content-Type is required")
	}
	switch contentType {
	case mediaTypeDockerSchema1, mediaTypeDockerSchema1Signed:
		return nil, fmt.Errorf("manifest schema1 (%s) is not supported; push schema2 or OCI", contentType)
	}

	// Presence probe: pointers distinguish "field absent" from "field
	// present" (a JSON null collapses onto absent — both are unusable).
	var probe struct {
		SchemaVersion *int             `json:"schemaVersion"`
		Manifests     *json.RawMessage `json:"manifests"`
		Config        *json.RawMessage `json:"config"`
		Subject       *manifestRef     `json:"subject"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		// A schemaVersion of the wrong JSON type (2.5, "2") fails the
		// *int unmarshal before anything else; name it for what it is
		// rather than blaming the whole body (review non-blocking #8).
		var tse *json.UnmarshalTypeError
		if errors.As(err, &tse) && tse.Field == "schemaVersion" {
			return nil, fmt.Errorf("manifest schemaVersion is not an integer: %w", err)
		}
		return nil, fmt.Errorf("manifest is not valid JSON: %w", err)
	}

	out := &parsedManifest{}
	if probe.Subject != nil && probe.Subject.Digest != "" {
		// The OCI-Subject echo only carries well-formed digests (the reverse
		// spec's "subject parse failure is log-only" posture — a malformed
		// subject degrades to no header, never a 400).
		if _, err := parseDigestParam(probe.Subject.Digest); err == nil {
			out.subject = probe.Subject.Digest
		}
	}

	knownIndex := contentType == mediaTypeDockerList || contentType == mediaTypeOCIIndex
	knownImage := contentType == mediaTypeDockerManifest || contentType == mediaTypeOCIManifest
	if knownIndex || (!knownImage && probe.Manifests != nil) {
		return parseIndexBody(body)
	}
	if knownImage || probe.Config != nil {
		return parseImageBody(body)
	}
	return nil, fmt.Errorf(
		"manifest body carries neither an index manifests[] array nor an image config (Content-Type %s)", contentType)
}

// parseIndexBody validates an index/list body: schemaVersion present,
// manifests[] present, every child carrying a digest.
func parseIndexBody(body []byte) (*parsedManifest, error) {
	var idx indexManifest
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("index manifest does not parse: %w", err)
	}
	if idx.SchemaVersion == 0 && !hasSchemaVersion(body) {
		return nil, errors.New("index manifest carries no schemaVersion")
	}
	if idx.Manifests == nil {
		return nil, errors.New("index manifest carries no manifests field")
	}
	for i, child := range idx.Manifests {
		if child.Digest == "" {
			return nil, fmt.Errorf("index child %d carries no digest", i)
		}
	}
	out := &parsedManifest{refs: idx.Manifests}
	if idx.Subject != nil && idx.Subject.Digest != "" {
		if _, err := parseDigestParam(idx.Subject.Digest); err == nil {
			out.subject = idx.Subject.Digest
		}
	}
	return out, nil
}

// parseImageBody validates a schema2/OCI image body: schemaVersion present,
// a config descriptor with a digest, and layers that each carry a digest
// (an empty/absent layers array is a legal scratch image).
func parseImageBody(body []byte) (*parsedManifest, error) {
	var img imageManifest
	if err := json.Unmarshal(body, &img); err != nil {
		return nil, fmt.Errorf("image manifest does not parse: %w", err)
	}
	if img.SchemaVersion == 0 && !hasSchemaVersion(body) {
		return nil, errors.New("image manifest carries no schemaVersion")
	}
	if img.Config == nil {
		return nil, errors.New("image manifest carries no config descriptor")
	}
	if img.Config.Digest == "" {
		return nil, errors.New("image manifest config carries no digest")
	}
	for i, l := range img.Layers {
		if l.Digest == "" {
			return nil, fmt.Errorf("layer %d carries no digest", i)
		}
	}
	refs := make([]manifestRef, 0, 1+len(img.Layers))
	refs = append(refs, *img.Config)
	refs = append(refs, img.Layers...)
	out := &parsedManifest{refs: refs}
	if img.Subject != nil && img.Subject.Digest != "" {
		if _, err := parseDigestParam(img.Subject.Digest); err == nil {
			out.subject = img.Subject.Digest
		}
	}
	return out, nil
}

// hasSchemaVersion reports whether the raw body literally carries a
// schemaVersion member (distinguishing "schemaVersion":0 — legal, schema2
// documents that predate the field's meaningful values — from its absence).
func hasSchemaVersion(body []byte) bool {
	var probe struct {
		SchemaVersion *int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	return probe.SchemaVersion != nil
}

// validateManifestTag applies the tag charset the service layer enforces
// (mirrored at the edge so an illegal reference refuses before any body
// byte is read; [a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}).
func validateManifestTag(tag string) error {
	if tag == "" {
		return errors.New("tag is empty")
	}
	if len(tag) > 128 {
		return fmt.Errorf("tag is %d characters, the limit is 128", len(tag))
	}
	c := tag[0]
	if !isTagAlphaNum(c) && c != '_' {
		return fmt.Errorf("first character %q must be [a-zA-Z0-9_]", c)
	}
	for i := 1; i < len(tag); i++ {
		c := tag[i]
		if !isTagAlphaNum(c) && c != '_' && c != '.' && c != '-' {
			return fmt.Errorf("illegal character %q at offset %d", c, i)
		}
	}
	return nil
}

// isTagAlphaNum reports whether c is an ASCII letter or digit.
func isTagAlphaNum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// acceptAllows reports whether the client's Accept set admits the stored
// media type. Docker clients send several comma-joined values across
// multiple headers; a wildcard (*/* or type/*) admits its scope. An ABSENT
// Accept set accepts everything (the spec's default posture — curl and
// plain HTTP tooling rely on it); a set of nothing but malformed tokens is
// treated as no constraint rather than failing every pull.
//
// NOTE (review non-blocking #1): q weights do not participate — a value
// with `;q=0` counts as accepted after the parameter is stripped. No
// docker-family client sends q=0 for manifest types; revisit only if a
// conformance run ever demands it.
func acceptAllows(acceptHeaders []string, mediaType string) bool {
	if len(acceptHeaders) == 0 {
		return true
	}
	sawToken := false
	for _, hv := range acceptHeaders {
		for _, tok := range strings.Split(hv, ",") {
			tok = strings.TrimSpace(strings.SplitN(tok, ";", 2)[0])
			if tok == "" {
				continue
			}
			sawToken = true
			if tok == "*/*" || tok == mediaType {
				return true
			}
			if strings.HasSuffix(tok, "/*") && strings.HasPrefix(mediaType, tok[:len(tok)-1]) {
				return true
			}
		}
	}
	return !sawToken
}

// ---- layout helpers ----

// manifestNodePath is the manifest layout path (architecture section 6):
// <image>/manifests/<hex>. It mirrors repo's unexported
// dockerImageManifestPath; the spelling is duplicated deliberately
// (exporting repo's helper would widen the package seam for one string) —
// an httpapi test pins the two together.
func manifestNodePath(image, hexPart string) string { return image + "/manifests/" + hexPart }

// manifestURL is the canonical manifest read URL (relative, root-level).
func manifestURL(repoKey, image, hexPart string) string {
	return "/v2/" + repoKey + "/" + image + "/manifests/" + digestPrefix + hexPart
}

// sha256HexOf hashes one in-memory payload to bare lowercase hex (the
// manifest digest = sha256 of the exact body bytes).
func sha256HexOf(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
