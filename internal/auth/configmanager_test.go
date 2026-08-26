package auth_test

// T-305 acceptance suite (ADR-0035 / FR-92): the ConfigManager's three
// elements — immutable-snapshot reads (the hot A→B directory switch with no
// restart, AC3), verify-then-replace writes (refusals leave the live
// configuration in force), single-row-per-section persistence replayed at
// boot — plus the dual-source rules (K31: DB row wins + WARN, missing row +
// configured file section seeds), the write-only secret semantics (sentinel
// keep / clear / replace, sealed enc:v1, no-master-key refusals), the
// auto-create gates and the test-connection probes.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	ldap "github.com/go-ldap/ldap/v3"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
)

// cipherFor builds the REAL enc:v1 chain over a fixed test master key (the
// production machine, not a fake — the sealed-doc assertions speak the
// actual wire format).
func cipherFor(t *testing.T) *remote.Cipher {
	t.Helper()
	c, err := remote.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("remote.NewCipher: %v", err)
	}
	return c
}

// openConfigStore opens a throwaway sqlite metadata store.
func openConfigStore(t *testing.T) metadata.Store {
	t.Helper()
	ctx := context.Background()
	st, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "binflow.db"),
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// newTestManager builds a ConfigManager over the store with immediate LDAP
// pool drain (deterministic swap tests).
func newTestManager(t *testing.T, st metadata.Store, opts func(*auth.ConfigOptions)) *auth.ConfigManager {
	t.Helper()
	o := auth.ConfigOptions{
		Store:     auth.NewConfigStoreAdapter(st.AuthConfigs()),
		Cipher:    cipherFor(t),
		PoolGrace: -1,
	}
	if opts != nil {
		opts(&o)
	}
	m, err := auth.NewAuthConfigManager(o)
	if err != nil {
		t.Fatalf("NewAuthConfigManager: %v", err)
	}
	return m
}

// seedLDAPSectionBody is a minimal enabled LDAP section document (wire
// spellings, §1.1/§1.2).
func seedLDAPSectionBody(url, bindDN, bindPassword string) []byte {
	return []byte(fmt.Sprintf(`{
		"enabled": true,
		"ldapUrl": %q,
		"search": {
			"searchFilter": "(&(objectClass=posixAccount)(uid={0}))",
			"searchSubTree": true,
			"managerDn": %q,
			"managerPassword": %q
		}
	}`, url, bindDN, bindPassword))
}

// ---------------------------------------------------------------------------
// Section round-trips, strict decode, masking (AC2)
// ---------------------------------------------------------------------------

