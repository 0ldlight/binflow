package search

// T-511 (FR-152.3 / aql.md §15): the build-family entry domains — the
// three entrances' parse/projection/evaluation contract. The §15.1 field
// table is frozen verbatim here (the assertion-inversion ⑥ positive
// halves: builds/modules/dependencies went 400 → query green); the
// still-closed faces (promotions/properties/artifacts/sensitive) keep
// their honest refusals in errors_test.go's rows.

import (
	"errors"
	"testing"
	"time"
)

// TestBuildEntriesParseGreen is AC1's first leg: the three entrances parse
// end to end — criteria, include, sort, offset, limit — with the entry's
// own field set, and the AST carries the entry spelling.
func TestBuildEntriesParseGreen(t *testing.T) {
	for _, q := range []string{
		`builds.find({"name":"cli"})`,
		`builds.find({"name":{"$match":"cli-*"},"number":{"$eq":"51"}}).include("name","number","repo")`,
		`builds.find({}).sort({"$desc":["started"]}).offset(1).limit(2)`,
		`modules.find({"name":{"$eq":"com.example:api:1.0"}})`,
		`modules.find({}).include("*")`,
		`dependencies.find({"scope":{"$eq":"test"},"type":"jar"})`,
		`dependencies.find({}).include("name","sha1","md5")`,
		`dependencies.find({"sha1":{"$eq":"aa"}}).sort({"$asc":["name"]})`,
	} {
		ast, err := Parse(q)
		if err != nil {
			t.Fatalf("Parse(%s): %v", q, err)
		}
		switch ast.Domain {
		case "builds", "modules", "dependencies":
		default:
			t.Fatalf("Parse(%s): domain = %q", q, ast.Domain)
		}
	}
}

// TestBuildDefaultOutputFreeze pins §15.1's default-output flags: builds
// renders nine members (url included), modules one, dependencies three —
// and include replaces the set while include("*") restores every
// projectable member.
func TestBuildDefaultOutputFreeze(t *testing.T) {
	tests := []struct {
		query   string
		want    []string
		isMatch bool
	}{
		{`builds.find({})`, []string{"url", "name", "number", "started", "repo", "created", "created_by", "modified", "modified_by"}, false},
		{`modules.find({})`, []string{"name"}, false},
		{`dependencies.find({})`, []string{"name", "scope", "type"}, false},
		{`builds.find({}).include("name")`, []string{"name"}, false},
		{`dependencies.find({}).include("name")`, []string{"name"}, false},
		{`dependencies.find({}).include("name","sha1","md5")`, []string{"name", "sha1", "md5"}, false},
		{`modules.find({}).include("*")`, []string{"name"}, false},
		{`dependencies.find({}).include("*")`, []string{"name", "scope", "type", "sha1", "md5"}, false},
		{`builds.find({}).include("*")`, []string{"url", "name", "number", "started", "repo", "created", "created_by", "modified", "modified_by"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			ast, err := Parse(tt.query)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			plan, err := PlanBuildQuery(ast, PlanOptions{Now: time.Now().UTC()})
			if err != nil {
				t.Fatalf("PlanBuildQuery: %v", err)
			}
			if len(plan.Output) != len(tt.want) {
				t.Fatalf("output = %v, want %v", outputKeys(plan.Output), tt.want)
			}
			for i, f := range plan.Output {
				if f.Key != tt.want[i] || f.Kind != OutputItem {
					t.Fatalf("output[%d] = %+v, want %s", i, f, tt.want[i])
				}
			}
		})
	}
}

func outputKeys(out []OutputField) []string {
	keys := make([]string, 0, len(out))
	for _, f := range out {
		keys = append(keys, f.Key)
	}
	return keys
}

