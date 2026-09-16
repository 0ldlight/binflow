// The L024-4 differential's repaired faces (ticket L024-5): the
// latestVersion non-wildcard concatenation (diff L2 — v + snapshot
// timestamp + "-N", no separator), the setItemProperties carriers (diff
// L8 — the query form with semicolon pairs, net/url's whole-query failure
// notwithstanding, and the path-matrix twin), and the AQL property.key /
// property.value single-member projections (diff L5).

package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestLatestVersionNonWildcardConcatenates: diff L2's live-pinned quirk —
// the answer glues the version, the line's snapshot timestamp and the
// build number with NO separator between v and the timestamp.
func TestLatestVersionNonWildcardConcatenates(t *testing.T) {
	h := newHarness(t)
	seedVersionStack(t, h)
	resp := h.do(http.MethodGet, "/binflow/api/search/latestVersion?g=com.acme&a=demo-app&v=2.0", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d\n%s", resp.StatusCode, mustGet(t, resp))
	}
	if body := mustGet(t, resp); body != "2.0"+"20260915.175736"+"-1" {
		t.Fatalf("body = %q, want the no-separator concatenation %q", body, "2.020260915.175736-1")
	}
}

// TestSetItemPropertiesCarriers: diff L8 — both spellings of the property
// write land (204) and merge: the official query form with
// semicolon-separated pairs (which net/url's whole-query parser rejects —
// the route reads the raw string), and the jf path-matrix twin.
func TestSetItemPropertiesCarriers(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "props-local")
	deposit(t, h, "props-local", "a/one.bin", "one-bytes")

	resp := h.do(http.MethodPut, "/binflow/api/storage/props-local/a/one.bin?properties=build.name=app;build.number=7",
		adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("query+semicolon form = %d\n%s", resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()

	resp = h.do(http.MethodPut, "/binflow/api/storage/props-local/a/one.bin;env=prod",
		adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("matrix form = %d\n%s", resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()

	got := h.do(http.MethodGet, "/binflow/api/storage/props-local/a/one.bin?properties", adminUser, adminPass, nil, nil)
	defer func() { _ = got.Body.Close() }()
	page := mustGet(t, got)
	for _, want := range []string{`"build.name"`, `"app"`, `"build.number"`, `"7"`, `"env"`, `"prod"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("merged properties missing %s:\n%s", want, page)
		}
	}
}

// TestAQLPropertySingleMemberProjections: diff L5 — include("property.key")
// renders key-only objects, include("property.value") value-only, and a
// property-less row omits the key in both spellings.
func TestAQLPropertySingleMemberProjections(t *testing.T) {
	h := newHarness(t)
	seedJFStack(t, h)

	status, body := aqlDo(t, h,
		`items.find({"repo":"jf-local","name":"artifact.zip"}).include("name","property.key")`,
		adminUser, adminPass, "")
	if status != http.StatusOK || !strings.Contains(body, `"key" : "build.name"`) ||
		strings.Contains(body, `"value"`) {
		t.Fatalf("property.key = %d %s, want key-only objects", status, body)
	}

	status, body = aqlDo(t, h,
		`items.find({"repo":"jf-local","name":"plain.bin"}).include("name","property.key")`,
		adminUser, adminPass, "")
	if status != http.StatusOK || strings.Contains(body, "properties") {
		t.Fatalf("property-less row must omit the key: %d %s", status, body)
	}

	status, body = aqlDo(t, h,
		`items.find({"repo":"jf-local","name":"artifact.zip"}).include("name","property.value")`,
		adminUser, adminPass, "")
	if status != http.StatusOK || !strings.Contains(body, `"value" : "mybuild"`) ||
		strings.Contains(body, `"key"`) {
		t.Fatalf("property.value = %d %s, want value-only objects", status, body)
	}
}
