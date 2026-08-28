package nuget

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// The v2 OData query-parameter model (nuget.md section 4): the endpoint
// signatures (searchTerm / targetFramework(s) / includePrerelease /
// packageIds / versions / includeAllVersions / versionConstraints / id /
// semVerLevel) plus the OData generic options ($filter / $orderby / $top /
// $skip / $inlinecount / $skiptoken / $select / $expand). Parameter names
// fold case-insensitively (the CaseInsensitiveMap posture).

// v2Query is the case-folded query surface.
type v2Query struct {
	raw  url.Values
	lows map[string][]string
}

// foldV2Query lowercases every parameter name (values verbatim).
func foldV2Query(q url.Values) *v2Query {
	v := &v2Query{raw: q, lows: make(map[string][]string, len(q))}
	for k, vv := range q {
		v.lows[lowerASCII(k)] = vv
	}
	return v
}

// foldV2QueryOf folds one request's query.
func foldV2QueryOf(r *http.Request) *v2Query { return foldV2Query(r.URL.Query()) }

// get returns the first value of name (case-insensitive).
func (v *v2Query) get(name string) string {
	if vv := v.lows[lowerASCII(name)]; len(vv) > 0 {
		return vv[0]
	}
	return ""
}

// boolOf parses the OData boolean family: "true" (case-insensitive) is true;
// every other present value is false.
func (v *v2Query) boolOf(name string) bool {
	return lowerASCII(strings.TrimSpace(v.get(name))) == "true"
}

// dequote strips one layer of OData single quotes and collapses the doubled
// escape ('x' → x, Can”t → Can't).
func dequote(s string) string {
	s = trimSpaceASCII(s)
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		s = s[1 : len(s)-1]
	}
	return strings.ReplaceAll(s, "''", "'")
}

// ---- semVerLevel (nuget.md section 4, official wiki semantics — the ladder
// live-verified against nuget.org's v2 face August 2026: plain feed hides
// dotted-prerelease and build-metadata versions, exactly the count delta of
// those spellings; 2.5.0 behaves as 2.0.0; 3.0.0 falls back to 2.0.0;
// garbage parses as absent; the comparison is case-insensitive) ----

// semverLevel is the parsed semVerLevel: 2 means "include SemVer 2.0.0
// versions", 0 the default (filter them out).
type semverLevel int

const (
	semverLevelDefault semverLevel = iota
	semverLevelInclude
)

// parseSemVerLevel applies the official ladder: absent or unparsable =
// default; >=2.0.0 and <3 = include; >=3 = fall back to include. The
// comparison is the version ORDER itself ("2.0.0-beta" sits below 2.0.0
// and therefore stays at the default level).
func parseSemVerLevel(raw string) semverLevel {
	raw = lowerASCII(trimSpaceASCII(raw))
	if raw == "" {
		return semverLevelDefault
	}
	if _, ok := parseNuGetVersion(raw); !ok {
		return semverLevelDefault
	}
	if compareNuGetVersions(raw, "2.0.0") >= 0 {
		return semverLevelInclude
	}
	return semverLevelDefault
}

// isSemVer2Version reports whether one version spelling needs semVerLevel
// 2.0.0 to be visible: build metadata, or a prerelease section of MORE THAN
// ONE dot-separated identifier ("7.0.0-alpha.1" is SemVer2-only;
// "3.0.0-alpha", "6.0.0-alpha0001", "2.1.0-rc1-final" are v1-visible).
// Live-verified against nuget.org (FluentAssertions 134 vs 147, the delta
// exactly its 13 alpha.N spellings; StackExchange.Redis 171 vs 200, the
// delta exactly its 29 dotted prereleases).
func isSemVer2Version(s string) bool {
	v, ok := parseNuGetVersion(s)
	if !ok {
		return true // never enters facts through validated paths; hidden when unparsable
	}
	if v.build != "" {
		return true
	}
	return len(v.prerelease) > 1
}

// semVerVisible reports whether the version survives the level's filter.
func semVerVisible(version string, lvl semverLevel) bool {
	if lvl >= semverLevelInclude {
		return true
	}
	return !isSemVer2Version(version)
}

// ---- the OData generic options ----

