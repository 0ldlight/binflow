package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
)

// TestRunShutdownLifecycle: Run serves until the context is canceled, then
// drains within the graceful timeout and returns nil (exit code 0 is
// cmd's translation of that, architecture section 7.4). Shutdown stays
// idempotent.
func TestRunShutdownLifecycle(t *testing.T) {
	h := newHarness(t)

	s := httpapi.New(httpapi.Deps{
		Config:   testListenConfig(),
		Auth:     h.authSvc,
		Authz:    h.authSvc,
		Metadata: h.md,
		Repos:    h.md.Repos(),
		DataDir:  "/tmp/unused-for-run-test",
		Console:  http.NotFoundHandler(),
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// Give the listener a moment, then cancel and require a clean return.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil after cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}

	// A second Shutdown call must be safe (idempotent drain).
	if err := s.Shutdown(time.Second); err != nil {
		t.Fatalf("second shutdown: %v", err)
	}
}

// testListenConfig is a Run-config on an ephemeral port.
func testListenConfig() *config.Config {
	cfg := config.Defaults()
	cfg.Server.Listen = "127.0.0.1:0"
	cfg.Server.GracefulTimeout = 2 * time.Second
	return cfg
}
