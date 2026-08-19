package maven

import (
	"fmt"
	"regexp"
	"strings"
)

// LayoutKind is what a repository-relative path addresses under the
// maven-2-default layout: an artifact file, a maven-metadata.xml document,
// or a checksum sidecar of either.
type LayoutKind string

const (
	// KindArtifact is a jar/pom/classified file matching the
	// artifactPathPattern (descriptor included: .pom is the distinctive
	// descriptor pattern of the same template family).
	KindArtifact LayoutKind = "artifact"
	// KindMetadata is maven-metadata.xml or the plugin-group variant
	// metadata-maven-metadata.xml, at module or version level.
	KindMetadata LayoutKind = "metadata"
	// KindSidecar is <file>.{sha1,md5,sha256,sha512}; Target carries the
	// repository-relative path of <file> itself.
	KindSidecar LayoutKind = "checksum-sidecar"
)

// checksumSuffixes maps the sidecar file suffixes onto their digest
// algorithm. sha512 is recognized for layout purposes (the six-field model
// peels ANY checksum suffix before template matching) but BinFlow's digest
// model is sha256/sha1/md5 only: a .sha512 sidecar GET answers 404 and its
// PUT is accepted without a comparison (there is no measured sha512 to
// compare against — ADR-0006's three-digest model).
var checksumSuffixes = []string{".sha256", ".sha512", ".sha1", ".md5"}

// metadataFileNames are the two metadata spellings the spec fixes
// (maven-npm-pypi.md section 1.2, high confidence): the standard
// maven-metadata.xml and the plugin-group variant whose group is literally
// named "metadata" (metadata-maven-metadata.xml).
var metadataFileNames = map[string]bool{
	"maven-metadata.xml":          true,
	"metadata-maven-metadata.xml": true,
}

// fileItegRevRegExp is fileIntegrationRevisionRegExp of maven-2-default:
// the literal SNAPSHOT spelling or the unique/timestamped snapshot form
// yyyyMMdd.HHmmss-N. The dot between date and time is matched literally —
// the Artifactory pattern's "." predates the escaping habit, but the only
// spelling real Maven clients produce is the literal dot.
var fileItegRevRegExp = regexp.MustCompile(`^[0-9]{8}\.[0-9]{6}-[0-9]+`)

// Layout is the parsed form of one repository-relative path. The zero
// value is not meaningful; Parse fills every field the Kind needs.
type Layout struct {
	// Kind is which of the three path families the request addressed.
	Kind LayoutKind
	// OrgPath is the groupId with dots (com.acme); empty never holds for
	// artifact/metadata paths (at least one org segment is required).
	OrgPath string
	// Module is the artifactId segment.
	Module string
	// VersionDir is the version directory spelling (1.0.0 or
	// 1.0.0-SNAPSHOT); empty for module-level metadata.
	VersionDir string
	// BaseRev is VersionDir without the -SNAPSHOT suffix.
	BaseRev string
	// Snapshot reports whether the version directory is a -SNAPSHOT
	// directory (the folder-integration-revision branch).
	Snapshot bool
	// Timestamped reports an artifact file in the unique/timestamped
	// snapshot spelling (module-baseRev-yyyyMMdd.HHmmss-N...).
	Timestamped bool
	// File is the requested file name verbatim (checksum suffix included
	// for sidecars).
	File string
	// Target is the repository-relative path of the file a sidecar belongs
	// to (KindSidecar only).
	Target string
	// TargetKind is the Kind of Target (artifact or metadata); KindSidecar
	// only.
	TargetKind LayoutKind
	// Algo is the sidecar's digest algorithm (sha1/md5/sha256/sha512);
	// KindSidecar only.
	Algo string
}

