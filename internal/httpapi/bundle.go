// The release-bundle REST family — L026-6 (D08-R04) rewrote the query
// face onto the live-verified wire of docs/reverse/release-bundle.md
// §10.5 (p01-p12, p59): the names map, the always-200 versions face, the
// descriptor GET/HEAD with their three distinct 404 message families, the
// status string, the artifacts face, and the two DELETE projections
// (TARGET vs SOURCE). The write entrances live in their own files: the
// AQL assembly probe (release_bundle_assembly.go, R01), the v2-signing
// transaction error face (release_bundle_transaction.go, R02), the
// Distribution store arm chain (release_bundle_store.go, R03), the
// config/fat_manifest management face (release_bundle_config.go, R05) and
// the v2 read plane (release_bundle_v2.go). POST /api/release/bundle no
// longer creates records — the reference face is an AQL assembly probe;
// records enter only through the store face's signing chain, which no
// BinFlow instance carries (the domain service keeps Create for internal
// callers and tests).
//
// Gates: the route doors demand authentication everywhere; the read
// faces' REAL decision stays the body/path-dependent dual gate
// (CapSystemRead ∨ the Any Distribution channel over the bundle name)
// inside the handlers (the permissions family-4 precedent); reads never
// consult the feature gate (D1 — records already written stay visible on
// a locked instance).
//
// The ?type= projection (§2.2, locked design): the reference default is
// TARGET — the receiving/edge-side records — and BinFlow is a source-only
// instance (architecture §26.2), so ?type=source answers the SOURCE
// records BinFlow actually keeps and every other value (absent, target,
// garbage) answers the TARGET projection, which is honestly empty
// forever: the names map collapses to {}, the versions face to [], the
// single faces to their 404 families. HEAD ignores ?type= (恒 SOURCE) and
// the main DELETE ignores it too (恒 TARGET — always the 404); the
// /source/ DELETE is 恒 SOURCE.

package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/bundle"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// BundleAddonID is the feature slot's identifier (slots.go's 20th slot).
const BundleAddonID = "release-bundle"

// bundleMaxBodyBytes caps the family's request documents (the build
// family's wire-size floor: manifests and AQL bodies are KB-scale
// metadata, and an unbounded body is a DoS).
const bundleMaxBodyBytes = 8 << 20

// bundleDetailURI is the family's self-addressing prefix.
const bundleDetailURI = "/api/release/bundles/"

// bundleFeatureMinTier derives the slot's own tier floor from the
// assembled registry (pro when no registry carries the slot — the
// slots.go value). The REST gate and the data-plane seam both route
// through this one lookup so the two verdicts cannot drift.
func bundleFeatureMinTier(deps Deps) license.Tier {
	minTier := license.TierPro
	if deps.Addons != nil {
		if a, ok := deps.Addons.ByID(BundleAddonID); ok {
			minTier = a.MinTier
		}
	}
	return minTier
}

// requireBundleAddon is the write-verb feature gate (the requireWebhook
// precedent): RequireAddon over the slot's own MinTier so the tier matrix
// stays slots.go's single source. false means the refusal was rendered.
func (s *Server) requireBundleAddon(w http.ResponseWriter, r *http.Request) bool {
	return s.RequireAddon(w, r, BundleAddonID, bundleFeatureMinTier(s.deps))
}

// refuseBundleProjects answers the honest 400 for the project family the
// platform does not carry (the build family's refusal verbatim in spirit;
// the reference's own projectKey arms are the store face's 404 passthrough
// and the assembly face's unprobed ignore).
func refuseBundleProjects(w http.ResponseWriter, r *http.Request) bool {
	q := r.URL.Query()
	for _, name := range []string{"project", "projectKey"} {
		if strings.TrimSpace(q.Get(name)) != "" {
			writeError(w, http.StatusBadRequest,
				"projects are not supported in BinFlow")
			return true
		}
	}
	return false
}

// writeBundleError maps the service faces onto the wire: 400 carries the
// validator's own wording, 403 the wrapped forbidden text; the 404s are
// FACE-specific (three message families, §10.5) so each handler renders
// its own before falling here. The data-plane entitlement sentinel answers
// the headerless 403 (defense in depth for internal callers).
func (s *Server) writeBundleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, bundle.ErrFeatureOff):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, bundle.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, bundle.ErrInvalidBundle):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.log.Error("httpapi: release bundle operation failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "release bundle operation failed")
	}
}

