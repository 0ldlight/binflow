// T-215 acceptance surface (PRD M7 FR-64, V01~V06): the capability-migrated
// route gates against the real stack — admin unchanged, readonly_admin
// reads the governance plane and modifies nothing, plain users keep their
// 403s, the adminRole wire field lands in users.role with immediate effect
// on live tokens, and every role move leaves a user.role.change audit
// event. The route inventory itself is architecture section 7.1 [M7]; the
// OBSERVABLE contract here is the PRD's target table.

package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// t215Setup provisions the fixtures every T-215 case shares: one local
// repository, a plain user, a sacrificial group and a readonly_admin
// account (created through the REAL wire path — the field under test).
func t215Setup(t *testing.T, h *harness) {
	t.Helper()
	t215Admin(t, h, http.MethodPut, "api/repositories/m7t",
		`{"rclass":"local","packageType":"generic"}`, 200)
	t215Admin(t, h, http.MethodPut, "api/security/users/plain",
		`{"name":"plain","email":"plain@t.io","password":"plain-pw","admin":false}`, 201)
	t215Admin(t, h, http.MethodPut, "api/security/users/roa",
		`{"name":"roa","email":"roa@t.io","password":"roa-pw","adminRole":"readonly_admin"}`, 201)
	t215Admin(t, h, http.MethodPut, "api/security/groups/sacgrp",
		`{"name":"sacgrp","description":"sacrificial"}`, 201)
}

// t215Admin issues an admin-authenticated request and demands the status.
func t215Admin(t *testing.T, h *harness, method, path, body string, want int) string {
	t.Helper()
	resp := h.do(method, "/binflow/"+path, adminUser, adminPass, []byte(body), nil)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s body: %v", method, path, err)
	}
	if resp.StatusCode != want {
		t.Fatalf("%s %s (admin) = %d, want %d (body: %s)", method, path, resp.StatusCode, want, got)
	}
	return string(got)
}

// t215As issues a request as one fixture principal.
func t215As(t *testing.T, h *harness, method, path, user, pass, body string) *http.Response {
	t.Helper()
	resp := h.do(method, "/binflow/"+path, user, pass, []byte(body), nil)
	return resp
}

