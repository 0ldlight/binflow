package webhook

import (
	"encoding/json"
	"strings"
)

// CriteriaFilter is the parsed event_filter.criteria (webhook.md 2.2: a
// domain-dependent loose object on the wire; BinFlow parses it STRICTLY —
// unknown keys answer 400, ADR-0041 decision 3's "strict 未知键拒收").
// The bool/string[] keys are the official schema's full dimension list;
// the three wired domains (artifact, artifact_property, docker) consume
// anyLocal/anyRemote/repoKeys/includePatterns/excludePatterns (webhook.md
// 2.2's criteria-by-domain table); the rest are stored and echoed for the
// dormant domains' future flips.
type CriteriaFilter struct {
	AnyLocal          bool                `json:"anyLocal"`
	AnyRemote         bool                `json:"anyRemote"`
	AnyFederated      bool                `json:"anyFederated"`
	AnyBuild          bool                `json:"anyBuild"`
	AnyReleaseBundle  bool                `json:"anyReleaseBundle"`
	RepoKeys          []string            `json:"repoKeys"`
	IncludePatterns   []string            `json:"includePatterns"`
	ExcludePatterns   []string            `json:"excludePatterns"`
	SelectedBuilds    []string            `json:"selectedBuilds"`
	RegReleaseBundles []string            `json:"registeredReleaseBundlesNames"`
	SelectedEnvirons  []string            `json:"selectedEnvironments"`
	ApplicationKeys   []string            `json:"applicationKeys"`
	Stages            []string            `json:"stages"`
	SelectedBundles   map[string][]string `json:"selectedReleaseBundles"`
}

// allowedCriteriaKeys is the strict closed set (json tag spellings).
var allowedCriteriaKeys = map[string]string{
	"anyLocal":                      "bool",
	"anyRemote":                     "bool",
	"anyFederated":                  "bool",
	"anyBuild":                      "bool",
	"anyReleaseBundle":              "bool",
	"repoKeys":                      "[]string",
	"includePatterns":               "[]string",
	"excludePatterns":               "[]string",
	"selectedBuilds":                "[]string",
	"registeredReleaseBundlesNames": "[]string",
	"selectedEnvironments":          "[]string",
	"applicationKeys":               "[]string",
	"stages":                        "[]string",
	"selectedReleaseBundles":        "map",
}

// ParseCriteria strict-parses one criteria object.
func ParseCriteria(raw json.RawMessage) (*CriteriaFilter, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, invalidf("event_filter.criteria is not a JSON object: %v", err)
	}
	for k, v := range probe {
		kind, ok := allowedCriteriaKeys[k]
		if !ok {
			return nil, invalidf("event_filter.criteria carries unknown key %q", k)
		}
		switch kind {
		case "bool":
			var b bool
			if err := json.Unmarshal(v, &b); err != nil {
				return nil, invalidf("criteria.%s must be a boolean", k)
			}
		case "[]string":
			var s []string
			if err := json.Unmarshal(v, &s); err != nil {
				return nil, invalidf("criteria.%s must be a string array", k)
			}
		case "map":
			var m map[string][]string
			if err := json.Unmarshal(v, &m); err != nil {
				return nil, invalidf("criteria.%s must be an object of string arrays", k)
			}
		}
	}
	var f CriteriaFilter
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, invalidf("event_filter.criteria does not parse: %v", err)
	}
	return &f, nil
}

// matchesRepo reports whether the criteria's repository selection admits
// an event out of repoKey (class "local"/"remote"/"virtual" — BinFlow's
// three; anyFederated never matches, BinFlow has no federated class).
// Semantics per webhook.md 2.2: repoKeys is exact membership; anyLocal /
// anyRemote are class families ("选中 any local/remote 时对未来新建仓库
// 同样生效" — class matching, not an enumeration). With NOTHING selected
// the filter admits nothing: a subscription must name its scope (the
// official UI ships anyLocal+anyRemote checked; the REST caller says so
// explicitly or lists repoKeys).
func (f *CriteriaFilter) MatchesRepo(repoKey, class string) bool {
	for _, k := range f.RepoKeys {
		if k == repoKey {
			return true
		}
	}
	if f.AnyLocal && class == "local" {
		return true
	}
	if f.AnyRemote && class == "remote" {
		return true
	}
	if f.AnyLocal && class == "virtual" {
		// A virtual write routes onto a local member before Emit fires, so
		// the events the bus sees are member-local; keep virtual admissible
		// under anyLocal for forward symmetry (remote cache events carry
		// the remote key itself).
		return true
	}
	return false
}

// matchesPath runs the include/exclude pair over the repo-relative path:
// excludes first and winning, then a non-empty include set must match
// (Ant-style two-level wildcards — '**' spans segments, '*' one segment;
// the same lineage as the governance patterns, reimplemented here because
// repo's matcher is deliberately unexported and this package must not
// import repo).
func (f *CriteriaFilter) MatchesPath(path string) bool {
	for _, p := range f.ExcludePatterns {
		if antMatch(p, path) {
			return false
		}
	}
	if len(f.IncludePatterns) == 0 {
		return true
	}
	for _, p := range f.IncludePatterns {
		if antMatch(p, path) {
			return true
		}
	}
	return false
}

// antMatch reports whether pattern matches path (Ant-style). "", "**" and
// "**/*" match everything.
func antMatch(pattern, path string) bool {
	switch pattern {
	case "", "**", "**/*":
		return true
	}
	path = strings.TrimSuffix(strings.TrimSpace(path), "/")
	if path == "" {
		return false
	}
	return antSegs(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

// antSegs matches pattern segments against path segments with
// backtracking on '**'.
func antSegs(pat, segs []string) bool {
	pi, ti := 0, 0
	for pi < len(pat) {
		switch {
		case pat[pi] == "**":
			rest := pat[pi+1:]
			for skip := ti; skip <= len(segs); skip++ {
				if antSegs(rest, segs[skip:]) {
					return true
				}
			}
			return false
		case ti >= len(segs):
			return false
		case !antSeg(pat[pi], segs[ti]):
			return false
		default:
			pi++
			ti++
		}
	}
	return ti == len(segs)
}

// antSeg matches one segment ('*' spans anything inside the segment).
func antSeg(pattern, seg string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == seg
	}
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(seg, parts[0]) {
		return false
	}
	s := seg[len(parts[0]):]
	last := parts[len(parts)-1]
	if !strings.HasSuffix(s, last) {
		return false
	}
	s = s[:len(s)-len(last)]
	for _, mid := range parts[1 : len(parts)-1] {
		idx := strings.Index(s, mid)
		if idx < 0 {
			return false
		}
		s = s[idx+len(mid):]
	}
	return true
}

// String renders the filter for logs (no secrets involved).
func (f *CriteriaFilter) String() string {
	if f == nil {
		return "{}"
	}
	b, err := json.Marshal(f)
	if err != nil {
		return "{}"
	}
	return string(b)
}
