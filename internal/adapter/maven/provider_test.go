package maven

import (
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// TestProviderClassify pins the content/metadata split the remote cache's
// two TTLs key on (T-66's consumer contract).
func TestProviderClassify(t *testing.T) {
	cases := []struct {
		path string
		want adapter.MetadataKind
	}{
		{"com/acme/app/1.0.0/app-1.0.0.jar", adapter.KindContent},
		{"com/acme/app/1.0.0/app-1.0.0.pom", adapter.KindContent},
		{"com/acme/app/1.0.0/app-1.0.0.jar.sha1", adapter.KindContent},
		{"com/acme/app/1.2.0-SNAPSHOT/app-1.2.0-20240819.101500-1.jar", adapter.KindContent},
		{"com/acme/app/maven-metadata.xml", adapter.KindMetadata},
		{"com/acme/app/maven-metadata.xml.sha1", adapter.KindMetadata},
		{"com/acme/app/maven-metadata.xml.md5", adapter.KindMetadata},
		{"com/acme/app/1.2.0-SNAPSHOT/maven-metadata.xml", adapter.KindMetadata},
		{"org/apache/maven/plugins/maven-metadata.xml", adapter.KindMetadata},
		{"org/apache/maven/plugins/metadata-maven-metadata.xml", adapter.KindMetadata},
		{"foo.jar", adapter.KindContent},                          // outside the layout: content (safe side)
		{"com/acme/app/1.0.0/zzz-1.0.0.jar", adapter.KindContent}, // valid layout? no — still content default
	}
	var p metadataProvider
	for _, tc := range cases {
		if got := p.Classify(tc.path); got != tc.want {
			t.Errorf("Classify(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// TestProviderPackageName pins the groupId:artifactId identity.
func TestProviderPackageName(t *testing.T) {
	cases := []struct {
		path string
		name string
		ok   bool
	}{
		{"com/acme/app/1.0.0/app-1.0.0.jar", "com.acme:app", true},
		{"org/apache/commons/commons-lang3/3.12.0/commons-lang3-3.12.0.pom", "org.apache.commons:commons-lang3", true},
		{"com/acme/app/1.2.0-SNAPSHOT/app-1.2.0-20240819.101500-1.jar.sha256", "com.acme:app", true},
		{"com/acme/app/maven-metadata.xml", "com.acme:app", true},
		{"com/acme/app/1.2.0-SNAPSHOT/maven-metadata.xml", "com.acme.app:1.2.0-SNAPSHOT", true}, // module-level heuristic (documented)
		{"foo.jar", "", false},
		{"com/app.jar", "", false},
	}
	var p metadataProvider
	for _, tc := range cases {
		got, ok := p.PackageName(tc.path)
		if ok != tc.ok || got != tc.name {
			t.Errorf("PackageName(%q) = %q, %v; want %q, %v", tc.path, got, ok, tc.name, tc.ok)
		}
	}
}

// TestProviderRegistration exercises the registry wiring (idempotent,
// keyed on "maven", comparator non-nil).
func TestProviderRegistration(t *testing.T) {
	RegisterMetadata()
	RegisterMetadata() // second call must not panic (multi-assembly guard)

	p, ok := adapter.ForProtocol(Protocol)
	if !ok {
		t.Fatal("ForProtocol(maven) miss after RegisterMetadata")
	}
	if p.Protocol() != Protocol {
		t.Errorf("Protocol() = %q, want %q", p.Protocol(), Protocol)
	}
	if p.Versions() == nil {
		t.Error("Versions() = nil, want the maven comparator")
	}
	if _, ok := adapter.ForProtocol("no-such-protocol"); ok {
		t.Error("ForProtocol(no-such-protocol) hit")
	}
	if !IsMetadataPath("com/acme/app/maven-metadata.xml") || IsMetadataPath("com/acme/app/1.0.0/app-1.0.0.jar") {
		t.Error("IsMetadataPath disagrees with Classify")
	}
	if gav, ok := GAVOf("com/acme/app/1.0.0/app-1.0.0.jar"); !ok || gav != "com.acme:app" {
		t.Errorf("GAVOf = %q, %v", gav, ok)
	}
}

// TestRepoConfigParsing pins the policy defaults and the lenient unknown
// spellings.
func TestRepoConfigParsing(t *testing.T) {
	def := ParseRepoConfig("")
	if def.ChecksumPolicy != ChecksumPolicyClient || def.SnapshotBehavior != BehaviorDeployer ||
		!def.HandleReleases || !def.HandleSnapshots {
		t.Fatalf("defaults wrong: %+v", def)
	}
	if got := ParseRepoConfig(`{"checksumPolicyType":"server-generated-checksums","handleSnapshots":false,"snapshotVersionBehavior":"non-unique"}`); got.AcceptsSnapshot() ||
		!got.AcceptsRelease() || got.ChecksumPolicy != ChecksumPolicyServerGenerated || got.SnapshotBehavior != BehaviorNonUnique {
		t.Fatalf("parsed wrong: %+v", got)
	}
	if got := ParseRepoConfig(`{"checksumPolicyType":"generate-if-absent"}`); got.ChecksumPolicy != ChecksumPolicyClient {
		t.Errorf("unknown policy must fall back to client-checksums, got %q", got.ChecksumPolicy)
	}
	if got := ParseRepoConfig("not json at all"); got.ChecksumPolicy != ChecksumPolicyClient || !got.AcceptsSnapshot() {
		t.Errorf("garbage blob must yield defaults, got %+v", got)
	}
	if got := ParseRepoConfig(`{"handleReleases":false}`); got.AcceptsRelease() || !got.AcceptsSnapshot() {
		t.Errorf("handleReleases=false not honored: %+v", got)
	}
}
