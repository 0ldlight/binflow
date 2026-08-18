package httpapi_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/httpapi"
)

// The T-41 suite: client-disconnect log demotion (PRD §6.4 O1 /
// NFR-OBS-1). A request whose >=500 outcome coincides with the client
// vanishing must be logged as a WARN line carrying client_disconnect=true
// instead of counting as a 5xx ERROR; a genuine 500 must stay ERROR.

// ---- helpers ----

// disconnectChain assembles the production chain head (requestID ->
// accessLog -> recover) around a terminal handler, capturing the log
// output (same composition as chainWithAccessLog in review_fixes_test.go;
// kept local so the level assertions read next to their scenarios).
func disconnectChain(t *testing.T, terminal http.Handler) (http.Handler, func() string) {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	logger := slog.New(slog.NewTextHandler(syncLineWriter{&lines, &mu}, nil))
	handler := httpapi.ChainHeadForTest(logger, terminal)
	return handler, func() string {
		mu.Lock()
		defer mu.Unlock()
		var b strings.Builder
		for _, l := range lines {
			b.WriteString(l)
			b.WriteByte('\n')
		}
		return b.String()
	}
}

// syncLineWriter appends whole log lines under a mutex.
type syncLineWriter struct {
	lines *[]string
	mu    *sync.Mutex
}

func (w syncLineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	*w.lines = append(*w.lines, strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// abortingUploadClient dials addr, sends a PUT with a large
// Content-Length but only a fraction of the body, waits for the handler
// to be mid-read, then aborts the connection in the caller's chosen
// style. It is the in-process equivalent of PRD O1's "--limit-rate +
// KILL curl" recipe (the real-curl variant lives at the bottom).
func abortingUploadClient(t *testing.T, addr, path string, abort func(*net.TCPConn)) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	tc, ok := conn.(*net.TCPConn)
	if !ok {
		t.Fatalf("connection is %T, want TCP", conn)
	}
	req := fmt.Sprintf("PUT %s HTTP/1.1\r\nHost: t\r\nAuthorization: Basic %s\r\nContent-Length: 1000000\r\n\r\npartial-bytes",
		path, base64Admin())
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write partial request: %v", err)
	}
	// Give the handler time to be inside the body read.
	time.Sleep(300 * time.Millisecond)
	abort(tc)
}

// abortFIN closes gracefully (FIN): the server's body read sees
// "unexpected EOF" and the request context cancels.
func abortFIN(*net.TCPConn) {}

// abortRST sets SO_LINGER 0 before Close: the kernel sends RST and the
// read fails with ECONNRESET.
func abortRST(tc *net.TCPConn) { _ = tc.SetLinger(0) }

// ---- AC1/AC3: the three states, table-driven ----

