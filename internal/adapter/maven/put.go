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
	// Checksum deploy (zero-transfer, T-73): detected here, SERVED after the
	// policy gates below — it is an artifact deploy like any other, only
	// without the bytes.
	checksumDeploy := false
	if v := r.Header.Get(hdrChecksumDeploy); v != "" && !strings.EqualFold(v, "false") {
		checksumDeploy = true
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
	// metadata PUTs acceptable). A checksum deploy of an artifact is bound
	// by the same gates: zero bytes is still a deploy of that version.
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

	if checksumDeploy {
		h.putChecksumDeploy(ctx, w, r, p, repoKey, relPath, l)
		return
	}

	if l.Kind == KindSidecar {
		h.putSidecar(ctx, w, r, p, repoKey, relPath, l, cfg)
		return
	}
	h.putFile(ctx, w, r, p, repoKey, relPath, l, cfg)
}

// putChecksumDeploy implements X-Checksum-Deploy on the maven plane (T-73,
// PRD §6.4-1): a zero-transfer artifact deploy addressed by sha256 — or by
// sha1 alone, the maven ecosystem's dominant algorithm (the ledger's sha1
// index resolves it inside the service). The header shapes and the miss
// rendering follow the generic plane's M1 C15 family: no checksum header →
// 400, malformed value → 404, miss → 404 "no content found". Artifacts only:
// sidecars are registration data with their own chain, metadata documents
// are regenerated bookkeeping — checksum-deploying either is refused rather
// than landing a node those chains would never have written.
func (h *Handler) putChecksumDeploy(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, relPath string, l Layout) {
	if l.Kind != KindArtifact {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("X-Checksum-Deploy supports artifact paths only on maven repositories; %q is a %s path",
				relPath, l.Kind))
		return
	}
	sha256 := strings.ToLower(strings.TrimSpace(r.Header.Get(hdrChecksumSha256)))
	sha1v := strings.ToLower(strings.TrimSpace(r.Header.Get(hdrChecksumSha1)))
	md5v := strings.ToLower(strings.TrimSpace(r.Header.Get(hdrChecksumMd5)))
	if sha256 == "" && sha1v == "" {
		writeError(w, http.StatusBadRequest,
			"Checksum deploy failed. no checksum header 'X-Checksum-Sha1/X-Checksum-Sha256' was found.")
		return
	}
	if sha256 != "" && !isHex(sha256, 64) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Checksum deploy failed: malformed sha256 value %q.", sha256))
		return
	}
	if sha1v != "" && !isHex(sha1v, 40) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Checksum deploy failed: malformed sha1 value %q.", sha1v))
		return
	}
	if md5v != "" && !isHex(md5v, 32) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Checksum deploy failed: malformed md5 value %q.", md5v))
		return
	}

	mime := r.Header.Get("Content-Type")
	if mime == "" {
		mime = mimeForPath(relPath, "")
	}
	ref := storage.BlobRef{Sha256: sha256, Sha1: sha1v, Md5: md5v}
	node, err := h.svc.PutFromBlob(ctx, p, repoKey, relPath, ref, mime)
	if err != nil {
		if errors.Is(err, repo.ErrOrphanBlob) || errors.Is(err, repo.ErrNodeNotFound) {
			writeError(w, http.StatusNotFound, "Checksum deploy failed: no content found for the given checksum.")
			return
		}
		h.writeServiceError(w, err, http.MethodPut, repoKey, relPath)
		return
	}
	// A landed artifact feeds FR-17's metadata calculator exactly like a
	// byte-carrying deploy (the facts and the metadata belong to where the
	// bytes — here the referenced blob — live).
	h.calc.afterArtifactDeploy(ctx, p, node.RepoKey, l)
	h.writeCreated(w, r, repoKey, relPath, node, declaredSetOf(ref))
}

