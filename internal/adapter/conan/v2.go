package conan

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

	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The v2 revision-aware plane (spec section 3.1's table — all seventeen
// endpoints). Every response the family renders crosses a capWriter, so
// the capability headers ride the errors too.

// msgNoRevisions is the pinned empty-chain 404 wording (spec section 3.1,
// S9).
const msgNoRevisions = "Couldn't find revisions"

// filesResponse is the file-list body: name -> {} per file (spec section
// 3.1's shape; the .timestamp marker never appears in a listing).
type filesResponse struct {
	Files map[string]struct{} `json:"files"`
}

// serveLatest answers GET <ref>/latest: the index's first row.
func (h *Handler) serveLatest(ctx context.Context, cw *capWriter, p *repo.Principal, repoKey string, r ref) {
	doc, err := h.readRecipeIndex(ctx, p, repoKey, r.coordinateRoot())
	if err != nil {
		h.writeError(cw, err, repoKey, r.coordinateRoot())
		return
	}
	latest, ok := latestOf(doc.Revisions)
	if !ok {
		writePlain(cw, http.StatusNotFound, msgNoRevisions)
		return
	}
	writeJSONDoc(cw, latest)
}

// serveRevisions answers GET <ref>/revisions: the whole chain, newest
// first (the index document's own shape).
func (h *Handler) serveRevisions(ctx context.Context, cw *capWriter, p *repo.Principal, repoKey string, r ref) {
	doc, err := h.readRecipeIndex(ctx, p, repoKey, r.coordinateRoot())
	if err != nil {
		h.writeError(cw, err, repoKey, r.coordinateRoot())
		return
	}
	if len(doc.Revisions) == 0 {
		writePlain(cw, http.StatusNotFound, msgNoRevisions)
		return
	}
	if doc.Reference == "" {
		doc.Reference = r.wireRef()
	}
	writeJSONDoc(cw, doc)
}

// serveFileList answers GET …/files (recipe) and
// …/packages/<pid>/revisions/<pRev>/files: the stored tree's file names.
func (h *Handler) serveFileList(ctx context.Context, cw *capWriter, p *repo.Principal,
	repoKey string, rf ref, rrev, pid, prev string) {
	prefix := recipeExportPrefix(rf.coordinateRoot(), rrev)
	if pid != "" {
		prefix = pkgFilePrefix(rf.coordinateRoot(), rrev, pid, prev)
	}
	files, err := h.listFiles(ctx, p, repoKey, prefix)
	if err != nil {
		h.writeError(cw, err, repoKey, prefix)
		return
	}
	if len(files) == 0 {
		writePlain(cw, http.StatusNotFound, msgPathNotFound)
		return
	}
	set := make(map[string]struct{}, len(files))
	for _, f := range files {
		set[f] = struct{}{}
	}
	writeJSONDoc(cw, filesResponse{Files: set})
}

// listFiles enumerates the FILE nodes under prefix, dropping folder rows
// and the .timestamp markers, returning repo-relative names.
func (h *Handler) listFiles(ctx context.Context, p *repo.Principal, repoKey, prefix string) ([]string, error) {
	nodes, err := h.svc.List(ctx, p, repoKey, prefix)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range nodes {
		if strings.HasSuffix(n.Path, "/") {
			continue
		}
		name := strings.TrimPrefix(n.Path, prefix)
		if name == "" || strings.HasSuffix(name, "/"+timestampFile) || name == timestampFile {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

// serveFileGet answers GET/HEAD …/files/<path>: the stored node's stream.
func (h *Handler) serveFileGet(ctx context.Context, cw *capWriter, r *http.Request, p *repo.Principal, repoKey, path string) {
	rc, node, err := h.svc.Get(ctx, p, repoKey, path)
	if err != nil {
		h.writeError(cw, err, repoKey, path)
		return
	}
	h.serveNode(ctx, cw, r, node, rc, "application/octet-stream")
}

// serveFilePut answers PUT …/files/<path> (recipe arm pid=="" / package
// arm pid!=""): land the file through the service, then run the revision
// registration tail (the .timestamp first-write plus the index row).
func (h *Handler) serveFilePut(ctx context.Context, cw *capWriter, r *http.Request, p *repo.Principal,
	repoKey string, rf ref, rrev, pid, prev, nodePath string) {
	expect, err := declaredDigests(r.Header)
	if err != nil {
		writePlain(cw, http.StatusBadRequest, err.Error())
		return
	}

	if isChecksumDeploy(r.Header) {
		h.serveChecksumDeploy(ctx, cw, p, repoKey, rf, rrev, pid, prev, nodePath, expect)
		return
	}

	mime := r.Header.Get("Content-Type")
	if mime == "" {
		mime = "application/octet-stream"
	}
	if _, err := h.svc.Put(ctx, p, repoKey, nodePath, r.Body, expect, mime); err != nil {
		h.writeError(cw, err, repoKey, nodePath)
		return
	}
	if err := h.registerRevision(ctx, p, repoKey, rf, rrev, pid, prev); err != nil {
		h.writeError(cw, err, repoKey, nodePath)
		return
	}
	cw.WriteHeader(http.StatusCreated)
}

// registerRevision is the after-PUT tail for whichever revision plane the
// write touched (recipe when pid=="", package otherwise).
func (h *Handler) registerRevision(ctx context.Context, p *repo.Principal, repoKey string, rf ref, rrev, pid, prev string) error {
	if pid == "" {
		return h.registerRecipeRevision(ctx, p, repoKey, rf, rrev)
	}
	return h.registerPkgRevision(ctx, p, repoKey, rf, rrev, pid, prev)
}

// serveChecksumDeploy implements X-Checksum-Deploy on both conan planes
// (spec section 6.3 / S8): a zero-transfer deploy addressed by sha256 or
// sha1, answered with the family's 201; a miss is the 404 semantics the
// spec pins (the C15 wording the maven/generic planes established).
func (h *Handler) serveChecksumDeploy(ctx context.Context, cw *capWriter, p *repo.Principal,
	repoKey string, rf ref, rrev, pid, prev, nodePath string, expect storage.BlobRef) {
	if expect.Sha256 == "" && expect.Sha1 == "" {
		writePlain(cw, http.StatusBadRequest,
			"Checksum deploy failed. no checksum header 'X-Checksum-Sha1/X-Checksum-Sha256' was found.")
		return
	}
	if _, err := h.svc.PutFromBlob(ctx, p, repoKey, nodePath, expect, "application/octet-stream"); err != nil {
		if errors.Is(err, repo.ErrOrphanBlob) || errors.Is(err, repo.ErrNodeNotFound) {
			writePlain(cw, http.StatusNotFound, "Checksum deploy failed: no content found for the given checksum.")
			return
		}
		h.writeError(cw, err, repoKey, nodePath)
		return
	}
	if err := h.registerRevision(ctx, p, repoKey, rf, rrev, pid, prev); err != nil {
		h.writeError(cw, err, repoKey, nodePath)
		return
	}
	cw.WriteHeader(http.StatusCreated)
}

// isChecksumDeploy reports the X-Checksum-Deploy header's truth value
// (absent or "false" is off — the maven reading).
func isChecksumDeploy(hdr http.Header) bool {
	v := hdr.Get(hdrChecksumDeploy)
	return v != "" && !strings.EqualFold(v, "false")
}

// serveRevisionDelete answers DELETE <ref>/revisions/{rRev}: the revision
// subtree plus its index row. The 404 wording is pinned (spec section 3.1).
func (h *Handler) serveRevisionDelete(ctx context.Context, cw *capWriter, p *repo.Principal, repoKey string, rf ref, rrev string) {
	root := rf.coordinateRoot()
	revRoot := root + "/" + rrev + "/"
	exists, err := h.subtreeExists(ctx, p, repoKey, revRoot)
	if err != nil {
		h.writeError(cw, err, repoKey, revRoot)
		return
	}
	if !exists {
		writePlain(cw, http.StatusNotFound, fmt.Sprintf("Couldn't find path '%s'", revRoot))
		return
	}
	if err := h.svc.Delete(ctx, p, repoKey, revRoot); err != nil {
		h.writeError(cw, err, repoKey, revRoot)
		return
	}
	if err := h.removeRecipeRevisionEntry(ctx, p, repoKey, rf, rrev); err != nil {
		h.writeError(cw, err, repoKey, revRoot)
		return
	}
	cw.WriteHeader(http.StatusOK)
}

// serveRecipeDelete answers DELETE <ref>: the whole coordinate — every
// revision, every package.
func (h *Handler) serveRecipeDelete(ctx context.Context, cw *capWriter, p *repo.Principal, repoKey string, rf ref) {
	root := rf.coordinateRoot() + "/"
	exists, err := h.subtreeExists(ctx, p, repoKey, root)
	if err != nil {
		h.writeError(cw, err, repoKey, root)
		return
	}
	if !exists {
		writePlain(cw, http.StatusNotFound, fmt.Sprintf("Couldn't find path '%s'", root))
		return
	}
	if err := h.svc.Delete(ctx, p, repoKey, root); err != nil {
		h.writeError(cw, err, repoKey, root)
		return
	}
	cw.WriteHeader(http.StatusOK)
}

// servePackagesDelete answers DELETE …/packages: every binary of one rRev.
// The empty-tree 404 wording is pinned (spec section 3.1).
func (h *Handler) servePackagesDelete(ctx context.Context, cw *capWriter, p *repo.Principal, repoKey string, rf ref, rrev string) {
	dir := rf.coordinateRoot() + "/" + rrev + "/" + dirPackage + "/"
	exists, err := h.subtreeExists(ctx, p, repoKey, dir)
	if err != nil {
		h.writeError(cw, err, repoKey, dir)
		return
	}
	if !exists {
		writePlain(cw, http.StatusNotFound, "Couldn't find packages for deletion")
		return
	}
	if err := h.svc.Delete(ctx, p, repoKey, dir); err != nil {
		h.writeError(cw, err, repoKey, dir)
		return
	}
	cw.WriteHeader(http.StatusOK)
}

// servePkgLatest answers GET …/packages/{pid}/latest.
func (h *Handler) servePkgLatest(ctx context.Context, cw *capWriter, p *repo.Principal, repoKey string, rf ref, rrev, pid string) {
	doc, err := h.readPkgIndex(ctx, p, repoKey, rf.coordinateRoot(), rrev, pid)
	if err != nil {
		h.writeError(cw, err, repoKey, rf.coordinateRoot())
		return
	}
	latest, ok := latestOf(doc.Revisions)
	if !ok {
		writePlain(cw, http.StatusNotFound, msgNoRevisions)
		return
	}
	writeJSONDoc(cw, latest)
}

// servePkgRevisions answers GET …/packages/{pid}/revisions (an empty chain
// is the 404 — spec section 3.1's column).
func (h *Handler) servePkgRevisions(ctx context.Context, cw *capWriter, p *repo.Principal, repoKey string, rf ref, rrev, pid string) {
	doc, err := h.readPkgIndex(ctx, p, repoKey, rf.coordinateRoot(), rrev, pid)
	if err != nil {
		h.writeError(cw, err, repoKey, rf.coordinateRoot())
		return
	}
	if len(doc.Revisions) == 0 {
		writePlain(cw, http.StatusNotFound, msgNoRevisions)
		return
	}
	if doc.Reference == "" {
		doc.Reference = rf.wireRef() + "#" + rrev + ":" + pid
	}
	writeJSONDoc(cw, doc)
}

// servePkgRevDelete answers DELETE …/packages/{pid}/revisions/{pRev}.
func (h *Handler) servePkgRevDelete(ctx context.Context, cw *capWriter, p *repo.Principal, repoKey string, rf ref, rrev, pid, prev string) {
	revRoot := pkgFilePrefix(rf.coordinateRoot(), rrev, pid, prev)
	exists, err := h.subtreeExists(ctx, p, repoKey, revRoot)
	if err != nil {
		h.writeError(cw, err, repoKey, revRoot)
		return
	}
	if !exists {
		writePlain(cw, http.StatusNotFound, fmt.Sprintf("Couldn't find path '%s'", revRoot))
		return
	}
	if err := h.svc.Delete(ctx, p, repoKey, revRoot); err != nil {
		h.writeError(cw, err, repoKey, revRoot)
		return
	}
	if err := h.removePkgRevisionEntry(ctx, p, repoKey, rf, rrev, pid, prev); err != nil {
		h.writeError(cw, err, repoKey, revRoot)
		return
	}
	cw.WriteHeader(http.StatusOK)
}

// subtreeExists reports whether any node lives under prefix (folder rows
// included — the coordinate's own folder row counts as existence).
func (h *Handler) subtreeExists(ctx context.Context, p *repo.Principal, repoKey, prefix string) (bool, error) {
	nodes, err := h.svc.List(ctx, p, repoKey, prefix)
	if err != nil {
		return false, err
	}
	return len(nodes) > 0, nil
}

// ---- shared small helpers ----

// declaredDigests parses the X-Checksum-* family (the shared contract;
// malformed → 400).
func declaredDigests(hdr http.Header) (storage.BlobRef, error) {
	parse := func(name string, width int) (string, error) {
		v := strings.ToLower(strings.TrimSpace(hdr.Get(name)))
		if v == "" {
			return "", nil
		}
		if !isHex(v, width) {
			return "", fmt.Errorf("%w: %s must be exactly %d hex characters, got %q",
				errInvalidChecksum, name, width, v)
		}
		return v, nil
	}
	sha256v, err := parse(hdrChecksumSha256, 64)
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
	return storage.BlobRef{Sha256: sha256v, Sha1: sha1, Md5: md5}, nil
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

// blobRefOf declares the measured sha256 of one small in-memory body (the
// index-document and .timestamp writes' expect argument).
func blobRefOf(b []byte) storage.BlobRef {
	sum := sha256.Sum256(b)
	return storage.BlobRef{Sha256: hex.EncodeToString(sum[:])}
}

// readBodyJSON reads one bounded JSON body (the v1 POST endpoints').
func readBodyJSON(r *http.Request, into any, limit int64) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if int64(len(raw)) > limit {
		return errors.New("body exceeds the size limit")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return errors.New("body is empty")
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("parse body: %w", err)
	}
	return nil
}
