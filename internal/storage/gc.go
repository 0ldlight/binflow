package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// DefaultGCGrace is the default grace period separating a blob's creation
// from its eligibility for collection. It exists to prevent a sweep from
// racing an in-flight upload whose metadata reference has not landed yet
// (blob-first ordering, architecture section 3.3).
const DefaultGCGrace = 24 * time.Hour

// gcResult carries the outcome of one mark-sweep pass.
type gcResult struct {
	Candidates []string // sha256 values eligible for deletion (always populated)
	Deleted    []string // sha256 values actually deleted (apply=true only)
}

// gcRecheck is the apply-phase half of the [M9] delete gates (ADR-0031
// mechanism A): it answers "is this candidate referenced RIGHT NOW" for each
// blob immediately before its deletion, closing the stale-snapshot window
// (W-2) that the candidacy snapshot cannot.
//
// Markers with a true single-point Live are asked per candidate. The legacy
// ReferencedFunc form has no such oracle — re-running it per candidate would
// cost one full reference-set rebuild per deleted blob — so the engine
// substitutes ONE refreshed Mark snapshot for the whole pass, taken lazily
// at the first gated candidate ("mark 集重查"). That closes W-2 at snapshot
// granularity: references landing after the refresh but before a candidate's
// gate are seen only by the hold-set gate (see GCSweep), which is why the
// full single-point form remains the recommended wiring.
type gcRecheck struct {
	marker GCMarker
	legacy bool

	fetched bool                // refresh snapshot taken (legacy form only)
	set     map[string]struct{} // the refreshed snapshot
	err     error               // its error, if any
}

func newGCRecheck(m GCMarker) *gcRecheck {
	_, legacy := m.(ReferencedFunc)
	return &gcRecheck{marker: m, legacy: legacy}
}

// live answers the pre-delete reference question for one candidate.
func (r *gcRecheck) live(sha string) (bool, error) {
	if !r.legacy {
		return r.marker.Live(sha)
	}
	if !r.fetched {
		r.set, r.err = r.marker.Mark()
		r.fetched = true
	}
	if r.err != nil {
		return false, r.err
	}
	_, ok := r.set[sha]
	return ok, nil
}

// gcDeleteGate is the load-bearing sequence of Engine.GCSweep's apply mode:
// the two checks whose ORDER is a correctness invariant, not a style choice.
//
//  1. hold gate  — the engine's in-flight hold set (ADR-0031 mechanism B)
//  2. Live gate  — the marker's reference recheck (mechanism A)
//  3. delete
//
// The hold gate MUST run before the Live gate. Sketch of the argument (the
// full version lives in GCSweep's godoc): a fresh publisher's timeline is
//
//	acquire(hold) -> publish(rename/copy) -> metadata commit -> release(hold)
//
// with acquire strictly before publish, and the sweep's scan having observed
// the publish. If the metadata commit lands before the Live gate, Live sees
// the reference and the blob survives. If it lands after, the release is
// still pending at the (earlier) hold gate — the hold gate sees the
// registration and the blob survives. Inverting the gates re-opens the
// window: a commit landing between Live and the hold gate is invisible to
// Live (too early) while its release has already emptied the hold set (the
// hold gate is then too late).
type gcDeleteGate struct {
	holds   *holdSet
	recheck *gcRecheck
}

// skip reports whether the candidate must NOT be deleted right now. A
// recheck error skips conservatively and is surfaced to the caller as the
// sweep's first error (no deletion happens for that blob this pass).
func (g *gcDeleteGate) skip(sha string) (bool, error) {
	if g.holds.held(sha) {
		return true, nil
	}
	live, err := g.recheck.live(sha)
	if err != nil {
		return true, err
	}
	return live, nil
}

// GC implements the legacy Engine.GC face: it adapts the callback form to
// GCMarker and delegates. nil referenced is rejected for the M4~M8 contract
// (TestGCZeroGraceDefaultsAndNilCallback pins it).
func (e *engine) GC(ctx context.Context, referenced func() (map[string]struct{}, error), grace time.Duration, apply bool) ([]string, error) {
	if referenced == nil {
		return nil, errors.New("storage: gc: referenced callback is nil")
	}
	return e.GCSweep(ctx, ReferencedFunc(referenced), grace, apply)
}

// ReleaseGCHold implements Engine.ReleaseGCHold: pure in-memory bookkeeping,
// it cannot fail and stays a no-op after Close (a shutdown-path release must
// not become log noise).
func (e *engine) ReleaseGCHold(sha256 string) error {
	e.holds.release(sha256)
	return nil
}

