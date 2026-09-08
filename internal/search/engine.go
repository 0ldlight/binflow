package search

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The AQL execution engine (T-413, FR-133.2/.4 / ADR-0043 pts 4-5): the
// entry that takes query TEXT end to end — Parse → gate → deadline →
// ACL weave → IR window → NodeQueryer → row re-check → projection
// decoration — and hands the HTTP layer (T-415) rows that are already
// visible, bounded and obfuscated. The engine still performs no IO of its
// own: the queryer, the ACL seam, the virtual resolver and the clock are
// injected at construction (doc.go's package contract), so the engine file
// is orchestration plus policy, nothing else.

// The K63 resource-gate constants (ADR-0043 pt 5 + Errata ⑤/⑥; aql.md §5).
// Engine-internal by design — zero new configuration keys (ADR-0040
// posture); the values are provisional on K63's aql.md write-back.
const (
	// ResultCap is the hard row ceiling one query may return (K63): the
	// engine fetches cap+1 rows to detect overflow, reports Truncated, and
	// never materializes more than cap+1 rows — the structural zero-OOM
	// bound NFR-P68 rests on ("流式装饰"的真身, ADR-0043 pt 5). The legacy
	// search endpoints (T-417) share the same ceiling and the same
	// truncation header so the two surfaces cannot drift.
	ResultCap = 1000
	// maxConcurrent is the K63 concurrency ceiling: official default 3,
	// BinFlow 4 (aql.md §5 C-layer ruling; full gate → 429, no queue).
	maxConcurrent = 4
	// queryTimeout bounds one query's execution segment (K63). The official
	// REST face runs 900s; 10s is the single-node SQLite posture (C-layer
	// ruling, Q2 leave-behind in ADR-0043).
	queryTimeout = 10 * time.Second
	// BusyRetryAfter is the Retry-After delay a busy gate advises. The
	// official face does not document the header; BinFlow adds it as the
	// machine-readable arm (aql.md §5). Exported: T-415 renders the header
	// from this single source.
	BusyRetryAfter = 1 * time.Second
)

// TruncationHeader and TruncationNotice are the truncation surface
// T-415/T-417 render: the C-layer machine-readable header beside the
// official A-layer range.notification copy (aql.md §5, verbatim — do not
// reword).
const (
	TruncatedHeader  = "X-BinFlow-Search-Truncated"
	TruncationNotice = "AQL query reached the search hard limit, results are trimmed."
)

// obfuscatedUser is the literal non-admin callers see in created_by
// (aql.md §6, ADR-0043 Errata ⑧: official ObfuscationUtils default; admin
// principals keep the original value).
const obfuscatedUser = "unknown"

// The engine's externally visible failures beyond the parser's *QueryError
// (every QueryError kind is a 400; ADR-0043 pt 6 + Errata ⑤). The messages
// are the final envelope copy — single assembly point, this package.
var (
	// ErrResourceBusy is the full-gate rejection: 429 with the verbatim
	// official body copy (aql.md §4 E8) and a Retry-After of
	// BusyRetryAfter seconds.
	ErrResourceBusy = errors.New("too many requests")
	// ErrQueryTimeout reports the execution deadline elapsed: 408
	// (aql.md §4 E7 — NOT 503/504; ADR-0043 Errata ⑤'s final ruling).
	ErrQueryTimeout = errors.New("AQL query execution timed out")
)

// ACL is the consumer-side seam of the two-stage read weave (ADR-0043
// pt 4): repo.Service satisfies it structurally. Stage one narrows the SQL
// to the readable repository set; stage two re-checks the rows of
// path-scoped repositories — the same allow() source the content plane
// runs, so the query plane cannot leak what a download would refuse.
type ACL interface {
	SearchScope(ctx context.Context, p *repo.Principal) ([]repo.ReadScope, error)
	CanRead(ctx context.Context, p *repo.Principal, repoKey, path string) bool
}

