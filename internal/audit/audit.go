// Package audit appends repository and security events to an append-only
// log and serves filtered queries (architecture section 3.5).
//
// Append is best-effort by contract: an audit failure is logged (slog,
// structured, never containing credentials — NFR-S3) but does not block the
// business operation it describes (architecture section 11 item 4 records
// this as a deliberate M1 compromise; strict two-phase audit is M4+).
//
// Layout:
//
//	audit.go   Logger implementation over metadata.AuditStore
//	api.go     Event/Filter types and the Logger interface
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// Logger is the audit contract (architecture section 3.5).
type Logger interface {
	// Append records one event. The error is returned for tests and for
	// callers that want it; production paths treat the call as best-effort
	// (Append itself never panics and the recommended wrapper is Record).
	Append(ctx context.Context, e Event) error
	// Query returns one page of events newest-first matching the filter
	// (full-parameter keyset form, GE-01/T-93).
	Query(ctx context.Context, f Filter) (*Page, error)
}

// store is the consumer-side slice of metadata.AuditStore. The legacy List
// seam was retired when T-93 moved the query plane onto the
// full-parameter keyset Query; metadata keeps serving List for its own
// compatibility, this package no longer consumes it. LastActionTimes is the
// FR-146.3 aggregation seam (M16): one GROUP BY statement behind the
// LastLogins derivation.
type store interface {
	Append(ctx context.Context, e *metadata.AuditEvent) error
	Query(ctx context.Context, q metadata.AuditQuery) ([]*metadata.AuditEvent, error)
	LastActionTimes(ctx context.Context, action string) (map[string]string, error)
}

// logger implements Logger over the metadata audit store.
type logger struct {
	st      store
	enabled bool // config.Audit.Enabled
}

// New builds the Logger. enabled is config.Audit.Enabled; when false,
// Append succeeds immediately without touching the store (audit off is a
// documented posture, not an error).
func New(st metadata.Store, enabled bool) Logger {
	return &logger{st: st.Audits(), enabled: enabled}
}

var _ Logger = (*logger)(nil)

// Append fills server-side fields (Time, Actor default), redacts the Detail
// payload and persists the event. Time is stamped here when the caller left
// it empty so every event carries a server clock reading even from clients
// that cannot be trusted with timestamps. RemoteAddr is merged into the
// stored Detail (the row schema has no column for it, see api.go).
func (l *logger) Append(ctx context.Context, e Event) error {
	if !l.enabled {
		return nil
	}
	if e.Time == "" {
		e.Time = metadata.Now()
	}
	if e.Actor == "" {
		e.Actor = ActorAnonymous
	}
	e = Redact(e)
	if e.RemoteAddr != "" {
		e.Detail = mergeDetail(e.Detail, "remote_addr", e.RemoteAddr)
	}
	return l.st.Append(ctx, &metadata.AuditEvent{
		Time: e.Time, Actor: e.Actor, Action: e.Action,
		RepoKey: e.Repo, Path: e.Path, Detail: e.Detail,
	})
}

// mergeDetail sets key in the Detail JSON object (created when malformed or
// empty — the merge must never drop the event).
func mergeDetail(detail, key, value string) string {
	m := map[string]any{}
	if detail != "" {
		_ = json.Unmarshal([]byte(detail), &m) // malformed: start fresh
	}
	m[key] = value
	b, err := json.Marshal(m)
	if err != nil {
		return detail
	}
	return string(b)
}

// Record is the best-effort wrapper production code should call: the
// append error is logged with the event coordinates (never its Detail
// payload, which may carry request context) and swallowed.
func (l *logger) Record(ctx context.Context, e Event) {
	if err := l.Append(ctx, e); err != nil {
		slog.ErrorContext(ctx, "audit: append failed (best-effort, continuing)",
			slog.String("action", e.Action),
			slog.String("actor", e.Actor),
			slog.String("repo", e.Repo),
			slog.String("path", e.Path),
			slog.String("error", err.Error()))
	}
}

