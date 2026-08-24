package httpapi

// POST /binflow/api/v1/system/gc — the managed GC face (T-94, PRD FR-30 /
// GE-03, ADR-0015 erratum ① and ③). The mark-sweep kernel is storage's
// GCSweep ([M9] ADR-0031: this REST face runs in the serve process, so the
// engine's in-flight hold set is directly visible and the gcMarker's
// single-point Live closes the stale-snapshot window); this file adds
// only the REST management plane: body parsing with a dry-run default, the
// data-directory maintenance lock shared with the gc/export CLIs (409 on
// contention, never a queue), the response shape, the gc.run audit event
// and the blobs-ledger teardown the CLI run performs.
//
// Execution is synchronous (PRD Q6 interim): one POST runs mark+sweep to
// completion and answers with the counts; very large stores stay on the
// CLI gc face (same semantics, out of band). The run is bound to the
// maintenance lock, not the connection — a client hang-up mid-apply must
// not strand half-deleted state (see sweepCtx in the handler). Grace keeps
// its ADR-0006 meaning untouched — the mtime-based window is what makes an
// online run safe next to serve's writers, and the online trigger is not an
// emergency delete channel (it cannot shorten grace below what the request
// states).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// ErrBlobNotListed marks a GC candidate the engine's inventory no longer
// carries at sizing time (deleted by a racing writer or foreign sweep).
var ErrBlobNotListed = errors.New("blob missing from the engine inventory")

// GarbageCollector is the consumer-side seam over storage.Engine's
// mark-sweep face ([M9] ADR-0031: the GCMarker-carrying GCSweep; the legacy
// single-callback GC face was retired from this call surface with the same
// change — the interface method the engine already implements, no adapter
// in between). cmd wires the opened engine; the test harness wires the same
// real engine. Assemblies without one leave it nil and the endpoint answers
// 503 rather than pretending a run happened.
type GarbageCollector interface {
	GCSweep(ctx context.Context, m storage.GCMarker, grace time.Duration, apply bool) ([]string, error)
}

// gcMarker is the REAL GCMarker the REST face runs with since T-256
// (ADR-0031 decision 3 / architecture section 14.2 point 3): Mark is the
// liveChecksumSet walk, Live is the metadata store's single-point
// IsReferenced probe — the per-candidate freshness oracle that closes W-2.
// The engine invokes both synchronously inside one GCSweep call, so the
// sweep's detached context rides along here.
type gcMarker struct {
	ctx context.Context
	md  metadata.Store
}

// Mark implements storage.GCMarker (the liveChecksumSet snapshot).
func (m gcMarker) Mark() (map[string]struct{}, error) {
	set, err := liveChecksumSet(m.ctx, m.md)
	if err != nil {
		return nil, fmt.Errorf("gc: referenced set: %w", err)
	}
	return set, nil
}

// Live implements storage.GCMarker (the single-point recheck).
func (m gcMarker) Live(sha256 string) (bool, error) {
	live, err := m.md.IsReferenced(m.ctx, sha256)
	if err != nil {
		return false, fmt.Errorf("gc: reference recheck %s: %w", sha256, err)
	}
	return live, nil
}

// maxGCHours bounds an explicit graceHours (100 years). Beyond it the
// time.Duration conversion would overflow negative, which storage.GC would
// silently read as "use the default grace" — an explicit 400 keeps the
// request honest instead of quietly meaning something else.
const maxGCHours = 100 * 365 * 24

// gcRequestBody is the GE-03 trigger body {"apply":bool,"graceHours":int?}.
// apply defaults to false — dry-run is the default posture, and that IS the
// confirmation form ADR-0015 erratum ① settled on: applying is the explicit
// "apply":true step taken after reviewing a dry-run (the console adds its
// own second confirmation on top, T-102). graceHours is a pointer so ABSENT
// (use storage.gc_grace_hours) stays distinct from an explicit 0 (no grace
// window at all).
type gcRequestBody struct {
	Apply      bool `json:"apply"`
	GraceHours *int `json:"graceHours"`
}

// gcResponse is the GE-03 result body. A dry-run reports candidates and
// never touches data (deletedCount pinned to 0); an apply reports the
// candidates the pre-pass observed plus what the sweep actually deleted —
// equal on a quiet instance, and an honest divergence (not an error) when
// serve's writers raced the two passes.
type gcResponse struct {
	CandidateCount int   `json:"candidateCount"`
	CandidateBytes int64 `json:"candidateBytes"`
	DeletedCount   int   `json:"deletedCount"`
}

