package search

import (
	"errors"
	"testing"
)

// T-409 AC coverage: table-driven parse tests over the item-domain field
// closure (aql.md §2.2), property forms (§2.3), operator closure (§2.4),
// $and/$or composition, the suffix chain (§2.5), and the honest-rejection
// error table (§2.1/§4). Verbatim copy anchors cite the t407-evidence file.

func mustParse(t *testing.T, q string) *Query {
	t.Helper()
	got, err := Parse(q)
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", q, err)
	}
	return got
}

// wantParseErr asserts the full structured error (kind, domain, field and
// verbatim message). An empty msg skips the message assertion (used by the
// syntax-arm table, whose full copy is byte-asserted separately).
func wantParseErr(t *testing.T, q string, kind ErrorKind, domain, field, msg string) *QueryError {
	t.Helper()
	_, err := Parse(q)
	if err == nil {
		t.Fatalf("Parse(%q) unexpectedly succeeded", q)
	}
	var qe *QueryError
	if !errors.As(err, &qe) {
		t.Fatalf("Parse(%q) error %v is not *QueryError", q, err)
	}
	if qe.Kind != kind {
		t.Errorf("kind = %q, want %q", qe.Kind, kind)
	}
	if qe.Domain != domain {
		t.Errorf("domain = %q, want %q", qe.Domain, domain)
	}
	if qe.Field != field {
		t.Errorf("field = %q, want %q", qe.Field, field)
	}
	if msg != "" && qe.Msg != msg {
		t.Errorf("msg:\n got %q\nwant %q", qe.Msg, msg)
	}
	if qe.Segment != "" && qe.Segment != q[qe.Pos:] {
		t.Errorf("segment = %q, want %q", qe.Segment, q[qe.Pos:])
	}
	return qe
}

func TestParseEmptyCriteria(t *testing.T) {
	q := mustParse(t, `items.find({})`)
	and, ok := q.Criteria.(*And)
	if !ok {
		t.Fatalf("criteria type %T, want *And", q.Criteria)
	}
	if len(and.Children) != 0 {
		t.Fatalf("children = %d, want 0", len(and.Children))
	}
	if q.Include != nil || q.Sort != nil || q.HasOffset || q.HasLimit {
		t.Errorf("empty query should carry no suffix state: %+v", q)
	}
}

// TestParseFieldClosure sweeps every registry field that has a storage
// source (aql.md §2.2 item-domain table + the property long forms).
func TestParseFieldClosure(t *testing.T) {
	tests := []struct {
		name  string
		query string
		field FieldID
		op    Operator
		str   string
		int   int64
	}{
		{"repo", `items.find({"repo":"maven"})`, FieldRepo, OpEq, "maven", 0},
		{"path", `items.find({"path":"com/acme"})`, FieldPath, OpEq, "com/acme", 0},
		{"name-match", `items.find({"name":{"$match":"lib-*"}})`, FieldName, OpMatch, "lib-*", 0},
		{"name-nmatch", `items.find({"name":{"$nmatch":"*.pom"}})`, FieldName, OpNmatch, "*.pom", 0},
		{"quoted-field-key", `items.find({"name":"x.jar"})`, FieldName, OpEq, "x.jar", 0},
		{"type-file", `items.find({"type":"file"})`, FieldType, OpEq, "file", 0},
		{"type-folder-ne", `items.find({"type":{"$ne":"folder"}})`, FieldType, OpNe, "folder", 0},
		{"type-any", `items.find({"type":"any"})`, FieldType, OpEq, "any", 0},
		{"created-gt", `items.find({"created":{"$gt":"2026-01-02"}})`, FieldCreated, OpGt, "2026-01-02", 0},
		{"modified-lt", `items.find({"modified":{"$lt":"2026"}})`, FieldModified, OpLt, "2026", 0},
		{"updated-gte", `items.find({"updated":{"$gte":"2026-01-02T03:04:05Z"}})`, FieldUpdated, OpGte, "2026-01-02T03:04:05Z", 0},
		{"created-by", `items.find({"created_by":"admin"})`, FieldCreatedBy, OpEq, "admin", 0},
		{"size-gt", `items.find({"size":{"$gt":1024}})`, FieldSize, OpGt, "", 1024},
		{"size-quoted-number", `items.find({"size":"4096"})`, FieldSize, OpEq, "", 4096},
		{"depth-lte", `items.find({"depth":{"$lte":3}})`, FieldDepth, OpLte, "", 3},
		{"sha256", `items.find({"sha256":"af19"})`, FieldSha256, OpEq, "af19", 0},
		{"actual-sha1", `items.find({"actual_sha1":"8ddc"})`, FieldActualSHA1, OpEq, "8ddc", 0},
		{"actual-md5", `items.find({"actual_md5":"a7bc"})`, FieldActualMD5, OpEq, "a7bc", 0},
		{"string-order-ops", `items.find({"name":{"$lt":"m"}})`, FieldName, OpLt, "m", 0},
		{"property-key", `items.find({"property.key":{"$eq":"license"}})`, FieldPropertyKey, OpEq, "license", 0},
		{"property-value", `items.find({"property.value":"Apache-2.0"})`, FieldPropertyValue, OpEq, "Apache-2.0", 0},
		{"virtual-repos-include", `items.find({}).include("virtual_repos")`, "", "", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := mustParse(t, tt.query)
			if tt.field == "" {
				return
			}
			inner := topCompare(t, q)
			if inner.Field.ID != tt.field {
				t.Errorf("field ID = %q, want %q", inner.Field.ID, tt.field)
			}
			if inner.Op != tt.op {
				t.Errorf("op = %q, want %q", inner.Op, tt.op)
			}
			if tt.str != "" && inner.Value.Str != tt.str {
				t.Errorf("value str = %q, want %q", inner.Value.Str, tt.str)
			}
			if tt.int != 0 && inner.Value.Int != tt.int {
				t.Errorf("value int = %d, want %d", inner.Value.Int, tt.int)
			}
		})
	}
}

