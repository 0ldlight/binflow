package helm

// Table-driven coverage of the S8 rewriting algebra and its helpers
// (helm.md section 7.3 / S10): every branch of rewriteVirtualURL, the
// charts-base boundary rule, the fold/parse round trip and the Ant-style
// allow-list matcher.

import (
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

func TestRewriteVirtualURL(t *testing.T) {
	const (
		base     = "https://binflow.example.com"
		chartsUp = "https://charts.example.com/stable"
	)
	remoteMember := memberCtx{key: "helm-r", typ: repo.TypeRemote, chartsBase: chartsUp}
	localMember := memberCtx{key: "helm-l", typ: repo.TypeLocal}
	cases := []struct {
		name string
		raw  string
		mc   memberCtx
		want string
	}{
		// Remote member, charts-base aligned: the member-relative path.
		{"remote aligned", chartsUp + "/acs-2.1.1.tgz", remoteMember, "acs-2.1.1.tgz"},
		{"remote aligned nested", chartsUp + "/sub/acs-2.1.1.tgz", remoteMember, "sub/acs-2.1.1.tgz"},
		// The base must end on a path boundary: /stable2 is a different base.
		{"remote base boundary", "https://charts.example.com/stable2/x-1.0.0.tgz", remoteMember,
			"_external/https/charts.example.com/stable2/x-1.0.0.tgz"},
		// The base itself carries no path to serve.
		{"remote base itself", chartsUp, remoteMember, chartsUp},
		// An entry under the upstream's OWN _external face is transitive.
		{"remote upstream external", chartsUp + "/_external/https/github.com/org/x-1.0.0.tgz", remoteMember,
			"_transitive/https/github.com/org/x-1.0.0.tgz"},
		// Outside the base with the default allow list: the folded proxy.
		{"external allow", "https://github.com/org/dep-1.0.0.tgz", remoteMember,
			"_external/https/github.com/org/dep-1.0.0.tgz"},
		{"external allow http", "http://mirror.example.net/dep-1.0.0.tgz", remoteMember,
			"_external/http/mirror.example.net/dep-1.0.0.tgz"},
		// The allow-list miss keeps the original URL.
		{"external deny", "https://github.com/org/dep-1.0.0.tgz",
			memberCtx{key: "helm-r", typ: repo.TypeRemote, chartsBase: chartsUp + "/x"}, ""},
		// Non-http(s) schemes never fold (the client goes direct).
		{"ftp stays", "ftp://files.example.com/x-1.0.0.tgz", remoteMember, "ftp://files.example.com/x-1.0.0.tgz"},
		// oci:// entries stay verbatim.
		{"oci stays", "oci://registry.example.com/charts/x", remoteMember, "oci://registry.example.com/charts/x"},
		// Relative URLs are the member-relative path on both classes.
		{"remote relative", "rel-1.0.0.tgz", remoteMember, "rel-1.0.0.tgz"},
		{"local relative", "mychart-0.1.0.tgz", localMember, "mychart-0.1.0.tgz"},
		// A local member's own content-plane URL collapses back to the path.
		{"local own plane", base + "/binflow/helm-l/mychart-0.1.0.tgz", localMember, "mychart-0.1.0.tgz"},
		// A local member's foreign absolute URL runs the external branch.
		{"local foreign absolute", "https://elsewhere.example.org/x-1.0.0.tgz", localMember,
			"_external/https/elsewhere.example.org/x-1.0.0.tgz"},
		// A remote member with an unknown base (no config row) treats its
		// absolute URLs through the external branch.
		{"remote unknown base", "https://charts.example.com/stable/acs-2.1.1.tgz",
			memberCtx{key: "helm-r", typ: repo.TypeRemote}, "_external/https/charts.example.com/stable/acs-2.1.1.tgz"},
		{"empty", "", remoteMember, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			patterns := []string{"**"}
			want := tc.want
			if tc.name == "external deny" {
				patterns = []string{"https://internal.example.com/**"}
				want = tc.raw
			}
			if got := rewriteVirtualURL(tc.raw, tc.mc, base, patterns); got != want {
				t.Errorf("rewriteVirtualURL(%q) = %q, want %q", tc.raw, got, want)
			}
		})
	}
}

