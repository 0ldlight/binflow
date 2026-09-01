package search

import (
	"strconv"
	"strings"
)

// Parse parses one AQL query text into the AST (T-409 language front-end).
// It is a pure function: no IO, no clock, no database — relative dates stay
// symbolic Periods and the T-411 planner/compiler consumes the result.
//
// Grammar (ADR-0043 pt 2 EBNF, literal contract calibrated by aql.md):
//
//	query   = domain "." "find" "(" criteria ")" { suffix } EOF
//	domain  = "items"                        (* M15's only entry domain *)
//	criteria, pair, array, comparator — see parseObject/parsePair below.
//	suffix  = "." ( "include" "(" fields ")"
//	             | "sort" "(" "{" ("$asc"|"$desc") ":" "[" fields "]" "}" ")"
//	             | "offset" "(" int ")" | "limit" "(" int ")" )
//
// Calibrations from aql.md (the literal-contract authority over the ADR's
// provisional EBNF): $not is NOT part of the language (§0-1 — the ADR EBNF
// line for it is superseded, flagged in the ticket log); $msp is in
// (§2.3); the operator closure is §2.4 (incl. $last/$before); the suffix
// chain order include→sort→offset→limit is enforced (§2.5, evidence v1c).
//
// Every failure returns a *QueryError (never a bare error); syntax failures
// carry the verbatim E1 copy anchored at the offending token.

// MaxQueryLen is the AQL query length cap (aql.md §5: official default
// 6,000 chars — BinFlow adopts the official value).
const MaxQueryLen = 6000

// Parse runs the lexer and parser over query. On failure the error is always
// a *QueryError (use errors.As to unwrap it).
func Parse(query string) (*Query, error) {
	p, qe := newParser(query)
	if qe != nil {
		return nil, qe
	}
	if qe := p.run(); qe != nil {
		return nil, qe
	}
	return p.result, nil
}

type parser struct {
	query  string
	toks   []token
	i      int
	domain string
	result *Query
	// includeItemSeen remembers whether .include() carried an item-domain
	// field (which replaces the domain's default output set — aql.md §2.5);
	// the sort validator needs it. includeStar marks the "*" projection.
	includeItemSeen bool
	includeStar     bool
	includeExtra    []string // non-item fields named by include
	lastRank        int      // suffix chain order state (1..4)
}

func newParser(query string) (*parser, *QueryError) {
	if len(query) > MaxQueryLen {
		return nil, &QueryError{
			Kind: ErrQueryTooLong,
			Pos:  0,
			Msg:  msgQueryTooLong,
		}
	}
	toks, err := lexAll(query)
	if err != nil {
		return nil, err
	}
	return &parser{query: query, toks: toks}, nil
}

func (p *parser) cur() token  { return p.toks[p.i] }
func (p *parser) next() token { t := p.toks[p.i]; p.i++; return t }

// run parses the whole query into p.result.
func (p *parser) run() *QueryError {
	// Domain: one bare identifier; anything other than "items" is an honest
	// unsupported-domain rejection naming what was written.
	dt := p.cur()
	if dt.kind != tkIdent {
		return p.syntaxErr(dt.pos)
	}
	p.domain = dt.text
	if dt.text != "items" {
		msg := "AQL domain not supported: " + dt.text + " (BinFlow AQL supports: items"
		if hint, ok := unsupportedDomains[dt.text]; ok {
			msg += "; " + hint
		}
		msg += ")"
		return p.queryErr(ErrUnsupportedDomain, dt.text, "", dt.pos, "%s", msg)
	}
	p.next()
	if e := p.expectPunct("."); e != nil {
		return e
	}
	if w := p.cur(); !w.isWord("find") {
		return p.syntaxErr(w.pos)
	}
	p.next()
	if e := p.expectPunct("("); e != nil {
		return e
	}
	if !p.cur().isPunct("{") {
		return p.syntaxErr(p.cur().pos)
	}
	crit, err := p.parseObject(false)
	if err != nil {
		return err
	}
	if e := p.expectPunct(")"); e != nil {
		return e
	}
	p.result = &Query{Domain: "items", Criteria: crit}
	for p.cur().isPunct(".") {
		if err := p.parseSuffix(); err != nil {
			return err
		}
	}
	if p.cur().kind != tkEOF {
		return p.syntaxErr(p.cur().pos)
	}
	return nil
}

