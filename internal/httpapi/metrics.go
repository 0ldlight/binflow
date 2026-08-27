// Prometheus instrumentation for the HTTP surface (T-163, ADR-0022 /
// PRD FR-61): the four BinFlow metric families, the request-counting
// middleware, the root-level GET /metrics endpoint and the scrape-time
// snapshot pulls.
//
// The generic registry lives in internal/metrics; this file owns the
// PRODUCT decisions — family names, labels, pre-seeding and which Deps each
// snapshot reads. Instrumentation is opt-in at assembly: Deps.Metrics nil
// (every pre-existing test stack) leaves s.metrics nil, the middleware out
// of the chain and /metrics answering 503 — zero behavior change for stacks
// that did not ask for metrics.

package httpapi

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metrics"
	"github.com/lzwzzy/binflow/internal/replication"
)

// Metric family names (ADR-0022 naming rule binflow_<subsystem>_<metric>_<unit>;
// the PRD FR-61 spellings for the families H52 asserts). The two storage
// gauges dropped their v1 `_total` suffixes in T-197 (D5): Prometheus
// reserves `_total` for counters — `promtool check metrics` lints it and the
// registry now rejects such declarations.
const (
	metricHTTPRequestsTotal = "binflow_http_requests_total"
	metricHTTPDuration      = "binflow_http_request_duration_seconds"
	metricHTTPInFlight      = "binflow_http_requests_in_flight"
	metricStorageBlobs      = "binflow_storage_blobs"
	metricStorageBlobBytes  = "binflow_storage_blob_bytes"
	metricAuthLoginsTotal   = "binflow_auth_logins_total"
	metricReplicationTasks  = "binflow_replication_tasks"
	// M10 T-283 (PRD FR-85.4, ADR-0032): the entitlement plane's three
	// families — the effective tier as a gauge over the closed tier order
	// (community=0, pro=1, enterprise=2; unlicensed IS community, the
	// floor), the addon gate's decision counter, and the per-slot unlock
	// level refreshed from the SAME evaluation /api/v1/addons serves
	// (visibility and execution cannot diverge).
	metricLicenseTier    = "binflow_license_tier"
	metricAddonGateTotal = "binflow_addon_gate_requests_total"
	metricAddonsEnabled  = "binflow_addons_enabled"
	// M11 T-324 (PRD FR-102.2 / §9 metrics row): the cleanup engine's two
	// cumulative observability families — artifacts removed and logical
	// bytes reclaimed by the unused-cleanup policy since process start.
	// Gauge-typed at scrape time (the replTasks snapshot precedent): the
	// engine owns the counters, /metrics only reads them, so a scrape can
	// never miss or double-count a run. The `_total` suffix stays off —
	// Prometheus reserves it for counter-typed families.
	metricCleanupObjects = "binflow_cleanup_objects"
	metricCleanupBytes   = "binflow_cleanup_bytes"
)

// metricsContentType is the Prometheus text exposition format version 0.0.4
// content type (PRD FR-61 / ADR-0022 design 1).
const metricsContentType = "text/plain; version=0.0.4; charset=utf-8"

// instrumentation bundles the registered family handles plus the snapshot
// inputs resolved once at assembly.
type instrumentation struct {
	reg         *metrics.Registry
	engineLabel string // storage backend as the storage gauges' engine label
	httpReqs    *metrics.Counter
	httpDur     *metrics.Histogram
	httpInFlt   *metrics.Gauge
	stBlobs     *metrics.Gauge
	stBytes     *metrics.Gauge
	authLogins  *metrics.Counter
	// replTasks is nil unless Deps.Replication is wired: FR-61-AC4 keeps the
	// family unexposed (not merely zero) when replication is disabled.
	replTasks *metrics.Gauge
	// licTier is the effective license tier as the closed order's ordinal
	// (M10 T-283); refreshed at scrape time from the license snapshot —
	// install/uninstall/expiry flip it on the next scrape, never a restart.
	licTier *metrics.Gauge
	// addonGates counts the addon gate's decisions (allow/deny) per slot on
	// the CONTENT plane (weave 1: gated-slot write verbs) and the feature
	// seam (RequireAddon). The CONFIGURATION plane's D3 refusals are the
	// audit trail's rows, not this counter — a 400 validation refusal is
	// not a gated request.
	addonGates *metrics.Counter
	// addonsOn is nil unless Deps.Addons is wired (the replTasks
	// precedent): one 0/1 series per slot, refreshed at scrape time from
	// the same evaluation the /api/v1/addons view serves.
	addonsOn *metrics.Gauge
	// cleanupObjs / cleanupBytes are nil unless Deps.Cleanup is wired (the
	// replTasks precedent, T-324): cumulative policy-pass totals refreshed
	// at scrape time from the engine's own counters.
	cleanupObjs  *metrics.Gauge
	cleanupBytes *metrics.Gauge
}

