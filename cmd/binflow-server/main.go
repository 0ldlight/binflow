// Command binflow-server is the BinFlow artifact repository server.
//
// It owns process assembly and lifecycle (architecture sections 1 and 7.4):
//
//	config -> metadata.Open (auto-migrate + admin seed)
//	        -> storage.OpenEngine (startup sweep of expired sessions)
//	        -> auth / audit -> repo.Service -> generic adapter -> httpapi
//
// Every collaborator is constructor-injected (architecture section 2 rule 3:
// no package-level singletons); this file is the only place that knows all
// the concrete types. Subcommands:
//
//	serve    run the HTTP server (also the default when no arguments are
//	         given, PRD FR-6-AC5; SIGINT/SIGTERM stop it gracefully)
//	gc       garbage-collect unreferenced blobs (dry-run by default,
//	         ADR-0006)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/docker"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/adapter/maven"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/console"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// version and revision are overwritten at build time (goreleaser
// -X main.version=... -X main.revision=..., M5). Until stamping lands both
// report the honest dev placeholder (Q4: never emulate an Artifactory
// version).
var (
	version  = "dev"
	revision = "dev"
)

// homeEnv is the cmd-reserved environment name (architecture section 8,
// fourth exception name): the resolution root for the DEFAULT config and
// data locations. The config package deliberately ignores it.
const homeEnv = "BINFLOW_HOME" //nolint:gosec // environment variable name, not a credential

// defaultConfigName is the config file looked up under $BINFLOW_HOME when
// serve runs without -c and no ./binflow.yaml exists.
const defaultConfigName = "binflow.yaml"

// defaultAdminPassword is the documented evaluation default (ADR-0009).
// metadata seeds it when BINFLOW_ADMIN_PASSWORD is unset; serve warns when
// it is still effective (see warnDefaultAdminPassword).
const defaultAdminPassword = "password"

// errPostgresDisabled is the fail-fast startup refusal for
// metadata.driver=postgres (FR-3-AC10). M1 ships SQLite only.
var errPostgresDisabled = errors.New("postgres support is not enabled")

