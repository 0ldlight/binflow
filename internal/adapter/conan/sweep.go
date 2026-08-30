package conan

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The ADR-0042 startup sweep (T-371, FR-119.2): re-home the D-F2 legacy
// trees — the v1 files-channel package files the pre-T-371 channelFileName
// bug landed double-spelled — onto the spec section 4 layout. The path-shape
// knowledge (the predicate) lives HERE, in the adapter package; the rewrite
// itself goes through repo.Service's one narrow primitive,
// RewriteSubtreePrefix, as the single seam (ADR-0042 decision 3).

// SweepV1FilesLayout walks every LOCAL conan repository and rewrites each
// D-F2 double-spelled package subtree onto the spec layout. It is a BOOT
// seam: the caller (cmd assembly) runs it before the HTTP listener goes up,
// so a half-applied tree is structurally unobservable and an interrupted
// pass resumes by predicate on the next boot — the rewrite is
// predicate-consuming, a second run finds nothing and changes nothing
// (moved=0). An instance without conan repositories returns after the
// manifest filter having scanned nothing (the cold-start no-op, FR-119
// AC4); a repository without legacy trees costs one listing. Any failure
// fails the boot: with the write path fixed, an unswept legacy tree would
// answer the channel GET's spec-shaped address with a 404, so serving
// before the sweep completes is the worse failure mode.
//
// The per-repository line (ADR-0042 decision 4's pinned shape) rides the
// logger at INFO, upgrading to WARN when any conflict was resolved — the
// production reconciliation gate is conflicts=0.
func SweepV1FilesLayout(ctx context.Context, svc repo.Service, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	// The system identity (the trash-capture posture): an internal,
	// boot-time operation rides the admin exemption, never a user grant.
	rows, err := svc.ListRepos(ctx, repo.SystemPrincipal())
	if err != nil {
		return fmt.Errorf("conan v1 layout sweep: list repositories: %w", err)
	}
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Type == repo.TypeLocal && row.PackageType == Protocol {
			keys = append(keys, row.RepoKey)
		}
	}
	if len(keys) == 0 {
		return nil // zero conan repositories: zero scans, zero cost
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := sweepRepoV1Layout(ctx, svc, key, log); err != nil {
			return err
		}
	}
	return nil
}

// sweepRepoV1Layout sweeps one repository: one whole-repo listing, the
// double-layout predicate applied in Go (O(nodes)), then one primitive call
// per distinct legacy prefix.
func sweepRepoV1Layout(ctx context.Context, svc repo.Service, repoKey string, log *slog.Logger) error {
	nodes, err := svc.List(ctx, repo.SystemPrincipal(), repoKey, "")
	if err != nil {
		return fmt.Errorf("conan v1 layout sweep: scan %s: %w", repoKey, err)
	}
	prefixes := map[string]string{} // src double prefix -> dst spec prefix
	for _, n := range nodes {
		src, dst, ok := doubleLayoutPrefix(n.Path)
		if ok {
			prefixes[src] = dst
		}
	}
	srcs := make([]string, 0, len(prefixes))
	for src := range prefixes {
		srcs = append(srcs, src)
	}
	sort.Strings(srcs)

	var moved, dedup, conflicts int
	for _, src := range srcs {
		stats, err := svc.RewriteSubtreePrefix(ctx, repoKey, src, prefixes[src])
		if err != nil {
			return fmt.Errorf("conan v1 layout sweep: rewrite %s/%s: %w", repoKey, src, err)
		}
		moved += stats.Moved
		dedup += stats.Deduped
		conflicts += len(stats.Conflicts)
		for _, c := range stats.Conflicts {
			log.Warn("conan v1 layout sweep: sha256 conflict resolved newer-wins (loser content stays in the blob store)",
				"repo", repoKey, "path", c.Path, "kept", c.Kept, "dropped", c.Dropped)
		}
	}
	line := fmt.Sprintf("conan v1 layout sweep: repo=%s moved=%d dedup=%d conflicts=%d",
		repoKey, moved, dedup, conflicts)
	if conflicts > 0 {
		log.Warn(line)
	} else {
		log.Info(line)
	}
	return nil
}

// doubleLayoutPrefix reports the D-F2 mapping of one node path: the source
// double prefix and its spec target when the path matches the legacy shape
//
//	<coordinateRoot>/0/package/<pid>/0/package/<pid>/<tail>
//	  -> <coordinateRoot>/0/package/<pid>/0/<tail>
//
// (coordinateRoot = user/name/version/channel; both revision segments are
// the v1 channel's literal default `0` and the packageId segment repeats).
// False positives are structurally barred (ADR-0042 decision 2): the v2
// plane's revision segments are hashes or VCS strings, never the literal
// `0`, and the triple literal constraint (two `/0/package/` separators plus
// the repeated pid) admits nothing else the planes write.
//
// Folder rows carry their trailing slash; a FILE ending exactly at the
// repeated pid segment is a legal nested tail (…/<pRev>/package/<pid> as a
// file name), not the double folder — only the folder-row spelling (the
// path with its trailing slash, ten segments) maps with an empty tail.
func doubleLayoutPrefix(path string) (src, dst string, ok bool) {
	segs := strings.Split(strings.TrimSuffix(path, "/"), "/")
	if len(segs) < 10 {
		return "", "", false
	}
	if _, err := parseRef(segs[1], segs[2], segs[0], segs[3]); err != nil {
		return "", "", false
	}
	pid := segs[6]
	if segs[4] != revDefaultV1 || segs[5] != dirPackage || !validPackageID(pid) ||
		segs[7] != revDefaultV1 || segs[8] != dirPackage || segs[9] != pid {
		return "", "", false
	}
	if len(segs) == 10 && !strings.HasSuffix(path, "/") {
		return "", "", false // the nested-tail file, not the double folder
	}
	return strings.Join(segs[:10], "/") + "/", strings.Join(segs[:8], "/") + "/", true
}
