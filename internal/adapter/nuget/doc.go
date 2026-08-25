// Package nuget is the NuGet package-type pilot adapter (M10/T-287, PRD
// FR-88): the v3 main face (service index, flatcontainer push/download,
// registrations, search) plus the v2 minimal face (FindPackagesById OData).
//
// # Behavior basis (the spec-ticket gap, honestly stated)
//
// The ticket's named basis, docs/reverse/nuget.md (T-280), did NOT exist at
// implementation time — no such file, no commit, no report. The grounding
// actually used, in precedence order:
//
//  1. the official NuGet API specifications (learn.microsoft.com/nuget/api:
//     service index, registration blob, flat container, package publish) —
//     the clean-room rule's own priority for protocols with a public spec;
//  2. PRD FR-88 (milestone-10.md §4.3): endpoint set, URL spellings under
//     /binflow/api/nuget/{v2,v3}/<repoKey>/…, rclass duties, gate slot;
//  3. live nuget.org shape probes recorded in reports/agents/T-287.md
//     (registration pagination, forced gzip on registration5-gz-*, the
//     azure.cn mirror redirect) — August 2026 snapshot, cited in code.
//
// Every ruling not covered by those three (the K28-class edges: v2 $count
// and Packages()Id= omitted, DELETE = hard delete not unlist, the nuget.org
// upstream prefixes as constants) is marked // T-287 ruling in the code and
// collected in the ticket log for T-280/T-293 to confirm or flip.
//
// # Layout (one namespace, three faces)
//
// Wire (after the /binflow prefix strip httpapi performs):
//
//	/binflow/api/nuget/v3/<repo>/index.json                service index
//	/binflow/api/nuget/v3/<repo>/flatcontainer/<id>/index.json
//	/binflow/api/nuget/v3/<repo>/flatcontainer/<id>/<version>          PUT push
//	/binflow/api/nuget/v3/<repo>/flatcontainer/<id>/<version>/<id>.<version>.{nupkg,nupkg.sha512,nuspec}
//	/binflow/api/nuget/v3/<repo>/registration/<id>/index.json
//	/binflow/api/nuget/v3/<repo>/registration/<id>/page/<file>          remote pages
//	/binflow/api/nuget/v3/<repo>/query?q=…                              search
//	/binflow/api/nuget/v2/<repo>/FindPackagesById()?id='<id>'           OData feed
//	/binflow/api/nuget/v2/<repo>/$metadata                              EDMX
//
// The /binflow/api/nuget mount rewrites onto the content plane as
// /binflow/<repo>/v3/… (router.go's plane-aware twin of the npm/pypi
// mount), so repository lookup, RBAC, the addon gate and adapter dispatch
// all run exactly once per request — the v3/v2 plane segment is part of the
// repository's own path namespace (the npm mount's shared-namespace rule).
//
// Storage (repo-relative; lowercase id/version keys, the flatcontainer
// spelling — so remote pull-through is the identity mapping):
//
//	<id>/<version>/<id>.<version>.nupkg            the package (content)
//	<id>/<version>/<id>.<version>.nupkg.sha512     base64 SHA-512 sidecar
//	<id>/<version>/<id>.<version>.nuspec           extracted nuspec sidecar
//	<id>/index.json                                remote: cached versions doc
//	<id>/.registration                             remote: cached registration index
//	<id>/.page/<file>                              remote: cached registration pages
//
// The sidecars are written by push (server-generated, the regenerable
// family) and are what registrations, search and the v2 feed render from —
// one source of truth per fact, no second metadata store.
package nuget
