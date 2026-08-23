// T-187 acceptance surface (T-174 D7/D8 + O-2), auth side: the login
// failure classification. Every rejected password login still satisfies
// errors.Is(err, ErrInvalidCredentials) — the uniform 401 is untouched —
// but FailureClass now names the arm (local/oidc/ldap) and the minimal
// reason, with the two O-2 classes (provider_error / tls_handshake)
// distinguishable from bad_credentials.

package auth_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// seedUser creates one user row with full control over the provider columns
// and the enabled flag (makeUser in ldap_login_test.go always enables).
type seedSpec struct {
	name       string
	password   string // plaintext; "" seeds an empty hash (federated row)
	provider   string
	providerID string
	enabled    bool
}

func seedRows(t *testing.T, st metadata.Store, rows []seedSpec) {
	t.Helper()
	ctx := context.Background()
	for _, r := range rows {
		hash := ""
		if r.password != "" {
			var err error
			hash, err = auth.HashPassword(r.password)
			if err != nil {
				t.Fatalf("HashPassword(%q): %v", r.name, err)
			}
		}
		now := metadata.Now()
		if err := st.Users().Create(ctx, &metadata.User{
			Username: r.name, PasswordHash: hash, IsAdmin: false, Enabled: r.enabled,
			CreatedAt: now, UpdatedAt: now, Provider: r.provider, ProviderID: r.providerID,
		}); err != nil {
			t.Fatalf("seed user %q: %v", r.name, err)
		}
	}
}

// dirUser is one mock directory entry (uid attr derived from the DN's first
// component).
type dirUser struct{ uid, password string }

func dn(uid string) string { return "uid=" + uid + ",ou=people,dc=example,dc=com" }

// TestAuthenticateCredentialsFailureClass is the T-187 classification
// matrix: which arm a failed password login is attributed to, and why.
func TestAuthenticateCredentialsFailureClass(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		// rows seeded into the metadata store.
		rows []seedSpec
		// dir seeds the mock directory (only meaningful with wireLDAP).
		dir []dirUser
		// wiring of the LDAP fallback arm.
		wireLDAP    bool
		startTLS    bool
		dialErr     error // the dialer fails before any LDAP traffic
		startTLSErr error // the StartTLS upgrade fails (O-2)

		username, password     string
		wantMethod, wantReason string
		wantInfra              bool
	}{
		{
			name:     "local user wrong password",
			rows:     []seedSpec{{name: "alice", password: "alice-pw", provider: "local", enabled: true}},
			dir:      []dirUser{{uid: "alice", password: "dir-pw"}},
			wireLDAP: true,
			username: "alice", password: "wrong",
			wantMethod: "local", wantReason: "bad_credentials",
		},
		{
			name:     "unknown user without an ldap arm",
			rows:     []seedSpec{{name: "alice", password: "alice-pw", provider: "local", enabled: true}},
			username: "ghost", password: "whatever",
			wantMethod: "local", wantReason: "user_not_found",
		},
		{
			name:     "unknown user everywhere with ldap wired",
			rows:     []seedSpec{{name: "alice", password: "alice-pw", provider: "local", enabled: true}},
			dir:      []dirUser{{uid: "alice", password: "dir-pw"}},
			wireLDAP: true,
			username: "ghost", password: "whatever",
			wantMethod: "ldap", wantReason: "user_not_found",
		},
		{
			name:     "disabled local row",
			rows:     []seedSpec{{name: "off", password: "off-pw", provider: "local", enabled: false}},
			username: "off", password: "off-pw",
			wantMethod: "local", wantReason: "user_disabled",
		},
		{
			name:     "ldap row wrong directory password",
			rows:     []seedSpec{{name: "jdoe", provider: "ldap", providerID: dn("jdoe"), enabled: true}},
			dir:      []dirUser{{uid: "jdoe", password: "dir-pw"}},
			wireLDAP: true,
			username: "jdoe", password: "wrong",
			wantMethod: "ldap", wantReason: "bad_credentials",
		},
		{
			name:     "directory user absent, row absent, bind never reached",
			dir:      []dirUser{},
			wireLDAP: true,
			username: "nobody", password: "whatever",
			wantMethod: "ldap", wantReason: "user_not_found",
		},
		{
			name:     "oidc-owned row cannot password-login",
			rows:     []seedSpec{{name: "sso", provider: "oidc", providerID: "sub-1", enabled: true}},
			username: "sso", password: "any",
			wantMethod: "oidc", wantReason: "bad_credentials",
		},
		{
			name:     "directory unreachable is provider_error not bad_credentials",
			rows:     []seedSpec{{name: "alice", password: "alice-pw", provider: "local", enabled: true}},
			wireLDAP: true,
			dialErr:  errors.New("dial tcp 10.0.0.1:389: connect: connection refused"),
			username: "alice", password: "wrong",
			wantMethod: "ldap", wantReason: "provider_error", wantInfra: true,
		},
		{
			name:     "failed starttls upgrade is tls_handshake not bad_credentials",
			wireLDAP: true, startTLS: true,
			startTLSErr: errors.New("ldap: starttls refused"),
			username:    "jdoe", password: "dir-pw",
			wantMethod: "ldap", wantReason: "tls_handshake", wantInfra: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, err := metadata.Open(ctx, metadata.Options{
				Driver: "sqlite",
				Path:   filepath.Join(t.TempDir(), "binflow.db"),
			})
			if err != nil {
				t.Fatalf("metadata.Open: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			seedRows(t, st, tc.rows)

			svc := auth.NewFromStore(st, false)
			if tc.wireLDAP {
				mock := newMockLDAPConn()
				// The service account the search-bind step uses (its absence
				// is itself a provider_error classification, not this table's
				// subject).
				mock.addUser("cn=admin,dc=example,dc=com", "adminpass", map[string][]string{
					"cn": {"admin"}, "objectClass": {"posixAccount"},
				})
				for _, u := range tc.dir {
					mock.addUser(dn(u.uid), u.password, map[string][]string{
						"uid": {u.uid}, "objectClass": {"posixAccount"},
					})
				}
				dialer := mockDialer(mock, mockDialErr(tc.dialErr))
				prov, perr := auth.NewLDAPProvider(&auth.LDAPConfig{
					Enabled: true, URL: "ldap://dir.example.com:389", BaseDN: "dc=example,dc=com",
					BindDN: "cn=admin,dc=example,dc=com", BindPassword: "adminpass",
					UserFilter: "(uid=%s)", UserIDAttr: "uid", GroupFilter: "",
					PoolSize: 1, StartTLS: tc.startTLS,
				}, auth.NewLDAPResolver(st.Users()), dialer)
				if perr != nil {
					t.Fatalf("NewLDAPProvider: %v", perr)
				}
				t.Cleanup(prov.Close)
				if tc.startTLSErr != nil {
					mock.startTLSErr = tc.startTLSErr
				}
				svc = svc.WithLDAP(prov)
			}

			p, err := svc.AuthenticateCredentials(ctx, tc.username, tc.password)
			if err == nil {
				t.Fatalf("AuthenticateCredentials(%q, ***) = %+v, want failure", tc.username, p)
			}
			// The uniform 401 contract survives every classification.
			if !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Fatalf("error %v does not satisfy ErrInvalidCredentials", err)
			}
			method, reason, ok := auth.FailureClass(err)
			if !ok {
				t.Fatalf("error %v carries no failure classification", err)
			}
			if method != tc.wantMethod || reason != tc.wantReason {
				t.Errorf("classification = %s/%s, want %s/%s (err: %v)",
					method, reason, tc.wantMethod, tc.wantReason, err)
			}
			if got := auth.InfraFailure(err); got != tc.wantInfra {
				t.Errorf("InfraFailure = %v, want %v", got, tc.wantInfra)
			}
			// The error text never embeds the presented password (NFR-S3).
			if strings.Contains(err.Error(), tc.password) {
				t.Errorf("error text leaks the password: %v", err)
			}
		})
	}
}