// EngineOptions carries the engine's injected dependencies.
type EngineOptions struct {
	// Nodes executes compiled queries; production wiring passes the store's
	// NodeQueryer (md.Nodes().(metadata.NodeQueryer), the NodeSearcher
	// assertion shape).
	Nodes metadata.NodeQueryer
	// ACL is the two-stage read weave. nil fails closed: every query
	// answers the empty scope — the engine never falls back to unfiltered
	// execution.
	ACL ACL
	// Virtual resolves virtual repository keys onto their member sets
	// (PlanOptions.Virtual semantics); nil passes every repo value through
	// literally.
	Virtual VirtualResolver
	// QRL is the DB-query rate plane (M16 T-452, aql.md §14.4): a DELAY
	// throttle that rides AFTER the K63 admission gate and never rejects —
	// orthogonal by construction. nil disables the plane entirely (the
	// factory-disabled limiter is itself a no-op, so wiring the shared
	// instance is free).
	QRL *QueryRateLimiter
	// Builds is the build-family record-plane facet (M17 T-511, aql.md
	// §15): the builds/modules/dependencies entries execute against it.
	// nil keeps the three entries at the honest ErrBuildSearchUnavailable
	// — items queries are unaffected.
	Builds BuildSearcher
	// Now is the clock $last/$before resolve against; nil means time.Now.
	Now func() time.Time
}

// Engine runs AQL queries. Construct once per process (the gate and the
// queryer are shared state) and call Run concurrently.
type Engine struct {
	nodes   metadata.NodeQueryer
	acl     ACL
	virtual VirtualResolver
	qrl     *QueryRateLimiter
	builds  BuildSearcher
	nowFn   func() time.Time
	gate    *gate
	// timeout is the per-query deadline; the field exists so tests can
	// tighten it (the production value is the queryTimeout constant — no
	// configuration surface, ADR-0043 pt 5).
	timeout time.Duration
}

// NewEngine assembles the engine. opt.Nodes must be non-nil for any query
// to execute (a nil queryer answers ErrQueryUnavailable, the honest 500
// rather than a panic); a nil ACL fails closed on scope.
func NewEngine(opt EngineOptions) *Engine {
	e := &Engine{
		nodes:   opt.Nodes,
		acl:     opt.ACL,
		virtual: opt.Virtual,
		qrl:     opt.QRL,
		builds:  opt.Builds,
		gate:    newGate(maxConcurrent),
		timeout: queryTimeout,
	}
	e.nowFn = opt.Now
	if e.nowFn == nil {
		e.nowFn = func() time.Time { return time.Now().UTC() }
	}
	return e
}

// ErrQueryUnavailable is the nil-queryer posture (a wiring bug, not a
// query failure): the honest 500-class refusal.
var ErrQueryUnavailable = errors.New("AQL query engine has no query executor wired")

// ErrBuildSearchUnavailable is the nil-BuildSearcher posture of the three
// build-family entries (builds/modules/dependencies): the engine is
// assembled but the build record plane is not wired — the honest 503-class
// refusal, never a pseudo-empty 200.
var ErrBuildSearchUnavailable = errors.New("AQL build-domain query engine has no build searcher wired")

