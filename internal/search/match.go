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