// usage is the --help text.
const usage = `binflow-server is the BinFlow artifact repository server.

Usage:

	binflow-server <command> [flags]

Commands:

	serve    Run the HTTP server (default when no command is given)
	gc       Garbage-collect unreferenced blobs (dry-run by default)

Flags for serve:

	-c string    Path to binflow.yaml (default "binflow.yaml"; when that
	             file is absent and BINFLOW_HOME is set, $BINFLOW_HOME/binflow.yaml)

Flags for gc:

	-c string       Path to binflow.yaml, same resolution as serve (PRD 6.4 O3)
	--apply         Actually delete blobs (default is a dry-run listing)
	--grace-days    Override storage.gc_grace for this run (positive integer)
	--grace-hours   Override storage.gc_grace with sub-day precision
	                (positive integer; wins over --grace-days)

Common flags:

	--help      Show this usage text
	--version   Show the binary version

Environment:

	BINFLOW_HOME                     resolution root for default config and
	                                 data paths (./data becomes
	                                 $BINFLOW_HOME/data)
	BINFLOW_ADMIN_PASSWORD           admin bootstrap password (env-only,
	                                 ADR-0009; unset means the documented
	                                 evaluation default "password")
	BINFLOW_STORAGE__DATA_DIR ...    every config key, see binflow.yaml
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(os.Stderr, "binflow-server: %v\n", err) //nolint:errcheck // startup failure path, no fallback exists
		}
		os.Exit(1)
	}
}

// run dispatches subcommands. args == nil (no arguments at all) prints usage
// and exits 0: the binary must be trivially inspectable (FR-1-AC1) without
// starting a server nobody asked for.
func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage) //nolint:errcheck // usage printing has no fallback if it fails
		return nil
	}

	switch args[0] {
	case "--help", "-h", "help":
		fmt.Fprint(stdout, usage) //nolint:errcheck // usage printing has no fallback if it fails
		return nil
	case "--version":
		fmt.Fprintf(stdout, "binflow-server %s (revision %s)\n", version, revision) //nolint:errcheck // usage printing has no fallback if it fails
		return nil
	case "serve":
		return runServe(args[1:], stderr)
	case "gc":
		return runGC(args[1:], stderr)
	default:
		return fmt.Errorf("unknown command %q, see --help for usage", args[0])
	}
}

// runServe loads the config and runs the assembled stack until SIGINT or
// SIGTERM arrives. Startup order follows architecture section 7.4 read
// backwards: config, logger, metadata (migrations + admin seed), storage
// (startup session sweep), services, HTTP. Shutdown runs the same list in
// reverse: HTTP drain, storage, metadata, exit 0.
func runServe(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "", "path to binflow.yaml (default binflow.yaml, or $BINFLOW_HOME/binflow.yaml)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing serve flags: %w", err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q, see --help for usage", fs.Arg(0))
	}

	cfg, err := loadServeConfig(*configPath)
	if err != nil {
		return err
	}

	logger, err := newLogger(cfg, stderr)
	if err != nil {
		return err
	}
	slog.SetDefault(logger)

	logger.Info("binflow starting",
		"version", version,
		"revision", revision,
		"config", describeConfigPath(*configPath),
		"listen", cfg.Server.Listen,
		"data_dir", cfg.Storage.DataDir,
		"driver", cfg.Metadata.Driver,
	)

	stack, err := openStack(context.Background(), cfg, logger)
	if err != nil {
		return err
	}
	// closeStack inverts the open order (HTTP -> storage -> metadata,
	// architecture section 7.4). It is registered once here so every
	// failure path after openStack shares one teardown sequence; serve's
	// regular shutdown reaches it through the deferred call below.
	defer stack.close(logger)

	warnDefaultAdminPassword(context.Background(), stack, logger)

	srv := newAssembledServer(cfg, stack, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = srv.Run(ctx)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	logger.Info("binflow stopped")
	return nil
}

// newAssembledServer builds the HTTP surface over an opened stack. serve
// and the tests share this one composition so the tested server is the
// served server (T-15's Deps seam list: ReposSvc / Passwords / Tokens).
func newAssembledServer(cfg *config.Config, stack *stack, logger *slog.Logger) *httpapi.Server {
	dockerHandler := docker.New(stack.svc, docker.NewRepoLookup(stack.md.Repos()),
		stack.authSvc, stack.authSvc, stack.md.Users(), docker.Options{
			AnonymousAccess: cfg.Security.AnonymousAccess,
			BaseURL:         cfg.Server.BaseURL,
			TokenTTL:        cfg.Auth.TokenDefaultTTL,
		}, logger).
		WithStorage(stack.st, stack.md.Blobs())
	// Maven (M3/T-67): same content namespace as generic — httpapi
	// dispatches on the repository row's package type, so mounting is the
	// whole wiring. The metadata-provider registration feeds the registry
	// T-66/T-68/T-72 consume (T-63 seam).
	mavenHandler := maven.New(stack.svc, stack.md.Repos(), stack.md.Blobs())
	maven.RegisterMetadata()
	return httpapi.New(httpapi.Deps{
		Config:    cfg,
		Auth:      stack.authSvc,
		Authz:     stack.authSvc,
		Metadata:  stack.md,
		Repos:     stack.md.Repos(),
		ReposSvc:  stack.svc,
		Passwords: stack.authSvc,
		Tokens:    stack.authSvc,
		DataDir:   cfg.Storage.DataDir,
		Console:   console.Handler(),
		Adapters:  []adapter.Handler{stack.genericHandler, dockerHandler, mavenHandler},
		Version:   version,
		Revision:  revision,
	}, logger)
}

// loadServeConfig resolves the config path and loads it. An explicitly
// passed -c path must exist (fail fast); the default lookup falls back to
// $BINFLOW_HOME/binflow.yaml when ./binflow.yaml is absent, and a totally
// absent config boots on defaults plus environment (FR-6-AC5: the bare
// binary must run).
func loadServeConfig(explicit string) (*config.Config, error) {
	home := homeDir()
	candidates := []string{}
	switch {
	case explicit != "":
		candidates = append(candidates, explicit)
	default:
		candidates = append(candidates, defaultConfigName)
		if home != "" {
			candidates = append(candidates, filepath.Join(home, defaultConfigName))
		}
	}

	for i, path := range candidates {
		if _, err := os.Stat(path); err != nil {
			if i == 0 && explicit != "" {
				// -c is an operator statement: a missing file is an error,
				// never a silent fallback to defaults.
				return nil, fmt.Errorf("config file %s: %w", path, err)
			}
			continue
		}
		cfg, err := config.Load(path)
		if err != nil {
			return nil, err
		}
		return resolveHome(cfg, home), nil
	}

	// No config file anywhere: defaults + environment. Load resolves env
	// overrides and validates, so the result is fully populated.
	cfg := config.Defaults()
	if err := applyEnvDefaults(cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return resolveHome(cfg, home), nil
}

// applyEnvDefaults applies BINFLOW_-prefixed environment overrides onto a
// hand-constructed Config (the no-config-file boot path). It reuses the
// config package's env mapping by round-tripping through an empty temp
// file: Load(path of an empty file) = defaults + env + validation, which is
// exactly this path's semantics without exporting config internals.
func applyEnvDefaults(cfg *config.Config) error {
	tmp, err := os.CreateTemp("", "binflow-empty-config-*.yaml")
	if err != nil {
		return fmt.Errorf("config: preparing empty config for env-only load: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: closing empty config file: %w", err)
	}
	loaded, err := config.Load(tmp.Name())
	if err != nil {
		return err
	}
	*cfg = *loaded
	return nil
}

// resolveHome anchors the default data directory (and the default sqlite
// path) at $BINFLOW_HOME. Relative operator-provided paths pass through
// untouched: DATA_DIR is a direct assignment and outranks the HOME-derived
// default (architecture section 8 exception-name boundary).
//
// The default spelling is exactly config.DefaultDataDir ("./data"), compared
// as a literal so an explicit "./data" in YAML is treated as the same
// default (both mean "data next to the working directory"); any other
// spelling — including BINFLOW_DATA_DIR — is operator intent and wins.
func resolveHome(cfg *config.Config, home string) *config.Config {
	if home == "" || cfg.Storage.DataDir != config.DefaultDataDir {
		return cfg
	}
	cfg.Storage.DataDir = filepath.Join(home, "data")
	return cfg
}

// homeDir reads the cmd-reserved environment variable.
func homeDir() string { return os.Getenv(homeEnv) }

// configDefaults is the test-side spelling of config.Defaults (kept here so
// tests read as assembly code, not package internals).
func configDefaults() *config.Config { return config.Defaults() }

// describeConfigPath renders the config provenance for the startup line.
func describeConfigPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return "defaults (no " + defaultConfigName + " found; environment overrides applied)"
}

// stack holds every opened collaborator plus its teardown. The open order
// is recorded in close so section 7.4's shutdown order cannot drift from
// it.
type stack struct {
	md             metadata.Store
	st             storage.Engine
	authSvc        *auth.Service
	auditLog       audit.Logger
	svc            repo.Service
	genericHandler *generic.Handler

	dataDir string
	closed  bool
}

// openStack builds the whole collaborator graph. Migrations and the admin
// seed happen inside metadata.Open (ADR-0007/ADR-0009); the startup sweep
// of expired upload sessions happens inside storage.OpenEngine
// (ADR-0006). Both are idempotent.
func openStack(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*stack, error) {
	if cfg.Metadata.Driver == config.DriverPostgres {
		// FR-3-AC10: refuse to boot with a clear, greppable message. The
		// config enum accepts postgres (pass-through), so this is the one
		// gate; it runs before anything is opened.
		logger.Error("Postgres support is not enabled",
			"message", "postgres support is not enabled in this build (M1 ships SQLite only); set metadata.driver: sqlite")
		return nil, fmt.Errorf("metadata.driver=%s: %w", cfg.Metadata.Driver, errPostgresDisabled)
	}

	md, err := metadata.Open(ctx, metadata.Options{
		Driver:        cfg.Metadata.Driver,
		DSN:           sqlitePath(cfg),
		AdminPassword: cfg.AdminPassword,
	})
	if err != nil {
		return nil, fmt.Errorf("opening metadata: %w", err)
	}

	st, err := storage.OpenEngine(cfg.Storage.DataDir, storage.Options{
		SessionTTL: cfg.Storage.SessionTTL,
	})
	if err != nil {
		_ = md.Close()
		return nil, fmt.Errorf("opening storage engine at %s: %w", cfg.Storage.DataDir, err)
	}
	logger.Info("startup session sweep complete",
		"data_dir", cfg.Storage.DataDir, "session_ttl", cfg.Storage.SessionTTL.String())

	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	auditLog := audit.New(md, cfg.Audit.Enabled)
	svc := repo.New(st, md, authSvc, auditLog)

	return &stack{
		md:             md,
		st:             st,
		authSvc:        authSvc,
		auditLog:       auditLog,
		svc:            svc,
		genericHandler: generic.New(svc, md.Blobs()),
		dataDir:        cfg.Storage.DataDir,
	}, nil
}

// sqlitePath resolves the sqlite DSN: an explicit metadata.dsn wins;
// otherwise the database lives in the data directory next to the blobs
// (architecture section 4.1 layout).
func sqlitePath(cfg *config.Config) string {
	if cfg.Metadata.DSN != "" {
		return cfg.Metadata.DSN
	}
	return filepath.Join(cfg.Storage.DataDir, "binflow.db")
}

// close tears the stack down in the section 7.4 order. It is safe to call
// twice (both closers are idempotent).
func (s *stack) close(logger *slog.Logger) {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	if s.st != nil {
		if err := s.st.Close(); err != nil {
			logger.Error("closing storage engine", "error", err.Error())
		}
	}
	if s.md != nil {
		if err := s.md.Close(); err != nil {
			logger.Error("closing metadata", "error", err.Error())
		}
	}
}

// warnDefaultAdminPassword logs the ADR-0009 WARN when the admin account
// still authenticates with the documented default password: either the
// operator booted without BINFLOW_ADMIN_PASSWORD, or an evaluation instance
// never rotated it. The check is a real authentication, never a hash
// comparison — the plaintext is not recoverable and must not be.
func warnDefaultAdminPassword(ctx context.Context, s *stack, logger *slog.Logger) {
	req, err := http.NewRequest(http.MethodGet, "/", nil)
	if err != nil {
		return // unreachable: a constant URL cannot fail to build
	}
	req.SetBasicAuth("admin", defaultAdminPassword)
	if p, err := s.authSvc.Authenticate(ctx, req); err == nil && p != nil && p.Name == "admin" && p.Admin {
		logger.Warn("admin account is using the default password",
			"message", "admin is using the documented default password \"password\" (evaluation only); set BINFLOW_ADMIN_PASSWORD and restart, or change the password via PUT /binflow/api/security/password",
		)
	}
}

// runGC implements the gc subcommand (ADR-0006): mark (the set of sha256
// values referenced by nodes ∪ docker_refs, architecture sections 4.4 and
// 11.12) then sweep (storage scans blobs/ and keeps blobs inside the grace
// window). Default is a dry-run listing; --apply deletes for real, and after
// a successful sweep the blobs ledger rows of the deleted checksums are
// dropped too.
//
// Flags (PRD 6.4 O3): -c resolves the config exactly like serve (explicit
// path must exist, otherwise the ./binflow.yaml → $BINFLOW_HOME/binflow.yaml
// → defaults cascade); --grace-hours overrides the configured grace with
// sub-day precision and, when both are given, wins over --grace-days —
// "hours" is the strictly more precise spelling of the same override, so
// there is no ambiguity to reject.
func runGC(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "", "path to binflow.yaml (same resolution as serve)")
	apply := fs.Bool("apply", false, "delete blobs instead of dry-run listing")
	graceDays := fs.Int("grace-days", 0, "override storage.gc_grace for this run in days (positive integer, 0 = use config)")
	graceHours := fs.Int("grace-hours", 0, "override storage.gc_grace for this run in hours, sub-day precision (positive integer, 0 = use config; wins over --grace-days)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing gc flags: %w", err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q, see --help for usage", fs.Arg(0))
	}
	if *graceDays < 0 {
		return fmt.Errorf("gc: --grace-days must be a positive integer, got %d", *graceDays)
	}
	if *graceHours < 0 {
		return fmt.Errorf("gc: --grace-hours must be a positive integer, got %d", *graceHours)
	}

	ctx := context.Background()

	cfg, err := loadServeConfig(*configPath)
	if err != nil {
		return err
	}
	grace := cfg.Storage.GCGrace
	if *graceDays > 0 {
		grace = time.Duration(*graceDays) * 24 * time.Hour
	}
	if *graceHours > 0 {
		grace = time.Duration(*graceHours) * time.Hour
	}

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	st, err := storage.OpenEngine(cfg.Storage.DataDir, storage.Options{SessionTTL: cfg.Storage.SessionTTL})
	if err != nil {
		return fmt.Errorf("gc: opening storage engine at %s: %w", cfg.Storage.DataDir, err)
	}
	defer func() { _ = st.Close() }()

	md, err := metadata.Open(ctx, metadata.Options{
		Driver:        cfg.Metadata.Driver,
		DSN:           sqlitePath(cfg),
		AdminPassword: cfg.AdminPassword,
	})
	if err != nil {
		return fmt.Errorf("gc: opening metadata: %w", err)
	}
	defer func() { _ = md.Close() }()

	referenced := func() (map[string]struct{}, error) {
		set, err := liveChecksumSet(ctx, md)
		if err != nil {
			return nil, fmt.Errorf("gc: referenced set: %w", err)
		}
		return set, nil
	}

	candidates, err := st.GC(ctx, referenced, grace, *apply)
	if err != nil {
		return fmt.Errorf("gc: %w", err)
	}

	mode := "dry-run"
	if *apply {
		mode = "apply"
	}
	writeCLIReport(stderr, "gc: mode=%s grace=%s data_dir=%s candidates=%d\n",
		mode, grace, cfg.Storage.DataDir, len(candidates))
	for _, sha := range candidates {
		writeCLIReport(stderr, "%s\n", sha)
	}

	// A real deletion removes the blobs-ledger rows with it: the ledger is
	// the "ever existed" record (ADR-0006), and a row whose physical file
	// is gone must not survive as a phantom.
	if *apply {
		for _, sha := range candidates {
			if err := md.Blobs().Delete(ctx, sha); err != nil && !errors.Is(err, metadata.ErrNotFound) {
				logger.Warn("gc: dropping blobs ledger row failed", "sha256", sha, "error", err.Error())
			}
		}
	}
	return nil
}

// liveChecksumSet returns the GC mark set: every sha256 referenced by any
// node row UNION every blob_digest referenced by any docker_refs row
// (architecture sections 4.4 and 11.12 — the SQL shape is "SELECT DISTINCT
// sha256 FROM nodes UNION SELECT DISTINCT blob_digest FROM docker_refs").
// docker_refs keys blobs by bare hex in the same keyspace, so the union is
// direct.
//
// cmd has no SQL handle by design, so both sides go through public store
// surfaces: the nodes half walks the NodeStore prefix listing per repository
// (repo-key scoped, path-ordered — the same rows a SELECT DISTINCT would
// yield); the refs half walks the DockerStore manifest index per (repo,
// image) and each manifest's ref rows, aggregated here because the Docker
// sub-store exposes no all-refs listing (by design: nothing else needs one).
// RefsByBlob is per (repoKey, blobDigest); using it for the mark set would
// need one EXISTS query per on-disk blob, which is the anti-join round the
// T-9 set-form callback retired.
//
// BlobStore.FilterUnreferenced is deliberately NOT used — since the T-9
// set-form callback it is the reconciliation path, not the GC mark
// (architecture section 3.2 note), and it anti-joins against nodes only and
// so would ignore docker_refs liveness anyway.
func liveChecksumSet(ctx context.Context, md metadata.Store) (map[string]struct{}, error) {
	set := map[string]struct{}{}

	repos, err := md.Repos().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing repositories: %w", err)
	}
	for _, r := range repos {
		nodes, err := md.Nodes().ListByPrefix(ctx, r.RepoKey, "")
		if err != nil {
			return nil, fmt.Errorf("listing nodes of %s: %w", r.RepoKey, err)
		}
		for _, n := range nodes {
			if n.Sha256 != "" {
				set[n.Sha256] = struct{}{}
			}
		}

		images, err := md.Docker().ListImages(ctx, r.RepoKey, "", 0)
		if err != nil {
			return nil, fmt.Errorf("listing docker images of %s: %w", r.RepoKey, err)
		}
		for _, image := range images {
			manifests, err := md.Docker().ListManifestsByImage(ctx, r.RepoKey, image)
			if err != nil {
				return nil, fmt.Errorf("listing manifests of %s/%s: %w", r.RepoKey, image, err)
			}
			for _, m := range manifests {
				refs, err := md.Docker().ListRefsByManifest(ctx, r.RepoKey, image, m.Digest)
				if err != nil {
					return nil, fmt.Errorf("listing refs of %s/%s@%s: %w", r.RepoKey, image, m.Digest, err)
				}
				for _, ref := range refs {
					if ref.BlobDigest != "" {
						set[ref.BlobDigest] = struct{}{}
					}
				}
			}
		}
	}
	return set, nil
}

// newLogger builds the process logger from logging.level / logging.format
// (architecture section 8). Everything lands on stderr: stdout belongs to
// 12-factor collection pipelines and the gc listing.
func newLogger(cfg *config.Config, w io.Writer) (*slog.Logger, error) {
	var level slog.Level
	switch cfg.Logging.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return nil, fmt.Errorf("config: logging.level %q is not a level (want debug, info, warn or error)", cfg.Logging.Level)
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if cfg.Logging.Format == "console" {
		h = slog.NewTextHandler(w, opts)
	} else {
		h = slog.NewJSONHandler(w, opts)
	}
	return slog.New(h), nil
}

// writeCLIReport prints an operator-facing line. Write errors on a CLI
// stream mean the consumer is gone (piped to head, terminal closed); there
// is no fallback surface, so they are swallowed.
func writeCLIReport(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
