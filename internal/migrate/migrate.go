package migrate

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/client"
)

// Options controls one migration run.
type Options struct {
	// DryRun performs reads and conversions only: nothing is written to
	// the target, and neither the progress file nor the passwords file is
	// created or touched.
	DryRun bool

	// Resume loads the progress file and skips already-recorded items.
	// Without it the run starts from scratch and overwrites the progress
	// file as it goes.
	Resume bool

	// AllowNonEmpty overrides the empty-target guard (FR-63-AC1). The run
	// then merges into a populated target under MergeSemanticsNote, which
	// the report records.
	AllowNonEmpty bool

	// ProgressPath is the checkpoint file location.
	ProgressPath string

	// ReportPath is where migration_report.json is written (D8). Empty
	// disables the report file.
	ReportPath string

	// ArtifactConcurrency is the parallel artifact copy width per
	// repository (PRD FR-63 --concurrency). Below 1 selects the default.
	ArtifactConcurrency int

	// ToolVersion is stamped into the report (the cmd wires its ldflags
	// version in). Empty lands as "".
	ToolVersion string

	// PasswordEnv names an environment variable holding a shared password
	// assigned to every migrated user (operator rotates afterwards).
	PasswordEnv string

	// PasswordsOut names a file (0600, append) that receives one generated
	// random password per migrated user as "<name> <password>" lines.
	// Exactly one of PasswordEnv / PasswordsOut must be usable when the
	// users phase has users to create.
	PasswordsOut string

	// SkipUsers makes the users phase optional (FR-77 AC2, the B-1 fix):
	// when the source REFUSES the user listing — HTTP 403 (non-admin
	// credentials) or 400 (the Artifactory OSS license gate answers
	// "available only in Artifactory Pro" to every caller, T-228 R-1) —
	// the phase is skipped with a recorded degradation warning instead of
	// aborting the whole run, and tokens/artifacts still migrate. A listing
	// that succeeds migrates users normally. Any OTHER listing error
	// (network, 5xx, decode) still aborts: fail-closed for unexpected
	// shapes. Without the flag the phase is unchanged — any listing error
	// aborts before the first user write.
	SkipUsers bool

	// Stdout receives the summary. Nil selects io.Discard.
	Stdout io.Writer

	// RandomPassword overrides the password generator (tests inject a
	// deterministic one). Nil uses crypto/rand.
	RandomPassword func() (string, error)

	// Now overrides the clock for progress timestamps (tests inject a
	// fixed one). Nil uses time.Now.
	Now func() time.Time
}

// PhaseCounts aggregates one phase's outcome.
type PhaseCounts struct {
	Found       int // items listed on the source
	Migrated    int // written (dry-run: would be written)
	Skipped     int // not migratable, with a recorded reason
	AlreadyDone int // skipped by --resume (recorded in a previous run)
	Failed      int // write or source-read error, retried on the next run
}

// ItemNote names one skipped or failed item and why.
type ItemNote struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// GuardInfo records the empty-target guard's outcome (FR-63-AC1, D6).
type GuardInfo struct {
	// Checked is true when the target's occupancy was determined (the
	// listing ran). A dry-run against an unreachable target leaves it
	// checked-but-noted.
	Checked bool

	// TargetNonEmpty is true when the target holds at least one
	// repository (the PRD's definition of a non-empty instance).
	TargetNonEmpty bool

	// Detail describes what the target holds (counts, first keys).
	Detail string

	// Refused is true when the run was rejected by the guard.
	Refused bool

	// AllowedViaFlag is true when --allow-non-empty overrode a refusal.
	AllowedViaFlag bool

	// ResumedRun is true when the guard was skipped because --resume
	// continues this same migration (endpoint-bound progress with records).
	ResumedRun bool

	// Note carries the soft explanations (dry-run observation, resume
	// exemption, merge semantics).
	Note string
}