// bundleTypeIsSource reads the ?type= projection: only the literal
// "source" selects the SOURCE records; the reference's TARGET default and
// every other value select the (empty-forever) TARGET projection.
func bundleTypeIsSource(r *http.Request) bool {
	return strings.TrimSpace(r.URL.Query().Get("type")) == "source"
}

// handleBundleList serves GET /api/release/bundles (p01/p02/p50): the
// names map — {"bundles": {<name>: [<versions>...]}} — filtered to the
// caller's visible set server-side (zero leakage). The row shape inside
// the map was never captured (empty-state probes only): version strings,
// the minimal reading of §1's "名 → 版本数组的 map", registered as
// spec-pending.
func (s *Server) handleBundleList(w http.ResponseWriter, r *http.Request) {
	if s.bundlesUnavailable(w) {
		return
	}
	names := map[string][]string{}
	if bundleTypeIsSource(r) {
		rows, err := s.bundles.ListBundleNames(r.Context(), principalFrom(r.Context()))
		if err != nil {
			s.writeBundleError(w, err)
			return
		}
		for _, row := range rows {
			vers, verr := s.bundles.ListBundleVersions(r.Context(), principalFrom(r.Context()), row.Name)
			if verr != nil {
				s.writeBundleError(w, verr)
				return
			}
			names[row.Name] = bundleVersionStrings(vers)
		}
	}
	s.countBundleGet("names")
	writeJSONBody(w, http.StatusOK, struct {
		Bundles map[string][]string `json:"bundles"`
	}{Bundles: names})
}

// bundleVersionStrings projects the store rows onto the wire's version
// strings.
func bundleVersionStrings(rows []*metadata.BundleVersion) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Version)
	}
	return out
}

// handleBundleVersions serves GET /api/release/bundles/{name} (p03): the
// versions array — 200 with [] even for a name that does not exist (the
// face has no 404 arm at all), the TARGET projection likewise always [].
func (s *Server) handleBundleVersions(w http.ResponseWriter, r *http.Request, name string) {
	if s.bundlesUnavailable(w) {
		return
	}
	versions := []string{}
	if bundleTypeIsSource(r) {
		rows, err := s.bundles.ListBundleVersions(r.Context(), principalFrom(r.Context()), name)
		if err != nil {
			// An invisible name is indistinguishable from a missing one —
			// the zero-leak law turns every refusal into the empty answer.
			if !errors.Is(err, bundle.ErrForbidden) {
				s.writeBundleError(w, err)
				return
			}
		} else {
			versions = bundleVersionStrings(rows)
		}
	}
	s.countBundleGet("versions")
	writeJSONBody(w, http.StatusOK, struct {
		Versions []string `json:"versions"`
	}{Versions: versions})
}

// bundleDescriptor is the single GET face's document: the record's header
// fields in the reference's own vocabulary (name/version/status/created/
// signature/type — §2.1's AQL releases-domain projection) plus the
// manifest rows; the fields the minimal face does not carry (storing_repo,
// keep, source_service_id) are omitted, never faked. The success shape is
// BinFlow-native (only the 404 arm was ever captured live — registered
// spec-pending).
type bundleDescriptor struct {
	Name      string                 `json:"name"`
	Version   string                 `json:"version"`
	Status    string                 `json:"status"`
	Created   string                 `json:"created"`
	CreatedBy string                 `json:"created_by"`
	Signature string                 `json:"signature"`
	Type      string                 `json:"type"`
	Artifacts []bundleDescriptorItem `json:"artifacts"`
}

