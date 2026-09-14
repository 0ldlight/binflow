package pypi

// L017-1 D7: index admission for invalid wheels — the L016 three-state
// matrix wire. A wheel whose metadata cannot be located (garbage bytes, or
// a valid zip with no *.dist-info/METADATA member) is STORED but never
// INDEXED, matching the reference's index-integrity posture; bad-name and
// bad-version wheels carry self-consistent METADATA and stay indexed on
// both ends alike; a metadata-less sdist is an un-evidenced face and keeps
// its entry. Upload answers stay 200 for every state (admission is an
// index-side verdict, not an upload rejection).

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// uploadState posts one three-state-matrix arm: good form fields, the file
// payload as given (curl shape — twine 7 rejects these client-side).
func (s *stack) uploadState(t *testing.T, filename string, content []byte) (int, string) {
	t.Helper()
	return s.upload("/binflow/api/pypi/pypi-local", map[string]string{
		":action": "file_upload", "name": "demo-pkg", "version": "1.0.0",
		"filetype": "bdist_wheel", "protocol_version": "1",
	}, filename, content)
}

// nodeExists asserts the STORED half of "stored but not indexed".
func (s *stack) nodeExists(t *testing.T, path string) {
	t.Helper()
	if _, err := s.md.Nodes().Get(context.Background(), "pypi-local", path); err != nil {
		t.Fatalf("node %s not stored: %v", path, err)
	}
}

func TestBadmetaWheelAdmission(t *testing.T) {
	s := newStack(t)

	cases := []struct {
		name      string
		filename  string
		content   []byte
		indexed   bool
		wantAttrs string // "" = no data-requires-python expected
	}{
		{
			// b1 arm: filename carries another project's name, METADATA
			// self-consistent without Requires-Python. D6 (index KEY source)
			// stays undecided; BinFlow keeps indexing under the form name.
			name:      "badname stays indexed (matrix parity)",
			filename:  "notdemo-1.0.0-py3-none-any.whl",
			content:   testWheelBytes(t, "notdemo-1.0.0", "", true),
			indexed:   true,
			wantAttrs: "",
		},
		{
			// b2 arm: valid zip, NO METADATA member — the D7 divergence:
			// stored, refused by the index.
			name:     "badmeta stored but not indexed",
			filename: "nometa-1.0.0-py3-none-any.whl",
			content:  testWheelBytes(t, "nometa-1.0.0", ">=3.8", false),
			indexed:  false,
		},
		{
			// L015 P4 arm: garbage bytes are not a zip — same refusal.
			name:     "garbage wheel stored but not indexed",
			filename: "garbage-1.0.0-py3-none-any.whl",
			content:  []byte("not a zip at all"),
			indexed:  false,
		},
		{
			// b3 arm: filename version abc, METADATA self-consistent — both
			// ends index it (no server-side PEP 440 validation).
			name:      "badver stays indexed (matrix parity)",
			filename:  "demo_pkg-abc-py3-none-any.whl",
			content:   testWheelBytes(t, "demo_pkg-abc", "", true),
			indexed:   true,
			wantAttrs: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if status, body := s.uploadState(t, tc.filename, tc.content); status != http.StatusOK {
				t.Fatalf("upload = %d, body %s (every state answers 200)", status, body)
			}
			s.nodeExists(t, "demo-pkg/1.0.0/"+tc.filename)

			_, page, _ := s.get("/binflow/api/pypi/pypi-local/simple/demo-pkg/")
			if got := strings.Contains(page, tc.filename); got != tc.indexed {
				t.Fatalf("index presence = %v, want %v; page:\n%s", got, tc.indexed, page)
			}
			if tc.wantAttrs != "" && !strings.Contains(page, tc.wantAttrs) {
				t.Fatalf("indexed entry lacks %q:\n%s", tc.wantAttrs, page)
			}
		})
	}

	// The refused wheels must not leave the project page empty (the good
	// badname/badver entries carry it) — and a project whose ONLY wheel is
	// metadata-less answers 404 (nothing indexable).
	_, page, _ := s.get("/binflow/api/pypi/pypi-local/simple/demo-pkg/")
	if !strings.Contains(page, "notdemo-1.0.0-py3-none-any.whl") ||
		!strings.Contains(page, "demo_pkg-abc-py3-none-any.whl") {
		t.Fatalf("indexed states missing from the page:\n%s", page)
	}
	if strings.Contains(page, "nometa-1.0.0-py3-none-any.whl") ||
		strings.Contains(page, "garbage-1.0.0-py3-none-any.whl") {
		t.Fatalf("refused states leaked into the page:\n%s", page)
	}

	// Download face unaffected: a refused wheel still downloads through
	// both entrances (stored, just not indexed).
	for _, p := range []string{
		"/binflow/api/pypi/pypi-local/packages/demo-pkg/1.0.0/nometa-1.0.0-py3-none-any.whl",
		"/binflow/pypi-local/demo-pkg/1.0.0/nometa-1.0.0-py3-none-any.whl",
	} {
		if status, _, _ := s.get(p); status != http.StatusOK {
			t.Fatalf("refused-wheel download %s = %d, want 200 (stored content)", p, status)
		}
	}
}

// TestBadmetaOnlyWheel404: a project whose only distribution is a
// metadata-less wheel has NO index page at all (the entry would be its
// sole content).
func TestBadmetaOnlyWheel404(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "only-bad", "1.0.0", "only_bad-1.0.0-py3-none-any.whl",
		testWheelBytes(t, "only_bad-1.0.0", ">=3.8", false))
	s.nodeExists(t, "only-bad/1.0.0/only_bad-1.0.0-py3-none-any.whl")

	if status, _, _ := s.get("/binflow/api/pypi/pypi-local/simple/only-bad/"); status != http.StatusNotFound {
		t.Fatalf("metadata-only-wheel project page = %d, want 404", status)
	}
	// And the root index carries no entry for it either.
	if _, root, _ := s.get("/binflow/api/pypi/pypi-local/simple/"); strings.Contains(root, "only-bad") {
		t.Fatalf("root index carries the refused project:\n%s", root)
	}
}

// TestSdistWithoutPkgInfoStaysIndexed: the evidence-bounded asymmetry — a
// metadata-less SDIST keeps its index entry (no arm ever evidenced an
// sdist admission rule; only wheels have one).
func TestSdistWithoutPkgInfoStaysIndexed(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0.tar.gz",
		testSdistBytes(t, "demo_pkg-1.0.0", "demo_pkg", "", false))

	status, page, _ := s.get("/binflow/api/pypi/pypi-local/simple/demo-pkg/")
	if status != http.StatusOK || !strings.Contains(page, "demo_pkg-1.0.0.tar.gz") {
		t.Fatalf("metadata-less sdist lost its entry (status %d):\n%s", status, page)
	}
	if strings.Contains(page, "data-requires-python") {
		t.Fatalf("no PKG-INFO means no attribute:\n%s", page)
	}
}
