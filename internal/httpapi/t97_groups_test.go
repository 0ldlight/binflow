// T-97 (FR-27, SE-01..08): the groups domain end to end — group CRUD,
// membership through the users plane, authorization inheritance with
// immediate effect, the delete guard, the effective-permission view and the
// no-admin-bit rule — all through the real HTTP stack.

package httpapi_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// putGroup creates (or replaces) one group through the REST plane.
func putGroup(t *testing.T, h *harness, name, description string) {
	t.Helper()
	resp := h.do(http.MethodPut, "/binflow/api/security/groups/"+name, adminUser, adminPass,
		[]byte(fmt.Sprintf(`{"name":%q,"description":%q}`, name, description)),
		map[string]string{"Content-Type": "application/json"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT group %s: status %d body %s", name, resp.StatusCode, mustGet(t, resp))
	}
}

// putUserWithGroups creates (or replaces) one user carrying a group set.
func putUserWithGroups(t *testing.T, h *harness, name, password string, groups []string) {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"email":"%s@example.com","password":%q,"admin":false,"groups":[%s]}`,
		name, name, password, quoteJoin(groups))
	resp := h.do(http.MethodPut, "/binflow/api/security/users/"+name, adminUser, adminPass,
		[]byte(body), map[string]string{"Content-Type": "application/json"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT user %s: status %d body %s", name, resp.StatusCode, mustGet(t, resp))
	}
}

// quoteJoin renders ["a","b"] (no trailing comma pitfalls).
func quoteJoin(vals []string) string {
	quoted := make([]string, 0, len(vals))
	for _, v := range vals {
		quoted = append(quoted, fmt.Sprintf("%q", v))
	}
	return strings.Join(quoted, ",")
}

// getUserDetail fetches one user's GET body (groups/email/...).
func getUserDetail(t *testing.T, h *harness, name string) map[string]any {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/security/users/"+name, adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET user %s: status %d", name, resp.StatusCode)
	}
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode user %s: %v", name, err)
	}
	return m
}

// postPermissionTarget creates one /api/v1/permissions target from raw JSON.
func postPermissionTarget(t *testing.T, h *harness, body string) *http.Response {
	t.Helper()
	return h.do(http.MethodPost, "/binflow/api/v1/permissions", adminUser, adminPass,
		[]byte(body), map[string]string{"Content-Type": "application/json"})
}

// putArtifact uploads body bytes as user ("" = anonymous).
func putArtifact(t *testing.T, h *harness, path, user, pass string, content []byte) int {
	t.Helper()
	resp := h.do(http.MethodPut, "/binflow/"+path, user, pass, content, nil)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// auditActions queries the audit plane for one action's events.
func auditActions(t *testing.T, h *harness, action string) []map[string]any {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/v1/audit?action="+action+"&limit=100", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit query %s: status %d", action, resp.StatusCode)
	}
	var page struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("decode audit page: %v", err)
	}
	return page.Events
}

// ---- SE-01..04: group CRUD (W17/W20) ----

func TestGroupsCRUD(t *testing.T) {
	h := newHarness(t)

	t.Run("PUT creates with 201 and no body", func(t *testing.T) {
		resp := h.do(http.MethodPut, "/binflow/api/security/groups/devs", adminUser, adminPass,
			[]byte(`{"name":"devs","description":"M4 QA"}`),
			map[string]string{"Content-Type": "application/json"})
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d body %s", resp.StatusCode, mustGet(t, resp))
		}
		if body := mustGet(t, resp); body != "" {
			t.Fatalf("body = %q, want empty", body)
		}
	})

	t.Run("list and single GET echo name/uri/description", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/security/groups", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list status = %d", resp.StatusCode)
		}
		var items []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if len(items) != 1 || items[0]["name"] != "devs" || items[0]["description"] != "M4 QA" {
			t.Fatalf("list = %+v", items)
		}
		if uri, _ := items[0]["uri"].(string); !strings.Contains(uri, "/api/security/groups/devs") {
			t.Fatalf("uri = %q", uri)
		}

		resp = h.do(http.MethodGet, "/binflow/api/security/groups/devs", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		var one map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&one); err != nil {
			t.Fatalf("decode single: %v", err)
		}
		if one["description"] != "M4 QA" {
			t.Fatalf("single = %+v", one)
		}
	})

	t.Run("PUT on existing updates with 200 (K1)", func(t *testing.T) {
		resp := h.do(http.MethodPut, "/binflow/api/security/groups/devs", adminUser, adminPass,
			[]byte(`{"name":"devs","description":"replaced"}`),
			map[string]string{"Content-Type": "application/json"})
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d body %s", resp.StatusCode, mustGet(t, resp))
		}
	})

	t.Run("POST partial-updates the description (W17)", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/binflow/api/security/groups/devs", adminUser, adminPass,
			[]byte(`{"description":"updated"}`),
			map[string]string{"Content-Type": "application/json"})
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d body %s", resp.StatusCode, mustGet(t, resp))
		}
		resp = h.do(http.MethodGet, "/binflow/api/security/groups/devs", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), `"updated"`) {
			t.Fatalf("description not round-tripped: %s", body)
		}
	})

	t.Run("POST on unknown group is 404", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/binflow/api/security/groups/ghosts", adminUser, adminPass,
			[]byte(`{"description":"x"}`), map[string]string{"Content-Type": "application/json"})
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})

	t.Run("GET on unknown group is 404", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/security/groups/ghosts", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})

	t.Run("body/path name mismatch is 400 (K1)", func(t *testing.T) {
		resp := h.do(http.MethodPut, "/binflow/api/security/groups/devs", adminUser, adminPass,
			[]byte(`{"name":"other","description":"x"}`),
			map[string]string{"Content-Type": "application/json"})
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d body %s", resp.StatusCode, mustGet(t, resp))
		}
	})

	t.Run("name rules (FR-27-AC9)", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			want int
		}{
			{"anonymous", http.StatusBadRequest},
			{"_system_", http.StatusBadRequest},
			{"Devs", http.StatusBadRequest},
			{"1devs", http.StatusBadRequest},
			{"dev team", http.StatusBadRequest},
			{strings.Repeat("d", 65), http.StatusBadRequest},
			{"d" + strings.Repeat("x", 63), http.StatusCreated}, // exactly 64 chars
			{"qa.team-2_x", http.StatusCreated},
		} {
			resp := h.do(http.MethodPut, "/binflow/api/security/groups/"+tc.name, adminUser, adminPass,
				[]byte(`{"description":"x"}`), map[string]string{"Content-Type": "application/json"})
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("group %q: status %d, want %d", tc.name, resp.StatusCode, tc.want)
			}
		}
	})

	t.Run("admin gate matrix (401/403/200)", func(t *testing.T) {
		h2 := newHarnessCfg(t, nil, [][2]string{{"ci-bot", "ci-pw"}})
		tests := []struct {
			name string
			user string
			pass string
			want int
		}{
			{"anonymous is 401", "", "", http.StatusUnauthorized},
			{"non-admin is 403", "ci-bot", "ci-pw", http.StatusForbidden},
			{"admin is 200", adminUser, adminPass, http.StatusOK},
		}
		for _, tc := range tests {
			resp := h2.do(http.MethodGet, "/binflow/api/security/groups", tc.user, tc.pass, nil, nil)
			_ = resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("%s: status %d, want %d", tc.name, resp.StatusCode, tc.want)
			}
		}
	})

	t.Run("group plane lands in the audit log", func(t *testing.T) {
		for _, action := range []string{"group.create", "group.update"} {
			if events := auditActions(t, h, action); len(events) == 0 {
				t.Fatalf("no %s events recorded", action)
			}
		}
	})
}

// ---- SE-05/06: membership through the users plane (W18/W40) ----

func TestGroupMembershipViaUsers(t *testing.T) {
	h := newHarness(t)
	putGroup(t, h, "devs", "developers")

	t.Run("PUT user with groups and email (W18/W40)", func(t *testing.T) {
		putUserWithGroups(t, h, "jane", "jane-pw", []string{"devs"})
		detail := getUserDetail(t, h, "jane")
		groups, _ := detail["groups"].([]any)
		if len(groups) != 1 || groups[0] != "devs" {
			t.Fatalf("groups = %v, want [devs]", groups)
		}
		if detail["email"] != "jane@example.com" {
			t.Fatalf("email = %v", detail["email"])
		}
		if _, leaked := detail["password"]; leaked {
			t.Fatal("user detail must never carry a password field")
		}
	})

	t.Run("unknown group is 400 with the spec wording", func(t *testing.T) {
		resp := h.do(http.MethodPut, "/binflow/api/security/users/jane2", adminUser, adminPass,
			[]byte(`{"name":"jane2","email":"j2@example.com","password":"x","groups":["nope"]}`),
			map[string]string{"Content-Type": "application/json"})
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if body := mustGet(t, resp); body != "Unable to find group by name 'nope'." {
			t.Fatalf("body = %q", body)
		}
	})

	t.Run("POST partial update clears membership with groups:[]", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/binflow/api/security/users/jane", adminUser, adminPass,
			[]byte(`{"groups":[]}`), map[string]string{"Content-Type": "application/json"})
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d body %s", resp.StatusCode, mustGet(t, resp))
		}
		detail := getUserDetail(t, h, "jane")
		if groups, _ := detail["groups"].([]any); len(groups) != 0 {
			t.Fatalf("groups = %v, want []", groups)
		}
		// Untouched fields kept their values (partial semantics).
		if detail["email"] != "jane@example.com" {
			t.Fatalf("email = %v, want untouched", detail["email"])
		}
	})

	t.Run("POST on unknown user is 404", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/binflow/api/security/users/ghost", adminUser, adminPass,
			[]byte(`{"groups":[]}`), map[string]string{"Content-Type": "application/json"})
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})

	t.Run("membership changes land as group.member events", func(t *testing.T) {
		if events := auditActions(t, h, "group.member"); len(events) == 0 {
			t.Fatal("no group.member events recorded")
		}
	})

	t.Run("list stays the simple shape (W40: 双端点分工)", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/security/users", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		var items []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, it := range items {
			for _, banned := range []string{"email", "groups", "admin", "password"} {
				if _, ok := it[banned]; ok {
					t.Fatalf("list entry carries %q: %+v", banned, it)
				}
			}
		}
	})
}

// ---- SE-07: authorization inheritance, union and immediate effect ----

func TestGroupAuthorizationInheritance(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putGroup(t, h, "devs", "developers")
	putUserWithGroups(t, h, "jane", "jane-pw", []string{"devs"})

	// W19's target: the devs group gets read+write on devs/** through the
	// groups principals column of /api/v1/permissions.
	resp := postPermissionTarget(t, h,
		`{"name":"devs-rw","repos":["generic-local"],"includePatterns":["devs/**"],`+
			`"principals":{"groups":{"devs":["read","write"]}}}`)
	if code := resp.StatusCode; code < 200 || code > 299 {
		t.Fatalf("POST permissions: status %d body %s", code, mustGet(t, resp))
	}
	_ = resp.Body.Close()

	t.Run("member inherits read+write, not delete (W19)", func(t *testing.T) {
		if code := putArtifact(t, h, "generic-local/devs/w.bin", "jane", "jane-pw", []byte("w")); code != http.StatusCreated {
			t.Fatalf("jane PUT status = %d, want 201", code)
		}
		resp := h.do(http.MethodDelete, "/binflow/generic-local/devs/w.bin", "jane", "jane-pw", nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("jane DELETE status = %d, want 403", resp.StatusCode)
		}
		if code := putArtifact(t, h, "generic-local/other/o.bin", "jane", "jane-pw", []byte("o")); code != http.StatusForbidden {
			t.Fatalf("jane PUT outside pattern status = %d, want 403", code)
		}
	})

	t.Run("membership removal is effective on the next request (W19b)", func(t *testing.T) {
		putUserWithGroups(t, h, "jane", "jane-pw", nil)
		if code := putArtifact(t, h, "generic-local/devs/w2.bin", "jane", "jane-pw", []byte("w2")); code != http.StatusForbidden {
			t.Fatalf("jane PUT after removal status = %d, want 403", code)
		}
		// Restore for the union subtests below.
		putUserWithGroups(t, h, "jane", "jane-pw", []string{"devs"})
	})

	t.Run("user and group grants union (W19c)", func(t *testing.T) {
		grant(t, h, "jane-direct", "generic-local", "**", "jane", true, false, false)
		// Direct read (any path) + group write (devs/**): GET everywhere,
		// PUT only inside the group's pattern.
		resp := h.do(http.MethodGet, "/binflow/generic-local/devs/w.bin", "jane", "jane-pw", nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("union GET status = %d", resp.StatusCode)
		}
		if code := putArtifact(t, h, "generic-local/devs/w3.bin", "jane", "jane-pw", []byte("w3")); code != http.StatusCreated {
			t.Fatalf("union PUT status = %d", code)
		}
		// A delete granted through a second group: one covering row is
		// enough (union, not intersection).
		putGroup(t, h, "cleaners", "")
		resp = h.do(http.MethodPut, "/binflow/api/security/users/jane", adminUser, adminPass,
			[]byte(`{"name":"jane","email":"jane@example.com","password":"jane-pw","admin":false,`+
				`"groups":["devs","cleaners"]}`),
			map[string]string{"Content-Type": "application/json"})
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("re-PUT jane status = %d", resp.StatusCode)
		}
		postPermissionTarget(t, h,
			`{"name":"clean-d","repos":["generic-local"],"includePatterns":["**"],`+
				`"principals":{"groups":{"cleaners":["delete"]}}}`)
		resp = h.do(http.MethodDelete, "/binflow/generic-local/devs/w3.bin", "jane", "jane-pw", nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("union DELETE status = %d, want 204", resp.StatusCode)
		}
	})

	t.Run("permission target with unknown group is 400", func(t *testing.T) {
		resp := postPermissionTarget(t, h,
			`{"name":"bad","repos":["generic-local"],"principals":{"groups":{"nope":["read"]}}}`)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d body %s", resp.StatusCode, mustGet(t, resp))
		}
	})

	t.Run("permission list echoes the groups column (W19 precondition)", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/v1/permissions", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		var targets []struct {
			Name       string `json:"name"`
			Principals struct {
				Users  map[string][]string `json:"users"`
				Groups map[string][]string `json:"groups"`
			} `json:"principals"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
			t.Fatalf("decode: %v", err)
		}
		var found bool
		for _, tg := range targets {
			if tg.Name != "devs-rw" {
				continue
			}
			found = true
			if acts := tg.Principals.Groups["devs"]; len(acts) != 2 {
				t.Fatalf("devs actions = %v, want [read write]", acts)
			}
			if len(tg.Principals.Users) != 0 {
				t.Fatalf("users column = %v, want empty", tg.Principals.Users)
			}
		}
		if !found {
			t.Fatal("devs-rw missing from the permission list")
		}
	})
}

