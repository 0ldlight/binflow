package search

import "testing"

// T-411 AC2: the wildcard→LIKE kernel — the single translator AQL
// $match/$nmatch and the legacy pattern endpoints (T-417) share. The table
// pins every wildcard and every escaped literal byte, including that
// multi-byte UTF-8 passes through untouched (byte-wise translation is
// safe because the special bytes are all ASCII).
func TestLikePattern(t *testing.T) {
	tests := []struct {
		pattern string
		want    string
	}{
		{"", ""},
		{"abc", "abc"},
		{"*", "%"},
		{"?", "_"},
		{"*.jar", "%.jar"},
		{"a?b*c", "a_b%c"},
		{"%", "\\%"},
		{"_", "\\_"},
		{"\\", "\\\\"},
		{"a%b_c\\d", "a\\%b\\_c\\\\d"},
		{"100%_done", "100\\%\\_done"},
		{"日本語*", "日本語%"},
		{"資料-??.txt", "資料-__.txt"},
		{"*/*/*.pom", "%/%/%.pom"},
	}
	for _, tt := range tests {
		if got := LikePattern(tt.pattern); got != tt.want {
			t.Errorf("LikePattern(%q) = %q, want %q", tt.pattern, got, tt.want)
		}
	}
}

func TestHasWildcard(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"plain", false},
		{"", false},
		{"with*star", true},
		{"with?question", true},
		{"日本語*", true},
		{"%literal-like-only", false},
	}
	for _, tt := range tests {
		if got := HasWildcard(tt.pattern); got != tt.want {
			t.Errorf("HasWildcard(%q) = %v, want %v", tt.pattern, got, tt.want)
		}
	}
}
