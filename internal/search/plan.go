package search

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// Plan is the compiled, storage-ready form of one parsed query (T-411,
// FR-133.2 / ADR-0043 pt 3): Plan.Query is the IR handed to
// metadata.NodeQueryer, and the remaining fields are the engine- and
// envelope-facing echoes (T-413 executes Plan.Query and re-checks row
// visibility; T-415 renders rows through Output and the window echoes).
//
// Plan is a pure function of the AST plus PlanOptions — no IO of its own;
// the context exists for the injected virtual-repository resolver.
type Plan struct {
	// Query is the compiled IR (predicate tree, sort keys, window,
	// projection). Where is never nil and always carries the implicit
	// file predicate unless the query states its own type condition
	// (aql.md §2.2: a query without a type condition searches files).
	Query metadata.NodeQuery

	// Output is the projection echo list in query order: one entry per
	// rendered field, including the property projections and the
	// virtual_repos logical field that have no storage column (those the
	// engine resolves after the rows come back). The default output set —
	// no .include() — is the aql.md §3.3 list minus modified_by, which
	// has no storage source (ADR-0043 pt 2: registered unsupported, never
	// faked).
	Output []OutputField

	Offset    int64
	HasOffset bool
	Limit     int64
	HasLimit  bool
}

// OutputKind classifies one projection entry.
type OutputKind int

const (
	// OutputItem is a storage-backed item field (Field identifies it).
	OutputItem OutputKind = iota
	// OutputProp is a property projection; PropKey is the property key or
	// "*" for the catch-all (NodeQueryRow.Props carries the values).
	OutputProp
	// OutputVirtualRepos is the logical virtual_repos field: resolved
	// from the repository registry at runtime, never from node storage.
	OutputVirtualRepos
)

// OutputField is one .include() argument (or one default/star field) in
// echo order. Key is the wire spelling as written ("repo", "@license",
// "virtual_repos").
type OutputField struct {
	Key     string
	Kind    OutputKind
	Field   FieldID
	PropKey string
}

// PlanOptions carries the planner's runtime dependencies. Now is the clock
// $last/$before resolve against (a query using them without a clock is an
// error, not a silent zero time); Virtual expands repository values that
// name virtual repositories (aql.md §7-1) and may be nil — with no
// resolver every repo value passes through literally, which is the shape
// snapshot tests compile under.
type PlanOptions struct {
	Now     time.Time
	Virtual VirtualResolver
}

// VirtualResolver resolves virtual repository keys onto their materialized
// member sets (aql.md §7-1: a virtual key is a legal query VALUE — the
// compiler expands it transparently; the virtual repository is never a
// query entity because nodes carry no virtual rows). Members returns an
// empty slice for a key that is not a virtual repository (or a memberless
// one — both keep the original predicate, which matches no storage row and
// yields the honest empty set of §7-4). The engine (T-413) injects the
// implementation built on the repository service; T-412 lands it.
type VirtualResolver interface {
	Members(ctx context.Context, virtualKey string) ([]string, error)
}

// PlanQuery compiles one parsed query. Failures are engine-internal errors
// (mapper: 500-class) — every user-facing rejection happened in Parse; the
// only externally visible failure surface is a VirtualResolver error,
// wrapped with the offending key.
func PlanQuery(ctx context.Context, q *Query, opt PlanOptions) (*Plan, error) {
	if q == nil || q.Criteria == nil {
		return nil, fmt.Errorf("planner: nil query or criteria")
	}
	p := &planner{ctx: ctx, opt: opt}
	where, err := p.lower(q.Criteria)
	if err != nil {
		return nil, err
	}
	// The implicit file predicate: a query without a type condition
	// searches files (aql.md §2.2, live evidence v09). {"type":"any"}
	// counts as the query's own type condition and disables the default.
	if !hasTypeCondition(q.Criteria) {
		if and, ok := where.(*metadata.QueryAnd); ok {
			and.Children = append(and.Children, &metadata.QueryFolderTest{Folder: false})
		} else {
			where = &metadata.QueryAnd{Children: []metadata.NodePredicate{where, &metadata.QueryFolderTest{Folder: false}}}
		}
	}
	fields, props, output := buildProjection(q)
	sorts := make([]metadata.NodeSort, 0, len(q.Sort))
	for _, sk := range q.Sort {
		f, ok := storageFieldOf(sk.Field.ID)
		if !ok {
			return nil, fmt.Errorf("planner: sort field %q has no storage mapping", sk.Field.Name)
		}
		sorts = append(sorts, metadata.NodeSort{Field: f, Asc: sk.Asc})
	}
	return &Plan{
		Query: metadata.NodeQuery{
			Where:  where,
			Sort:   sorts,
			Offset: q.Offset, HasOffset: q.HasOffset,
			Limit: q.Limit, HasLimit: q.HasLimit,
			Fields: fields,
			Props:  props,
		},
		Output: output,
		Offset: q.Offset, HasOffset: q.HasOffset,
		Limit: q.Limit, HasLimit: q.HasLimit,
	}, nil
}

