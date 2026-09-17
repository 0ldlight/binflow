// L026-6 (D08-R05 companion): the v2 Release Lifecycle read face
// (/api/v2/release_bundle/*, underscore-singular — release-bundle.md
// §10.1/§10.6, wire p13-p26/p60) plus the /api/v2/audit family (p22/p26).
// Every probed arm is an EMPTY-STATE or ERROR arm: BinFlow is a
// source-only instance with no v2 records (§10.1's ruling — the v2 plane
// is a parallel read face, its write faces belong to the Distribution
// service side), so the faces answer their frozen empty envelopes and
// 404/400 families verbatim; the populated-row shapes were never captured
// and stay unimplemented rather than invented.

package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// The v2 face's frozen copy (release-bundle.md §10.6 — do not reword).
const (
	// msgV2NotFound is the family's catch-all 404 (p13: the bare records
	// path; every unrouted subpath takes it too).
	msgV2NotFound = "Not Found"
	// msgV2NameRule is the name-validator copy the received single GET
	// answers REGARDLESS of the name's validity (p20/p23 — the face has no
	// GET route and every request falls into the validator; §10.6 待验证
	// #6).
	msgV2NameRule = "Bad request: [`Release Bundle name must begin with a {letter | _ | digit} and consist of {letters | _ | . | - | digits}`]"
	// msgV2RecordsStore / msgV2JFDSStore are the two v2 store keys the
	// faces leak (p16: release-bundles-v2; p60: the DELETE face's
	// release-bundles-v2-jfds — the jfds suffix rides verbatim, its exact
	// provisioning semantics are the spec's 中-confidence item).
	msgV2RecordsStore = "release-bundles-v2"
	msgV2JFDSStore    = "release-bundles-v2-jfds"
	// v2RecordEvidenceFile is the evidence-document file name the records
	// single GET leaks in its path-shaped 404 (p17/p24).
	v2RecordEvidenceFile = "release-bundle.json.evd"
	// v2RecordsDefaultLimit is the records-per-name envelope's factory
	// limit echo (p18).
	v2RecordsDefaultLimit = 1000
)

// handleV2BundleNames serves GET /api/v2/release_bundle/names (p14):
// 200 {"release_bundles": []} — BinFlow carries no v2 records, the honest
// empty list.
func (s *Server) handleV2BundleNames(w http.ResponseWriter, _ *http.Request) {
	writeJSONBody(w, http.StatusOK, struct {
		ReleaseBundles []struct{} `json:"release_bundles"`
	}{ReleaseBundles: []struct{}{}})
}

// handleV2BundleReceived serves GET /api/v2/release_bundle/received
// (p15): the received-set summary envelope.
func (s *Server) handleV2BundleReceived(w http.ResponseWriter, _ *http.Request) {
	writeJSONBody(w, http.StatusOK, struct {
		ReleaseBundles []struct{} `json:"release_bundles"`
		Total          int        `json:"total"`
	}{ReleaseBundles: []struct{}{}, Total: 0})
}

// handleV2BundleReceivedVersions serves GET /api/v2/release_bundle/
// received/{name} (p19): the per-name version summary — a name with no v2
// records answers the empty envelope, never a 404.
func (s *Server) handleV2BundleReceivedVersions(w http.ResponseWriter, _ *http.Request) {
	writeJSONBody(w, http.StatusOK, struct {
		Versions []struct{} `json:"versions"`
		Total    int        `json:"total"`
	}{Versions: []struct{}{}, Total: 0})
}

// handleV2BundleReceivedGet serves GET /api/v2/release_bundle/received/
// {name}/{version} (p20/p23): 恒 400 — the face has no GET route on the
// reference either; every request lands in the name validator, whose copy
// does not echo the offending name.
func (s *Server) handleV2BundleReceivedGet(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusBadRequest, msgV2NameRule)
}

// handleV2BundleReceivedDelete serves DELETE /api/v2/release_bundle/
// received/{name}/{version} (p60): the jfds-store record 404 — no received
// v2 record can exist on a source-only instance.
func (s *Server) handleV2BundleReceivedDelete(w http.ResponseWriter, r *http.Request, name, version string) {
	if !s.requireBundleAddon(w, r) {
		return // a write face of the family — the D4 gate
	}
	writeError(w, http.StatusNotFound,
		"Record not found, repository: "+msgV2JFDSStore+", name: "+name+", version: "+version)
}

