package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The T-15 compatible-plane suite, part 1: repository CRUD (E-04..E-08) and
// /api/storage item info (E-09/E-10). Every case runs against the full
// harness stack (real router + middleware + services).

// putRepo issues the create-repository request and returns the response.
func putRepo(t *testing.T, h *harness, key, body string) *http.Response {
	t.Helper()
	return h.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass,
		[]byte(body), map[string]string{"Content-Type": "application/json"})
}

// TestRepositoriesCRUD walks C03 -> C04 -> C05 -> C06 -> C19 plus the
// remote/virtual rejection (C26): create 200 plain text, invalid key 400,
// list fields/ordering/no-store, single-repo config, delete semantics.
func TestRepositoriesCRUD(t *testing.T) {
	h := newHarness(t)

	t.Run("C03 create is 200 plain text", func(t *testing.T) {
		resp := putRepo(t, h, "generic-local",
			`{"rclass":"local","packageType":"generic","description":"M1 QA"}`)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		if strings.TrimSpace(body) != "Successfully created repository 'generic-local'" {
			t.Fatalf("body = %q, want the created wording", body)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
			t.Fatalf("content-type = %q", ct)
		}
	})

	t.Run("C03 repeat create is 200 update wording", func(t *testing.T) {
		resp := putRepo(t, h, "generic-local", `{"rclass":"local","packageType":"generic"}`)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		if !strings.Contains(body, "update successfully") {
			t.Fatalf("body = %q, want the update wording", body)
		}
	})

	t.Run("C04 invalid key is 400 envelope", func(t *testing.T) {
		resp := putRepo(t, h, "Bad_Key!", `{"rclass":"local","packageType":"generic"}`)
		eb := decodeError(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if eb.Errors[0].Status != http.StatusBadRequest {
			t.Fatalf("envelope status = %d", eb.Errors[0].Status)
		}
	})

	t.Run("C26 remote rclass is 400", func(t *testing.T) {
		resp := putRepo(t, h, "docker-remote",
			`{"rclass":"remote","packageType":"docker","url":"https://registry-1.docker.io"}`)
		eb := decodeError(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, eb.Errors[0].Message)
		}
		if !strings.Contains(eb.Errors[0].Message, "M3") {
			t.Fatalf("message = %q, want the supported-from-M3 wording", eb.Errors[0].Message)
		}
	})

	t.Run("C26 virtual rclass is 400", func(t *testing.T) {
		resp := putRepo(t, h, "aggregated", `{"rclass":"virtual","packageType":"generic"}`)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		decodeError(t, resp)
	})

	t.Run("C05 list fields and ordering", func(t *testing.T) {
		// A second repo sorts after the first by key.
		if resp := putRepo(t, h, "another-local", `{"rclass":"local","packageType":"generic"}`); resp.StatusCode != http.StatusOK {
			t.Fatalf("seed second repo: %d", resp.StatusCode)
		} else {
			_ = resp.Body.Close()
		}
		resp := h.do(http.MethodGet, "/binflow/api/repositories", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
			t.Fatalf("Cache-Control = %q, want no-store", cc)
		}
		var items []struct {
			Key         string `json:"key"`
			Description string `json:"description"`
			Type        string `json:"type"`
			PackageType string `json:"packageType"`
			URL         string `json:"url"`
		}
		if err := json.Unmarshal([]byte(body), &items); err != nil {
			t.Fatalf("list body %q: %v", body, err)
		}
		if len(items) != 2 {
			t.Fatalf("len = %d, want 2; body=%s", len(items), body)
		}
		if items[0].Key != "another-local" || items[1].Key != "generic-local" {
			t.Fatalf("order = [%s %s], want key-ascending", items[0].Key, items[1].Key)
		}
		for _, it := range items {
			if it.Type != "local" || it.PackageType != "generic" || it.URL == "" {
				t.Fatalf("entry %+v missing type/packageType/url", it)
			}
		}
	})

	t.Run("C06 single repo config", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/repositories/generic-local", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		var cfg struct {
			Key         string `json:"key"`
			RClass      string `json:"rclass"`
			PackageType string `json:"packageType"`
		}
		if err := json.Unmarshal([]byte(body), &cfg); err != nil {
			t.Fatalf("config body %q: %v", body, err)
		}
		if cfg.RClass != "local" || cfg.PackageType != "generic" || cfg.Key != "generic-local" {
			t.Fatalf("config = %+v", cfg)
		}
	})

	t.Run("unknown repo get is 404 envelope", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/repositories/no-such", adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		decodeError(t, resp)
	})

	t.Run("C19 delete semantics", func(t *testing.T) {
		// Empty repo deletes directly.
		resp := h.do(http.MethodDelete, "/binflow/api/repositories/another-local", adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("empty delete status = %d; body=%s", resp.StatusCode, mustGet(t, resp))
		}
		_ = resp.Body.Close()

		// Non-empty without the flag -> 400 naming deleteContent.
		if resp := h.do(http.MethodPut, "/binflow/generic-local/acme/x.bin", adminUser, adminPass, []byte("x"), nil); resp.StatusCode != http.StatusCreated {
			t.Fatalf("seed node: %d", resp.StatusCode)
		} else {
			_ = resp.Body.Close()
		}
		resp = h.do(http.MethodDelete, "/binflow/api/repositories/generic-local", adminUser, adminPass, nil, nil)
		eb := decodeError(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("non-empty delete status = %d", resp.StatusCode)
		}
		if !strings.Contains(eb.Errors[0].Message, "deleteContent") {
			t.Fatalf("message = %q, want the deleteContent hint", eb.Errors[0].Message)
		}

		// With the flag -> 2xx and the node is gone.
		resp = h.do(http.MethodDelete, "/binflow/api/repositories/generic-local?deleteContent=true", adminUser, adminPass, nil, nil)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			t.Fatalf("forced delete status = %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
		resp = h.do(http.MethodGet, "/binflow/generic-local/acme/x.bin", adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("node after repo delete = %d, want 404", resp.StatusCode)
		}
		_ = resp.Body.Close()
	})
}

