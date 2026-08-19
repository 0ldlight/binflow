package npm

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
)

// TestConcurrentPublishesMergeAllVersions: N goroutines publish N different
// versions of one package concurrently; the serialized read-modify-write must
// land every version in the packument (a lost merge would surface as a
// missing version or a 404 packument).
func TestConcurrentPublishesMergeAllVersions(t *testing.T) {
	s := newStack(t)
	const n = 8

	var wg sync.WaitGroup
	errs := make([]int, n) // response status per publisher, 0 = not run
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			version := "1.0." + string(rune('0'+i))
			doc := publishDoc("demo-pkg", version, "TARBALL-"+version, nil, nil)
			rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(doc), adminPrincipal, nil)
			errs[i] = rr.Code
		}(i)
	}
	wg.Wait()

	for i, code := range errs {
		if code != http.StatusCreated {
			t.Fatalf("publisher %d status = %d, want 201", i, code)
		}
	}

	rr := s.call(http.MethodGet, "/npm-local/demo-pkg", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("packument status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("packument not JSON: %v", err)
	}
	versions := doc["versions"].(map[string]any)
	if len(versions) != n {
		t.Fatalf("packument holds %d versions, want %d (a concurrent merge was lost): %s",
			len(versions), n, bodyOf(rr))
	}
}
