// T-204 acceptance surface, arm 2 (T-192 leftover 1): the
// auth.hash_concurrency key decodes, stays at its sentinel default of 0
// (unset = the auth service derives GOMAXPROCS clamped to [1,16], zero
// behavior change), is overridable through the generic env mapping, and
// rejects nonsense values before the WithHashConcurrency builder's
// never-reached panic assertion.

package config

import (
	"strconv"
	"strings"
	"testing"
)

// Table: YAML decode, the sentinel default, explicit-zero-as-unset, and
// the env override (generic BINFLOW_AUTH__HASH_CONCURRENCY mapping).
func TestLoadAuthHashConcurrency(t *testing.T) {
	tests := []struct {
		name string
		body string
		env  map[string]string
		want int
	}{
		{"absent keeps the sentinel default", "auth:\n  argon2_memory_mb: 64\n", nil, 0},
		{"explicit yaml value", "auth:\n  hash_concurrency: 4\n", nil, 4},
		{"explicit yaml zero counts as unset", "auth:\n  hash_concurrency: 0\n", nil, 0},
		{"env override", "", map[string]string{"BINFLOW_AUTH__HASH_CONCURRENCY": "9"}, 9},
		{"env wins over yaml", "auth:\n  hash_concurrency: 4\n",
			map[string]string{"BINFLOW_AUTH__HASH_CONCURRENCY": "7"}, 7},
		{"large override passes through unclamped", "auth:\n  hash_concurrency: 64\n", nil, 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := mustLoad(t, tt.body, tt.env)
			if got := c.Auth.HashConcurrency; got != tt.want {
				t.Errorf("Auth.HashConcurrency = %d, want %d", got, tt.want)
			}
		})
	}
}

// The resolution passes the operator's number through UNCLAMPED: sizing
// (and any clamping) belongs to the auth service's builder, so a value
// above the default ceiling survives Load+Validate intact for cmd's
// one-line wiring (0 = skip the builder, positive = WithHashConcurrency(n)
// verbatim). A helpful clamp here would silently shrink an operator's
// explicit override.
func TestAuthHashConcurrencyPassThroughUnclamped(t *testing.T) {
	for _, n := range []int{1, 3, 16, 17, 64} {
		c := mustLoad(t, "auth:\n  hash_concurrency: "+strconv.Itoa(n)+"\n", nil)
		if got := c.Auth.HashConcurrency; got != n {
			t.Errorf("hash_concurrency %d resolved as %d, want verbatim", n, got)
		}
		if err := c.Validate(); err != nil {
			t.Errorf("Validate(hash_concurrency=%d) = %v, want nil", n, err)
		}
	}
}

// Table: nonsense spellings fail the load — negative YAML (Validate),
// non-positive or non-numeric env (envIntPos), unknown-key spellings stay
// rejected by the strict decoder.
func TestLoadAuthHashConcurrencyRejections(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "negative yaml",
			body:    "auth:\n  hash_concurrency: -2\n",
			wantErr: "auth.hash_concurrency must be a positive integer when set",
		},
		{
			name:    "env zero",
			env:     map[string]string{"BINFLOW_AUTH__HASH_CONCURRENCY": "0"},
			wantErr: "must be a positive integer",
		},
		{
			name:    "env negative",
			env:     map[string]string{"BINFLOW_AUTH__HASH_CONCURRENCY": "-4"},
			wantErr: "must be a positive integer",
		},
		{
			name:    "env non-numeric",
			env:     map[string]string{"BINFLOW_AUTH__HASH_CONCURRENCY": "four"},
			wantErr: "invalid syntax",
		},
		{
			name:    "camelCase spelling stays unknown",
			body:    "auth:\n  hashConcurrency: 4\n",
			wantErr: "field hashConcurrency not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWithEnv(t, tt.body, tt.env)
			if err == nil {
				t.Fatal("Load() error = nil, want rejection")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Load() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// A hand-built Config (the assembly re-check path Validate is exported
// for) rejects a negative knob the same way Load does.
func TestValidateAuthHashConcurrencyNegative(t *testing.T) {
	t.Chdir(t.TempDir())
	c := Defaults()
	c.Auth.HashConcurrency = -1
	if err := c.Validate(); err == nil ||
		!strings.Contains(err.Error(), "auth.hash_concurrency must be a positive integer when set") {
		t.Errorf("Validate() = %v, want the hash_concurrency rejection", err)
	}
	c.Auth.HashConcurrency = 0
	if err := c.Validate(); err != nil {
		t.Errorf("Validate() with sentinel 0 = %v, want nil", err)
	}
}
