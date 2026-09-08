package httpapi_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/search"
)

// The AQL REST entrance over the real stack (M15 T-415, FR-133.3/.5):
// POST /binflow/api/search/aql. Every leg drives the assembled server the
// production shape wires (httpapi.New builds the engine from Deps), so the
// assertions cover the full chain — parse, gate, ACL weave, execution,
// projection and envelope — not a handler slice. Wire-format anchors are the
// t407 evidence bodies (docs/reverse/aql.md sections 1/3/4/7).

// aqlDo posts one AQL query and returns status plus body.
func aqlDo(t *testing.T, h *harness, query, user, pass, extraQuery string) (int, string) {
	t.Helper()
	path := "/binflow/api/search/aql" + extraQuery
	resp := h.do(http.MethodPost, path, user, pass, []byte(query), nil)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, mustGet(t, resp)
}

// aqlPost posts one AQL query and returns the raw response (header legs).
func aqlPost(t *testing.T, h *harness, query, user, pass string) *http.Response {
	t.Helper()
	resp := h.do(http.MethodPost, "/binflow/api/search/aql", user, pass, []byte(query), nil)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// aqlNames extracts the "name" values of every row, in order — the cheap
// projection most tables assert on.
func aqlNames(t *testing.T, body string) []string {
	t.Helper()
	var parsed struct {
		Results []map[string]any `json:"results"`
		Range   struct {
			StartPos     int64 `json:"start_pos"`
			EndPos       int64 `json:"end_pos"`
			Total        int64 `json:"total"`
			Limit        *int64
			Notification string
		} `json:"range"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("aql body is not JSON: %v\nbody: %q", err, body)
	}
	names := make([]string, 0, len(parsed.Results))
	for _, row := range parsed.Results {
		v, _ := row["name"].(string)
		names = append(names, v)
	}
	return names
}

// aqlRange decodes the range tail.
func aqlRange(t *testing.T, body string) (startPos, endPos, total int64, limit *int64, notification string) {
	t.Helper()
	var parsed struct {
		Range struct {
			StartPos     int64 `json:"start_pos"`
			EndPos       int64 `json:"end_pos"`
			Total        int64 `json:"total"`
			Limit        *int64
			Notification string
		} `json:"range"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("aql body is not JSON: %v", err)
	}
	return parsed.Range.StartPos, parsed.Range.EndPos, parsed.Range.Total,
		parsed.Range.Limit, parsed.Range.Notification
}

// seedAQLStack lands the wire-format playground: two artifacts under acme/
// in generic-local.
func seedAQLStack(t *testing.T, h *harness) {
	t.Helper()
	seedRepo(t, h, "generic-local")
	deposit(t, h, "generic-local", "acme/artifact.bin", "artifact-bytes") // 14 bytes
	deposit(t, h, "generic-local", "acme/lib.jar", "jar-bytes")           // 9 bytes
}

// TestSearchAQLWireEnvelope pins the streaming envelope byte-for-byte
// (aql.md section 3.1, t407 evidence v01/v07/v6): the leading newline, the
// " : " field spacing, the "},{" row separator, the two-space empty form,
// the range tail and the millis date echo — plus the default projection's
// modified_by absence (no storage source, never faked; T-411's leave-behind
// for this ticket).
func TestSearchAQLWireEnvelope(t *testing.T) {
	h := newHarness(t)
	seedAQLStack(t, h)

	t.Run("pretty rows, include projection, no stated limit", func(t *testing.T) {
		status, body := aqlDo(t, h,
			`items.find({"repo":"generic-local"}).include("repo","path","name","type","size")`,
			adminUser, adminPass, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200\nbody: %s", status, body)
		}
		want := "\n{\n" +
			"\"results\" : [ {\n" +
			"  \"repo\" : \"generic-local\",\n" +
			"  \"path\" : \"acme\",\n" +
			"  \"name\" : \"artifact.bin\",\n" +
			"  \"type\" : \"file\",\n" +
			"  \"size\" : 14\n" +
			"},{\n" +
			"  \"repo\" : \"generic-local\",\n" +
			"  \"path\" : \"acme\",\n" +
			"  \"name\" : \"lib.jar\",\n" +
			"  \"type\" : \"file\",\n" +
			"  \"size\" : 9\n" +
			"} ],\n" +
			"\"range\" : {\n" +
			"  \"start_pos\" : 0,\n" +
			"  \"end_pos\" : 2,\n" +
			"  \"total\" : 2\n" +
			"}\n" +
			"}\n"
		if body != want {
			t.Fatalf("body =\n%q\nwant=\n%q", body, want)
		}
	})

	t.Run("default projection: the v10 field set minus modified_by", func(t *testing.T) {
		status, body := aqlDo(t, h, `items.find({"repo":"generic-local"})`, adminUser, adminPass, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		for _, field := range []string{"repo", "path", "name", "type", "size", "created", "created_by", "modified", "updated"} {
			if !strings.Contains(body, "\""+field+"\" : ") {
				t.Fatalf("default output missing field %q\nbody: %s", field, body)
			}
		}
		if strings.Contains(body, "modified_by") {
			t.Fatalf("default output carries modified_by — no storage source, the key must be absent\nbody: %s", body)
		}
		if !strings.Contains(body, `"created_by" : "admin"`) {
			t.Fatalf("admin caller must see the real identity\nbody: %s", body)
		}
		// ISO8601 with milliseconds, UTC (aql.md section 3.1).
		if !strings.Contains(body, `"created" : "20`) || !strings.HasSuffix(strings.SplitN(strings.Split(body, `"created" : "`)[1], `"`, 2)[0], "Z") {
			t.Fatalf("created echo is not the millis UTC form\nbody: %s", body)
		}
	})

	t.Run("empty set: the two-space form, byte-exact", func(t *testing.T) {
		_, body := aqlDo(t, h, `items.find({"repo":"no-such-repo"})`, adminUser, adminPass, "")
		want := "\n{\n\"results\" : [  ],\n\"range\" : {\n  \"start_pos\" : 0,\n  \"end_pos\" : 0,\n  \"total\" : 0\n}\n}\n"
		if body != want {
			t.Fatalf("empty body = %q, want %q", body, want)
		}
	})
}

// TestSearchAQLCompactDoubleForm is the ?compact contrast leg (aql.md
// section 1/3.1): the outer wrapper keeps its shape, rows and range compress
// to single lines.
func TestSearchAQLCompactDoubleForm(t *testing.T) {
	h := newHarness(t)
	seedAQLStack(t, h)
	const q = `items.find({"repo":"generic-local"}).include("repo","path","name")`

	prettyWant := "\n{\n\"results\" : [ {\n" +
		"  \"repo\" : \"generic-local\",\n  \"path\" : \"acme\",\n  \"name\" : \"artifact.bin\"\n},{\n" +
		"  \"repo\" : \"generic-local\",\n  \"path\" : \"acme\",\n  \"name\" : \"lib.jar\"\n" +
		"} ],\n\"range\" : {\n  \"start_pos\" : 0,\n  \"end_pos\" : 2,\n  \"total\" : 2\n}\n}\n"
	compactWant := "\n{\n\"results\" : [ " +
		"{\"repo\":\"generic-local\",\"path\":\"acme\",\"name\":\"artifact.bin\"}," +
		"{\"repo\":\"generic-local\",\"path\":\"acme\",\"name\":\"lib.jar\"}" +
		" ],\n\"range\" : {\"start_pos\":0,\"end_pos\":2,\"total\":2}\n}\n"

	for _, tc := range []struct {
		query string
		want  string
	}{
		{"", prettyWant},
		{"?compact=true", compactWant},
		{"?compact=false", prettyWant},
		{"?compact", prettyWant}, // bare flag: not the documented spelling, pretty stays
		{"?compact=bogus", prettyWant},
	} {
		t.Run("extra="+tc.query, func(t *testing.T) {
			status, body := aqlDo(t, h, q, adminUser, adminPass, tc.query)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200", status)
			}
			if body != tc.want {
				t.Fatalf("body =\n%q\nwant=\n%q", body, tc.want)
			}
		})
	}
}

// TestSearchAQLQueryOperators is the operator-composite leg (AC1): boolean
// nesting, the comparator family, property nesting over the M10 property
// fixtures, and relative time.
func TestSearchAQLQueryOperators(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "ops-local")
	deposit(t, h, "ops-local", "team/a.bin", "aaaa") // 4
	deposit(t, h, "ops-local", "team/b.bin", "bb")   // 2
	deposit(t, h, "ops-local", "solo/c.bin", "ccc")  // 3
	if err := h.md.NodeProps().Merge(context.Background(), "ops-local", "team/a.bin", map[string][]string{
		"team": {"platform"}, "severity": {"high"},
	}); err != nil {
		t.Fatalf("seed props: %v", err)
	}
	if err := h.md.NodeProps().Merge(context.Background(), "ops-local", "solo/c.bin", map[string][]string{
		"team": {"infra"},
	}); err != nil {
		t.Fatalf("seed props: %v", err)
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"$eq", `items.find({"repo":"ops-local","name":{"$eq":"a.bin"}}).include("name")`, []string{"a.bin"}},
		// No sort stated: the (repo, path) tiebreaker orders the rows, so
		// solo/c.bin precedes team/b.bin.
		{"$or/$match composite", `items.find({"$or":[{"name":{"$eq":"b.bin"}},{"name":{"$match":"c*"}}]}).include("repo","name")`, []string{"c.bin", "b.bin"}},
		{"nested $and inside $or", `items.find({"$or":[{"$and":[{"name":"a.bin"},{"path":"team"}]},{"name":"b.bin"}]}).include("name")`, []string{"a.bin", "b.bin"}},
		{"$ne", `items.find({"repo":"ops-local","name":{"$ne":"b.bin"}}).include("name").sort({"$asc":["name"]})`, []string{"a.bin", "c.bin"}},
		{"$gt on size", `items.find({"repo":"ops-local","size":{"$gt":2}}).include("name","size").sort({"$desc":["size"]})`, []string{"a.bin", "c.bin"}},
		{"$nmatch wildcard", `items.find({"repo":"ops-local","name":{"$nmatch":"*.bin"}}).include("name")`, nil},
		{"property nested @key", `items.find({"repo":"ops-local","@team":"platform"}).include("name")`, []string{"a.bin"}},
		{"property catch-all value", `items.find({"repo":"ops-local","@*":"infra"}).include("name")`, []string{"c.bin"}},
		{"property key existence", `items.find({"repo":"ops-local","@severity":"*"}).include("name")`, []string{"a.bin"}},
		{"property.key long form", `items.find({"repo":"ops-local","property.key":"team","property.value":"infra"}).include("name")`, []string{"c.bin"}},
		{"relative $last window", `items.find({"repo":"ops-local","created":{"$last":"1000 d"}}).include("name").sort({"$asc":["name"]})`, []string{"a.bin", "b.bin", "c.bin"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := aqlDo(t, h, tt.query, adminUser, adminPass, "")
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200\nbody: %s", status, body)
			}
			got := aqlNames(t, body)
			if len(got) != len(tt.want) {
				t.Fatalf("names = %v, want %v\nbody: %s", got, tt.want, body)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("names = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// TestSearchAQLWindowSuffixes is the include/sort/offset/limit leg (AC1):
// every suffix echoed field by field — sort order, start_pos echo, limit
// echo, the no-limit omission and the v07 page shape.
func TestSearchAQLWindowSuffixes(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "win-local")
	for _, n := range []string{"a.bin", "b.bin", "c.bin", "d.bin"} {
		deposit(t, h, "win-local", n, "x-"+n)
	}

	t.Run("sort asc", func(t *testing.T) {
		_, body := aqlDo(t, h, `items.find({"repo":"win-local"}).include("name").sort({"$asc":["name"]})`, adminUser, adminPass, "")
		if got := aqlNames(t, body); strings.Join(got, ",") != "a.bin,b.bin,c.bin,d.bin" {
			t.Fatalf("asc order = %v", got)
		}
	})

	t.Run("sort desc", func(t *testing.T) {
		_, body := aqlDo(t, h, `items.find({"repo":"win-local"}).include("name").sort({"$desc":["name"]})`, adminUser, adminPass, "")
		if got := aqlNames(t, body); strings.Join(got, ",") != "d.bin,c.bin,b.bin,a.bin" {
			t.Fatalf("desc order = %v", got)
		}
	})

	t.Run("offset echoes in start_pos", func(t *testing.T) {
		_, body := aqlDo(t, h, `items.find({"repo":"win-local"}).include("name").sort({"$asc":["name"]}).offset(1).limit(2)`, adminUser, adminPass, "")
		if got := aqlNames(t, body); strings.Join(got, ",") != "b.bin,c.bin" {
			t.Fatalf("page = %v", got)
		}
		start, end, total, limit, _ := aqlRange(t, body)
		if start != 1 || end != 2 || total != 2 {
			t.Fatalf("range = %d/%d/%d, want 1/2/2 (the v07 page shape)", start, end, total)
		}
		if limit == nil || *limit != 2 {
			t.Fatalf("range.limit = %v, want the echo 2", limit)
		}
	})

	t.Run("limit without offset", func(t *testing.T) {
		_, body := aqlDo(t, h, `items.find({"repo":"win-local"}).include("name").sort({"$asc":["name"]}).limit(1)`, adminUser, adminPass, "")
		if got := aqlNames(t, body); strings.Join(got, ",") != "a.bin" {
			t.Fatalf("page = %v", got)
		}
		start, end, total, limit, _ := aqlRange(t, body)
		if start != 0 || end != 1 || total != 1 || limit == nil || *limit != 1 {
			t.Fatalf("range = %d/%d/%d/%v, want 0/1/1/1", start, end, total, limit)
		}
	})

	t.Run("no stated limit omits the key", func(t *testing.T) {
		_, body := aqlDo(t, h, `items.find({"repo":"win-local"}).include("name")`, adminUser, adminPass, "")
		if strings.Contains(body, "limit") {
			t.Fatalf("range carries limit though the query stated none\nbody: %s", body)
		}
	})

	t.Run("property projection renders the nested properties member", func(t *testing.T) {
		if err := h.md.NodeProps().Merge(context.Background(), "win-local", "a.bin", map[string][]string{
			"team": {"platform"}, "severity": {"high"},
		}); err != nil {
			t.Fatalf("seed props: %v", err)
		}
		_, body := aqlDo(t, h, `items.find({"repo":"win-local","name":"a.bin"}).include("name","@team","@severity")`, adminUser, adminPass, "")
		if !strings.Contains(body, "\"properties\" : [ {\n    \"key\" : \"severity\",\n    \"value\" : \"high\"\n  },{\n    \"key\" : \"team\",\n    \"value\" : \"platform\"\n  } ]") {
			t.Fatalf("properties member wrong\nbody: %s", body)
		}
		// The propless row keeps the two-space empty form.
		_, body = aqlDo(t, h, `items.find({"repo":"win-local","name":"b.bin"}).include("name","@team")`, adminUser, adminPass, "")
		if !strings.Contains(body, "\"properties\" : [ ]") {
			t.Fatalf("propless properties member wrong\nbody: %s", body)
		}
	})
}

// TestSearchAQLRejections is the 400 family (AC1): the E1 syntax copy
// verbatim from the t407 sample, the honest named rejections for unknown
// fields / unsupported domains / statistics fields / modified_by / $not, the
// 6,000-char gate and the empty-body E2 verdict. Domain-not-supported must
// be a 400 that names the domain — never a pseudo-empty 200.
func TestSearchAQLRejections(t *testing.T) {
	h := newHarness(t)
	seedAQLStack(t, h)

	const v1c = `items.find({"repo":"generic-local"}).limit(1).sort({"$asc":["name"]})`
	const v1cCopy = `Failed to parse query: items.find({"repo":"generic-local"}).limit(1).sort({"$asc":["name"]}), it looks like there is syntax error near the following sub-query: sort({"$asc":["name"]})`

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantMsg    string // exact when set; contains otherwise
		contains   string
	}{
		{"syntax error: E1 verbatim (v1c)", v1c, http.StatusBadRequest, v1cCopy, ""},
		{"truncated syntax: E1 shape", `items.find({"repo":`, http.StatusBadRequest, "", "Failed to parse query:"},
		// T-511 assertion inversion ⑥: builds/modules/dependencies query
		// green now — the "names itself" example moved to a domain that
		// still refuses (the M18+ flip-point face).
		{"unsupported domain names itself", `releases.find({})`, http.StatusBadRequest, "", "releases"},
		{"unsupported domain via dotted entry", `build.promotions.find({})`, http.StatusBadRequest, "", "build"},
		{"statistics internal id named", `items.find({"stat.id":{"$eq":1}})`, http.StatusBadRequest, "", "stat.id"},
		{"unknown field named", `items.find({"repossss":"x"})`, http.StatusBadRequest, "", "repossss"},
		{"modified_by honest refusal", `items.find({"modified_by":"admin"})`, http.StatusBadRequest, "", "modified_by"},
		{"$not refused by name", `items.find({"$not":{"repo":"x"}})`, http.StatusBadRequest, "", "$not"},
		{".delete() refused", `items.find({"repo":"generic-local"}).delete()`, http.StatusBadRequest, "", "read-only"},
		{"query over 6,000 chars: E4 verbatim", `items.find({"repo":"` + strings.Repeat("a", 6000) + `"})`, http.StatusBadRequest,
			"AQL query is too long; please reduce the query length to less than 6000 chars", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := aqlDo(t, h, tt.query, adminUser, adminPass, "")
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d\nbody: %s", status, tt.wantStatus, body)
			}
			var env struct {
				Errors []struct {
					Status  int    `json:"status"`
					Message string `json:"message"`
				} `json:"errors"`
			}
			if err := json.Unmarshal([]byte(body), &env); err != nil {
				t.Fatalf("error body is not the errors[] envelope: %v\nbody: %s", err, body)
			}
			if len(env.Errors) != 1 || env.Errors[0].Status != tt.wantStatus {
				t.Fatalf("envelope = %+v", env.Errors)
			}
			msg := env.Errors[0].Message
			if tt.wantMsg != "" && msg != tt.wantMsg {
				t.Fatalf("message = %q\nwant    %q", msg, tt.wantMsg)
			}
			if tt.contains != "" && !strings.Contains(msg, tt.contains) {
				t.Fatalf("message %q does not name %q", msg, tt.contains)
			}
		})
	}

	t.Run("empty body and no query param: E2 Bad Request", func(t *testing.T) {
		status, body := aqlDo(t, h, "", adminUser, adminPass, "")
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", status)
		}
		var env struct {
			Errors []struct {
				Status  int    `json:"status"`
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal([]byte(body), &env); err != nil {
			t.Fatalf("body not JSON: %v", err)
		}
		if env.Errors[0].Message != "Bad Request" {
			t.Fatalf("message = %q, want the E2 generic copy", env.Errors[0].Message)
		}
	})

	t.Run("the transport E4 copy matches the parser's", func(t *testing.T) {
		// The transport rejects an oversized BODY before Parse runs; the
		// copy must stay identical to the parser's single assembly point.
		_, perr := search.Parse(strings.Repeat("x", search.MaxQueryLen+1))
		if perr == nil {
			t.Fatalf("parser accepted an oversized query")
		}
		var qe *search.QueryError
		if !errors.As(perr, &qe) {
			t.Fatalf("oversize rejection is not a *QueryError: %T", perr)
		}
		if qe.Msg != "AQL query is too long; please reduce the query length to less than 6000 chars" {
			t.Fatalf("parser copy = %q", qe.Msg)
		}
	})

	t.Run("foreign verbs stay the E-26 404", func(t *testing.T) {
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			resp := h.do(method, "/binflow/api/search/aql", adminUser, adminPass, nil, nil)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("%s status = %d, want 404", method, resp.StatusCode)
			}
		}
	})
}