// newInstrumentation registers the four families on reg and pre-seeds the
// label combinations whose presence FR-61-AC1 asserts before any traffic:
// an exposition that only grows series after boot is also kinder to
// Prometheus restarts.
func newInstrumentation(deps Deps) *instrumentation {
	ins := &instrumentation{
		reg:         deps.Metrics,
		engineLabel: engineLabelOf(deps.Config.Storage.Backend),
	}
	reg := deps.Metrics
	ins.httpReqs = mustCounter(reg, metricHTTPRequestsTotal,
		"Total HTTP requests by method, normalized route and status code.")
	ins.httpDur = mustHistogram(reg, metricHTTPDuration,
		"HTTP request duration in seconds by method and normalized route.", metrics.DefaultBuckets)
	ins.httpInFlt = mustGauge(reg, metricHTTPInFlight,
		"HTTP requests currently being served.")
	ins.stBlobs = mustGauge(reg, metricStorageBlobs,
		"Blob ledger rows in the metadata store, by storage engine.")
	ins.stBytes = mustGauge(reg, metricStorageBlobBytes,
		"Stored blob bytes by storage engine: physical bytes under blobs/ on disk or, on s3, summed blob object bytes from the engine's bucket listing.")
	ins.authLogins = mustCounter(reg, metricAuthLoginsTotal,
		"Console logins by identity provider source.")

	// The entitlement plane (M10 T-283): tier gauge seeded at the community
	// floor (the honest pre-first-scrape state of an unlicensed instance)
	// and the decision counter pre-seeded per slot so the family is visible
	// before the first gated request. The per-slot unlock gauge rides the
	// registry's presence like replTasks rides the replication store.
	ins.licTier = mustGauge(reg, metricLicenseTier,
		"Effective license tier as the closed order's ordinal: 0=community (the unlicensed floor), 1=pro, 2=enterprise.")
	ins.addonGates = mustCounter(reg, metricAddonGateTotal,
		"Addon entitlement gate decisions on the content plane (gated-slot write verbs) and the feature-addon seam, by addon and decision.")
	ins.licTier.Set(float64(license.TierCommunity))

	if deps.Addons != nil {
		ins.addonsOn = mustGauge(reg, metricAddonsEnabled,
			"Addon slot unlock level (1=enabled) at scrape time, from the same evaluation GET /api/v1/addons serves.")
		for _, a := range deps.Addons.PackageTypeAddons() {
			ins.addonGates.Add(0, "addon", a.ID, "decision", gateDecisionAllow)
			ins.addonGates.Add(0, "addon", a.ID, "decision", gateDecisionDeny)
		}
		for _, a := range deps.Addons.FeatureAddons() {
			ins.addonGates.Add(0, "addon", a.ID, "decision", gateDecisionAllow)
			ins.addonGates.Add(0, "addon", a.ID, "decision", gateDecisionDeny)
		}
	}

	ins.httpInFlt.Set(0)
	ins.stBlobs.Set(0, "engine", ins.engineLabel)
	ins.stBytes.Set(0, "engine", ins.engineLabel)
	for _, source := range []string{"local", "oidc", "ldap"} {
		ins.authLogins.Add(0, "source", source)
	}

	if deps.Replication != nil {
		ins.replTasks = mustGauge(reg, metricReplicationTasks,
			"Push-replication task rows in the ledger, by status.")
		for _, status := range []string{
			replication.TaskStatusPending, replication.TaskStatusInProgress,
			replication.TaskStatusSuccess, replication.TaskStatusFailed,
			replication.TaskStatusSkipped,
		} {
			ins.replTasks.Set(0, "status", status)
		}
	}
	if deps.Cleanup != nil {
		ins.cleanupObjs = mustGauge(reg, metricCleanupObjects,
			"Artifacts removed by the unused-cleanup policy since process start (cumulative).")
		ins.cleanupBytes = mustGauge(reg, metricCleanupBytes,
			"Logical artifact bytes reclaimed by the unused-cleanup policy since process start (cumulative).")
		ins.cleanupObjs.Set(0)
		ins.cleanupBytes.Set(0)
	}
	return ins
}

