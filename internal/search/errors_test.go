package search

import (
	"strings"
	"testing"
)

// T-409 AC2: the honest-rejection error table. Syntax arms assert the
// verbatim E1 copy from the t407-evidence samples; domain/field/operator
// arms assert the C-layer enhanced copy sanctioned by aql.md §2.1 (code 400
// in T-415, message names the offender — never a pseudo-empty result).

func TestParseErrorSyntaxVerbatim(t *testing.T) {
	// t407-evidence v1c, byte for byte: out-of-order suffix chain, residual
	// segment anchored at the sort suffix.
	v1c := `items.find({"repo":"t228-maven"}).limit(1).sort({"$asc":["name"]})`
	qe := wantParseErr(t, v1c, ErrSyntax, "items", "",
		`Failed to parse query: items.find({"repo":"t228-maven"}).limit(1).sort({"$asc":["name"]}), it looks like there is syntax error near the following sub-query: sort({"$asc":["name"]})`)
	if want := `sort({"$asc":["name"]})`; qe.Segment != want {
		t.Errorf("segment = %q, want %q", qe.Segment, want)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		kind   ErrorKind
		domain string
		field  string
		msg    string
	}{
		// Unsupported domains (AC2 names build.find / statistics fields).
		// T-511 assertion inversion ⑥ (aql.md §15.3): builds/modules/
		// dependencies LEFT this table — the three entries parse green
		// (build_domain_test.go owns the positive halves); the promoted
		// row flips from "builds" to "build.promotions", the closed face
		// whose data plane is already loaded (the M18+ flip point).
		{"domain-build", `build.find({})`, ErrUnsupportedDomain, "build", "",
			"AQL domain not supported: build (BinFlow AQL supports: items, builds, modules, dependencies)"},
		{"domain-build-promotions-hint", `build.promotions.find({})`, ErrUnsupportedDomain, "build.promotions", "",
			"AQL domain not supported: build.promotions (BinFlow AQL supports: items, builds, modules, dependencies; promotion history is stored (build_promotions) but the query entry is not open yet (M18+ flip point))"},
		{"domain-properties-hint", `properties.find({})`, ErrUnsupportedDomain, "properties", "",
			`AQL domain not supported: properties (BinFlow AQL supports: items, builds, modules, dependencies; query properties through items.find with {"@key": value} criteria)`},
		{"domain-statistics", `statistics.find({})`, ErrUnsupportedDomain, "statistics", "",
			`AQL domain not supported: statistics (BinFlow AQL supports: items, builds, modules, dependencies; query statistics through items.find with {"stat.<field>": value} criteria)`},

		// Fields: unknown (evidence v15 names repossss), known-but-no-source.
		{"unknown-field", `items.find({"repossss":"x"})`, ErrUnknownField, "items", "repossss",
			"Unknown AQL field: repossss"},
		{"unknown-field-include", `items.find({}).include("repossss")`, ErrUnknownField, "items", "repossss",
			"Unknown AQL field: repossss"},
		// The statistics family is OPEN since T-440 (aql.md §14.1) — the
		// honest refusals left in it are the two internal ids, and the null
		// literal's operator gate.
		{"stat-internal-id", `items.find({"stat.id":{"$eq":1}})`, ErrUnsupportedField, "statistics", "stat.id",
			"AQL field not supported yet: stat.id (internal field, not exposed)"},
		{"stat-internal-remote-id-include", `items.find({}).include("stat.remote_id")`, ErrUnsupportedField, "statistics", "stat.remote_id",
			"AQL field not supported yet: stat.remote_id (internal field, not exposed)"},
		{"stat-null-order-op", `items.find({"stat.downloads":{"$gt":null}})`, ErrBadValue, "statistics", "stat.downloads",
			"AQL null values support $eq and $ne only on field stat.downloads"},
		{"modified-by", `items.find({"modified_by":"admin"})`, ErrUnsupportedField, "item", "modified_by",
			"AQL field not supported yet: modified_by (no storage source in BinFlow yet)"},
		{"original-sha1", `items.find({"original_sha1":"8ddc"})`, ErrUnsupportedField, "item", "original_sha1",
			"AQL field not supported yet: original_sha1 (BinFlow does not store separate original checksums)"},
		{"internal-id", `items.find({"id":{"$eq":1}})`, ErrUnsupportedField, "item", "id",
			"AQL field not supported yet: id (internal field, not exposed)"},
		{"virtual-repos-criteria", `items.find({"virtual_repos":"v"})`, ErrUnsupportedField, "item", "virtual_repos",
			"AQL field is output-only, not usable in criteria: virtual_repos"},

		// Operators (evidence v03 is the $not arm).
		{"op-not", `items.find({"repo":"r","$not":{"name":{"$match":"*.pom"}}})`, ErrBadOperator, "items", "",
			"AQL operator not supported: $not ($not is not part of the AQL language; use $ne or $nmatch)"},
		{"op-contains", `items.find({"name":{"$contains":"lib"}})`, ErrBadOperator, "item", "name",
			"AQL operator not supported: $contains (not an AQL operator; use $match with wildcards)"},
		{"op-eqic", `items.find({"name":{"$eqic":"lib"}})`, ErrBadOperator, "item", "name",
			"AQL operator not supported: $eqic (case-insensitive operator variants are not part of the BinFlow subset)"},
		{"op-unknown", `items.find({"name":{"$foo":"x"}})`, ErrBadOperator, "item", "name",
			"Unknown AQL operator: $foo"},
		{"op-match-on-int", `items.find({"size":{"$match":"10*"}})`, ErrBadOperator, "item", "size",
			"AQL operator not allowed: $match on field size ($match and $nmatch apply to string fields only)"},
		{"op-last-on-string", `items.find({"name":{"$last":"3 days"}})`, ErrBadOperator, "item", "name",
			"AQL operator not allowed: $last on field name ($last and $before apply to date fields only)"},
		{"op-match-on-type", `items.find({"type":{"$match":"f*"}})`, ErrBadOperator, "item", "type",
			"AQL operator not allowed: $match on field type (the type field supports $eq and $ne only)"},

		// Values.
		{"value-type-set", `items.find({"type":"fyle"})`, ErrBadValue, "item", "type",
			"Invalid value for field type: fyle (allowed: any, file, folder)"},
		{"value-null", `items.find({"name":null})`, ErrBadValue, "item", "name",
			"AQL null values are not supported for field name (only statistics-domain fields use null)"},
		{"value-bool", `items.find({"name":true})`, ErrBadValue, "item", "name",
			"Invalid value for field name: true (boolean is not an AQL value)"},
		{"value-number-on-string", `items.find({"repo":5})`, ErrBadValue, "item", "repo",
			"Invalid value for field repo: 5 (string required)"},
		{"value-non-int-string", `items.find({"size":"abc"})`, ErrBadValue, "item", "size",
			"Invalid number for field size: abc (integer required)"},
		{"value-fraction", `items.find({"size":1.5})`, ErrBadValue, "item", "size",
			"Invalid number for field size: 1.5 (integer required)"},
		{"value-date", `items.find({"created":"2026-13-01"})`, ErrBadValue, "item", "created",
			"Invalid date value for field created: 2026-13-01"},
		{"value-relative-date", `items.find({"created":{"$last":"3 fortnights"}})`, ErrBadValue, "item", "created",
			"Invalid relative date format for: 3 fortnights"},
		{"value-prop-number", `items.find({"@license":2})`, ErrBadValue, "items", "@license",
			"Invalid value for property @license: 2 (string required)"},

		// Suffix chain (§2.5).
		{"suffix-distinct", `items.find({}).distinct(true)`, ErrUnsupportedSuffix, "items", "",
			"AQL suffix not supported: .distinct (the .distinct() modifier is not part of the BinFlow subset)"},
		{"suffix-delete", `items.find({"repo":"r"}).delete()`, ErrUnsupportedSuffix, "items", "",
			"AQL suffix not supported: .delete (AQL write actions are not supported; BinFlow AQL is read-only)"},
		{"suffix-transitive", `items.find({}).include("*").transitive(true)`, ErrUnsupportedSuffix, "items", "",
			"AQL suffix not supported: .transitive (the .transitive modifier is not implemented (Smart Remote deep links))"},
		{"sort-not-result-field", `items.find({}).include("name").sort({"$asc":["path"]})`, ErrSortField, "item", "path",
			"Only the result fields are allowed to use in the sort section."},
		{"sort-not-in-default-set", `items.find({}).sort({"$asc":["sha256"]})`, ErrSortField, "item", "sha256",
			"Only the result fields are allowed to use in the sort section."},
		{"sort-duplicate", `items.find({}).sort({"$asc":["name","name"]})`, ErrSortField, "item", "name",
			"Duplicate fields, all the fields in the sort section should be unique."},
		{"sort-modified-by-no-source", `items.find({}).sort({"$asc":["modified_by"]})`, ErrUnsupportedField, "item", "modified_by",
			"AQL field not supported yet: modified_by (no storage source in BinFlow yet)"},
		{"offset-negative", `items.find({}).offset(-1)`, ErrBadValue, "items", "",
			"Invalid offset value: -1 (non-negative integer required)"},
		{"limit-not-number", `items.find({}).limit("abc")`, ErrBadValue, "items", "",
			"Invalid limit value: abc (non-negative integer required)"},

		// $msp scoping and degenerate arrays.
		{"msp-non-property", `items.find({"$msp":[{"@os":"linux"},{"name":"x"}]})`, ErrBadValue, "items", "name",
			"AQL $msp conditions may only contain property criteria, got: name"},
		{"msp-empty-array", `items.find({"$msp":[]})`, ErrSyntax, "items", "", ""},
		{"and-empty-array", `items.find({"$and":[]})`, ErrSyntax, "items", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertParseErr(t, tt.query, tt.kind, tt.domain, tt.field, tt.msg)
		})
	}
}

