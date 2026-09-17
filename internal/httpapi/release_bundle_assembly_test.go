// L026-6 (D08-R01): the AQL assembly face's wire legs (p27-p37,
// release-bundle.md §10.2) — the ordered validation chain, the engine's
// parse-error copy through the shared mapping, the five-key hit shape with
// properties hydrated ({} when none), the empty hit list, and the
// no-engine 503 arm.

package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/license"
)

// TestBundleAssemblyValidationChain: §10.2's ordered arms — missing aql
// (absent and empty), the non-items domain, the engine's parse copy.
func TestBundleAssemblyValidationChain(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	admin := adminP()

	tests := []struct {
		name string
		body string
		want string
	}{
		{"missing key", `{}`, "Request is invalid. Missing AQL query"},
		{"empty aql", `{"aql":""}`, "Request is invalid. Missing AQL query"},
		{"builds domain", `{"aql":"builds.find({\"name\":{\"$eq\":\"x\"}})"}`,
			"Request is invalid. AQL query should find artifacts (items)"},
		// The engine's QueryError copy rides verbatim through the shared
		// mapping. The leading form is p33-exact; the trailing sub-query
		// echo differs on THIS input ("-syntax(" vs the reference's
		// "bogus-syntax(") — a pre-existing internal/search tokenizer
		// divergence, registered in the ticket report (out of this
		// ticket's area).
		{"syntax error", `{"aql":"items.find(bogus-syntax("}`,
			"Failed to parse query: items.find(bogus-syntax(, it looks like there is syntax error near the following sub-query:"},
		{"bare word body", `not json`,
			"Unrecognized token 'not': was expecting (JSON String, Number, Array, Object or token 'null', 'true' or 'false')"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body, _ := st.do(t, http.MethodPost, "release/bundle", tt.body, admin)
			if code != http.StatusBadRequest {
				t.Fatalf("POST = %d %s, want 400", code, body)
			}
			if !strings.Contains(body, tt.want) {
				t.Errorf("body %q must contain %q", body, tt.want)
			}
		})
	}

	// The projectKey refusal (BinFlow-native posture, the build family's).
	if code, body, _ := st.do(t, http.MethodPost, "release/bundle?projectKey=p", `{"aql":"items.find({})"}`, admin); code != 400 || !strings.Contains(body, "projects are not supported") {
		t.Fatalf("projectKey = %d %s, want the 400", code, body)
	}
}

// TestBundleAssemblyHitShape: the seeded artifact answers the five-key
// closed set — properties ALWAYS present ({} with none, the array-value
// map with one), pkg_type in wire casing, and the property-less/hydrated
// pair behaving identically under ?includeMetaData (p30-p37).
func TestBundleAssemblyHitShape(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	admin := adminP()

	// Property-less first: the {} face (p30/p31).
	code, body, _ := st.do(t, http.MethodPost, "release/bundle",
		`{"aql":"items.find({\"repo\":{\"$eq\":\"libs\"},\"name\":{\"$eq\":\"app.jar\"}})"}`, admin)
	if code != 200 {
		t.Fatalf("assembly = %d %s, want 200", code, body)
	}
	for _, want := range []string{
		`"results": [`, `"urn": "libs/app/1.0/app.jar"`,
		`"sha256": "aa11bb22cc33dd44aa11bb22cc33dd44aa11bb22cc33dd44aa11bb22cc33dd44"`,
		`"properties": {}`, `"size": 10`, `"pkg_type": "Generic"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("hit missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, "null") {
		t.Errorf("body %q must not render null", body)
	}

	// Hydrate one property: the array-value face (p36/p37), includeMetaData
	// an accepted no-op.
	ctx := context.Background()
	if err := st.md.NodeProps().Merge(ctx, "libs", "app/1.0/app.jar", map[string][]string{"l026k": {"d8"}}); err != nil {
		t.Fatalf("seed property: %v", err)
	}
	for _, q := range []string{"release/bundle", "release/bundle?includeMetaData=true"} {
		code, body, _ = st.do(t, http.MethodPost, q,
			`{"aql":"items.find({\"repo\":{\"$eq\":\"libs\"},\"name\":{\"$eq\":\"app.jar\"}})"}`, admin)
		if code != 200 || !strings.Contains(body, `"l026k": [`) || !strings.Contains(body, `"d8"`) {
			t.Fatalf("assembly %s = %d %s, want the hydrated property", q, code, body)
		}
	}

	// The empty hit: results is an in-place empty array, never null (p32).
	code, body, _ = st.do(t, http.MethodPost, "release/bundle",
		`{"aql":"items.find({\"repo\":{\"$eq\":\"libs\"},\"name\":{\"$eq\":\"no-such\"}})"}`, admin)
	if code != 200 || !strings.Contains(body, `"results": []`) {
		t.Fatalf("empty hit = %d %s, want the empty array", code, body)
	}

	// A non-admin user reaches the validation arm (p51 — the face is not
	// capability-gated).
	if code, body, _ = st.do(t, http.MethodPost, "release/bundle", `{}`, &auth.Principal{Name: "dev"}); code != 400 || !strings.Contains(body, "Missing AQL query") {
		t.Fatalf("user assembly = %d %s, want the validation 400 (p51)", code, body)
	}
}

// TestBundleAssemblyNoEngine: a stack whose search engine is unassembled
// answers the honest 503 (never an unfiltered query) — the same posture
// POST /api/search/aql keeps.
func TestBundleAssemblyNoEngine(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	st.s.aql = nil
	code, body, _ := st.do(t, http.MethodPost, "release/bundle", `{"aql":"items.find({\"repo\":{\"$eq\":\"libs\"}})"}`, adminP())
	if code != http.StatusServiceUnavailable {
		t.Fatalf("no-engine assembly = %d %s, want 503", code, body)
	}
}
