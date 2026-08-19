package maven

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// handlePut implements the maven deploy chain (ME-01). Ordering follows
// repo-semantics section 2 (validate path/policy before permissions before
// body):
//
//  1. layout (already parsed in ServeHTTP),
//  2. repository resolution + class refusal (remote 405 / virtual 405 via
//     the service),
//  3. handleReleases/handleSnapshots policy (409, ME-08),
//  4. the checksum policy chain: X-Checksum-* headers and sidecar bodies
//     per checksumPolicyType (ME-09), server-computed acceptance when the
//     client declared nothing.
func (h *Handler) handlePut(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, relPath string, l Layout) {
	if v := r.Header.Get(hdrExplodeArchive); v != "" && !strings.EqualFold(v, "false") {
		writeError(w, http.StatusBadRequest, "X-Explode-Archive is not supported in BinFlow")
		return
	}
	// Checksum deploy (zero-transfer) is the generic plane's M1 feature;
	// the maven slice lands with T-73. Refuse it explicitly — silently
	// landing an empty node would corrupt the caller's accounting.
	if v := r.Header.Get(hdrChecksumDeploy); v != "" && !strings.EqualFold(v, "false") {
		writeError(w, http.StatusBadRequest,
			"X-Checksum-Deploy on maven repositories is not implemented in BinFlow yet")
		return
	}

	row, err := h.svc.GetRepo(ctx, p, repoKey)
	if err != nil {
		h.writeServiceError(w, err, http.MethodPut, repoKey, relPath)
		return
	}
	cfg := ParseRepoConfig(row.Config)

	// Release/snapshot handling gates (ME-08): they classify the DEPLOY's
	// version type from the version directory, and they bind artifact
	// uploads plus their checksum sidecars — the sidecar of a refused
	// artifact must not land. Metadata documents are bookkeeping, not a
	// deploy of a version: the gate does not apply (ME-06 keeps client
	// metadata PUTs acceptable).
	if l.Kind != KindMetadata {
		snapshotDeploy := l.Snapshot || l.Timestamped
		if snapshotDeploy && !cfg.AcceptsSnapshot() {
			writeError(w, http.StatusConflict, fmt.Sprintf(
				"Repository '%s' rejected deployment of '%s/%s': handling of snapshots is disabled (handleSnapshots=false).",
				repoKey, repoKey, relPath))
			return
		}
		if !snapshotDeploy && !cfg.AcceptsRelease() {
			writeError(w, http.StatusConflict, fmt.Sprintf(
				"Repository '%s' rejected deployment of '%s/%s': handling of releases is disabled (handleReleases=false).",
				repoKey, repoKey, relPath))
			return
		}
	}

	if l.Kind == KindSidecar {
		h.putSidecar(ctx, w, r, p, repoKey, relPath, l, cfg)
		return
	}
	h.putFile(ctx, w, r, p, repoKey, relPath, l, cfg)
}

// putFile lands an artifact or a client maven-metadata.xml document
// (ME-06: metadata PUTs ride the generic upload chain and are accepted).
func (h *Handler) putFile(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, relPath string, l Layout, cfg RepoConfig) {
	mime := r.Header.Get("Content-Type")
	if mime == "" {
		mime = mimeForPath(relPath, "")
	}

	declared, err := declaredDigests(r.Header)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// server-generated-checksums: the client's disagreeing claims are
	// accepted silently and the measured values are authoritative
	// (repo-semantics section 5) — dropping them here means storage
	// computes and accepts, and the response echoes the measured set.
	if cfg.ChecksumPolicy == ChecksumPolicyServerGenerated {
		declared = storage.BlobRef{}
	}

	var body io.Reader = r.Body
	// Client metadata re-PUTs are routine (every mvn deploy refreshes the
	// artifact-level document), and a metadata re-send with IDENTICAL
	// bytes must never trip anything: buffering the (small) body to pass
	// its own sha256 makes the service treat that case as the idempotent
	// retransmit its overwrite chain already exempts. Bodies beyond the
	// buffer ceiling stream unbuffered (no shortcut, still stored).
	if l.Kind == KindMetadata {
		buf, rerr := io.ReadAll(io.LimitReader(r.Body, maxMetadataBuffer+1))
		if rerr != nil {
			writeError(w, http.StatusBadRequest, "read metadata body: "+rerr.Error())
			return
		}
		if int64(len(buf)) > maxMetadataBuffer {
			body = io.MultiReader(bytes.NewReader(buf), r.Body)
		} else {
			body = bytes.NewReader(buf)
			if declared.Sha256 == "" && len(buf) > 0 {
				declared.Sha256 = sha256Hex(buf)
			}
		}
	}

	node, err := h.svc.Put(ctx, p, repoKey, relPath, body, declared, mime)
	if err != nil {
		h.writeServiceError(w, err, http.MethodPut, repoKey, relPath)
		return
	}
	declaredSet := map[string]bool{}
	if cfg.ChecksumPolicy != ChecksumPolicyServerGenerated {
		declaredSet = declaredSetOf(declared)
	}
	h.writeCreated(w, r, repoKey, relPath, node, declaredSet)
}

