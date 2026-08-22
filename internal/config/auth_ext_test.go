// T-179 acceptance surface, arm 1: the auth.oidc / auth.ldap sections
// (AC 1). Table-driven coverage of section decoding, the disabled-by-default
// posture (zero behavior change), the strict decoder's rejection of unknown
// keys, the rejectSecrets rule for the two env-only secrets, and the
// enabled=true validation requirements.

package config

import (
	"reflect"
	"strings"
	"testing"
)

// TestAuthProvidersDisabledByDefault (AC 1, "默认 enabled=false"): an empty
// or section-free config leaves both identity providers disabled with an
// empty OIDC payload and the documented LDAP pool default — the pre-M6
// local-only boot, byte-for-byte.
func TestAuthProvidersDisabledByDefault(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"empty file", ""},
		{"auth section without providers", "auth:\n  argon2_memory_mb: 128\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mustLoad(t, tc.body, nil)
			if c.Auth.OIDC.Enabled {
				t.Error("OIDC.Enabled = true, want false by default")
			}
			if c.Auth.LDAP.Enabled {
				t.Error("LDAP.Enabled = true, want false by default")
			}
			if c.Auth.OIDC.IssuerURL != "" || c.Auth.OIDC.ClientID != "" ||
				c.Auth.OIDC.RedirectURL != "" || len(c.Auth.OIDC.Scopes) != 0 {
				t.Errorf("OIDC = %+v, want the zero payload", c.Auth.OIDC)
			}
			if got, want := c.Auth.LDAP.PoolSize, DefaultLDAPPoolSize; got != want {
				t.Errorf("LDAP.PoolSize = %d, want default %d", got, want)
			}
			if c.Auth.OIDC.ClientSecret != "" || c.Auth.LDAP.BindPassword != "" {
				t.Error("secrets populated without their env variables")
			}
		})
	}
}

// TestLoadAuthOIDCSection: the full auth.oidc section decodes field for
// field; absent keys keep their defaults.
func TestLoadAuthOIDCSection(t *testing.T) {
	c := mustLoad(t, `
auth:
  oidc:
    enabled: true
    issuer_url: "https://keycloak.example.com/realms/myorg"
    client_id: "binflow"
    redirect_url: "https://binflow.example.com/binflow/api/v1/oidc/callback"
    scopes: ["openid", "profile", "email", "groups"]
    user_claim: "name"
    group_claim: "realm_access.roles"
    admin_group: "binflow-admins"
`, map[string]string{OIDCClientSecretEnvVar: "env-only-secret"})

	want := OIDCConfig{
		Enabled:      true,
		IssuerURL:    "https://keycloak.example.com/realms/myorg",
		ClientID:     "binflow",
		ClientSecret: "env-only-secret",
		RedirectURL:  "https://binflow.example.com/binflow/api/v1/oidc/callback",
		Scopes:       []string{"openid", "profile", "email", "groups"},
		UserClaim:    "name",
		GroupClaim:   "realm_access.roles",
		AdminGroup:   "binflow-admins",
	}
	if !reflect.DeepEqual(c.Auth.OIDC, want) {
		t.Errorf("OIDC = %+v,\nwant %+v", c.Auth.OIDC, want)
	}
}

// TestLoadAuthLDAPSection: the full auth.ldap section decodes field for
// field; absent optionals stay empty, pool_size carries the explicit value.
// The T-186 keys (group_base_dn / skip_tls_verify) are part of the full
// section (T-174 D6: they used to be rejected by the strict decoder).
func TestLoadAuthLDAPSection(t *testing.T) {
	c := mustLoad(t, `
auth:
  ldap:
    enabled: true
    url: "ldap://ad.example.com:389"
    base_dn: "DC=example,DC=com"
    bind_dn: "CN=binflow-bind,CN=Users,DC=example,DC=com"
    user_filter: "(&(objectClass=user)(sAMAccountName=%s))"
    user_id_attr: "sAMAccountName"
    group_filter: "(&(objectClass=group)(member=%s))"
    group_base_dn: "OU=Groups,DC=example,DC=com"
    group_name_attr: "cn"
    admin_group: "CN=BinFlowAdmins,CN=Users,DC=example,DC=com"
    pool_size: 12
    start_tls: true
    skip_tls_verify: true
`, map[string]string{LDAPBindPasswordEnvVar: "env-only-bind-pw"})

	want := LDAPConfig{
		Enabled:       true,
		URL:           "ldap://ad.example.com:389",
		BaseDN:        "DC=example,DC=com",
		BindDN:        "CN=binflow-bind,CN=Users,DC=example,DC=com",
		BindPassword:  "env-only-bind-pw",
		UserFilter:    "(&(objectClass=user)(sAMAccountName=%s))",
		UserIDAttr:    "sAMAccountName",
		GroupFilter:   "(&(objectClass=group)(member=%s))",
		GroupBaseDN:   "OU=Groups,DC=example,DC=com",
		GroupNameAttr: "cn",
		AdminGroup:    "CN=BinFlowAdmins,CN=Users,DC=example,DC=com",
		PoolSize:      12,
		StartTLS:      true,
		SkipTLSVerify: true,
	}
	if c.Auth.LDAP != want {
		t.Errorf("LDAP = %+v,\nwant %+v", c.Auth.LDAP, want)
	}
}

