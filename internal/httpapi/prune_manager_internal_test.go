package httpapi

// The prune manager's unit tests (LOOP 005 review N2): the report's
// progress numerator must be monotonic — a resumed run begins at its
// startFrom position while the walk's skipped-directory observations
// arrive with indices below it, and the clamp keeps the rendered
// "N of 256" from regressing across the skip span.

import (
	"context"
	"testing"

	"github.com/lzwzzy/binflow/internal/storage"
)

type quietWarnLog struct{}

func (quietWarnLog) WarnContext(context.Context, string, ...any) {}

func TestPruneManagerProgressMonotonicAcrossResume(t *testing.T) {
	m := newPruneManager(t.TempDir(), quietWarnLog{})
	if !m.begin(false, "80") {
		t.Fatal("begin must claim the single-flight slot")
	}
	if got := m.snapshotProgress(); got != 0x81 {
		t.Fatalf("resume start progress = %d, want %d (shard 80's 1-based position)", got, 0x81)
	}
	// The walk enumerates from 00: the skipped directories observe with
	// indices below the resume point — the numerator must hold, not
	// regress to the raw observe index.
	m.observe(storage.PruneDirStats{Name: "00", Index: 1, Skipped: true})
	m.observe(storage.PruneDirStats{Name: "7f", Index: 0x80, Skipped: true})
	if got := m.snapshotProgress(); got != 0x81 {
		t.Fatalf("progress regressed to %d across skipped-dir observes, want %d", got, 0x81)
	}
	m.observe(storage.PruneDirStats{Name: "c8", Index: 0xc9})
	if got := m.snapshotProgress(); got != 0xc9 {
		t.Fatalf("progress = %d after observing shard c8, want 0xc9", got)
	}
	m.finish("finished")
}
