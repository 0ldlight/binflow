package addons

import (
	"github.com/lzwzzy/binflow/internal/license"
)

// The M10 slot manifest constructors (PRD FR-86.2, ADR-0033 decision 4/5).
// cmd/binflow-server's assembly gathers them into the literal slice — THIS
// FILE is where a new slot's metadata lands; no gate, view or validation
// branch anywhere else learns about it (FR-86-AC2's single-source rule).
//
// Tier rulings (PRD 86.2, deliberately diverging from Artifactory's
// oss/pro/ent marks where BinFlow's M1~M9 commitments say so):
//   - the five core package types are community — the floor. Retro-fitting
//     them as addons changed zero behavior: MinTier=TierCommunity means
//     AddonEnabled answers true on every instance, licensed or not.
//   - go / nuget / cargo are pro (the pilot tier; their protocol adapters
//     land with their own tickets — the slots exist so the gate, the view
//     and the repo-create set are complete from day one).
//   - properties is community (Artifactory puts it in pro; BinFlow's
//     artifact properties are an M10 core deliverable, not an upsell).
//   - ha / xray-integration are enterprise placeholder slots: visible and
//     evaluable, the feature bodies themselves land M11+.

// Generic is the five-core retro-fit slot for package type "generic".
func Generic() Addon {
	return Addon{
		ID:          "generic",
		Kind:        KindPackageType,
		MinTier:     license.TierCommunity,
		PackageType: "generic",
		DisplayName: "Generic",
		Description: "Unstructured binary artifacts on any layout (the M1 foundation package type, community floor).",
	}
}

// Docker is the five-core retro-fit slot for package type "docker".
func Docker() Addon {
	return Addon{
		ID:          "docker",
		Kind:        KindPackageType,
		MinTier:     license.TierCommunity,
		PackageType: "docker",
		DisplayName: "Docker Registry",
		Description: "OCI/Docker registry v2 push, pull and token planes (community floor).",
	}
}

// Maven is the five-core retro-fit slot for package type "maven".
func Maven() Addon {
	return Addon{
		ID:          "maven",
		Kind:        KindPackageType,
		MinTier:     license.TierCommunity,
		PackageType: "maven",
		DisplayName: "Maven",
		Description: "Maven repository layout with maven-metadata.xml calculation (community floor).",
	}
}

// Npm is the five-core retro-fit slot for package type "npm".
func Npm() Addon {
	return Addon{
		ID:          "npm",
		Kind:        KindPackageType,
		MinTier:     license.TierCommunity,
		PackageType: "npm",
		DisplayName: "npm",
		Description: "npm registry API with packument/tarball planes (community floor).",
	}
}

// Pypi is the five-core retro-fit slot for package type "pypi".
func Pypi() Addon {
	return Addon{
		ID:          "pypi",
		Kind:        KindPackageType,
		MinTier:     license.TierCommunity,
		PackageType: "pypi",
		DisplayName: "PyPI",
		Description: "Python simple index and upload planes (community floor).",
	}
}

// Go is the gated pilot slot for package type "go" (GOPROXY protocol; the
// adapter lands with the M10 Go pilot).
func Go() Addon {
	return Addon{
		ID:          "go",
		Kind:        KindPackageType,
		MinTier:     license.TierPro,
		PackageType: "go",
		DisplayName: "Go Modules",
		Description: "GOPROXY module proxy planes (@v/list, .info/.mod/.zip); the protocol adapter lands with the M10 Go pilot.",
	}
}

// NuGet is the gated pilot slot for package type "nuget" (v2/v3 dual stack;
// the adapter lands M11).
func NuGet() Addon {
	return Addon{
		ID:          "nuget",
		Kind:        KindPackageType,
		MinTier:     license.TierPro,
		PackageType: "nuget",
		DisplayName: "NuGet",
		Description: "NuGet v2/v3 feed planes; the protocol adapter lands M11.",
	}
}

