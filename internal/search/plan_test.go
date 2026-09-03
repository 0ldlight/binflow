package search

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// T-411 AC1: the AST→IR snapshot leg. Every case pins the planner's
// decisions — the implicit file predicate, the parent-path semantics, the
// partial-date period arms, $last/$before boundaries, the property
// condition shapes, $msp same-instance flattening, virtual expansion and
// the projection echo list. The IR→SQL leg lives in
// internal/metadata/aqlquery_internal_test.go; the two tables meet in the
// end-to-end chain test (plan_chain_test.go).

// mustPlanQuery parses and compiles q, failing the test on any error.
func mustPlanQuery(t *testing.T, q string, opt PlanOptions) *Plan {
	t.Helper()
	ast, err := Parse(q)
	if err != nil {
		t.Fatalf("Parse(%q): %v", q, err)
	}
	plan, err := PlanQuery(context.Background(), ast, opt)
	if err != nil {
		t.Fatalf("PlanQuery(%q): %v", q, err)
	}
	return plan
}

// renderPredicate renders the IR predicate tree deterministically for the
// snapshot table (test-local: the wire shape is T-413/T-415 business).
func renderPredicate(p metadata.NodePredicate) string {
	switch n := p.(type) {
	case *metadata.QueryAnd:
		return "and(" + renderChildren(n.Children) + ")"
	case *metadata.QueryOr:
		return "or(" + renderChildren(n.Children) + ")"
	case *metadata.QueryNot:
		return "not(" + renderPredicate(n.Child) + ")"
	case *metadata.QueryFalse:
		return "false"
	case *metadata.QueryFolderTest:
		if n.Folder {
			return "folder"
		}
		return "file"
	case *metadata.QueryCompare:
		return fmt.Sprintf("cmp(%s %s %s)", n.Field, n.Op, quote(n.Value))
	case *metadata.QueryPropExists:
		return "prop[" + renderConds(n.Conds) + "]"
	case *metadata.QueryPropSame:
		return "msp[" + renderConds(n.Conds) + "]"
	case *metadata.QueryZero:
		if n.Negate {
			return fmt.Sprintf("nonzero(%s)", n.Field)
		}
		return fmt.Sprintf("zero(%s)", n.Field)
	}
	return fmt.Sprintf("%T", p)
}

func renderChildren(children []metadata.NodePredicate) string {
	parts := make([]string, 0, len(children))
	for _, ch := range children {
		parts = append(parts, renderPredicate(ch))
	}
	return strings.Join(parts, ", ")
}

func renderConds(conds []metadata.QueryPropCond) string {
	parts := make([]string, 0, len(conds))
	for _, cd := range conds {
		arm := "name"
		if cd.OnValue {
			arm = "value"
		}
		parts = append(parts, fmt.Sprintf("%s %s %q", arm, cd.Op, cd.Value))
	}
	return strings.Join(parts, "; ")
}

func quote(v any) string {
	if i, ok := v.(int64); ok {
		return fmt.Sprintf("%d", i)
	}
	return fmt.Sprintf("%q", v)
}