func t215Code(t *testing.T, h *harness, method, path, user, pass, body string) int {
	t.Helper()
	resp := t215As(t, h, method, path, user, pass, body)
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// TestT215RoleMatrixReadFace: the PRD's 11-endpoint governance read face
// (plus the ?permissions view) across the three roles. 501 rows are the
// pass-gate posture: the capability gate PASSED and the instance honestly
// reports the feature is not wired (storage/migration on a bare stack; the
// replication pair on the harness, whose Deps.Replication is nil) — the
// discriminating assertion is that a plain user collects 403 on the same
// rows, proving the gate decided.
func TestT215RoleMatrixReadFace(t *testing.T) {
	h := newHarness(t)
	t215Setup(t, h)

	rows := []struct {
		path                  string
		admin, roa, plainUser int
	}{
		{"api/repositories", 200, 200, 403},
		{"api/repositories/m7t", 200, 200, 403},
		{"api/v1/health", 200, 200, 403},
		{"api/security/users", 200, 200, 403},
		{"api/security/users/roa", 200, 200, 403},
		{"api/security/groups", 200, 200, 403},
		{"api/v1/permissions", 200, 200, 403},
		{"api/v1/audit", 200, 200, 403},
		{"api/v1/replications", 501, 501, 403},
		{"api/v1/replication/status", 501, 501, 403},
		{"api/v1/storage/stats", 200, 200, 403},
		{"api/v1/storage/migration", 501, 501, 403},
		{"api/storage/m7t/?permissions", 200, 200, 403},
	}
	for _, row := range rows {
		if got := t215Code(t, h, http.MethodGet, row.path, adminUser, adminPass, ""); got != row.admin {
			t.Errorf("GET %s admin = %d, want %d", row.path, got, row.admin)
		}
		if got := t215Code(t, h, http.MethodGet, row.path, "roa", "roa-pw", ""); got != row.roa {
			t.Errorf("GET %s readonly_admin = %d, want %d", row.path, got, row.roa)
		}
		if got := t215Code(t, h, http.MethodGet, row.path, "plain", "plain-pw", ""); got != row.plainUser {
			t.Errorf("GET %s plain user = %d, want %d", row.path, got, row.plainUser)
		}
	}
}

// TestT215ReadOnlyAdminChangeFaceDenied: every change-plane sample is a 403
// for readonly_admin AND leaves the addressed entity byte-identical (the
// PRD's zero-side-effect assertion). The plain user's 403s are asserted on
// the same rows.
func TestT215ReadOnlyAdminChangeFaceDenied(t *testing.T) {
	h := newHarness(t)
	t215Setup(t, h)

	type write struct {
		method, path, body string
		snapshot           string // path whose GET body must survive verbatim
	}
	writes := []write{
		// family 6: the CREATE arm of PUT (repo does not exist)
		{http.MethodPut, "api/repositories/brand-new", `{"rclass":"local","packageType":"generic"}`, ""},
		// family 7: replace arm, partial update carrying the quota field
		{http.MethodPut, "api/repositories/m7t", `{"rclass":"local","packageType":"generic"}`, "api/repositories/m7t"},
		{http.MethodPost, "api/repositories/m7t", `{"quotaBytes":99999}`, "api/repositories/m7t"},
		// family 4: users, groups, permission targets, token revoke
		{http.MethodPut, "api/security/users/plain", `{"email":"hax@t.io","password":"hax-pw-123"}`, "api/security/users/plain"},
		{http.MethodPost, "api/security/users/plain", `{"email":"hax@t.io"}`, "api/security/users/plain"},
		{http.MethodDelete, "api/security/groups/sacgrp", "", "api/security/groups/sacgrp"},
		{http.MethodPost, "api/v1/permissions", `{"name":"t-m7","repos":["m7t"],"principals":{"users":{},"groups":{}}}`, "api/v1/permissions"},
		{http.MethodPost, "api/security/token/revoke", "token=sha256-deadbeef", ""},
		// family 2: GC (dry-run arm included) and migration start
		{http.MethodPost, "api/v1/system/gc", `{"apply":false}`, ""},
		{http.MethodPost, "api/v1/storage/migration/start", "", ""},
	}
	for _, w := range writes {
		var before string
		if w.snapshot != "" {
			before = t215Admin(t, h, http.MethodGet, w.snapshot, "", 200)
		}
		if got := t215Code(t, h, w.method, w.path, "roa", "roa-pw", w.body); got != http.StatusForbidden {
			t.Errorf("%s %s readonly_admin = %d, want 403", w.method, w.path, got)
		}
		if got := t215Code(t, h, w.method, w.path, "plain", "plain-pw", w.body); got != http.StatusForbidden {
			t.Errorf("%s %s plain user = %d, want 403", w.method, w.path, got)
		}
		if w.snapshot != "" {
			if after := t215Admin(t, h, http.MethodGet, w.snapshot, "", 200); after != before {
				t.Errorf("%s %s: entity changed across a denied write\nbefore: %s\nafter:  %s",
					w.method, w.path, before, after)
			}
		}
	}
	// The create-arm row must not have minted the repository either.
	if got := t215Code(t, h, http.MethodGet, "api/repositories/brand-new", adminUser, adminPass, ""); got != http.StatusNotFound {
		t.Errorf("GET brand-new after denied create = %d, want 404", got)
	}
}

// TestT215AdminRoleWireAssignment (V01): the create/replace/partial-update
// bodies accept adminRole, the closed set validates, the echo reports it
// and users.role lands through the real store.
func TestT215AdminRoleWireAssignment(t *testing.T) {
	h := newHarness(t)
	t215Setup(t, h)

	getRole := func(name string) string {
		u, err := h.md.Users().Get(t.Context(), name)
		if err != nil {
			t.Fatalf("store get %s: %v", name, err)
		}
		return u.Role
	}
	echoRole := func(name string) string {
		body := t215Admin(t, h, http.MethodGet, "api/security/users/"+name, "", 200)
		var d struct {
			Admin     bool   `json:"admin"`
			AdminRole string `json:"adminRole"`
		}
		if err := json.Unmarshal([]byte(body), &d); err != nil {
			t.Fatalf("decode user detail: %v", err)
		}
		return d.AdminRole
	}

	// V01 happy path: explicit readonly_admin on create; echo + DB landing.
	if got := echoRole("roa"); got != "readonly_admin" {
		t.Errorf("roa echo adminRole = %q, want readonly_admin", got)
	}
	if got := getRole("roa"); got != "readonly_admin" {
		t.Errorf("roa users.role = %q, want readonly_admin", got)
	}
	// The mirror: readonly_admin is not admin.
	if body := t215Admin(t, h, http.MethodGet, "api/security/users/roa", "", 200); !strings.Contains(body, `"admin": false`) {
		t.Errorf("roa detail admin flag should be false: %s", body)
	}

	// Partial update moves the role (POST /{name}, adminRole-only).
	t215Admin(t, h, http.MethodPost, "api/security/users/roa", `{"adminRole":"user"}`, 200)
	if got := getRole("roa"); got != "user" {
		t.Errorf("roa users.role after demote = %q, want user", got)
	}
	t215Admin(t, h, http.MethodPost, "api/security/users/roa", `{"adminRole":"readonly_admin"}`, 200)
	if got := getRole("roa"); got != "readonly_admin" {
		t.Errorf("roa users.role after promote = %q, want readonly_admin", got)
	}

	// Replace keeps the explicit role and re-derives the boolean path.
	t215Admin(t, h, http.MethodPut, "api/security/users/roa",
		`{"email":"roa2@t.io","password":"roa-pw","adminRole":"readonly_admin"}`, 201)
	if got := getRole("roa"); got != "readonly_admin" {
		t.Errorf("roa users.role after replace = %q, want readonly_admin", got)
	}

	// The pre-M7 boolean spelling keeps working byte-for-byte.
	t215Admin(t, h, http.MethodPut, "api/security/users/booladmin",
		`{"email":"b@t.io","password":"bool-pw","admin":true}`, 201)
	if got := getRole("booladmin"); got != "admin" {
		t.Errorf("booladmin users.role = %q, want admin (boolean derivation)", got)
	}
	if got := echoRole("booladmin"); got != "admin" {
		t.Errorf("booladmin echo adminRole = %q, want admin", got)
	}

	// Conflicts and closed-set violations are 400 PLAIN TEXT, nothing stored.
	for _, tc := range []struct{ name, body string }{
		{"false boolean with admin role", `{"email":"c@t.io","password":"conf-pw","admin":false,"adminRole":"admin"}`},
		{"absent boolean with admin role", `{"email":"c@t.io","password":"conf-pw","adminRole":"admin"}`},
		{"true boolean with user role", `{"email":"c@t.io","password":"conf-pw","admin":true,"adminRole":"user"}`},
		{"true boolean with readonly role", `{"email":"c@t.io","password":"conf-pw","admin":true,"adminRole":"readonly_admin"}`},
		{"kebab role spelling", `{"email":"c@t.io","password":"conf-pw","adminRole":"read-only-admin"}`},
		{"unknown role value", `{"email":"c@t.io","password":"conf-pw","adminRole":"superuser"}`},
	} {
		resp := t215As(t, h, http.MethodPut, "api/security/users/conflictor", adminUser, adminPass, tc.body)
		gotBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: PUT = %d, want 400 (body: %s)", tc.name, resp.StatusCode, gotBody)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
			t.Errorf("%s: conflict body Content-Type = %q, want the plain-text layer", tc.name, ct)
		}
	}
	if _, err := h.md.Users().Get(t.Context(), "conflictor"); err == nil {
		t.Error("a conflicting body created the user anyway")
	}
	// Partial-update conflicts are 400 too.
	for _, body := range []string{
		`{"admin":true,"adminRole":"readonly_admin"}`,
		`{"admin":false,"adminRole":"admin"}`,
		`{"adminRole":"nonsense"}`,
	} {
		resp := t215As(t, h, http.MethodPost, "api/security/users/roa", adminUser, adminPass, body)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("POST partial %s = %d, want 400", body, resp.StatusCode)
		}
	}

	// V05: a non-admin carrying the field never reaches it — the route gate
	// (security:write) answers first, whatever the body asks for.
	resp := t215As(t, h, http.MethodPut, "api/security/users/selfmade", "plain", "plain-pw",
		`{"email":"s@t.io","password":"self-pw","adminRole":"admin"}`)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("plain user self-promotion = %d, want 403", resp.StatusCode)
	}

	// whoami echoes the closed-set role for the console's read-only mode.
	resp = t215As(t, h, http.MethodGet, "api/v1/session", "roa", "roa-pw", "")
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"adminRole": "readonly_admin"`) {
		t.Errorf("whoami = %d %s, want the adminRole echo", resp.StatusCode, body)
	}
}

// TestT215RoleImmediateEffectOnLiveToken (V04): one minted token, three
// verdicts — 403 as user, 200 after promotion, 403 after demotion, no
// reissue, no restart.
func TestT215RoleImmediateEffectOnLiveToken(t *testing.T) {
	h := newHarness(t)
	t215Setup(t, h)

	mint := t215As(t, h, http.MethodPost, "api/security/token", "plain", "plain-pw", "")
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	body, _ := io.ReadAll(mint.Body)
	_ = mint.Body.Close()
	if mint.StatusCode != http.StatusOK {
		t.Fatalf("mint token = %d (%s)", mint.StatusCode, body)
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		t.Fatalf("mint token body: %s", body)
	}

	replay := func() int {
		req, err := http.NewRequest(http.MethodGet, h.srv.URL+"/binflow/api/security/users", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		resp, err := h.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}

	if got := replay(); got != http.StatusForbidden {
		t.Fatalf("same token as user = %d, want 403", got)
	}
	t215Admin(t, h, http.MethodPost, "api/security/users/plain", `{"adminRole":"readonly_admin"}`, 200)
	if got := replay(); got != http.StatusOK {
		t.Fatalf("same token after promotion = %d, want 200", got)
	}
	t215Admin(t, h, http.MethodPost, "api/security/users/plain", `{"adminRole":"user"}`, 200)
	if got := replay(); got != http.StatusForbidden {
		t.Fatalf("same token after demotion = %d, want 403", got)
	}
}

// TestT215RoleChangeAudit (V06): assignment, moves and the boolean-path
// promotion all leave user.role.change events with actor, target, old and
// new roles.
func TestT215RoleChangeAudit(t *testing.T) {
	h := newHarness(t)
	t215Setup(t, h)

	// A fresh account whose whole lifecycle the audit query can see:
	// created readonly_admin, demoted to user, promoted to admin via the
	// boolean field, demoted back.
	t215Admin(t, h, http.MethodPut, "api/security/users/auditee",
		`{"email":"a@t.io","password":"aud-pw","adminRole":"readonly_admin"}`, 201)
	t215Admin(t, h, http.MethodPost, "api/security/users/auditee", `{"adminRole":"user"}`, 200)
	t215Admin(t, h, http.MethodPost, "api/security/users/auditee", `{"admin":true}`, 200)
	t215Admin(t, h, http.MethodPost, "api/security/users/auditee", `{"admin":false}`, 200)

	body := t215Admin(t, h, http.MethodGet, "api/v1/audit?action=user.role.change&limit=50", "", 200)
	var page struct {
		Events []struct {
			Actor  string          `json:"actor"`
			Action string          `json:"action"`
			Detail json.RawMessage `json:"detail"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decode audit page: %v", err)
	}

	type move struct{ old, new string }
	got := map[string]move{}
	for _, e := range page.Events {
		if e.Action != "user.role.change" {
			t.Errorf("action filter returned %q", e.Action)
			continue
		}
		var d struct {
			User string `json:"user"`
			Old  string `json:"old"`
			New  string `json:"new"`
		}
		if err := json.Unmarshal(e.Detail, &d); err != nil {
			t.Fatalf("decode event detail %s: %v", e.Detail, err)
		}
		if d.User == "auditee" && e.Actor == adminUser {
			got[d.Old+"->"+d.New] = move{d.Old, d.New}
		}
	}
	for _, want := range []move{
		{"", "readonly_admin"}, // creation with an explicit non-default role
		{"readonly_admin", "user"},
		{"user", "admin"}, // the boolean promotion leaves the same trail
		{"admin", "user"},
	} {
		key := want.old + "->" + want.new
		if _, ok := got[key]; !ok {
			t.Errorf("missing user.role.change %s for auditee (got %v)", key, got)
		}
	}
	// A default-role creation is not a role event.
	if _, ok := got["->user"]; ok {
		t.Error("default-role creation recorded a role change")
	}
}