// Result is one executed query. T-415 renders rows through Plan.Output
// (the projection echo list) and builds the range object from the window
// echoes: start_pos = Offset, end_pos/total = len(Rows) (the streaming
// convention, aql.md §3.2), limit echoed only when HasLimit, notification
// = TruncationNotice exactly when Truncated. Elapsed carries the execution
// segment's wall time for the metrics family and the >5s slow-query WARN
// (ADR-0043 pt 8 — the engine measures, the transport layer reports; this
// package keeps its zero-IO contract).
type Result struct {
	// Plan is the compiled query (IR + projection echo list + window).
	// A build-family query fills only the projection/window echoes — its
	// Query stays zero (the entry executes through the build plane, never
	// the node IR).
	Plan *Plan
	// Rows are the visible rows, already truncated to the effective window
	// and re-checked against the path-scoped ACL; created_by is obfuscated
	// for non-admin callers.
	Rows []*metadata.NodeQueryRow
	// EntryDomain echoes the query's entry domain: "" (or "items") for an
	// items query, builds/modules/dependencies for the build family —
	// T-415 branches the row rendering on it.
	EntryDomain string
	// BuildRows carries the build-family rows (nil for an items query):
	// already ACL-filtered, obfuscated, sorted and windowed exactly like
	// Rows.
	BuildRows []*BuildRow
	// Truncated reports the cap+1 probe saw more raw rows past the window
	// — the honest upper bound on the raw row sequence (ADR-0043 pt 4's
	// paging semantics: offset/limit walk the RAW row order, per-page
	// re-check).
	Truncated bool
	// Offset/HasOffset/Limit/HasLimit echo the query's own window (NOT the
	// engine's effective window — range.limit omits the key when the query
	// stated none, aql.md §3.2).
	Offset    int64
	HasOffset bool
	Limit     int64
	HasLimit  bool
	// Elapsed is the execution segment's duration (gate release to re-check
	// complete).
	Elapsed time.Duration
}

// Run executes one AQL query as p (nil = anonymous). Execution order is
// the ADR-0043 pt 5 contract: Parse first (a syntax rejection is a 400
// that never occupies a resource slot), then the non-blocking gate
// (full → ErrResourceBusy), then the deadline-bounded segment (scope →
// compile → weave → execute → re-check → decorate).
//
// Error families: *QueryError (400s, from Parse), ErrResourceBusy (429 +
// Retry-After BusyRetryAfter), ErrQueryTimeout (408), repo.ErrForbidden
// (the anonymous closed-instance gate) and everything else is the 500
// class (wrapped, never swallowed).
func (e *Engine) Run(ctx context.Context, p *repo.Principal, query string) (*Result, error) {
	ast, err := Parse(query)
	if err != nil {
		return nil, err // *QueryError — already the final copy
	}
	if isBuildEntry(ast.Domain) {
		return e.runBounded(ctx, func(qctx context.Context) (*Result, error) {
			return e.executeBuild(qctx, p, ast)
		})
	}
	return e.runAST(ctx, p, ast)
}

// runAST is the items-family entrance of the bounded segment.
func (e *Engine) runAST(ctx context.Context, p *repo.Principal, ast *Query) (*Result, error) {
	return e.runBounded(ctx, func(qctx context.Context) (*Result, error) {
		return e.execute(qctx, p, ast)
	})
}

// runBounded is the shared execution segment behind Run (both families),
// RunUsage and the dates/creation templates: the non-blocking gate, the
// QRL delay plane, the deadline and the bounded segment. Every entrance is
// the SAME query plane — one concurrency ceiling, one timeout, one row cap
// (K63 zero-exemption posture: a fixed-template caller buys no relief a
// hand-written query does not get — the build-family entries joined the
// same plane in T-511, no second gate to slip past). The QRL slot sits
// strictly AFTER the K63 gate (a 429 rejection never occupies rate budget)
// and strictly INSIDE the deadline (a throttled wait is bounded by the same
// 10s ceiling — the anchor's "delay, never reject" posture, aql.md §14.4).
// The principal rides the exec closure — the segment itself is
// identity-blind.
func (e *Engine) runBounded(ctx context.Context, exec func(context.Context) (*Result, error)) (*Result, error) {
	if !e.gate.tryAcquire() {
		return nil, ErrResourceBusy
	}
	defer e.gate.release()

	qctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	if e.qrl != nil {
		if err := e.qrl.Acquire(qctx, QRLTypeDefault); err != nil {
			// A throttle wait that exhausted the deadline is the SAME 408
			// timeout family an execution overrun is (the K63 deadline
			// bounds the whole segment, wait included); a canceled caller
			// context keeps its own error.
			if qctx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf("%w: %v", ErrQueryTimeout, err) //nolint:errorlint // cause rendered, not wrapped — the family rule above
			}
			return nil, err
		}
	}

	start := e.nowFn()
	res, err := exec(qctx)
	if e.qrl != nil {
		e.qrl.Charge(QRLTypeDefault, e.nowFn().Sub(start))
	}
	if err != nil {
		// The deadline ruling: a failure inside the bounded segment with
		// the deadline elapsed is the timeout family (408), whatever the
		// driver surfaced it as (aql.md §4 E7's cause-passthrough shape).
		// The cause is RENDERED, not wrapped: wrapping would keep the
		// context error matchable and let the 408 family leak
		// context.DeadlineExceeded to transport-layer matching.
		if qctx.Err() == context.DeadlineExceeded && !isQueryError(err) {
			return nil, fmt.Errorf("%w: %v", ErrQueryTimeout, err) //nolint:errorlint // cause rendered, not wrapped — see above
		}
		return nil, err
	}
	res.Elapsed = e.nowFn().Sub(start)
	return res, nil
}

