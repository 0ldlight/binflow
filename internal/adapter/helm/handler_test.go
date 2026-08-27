package helm

// The full-stack wire tests (helm.md section 2's local column, driven
// through the real httpapi router): the PUT chain with its index
// recomputation, downloads with checksum headers, .prov sidecars, the
// read-only api/helm alias matrix, the class doors, the reindex family
// and the concurrent-upload index consistency.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

func TestUploadBuildsIndex(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-local", repo.TypeLocal, "{}")

	// Empty repository: no index yet (the client's 404).
	if status, _, _ := s.get("/binflow/helm-local/index.yaml"); status != http.StatusNotFound {
		t.Fatalf("empty repo index status = %d, want 404", status)
	}

	chart := fixtureChart(t, "mychart", defaultChartYAML("mychart", "0.1.0"), nil)
	status, body, hdr := s.put("/binflow/helm-local/mychart-0.1.0.tgz", chart, nil)
	if status != http.StatusCreated {
		t.Fatalf("chart PUT = (%d, %s), want 201", status, body)
	}
	if got := hdr.Get("X-Checksum-Sha256"); got != sha256Hex(chart) {
		t.Errorf("PUT X-Checksum-Sha256 = %q, want the storage-measured digest", got)
	}

	// The index materializes: digest reconciles with the tgz bytes, urls
	// are the relative in-repo path.
	status, body, _ = s.get("/binflow/helm-local/index.yaml")
	if status != http.StatusOK {
		t.Fatalf("index after upload = (%d, %s), want 200", status, body)
	}
	if !strings.Contains(body, "digest: "+sha256Hex(chart)) {
		t.Errorf("index digest does not reconcile with the tgz bytes:\n%s", body)
	}
	if !strings.Contains(body, "version: \"0.1.0\"") || !strings.Contains(body, "name: mychart") {
		t.Errorf("index entry identity wrong:\n%s", body)
	}
	if !strings.Contains(body, "- mychart-0.1.0.tgz") {
		t.Errorf("index urls not the relative path:\n%s", body)
	}

	// A second version: newest-first ordering.
	chart2 := fixtureChart(t, "mychart", defaultChartYAML("mychart", "0.2.0"), nil)
	if status, body, _ := s.put("/binflow/helm-local/mychart-0.2.0.tgz", chart2, nil); status != http.StatusCreated {
		t.Fatalf("second chart PUT = (%d, %s), want 201", status, body)
	}
	_, body, _ = s.get("/binflow/helm-local/index.yaml")
	if strings.Index(body, "0.2.0") > strings.Index(body, "0.1.0") {
		t.Errorf("index not newest-first:\n%s", body)
	}

	// The download: bytes verbatim plus the checksum header family.
	status, body, hdr = s.get("/binflow/helm-local/mychart-0.1.0.tgz")
	if status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(chart) {
		t.Fatalf("chart GET = %d (%d bytes), want the stored bytes", status, len(body))
	}
	if hdr.Get("X-Checksum-Sha256") != sha256Hex(chart) {
		t.Errorf("GET X-Checksum-Sha256 = %q", hdr.Get("X-Checksum-Sha256"))
	}

	// Subdirectory uploads keep their relative url shape.
	chart3 := fixtureChart(t, "mychart", defaultChartYAML("mychart", "3.0.0"), nil)
	if status, body, _ := s.put("/binflow/helm-local/stable/mychart-3.0.0.tgz", chart3, nil); status != http.StatusCreated {
		t.Fatalf("subdir chart PUT = (%d, %s), want 201", status, body)
	}
	_, body, _ = s.get("/binflow/helm-local/index.yaml")
	if !strings.Contains(body, "- stable/mychart-3.0.0.tgz") {
		t.Errorf("subdir chart url not path-qualified:\n%s", body)
	}
}

