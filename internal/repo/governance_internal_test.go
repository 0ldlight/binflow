package repo

import "testing"

// T-95 W12a: the governance pattern matcher's table — the Ant two-level
// wildcard semantics the permission-target matcher (internal/auth) already
// implements, mirrored here for repository includes/excludes. The matrix
// pins the W12a configuration's behavior precisely: "**/*.jar" admits jars
// at any depth (and at the root — '**' spans zero directories too), and
// "secret/**" bars everything beneath secret/ including the folder itself.
func TestGovMatcherMatrix(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		// everything-spellings
		{"**/*", "a/b/c.bin", true},
		{"**", "a/b/c.bin", true},
		{"", "a/b/c.bin", true},
		// W12a's include pattern, both depths ('**' matches zero segments)
		{"**/*.jar", "lib/app.jar", true},
		{"**/*.jar", "app.jar", true},
		{"**/*.jar", "a/b/app.jar", true},
		{"**/*.jar", "a/b/app.txt", false},
		{"**/*.jar", "a/b/app.tar.gz", false},
		// W12a's exclude pattern
		{"secret/**", "secret/x.jar", true},
		{"secret/**", "secret", true},          // the directory itself
		{"secret/**", "secret/nested/x", true}, // any depth below
		{"secret/**", "pub/x.jar", false},
		{"secret/**", "notsecret/x", false},
		// single-segment '*'
		{"*.jar", "app.jar", true},
		{"*.jar", "d/app.jar", false},
		{"a/*/c", "a/b/c", true},
		{"a/*/c", "a/b/c/d", false},
		{"*/*", "a/b", true},
		{"*/*", "a", false},
		// literal patterns
		{"acme/artifact.bin", "acme/artifact.bin", true},
		{"acme/artifact.bin", "acme/other.bin", false},
		{"acme/artifact.bin", "acme/artifact.bin/evil", false}, // file pattern: sub-path files never match
		{"acme/artifact.bin", "acme/artifact.bin/evil/", true}, // sub-path FOLDER: matchStart covers it
		// directory-prefix rule through a plain directory name
		{"ci-out", "ci-out/", true},       // the folder itself
		{"ci-out", "ci-out/a.bin", false}, // a FILE below: no match (the isFolder gate)
		{"ci-out", "ci-out/sub/", true},   // deeper folders: covered
		// comma lists are split before matching (splitPatterns), never seen here
		{"a,b", "a", false},
	}
	for _, tt := range tests {
		if got := (govMatcher{pattern: tt.pattern}).match(tt.path); got != tt.want {
			t.Errorf("match(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}

// T-95: governance parsing — field defaults, list splitting, quota probe —
// and the default short-circuit that keeps unconfigured repositories on the
// M1~M3 path (the property W12a's regression clause rests on).
func TestParseGovernance(t *testing.T) {
	tests := []struct {
		name        string
		config      string
		defaulted   bool
		allow       string // path probed through allowsPath ("" = skip)
		allowWant   bool
		quota       int64
		includesRaw string
	}{
		{"empty config", `{}`, true, "", false, 0, ""},
		{"blank patterns", `{"includesPattern":"","excludesPattern":""}`, true, "", false, 0, ""},
		{"default include spelling", `{"includesPattern":"**/*"}`, true, "", false, 0, "**/*"},
		{"include misses", `{"includesPattern":"**/*.jar"}`, false, "a/b.txt", false, 0, "**/*.jar"},
		{"include hits", `{"includesPattern":"**/*.jar"}`, false, "a/b.jar", true, 0, "**/*.jar"},
		{
			"W12a pair: exclude wins over include",
			`{"includesPattern":"**/*.jar","excludesPattern":"secret/**"}`,
			false, "secret/x.jar", false, 0, "**/*.jar",
		},
		{
			"W12a pair: include hit outside exclude",
			`{"includesPattern":"**/*.jar","excludesPattern":"secret/**"}`,
			false, "pub/x.jar", true, 0, "**/*.jar",
		},
		{
			"comma-separated includes",
			`{"includesPattern":"**/*.jar, **/*.war"}`,
			false, "app.war", true, 0, "**/*.jar, **/*.war",
		},
		{"quota positive", `{"quotaBytes":1024}`, false, "", false, 1024, ""},
		{"quota zero is the default", `{"quotaBytes":0}`, true, "", false, 0, ""},
		{"quota negative tolerated on read", `{"quotaBytes":-5}`, true, "", false, 0, ""},
		{"not JSON degrades to defaults", `not json`, true, "", false, 0, ""},
		{"empty string config", ``, true, "", false, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := parseGovernance(tt.config)
			if g.defaulted() != tt.defaulted {
				t.Errorf("defaulted() = %v, want %v", g.defaulted(), tt.defaulted)
			}
			if tt.allow != "" && g.allowsPath(tt.allow) != tt.allowWant {
				t.Errorf("allowsPath(%q) = %v, want %v", tt.allow, g.allowsPath(tt.allow), tt.allowWant)
			}
			if g.quotaBytes != tt.quota {
				t.Errorf("quotaBytes = %d, want %d", g.quotaBytes, tt.quota)
			}
		})
	}
}
