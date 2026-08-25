package goproxy

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version grammar and ordering (goproxy.md section 5.1 / section 4.5).
//
// The grammar anchor set is the reverse spec's double-sourced regex table
// (official canonical definition + the decompiled equivalence classes):
//
//	release:      vMAJOR.MINOR.PATCH[-prerelease][+incompatible]
//	pseudo:       vX.0.0-yyyymmddhhmmss-abcdefabcdef
//	              | vX.Y.Z-pre.0.yyyymmddhhmmss-abcdefabcdef
//	              | vX.Y.(Z+1)-0.yyyymmddhhmmss-abcdefabcdef
//
// PUT accepts exactly the canonical spellings (no leading zeros in the
// numeric core); GET paths tolerate non-canonical versions (the official
// protocol allows querying .info by branch name or revision).

var (
	// releaseRE is the canonical release form, prerelease included, with the
	// no-leading-zero rule on the numeric core.
	releaseRE = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
		`(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+incompatible)?$`)
	// pseudoRE is the three pseudo-version shapes verbatim from the spec
	// table (goproxy.md section 5.1, third row).
	pseudoRE = regexp.MustCompile(`^v[0-9]+\.(0\.0-|[0-9]+\.[0-9]+-([^+]*\.)?0\.)` +
		`\d{14}-[A-Za-z0-9]+(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
	// canonicalCoreRE splits the vX.Y.Z core off any version spelling.
	canonicalCoreRE = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)`)
	// majorSuffixRE matches a module path's final "/vN" element with N != 0/1
	// (module paths may not carry /v0 or /v1 — the official module path rule).
	majorSuffixRE = regexp.MustCompile(`^v([2-9][0-9]*)$`)
	// gopkgMajorRE matches a gopkg.in element's ".vN" suffix (the gopkg.in
	// dot-suffix spelling of the same major rule).
	gopkgMajorRE = regexp.MustCompile(`\.v([2-9][0-9]*)$`)
)

// validPutVersion reports whether a PUT target version is a canonical
// release, +incompatible or pseudo-version spelling (PRD 87.3's "version
// grammar" gate; GET stays tolerant per the official protocol).
func validPutVersion(version string) bool {
	return releaseRE.MatchString(version) || pseudoRE.MatchString(version)
}

// checkMajorConsistency enforces the module-path major rule on PUT
// (goproxy.md section 5.1 last bullet): a path ending in /vN (N>=2) — or a
// gopkg.in path ending in .vN — must carry a version with the same major.
func checkMajorConsistency(module, version string) error {
	seg := module[strings.LastIndexByte(module, '/')+1:]
	want := int64(-1)
	if m := majorSuffixRE.FindStringSubmatch(seg); m != nil {
		want, _ = strconv.ParseInt(m[1], 10, 64)
	} else if strings.HasPrefix(module, "gopkg.in/") {
		if m := gopkgMajorRE.FindStringSubmatch(seg); m != nil {
			want, _ = strconv.ParseInt(m[1], 10, 64)
		}
	}
	if want < 0 {
		return nil
	}
	m := canonicalCoreRE.FindStringSubmatch(version)
	if m == nil {
		return fmt.Errorf("version %q is not a canonical version and cannot be checked against the module path major", version)
	}
	got, _ := strconv.ParseInt(m[1], 10, 64)
	if got != want {
		return fmt.Errorf("module path %q requires major version v%d, got %q", module, want, version)
	}
	return nil
}

// isIncompatible reports the +incompatible suffix (the synthesized-.mod
// trigger of goproxy.md section 4.2).
func isIncompatible(version string) bool {
	return strings.HasSuffix(version, "+incompatible")
}

// parsedVersion is the ordering key of one version spelling.
type parsedVersion struct {
	major, minor, patch uint64
	prerelease          string // "" = release class
	pseudo              bool
	pseudoTime          string // 14-digit commit timestamp of a pseudo-version
	raw                 string
}

// class ranks the @latest candidate classes by the official preference
// order (goproxy.md section 4.5): release beats pre-release beats
// pseudo-version.
func (v parsedVersion) class() int {
	switch {
	case v.pseudo:
		return 2
	case v.prerelease != "":
		return 1
	default:
		return 0
	}
}

