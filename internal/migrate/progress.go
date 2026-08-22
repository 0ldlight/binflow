package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Progress item states recorded in the progress file.
const (
	StateDone    = "done"
	StateSkipped = "skipped"
)

const progressVersion = 2

// Progress is the on-disk checkpoint of a migration run (JSON, one file,
// rewritten atomically after every item — artifacts are flushed in batches,
// see markArtifacts). Resume reloads it and skips items already recorded;
// because every target write verb is create-or-replace, a crash between
// "write succeeded" and "mark saved" is harmless — the item is simply
// redone.
type Progress struct {
	Version      int               `json:"version"`
	SourceURL    string            `json:"source_url"`
	TargetURL    string            `json:"target_url"`
	Repos        map[string]string `json:"repos"`     // key -> state
	Users        map[string]string `json:"users"`     // name -> state
	Artifacts    map[string]string `json:"artifacts"` // "repo/path" -> state
	TokensListed bool              `json:"tokens_listed"`
	UpdatedAt    string            `json:"updated_at"`

	path string
}

// newProgress returns an empty progress bound to the given endpoints.
func newProgress(path, source, target string, now time.Time) *Progress {
	return &Progress{
		Version:   progressVersion,
		SourceURL: source,
		TargetURL: target,
		Repos:     map[string]string{},
		Users:     map[string]string{},
		Artifacts: map[string]string{},
		UpdatedAt: now.UTC().Format(time.RFC3339),
		path:      path,
	}
}

// loadProgress reads the progress file for a --resume run. A missing file
// means "nothing recorded yet" and yields a fresh Progress, so resuming a
// run that crashed before its first save just starts over. A file written
// for DIFFERENT endpoints is refused: mixing two migrations into one
// checkpoint would silently skip items that were never migrated.
func loadProgress(path, source, target string, now time.Time) (*Progress, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: the path is the operator-provided --progress-file argument; reading it is the feature.
	if err != nil {
		if os.IsNotExist(err) {
			return newProgress(path, source, target, now), nil
		}
		return nil, fmt.Errorf("migrate: read progress file: %w", err)
	}
	var p Progress
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("migrate: progress file %s is not valid JSON (delete it or pick another --progress-file): %w", path, err)
	}
	if p.SourceURL != source || p.TargetURL != target {
		return nil, fmt.Errorf("migrate: progress file %s was written for source %q / target %q, refusing to resume against source %q / target %q (use a different --progress-file for this pair)",
			path, p.SourceURL, p.TargetURL, source, target)
	}
	if p.Repos == nil {
		p.Repos = map[string]string{}
	}
	if p.Users == nil {
		p.Users = map[string]string{}
	}
	if p.Artifacts == nil {
		p.Artifacts = map[string]string{} // v1 files predate the artifact phase
	}
	p.path = path
	return &p, nil
}

// hasRecords reports whether any item of any phase was recorded — the
// signal that a --resume run continues THIS migration (the file is already
// endpoint-bound), which is what exempts it from the empty-target guard.
func (p *Progress) hasRecords() bool {
	return len(p.Repos) > 0 || len(p.Users) > 0 || len(p.Artifacts) > 0 || p.TokensListed
}

// state returns the recorded state of one repository ("" when absent).
func (p *Progress) repoState(key string) string { return p.Repos[key] }

// userState returns the recorded state of one user ("" when absent).
func (p *Progress) userState(name string) string { return p.Users[name] }

// artifactKey is the progress key of one artifact ("repo/path" — the slash
// inside the repo key is impossible, so the spelling is unambiguous).
func artifactKey(repo, path string) string { return repo + "/" + path }

// artifactState returns the recorded state of one artifact ("" when absent).
func (p *Progress) artifactState(repo, path string) string {
	return p.Artifacts[artifactKey(repo, path)]
}

// markRepo records a repository state and persists the file.
func (p *Progress) markRepo(key, state string, now time.Time) error {
	p.Repos[key] = state
	return p.save(now)
}

// markUser records a user state and persists the file.
func (p *Progress) markUser(name, state string, now time.Time) error {
	p.Users[name] = state
	return p.save(now)
}

// setArtifact records an artifact state in memory only. The artifact phase
// folds results on one goroutine and persists in batches (markArtifacts):
// rewriting the file per artifact would make a large repository's
// checkpoint cost quadratic in the artifact count.
func (p *Progress) setArtifact(repo, path, state string) {
	p.Artifacts[artifactKey(repo, path)] = state
}

// markArtifacts persists the file after a batch of setArtifact updates.
func (p *Progress) markArtifacts(now time.Time) error {
	return p.save(now)
}

// markTokensListed records that the token accounting phase completed.
func (p *Progress) markTokensListed(now time.Time) error {
	p.TokensListed = true
	return p.save(now)
}

// save rewrites the progress file atomically (temp file + rename in the
// same directory) with 0600 permissions.
func (p *Progress) save(now time.Time) error {
	p.UpdatedAt = now.UTC().Format(time.RFC3339)
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("migrate: encode progress: %w", err)
	}
	dir := filepath.Dir(p.path)
	tmp, err := os.CreateTemp(dir, filepath.Base(p.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("migrate: create progress temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("migrate: chmod progress temp file: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("migrate: write progress temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("migrate: close progress temp file: %w", err)
	}
	if err := os.Rename(tmpName, p.path); err != nil {
		return fmt.Errorf("migrate: rename progress file: %w", err)
	}
	return nil
}