// Summary is the full run report, also rendered by Print.
type Summary struct {
	DryRun bool

	// SourceURL/TargetURL name the run's endpoints (also in the report).
	SourceURL string
	TargetURL string

	Guard GuardInfo

	Repos        PhaseCounts
	RepoSkips    []ItemNote
	RepoFailures []ItemNote
	RepoWarnings []string

	Users        PhaseCounts
	UserSkips    []ItemNote
	UserFailures []ItemNote

	// UserPhaseWarn carries the users-phase degradation note (--skip-users
	// with a refused listing): rendered under the users block and persisted
	// in the report's users warnings.
	UserPhaseWarn string

	// TokensMigrated is always 0: token values are not exportable
	// (TokenPolicyNote).
	TokensFound    int
	TokensMigrated int
	TokensSkipped  int
	TokenPhaseNote string
	TokenPhaseWarn string

	// Artifacts aggregates the artifact phase (D7); ArtifactRepos carries
	// the per-repository rows the report renders.
	Artifacts        PhaseCounts
	ArtifactRepos    []RepoArtifactStats
	ArtifactFailures []ItemNote
	ArtifactWarnings []string
	ArtifactBytes    int64

	// PasswordsPath/PasswordsWritten report the generated-passwords file.
	PasswordsPath    string
	PasswordsWritten int

	ProgressPath      string
	ProgressRepos     int
	ProgressUsers     int
	ProgressArtifacts int

	// ReportPath/ReportErr report the migration_report.json outcome (D8).
	ReportPath string
	ReportErr  string
}

// TokenPolicyNote is the honest statement of the token phase's limit,
// quoted in every summary (source: docs/reverse/auth-model.md 3.3 — the
// listing is metadata-only; values are returned exactly once at creation).
const TokenPolicyNote = "token values cannot be exported from Artifactory (metadata-only listing); tokens are accounted and skipped — recreate them on the target and redistribute"

// generatedPasswordCharset excludes ambiguous glyphs (0/O, 1/l/I).
const generatedPasswordCharset = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// generatedPasswordLen is the per-user password length.
const generatedPasswordLen = 24

// Repo-phase outcome statuses feeding the artifact phase.
const (
	repoStatusMigrated = "migrated"     // written to the target this run
	repoStatusAlready  = "already-done" // recorded by a previous run (--resume)
	repoStatusPlanned  = "planned"      // dry-run: would be written
	repoStatusSkipped  = "skipped"      // conversion refused it
	repoStatusFailed   = "failed"       // the write failed this run
)

// repoArtifactRef is one repository's eligibility input for the artifact
// phase: what the repo phase did to it, plus the type information the
// artifact support matrix needs.
type repoArtifactRef struct {
	Key         string
	Rclass      string
	PackageType string // target spelling (after the gradle->maven alias)
	Status      string
}

// Run executes the four-phase migration (repos -> users -> tokens ->
// artifacts) and returns the summary. Item-level failures do not abort the
// run: they are recorded, excluded from the progress file (so --resume
// retries them) and surfaced through a non-nil error once everything else
// has been attempted. The artifact phase runs LAST: the cheap
// configuration phases fail fast (password strategy, target refusals)
// before the bulk data copy starts.
//
// The empty-target guard (FR-63-AC1, D6) runs before any write. The report
// file (D8) is written on every exit path past options validation —
// including guard refusals and phase aborts, so the operator always has
// the durable outcome.
func Run(ctx context.Context, rd *Reader, wr *Writer, opts Options) (*Summary, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	out := opts.Stdout
	if out == nil {
		out = io.Discard
	}

	targetURL := ""
	if wr != nil && wr.c != nil {
		targetURL = wr.c.BaseURL
	}

	var progress *Progress
	var err error
	if opts.Resume {
		progress, err = loadProgress(opts.ProgressPath, rd.base, targetURL, now())
	} else {
		progress = newProgress(opts.ProgressPath, rd.base, targetURL, now())
	}
	if err != nil {
		return nil, err
	}

	started := now()
	s := &Summary{
		DryRun:       opts.DryRun,
		SourceURL:    rd.base,
		TargetURL:    targetURL,
		ProgressPath: opts.ProgressPath,
		ReportPath:   opts.ReportPath,
	}

	// writeReport persists the D8 document; its own failure is surfaced
	// both in the summary and as the run's error.
	writeReport := func() {
		if err := writeReportFile(opts.ReportPath, opts.ToolVersion, s, started, now()); err != nil {
			s.ReportErr = err.Error()
		}
	}

	if gerr := checkTargetGuard(ctx, wr, opts, progress, s); gerr != nil {
		writeReport()
		s.Print(out)
		return s, gerr
	}

	refs, rerr := runRepoPhase(ctx, rd, wr, opts, progress, s, now)
	if rerr != nil {
		writeReport()
		return s, rerr
	}
	if uerr := runUserPhase(ctx, rd, wr, opts, progress, s, now); uerr != nil {
		writeReport()
		return s, uerr
	}
	runTokenPhase(ctx, rd, opts, progress, s, now)
	if aerr := runArtifactPhase(ctx, rd, wr, opts, progress, s, now, refs); aerr != nil {
		writeReport()
		return s, aerr
	}

	s.ProgressRepos = len(progress.Repos)
	s.ProgressUsers = len(progress.Users)
	s.ProgressArtifacts = len(progress.Artifacts)
	s.Print(out)
	writeReport()

	if s.Repos.Failed > 0 || s.Users.Failed > 0 || s.Artifacts.Failed > 0 {
		return s, s.failureError()
	}
	if s.ReportErr != "" {
		return s, fmt.Errorf("migrate: %s", s.ReportErr)
	}
	return s, nil
}

