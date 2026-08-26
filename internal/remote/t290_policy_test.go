package remote

// T-290 (FR-90.2 / L25): fetcher-side consumption of the smart remote
// effective fields —
//
//   - socketTimeoutMs: the 014 row column wins, then the canonical JSON ms
//     field, then the legacy socketTimeoutSecs, then 15000ms; a sub-second
//     timeout actually trips the outbound client (the probe the seconds
//     spelling cannot express).
//   - metadataRetrievalTimeoutSecs: the engine-wide 60s wait cap becomes
//     per-repository; a short per-repo value sends a queueing waitor to the
//     expired-copy fallback instead of waiting the engine default.
//
// missRetrievalCachePeriodSecs needs no new consumption (the negative-cache
// window already reads missedRetrievalCachePeriodSecs, and the alias
// canonicalizes to that spelling at the repo layer — pinned in
// internal/repo/t290_smart_remote_test.go).

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// TestT290EffectiveSocketTimeoutMsMatrix: the single resolution point's
// precedence matrix.
func TestT290EffectiveSocketTimeoutMsMatrix(t *testing.T) {
	cfg := func(ms int64) *metadata.RemoteConfig { return &metadata.RemoteConfig{SocketTimeoutMs: ms} }
	tests := []struct {
		name string
		cfg  *metadata.RemoteConfig
		pol  repoPolicy
		want int64
	}{
		{"row column wins over everything", cfg(2500), repoPolicy{SocketTimeoutMs: 9000, SocketTimeoutSecs: 30}, 2500},
		{"json ms beats legacy secs", nil, repoPolicy{SocketTimeoutMs: 9000, SocketTimeoutSecs: 30}, 9000},
		{"legacy secs * 1000", nil, repoPolicy{SocketTimeoutSecs: 30}, 30000},
		{"all unset -> product default", nil, repoPolicy{}, 15000},
		{"negative row column ignored (unset semantics)", cfg(-1), repoPolicy{SocketTimeoutMs: 4000}, 4000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveSocketTimeoutMs(tt.cfg, tt.pol); got != tt.want {
				t.Fatalf("effectiveSocketTimeoutMs = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestT290EffectiveMetadataWaitMatrix: row column > JSON field > engine
// default.
func TestT290EffectiveMetadataWaitMatrix(t *testing.T) {
	e := &Engine{metaWait: 60 * time.Second}
	if got := e.effectiveMetadataWait(
		&metadata.RemoteConfig{MetadataRetrievalTimeoutSecs: 30},
		repoPolicy{MetadataRetrievalTimeoutSecs: 20}); got != 30*time.Second {
		t.Fatalf("row column: %v, want 30s", got)
	}
	if got := e.effectiveMetadataWait(nil, repoPolicy{MetadataRetrievalTimeoutSecs: 20}); got != 20*time.Second {
		t.Fatalf("json field: %v, want 20s", got)
	}
	if got := e.effectiveMetadataWait(nil, repoPolicy{}); got != 60*time.Second {
		t.Fatalf("engine default: %v, want 60s", got)
	}
	custom := &Engine{metaWait: 5 * time.Second}
	if got := custom.effectiveMetadataWait(nil, repoPolicy{}); got != 5*time.Second {
		t.Fatalf("engine override: %v, want 5s", got)
	}
}

// TestT290DefaultPolicyDoesNotShadowLegacyRows: loadRemote seeds the policy
// from defaultPolicy and then unmarshals the row's JSON over it, so the
// T-290 fields must stay ZERO in the seed — a legacy row carrying only
// socketTimeoutSecs=30 must resolve to 30000ms, never the 15000ms engine
// default (the regression this test guards).
func TestT290DefaultPolicyDoesNotShadowLegacyRows(t *testing.T) {
	if defaultPolicy.SocketTimeoutMs != 0 || defaultPolicy.MetadataRetrievalTimeoutSecs != 0 ||
		defaultPolicy.UnusedCleanupPeriodHours != 0 {
		t.Fatalf("defaultPolicy seeds the T-290 fields (%+v); they must stay zero — see its comment",
			defaultPolicy)
	}
	// A pre-014 row: canonical JSON and config row without any T-290 value.
	e := newFetchEnv(t, func(row *metadata.Repo, _ *metadata.RemoteConfig) {
		row.Config = strings.Replace(row.Config, `"socketTimeoutSecs":15`, `"socketTimeoutSecs":30`, 1)
	})
	cfg, pol := e.mustLoadPolicy(t, "generic-remote")
	if got := effectiveSocketTimeoutMs(cfg, pol); got != 30000 {
		t.Fatalf("legacy row resolves to %dms, want 30000 (the row's 30s)", got)
	}
}

// TestT290ClientSignatureTracksMs: a config update from 15s to 1500ms (same
// URL/credentials) rebuilds the pooled client — the signature carries the
// effective ms value, not the legacy seconds.
func TestT290ClientSignatureTracksMs(t *testing.T) {
	e := newFetchEnv(t, nil)
	cfg, pol := e.mustLoadPolicy(t, "generic-remote")
	c1, err := e.eng.clientFor("generic-remote", cfg, pol)
	if err != nil {
		t.Fatalf("clientFor: %v", err)
	}
	cfg.SocketTimeoutMs = 1500
	c2, err := e.eng.clientFor("generic-remote", cfg, pol)
	if err != nil {
		t.Fatalf("clientFor after ms change: %v", err)
	}
	if c1 == c2 {
		t.Fatal("client not rebuilt after a socketTimeoutMs change")
	}
	if got := c1.opts.SocketTimeout; got != 15*time.Second {
		t.Fatalf("legacy client timeout = %v, want 15s", got)
	}
	if got := c2.opts.SocketTimeout; got != 1500*time.Millisecond {
		t.Fatalf("rebuilt client timeout = %v, want 1500ms", got)
	}
}

// mustLoadPolicy resolves one repository's (cfg, policy) pair.
func (e *fetchEnv) mustLoadPolicy(t *testing.T, repoKey string) (*metadata.RemoteConfig, repoPolicy) {
	t.Helper()
	ctx := context.Background()
	row, err := e.md.Repos().Get(ctx, repoKey)
	if err != nil {
		t.Fatalf("get repo %s: %v", repoKey, err)
	}
	cfg, err := e.md.Remote().GetConfig(ctx, repoKey)
	if err != nil {
		t.Fatalf("get config %s: %v", repoKey, err)
	}
	pol := defaultPolicy
	if row.Config != "" {
		if err := json.Unmarshal([]byte(row.Config), &pol); err != nil {
			t.Fatalf("policy unmarshal: %v", err)
		}
	}
	return cfg, pol
}

// TestT290SubSecondSocketTimeoutTrips: socketTimeoutMs=250 against an
// upstream that stalls 700ms before the response headers — the fetch must
// fail on the timeout arm (assumed-offline downgrade without a copy), which
// the seconds spelling could never express. This is the L25 behavior probe.
func TestT290SubSecondSocketTimeoutTrips(t *testing.T) {
	e := newFetchEnv(t, func(_ *metadata.Repo, cfg *metadata.RemoteConfig) {
		cfg.SocketTimeoutMs = 250 // sub-second: unreachable via socketTimeoutSecs
	})
	e.state.mu.Lock()
	e.state.delay = 700 * time.Millisecond
	e.state.mu.Unlock()
	e.state.files["/slow.bin"] = "payload"

	res, err := e.eng.Fetch(context.Background(), "generic-remote", "slow.bin")
	if err == nil {
		if res != nil && res.Body != nil {
			_ = res.Body.Close()
		}
		t.Fatal("fetch against a stalled upstream with a 250ms timeout unexpectedly succeeded")
	}
	if res != nil {
		t.Fatalf("expected no result, got %+v", res)
	}
	// The timeout arm is a transport fault: the repository enters the
	// assumed-offline window (the RE-04 downgrade, not a 5xx of its own).
	if _, off := e.eng.offlineWindow("generic-remote", e.clk.Now()); !off {
		t.Fatal("assumed-offline window not opened after the socket timeout")
	}
	msg := err.Error()
	if !strings.Contains(msg, "offline") && !strings.Contains(msg, "Failed to find") {
		t.Fatalf("error %q is not the offline-downgrade family", msg)
	}
}

// TestT290PerRepoMetadataWaitFallback: metadataRetrievalTimeoutSecs=1 on the
// row caps a waitor blocked behind a stalled winner at ONE second (not the
// engine's 60s default); the expired copy is served, the waitor never
// contacts the upstream itself.
func TestT290PerRepoMetadataWaitFallback(t *testing.T) {
	registerTestProvider.Do(func() { adapter.RegisterMetadata(t66Provider{}) })
	// The tuning value lands on the 014 ROW COLUMN (the primary path —
	// repo.Service writes the column alongside the canonical JSON).
	e := newFetchEnv(t, func(row *metadata.Repo, cfg *metadata.RemoteConfig) {
		row.PackageType = "npm" // ".json" classifies as metadata
		cfg.MetadataRetrievalTimeoutSecs = 1
	})
	e.state.files["/pack.json"] = "old-copy"
	if body := readAll(t, mustFetch(t, e, "pack.json")); body != "old-copy" {
		t.Fatalf("prefetch body = %q", body)
	}
	e.clk.Advance(601 * time.Second) // past the 600s metadata TTL

	e.state.mu.Lock()
	e.state.delay = 1600 * time.Millisecond // the winner stalls past the 1s cap
	e.state.mu.Unlock()
	winnerDone := make(chan struct{})
	go func() {
		defer close(winnerDone)
		res, err := e.eng.Fetch(context.Background(), "generic-remote", "pack.json")
		if err != nil {
			t.Errorf("winner fetch: %v", err)
			return
		}
		_ = res.Body.Close()
	}()
	time.Sleep(200 * time.Millisecond) // the winner is inside its stalled upstream call

	start := time.Now()
	res, err := e.eng.Fetch(context.Background(), "generic-remote", "pack.json")
	waited := time.Since(start)
	if err != nil {
		t.Fatalf("waitor after the per-repo cap: %v", err)
	}
	if body := readAll(t, res); body != "old-copy" {
		t.Fatalf("fallback body = %q, want the old copy", body)
	}
	if res.CacheState != CacheStale {
		t.Fatalf("fallback state = %q, want STALE", res.CacheState)
	}
	if waited > 1500*time.Millisecond {
		t.Fatalf("waitor blocked %v — the 1s per-repo cap did not apply (engine default 60s?)", waited)
	}
	<-winnerDone
	if got := e.hits.Load(); got != 2 { // prefetch + the winner's refresh
		t.Fatalf("upstream hits = %d, want 2 (the waitor must not contact)", got)
	}
}
