package search

import (
	"testing"
	"time"
)

// The dates/creation fixed templates (T-452, aql.md §14.3): the compile
// legs live here — the templates must build ASTs the planner accepts, and
// the interval/exclusion/field-map shapes must be exactly the anchor's.
// The wire legs (envelope, 404 family, V-o echo fallback, ACL) run through
// the real HTTP stack in internal/httpapi/search_dates_test.go.

func TestCreationTemplateCompilesAndExcludes(t *testing.T) {
	ast := creationTemplate(CreationQuery{
		From:  1_700_000_000_000,
		To:    1_700_000_060_000,
		Repos: []string{"r-local"},
	}, time.UnixMilli(1_700_000_120_000).UTC())
	if _, err := PlanQuery(t.Context(), ast, PlanOptions{}); err != nil {
		t.Fatalf("creation template does not compile: %v", err)
	}
	where := ast.Criteria.(*And)
	// The maven-metadata.xml exclusion rides the criteria tree's head.
	first, ok := where.Children[0].(*Compare)
	if !ok || first.Field.ID != FieldName || first.Op != OpNe || first.Value.Str != "maven-metadata.xml" {
		t.Fatalf("exclusion conjunct = %#v, want name != maven-metadata.xml", where.Children[0])
	}
	// The OR group spans created and modified; the repo narrowing is the
	// third conjunct.
	if len(where.Children) != 3 {
		t.Fatalf("children = %d, want 3 (exclusion, range OR, repo OR)", len(where.Children))
	}
}

func TestDatesTemplateFieldMapAndDefault(t *testing.T) {
	now := time.UnixMilli(1_700_000_120_000).UTC()
	// No dateFields: the default pair {created, lastModified} (V-k).
	ast := datesTemplate(DatesQuery{From: 1_700_000_000_000}, now)
	if _, err := PlanQuery(t.Context(), ast, PlanOptions{}); err != nil {
		t.Fatalf("default dates template does not compile: %v", err)
	}
	or := ast.Criteria.(*And).Children[1].(*Or)
	if len(or.Children) != 2 {
		t.Fatalf("default field arms = %d, want the {created, lastModified} pair", len(or.Children))
	}

	// Every legal name maps onto a registry ref; the closed set is exactly
	// the wire echo.
	for _, name := range []string{"created", "lastModified", "lastDownloaded", "remote_last_downloaded"} {
		if !ValidDatesField(name) {
			t.Fatalf("%q is not a valid dateFields name", name)
		}
		ast := datesTemplate(DatesQuery{From: 1, To: 2, Fields: []string{name}}, now)
		if _, err := PlanQuery(t.Context(), ast, PlanOptions{}); err != nil {
			t.Fatalf("dates template for %q does not compile: %v", name, err)
		}
	}
	if ValidDatesField("created_at") {
		t.Fatal("unknown field name accepted")
	}

	// The interval shape: one arm is `> from AND <= to` (§14.3's strict
	// boundaries).
	ast = datesTemplate(DatesQuery{From: 1_700_000_000_000, To: 1_700_000_060_000, Fields: []string{"created"}}, now)
	arm := ast.Criteria.(*And).Children[1].(*Or).Children[0].(*And)
	gt, gtOK := arm.Children[0].(*Compare)
	lt, ltOK := arm.Children[1].(*Compare)
	if !gtOK || !ltOK || gt.Op != OpGt || lt.Op != OpLte {
		t.Fatalf("arm = %#v, want > from AND <= to", arm)
	}
	if gt.Value.Str != "2023-11-14T22:13:20.000Z" || lt.Value.Str != "2023-11-14T22:14:20.000Z" {
		t.Fatalf("boundaries = %q..%q, want the epoch-ms instants at millisecond precision",
			gt.Value.Str, lt.Value.Str)
	}
}
