package main

import "testing"

// TestT394DescribeConfigPath pins the startup line's config provenance
// (T-376's wording leftover, corrected in T-394): an explicit --config is
// echoed verbatim, and the no-file boot must read as the DESIGNED posture
// it is — built-in defaults plus environment overrides — not as a file
// that went missing ("no binflow.yaml found").
func TestT394DescribeConfigPath(t *testing.T) {
	tests := []struct {
		name     string
		explicit string
		want     string
	}{
		{"explicit config echoes the path", "/etc/binflow/binflow.yaml",
			"/etc/binflow/binflow.yaml"},
		{"no-file boot states the built-in defaults posture", "",
			"built-in defaults (no custom binflow.yaml; environment overrides applied)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeConfigPath(tc.explicit); got != tc.want {
				t.Fatalf("describeConfigPath(%q) = %q, want %q", tc.explicit, got, tc.want)
			}
		})
	}
}
