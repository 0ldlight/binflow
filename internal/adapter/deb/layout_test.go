package deb

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestParseRoute: the wire-target grammar, table-driven over the four
// faces (index family / debPUT suffixes / dists subtree / bare) plus the
// DB-3 domain's pattern edges.
func TestParseRoute(t *testing.T) {
	tests := []struct {
		path string
		want routeKind
	}{
		{"", kindRoot},
		// The debPUT faces.
		{"pool/main/m/mypkg/mypkg_1.0_amd64.deb", kindDeb},
		{"pool/main/h/hello/hello_2.10-3.dsc", kindDsc},
		{"root.deb", kindDeb},
		// The DB-3 domain (index family under dists/).
		{"dists/stable/Release", kindIndex},
		{"dists/stable/Release.gpg", kindIndex},
		{"dists/stable/InRelease", kindIndex},
		{"dists/stable/main/binary-amd64/Packages", kindIndex},
		{"dists/stable/main/binary-amd64/Packages.gz", kindIndex},
		{"dists/stable/main/binary-i386/Packages.bz2", kindIndex},
		{"dists/stable/main/source/Sources", kindIndex},
		{"dists/stable/main/binary-amd64/by-hash/SHA256/abc", kindIndex},
		{"dists/wheezy/updates/main/binary-amd64/Packages", kindIndex},
		// Other paths under dists/ stay plain storage.
		{"dists/stable/README", kindDists},
		{"dists/stable/extra/notes.txt", kindDists},
		// Bare storage.
		{"pool/main/h/hello/hello_2.10.orig.tar.gz", kindBare},
		{"some/dir/file.bin", kindBare},
	}
	for _, tt := range tests {
		if got := parseRoute(tt.path); got.kind != tt.want {
			t.Errorf("parseRoute(%q) = %v, want %v", tt.path, got.kind, tt.want)
		}
	}
}

// TestLayoutRejects: the decode-point refusals, table-driven.
func TestLayoutRejects(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"dot-segment key escape", "/../etc/passwd"},
		{"dot segment in path", "/deb-local/a/../b.deb"},
		{"double slash", "/deb-local//x.deb"},
		{"backslash", "/deb-local/a\\b.deb"},
		{"reserved key", "/api/x.deb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{URL: &url.URL{Path: tt.raw, RawPath: tt.raw}}
			if _, _, _, err := layout(r); err == nil {
				t.Fatalf("layout(%q) accepted, want refusal", tt.raw)
			}
		})
	}
}

// TestLayoutMatrixPeel: the debPUT coordinates ride the shared peel; a
// path without the paired k=v tail keeps literal semantics.
func TestLayoutMatrixPeel(t *testing.T) {
	r := &http.Request{URL: &url.URL{
		Path: "/deb-local/pool/main/m/mypkg/mypkg_1.0_amd64.deb;deb.distribution=stable;deb.component=main;deb.architecture=amd64;deb.architecture=i386",
	}}
	key, rel, props, err := layout(r)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if key != "deb-local" || rel != "pool/main/m/mypkg/mypkg_1.0_amd64.deb" {
		t.Fatalf("layout split = (%q, %q)", key, rel)
	}
	coords := debCoordinates(props)
	if !coords.complete() {
		t.Fatalf("coordinates incomplete: %+v", coords)
	}
	if got := coords.distributions[0]; got != "stable" {
		t.Errorf("distribution = %q", got)
	}
	if len(coords.architectures) != 2 {
		t.Errorf("architectures = %v, want the repeated-key pair", coords.architectures)
	}

	// The legacy fallback: a ';' without the k=v shape stays literal.
	r2 := &http.Request{URL: &url.URL{Path: "/deb-local/file;name.deb"}}
	_, rel2, props2, err := layout(r2)
	if err != nil {
		t.Fatalf("layout legacy: %v", err)
	}
	if rel2 != "file;name.deb" || len(props2) != 0 {
		t.Fatalf("legacy path = %q, props = %v", rel2, props2)
	}
}

