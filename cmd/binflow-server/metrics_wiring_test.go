// T-163 cmd arm: the /metrics wiring through the real openStack chain —
// Deps.Metrics constructed at assembly (newAssembledServer's registry
// injection), the endpoint mounted root-level, and the require_auth gate
// both ways through the assembled surface.
//
// The full newAssembledServer cannot run twice in one process (the
// process-wide adapter registry panics on the second npm/maven
// registration — T-168's known full-suite red), so this file assembles the
// light Deps shape auth_wiring_test.go established, plus the two T-163
// entries: Metrics and (for the replication family's presence rule)
// Replication.

package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
	"github.com/lzwzzy/binflow/internal/replication"
)

// metricsTestServer builds the HTTP surface over a real openStack result
// with the T-163 Deps entries wired the way newAssembledServer does.
func metricsTestServer(t *testing.T, cfg *config.Config, st *stack, reg *metrics.Registry) *httptest.Server {
	t.Helper()
	s := httpapi.New(httpapi.Deps{
		Config:      cfg,
		Auth:        st.authSvc,
		Authz:       st.authSvc,
		Metadata:    st.md,
		Repos:       st.md.Repos(),
		DataDir:     cfg.Storage.DataDir,
		Metrics:     reg,
		Replication: st.replStore,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// TestMetricsCmdWiringAnonymous: the default config serves /metrics
// anonymously with the four families declared, and the stack's real
// replication store (openStack always opens one, T-180) exposes the task
// gauge family at zero.
func TestMetricsCmdWiringAnonymous(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	st := testStack(t, cfg)

	ts := metricsTestServer(t, cfg, st, metrics.NewRegistry())

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Fatalf("Content-Type = %q, want text/plain; version=0.0.4...", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	for _, want := range []string{
		"# TYPE binflow_http_requests_total counter",
		"# TYPE binflow_http_request_duration_seconds histogram",
		"# HELP binflow_storage_blobs_total ",
		`binflow_auth_logins_total{source="local"} 0`,
		"# TYPE binflow_replication_tasks gauge",
		`binflow_replication_tasks{status="pending"} 0`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("body missing %q", want)
		}
	}
}

// TestMetricsCmdWiringRequireAuth: metrics.require_auth=true gates the
// endpoint — anonymous 401, admin Basic 200.
func TestMetricsCmdWiringRequireAuth(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Metrics.RequireAuth = true
	st := testStack(t, cfg)

	ts := metricsTestServer(t, cfg, st, metrics.NewRegistry())

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /metrics = %d, want 401", resp.StatusCode)
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/metrics", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.SetBasicAuth("admin", "password") // the ADR-0009 evaluation default (testStack boots without the env)
	authed, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated GET /metrics: %v", err)
	}
	defer func() { _ = authed.Body.Close() }()
	if authed.StatusCode != http.StatusOK {
		t.Fatalf("authenticated GET /metrics = %d, want 200", authed.StatusCode)
	}
}

// TestMetricsCmdReplicationSnapshotCounts: the snapshot sums real ledger
// rows — two configs created through the store, one task each, land as
// pending 2.
func TestMetricsCmdReplicationSnapshotCounts(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	st := testStack(t, cfg)

	ctx := context.Background()
	if err := st.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "libs-release", Type: "local", PackageType: "generic",
		CreatedAt: "2026-08-22T00:00:00Z", UpdatedAt: "2026-08-22T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	for _, name := range []string{"push-a", "push-b"} {
		id, err := st.replStore.CreateConfig(ctx, &replication.ReplicationConfig{
			Name: name, SourceRepo: "libs-release", TargetURL: "http://target.example.com/binflow",
			TargetRepo: "libs-release", Enabled: true,
			CreatedAt: "2026-08-22T00:00:00Z", UpdatedAt: "2026-08-22T00:00:00Z",
		})
		if err != nil {
			t.Fatalf("create replication config %s: %v", name, err)
		}
		// Empty Status defaults to pending at enqueue time (Store contract).
		if _, err := st.replStore.CreateTask(ctx, &replication.ReplicationTask{
			ReplicationID: id, BlobSHA256: strings.Repeat("ab", 32), NodePath: "a/b.jar",
			CreatedAt: "2026-08-22T00:00:00Z",
		}); err != nil {
			t.Fatalf("create task for %s: %v", name, err)
		}
	}

	ts := metricsTestServer(t, cfg, st, metrics.NewRegistry())
	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if want := `binflow_replication_tasks{status="pending"} 2`; !strings.Contains(string(body), want) {
		t.Errorf("body missing %q (snapshot must sum real ledger rows)\nbody:\n%s", want, body)
	}
}
