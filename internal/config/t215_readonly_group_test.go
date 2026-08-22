// T-215: the federated read-only-admin group mapping keys (M7, ADR-0026
// decision 6 — oidc.readonly_group / ldap.readonly_group, same section and
// shape as admin_group). Absent keys keep the empty default (= no mapping =
// behavior identical to pre-M7), and both env spellings reach the fields.

package config

import "testing"

// TestReadOnlyGroupYAMLKeys: the two YAML keys decode into their config
// fields; absent keys stay empty (the zero-configuration contract the auth
// layer's ladder evaluation relies on).
func TestReadOnlyGroupYAMLKeys(t *testing.T) {
	c := mustLoad(t, `
auth:
  oidc:
    enabled: false
    admin_group: "binflow-admins"
    readonly_group: "binflow-auditors"
  ldap:
    enabled: false
    admin_group: "CN=BinFlowAdmins,CN=Users,DC=example,DC=com"
    readonly_group: "CN=BinFlowAuditors,CN=Users,DC=example,DC=com"
`, nil)
	if got, want := c.Auth.OIDC.ReadOnlyGroup, "binflow-auditors"; got != want {
		t.Errorf("OIDC.ReadOnlyGroup = %q, want %q", got, want)
	}
	if got, want := c.Auth.LDAP.ReadOnlyGroup, "CN=BinFlowAuditors,CN=Users,DC=example,DC=com"; got != want {
		t.Errorf("LDAP.ReadOnlyGroup = %q, want %q", got, want)
	}
	// The admin mapping is untouched by the sibling key.
	if got, want := c.Auth.OIDC.AdminGroup, "binflow-admins"; got != want {
		t.Errorf("OIDC.AdminGroup = %q, want %q", got, want)
	}
}

// TestReadOnlyGroupDefaultsEmpty: no keys, no mapping — the zero-change
// posture every pre-M7 config keeps.
func TestReadOnlyGroupDefaultsEmpty(t *testing.T) {
	c := mustLoad(t, "auth:\n  oidc:\n    enabled: false\n  ldap:\n    enabled: false\n", nil)
	if c.Auth.OIDC.ReadOnlyGroup != "" || c.Auth.LDAP.ReadOnlyGroup != "" {
		t.Errorf("readonly_group default = %q/%q, want empty/empty",
			c.Auth.OIDC.ReadOnlyGroup, c.Auth.LDAP.ReadOnlyGroup)
	}
}

// TestReadOnlyGroupEnvSpellings: both documented env forms land — the
// single-underscore spelling (BINFLOW_AUTH_OIDC_READONLY_GROUP) and the
// generic double-underscore path (BINFLOW_AUTH__LDAP__READONLY_GROUP), the
// T-210 pattern.
func TestReadOnlyGroupEnvSpellings(t *testing.T) {
	c := mustLoad(t, "", map[string]string{
		"BINFLOW_AUTH_OIDC_READONLY_GROUP":   "env-auditors",
		"BINFLOW_AUTH__LDAP__READONLY_GROUP": "CN=EnvAuditors,DC=example,DC=com",
	})
	if got, want := c.Auth.OIDC.ReadOnlyGroup, "env-auditors"; got != want {
		t.Errorf("OIDC.ReadOnlyGroup via env = %q, want %q", got, want)
	}
	if got, want := c.Auth.LDAP.ReadOnlyGroup, "CN=EnvAuditors,DC=example,DC=com"; got != want {
		t.Errorf("LDAP.ReadOnlyGroup via env = %q, want %q", got, want)
	}
}

// TestReadOnlyGroupStrictDecoderRejectsTypos: the strict decoder keeps
// guarding the section — a misspelled readonly key is a boot failure, not a
// silent no-op.
func TestReadOnlyGroupStrictDecoderRejectsTypos(t *testing.T) {
	for _, body := range []string{
		"auth:\n  oidc:\n    readonly_groups: \"x\"\n",
		"auth:\n  ldap:\n    readonly-group: \"x\"\n",
	} {
		if _, err := loadWithEnv(t, body, nil); err == nil {
			t.Errorf("Load(%q) succeeded, want the strict-decoder rejection", body)
		}
	}
}