// planner carries the per-call dependencies through the recursion.
type planner struct {
	ctx context.Context
	opt PlanOptions
}

// lower compiles the criteria tree. The AST keeps the written shape — an
// array element is an And around its pairs, a single-object $and value
// yields one child — so boolean folding is done here: a one-child And/Or
// collapses into its child (a zero-child And is the top-level match-all
// marker and survives until the implicit file predicate lands on it).
func (p *planner) lower(c Criteria) (metadata.NodePredicate, error) {
	switch n := c.(type) {
	case *And:
		children, err := p.lowerChildren(n.Children)
		if err != nil {
			return nil, err
		}
		if len(children) == 1 {
			return children[0], nil
		}
		return &metadata.QueryAnd{Children: children}, nil
	case *Or:
		children, err := p.lowerChildren(n.Children)
		if err != nil {
			return nil, err
		}
		if len(children) == 0 {
			return nil, fmt.Errorf("planner: empty disjunction")
		}
		if len(children) == 1 {
			return children[0], nil
		}
		return &metadata.QueryOr{Children: children}, nil
	case *Compare:
		return p.lowerCompare(n)
	case *PropMatch:
		return &metadata.QueryPropExists{Conds: propMatchConds(n)}, nil
	case *MSP:
		conds, err := p.flattenProp(n.Conditions)
		if err != nil {
			return nil, err
		}
		return &metadata.QueryPropSame{Conds: conds}, nil
	}
	return nil, fmt.Errorf("planner: unknown criteria node %T", c)
}

// lowerChildren lowers a child list, dropping vacuous predicates.
func (p *planner) lowerChildren(in []Criteria) ([]metadata.NodePredicate, error) {
	children := make([]metadata.NodePredicate, 0, len(in))
	for _, ch := range in {
		l, err := p.lower(ch)
		if err != nil {
			return nil, err
		}
		if l != nil {
			children = append(children, l)
		}
	}
	return children, nil
}

// lowerCompare compiles one field comparator. A nil return (never an
// error) means the predicate is vacuous and should be dropped — only the
// {"type":"any"} arm produces it.
func (p *planner) lowerCompare(cmp *Compare) (metadata.NodePredicate, error) {
	v := cmp.Value
	switch cmp.Field.ID {
	case FieldRepo:
		return p.lowerRepo(cmp.Op, v.Str)
	case FieldType:
		return lowerType(cmp.Op, v.Str), nil
	case FieldCreated, FieldModified, FieldUpdated:
		f, _ := storageFieldOf(cmp.Field.ID)
		if v.Period != nil {
			return p.periodArm(f, cmp.Op, v.Period)
		}
		return p.dateArm(f, cmp.Op, v.Str)
	case FieldDepth, FieldSize:
		f, _ := storageFieldOf(cmp.Field.ID)
		return &metadata.QueryCompare{Field: f, Op: plainOp(cmp.Op), Value: v.Int}, nil
	case FieldPropertyKey, FieldPropertyValue:
		return &metadata.QueryPropExists{Conds: []metadata.QueryPropCond{{
			OnValue: cmp.Field.ID == FieldPropertyValue,
			Op:      patternOp(cmp.Op),
			Value:   patternValue(cmp.Op, v.Str),
		}}}, nil
	}
	f, ok := storageFieldOf(cmp.Field.ID)
	if !ok {
		return nil, fmt.Errorf("planner: field %q has no storage mapping", cmp.Field.Name)
	}
	return &metadata.QueryCompare{Field: f, Op: patternOp(cmp.Op), Value: patternValue(cmp.Op, v.Str)}, nil
}

