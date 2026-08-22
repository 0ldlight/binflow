// T-212 IdP role-rewrite acceptance surface (ADR-0026 decision 6, T-214 ④):
// the readonly group mapping keys, the authority ladder
// (admin_group > readonly_group > user) applied at claim mapping AND at the
// per-authentication row rewrite, mirror maintenance, and auto-create
// carrying the role. Zero-config (both keys empty) must behave exactly like
// the pre-M7 admin-only mapping.

package auth_test

import (
	"context"
	"encoding/json"

	"testing"
	"time"

	ldap "github.com/go-ldap/ldap/v3"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// oidcIDToken builds and signs one ID Token carrying the groups set — the
// mock-server spelling the ladder tests share.
func oidcIDToken(t *testing.T, m *mockOIDCServer, sub, user string, groups []string) string {
	t.Helper()
	now := time.Now()
	claims := map[string]any{
		"iss": m.issuer, "aud": "test-client-id", "sub": sub,
		"preferred_username": user, "groups": groups,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return m.signIDToken(string(raw))
}

// roleOfRow reads the stored role of one user row.
func roleOfRow(t *testing.T, st metadata.Store, username string) (role string, isAdmin bool) {
	t.Helper()
	u, err := st.Users().Get(context.Background(), username)
	if err != nil {
		t.Fatalf("get user %q: %v", username, err)
	}
	return u.Role, u.IsAdmin
}

// TestMapGroupsRoleLadder pins the authority ladder at the pure function
// level: admin wins over readonly, no hit is user, and the zero-config case
// (both keys empty) keeps the pre-M7 outcome for every input.
func TestMapGroupsRoleLadder(t *testing.T) {
	tests := []struct {
		name        string
		adminGroup  string
		readOnlyGrp string
		groups      []string
		want        auth.Role
	}{
		{"admin hit", "admins", "auditors", []string{"devs", "admins"}, auth.RoleAdmin},
		{"readonly hit", "admins", "auditors", []string{"devs", "auditors"}, auth.RoleReadOnlyAdmin},
		{"both hit: admin wins", "admins", "auditors", []string{"auditors", "admins"}, auth.RoleAdmin},
		{"neither hit", "admins", "auditors", []string{"devs"}, auth.RoleUser},
		{"no groups at all", "admins", "auditors", nil, auth.RoleUser},
		{"zero config, admin key only", "admins", "", []string{"admins"}, auth.RoleAdmin},
		{"zero config: no readonly key, would-be readonly group", "admins", "", []string{"auditors"}, auth.RoleUser},
		{"zero config entirely", "", "", []string{"admins", "auditors"}, auth.RoleUser},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapGroupsRoleForTest(t, tt.adminGroup, tt.readOnlyGrp, tt.groups)
			if got != tt.want {
				t.Fatalf("ladder(%q, %q, %v) = %q, want %q", tt.adminGroup, tt.readOnlyGrp, tt.groups, got, tt.want)
			}
		})
	}
}

// mapGroupsRoleForTest reaches the provider-side ladder through a signed
// token so the assertion covers the real OIDC mapping path, not a copy.
func mapGroupsRoleForTest(t *testing.T, adminGroup, readOnlyGroup string, groups []string) auth.Role {
	t.Helper()
	m := newMockOIDCServer(t)
	defer m.Close()
	cfg := m.newOIDCConfig()
	cfg.AdminGroup = adminGroup
	cfg.ReadOnlyGroup = readOnlyGroup
	prov := m.newProvider(context.Background(), t, cfg, nil)

	c, err := prov.Authenticate(context.Background(), oidcIDToken(t, m, "ladder-sub", "ladder", groups))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	return c.Role
}

