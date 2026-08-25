package repo

import (
	"fmt"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// Node property seam of the repository domain (M10 T-286, FR-89 /
// ADR-0033, architecture sections 15.3.1/15.3.2).
//
// The closed shape rules (charset, lengths, cardinality) and the
// ErrInvalidProperties sentinel live in the metadata package — the 013
// node_props table's row-shape law, placed where every writer (adapter
// matrix parse, this package's options guard, the httpapi REST arm)
// reaches it without an import cycle (repo -> remote -> adapter forbids
// adapter -> repo). This file aliases the pieces under the domain facade
// callers already hold, and owns the one thing that IS this layer's: the
// PutOptions.Properties documentation and its service-side guard.

// ErrInvalidProperties marks a property set rejected by the closed rules
// (metadata.ErrInvalidProperties, aliased): bad key charset, oversize or
// empty value, cardinality over the caps. The HTTP plane maps it to 400 —
// both the matrix-parameter arm and the REST write arm.
var ErrInvalidProperties = metadata.ErrInvalidProperties

// ValidatePropSet guards a deploy-time or REST property set before any
// write: the metadata layer's closed rules under the domain facade.
func ValidatePropSet(props map[string][]string) error { return metadata.ValidatePropSet(props) }

// propSetGuard is the early refusal PutWithOptions runs: an illegal
// matrix set must die as a 400 with zero side effects (the same
// atomic-rejection posture every other input shape upholds). The wrap adds
// the addressing context operators log without changing the sentinel.
func propSetGuard(repoKey, path string, props map[string][]string) error {
	if err := metadata.ValidatePropSet(props); err != nil {
		return fmt.Errorf("deploy %s/%s: %w", repoKey, path, err)
	}
	return nil
}
