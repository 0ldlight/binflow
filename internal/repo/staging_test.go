package repo

// Unit pins for the upload-staging twin primitives (T-477): stagingRoot's
// resolve-or-create contract (the adapter spool family's semantics, held
// on the service side of the one-way dependency) and stagingLabel's
// bounded-disclosure rendering. The end-to-end explode matrix lives in
// explode_staging_test.go (package repo_test).

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStagingRootResolvesAndCreatesOnDemand(t *testing.T) {
	// The OS-temp fallback arm: "" resolves to os.TempDir().
	t.Run("os temp fallback", func(t *testing.T) {
		root, err := stagingRoot("")
		if err != nil {
			t.Fatalf("stagingRoot(\"\"): %v", err)
		}
		if root != os.TempDir() {
			t.Fatalf("root = %q, want os.TempDir() %q", root, os.TempDir())
		}
	})
	// The configured arm: a missing root is created (0o700), an existing
	// one passes through untouched.
	t.Run("configured root created on demand", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "a", "b", "staging")
		got, err := stagingRoot(root)
		if err != nil {
			t.Fatalf("stagingRoot(%s): %v", root, err)
		}
		if got != root {
			t.Fatalf("root = %q, want %q", got, root)
		}
		fi, err := os.Stat(root)
		if err != nil || !fi.IsDir() {
			t.Fatalf("root %s not created: %v", root, err)
		}
		// The second call over the now-existing root is a no-op success.
		if _, err := stagingRoot(root); err != nil {
			t.Fatalf("stagingRoot(existing): %v", err)
		}
	})
}

func TestStagingRootRefusalStillNamesTheRoot(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory write bits are not enforced for this runner; the read-only stand-in cannot bite")
	}
	ro := t.TempDir()
	if err := os.Chmod(ro, 0o444); err != nil {
		t.Fatalf("chmod 0444 %s: %v", ro, err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) }) // before t.TempDir's own cleanup (LIFO)

	root := filepath.Join(ro, "staging")
	got, err := stagingRoot(root)
	if err == nil {
		t.Fatalf("stagingRoot under a read-only parent succeeded (root %q)", got)
	}
	// The refusal still returns the RESOLVED root — the caller's 507 face
	// and log name it (the diagnosability contract).
	if got != root {
		t.Fatalf("refusal root = %q, want %q", got, root)
	}
}

func TestStagingLabelRendersBoundedDisclosure(t *testing.T) {
	tests := []struct {
		dir  string
		want string
	}{
		{dir: "", want: "the OS temp directory"},
		{dir: "/data/staging", want: fmt.Sprintf("%q", "/data/staging")},
	}
	for _, tt := range tests {
		if got := stagingLabel(tt.dir); got != tt.want {
			t.Errorf("stagingLabel(%q) = %q, want %q", tt.dir, got, tt.want)
		}
	}
}
