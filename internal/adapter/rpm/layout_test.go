package rpm

// Table-driven coverage for the wire grammar (layout.go) and the metadata
// provider's EVR comparator (provider.go).

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
)

func TestParseRoute(t *testing.T) {
	tests := []struct {
		rel  string
		want routeKind
	}{
		{"", kindRoot},
		{"mypkg-1.0-1.noarch.rpm", kindRpm},
		{"sub/dir/mypkg-1.0-1.noarch.rpm", kindRpm},
		{"mypkg-1.0-1.noarch.rpm.sha256", kindSidecar},
		{"mypkg-1.0-1.noarch.rpm.sha1", kindSidecar},
		{"mypkg-1.0-1.noarch.rpm.md5", kindSidecar},
		{"repodata/repomd.xml", kindRepomd},
		{"repodata/repomd.xml.asc", kindRepomd},
		{"fedora/40/repodata/repomd.xml", kindRepomd}, // deep yumRootDepth form
		{"repodata/" + sha256Hex([]byte("x")) + "-primary.xml.gz", kindIndex},
		{"el/9/repodata/" + sha256Hex([]byte("x")) + "-other.xml.gz", kindIndex},
		{"repodata/old-primary.sqlite.bz2", kindIndex},
		{"repodata/comps.xml", kindGroup},
		{"repodata/" + sha256Hex([]byte("y")) + "-comps.xml", kindIndex}, // the RENAMED spelling is server-generated
		{"repodata/modules.yaml", kindRepodata},
		{"_tmp_1234567890123/repodata/x", kindTmp},
		{"_tmp_dir/anything", kindTmp},
		{"plain.txt", kindBare},
		{"sub/plain.bin", kindBare},
	}
	for _, tt := range tests {
		if got := parseRoute(tt.rel).kind; got != tt.want {
			t.Errorf("parseRoute(%q) = %d, want %d", tt.rel, got, tt.want)
		}
	}
}

func TestLayoutRejects(t *testing.T) {
	tests := []string{
		"../escape.rpm", "repo/../x.rpm", "a//b.rpm", "a/./b.rpm", "a\\b.rpm",
	}
	for _, p := range tests {
		req := &http.Request{URL: &url.URL{Path: "/" + p}}
		if _, _, err := layout(req); err == nil {
			t.Errorf("layout(%q) accepted, want rejection", p)
		}
	}
	// Reserved keys and oversized paths.
	req := &http.Request{URL: &url.URL{Path: "/api/x"}}
	if _, _, err := layout(req); err == nil {
		t.Error("reserved segment accepted")
	}
	long := "/repo/" + string(make([]byte, 600))
	req = &http.Request{URL: &url.URL{Path: long}}
	if _, _, err := layout(req); err == nil {
		t.Error("oversized path accepted")
	}
}

func TestYumRootOf(t *testing.T) {
	tests := []struct {
		path  string
		depth int
		root  string
		ok    bool
	}{
		{"a.rpm", 0, "", true},
		{"d/a.rpm", 0, "", true},
		{"d/a.rpm", 1, "d", true},
		{"a.rpm", 1, "", false},   // no directory: shallower than depth
		{"d/a.rpm", 2, "", false}, // one segment < depth 2
		{"fedora/40/x86_64/p.rpm", 3, "fedora/40/x86_64", true},
		{"fedora/40/x86_64/sub/p.rpm", 3, "fedora/40/x86_64", true},
	}
	for _, tt := range tests {
		root, ok := yumRootOf(tt.path, tt.depth)
		if root != tt.root || ok != tt.ok {
			t.Errorf("yumRootOf(%q, %d) = (%q, %v), want (%q, %v)", tt.path, tt.depth, root, ok, tt.root, tt.ok)
		}
	}
}

func TestProviderClassify(t *testing.T) {
	p := provider{}
	// The expirable family (S10): repomd and its signature pair, the
	// non-digest-prefixed repodata files, the key-class spellings.
	for _, path := range []string{
		"repodata/repomd.xml",
		"repodata/repomd.xml.asc",
		"repodata/repomd.xml.key",
		"el/9/repodata/repomd.xml",
		"repodata/comps.xml",          // the un-prefixed group spelling
		"repodata/modules.yaml",       // the modules upload spelling
		"RPM-GPG-KEY-binflow",         // extension-less gpg name
		"keys/fedora.gpg",             // key-class extension
		"some/dir/RPM-GPG-KEY-centos", // deep extension-less gpg name
	} {
		if p.Classify(path) != adapter.KindMetadata {
			t.Errorf("Classify(%q) not the expirable metadata class", path)
		}
	}
	// The artifact/content family: .rpm packages and the digest-prefixed
	// index generations (immutable by construction — a new generation
	// means a new digest name).
	digest := strings.Repeat("a", 64)
	for _, path := range []string{
		"mypkg-1.0-1.noarch.rpm",
		"Packages/m/mypkg-1.0-1.noarch.rpm",
		"repodata/" + digest + "-primary.xml.gz",
		"el/9/repodata/" + digest + "-other.xml.gz",
		"repodata/" + digest + "-modules.yaml.gz",
		"repodata/" + digest + "-comps.xml",
		"readme.txt", // a non-key bare file: content
	} {
		if p.Classify(path) != adapter.KindContent {
			t.Errorf("Classify(%q) not the artifact content class", path)
		}
	}
	// The non-digest-prefixed index spelling the old test pinned (a
	// hand-uploaded or mirrored index without the checksum prefix stays
	// expirable).
	if p.Classify("el/9/repodata/x-primary.xml.gz") != adapter.KindMetadata {
		t.Error("deep un-prefixed index not metadata class")
	}
}

func TestProviderPackageName(t *testing.T) {
	p := provider{}
	tests := []struct {
		path string
		want string
		ok   bool
	}{
		{"mypkg-1.0-1.el9.x86_64.rpm", "mypkg", true},
		{"sub/lib-fancy-2.0.1-3.fc40.aarch64.rpm", "lib-fancy", true},
		{"weird-1.0-1.rpm", "", false}, // arch without a dot
		{"nodashes.rpm", "", false},
		{"plain.txt", "", false},
	}
	for _, tt := range tests {
		got, ok := p.PackageName(tt.path)
		if got != tt.want || ok != tt.ok {
			t.Errorf("PackageName(%q) = (%q, %v), want (%q, %v)", tt.path, got, ok, tt.want, tt.ok)
		}
	}
}

func TestCompareEVR(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0-1", "1.0-1", 0},
		{"1.0-1", "1.0-2", -1},
		{"1.0-2", "1.0-1", 1},
		{"1.0-1", "1.1-1", -1},
		{"2.0-1", "1.0-1", 1},
		{"0:1.0-1", "1.0-1", 0},
		{"1:1.0-1", "2:0.1-1", -1},
		{"2:0.1-1", "1:9.9-9", 1},
		{"1.0~rc1-1", "1.0-1", -1}, // tilde sorts before
		{"1.0-1", "1.0~rc1-1", 1},
		{"1.05-1", "1.5-1", 0}, // numeric equality
		{"1.0a-1", "1.0-1", 1}, // the longer string sorts after (rpm exhaustion rule)
		{"1.a-1", "1.0-1", -1}, // numeric segment sorts after alpha
		{"", "", 0},
	}
	for _, tt := range tests {
		if got := compareEVR(tt.a, tt.b); got != tt.want {
			t.Errorf("compareEVR(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