// TestBuildEntryRejections pins the field-level honesty inside the three
// entrances: item-only fields do not exist there, the internal ids refuse,
// @key arms refuse (property entries stay closed), null refuses, url is
// output-only, and sort must name result fields.
func TestBuildEntryRejections(t *testing.T) {
	tests := []struct {
		name  string
		query string
		kind  ErrorKind
		field string
		msg   string
	}{
		{"item-field-unknown-in-builds", `builds.find({"path":"x"})`, ErrUnknownField, "path",
			"Unknown AQL field: path"},
		{"item-field-unknown-in-deps", `dependencies.find({"repo":"x"})`, ErrUnknownField, "repo",
			"Unknown AQL field: repo"},
		{"internal-id", `builds.find({"id":{"$eq":1}})`, ErrUnsupportedField, "id",
			"AQL field not supported yet: id (internal field, not exposed)"},
		{"dep-internal-id", `dependencies.find({"id":{"$eq":1}})`, ErrUnsupportedField, "id",
			"AQL field not supported yet: id (internal field, not exposed)"},
		{"at-key-refused", `builds.find({"@os":"linux"})`, ErrUnsupportedField, "@os",
			"AQL property criteria are not part of the build-family subset: @os (property entries stay closed, aql.md section 15.3)"},
		{"at-key-include-refused", `builds.find({}).include("@os")`, ErrUnsupportedField, "@os",
			"AQL property projections are not part of the build-family subset: @os (property entries stay closed, aql.md section 15.3)"},
		{"quoted-at-key-refused", `modules.find({"@env":"prod"})`, ErrUnsupportedField, "@env",
			"AQL property criteria are not part of the build-family subset: @env (property entries stay closed, aql.md section 15.3)"},
		{"null-refused", `builds.find({"name":null})`, ErrBadValue, "name",
			"AQL null values are not supported for field name (only statistics-domain fields use null)"},
		{"url-output-only", `builds.find({"url":{"$eq":"https://ci"}})`, ErrUnsupportedField, "url",
			"AQL field is output-only, not usable in criteria: url"},
		{"url-not-sortable", `builds.find({}).sort({"$asc":["url"]})`, ErrSortField, "url",
			"AQL field is not sortable: url"},
		{"sort-outside-include", `builds.find({}).include("name").sort({"$asc":["number"]})`, ErrSortField, "number",
			"Only the result fields are allowed to use in the sort section."},
		{"number-on-string", `builds.find({"name":5})`, ErrBadValue, "name",
			"Invalid value for field name: 5 (string required)"},
		{"bad-date", `builds.find({"started":"2026-13-01"})`, ErrBadValue, "started",
			"Invalid date value for field started: 2026-13-01"},
		{"relative-on-string", `builds.find({"name":{"$last":"3 days"}})`, ErrBadOperator, "name",
			"AQL operator not allowed: $last on field name ($last and $before apply to date fields only)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qe := parseMustFail(t, tt.query)
			if qe.Kind != tt.kind {
				t.Fatalf("kind = %s, want %s", qe.Kind, tt.kind)
			}
			if qe.Field != tt.field {
				t.Fatalf("field = %q, want %q", qe.Field, tt.field)
			}
			if qe.Msg != tt.msg {
				t.Fatalf("msg:\n got %s\nwant %s", qe.Msg, tt.msg)
			}
		})
	}
}

func parseMustFail(t *testing.T, query string) *QueryError {
	t.Helper()
	_, err := Parse(query)
	if err == nil {
		t.Fatalf("Parse(%s) unexpectedly succeeded", query)
	}
	var qe *QueryError
	if !errors.As(err, &qe) {
		t.Fatalf("Parse(%s) error = %T, want *QueryError", query, err)
	}
	return qe
}

