// LOOP 012 L012-3: the recursive property-write arm's per-node
// mdTimestamps.properties behavior, pinned to live reference evidence taken
// on :8082 (T-L011-1 Outputs E1-E4, re-probed for this ticket): a recursive
// folder write moves EVERY child's props-mtime to the write time (E1), a
// second recursive write moves them again (E2), a direct write moves only
// the one child (E3), and a value-set-identical re-PUT or a delete of keys
// nobody carries parks every mtime — the row (and the audit-derived mtime)
// follows the mutation, not the request (E4). The wildcard-miss delete —
// which used to fall into the store's empty "delete everything" form and
// wipe the node's properties — keeps them (reference: 204, untouched).
package httpapi_test

import (
	"net/http"
	"testing"
	"time"
)

// mdPropsOf reads one entry's mdTimestamps.properties off a deep listing of
// repo+path; "" when the entry carries none.
func mdPropsOf(t *testing.T, h *harness, repo, path, uri string) string {
	t.Helper()
	_, lb := getL009List(t, h, repo+path+"?list&deep=1&listFolders=1&mdTimestamps=1")
	return entryOf(t, lb, uri).MDTimestamps["properties"]
}

// crossSecond waits past the audit log's second-resolution clock so a
// moved-mtime assertion can compare strictly.
func crossSecond() { time.Sleep(1100 * time.Millisecond) }

func TestPropsRecursiveWriteMovesChildMtimes(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "l0123-loc")
	putContent(t, h, "/binflow/l0123-loc/rel/x.bin", "x")
	putContent(t, h, "/binflow/l0123-loc/rel/sub/y.bin", "y")

	const base = "/binflow/api/storage/l0123-loc/rel"
	const repoPath = "l0123-loc/rel"
	entries := []string{"/x.bin", "/sub", "/sub/y.bin"}
	readAll := func() map[string]string {
		out := map[string]string{}
		for _, uri := range entries {
			out[uri] = mdPropsOf(t, h, repoPath, "", uri)
		}
		return out
	}

	// E1: one recursive folder write puts mdTimestamps.properties on every
	// child — file rows, nested folder rows, and deep file rows alike (the
	// pre-fix BinFlow recorded only the addressed node's audit row, so the
	// children's key was absent even though their properties were written).
	doProps(t, h, http.MethodPut, base+"?properties=pk=base&recursive=1", adminUser, adminPass)
	m1 := readAll()
	for _, uri := range entries {
		if m1[uri] == "" {
			t.Errorf("E1 %s: mdTimestamps.properties missing after recursive write: %+v", uri, m1)
		}
	}

	// E2: a second recursive write moves every child's mtime again.
	crossSecond()
	doProps(t, h, http.MethodPut, base+"?properties=pk2=second&recursive=1", adminUser, adminPass)
	m2 := readAll()
	for _, uri := range entries {
		if m2[uri] <= m1[uri] {
			t.Errorf("E2 %s: mtime did not move on second recursive write: %q -> %q", uri, m1[uri], m2[uri])
		}
	}

	// E3: a direct write moves only the addressed child; siblings park.
	crossSecond()
	doProps(t, h, http.MethodPut, "/binflow/api/storage/l0123-loc/rel/x.bin?properties=solo=only",
		adminUser, adminPass)
	m3 := readAll()
	if m3["/x.bin"] <= m2["/x.bin"] {
		t.Errorf("E3 /x.bin: direct write did not move its mtime: %q -> %q", m2["/x.bin"], m3["/x.bin"])
	}
	for _, uri := range []string{"/sub", "/sub/y.bin"} {
		if m3[uri] != m2[uri] {
			t.Errorf("E3 %s: direct write on a sibling moved it: %q -> %q", uri, m2[uri], m3[uri])
		}
	}

	// E4a: a value-set-identical recursive re-PUT parks every mtime.
	crossSecond()
	doProps(t, h, http.MethodPut, base+"?properties=pk2=second&recursive=1", adminUser, adminPass)
	m4 := readAll()
	for _, uri := range entries {
		if m4[uri] != m3[uri] {
			t.Errorf("E4a %s: no-op recursive re-PUT moved mtime: %q -> %q", uri, m3[uri], m4[uri])
		}
	}

	// E4b: a recursive delete of a key nobody carries parks every mtime.
	doProps(t, h, http.MethodDelete, base+"?properties=nosuchkey&recursive=1", adminUser, adminPass)
	if got := readAll(); got["/x.bin"] != m4["/x.bin"] || got["/sub"] != m4["/sub"] {
		t.Errorf("E4b: no-op recursive delete moved mtimes: %+v (was %+v)", got, m4)
	}

	// E4c: a recursive delete of a really-carried key moves every mtime.
	crossSecond()
	doProps(t, h, http.MethodDelete, base+"?properties=pk2&recursive=1", adminUser, adminPass)
	m5 := readAll()
	for _, uri := range entries {
		if m5[uri] <= m4[uri] {
			t.Errorf("E4c %s: real recursive delete did not move mtime: %q -> %q", uri, m4[uri], m5[uri])
		}
	}

	// Wiping every property drops the key outright: property-less entries
	// carry no mdTimestamps at all.
	doProps(t, h, http.MethodDelete, base+"?properties=*&recursive=1", adminUser, adminPass)
	for _, uri := range entries {
		if got := mdPropsOf(t, h, repoPath, "", uri); got != "" {
			t.Errorf("full wipe: %s still carries mdTimestamps.properties %q", uri, got)
		}
	}
}

// TestPropsDeleteWildcardMissKeepsProperties: a wildcard that matches none
// of the node's keys drops NOTHING — the empty resolved set must never fall
// into the store's "drop everything" form (pre-fix it wiped every property
// of the node and, recursively, of every child; reference behavior: 204
// with the properties untouched).
func TestPropsDeleteWildcardMissKeepsProperties(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "l0123-loc")
	putContent(t, h, "/binflow/l0123-loc/w.bin", "w")
	putContent(t, h, "/binflow/l0123-loc/wd/inner.bin", "i")

	const base = "/binflow/api/storage/l0123-loc"
	doProps(t, h, http.MethodPut, base+"/w.bin?properties=keep=1,other=2", adminUser, adminPass)
	doProps(t, h, http.MethodPut, base+"/wd/inner.bin?properties=keep=1", adminUser, adminPass)

	if s := doProps(t, h, http.MethodDelete, base+"/w.bin?properties=nosuch*", adminUser, adminPass); s != http.StatusNoContent {
		t.Fatalf("direct wildcard-miss DELETE status = %d", s)
	}
	_, p := getProps(t, h, base+"/w.bin?properties", adminUser, adminPass)
	if p["keep"] == nil || p["other"] == nil {
		t.Fatalf("direct wildcard-miss wiped properties: %+v", p)
	}

	if s := doProps(t, h, http.MethodDelete, base+"/wd?properties=zzz*&recursive=1", adminUser, adminPass); s != http.StatusNoContent {
		t.Fatalf("recursive wildcard-miss DELETE status = %d", s)
	}
	_, p = getProps(t, h, base+"/wd/inner.bin?properties", adminUser, adminPass)
	if p["keep"] == nil {
		t.Fatalf("recursive wildcard-miss wiped child properties: %+v", p)
	}
}