func (p *parser) expectPunct(c string) *QueryError {
	if t := p.cur(); t.isPunct(c) {
		p.i++
		return nil
	}
	return p.syntaxErr(p.cur().pos)
}

// suffixRank enforces the chain order include → sort → offset → limit
// (aql.md §2.5; a repeated or out-of-order suffix is a syntax error, E1).
func suffixRank(name string) int {
	switch name {
	case "include":
		return 1
	case "sort":
		return 2
	case "offset":
		return 3
	case "limit":
		return 4
	}
	return 0
}

func (p *parser) parseSuffix() *QueryError {
	p.next() // "."
	name := p.cur()
	if name.kind != tkIdent {
		return p.syntaxErr(name.pos)
	}
	if reason, ok := notSupportedSuffixes[name.text]; ok {
		return p.queryErr(ErrUnsupportedSuffix, p.domain, "", name.pos,
			"AQL suffix not supported: .%s (%s)", name.text, reason)
	}
	rank := suffixRank(name.text)
	if rank == 0 {
		// Unknown method name: plain syntax error, E1 anchored at the name
		// (same anchoring as the live v1c sample).
		return p.syntaxErr(name.pos)
	}
	if rank <= p.lastRank {
		return p.syntaxErr(name.pos)
	}
	p.lastRank = rank
	p.next()
	if e := p.expectPunct("("); e != nil {
		return e
	}
	switch name.text {
	case "include":
		return p.parseInclude()
	case "sort":
		return p.parseSort()
	case "offset":
		return p.parseWindow("offset")
	case "limit":
		return p.parseWindow("limit")
	}
	return p.syntaxErr(name.pos)
}

// parseObject parses a criteria object. propertyOnly restricts pairs to the
// property domain ($msp inner conditions).
func (p *parser) parseObject(propertyOnly bool) (Criteria, *QueryError) {
	open := p.cur()
	if !open.isPunct("{") {
		return nil, p.syntaxErr(open.pos)
	}
	p.next()
	var children []Criteria
	for !p.cur().isPunct("}") {
		if p.cur().kind == tkEOF {
			return nil, p.syntaxErr(p.cur().pos)
		}
		c, err := p.parsePair(propertyOnly)
		if err != nil {
			return nil, err
		}
		children = append(children, c)
		if p.cur().isPunct(",") {
			p.next()
			continue
		}
		break
	}
	if e := p.expectPunct("}"); e != nil {
		return nil, e
	}
	return &And{Children: children}, nil
}

// parsePair parses one "key : value" pair of a criteria object.
func (p *parser) parsePair(propertyOnly bool) (Criteria, *QueryError) {
	key := p.cur()
	switch key.kind {
	case tkDollar:
		return p.parseDollarPair(key)
	case tkPunct:
		if key.isPunct("@") {
			return p.parseAtPair(key)
		}
		return nil, p.syntaxErr(key.pos)
	case tkIdent, tkString:
		// A quoted key starting with $ is an operator/logical word
		// ("$and", "$not" — the wire form every evidence sample uses);
		// everything else is a field reference.
		if key.kind == tkString && strings.HasPrefix(key.text, "$") {
			return p.parseDollarPair(key)
		}
		return p.parseFieldPair(key, propertyOnly)
	case tkEOF:
		return nil, p.syntaxErr(key.pos)
	default:
		return nil, p.syntaxErr(key.pos)
	}
}

// parseDollarPair handles $and / $or / $msp and rejects other operators in
// key position.
func (p *parser) parseDollarPair(key token) (Criteria, *QueryError) {
	switch key.text {
	case "$and", "$or":
		p.next()
		if e := p.expectPunct(":"); e != nil {
			return nil, e
		}
		children, err := p.parseObjectList()
		if err != nil {
			return nil, err
		}
		if key.text == "$or" {
			return &Or{Children: children}, nil
		}
		return &And{Children: children}, nil
	case "$msp":
		p.next()
		if e := p.expectPunct(":"); e != nil {
			return nil, e
		}
		if !p.cur().isPunct("[") {
			return nil, p.syntaxErr(p.cur().pos)
		}
		conds, err := p.parseObjectList()
		if err != nil {
			return nil, err
		}
		for _, c := range conds {
			if ok, offender := propertyScoped(c); !ok {
				return nil, p.queryErr(ErrBadValue, p.domain, offender, key.pos,
					"AQL $msp conditions may only contain property criteria, got: %s", offender)
			}
		}
		return &MSP{Conditions: conds}, nil
	}
	if reason, ok := notSupportedOps[key.text]; ok {
		return nil, p.queryErr(ErrBadOperator, p.domain, "", key.pos,
			"AQL operator not supported: %s (%s)", key.text, reason)
	}
	return nil, p.syntaxErr(key.pos)
}

