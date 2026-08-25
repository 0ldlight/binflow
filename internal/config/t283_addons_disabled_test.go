package config

import (
	"os"
	"path/filepath"
	"testing"
)

// M10 T-283: the addons.disabled circuit-breaker key (ADR-0032 / architecture
// section 15.5) — YAML CSV scalar, env override, empty default, strict-schema
// posture. The key's SEMANTICS (parsing, the core-id WARN, enforcement) are
// the license Manager's; this file pins only the config plane.

// TestT283AddonsDisabledYAML: the CSV scalar loads verbatim; an absent section
// stays the empty (nothing disabled) default.
func TestT283AddonsDisabledYAML(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{name: "absent section", yaml: "server:\n  listen: :0\n", want: ""},
		{name: "empty section", yaml: "addons: {}\n", want: ""},
		{name: "empty value", yaml: "addons:\n  disabled: \"\"\n", want: ""},
		{name: "single id", yaml: "addons:\n  disabled: npm\n", want: "npm"},
		{name: "csv with spaces", yaml: "addons:\n  disabled: npm, go ,ha\n", want: "npm, go ,ha"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "binflow.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Addons.Disabled != tt.want {
				t.Fatalf("addons.disabled = %q, want %q", cfg.Addons.Disabled, tt.want)
			}
		})
	}
}

// TestT283AddonsDisabledEnv: BINFLOW_ADDONS__DISABLED overrides the YAML
// value like every other config scalar.
func TestT283AddonsDisabledEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.yaml")
	if err := os.WriteFile(path, []byte("addons:\n  disabled: npm\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("BINFLOW_ADDONS__DISABLED", "go,nuget")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addons.Disabled != "go,nuget" {
		t.Fatalf("addons.disabled = %q, want the env value %q", cfg.Addons.Disabled, "go,nuget")
	}
}

// TestT283AddonsStrictSchema: the section stays strict — an unknown subkey or
// a YAML LIST under disabled (the key is a CSV scalar) refuses the load.
func TestT283AddonsStrictSchema(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{name: "unknown subkey", yaml: "addons:\n  enabled: true\n"},
		{name: "list form", yaml: "addons:\n  disabled:\n    - npm\n    - go\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "binflow.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			if _, err := Load(path); err == nil {
				t.Fatalf("load accepted an invalid addons section:\n%s", tt.yaml)
			}
		})
	}
}

// TestT283AddonsDisabledDefault: Defaults() carries the empty breaker (the
// bare `make build` boot changes nothing, invariant 1).
func TestT283AddonsDisabledDefault(t *testing.T) {
	if got := Defaults().Addons.Disabled; got != "" {
		t.Fatalf("Defaults().Addons.Disabled = %q, want empty", got)
	}
}
