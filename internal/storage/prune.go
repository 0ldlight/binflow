package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// The prune face (LOOP 003 / docs/reverse/storage/prune-gc-admin.md E2-E4):
// a per-shard-directory walk over the blob store with the SAME safety gates
// as GCSweep — the mark snapshot, the in-flight hold gate and the
// per-candidate Live recheck (ADR-0031) — extended with the per-directory
// counters, the stop marker and the resume point the PUD (Prune
// Unreferenced Data) status report is built from. It is not a second
// deletion path: every deletion routes through the engine's own Delete
// behind the same gcDeleteGate sequence GCSweep applies, and a dry run
// (Artifactory's dryRun:true estimate mode) never evaluates candidacy at
// all — processed counts, cleaned stays zero, per the live-instance
// evidence (spec §2.2 note on dry-run reports).

// PruneShardCount is the addressable shard space of the blob store
// (00..ff). The prune walk iterates ALL of it — the progress denominator
// is 256 even when the directory does not exist on disk yet, mirroring the
// FilestorePruner iteration the spec records ("任务后台遍历 00..ff 全部
// 256 个分片目录").
const PruneShardCount = 256

// PruneOptions is one prune pass's request.
type PruneOptions struct {
	// Marker is the same two-method reference oracle GCSweep consumes.
	// Mark runs once per pass; Live runs per deletion candidate.
	Marker GCMarker
	// Grace is the mtime window protecting a blob from collection.
	// Zero or negative means DefaultGCGrace (the engine contract; the
	// REST face passes the configured storage.gc_grace_hours).
	Grace time.Duration
	// Apply deletes; false is the estimate mode (count only).
	Apply bool
	// StartFrom resumes the walk at a shard name ("00".."ff"); "" starts
	// at 00. Directories before it are enumerated (the progress
	// denominator stays 256, the live instance's observed shape) but not
	// processed.
	StartFrom string
	// Stop is consulted before every directory; a true answer ends the
	// pass as stopped. May be nil.
	Stop func() bool
	// Observe, when set, receives one PruneDirStats per enumerated
	// directory — skipped ones included — after the directory settles.
	Observe func(PruneDirStats)
}

// PruneDirStats is one shard directory's slice of the report.
type PruneDirStats struct {
	// Name is the two-hex shard name.
	Name string
	// Index is the 1-based position inside the 00..ff enumeration — the
	// progress numerator.
	Index int
	// Skipped marks a directory the StartFrom resume point jumped over
	// (zero counters, never a deletion candidate).
	Skipped bool
	// BinariesProcessed counts every valid blob file examined.
	BinariesProcessed int64
	// BinariesCleaned counts blobs deleted (apply mode only).
	BinariesCleaned int64
	// BytesCleaned sums the deleted blobs' sizes (apply mode only).
	BytesCleaned int64
	// StartedAt/FinishedAt bound the directory's processing.
	StartedAt  time.Time
	FinishedAt time.Time
}

// PruneTotals is the pass's accumulated report block.
type PruneTotals struct {
	BinariesProcessed int64
	BinariesCleaned   int64
	BytesCleaned      int64
}

// PruneOutcome is one completed (or stopped) pass. Err semantics follow
// GCSweep: the first error is returned alongside the outcome; a partial
// pass's counters still describe what it did.
type PruneOutcome struct {
	// Stopped reports a Stop() answer ended the walk early. The last
	// observed directory is the stop landing point (the report renders it
	// with status "stopped", the live instance's terminal shape).
	Stopped bool
	// Totals accumulates the observed directories' counters.
	Totals PruneTotals
	// LastDir is the last processed directory (zero when none ran).
	LastDir PruneDirStats
	// Deleted lists the shas the pass deleted (apply mode only) — the
	// blobs-ledger teardown list the REST face consumes.
	Deleted []string
}

// Pruner is the optional Engine capability behind the PUD admin plane
// (the MultipartUploads/SessionSweeper discovery pattern: asserted on the
// wired engine, absent → the endpoints answer their honest 503). The disk
// engine implements it; the walk reuses the GCSweep gates, so prune and
// gc share one deletion discipline.
type Pruner interface {
	Prune(ctx context.Context, opts PruneOptions) (*PruneOutcome, error)
}

