// T-192 acceptance surface, HTTP layer: T-172 defect D-1's repro script
// (burst of Basic-authenticated requests, most clients disconnecting
// mid-flight) translated into a regression test against the real router
// and middleware chain. The argon2 gate is injected at limit 1 so the
// queue is deterministically saturated by a small storm; the assertion is
// that the anonymous plane (ping) keeps answering while the storm drains,
// that abandoned requests never turn into credential rejections, and that
// the T-147 P50x20 wave shape (20 waves x 50 concurrent authenticated
// requests, 1000 total, zero failures) still passes on the default gate.
//
// Storm users carry a real argon2id hash at m=16 MiB (parameters live in
// the PHC string, so verification honestly re-derives at them) to keep
// the suite fast; the memory-recovery proof at the full 64 MiB parameters
// lives in internal/auth (TestT192HashStormHeapRecoversNoLeak).

package httpapi_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"

	"golang.org/x/crypto/argon2"
)

const (
	t192User      = "t192-stormer"
	t192PW        = "t192-pw" //nolint:gosec // throwaway test fixture password
	t192HashKiB   = 16 * 1024 // m=16 MiB: real argon2id, storm-sized
	t192PingPath  = "/binflow/api/system/ping"
	t192StormN    = 80
	t192ProbeN    = 100
	t192WaveWaves = 20
	t192WaveConc  = 50
)

// t192Hash derives a PHC string at the reduced storm parameters.
func t192Hash(t *testing.T, password string) string {
	t.Helper()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatalf("salt: %v", err)
	}
	tag := argon2.IDKey([]byte(password), salt, 1, t192HashKiB, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		t192HashKiB, 1, 1,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(tag))
}

// seedT192User creates an admin local user with the storm-strength hash
// (admin keeps the ping path free of ACL setup; every route still runs
// the authenticator, which is the plane under test).
func seedT192User(t *testing.T, h *harness) {
	t.Helper()
	now := metadata.Now()
	err := h.md.Users().Create(context.Background(), &metadata.User{
		Username: t192User, PasswordHash: t192Hash(t, t192PW),
		IsAdmin: true, Enabled: true, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("seed storm user: %v", err)
	}
}

// t192Client is a keep-alive client sized for the wave concurrency. One
// instance per test (NOT per request): a Transport spawns per-connection
// goroutines that idle for 90s, so per-request clients would turn the
// goroutine-leak assertion into a measurement of client litter.
func t192Client() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        256,
			MaxIdleConnsPerHost: 256,
		},
	}
}

func t192AuthedGet(ctx context.Context, c *http.Client, url string) (*http.Response, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Authorization",
		"Basic "+base64.StdEncoding.EncodeToString([]byte(t192User+":"+t192PW)))
	return c.Do(r)
}

