// T-190 / K9 acceptance surface: the auth.token_nonadmin_max_ttl key — the
// cap on the lifetime a non-admin caller may request on
// POST /api/security/token (PRD M6 v1.2 §7 Q11 guardrail 2, T-188 ruling).
// Table-driven coverage of the default (365d, Artifactory's
// access.token.non.admin.max.expires.in value), YAML decoding, both env
// spellings, and the positive-value validation.

package config

import (
	"strings"
	"testing"
	"time"
)

// TestTokenNonAdminMaxTTLDefault: an unconfigured boot carries the documented
// 365-day cap (Defaults() is also what Load falls back to).
func TestTokenNonAdminMaxTTLDefault(t *testing.T) {
	c := mustLoad(t, "", nil)
	if got, want := c.Auth.TokenNonAdminMaxTTL, DefaultTokenNonAdminMaxTTL; got != want {
		t.Errorf("TokenNonAdminMaxTTL = %s, want default %s", got, want)
	}
	if want := 365 * 24 * time.Hour; DefaultTokenNonAdminMaxTTL != want {
		t.Errorf("DefaultTokenNonAdminMaxTTL = %s, want %s (Artifactory parity)", DefaultTokenNonAdminMaxTTL, want)
	}
}

// TestTokenNonAdminMaxTTLYAML: the auth section decodes the seconds-valued
// key; sibling keys keep their defaults.
func TestTokenNonAdminMaxTTLYAML(t *testing.T) {
	c := mustLoad(t, "auth:\n  token_nonadmin_max_ttl: 3600\n", nil)
	if got, want := c.Auth.TokenNonAdminMaxTTL, time.Hour; got != want {
		t.Errorf("TokenNonAdminMaxTTL = %s, want %s", got, want)
	}
	if got, want := c.Auth.TokenDefaultTTL, DefaultTokenTTL; got != want {
		t.Errorf("TokenDefaultTTL = %s, want untouched default %s", got, want)
	}
}

// TestTokenNonAdminMaxTTLEnv: both accepted spellings override YAML — the
// K9-documented single-underscore form and the generic BINFLOW_<S>__<K> form.
func TestTokenNonAdminMaxTTLEnv(t *testing.T) {
	for name, env := range map[string]string{
		"K9 spelling":         "BINFLOW_AUTH_TOKEN_NONADMIN_MAX_TTL",
		"generic __ spelling": "BINFLOW_AUTH__TOKEN_NONADMIN_MAX_TTL",
	} {
		t.Run(name, func(t *testing.T) {
			c := mustLoad(t, "auth:\n  token_nonadmin_max_ttl: 60\n",
				map[string]string{env: "7200"})
			if got, want := c.Auth.TokenNonAdminMaxTTL, 2*time.Hour; got != want {
				t.Errorf("TokenNonAdminMaxTTL = %s, want %s (env wins over YAML)", got, want)
			}
		})
	}
}

// TestTokenNonAdminMaxTTLValidation: a non-positive cap refuses the boot — a
// zero cap would brick every non-admin token create (the guardrail demands
// 0 < ttl <= cap). The YAML spellings fail in Validate; the env spelling is
// rejected as a positive integer before Validate even runs.
func TestTokenNonAdminMaxTTLValidation(t *testing.T) {
	t.Run("yaml zero", func(t *testing.T) {
		_, err := loadWithEnv(t, "auth:\n  token_nonadmin_max_ttl: 0\n", nil)
		if err == nil || !strings.Contains(err.Error(), "token_nonadmin_max_ttl must be positive") {
			t.Fatalf("err = %v, want the positivity failure", err)
		}
	})
	t.Run("yaml negative", func(t *testing.T) {
		_, err := loadWithEnv(t, "auth:\n  token_nonadmin_max_ttl: -1\n", nil)
		if err == nil || !strings.Contains(err.Error(), "token_nonadmin_max_ttl must be positive") {
			t.Fatalf("err = %v, want the positivity failure", err)
		}
	})
	t.Run("env zero", func(t *testing.T) {
		_, err := loadWithEnv(t, "",
			map[string]string{"BINFLOW_AUTH_TOKEN_NONADMIN_MAX_TTL": "0"})
		if err == nil {
			t.Fatal("err = nil, want the positive-integer env rejection")
		}
	})
}