func TestUploadProvSidecar(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-local", repo.TypeLocal, "{}")
	chart := fixtureChart(t, "mychart", defaultChartYAML("mychart", "0.1.0"), nil)
	if status, body, _ := s.put("/binflow/helm-local/mychart-0.1.0.tgz", chart, nil); status != http.StatusCreated {
		t.Fatalf("chart PUT = (%d, %s)", status, body)
	}
	prov := []byte("-----BEGIN PGP SIGNATURE-----\nfake prov body\n-----END PGP SIGNATURE-----\n")
	if status, body, _ := s.put("/binflow/helm-local/mychart-0.1.0.tgz.prov", prov, nil); status != http.StatusCreated {
		t.Fatalf("prov PUT = (%d, %s), want 201", status, body)
	}
	status, body, hdr := s.get("/binflow/helm-local/mychart-0.1.0.tgz.prov")
	if status != http.StatusOK || body != string(prov) {
		t.Fatalf("prov GET = (%d, %d bytes), want the stored bytes", status, len(body))
	}
	if !strings.HasPrefix(hdr.Get("Content-Type"), "text/plain") {
		t.Errorf("prov Content-Type = %q", hdr.Get("Content-Type"))
	}
	// S14: the prov never enters the index.
	_, index, _ := s.get("/binflow/helm-local/index.yaml")
	if strings.Contains(index, "prov") {
		t.Errorf("index mentions the prov file:\n%s", index)
	}
}

func TestDeleteRemovesIndexEntry(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-local", repo.TypeLocal, "{}")
	for _, v := range []string{"0.1.0", "0.2.0"} {
		chart := fixtureChart(t, "c", defaultChartYAML("c", v), nil)
		if status, body, _ := s.put(fmt.Sprintf("/binflow/helm-local/c-%s.tgz", v), chart, nil); status != http.StatusCreated {
			t.Fatalf("PUT %s = (%d, %s)", v, status, body)
		}
	}
	if status, body, _ := s.delete("/binflow/helm-local/c-0.1.0.tgz"); status != http.StatusNoContent {
		t.Fatalf("DELETE = (%d, %s), want 204", status, body)
	}
	_, index, _ := s.get("/binflow/helm-local/index.yaml")
	if strings.Contains(index, "0.1.0") || !strings.Contains(index, "0.2.0") {
		t.Errorf("index after delete wrong:\n%s", index)
	}
	// The last entry's removal leaves an empty entries map helm can parse.
	s.delete("/binflow/helm-local/c-0.2.0.tgz")
	_, index, _ = s.get("/binflow/helm-local/index.yaml")
	if strings.Contains(index, "c-0.2.0.tgz") {
		t.Errorf("index still carries the last entry:\n%s", index)
	}
	if !strings.Contains(index, "apiVersion: v1") {
		t.Errorf("index lost the skeleton head:\n%s", index)
	}
}

func TestUnparsableTgzStoresWithoutIndexing(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-local", repo.TypeLocal, "{}")
	// A .tar.gz spelling: plain file, never indexed (section 3's rule).
	targz := fixtureChart(t, "c", defaultChartYAML("c", "1.0.0"), nil)
	if status, body, _ := s.put("/binflow/helm-local/c-1.0.0.tar.gz", targz, nil); status != http.StatusCreated {
		t.Fatalf("tar.gz PUT = (%d, %s), want 201 (plain file)", status, body)
	}
	// A .tgz that is not a chart archive at all.
	if status, body, _ := s.put("/binflow/helm-local/notachart-1.0.0.tgz", []byte("not gzip at all"), nil); status != http.StatusCreated {
		t.Fatalf("garbage .tgz PUT = (%d, %s), want 201 (stored, skipped)", status, body)
	}
	// A .tgz whose Chart.yaml lacks name/version.
	nameless := fixtureChart(t, "c", "apiVersion: v2\nversion: 1.0.0\n", nil)
	if status, body, _ := s.put("/binflow/helm-local/c-1.0.0.tgz", nameless, nil); status != http.StatusCreated {
		t.Fatalf("nameless PUT = (%d, %s), want 201 (stored, skipped)", status, body)
	}
	if status, _, _ := s.get("/binflow/helm-local/index.yaml"); status != http.StatusNotFound {
		t.Fatalf("index after only skipped charts = %d, want 404 (nothing indexed)", status)
	}
}

