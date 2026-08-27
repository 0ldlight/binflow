package deb

import (
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// The Debian metadata provider (the adapter SPI's per-protocol member,
// architecture section 5.4). Consumers:
//
//   - Classify feeds the remote cache's dual TTL split (the M11 remote
//     ticket's engine hop): everything under dists/ is the regenerable
//     protocol document family (metadata class); every .deb, .dsc and
//     companion file is immutable content.
//   - PackageName is the identity a path belongs to. Debian identity is
//     CONTROL-carried, not path-carried — the pool convention is
//     pool/<comp>/<prefix>/<source>/<name>_<ver>_<arch>.deb, so the
//     best-effort arm splits the FINAL segment at the first '_' (the
//     name never carries one); a path that does not resolve answers
//     ok=false (the virtual ticket reads the deb.metadata.* node
//     properties for the exact view).
//   - Versions is the epoch:upstream-revision ordering the virtual
//     best-version seam consumes — dpkg's own comparison algorithm
//     (Debian Policy 5.6.12: epochs numerically, '~' sorts before
//     everything including the empty tail, letters before non-letters,
//     digit runs numerically) — the comparator apt and dpkg agree on.
type provider struct{}

// RegisterMetadata enters the provider in the process-wide registry; the
// duplicate guard keeps repeated assembly (and test stacks that mount the
// handler more than once) from panicking (the rpm convention).
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
	if underDists(relPath) {
		return adapter.KindMetadata
	}
	return adapter.KindContent
}

// PackageName implements adapter.MetadataProvider (the best-effort pool
// filename split; see the type comment).
func (provider) PackageName(relPath string) (string, bool) {
	base := relPath
	if i := strings.LastIndexByte(relPath, '/'); i >= 0 {
		base = relPath[i+1:]
	}
	stem := ""
	switch {
	case strings.HasSuffix(base, suffixDeb):
		stem = strings.TrimSuffix(base, suffixDeb)
	case strings.HasSuffix(base, suffixDsc):
		stem = strings.TrimSuffix(base, suffixDsc)
	default:
		return "", false
	}
	// <name>_<version>_<arch>: the name is the head before the first '_'
	// (Debian names never carry '_'; versions frequently do).
	name, _, ok := strings.Cut(stem, "_")
	if !ok || name == "" {
		return "", false
	}
	return name, true
}

// Versions implements adapter.MetadataProvider.
func (provider) Versions() adapter.VersionComparator { return comparator{} }

// comparator adapts compareDpkgVersion to adapter.VersionComparator.
type comparator struct{}

func (comparator) CompareVersions(a, b string) int { return compareDpkgVersion(a, b) }

// compareDpkgVersion orders two full Debian version strings
// ([epoch:]upstream[-revision]); missing pieces compare as empty.
func compareDpkgVersion(a, b string) int {
	ea, va, ra := splitDpkgVersion(a)
	eb, vb, rb := splitDpkgVersion(b)
	if ea != eb {
		if ea < eb {
			return -1
		}
		return 1
	}
	if c := verrevcmp(va, vb); c != 0 {
		return c
	}
	return verrevcmp(ra, rb)
}

// splitDpkgVersion splits [epoch:]upstream[-revision] (the LAST '-'
// bounds the revision; the FIRST ':' bounds the epoch).
func splitDpkgVersion(v string) (epoch int, upstream, revision string) {
	if i := strings.IndexByte(v, ':'); i >= 0 {
		for _, c := range v[:i] {
			if c < '0' || c > '9' {
				return 0, v, ""
			}
			epoch = epoch*10 + int(c-'0')
		}
		v = v[i+1:]
	}
	if i := strings.LastIndexByte(v, '-'); i >= 0 {
		return epoch, v[:i], v[i+1:]
	}
	return epoch, v, ""
}

// verrevcmp orders two version fragments — the public dpkg algorithm
// (Debian Policy 5.6.12 verrevcmp): walk non-digit runs comparing by the
// modified collation ('~' first, then end-of-string, then letters, then
// non-letters in ASCII order), then digit runs numerically with leading
// zeros erased.
func verrevcmp(a, b string) int {
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		firstDiff := 0
		for (i < len(a) && !isDecDigit(a[i])) || (j < len(b) && !isDecDigit(b[j])) {
			ac, bc := dpkgOrder(a, i), dpkgOrder(b, j)
			if ac != bc {
				if ac < bc {
					return -1
				}
				return 1
			}
			i++
			j++
		}
		for i < len(a) && a[i] == '0' {
			i++
		}
		for j < len(b) && b[j] == '0' {
			j++
		}
		for i < len(a) && isDecDigit(a[i]) && j < len(b) && isDecDigit(b[j]) {
			if firstDiff == 0 {
				firstDiff = int(a[i]) - int(b[j])
			}
			i++
			j++
		}
		if i < len(a) && isDecDigit(a[i]) {
			return 1
		}
		if j < len(b) && isDecDigit(b[j]) {
			return -1
		}
		if firstDiff != 0 {
			if firstDiff < 0 {
				return -1
			}
			return 1
		}
	}
	return 0
}

// dpkgOrder renders the collation rank of s[i] (0 past the end):
// '~' < end < letters < everything else (ASCII within the classes).
func dpkgOrder(s string, i int) int {
	if i >= len(s) {
		return 0
	}
	c := s[i]
	switch {
	case c == '~':
		return -1
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		return int(c)
	case isDecDigit(c):
		return 0
	default:
		return int(c) + 256
	}
}

// isDecDigit reports a decimal digit.
func isDecDigit(c byte) bool { return c >= '0' && c <= '9' }
