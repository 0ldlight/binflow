package httpapi

// The gc-cron-gap three carriers' kernels (M17 T-495, FR-158): the quota
// check, the metadata compress and the prune dry-run the maintenance
// plane's three new cron slots (console-ui.md §3.9 blocks 2/5 — "Enable
// Quota Control" / "Compress the Internal Database" / "Prune Unreferenced
// Data") dispatch at fire time. M16 T-462 registered the three as an
// honest gap ("无载体不伪造" — no carrier, no fabricated face); this file is
// the carrier half of that gap's closure, the RunGC family's third member
// set: one kernel per carrier, ridden by the cron slot through the cmd
// wiring exactly the way maintenance/gc rides RunGC (ADR-0044 decisions
// 6/8⑤ — never a second executor).
//
// The honest action surface of each carrier (the ticket's registered
// split, real enforcement deferred to a later enforcement ticket):
//
//   - quota: a THRESHOLD CHECK plus the alert trail. The thresholds are
//     the per-repository quotaBytes ceilings the storage-quota page owns
//     (T-462's FE note registered that reading: "quota 阈值走存储配额页");
//     the instance-level percentage pair of the Artifactory page has no
//     BinFlow carrier and stays uncarried. The check is read-only — it
//     neither blocks nor deletes; each repository over its ceiling gets
//     one WARN log and one row in the maintenance.quota.check audit
//     detail.
//   - compress: the metadata store's VACUUM rebuild (the sqlite form; a
//     future postgres store satisfies the same consumer-side seam with
//     its own form — VACUUM ANALYZE).
//   - prune: the unreferenced-blob sweep in the GC ENGINE's dry-run form
//     — it reports the magnitude, it does not delete (the gc slot stays
//     the deletion surface; ADR-0044's one-carrier law means the prune
//     slot reuses RunGC rather than growing a second sweep).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/repo"
)

// metadataCompressor is the compress carrier's consumer-side seam over the
// metadata store's rebuild face (the cmd export kernel's
// metadataSnapshotter precedent: the capability lives on the concrete
// store, the interface on the consumer). sqliteStore implements it; an
// assembly whose store carries no such face fails the carrier honestly
// instead of pretending a rebuild happened.
type metadataCompressor interface {
	Vacuum(ctx context.Context) (beforeBytes, afterBytes int64, err error)
}

// QuotaCheckRequest is one scheduled quota check's request: the actor the
// maintenance.quota.check audit row names ("scheduler" for a cron fire).
type QuotaCheckRequest struct {
	Actor      string
	RemoteAddr string
}

// QuotaFinding is one repository the check found over its ceiling.
type QuotaFinding struct {
	Repo       string `json:"repo"`
	UsedBytes  int64  `json:"usedBytes"`
	QuotaBytes int64  `json:"quotaBytes"`
	Percent    int64  `json:"percent"`
}

// QuotaCheckResult is one completed check's report.
type QuotaCheckResult struct {
	ReposChecked int
	OverQuota    []QuotaFinding
}

// quotaCheckDetail is the maintenance.quota.check audit payload: what was
// measured and what it found, capped at the first dozen findings (a fleet
// of hundreds of over-quota repositories is an operator problem the log's
// per-repo WARN lines carry in full; the audit row stays a bounded
// document).
type quotaCheckDetail struct {
	ReposChecked int            `json:"reposChecked"`
	OverQuota    int            `json:"overQuota"`
	Findings     []QuotaFinding `json:"findings,omitempty"`
}

