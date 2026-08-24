package storage

// [M9] ADR-0031 test suite: the GC hold set (mechanism B, W-1) and the
// per-candidate delete recheck (mechanism A, W-2). The two controlled W-1/W-2
// legs, the gate-ordering pin, the TTL backstop, the refcount semantics and
// the T-232-style stress race live here; the three-engine contract coverage
// (disk / S3 / migration) is table-driven where the harness allows it.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// commitUnreleased uploads content through a raw session and DOES NOT call
// ReleaseGCHold — the model of an upload whose metadata transaction has not
// landed yet (or whose owner crashed right after Commit).
func commitUnreleased(t *testing.T, eng Engine, content []byte) BlobRef {
	t.Helper()
	s, err := eng.BeginSession(context.Background())
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if _, err := s.Append(context.Background(), bytes.NewReader(content)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	ref, err := s.Commit(context.Background(), BlobRef{})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return ref
}

// TestGCHeldBlobSurvivesZeroGraceApply is the controlled W-1 leg (the T-232
// t134 scenario distilled): a blob committed but not yet referenced, an
// explicit grace=0 apply, an mtime backdated past any grace. Pre-M9 the
// apply physically deleted exactly this blob; the hold set must keep it
// until the caller's release — and the release must restore the W24
// graceHours:0 recipe unchanged.
func TestGCHeldBlobSurvivesZeroGraceApply(t *testing.T) {
	root := t.TempDir()
	eng := newEngineAt(t, root, Options{})
	ctx := context.Background()

	inFlight := commitUnreleased(t, eng, []byte("blob-first, metadata pending"))
	backdateBlob(t, root, inFlight.Sha256, time.Hour) // old enough for any grace

	// Dry-run excludes held blobs from the candidate list (ADR-0031 point 2).
	got, err := eng.GC(ctx, refsSet(), time.Nanosecond, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("dry-run candidates = %v, want none (blob is held)", got)
	}

	// Apply with grace=0: the blob must survive (W-1 closed).
	deleted, err := eng.GC(ctx, refsSet(), time.Nanosecond, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 {
		t.Fatalf("grace=0 apply deleted held in-flight blob: %v", deleted)
	}
	if _, err := eng.Stat(ctx, inFlight.Sha256); err != nil {
		t.Fatalf("in-flight blob lost: %v", err)
	}

	// The caller's metadata transaction lands -> release -> the same apply
	// now collects it. The W24 recipe (upload, unreferenced, graceHours:0)
	// behaves exactly as before once the upload completes.
	if err := eng.ReleaseGCHold(inFlight.Sha256); err != nil {
		t.Fatalf("ReleaseGCHold: %v", err)
	}
	deleted, err = eng.GC(ctx, refsSet(), time.Nanosecond, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != inFlight.Sha256 {
		t.Fatalf("apply after release = %v, want [%s]", deleted, inFlight.Sha256)
	}
	if _, err := eng.Stat(ctx, inFlight.Sha256); !errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("Stat after collection err = %v, want ErrBlobNotFound", err)
	}
}

// TestGCHoldTTLBackstop pins the TTL lifecycle (architecture section 14.2
// point 1): an unreleased hold protects for its TTL and stops protecting
// after it — the crash backstop that keeps a lost release from wedging
// reclamation forever. TTL normalization: zero takes the 600s default,
// sub-floor values clamp up to 60s.
func TestGCHoldTTLBackstop(t *testing.T) {
	cases := []struct {
		name     string
		ttl      time.Duration
		advance  time.Duration
		wantHeld bool
	}{
		{"zero value takes the default", 0, DefaultGCHoldTTL - time.Second, true},
		{"default expires", 0, DefaultGCHoldTTL + time.Second, false},
		{"sub-floor clamps up", time.Second, MinGCHoldTTL - time.Second, true},
		{"floor expiry", time.Second, MinGCHoldTTL + time.Second, false},
		{"configured honored", 2 * time.Minute, 2*time.Minute - time.Second, true},
		{"configured expires", 2 * time.Minute, 2*time.Minute + time.Second, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			eng, err := OpenEngine(t.TempDir(), Options{GCHoldTTL: tt.ttl, Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			defer eng.Close() //nolint:errcheck // test
			ctx := context.Background()

			ref := commitUnreleased(t, eng, []byte("ttl probe "+tt.name))
			now = now.Add(tt.advance)

			e := eng.(*engine)
			if got := e.holds.held(ref.Sha256); got != tt.wantHeld {
				t.Fatalf("held after advance = %v, want %v", got, tt.wantHeld)
			}
			// The GC face agrees with the set: expired holds collect under
			// grace=0, live ones do not.
			deleted, err := eng.GC(ctx, refsSet(), time.Nanosecond, true)
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantHeld {
				if len(deleted) != 0 {
					t.Fatalf("apply deleted a held blob: %v", deleted)
				}
			} else {
				if len(deleted) != 1 || deleted[0] != ref.Sha256 {
					t.Fatalf("apply after TTL expiry = %v, want [%s]", deleted, ref.Sha256)
				}
			}
		})
	}
}

// trueMarker is a GCMarker with instrumented, controllable Live — the
// metadata.IsReferenced shape T-256 wires (single-point, no set rebuild).
type trueMarker struct {
	markCalls atomic.Int64
	liveCalls atomic.Int64
	refs      map[string]struct{}
	liveFn    func(sha string) (bool, error) // nil -> membership in refs
}

func (m *trueMarker) Mark() (map[string]struct{}, error) {
	m.markCalls.Add(1)
	cp := make(map[string]struct{}, len(m.refs))
	for k := range m.refs {
		cp[k] = struct{}{}
	}
	return cp, nil
}

func (m *trueMarker) Live(sha string) (bool, error) {
	m.liveCalls.Add(1)
	if m.liveFn != nil {
		return m.liveFn(sha)
	}
	_, ok := m.refs[sha]
	return ok, nil
}

// TestGCLiveRecheckSkipsLateReference is the controlled W-2 leg: a reference
// that landed after the candidacy snapshot must still stop the delete, via
// the per-candidate Live gate. Also pins that a true marker is NOT
// re-Marked for the recheck (its Live is the fresher oracle).
func TestGCLiveRecheckSkipsLateReference(t *testing.T) {
	ctx := context.Background()

	t.Run("live true skips the delete", func(t *testing.T) {
		eng := newEngine(t, Options{})
		ref := put(t, eng, []byte("w2 survivor"))
		// The Mark snapshot is empty — exactly the stale-snapshot condition
		// W-2 describes; the single-point Live knows the reference landed.
		m := &trueMarker{liveFn: func(sha string) (bool, error) { return sha == ref.Sha256, nil }}
		deleted, err := eng.GCSweep(ctx, m, time.Nanosecond, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(deleted) != 0 {
			t.Fatalf("apply deleted a blob whose reference landed late: %v", deleted)
		}
		if m.markCalls.Load() != 1 {
			t.Fatalf("Mark calls = %d, want 1 (true markers are not re-snapshotted)", m.markCalls.Load())
		}
		if m.liveCalls.Load() == 0 {
			t.Fatal("Live was never consulted for the candidate")
		}
	})

	t.Run("live false deletes", func(t *testing.T) {
		eng := newEngine(t, Options{})
		ref := put(t, eng, []byte("w2 garbage"))
		m := &trueMarker{}
		deleted, err := eng.GCSweep(ctx, m, time.Nanosecond, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(deleted) != 1 || deleted[0] != ref.Sha256 {
			t.Fatalf("apply = %v, want [%s]", deleted, ref.Sha256)
		}
	})

	t.Run("live error skips conservatively and surfaces", func(t *testing.T) {
		eng := newEngine(t, Options{})
		ref := put(t, eng, []byte("w2 uncertain"))
		m := &trueMarker{liveFn: func(string) (bool, error) { return false, errors.New("metadata unavailable") }}
		_, err := eng.GCSweep(ctx, m, time.Nanosecond, true)
		if err == nil || !strings.Contains(err.Error(), "live recheck") {
			t.Fatalf("err = %v, want a live recheck failure", err)
		}
		// The uncertain candidate must survive: an unreliable answer never
		// deletes. (The sweep's error return carries the candidate list, not
		// deletions — the pre-existing firstErr contract — so the physical
		// probe below is the assertion.)
		if _, serr := eng.Stat(ctx, ref.Sha256); serr != nil {
			t.Fatalf("uncertain blob lost: %v", serr)
		}
	})
}

// TestGCLegacyMarkerResnapshotSkipsLateReference pins the legacy form's W-2
// closure: ReferencedFunc has no single-point Live, so the engine
// re-snapshots ONCE for the gated phase ("mark 集重查"). A reference landing
// between the candidacy Mark (call 1) and the recheck Mark (call 2) stops
// the delete even on the old face — this is what the CLI/REST callers get
// before T-256 upgrades their marker.
func TestGCLegacyMarkerResnapshotSkipsLateReference(t *testing.T) {
	eng := newEngine(t, Options{})
	ctx := context.Background()
	ref := put(t, eng, []byte("w2 legacy survivor"))

	var calls atomic.Int64
	fn := ReferencedFunc(func() (map[string]struct{}, error) {
		if calls.Add(1) == 1 {
			return map[string]struct{}{}, nil // candidacy: not referenced (stale)
		}
		return map[string]struct{}{ref.Sha256: {}}, nil // recheck: reference landed
	})

	deleted, err := eng.GCSweep(ctx, fn, time.Nanosecond, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 {
		t.Fatalf("apply deleted a late-referenced blob: %v", deleted)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("mark invocations = %d, want 2 (candidacy + one refresh)", got)
	}
}

// TestGCHoldGatePrecedesLiveRecheck pins the ORDER of gcDeleteGate's checks:
// the hold gate must run BEFORE Live. The probe registers a hold and makes
// Live's side effect release it — modeling a repo whose metadata transaction
// completes exactly while the sweep is between the two gates. With the
// correct order the hold gate has already seen the registration (skip); an
// inverted engine would consult Live first (false), then find the hold set
// emptied by the release and delete a just-referenced blob.
func TestGCHoldGatePrecedesLiveRecheck(t *testing.T) {
	const sha = "0000000000000000000000000000000000000000000000000000000000000000"
	holds := newHoldSet(0, nil)
	holds.acquire(sha)

	var liveCalled atomic.Bool
	m := &trueMarker{liveFn: func(s string) (bool, error) {
		liveCalled.Store(true)
		holds.release(s)  // the reference landed; repo released the hold
		return false, nil // ...and Live itself was queried a hair too early
	}}
	gate := &gcDeleteGate{holds: holds, recheck: newGCRecheck(m)}

	skip, err := gate.skip(sha)
	if err != nil {
		t.Fatalf("gate.skip: %v", err)
	}
	if !skip {
		t.Fatal("gate did not skip: the hold gate must precede Live (see gcDeleteGate)")
	}
	if liveCalled.Load() {
		t.Fatal("Live was consulted although the hold gate already decided")
	}
}

// TestGCHoldRefcountSurvivesConcurrentDedup pins the refcount semantics:
// two Commits of identical content each acquire; one release must not strip
// the other's protection while its metadata transaction is still pending.
func TestGCHoldRefcountSurvivesConcurrentDedup(t *testing.T) {
	eng := newEngine(t, Options{})
	ctx := context.Background()

	first := commitUnreleased(t, eng, []byte("identical content"))
	second := commitUnreleased(t, eng, []byte("identical content")) // dedup hit
	if first.Sha256 != second.Sha256 {
		t.Fatalf("dedup shas disagree: %s vs %s", first.Sha256, second.Sha256)
	}

	// First uploader's metadata lands and releases; the second is still
	// in flight — the blob must stay protected.
	if err := eng.ReleaseGCHold(first.Sha256); err != nil {
		t.Fatal(err)
	}
	if deleted, err := eng.GC(ctx, refsSet(), time.Nanosecond, true); err != nil {
		t.Fatal(err)
	} else if len(deleted) != 0 {
		t.Fatalf("release from one uploader stripped the other's hold: %v", deleted)
	}

	// Second uploader finishes -> collectable again.
	if err := eng.ReleaseGCHold(second.Sha256); err != nil {
		t.Fatal(err)
	}
	if deleted, err := eng.GC(ctx, refsSet(), time.Nanosecond, true); err != nil {
		t.Fatal(err)
	} else if len(deleted) != 1 || deleted[0] != first.Sha256 {
		t.Fatalf("apply after both releases = %v, want [%s]", deleted, first.Sha256)
	}
}

// TestFailedCommitReleasesHold: a Commit that fails AFTER acquiring (here:
// the rename cannot land because the shard dir is read-only) must release
// its own registration — a failed upload cannot hold the sha hostage for a
// full TTL.
func TestFailedCommitReleasesHold(t *testing.T) {
	root := t.TempDir()
	eng := newEngineAt(t, root, Options{})
	ctx := context.Background()

	s, err := eng.BeginSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("doomed commit")
	if _, err := s.Append(ctx, bytes.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	sum := sha256HexForTest(string(content))
	// Pre-create the target shard dir read-only: MkdirAll succeeds (the dir
	// exists), the rename into it fails with EACCES — a post-acquire failure.
	shard := filepath.Join(root, "blobs", sum[:2])
	if err := os.MkdirAll(shard, 0o500); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Commit(ctx, BlobRef{}); err == nil {
		t.Fatal("Commit into a read-only shard unexpectedly succeeded")
	}
	if eng.(*engine).holds.held(sum) {
		t.Fatal("failed Commit left its hold registered")
	}
}

// TestReleaseGCHoldContract: the release seam is infallible and forgiving —
// unknown shas, double releases and post-Close releases are no-ops.
func TestReleaseGCHoldContract(t *testing.T) {
	eng := newEngine(t, Options{})
	if err := eng.ReleaseGCHold("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"); err != nil {
		t.Fatalf("release of an unknown sha: %v", err)
	}
	ref := commitUnreleased(t, eng, []byte("release contract"))
	for i := 0; i < 2; i++ {
		if err := eng.ReleaseGCHold(ref.Sha256); err != nil {
			t.Fatalf("release #%d: %v", i, err)
		}
	}
	if err := eng.Close(); err != nil {
		t.Fatal(err)
	}
	if err := eng.ReleaseGCHold(ref.Sha256); err != nil {
		t.Fatalf("release after Close: %v", err)
	}
}

// TestGCSweepNilMarkerRejected: every engine's GCSweep face rejects a nil
// marker (the M4~M8 GC face's nil-callback contract, carried over).
func TestGCSweepNilMarkerRejected(t *testing.T) {
	ctx := context.Background()
	eng := newEngine(t, Options{})
	if _, err := eng.GCSweep(ctx, nil, time.Hour, false); err == nil {
		t.Fatal("disk GCSweep(nil) should fail")
	}

	bypass := NewMigrationEngine(eng, nil, MigrationConfig{})
	if _, err := bypass.GCSweep(ctx, nil, time.Hour, false); err == nil {
		t.Fatal("migration GCSweep(nil) should fail")
	}

	s3e, _, _ := newS3Engine(t)
	if _, err := s3e.GCSweep(ctx, nil, time.Hour, false); err == nil {
		t.Fatal("s3 GCSweep(nil) should fail")
	}
}

// stressMarker is the concurrency-safe metadata-backed marker the race leg
// uses: one shared "nodes" map guarded by mu, snapshotted by Mark and probed
// single-point by Live — the shape repo.Service wires (T-256).
type stressMarker struct {
	mu   *sync.Mutex
	refs map[string]struct{}
}

func (m *stressMarker) Mark() (map[string]struct{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make(map[string]struct{}, len(m.refs))
	for k := range m.refs {
		cp[k] = struct{}{}
	}
	return cp, nil
}

func (m *stressMarker) Live(sha string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.refs[sha]
	return ok, nil
}

// TestGCApplyRacingUploadsZeroGraceNeverDeletesReferenced is the T-232
// scenario as a stress leg: parallel uploads racing a hammering grace=0
// apply, each upload following the repo contract (Commit -> reference lands
// -> release). The invariant is t134's failure mode inverted: every blob
// whose reference has landed must still be physically present at the end —
// a single miss is the "manifest PUT 500 blob not found" bug.
//
// The marker models the metadata-backed GCMarker T-256 wires (Mark = set
// snapshot, Live = single-point probe over the same rows): that is the form
// the GCSweep soundness argument covers end to end. The legacy func form is
// deliberately NOT stressed here — its documented residual window (apply
// refresh -> release -> unlink) is exactly what a stress loop would keep
// hitting, and the fix for it IS the true-marker wiring.
func TestGCApplyRacingUploadsZeroGraceNeverDeletesReferenced(t *testing.T) {
	root := t.TempDir()
	eng := newEngineAt(t, root, Options{})
	ctx := context.Background()

	const uploaders = 4
	const perUploader = 40

	var mu sync.Mutex
	refs := make(map[string]struct{}) // the "metadata": landed references

	// stressMarker is the metadata-backed marker shape under stress: Mark
	// snapshots the shared set under its lock, Live probes it single-point.
	marker := &stressMarker{mu: &mu, refs: refs}

	stop := make(chan struct{})
	var gcErrs atomic.Int64
	var wgGC sync.WaitGroup
	for g := 0; g < 2; g++ {
		wgGC.Add(1)
		go func() {
			defer wgGC.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// grace=1ns: the explicit no-grace form whose race with
				// parallel uploads is the whole point of the hold set.
				if _, err := eng.GCSweep(ctx, marker, time.Nanosecond, true); err != nil {
					gcErrs.Add(1)
					return
				}
			}
		}()
	}
	var wgUp sync.WaitGroup
	for u := 0; u < uploaders; u++ {
		wgUp.Add(1)
		go func(u int) {
			defer wgUp.Done()
			for i := 0; i < perUploader; i++ {
				content := []byte(fmt.Sprintf("race-%d-%d-%d", u, i, time.Now().UnixNano()))
				s, err := eng.BeginSession(ctx)
				if err != nil {
					t.Errorf("uploader %d: BeginSession: %v", u, err)
					return
				}
				if _, err := s.Append(ctx, bytes.NewReader(content)); err != nil {
					t.Errorf("uploader %d: Append: %v", u, err)
					return
				}
				ref, err := s.Commit(ctx, BlobRef{})
				if err != nil {
					t.Errorf("uploader %d: Commit: %v", u, err)
					return
				}
				// repo contract: the metadata transaction lands, THEN the
				// hold is released. Between Commit and this map write the
				// blob is exactly the W-1 window the hold set covers.
				mu.Lock()
				refs[ref.Sha256] = struct{}{}
				mu.Unlock()
				if err := eng.ReleaseGCHold(ref.Sha256); err != nil {
					t.Errorf("uploader %d: ReleaseGCHold: %v", u, err)
					return
				}
			}
		}(u)
	}
	wgUp.Wait()
	close(stop)
	wgGC.Wait()

	if n := gcErrs.Load(); n != 0 {
		t.Fatalf("%d GC goroutines errored during the race", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(refs) != uploaders*perUploader {
		t.Fatalf("landed references = %d, want %d", len(refs), uploaders*perUploader)
	}
	for sha := range refs {
		if _, _, err := eng.Open(ctx, sha); err != nil {
			t.Fatalf("referenced blob %s was physically deleted by the racing apply (the t134 bug): %v", sha, err)
		}
	}
}

// TestGCInFlightUploadMidSweepHold pins the same-pass protection: a hold
// acquired AFTER the sweep began (mid-pass) must still stop that blob's
// deletion. The scheduling hook is the legacy marker's lazy apply-phase
// refresh, which fires at the first gated candidate — the earlier blob's
// gates — so the later blob is held by the time the sweep reaches it
// (content is chosen so the first blob's shard sorts earlier, making the
// scheduling deterministic).
func TestGCInFlightUploadMidSweepHold(t *testing.T) {
	eng := newEngine(t, Options{})
	ctx := context.Background()

	// Pick contents whose shards are strictly ordered: first < later, so the
	// scan reaches `first` (and fires the refresh hook) before `later`.
	var first, later BlobRef
	for i := 0; ; i++ {
		a := put(t, eng, []byte(fmt.Sprintf("mid-sweep a #%d", i)))
		b := put(t, eng, []byte(fmt.Sprintf("mid-sweep b #%d", i)))
		if a.Sha256[:2] < b.Sha256[:2] {
			first, later = a, b
			break
		}
	}

	e := eng.(*engine)
	var refreshed atomic.Bool
	fn := ReferencedFunc(func() (map[string]struct{}, error) {
		if !refreshed.CompareAndSwap(false, true) {
			// Apply-phase refresh: an upload of `later` commits right now —
			// acquire-before-publish, exactly what Session.Commit does.
			e.holds.acquire(later.Sha256)
		}
		return map[string]struct{}{}, nil
	})

	deleted, err := eng.GCSweep(ctx, fn, time.Nanosecond, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != first.Sha256 {
		t.Fatalf("apply = %v, want only [%s] (the mid-sweep hold must protect %s)", deleted, first.Sha256, later.Sha256)
	}
	if _, err := eng.Stat(ctx, later.Sha256); err != nil {
		t.Fatalf("blob held mid-sweep was deleted: %v", err)
	}
}

// ---------------------------------------------------------------------------
// S3 + migration legs
// ---------------------------------------------------------------------------

// TestS3HeldBlobSurvivesZeroGraceApply: the S3 engine registers the hold at
// s3Session.Commit (before the CopyObject that publishes the blob key), so
// an un-released multipart commit survives a grace=0 apply and becomes
// collectable after the release.
func TestS3HeldBlobSurvivesZeroGraceApply(t *testing.T) {
	eng, _, _ := newS3Engine(t)
	ctx := context.Background()
	empty := func() (map[string]struct{}, error) { return map[string]struct{}{}, nil }

	inFlight := commitUnreleased(t, eng, []byte("s3 in-flight layer"))
	deleted, err := eng.GC(ctx, empty, time.Nanosecond, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 {
		t.Fatalf("grace=0 apply deleted a held S3 blob: %v", deleted)
	}
	if _, _, err := eng.Open(ctx, inFlight.Sha256); err != nil {
		t.Fatalf("held S3 blob lost: %v", err)
	}

	if err := eng.ReleaseGCHold(inFlight.Sha256); err != nil {
		t.Fatal(err)
	}
	deleted, err = eng.GC(ctx, empty, time.Nanosecond, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != inFlight.Sha256 {
		t.Fatalf("apply after release = %v, want [%s]", deleted, inFlight.Sha256)
	}
}

// TestMigrationDualWriteHoldProtectsBothBackends: a dual-write Commit
// registers the sha in BOTH engines' hold sets, and the migration face's
// ReleaseGCHold reaches both — each backend's sweep is protected by its own
// set, so both must skip the held blob and both must collect after the
// release.
func TestMigrationDualWriteHoldProtectsBothBackends(t *testing.T) {
	h := newHarness(t, true, false, 5)
	ctx := context.Background()
	empty := func() (map[string]struct{}, error) { return map[string]struct{}{}, nil }

	// Raw dual-write session, no release: both backends hold.
	sess, err := h.me.BeginSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Append(ctx, strings.NewReader("dual-write in-flight")); err != nil {
		t.Fatal(err)
	}
	ref, err := sess.Commit(ctx, BlobRef{})
	if err != nil {
		t.Fatal(err)
	}

	if deleted, err := h.disk.GC(ctx, empty, time.Nanosecond, true); err != nil {
		t.Fatal(err)
	} else if len(deleted) != 0 {
		t.Fatalf("disk sweep deleted a held blob: %v", deleted)
	}
	if deleted, err := h.s3.GC(ctx, empty, time.Nanosecond, true); err != nil {
		t.Fatal(err)
	} else if len(deleted) != 0 {
		t.Fatalf("s3 sweep deleted a held blob: %v", deleted)
	}
	if !h.diskHas(ref.Sha256) || !h.s3Has(ref.Sha256) {
		t.Fatal("held dual-write blob missing from a backend")
	}

	// The migration face releases both sets.
	if err := h.me.ReleaseGCHold(ref.Sha256); err != nil {
		t.Fatal(err)
	}
	if deleted, err := h.disk.GC(ctx, empty, time.Nanosecond, true); err != nil {
		t.Fatal(err)
	} else if len(deleted) != 1 {
		t.Fatalf("disk apply after release = %v, want the blob", deleted)
	}
	if deleted, err := h.s3.GC(ctx, empty, time.Nanosecond, true); err != nil {
		t.Fatal(err)
	} else if len(deleted) != 1 {
		t.Fatalf("s3 apply after release = %v, want the blob", deleted)
	}
}