// checkTargetGuard enforces the empty-instance rule (FR-63-AC1, D6): a
// migration may only populate a target that holds ZERO repositories.
//
//   - Dry-run never refuses (it writes nothing) but still observes and
//     records the occupancy, with soft failures when the target cannot be
//     read without credentials.
//   - --resume against a progress file WITH records continues regardless:
//     the file is endpoint-bound (loadProgress refuses a foreign pair), so
//     the occupancy is this migration's own earlier writes.
//   - A write run that cannot even LIST the target repos fails closed.
//   - --allow-non-empty overrides a refusal and stamps MergeSemanticsNote
//     into the summary and the report.
func checkTargetGuard(ctx context.Context, wr *Writer, opts Options, progress *Progress, s *Summary) error {
	s.Guard.Checked = true

	repos, err := wr.ListTargetRepos(ctx)
	switch {
	case err != nil:
		if opts.DryRun {
			s.Guard.Note = fmt.Sprintf("target occupancy not verified (dry-run): %v", err)
			return nil
		}
		return fmt.Errorf("migrate: target occupancy check failed — refusing to proceed blindly; fix target access or pass --allow-non-empty: %w", err)
	case len(repos) > 0:
		s.Guard.TargetNonEmpty = true
		s.Guard.Detail = describeTargetRepos(repos)
	}

	if opts.DryRun {
		if s.Guard.TargetNonEmpty {
			s.Guard.Note = "dry-run only: the target is not empty — a real run would refuse without --allow-non-empty"
		}
		return nil
	}
	if opts.Resume && progress.hasRecords() {
		s.Guard.ResumedRun = true
		s.Guard.Note = "continuing an interrupted run of this endpoint pair; the target's occupancy includes this migration's own earlier writes"
		return nil
	}
	if !s.Guard.TargetNonEmpty {
		return nil
	}
	if opts.AllowNonEmpty {
		s.Guard.AllowedViaFlag = true
		s.Guard.Note = MergeSemanticsNote
		return nil
	}

	s.Guard.Refused = true
	return fmt.Errorf("%w: %s — only an empty instance (zero repositories) may be migrated into; pass --allow-non-empty to merge anyway (%s)",
		ErrTargetNotEmpty, s.Guard.Detail, MergeSemanticsNote)
}

// ErrTargetNotEmpty is the guard's refusal (FR-63-AC1): the run exits
// non-zero without touching the target.
var ErrTargetNotEmpty = errors.New("migrate: target instance is not empty")

// describeTargetRepos renders the occupancy evidence for the error and
// the report (count plus the first few keys).
func describeTargetRepos(repos []client.RepoInfo) string {
	const show = 5
	keys := make([]string, 0, len(repos))
	for _, r := range repos {
		keys = append(keys, r.Key)
	}
	who := strings.Join(keys, ", ")
	if len(keys) > show {
		who = strings.Join(keys[:show], ", ") + ", …"
	}
	return fmt.Sprintf("%d repositories (%s)", len(repos), who)
}

