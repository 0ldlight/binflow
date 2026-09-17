// L026-6 (D08-R01): POST /api/release/bundle — the AQL assembly probe,
// wire-frozen by release-bundle.md §10.2 (p27-p37). The face executes the
// caller's items.find AQL through the SAME search engine /api/search/aql
// runs on (D03, ADR-0043) and answers the manifest-shaped hit list; it
// stores nothing (the store face's 202/200/409 tri-state is the write
// entrance, and it needs a signing chain BinFlow does not carry).

package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// The assembly face's frozen copy (release-bundle.md §10.2 — do not reword).
const (
	msgRBMissingAQL  = "Request is invalid. Missing AQL query"
	msgRBAQLNotItems = "Request is invalid. AQL query should find artifacts (items)"
	aqlItemsFindOpen = "items.find("
)

// bundleAssemblyHit is one manifest row of the hit list — the five-key
// closed set of p30/p36 (urn, sha256, properties, size, pkg_type), field
// order included. properties is ALWAYS present ({} when the artifact
// carries none — the key is never omitted) and its values are arrays.
type bundleAssemblyHit struct {
	URN        string              `json:"urn"`
	Sha256     string              `json:"sha256"`
	Properties map[string][]string `json:"properties"`
	Size       int64               `json:"size"`
	PkgType    string              `json:"pkg_type"`
}

// handleBundleAssemble serves POST /api/release/bundle (§10.2's validation
// chain, ordered): addon gate → the projects refusal (BinFlow-native, the
// build family posture — the official ?projectKey arm is unprobed) → body
// parse → the missing-AQL 400 → the not-items 400 → engine execution with
// the AQL family's own error mapping → the five-key hit list.
//
// ?includeMetaData is accepted and ignored: the reference's MetaDataFilter
// filters by package-type metadata PATHS (generic repositories have none —
// p36/p37 answered identically in both arms); BinFlow's minimal face keeps
// the generic-face behavior everywhere and registers the maven-path arm as
// unprobed.
func (s *Server) handleBundleAssemble(w http.ResponseWriter, r *http.Request) {
	if !s.requireBundleAddon(w, r) {
		return
	}
	if refuseBundleProjects(w, r) {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bundleMaxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "release bundle body could not be read: "+err.Error())
		return
	}
	var wire struct {
		AQL string `json:"aql"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		// The bare-word arm reuses the family's Jackson copy (both probed
		// siblings — open p40, store p46 — answer it); the rest is the
		// platform's honest 400 (this face's own parse error was never
		// probed, sub-arm registered UNKNOWN).
		if msg, ok := jacksonUnrecognizedToken(raw); ok {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		writeError(w, http.StatusBadRequest, "release bundle body is not valid JSON: "+err.Error())
		return
	}
	aql := strings.TrimSpace(wire.AQL)
	if aql == "" {
		writeError(w, http.StatusBadRequest, msgRBMissingAQL) // p27/p28
		return
	}
	if !strings.HasPrefix(aql, aqlItemsFindOpen) {
		writeError(w, http.StatusBadRequest, msgRBAQLNotItems) // p29
		return
	}
	if s.aql == nil {
		s.log.ErrorContext(r.Context(), "httpapi: release bundle assembly reached but no search engine is assembled")
		writeError(w, http.StatusServiceUnavailable, "search is not available on this instance")
		return
	}
	p := principalFrom(r.Context())
	res, err := s.aql.Run(r.Context(), p, aql)
	if err != nil {
		// The engine's families are the AQL plane's own (QueryError → the
		// p33-exact parse copy; 429/408/500 the resource gate) — one
		// mapping, two entrances.
		s.writeAQLRunError(w, r, err)
		return
	}
	hits := make([]bundleAssemblyHit, 0, len(res.Rows))
	pkgTypes := map[string]string{}
	for _, row := range res.Rows {
		if row.Type != "file" {
			continue // a manifest lists artifacts, not folder markers
		}
		props := map[string][]string{}
		if s.deps.Metadata != nil {
			if live, perr := s.deps.Metadata.NodeProps().List(r.Context(), row.RepoKey, row.Path); perr == nil && len(live) > 0 {
				props = live
			}
		}
		pkgType, seen := pkgTypes[row.RepoKey]
		if !seen {
			pkgType = s.repoPkgTypeFace(r, row.RepoKey)
			pkgTypes[row.RepoKey] = pkgType
		}
		hits = append(hits, bundleAssemblyHit{
			URN: row.RepoKey + "/" + row.Path, Sha256: row.Sha256,
			Properties: props, Size: row.Size, PkgType: pkgType,
		})
	}
	writeJSONBody(w, http.StatusOK, struct {
		Results []bundleAssemblyHit `json:"results"`
	}{Results: hits})
}

// repoPkgTypeFace renders the repository's package type in the reference's
// wire casing (p30: "Generic" for a generic repository — first letter up,
// rest verbatim). A repository that vanished mid-query answers "" (the row
// is already in hand; refusing the whole answer over the label would lie
// about the manifest).
func (s *Server) repoPkgTypeFace(r *http.Request, repoKey string) string {
	if s.deps.Repos == nil {
		return ""
	}
	row, err := s.deps.Repos.Get(r.Context(), repoKey)
	if err != nil || row == nil || row.PackageType == "" {
		return ""
	}
	return strings.ToUpper(row.PackageType[:1]) + row.PackageType[1:]
}
