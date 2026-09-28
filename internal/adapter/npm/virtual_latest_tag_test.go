package npm

// T-548 (L031): the virtual merge's dist-tags.latest is CONDITIONAL —
// maven-npm-pypi.md section 2.6 「被排除模式过滤后最新版本与 `latest`
// 标签重算」 read against the L031 live evidence (r2≡r3): with NO exclusion
// pattern in effect the reference (Artifactory 7.161.26) keeps the base
// member's latest even when it points below the union's greatest; the
// recompute fires only when the tag's target is gone from the merged
// version set.

import (
	"net/http"
	"testing"
)

// memberDoc builds one merge input from a literal document (docOf is the
// package's shared decoder helper).
func memberDoc(t *testing.T, raw string) memberPackument {
	t.Helper()
	return memberPackument{doc: docOf(t, raw)}
}

// TestMergeLatestTagConditional is the preserve/recompute table: the base
// member's latest passes through untouched while its target survives the
// merged version set (the preserve arm — no exclusion pattern in effect,
// L031 Case 1); the recompute arm fires only when the target is gone from
// the version set, repointing at the greatest visible version.
func TestMergeLatestTagConditional(t *testing.T) {
	cases := []struct {
		name       string
		base       string
		later      string // "" = single-member merge
		wantLatest string // "" = the latest key must be absent
	}{
		{
			name: "preserve: base latest below the union's greatest",
			base: `{"name":"p","dist-tags":{"latest":"1.0.0","stable":"1.0.0"},` +
				`"versions":{"1.0.0":{"version":"1.0.0"}}}`,
			later: `{"name":"p","dist-tags":{"latest":"2.0.0"},` +
				`"versions":{"2.0.0":{"version":"2.0.0"}}}`,
			wantLatest: "1.0.0",
		},
		{
			name: "preserve: single member, latest below its own greatest",
			base: `{"name":"p","dist-tags":{"latest":"1.0.0"},` +
				`"versions":{"1.0.0":{"version":"1.0.0"},"2.0.0":{"version":"2.0.0"}}}`,
			wantLatest: "1.0.0",
		},
		{
			name: "preserve: a later member's different latest loses the first-wins union",
			base: `{"name":"p","dist-tags":{"latest":"1.0.0"},` +
				`"versions":{"1.0.0":{"version":"1.0.0"}}}`,
			later: `{"name":"p","dist-tags":{"latest":"9.9.9"},` +
				`"versions":{"9.9.9":{"version":"9.9.9"}}}`,
			wantLatest: "1.0.0",
		},
		{
			name: "recompute: the exclusion filter removed latest's target",
			// The shape an exclusion pattern leaves behind: the tag still
			// names 3.0.0 but the version set holds only the visible 1.0.0
			// and 2.0.0 — the recompute repoints at the greatest visible.
			base: `{"name":"p","dist-tags":{"latest":"3.0.0"},` +
				`"versions":{"1.0.0":{"version":"1.0.0"},"2.0.0":{"version":"2.0.0"}}}`,
			wantLatest: "2.0.0",
		},
		{
			name: "recompute: dangling target with a later member's versions visible",
			base: `{"name":"p","dist-tags":{"latest":"0.9.0"},` +
				`"versions":{"1.0.0":{"version":"1.0.0"}}}`,
			later: `{"name":"p","dist-tags":{},` +
				`"versions":{"2.0.0":{"version":"2.0.0"}}}`,
			wantLatest: "2.0.0",
		},
		{
			name: "absent latest stays absent at the merge",
			// The read-time crown (crownLatest) owns the absent-latest
			// projection on every read face; the merge must not pre-crown.
			base: `{"name":"p","dist-tags":{},` +
				`"versions":{"1.0.0":{"version":"1.0.0"}}}`,
			wantLatest: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			members := []memberPackument{memberDoc(t, tc.base)}
			if tc.later != "" {
				members = append(members, memberDoc(t, tc.later))
			}
			merged := mergeVirtualPackuments(members)
			tags := distTagsOf(merged)
			if tc.wantLatest == "" {
				if _, ok := mapOf(merged["dist-tags"])["latest"]; ok {
					t.Fatalf("merged latest = %q, want the key absent (the crown owns it)", tags["latest"])
				}
				return
			}
			if tags["latest"] != tc.wantLatest {
				t.Errorf("merged latest = %q, want %q (tags %v)", tags["latest"], tc.wantLatest, tags)
			}
		})
	}
}

// TestVirtualLatestTagPassthroughThroughStack pins the L031 wire shape on
// the real stack: the base member (npmv-a, latest=1.0.0 from its own
// publish) leads the merge, the union's greatest is 3.0.0 — BOTH read
// faces of the virtual must answer latest=1.0.0, the base member's
// original value, not the union's greatest.
func TestVirtualLatestTagPassthroughThroughStack(t *testing.T) {
	f := newNPMVirtualFixture(t, `{"repositories":["npmv-a","npmv-b","npmv-rem"]}`)
	f.publish(t, "npmv-a", "demo-pkg", "1.0.0", "A-TARBALL", "")
	f.publish(t, "npmv-b", "demo-pkg", "2.0.0", "B2-TARBALL", "")
	f.publish(t, "npmv-b", "demo-pkg", "3.0.0", "B3-TARBALL", "")

	// The packument face.
	code, doc := f.packument(t, "npmv-virt", "demo-pkg")
	if code != http.StatusOK {
		t.Fatalf("virtual packument = %d", code)
	}
	versions := versionsOf(doc)
	for _, v := range []string{"1.0.0", "2.0.0", "3.0.0"} {
		if versions[v] == nil {
			t.Errorf("merged versions missing %s: %v", v, versions)
		}
	}
	if got := distTagsOf(doc)["latest"]; got != "1.0.0" {
		t.Errorf("packument latest = %q, want 1.0.0 (the base member's original, below the union's 3.0.0)", got)
	}

	// The dist-tags collection face must agree (both read faces share the
	// merged document); its body is the FLAT tag map.
	rr := f.s.call(http.MethodGet, "/npmv-virt/-/package/demo-pkg/dist-tags", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("virtual dist-tags = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if got := stringOf(docOf(t, bodyOf(rr))["latest"]); got != "1.0.0" {
		t.Errorf("dist-tags face latest = %q, want 1.0.0 (both faces one document)", got)
	}
}
