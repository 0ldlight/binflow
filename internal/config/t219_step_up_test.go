// T-219 acceptance, config plane (M7 FR-68 / ADR-0027 decision 6): the two
// auth keys — auth.token_step_up (bool, default false) and
// auth.token_step_up_grant_ttl_seconds (int, default 300, domain [60, 3600]
// enforced at boot). The strict-schema posture of T-179: an out-of-domain
// TTL refuses the boot even with the switch off.

package config

import (
	"testing"
	"time"
)

// TestTokenStepUpDefaults: an unconfigured boot carries the documented
// defaults — switch OFF (zero behavior change, the Q11 posture stands) and
// the 300s grant TTL.
func TestTokenStepUpDefaults(t *testing.T) {
	c := mustLoad(t, "", nil)
	if c.Auth.TokenStepUp {
		t.Error("auth.token_step_up defaults to true; ADR-0027 demands false")
	}
	if got, want := c.Auth.TokenStepUpGrantTTL, 300*time.Second; got != want {
		t.Errorf("TokenStepUpGrantTTL = %s, want %s", got, want)
	}
	if got, want := DefaultTokenStepUpGrantTTL, 300*time.Second; got != want {
		t.Errorf("DefaultTokenStepUpGrantTTL = %s, want %s", got, want)
	}
}

// TestTokenStepUpYAML: both keys decode from the auth section; a sibling key
// keeps its default.
func TestTokenStepUpYAML(t *testing.T) {
	c := mustLoad(t, "auth:\n  token_step_up: true\n  token_step_up_grant_ttl_seconds: 900\n", nil)
	if !c.Auth.TokenStepUp {
		t.Error("auth.token_step_up = false, want true")
	}
	if got, want := c.Auth.TokenStepUpGrantTTL, 900*time.Second; got != want {
		t.Errorf("TokenStepUpGrantTTL = %s, want %s", got, want)
	}
	if got, want := c.Auth.TokenNonAdminMaxTTL, DefaultTokenNonAdminMaxTTL; got != want {
		t.Errorf("TokenNonAdminMaxTTL = %s, want untouched default %s", got, want)
	}
}

// TestTokenStepUpEnvDualSpelling: both documented spellings of each key
// override YAML — the single-underscore convenience form and the generic
// BINFLOW_<SECTION>__<KEY> form.
func TestTokenStepUpEnvDualSpelling(t *testing.T) {
	doc := "auth:\n  token_step_up: false\n  token_step_up_grant_ttl_seconds: 120\n"
	cases := []struct {
		name string
		env  map[string]string
		want bool
		ttl  time.Duration
	}{
		{
			name: "single-underscore spellings",
			env:  map[string]string{"BINFLOW_AUTH_TOKEN_STEP_UP": "true", "BINFLOW_AUTH_TOKEN_STEP_UP_GRANT_TTL_SECONDS": "600"},
			want: true, ttl: 600 * time.Second,
		},
		{
			name: "generic double-underscore spellings",
			env:  map[string]string{"BINFLOW_AUTH__TOKEN_STEP_UP": "on", "BINFLOW_AUTH__TOKEN_STEP_UP_GRANT_TTL_SECONDS": "3600"},
			want: true, ttl: 3600 * time.Second,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := loadWithEnv(t, doc, tc.env)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if c.Auth.TokenStepUp != tc.want {
				t.Errorf("TokenStepUp = %v, want %v", c.Auth.TokenStepUp, tc.want)
			}
			if c.Auth.TokenStepUpGrantTTL != tc.ttl {
				t.Errorf("TokenStepUpGrantTTL = %s, want %s", c.Auth.TokenStepUpGrantTTL, tc.ttl)
			}
		})
	}
}

// TestTokenStepUpGrantTTLDomain: the [60, 3600] window is enforced on boot —
// the inclusive bounds boot, everything outside is refused — REGARDLESS of
// the switch (strict schema: an operator who spelled the key stated intent).
func TestTokenStepUpGrantTTLDomain(t *testing.T) {
	cases := []struct {
		name    string
		doc     string
		wantErr bool
	}{
		{name: "60 is the inclusive floor", doc: "auth:\n  token_step_up_grant_ttl_seconds: 60\n"},
		{name: "3600 is the inclusive ceiling", doc: "auth:\n  token_step_up_grant_ttl_seconds: 3600\n"},
		{name: "59 refuses the boot", doc: "auth:\n  token_step_up_grant_ttl_seconds: 59\n", wantErr: true},
		{name: "3601 refuses the boot", doc: "auth:\n  token_step_up_grant_ttl_seconds: 3601\n", wantErr: true},
		{
			name:    "out-of-domain refuses the boot even with the switch off",
			doc:     "auth:\n  token_step_up: false\n  token_step_up_grant_ttl_seconds: 5\n",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadWithEnv(t, tc.doc, nil)
			if tc.wantErr && err == nil {
				t.Fatal("Load succeeded, want a domain error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Load: %v", err)
			}
			if tc.wantErr {
				if want := "auth.token_step_up_grant_ttl_seconds"; !stringContains(err.Error(), want) {
					t.Errorf("error %q does not name the key %q", err.Error(), want)
				}
			}
		})
	}
}

// TestTokenStepUpEnvOutOfRangeRefusesBoot: the env override path lands in the
// same Validate domain check (not just the YAML path).
func TestTokenStepUpEnvOutOfRangeRefusesBoot(t *testing.T) {
	if _, err := loadWithEnv(t, "", map[string]string{"BINFLOW_AUTH_TOKEN_STEP_UP_GRANT_TTL_SECONDS": "10"}); err == nil {
		t.Error("out-of-domain env override was accepted")
	}
}

// TestTokenStepUpUnknownEnvIsRejected: an unrecognized BINFLOW_AUTH_TOKEN_*
// variable is unknown, not silently ignored (the loader's blanket rule).
func TestTokenStepUpUnknownEnvIsRejected(t *testing.T) {
	if _, err := loadWithEnv(t, "", map[string]string{"BINFLOW_AUTH_TOKEN_STEPUP": "true"}); err == nil {
		t.Error("unknown env variable BINFLOW_AUTH_TOKEN_STEPUP was accepted")
	}
}

// stringContains is strings.Contains (named locally to keep the test's
// imports to the two it needs).
func stringContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