// defaultQueryLimit mirrors metadata.AuditQuery's default and the GE-01
// HTTP contract: a page holds 100 events unless the caller says otherwise.
const defaultQueryLimit = 100

// auditCursorSeparator joins the cursor tuple "<time>|<id>" — the format
// metadata.AuditQuery documents (RFC3339 UTC timestamps never contain
// '|'). Building it here reads that documented contract from the consumer
// side; TestQueryCursorFollowPages round-trips through the real store and
// fails the moment the two sides drift apart.
const auditCursorSeparator = "|"

// cursorOf renders e's keyset position as the opaque cursor the next page
// passes back through Filter.Cursor.
func cursorOf(e Event) string {
	return e.Time + auditCursorSeparator + strconv.FormatInt(e.ID, 10)
}

// Query returns one page of events newest-first (time DESC, id DESC)
// matching the filter (GE-01). The page holds at most f.Limit rows
// (default 100). NextCursor carries the position of the following page
// and is empty on the last page — detected by fetching one row beyond the
// limit, so a page that exactly fills the limit is still distinguishable
// from a terminal one.
func (l *logger) Query(ctx context.Context, f Filter) (*Page, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultQueryLimit
	}
	fetch := limit + 1
	if fetch <= 0 {
		// An absurd limit overflowed the increment: ask for it verbatim;
		// the has-more probe degrades to "full page implies a cursor".
		fetch = limit
	}
	rows, err := l.st.Query(ctx, metadata.AuditQuery{
		RepoKey: f.Repo, Actor: f.Actor, Action: f.Action,
		Since: f.Since, Until: f.Until, Cursor: f.Cursor, Limit: fetch,
	})
	if err != nil {
		return nil, err
	}
	page := &Page{Events: make([]Event, 0, len(rows))}
	for _, r := range rows {
		page.Events = append(page.Events, Event{
			ID: r.ID, Time: r.Time, Actor: r.Actor, Action: r.Action,
			Repo: r.RepoKey, Path: r.Path, Detail: r.Detail,
		})
	}
	if len(page.Events) > limit {
		page.Events = page.Events[:limit]
		page.NextCursor = cursorOf(page.Events[limit-1])
	}
	return page, nil
}

// LastLogins derives every user's most recent successful login from the
// audit log (FR-146.3, M16): the login.success rows this very package's
// Append records (the console session and OIDC callback planes) collapse
// into one actor -> RFC3339 UTC time entry each, via a single GROUP BY
// query on the store. The derivation runs at QUERY time, deliberately not
// materialized: no schema change, no write-path coupling, and the
// append-only log stays the single source of truth — a re-derivation can
// never disagree with the stored trail (the ruling M16-SPLIT T-454 AC1
// asks to pin). Users without a login history simply have no map entry.
// The method lives on the concrete logger (not the Logger interface): the
// httpapi users-list projection discovers it as a facet, the
// session/permView/stepUp discovery precedent — bare Logger fakes stay
// untouched and their stacks render the field absent.
func (l *logger) LastLogins(ctx context.Context) (map[string]string, error) {
	times, err := l.st.LastActionTimes(ctx, ActionLoginOK)
	if err != nil {
		return nil, fmt.Errorf("audit: derive last logins: %w", err)
	}
	return times, nil
}

// BestEffort returns a Recorder around any Logger, so callers depending on
// the interface (repo.Service, httpapi) get the swallow-and-log behavior
// without depending on the concrete type.
func BestEffort(l Logger) Recorder { return recorder{l: l} }

// Recorder is the best-effort facet of Logger.
type Recorder interface {
	Record(ctx context.Context, e Event)
}

type recorder struct{ l Logger }

func (r recorder) Record(ctx context.Context, e Event) {
	if err := r.l.Append(ctx, e); err != nil {
		slog.ErrorContext(ctx, "audit: append failed (best-effort, continuing)",
			slog.String("action", e.Action),
			slog.String("actor", e.Actor),
			slog.String("repo", e.Repo),
			slog.String("path", e.Path),
			slog.String("error", err.Error()))
	}
}
