package maven

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The copy-side index recompute entry (M12 T-354, the §15.4.2 leftover seam
// / architecture §11.44): repo's copy/move pipeline fires the
// CopyMoveObserver after every completed non-dry COPY, handing the target
// repository and the candidate directory set — the deduplicated, sorted
// parent directories (trailing-slash spellings) of every landed FILE node,
// repo-operations.md section 1.4's own trigger shape. The cmd assembly
// dispatches by the target repository's package type onto this entry,
// running it as repo.SystemPrincipal() (the trash chain's internal
// identity; the identity IS the exemption, and the D1 server-internal
// license posture holds — the gate is the HTTP verb face's, an in-process
// recompute never re-asks the question).
//
// The narrow interface is one method on purpose: the observer's consumer
// (cmd) knows nothing about maven coordinates, and this package knows
// nothing about the observer contract beyond (principal, repo, dirs).

// ReindexDirs recomputes maven-metadata.xml for the candidate directory
// set. Every candidate dir is the parent of a landed file — under
// maven-2-default that is a VERSION directory (<orgPath>/<module>/<version>),
// so each dir fires both recalculation arms the deploy-side taxonomy
// (calc.go, ME-04) names for a landed artifact:
//
//   - the version directory itself (the SNAPSHOT document generator; a
//     release directory is the cheap no-pom cleanup pass), and
//   - the parent MODULE directory (the version-group document — the list a
//     copied-in version must join), deduplicated across the set.
//
// Directories that cannot spell a version directory (fewer than three
// segments — a file copied to the repository root or directly under a
// one-segment folder) are skipped with one log line: no coordinate can be
// derived from them, and guessing one would write metadata for a layout
// the repository does not carry. Synchronous on the caller's goroutine —
// the observer already runs the entry off the request path, and the
// calculator's process-wide execution lock serializes concurrent runs.
func (h *Handler) ReindexDirs(ctx context.Context, p *repo.Principal, repoKey string, dirs []string) error {
	if h == nil || h.calc == nil {
		// The calculator-less assembly (nodes seam absent): nothing to
		// recompute — the same posture the deploy path takes.
		return nil
	}
	var errs []error
	modules := map[trigger]bool{}
	moduleOrder := []trigger{}
	for _, dir := range dirs {
		segs := strings.Split(strings.TrimSuffix(dir, "/"), "/")
		if len(segs) < 3 || segs[0] == "" {
			slog.DebugContext(ctx, "maven: reindex skipped a non-coordinate directory",
				slog.String("repo", repoKey), slog.String("dir", dir))
			continue
		}
		org := dotJoin(segs[:len(segs)-2])
		t := trigger{repoKey: repoKey, orgPath: org, module: segs[len(segs)-2], version: segs[len(segs)-1]}
		if err := h.calc.recalc(ctx, p, t); err != nil {
			errs = append(errs, fmt.Errorf("version dir %s: %w", t.dirPath(), err))
		}
		mod := trigger{repoKey: repoKey, orgPath: org, module: t.module}
		if !modules[mod] {
			modules[mod] = true
			moduleOrder = append(moduleOrder, mod)
		}
	}
	for _, mod := range moduleOrder {
		if err := h.calc.recalc(ctx, p, mod); err != nil {
			errs = append(errs, fmt.Errorf("module dir %s: %w", mod.dirPath(), err))
		}
	}
	return errors.Join(errs...)
}
