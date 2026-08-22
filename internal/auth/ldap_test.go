package auth_test

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	ldap "github.com/go-ldap/ldap/v3"

	"github.com/lzwzzy/binflow/internal/auth"
)

// mockLDAPConn implements auth's internal ldapConn interface for testing the
// LDAPProvider bind-search flow without a real LDAP server. It stores a set
// of user and group entries and simulates Bind and Search operations.
type mockLDAPConn struct {
	// bound tracks whether the connection is currently bound, and as whom.
	bound   bool
	boundDN string

	// users maps DN -> password for bind verification.
	users map[string]mockLDAPUser

	// groups maps group DN -> group info.
	groups map[string]mockLDAPGroup

	// startTLSCalls counts StartTLS invocations seen by this connection and
	// startTLSConfig captures the tls.Config of the last one (T-186 wiring
	// assertions); startTLSErr, when set, makes the upgrade fail.
	startTLSCalls  int
	startTLSConfig *tls.Config
	startTLSErr    error

	// closed tracks whether the connection has been closed.
	closed bool
}

type mockLDAPUser struct {
	password   string
	attributes map[string][]string
}

type mockLDAPGroup struct {
	attributes map[string][]string
}

func newMockLDAPConn() *mockLDAPConn {
	return &mockLDAPConn{
		users:  make(map[string]mockLDAPUser),
		groups: make(map[string]mockLDAPGroup),
	}
}

func (m *mockLDAPConn) addUser(dn, password string, attrs map[string][]string) {
	m.users[dn] = mockLDAPUser{password: password, attributes: attrs}
}

func (m *mockLDAPConn) addGroup(dn string, attrs map[string][]string) {
	m.groups[dn] = mockLDAPGroup{attributes: attrs}
}

func (m *mockLDAPConn) Bind(username, password string) error {
	if m.closed {
		return errors.New("connection closed")
	}
	user, ok := m.users[username]
	if !ok {
		return ldap.NewError(ldap.LDAPResultInvalidCredentials,
			fmt.Errorf("invalid credentials"))
	}
	if user.password != password {
		return ldap.NewError(ldap.LDAPResultInvalidCredentials,
			fmt.Errorf("invalid credentials"))
	}
	m.bound = true
	m.boundDN = username
	return nil
}

func (m *mockLDAPConn) Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error) {
	if m.closed {
		return nil, errors.New("connection closed")
	}

	// Parse the filter to determine what we're searching for.
	// We support simple filter patterns used by the LDAPProvider:
	//   (uid=xxx) or (&(objectClass=posixAccount)(uid=xxx))
	filter := searchRequest.Filter
	username := extractFilterValue(filter, "uid")
	if username == "" {
		username = extractFilterValue(filter, "cn")
	}

	var entries []*ldap.Entry

	// Search users.
	for dn, user := range m.users {
		// Check if this entry matches the filter.
		if !matchesFilter(dn, user.attributes, filter) {
			continue
		}
		// Check if the DN is under the BaseDN.
		if !strings.HasSuffix(dn, searchRequest.BaseDN) {
			continue
		}
		attrs := filterAttributes(user.attributes, searchRequest.Attributes)
		// Always include DN.
		attrs["dn"] = []string{dn}
		entries = append(entries, ldap.NewEntry(dn, attrs))
	}

	// Search groups.
	for dn, group := range m.groups {
		if !matchesFilter(dn, group.attributes, filter) {
			continue
		}
		if !strings.HasSuffix(dn, searchRequest.BaseDN) {
			continue
		}
		attrs := filterAttributes(group.attributes, searchRequest.Attributes)
		attrs["dn"] = []string{dn}
		entries = append(entries, ldap.NewEntry(dn, attrs))
	}

	// Enforce size limit.
	if searchRequest.SizeLimit > 0 && len(entries) > searchRequest.SizeLimit {
		entries = entries[:searchRequest.SizeLimit]
	}

	return &ldap.SearchResult{Entries: entries}, nil
}

func (m *mockLDAPConn) StartTLS(config *tls.Config) error {
	m.startTLSCalls++
	m.startTLSConfig = config
	if m.startTLSErr != nil {
		return m.startTLSErr
	}
	return nil
}

