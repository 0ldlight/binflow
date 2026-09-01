package search

// AST — the output contract of Parse and the input contract of the T-411
// planner (ADR-0043 pt 1/10: the AST types are the seam between the language
// front-end and the AST→IR compiler; both sides are pure functions).
//
// Consumption notes for T-411:
//   - Every field reference is registry-resolved: FieldRef.ID is the closed-set
//     key (FieldRepo, FieldActualSHA1, ...) that maps onto the
//     metadata.NodeQuery field enum; FieldRef.Name preserves the wire form for
//     projection keys (include("modified") and include("updated") share a
//     column but must echo their own key).
//   - Query.Criteria is never nil; the empty criteria items.find({}) parses to
//     &And{} — match-all, per the official grammar (ADR-0043 pt 2).
//   - Implicit conjunctions ({"repo":"a","path":"b"}) normalize to And nodes;
//     a $and/$or whose value is a single object yields And/Or with one child.
//     Array elements are the element objects themselves (And wrappers around
//     their pairs) — the tree keeps the written shape, boolean folding is the
//     planner's. $msp keeps its own node: its conditions must bind the SAME
//     property instance, unlike a plain And over property conditions
//     (aql.md §2.3).
//   - Values are validated literals: dates are ISO8601/partial strings,
//     $last/$before values arrive with Period already parsed, integers arrive
//     as int64 (quoted numbers included — aql.md §2.5). Wildcards inside
//     $match/$nmatch strings stay raw ("*", "?"); translating them is
//     match.go's job (T-411), one translator for AQL and the legacy pattern
//     endpoint alike.

// Query is one parsed items.find query with its suffix chain.
type Query struct {
	// Domain is the query entry domain; only "items" parses successfully.
	Domain string
	// Criteria is the find() predicate tree (never nil; &And{} for {}).
	Criteria Criteria
	// Include is the .include() projection list in query order; empty when
	// the suffix is absent (T-411 then projects the default output set).
	Include []IncludeField
	// Sort is the .sort() key list in query order (stable tiebreak by
	// (repo, path) is the engine's business, ADR-0043 pt 3).
	Sort []SortKey
	// Offset/Limit carry HasOffset/HasLimit because 0 is a legal value for
	// both and "absent" must stay distinguishable (range.limit echo omits
	// the key when absent — aql.md §3.2).
	Offset    int64
	HasOffset bool
	Limit     int64
	HasLimit  bool
}

// Criteria is the sealed predicate tree. Type-switch over And, Or, Compare,
// PropMatch, MSP.
type Criteria interface{ isCriteria() }

// And is a conjunction: explicit $and, comma-separated pairs (implicit $and,
// aql.md §2.4), or the top-level criteria object. One child is normal.
type And struct{ Children []Criteria }

// Or is a disjunction ($or); one child is normal (object-valued $or).
type Or struct{ Children []Criteria }

// Compare is a comparator over a registry field, including the property
// long forms property.key / property.value (their FieldRef.ID is
// FieldPropertyKey / FieldPropertyValue, domain property).
type Compare struct {
	Field FieldRef
	Op    Operator
	Value Value
}

// PropMatch is the @key shorthand: {"@<key>": <value>} or {"@*": <value>}.
// WildcardKey marks the @* catch-all ("any property with this value"); a
// value of "*" means "this key with any value" (key existence, aql.md §2.3)
// and stays an ordinary string Value.
type PropMatch struct {
	Key         string
	WildcardKey bool
	Op          Operator
	Value       Value
}

// MSP is $msp: every condition must be satisfied by the same single property
// instance. Conditions are validated property-scoped criteria (PropMatch or
// Compare on property.* fields, possibly And-wrapped).
type MSP struct{ Conditions []Criteria }

// FieldRef is a registry-resolved field reference.
type FieldRef struct {
	ID     FieldID
	Name   string
	Domain Domain
}

// IncludeField is one .include() argument.
type IncludeField struct {
	// Raw is the argument as written ("name", "*", "@license").
	Raw string
	// Star marks the "*" projection (all fields).
	Star bool
	// PropKey is set for the "@<key>" property projection; "@*" leaves it
	// "*". Takes precedence over Field.
	PropKey string
	// Field is the registry resolution for plain field arguments; zero
	// (ID "") when Star or PropKey is set.
	Field FieldRef
}

// SortKey is one .sort() entry.
type SortKey struct {
	Field FieldRef
	Asc   bool
}

// ValueKind is the literal kind actually written in the query.
type ValueKind int

// LitString, LitNumber and LitNull are the literal kinds a query can carry.
const (
	LitString ValueKind = iota
	LitNumber
	LitNull
)

// Value is a validated criteria literal.
type Value struct {
	Kind ValueKind
	// Str is the decoded string for LitString; Raw is the literal as written
	// (diagnostics).
	Str string
	Raw string
	// Int is set for integral LitNumber values (quoted digits included).
	Int int64
	// Period is set for $last/$before operands (already validated).
	Period *Period
}

// Period is a parsed relative-date operand: 3 days, 10 minutes, 1 mo...
type Period struct {
	Count int64
	Unit  TimeUnit
}

// TimeUnit is the canonicalized relative-time unit (aql.md §2.4 table; the
// millisecond unit registers the implemented spelling "millis", not the
// official table's "mills" typo — spec registers implementation behavior).
type TimeUnit string

// UnitMillis through UnitYears are the canonicalized relative-time units
// (aql.md §2.4).
const (
	UnitMillis  TimeUnit = "millis"
	UnitSeconds TimeUnit = "seconds"
	UnitMinutes TimeUnit = "minutes"
	UnitDays    TimeUnit = "days"
	UnitWeeks   TimeUnit = "weeks"
	UnitMonths  TimeUnit = "months"
	UnitYears   TimeUnit = "years"
)

func (*And) isCriteria()       {}
func (*Or) isCriteria()        {}
func (*Compare) isCriteria()   {}
func (*PropMatch) isCriteria() {}
func (*MSP) isCriteria()       {}
