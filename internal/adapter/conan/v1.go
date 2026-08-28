package conan

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The v1 plane (spec sections 2/3.2 — CN-1's final scope: the FULL data
// face, not the narrowed handshake). The handshake family is a conan 2
// client's hard dependency (it walks v1/ping, v1/users/authenticate and
// v1/users/check_credentials even for v2-only work); the data family is
// the conan 1.x wire.
//
// Revision resolution on this plane: the URL/storage legs carry the
// DEFAULT revision segment `0` (getExportPathDefaultRevision's shape), and
// the server resolves the latest revision through the index layer — a v1
// read of a `0` address serves the LATEST revision's tree, which for a
// pure-v1 coordinate IS the `0` tree (the v1 PUT registers `0` with a
// fresh .timestamp). A v2-uploaded coordinate therefore answers its v1
// readers from the v2 revision — the two planes share one truth.

// urlBody is the download_urls/upload_urls/digest response shape.
type urlBody map[string]string

// snapshotBody is the v1 snapshot shape: file name -> md5.
type snapshotBody map[string]string

// servePing answers GET v1/ping: 200 with an empty body — reachable
// anonymous (the client's probe).
func (h *Handler) servePing(cw *capWriter) {
	cw.Header().Set("Content-Length", "0")
	cw.WriteHeader(http.StatusOK)
}

// serveAuthenticate answers GET v1/users/authenticate: the plain-text
// token body. The middleware chain settled the Authorization header, so a
// present principal IS a verified credential (Basic or Bearer); anonymous
// meets the 401 challenge.
func (h *Handler) serveAuthenticate(ctx context.Context, cw *capWriter, p *repo.Principal) {
	if p == nil {
		cw.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
		writePlain(cw, http.StatusUnauthorized, "unauthorized user")
		return
	}
	if h.toks == nil {
		writePlain(cw, http.StatusServiceUnavailable, "token issuance is not configured on this instance")
		return
	}
	tok, err := h.toks.Issue(ctx, p.Name, h.opts.TokenTTL)
	if err != nil {
		writePlain(cw, http.StatusInternalServerError, "token issuance failed: "+err.Error())
		return
	}
	writePlain(cw, http.StatusOK, tok.AccessToken)
}

// serveCheckCredentials answers GET v1/users/check_credentials: 200 empty
// for a verified credential, the 401 challenge for anonymous.
func (h *Handler) serveCheckCredentials(cw *capWriter, p *repo.Principal) {
	if p == nil {
		cw.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
		writePlain(cw, http.StatusUnauthorized, "unauthorized user")
		return
	}
	cw.Header().Set("Content-Length", "0")
	cw.WriteHeader(http.StatusOK)
}

// filesBase renders the v1 files channel's absolute-URL prefix (TL-1's
// base; the coordinate-order storage path under revision 0 is appended by
// the callers).
func (h *Handler) filesBase(r *http.Request, repoKey string, rf ref) string {
	return h.baseURLFor(r) + "/binflow/" + repoKey + "/" + segV1 + "/" + segFiles + "/" +
		rf.user + "/" + rf.name + "/" + rf.version + "/" + rf.channel + "/" + revDefaultV1 + "/"
}

// serveDigest answers GET conans/<ref>[/packages/<pid>]/digest: the
// manifest's download address (latest revision, index-resolved).
func (h *Handler) serveDigest(ctx context.Context, cw *capWriter, r *http.Request, p *repo.Principal, repoKey string, rf ref, pid string) {
	rrev, _, err := h.resolveRRev(ctx, p, repoKey, rf, "")
	if err != nil {
		h.writeError(cw, err, repoKey, rf.coordinateRoot())
		return
	}
	const manifest = "conanmanifest.txt"
	var path string
	sub := dirExport + "/" + manifest
	if pid == "" {
		path = recipeFile(rf.coordinateRoot(), rrev, manifest)
	} else {
		latest, ok, err := h.latestPRev(ctx, p, repoKey, rf, rrev, pid)
		if err != nil {
			h.writeError(cw, err, repoKey, rf.coordinateRoot())
			return
		}
		if !ok {
			writePlain(cw, http.StatusNotFound, msgPathNotFound)
			return
		}
		path = pkgFile(rf.coordinateRoot(), rrev, pid, latest, manifest)
		sub = dirPackage + "/" + pid + "/" + manifest
	}
	if _, _, err := h.svc.Get(ctx, p, repoKey, path); err != nil {
		h.writeError(cw, err, repoKey, path)
		return
	}
	writeJSONDoc(cw, urlBody{manifest: h.filesBase(r, repoKey, rf) + sub})
}

// latestPRev resolves the packageId's newest revision under rrev.
func (h *Handler) latestPRev(ctx context.Context, p *repo.Principal, repoKey string, rf ref, rrev, pid string) (string, bool, error) {
	doc, err := h.readPkgIndex(ctx, p, repoKey, rf.coordinateRoot(), rrev, pid)
	if err != nil {
		return "", false, err
	}
	latest, ok := latestOf(doc.Revisions)
	if !ok {
		return "", false, nil
	}
	return latest.Revision, true, nil
}

// serveDownloadURLs answers GET conans/<ref>[/packages/<pid>]/download_urls:
// every file of the latest revision, each mapped to its v1 channel GET
// address.
func (h *Handler) serveDownloadURLs(ctx context.Context, cw *capWriter, r *http.Request, p *repo.Principal, repoKey string, rf ref, pid string) {
	rrev, _, err := h.resolveRRev(ctx, p, repoKey, rf, "")
	if err != nil {
		h.writeError(cw, err, repoKey, rf.coordinateRoot())
		return
	}
	var prefix, sub string
	if pid == "" {
		prefix = recipeExportPrefix(rf.coordinateRoot(), rrev)
		sub = dirExport + "/"
	} else {
		prev, ok, err := h.latestPRev(ctx, p, repoKey, rf, rrev, pid)
		if err != nil {
			h.writeError(cw, err, repoKey, rf.coordinateRoot())
			return
		}
		if !ok {
			writePlain(cw, http.StatusNotFound, msgPathNotFound)
			return
		}
		prefix = pkgFilePrefix(rf.coordinateRoot(), rrev, pid, prev)
		sub = dirPackage + "/" + pid + "/"
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
	base := h.filesBase(r, repoKey, rf) + sub
	out := urlBody{}
	for _, f := range files {
		out[f] = base + f
	}
	writeJSONDoc(cw, out)
}

// uploadBody is the upload_urls request shape: file -> size.
type uploadBody map[string]int64

// serveUploadURLs answers POST conans/<ref>[/packages/<pid>]/upload_urls:
// the same keys mapped to v1 channel PUT addresses.
func (h *Handler) serveUploadURLs(cw *capWriter, r *http.Request, repoKey string, rf ref, pid string) {
	var body uploadBody
	if err := readBodyJSON(r, &body, maxURLBody); err != nil {
		writePlain(cw, http.StatusBadRequest, err.Error())
		return
	}
	base := h.filesBase(r, repoKey, rf)
	if pid == "" {
		base += dirExport + "/"
	} else {
		base += dirPackage + "/" + pid + "/"
	}
	names := make([]string, 0, len(body))
	for name := range body {
		names = append(names, name)
	}
	sort.Strings(names)
	out := urlBody{}
	for _, name := range names {
		if !validChannelPath(name) {
			writePlain(cw, http.StatusBadRequest, fmt.Sprintf("illegal file name %q", name))
			return
		}
		out[name] = base + name
	}
	writeJSONDoc(cw, out)
}

// maxURLBody bounds the upload_urls / remove_files bodies.
const maxURLBody = 1 << 20

// serveSnapshot answers GET conans/<ref>[/packages/<pid>]: file name ->
// md5 of the latest revision's tree (the .timestamp excluded).
func (h *Handler) serveSnapshot(ctx context.Context, cw *capWriter, p *repo.Principal, repoKey string, rf ref, pid string) {
	rrev, _, err := h.resolveRRev(ctx, p, repoKey, rf, "")
	if err != nil {
		h.writeError(cw, err, repoKey, rf.coordinateRoot())
		return
	}
	var prefix string
	if pid == "" {
		prefix = recipeExportPrefix(rf.coordinateRoot(), rrev)
	} else {
		prev, ok, err := h.latestPRev(ctx, p, repoKey, rf, rrev, pid)
		if err != nil {
			h.writeError(cw, err, repoKey, rf.coordinateRoot())
			return
		}
		if !ok {
			writePlain(cw, http.StatusNotFound, msgPathNotFound)
			return
		}
		prefix = pkgFilePrefix(rf.coordinateRoot(), rrev, pid, prev)
	}
	nodes, err := h.svc.List(ctx, p, repoKey, prefix)
	if err != nil {
		h.writeError(cw, err, repoKey, prefix)
		return
	}
	out := snapshotBody{}
	for _, n := range nodes {
		if strings.HasSuffix(n.Path, "/") {
			continue
		}
		name := strings.TrimPrefix(n.Path, prefix)
		if name == "" || strings.HasSuffix(name, "/"+timestampFile) || name == timestampFile {
			continue
		}
		out[name] = h.md5Of(ctx, n)
	}
	if len(out) == 0 {
		writePlain(cw, http.StatusNotFound, msgPathNotFound)
		return
	}
	writeJSONDoc(cw, out)
}

// serveV1RecipeDelete answers DELETE conans/<ref>: the LATEST revision
// chain (spec section 3.2's parenthetical — the v1 plane has no revision
// addressing, so "the recipe" is what the index calls newest).
func (h *Handler) serveV1RecipeDelete(ctx context.Context, cw *capWriter, p *repo.Principal, repoKey string, rf ref) {
	rrev, _, err := h.resolveRRev(ctx, p, repoKey, rf, "")
	if err != nil {
		h.writeError(cw, err, repoKey, rf.coordinateRoot())
		return
	}
	revRoot := rf.coordinateRoot() + "/" + rrev + "/"
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

// removeFilesBody is the remove_files request shape.
type removeFilesBody struct {
	Files []string `json:"files"`
}

// serveRemoveFiles answers POST …/remove_files (recipe and package arms):
// delete the named files from the latest revision's tree.
func (h *Handler) serveRemoveFiles(ctx context.Context, cw *capWriter, r *http.Request, p *repo.Principal, repoKey string, rf ref, pid string) {
	var body removeFilesBody
	if err := readBodyJSON(r, &body, maxURLBody); err != nil {
		writePlain(cw, http.StatusBadRequest, err.Error())
		return
	}
	rrev, _, err := h.resolveRRev(ctx, p, repoKey, rf, "")
	if err != nil {
		h.writeError(cw, err, repoKey, rf.coordinateRoot())
		return
	}
	for _, name := range body.Files {
		if !validChannelPath(name) {
			writePlain(cw, http.StatusBadRequest, fmt.Sprintf("illegal file name %q", name))
			return
		}
		var path string
		if pid == "" {
			path = recipeFile(rf.coordinateRoot(), rrev, name)
		} else {
			prev, ok, err := h.latestPRev(ctx, p, repoKey, rf, rrev, pid)
			if err != nil {
				h.writeError(cw, err, repoKey, rf.coordinateRoot())
				return
			}
			if !ok {
				writePlain(cw, http.StatusNotFound, msgPathNotFound)
				return
			}
			path = pkgFile(rf.coordinateRoot(), rrev, pid, prev, name)
		}
		if err := h.svc.Delete(ctx, p, repoKey, path); err != nil {
			h.writeError(cw, err, repoKey, path)
			return
		}
	}
	cw.WriteHeader(http.StatusOK)
}

// packagesDeleteBody is the packages/delete request shape.
type packagesDeleteBody struct {
	PackageIDs []string `json:"package_ids"`
}

// servePackagesDeleteIDs answers POST conans/<ref>/packages/delete: drop
// the named packageIds under the latest revision. The batch is IDEMPOTENT:
// spec section 3.2's row for this endpoint carries no 404 arm (unlike the
// recipe-delete row's "200 / 404"), and the reference implementation's
// package delete is a filesystem removal — a missing path is a silent
// no-op. A packageId with no tree under the resolved revision (a conan-2
// coordinate's leftover from an older upload, or a pid another operator
// already removed) is therefore skipped, not an error; the request still
// answers 200. D-F: the loop used to let one missing pid fail the whole
// batch AFTER the earlier pids' trees were already gone (the "delete
// succeeded, status said 404" posture). Only the ref itself failing to
// resolve keeps the 404.
func (h *Handler) servePackagesDeleteIDs(ctx context.Context, cw *capWriter, r *http.Request, p *repo.Principal, repoKey string, rf ref) {
	var body packagesDeleteBody
	if err := readBodyJSON(r, &body, maxURLBody); err != nil {
		writePlain(cw, http.StatusBadRequest, err.Error())
		return
	}
	rrev, _, err := h.resolveRRev(ctx, p, repoKey, rf, "")
	if err != nil {
		h.writeError(cw, err, repoKey, rf.coordinateRoot())
		return
	}
	for _, pid := range body.PackageIDs {
		if !validPackageID(pid) {
			writePlain(cw, http.StatusBadRequest, fmt.Sprintf("illegal package id %q", pid))
			return
		}
		dir := pkgDir(rf.coordinateRoot(), rrev, pid) + "/"
		if err := h.svc.Delete(ctx, p, repoKey, dir); err != nil {
			if errors.Is(err, repo.ErrNodeNotFound) {
				continue // already gone: the batch is idempotent
			}
			h.writeError(cw, err, repoKey, dir)
			return
		}
	}
	cw.WriteHeader(http.StatusOK)
}

// serveV1Files answers the direct channel (PUT/GET
// v1/files/<user>/<name>/<ver>/<channel>/[0/]export|package/…): the storage
// legs the url endpoints hand out. A PUT lands at the literal `0` tree and
// registers revision `0` (a fresh .timestamp makes it the index's latest);
// a GET resolves the latest revision at the index layer and serves that
// tree — the two legs meet on the same truth. The 404's wording is pinned
// (spec section 3.2).
func (h *Handler) serveV1Files(ctx context.Context, cw *capWriter, r *http.Request, p *repo.Principal, repoKey string, rt route) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		rrev, _, err := h.resolveRRev(ctx, p, repoKey, rt.ref, "")
		if err != nil {
			h.writeError(cw, err, repoKey, rt.ref.coordinateRoot())
			return
		}
		nodePath := rt.ref.coordinateRoot() + "/" + rrev + "/" + rt.path
		if rt.pid != "" {
			prev, ok, err := h.latestPRev(ctx, p, repoKey, rt.ref, rrev, rt.pid)
			if err != nil {
				h.writeError(cw, err, repoKey, rt.ref.coordinateRoot())
				return
			}
			if !ok {
				writePlain(cw, http.StatusNotFound, msgPathNotFound)
				return
			}
			nodePath = pkgFilePrefix(rt.ref.coordinateRoot(), rrev, rt.pid, prev) + channelFileName(rt.path, rt.pid)
		}
		rc, node, err := h.svc.Get(ctx, p, repoKey, nodePath)
		if err != nil {
			h.writeError(cw, err, repoKey, nodePath)
			return
		}
		h.serveNode(ctx, cw, r, node, rc, "application/octet-stream")
	case http.MethodPut:
		h.serveV1FilesPut(ctx, cw, r, p, repoKey, rt)
	default:
		cw.Header().Set("Allow", "GET, HEAD, PUT")
		writePlain(cw, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the files channel")
	}
}

// channelFileName strips the assembled channel path's package prefix down
// to the file tail (the GET leg re-addresses the latest pRev tree, so the
// name must ride over).
func channelFileName(assembled, pid string) string {
	trimmed := strings.TrimPrefix(assembled, segPackages+"/"+pid+"/")
	return trimmed
}

// serveV1FilesPut is the channel's write leg: land at the `0` tree, then
// register `0` in whichever index the path belongs to. The package arm's
// channel address carries no pRev segment — the default pRev `0`'s tree
// is what it addresses, so the write inserts that segment (the channel's
// GET leg resolves it back through the index).
func (h *Handler) serveV1FilesPut(ctx context.Context, cw *capWriter, r *http.Request, p *repo.Principal, repoKey string, rt route) {
	nodePath := rt.ref.coordinateRoot() + "/" + revDefaultV1 + "/" + rt.path
	if rt.pid != "" {
		nodePath = pkgDir(rt.ref.coordinateRoot(), revDefaultV1, rt.pid) + "/" + revDefaultV1 + "/" +
			channelFileName(rt.path, rt.pid)
	}
	expect, err := declaredDigests(r.Header)
	if err != nil {
		writePlain(cw, http.StatusBadRequest, err.Error())
		return
	}
	if isChecksumDeploy(r.Header) {
		if expect.Sha256 == "" && expect.Sha1 == "" {
			writePlain(cw, http.StatusBadRequest,
				"Checksum deploy failed. no checksum header 'X-Checksum-Sha1/X-Checksum-Sha256' was found.")
			return
		}
		if _, err := h.svc.PutFromBlob(ctx, p, repoKey, nodePath, expect, "application/octet-stream"); err != nil {
			// S8: the channel's miss is the EXPLICIT 404 passthrough.
			writePlain(cw, http.StatusNotFound, msgPathNotFound)
			return
		}
	} else {
		mime := r.Header.Get("Content-Type")
		if mime == "" {
			mime = "application/octet-stream"
		}
		if _, err := h.svc.Put(ctx, p, repoKey, nodePath, r.Body, expect, mime); err != nil {
			h.writeError(cw, err, repoKey, nodePath)
			return
		}
	}
	if rt.pid == "" {
		if err := h.registerRecipeRevision(ctx, p, repoKey, rt.ref, revDefaultV1); err != nil {
			h.writeError(cw, err, repoKey, nodePath)
			return
		}
	} else {
		if err := h.registerPkgRevision(ctx, p, repoKey, rt.ref, revDefaultV1, rt.pid, revDefaultV1); err != nil {
			h.writeError(cw, err, repoKey, nodePath)
			return
		}
	}
	cw.WriteHeader(http.StatusCreated)
}
