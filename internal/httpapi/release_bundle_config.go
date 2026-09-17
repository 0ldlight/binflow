// L026-6 (D08-R05): the release-bundle management face — GET/PUT
// /api/release/bundles/config (the incomplete-bundle cleanup period,
// release-bundle.md §10.6 / §1 rows 16-17, wire p06/p53) and GET
// /api/release/fat_manifest_content/{path} (the v2 evidence-file viewer's
// validation arm, wire p47/p55). Both are admin faces whose non-admin
// answer is the family's bare "Forbidden" envelope (§10.7).

package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// The management face's frozen copy (release-bundle.md §10.6 — do not
// reword).
const (
	// msgRBConfigUpdated is PUT's 202 body (§1 row 16; the probe captured
	// the copy, the content type is unpinned — text/plain rides the
	// platform's plain-message posture, registered in the report).
	msgRBConfigUpdated = "Successfully updated release bundles config"
	// msgRBFatManifestReject answers a fat-manifest path that is not a
	// list.manifest.json (p47 verbatim, the caller's whole path echoed in
	// brackets).
	msgRBFatManifestReject = "Fat manifest content view is only allowed on list.manifest.json files. Got: [%s]"
	// fatManifestSuffix is the one file name the face serves.
	fatManifestSuffix = "list.manifest.json"
)

// handleBundleConfigGet serves GET /api/release/bundles/config: the cleanup
// period hours (the factory default 720 until a PUT changes it, p06).
func (s *Server) handleBundleConfigGet(w http.ResponseWriter, r *http.Request) {
	if !bundleAdminGate(w, r) {
		return // p53: the bare "Forbidden" envelope
	}
	if s.bundlesUnavailable(w) {
		return
	}
	writeJSONBody(w, http.StatusOK, struct {
		IncompleteCleanupPeriodHours int `json:"incompleteCleanupPeriodHours"`
	}{IncompleteCleanupPeriodHours: s.bundles.CleanupPeriodHours()})
}

// handleBundleConfigPut serves PUT /api/release/bundles/config: the addon
// gate (a write face — D4), then the body {"incompleteCleanupPeriodHours":
// <n>}. The success arm renders the frozen copy under 202 (§1 row 16); the
// body-validation arms were never probed — the honest refusal below is
// BinFlow-native (registered in the report, not a spec copy).
func (s *Server) handleBundleConfigPut(w http.ResponseWriter, r *http.Request) {
	if !bundleAdminGate(w, r) {
		return
	}
	if !s.requireBundleAddon(w, r) {
		return
	}
	if s.bundlesUnavailable(w) {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bundleMaxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "release bundles config body could not be read: "+err.Error())
		return
	}
	var wire struct {
		IncompleteCleanupPeriodHours *int `json:"incompleteCleanupPeriodHours"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		writeError(w, http.StatusBadRequest, "release bundles config body is not valid JSON: "+err.Error())
		return
	}
	if wire.IncompleteCleanupPeriodHours == nil || *wire.IncompleteCleanupPeriodHours < 0 {
		writeError(w, http.StatusBadRequest, "incompleteCleanupPeriodHours must be a non-negative integer")
		return
	}
	s.bundles.SetCleanupPeriodHours(*wire.IncompleteCleanupPeriodHours)
	writePlainText(w, http.StatusAccepted, msgRBConfigUpdated)
}

// handleBundleFatManifest serves GET /api/release/fat_manifest_content/
// {path}: the admin-gated v2 evidence viewer. The probed arm is the
// path-shape rejection (p47); the list.manifest.json arm needs a v2 record
// no BinFlow instance carries — the honest 404 below keeps the family's
// generic wording (UNKNOWN sub-arm, registered).
func (s *Server) handleBundleFatManifest(w http.ResponseWriter, r *http.Request, path string) {
	if !bundleAdminGate(w, r) {
		return // p55: the bare "Forbidden" envelope
	}
	if !s.requireBundleAddon(w, r) {
		return
	}
	if !strings.HasSuffix(path, fatManifestSuffix) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(msgRBFatManifestReject, path)) // p47
		return
	}
	writeError(w, http.StatusNotFound, "Not Found")
}