func TestParseExternalPathRoundTrip(t *testing.T) {
	// fold → parse round trips the full URL tail (query strings included).
	for _, raw := range []string{
		"https://github.com/org/dep-1.0.0.tgz",
		"http://mirror.example.net:8080/base/dep-1.0.0.tgz?token=abc",
	} {
		folded, ok := foldExternalURL(raw)
		if !ok {
			t.Fatalf("foldExternalURL(%q) not ok", raw)
		}
		back, err := parseExternalPath(folded, segExternal)
		if err != nil {
			t.Fatalf("parseExternalPath(%q): %v", folded, err)
		}
		if back != raw {
			t.Errorf("round trip: %q -> %q -> %q", raw, folded, back)
		}
	}
}

func TestParseExternalPathRefusals(t *testing.T) {
	cases := []struct{ prefix, rel string }{
		{segExternal, "_external"},
		{segExternal, "_external/"},
		{segExternal, "_external/ftp/example.com/x.tgz"},
		{segExternal, "_external/https"},
		{segExternal, "_external/https/"},
		{segExternal, "_external/https/user@example.com/x.tgz"},
		{segTransitive, "_transitive/gopher/example.com/x"},
	}
	for _, tc := range cases {
		if _, err := parseExternalPath(tc.rel, tc.prefix); err == nil {
			t.Errorf("parseExternalPath(%q, %s) accepted a malformed path", tc.rel, tc.prefix)
		}
	}
	// The wrong prefix for the spelling is a refusal too.
	if _, err := parseExternalPath("_external/https/example.com/x", segTransitive); err == nil {
		t.Errorf("parseExternalPath under the wrong prefix accepted")
	}
}

func TestAntMatch(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"**", "anything at all", true},
		{"**", "https://github.com/org/x.tgz", true},
		{"https://github.com/**", "https://github.com/org/dep-1.0.0.tgz", true},
		{"https://github.com/**", "https://gitlab.com/org/dep-1.0.0.tgz", false},
		{"https://*.example.com/**", "https://mirror.example.com/deps/x.tgz", true},
		{"https://*.example.com/**", "https://mirror.example.org/deps/x.tgz", false},
		{"**/dep-*.tgz", "https://github.com/org/dep-1.0.0.tgz", true},
		{"**/dep-?.tgz", "https://github.com/org/dep-1.tgz", true},
		{"**/dep-?.tgz", "https://github.com/org/dep-1.0.tgz", false},
		{"https://charts.example.com/*", "https://charts.example.com/x.tgz", true},
		{"https://charts.example.com/*", "https://charts.example.com/sub/x.tgz", false},
	}
	for _, tc := range cases {
		if got := antMatch(tc.pattern, tc.s); got != tc.want {
			t.Errorf("antMatch(%q, %q) = %v, want %v", tc.pattern, tc.s, got, tc.want)
		}
	}
}

func TestExternalAllowed(t *testing.T) {
	if !externalAllowed(externalPatterns(nil), "https://anything.example/x") {
		t.Errorf("the nil (default **) list must admit everything")
	}
	narrow := []string{"https://github.com/**", "https://mirrors.example.com/**"}
	for _, admit := range []string{"https://github.com/org/x.tgz", "https://mirrors.example.com/y.tgz"} {
		if !externalAllowed(narrow, admit) {
			t.Errorf("narrow list refused %q", admit)
		}
	}
	if externalAllowed(narrow, "https://gitlab.com/org/x.tgz") {
		t.Errorf("narrow list admitted an off-list URL")
	}
}