// runRepoPhase migrates repositories in source list order. The order is
// load-bearing: the real source sorts by type (local/remote before
// virtual), so a virtual repository's members already exist on the target
// by the time it is created. It returns the artifact-phase input: one ref
// per source repository recording what this run did to it.
func runRepoPhase(ctx context.Context, rd *Reader, wr *Writer, opts Options, progress *Progress, s *Summary, now func() time.Time) ([]repoArtifactRef, error) {
	list, err := rd.ListRepositories(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate: repo phase aborted before any write: %w", err)
	}
	s.Repos.Found = len(list)

	var refs []repoArtifactRef
	for _, item := range list {
		if state := progress.repoState(item.Key); state != "" {
			s.Repos.AlreadyDone++
			refs = append(refs, repoArtifactRef{
				Key:         item.Key,
				Rclass:      strings.ToLower(item.Type),
				PackageType: aliasPackageType(item.PackageType),
				Status:      repoStatusSkippedRef(state),
			})
			continue
		}

		cfg, err := rd.RepositoryConfig(ctx, item.Key)
		if err != nil {
			s.Repos.Failed++
			s.RepoFailures = append(s.RepoFailures, ItemNote{Name: item.Key, Reason: err.Error()})
			refs = append(refs, repoArtifactRef{Key: item.Key, Rclass: strings.ToLower(item.Type), PackageType: aliasPackageType(item.PackageType), Status: repoStatusFailed})
			continue
		}

		plan := ConvertRepo(*cfg)
		for _, w := range plan.Warnings {
			s.RepoWarnings = append(s.RepoWarnings, fmt.Sprintf("repo %s: %s", plan.Key, w))
		}
		if plan.Skipped {
			s.Repos.Skipped++
			s.RepoSkips = append(s.RepoSkips, ItemNote{Name: plan.Key, Reason: plan.Reason})
			if !opts.DryRun {
				if err := progress.markRepo(plan.Key, StateSkipped, now()); err != nil {
					return nil, err
				}
			}
			refs = append(refs, repoArtifactRef{Key: plan.Key, Rclass: strings.ToLower(cfg.Rclass), PackageType: aliasPackageType(cfg.PackageType), Status: repoStatusSkipped})
			continue
		}

		if opts.DryRun {
			s.Repos.Migrated++
			refs = append(refs, repoArtifactRef{Key: plan.Key, Rclass: strings.ToLower(cfg.Rclass), PackageType: plan.Request.PackageType, Status: repoStatusPlanned})
			continue
		}
		if err := wr.EnsureRepo(ctx, plan.Request); err != nil {
			s.Repos.Failed++
			s.RepoFailures = append(s.RepoFailures, ItemNote{Name: plan.Key, Reason: err.Error()})
			refs = append(refs, repoArtifactRef{Key: plan.Key, Rclass: strings.ToLower(cfg.Rclass), PackageType: plan.Request.PackageType, Status: repoStatusFailed})
			continue
		}
		s.Repos.Migrated++
		if err := progress.markRepo(plan.Key, StateDone, now()); err != nil {
			return nil, err
		}
		refs = append(refs, repoArtifactRef{Key: plan.Key, Rclass: strings.ToLower(cfg.Rclass), PackageType: plan.Request.PackageType, Status: repoStatusMigrated})
	}
	return refs, nil
}

// repoStatusSkippedRef maps a recorded progress state onto the artifact
// ref status: a repository recorded "skipped" never reached the target, a
// repository recorded "done" is one.
func repoStatusSkippedRef(state string) string {
	if state == StateDone {
		return repoStatusAlready
	}
	return repoStatusSkipped
}

// aliasPackageType applies the converter's package-type aliasing to a list
// item's raw spelling (gradle -> maven), so artifact refs built without a
// full conversion still see the target vocabulary.
func aliasPackageType(pkg string) string {
	if alias, ok := artifactoryPackageTypeAliases[strings.ToLower(strings.TrimSpace(pkg))]; ok {
		return alias
	}
	return strings.ToLower(strings.TrimSpace(pkg))
}

