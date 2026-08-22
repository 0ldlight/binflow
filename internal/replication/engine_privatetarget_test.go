package replication_test

import (
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/replication"
)

// TestPrivateTargetDeniedWhenExplicit pins AC ③ of T-210: when
// DenyPrivateTargets=true (the operators spelling replication.allow_private_target
// = false), the SSRF screening list is ON for target addresses. The httptest
// target binds to loopback (127.0.0.1), so the guard rejects it at CheckURL
// BEFORE any outbound request — the target sees zero calls and the task fails
// as not-retryable with a loopback diagnosis.
func TestPrivateTargetDeniedWhenExplicit(t *testing.T) {
	sha := testSHA256(testPayload)
	target := &scriptTarget{}
	f := newEngineFixture(t, target, &replication.EngineOptions{DenyPrivateTargets: true}, nil)
	f.engine.Enqueue(f.ctx, "libs-local", "org/app/1.bin", sha)

	task := f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusFailed
	}, "failed (private target denied)")

	heads, puts, _, _, _ := target.snapshot()
	if heads != 0 || puts != 0 {
		t.Fatalf("target saw %d HEAD, %d PUT; want 0/0 (denied before connect)", heads, puts)
	}
	if task.Attempts != 6 {
		t.Fatalf("Attempts = %d, want the cap 6 (not retryable)", task.Attempts)
	}
	if !strings.Contains(task.LastError, "loopback") {
		t.Fatalf("LastError = %q, want a loopback denial diagnosis", task.LastError)
	}
	if !strings.Contains(task.LastError, "not retryable") {
		t.Fatalf("LastError = %q, want the not-retryable marker", task.LastError)
	}
}

// TestPrivateTargetAllowedByDefault pins AC ② of T-210: the zero-value engine
// (private targets allowed, the pre-config default) still pushes through to
// the loopback httptest target. This is the regression guard that proves the
// default posture is byte-for-byte unchanged.
func TestPrivateTargetAllowedByDefault(t *testing.T) {
	sha := testSHA256(testPayload)
	f := newEngineFixture(t, &scriptTarget{}, nil, nil)
	f.engine.Enqueue(f.ctx, "libs-local", "org/app/1.bin", sha)

	f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusSuccess
	}, "success (private target allowed by default)")
	if heads, puts, _, _, _ := f.target.snapshot(); heads != 1 || puts != 1 {
		t.Fatalf("target calls = %d HEAD, %d PUT; want 1/1", heads, puts)
	}
}
