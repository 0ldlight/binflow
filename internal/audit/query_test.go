package audit_test

// T-93: the full-parameter query plane (GE-01) — filter matrix, closed-open
// time window, keyset cursor follow, limit defaults — plus the GE-02
// vocabulary round-trip and the append-only source scan (NFR-S21).

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// queryFixture seeds ten events with explicit, strictly ordered times so
// window and ordering assertions are exact. Times share one fictional
// minute; the identity of an event is its Time string.
type queryFixture struct {
	lg audit.Logger
}

// tm builds a fixture timestamp: tm(3) is 2026-08-19T10:00:03Z.
func tm(second int) string {
	return "2026-08-19T10:00:" + fmt.Sprintf("%02d", second) + "Z"
}

func seedQueryFixture(t *testing.T) *queryFixture {
	t.Helper()
	lg, _ := newLogger(t, true)
	seed := []audit.Event{
		{Time: tm(0), Actor: "jane", Action: audit.ActionDeploy, Repo: "generic-local", Path: "a.bin"},
		{Time: tm(1), Actor: "ci-bot", Action: audit.ActionDelete, Repo: "generic-local", Path: "b.bin"},
		{Time: tm(2), Actor: audit.ActorAnonymous, Action: audit.ActionDownload, Repo: "docker-local", Path: "img/manifests/sha256:aa"},
		{Time: tm(3), Actor: "jane", Action: audit.ActionAuthFail},
		{Time: tm(4), Actor: "admin", Action: audit.ActionRepoCreate, Repo: "generic-local"},
		{Time: tm(5), Actor: "admin", Action: audit.ActionGroupCreate},
		{Time: tm(6), Actor: "admin", Action: audit.ActionGCRun},
		{Time: tm(7), Actor: "ci-bot", Action: audit.ActionQuotaExceeded, Repo: "generic-local", Path: "big.bin"},
		{Time: tm(8), Actor: "jane", Action: audit.ActionDeploy, Repo: "docker-local", Path: "img/manifests/sha256:bb"},
		{Time: tm(9), Actor: "admin", Action: audit.ActionExportRun},
	}
	ctx := context.Background()
	for _, e := range seed {
		if err := lg.Append(ctx, e); err != nil {
			t.Fatalf("seed append %s/%s: %v", e.Time, e.Action, err)
		}
	}
	return &queryFixture{lg: lg}
}

// times extracts the event identity list, newest-first.
func times(p *audit.Page) []string {
	out := make([]string, 0, len(p.Events))
	for _, e := range p.Events {
		out = append(out, e.Time)
	}
	return out
}