func TestChecksumHeaderChain(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-local", repo.TypeLocal, "{}")
	chart := fixtureChart(t, "c", defaultChartYAML("c", "1.0.0"), nil)
	// Malformed header: 400.
	status, body, _ := s.put("/binflow/helm-local/c-1.0.0.tgz", chart, map[string]string{"X-Checksum-Sha256": "zz"})
	if status != http.StatusBadRequest || !strings.Contains(body, "X-Checksum-Sha256") {
		t.Fatalf("malformed checksum PUT = (%d, %s), want 400 naming the header", status, body)
	}
	// Well-formed but wrong: 409.
	status, body, _ = s.put("/binflow/helm-local/c-1.0.0.tgz", chart,
		map[string]string{"X-Checksum-Sha256": strings.Repeat("a", 64)})
	if status != http.StatusConflict {
		t.Fatalf("mismatched checksum PUT = (%d, %s), want 409", status, body)
	}
	// Matching: 201.
	status, body, _ = s.put("/binflow/helm-local/c-1.0.0.tgz", chart,
		map[string]string{"X-Checksum-Sha256": sha256Hex(chart)})
	if status != http.StatusCreated {
		t.Fatalf("matching checksum PUT = (%d, %s), want 201", status, body)
	}
}

func TestIndexDirectWriteRefused(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-local", repo.TypeLocal, "{}")
	status, body, _ := s.put("/binflow/helm-local/index.yaml", []byte("apiVersion: v1\n"), nil)
	if status != http.StatusForbidden || !strings.Contains(body, "server-generated") {
		t.Fatalf("index PUT = (%d, %s), want the 403 server-generated refusal", status, body)
	}
	if status, _, _ = s.delete("/binflow/helm-local/index.yaml"); status != http.StatusForbidden {
		t.Fatalf("index DELETE = %d, want 403", status)
	}
}

func TestLocalRefusals(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-local", repo.TypeLocal, "{}")
	s.seedRepo(t, "helm-remote-ish", repo.TypeRemote, "{}")
	s.seedRepo(t, "helm-virt", repo.TypeVirtual, "{}")

	// _external/_transitive: the remote family's local 400.
	for _, p := range []string{"/binflow/helm-local/_external/https/example.com/x.tgz", "/binflow/helm-local/_transitive/https/example.com/x.tgz"} {
		if status, body, _ := s.get(p); status != http.StatusBadRequest {
			t.Fatalf("%s = (%d, %s), want 400", p, status, body)
		}
	}
	// The class door: remote/virtual rows answer the not-served refusal.
	for _, p := range []string{"/binflow/helm-remote-ish/index.yaml", "/binflow/helm-virt/index.yaml"} {
		if status, body, _ := s.get(p); status != http.StatusNotFound || !strings.Contains(body, "not served by this BinFlow release") {
			t.Fatalf("%s = (%d, %s), want the class-door 404", p, status, body)
		}
	}
	// The repository-root probe.
	if status, _, _ := s.get("/binflow/helm-local/"); status != http.StatusOK {
		t.Fatalf("repo root = %d, want 200", status)
	}
}