// RunQuotaCheck executes ONE storage-quota threshold pass — the carrier
// the maintenance/quota cron slot rides. Every repository's usage row is
// compared against its configured quotaBytes ceiling (the same numbers
// GET /api/v1/storage/usage serves, read through the same service seam so
// there is no second accounting channel): repositories over their ceiling
// get one WARN log each, and the check lands one maintenance.quota.check
// audit row. Nothing is refused, blocked or deleted — enforcement is the
// later enforcement ticket; this carrier makes the violation visible on a schedule.
func (s *Server) RunQuotaCheck(ctx context.Context, req QuotaCheckRequest) (*QuotaCheckResult, error) {
	if req.Actor == "" {
		req.Actor = "scheduler"
	}
	if s.deps.ReposSvc == nil {
		return nil, errors.New("quota check is not available on this instance (no repository service wired)")
	}
	// The system identity, not a fabricated user: the scheduled check sees
	// every repository (the role short-circuit passes every allow() the
	// visibility formula runs — repo.SystemPrincipal's own contract), the
	// same full-fleet view the storage-quota page's admin reader has.
	rows, err := s.deps.ReposSvc.UsageBatch(ctx, repo.SystemPrincipal(), repo.UsageBatchQuery{})
	if err != nil {
		return nil, fmt.Errorf("quota check: %w", err)
	}
	res := &QuotaCheckResult{ReposChecked: len(rows)}
	for _, row := range rows {
		if row.QuotaBytes <= 0 || row.UsedBytes <= row.QuotaBytes {
			continue // unlimited or inside its ceiling
		}
		finding := QuotaFinding{
			Repo:       row.RepoKey,
			UsedBytes:  row.UsedBytes,
			QuotaBytes: row.QuotaBytes,
			// Float math: an int64 used*100 would overflow past ~92 PiB of
			// usage, and a percentage is a display number, not accounting.
			Percent: int64(float64(row.UsedBytes) / float64(row.QuotaBytes) * 100),
		}
		res.OverQuota = append(res.OverQuota, finding)
		s.log.WarnContext(ctx, "httpapi: scheduled quota check: repository over its quota",
			"repo", finding.Repo,
			"used_bytes", finding.UsedBytes, "quota_bytes", finding.QuotaBytes,
			"percent", finding.Percent)
	}
	detail := quotaCheckDetail{ReposChecked: res.ReposChecked, OverQuota: len(res.OverQuota)}
	if len(res.OverQuota) > 0 {
		capped := res.OverQuota
		if len(capped) > 12 {
			capped = capped[:12]
		}
		detail.Findings = capped
	}
	raw, derr := json.Marshal(detail)
	if derr != nil {
		s.log.WarnContext(ctx, "httpapi: quota check audit detail marshal failed", "error", derr.Error())
		raw = []byte("{}")
	}
	s.audit.Record(ctx, audit.Event{
		Actor:      req.Actor,
		Action:     audit.ActionMaintenanceQuotaCheck,
		RemoteAddr: req.RemoteAddr,
		Detail:     string(raw),
	})
	s.log.InfoContext(ctx, "httpapi: scheduled quota check complete",
		"repos", res.ReposChecked, "over_quota", len(res.OverQuota))
	return res, nil
}

// CompressRequest is one scheduled metadata compress run's request.
type CompressRequest struct {
	Actor      string
	RemoteAddr string
}

// CompressResult is one completed rebuild's report: the main database
// file's size before and after (the difference may legitimately be ~0 on
// a compact database — a rebuilt-but-not-smaller file is a successful
// run).
type CompressResult struct {
	BeforeBytes    int64
	AfterBytes     int64
	ReclaimedBytes int64
}

// compressRunDetail is the maintenance.compress.run audit payload.
type compressRunDetail struct {
	BeforeBytes    int64 `json:"beforeBytes"`
	AfterBytes     int64 `json:"afterBytes"`
	ReclaimedBytes int64 `json:"reclaimedBytes"`
}

