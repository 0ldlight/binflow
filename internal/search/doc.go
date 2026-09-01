// Package search is the AQL language front-end and planner (T-409/T-411,
// FR-133.1/.2): a hand-written lexer and recursive-descent parser that turn
// AQL query text into a registry-validated AST (plus the structured
// QueryError the HTTP and console layers present), and the PlanQuery
// compiler that lowers the AST onto the metadata.NodeQuery IR executed by
// metadata.NodeQueryer, per ADR-0043.
//
// The package does no IO of its own: no file or network access, no
// database handle — metadata is imported for the IR types only. The two
// runtime dependencies that are not pure are injected: the clock $last/
// $before resolve against (PlanOptions.Now) and the virtual-repository
// resolver (PlanOptions.Virtual).
//
// Behavioral authority is docs/reverse/aql.md (T-407): the item-domain
// field closure (§2.2), property forms (§2.3), operator closure (§2.4, no
// $not), suffix chain and order (§2.5), the output projections (§3), and
// the verbatim error copy (§4, evidence t407-evidence/*).
package search