type bundleDescriptorItem struct {
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// renderBundleDescriptor assembles the single face's document.
func renderBundleDescriptor(b *metadata.Bundle, items []*metadata.BundleItem) bundleDescriptor {
	d := bundleDescriptor{
		Name: b.Name, Version: b.Version, Status: b.State,
		Created: b.CreatedAt, CreatedBy: b.CreatedBy,
		Signature: b.Signature, Type: bundle.BundleTypeSource,
		Artifacts: make([]bundleDescriptorItem, 0, len(items)),
	}
	for _, it := range items {
		d.Artifacts = append(d.Artifacts, bundleDescriptorItem{
			Repo: it.RepoKey, Path: it.Path, Sha256: it.Sha256, Size: it.Size})
	}
	return d
}

// handleBundleGet serves GET /api/release/bundles/{name}/{version}
// (p04/p12/p59): the descriptor document; the 404 family's base wording
// ("Bundle not found") answers the TARGET projection and every missing
// record. ?format=jws is validated AFTER the lookup (p12: a missing
// record answers 404, not the format's refusal); a found record with the
// signed form requested answers the honest 400 — BinFlow carries no JWS.
func (s *Server) handleBundleGet(w http.ResponseWriter, r *http.Request, name, version string) {
	if s.bundlesUnavailable(w) {
		return
	}
	if !bundleTypeIsSource(r) {
		writeError(w, http.StatusNotFound, "Bundle not found") // p04: the TARGET projection is empty
		return
	}
	b, items, err := s.bundles.GetBundleWithItems(r.Context(), principalFrom(r.Context()), name, version)
	if err != nil {
		if errors.Is(err, metadata.ErrBundleNotFound) {
			writeError(w, http.StatusNotFound, "Bundle not found") // p04
			return
		}
		s.writeBundleError(w, err)
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("format")) != "" {
		writeError(w, http.StatusBadRequest,
			"the format parameter is not supported in BinFlow (v2 signed release bundles are a future face)")
		return
	}
	s.countBundleGet("detail")
	d := renderBundleDescriptor(b, items)
	uri := bundleDetailURI + url.PathEscape(name) + "/" + url.PathEscape(version)
	writeJSONBody(w, http.StatusOK, struct {
		URI  string           `json:"uri"`
		Info bundleDescriptor `json:"info"`
	}{URI: uri, Info: d})
}

// handleBundleHead serves HEAD /api/release/bundles/{name}/{version}
// (p07): the bodyless checksum probe — 404 answers BARE (no envelope
// bytes at all), 200 carries X-Checksum-Sha256 over the descriptor
// document's bytes (the GET face's own JSON). 恒 SOURCE: ?type= is ignored.
func (s *Server) handleBundleHead(w http.ResponseWriter, r *http.Request, name, version string) {
	if s.bundlesUnavailable(w) {
		return
	}
	b, items, err := s.bundles.GetBundleWithItems(r.Context(), principalFrom(r.Context()), name, version)
	if err != nil {
		w.WriteHeader(http.StatusNotFound) // p07: bodyless by definition
		return
	}
	doc, err := json.Marshal(renderBundleDescriptor(b, items))
	if err != nil {
		s.log.Error("httpapi: release bundle descriptor marshal failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "release bundle operation failed")
		return
	}
	sum := sha256.Sum256(doc)
	w.Header().Set("X-Checksum-Sha256", hex.EncodeToString(sum[:]))
	w.WriteHeader(http.StatusOK) // bodyless by definition
}

// handleBundleStatus serves GET /api/release/bundles/{name}/{version}/
// status (p05): the state string alone — and the 404 family's COLON form
// ("<name>:<version> not found"), a different message family from the
// descriptor's "Bundle not found".
func (s *Server) handleBundleStatus(w http.ResponseWriter, r *http.Request, name, version string) {
	if s.bundlesUnavailable(w) {
		return
	}
	if !bundleTypeIsSource(r) {
		writeError(w, http.StatusNotFound, name+":"+version+" not found") // p05
		return
	}
	b, err := s.bundles.GetBundle(r.Context(), principalFrom(r.Context()), name, version)
	if err != nil {
		if errors.Is(err, metadata.ErrBundleNotFound) {
			writeError(w, http.StatusNotFound, name+":"+version+" not found") // p05
			return
		}
		s.writeBundleError(w, err)
		return
	}
	s.countBundleGet("status")
	writeJSONBody(w, http.StatusOK, b.State)
}

// handleBundleArtifacts serves GET /api/release/bundles/{name}/{version}/
// artifacts (p08): the manifest face — 404 carries the descriptor's base
// family wording. The success shape (the reference's
// ReleaseBundleArtifactsModel) was never captured: BinFlow answers its own
// manifest rows, registered spec-pending.
func (s *Server) handleBundleArtifacts(w http.ResponseWriter, r *http.Request, name, version string) {
	if s.bundlesUnavailable(w) {
		return
	}
	if !bundleTypeIsSource(r) {
		writeError(w, http.StatusNotFound, "Bundle not found") // p08
		return
	}
	_, items, err := s.bundles.GetBundleWithItems(r.Context(), principalFrom(r.Context()), name, version)
	if err != nil {
		if errors.Is(err, metadata.ErrBundleNotFound) {
			writeError(w, http.StatusNotFound, "Bundle not found") // p08
			return
		}
		s.writeBundleError(w, err)
		return
	}
	s.countBundleGet("artifacts")
	rows := make([]bundleDescriptorItem, 0, len(items))
	for _, it := range items {
		rows = append(rows, bundleDescriptorItem{
			Repo: it.RepoKey, Path: it.Path, Sha256: it.Sha256, Size: it.Size})
	}
	writeJSONBody(w, http.StatusOK, struct {
		Artifacts []bundleDescriptorItem `json:"artifacts"`
	}{Artifacts: rows})
}

// handleBundleDeleteTarget serves DELETE /api/release/bundles/{name}/
// {version} (p09): 恒 TARGET — the TARGET projection is empty forever on a
// source-only instance, so the face's whole reachable behavior is the
// "Bundle not found" 404.
func (s *Server) handleBundleDeleteTarget(w http.ResponseWriter, _ *http.Request, _, _ string) {
	writeError(w, http.StatusNotFound, "Bundle not found") // p09
}

// handleBundleDeleteSource serves DELETE /api/release/bundles/source/
// {name}/{version} (p10): the SOURCE projection — the family's THIRD 404
// message ("Release bundle not found") for a missing record. A found
// record hits an honest 500: the metadata BundleStore carries no delete
// seam yet (registered — the gap belongs to a metadata ticket, not this
// face).
func (s *Server) handleBundleDeleteSource(w http.ResponseWriter, r *http.Request, name, version string) {
	if s.bundlesUnavailable(w) {
		return
	}
	if _, err := s.bundles.GetBundle(r.Context(), principalFrom(r.Context()), name, version); err != nil {
		if errors.Is(err, metadata.ErrBundleNotFound) {
			writeError(w, http.StatusNotFound, "Release bundle not found") // p10
			return
		}
		s.writeBundleError(w, err)
		return
	}
	s.log.Error("httpapi: release bundle source delete reached but the metadata store carries no bundle delete seam",
		"bundle", name+"/"+version)
	writeError(w, http.StatusInternalServerError, "release bundle operation failed")
}

// bundlesUnavailable renders the honest 503 of a metadata-less unit stack
// (the build family's posture).
func (s *Server) bundlesUnavailable(w http.ResponseWriter) bool {
	if s.bundles == nil {
		writeError(w, http.StatusServiceUnavailable, "release bundles are not available on this instance")
		return true
	}
	return false
}

// splitBundlePath splits the /api/release/bundles family's tail after the
// prefix into the DECODED segments: one segment addresses the versions
// face, two the descriptor/HEAD/DELETE faces, three either a literal
// third segment ("status"/"artifacts") or the SOURCE delete face's
// leading "source"; anything else (empty segment, deeper tail, a
// non-literal third segment, "source" with a non-DELETE verb) is not the
// family's grammar — routed=false leaves the E-26 404 to the caller. err
// reports a segment whose percent-escaping cannot decode (the honest
// 400 — names and versions may legally carry ':' and other escaped
// characters).
func splitBundlePath(rest, prefix string) (segs []string, routed bool, err error) {
	tail := strings.TrimPrefix(rest, prefix)
	if tail == "" || strings.HasSuffix(tail, "/") {
		return nil, false, nil
	}
	parts := strings.Split(tail, "/")
	if len(parts) > 3 {
		return nil, false, nil
	}
	for _, p := range parts {
		if p == "" {
			return nil, false, nil
		}
		dec, uerr := url.PathUnescape(p)
		if uerr != nil {
			return nil, true, uerr
		}
		segs = append(segs, dec)
	}
	if len(parts) == 3 && parts[2] != "status" && parts[2] != "artifacts" && parts[0] != "source" {
		return nil, false, nil
	}
	return segs, true, nil
}
