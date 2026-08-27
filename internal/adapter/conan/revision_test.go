package conan

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The revision-index legs: .timestamp first-write-wins (TL-3), the
// index.json isomorphism (S2), the latest-ordering rule and the
// concurrency discipline (the cargo B1 shape).

// TestTimestampFirstWriteWins: re-uploading into a revision NEVER moves
// its .timestamp or its index time (TL-3) — the marker is the revision's
// birth mark, and no later write refreshes it.
func TestTimestampFirstWriteWins(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	revA, revB := fixtureRev(1), fixtureRev(7)

	if code, body, _ := s.putRecipeFile("cn-local", r, revA, "conanfile.py", []byte("a")); code != http.StatusCreated {
		t.Fatalf("first PUT = %d (body %s)", code, body)
	}
	_, first, _ := s.get(v2("cn-local", "hello/1.0/myuser/stable/latest"))
	if strings.Contains(first, revB) == false && first == "" {
		t.Fatalf("latest after first PUT = %q", first)
	}

	// A second revision becomes latest (newer birth)…
	if code, _, _ := s.putRecipeFile("cn-local", r, revB, "conanfile.py", []byte("b")); code != http.StatusCreated {
		t.Fatalf("second PUT = %d", code)
	}
	_, latest, _ := s.get(v2("cn-local", "hello/1.0/myuser/stable/latest"))
	if !strings.Contains(latest, revB) {
		t.Fatalf("latest after second PUT = %q, want revB", latest)
	}

	// …and re-uploading revA's bytes must NOT reclaim latest (TL-3: the
	// birth mark is frozen).
	if code, _, _ := s.putRecipeFile("cn-local", r, revA, "conanfile.py", []byte("a2")); code != http.StatusCreated {
		t.Fatalf("re-PUT = %d", code)
	}
	_, latest, _ = s.get(v2("cn-local", "hello/1.0/myuser/stable/latest"))
	if !strings.Contains(latest, revB) {
		t.Fatalf("latest after re-PUT = %q, want revB (TL-3: first write wins)", latest)
	}

	// The index time of revA is unchanged too.
	_, body, _ := s.get(v2("cn-local", "hello/1.0/myuser/stable/revisions"))
	var doc recipeIndexDoc
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("revisions body %q: %v", body, err)
	}
	var timeA string
	for _, e := range doc.Revisions {
		if e.Revision == revA {
			timeA = e.Time
		}
	}
	if timeA == "" {
		t.Fatalf("revA missing from %s", body)
	}
	if code, _, _ := s.putRecipeFile("cn-local", r, revA, "conanmanifest.txt", []byte("m")); code != http.StatusCreated {
		t.Fatalf("manifest PUT = %d", code)
	}
	_, body, _ = s.get(v2("cn-local", "hello/1.0/myuser/stable/revisions"))
	_ = json.Unmarshal([]byte(body), &doc)
	for _, e := range doc.Revisions {
		if e.Revision == revA && e.Time != timeA {
			t.Fatalf("revA time moved: %q -> %q (TL-3 violated)", timeA, e.Time)
		}
	}

	// The .timestamp node itself still carries the ORIGINAL ms epoch.
	ctx := context.Background()
	p := adminPrincipal()
	ts, ok, err := s.handlerForTest().readTimestamp(ctx, p, "cn-local", recipeTimestamp(r.coordinateRoot(), revA))
	if err != nil || !ok {
		t.Fatalf("read .timestamp: ok=%v err=%v", ok, err)
	}
	if code, _, _ := s.putRecipeFile("cn-local", r, revA, "conan_sources.tgz", []byte("src")); code != http.StatusCreated {
		t.Fatalf("sources PUT = %d", code)
	}
	ts2, _, _ := s.handlerForTest().readTimestamp(ctx, p, "cn-local", recipeTimestamp(r.coordinateRoot(), revA))
	if ts != ts2 {
		t.Fatalf(".timestamp changed: %q -> %q (TL-3 violated)", ts, ts2)
	}
}

