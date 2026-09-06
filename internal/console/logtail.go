// logtail.go is the System Logs reading face (M17 T-493, FR-157③): the
// in-process tail of the server's own log stream, served by
// GET /api/v1/system/logs (internal/httpapi/system_logs.go). The face lives
// in the console package because it exists for exactly one consumer — the
// console's System Logs viewer — and carries no protocol or storage
// coupling; httpapi defines the seam it consumes (SystemLogTail) and this
// package ships the sole production implementation.
//
// Source ruling (the ticket's "进程日志来源" decision): BinFlow logs 12-factor
// style to stderr (cmd newLogger — no log file by default), so the honest
// process-log source is an in-process RING fed from the assembled logger,
// not a filesystem read. The ring holds complete rendered lines with hard
// bounds on both axes:
//
//   - line count: DefaultLogRingLines (4096) — eviction is ring-shaped,
//     oldest first, and reported so the viewer can say "older lines were
//     evicted" instead of implying a full history;
//   - line length: MaxLogLineBytes per line — a pathological record cannot
//     balloon the ring past lines x cap bytes.
//
// Read-safety boundaries: the ring captures exactly what the process logger
// emits (never credentials — the access log records method/path/status/
// actor, and the auth plane logs failure reasons, not secrets); the endpoint
// gates the read behind system:read and applies its own limit/filter caps.

package console

import (
	"bytes"
	"strings"
	"sync"
)

// Ring capacity and per-line bounds. The defaults bound worst-case ring
// memory at 4096 x 4096 B = 16 MiB; typical access-log lines are a few
// hundred bytes, so a full ring is ~1 MiB in practice.
const (
	// DefaultLogRingLines is the ring capacity NewLogRing installs for a
	// non-positive argument.
	DefaultLogRingLines = 4096
	// MaxLogLineBytes caps one stored line; longer input is stored with the
	// truncation marker so the viewer sees that bytes were dropped.
	MaxLogLineBytes = 4096
	// logTruncationSuffix marks a length-capped line.
	logTruncationSuffix = " …[truncated]"
)

// minLogRingLines is the floor for a caller-supplied capacity (a ring below
// this serves no tail purpose and would flap the eviction flag).
const minLogRingLines = 16

// LogRing is a bounded, concurrency-safe ring of complete log lines. It
// implements io.Writer (feeding it the rendered output of a slog handler —
// one Write per record, but partial lines are buffered so any chunked feed
// works) and the httpapi.SystemLogTail seam (TailLines/Capacity).
type LogRing struct {
	mu      sync.RWMutex
	lines   []string
	head    int // index of the oldest held line
	n       int // lines currently held
	pending []byte
}

// NewLogRing returns a ring holding up to capacity complete lines. A
// non-positive capacity selects DefaultLogRingLines; a capacity below
// minLogRingLines is clamped up to it.
func NewLogRing(capacity int) *LogRing {
	if capacity <= 0 {
		capacity = DefaultLogRingLines
	}
	if capacity < minLogRingLines {
		capacity = minLogRingLines
	}
	return &LogRing{lines: make([]string, capacity)}
}

// Write buffers p and appends every complete line (split on '\n', a
// trailing '\r' trimmed) to the ring. It always reports len(p) and never
// errors: the ring is a best-effort observability sink, and a writer that
// had to inspect an error would be a logger that could block on logging.
func (r *LogRing) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = append(r.pending, p...)
	for {
		i := bytes.IndexByte(r.pending, '\n')
		if i < 0 {
			return len(p), nil // remainder stays buffered for the next Write
		}
		r.appendLine(string(r.pending[:i+1]))
		r.pending = r.pending[:copy(r.pending, r.pending[i+1:])]
	}
}

// appendLine stores one complete line (caller holds the write lock).
func (r *LogRing) appendLine(raw string) {
	line := strings.TrimSuffix(raw, "\n")
	line = strings.TrimSuffix(line, "\r")
	if len(line) > MaxLogLineBytes {
		line = line[:MaxLogLineBytes-len(logTruncationSuffix)] + logTruncationSuffix
	}
	if r.n < len(r.lines) {
		r.lines[(r.head+r.n)%len(r.lines)] = line
		r.n++
		return
	}
	// Full: overwrite the oldest and advance the head — the eviction the
	// TailLines report surfaces as its third return.
	r.lines[r.head] = line
	r.head = (r.head + 1) % len(r.lines)
}

// TailLines returns up to last lines matching the filter substring
// ("" matches everything; the comparison is a plain case-sensitive
// strings.Contains — no regex, so no pathological-input surface), ordered
// oldest to newest. held is the total number of lines currently in the ring
// (pre-filter); evicted reports whether older lines were dropped by the
// ring's bound, so a viewer can distinguish "the window" from "the history".
func (r *LogRing) TailLines(last int, filter string) (lines []string, held int, evicted bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	held = r.n
	evicted = r.n == len(r.lines) && r.n > 0
	if last <= 0 {
		last = r.n
	}
	var matching []string
	for i := 0; i < r.n; i++ {
		line := r.lines[(r.head+i)%len(r.lines)]
		if filter != "" && !strings.Contains(line, filter) {
			continue
		}
		matching = append(matching, line)
	}
	if len(matching) > last {
		matching = matching[len(matching)-last:]
	}
	if matching == nil {
		matching = []string{}
	}
	return matching, held, evicted
}

// Capacity reports the ring's line bound (the endpoint echoes it so the
// viewer's "window vs history" note stays honest).
func (r *LogRing) Capacity() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.lines)
}

// Len reports the number of lines currently held.
func (r *LogRing) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.n
}