// parseObjectList parses "{" object "}" or "[" object {"," object} "]"
// (the $and/$or/$msp value forms). An empty array is rejected: it is
// degenerate and unspecified on the Artifactory side (logged as such).
func (p *parser) parseObjectList() ([]Criteria, *QueryError) {
	t := p.cur()
	switch {
	case t.isPunct("{"):
		c, err := p.parseObject(false)
		if err != nil {
			return nil, err
		}
		return []Criteria{c}, nil
	case t.isPunct("["):
		p.next()
		var out []Criteria
		for !p.cur().isPunct("]") {
			if p.cur().kind == tkEOF {
				return nil, p.syntaxErr(p.cur().pos)
			}
			if !p.cur().isPunct("{") {
				return nil, p.syntaxErr(p.cur().pos)
			}
			c, err := p.parseObject(false)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
			if p.cur().isPunct(",") {
				p.next()
				continue
			}
			break
		}
		if e := p.expectPunct("]"); e != nil {
			return nil, e
		}
		if len(out) == 0 {
			// Degenerate: the grammar requires at least one criteria object
			// in a $and/$or/$msp array (Artifactory behavior unregistered —
			// honest E1 rejection rather than invented semantics).
			return nil, p.syntaxErr(t.pos)
		}
		return out, nil
	default:
		return nil, p.syntaxErr(t.pos)
	}
}

// parseAtPair parses the {"@<key>": value} property shorthand. Dots are
// accepted inside the key (property keys like build.name appear as keys on
// the legacy prop search — aql.md §8.2). @* is the catch-all.
func (p *parser) parseAtPair(key token) (Criteria, *QueryError) {
	p.next() // "@"
	name := p.cur()
	if name.kind != tkIdent && !name.isPunct("*") {
		return nil, p.syntaxErr(name.pos)
	}
	p.next()
	// Dotted property key: consume ".ident" segments.
	propName := name.text
	for p.cur().isPunct(".") && p.toks[p.i+1].kind == tkIdent {
		p.next()
		propName += "." + p.next().text
	}
	return p.propPair(propName, key.pos)
}

// propPair builds the PropMatch node for an @key pair (key already consumed)
// and parses its value side.
func (p *parser) propPair(propName string, pos int) (Criteria, *QueryError) {
	if propName == "" {
		return nil, p.queryErr(ErrUnknownField, p.domain, "@", pos,
			"Unknown AQL property key: @")
	}
	if e := p.expectPunct(":"); e != nil {
		return nil, e
	}
	wild := propName == "*"
	op, val, err := p.parseCriteriaValue(keyField{propAt: propName, star: wild}, stringOps)
	if err != nil {
		return nil, err
	}
	return &PropMatch{
		Key:         propName,
		WildcardKey: wild,
		Op:          op,
		Value:       val,
	}, nil
}

