// Package nuget is the NuGet package-type adapter (M10/T-287 pilot, the
// T-337 v2 completion, the T-341 v3 upstream proxy, PRD FR-88/FR-103/
// FR-104): the v3 main face (service index, flatcontainer
// push/download, registrations, search) plus the FULL v2 OData face
// (docs/reverse/nuget.md section 2's 18-endpoint table).
//
// # Behavior basis
//
// T-287's grounding (the official NuGet API specifications, PRD FR-88, and
// the live nuget.org probes of August 2026) still carries the v3 LOCAL
// face; the v3 REMOTE/VIRTUAL faces since T-341 follow docs/reverse/
// nuget.md sections 8 and 9 (the T-304 L3'/L4/L7 rulings: the live
// SearchQueryService proxy and the service-index type ladders). The v2
// face's basis since T-337 is docs/reverse/nuget.md itself — the T-334
// activation of the T-304 reverse-engineering pass — with the
// medium-confidence edges closed by fresh live probes against nuget.org's
// own v2 face (the semVerLevel dotted-prerelease boundary, GetUpdates'
// server-side filtering, the $batch wire shape, the DataServiceVersion
// header family), all recorded in the ticket logs.
//
// # Layout (one namespace, three faces)
//
// Wire (after the /binflow prefix strip httpapi performs):
//
//	/binflow/api/nuget/v3/<repo>/index.json                service index
//	/binflow/api/nuget/v3/<repo>/flatcontainer/<id>/index.json
//	/binflow/api/nuget/v3/<repo>/flatcontainer/<id>/<version>          PUT push
//	/binflow/api/nuget/v3/<repo>/flatcontainer/<id>/<version>/<id>.<version>.{nupkg,nupkg.sha512,nuspec}
//	/binflow/api/nuget/v3/<repo>/registration[-semver2]/<id>/index.json
//	/binflow/api/nuget/v3/<repo>/registration[-semver2]/<id>/page/<file>          remote pages
//	/binflow/api/nuget/v3/<repo>/registration[-semver2]/<id>/<version>.json       single-version leaf
//	/binflow/api/nuget/v3/<repo>/query?q=…                              search
//
// The v2 OData face (T-337, nuget.md section 2's 18-endpoint table — every
// collection endpoint also routes ±/$count, and every function import also
// routes without its parens):
//
//	/binflow/api/nuget/v2/<repo>/            service document (GET) / publish root (PUT, #1/#17)
//	/binflow/api/nuget/v2/<repo>/$metadata   EDMX (#2)
//	/binflow/api/nuget/v2/<repo>/Search()    the search family (#3/#4)
//	/binflow/api/nuget/v2/<repo>/FindPackagesById()?id='<id>'           the restore carrier (#5/#6)
//	/binflow/api/nuget/v2/<repo>/Packages()[(Id='x'[,Version='y'])][/Id] the entity-set family (#7-#10)
//	/binflow/api/nuget/v2/<repo>/GetUpdates()/?packageIds=…&versions=…  the VS update check (#11/#12)
//	/binflow/api/nuget/v2/<repo>/$batch      OData $batch, POST (#13)
//	/binflow/api/nuget/v2/<repo>/Download/<id>/<version>                protocol download (#14)
//	/binflow/api/nuget/v2/<repo>/<path>/<file>.nupkg                    bare download (#15)
//	/binflow/api/nuget/v2/<repo>/<path>     PUT publish-with-prefix (#18) / DELETE id/version (#16)
//
// (The base-root spellings — #1/#17 — reach the adapter through the
// content-plane form /binflow/<repo>/v2 until the api-mount rewrite
// accepts an empty rest; the registered httpapi gap.)
//
// Storage (repo-relative; lowercase id/version keys, the flatcontainer
// spelling — the local layout's identity):
//
//	<id>/<version>/<id>.<version>.nupkg            the package (content)
//	<id>/<version>/<id>.<version>.nupkg.sha512     base64 SHA-512 sidecar
//	<id>/<version>/<id>.<version>.nuspec           extracted nuspec sidecar
//	.nuGetV3/feed.json                             remote: cached upstream service index
//	.nuGetV3/<upstream-path>/<id>/index.json       remote: cached registration index
//	.nuGetV3/<upstream-path>/<id>/page/<file>      remote: cached registration pages
//	.nuGetV3/<upstream-path>/<id>/<version>.json   remote: cached single-version leaf
//	.nuGetV3/<upstream-path>/<id>/<version>/…      remote: cached flatcontainer family
//	.nuget-v2/<hex>.xml                            remote: cached upstream v2 feed (query-keyed)
//	.nuget-v2/dl/<id>.<version>.nupkg              remote: the api/v2/package alternative hop
//
// The remote class's v3 document family is addressed DYNAMICALLY: the
// upstream service index (cached at .nuGetV3/feed.json, the metadata TTL)
// resolves each resource family's @id (nuget.md sections 8.1/9.2 — the
// type ladders), and the marker carries the resolved upstream path
// verbatim so the provider's UpstreamPath facet is the identity strip. An
// unresolvable index degrades to the nuget.org-family constants (the
// as-built spellings — the regression guardrail). The search face's
// upstream (SearchQueryService) is fetched DIRECTLY through the guarded
// egress client and never lands as an artifact (section 8.1-4).
//
// The sidecars are written by push (server-generated, the regenerable
// family) and are what registrations, search and the v2 feed render from —
// one source of truth per fact, no second metadata store. A v2 publish
// with a path prefix lands at <prefix>/<id>.<version>.nupkg (nuget.md
// section 5.1's derivation) and stays addressable through the deep
// resolution chain (section 6's property-index level: any stored
// <id>.<version>.nupkg base name).
package nuget
