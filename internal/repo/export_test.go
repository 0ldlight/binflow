package repo

// In-package test seam for the external suite (virtual_test.go drives the
// behavior end to end; these two pins cover the degraded config shapes —
// hand-mangled blobs and raw-seeded rows — that the behavior tests cannot
// reach cheaply).

var (
	// MemberPriorityResolutionForTest exposes the priority-mark probe.
	MemberPriorityResolutionForTest = memberPriorityResolution
	// VirtualRouteTargetForTest exposes the tolerant write-route reader.
	VirtualRouteTargetForTest = virtualRouteTarget
)
