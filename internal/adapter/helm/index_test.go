package helm

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

// The index.yaml document model's table-driven unit tests (helm.md
// section 5.1): SemVer descending order, the digest/urls field set, the
// serialization details (version/appVersion/created quoting, no document
// marker) and the remove/prefix-remove arms.

var fixedNow = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

// buildFixtureEntry wraps buildEntry with the fixed clock.
func buildFixtureEntry(t *testing.T, chartYAML, relPath, digest string) *yaml.Node {
	t.Helper()
	arc, err := parseChartArchive(bytes.NewReader(fixtureChart(t, "c", chartYAML, nil)))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	entry, ok := buildEntry(arc, relPath, digest, fixedNow, "")
	if !ok {
		t.Fatal("buildEntry roundtrip failed")
	}
	return entry
}

func TestIndexRenderShape(t *testing.T) {
	doc := &indexDoc{entries: map[string][]*yaml.Node{}}
	doc.upsertEntry("mychart", buildFixtureEntry(t, defaultChartYAML("mychart", "0.1.0"), "mychart-0.1.0.tgz", "abc123"))
	body := string(doc.render(fixedNow))

	for _, want := range []string{
		"apiVersion: v1\n",                    // the head, no document marker
		"generated: \"2026-08-27T12:00:00Z\"", // quoted timestamp
		"entries:",                            // the entries map
		"mychart:",                            // the chart key
		"digest: abc123",                      // bare hex, no prefix
		"version: \"0.1.0\"",                  // S4: version always quoted
		"appVersion: \"1.16.0\"",              // S4: appVersion always quoted
		"created: \"2026-08-27T12:00:00Z\"",
		"description: A mychart chart for BinFlow tests",
		"name: mychart",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered index missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "serverInfo") {
		t.Errorf("rendered index carries serverInfo (the spec's absent field):\n%s", body)
	}
	if strings.Contains(body, digestPrefixSha256Colon) {
		t.Errorf("digest carries an algorithm prefix:\n%s", body)
	}
	// urls: the RELATIVE form (HL-2) — the entry cites the in-repo path
	// (the fixture's own home/sources fields do carry https URLs; the
	// assertion below reads the re-parsed urls, not a naive body scan).
	if !strings.Contains(body, "- mychart-0.1.0.tgz") {
		t.Errorf("rendered index missing the relative url:\n%s", body)
	}
	// helm must be able to re-parse the whole document.
	var back struct {
		APIVersion string `yaml:"apiVersion"`
		Entries    map[string][]struct {
			Name    string   `yaml:"name"`
			Version string   `yaml:"version"`
			Digest  string   `yaml:"digest"`
			URLs    []string `yaml:"urls"`
		} `yaml:"entries"`
	}
	if err := yaml.Unmarshal([]byte(body), &back); err != nil {
		t.Fatalf("helm-side re-parse: %v\n%s", err, body)
	}
	if back.APIVersion != "v1" || len(back.Entries["mychart"]) != 1 {
		t.Fatalf("re-parsed index wrong: %+v", back)
	}
	e := back.Entries["mychart"][0]
	if e.Version != "0.1.0" || e.Digest != "abc123" || len(e.URLs) != 1 || e.URLs[0] != "mychart-0.1.0.tgz" {
		t.Fatalf("re-parsed entry wrong: %+v", e)
	}
	if strings.HasPrefix(e.URLs[0], "http://") || strings.HasPrefix(e.URLs[0], "https://") {
		t.Fatalf("relative mode rendered an absolute url: %q", e.URLs[0])
	}
}

const digestPrefixSha256Colon = "digest: sha256:"

func TestIndexSemVerDescending(t *testing.T) {
	doc := &indexDoc{entries: map[string][]*yaml.Node{}}
	// Uploaded in scrambled order, including a prerelease and an invalid
	// spelling (the string-fallback arm).
	for _, v := range []string{"1.0.0", "0.2.1", "1.1.0-alpha.1", "0.9.0", "not.semver"} {
		doc.upsertEntry("c", buildFixtureEntry(t, defaultChartYAML("c", v), "c-"+v+".tgz", "d"+v))
	}
	got := make([]string, 0, 5)
	for _, e := range doc.entries["c"] {
		got = append(got, entryVersion(e))
	}
	// "not.semver" is unparsable: the string-descending fallback ranks it
	// above the digit-leading spellings ("n" > "1") — the spec's own rule.
	want := []string{"not.semver", "1.1.0-alpha.1", "1.0.0", "0.9.0", "0.2.1"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry order = %v, want %v (SemVer descending, string-descending fallback)", got, want)
		}
	}
}

func TestIndexUpsertOverwritesSameVersion(t *testing.T) {
	doc := &indexDoc{entries: map[string][]*yaml.Node{}}
	doc.upsertEntry("c", buildFixtureEntry(t, defaultChartYAML("c", "0.1.0"), "c-0.1.0.tgz", "old"))
	// Re-upload at a DIFFERENT path (the duplicate-tolerance shape): the
	// same name+version replaces, one entry remains (S1 remove+add).
	doc.upsertEntry("c", buildFixtureEntry(t, defaultChartYAML("c", "0.1.0"), "copy/c-0.1.0.tgz", "new"))
	if len(doc.entries["c"]) != 1 {
		t.Fatalf("entries = %d, want 1 (same name+version replaces)", len(doc.entries["c"]))
	}
	if entryURL(doc.entries["c"][0]) != "copy/c-0.1.0.tgz" {
		t.Fatalf("surviving entry url = %q, want the latest upload's", entryURL(doc.entries["c"][0]))
	}
}

func TestIndexRemoveAndPrefix(t *testing.T) {
	doc := &indexDoc{entries: map[string][]*yaml.Node{}}
	for _, spec := range []struct{ name, ver, path string }{
		{"a", "1.0.0", "a-1.0.0.tgz"},
		{"a", "0.1.0", "sub/a-0.1.0.tgz"},
		{"b", "2.0.0", "sub/b-2.0.0.tgz"},
	} {
		doc.upsertEntry(spec.name, buildFixtureEntry(t, defaultChartYAML(spec.name, spec.ver), spec.path, "d"))
	}
	doc.removeEntry("a", "1.0.0")
	if len(doc.entries["a"]) != 1 || entryVersion(doc.entries["a"][0]) != "0.1.0" {
		t.Fatalf("removeEntry left the wrong slice: %+v", doc.entries["a"])
	}
	removed := doc.removePrefix("sub")
	if removed != 2 {
		t.Fatalf("removePrefix removed %d, want 2 (urls-prefix match)", removed)
	}
	if len(doc.entries) != 0 {
		t.Fatalf("entries after prefix removal = %v", doc.entries)
	}
}

func TestParseIndexToleratesGarbage(t *testing.T) {
	doc := parseIndex([]byte("::: not yaml ["))
	if doc == nil || doc.entries == nil || len(doc.entries) != 0 {
		t.Fatalf("garbage index = %+v, want the empty doc", doc)
	}
	doc = parseIndex([]byte("apiVersion: v1\nentries:\n  c:\n  - name: c\n    version: \"1.0.0\"\n"))
	if len(doc.entries["c"]) != 1 {
		t.Fatalf("parsed entries = %+v", doc.entries)
	}
}
