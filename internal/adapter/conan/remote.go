package conan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The remote-repository face (spec section 7's remote row): every read is
// served off repo.Service's pull-through engine — svc.Get on a remote
// repository runs the FR-20 chain (negative cache, TTL-classed copy,
// guarded upstream fetch with the stale-while-error downgrade), and the
// engine's upstream hop translates the STORAGE path onto the v2 wire
// grammar through provider.UpstreamPath (the goproxy T-285 posture).
//
// What lives HERE:
//
//   - the index documents (both planes' index.json) fetched as the
//     revisions endpoints' isomorphic bodies and re-rendered through the
//     same latest/revisions shapes the local class serves;
//   - the two document faces with no storage shape of their own (the
//     files listing, the packageId search): the upstream body is cached
//     under the layout's marker paths and served verbatim, so the engine's
//     TTL classes, negative cache and stale service all apply unmodified;
//   - the write refusals: PUT rides the shared arm and the SERVICE answers
//     RE-05's read-only 405 (the BinFlow-wide remote write posture, after
//     the protocol grammar the shared arm checks first); the DELETE
//     endpoints refuse in-handler — the local arms open with a subtree
//     probe, and svc.List does not serve the remote class, so the refusal
//     must land before the probe;
//   - search: the upstream search endpoint carries its pattern in the URL
//     query, which the engine's path-joined upstream hop cannot address —
//     the T-287 ruling family (nuget's precedent). The honest refusal
//     names the boundary.
//
// The handshake family (ping/authenticate/check_credentials, both version
// prefixes) is class-independent and served by the shared arms — it is how
// a conan client discovers the only_v2 capability this class advertises.

// msgRemoteSearch is the search refusal's body (the T-287 constraint,
// named honestly).
const msgRemoteSearch = "Conan search is not proxied on remote repositories by this BinFlow release; address the upstream or a virtual repository directly."

// refuseRemoteDelete answers the v2 DELETE family on a remote repository
// with the RE-05 wording (in-handler so the arms' subtree probes — a
// svc.List the remote class refuses — never run; spec section 7's
// "PUT/DELETE 拒绝" pinned to the BinFlow-wide remote write posture).
func refuseRemoteDelete(cw *capWriter, repoKey string) {
	cw.Header().Set("Allow", http.MethodGet)
	writePlain(cw, http.StatusMethodNotAllowed, fmt.Sprintf(
		"Remote repository '%s' is a read-only proxy cache; deployments to remote repositories are not accepted.", repoKey))
}

// serveRemote dispatches the v2 family on a remote repository. The class
// door has already refused the v1 data plane with S6's 400; the handshake
// trio and the search/later arms carry their own method gates.
func (h *Handler) serveRemote(ctx context.Context, cw *capWriter, r *http.Request, p *repo.Principal, repoKey string, rt route) {
	switch rt.kind {
	// ---- the class-independent handshake (both version prefixes) ----
	case kindV1Ping:
		h.servePing(cw)
	case kindV1Authenticate:
		h.serveAuthenticate(ctx, cw, p)
	case kindV1CheckCredentials:
		h.serveCheckCredentials(cw, p)

	// ---- search: the honest refusal (T-287's query-in-URL constraint) ----
	case kindV2Search:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		writePlain(cw, http.StatusNotFound, msgRemoteSearch)

	// ---- the revision chain, off the proxied index documents ----
	case kindV2Latest:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		doc, hints, err := h.proxiedRecipeIndex(ctx, p, repoKey, rt.ref)
		if err != nil {
			h.writeError(cw, err, repoKey, rt.ref.coordinateRoot())
			return
		}
		latest, ok := latestOf(doc.Revisions)
		if !ok {
			writePlain(cw, http.StatusNotFound, msgNoRevisions)
			return
		}
		mergeHints(cw, hints)
		writeJSONDoc(cw, latest)
	case kindV2Revisions:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		doc, hints, err := h.proxiedRecipeIndex(ctx, p, repoKey, rt.ref)
		if err != nil {
			h.writeError(cw, err, repoKey, rt.ref.coordinateRoot())
			return
		}
		if len(doc.Revisions) == 0 {
			writePlain(cw, http.StatusNotFound, msgNoRevisions)
			return
		}
		if doc.Reference == "" {
			doc.Reference = rt.ref.wireRef()
		}
		mergeHints(cw, hints)
		writeJSONDoc(cw, doc)

	// ---- the packageId metadata document (marker-cached, verbatim) ----
	case kindV2RefSearch:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		// The implicit-latest variant resolves through the proxied index
		// first (the index-layer latest resolution holding through the hop).
		rrev, _, err := h.resolveRRevRemote(ctx, p, repoKey, rt.ref)
		if err != nil {
			h.writeError(cw, err, repoKey, rt.ref.coordinateRoot())
			return
		}
		h.serveProxiedDoc(ctx, cw, r, p, repoKey, refSearchMarker(rt.ref.coordinateRoot(), rrev))
	case kindV2RevSearch:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveProxiedDoc(ctx, cw, r, p, repoKey, refSearchMarker(rt.ref.coordinateRoot(), rt.rRev))

	// ---- the files family: marker listing / engine file / shared PUT ----
	case kindV2Files:
		root := rt.ref.coordinateRoot()
		h.serveRemoteFiles(ctx, cw, r, p, repoKey, rt,
			recipeFilesMarker(root, rt.rRev),
			func(path string) string { return recipeFile(root, rt.rRev, path) })
	case kindV2PkgFiles:
		root := rt.ref.coordinateRoot()
		h.serveRemoteFiles(ctx, cw, r, p, repoKey, rt,
			pkgFilesMarker(root, rt.rRev, rt.pid, rt.pRev),
			func(path string) string { return pkgFile(root, rt.rRev, rt.pid, rt.pRev, path) })

	// ---- the package revision chain, off the proxied pkg index ----
	case kindV2PkgLatest:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		doc, hints, err := h.proxiedPkgIndex(ctx, p, repoKey, rt.ref, rt.rRev, rt.pid)
		if err != nil {
			h.writeError(cw, err, repoKey, rt.ref.coordinateRoot())
			return
		}
		latest, ok := latestOf(doc.Revisions)
		if !ok {
			writePlain(cw, http.StatusNotFound, msgNoRevisions)
			return
		}
		mergeHints(cw, hints)
		writeJSONDoc(cw, latest)
	case kindV2PkgRevisions:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		doc, hints, err := h.proxiedPkgIndex(ctx, p, repoKey, rt.ref, rt.rRev, rt.pid)
		if err != nil {
			h.writeError(cw, err, repoKey, rt.ref.coordinateRoot())
			return
		}
		if len(doc.Revisions) == 0 {
			writePlain(cw, http.StatusNotFound, msgNoRevisions)
			return
		}
		if doc.Reference == "" {
			doc.Reference = rt.ref.wireRef() + "#" + rt.rRev + ":" + rt.pid
		}
		mergeHints(cw, hints)
		writeJSONDoc(cw, doc)

	// ---- the write family ----
	case kindV2RecipeDelete, kindV2RevDelete, kindV2PackagesDelete, kindV2PkgRevDelete:
		if !h.requireMethod(cw, r, http.MethodDelete) {
			return
		}
		refuseRemoteDelete(cw, repoKey)

	default:
		writePlain(cw, http.StatusNotFound, "not found")
	}
}

// serveRemoteFiles serves the v2 files family on a remote repository:
// the listing is the marker-cached upstream document, the file bodies ride
// the engine through svc.Get, and a PUT falls through to the shared arm
// (whose service call answers the read-only 405).
func (h *Handler) serveRemoteFiles(ctx context.Context, cw *capWriter, r *http.Request, p *repo.Principal,
	repoKey string, rt route, marker string, nodePathOf func(path string) string) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if rt.path == "" {
			h.serveProxiedDoc(ctx, cw, r, p, repoKey, marker)
			return
		}
		path := nodePathOf(rt.path)
		rc, node, err := h.svc.Get(ctx, p, repoKey, path)
		if err != nil {
			h.writeError(cw, err, repoKey, path)
			return
		}
		h.serveNode(ctx, cw, r, node, rc, "application/octet-stream")
	case http.MethodPut:
		path := nodePathOf(rt.path)
		if rt.pid == "" {
			h.serveFilePut(ctx, cw, r, p, repoKey, rt.ref, rt.rRev, "", "", path)
			return
		}
		h.serveFilePut(ctx, cw, r, p, repoKey, rt.ref, rt.rRev, rt.pid, rt.pRev, path)
	default:
		cw.Header().Set("Allow", "GET, HEAD, PUT")
		writePlain(cw, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the files target")
	}
}