// lowerRepo compiles a repo comparator, expanding values that name virtual
// repositories per aql.md §7-1: $eq is REPLACED by the member group;
// positive pattern/keep forms keep the original and OR the members;
// negative forms keep the original and AND-exclude the members (nodes
// never carry the virtual key itself, so the original arm is exactness,
// the member arm is semantics). Range comparators pass through: expanding
// them would need enumerating the repos inside a lexical range, a seam
// beyond value-exact expansion (deferred with the resolver's T-413 wiring).
func (p *planner) lowerRepo(op Operator, key string) (metadata.NodePredicate, error) {
	plain := &metadata.QueryCompare{Field: metadata.QueryRepo, Op: patternOp(op), Value: patternValue(op, key)}
	if p.opt.Virtual == nil {
		return plain, nil
	}
	literal := op == OpEq || op == OpNe || ((op == OpMatch || op == OpNmatch) && !HasWildcard(key))
	if !literal {
		return plain, nil
	}
	members, err := p.opt.Virtual.Members(p.ctx, key)
	if err != nil {
		return nil, fmt.Errorf("planner: resolving virtual repository %q: %w", key, err)
	}
	if len(members) == 0 {
		return plain, nil
	}
	group := repoGroup(members)
	switch op {
	case OpEq:
		return group, nil
	case OpMatch:
		return &metadata.QueryOr{Children: []metadata.NodePredicate{plain, group}}, nil
	case OpNe, OpNmatch:
		return &metadata.QueryAnd{Children: []metadata.NodePredicate{plain, &metadata.QueryNot{Child: group}}}, nil
	}
	return plain, nil
}

// repoGroup builds the member disjunction of one virtual expansion.
func repoGroup(members []string) metadata.NodePredicate {
	children := make([]metadata.NodePredicate, 0, len(members))
	for _, m := range members {
		children = append(children, &metadata.QueryCompare{
			Field: metadata.QueryRepo, Op: metadata.QueryEq, Value: m,
		})
	}
	return &metadata.QueryOr{Children: children}
}

// lowerType compiles the type enum onto the folder test (nodes has no
// type column; folder rows are the trailing-slash markers).
func lowerType(op Operator, val string) metadata.NodePredicate {
	if val == "any" {
		// Everything is "any": $eq any is vacuous-true (dropped), $ne any
		// is unsatisfiable.
		if op == OpEq {
			return nil
		}
		return &metadata.QueryFalse{}
	}
	test := &metadata.QueryFolderTest{Folder: val == "folder"}
	if op == OpEq {
		return test
	}
	return &metadata.QueryNot{Child: test}
}

// propMatchConds compiles the @key shorthand: the name arm (unless @*) and
// the value arm (unless "*" — key existence, aql.md §2.3).
func propMatchConds(pm *PropMatch) []metadata.QueryPropCond {
	var conds []metadata.QueryPropCond
	if !pm.WildcardKey {
		conds = append(conds, metadata.QueryPropCond{OnValue: false, Op: metadata.QueryEq, Value: pm.Key})
	}
	if !isStarValue(pm.Op, pm.Value) {
		conds = append(conds, metadata.QueryPropCond{OnValue: true, Op: patternOp(pm.Op), Value: patternValue(pm.Op, pm.Value.Str)})
	}
	return conds
}

// isStarValue reports the {"@<key>": "*"} key-existence form (the short
// form and the explicit $eq are indistinguishable in the AST by design —
// aql.md §2.3 defines the semantics for the shared shape).
func isStarValue(op Operator, v Value) bool {
	return (op == OpEq || op == OpMatch) && v.Kind == LitString && v.Str == "*"
}

