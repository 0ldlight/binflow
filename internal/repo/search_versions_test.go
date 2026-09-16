package repo

// The version kernel's ordering and literal-segment projection (aql.md
// §16.2 as the L024-4 differential pinned it — the directory segment rides
// verbatim, expansion never; the snapshot parts feed the latestVersion
// non-wildcard arm) — pure table legs; the wire faces live in httpapi.

import (
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int // sign of CompareVersions(a, b)
	}{
		{"1.1", "1.0", 1},
		{"1.0", "1.1", -1},
		{"1.10", "1.9", 1}, // numeric segments, not lexicographic
		{"1.0", "1.0-SNAPSHOT", 1},
		{"2.0-20260915.175736-1", "2.0-20260914.175736-1", 1},
		{"2.0-20260915.175736-1", "1.1", 1},
		{"1.0", "1.0", 0},
		{"0.9", "1.0", -1},
	}
	for _, tt := range tests {
		got := CompareVersions(tt.a, tt.b)
		if (got > 0) != (tt.want > 0) || (got < 0) != (tt.want < 0) {
			t.Fatalf("CompareVersions(%q, %q) = %d, want sign %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestCollectVersions(t *testing.T) {
	nodes := []*metadata.Node{
		{RepoKey: "m", Path: "com/acme/app/1.0/app-1.0.jar"},
		{RepoKey: "m", Path: "com/acme/app/1.1/app-1.1.jar"},
		{RepoKey: "m", Path: "com/acme/app/1.1/app-1.1-sources.jar"}, // classifier: same version line
		{RepoKey: "m", Path: "com/acme/app/2.0-SNAPSHOT/app-2.0-20260915.175736-1.jar"},
		{RepoKey: "m", Path: "com/acme/app/2.0-SNAPSHOT/app-2.0-SNAPSHOT.jar"}, // non-unique spelling
		{RepoKey: "m", Path: "com/acme/app/2.0-SNAPSHOT/app-2.0-sources.jar"},  // classifier inside a snapshot dir
	}
	got := collectVersions(nodes, "com.acme", "app")
	// Diff L1: the segment is literal — the 2.0-SNAPSHOT directory is ONE
	// row (integration), carrying the line's snapshot parts for the
	// latestVersion arm (diff L2); the expansion never renders here.
	want := []ArtifactVersion{
		{Value: "2.0-SNAPSHOT", Integration: true, SnapshotTS: "20260915.175736", SnapshotBuild: "1"},
		{Value: "1.1", Integration: false},
		{Value: "1.0", Integration: false},
	}
	if len(got) != len(want) {
		t.Fatalf("versions = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