// Parse validates relPath against the maven-2-default layout and returns
// its parsed form. The classification order is the spec's (section 1.2,
// high confidence): checksum suffix first, then the metadata file names,
// then the artifact template. Segments are assumed already validated by
// the shared content-path rules (adapter.Layout: no empty/dot segments, no
// control bytes, bounded length) — Parse adds only the maven structure.
//
// The rejection status code is BinFlow's interim C2 ruling (400): the
// six-field model itself is high confidence, the refusal code is not in
// the spec.
func Parse(relPath string) (Layout, error) {
	segs := strings.Split(relPath, "/")
	n := len(segs)
	file := segs[n-1]

	// Step 1: peel ONE checksum suffix (the outermost). A sidecar of a
	// sidecar (x.jar.sha1.sha1) classifies as the sidecar of x.jar.sha1,
	// which itself parses as an artifact with a dotted extension — lenient
	// by construction, and the ext token is arbitrary per FR-16.
	if algo, stripped, ok := stripChecksumSuffix(file); ok {
		if stripped == "" {
			return Layout{}, fmt.Errorf("maven layout: %q is a bare checksum suffix", relPath)
		}
		l, err := parseNonSidecar(joinPath(segs[:n-1], stripped))
		if err != nil {
			return Layout{}, err
		}
		return Layout{
			Kind:        KindSidecar,
			OrgPath:     l.OrgPath,
			Module:      l.Module,
			VersionDir:  l.VersionDir,
			BaseRev:     l.BaseRev,
			Snapshot:    l.Snapshot,
			Timestamped: l.Timestamped,
			File:        file,
			Target:      joinPath(segs[:n-1], stripped),
			TargetKind:  l.Kind,
			Algo:        algo,
		}, nil
	}
	return parseNonSidecar(relPath)
}

// parseNonSidecar parses a path whose final segment is not a checksum
// suffix: a metadata document or an artifact file.
func parseNonSidecar(relPath string) (Layout, error) {
	segs := strings.Split(relPath, "/")
	n := len(segs)
	file := segs[n-1]
	dirs := segs[:n-1]

	if metadataFileNames[file] {
		// Metadata lives at module level ({orgPath}/{module}/…) and at
		// version level ({orgPath}/{module}/{version}/…). One org segment
		// plus the module is the floor (>= 2 directories): a metadata file
		// directly under the repository root is not a maven path. Deeper
		// directory stacks are absorbed into orgPath — the version-level
		// spelling cannot be told from module level without content, and
		// the transfer plane treats both identically (the distinction is
		// the metadata calculator's business, T-68).
		if n < 3 {
			return Layout{}, fmt.Errorf(
				"maven layout: %q: maven-metadata.xml needs at least a groupId and artifactId directory", relPath)
		}
		return Layout{
			Kind:    KindMetadata,
			OrgPath: dotJoin(dirs[:len(dirs)-1]),
			Module:  dirs[len(dirs)-1],
			File:    file,
		}, nil
	}

	// Artifact template: [orgPath]/[module]/[version]/[module]-…
	if n < 4 {
		return Layout{}, fmt.Errorf(
			"maven layout: %q: artifacts need <groupId path>/<artifactId>/<version>/<file> (at least 3 directories)", relPath)
	}
	module := segs[n-3]
	versionDir := segs[n-2]
	org := dotJoin(segs[:n-3])

	baseRev, snapshot := versionDir, false
	if strings.HasSuffix(versionDir, "-SNAPSHOT") {
		baseRev, snapshot = strings.TrimSuffix(versionDir, "-SNAPSHOT"), true
		if baseRev == "" {
			return Layout{}, fmt.Errorf("maven layout: %q: the version directory is a bare -SNAPSHOT", relPath)
		}
	}

	timestamped, err := matchArtifactFileName(module, versionDir, baseRev, snapshot, file)
	if err != nil {
		return Layout{}, fmt.Errorf("maven layout: %q: %w", relPath, err)
	}
	return Layout{
		Kind:        KindArtifact,
		OrgPath:     org,
		Module:      module,
		VersionDir:  versionDir,
		BaseRev:     baseRev,
		Snapshot:    snapshot,
		Timestamped: timestamped,
		File:        file,
	}, nil
}

