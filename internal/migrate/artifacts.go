package migrate

import (
	"context"
	"crypto/sha1" //nolint:gosec // SHA-1 only cross-checks the source's own listing digest; SHA-256 remains the integrity anchor
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// DefaultArtifactConcurrency is the parallel artifact copy width
// (PRD FR-63: --concurrency, default 4).
const DefaultArtifactConcurrency = 4

// artifactFlushMarks is the number of artifact progress marks buffered
// before the checkpoint file is rewritten. Crashing inside the window only
// re-copies idempotent uploads on --resume; rewriting per artifact would
// make a large repository's checkpoint cost quadratic in the file count.
const artifactFlushMarks = 32

// RepoArtifactStats is one repository's row in the artifact phase report.
type RepoArtifactStats struct {
	Repo        string `json:"repo"`
	Rclass      string `json:"rclass"`
	PackageType string `json:"package_type"`
	Supported   bool   `json:"supported"`
	Found       int    `json:"found"`
	Migrated    int    `json:"migrated"`
	Skipped     int    `json:"skipped"`
	AlreadyDone int    `json:"already_done"`
	Failed      int    `json:"failed"`
	Bytes       int64  `json:"bytes"`
	Reason      string `json:"reason,omitempty"`
}

// artifactUnsupported lists the local package types whose artifacts cannot
// be copied through the plain-file face, with the honest reason. The
// repository CONFIGURATION still migrates (repo phase); the artifacts are
// left behind and accounted in the report — the T-175 D2/D3/D4 evidence
// behind each reason is why the tool refuses to guess.
var artifactUnsupported = map[string]string{
	"docker": "docker artifacts upload through the registry protocol (manifest/blob sessions on /v2/); the plain-file copy face cannot reproduce them — repository configuration migrated, artifacts left behind",
	"npm":    "npm artifacts upload through the npm publish face (tarball + package metadata); the plain-file copy face cannot reproduce them — repository configuration migrated, artifacts left behind",
	"pypi":   "pypi artifacts upload through the twine upload face (multipart with a generated simple index); the plain-file copy face cannot reproduce them — repository configuration migrated, artifacts left behind",
}

// nonLocalArtifactReason explains why a non-local repository carries no
// artifact migration at all (nothing is listed, nothing is left behind).
func nonLocalArtifactReason(rclass string) string {
	switch rclass {
	case "remote":
		return "remote repositories proxy an upstream; their cache is not migrated (reconfigure the upstream and the cache refills on demand)"
	case "virtual":
		return "virtual repositories aggregate their members; artifacts live in the member local repositories"
	default:
		return fmt.Sprintf("repository class %q holds no artifacts to migrate", rclass)
	}
}

// artifactSupport decides whether one local repository's artifacts can be
// copied. generic is the primary face; maven (and the gradle alias) works
// because the layout is plain files the target's maven adapter accepts.
func artifactSupport(packageType string) (bool, string) {
	if reason, unsupported := artifactUnsupported[packageType]; unsupported {
		return false, reason
	}
	switch packageType {
	case "generic", "maven":
		return true, ""
	default:
		return false, fmt.Sprintf("package type %q has no artifact migration support", packageType)
	}
}

// artifactResult is one copied (or failed) artifact.
type artifactResult struct {
	Path   string
	Size   int64
	Sha256 string
	Err    error
}

// migrateOneArtifact copies one artifact: download to a temp file while
// hashing, verify the digests the listing promised, then upload through
// the target's plain-file face.
//
// The spool lands in the OS temp directory. T-477's ruling (the T-474/476
// read-only-rootfs family survey): this is the OPERATIONS CLI, run by an
// operator in their own shell against a server they control — not the
// hardened server runtime, so the in-process staging root the server
// carries does not apply. Operators on constrained hosts point TMPDIR at
// a writable volume (Go's os.CreateTemp("") honors it); documenting that
// replaces a --spool-dir flag here, registered in the T-477 report.
func migrateOneArtifact(ctx context.Context, rd *Reader, wr *Writer, repo string, f SourceFile) artifactResult {
	res := artifactResult{Path: f.Path}
	if err := validateArtifactPath(f.Path); err != nil {
		res.Err = err
		return res
	}

	tmp, err := os.CreateTemp("", "bf-migrate-art-*")
	if err != nil {
		res.Err = fmt.Errorf("create temp file: %w", err)
		return res
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()

	h256 := sha256.New()
	h1 := sha1.New() //nolint:gosec // secondary cross-check only, see the import rationale
	rc, err := rd.OpenFile(ctx, repo, f.Path)
	if err != nil {
		res.Err = fmt.Errorf("download: %w", err)
		return res
	}
	size, err := io.Copy(io.MultiWriter(tmp, h256, h1), rc)
	closeErr := rc.Close()
	if err != nil {
		res.Err = fmt.Errorf("download: %w", err)
		return res
	}
	if closeErr != nil {
		res.Err = fmt.Errorf("download: close body: %w", closeErr)
		return res
	}

	got256 := hex.EncodeToString(h256.Sum(nil))
	if f.Sha256 != "" && !strings.EqualFold(got256, f.Sha256) {
		res.Err = fmt.Errorf("sha256 mismatch: source listing says %s, downloaded content hashes to %s", f.Sha256, got256)
		return res
	}
	got1 := hex.EncodeToString(h1.Sum(nil))
	if f.Sha1 != "" && !strings.EqualFold(got1, f.Sha1) {
		res.Err = fmt.Errorf("sha1 mismatch: source listing says %s, downloaded content hashes to %s", f.Sha1, got1)
		return res
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		res.Err = fmt.Errorf("rewind temp file: %w", err)
		return res
	}
	if err := wr.UploadArtifact(ctx, repo, f.Path, tmp, size, ""); err != nil {
		res.Err = fmt.Errorf("upload: %w", err)
		return res
	}
	res.Size = size
	res.Sha256 = got256
	return res
}

// validateArtifactPath rejects entries the listing should never produce:
// empty, dot/dot-dot segments, absolute or trailing-slash shapes. The
// upload path must stay a clean relative path or the copy would target an
// unintended node.
func validateArtifactPath(path string) error {
	if path == "" {
		return fmt.Errorf("empty artifact path")
	}
	if strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return fmt.Errorf("artifact path %q is not a plain relative file path", path)
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("artifact path %q has an invalid segment", path)
		}
	}
	return nil
}

