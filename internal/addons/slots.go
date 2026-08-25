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
