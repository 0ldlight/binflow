package search

import (
	"context"
	"fmt"
	"time"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The build-family query domains (M17 T-511, FR-152.3 / aql.md §15):
// builds.find / modules.find / dependencies.find — the three entry domains
// whose data plane is the 024 table family (T-507~T-509). Where the item
// domain compiles to a metadata.NodeQuery and executes inside SQL, the
// build family evaluates row-side over materialized rows: the seam below
// enumerates the record plane, the engine filters, sorts and windows — the
// same K63 gate, deadline and row cap the items path runs, a single
// resource plane for every AQL entrance.
//
// ACL (aql.md §6 + §15.2, architecture §26.1): the row-level filter is
// r(buildRepo, buildName) — the SAME allow() mirror the build domain's
// read faces run, injected here as CanReadBuild (build.Service satisfies
// it through the httpapi assembly; the boundary stays one-way — build
// never imports search).

// BuildRun is one run header row as the builds entry projects it: the
// four-tuple coordinates plus the audit columns. URL is the CI server URL
// extracted from the archived payload document (BinFlow keeps no url
// column — the field is output-only, aql.md §15.1).
type BuildRun struct {
	Name    string
	Number  string
	Started string
	Repo    string
	URL     string

	CreatedAt string
	CreatedBy string
	UpdatedAt string
	UpdatedBy string
}

// BuildDependencyRow is one build_dependencies row: the wire `id` whole
// coordinate, type, the scopes[] joined with ',' and the checksums.
type BuildDependencyRow struct {
	ID     string
	Type   string
	Scopes string
	Sha1   string
	Sha256 string
	Md5    string
	Seq    int64
}

// BuildModuleRow is one build_modules row with its run coordinates (the
// ACL key) and its dependency segment. The artifact segment is NOT carried
// — the M17 query entries do not open the artifact face (aql.md §15.3).
type BuildModuleRow struct {
	Repo    string
	Name    string
	Number  string
	Started string

	ModuleID     string
	Dependencies []*BuildDependencyRow
}

// BuildSearcher is the injected read facet over the build domain's record
// plane (T-507's boundary ruling: weaving happens through injected facets
// at the assembly layer — httpapi adapts metadata's BuildStore and the
// build.Service ACL face onto this interface; search never imports build).
// The searcher applies NO criteria and NO visibility filter: enumeration
// is complete, evaluation and the row-level ACL recheck are the engine's —
// the same division the items path runs (queryer vs. weave).
//
// Implementations must be safe for concurrent use.
type BuildSearcher interface {
	// Runs enumerates every run header, ordered (name, started DESC,
	// number DESC, repo) — the store's ListBuildNumbers order per name.
	Runs(ctx context.Context) ([]*BuildRun, error)
	// Modules enumerates every run's module segment, runs in the Runs
	// order, modules by module_id, dependencies by seq.
	Modules(ctx context.Context) ([]*BuildModuleRow, error)
	// CanReadBuild is the row-level ACL: the r(buildRepo, buildName)
	// decision of the SAME allow() mirror the build read faces run (admin
	// short-circuits inside the mirror).
	CanReadBuild(ctx context.Context, p *repo.Principal, buildRepo, buildName string) bool
}

// BuildRow is one result row of a build-family query: every entry domain's
// row reduces to the run coordinates plus the entry's own members (the
// NodeQueryRow precedent — one flat row, field IDs address the members).
// order is the natural materialization index, the deterministic sort
// tiebreak.
type BuildRow struct {
	// Run coordinates: the ACL key (Repo, Name) and the builds entry's
	// source columns.
	Repo    string
	Name    string
	Number  string
	Started string
	URL     string

	CreatedAt string
	CreatedBy string
	UpdatedAt string
	UpdatedBy string

	// ModuleID carries the modules entry's name member (module_id).
	ModuleID string

	// Dep* carry the dependencies entry's members.
	DepID     string
	DepType   string
	DepScopes string
	DepSha1   string
	DepMd5    string

	order int
}

// buildRowsFromRuns materializes the builds entry's rows.
func buildRowsFromRuns(runs []*BuildRun) []*BuildRow {
	out := make([]*BuildRow, 0, len(runs))
	for i, r := range runs {
		out = append(out, &BuildRow{
			Repo: r.Repo, Name: r.Name, Number: r.Number, Started: r.Started,
			URL: r.URL, CreatedAt: r.CreatedAt, CreatedBy: r.CreatedBy,
			UpdatedAt: r.UpdatedAt, UpdatedBy: r.UpdatedBy,
			order: i,
		})
	}
	return out
}

// buildRowsFromModules materializes the modules entry's rows (one per
// module) — the dependencies entry flattens the nested segment instead
// (buildRowsFromDependencies).
func buildRowsFromModules(mods []*BuildModuleRow) []*BuildRow {
	var out []*BuildRow
	for _, m := range mods {
		out = append(out, &BuildRow{
			Repo: m.Repo, Name: m.Name, Number: m.Number, Started: m.Started,
			ModuleID: m.ModuleID,
			order:    len(out),
		})
	}
	return out
}

// buildRowsFromDependencies materializes the dependencies entry's rows:
// one per dependency line, carrying its run coordinates and module id.
func buildRowsFromDependencies(mods []*BuildModuleRow) []*BuildRow {
	var out []*BuildRow
	for _, m := range mods {
		for _, d := range m.Dependencies {
			out = append(out, &BuildRow{
				Repo: m.Repo, Name: m.Name, Number: m.Number, Started: m.Started,
				ModuleID:  m.ModuleID,
				DepID:     d.ID,
				DepType:   d.Type,
				DepScopes: d.Scopes,
				DepSha1:   d.Sha1,
				DepMd5:    d.Md5,
				order:     len(out),
			})
		}
	}
	return out
}

// BuildFieldValue reads one registry field off a row. Unknown ids answer
// "" — the registry rejects them long before a plan renders.
func BuildFieldValue(r *BuildRow, id FieldID) string {
	switch id {
	case FieldBuildURL:
		return r.URL
	case FieldBuildName:
		return r.Name
	case FieldBuildNumber:
		return r.Number
	case FieldBuildStarted:
		return r.Started
	case FieldBuildRepo:
		return r.Repo
	case FieldBuildCreated:
		return r.CreatedAt
	case FieldBuildCreatedBy:
		return r.CreatedBy
	case FieldBuildModified:
		return r.UpdatedAt
	case FieldBuildModifiedBy:
		return r.UpdatedBy
	case FieldModuleName:
		return r.ModuleID
	case FieldDepName:
		return r.DepID
	case FieldDepScope:
		return r.DepScopes
	case FieldDepType:
		return r.DepType
	case FieldDepSha1:
		return r.DepSha1
	case FieldDepMd5:
		return r.DepMd5
	}
	return ""
}

// buildDateFields are the build-family date members (aql.md §15.1).
var buildDateFields = map[FieldID]bool{
	FieldBuildStarted:  true,
	FieldBuildCreated:  true,
	FieldBuildModified: true,
}

// buildInstantLayouts are the timestamp spellings the build plane stores:
// RFC3339 for the audit columns (T-508's upload face), the Java-canonical
// millis form for started (T-508's NormalizeStarted output).
var buildInstantLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.000-0700",
	"2006-01-02T15:04:05-0700",
}

