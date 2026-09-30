package remote

// T-619 / BIN-101: the assumed-offline COLD BREAKER opens at the SECOND
// consecutive configured-upstream transport fault (contract
// docs/compatibility/contracts/remote-fetch.yaml#remote/offline-window-open-
// threshold; live A 7.161.26 T-597 r3/r4 pairs: contacts 1 and 2 answer the
// retrieval-error 404, the window silences contact 3 onward — the as-built
// first-fault window was ruled the BUG). These tests pin the count semantics
// the contract leaves to the minimal reading: "consecutive" means any
// definitive upstream answer restarts the count, and the window's own expiry
// restarts the cold sequence. The 5xx arm stays OUTSIDE the counter (first
// 5xx still opens the window immediately — live-unprobed on A, unchanged;
// pinned by TestFetchUpstreamFaultMatrix in fetcher_test.go).

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// pointUpstreamAt rewrites the repository's configured upstream URL — both
// the repo row's config JSON and the remote_configs column — keeping the
// same repository key so the breaker state under test survives the swap.
func (e *fetchEnv) pointUpstreamAt(t *testing.T, url string) {
	t.Helper()
	ctx := context.Background()
	cfg, err := e.md.Remote().GetConfig(ctx, "generic-remote")
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	row, err := e.md.Repos().Get(ctx, "generic-remote")
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	row.Config = strings.Replace(row.Config, cfg.URL, url, 1)
	if err := e.md.Repos().Update(ctx, row); err != nil {
		t.Fatalf("update repo: %v", err)
	}
	cfg.URL = url
	if err := e.md.Remote().UpdateConfig(ctx, cfg); err != nil {
		t.Fatalf("update config: %v", err)
	}
}

// wantRetrieval asserts the first-contact fault family: the 404 body cites
// the requested path and the hop's full upstream URL (the cause text is the
// local runtime's own wording — family match, not byte match).
func wantRetrieval(t *testing.T, fe *FetchError, repoKey, path, upURL string) {
	t.Helper()
	if fe.Status != http.StatusNotFound {
		t.Fatalf("%s: status = %d, want 404 (retrieval form)", path, fe.Status)
	}
	for _, want := range []string{
		"Error in getting information for '" + path + "'",
		"Failed retrieving resource from " + upURL + ":",
		"Path: '" + repoKey + ":" + path + "'",
	} {
		if !strings.Contains(fe.Message, want) {
			t.Fatalf("%s: retrieval form misses %q: %s", path, want, fe.Message)
		}
	}
}

// wantOfflineWindow asserts the in-window family: the 404 body names the
// assumed-offline state and never re-externalizes the retrieval error.
func wantOfflineWindow(t *testing.T, fe *FetchError, repoKey, path string) {
	t.Helper()
	if fe.Status != http.StatusNotFound {
		t.Fatalf("%s: status = %d, want 404 (offline form)", path, fe.Status)
	}
	if want := "is assumed offline, '" + repoKey + ":" + path + "' is not found at '" + path + "'"; !strings.Contains(fe.Message, want) {
		t.Fatalf("%s: offline form misses %q: %s", path, want, fe.Message)
	}
	if strings.Contains(fe.Message, "Failed retrieving resource from") {
		t.Fatalf("%s: in-window message re-externalizes the retrieval error: %s", path, fe.Message)
	}
}

// TestOfflineBreakerOpensAfterSecondTransportFault is the contract's cold
// contact sequence: fault 1 -> retrieval form, fault 2 (the boundary leg
// that OPENS the window) -> STILL the retrieval form, fault 3 -> offline
// form, and a no-suffix control contact shares the same window. Sidecar
// terminal suffixes ride the same breaker at engine level (the adapters
// resolve the suffix to its source before calling in — T-597); the HEAD
// wire face is the same engine path (verb-agnostic Fetch) and is covered by
// the live differential's curl -I legs.
func TestOfflineBreakerOpensAfterSecondTransportFault(t *testing.T) {
	e := newFetchEnv(t, nil)
	ctx := context.Background()
	e.pointUpstreamAt(t, "http://127.0.0.1:1")

	// Fault 1: retrieval form, window not open yet.
	res, err := e.eng.Fetch(ctx, "generic-remote", "t619/a.bin.sha1")
	wantRetrieval(t, fetchErr(t, res, err), "generic-remote", "t619/a.bin.sha1", "http://127.0.0.1:1/t619/a.bin.sha1")

	// Fault 2 — the boundary leg (live A g-side-md5-inwin): the contact
	// that opens the window answers the retrieval error itself.
	res, err = e.eng.Fetch(ctx, "generic-remote", "t619/a.bin.md5")
	wantRetrieval(t, fetchErr(t, res, err), "generic-remote", "t619/a.bin.md5", "http://127.0.0.1:1/t619/a.bin.md5")

	// Fault 3: inside the window now — offline form, zero upstream traffic.
	res, err = e.eng.Fetch(ctx, "generic-remote", "t619/a.bin.sha256")
	wantOfflineWindow(t, fetchErr(t, res, err), "generic-remote", "t619/a.bin.sha256")

	// Fault 4 (contact 4, contract leg): same window, same form.
	res, err = e.eng.Fetch(ctx, "generic-remote", "t619/b.bin")
	wantOfflineWindow(t, fetchErr(t, res, err), "generic-remote", "t619/b.bin")

	// No-suffix control (g-ctrl-inwin): the bare path shares the window.
	res, err = e.eng.Fetch(ctx, "generic-remote", "t619/a.bin")
	wantOfflineWindow(t, fetchErr(t, res, err), "generic-remote", "t619/a.bin")
}