// runUserPhase migrates internal-realm users. Passwords are assigned at
// write time: either the shared value from PasswordEnv, or a generated
// per-user value appended to PasswordsOut.
func runUserPhase(ctx context.Context, rd *Reader, wr *Writer, opts Options, progress *Progress, s *Summary, now func() time.Time) error {
	list, err := rd.ListUsers(ctx)
	if err != nil {
		// B-1 (FR-77 AC2): with --skip-users a REFUSED listing degrades to
		// a warning and the run continues without users. Only 403/400
		// qualify — the deterministic refusals T-228 documented on a real
		// OSS source (non-admin credentials / the license gate); anything
		// else (network, 5xx, decode) still aborts before the first write.
		if opts.SkipUsers && IsStatus(err, 400, 403) {
			s.UserPhaseWarn = fmt.Sprintf("users phase skipped via --skip-users: source refused the user listing (403 = non-admin credentials, 400 = e.g. the Artifactory OSS license gate): %v — no users migrated; recreate accounts on the target", err)
			return nil
		}
		return fmt.Errorf("migrate: user phase aborted before any write: %w", err)
	}
	s.Users.Found = len(list)

	sharedPassword, useShared, err := resolveSharedPassword(opts)
	if err != nil {
		return err
	}

	var generated []ItemNote // Reason field carries the password until the file write
	for _, item := range list {
		if state := progress.userState(item.Name); state != "" {
			s.Users.AlreadyDone++
			continue
		}

		detail, err := rd.UserDetail(ctx, item.Name)
		if err != nil {
			s.Users.Failed++
			s.UserFailures = append(s.UserFailures, ItemNote{Name: item.Name, Reason: err.Error()})
			continue
		}

		plan := ConvertUser(*detail)
		if plan.Skipped {
			s.Users.Skipped++
			s.UserSkips = append(s.UserSkips, ItemNote{Name: item.Name, Reason: plan.Reason})
			if !opts.DryRun {
				if err := progress.markUser(plan.Name, StateSkipped, now()); err != nil {
					return err
				}
			}
			continue
		}

		if opts.DryRun {
			s.Users.Migrated++
			continue
		}

		password := sharedPassword
		if !useShared {
			pw, err := generatePassword(opts)
			if err != nil {
				return err
			}
			password = pw
		}

		if err := wr.EnsureUser(ctx, client.UserCreateRequest{
			Name:     plan.Name,
			Password: password,
			Email:    plan.Email,
			Admin:    plan.Admin,
			Groups:   plan.Groups,
		}); err != nil {
			s.Users.Failed++
			s.UserFailures = append(s.UserFailures, ItemNote{Name: plan.Name, Reason: err.Error()})
			continue
		}
		if !useShared {
			// Only a password whose account write SUCCEEDED lands in the
			// file — a failed write leaves no account behind, and a --resume
			// retry generates (and records) a fresh one.
			generated = append(generated, ItemNote{Name: plan.Name, Reason: password})
		}
		s.Users.Migrated++
		if err := progress.markUser(plan.Name, StateDone, now()); err != nil {
			return err
		}
	}

	if opts.DryRun || len(generated) == 0 {
		return nil
	}
	if err := appendGeneratedPasswords(opts.PasswordsOut, generated); err != nil {
		return err
	}
	s.PasswordsPath = opts.PasswordsOut
	s.PasswordsWritten = len(generated)
	return nil
}

// runTokenPhase accounts for source tokens. Token values are not
// exportable (TokenPolicyNote), so every found token is skipped by policy;
// a refused listing (e.g. non-admin credentials -> 403) is a warning, not
// a failure, and is not marked in the progress file so --resume retries it.
func runTokenPhase(ctx context.Context, rd *Reader, opts Options, progress *Progress, s *Summary, now func() time.Time) {
	s.TokenPhaseNote = TokenPolicyNote

	if opts.Resume && progress.TokensListed {
		s.TokenPhaseNote = "token listing already recorded by a previous run (--resume); " + TokenPolicyNote
		return
	}

	tokens, err := rd.ListTokens(ctx)
	if err != nil {
		if IsStatus(err, 401, 403) {
			s.TokenPhaseWarn = fmt.Sprintf("token listing refused (admin-only endpoint): %v", err)
			return
		}
		s.TokenPhaseWarn = fmt.Sprintf("token listing failed: %v", err)
		return
	}
	s.TokensFound = len(tokens)
	s.TokensSkipped = len(tokens)
	if !opts.DryRun {
		if err := progress.markTokensListed(now()); err != nil {
			s.TokenPhaseWarn = fmt.Sprintf("progress file update failed: %v", err)
		}
	}
}