// topCompare unwraps And{Compare} to the single comparator of a one-pair
// criteria object.
func topCompare(t *testing.T, q *Query) *Compare {
	t.Helper()
	and, ok := q.Criteria.(*And)
	if !ok {
		t.Fatalf("criteria type %T, want *And", q.Criteria)
	}
	if len(and.Children) != 1 {
		t.Fatalf("children = %d, want 1", len(and.Children))
	}
	c, ok := and.Children[0].(*Compare)
	if !ok {
		t.Fatalf("child type %T, want *Compare", and.Children[0])
	}
	return c
}

// TestParseOperatorClosure sweeps the comparator set (aql.md §2.4) in the
// quoted wire form, plus the bare $word liberal arm.
func TestParseOperatorClosure(t *testing.T) {
	for _, op := range []string{"$eq", "$ne", "$gt", "$gte", "$lt", "$lte"} {
		q := mustParse(t, `items.find({"size":{"`+op+`":7}})`)
		if got := topCompare(t, q).Op; string(got) != op {
			t.Errorf("op = %q, want %q", got, op)
		}
	}
	for _, op := range []string{"$eq", "$ne", "$gt", "$gte", "$lt", "$lte", "$match", "$nmatch"} {
		q := mustParse(t, `items.find({"name":{"`+op+`":"v"}})`)
		if got := topCompare(t, q).Op; string(got) != op {
			t.Errorf("op = %q, want %q", got, op)
		}
	}
	// Bare $word spelling (liberal arm).
	q := mustParse(t, `items.find({"size":{$gt:7}})`)
	if got := topCompare(t, q).Op; got != OpGt {
		t.Errorf("bare op = %q, want $gt", got)
	}
}

// TestParseLogicalComposition covers $and/$or at any depth with both value
// forms (object and array) and the comma implicit-$and.
func TestParseLogicalComposition(t *testing.T) {
	t.Run("implicit-and", func(t *testing.T) {
		q := mustParse(t, `items.find({"repo":"a","path":"b","name":"c"})`)
		and := q.Criteria.(*And)
		if len(and.Children) != 3 {
			t.Fatalf("children = %d, want 3", len(and.Children))
		}
	})
	t.Run("or-array-and-object", func(t *testing.T) {
		q := mustParse(t, `items.find({"$or":[{"$and":[{"repo":"a"},{"path":"p"}]},{"name":{"$match":"x*"}}]})`)
		or, ok := q.Criteria.(*And).Children[0].(*Or)
		if !ok {
			t.Fatalf("child type %T, want *Or", q.Criteria.(*And).Children[0])
		}
		if len(or.Children) != 2 {
			t.Fatalf("or children = %d, want 2", len(or.Children))
		}
		// Array elements are the criteria objects themselves (And wrappers
		// around their pairs) — the AST keeps the written shape.
		inner, ok := or.Children[0].(*And)
		if !ok || len(inner.Children) != 1 {
			t.Fatalf("array element shape wrong: %#v", or.Children[0])
		}
		nested, ok := inner.Children[0].(*And)
		if !ok || len(nested.Children) != 2 {
			t.Errorf("nested $and shape wrong: %#v", inner.Children[0])
		}
	})
	t.Run("and-single-object", func(t *testing.T) {
		q := mustParse(t, `items.find({"$and":{"repo":"a"}})`)
		and := q.Criteria.(*And)
		if len(and.Children) != 1 {
			t.Fatalf("children = %d, want 1", len(and.Children))
		}
		if _, ok := and.Children[0].(*And); !ok {
			t.Fatalf("child type %T, want *And", and.Children[0])
		}
	})
	t.Run("mixed-with-property", func(t *testing.T) {
		q := mustParse(t, `items.find({"$or":[{"@license":"Apache-2.0"},{"repo":"rel"}]})`)
		or := q.Criteria.(*And).Children[0].(*Or)
		elem, ok := or.Children[0].(*And)
		if !ok || len(elem.Children) != 1 {
			t.Fatalf("array element shape wrong: %#v", or.Children[0])
		}
		if _, ok := elem.Children[0].(*PropMatch); !ok {
			t.Fatalf("child type %T, want *PropMatch", elem.Children[0])
		}
	})
}