// ---- SE-04's delete guard (W20) ----

func TestGroupDeleteProtection(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putGroup(t, h, "devs", "developers")
	putUserWithGroups(t, h, "jane", "jane-pw", []string{"devs"})
	resp := postPermissionTarget(t, h,
		`{"name":"devs-read","repos":["generic-local"],"includePatterns":["devs/**"],`+
			`"principals":{"groups":{"devs":["read","write"]}}}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST permissions: status %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	t.Run("referenced group is not deletable (409 names the target)", func(t *testing.T) {
		resp := h.do(http.MethodDelete, "/binflow/api/security/groups/devs", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("status = %d body %s", resp.StatusCode, mustGet(t, resp))
		}
		if body := mustGet(t, resp); !strings.Contains(body, "devs-read") {
			t.Fatalf("message must list the referencing target: %q", body)
		}
	})

	t.Run("unknown group is 404", func(t *testing.T) {
		resp := h.do(http.MethodDelete, "/binflow/api/security/groups/ghosts", adminUser, adminPass, nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})

	t.Run("after the target goes, deletion cascades the membership (W20)", func(t *testing.T) {
		resp := h.do(http.MethodDelete, "/binflow/api/v1/permissions/devs-read", adminUser, adminPass, nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("delete target status = %d", resp.StatusCode)
		}
		resp = h.do(http.MethodDelete, "/binflow/api/security/groups/devs", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("delete group status = %d body %s", resp.StatusCode, mustGet(t, resp))
		}
		if body := mustGet(t, resp); !strings.Contains(body, "devs") {
			t.Fatalf("plain-text body = %q", body)
		}
		detail := getUserDetail(t, h, "jane")
		if groups, _ := detail["groups"].([]any); len(groups) != 0 {
			t.Fatalf("jane groups after cascade = %v, want []", groups)
		}
		if events := auditActions(t, h, "group.delete"); len(events) == 0 {
			t.Fatal("no group.delete event recorded")
		}
	})
}

// ---- SE-08: the effective-permission view (W21) ----

func TestStoragePermissionsView(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	// A remote and a virtual repo for the 400 leg (the view is local-only,
	// rest-api.md section 3: non local/cached -> 400).
	resp := h.do(http.MethodPut, "/binflow/api/repositories/remote-x", adminUser, adminPass,
		[]byte(`{"rclass":"remote","packageType":"generic","url":"http://127.0.0.1:9099"}`),
		map[string]string{"Content-Type": "application/json"})
	_ = resp.Body.Close()
	resp = h.do(http.MethodPut, "/binflow/api/repositories/virtual-x", adminUser, adminPass,
		[]byte(`{"rclass":"virtual","packageType":"generic","repositories":["generic-local"]}`),
		map[string]string{"Content-Type": "application/json"})
	_ = resp.Body.Close()

	putGroup(t, h, "devs", "")
	putUserWithGroups(t, h, "jane", "jane-pw", []string{"devs"})
	grant(t, h, "jane-read", "generic-local", "devs/**", "jane", true, false, false)
	postPermissionTarget(t, h,
		`{"name":"devs-rw","repos":["generic-local"],"includePatterns":["devs/**"],`+
			`"principals":{"groups":{"devs":["read","write"]}}}`)
	if code := putArtifact(t, h, "generic-local/devs/w.bin", "jane", "jane-pw", []byte("w")); code != http.StatusCreated {
		t.Fatalf("seed artifact status = %d", code)
	}

	t.Run("local item answers users+groups bits (W21)", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/devs/w.bin?permissions", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d body %s", resp.StatusCode, mustGet(t, resp))
		}
		var view struct {
			URI        string `json:"uri"`
			Principals struct {
				Users  map[string][]string `json:"users"`
				Groups map[string][]string `json:"groups"`
			} `json:"principals"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !strings.Contains(view.URI, "generic-local") {
			t.Fatalf("uri = %q", view.URI)
		}
		// Orientation (T-97 review B1): key = principal name, value = its
		// permission letters — {"users":{"jane":["r"]},"groups":{"devs":["r","w"]}}.
		if got := view.Principals.Users["jane"]; len(got) != 1 || got[0] != "r" {
			t.Fatalf("users[jane] = %v, want [r]", got)
		}
		if len(view.Principals.Users) != 1 {
			t.Fatalf("users = %v, want exactly jane", view.Principals.Users)
		}
		if got := view.Principals.Groups["devs"]; len(got) != 2 || got[0] != "r" || got[1] != "w" {
			t.Fatalf("groups[devs] = %v, want [r w]", got)
		}
		if len(view.Principals.Groups) != 1 {
			t.Fatalf("groups = %v, want exactly devs", view.Principals.Groups)
		}
	})

	t.Run("non-local repositories answer 400 (P2 branch, spec-literal)", func(t *testing.T) {
		for _, key := range []string{"remote-x", "virtual-x"} {
			resp := h.do(http.MethodGet, "/binflow/api/storage/"+key+"?permissions", adminUser, adminPass, nil, nil)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("%s: status = %d, want 400", key, resp.StatusCode)
			}
		}
	})

	t.Run("unknown repo and unknown item are 404 envelope", func(t *testing.T) {
		for _, path := range []string{
			"/binflow/api/storage/no-such-repo?permissions",
			"/binflow/api/storage/generic-local/nope/x.bin?permissions",
		} {
			resp := h.do(http.MethodGet, path, adminUser, adminPass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("%s: status = %d", path, resp.StatusCode)
			}
			decodeError(t, resp)
		}
	})

	t.Run("gate matrix: anonymous 401, non-admin 403 (review B2)", func(t *testing.T) {
		h2 := newHarnessCfg(t, nil, [][2]string{{"ci-bot", "ci-pw"}})
		seedRepo(t, h2, "generic-local")
		// The view enumerates principal names and their bits — security
		// configuration — so it sits behind the admin gate even on an
		// anonymous-read instance (403/404 envelopes of the route plane).
		resp := h2.do(http.MethodGet, "/binflow/api/storage/generic-local/anything.bin?permissions", "", "", nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous status = %d, want 401", resp.StatusCode)
		}
		resp = h2.do(http.MethodGet, "/binflow/api/storage/generic-local/anything.bin?permissions", "ci-bot", "ci-pw", nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("non-admin status = %d, want 403", resp.StatusCode)
		}
	})
}

// ---- FR-27-AC9: groups carry no admin bit ----

func TestGroupHasNoAdminBit(t *testing.T) {
	h := newHarness(t)
	putGroup(t, h, "admins", "a group named like a privilege grants none")
	putUserWithGroups(t, h, "bob", "bob-pw", []string{"admins"})

	// bob authenticates (his membership is real)...
	resp := h.do(http.MethodGet, "/binflow/api/system/version", "bob", "bob-pw", nil, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bob auth status = %d", resp.StatusCode)
	}
	// ...but the management plane still refuses him: an "admins" membership
	// is not an admin flag (SE-07's intentional incompatibility).
	for _, path := range []string{
		"/binflow/api/security/users",
		"/binflow/api/security/groups",
		"/binflow/api/v1/permissions",
	} {
		resp := h.do(http.MethodGet, path, "bob", "bob-pw", nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s: status = %d, want 403", path, resp.StatusCode)
		}
	}
	// And his own detail shows admin=false.
	if detail := getUserDetail(t, h, "bob"); detail["admin"] != false {
		t.Fatalf("bob admin = %v, want false", detail["admin"])
	}
}
