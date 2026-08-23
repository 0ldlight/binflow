// T-157 acceptance surface, arm 2 (PRD FR-55/FR-56 / LD-01, H30~H37):
// POST /api/v1/session authenticates local-first with LDAP fallback — the
// HTTP plane consumes the T-156 seam (AuthenticateCredentials) end to end
// through a real auth.Service whose LDAP arm is backed by a real
// auth.LDAPProvider over a mock directory (the same conn/dialer mock shape
// internal/auth/ldap_test.go uses).

package httpapi_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ldap "github.com/go-ldap/ldap/v3"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// ---- mock directory ----

const (
	mockBaseDN     = "dc=example,dc=com"
	mockAdminGroup = "cn=admins,ou=groups,dc=example,dc=com"
)

// mockDirectory is an in-memory directory: uid-keyed users plus cn-keyed
// groups (memberUid lists). The mock conn below answers the exact search
// shapes auth.LDAPProvider emits.
type mockDirectory struct {
	users  map[string]mockDirUser // uid -> entry
	groups map[string][]string    // cn -> member uids
}

type mockDirUser struct {
	dn, password string
}

func userDN(uid string) string { return "uid=" + uid + ",ou=people," + mockBaseDN }

func groupDN(cn string) string { return "cn=" + cn + ",ou=groups," + mockBaseDN }

// mockLDAPConn implements auth.LDAPConn over the in-memory directory.
type mockLDAPConn struct{ dir *mockDirectory }

func (c *mockLDAPConn) Bind(dn, password string) error {
	for _, u := range c.dir.users {
		if u.dn == dn {
			if password == u.password {
				return nil
			}
			return ldap.NewError(ldap.LDAPResultInvalidCredentials, errors.New("bad password"))
		}
	}
	return ldap.NewError(ldap.LDAPResultInvalidCredentials, errors.New("no such user"))
}

func (c *mockLDAPConn) Search(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
	var entries []*ldap.Entry
	switch {
	case req.Filter == "(objectClass=*)":
		// Base-object lookup: the AdminGroup DN's name attribute
		// (LDAPProvider.adminGroupNameFromDN).
		for cn := range c.dir.groups {
			if strings.EqualFold(groupDN(cn), req.BaseDN) {
				entries = append(entries, ldap.NewEntry(groupDN(cn), map[string][]string{"cn": {cn}}))
			}
		}
	case strings.Contains(req.Filter, "memberUid="):
		// Group search: groups whose memberUid list names the user. Checked
		// BEFORE the uid match — "memberUid=" embeds no case-sensitive
		// "uid=", but ordering keeps the mock's intent explicit.
		member := filterValue(req.Filter, "memberUid")
		for cn, members := range c.dir.groups {
			for _, m := range members {
				if m == member {
					entries = append(entries,
						ldap.NewEntry(groupDN(cn), map[string][]string{"cn": {cn}, "memberUid": members}))
				}
			}
		}
	default:
		// User search: exactly one entry per uid (resolveUserDN rejects
		// multi-match directories).
		if uid := filterValue(req.Filter, "uid"); uid != "" {
			if _, ok := c.dir.users[uid]; ok {
				entries = append(entries,
					ldap.NewEntry(userDN(uid), map[string][]string{"uid": {uid}, "objectClass": {"posixAccount"}}))
			}
		}
	}
	return &ldap.SearchResult{Entries: entries}, nil
}

func (c *mockLDAPConn) StartTLS(_ *tls.Config) error { return nil }

func (c *mockLDAPConn) Close() error { return nil }

func (c *mockLDAPConn) SetTimeout(_ time.Duration) {}

// ldapDialFaults injects the failure arms of the httpapi LDAP dialer mock.
type ldapDialFaults struct {
	err         error // non-nil: every dial fails with err (dial outage)
	upgradeFail bool  // hand out startTLSFailConn instead (StartTLS refusal)
}

// ldapTestDialer is the httpapi-side constructor for the auth.LDAPDialer
// seam (FR-77 AC5 / T-233): it hands out a FRESH mockLDAPConn over dir on
// every dial — this mock keeps no shared bookkeeping, so nothing resets.
// It replaces the per-file inline dialer closures; the old blank-parameter
// dialer signature must stay at ZERO grep hits under internal/.
func ldapTestDialer(dir *mockDirectory, faults ldapDialFaults) auth.LDAPDialer {
	return func(context.Context, string, ...ldap.DialOpt) (auth.LDAPConn, error) {
		if faults.err != nil {
			return nil, faults.err
		}
		if faults.upgradeFail {
			return startTLSFailConn{&mockLDAPConn{dir: dir}}, nil
		}
		return &mockLDAPConn{dir: dir}, nil
	}
}