// flattenProp flattens $msp conditions into one condition list: every
// condition constrains the SAME property instance, so they must land in
// one QueryPropSame (parser guarantees property-scoped input; the walker
// keeps that contract honest).
func (p *planner) flattenProp(conds []Criteria) ([]metadata.QueryPropCond, error) {
	var out []metadata.QueryPropCond
	for _, c := range conds {
		switch n := c.(type) {
		case *PropMatch:
			out = append(out, propMatchConds(n)...)
		case *Compare:
			if n.Field.Domain != DomainProperty {
				return nil, fmt.Errorf("planner: $msp condition on non-property field %q", n.Field.Name)
			}
			out = append(out, metadata.QueryPropCond{
				OnValue: n.Field.ID == FieldPropertyValue,
				Op:      patternOp(n.Op),
				Value:   patternValue(n.Op, n.Value.Str),
			})
		case *And:
			more, err := p.flattenProp(n.Children)
			if err != nil {
				return nil, err
			}
			out = append(out, more...)
		default:
			return nil, fmt.Errorf("planner: $msp condition %T is not property-scoped", c)
		}
	}
	return out, nil
}

// dateArm compiles a date comparator. Partial-precision literals denote
// their whole period (aql.md §2.5): $eq/$ne match the period, and the
// order comparators take the edge that keeps the period on the matching
// side ($gt leaves the period, $gte includes it, and symmetrically for
// $lt/$lte). Full-precision literals compare the instant exactly. Both
// sides are normalized inside the compiler (julianday), so zone offsets
// and fractional seconds in the query text compare correctly.
func (p *planner) dateArm(f metadata.QueryField, op Operator, str string) (metadata.NodePredicate, error) {
	start, end, exact, err := resolveDateLiteral(str)
	if err != nil {
		return nil, err
	}
	if exact {
		return &metadata.QueryCompare{Field: f, Op: plainOp(op), Value: start}, nil
	}
	gte := &metadata.QueryCompare{Field: f, Op: metadata.QueryGte, Value: start}
	ltEnd := &metadata.QueryCompare{Field: f, Op: metadata.QueryLt, Value: end}
	switch op {
	case OpEq:
		return &metadata.QueryAnd{Children: []metadata.NodePredicate{gte, ltEnd}}, nil
	case OpNe:
		return &metadata.QueryNot{Child: &metadata.QueryAnd{Children: []metadata.NodePredicate{gte, ltEnd}}}, nil
	case OpGt:
		return &metadata.QueryCompare{Field: f, Op: metadata.QueryGte, Value: end}, nil
	case OpGte:
		return gte, nil
	case OpLt:
		return &metadata.QueryCompare{Field: f, Op: metadata.QueryLt, Value: start}, nil
	case OpLte:
		return ltEnd, nil
	}
	return nil, fmt.Errorf("planner: operator %q is not a date comparator", op)
}

// periodArm compiles $last/$before: the boundary is now∓period and the
// comparator is fixed (aql.md §2.4: $last → > now−period, $before → <
// now−period).
func (p *planner) periodArm(f metadata.QueryField, op Operator, per *Period) (metadata.NodePredicate, error) {
	if p.opt.Now.IsZero() {
		return nil, fmt.Errorf("planner: relative date operand needs a clock (PlanOptions.Now)")
	}
	if per.Count > 100_000_000 {
		return nil, fmt.Errorf("planner: relative period count %d out of range", per.Count)
	}
	boundary := p.opt.Now
	switch per.Unit {
	case UnitMillis:
		boundary = boundary.Add(-time.Duration(per.Count) * time.Millisecond)
	case UnitSeconds:
		boundary = boundary.Add(-time.Duration(per.Count) * time.Second)
	case UnitMinutes:
		boundary = boundary.Add(-time.Duration(per.Count) * time.Minute)
	case UnitDays:
		boundary = boundary.Add(-time.Duration(per.Count) * 24 * time.Hour)
	case UnitWeeks:
		boundary = boundary.Add(-time.Duration(per.Count) * 7 * 24 * time.Hour)
	case UnitMonths:
		boundary = boundary.AddDate(0, -int(per.Count), 0)
	case UnitYears:
		boundary = boundary.AddDate(-int(per.Count), 0, 0)
	default:
		return nil, fmt.Errorf("planner: unknown relative-time unit %q", per.Unit)
	}
	irOp := metadata.QueryGt
	if op == OpBefore {
		irOp = metadata.QueryLt
	}
	return &metadata.QueryCompare{
		Field: f, Op: irOp, Value: boundary.UTC().Format(time.RFC3339Nano),
	}, nil
}

// dateLayouts are the full-precision datetime spellings validPartialDate
// admits beyond bare dates (zone optional, minutes/seconds optional).
var dateLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02T15",
}

