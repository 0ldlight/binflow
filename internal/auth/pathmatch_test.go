package auth

import "testing"

// Pattern table driven by auth-model.md section 4 (high confidence):
// '**'/'**/*' = everything; the directory-prefix rule (Ant matchStart) is
// gated on the path being a folder (trailing '/'), mirroring upstream's
// isFolder() gate — file paths match fully only (B-2 fix).
func TestPathMatcherMatch(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		// everything forms
		{"", "a/b.bin", true},
		{"**", "a/b.bin", true},
		{"**", "", true},
		{"**/*", "a/b.bin", true},
		{"**/*", "a.bin", true},

		// directory + descendants
		{"ci-out/**", "ci-out", true},                // the directory itself (file form)
		{"ci-out/**", "ci-out/", true},               // the directory itself (folder form)
		{"ci-out/**", "ci-out/y.bin", true},          // direct child
		{"ci-out/**", "ci-out/sub/deep/z.bin", true}, // deep descendant
		{"ci-out/**", "ci-outside.bin", false},       // no partial-segment bleed
		{"ci-out/**", "other/y.bin", false},

		// bare directory prefix: folder paths only (matchStart under the
		// isFolder gate). A FILE never matches through a prefix rule.
		{"ci-out", "ci-out/", true},           // folder itself
		{"ci-out", "ci-out/y.bin", false},     // B-2: file under it — full match required
		{"ci-out", "ci-out/sub/", true},       // deeper folder
		{"ci-out", "ci-out/sub/z.bin", false}, // B-2: file deeper still
		{"ci-out", "other/y.bin", false},

		// B-2 regression rows (reviewer probes): wildcard/plain file
		// patterns must NOT grant their sub-paths.
		{"a/*/c", "a/b/c/d", false},                            // wildcard file pattern, deeper path
		{"a/*/c", "a/b/c/", true},                              // ...but a folder below it is covered
		{"acme/artifact.bin", "acme/artifact.bin/evil", false}, // file pattern, sub-path file
		{"acme/artifact.bin", "acme/artifact.bin/evil/", true}, // sub-path folder: matchStart covers folders below a plain-name pattern
		{"**/release", "x/release/inner", false},               // trailing plain segment, deeper file
		{"**/release", "x/release/inner/", true},               // deeper folder is covered

		// single '*' inside a segment
		{"*.bin", "a.bin", true},
		{"*.bin", "a/b.bin", false}, // '*' does not cross '/'
		{"ci-*", "ci-out", true},
		{"ci-*", "ci", false},
		{"a/*/c", "a/b/c", true},
		{"a/*/c", "a/b/d/c", false}, // '*' spans exactly one segment

		// '**' in the middle
		{"a/**/c", "a/c", true},
		{"a/**/c", "a/x/c", true},
		{"a/**/c", "a/x/y/c", true},
		{"a/**/c", "a/x/d", false},

		// exact
		{"acme/artifact.bin", "acme/artifact.bin", true},
		{"acme/artifact.bin", "acme/artifact2.bin", false},

		// leading/trailing slashes and whitespace tolerance
		{"/ci-out/**", "ci-out/a.bin", true},
		{" ci-out/** ", "ci-out/a.bin", true},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+" ~ "+tt.path, func(t *testing.T) {
			if got := (pathMatcher{pattern: tt.pattern}).match(tt.path); got != tt.want {
				t.Fatalf("match(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

// Empty path: only the everything-forms match it (M-4 pin). Can callers
// should normalize repo-root listings before consulting the Authorizer.
func TestPathMatcherEmptyPath(t *testing.T) {
	for _, p := range []string{"", "**", "**/*"} {
		if !(pathMatcher{pattern: p}).match("") {
			t.Fatalf("everything-form %q must match the empty path", p)
		}
	}
	for _, p := range []string{"a/**", "a", "*", "a/*/c"} {
		if (pathMatcher{pattern: p}).match("") {
			t.Fatalf("pattern %q must not match the empty path", p)
		}
	}
}

func TestMatchesAny(t *testing.T) {
	if !matchesAny([]string{"a/**", "b/**"}, "a/x.bin") {
		t.Fatal("first pattern should hit")
	}
	if !matchesAny([]string{"a/**", "b/**"}, "b/x.bin") {
		t.Fatal("second pattern should hit")
	}
	if matchesAny([]string{"a/**"}, "c/x.bin") {
		t.Fatal("no pattern should hit")
	}
	if matchesAny(nil, "a/x.bin") {
		t.Fatal("empty pattern list matches nothing; callers handle the empty-includes-means-all rule")
	}
}