// RunMetadataCompress executes ONE metadata database rebuild — the carrier
// the maintenance/compress cron slot rides (console-ui.md §3.9's "Compress
// the Internal Database", in its scheduled form). The rebuild itself is
// the store's own VACUUM face (see metadata.Vacuum's comment for the
// concurrency posture); a store without that face (the postgres
// placeholder) fails the carrier honestly.
func (s *Server) RunMetadataCompress(ctx context.Context, req CompressRequest) (*CompressResult, error) {
	if req.Actor == "" {
		req.Actor = "scheduler"
	}
	comp, ok := s.deps.Metadata.(metadataCompressor)
	if !ok {
		return nil, errors.New("metadata compression is not available on this instance (the store carries no rebuild face)")
	}
	// The family posture (RunGC's T-94 review N1): a rebuild already in
	// progress must run to its conclusion even when the scheduler's
	// context winds down mid-fire — VACUUM is atomic, and the audit row
	// for a completed rebuild must still land.
	runCtx := context.WithoutCancel(ctx)
	started := time.Now()
	before, after, err := comp.Vacuum(runCtx)
	if err != nil {
		s.log.ErrorContext(runCtx, "httpapi: scheduled metadata compress failed", "error", err.Error())
		return nil, fmt.Errorf("metadata compress: %w", err)
	}
	res := &CompressResult{BeforeBytes: before, AfterBytes: after, ReclaimedBytes: before - after}
	raw, derr := json.Marshal(compressRunDetail{
		BeforeBytes: res.BeforeBytes, AfterBytes: res.AfterBytes, ReclaimedBytes: res.ReclaimedBytes,
	})
	if derr != nil {
		s.log.WarnContext(runCtx, "httpapi: compress audit detail marshal failed", "error", derr.Error())
		raw = []byte("{}")
	}
	s.audit.Record(runCtx, audit.Event{
		Actor:      req.Actor,
		Action:     audit.ActionMaintenanceCompressRun,
		RemoteAddr: req.RemoteAddr,
		Detail:     string(raw),
	})
	s.log.InfoContext(runCtx, "httpapi: scheduled metadata compress complete",
		"before_bytes", res.BeforeBytes, "after_bytes", res.AfterBytes,
		"reclaimed_bytes", res.ReclaimedBytes,
		"elapsed", time.Since(started).Round(time.Millisecond).String())
	return res, nil
}

// PruneRunRequest is one scheduled prune dry-run's request.
type PruneRunRequest struct {
	Actor      string
	RemoteAddr string
}

// PruneRunResult is one completed dry-run's report.
type PruneRunResult struct {
	CandidateCount int
	CandidateBytes int64
}

// pruneRunDetail is the maintenance.prune.run audit payload.
type pruneRunDetail struct {
	CandidateCount int   `json:"candidateCount"`
	CandidateBytes int64 `json:"candidateBytes"`
	Applied        bool  `json:"applied"`
}

// RunPruneSweep executes ONE unreferenced-blob report pass — the carrier
// the maintenance/prune cron slot rides: the gc engine's dry-run form
// (RunGC with apply:false, the standing grace the scheduled gc pass also
// takes), wrapped in the prune slot's own carrier word. Nothing is
// deleted — the sweep lists the unreferenced magnitude on the schedule,
// and the gc slot (apply:true) stays the deletion surface. The audit
// trail is layered exactly the way a gc-slot fire's is: the engine's
// maintenance.schedule.run row, the sweep's own gc.run row (apply:false —
// a true record of the pass RunGC made) and this carrier's
// maintenance.prune.run row — three views of one fire, per ADR-0044
// decision 10's "the scheduler records that a schedule fired, the carrier
// records what it did".
func (s *Server) RunPruneSweep(ctx context.Context, req PruneRunRequest) (*PruneRunResult, error) {
	if req.Actor == "" {
		req.Actor = "scheduler"
	}
	gc, err := s.RunGC(ctx, GCRunRequest{Actor: req.Actor, Apply: false, RemoteAddr: req.RemoteAddr})
	if err != nil {
		return nil, fmt.Errorf("prune sweep: %w", err)
	}
	res := &PruneRunResult{CandidateCount: gc.CandidateCount, CandidateBytes: gc.CandidateBytes}
	raw, derr := json.Marshal(pruneRunDetail{
		CandidateCount: res.CandidateCount, CandidateBytes: res.CandidateBytes, Applied: false,
	})
	if derr != nil {
		s.log.WarnContext(ctx, "httpapi: prune audit detail marshal failed", "error", derr.Error())
		raw = []byte("{}")
	}
	s.audit.Record(ctx, audit.Event{
		Actor:      req.Actor,
		Action:     audit.ActionMaintenancePruneRun,
		RemoteAddr: req.RemoteAddr,
		Detail:     string(raw),
	})
	s.log.InfoContext(ctx, "httpapi: scheduled prune dry-run complete",
		"candidates", res.CandidateCount, "candidate_bytes", res.CandidateBytes)
	return res, nil
}
