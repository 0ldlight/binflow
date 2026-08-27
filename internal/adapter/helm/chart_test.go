package helm

import (
	"bytes"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The Chart.yaml reader's table-driven tests (helm.md section 4.1 step 3).

func TestParseChartArchiveTable(t *testing.T) {
	tests := []struct {
		name      string
		chartYAML string
		extra     map[string]string
		wantName  string
		wantVer   string
		wantErr   bool
	}{
		{
			name:      "standard v2 chart",
			chartYAML: defaultChartYAML("mychart", "0.1.0"),
			wantName:  "mychart",
			wantVer:   "0.1.0",
		},
		{
			name:      "numeric appVersion tolerated",
			chartYAML: "apiVersion: v2\nname: c\nversion: 1.0.0\nappVersion: 1.16\n",
			wantName:  "c",
			wantVer:   "1.0.0",
		},
		{
			name:      "no name",
			chartYAML: "apiVersion: v2\nversion: 1.0.0\n",
			wantName:  "", // parsed, identity not ok — the skip shape
		},
		{
			name:      "no version",
			chartYAML: "apiVersion: v2\nname: c\n",
			wantName:  "c",
		},
		{
			name:    "no Chart.yaml",
			wantErr: true,
			extra:   map[string]string{"values.yaml": "foo: bar\n"},
		},
		{
			name:      "requirements dependencies merge",
			chartYAML: "apiVersion: v2\nname: c\nversion: 1.0.0\ndependencies:\n  - name: inline\n    version: 1.1.0\n",
			extra: map[string]string{
				"requirements.yaml": "dependencies:\n  - name: legacy\n    version: 0.0.9\n    repository: https://charts.example.com\n",
			},
			wantName: "c",
			wantVer:  "1.0.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fixtureChart(t, "pkg", tt.chartYAML, tt.extra)
			arc, err := parseChartArchive(bytes.NewReader(body))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parse err = nil, want failure")
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			name, ver, ok := arc.identity()
			if name != tt.wantName || (tt.wantVer != "" && ver != tt.wantVer) {
				t.Fatalf("identity = (%q,%q,%v), want (%q,%q)", name, ver, ok, tt.wantName, tt.wantVer)
			}
		})
	}
}

// TestParseChartNestedDirectoryIgnored: a Chart.yaml BELOW the archive
// root (a vendored chart under charts/) must not shadow the root one.
func TestParseChartNestedDirectoryIgnored(t *testing.T) {
	arc, err := parseChartArchive(bytes.NewReader(fixtureChart(t, "outer", defaultChartYAML("outer", "1.0.0"),
		map[string]string{"charts/inner/Chart.yaml": defaultChartYAML("inner", "0.0.1")})))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if name, _, _ := arc.identity(); name != "outer" {
		t.Fatalf("identity name = %q, want the archive root's outer", name)
	}
}

// TestParseChartDependenciesMerge: the inline and requirements lists both
// land in allDependencies.
func TestParseChartDependenciesMerge(t *testing.T) {
	arc, err := parseChartArchive(bytes.NewReader(fixtureChart(t, "pkg",
		"apiVersion: v2\nname: c\nversion: 1.0.0\ndependencies:\n  - name: inline\n    version: 1.1.0\n",
		map[string]string{"requirements.yaml": "dependencies:\n  - name: legacy\n    version: 0.0.9\n"})))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	deps := arc.allDependencies()
	if len(deps) != 2 || deps[0].Name != "inline" || deps[1].Name != "legacy" {
		t.Fatalf("allDependencies = %+v, want inline then legacy", deps)
	}
}

func TestSemverTable(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.0.0", "1.1.0", -1},
		{"1.0.0", "1.0.0-rc.1", 1}, // release outranks prerelease
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta", -1},
		{"1.0.0-alpha.beta", "1.0.0-beta", -1},
		{"1.0.0-beta.2", "1.0.0-beta.11", -1},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0+build", "1.0.0", 0}, // build metadata ignored
		{"garbage", "1.0.0", 1},     // string fallback ("g" > "1")
		{"1.0", "1.0.0", -1},        // invalid vs valid: string compare
	}
	for _, tt := range tests {
		if got := compareSemver(tt.a, tt.b); got != tt.want {
			t.Errorf("compareSemver(%q,%q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
	for _, bad := range []string{"1.0", "1.0.0.0", "01.0.0", "1.0.0-", "1.0.0+", "a.b.c", ""} {
		if _, err := parseSemver(bad); err == nil {
			t.Errorf("parseSemver(%q) accepted an invalid spelling", bad)
		}
	}
	for _, good := range []string{"0.0.0", "1.2.3", "1.0.0-alpha.1", "1.0.0+meta.data", "10.20.30-21AK"} {
		if _, err := parseSemver(good); err != nil {
			t.Errorf("parseSemver(%q) = %v, want ok", good, err)
		}
	}
}

func TestChartPropsFieldSet(t *testing.T) {
	arc, err := parseChartArchive(bytes.NewReader(fixtureChart(t, "c", defaultChartYAML("c", "0.3.0"), nil)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	props := chartProps(arc, fixedNow)
	for _, key := range []string{
		propName, propVersion, propAppVersion, propDescription, propHome,
		propCreated, propType, propAPIVersion, propSources, propMaintainers,
	} {
		if _, ok := props[key]; !ok {
			t.Errorf("chartProps missing %s: %+v", key, props)
		}
	}
	if _, ok := props[propIsDeprecated]; ok {
		t.Errorf("chartProps wrote chart.isDeprecated for a non-deprecated chart")
	}
	if props[propCreated][0] != "2026-08-27T12:00:00Z" {
		t.Errorf("chart.created = %q, want the index-time timestamp", props[propCreated][0])
	}
	// The closed property rules must accept the set (the landing consults
	// the same validator).
	if err := validateTestProps(props); err != nil {
		t.Errorf("chartProps fails the shared property rules: %v", err)
	}
}

// validateTestProps pins the property set against the shared validator
// (the metadata package's own rule — the landing path relies on it).
func validateTestProps(props map[string][]string) error {
	return metadata.ValidatePropSet(props)
}