// resolveSharedPassword resolves the password strategy before the first
// user write. Fails fast with an actionable message when the users phase
// is about to create accounts but neither strategy is usable.
func resolveSharedPassword(opts Options) (string, bool, error) {
	if opts.DryRun {
		return "", false, nil
	}
	if opts.PasswordEnv != "" {
		pw, ok := os.LookupEnv(opts.PasswordEnv)
		if !ok || pw == "" {
			return "", false, fmt.Errorf("migrate: user phase aborted before any user write: --password-env %s is not set or empty", opts.PasswordEnv)
		}
		return pw, true, nil
	}
	if opts.PasswordsOut == "" {
		return "", false, fmt.Errorf("migrate: user phase aborted before any user write: no password strategy — pass --password-env <var> (shared password) or --passwords-out <file> (generated per-user passwords)")
	}
	return "", false, nil
}

// generatePassword produces one random password (deterministic when the
// test generator is injected).
func generatePassword(opts Options) (string, error) {
	if opts.RandomPassword != nil {
		return opts.RandomPassword()
	}
	var sb strings.Builder
	for i := 0; i < generatedPasswordLen; i++ {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(generatedPasswordCharset))))
		if err != nil {
			return "", fmt.Errorf("migrate: generate password: %w", err)
		}
		sb.WriteByte(generatedPasswordCharset[idx.Int64()])
	}
	return sb.String(), nil
}

// appendGeneratedPasswords appends "<name> <password>" lines to the
// operator-named passwords file, creating it with 0600 when absent.
// Append (not truncate) so a --resume run adds only the users it actually
// created — earlier lines belong to the previous run's file.
func appendGeneratedPasswords(path string, entries []ItemNote) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // G304: the path is the operator-provided --passwords-out argument; writing it is the feature. G306: 0600 is already the tightest usable mode.
	if err != nil {
		return fmt.Errorf("migrate: open passwords file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(e.Name)
		sb.WriteByte(' ')
		sb.WriteString(e.Reason)
		sb.WriteByte('\n')
	}
	if _, err := f.WriteString(sb.String()); err != nil {
		return fmt.Errorf("migrate: write passwords file: %w", err)
	}
	return nil
}

// failureError aggregates the item failures of the run.
func (s *Summary) failureError() error {
	var notes []string
	for _, n := range s.RepoFailures {
		notes = append(notes, fmt.Sprintf("repo %s: %s", n.Name, n.Reason))
	}
	for _, n := range s.UserFailures {
		notes = append(notes, fmt.Sprintf("user %s: %s", n.Name, n.Reason))
	}
	for _, n := range s.ArtifactFailures {
		notes = append(notes, fmt.Sprintf("artifact %s: %s", n.Name, n.Reason))
	}
	first := "unknown"
	if len(notes) > 0 {
		first = notes[0]
	}
	return fmt.Errorf("migrate: %d item(s) failed (%d repo, %d user, %d artifact); fix and re-run with --resume; first failure: %s",
		len(notes), s.Repos.Failed, s.Users.Failed, s.Artifacts.Failed, first)
}