// Registration helpers: the names above are package constants, so a
// registration failure is an assembly bug (name drift or double family) —
// the panic surfaces in tests, matching the adapter registry's posture.

func mustCounter(r *metrics.Registry, name, help string) *metrics.Counter {
	c, err := r.NewCounter(name, help)
	if err != nil {
		panic("httpapi: metric registration: " + err.Error())
	}
	return c
}

func mustGauge(r *metrics.Registry, name, help string) *metrics.Gauge {
	g, err := r.NewGauge(name, help)
	if err != nil {
		panic("httpapi: metric registration: " + err.Error())
	}
	return g
}

func mustHistogram(r *metrics.Registry, name, help string, buckets []float64) *metrics.Histogram {
	h, err := r.NewHistogram(name, help, buckets)
	if err != nil {
		panic("httpapi: metric registration: " + err.Error())
	}
	return h
}

// engineLabelOf resolves the storage engine label ("disk" default).
func engineLabelOf(backend string) string {
	if backend == "" {
		return "disk"
	}
	return backend
}

// middleware counts every request through the base chain: in-flight level,
// request total by (method, normalized route, status) and the latency
// histogram by (method, normalized route).
//
// Position: between accessLog and recoverPanic. accessLog's statusRecorder
// must already wrap the writer (this layer reads the recorded status), and
// recover runs INSIDE so a recovered panic still lands in the counters —
// the deferred accounting survives the unwinding because the recover is
// consumed before control returns here.
func (ins *instrumentation) middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ins.httpInFlt.Add(1)
			defer func() {
				ins.httpInFlt.Add(-1)
				status := http.StatusOK
				if rec, ok := w.(*statusRecorder); ok {
					status = rec.status
				}
				route := normalizeMetricsPath(r.URL.EscapedPath())
				ins.httpReqs.Inc("method", r.Method, "path", route, "status", strconv.Itoa(status))
				ins.httpDur.Observe(time.Since(start).Seconds(), "method", r.Method, "path", route)
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// countLogin records one successful console login by provider source (the
// auth family's only instrumented event in T-163's minimal wiring).
func (ins *instrumentation) countLogin(source string) {
	ins.authLogins.Inc("source", source)
}

// countAddonGate records one addon-gate decision (M10 T-283): the counter is
// the gate's ONLY always-on observer — audit rows carry the refusals'
// context, the counter carries both directions' volume. Metrics-less stacks
// (Deps.Metrics nil) count nothing, gate exactly the same.
func (s *Server) countAddonGate(id, decision string) {
	if s.metrics == nil {
		return
	}
	s.metrics.addonGates.Inc("addon", id, "decision", decision)
}

// metricsHandler assembles GET /metrics (root level, the /healthz-family
// probe exemption — ADR-0022 design 1, PRD FR-61). Anonymous by default;
// metrics.require_auth=true adds the authenticator plus a principal gate
// (any authenticated arm passes — the PRD's "需认证", not an admin gate).
// The authenticator is deliberately ABSENT in the default posture: a scrape
// must not pay an auth round trip (the same reason probes skip it), and a
// rejected credential cannot downgrade anything an anonymous caller could
// already read.
func (s *Server) metricsHandler() http.Handler {
	if s.metrics == nil {
		return s.baseChain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusServiceUnavailable, "metrics are not configured on this instance")
		}))
	}
	terminal := http.HandlerFunc(s.serveMetrics)
	if !s.deps.Config.Metrics.RequireAuth {
		return s.baseChain(terminal)
	}
	gate := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principalFrom(r.Context()) == nil {
			// Covers both "no credential" and authenticate's
			// presented-but-rejected signal (which never downgrades).
			w.Header().Set("WWW-Authenticate", basicChallenge)
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		terminal.ServeHTTP(w, r)
	})
	return s.baseChain(chain(authenticate(s.deps.Auth))(gate))
}

