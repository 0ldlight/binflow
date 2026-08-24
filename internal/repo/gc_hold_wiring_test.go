package repo_test

// T-256: the ADR-0031 repo-side release wiring ([M9] architecture section
// 14.2 point 1). The engine layer (T-255) acquires an in-flight GC hold at
// every session Commit; these tests pin that the FIVE landing paths hand
// that hold back at the right moment — after the metadata rows referencing
// the blob have committed — observable through the sweep's candidacy: a
// released hold plus a deleted node makes the blob a grace=0 candidate
// again (the W24 recipe), an unreleased hold keeps it protected.
//
// The stress leg at the bottom is the repo-integration form of the T-232
// race (referenced blobs physically deleted by a grace=0 apply racing the
// uploads) — the CI "GC concurrency stress" gate runs it with -race.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// sweepMarker is the REAL GCMarker shape the REST and CLI gc faces use since
// T-256: Mark walks the store's public listing surfaces (the liveChecksumSet
// shape shared with cmd/httpapi), Live is the store's single-point
// IsReferenced probe (ADR-0031 mechanism A).
type sweepMarker struct {
	ctx context.Context
	md  metadata.Store
}

func (m sweepMarker) Mark() (map[string]struct{}, error) {
	set := map[string]struct{}{}
	repos, err := m.md.Repos().List(m.ctx)
	if err != nil {
		return nil, fmt.Errorf("listing repositories: %w", err)
	}
	for _, r := range repos {
		nodes, err := m.md.Nodes().ListByPrefix(m.ctx, r.RepoKey, "")
		if err != nil {
			return nil, fmt.Errorf("listing nodes of %s: %w", r.RepoKey, err)
		}
		for _, n := range nodes {
			if n.Sha256 != "" && n.Sha256 != metadata.FolderMarkerSHA {
				set[n.Sha256] = struct{}{}
			}
		}
		images, err := m.md.Docker().ListImages(m.ctx, r.RepoKey, "", 0)
		if err != nil {
			return nil, fmt.Errorf("listing docker images of %s: %w", r.RepoKey, err)
		}
		for _, image := range images {
			manifests, err := m.md.Docker().ListManifestsByImage(m.ctx, r.RepoKey, image)
			if err != nil {
				return nil, fmt.Errorf("listing manifests of %s/%s: %w", r.RepoKey, image, err)
			}
			for _, mf := range manifests {
				refs, err := m.md.Docker().ListRefsByManifest(m.ctx, r.RepoKey, image, mf.Digest)
				if err != nil {
					return nil, fmt.Errorf("listing refs of %s/%s@%s: %w", r.RepoKey, image, mf.Digest, err)
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

func (m sweepMarker) Live(sha string) (bool, error) {
	return m.md.IsReferenced(m.ctx, sha)
}

// sweep runs one grace=0 pass and returns the candidate/deleted shas.
func sweep(t *testing.T, e *env, apply bool) []string {
	t.Helper()
	out, err := e.st.GCSweep(context.Background(), sweepMarker{ctx: context.Background(), md: e.md},
		time.Nanosecond, apply)
	if err != nil {
		t.Fatalf("GCSweep(apply=%t): %v", apply, err)
	}
	return out
}

// assertCandidate asserts sha's candidacy (present/absent) in a grace=0
// dry-run — the observable of the hold wiring.
func assertCandidate(t *testing.T, e *env, sha string, want bool) {
	t.Helper()
	for _, c := range sweep(t, e, false) {
		if c == sha {
			if !want {
				t.Fatalf("sha %s is a grace=0 candidate, want still protected (hold not released / reference dropped)", sha)
			}
			return
		}
	}
	if want {
		t.Fatalf("sha %s is NOT a grace=0 candidate, want collected (the hold should have been released with the node gone)", sha)
	}
}

// commitRaw commits content through a bare storage session WITHOUT going
// through the service — the hold stays registered (the T-255 scaffolding's
// "unreleased" shape), proving the assertions above can actually see a
// held hold.
func commitRaw(t *testing.T, e *env, content string) string {
	t.Helper()
	ctx := context.Background()
	sess, err := e.st.BeginSession(ctx)
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if _, err := sess.Append(ctx, strings.NewReader(content)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	ref, err := sess.Commit(ctx, storage.BlobRef{})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return ref.Sha256
}

// TestPutReleasesGCHold pins the primary landing path: Put's node row
// commits, the hold commitBlob acquired is released, and the W24 recipe
// (upload → delete node → grace=0 collect) works again — the exact
// behavior the t94 REST legs assert on the other side of the wire. The
// control leg proves the sweep really does exclude a still-held blob.
func TestPutReleasesGCHold(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "gcl")
	ctx := context.Background()

	body := strings.Repeat("put-hold-release-payload\n", 32)
	n := put(t, e, admin(), "gcl", "a/x.bin", body)

	// While referenced: never a candidate.
	assertCandidate(t, e, n.Sha256, false)
	if err := e.svc.Delete(ctx, admin(), "gcl", "a/x.bin"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Node gone AND hold released by Put: collectable again (W24).
	assertCandidate(t, e, n.Sha256, true)

	// Control: a raw Commit nobody released stays protected at grace=0.
	held := commitRaw(t, e, "never-released-hold-payload")
	assertCandidate(t, e, held, false)
	e.st.ReleaseGCHold(held) //nolint:errcheck // test teardown of the held entry
	assertCandidate(t, e, held, true)
}

// TestPutLandedBlobReleasesGCHold pins the docker/pypi finalize shape: the
// ADAPTER owns the session Commit (the acquire), PutLandedBlob lands the
// metadata and must hand the hold back. Missing this wiring is exactly the
// t134 failure family (a pushed layer's blob deleted before its node row
// exists to protect it — protected here only for the TTL window).
func TestPutLandedBlobReleasesGCHold(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "gcl")
	ctx := context.Background()

	// The adapter's half: a bare Commit registers the hold.
	sess, err := e.st.BeginSession(ctx)
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	body := strings.Repeat("landed-finalize-payload\n", 24)
	if _, err := sess.Append(ctx, strings.NewReader(body)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	ref, err := sess.Commit(ctx, storage.BlobRef{})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// The service's half: finalize lands ledger + node, then releases.
	if _, err := e.svc.PutLandedBlob(ctx, admin(), "gcl", "layers/x.bin", ref, "application/octet-stream"); err != nil {
		t.Fatalf("PutLandedBlob: %v", err)
	}
	assertCandidate(t, e, ref.Sha256, false)
	if err := e.svc.Delete(ctx, admin(), "gcl", "layers/x.bin"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	assertCandidate(t, e, ref.Sha256, true)
}

// TestRemotePullThroughReleasesGCHold pins the fifth landing path: a MISS
// lands the upstream body through the engine's own session (acquire); once
// Fetch returns, the cached node row is committed and repo releases. A
// second GET serving the cached copy (HIT) must NOT release anything —
// the refcount-strip guard (an HIT after the release is a no-op).
func TestRemotePullThroughReleasesGCHold(t *testing.T) {
	files := map[string]string{"/up/pulled.bin": "remote-pull-hold-payload"}
	srv, hits := countingUpstream(t, files)
	e := newEnv(t)
	createRemote(t, e, srv.URL, "")
	ctx := context.Background()

	rc, node, err := e.svc.Get(ctx, admin(), "generic-remote", "up/pulled.bin")
	if err != nil {
		t.Fatalf("first Get (MISS): %v", err)
	}
	_ = rc.Close() //nolint:errcheck // read-only fd

	// The landed copy is referenced by the cache node; deleting the cache
	// (RE-06) plus the wiring's release makes it collectable at grace=0.
	assertCandidate(t, e, node.Sha256, false)
	if err := e.svc.Delete(ctx, admin(), "generic-remote", "up/pulled.bin"); err != nil {
		t.Fatalf("Delete remote cache: %v", err)
	}
	assertCandidate(t, e, node.Sha256, true)

	// The HIT path neither lands nor releases: refetch (a fresh MISS that
	// re-lands the same sha) followed by cache-hit serves must leave the
	// refcount balanced — one MISS, one release.
	rc2, node2, err := e.svc.Get(ctx, admin(), "generic-remote", "up/pulled.bin")
	if err != nil {
		t.Fatalf("refetch (MISS): %v", err)
	}
	_ = rc2.Close() //nolint:errcheck // read-only fd
	if node2.Sha256 != node.Sha256 {
		t.Fatalf("refetch landed %s, want the same sha %s", node2.Sha256, node.Sha256)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("upstream hits = %d, want 2", got)
	}
	// A pure HIT (the cache row is fresh now) — release is a no-op there,
	// so the sha stays collectable once its node goes again.
	if _, _, err := e.svc.Get(ctx, admin(), "generic-remote", "up/pulled.bin"); err != nil {
		t.Fatalf("cached Get (HIT): %v", err)
	}
	if err := e.svc.Delete(ctx, admin(), "generic-remote", "up/pulled.bin"); err != nil {
		t.Fatalf("Delete remote cache again: %v", err)
	}
	assertCandidate(t, e, node.Sha256, true)
}

// TestRepoUploadsRacingZeroGraceSweep is the repo-integration stress leg of
// the T-232 race (the CI gate, ADR-0031 section 14.2 point 6): uploads race
// grace=0 APPLY sweeps on the same engine + store, and the invariant is the
// one t134 lost — every blob whose node row is COMMITTED stays physically
// present, and every completed Put's readback works. A concurrent
// upload→delete stream (W-2's shaping) rides along; those blobs MAY be
// collected legally, so nothing asserts them.
func TestRepoUploadsRacingZeroGraceSweep(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "race")
	ctx := context.Background()

	const workers = 4
	const perWorker = 8

	var drivers sync.WaitGroup
	var hammers sync.WaitGroup
	stop := make(chan struct{})
	// Two hammer goroutines loop grace=0 apply sweeps for the race window.
	// A short breath between passes keeps the sweeps CONTENDING with the
	// uploads (the race under test) without starving them of every SQLite
	// write slot — an unthrottled hammer serializes the store and the leg
	// stops exercising interleavings.
	for h := 0; h < 2; h++ {
		hammers.Add(1)
		go func() {
			defer hammers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, err := e.st.GCSweep(ctx, sweepMarker{ctx: ctx, md: e.md}, time.Nanosecond, true)
				if err != nil && !errors.Is(err, storage.ErrBlobNotFound) {
					t.Errorf("hammer GCSweep: %v", err)
					return
				}
				// ErrBlobNotFound from a hammer delete is the benign
				// double-sweep race: the two hammers (and the legal collect
				// of the delete stream's blobs) can race one unlink. The
				// supported faces never run concurrent applies — the data
				// maintenance lock serializes them — so the engine's honest
				// error here carries no signal for this leg.
				select {
				case <-stop:
					return
				case <-time.After(3 * time.Millisecond):
				}
			}
		}()
	}

	// bodies records the exact upload body per completed Put path — the
	// readback oracle for the invariant below.
	bodies := make(map[string]string)
	var mu sync.Mutex
	for w := 0; w < workers; w++ {
		drivers.Add(1)
		go func(w int) {
			defer drivers.Done()
			for i := 0; i < perWorker; i++ {
				// Half the uploads are shared-content (dedup refcount legs),
				// half unique per worker.
				body := fmt.Sprintf("race-worker-%d-payload-%d\n", w, i)
				if i%2 == 0 {
					body = fmt.Sprintf("race-shared-payload-%d\n", i)
				}
				path := fmt.Sprintf("w%d/f%d.bin", w, i)
				if _, err := e.svc.Put(ctx, admin(), "race", path,
					strings.NewReader(body), storage.BlobRef{}, "application/octet-stream"); err != nil {
					t.Errorf("racing Put: %v", err)
					return
				}
				mu.Lock()
				bodies[path] = body
				mu.Unlock()

				// The W-2 shaping: a concurrent delete stream whose blobs
				// legally float (deleted nodes make them collectable).
				if w == 0 && i%3 == 0 {
					_ = e.svc.Delete(ctx, admin(), "race", fmt.Sprintf("w%d/f%d.bin", w, i))
				}
			}
		}(w)
	}
	drivers.Wait()
	close(stop)
	hammers.Wait()

	// Invariant 1 (t134's lost one): every COMMITTED node's blob is
	// physically openable AND reads back the exact bytes that were uploaded —
	// zero blob-not-found, zero truncated/missing content. Blobs whose nodes
	// the delete stream removed may legally have been swept; nothing asserts
	// them.
	nodes, err := e.md.Nodes().ListByPrefix(ctx, "race", "")
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("no nodes survived the race — uploads never landed")
	}
	seen := 0
	for _, n := range nodes {
		if n.Sha256 == metadata.FolderMarkerSHA {
			continue // folder marker rows ride no physical blob (T-124)
		}
		want, ok := bodies[n.Path]
		if !ok {
			t.Fatalf("surviving node %s has no recorded upload body", n.Path)
		}
		rc, _, oerr := e.st.Open(ctx, n.Sha256)
		if oerr != nil {
			t.Fatalf("node race/%s references a physically deleted blob %s: %v (the t134 failure family)",
				n.Path, n.Sha256, oerr)
		}
		got, rerr := io.ReadAll(rc)
		_ = rc.Close() //nolint:errcheck // read-only fd
		if rerr != nil {
			t.Fatalf("read back race/%s: %v", n.Path, rerr)
		}
		if string(got) != want {
			t.Fatalf("race/%s read back %d bytes, want the exact %d-byte upload", n.Path, len(got), len(want))
		}
		seen++
	}
	if seen == 0 {
		t.Fatal("every uploaded node was deleted by the shaping stream — the race never asserted anything")
	}
}
