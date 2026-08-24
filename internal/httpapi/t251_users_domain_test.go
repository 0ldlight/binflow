package httpapi_test

// T-251 (M9, ADR-0030 / architecture section 14.1): the users-domain
// endpoint widening and the DELETE verb, end to end on the real stack
// (sqlite metadata, real auth.Service, real router).
//
//   - E2: GET /api/security/users list items widen additively with
//     email/adminRole/enabled/groups (groups empty = [], never null);
//   - E3: GET /api/security/users/{name} echoes enabled (T-208 write-side
//     closure);
//   - E4: DELETE /api/security/users/{name} — the three guards (400), the
//     404 with zero side effects, the 200 plain text, and the cascade
//     (token 401, membership gone, permission principals stripped).

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// t251ListItem is the widened list entry the test decodes (fields the wire
// contract pins; unknown extras are ignored by design — additive widening).
type t251ListItem struct {
	Name      string   `json:"name"`
	Email     string   `json:"email"`
	AdminRole string   `json:"adminRole"`
	Enabled   bool     `json:"enabled"`
	Groups    []string `json:"groups"`
	Realm     string   `json:"realm"`
	Source    string   `json:"source"`
}

// t251CreateUser PUTs a user through the admin API (the same path N01 uses).
func t251CreateUser(t *testing.T, h *harness, name, email, password string, enabled *bool, groups []string) {
	t.Helper()
	body := map[string]any{"name": name, "email": email, "password": password}
	if enabled != nil {
		body["enabled"] = *enabled
	}
	if groups != nil {
		body["groups"] = groups
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp := h.do(http.MethodPut, "/binflow/api/security/users/"+name, adminUser, adminPass,
		raw, map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create %s = %d body=%s", name, resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()
}

// TestUserListWidening (E2): every entry carries the widened fields, the
// membership sets arrive aggregated and sorted, group-less users render []
// (not null), and the enabled flag reflects the stored row.
func TestUserListWidening(t *testing.T) {
	h := newHarness(t)

	// Two groups; u-one joins both (declared in reverse order — the wire
	// set must come back sorted), u-bare joins none, u-off is disabled.
	for _, g := range []string{"aa-devs", "zz-devs"} {
		resp := h.do(http.MethodPut, "/binflow/api/security/groups/"+g, adminUser, adminPass,
			[]byte(`{"name":"`+g+`","description":""}`),
			map[string]string{"Content-Type": "application/json"})
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			t.Fatalf("create group %s = %d body=%s", g, resp.StatusCode, mustGet(t, resp))
		}
		_ = resp.Body.Close()
	}
	t251CreateUser(t, h, "u-one", "one@example.com", "pw-one", nil, []string{"zz-devs", "aa-devs"})
	t251CreateUser(t, h, "u-bare", "bare@example.com", "pw-bare", nil, nil)
	off := false
	t251CreateUser(t, h, "u-off", "off@example.com", "pw-off", &off, nil)

	resp := h.do(http.MethodGet, "/binflow/api/security/users", adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list = %d body=%s", resp.StatusCode, body)
	}
	// The body must decode as a bare array of objects whose groups field is
	// a JSON array in EVERY entry (null would surface as a decode mismatch
	// below — decode into []*t251ListItem with Groups []string).
	var items []*t251ListItem
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	byName := map[string]*t251ListItem{}
	for _, it := range items {
		byName[it.Name] = it
		if it.Groups == nil {
			t.Fatalf("entry %s groups = null, want [] (E2 pins empty as [])", it.Name)
		}
	}
	if got := byName["u-one"]; got == nil || strings.Join(got.Groups, ",") != "aa-devs,zz-devs" {
		t.Fatalf("u-one = %+v, want sorted membership aa-devs,zz-devs", got)
	}
	if got := byName["u-bare"]; got == nil || len(got.Groups) != 0 || got.Email != "bare@example.com" {
		t.Fatalf("u-bare = %+v, want empty groups and stored email", got)
	}
	if got := byName["u-off"]; got == nil || got.Enabled {
		t.Fatalf("u-off = %+v, want enabled=false echoing the stored row", got)
	}
	if got := byName[adminUser]; got == nil || got.AdminRole != "admin" || !got.Enabled {
		t.Fatalf("admin entry = %+v, want adminRole=admin enabled=true", got)
	}
	// The pre-widening fields survive untouched on every entry.
	for _, it := range items {
		if it.Realm != "internal" || it.Source != "local" {
			t.Fatalf("entry %s realm/source = %q/%q", it.Name, it.Realm, it.Source)
		}
	}
}

// TestUserDetailEnabledEcho (E3): the single-user body always carries
// enabled — true for a live account, false for a disabled one, with no
// dependence on which plane last wrote it.
func TestUserDetailEnabledEcho(t *testing.T) {
	h := newHarness(t)
	off := false
	t251CreateUser(t, h, "u-off", "off@example.com", "pw-off", &off, nil)
	t251CreateUser(t, h, "u-on", "on@example.com", "pw-on", nil, nil)

	for name, want := range map[string]bool{"u-off": false, "u-on": true} {
		resp := h.do(http.MethodGet, "/binflow/api/security/users/"+name, adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get %s = %d body=%s", name, resp.StatusCode, body)
		}
		var detail map[string]any
		if err := json.Unmarshal([]byte(body), &detail); err != nil {
			t.Fatalf("decode %q: %v", body, err)
		}
		got, ok := detail["enabled"].(bool)
		if !ok {
			t.Fatalf("%s detail carries no boolean enabled: %v", name, detail)
		}
		if got != want {
			t.Fatalf("%s enabled = %v, want %v", name, got, want)
		}
	}
}

// TestUserDeleteGuards (E4) walks the three guards plus the 404
// table-driven. Every refused leg must leave the account untouched.
func TestUserDeleteGuards(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"u9", "pw-u9"}})

	tests := []struct {
		name       string
		target     string
		asUser     string // credential issuing the DELETE ("" = admin, unless anon)
		asPass     string // its password
		anon       bool   // issue the request with no credential at all
		wantStatus int
		wantBody   string
	}{
		{
			name:       "built-in admin is refused",
			target:     "admin",
			wantStatus: http.StatusBadRequest,
			wantBody:   "Cannot delete the built-in admin user.",
		},
		{
			name:       "built-in guard outranks the self guard for admin itself",
			target:     "admin",
			asUser:     "admin",
			asPass:     adminPass,
			wantStatus: http.StatusBadRequest,
			wantBody:   "Cannot delete the built-in admin user.",
		},
		{
			name:       "plain user never passes the route gate",
			target:     "u9",
			asUser:     "u-plain",
			asPass:     "pw-plain",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "second admin self-delete is refused",
			target:     "u9",
			asUser:     "u9",
			asPass:     "pw-u9",
			wantStatus: http.StatusBadRequest,
			wantBody:   "Cannot delete the current authenticated user.",
		},
		{
			name:       "unknown user is 404 with the users-plane wording",
			target:     "ghost",
			wantStatus: http.StatusNotFound,
			wantBody:   "User not found",
		},
		{
			name:       "anonymous is challenged",
			target:     "u9",
			anon:       true,
			wantStatus: http.StatusUnauthorized,
		},
	}
	// u9 must be an admin for the self-delete leg to reach the guard chain
	// (the route gate demands security:write); u-plain stays a plain user
	// for the gate leg.
	t251CreateUser(t, h, "u-plain", "plain@example.com", "pw-plain", nil, nil)
	promote := h.do(http.MethodPost, "/binflow/api/security/users/u9", adminUser, adminPass,
		[]byte(`{"adminRole":"admin"}`), map[string]string{"Content-Type": "application/json"})
	if promote.StatusCode != http.StatusOK {
		t.Fatalf("promote u9 = %d body=%s", promote.StatusCode, mustGet(t, promote))
	}
	_ = promote.Body.Close()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			user, pass := adminUser, adminPass
			if tc.anon {
				user, pass = "", ""
			} else if tc.asUser != "" {
				user, pass = tc.asUser, tc.asPass
			}
			resp := h.do(http.MethodDelete, "/binflow/api/security/users/"+tc.target, user, pass, nil, nil)
			body := mustGet(t, resp)
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d body=%s, want %d", resp.StatusCode, body, tc.wantStatus)
			}
			if tc.wantBody != "" && body != tc.wantBody {
				t.Fatalf("body = %q, want %q", body, tc.wantBody)
			}
			// The refused target still exists (guards are side-effect free).
			if tc.target != "ghost" {
				get := h.do(http.MethodGet, "/binflow/api/security/users/"+tc.target, adminUser, adminPass, nil, nil)
				if get.StatusCode != http.StatusOK {
					t.Fatalf("%s must survive the refused delete, GET = %d", tc.target, get.StatusCode)
				}
				_ = get.Body.Close()
			}
		})
	}

	// readonly_admin holds no security:write — the route gate answers 403
	// before any guard runs (FR-78.2's gate pin).
	t251CreateUser(t, h, "auditor", "a@example.com", "pw-a", nil, nil)
	resp := h.do(http.MethodPost, "/binflow/api/security/users/auditor", adminUser, adminPass,
		[]byte(`{"adminRole":"readonly_admin"}`),
		map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("promote auditor = %d body=%s", resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()
	resp = h.do(http.MethodDelete, "/binflow/api/security/users/u9", "auditor", "pw-a", nil, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("readonly_admin delete = %d, want 403", resp.StatusCode)
	}
	_ = mustGet(t, resp)
	get := h.do(http.MethodGet, "/binflow/api/security/users/u9", adminUser, adminPass, nil, nil)
	if get.StatusCode != http.StatusOK {
		t.Fatalf("u9 must survive the readonly_admin attempt, GET = %d", get.StatusCode)
	}
	_ = get.Body.Close()
}

// TestUserDeleteLastAdminGuard (E4): with the built-in admin demoted, the
// last admin-role account is not deletable — the rbac-model section 1.2.2
// wording family mirrored from the group face (400, not 409). On the wire
// the security:write gate means the solo admin can only name ITSELF, which
// is exactly why the use case checks last-admin before the self guard (the
// order architecture 14.1 lists): the self wording would hide the real
// reason. The contrast leg re-promotes a second admin and proves the same
// self-delete then lands on the self wording.
func TestUserDeleteLastAdminGuard(t *testing.T) {
	h := newHarness(t)

	// Promote u9, then demote the built-in admin: u9 is the only admin left.
	t251CreateUser(t, h, "u9", "u9@example.com", "pw-u9", nil, nil)
	resp := h.do(http.MethodPost, "/binflow/api/security/users/u9", adminUser, adminPass,
		[]byte(`{"adminRole":"admin"}`), map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("promote u9 = %d body=%s", resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()
	resp = h.do(http.MethodPost, "/binflow/api/security/users/admin", adminUser, adminPass,
		[]byte(`{"adminRole":"user"}`), map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("demote admin = %d body=%s", resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()

	// u9 (the only admin) tries to delete itself: the last-admin guard
	// answers, not the self guard.
	resp = h.do(http.MethodDelete, "/binflow/api/security/users/u9", "u9", "pw-u9", nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("last-admin delete = %d body=%s, want 400", resp.StatusCode, body)
	}
	want := "Cannot delete user 'u9'. There must be at least one user configured with admin privileges."
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
	get := h.do(http.MethodGet, "/binflow/api/security/users/u9", "u9", "pw-u9", nil, nil)
	if get.StatusCode != http.StatusOK {
		t.Fatalf("u9 must survive the refused delete, GET = %d", get.StatusCode)
	}
	_ = get.Body.Close()

	// Contrast: with a second admin back, the same self-delete lands on the
	// self guard's wording.
	resp = h.do(http.MethodPost, "/binflow/api/security/users/admin", "u9", "pw-u9",
		[]byte(`{"adminRole":"admin"}`), map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-promote admin = %d body=%s", resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()
	resp = h.do(http.MethodDelete, "/binflow/api/security/users/u9", "u9", "pw-u9", nil, nil)
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusBadRequest || body != "Cannot delete the current authenticated user." {
		t.Fatalf("second-admin self-delete = %d %q, want 400 self wording", resp.StatusCode, body)
	}
}

// TestUserDeleteFullChain (E4, AC3): the success path's 200 plain text, the
// immediate GET 404, the dead token, the stripped membership and permission
// principals, the audit row, and the repeat-delete 404.
func TestUserDeleteFullChain(t *testing.T) {
	ctx := t.Context()
	h := newHarness(t)

	// Fixture: u9 in group devs, holding an API token, named as a user
	// principal of target t (which also names admin, to prove the strip is
	// per-principal).
	resp := h.do(http.MethodPut, "/binflow/api/security/groups/devs", adminUser, adminPass,
		[]byte(`{"name":"devs","description":""}`),
		map[string]string{"Content-Type": "application/json"})
	_ = resp.Body.Close()
	t251CreateUser(t, h, "u9", "u9@example.com", "pw-u9", nil, []string{"devs"})
	now := metadata.Now()
	if err := h.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "r", Type: "local", PackageType: "generic", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	resp = h.do(http.MethodPost, "/binflow/api/v1/permissions", adminUser, adminPass,
		[]byte(`{"name":"t","repos":["r"],"principals":{"users":{"u9":["read"],"admin":["manage"]}}}`),
		map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed target = %d body=%s", resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()
	resp = h.do(http.MethodPost, "/binflow/api/security/token", adminUser, adminPass,
		[]byte(`{"username":"u9","expires_in":3600}`),
		map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mint for u9 = %d body=%s", resp.StatusCode, mustGet(t, resp))
	}
	var tok tokenResp
	if err := json.Unmarshal([]byte(mustGet(t, resp)), &tok); err != nil {
		t.Fatalf("mint body: %v", err)
	}
	_ = resp.Body.Close()

	// The token works before the delete.
	use := h.do(http.MethodGet, "/binflow/api/system/version", "", "",
		nil, map[string]string{"Authorization": "Bearer " + tok.AccessToken})
	if use.StatusCode != http.StatusOK {
		t.Fatalf("token before delete = %d, want 200", use.StatusCode)
	}
	_ = use.Body.Close()

	// Delete: 200, the auth-model section 1 wording verbatim.
	resp = h.do(http.MethodDelete, "/binflow/api/security/users/u9", adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete = %d body=%s, want 200", resp.StatusCode, body)
	}
	if body != "The user: 'u9' has been removed successfully." {
		t.Fatalf("body = %q", body)
	}

	// The account is gone; the repeat delete is a 404 (BinFlow's own probe
	// answers where Artifactory would swallow the Access 404 — architecture
	// 14.1 pins the probe).
	get := h.do(http.MethodGet, "/binflow/api/security/users/u9", adminUser, adminPass, nil, nil)
	if get.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete = %d, want 404", get.StatusCode)
	}
	_ = get.Body.Close()
	resp = h.do(http.MethodDelete, "/binflow/api/security/users/u9", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("repeat delete = %d, want 404", resp.StatusCode)
	}
	_ = mustGet(t, resp)

	// The token died with the row (verify re-resolves the owner).
	use = h.do(http.MethodGet, "/binflow/api/system/version", "", "",
		nil, map[string]string{"Authorization": "Bearer " + tok.AccessToken})
	if use.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token after delete = %d, want 401", use.StatusCode)
	}
	_ = use.Body.Close()

	// Membership and principals: no orphan rows naming u9.
	if gs, err := h.md.Groups().GroupsOfUser(ctx, "u9"); err != nil || len(gs) != 0 {
		t.Fatalf("memberships after delete = %v/%d, want none", err, len(gs))
	}
	_, principals, err := h.md.Permissions().GetTarget(ctx, "t")
	if err != nil {
		t.Fatalf("get target: %v", err)
	}
	for _, p := range principals {
		if p.Principal == "u9" {
			t.Fatalf("target t still carries a u9 principal: %+v", principals)
		}
	}
	if len(principals) != 1 {
		t.Fatalf("principals after delete = %+v, want only admin's row", principals)
	}

	// The audit trail: exactly one user.delete, actor admin, detail u9.
	events, err := h.md.Audits().Query(ctx, metadata.AuditQuery{Action: "user.delete"})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("user.delete events = %d, want 1", len(events))
	}
	if events[0].Actor != "admin" || !strings.Contains(events[0].Detail, `"user":"u9"`) {
		t.Fatalf("user.delete event = %+v", events[0])
	}
}