// odataOpts is the parsed option set the feed endpoints honor.
type odataOpts struct {
	filter      string
	orderBy     string // the first $orderby clause's field (id|version), "" = default
	orderDesc   bool
	top, skip   int
	inlinecount bool
	skipToken   string // accepted, never answered — feeds serve whole
	sel         string // $select/$expand accepted, the fan-out resets them (section 7.2-3)
}

// parseOData reads the generic options; an UNSUPPORTED $filter form is the
// 400 (the honest refusal — silently ignoring a filter would answer a
// different question than the client asked).
func parseOData(q *v2Query) (odataOpts, error) {
	var o odataOpts
	o.filter = strings.TrimSpace(q.get("$filter"))
	o.skipToken = q.get("$skiptoken")
	o.sel = q.get("$select")
	_ = q.get("$expand")
	o.inlinecount = lowerASCII(q.get("$inlinecount")) == "allpages"
	o.top = parseNonNegative(q.get("$top"))
	o.skip = parseNonNegative(q.get("$skip"))
	if clause := strings.TrimSpace(q.get("$orderby")); clause != "" {
		field, dir, found := strings.Cut(clause, " ")
		switch lowerASCII(trimSpaceASCII(field)) {
		case "version":
			o.orderBy = "version"
		case "id":
			o.orderBy = "id"
		default:
			return o, fmt.Errorf("unsupported $orderby field %q", field)
		}
		if found {
			switch lowerASCII(trimSpaceASCII(dir)) {
			case "desc":
				o.orderDesc = true
			case "asc", "":
			default:
				return o, fmt.Errorf("unsupported $orderby direction %q", dir)
			}
		}
	}
	if o.filter != "" {
		if !validV2Filter(o.filter) {
			return o, fmt.Errorf("unsupported $filter %q", o.filter)
		}
	}
	return o, nil
}

// validV2Filter accepts the filter family the v2 clients send: the latest
// flags, the Id equality, and the substringof form (the official "Filter
// OData query requests" set). The IsLatestValue drop when semVerLevel=2.0.0
// is applied at evaluation, not parse (nuget.ignoreIsLatestVersionFilter
// defaults true — section 4's Artifactory supplement).
func validV2Filter(f string) bool {
	switch {
	case eqBoolFilter(f, "islatestversion"),
		eqBoolFilter(f, "isabsolutelatestversion"):
		return true
	case strings.HasPrefix(lowerASCII(f), "id eq '") && strings.HasSuffix(f, "'"):
		return true
	case isSubstringOfFilter(f):
		return true
	}
	return false
}

func eqBoolFilter(f, field string) bool {
	low := lowerASCII(trimSpaceASCII(f))
	return low == field+" eq true" || low == field+" eq false"
}

func isSubstringOfFilter(f string) bool {
	low := lowerASCII(trimSpaceASCII(f))
	if !strings.HasPrefix(low, "substringof('") {
		return false
	}
	rest := low[strings.IndexByte(low, ','):]
	return rest == ", id) eq true" || rest == ",tolower(id)) eq true"
}

// filterID extracts the Id operand of an "Id eq 'x'" / substringof filter
// (the operand keeps the ORIGINAL casing — ids compare case-insensitively
// downstream).
func (o odataOpts) filterID() (string, bool) {
	f := trimSpaceASCII(o.filter)
	low := lowerASCII(f)
	if rest, ok := strings.CutPrefix(low, "id eq '"); ok && len(rest) >= 1 && strings.HasSuffix(rest, "'") {
		return f[len(`id eq '`) : len(f)-1], true
	}
	if strings.HasPrefix(low, "substringof('") {
		if open := strings.IndexByte(low, '\''); open >= 0 {
			if second := strings.Index(low[open+1:], "'"); second >= 0 {
				start := open + 1
				return f[start : start+second], true
			}
		}
	}
	return "", false
}

// filterLatest reports which latest flag the filter tests, and its wanted
// value.
func (o odataOpts) filterLatest() (absolute bool, wanted, isLatestFilter bool) {
	low := lowerASCII(trimSpaceASCII(o.filter))
	if eqBoolFilter(low, "islatestversion") {
		return false, low == "islatestversion eq true", true
	}
	if eqBoolFilter(low, "isabsolutelatestversion") {
		return true, low == "isabsolutelatestversion eq true", true
	}
	return false, false, false
}

