package bundle

import "github.com/lzwzzy/binflow/internal/metadata"

// Store is the persistence seam the bundle service consumes: metadata's
// BundleStore sub-store over the 025 table family, the BuildStore seam's
// exact precedent — a dumb ledger whose only interpretation is the
// (name, version) pair addressing and the UNIQUE-key fact. The alias keeps
// the service's signatures in domain terms; the assembly layer passes
// metadata.Store.Bundles() here (ADR-0046 decision 1: metadata is a
// sanctioned dependency, the seam is its public face).
type Store = metadata.BundleStore