// parseFieldPair parses {"<field>": value} — a registry field, dotted path
// included (property.key, property.value, stat.*). Bare and quoted field keys
// are both accepted (live traffic is bare — t407-evidence v1c/v15; aql.md
// §2.5 registers the double-quoted form).
func (p *parser) parseFieldPair(key token, propertyOnly bool) (Criteria, *QueryError) {
	p.next()
	name := key.text
	// Quoted "@key" keys are the same property shorthand as the bare form.
	if key.kind == tkString && strings.HasPrefix(name, "@") {
		prop := name[1:]
		if prop == "" {
			return nil, p.queryErr(ErrUnknownField, p.domain, name, key.pos,
				"Unknown AQL property key: %s", name)
		}
		return p.propPair(prop, key.pos)
	}
	if p.cur().isPunct(".") && p.toks[p.i+1].kind == tkIdent {
		p.next()
		name += "." + p.next().text
		if p.cur().isPunct(".") {
			// Three or more segments only occur in build-family paths
			// (artifact.module.build.@os — remote scope), outside the subset.
			return nil, p.syntaxErr(p.cur().pos)
		}
	}
	f, ok := lookupField(name)
	if !ok {
		return nil, p.queryErr(ErrUnknownField, p.domain, name, key.pos,
			"Unknown AQL field: %s", name)
	}
	if f.Unsupported != "" {
		return nil, p.queryErr(ErrUnsupportedField, string(f.Domain), name, key.pos,
			"AQL field not supported yet: %s (%s)", name, f.Unsupported)
	}
	if propertyOnly && f.Domain != DomainProperty {
		return nil, p.queryErr(ErrBadValue, string(f.Domain), name, key.pos,
			"AQL $msp conditions may only contain property criteria, got: %s", name)
	}
	if len(f.Ops) == 0 {
		// virtual_repos: a logical output field, not a criteria target.
		return nil, p.queryErr(ErrUnsupportedField, string(f.Domain), name, key.pos,
			"AQL field is output-only, not usable in criteria: %s", name)
	}
	if e := p.expectPunct(":"); e != nil {
		return nil, e
	}
	op, val, err := p.parseCriteriaValue(keyField{field: f}, f.Ops)
	if err != nil {
		return nil, err
	}
	return &Compare{
		Field: FieldRef{ID: f.ID, Name: f.Name, Domain: f.Domain},
		Op:    op,
		Value: val,
	}, nil
}

// keyField carries the criteria target into value parsing: either a registry
// field or an @key property shorthand.
type keyField struct {
	field  Field
	propAt string // key for the @key form
	star   bool   // @* catch-all
}

func (k keyField) displayName() string {
	if k.field.Name != "" {
		return k.field.Name
	}
	return "@" + k.propAt
}

// errDomain is the QueryError.Domain for a field-scoped failure: the field's
// entity domain, or the query domain when the target is an @key shorthand.
func (k keyField) errDomain(fallback string) string {
	if k.field.Name != "" {
		return string(k.field.Domain)
	}
	return fallback
}

// parseCriteriaValue parses the value side of a pair: a comparator object
// {"$op": scalar} or a bare scalar (implicit $eq). allowedOps gates the
// comparator; the scalar's shape is validated against the target.
func (p *parser) parseCriteriaValue(k keyField, allowedOps []Operator) (Operator, Value, *QueryError) {
	if p.cur().isPunct("{") {
		return p.parseComparator(k, allowedOps)
	}
	val, err := p.parseScalar(k, OpEq)
	if err != nil {
		return "", Value{}, err
	}
	return OpEq, val, nil
}

func (p *parser) parseComparator(k keyField, allowedOps []Operator) (Operator, Value, *QueryError) {
	p.next() // "{"
	opTok := p.cur()
	opText := ""
	switch {
	case opTok.kind == tkDollar:
		opText = opTok.text
	case opTok.kind == tkString && strings.HasPrefix(opTok.text, "$"):
		opText = opTok.text
	default:
		return "", Value{}, p.syntaxErr(opTok.pos)
	}
	switch opText {
	case "$and", "$or", "$msp", "$asc", "$desc":
		return "", Value{}, p.syntaxErr(opTok.pos)
	}
	if reason, ok := notSupportedOps[opText]; ok {
		return "", Value{}, p.queryErr(ErrBadOperator, k.errDomain(p.domain), k.displayName(), opTok.pos,
			"AQL operator not supported: %s (%s)", opText, reason)
	}
	op := Operator(opText)
	if !operatorKnown(op) {
		return "", Value{}, p.queryErr(ErrBadOperator, k.errDomain(p.domain), k.displayName(), opTok.pos,
			"Unknown AQL operator: %s", opText)
	}
	if !opAllowedFor(op, allowedOps) {
		return "", Value{}, p.queryErr(ErrBadOperator, k.errDomain(p.domain), k.displayName(), opTok.pos,
			"AQL operator not allowed: %s on field %s (%s)", opText, k.displayName(), opReasonFor(op, k.field.Kind))
	}
	p.next()
	if e := p.expectPunct(":"); e != nil {
		return "", Value{}, e
	}
	val, err := p.parseScalar(k, op)
	if err != nil {
		return "", Value{}, err
	}
	if e := p.expectPunct("}"); e != nil {
		return "", Value{}, e
	}
	// Exactly one operator per comparator object (ADR-0043 pt 2 grammar):
	// the enclosing criteria object's "}" must follow.
	if !p.cur().isPunct("}") {
		return "", Value{}, p.syntaxErr(p.cur().pos)
	}
	return op, val, nil
}

