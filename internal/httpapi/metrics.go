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
	"github.com/lzwzzy/binflow/internal/search"
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
	// The fail-open replay family (M12 T-338, ADR-0040 observability):
	// queue depth and window state are gauges of the present; the five
	// totals are cumulative counters published as gauges refreshed at
	// scrape time (the cleanup precedent — the engine owns the counts).
	metricReplayQueueDepth   = "binflow_replay_queue_depth"
	metricReplayWindowOpen   = "binflow_replay_window_open"
	metricReplayDrained      = "binflow_replay_drained"
	metricReplayFailedRetry  = "binflow_replay_failed_retry"
	metricReplayFailedPerm   = "binflow_replay_failed_permanent"
	metricReplaySourceGone   = "binflow_replay_source_gone"
	metricReplayReadFallback = "binflow_replay_read_fallback"
	// The search family (M15 T-415, ADR-0043 pt 8): the query counter keyed
	// by plane (aql today; the legacy endpoints join with their own plane
	// values in T-417), the latency histogram and the resource-gate
	// rejection counter (concurrency = the 429 arm, timeout = the 408 arm).
	metricSearchQueries    = "binflow_search_queries_total"
	metricSearchDuration   = "binflow_search_query_duration_seconds"
	metricSearchRejections = "binflow_search_rejections_total"
	// The QRL family (M16 T-452, FR-148.2 / aql.md §14.4 — Artifactory's
	// jfrt_qrl provider mapped onto the BinFlow naming rule): the tri-state
	// as a gauge ordinal plus the sampled-window counters, one series per
	// bucket type. The anchor's 60s sampling job maps onto scrape-time
	// sampling (the cleanup/replay gauge precedent: the engine owns the
	// counters, /metrics samples-and-resets them; a 60s scrape cadence IS
	// the anchor's 60s window).
	metricQRLMode           = "binflow_qrl_mode"
	metricQRLQueries        = "binflow_qrl_window_queries"
	metricQRLPermits        = "binflow_qrl_window_permits"
	metricQRLSlowdown       = "binflow_qrl_slowed_down_millis"
	metricQRLSlowdownByTime = "binflow_qrl_slowed_down_by_time_millis"
	metricQRLCharged        = "binflow_qrl_charged_query_time_millis"
	// The build family (M17 T-508/T-509, FR-152.2 / ADR-0045 decision 10):
	// the write counter by operation and outcome, the read counter by face,
	// the upload/append duration histogram, and T-509's promote half — a
	// counter by outcome (promoted / dry-run / status-only) plus the promote
	// duration histogram (decision 10's second half, the NFR-P80
	// observability arm). All on the Prometheus naming rule.
	metricBuildsPutTotal       = "binflow_builds_put_total"
	metricBuildsGetTotal       = "binflow_builds_get_total"
	metricBuildsPutSeconds     = "binflow_builds_put_duration_seconds"
	metricBuildsPromoteTotal   = "binflow_builds_promote_total"
	metricBuildsPromoteSeconds = "binflow_builds_promote_duration_seconds"
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
	// replay* are nil unless Deps.Replay is wired (dual-write only).
	replayQD     *metrics.Gauge
	replayWin    *metrics.Gauge
	replayDrain  *metrics.Gauge
	replayRetry  *metrics.Gauge
	replayPerm   *metrics.Gauge
	replayGone   *metrics.Gauge
	replayReadFB *metrics.Gauge
	// search* are the AQL plane's three families (M15 T-415): query volume,
	// latency and resource-gate rejections.
	searchQueries *metrics.Counter
	searchDur     *metrics.Histogram
	searchRej     *metrics.Counter
	// qrl* are the query rate limiter's sampled-window gauges (M16 T-452),
	// refreshed at scrape time from the shared limiter (s.qrl).
	qrlMode           *metrics.Gauge
	qrlQueries        *metrics.Gauge
	qrlPermits        *metrics.Gauge
	qrlSlowdown       *metrics.Gauge
	qrlSlowdownByTime *metrics.Gauge
	qrlCharged        *metrics.Gauge
	// builds* are the build family's write/read counters, the write duration
	// histogram (M17 T-508) and the promote counter/histogram pair (T-509).
	buildsPut        *metrics.Counter
	buildsGet        *metrics.Counter
	buildsPutDur     *metrics.Histogram
	buildsPromote    *metrics.Counter
	buildsPromoteDur *metrics.Histogram
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

	// The search family (M15 T-415; the legacy plane joined in T-417):
	// pre-seeded so the exposition shows both planes and both rejection
	// reasons before the first query lands.
	ins.searchQueries = mustCounter(reg, metricSearchQueries,
		"Search-plane queries executed, by plane.")
	ins.searchDur = mustHistogram(reg, metricSearchDuration,
		"Search-plane query duration in seconds.", metrics.DefaultBuckets)
	ins.searchRej = mustCounter(reg, metricSearchRejections,
		"Search-plane query rejections by resource-gate reason (concurrency = the 429 gate arm, timeout = the 408 deadline arm).")
	ins.searchQueries.Add(0, "plane", "aql")
	ins.searchQueries.Add(0, "plane", "legacy")
	ins.searchRej.Add(0, "reason", "concurrency")
	ins.searchRej.Add(0, "reason", "timeout")

	// The QRL family (M16 T-452): the mode gauge seeded at the disabled
	// ordinal, the window counters pre-seeded per bucket type so the
	// exposition shows the family before the first enabled window.
	ins.qrlMode = mustGauge(reg, metricQRLMode,
		"Query rate limiter tri-state as an ordinal: 0=disabled (factory), 1=enabled, 2=simulation.")
	ins.qrlQueries = mustGauge(reg, metricQRLQueries,
		"Query rate limiter sampled-window query attempts, by bucket type (reset each sample).")
	ins.qrlPermits = mustGauge(reg, metricQRLPermits,
		"Query rate limiter sampled-window permits granted, by bucket type (reset each sample).")
	ins.qrlSlowdown = mustGauge(reg, metricQRLSlowdown,
		"Query rate limiter sampled-window throttle delay from permit starvation, in milliseconds, by bucket type.")
	ins.qrlSlowdownByTime = mustGauge(reg, metricQRLSlowdownByTime,
		"Query rate limiter sampled-window throttle delay from the charged-time quota, in milliseconds, by bucket type.")
	ins.qrlCharged = mustGauge(reg, metricQRLCharged,
		"Query rate limiter sampled-window charged query time, in milliseconds, by bucket type.")
	ins.qrlMode.Set(0)
	for _, t := range []string{search.QRLTypeDefault, search.QRLTypeLowPriority} {
		ins.qrlQueries.Set(0, "type", t)
		ins.qrlPermits.Set(0, "type", t)
		ins.qrlSlowdown.Set(0, "type", t)
		ins.qrlSlowdownByTime.Set(0, "type", t)
		ins.qrlCharged.Set(0, "type", t)
	}

	// The build family (M17 T-508): pre-seeded per operation/outcome and
	// per read face so the exposition shows the family before the first CI
	// publish — upload's two arms, append's merge arm, and the three GET
	// faces (names, numbers, detail).
	ins.buildsPut = mustCounter(reg, metricBuildsPutTotal,
		"Build-info write operations (PUT upload and POST append) by operation and outcome.")
	ins.buildsGet = mustCounter(reg, metricBuildsGetTotal,
		"Build-info read operations by face (names list, numbers list, single-run detail).")
	ins.buildsPutDur = mustHistogram(reg, metricBuildsPutSeconds,
		"Build-info write operation duration in seconds by operation.", metrics.DefaultBuckets)
	for _, outcome := range []string{"created", "replaced"} {
		ins.buildsPut.Add(0, "operation", "upload", "outcome", outcome)
	}
	ins.buildsPut.Add(0, "operation", "append", "outcome", "merged")
	for _, face := range []string{"names", "numbers", "detail"} {
		ins.buildsGet.Add(0, "face", face)
	}
	// T-509's promote half: pre-seeded per outcome so the family exposes
	// itself before the first promotion (the restart-kindness rule above).
	ins.buildsPromote = mustCounter(reg, metricBuildsPromoteTotal,
		"Build promotions by outcome (promoted, dry-run, status-only).")
	ins.buildsPromoteDur = mustHistogram(reg, metricBuildsPromoteSeconds,
		"Build promotion duration in seconds.", metrics.DefaultBuckets)
	for _, outcome := range []string{"promoted", "dry-run", "status-only"} {
		ins.buildsPromote.Add(0, "outcome", outcome)
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
	if deps.Replay != nil {
		ins.replayQD = mustGauge(reg, metricReplayQueueDepth,
			"Blobs queued for S3 replay by the dual-write fail-open window (gauge).")
		ins.replayWin = mustGauge(reg, metricReplayWindowOpen,
			"1 while the dual-write S3 failure window is open, else 0.")
		ins.replayDrain = mustGauge(reg, metricReplayDrained,
			"Blobs confirmed on S3 by replay drains since process start (cumulative).")
		ins.replayRetry = mustGauge(reg, metricReplayFailedRetry,
			"Replay copy attempts that failed and will retry (cumulative).")
		ins.replayPerm = mustGauge(reg, metricReplayFailedPerm,
			"Replay entries exhausted to permanent failure — still queued, accounted (cumulative).")
		ins.replayGone = mustGauge(reg, metricReplaySourceGone,
			"Replay entries dropped because the source blob vanished (cumulative).")
		ins.replayReadFB = mustGauge(reg, metricReplayReadFallback,
			"Reads served from disk after S3 missed or errored (cumulative).")
		for _, g := range []*metrics.Gauge{ins.replayQD, ins.replayWin, ins.replayDrain,
			ins.replayRetry, ins.replayPerm, ins.replayGone, ins.replayReadFB} {
			g.Set(0)
		}
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

// observeBuildsPut records one successful build-info write (M17 T-508): the
// operation/outcome counter family and the duration histogram. Failures are
// the HTTP request counter's status series, not this family's — the
// counter answers "how many publishes landed, and which arm", matching the
// ticket's upload-count-and-duration wording.
func (s *Server) observeBuildsPut(operation, outcome string, d time.Duration) {
	if s.metrics == nil {
		return
	}
	s.metrics.buildsPut.Inc("operation", operation, "outcome", outcome)
	s.metrics.buildsPutDur.Observe(d.Seconds(), "operation", operation)
}

// countBuildsGet records one successful build-info read by face (M17 T-508).
func (s *Server) countBuildsGet(face string) {
	if s.metrics == nil {
		return
	}
	s.metrics.buildsGet.Inc("face", face)
}

// observeBuildsPromote records one successful promotion (M17 T-509): the
// outcome counter (promoted / dry-run / status-only — failures stay the HTTP
// status series) and the duration histogram (NFR-P80's observability arm:
// the large-build migration budget reads off this family).
func (s *Server) observeBuildsPromote(outcome string, d time.Duration) {
	if s.metrics == nil {
		return
	}
	s.metrics.buildsPromote.Inc("outcome", outcome)
	s.metrics.buildsPromoteDur.Observe(d.Seconds())
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
	if s.deps.Replay != nil && ins.replayQD != nil {
		rs := s.deps.Replay.ReplayStats()
		ins.replayQD.Set(float64(rs.QueueDepth))
		ins.replayWin.Set(boolFloat(rs.WindowOpen))
		ins.replayDrain.Set(float64(rs.DrainedTotal))
		ins.replayRetry.Set(float64(rs.FailedRetry))
		ins.replayPerm.Set(float64(rs.FailedPermanent))
		ins.replayGone.Set(float64(rs.SourceGone))
		ins.replayReadFB.Set(float64(rs.ReadFallbackTotal))
	}
	s.refreshQRLMetrics()
	s.refreshLicenseMetrics(ctx)
}

// refreshQRLMetrics samples the query rate limiter (M16 T-452, aql.md
// §14.4): the mode gauge always; when the limiter is ACTIVE the window
// counters are sampled-and-reset (the anchor's metrics job — one sampling
// per scrape, the disabled state samples nothing per the anchor's
// "仅 enabled 态调度"). A window with throttle delay also emits the INFO
// line with the anchor's verbatim copy, placeholders filled.
func (s *Server) refreshQRLMetrics() {
	ins := s.metrics
	mode := s.qrl.Mode()
	ins.qrlMode.Set(float64(qrlModeOrdinal(mode)))
	if mode == search.QRLModeDisabled {
		return
	}
	sample := s.qrl.Sample()
	for _, b := range sample.Buckets {
		ins.qrlQueries.Set(float64(b.TotalQueries), "type", b.RLType)
		ins.qrlPermits.Set(float64(b.TotalPermits), "type", b.RLType)
		ins.qrlSlowdown.Set(float64(b.SlowedDownMillis), "type", b.RLType)
		ins.qrlSlowdownByTime.Set(float64(b.SlowedDownByTimeMillis), "type", b.RLType)
		ins.qrlCharged.Set(float64(b.ChargedQueryTime), "type", b.RLType)
		if slowed := b.SlowedDownMillis + b.SlowedDownByTimeMillis; slowed > 0 {
			pct := 0.0
			if sample.WindowMillis > 0 {
				pct = float64(slowed) / float64(sample.WindowMillis) * 100
			}
			s.log.Info("Artifactory database queries have reached the set limit ("+
				strconv.FormatInt(bucketPermits(s.qrl.Settings(), b.RLType), 10)+" per "+
				strconv.FormatInt(bucketFrame(s.qrl.Settings(), b.RLType), 10)+" ms). "+
				"Throttling has been applied ("+strconv.FormatFloat(pct, 'f', 0, 64)+
				"% of the time) to protect system health.",
				"rl_type", b.RLType)
		}
	}
}

// qrlModeOrdinal maps the tri-state onto the gauge ordinal.
func qrlModeOrdinal(m search.QRLMode) int {
	switch m {
	case search.QRLModeEnabled:
		return 1
	case search.QRLModeSimulation:
		return 2
	default:
		return 0
	}
}

// bucketPermits reads one bucket type's permit setting (0 when absent).
func bucketPermits(settings []search.QRLSetting, rlType string) int64 {
	for _, s := range settings {
		if s.RLType == rlType {
			return s.PermitsPerTimeFrame
		}
	}
	return 0
}

// bucketFrame reads one bucket type's frame setting (0 when absent).
func bucketFrame(settings []search.QRLSetting, rlType string) int64 {
	for _, s := range settings {
		if s.RLType == rlType {
			return s.TimeFrameMillis
		}
	}
	return 0
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
	case segs[0] == "build" && len(segs) > 1:
		// The build family's variable tails (names list, numbers, detail,
		// append) collapse to one template — build names and numbers are
		// CI free-form strings (the highest-cardinality identifiers this
		// family carries).
		return tail(1, ":name/:number")
	}
	if len(segs) > 3 {
		return tail(2, ":rest")
	}
	return literal()
}

// boolFloat renders a boolean gauge value.
func boolFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
