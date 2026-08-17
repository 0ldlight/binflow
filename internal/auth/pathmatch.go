package auth

import "strings"

// pathMatcher implements the Ant-style two-level wildcard matching of
// Artifactory permission patterns (auth-model.md section 4, high
// confidence: o.a.a.util.PathMatcher over Spring AntPathMatcher, tokens
// trimmed of whitespace; '**' and '**/*' both mean "everything").
//
// Semantics implemented (kept deliberately minimal — M1 patterns are repo
// paths, not general Ant paths):
//
//   - pattern "" (or "**" or "**/*") matches every path;
//   - '*' matches any run of characters except '/';
//   - '**' matches any run including '/';
//   - a pattern ending in '/**' matches the directory itself and everything
//     under it ("ci-out/**" matches "ci-out" and "ci-out/a/b.bin");
//   - a prefix directory match (Ant matchStart) covers paths under a
//     pattern that names a directory — **but only when the path is itself a
//     folder** (trailing '/'): upstream enables matchStart exactly when
//     repoPath.isFolder() is true, so a *file* path never gains access
//     through a pattern that merely names one of its ancestor directories.
//     Convention (B-2 fix, mirrors repo.isFolderNode): a path ending in '/'
//     is a folder, everything else is a file. "ci-out" therefore matches
//     the folder "ci-out/" and anything below it, but NOT the file
//     "ci-out/a.bin" — use "ci-out/**" for that;
//   - matching is case-sensitive; segments are trimmed of surrounding
//     whitespace before comparison (AntPathMatcher token trimming).
type pathMatcher struct {
	pattern string
}

// match reports whether path (repo-relative, no leading '/') matches the
// pattern. A trailing '/' on path marks a folder and is the only form that
// can match through the directory-prefix rule.
func (m pathMatcher) match(path string) bool {
	pattern := m.pattern
	if pattern == "" || pattern == "**" || pattern == "**/*" {
		return true
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	isFolder := strings.HasSuffix(path, "/")
	pSegs := splitSegments(pattern)
	tSegs := splitSegments(path)
	return segsMatch(pSegs, tSegs, isFolder)
}

// segsMatch matches pattern segments against path segments with backtracking
// on '**'. After all pattern segments are consumed:
//   - a path exactly consumed is a full match;
//   - leftover path segments are covered only when the path is a folder AND
//     the last pattern segment is a plain directory name (Ant matchStart
//     under its upstream isFolder() gate) — file paths must match fully.
func segsMatch(pSegs, tSegs []string, isFolder bool) bool {
	pi, ti := 0, 0
	lastPlain := false
	for pi < len(pSegs) {
		seg := pSegs[pi]
		switch {
		case seg == "**":
			// '**' at the end absorbs the whole rest (zero or more
			// segments); otherwise try every alignment of the remaining
			// pattern (classic Ant backtracking).
			rest := pSegs[pi+1:]
			for skip := ti; skip <= len(tSegs); skip++ {
				if segsMatch(rest, tSegs[skip:], isFolder) {
					return true
				}
			}
			return false
		case ti >= len(tSegs):
			return false
		case !segmentMatch(seg, tSegs[ti]):
			return false
		default:
			lastPlain = !strings.Contains(seg, "*")
			pi++
			ti++
		}
	}
	if ti == len(tSegs) {
		return true
	}
	// Pattern exhausted with path segments left: the directory-prefix rule
	// (matchStart) applies only to folder paths.
	return isFolder && lastPlain && pi > 0
}

// segmentMatch matches one path segment: '*' spans anything except '/'
// (already guaranteed by segmentation), all other characters compare
// literally.
func segmentMatch(pattern, segment string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == segment
	}
	return wildcardEqual(pattern, segment)
}

// wildcardEqual compares a segment containing '*' with a concrete segment.
func wildcardEqual(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	// Must start with the first literal and end with the last.
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	last := parts[len(parts)-1]
	if !strings.HasSuffix(s, last) {
		return false
	}
	s = s[:len(s)-len(last)]
	// Middle literals must appear in order.
	for _, mid := range parts[1 : len(parts)-1] {
		if mid == "" {
			continue
		}
		idx := strings.Index(s, mid)
		if idx < 0 {
			return false
		}
		s = s[idx+len(mid):]
	}
	return true
}

// splitSegments splits a pattern/path on '/' and trims whitespace from each
// segment (Ant token trimming), dropping empties. A trailing '/' therefore
// leaves no marker segment; folder-ness is tracked separately by the caller.
func splitSegments(s string) []string {
	raw := strings.Split(s, "/")
	segs := make([]string, 0, len(raw))
	for _, seg := range raw {
		seg = strings.TrimSpace(seg)
		if seg != "" {
			segs = append(segs, seg)
		}
	}
	return segs
}

// matchesAny reports whether path matches any of the patterns (an empty
// pattern list contributes nothing; callers treat "no include patterns" as
// "include everything" separately, per auth-model.md section 4).
func matchesAny(patterns []string, path string) bool {
	for _, p := range patterns {
		if (pathMatcher{pattern: p}).match(path) {
			return true
		}
	}
	return false
}