// TestRepositoriesAuthMatrix: the management plane demands authentication,
// and mutations additionally demand admin.
func TestRepositoriesAuthMatrix(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	tests := []struct {
		name       string
		method     string
		path       string
		user, pass string
		want       int
	}{
		{"anonymous list is 401", http.MethodGet, "/binflow/api/repositories", "", "", http.StatusUnauthorized},
		{"anonymous create is 401", http.MethodPut, "/binflow/api/repositories/new-repo", "", "", http.StatusUnauthorized},
		{"non-admin list is 200", http.MethodGet, "/binflow/api/repositories", "ci-bot", "ci-pw", http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h2 := h
			if tc.user == "ci-bot" {
				h2 = newHarnessCfg(t, nil, [][2]string{{"ci-bot", "ci-pw"}})
				seedRepo(t, h2, "generic-local")
			}
			var body []byte
			if tc.method == http.MethodPut {
				body = []byte(`{"rclass":"local","packageType":"generic"}`)
			}
			resp := h2.do(tc.method, tc.path, tc.user, tc.pass, body,
				map[string]string{"Content-Type": "application/json"})
			_ = mustGet(t, resp)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}

	t.Run("non-admin create is 403", func(t *testing.T) {
		h3 := newHarnessCfg(t, nil, [][2]string{{"ci-bot", "ci-pw"}})
		resp := h3.do(http.MethodPut, "/binflow/api/repositories/new-repo", "ci-bot", "ci-pw",
			[]byte(`{"rclass":"local","packageType":"generic"}`),
			map[string]string{"Content-Type": "application/json"})
		_ = mustGet(t, resp)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
	})
	t.Run("non-admin delete is 403", func(t *testing.T) {
		h4 := newHarnessCfg(t, nil, [][2]string{{"ci-bot", "ci-pw"}})
		seedRepo(t, h4, "generic-local")
		resp := h4.do(http.MethodDelete, "/binflow/api/repositories/generic-local", "ci-bot", "ci-pw", nil, nil)
		_ = mustGet(t, resp)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
	})
}

// TestStorageItemInfo walks C10/C16 semantics: the full FileInfo field set
// on a file (size as STRING, ISO8601-millis timestamps, checksums and
// originalChecksums triples), FolderInfo with sorted children, and the
// unimplemented query arms' 404.
func TestStorageItemInfo(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	content := []byte("artifact-body-bytes")
	sum := sha256Hex(content)
	// The folder row must exist for FolderInfo lookups (nodes are not
	// auto-materialized for implicit parents, only explicit mkdir rows are).
	resp := h.do(http.MethodPut, "/binflow/generic-local/acme/", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("mkdir acme status = %d; body=%s", resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()
	resp = h.do(http.MethodPut, "/binflow/generic-local/acme/artifact.bin", adminUser, adminPass, content, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT status = %d; body=%s", resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()
	// A second file under the same folder to exercise children ordering.
	resp = h.do(http.MethodPut, "/binflow/generic-local/acme/a-first.bin", adminUser, adminPass, []byte("a"), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT 2 status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	// A nested folder to mark acme's child as a folder entry.
	resp = h.do(http.MethodPut, "/binflow/generic-local/acme/sub/", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("mkdir status = %d; body=%s", resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()

	t.Run("C10 file info field set", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/acme/artifact.bin", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(body), &raw); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		// FR-3-AC6 high-confidence field set.
		for _, field := range []string{
			"uri", "downloadUri", "repo", "path", "created", "createdBy",
			"lastModified", "modifiedBy", "lastUpdated", "size", "mimeType",
			"checksums", "originalChecksums",
		} {
			if _, ok := raw[field]; !ok {
				t.Fatalf("field %q missing; body=%s", field, body)
			}
		}
		if _, ok := raw["size"].(string); !ok {
			t.Fatalf("size is %T, want a string (raw JSON: %v)", raw["size"], raw["size"])
		}
		if raw["size"] != fmt.Sprint(len(content)) {
			t.Fatalf("size = %v, want %d", raw["size"], len(content))
		}
		if raw["repo"] != "generic-local" || raw["path"] != "/acme/artifact.bin" {
			t.Fatalf("repo/path = %v/%v", raw["repo"], raw["path"])
		}
		sums := raw["checksums"].(map[string]any)
		if sums["sha256"] != sum {
			t.Fatalf("checksums.sha256 = %v, want %s", sums["sha256"], sum)
		}
		for _, field := range []string{"created", "lastModified", "lastUpdated"} {
			v, _ := raw[field].(string)
			if !strings.Contains(v, ".") || !strings.Contains(v, "+") && !strings.Contains(v, "Z") {
				t.Fatalf("%s = %q, want ISO8601 with millis and zone", field, v)
			}
		}
	})

	t.Run("C16 folder info with sorted children", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/acme", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		var fi struct {
			Repo     string `json:"repo"`
			Path     string `json:"path"`
			Children []struct {
				URI    string `json:"uri"`
				Folder bool   `json:"folder"`
			} `json:"children"`
		}
		if err := json.Unmarshal([]byte(body), &fi); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if fi.Path != "/acme" && fi.Path != "/acme/" {
			t.Fatalf("path = %q", fi.Path)
		}
		var names []string
		folders := map[string]bool{}
		for _, c := range fi.Children {
			names = append(names, strings.TrimPrefix(c.URI, "/"))
			folders[strings.TrimPrefix(c.URI, "/")] = c.Folder
		}
		want := []string{"a-first.bin", "artifact.bin", "sub"}
		if fmt.Sprint(names) != fmt.Sprint(want) {
			t.Fatalf("children = %v, want %v (sorted, folder flags %v)", names, want, folders)
		}
		if !folders["sub"] {
			t.Fatalf("sub not flagged as folder: %v", folders)
		}
		if folders["artifact.bin"] {
			t.Fatalf("artifact.bin flagged as folder: %v", folders)
		}
	})

	t.Run("repository root folder", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		var fi struct {
			Children []struct {
				URI    string `json:"uri"`
				Folder bool   `json:"folder"`
			} `json:"children"`
		}
		if err := json.Unmarshal([]byte(body), &fi); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if len(fi.Children) != 1 || fi.Children[0].URI != "/acme" || !fi.Children[0].Folder {
			t.Fatalf("root children = %+v", fi.Children)
		}
	})

	t.Run("unknown item is 404 envelope", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/nope/x.bin", adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		decodeError(t, resp)
	})

	t.Run("unimplemented query arms are E-26 404", func(t *testing.T) {
		for _, q := range []string{"properties", "stats", "lastModified", "permissions"} {
			resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/acme/artifact.bin?"+q, adminUser, adminPass, nil, nil)
			eb := decodeError(t, resp)
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("?%s status = %d", q, resp.StatusCode)
			}
			if !strings.Contains(eb.Errors[0].Message, "not implemented") {
				t.Fatalf("?%s message = %q", q, eb.Errors[0].Message)
			}
		}
	})
}

// TestStorageList (E-10, C17): the ?list rejection ladder (anonymous 403,
// root 400, file target 400) and the deep listing's relative uris.
func TestStorageList(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	resp := h.do(http.MethodPut, "/binflow/generic-local/acme/", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("mkdir acme status = %d; body=%s", resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()
	for _, p := range []string{"acme/a.bin", "acme/sub/deep.bin"} {
		resp := h.do(http.MethodPut, "/binflow/generic-local/"+p, adminUser, adminPass, []byte(p), nil)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("PUT %s: %d", p, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	t.Run("anonymous is 403", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/acme?list&deep=1", "", "", nil, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
		decodeError(t, resp)
	})

	t.Run("root is 400 with the spec wording", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local?list&deep=1", adminUser, adminPass, nil, nil)
		eb := decodeError(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if eb.Errors[0].Message != "Cannot list files of root." {
			t.Fatalf("message = %q", eb.Errors[0].Message)
		}
	})

	t.Run("file target is 400", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/acme/a.bin?list", adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		decodeError(t, resp)
	})

	t.Run("C17 deep listing has relative uris", func(t *testing.T) {
		// The folder row for acme exists (explicit mkdir above).
		resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/acme?list&deep=1", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		var lr struct {
			Files []struct {
				URI  string `json:"uri"`
				Size int64  `json:"size"`
			} `json:"files"`
		}
		if err := json.Unmarshal([]byte(body), &lr); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		got := map[string]int64{}
		for _, f := range lr.Files {
			got[f.URI] = f.Size
		}
		if len(got) != 2 || got["a.bin"] != int64(len("acme/a.bin")) || got["sub/deep.bin"] != int64(len("acme/sub/deep.bin")) {
			t.Fatalf("files = %v, want relative a.bin and sub/deep.bin", got)
		}
	})

	t.Run("shallow depth excludes nested files", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/acme?list&depth=1", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		var lr struct {
			Files []struct {
				URI string `json:"uri"`
			} `json:"files"`
		}
		if err := json.Unmarshal([]byte(body), &lr); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		for _, f := range lr.Files {
			if strings.Contains(f.URI, "/") {
				t.Fatalf("depth=1 returned nested uri %q", f.URI)
			}
		}
	})
}