// filterValue extracts the value of attr= from an LDAP filter term
// ("(uid=jdoe)" -> "jdoe").
func filterValue(filter, attr string) string {
	i := strings.Index(filter, attr+"=")
	if i < 0 {
		return ""
	}
	rest := filter[i+len(attr)+1:]
	if j := strings.IndexByte(rest, ')'); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// ---- assembly ----

// ldapStack is a session plane over a real auth.Service whose LDAP arm is
// the real auth.LDAPProvider on the mock directory — the exact wiring cmd
// will assemble from auth.ldap.enabled.
type ldapStack struct {
	ts  *httptest.Server
	md  metadata.Store
	dir *mockDirectory
}

// newLDAPStack builds the stack. wireLDAP=false assembles the pre-M6
// service (no WithLDAP) to pin the fallback-off behavior.
func newLDAPStack(t *testing.T, wireLDAP bool) *ldapStack {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	dir := &mockDirectory{
		users: map[string]mockDirUser{
			"jdoe": {dn: userDN("jdoe"), password: "jdoe-secret"},
			"boss": {dn: userDN("boss"), password: "boss-secret"},
		},
		groups: map[string][]string{
			"developers": {"jdoe"},
			"admins":     {"boss"},
		},
	}

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	if wireLDAP {
		provider, err := auth.NewLDAPProvider(&auth.LDAPConfig{
			Enabled:        true,
			URL:            "ldap://ldap.example.com:389",
			BaseDN:         mockBaseDN,
			UserFilter:     "(uid=%s)",
			UserIDAttr:     "uid",
			GroupFilter:    "(&(objectClass=posixGroup)(memberUid=%s))",
			GroupNameAttr:  "cn",
			AdminGroup:     mockAdminGroup,
			PoolSize:       2,
			ConnectTimeout: 2 * time.Second,
			RequestTimeout: 5 * time.Second,
		}, auth.NewLDAPResolver(md.Users()),
			ldapTestDialer(dir, ldapDialFaults{}))
		if err != nil {
			t.Fatalf("NewLDAPProvider: %v", err)
		}
		authSvc = authSvc.WithLDAP(provider)
	}

	// Local seed rows: a plain local account (alice) and a disabled LDAP
	// row (frozen) whose directory bind would succeed — the resolve path
	// must still refuse it.
	for _, u := range []struct {
		name, hash, provider, providerID string
		admin, enabled                   bool
	}{
		{"alice", mustHash(t, "alice-local-pw"), "local", "", false, true},
		{"frozen", "", "ldap", userDN("frozen"), false, false},
	} {
		now := metadata.Now()
		if err := md.Users().Create(ctx, &metadata.User{
			Username: u.name, PasswordHash: u.hash, IsAdmin: u.admin, Enabled: u.enabled,
			CreatedAt: now, UpdatedAt: now, Provider: u.provider, ProviderID: u.providerID,
		}); err != nil {
			t.Fatalf("seed user %s: %v", u.name, err)
		}
	}
	dir.users["frozen"] = mockDirUser{dn: userDN("frozen"), password: "frozen-secret"}

	s := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Auth:     authSvc,
		Authz:    authSvc,
		Metadata: md,
		Repos:    md.Repos(),
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &ldapStack{ts: ts, md: md, dir: dir}
}

func mustHash(t *testing.T, pw string) string {
	t.Helper()
	h, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	return h
}

// ldapLogin posts one JSON login body and returns the response.
func ldapLogin(t *testing.T, tsURL, user, pass string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	req, err := http.NewRequest(http.MethodPost, tsURL+"/binflow/api/v1/session", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("build login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login %s: %v", user, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// ---- LD-01 / H30~H32, H36~H37: local first, LDAP fallback ----

func TestLDAPSessionLoginTable(t *testing.T) {
	st := newLDAPStack(t, true)
	ctx := context.Background()

	cases := []struct {
		name string
		user string
		pass string
		want int
		// wantAdmin asserts the whoami admin flag after a successful login.
		wantAdmin bool
		// wantRow asserts the post-login row shape of wantRowUser.
		wantRowUser string
		wantRowProv string
	}{
		{
			name: "ldap-only user first login falls back to bind", user: "jdoe", pass: "jdoe-secret",
			want: http.StatusOK, wantRowUser: "jdoe", wantRowProv: "ldap",
		},
		{
			name: "ldap user wrong password is a uniform 401", user: "jdoe", pass: "wrong",
			want: http.StatusUnauthorized,
		},
		{
			name: "ldap admin group member maps to admin", user: "boss", pass: "boss-secret",
			want: http.StatusOK, wantAdmin: true, wantRowUser: "boss", wantRowProv: "ldap",
		},
		{
			name: "local user stays on the local arm", user: "alice", pass: "alice-local-pw",
			want: http.StatusOK, wantRowUser: "alice", wantRowProv: "local",
		},
		{
			name: "local user wrong password gets no LDAP rescue", user: "alice", pass: "wrong",
			want: http.StatusUnauthorized,
		},
		{
			name: "disabled ldap row refuses despite a valid bind", user: "frozen", pass: "frozen-secret",
			want: http.StatusUnauthorized,
		},
		{
			name: "unknown everywhere", user: "ghost", pass: "whatever",
			want: http.StatusUnauthorized,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := ldapLogin(t, st.ts.URL, tc.user, tc.pass)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d, body=%s", resp.StatusCode, tc.want, mustGet(t, resp))
			}
			if tc.want != http.StatusOK {
				// No failure path may mint a session cookie.
				if c := responseCookie(resp, auth.CookieSessionName); c != nil {
					t.Errorf("failed login minted a session cookie: %q", c.raw)
				}
				return
			}

			// The minted session works on the whoami plane (H30) with the
			// cookie contract of the password login.
			c := responseCookie(resp, auth.CookieSessionName)
			if c == nil || c.value == "" {
				t.Fatalf("login carries no session cookie: %q", resp.Header.Values("Set-Cookie"))
			}
			if c.attrs["httponly"] != "" || !strings.Contains(strings.ToLower(c.raw), "samesite=lax") {
				t.Errorf("session cookie attributes missing: %q", c.raw)
			}
			who := oidcDo(t, st.ts.URL, http.MethodGet, "/binflow/api/v1/session",
				auth.CookieSessionName+"="+c.value)
			if who.StatusCode != http.StatusOK {
				t.Fatalf("whoami status = %d, body=%s", who.StatusCode, mustGet(t, who))
			}
			var body struct {
				Username string `json:"username"`
				Admin    bool   `json:"admin"`
			}
			if err := json.NewDecoder(who.Body).Decode(&body); err != nil {
				t.Fatalf("decode whoami: %v", err)
			}
			if body.Username != tc.user {
				t.Errorf("whoami username = %q, want %q", body.Username, tc.user)
			}
			if body.Admin != tc.wantAdmin {
				t.Errorf("whoami admin = %v, want %v", body.Admin, tc.wantAdmin)
			}

			// Row shape: provider columns per ADR-0020 (H31 source visible
			// in the store; the whoami source field is a T-174 concern).
			u, err := st.md.Users().Get(ctx, tc.wantRowUser)
			if err != nil {
				t.Fatalf("user row %s missing: %v", tc.wantRowUser, err)
			}
			if u.Provider != tc.wantRowProv {
				t.Errorf("user %s provider = %q, want %q", tc.wantRowUser, u.Provider, tc.wantRowProv)
			}
			if tc.wantRowProv != "local" && u.PasswordHash != "" {
				t.Errorf("user %s has a local password hash; federated users must not", tc.wantRowUser)
			}
			if !u.Enabled {
				t.Errorf("user %s is disabled after a successful login", tc.wantRowUser)
			}
		})
	}
}

// TestLDAPNotWiredKeepsLocalOnly: without WithLDAP the login endpoint keeps
// the pre-M6 behavior — a directory-valid credential is still a 401 and no
// row is auto-created.
func TestLDAPNotWiredKeepsLocalOnly(t *testing.T) {
	st := newLDAPStack(t, false)

	resp := ldapLogin(t, st.ts.URL, "jdoe", "jdoe-secret")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body=%s", resp.StatusCode, mustGet(t, resp))
	}
	if _, err := st.md.Users().Get(context.Background(), "jdoe"); err == nil {
		t.Error("jdoe row auto-created without the LDAP arm wired")
	}
}
