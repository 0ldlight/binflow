// Package bundle is the release-bundle record domain (M17 FR-153.1,
// ADR-0046 — the minimal face of Q2 exit ①): versioned release records —
// name, version, an explicit artifact manifest snapshotted against the
// live nodes table, a creator and a state — persisted through metadata's
// BundleStore sub-store (the 025 table family, the BuildStore/scheduleStore
// precedent). A bundle is a RECORD, not a content copy: nothing enters
// storage/nodes and no repository is ever auto-created for it (the
// zero-silent-writes posture that declines the reference's default
// `release-bundles` storing repository — ADR-0046 Errata ⑤).
//
// Faces (ADR-0046 decision 2 + Errata ① E5, release-bundle.md §1): the
// create POST /api/release/bundle over an EXPLICIT manifest (the official
// AQL-assembly body degraded to the explicit-list subset, soft-seam ⑥) and
// the source-side query family — names, versions, the single descriptor
// with its HEAD checksum probe and the status string. The signature chain
// (v2 JWS/GPG) and the Distribution service (/api/v1/distribution/*) are
// OUT: the signature column carries the CONTENT DIGEST placeholder, and
// the conflict tri-state's "same signature" arm is the digest equality
// (Errata ② E6).
//
// Gates (ADR-0046 decision 3): create rides CapSystemWrite; the read faces
// ride CapSystemRead OR the Any Distribution pseudo-key channel — Can(p,
// "ANY DISTRIBUTION", bundle_name, r), the T-491 preset bucket evaluated
// EXACTLY, path position carrying the bundle name so includes/excludes
// apply by name. The feature rides the release-bundle slot (MinTier=pro
// interim): writes consult the injected entitlement verdict (fail closed
// when unwired), reads never do (D1).
//
// Import ban (ADR-0046 decision 1, the ADR-0044/0045 structural-guarantee
// shape): this package imports only the sanctioned internal dependencies —
// metadata (the BundleStore sub-store), auth (Authorizer + Principal +
// capability seam), audit (facet). It must NEVER import internal/webhook,
// internal/search, internal/httpapi, internal/repo (the bundle record
// never touches the content plane — the structural boundary against the
// build domain), internal/scheduler, internal/insights, internal/license
// or internal/replication: weaving happens through injected facets at the
// assembly layer, never through direct imports — boundary_test.go asserts
// the import graph, so the ban is a compile-time companion, not a
// convention.
package bundle
