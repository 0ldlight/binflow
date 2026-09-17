package httpapi_test

// L025-3A (D02 layout face, rest-api.md section 2.1.8): the retired
// /api/repo_layouts mount answers the plain 404 envelope; the live
// /api/admin/repolayouts serves the built-in layout list and the
// full-record single get, with the reference's verbatim 500 "No value
// present" for unknown names.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// TestRepoLayoutsRetiredMount: both old-mount spellings answer the 404
// envelope with the two recorded wordings — collection "Not Found", item
// "Not found".
func TestRepoLayoutsRetiredMount(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		path    string
		message string
	}{
		{"/binflow/api/repo_layouts", "Not Found"},
		{"/binflow/api/repo_layouts/maven-2-default", "Not found"},
	}
	for _, tt := range tests {
		resp := h.do(http.MethodGet, tt.path, adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status %d body=%s", tt.path, resp.StatusCode, body)
		}
		m := decodeJSONMap(t, body)
		errs, _ := m["errors"].([]any)
		entry, _ := errs[0].(map[string]any)
		if entry["message"] != tt.message {
			t.Errorf("%s message = %v, want %q", tt.path, entry["message"], tt.message)
		}
	}
}

// TestRepoLayoutsList: the live mount's list face — all 25 built-in rows
// (the conductor's reference capture), the {name, artifactPathPattern,
// layoutActions-object} row shape, representative patterns verbatim,
// admin-gated with the verbatim 403 "Forbidden" envelope.
func TestRepoLayoutsList(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"u1", "p1"}})

	resp := h.do(http.MethodGet, "/binflow/api/admin/repolayouts", adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body=%s", resp.StatusCode, body)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if len(items) != 25 {
		t.Fatalf("built-in layout count = %d, want 25 (body %s)", len(items), body)
	}
	byName := map[string]map[string]any{}
	for _, it := range items {
		byName[it["name"].(string)] = it
	}
	// The conductor capture's first three and last rows, verbatim.
	for name, pattern := range map[string]string{
		"maven-2-default":            "[orgPath]/[module]/[baseRev](-[folderItegRev])/[module]-[baseRev](-[fileItegRev])(-[classifier]).[ext]",
		"ivy-default":                "[org]/[module]/[baseRev](-[folderItegRev])/[type]s/[module](-[classifier])-[baseRev](-[fileItegRev]).[ext]",
		"simple-default":             "[orgPath]/[module]/[module]-[baseRev].[ext]",
		"npm-default":                "[orgPath]/-/[module]-[baseRev](-[fileItegRev]).tgz",
		"conan-default":              "[org]/[module]/[baseRev]/[channel<[^/]+>]/[folderItegRev]/(package/[package_id<[^/]+>]/[fileItegRev]/)[remainder<(?:.+)>]",
		"terraform-provider-default": "\n                [namespace]/[provider-name]/[version]/terraform-provider-[provider-name]_[version]_[os]_[arch].[ext]\n            ",
		"luarocks-default":           "[module]/[module]-[baseRev](-[fileItegRev]).[ext]",
	} {
		it, ok := byName[name]
		if !ok {
			t.Errorf("%s missing from the 25-row table", name)
			continue
		}
		if it["artifactPathPattern"] != pattern {
			t.Errorf("%s pattern = %q, want %q", name, it["artifactPathPattern"], pattern)
		}
	}
	// layoutActions is the captured object: copy-only for the fixed
	// built-ins, the full triple for the editable ones.
	for name, want := range map[string]string{
		"maven-2-default":  "map[copy:true delete:false edit:false]",
		"composer-default": "map[copy:true delete:true edit:true]",
		"build-default":    "map[copy:true delete:true edit:true]",
		"go-default":       "map[copy:true delete:false edit:false]",
	} {
		if got := fmt.Sprint(byName[name]["layoutActions"]); got != want {
			t.Errorf("%s layoutActions = %v, want %s", name, byName[name]["layoutActions"], want)
		}
	}
	for _, k := range []string{"name", "artifactPathPattern", "layoutActions"} {
		if _, ok := byName["maven-2-default"][k]; !ok {
			t.Errorf("list row lacks %q", k)
		}
	}

	// The non-admin arm.
	resp = h.do(http.MethodGet, "/binflow/api/admin/repolayouts", "u1", "p1", nil, nil)
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin status %d body=%s", resp.StatusCode, body)
	}
	assertForbiddenEnvelope(t, body)
}

