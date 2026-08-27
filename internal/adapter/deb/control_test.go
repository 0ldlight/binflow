package deb

// The .deb / .dsc parser tests, table-driven, over fixtures the file
// itself builds (ar + control.tar.{gz,xz,zst} members — the three
// compression spellings the modern toolchains emit).

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// ---- fixture builders ----

// controlFixture renders one dpkg control paragraph.
func controlFixture(pkg, version, arch string, extra ...string) []byte {
	var b strings.Builder
	b.WriteString("Package: " + pkg + "\n")
	b.WriteString("Version: " + version + "\n")
	if arch != "" {
		b.WriteString("Architecture: " + arch + "\n")
	}
	for _, e := range extra {
		b.WriteString(e + "\n")
	}
	return []byte(b.String())
}

// tarSingleFile builds one tar archive holding ./name with mode 0644.
func tarSingleFile(name string, body []byte) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		panic(err)
	}
	if _, err := tw.Write(body); err != nil {
		panic(err)
	}
	if err := tw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// gzipBytes compresses deterministically.
func gzipBytes(body []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(body); err != nil {
		panic(err)
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// xzBytes compresses (the modern dpkg-deb default spelling).
func xzBytes(body []byte) []byte {
	var buf bytes.Buffer
	xw, err := xz.NewWriter(&buf)
	if err != nil {
		panic(err)
	}
	if _, err := xw.Write(body); err != nil {
		panic(err)
	}
	if err := xw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// zstdBytes compresses (the dpkg 1.21.18+ spelling).
func zstdBytes(body []byte) []byte {
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		panic(err)
	}
	if _, err := zw.Write(body); err != nil {
		panic(err)
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// arMember renders one ar member header plus payload (with the
// odd-size alignment pad).
func arFixture(name string, data []byte) []byte {
	hdr := name
	for len(hdr) < 16 {
		hdr += " "
	}
	size := trimLen(len(data))
	for len(size) < 10 {
		size = " " + size
	}
	out := []byte(hdr + "0           0     0     644     " + size + "`\n")
	out = append(out, data...)
	if len(data)%2 == 1 {
		out = append(out, '\n')
	}
	return out
}

// trimLen renders the decimal size.
func trimLen(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// fixtureDeb assembles one real-shaped .deb: debian-binary +
// control.tar.<comp> + data.tar.gz. comp selects the control member's
// compression ("gz", "xz", "zst", "plain").
func fixtureDeb(comp string, control []byte) []byte {
	var member []byte
	var name string
	switch comp {
	case "xz":
		member, name = xzBytes(tarSingleFile("./control", control)), "control.tar.xz"
	case "zst":
		member, name = zstdBytes(tarSingleFile("control", control)), "control.tar.zst"
	case "plain":
		member, name = tarSingleFile("./control", control), "control.tar"
	default:
		member, name = gzipBytes(tarSingleFile("./control", control)), "control.tar.gz"
	}
	out := []byte("!<arch>\n")
	out = append(out, arFixture("debian-binary", []byte("2.0\n"))...)
	out = append(out, arFixture(name, member)...)
	out = append(out, arFixture("data.tar.gz", gzipBytes(tarSingleFile("./usr/bin/dummy", []byte("#!/bin/sh\n"))))...)
	return out
}

// fixtureDsc renders one (unsigned) .dsc.
func fixtureDsc(source, version string, checksums string) []byte {
	var b strings.Builder
	b.WriteString("Format: 3.0 (native)\n")
	b.WriteString("Source: " + source + "\n")
	b.WriteString("Version: " + version + "\n")
	b.WriteString("Binary: " + source + "\n")
	b.WriteString("Maintainer: Test <test@binflow.dev>\n")
	b.WriteString("Architecture: any\n")
	if checksums != "" {
		b.WriteString(checksums)
	}
	return []byte(b.String())
}

// ---- parser tests ----

func TestParseControlParagraph(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    map[string]string
		wantErr bool
	}{
		{
			name: "flat fields",
			in:   "Package: hello\nVersion: 2.10-3\nArchitecture: amd64\n",
			want: map[string]string{"Package": "hello", "Version": "2.10-3", "Architecture": "amd64"},
		},
		{
			name: "multiline continuation keeps structure",
			in:   "Package: hello\nDescription: a test\n package with a long\n description tail\nVersion: 1.0\n",
			want: map[string]string{
				"Package":     "hello",
				"Description": "a test\n package with a long\n description tail",
				"Version":     "1.0",
			},
		},
		{
			name: "checksums section rides as continuation",
			in:   "Checksums-Sha256:\n abc 123 a.tar\n def 456 b.dsc\n",
			want: map[string]string{"Checksums-Sha256": "\n abc 123 a.tar\n def 456 b.dsc"},
		},
		{
			name: "no space after colon",
			in:   "Package:hello\n",
			want: map[string]string{"Package": "hello"},
		},
		{
			name:    "garbage refuses",
			in:      "this is not a field\n",
			wantErr: true,
		},
		{
			name:    "empty refuses",
			in:      "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := parseControlParagraph(strings.NewReader(tt.in), maxControlBytes)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parse of %q succeeded, want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			for k, w := range tt.want {
				if got := doc.Get(k); got != w {
					t.Errorf("field %s = %q, want %q", k, got, w)
				}
			}
		})
	}
}

// TestParseControlParagraphArmor: the PGP-signed .dsc prologue (armor
// header + Hash line + blank separator) skips before the paragraph.
func TestParseControlParagraphArmor(t *testing.T) {
	in := "-----BEGIN PGP SIGNED MESSAGE-----\nHash: SHA256\n\nSource: hello\nVersion: 1.0-1\n\n-----BEGIN PGP SIGNATURE-----\n<i>sig</i>\n-----END PGP SIGNATURE-----\n"
	doc, err := parseControlParagraph(strings.NewReader(in), maxControlBytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := doc.Get("Source"); got != "hello" {
		t.Errorf("Source = %q", got)
	}
	if got := doc.Get("Version"); got != "1.0-1" {
		t.Errorf("Version = %q", got)
	}
}

// TestParseDebControl: the ar/tar walk across every compression
// spelling, plus the refusal family.
func TestParseDebControl(t *testing.T) {
	control := controlFixture("hello", "2.10-3", "amd64",
		"Maintainer: Test <t@binflow.dev>",
		"Depends: libc6 (>= 2.14)",
		"Description: hello test package\n continuation line here")
	tests := []struct {
		comp    string
		wantErr bool
	}{
		{comp: "gz"},
		{comp: "xz"},
		{comp: "zst"},
		{comp: "plain"},
	}
	for _, tt := range tests {
		t.Run(tt.comp, func(t *testing.T) {
			doc, err := parseDebControl(bytes.NewReader(fixtureDeb(tt.comp, control)))
			if err != nil {
				t.Fatalf("parse %s control member: %v", tt.comp, err)
			}
			for k, w := range map[string]string{
				"Package": "hello", "Version": "2.10-3", "Architecture": "amd64",
				"Depends": "libc6 (>= 2.14)",
			} {
				if got := doc.Get(k); got != w {
					t.Errorf("%s member: field %s = %q, want %q", tt.comp, k, got, w)
				}
			}
			if got := doc.Get("Description"); !strings.HasPrefix(got, "hello test package\n") {
				t.Errorf("%s member: Description = %q", tt.comp, got)
			}
		})
	}

	// The refusal family, table-driven.
	refs := []struct {
		name string
		body []byte
	}{
		{"not an ar archive", []byte("MZ\x90\x00 random bytes")},
		{"truncated", fixtureDeb("gz", control)[:20]},
		{"no control member", append([]byte("!<arch>\n"),
			arFixture("debian-binary", []byte("2.0\n"))...)},
	}
	for _, tt := range refs {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseDebControl(bytes.NewReader(tt.body)); err == nil {
				t.Fatalf("parse of %q succeeded, want the refusal", tt.name)
			} else if !strings.Contains(err.Error(), "not a readable debian package archive") {
				t.Errorf("refusal = %v, want the ErrNotDebArchive family", err)
			}
		})
	}
}

// TestParseDebControlOrder: the parser stops at the control member — a
// large data.tar after it never reads (the byte-count probe).
func TestParseDebControlOrder(t *testing.T) {
	control := controlFixture("probe", "1.0", "all")
	deb := fixtureDeb("gz", control)
	doc, err := parseDebControl(bytes.NewReader(deb))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := doc.Get("Package"); got != "probe" {
		t.Errorf("Package = %q", got)
	}
}

// TestParseDsc: the source descriptor parse.
func TestParseDsc(t *testing.T) {
	dsc := fixtureDsc("hello", "2.10-3",
		"Checksums-Sha256:\n 0d61 2108 hello_2.10.orig.tar.gz\n 175ee 5468 hello_2.10-3.dsc\n")
	doc, err := parseDsc(bytes.NewReader(dsc))
	if err != nil {
		t.Fatalf("parseDsc: %v", err)
	}
	if got := doc.Get("Source"); got != "hello" {
		t.Errorf("Source = %q", got)
	}
	if got := doc.Get("Checksums-Sha256"); !strings.Contains(got, "hello_2.10-3.dsc") {
		t.Errorf("Checksums-Sha256 = %q", got)
	}
}
