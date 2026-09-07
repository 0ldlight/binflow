// The webhook Emit facet's laws (M17 T-510, FR-152.3 / ADR-0045 decision
// 7): every successful publication tail — upload (first and overwrite
// alike), append, promote (never dry run) and retention's per-run discard
// — hands the bus the run's four facts (name/number/started/repo) plus
// the acting principal; the facet is best-effort (nil = no-op, a
// panicking adapter never fails the business operation).

package build_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/build"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// emitCapture is the injected facet's recording form (mutex-guarded — the
// service contract allows concurrent emitters).
type emitCapture struct {
	mu   sync.Mutex
	seen []build.WebhookEvent
}

func (c *emitCapture) record(_ context.Context, e build.WebhookEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, e)
}

func (c *emitCapture) events() []build.WebhookEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]build.WebhookEvent(nil), c.seen...)
}

// emitWorld is the bare stack the facet needs: a real metadata store, the
// auth mirror and the capturing facet. The carrier/docker/props seams stay
// unwired — the status-only promote and artifact-less retention arms this
// file drives never reach them.
type emitWorld struct {
	svc *build.Service
	t   *testing.T
}

func newEmitWorld(t *testing.T, e build.WebhookEmitter) *emitWorld {
	t.Helper()
	st, err := metadata.Open(context.Background(), metadata.Options{
		Path:          filepath.Join(t.TempDir(), "binflow.db"),
		AdminPassword: "pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &emitWorld{
		svc: build.New(st.Builds(), auth.NewFromStore(st, false), build.WithEmitter(e)),
		t:   t,
	}
}

// emitDoc is a complete-enough document with no artifact association.
func emitDoc(name, number, started string) string {
	return `{"name":"` + name + `","number":"` + number + `","type":"GENERIC",` +
		`"started":"` + started + `","modules":[]}`
}

// upload drives the publish face as the admin principal.
func (w *emitWorld) upload(doc string) *build.UploadResult {
	w.t.Helper()
	res, err := w.svc.Upload(context.Background(), adminP, []byte(doc), "")
	if err != nil {
		w.t.Fatalf("upload: %v", err)
	}
	return res
}

// TestUploadEmitsUploadedOnEveryPublication: the first upload, the
// overwrite and the append each fire ONE uploaded event carrying the
// canonical started literal and the resolved default build repo — the
// append speaks the RESOLVED parent's coordinates, not the caller's.
func TestUploadEmitsUploadedOnEveryPublication(t *testing.T) {
	capture := &emitCapture{}
	w := newEmitWorld(t, capture.record)
	if got := w.upload(emitDoc("pub-app", "51", "2026-09-07T10:00:00.000+0000")); !got.Created {
		t.Fatalf("first upload Created = false")
	}
	w.upload(emitDoc("pub-app", "51", "2026-09-07T10:00:00.000+0000")) // overwrite arm
	if _, err := w.svc.Append(context.Background(), adminP,
		build.Coordinate{Name: "pub-app", Number: "51"},
		[]byte(`[{"id":"m1","dependencies":[]}]`)); err != nil {
		t.Fatalf("append: %v", err)
	}

	events := capture.events()
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3 (upload/overwrite/append): %+v", len(events), events)
	}
	for i, e := range events {
		if e.Type != build.EventUploaded {
			t.Errorf("event %d type = %q, want uploaded", i, e.Type)
		}
		if e.Name != "pub-app" || e.Number != "51" {
			t.Errorf("event %d coordinates = %s#%s, want pub-app#51", i, e.Name, e.Number)
		}
		if e.Repo != metadata.DefaultBuildRepo {
			t.Errorf("event %d repo = %q, want the default %q", i, e.Repo, metadata.DefaultBuildRepo)
		}
		if e.Principal == nil || e.Principal.Name != "admin" {
			t.Errorf("event %d principal = %+v, want the acting admin", i, e.Principal)
		}
	}
	// The started literal is the CANONICAL UTC rendering (mixed-zone
	// input would leak through as-is without normalization).
	if got := events[0].Started; got != "2026-09-07T10:00:00.000+0000" {
		t.Errorf("started = %q, want the canonical literal", got)
	}
	// A zone-offset spelling canonicalizes onto the same UTC literal.
	zonedCap := &emitCapture{}
	w2 := newEmitWorld(t, zonedCap.record)
	w2.upload(emitDoc("zoned", "1", "2026-09-07T18:00:00.000+0800"))
	if got := zonedCap.events()[0].Started; got != "2026-09-07T10:00:00.000+0000" {
		t.Errorf("zoned started = %q, want canonical UTC 2026-09-07T10:00:00.000+0000", got)
	}
}