// parseScalar parses one scalar literal and validates it against the target
// field and operator.
func (p *parser) parseScalar(k keyField, op Operator) (Value, *QueryError) {
	t := p.cur()
	var val Value
	switch t.kind {
	case tkString:
		p.next()
		val = Value{Kind: LitString, Str: t.text, Raw: t.text}
	case tkNumber:
		p.next()
		val = Value{Kind: LitNumber, Int: t.intVal, Raw: t.text}
	case tkIdent:
		p.next()
		val = Value{Kind: LitNull, Raw: t.text}
		if t.text == "true" || t.text == "false" {
			return val, p.queryErr(ErrBadValue, k.errDomain(p.domain), k.displayName(), t.pos,
				"Invalid value for field %s: %s (boolean is not an AQL value)", k.displayName(), t.text)
		}
		if t.text != "null" {
			return val, p.syntaxErr(t.pos)
		}
	default:
		return val, p.syntaxErr(t.pos)
	}
	integral := false
	if val.Kind == LitNumber {
		_, err := strconv.ParseInt(val.Raw, 10, 64)
		integral = err == nil
	}
	if err := p.validateValue(k, op, &val, integral, t.pos); err != nil {
		return Value{}, err
	}
	return val, nil
}

// validateValue applies the field-kind rules to a parsed literal:
//   - string fields take strings only; int/long fields take integers
//     (quoted digits included — aql.md §2.5);
//   - date fields take ISO8601 (partial precision allowed) strings, or a
//     relative period under $last/$before (aql.md §2.4);
//   - the type enum takes its closed value set;
//   - null is registered for the statistics domain only (M16) — rejected
//     here, honestly, for every M15 field.
func (p *parser) validateValue(k keyField, op Operator, val *Value, integral bool, pos int) *QueryError {
	name := k.displayName()
	target := k.field
	if k.propAt != "" {
		// Property values are strings; no kind ladder applies.
		switch val.Kind {
		case LitString:
			return nil
		case LitNumber:
			return p.queryErr(ErrBadValue, p.domain, name, pos,
				"Invalid value for property %s: %s (string required)", name, val.Raw)
		default:
			return p.queryErr(ErrBadValue, p.domain, name, pos,
				"AQL null values are not supported for property %s", name)
		}
	}
	if val.Kind == LitNull {
		return p.queryErr(ErrBadValue, string(target.Domain), name, pos,
			"AQL null values are not supported for field %s (only statistics-domain fields use null)", name)
	}
	switch target.Kind {
	case KindString:
		if val.Kind != LitString {
			return p.queryErr(ErrBadValue, string(target.Domain), name, pos,
				"Invalid value for field %s: %s (string required)", name, val.Raw)
		}
	case KindInt, KindLong:
		if val.Kind == LitString {
			n, err := strconv.ParseInt(val.Str, 10, 64)
			if err != nil {
				return p.queryErr(ErrBadValue, string(target.Domain), name, pos,
					"Invalid number for field %s: %s (integer required)", name, val.Str)
			}
			val.Int = n // quoted digits carry the integer too (aql.md §2.5)
			return nil
		}
		if !integral {
			return p.queryErr(ErrBadValue, string(target.Domain), name, pos,
				"Invalid number for field %s: %s (integer required)", name, val.Raw)
		}
	case KindDate:
		if val.Kind != LitString {
			return p.queryErr(ErrBadValue, string(target.Domain), name, pos,
				"Invalid date value for field %s: %s", name, val.Raw)
		}
		if op == OpLast || op == OpBefore {
			per, ok := parsePeriod(val.Str)
			if !ok {
				return p.queryErr(ErrBadValue, string(target.Domain), name, pos,
					msgInvalidRelativeDate, val.Str)
			}
			val.Period = &per
			return nil
		}
		if !validPartialDate(val.Str) {
			return p.queryErr(ErrBadValue, string(target.Domain), name, pos,
				"Invalid date value for field %s: %s", name, val.Str)
		}
	case KindEnum:
		if val.Kind != LitString || !typeValues[val.Str] {
			return p.queryErr(ErrBadValue, string(target.Domain), name, pos,
				"Invalid value for field %s: %s (allowed: any, file, folder)", name, val.Raw)
		}
	}
	return nil
}