func TestAuthConfigSectionRoundTripAndMasking(t *testing.T) {
	st := openConfigStore(t)
	m := newTestManager(t, st, nil)
	ctx := context.Background()

	tests := []struct {
		name    string
		section string
		body    string
		secret  string // the plaintext secret the body carries
		echo    string // the masked echo's secret field
	}{
		{
			name:    "ldap",
			section: auth.SectionLDAP,
			body:    `{"enabled":true,"ldapUrl":"ldap://dir.example.com:389/dc=example,dc=com","search":{"searchFilter":"(uid={0})","managerDn":"cn=admin,dc=example,dc=com","managerPassword":"dir-secret"}}`,
			secret:  "dir-secret",
		},
		{
			name:    "oidc",
			section: auth.SectionOIDC,
			body:    `{"enabled":false,"issuer_url":"https://idp.example.com","client_id":"cid","client_secret":"oidc-secret","redirect_url":"https://binflow.example.com/cb"}`,
			secret:  "oidc-secret",
		},
		{
			name:    "saml",
			section: auth.SectionSAML,
			body:    `{"enableIntegration":false,"loginUrl":"https://idp.example.com/saml","serviceProviderName":"binflow"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			echo, _, err := m.PutAuthSection(ctx, tt.section, []byte(tt.body), "admin")
			if err != nil {
				t.Fatalf("PutAuthSection: %v", err)
			}
			echoStr := string(echo)
			if tt.secret != "" {
				if strings.Contains(echoStr, tt.secret) {
					t.Fatalf("echo leaks the plaintext secret: %s", echoStr)
				}
				if !strings.Contains(echoStr, auth.MaskedSecretEcho) {
					t.Fatalf("echo carries no sentinel: %s", echoStr)
				}
			}
			// The STORED doc is sealed, never plaintext (AC2: 明文 grep 零命中).
			rec, err := st.AuthConfigs().GetAuthConfig(ctx, tt.section)
			if err != nil || rec == nil {
				t.Fatalf("GetAuthConfig: %v %v", rec, err)
			}
			if tt.secret != "" {
				if strings.Contains(rec.Doc, tt.secret) {
					t.Fatalf("stored doc leaks the plaintext secret: %s", rec.Doc)
				}
				if !strings.Contains(rec.Doc, "enc:v1:") {
					t.Fatalf("stored doc secret is not enc:v1-sealed: %s", rec.Doc)
				}
				if rec.UpdatedBy != "admin" {
					t.Errorf("UpdatedBy = %q, want admin", rec.UpdatedBy)
				}
			}

			// GET renders the same masked echo.
			got, err := m.GetAuthSection(ctx, tt.section)
			if err != nil {
				t.Fatalf("GetAuthSection: %v", err)
			}
			if string(got) != echoStr {
				t.Fatalf("GET echo drifts from the PUT echo:\n%s\n%s", echoStr, got)
			}

			// The sentinel write-back KEEPS the stored secret (write-only
			// mode, ADR-0035 decision 5).
			if tt.secret != "" {
				sentinelBody := strings.Replace(tt.body, tt.secret, auth.MaskedSecretEcho, 1)
				echo2, _, err := m.PutAuthSection(ctx, tt.section, []byte(sentinelBody), "admin")
				if err != nil {
					t.Fatalf("PutAuthSection(sentinel): %v", err)
				}
				if !strings.Contains(string(echo2), auth.MaskedSecretEcho) {
					t.Fatalf("sentinel write-back lost the secret (echo carries no sentinel): %s", echo2)
				}
				rec2, _ := st.AuthConfigs().GetAuthConfig(ctx, tt.section)
				if rec2 == nil || !strings.Contains(rec2.Doc, "enc:v1:") {
					t.Fatalf("sentinel write-back dropped the secret: %v", rec2)
				}

				// An explicit empty string CLEARS the secret.
				clearBody := strings.Replace(tt.body, fmt.Sprintf("%q", tt.secret), `""`, 1)
				if _, _, err := m.PutAuthSection(ctx, tt.section, []byte(clearBody), "admin"); err != nil {
					t.Fatalf("PutAuthSection(clear): %v", err)
				}
				rec3, _ := st.AuthConfigs().GetAuthConfig(ctx, tt.section)
				if rec3 == nil || strings.Contains(rec3.Doc, "enc:v1:") {
					t.Fatalf("clear left a sealed secret behind: %v", rec3)
				}
			}
		})
	}
}

func TestAuthConfigUnknownKeyRejected(t *testing.T) {
	st := openConfigStore(t)
	m := newTestManager(t, st, nil)
	ctx := context.Background()

	tests := []struct {
		name    string
		section string
		body    string
	}{
		{"ldap top level", auth.SectionLDAP, `{"enabled":false,"ldapUrlX":"ldap://h/dc=x"}`},
		{"ldap search sub-object", auth.SectionLDAP, `{"enabled":false,"search":{"searchFilterX":"(uid={0})"}}`},
		{"oidc", auth.SectionOIDC, `{"enabled":false,"issuer":"https://x"}`},
		{"saml", auth.SectionSAML, `{"enableIntegration":false,"loginURL":"https://x"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := m.PutAuthSection(ctx, tt.section, []byte(tt.body), "admin")
			if !errors.Is(err, auth.ErrUnknownAuthConfigField) {
				t.Fatalf("err = %v, want ErrUnknownAuthConfigField", err)
			}
		})
	}
}

