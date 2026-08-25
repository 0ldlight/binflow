package addons

import (
	"fmt"

	"github.com/lzwzzy/binflow/internal/license"
)

// Kind is the descriptor's consumer split (ADR-0033 decision 3: one struct,
// two kinds — the difference lives at the consumption seams, not in the type
// hierarchy). A package-type addon's behavior carrier is an adapter.Handler;
// a feature addon's carrier is its own services and REST routes (M11+).
type Kind string

const (
	// KindPackageType marks a slot whose id IS a legal
	// repositories.package_type value ("generic", "go", ...): creating,
	// writing and reading repositories of that type is the capability being
	// licensed.
	KindPackageType Kind = "package-type"
	// KindFeature marks a cross-cutting capability slot (properties, ha,
	// xray-integration, ...): no package type, its own consumption seams.
	KindFeature Kind = "feature"
)

// Addon is one licensable capability's descriptor. All fields are static
// assembly facts — the tier x addon unlock MATRIX IS THIS CODE, not data:
// MinTier is a descriptor constant, and changing a slot's tier is a release,
// never a runtime edit (ADR-0033's "no runtime-mutable licensing surface").
type Addon struct {
	// ID is the stable identifier. For a package-type addon the id EQUALS
	// the package type ("go", "cargo", ...); for a feature addon it is the
	// feature's own name ("ha"). New asserts this invariant.
	ID string
	// Kind selects the consumption seam (package-type vs feature).
	Kind Kind
	// MinTier is the lowest license tier that unlocks the slot. The five
	// core package types and the properties feature sit on
	// license.TierCommunity — the floor, unlocked with no license at all
	// (architecture invariant 1: M1~M9 capabilities never degrade).
	MinTier license.Tier
	// PackageType is Kind=KindPackageType's routing key (== ID, asserted at
	// assembly) and MUST be empty for Kind=KindFeature.
	PackageType string
	// DisplayName and Description are the console metadata (the repo-create
	// dialog's package-type list and the License & Add-ons page cards).
	DisplayName string
	Description string
}

// Registry is the assembled manifest: an ordered slot list plus its lookup
// indexes, immutable after New (constructed once at cmd assembly, read by
// every seam — no locking needed).
type Registry struct {
	byID      map[string]Addon
	ordered   []Addon
	byPackage map[string]Addon
	pkgOrder  []Addon
	featOrder []Addon
}

// New builds the registry over the assembly's literal slice. A malformed
// descriptor PANICS — duplicate id, unknown Kind, Kind/package-type field
// misuse, an id that disagrees with its PackageType, a MinTier outside the
// closed tier set, or missing console metadata are assembly bugs that must
// surface at startup, never at request time (adapter.Register's posture;
// section 15.2.4 names the missing-five-core-descriptor case this catches).
func New(items ...Addon) *Registry {
	r := &Registry{
		byID:      make(map[string]Addon, len(items)),
		ordered:   make([]Addon, 0, len(items)),
		byPackage: make(map[string]Addon, len(items)),
	}
	for _, a := range items {
		switch {
		case a.ID == "":
			panic("addons: New: descriptor with an empty ID")
		case a.DisplayName == "":
			panic(fmt.Sprintf("addons: New(%s): DisplayName is empty (console metadata is required)", a.ID))
		}
		if a.MinTier < license.TierCommunity || a.MinTier > license.TierEnterprise {
			panic(fmt.Sprintf("addons: New(%s): MinTier %d is outside the closed tier set", a.ID, a.MinTier))
		}
		switch a.Kind {
		case KindPackageType:
			// The id IS the package type (ADR-0033 decision 1): dispatch,
			// gate and status all key on one string, so two spellings of
			// one slot would be three divergent behaviors.
			if a.PackageType == "" {
				panic(fmt.Sprintf("addons: New(%s): package-type addon needs its PackageType set", a.ID))
			}
			if a.PackageType != a.ID {
				panic(fmt.Sprintf("addons: New(%s): PackageType %q must equal the ID (the addon id IS the package type)",
					a.ID, a.PackageType))
			}
		case KindFeature:
			if a.PackageType != "" {
				panic(fmt.Sprintf("addons: New(%s): feature addon must leave PackageType empty (got %q)",
					a.ID, a.PackageType))
			}
		default:
			panic(fmt.Sprintf("addons: New(%s): unknown Kind %q (want %q or %q)",
				a.ID, a.Kind, KindPackageType, KindFeature))
		}
		if _, dup := r.byID[a.ID]; dup {
			panic(fmt.Sprintf("addons: New: duplicate addon id %q", a.ID))
		}
		r.byID[a.ID] = a
		r.ordered = append(r.ordered, a)
		if a.Kind == KindPackageType {
			r.byPackage[a.PackageType] = a
			r.pkgOrder = append(r.pkgOrder, a)
		} else {
			r.featOrder = append(r.featOrder, a)
		}
	}
	return r
}

// All returns every slot in assembly order (the manifest as assembled —
// GET /api/v1/addons renders this order; a diff of the cmd literal slice is
// a diff of the response).
func (r *Registry) All() []Addon { return append([]Addon(nil), r.ordered...) }

// ByID resolves one slot. ok is false for an unknown id (callers treat that
// as "not an addon question", never as a denial).
func (r *Registry) ByID(id string) (Addon, bool) {
	a, ok := r.byID[id]
	return a, ok
}

// ForPackageType resolves the package-type slot serving that
// repositories.package_type value. ok is false when no package-type addon is
// registered for it. The CALLER derives gated-ness: a returned slot is gated
// exactly when a.MinTier > license.TierCommunity (section 15.2.2) — the
// five core slots answer MinTier=TierCommunity and are therefore never
// gated, on any instance, licensed or not.
func (r *Registry) ForPackageType(packageType string) (Addon, bool) {
	a, ok := r.byPackage[packageType]
	return a, ok
}

// PackageTypeAddons returns the package-type slots in assembly order (the
// repo-create plane's dynamic legal-set and the /api/v1/addons view's
// package-type half).
func (r *Registry) PackageTypeAddons() []Addon { return append([]Addon(nil), r.pkgOrder...) }

// FeatureAddons returns the feature slots in assembly order.
func (r *Registry) FeatureAddons() []Addon { return append([]Addon(nil), r.featOrder...) }

// Len is the slot count (the FR-86-AC1 ">= 10 slots" guard's input).
func (r *Registry) Len() int { return len(r.ordered) }