// SyntaxErrorArms pins the grammar-level rejections (E1 kind + segment
// anchoring; the full copy is byte-asserted in TestParseErrorSyntaxVerbatim).
// Lexer-stage failures carry no domain yet (domain "") — they fire before the
// query domain is even read.
func TestSyntaxErrorArms(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		domain  string
		segment string // query[pos:] — the E1 residual
	}{
		{"missing-criteria", `items.find()`, "items", `)`},
		{"unterminated-string", `items.find({"name":"x`, "", `"x`},
		{"trailing-garbage", `items.find({}) .foo(`, "items", `foo(`},
		{"double-comparator", `items.find({"size":{"$gt":1,"$lt":9}})`, "items", `,"$lt":9}})`},
		{"wrong-method", `items.find({}).orderBy("name")`, "items", `orderBy("name")`},
		{"repeat-limit", `items.find({}).limit(1).limit(2)`, "items", `limit(2)`},
		{"offset-before-sort", `items.find({}).offset(1).sort({"$asc":["name"]})`, "items", `sort({"$asc":["name"]})`},
		{"illegal-char", `items.find({});`, "", `;`},
		{"deep-path-bare", `items.find({artifact.module.build.@os:"linux"})`, "items", `.build.@os:"linux"})`},
		{"sort-empty-array", `items.find({}).sort({"$asc":[]})`, "items", `"$asc":[]})`},
		{"comparator-empty", `items.find({"name":{}})`, "items", `}})`},
		{"array-of-scalars", `items.find({"$or":["a"]})`, "items", `"a"]})`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qe := wantParseErr(t, tt.query, ErrSyntax, tt.domain, "", "")
			if qe.Segment != tt.segment {
				t.Errorf("segment = %q, want %q", qe.Segment, tt.segment)
			}
		})
	}
}

// assertParseErr is wantParseErr for callers that ignore the returned error
// object (table rows) — keeps errcheck honest.
func assertParseErr(t *testing.T, q string, kind ErrorKind, domain, field, msg string) {
	t.Helper()
	_ = wantParseErr(t, q, kind, domain, field, msg)
}

// Quoted deep paths (build-family cross-domain) are rejected as unknown
// fields naming what was written — the honest-refusal arm for the 远期 family.
func TestQuotedDeepPathUnknownField(t *testing.T) {
	assertParseErr(t, `items.find({"artifact.module.build.@os":"linux"})`,
		ErrUnknownField, "items", "artifact.module.build.@os",
		"Unknown AQL field: artifact.module.build.@os")
}

func TestParseQueryTooLong(t *testing.T) {
	q := `items.find({"name":"` + strings.Repeat("a", MaxQueryLen) + `"})`
	if len(q) <= MaxQueryLen {
		t.Fatalf("test query not over the cap: %d", len(q))
	}
	assertParseErr(t, q, ErrQueryTooLong, "", "",
		"AQL query is too long; please reduce the query length to less than 6000 chars")
}
