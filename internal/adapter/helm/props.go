package helm

import (
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The chart.* node properties (helm.md section 4.2 — the S2 set). List
// facts ride as native multi-values; every value must satisfy the shared
// property rules (non-empty, <= 1KiB, no control bytes), so over-long or
// empty facts are dropped rather than failing the landing — the property
// plane is an index of the chart, never its gate.

const (
	propName         = "chart.name"
	propVersion      = "chart.version"
	propAppVersion   = "chart.appVersion"
	propDescription  = "chart.description"
	propHome         = "chart.home"
	propCreated      = "chart.created"
	propType         = "chart.type"
	propAPIVersion   = "chart.apiVersion"
	propAnnotations  = "chart.annotations"
	propIsDeprecated = "chart.isDeprecated"
	propSources      = "chart.sources"
	propMaintainers  = "chart.maintainers"
	propDependencies = "chart.dependencies"
)

// chartProps renders the .tgz node's protocol properties. created is the
// INDEX time (not Chart.yaml's own created field — the spec's call).
func chartProps(arc *chartArchive, now time.Time) map[string][]string {
	if arc == nil {
		return nil
	}
	m := arc.meta
	props := map[string][]string{}
	set := func(key, value string) {
		if value != "" {
			props[key] = []string{value}
		}
	}
	set(propName, m.Name.String())
	set(propVersion, m.Version.String())
	set(propAppVersion, m.AppVersion.String())
	set(propDescription, m.Description.String())
	set(propHome, m.Home.String())
	set(propCreated, now.UTC().Format(time.RFC3339))
	set(propType, m.Type.String())
	set(propAPIVersion, m.APIVersion.String())
	if m.Deprecated {
		set(propIsDeprecated, "true")
	}
	if n := len(m.Annotations); n > 0 {
		pairs := make([]string, 0, n)
		for k, v := range m.Annotations {
			pairs = append(pairs, k+"="+v)
		}
		sortStrings(pairs)
		set(propAnnotations, strings.Join(pairs, ";"))
	}
	if vs := filterPropValues(m.Sources); len(vs) > 0 {
		props[propSources] = vs
	}
	if ms := maintainerValues(m.Maintainers); len(ms) > 0 {
		props[propMaintainers] = ms
	}
	if ds := dependencyValues(arc.allDependencies()); len(ds) > 0 {
		props[propDependencies] = ds
	}
	return props
}

// maintainerValues renders maintainers as "name <email>" / "name (url)".
func maintainerValues(ms []chartMaintainer) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		switch {
		case m.Name == "":
			continue
		case m.Email != "":
			out = append(out, m.Name+" <"+m.Email+">")
		case m.URL != "":
			out = append(out, m.Name+" ("+m.URL+")")
		default:
			out = append(out, m.Name)
		}
	}
	return filterPropValues(out)
}

// dependencyValues renders one compact per-dependency spelling
// "name:version:repository" (the wire format is not spec-pinned; the
// compact form round-trips through the property plane's ';' conventions
// without ambiguity because names carry no colons).
func dependencyValues(ds []chartDependency) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		parts := []string{d.Name, d.Version}
		if d.Repository != "" {
			parts = append(parts, d.Repository)
		}
		v := strings.Join(parts, ":")
		if d.Condition != "" {
			v += " (" + d.Condition + ")"
		}
		out = append(out, v)
	}
	return filterPropValues(out)
}

// filterPropValues keeps the values the shared property rules accept.
func filterPropValues(vs []string) []string {
	out := make([]string, 0, len(vs))
	seen := make(map[string]bool, len(vs))
	for _, v := range vs {
		if v == "" || seen[v] || len(v) > metadata.MaxPropValueLen || strings.ContainsFunc(v, isPropControl) {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// isPropControl mirrors the property plane's control-byte refusal.
func isPropControl(r rune) bool { return r < 0x20 || r == 0x7f }

// sortStrings is the tiny in-place string sort (avoids importing sort for
// two call sites).
func sortStrings(vs []string) {
	for i := 1; i < len(vs); i++ {
		for j := i; j > 0 && vs[j] < vs[j-1]; j-- {
			vs[j], vs[j-1] = vs[j-1], vs[j]
		}
	}
}