// TestOIDCReadOnlyGroupMapping exercises the provider mapping through the
// Bearer arm: readonly member, both-groups member, plain member.
func TestOIDCReadOnlyGroupMapping(t *testing.T) {
	f := newFixture(t, false)
	m := newMockOIDCServer(t)
	defer m.Close()
	cfg := m.newOIDCConfig()
	cfg.AdminGroup = "admins"
	cfg.ReadOnlyGroup = "auditors"

	// Local rows exist so the arm resolves instead of auto-creating.
	makeUser(t, f.ctx, f.st.Users(), "mapper", "", false, "oidc", "map-sub")
	prov := m.newProvider(f.ctx, t, cfg, auth.NewOIDCResolver(f.st.Users()))
	svc := f.svc.WithOIDC(prov, nil)

	tests := []struct {
		name      string
		groups    []string
		wantRole  auth.Role
		wantAdmin bool
	}{
		{"readonly member", []string{"auditors"}, auth.RoleReadOnlyAdmin, false},
		{"both groups: admin wins", []string{"auditors", "admins"}, auth.RoleAdmin, true},
		{"plain member", []string{"developers"}, auth.RoleUser, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := svc.Authenticate(f.ctx, req("Bearer "+oidcIDToken(t, m, "map-sub", "mapper", tt.groups), ""))
			if err != nil {
				t.Fatalf("Authenticate: %v", err)
			}
			if p.Role != tt.wantRole || p.Admin != tt.wantAdmin {
				t.Fatalf("principal = (role %q, admin %v), want (%q, %v)", p.Role, p.Admin, tt.wantRole, tt.wantAdmin)
			}
			if got := p.EffectiveRole(); got != tt.wantRole {
				t.Fatalf("EffectiveRole = %q, want %q", got, tt.wantRole)
			}
		})
	}
}

// TestOIDCRoleRefreshAuthoritative pins the row rewrite: the provider is the
// authority on EVERY authentication — drift in any direction (including
// demotion) is corrected in place, the is_admin mirror rides the same write,
// and the request's principal carries the claims' role.
func TestOIDCRoleRefreshAuthoritative(t *testing.T) {
	f := newFixture(t, false)

	loginAs := func(t *testing.T, claims *auth.Claims, storedRole string) *auth.Principal {
		t.Helper()
		makeUser(t, f.ctx, f.st.Users(), claims.Name, "", storedRole == string(auth.RoleAdmin), "oidc", claims.ProviderID)
		prov := newAuthenticatorMockOIDCProvider()
		prov.addValidToken("tok-"+claims.Name, claims)
		prov.addResolvedUser(claims.ProviderID, auth.ProviderUser{
			Username: claims.Name, Enabled: true,
		})
		svc := f.svc.WithOIDC(prov, nil)
		p, err := svc.Authenticate(f.ctx, req("Bearer tok-"+claims.Name, ""))
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
		return p
	}

	t.Run("readonly claims rewrite a user row", func(t *testing.T) {
		p := loginAs(t, &auth.Claims{Name: "aud", Groups: nil, Role: auth.RoleReadOnlyAdmin, ProviderID: "sub-ro"},
			string(auth.RoleUser))
		if p.Role != auth.RoleReadOnlyAdmin || p.Admin {
			t.Fatalf("principal = (role %q, admin %v), want readonly_admin", p.Role, p.Admin)
		}
		role, isAdmin := roleOfRow(t, f.st, "aud")
		if role != string(auth.RoleReadOnlyAdmin) || isAdmin {
			t.Fatalf("row = (role %q, is_admin %v), want (readonly_admin, false)", role, isAdmin)
		}
	})

	t.Run("user claims demote a drifted admin row", func(t *testing.T) {
		p := loginAs(t, &auth.Claims{Name: "demoted", Groups: nil, ProviderID: "sub-dem"},
			string(auth.RoleAdmin))
		if p.Role != auth.RoleUser || p.Admin {
			t.Fatalf("principal = (role %q, admin %v), want user", p.Role, p.Admin)
		}
		role, isAdmin := roleOfRow(t, f.st, "demoted")
		if role != string(auth.RoleUser) || isAdmin {
			t.Fatalf("row = (role %q, is_admin %v), want (user, false) — the provider is the authority", role, isAdmin)
		}
	})

	t.Run("admin claims (pre-M7 boolean) promote", func(t *testing.T) {
		p := loginAs(t, &auth.Claims{Name: "promoted", Groups: nil, Admin: true, ProviderID: "sub-pro"},
			string(auth.RoleUser))
		if p.Role != auth.RoleAdmin || !p.Admin {
			t.Fatalf("principal = (role %q, admin %v), want admin", p.Role, p.Admin)
		}
		role, isAdmin := roleOfRow(t, f.st, "promoted")
		if role != string(auth.RoleAdmin) || !isAdmin {
			t.Fatalf("row = (role %q, is_admin %v), want (admin, true)", role, isAdmin)
		}
	})
}

