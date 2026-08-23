// Internal step-up ledger tests (package auth): the expiry verdict needs a
// grant that is already past its deadline, and the config-validated TTL
// floor makes issuing one through the public API impossible — so the ledger
// row is planted directly.

package auth

import (
	"context"
	"testing"
	"time"
)

// TestStepUpGrantExpiredRefusedAndBurned: an expired grant is refused AND
// burned (the sweep-equivalent verdict on the consume path), so no later
// retry can resurrect it.
func TestStepUpGrantExpiredRefusedAndBurned(t *testing.T) {
	s := &Service{stepUpGrants: newStepUpLedger()}
	h := grantHash("expired-grant-value")
	s.stepUpGrants.grants[h] = stepUpGrant{
		username:    "ssouser",
		sessionHash: "sess",
		expiresAt:   time.Now().Add(-time.Minute),
	}
	if s.ConsumeStepUpGrant("expired-grant-value", "ssouser", "sess") {
		t.Error("expired grant accepted")
	}
	if _, still := s.stepUpGrants.grants[h]; still {
		t.Error("expired grant survived its consumption attempt; it must burn")
	}
}

// TestStepUpGrantSweepOnIssue: issuance drops expired entries, so an
// abandoned flow's grant cannot accumulate.
func TestStepUpGrantSweepOnIssue(t *testing.T) {
	s := &Service{stepUpGrants: newStepUpLedger()}
	s.stepUpGrants.grants[grantHash("dead")] = stepUpGrant{
		username: "u", sessionHash: "s", expiresAt: time.Now().Add(-time.Hour),
	}
	if _, err := s.IssueStepUpGrant(context.Background(), "u2", "s2", time.Minute); err != nil {
		t.Fatalf("IssueStepUpGrant: %v", err)
	}
	if _, dead := s.stepUpGrants.grants[grantHash("dead")]; dead {
		t.Error("expired entry survived the issuance sweep")
	}
	if len(s.stepUpGrants.grants) != 1 {
		t.Errorf("ledger holds %d entries, want 1", len(s.stepUpGrants.grants))
	}
}

// TestStepUpGrantWrongSessionBinding: the session binding is part of the
// verdict — the same username presenting a grant over a DIFFERENT session is
// refused (a stolen grant cannot be replayed from another browser).
func TestStepUpGrantWrongSessionBinding(t *testing.T) {
	s := &Service{stepUpGrants: newStepUpLedger()}
	grant, err := s.IssueStepUpGrant(context.Background(), "ssouser", "session-a", time.Minute)
	if err != nil {
		t.Fatalf("IssueStepUpGrant: %v", err)
	}
	if s.ConsumeStepUpGrant(grant, "ssouser", "session-b") {
		t.Error("grant accepted over a session it was not bound to")
	}
}
