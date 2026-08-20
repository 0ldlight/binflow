package storage

// T-94: the exported holder-record readers (architecture review N1..N3 on
// T-96's lock). The 409 diagnostics face of the REST gc endpoint consumes
// exactly these; nothing outside the package parses the record or the
// acquire error string.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataLockHolderHelpers(t *testing.T) {
	t.Run("holder op vocabulary matches the acquire record", func(t *testing.T) {
		dir := t.TempDir()
		lock, err := AcquireDataLock(dir, DataLockOpExport)
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		holder := LockHolder(dir)
		if HolderOp(holder) != DataLockOpExport {
			t.Fatalf("holder %q: op = %q, want %q", holder, HolderOp(holder), DataLockOpExport)
		}
		if err := lock.Release(); err != nil {
			t.Fatalf("release: %v", err)
		}
	})

	cases := []struct {
		name   string
		holder string
		want   string
	}{
		{"full record", "pid=123 op=gc", DataLockOpGC},
		{"export record", "pid=1 op=export", DataLockOpExport},
		{"op first", "op=gc pid=1", DataLockOpGC},
		{"no op token", "pid=123", ""},
		{"empty", "", ""},
		{"garbage", "pid=123 opration=export", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HolderOp(tc.holder); got != tc.want {
				t.Fatalf("HolderOp(%q) = %q, want %q", tc.holder, got, tc.want)
			}
		})
	}

	t.Run("lock holder degrades to empty", func(t *testing.T) {
		dir := t.TempDir()
		if got := LockHolder(dir); got != "" {
			t.Fatalf("missing lock file: LockHolder = %q, want empty", got)
		}
		if got := LockHolder(""); got != "" {
			t.Fatalf("empty data dir: LockHolder = %q, want empty", got)
		}
		// The Windows shape (N3): the record exists but a contender cannot
		// read it — modeled by a directory the reader cannot open; ""
		// (not an error, not "unknown") is the documented degradation.
		if err := os.WriteFile(filepath.Join(dir, maintenanceLockName), nil, 0o000); err != nil {
			t.Fatalf("blank record: %v", err)
		}
		if got := LockHolder(dir); got != "" {
			t.Fatalf("unreadable record: LockHolder = %q, want empty", got)
		}
	})
}