// UsageQuery is the fixed template of GET /api/search/usage (aql.md
// §14.2): artifacts whose last download predates NotUsedSince (never
// downloaded included) AND whose creation predates CreatedBefore. The
// endpoint validates and converts the wire parameters; zero values here
// mean "absent" (CreatedBefore falling back to NotUsedSince per the
// official rule, Repos leaving the scope un narrowed beyond the ACL).
type UsageQuery struct {
	NotUsedSince  int64    // epoch milliseconds
	CreatedBefore int64    // epoch milliseconds; 0 → NotUsedSince
	Repos         []string // optional repository narrowing (CSV on the wire)
}

// RunUsage executes the usage endpoint's fixed statistics-domain template
// through the very same engine path a hand-written AQL query takes — the
// spec's own "usage REST = statistics domain, one source" shape (aql.md
// §14.1: the endpoint's hit set is behaviorally the template
// (downloaded < T OR null) AND (remote_downloaded < T OR null) AND
// created < T'). The counting columns T-438 landed are the single data
// source; the remote_* arm folds to a constant today and swaps in one
// place should a smart-remote source ever exist.
func (e *Engine) RunUsage(ctx context.Context, p *repo.Principal, uq UsageQuery) (*Result, error) {
	return e.runAST(ctx, p, usageTemplate(uq))
}

// usageTemplate builds the endpoint's AST. The arms land in template
// order: the downloaded disjunction, the remote-downloaded disjunction
// (constant-folded away by the planner — kept literal for parity with the
// spec's recorded internal form), the created bound, then the optional
// repository narrowing. The projection carries the identity triple (repo,
// path, name — the uri's raw material) plus the two counting members the
// five-field row renders; the sort is lastDownloaded ascending (the
// spec's recorded order key, §14.2/V-i).
func usageTemplate(uq UsageQuery) *Query {
	notUsed := time.UnixMilli(uq.NotUsedSince).UTC()
	createdBefore := notUsed
	if uq.CreatedBefore != 0 {
		createdBefore = time.UnixMilli(uq.CreatedBefore).UTC()
	}
	downloadedRef := statRef(FieldStatDownloaded)
	remoteRef := statRef(FieldStatRemoteDownloaded)
	children := []Criteria{
		&Or{Children: []Criteria{
			&Compare{Field: downloadedRef, Op: OpLt,
				Value: Value{Kind: LitString, Str: epochMillisLiteral(notUsed)}},
			&Compare{Field: downloadedRef, Op: OpEq, Value: Value{Kind: LitNull}},
		}},
		&Or{Children: []Criteria{
			&Compare{Field: remoteRef, Op: OpLt,
				Value: Value{Kind: LitString, Str: epochMillisLiteral(notUsed)}},
			&Compare{Field: remoteRef, Op: OpEq, Value: Value{Kind: LitNull}},
		}},
		&Compare{Field: FieldRef{ID: FieldCreated, Name: "created", Domain: DomainItem},
			Op: OpLt, Value: Value{Kind: LitString, Str: epochMillisLiteral(createdBefore)}},
	}
	if len(uq.Repos) > 0 {
		arms := make([]Criteria, 0, len(uq.Repos))
		for _, key := range uq.Repos {
			arms = append(arms, &Compare{
				Field: FieldRef{ID: FieldRepo, Name: "repo", Domain: DomainItem},
				Op:    OpEq, Value: Value{Kind: LitString, Str: key},
			})
		}
		children = append(children, &Or{Children: arms})
	}
	return &Query{
		Domain:   "items",
		Criteria: &And{Children: children},
		Include: []IncludeField{
			{Raw: "repo", Field: FieldRef{ID: FieldRepo, Name: "repo", Domain: DomainItem}},
			{Raw: "path", Field: FieldRef{ID: FieldPath, Name: "path", Domain: DomainItem}},
			{Raw: "name", Field: FieldRef{ID: FieldName, Name: "name", Domain: DomainItem}},
			{Raw: "stat.downloaded", Field: statRef(FieldStatDownloaded)},
			{Raw: "stat.downloads", Field: statRef(FieldStatDownloads)},
		},
		Sort: []SortKey{{Field: statRef(FieldStatDownloaded), Asc: true}},
	}
}