// Print renders the summary as the tool's run report.
func (s *Summary) Print(w io.Writer) {
	mode := ""
	if s.DryRun {
		mode = " (dry-run)"
	}
	_, _ = fmt.Fprintf(w, "bf-migrate summary%s\n", mode)

	_, _ = fmt.Fprintf(w, "source: %s\n", s.SourceURL)
	_, _ = fmt.Fprintf(w, "target: %s\n", s.TargetURL)

	switch {
	case s.Guard.Refused:
		_, _ = fmt.Fprintf(w, "guard: REFUSED — target is not empty: %s\n", s.Guard.Detail)
	case s.Guard.AllowedViaFlag:
		_, _ = fmt.Fprintf(w, "guard: target non-empty (%s) — merging via --allow-non-empty: %s\n", s.Guard.Detail, MergeSemanticsNote)
	case s.Guard.ResumedRun:
		_, _ = fmt.Fprintf(w, "guard: resumed run (occupancy from this migration's earlier writes is expected)\n")
	case s.Guard.Checked && s.Guard.TargetNonEmpty:
		_, _ = fmt.Fprintf(w, "guard: target non-empty (%s)\n", s.Guard.Detail)
	case s.Guard.Checked:
		_, _ = fmt.Fprintf(w, "guard: target empty\n")
	}
	if s.Guard.Note != "" && !s.Guard.AllowedViaFlag && !s.Guard.ResumedRun {
		_, _ = fmt.Fprintf(w, "  %s\n", s.Guard.Note)
	}

	_, _ = fmt.Fprintf(w, "repos: found=%d migrated=%d skipped=%d already-done=%d failed=%d\n",
		s.Repos.Found, s.Repos.Migrated, s.Repos.Skipped, s.Repos.AlreadyDone, s.Repos.Failed)
	for _, n := range s.RepoSkips {
		_, _ = fmt.Fprintf(w, "  skipped repo %s: %s\n", n.Name, n.Reason)
	}
	for _, n := range s.RepoFailures {
		_, _ = fmt.Fprintf(w, "  FAILED repo %s: %s\n", n.Name, n.Reason)
	}
	for _, warn := range s.RepoWarnings {
		_, _ = fmt.Fprintf(w, "  warning: %s\n", warn)
	}

	_, _ = fmt.Fprintf(w, "users: found=%d migrated=%d skipped=%d already-done=%d failed=%d\n",
		s.Users.Found, s.Users.Migrated, s.Users.Skipped, s.Users.AlreadyDone, s.Users.Failed)
	for _, n := range s.UserSkips {
		_, _ = fmt.Fprintf(w, "  skipped user %s: %s\n", n.Name, n.Reason)
	}
	for _, n := range s.UserFailures {
		_, _ = fmt.Fprintf(w, "  FAILED user %s: %s\n", n.Name, n.Reason)
	}
	if s.UserPhaseWarn != "" {
		_, _ = fmt.Fprintf(w, "  warning: %s\n", s.UserPhaseWarn)
	}
	if s.PasswordsWritten > 0 {
		_, _ = fmt.Fprintf(w, "  generated passwords appended to %s (mode 0600)\n", s.PasswordsPath)
	}

	_, _ = fmt.Fprintf(w, "tokens: found=%d migrated=%d skipped=%d\n", s.TokensFound, s.TokensMigrated, s.TokensSkipped)
	_, _ = fmt.Fprintf(w, "  %s\n", s.TokenPhaseNote)
	if s.TokenPhaseWarn != "" {
		_, _ = fmt.Fprintf(w, "  warning: %s\n", s.TokenPhaseWarn)
	}

	_, _ = fmt.Fprintf(w, "artifacts: found=%d migrated=%d skipped=%d already-done=%d failed=%d bytes=%d\n",
		s.Artifacts.Found, s.Artifacts.Migrated, s.Artifacts.Skipped, s.Artifacts.AlreadyDone, s.Artifacts.Failed, s.ArtifactBytes)
	for _, st := range s.ArtifactRepos {
		if st.Reason != "" {
			_, _ = fmt.Fprintf(w, "  repo %s (%s/%s): found=%d migrated=%d skipped=%d already-done=%d failed=%d — %s\n",
				st.Repo, st.Rclass, st.PackageType, st.Found, st.Migrated, st.Skipped, st.AlreadyDone, st.Failed, st.Reason)
			continue
		}
		_, _ = fmt.Fprintf(w, "  repo %s (%s/%s): found=%d migrated=%d skipped=%d already-done=%d failed=%d\n",
			st.Repo, st.Rclass, st.PackageType, st.Found, st.Migrated, st.Skipped, st.AlreadyDone, st.Failed)
	}
	for _, n := range s.ArtifactFailures {
		_, _ = fmt.Fprintf(w, "  FAILED artifact %s: %s\n", n.Name, n.Reason)
	}
	for _, warn := range s.ArtifactWarnings {
		_, _ = fmt.Fprintf(w, "  warning: %s\n", warn)
	}

	_, _ = fmt.Fprintf(w, "progress: %s (%d repo, %d user, %d artifact recorded; resume with --resume)\n",
		s.ProgressPath, s.ProgressRepos, s.ProgressUsers, s.ProgressArtifacts)
	if s.ReportPath != "" {
		if s.ReportErr != "" {
			_, _ = fmt.Fprintf(w, "report: %s (WRITE FAILED: %s)\n", s.ReportPath, s.ReportErr)
		} else {
			_, _ = fmt.Fprintf(w, "report: %s\n", s.ReportPath)
		}
	}
}
