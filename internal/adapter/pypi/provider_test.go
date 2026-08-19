package pypi

import (
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// TestProviderClassify pins the TTL split key for the remote cache (T-66)
// and the virtual aggregation (T-72): simple/ routes are regenerable
// metadata (short TTL), everything else is immutable content.
func TestProviderClassify(t *testing.T) {
	p := metadataProvider{}
	tests := []struct {
		relPath string
		want    adapter.MetadataKind
	}{
		{"simple/", adapter.KindMetadata},
		{"simple", adapter.KindMetadata},
		{"simple/demo-pkg/", adapter.KindMetadata},
		{"packages/Demo_Pkg/1.0/Demo_Pkg-1.0.whl", adapter.KindContent},
		{"Demo_Pkg/1.0/Demo_Pkg-1.0.whl", adapter.KindContent},
		{"Demo_Pkg/1.0/", adapter.KindContent},
		{"pypi/demo/json", adapter.KindContent},
		{"stray-file.bin", adapter.KindContent},
		{"", adapter.KindContent},
	}
	for _, tc := range tests {
		if got := p.Classify(tc.relPath); got != tc.want {
			t.Errorf("Classify(%q) = %q, want %q", tc.relPath, got, tc.want)
		}
	}
}

// TestProviderPackageName pins the aggregation grouping key: every legal
// spelling resolves to the PEP 503 normalized name; reserved and
// non-package shapes report ok=false.
func TestProviderPackageName(t *testing.T) {
	p := metadataProvider{}
	tests := []struct {
		relPath string
		want    string
		ok      bool
	}{
		{"Demo_Pkg/1.0/Demo_Pkg-1.0.whl", "demo-pkg", true},
		{"demo_pkg/2.0/x.tar.gz", "demo-pkg", true},
		{"packages/Demo.Pkg/1.0/x.whl", "demo-pkg", true},
		{"simple/Demo_Pkg/", "demo-pkg", true},
		{"simple/demo-pkg", "demo-pkg", true},
		// Reserved / non-package shapes.
		{"simple/demo-pkg/1.0", "", false},
		{"pypi/demo-pkg/json", "", false},
		{"", "", false},
		{"solitary", "", false},
		{"packages/", "", false},
		{"simple/", "", false},
	}
	for _, tc := range tests {
		got, ok := p.PackageName(tc.relPath)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("PackageName(%q) = (%q, %t), want (%q, %t)", tc.relPath, got, ok, tc.want, tc.ok)
		}
	}
}

// TestProviderVersionsNil pins the M3 posture: no PEP 440 comparator, the
// SPI-documented first-hit fallback for virtual resolution.
func TestProviderVersionsNil(t *testing.T) {
	if v := (metadataProvider{}).Versions(); v != nil {
		t.Fatalf("Versions() = %v, want nil in M3", v)
	}
}

// TestNormalizePackageName is the PEP 503 matrix (AC: the Demo_Pkg /
// demo_pkg / demo-pkg trio plus runs and case).
func TestNormalizePackageName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Demo_Pkg", "demo-pkg"},
		{"demo_pkg", "demo-pkg"},
		{"demo-pkg", "demo-pkg"},
		{"Demo.Pkg", "demo-pkg"},
		{"DEMO__PKG", "demo-pkg"},
		{"demo___pkg...x", "demo-pkg-x"},
		{"a", "a"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := normalizePackageName(tc.in); got != tc.want {
			t.Errorf("normalizePackageName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
