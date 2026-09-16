package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The /api/metadata incremental property face (L024-8 / D01-R08; the spec
// rest-api.md section 3.1, live-calibrated by L024-1 against 7.161.15) and
// the POST /api/storage 405 that retired the old form: the wire contract of
// PATCH {"props":…} (replace/null/array-element laws, the verbatim 400s,
// the annotate gate and its own 403 wording, the recursion defaults) and
// DELETE-all (idempotent 204, the same target guards).

// doMeta runs one metadata-face verb with a body and returns status plus
// the envelope message ("" for the bodyless 204s).
func doMeta(t *testing.T, h *harness, method, path, user, pass, body string) (int, string) {
	t.Helper()
	var raw []byte
	if body != "" {
		raw = []byte(body)
	}
	resp := h.do(method, path, user, pass, raw, nil)
	defer drain(resp)
	got := mustGet(t, resp)
	if strings.TrimSpace(got) == "" {
		return resp.StatusCode, ""
	}
	return resp.StatusCode, envelopeMsg(t, []byte(got))
}

func TestMetadataPatchPropsLaws(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/l024/app.bin", "app")
	const base = "/binflow/api/metadata/generic-local/l024/app.bin"

	// Section 3.1 item 1: a new key lands with its whole array.
	if st, msg := doMeta(t, h, http.MethodPatch, base, adminUser, adminPass,
		`{"props":{"k":["v1","v2"]}}`); st != http.StatusNoContent {
		t.Fatalf("PATCH new key = %d %s", st, msg)
	}
	if _, props := getProps(t, h, "/binflow/api/storage/generic-local/l024/app.bin?properties", adminUser, adminPass); len(props["k"]) != 2 {
		t.Fatalf("props after PATCH = %#v, want k=[v1 v2]", props)
	}

	// Item 2: an existing key is REPLACED (delete-then-set), other keys kept.
	if st, msg := doMeta(t, h, http.MethodPatch, base, adminUser, adminPass,
		`{"props":{"other":"x"}}`); st != http.StatusNoContent {
		t.Fatalf("PATCH second key = %d %s", st, msg)
	}
	if st, msg := doMeta(t, h, http.MethodPatch, base, adminUser, adminPass,
		`{"props":{"k":["new"]}}`); st != http.StatusNoContent {
		t.Fatalf("PATCH replace = %d %s", st, msg)
	}
	_, props := getProps(t, h, "/binflow/api/storage/generic-local/l024/app.bin?properties", adminUser, adminPass)
	if len(props["k"]) != 1 || props["k"][0] != "new" || len(props["other"]) != 1 {
		t.Fatalf("props after replace = %#v, want k=[new] and other kept", props)
	}

	// Item 3: null drops the key, idempotently.
	for i := 0; i < 2; i++ {
		if st, msg := doMeta(t, h, http.MethodPatch, base, adminUser, adminPass,
			`{"props":{"k":null}}`); st != http.StatusNoContent {
			t.Fatalf("PATCH null #%d = %d %s", i, st, msg)
		}
	}
	if _, props := getProps(t, h, "/binflow/api/storage/generic-local/l024/app.bin?properties", adminUser, adminPass); len(props) != 1 || props["k"] != nil {
		t.Fatalf("props after null = %#v, want only other", props)
	}

	// Item 7: non-text array elements are silently skipped; a bare string
	// value is a one-value set.
	if st, msg := doMeta(t, h, http.MethodPatch, base, adminUser, adminPass,
		`{"props":{"m":["a",7],"s":"solo"}}`); st != http.StatusNoContent {
		t.Fatalf("PATCH mixed array = %d %s", st, msg)
	}
	_, props = getProps(t, h, "/binflow/api/storage/generic-local/l024/app.bin?properties=m,s", adminUser, adminPass)
	if len(props["m"]) != 1 || props["m"][0] != "a" || len(props["s"]) != 1 || props["s"][0] != "solo" {
		t.Fatalf("mixed props = %#v, want m=[a] s=[solo]", props)
	}
}

func TestMetadataPatchBodyErrors(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/l024/app.bin", "app")
	const base = "/binflow/api/metadata/generic-local/l024/app.bin"

	cases := []struct {
		name string
		body string
		want string
	}{
		// Item 4, verbatim: neither field (nor a null one) carries a request.
		{"empty object", `{}`, "props or stats fields required"},
		{"null props", `{"props":null}`, "props or stats fields required"},
		// Item 5, verbatim inside the set-failure wrap (period included):
		// a value that is neither a string nor an array.
		{"number value", `{"props":{"n":5}}`,
			"Failed to set properties on generic-local:l024/app.bin: Failed to parse json object while performing patch properties request."},
		{"object value", `{"props":{"n":{"x":1}}}`,
			"Failed to set properties on generic-local:l024/app.bin: Failed to parse json object while performing patch properties request."},
		{"malformed body", `not json`,
			"Failed to set properties on generic-local:l024/app.bin: Failed to parse json object while performing patch properties request."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, msg := doMeta(t, h, http.MethodPatch, base, adminUser, adminPass, tc.body)
			if st != http.StatusBadRequest {
				t.Fatalf("status = %d msg = %s, want 400", st, msg)
			}
			if msg != tc.want {
				t.Fatalf("message =\n %s\nwant\n %s", msg, tc.want)
			}
		})
	}

	// Item 10 as the L024-10 differential closed it (L024-11 / diff T4):
	// a stats-only body MERGES — the count lands absolutely, the "import"
	// marker rides lastDownloadedBy, and the ?stats echo renders the
	// download-form uri with every field present.
	if st, msg := doMeta(t, h, http.MethodPatch, base, adminUser, adminPass,
		`{"stats":{"downloadCount":5}}`); st != http.StatusNoContent {
		t.Fatalf("stats-only PATCH = %d %s", st, msg)
	}
	if _, props := getProps(t, h, "/binflow/api/storage/generic-local/l024/app.bin?properties", adminUser, adminPass); len(props) != 0 {
		t.Fatalf("stats leg touched props: %#v", props)
	}
	resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/l024/app.bin?stats", adminUser, adminPass, nil, nil)
	page, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var stats struct {
		URI                  string `json:"uri"`
		DownloadCount        int64  `json:"downloadCount"`
		LastDownloaded       int64  `json:"lastDownloaded"`
		LastDownloadedBy     string `json:"lastDownloadedBy"`
		RemoteDownloadCount  int64  `json:"remoteDownloadCount"`
		RemoteLastDownloaded int64  `json:"remoteLastDownloaded"`
	}
	if json.Unmarshal(page, &stats) != nil || stats.DownloadCount != 5 ||
		stats.LastDownloadedBy != "import" || stats.LastDownloaded != 0 ||
		stats.RemoteDownloadCount != 0 || stats.RemoteLastDownloaded != 0 {
		t.Fatalf("stats echo = %s, want the merged import-marked shape", page)
	}
	if !strings.HasSuffix(stats.URI, "/binflow/generic-local/l024/app.bin") ||
		strings.Contains(stats.URI, "/api/storage/") {
		t.Fatalf("stats uri = %q, want the download form", stats.URI)
	}
	// A malformed stats value is the same parse 400.
	if st, msg := doMeta(t, h, http.MethodPatch, base, adminUser, adminPass,
		`{"stats":5}`); st != http.StatusBadRequest || !strings.Contains(msg, "Failed to parse json object") {
		t.Fatalf("malformed stats = %d %s", st, msg)
	}
	// props+stats together: the props leg still applies.
	if st, msg := doMeta(t, h, http.MethodPatch, base, adminUser, adminPass,
		`{"props":{"both":"yes"},"stats":{}}`); st != http.StatusNoContent {
		t.Fatalf("props+stats PATCH = %d %s", st, msg)
	}
	if _, props := getProps(t, h, "/binflow/api/storage/generic-local/l024/app.bin?properties", adminUser, adminPass); len(props["both"]) != 1 {
		t.Fatalf("props+stats props = %#v", props)
	}
}

