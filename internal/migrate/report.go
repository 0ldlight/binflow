package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultReportPath is where the run report lands unless the operator
// redirects it (PRD FR-63 flow step 8 / MG-02).
const DefaultReportPath = "migration_report.json"

// MergeSemanticsNote is recorded (summary + report) whenever the operator
// overrides the empty-target guard: it states exactly what merging into a
// populated target does and does not touch.
const MergeSemanticsNote = "merge semantics: repositories with keys matching a source repository are reconfigured (create-or-replace) and artifacts at identical paths are overwritten; target content the source does not name is left untouched"

// reportGuard is the guard section of the report file.
type reportGuard struct {
	Checked        bool   `json:"checked"`
	TargetNonEmpty bool   `json:"target_non_empty"`
	Detail         string `json:"detail,omitempty"`
	Refused        bool   `json:"refused"`
	AllowedViaFlag bool   `json:"allowed_via_flag"`
	ResumedRun     bool   `json:"resumed_run"`
	Note           string `json:"note,omitempty"`
}

// reportPhase is one phase's counts plus its item-level notes.
type reportPhase struct {
	Found        int        `json:"found"`
	Migrated     int        `json:"migrated"`
	Skipped      int        `json:"skipped"`
	AlreadyDone  int        `json:"already_done"`
	Failed       int        `json:"failed"`
	SkippedItems []ItemNote `json:"skipped_items,omitempty"`
	FailedItems  []ItemNote `json:"failed_items,omitempty"`
	Warnings     []string   `json:"warnings,omitempty"`
}

// reportTokens carries the token phase accounting plus its policy note.
type reportTokens struct {
	Found    int    `json:"found"`
	Migrated int    `json:"migrated"`
	Skipped  int    `json:"skipped"`
	Note     string `json:"note,omitempty"`
	Warning  string `json:"warning,omitempty"`
}

// reportArtifacts is the artifact phase section: aggregate counts, bytes
// and one row per repository (per-repo counts, skip reasons).
type reportArtifacts struct {
	reportPhase
	Bytes        int64               `json:"bytes"`
	Repositories []RepoArtifactStats `json:"repositories"`
}

// reportFile is the migration_report.json document (D8): the run's
// durable, machine-readable outcome.
type reportFile struct {
	Tool        string          `json:"tool"`
	Version     string          `json:"version"`
	GeneratedAt string          `json:"generated_at"`
	DurationMs  int64           `json:"duration_ms"`
	DryRun      bool            `json:"dry_run"`
	Source      string          `json:"source"`
	Target      string          `json:"target"`
	Guard       reportGuard     `json:"guard"`
	Repos       reportPhase     `json:"repos"`
	Users       reportPhase     `json:"users"`
	Tokens      reportTokens    `json:"tokens"`
	Artifacts   reportArtifacts `json:"artifacts"`
	Passwords   struct {
		Path    string `json:"path,omitempty"`
		Written int    `json:"written"`
	} `json:"passwords"`
	Progress struct {
		Path      string `json:"path"`
		Repos     int    `json:"repos"`
		Users     int    `json:"users"`
		Artifacts int    `json:"artifacts"`
	} `json:"progress"`
}

// phaseReportOf projects one Summary phase into the report shape.
func phaseReportOf(found, migrated, skipped, already, failed int, skips, failures []ItemNote, warnings []string) reportPhase {
	return reportPhase{
		Found:        found,
		Migrated:     migrated,
		Skipped:      skipped,
		AlreadyDone:  already,
		Failed:       failed,
		SkippedItems: skips,
		FailedItems:  failures,
		Warnings:     warnings,
	}
}

// userWarnings projects the users-phase degradation note (--skip-users with
// a refused listing, B-1) into the report's warnings slot; nil keeps the
// field absent for a phase that ran normally.
func userWarnings(s *Summary) []string {
	if s.UserPhaseWarn == "" {
		return nil
	}
	return []string{s.UserPhaseWarn}
}

// writeReportFile renders the summary into migration_report.json
// (atomically, 0600 — the document names accounts and repository layout).
// A missing path disables the report (tests and library embedding).
func writeReportFile(path, toolVersion string, s *Summary, started, ended time.Time) error {
	if path == "" {
		return nil
	}
	var rep reportFile
	rep.Tool = "bf-migrate"
	rep.Version = toolVersion
	rep.GeneratedAt = ended.UTC().Format(time.RFC3339)
	rep.DurationMs = ended.Sub(started).Milliseconds()
	rep.DryRun = s.DryRun
	rep.Source = s.SourceURL
	rep.Target = s.TargetURL
	rep.Guard = reportGuard{
		Checked:        s.Guard.Checked,
		TargetNonEmpty: s.Guard.TargetNonEmpty,
		Detail:         s.Guard.Detail,
		Refused:        s.Guard.Refused,
		AllowedViaFlag: s.Guard.AllowedViaFlag,
		ResumedRun:     s.Guard.ResumedRun,
		Note:           s.Guard.Note,
	}
	rep.Repos = phaseReportOf(s.Repos.Found, s.Repos.Migrated, s.Repos.Skipped, s.Repos.AlreadyDone, s.Repos.Failed, s.RepoSkips, s.RepoFailures, s.RepoWarnings)
	rep.Users = phaseReportOf(s.Users.Found, s.Users.Migrated, s.Users.Skipped, s.Users.AlreadyDone, s.Users.Failed, s.UserSkips, s.UserFailures, userWarnings(s))
	rep.Tokens = reportTokens{Found: s.TokensFound, Migrated: s.TokensMigrated, Skipped: s.TokensSkipped, Note: s.TokenPhaseNote, Warning: s.TokenPhaseWarn}
	rep.Artifacts = reportArtifacts{
		reportPhase:  phaseReportOf(s.Artifacts.Found, s.Artifacts.Migrated, s.Artifacts.Skipped, s.Artifacts.AlreadyDone, s.Artifacts.Failed, nil, s.ArtifactFailures, s.ArtifactWarnings),
		Bytes:        s.ArtifactBytes,
		Repositories: s.ArtifactRepos,
	}
	if rep.Artifacts.Repositories == nil {
		rep.Artifacts.Repositories = []RepoArtifactStats{}
	}
	rep.Passwords.Path = s.PasswordsPath
	rep.Passwords.Written = s.PasswordsWritten
	rep.Progress.Path = s.ProgressPath
	rep.Progress.Repos = s.ProgressRepos
	rep.Progress.Users = s.ProgressUsers
	rep.Progress.Artifacts = s.ProgressArtifacts

	raw, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("migrate: encode report: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("migrate: create report directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("migrate: create report temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("migrate: chmod report temp file: %w", err)
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("migrate: write report temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("migrate: close report temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("migrate: rename report file: %w", err)
	}
	return nil
}
