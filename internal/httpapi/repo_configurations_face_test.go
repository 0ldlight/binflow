package httpapi_test

// L025-3A (D02 read family, rest-api.md sections 2.1.1/2.1.2): the grouped
// all-configurations inventory and the project x type existence probe —
// grouping/filters/error texts pinned verbatim against the spec.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// seedConfigFamilyRepo creates one repository over the real REST plane.
func seedConfigFamilyRepo(t *testing.T, h *harness, key, body string) {
	t.Helper()
	resp := putRepo(t, h, key, body)
	if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusOK {
		t.Fatalf("seed %s: status %d body=%s", key, code, out)
	}
}

func decodeJSONMap(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	return m
}

// TestRepoConfigurationsGroupedInventory: admin read groups by uppercase
// class, sorts by key, carries rclass lowercase and the vendor wire
// headers; both filter axes stack as comma-OR; a no-match filter answers
// 200 {} (not an error).
func TestRepoConfigurationsGroupedInventory(t *testing.T) {
	h := newHarness(t)
	seedConfigFamilyRepo(t, h, "zlocal", `{"rclass":"local","packageType":"generic","description":"z first","blackedOut":true,"repoLayoutRef":"simple-default","environments":["DEV"]}`)
	seedConfigFamilyRepo(t, h, "alocal", `{"rclass":"local","packageType":"maven","description":"a second"}`)
	seedConfigFamilyRepo(t, h, "rem", `{"rclass":"remote","packageType":"generic","url":"https://example.com/up"}`)
	seedConfigFamilyRepo(t, h, "virt", `{"rclass":"virtual","packageType":"generic","repositories":["alocal"]}`)

	tests := []struct {
		name       string
		query      string
		wantKeys   []string // top-level group keys, in order
		wantCounts map[string]int
	}{
		{"no filter", "", []string{"LOCAL", "REMOTE", "VIRTUAL"}, map[string]int{"LOCAL": 2, "REMOTE": 1, "VIRTUAL": 1}},
		{"packageType comma OR", "?packageType=generic,maven", []string{"LOCAL", "REMOTE", "VIRTUAL"}, map[string]int{"LOCAL": 2, "REMOTE": 1, "VIRTUAL": 1}},
		{"packageType single", "?packageType=maven", []string{"LOCAL"}, map[string]int{"LOCAL": 1}},
		{"repoType comma OR", "?repoType=local,remote", []string{"LOCAL", "REMOTE"}, map[string]int{"LOCAL": 2, "REMOTE": 1}},
		{"both axes stack", "?packageType=generic&repoType=remote", []string{"REMOTE"}, map[string]int{"REMOTE": 1}},
		{"no match empty object", "?packageType=nosuchtype", nil, nil},
		{"unknown repoType empty object", "?repoType=bogus", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.do(http.MethodGet, "/binflow/api/repositories/configurations"+tt.query, adminUser, adminPass, nil, nil)
			body := mustGet(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d body=%s", resp.StatusCode, body)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "application/vnd.org.jfrog.artifactory.repositories.RepositoryConfigurationsList+json" {
				t.Errorf("Content-Type = %q", ct)
			}
			if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", cc)
			}
			m := decodeJSONMap(t, body)
			if tt.wantKeys == nil {
				if len(m) != 0 {
					t.Fatalf("want empty object {}, got %s", body)
				}
				return
			}
			if fmt.Sprint(groupKeysOf(m)) != fmt.Sprint(tt.wantKeys) {
				// map decode loses order; compare as sets + counts.
				if len(m) != len(tt.wantKeys) {
					t.Fatalf("groups = %v, want %v (body %s)", groupKeysOf(m), tt.wantKeys, body)
				}
				for _, k := range tt.wantKeys {
					if _, ok := m[k]; !ok {
						t.Fatalf("groups = %v, want %v", groupKeysOf(m), tt.wantKeys)
					}
				}
			}
			for group, want := range tt.wantCounts {
				g, ok := m[group].([]any)
				if !ok || len(g) != want {
					t.Errorf("group %s: %d entries, want %d (body %s)", group, len(g), want, body)
				}
			}
		})
	}

	// The LOCAL group renders sorted by key with the modeled field subset.
	t.Run("local group sorted with common fields", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/repositories/configurations?repoType=local", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		var top struct {
			LOCAL []map[string]any `json:"LOCAL"`
		}
		if err := json.Unmarshal([]byte(body), &top); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if len(top.LOCAL) != 2 || top.LOCAL[0]["key"] != "alocal" || top.LOCAL[1]["key"] != "zlocal" {
			t.Fatalf("LOCAL order = %v", body)
		}
		z := top.LOCAL[1]
		if z["rclass"] != "local" || z["packageType"] != "generic" || z["description"] != "z first" {
			t.Errorf("zlocal common fields = %v", z)
		}
		if z["blackedOut"] != true || z["repoLayoutRef"] != "simple-default" {
			t.Errorf("zlocal blob fields = %v", z)
		}
		if fmt.Sprint(z["environments"]) != "[DEV]" {
			t.Errorf("zlocal environments = %v", z["environments"])
		}
		if _, has := z["url"]; has {
			t.Errorf("configurations entry must not carry url: %v", z)
		}
		if _, has := z["type"]; has {
			t.Errorf("configurations entry uses rclass, not type: %v", z)
		}
	})
}

func groupKeysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestRepoConfigurationsForbidden: the non-admin arm answers the spec's
// verbatim 403 "Forbidden" envelope; anonymous answers the 401 challenge.
func TestRepoConfigurationsForbidden(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"u1", "p1"}})

	resp := h.do(http.MethodGet, "/binflow/api/repositories/configurations", "u1", "p1", nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin status %d body=%s", resp.StatusCode, body)
	}
	assertForbiddenEnvelope(t, body)

	resp = h.do(http.MethodGet, "/binflow/api/repositories/configurations", "", "", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status %d", resp.StatusCode)
	}
}

// TestRepoExistenceProbe: type validation (repeated OR, comma and unknown
// 400s verbatim), project= silently ignored, the admin passthrough echo
// for unknown projectKeys, and the non-admin 403.
func TestRepoExistenceProbe(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"u1", "p1"}})
	seedConfigFamilyRepo(t, h, "lib", `{"rclass":"local","packageType":"generic"}`)

	get := func(query string) (int, string) {
		resp := h.do(http.MethodGet, "/binflow/api/repositories/existence"+query, adminUser, adminPass, nil, nil)
		return resp.StatusCode, mustGet(t, resp)
	}

	t.Run("no type echoes all five classes", func(t *testing.T) {
		code, body := get("")
		if code != http.StatusOK {
			t.Fatalf("status %d body=%s", code, body)
		}
		m := decodeJSONMap(t, body)
		if m["exists"] != true || m["projectKey"] != "default" {
			t.Fatalf("body = %s", body)
		}
		if fmt.Sprint(m["matchingRepoTypes"]) != "[LOCAL REMOTE VIRTUAL FEDERATED RELEASE_BUNDLE]" {
			t.Errorf("matchingRepoTypes = %v", m["matchingRepoTypes"])
		}
	})

	t.Run("type filter resolves and answers", func(t *testing.T) {
		code, body := get("?type=local")
		if code != http.StatusOK {
			t.Fatalf("status %d body=%s", code, body)
		}
		m := decodeJSONMap(t, body)
		if m["exists"] != true || fmt.Sprint(m["matchingRepoTypes"]) != "[LOCAL]" {
			t.Errorf("body = %s", body)
		}
		code, body = get("?type=remote")
		m = decodeJSONMap(t, body)
		if code != http.StatusOK || m["exists"] != false || fmt.Sprint(m["matchingRepoTypes"]) != "[REMOTE]" {
			t.Errorf("remote arm body = %s", body)
		}
	})

	t.Run("repeated type ORs", func(t *testing.T) {
		code, body := get("?type=local&type=remote")
		if code != http.StatusOK {
			t.Fatalf("status %d body=%s", code, body)
		}
		m := decodeJSONMap(t, body)
		if m["exists"] != true || fmt.Sprint(m["matchingRepoTypes"]) != "[LOCAL REMOTE]" {
			t.Errorf("body = %s", body)
		}
	})

	t.Run("comma and unknown type 400 verbatim", func(t *testing.T) {
		for _, q := range []string{"?type=local,remote", "?type=bogus"} {
			code, body := get(q)
			if code != http.StatusBadRequest {
				t.Fatalf("%s status %d body=%s", q, code, body)
			}
			want := "Invalid repository type: " + q[len("?type="):]
			m := decodeJSONMap(t, body)
			errs, _ := m["errors"].([]any)
			entry, _ := errs[0].(map[string]any)
			if entry["message"] != want {
				t.Errorf("%s message = %v, want %q", q, entry["message"], want)
			}
		}
	})

	t.Run("project silently ignored, projectKey echoed", func(t *testing.T) {
		code, body := get("?project=whatever&type=local")
		if code != http.StatusOK {
			t.Fatalf("status %d body=%s", code, body)
		}
		m := decodeJSONMap(t, body)
		if m["projectKey"] != "default" {
			t.Errorf("project param must not rename projectKey: %s", body)
		}
		code, body = get("?projectKey=nonexistent")
		m = decodeJSONMap(t, body)
		if code != http.StatusOK || m["exists"] != false || m["projectKey"] != "nonexistent" {
			t.Errorf("unknown projectKey echo body = %s", body)
		}
	})

	t.Run("non-admin forbidden verbatim", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/repositories/existence?projectKey=default&type=local", "u1", "p1", nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status %d body=%s", resp.StatusCode, body)
		}
		assertForbiddenEnvelope(t, body)
	})
}

// compactJSON strips the pretty-print indentation so verbatim body
// comparisons stay readable.
func compactJSON(t *testing.T, body string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("remarshal: %v", err)
	}
	return string(out)
}

// assertForbiddenEnvelope pins the D02 read family's shared 403 body —
// exactly one errors[] entry {status:403, message:"Forbidden"}.
func assertForbiddenEnvelope(t *testing.T, body string) {
	t.Helper()
	m := decodeJSONMap(t, body)
	errs, ok := m["errors"].([]any)
	if !ok || len(errs) != 1 {
		t.Fatalf("body %s: want one errors[] entry", body)
	}
	entry, _ := errs[0].(map[string]any)
	if entry["status"] != float64(403) || entry["message"] != "Forbidden" {
		t.Errorf("403 envelope entry = %v, want {status:403 message:Forbidden}", entry)
	}
}
