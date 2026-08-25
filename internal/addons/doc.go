// Package addons is the compile-time addon registry (M10 T-282, ADR-0033 /
// architecture section 15.2): every licensable capability — each package
// type and each cross-cutting feature — is one Addon descriptor, and the set
// of descriptors an assembly carries is a LITERAL SLICE at the cmd assembly
// point (the explicit-assembly ruling: no init() self-registration, the same
// convention internal/adapter's registry already established).
//
// The behavior pattern mirrors Artifactory's META-INF/addon.{xml,properties}
// (a static assembly unit carrying metadata and a tier mark, inv-2 §3/§4);
// the FORM is BinFlow's own: a Go slice of descriptors, greppable and
// diffable, complete at compile time (a forgotten slot is a code review
// diff, never a runtime surprise).
//
// Three shapes live here:
//
//   - the descriptor and registry (Addons are validated at assembly; a
//     malformed manifest panics at startup, adapter.Register's posture);
//   - the status view (Statuses joins the registry with the license
//     Manager's gate facet — GET /api/v1/addons and every future consumer
//     share one evaluation, so visibility and enforcement cannot diverge);
//   - the package-type gate seam (PackageTypeAvailable is weave point 2's
//     single spelling, section 15.1.5; T-283 wires it into repo.Service's
//     validation chain).
//
// Dependency direction (ADR-0033): addons imports license for the Tier type
// only; license never imports addons (its AddonEnabled takes primitives).
package addons
