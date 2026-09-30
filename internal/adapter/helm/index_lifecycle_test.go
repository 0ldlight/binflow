package helm

// BIN-111 / T-627: the local repository's empty-index lifecycle — the
// never-populated fetch materializes the A-form empty document (200, not
// the lifecycle-gap 404 that broke `helm repo add`), repeat fetches are
// byte-stable off the stored node, the populate step merges into the
// materialized document, and the delete cycle converges on the SAME
// empty form (the two empty states are one shape).

import (
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/lzwzzy/binflow/internal/repo"
)

// emptyIndexForm pins the A-face empty document: an UNQUOTED RFC3339Nano
// generated stamp (Artifactory's Instant.toString shape, 69B at full
// nanosecond width).
var emptyIndexForm = regexp.MustCompile(`\AapiVersion: v1\nentries: \{\}\ngenerated: \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z\n\z`)

// TestRenderEmptyIndexAForm pins the serialization seam: the empty
// document carries the unquoted nanosecond stamp (Artifactory's
// Instant.toString 3-group truncation), the populated document keeps the
// quoted-seconds S4 contract.
func TestRenderEmptyIndexAForm(t *testing.T) {
	for _, tc := range []struct {
		nanos int64
		want  string
	}{
		{123456789, "generated: 2026-09-30T01:02:03.123456789Z\n"},
		{123456000, "generated: 2026-09-30T01:02:03.123456Z\n"}, // 3-group trim, not single-zero
		{120000000, "generated: 2026-09-30T01:02:03.120Z\n"},
		{0, "generated: 2026-09-30T01:02:03Z\n"},
	} {
		now := time.Date(2026, 9, 30, 1, 2, 3, int(tc.nanos), time.UTC)
		if got, want := string((&indexDoc{}).render(now)),
			"apiVersion: v1\nentries: {}\n"+tc.want; got != want {
			t.Errorf("empty render (nanos=%d) = %q, want %q", tc.nanos, got, want)
		}
	}
	doc := &indexDoc{entries: map[string][]*yaml.Node{}}
	doc.upsertEntry("c", buildFixtureEntry(t, defaultChartYAML("c", "0.1.0"), "c-0.1.0.tgz", "abc123"))
	now := time.Date(2026, 9, 30, 1, 2, 3, 123456789, time.UTC)
	if body := string(doc.render(now)); !strings.Contains(body, `generated: "2026-09-30T01:02:03Z"`) {
		t.Errorf("populated render lost the quoted-seconds stamp:\n%s", body)
	}
}

// TestNeverPopulatedIndexMaterializes walks the full client lifecycle:
// both wire spellings, HEAD, byte stability, populate-into-materialized
// and the post-delete convergence.
func TestNeverPopulatedIndexMaterializes(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-local", repo.TypeLocal, "{}")

	for _, path := range []string{
		"/binflow/helm-local/index.yaml",
		"/binflow/api/helm/helm-local/index.yaml",
	} {
		status, body, hdr := s.get(path)
		if status != http.StatusOK || !emptyIndexForm.MatchString(body) {
			t.Errorf("GET %s = (%d, %q), want 200 A-form empty", path, status, body)
			continue
		}
		if ct := hdr.Get("Content-Type"); ct != "text/yaml" {
			t.Errorf("GET %s Content-Type = %q, want text/yaml", path, ct)
		}
	}
	if status, body, _ := s.do(http.MethodHead, "/binflow/helm-local/index.yaml", "", "", nil, nil); status != http.StatusOK || body != "" {
		t.Errorf("HEAD empty index = (%d, %q), want 200 with no body", status, body)
	}

	// The materialized node is the cache: repeat fetches byte-stable. The
	// landing needs a write-capable principal (an anonymous fetcher is
	// served the synthesized body — the read-only fallback arm above);
	// once materialized, ANY fetcher serves the stored bytes.
	if status, body, _ := s.do(http.MethodGet, "/binflow/helm-local/index.yaml", adminUser, adminPass, nil, nil); status != http.StatusOK || !emptyIndexForm.MatchString(body) {
		t.Fatalf("admin GET = (%d, %q), want 200 A-form empty", status, body)
	}
	_, first, _ := s.get("/binflow/helm-local/index.yaml")
	_, second, _ := s.get("/binflow/helm-local/index.yaml")
	if first != second {
		t.Errorf("repeat fetch regenerated the empty index:\n%q\n%q", first, second)
	}

	chart := fixtureChart(t, "c", defaultChartYAML("c", "1.0.0"), nil)
	if status, body, _ := s.put("/binflow/helm-local/c-1.0.0.tgz", chart, nil); status != http.StatusCreated {
		t.Fatalf("chart PUT = (%d, %s), want 201", status, body)
	}
	if _, body, _ := s.get("/binflow/helm-local/index.yaml"); !strings.Contains(body, "name: c") {
		t.Fatalf("populated index lost the chart (merge into the materialized doc failed):\n%s", body)
	}
	if status, _, _ := s.delete("/binflow/helm-local/c-1.0.0.tgz"); status != http.StatusNoContent {
		t.Fatalf("chart DELETE = %d, want 204", status)
	}
	if status, body, _ := s.get("/binflow/helm-local/index.yaml"); status != http.StatusOK || !emptyIndexForm.MatchString(body) {
		t.Errorf("post-delete index = (%d, %q), want 200 A-form empty (two states, one shape)", status, body)
	}
}

// TestEmptyIndexMaterializeRacesUpload is the lost-update tripwire: an
// empty-index materialization interleaved with chart uploads must never
// wipe a landed entry (the materialize path re-checks under the
// repository's index lock and defers to a populated document).
func TestEmptyIndexMaterializeRacesUpload(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-local", repo.TypeLocal, "{}")

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		version := []string{"0.1.0", "0.2.0", "0.3.0"}[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			chart := fixtureChart(t, "c", defaultChartYAML("c", version), nil)
			s.put("/binflow/helm-local/c-"+version+".tgz", chart, nil)
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.get("/binflow/helm-local/index.yaml")
		}()
	}
	wg.Wait()

	_, body, _ := s.get("/binflow/helm-local/index.yaml")
	for _, version := range []string{"0.1.0", "0.2.0", "0.3.0"} {
		if !strings.Contains(body, "c-"+version+".tgz") {
			t.Errorf("raced index lost chart %s (materialize wiped a landed entry):\n%s", version, body)
		}
	}
}
