package config

import "testing"

// TestReplicationAllowPrivateTargetResolution (T-210 / ADR-0025 decision 4):
// replication.allow_private_target is the explicit operator spelling of the
// replication engine's SSRF posture. Default true preserves the pre-config
// behavior (DenyPrivateTargets=false — private targets allowed, since every
// realistic replication target resolves to a private host); false is an
// explicit opt-in to denying private targets. Both env spellings are honored:
// the generic double-underscore form and the single-underscore convenience
// form.
func TestReplicationAllowPrivateTargetResolution(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		env  map[string]string
		want bool
	}{
		{name: "absent section defaults to true", want: true},
		{name: "explicit true", yaml: "replication:\n  allow_private_target: true\n", want: true},
		{name: "explicit false", yaml: "replication:\n  allow_private_target: false\n", want: false},
		{
			name: "generic double-underscore env wins over yaml",
			yaml: "replication:\n  allow_private_target: true\n",
			env:  map[string]string{"BINFLOW_REPLICATION__ALLOW_PRIVATE_TARGET": "false"},
			want: false,
		},
		{
			name: "single-underscore env spelling false",
			env:  map[string]string{"BINFLOW_REPLICATION_ALLOW_PRIVATE_TARGET": "false"},
			want: false,
		},
		{
			name: "single-underscore env spelling on",
			env:  map[string]string{"BINFLOW_REPLICATION_ALLOW_PRIVATE_TARGET": "on"},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := mustLoad(t, tc.yaml, tc.env)
			if c.Replication.AllowPrivateTarget != tc.want {
				t.Fatalf("Replication.AllowPrivateTarget = %v, want %v", c.Replication.AllowPrivateTarget, tc.want)
			}
		})
	}
}

// TestReplicationSectionStrictSchema: an unknown key inside replication is
// rejected by the strict decoder — the section is not a free-form bag.
func TestReplicationSectionStrictSchema(t *testing.T) {
	path := writeConfig(t, "replication:\n  allow_private_target: true\n  bogus: 1\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load succeeded with an unknown replication key, want strict-schema error")
	}
}