// putSidecar implements the checksum-file upload chain (rest-api.md
// section 1.5, high confidence): the sidecar body is the client's declared
// digest of the TARGET artifact; the >1024B guard, the target-must-exist
// 404 and the two-policy comparison all precede any storage write.
func (h *Handler) putSidecar(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, relPath string, l Layout, cfg RepoConfig) {
	// Suspicious-size guard first: a checksum file is a digest plus
	// whitespace; Content-Length beyond the ceiling answers without
	// reading the body, an oversized chunked body at the read.
	if r.ContentLength > maxSidecarBytes {
		writeError(w, http.StatusConflict, suspiciousSidecarMessage(r.ContentLength))
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxSidecarBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read checksum file: "+err.Error())
		return
	}
	if int64(len(raw)) > maxSidecarBytes {
		writeError(w, http.StatusConflict, suspiciousSidecarMessage(int64(len(raw))))
		return
	}
	declared := strings.TrimSpace(string(raw)) // trailing newline tolerated (FR-16)

	// The target must exist: a checksum for nothing registers nothing.
	rc, node, err := h.svc.Get(ctx, p, repoKey, l.Target)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) || errors.Is(err, repo.ErrIsFolder) {
			writeError(w, http.StatusNotFound,
				fmt.Sprintf("Could not locate artifact. Path: '%s/%s'.", repoKey, l.Target))
			return
		}
		h.writeServiceError(w, err, http.MethodPut, repoKey, relPath)
		return
	}
	_ = rc.Close() //nolint:errcheck // read-only fd; only the node metadata is needed

	// What lands: the server-measured digest under server-generated policy
	// (the client's claim never becomes stored bytes), the client's own
	// bytes otherwise.
	land := raw
	if measured, ok := h.digestOf(ctx, node, l.Algo); ok && measured != declared {
		if cfg.ChecksumPolicy == ChecksumPolicyClient {
			writeError(w, http.StatusConflict, fmt.Sprintf(
				"Checksum error for '%s/%s': received '%s' but actual is '%s'",
				repoKey, relPath, declared, measured))
			return
		}
		land = []byte(measured)
	}

	ref := storage.BlobRef{}
	if sum := sha256Hex(land); sum != "" {
		ref.Sha256 = sum
	}
	mavenNode, err := h.svc.Put(ctx, p, repoKey, relPath, bytes.NewReader(land), ref, sidecarContentType)
	if err != nil {
		h.writeServiceError(w, err, http.MethodPut, repoKey, relPath)
		return
	}
	_ = mavenNode // node freshness only; the sidecar 201 carries no body

	// Sidecar deploys answer 201 with Location and no body (rest-api.md
	// section 1.1's dedicated column for the checksum-file PUT).
	w.Header().Set("Location", requestBase(r)+"/"+repoKey+"/"+escapePath(relPath))
	w.WriteHeader(http.StatusCreated)
}

// maxSidecarBytes is the checksum-file size ceiling (rest-api.md 1.5).
const maxSidecarBytes = 1024

// suspiciousSidecarMessage renders the fixed refusal wording.
func suspiciousSidecarMessage(n int64) string {
	return fmt.Sprintf("Suspicious checksum file, content length of %d bytes is bigger than allowed.", n)
}