// Cargo is the gated placeholder slot for package type "cargo" (Rust crates
// sparse index; the adapter lands M11).
func Cargo() Addon {
	return Addon{
		ID:          "cargo",
		Kind:        KindPackageType,
		MinTier:     license.TierPro,
		PackageType: "cargo",
		DisplayName: "Cargo (Rust)",
		Description: "Rust crates publish/download with the sparse index; the protocol adapter lands M11.",
	}
}

// Conan is the gated slot for package type "conan" (C/C++ packages, the
// v1+v2 revision-chain protocol; the local adapter lands with M11's T-308,
// the remote/virtual faces with their own tickets).
func Conan() Addon {
	return Addon{
		ID:          "conan",
		Kind:        KindPackageType,
		MinTier:     license.TierPro,
		PackageType: "conan",
		DisplayName: "Conan (C/C++)",
		Description: "Conan v2 revision protocol plus the full v1 data plane on local repositories; remote and virtual land with their own tickets.",
	}
}

// Helm is the gated slot for package type "helm" (the CLASSIC chart
// repository face — index.yaml + tgz; M11/T-309). The HelmOCI face is a
// SEPARATE package type and slot (HelmOCI below) — the two protocol
// families never share one virtual repository.
func Helm() Addon {
	return Addon{
		ID:          "helm",
		Kind:        KindPackageType,
		MinTier:     license.TierPro,
		PackageType: "helm",
		DisplayName: "Helm Charts",
		Description: "Classic Helm chart repositories: index.yaml calculation, chart and provenance upload, repo add/update/pull.",
	}
}

// HelmOCI is the gated slot for package type "helmoci" (the registry-v2
// Helm face — charts as OCI artifacts on helm push/pull oci://; M12/T-342,
// FR-109/HL-3). The package type rides the docker adapter's /v2 plane
// (helm.md section 8.2's isDockerGroup posture), so this slot's gate seam
// is the /v2 write face and the repo-create plane, never a separate
// handler's first line. LOCAL repositories only at this tier of the
// roadmap: the remote pull-through and virtual aggregation are their own
// future tickets (the Helm/HelmOCI no-mix rule already guards the virtual
// member plane, T-309).
func HelmOCI() Addon {
	return Addon{
		ID:          "helmoci",
		Kind:        KindPackageType,
		MinTier:     license.TierPro,
		PackageType: "helmoci",
		DisplayName: "Helm OCI Charts",
		Description: "Helm charts as OCI artifacts: helm push/pull/install over oci:// references on the registry v2 plane.",
	}
}

// Rpm is the gated slot for package type "rpm" (the YUM/repomd repository
// face — .rpm upload with header parsing plus the repodata engine;
// M11/T-311). Local repositories only here; the remote pull-through and
// the virtual aggregation land with their own tickets.
func Rpm() Addon {
	return Addon{
		ID:          "rpm",
		Kind:        KindPackageType,
		MinTier:     license.TierPro,
		PackageType: "rpm",
		DisplayName: "RPM (Yum)",
		Description: "YUM repositories: .rpm upload with RPM header parsing, repodata calculation (primary/filelists/other) and the reindex family; remote and virtual land with their own tickets.",
	}
}

// Debian is the gated slot for package type "debian" (the apt repository
// face — debPUT with coordinate matrix parameters plus the automatic
// Packages/Sources/Release/By-Hash engine; M11/T-310). Local
// repositories only here; the remote pull-through (with the path
// normalization family) and the virtual stanza aggregation land with
// their own tickets.
func Debian() Addon {
	return Addon{
		ID:          "debian",
		Kind:        KindPackageType,
		MinTier:     license.TierPro,
		PackageType: "debian",
		DisplayName: "Debian",
		Description: "Debian repositories: debPUT with distribution/component/architecture coordinates, automatic Packages/Sources index calculation with By-Hash support, Release generation and the reindex family; remote and virtual land with their own tickets.",
	}
}

// Properties is the artifact properties feature slot (community by design:
// the cross-cutting base of the M10 deliverable, not an upsell).
func Properties() Addon {
	return Addon{
		ID:          "properties",
		Kind:        KindFeature,
		MinTier:     license.TierCommunity,
		DisplayName: "Artifact Properties",
		Description: "Matrix-parameter stripping and node property read/write planes.",
	}
}

