// The build-run deletion face (L023-2A, FR-152.2 / build-info.md §11.7 —
// D07-R04's DELETE arm): DELETE /api/build/{buildName}?buildNumbers=…
// with the E6 verbatim wording (have, the Warning segment, the trailing
// newline) and the two-branch 404 law. The batch POST twin (6.13+ body
// form) is ticket C's surface — same semantics, another door.

package build

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// ErrBuildNumbersNotFound marks the all-missing arm of the numbers
// deletion: the name exists but NONE of the requested numbers did — the
// distinct 404 from a missing name (build-info.md §11.7 step 3).
var ErrBuildNumbersNotFound = fmt.Errorf("build: none of the given build numbers exist: %w", metadata.ErrBuildNotFound)

// DeleteResult reports one deletion call's outcome: the deleted runs as
// "<name>#<number>" entries, the numbers no run answered, and the
// resolved build_repo the wording echoes.
type DeleteResult struct {
	Deleted []string
	Missing []string
	Repo    string
}

// DeleteRuns is DELETE /api/build/{buildName}: deleteAll drops every run
// of the name; otherwise each requested number deletes AT MOST ONE run —
// its newest-started (§11.7 step 3's quirk: a same-number multi-run
// stack needs repeated calls, number-first ordering from the newest-run
// snapshot). The d(buildRepo, buildName) gate runs before any lookup (no
// existence oracle for the denied caller).
func (s *Service) DeleteRuns(ctx context.Context, p *Principal, name, buildRepo string, numbers []string, deleteAll, deleteArtifacts bool) (*DeleteResult, error) {
	c := Coordinate{Name: name, Repo: buildRepo}.Resolve()
	if err := ValidateBuildName(c.Name); err != nil {
		return nil, err
	}
	if hasControl(c.Repo) {
		return nil, fmt.Errorf("build repo %q contains control characters: %w", c.Repo, ErrInvalidCoordinate)
	}
	if !s.allow(ctx, p, c.Repo, c.Name, auth.ActionDelete) {
		return nil, fmt.Errorf("delete build %s: %w", c.Name, forbiddenf(p, "delete", "Delete"))
	}

	rows, err := s.store.ListBuildNumbers(ctx, c.Name, c.Repo)
	if err != nil {
		return nil, fmt.Errorf("delete build %s runs read: %w", c.Name, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("delete build %s: %w", c.Name, metadata.ErrBuildNotFound)
	}

	out := &DeleteResult{Repo: c.Repo}
	actor := actorOf(p)
	if deleteAll {
		for _, run := range rows {
			if err := s.deleteRun(ctx, p, actor, c.Name, run.Number, run.Started, c.Repo, deleteArtifacts); err != nil {
				return out, err
			}
			out.Deleted = append(out.Deleted, c.Name+"#"+run.Number)
		}
		return out, nil
	}

	// rows are newest-first globally, so the FIRST row of each number is
	// that number's newest run — the one this call deletes.
	newest := make(map[string]string, len(rows))
	for _, run := range rows {
		if _, ok := newest[run.Number]; !ok {
			newest[run.Number] = run.Started
		}
	}
	seen := make(map[string]bool, len(numbers))
	for _, number := range numbers {
		if seen[number] {
			continue // a repeated CSV entry collapses (the pending-set read)
		}
		seen[number] = true
		started, ok := newest[number]
		if !ok {
			out.Missing = append(out.Missing, number)
			continue
		}
		if err := s.deleteRun(ctx, p, actor, c.Name, number, started, c.Repo, deleteArtifacts); err != nil {
			return out, err
		}
		out.Deleted = append(out.Deleted, c.Name+"#"+number)
	}
	if len(out.Deleted) == 0 {
		return out, fmt.Errorf("delete build %s numbers: %w", c.Name, ErrBuildNumbersNotFound)
	}
	return out, nil
}

// deleteRun removes one run: the associated artifact nodes first
// (best-effort, one slog row per failure — the retention posture §11.6
// step 7, the run's own deletion never blocks on an artifact), then the
// run row and its cascade, then the build.delete audit row and the
// webhook deleted event at the same address (T-510's law).
func (s *Service) deleteRun(ctx context.Context, p *Principal, actor, name, number, started, buildRepo string, deleteArtifacts bool) error {
	if deleteArtifacts && s.carrier != nil {
		modules, err := s.store.ListModules(ctx, name, number, started, buildRepo)
		if err != nil {
			return fmt.Errorf("delete build %s#%s modules read: %w", name, number, err)
		}
		for _, m := range modules {
			for _, a := range m.Artifacts {
				if a.RepoKey == "" || a.Path == "" {
					continue
				}
				if err := s.carrier.Delete(ctx, p, a.RepoKey, a.Path); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
					slog.WarnContext(ctx, "build: delete artifact failed",
						"build", name+"#"+number, "node", a.RepoKey+"/"+a.Path, "error", err.Error())
				}
			}
		}
	}
	if err := s.store.DeleteBuild(ctx, name, number, started, buildRepo); err != nil {
		return fmt.Errorf("delete build %s#%s: %w", name, number, err)
	}
	s.recordAudit(ctx, audit.Event{
		Actor: actor, Action: audit.ActionBuildDelete,
		Repo: buildRepo, Path: name,
		Detail: fmt.Sprintf(`{"number":%q,"started":%q,"artifacts":%t}`,
			number, started, deleteArtifacts),
	})
	s.emitWebhook(ctx, WebhookEvent{
		Type: EventDeleted, Name: name, Number: number,
		Started: started, Repo: buildRepo, Principal: p,
	})
	return nil
}
