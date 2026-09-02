package remote

// T-422 (FR-138.3, replication.md §9.2-B-4) — the pull half of the global
// blockPush/blockPull brake: while the installed probe reports a block, the
// pull-through plane contacts NO upstream at all. Cached copies inside
// their TTL keep serving (step 4 never reaches the network), an expired
// copy degrades STALE with the brake named in the hint, a miss answers the
// family's unfound 404 naming the BRAKE (never "assumed offline" — the
// window is not written, lifting the brake restores service immediately),
// and a hardFail repository answers its 502.

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// blocked probes for the install/uninstall cycle.
func blocked(b bool) func() bool { return func() bool { return b } }

func TestPullBlockStopsUpstreamContact(t *testing.T) {
	e := newFetchEnv(t, func(_ *metadata.Repo, _ *metadata.RemoteConfig) {})
	e.state.files["/dir/up.bin"] = "v1"
	InstallPullBlockProbe(blocked(true))
	t.Cleanup(func() { InstallPullBlockProbe(nil) })

	// Miss with no copy: the unfound 404 naming the brake, ZERO upstream
	// packets (the AC's 拒绝 arm).
	res, err := e.eng.Fetch(context.Background(), "generic-remote", "dir/up.bin")
	fe := fetchErr(t, res, err)
	if fe.Status != http.StatusNotFound || !fe.Unfound {
		t.Fatalf("blocked miss = (%d, unfound=%t), want (404, true)", fe.Status, fe.Unfound)
	}
	if !strings.Contains(fe.Message, "pull replication is blocked") {
		t.Fatalf("message must name the brake, got %q", fe.Message)
	}
	if got := e.hits.Load(); got != 0 {
		t.Fatalf("upstream hits while blocked = %d, want 0", got)
	}

	// Lift the brake: the same fetch lands normally — no offline window was
	// written by the block (the 照既有语义降级 arm's recovery half).
	InstallPullBlockProbe(blocked(false))
	fetched := mustFetch(t, e, "dir/up.bin")
	if fetched.CacheState != CacheMiss {
		t.Fatalf("post-unblock state = %q, want MISS", fetched.CacheState)
	}
	if got := e.hits.Load(); got != 1 {
		t.Fatalf("upstream hits after unblock = %d, want 1", got)
	}

	// Re-block past the TTL: the expired copy degrades STALE with the brake
	// in the hint, still zero new upstream packets.
	InstallPullBlockProbe(blocked(true))
	e.clk.Advance(7201 * time.Second)
	stale := mustFetch(t, e, "dir/up.bin")
	if stale.CacheState != CacheStale {
		t.Fatalf("blocked expired-copy state = %q, want STALE", stale.CacheState)
	}
	if body := readAll(t, stale); body != "v1" {
		t.Fatalf("stale body = %q, want the cached v1", body)
	}
	if !strings.Contains(stale.UpstreamError, "pull replication is blocked") {
		t.Fatalf("stale hint = %q, want the brake named", stale.UpstreamError)
	}
	if got := e.hits.Load(); got != 1 {
		t.Fatalf("upstream hits on the blocked stale serve = %d, want still 1", got)
	}

	// No probe installed at all: the pre-T-422 posture, nothing blocked.
	InstallPullBlockProbe(nil)
	if PullBlocked() {
		t.Fatalf("uninstalled probe must report unblocked")
	}
}

func TestPullBlockHardFailAnswers502(t *testing.T) {
	e := newFetchEnv(t, func(row *metadata.Repo, _ *metadata.RemoteConfig) {
		row.Config = strings.Replace(row.Config, `"hardFail":false`, `"hardFail":true`, 1)
	})
	InstallPullBlockProbe(blocked(true))
	t.Cleanup(func() { InstallPullBlockProbe(nil) })
	res, err := e.eng.Fetch(context.Background(), "generic-remote", "nope.bin")
	fe := fetchErr(t, res, err)
	if fe.Status != http.StatusBadGateway {
		t.Fatalf("blocked hardFail status = %d, want 502", fe.Status)
	}
	if !strings.Contains(fe.Message, "pull replication is blocked") {
		t.Fatalf("hardFail message must name the brake, got %q", fe.Message)
	}
	if got := e.hits.Load(); got != 0 {
		t.Fatalf("upstream hits while blocked = %d, want 0", got)
	}
}

// TestPullBlockFreshCopyStillServes pins the placement: the gate sits AFTER
// the local-copy step — a copy inside its TTL serves HIT with zero upstream
// contact whether or not the brake is on (blocking pull replication never
// takes the cache itself offline).
func TestPullBlockFreshCopyStillServes(t *testing.T) {
	e := newFetchEnv(t, func(_ *metadata.Repo, _ *metadata.RemoteConfig) {})
	e.state.files["/dir/up.bin"] = "v1"
	mustFetch(t, e, "dir/up.bin") // land the copy while unblocked
	InstallPullBlockProbe(blocked(true))
	t.Cleanup(func() { InstallPullBlockProbe(nil) })

	res := mustFetch(t, e, "dir/up.bin")
	if res.CacheState != CacheHit {
		t.Fatalf("blocked fresh-copy state = %q, want HIT", res.CacheState)
	}
	if got := e.hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want still 1 (the cache serve)", got)
	}
}
