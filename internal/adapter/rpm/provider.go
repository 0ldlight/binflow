package rpm

import (
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// The RPM metadata provider (the adapter SPI's per-protocol member,
// architecture section 5.4). Consumers:
//
//   - Classify feeds the remote cache's dual TTL split (the M11 remote
//     ticket's engine hop): everything under a repodata/ segment is the
//     regenerable protocol document family (metadata class); every .rpm
//     and every other file is immutable content.
//   - PackageName is the identity a path belongs to. RPM identity is
//     HEADER-carried, not path-carried — the path form is the conventional
//     <name>-<version>-<release>.<arch>.rpm, so the best-effort arm splits
//     the FINAL four dash/suffix segments and answers the head; a path
//     that does not resolve answers ok=false (the virtual ticket reads
//     the rpm.metadata.* node properties for the exact view).
//   - Versions is the EVR ordering the virtual best-version seam consumes
//     (epoch numeric, then rpmvercmp on version and release — the
//     comparator createrepo_c and dnf's libsolv agree on).
type provider struct{}

// RegisterMetadata enters the provider in the process-wide registry; the
// duplicate guard keeps repeated assembly (and test stacks that mount the
// handler more than once) from panicking (the maven.RegisterMetadata
// convention).
func RegisterMetadata() {
	if _, ok := adapter.ForProtocol(Protocol); ok {
		return
	}
	adapter.RegisterMetadata(provider{})
}

// Protocol implements adapter.MetadataProvider.
func (provider) Protocol() string { return Protocol }

// Classify implements adapter.MetadataProvider.
func (provider) Classify(relPath string) adapter.MetadataKind {
	if _, _, ok := splitRepodata(relPath); ok {
		return adapter.KindMetadata
	}
	return adapter.KindContent
}

// PackageName implements adapter.MetadataProvider (the best-effort
// filename split; see the type comment).
func (provider) PackageName(relPath string) (string, bool) {
	base := relPath
	if i := strings.LastIndexByte(relPath, '/'); i >= 0 {
		base = relPath[i+1:]
	}
	if !strings.HasSuffix(base, suffixRpm) {
		return "", false
	}
	stem := strings.TrimSuffix(base, suffixRpm)
	// <name>-<version>-<release>.<arch>: split from the right on '-'
	// twice, then the arch on '.'; the remainder is the name.
	first := strings.LastIndexByte(stem, '-')
	if first <= 0 {
		return "", false
	}
	second := strings.LastIndexByte(stem[:first], '-')
	if second <= 0 {
		return "", false
	}
	arch := stem[first+1:]
	if !strings.Contains(arch, ".") || arch == "." {
		return "", false
	}
	return stem[:second], true
}

// Versions implements adapter.MetadataProvider.
func (provider) Versions() adapter.VersionComparator { return comparator{} }

// comparator adapts compareEVR to adapter.VersionComparator (both sides
// may carry "E:V-R"; missing pieces compare as empty).
type comparator struct{}

func (comparator) CompareVersions(a, b string) int { return compareEVR(a, b) }

// compareEVR orders two EVR strings: epoch numerically (missing = 0), then
// version, then release, each with rpmvercmp.
func compareEVR(a, b string) int {
	ae, av, ar := parseEVR(a)
	be, bv, br := parseEVR(b)
	an, bn := atoiDefault(ae), atoiDefault(be)
	if an != bn {
		if an < bn {
			return -1
		}
		return 1
	}
	if c := rpmvercmp(av, bv); c != 0 {
		return c
	}
	return rpmvercmp(ar, br)
}

// atoiDefault parses a decimal epoch ("" and garbage = 0).
func atoiDefault(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}

// rpmvercmp orders two version/release segments — the public RPM
// algorithm: segments split at non-alphanumeric boundaries, numeric
// segments numerically (longer digit run wins when all-equal digits
// compare equal), alpha segments lexically, '~' sorts before everything
// (pre-release), '^' sorts after end-of-segment but before anything else
// (the v6 caret rule). One segment's exhaustion wins unless the other
// continues with '~'.
func rpmvercmp(a, b string) int {
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		var sa, sb string
		i, sa = nextSeg(a, i)
		j, sb = nextSeg(b, j)
		if sa == "" && sb == "" {
			return 0
		}
		// Tilde handling: a segment consisting of '~' sorts before a
		// missing segment and before any non-'~' segment.
		if sa == "~" || sb == "~" {
			if sa != "~" {
				return 1
			}
			if sb != "~" {
				return -1
			}
			continue
		}
		if sa == "" { // a exhausted
			if len(sb) > 0 && sb[0] == '~' {
				return 1
			}
			return -1
		}
		if sb == "" { // b exhausted
			if len(sa) > 0 && sa[0] == '~' {
				return -1
			}
			return 1
		}
		// Caret rule: '^' sorts before everything except a missing segment.
		if (len(sa) > 0 && sa[0] == '^') || (len(sb) > 0 && sb[0] == '^') {
			if sa[0] != '^' {
				return 1
			}
			if sb[0] != '^' {
				return -1
			}
			sa, sb = sa[1:], sb[1:]
			if sa == "" && sb == "" {
				continue
			}
		}
		an, bn := isDigits(sa), isDigits(sb)
		switch {
		case an && bn:
			na, nb := strings.TrimLeft(sa, "0"), strings.TrimLeft(sb, "0")
			if len(na) != len(nb) {
				if len(na) < len(nb) {
					return -1
				}
				return 1
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
		case an:
			return 1 // numeric segments sort after alpha
		case bn:
			return -1
		default:
			if c := strings.Compare(sa, sb); c != 0 {
				return c
			}
		}
	}
	return 0
}

// nextSeg reads the next segment of s from i: a run of digits, a run of
// letters, or one punctuation character ('~' and '^' are their own
// segments; other punctuation groups).
func nextSeg(s string, i int) (int, string) {
	if i >= len(s) {
		return i, ""
	}
	start := i
	c := s[i]
	class := func(b byte) int {
		switch {
		case b >= '0' && b <= '9':
			return 0
		case (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z'):
			return 1
		default:
			return 2
		}
	}
	if c == '~' || c == '^' {
		return i + 1, s[start : start+1]
	}
	k := class(c)
	for i < len(s) && class(s[i]) == k {
		i++
	}
	return i, s[start:i]
}