// TestAccessLogDisconnectTriState drives the production chain head with a
// 500-rendering terminal handler under the three context outcomes the
// classifier must distinguish (T-41 AC3):
//
//	canceled           -> WARN + client_disconnect=true (NOT ERROR)
//	deadline exceeded  -> ERROR (a server-side timeout is not a disconnect)
//	normal             -> ERROR (a real 500 must not be swallowed)
//
// The canceled state is produced by the REAL transport (an upload client
// that aborts mid-body, both FIN and RST), not by a forged context —
// the probe backing this classifier showed net/http itself cancels the
// request context in both cases. The deadline state needs the forged
// context because the loopback stack never times out on its own.
func TestAccessLogDisconnectTriState(t *testing.T) {
	// The deadline/normal rows need the handler chain driven directly.
	directRows := []struct {
		name       string
		ctxFactory func(context.Context) context.Context
		wantERROR  bool
	}{
		{
			name: "deadline exceeded is a server timeout, stays ERROR",
			ctxFactory: func(ctx context.Context) context.Context {
				expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
				cancel() // release the timer; Err() stays DeadlineExceeded
				return expired
			},
			wantERROR: true,
		},
		{
			name: "normal real 500 stays ERROR",
			ctxFactory: func(ctx context.Context) context.Context {
				return ctx
			},
			wantERROR: true,
		},
	}
	for _, row := range directRows {
		t.Run(row.name, func(t *testing.T) {
			terminal := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"errors":[{"status":500}]}`))
			})
			handler, logs := disconnectChain(t, terminal)

			req := httptest.NewRequest(http.MethodPut, "/binflow/r/x", nil)
			req = req.WithContext(row.ctxFactory(req.Context()))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			logged := logs()
			if !strings.Contains(logged, "status=500") {
				t.Fatalf("access log lost the 500; logs:\n%s", logged)
			}
			if !strings.Contains(logged, "level=ERROR") {
				t.Fatalf("real 500 was demoted (no ERROR line); logs:\n%s", logged)
			}
			if strings.Contains(logged, "client_disconnect=true") {
				t.Fatalf("non-disconnect request got the annotation; logs:\n%s", logged)
			}
		})
	}

	// The canceled row: real transport aborts, FIN and RST.
	for _, abort := range []struct {
		name   string
		style  func(*net.TCPConn)
		reason string
	}{
		{name: "canceled graceful FIN", style: abortFIN, reason: "context_canceled"},
		{name: "canceled RST (kill -9 analogue)", style: abortRST, reason: "context_canceled"},
	} {
		t.Run(abort.name, func(t *testing.T) {
			terminal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// The observed M1 upload-collapse shape: drain until the
				// abort surfaces as an error, then render the 500.
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"errors":[{"status":500}]}`))
			})
			handler, logs := disconnectChain(t, terminal)
			ts := httptest.NewUnstartedServer(handler)
			ts.Config.Handler = handler
			ts.Start()
			defer ts.Close()

			abortingUploadClient(t, ts.Listener.Addr().String(), "/binflow/r/slow.bin", abort.style)

			// Wait for the access line (the log write happens after the
			// handler returns, which trails the abort slightly).
			deadline := time.Now().Add(3 * time.Second)
			var logged string
			for time.Now().Before(deadline) {
				logged = logs()
				if strings.Contains(logged, "path=/binflow/r/slow.bin") {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}

			if !strings.Contains(logged, "path=/binflow/r/slow.bin") {
				t.Fatalf("no access line for the aborted upload; logs:\n%s", logged)
			}
			if !strings.Contains(logged, "client_disconnect=true") {
				t.Fatalf("missing client_disconnect annotation; logs:\n%s", logged)
			}
			if !strings.Contains(logged, "disconnect_reason="+abort.reason) {
				t.Fatalf("missing disconnect_reason=%s; logs:\n%s", abort.reason, logged)
			}
			if strings.Contains(logged, "level=ERROR") {
				t.Fatalf("aborted upload counted as 5xx ERROR; logs:\n%s", logged)
			}
			if !strings.Contains(logged, "level=WARN") {
				t.Fatalf("aborted upload not demoted to WARN; logs:\n%s", logged)
			}
		})
	}
}

// ---- AC2: a genuine 500 keeps its ERROR line ----

// TestReal500StaysERROR injects a handler returning a true 500 with the
// client alive and reading (the raw-TCP client below consumes the whole
// response): the access line must stay level=ERROR with no disconnect
// annotation (T-41 AC2 — the demotion must not swallow real faults).
func TestReal500StaysERROR(t *testing.T) {
	terminal := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":[{"status":500}]}`))
	})
	handler, logs := disconnectChain(t, terminal)
	ts := httptest.NewUnstartedServer(handler)
	ts.Config.Handler = handler
	ts.Start()
	defer ts.Close()

	conn, err := net.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = conn.Write([]byte("PUT /binflow/r/x HTTP/1.1\r\nHost: t\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", resp.StatusCode, body)
	}

	deadline := time.Now().Add(2 * time.Second)
	var logged string
	for time.Now().Before(deadline) {
		logged = logs()
		if strings.Contains(logged, "path=/binflow/r/x") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(logged, "level=ERROR") {
		t.Fatalf("real 500 has no ERROR access line; logs:\n%s", logged)
	}
	if strings.Contains(logged, "client_disconnect") {
		t.Fatalf("healthy request got a disconnect annotation; logs:\n%s", logged)
	}
}

// ---- AC1: recover demotes write-path disconnect panics ----

