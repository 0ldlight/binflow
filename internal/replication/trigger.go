package replication

import (
	"context"
	"errors"
	"fmt"
)

// The manual full-sync trigger (M15 T-420, FR-138.1; replication.md §9.2-A,
// the executereplicationnow counterpart). The REST face's engine side is ONE
// method: seed the config's pending ledger rows for every FILE node of the
// source repository — "对该配置种一次全量对账" (§9.6's landing note) — then
// wake the existing worker. There is deliberately no second executor, no new
// queue and no engine rework: the tasks the trigger appends are the same
// ReplicationTask rows the event hook (Enqueue) appends and the Run loop
// (wake + the 1m sweep) drains, so retry/backoff/revive, the protocol push
// planes, the property carry and the 009 ledger's observability faces all
// apply unchanged (the ADR-0041 outbox/queue pattern this engine already IS).
//
// Scheduling is async (§9.2-A-3): the method returns once the rows are
// appended, never waiting for a push. Repeated triggers do NOT merge or
// dedup (§9.2-A-7, medium confidence): a second trigger appends a second
// pass whose pushes converge through the target-side sha256 idempotent-hit
// (the same no-dedup posture as Enqueue, ADR-0021 note 28).

// ErrTriggerDisabled marks a run refused because the config's enabled bit is
// down. The drain skips disabled configs, so scheduling them would only park
// dead rows — the REST face maps this onto its refusal status.
var ErrTriggerDisabled = errors.New("replication: config is disabled")

// ErrNoMetaSeam marks a run refused because the engine was assembled without
// the MetaSource seam (no source-repository enumeration). The production
// wiring always carries it (cmd); a bare assembly answers the honest error
// instead of pretending an empty repository.
var ErrNoMetaSeam = errors.New("replication: source metadata seam is not wired")

// maxFullSyncProbe clamps the enumeration LIMIT's cap+1 arithmetic: a
// max_items_per_push at or above this bound is treated as unlimited, keeping
// the int conversion from overflowing on a hand-edited row.
const maxFullSyncProbe = int64(1) << 40

// FullSyncResult reports what one trigger seeded.
type FullSyncResult struct {
	// Scheduled is the number of pending task rows appended (0 for a
	// repository without artifacts — a valid, successful run).
	Scheduled int64
	// Capped reports that max_items_per_push cut the enumeration short: more
	// file nodes exist than the config's per-trigger item limit; a second
	// trigger picks up the next slice (path-ordered, deterministic).
	Capped bool
}

// TriggerFullSync seeds one full reconciliation of cfg (FR-138.1): every FILE
// node of cfg.SourceRepo becomes one pending ReplicationTask, then the worker
// wakes. The method is safe for concurrent use with the worker loop — it only
// reads immutable engine state and writes through the store. The enumeration
// honors cfg.MaxItemsPerPush (the 009 column's documented "per-trigger item
// limit"): cap > 0 asks the seam for cap+1 rows (the K63 cap+1 probe shape)
// and truncates to the cap, reporting Capped when the probe came back full.
func (e *Engine) TriggerFullSync(ctx context.Context, cfg *ReplicationConfig) (*FullSyncResult, error) {
	if cfg == nil {
		return nil, errors.New("replication: TriggerFullSync: config is nil")
	}
	if !cfg.Enabled {
		return nil, fmt.Errorf("%w: %s: a disabled config's tasks are never claimed; flip enabled first", ErrTriggerDisabled, cfg.Name)
	}
	// The global push block gates the scheduling entry (T-422, §9.2-A-5):
	// a blocked trigger fails immediately — it does not queue behind the
	// brake ("封锁与触发同门").
	if e.pushBlocked() {
		return nil, fmt.Errorf("%w: config %s; unblock push replication first", ErrPushBlocked, cfg.Name)
	}
	if e.cfg.meta == nil {
		return nil, fmt.Errorf("%w: full sync of %s cannot enumerate the source repository", ErrNoMetaSeam, cfg.Name)
	}

	limit, capped := 0, false // 0 = unbounded
	if cfg.MaxItemsPerPush > 0 && cfg.MaxItemsPerPush < maxFullSyncProbe {
		limit = int(cfg.MaxItemsPerPush) + 1
	}
	files, err := e.cfg.meta.RepoFiles(ctx, cfg.SourceRepo, limit)
	if err != nil {
		return nil, fmt.Errorf("replication: full sync of %s: %w", cfg.Name, err)
	}
	if limit > 0 && int64(len(files)) > cfg.MaxItemsPerPush {
		files = files[:cfg.MaxItemsPerPush]
		capped = true
	}

	// One clock stamp for the whole pass: every row of one trigger sorts as
	// a contiguous, chronological block in the ledger (the drain orders by
	// created_at, then id — the seeding order survives).
	stamp := e.nowStamp()
	var created int64
	for _, f := range files {
		if f.Path == "" || f.Sha256 == "" {
			continue // the seam's contract excludes these; stayed defensive
		}
		if _, err := e.store.CreateTask(ctx, &ReplicationTask{
			ReplicationID: cfg.ID,
			BlobSHA256:    f.Sha256,
			NodePath:      f.Path,
			Status:        TaskStatusPending,
			CreatedAt:     stamp,
		}); err != nil {
			err = fmt.Errorf("replication: full sync of %s: task %s: %w", cfg.Name, f.Path, err)
			if created > 0 {
				// A partial pass is still a pass: wake the worker for the
				// rows that landed; the error names the first miss.
				e.signal()
			}
			return nil, err
		}
		created++
	}
	if created > 0 {
		e.signal()
	}
	return &FullSyncResult{Scheduled: created, Capped: capped}, nil
}