// proxiedRecipeIndex loads the coordinate's index document through the
// pull-through chain (the upstream revisions body, isomorphic to the index)
// and sorts it by the index's own rule. The unfound family maps to the
// empty chain — the callers answer the pinned no-revisions 404. The
// engine's response hints (X-BinFlow-Cache) ride back for the caller to
// merge onto its rebuilt response.
func (h *Handler) proxiedRecipeIndex(ctx context.Context, p *repo.Principal, repoKey string, rf ref) (*recipeIndexDoc, http.Header, error) {
	path := recipeIndex(rf.coordinateRoot())
	rc, _, err := h.svc.Get(ctx, p, repoKey, path)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) || errors.Is(err, repo.ErrIsFolder) {
			return &recipeIndexDoc{Reference: "", Revisions: nil}, nil, nil
		}
		return nil, nil, err
	}
	hints := readerHints(rc)
	raw, rerr := io.ReadAll(io.LimitReader(rc, maxIndexBytes))
	closeErr := rc.Close()
	if rerr != nil {
		return nil, nil, fmt.Errorf("read proxied recipe index %s: %w", path, rerr)
	}
	if closeErr != nil {
		return nil, nil, fmt.Errorf("read proxied recipe index %s: %w", path, closeErr)
	}
	var doc recipeIndexDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		// A corrupted upstream document degrades to the empty chain, the
		// same posture a corrupted local index takes (reindex's repair
		// stance, applied to the proxied copy).
		return &recipeIndexDoc{Reference: "", Revisions: nil}, hints, nil
	}
	sortRevisions(doc.Revisions)
	return &doc, hints, nil
}