// TestSearchAQLQueryParamFallback pins the body-empty fallback onto
// ?query=<urlencoded AQL> (aql.md section 1, live v4c).
func TestSearchAQLQueryParamFallback(t *testing.T) {
	h := newHarness(t)
	seedAQLStack(t, h)
	q := url.QueryEscape(`items.find({"repo":"generic-local"}).include("name")`)

	resp := h.do(http.MethodPost, "/binflow/api/search/aql?query="+q, adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := mustGet(t, resp)
	if got := aqlNames(t, body); len(got) != 2 || got[0] != "artifact.bin" {
		t.Fatalf("names = %v", got)
	}

	// A non-empty body wins over the parameter (the fallback is the empty
	// body's arm only).
	resp = h.do(http.MethodPost, "/binflow/api/search/aql?query="+q, adminUser, adminPass,
		[]byte(`items.find({"repo":"ghost"}).include("name")`), nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if body := mustGet(t, resp); !strings.Contains(body, "[  ]") {
		t.Fatalf("body param did not lose to the body: %s", body)
	}
}

// TestSearchAQLAccessControl is the L23 dual-user probe (AC2, the t92 form
// plus the AQL leg): a restricted user's whole-domain and per-repo queries
// never surface out-of-scope rows, identity is obfuscated for non-admins,
// and the anonymous arms answer the spec copy on both instance shapes.
func TestSearchAQLAccessControl(t *testing.T) {
	h := newHarnessCfg(t, func(c *config.Config) { c.Security.AnonymousAccess = false },
		[][2]string{{"ci-bot", "ci-pw"}})
	seedRepo(t, h, "generic-local")
	seedRepo(t, h, "vault-local")
	grant(t, h, "ci-out-r", "generic-local", "ci-out/**", "ci-bot", true, false, false)
	deposit(t, h, "generic-local", "ci-out/ok.bin", "visible")
	deposit(t, h, "generic-local", "acme/secret.bin", "hidden")
	deposit(t, h, "vault-local", "top.bin", "classified")

	t.Run("restricted user: whole-domain query, zero leak", func(t *testing.T) {
		status, body := aqlDo(t, h, `items.find({}).include("repo","path","name")`, "ci-bot", "ci-pw", "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200\nbody: %s", status, body)
		}
		if strings.Contains(body, "secret.bin") || strings.Contains(body, "top.bin") || strings.Contains(body, "vault-local") {
			t.Fatalf("out-of-scope rows leaked\nbody: %s", body)
		}
		if !strings.Contains(body, "ok.bin") {
			t.Fatalf("the granted arm is missing\nbody: %s", body)
		}
	})

	t.Run("restricted user: per-repo query on an ungranted repo is the honest empty set", func(t *testing.T) {
		status, body := aqlDo(t, h, `items.find({"repo":"vault-local"}).include("name")`, "ci-bot", "ci-pw", "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (an unreadable repo is not an error)", status)
		}
		if !strings.Contains(body, "[  ]") {
			t.Fatalf("body = %s, want the empty form", body)
		}
	})

	t.Run("path-scoped grant: rows outside the pattern never surface", func(t *testing.T) {
		status, body := aqlDo(t, h, `items.find({"repo":"generic-local"}).include("name")`, "ci-bot", "ci-pw", "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		if strings.Contains(body, "secret.bin") {
			t.Fatalf("a row outside the ci-out/** include pattern leaked\nbody: %s", body)
		}
	})

	t.Run("non-admin identity obfuscation (aql.md section 6)", func(t *testing.T) {
		_, body := aqlDo(t, h, `items.find({"repo":"generic-local","path":"ci-out"})`, "ci-bot", "ci-pw", "")
		if !strings.Contains(body, `"created_by" : "unknown"`) {
			t.Fatalf("non-admin caller must see the masked identity\nbody: %s", body)
		}
	})

	t.Run("admin sees everything with real identities", func(t *testing.T) {
		_, body := aqlDo(t, h, `items.find({}).include("repo","name","created_by")`, adminUser, adminPass, "")
		for _, want := range []string{"secret.bin", "top.bin", `"created_by" : "admin"`} {
			if !strings.Contains(body, want) {
				t.Fatalf("admin view missing %q\nbody: %s", want, body)
			}
		}
	})

	t.Run("anonymous on the closed instance: 401 with the E5 copy", func(t *testing.T) {
		status, body := aqlDo(t, h, `items.find({})`, "", "", "")
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", status)
		}
		var env struct {
			Errors []struct {
				Status  int    `json:"status"`
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal([]byte(body), &env); err != nil {
			t.Fatalf("body not JSON: %v", err)
		}
		if env.Errors[0].Message != "Authentication is required" {
			t.Fatalf("message = %q", env.Errors[0].Message)
		}
	})

	t.Run("anonymous on the open instance: 403 with the E6 copy verbatim", func(t *testing.T) {
		open := newHarness(t)
		seedAQLStack(t, open)
		status, body := aqlDo(t, open, `items.find({})`, "", "", "")
		if status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", status)
		}
		var env struct {
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal([]byte(body), &env); err != nil {
			t.Fatalf("body not JSON: %v", err)
		}
		if want := "Only non-anonymous users are allowed to access AQL queries\n"; env.Errors[0].Message != want {
			t.Fatalf("message = %q, want %q (trailing newline verbatim)", env.Errors[0].Message, want)
		}
	})
}

// TestSearchAQLVirtualRepositories is the FR-133.5 leg (aql.md section 7):
// a virtual key is a legal query VALUE that expands onto its member rows —
// the repository is never a query entity; result rows carry the actual
// storage repo key; virtual_repos joins the output (implicitly when the
// query targeted the virtual, by include otherwise); a nonexistent key is
// the honest empty set.
func TestSearchAQLVirtualRepositories(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "va-local")
	seedRepo(t, h, "vb-local")
	deposit(t, h, "va-local", "one.bin", "one")
	deposit(t, h, "vb-local", "two.bin", "two")
	ctx := context.Background()
	if err := h.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "agg-virtual", Type: repo.TypeVirtual, PackageType: "generic",
		Config: "{}", CreatedAt: "2026-09-01T00:00:00Z", UpdatedAt: "2026-09-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed virtual repo: %v", err)
	}
	if err := h.md.Virtual().SetMembers(ctx, "agg-virtual", []string{"va-local", "vb-local"}); err != nil {
		t.Fatalf("seed members: %v", err)
	}

	t.Run("querying the virtual returns member rows with member keys", func(t *testing.T) {
		status, body := aqlDo(t, h, `items.find({"repo":"agg-virtual"}).include("repo","name").sort({"$asc":["repo"]})`, adminUser, adminPass, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200\nbody: %s", status, body)
		}
		if strings.Contains(body, `"repo" : "agg-virtual"`) {
			t.Fatalf("a result row carries the virtual key — rows are the actual storage rows\nbody: %s", body)
		}
		if !strings.Contains(body, `"repo" : "va-local"`) || !strings.Contains(body, `"repo" : "vb-local"`) {
			t.Fatalf("member rows missing\nbody: %s", body)
		}
	})

	t.Run("virtual_repos by explicit include", func(t *testing.T) {
		_, body := aqlDo(t, h, `items.find({"repo":"vb-local"}).include("repo","virtual_repos")`, adminUser, adminPass, "")
		if !strings.Contains(body, "\"virtual_repos\" : [ \"agg-virtual\" ]") {
			t.Fatalf("virtual_repos projection wrong\nbody: %s", body)
		}
		// A member of no virtual carries the empty form.
		seedRepo(t, h, "lonely-local")
		deposit(t, h, "lonely-local", "lone.bin", "l")
		_, body = aqlDo(t, h, `items.find({"repo":"lonely-local"}).include("repo","virtual_repos")`, adminUser, adminPass, "")
		if !strings.Contains(body, "\"virtual_repos\" : [ ]") {
			t.Fatalf("non-member virtual_repos must be the empty form\nbody: %s", body)
		}
	})

	t.Run("virtual_repos implicit when the query targets the virtual", func(t *testing.T) {
		_, body := aqlDo(t, h, `items.find({"repo":"agg-virtual"})`, adminUser, adminPass, "")
		if !strings.Contains(body, "\"virtual_repos\" : [ \"agg-virtual\" ]") {
			t.Fatalf("implicit virtual_repos missing\nbody: %s", body)
		}
		// A plain local query keeps the default set only.
		_, body = aqlDo(t, h, `items.find({"repo":"va-local"})`, adminUser, adminPass, "")
		if strings.Contains(body, "virtual_repos") {
			t.Fatalf("virtual_repos must not leak into a non-virtual query's default output\nbody: %s", body)
		}
	})

	t.Run("nonexistent repo key is the honest empty set (section 7-4)", func(t *testing.T) {
		status, body := aqlDo(t, h, `items.find({"repo":"misspelled-virtual"})`, adminUser, adminPass, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		if !strings.Contains(body, "[  ]") {
			t.Fatalf("body = %s", body)
		}
	})
}