// TestBuildPredicateEvaluation drives the lowered comparator over row
// fixtures: string order/wildcards, date partial precision and edges,
// $last/$before, and the unparseable-date NULL rule.
func TestBuildPredicateEvaluation(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	row := &BuildRow{
		Name: "cli", Number: "51", Started: "2026-09-05T10:00:00.000+0000",
		Repo: "artifactory-build-info", URL: "https://ci/job/cli/51",
		CreatedAt: "2026-09-05T10:01:00Z", CreatedBy: "jenkins",
		ModuleID: "com.example:api:1.0",
		DepID:    "junit:junit:4.13", DepType: "jar", DepScopes: "compile,test",
		DepSha1: "dd", DepMd5: "cc",
	}
	badDateRow := &BuildRow{Name: "cli", Started: "not-a-date"}

	tests := []struct {
		name  string
		query string
		row   *BuildRow
		want  bool
	}{
		{"string-eq", `builds.find({"name":"cli"})`, row, true},
		{"string-eq-miss", `builds.find({"name":"web"})`, row, false},
		{"string-ne", `builds.find({"name":{"$ne":"web"}})`, row, true},
		{"string-gt", `builds.find({"number":{"$gt":"50"}})`, row, true},
		{"string-lte", `builds.find({"number":{"$lte":"51"}})`, row, true},
		{"match-star", `builds.find({"name":{"$match":"cl*"}})`, row, true},
		{"match-question", `builds.find({"name":{"$match":"cl?"}})`, row, true},
		{"nmatch", `builds.find({"name":{"$nmatch":"web*"}})`, row, true},
		{"dep-scope-eq", `dependencies.find({"scope":"compile,test"})`, row, true},
		{"dep-sha1", `dependencies.find({"sha1":"dd"})`, row, true},
		{"module-name", `modules.find({"name":"com.example:api:1.0"})`, row, true},
		{"date-exact", `builds.find({"started":"2026-09-05T10:00:00.000Z"})`, row, true},
		{"date-partial-day-eq", `builds.find({"started":"2026-09-05"})`, row, true},
		{"date-partial-month-eq", `builds.find({"started":"2026-09"})`, row, true},
		{"date-partial-day-ne", `builds.find({"started":{"$ne":"2026-09-06"}})`, row, true},
		{"date-day-gt", `builds.find({"started":{"$gt":"2026-09-04"}})`, row, true},
		{"date-day-gte-edge", `builds.find({"started":{"$gte":"2026-09-05"}})`, row, true},
		{"date-day-lt-edge", `builds.find({"started":{"$lt":"2026-09-06"}})`, row, true},
		{"date-day-gt-same-day", `builds.find({"started":{"$gt":"2026-09-05"}})`, row, false},
		{"date-last", `builds.find({"started":{"$last":"3 days"}})`, row, true},
		{"date-last-miss", `builds.find({"started":{"$last":"1 days"}})`, row, false},
		{"date-before", `builds.find({"started":{"$before":"1 days"}})`, row, true},
		{"date-before-miss", `builds.find({"started":{"$before":"10 days"}})`, row, false},
		{"created-rfc3339", `builds.find({"created":{"$gte":"2026-09-05"}})`, row, true},
		{"and-or", `builds.find({"$or":[{"name":"web"},{"$and":[{"name":"cli"},{"number":"51"}]}]})`, row, true},
		{"and-miss", `builds.find({"name":"cli","number":"52"})`, row, false},
		{"unparseable-date-null-rule", `builds.find({"started":"2026-09-05"})`, badDateRow, false},
		{"unparseable-date-ne-null-rule", `builds.find({"started":{"$ne":"2026-09-05"}})`, badDateRow, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := Parse(tt.query)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			plan, err := PlanBuildQuery(ast, PlanOptions{Now: now})
			if err != nil {
				t.Fatalf("PlanBuildQuery: %v", err)
			}
			if plan.Where == nil {
				t.Fatal("Where = nil (match-all) for a constrained query")
			}
			if got := plan.Where(tt.row); got != tt.want {
				t.Fatalf("eval = %v, want %v", got, tt.want)
			}
		})
	}

	// Match-all: the empty criteria object.
	ast, err := Parse(`builds.find({})`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	plan, err := PlanBuildQuery(ast, PlanOptions{Now: now})
	if err != nil {
		t.Fatalf("PlanBuildQuery: %v", err)
	}
	if plan.Where != nil {
		t.Fatal("empty criteria lowered to a non-nil predicate, want match-all")
	}
}

// TestBuildPredicateNeedsClock: a relative date without a clock is the
// honest planner error, not a silent zero instant.
func TestBuildPredicateNeedsClock(t *testing.T) {
	ast, err := Parse(`builds.find({"started":{"$last":"3 days"}})`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, err := PlanBuildQuery(ast, PlanOptions{}); err == nil {
		t.Fatal("PlanBuildQuery without a clock succeeded, want error")
	}
}

// TestMatchesPattern pins the in-Go wildcard executor against the SQL
// translation's semantics: whole-value match, '*' any sequence, '?' one
// byte, everything else literal (including the SQL wildcards % and _).
func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		pattern, s string
		want       bool
	}{
		{"cli-*", "cli-core", true},
		{"cli-*", "web-core", false},
		{"cl?", "cli", true},
		{"cl?", "clii", false},
		{"*", "", true},
		{"*", "anything", true},
		{"", "", true},
		{"", "x", false},
		{"a%b", "a%b", true},
		{"a%b", "aXb", false},
		{"a_b", "a_b", true},
		{"a_b", "aXb", false},
		{"com.example:*", "com.example:api:1.0", true},
		{"日本*", "日本語", true},
		{"日本?", "日本語", false}, // '?' is one BYTE, the SQL '_' semantics
	}
	for _, tt := range tests {
		if got := MatchesPattern(tt.pattern, tt.s); got != tt.want {
			t.Errorf("MatchesPattern(%q, %q) = %v, want %v", tt.pattern, tt.s, got, tt.want)
		}
	}
}
