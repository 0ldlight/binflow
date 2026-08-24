// T-256 acceptance surface: the storage.gc_hold_ttl_seconds key
// ([M9] ADR-0031 / architecture section 8): YAML decode, the 0
// sentinel-default (the engine applies DefaultGCHoldTTL), explicit-zero as
// the documented "take the default" spelling, the env override, and the
// boot-refusing domain — a CONFIGURED value below MinGCHoldTTL (60s) fails
// Validate instead of clamping, because a hold TTL shorter than the
// Commit-to-metadata-commit window silently re-opens the W-1 race.

package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadStorageGCHoldTTL(t *testing.T) {
	tests := []struct {
		name string
		body string
		env  map[string]string
		want time.Duration
	}{
		{"absent keeps the sentinel default", "storage:\n  gc_grace_hours: 24\n", nil, 0},
		{"explicit yaml value", "storage:\n  gc_hold_ttl_seconds: 300\n", nil, 300 * time.Second},
		{"explicit yaml zero is the default sentinel", "storage:\n  gc_hold_ttl_seconds: 0\n", nil, 0},
		{"env override", "", map[string]string{"BINFLOW_STORAGE__GC_HOLD_TTL_SECONDS": "120"}, 120 * time.Second},
		{"env wins over yaml", "storage:\n  gc_hold_ttl_seconds: 300\n",
			map[string]string{"BINFLOW_STORAGE__GC_HOLD_TTL_SECONDS": "900"}, 900 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := mustLoad(t, tt.body, tt.env)
			if got := c.Storage.GCHoldTTL; got != tt.want {
				t.Errorf("Storage.GCHoldTTL = %s, want %s", got, tt.want)
			}
			if err := c.Validate(); err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestValidateStorageGCHoldTTLDomain(t *testing.T) {
	tests := []struct {
		name    string
		value   time.Duration
		wantErr string // "" = accepted
	}{
		{"zero sentinel", 0, ""},
		{"the 60s floor itself", MinGCHoldTTL, ""},
		{"above the floor", 600 * time.Second, ""},
		{"one second below the floor refuses", 59 * time.Second, "must be at least 60 seconds"},
		{"a negative value refuses", -time.Second, "must be a non-negative integer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Defaults()
			c.Storage.GCHoldTTL = tt.value
			err := c.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate(GCHoldTTL=%s) = %v, want nil", tt.value, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate(GCHoldTTL=%s) = %v, want it to contain %q", tt.value, err, tt.wantErr)
			}
		})
	}
}