// TestSearchAQLTruncation is the K63 cap leg: a query with no stated limit
// stops at the 1,000-row cap with the truncation header AND the verbatim
// range.notification (ADR-0043 Errata 6); a tighter limit truncates nothing.
func TestSearchAQLTruncation(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "cap-local")
	ctx := context.Background()
	sha := strings.Repeat("0", 64)
	if err := h.md.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: 1, CreatedAt: "2026-09-01T00:00:00Z"}); err != nil {
		t.Fatalf("seed blob: %v", err)
	}
	raw, ok := h.md.(interface {
		RawConn(ctx context.Context, fn func(*sql.Conn) error) error
	})
	if !ok {
		t.Fatalf("the sqlite store does not expose RawConn")
	}
	err := raw.RawConn(ctx, func(conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
			return err
		}
		stmt, err := conn.PrepareContext(ctx,
			`INSERT INTO nodes (repo_key, path, sha256, size, mime, created_by, created_at, updated_at)
			 VALUES ('cap-local', ?, ?, 1024, 'application/octet-stream', 'admin', '2026-09-01T00:00:00Z', '2026-09-01T00:00:00Z')`)
		if err != nil {
			return err
		}
		for i := 0; i < search.ResultCap+5; i++ {
			if _, err := stmt.ExecContext(ctx, fmt.Sprintf("f%05d.bin", i), sha); err != nil {
				return err
			}
		}
		if err := stmt.Close(); err != nil {
			return err
		}
		_, err = conn.ExecContext(ctx, "COMMIT")
		return err
	})
	if err != nil {
		t.Fatalf("seed %d nodes: %v", search.ResultCap+5, err)
	}

	resp := aqlPost(t, h, `items.find({"repo":"cap-local"}).include("name")`, adminUser, adminPass)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get(search.TruncatedHeader); got != "true" {
		t.Fatalf("truncation header = %q, want true", got)
	}
	body := mustGet(t, resp)
	if !strings.Contains(body, `"notification" : "AQL query reached the search hard limit, results are trimmed."`) {
		t.Fatalf("notification copy missing\nbody tail: %s", body[len(body)-300:])
	}
	start, end, total, limit, _ := aqlRange(t, body)
	if start != 0 || end != search.ResultCap || total != search.ResultCap {
		t.Fatalf("range = %d/%d/%d, want 0/%d/%d", start, end, total, search.ResultCap, search.ResultCap)
	}
	if limit != nil {
		t.Fatalf("range.limit echoed %v though the query stated none", *limit)
	}
	if got := aqlNames(t, body); len(got) != search.ResultCap {
		t.Fatalf("rows = %d, want the cap %d", len(got), search.ResultCap)
	}

	// A user limit below the raw row count still truncates by design: the
	// cap+1 probe walks the RAW row order, so the marker is the honest
	// upper bound "more rows follow", not "the hard cap fired" (ADR-0043
	// pt 4/5, the T-413 engine's frozen semantics).
	resp = aqlPost(t, h, `items.find({"repo":"cap-local"}).include("name").limit(5)`, adminUser, adminPass)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get(search.TruncatedHeader); got != "true" {
		t.Fatalf("truncation header = %q, want true (raw rows remain past the window)", got)
	}
	if got := aqlNames(t, mustGet(t, resp)); len(got) != 5 {
		t.Fatalf("rows = %d, want 5", len(got))
	}

	// The final page of the raw sequence: no row follows it, both layers go
	// quiet.
	resp = aqlPost(t, h, `items.find({"repo":"cap-local"}).include("name").offset(`+
		fmt.Sprint(search.ResultCap+4)+`)`, adminUser, adminPass)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get(search.TruncatedHeader); got != "" {
		t.Fatalf("truncation header = %q, want unset on the last raw page", got)
	}
	body = mustGet(t, resp)
	if strings.Contains(body, "notification") {
		t.Fatalf("notification present without truncation\nbody: %s", body)
	}
	if got := aqlNames(t, body); len(got) != 1 {
		t.Fatalf("rows = %d, want the single last row", len(got))
	}
}