// serveMetrics renders the registry. Snapshots refresh FIRST so one scrape
// observes a consistent instant; a snapshot failure logs at Debug and keeps
// the previous values — the endpoint itself must never fail over a
// store hiccup.
func (s *Server) serveMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "metrics endpoint supports GET and HEAD only")
		return
	}
	s.refreshMetricsSnapshots(r.Context())
	body := s.metrics.reg.Format()
	w.Header().Set("Content-Type", metricsContentType)
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, body) //nolint:errcheck // scrape body; a vanished scraper is not an error
}

// refreshMetricsSnapshots pulls the storage and replication gauges from
// Deps on every scrape (T-163's minimal instrumentation: no write-path
// hooks, the snapshot is computed at read time).
func (s *Server) refreshMetricsSnapshots(ctx context.Context) {
	ins := s.metrics
	if s.deps.Metadata != nil {
		if n, err := s.deps.Metadata.Blobs().Count(ctx); err != nil {
			s.log.DebugContext(ctx, "httpapi: metrics blob-count snapshot failed", "error", err.Error())
		} else {
			ins.stBlobs.Set(float64(n), "engine", ins.engineLabel)
		}
		s.refreshStorageBytes(ctx)
	}
	if s.deps.Replication != nil && ins.replTasks != nil {
		s.refreshReplication(ctx)
	}
	if s.deps.Cleanup != nil && ins.cleanupObjs != nil {
		st := s.deps.Cleanup.Stats()
		ins.cleanupObjs.Set(float64(st.ObjectsCleaned))
		ins.cleanupBytes.Set(float64(st.BytesReclaimed))
	}
	s.refreshLicenseMetrics(ctx)
}

// refreshLicenseMetrics pulls the entitlement gauges (M10 T-283): the
// effective tier and, when the registry is mounted, every slot's unlock
// level — read from the SAME single evaluation the /api/v1/addons view and
// the enforcement gates consult (s.addonsEval), so a scrape can never
// disagree with a refusal the instance just answered.
func (s *Server) refreshLicenseMetrics(ctx context.Context) {
	ins := s.metrics
	tier := license.TierCommunity
	if s.addonsEval != nil {
		tier = s.addonsEval.State().Tier
	}
	ins.licTier.Set(float64(tier))
	if s.deps.Addons == nil || ins.addonsOn == nil {
		return
	}
	for _, row := range s.deps.Addons.Statuses(ctx, s.addonsEval) {
		level := float64(0)
		if row.Enable {
			level = 1
		}
		ins.addonsOn.Set(level, "addon", row.Addon.ID)
	}
}

// refreshStorageBytes sets the byte gauge per engine: the disk backend
// walks blobs/ (physical bytes, the same walk /api/v1/storage/stats uses).
// An engine-backed inventory (s3 backend since T-201, T-173 D-1) sizes the
// bucket through the engine's listing — physical object bytes, the same
// number the stats endpoint reports. The pre-T-201 s3 fallback (summing
// repo_usage logical bytes, an approximation ADR-0022 documented as an
// M6+ refinement) remains for s3-labeled stacks assembled without the
// seam: dual-write instances, whose data dir still carries every blob and
// therefore answer through the disk walk's engineLabel != "s3" arm anyway.
func (s *Server) refreshStorageBytes(ctx context.Context) {
	ins := s.metrics
	if s.deps.BlobInventory != nil {
		stats, err := s.deps.BlobInventory.BlobStats(ctx)
		if err != nil {
			s.log.DebugContext(ctx, "httpapi: metrics blob-bytes snapshot failed", "error", err.Error())
			return
		}
		var total int64
		for _, st := range stats {
			total += st.Size
		}
		ins.stBytes.Set(float64(total), "engine", ins.engineLabel)
		return
	}
	if ins.engineLabel != "s3" {
		if s.deps.DataDir == "" {
			return
		}
		n, err := dirSize(filepath.Join(s.deps.DataDir, "blobs"))
		if err != nil {
			s.log.DebugContext(ctx, "httpapi: metrics blob-bytes snapshot failed", "error", err.Error())
			return
		}
		ins.stBytes.Set(float64(n), "engine", ins.engineLabel)
		return
	}
	// s3 without the inventory seam: sum the per-repository usage rows
	// (repos × one indexed read; a failed read skips that repo's
	// contribution rather than the scrape).
	repos, err := s.deps.Metadata.Repos().List(ctx)
	if err != nil {
		s.log.DebugContext(ctx, "httpapi: metrics usage snapshot failed", "error", err.Error())
		return
	}
	var total int64
	for _, repo := range repos {
		u, err := s.deps.Metadata.Usage().Get(ctx, repo.RepoKey)
		if err != nil {
			continue
		}
		total += u.LogicalBytes
	}
	ins.stBytes.Set(float64(total), "engine", ins.engineLabel)
}