// matchArtifactFileName checks the file name against the artifact template
// tail `[module]-[baseRev](-[fileItegRev])(-[classifier]).[ext]` and
// reports the timestamped-snapshot spelling. Classifier and extension
// spellings are deliberately free (FR-16: "classifier/扩展任意") — the
// load-bearing rule is the `<artifactId>-<version>` prefix (M20's second
// refusal case) plus the integration-revision grammar for snapshot
// directories.
func matchArtifactFileName(module, versionDir, baseRev string, snapshotDir bool, file string) (timestamped bool, err error) {
	prefix := module + "-"
	if !strings.HasPrefix(file, prefix) {
		return false, fmt.Errorf("file name %q must start with %q", file, module+"-<version>")
	}
	rest := file[len(prefix):]

	if snapshotDir {
		// Non-unique branch: the file spells the full version directory
		// (module-1.0.0-SNAPSHOT[-classifier].ext).
		if strings.HasPrefix(rest, versionDir) {
			if err := checkTail(rest[len(versionDir):], file); err != nil {
				return false, err
			}
			return false, nil
		}
		// Unique/timestamped branch: module-1.0.0-yyyyMMdd.HHmmss-N…
		if strings.HasPrefix(rest, baseRev+"-") {
			tail := rest[len(baseRev)+1:]
			m := fileItegRevRegExp.FindString(tail)
			if m == "" {
				return false, fmt.Errorf(
					"file name %q does not spell the version as %q or a timestamped %q form", file, versionDir, baseRev+"-yyyyMMdd.HHmmss-N")
			}
			if err := checkTail(tail[len(m):], file); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, fmt.Errorf(
			"file name %q does not spell the version as %q or a timestamped %q form", file, versionDir, baseRev+"-yyyyMMdd.HHmmss-N")
	}

	// Release directory: the file spells the directory version, everything
	// after it is -classifier/.ext.
	if !strings.HasPrefix(rest, versionDir) {
		return false, fmt.Errorf("file name %q must start with %q", file, module+"-"+versionDir)
	}
	if err := checkTail(rest[len(versionDir):], file); err != nil {
		return false, err
	}
	return false, nil
}

// checkTail validates what follows the version spelling inside an artifact
// file name: either ".ext" or "-classifier.ext" (a dot inside the
// classifier is tolerated — the ext token is the tail after the LAST dot).
// An empty tail or one without any extension is refused: [ext] is a
// mandatory template token.
func checkTail(tail, file string) error {
	if tail == "" {
		return fmt.Errorf("file name %q has no extension after the version", file)
	}
	if tail[0] != '.' && tail[0] != '-' {
		return fmt.Errorf("file name %q: unexpected %q after the version spelling", file, tail[0:1])
	}
	if !strings.Contains(tail, ".") {
		return fmt.Errorf("file name %q has no extension", file)
	}
	if strings.HasSuffix(tail, ".") {
		return fmt.Errorf("file name %q ends with an empty extension", file)
	}
	return nil
}

// stripChecksumSuffix peels one checksum suffix off file, reporting the
// algorithm name and the stripped name.
func stripChecksumSuffix(file string) (algo, stripped string, ok bool) {
	for _, sfx := range checksumSuffixes {
		if strings.HasSuffix(file, sfx) && len(file) > len(sfx) {
			return strings.TrimPrefix(sfx, "."), file[:len(file)-len(sfx)], true
		}
	}
	return "", "", false
}

// joinPath rebuilds directory segments plus one file name.
func joinPath(dirs []string, file string) string {
	return strings.Join(append(dirs, file), "/")
}

// dotJoin renders groupId path segments in dotted form ("com/acme" ->
// "com.acme"). An empty segment list yields "".
func dotJoin(segs []string) string { return strings.Join(segs, ".") }

// GAV renders the path's groupId:artifactId identity (the MetadataProvider
// package name and log/audit contexts).
func (l Layout) GAV() string { return l.OrgPath + ":" + l.Module }
