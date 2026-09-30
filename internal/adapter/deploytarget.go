package adapter

import "encoding/json"

// VirtualDeploymentTarget is the single source of the tolerant write-route
// probe of a virtual repository's config JSON: the first non-empty of the
// primary spelling defaultDeploymentRepo and the two Artifactory aliases
// defaultDeploymentRepoRef / deploymentRepository that raw-seeded rows may
// carry. It restates repo's own unexported virtualRouteTarget reader (the
// write-plane seam there is deliberately unexported — the ClassReader hands
// the Config blob verbatim, so the adapter side must read it itself); the
// strict alias-agreement rules live at config time (validateVirtualMembers),
// and a config that fails the strict shape still gets its truthful answer
// here: no route ("").
//
// Tolerance contract, byte-pinned by the snapshot test in this package: the
// alias triple is consulted in the fixed order above and an EMPTY-STRING
// value is skipped (never a winner — first non-empty wins); key matching is
// encoding/json's own (tag spelling, case-insensitive fallback); values are
// returned verbatim, NOT trimmed; unparseable input, a non-object shape, or
// a wrong-typed alias value unmarshals to an error and answers "".
//
// Single-source discipline (the mimetable precedent, T-581): hoisted from
// eight byte-identical lockstep copies across the protocol packages
// (cargo/conan/deb/generic/helm/maven/npm/nuget — the seven counted by the
// R11 review plus helm's virtualWriteTarget, found during convergence),
// T-590 / BIN-72, R11 Reviewer B NB-①. Any change to the alias set or the
// tolerance rule lands here exactly once and every consumer inherits it the
// same instant; a consumer re-growing a local copy of the probe is the drift
// this hoist exists to prevent. Consumers keep thin local aliases of this
// function (their call sites read unchanged); do not add new probe bodies.
func VirtualDeploymentTarget(config string) string {
	var probe struct {
		DefaultDeploymentRepo    string `json:"defaultDeploymentRepo"`
		DefaultDeploymentRepoRef string `json:"defaultDeploymentRepoRef"`
		DeploymentRepository     string `json:"deploymentRepository"`
	}
	if err := json.Unmarshal([]byte(config), &probe); err != nil {
		return ""
	}
	for _, alias := range []string{probe.DefaultDeploymentRepo, probe.DefaultDeploymentRepoRef, probe.DeploymentRepository} {
		if alias != "" {
			return alias
		}
	}
	return ""
}