// TestIndexIsomorphism: the revisions response and the stored index.json
// node are the SAME document (S2) — reading the node through the content
// plane's storage face equals the endpoint's body modulo the read-sort.
func TestIndexIsomorphism(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	r := ref{name: "iso", version: "2.0", user: "u", channel: "c"}
	for _, rev := range []string{fixtureRev(1), fixtureRev(4), fixtureRev(9)} {
		s.putRecipeFile("cn-local", r, rev, "conanfile.py", []byte("x"+rev))
	}

	code, body, _ := s.get(v2("cn-local", "iso/2.0/u/c/revisions"))
	if code != http.StatusOK {
		t.Fatalf("revisions = %d", code)
	}
	rc, _, err := s.svc.Get(context.Background(), adminPrincipal(), "cn-local", recipeIndex(r.coordinateRoot()))
	if err != nil {
		t.Fatalf("read index node: %v", err)
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	buf := make([]byte, 4096)
	n, _ := rc.Read(buf)

	var fromWire, fromStore recipeIndexDoc
	if err := json.Unmarshal([]byte(body), &fromWire); err != nil {
		t.Fatalf("wire body: %v", err)
	}
	if err := json.Unmarshal(buf[:n], &fromStore); err != nil {
		t.Fatalf("stored body: %v", err)
	}
	if len(fromWire.Revisions) != len(fromStore.Revisions) || fromWire.Reference != fromStore.Reference {
		t.Fatalf("isomorphism broken: wire %+v vs store %+v", fromWire, fromStore)
	}
	for i := range fromWire.Revisions {
		if fromWire.Revisions[i] != fromStore.Revisions[i] {
			t.Errorf("row %d: wire %+v vs store %+v", i, fromWire.Revisions[i], fromStore.Revisions[i])
		}
	}
	// Time-descending, ties by revision descending.
	for i := 1; i < len(fromStore.Revisions); i++ {
		if fromStore.Revisions[i-1].Time < fromStore.Revisions[i].Time {
			t.Fatalf("index not time-descending: %+v", fromStore.Revisions)
		}
	}
}

// TestConcurrentUploadsNoLostRows: N goroutines upload N distinct files
// into the SAME (fresh) revision concurrently; the index must carry the
// revision exactly once and every file must survive (the cargo B1
// discipline's conan shape).
func TestConcurrentUploadsNoLostRows(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	r := ref{name: "race", version: "1.0", user: "u", channel: "c"}
	rev := fixtureRev(5)
	const n = 12

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code, body, _ := s.putRecipeFile("cn-local", r, rev, "file"+string(rune('a'+i))+".txt", []byte("x"))
			if code != http.StatusCreated {
				errs <- &httpError{code: code, body: body}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent PUT: %v", err)
	}

	code, body, _ := s.get(v2("cn-local", "race/1.0/u/c/revisions"))
	if code != http.StatusOK {
		t.Fatalf("revisions = %d (body %s)", code, body)
	}
	var doc recipeIndexDoc
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("revisions body: %v", err)
	}
	count := 0
	for _, e := range doc.Revisions {
		if e.Revision == rev {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("revision appears %d times, want exactly 1 (lost-update guard failed)", count)
	}

	_, files, _ := s.get(v2("cn-local", "race/1.0/u/c/revisions/"+rev+"/files"))
	var list filesResponse
	if err := json.Unmarshal([]byte(files), &list); err != nil {
		t.Fatalf("files body: %v", err)
	}
	if len(list.Files) != n {
		t.Fatalf("files = %d entries, want %d (rows lost)", len(list.Files), n)
	}
}

// httpError carries a failed request's facts through the error channel.
type httpError struct {
	code int
	body string
}

func (e *httpError) Error() string {
	return strings.TrimSpace(e.body)
}

// TestPackageIndexConcurrency: the package plane's own registration races
// — two pRevs of one pid landing concurrently both survive.
func TestPackageIndexConcurrency(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	r := ref{name: "prace", version: "1.0", user: "u", channel: "c"}
	rev, pid := fixtureRev(2), fixturePID(3)
	s.putRecipeFile("cn-local", r, rev, "conanfile.py", []byte("x"))

	var wg sync.WaitGroup
	for _, prev := range []string{fixtureRev(6), fixtureRev(8)} {
		wg.Add(1)
		go func(prev string) {
			defer wg.Done()
			s.putPkgFile("cn-local", r, rev, pid, prev, "conaninfo.txt", []byte("i"+prev))
		}(prev)
	}
	wg.Wait()

	pkgBase := v2("cn-local", "prace/1.0/u/c/revisions/"+rev+"/packages/"+pid)
	code, body, _ := s.get(pkgBase + "/revisions")
	if code != http.StatusOK {
		t.Fatalf("pkg revisions = %d (body %s)", code, body)
	}
	var doc pkgIndexDoc
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("pkg revisions body: %v", err)
	}
	if len(doc.Revisions) != 2 {
		t.Fatalf("pkg revisions = %d entries, want 2 (a row was lost)", len(doc.Revisions))
	}
}
