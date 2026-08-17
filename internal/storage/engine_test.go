package storage

import (
	"bytes"
	"context"
	"crypto/md5"  //nolint:gosec // test digest comparison
	"crypto/sha1" //nolint:gosec // test digest comparison
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newEngine opens an engine in a fresh temp dir and registers cleanup.
func newEngine(t *testing.T, opts Options) Engine {
	t.Helper()
	root := t.TempDir()
	return newEngineAt(t, root, opts)
}

func newEngineAt(t *testing.T, root string, opts Options) Engine {
	t.Helper()
	eng, err := OpenEngine(root, opts)
	if err != nil {
		t.Fatalf("OpenEngine(%s): %v", root, err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

// put uploads content in one shot and returns the Commit result.
func put(t *testing.T, eng Engine, content []byte) BlobRef {
	t.Helper()
	return putExpect(t, eng, content, BlobRef{})
}

func putExpect(t *testing.T, eng Engine, content []byte, expect BlobRef) BlobRef {
	t.Helper()
	s, err := eng.BeginSession(context.Background())
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if _, err := s.Append(context.Background(), bytes.NewReader(content)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	ref, err := s.Commit(context.Background(), expect)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return ref
}

// countBlobs walks <root>/blobs and counts regular files.
func countBlobs(t *testing.T, root string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(filepath.Join(root, "blobs"), func(_ string, d os.DirEntry, err error) error {
		if err == nil && d != nil && d.Type().IsRegular() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk blobs: %v", err)
	}
	return n
}

func countSessionDirs(t *testing.T, root string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "sessions"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read sessions dir: %v", err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	return n
}

func wantBlobPath(t *testing.T, root, sha256 string) string {
	t.Helper()
	if !validSha256(sha256) {
		t.Fatalf("bad sha256 %q in test", sha256)
	}
	return filepath.Join(root, "blobs", sha256[:2], sha256)
}

func TestPutLayoutAndDigests(t *testing.T) {
	root := t.TempDir()
	eng := newEngineAt(t, root, Options{})
	content := []byte("binflow storage engine")
	ref := put(t, eng, content)

	sha256Want := fmt.Sprintf("%x", sha256.Sum256(content))
	sha1Want := fmt.Sprintf("%x", sha1.Sum(content))
	md5Want := fmt.Sprintf("%x", md5.Sum(content)) // test digest comparison
	if ref.Sha256 != sha256Want || ref.Sha1 != sha1Want || ref.Md5 != md5Want {
		t.Fatalf("digests = %+v, want sha256=%s sha1=%s md5=%s", ref, sha256Want, sha1Want, md5Want)
	}
	if ref.Size != int64(len(content)) {
		t.Fatalf("size = %d, want %d", ref.Size, len(content))
	}

	// Layout: blobs/<xx>/<sha256>, content-addressed.
	p := wantBlobPath(t, root, ref.Sha256)
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("blob file missing at %s: %v", p, err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("blob content mismatch: %q", got)
	}

	// Session directory is gone after a successful commit.
	if n := countSessionDirs(t, root); n != 0 {
		t.Fatalf("session dirs after commit = %d, want 0", n)
	}

	// Stat re-derives all three digests from disk.
	st, err := eng.Stat(context.Background(), ref.Sha256)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if st != (BlobRef{Sha256: sha256Want, Sha1: sha1Want, Md5: md5Want, Size: int64(len(content))}) {
		t.Fatalf("Stat = %+v", st)
	}
}

func TestCommitIdempotentDedup(t *testing.T) {
	root := t.TempDir()
	eng := newEngineAt(t, root, Options{})
	content := []byte("same bytes, two commits")

	r1 := put(t, eng, content)
	r2 := put(t, eng, content)
	if r1 != r2 {
		t.Fatalf("two commits of same content disagree: %+v vs %+v", r1, r2)
	}
	if n := countBlobs(t, root); n != 1 {
		t.Fatalf("physical blobs = %d, want 1 (dedup)", n)
	}
	if n := countSessionDirs(t, root); n != 0 {
		t.Fatalf("session dirs = %d, want 0", n)
	}

	// Commit with correct expectation hits the existing blob too.
	r3 := putExpect(t, eng, content, r1)
	if r3 != r1 {
		t.Fatalf("expected-digest commit = %+v, want %+v", r3, r1)
	}
	if n := countBlobs(t, root); n != 1 {
		t.Fatalf("physical blobs after dedup commit = %d, want 1", n)
	}
}

func TestCommitChecksumMismatchRejected(t *testing.T) {
	tests := []struct {
		name   string
		expect BlobRef
	}{
		{name: "sha256 mismatch", expect: BlobRef{Sha256: strings.Repeat("0", 64)}},
		{name: "sha1 mismatch", expect: BlobRef{Sha1: strings.Repeat("0", 40)}},
		{name: "md5 mismatch", expect: BlobRef{Md5: strings.Repeat("0", 32)}},
		{name: "sha256 wrong length", expect: BlobRef{Sha256: "abc"}},
		{name: "sha256 not hex", expect: BlobRef{Sha256: strings.Repeat("z", 64)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			eng := newEngineAt(t, root, Options{})
			s, err := eng.BeginSession(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Append(context.Background(), strings.NewReader("payload")); err != nil {
				t.Fatal(err)
			}
			_, err = s.Commit(context.Background(), tt.expect)
			if !errors.Is(err, ErrChecksumMismatch) {
				t.Fatalf("Commit error = %v, want ErrChecksumMismatch", err)
			}
			if n := countBlobs(t, root); n != 0 {
				t.Fatalf("blobs on disk after mismatch = %d, want 0", n)
			}
			if n := countSessionDirs(t, root); n != 0 {
				t.Fatalf("session dirs after failed commit = %d, want 0", n)
			}
		})
	}
}

func TestCommitAcceptsUppercaseExpectation(t *testing.T) {
	eng := newEngine(t, Options{})
	content := []byte("uppercase client")
	ref := put(t, eng, content)
	up := BlobRef{Sha256: strings.ToUpper(ref.Sha256), Sha1: strings.ToUpper(ref.Sha1), Md5: strings.ToUpper(ref.Md5)}
	if _, err := putExpect2(eng, content, up); err != nil {
		t.Fatalf("uppercase expectation rejected: %v", err)
	}
}

// putExpect2 is putExpect without the test-fatal helper (returns the error).
func putExpect2(eng Engine, content []byte, expect BlobRef) (BlobRef, error) {
	s, err := eng.BeginSession(context.Background())
	if err != nil {
		return BlobRef{}, err
	}
	if _, err := s.Append(context.Background(), bytes.NewReader(content)); err != nil {
		return BlobRef{}, err
	}
	return s.Commit(context.Background(), expect)
}

func TestAbortZeroResidue(t *testing.T) {
	root := t.TempDir()
	eng := newEngineAt(t, root, Options{})
	s, err := eng.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(context.Background(), strings.NewReader("doomed upload")); err != nil {
		t.Fatal(err)
	}
	if err := s.Abort(context.Background()); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if n := countSessionDirs(t, root); n != 0 {
		t.Fatalf("session dirs after abort = %d, want 0", n)
	}
	if n := countBlobs(t, root); n != 0 {
		t.Fatalf("blobs after abort = %d, want 0", n)
	}
	// Abort is idempotent; Append/Commit after finalization fail cleanly.
	if err := s.Abort(context.Background()); err != nil {
		t.Fatalf("second Abort: %v", err)
	}
	if _, err := s.Append(context.Background(), strings.NewReader("x")); err == nil {
		t.Fatal("Append after Abort should fail")
	}
	if _, err := s.Commit(context.Background(), BlobRef{}); err == nil {
		t.Fatal("Commit after Abort should fail")
	}
}

func TestOpenReadSeekCloserAndMissing(t *testing.T) {
	eng := newEngine(t, Options{})
	ref := put(t, eng, []byte("0123456789abcdef"))

	f, got, err := eng.Open(context.Background(), ref.Sha256)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close() //nolint:errcheck // test
	if got.Sha256 != ref.Sha256 || got.Size != 16 {
		t.Fatalf("Open ref = %+v", got)
	}
	// Seek then read: the ReadSeekCloser contract T-20 (Range) relies on.
	if _, err := f.Seek(4, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(f, buf); err != nil {
		t.Fatalf("Read after Seek: %v", err)
	}
	if string(buf) != "4567" {
		t.Fatalf("read after seek = %q, want %q", buf, "4567")
	}

	_, _, err = eng.Open(context.Background(), strings.Repeat("0", 64))
	if !errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("Open missing error = %v, want ErrBlobNotFound", err)
	}
	_, err = eng.Stat(context.Background(), strings.Repeat("0", 64))
	if !errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("Stat missing error = %v, want ErrBlobNotFound", err)
	}
	// Malformed digests are rejected before touching the filesystem.
	if _, _, err := eng.Open(context.Background(), "../escape"); err == nil || errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("Open malformed digest err = %v, want validation error", err)
	}
}

func TestMultipleAppendsAccumulate(t *testing.T) {
	eng := newEngine(t, Options{})
	s, err := eng.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var off int64
	for _, part := range []string{"alpha", "beta", "gamma"} {
		off, err = s.Append(context.Background(), strings.NewReader(part))
		if err != nil {
			t.Fatal(err)
		}
	}
	if off != int64(len("alphabetagamma")) {
		t.Fatalf("cumulative offset = %d, want %d", off, len("alphabetagamma"))
	}
	ref, err := s.Commit(context.Background(), BlobRef{})
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte("alphabetagamma")))
	if ref.Sha256 != want {
		t.Fatalf("sha256 over concatenated appends = %s, want %s", ref.Sha256, want)
	}
}

func TestResumeSessionNotSupported(t *testing.T) {
	eng := newEngine(t, Options{})
	s, err := eng.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.ResumeSession(context.Background(), s.ID()); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("ResumeSession error = %v, want ErrSessionNotFound", err)
	}
	_ = s.Abort(context.Background())
}