// putFile lands an artifact or a client maven-metadata.xml document
// (ME-06: metadata PUTs ride the generic upload chain and are accepted).
func (h *Handler) putFile(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, relPath string, l Layout, cfg RepoConfig) {
	// L014-2 BUG 1: under snapshotVersionBehavior=unique a -SNAPSHOT file
	// name is rewritten to the timestamped spelling BEFORE the bytes land
	// (the 201 Location, the storage node and the calculator trigger all
	// address the rewritten name; the version directory keeps -SNAPSHOT).
	if l.Kind == KindArtifact && cfg.SnapshotBehavior == BehaviorUnique && l.Snapshot && !l.Timestamped {
		if name := h.calc.adjustUniqueSnapshot(ctx, p, repoKey, l); name != l.File {
			relPath = relPath[:strings.LastIndexByte(relPath, '/')+1] + name
			l.File, l.Timestamped = name, true
		}
	}
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

	// The metadata family never triggers the overwrite check
	// (repo-semantics section 3, high confidence): sidecar and metadata
	// documents are freely rewritable — every mvn deploy re-PUTs them with
	// changed bytes, which must not demand DELETE on the old node (the
	// T-67 leftover this closes). The write grant still applies.
	//
	// Deploy matrix properties (T-286) ride the same options either way:
	// the ";k=v" set ServeHTTP boxed off the path lands with the node.
	opts := repo.PutOptions{Properties: deployPropsOf(ctx)}
	if l.Kind == KindMetadata {
		opts.SkipOverwriteCheck = true
	}
	node, err := h.svc.PutWithOptions(ctx, p, repoKey, relPath, body, declared, mime, opts)
	if err != nil {
		h.writeServiceError(w, err, http.MethodPut, repoKey, relPath)
		return
	}

	// FR-17's trigger taxonomy fires on the LANDED node's repository (a
	// routed virtual write lands in the deployment member — the facts and
	// the metadata belong to where the bytes are).
	if l.Kind == KindArtifact {
		h.calc.afterArtifactDeploy(ctx, p, node.RepoKey, l)
	} else {
		h.calc.afterMetadataDeploy(ctx, p, node.RepoKey, l)
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
// 404 and the two-policy comparison all precede the registration. L014-2
// BUG 2: a checksum-file PUT is REGISTRATION ONLY — no sidecar storage
// item materializes (the reference's post-deploy listing shows pom, jar
// and maven-metadata.xml only; BinFlow's phantom .sha1/.md5 nodes were the
// E2 divergence). The 201 carries Location = the TARGET artifact and no
// body; GET of the sidecar path answers the server-computed digest, as it
// always did.
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

	// Under unique behavior the target of a -SNAPSHOT sidecar is the
	// REWRITTEN artifact name (the checksum registers against the file
	// that actually landed, spec section 1.3's companion-follows rule).
	if cfg.SnapshotBehavior == BehaviorUnique && l.TargetKind == KindArtifact &&
		l.Snapshot && !l.Timestamped {
		if tl, terr := Parse(l.Target); terr == nil {
			if name := h.calc.adjustCompanionTarget(ctx, p, repoKey, tl); name != tl.File {
				l.Target = l.Target[:strings.LastIndexByte(l.Target, '/')+1] + name
			}
		}
	}

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

	// The policy comparison stays the artifact-plane gate: a disagreeing
	// claim on a client-checksums repository is a 409. The metadata family
	// keeps its tolerance — its target is server-authoritative and
	// regenerated at any deploy (FR-17), so a client value checksummed
	// against pre-recalculation bytes may legitimately disagree already.
	if measured, ok := h.digestOf(ctx, node, l.Algo); ok && measured != declared {
		if cfg.ChecksumPolicy == ChecksumPolicyClient && l.TargetKind != KindMetadata {
			writeError(w, http.StatusConflict, fmt.Sprintf(
				"Checksum error for '%s/%s': received '%s' but actual is '%s'",
				repoKey, relPath, declared, measured))
			return
		}
	}

	// Registration only (L014-2 BUG 2): the 201 carries the TARGET's
	// Location and no body (rest-api.md section 1.1's dedicated column for
	// the checksum-file PUT).
	w.Header().Set("Location", requestBase(r)+"/"+repoKey+"/"+escapePath(l.Target))
	w.WriteHeader(http.StatusCreated)
}

// maxSidecarBytes is the checksum-file size ceiling (rest-api.md 1.5).
const maxSidecarBytes = 1024

// deployPropsOf lifts the request's matrix-parameter set into the option
// shape (nil for a path without any — the plain-deploy options).
func deployPropsOf(ctx context.Context) map[string][]string {
	if props := adapter.DeployPropsFrom(ctx); len(props) > 0 {
		return map[string][]string(props)
	}
	return nil
}

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
