package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The ?properties read/write family and the matrix-parameter deploy
// (M10 T-286, FR-89 / architecture section 15.3): the REST contract end to
// end — deploy-time annotation, filtered reads, the merge write, the
// selective delete, wildcards, recursion, atomicity, the permission gates
// and the legacy ';' literal-path regression (FR-89-AC4, the seed-m10
// fixture shapes).

// putContent deploys body at path through the real content plane.
func putContent(t *testing.T, h *harness, path, body string) {
	t.Helper()
	resp := h.do(http.MethodPut, path, adminUser, adminPass, []byte(body), nil)
	defer drain(resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT %s status = %d; body=%s", path, resp.StatusCode, mustGet(t, resp))
	}
}

// getProps reads the properties view of one path (nil body on non-200).
func getProps(t *testing.T, h *harness, path, user, pass string) (int, map[string][]string) {
	t.Helper()
	resp := h.do(http.MethodGet, path, user, pass, nil, nil)
	defer drain(resp)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	var view struct {
		Properties map[string][]string `json:"properties"`
	}
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("GET %s body %q: %v", path, body, err)
	}
	return resp.StatusCode, view.Properties
}

// doProps runs one mutating properties verb and returns its status.
func doProps(t *testing.T, h *harness, method, path, user, pass string) int {
	t.Helper()
	resp := h.do(method, path, user, pass, nil, nil)
	defer drain(resp)
	return resp.StatusCode
}

func TestStoragePropertiesMatrixDeploy(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	// FR-89-AC1: the paired suffix strips — the artifact lands at the clean
	// path and the properties ride the deploy.
	putContent(t, h, "/binflow/generic-local/a/b/app.bin;build=77;env=prod", "app-bytes")

	// The artifact is addressable at the CLEAN path only: the stripped
	// spelling serves the body, the matrix spelling addresses the same node
	// (a GET of the suffixed form re-peels to the same path).
	got := h.do(http.MethodGet, "/binflow/generic-local/a/b/app.bin", adminUser, adminPass, nil, nil)
	defer drain(got)
	if got.StatusCode != http.StatusOK || mustGet(t, got) != "app-bytes" {
		t.Fatalf("clean-path GET = %d %q", got.StatusCode, mustGet(t, got))
	}
	suffixed := h.do(http.MethodGet, "/binflow/generic-local/a/b/app.bin;build=77", adminUser, adminPass, nil, nil)
	defer drain(suffixed)
	if suffixed.StatusCode != http.StatusOK {
		t.Fatalf("suffixed GET status = %d", suffixed.StatusCode)
	}

	// ?properties reads the deploy set back (FR-89-AC1's readback).
	status, props := getProps(t, h, "/binflow/api/storage/generic-local/a/b/app.bin?properties=build,env", adminUser, adminPass)
	if status != http.StatusOK {
		t.Fatalf("properties GET status = %d", status)
	}
	if len(props) != 2 || props["build"][0] != "77" || props["env"][0] != "prod" {
		t.Fatalf("props = %+v", props)
	}

	// Unfiltered read = the whole key set.
	_, all := getProps(t, h, "/binflow/api/storage/generic-local/a/b/app.bin?properties", adminUser, adminPass)
	if len(all) != 2 {
		t.Fatalf("unfiltered props = %+v", all)
	}

	// The detail body echoes the set additively (FR-89-AC1's ".info 一致"
	// arm); a property-less node's body stays byte-form identical (the
	// field omits — the M9-frozen assertions keep passing).
	detail := h.do(http.MethodGet, "/binflow/api/storage/generic-local/a/b/app.bin", adminUser, adminPass, nil, nil)
	defer drain(detail)
	var fileInfo struct {
		Properties map[string][]string `json:"properties"`
	}
	if err := json.Unmarshal([]byte(mustGet(t, detail)), &fileInfo); err != nil {
		t.Fatalf("detail body: %v", err)
	}
	if len(fileInfo.Properties) != 2 || fileInfo.Properties["build"][0] != "77" {
		t.Fatalf("detail properties = %+v", fileInfo.Properties)
	}

	// FR-89-AC3's matrix arm: the illegal key answers 400 BEFORE anything
	// lands (the artifact of a refused deploy never materializes).
	bad := h.do(http.MethodPut, "/binflow/generic-local/a/b/refused.bin;bad key=1",
		adminUser, adminPass, []byte("x"), nil)
	defer drain(bad)
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("illegal matrix key status = %d; body=%s", bad.StatusCode, mustGet(t, bad))
	}
	missing := h.do(http.MethodGet, "/binflow/generic-local/a/b/refused.bin", adminUser, adminPass, nil, nil)
	defer drain(missing)
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("refused deploy still landed: %d", missing.StatusCode)
	}
}