func equalTimes(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// assertDescending verifies the (time DESC, id DESC) contract — strictly
// descending times here because the fixture times are distinct.
func assertDescending(t *testing.T, p *audit.Page) {
	t.Helper()
	for i := 1; i < len(p.Events); i++ {
		if p.Events[i-1].Time <= p.Events[i].Time {
			t.Fatalf("events not strictly newest-first: %v", times(p))
		}
	}
}

// TestQueryFilterMatrix walks every filter shape over the fixed fixture
// (GE-01: actor/action/repo equality, closed-open since/until window,
// combined filters, default newest-first ordering, unknown action).
func TestQueryFilterMatrix(t *testing.T) {
	f := seedQueryFixture(t)
	ctx := context.Background()

	tests := []struct {
		name   string
		filter audit.Filter
		want   []string // event times, newest first
	}{
		{name: "no filter returns all newest-first", filter: audit.Filter{},
			want: []string{tm(9), tm(8), tm(7), tm(6), tm(5), tm(4), tm(3), tm(2), tm(1), tm(0)}},
		{name: "actor", filter: audit.Filter{Actor: "jane"},
			want: []string{tm(8), tm(3), tm(0)}},
		{name: "action", filter: audit.Filter{Action: audit.ActionDeploy},
			want: []string{tm(8), tm(0)}},
		{name: "repo", filter: audit.Filter{Repo: "generic-local"},
			want: []string{tm(7), tm(4), tm(1), tm(0)}},
		{name: "actor and action", filter: audit.Filter{Actor: "jane", Action: audit.ActionDeploy},
			want: []string{tm(8), tm(0)}},
		{name: "actor and repo", filter: audit.Filter{Actor: "ci-bot", Repo: "generic-local"},
			want: []string{tm(7), tm(1)}},
		// Closed-open window (PRD GE-01): since inclusive, until exclusive —
		// both bounds sit exactly ON fixture events.
		{name: "since inclusive at boundary", filter: audit.Filter{Since: tm(4)},
			want: []string{tm(9), tm(8), tm(7), tm(6), tm(5), tm(4)}},
		{name: "until exclusive at boundary", filter: audit.Filter{Until: tm(4)},
			want: []string{tm(3), tm(2), tm(1), tm(0)}},
		{name: "window since+until", filter: audit.Filter{Since: tm(4), Until: tm(8)},
			want: []string{tm(7), tm(6), tm(5), tm(4)}},
		{name: "empty window", filter: audit.Filter{Since: tm(5), Until: tm(5)},
			want: []string{}},
		{name: "window combined with actor", filter: audit.Filter{Actor: "admin", Since: tm(5), Until: tm(9)},
			want: []string{tm(6), tm(5)}},
		{name: "unknown action matches nothing", filter: audit.Filter{Action: "nonsense.action"},
			want: []string{}},
		{name: "unknown actor matches nothing", filter: audit.Filter{Actor: "nobody"},
			want: []string{}},
		{name: "limit caps the page", filter: audit.Filter{Limit: 3},
			want: []string{tm(9), tm(8), tm(7)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := f.lg.Query(ctx, tt.filter)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if !equalTimes(times(p), tt.want...) {
				t.Fatalf("times = %v, want %v", times(p), tt.want)
			}
			assertDescending(t, p)
			if len(tt.want) == 0 && p.Events == nil {
				t.Fatalf("empty page Events must be non-nil")
			}
			// Every event carries its row id (the HTTP plane renders it).
			for _, e := range p.Events {
				if e.ID <= 0 {
					t.Fatalf("event %s has id %d, want a positive row id", e.Time, e.ID)
				}
			}
		})
	}
}

// TestQueryCursorFollowPages paginates the whole fixture through the keyset
// cursor: pages tile the result without gaps or repeats, stay newest-first
// across page boundaries, and the last page carries no cursor.
func TestQueryCursorFollowPages(t *testing.T) {
	f := seedQueryFixture(t)
	ctx := context.Background()

	full, err := f.lg.Query(ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("full query: %v", err)
	}
	fullTimes := times(full)

	var walked []string
	pages := 0
	cursor := ""
	for {
		p, err := f.lg.Query(ctx, audit.Filter{Limit: 3, Cursor: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", pages+1, err)
		}
		walked = append(walked, times(p)...)
		pages++
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
		if pages > 10 {
			t.Fatalf("cursor did not terminate after %d pages", pages)
		}
	}
	if pages != 4 { // 10 events, 3 per page
		t.Fatalf("pages = %d, want 4", pages)
	}
	if !equalTimes(walked, fullTimes...) {
		t.Fatalf("paged walk %v != full query %v", walked, fullTimes)
	}
	seen := map[string]bool{}
	for _, tm := range walked {
		if seen[tm] {
			t.Fatalf("event %s appeared twice across pages", tm)
		}
		seen[tm] = true
	}

	// A page that exactly fills the limit still reports a cursor (the
	// has-more probe read one extra row); only the true tail is cursorless.
	exact, err := f.lg.Query(ctx, audit.Filter{Limit: 10})
	if err != nil {
		t.Fatalf("exact page: %v", err)
	}
	if exact.NextCursor != "" {
		t.Fatalf("full-result page has cursor %q, want none", exact.NextCursor)
	}
}

// TestQueryDefaultLimit: Limit <= 0 means 100 (the GE-01 default), and the
// 101st row's existence is visible through NextCursor.
func TestQueryDefaultLimit(t *testing.T) {
	lg, _ := newLogger(t, true)
	ctx := context.Background()
	for i := 0; i < 130; i++ {
		if err := lg.Append(ctx, audit.Event{
			Actor: "flood", Action: audit.ActionDownload, Repo: "r", Path: "p",
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	for _, limit := range []int{0, -5} {
		p, err := lg.Query(ctx, audit.Filter{Limit: limit})
		if err != nil {
			t.Fatalf("Query(limit=%d): %v", limit, err)
		}
		if len(p.Events) != 100 {
			t.Fatalf("limit=%d returned %d events, want the 100 default", limit, len(p.Events))
		}
		if p.NextCursor == "" {
			t.Fatalf("limit=%d: 130 rows exist, page must carry nextCursor", limit)
		}
	}
}

// TestQueryInvalidCursor: a cursor this store never issued is an error the
// HTTP plane maps to 400 (metadata.ErrInvalidCursor — distinct from
// repo.ErrInvalidCursor of the docker pagination plane).
func TestQueryInvalidCursor(t *testing.T) {
	f := seedQueryFixture(t)
	_, err := f.lg.Query(context.Background(), audit.Filter{Cursor: "garbage"})
	if err == nil {
		t.Fatalf("garbage cursor accepted")
	}
	if !errors.Is(err, metadata.ErrInvalidCursor) {
		t.Fatalf("err = %v, want metadata.ErrInvalidCursor", err)
	}
	// The empty cursor is the first page, not an error.
	if p, err := f.lg.Query(context.Background(), audit.Filter{Cursor: ""}); err != nil || len(p.Events) != 10 {
		t.Fatalf("empty cursor: %v %d", err, len(p.Events))
	}
}

// TestNormalizeTimestamp fixes the Since/Until parameter normalization
// (T-90 review note): parse RFC3339, re-emit as UTC, floor sub-seconds.
func TestNormalizeTimestamp(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: ""},
		{in: "2026-08-19T10:00:00Z", want: "2026-08-19T10:00:00Z"},
		{in: "2026-08-19T12:00:00+02:00", want: "2026-08-19T10:00:00Z"},
		{in: "2026-08-19T08:00:00-02:00", want: "2026-08-19T10:00:00Z"},
		{in: "2026-08-19T10:00:00.75Z", want: "2026-08-19T10:00:00Z"},
		{in: "2026-08-19T10:00:00.75+02:00", want: "2026-08-19T08:00:00Z"},
		{in: "2026-08-19", wantErr: true},
		{in: "yesterday", wantErr: true},
		{in: "2026-13-40T99:00:00Z", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := audit.NormalizeTimestamp(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeTimestamp(%q) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeTimestamp(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeTimestamp(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if got != "" {
				if _, err := time.Parse(time.RFC3339, got); err != nil {
					t.Fatalf("normalized %q is not RFC3339", got)
				}
			}
		})
	}
}

// TestVocabularyQueryable is the GE-02 query-face stub: every action in the
// M1~M4 vocabulary is appendable and filterable, and the list carries no
// duplicates (W23's assertions run against exactly this set).
func TestVocabularyQueryable(t *testing.T) {
	lg, _ := newLogger(t, true)
	ctx := context.Background()

	vocab := audit.Actions()
	seen := map[string]bool{}
	for _, a := range vocab {
		if a == "" {
			t.Fatalf("vocabulary contains an empty action")
		}
		if seen[a] {
			t.Fatalf("vocabulary contains %q twice", a)
		}
		seen[a] = true
		if err := lg.Append(ctx, audit.Event{Actor: "op", Action: a}); err != nil {
			t.Fatalf("append %s: %v", a, err)
		}
		p, err := lg.Query(ctx, audit.Filter{Action: a, Limit: 1})
		if err != nil || len(p.Events) != 1 {
			t.Fatalf("filter %s: %v (%d events)", a, err, len(p.Events))
		}
		if p.Events[0].Action != a {
			t.Fatalf("filter %s returned action %q", a, p.Events[0].Action)
		}
	}
	// The M4 additions named by the PRD are all present.
	for _, a := range []string{
		audit.ActionGroupCreate, audit.ActionGroupUpdate, audit.ActionGroupDelete,
		audit.ActionPermissionCreate, audit.ActionPermissionUpdate, audit.ActionPermissionDelete,
		audit.ActionGCRun, audit.ActionExportRun, audit.ActionImportRun, audit.ActionQuotaExceeded,
	} {
		if !seen[a] {
			t.Fatalf("M4 action %q missing from Actions()", a)
		}
	}
	// T-346 (FR-113.4): the accumulated planes join the picker — every
	// action an emit site spells is filterable now (L31's picker-visibility
	// assertion; the families' provenance is the api.go block comment).
	for _, a := range []string{
		audit.ActionPropsWrite, audit.ActionPropsDelete,
		audit.ActionReplicationPush, audit.ActionReplicationPushFailed,
		audit.ActionReplicationCfgCreate, audit.ActionReplicationCfgDelete,
		audit.ActionKeypairCreate, audit.ActionKeypairUpdate, audit.ActionKeypairGenerate,
		audit.ActionKeypairDelete, audit.ActionKeypairVerify, audit.ActionKeypairAssociate,
		audit.ActionAuthConfigUpdate, audit.ActionAuthConfigTest,
		audit.ActionSAMLKeyGenerate, audit.ActionSAMLKeyRegen,
		audit.ActionLicenseInstall, audit.ActionLicenseDelete, audit.ActionLicenseInvalid,
		audit.ActionLicenseAddonDeny,
		audit.ActionArtifactCopy, audit.ActionArtifactMove, audit.ActionArtifactExplode,
		audit.ActionTrashRestore, audit.ActionTrashEmpty, audit.ActionTrashClean, audit.ActionTrashRetention,
		audit.ActionStorageReplayWindow, audit.ActionStorageReplayDrain,
	} {
		if !seen[a] {
			t.Fatalf("T-346 action %q missing from Actions()", a)
		}
	}
	// M15 T-422 (replication.md §9.4 #1~#4): the replication config family's
	// batch — the T-405 PUT-enabled word (legacy #1, finally registered),
	// the T-420 trigger word (#3) and the two new faces (test #2, the
	// global block flip #4).
	for _, a := range []string{
		audit.ActionReplicationCfgUpdate, audit.ActionReplicationCfgTest,
		audit.ActionReplicationRun, audit.ActionReplicationBlockUpdate,
	} {
		if !seen[a] {
			t.Fatalf("T-422 action %q missing from Actions()", a)
		}
	}
	// M16 T-446 (FR-150.2 / ADR-0044 decision 10): the cron scheduler's
	// nine words — one set/run/fail triple per consuming domain.
	for _, a := range []string{
		audit.ActionMaintenanceScheduleSet, audit.ActionMaintenanceScheduleRun, audit.ActionMaintenanceScheduleFail,
		audit.ActionBackupScheduleSet, audit.ActionBackupScheduleRun, audit.ActionBackupScheduleFail,
		audit.ActionReplicationScheduleSet, audit.ActionReplicationScheduleRun, audit.ActionReplicationScheduleFail,
	} {
		if !seen[a] {
			t.Fatalf("T-446 action %q missing from Actions()", a)
		}
	}
}
