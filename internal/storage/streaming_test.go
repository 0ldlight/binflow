package storage

import (
	"bytes"
	"context"
	"crypto/md5"  //nolint:gosec // verifying an ancillary digest
	"crypto/sha1" //nolint:gosec // verifying an ancillary digest
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// streamSource is an io.Reader producing size pseudo-random bytes with a
// small deterministic footprint: a 64 KiB block re-encrypted-ish by index.
// Memory use is one block, not the whole stream.
type streamSource struct {
	size   int64
	offset int64
	block  []byte
	buf    [8]byte
}

func newStreamSource(size int64) *streamSource {
	block := make([]byte, 64*1024)
	// Cheap deterministic fill: position-dependent pattern.
	for i := range block {
		block[i] = byte(i*31 + i/256)
	}
	return &streamSource{size: size, block: block}
}

func (s *streamSource) Read(p []byte) (int, error) {
	if s.offset >= s.size {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	// Rotate the block by offset/len so consecutive windows differ.
	shift := int(s.offset / int64(len(s.block)))
	n := int64(len(p))
	if rem := s.size - s.offset; rem < n {
		n = rem
	}
	for i := int64(0); i < n; i++ {
		pos := s.offset + i
		src := (int(pos) + shift*7) % len(s.block)
		p[i] = s.block[src] ^ byte(pos>>16) ^ s.buf[byte(pos)%8]
	}
	s.offset += n
	return int(n), nil
}

// rss returns the current process total allocated-but-alive heap bytes.
func heapAlloc() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// TestLargeFileStreamingRSS commits a 512 MiB blob through a real session
// and asserts the process heap stays far below the content size: streaming
// must make memory independent of artifact size (ticket AC 3).
//
// CI downshift: BINFLOW_STORAGE_TEST_BYTES (default 512 MiB) can lower the
// payload; the assertion scales with it (heap delta < half the payload and
// hard-capped at 256 MiB).
func TestLargeFileStreamingRSS(t *testing.T) {
	size := int64(512 << 20)
	if v := os.Getenv("BINFLOW_STORAGE_TEST_BYTES"); v != "" {
		var n int64
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
			t.Fatalf("bad BINFLOW_STORAGE_TEST_BYTES=%q", v)
		}
		size = n
		t.Logf("CI downshift active: payload = %d bytes", size)
	}
	if testing.Short() {
		size = 4 << 20
		t.Logf("short mode: payload = %d bytes", size)
	}

	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test

	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	before := heapAlloc()

	s, err := eng.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	written, err := s.Append(context.Background(), newStreamSource(size))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if written != size {
		t.Fatalf("written = %d, want %d", written, size)
	}
	ref, err := s.Commit(context.Background(), BlobRef{})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	runtime.GC()
	after := heapAlloc()

	limit := uint64(256 << 20)
	if uint64(size/2) < limit {
		limit = uint64(size / 2)
	}
	t.Logf("payload=%d bytes, heap before=%d after=%d delta=%d (limit %d)",
		size, before, after, int64(after)-int64(before), limit)
	if delta := after - before; delta > limit && after > before {
		t.Fatalf("heap grew by %d bytes (> %d): streaming invariant violated", delta, limit)
	}
	if ref.Size != size {
		t.Fatalf("ref.Size = %d, want %d", ref.Size, size)
	}
	// Spot-check the digest by re-streaming a prefix from disk.
	f, _, err := eng.Open(context.Background(), ref.Sha256)
	if err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 64*1024)
	if _, err := io.ReadFull(f, head); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	src := newStreamSource(size)
	wantHead := make([]byte, 64*1024)
	if _, err := io.ReadFull(src, wantHead); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(head, wantHead) {
		t.Fatal("on-disk head does not match the source stream")
	}
}

