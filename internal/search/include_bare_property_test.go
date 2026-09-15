package search

// L024-3A: the bare domain-name include operand include("property") — the
// jf CLI's own spelling (aql.md §16.1-2/§16.6-2, live-verbatim through the
// reference 200). Parse and plan carry it as a full-catch-all property
// projection with the omit-when-empty flag the renderer honors (§16.1-4);
// inside the build-family entries the name stays unknown (the property
// entries are closed there, §15.3).

import (
	"errors"
	"testing"
)

func TestParseIncludeBareProperty(t *testing.T) {
	t.Run("items domain accepts the bare operand", func(t *testing.T) {
		q, err := Parse(`items.find({"repo":"libs"}).include("name","property")`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if len(q.Include) != 2 {
			t.Fatalf("include args = %d, want 2", len(q.Include))
		}
		inc := q.Include[1]
		if !inc.BareProperty || inc.PropKey != "*" || inc.Field.ID != "" {
			t.Fatalf("bare property include = %+v", inc)
		}
	})
	t.Run("plan projects it as the omit-when-empty catch-all", func(t *testing.T) {
		q, err := Parse(`items.find({"repo":"libs"}).include("property")`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		plan, err := PlanQuery(t.Context(), q, PlanOptions{})
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if len(plan.Output) != 1 || plan.Output[0].Kind != OutputProp {
			t.Fatalf("output = %+v", plan.Output)
		}
		of := plan.Output[0]
		if !of.PropBare || of.PropKey != "*" {
			t.Fatalf("bare output field = %+v", of)
		}
		if len(plan.Query.Props) != 1 || plan.Query.Props[0] != "*" {
			t.Fatalf("props fetch list = %v", plan.Query.Props)
		}
	})
	t.Run("the @key and long forms stay non-bare", func(t *testing.T) {
		q, err := Parse(`items.find({"repo":"libs"}).include("@license","property.key")`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		for i, want := range []bool{false, false} {
			if q.Include[i].BareProperty != want {
				t.Fatalf("include[%d].BareProperty = %v", i, q.Include[i].BareProperty)
			}
		}
	})
	t.Run("build-family entries keep the name unknown", func(t *testing.T) {
		_, err := Parse(`builds.find({"name":"ci"}).include("property")`)
		if err == nil {
			t.Fatalf("builds entry must refuse the bare property operand")
		}
		var qe *QueryError
		if !errors.As(err, &qe) || qe.Kind != ErrUnknownField {
			t.Fatalf("error = %v, want ErrUnknownField", err)
		}
	})
	t.Run("criteria position stays a field miss", func(t *testing.T) {
		_, err := Parse(`items.find({"property":"x"})`)
		if err == nil {
			t.Fatalf("criteria position must not accept the bare domain name")
		}
	})
}
