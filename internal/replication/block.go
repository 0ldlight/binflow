package replication

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
)

// The global blockPush/blockPull emergency brake (M15 T-422, FR-138.3;
// replication.md §9.1-B/§9.2-B — the blocksystemreplication counterpart).
//
// Two independent direction flags, one runtime owner each instance: the
// BlockGate. Semantics per §9.2-B:
//
//   - 写入即持久 (B-2, high confidence): every REST flip persists as the
//     single replication_globals row and survives restarts. Boot adopts the
//     persisted row when present; a missing row is SEEDED once from the
//     binflow.yaml replication.block_push/block_pull keys and the row is
//     database-managed from then on — the K31 dual-source posture the auth
//     ConfigManager established (a later YAML edit draws no effect; the
//     REST face is the live surface).
//   - 生效面 (B-4): push blocked stops the event track (Enqueue appends no
//     task rows), the sweep/claim track (drain claims nothing; a task
//     cycling its retry loop is reverted to pending at the next attempt
//     boundary — an attempt literally in flight runs to its own conclusion,
//     the same posture the T-405 enabled flip took) and the manual trigger
//     (TriggerFullSync refuses, §9.2-A-5). Pull blocked stops every upstream
//     fetch of the remote pull-through plane (internal/remote consults the
//     gate through its own installed probe; cached copies still serve).
//   - Idempotent (B-5): re-writing the same state is a plain 200 with the
//     same message, no versioning.
//
// The REST face (httpapi) owns the block/unblock wordings verbatim; this
// type only owns state, persistence and the swap.

// ErrPushBlocked marks a manual full-sync trigger refused by the global
// push block (§9.2-A-5: the scheduling entry checks the brake first). The
// REST face maps it onto its refusal status; the anchored status text
// lives in the message.
var ErrPushBlocked = errors.New("replication: push replication is blocked, skipping replication")

// GlobalBlock is the two direction flags plus their bookkeeping. The zero
// value is the unblocked posture.
type GlobalBlock struct {
	BlockPush bool
	BlockPull bool
	// UpdatedAt is the RFC3339 UTC stamp of the last persist.
	UpdatedAt string
	// UpdatedBy names the actor of the last persist ("" = the boot seed).
	UpdatedBy string
}

// BlockKeeper persists the global block row (migration 019,
// replication_globals — one row, id=1). Consumer-side seam satisfied by
// *SQLiteStore; (nil, nil) from Get means "never written".
type BlockKeeper interface {
	GetGlobalBlock(ctx context.Context) (*GlobalBlock, error)
	PutGlobalBlock(ctx context.Context, b *GlobalBlock) error
}

// BlockGate is the runtime owner of the global block flags: one immutable
// snapshot behind an atomic pointer, every consumer (engine hooks, the REST
// face, the remote pull plane's probe) reads it lock-free. Safe for
// concurrent use; build with NewBlockGate, boot with Load, flip with
// Update.
type BlockGate struct {
	keeper BlockKeeper
	now    func() time.Time
	log    *slog.Logger
	// wake fires after every successful Update — cmd wires the push
	// engine's Kick so an UNBLOCK resumes the queue immediately instead of
	// waiting for the next sweep tick (blocking needs no wake: the next
	// drain pass parks itself).
	wake func()
	v    atomic.Pointer[GlobalBlock]
}

// NewBlockGate builds the gate around seed (the binflow.yaml carrier's
// resolved values — the fallback until Load finds or writes the row). nil
// keeper keeps the gate in-memory only (tests, degraded assemblies): Update
// then just swaps the snapshot. nil now means UTC time.Now; nil log means
// slog.Default().
func NewBlockGate(keeper BlockKeeper, seed GlobalBlock, now func() time.Time, log *slog.Logger) *BlockGate {
	g := &BlockGate{keeper: keeper, now: time.Now, log: slog.Default()}
	if now != nil {
		g.now = now
	}
	if log != nil {
		g.log = log
	}
	g.v.Store(&seed)
	return g
}

// Load resolves the boot state: a persisted row is authoritative; a missing
// row seeds the database ONCE from the gate's initial value (K31: the
// section is database-managed from then on, INFO named so an operator who
// later edits the YAML keys is not surprised). A nil keeper is a no-op.
func (g *BlockGate) Load(ctx context.Context) error {
	if g.keeper == nil {
		return nil
	}
	row, err := g.keeper.GetGlobalBlock(ctx)
	if err != nil {
		return fmt.Errorf("replication: load global block: %w", err)
	}
	if row != nil {
		g.v.Store(row)
		return nil
	}
	seed := g.v.Load()
	stored := &GlobalBlock{BlockPush: seed.BlockPush, BlockPull: seed.BlockPull,
		UpdatedAt: g.now().UTC().Format(time.RFC3339), UpdatedBy: ""}
	if err := g.keeper.PutGlobalBlock(ctx, stored); err != nil {
		return fmt.Errorf("replication: seed global block: %w", err)
	}
	g.v.Store(stored)
	g.log.Info("replication: global block state seeded from binflow.yaml and now REST-managed",
		"block_push", stored.BlockPush, "block_pull", stored.BlockPull)
	return nil
}

// Flags returns the current snapshot.
func (g *BlockGate) Flags() GlobalBlock { return *g.v.Load() }

// PushBlocked reports whether the push direction is blocked.
func (g *BlockGate) PushBlocked() bool { return g.v.Load().BlockPush }

// PullBlocked reports whether the pull direction is blocked. This is the
// method internal/remote's installed probe forwards to.
func (g *BlockGate) PullBlocked() bool { return g.v.Load().BlockPull }

// SetWake installs the post-update hook (cmd wiring: the engine's Kick).
func (g *BlockGate) SetWake(fn func()) { g.wake = fn }

// Update persists and swaps the flags (both directions in one write — the
// REST face computes which directions it touches and passes the resulting
// whole state). Returns the new snapshot. Idempotent by construction: the
// same values re-write the same row.
func (g *BlockGate) Update(ctx context.Context, push, pull bool, actor string) (GlobalBlock, error) {
	next := GlobalBlock{BlockPush: push, BlockPull: pull,
		UpdatedAt: g.now().UTC().Format(time.RFC3339), UpdatedBy: actor}
	if g.keeper != nil {
		if err := g.keeper.PutGlobalBlock(ctx, &next); err != nil {
			return g.Flags(), fmt.Errorf("replication: update global block: %w", err)
		}
	}
	g.v.Store(&next)
	if g.wake != nil {
		g.wake()
	}
	return next, nil
}
