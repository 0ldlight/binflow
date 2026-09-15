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
// Version semantics (§16.2, live-verified):
//   - the version value is the [version] path segment between the module
//     directory and the file name;
//   - an integration version (the "-SNAPSHOT" directory spelling) reports
//     the EXPANDED unique form (2.0-SNAPSHOT -> 2.0-20260915.175736-1,
//     integration=true) when the directory's files carry unique-snapshot
//     names, and the directory spelling itself otherwise;
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
// newest first.
func collectVersions(nodes []*metadata.Node, g, a string) []ArtifactVersion {
	prefix := strings.ReplaceAll(g, ".", "/") + "/" + a + "/"
	byValue := map[string]bool{}
	var out []ArtifactVersion
	for _, n := range nodes {
		if !strings.HasPrefix(n.Path, prefix) {
			continue
		}
		rest := n.Path[len(prefix):]
		dir, _, _ := strings.Cut(rest, "/")
		if dir == "" {
			continue
		}
		integration := strings.Contains(dir, "-SNAPSHOT")
		value := dir
		if integration {
			value = uniqueSnapshotValue(dir, n.Path, a)
		}
		if byValue[value] {
			continue
		}
		byValue[value] = true
		out = append(out, ArtifactVersion{Value: value, Integration: integration})
	}
	sort.SliceStable(out, func(i, j int) bool { return CompareVersions(out[i].Value, out[j].Value) > 0 })
	return out
}

// uniqueSnapshotValue resolves an integration directory's reported value
// (§16.2): the expanded unique form when the directory's files carry one
// (module-2.0-20260915.175736-1.jar -> 2.0-20260915.175736-1), the directory
// spelling otherwise. The file name is derived from the path itself; any
// shape that does not parse as the module's artifact keeps the directory
// spelling — the honest fallback, never a guess.
func uniqueSnapshotValue(dir, path, module string) string {
	file := path
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		file = path[i+1:]
	}
	base := strings.TrimPrefix(file, module+"-")
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	if base == dir {
		return dir
	}
	if isUniqueSnapshotOf(base, dir) {
		return base
	}
	return dir
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