// TestRepoLayoutGetDetail: the single-get face — the captured six/seven-key
// shape (boolean distinctiveDescriptorPathPattern, descriptorPathPattern
// only on the descriptor layouts, both regexps always present, no
// layoutActions), the usage-derived association block, and the verbatim
// 500 "No value present" for unknown names (the reference's unguarded
// Optional.get — its own capture shows gradle-default/pypi-default/…
// answering the same 500).
func TestRepoLayoutGetDetail(t *testing.T) {
	h := newHarness(t)

	tests := []struct {
		name string
		want map[string]any // the captured single-read wire rows
	}{
		{"maven-2-default", map[string]any{
			"name":                             "maven-2-default",
			"artifactPathPattern":              "[orgPath]/[module]/[baseRev](-[folderItegRev])/[module]-[baseRev](-[fileItegRev])(-[classifier]).[ext]",
			"distinctiveDescriptorPathPattern": true,
			"descriptorPathPattern":            "[orgPath]/[module]/[baseRev](-[folderItegRev])/[module]-[baseRev](-[fileItegRev])(-[classifier]).pom",
			"folderIntegrationRevisionRegExp":  "SNAPSHOT",
			"fileIntegrationRevisionRegExp":    "SNAPSHOT|(?:(?:[0-9]{8}.[0-9]{6})-(?:[0-9]+))",
			"repositoryAssociations":           map[string]any{"localRepositories": []any{}, "remoteRepositories": []any{}, "virtualRepositories": []any{}},
		}},
		{"ivy-default", map[string]any{
			"name":                             "ivy-default",
			"artifactPathPattern":              "[org]/[module]/[baseRev](-[folderItegRev])/[type]s/[module](-[classifier])-[baseRev](-[fileItegRev]).[ext]",
			"distinctiveDescriptorPathPattern": true,
			"descriptorPathPattern":            "[org]/[module]/[baseRev](-[folderItegRev])/[type]s/ivy-[baseRev](-[fileItegRev]).xml",
			"folderIntegrationRevisionRegExp":  `\d{14}`,
			"fileIntegrationRevisionRegExp":    `\d{14}`,
			"repositoryAssociations":           map[string]any{"localRepositories": []any{}, "remoteRepositories": []any{}, "virtualRepositories": []any{}},
		}},
		{"vcs-default", map[string]any{
			"name":                             "vcs-default",
			"artifactPathPattern":              "[orgPath]/[module]/[refs<tags|branches>]/[baseRev]/[module]-[baseRev](-[fileItegRev])(-[classifier]).[ext]",
			"distinctiveDescriptorPathPattern": false,
			"folderIntegrationRevisionRegExp":  ".*",
			"fileIntegrationRevisionRegExp":    "[a-zA-Z0-9]{40}",
			"repositoryAssociations":           map[string]any{"localRepositories": []any{}, "remoteRepositories": []any{}, "virtualRepositories": []any{}},
		}},
		{"npm-default", map[string]any{
			"name":                             "npm-default",
			"artifactPathPattern":              "[orgPath]/-/[module]-[baseRev](-[fileItegRev]).tgz",
			"distinctiveDescriptorPathPattern": false,
			"folderIntegrationRevisionRegExp":  ".*",
			"fileIntegrationRevisionRegExp":    ".*",
			"repositoryAssociations":           map[string]any{"localRepositories": []any{}, "remoteRepositories": []any{}, "virtualRepositories": []any{}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.do(http.MethodGet, "/binflow/api/admin/repolayouts/"+tt.name, adminUser, adminPass, nil, nil)
			body := mustGet(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d body=%s", resp.StatusCode, body)
			}
			m := decodeJSONMap(t, body)
			if compactJSON(t, body) != compactJSON(t, mustJSON(t, tt.want)) {
				t.Errorf("single-read = %s, want the captured wire row %v", body, tt.want)
			}
			if _, has := m["layoutActions"]; has {
				t.Errorf("single-read must not carry layoutActions: %s", body)
			}
		})
	}

	t.Run("associations follow repoLayoutRef usage", func(t *testing.T) {
		seedConfigFamilyRepo(t, h, "example-repo-local", `{"rclass":"local","packageType":"generic","repoLayoutRef":"simple-default"}`)
		resp := h.do(http.MethodGet, "/binflow/api/admin/repolayouts/simple-default", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d body=%s", resp.StatusCode, body)
		}
		m := decodeJSONMap(t, body)
		assoc, _ := m["repositoryAssociations"].(map[string]any)
		if fmt.Sprint(assoc["localRepositories"]) != "[example-repo-local]" {
			t.Errorf("simple-default localRepositories = %v, want [example-repo-local] (the capture's own usage-derived row)", assoc["localRepositories"])
		}
		// An unrelated layout stays unassociated.
		resp = h.do(http.MethodGet, "/binflow/api/admin/repolayouts/npm-default", adminUser, adminPass, nil, nil)
		m = decodeJSONMap(t, mustGet(t, resp))
		assoc, _ = m["repositoryAssociations"].(map[string]any)
		if fmt.Sprint(assoc["localRepositories"]) != "[]" {
			t.Errorf("npm-default localRepositories = %v, want empty", assoc["localRepositories"])
		}
	})

	t.Run("unknown name 500 verbatim", func(t *testing.T) {
		for _, name := range []string{"no-such-layout", "gradle-default", "pypi-default"} {
			resp := h.do(http.MethodGet, "/binflow/api/admin/repolayouts/"+name, adminUser, adminPass, nil, nil)
			body := mustGet(t, resp)
			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("%s status %d body=%s", name, resp.StatusCode, body)
			}
			m := decodeJSONMap(t, body)
			errs, _ := m["errors"].([]any)
			entry, _ := errs[0].(map[string]any)
			if entry["message"] != "No value present" {
				t.Errorf("%s message = %v, want %q", name, entry["message"], "No value present")
			}
		}
	})
}
