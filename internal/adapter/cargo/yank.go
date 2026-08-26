package cargo

import (
	"context"
	"net/http"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The yank family (spec section 6): yank is NOT a delete — the .crate
// node stays, only its crate.yanked property appears (and disappears on
// unyank), and the index rewrite flips the row's boolean. Already-locked
// Cargo.lock builds keep downloading; new resolutions skip the version.
//
// TL-6: an unknown crate or version answers 404 + the envelope. The
// write-verb permissions (DELETE for yank, write for unyank) are the
// middleware's — the verb itself carries them through contentAction.

// serveYank implements DELETE api/v1/crates/{n}/{v}/yank and PUT
// …/unyank: flag = true adds the property, false removes it.
func (h *Handler) serveYank(ctx context.Context, w http.ResponseWriter, p *repo.Principal, repoKey, name, version string, flag bool) {
	path := cratePath(name, version)
	rc, _, err := h.svc.Get(ctx, p, repoKey, path)
	if err != nil {
		h.writeError(w, err, repoKey, path)
		return
	}
	_ = rc.Close() //nolint:errcheck // existence probe only
	if h.props == nil {
		writeEnvelope(w, http.StatusInternalServerError, "property store is not wired")
		return
	}
	var storeErr error
	if flag {
		storeErr = h.props.Merge(ctx, repoKey, path, map[string][]string{propYanked: {"true"}})
	} else {
		storeErr = h.props.Delete(ctx, repoKey, path, []string{propYanked})
	}
	if storeErr != nil {
		writeEnvelope(w, http.StatusInternalServerError, "update the yank property: "+storeErr.Error())
		return
	}
	if err := h.rewriteIndex(ctx, p, repoKey, name); err != nil {
		writeEnvelope(w, http.StatusInternalServerError, "index rewrite: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, []byte(`{"ok":true}`))
}
