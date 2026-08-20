// Package console serves the embedded web console (ADR-0014 as amended by
// the T-108 errata): the SPA mount redirect (/binflow -> /binflow/ui/), the
// /binflow/ui/** segment (shell + history fallback, no-cache) and the shared
// /binflow/assets/** fingerprinted-asset mount (immutable). The bundle is
// built from web/ by `make console` and go:embed-ed here; the committed
// dist/placeholder.html keeps a node-less checkout buildable.
package console
