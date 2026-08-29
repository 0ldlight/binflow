package maven

// T-354's copy-side reindex entry: the candidate directory set the copy
// observer hands over (§1.4 — the deduplicated parent directories of every
// landed FILE, trailing-slash spellings) must regenerate both metadata
// arms the deploy taxonomy names, skip non-coordinate directories without
// error, and stay a no-op on the calculator-less assembly.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// TestReindexDirsVersionGroup: a copy-shaped fact set (poms + jars under
// two version directories, seeded straight through the service — the
// calculator's inputs are storage facts) regenerates the module's
// version-group document with both versions; the release version
// directories themselves stay document-less (the spec's generator set).
func TestReindexDirsVersionGroup(t *testing.T) {
	hs := newHarness(t)
	hs.seedNode("maven-local", "com/example/lib/1.0/lib-1.0.pom", []byte("pom-1.0"))
	hs.seedNode("maven-local", "com/example/lib/1.0/lib-1.0.jar", []byte("jar-1.0"))
	hs.seedNode("maven-local", "com/example/lib/1.1/lib-1.1.pom", []byte("pom-1.1"))

	dirs := []string{"com/example/lib/1.0/", "com/example/lib/1.1/"} // the seam's spelling
	if err := hs.h.ReindexDirs(context.Background(), adminP, "maven-local", dirs); err != nil {
		t.Fatalf("ReindexDirs: %v", err)
	}

	code, body := hs.getMeta("maven-local", "com.example", "lib", "")
	if code != http.StatusOK {
		t.Fatalf("module metadata = %d, want 200", code)
	}
	mustContain(t, "module metadata", body,
		[]string{"<groupId>com.example</groupId>", "<artifactId>lib</artifactId>",
			"<version>1.0</version>", "<version>1.1</version>",
			"<latest>1.1</latest>", "<release>1.1</release>"},
		nil)

	// A release version directory carries no document of its own.
	if code, _ := hs.getMeta("maven-local", "com.example", "lib", "1.0"); code != http.StatusNotFound {
		t.Fatalf("release version dir metadata = %d, want 404 (no generator)", code)
	}
}

// TestReindexDirsSnapshot: a SNAPSHOT candidate directory regenerates the
// version document (buildNumber/timestamp from the newest unique snapshot
// pom) and the module list underneath it.
func TestReindexDirsSnapshot(t *testing.T) {
	hs := newHarness(t)
	hs.seedNode("maven-local", "com/example/snap/1.0-SNAPSHOT/snap-1.0-20260101.000001-1.pom", []byte("pom"))
	hs.seedNode("maven-local", "com/example/snap/1.0-SNAPSHOT/snap-1.0-20260101.000001-1.jar", []byte("jar"))

	if err := hs.h.ReindexDirs(context.Background(), adminP, "maven-local",
		[]string{"com/example/snap/1.0-SNAPSHOT/"}); err != nil {
		t.Fatalf("ReindexDirs: %v", err)
	}

	code, body := hs.getMeta("maven-local", "com.example", "snap", "1.0-SNAPSHOT")
	if code != http.StatusOK {
		t.Fatalf("snapshot metadata = %d, want 200", code)
	}
	mustContain(t, "snapshot metadata", body,
		[]string{"<version>1.0-SNAPSHOT</version>", "<timestamp>20260101.000001</timestamp>",
			"<buildNumber>1</buildNumber>", "<value>1.0-20260101.000001-1</value>"},
		nil)
	if code, body := hs.getMeta("maven-local", "com.example", "snap", ""); code != http.StatusOK ||
		!strings.Contains(body, "<version>1.0-SNAPSHOT</version>") {
		t.Fatalf("module metadata after snapshot reindex = (%d, %s)", code, body)
	}
}

// TestReindexDirsSkipsNonCoordinates: directories that cannot spell a
// version directory (fewer than three segments) are skipped, never an
// error — no metadata materializes for them.
func TestReindexDirsSkipsNonCoordinates(t *testing.T) {
	hs := newHarness(t)
	err := hs.h.ReindexDirs(context.Background(), adminP, "maven-local",
		[]string{"lib/", "lib/1.0/", "-", "a/b/"})
	if err != nil {
		t.Fatalf("ReindexDirs over non-coordinate dirs: %v", err)
	}
	nodes, err := hs.md.Nodes().ListByPrefix(context.Background(), "maven-local", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, n := range nodes {
		if strings.HasSuffix(n.Path, metadataFileName) {
			t.Fatalf("metadata materialized for a non-coordinate dir: %s", n.Path)
		}
	}
}

// TestReindexDirsNilCalculator: the calculator-less assembly (nodes seam
// absent) is a documented no-op, not a panic.
func TestReindexDirsNilCalculator(t *testing.T) {
	hs := newHarness(t)
	bare := New(hs.svc, hs.md.Repos(), nil, nil)
	if err := bare.ReindexDirs(context.Background(), adminP, "maven-local",
		[]string{"com/example/lib/1.0/"}); err != nil {
		t.Fatalf("calculator-less ReindexDirs: %v", err)
	}
}

// TestReindexDirsPrunesEmptiedModule: the module recalculation is the full
// generator — a module holding only a hand-seeded document (the shape a
// copy of a maven-metadata.xml without its poms leaves behind) loses it
// under the no-pom cleanup rule.
func TestReindexDirsPrunesEmptiedModule(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()
	hs.seedNode("maven-local", "com/example/ghost/maven-metadata.xml",
		[]byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<metadata><groupId>com.example</groupId><artifactId>ghost</artifactId><versioning><versions><version>9.9</version></versions></versioning></metadata>\n"))
	if err := hs.h.ReindexDirs(ctx, adminP, "maven-local", []string{"com/example/ghost/1.0/"}); err != nil {
		t.Fatalf("ReindexDirs: %v", err)
	}
	_, _, err := hs.svc.Get(ctx, adminP, "maven-local", "com/example/ghost/maven-metadata.xml")
	if !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("stale module document survived the recompute: %v", err)
	}
}