// resolveDateLiteral splits a date literal into its period bounds. The
// bool result reports an exact instant (start carries it, end unused).
func resolveDateLiteral(s string) (start, end string, exact bool, err error) {
	digits := func(b string) bool {
		for i := 0; i < len(b); i++ {
			if b[i] < '0' || b[i] > '9' {
				return false
			}
		}
		return b != ""
	}
	switch {
	case len(s) == 4 && digits(s):
		y, _ := strconv.Atoi(s)
		t0 := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
		return t0.Format(time.RFC3339), t0.AddDate(1, 0, 0).Format(time.RFC3339), false, nil
	case len(s) == 7 && s[4] == '-' && digits(s[:4]) && digits(s[5:]):
		y, _ := strconv.Atoi(s[:4])
		m, _ := strconv.Atoi(s[5:])
		t0 := time.Date(y, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
		return t0.Format(time.RFC3339), t0.AddDate(0, 1, 0).Format(time.RFC3339), false, nil
	case len(s) == 10 && s[4] == '-' && s[7] == '-':
		t, perr := time.Parse("2006-01-02", s)
		if perr != nil {
			return "", "", false, fmt.Errorf("planner: invalid date literal %q: %w", s, perr)
		}
		t0 := t.UTC()
		return t0.Format(time.RFC3339), t0.AddDate(0, 0, 1).Format(time.RFC3339), false, nil
	}
	for _, layout := range dateLayouts {
		if t, perr := time.Parse(layout, s); perr == nil {
			// Normalize to RFC3339 UTC so hour-only and zone-offset
			// spellings reach julianday(?) in the shape SQLite parses.
			return t.UTC().Format(time.RFC3339Nano), "", true, nil
		}
	}
	return "", "", false, fmt.Errorf("planner: invalid date literal %q", s)
}

// patternValue routes a comparator value through the wildcard kernel: the
// ONE translator $match/$nmatch values pass through (match.go), shared
// with the legacy pattern endpoints so the surfaces cannot drift.
func patternValue(op Operator, v string) string {
	if op == OpMatch || op == OpNmatch {
		return LikePattern(v)
	}
	return v
}

// plainOp maps the six order/equality comparators onto the IR vocabulary.
func plainOp(op Operator) metadata.QueryOp {
	switch op {
	case OpEq:
		return metadata.QueryEq
	case OpNe:
		return metadata.QueryNe
	case OpGt:
		return metadata.QueryGt
	case OpGte:
		return metadata.QueryGte
	case OpLt:
		return metadata.QueryLt
	case OpLte:
		return metadata.QueryLte
	}
	return metadata.QueryEq
}

// patternOp extends plainOp with the wildcard comparators: the pattern
// itself is translated by the single kernel in match.go before it reaches
// the IR.
func patternOp(op Operator) metadata.QueryOp {
	switch op {
	case OpMatch:
		return metadata.QueryLike
	case OpNmatch:
		return metadata.QueryNotLike
	}
	return plainOp(op)
}

// storageFieldOf maps a registry field onto its storage query field.
// Fields without a mapping never reach the planner through Parse (the
// registry rejects them); the bool keeps hand-built ASTs honest.
func storageFieldOf(id FieldID) (metadata.QueryField, bool) {
	switch id {
	case FieldRepo:
		return metadata.QueryRepo, true
	case FieldPath:
		return metadata.QueryPath, true
	case FieldName:
		return metadata.QueryName, true
	case FieldDepth:
		return metadata.QueryDepth, true
	case FieldSize:
		return metadata.QuerySize, true
	case FieldCreated:
		return metadata.QueryCreated, true
	case FieldModified:
		return metadata.QueryModified, true
	case FieldUpdated:
		return metadata.QueryUpdated, true
	case FieldCreatedBy:
		return metadata.QueryCreatedBy, true
	case FieldSha256:
		return metadata.QuerySha256, true
	case FieldActualSHA1:
		return metadata.QuerySha1, true
	case FieldActualMD5:
		return metadata.QueryMd5, true
	}
	return "", false
}

// hasTypeCondition reports whether the criteria constrains type anywhere —
// the gate for the implicit file predicate.
func hasTypeCondition(c Criteria) bool {
	switch n := c.(type) {
	case *And:
		for _, ch := range n.Children {
			if hasTypeCondition(ch) {
				return true
			}
		}
	case *Or:
		for _, ch := range n.Children {
			if hasTypeCondition(ch) {
				return true
			}
		}
	case *Compare:
		return n.Field.ID == FieldType
	}
	return false
}

// projectionEntry pairs an echo field with its storage mapping.
type projectionEntry struct {
	key   string
	id    FieldID
	field metadata.QueryField
}

// defaultProjection is the default output set (aql.md §3.3, live-verified
// v01) minus modified_by — no storage source, never faked (ADR-0043 pt 2).
var defaultProjection = []projectionEntry{
	{"repo", FieldRepo, metadata.QueryRepo},
	{"path", FieldPath, metadata.QueryPath},
	{"name", FieldName, metadata.QueryName},
	{"type", FieldType, ""},
	{"size", FieldSize, metadata.QuerySize},
	{"created", FieldCreated, metadata.QueryCreated},
	{"created_by", FieldCreatedBy, metadata.QueryCreatedBy},
	{"modified", FieldModified, metadata.QueryModified},
	{"updated", FieldUpdated, metadata.QueryUpdated},
}

// starProjection is the include("*") set: every storage-backed projectable
// field plus the virtual_repos logical field, in the order of the v05
// live sample, minus the fields BinFlow has no source for (modified_by,
// repo_path_checksum, id — all registered unsupported).
var starProjection = []projectionEntry{
	{"repo", FieldRepo, metadata.QueryRepo},
	{"path", FieldPath, metadata.QueryPath},
	{"name", FieldName, metadata.QueryName},
	{"type", FieldType, ""},
	{"size", FieldSize, metadata.QuerySize},
	{"created", FieldCreated, metadata.QueryCreated},
	{"created_by", FieldCreatedBy, metadata.QueryCreatedBy},
	{"modified", FieldModified, metadata.QueryModified},
	{"updated", FieldUpdated, metadata.QueryUpdated},
	{"depth", FieldDepth, metadata.QueryDepth},
	{"actual_md5", FieldActualMD5, metadata.QueryMd5},
	{"actual_sha1", FieldActualSHA1, metadata.QuerySha1},
	{"sha256", FieldSha256, metadata.QuerySha256},
}

// buildProjection lowers the .include() list (or the default/star set)
// into the storage field list, the property key list and the echo list.
func buildProjection(q *Query) (fields []metadata.QueryField, props []string, out []OutputField) {
	seen := map[metadata.QueryField]bool{}
	addEntry := func(e projectionEntry) {
		if e.field == "" || seen[e.field] {
			out = append(out, OutputField{Key: e.key, Kind: OutputItem, Field: e.id})
			return
		}
		seen[e.field] = true
		fields = append(fields, e.field)
		out = append(out, OutputField{Key: e.key, Kind: OutputItem, Field: e.id})
	}
	addProp := func(key, raw string) {
		props = append(props, key)
		out = append(out, OutputField{Key: raw, Kind: OutputProp, PropKey: key})
	}
	if len(q.Include) == 0 {
		for _, e := range defaultProjection {
			addEntry(e)
		}
		return fields, props, out
	}
	for _, inc := range q.Include {
		switch {
		case inc.Star:
			for _, e := range starProjection {
				addEntry(e)
			}
			out = append(out, OutputField{Key: inc.Raw, Kind: OutputVirtualRepos, Field: FieldVirtualRepos})
		case inc.PropKey != "":
			addProp(inc.PropKey, inc.Raw)
		case inc.Field.ID == FieldVirtualRepos:
			out = append(out, OutputField{Key: inc.Raw, Kind: OutputVirtualRepos, Field: FieldVirtualRepos})
		case inc.Field.ID == FieldPropertyKey || inc.Field.ID == FieldPropertyValue:
			// The long forms project the whole property pair of every
			// property the node carries.
			addProp("*", inc.Raw)
		default:
			if f, ok := storageFieldOf(inc.Field.ID); ok {
				addEntry(projectionEntry{key: inc.Field.Name, id: inc.Field.ID, field: f})
			} else {
				out = append(out, OutputField{Key: inc.Raw, Kind: OutputItem, Field: inc.Field.ID})
			}
		}
	}
	return fields, props, out
}
