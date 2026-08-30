package config

import "testing"

// TestWebhookAllowPrivateTargetResolution (M13 T-362 / ADR-0041 decision 6,
// regression for the T-366 §4-1 dead key): webhook.allow_private_target is
// the operator spelling of the webhook plane's SSRF posture. Default FALSE —
// the deliberate asymmetry against replication's true: webhook targets are
// REST-CRUD dynamic (any subscription writer can spell an outbound URL),
// the classic SSRF escalation surface, so deny is the safe default. Both
// env spellings are honored like every other bool key.
func TestWebhookAllowPrivateTargetResolution(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		env  map[string]string
		want bool
	}{
		{name: "absent section defaults to false", want: false},
		{name: "explicit true", yaml: "webhook:\n  allow_private_target: true\n", want: true},
		{name: "explicit false", yaml: "webhook:\n  allow_private_target: false\n", want: false},
		{
			name: "generic double-underscore env wins over yaml",
			yaml: "webhook:\n  allow_private_target: false\n",
			env:  map[string]string{"BINFLOW_WEBHOOK__ALLOW_PRIVATE_TARGET": "true"},
			want: true,
		},
		{
			name: "single-underscore env spelling on",
			env:  map[string]string{"BINFLOW_WEBHOOK_ALLOW_PRIVATE_TARGET": "on"},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := mustLoad(t, tc.yaml, tc.env)
			if c.Webhook.AllowPrivateTarget != tc.want {
				t.Fatalf("Webhook.AllowPrivateTarget = %v, want %v", c.Webhook.AllowPrivateTarget, tc.want)
			}
		})
	}
}

// TestWebhookSectionStrictSchema: an unknown key inside webhook is rejected
// by the strict decoder — the section is not a free-form bag.
func TestWebhookSectionStrictSchema(t *testing.T) {
	path := writeConfig(t, "webhook:\n  allow_private_target: true\n  bogus: 1\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load succeeded with an unknown webhook key, want strict-schema error")
	}
}