// runArtifactPhase copies the artifacts of every repository the repo phase
// put (or would put) on the target. Single-artifact failures never abort
// the run (PRD FR-63): they are recorded, left out of the progress file
// and retried by --resume.
func runArtifactPhase(ctx context.Context, rd *Reader, wr *Writer, opts Options, progress *Progress, s *Summary, now func() time.Time, refs []repoArtifactRef) error {
	workers := opts.ArtifactConcurrency
	if workers < 1 {
		workers = DefaultArtifactConcurrency
	}

	for _, ref := range refs {
		stats := RepoArtifactStats{Repo: ref.Key, Rclass: ref.Rclass, PackageType: ref.PackageType}

		switch {
		case ref.Status == repoStatusSkipped:
			stats.Reason = "repository skipped in the repo phase (unsupported on the target); artifacts not migrated"
		case ref.Status == repoStatusFailed:
			stats.Reason = "repository creation failed in the repo phase; artifacts not attempted (re-run with --resume)"
		case ref.Rclass != "local":
			stats.Reason = nonLocalArtifactReason(ref.Rclass)
		default:
			ok, reason := artifactSupport(ref.PackageType)
			stats.Supported = ok
			files, err := rd.ListRepoFiles(ctx, ref.Key)
			if err != nil {
				stats.Reason = fmt.Sprintf("file listing failed: %v", err)
				s.Artifacts.Failed++ // one repo-level failure item, retried by --resume
				s.ArtifactFailures = append(s.ArtifactFailures, ItemNote{Name: ref.Key, Reason: stats.Reason})
				break
			}
			stats.Found = len(files)

			// Split off artifacts a previous run already recorded; the
			// split runs on this goroutine so workers never read the
			// progress map while it is being mutated.
			todo := files[:0:0]
			for _, f := range files {
				if progress.artifactState(ref.Key, f.Path) != "" {
					stats.AlreadyDone++
					s.Artifacts.AlreadyDone++
					continue
				}
				todo = append(todo, f)
			}

			switch {
			case !ok:
				stats.Skipped = len(files)
				stats.Reason = reason
				s.Artifacts.Skipped += len(files)
			case opts.DryRun:
				stats.Migrated = len(todo)
				s.Artifacts.Migrated += len(todo)
			default:
				if err := copyRepoArtifacts(ctx, rd, wr, progress, s, &stats, todo, workers, now); err != nil {
					return err
				}
			}
		}

		s.Artifacts.Found += stats.Found
		s.ArtifactRepos = append(s.ArtifactRepos, stats)
	}
	return nil
}

// copyRepoArtifacts runs the bounded-parallel copy for one repository and
// folds results on the caller's goroutine (the only one mutating stats,
// the summary and the progress map).
func copyRepoArtifacts(ctx context.Context, rd *Reader, wr *Writer, progress *Progress, s *Summary, stats *RepoArtifactStats, todo []SourceFile, workers int, now func() time.Time) error {
	if len(todo) == 0 {
		return nil
	}

	jobs := make(chan SourceFile)
	results := make(chan artifactResult, len(todo)) // workers never block on send

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				results <- migrateOneArtifact(ctx, rd, wr, stats.Repo, f)
			}
		}()
	}
	go func() {
	feed:
		for _, f := range todo {
			select {
			case jobs <- f:
			case <-ctx.Done():
				break feed
			}
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	pending := 0
	interrupted := false
	for res := range results {
		switch {
		case res.Err == nil:
			stats.Migrated++
			stats.Bytes += res.Size
			s.Artifacts.Migrated++
			s.ArtifactBytes += res.Size
			progress.setArtifact(stats.Repo, res.Path, StateDone)
			pending++
			if pending >= artifactFlushMarks {
				if err := progress.markArtifacts(now()); err != nil {
					return err
				}
				pending = 0
			}
		case errors.Is(res.Err, context.Canceled) || errors.Is(res.Err, context.DeadlineExceeded):
			// The interruption is one warning, not one failure per file;
			// nothing was recorded for the interrupted files, so --resume
			// picks them up.
			if !interrupted {
				interrupted = true
				s.ArtifactWarnings = append(s.ArtifactWarnings,
					fmt.Sprintf("artifact copy interrupted (%v); re-run with --resume to finish", ctx.Err()))
			}
		default:
			stats.Failed++
			s.Artifacts.Failed++
			s.ArtifactFailures = append(s.ArtifactFailures, ItemNote{Name: stats.Repo + "/" + res.Path, Reason: res.Err.Error()})
		}
	}
	if pending > 0 {
		if err := progress.markArtifacts(now()); err != nil {
			return err
		}
	}
	return nil
}