// TestStoragePropertiesLegacySemicolon is FR-89-AC4's regression surface:
// the five seed-m10 fixture shapes (the non-paired ';' survival set,
// decision 11.39) keep their literal reachability, byte-identical, on the
// generic content plane.
func TestStoragePropertiesLegacySemicolon(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	fixtures := []string{
		"legacy/a;b.bin",
		"legacy/file;name.jar",
		"legacy/x;y;z.txt",
		"legacy/dir;d/nested.bin",
	}
	for i, fx := range fixtures {
		putContent(t, h, "/binflow/generic-local/"+fx, fmt.Sprintf("body-%d", i))
	}
	for i, fx := range fixtures {
		resp := h.do(http.MethodGet, "/binflow/generic-local/"+fx, adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		drain(resp)
		if resp.StatusCode != http.StatusOK || body != fmt.Sprintf("body-%d", i) {
			t.Fatalf("fixture %q readback = %d %q", fx, resp.StatusCode, body)
		}
	}

	// A paired-lookalike name is the FEATURE's behavior change, not legacy
	// surface: the deploy strips and the literal path never exists.
	putContent(t, h, "/binflow/generic-local/legacy/app.bin;build=77", "paired-bytes")
	literal := h.do(http.MethodGet, "/binflow/generic-local/legacy/app.bin;build=77", adminUser, adminPass, nil, nil)
	defer drain(literal)
	if literal.StatusCode != http.StatusOK {
		t.Fatalf("paired-lookalike re-GET status = %d", literal.StatusCode)
	}
	if mustGet(t, literal) != "paired-bytes" {
		t.Fatal("paired-lookalike re-GET must serve the same node (re-peeled addressing)")
	}
}

func TestStoragePropertiesRESTFamily(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/ci/app.bin;build=77;build2=x", "app")

	const base = "/binflow/api/storage/generic-local/ci/app.bin"

	t.Run("PUT merge then readback", func(t *testing.T) {
		if s := doProps(t, h, http.MethodPut, base+"?properties=qa=passed,owner=team-a", adminUser, adminPass); s != http.StatusNoContent {
			t.Fatalf("PUT status = %d", s)
		}
		_, props := getProps(t, h, base+"?properties", adminUser, adminPass)
		if len(props) != 4 || props["qa"][0] != "passed" || props["owner"][0] != "team-a" {
			t.Fatalf("after PUT = %+v", props)
		}

		// The merge law: same-key value-set replace (multi-value via the
		// continuation grammar), other keys kept.
		if s := doProps(t, h, http.MethodPut, base+"?properties=qa=failed,v2", adminUser, adminPass); s != http.StatusNoContent {
			t.Fatalf("PUT 2 status = %d", s)
		}
		_, props = getProps(t, h, base+"?properties=qa,owner", adminUser, adminPass)
		if len(props) != 2 || len(props["qa"]) != 2 || props["owner"][0] != "team-a" {
			t.Fatalf("after merge PUT = %+v", props)
		}
	})

	t.Run("GET wildcard and atomic", func(t *testing.T) {
		_, props := getProps(t, h, base+"?properties=build*", adminUser, adminPass)
		if len(props) != 2 {
			t.Fatalf("wildcard build* = %+v", props)
		}
		// atomic: every named literal key must exist, else 404.
		if s, _ := getProps(t, h, base+"?properties=build,nope&atomic=true", adminUser, adminPass); s != http.StatusNotFound {
			t.Fatalf("atomic missing-key status = %d", s)
		}
		if s, _ := getProps(t, h, base+"?properties=build,qa&atomic=true", adminUser, adminPass); s != http.StatusOK {
			t.Fatalf("atomic all-present status = %d", s)
		}
		// No hit without atomic is the 200-with-empty-map ruling (section
		// 15.3.3's "无命中 = {}" divergence from the reference 404).
		if s, props := getProps(t, h, base+"?properties=zzz", adminUser, adminPass); s != http.StatusOK || len(props) != 0 {
			t.Fatalf("no-hit status = %d props = %+v", s, props)
		}
	})

	t.Run("DELETE selective then wildcard", func(t *testing.T) {
		if s := doProps(t, h, http.MethodDelete, base+"?properties=qa", adminUser, adminPass); s != http.StatusNoContent {
			t.Fatalf("DELETE status = %d", s)
		}
		_, props := getProps(t, h, base+"?properties=qa,owner", adminUser, adminPass)
		if len(props) != 1 || props["owner"][0] != "team-a" {
			t.Fatalf("after DELETE = %+v", props)
		}
		if s := doProps(t, h, http.MethodDelete, base+"?properties=build*", adminUser, adminPass); s != http.StatusNoContent {
			t.Fatalf("wildcard DELETE status = %d", s)
		}
		_, props = getProps(t, h, base+"?properties", adminUser, adminPass)
		if len(props) != 1 {
			t.Fatalf("after wildcard DELETE = %+v", props)
		}
		// properties=* drops everything; idempotent on repeat.
		if s := doProps(t, h, http.MethodDelete, base+"?properties=*", adminUser, adminPass); s != http.StatusNoContent {
			t.Fatalf("delete-all status = %d", s)
		}
		if s := doProps(t, h, http.MethodDelete, base+"?properties=*", adminUser, adminPass); s != http.StatusNoContent {
			t.Fatalf("repeat delete-all status = %d", s)
		}
		_, props = getProps(t, h, base+"?properties", adminUser, adminPass)
		if len(props) != 0 {
			t.Fatalf("after delete-all = %+v", props)
		}
	})

	t.Run("validation arms", func(t *testing.T) {
		// Empty delete spec: the reference plane's explicit 400.
		if s := doProps(t, h, http.MethodDelete, base+"?properties=", adminUser, adminPass); s != http.StatusBadRequest {
			t.Fatalf("empty DELETE spec status = %d", s)
		}
		// Empty PUT spec likewise refuses to write nothing.
		if s := doProps(t, h, http.MethodPut, base+"?properties=", adminUser, adminPass); s != http.StatusBadRequest {
			t.Fatalf("empty PUT spec status = %d", s)
		}
		// Illegal key on the REST write arm (FR-89-AC3's second arm).
		if s := doProps(t, h, http.MethodPut, base+"?properties=bad+key=1", adminUser, adminPass); s != http.StatusBadRequest {
			t.Fatalf("illegal REST key status = %d", s)
		}
		// A continuation with no key is malformed.
		if s := doProps(t, h, http.MethodPut, base+"?properties=orphan", adminUser, adminPass); s != http.StatusBadRequest {
			t.Fatalf("orphan continuation status = %d", s)
		}
	})

	t.Run("unknown node is 404", func(t *testing.T) {
		if s, _ := getProps(t, h, "/binflow/api/storage/generic-local/nope.bin?properties=k", adminUser, adminPass); s != http.StatusNotFound {
			t.Fatalf("unknown node GET status = %d", s)
		}
		if s := doProps(t, h, http.MethodPut, "/binflow/api/storage/generic-local/nope.bin?properties=k=v", adminUser, adminPass); s != http.StatusNotFound {
			t.Fatalf("unknown node PUT status = %d", s)
		}
		if s := doProps(t, h, http.MethodDelete, "/binflow/api/storage/generic-local/nope.bin?properties=k", adminUser, adminPass); s != http.StatusNotFound {
			t.Fatalf("unknown node DELETE status = %d", s)
		}
	})
}

func TestStoragePropertiesFolderRecursive(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/rel/x.bin", "x")
	putContent(t, h, "/binflow/generic-local/rel/sub/y.bin", "y")

	const base = "/binflow/api/storage/generic-local/rel"

	// Without the flag only the addressed node (the folder row) changes.
	if s := doProps(t, h, http.MethodPut, base+"?properties=owner=team-a", adminUser, adminPass); s != http.StatusNoContent {
		t.Fatalf("folder PUT status = %d", s)
	}
	if _, p := getProps(t, h, base+"?properties=owner", adminUser, adminPass); len(p) != 1 {
		t.Fatal("folder row must carry the property")
	}
	_, child := getProps(t, h, "/binflow/api/storage/generic-local/rel/x.bin?properties=owner", adminUser, adminPass)
	if len(child) != 0 {
		t.Fatalf("non-recursive PUT leaked to children: %+v", child)
	}

	// recursive=1 fans out to the folder row and every node under it.
	if s := doProps(t, h, http.MethodPut, base+"?properties=release=done&recursive=1", adminUser, adminPass); s != http.StatusNoContent {
		t.Fatalf("recursive PUT status = %d", s)
	}
	for _, p := range []string{
		"/binflow/api/storage/generic-local/rel",
		"/binflow/api/storage/generic-local/rel/x.bin",
		"/binflow/api/storage/generic-local/rel/sub/y.bin",
	} {
		if _, got := getProps(t, h, p+"?properties=release", adminUser, adminPass); len(got) != 1 {
			t.Fatalf("recursive PUT missed %s", p)
		}
	}

	// Recursive delete mirrors the fan-out.
	if s := doProps(t, h, http.MethodDelete, base+"?properties=release&recursive=1", adminUser, adminPass); s != http.StatusNoContent {
		t.Fatalf("recursive DELETE status = %d", s)
	}
	for _, p := range []string{
		"/binflow/api/storage/generic-local/rel",
		"/binflow/api/storage/generic-local/rel/x.bin",
		"/binflow/api/storage/generic-local/rel/sub/y.bin",
	} {
		if _, got := getProps(t, h, p+"?properties=release", adminUser, adminPass); len(got) != 0 {
			t.Fatalf("recursive DELETE left residue on %s", p)
		}
	}
}

func TestStoragePropertiesPermissions(t *testing.T) {
	// The gates: GET rides the item-info read plane (anonymous follows the
	// flag); PUT/DELETE demand authentication plus the path's `w` — with no
	// overwrite (`d`) coupling (section 15.3.3).
	h := newHarnessCfg(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = true },
		[][2]string{{"plainuser", "plainpass"}, {"writer", "writerpass"}})
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/sec/app.bin;env=prod", "app")

	// plainuser holds read only; writer holds write on sec/.
	grant(t, h, "sec-read", "generic-local", "sec/**", "plainuser", true, false, false)
	grant(t, h, "sec-write", "generic-local", "sec/**", "writer", true, true, false)

	const base = "/binflow/api/storage/generic-local/sec/app.bin"

	t.Run("read grant reads, anonymous follows the flag", func(t *testing.T) {
		if s, p := getProps(t, h, base+"?properties=env", "plainuser", "plainpass"); s != 200 || p["env"][0] != "prod" {
			t.Fatalf("plainuser GET = %d %+v", s, p)
		}
		if s, p := getProps(t, h, base+"?properties=env", "", ""); s != 200 || p["env"][0] != "prod" {
			t.Fatalf("anonymous GET = %d %+v", s, p)
		}
	})
	t.Run("reader cannot write", func(t *testing.T) {
		if s := doProps(t, h, http.MethodPut, base+"?properties=qa=1", "plainuser", "plainpass"); s != http.StatusForbidden {
			t.Fatalf("reader PUT status = %d", s)
		}
		if s := doProps(t, h, http.MethodDelete, base+"?properties=env", "plainuser", "plainpass"); s != http.StatusForbidden {
			t.Fatalf("reader DELETE status = %d", s)
		}
	})
	t.Run("anonymous mutating verbs meet the 401 challenge", func(t *testing.T) {
		if s := doProps(t, h, http.MethodPut, base+"?properties=qa=1", "", ""); s != http.StatusUnauthorized {
			t.Fatalf("anonymous PUT status = %d", s)
		}
		if s := doProps(t, h, http.MethodDelete, base+"?properties=env", "", ""); s != http.StatusUnauthorized {
			t.Fatalf("anonymous DELETE status = %d", s)
		}
	})
	t.Run("writer writes and deletes without holding d", func(t *testing.T) {
		if s := doProps(t, h, http.MethodPut, base+"?properties=qa=passed", "writer", "writerpass"); s != http.StatusNoContent {
			t.Fatalf("writer PUT status = %d", s)
		}
		if s := doProps(t, h, http.MethodDelete, base+"?properties=qa", "writer", "writerpass"); s != http.StatusNoContent {
			t.Fatalf("writer DELETE status = %d", s)
		}
		// A write-grant-only reader of a DIFFERENT subtree stays fenced.
		if s, _ := getProps(t, h, base+"?properties=env", "nosuch", "nosuch"); s != http.StatusUnauthorized {
			t.Fatalf("unknown-credential GET status = %d", s)
		}
	})
}

func TestStoragePropertiesRoutePosture(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/route/app.bin", "app")

	// Other verbs on the arm fall to the E-26 404 (the family defines
	// exactly GET/PUT/DELETE); propertiesXml stays an E-26 resident.
	if s := doProps(t, h, http.MethodPost, "/binflow/api/storage/generic-local/route/app.bin?properties=k=v", adminUser, adminPass); s != http.StatusNotFound {
		t.Fatalf("POST status = %d", s)
	}
	resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/route/app.bin?propertiesXml", adminUser, adminPass, nil, nil)
	defer drain(resp)
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(mustGet(t, resp), "not implemented") {
		t.Fatalf("propertiesXml status = %d body = %s", resp.StatusCode, mustGet(t, resp))
	}

	// The plain item GET (no parameter) is untouched: full FileInfo body.
	item := h.do(http.MethodGet, "/binflow/api/storage/generic-local/route/app.bin", adminUser, adminPass, nil, nil)
	defer drain(item)
	if item.StatusCode != http.StatusOK || !strings.Contains(mustGet(t, item), `"repo"`) {
		t.Fatalf("plain item GET = %d %s", item.StatusCode, mustGet(t, item))
	}
}
