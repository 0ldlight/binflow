package metadata

// FR-146.3 (M16, T-454): the per-actor LastActionTimes aggregation the
// users-list lastLoggedIn projection rides. Table-driven over the real
// sqlite store: MAX(time) per actor for the requested action only, the
// empty log, the hundred-user single-query leg, and the access path pinned
// with EXPLAIN QUERY PLAN (idx_audit_action serves the filter, no full
// table scan — the GE-01 posture Query already carries).

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// lastActionStore opens a throwaway store with the audit events pre-seeded.
func lastActionStore(t *testing.T, events ...*AuditEvent) Store {
	t.Helper()
	ctx := context.Background()
	st, err := Open(ctx, Options{Path: t.TempDir() + "/last-action.db", AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, e := range events {
		if err := st.Audits().Append(ctx, e); err != nil {
			t.Fatalf("seed audit event %+v: %v", e, err)
		}
	}
	return st
}

// TestLastActionTimesAggregation walks the derivation table: which actors
// land in the map, and with which time.
func TestLastActionTimesAggregation(t *testing.T) {
	// RFC3339 UTC text compares chronologically, so hand-seeded second-
	// granular times are deterministic whatever the wall clock says.
	ev := func(actor, action, ts string) *AuditEvent {
		return &AuditEvent{Time: ts, Actor: actor, Action: action, Detail: "{}"}
	}
	seed := []*AuditEvent{
		// alice logged in three times: the newest must win.
		ev("alice", "login.success", "2026-01-01T00:00:01Z"),
		ev("alice", "login.success", "2026-03-04T05:06:07Z"),
		ev("alice", "login.success", "2026-02-02T00:00:00Z"),
		// bob logged in once, then failed twice: failures are not logins.
		ev("bob", "login.success", "2026-01-02T00:00:00Z"),
		ev("bob", "auth.failed", "2026-05-05T00:00:00Z"),
		ev("bob", "auth.failed", "2026-06-06T00:00:00Z"),
		// carol only ever failed: no entry under login.success, but the
		// auth.failed derivation sees her newest failure.
		ev("carol", "auth.failed", "2026-04-04T00:00:00Z"),
		ev("carol", "auth.failed", "2026-04-05T00:00:00Z"),
		// non-auth actions are invisible to a login derivation and
		// vice versa.
		ev("alice", "deploy", "2026-12-31T23:59:59Z"),
		ev("dave", "token.issue", "2026-07-07T00:00:00Z"),
	}

	tests := []struct {
		name   string
		action string
		want   map[string]string
	}{
		{
			name:   "login derivation takes each actor's newest success",
			action: "login.success",
			want: map[string]string{
				"alice": "2026-03-04T05:06:07Z",
				"bob":   "2026-01-02T00:00:00Z",
			},
		},
		{
			name:   "failure derivation takes each actor's newest failure",
			action: "auth.failed",
			want: map[string]string{
				"bob":   "2026-06-06T00:00:00Z",
				"carol": "2026-04-05T00:00:00Z",
			},
		},
		{
			name:   "an action nobody performed yields an empty map",
			action: "repo.create",
			want:   map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := lastActionStore(t, seed...)
			got, err := st.Audits().LastActionTimes(context.Background(), tt.action)
			if err != nil {
				t.Fatalf("LastActionTimes(%q): %v", tt.action, err)
			}
			if got == nil {
				t.Fatal("nil map — the contract pins empty-but-non-nil so callers can range it")
			}
			if len(got) != len(tt.want) {
				t.Fatalf("map = %v (len %d), want %v (len %d)", got, len(got), tt.want, len(tt.want))
			}
			for actor, wantTime := range tt.want {
				if got[actor] != wantTime {
					t.Errorf("actor %q = %q, want %q", actor, got[actor], wantTime)
				}
			}
		})
	}
}

// TestLastActionTimesEmptyLog: the fresh-store derivation answers an empty
// map, not nil and not an error — "no login history" is a state, not a
// failure.
func TestLastActionTimesEmptyLog(t *testing.T) {
	st := lastActionStore(t)
	got, err := st.Audits().LastActionTimes(context.Background(), "login.success")
	if err != nil {
		t.Fatalf("LastActionTimes on empty log: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %v, want empty non-nil map", got)
	}
}

// TestLastActionTimesHundredUsersSingleQuery: the FR-146.3 performance
// posture — a hundred-user instance derives every actor's last login from
// ONE call whose cost is the single GROUP BY statement (the row count of
// the aggregation equals the actor count; the store holds no per-actor
// shortcut). The httpapi N+1 gate (users_last_login_test.go) pins the
// caller side: one call per list, user-count-independent.
func TestLastActionTimesHundredUsersSingleQuery(t *testing.T) {
	const users = 100
	seed := make([]*AuditEvent, 0, users*3)
	for i := 0; i < users; i++ {
		actor := fmt.Sprintf("user-%03d", i)
		seed = append(seed,
			&AuditEvent{Time: "2026-01-01T00:00:00Z", Actor: actor, Action: "login.success", Detail: "{}"},
			&AuditEvent{Time: fmt.Sprintf("2026-02-%02dT12:00:00Z", 1+i%28), Actor: actor, Action: "login.success", Detail: "{}"},
			// noise the aggregation must skip
			&AuditEvent{Time: "2026-09-09T00:00:00Z", Actor: actor, Action: "deploy", Detail: "{}"},
		)
	}
	st := lastActionStore(t, seed...)
	got, err := st.Audits().LastActionTimes(context.Background(), "login.success")
	if err != nil {
		t.Fatalf("LastActionTimes: %v", err)
	}
	if len(got) != users {
		t.Fatalf("len = %d, want %d", len(got), users)
	}
	for i := 0; i < users; i++ {
		actor := fmt.Sprintf("user-%03d", i)
		want := fmt.Sprintf("2026-02-%02dT12:00:00Z", 1+i%28)
		if got[actor] != want {
			t.Fatalf("actor %q = %q, want %q", actor, got[actor], want)
		}
	}
}

// TestLastActionTimesPlanUsesIndex pins the access path: the equality
// filter seeks through the 004 idx_audit_action(action,time) composite and
// never full-scans audit_events (the GE-01 posture the Query face already
// pins for its own shapes; the GROUP BY rides the same index).
func TestLastActionTimesPlanUsesIndex(t *testing.T) {
	st := lastActionStore(t)
	db := st.(*sqliteStore).db
	plans := planOf(t, db, `SELECT actor, MAX(time) FROM audit_events WHERE action = ? GROUP BY actor`, "login.success")
	joined := strings.Join(plans, " | ")
	if !strings.Contains(joined, "idx_audit_action") {
		t.Fatalf("plan %q does not use idx_audit_action", joined)
	}
	if strings.Contains(joined, "SCAN audit_events") {
		t.Fatalf("plan %q full-scans audit_events", joined)
	}
}