// proxiedPkgIndex loads the packageId's index document the same way.
func (h *Handler) proxiedPkgIndex(ctx context.Context, p *repo.Principal, repoKey string, rf ref, rrev, pid string) (*pkgIndexDoc, http.Header, error) {
	path := pkgIndex(rf.coordinateRoot(), rrev, pid)
	rc, _, err := h.svc.Get(ctx, p, repoKey, path)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) || errors.Is(err, repo.ErrIsFolder) {
			return &pkgIndexDoc{Reference: "", Revisions: nil}, nil, nil
		}
		return nil, nil, err
	}
	hints := readerHints(rc)
	raw, rerr := io.ReadAll(io.LimitReader(rc, maxIndexBytes))
	closeErr := rc.Close()
	if rerr != nil {
		return nil, nil, fmt.Errorf("read proxied package index %s: %w", path, rerr)
	}
	if closeErr != nil {
		return nil, nil, fmt.Errorf("read proxied package index %s: %w", path, closeErr)
	}
	var doc pkgIndexDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return &pkgIndexDoc{Reference: "", Revisions: nil}, hints, nil
	}
	sortRevisions(doc.Revisions)
	return &doc, hints, nil
}

// readerHints extracts a reader's structural response hints (the remote
// engine's X-BinFlow-Cache family; the npm readerHints posture).
func readerHints(rc io.ReadSeekCloser) http.Header {
	if extra, ok := rc.(interface{ ExtraHeaders() http.Header }); ok {
		return extra.ExtraHeaders()
	}
	return nil
}

// mergeHints folds one hint set onto the response writer (nil-safe).
func mergeHints(w http.ResponseWriter, hints http.Header) {
	for k, vv := range hints {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
}

// resolveRRevRemote is the remote class's implicit-latest resolver (the
// index-layer rule holding through the hop).
func (h *Handler) resolveRRevRemote(ctx context.Context, p *repo.Principal, repoKey string, rf ref) (string, revEntry, error) {
	doc, _, err := h.proxiedRecipeIndex(ctx, p, repoKey, rf)
	if err != nil {
		return "", revEntry{}, err
	}
	latest, ok := latestOf(doc.Revisions)
	if !ok {
		return "", revEntry{}, fmt.Errorf("%w: no revision of %s", repo.ErrNodeNotFound, rf.wireRef())
	}
	return latest.Revision, latest, nil
}

// serveProxiedDoc streams one marker-cached upstream document verbatim:
// the body IS the endpoint's response shape, so no rebuild happens — the
// engine's response hints (X-BinFlow-Cache) ride along structurally.
func (h *Handler) serveProxiedDoc(ctx context.Context, cw *capWriter, r *http.Request, p *repo.Principal, repoKey, path string) {
	rc, node, err := h.svc.Get(ctx, p, repoKey, path)
	if err != nil {
		h.writeError(cw, err, repoKey, path)
		return
	}
	h.serveNode(ctx, cw, r, node, rc, "application/json")
}