// refreshReplication sums the per-config task counts into the status gauge.
func (s *Server) refreshReplication(ctx context.Context) {
	configs, err := s.deps.Replication.ListConfigs(ctx)
	if err != nil {
		s.log.DebugContext(ctx, "httpapi: metrics replication snapshot failed", "error", err.Error())
		return
	}
	totals := map[string]int64{}
	for _, c := range configs {
		st, err := s.deps.Replication.Status(ctx, c.ID)
		if err != nil {
			continue
		}
		totals[replication.TaskStatusPending] += st.Pending
		totals[replication.TaskStatusInProgress] += st.InProgress
		totals[replication.TaskStatusSuccess] += st.Succeeded
		totals[replication.TaskStatusFailed] += st.Failed
		totals[replication.TaskStatusSkipped] += st.Skipped
	}
	for status, n := range totals {
		s.metrics.replTasks.Set(float64(n), "status", status)
	}
}

// normalizeMetricsPath maps a raw escaped path onto a bounded-cardinality
// route template (FR-61-AC2: normalized path label cardinality < 100).
// Unbounded surfaces collapse to their family:
//
//   - probes and /metrics keep their exact spelling (3 values);
//   - the /v2 registry plane collapses to "/v2" (repo/image digests are the
//     highest-cardinality surface BinFlow serves);
//   - console/assets/docs collapse to their segment;
//   - content paths collapse to "/binflow/:repo/:path";
//   - API routes collapse per known variable-tail family, with a two-segment
//     literal prefix + ":rest" fallback for families this table has not met.
func normalizeMetricsPath(path string) string {
	switch {
	case path == "/healthz", path == "/readyz", path == "/metrics":
		return path
	case path == "/v2" || strings.HasPrefix(path, "/v2/"):
		return "/v2"
	}
	rest, ok := strings.CutPrefix(path, "/binflow")
	if !ok {
		return path // outside the product prefix: /healthz-style strays, at most a handful
	}
	if rest == "" || rest == "/" {
		return "/binflow"
	}
	segs := strings.Split(strings.TrimPrefix(rest, "/"), "/")
	switch segs[0] {
	case "ui", "assets", "docs":
		return "/binflow/" + segs[0]
	case "api":
		return "/binflow/api" + normalizeAPIPath(segs[1:])
	default:
		return "/binflow/:repo/:path"
	}
}

// normalizeAPIPath collapses the variable tails of the known API families
// (segment slice after "/binflow/api"). Routes the table does not know keep
// their literal spelling up to three segments; anything deeper collapses to
// a two-segment literal prefix plus :rest, so an unmapped family cannot
// reopen the cardinality hole.
func normalizeAPIPath(segs []string) string {
	literal := func() string { return "/" + strings.Join(segs, "/") }
	tail := func(keep int, param string) string {
		return "/" + strings.Join(segs[:keep], "/") + "/" + param
	}
	switch {
	case len(segs) == 0:
		return ""
	case segs[0] == "storage" && len(segs) > 1:
		return tail(1, ":repo/:path")
	case segs[0] == "repositories" && len(segs) > 1:
		return tail(1, ":key")
	case segs[0] == "npm" && len(segs) > 1:
		return tail(1, ":repo/:path")
	case segs[0] == "pypi" && len(segs) > 1:
		return tail(1, ":repo/:path")
	case segs[0] == "security" && len(segs) > 2 && (segs[1] == "users" || segs[1] == "groups"):
		return tail(2, ":name")
	case segs[0] == "v1" && len(segs) > 2 && segs[1] == "replications":
		return tail(2, ":name")
	case segs[0] == "v1" && len(segs) > 3 && segs[1] == "storage" && segs[2] == "usage":
		return tail(3, ":repo")
	}
	if len(segs) > 3 {
		return tail(2, ":rest")
	}
	return literal()
}