// gcRunDetail is the gc.run audit payload (FR-30-AC5: 候选数/释放字节 — the
// audit trail IS the gc history surface, because the GET status endpoint is
// a recorded P2 debt, ADR-0015 erratum ②). graceHours is null exactly when
// the request left it absent; the effective grace is then the configured
// storage.gc_grace_hours.
type gcRunDetail struct {
	Apply          bool  `json:"apply"`
	GraceHours     *int  `json:"graceHours"`
	CandidateCount int   `json:"candidateCount"`
	CandidateBytes int64 `json:"candidateBytes"`
	DeletedCount   int   `json:"deletedCount"`
}

// handleSystemGC serves POST /binflow/api/v1/system/gc. The route gate
// already demanded an authenticated admin; the handler re-checks because it
// writes the actor into the audit trail and must not depend on route edits
// staying careful.
func (s *Server) handleSystemGC(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "gc requires an administrator account")
		return
	}
	if s.deps.GC == nil {
		writeError(w, http.StatusServiceUnavailable, "gc is not available on this instance")
		return
	}

	var body gcRequestBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		// EOF alone is an empty body: the dry-run default posture, same
		// reading the repo PUT family gives a missing body.
		writeError(w, http.StatusBadRequest, "request body is not valid gc request JSON: "+err.Error())
		return
	}

	// Grace resolution (PRD FR-30): absent graceHours uses
	// storage.gc_grace_hours; an explicit 0 asks for no grace window.
	// storage.GC maps grace <= 0 to its own 24h default (architecture
	// 3.1: zero is NOT an immediate-collect request), so "no window" is
	// carried as the sub-second duration that contract prescribes — the
	// W24 orphan recipe (upload, delete the node, ask with
	// graceHours:0) depends on exactly this reading.
	grace := s.deps.Config.Storage.GCGrace
	if body.GraceHours != nil {
		if *body.GraceHours < 0 || *body.GraceHours > maxGCHours {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"graceHours must be between 0 and %d, got %d", maxGCHours, *body.GraceHours))
			return
		}
		grace = gcGraceFromHours(*body.GraceHours)
	}

	// Data-directory maintenance lock — the SAME primitive the gc and
	// export CLIs hold (ADR-0015 erratum ③): an export run refuses this
	// POST with 409 and vice versa, and a second concurrent gc (REST or
	// CLI, cross-process or not) is refused the same way. Fail fast,
	// never queue: a queued GC would hold its HTTP connection hostage
	// for an unbounded export.
	lock, err := storage.AcquireDataLock(s.deps.DataDir, storage.DataLockOpGC)
	if err != nil {
		if errors.Is(err, storage.ErrDataLockHeld) {
			writeError(w, http.StatusConflict, s.gcLockRefusedMessage(err))
			return
		}
		s.log.ErrorContext(r.Context(), "httpapi: gc data lock acquisition failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "gc: acquiring the data directory lock failed")
		return
	}
	defer func() { _ = lock.Release() }()

	// The run executes detached from the connection's lifetime (T-94 review
	// N1): a client hang-up, a proxy timeout or a drain overrun would cancel
	// r.Context() mid-sweep, and an apply canceled between deletions leaves
	// the already-swept blobs behind as permanent phantom blobs-ledger rows
	// (the sweep only looks at disk — a row without a file has no
	// self-healing path) with no gc.run row for the partially-effective
	// deletion. WithoutCancel keeps every value (principal et al.) and drops
	// only the cancellation: one HTTP connection's lifespan must not bound a
	// maintenance run that already holds the data-directory lock. If the
	// client is gone the response write fails silently, which is the honest
	// outcome — the run itself completed and was audited. The storage.GCSweep
	// kernel is untouched.
	sweepCtx := context.WithoutCancel(r.Context())
	marker := gcMarker{ctx: sweepCtx, md: s.deps.Metadata}

	// Pass 1 is always the dry pass: it yields the candidate list (and,
	// while every file still exists, their on-disk bytes). apply runs a
	// second sweep that deletes — the counts then describe what the
	// pre-pass saw vs what the sweep freed.
	started := time.Now()
	candidates, err := s.deps.GC.GCSweep(sweepCtx, marker, grace, false)
	if err != nil {
		s.log.ErrorContext(sweepCtx, "httpapi: gc sweep failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "gc: "+err.Error())
		return
	}
	// Candidate sizing is engine-aware (T-201, T-173 D-1): an S3-backed
	// instance sizes the candidates from the engine's bucket listing — the
	// disk-only walk below could only ever log "sizing incomplete" there,
	// because the candidates name bucket objects, not files under blobs/.
	var candidateBytes int64
	var statErr error
	if s.deps.BlobInventory != nil {
		candidateBytes, statErr = sumCandidateBytesFromInventory(sweepCtx, s.deps.BlobInventory, candidates)
	} else {
		candidateBytes, statErr = sumBlobFileSizes(s.deps.DataDir, candidates)
	}
	if statErr != nil {
		// The sweep listed these files a moment ago; a stat failure means
		// something foreign raced the tree. The count stays honest (the
		// blob counts, its bytes do not) and the log carries the cause.
		s.log.WarnContext(sweepCtx, "httpapi: gc candidate sizing incomplete", "error", statErr.Error())
	}

	var deleted []string
	if body.Apply {
		deleted, err = s.deps.GC.GCSweep(sweepCtx, marker, grace, true)
		if err != nil {
			s.log.ErrorContext(sweepCtx, "httpapi: gc apply pass failed", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "gc: "+err.Error())
			return
		}
		// A real deletion removes the blobs-ledger row with it (the same
		// teardown the CLI run performs): the ledger is the "ever
		// existed" record (ADR-0006) and a row whose physical file is
		// gone must not survive as a phantom.
		for _, sha := range deleted {
			if err := s.deps.Metadata.Blobs().Delete(sweepCtx, sha); err != nil && !errors.Is(err, metadata.ErrNotFound) {
				s.log.WarnContext(sweepCtx, "httpapi: dropping blobs ledger row failed",
					"sha256", sha, "error", err.Error())
			}
		}
	}

	resp := gcResponse{
		CandidateCount: len(candidates),
		CandidateBytes: candidateBytes,
		DeletedCount:   len(deleted),
	}

	detail, derr := json.Marshal(gcRunDetail{
		Apply:          body.Apply,
		GraceHours:     body.GraceHours,
		CandidateCount: resp.CandidateCount,
		CandidateBytes: resp.CandidateBytes,
		DeletedCount:   resp.DeletedCount,
	})
	if derr != nil {
		s.log.WarnContext(sweepCtx, "httpapi: gc audit detail marshal failed", "error", derr.Error())
		detail = []byte("{}")
	}
	s.audit.Record(sweepCtx, audit.Event{
		Actor:      p.Name,
		Action:     audit.ActionGCRun,
		RemoteAddr: r.RemoteAddr,
		Detail:     string(detail),
	})
	mode := "dry-run"
	if body.Apply {
		mode = "apply"
	}
	s.log.InfoContext(sweepCtx, "httpapi: gc run complete",
		"mode", mode,
		"grace", grace.String(),
		"candidates", resp.CandidateCount,
		"candidate_bytes", resp.CandidateBytes,
		"deleted", resp.DeletedCount,
		"elapsed", time.Since(started).Round(time.Millisecond).String(),
	)

	b, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render gc response: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// gcGraceFromHours maps an explicit graceHours onto the duration
// storage.GC consumes: 0 is the no-window request, carried as the
// sub-second duration the engine contract defines for it (see the handler
// comment); anything positive is hours.
func gcGraceFromHours(hours int) time.Duration {
	if hours == 0 {
		return time.Nanosecond
	}
	return time.Duration(hours) * time.Hour
}

// gcLockRefusedMessage shapes the 409 body. The operation word comes from
// the lock file's holder record read through the storage helpers (T-96
// architecture review N1/N3 — never parsed out of the acquire error
// string): "export in progress" is the PRD's literal requirement when an
// export holds the lock (FR-30/GE-03), another gc run names itself, and an
// unreadable record (Windows byte-range lock, stale empty file) falls back
// to the generic maintenance wording instead of guessing. err's own text
// carries the ErrDataLockHeld sentence and the holder diagnostics, so the
// envelope stays self-describing in every branch.
func (s *Server) gcLockRefusedMessage(err error) string {
	var because string
	switch storage.HolderOp(storage.LockHolder(s.deps.DataDir)) {
	case storage.DataLockOpExport:
		because = "export in progress; "
	case storage.DataLockOpGC:
		because = "another gc run is in progress; "
	default:
		because = "another maintenance operation is in progress; "
	}
	return "gc rejected: " + because + err.Error()
}

// sumCandidateBytesFromInventory sizes GC candidates through the engine's
// read-only blob listing (the S3-backend arm, T-201): one bucket walk
// answers every candidate's stored size. A candidate absent from the
// listing contributes zero and is reported through the error — the same
// "vanished between sweep and sizing" reading sumBlobFileSizes gives a
// disk-resident blob that disappeared mid-run.
func sumCandidateBytesFromInventory(ctx context.Context, inv BlobInventory, shas []string) (int64, error) {
	stats, err := inv.BlobStats(ctx)
	if err != nil {
		return 0, fmt.Errorf("sizing candidates through the storage engine: %w", err)
	}
	var total int64
	var firstErr error
	for _, sha := range shas {
		st, ok := stats[sha]
		if !ok {
			if firstErr == nil {
				firstErr = fmt.Errorf("blob %s: %w", sha, ErrBlobNotListed)
			}
			continue
		}
		total += st.Size
	}
	return total, firstErr
}

// sumBlobFileSizes totals the on-disk size of the given blobs through the
// exported BlobPath helper (the engine's own path shape). A blob that
// vanished between sweep and stat contributes zero and is reported through
// the error — while the maintenance lock is held only serve writes blobs,
// so this branch signals foreign interference rather than an expected
// race.
func sumBlobFileSizes(dataDir string, shas []string) (int64, error) {
	var total int64
	var firstErr error
	for _, sha := range shas {
		path, err := storage.BlobPath(dataDir, sha)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("blob path %s: %w", sha, err)
			}
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("stat %s: %w", path, err)
			}
			continue
		}
		total += info.Size()
	}
	return total, firstErr
}