// TestPromoteEmitsPromotedAndDryRunStaysSilent: the status-only promotion
// fires promoted with the run's own repo (never a target); dryRun fires
// NOTHING — zero side effects is zero events.
func TestPromoteEmitsPromotedAndDryRunStaysSilent(t *testing.T) {
	capture := &emitCapture{}
	w := newEmitWorld(t, capture.record)
	w.upload(emitDoc("rel-app", "7", "2026-09-07T11:00:00.000+0000"))

	if _, err := w.svc.Promote(context.Background(), adminP,
		build.Coordinate{Name: "rel-app", Number: "7"},
		[]byte(`{"status":"released","dryRun":true}`)); err != nil {
		t.Fatalf("dry-run promote: %v", err)
	}
	if n := len(capture.events()); n != 1 {
		t.Fatalf("events after dry run = %d, want 1 (the seeding upload only)", n)
	}

	if _, err := w.svc.Promote(context.Background(), adminP,
		build.Coordinate{Name: "rel-app", Number: "7"},
		[]byte(`{"status":"released"}`)); err != nil {
		t.Fatalf("promote: %v", err)
	}
	events := capture.events()
	last := events[len(events)-1]
	if last.Type != build.EventPromoted {
		t.Fatalf("last event type = %q, want promoted", last.Type)
	}
	if last.Name != "rel-app" || last.Number != "7" ||
		last.Started != "2026-09-07T11:00:00.000+0000" ||
		last.Repo != metadata.DefaultBuildRepo {
		t.Fatalf("promoted coordinates: %+v", last)
	}
}

// TestRetentionEmitsDeletedPerDiscardedRun: each run the window discards
// fires its own deleted event (name/number/started/repo of THAT run);
// kept and exempt runs fire nothing.
func TestRetentionEmitsDeletedPerDiscardedRun(t *testing.T) {
	capture := &emitCapture{}
	w := newEmitWorld(t, capture.record)
	w.upload(emitDoc("old-app", "1", "2026-08-01T10:00:00.000+0000"))
	w.upload(emitDoc("old-app", "2", "2026-09-01T10:00:00.000+0000"))
	w.upload(emitDoc("old-app", "3", "2026-09-07T10:00:00.000+0000"))

	plan, err := w.svc.PrepareRetention(context.Background(), adminP, "old-app", "",
		build.RetentionRequest{Count: 1})
	if err != nil {
		t.Fatalf("prepare retention: %v", err)
	}
	res, err := plan.Execute(context.Background(), w.svc, adminP)
	if err != nil {
		t.Fatalf("execute retention: %v", err)
	}
	if len(res.Deleted) != 2 {
		t.Fatalf("deleted runs = %v, want the two older ones", res.Deleted)
	}

	var deleted []build.WebhookEvent
	for _, e := range capture.events() {
		if e.Type == build.EventDeleted {
			deleted = append(deleted, e)
		}
	}
	if len(deleted) != 2 {
		t.Fatalf("deleted events = %d, want 2", len(deleted))
	}
	// The window walks the runs newest-first, so the discard (and the
	// events) arrive run 2 then run 1 — the kept run 3 stays silent.
	if deleted[0].Number != "2" || deleted[1].Number != "1" {
		t.Fatalf("deleted event order = %s, %s — want runs 2 and 1",
			deleted[0].Number, deleted[1].Number)
	}
	for _, e := range deleted {
		if e.Name != "old-app" || e.Repo != metadata.DefaultBuildRepo || e.Started == "" {
			t.Fatalf("deleted event coordinates: %+v", e)
		}
	}
}

// TestEmitterPanicContainedAndNilSilent: the facet is best-effort both
// ways — a panicking adapter never fails the upload (the WARN path is the
// bus shield's posture carried one layer out), and the nil facet is the
// bare unit stack.
func TestEmitterPanicContainedAndNilSilent(t *testing.T) {
	w := newEmitWorld(t, func(context.Context, build.WebhookEvent) {
		panic("adapter bug")
	})
	if res := w.upload(emitDoc("tough-app", "1", "2026-09-07T10:00:00.000+0000")); res == nil {
		t.Fatal("upload failed under a panicking emitter")
	}

	bare := newEmitWorld(t, nil)
	if bare.upload(emitDoc("quiet-app", "1", "2026-09-07T10:00:00.000+0000")) == nil {
		t.Fatal("upload failed under the nil emitter")
	}
}

// TestEmitFacetTypesAreTheRegisteredSpellings pins the adapter contract:
// the three literals the bus side's closed set registers (a typo here is
// a silent drop at Emit, so the spellings are asserted verbatim).
func TestEmitFacetTypesAreTheRegisteredSpellings(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{build.EventUploaded, "uploaded"},
		{build.EventDeleted, "deleted"},
		{build.EventPromoted, "promoted"},
	} {
		if c.got != c.want {
			t.Errorf("event literal = %q, want %q", c.got, c.want)
		}
		if strings.ContainsAny(c.got, " ") {
			t.Errorf("event literal %q carries whitespace", c.got)
		}
	}
}
