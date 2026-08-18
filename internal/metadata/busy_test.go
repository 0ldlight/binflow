package metadata

// T-54 regression: busy-class driver errors (SQLITE_BUSY/SQLITE_LOCKED that
// outlived busy_timeout) must be classifiable by upper layers so they can
// answer retryable (503) instead of permanent-failure (500). Pins both the
// typed classifier and wrapExec's sentinel attachment, using a REAL driver
// busy error produced by a second connection holding an exclusive lock with
// busy_timeout=0 on the waiter.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

// TestIsSQLiteBusyRealDriverError produces a genuine SQLITE_BUSY through the
// real driver (exclusive lock on one connection, zero busy_timeout on the
// other) and asserts the classifier sees it, that unrelated driver errors are
// not misclassified, and that IsStoreBusy traverses a repo-style wrap chain.
func TestIsSQLiteBusyRealDriverError(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "busy.db")

	holder, err := sql.Open(sqliteDriverName, dsn(path))
	if err != nil {
		t.Fatalf("open holder: %v", err)
	}
	defer holder.Close() //nolint:errcheck // test cleanup
	// The waiter: same file, DSN of its own with busy_timeout(0) so the lock
	// contention surfaces immediately instead of retrying.
	waiter, err := sql.Open(sqliteDriverName,
		"file:"+path+"?_pragma=busy_timeout(0)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open waiter: %v", err)
	}
	defer waiter.Close() //nolint:errcheck // test cleanup

	if _, err := holder.ExecContext(ctx, "CREATE TABLE t (v TEXT)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := holder.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("begin exclusive: %v", err)
	}
	// Hold the write lock; the waiter's insert must fail busy at once.
	_, err = waiter.ExecContext(ctx, "INSERT INTO t (v) VALUES ('x')")
	if err == nil {
		t.Fatal("insert under exclusive lock unexpectedly succeeded")
	}
	if !isSQLiteBusy(err) {
		t.Fatalf("real driver error not classified busy: %v", err)
	}

	// The sentinel chain: wrapExec's product answers errors.Is/IsStoreBusy
	// through arbitrary upper-layer re-wrapping (the repo/service style).
	wrapped := wrapExec("blobs put", "abc", err)
	if !errors.Is(wrapped, ErrStoreBusy) {
		t.Fatalf("wrapExec product misses ErrStoreBusy: %v", wrapped)
	}
	rewrapped := fmt.Errorf("blob row abc: %w", wrapped)
	if !IsStoreBusy(rewrapped) {
		t.Fatalf("IsStoreBusy does not traverse a repo-style chain: %v", rewrapped)
	}

	// Non-busy errors must not carry the sentinel.
	plain := wrapExec("blobs put", "abc", errors.New("disk exploded"))
	if IsStoreBusy(plain) {
		t.Fatalf("non-busy error carries ErrStoreBusy: %v", plain)
	}
	if isSQLiteBusy(errors.New("database is locked (fake string)")) {
		t.Fatal("string look-alike classified busy: classification must be typed")
	}

	if err := holder.Close(); err != nil {
		t.Fatalf("close holder: %v", err)
	}
}
