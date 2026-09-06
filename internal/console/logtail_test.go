package console

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// logtail_test.go pins the System Logs reading face's ring semantics
// (T-493, FR-157③): complete-line buffering, tail windows, substring
// filtering, eviction reporting, the per-line length cap and concurrency —
// the behaviors the /api/v1/system/logs endpoint's contract rests on.

func TestLogRingTailWindowAndOrder(t *testing.T) {
	r := NewLogRing(0) // default capacity
	for i := 1; i <= 5; i++ {
		if _, err := fmt.Fprintf(r, "line-%d\n", i); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	lines, held, evicted := r.TailLines(3, "")
	if len(lines) != 3 || lines[0] != "line-3" || lines[2] != "line-5" {
		t.Fatalf("tail = %v, want [line-3 line-4 line-5] oldest first", lines)
	}
	if held != 5 || evicted {
		t.Fatalf("held=%d evicted=%v, want 5/false", held, evicted)
	}
	if r.Len() != 5 {
		t.Fatalf("Len = %d, want 5", r.Len())
	}
	// A window larger than the held set returns everything.
	all, _, _ := r.TailLines(1000, "")
	if len(all) != 5 || all[0] != "line-1" {
		t.Fatalf("over-window tail = %v", all)
	}
}

func TestLogRingPartialLineBuffering(t *testing.T) {
	r := NewLogRing(0)
	if _, err := r.Write([]byte("first-half,")); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if r.Len() != 0 {
		t.Fatal("a partial line must not be stored yet")
	}
	if _, err := r.Write([]byte("second-half\r\n")); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	lines, _, _ := r.TailLines(10, "")
	if len(lines) != 1 || lines[0] != "first-half,second-half" {
		t.Fatalf("joined line = %v, want the \\r\\n-trimmed join", lines)
	}
}

func TestLogRingFilterIsSubstringAndTailOfMatches(t *testing.T) {
	r := NewLogRing(0)
	_, _ = r.Write([]byte("level=info msg=one\n"))
	_, _ = r.Write([]byte("level=warn msg=two\n"))
	_, _ = r.Write([]byte("level=info msg=three\n"))
	// The filter narrows the stream, THEN the tail window cuts it: the two
	// matching lines both fit, in order.
	lines, _, _ := r.TailLines(10, "level=info")
	if len(lines) != 2 || !strings.Contains(lines[0], "one") || !strings.Contains(lines[1], "three") {
		t.Fatalf("filtered = %v", lines)
	}
	// A one-line window keeps the NEWEST match.
	lines, _, _ = r.TailLines(1, "level=info")
	if len(lines) != 1 || !strings.Contains(lines[0], "three") {
		t.Fatalf("filtered tail-1 = %v", lines)
	}
	// Case-sensitive plain substring (no regex, no folding).
	if lines, _, _ = r.TailLines(10, "LEVEL=INFO"); len(lines) != 0 {
		t.Fatalf("case-sensitive filter matched %v", lines)
	}
	// A JSON-body shape (the stderr default format) filters the same way.
	if lines, _, _ = r.TailLines(10, "msg=two"); len(lines) != 1 {
		t.Fatalf("exact-substring filter = %v", lines)
	}
}

func TestLogRingEvictionAndCapacity(t *testing.T) {
	r := NewLogRing(16) // the floor clamp (a smaller ask rises to it)
	if r.Capacity() != minLogRingLines {
		t.Fatalf("capacity = %d, want the %d floor", r.Capacity(), minLogRingLines)
	}
	for i := 1; i <= r.Capacity()+10; i++ {
		_, _ = fmt.Fprintf(r, "e-%d\n", i)
	}
	lines, held, evicted := r.TailLines(5, "")
	if held != r.Capacity() || !evicted {
		t.Fatalf("held=%d evicted=%v, want full ring and evictions reported", held, evicted)
	}
	if lines[0] != fmt.Sprintf("e-%d", r.Capacity()+6) || lines[4] != fmt.Sprintf("e-%d", r.Capacity()+10) {
		t.Fatalf("post-eviction tail = %v, want the newest five", lines)
	}
}

func TestLogRingLineLengthCap(t *testing.T) {
	r := NewLogRing(0)
	long := strings.Repeat("x", MaxLogLineBytes+1000)
	_, _ = r.Write([]byte(long + "\n"))
	lines, _, _ := r.TailLines(1, "")
	if len(lines) != 1 {
		t.Fatalf("stored lines = %d", len(lines))
	}
	if len(lines[0]) != MaxLogLineBytes {
		t.Fatalf("capped line length = %d, want exactly %d", len(lines[0]), MaxLogLineBytes)
	}
	if !strings.HasSuffix(lines[0], logTruncationSuffix) {
		t.Fatalf("capped line lacks the marker: %q", lines[0][len(lines[0])-40:])
	}
}

func TestLogRingEmptyRingIsEmptyArray(t *testing.T) {
	r := NewLogRing(0)
	lines, held, evicted := r.TailLines(10, "")
	if lines == nil || len(lines) != 0 {
		t.Fatalf("empty tail = %#v, want a non-nil empty array (the wire shape)", lines)
	}
	if held != 0 || evicted {
		t.Fatalf("held=%d evicted=%v on a fresh ring", held, evicted)
	}
}

func TestLogRingConcurrentWritersAndReader(t *testing.T) {
	r := NewLogRing(0)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_, _ = fmt.Fprintf(r, "w%d-i%d\n", w, i)
			}
		}(w)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			lines, _, _ := r.TailLines(50, "")
			if len(lines) > 50 {
				panic("tail window exceeded its bound")
			}
		}
	}()
	wg.Wait()
	if r.Len() != 1600 {
		t.Fatalf("Len = %d, want 1600 (default capacity holds them all)", r.Len())
	}
}