// TestOIDCAutoCreateCarriesRole: the first-login row lands with the claims'
// role, readonly_admin included, and the mirror agrees.
func TestOIDCAutoCreateCarriesRole(t *testing.T) {
	f := newFixture(t, false)
	prov := newAuthenticatorMockOIDCProvider()
	prov.addValidToken("ro-first", &auth.Claims{
		Name: "ro-fresh", Groups: nil, Role: auth.RoleReadOnlyAdmin, ProviderID: "sub-ro-fresh",
	})
	svc := f.svc.WithOIDC(prov, auth.NewUserCreator(f.st.Users()))
	p, err := svc.Authenticate(f.ctx, req("Bearer ro-first", ""))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if p.Role != auth.RoleReadOnlyAdmin || p.Admin {
		t.Fatalf("principal = (role %q, admin %v), want readonly_admin", p.Role, p.Admin)
	}
	role, isAdmin := roleOfRow(t, f.st, "ro-fresh")
	if role != string(auth.RoleReadOnlyAdmin) || isAdmin {
		t.Fatalf("auto-created row = (role %q, is_admin %v), want (readonly_admin, false)", role, isAdmin)
	}
}

// TestLDAPReadOnlyGroupMapping: the directory mapping resolves the ladder at
// bind time (group DNs, the auth.ldap.readonly_group key's shape).
func TestLDAPReadOnlyGroupMapping(t *testing.T) {
	newDir := func() *mockLDAPConn {
		mock := newMockLDAPConn()
		mock.addUser("cn=admin,dc=example,dc=com", "adminpass", map[string][]string{
			"uid": {"admin"}, "objectClass": {"posixAccount"},
		})
		mock.addUser("uid=alice,dc=example,dc=com", "alicepass", map[string][]string{
			"uid": {"alice"}, "objectClass": {"posixAccount"},
		})
		mock.addGroup("cn=binflow-admin,dc=example,dc=com", map[string][]string{
			"cn": {"binflow-admin"}, "objectClass": {"posixGroup"}, "memberUid": {"alice"},
		})
		mock.addGroup("cn=binflow-readonly,dc=example,dc=com", map[string][]string{
			"cn": {"binflow-readonly"}, "objectClass": {"posixGroup"}, "memberUid": {"alice"},
		})
		return mock
	}

	tests := []struct {
		name      string
		cfg       func(*auth.LDAPConfig)
		wantRole  auth.Role
		wantAdmin bool
	}{
		{
			name: "readonly group only",
			cfg: func(c *auth.LDAPConfig) {
				c.AdminGroup = ""
				c.ReadOnlyGroup = "cn=binflow-readonly,dc=example,dc=com"
			},
			wantRole:  auth.RoleReadOnlyAdmin,
			wantAdmin: false,
		},
		{
			name: "both groups: admin wins",
			cfg: func(c *auth.LDAPConfig) {
				c.AdminGroup = "cn=binflow-admin,dc=example,dc=com"
				c.ReadOnlyGroup = "cn=binflow-readonly,dc=example,dc=com"
			},
			wantRole:  auth.RoleAdmin,
			wantAdmin: true,
		},
		{
			name:      "no keys configured: pre-M7 behavior",
			cfg:       func(*auth.LDAPConfig) {},
			wantRole:  auth.RoleUser,
			wantAdmin: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newDir()
			dialer := func(_ context.Context, _ string, _ ...ldap.DialOpt) (auth.LDAPConn, error) {
				mock.bound = false
				mock.boundDN = ""
				mock.closed = false
				return mock, nil
			}
			cfg := &auth.LDAPConfig{
				Enabled: true, URL: "ldap://ldap.example.com:389",
				BaseDN:        "dc=example,dc=com",
				BindDN:        "cn=admin,dc=example,dc=com",
				BindPassword:  "adminpass",
				UserFilter:    "(&(objectClass=posixAccount)(uid=%s))",
				UserIDAttr:    "uid",
				GroupFilter:   "(&(objectClass=posixGroup)(memberUid=%s))",
				GroupNameAttr: "cn",
				PoolSize:      1,
			}
			tt.cfg(cfg)
			prov, err := auth.NewLDAPProvider(cfg, nil, dialer)
			if err != nil {
				t.Fatalf("NewLDAPProvider: %v", err)
			}
			defer prov.Close()

			claims, err := prov.Bind(context.Background(), "alice", "alicepass")
			if err != nil {
				t.Fatalf("Bind: %v", err)
			}
			if claims.Role != tt.wantRole || claims.Admin != tt.wantAdmin {
				t.Fatalf("claims = (role %q, admin %v), want (%q, %v)", claims.Role, claims.Admin, tt.wantRole, tt.wantAdmin)
			}
		})
	}
}