// Prune implements Pruner: enumerate 00..ff, and for each present shard
// directory apply the GCSweep candidacy rules (mark hit → hold gate →
// grace window → delete gates). The stop marker is consumed between
// directories — a stop lands within one directory, the granularity the
// live instance showed ("stop 后任务在 ~1 个目录内停下").
func (e *engine) Prune(ctx context.Context, opts PruneOptions) (*PruneOutcome, error) {
	if opts.Marker == nil {
		return nil, errors.New("storage: prune: marker is nil")
	}
	if opts.StartFrom != "" && !validShardName(opts.StartFrom) {
		return nil, fmt.Errorf("storage: prune: startFromDirectory %q is not a two-hex shard name", opts.StartFrom)
	}
	grace := opts.Grace
	if grace <= 0 {
		grace = DefaultGCGrace
	}
	if err := e.checkOpen(); err != nil {
		return nil, fmt.Errorf("storage: prune: %w", err)
	}

	refs, err := opts.Marker.Mark()
	if err != nil {
		return nil, fmt.Errorf("storage: prune: referenced set: %w", err)
	}
	var gate *gcDeleteGate
	if opts.Apply {
		gate = &gcDeleteGate{holds: e.holds, recheck: newGCRecheck(opts.Marker)}
	}

	out := &PruneOutcome{}
	shardRoot := filepath.Join(e.root, blobsDirName)
	var firstErr error
	keepErr := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for i := 0; i < PruneShardCount; i++ {
		name := fmt.Sprintf("%02x", i)
		if err := ctx.Err(); err != nil {
			return out, fmt.Errorf("storage: prune: %w", err)
		}
		if opts.Stop != nil && opts.Stop() {
			out.Stopped = true
			break
		}
		if opts.StartFrom != "" && name < opts.StartFrom {
			// Resume point: enumerated (progress advances), not
			// processed (spec §3.2-5 — the denominator stays 256).
			stats := PruneDirStats{Name: name, Index: i + 1, Skipped: true}
			if opts.Observe != nil {
				opts.Observe(stats)
			}
			continue
		}
		stats, serr := e.pruneShard(ctx, shardRoot, name, refs, gate, grace, opts.Apply, out)
		stats.Name, stats.Index = name, i+1
		keepErr(serr)
		if opts.Observe != nil {
			opts.Observe(stats)
		}
		out.LastDir = stats
	}
	if firstErr != nil {
		return out, fmt.Errorf("storage: prune: %w", firstErr)
	}
	return out, nil
}

// pruneShard processes one shard directory: every valid blob file counts
// as processed; candidacy follows GCSweep (mark hit / hold / grace), and
// apply-mode deletions pass the same hold→Live delete gate. Estimate mode
// (apply=false) never evaluates candidacy — the spec's dry-run report has
// processed counts and cleaned=0. The returned error is the shard's first
// fault (unreadable directory, uncertain Live answer, failed delete); the
// walk continues past it and the caller reports it as the pass's error.
func (e *engine) pruneShard(ctx context.Context, shardRoot, name string, refs map[string]struct{}, gate *gcDeleteGate, grace time.Duration, apply bool, out *PruneOutcome) (stats PruneDirStats, firstErr error) {
	stats.StartedAt = e.opts.now()
	defer func() { stats.FinishedAt = e.opts.now() }() // named return: the defer must land on the returned value

	dir := filepath.Join(shardRoot, name)
	blobs, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return stats, nil // absent shard: enumerated, empty
		}
		return stats, fmt.Errorf("scan shard %s: %w", name, err)
	}
	now := e.opts.now()
	for _, b := range blobs {
		sum := b.Name()
		if b.IsDir() || !validSha256(sum) || sum[:2] != name {
			continue
		}
		stats.BinariesProcessed++
		out.Totals.BinariesProcessed++
		if !apply {
			continue // estimate mode: counting only
		}
		if _, ok := refs[sum]; ok {
			continue
		}
		if e.holds.held(sum) {
			continue
		}
		info, err := b.Info()
		if err != nil {
			continue // vanished mid-walk: not a candidate
		}
		if now.Sub(info.ModTime()) <= grace {
			continue
		}
		skip, err := gate.skip(sum)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("live recheck %s: %w", sum, err)
			}
			continue // conservative: no deletion on an uncertain answer
		}
		if skip {
			continue
		}
		if err := e.Delete(ctx, sum); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("delete %s: %w", sum, err)
			}
			continue
		}
		stats.BinariesCleaned++
		stats.BytesCleaned += info.Size()
		out.Totals.BinariesCleaned++
		out.Totals.BytesCleaned += info.Size()
		out.Deleted = append(out.Deleted, sum)
	}
	return stats, firstErr
}
