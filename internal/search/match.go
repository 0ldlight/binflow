package search

import "strings"

// The wildcard→LIKE translation kernel — the SINGLE translator shared by
// AQL $match/$nmatch (T-411) and the legacy pattern search endpoints
// (T-417), so the two surfaces cannot drift apart (ADR-0043 pt 1/3: "防三
// 个译者"). AQL wildcards are only meaningful inside $match/$nmatch;
// everywhere else values are literal (aql.md §2.5).
//
// Semantics (aql.md §2.4, official criteria page):
//   - '*' matches any sequence (SQL '%')
//   - '?' matches exactly one character (SQL '_')
//   - literal '%', '_' and '\' in the user's value are escaped so they
//     always mean themselves; queries render every LIKE with
//     ESCAPE '\' (the metadata compiler appends the clause)
//
// The translation is byte-wise on purpose: the special bytes are all ASCII
// and multi-byte UTF-8 sequences pass through untouched, so Unicode names
// keep matching exactly (see the 日本語/資料 corpus arm).

// LikePattern translates one AQL wildcard pattern into a SQL LIKE pattern.
// The result matches the WHOLE value (no implicit surrounding %) — callers
// wanting substring semantics (the legacy artifact search, K64) wrap the
// result themselves.
func LikePattern(pattern string) string {
	var b strings.Builder
	b.Grow(len(pattern))
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '*':
			b.WriteByte('%')
		case '?':
			b.WriteByte('_')
		case '%', '_', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// HasWildcard reports whether pattern contains an AQL wildcard ('*' or
// '?'). The planner uses it to tell literal $match values (which can name
// a repository key exactly, and therefore take the virtual-expansion arm)
// from real patterns.
func HasWildcard(pattern string) bool {
	return strings.ContainsAny(pattern, "*?")
}

// MatchesPattern is the in-Go executor of the SAME wildcard semantics
// LikePattern translates for SQL (T-511: the build-family domains evaluate
// predicates row-side over materialized rows, so $match/$nmatch need the
// Go-side half of the one kernel — '*' any sequence, '?' exactly one rune,
// everything else literal; the match is WHOLE-value, no implicit
// surrounding '*'). Byte-wise like the translation: multi-byte UTF-8
// passes through, and '?' consumes ONE byte — matching the SQL '_'
// semantics the item domain runs (a multi-byte character is several '_'
// positions there too).
func MatchesPattern(pattern, s string) bool {
	// Iterative two-pointer match with backtracking on the last '*'.
	pi, si := 0, 0
	star, mark := -1, -1
	for si < len(s) {
		switch {
		case pi < len(pattern) && (pattern[pi] == '?' || pattern[pi] == s[si]):
			pi++
			si++
		case pi < len(pattern) && pattern[pi] == '*':
			star = pi
			mark = si
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			si = mark
		default:
			return false
		}
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}
