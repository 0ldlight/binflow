package main

// T-279 cmd-side wiring: openStack must build and LOAD the license manager
// (embedded verify keys, 012 store, community floor), the Deps seam must
// serve it on /api/system/license, and a DB row that cannot verify must
// degrade the boot to community with a WARN — never refuse the start
// (NFR-S53 / FR-84-AC6). The degraded-boot leg plants the row directly
// through the store: the production verify key has no matching signer by
// design (internal/license/verifykey.go), so REST Install is not a path
// here. Like the metrics/auth wiring tests, this file assembles LIGHT Deps
// shapes — newAssembledServer has exactly one full-assembly caller per
// process (the adapter registry contract, T-168).

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// captureLogger is testSlogLogger's capturing twin (s3_stack_test owns the
// discarding spelling; the fail-safe leg needs the WARN line).
type syncWriter struct {
	mu sync.Mutex
	b  strings.Builder
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

func captureLogger(t *testing.T) (*slog.Logger, func() string) {
	t.Helper()
	w := &syncWriter{}
	return slog.New(slog.NewTextHandler(w, nil)), w.String
}

// licenseTestServer builds the HTTP surface over a real openStack result
// with the license seam wired the way newAssembledServer does.
func licenseTestServer(t *testing.T, cfg *config.Config, st *stack) *httptest.Server {
	t.Helper()
	s := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Auth:     st.authSvc,
		Authz:    st.authSvc,
		Metadata: st.md,
		Repos:    st.md.Repos(),
		DataDir:  cfg.Storage.DataDir,
		License:  st.licenseMgr,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestT279LicenseManagerWiredAndLoaded(t *testing.T) {
	cfg := configDefaults()
	cfg.Storage.DataDir = t.TempDir()
	logger, _ := captureLogger(t)

	st, err := openStack(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	defer st.close(logger)

	if st.licenseMgr == nil {
		t.Fatal("stack.licenseMgr is nil, want the manager openStack builds")
	}
	if state := st.licenseMgr.State(); state.Licensed || state.Tier.String() != "community" {
		t.Fatalf("fresh stack not on the community floor: %+v", state)
	}

	ts := licenseTestServer(t, cfg, st)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/binflow/api/system/license", nil)
	req.SetBasicAuth("admin", "password")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET license: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET license = %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"community"`) || !strings.Contains(string(body), `"licensed": false`) {
		t.Fatalf("assembled GET body wrong: %s", body)
	}

	// readyz stays 200: the license plane never gates process health.
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/readyz", nil)
	resp2, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET readyz: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("readyz = %d, want 200", resp2.StatusCode)
	}
}

// TestT279RestartUnverifiableRowFailsafe: a row that cannot verify under
// the embedded key — an offline-written or externally mutated doc — must
// degrade the SECOND boot to community with a WARN and a license.invalid
// audit row, while the boot itself succeeds and keeps the row as evidence.
func TestT279RestartUnverifiableRowFailsafe(t *testing.T) {
	cfg := configDefaults()
	cfg.Storage.DataDir = t.TempDir()

	logger1, _ := captureLogger(t)
	st1, err := openStack(context.Background(), cfg, logger1)
	if err != nil {
		t.Fatalf("first openStack: %v", err)
	}

	// External tamper shape: a row lands in the 012 table outside Install.
	now := metadata.Now()
	if err := st1.md.Licenses().PutLicense(context.Background(), &metadata.LicenseRecord{
		LicenseID: "tampered-1", Tier: "enterprise", Licensee: "Evil Corp",
		Doc:      "ZmFrZWQ.ZmFrZWQ",
		IssuedAt: now, NotBefore: now, ExpiresAt: now, InstalledAt: now,
	}); err != nil {
		t.Fatalf("planting unverifiable row: %v", err)
	}
	st1.close(logger1)

	logger2, logs2 := captureLogger(t)
	st2, err := openStack(context.Background(), cfg, logger2)
	if err != nil {
		t.Fatalf("second openStack must boot despite the bad row: %v", err)
	}
	defer st2.close(logger2)

	state := st2.licenseMgr.State()
	if state.Licensed || state.Tier.String() != "community" {
		t.Fatalf("unverifiable row kept a tier: %+v", state)
	}
	if logs := logs2(); !strings.Contains(logs, "failed verification") || !strings.Contains(logs, "degrading") {
		t.Fatalf("fail-safe WARN missing from the boot log: %s", logs)
	}
	// The audit trail carries the system-actor invalid event.
	events, err := st2.md.Audits().Query(context.Background(), metadata.AuditQuery{Action: "license.invalid"})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("license.invalid audit row missing after the degraded boot")
	}
	if events[0].Actor != "system" {
		t.Fatalf("degraded-boot invalid actor = %q, want system", events[0].Actor)
	}

	// The row itself survives as the operator's evidence.
	rec, err := st2.md.Licenses().GetLicense(context.Background())
	if err != nil || rec == nil {
		t.Fatalf("unverifiable row was deleted: (%v, %v)", rec, err)
	}
}
