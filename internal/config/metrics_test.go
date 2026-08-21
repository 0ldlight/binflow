// metrics section tests (T-163): the metrics.require_auth knob follows the
// T-179 config-section pattern — YAML key, default false, env override, and
// the strict schema rejecting unknown spellings inside the section.

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeMetricsConfig writes a YAML file and returns its path.
func writeMetricsConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "binflow.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// TestMetricsRequireAuthResolution walks the three resolution legs.
func TestMetricsRequireAuthResolution(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		env  map[string]string
		want bool
	}{
		{name: "absent section defaults to false", yaml: "", want: false},
		{name: "explicit false", yaml: "metrics:\n  require_auth: false\n", want: false},
		{name: "explicit true", yaml: "metrics:\n  require_auth: true\n", want: true},
		{
			name: "env override wins over yaml",
			yaml: "metrics:\n  require_auth: true\n",
			env:  map[string]string{"BINFLOW_METRICS__REQUIRE_AUTH": "0"},
			want: false,
		},
		{
			name: "env override without yaml",
			env:  map[string]string{"BINFLOW_METRICS__REQUIRE_AUTH": "true"},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeMetricsConfig(t, tc.yaml)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Metrics.RequireAuth != tc.want {
				t.Fatalf("Metrics.RequireAuth = %v, want %v", cfg.Metrics.RequireAuth, tc.want)
			}
		})
	}
}

// TestMetricsSectionStrictSchema: an unknown key inside metrics is rejected
// by the strict decoder (the section is not a free-form bag).
func TestMetricsSectionStrictSchema(t *testing.T) {
	path := writeMetricsConfig(t, "metrics:\n  require_auth: true\n  sample_rate: 0.1\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load succeeded with an unknown metrics key, want strict-schema error")
	}
}
