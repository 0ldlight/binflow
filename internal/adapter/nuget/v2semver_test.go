package nuget

import (
	"encoding/xml"
	"net/http"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The semVerLevel ladder (nuget.md section 4) — the boundary rule and the
// ladder's behavior live-verified against nuget.org's v2 face (August
// 2026): FluentAssertions counts 134 plain vs 147 at 2.0.0 (the delta is
// exactly its 13 dotted alpha.N spellings), StackExchange.Redis 171 vs 200
// (exactly its 29), 2.5.0 behaves as 2.0.0, 3.0.0 falls back to 2.0.0,
// garbage parses as absent, and the comparison is case-insensitive.

func TestIsSemVer2Version(t *testing.T) {
	cases := []struct {
		version string
		semver2 bool
		note    string
	}{
		{"1.0.0", false, "plain release"},
		{"1.0.0-a", false, "single-identifier prerelease (nuget.org serves it plainly)"},
		{"1.0.0-alpha0001", false, "mixed single identifier"},
		{"2.1.0-rc1-final", false, "hyphenated single identifier"},
		{"1.0.0-b.1", true, "DOTTED prerelease — the spec's own 7.2 example family"},
		{"7.0.0-alpha.1", true, "the live-probe family"},
		{"1.0.0+build.5", true, "build metadata"},
		{"4.0.0-rc.1.23421.29", true, "long dotted"},
	}
	for _, tc := range cases {
		if got := isSemVer2Version(tc.version); got != tc.semver2 {
			t.Errorf("isSemVer2Version(%s) = %v, want %v (%s)", tc.version, got, tc.semver2, tc.note)
		}
	}
}

func TestParseSemVerLevel(t *testing.T) {
	cases := []struct {
		raw  string
		want semverLevel
	}{
		{"", semverLevelDefault},
		{"1.0.0", semverLevelDefault},
		{"2.0.0", semverLevelInclude},
		{"2.5.0", semverLevelInclude},
		{"3.0.0", semverLevelInclude}, // the >=3 fallback onto 2.0.0
		{"9.9.9", semverLevelInclude},
		{"2.0.0-beta", semverLevelDefault},
		{"garbage", semverLevelDefault},
	}
	for _, tc := range cases {
		if got := parseSemVerLevel(tc.raw); got != tc.want {
			t.Errorf("parseSemVerLevel(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

// TestV2SemVerLevelFeed: the feed filters the SemVer2-only versions by
// default and includes them at level 2.0.0; the latest flags follow the
// visible set (nuget.md section 4's IsLatest interplay — the spec's own
// 1.0.0-a / 1.0.0-b.1 example).
func TestV2SemVerLevelFeed(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	mustPushV2(t, s, "ng-local", buildNupkg(t, "Sem.Pkg", "1.0.0-a", flatDeps("none")))
	mustPushV2(t, s, "ng-local", buildNupkg(t, "Sem.Pkg", "1.0.0-b.1", flatDeps("none")))

	feed := func(q string) (int, atomFeed) {
		status, body, _ := s.get(apiV2Path("ng-local") + "/FindPackagesById()?id='Sem.Pkg'" + q)
		var doc atomFeed
		if status == http.StatusOK {
			if err := xml.Unmarshal([]byte(body), &doc); err != nil {
				t.Fatalf("feed: %v\n%s", err, body)
			}
		}
		return status, doc
	}

	status, doc := feed("&includePrerelease=true")
	if status != http.StatusOK || len(doc.Entries) != 1 || doc.Entries[0].Properties.Version != "1.0.0-a" {
		t.Fatalf("default level = (%d, %+v), want only 1.0.0-a", status, doc.Entries)
	}
	if !doc.Entries[0].Properties.IsAbsoluteLatest {
		t.Errorf("default level: 1.0.0-a must carry the absolute-latest flag")
	}
	status, doc = feed("&includePrerelease=true&semVerLevel=2.0.0")
	if status != http.StatusOK || len(doc.Entries) != 2 {
		t.Fatalf("level 2.0.0 = (%d, %d entries), want both", status, len(doc.Entries))
	}
	if doc.Entries[1].Properties.Version != "1.0.0-b.1" || !doc.Entries[1].Properties.IsAbsoluteLatest {
		t.Errorf("level 2.0.0: b.1 must be the absolute latest (it sorts above a)")
	}
	if doc.Entries[0].Properties.IsAbsoluteLatest {
		t.Errorf("level 2.0.0: a must have lost the flag")
	}
	// The ladder: 3.0.0 falls back to include; garbage stays default; the
	// comparison ignores case.
	status, doc = feed("&includePrerelease=true&semVerLevel=3.0.0")
	if status != http.StatusOK || len(doc.Entries) != 2 {
		t.Fatalf("level 3.0.0 = %d entries, want the 2.0.0 fallback", len(doc.Entries))
	}
	if status, doc = feed("&includePrerelease=true&semVerLevel=garbage"); status != http.StatusOK || len(doc.Entries) != 1 {
		t.Fatalf("level garbage = %d entries, want default", len(doc.Entries))
	}
	if status, doc = feed("&includePrerelease=true&semVerLevel=2.0.0"); status != http.StatusOK || len(doc.Entries) != 2 {
		t.Fatalf("uppercase probe = %d entries", len(doc.Entries))
	}
	// $count walks the same ladder.
	if status, body, _ := s.get(apiV2Path("ng-local") + "/FindPackagesById()/$count?id='Sem.Pkg'&includePrerelease=true"); status != http.StatusOK || body != "1" {
		t.Errorf("default count = (%d, %q)", status, body)
	}
}
