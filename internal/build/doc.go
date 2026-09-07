// Package build is the build-info domain (M17 FR-152, ADR-0045): the
// record plane of CI builds — runs, modules, artifacts, dependencies,
// promotions and properties — persisted through metadata's BuildStore
// sub-store (the 024 table family, the ScheduleStore/webhook-store
// precedent), with authorization by the SAME allow() source the
// repository domain uses: the path position carries the build NAME, so a
// permission target's include/exclude patterns grant by name pattern with
// no new permission face (ADR-0045 decision 4; T-92 lineage).
//
// Import ban (ADR-0045 decision 1, the ADR-0044 structural-guarantee
// shape): this package imports only the sanctioned internal dependencies —
// metadata (the BuildStore sub-store), auth (Authorizer + Principal),
// later repo (CopyOrMove carrier), audit (facet) and config (read-only).
// It must NEVER import internal/webhook, internal/search,
// internal/httpapi, internal/scheduler, internal/bundle,
// internal/insights, internal/license or internal/replication: weaving
// happens through injected facets at the assembly layer (webhook's Emit
// facet, search's BuildSearcher read), never through direct imports —
// boundary_test.go asserts the import graph, so the ban is a compile-time
// companion, not a convention.
package build
