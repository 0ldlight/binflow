package helm

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"path"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The Chart.yaml reader (helm.md section 4.1 step 3): gzip → tar, the
// archive-ROOT Chart.yaml plus the optional requirements.yaml (the legacy
// dependencies home; Chart.yaml's inline dependencies are read too). An
// unreadable archive or a Chart.yaml without name/version is NOT an
// upload refusal — the package skips indexing (spec: 该包跳过,不拒 PUT) —
// unless the Enforce Layout policy is on (enforce.go turns the skip into
// the 403).

// maxChartYAML bounds one in-archive document (a Chart.yaml is manifest
// facts; a hostile member must fail fast instead of streaming into the
// parser).
const maxChartYAML = 1 << 20

// chartMaintainer is one Chart.yaml maintainer entry.
type chartMaintainer struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
	URL   string `yaml:"url"`
}

// chartDependency is one dependencies entry (requirements.yaml or the
// Chart.yaml inline list).
type chartDependency struct {
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
	Repository string `yaml:"repository"`
	Condition  string `yaml:"condition"`
}

// chartMeta is the typed view of Chart.yaml this package consumes; every
// other field survives verbatim through the raw-node copy index.go keeps.
type chartMeta struct {
	APIVersion   flexString        `yaml:"apiVersion"`
	Name         flexString        `yaml:"name"`
	Version      flexString        `yaml:"version"`
	AppVersion   flexString        `yaml:"appVersion"`
	Description  flexString        `yaml:"description"`
	Home         flexString        `yaml:"home"`
	Type         flexString        `yaml:"type"`
	KubeVersion  flexString        `yaml:"kubeVersion"`
	Deprecated   bool              `yaml:"deprecated"`
	Annotations  map[string]string `yaml:"annotations"`
	Keywords     []string          `yaml:"keywords"`
	Sources      []string          `yaml:"sources"`
	Maintainers  []chartMaintainer `yaml:"maintainers"`
	Dependencies []chartDependency `yaml:"dependencies"`
}

// flexString decodes any YAML scalar as its string spelling — appVersion
// and friends appear unquoted in the wild (appVersion: 1.16), and a strict
// string decode would reject a chart helm itself accepts.
type flexString string

// UnmarshalYAML implements yaml.Unmarshaler.
func (f *flexString) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("expected a scalar, got %v", value.Tag)
	}
	*f = flexString(value.Value)
	return nil
}

// String renders the flexString (the empty default).
func (f flexString) String() string { return string(f) }

// chartArchive is one parsed package: the typed fields plus the RAW
// Chart.yaml mapping node (index entries carry the full field set, unknown
// keys included — the raw copy is what keeps them).
type chartArchive struct {
	meta         chartMeta
	raw          *yaml.Node // the Chart.yaml mapping node (nil when absent)
	requirements []chartDependency
}

// parseChartArchive reads one .tgz stream.
func parseChartArchive(r io.Reader) (*chartArchive, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip header: %w", err)
	}
	defer func() { _ = gz.Close() }() //nolint:errcheck // read-only fd

	tr := tar.NewReader(gz)
	arc := &chartArchive{}
	var sawChart, sawRequirements bool
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar stream: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Clean(hdr.Name)
		dir, base := path.Split(name)
		switch {
		case base == "Chart.yaml" && rootIsArchiveRoot(dir) && !sawChart:
			raw, err := readYAMLMember(tr)
			if err != nil {
				return nil, fmt.Errorf("chart.yaml member: %w", err)
			}
			arc.raw = raw
			if err := tr2node(raw, &arc.meta); err != nil {
				return nil, fmt.Errorf("chart.yaml fields: %w", err)
			}
			sawChart = true
		case base == "requirements.yaml" && rootIsArchiveRoot(dir) && !sawRequirements:
			var req struct {
				Dependencies []chartDependency `yaml:"dependencies"`
			}
			raw, err := readYAMLMember(tr)
			if err != nil {
				return nil, fmt.Errorf("requirements.yaml member: %w", err)
			}
			if err := tr2node(raw, &req); err != nil {
				return nil, fmt.Errorf("requirements.yaml fields: %w", err)
			}
			arc.requirements = req.Dependencies
			sawRequirements = true
		}
	}
	if !sawChart {
		return nil, fmt.Errorf("no Chart.yaml at the archive root")
	}
	return arc, nil
}

// rootIsArchiveRoot reports whether dir is the archive root or a
// single-level directory ("", "mychart/", "./"). Helm packages carry
// exactly one root directory; the archive-root Chart.yaml is the one the
// spec reads.
func rootIsArchiveRoot(dir string) bool {
	return !strings.Contains(strings.TrimPrefix(path.Clean("/"+dir), "/"), "/")
}

// readYAMLMember reads one archive member bounded by maxChartYAML and
// decodes it into a raw mapping node (nil-safe for empty members).
func readYAMLMember(r io.Reader) (*yaml.Node, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxChartYAML))
	if err != nil {
		return nil, err
	}
	if len(body) >= maxChartYAML {
		return nil, fmt.Errorf("member exceeds the %d byte limit", maxChartYAML)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0], nil
	}
	return nil, fmt.Errorf("empty document")
}

// tr2node re-decodes a raw node into a typed value.
func tr2node(raw *yaml.Node, into any) error {
	if raw == nil {
		return nil
	}
	return raw.Decode(into)
}

// identity reports the (name, version) pair; ok is false when either is
// missing — the skip-indexing shape (and the Enforce Layout 403's cause).
func (a *chartArchive) identity() (name, version string, ok bool) {
	if a == nil {
		return "", "", false
	}
	name, version = a.meta.Name.String(), a.meta.Version.String()
	return name, version, name != "" && version != ""
}

// allDependencies merges the Chart.yaml inline list with requirements.yaml
// (Chart.yaml first — the modern home).
func (a *chartArchive) allDependencies() []chartDependency {
	if a == nil {
		return nil
	}
	out := a.meta.Dependencies
	out = append(out, a.requirements...)
	return out
}
