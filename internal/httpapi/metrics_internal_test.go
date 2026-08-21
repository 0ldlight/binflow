// Path-normalization table (internal test: normalizeMetricsPath is
// unexported). The cardinality contract (FR-61-AC2) is exercised end to end
// in metrics_test.go's TestMetricsPathCardinality.

package httpapi

import "testing"

// TestMetricsNormalizePath pins every route family's template.
func TestMetricsNormalizePath(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		// Probes and the scrape itself keep their exact spelling.
		{"/healthz", "/healthz"},
		{"/readyz", "/readyz"},
		{"/metrics", "/metrics"},
		// The /v2 registry plane collapses wholesale (repo/image digests
		// are the highest-cardinality surface BinFlow serves).
		{"/v2", "/v2"},
		{"/v2/", "/v2"},
		{"/v2/myrepo/manifests/sha256:abcd", "/v2"},
		// Console/docs-owned segments.
		{"/binflow", "/binflow"},
		{"/binflow/", "/binflow"},
		{"/binflow/ui/", "/binflow/ui"},
		{"/binflow/ui/artifacts", "/binflow/ui"},
		{"/binflow/assets/main-abc123.js", "/binflow/assets"},
		{"/binflow/docs/guides/oidc-config", "/binflow/docs"},
		// Content plane: one template for every repository path.
		{"/binflow/libs-release/a/b/c.jar", "/binflow/:repo/:path"},
		// API families with variable tails.
		{"/binflow/api", "/binflow/api"},
		{"/binflow/api/storage", "/binflow/api/storage"},
		{"/binflow/api/storage/libs/a/b.tar.gz", "/binflow/api/storage/:repo/:path"},
		{"/binflow/api/repositories", "/binflow/api/repositories"},
		{"/binflow/api/repositories/libs-release", "/binflow/api/repositories/:key"},
		{"/binflow/api/security/users", "/binflow/api/security/users"},
		{"/binflow/api/security/users/alice", "/binflow/api/security/users/:name"},
		{"/binflow/api/security/groups/devs", "/binflow/api/security/groups/:name"},
		{"/binflow/api/security/token", "/binflow/api/security/token"},
		{"/binflow/api/v1/replications", "/binflow/api/v1/replications"},
		{"/binflow/api/v1/replications/push-a", "/binflow/api/v1/replications/:name"},
		{"/binflow/api/v1/storage/usage/libs", "/binflow/api/v1/storage/usage/:repo"},
		{"/binflow/api/v1/system/gc", "/binflow/api/v1/system/gc"},
		{"/binflow/api/search/artifact", "/binflow/api/search/artifact"},
		{"/binflow/api/npm/reg/@scope/pkg", "/binflow/api/npm/:repo/:path"},
		{"/binflow/api/pypi/reg/simple/x/", "/binflow/api/pypi/:repo/:path"},
		// Unknown deep families collapse to a two-segment literal + :rest.
		{"/binflow/api/v1/unknown/deep/tail", "/binflow/api/v1/unknown/:rest"},
		// Outside the product prefix: probes family, passed through.
		{"/stray", "/stray"},
	}
	for _, tc := range cases {
		if got := normalizeMetricsPath(tc.raw); got != tc.want {
			t.Errorf("normalizeMetricsPath(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