// parseNonNegative parses a bounded non-negative integer ("" and malformed
// → 0; the take ceiling guards the upper end).
func parseNonNegative(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	if n > searchMaxTake {
		return searchMaxTake
	}
	return n
}

// ---- GetUpdates (nuget.md section 2 #11; the server-side filtering
// semantics live-verified against nuget.org August 2026: versions strictly
// greater than the given one, prerelease/semVerLevel filtered,
// versionConstraints ranged server-side, includeAllVersions=false keeps
// only the newest qualifying version per package) ----

// updatesQuery is the GetUpdates parameter triple.
type updatesQuery struct {
	ids                []string
	versions           []string
	constraints        []string
	includePrerelease  bool
	includeAllVersions bool
	targetFrameworks   string // accepted; BinFlow has no TFM compatibility index — registered degradation
}

// parseUpdatesQuery splits the pipe-separated lists (positional alignment:
// packageIds[i] pairs with versions[i] and versionConstraints[i]).
func parseUpdatesQuery(q *v2Query) updatesQuery {
	u := updatesQuery{
		ids:                splitPipe(q.get("packageIds")),
		versions:           splitPipe(q.get("versions")),
		constraints:        splitPipe(q.get("versionConstraints")),
		includePrerelease:  q.boolOf("includePrerelease"),
		includeAllVersions: q.boolOf("includeAllVersions"),
		targetFrameworks:   q.get("targetFrameworks"),
	}
	for len(u.versions) < len(u.ids) {
		u.versions = append(u.versions, "")
	}
	for len(u.constraints) < len(u.ids) {
		u.constraints = append(u.constraints, "")
	}
	return u
}

// splitPipe splits a pipe-separated multi-value parameter (OData quotes
// stripped per element).
func splitPipe(s string) []string {
	s = trimSpaceASCII(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, dequote(p))
	}
	return out
}

// versionRange is one parsed NuGet version range (the bracket grammar;
// a bare exact version pins min=max inclusive).
type versionRange struct {
	min, max       string
	minInc, maxInc bool
	ok             bool
}

// parseVersionRange parses "[1.0,2.0)", "(,2.0]", "[1.0,)" and the bare
// exact "1.0"; ok is false for a malformed spelling (the constraint is then
// ignored — never a wire error, GetUpdates is advisory).
func parseVersionRange(s string) versionRange {
	s = trimSpaceASCII(s)
	if s == "" {
		return versionRange{}
	}
	if !strings.HasPrefix(s, "[") && !strings.HasPrefix(s, "(") {
		v, ok := normalizeNuGetVersion(dequote(s))
		if !ok {
			return versionRange{}
		}
		return versionRange{min: v, max: v, minInc: true, maxInc: true, ok: true}
	}
	if len(s) < 3 || (!strings.HasSuffix(s, "]") && !strings.HasSuffix(s, ")")) {
		return versionRange{}
	}
	r := versionRange{minInc: s[0] == '[', maxInc: s[len(s)-1] == ']'}
	lo, hi, found := strings.Cut(s[1:len(s)-1], ",")
	if !found {
		return versionRange{}
	}
	if v, ok := normalizeNuGetVersion(trimSpaceASCII(lo)); ok && trimSpaceASCII(lo) != "" {
		r.min = v
	} else if trimSpaceASCII(lo) != "" {
		return versionRange{}
	}
	if v, ok := normalizeNuGetVersion(trimSpaceASCII(hi)); ok && trimSpaceASCII(hi) != "" {
		r.max = v
	} else if trimSpaceASCII(hi) != "" {
		return versionRange{}
	}
	r.ok = true
	return r
}

// contains reports whether version sits inside the range.
func (r versionRange) contains(version string) bool {
	if !r.ok {
		return true
	}
	if r.min != "" {
		c := compareNuGetVersions(version, r.min)
		if c < 0 || (c == 0 && !r.minInc) {
			return false
		}
	}
	if r.max != "" {
		c := compareNuGetVersions(version, r.max)
		if c > 0 || (c == 0 && !r.maxInc) {
			return false
		}
	}
	return true
}
