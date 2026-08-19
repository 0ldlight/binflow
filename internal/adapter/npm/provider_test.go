package npm

import (
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// TestProviderClassify: packument nodes are metadata (short remote-cache
// TTL); tarballs and everything else are content.
func TestProviderClassify(t *testing.T) {
	p := provider{}
	for _, tc := range []struct {
		path string
		want adapter.MetadataKind
	}{
		{"demo-pkg/packument.json", adapter.KindMetadata},
		{"@acme/util/packument.json", adapter.KindMetadata},
		{"demo-pkg/-/demo-pkg-1.0.0.tgz", adapter.KindContent},
		{"@acme/util/-/@acme/util-1.0.0.tgz", adapter.KindContent},
		{"some/random/path", adapter.KindContent},
		{"-/package/demo-pkg/dist-tags", adapter.KindContent},
	} {
		if got := p.Classify(tc.path); got != tc.want {
			t.Fatalf("Classify(%s) = %s, want %s", tc.path, got, tc.want)
		}
	}
}

// TestProviderPackageName: package identity of every npm layout shape;
// service routes and malformed tails report ok=false.
func TestProviderPackageName(t *testing.T) {
	p := provider{}
	for _, tc := range []struct {
		path string
		name string
		ok   bool
	}{
		{"demo-pkg", "demo-pkg", true},
		{"demo-pkg/packument.json", "demo-pkg", true},
		{"demo-pkg/-/demo-pkg-1.0.0.tgz", "demo-pkg", true},
		{"demo-pkg/-/demo-pkg-1.0.0.tgz/-rev/3", "demo-pkg", true},
		{"demo-pkg/-rev/3", "demo-pkg", true},
		{"demo-pkg/1.0.0", "", false}, // a version READ route, not storage
		{"@acme/util", "@acme/util", true},
		{"@acme/util/-/@acme/util-1.0.0.tgz", "@acme/util", true},
		{"-/package/@acme/util/dist-tags", "", false},
		{"-/ping", "", false},
		{"weird/a/b/c", "", false},
	} {
		got, ok := p.PackageName(tc.path)
		if got != tc.name || ok != tc.ok {
			t.Fatalf("PackageName(%s) = (%q,%v), want (%q,%v)", tc.path, got, ok, tc.name, tc.ok)
		}
	}
}

// TestRegisterKeysHandlerAndProvider: the single Register call keys BOTH
// registries under the same literal (T-63 review N2).
func TestRegisterKeysHandlerAndProvider(t *testing.T) {
	s := newStack(t)
	Register(s.h)

	if h, ok := adapter.ForRepoType(Protocol); !ok || h != adapter.Handler(s.h) {
		t.Fatalf("handler registry: ok=%v handler=%v", ok, h)
	}
	if p, ok := adapter.ForProtocol(Protocol); !ok {
		t.Fatalf("metadata registry: ok=%v", ok)
	} else if p.Protocol() != Protocol {
		t.Fatalf("metadata registry keyed %q", p.Protocol())
	}
	names := adapter.MetadataProtocols()
	found := false
	for _, n := range names {
		if n == Protocol {
			found = true
		}
	}
	if !found {
		t.Fatalf("MetadataProtocols = %v, missing npm", names)
	}
}