func TestPlanPredicateSnapshots(t *testing.T) {
	tests := []struct {
		name string
		aql  string
		want string
	}{
		{"empty criteria adds the implicit file predicate",
			`items.find({})`, "and(file)"},
		{"repo equality",
			`items.find({"repo":"libs"})`, `and(cmp(repo eq "libs"), file)`},
		{"implicit conjunction of two fields",
			`items.find({"repo":"libs","path":"org/foo"})`,
			`and(cmp(repo eq "libs"), cmp(path eq "org/foo"), file)`},
		{"disjunction",
			`items.find({"$or":[{"repo":"a"},{"repo":"b"}]})`,
			`and(or(cmp(repo eq "a"), cmp(repo eq "b")), file)`},
		{"type any is the query's own type condition",
			`items.find({"type":"any"})`, "and()"},
		{"type folder keeps folders",
			`items.find({"type":"folder"})`, "folder"},
		{"type $ne file is folders",
			`items.find({"type":{"$ne":"file"}})`, "not(file)"},
		{"type $ne any is unsatisfiable",
			`items.find({"type":{"$ne":"any"}})`, "false"},
		{"name wildcard translates through the single kernel",
			`items.find({"name":{"$match":"*.jar"}})`,
			`and(cmp(name like "%.jar"), file)`},
		{"nmatch with every wildcard kind and escapes",
			`items.find({"name":{"$nmatch":"a?b%c\\d"}})`,
			`and(cmp(name notlike "a_b\\%c\\\\d"), file)`},
		{"depth compares int64",
			`items.find({"depth":{"$gte":3}})`, "and(cmp(depth gte 3), file)"},
		{"size accepts quoted digits",
			`items.find({"size":{"$lt":"1024"}})`, "and(cmp(size lt 1024), file)"},
		{"exact date compares the instant",
			`items.find({"created":{"$gt":"2026-08-23T08:19:04Z"}})`,
			`and(cmp(created gt "2026-08-23T08:19:04Z"), file)`},
		{"date with offset normalizes to UTC",
			`items.find({"created":{"$lt":"2026-08-23T10:19:04+02:00"}})`,
			`and(cmp(created lt "2026-08-23T08:19:04Z"), file)`},
		{"month partial as lower bound includes the month",
			`items.find({"created":{"$gte":"2026-08"}})`,
			`and(cmp(created gte "2026-08-01T00:00:00Z"), file)`},
		{"month partial as strict upper bound excludes the month",
			`items.find({"created":{"$lt":"2026-08"}})`,
			`and(cmp(created lt "2026-08-01T00:00:00Z"), file)`},
		{"month partial equality is the whole period",
			`items.find({"created":"2026-08"})`,
			`and(cmp(created gte "2026-08-01T00:00:00Z"), cmp(created lt "2026-09-01T00:00:00Z"), file)`},
		{"year partial leaves the period under $gt",
			`items.find({"created":{"$gt":"2026"}})`,
			`and(cmp(created gte "2027-01-01T00:00:00Z"), file)`},
		{"day partial under $lte covers the day",
			`items.find({"modified":{"$lte":"2026-08-23"}})`,
			`and(cmp(modified lt "2026-08-24T00:00:00Z"), file)`},
		{"date $ne is the negated period",
			`items.find({"updated":{"$ne":"2026"}})`,
			`and(not(and(cmp(updated gte "2026-01-01T00:00:00Z"), cmp(updated lt "2027-01-01T00:00:00Z"))), file)`},
		{"@key shorthand",
			`items.find({"@license":"Apache-2.0"})`,
			`and(prop[name eq "license"; value eq "Apache-2.0"], file)`},
		{"@key star value is key existence",
			`items.find({"@license":"*"})`,
			`and(prop[name eq "license"], file)`},
		{"@* catch-all constrains the value only",
			`items.find({"@*":"v1"})`, `and(prop[value eq "v1"], file)`},
		{"property long forms",
			`items.find({"property.key":"build","property.value":{"$match":"b*"}})`,
			`and(prop[name eq "build"], prop[value like "b%"], file)`},
		{"$msp flattens onto one property instance",
			`items.find({"$msp":[{"@env":"prod"},{"@env":{"$nmatch":"dev*"}}]})`,
			`and(msp[name eq "env"; value eq "prod"; name eq "env"; value notlike "dev%"], file)`},
		{"created_by and checksums address storage directly",
			`items.find({"created_by":"alice","actual_sha1":"8ddc"})`,
			`and(cmp(created_by eq "alice"), cmp(sha1 eq "8ddc"), file)`},

		// The statistics domain (T-440, aql.md §14.1): the null literal
		// lowers onto the structural-zero predicates, the counting columns
		// behave like their kinds, and the constant-zero remote_* stubs
		// fold — a vacuous arm inside an Or ABSORBS the disjunction.
		{"stat counter",
			`items.find({"stat.downloads":{"$gte":3}})`,
			"and(cmp(downloads gte 3), file)"},
		{"stat downloaded date arm",
			`items.find({"stat.downloaded":{"$lt":"2026-09-01T13:00:00.000Z"}})`,
			`and(cmp(downloaded lt "2026-09-01T13:00:00Z"), file)`},
		{"stat null literals are the structural zeros",
			`items.find({"stat.downloads":{"$eq":null},"stat.downloaded":{"$ne":null}})`,
			"and(zero(downloads), nonzero(downloaded), file)"},
		{"stat downloaded_by string arm",
			`items.find({"stat.downloaded_by":{"$eq":"bob"}})`,
			`and(cmp(downloaded_by eq "bob"), file)`},
		{"stub counter folds against its constant",
			`items.find({"stat.remote_downloads":{"$gt":0}})`, "and(false, file)"},
		{"stub null matches everything",
			`items.find({"stat.remote_downloaded":{"$eq":null},"repo":"libs"})`,
			`and(cmp(repo eq "libs"), file)`},
		{"a vacuous Or arm absorbs the disjunction",
			`items.find({"$or":[{"stat.remote_downloaded":{"$eq":null}},{"repo":"libs"}]})`,
			"and(file)"},
		{"the usage template shape folds its remote arm away",
			`items.find({"$or":[{"stat.remote_downloaded":{"$lt":"2030-01-01"}},{"stat.remote_downloaded":{"$eq":null}}],"repo":"libs"})`,
			`and(cmp(repo eq "libs"), file)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := mustPlanQuery(t, tt.aql, PlanOptions{})
			if got := renderPredicate(plan.Query.Where); got != tt.want {
				t.Errorf("where:\n got %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestPlanRelativeDates(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		aql  string
		want string
	}{
		{"$last weeks", `items.find({"created":{"$last":"2 weeks"}})`,
			`and(cmp(created gt "2026-08-18T12:00:00Z"), file)`},
		{"$before days", `items.find({"updated":{"$before":"3 days"}})`,
			`and(cmp(updated lt "2026-08-29T12:00:00Z"), file)`},
		{"$last months uses calendar arithmetic", `items.find({"created":{"$last":"1 months"}})`,
			`and(cmp(created gt "2026-08-01T12:00:00Z"), file)`},
		{"$last years uses calendar arithmetic", `items.find({"created":{"$last":"1 years"}})`,
			`and(cmp(created gt "2025-09-01T12:00:00Z"), file)`},
		{"$last millis", `items.find({"created":{"$last":"500 millis"}})`,
			`and(cmp(created gt "2026-09-01T11:59:59.5Z"), file)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := mustPlanQuery(t, tt.aql, PlanOptions{Now: now})
			if got := renderPredicate(plan.Query.Where); got != tt.want {
				t.Errorf("where:\n got %s\nwant %s", got, tt.want)
			}
		})
	}

	// The clock is a hard requirement: a relative operand with the zero
	// clock is a planner error, never a silently wrong boundary.
	_, err := PlanQuery(context.Background(), mustParse(t, `items.find({"created":{"$last":"1 days"}})`), PlanOptions{})
	if err == nil || !strings.Contains(err.Error(), "clock") {
		t.Fatalf("zero-clock relative date must fail with a clock error, got %v", err)
	}
	// Absurd counts are refused rather than overflowing the calendar.
	_, err = PlanQuery(context.Background(),
		mustParse(t, `items.find({"created":{"$last":"999999999999 years"}})`),
		PlanOptions{Now: now})
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("absurd period must fail with a range error, got %v", err)
	}
}