// statRef resolves a statistics FieldID through the registry (the single
// spelling source for hand-built ASTs).
func statRef(id FieldID) FieldRef {
	f, _ := lookupField(string(id))
	return FieldRef{ID: f.ID, Name: f.Name, Domain: f.Domain}
}

// epochMillisLiteral renders an epoch-milliseconds instant as the
// RFC3339 UTC form the planner's date kernel normalizes — millisecond
// precision kept so the strict-< boundary is exact to the wire parameter.
func epochMillisLiteral(t time.Time) string {
	return t.Format("2006-01-02T15:04:05.000Z07:00")
}

// execute is the deadline-bounded segment. Every store touch happens here.
func (e *Engine) execute(ctx context.Context, p *repo.Principal, ast *Query) (*Result, error) {
	plan, err := PlanQuery(ctx, ast, PlanOptions{Now: e.nowFn(), Virtual: e.virtual})
	if err != nil {
		return nil, fmt.Errorf("engine: compiling query: %w", err)
	}
	res := &Result{
		Plan:   plan,
		Offset: plan.Offset, HasOffset: plan.HasOffset,
		Limit: plan.Limit, HasLimit: plan.HasLimit,
	}

	scope, err := e.scope(ctx, p)
	if err != nil {
		return nil, err // ErrForbidden and store failures surface as-is
	}
	if len(scope) == 0 {
		// Readable set empty (or no ACL wired): the honest empty result,
		// never an unfiltered query (ADR-0043 pt 4).
		return res, nil
	}
	if e.nodes == nil {
		return nil, ErrQueryUnavailable
	}

	weaveScope(plan, scope)
	eff := effectiveLimit(plan)
	plan.Query.Limit, plan.Query.HasLimit = eff+1, true // the cap+1 probe

	rows, err := e.nodes.QueryNodes(ctx, plan.Query)
	if err != nil {
		return nil, fmt.Errorf("engine: executing query: %w", err)
	}
	if int64(len(rows)) > eff {
		rows = rows[:eff]
		res.Truncated = true
	}
	rows = e.recheckRows(ctx, p, scope, rows)
	obfuscateRows(p, rows)
	res.Rows = rows
	return res, nil
}

// scope resolves stage one of the read weave; a nil ACL fails closed (the
// empty scope).
func (e *Engine) scope(ctx context.Context, p *repo.Principal) ([]repo.ReadScope, error) {
	if e.acl == nil {
		return nil, nil
	}
	return e.acl.SearchScope(ctx, p)
}

// effectiveLimit is the window the engine materializes: the query's own
// limit when it is tighter than the cap, the cap otherwise (a query with
// no .limit() gets the cap — K63's "no default limit" rejection, aql.md
// §5's C-layer row).
func effectiveLimit(plan *Plan) int64 {
	if plan.HasLimit && plan.Limit < ResultCap {
		return plan.Limit
	}
	return ResultCap
}