// HA is the enterprise high-availability placeholder slot.
func HA() Addon {
	return Addon{
		ID:          "ha",
		Kind:        KindFeature,
		MinTier:     license.TierEnterprise,
		DisplayName: "High Availability",
		Description: "Multi-node cluster plane (slot reserved — the feature body lands M11+).",
	}
}

// RepoOperations is the artifact-operations family's feature slot (M12
// T-339, pro): copy/move now, the archive trio (folder zip, archive!/
// member reads, exploded upload — T-343) on the same entitlement. The tier
// ruling is the Q4 final decision (BOARD M12, user 2026-08-28): Artifactory
// lists the ENTIRE operations family at pro (repo-operations.md section 6's
// three-way evidence — the REST addon's MissingRestAddonException, the
// Filtered-resources 403, the RestCoreAddon explode refusal), and BinFlow
// mirrors the posture instead of keeping PRD 105.4's interim no-gate. The
// trash-can chain's internal restore moves bypass the gate through the
// service-layer system seam, never this slot (T-345's consumer contract).
func RepoOperations() Addon {
	return Addon{
		ID:          "repo-operations",
		Kind:        KindFeature,
		MinTier:     license.TierPro,
		DisplayName: "Repository Operations",
		Description: "Copy and move artifacts across repositories with dry-run, permission checks and property carry; the archive family (folder download, archive member reads, exploded upload) joins this slot.",
	}
}

// XrayIntegration is the enterprise Xray-integration placeholder slot.
func XrayIntegration() Addon {
	return Addon{
		ID:          "xray-integration",
		Kind:        KindFeature,
		MinTier:     license.TierEnterprise,
		DisplayName: "Xray Integration",
		Description: "Artifact scanning integration surface (slot reserved — the feature body lands M11+).",
	}
}

// Trashcan is the trash-can feature slot (M12 T-345, FR-106). The tier
// marked here is PRD 106.4's INTERIM ruling (Q3's 暂行 pro+); the terminal
// decision rides the T-345 evidence brief — the reverse-docs record shows
// NO addon gating on the Artifactory side (inv-4's addon inventory lists
// no trash entry; the OSS repo's rest-common carries the trash service
// base; the OSS config template ships trashcanConfig), so flipping this
// to TierCommunity is the recommended terminal shape and a one-line change
// (the gate, view and delete-seam behavior follow the slot, FR-86-AC2's
// single-source rule). Kind is KindFeature — BinFlow has no separate
// feature-gov kind (the PRD's 暂定 naming); the governance flavor is the
// description's, registered.
func Trashcan() Addon {
	return Addon{
		ID:          "trashcan",
		Kind:        KindFeature,
		MinTier:     license.TierPro,
		DisplayName: "Trash Can",
		Description: "Soft-delete safety net: deletes are captured into the built-in auto-trashcan with provenance properties, 14-day retention, restore/empty/clean.",
	}
}

// Webhook is the unified-event webhook feature slot (M13 T-362, FR-114 /
// ADR-0041 decision 8 — the 19th slot). The tier is the Q4 final ruling:
// the official Feature Comparison Matrix puts webhooks OUTSIDE the
// non-commercial column and INSIDE Pro X and up (webhook.md section 8,
// high confidence), so MinTier=pro mirrors the boundary and community
// instances keep the subscription writes gated (403 + the license header)
// while reads stay open. Kind=KindFeature — no third kind (the ADR's
// closed-set ruling; the PRD's "feature-int" interim spelling is a
// naming note, not a routing difference).
func Webhook() Addon {
	return Addon{
		ID:          "webhook",
		Kind:        KindFeature,
		MinTier:     license.TierPro,
		DisplayName: "Webhooks",
		Description: "Unified-event webhooks: subscribe to artifact, property and docker events over the /event/api/v1 plane with criteria filters, HMAC-signed delivery and a delivery outbox.",
	}
}