// TestOfflineBreakerSuccessResetsFaultCount pins the "consecutive" reading:
// a definitive upstream answer between two faults restarts the count, so a
// single fault after a success never leaves a half-charged breaker.
func TestOfflineBreakerSuccessResetsFaultCount(t *testing.T) {
	e := newFetchEnv(t, nil)
	ctx := context.Background()
	e.state.files["/t619/ok.bin"] = "ok-body"

	// Fault 1 (dead upstream): retrieval form, count = 1.
	e.pointUpstreamAt(t, "http://127.0.0.1:1")
	res, err := e.eng.Fetch(ctx, "generic-remote", "t619/x.bin")
	wantRetrieval(t, fetchErr(t, res, err), "generic-remote", "t619/x.bin", "http://127.0.0.1:1/t619/x.bin")

	// A successful contact (upstream back, 200 lands): count restarts.
	e.pointUpstreamAt(t, e.srv.URL)
	fetched := mustFetch(t, e, "t619/ok.bin")
	if body := readAll(t, fetched); body != "ok-body" {
		t.Fatalf("recovered body = %q", body)
	}

	// Fault again: count = 1 (NOT 2) — the discriminator is the NEXT fault,
	// which must still answer the retrieval form (without the reset it
	// would open the window here and answer the following contact with the
	// offline form).
	e.pointUpstreamAt(t, "http://127.0.0.1:1")
	res, err = e.eng.Fetch(ctx, "generic-remote", "t619/y.bin")
	wantRetrieval(t, fetchErr(t, res, err), "generic-remote", "t619/y.bin", "http://127.0.0.1:1/t619/y.bin")

	// Count = 2 now: this contact OPENS the window and still externalizes
	// the retrieval error.
	res, err = e.eng.Fetch(ctx, "generic-remote", "t619/z.bin")
	wantRetrieval(t, fetchErr(t, res, err), "generic-remote", "t619/z.bin", "http://127.0.0.1:1/t619/z.bin")

	// Count irrelevant inside the window: offline form.
	res, err = e.eng.Fetch(ctx, "generic-remote", "t619/w.bin")
	wantOfflineWindow(t, fetchErr(t, res, err), "generic-remote", "t619/w.bin")
}

// TestOfflineBreakerExpiryRestartsColdSequence pins the r3/r4 anchor: once
// the 300s window expires, the sequence restarts from the cold state —
// retrieval, retrieval (window re-opens), offline.
func TestOfflineBreakerExpiryRestartsColdSequence(t *testing.T) {
	e := newFetchEnv(t, nil)
	ctx := context.Background()
	e.pointUpstreamAt(t, "http://127.0.0.1:1")

	res, err := e.eng.Fetch(ctx, "generic-remote", "r/a.bin")
	wantRetrieval(t, fetchErr(t, res, err), "generic-remote", "r/a.bin", "http://127.0.0.1:1/r/a.bin")
	res, err = e.eng.Fetch(ctx, "generic-remote", "r/b.bin")
	wantRetrieval(t, fetchErr(t, res, err), "generic-remote", "r/b.bin", "http://127.0.0.1:1/r/b.bin")
	res, err = e.eng.Fetch(ctx, "generic-remote", "r/c.bin")
	wantOfflineWindow(t, fetchErr(t, res, err), "generic-remote", "r/c.bin")

	// Past assumedOfflinePeriodSecs (300 in the fixture config): the lazy
	// expiry drops the window AND the fault count — round 2 starts cold.
	e.clk.Advance(301 * time.Second)

	res, err = e.eng.Fetch(ctx, "generic-remote", "r/d.bin")
	wantRetrieval(t, fetchErr(t, res, err), "generic-remote", "r/d.bin", "http://127.0.0.1:1/r/d.bin")
	res, err = e.eng.Fetch(ctx, "generic-remote", "r/e.bin")
	wantRetrieval(t, fetchErr(t, res, err), "generic-remote", "r/e.bin", "http://127.0.0.1:1/r/e.bin")
	res, err = e.eng.Fetch(ctx, "generic-remote", "r/f.bin")
	wantOfflineWindow(t, fetchErr(t, res, err), "generic-remote", "r/f.bin")
}
