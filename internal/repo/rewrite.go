package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The ADR-0042 boot-sweep primitive (T-371): a subtree prefix rewrite over
// node ROWS only. Physical content is checksum-addressed (ADR-0006), so a
// logical-path move is a metadata rewrite — the blob store is never opened,
// and every moved row keeps its sha256 (the "同 blob link 平移" contract
// point). The conan v1 files-channel layout sweep (internal/adapter/conan
// sweep.go) is the one caller; the path-shape knowledge of WHICH subtrees to
// rewrite lives there, this method only executes one prefix mapping.

// normalizeRewritePrefix validates and canonicalizes one rewrite prefix to
// its trailing-slash folder form ("a/b" and "a/b/" both become "a/b/").
func normalizeRewritePrefix(prefix, field string) (string, error) {
	if prefix == "" {
		return "", fmt.Errorf("%w: %s is empty", ErrInvalidPath, field)
	}
	canonical := strings.TrimSuffix(prefix, "/")
	if canonical == "" {
		return "", fmt.Errorf("%w: %s is the repository root", ErrInvalidPath, field)
	}
	if err := validateNodePath(canonical + "/"); err != nil {
		return "", err
	}
	return canonical + "/", nil
}

// RewriteSubtreePrefix implements Service.RewriteSubtreePrefix: every node
// row under srcPrefix (files and folder rows alike) is re-homed onto the
// corresponding path under dstPrefix. Rows are processed in the store's
// path order; each landing materializes the target's ancestor folder rows
// first (idempotent — in the ADR-0042 shape dstPrefix's ancestors already
// exist, being ancestors of the source too), and the source row is deleted
// in the same iteration. Emptied source folder rows need no special case:
// they are themselves rows under srcPrefix and are rewritten (or merged by
// the same-sha dedup arm) like everything else, so a completed rewrite
// leaves NOTHING under srcPrefix — the predicate-consuming idempotency the
// ADR's "二跑零改" rests on.
//
// Deliberate omissions (the ADR's zero-side-effect contract): no permission
// gate (system state), no audit row, no webhook emission, no copy/move
// observer, no trash capture, no replication enqueue, no GC-hold traffic —
// nothing the user-plane Put/Delete tails carry. Node PROPERTIES do not
// ride either: the conan package trees this was built for carry none, and a
// caller needing property migration must say so on its own face.
func (s *service) RewriteSubtreePrefix(ctx context.Context, repoKey, srcPrefix, dstPrefix string) (*SubtreeRewrite, error) {
	src, err := normalizeRewritePrefix(srcPrefix, "srcPrefix")
	if err != nil {
		return nil, err
	}
	dst, err := normalizeRewritePrefix(dstPrefix, "dstPrefix")
	if err != nil {
		return nil, err
	}
	if src == dst {
		return nil, fmt.Errorf("%w: rewrite source and target are the same prefix %q", ErrInvalidPath, src)
	}
	if strings.HasPrefix(dst, src) {
		return nil, fmt.Errorf("%w: target prefix %q is inside the source prefix %q", ErrInvalidPath, dst, src)
	}
	row, err := s.loadRepoRow(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	if row.Type != TypeLocal {
		return nil, fmt.Errorf("%w: %s repositories are not served by the local content plane", ErrRepoTypeNotSupported, row.Type)
	}

	// The slash-stripped probe matches the exact folder row plus the whole
	// subtree (the "d//%" dead arm the store's prefix queries document).
	rows, err := s.md.Nodes().ListByPrefix(ctx, repoKey, strings.TrimSuffix(src, "/"))
	if err != nil {
		return nil, fmt.Errorf("rewrite list %s/%s: %w", repoKey, src, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("rewrite %s/%s: %w", repoKey, src, ErrNodeNotFound)
	}

	stats := &SubtreeRewrite{}
	now := s.now()
	for _, n := range rows {
		// A same-named FILE beside the folder ("a/b" next to "a/b/") is not
		// subtree content — the directory move must spare it.
		if n.Path != src && !strings.HasPrefix(n.Path, src) {
			continue
		}
		target := dst + strings.TrimPrefix(n.Path, src)
		existing, err := s.md.Nodes().Get(ctx, repoKey, target)
		if err != nil && !errors.Is(err, metadata.ErrNodeNotFound) {
			return nil, fmt.Errorf("rewrite probe %s/%s: %w", repoKey, target, err)
		}
		switch {
		case errors.Is(err, metadata.ErrNodeNotFound):
			// Fresh landing: ancestors first (ADR-0016, idempotent here),
			// then the row itself with every stored field carried verbatim
			// except the path.
			if err := s.materializeAncestors(ctx, SystemPrincipal(), repoKey, target); err != nil {
				return nil, err
			}
			moved := *n
			moved.RepoKey = repoKey
			moved.Path = target
			if err := s.md.Usage().PutNodeWithUsage(ctx, &moved, now); err != nil {
				return nil, fmt.Errorf("rewrite write %s/%s: %w", repoKey, target, err)
			}
			stats.Moved++
		case existing.Sha256 == n.Sha256:
			// Same content already at the target: the retransmit-idempotent
			// form. The target row stays (its provenance is the earlier
			// write's), the source row simply goes.
			stats.Deduped++
		default:
			// Differing sha256 at the target: the NEWER row (UpdatedAt)
			// resides at the target; the loser's content stays in the blob
			// store, recoverable within GC's grace. Reported, never silent.
			kept, dropped := existing.Sha256, n.Sha256
			if n.UpdatedAt > existing.UpdatedAt {
				kept, dropped = n.Sha256, existing.Sha256
				if err := s.materializeAncestors(ctx, SystemPrincipal(), repoKey, target); err != nil {
					return nil, err
				}
				winner := *n
				winner.RepoKey = repoKey
				winner.Path = target
				if err := s.md.Usage().PutNodeWithUsage(ctx, &winner, now); err != nil {
					return nil, fmt.Errorf("rewrite write %s/%s: %w", repoKey, target, err)
				}
			}
			stats.Conflicts = append(stats.Conflicts, RewriteConflict{
				Path: target, Kept: kept, Dropped: dropped,
			})
		}
		// The source row goes in every arm (a concurrent delete already
		// took it: the move's outcome is unchanged).
		if err := s.md.Usage().DeleteNodeWithUsage(ctx, repoKey, n.Path, now); err != nil &&
			!errors.Is(err, metadata.ErrNodeNotFound) {
			return nil, fmt.Errorf("rewrite delete %s/%s: %w", repoKey, n.Path, err)
		}
	}

	// Emptied intermediate folder rows go with the migration (ADR-0042
	// decision 2: "清空后的源文件夹行随迁删除"): when dstPrefix is an
	// ancestor of srcPrefix, the folder rows STRICTLY between them are the
	// double layout's own scaffolding — drop each that no longer carries
	// anything. Walked deepest-first so a fully-drained chain collapses in
	// one pass; dstPrefix itself is never a candidate (it holds the moved
	// content), and a folder with a surviving child (a same-named file
	// counts) is kept — the safe direction.
	for _, folder := range intermediateFolders(dst, src) {
		children, err := s.md.Nodes().ListByPrefix(ctx, repoKey, strings.TrimSuffix(folder, "/"))
		if err != nil {
			return nil, fmt.Errorf("rewrite prune list %s/%s: %w", repoKey, folder, err)
		}
		if !onlyOwnRow(children, folder) {
			continue // a live child remains: keep the scaffolding
		}
		if err := s.md.Usage().DeleteNodeWithUsage(ctx, repoKey, folder, now); err != nil &&
			!errors.Is(err, metadata.ErrNodeNotFound) {
			return nil, fmt.Errorf("rewrite prune %s/%s: %w", repoKey, folder, err)
		}
	}
	return stats, nil
}

// onlyOwnRow reports whether the listing holds nothing but folder's own row.
func onlyOwnRow(nodes []*metadata.Node, folder string) bool {
	for _, n := range nodes {
		if n.Path != folder {
			return false
		}
	}
	return true
}

// intermediateFolders lists the folder paths strictly between dst and src
// (dst exclusive, src exclusive — src's own row was rewritten above),
// deepest first. Empty when src does not sit under dst.
func intermediateFolders(dst, src string) []string {
	if !strings.HasPrefix(src, dst) {
		return nil
	}
	var out []string
	for p := parentPrefix(src); strings.HasPrefix(p, dst) && p != dst; p = parentPrefix(p) {
		out = append(out, p)
	}
	return out
}