// TestSearchAQLMetrics is the observability leg (AC3, ADR-0043 pt 8): the
// search family's three shapes appear on /metrics with the query counted for
// every executed query (rejections included) and both rejection reasons
// pre-seeded.
func TestSearchAQLMetrics(t *testing.T) {
	h := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		d.Metrics = metrics.NewRegistry()
	}, nil)
	seedAQLStack(t, h)

	for _, q := range []string{
		`items.find({"repo":"generic-local"}).include("name")`,
		`items.find({"repo":"generic-local"}).include("name").limit(1)`,
		`items.find({"repo":`, // 400: counted, not a gate rejection
	} {
		status, _ := aqlDo(t, h, q, adminUser, adminPass, "")
		if status != http.StatusOK && status != http.StatusBadRequest {
			t.Fatalf("status = %d", status)
		}
	}

	resp := h.do(http.MethodGet, "/metrics", "", "", nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics status = %d", resp.StatusCode)
	}
	body := mustGet(t, resp)
	for _, want := range []string{
		"# TYPE binflow_search_queries_total counter",
		`binflow_search_queries_total{plane="aql"} 3`,
		"# TYPE binflow_search_query_duration_seconds histogram",
		"binflow_search_query_duration_seconds_count 3",
		"# TYPE binflow_search_rejections_total counter",
		`binflow_search_rejections_total{reason="concurrency"} 0`,
		`binflow_search_rejections_total{reason="timeout"} 0`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q\nbody:\n%s", want, body)
		}
	}
	// The T-92 family keeps its own counters (zero-regression posture).
	if !strings.Contains(body, "# TYPE binflow_http_requests_total counter") {
		t.Fatalf("the HTTP family vanished")
	}
}

// TestSearchAQLLegacyEndpointsUnchanged is AC3's zero-regression spot pin
// beside the full-suite run: the two legacy entrances answer exactly as
// before on the same stack the AQL route now shares.
func TestSearchAQLLegacyEndpointsUnchanged(t *testing.T) {
	h := newHarness(t)
	seedAQLStack(t, h)

	resp := h.do(http.MethodGet, "/binflow/api/search/artifact?name=artifact", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("legacy artifact search status = %d", resp.StatusCode)
	}
	var b searchBody
	if err := json.Unmarshal([]byte(mustGet(t, resp)), &b); err != nil {
		t.Fatalf("legacy body: %v", err)
	}
	if len(b.Results) != 1 || b.Results[0].Repo != "generic-local" {
		t.Fatalf("legacy results = %+v", b.Results)
	}

	sum := sha256.Sum256([]byte("artifact-bytes"))
	resp = h.do(http.MethodGet, "/binflow/api/search/checksum?sha256="+hex.EncodeToString(sum[:]), adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("legacy checksum search status = %d", resp.StatusCode)
	}
}