func (m *mockLDAPConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockLDAPConn) SetTimeout(timeout time.Duration) {}

// extractFilterValue extracts the value of a named attribute from a simple
// LDAP filter string. Supports patterns like (attr=value) and
// (&(attr1=value1)(attr2=value2)).
func extractFilterValue(filter, attr string) string {
	// Look for (attr=value) pattern.
	prefix := "(" + attr + "="
	idx := strings.Index(filter, prefix)
	if idx < 0 {
		return ""
	}
	start := idx + len(prefix)
	end := strings.IndexByte(filter[start:], ')')
	if end < 0 {
		return ""
	}
	return filter[start : start+end]
}

// matchesFilter checks whether a DN and attributes match a simple LDAP filter.
// Supports: (attr=value), (&(a1=v1)(a2=v2)), (objectClass=*).
func matchesFilter(dn string, attrs map[string][]string, filter string) bool {
	filter = strings.TrimSpace(filter)

	// (&(a1=v1)(a2=v2)) — all must match.
	if strings.HasPrefix(filter, "(&") {
		inner := filter[2 : len(filter)-1] // strip (& and trailing )
		parts := splitFilterParts(inner)
		for _, part := range parts {
			if !matchesFilter(dn, attrs, part) {
				return false
			}
		}
		return true
	}

	// (attr=value)
	if strings.HasPrefix(filter, "(") && strings.HasSuffix(filter, ")") {
		inner := filter[1 : len(filter)-1]
		eqIdx := strings.IndexByte(inner, '=')
		if eqIdx < 0 {
			return false
		}
		attrName := inner[:eqIdx]
		attrValue := inner[eqIdx+1:]

		// (objectClass=*) matches everything.
		if attrName == "objectClass" && attrValue == "*" {
			return true
		}

		// (dn=value) matches the DN.
		if attrName == "dn" {
			return dn == attrValue
		}

		// Check attribute values.
		vals, ok := attrs[attrName]
		if !ok {
			return false
		}
		for _, v := range vals {
			if v == attrValue {
				return true
			}
		}
		return false
	}

	return false
}

// splitFilterParts splits a concatenated filter string like "(a=1)(b=2)" into
// individual filter parts like ["(a=1)", "(b=2)"].
func splitFilterParts(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, c := range s {
		if c == '(' {
			if depth == 0 {
				start = i
			}
			depth++
		} else if c == ')' {
			depth--
			if depth == 0 {
				parts = append(parts, s[start:i+1])
			}
		}
	}
	return parts
}

// filterAttributes returns only the requested attributes from the entry.
// If the request asks for ["*"], all attributes are returned.
func filterAttributes(attrs map[string][]string, requested []string) map[string][]string {
	if len(requested) == 0 {
		// Return all attributes.
		out := make(map[string][]string, len(attrs))
		for k, v := range attrs {
			out[k] = v
		}
		return out
	}
	out := make(map[string][]string)
	for _, req := range requested {
		if req == "*" {
			for k, v := range attrs {
				out[k] = v
			}
			return out
		}
		if vals, ok := attrs[req]; ok {
			out[req] = vals
		}
	}
	return out
}

// mockLDAPDialer is a helper that creates a dialer function returning a
// pre-configured mockLDAPConn. This is injected into NewLDAPProvider during
// tests.
type mockLDAPDialer struct {
	conn *mockLDAPConn
}

