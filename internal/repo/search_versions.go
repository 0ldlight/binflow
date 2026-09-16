package repo

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The D03 version-search kernel (L024-3A, aql.md §16.2/§16.3): the ordered
// version list behind GET /api/search/versions and GET /api/search/
// latestVersion. The two endpoints share Artifactory's own single source —
// getArtifactVersions — so BinFlow derives both from one collector: the gavc
// kernel's hit set (g+a literal path pieces) projected onto version rows,
// newest first.
//
// Version semantics (§16.2 as the L024-4 differential pinned it, diff L1):
//   - the version value is the [version] path segment LITERALLY — a
//     directory holding the literal "1.1-SNAPSHOT" file name reports
//     "1.1-SNAPSHOT"; the expanded form appears only for directories the
//     storage itself names with the timestamp (a direct PUT of a
//     timestamp-named file); metadata expansion is never consulted;
//   - integration is the literal "-SNAPSHOT" substring of the segment;
//   - ordering is newest first.

// VersionSearchService is the version-family capability face of the concrete
// Service implementation (the LegacySearchService precedent: asserted by
// consumers, deliberately not part of the big Service interface so the
// hand-written fakes stay untouched).
type VersionSearchService interface {
	// SearchVersions returns every version of the g:a coordinate, newest
	// first, deduplicated. g and a are both required; limit must be positive;
	// repos narrows like the legacy family's filter.
	SearchVersions(ctx context.Context, p *Principal, g, a string, limit int, repos []string) ([]ArtifactVersion, error)
}

// ArtifactVersion is one row of the versions family (§16.2: the two-key thin
// row — no uri).
type ArtifactVersion struct {
	Value       string
	Integration bool
	// SnapshotTS/SnapshotBuild are the integration line's unique-snapshot
	// parts (yyyyMMdd.HHmmss and the -N build number, from the newest
	// uniquely-named file) — the latestVersion non-wildcard arm's raw
	// material (diff L2).
	SnapshotTS    string
	SnapshotBuild string
}

// SearchVersions implements VersionSearchService: the gavc kernel's hit set
// projected onto version rows. The endpoint-side v-pattern filter
// deliberately does NOT live here — §16.2's execution order filters by
// pattern only AFTER the empty-set verdict, which is a wire concern.
func (s *service) SearchVersions(ctx context.Context, p *Principal, g, a string, limit int, repos []string) ([]ArtifactVersion, error) {
	g, a = strings.TrimSpace(g), strings.TrimSpace(a)
	if g == "" || a == "" {
		return nil, fmt.Errorf("%w: version search requires both g and a", ErrInvalidSearchQuery)
	}
	nodes, err := s.SearchGavc(ctx, p, GavcQuery{Group: g, Artifact: a}, limit, repos)
	if err != nil {
		return nil, err
	}
	return collectVersions(nodes, g, a), nil
}

// collectVersions projects gavc hit paths onto deduplicated version rows,
// newest first (diff L1: the segment rides verbatim, no expansion).
func collectVersions(nodes []*metadata.Node, g, a string) []ArtifactVersion {
	prefix := strings.ReplaceAll(g, ".", "/") + "/" + a + "/"
	byValue := map[string]*ArtifactVersion{}
	var out []*ArtifactVersion
	for _, n := range nodes {
		if !strings.HasPrefix(n.Path, prefix) {
			continue
		}
		rest := n.Path[len(prefix):]
		dir, _, _ := strings.Cut(rest, "/")
		if dir == "" {
			continue
		}
		row := &ArtifactVersion{
			Value:       dir,
			Integration: strings.Contains(dir, "-SNAPSHOT"),
		}
		if row.Integration {
			// The line's unique-snapshot parts feed the latestVersion
			// non-wildcard arm (diff L2): keep the NEWEST unique file's
			// timestamp/build pair seen for the line.
			if ts, build, ok := uniqueSnapshotParts(dir, n.Path, a); ok &&
				(byValue[dir] == nil || ts > byValue[dir].SnapshotTS) {
				row.SnapshotTS, row.SnapshotBuild = ts, build
			} else if byValue[dir] != nil && byValue[dir].SnapshotTS != "" {
				row.SnapshotTS, row.SnapshotBuild = byValue[dir].SnapshotTS, byValue[dir].SnapshotBuild
			}
		}
		if prev := byValue[dir]; prev != nil {
			if row.SnapshotTS > prev.SnapshotTS {
				prev.SnapshotTS, prev.SnapshotBuild = row.SnapshotTS, row.SnapshotBuild
			}
			continue
		}
		byValue[dir] = row
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return CompareVersions(out[i].Value, out[j].Value) > 0 })
	rows := make([]ArtifactVersion, 0, len(out))
	for _, r := range out {
		rows = append(rows, *r)
	}
	return rows
}

