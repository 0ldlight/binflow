package build

import "github.com/lzwzzy/binflow/internal/metadata"

// Store is the persistence seam the build service consumes: metadata's
// BuildStore sub-store over the 024 table family, the ScheduleStore /
// webhook-store precedent — a dumb ledger whose only interpretation is the
// four-tuple addressing. The alias keeps the service's signatures in
// domain terms; the assembly layer passes metadata.Store.Builds() here
// (ADR-0045 decision 1: metadata is a sanctioned dependency, the seam is
// its public face).
type Store = metadata.BuildStore