// declaredDigests parses the X-Checksum-* headers into a BlobRef
// (malformed values are 400-shaped, the shared adapter contract).
func declaredDigests(hdr http.Header) (storage.BlobRef, error) {
	parse := func(name string, width int) (string, error) {
		v := strings.ToLower(strings.TrimSpace(hdr.Get(name)))
		if v == "" {
			return "", nil
		}
		if !isHex(v, width) {
			return "", fmt.Errorf("%w: %s must be exactly %d hex characters, got %q",
				adapter.ErrInvalidChecksum, name, width, v)
		}
		return v, nil
	}
	sha256, err := parse(hdrChecksumSha256, 64)
	if err != nil {
		return storage.BlobRef{}, err
	}
	sha1, err := parse(hdrChecksumSha1, 40)
	if err != nil {
		return storage.BlobRef{}, err
	}
	md5, err := parse(hdrChecksumMd5, 32)
	if err != nil {
		return storage.BlobRef{}, err
	}
	return storage.BlobRef{Sha256: sha256, Sha1: sha1, Md5: md5}, nil
}

// declaredSetOf remembers which algorithms the client declared.
func declaredSetOf(expect storage.BlobRef) map[string]bool {
	m := map[string]bool{}
	if expect.Sha256 != "" {
		m["sha256"] = true
	}
	if expect.Sha1 != "" {
		m["sha1"] = true
	}
	if expect.Md5 != "" {
		m["md5"] = true
	}
	return m
}

// isHex reports whether s is exactly n hex characters.
func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// requestBase is scheme://host from the request (Location header base).
func requestBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// escapePath percent-encodes the path for the Location header.
func escapePath(rel string) string {
	segs := strings.Split(rel, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// writeCreated renders the 201 of an artifact/metadata deploy: Location,
// X-Checksum-Sha256 and the FileInfo ItemCreated body (rest-api.md 1.2).
func (h *Handler) writeCreated(w http.ResponseWriter, r *http.Request, repoKey, relPath string,
	node *metadata.Node, declared map[string]bool) {
	sums := h.digestTriple(r.Context(), node)
	w.Header().Set("Location", requestBase(r)+"/"+repoKey+"/"+escapePath(relPath))
	if sums.sha256 != "" && !isFolder(node) {
		w.Header().Set(hdrChecksumSha256, sums.sha256)
	}
	w.Header().Set("Content-Type", contentTypeItemCreated)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, h.itemInfo(requestBase(r), repoKey, relPath, node, sums, declared))
}

// isFolder reports a folder-marker node (no checksums exist for one).
func isFolder(n *metadata.Node) bool {
	return n != nil && strings.HasSuffix(n.Path, "/")
}

// itemInfo renders the FileInfo shape (rest-api.md 1.2): size is a string,
// checksums the measured triple, originalChecksums the client-declared
// subset of the upload.
func (h *Handler) itemInfo(base, repoKey, relPath string, node *metadata.Node,
	sums digestTriple, declared map[string]bool) fileInfo {
	info := fileInfo{
		URI:         base + "/" + repoKey + "/" + escapePath(relPath),
		DownloadURI: base + "/" + repoKey + "/" + escapePath(relPath),
		Repo:        repoKey,
		Path:        "/" + relPath,
		Created:     node.CreatedAt,
		CreatedBy:   node.CreatedBy,
		Size:        strconv.FormatInt(node.Size, 10),
		MimeType:    mimeForPath(relPath, node.Mime),
	}
	if !isFolder(node) {
		info.Checksums = &checksums{Sha1: sums.sha1, Md5: sums.md5, Sha256: sums.sha256}
		orig := &checksums{}
		if declared["sha1"] {
			orig.Sha1 = sums.sha1
		}
		if declared["md5"] {
			orig.Md5 = sums.md5
		}
		if declared["sha256"] {
			orig.Sha256 = sums.sha256
		}
		info.OriginalChecksums = orig
	}
	if node.UpdatedAt != "" && node.UpdatedAt != node.CreatedAt {
		info.LastModified = node.UpdatedAt
		info.LastUpdated = node.UpdatedAt
		info.ModifiedBy = node.CreatedBy
	}
	return info
}