// TestLDAPLoginRoleRefresh drives the full login arm (AuthenticateCredentials
// → bind → resolve → refresh): a readonly-group directory rewrites the local
// row and the login principal carries readonly_admin end to end.
func TestLDAPLoginRoleRefresh(t *testing.T) {
	f := newFixture(t, false)
	makeUser(t, f.ctx, f.st.Users(), "alice", "", false, "ldap", "uid=alice,dc=example,dc=com")

	mock := newMockLDAPConn()
	mock.addUser("cn=admin,dc=example,dc=com", "adminpass", map[string][]string{
		"uid": {"admin"}, "objectClass": {"posixAccount"},
	})
	mock.addUser("uid=alice,dc=example,dc=com", "alicepass", map[string][]string{
		"uid": {"alice"}, "objectClass": {"posixAccount"},
	})
	mock.addGroup("cn=binflow-readonly,dc=example,dc=com", map[string][]string{
		"cn": {"binflow-readonly"}, "objectClass": {"posixGroup"}, "memberUid": {"alice"},
	})
	dialer := func(_ context.Context, _ string, _ ...ldap.DialOpt) (auth.LDAPConn, error) {
		mock.bound = false
		mock.boundDN = ""
		mock.closed = false
		return mock, nil
	}
	cfg := &auth.LDAPConfig{
		Enabled: true, URL: "ldap://ldap.example.com:389",
		BaseDN: "dc=example,dc=com",
		BindDN: "cn=admin,dc=example,dc=com", BindPassword: "adminpass",
		UserFilter:    "(&(objectClass=posixAccount)(uid=%s))",
		UserIDAttr:    "uid",
		GroupFilter:   "(&(objectClass=posixGroup)(memberUid=%s))",
		GroupNameAttr: "cn",
		ReadOnlyGroup: "cn=binflow-readonly,dc=example,dc=com",
		PoolSize:      1,
	}
	ldapProv, err := auth.NewLDAPProvider(cfg, auth.NewLDAPResolver(f.st.Users()), dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	t.Cleanup(ldapProv.Close)
	svc := f.svc.WithLDAP(ldapProv)

	p, err := svc.AuthenticateCredentials(f.ctx, "alice", "alicepass")
	if err != nil {
		t.Fatalf("AuthenticateCredentials: %v", err)
	}
	if p.Role != auth.RoleReadOnlyAdmin || p.Admin {
		t.Fatalf("principal = (role %q, admin %v), want readonly_admin", p.Role, p.Admin)
	}
	if !svc.CanManage(f.ctx, p, auth.CapSystemRead) {
		t.Fatal("the readonly login must hold the read capabilities")
	}
	if svc.CanManage(f.ctx, p, auth.CapSecurityWrite) {
		t.Fatal("the readonly login must not hold write capabilities")
	}
	role, isAdmin := roleOfRow(t, f.st, "alice")
	if role != string(auth.RoleReadOnlyAdmin) || isAdmin {
		t.Fatalf("row = (role %q, is_admin %v), want (readonly_admin, false)", role, isAdmin)
	}
}
