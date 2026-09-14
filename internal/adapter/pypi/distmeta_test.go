package pypi

// L017-1 (D1/D7): unit coverage of the distribution-metadata extraction —
// the wheel METADATA / sdist PKG-INFO parses that feed the simple index's
// data-requires-python attribute and the bad-metadata admission verdict.

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

// testWheelBytes builds a real wheel zip: {distInfo}.dist-info/METADATA
// (carrying requiresPython when non-empty) plus the WHEEL and payload
// members. withMetadata=false produces the D7 badmeta state (valid zip, no
// METADATA member). Byte-deterministic for fixed inputs, so callers can
// assert sha256s over the result.
func testWheelBytes(t *testing.T, distInfo, requiresPython string, withMetadata bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, body string) {
		t.Helper()
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	meta := "Metadata-Version: 2.1\nName: x\nVersion: 1.0.0\n"
	if requiresPython != "" {
		meta += "Requires-Python: " + requiresPython + "\n"
	}
	meta += "\nbody after the header block\n"
	if withMetadata {
		write(distInfo+".dist-info/METADATA", meta)
	}
	write(distInfo+".dist-info/WHEEL", "Wheel-Version: 1.0\n")
	write("pkg/__init__.py", "")
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// testSdistBytes builds a real sdist tar.gz: {stem}/PKG-INFO (carrying
// requiresPython when non-empty), {stem}/{pkgdir}/__init__.py and
// {stem}/setup.py — the L016 fixture shape (PKG-INFO one directory deep).
// withPkgInfo=false produces a metadata-less sdist (the un-evidenced state
// that must KEEP its index entry).
func testSdistBytes(t *testing.T, stem, pkgdir, requiresPython string, withPkgInfo bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(name, body string) {
		t.Helper()
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatalf("tar header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("tar write %s: %v", name, err)
		}
	}
	meta := "Metadata-Version: 2.1\nName: x\nVersion: 1.0.0\n"
	if requiresPython != "" {
		meta += "Requires-Python: " + requiresPython + "\n"
	}
	if withPkgInfo {
		add(stem+"/PKG-INFO", meta)
	}
	add(stem+"/"+pkgdir+"/__init__.py", "")
	add(stem+"/setup.py", "from setuptools import setup\nsetup(name='x', version='1.0.0')\n")
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func TestWheelFacts(t *testing.T) {
	cases := []struct {
		name string
		zip  []byte
		want distMetaFacts
	}{
		{"metadata with requires-python", testWheelBytes(t, "demo-1.0.0", ">=3.8", true),
			distMetaFacts{requiresPython: ">=3.8", indexable: true}},
		{"metadata without requires-python", testWheelBytes(t, "demo-1.0.0", "", true),
			distMetaFacts{indexable: true}},
		{"no METADATA member (badmeta)", testWheelBytes(t, "demo-1.0.0", ">=3.8", false),
			distMetaFacts{indexable: false}},
		{"garbage bytes (not a zip)", []byte("definitely not a zip archive"),
			distMetaFacts{indexable: false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc := io.NopCloser(bytes.NewReader(tc.zip))
			got := wheelFacts(rc, int64(len(tc.zip)))
			if got != tc.want {
				t.Fatalf("wheelFacts = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSdistFacts(t *testing.T) {
	cases := []struct {
		name  string
		gz    []byte
		want  string
		wantI bool
	}{
		{"PKG-INFO one directory deep with value",
			testSdistBytes(t, "demo-1.0.0", "demo", ">=3.9", true), ">=3.9", true},
		{"no PKG-INFO stays indexable",
			testSdistBytes(t, "demo-1.0.0", "demo", ">=3.9", false), "", true},
		{"garbage gzip stream stays indexable", []byte("not a gzip stream"), "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sdistFacts(io.NopCloser(bytes.NewReader(tc.gz)))
			if got.requiresPython != tc.want || got.indexable != tc.wantI {
				t.Fatalf("sdistFacts = %+v, want {%q %v}", got, tc.want, tc.wantI)
			}
		})
	}
}

func TestScanRequiresPython(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"simple", "Metadata-Version: 2.1\nRequires-Python: >=3.8\nName: x\n", ">=3.8"},
		{"case-insensitive name", "REQUIRES-PYTHON: >=3.8\n", ">=3.8"},
		{"surrounding whitespace", "Requires-Python:   >=3.8  \n", ">=3.8"},
		{"folded continuation", "Requires-Python: >=3.8,\n <4.0\nName: x\n", ">=3.8, <4.0"},
		{"marker spec form", "Requires-Python: ~=3.10\n", "~=3.10"},
		{"absent", "Metadata-Version: 2.1\nName: x\n", ""},
		{"only first occurrence", "Requires-Python: >=3.8\nRequires-Python: >=2.7\n", ">=3.8"},
		{"header block ends at blank line", "Name: x\n\nRequires-Python: >=3.8\n", ""},
		{"not a header line", "Requires-Python >=3.8\n", ""},
		{"continuation of a different header", "Summary: one\n folded\nRequires-Python: >=3.8\n", ">=3.8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scanRequiresPython(strings.NewReader(tc.in)); got != tc.want {
				t.Fatalf("scanRequiresPython(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
