package adapter

// Byte-parity guard for the hoisted VirtualDeploymentTarget probe (T-590 /
// BIN-72): the golden below pins the tolerant write-route semantics over the
// alias triple × key-case × missing × invalid corpus, verified as
// byte-identical across the eight pre-hoist lockstep copies (mechanical
// extraction harness, hash proof in reports/agents/T-590.md). Any edit to
// the alias set or the tolerance rule — or a re-added local copy drifting
// from it — surfaces here as a failure that cannot be silenced without
// consciously updating the golden.
import "testing"

func TestVirtualDeploymentTargetGolden(t *testing.T) {
	cases := []struct {
		name   string
		config string
		want   string
	}{
		{"empty config", "", ""},
		{"no aliases", `{}`, ""},
		{"whitespace config", "   ", ""},
		{"unterminated json", `{"defaultDeploymentRepo":`, ""},
		{"non-object shape", `[1,2,3]`, ""},
		{"wrong-typed alias", `{"defaultDeploymentRepo":5}`, ""},
		{"null object", `null`, ""},

		{"primary alone", `{"defaultDeploymentRepo":"libs-release"}`, "libs-release"},
		{"ref alias alone", `{"defaultDeploymentRepoRef":"ref-t"}`, "ref-t"},
		{"third alias alone", `{"deploymentRepository":"dep-t"}`, "dep-t"},

		{"primary wins over both", `{"defaultDeploymentRepo":"a","defaultDeploymentRepoRef":"b","deploymentRepository":"c"}`, "a"},
		{"ref wins over third", `{"defaultDeploymentRepoRef":"b","deploymentRepository":"c"}`, "b"},
		{"empty primary skipped", `{"defaultDeploymentRepo":"","defaultDeploymentRepoRef":"b","deploymentRepository":"c"}`, "b"},
		{"empty primary and ref skipped", `{"defaultDeploymentRepo":"","defaultDeploymentRepoRef":"","deploymentRepository":"c"}`, "c"},
		{"all empty answers no route", `{"defaultDeploymentRepo":"","defaultDeploymentRepoRef":"","deploymentRepository":""}`, ""},

		{"pascal-case key matches tag case-insensitively", `{"DefaultDeploymentRepo":"x"}`, "x"},
		{"upper-case key matches tag case-insensitively", `{"DEFAULTDEPLOYMENTREPOREF":"x"}`, "x"},
		{"unknown key ignored", `{"DefaultdeploymentrepoTypo":"x"}`, ""},
		{"unknown key with known third", `{"zzz":"x","deploymentRepository":"d"}`, "d"},

		{"value returned verbatim not trimmed", `{"defaultDeploymentRepo":"  spaced  "}`, "  spaced  "},
		{"extra fields ignored", `{"repoType":"virtual","defaultDeploymentRepo":"v"}`, "v"},
	}
	for _, tc := range cases {
		if got := VirtualDeploymentTarget(tc.config); got != tc.want {
			t.Errorf("%s: VirtualDeploymentTarget(%q) = %q, want %q", tc.name, tc.config, got, tc.want)
		}
	}
}
