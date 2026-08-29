package auth_test

// T-346 (FR-113.4, the T-305 leftover): userDnPattern is CONSUMED — the
// direct-bind DN template of auth-integration.md section 1.1 #4 drives the
// section 1.5 rule-5 ladder:
//
//	direct arm  — {0} → RFC 4514-escaped username, bind that DN, no search
//	search arm  — manager (or anonymous) bind + filter search, bind the DN
//	fallback    — invalid-credentials on the direct arm falls through to the
//	              search arm ("两者同时配置时仍可认证")
//
// The provider-level table pins the ladder sequence (the mock records every
// attempted bind DN); the ConfigManager leg proves the section PUT arms the
// pattern (the T-305 gap was exactly this projection being dropped).

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
)

// t346Provider builds one LDAPProvider over the mock directory.
func t346Provider(t *testing.T, mock *mockLDAPConn, cfg *auth.LDAPConfig) *auth.LDAPProvider {
	t.Helper()
	prov, err := auth.NewLDAPProvider(cfg, nil, mockDialer(mock))
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	t.Cleanup(prov.Close)
	return prov
}

// TestT346UserDNPatternDirectBind: the pattern set, the user at the pattern
// DN — ONE bind total (the substituted DN), no manager/anonymous bind, no
// user search (only the post-bind extractUserID read).
func TestT346UserDNPatternDirectBind(t *testing.T) {
	ctx := context.Background()
	mock := newMockLDAPConn()
	mock.addUser("uid=alice,ou=people,dc=example,dc=com", "alice-pass", map[string][]string{
		"uid": {"alice"}, "objectClass": {"posixAccount"},
	})
	// The search arm's fixtures — deliberately present and NOT touched.
	mock.addUser("cn=manager,dc=example,dc=com", "manager-pass", map[string][]string{
		"cn": {"manager"}, "objectClass": {"posixAccount"},
	})

	prov := t346Provider(t, mock, &auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://dir.example.com:389",
		BaseDN:        "dc=example,dc=com",
		UserDNPattern: "uid={0},ou=people,dc=example,dc=com",
		BindDN:        "cn=manager,dc=example,dc=com",
		BindPassword:  "manager-pass",
		UserFilter:    "(&(objectClass=posixAccount)(uid=%s))",
		UserIDAttr:    "uid",
	})

	claims, err := prov.Bind(ctx, "alice", "alice-pass")
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if claims.Name != "alice" || claims.ProviderID != "uid=alice,ou=people,dc=example,dc=com" {
		t.Fatalf("claims = %+v, want name alice / the pattern DN as ProviderID", claims)
	}
	if got := mock.binds; !reflect.DeepEqual(got, []string{"uid=alice,ou=people,dc=example,dc=com"}) {
		t.Fatalf("bind ladder = %v, want exactly the substituted pattern DN (no manager, no search-then-bind)", got)
	}
	if mock.searches != 1 {
		t.Fatalf("searches = %d, want 1 (the extractUserID read only — the user search never ran)", mock.searches)
	}
}

// TestT346UserDNPatternEscaping: a username carrying DN metacharacters is
// escaped into the pattern (RFC 4514) — the ",dc=" splice cannot re-target
// the subtree, so the direct arm fails closed and the ladder falls through
// to the search arm's filter-escaped lookup.
func TestT346UserDNPatternEscaping(t *testing.T) {
	ctx := context.Background()
	mock := newMockLDAPConn()
	mock.addUser("uid=alice,dc=example,dc=com", "alice-pass", map[string][]string{
		"uid": {"alice"}, "objectClass": {"posixAccount"},
	})

	prov := t346Provider(t, mock, &auth.LDAPConfig{
		Enabled: true,
		URL:     "ldap://dir.example.com:389",
		BaseDN:  "dc=example,dc=com",
		// A malicious username must land INSIDE the uid value, not splice
		// a second RDN: "alice,dc=evil" → uid=alice\,dc=evil,…
		UserDNPattern: "uid={0},dc=example,dc=com",
		UserFilter:    "(&(objectClass=posixAccount)(uid=%s))",
		UserIDAttr:    "uid",
	})

	_, err := prov.Bind(ctx, "alice,dc=evil", "whatever")
	if err == nil {
		t.Fatal("Bind with a DN-splicing username succeeded, want failure")
	}
	want := "uid=alice\\,dc=evil,dc=example,dc=com"
	if len(mock.binds) == 0 || mock.binds[0] != want {
		t.Fatalf("first bind DN = %v, want the escaped %q", mock.binds, want)
	}
	if _, _, ok := auth.FailureClass(err); !ok {
		t.Fatalf("error %v is not a classified failure", err)
	}
}

// TestT346UserDNPatternFallbackToSearch: the pattern set but the user lives
// elsewhere — the direct arm's invalid-credentials failure falls through to
// the search arm, which still authenticates (spec rule 5's "两者同时配置时
// 仍可认证" — both arms configured, authentication works).
func TestT346UserDNPatternFallbackToSearch(t *testing.T) {
	ctx := context.Background()
	mock := newMockLDAPConn()
	mock.addUser("cn=manager,dc=example,dc=com", "manager-pass", map[string][]string{
		"cn": {"manager"}, "objectClass": {"posixAccount"},
	})
	mock.addUser("uid=bob,ou=contractors,dc=example,dc=com", "bob-pass", map[string][]string{
		"uid": {"bob"}, "objectClass": {"posixAccount"},
	})

	prov := t346Provider(t, mock, &auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://dir.example.com:389",
		BaseDN:        "dc=example,dc=com",
		UserDNPattern: "uid={0},ou=people,dc=example,dc=com",
		BindDN:        "cn=manager,dc=example,dc=com",
		BindPassword:  "manager-pass",
		UserFilter:    "(&(objectClass=posixAccount)(uid=%s))",
		UserIDAttr:    "uid",
	})

	claims, err := prov.Bind(ctx, "bob", "bob-pass")
	if err != nil {
		t.Fatalf("Bind: %v (the search-arm fallback must authenticate)", err)
	}
	if claims.ProviderID != "uid=bob,ou=contractors,dc=example,dc=com" {
		t.Fatalf("ProviderID = %q, want the SEARCHED DN", claims.ProviderID)
	}
	want := []string{
		"uid=bob,ou=people,dc=example,dc=com",      // direct arm (fails)
		"cn=manager,dc=example,dc=com",             // search-arm service bind
		"uid=bob,ou=contractors,dc=example,dc=com", // the found DN
	}
	if !reflect.DeepEqual(mock.binds, want) {
		t.Fatalf("bind ladder = %v, want %v", mock.binds, want)
	}
}

