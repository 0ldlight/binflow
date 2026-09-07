// The release-bundle REST family (M17 T-513, FR-153.1 / ADR-0046 decision
// 2 + Errata ① E5, wire frozen by docs/reverse/release-bundle.md §1): the
// create POST /api/release/bundle — the official AQL-assembly body
// degraded to the EXPLICIT-MANIFEST subset (soft-seam ⑥: {name, version,
// artifacts[]} replaces {uuid, signature, aql}; the aql/signature/uuid
// channels are refused with pointed 400s, never silently dropped) — and
// the source-side query family: names GET /api/release/bundles, versions
// GET /api/release/bundles/{name}, the descriptor GET (plus the HEAD
// checksum probe the E5 anchor pins, X-Checksum-Sha256 over the descriptor
// bytes) and the status string GET. The transaction/store/config/
// fat_manifest families stay UNROUTED (v2 signing and the Distribution
// plane are the exit-② face); so does DELETE (the minimal face ships no
// delete verb — E-26 404).
//
// Gates: the route doors demand authentication everywhere, CapSystemWrite
// on the create; the read faces' REAL decision is the body/path-dependent
// dual gate (CapSystemRead ∨ the Any Distribution channel over the bundle
// name), so their handlers own it (the permissions family-4 precedent).
// The feature slot rides the write verb's first line (RequireAddon —
// community 403 + the license header, the addons.disabled breaker's
// no-header refusal); reads never consult the gate (D1 — records already
// written stay visible on a locked instance).
//
// The BinFlow-native parameter rulings this file owns: ?type= is refused
// (BinFlow keeps SOURCE records only — honoring the official TARGET
// default would answer nothing forever, and silently flipping the default
// would lie); ?format=jws is refused (the v2 signed form is face-out);
// ?projectKey= is refused (no projects domain, the build family's
// posture).

package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/bundle"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// BundleAddonID is the feature slot's identifier (slots.go's 20th slot).
const BundleAddonID = "release-bundle"

// bundleMaxBodyBytes caps the create document (the build family's
// wire-size floor: a manifest is KB-scale metadata, and the reference's
// own AQL input ceiling exists precisely because an unbounded body is a
// DoS).
const bundleMaxBodyBytes = 8 << 20

// bundleDetailURI is the family's self-addressing prefix.
const bundleDetailURI = "/api/release/bundles/"

// bundleConflictBody is the 409 arm's frozen wire form (ADR-0046 Errata ②
// E6, verbatim — the product's generic ErrorResponse shape, NOT the
// platform errors[] envelope): the reason enum (UNMATCHING_SIGNATURES /
// ALREADY_COMPLETED) stays internal, never on the wire.
const bundleConflictMessage = "Bundle already exists"