func (d *mockLDAPDialer) dial(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (auth.LDAPConn, error) {
	// Reset the mock connection state for each dial.
	d.conn.bound = false
	d.conn.boundDN = ""
	d.conn.closed = false
	return d.conn, nil
}

// TestLDAPProviderBind tests the primary Bind flow of LDAPProvider.
func TestLDAPProviderBind(t *testing.T) {
	mock := newMockLDAPConn()

	// Add a service account for searching.
	mock.addUser("cn=admin,dc=example,dc=com", "adminpass", map[string][]string{
		"uid":         {"admin"},
		"objectClass": {"posixAccount"},
	})

	// Add a regular user.
	mock.addUser("uid=alice,dc=example,dc=com", "alicepass", map[string][]string{
		"uid":         {"alice"},
		"cn":          {"Alice Smith"},
		"objectClass": {"posixAccount"},
	})

	// Add a group.
	mock.addGroup("cn=developers,dc=example,dc=com", map[string][]string{
		"cn":          {"developers"},
		"objectClass": {"posixGroup"},
		"memberUid":   {"alice"},
	})

	// Add an admin group.
	mock.addGroup("cn=binflow-admin,dc=example,dc=com", map[string][]string{
		"cn":          {"binflow-admin"},
		"objectClass": {"posixGroup"},
		"memberUid":   {"alice"},
	})

	// Create a dialer that returns our mock.
	dialCount := 0
	dialer := func(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (auth.LDAPConn, error) {
		dialCount++
		mock.bound = false
		mock.boundDN = ""
		mock.closed = false
		return mock, nil
	}

	cfg := &auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://ldap.example.com:389",
		BaseDN:        "dc=example,dc=com",
		BindDN:        "cn=admin,dc=example,dc=com",
		BindPassword:  "adminpass",
		UserFilter:    "(&(objectClass=posixAccount)(uid=%s))",
		UserIDAttr:    "uid",
		GroupFilter:   "(&(objectClass=posixGroup)(memberUid=%s))",
		GroupNameAttr: "cn",
		AdminGroup:    "cn=binflow-admin,dc=example,dc=com",
		PoolSize:      2,
	}

	prov, err := auth.NewLDAPProvider(cfg, nil, dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	defer prov.Close()

	claims, err := prov.Bind(context.Background(), "alice", "alicepass")
	if err != nil {
		t.Fatalf("Bind(alice, alicepass): %v", err)
	}

	if claims.Name != "alice" {
		t.Fatalf("Claims.Name = %q, want %q", claims.Name, "alice")
	}
	if claims.ProviderID != "uid=alice,dc=example,dc=com" {
		t.Fatalf("Claims.ProviderID = %q, want %q", claims.ProviderID, "uid=alice,dc=example,dc=com")
	}
	if !claims.Admin {
		t.Fatal("Claims.Admin should be true (member of admin group)")
	}

	foundDev := false
	for _, g := range claims.Groups {
		if g == "developers" {
			foundDev = true
			break
		}
	}
	if !foundDev {
		t.Fatalf("Claims.Groups = %v, want to include 'developers'", claims.Groups)
	}

	t.Logf("Bind OK: dials=%d claims=%+v", dialCount, claims)
}

// TestLDAPProviderBindWrongPassword tests that Bind returns ErrInvalidCredentials
// when the password is incorrect.
func TestLDAPProviderBindWrongPassword(t *testing.T) {
	mock := newMockLDAPConn()

	mock.addUser("cn=admin,dc=example,dc=com", "adminpass", map[string][]string{
		"uid": {"admin"},
	})
	mock.addUser("uid=alice,dc=example,dc=com", "alicepass", map[string][]string{
		"uid": {"alice"},
	})

	dialer := func(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (auth.LDAPConn, error) {
		mock.bound = false
		mock.boundDN = ""
		mock.closed = false
		return mock, nil
	}

	cfg := &auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://ldap.example.com:389",
		BaseDN:        "dc=example,dc=com",
		BindDN:        "cn=admin,dc=example,dc=com",
		BindPassword:  "adminpass",
		UserFilter:    "(&(objectClass=posixAccount)(uid=%s))",
		UserIDAttr:    "uid",
		GroupFilter:   "",
		GroupNameAttr: "cn",
		PoolSize:      1,
	}

	prov, err := auth.NewLDAPProvider(cfg, nil, dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	defer prov.Close()

	_, err = prov.Bind(context.Background(), "alice", "wrongpassword")
	if err == nil {
		t.Fatal("Bind with wrong password should fail")
	}
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Bind(wrong password) err = %v, want ErrInvalidCredentials", err)
	}
	t.Logf("Bind wrong password OK: %v", err)
}

// TestLDAPProviderBindUnknownUser tests that Bind returns ErrInvalidCredentials
// when the user does not exist in the directory.
func TestLDAPProviderBindUnknownUser(t *testing.T) {
	mock := newMockLDAPConn()

	mock.addUser("cn=admin,dc=example,dc=com", "adminpass", map[string][]string{
		"uid": {"admin"},
	})

	dialer := func(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (auth.LDAPConn, error) {
		mock.bound = false
		mock.boundDN = ""
		mock.closed = false
		return mock, nil
	}

	cfg := &auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://ldap.example.com:389",
		BaseDN:        "dc=example,dc=com",
		BindDN:        "cn=admin,dc=example,dc=com",
		BindPassword:  "adminpass",
		UserFilter:    "(&(objectClass=posixAccount)(uid=%s))",
		UserIDAttr:    "uid",
		GroupFilter:   "",
		GroupNameAttr: "cn",
		PoolSize:      1,
	}

	prov, err := auth.NewLDAPProvider(cfg, nil, dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	defer prov.Close()

	_, err = prov.Bind(context.Background(), "nonexistent", "anypass")
	if err == nil {
		t.Fatal("Bind with unknown user should fail")
	}
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Bind(unknown user) err = %v, want ErrInvalidCredentials", err)
	}
	t.Logf("Bind unknown user OK: %v", err)
}

// TestLDAPProviderBindEmptyCredentials tests that Bind returns
// ErrInvalidCredentials when username or password is empty.
func TestLDAPProviderBindEmptyCredentials(t *testing.T) {
	mock := newMockLDAPConn()

	dialer := func(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (auth.LDAPConn, error) {
		mock.bound = false
		mock.boundDN = ""
		mock.closed = false
		return mock, nil
	}

	cfg := &auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://ldap.example.com:389",
		BaseDN:        "dc=example,dc=com",
		UserFilter:    "(uid=%s)",
		UserIDAttr:    "uid",
		GroupFilter:   "",
		GroupNameAttr: "cn",
		PoolSize:      1,
	}

	prov, err := auth.NewLDAPProvider(cfg, nil, dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	defer prov.Close()

	tests := []struct {
		name     string
		username string
		password string
	}{
		{"empty username", "", "pass"},
		{"empty password", "user", ""},
		{"both empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := prov.Bind(context.Background(), tt.username, tt.password)
			if err == nil {
				t.Fatal("Bind with empty credentials should fail")
			}
			if !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Fatalf("Bind(empty) err = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

// TestLDAPProviderBindWithoutBindDN tests direct bind mode (no service account
// search). In this mode, the user DN is constructed from the username and
// BaseDN.
func TestLDAPProviderBindWithoutBindDN(t *testing.T) {
	mock := newMockLDAPConn()

	// Add user: the provider will search for (uid=alice) and find this entry.
	mock.addUser("uid=alice,dc=example,dc=com", "alicepass", map[string][]string{
		"uid":         {"alice"},
		"objectClass": {"posixAccount"},
	})

	dialer := func(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (auth.LDAPConn, error) {
		mock.bound = false
		mock.boundDN = ""
		mock.closed = false
		return mock, nil
	}

	cfg := &auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://ldap.example.com:389",
		BaseDN:        "dc=example,dc=com",
		BindDN:        "", // No service account — direct bind mode.
		BindPassword:  "",
		UserFilter:    "(uid=%s)",
		UserIDAttr:    "uid",
		GroupFilter:   "",
		GroupNameAttr: "cn",
		PoolSize:      1,
	}

	prov, err := auth.NewLDAPProvider(cfg, nil, dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	defer prov.Close()

	claims, err := prov.Bind(context.Background(), "alice", "alicepass")
	if err != nil {
		t.Fatalf("Bind(alice, alicepass) without BindDN: %v", err)
	}

	if claims.Name != "alice" {
		t.Fatalf("Claims.Name = %q, want %q", claims.Name, "alice")
	}
	if claims.ProviderID != "uid=alice,dc=example,dc=com" {
		t.Fatalf("Claims.ProviderID = %q, want %q", claims.ProviderID, "uid=alice,dc=example,dc=com")
	}
	t.Logf("Bind without BindDN OK: %+v", claims)
}

// TestLDAPProviderBindWithGroups tests that group memberships are correctly
// extracted from the directory.
func TestLDAPProviderBindWithGroups(t *testing.T) {
	mock := newMockLDAPConn()

	mock.addUser("cn=admin,dc=example,dc=com", "adminpass", map[string][]string{
		"uid": {"admin"},
	})
	mock.addUser("uid=bob,dc=example,dc=com", "bobpass", map[string][]string{
		"uid":         {"bob"},
		"objectClass": {"posixAccount"},
	})

	// Bob belongs to three groups.
	mock.addGroup("cn=dev,dc=example,dc=com", map[string][]string{
		"cn":          {"dev"},
		"objectClass": {"posixGroup"},
		"memberUid":   {"bob"},
	})
	mock.addGroup("cn=ops,dc=example,dc=com", map[string][]string{
		"cn":          {"ops"},
		"objectClass": {"posixGroup"},
		"memberUid":   {"bob"},
	})
	mock.addGroup("cn=qa,dc=example,dc=com", map[string][]string{
		"cn":          {"qa"},
		"objectClass": {"posixGroup"},
		"memberUid":   {"bob"},
	})

	dialer := func(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (auth.LDAPConn, error) {
		mock.bound = false
		mock.boundDN = ""
		mock.closed = false
		return mock, nil
	}

	cfg := &auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://ldap.example.com:389",
		BaseDN:        "dc=example,dc=com",
		BindDN:        "cn=admin,dc=example,dc=com",
		BindPassword:  "adminpass",
		UserFilter:    "(&(objectClass=posixAccount)(uid=%s))",
		UserIDAttr:    "uid",
		GroupFilter:   "(&(objectClass=posixGroup)(memberUid=%s))",
		GroupNameAttr: "cn",
		PoolSize:      1,
	}

	prov, err := auth.NewLDAPProvider(cfg, nil, dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	defer prov.Close()

	claims, err := prov.Bind(context.Background(), "bob", "bobpass")
	if err != nil {
		t.Fatalf("Bind(bob, bobpass): %v", err)
	}

	if len(claims.Groups) != 3 {
		t.Fatalf("len(Claims.Groups) = %d, want 3, groups=%v", len(claims.Groups), claims.Groups)
	}

	groupSet := make(map[string]bool)
	for _, g := range claims.Groups {
		groupSet[g] = true
	}
	for _, want := range []string{"dev", "ops", "qa"} {
		if !groupSet[want] {
			t.Fatalf("Claims.Groups missing %q, groups=%v", want, claims.Groups)
		}
	}

	t.Logf("Bind with groups OK: %+v", claims)
}

// TestLDAPProviderProviderName tests that ProviderName returns ProviderLDAP.
func TestLDAPProviderProviderName(t *testing.T) {
	mock := newMockLDAPConn()
	dialer := func(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (auth.LDAPConn, error) {
		mock.bound = false
		mock.boundDN = ""
		mock.closed = false
		return mock, nil
	}

	cfg := &auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://ldap.example.com:389",
		BaseDN:        "dc=example,dc=com",
		UserFilter:    "(uid=%s)",
		UserIDAttr:    "uid",
		GroupFilter:   "",
		GroupNameAttr: "cn",
		PoolSize:      1,
	}

	prov, err := auth.NewLDAPProvider(cfg, nil, dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	defer prov.Close()

	if prov.ProviderName() != auth.ProviderLDAP {
		t.Fatalf("ProviderName() = %q, want %q", prov.ProviderName(), auth.ProviderLDAP)
	}
}

// TestLDAPProviderAuthenticateAlwaysFails tests that Authenticate returns
// ErrInvalidCredentials (LDAP does not support Bearer tokens).
func TestLDAPProviderAuthenticateAlwaysFails(t *testing.T) {
	mock := newMockLDAPConn()
	dialer := func(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (auth.LDAPConn, error) {
		mock.bound = false
		mock.boundDN = ""
		mock.closed = false
		return mock, nil
	}

	cfg := &auth.LDAPConfig{
		Enabled:    true,
		URL:        "ldap://ldap.example.com:389",
		BaseDN:     "dc=example,dc=com",
		UserFilter: "(uid=%s)",
		UserIDAttr: "uid",
		PoolSize:   1,
	}

	prov, err := auth.NewLDAPProvider(cfg, nil, dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	defer prov.Close()

	_, err = prov.Authenticate(context.Background(), "some-token")
	if err == nil {
		t.Fatal("Authenticate should fail for LDAP")
	}
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Authenticate err = %v, want ErrInvalidCredentials", err)
	}
}

// TestLDAPProviderResolveImplementsIdentityProvider tests that Resolve and
// ProviderName make LDAPProvider a valid IdentityProvider.
func TestLDAPProviderResolveImplementsIdentityProvider(t *testing.T) {
	mock := newMockLDAPConn()
	dialer := func(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (auth.LDAPConn, error) {
		return mock, nil
	}

	cfg := &auth.LDAPConfig{
		Enabled:    true,
		URL:        "ldap://ldap.example.com:389",
		BaseDN:     "dc=example,dc=com",
		UserFilter: "(uid=%s)",
		UserIDAttr: "uid",
		PoolSize:   1,
	}

	prov, err := auth.NewLDAPProvider(cfg, nil, dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	defer prov.Close()

	// Verify LDAPProvider satisfies IdentityProvider interface.
	var _ auth.IdentityProvider = prov

	// Resolve without a resolver should return ErrProviderUserNotFound.
	_, err = prov.Resolve(context.Background(), auth.ProviderLDAP, "uid=alice,dc=example,dc=com")
	if err == nil {
		t.Fatal("Resolve without resolver should fail")
	}
	if !errors.Is(err, auth.ErrProviderUserNotFound) {
		t.Fatalf("Resolve err = %v, want ErrProviderUserNotFound", err)
	}
}

// TestLDAPProviderDisabled tests that NewLDAPProvider returns (nil, nil) when
// Enabled is false.
func TestLDAPProviderDisabled(t *testing.T) {
	cfg := &auth.LDAPConfig{
		Enabled: false,
	}
	prov, err := auth.NewLDAPProvider(cfg, nil, nil)
	if err != nil {
		t.Fatalf("NewLDAPProvider(disabled): %v", err)
	}
	if prov != nil {
		t.Fatal("NewLDAPProvider(disabled) should return nil provider")
	}
}

// TestLDAPProviderMissingURL tests that NewLDAPProvider returns an error when
// URL is missing.
func TestLDAPProviderMissingURL(t *testing.T) {
	cfg := &auth.LDAPConfig{
		Enabled: true,
		BaseDN:  "dc=example,dc=com",
	}
	_, err := auth.NewLDAPProvider(cfg, nil, nil)
	if err == nil {
		t.Fatal("NewLDAPProvider without URL should fail")
	}
}

// TestLDAPProviderMissingBaseDN tests that NewLDAPProvider returns an error
// when BaseDN is missing.
func TestLDAPProviderMissingBaseDN(t *testing.T) {
	cfg := &auth.LDAPConfig{
		Enabled: true,
		URL:     "ldap://ldap.example.com:389",
	}
	_, err := auth.NewLDAPProvider(cfg, nil, nil)
	if err == nil {
		t.Fatal("NewLDAPProvider without BaseDN should fail")
	}
}

// TestLDAPProviderConfigDefaults tests that zero-valued config fields are
// populated with sensible defaults.
func TestLDAPProviderConfigDefaults(t *testing.T) {
	mock := newMockLDAPConn()
	dialer := func(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (auth.LDAPConn, error) {
		mock.bound = false
		mock.boundDN = ""
		mock.closed = false
		return mock, nil
	}

	cfg := &auth.LDAPConfig{
		Enabled: true,
		URL:     "ldap://ldap.example.com:389",
		BaseDN:  "dc=example,dc=com",
		// All defaults: UserFilter, UserIDAttr, GroupNameAttr, PoolSize,
		// ConnectTimeout, RequestTimeout are zero-valued.
	}

	prov, err := auth.NewLDAPProvider(cfg, nil, dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider with defaults: %v", err)
	}
	defer prov.Close()

	// Verify that ProviderName works — the provider was constructed
	// successfully with defaults.
	if prov.ProviderName() != auth.ProviderLDAP {
		t.Fatalf("ProviderName() = %q, want %q", prov.ProviderName(), auth.ProviderLDAP)
	}
}

// TestLDAPProviderGetByProviderError tests Resolve when the resolver returns a
// non-ErrProviderUserNotFound error.
func TestLDAPProviderGetByProviderError(t *testing.T) {
	mock := newMockLDAPConn()
	dialer := func(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (auth.LDAPConn, error) {
		return mock, nil
	}

	cfg := &auth.LDAPConfig{
		Enabled:    true,
		URL:        "ldap://ldap.example.com:389",
		BaseDN:     "dc=example,dc=com",
		UserFilter: "(uid=%s)",
		UserIDAttr: "uid",
		PoolSize:   1,
	}

	// Create a resolver that returns a generic error.
	failingResolver := &failingLDAPResolver{err: errors.New("db connection lost")}
	prov, err := auth.NewLDAPProvider(cfg, failingResolver, dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	defer prov.Close()

	_, err = prov.Resolve(context.Background(), auth.ProviderLDAP, "uid=alice,dc=example,dc=com")
	if err == nil {
		t.Fatal("Resolve with failing resolver should fail")
	}
	if errors.Is(err, auth.ErrProviderUserNotFound) {
		t.Fatal("Resolve error should not be ErrProviderUserNotFound when resolver fails")
	}
}

type failingLDAPResolver struct {
	err error
}

func (r *failingLDAPResolver) GetByProvider(ctx context.Context, provider auth.Provider, providerID string) (*auth.ProviderUser, error) {
	return nil, r.err
}

// TestLDAPProviderBindServiceAccountFailure tests that Bind fails when the
// service account (BindDN) credentials are invalid.
func TestLDAPProviderBindServiceAccountFailure(t *testing.T) {
	mock := newMockLDAPConn()

	// Service account has different password than what we'll configure.
	mock.addUser("cn=admin,dc=example,dc=com", "correctpass", map[string][]string{
		"uid": {"admin"},
	})

	dialer := func(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (auth.LDAPConn, error) {
		mock.bound = false
		mock.boundDN = ""
		mock.closed = false
		return mock, nil
	}

	cfg := &auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://ldap.example.com:389",
		BaseDN:        "dc=example,dc=com",
		BindDN:        "cn=admin,dc=example,dc=com",
		BindPassword:  "wrongpass", // Wrong password for the service account.
		UserFilter:    "(uid=%s)",
		UserIDAttr:    "uid",
		GroupFilter:   "",
		GroupNameAttr: "cn",
		PoolSize:      1,
	}

	prov, err := auth.NewLDAPProvider(cfg, nil, dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	defer prov.Close()

	_, err = prov.Bind(context.Background(), "alice", "alicepass")
	if err == nil {
		t.Fatal("Bind with wrong service account password should fail")
	}
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Bind(service account wrong pass) err = %v, want ErrInvalidCredentials", err)
	}
}

// TestLDAPProviderNoAdminGroup tests that admin is false when AdminGroup is
// not configured.
func TestLDAPProviderNoAdminGroup(t *testing.T) {
	mock := newMockLDAPConn()

	mock.addUser("cn=admin,dc=example,dc=com", "adminpass", map[string][]string{
		"uid": {"admin"},
	})
	mock.addUser("uid=alice,dc=example,dc=com", "alicepass", map[string][]string{
		"uid":         {"alice"},
		"objectClass": {"posixAccount"},
	})

	dialer := func(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (auth.LDAPConn, error) {
		mock.bound = false
		mock.boundDN = ""
		mock.closed = false
		return mock, nil
	}

	cfg := &auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://ldap.example.com:389",
		BaseDN:        "dc=example,dc=com",
		BindDN:        "cn=admin,dc=example,dc=com",
		BindPassword:  "adminpass",
		UserFilter:    "(&(objectClass=posixAccount)(uid=%s))",
		UserIDAttr:    "uid",
		GroupFilter:   "",
		GroupNameAttr: "cn",
		AdminGroup:    "", // No admin group configured.
		PoolSize:      1,
	}

	prov, err := auth.NewLDAPProvider(cfg, nil, dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	defer prov.Close()

	claims, err := prov.Bind(context.Background(), "alice", "alicepass")
	if err != nil {
		t.Fatalf("Bind(alice, alicepass): %v", err)
	}
	if claims.Admin {
		t.Fatal("Claims.Admin should be false when AdminGroup is not configured")
	}
}
