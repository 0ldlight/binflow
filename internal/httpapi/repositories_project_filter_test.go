package httpapi_test

// The ?project= filter arm of GET /api/repositories (L013, R-15c/d —
// matrix D02-R01, evidence reports/compatibility/L013-r15-packument-probes.md
// §1 five arms): the live reference FILTERS for an unknown project (empty
// set, never the full list) even with the Projects addon inactive, the
// ?type=<invalid> family posture on the project axis; the empty string is
// no-parameter semantics on both sides (c2). BinFlow has no projects domain,
// so every non-empty value truthfully matches nothing (pending-rulings §2
// R-15c 案乙). This file pins the five probe arms plus the whitespace form
// of "empty" the family's TrimSpace posture covers.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestRepositoriesProjectFilter(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	if s, b := putRepoStatus(t, h, "maven-local", `{"rclass":"local","packageType":"maven"}`); s != http.StatusOK {
		t.Fatalf("seed maven-local: %d %s", s, b)
	}

	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"c0 baseline", "", "[generic-local maven-local]"},
		{"c1 unknown project filters to the empty set", "?project=l0134-nonexistent", "[]"},
		{"c2 empty string is no-parameter (ignored)", "?project=", "[generic-local maven-local]"},
		{"c3 project dominates the combo arm", "?project=l0134-nonexistent&type=local", "[]"},
		{"c4 invalid type family control", "?type=l0134-nosuchtype", "[]"},
		{"whitespace-only project is no-parameter", "?project=%20%20", "[generic-local maven-local]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.do(http.MethodGet, "/binflow/api/repositories"+tt.query, adminUser, adminPass, nil, nil)
			body := mustGet(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("list%q status = %d; body=%s", tt.query, resp.StatusCode, body)
			}
			if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store (the full path's wire header)", cc)
			}
			var items []struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal([]byte(body), &items); err != nil {
				t.Fatalf("list body %q: %v", body, err)
			}
			keys := make([]string, len(items))
			for i, it := range items {
				keys[i] = it.Key
			}
			if got := fmt.Sprint(keys); got != tt.want {
				t.Fatalf("keys = %s, want %s", got, tt.want)
			}
		})
	}

	// The c1 wire body is the literal [] of the c4 family form — an empty
	// non-nil slice through the shared renderer — never null.
	t.Run("c1 body is the literal empty array", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/repositories?project=any", adminUser, adminPass, nil, nil)
		if body := mustGet(t, resp); strings.TrimSpace(body) != "[]" {
			t.Fatalf("body = %q, want []", body)
		}
	})
}