// writeBundleConflict emits the verbatim flat 409 body:
// {"status":409,"message":"Bundle already exists"}.
func writeBundleConflict(w http.ResponseWriter) {
	body, err := json.Marshal(struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
	}{Status: http.StatusConflict, Message: bundleConflictMessage})
	if err != nil {
		// unreachable: two flat scalars always marshal
		body = []byte(`{"status":409,"message":"Bundle already exists"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_, _ = w.Write(body)
}

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
// platform does not carry (the build family's refusal verbatim in spirit).
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

// refuseBundleType answers the honest 400 for ?type= (BinFlow keeps
// SOURCE records only; the official family's TARGET default is a
// two-instance topology this single-instance domain does not carry).
func refuseBundleType(w http.ResponseWriter, r *http.Request) bool {
	if strings.TrimSpace(r.URL.Query().Get("type")) != "" {
		writeError(w, http.StatusBadRequest,
			"the type parameter is not supported in BinFlow (release bundles are source-side records only on this single instance)")
		return true
	}
	return false
}

// writeBundleError maps the service faces onto the wire: the 409 arm
// renders the frozen flat body BEFORE the envelope mapping; 400 carries
// the validator's own wording; 403 the wrapped forbidden text; 404 the
// family's wording ("Release Bundle not found" — not a spec-frozen
// literal, the spec pins only the HEAD face's bodyless 404); the
// data-plane entitlement sentinel answers the headerless 403 (the REST
// seam's richer refusal already ran — this is the internal-caller arm).
func (s *Server) writeBundleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, bundle.ErrBundleConflict):
		writeBundleConflict(w)
	case errors.Is(err, bundle.ErrFeatureOff):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, bundle.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, bundle.ErrInvalidBundle):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, metadata.ErrBundleNotFound):
		writeError(w, http.StatusNotFound, "Release Bundle not found")
	default:
		s.log.Error("httpapi: release bundle operation failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "release bundle operation failed")
	}
}

// bundleWireRequest is the create body's wire shape (the explicit-manifest
// subset of the official {uuid, signature, aql} form — soft-seam ⑥). The
// three official keys stay declared so their refusal can NAME them; every
// other unknown key is ignored (Go's default — the family's standing
// posture).
type bundleWireRequest struct {
	Name    string                `json:"name"`
	Version string                `json:"version"`
	Items   []bundle.ManifestItem `json:"artifacts"`
	// The refused official channels (AQL assembly and the signature chain
	// are face-out; uuid is a Distribution-side identifier with no M17
	// meaning — silently dropping any of the three would be dishonest).
	AQL       string `json:"aql"`
	Signature string `json:"signature"`
	UUID      string `json:"uuid"`
}

// handleBundleCreate serves POST /api/release/bundle: the conflict
// tri-state — 202 (new) / 200 (same-manifest resume) / 409 (different
// manifest or already complete), the success bodies carrying the
// bundle_path echo (§3.1's 202 shape).
func (s *Server) handleBundleCreate(w http.ResponseWriter, r *http.Request) {
	if s.bundlesUnavailable(w) {
		return
	}
	if !s.requireBundleAddon(w, r) {
		return
	}
	if refuseBundleProjects(w, r) {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bundleMaxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest,
			"release bundle body could not be read (max "+strconv.Itoa(bundleMaxBodyBytes>>20)+"MiB): "+err.Error())
		return
	}
	var wire bundleWireRequest
	if err := json.Unmarshal(raw, &wire); err != nil {
		writeError(w, http.StatusBadRequest, "release bundle body is not valid JSON: "+err.Error())
		return
	}
	// The honest refusals of the official channels this face degrades.
	if strings.TrimSpace(wire.AQL) != "" {
		writeError(w, http.StatusBadRequest,
			"AQL assembly is not supported in BinFlow's release-bundle minimal face; pass an explicit artifacts manifest")
		return
	}
	if strings.TrimSpace(wire.Signature) != "" {
		writeError(w, http.StatusBadRequest,
			"signed release bundles are not supported in BinFlow's release-bundle minimal face (v2 signing is a future face)")
		return
	}
	if strings.TrimSpace(wire.UUID) != "" {
		writeError(w, http.StatusBadRequest,
			"the uuid field belongs to the Distribution-side create form and is not carried by BinFlow's explicit-manifest subset")
		return
	}
	items := make([]*bundle.ManifestItem, 0, len(wire.Items))
	for i := range wire.Items {
		items = append(items, &wire.Items[i])
	}
	outcome, err := s.bundles.Create(r.Context(), principalFrom(r.Context()), &bundle.CreateRequest{
		Name: wire.Name, Version: wire.Version, Items: items,
	})
	if err != nil {
		s.writeBundleError(w, err)
		return
	}
	which := "resumed"
	if outcome.Created {
		which = "created"
	}
	s.observeBundleCreate(which)
	status := http.StatusOK // the resume arm (E6: same digest, not COMPLETE)
	if outcome.Created {
		status = http.StatusAccepted
	}
	path := bundleDetailURI + url.PathEscape(wire.Name) + "/" + url.PathEscape(wire.Version)
	writeJSONBody(w, status, struct {
		BundlePath string `json:"bundle_path"`
	}{BundlePath: path})
}

// bundleNameEntry and bundleVersionEntry are the list faces' row shapes
// (relative URIs, the build family's echo form; the reference's
// BundlesResponse row fields are unverified — release-bundle.md §8 #2 —
// so BinFlow's list rows are the platform's own shape, logged as
// spec-pending in the ticket).
type bundleNameEntry struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

type bundleVersionEntry struct {
	URI     string `json:"uri"`
	Version string `json:"version"`
	State   string `json:"state"`
	Created string `json:"created"`
}

// handleBundleList serves GET /api/release/bundles: every bundle NAME the
// caller may read — the server-side visible-set filter already ran (zero
// leakage, NFR-S81). bundles is never null: a fresh or fully-filtered
// view is [].
func (s *Server) handleBundleList(w http.ResponseWriter, r *http.Request) {
	if s.bundlesUnavailable(w) {
		return
	}
	if refuseBundleType(w, r) {
		return
	}
	rows, err := s.bundles.ListBundleNames(r.Context(), principalFrom(r.Context()))
	if err != nil {
		s.writeBundleError(w, err)
		return
	}
	s.countBundleGet("names")
	body := struct {
		URI     string            `json:"uri"`
		Bundles []bundleNameEntry `json:"bundles"`
	}{URI: "/api/release/bundles", Bundles: make([]bundleNameEntry, 0, len(rows))}
	for _, row := range rows {
		body.Bundles = append(body.Bundles, bundleNameEntry{URI: "/" + row.Name, Name: row.Name})
	}
	writeJSONBody(w, http.StatusOK, body)
}

// handleBundleVersions serves GET /api/release/bundles/{name}: every
// version of one name the caller may read, newest first. A name the
// caller cannot read is indistinguishable from a name that does not exist
// (the zero-leak 404, the build numbers face's law).
func (s *Server) handleBundleVersions(w http.ResponseWriter, r *http.Request, name string) {
	if s.bundlesUnavailable(w) {
		return
	}
	if refuseBundleType(w, r) {
		return
	}
	rows, err := s.bundles.ListBundleVersions(r.Context(), principalFrom(r.Context()), name)
	if err != nil {
		s.writeBundleError(w, err)
		return
	}
	if len(rows) == 0 {
		writeError(w, http.StatusNotFound, "Release Bundle not found")
		return
	}
	s.countBundleGet("versions")
	body := struct {
		URI      string               `json:"uri"`
		Versions []bundleVersionEntry `json:"versions"`
	}{URI: bundleDetailURI + url.PathEscape(name), Versions: make([]bundleVersionEntry, 0, len(rows))}
	for _, row := range rows {
		body.Versions = append(body.Versions, bundleVersionEntry{
			URI: "/" + url.PathEscape(row.Version), Version: row.Version,
			State: row.State, Created: row.Created,
		})
	}
	writeJSONBody(w, http.StatusOK, body)
}

// bundleDescriptor is the single GET face's document: the record's header
// fields in the reference's own vocabulary (name/version/status/created/
// signature/type — §2.1's AQL releases-domain projection) plus the
// manifest rows; the fields the minimal face does not carry (storing_repo,
// keep, source_service_id) are omitted, never faked.
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

// handleBundleGet serves GET /api/release/bundles/{name}/{version}: the
// descriptor document. ?format=jws is refused (the signed form is
// face-out — the honest 400, never a fake JWS).
func (s *Server) handleBundleGet(w http.ResponseWriter, r *http.Request, name, version string) {
	if s.bundlesUnavailable(w) {
		return
	}
	if refuseBundleType(w, r) {
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("format")) != "" {
		writeError(w, http.StatusBadRequest,
			"the format parameter is not supported in BinFlow (v2 signed release bundles are a future face)")
		return
	}
	b, items, err := s.bundles.GetBundleWithItems(r.Context(), principalFrom(r.Context()), name, version)
	if err != nil {
		s.writeBundleError(w, err)
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

// handleBundleHead serves HEAD /api/release/bundles/{name}/{version}: the
// bodyless checksum probe (the E5 anchor's face — X-Checksum-Sha256 over
// the descriptor document's bytes, the GET face's own JSON; 404 answers
// bare, per §1).
func (s *Server) handleBundleHead(w http.ResponseWriter, r *http.Request, name, version string) {
	if s.bundlesUnavailable(w) {
		return
	}
	b, items, err := s.bundles.GetBundleWithItems(r.Context(), principalFrom(r.Context()), name, version)
	if err != nil {
		s.writeBundleError(w, err)
		return
	}
	// The checksum covers the descriptor's marshalled bytes — the same
	// document the GET face answers, so a client can verify what it got.
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
// status: the state string alone (§1's "200 状态字符串" — a JSON string
// body).
func (s *Server) handleBundleStatus(w http.ResponseWriter, r *http.Request, name, version string) {
	if s.bundlesUnavailable(w) {
		return
	}
	b, err := s.bundles.GetBundle(r.Context(), principalFrom(r.Context()), name, version)
	if err != nil {
		s.writeBundleError(w, err)
		return
	}
	s.countBundleGet("status")
	writeJSONBody(w, http.StatusOK, b.State)
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
// face, two the descriptor/HEAD face, three with the literal "status" the
// status face; anything else (empty segment, deeper tail, a non-literal
// third segment) is not the family's grammar — routed=false leaves the
// E-26 404 to the caller. err reports a segment whose percent-escaping
// cannot decode (the honest 400 — names and versions may legally carry
// ':' and other escaped characters).
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
	if len(parts) == 3 && parts[2] != "status" {
		return nil, false, nil
	}
	return segs, true, nil
}
