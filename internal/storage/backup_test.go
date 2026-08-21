package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeBlobFile seeds a well-formed blob under root with the given body,
// permission bits and mtime, returning its sha256.
func writeBlobFile(t *testing.T, root, body string, mode os.FileMode, mtime time.Time) string {
	t.Helper()
	sum := sha256HexForTest(body)
	path := filepath.Join(root, "blobs", sum[:2], sum)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir shard: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("write blob: %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod blob: %v", err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes blob: %v", err)
	}
	return sum
}

// sha256HexForTest is the plain reference digest (the engine's streaming
// digesters are exercised by their own tests; here only a stable checksum is
// needed).
func sha256HexForTest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func TestManifestValidateMatrix(t *testing.T) {
	validBlob := func(sha string, size int64) ManifestBlob {
		return ManifestBlob{Sha256: sha, Size: size, MTime: "2026-08-20T00:00:00Z"}
	}
	base := func() *Manifest {
		return &Manifest{
			FormatVersion:  BackupFormatVersion,
			CreatedAt:      "2026-08-20T00:00:00Z",
			BinflowVersion: "dev",
			Metadata:       ManifestMetadata{File: "metadata.db", Sha256: sha256HexForTest("db")},
			GraceNote:      ManifestGraceNote,
			Blobs: []ManifestBlob{
				validBlob(sha256HexForTest("a"), 1),
				validBlob(sha256HexForTest("b"), 2),
			},
			BlobCount:  2,
			TotalBytes: 3,
		}
	}
	tests := []struct {
		name   string
		mutate func(m *Manifest)
		wantOK bool
	}{
		{name: "well-formed manifest passes", mutate: func(*Manifest) {}, wantOK: true},
		{name: "newer format version refused", mutate: func(m *Manifest) { m.FormatVersion = BackupFormatVersion + 1 }, wantOK: false},
		{name: "zero format version refused", mutate: func(m *Manifest) { m.FormatVersion = 0 }, wantOK: false},
		{name: "blobCount tampered (W31)", mutate: func(m *Manifest) { m.BlobCount++ }, wantOK: false},
		{name: "totalBytes tampered", mutate: func(m *Manifest) { m.TotalBytes++ }, wantOK: false},
		{name: "empty metadata file refused", mutate: func(m *Manifest) { m.Metadata.File = "" }, wantOK: false},
		{name: "metadata file path traversal refused", mutate: func(m *Manifest) { m.Metadata.File = "../escape.db" }, wantOK: false},
		{name: "metadata file nested path refused", mutate: func(m *Manifest) { m.Metadata.File = "sub/dir.db" }, wantOK: false},
		{name: "metadata sha not a digest refused", mutate: func(m *Manifest) { m.Metadata.Sha256 = "nothex" }, wantOK: false},
		{name: "malformed blob sha refused", mutate: func(m *Manifest) {
			m.Blobs[0].Sha256 = "zz"
			m.Blobs[0].Size = 1
		}, wantOK: false},
		{name: "duplicate blob refused", mutate: func(m *Manifest) {
			m.Blobs[1] = m.Blobs[0]
			m.TotalBytes = m.Blobs[0].Size * 2
		}, wantOK: false},
		{name: "negative blob size refused", mutate: func(m *Manifest) {
			m.Blobs[0].Size = -1
			m.TotalBytes = m.Blobs[1].Size - 1
		}, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := base()
			tt.mutate(m)
			err := m.Validate()
			if tt.wantOK && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
			if !tt.wantOK {
				if err == nil {
					t.Fatal("Validate() error = nil, want ErrManifestInvalid")
				}
				if !errors.Is(err, ErrManifestInvalid) {
					t.Fatalf("Validate() error = %v, want ErrManifestInvalid in the chain", err)
				}
			}
		})
	}
}

func TestWriteLoadManifestRoundtrip(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		FormatVersion:  BackupFormatVersion,
		CreatedAt:      "2026-08-20T00:00:00Z",
		BinflowVersion: "dev",
		Metadata:       ManifestMetadata{File: exportDBNameForTest, Sha256: sha256HexForTest("db")},
		GraceNote:      ManifestGraceNote,
		Blobs:          []ManifestBlob{{Sha256: sha256HexForTest("a"), Size: 10, MTime: "2026-08-20T00:00:00Z"}},
		BlobCount:      1,
		TotalBytes:     10,
	}
	path := ManifestPath(dir)
	if err := WriteManifest(m, path); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat manifest: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("manifest mode = %o, want 0600 (secret-grade artifact)", perm)
	}
	got, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if got.BlobCount != 1 || got.Blobs[0].Sha256 != m.Blobs[0].Sha256 || got.Metadata.Sha256 != m.Metadata.Sha256 {
		t.Fatalf("roundtrip drifted: %+v", got)
	}
	// A write of an INVALID manifest must be refused, not half-written.
	bad := *m
	bad.BlobCount = 99
	if err := WriteManifest(&bad, path); err == nil {
		t.Fatal("WriteManifest(invalid) error = nil, want refusal")
	}
}

