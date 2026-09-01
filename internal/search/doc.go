// Package search is the AQL language front-end, planner and engine
// (T-409/T-411/T-413, FR-133.1/.2/.4): a hand-written lexer and
// recursive-descent parser that turn AQL query text into a
// registry-validated AST (plus the structured QueryError the HTTP and
// console layers present), the PlanQuery compiler that lowers the AST onto
// the metadata.NodeQuery IR executed by metadata.NodeQueryer, and the
// Engine entry that weaves the caller's read scope into the query and runs
// it behind the K63 resource gate — per ADR-0043.
//
// The package does no IO of its own: no file or network access, no
// database handle. Every runtime dependency that is not pure is injected:
// the clock $last/$before resolve against (PlanOptions.Now /
// EngineOptions.Now), the virtual-repository resolver (PlanOptions.Virtual
// / EngineOptions.Virtual), the query executor (EngineOptions.Nodes) and
// the two-stage read ACL seam (EngineOptions.ACL — repo.Service satisfies
// it structurally). The engine measures its own duration but leaves
// logging and metrics to the transport layer (T-415), keeping the
// zero-IO contract whole.
//
// Behavioral authority is docs/reverse/aql.md (T-407): the item-domain
// field closure (§2.2), property forms (§2.3), operator closure (§2.4, no
// $not), suffix chain and order (§2.5), the output projections (§3), the
// verbatim error copy (§4) and the resource-gate calibration (§5).
package search
