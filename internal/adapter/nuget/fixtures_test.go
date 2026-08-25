package nuget

import (
	"archive/zip"
	"bytes"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"testing"
)

// Real .nupkg fixtures: a minimal but structurally true package (the zip
// carries the nuspec at the root, named after the package id — the
// official layout the push validation walks).

// nupkgFixture is one built package.
type nupkgFixture struct {
	body   []byte
	sha512 string // base64, the official sidecar spelling
	nuspec string // the embedded nuspec bytes
}

// buildNupkg builds one package: id/version drive both the nuspec and the
// file name, deps renders one dependency group.
func buildNupkg(t *testing.T, id, version string, deps string) *nupkgFixture {
	t.Helper()
	nuspec := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2013/05/nuspec.xsd">
  <metadata>
    <id>%s</id>
    <version>%s</version>
    <title>%s demo package</title>
    <authors>BinFlow Test</authors>
    <owners>binflow</owners>
    <description>A fixture package for the nuget adapter matrix.</description>
    <language>en-US</language>
    <projectUrl>https://binflow.example/%s</projectUrl>
    <tags>fixture binflow</tags>
    <requireLicenseAcceptance>false</requireLicenseAcceptance>
%s
  </metadata>
</package>
`, id, version, id, id, deps)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(id + ".nuspec")
	if err != nil {
		t.Fatalf("zip create nuspec: %v", err)
	}
	if _, err := w.Write([]byte(nuspec)); err != nil {
		t.Fatalf("zip write nuspec: %v", err)
	}
	// One payload file — a package with only a manifest is structurally
	// degenerate even though the spec allows it.
	pw, err := zw.Create("tools/hello.txt")
	if err != nil {
		t.Fatalf("zip create payload: %v", err)
	}
	if _, err := pw.Write([]byte("hello from " + id + " " + version + "\n")); err != nil {
		t.Fatalf("zip write payload: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	body := buf.Bytes()
	s512 := sha512.Sum512(body)
	return &nupkgFixture{
		body:   body,
		sha512: base64.StdEncoding.EncodeToString(s512[:]),
		nuspec: nuspec,
	}
}

// flatDeps renders one netstandard2.0 dependency group with the given
// id:range pairs ("none" renders no dependencies element).
func flatDeps(pairs ...string) string {
	if len(pairs) == 1 && pairs[0] == "none" {
		return ""
	}
	out := "    <dependencies>\n      <group targetFramework=\"netstandard2.0\">\n"
	for i := 0; i+1 < len(pairs); i += 2 {
		out += fmt.Sprintf("        <dependency id=\"%s\" version=\"%s\" />\n", pairs[i], pairs[i+1])
	}
	out += "      </group>\n    </dependencies>"
	return out
}

// pushPath renders the v3 push target.
func pushPath(repo, id, version string) string {
	return apiPath(repo) + "/" + segFlat + "/" + id + "/" + version
}

// packagePath renders the v3 package-file target.
func packagePath(repo, id, version, file string) string {
	name := id + "." + version
	switch file {
	case "nupkg":
		name += suffixNupkg
	case "sha512":
		name += suffixSha512
	case "nuspec":
		name += suffixNuspec
	}
	return apiPath(repo) + "/" + segFlat + "/" + id + "/" + version + "/" + name
}