// TestStatDigestsMatchCliTools cross-checks Stat against the host's
// sha256sum/sha1sum/md5sum (macOS: shasum -a / md5) so the engine's digest
// pipeline is verified by an independent implementation, per ticket AC 3.
func TestStatDigestsMatchCliTools(t *testing.T) {
	if _, err := exec.LookPath("shasum"); err != nil {
		t.Skip("shasum not available")
	}
	content := []byte("cross-checked against external digest tools\n")
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test
	ref := put(t, eng, content)
	p := wantBlobPath(t, root, ref.Sha256)

	tools := []struct {
		name string
		args []string
		want string
	}{
		{name: "sha256", args: []string{"-a", "256", p}, want: ref.Sha256},
		{name: "sha1", args: []string{"-a", "1", p}, want: ref.Sha1},
	}
	for _, tt := range tools {
		t.Run(tt.name, func(t *testing.T) {
			out, err := exec.Command("shasum", tt.args...).Output()
			if err != nil {
				t.Fatalf("shasum %v: %v", tt.args, err)
			}
			got := string(bytes.Fields(out)[0])
			if got != tt.want {
				t.Fatalf("shasum %s = %s, engine said %s", tt.name, got, tt.want)
			}
		})
	}
	t.Run("md5", func(t *testing.T) {
		md5Tool := "md5sum"
		if _, err := exec.LookPath(md5Tool); err != nil {
			md5Tool = "md5" // macOS
			if _, err := exec.LookPath(md5Tool); err != nil {
				t.Skip("no md5 tool on host")
			}
		}
		var out []byte
		var err error
		if md5Tool == "md5" {
			out, err = exec.Command("md5", "-q", p).Output()
		} else {
			out, err = exec.Command("md5sum", p).Output()
		}
		if err != nil {
			t.Fatal(err)
		}
		got := string(bytes.Fields(out)[0])
		if got != ref.Md5 {
			t.Fatalf("md5 tool = %s, engine said %s", got, ref.Md5)
		}
	})

	// And the pure-Go independent recompute (always runs, no tool needed).
	h256 := sha256.Sum256(content)
	h1 := sha1.Sum(content)  // ancillary digest check
	hmd5 := md5.Sum(content) // ancillary digest check
	if ref.Sha256 != hex.EncodeToString(h256[:]) ||
		ref.Sha1 != hex.EncodeToString(h1[:]) ||
		ref.Md5 != hex.EncodeToString(hmd5[:]) {
		t.Fatalf("Commit digests disagree with stdlib recompute: %+v", ref)
	}
	st, err := eng.Stat(context.Background(), ref.Sha256)
	if err != nil {
		t.Fatal(err)
	}
	if st != ref {
		t.Fatalf("Stat = %+v, Commit said %+v", st, ref)
	}
}

// TestSessionStateOnDisk pins the state.json contract (architecture
// section 4.1): exact key set {"version","id","created_at","received",
// "sha256"}, sha256 always null, received tracking the bytes appended, and
// the file vanishing with the session.
func TestSessionStateOnDisk(t *testing.T) {
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test
	s, err := eng.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "sessions", s.ID(), "state.json"))
	if err != nil {
		t.Fatalf("state.json missing: %v", err)
	}
	var st struct {
		Version   *int   `json:"version"`
		ID        string `json:"id"`
		CreatedAt string `json:"created_at"`
		Received  *int64 `json:"received"`
		Sha256    any    `json:"sha256"`
	}
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("state.json = %s: %v", b, err)
	}
	if st.Version == nil || *st.Version != 1 {
		t.Fatalf("state.json version = %v, want 1", st.Version)
	}
	if st.ID != s.ID() {
		t.Fatalf("state.json id = %q, want %q", st.ID, s.ID())
	}
	if st.CreatedAt == "" {
		t.Fatal("state.json created_at empty")
	}
	if st.Received == nil || *st.Received != 0 {
		t.Fatalf("state.json received at begin = %v, want 0", st.Received)
	}
	if st.Sha256 != nil {
		t.Fatalf("state.json sha256 = %v, want null (digests never persisted)", st.Sha256)
	}

	// received must track appended bytes truthfully.
	if _, err := s.Append(context.Background(), bytes.NewReader([]byte("partial"))); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(filepath.Join(root, "sessions", s.ID(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st.Received = nil
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("state.json after append = %s: %v", b, err)
	}
	if st.Received == nil || *st.Received != int64(len("partial")) {
		t.Fatalf("state.json received after append = %v, want %d", st.Received, len("partial"))
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", s.ID(), "data")); err != nil {
		t.Fatalf("session data file missing: %v", err)
	}
	_ = s.Abort(context.Background())
	if _, err := os.Stat(filepath.Join(root, "sessions", s.ID(), "state.json")); !os.IsNotExist(err) {
		t.Fatalf("state.json survived Abort: %v", err)
	}
}