// ParseBuildInstant parses one stored build timestamp. The bool is false
// for a value no layout accepts (a drifted schema, a hand-seeded row) —
// every date comparison over it then answers false, the SQL NULL row rule
// the item domain's julianday kernel runs. Exported for T-415's build-row
// renderer: the layout list lives once, beside the evaluator that orders
// by it.
func ParseBuildInstant(raw string) (time.Time, bool) {
	for _, layout := range buildInstantLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// ---- the planner ----

// buildPredicate evaluates one lowered criteria node over a row; nil
// means match-all (the vacuous-arm convention of the item planner).
type buildPredicate func(*BuildRow) bool

// buildSortKey is one .sort() entry of a build-family query.
type buildSortKey struct {
	Field FieldID
	Asc   bool
}

// BuildPlan is the compiled form of one build-family query: the lowered
// predicate tree, the projection echo list (the same OutputField shape the
// item Plan renders through), the sort keys and the window echoes. Pure
// function of the AST plus PlanOptions — no IO.
type BuildPlan struct {
	Entry  string // "builds" | "modules" | "dependencies"
	Where  buildPredicate
	Output []OutputField
	Sort   []buildSortKey

	Offset    int64
	HasOffset bool
	Limit     int64
	HasLimit  bool
}

// PlanBuildQuery compiles one parsed build-family query. Failures are
// engine-internal (500-class): every user-facing rejection happened in
// Parse; the only reachable errors are hand-built-AST defenses and the
// missing-clock arm of relative dates.
func PlanBuildQuery(q *Query, opt PlanOptions) (*BuildPlan, error) {
	if q == nil || q.Criteria == nil {
		return nil, fmt.Errorf("build planner: nil query or criteria")
	}
	if !isBuildEntry(q.Domain) {
		return nil, fmt.Errorf("build planner: %q is not a build-family entry", q.Domain)
	}
	where, err := lowerBuildPredicate(q.Criteria, opt)
	if err != nil {
		return nil, err
	}
	sorts := make([]buildSortKey, 0, len(q.Sort))
	for _, sk := range q.Sort {
		sorts = append(sorts, buildSortKey{Field: sk.Field.ID, Asc: sk.Asc})
	}
	return &BuildPlan{
		Entry:  q.Domain,
		Where:  where,
		Output: buildBuildProjection(q),
		Sort:   sorts,
		Offset: q.Offset, HasOffset: q.HasOffset,
		Limit: q.Limit, HasLimit: q.HasLimit,
	}, nil
}

// lowerBuildPredicate compiles the criteria tree; the boolean folding
// mirrors the item planner (a zero-child And is the top-level match-all
// marker, one-child groups collapse, an Or over a vacuous arm is
// match-all — true absorbs a disjunction).
func lowerBuildPredicate(c Criteria, opt PlanOptions) (buildPredicate, error) {
	switch n := c.(type) {
	case *And:
		children := make([]buildPredicate, 0, len(n.Children))
		for _, ch := range n.Children {
			p, err := lowerBuildPredicate(ch, opt)
			if err != nil {
				return nil, err
			}
			if p != nil {
				children = append(children, p)
			}
		}
		switch len(children) {
		case 0:
			return nil, nil
		case 1:
			return children[0], nil
		}
		return func(r *BuildRow) bool {
			for _, p := range children {
				if !p(r) {
					return false
				}
			}
			return true
		}, nil
	case *Or:
		var children []buildPredicate
		for _, ch := range n.Children {
			p, err := lowerBuildPredicate(ch, opt)
			if err != nil {
				return nil, err
			}
			if p == nil {
				return nil, nil // vacuous-true arm absorbs
			}
			children = append(children, p)
		}
		if len(children) == 0 {
			return nil, fmt.Errorf("build planner: empty disjunction")
		}
		if len(children) == 1 {
			return children[0], nil
		}
		return func(r *BuildRow) bool {
			for _, p := range children {
				if p(r) {
					return true
				}
			}
			return false
		}, nil
	case *Compare:
		return lowerBuildCompare(n, opt)
	}
	return nil, fmt.Errorf("build planner: criteria node %T is outside the build-family subset", c)
}

// lowerBuildCompare compiles one comparator: date members take the
// partial-precision/relative kernels (the dateArm/periodArm semantics the
// item planner runs, evaluated row-side), every other member is a string
// comparator — plain byte order for the six order operators (the SQLite
// BINARY collation the item domain compares under), the one wildcard
// kernel for $match/$nmatch.
func lowerBuildCompare(cmp *Compare, opt PlanOptions) (buildPredicate, error) {
	if buildDateFields[cmp.Field.ID] {
		return buildDatePredicate(cmp.Field.ID, cmp.Op, cmp.Value, opt)
	}
	v := cmp.Value
	if v.Kind != LitString {
		return nil, fmt.Errorf("build planner: field %q takes a string literal", cmp.Field.Name)
	}
	id, val, op := cmp.Field.ID, v.Str, cmp.Op
	return func(r *BuildRow) bool {
		return compareBuildString(BuildFieldValue(r, id), op, val)
	}, nil
}

// compareBuildString evaluates one string comparator. Unknown operators
// answer false (unreachable through Parse — the registry op sets gate).
func compareBuildString(row string, op Operator, val string) bool {
	switch op {
	case OpEq:
		return row == val
	case OpNe:
		return row != val
	case OpGt:
		return row > val
	case OpGte:
		return row >= val
	case OpLt:
		return row < val
	case OpLte:
		return row <= val
	case OpMatch:
		return MatchesPattern(val, row)
	case OpNmatch:
		return !MatchesPattern(val, row)
	}
	return false
}

// buildDatePredicate compiles one date comparator with the item domain's
// edge semantics (plan.go dateArm): partial-precision literals denote
// their whole period ($eq/$ne match the period; the order comparators
// take the edge that keeps the period on the matching side), full
// precision compares the instant exactly, $last/$before compare against
// now∓period. A row value no layout parses answers false for EVERY
// operator — the SQL NULL row rule.
func buildDatePredicate(id FieldID, op Operator, v Value, opt PlanOptions) (buildPredicate, error) {
	if v.Period != nil {
		if opt.Now.IsZero() {
			return nil, fmt.Errorf("build planner: relative date operand needs a clock (PlanOptions.Now)")
		}
		boundary, err := periodBoundary(opt.Now, v.Period)
		if err != nil {
			return nil, err
		}
		after := op == OpLast
		return func(r *BuildRow) bool {
			t, ok := ParseBuildInstant(BuildFieldValue(r, id))
			if !ok {
				return false
			}
			if after {
				return t.After(boundary)
			}
			return t.Before(boundary)
		}, nil
	}
	start, end, exact, err := resolveDateLiteral(v.Str)
	if err != nil {
		return nil, fmt.Errorf("build planner: field date literal: %w", err)
	}
	var sStart, sEnd time.Time
	if !exact {
		if sStart, err = time.Parse(time.RFC3339, start); err != nil {
			return nil, fmt.Errorf("build planner: date bound %q: %w", start, err)
		}
		if sEnd, err = time.Parse(time.RFC3339, end); err != nil {
			return nil, fmt.Errorf("build planner: date bound %q: %w", end, err)
		}
	} else if sStart, err = time.Parse(time.RFC3339Nano, start); err != nil {
		return nil, fmt.Errorf("build planner: date literal %q: %w", start, err)
	}
	return func(r *BuildRow) bool {
		t, ok := ParseBuildInstant(BuildFieldValue(r, id))
		if !ok {
			return false
		}
		if exact {
			switch op {
			case OpEq:
				return t.Equal(sStart)
			case OpNe:
				return !t.Equal(sStart)
			case OpGt:
				return t.After(sStart)
			case OpGte:
				return !t.Before(sStart)
			case OpLt:
				return t.Before(sStart)
			case OpLte:
				return !t.After(sStart)
			}
			return false
		}
		inPeriod := !t.Before(sStart) && t.Before(sEnd)
		switch op {
		case OpEq:
			return inPeriod
		case OpNe:
			return !inPeriod
		case OpGt:
			return !t.Before(sEnd)
		case OpGte:
			return !t.Before(sStart)
		case OpLt:
			return t.Before(sStart)
		case OpLte:
			return t.Before(sEnd)
		}
		return false
	}, nil
}

// periodBoundary resolves now∓period (aql.md §2.4: $last → now−period,
// $before symmetrically). The unit table is the item planner's periodArm
// verbatim — one table, two evaluators.
func periodBoundary(now time.Time, per *Period) (time.Time, error) {
	if per.Count > 100_000_000 {
		return time.Time{}, fmt.Errorf("build planner: relative period count %d out of range", per.Count)
	}
	switch per.Unit {
	case UnitMillis:
		return now.Add(-time.Duration(per.Count) * time.Millisecond), nil
	case UnitSeconds:
		return now.Add(-time.Duration(per.Count) * time.Second), nil
	case UnitMinutes:
		return now.Add(-time.Duration(per.Count) * time.Minute), nil
	case UnitDays:
		return now.Add(-time.Duration(per.Count) * 24 * time.Hour), nil
	case UnitWeeks:
		return now.Add(-time.Duration(per.Count) * 7 * 24 * time.Hour), nil
	case UnitMonths:
		return now.AddDate(0, -int(per.Count), 0), nil
	case UnitYears:
		return now.AddDate(-int(per.Count), 0, 0), nil
	}
	return time.Time{}, fmt.Errorf("build planner: unknown relative-time unit %q", per.Unit)
}

// buildBuildProjection lowers the include() list (or the entry's default /
// star set) into the echo list — the same shape and override rule as the
// item domain (aql.md §2.5/§15.1: the first field include names replaces
// the entry's default set; "*" projects every projectable member).
func buildBuildProjection(q *Query) []OutputField {
	resolve := func(name string) OutputField {
		f, _ := lookupBuildField(q.Domain, name)
		return OutputField{Key: f.Name, Kind: OutputItem, Field: f.ID}
	}
	var out []OutputField
	if len(q.Include) == 0 {
		for _, name := range buildDefaultOutput[q.Domain] {
			out = append(out, resolve(name))
		}
		return out
	}
	for _, inc := range q.Include {
		if inc.Star {
			for _, name := range buildStarOutput[q.Domain] {
				out = append(out, resolve(name))
			}
			continue
		}
		out = append(out, OutputField{Key: inc.Field.Name, Kind: OutputItem, Field: inc.Field.ID})
	}
	return out
}

// buildSortableValue renders one row member in sort order: dates
// normalize to the RFC3339 instant (an unparseable value sorts first,
// ascending — the deterministic fail-closed arm), strings compare as
// stored.
func buildSortableValue(r *BuildRow, id FieldID) string {
	v := BuildFieldValue(r, id)
	if !buildDateFields[id] {
		return v
	}
	if t, ok := ParseBuildInstant(v); ok {
		return t.Format(time.RFC3339Nano)
	}
	return ""
}