func drainAndClose(t *testing.T, resp *http.Response) {
	t.Helper()
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// D-1 repro at the HTTP layer, gate injected at 1: the Basic storm queues
// behind the argon2 gate, half the clients disconnect after 2ms (their
// queue slots are abandoned — those requests are transport casualties,
// never credential rejections), and the anonymous ping plane answers
// throughout with bounded latency.
func TestT192AuthStormAnonymousPingNotStarved(t *testing.T) {
	h := newHarnessAuth(t, nil, func(s *auth.Service) *auth.Service {
		return s.WithHashConcurrency(1)
	}, nil, nil)
	seedT192User(t, h)
	client := t192Client()
	defer client.CloseIdleConnections()

	// Warm one authenticated round trip so sqlite/page caches are hot and
	// the storm measures queueing, not first-touch costs.
	warm, err := t192AuthedGet(context.Background(), client, h.srv.URL+t192PingPath)
	if err != nil || warm.StatusCode != http.StatusOK {
		t.Fatalf("warm authed ping: %v status=%d", err, warm.StatusCode)
	}
	drainAndClose(t, warm)

	goroutineBaseline := runtime.NumGoroutine()

	stormDone := make(chan error)
	go func() {
		defer close(stormDone)
		var (
			wg      sync.WaitGroup
			liveOK  atomic.Int32
			abandon atomic.Int32
			other   atomic.Int32
		)
		for i := 0; i < t192StormN; i++ {
			wg.Add(1)
			go func(slot int) {
				defer wg.Done()
				ctx := context.Background()
				if slot%2 == 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, 2*time.Millisecond)
					defer cancel()
				}
				resp, err := t192AuthedGet(ctx, client, h.srv.URL+t192PingPath)
				switch {
				case err != nil && ctx.Err() != nil:
					abandon.Add(1) // client hung up mid-queue: a transport casualty
				case err != nil:
					other.Add(1)
					t.Errorf("live storm request failed: %v", err)
				case resp.StatusCode != http.StatusOK:
					other.Add(1)
					t.Errorf("live storm request status=%d (a disconnected waiter must never surface as a 401 credential rejection)", resp.StatusCode)
					drainAndClose(t, resp)
				default:
					liveOK.Add(1)
					drainAndClose(t, resp)
				}
			}(i)
		}
		wg.Wait()
		if other.Load() > 0 {
			stormDone <- fmt.Errorf("%d unexpected storm failures", other.Load())
		}
		t.Logf("HTTP storm (gate=1, n=%d): %d completed 200, %d abandoned by client disconnect",
			t192StormN, liveOK.Load(), abandon.Load())
	}()

	// The anonymous prober: sequential pings with NO credential, measuring
	// each latency while the storm drains.
	latencies := make([]time.Duration, 0, t192ProbeN)
	for i := 0; i < t192ProbeN; i++ {
		start := time.Now()
		resp, err := client.Get(h.srv.URL + t192PingPath)
		if err != nil {
			t.Fatalf("anonymous ping #%d under storm: %v", i, err)
		}
		drainAndClose(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("anonymous ping #%d status=%d (anonymous plane starved)", i, resp.StatusCode)
		}
		latencies = append(latencies, time.Since(start))
	}
	if err := <-stormDone; err != nil {
		t.Fatal(err)
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50, p99 := percentileDurations(latencies, 50), percentileDurations(latencies, 99)
	t.Logf("anonymous ping under HTTP storm: p50=%s p99=%s max=%s (n=%d)",
		p50, p99, latencies[len(latencies)-1], len(latencies))
	// Pre-fix the D-1 repro starved pings for 10+ minutes; these bounds
	// are CI-generous while still proving the anonymous plane never queues
	// behind the argon2 gate.
	if p99 > time.Second {
		t.Fatalf("anonymous ping p99 = %s under storm, want < 1s", p99)
	}
	if p50 > 250*time.Millisecond {
		t.Fatalf("anonymous ping p50 = %s under storm, want < 250ms", p50)
	}

	// No leaked request goroutines once the storm drained: closing the
	// client's idle connections tears down both sides' per-connection
	// goroutines, so what remains counts server work only.
	client.CloseIdleConnections()
	time.Sleep(200 * time.Millisecond)
	if got := runtime.NumGoroutine(); got > goroutineBaseline+10 {
		t.Fatalf("goroutines after storm = %d, baseline %d (leaked waiters)", got, goroutineBaseline)
	}
}

// percentileDurations returns the p-th percentile of an ascending-sorted
// slice (shared with the auth-layer T-192 tests in spirit; kept local
// because the two packages' helpers must not couple).
func percentileDurations(sorted []time.Duration, p float64) time.Duration {
	idx := int(p/100*float64(len(sorted)-1) + 0.5)
	return sorted[idx]
}

// AC 4 — T-147's G27 shape on the default gate: 20 waves of 50 concurrent
// authenticated requests (P50x20 = 1000 total), zero failures, zero 5xx.
// The throughput NUMBER of the perf run is QA's to re-measure on quiet
// hardware; what must not regress here is the shape's correctness under
// the gate.
func TestT192P50x20WavesAuthenticatedZeroFailure(t *testing.T) {
	h := newHarness(t)
	seedT192User(t, h)
	client := t192Client()
	defer client.CloseIdleConnections()

	var (
		total    atomic.Int32
		okCount  atomic.Int32
		badCount atomic.Int32
	)
	start := time.Now()
	for wave := 0; wave < t192WaveWaves; wave++ {
		var wg sync.WaitGroup
		for i := 0; i < t192WaveConc; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := t192AuthedGet(context.Background(), client, h.srv.URL+t192PingPath)
				total.Add(1)
				if err != nil {
					badCount.Add(1)
					t.Errorf("wave request failed: %v", err)
					return
				}
				drainAndClose(t, resp)
				if resp.StatusCode != http.StatusOK {
					badCount.Add(1)
					t.Errorf("wave request status=%d", resp.StatusCode)
					return
				}
				okCount.Add(1)
			}()
		}
		wg.Wait()
	}
	elapsed := time.Since(start)
	t.Logf("P50x20 waves: %d requests in %s (%.0f req/s), ok=%d bad=%d",
		total.Load(), elapsed, float64(total.Load())/elapsed.Seconds(), okCount.Load(), badCount.Load())
	if got := total.Load(); got != t192WaveWaves*t192WaveConc {
		t.Fatalf("total requests = %d, want %d", got, t192WaveWaves*t192WaveConc)
	}
	if badCount.Load() != 0 {
		t.Fatalf("%d failed requests (T-147 P50x20 baseline demands zero)", badCount.Load())
	}
	if okCount.Load() != t192WaveWaves*t192WaveConc {
		t.Fatalf("ok requests = %d, want %d", okCount.Load(), t192WaveWaves*t192WaveConc)
	}
}