// TestAPIHelmAliasReadOnlyMatrix: the /binflow/api/helm alias serves the
// download face read-only (HL-1) — GET/HEAD answer through the rewrite,
// every write verb answers 405 with Allow before any adapter logic.
func TestAPIHelmAliasReadOnlyMatrix(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-local", repo.TypeLocal, "{}")
	chart := fixtureChart(t, "mychart", defaultChartYAML("mychart", "0.1.0"), nil)
	if status, body, _ := s.put("/binflow/helm-local/mychart-0.1.0.tgz", chart, nil); status != http.StatusCreated {
		t.Fatalf("content-plane PUT = (%d, %s), want 201", status, body)
	}

	status, body, hdr := s.get("/binflow/api/helm/helm-local/index.yaml")
	if status != http.StatusOK || !strings.Contains(body, "mychart") {
		t.Fatalf("alias index GET = (%d, %s), want the index body", status, body)
	}
	if !strings.HasPrefix(hdr.Get("Content-Type"), "text/yaml") {
		t.Errorf("alias index Content-Type = %q", hdr.Get("Content-Type"))
	}
	status, body, _ = s.get("/binflow/api/helm/helm-local/mychart-0.1.0.tgz")
	if status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(chart) {
		t.Fatalf("alias chart GET = (%d, %d bytes), want the stored bytes", status, len(body))
	}
	// HEAD works through the alias too.
	if status, _, _ = s.do(http.MethodHead, "/binflow/api/helm/helm-local/index.yaml", "", "", nil, nil); status != http.StatusOK {
		t.Fatalf("alias HEAD = %d, want 200", status)
	}

	// The write verbs: 405 + Allow, never a second upload entrance.
	for _, spec := range []struct{ method, path string }{
		{http.MethodPut, "/binflow/api/helm/helm-local/other-1.0.0.tgz"},
		{http.MethodDelete, "/binflow/api/helm/helm-local/mychart-0.1.0.tgz"},
		{http.MethodPost, "/binflow/api/helm/helm-local/mychart-0.1.0.tgz"},
	} {
		status, body, hdr := s.do(spec.method, spec.path, adminUser, adminPass, bytesReader(fixtureChart(t, "other", defaultChartYAML("other", "1.0.0"), nil)), nil)
		if status != http.StatusMethodNotAllowed {
			t.Fatalf("alias %s = (%d, %s), want 405", spec.method, status, body)
		}
		if hdr.Get("Allow") != "GET, HEAD" {
			t.Errorf("alias %s Allow = %q", spec.method, hdr.Get("Allow"))
		}
	}
	// Nothing landed through the alias.
	if status, _, _ := s.get("/binflow/helm-local/other-1.0.0.tgz"); status != http.StatusNotFound {
		t.Fatalf("alias PUT leaked a node: status %d, want 404", status)
	}
}