// TestT346UserDNPatternBothArmsFail: pattern set, user nowhere — the ladder
// exhausts and classifies (user_not_found when the search answered empty;
// bad_credentials when the found DN rejects the password).
func TestT346UserDNPatternBothArmsFail(t *testing.T) {
	ctx := context.Background()
	mock := newMockLDAPConn()
	mock.addUser("cn=manager,dc=example,dc=com", "manager-pass", map[string][]string{
		"cn": {"manager"}, "objectClass": {"posixAccount"},
	})
	prov := t346Provider(t, mock, &auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://dir.example.com:389",
		BaseDN:        "dc=example,dc=com",
		UserDNPattern: "uid={0},ou=people,dc=example,dc=com",
		BindDN:        "cn=manager,dc=example,dc=com",
		BindPassword:  "manager-pass",
		UserFilter:    "(&(objectClass=posixAccount)(uid=%s))",
		UserIDAttr:    "uid",
	})

	_, err := prov.Bind(ctx, "nobody", "pass")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want the ErrInvalidCredentials chain", err)
	}
	if _, reason, ok := auth.FailureClass(err); !ok || reason != auth.ReasonUserNotFound {
		t.Fatalf("classification = %q (%v), want user_not_found", reason, err)
	}

	// The bad-credentials arm: the search FINDS the user but the password
	// fails both the pattern DN and the searched DN.
	mock2 := newMockLDAPConn()
	mock2.addUser("cn=manager,dc=example,dc=com", "manager-pass", map[string][]string{
		"cn": {"manager"}, "objectClass": {"posixAccount"},
	})
	mock2.addUser("uid=carol,ou=contractors,dc=example,dc=com", "carol-pass", map[string][]string{
		"uid": {"carol"}, "objectClass": {"posixAccount"},
	})
	prov2 := t346Provider(t, mock2, &auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://dir.example.com:389",
		BaseDN:        "dc=example,dc=com",
		UserDNPattern: "uid={0},ou=people,dc=example,dc=com",
		BindDN:        "cn=manager,dc=example,dc=com",
		BindPassword:  "manager-pass",
		UserFilter:    "(&(objectClass=posixAccount)(uid=%s))",
		UserIDAttr:    "uid",
	})
	_, err = prov2.Bind(ctx, "carol", "wrong")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want the ErrInvalidCredentials chain", err)
	}
	if _, reason, ok := auth.FailureClass(err); !ok || reason != auth.ReasonBadCredentials {
		t.Fatalf("classification = %q (%v), want bad_credentials", reason, err)
	}
}

// TestT346UserDNPatternSectionConsumption: the ConfigManager leg — a section
// PUT carrying userDnPattern arms the direct mode on the very next
// authentication (the T-305 gap: the field validated and echoed but was
// dropped at the provider projection).
func TestT346UserDNPatternSectionConsumption(t *testing.T) {
	ctx := context.Background()
	st := openConfigStore(t)

	dir := newMockLDAPConn()
	dir.addUser("uid=alice,dc=example,dc=com", "alice-pass", map[string][]string{
		"uid": {"alice"}, "objectClass": {"posixAccount"},
	})
	url := "ldap://dir.example.com:389"
	m := newTestManager(t, st, func(o *auth.ConfigOptions) {
		o.LDAPDialer = routeLDAPDialer(map[string]*mockLDAPConn{url: dir})
		o.LDAPResolver = auth.NewLDAPResolver(st.Users())
	})
	if err := m.Load(ctx, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
	svc := hotLDAPService(t, m, st)

	// The pattern points at the directory's flat DN layout; the search arm
	// stays configured (both modes — rule 5).
	body := []byte(fmt.Sprintf(`{
		"enabled": true,
		"ldapUrl": %q,
		"userDnPattern": "uid={0},dc=example,dc=com",
		"search": {"searchFilter": "(&(objectClass=posixAccount)(uid={0}))"}
	}`, url+"/dc=example,dc=com"))
	if _, _, err := m.PutAuthSection(ctx, auth.SectionLDAP, body, "admin"); err != nil {
		t.Fatalf("PutAuthSection: %v", err)
	}

	// The provider row keyed by the pattern DN (the direct arm's bind DN is
	// the Claims ProviderID) — seeded like the hot-switch suite so the leg
	// does not depend on the auto-create wiring.
	makeUser(ctx, t, st.Users(), "alice", "", false, "ldap", "uid=alice,dc=example,dc=com")
	if p, err := svc.AuthenticateCredentials(ctx, "alice", "alice-pass"); err != nil || p == nil {
		t.Fatalf("auth via userDnPattern = %v, want success", err)
	}
	if _, err := svc.AuthenticateCredentials(ctx, "alice", "wrong"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("auth with wrong password = %v, want ErrInvalidCredentials", err)
	}
}