// TestLoadAuthLDAPTLSDefaults (T-186 AC 2): skip_tls_verify defaults to
// false (NFR-S36 — verification is always on unless the operator opts out)
// and group_base_dn defaults to empty (= base_dn, resolved in internal/auth
// at construction); the start_tls default stays false.
func TestLoadAuthLDAPTLSDefaults(t *testing.T) {
	c := mustLoad(t, `
auth:
  ldap:
    enabled: true
    url: "ldap://ad.example.com:389"
    base_dn: "DC=example,DC=com"
`, nil)
	if c.Auth.LDAP.SkipTLSVerify {
		t.Error("LDAP.SkipTLSVerify = true, want false by default")
	}
	if c.Auth.LDAP.StartTLS {
		t.Error("LDAP.StartTLS = true, want false by default")
	}
	if c.Auth.LDAP.GroupBaseDN != "" {
		t.Errorf("LDAP.GroupBaseDN = %q, want empty (= base_dn) by default", c.Auth.LDAP.GroupBaseDN)
	}
}

// TestAuthProvidersSecretsRejectedInYAML (AC 1, rejectSecrets 铁律): the two
// env-only secrets are rejected with the pointed message wherever they are
// spelled — inside their own sections, mis-nested one level up, or at the
// top level.
func TestAuthProvidersSecretsRejectedInYAML(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
		hint string
	}{
		{
			"oidc client_secret",
			"auth:\n  oidc:\n    enabled: false\n    client_secret: \"nope\"\n",
			`auth.oidc.client_secret`,
			OIDCClientSecretEnvVar,
		},
		{
			"oidc clientsecret spelling",
			"auth:\n  oidc:\n    clientsecret: \"nope\"\n",
			`auth.oidc.clientsecret`,
			OIDCClientSecretEnvVar,
		},
		{
			"ldap bind_password",
			"auth:\n  ldap:\n    enabled: false\n    bind_password: \"nope\"\n",
			`auth.ldap.bind_password`,
			LDAPBindPasswordEnvVar,
		},
		{
			"secret mis-nested directly under auth",
			"auth:\n  bind_password: \"nope\"\n",
			`auth.bind_password`,
			LDAPBindPasswordEnvVar,
		},
		{
			"secret at the top level",
			"client_secret: \"nope\"\n",
			`client_secret`,
			OIDCClientSecretEnvVar,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadWithEnv(t, tc.body, nil)
			if err == nil {
				t.Fatal("Load() error = nil, want the secret-in-YAML rejection")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to name %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), tc.hint) {
				t.Errorf("error = %v, want the escape hatch to name %q", err, tc.hint)
			}
		})
	}
}

// TestAuthProvidersStrictDecoderRejectsUnknownKeys (AC 1): the strict
// decoder fails fast on unknown keys inside the two new sections (and on an
// unknown provider name under auth).
func TestAuthProvidersStrictDecoderRejectsUnknownKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"unknown oidc key", "auth:\n  oidc:\n    issuer: \"https://x\"\n"},
		{"unknown ldap key", "auth:\n  ldap:\n    admin_group_dn: \"cn=a\"\n"},
		{"unknown provider section", "auth:\n  saml:\n    enabled: true\n"},
		{"typo in enabled", "auth:\n  oidc:\n    enbaled: true\n"},
		{"typo in skip_tls_verify", "auth:\n  ldap:\n    skip_tls_verrify: true\n"},
		{"typo in group_base_dn", "auth:\n  ldap:\n    group_base_dns: \"ou=g\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadWithEnv(t, tc.body, nil)
			if err == nil {
				t.Fatal("Load() error = nil, want strict-decoder rejection")
			}
		})
	}
}