// handleV2BundleRecordsList serves GET /api/v2/release_bundle/records/
// {name} (p18): the pagination-echo envelope — three keys after the rows
// (limit/offset echo the factory values; the ?limit=/?offset= arms are
// unprobed and stay at the factory echo, registered).
func (s *Server) handleV2BundleRecordsList(w http.ResponseWriter, _ *http.Request) {
	writeJSONBody(w, http.StatusOK, struct {
		ReleaseBundles []struct{} `json:"release_bundles"`
		Total          int        `json:"total"`
		Limit          int        `json:"limit"`
		Offset         int        `json:"offset"`
	}{ReleaseBundles: []struct{}{}, Total: 0, Limit: v2RecordsDefaultLimit, Offset: 0})
}

// handleV2BundleRecordsGet serves GET /api/v2/release_bundle/records/
// {name}/{version} (p17/p24): the path-shaped 404 that leaks the v2
// storage layout's three-segment path and the .evd evidence file name.
func (s *Server) handleV2BundleRecordsGet(w http.ResponseWriter, _ *http.Request, name, version string) {
	writeError(w, http.StatusNotFound,
		"Path not found: "+msgV2RecordsStore+"/"+name+"/"+version+"/"+v2RecordEvidenceFile)
}

// handleV2BundleStatus serves GET /api/v2/release_bundle/statuses/{name}/
// {version} (p16/p25): the record 404 over the plain v2 store key.
func (s *Server) handleV2BundleStatus(w http.ResponseWriter, _ *http.Request, name, version string) {
	writeError(w, http.StatusNotFound,
		"Record not found, repository: "+msgV2RecordsStore+", name: "+name+", version: "+version)
}

// v2AuditRequiredFields is POST /api/v2/audit's seven-field validation
// list in the probed order (p26) — a body missing any of them answers the
// joined copy. The audit's WRITE side belongs to the lifecycle service;
// BinFlow implements the validation arm only.
var v2AuditRequiredFields = []string{
	"event_summary", "event_status", "release_bundle_version",
	"subject_reference", "release_bundle_name", "subject_type", "created_by",
}

// handleV2AuditPost serves POST /api/v2/audit (p26): the seven-field
// null-check validation. A body that passes validation has no captured
// success shape on the wire — the honest E-26 refusal answers it (UNKNOWN,
// registered in the report).
func (s *Server) handleV2AuditPost(w http.ResponseWriter, r *http.Request) {
	if !s.requireBundleAddon(w, r) {
		return // a write face of the family — the D4 gate
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bundleMaxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "audit body could not be read: "+err.Error())
		return
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		if msg, ok := jacksonUnrecognizedToken(raw); ok {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		writeError(w, http.StatusBadRequest, "audit body is not valid JSON: "+err.Error())
		return
	}
	var missing []string
	for _, f := range v2AuditRequiredFields {
		if _, ok := body[f]; !ok {
			// p26: each field spec rides backtick-wrapped inside the
			// bracketed list.
			missing = append(missing, "`'"+f+"' must be non-null`")
		}
	}
	if len(missing) > 0 {
		writeError(w, http.StatusBadRequest, "Bad request: ["+strings.Join(missing, ", ")+"]")
		return
	}
	notImplemented(w, "/binflow/api/v2/audit")
}

// handleV2AuditMethodNotAllowed answers every non-POST verb on
// /api/v2/audit (p22: GET → 405) — the family registers the path POST-only.
func handleV2AuditMethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
}

// splitV2Tail splits a v2 release_bundle sub-tail (after the family
// prefix) into decoded segments; more than two, an empty segment or an
// undecodable one is not the family's grammar — routed=false sends the
// caller to the family catch-all 404.
func splitV2Tail(tail string) (segs []string, routed bool, err error) {
	if tail == "" {
		return nil, false, nil
	}
	for _, p := range strings.Split(tail, "/") {
		if p == "" {
			return nil, false, nil
		}
		dec, uerr := url.PathUnescape(p)
		if uerr != nil {
			return nil, true, uerr
		}
		segs = append(segs, dec)
	}
	if len(segs) > 2 {
		return nil, false, nil
	}
	return segs, true, nil
}