const exportDBNameForTest = "metadata.db"

func TestCopyBlobsTreePreservesMtimeAndMode(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	stamp := time.Now().Add(-73 * time.Hour).Truncate(time.Second) // aged past any grace; sub-second truncated for cross-fs stability
	shaA := writeBlobFile(t, src, "body-a", 0o600, stamp)
	shaB := writeBlobFile(t, src, "body-b", 0o600, stamp.Add(time.Minute))

	// Foreign entries must not be laundered into the copy.
	if err := os.MkdirAll(filepath.Join(src, "blobs", "zz"), 0o700); err != nil {
		t.Fatalf("mkdir foreign shard: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "blobs", "zz", "not-a-digest"), []byte("junk"), 0o600); err != nil {
		t.Fatalf("write foreign file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "blobs", "zz", shaA), []byte("misfiled"), 0o600); err != nil {
		t.Fatalf("write misfiled blob: %v", err)
	}

	files, bytes, err := CopyBlobsTree(src, dst)
	if err != nil {
		t.Fatalf("CopyBlobsTree: %v", err)
	}
	if files != 2 || bytes != int64(len("body-a")+len("body-b")) {
		t.Fatalf("files=%d bytes=%d, want 2 files and the two bodies' bytes", files, bytes)
	}
	for _, sha := range []string{shaA, shaB} {
		dstPath := filepath.Join(dst, "blobs", sha[:2], sha)
		got, err := os.ReadFile(dstPath)
		if err != nil {
			t.Fatalf("copied blob %s missing: %v", sha, err)
		}
		body := "body-a"
		if sha == shaB {
			body = "body-b"
		}
		if string(got) != body {
			t.Fatalf("blob %s content = %q, want %q", sha, got, body)
		}
		info, err := os.Stat(dstPath)
		if err != nil {
			t.Fatalf("stat copied blob: %v", err)
		}
		if !info.ModTime().Equal(stamp) && !info.ModTime().Equal(stamp.Add(time.Minute)) {
			t.Fatalf("blob %s mtime = %v, want the source stamp (grace clock must not reset)", sha, info.ModTime())
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("blob %s mode = %o, want 0600", sha, perm)
		}
	}
	// The misfiled copy of shaA under zz/ is foreign (shard prefix mismatch)
	// and must not exist in the destination.
	if _, err := os.Stat(filepath.Join(dst, "blobs", "zz")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("foreign shard copied into the backup: %v", err)
	}
}

func TestCopyBlobsTreeMissingSourceIsEmpty(t *testing.T) {
	dst := t.TempDir()
	files, bytes, err := CopyBlobsTree(t.TempDir(), dst)
	if err != nil {
		t.Fatalf("CopyBlobsTree on an empty source: %v", err)
	}
	if files != 0 || bytes != 0 {
		t.Fatalf("files=%d bytes=%d, want zeros for an empty store", files, bytes)
	}
}

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	sum, size, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}
	if sum != sha256HexForTest("hello") || size != 5 {
		t.Fatalf("HashFile = %s/%d, want %s/5", sum, size, sha256HexForTest("hello"))
	}
}

func TestDataLockMutualExclusion(t *testing.T) {
	dir := t.TempDir()
	first, err := AcquireDataLock(dir, "gc")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	// A second acquisition — same process, second fd — must contend: the
	// serve-process REST gc and the CLI export process meet exactly here.
	second, err := AcquireDataLock(dir, "export")
	if err == nil {
		_ = second.Release()
		t.Fatal("second AcquireDataLock succeeded while the lock was held")
	}
	if !errors.Is(err, ErrDataLockHeld) {
		t.Fatalf("second acquire error = %v, want ErrDataLockHeld in the chain", err)
	}
	// The holder record rides the contention error on unix (flock leaves the
	// file readable for contenders). On windows the exclusive byte-range
	// lock makes the record unreadable while held, so the message degrades
	// to the "unknown" last holder (LockHolder doc, N3). Pin each
	// platform's documented shape — the G05 windows leg (T-169) runs this
	// same test on a real windows host.
	wantHolder := "op=gc"
	if runtime.GOOS == "windows" {
		wantHolder = "last holder: unknown"
	}
	if !strings.Contains(err.Error(), wantHolder) {
		t.Fatalf("contention error = %q, want holder diagnostics %q", err.Error(), wantHolder)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	// After release the lock is takeable again; a nil/released lock is a
	// no-op (the defer-friendly shape).
	again, err := AcquireDataLock(dir, "export")
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	var nilLock *DataLock
	if err := nilLock.Release(); err != nil {
		t.Fatalf("nil Release: %v", err)
	}
	if err := again.Release(); err != nil {
		t.Fatalf("second release: %v", err)
	}
}