// parseVersion splits a version spelling into its ordering key. ok is false
// for anything without a vX.Y.Z core (a branch-name query, garbage); such
// spellings never enter @latest selection.
func parseVersion(version string) (parsedVersion, bool) {
	m := canonicalCoreRE.FindStringSubmatch(version)
	if m == nil {
		return parsedVersion{}, false
	}
	major, _ := strconv.ParseUint(m[1], 10, 64)
	minor, _ := strconv.ParseUint(m[2], 10, 64)
	patch, _ := strconv.ParseUint(m[3], 10, 64)
	pv := parsedVersion{major: major, minor: minor, patch: patch, raw: version}
	rest := version[len(m[0]):]
	if pseudoRE.MatchString(version) {
		pv.pseudo = true
		// The 14-digit timestamp is the ordering key inside the pseudo class.
		if i := strings.IndexByte(rest, '-'); i >= 0 {
			body := rest[i+1:]
			for j := 0; j+len("yyyymmddhhmmss") <= len(body); j++ {
				if isAllDigits(body[j : j+14]) {
					pv.pseudoTime = body[j : j+14]
					break
				}
			}
		}
		return pv, true
	}
	rest = strings.TrimSuffix(rest, "+incompatible")
	if strings.HasPrefix(rest, "-") {
		pv.prerelease = rest[1:]
	}
	return pv, true
}

func isAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

// CompareVersions orders two version spellings by the @latest candidate
// semantics (release > pre-release > pseudo; within a class the semver
// order, pseudo-versions by commit timestamp). It implements
// adapter.VersionComparator for this protocol.
func CompareVersions(a, b string) int {
	pa, oka := parseVersion(a)
	pb, okb := parseVersion(b)
	switch {
	case oka && !okb:
		return 1
	case !oka && okb:
		return -1
	case !oka && !okb:
		return strings.Compare(a, b)
	}
	if pa.class() != pb.class() {
		if pa.class() < pb.class() {
			return 1 // release beats pre-release beats pseudo
		}
		return -1
	}
	if c := compareU64(pa.major, pb.major); c != 0 {
		return c
	}
	if c := compareU64(pa.minor, pb.minor); c != 0 {
		return c
	}
	if c := compareU64(pa.patch, pb.patch); c != 0 {
		return c
	}
	if pa.pseudo || pb.pseudo {
		// Inside the pseudo class the commit time decides (equal times fall
		// back to the raw spelling for a total order).
		if c := strings.Compare(pa.pseudoTime, pb.pseudoTime); c != 0 {
			return c
		}
		return strings.Compare(pa.raw, pb.raw)
	}
	return comparePrerelease(pa.prerelease, pb.prerelease)
}

func compareU64(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// comparePrerelease applies the semver pre-release ordering (x/mod/semver
// semantics): no pre-release beats a pre-release; dot-separated identifiers
// compare left to right, numeric identifiers numerically and always below
// alphanumeric ones; a shorter identifier list is lower when it is a prefix.
func comparePrerelease(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		ai, bi := as[i], bs[i]
		an, bn := isAllDigits(ai), isAllDigits(bi)
		switch {
		case an && bn:
			// Numeric identifiers compare numerically; equal values fall
			// through to length (v1.0.0-2 vs v1.0.0-10).
			if len(ai) != len(bi) {
				if len(ai) < len(bi) {
					return -1
				}
				return 1
			}
			if c := strings.Compare(ai, bi); c != 0 {
				return c
			}
		case an:
			return -1
		case bn:
			return 1
		default:
			if c := strings.Compare(ai, bi); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	default:
		return 0
	}
}

// pickLatest selects the @latest winner of a version set: the official
// candidate order (release > pre-release > newest pseudo). Ties (identical
// spellings) keep the first occurrence.
func pickLatest(versions []string) (string, bool) {
	best := ""
	found := false
	for _, v := range versions {
		if !found || CompareVersions(v, best) > 0 {
			best, found = v, true
		}
	}
	return best, found
}