// stubVirtualResolver is the T-413 stand-in: one virtual, two members.
type stubVirtualResolver struct {
	members map[string][]string
	calls   []string
}

func (s *stubVirtualResolver) Members(_ context.Context, key string) ([]string, error) {
	s.calls = append(s.calls, key)
	return s.members[key], nil
}

func TestPlanVirtualRepoExpansion(t *testing.T) {
	resolver := &stubVirtualResolver{members: map[string][]string{
		"dist": {"libs", "libs-cache"},
	}}
	tests := []struct {
		name string
		aql  string
		want string
	}{
		{"$eq is replaced by the member group",
			`items.find({"repo":"dist"})`,
			`and(or(cmp(repo eq "libs"), cmp(repo eq "libs-cache")), file)`},
		{"literal $match keeps the original and ORs the members",
			`items.find({"repo":{"$match":"dist"}})`,
			`and(or(cmp(repo like "dist"), or(cmp(repo eq "libs"), cmp(repo eq "libs-cache"))), file)`},
		{"$ne keeps the original and AND-excludes the members",
			`items.find({"repo":{"$ne":"dist"}})`,
			`and(cmp(repo ne "dist"), not(or(cmp(repo eq "libs"), cmp(repo eq "libs-cache"))), file)`},
		{"pattern $match passes through (a pattern cannot name one key)",
			`items.find({"repo":{"$match":"*dist*"}})`,
			`and(cmp(repo like "%dist%"), file)`},
		{"non-virtual keys pass through",
			`items.find({"repo":"plain"})`, `and(cmp(repo eq "plain"), file)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := mustPlanQuery(t, tt.aql, PlanOptions{Virtual: resolver})
			if got := renderPredicate(plan.Query.Where); got != tt.want {
				t.Errorf("where:\n got %s\nwant %s", got, tt.want)
			}
		})
	}
	if len(resolver.calls) == 0 {
		t.Fatal("resolver was never consulted")
	}
	// Without a resolver every value passes through literally (the shape
	// snapshot tests compile under this arm).
	plan := mustPlanQuery(t, `items.find({"repo":"dist"})`, PlanOptions{})
	if got := renderPredicate(plan.Query.Where); got != `and(cmp(repo eq "dist"), file)` {
		t.Fatalf("nil resolver must not expand: %s", got)
	}
	// A resolver failure wraps the offending key.
	failing := &failingResolver{}
	_, err := PlanQuery(context.Background(), mustParse(t, `items.find({"repo":"dist"})`), PlanOptions{Virtual: failing})
	if err == nil || !strings.Contains(err.Error(), `"dist"`) || !errors.Is(err, errStub) {
		t.Fatalf("resolver failure must wrap the key and the cause, got %v", err)
	}
}

type failingResolver struct{}

var errStub = errors.New("stub failure")

func (*failingResolver) Members(_ context.Context, _ string) ([]string, error) {
	return nil, errStub
}

func TestPlanProjectionAndWindow(t *testing.T) {
	t.Run("default output set drops only modified_by", func(t *testing.T) {
		plan := mustPlanQuery(t, `items.find({"repo":"libs"})`, PlanOptions{})
		var keys []string
		for _, o := range plan.Output {
			keys = append(keys, o.Key)
		}
		want := "repo,path,name,type,size,created,created_by,modified,updated"
		if strings.Join(keys, ",") != want {
			t.Fatalf("default output = %v, want %s", keys, want)
		}
		for _, o := range plan.Output {
			if o.Field == FieldModifiedBy {
				t.Fatalf("modified_by must not be projected: %v", plan.Output)
			}
		}
	})
	t.Run("include echoes query order and collects props", func(t *testing.T) {
		plan := mustPlanQuery(t,
			`items.find({"repo":"libs"}).include("name","repo","@license","virtual_repos")`, PlanOptions{})
		var keys []string
		for _, o := range plan.Output {
			keys = append(keys, o.Key)
		}
		if strings.Join(keys, ",") != "name,repo,@license,virtual_repos" {
			t.Fatalf("output echo = %v", keys)
		}
		if plan.Output[2].Kind != OutputProp || plan.Output[2].PropKey != "license" {
			t.Fatalf("prop echo = %+v", plan.Output[2])
		}
		if plan.Output[3].Kind != OutputVirtualRepos {
			t.Fatalf("virtual_repos echo = %+v", plan.Output[3])
		}
		if fmt.Sprint(plan.Query.Props) != "[license]" {
			t.Fatalf("props = %v", plan.Query.Props)
		}
		if fmt.Sprint(plan.Query.Fields) != "[name repo]" {
			t.Fatalf("fields = %v", plan.Query.Fields)
		}
	})
	t.Run("include star covers every storage-backed field", func(t *testing.T) {
		plan := mustPlanQuery(t, `items.find({}).include("*")`, PlanOptions{})
		var keys []string
		for _, o := range plan.Output {
			keys = append(keys, o.Key)
		}
		want := "repo,path,name,type,size,created,created_by,modified,updated,depth,actual_md5,actual_sha1,sha256,*"
		if strings.Join(keys, ",") != want {
			t.Fatalf("star output = %v, want %s", keys, want)
		}
	})
	t.Run("property long forms project the catch-all", func(t *testing.T) {
		plan := mustPlanQuery(t, `items.find({}).include("property.key","property.value")`, PlanOptions{})
		if fmt.Sprint(plan.Query.Props) != "[* *]" {
			t.Fatalf("props = %v", plan.Query.Props)
		}
		if plan.Output[0].Kind != OutputProp || plan.Output[1].Kind != OutputProp {
			t.Fatalf("long forms must be property projections: %+v", plan.Output)
		}
	})
	t.Run("window echoes keep zero distinct from absent", func(t *testing.T) {
		plan := mustPlanQuery(t, `items.find({}).offset(0).limit(0)`, PlanOptions{})
		if !plan.HasOffset || plan.Offset != 0 || !plan.HasLimit || plan.Limit != 0 {
			t.Fatalf("explicit zeros must stay distinguishable: %+v", plan)
		}
		if !plan.Query.HasOffset || plan.Query.Offset != 0 || !plan.Query.HasLimit || plan.Query.Limit != 0 {
			t.Fatalf("window must ride the IR too: %+v", plan.Query)
		}
		plain := mustPlanQuery(t, `items.find({})`, PlanOptions{})
		if plain.HasOffset || plain.HasLimit {
			t.Fatalf("absent window must stay absent: %+v", plain)
		}
	})
	t.Run("sort lowers onto storage keys", func(t *testing.T) {
		plan := mustPlanQuery(t, `items.find({}).include("name","created").sort({"$desc":["created","name"]})`, PlanOptions{})
		if fmt.Sprint(plan.Query.Sort) != "[{created false} {name false}]" {
			t.Fatalf("sort = %v", plan.Query.Sort)
		}
	})
	t.Run("nil query is refused", func(t *testing.T) {
		if _, err := PlanQuery(context.Background(), nil, PlanOptions{}); err == nil {
			t.Fatal("nil query must fail")
		}
	})
}