// TestCoordinateValidation: the coordinate token floor, table-driven.
func TestCoordinateValidation(t *testing.T) {
	tests := []struct {
		name  string
		coord coordinates
		ok    bool
	}{
		{"plain", coordinates{[]string{"stable"}, []string{"main"}, []string{"amd64"}}, true},
		{"nested suite", coordinates{[]string{"wheezy/updates"}, []string{"main"}, []string{"i386"}}, true},
		{"dotted component", coordinates{[]string{"stable"}, []string{"non-free-firmware"}, []string{"all"}}, true},
		{"plus arch", coordinates{[]string{"stable"}, []string{"main"}, []string{"kfreebsd-amd64"}}, true},
		{"empty dist", coordinates{[]string{""}, []string{"main"}, []string{"amd64"}}, false},
		{"slash in component", coordinates{[]string{"stable"}, []string{"a/b"}, []string{"amd64"}}, false},
		{"slash in arch", coordinates{[]string{"stable"}, []string{"main"}, []string{"a/b"}}, false},
		{"traversal dist", coordinates{[]string{"../.."}, []string{"main"}, []string{"amd64"}}, false},
		{"dot-segment dist", coordinates{[]string{"a/../b"}, []string{"main"}, []string{"amd64"}}, false},
		{"space dist", coordinates{[]string{"a b"}, []string{"main"}, []string{"amd64"}}, false},
		{"oversize", coordinates{[]string{strings.Repeat("a", 65)}, []string{"main"}, []string{"amd64"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.coord.validate()
			if tt.ok && err != nil {
				t.Fatalf("validate: %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("validate accepted an illegal token")
			}
		})
	}
}

// TestProviderClassify: the dists/ metadata split (the future remote
// engine's TTL seam).
func TestProviderClassify(t *testing.T) {
	var p provider
	tests := []struct {
		path string
		want bool // true = metadata
	}{
		{"dists/stable/Release", true},
		{"dists/stable/main/binary-amd64/Packages.gz", true},
		{"pool/main/m/mypkg/mypkg_1.0_amd64.deb", false},
		{"pool/main/h/hello/hello_2.10.orig.tar.gz", false},
		{"anything/else.bin", false},
	}
	for _, tt := range tests {
		got := p.Classify(tt.path)
		if tt.want && got != "metadata" {
			t.Errorf("Classify(%q) = %v, want metadata", tt.path, got)
		}
		if !tt.want && got != "content" {
			t.Errorf("Classify(%q) = %v, want content", tt.path, got)
		}
	}
}

// TestProviderPackageName: the best-effort pool filename split.
func TestProviderPackageName(t *testing.T) {
	var p provider
	tests := []struct {
		path string
		want string
		ok   bool
	}{
		{"pool/main/m/mypkg/mypkg_1.0_amd64.deb", "mypkg", true},
		{"pool/main/h/hello/hello_2.10-3.dsc", "hello", true},
		{"plain_1.0_all.deb", "plain", true},
		{"no-version.deb", "", false},
		{"pool/main/h/hello/hello_2.10.orig.tar.gz", "", false},
	}
	for _, tt := range tests {
		got, ok := p.PackageName(tt.path)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("PackageName(%q) = (%q, %v), want (%q, %v)", tt.path, got, ok, tt.want, tt.ok)
		}
	}
}

// TestCompareDpkgVersion: the policy comparator, table-driven over the
// documented ordering edges.
func TestCompareDpkgVersion(t *testing.T) {
	tests := []struct {
		a, b string
		want int // sign of compare(a,b)
	}{
		{"1.0", "1.0", 0},
		{"1.0", "2.0", -1},
		{"2.0", "1.0", 1},
		{"1.0-1", "1.0-2", -1},
		{"1:0.9", "2.0", 1},    // epoch dominates upstream
		{"1:1.0", "2.0", 1},    // epoch dominates upstream
		{"2:0.1", "1:9.9", 1},  // epochs compare numerically
		{"1.0~rc1", "1.0", -1}, // tilde pre-release sorts first
		{"1.0~~", "1.0~", -1},
		{"1.0a", "1.0", 1},    // letters extend
		{"1.0.1", "1.0", 1},   // numeric run compares numerically
		{"1.01", "1.1", 0},    // leading zeros erased
		{"1.0-1", "1.0", 1},   // revision presence sorts after
		{"1.0+a", "1.0-z", 1}, // upstream compares before the revision
		{"0.9-1", "1.0-1", -1},
	}
	for _, tt := range tests {
		got := compareDpkgVersion(tt.a, tt.b)
		if (got < 0) != (tt.want < 0) || (got > 0) != (tt.want > 0) {
			t.Errorf("compareDpkgVersion(%q,%q) = %d, want sign %d", tt.a, tt.b, got, tt.want)
		}
		if back := compareDpkgVersion(tt.b, tt.a); (back < 0) != (got > 0) || (back > 0) != (got < 0) {
			t.Errorf("comparator not antisymmetric on (%q,%q)", tt.a, tt.b)
		}
	}
}