func TestAuthConfigValidation(t *testing.T) {
	st := openConfigStore(t)
	m := newTestManager(t, st, nil)
	ctx := context.Background()

	tests := []struct {
		name    string
		section string
		body    string
		wantErr string
	}{
		{"ldap missing url", auth.SectionLDAP, `{"enabled":true}`, "ldapUrl is required"},
		{"ldap bad scheme", auth.SectionLDAP, `{"enabled":true,"ldapUrl":"http://h/dc=x"}`, "scheme must be ldap or ldaps"},
		{"ldap no base dn", auth.SectionLDAP, `{"enabled":true,"ldapUrl":"ldap://h:389"}`, "base DN"},
		{"ldap bad key", auth.SectionLDAP, `{"key":"other","enabled":false}`, `key must be "ldap"`},
		{"ldap filter without placeholder", auth.SectionLDAP, `{"enabled":true,"ldapUrl":"ldap://h/dc=x","search":{"searchFilter":"(uid=fix)"}}`, "{0}"},
		{"ldap disabled tolerates empty", auth.SectionLDAP, `{"enabled":false}`, ""},
		{"oidc missing issuer", auth.SectionOIDC, `{"enabled":true,"client_id":"c","redirect_url":"https://x/cb"}`, "issuer_url is required"},
		{"oidc missing client", auth.SectionOIDC, `{"enabled":true,"issuer_url":"https://x","redirect_url":"https://x/cb"}`, "client_id is required"},
		{"oidc disabled tolerates empty", auth.SectionOIDC, `{"enabled":false}`, ""},
		{"saml missing login url", auth.SectionSAML, `{"enableIntegration":true,"serviceProviderName":"sp","certificate":"x"}`, "loginUrl is required"},
		{"saml missing cert", auth.SectionSAML, `{"enableIntegration":true,"loginUrl":"https://idp/sso","serviceProviderName":"sp"}`, "certificate is required"},
		{"saml bad cert", auth.SectionSAML, `{"enableIntegration":true,"loginUrl":"https://idp/sso","serviceProviderName":"sp","certificate":"not-a-pem"}`, "certificate"},
		{"saml disabled tolerates empty", auth.SectionSAML, `{"enableIntegration":false}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := m.PutAuthSection(ctx, tt.section, []byte(tt.body), "admin")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestAuthConfigDefaultsDoc(t *testing.T) {
	st := openConfigStore(t)
	m := newTestManager(t, st, nil)
	ctx := context.Background()

	// An unset section: GET answers nil, the plane serves the default doc.
	doc, err := m.GetAuthSection(ctx, auth.SectionSAML)
	if err != nil || doc != nil {
		t.Fatalf("GetAuthSection(unset saml) = %s, %v; want nil, nil", doc, err)
	}
	if got := string(auth.DefaultAuthSectionDoc(auth.SectionSAML)); got != "{}" {
		t.Fatalf("saml default doc = %s, want {} (the anchored §3.2 empty state)", got)
	}
	ldapDoc := string(auth.DefaultAuthSectionDoc(auth.SectionLDAP))
	for _, want := range []string{`"enabled":true`, `"autoCreateUser":true`, `"emailAttribute":"mail"`, `"searchSubTree":true`} {
		if !strings.Contains(ldapDoc, want) {
			t.Fatalf("ldap default doc misses %s: %s", want, ldapDoc)
		}
	}
}

// ---------------------------------------------------------------------------
// Dual-source rules (K31) and startup replay
// ---------------------------------------------------------------------------

func TestAuthConfigDualSourcePriority(t *testing.T) {
	ctx := context.Background()

	t.Run("db row wins over the file section", func(t *testing.T) {
		st := openConfigStore(t)
		m := newTestManager(t, st, nil)
		// A stored row (written through the plane)...
		if _, _, err := m.PutAuthSection(ctx, auth.SectionOIDC,
			[]byte(`{"enabled":false,"issuer_url":"https://db-issuer","client_id":"db","redirect_url":"https://x/cb"}`), "admin"); err != nil {
			t.Fatalf("PutAuthSection: %v", err)
		}
		// ...and a DIFFERENT file-section seed still present at boot.
		seeds := map[string][]byte{
			auth.SectionOIDC: []byte(`{"enabled":true,"issuer_url":"https://file-issuer","client_id":"file","redirect_url":"https://x/cb","auto_create_users":true}`),
		}
		if err := m.Load(ctx, seeds); err != nil {
			t.Fatalf("Load with an overridden file section = %v, want the WARN posture (no error)", err)
		}
		doc, err := m.GetAuthSection(ctx, auth.SectionOIDC)
		if err != nil {
			t.Fatalf("GetAuthSection: %v", err)
		}
		if strings.Contains(string(doc), "file-issuer") {
			t.Fatalf("the file seed overrode the DB row: %s", doc)
		}
		if !strings.Contains(string(doc), "db-issuer") {
			t.Fatalf("the DB row is not in force: %s", doc)
		}
	})

	t.Run("missing row plus configured file section seeds once", func(t *testing.T) {
		st := openConfigStore(t)
		m := newTestManager(t, st, nil)
		seeds := map[string][]byte{
			auth.SectionOIDC: []byte(`{"enabled":false,"issuer_url":"https://seed-issuer","client_id":"seed","client_secret":"seed-secret","redirect_url":"https://x/cb","auto_create_users":true}`),
		}
		if err := m.Load(ctx, seeds); err != nil {
			t.Fatalf("Load(seed) = %v, want nil", err)
		}
		rec, err := st.AuthConfigs().GetAuthConfig(ctx, auth.SectionOIDC)
		if err != nil || rec == nil {
			t.Fatalf("the seed did not land: %v %v", rec, err)
		}
		if rec.UpdatedBy != "system-seed" {
			t.Errorf("seed UpdatedBy = %q, want system-seed", rec.UpdatedBy)
		}
		if strings.Contains(rec.Doc, "seed-secret") || !strings.Contains(rec.Doc, "enc:v1:") {
			t.Fatalf("the seeded secret is not sealed: %s", rec.Doc)
		}
		// The snapshot replayed the row (an enabled section would build a
		// provider; the disabled one still shows as stored).
		doc, _ := m.GetAuthSection(ctx, auth.SectionOIDC)
		if !strings.Contains(string(doc), "seed-issuer") {
			t.Fatalf("the snapshot did not replay the seeded row: %s", doc)
		}
	})

	t.Run("an all-default file section never seeds", func(t *testing.T) {
		st := openConfigStore(t)
		m := newTestManager(t, st, nil)
		seeds := map[string][]byte{
			auth.SectionOIDC: auth.DefaultAuthSectionDoc(auth.SectionOIDC),
			auth.SectionLDAP: auth.DefaultAuthSectionDoc(auth.SectionLDAP),
		}
		if err := m.Load(ctx, seeds); err != nil {
			t.Fatalf("Load(defaults) = %v, want nil", err)
		}
		rows, err := st.AuthConfigs().ListAuthConfigs(ctx)
		if err != nil {
			t.Fatalf("ListAuthConfigs: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("a defaults-only boot seeded %d rows, want 0", len(rows))
		}
	})
}

func TestAuthConfigStartupReplay(t *testing.T) {
	ctx := context.Background()
	st := openConfigStore(t)

	// First life: write a section and close.
	m1 := newTestManager(t, st, nil)
	if _, _, err := m1.PutAuthSection(ctx, auth.SectionLDAP,
		seedLDAPSectionBody("ldap://dir.example.com:389/dc=example,dc=com", "cn=admin,dc=example,dc=com", "dir-pass"), "admin"); err != nil {
		t.Fatalf("PutAuthSection: %v", err)
	}
	m1.Close()

	// Second life: a fresh manager replays the row into a live provider
	// (the boot path — providers rebuilt, secrets unsealed).
	m2 := newTestManager(t, st, nil)
	if err := m2.Load(ctx, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m2.CurrentLDAP() == nil {
		t.Fatal("startup replay left the LDAP arm inert, want the live provider")
	}
}

func TestAuthConfigNoMasterKeyPostures(t *testing.T) {
	ctx := context.Background()

	t.Run("write with a new secret refuses", func(t *testing.T) {
		st := openConfigStore(t)
		m := newTestManager(t, st, func(o *auth.ConfigOptions) { o.Cipher = nil })
		_, _, err := m.PutAuthSection(ctx, auth.SectionOIDC,
			[]byte(`{"enabled":false,"issuer_url":"https://x","client_id":"c","client_secret":"s","redirect_url":"https://x/cb"}`), "admin")
		if !errors.Is(err, auth.ErrNoMasterKey) {
			t.Fatalf("err = %v, want ErrNoMasterKey", err)
		}
	})

	t.Run("stored secret without a key fails the boot", func(t *testing.T) {
		st := openConfigStore(t)
		m := newTestManager(t, st, nil)
		if _, _, err := m.PutAuthSection(ctx, auth.SectionOIDC,
			[]byte(`{"enabled":false,"issuer_url":"https://x","client_id":"c","client_secret":"s","redirect_url":"https://x/cb"}`), "admin"); err != nil {
			t.Fatalf("PutAuthSection: %v", err)
		}
		m2 := newTestManager(t, st, func(o *auth.ConfigOptions) { o.Cipher = nil })
		if err := m2.Load(ctx, nil); !errors.Is(err, auth.ErrSecretUnreadable) {
			t.Fatalf("Load = %v, want ErrSecretUnreadable (boot fail-fast)", err)
		}
	})
}

// ---------------------------------------------------------------------------
// The hot arms (AC3: change-effective-immediately, no restart)
// ---------------------------------------------------------------------------

// routeLDAPDialer routes each dial by the provider URL — the hot A→B
// directory switch's stand-in for two directories. Every dial returns a
// FRESH conn copied from the template (concurrent-authentication safety:
// no shared mutable mock state under -race).
func routeLDAPDialer(templates map[string]*mockLDAPConn) auth.LDAPDialer {
	return func(_ context.Context, urlStr string, _ ...ldap.DialOpt) (auth.LDAPConn, error) {
		tmpl, ok := templates[urlStr]
		if !ok {
			return nil, fmt.Errorf("no mock directory for %q", urlStr)
		}
		c := newMockLDAPConn()
		for dn, u := range tmpl.users {
			c.addUser(dn, u.password, u.attributes)
		}
		return c, nil
	}
}

func hotLDAPService(t *testing.T, m *auth.ConfigManager, st metadata.Store) *auth.Service {
	t.Helper()
	return auth.NewFromStore(st, false).WithAuthConfig(m)
}

// TestAuthConfigHotLDAPDirectorySwitch (AC3, the mock A→B leg): a PUT that
// retargets the LDAP section takes effect on the NEXT authentication — the
// old directory's credentials stop working, the new one's start, with no
// restart anywhere.
func TestAuthConfigHotLDAPDirectorySwitch(t *testing.T) {
	ctx := context.Background()
	st := openConfigStore(t)

	// Two directories with different credentials.
	dirA := newMockLDAPConn()
	dirA.addUser("uid=alice,dc=example,dc=com", "alice-A", map[string][]string{
		"uid": {"alice"}, "objectClass": {"posixAccount"},
	})
	dirB := newMockLDAPConn()
	dirB.addUser("uid=alice,dc=example,dc=com", "alice-B", map[string][]string{
		"uid": {"alice"}, "objectClass": {"posixAccount"},
	})
	urlA := "ldap://dir-a.example.com:389"
	urlB := "ldap://dir-b.example.com:389"

	m := newTestManager(t, st, func(o *auth.ConfigOptions) {
		o.LDAPDialer = routeLDAPDialer(map[string]*mockLDAPConn{urlA: dirA, urlB: dirB})
		o.LDAPResolver = auth.NewLDAPResolver(st.Users())
	})
	if err := m.Load(ctx, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
	svc := hotLDAPService(t, m, st)

	// Install directory A.
	if _, _, err := m.PutAuthSection(ctx, auth.SectionLDAP,
		seedLDAPSectionBody(urlA+"/dc=example,dc=com", "", ""), "admin"); err != nil {
		t.Fatalf("PutAuthSection(A): %v", err)
	}
	makeUser(ctx, t, st.Users(), "alice", "", false, "ldap", "uid=alice,dc=example,dc=com")

	// Against A: the A password works, the B password does not.
	if p, err := svc.AuthenticateCredentials(ctx, "alice", "alice-A"); err != nil || p == nil {
		t.Fatalf("auth(A creds on A) = %v, want success", err)
	}
	if _, err := svc.AuthenticateCredentials(ctx, "alice", "alice-B"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("auth(B creds on A) = %v, want ErrInvalidCredentials", err)
	}

	// THE SWITCH: retarget to directory B — no restart, no rebuild of the
	// service, just the section PUT.
	start := time.Now()
	if _, _, err := m.PutAuthSection(ctx, auth.SectionLDAP,
		seedLDAPSectionBody(urlB+"/dc=example,dc=com", "", ""), "admin"); err != nil {
		t.Fatalf("PutAuthSection(B): %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("the PUT itself took %s — the ≤1s bar is structural", elapsed)
	}

	// The very next authentication walks B (AC3's double assertion).
	if _, err := svc.AuthenticateCredentials(ctx, "alice", "alice-A"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("auth(A creds on B) = %v, want ErrInvalidCredentials (old config stopped working)", err)
	}
	if p, err := svc.AuthenticateCredentials(ctx, "alice", "alice-B"); err != nil || p == nil {
		t.Fatalf("auth(B creds on B) = %v, want success (new config in force)", err)
	}
}

// TestAuthConfigHotOIDCArmActivation: flipping the OIDC section's enabled
// flag arms and disarms the Bearer arm on the next request (the facet is
// the arm-dispatch's own question, OIDCWired).
func TestAuthConfigHotOIDCArmActivation(t *testing.T) {
	ctx := context.Background()
	st := openConfigStore(t)
	m := newTestManager(t, st, func(o *auth.ConfigOptions) {
		o.OIDCResolver = auth.NewOIDCResolver(st.Users())
	})
	if err := m.Load(ctx, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
	svc := auth.NewFromStore(st, false).WithAuthConfig(m)

	if svc.OIDCWired() {
		t.Fatal("OIDC arm wired on an empty config, want inert")
	}
	// Enable against a dead issuer: the PUT REFUSES (discovery failure =
	// write failure) and the arm stays inert — D7, the current
	// configuration survives every refusal.
	if _, _, err := m.PutAuthSection(ctx, auth.SectionOIDC,
		[]byte(`{"enabled":true,"issuer_url":"http://127.0.0.1:1","client_id":"c","redirect_url":"https://x/cb"}`), "admin"); err == nil {
		t.Fatal("PUT against a dead issuer succeeded, want the discovery refusal")
	}
	if svc.OIDCWired() {
		t.Fatal("a refused PUT armed the OIDC arm, want the previous state in force")
	}
	// Disable-side: an enabled section that later goes disabled deactivates
	// the arm on the next request.
	if _, _, err := m.PutAuthSection(ctx, auth.SectionOIDC,
		[]byte(`{"enabled":false,"issuer_url":"http://127.0.0.1:1","client_id":"c","redirect_url":"https://x/cb"}`), "admin"); err != nil {
		t.Fatalf("PutAuthSection(disabled): %v", err)
	}
	if svc.OIDCWired() {
		t.Fatal("the disabled section left the OIDC arm wired")
	}
}

// TestAuthConfigConcurrentPutAndAuthenticate: PUTs retargeting the section
// between two directories while logins run — no tearing (every login either
// succeeds or answers a clean ErrInvalidCredentials), run under -race in CI.
func TestAuthConfigConcurrentPutAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	st := openConfigStore(t)

	dirA := newMockLDAPConn()
	dirA.addUser("uid=alice,dc=example,dc=com", "alice-A", map[string][]string{
		"uid": {"alice"}, "objectClass": {"posixAccount"},
	})
	dirB := newMockLDAPConn()
	dirB.addUser("uid=alice,dc=example,dc=com", "alice-B", map[string][]string{
		"uid": {"alice"}, "objectClass": {"posixAccount"},
	})
	urlA := "ldap://dir-a.example.com:389"
	urlB := "ldap://dir-b.example.com:389"

	m := newTestManager(t, st, func(o *auth.ConfigOptions) {
		o.LDAPDialer = routeLDAPDialer(map[string]*mockLDAPConn{urlA: dirA, urlB: dirB})
		o.LDAPResolver = auth.NewLDAPResolver(st.Users())
	})
	if err := m.Load(ctx, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, _, err := m.PutAuthSection(ctx, auth.SectionLDAP,
		seedLDAPSectionBody(urlA+"/dc=example,dc=com", "", ""), "admin"); err != nil {
		t.Fatalf("PutAuthSection(A): %v", err)
	}
	makeUser(ctx, t, st.Users(), "alice", "", false, "ldap", "uid=alice,dc=example,dc=com")
	svc := hotLDAPService(t, m, st)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writers: toggle A <-> B.
	wg.Add(1)
	go func() {
		defer wg.Done()
		targets := [][]byte{
			seedLDAPSectionBody(urlA+"/dc=example,dc=com", "", ""),
			seedLDAPSectionBody(urlB+"/dc=example,dc=com", "", ""),
		}
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, _, err := m.PutAuthSection(ctx, auth.SectionLDAP, targets[i%2], "admin"); err != nil {
				t.Errorf("concurrent PutAuthSection: %v", err)
				return
			}
			i++
		}
	}()

	// Readers: authenticate with either credential set.
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				p, err := svc.AuthenticateCredentials(ctx, "alice", "alice-A")
				if err == nil {
					if p == nil || p.Name != "alice" {
						t.Errorf("torn principal: %+v", p)
						return
					}
					continue
				}
				if !errors.Is(err, auth.ErrInvalidCredentials) {
					t.Errorf("non-credential failure under a config swap: %v", err)
					return
				}
				p, err = svc.AuthenticateCredentials(ctx, "alice", "alice-B")
				if err == nil {
					if p == nil || p.Name != "alice" {
						t.Errorf("torn principal: %+v", p)
						return
					}
					continue
				}
				if !errors.Is(err, auth.ErrInvalidCredentials) {
					t.Errorf("both credential sets failed non-credentially: %v", err)
					return
				}
			}
		}()
	}

	// Let the readers drain, then stop the writer.
	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestAuthConfigAutoCreateGates: the section flags gate first-login
// auto-create live (default true = the M6 posture; false = user_not_found).
func TestAuthConfigAutoCreateGates(t *testing.T) {
	ctx := context.Background()
	st := openConfigStore(t)
	dir := newMockLDAPConn()
	dir.addUser("uid=zed,dc=example,dc=com", "zed-pass", map[string][]string{
		"uid": {"zed"}, "objectClass": {"posixAccount"},
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

	// Default (autoCreateUser=true, the §1.1 default): the first login
	// creates the row.
	if _, _, err := m.PutAuthSection(ctx, auth.SectionLDAP,
		seedLDAPSectionBody(url+"/dc=example,dc=com", "", ""), "admin"); err != nil {
		t.Fatalf("PutAuthSection: %v", err)
	}
	if p, err := svc.AuthenticateCredentials(ctx, "zed", "zed-pass"); err != nil || p == nil {
		t.Fatalf("auth(auto-create on) = %v, want the first login to create the row", err)
	}
	if _, err := st.Users().Get(ctx, "zed"); err != nil {
		t.Fatalf("the auto-created row is missing: %v", err)
	}

	// autoCreateUser=false: the next NEW user is transient-refused.
	if _, _, err := m.PutAuthSection(ctx, auth.SectionLDAP,
		[]byte(`{"enabled":true,"ldapUrl":"ldap://dir.example.com:389/dc=example,dc=com","autoCreateUser":false,"search":{"searchFilter":"(&(objectClass=posixAccount)(uid={0}))"}}`), "admin"); err != nil {
		t.Fatalf("PutAuthSection(autoCreateUser=false): %v", err)
	}
	dir.addUser("uid=yao,dc=example,dc=com", "yao-pass", map[string][]string{
		"uid": {"yao"}, "objectClass": {"posixAccount"},
	})
	if _, err := svc.AuthenticateCredentials(ctx, "yao", "yao-pass"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("auth(auto-create off) = %v, want the transient refusal", err)
	}
	if _, err := st.Users().Get(ctx, "yao"); err == nil {
		t.Fatal("auto-create-off still created the row")
	}
}

// ---------------------------------------------------------------------------
// Test connection (AC4)
// ---------------------------------------------------------------------------

// probeConn adapts a mock directory onto the probe's connection seam (the
// provider-level mock already speaks Bind/Search/StartTLS/Close; only the
// probe's timeout knob is missing).
type probeConn struct{ m *mockLDAPConn }

func (p probeConn) Bind(u, pw string) error { return p.m.Bind(u, pw) }
func (p probeConn) Search(r *ldap.SearchRequest) (*ldap.SearchResult, error) {
	return p.m.Search(r)
}
func (p probeConn) StartTLS(c *tls.Config) error { return p.m.StartTLS(c) }
func (p probeConn) Close() error                 { return p.m.Close() }
func (p probeConn) SetTimeout(time.Duration)     {}

// probeDialer builds an LDAPProbeDialer over one mock directory.
func probeDialer(conn *mockLDAPConn, dialErr error) auth.LDAPProbeDialer {
	return func(_ context.Context, _ string, _ *tls.Config, _ bool) (auth.LDAPProbeConn, error) {
		if dialErr != nil {
			return nil, dialErr
		}
		return probeConn{m: conn}, nil
	}
}

func TestAuthConfigTestLDAP(t *testing.T) {
	ctx := context.Background()

	// freshProbeDir builds a directory with the manager and alice entries
	// (each subtest gets its OWN mock — the probe's Close is per-connection
	// on the real wire, and a shared mock would carry the closed flag over).
	freshProbeDir := func() *mockLDAPConn {
		dir := newMockLDAPConn()
		dir.addUser("cn=admin,dc=example,dc=com", "admin-pass", map[string][]string{
			"uid": {"admin"}, "objectClass": {"posixAccount"},
		})
		dir.addUser("uid=alice,dc=example,dc=com", "alice-pass", map[string][]string{
			"uid": {"alice"}, "objectClass": {"posixAccount"},
		})
		return dir
	}

	body := func(extra string) []byte {
		return []byte(`{"enabled":true,"ldapUrl":"ldap://dir.example.com:389/dc=example,dc=com",` +
			`"search":{"searchFilter":"(&(objectClass=posixAccount)(uid={0}))","managerDn":"cn=admin,dc=example,dc=com","managerPassword":"admin-pass"}` +
			extra + `}`)
	}

	t.Run("manager bind and trial search", func(t *testing.T) {
		st := openConfigStore(t)
		m := newTestManager(t, st, func(o *auth.ConfigOptions) { o.LDAPProbeDial = probeDialer(freshProbeDir(), nil) })
		rep, err := m.TestAuthSection(ctx, auth.SectionLDAP, body(""))
		if err != nil {
			t.Fatalf("TestAuthSection: %v", err)
		}
		if !rep.OK {
			t.Fatalf("report = %+v, want ok", rep)
		}
		if rep.Message == "" || strings.Contains(rep.Message, "admin-pass") {
			t.Fatalf("message = %q, want a diagnostic without credentials", rep.Message)
		}
	})

	t.Run("full user bind form", func(t *testing.T) {
		st := openConfigStore(t)
		m := newTestManager(t, st, func(o *auth.ConfigOptions) { o.LDAPProbeDial = probeDialer(freshProbeDir(), nil) })
		rep, _ := m.TestAuthSection(ctx, auth.SectionLDAP,
			body(`,"testUsername":"alice","testPassword":"alice-pass"`))
		if !rep.OK || rep.Phase != "user_bind" {
			t.Fatalf("report = %+v, want the anchored user-bind success", rep)
		}
		if rep.Message != "Successfully connected and authenticated the test user" {
			t.Fatalf("message = %q, want the anchored §1.6 text", rep.Message)
		}
		// The anchored §1.6 rejection: only one of the two halves.
		rep2, _ := m.TestAuthSection(ctx, auth.SectionLDAP, body(`,"testUsername":"alice"`))
		if rep2.OK || rep2.Message != auth.ErrTestCredsIncomplete.Error() {
			t.Fatalf("report = %+v, want the anchored incomplete-creds rejection", rep2)
		}
	})

	t.Run("unreachable directory", func(t *testing.T) {
		st := openConfigStore(t)
		m := newTestManager(t, st, func(o *auth.ConfigOptions) {
			o.LDAPProbeDial = probeDialer(nil, errors.New("dial tcp 127.0.0.1:1: connect: connection refused"))
		})
		rep, _ := m.TestAuthSection(ctx, auth.SectionLDAP, body(""))
		if rep.OK || rep.Category != "unreachable" {
			t.Fatalf("report = %+v, want the unreachable failure form", rep)
		}
	})

	t.Run("wrong manager credentials", func(t *testing.T) {
		st := openConfigStore(t)
		m := newTestManager(t, st, func(o *auth.ConfigOptions) { o.LDAPProbeDial = probeDialer(freshProbeDir(), nil) })
		rep, _ := m.TestAuthSection(ctx, auth.SectionLDAP,
			[]byte(`{"enabled":true,"ldapUrl":"ldap://dir.example.com:389/dc=example,dc=com","search":{"searchFilter":"(uid={0})","managerDn":"cn=admin,dc=example,dc=com","managerPassword":"WRONG"}}`))
		if rep.OK || rep.Category != "bad_credentials" {
			t.Fatalf("report = %+v, want bad_credentials", rep)
		}
	})
}

func TestAuthConfigTestOIDC(t *testing.T) {
	ctx := context.Background()

	idpOK := httptestOIDCDiscovery(t, true)
	idpBad := httptestOIDCDiscovery(t, false)

	body := func(issuer string) []byte {
		return []byte(fmt.Sprintf(`{"enabled":true,"issuer_url":%q,"client_id":"c","redirect_url":"https://x/cb"}`, issuer))
	}

	t.Run("discovery success", func(t *testing.T) {
		st := openConfigStore(t)
		m := newTestManager(t, st, nil)
		rep, err := m.TestAuthSection(ctx, auth.SectionOIDC, body(idpOK.URL))
		if err != nil {
			t.Fatalf("TestAuthSection: %v", err)
		}
		if !rep.OK {
			t.Fatalf("report = %+v, want ok", rep)
		}
	})
	t.Run("malformed discovery document", func(t *testing.T) {
		st := openConfigStore(t)
		m := newTestManager(t, st, nil)
		rep, _ := m.TestAuthSection(ctx, auth.SectionOIDC, body(idpBad.URL))
		if rep.OK || rep.Category != "invalid" {
			t.Fatalf("report = %+v, want the invalid-metadata failure", rep)
		}
	})
	t.Run("unreachable issuer", func(t *testing.T) {
		st := openConfigStore(t)
		m := newTestManager(t, st, nil)
		rep, _ := m.TestAuthSection(ctx, auth.SectionOIDC, body("http://127.0.0.1:1"))
		if rep.OK || rep.Category != "unreachable" {
			t.Fatalf("report = %+v, want unreachable", rep)
		}
	})
	t.Run("disabled section", func(t *testing.T) {
		st := openConfigStore(t)
		m := newTestManager(t, st, nil)
		rep, _ := m.TestAuthSection(ctx, auth.SectionOIDC, []byte(`{"enabled":false}`))
		if rep.OK || rep.Category != "disabled" {
			t.Fatalf("report = %+v, want the disabled posture", rep)
		}
	})
}

func TestAuthConfigTestSAML(t *testing.T) {
	ctx := context.Background()
	ok := httptestSAMLTarget(t, http.StatusOK)
	bad := httptestSAMLTarget(t, http.StatusInternalServerError)

	body := func(url string) []byte {
		return []byte(fmt.Sprintf(`{"enableIntegration":true,"loginUrl":%q,"serviceProviderName":"binflow","certificate":%q}`, url, testSAMLCert(t)))
	}

	t.Run("login url reachable", func(t *testing.T) {
		st := openConfigStore(t)
		m := newTestManager(t, st, nil)
		rep, err := m.TestAuthSection(ctx, auth.SectionSAML, body(ok.URL))
		if err != nil {
			t.Fatalf("TestAuthSection: %v", err)
		}
		if !rep.OK {
			t.Fatalf("report = %+v, want ok", rep)
		}
	})
	t.Run("login url answers an error status", func(t *testing.T) {
		st := openConfigStore(t)
		m := newTestManager(t, st, nil)
		rep, _ := m.TestAuthSection(ctx, auth.SectionSAML, body(bad.URL))
		if rep.OK || rep.Category != "unreachable" {
			t.Fatalf("report = %+v, want the fetch failure form", rep)
		}
	})
}

// ---------------------------------------------------------------------------
// Probe fixtures (http legs)
// ---------------------------------------------------------------------------

// httptestOIDCDiscovery serves a valid or malformed discovery document at
// its own URL (the issuer IS the server URL).
func httptestOIDCDiscovery(t *testing.T, valid bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if !valid {
			_, _ = w.Write([]byte(`{"not":"oidc metadata"}`))
			return
		}
		issuer := "http://" + r.Host
		_, _ = fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q,"response_types_supported":["code"],"subject_types_supported":["public"],"id_token_signing_alg_values_supported":["RS256"]}`,
			issuer, issuer+"/auth", issuer+"/token", issuer+"/keys")
	}))
	t.Cleanup(srv.Close)
	return srv
}

// httptestSAMLTarget answers every GET with the given status.
func httptestSAMLTarget(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// testSAMLCert renders a parseable self-signed PEM certificate (the write
// path's §3.3 parse check and the test bodies share it).
func testSAMLCert(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		DNSNames:     []string{"idp.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("x509: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
