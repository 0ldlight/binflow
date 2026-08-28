package nuget

import (
	"encoding/json"
	"net/http"
)

// The v3 service index (the official Service Index spec): version 3.0.0
// plus the resource set this adapter serves. Clients pick the HIGHEST
// versioned resource of each family they understand (dotnet today picks
// SearchQueryService/3.5.0 and RegistrationsBaseUrl/3.6.0), so emitting
// the unversioned and the versioned spellings of one base is the
// compatible minimal set — with the SemVer2 registration family
// announcing its own -semver2 base (nuget.md section 9.1), which the
// remote class resolves through the upstream's own index ladder
// (section 9.2).
//
// The PackagePublish/2.0.0 @id deliberately points at the flatcontainer
// base (no trailing slash): the client appends /{id}/{version}, making
// the push land on the same route family as the downloads — PRD 88.1's
// "flatcontainer PUT（dotnet push 落点）".

// serviceIndexResource is one @id/@type pair.
type serviceIndexResource struct {
	ID   string `json:"@id"`
	Type string `json:"@type"`
}

// serviceIndexDocument is the index body.
type serviceIndexDocument struct {
	Version   string                 `json:"version"`
	Resources []serviceIndexResource `json:"resources"`
}

// serveServiceIndex renders the per-repository service index. The resource
// rows follow nuget.md section 9.1's internal-shape table: the
// unversioned registration family and /3.4.0 announce the plain
// registration base, the SemVer2 family (/3.6.0, /Versioned) the
// -semver2 base, the search family the query base, the legacy/publish
// pair the v2 and flatcontainer faces, and the two display templates the
// registration document family they address. (PackagePublish keeps the
// flatcontainer @id of the T-287 pilot — the dotnet-verified push landing
// — rather than section 9.1's v2 row; the deviation register in the
// ticket log carries it.)
func (h *Handler) serveServiceIndex(w http.ResponseWriter, r *http.Request, repoKey string) {
	origin := h.baseURLFor(r)
	flat := flatBase(origin, repoKey)
	reg := regBase(origin, repoKey)
	regSemVer2 := regSemVer2Base(origin, repoKey)
	query := apiBase(origin, planeV3, repoKey) + "/" + segQuery
	v2 := v2Base(origin, repoKey)

	doc := serviceIndexDocument{
		Version: "3.0.0",
		Resources: []serviceIndexResource{
			{ID: query, Type: "SearchQueryService"},
			{ID: query, Type: "SearchQueryService/3.5.0"},
			{ID: query, Type: "SearchQueryService/3.0.0-beta"},
			{ID: query, Type: "SearchQueryService/3.0.0-rc"},
			{ID: reg, Type: "RegistrationsBaseUrl"},
			{ID: reg, Type: "RegistrationsBaseUrl/3.4.0"},
			{ID: reg, Type: "RegistrationsBaseUrl/3.0.0-rc"},
			{ID: reg, Type: "RegistrationsBaseUrl/3.0.0-beta"},
			{ID: regSemVer2, Type: "RegistrationsBaseUrl/3.6.0"},
			{ID: regSemVer2, Type: "RegistrationsBaseUrl/Versioned"},
			{ID: flat, Type: "PackageBaseAddress/3.0.0"},
			{ID: trimTrailingSlash(flat), Type: "PackagePublish/2.0.0"},
			{ID: v2, Type: "LegacyGallery"},
			{ID: v2, Type: "LegacyGallery/2.0.0"},
			{ID: reg + "{id-lower}/index.json", Type: "PackageDisplayMetadataUriTemplate/3.0.0-rc"},
			{ID: reg + "{id-lower}/{version-lower}.json", Type: "PackageVersionDisplayMetadataUriTemplate/3.0.0-rc"},
		},
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		writePlain(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, append(body, '\n'))
}

// trimTrailingSlash is the one-liner the publish @id needs.
func trimTrailingSlash(s string) string {
	if len(s) > 0 && s[len(s)-1] == '/' {
		return s[:len(s)-1]
	}
	return s
}
