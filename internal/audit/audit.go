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
	"log/slog"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// Logger is the audit contract (architecture section 3.5).
type Logger interface {
	// Append records one event. The error is returned for tests and for
	// callers that want it; production paths treat the call as best-effort
	// (Append itself never panics and the recommended wrapper is Record).
	Append(ctx context.Context, e Event) error
	// Query returns events newest-first matching the filter.
	Query(ctx context.Context, f Filter) ([]Event, error)
}

// store is the consumer-side slice of metadata.AuditStore.
type store interface {
	Append(ctx context.Context, e *metadata.AuditEvent) error
	List(ctx context.Context, repoKey, actor string, limit int) ([]*metadata.AuditEvent, error)
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

// Query returns events newest-first, at most f.Limit rows (default 100),
// optionally narrowed by repo and actor.
func (l *logger) Query(ctx context.Context, f Filter) ([]Event, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := l.st.List(ctx, f.Repo, f.Actor, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Event, len(rows))
	for i, r := range rows {
		out[i] = Event{
			Time: r.Time, Actor: r.Actor, Action: r.Action,
			Repo: r.RepoKey, Path: r.Path, Detail: r.Detail,
		}
	}
	return out, nil
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