// TestParsePropertyForms covers the @key shorthand family (aql.md §2.3).
func TestParsePropertyForms(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		key      string
		wildcard bool
		op       Operator
		str      string
	}{
		{"short", `items.find({"@license":"Apache-2.0"})`, "license", false, OpEq, "Apache-2.0"},
		{"comparator", `items.find({"@version":{"$gt":"2"}})`, "version", false, OpGt, "2"},
		{"catch-all-key", `items.find({"@*":"v1"})`, "*", true, OpEq, "v1"},
		{"key-existence", `items.find({"@license":"*"})`, "license", false, OpEq, "*"},
		{"dotted-key", `items.find({"@build.name":"ci"})`, "build.name", false, OpEq, "ci"},
		// Liberal arm: the bare @key spelling (no quotes) — the registered
		// form is the quoted one above (aql.md §2.3), live traffic quotes it.
		{"bare-at-key", `items.find({@license:"Apache-2.0"})`, "license", false, OpEq, "Apache-2.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm, ok := mustParse(t, tt.query).Criteria.(*And).Children[0].(*PropMatch)
			if !ok {
				t.Fatalf("criteria child is %T, want *PropMatch", mustParse(t, tt.query).Criteria.(*And).Children[0])
			}
			if pm.Key != tt.key || pm.WildcardKey != tt.wildcard || pm.Op != tt.op || pm.Value.Str != tt.str {
				t.Errorf("got %+v, want key=%s wildcard=%v op=%s str=%s", pm, tt.key, tt.wildcard, tt.op, tt.str)
			}
		})
	}
}

// TestParseMSP covers $msp (same-property-instance semantics, aql.md §2.3).
func TestParseMSP(t *testing.T) {
	q := mustParse(t, `items.find({"$and":[{"repo":"r"},{"$msp":[{"@os":"linux"},{"@version":{"$gt":"2"}}]}]}).limit(5)`)
	and, ok := q.Criteria.(*And).Children[0].(*And)
	if !ok {
		t.Fatalf("criteria child type %T, want *And ($and array)", q.Criteria.(*And).Children[0])
	}
	if len(and.Children) != 2 {
		t.Fatalf("children = %d, want 2", len(and.Children))
	}
	msp, ok := and.Children[1].(*And).Children[0].(*MSP)
	if !ok {
		t.Fatalf("child type %T, want *MSP", and.Children[1])
	}
	if len(msp.Conditions) != 2 {
		t.Fatalf("msp conditions = %d, want 2", len(msp.Conditions))
	}
	if !q.HasLimit || q.Limit != 5 {
		t.Errorf("limit = %d/%v, want 5/true", q.Limit, q.HasLimit)
	}
}

