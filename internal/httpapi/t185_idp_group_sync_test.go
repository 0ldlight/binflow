// T-185 acceptance surface at the HTTP plane: the T-174 H28 failure
// reproduced and closed in-process — a mock IdP's groups_claim maps onto a
// local group, the login syncs the membership into user_groups, and the
// SESSION arm (the arm H28 showed losing the groups) authorizes against a
// group-permissioned repository. Also covers the D1/D2 field gaps: the
// users list/detail carry source, whoami carries groups, and the IdP-side
// removal leg (FR-54-AC5) plus the admin_group refresh (D4/O-4) end to end.

package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/console"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// t185Stack is a full generic-content stack with the OIDC login flow armed
// (the oidcStack of oidc_routes_test.go plus the storage/repo plane, which
// H28 needs: the assertion is a content GET over a group grant).
type t185Stack struct {
	ts  *httptest.Server
	md  metadata.Store
	idp *mockOIDCIDP
}

func newT185Stack(t *testing.T, adminGroup string) *t185Stack {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()

	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	idp := newMockOIDCIDP(t)
	provider, err := auth.NewOIDCProvider(ctx, &auth.OIDCConfig{
		IssuerURL:    idp.issuer,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://binflow.example.com/binflow/api/v1/oidc/callback",
		Scopes:       []string{"openid", "profile", "email"},
		UserClaim:    "preferred_username",
		GroupClaim:   "groups",
		AdminGroup:   adminGroup,
	}, auth.NewLDAPResolver(md.Users()))
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess).
		WithOIDC(provider, storeUserCreator{md.Users()})
	svc := repo.New(st, md, authSvc, audit.New(md, true))
	genericHandler := generic.New(svc, md.Blobs())

	s := httpapi.New(httpapi.Deps{
		Config:    cfg,
		Auth:      authSvc,
		Authz:     authSvc,
		Metadata:  md,
		Repos:     md.Repos(),
		ReposSvc:  svc,
		Passwords: authSvc,
		Tokens:    authSvc,
		OIDC:      provider,
		DataDir:   dataDir,
		Console:   console.Handler(),
		Adapters:  []adapter.Handler{genericHandler},
		Version:   "1.0.0-test",
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &t185Stack{ts: ts, md: md, idp: idp}
}

// t185Seed installs the H28 fixtures: a local repo, the local group the IdP
// claim names, and a group-typed read grant on the repo.
func t185Seed(t *testing.T, md metadata.Store) {
	t.Helper()
	ctx := context.Background()
	if err := md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "oidc-sync-repo", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	now := metadata.Now()
	if err := md.Groups().Create(ctx, &metadata.Group{
		Name: "developers", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	repos, _ := json.Marshal([]string{"oidc-sync-repo"})
	includes, _ := json.Marshal([]string{"**"})
	excludes, _ := json.Marshal([]string{})
	if err := md.Permissions().PutTarget(ctx,
		&metadata.PermissionTarget{
			Name: "h28-target", Repos: string(repos), Includes: string(includes),
			Excludes: string(excludes), CreatedAt: now, UpdatedAt: now,
		},
		[]*metadata.PermissionPrincipal{{
			TargetName: "h28-target", Principal: "developers", PrincipalType: "group",
			CanRead: true,
		}}); err != nil {
		t.Fatalf("seed permission: %v", err)
	}
}

// t185Do issues one request: optional Basic pair for the admin legs, one
// optional cookie header, optional body. Redirects are not followed.
func t185Do(t *testing.T, tsURL, method, path, basicUser, basicPass, cookie, body string) *http.Response {
	t.Helper()
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	var reqBody *strings.Reader
	if rdr != nil {
		reqBody = rdr
	}
	var r *http.Request
	var err error
	if reqBody != nil {
		r, err = http.NewRequest(method, tsURL+path, reqBody)
	} else {
		r, err = http.NewRequest(method, tsURL+path, nil)
	}
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if basicUser != "" {
		r.SetBasicAuth(basicUser, basicPass)
	}
	if cookie != "" {
		r.Header.Set("Cookie", cookie)
	}
	resp, err := noRedirectClient().Do(r)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// t185SSOLogin runs one browser SSO round trip and returns the session
// cookie header value.
func t185SSOLogin(t *testing.T, st *t185Stack) string {
	t.Helper()
	state, tx := oidcLoginLeg(t, st.ts.URL, "")
	resp := oidcDo(t, st.ts.URL, http.MethodGet,
		"/binflow/api/v1/oidc/callback?code=auth-code-1&state="+state, tx)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302, body=%s", resp.StatusCode, mustGet(t, resp))
	}
	sess := responseCookie(resp, auth.CookieSessionName)
	if sess == nil || sess.value == "" {
		t.Fatalf("callback carries no session cookie: %q", resp.Header.Values("Set-Cookie"))
	}
	return auth.CookieSessionName + "=" + sess.value
}

// t185Whoami decodes the whoami body.
func t185Whoami(t *testing.T, tsURL, cookie string) (username string, admin bool, groups []string) {
	t.Helper()
	resp := t185Do(t, tsURL, http.MethodGet, "/binflow/api/v1/session", "", "", cookie, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("whoami status = %d, body=%s", resp.StatusCode, mustGet(t, resp))
	}
	var body struct {
		Username string   `json:"username"`
		Admin    bool     `json:"admin"`
		Groups   []string `json:"groups"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode whoami: %v", err)
	}
	return body.Username, body.Admin, body.Groups
}

// TestT185H28GroupAuthorizedRepoOverSession is the H28 Go reproduction:
// groups_claim -> user_groups -> group-authorized repository readable over
// the session arm; the IdP-side removal expires the grant on re-login
// (FR-54-AC5), and the D1/D2 field gaps are asserted on the way.
func TestT185H28GroupAuthorizedRepoOverSession(t *testing.T) {
	st := newT185Stack(t, "")
	t185Seed(t, st.md)

	// The artifact lands through the admin credential (the pre-T-185
	// posture already allowed this leg).
	up := t185Do(t, st.ts.URL, http.MethodPut, "/binflow/oidc-sync-repo/h28/test.txt",
		adminUser, adminPass, "", "h28 payload")
	if up.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d, body=%s", up.StatusCode, mustGet(t, up))
	}

	// SSO login with groups_claim ["developers"] — the membership must be
	// visible in the row, not just in the login Principal.
	st.idp.tokenClaims = st.idp.claimsFor("sub-h28", "h28user", `"developers"`)
	cookie := t185SSOLogin(t, st)

	if _, _, groups := t185Whoami(t, st.ts.URL, cookie); len(groups) != 1 || groups[0] != "developers" {
		t.Fatalf("whoami groups = %v, want [developers] (D2)", groups)
	}
	rows, err := st.md.Groups().GroupsOfUser(context.Background(), "h28user")
	if err != nil {
		t.Fatalf("GroupsOfUser: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "developers" {
		t.Fatalf("user_groups rows = %v, want developers materialized (D4)", rows)
	}

	// H28 core: the session arm authorizes the group-permissioned GET.
	get := t185Do(t, st.ts.URL, http.MethodGet, "/binflow/oidc-sync-repo/h28/test.txt", "", "", cookie, "")
	if get.StatusCode != http.StatusOK {
		t.Fatalf("session GET status = %d, want 200 (H28), body=%s", get.StatusCode, mustGet(t, get))
	}
	if body := mustGet(t, get); body != "h28 payload" {
		t.Fatalf("session GET body = %q, want the stored artifact", body)
	}

	// D1: the users surface names the owning provider.
	list := t185Do(t, st.ts.URL, http.MethodGet, "/binflow/api/security/users", adminUser, adminPass, "", "")
	if list.StatusCode != http.StatusOK {
		t.Fatalf("users list status = %d", list.StatusCode)
	}
	var items []struct {
		Name   string `json:"name"`
		Realm  string `json:"realm"`
		Source string `json:"source"`
	}
	if err := json.NewDecoder(list.Body).Decode(&items); err != nil {
		t.Fatalf("decode users list: %v", err)
	}
	var h28item *struct {
		Name   string `json:"name"`
		Realm  string `json:"realm"`
		Source string `json:"source"`
	}
	for i := range items {
		if items[i].Name == "h28user" {
			h28item = &items[i]
		}
	}
	if h28item == nil {
		t.Fatalf("users list has no h28user entry: %+v", items)
	}
	if h28item.Source != "oidc" || h28item.Realm != "oidc" {
		t.Fatalf("h28user list entry source/realm = %q/%q, want oidc/oidc (D1)", h28item.Source, h28item.Realm)
	}
	detail := t185Do(t, st.ts.URL, http.MethodGet, "/binflow/api/security/users/h28user", adminUser, adminPass, "", "")
	if detail.StatusCode != http.StatusOK {
		t.Fatalf("user detail status = %d", detail.StatusCode)
	}
	var one struct {
		Groups []string `json:"groups"`
		Realm  string   `json:"realm"`
		Source string   `json:"source"`
	}
	if err := json.NewDecoder(detail.Body).Decode(&one); err != nil {
		t.Fatalf("decode user detail: %v", err)
	}
	if one.Source != "oidc" || len(one.Groups) != 1 || one.Groups[0] != "developers" {
		t.Fatalf("user detail source/groups = %q/%v, want oidc/[developers] (D1)", one.Source, one.Groups)
	}

	// FR-54-AC5: the IdP removes the user from the group; after a fresh
	// login the same session-arm GET falls to 403 and whoami reports no
	// groups.
	st.idp.tokenClaims = st.idp.claimsFor("sub-h28", "h28user", "")
	cookie2 := t185SSOLogin(t, st)
	if _, _, groups := t185Whoami(t, st.ts.URL, cookie2); len(groups) != 0 {
		t.Fatalf("whoami groups after removal = %v, want []", groups)
	}
	get2 := t185Do(t, st.ts.URL, http.MethodGet, "/binflow/oidc-sync-repo/h28/test.txt", "", "", cookie2, "")
	if get2.StatusCode != http.StatusForbidden {
		t.Fatalf("session GET after removal status = %d, want 403 (FR-54-AC5)", get2.StatusCode)
	}
}

// TestT185AdminGroupRefreshOverLogins pins the D4/O-4 fix end to end: the
// admin_group verdict is re-derived on every login, so an IdP-side demotion
// takes effect on the next login instead of being frozen at the first.
func TestT185AdminGroupRefreshOverLogins(t *testing.T) {
	st := newT185Stack(t, "binflow-admins")

	st.idp.tokenClaims = st.idp.claimsFor("sub-boss", "boss", `"binflow-admins"`)
	cookie := t185SSOLogin(t, st)
	if name, admin, _ := t185Whoami(t, st.ts.URL, cookie); name != "boss" || !admin {
		t.Fatalf("whoami after promotion = %s/%v, want boss/admin=true", name, admin)
	}

	st.idp.tokenClaims = st.idp.claimsFor("sub-boss", "boss", `"developers"`)
	cookie2 := t185SSOLogin(t, st)
	if _, admin, _ := t185Whoami(t, st.ts.URL, cookie2); admin {
		t.Fatal("whoami after IdP demotion admin = true, want false (D4/O-4)")
	}
	u, err := st.md.Users().Get(context.Background(), "boss")
	if err != nil {
		t.Fatalf("Get boss: %v", err)
	}
	if u.IsAdmin {
		t.Fatal("row is_admin = true after IdP demotion, want the refresh to persist")
	}
}

// TestT185LocalUserWhoamiGroupsShape pins the D2 wire form for the local
// arm: groups renders as [] (never null) and the local memberships report.
func TestT185LocalUserWhoamiGroupsShape(t *testing.T) {
	st := newT185Stack(t, "")
	t185Seed(t, st.md)

	raw := t185Do(t, st.ts.URL, http.MethodGet, "/binflow/api/v1/session", adminUser, adminPass, "", "")
	if raw.StatusCode != http.StatusOK {
		t.Fatalf("whoami status = %d", raw.StatusCode)
	}
	body := mustGet(t, raw)
	if !strings.Contains(body, `"groups": []`) {
		t.Fatalf("local admin whoami body = %s, want groups: [] (D2 wire form)", body)
	}
	if strings.Contains(body, `"groups": null`) {
		t.Fatalf("local admin whoami body = %s, groups must never be null", body)
	}

	// A local user with a DB membership reports it on login AND on whoami.
	now := metadata.Now()
	if err := st.md.Groups().Create(context.Background(), &metadata.Group{
		Name: "locals", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed locals group: %v", err)
	}
	hash, err := auth.HashPassword("local-pw-42")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := st.md.Users().Create(context.Background(), &metadata.User{
		Username: "localone", PasswordHash: hash, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed local user: %v", err)
	}
	if err := st.md.Groups().SetUserGroups(context.Background(), "localone", []string{"locals"}); err != nil {
		t.Fatalf("seed membership: %v", err)
	}
	login := t185Do(t, st.ts.URL, http.MethodPost, "/binflow/api/v1/session", "", "", "",
		`{"username":"localone","password":"local-pw-42"}`)
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", login.StatusCode, mustGet(t, login))
	}
	var loginBody struct {
		Groups []string `json:"groups"`
	}
	if err := json.NewDecoder(login.Body).Decode(&loginBody); err != nil {
		t.Fatalf("decode login body: %v", err)
	}
	if len(loginBody.Groups) != 1 || loginBody.Groups[0] != "locals" {
		t.Fatalf("login whoami groups = %v, want [locals] (D2 via the login fill)", loginBody.Groups)
	}
}