func TestReindexEndpoints(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-local", repo.TypeLocal, "{}")
	s.seedRepo(t, "helm-virt", repo.TypeVirtual, "{}")

	chart1 := fixtureChart(t, "a", defaultChartYAML("a", "1.0.0"), nil)
	chart2 := fixtureChart(t, "b", defaultChartYAML("b", "1.0.0"), nil)
	for path, body := range map[string][]byte{
		"/binflow/helm-local/a-1.0.0.tgz":     chart1,
		"/binflow/helm-local/sub/b-1.0.0.tgz": chart2,
	} {
		if status, b, _ := s.put(path, body, nil); status != http.StatusCreated {
			t.Fatalf("PUT %s = (%d, %s)", path, status, b)
		}
	}

	// Hand-corrupt the index (a service-level write the client plane
	// refuses), then the FULL async reindex rebuilds it from storage.
	stale := "apiVersion: v1\nentries: {}\ngenerated: \"2020-01-01T00:00:00Z\"\n"
	if _, err := s.svc.Put(context.Background(), &auth.Principal{Name: adminUser, Admin: true},
		"helm-local", "index.yaml", strings.NewReader(stale), storage.BlobRef{Sha256: sha256Hex([]byte(stale))}, "text/yaml"); err != nil {
		t.Fatalf("seed stale index: %v", err)
	}
	if status, body, _ := s.post("/binflow/api/helm/helm-local/reindex"); status != http.StatusOK {
		t.Fatalf("full reindex = (%d, %s), want 200", status, body)
	}
	waitFor(t, func() bool {
		_, body, _ := s.get("/binflow/helm-local/index.yaml")
		return strings.Contains(body, "a-1.0.0.tgz") && strings.Contains(body, "sub/b-1.0.0.tgz")
	}, "full reindex rebuilt both entries")

	// The partial reindex is SYNCHRONOUS: seed a stale entry whose node is
	// gone, then reindex its directory — the urls-prefix removal drops it,
	// the rescan finds nothing to re-add.
	orphan := fixtureChart(t, "gone", defaultChartYAML("gone", "5.0.0"), nil)
	staleEntry := "apiVersion: v1\nentries:\n  gone:\n  - name: gone\n    version: \"5.0.0\"\n    digest: " + sha256Hex(orphan) + "\n    urls:\n    - sub/gone-5.0.0.tgz\n"
	if _, err := s.svc.Put(context.Background(), &auth.Principal{Name: adminUser, Admin: true},
		"helm-local", "index.yaml", strings.NewReader(staleEntry), storage.BlobRef{Sha256: sha256Hex([]byte(staleEntry))}, "text/yaml"); err != nil {
		t.Fatalf("seed orphan entry: %v", err)
	}
	if status, body, _ := s.post("/binflow/api/helm/helm-local/reindex/sub"); status != http.StatusOK {
		t.Fatalf("partial reindex = (%d, %s), want 200", status, body)
	}
	_, body, _ := s.get("/binflow/helm-local/index.yaml")
	if strings.Contains(body, "gone-5.0.0.tgz") || !strings.Contains(body, "sub/b-1.0.0.tgz") {
		t.Fatalf("partial reindex left the wrong slice:\n%s", body)
	}

	// The single-package arm re-adds one path.
	chartB2 := fixtureChart(t, "b", defaultChartYAML("b", "2.0.0"), nil)
	if status, body, _ := s.put("/binflow/helm-local/sub/b-2.0.0.tgz", chartB2, nil); status != http.StatusCreated {
		t.Fatalf("PUT b2 = (%d, %s)", status, body)
	}
	if status, body, _ := s.post("/binflow/api/helm/helm-local/reindex/sub/b-2.0.0.tgz"); status != http.StatusOK {
		t.Fatalf("single reindex = (%d, %s), want 200", status, body)
	}
	_, body, _ = s.get("/binflow/helm-local/index.yaml")
	if !strings.Contains(body, "sub/b-2.0.0.tgz") {
		t.Fatalf("single reindex did not re-add the entry:\n%s", body)
	}

	// A missing single path: the honest 404.
	if status, _, _ := s.post("/binflow/api/helm/helm-local/reindex/nope-9.9.9.tgz"); status != http.StatusNotFound {
		t.Fatalf("missing-path reindex = %d, want 404", status)
	}
	// The class door on the management plane.
	if status, body, _ := s.post("/binflow/api/helm/helm-virt/reindex"); status != http.StatusBadRequest {
		t.Fatalf("virtual reindex = (%d, %s), want 400", status, body)
	}
	// A repository of another package type.
	status, body, _ := s.post("/binflow/api/helm/helm-local/reindex/sub")
	if status != http.StatusOK {
		t.Fatalf("sanity re-reindex = (%d, %s), want 200", status, body)
	}
}

// waitFor polls cond until it holds (the async reindex's observable
// completion).
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	// T-311 note: the poll yields between probes — a no-sleep busy loop
	// raced the async reindex goroutine off the CPU under a full-suite
	// parallel load and flaked here. 200 × 25ms bounds the wait at 5s
	// while letting the background work actually run.
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

// TestConcurrentUploadsIndexConsistency: N versions of one chart uploaded
// concurrently — the final index carries exactly N entries, one per
// version (the per-repo lock's lost-update guard; the cargo B1 posture).
func TestConcurrentUploadsIndexConsistency(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-local", repo.TypeLocal, "{}")
	const n = 8
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v := fmt.Sprintf("0.%d.0", i)
			chart := fixtureChart(t, "mychart", defaultChartYAML("mychart", v), nil)
			if status, body, _ := s.put("/binflow/helm-local/mychart-"+v+".tgz", chart, nil); status != http.StatusCreated {
				t.Errorf("concurrent PUT %s = (%d, %s)", v, status, body)
			}
		}(i)
	}
	wg.Wait()
	_, body, _ := s.get("/binflow/helm-local/index.yaml")
	count := strings.Count(body, "digest: ")
	if count != n {
		t.Fatalf("index carries %d entries after %d concurrent uploads:\n%s", count, n, body)
	}
}