// TestRecoverWritePathDisconnectDemoted: a streaming handler that panics
// with a broken-pipe-class error (the write path discovering the vanished
// reader) recovers as WARN + client_disconnect, injects NO envelope (the
// bytes are undeliverable anyway) and the access log line for the same
// request is demoted (T-41 AC1's recover clause).
func TestRecoverWritePathDisconnectDemoted(t *testing.T) {
	terminal := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PARTIAL-ARTIFACT-BYTES"))
		panic(fmt.Errorf("write stream: %w", syscall.EPIPE))
	})
	handler, logs := disconnectChain(t, terminal)

	req := httptest.NewRequest(http.MethodGet, "/binflow/r/stream.bin", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	logged := logs()
	if !strings.Contains(logged, "panic recovered") {
		t.Fatalf("no recover line; logs:\n%s", logged)
	}
	if !strings.Contains(logged, "client_disconnect=true") {
		t.Fatalf("recover line missing the disconnect annotation; logs:\n%s", logged)
	}
	if !strings.Contains(logged, "disconnect_reason=broken_pipe") {
		t.Fatalf("want disconnect_reason=broken_pipe; logs:\n%s", logged)
	}
	if strings.Contains(logged, "panic recovered") && strings.Contains(logged, "level=ERROR") {
		t.Fatalf("disconnect panic logged as ERROR; logs:\n%s", logged)
	}
	if got := rec.Body.String(); got != "PARTIAL-ARTIFACT-BYTES" {
		t.Fatalf("body = %q, want the partial bytes only (no envelope)", got)
	}
	// The access line for the same request is demoted too.
	if !strings.Contains(logged, "client_disconnect=true") || !strings.Contains(logged, "level=WARN") {
		t.Fatalf("access line not demoted; logs:\n%s", logged)
	}
	if strings.Contains(logged, "level=ERROR") {
		t.Fatalf("a line is ERROR despite the disconnect; logs:\n%s", logged)
	}
}

// TestRecoverOrdinaryPanicStillError: the demotion is scoped to
// disconnect-shaped panics — an ordinary panic keeps its ERROR line and
// the envelope 500.
func TestRecoverOrdinaryPanicStillError(t *testing.T) {
	terminal := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })
	handler, logs := disconnectChain(t, terminal)

	req := httptest.NewRequest(http.MethodGet, "/binflow/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	logged := logs()
	if !strings.Contains(logged, "level=ERROR") {
		t.Fatalf("ordinary panic lost its ERROR line; logs:\n%s", logged)
	}
	if strings.Contains(logged, "client_disconnect=true") {
		t.Fatalf("ordinary panic got the disconnect annotation; logs:\n%s", logged)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// ---- AC1: the demotion is middleware-layer, both planes benefit ----

// TestDisconnectDemotionGenericAndV2Planes runs the same aborted upload
// against the FULL assembled stack (real storage, real metadata, real
// adapters) on both upload domains — a generic content-path PUT and a
// /v2 docker-plane request — asserting the identical WARN annotation
// with no ERROR line anywhere (middleware-layer uniformity, T-41 AC1's
// "generic 与 docker 上传域同一生效").
func TestDisconnectDemotionGenericAndV2Planes(t *testing.T) {
	t.Run("generic upload domain", func(t *testing.T) {
		h := newHarness(t)
		seedRepo(t, h, "generic-local")
		h.resetLogs()
		abortingUploadClient(t, h.srv.Listener.Addr().String(),
			"/binflow/generic-local/acme/slow.bin", abortRST)

		logged := waitForLog(t, h, "/binflow/generic-local/acme/slow.bin")
		if strings.Contains(logged, "level=ERROR") {
			t.Fatalf("aborted generic upload produced an ERROR line; logs:\n%s", logged)
		}
		if !strings.Contains(logged, "client_disconnect=true") {
			t.Fatalf("missing client_disconnect annotation; logs:\n%s", logged)
		}
		// The QA criterion verbatim: no 5xx-level ERROR after the abort.
		for _, line := range strings.Split(logged, "\n") {
			if strings.Contains(line, "level=ERROR") {
				t.Fatalf("ERROR line present: %s", line)
			}
		}
	})

	t.Run("docker v2 plane", func(t *testing.T) {
		h := newHarness(t)
		h.resetLogs()
		// A /v2 name route under the mounted docker adapter. The blob
		// upload endpoints land in T-38; what this exercises is the part
		// T-41 owns: the SAME middleware chain demotes the aborted
		// request on the /v2 plane exactly like the generic one. The
		// terminal handler is the current 404-rendering name route, so a
		// slow-draining variant is driven through the chain head at the
		// /v2 path with a 500 — the uniformity under test is the
		// middleware's, not the route's.
		terminal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errors":[{"code":"UNKNOWN"}]}`))
		})
		handler, logs := disconnectChain(t, terminal)
		ts := httptest.NewUnstartedServer(handler)
		ts.Config.Handler = handler
		ts.Start()
		defer ts.Close()
		abortingUploadClient(t, ts.Listener.Addr().String(), "/v2/team1/app/blobs/uploads/seed", abortRST)

		logged := logs()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			logged = logs()
			if strings.Contains(logged, "/v2/team1/app/blobs/uploads/seed") {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !strings.Contains(logged, "/v2/team1/app/blobs/uploads/seed") {
			t.Fatalf("no access line for the aborted /v2 request; logs:\n%s", logged)
		}
		if strings.Contains(logged, "level=ERROR") {
			t.Fatalf("aborted /v2 request produced an ERROR line; logs:\n%s", logged)
		}
		if !strings.Contains(logged, "client_disconnect=true") {
			t.Fatalf("missing client_disconnect annotation; logs:\n%s", logged)
		}
	})
}

// waitForLog polls the harness log store until a line mentioning needle
// shows up (access lines trail the transport abort slightly).
func waitForLog(t *testing.T, h *harness, needle string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if logged := h.logs(); strings.Contains(logged, needle) {
			return logged
		}
		time.Sleep(50 * time.Millisecond)
	}
	return h.logs()
}

