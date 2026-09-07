package httpapi

// GET /api/v1/system/logs — the System Logs process-log tail face (M17
// T-493, FR-157③ / rest-compat-matrix D06 row 13): the service's own log
// stream, tailed, filtered and downloadable. This closes T-459's contract
// drift ("System Logs rides the audit trail — no process-log endpoint"): the
// audit face stays (it answers a different question — WHO did WHAT), and the
// console viewer gains a real process-log source to switch to (T-494's FE
// leg).
//
// Endpoint form (the ticket's ruling, following the v1-system-plane family
// precedent — settings/schedules/backups are BinFlow-native C-layer rows the
// compat matrix carries beside their Artifactory A-layer cousins): the
// Artifactory live-logs face is a medium-confidence internal UI surface
// (inv-1-core.md "system/logs/config|data", ui/.../systemlogs/*), so BinFlow
// ships its own spelling /api/v1/system/logs and the A-layer alias stays a
// future ruling. Capabilities, one route, three arms:
//
//	GET /api/v1/system/logs?limit=N&filter=substr[&download=1]
//
//   - tail: the LAST limit lines (1..1000, default 200), oldest→newest —
//     the audit family's limit convention;
//   - filter: a server-side case-sensitive substring applied BEFORE the
//     window cut (the tail of the MATCHING stream), capped at 256 chars —
//     plain Contains, never a regex;
//   - download: the same window served as text/plain with
//     Content-Disposition attachment (binflow-service.log).
//
// Read-safety: the gate is system:read (the audit read's posture —
// readonly_admin may read the process log, a plain user 403s); the source
// ring never carries credentials (it mirrors the process logger, whose
// access lines record method/path/status/actor only); both memory axes are
// bounded (ring lines and per-line bytes — internal/console/logtail.go).
//
// The capture is assembled HERE (the qrl/aql precedent — cmd's Deps wiring
// stays untouched): New wraps the incoming logger in a fan-out handler that
// renders every emitted record into the ring beside its real destination.
// The ring therefore covers the server-assembly-onward runtime stream (every
// access line and handler log); boot lines emitted before assembly stay on
// stderr only — a cmd-side wiring of Deps.ServiceLog can widen the window
// later without touching this file.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/lzwzzy/binflow/internal/console"
)

// The endpoint's parameter bounds (the audit family's limit shape).
const (
	// serviceLogDefaultLines is the tail window when ?limit is absent.
	serviceLogDefaultLines = 200
	// serviceLogMaxLines is the largest window one request may ask for.
	serviceLogMaxLines = 1000
	// serviceLogFilterMax caps the ?filter substring (a bounded parameter,
	// not an unbounded scan input).
	serviceLogFilterMax = 256
	// serviceLogFilename is the download arm's attachment name.
	serviceLogFilename = "binflow-service.log"
)

// SystemLogTail is the process-log reading seam behind
// GET /api/v1/system/logs (consumer-side interface; the sole production
// implementation is console.LogRing — the ticket's "日志读取面" ruling).
// TailLines returns up to last lines matching filter, oldest→newest, plus
// the ring's held-line count and whether older lines were evicted (the
// window-vs-history honesty the body's truncated field carries).
type SystemLogTail interface {
	TailLines(last int, filter string) (lines []string, held int, evicted bool)
	Capacity() int
}

// systemLogsBody is the JSON arm's body. Lines is never null (an empty ring
// answers []).
type systemLogsBody struct {
	Lines       []string `json:"lines"`
	Count       int      `json:"count"`
	Held        int      `json:"held"`
	Capacity    int      `json:"capacity"`
	Truncated   bool     `json:"truncated"`
	GeneratedAt string   `json:"generatedAt"`
}

// handleSystemLogsGet serves GET /api/v1/system/logs (see the file comment
// for the three arms). Parameter validation answers the envelope 400s; the
// download arm shares the exact same tail+filter resolution so what a
// viewer shows is what the download carries.
func (s *Server) handleSystemLogsGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := serviceLogDefaultLines
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > serviceLogMaxLines {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("limit must be an integer between 1 and %d", serviceLogMaxLines))
			return
		}
		limit = n
	}
	filter := q.Get("filter")
	if len(filter) > serviceLogFilterMax {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("filter must be at most %d characters", serviceLogFilterMax))
		return
	}

	lines, held, evicted := s.serviceLog.TailLines(limit, filter)
	if isTruthyFlag(q.Get("download")) {
		h := w.Header()
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Content-Disposition", `attachment; filename="`+serviceLogFilename+`"`)
		h.Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		for _, line := range lines {
			//nolint:gosec // G705: the bytes are the server's own log lines
			// (ring-sourced, never request-tainted), served text/plain with
			// nosniff — there is no HTML context to escape into.
			_, _ = w.Write([]byte(line))
			_, _ = w.Write([]byte("\n"))
		}
		return
	}
	writeJSONBody(w, http.StatusOK, systemLogsBody{
		Lines:     lines,
		Count:     len(lines),
		Held:      held,
		Capacity:  s.serviceLog.Capacity(),
		Truncated: evicted,
		GeneratedAt: time.Now().UTC().Format(
			"2006-01-02T15:04:05.000Z07:00"),
	})
}

// assembleServiceLog resolves the endpoint's collaborators (called from New
// before the Server struct exists): a caller-supplied seam wins verbatim
// (tests inject their own ring); otherwise the default console.LogRing is
// created and the incoming logger is wrapped so every record it emits is
// ALSO rendered — as a stable text line, independent of the configured
// logging.format — into the ring. The returned logger replaces the server's
// own, so the access log and every handler line flow through the fan-out.
func assembleServiceLog(log *slog.Logger, custom SystemLogTail) (*slog.Logger, SystemLogTail) {
	if custom != nil {
		return log, custom
	}
	ring := console.NewLogRing(0)
	view := slog.NewTextHandler(ring, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(&serviceLogFanout{next: log.Handler(), view: view}), ring
}

// serviceLogFanout is a slog.Handler forwarding every record to both the
// real destination and the ring-feeding text view (the ring capture is
// best-effort: a view failure never blocks the real log write).
type serviceLogFanout struct {
	next slog.Handler
	view slog.Handler
}

// Enabled defers to the real destination: the ring mirrors what is actually
// emitted, never a level the operator turned off.
func (f *serviceLogFanout) Enabled(ctx context.Context, l slog.Level) bool {
	return f.next.Enabled(ctx, l)
}

// Handle feeds the view first (synchronous, best-effort), then the real
// destination. Both consume the record within this call, so the record's
// clone-before-return contract holds without an explicit Clone.
func (f *serviceLogFanout) Handle(ctx context.Context, rec slog.Record) error {
	_ = f.view.Handle(ctx, rec)
	return f.next.Handle(ctx, rec)
}

func (f *serviceLogFanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &serviceLogFanout{next: f.next.WithAttrs(attrs), view: f.view.WithAttrs(attrs)}
}

func (f *serviceLogFanout) WithGroup(name string) slog.Handler {
	return &serviceLogFanout{next: f.next.WithGroup(name), view: f.view.WithGroup(name)}
}