// TestParseSuffixChain covers include/sort/offset/limit in order (§2.5).
func TestParseSuffixChain(t *testing.T) {
	t.Run("full-chain", func(t *testing.T) {
		q := mustParse(t, `items.find({}).include("name","repo","created").sort({"$desc":["name","created"]}).offset("10").limit(5)`)
		if len(q.Include) != 3 || q.Include[0].Raw != "name" || q.Include[1].Raw != "repo" || q.Include[2].Raw != "created" {
			t.Errorf("include = %+v", q.Include)
		}
		if len(q.Sort) != 2 || q.Sort[0].Asc || q.Sort[0].Field.ID != FieldName || q.Sort[1].Field.ID != FieldCreated {
			t.Errorf("sort = %+v", q.Sort)
		}
		if !q.HasOffset || q.Offset != 10 {
			t.Errorf("offset = %d/%v", q.Offset, q.HasOffset)
		}
		if !q.HasLimit || q.Limit != 5 {
			t.Errorf("limit = %d/%v", q.Limit, q.HasLimit)
		}
	})
	t.Run("asc", func(t *testing.T) {
		q := mustParse(t, `items.find({}).sort({"$asc":["name"]})`)
		if len(q.Sort) != 1 || !q.Sort[0].Asc || q.Sort[0].Field.ID != FieldName {
			t.Errorf("sort = %+v", q.Sort)
		}
	})
	t.Run("sort-default-output-fields", func(t *testing.T) {
		// Without include, the default output set (aql.md §3.3) is sortable —
		// except modified_by, which BinFlow has no storage source for (its
		// rejection is pinned in the error table).
		mustParse(t, `items.find({}).sort({"$asc":["repo","path","name","type","size","created","created_by","modified","updated"]})`)
	})
	t.Run("sort-included-field", func(t *testing.T) {
		q := mustParse(t, `items.find({}).include("sha256").sort({"$asc":["sha256"]})`)
		if q.Sort[0].Field.ID != FieldSha256 {
			t.Errorf("sort field = %q", q.Sort[0].Field.ID)
		}
	})
	t.Run("include-star", func(t *testing.T) {
		q := mustParse(t, `items.find({}).include("*")`)
		if len(q.Include) != 1 || !q.Include[0].Star {
			t.Errorf("include = %+v", q.Include)
		}
	})
	t.Run("include-at-key", func(t *testing.T) {
		q := mustParse(t, `items.find({}).include("@license")`)
		if len(q.Include) != 1 || q.Include[0].PropKey != "license" {
			t.Errorf("include = %+v", q.Include)
		}
	})
	t.Run("include-empty", func(t *testing.T) {
		// The ADR EBNF allows .include() with no fields.
		q := mustParse(t, `items.find({}).include()`)
		if len(q.Include) != 0 {
			t.Errorf("include = %+v", q.Include)
		}
	})
	t.Run("zero-window", func(t *testing.T) {
		q := mustParse(t, `items.find({}).offset(0).limit(0)`)
		if !q.HasOffset || q.Offset != 0 || !q.HasLimit || q.Limit != 0 {
			t.Errorf("window = %+v", q)
		}
	})
	t.Run("bare-include-field", func(t *testing.T) {
		// Liberal arm: bare identifier where a quoted field name is canonical.
		q := mustParse(t, `items.find({}).include(name)`)
		if len(q.Include) != 1 || q.Include[0].Field.ID != FieldName {
			t.Errorf("include = %+v", q.Include)
		}
	})
}

// TestParseRelativeDates sweeps the $last/$before period grammar (§2.4).
func TestParseRelativeDates(t *testing.T) {
	tests := []struct {
		raw   string
		count int64
		unit  TimeUnit
	}{
		{"3 days", 3, UnitDays},
		{"2 weeks", 2, UnitWeeks},
		{"10 minutes", 10, UnitMinutes},
		{"1 mo", 1, UnitMonths},
		{"6 months", 6, UnitMonths},
		{"1 y", 1, UnitYears},
		{"500 ms", 500, UnitMillis},
		{"7 millis", 7, UnitMillis},
		{"90 s", 90, UnitSeconds},
		{"1 day", 1, UnitDays}, // singular long form (liberal arm)
	}
	for _, tt := range tests {
		for _, op := range []string{"$last", "$before"} {
			q := mustParse(t, `items.find({"created":{`+op+`:"`+tt.raw+`"}})`)
			c := topCompare(t, q)
			if c.Value.Period == nil {
				t.Fatalf("%s: period not parsed for %q", op, tt.raw)
			}
			if c.Value.Period.Count != tt.count || c.Value.Period.Unit != tt.unit {
				t.Errorf("%s %q = {%d %s}, want {%d %s}", op, tt.raw,
					c.Value.Period.Count, c.Value.Period.Unit, tt.count, tt.unit)
			}
		}
	}
}

// TestParseDateLiterals pins the ISO8601 partial-precision acceptance.
func TestParseDateLiterals(t *testing.T) {
	for _, s := range []string{
		"2026", "2026-08", "2026-08-23", "2026-08-23T08:19:04.618Z",
		"2026-08-23T08:19:04Z", "2026-08-23T08:19:04+02:00", "2026-08-23T08Z",
	} {
		mustParse(t, `items.find({"created":{"$gt":"`+s+`"}})`)
	}
}

// TestParseStringEscapes covers lexer string decoding through Parse.
func TestParseStringEscapes(t *testing.T) {
	tests := []struct {
		query string
		want  string
	}{
		{`items.find({"name":"a\"b"})`, `a"b`},
		{`items.find({"name":"a\\b"})`, `a\b`},
		{`items.find({"name":"a\n\tb"})`, "a\n\tb"},
		{`items.find({"name":"日本語"})`, "日本語"},
		{`items.find({"name":"日"})`, "日"},
		{`items.find({"name":"😀"})`, "\U0001F600"},
	}
	for _, tt := range tests {
		c := topCompare(t, mustParse(t, tt.query))
		if c.Value.Str != tt.want {
			t.Errorf("%s: str = %q, want %q", tt.query, c.Value.Str, tt.want)
		}
	}
}