// uniqueSnapshotParts extracts the timestamp/buildNumber pair of a
// uniquely-named snapshot file (module-<base>-yyyyMMdd.HHmmss-N.ext) inside
// the dir's line — the latestVersion non-wildcard arm's raw material.
func uniqueSnapshotParts(dir, path, module string) (ts, build string, ok bool) {
	file := path
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		file = path[i+1:]
	}
	base := strings.TrimPrefix(file, module+"-")
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	if !isUniqueSnapshotOf(base, dir) {
		return "", "", false
	}
	tail := base[len(strings.TrimSuffix(dir, "-SNAPSHOT"))+1:]
	parts := strings.Split(tail, "-")
	return parts[0], parts[len(parts)-1], true
}

// isUniqueSnapshotOf reports whether candidate is dir's unique-snapshot
// expansion: the base version (dir minus "-SNAPSHOT") followed by the
// timestamp-buildNumber pair (yyyyMMdd.HHmmss-N — the maven unique layout).
func isUniqueSnapshotOf(candidate, dir string) bool {
	base := strings.TrimSuffix(dir, "-SNAPSHOT")
	if !strings.HasPrefix(candidate, base+"-") {
		return false
	}
	tail := candidate[len(base)+1:]
	parts := strings.Split(tail, "-")
	if len(parts) < 2 {
		return false
	}
	build := parts[len(parts)-1]
	if _, err := strconv.Atoi(build); err != nil {
		return false
	}
	stamp := parts[len(parts)-2]
	if len(stamp) != 15 || stamp[8] != '.' {
		return false
	}
	for i := 0; i < len(stamp); i++ {
		if i == 8 {
			continue
		}
		if stamp[i] < '0' || stamp[i] > '9' {
			return false
		}
	}
	return true
}

// CompareVersions orders two version strings: >0 when a is newer than b,
// <0 older, 0 equal. Segments split on '.' and '-' compare numerically when
// both are digits, lexically otherwise; a numeric segment beats a qualifier
// word at the same position (2.0-20260915... > 2.0-SNAPSHOT), and a
// prefix-equal longer spelling is newer while its extension stays numeric
// (1.0.1 > 1.0) but older once qualifiers ride it (1.0 > 1.0-SNAPSHOT,
// maven's release-beats-snapshot).
// ponytail: qualifier words (alpha/RC/...) compare lexically, not by
// maven's qualifier table — recalibrate against a qualifier-bearing corpus
// if a differential flags it.
func CompareVersions(a, b string) int {
	as, bs := splitVersionSegments(a), splitVersionSegments(b)
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aerr := strconv.Atoi(as[i])
		bn, berr := strconv.Atoi(bs[i])
		switch {
		case aerr == nil && berr == nil:
			if an != bn {
				if an > bn {
					return 1
				}
				return -1
			}
		case aerr == nil:
			return 1 // numeric beats the qualifier word
		case berr == nil:
			return -1
		case as[i] != bs[i]:
			if as[i] > bs[i] {
				return 1
			}
			return -1
		}
	}
	switch {
	case len(as) > len(bs):
		if allNumeric(as[len(bs):]) {
			return 1
		}
		return -1
	case len(as) < len(bs):
		if allNumeric(bs[len(as):]) {
			return -1
		}
		return 1
	}
	return 0
}

// allNumeric reports whether every segment is digits.
func allNumeric(segs []string) bool {
	for _, s := range segs {
		if _, err := strconv.Atoi(s); err != nil {
			return false
		}
	}
	return len(segs) > 0
}

// splitVersionSegments splits a version spelling into its dot/dash
// segments, dropping empties (the "1..0" and trailing-dash shapes).
func splitVersionSegments(v string) []string {
	return strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '-' })
}