// parseInclude parses the include() argument list. Quoted names are the
// canonical form (t407-evidence); bare identifiers and the bare @key form
// are accepted as the liberal arm of the same grammar.
func (p *parser) parseInclude() *QueryError {
	for {
		if p.cur().isPunct(")") {
			p.next()
			return nil
		}
		inc, err := p.parseIncludeField()
		if err != nil {
			return err
		}
		p.result.Include = append(p.result.Include, inc)
		if p.cur().isPunct(",") {
			p.next()
			continue
		}
		if e := p.expectPunct(")"); e != nil {
			return e
		}
		return nil
	}
}

func (p *parser) parseIncludeField() (IncludeField, *QueryError) {
	t := p.cur()
	var raw string
	switch t.kind {
	case tkString, tkIdent:
		p.next()
		raw = t.text
	case tkPunct:
		if !t.isPunct("@") {
			return IncludeField{}, p.syntaxErr(t.pos)
		}
		p.next()
		n := p.cur()
		if n.kind != tkIdent && !n.isPunct("*") {
			return IncludeField{}, p.syntaxErr(n.pos)
		}
		p.next()
		return p.makePropInclude("@"+n.text, n.text, t.pos)
	default:
		return IncludeField{}, p.syntaxErr(t.pos)
	}
	switch {
	case raw == "*":
		p.includeStar = true
		return IncludeField{Raw: raw, Star: true}, nil
	case len(raw) > 0 && raw[0] == '@':
		return p.makePropInclude(raw, raw[1:], t.pos)
	}
	f, ok := lookupField(raw)
	if !ok {
		return IncludeField{}, p.queryErr(ErrUnknownField, p.domain, raw, t.pos,
			"Unknown AQL field: %s", raw)
	}
	if f.Unsupported != "" {
		return IncludeField{}, p.queryErr(ErrUnsupportedField, string(f.Domain), raw, t.pos,
			"AQL field not supported yet: %s (%s)", raw, f.Unsupported)
	}
	if !f.Projectable {
		return IncludeField{}, p.queryErr(ErrUnsupportedField, string(f.Domain), raw, t.pos,
			"AQL field cannot be used in include: %s", raw)
	}
	if f.Domain == DomainItem {
		// First item-domain field in include replaces the domain's default
		// output set (aql.md §2.5) — recorded for the sort validator.
		p.includeItemSeen = true
	} else {
		p.includeExtra = append(p.includeExtra, f.Name)
	}
	return IncludeField{
		Raw:   raw,
		Field: FieldRef{ID: f.ID, Name: f.Name, Domain: f.Domain},
	}, nil
}

func (p *parser) makePropInclude(raw, key string, pos int) (IncludeField, *QueryError) {
	if key == "" {
		return IncludeField{}, p.queryErr(ErrUnknownField, p.domain, raw, pos,
			"Unknown AQL property projection: %s", raw)
	}
	return IncludeField{Raw: raw, PropKey: key}, nil
}

// parseSort parses sort({"$asc"|"$desc": ["<field>", ...]}) and runs the two
// Artifactory validator rules verbatim (aql.md §2.5): sort fields must be
// result (output) fields, and must be unique.
func (p *parser) parseSort() *QueryError {
	if e := p.expectPunct("{"); e != nil {
		return e
	}
	dir := p.cur()
	if !dir.isOpWord("$asc") && !dir.isOpWord("$desc") {
		return p.syntaxErr(dir.pos)
	}
	asc := dir.isOpWord("$asc")
	p.next()
	if e := p.expectPunct(":"); e != nil {
		return e
	}
	if e := p.expectPunct("["); e != nil {
		return e
	}
	seen := map[string]bool{}
	count := 0
	for {
		t := p.cur()
		if t.isPunct("]") {
			p.next()
			break
		}
		if t.kind == tkEOF {
			return p.syntaxErr(t.pos)
		}
		if t.kind != tkString && t.kind != tkIdent {
			return p.syntaxErr(t.pos)
		}
		p.next()
		f, ok := lookupField(t.text)
		if !ok {
			return p.queryErr(ErrUnknownField, p.domain, t.text, t.pos,
				"Unknown AQL field: %s", t.text)
		}
		if f.Unsupported != "" {
			return p.queryErr(ErrUnsupportedField, string(f.Domain), t.text, t.pos,
				"AQL field not supported yet: %s (%s)", t.text, f.Unsupported)
		}
		if !f.Sortable {
			return p.queryErr(ErrSortField, string(f.Domain), t.text, t.pos,
				"AQL field is not sortable: %s", t.text)
		}
		if !p.isResultField(f) {
			return p.queryErr(ErrSortField, string(f.Domain), t.text, t.pos,
				"%s", msgSortNotResultField)
		}
		if seen[t.text] {
			return p.queryErr(ErrSortField, string(f.Domain), t.text, t.pos,
				"%s", msgSortDuplicateField)
		}
		seen[t.text] = true
		count++
		p.result.Sort = append(p.result.Sort, SortKey{
			Field: FieldRef{ID: f.ID, Name: f.Name, Domain: f.Domain},
			Asc:   asc,
		})
		if p.cur().isPunct(",") {
			p.next()
		}
	}
	if count == 0 {
		// The grammar requires at least one sort field.
		return p.syntaxErr(dir.pos)
	}
	if e := p.expectPunct("}"); e != nil {
		return e
	}
	return p.expectPunct(")")
}