// TestAuthProvidersSecretsFromEnv: the two env-only secrets land in their
// config fields case-insensitively and an empty value counts as unset.
func TestAuthProvidersSecretsFromEnv(t *testing.T) {
	c := mustLoad(t, "", map[string]string{
		"binflow_auth_oidc_client_secret": "oidc-secret-value",
		LDAPBindPasswordEnvVar:            "",
	})
	if got, want := c.Auth.OIDC.ClientSecret, "oidc-secret-value"; got != want {
		t.Errorf("OIDC.ClientSecret = %q, want %q (case-insensitive env name)", got, want)
	}
	if c.Auth.LDAP.BindPassword != "" {
		t.Errorf("LDAP.BindPassword = %q, want empty for an unset env value", c.Auth.LDAP.BindPassword)
	}
}

// TestValidateAuthProviders: the enabled=true requirements (AC 1 + fail-fast
// posture). Table rows pin each missing field and each malformed URL.
func TestValidateAuthProviders(t *testing.T) {
	validOIDC := func() OIDCConfig {
		return OIDCConfig{
			Enabled:     true,
			IssuerURL:   "https://idp.example.com",
			ClientID:    "binflow",
			RedirectURL: "https://binflow.example.com/binflow/api/v1/oidc/callback",
		}
	}
	for _, tc := range []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			"oidc missing issuer",
			func(c *Config) { c.Auth.OIDC = validOIDC(); c.Auth.OIDC.IssuerURL = "" },
			"auth.oidc.issuer_url is required",
		},
		{
			"oidc missing client id",
			func(c *Config) { c.Auth.OIDC = validOIDC(); c.Auth.OIDC.ClientID = "" },
			"auth.oidc.client_id is required",
		},
		{
			"oidc missing redirect",
			func(c *Config) { c.Auth.OIDC = validOIDC(); c.Auth.OIDC.RedirectURL = "" },
			"auth.oidc.redirect_url is required",
		},
		{
			"oidc issuer wrong scheme",
			func(c *Config) { c.Auth.OIDC = validOIDC(); c.Auth.OIDC.IssuerURL = "ldap://idp.example.com" },
			`scheme must be http or https`,
		},
		{
			"oidc issuer not a url",
			func(c *Config) { c.Auth.OIDC = validOIDC(); c.Auth.OIDC.IssuerURL = "idp.example.com/realm" },
			"must be an absolute http(s) URL",
		},
		{
			"ldap missing url",
			func(c *Config) { c.Auth.LDAP = LDAPConfig{Enabled: true, BaseDN: "dc=x", PoolSize: 5} },
			"auth.ldap.url is required",
		},
		{
			"ldap missing base dn",
			func(c *Config) { c.Auth.LDAP = LDAPConfig{Enabled: true, URL: "ldap://dir:389", PoolSize: 5} },
			"auth.ldap.base_dn is required",
		},
		{
			"ldap wrong scheme",
			func(c *Config) {
				c.Auth.LDAP = LDAPConfig{Enabled: true, URL: "http://dir:389", BaseDN: "dc=x", PoolSize: 5}
			},
			"scheme must be ldap or ldaps",
		},
		{
			"ldap hostless url",
			func(c *Config) {
				c.Auth.LDAP = LDAPConfig{Enabled: true, URL: "ldap://", BaseDN: "dc=x", PoolSize: 5}
			},
			"must be an ldap(s) URL",
		},
		{
			"ldap zero pool",
			func(c *Config) {
				c.Auth.LDAP = LDAPConfig{Enabled: true, URL: "ldap://dir:389", BaseDN: "dc=x", PoolSize: 0}
			},
			"auth.ldap.pool_size must be positive",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Defaults()
			c.Storage.DataDir = t.TempDir()
			tc.mutate(c)
			err := c.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want failure")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Validate() error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}

	// Both sections fully configured pass validation.
	t.Run("both enabled and valid", func(t *testing.T) {
		c := Defaults()
		c.Storage.DataDir = t.TempDir()
		c.Auth.OIDC = validOIDC()
		c.Auth.LDAP = LDAPConfig{
			Enabled: true, URL: "ldap://dir.example.com:389", BaseDN: "dc=example,dc=com", PoolSize: 5,
		}
		if err := c.Validate(); err != nil {
			t.Fatalf("Validate() error = %v, want nil", err)
		}
	})
}