// TestFailureClassUnclassified: a plain rejection (no Failure in the chain)
// reports ok=false and no infra signal — the HTTP plane's defensive default
// applies then.
func TestFailureClassUnclassified(t *testing.T) {
	err := errors.New("auth: basic credential rejected: auth: invalid credentials")
	if _, _, ok := auth.FailureClass(err); ok {
		t.Error("FailureClass reported a classification for an unclassified error")
	}
	if auth.InfraFailure(err) {
		t.Error("InfraFailure reported infra for an unclassified error")
	}
}

// TestLoginStoreFailureStays500: a metadata-store failure on the lookup
// path is a server error, not a login rejection — it must NOT be folded
// into ErrInvalidCredentials (which would turn the 500 into a 401).
func TestLoginStoreFailureStays500(t *testing.T) {
	ctx := context.Background()
	st, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "binflow.db"),
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	seedRows(t, st, []seedSpec{{name: "alice", password: "alice-pw", provider: "local", enabled: true}})
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	_, err = auth.NewFromStore(st, false).AuthenticateCredentials(ctx, "alice", "alice-pw")
	if err == nil {
		t.Fatal("AuthenticateCredentials succeeded on a closed store")
	}
	if errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("store failure was classified as a rejection (would 401): %v", err)
	}
}

// TestProviderFailureReason: transport errors classify as tls_handshake
// when the chain carries a TLS failure, provider_error otherwise.
func TestProviderFailureReason(t *testing.T) {
	tlsErr := fmt.Errorf("oauth2: cannot fetch token: %w",
		&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}) //nolint:govet // zero Err is fine for errors.As
	if got, want := auth.ProviderFailureReason(tlsErr), auth.ReasonTLSHandshake; got != want {
		t.Errorf("ProviderFailureReason(tls) = %q, want %q", got, want)
	}
	dialErr := fmt.Errorf("Post \"https://idp.example.com/token\": %w",
		&net.OpError{Op: "dial", Err: errors.New("connection refused")})
	if got, want := auth.ProviderFailureReason(dialErr), auth.ReasonProviderError; got != want {
		t.Errorf("ProviderFailureReason(dial) = %q, want %q", got, want)
	}
	if got, want := auth.ProviderFailureReason(nil), auth.ReasonProviderError; got != want {
		t.Errorf("ProviderFailureReason(nil) = %q, want %q", got, want)
	}
}