// ---- AC3: the PRD's real-client recipe ----

// TestSlowUploadKilledCurlIsWarn reproduces the PRD O1 acceptance shape
// with the real client: curl uploads with --limit-rate, gets KILLed
// mid-body, and the server log must contain the request as a
// client_disconnect WARN — no 5xx ERROR line.
func TestSlowUploadKilledCurlIsWarn(t *testing.T) {
	curlPath, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl not available on PATH; real-client disconnect case skipped")
	}

	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	h.resetLogs()

	// 4MB from a file, capped at 64k/s: plenty of window to kill.
	payload := t.TempDir() + "/payload.bin"
	dd := exec.Command("/bin/dd", "if=/dev/zero", "of="+payload, "bs=1048576", "count=4") //nolint:gosec // test fixture
	if out, err := dd.CombinedOutput(); err != nil {
		t.Skipf("dd unavailable: %v (%s)", err, out)
	}

	cmd := exec.Command(curlPath, "-sS", "--limit-rate", "64k",
		"-u", adminUser+":"+adminPass, "-T", payload,
		h.srv.URL+"/binflow/generic-local/acme/killed.bin")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start curl: %v", err)
	}
	// Let it stream a couple of chunks, then KILL (PRD: "KILL curl").
	time.Sleep(600 * time.Millisecond)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill curl: %v", err)
	}
	_ = cmd.Wait()

	logged := waitForLog(t, h, "acme/killed.bin")
	if !strings.Contains(logged, "acme/killed.bin") {
		t.Fatalf("no access line for the killed upload; logs:\n%s", logged)
	}
	if strings.Contains(logged, "level=ERROR") {
		t.Fatalf("killed upload produced a 5xx ERROR line; logs:\n%s", logged)
	}
	if !strings.Contains(logged, "client_disconnect=true") {
		t.Fatalf("missing client_disconnect annotation; logs:\n%s", logged)
	}

	// The aborted upload leaves no residue (M1 behavior preserved under
	// the new demotion): the path 404s and no ERROR was logged.
	resp := h.do(http.MethodGet, "/binflow/generic-local/acme/killed.bin", "", "", nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("aborted upload path status = %d, want 404", resp.StatusCode)
	}
}
