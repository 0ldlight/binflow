package audit_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
)

func newLogger(t *testing.T, enabled bool) (audit.Logger, metadata.Store) {
	t.Helper()
	st, err := metadata.Open(context.Background(), metadata.Options{
		Driver:        "sqlite",
		Path:          filepath.Join(t.TempDir(), "binflow.db"),
		AdminPassword: "test-admin-pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return audit.New(st, enabled), st
}

// AC 4: events carry actor/action/repo/path (+remote_addr in detail)/time
// and round-trip through the store shape.
func TestAppendAndQuery(t *testing.T) {
	lg, _ := newLogger(t, true)
	ctx := context.Background()

	err := lg.Append(ctx, audit.Event{
		Actor: "ci-bot", Action: audit.ActionDeploy,
		Repo: "generic-local", Path: "ci-out/y.bin",
		RemoteAddr: "10.0.0.7:44312",
		Detail:     `{"size":128}`,
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	err = lg.Append(ctx, audit.Event{
		Actor: audit.ActorAnonymous, Action: audit.ActionDownload,
		Repo: "generic-local", Path: "acme/artifact.bin",
	})
	if err != nil {
		t.Fatalf("Append #2: %v", err)
	}

	events, err := lg.Query(ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	// newest-first
	if events[0].Action != audit.ActionDownload {
		t.Fatalf("first event = %q, want %q (newest first)", events[0].Action, audit.ActionDownload)
	}

	first := events[1] // the deploy event
	if first.Actor != "ci-bot" || first.Repo != "generic-local" || first.Path != "ci-out/y.bin" {
		t.Fatalf("deploy event fields = %+v", first)
	}
	if first.Time == "" || !strings.Contains(first.Time, "T") {
		t.Fatalf("time not RFC3339-stamped: %q", first.Time)
	}
	if !strings.Contains(first.Detail, `"remote_addr":"10.0.0.7:44312"`) {
		t.Fatalf("remote_addr not merged into detail: %q", first.Detail)
	}
	if !strings.Contains(first.Detail, `"size":128`) {
		t.Fatalf("existing detail keys lost: %q", first.Detail)
	}

	// Anonymous default actor was applied.
	if events[0].Actor != audit.ActorAnonymous {
		t.Fatalf("anonymous event actor = %q", events[0].Actor)
	}

	// Filters narrow.
	byRepo, err := lg.Query(ctx, audit.Filter{Repo: "nope"})
	if err != nil || len(byRepo) != 0 {
		t.Fatalf("repo filter: %v %d", err, len(byRepo))
	}
	byActor, err := lg.Query(ctx, audit.Filter{Actor: "ci-bot"})
	if err != nil || len(byActor) != 1 {
		t.Fatalf("actor filter: %v %d", err, len(byActor))
	}
	limited, err := lg.Query(ctx, audit.Filter{Limit: 1})
	if err != nil || len(limited) != 1 {
		t.Fatalf("limit: %v %d", err, len(limited))
	}
}

// AC 4: disabled audit is a no-op, not an error.
func TestAppendDisabled(t *testing.T) {
	lg, st := newLogger(t, false)
	ctx := context.Background()
	if err := lg.Append(ctx, audit.Event{Actor: "x", Action: "deploy"}); err != nil {
		t.Fatalf("Append disabled: %v", err)
	}
	events, err := lg.Query(ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("disabled logger stored %d events", len(events))
	}
	_ = st
}

// Best-effort: a failing store is logged and swallowed, the operation
// continues.
func TestBestEffortSwallowsErrors(_ *testing.T) {
	failing := &failingStore{}
	rec := audit.BestEffort(audit.New(failing, true))
	rec.Record(context.Background(), audit.Event{Action: "deploy", Actor: "a"}) // must not panic
}

type failingStore struct{ metadata.Store }

func (f *failingStore) Audits() metadata.AuditStore { return &failingAudits{} }

type failingAudits struct{ metadata.AuditStore }

func (f *failingAudits) Append(_ context.Context, _ *metadata.AuditEvent) error {
	return errors.New("disk on fire")
}

func (f *failingAudits) List(_ context.Context, _, _ string, _ int) ([]*metadata.AuditEvent, error) {
	return nil, errors.New("disk on fire")
}

// NFR-S3: credential-shaped detail keys are masked before storage.
// M-1: key matching is case-insensitive and covers the protocol's
// camelCase field names (auth-model.md section 2.1) and header spellings.
func TestRedact(t *testing.T) {
	tests := []struct {
		name   string
		detail string
		want   string // substring assertions
		banned []string
	}{
		{
			name:   "password masked",
			detail: `{"password":"hunter2","note":"ok"}`,
			want:   `"[REDACTED]"`,
			banned: []string{"hunter2"},
		},
		{
			name:   "token masked",
			detail: `{"access_token":"abc123","user":"ci"}`,
			want:   `"[REDACTED]"`,
			banned: []string{"abc123"},
		},
		{
			name:   "camelCase oldPassword masked (protocol field)",
			detail: `{"userName":"ci-bot","oldPassword":"p1","newPassword1":"p2","newPassword2":"p2"}`,
			want:   `"[REDACTED]"`,
			banned: []string{"p1", "p2"},
		},
		{
			name:   "header-name keys masked",
			detail: `{"Authorization":"Basic dXNlcjpwdw==","X-JFrog-Art-Api":"tok","api-key":"k"}`,
			want:   `"[REDACTED]"`,
			banned: []string{"dXNlcjpwdw==", "tok"},
		},
		{
			name:   "mixed case key masked",
			detail: `{"Password":"upper"}`,
			want:   `"[REDACTED]"`,
			banned: []string{"upper"},
		},
		{
			name:   "empty normalized",
			detail: "",
			want:   "{}",
			banned: nil,
		},
		{
			name:   "non-object stored as-is",
			detail: `"plain"`,
			want:   `"plain"`,
			banned: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := audit.Redact(audit.Event{Detail: tt.detail}).Detail
			if !strings.Contains(got, tt.want) {
				t.Fatalf("redacted detail %q lacks %q", got, tt.want)
			}
			for _, b := range tt.banned {
				if strings.Contains(got, b) {
					t.Fatalf("redacted detail %q still contains %q", got, b)
				}
			}
		})
	}
}

// End to end: a redacted event never persists the credential.
func TestAppendRedactsBeforeStorage(t *testing.T) {
	lg, st := newLogger(t, true)
	ctx := context.Background()
	err := lg.Append(ctx, audit.Event{
		Actor: "ci-bot", Action: audit.ActionLoginOK, Detail: `{"password":"s3cret-pw"}`,
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	events, err := lg.Query(ctx, audit.Filter{})
	if err != nil || len(events) != 1 {
		t.Fatalf("query: %v %d", err, len(events))
	}
	if strings.Contains(events[0].Detail, "s3cret-pw") {
		t.Fatalf("credential persisted: %q", events[0].Detail)
	}
	_ = st
}