// isResultField reports whether f belongs to the effective output set:
// the include() item fields (or the domain defaults when include named no
// item field), plus every non-item field include pulled in.
func (p *parser) isResultField(f Field) bool {
	if p.includeStar {
		return true
	}
	if f.Domain != DomainItem {
		for _, n := range p.includeExtra {
			if n == f.Name {
				return true
			}
		}
		return false
	}
	if !p.includeItemSeen {
		for _, n := range defaultItemOutput {
			if n == f.Name {
				return true
			}
		}
		return false
	}
	for _, inc := range p.result.Include {
		if inc.Field.Name == f.Name {
			return true
		}
	}
	return false
}

// parseWindow parses offset(n)/limit(n): a bare or quoted non-negative
// integer (aql.md §2.5).
func (p *parser) parseWindow(name string) *QueryError {
	t := p.cur()
	var raw string
	switch t.kind {
	case tkNumber:
		p.next()
		raw = t.text
	case tkString:
		p.next()
		raw = t.text
	default:
		return p.syntaxErr(t.pos)
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return p.queryErr(ErrBadValue, p.domain, "", t.pos,
			"Invalid %s value: %s (non-negative integer required)", name, raw)
	}
	switch name {
	case "offset":
		p.result.Offset, p.result.HasOffset = n, true
	case "limit":
		p.result.Limit, p.result.HasLimit = n, true
	}
	return p.expectPunct(")")
}

// operatorKnown is the M15 comparator closure (aql.md §2.4 official set).
func operatorKnown(op Operator) bool {
	switch op {
	case OpEq, OpNe, OpGt, OpGte, OpLt, OpLte, OpMatch, OpNmatch, OpLast, OpBefore:
		return true
	}
	return false
}

func opAllowedFor(op Operator, allowed []Operator) bool {
	for _, o := range allowed {
		if o == op {
			return true
		}
	}
	return false
}

// opReasonFor explains why an operator is illegal, taking the field kind
// into account (the type enum has its own copy, aql.md §2.2/§2.4).
func opReasonFor(op Operator, kind FieldKind) string {
	switch {
	case kind == KindEnum:
		return "the type field supports $eq and $ne only"
	case op == OpMatch || op == OpNmatch:
		return "$match and $nmatch apply to string fields only"
	case op == OpLast || op == OpBefore:
		return "$last and $before apply to date fields only"
	}
	return "operator not allowed on this field"
}

// propertyScoped reports whether a $msp inner condition only constrains
// properties (PropMatch nodes, Compare on property.*, And-wrapped mixes),
// naming the first offender otherwise.
func propertyScoped(c Criteria) (bool, string) {
	switch n := c.(type) {
	case *PropMatch:
		return true, ""
	case *Compare:
		if n.Field.Domain == DomainProperty {
			return true, ""
		}
		return false, n.Field.Name
	case *And:
		for _, ch := range n.Children {
			if ok, off := propertyScoped(ch); !ok {
				return false, off
			}
		}
		return true, ""
	}
	return false, describeCriteria(c)
}

// describeCriteria renders a short diagnostic name for a criteria node.
func describeCriteria(c Criteria) string {
	switch n := c.(type) {
	case *And:
		return "$and"
	case *Or:
		return "$or"
	case *MSP:
		return "$msp"
	case *PropMatch:
		return "@" + n.Key
	case *Compare:
		return n.Field.Name
	}
	return "criteria"
}
