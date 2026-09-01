// Package search is the AQL language front-end (T-409, FR-133.1): a
// hand-written lexer and recursive-descent parser that turn AQL query text
// into a registry-validated AST, plus the structured QueryError the HTTP and
// console layers present. It is a pure function package — no IO, no clock,
// no database; the AST is the seam the T-411 planner (AST→IR→parameterized
// SQL) consumes, per ADR-0043.
//
// Behavioral authority is docs/reverse/aql.md (T-407): the item-domain field
// closure (§2.2), property forms (§2.3), operator closure (§2.4, no $not),
// suffix chain and order (§2.5), and the verbatim error copy (§4, evidence
// t407-evidence/*).
package search
