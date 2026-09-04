package audit_test

// FR-146.3 (M16, T-454): the LastLogins derivation — the login.success
// rows the Logger itself records collapse into one most-recent-login entry
// per user, from a single GROUP BY. Table-driven over the real store; the
// facet is asserted through the audit.New return (Logger interface) so the
// httpapi discovery shape — assertion on the interface value — is exercised
// the way the server does it.

import (
	"context"
	"testing"

	"github.com/lzwzzy/binflow/internal/audit"
)

// deriveLastLogins appends the events, then derives. Events with an empty
// Time get the Logger's server stamp — same as the login plane.
func deriveLastLogins(t *testing.T, lg audit.Logger, events ...audit.Event) map[string]string {
	t.Helper()
	ctx := context.Background()
	for _, e := range events {
		if err := lg.Append(ctx, e); err != nil {
			t.Fatalf("Append %+v: %v", e, err)
		}
	}
	src, ok := lg.(interface {
		LastLogins(context.Context) (map[string]string, error)
	})
	if !ok {
		t.Fatal("audit.New's logger does not carry the LastLogins facet — the httpapi discovery would render lastLoggedIn absent on every stack")
	}
	got, err := src.LastLogins(ctx)
	if err != nil {
		t.Fatalf("LastLogins: %v", err)
	}
	return got
}

func TestLastLoginsDerivation(t *testing.T) {
	tests := []struct {
		name   string
		events []audit.Event
		want   map[string]string // nil = empty derivation expected
	}{
		{
			name: "multiple logins collapse to the newest per user",
			events: []audit.Event{
				{Actor: "alice", Action: audit.ActionLoginOK, Time: "2026-01-01T00:00:00Z"},
				{Actor: "bob", Action: audit.ActionLoginOK, Time: "2026-01-02T00:00:00Z"},
				{Actor: "alice", Action: audit.ActionLoginOK, Time: "2026-03-04T05:06:07Z"},
				{Actor: "bob", Action: audit.ActionLoginOK, Time: "2026-02-02T00:00:00Z"},
			},
			want: map[string]string{
				"alice": "2026-03-04T05:06:07Z",
				"bob":   "2026-02-02T00:00:00Z",
			},
		},
		{
			name: "failures and other actions never count as logins",
			events: []audit.Event{
				{Actor: "carol", Action: audit.ActionAuthFail, Time: "2026-06-06T00:00:00Z"},
				{Actor: "alice", Action: audit.ActionDeploy, Time: "2026-12-31T23:59:59Z"},
				{Actor: "dave", Action: audit.ActionTokenIssue, Time: "2026-07-07T00:00:00Z"},
			},
			want: nil,
		},
		{
			name: "a login among noise still lands with its own time",
			events: []audit.Event{
				{Actor: "dave", Action: audit.ActionTokenIssue, Time: "2026-07-07T00:00:00Z"},
				{Actor: "dave", Action: audit.ActionLoginOK, Time: "2026-07-08T00:00:00Z"},
				{Actor: "dave", Action: audit.ActionAuthFail, Time: "2026-07-09T00:00:00Z"},
			},
			want: map[string]string{"dave": "2026-07-08T00:00:00Z"},
		},
		{
			name:   "an empty log derives an empty map",
			events: nil,
			want:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lg, _ := newLogger(t, true)
			got := deriveLastLogins(t, lg, tt.events...)
			if len(got) != len(tt.want) {
				t.Fatalf("derivation = %v, want %v", got, tt.want)
			}
			for actor, wantTime := range tt.want {
				if got[actor] != wantTime {
					t.Errorf("user %q = %q, want %q", actor, got[actor], wantTime)
				}
			}
		})
	}
}

// TestLastLoginsAfterRealLoginPlane: the derivation reads back exactly what
// the login plane writes — server-stamped times (empty Time on Append),
// RFC3339 UTC — so the users-list projection can pass the value through
// verbatim. This is the shape lock: if the stamp format ever drifted from
// RFC3339 UTC the list column would render a foreign spelling.
func TestLastLoginsAfterRealLoginPlane(t *testing.T) {
	lg, _ := newLogger(t, true)
	got := deriveLastLogins(t, lg,
		audit.Event{Actor: "eve", Action: audit.ActionLoginOK, Detail: audit.AuthEventDetail("local", "")},
	)
	if len(got) != 1 {
		t.Fatalf("derivation = %v, want exactly eve", got)
	}
	ts := got["eve"]
	if len(ts) != len("2006-01-02T15:04:05Z") || ts[len(ts)-1] != 'Z' {
		t.Fatalf("eve's last login = %q, want second-granular RFC3339 UTC (metadata.Now shape)", ts)
	}
}

// TestLastLoginsDisabledAuditSeesNoLogins: with audit disabled Append is a
// documented no-op, so no login trail exists and the derivation is empty —
// the projection then renders lastLoggedIn absent for everyone, the honest
// reading of "nothing was recorded", not an error.
func TestLastLoginsDisabledAuditSeesNoLogins(t *testing.T) {
	lg, _ := newLogger(t, false)
	got := deriveLastLogins(t, lg,
		audit.Event{Actor: "mallory", Action: audit.ActionLoginOK},
	)
	if len(got) != 0 {
		t.Fatalf("derivation = %v, want empty (disabled audit records nothing)", got)
	}
}