// weaveScope ANDs the readable repository set into the IR's WHERE tree as
// a repo_key disjunction — the repoFilter narrowing hoisted to the whole
// scope (ADR-0043 pt 4). The conjunct joins the top-level AND so the
// compiler's property-join hoisting keeps working (aqlquery.go's whereSQL
// only hoists top-level conjuncts).
func weaveScope(plan *Plan, scope []repo.ReadScope) {
	children := make([]metadata.NodePredicate, 0, len(scope))
	for _, s := range scope {
		children = append(children, &metadata.QueryCompare{
			Field: metadata.QueryRepo, Op: metadata.QueryEq, Value: s.Repo,
		})
	}
	group := &metadata.QueryOr{Children: children}
	switch where := plan.Query.Where.(type) {
	case nil:
		plan.Query.Where = group
	case *metadata.QueryAnd:
		where.Children = append(where.Children, group)
	default:
		plan.Query.Where = &metadata.QueryAnd{
			Children: []metadata.NodePredicate{plan.Query.Where, group},
		}
	}
}

// recheckRows is stage two of the read weave: only the rows of
// path-scoped repositories clear CanRead (the same allow() a download
// runs). Repositories whose grants carry no patterns skip the stage
// entirely — the dominant production shape pays zero per-row checks
// (ADR-0043 pt 4).
func (e *Engine) recheckRows(ctx context.Context, p *repo.Principal, scope []repo.ReadScope, rows []*metadata.NodeQueryRow) []*metadata.NodeQueryRow {
	var pathScoped map[string]bool
	for _, s := range scope {
		if s.PathScoped {
			if pathScoped == nil {
				pathScoped = make(map[string]bool, len(scope))
			}
			pathScoped[s.Repo] = true
		}
	}
	if pathScoped == nil {
		return rows
	}
	out := rows[:0]
	for _, r := range rows {
		if pathScoped[r.RepoKey] && !e.acl.CanRead(ctx, p, r.RepoKey, r.Path) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// obfuscateRows applies the non-admin identity masking (aql.md §6 /
// ADR-0043 Errata ⑧; §14.1 extends it to the statistics domain's
// downloaded_by): created_by becomes the literal "unknown" for every
// caller but full admins, and downloaded_by joins it — an EMPTY
// downloaded_by (never downloaded) stays empty: it renders as the null
// arm on the wire, and "unknown" is an identity, not a null. The rows are
// engine-owned copies of the result set (never shared store state), so
// the decoration is safe in place.
func obfuscateRows(p *repo.Principal, rows []*metadata.NodeQueryRow) {
	if p != nil && p.Admin {
		return
	}
	for _, r := range rows {
		r.CreatedBy = obfuscatedUser
		if r.LastDownloadedBy != "" {
			r.LastDownloadedBy = obfuscatedUser
		}
	}
}

// isQueryError keeps parser rejections recognizable inside the timeout
// mapping (a QueryError can no longer surface past Parse, but the check
// makes the invariant explicit rather than load-bearing on call order).
func isQueryError(err error) bool {
	var qe *QueryError
	return errors.As(err, &qe)
}

// ---- the build-family execution segment (M17 T-511, aql.md §15) ----

// executeBuild runs one builds/modules/dependencies query: plan →
// enumerate (the searcher's complete, unfiltered plane) → criteria
// evaluation → the row-level ACL weave → obfuscation → sort → window →
// cap. Every stage mirrors its items-path twin so the two families share
// one posture: visible rows only, bounded windows, masked identities, one
// truncation rule.
func (e *Engine) executeBuild(ctx context.Context, p *repo.Principal, ast *Query) (*Result, error) {
	if e.builds == nil {
		return nil, ErrBuildSearchUnavailable
	}
	plan, err := PlanBuildQuery(ast, PlanOptions{Now: e.nowFn()})
	if err != nil {
		return nil, fmt.Errorf("engine: compiling build query: %w", err)
	}
	res := &Result{
		Plan: &Plan{
			Output: plan.Output,
			Offset: plan.Offset, HasOffset: plan.HasOffset,
			Limit: plan.Limit, HasLimit: plan.HasLimit,
		},
		EntryDomain: plan.Entry,
		Offset:      plan.Offset, HasOffset: plan.HasOffset,
		Limit: plan.Limit, HasLimit: plan.HasLimit,
	}

	var rows []*BuildRow
	switch plan.Entry {
	case "builds":
		runs, err := e.builds.Runs(ctx)
		if err != nil {
			return nil, fmt.Errorf("engine: enumerating build runs: %w", err)
		}
		rows = buildRowsFromRuns(runs)
	case "modules":
		mods, err := e.builds.Modules(ctx)
		if err != nil {
			return nil, fmt.Errorf("engine: enumerating build modules: %w", err)
		}
		rows = buildRowsFromModules(mods)
	case "dependencies":
		mods, err := e.builds.Modules(ctx)
		if err != nil {
			return nil, fmt.Errorf("engine: enumerating build dependencies: %w", err)
		}
		rows = buildRowsFromDependencies(mods)
	default:
		return nil, fmt.Errorf("engine: unknown build entry %q", plan.Entry)
	}

	if plan.Where != nil {
		kept := make([]*BuildRow, 0, len(rows))
		for _, r := range rows {
			if plan.Where(r) {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	rows = e.filterBuildRows(ctx, p, rows)
	obfuscateBuildRows(p, rows)
	sortBuildRows(rows, plan.Sort)

	// The window: the query's own offset first, then the effective limit
	// (its own limit when tighter than the cap, the cap otherwise — the
	// same effectiveLimit rule the items path runs); rows past the window
	// set Truncated, the cap+1 probe's structural equivalent over a fully
	// materialized plane.
	off := int64(0)
	if plan.HasOffset {
		off = plan.Offset
	}
	if off > int64(len(rows)) {
		off = int64(len(rows))
	}
	rows = rows[off:]
	eff := int64(ResultCap)
	if plan.HasLimit && plan.Limit < eff {
		eff = plan.Limit
	}
	if int64(len(rows)) > eff {
		rows = rows[:eff]
		res.Truncated = true
	}
	res.BuildRows = rows
	return res, nil
}

// filterBuildRows is the build family's read weave (aql.md §6 + §15.2):
// every row is re-checked against r(buildRepo, buildName) — the SAME
// allow() mirror the build read faces run, injected as the searcher's
// CanReadBuild. Decisions are memoized per (repo, name): a run's rows
// share one verdict, and a build with a hundred dependencies costs one
// evaluation, not a hundred.
func (e *Engine) filterBuildRows(ctx context.Context, p *repo.Principal, rows []*BuildRow) []*BuildRow {
	verdicts := make(map[string]bool, 8)
	out := make([]*BuildRow, 0, len(rows))
	for _, r := range rows {
		key := r.Repo + "\x00" + r.Name
		visible, seen := verdicts[key]
		if !seen {
			visible = e.builds.CanReadBuild(ctx, p, r.Repo, r.Name)
			verdicts[key] = visible
		}
		if visible {
			out = append(out, r)
		}
	}
	return out
}

// obfuscateBuildRows applies the non-admin identity masking to the build
// family (aql.md §6 — created_by/modified_by join the masked set; the
// builds entry projects both by default, §15.1).
func obfuscateBuildRows(p *repo.Principal, rows []*BuildRow) {
	if p != nil && p.Admin {
		return
	}
	for _, r := range rows {
		r.CreatedBy = obfuscatedUser
		r.UpdatedBy = obfuscatedUser
	}
}

// sortBuildRows orders the rows by the query's sort keys (stable — the
// natural materialization index is the deterministic tiebreak, the same
// (repo, path) role the items compiler's trailing order plays).
func sortBuildRows(rows []*BuildRow, keys []buildSortKey) {
	if len(keys) == 0 {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		for _, k := range keys {
			av := buildSortableValue(a, k.Field)
			bv := buildSortableValue(b, k.Field)
			if av == bv {
				continue
			}
			if av < bv {
				return k.Asc
			}
			return !k.Asc
		}
		return a.order < b.order
	})
}