func TestMetadataPatchTargetErrors(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/l024/app.bin", "app")
	if resp := putRepo(t, h, "agg-virt",
		`{"rclass":"virtual","packageType":"generic","repositories":["generic-local"]}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("virtual create: status %d body=%s", resp.StatusCode, mustGet(t, resp))
	}
	const body = `{"props":{"k":"v"}}`

	// Item 6: a missing item is the 400 wrap (NOT a 404), colon spelling.
	st, msg := doMeta(t, h, http.MethodPatch,
		"/binflow/api/metadata/generic-local/l024/nope.bin", adminUser, adminPass, body)
	if st != http.StatusBadRequest || msg !=
		"Failed to set properties on generic-local:l024/nope.bin: Item generic-local:l024/nope.bin does not exist" {
		t.Fatalf("missing item = %d %s", st, msg)
	}
	// Item 12: a virtual repository is its own 400 cause.
	st, msg = doMeta(t, h, http.MethodPatch,
		"/binflow/api/metadata/agg-virt/l024/app.bin", adminUser, adminPass, body)
	if st != http.StatusBadRequest || msg !=
		"Failed to set properties on agg-virt:l024/app.bin: Repository 'agg-virt' is not a local repository" {
		t.Fatalf("virtual target = %d %s", st, msg)
	}
	// An unknown repository key fails the same local test (the shared
	// guard; the spec records only virtual/remote — registered reading).
	st, msg = doMeta(t, h, http.MethodPatch,
		"/binflow/api/metadata/ghost/l024/app.bin", adminUser, adminPass, body)
	if st != http.StatusBadRequest || !strings.Contains(msg, "Repository 'ghost' is not a local repository") {
		t.Fatalf("unknown repo = %d %s", st, msg)
	}
}

func TestMetadataPatchPermissions(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"plainuser", "plainpass"}, {"annotator", "annotatorpass"}})
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/sec/app.bin", "app")
	grant(t, h, "sec-read", "generic-local", "sec/**", "plainuser", true, false, false)
	grantBitsHTTP(t, h, "sec-annotate", "annotator", true, false, false, false, true)
	const base = "/binflow/api/metadata/generic-local/sec/app.bin"

	// Item 11: the PATCH 403 carries the face's own wording, verbatim.
	st, msg := doMeta(t, h, http.MethodPatch, base, "plainuser", "plainpass", `{"props":{"k":"v"}}`)
	if st != http.StatusForbidden || msg !=
		"Request for 'generic-local:sec/app.bin' is forbidden for user: 'plainuser', You must have annotate permission on this path" {
		t.Fatalf("reader PATCH = %d %s", st, msg)
	}
	// Anonymous meets the route's 401 challenge.
	if st, _ := doMeta(t, h, http.MethodPatch, base, "", "", `{"props":{"k":"v"}}`); st != http.StatusUnauthorized {
		t.Fatalf("anonymous PATCH = %d, want 401", st)
	}
	// The annotate-only principal writes and drops through this face.
	if st, msg := doMeta(t, h, http.MethodPatch, base, "annotator", "annotatorpass",
		`{"props":{"k":"v"}}`); st != http.StatusNoContent {
		t.Fatalf("annotator PATCH = %d %s", st, msg)
	}
	if st, msg := doMeta(t, h, http.MethodPatch, base, "annotator", "annotatorpass",
		`{"props":{"k":null}}`); st != http.StatusNoContent {
		t.Fatalf("annotator null PATCH = %d %s", st, msg)
	}
}

func TestMetadataPatchRecursive(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/rel/x.bin", "x")
	putContent(t, h, "/binflow/generic-local/rel/sub/y.bin", "y")
	const base = "/binflow/api/metadata/generic-local/rel"
	const readBack = "/binflow/api/storage/generic-local"

	// Item 8: a folder target defaults RECURSIVE (the PUT family's law).
	if st, msg := doMeta(t, h, http.MethodPatch, base, adminUser, adminPass,
		`{"props":{"rel":"default"}}`); st != http.StatusNoContent {
		t.Fatalf("folder-default PATCH = %d %s", st, msg)
	}
	for _, p := range []string{"rel", "rel/x.bin", "rel/sub/y.bin"} {
		if _, props := getProps(t, h, readBack+"/"+p+"?properties=rel", adminUser, adminPass); len(props) != 1 {
			t.Fatalf("folder-default fan-out missed %s", p)
		}
	}
	// recursiveProperties=0 pins the write to the folder row.
	if st, msg := doMeta(t, h, http.MethodPatch, base+"?recursiveProperties=0", adminUser, adminPass,
		`{"props":{"pinned":"row"}}`); st != http.StatusNoContent {
		t.Fatalf("pinned PATCH = %d %s", st, msg)
	}
	if _, props := getProps(t, h, readBack+"/rel?properties=pinned", adminUser, adminPass); len(props) != 1 {
		t.Fatal("folder row must carry the pinned key")
	}
	if _, props := getProps(t, h, readBack+"/rel/x.bin?properties=pinned", adminUser, adminPass); len(props) != 0 {
		t.Fatalf("recursiveProperties=0 leaked to children: %#v", props)
	}
	// A FILE target defaults non-recursive — and an explicit 1 on a file
	// still touches exactly the file.
	if st, msg := doMeta(t, h, http.MethodPatch, base+"/x.bin?recursiveProperties=1", adminUser, adminPass,
		`{"props":{"fileonly":"1"}}`); st != http.StatusNoContent {
		t.Fatalf("file PATCH = %d %s", st, msg)
	}
	if _, props := getProps(t, h, readBack+"/rel/sub/y.bin?properties=fileonly", adminUser, adminPass); len(props) != 0 {
		t.Fatal("file PATCH leaked to a sibling")
	}
}

func TestMetadataDeleteAll(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/rel/x.bin;keep=1;drop=2", "x")
	putContent(t, h, "/binflow/generic-local/rel/sub/y.bin;drop=3", "y")
	putContent(t, h, "/binflow/generic-local/bare/b1.bin", "bare")
	const base = "/binflow/api/metadata/generic-local"
	const readBack = "/binflow/api/storage/generic-local"

	// The folder default fans the drop out (recursive by target type).
	if st, msg := doMeta(t, h, http.MethodDelete, base+"/rel", adminUser, adminPass, ""); st != http.StatusNoContent {
		t.Fatalf("folder DELETE = %d %s", st, msg)
	}
	for _, p := range []string{"rel", "rel/x.bin", "rel/sub/y.bin"} {
		if _, props := getProps(t, h, readBack+"/"+p+"?properties", adminUser, adminPass); len(props) != 0 {
			t.Fatalf("DELETE left residue on %s: %#v", p, props)
		}
	}
	// The no-op law: an item without properties still answers 204.
	if st, msg := doMeta(t, h, http.MethodDelete, base+"/bare/b1.bin", adminUser, adminPass, ""); st != http.StatusNoContent {
		t.Fatalf("no-op DELETE = %d %s", st, msg)
	}
	// recursive=0 pins the drop to the folder row.
	putContent(t, h, "/binflow/generic-local/pin/a.bin;pk=1", "a")
	if st, msg := doMeta(t, h, http.MethodDelete, base+"/pin?recursive=0", adminUser, adminPass, ""); st != http.StatusNoContent {
		t.Fatalf("pinned DELETE = %d %s", st, msg)
	}
	if _, props := getProps(t, h, readBack+"/pin/a.bin?properties=pk", adminUser, adminPass); len(props) != 1 {
		t.Fatal("recursive=0 DELETE leaked to the child")
	}
	// L024-11 / diff T1: the DELETE face is GUARD-LESS — a missing item
	// (and a virtual repository alike) answers the silent 204, the PATCH
	// family's 400 wordings never ride this verb (the differential's live
	// closure of the low-confidence arm).
	if st, _ := doMeta(t, h, http.MethodDelete, base+"/ghost.bin", adminUser, adminPass, ""); st != http.StatusNoContent {
		t.Fatal("missing item DELETE must be the guard-less 204")
	}
}

// TestMetadataOtherVerbs405 (L024-11 / diff T2): PUT/GET/POST on the
// /api/metadata face are the 405 envelope with the Allow header — never
// the E-26 404.
func TestMetadataOtherVerbs405(t *testing.T) {
	h := newHarnessCfg(t, nil, nil)
	seedRepo(t, h, "generic-local")
	for _, m := range []string{http.MethodPut, http.MethodGet, http.MethodPost} {
		resp := h.do(m, "/binflow/api/metadata/generic-local/a.bin", adminUser, adminPass, nil, nil)
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("%s = %d %s, want 405", m, resp.StatusCode, body)
		}
		var env struct {
			Errors []struct {
				Status  int    `json:"status"`
				Message string `json:"message"`
			} `json:"errors"`
		}
		if json.Unmarshal(body, &env) != nil || len(env.Errors) != 1 || env.Errors[0].Status != 405 ||
			env.Errors[0].Message != "Method Not Allowed" {
			t.Fatalf("%s body = %s, want the 405 envelope verbatim", m, body)
		}
		if allow := resp.Header.Get("Allow"); allow != "DELETE,OPTIONS,PATCH" {
			t.Fatalf("%s Allow = %q", m, allow)
		}
	}
	// L024-12 micro-residual: the retired POST /api/storage form carries its
	// own resource's Allow set (the reference's live probe: DELETE,GET,
	// OPTIONS,PUT), whatever query arms ride along.
	resp := h.do(http.MethodPost, "/binflow/api/storage/generic-local/a.bin", adminUser, adminPass, nil, nil)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "DELETE,GET,OPTIONS,PUT" {
		t.Fatalf("POST storage = %d Allow=%q body=%s, want 405 with the resource Allow set",
			resp.StatusCode, resp.Header.Get("Allow"), body)
	}
}

func TestMetadataDeletePermissions(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"plainuser", "plainpass"}})
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/sec/app.bin;k=v", "app")
	grant(t, h, "sec-read", "generic-local", "sec/**", "plainuser", true, false, false)
	const base = "/binflow/api/metadata/generic-local/sec/app.bin"

	// Item 11's DELETE arm: the bare family 403, then the 401 challenge.
	if st, _ := doMeta(t, h, http.MethodDelete, base, "plainuser", "plainpass", ""); st != http.StatusForbidden {
		t.Fatalf("reader DELETE = %d, want 403", st)
	}
	if st, _ := doMeta(t, h, http.MethodDelete, base, "", "", ""); st != http.StatusUnauthorized {
		t.Fatalf("anonymous DELETE = %d, want 401", st)
	}
}

// TestStoragePostFormRetired pins the old "Update Item Properties" POST
// form's grave (rest-api.md section 3, live 7.161.x): whatever arms ride
// along, the verb is the bare 405 envelope — verbatim, both message text
// and status field.
func TestStoragePostFormRetired(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/route/app.bin", "app")

	for _, path := range []string{
		"/binflow/api/storage/generic-local/route/app.bin",
		"/binflow/api/storage/generic-local/route/app.bin?properties=k=v",
		"/binflow/api/storage/generic-local/route/app.bin?recursive=true&atomic=true",
	} {
		resp := h.do(http.MethodPost, path, adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		drain(resp)
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s = %d, want 405", path, resp.StatusCode)
		}
		if !strings.Contains(body, `"status": 405`) || !strings.Contains(body, `"message": "Method Not Allowed"`) {
			t.Fatalf("POST %s envelope = %s, want the verbatim 405", path, body)
		}
	}

	// The no-segment spelling keeps the storage family's generic 404 (the
	// reference's resource grammar demands a path).
	resp := h.do(http.MethodPost, "/binflow/api/storage", adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	drain(resp)
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "Not Found") {
		t.Fatalf("bare POST /api/storage = %d %s, want the generic 404", resp.StatusCode, body)
	}
}
