// Command bf-migrate is the BinFlow Artifactory migration tool.
//
// It reads repositories, users, token accounting data and repository
// content from an Artifactory instance over its REST API and writes what
// is migratable into a BinFlow instance through internal/client (T-167;
// the artifact phase, empty-target guard and run report landed with
// T-196). The migration runs in four phases — repos, users, tokens,
// artifacts. The first three are ordered by their references (virtual
// repositories reference migrated members; tokens reference users); the
// artifact phase runs last so configuration failures surface before the
// bulk data copy starts.
//
// Semantics:
//
//   - The target must be EMPTY (zero repositories) unless --allow-non-empty
//     is passed; a populated target refuses the run with a non-zero exit
//     (FR-63-AC1). --resume of this same endpoint pair is exempt — the
//     occupancy is this migration's own earlier writes.
//   - --dry-run performs every read and conversion but no writes; the
//     progress file is not touched.
//   - Progress is checkpointed per item (artifacts in batches); --resume
//     reloads the checkpoint and skips completed items. Every target write
//     verb is create-or-replace, so re-running a completed item is
//     harmless.
//   - Artifact copying: generic repositories fully (list -> download ->
//     digest-verify -> upload, --concurrency workers); maven too (plain
//     layout files); docker/npm/pypi repository CONFIGURATIONS migrate but
//     their artifacts are left behind with a recorded reason (their upload
//     faces are protocol-specific).
//   - Token values are never migratable (Artifactory's listing is
//     metadata-only); the token phase accounts and skips them.
//   - User passwords are never exportable either: pass --password-env
//     (one shared password) or --passwords-out (generated per-user
//     passwords written to a 0600 file).
//   - --skip-users makes the users phase optional: a REFUSED listing
//     (HTTP 400/403 — the Artifactory OSS license gate or non-admin
//     credentials) is degraded to a recorded warning and the run continues
//     without users; a readable listing still migrates them.
//   - Every run writes migration_report.json (path overridable) recording
//     per-phase counts, per-repository artifact rows, skip reasons and
//     failure lists.
//
// Acceptance against a real Artifactory instance is a conditional leg
// (ticket Q9, pending the user's environment decision); the reader is
// validated against mock servers implementing the documented REST shapes
// (docs/reverse/rest-api.md, docs/reverse/auth-model.md).
//
// Version is injected at build time via ldflags (same source as
// binflow-server and bf: .goreleaser.yaml or the Makefile release
// targets). A bare build leaves the honest dev placeholder in place.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lzwzzy/binflow/internal/client"
	"github.com/lzwzzy/binflow/internal/migrate"
)

// version and revision are stamped at build time by the release faces
// (goreleaser ldflags -X main.version/-X main.revision, T-127/FR-34, T-148).
// A bare build leaves the dev placeholder.
var (
	version  = "dev"
	revision = "dev"
)

const usage = `bf-migrate is the BinFlow Artifactory migration tool.

Usage:

	bf-migrate <command> [flags]

Commands:

	migrate      Migrate repositories, users, token accounting and
	             repository content from an Artifactory instance into
	             BinFlow (four phases: repos -> users -> tokens ->
	             artifacts)
	help         Show this help text

Common flags:

	--help, -h     Show this usage text
	--version      Show the binary version

Run "bf-migrate migrate --help" for the migration flags.

Environment:

	ARTIFACTORY_URL           Source base URL, including the context path
	                          when deployed (e.g. http://host:8081/artifactory)
	ARTIFACTORY_TOKEN         Source bearer token (preferred credential)
	ARTIFACTORY_API_KEY       Source API key (X-JFrog-Art-Api; used when
	                          ARTIFACTORY_TOKEN is unset)
	BINFLOW_SERVER_URL        Target BinFlow base URL (default http://localhost:8080)
	BINFLOW_TOKEN             Target admin API token (bearer)
	BF_TOKEN                  Alias for BINFLOW_TOKEN
`