// GCSweep implements Engine.GCSweep: mark-sweep over the blob directory with
// the [M9] ADR-0031 concurrency gates.
//
//   - mark: m.Mark() yields the live sha256 set (nodes ∪ docker_refs in the
//     metadata store; storage itself never reads metadata).
//   - candidacy: every on-disk blob that is unreferenced, not in the hold
//     set, and older than grace is a candidate. The hold gate runs BEFORE
//     the grace check (a held sha is skipped however old its mtime is).
//   - apply: each candidate passes the gcDeleteGate immediately before its
//     deletion — hold set re-check, then Live.
//
// Soundness argument (why a concurrent uploader's blob can no longer be
// deleted out from under it, grace=0 included):
//
// A publisher that wants sha B referenced executes
//
//	H: holds.acquire(B)          (Session.Commit, before the rename)
//	P: rename into blobs/        (B becomes scan-visible)
//	M: metadata commit           (B becomes Mark/Live-visible)
//	R: ReleaseGCHold(B)          (repo, after M)
//
// with H < P < M < R. A sweep that deletes B must have scanned B (so its
// scan is after P, hence after H), passed the hold gate at some T_h, passed
// the Live gate at T_l > T_h, and unlinked at T_d > T_l. Two cases:
//
//   - M ≤ T_l: a faithful Live sees the reference — the Live gate skips B.
//   - M > T_l: then R > M > T_l > T_h, so at T_h the registration H (< P <
//     scan < T_h) is present and unreleased — the hold gate skips B.
//
// Either way B survives. This requires Live to be a true single-point query
// (see GCMarker); with the legacy ReferencedFunc form the Live gate reads a
// snapshot refreshed at the start of the gated phase, which shrinks — but
// does not eliminate — the residual window to (refresh, unlink). The
// documented residual races, bounded to the microsecond class of any
// check-then-unlink filesystem sequence: a dedup-hit Commit (no fresh
// publish to anchor its acquire before the scan) and the gap between the
// last gate and the unlink itself. Eliminating those would need the
// write-quiesce design ADR-0031 rejected as disproportionate (candidate C).
//
// The returned []string is the candidate list in dry-run and the deleted
// list in apply mode (ticket T-9 contract). Candidates skipped by the
// delete gates appear in neither count: the numbers only ever get more
// conservative (ADR-0031 point 5).
//
// grace <= 0 still means DefaultGCGrace; holds are consulted regardless of
// grace — an explicitly tiny grace (the W24 graceHours:0 recipe) is exactly
// the case the hold set exists for.
func (e *engine) GCSweep(ctx context.Context, m GCMarker, grace time.Duration, apply bool) ([]string, error) {
	return e.gcSweep(ctx, m, grace, apply, true)
}

// gcSweep is GCSweep with a hold-gate switch: the background migration's
// blob enumeration needs the raw inventory (every blob, holds included) and
// calls it with countHolds=false; every GC face passes true.
func (e *engine) gcSweep(ctx context.Context, m GCMarker, grace time.Duration, apply, countHolds bool) ([]string, error) {
	if m == nil {
		return nil, errors.New("storage: gc: marker is nil")
	}
	if grace <= 0 {
		grace = DefaultGCGrace
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("storage: gc: %w", err)
	}
	if err := e.checkOpen(); err != nil {
		return nil, fmt.Errorf("storage: gc: %w", err)
	}

	refs, err := m.Mark()
	if err != nil {
		return nil, fmt.Errorf("storage: gc: referenced set: %w", err)
	}

	var gate *gcDeleteGate
	if apply {
		gate = &gcDeleteGate{holds: e.holds, recheck: newGCRecheck(m)}
	}

	var res gcResult
	shardRoot := filepath.Join(e.root, blobsDirName)
	shards, err := os.ReadDir(shardRoot)
	if err != nil {
		return nil, fmt.Errorf("storage: gc: scan %s: %w", shardRoot, err)
	}

	now := e.opts.now()
	var firstErr error
	for _, shard := range shards {
		if err := ctx.Err(); err != nil {
			return res.Candidates, fmt.Errorf("storage: gc: %w", err)
		}
		if !shard.IsDir() || !validShardName(shard.Name()) {
			continue
		}
		blobs, err := os.ReadDir(filepath.Join(shardRoot, shard.Name()))
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("storage: gc: scan shard %s: %w", shard.Name(), err)
			}
			continue
		}
		for _, b := range blobs {
			sum := b.Name()
			if b.IsDir() || !validSha256(sum) || sum[:2] != shard.Name() {
				continue
			}
			if _, ok := refs[sum]; ok {
				continue // mark hit: keep
			}
			if countHolds && e.holds.held(sum) {
				continue // in-flight upload owns this sha: keep, grace aside
			}
			path := filepath.Join(shardRoot, shard.Name(), sum)
			info, err := b.Info()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("storage: gc: stat %s: %w", path, err)
				}
				continue
			}
			if now.Sub(info.ModTime()) <= grace {
				continue // inside grace window: skip
			}
			res.Candidates = append(res.Candidates, sum)
			if apply {
				skip, err := gate.skip(sum)
				if err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("storage: gc: live recheck %s: %w", sum, err)
					}
					continue // conservative: no deletion on an uncertain answer
				}
				if skip {
					continue
				}
				if err := e.Delete(ctx, sum); err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("storage: gc: delete %s: %w", sum, err)
					}
					continue
				}
				res.Deleted = append(res.Deleted, sum)
			}
		}
	}
	sort.Strings(res.Candidates)
	sort.Strings(res.Deleted)
	if firstErr != nil {
		return res.Candidates, firstErr
	}
	if apply {
		return res.Deleted, nil
	}
	return res.Candidates, nil
}

// gcList enumerates every blob under blobs/ with all GC candidacy gates
// disabled — the raw inventory the background migration diffing needs.
// Grace is sub-second so every blob qualifies; the empty legacy marker keeps
// nothing referenced; countHolds=false keeps in-flight shas listed.
func (e *engine) gcList(ctx context.Context) ([]string, error) {
	return e.gcSweep(ctx, emptyMarker{}, time.Nanosecond, false, false)
}

// emptyMarker is the always-empty GCMarker for inventory-only sweeps.
type emptyMarker struct{}

func (emptyMarker) Mark() (map[string]struct{}, error) { return map[string]struct{}{}, nil }

func (emptyMarker) Live(string) (bool, error) { return false, nil }

// validShardName accepts exactly two lowercase hex characters, so the sweep
// never follows foreign directories into unexpected paths.
func validShardName(name string) bool {
	if len(name) != 2 {
		return false
	}
	for i := 0; i < 2; i++ {
		c := name[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
