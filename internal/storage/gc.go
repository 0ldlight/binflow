package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// DefaultGCGrace is the default grace period separating a blob's creation
// from its eligibility for collection. It exists to prevent a sweep from
// racing an in-flight upload whose metadata reference has not landed yet
// (blob-first ordering, architecture section 3.3).
const DefaultGCGrace = 24 * time.Hour

// gcResult carries the outcome of one mark-sweep pass.
type gcResult struct {
	Candidates []string // sha256 values eligible for deletion (always populated)
	Deleted    []string // sha256 values actually deleted (apply=true only)
}

// GC implements Engine.GC: mark-sweep over the blob directory.
//
//   - mark: referenced() yields the live sha256 set (nodes.sha256 in the
//     metadata store; storage itself never reads metadata).
//   - sweep: every on-disk blob that is unreferenced AND older than grace is
//     a candidate. apply=false (dry-run, the default posture) deletes
//     nothing and returns the candidate list; apply=true deletes and also
//     returns what it deleted.
//
// The returned []string for the Engine interface is the candidate list in
// dry-run and the deleted list in apply mode (ticket T-9 contract).
func (e *engine) GC(ctx context.Context, referenced func() (map[string]struct{}, error), grace time.Duration, apply bool) ([]string, error) {
	if referenced == nil {
		return nil, errors.New("storage: gc: referenced callback is nil")
	}
	if grace <= 0 {
		grace = DefaultGCGrace
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("storage: gc: %w", err)
	}
	if err := e.checkOpen(); err != nil {
		return nil, fmt.Errorf("storage: gc: %w", err)
	}

	refs, err := referenced()
	if err != nil {
		return nil, fmt.Errorf("storage: gc: referenced set: %w", err)
	}

	var res gcResult
	shardRoot := filepath.Join(e.root, blobsDirName)
	shards, err := os.ReadDir(shardRoot)
	if err != nil {
		return nil, fmt.Errorf("storage: gc: scan %s: %w", shardRoot, err)
	}

	now := e.opts.now()
	var firstErr error
	for _, shard := range shards {
		if err := ctx.Err(); err != nil {
			return res.Candidates, fmt.Errorf("storage: gc: %w", err)
		}
		if !shard.IsDir() || !validShardName(shard.Name()) {
			continue
		}
		blobs, err := os.ReadDir(filepath.Join(shardRoot, shard.Name()))
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("storage: gc: scan shard %s: %w", shard.Name(), err)
			}
			continue
		}
		for _, b := range blobs {
			sum := b.Name()
			if b.IsDir() || !validSha256(sum) || sum[:2] != shard.Name() {
				continue
			}
			if _, ok := refs[sum]; ok {
				continue // mark hit: keep
			}
			path := filepath.Join(shardRoot, shard.Name(), sum)
			info, err := b.Info()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("storage: gc: stat %s: %w", path, err)
				}
				continue
			}
			if now.Sub(info.ModTime()) <= grace {
				continue // inside grace window: skip
			}
			res.Candidates = append(res.Candidates, sum)
			if apply {
				if err := e.Delete(ctx, sum); err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("storage: gc: delete %s: %w", sum, err)
					}
					continue
				}
				res.Deleted = append(res.Deleted, sum)
			}
		}
	}
	sort.Strings(res.Candidates)
	sort.Strings(res.Deleted)
	if firstErr != nil {
		return res.Candidates, firstErr
	}
	if apply {
		return res.Deleted, nil
	}
	return res.Candidates, nil
}

// validShardName accepts exactly two lowercase hex characters, so the sweep
// never follows foreign directories into unexpected paths.
func validShardName(name string) bool {
	if len(name) != 2 {
		return false
	}
	for i := 0; i < 2; i++ {
		c := name[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