//nolint:gosec // G101 false positive: the help text names credential flags; no credential value is hardcoded.
const migrateUsage = `bf-migrate migrate — migrate an Artifactory instance into BinFlow.

Phases (always in this order):

	repos     local/remote/virtual repositories (federated and unsupported
	          package types are skipped with a recorded reason)
	users     internal-realm users (anonymous, _system_ and identity-provider
	          realms are skipped); passwords are NOT exportable, so pass
	          --password-env or --passwords-out; --skip-users degrades a
	          REFUSED listing (HTTP 400/403 — OSS license gate, non-admin
	          credentials) to a warning and continues without users
	tokens    accounting only: Artifactory's token listing is metadata-only,
	          token values cannot be exported — recreate them on the target
	artifacts repository content: generic (and maven layout) repositories
	          are copied file by file with digest verification; docker/npm/
	          pypi repository configurations migrate but their artifacts
	          are left behind with a recorded reason (protocol-specific
	          upload faces)

The target must be EMPTY (zero repositories). A populated target refuses
the run with a non-zero exit; pass --allow-non-empty to merge anyway.
Resuming (--resume) this same endpoint pair is exempt: the occupancy is
this migration's own earlier writes.

Usage:

	bf-migrate migrate [flags]

Flags:

	--artifactory-url <url>        Source base URL including the context
	                                   path, e.g. http://host:8081/artifactory
	                                   (default $ARTIFACTORY_URL; required)
	--artifactory-token-env <var>  Env var holding the source bearer token
	                                   (default ARTIFACTORY_TOKEN)
	--artifactory-api-key-env <var>
	                                 Env var holding the source API key
	                                   (default ARTIFACTORY_API_KEY; used
	                                   only when the token env is unset)
	--server <url>                 Target BinFlow base URL (default
	                                   $BINFLOW_SERVER_URL, else
	                                   http://localhost:8080)
	--token-env <var>              Env var holding the target admin token
	                                   (default: BINFLOW_TOKEN, else BF_TOKEN;
	                                   required unless --dry-run)
	--retry-max <n>                Retries for transient target failures
	                                   (default 5; negative disables)
	--allow-non-empty              Merge into a populated target: repositories
	                                   with matching keys are reconfigured and
	                                   same-path artifacts overwritten; other
	                                   target content is left untouched
	--concurrency <n>              Parallel artifact copies per repository
	                                   (default 4)
	--dry-run                      Count and report only: no target writes,
	                                   no progress file
	--resume                       Skip items recorded in the progress file
	                                   (crashed runs resume where they left
	                                   off; failed items are retried)
	--progress-file <path>         Checkpoint file (default
	                                   .bf-migrate-progress.json)
	--report-file <path>           Run report (default
	                                   migration_report.json; per-phase counts,
	                                   per-repo artifact rows, skip reasons,
	                                   failure lists)
	--password-env <var>           Shared password assigned to every
	                                   migrated user (rotate afterwards)
	--passwords-out <file>         Write one generated random password per
	                                   migrated user as "<name> <password>"
	                                   lines (mode 0600, appended on resume)
	--skip-users                   Treat the users phase as optional: when the
	                                   source refuses the user listing (HTTP
	                                   400/403 — the Artifactory OSS license
	                                   gate or non-admin credentials), skip
	                                   the phase with a recorded warning
	                                   instead of aborting the run; a
	                                   readable listing still migrates users
	--help, -h                     Show this help text

Examples:

	# See what would happen (no credentials needed on the target):
	bf-migrate migrate --artifactory-url http://src:8081/artifactory --dry-run

	# Full run with generated per-user passwords:
	bf-migrate migrate --artifactory-url http://src:8081/artifactory \
	    --server http://binflow:8080 --passwords-out ./migrated-passwords.txt

	# Merge into a target that already holds content:
	bf-migrate migrate --artifactory-url http://src:8081/artifactory \
	    --server http://binflow:8080 --allow-non-empty

	# Resume a crashed run:
	bf-migrate migrate --artifactory-url http://src:8081/artifactory \
	    --server http://binflow:8080 --passwords-out ./migrated-passwords.txt \
	    --resume

	# Artifactory OSS source (the user API is license-gated there):
	bf-migrate migrate --artifactory-url http://src:8081/artifactory \
	    --server http://binflow:8080 --skip-users

Notes:

	- Remote repositories migrate without their upstream password
	  (Artifactory never echoes credentials); re-enter it on the target.
	- Groups referenced by migrated users must already exist on the
	  target (an unknown group fails that user with 400).
	- A fresh run without --resume resets the progress file.
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintf(os.Stderr, "bf-migrate: %v\n", err)
		}
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stdout, usage)
		return nil
	}

	switch args[0] {
	case "--help", "-h", "help":
		_, _ = fmt.Fprint(stdout, usage)
		return nil
	case "--version":
		_, _ = fmt.Fprintf(stdout, "bf-migrate %s (%s)\n", version, revision)
		return nil
	case "migrate":
		return ignoreHelp(runMigrate(args[1:], stdout, stderr))
	default:
		return fmt.Errorf("unknown command %q, see --help for usage", args[0])
	}
}

// ignoreHelp maps flag.ErrHelp to success: the subcommand's FlagSet has
// already printed its help text (via fs.Usage), so -h exits zero.
func ignoreHelp(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

// migrateFlags is the parsed flag set of the migrate subcommand.
type migrateFlags struct {
	artifactoryURL     string
	artifactoryTokenEv string
	artifactoryAPIKey  string
	server             string
	tokenEnv           string
	retryMax           int
	allowNonEmpty      bool
	concurrency        int
	dryRun             bool
	resume             bool
	progressFile       string
	reportFile         string
	passwordEnv        string
	passwordsOut       string
	skipUsers          bool
}

// runMigrate wires the flags into a migrate.Run call.
func runMigrate(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("bf-migrate migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { _, _ = fmt.Fprint(stdout, migrateUsage) }
	var f migrateFlags
	fs.StringVar(&f.artifactoryURL, "artifactory-url", os.Getenv("ARTIFACTORY_URL"), "source base URL including the context path")
	fs.StringVar(&f.artifactoryTokenEv, "artifactory-token-env", "ARTIFACTORY_TOKEN", "env var holding the source bearer token")
	fs.StringVar(&f.artifactoryAPIKey, "artifactory-api-key-env", "ARTIFACTORY_API_KEY", "env var holding the source API key")
	fs.StringVar(&f.server, "server", serverDefault(), "target BinFlow server base URL")
	fs.StringVar(&f.tokenEnv, "token-env", "", "env var holding the target admin token (default BINFLOW_TOKEN, else BF_TOKEN)")
	fs.IntVar(&f.retryMax, "retry-max", client.DefaultRetryMax, "retries for transient target failures (negative disables)")
	fs.BoolVar(&f.allowNonEmpty, "allow-non-empty", false, "merge into a populated target (create-or-replace semantics)")
	fs.IntVar(&f.concurrency, "concurrency", migrate.DefaultArtifactConcurrency, "parallel artifact copies per repository")
	fs.BoolVar(&f.dryRun, "dry-run", false, "count and report only, no writes")
	fs.BoolVar(&f.resume, "resume", false, "skip items recorded in the progress file")
	fs.StringVar(&f.progressFile, "progress-file", ".bf-migrate-progress.json", "checkpoint file path")
	fs.StringVar(&f.reportFile, "report-file", migrate.DefaultReportPath, "run report file path (migration_report.json shape)")
	fs.StringVar(&f.passwordEnv, "password-env", "", "env var holding a shared password for migrated users")
	fs.StringVar(&f.passwordsOut, "passwords-out", "", "file to write generated per-user passwords to (0600)")
	fs.BoolVar(&f.skipUsers, "skip-users", false, "treat the users phase as optional: a refused listing (HTTP 400/403) is skipped with a warning instead of aborting")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("bf-migrate migrate: %w", err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("bf-migrate migrate takes no positional arguments, got %d", fs.NArg())
	}
	return doMigrate(f, stdout, stderr)
}

// serverDefault resolves the target base URL from the environment.
func serverDefault() string {
	if v := strings.TrimSpace(os.Getenv("BINFLOW_SERVER_URL")); v != "" {
		return v
	}
	return client.DefaultBaseURL
}

// doMigrate resolves credentials, builds the reader/writer pair and runs
// the migration.
func doMigrate(f migrateFlags, stdout, stderr io.Writer) error {
	sourceURL := strings.TrimSpace(f.artifactoryURL)
	if sourceURL == "" {
		return fmt.Errorf("bf-migrate migrate: --artifactory-url is required (or set ARTIFACTORY_URL); it must include the context path, e.g. http://host:8081/artifactory")
	}

	// Source credentials: bearer token wins over API key. Neither is a
	// hard error — anonymous-readable instances exist; auth failures
	// surface as phase errors with the server's own status.
	sourceToken := strings.TrimSpace(os.Getenv(f.artifactoryTokenEv))
	sourceAPIKey := strings.TrimSpace(os.Getenv(f.artifactoryAPIKey))
	if sourceToken == "" && sourceAPIKey == "" {
		_, _ = fmt.Fprintf(stderr, "bf-migrate: warning: no Artifactory credentials (set %s or %s); proceeding, auth failures will abort the affected phase\n",
			f.artifactoryTokenEv, f.artifactoryAPIKey)
	}
	rd, err := migrate.NewSourceReader(migrate.SourceConfig{
		BaseURL: sourceURL,
		APIKey:  sourceAPIKey,
		Token:   sourceToken,
	})
	if err != nil {
		return err
	}

	// Target credentials: the write plane is admin-only, so a missing
	// token fails every write — fail fast instead (dry-run needs none).
	targetToken, err := resolveTargetToken(f.tokenEnv)
	if err != nil {
		return err
	}
	if targetToken == "" && !f.dryRun {
		return fmt.Errorf("bf-migrate migrate: no BinFlow credentials: pass --token-env <var> or set BINFLOW_TOKEN/BF_TOKEN (required for writes; --dry-run needs none)")
	}
	wr := migrate.NewTargetWriter(migrate.TargetConfig{
		BaseURL:  strings.TrimSpace(f.server),
		Token:    targetToken,
		RetryMax: f.retryMax,
	})

	_, err = migrate.Run(context.Background(), rd, wr, migrate.Options{
		DryRun:              f.dryRun,
		Resume:              f.resume,
		AllowNonEmpty:       f.allowNonEmpty,
		ProgressPath:        f.progressFile,
		ReportPath:          f.reportFile,
		ArtifactConcurrency: f.concurrency,
		ToolVersion:         version,
		PasswordEnv:         f.passwordEnv,
		PasswordsOut:        f.passwordsOut,
		SkipUsers:           f.skipUsers,
		Stdout:              stdout,
	})
	return err
}

// resolveTargetToken resolves the BinFlow admin token: an explicit
// --token-env var wins, then BINFLOW_TOKEN, then BF_TOKEN.
func resolveTargetToken(tokenEnv string) (string, error) {
	if tokenEnv != "" {
		v := strings.TrimSpace(os.Getenv(tokenEnv))
		if v == "" {
			return "", fmt.Errorf("bf-migrate migrate: --token-env %s is not set or empty", tokenEnv)
		}
		return v, nil
	}
	for _, name := range []string{"BINFLOW_TOKEN", "BF_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v, nil
		}
	}
	return "", nil
}
