// Package helmoci is the HelmOCI package type's registration shell (M12
// T-342, FR-109/HL-3): charts as OCI artifacts served on the docker
// adapter's registry-v2 plane.
//
// # Why a shell (HL-3, helm.md section 8)
//
// The HelmOCI client protocol IS the OCI distribution protocol — helm's
// registry client (ORAS) speaks plain registry v2 with Helm's media types.
// One plane therefore serves the whole family, exactly as Artifactory's
// isDockerGroup = {Docker, OCI, HelmOCI} does:
//
//	helm registry login $HOST            → GET /v2 (ping) + the token plane
//	helm push c-0.1.0.tgz oci://$HOST/$REPO
//	                                     → POST/PATCH/PUT /v2/$REPO/$CHART/blobs/uploads/
//	                                       + PUT /v2/$REPO/$CHART/manifests/0.1.0
//	helm pull oci://$HOST/$REPO/$CHART --version 0.1.0
//	                                     → GET /v2/$REPO/$CHART/manifests/0.1.0
//	                                       + GET /v2/$REPO/$CHART/blobs/<digest>
//
// The docker adapter owns that wire surface; repositories with
// package_type=helmoci route to it through the /v2 plane's family gate
// (docker.servesV2Plane) and the repo service's registry-v2 family check
// (isV2PlaneFamily). This package contributes what the package type needs
// AROUND the plane: the adapter registration under the "helmoci" key (the
// content plane dispatch and the addon-slot assembly guard) and the
// package-type documentation the slot's tier ruling leans on.
//
// # Manifest shape (helm.md section 8.1; the media-type chain)
//
// A helm chart push lands one OCI image manifest whose descriptors carry
// the Helm media types — the chain the acceptance asserts verbatim:
//
//	manifest  application/vnd.oci.image.manifest.v1+json
//	config    application/vnd.cncf.helm.config.v1+json   (Chart.yaml as JSON)
//	layer 1   application/vnd.cncf.helm.chart.content.v1.tar+gzip
//	layer 2   application/vnd.cncf.helm.chart.provenance.v1.prov (only when
//	          a .prov file sits beside the tgz — helm pushes it as a second
//	          layer automatically, and helm pull --verify reads it back)
//
// The docker plane's manifest validation is deliberately PASS-THROUGH
// (manifest.go's no-whitelist ruling, T-32 R3): the stored Content-Type is
// the client's own header value and the descriptors are judged structurally
// (config.digest + layers[].digest presence, reference integrity against
// the repository's blob paths). Nothing here re-validates chart semantics —
// Artifactory's own documented posture ("any OCI artifact may be pushed to
// a HelmOCI repository"), so the three media types need no extra mapping:
// they ride the pass-through as-is, and the round trip is byte-identical.
//
// # Tag semantics
//
// helm binds the manifest tag STRICTLY to the Chart.yaml version (the
// client enforces it; the registry only stores tags). A no-version
// `helm pull oci://…/<chart>` resolves through GET tags/list and picks the
// newest version client-side — the tags listing this plane already serves.
//
// # Content plane
//
// The /binflow/<repoKey>/... face answers the registry family's uniform
// spec-body 404: the /v2 plane is the only protocol face (the docker
// package type's posture verbatim — direct file PUT/GET is not a HelmOCI
// client operation; browsing rides the /api/storage management plane,
// which is adapter-independent).
//
// # Scope
//
// LOCAL repositories in full, and — since M13's T-363 — REMOTE
// repositories: the /v2 pull-through against an upstream OCI registry
// (manifest by tag/digest and blob proxying, the upstream Bearer token
// dance, checksum-addressed caching; the behavior spec is helm.md
// section 8.3). The virtual aggregation is T-365's; the Helm/HelmOCI
// no-mix rule on virtual member sets already guards the boundary (T-309's
// validateHelmFamilyMix).
package helmoci
