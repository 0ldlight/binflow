// T-185 acceptance surface (T-174 defect D4, PRD FR-54-AC5/OD-05 and
// FR-55/LD-02): the IdP group membership lands in user_groups on every
// provider authentication (replace semantics — removals expire with the
// next login), unmateralized IdP groups are skipped, a nil claims group
// list never clobbers memberships, is_admin refreshes per authentication
// instead of being frozen at first login, and the session arm carries the
// synced groups after the login request is gone.

package auth_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// makeGroup seeds one local group row (the H28 posture: an admin
// materialized the IdP's group and attached permissions to it).
func makeGroup(t *testing.T, st metadata.Store, name string) {
	t.Helper()
	now := metadata.Now()
	if err := st.Groups().Create(context.Background(), &metadata.Group{
		Name: name, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create group %q: %v", name, err)
	}
}

// groupsOf reports the user's synced membership as a sorted name list.
func groupsOf(t *testing.T, st metadata.Store, username string) []string {
	t.Helper()
	rows, err := st.Groups().GroupsOfUser(context.Background(), username)
	if err != nil {
		t.Fatalf("GroupsOfUser(%q): %v", username, err)
	}
	names := make([]string, 0, len(rows))
	for _, g := range rows {
		names = append(names, g.Name)
	}
	sort.Strings(names)
	return names
}

// seedOIDCUser installs a pre-existing oidc-owned row the mock provider
// resolves, plus the local groups the claims will reference.
func seedOIDCUser(t *testing.T, f *fixture, username, sub string, admin bool) {
	t.Helper()
	makeUser(f.ctx, t, f.st.Users(), username, "", admin, "oidc", sub)
}

// TestOIDCGroupsSyncToUserGroups is the core D4 fix: the Bearer arm's claims
// groups are MATERIALIZED into user_groups (only the materialized subset —
// an IdP group with no local row grants nothing and breaks nothing).
func TestOIDCGroupsSyncToUserGroups(t *testing.T) {
	f := newFixture(t, true)
	makeGroup(t, f.st, "developers") // ghost-group deliberately NOT created
	seedOIDCUser(t, f, "oidc-user", "sub-1", false)

	prov := newAuthenticatorMockOIDCProvider()
	prov.addValidToken("tok", &auth.Claims{
		Name: "oidc-user", Groups: []string{"developers", "ghost-group"},
		Admin: false, ProviderID: "sub-1",
	})
	prov.addResolvedUser("sub-1", auth.ProviderUser{
		Username: "oidc-user", IsAdmin: false, Enabled: true,
	})
	svc := f.svc.WithOIDC(prov, nil)

	p, err := svc.Authenticate(f.ctx, req("Bearer tok", ""))
	if err != nil {
		t.Fatalf("Authenticate = %v", err)
	}
	// The Principal keeps the raw claims set (the pre-T-185 Bearer behavior
	// T-174 H28 verified as correct); the DB keeps the materialized subset.
	if len(p.Groups) != 2 {
		t.Errorf("principal groups = %v, want the raw claims set [developers ghost-group]", p.Groups)
	}
	got := groupsOf(t, f.st, "oidc-user")
	if len(got) != 1 || got[0] != "developers" {
		t.Fatalf("user_groups = %v, want [developers] (ghost-group has no local row)", got)
	}

	// Auto-created rows get the same sync: first login writes the
	// membership together with the row.
	prov2 := newAuthenticatorMockOIDCProvider()
	prov2.addValidToken("tok2", &auth.Claims{
		Name: "fresh-user", Groups: []string{"developers"}, Admin: false, ProviderID: "sub-2",
	})
	svc2 := f.svc.WithOIDC(prov2, auth.NewUserCreator(f.st.Users()))
	if _, err := svc2.Authenticate(f.ctx, req("Bearer tok2", "")); err != nil {
		t.Fatalf("Authenticate(first login) = %v", err)
	}
	if got := groupsOf(t, f.st, "fresh-user"); len(got) != 1 || got[0] != "developers" {
		t.Fatalf("auto-created user_groups = %v, want [developers]", got)
	}
}

// TestOIDCGroupSyncReplaceOnRemoval pins the replace semantics (PRD
// FR-54-AC5/H28): a group the IdP removed disappears from user_groups at
// the next authentication — group-derived permissions expire immediately.
func TestOIDCGroupSyncReplaceOnRemoval(t *testing.T) {
	f := newFixture(t, true)
	makeGroup(t, f.st, "developers")
	makeGroup(t, f.st, "qa")
	seedOIDCUser(t, f, "oidc-user", "sub-1", false)

	prov := newAuthenticatorMockOIDCProvider()
	prov.addResolvedUser("sub-1", auth.ProviderUser{
		Username: "oidc-user", IsAdmin: false, Enabled: true,
	})
	svc := f.svc.WithOIDC(prov, nil)

	prov.addValidToken("t1", &auth.Claims{
		Name: "oidc-user", Groups: []string{"developers", "qa"}, ProviderID: "sub-1",
	})
	if _, err := svc.Authenticate(f.ctx, req("Bearer t1", "")); err != nil {
		t.Fatalf("Authenticate(t1) = %v", err)
	}
	if got := groupsOf(t, f.st, "oidc-user"); len(got) != 2 {
		t.Fatalf("membership after first auth = %v, want [developers qa]", got)
	}

	// The IdP dropped the user from qa.
	prov.addValidToken("t2", &auth.Claims{
		Name: "oidc-user", Groups: []string{"developers"}, ProviderID: "sub-1",
	})
	if _, err := svc.Authenticate(f.ctx, req("Bearer t2", "")); err != nil {
		t.Fatalf("Authenticate(t2) = %v", err)
	}
	got := groupsOf(t, f.st, "oidc-user")
	if len(got) != 1 || got[0] != "developers" {
		t.Fatalf("membership after removal = %v, want [developers] only", got)
	}
}

// TestOIDCGroupSyncNilClaimNoClobber pins the nil-vs-empty distinction: a
// nil claims list (groups_claim absent / GroupFilter unset — "no group
// information") must not touch the membership, while a present-but-empty
// list ("groups": [], a genuine zero set) clears it.
func TestOIDCGroupSyncNilClaimNoClobber(t *testing.T) {
	f := newFixture(t, true)
	makeGroup(t, f.st, "qa")
	seedOIDCUser(t, f, "oidc-user", "sub-1", false)
	// An admin-assigned local membership the nil-claim case must survive.
	if err := f.st.Groups().SetUserGroups(f.ctx, "oidc-user", []string{"qa"}); err != nil {
		t.Fatalf("seed membership: %v", err)
	}

	prov := newAuthenticatorMockOIDCProvider()
	prov.addResolvedUser("sub-1", auth.ProviderUser{
		Username: "oidc-user", IsAdmin: false, Enabled: true,
	})
	svc := f.svc.WithOIDC(prov, nil)

	prov.addValidToken("nil-claim", &auth.Claims{
		Name: "oidc-user", Groups: nil, ProviderID: "sub-1",
	})
	if _, err := svc.Authenticate(f.ctx, req("Bearer nil-claim", "")); err != nil {
		t.Fatalf("Authenticate(nil-claim) = %v", err)
	}
	if got := groupsOf(t, f.st, "oidc-user"); len(got) != 1 || got[0] != "qa" {
		t.Fatalf("nil claims clobbered the membership: %v, want [qa]", got)
	}

	prov.addValidToken("empty-claim", &auth.Claims{
		Name: "oidc-user", Groups: []string{}, ProviderID: "sub-1",
	})
	if _, err := svc.Authenticate(f.ctx, req("Bearer empty-claim", "")); err != nil {
		t.Fatalf("Authenticate(empty-claim) = %v", err)
	}
	if got := groupsOf(t, f.st, "oidc-user"); len(got) != 0 {
		t.Fatalf("explicit zero set did not clear the membership: %v, want []", got)
	}
}

// resolvingOIDCProvider delegates Resolve to the real store adapter so the
// is_admin assertions read the LIVE row — the static mock keeps answering
// the first-login snapshot, which would hide exactly the refresh drift
// this test exists to catch.
type resolvingOIDCProvider struct {
	*authenticatorMockOIDCProvider
	resolver auth.OIDCUserResolver
}

func (m *resolvingOIDCProvider) Resolve(ctx context.Context, p auth.Provider, id string) (*auth.ProviderUser, error) {
	return m.resolver.GetByProvider(ctx, p, id)
}

// TestOIDCAdminRefreshEveryAuth closes the "first login freezes is_admin"
// half of D4 (ADR-0020: the provider refreshes the flag on EVERY
// authentication — IdP demotion must take effect).
func TestOIDCAdminRefreshEveryAuth(t *testing.T) {
	f := newFixture(t, true)
	seedOIDCUser(t, f, "oidc-user", "sub-1", false)

	inner := newAuthenticatorMockOIDCProvider()
	prov := &resolvingOIDCProvider{
		authenticatorMockOIDCProvider: inner,
		resolver:                      auth.NewOIDCResolver(f.st.Users()),
	}
	svc := f.svc.WithOIDC(prov, nil)

	// Promotion: the IdP put the user in the admin group.
	inner.addValidToken("promote", &auth.Claims{
		Name: "oidc-user", Admin: true, ProviderID: "sub-1",
	})
	p, err := svc.Authenticate(f.ctx, req("Bearer promote", ""))
	if err != nil {
		t.Fatalf("Authenticate(promote) = %v", err)
	}
	if !p.Admin {
		t.Fatal("principal admin = false after promotion, want true")
	}
	u, err := f.st.Users().Get(f.ctx, "oidc-user")
	if err != nil {
		t.Fatalf("Get user: %v", err)
	}
	if !u.IsAdmin {
		t.Fatal("row is_admin = false after promotion, want the refresh to persist")
	}

	// Demotion: the IdP removed the user again — the next authentication
	// must demote both the row and the principal.
	inner.addValidToken("demote", &auth.Claims{
		Name: "oidc-user", Admin: false, ProviderID: "sub-1",
	})
	p, err = svc.Authenticate(f.ctx, req("Bearer demote", ""))
	if err != nil {
		t.Fatalf("Authenticate(demote) = %v", err)
	}
	if p.Admin {
		t.Fatal("principal admin = true after demotion, want false")
	}
	u, err = f.st.Users().Get(f.ctx, "oidc-user")
	if err != nil {
		t.Fatalf("Get user: %v", err)
	}
	if u.IsAdmin {
		t.Fatal("row is_admin = true after demotion, want the refresh to persist")
	}
}

// TestSessionArmCarriesSyncedGroups is D4's session half at the auth layer:
// after a Bearer-arm login synced the membership, a plain session-cookie
// authentication (the arm that rebuilds the Principal from the user row)
// resolves the groups through the standard fill.
func TestSessionArmCarriesSyncedGroups(t *testing.T) {
	f := newFixture(t, true)
	makeGroup(t, f.st, "developers")
	seedOIDCUser(t, f, "oidc-user", "sub-1", false)

	prov := newAuthenticatorMockOIDCProvider()
	prov.addValidToken("tok", &auth.Claims{
		Name: "oidc-user", Groups: []string{"developers"}, ProviderID: "sub-1",
	})
	prov.addResolvedUser("sub-1", auth.ProviderUser{
		Username: "oidc-user", IsAdmin: false, Enabled: true,
	})
	svc := f.svc.WithOIDC(prov, nil)

	if _, err := svc.Authenticate(f.ctx, req("Bearer tok", "")); err != nil {
		t.Fatalf("Authenticate(bearer) = %v", err)
	}
	sess, err := svc.IssueSession(f.ctx, "oidc-user", time.Hour)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	p, err := svc.Authenticate(f.ctx, sessionReq(sess.ID))
	if err != nil {
		t.Fatalf("Authenticate(session) = %v", err)
	}
	if !p.ViaSession || p.Source != auth.ProviderOIDC {
		t.Fatalf("principal = viaSession %v source %v, want the oidc session arm", p.ViaSession, p.Source)
	}
	if len(p.Groups) != 1 || p.Groups[0] != "developers" {
		t.Fatalf("session principal groups = %v, want [developers] from the synced user_groups", p.Groups)
	}
}

// TestLDAPLoginSyncsGroups is the LDAP leg of D4 (LD-02): the directory's
// group search lands in user_groups at login, directory-side removal
// expires at the next login, and the login Principal itself reports the
// groups (the fill AuthenticateCredentials applies since T-185).
func TestLDAPLoginSyncsGroups(t *testing.T) {
	f := newFixture(t, true)
	makeGroup(t, f.st, "developers")
	makeGroup(t, f.st, "binflow-admins")
	makeUser(f.ctx, t, f.st.Users(), "jdoe", "", false, "ldap", "uid=jdoe,ou=people,dc=example,dc=org")

	mock := newMockLDAPConn()
	mock.addUser("uid=jdoe,ou=people,dc=example,dc=org", "jdoe-secret-42", map[string][]string{
		"uid": {"jdoe"}, "objectClass": {"inetOrgPerson"},
	})
	addMockGroup := func(dn string, memberUIDs ...string) {
		mock.addGroup(dn, map[string][]string{
			"objectClass": {"groupOfNames"},
			"cn":          {dnToCN(dn)},
			"memberUid":   memberUIDs,
		})
	}
	addMockGroup("cn=developers,ou=groups,dc=example,dc=org", "jdoe")

	dialer := mockDialer(mock, mockDialKeepState())
	prov, err := auth.NewLDAPProvider(&auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://ldap.example.com:389",
		BaseDN:        "dc=example,dc=org",
		UserFilter:    "(uid=%s)",
		GroupFilter:   "(&(objectClass=groupOfNames)(memberUid=%s))",
		GroupNameAttr: "cn",
		AdminGroup:    "cn=binflow-admins,ou=groups,dc=example,dc=org",
	}, auth.NewLDAPResolver(f.st.Users()), dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	t.Cleanup(prov.Close)
	svc := f.svc.WithLDAP(prov)

	p, err := svc.AuthenticateCredentials(f.ctx, "jdoe", "jdoe-secret-42")
	if err != nil {
		t.Fatalf("AuthenticateCredentials = %v", err)
	}
	if len(p.Groups) != 1 || p.Groups[0] != "developers" {
		t.Fatalf("login principal groups = %v, want [developers]", p.Groups)
	}
	if got := groupsOf(t, f.st, "jdoe"); len(got) != 1 || got[0] != "developers" {
		t.Fatalf("user_groups = %v, want [developers]", got)
	}

	// Directory-side removal: the next login drops the membership, and the
	// admin-group membership the directory grants refreshes is_admin.
	addMockGroup("cn=binflow-admins,ou=groups,dc=example,dc=org", "jdoe")
	mock.groups["cn=developers,ou=groups,dc=example,dc=org"] = mockLDAPGroup{
		attributes: map[string][]string{
			"objectClass": {"groupOfNames"}, "cn": {"developers"},
		},
	}
	p, err = svc.AuthenticateCredentials(f.ctx, "jdoe", "jdoe-secret-42")
	if err != nil {
		t.Fatalf("AuthenticateCredentials(second login) = %v", err)
	}
	if !p.Admin {
		t.Fatal("second login principal admin = false, want the admin-group refresh")
	}
	if got := groupsOf(t, f.st, "jdoe"); len(got) != 1 || got[0] != "binflow-admins" {
		t.Fatalf("user_groups after directory change = %v, want [binflow-admins]", got)
	}
	u, err := f.st.Users().Get(f.ctx, "jdoe")
	if err != nil {
		t.Fatalf("Get user: %v", err)
	}
	if !u.IsAdmin {
		t.Fatal("row is_admin = false after the admin-group login, want the refresh to persist")
	}
}

// dnToCN pulls the cn component of a test DN ("cn=x,ou=..." -> "x").
func dnToCN(dn string) string {
	for _, part := range splitDN(dn) {
		if k, v, ok := cutAttr(part); ok && k == "cn" {
			return v
		}
	}
	return dn
}

// splitDN splits a DN on unescaped commas — test fixtures only, no escaping
// in the values used here.
func splitDN(dn string) []string {
	var out []string
	start := 0
	for i := 0; i < len(dn); i++ {
		if dn[i] == ',' {
			out = append(out, dn[start:i])
			start = i + 1
		}
	}
	return append(out, dn[start:])
}

// cutAttr splits "k=v" (test-fixture helper).
func cutAttr(s string) (k, v string, ok bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}
