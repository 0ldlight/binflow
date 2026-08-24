package main

// T-256: the serve.lock heartbeat and the gc CLI's cross-process gate
// ([M9] ADR-0031 / architecture section 14.2 point 4). The lock primitive
// is pinned directly (acquire/contend/release/probe), then the gc gate is
// driven end to end through runGC: a sub-minute --apply against a data
// directory whose heartbeat lock is held refuses with the REST-or-window
// guidance, while the dry-run and the default-grace arms stay available and
// the dry-run annotates the K22 over-report caveat.

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeLockAcquireContendRelease(t *testing.T) {
	dir := t.TempDir()

	lock, err := acquireServeLock(dir)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !serveRunning(dir) {
		t.Fatal("serveRunning = false while the lock is held")
	}

	// A second acquirer (the "second serve on one data directory" case) is
	// refused with the pointed multi-instance wording.
	_, err = acquireServeLock(dir)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("second acquire error = %v, want the multi-instance refusal", err)
	}

	if err := lock.release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if serveRunning(dir) {
		t.Fatal("serveRunning = true after release")
	}
	// Release is idempotent; the lock re-acquires cleanly afterwards.
	if err := lock.release(); err != nil {
		t.Fatalf("second release: %v", err)
	}
	if _, err := acquireServeLock(dir); err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
}

// serveLockHeld holds dir's heartbeat lock for the test's duration.
func serveLockHeld(t *testing.T, dir string) {
	t.Helper()
	lock, err := acquireServeLock(dir)
	if err != nil {
		t.Fatalf("hold serve.lock: %v", err)
	}
	t.Cleanup(func() { _ = lock.release() })
}

func TestGCServeLockGate(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	withEnv(t, map[string]string{
		"BINFLOW_HOME":              "",
		"BINFLOW_ADMIN_PASSWORD":    "test-admin-pw",
		"BINFLOW_STORAGE__DATA_DIR": dataDir,
	})
	restore := chdirTemp(t)
	defer restore()

	// The gate only bites while a serve process holds the heartbeat lock.
	serveLockHeld(t, dataDir)

	cases := []struct {
		name    string
		args    []string
		wantErr string // "" = the run must succeed
		wantOut string // asserted inside the report when non-empty
	}{
		{
			name:    "sub-minute apply is refused while serve runs",
			args:    []string{"gc", "--apply", "--grace-seconds", "30"},
			wantErr: "refusing --apply with grace 30s while serve is running",
		},
		{
			name:    "the refusal names the REST alternative",
			args:    []string{"gc", "--apply", "--grace-seconds", "1"},
			wantErr: "POST /binflow/api/v1/system/gc",
		},
		{
			name:    "dry-run is never refused, and carries the K22 note",
			args:    []string{"gc", "--grace-seconds", "30"},
			wantOut: "candidates may over-report",
		},
		{
			name: "apply at the 60s floor is allowed (mtime window covers it)",
			args: []string{"gc", "--apply", "--grace-seconds", "60"},
		},
		{
			name: "default-grace apply is allowed while serve runs",
			args: []string{"gc", "--apply"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tc.args, &stdout, &stderr)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("run(%v) error = %v (stderr: %s)", tc.args, err, stderr.String())
				}
				if tc.wantOut != "" && !strings.Contains(stderr.String(), tc.wantOut) {
					t.Fatalf("run(%v) output = %q, want it to contain %q", tc.args, stderr.String(), tc.wantOut)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("run(%v) error = %v, want it to contain %q", tc.args, err, tc.wantErr)
			}
		})
	}
}

// TestGCServeLockGateQuietWithoutServe pins the other half of the gate: no
// serve heartbeat, no refusal — the sub-minute W24 recipe keeps working on
// a stopped instance (the maintenance-window posture the refusal suggests).
func TestGCServeLockGateQuietWithoutServe(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, map[string]string{
		"BINFLOW_HOME":              "",
		"BINFLOW_ADMIN_PASSWORD":    "test-admin-pw",
		"BINFLOW_STORAGE__DATA_DIR": filepath.Join(dir, "data"),
	})
	restore := chdirTemp(t)
	defer restore()

	var stdout, stderr bytes.Buffer
	if err := run([]string{"gc", "--apply", "--grace-seconds", "30"}, &stdout, &stderr); err != nil {
		t.Fatalf("gc --apply --grace-seconds 30 (no serve): %v\n%s", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "candidates may over-report") {
		t.Fatalf("quiet instance report carries the serve-running note:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "grace=30s") {
		t.Fatalf("report = %q, want the echoed grace=30s", stderr.String())
	}
}

// TestGCGraceSecondsFlagMatrix pins the flag ladder: seconds echo, seconds
// win over hours and days, negatives refuse.
func TestGCGraceSecondsFlagMatrix(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, map[string]string{
		"BINFLOW_HOME":              "",
		"BINFLOW_ADMIN_PASSWORD":    "test-admin-pw",
		"BINFLOW_STORAGE__DATA_DIR": filepath.Join(dir, "data"),
	})
	restore := chdirTemp(t)
	defer restore()

	cases := []struct {
		name    string
		args    []string
		wantErr string
		wantOut string
	}{
		{
			name:    "seconds echo",
			args:    []string{"gc", "--grace-seconds", "90"},
			wantOut: "grace=1m30s",
		},
		{
			name:    "seconds win over hours",
			args:    []string{"gc", "--grace-hours", "2", "--grace-seconds", "90"},
			wantOut: "grace=1m30s",
		},
		{
			name:    "seconds win over days and hours",
			args:    []string{"gc", "--grace-days", "1", "--grace-hours", "2", "--grace-seconds", "90"},
			wantOut: "grace=1m30s",
		},
		{
			name:    "negative seconds refuse",
			args:    []string{"gc", "--grace-seconds", "-1"},
			wantErr: "must be a positive integer",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tc.args, &stdout, &stderr)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("run(%v) error = %v (stderr: %s)", tc.args, err, stderr.String())
				}
				if !strings.Contains(stderr.String(), tc.wantOut) {
					t.Fatalf("output = %q, want %q", stderr.String(), tc.wantOut)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("run(%v) error = %v, want %q", tc.args, err, tc.wantErr)
			}
		})
	}
}

// TestServeRefusesSecondInstanceOnSameDataDir drives runServe into a data
// directory whose heartbeat lock is already held: the boot must fail naming
// serve.lock before any listener opens (the gate's serve-side half).
func TestServeRefusesSecondInstanceOnSameDataDir(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	serveLockHeld(t, dataDir)
	withEnv(t, map[string]string{
		"BINFLOW_HOME":              "",
		"BINFLOW_ADMIN_PASSWORD":    "test-admin-pw",
		"BINFLOW_STORAGE__DATA_DIR": dataDir,
		"BINFLOW_SERVER__LISTEN":    "127.0.0.1:0",
	})
	restore := chdirTemp(t)
	defer restore()

	var stdout, stderr bytes.Buffer
	err := run([]string{"serve"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "serve.lock") {
		t.Fatalf("run(serve) error = %v, want the serve.lock refusal", err)
	}
}