// liveChecksumSet computes the GC mark set: every sha256 referenced by any
// node row UNION every blob_digest referenced by any docker_refs row
// (architecture sections 4.4 and 11.12 — "SELECT DISTINCT sha256 FROM nodes
// UNION SELECT DISTINCT blob_digest FROM docker_refs"). It is the same
// consumer-side walk cmd/binflow-server's gc performs over the public
// store surfaces: storage never reads metadata by design and metadata
// offers no DISTINCT helper, so every GC caller builds the set itself —
// keep the two walks in sync (same shape, same skips).
//
// Folder marker rows are skipped (T-124, same exclusion as the snapshot
// manifest boundary): their sha256 is the shared metadata.FolderMarkerSHA
// sentinel with no physical file behind it, so no sweep decision changes —
// the skip keeps the mark set exactly "physical blob shas".
func liveChecksumSet(ctx context.Context, md metadata.Store) (map[string]struct{}, error) {
	set := map[string]struct{}{}

	repos, err := md.Repos().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing repositories: %w", err)
	}
	for _, r := range repos {
		nodes, err := md.Nodes().ListByPrefix(ctx, r.RepoKey, "")
		if err != nil {
			return nil, fmt.Errorf("listing nodes of %s: %w", r.RepoKey, err)
		}
		for _, n := range nodes {
			if n.Sha256 != "" && n.Sha256 != metadata.FolderMarkerSHA {
				set[n.Sha256] = struct{}{}
			}
		}

		images, err := md.Docker().ListImages(ctx, r.RepoKey, "", 0)
		if err != nil {
			return nil, fmt.Errorf("listing docker images of %s: %w", r.RepoKey, err)
		}
		for _, image := range images {
			manifests, err := md.Docker().ListManifestsByImage(ctx, r.RepoKey, image)
			if err != nil {
				return nil, fmt.Errorf("listing manifests of %s/%s: %w", r.RepoKey, image, err)
			}
			for _, m := range manifests {
				refs, err := md.Docker().ListRefsByManifest(ctx, r.RepoKey, image, m.Digest)
				if err != nil {
					return nil, fmt.Errorf("listing refs of %s/%s@%s: %w", r.RepoKey, image, m.Digest, err)
				}
				for _, ref := range refs {
					if ref.BlobDigest != "" {
						set[ref.BlobDigest] = struct{}{}
					}
				}
			}
		}
	}
	return set, nil
}
