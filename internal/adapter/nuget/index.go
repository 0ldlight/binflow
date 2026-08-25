package nuget

import (
	"encoding/json"
	"net/http"
)

// The v3 service index (the official Service Index spec): version 3.0.0
// plus the resource set the pilot serves. Clients pick the HIGHEST
// versioned resource of each family they understand (dotnet today picks
// SearchQueryService/3.5.0 and RegistrationsBaseUrl/3.6.0), so emitting
// the unversioned and the common versioned spellings of one base is the
// compatible minimal set.
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

// serveServiceIndex renders the per-repository service index.
func (h *Handler) serveServiceIndex(w http.ResponseWriter, r *http.Request, repoKey string) {
	origin := h.baseURLFor(r)
	flat := flatBase(origin, repoKey)
	reg := regBase(origin, repoKey)
	query := apiBase(origin, planeV3, repoKey) + "/" + segQuery
	v2 := v2Base(origin, repoKey)

	doc := serviceIndexDocument{
		Version: "3.0.0",
		Resources: []serviceIndexResource{
			{ID: query, Type: "SearchQueryService"},
			{ID: query, Type: "SearchQueryService/3.5.0"},
			{ID: query, Type: "SearchQueryService/3.0.0-beta"},
			{ID: reg, Type: "RegistrationsBaseUrl"},
			{ID: reg, Type: "RegistrationsBaseUrl/3.4.0"},
			{ID: reg, Type: "RegistrationsBaseUrl/3.6.0"},
			{ID: flat, Type: "PackageBaseAddress/3.0.0"},
			{ID: trimTrailingSlash(flat), Type: "PackagePublish/2.0.0"},
			{ID: v2, Type: "LegacyGallery"},
			{ID: v2, Type: "LegacyGallery/2.0.0"},
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
