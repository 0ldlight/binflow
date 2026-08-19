package npm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/lzwzzy/binflow/internal/repo"
)

// Unpublish (NE-05). npm's two-step client flow against BinFlow:
//
//	step 1  PUT /<name>/-rev/<rev>          — the couch revision placeholder.
//	        The REVERSE SPEC pins "always 200 fake success, server does
//	        nothing" (maven-npm-pypi.md section 2.7-1) — but the npm 10
//	        client (libnpmpublish/lib/unpublish.js, verified on npm
//	        10.9.8) carries the FULL modified packument in this very PUT
//	        when unpublishing ONE version of several, and only the DELETE
//	        that follows removes the tarball. BinFlow therefore answers
//	        200 {"ok":"updated package"} unconditionally AND applies a
//	        packument-shaped body (the spec's own open question "隐式副作
//	        用需抓包确认" resolved by that client reading); a non-object
//	        body (the placeholder string of M27-AC11) changes nothing,
//	        which keeps the pinned fake-success contract byte-identical.
//	step 2  DELETE /<name>/-rev/<rev>              — whole package
//	        DELETE /<name>/-/<file>/-rev/<rev>     — one version
//
// Both DELETEs answer 200 and clean dist-tag references (the T-61 AC①
// "dist-tags 引用联动清理").

// revBodyMax bounds the -rev PUT bodies (a full packument or a tiny string).
const revBodyMax = 64 << 20

// servePackageRev handles /<name>/-rev/<rev>.
func (h *Handler) servePackageRev(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *Principal, repoKey, name, rev string) {
	_ = rev // opaque placeholder: never validated (spec pins it unused)
	switch r.Method {
	case http.MethodPut:
		h.serveRevPut(ctx, w, r, p, repoKey, name)
	case http.MethodDelete:
		h.unpublishWhole(ctx, w, p, repoKey, name)
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		writeError(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on a package revision")
	}
}

// serveRevPut is the fake-success PUT: 200 always; a packument-shaped body
// is applied (npm 10 single-version unpublish/deprecate spelling), any other
// body is a no-op.
func (h *Handler) serveRevPut(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *Principal, repoKey, name string) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, revBodyMax))
	if err == nil && len(raw) > 0 {
		var body map[string]any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if decErr := dec.Decode(&body); decErr == nil && body != nil {
			if _, hasVersions := body["versions"]; hasVersions {
				h.docMu.Lock()
				applyErr := h.applyRevDocumentLocked(ctx, p, repoKey, name, body)
				h.docMu.Unlock()
				if applyErr != nil && !errors.Is(applyErr, repo.ErrNodeNotFound) {
					h.writeServiceError(w, applyErr)
					return
				}
				// ErrNodeNotFound: nothing to apply the document to; the
				// fake success stands (npm's own flow never sends a -rev
				// body for a package it has not GET first).
			}
		}
	}
	writeJSONBody(w, http.StatusOK, map[string]any{"ok": "updated package"})
}

// applyRevDocumentLocked is the -rev PUT's document replacement under docMu:
// load, replace versions/dist-tags (normalizing tarball references), save.
// repo.ErrNodeNotFound means "nothing to apply to" — the caller treats it as
// a no-op, not a failure.
func (h *Handler) applyRevDocumentLocked(ctx context.Context, p *Principal,
	repoKey, name string, body map[string]any) error {
	doc, _, _, err := h.loadPackument(ctx, p, repoKey, name)
	if err != nil {
		return err
	}
	merged := applyRevDocument(doc, body, name, h.clock)
	if merged == nil {
		return nil
	}
	bumpRev(merged)
	return h.savePackument(ctx, p, repoKey, name, merged)
}

// unpublishWhole removes the package: every tarball node under <name>/-/,
// then the packument node. Missing package = 404.
func (h *Handler) unpublishWhole(ctx context.Context, w http.ResponseWriter,
	p *Principal, repoKey, name string) {
	if _, _, _, err := h.loadPackument(ctx, p, repoKey, name); err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) {
			writeError(w, http.StatusNotFound, "package not found: "+name)
			return
		}
		h.writeServiceError(w, err)
		return
	}
	// Folder delete recursion drops every tarball (and tolerates a missing
	// folder row when the package somehow holds none).
	if err := h.svc.Delete(ctx, p, repoKey, name+"/"+tarballDir); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
		h.writeServiceError(w, err)
		return
	}
	if err := h.svc.Delete(ctx, p, repoKey, packumentPath(name)); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// serveTarballRev handles DELETE /<name>/-/<file>/-rev/<rev>: remove the
// version whose tarball sits at that path. The packument entry goes first if
// still present (the npm 10 flow already removed it via the -rev PUT; a
// direct curl DELETE without that step must still clean it), then the node.
func (h *Handler) serveTarballRev(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *Principal, repoKey, name, file string) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", "DELETE")
		writeError(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported here")
		return
	}
	nodePath := name + "/" + tarballDir + file

	h.docMu.Lock()
	defer h.docMu.Unlock()
	doc, _, _, err := h.loadPackument(ctx, p, repoKey, name)
	switch {
	case err == nil:
		// Find the version owning this tarball path (matching the stored
		// dist reference keeps scoped filenames honest without re-parsing
		// "<name>-<version>.tgz").
		versions := versionsOf(doc)
		removed := false
		for v, mv := range versions {
			dist := distOf(mapOf(mv))
			if dist == nil {
				continue
			}
			if versionDistTarball(name, v, dist) == nodePath {
				removed = removeVersion(doc, v, h.clock) || removed
			}
		}
		if removed {
			bumpRev(doc)
			if err := h.savePackument(ctx, p, repoKey, name, doc); err != nil {
				h.writeServiceError(w, err)
				return
			}
		}
	case errors.Is(err, repo.ErrNodeNotFound):
		// No packument: still honor the node delete below (a stale tarball
		// without its document is garbage, not a 404).
	default:
		h.writeServiceError(w, err)
		return
	}

	if err := h.svc.Delete(ctx, p, repoKey, nodePath); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
