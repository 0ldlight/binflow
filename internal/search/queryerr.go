package search

import "fmt"

// QueryError is the structured, pre-envelope form of every AQL query failure
// (ADR-0043 pt 6). Parse produces it; the httpapi layer (T-415) is the single
// point that maps it onto the E-01 envelope `{"errors":[{status,message}]}`,
// and the console (FR-135/T-419) renders the same Msg inline. Carrying the
// domain/field names structurally is what makes the rejection honest: the
// caller can name what was refused instead of returning a pseudo-empty 200.
type QueryError struct {
	// Kind classifies the failure; it decides the HTTP status in T-415
	// (every kind defined here is a 400 — resource-gate errors live in the
	// engine, T-413, not in the parser).
	Kind ErrorKind
	// Domain names the offending AQL domain: the query entry domain for
	// ErrUnsupportedDomain, the field's entity domain for field errors
	// (e.g. "statistics" for stat.downloads), the query domain otherwise.
	Domain string
	// Field names the offending field, when the error is field-scoped.
	Field string
	// Pos is the byte offset into the query text where the failure was
	// detected; Segment is query[Pos:] — the residual sub-query embedded in
	// the E1 syntax-error copy (t407-evidence v1c/v03/v15 show Artifactory
	// reports exactly this slice).
	Pos     int
	Segment string
	// Msg is the final, ready-to-envelope message. It is assembled once, at
	// construction, by this package (single assembly point per ADR-0043).
	Msg string
}

// Error implements error.
func (e *QueryError) Error() string { return e.Msg }

// ErrorKind enumerates the parser-level failure classes.
type ErrorKind string

const (
	// ErrSyntax covers grammar and suffix-chain-order failures. Message is
	// the verbatim E1 copy from aql.md §4 (t407-evidence v1c/v14/v15).
	ErrSyntax ErrorKind = "syntax"
	// ErrQueryTooLong rejects queries over MaxQueryLen chars (aql.md §5,
	// official 6,000).
	ErrQueryTooLong ErrorKind = "query_too_long"
	// ErrUnsupportedDomain rejects query entry domains outside the M15
	// subset (items only) — C-layer enhanced copy, sanctioned by aql.md §2.1.
	ErrUnsupportedDomain ErrorKind = "unsupported_domain"
	// ErrUnknownField rejects field names not in the registry at all.
	ErrUnknownField ErrorKind = "unknown_field"
	// ErrUnsupportedField rejects known AQL fields with no BinFlow storage
	// source (modified_by, original_*, stat.*, internal ids) — the message
	// carries the registry reason.
	ErrUnsupportedField ErrorKind = "unsupported_field"
	// ErrBadOperator rejects unknown operators, operators outside the subset
	// ($not, $contains, case-insensitive variants) and operators illegal for
	// the field kind ($match on an int field, $last on a string field).
	ErrBadOperator ErrorKind = "bad_operator"
	// ErrBadValue rejects literals illegal for the field (type value set,
	// null, non-integer number, malformed date or relative period).
	ErrBadValue ErrorKind = "bad_value"
	// ErrUnsupportedSuffix rejects method-chain suffixes outside the M15
	// read-only subset (.distinct/.transitive/.delete/.update/.dryRun).
	ErrUnsupportedSuffix ErrorKind = "unsupported_suffix"
	// ErrSortField reports sort-section validation failures; the two message
	// constants below are verbatim from the Artifactory validator (§2.5).
	ErrSortField ErrorKind = "sort_field"
)

// Verbatim copy anchors (aql.md §4/§2.5 — do not reword; T-415 asserts these).
const (
	// e1Format is the universal Artifactory parse-error shape: original
	// query, then the residual sub-query from the failure position to the
	// end of the text (evidence: v1c, v03, v14, v15).
	e1Format = "Failed to parse query: %s, it looks like there is syntax error near the following sub-query: %s"
	// msgQueryTooLong (aql.md §4 E4).
	msgQueryTooLong = "AQL query is too long; please reduce the query length to less than 6000 chars"
	// msgSortNotResultField / msgSortDuplicateField (aql.md §2.5, verbatim).
	msgSortNotResultField = "Only the result fields are allowed to use in the sort section."
	msgSortDuplicateField = "Duplicate fields, all the fields in the sort section should be unique."
	// msgInvalidRelativeDate (aql.md §2.4, verbatim decompiled copy).
	msgInvalidRelativeDate = "Invalid relative date format for: %s"
)

// syntaxErr builds the E1-shaped error anchored at pos. what is a short
// internal description used only when pos cannot be anchored to a token
// (never surfaced — the E1 copy is the surface).
func (p *parser) syntaxErr(pos int) *QueryError {
	if pos < 0 || pos > len(p.query) {
		pos = len(p.query)
	}
	return &QueryError{
		Kind:    ErrSyntax,
		Domain:  p.domain,
		Pos:     pos,
		Segment: p.query[pos:],
		Msg:     fmt.Sprintf(e1Format, p.query, p.query[pos:]),
	}
}

// queryErr builds an enhanced (C-layer) rejection. aql.md §2.1 sanctions
// clearer-than-Artifactory copy for domain/field/operator refusals: the HTTP
// code stays 400, the message names the offender.
func (p *parser) queryErr(kind ErrorKind, domain, field string, pos int, format string, args ...any) *QueryError {
	if pos < 0 || pos > len(p.query) {
		pos = len(p.query)
	}
	return &QueryError{
		Kind:    kind,
		Domain:  domain,
		Field:   field,
		Pos:     pos,
		Segment: p.query[pos:],
		Msg:     fmt.Sprintf(format, args...),
	}
}