// TestT215ReadOnlyAdminDockerFormLeg (T-212 review handover 1): the
// /v2/token form credential arm carries the role — a readonly_admin form
// login gets its global read honored in the scope narrowing (pull granted,
// push stripped), where the pre-fix principal folded it down to a plain
// user with nothing granted.
func TestT215ReadOnlyAdminDockerFormLeg(t *testing.T) {
	h := newHarness(t)
	t215Setup(t, h)

	token := func(user, pass, scope string) (int, string) {
		body := "grant_type=password&service=binflow&scope=" + scope +
			"&username=" + user + "&password=" + pass
		req, err := http.NewRequest(http.MethodPost, h.srv.URL+"/v2/token", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := h.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		var tok struct {
			Scope string `json:"scope"`
		}
		_ = json.Unmarshal(raw, &tok)
		return resp.StatusCode, tok.Scope
	}

	// readonly_admin: pull survives narrowing (global r), push is stripped.
	code, scope := token("roa", "roa-pw", "repository:animg:pull,push")
	if code != http.StatusOK {
		t.Fatalf("/v2/token form leg = %d", code)
	}
	if scope != "repository:animg:pull" {
		t.Errorf("readonly_admin form-leg scope = %q, want repository:animg:pull (the pre-fix bug narrowed it to empty)", scope)
	}
	// A plain user with no grants gets nothing — the negative control that
	// proves the grant above came from the role, not from a leaky default.
	code, scope = token("plain", "plain-pw", "repository:animg:pull")
	if code != http.StatusOK || scope != "" {
		t.Errorf("plain user form-leg scope = %q (code %d), want empty", scope, code)
	}
}

// TestT215StorageUsageStaysReadPlane (W26b): the usage endpoint keeps its
// admin-OR-read-grant seam — readonly_admin passes through the content-plane
// global r, a plain user without grants collects 403. The route itself
// never grew a management gate (family 7 leaves the judgment to the use
// case).
func TestT215StorageUsageStaysReadPlane(t *testing.T) {
	h := newHarness(t)
	t215Setup(t, h)

	if got := t215Code(t, h, http.MethodGet, "api/v1/storage/usage/m7t", adminUser, adminPass, ""); got != 200 {
		t.Errorf("usage admin = %d, want 200", got)
	}
	if got := t215Code(t, h, http.MethodGet, "api/v1/storage/usage/m7t", "roa", "roa-pw", ""); got != 200 {
		t.Errorf("usage readonly_admin = %d, want 200 (global content read)", got)
	}
	if got := t215Code(t, h, http.MethodGet, "api/v1/storage/usage/m7t", "plain", "plain-pw", ""); got != 403 {
		t.Errorf("usage plain user = %d, want 403", got)
	}
}

// TestT215StoreLandsRoleMirrorInvariant: every wire write keeps users.role
// and the is_admin mirror in step (the T-212 review's contradictory-input
// concern — the handler's resolveCreateLevel guard is what keeps the pair
// consistent on the way in).
func TestT215StoreLandsRoleMirrorInvariant(t *testing.T) {
	h := newHarness(t)
	t215Setup(t, h)

	check := func(name string, wantRole string, wantAdmin bool) {
		t.Helper()
		u, err := h.md.Users().Get(t.Context(), name)
		if err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		if u.Role != wantRole || u.IsAdmin != wantAdmin {
			t.Errorf("%s = role %q is_admin %v, want %q/%v", name, u.Role, u.IsAdmin, wantRole, wantAdmin)
		}
	}
	check("roa", "readonly_admin", false)
	check("plain", "user", false)
	check(adminUser, "admin", true)

	t215Admin(t, h, http.MethodPost, "api/security/users/roa", `{"adminRole":"admin"}`, 200)
	check("roa", "admin", true)
	t215Admin(t, h, http.MethodPost, "api/security/users/roa", `{"adminRole":"readonly_admin"}`, 200)
	check("roa", "readonly_admin", false)
}
