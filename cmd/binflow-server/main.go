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
//	export   write an online backup of the running instance (FR-32)
//	import   restore a backup into an empty data directory (FR-32)
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/docker"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/adapter/maven"
	"github.com/lzwzzy/binflow/internal/adapter/npm"
	"github.com/lzwzzy/binflow/internal/adapter/pypi"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/console"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/replication"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	_ "modernc.org/sqlite" // driver for the replication store's own connection
)

// version and revision are stamped at build time by the release faces
// (goreleaser ldflags -X main.version/-X main.revision, T-127/FR-34; the
// Makefile release targets own the baseline, VER ?= v1.0.0 — Q4). A bare
// build leaves the honest dev placeholder in place (never emulate an
// Artifactory version).
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
	export   Write an online backup (runs while the server is up)
	import   Restore a backup into an empty data directory (server stopped)

Flags for serve:

	-c string    Path to binflow.yaml (default "binflow.yaml"; when that
	             file is absent and BINFLOW_HOME is set, $BINFLOW_HOME/binflow.yaml)

Flags for gc:

	-c string       Path to binflow.yaml, same resolution as serve (PRD 6.4 O3)
	--apply         Actually delete blobs (default is a dry-run listing)
	--grace-days    Override storage.gc_grace for this run (positive integer)
	--grace-hours   Override storage.gc_grace with sub-day precision
	                (positive integer; wins over --grace-days)

Flags for export:

	-c string     Path to binflow.yaml, same resolution as serve
	--output      Directory to write the backup into (must not exist, or
	              exist empty; created with 0700 — the artifact carries
	              password hashes and encrypted credentials, NFR-S22)
	--tar         Not implemented in M4 (P2 backlog); directory artifact only

Flags for import:

	-c string     Path to binflow.yaml, same resolution as serve
	--input       Backup directory produced by export
	--verify      Integrity mode: "spot" (default; size for every blob,
	              sha256 for the first 100) or "full" (rehash every blob)

Notes on backup/restore (FR-32): export holds the data-directory maintenance
lock, so it never overlaps a GC run; the SQLite snapshot is taken BEFORE the
blob copy and blob mtimes are preserved (the GC grace clock never resets).
import restores into an empty data directory only, verifies the whole backup
before writing, and on any failure clears the target back to empty.

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
		// FR-34-AC2/G02 banner: "binflow-server <VER> (<git short sha>)" —
		// bare builds pin the same shape with the dev fallback.
		fmt.Fprintf(stdout, "binflow-server %s (%s)\n", version, revision) //nolint:errcheck // usage printing has no fallback if it fails
		return nil
	case "serve":
		return runServe(args[1:], stderr)
	case "gc":
		return runGC(args[1:], stderr)
	case "export":
		return runExport(args[1:], stderr)
	case "import":
		return runImport(args[1:], stderr)
	default:
		return fmt.Errorf("unknown command %q, see --help for usage", args[0])
	}
}

// runServe loads the config and runs the assembled stack until SIGINT or
// SIGTERM arrives. Startup order follows architecture section 7.4 read
// backwards: config, logger, metadata (migrations + admin seed), storage
// (startup session sweep), services, HTTP. Shutdown runs the same list in
// reverse: HTTP drain (graceful period 30s), close Engine, close metadata
// Store, exit 0.
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
	// closeStack inverts the open order (Engine -> metadata, architecture
	// section 7.4). It is registered as a deferred safety net so every
	// failure path after openStack shares one teardown sequence; the regular
	// graceful path reaches it through the explicit call below, and the
	// closed flag makes the double call harmless.
	defer stack.close(logger)

	warnDefaultAdminPassword(context.Background(), stack, logger)

	srv := newAssembledServer(cfg, stack, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The push-replication worker (T-180, ADR-0021) runs for the server's
	// lifetime: an initial drain recovers tasks a previous process left
	// pending, then wake signals and the sweep ticker keep the ledger
	// flowing. drainReplication cancels the engine context and WAITS for
	// Run to return — every task is reverted to pending and the blob reads
	// stop — before the storage engine closes underneath it.
	drainReplication := stack.startReplication(ctx, logger)

	err = srv.Run(ctx)
	if err != nil {
		drainReplication()
		return fmt.Errorf("serve: %w", err)
	}

	// Signal received and HTTP server drained; now close the remaining
	// collaborators in the section 7.4 order. The "shutting down gracefully"
	// message is the operator-facing signal that the drain has completed and
	// the final teardown is beginning.
	logger.Info("shutting down gracefully")
	drainReplication()
	stack.close(logger)
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
	// T-66/T-68/T-72 consume (T-63 seam); the nodes seam feeds the
	// maven-metadata.xml calculator (T-68/FR-17).
	mavenHandler := maven.New(stack.svc, stack.md.Repos(), stack.md.Blobs(), stack.md.Nodes())
	maven.RegisterMetadata()
	// npm (M3/T-69): the /binflow/api/npm mount rewrites onto the content
	// plane, so mounting is the Deps.Adapters entry; Register is the
	// one-call hook that also feeds the metadata registry T-66/T-72 consume.
	// WithAuth wires login (NE-06: TokenRegistry + user directory + the
	// early write gate — the /v2/token dual-entry precedent).
	npmHandler := npm.New(stack.svc, stack.md.Repos(), npm.Options{BaseURL: cfg.Server.BaseURL}).
		WithAuth(stack.authSvc, stack.md.Users(), stack.authSvc).
		WithLedger(stack.md.Blobs())
	npm.Register(npmHandler)
	// pypi (M3/T-70): Register builds the handler and enters both
	// registries under one literal; the upload plane rides the storage
	// engine seam (PutLandedBlob's late-path-binding shape, T-64).
	pypiHandler := pypi.Register(stack.svc, stack.md.Repos(), stack.md.Blobs(), stack.st)
	deps := httpapi.Deps{
		Config:    cfg,
		Auth:      stack.authSvc,
		Authz:     stack.authSvc,
		Metadata:  stack.md,
		Repos:     stack.md.Repos(),
		ReposSvc:  stack.svc,
		Passwords: stack.authSvc,
		Tokens:    stack.authSvc,
		// The engine's mark-sweep face backs POST /api/v1/system/gc (T-94);
		// the same opened engine serve writes through, so the REST gc and
		// the CLI gc sweep one identical blob tree.
		GC:       stack.st,
		DataDir:  cfg.Storage.DataDir,
		Console:  console.Handler(),
		Adapters: []adapter.Handler{stack.genericHandler, dockerHandler, mavenHandler, npmHandler, pypiHandler},
		Version:  version,
		Revision: revision,
	}
	// The migration REST endpoints (GET/POST /api/v1/storage/migration,
	// T-164) ride the engine exactly when the assembly runs dual-write;
	// every other boot leaves Migration nil and the endpoints answer 501
	// (T-178 wiring). Since T-180 the seam's signatures are the engine's
	// own method set, so the asserted engine plugs in directly — no
	// method-set adapter (the T-178 leftover this collapsed).
	if mig, ok := stack.st.(*storage.MigrationEngine); ok {
		deps.Migration = mig
	}
	// The push-replication plane (T-180, ADR-0021): the store the REST
	// handlers and the engine share, and the cipher that seals target
	// passwords at create time. openStack owns both; a stack that failed to
	// open them never reaches assembly.
	deps.Replication = stack.replStore
	deps.ReplicationCipher = replicationCipherSeam(stack.replCipher)
	// The OIDC login seam (T-157/T-179) rides the SAME provider instance the
	// auth service's Bearer arm verifies against; a nil Deps.OIDC keeps both
	// browser routes at the E-26 404 (FR-54-AC6/H29).
	deps.OIDC = oidcLoginSeam(stack.oidcProv)
	return httpapi.New(deps, logger)
}

// replicationCipherSeam decides the httpapi Deps.ReplicationCipher injection
// (T-180): a nil *remote.Cipher must produce a NIL interface, never a typed
// nil — the create handler tests `Deps.ReplicationCipher == nil` to refuse
// password-carrying configs with a 400 naming the environment variable, and
// assigning the nil pointer directly would make the interface non-nil. The
// same single-point guard oidcLoginSeam established (T-179).
func replicationCipherSeam(c *remote.Cipher) httpapi.CredentialEncryptor {
	if c == nil {
		return nil
	}
	return c
}

// oidcLoginSeam decides the httpapi Deps.OIDC injection (T-179): a nil
// provider must produce a NIL interface, never a typed nil — the login
// handlers test `Deps.OIDC == nil` to keep the two browser routes at the
// E-26 404 (FR-54-AC6/H29), and assigning a nil *auth.OIDCProvider directly
// would make the interface non-nil and flip those routes to a 500 handler.
// The helper is the single point that owns the guard.
func oidcLoginSeam(p *auth.OIDCProvider) httpapi.OIDCLoginFlow {
	if p == nil {
		return nil
	}
	return p
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
	// oidcProv/ldapProv are the config-driven identity providers (T-179,
	// ADR-0020): nil when the section is disabled. oidcProv feeds BOTH the
	// auth service's OIDC Bearer arm and httpapi's login-flow seam;
	// ldapProv owns a connection pool closed in close().
	oidcProv *auth.OIDCProvider
	ldapProv *auth.LDAPProvider
	// The push-replication collaborators (T-180, ADR-0021): replStore is
	// the REST plane's seam (never nil on an opened stack), replDB its own
	// pooled connection (closed after the storage engine in close), and
	// replEngine the worker attached to the repo service's enqueue seam.
	// replCipher seals target passwords on the REST create path and unseals
	// them in the engine — nil when no master key is configured, in which
	// case password-carrying configs are refused at create time. Run is a
	// LIFECYCLE concern, not an open one: startReplication launches it.
	replStore  replication.Store
	replDB     *sql.DB
	replEngine *replication.Engine
	replCipher *remote.Cipher

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

	// Identity providers (T-179, ADR-0020) construct between metadata and
	// storage: they need md's user store for their resolve seams, and an
	// early failure (unreachable OIDC issuer) tears down only metadata.
	oidcProv, ldapProv, err := wireAuthProviders(ctx, cfg, md)
	if err != nil {
		_ = md.Close()
		return nil, err
	}
	if oidcProv != nil {
		logger.Info("oidc authentication active", "issuer", cfg.Auth.OIDC.IssuerURL)
	}
	if ldapProv != nil {
		logger.Info("ldap authentication active",
			"url", cfg.Auth.LDAP.URL, "base_dn", cfg.Auth.LDAP.BaseDN)
	}

	st, err := openStorageEngine(ctx, cfg, logger)
	if err != nil {
		_ = md.Close()
		return nil, err
	}
	if cfg.Storage.Backend == config.StorageBackendS3 {
		logger.Info("s3 storage backend active",
			"bucket", cfg.Storage.S3.Bucket,
			"endpoint", cfg.Storage.S3.Endpoint,
			"bucket_prefix", cfg.Storage.S3.BucketPrefix)
		// Dual-write opens a disk engine too, so its startup sweep also ran.
		if cfg.Storage.Migration.Enabled && !cfg.Storage.Migration.Completed {
			logger.Info("startup session sweep complete",
				"data_dir", cfg.Storage.DataDir, "session_ttl", cfg.Storage.SessionTTL.String())
		}
	} else {
		logger.Info("startup session sweep complete",
			"data_dir", cfg.Storage.DataDir, "session_ttl", cfg.Storage.SessionTTL.String())
	}

	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	// Arm the external providers. WithOIDC's creator parameter REPLACES the
	// creator NewFromStore wired, so cmd passes the exported store-backed
	// constructor — the T-157 leftover-2 fix; passing nil here would silently
	// disable first-login auto-create. WithLDAP keeps the existing creator.
	if oidcProv != nil {
		authSvc = authSvc.WithOIDC(oidcProv, auth.NewUserCreator(md.Users()))
	}
	if ldapProv != nil {
		authSvc = authSvc.WithLDAP(ldapProv)
	}
	auditLog := audit.New(md, cfg.Audit.Enabled)
	svc := repo.New(st, md, authSvc, auditLog)

	// Push replication (T-180, ADR-0021): the store opens its own pooled
	// connection to the metadata database (the 009 tables; metadata.Open
	// applied the migration), the cipher reuses the remote-credential master
	// key, and the engine reads blobs through the same storage engine serve
	// writes to. AttachReplicator hooks the Put tail so landed artifacts
	// enqueue tasks; startReplication (runServe) owns the Run loop.
	replDB, err := openReplicationDB(ctx, sqlitePath(cfg))
	if err != nil {
		_ = st.Close()
		_ = md.Close()
		return nil, err
	}
	replStore := replication.NewSQLiteStore(replDB)
	replCipher, err := replicationCipher()
	if err != nil {
		_ = replDB.Close()
		_ = st.Close()
		_ = md.Close()
		return nil, err
	}
	replEngine, err := replication.NewEngine(replStore, st, replication.EngineOptions{
		Logger: logger,
		Cipher: replCipher,
		Audit:  auditLog,
	})
	if err != nil {
		_ = replDB.Close()
		_ = st.Close()
		_ = md.Close()
		return nil, fmt.Errorf("wiring replication engine: %w", err)
	}
	repo.AttachReplicator(svc, replEngine)

	return &stack{
		md:             md,
		st:             st,
		authSvc:        authSvc,
		auditLog:       auditLog,
		svc:            svc,
		genericHandler: generic.New(svc, md.Blobs()),
		oidcProv:       oidcProv,
		ldapProv:       ldapProv,
		replStore:      replStore,
		replDB:         replDB,
		replEngine:     replEngine,
		replCipher:     replCipher,
		dataDir:        cfg.Storage.DataDir,
	}, nil
}

// openReplicationDB opens the replication store's own connection pool on
// the metadata database (T-180). The metadata package keeps its *sql.DB to
// itself (sub-stores only) and the replication store speaks SQL directly,
// so the second connection is the designed posture — the T-162 integration
// harness already validated it ("the same posture the cmd wiring will
// have"). The DSN carries the per-connection pragmas the replication SQL
// depends on: foreign_keys (the 009 task cascade rides it) and the shared
// busy_timeout budget (this pool contends with the metadata pool on one
// WAL writer; metadata.BusyTimeoutMs is the single spelling of that
// number). journal_mode is a database-file property metadata.Open already
// set, and the store issues no LIKE queries, so the rest of metadata's DSN
// stays metadata-internal.
func openReplicationDB(ctx context.Context, path string) (*sql.DB, error) {
	dsn := "file:" + url.PathEscape(path) +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(" + strconv.Itoa(metadata.BusyTimeoutMs) + ")"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening replication store %s: %w", path, err)
	}
	db.SetMaxOpenConns(metadata.NumCPUConcurrency())
	db.SetMaxIdleConns(metadata.NumCPUConcurrency())
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pinging replication store %s: %w", path, err)
	}
	return db, nil
}

// replicationCipher builds the credential cipher from the same
// BINFLOW_REMOTE_CREDENTIALS_KEY the remote-repository plane uses
// (ADR-0012 decision 4; replication reuses the scheme and the key,
// ADR-0021). Unset key answers (nil, nil) — the engine then fails
// encrypted-password tasks as not-retryable and the REST create path
// refuses new passwords; a malformed key is always fatal.
func replicationCipher() (*remote.Cipher, error) {
	key, err := remote.LoadKey()
	if err != nil {
		return nil, fmt.Errorf("replication credentials: %w", err)
	}
	if key == nil {
		return nil, nil
	}
	c, err := remote.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("replication credentials: %w", err)
	}
	return c, nil
}

// startReplication launches the engine's Run loop on a context derived from
// ctx and returns the drain function the shutdown path MUST call before
// closing the storage engine: it cancels the loop (Run then reverts any
// in-flight task to pending and returns), waits for that to happen, and
// releases the pooled target connections. The drain is idempotent and safe
// to call even when ctx was never canceled (an early serve failure) — the
// dedicated cancel closes the loop regardless.
func (s *stack) startReplication(ctx context.Context, logger *slog.Logger) (drain func()) {
	if s.replEngine == nil {
		return func() {}
	}
	engCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := s.replEngine.Run(engCtx); err != nil {
			logger.Warn("replication engine stopped", "error", err.Error())
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
			s.replEngine.CloseIdleConnections()
			logger.Info("replication engine drained")
		})
	}
}

// wireAuthProviders constructs the external identity providers the config
// asks for (T-179, ADR-0020): auth.oidc.enabled builds an OIDCProvider (its
// construction performs OIDC discovery, so an unreachable issuer fails the
// boot with a pointed error instead of 500-ing on the first login);
// auth.ldap.enabled builds an LDAPProvider (its pool dials lazily — no
// directory round trip at boot; a broken directory surfaces at login per
// FR-55-AC5). Disabled sections yield nil providers, the pre-M6 posture.
//
// The secrets are belt-and-braces reads: config.Load already resolves the
// env names into the config fields, the direct os.Getenv covers hand-built
// configs (the same posture openS3Engine keeps for its secret).
func wireAuthProviders(ctx context.Context, cfg *config.Config, md metadata.Store) (*auth.OIDCProvider, *auth.LDAPProvider, error) {
	var oidcProv *auth.OIDCProvider
	if oc := cfg.Auth.OIDC; oc.Enabled {
		secret := oc.ClientSecret
		if secret == "" {
			secret = os.Getenv(config.OIDCClientSecretEnvVar)
		}
		p, err := auth.NewOIDCProvider(ctx, &auth.OIDCConfig{
			IssuerURL:    oc.IssuerURL,
			ClientID:     oc.ClientID,
			ClientSecret: secret,
			RedirectURL:  oc.RedirectURL,
			Scopes:       oc.Scopes,
			UserClaim:    oc.UserClaim,
			GroupClaim:   oc.GroupClaim,
			AdminGroup:   oc.AdminGroup,
		}, auth.NewOIDCResolver(md.Users()))
		if err != nil {
			return nil, nil, fmt.Errorf("wiring auth.oidc: %w", err)
		}
		oidcProv = p
	}

	var ldapProv *auth.LDAPProvider
	if lc := cfg.Auth.LDAP; lc.Enabled {
		bindPassword := lc.BindPassword
		if bindPassword == "" {
			bindPassword = os.Getenv(config.LDAPBindPasswordEnvVar)
		}
		p, err := auth.NewLDAPProvider(&auth.LDAPConfig{
			Enabled:       true,
			URL:           lc.URL,
			BaseDN:        lc.BaseDN,
			BindDN:        lc.BindDN,
			BindPassword:  bindPassword,
			UserFilter:    lc.UserFilter,
			UserIDAttr:    lc.UserIDAttr,
			GroupFilter:   lc.GroupFilter,
			GroupNameAttr: lc.GroupNameAttr,
			AdminGroup:    lc.AdminGroup,
			PoolSize:      lc.PoolSize,
			StartTLS:      lc.StartTLS,
		}, auth.NewLDAPResolver(md.Users()), nil)
		if err != nil {
			return nil, nil, fmt.Errorf("wiring auth.ldap: %w", err)
		}
		ldapProv = p
	}
	return oidcProv, ldapProv, nil
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

// openStorageEngine assembles the blob engine per storage.backend (T-178):
//
//	disk (default)  OpenEngine at data_dir — the M1 behavior, unchanged
//	               (startup sweep of expired upload sessions included).
//	s3              minio client from cfg.Storage.S3 (the secret arrives
//	               through BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY, resolved
//	               into the config by config.Load), bucket existence
//	               verified, then storage.OpenS3EngineWithClient.
//
// storage.migration layers the T-164 MigrationEngine semantics on the s3
// backend: enabled && !completed boots dual-write (disk AND s3 engines
// wrapped, exposed to the REST migration endpoints through the
// httpapi.MigrationStarter seam); completed — like unconfigured — runs S3
// alone, because a completed migration's source of truth IS S3 (the
// MigrationEngine's completed mode would only delegate to it again).
//
// migration.enabled under backend=disk is refused: dual-write writes to S3,
// whose endpoint and credentials the config only validates under
// backend=s3 — silently skipping half of every write is not an acceptable
// reading of the flag.
func openStorageEngine(ctx context.Context, cfg *config.Config, logger *slog.Logger) (storage.Engine, error) {
	if cfg.Storage.Backend != config.StorageBackendS3 {
		if cfg.Storage.Migration.Enabled && !cfg.Storage.Migration.Completed {
			return nil, fmt.Errorf("opening storage engine: storage.migration.enabled requires storage.backend=s3 (dual-write targets the S3 backend)")
		}
		st, err := storage.OpenEngine(cfg.Storage.DataDir, storage.Options{
			SessionTTL: cfg.Storage.SessionTTL,
		})
		if err != nil {
			return nil, fmt.Errorf("opening storage engine at %s: %w", cfg.Storage.DataDir, err)
		}
		return st, nil
	}

	s3Engine, err := openS3Engine(ctx, cfg)
	if err != nil {
		return nil, err
	}

	mig := cfg.Storage.Migration
	if mig.Enabled && !mig.Completed {
		diskEngine, derr := storage.OpenEngine(cfg.Storage.DataDir, storage.Options{
			SessionTTL: cfg.Storage.SessionTTL,
		})
		if derr != nil {
			_ = s3Engine.Close()
			return nil, fmt.Errorf("opening disk engine for migration at %s: %w", cfg.Storage.DataDir, derr)
		}
		logger.Info("storage migration dual-write mode",
			"data_dir", cfg.Storage.DataDir,
			"bucket", cfg.Storage.S3.Bucket,
			"concurrency", mig.Concurrency)
		return storage.NewMigrationEngine(diskEngine, s3Engine, storage.MigrationConfig{
			Enabled:     true,
			Completed:   false,
			Concurrency: mig.Concurrency,
		}), nil
	}
	if mig.Completed {
		logger.Info("storage migration completed; s3 is the source of truth",
			"bucket", cfg.Storage.S3.Bucket)
	}
	return s3Engine, nil
}

// openS3Engine builds the minio client from cfg.Storage.S3 and returns the
// S3 engine behind it. The secret never appears in YAML: config.Load
// resolves BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY into
// S3Config.SecretAccessKey; the direct os.Getenv here is the belt-and-braces
// read for hand-built configs (config.S3SecretEnvVar is the one spelling of
// the name, case per the config package's env mapping).
func openS3Engine(ctx context.Context, cfg *config.Config) (storage.Engine, error) {
	sc := cfg.Storage.S3
	secret := sc.SecretAccessKey
	if secret == "" {
		secret = os.Getenv(config.S3SecretEnvVar)
	}
	if secret == "" {
		return nil, fmt.Errorf("opening s3 engine: %s is not set (backend=s3 needs the secret access key)", config.S3SecretEnvVar)
	}

	lookup := minio.BucketLookupAuto
	if sc.UsePathStyle {
		// MinIO and IP endpoints do not support virtual-host style.
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(sc.Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(sc.AccessKeyID, secret, ""),
		Secure:       httpapi.SecureFromEndpoint(sc.Endpoint),
		Region:       sc.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("opening s3 engine: minio client for %s: %w", sc.Endpoint, err)
	}

	// OpenS3Engine's contract requires the bucket to exist; verify at boot
	// with a pointed refusal instead of failing on the first upload.
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	exists, err := client.BucketExists(probeCtx, sc.Bucket)
	if err != nil {
		return nil, fmt.Errorf("opening s3 engine: probing bucket %s at %s: %w", sc.Bucket, sc.Endpoint, err)
	}
	if !exists {
		return nil, fmt.Errorf("opening s3 engine: bucket %s does not exist at %s (create the bucket or point storage.s3.bucket at the right one)", sc.Bucket, sc.Endpoint)
	}

	return storage.OpenS3EngineWithClient(client, sc.Bucket, &storage.S3EngineOptions{
		BucketPrefix: sc.BucketPrefix,
	}), nil
}

// close tears the stack down in the section 7.4 order. It is safe to call
// twice (every closer is idempotent).
func (s *stack) close(logger *slog.Logger) {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	// The LDAP provider's connection pool dials outside both engines, so it
	// drains first (the auth plane closes before the data plane).
	if s.ldapProv != nil {
		s.ldapProv.Close()
	}
	if s.st != nil {
		if err := s.st.Close(); err != nil {
			logger.Error("closing storage engine", "error", err.Error())
		}
	}
	// The replication store's own pool closes after the storage engine: the
	// engine loop only reads blobs through storage, and a caller that
	// skipped drainReplication (an error path) tolerates the pool's closure
	// — the loop's next store call fails, is logged, and the process is
	// exiting anyway.
	if s.replDB != nil {
		if err := s.replDB.Close(); err != nil {
			logger.Error("closing replication store", "error", err.Error())
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
// dropped too. Every successful run — dry-run and apply alike — records a
// gc.run audit event (FR-30-AC5: the CLI leg of the two-path ruling; actor
// is the CLI's admin convention, detail shaped like the REST face's).
//
// The whole run holds the data-directory maintenance lock (ADR-0015 erratum
// 3): GC deletes blobs while an online export copies them, so the two are
// mutually exclusive — gc refuses (non-zero exit) while an export runs, and
// export refuses while gc runs. The REST gc face (T-94) holds the SAME lock
// through the same primitive.
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

	// Data-directory maintenance lock: held for the whole run (mark, sweep,
	// ledger cleanup) — export is refused while gc works and vice versa.
	lock, err := storage.AcquireDataLock(cfg.Storage.DataDir, storage.DataLockOpGC)
	if err != nil {
		return fmt.Errorf("gc: %w", err)
	}
	defer func() { _ = lock.Release() }()

	// graceOverride remembers whether an explicit flag overrode the
	// configured grace: the gc.run audit detail carries graceHours as null
	// exactly when the run used storage.gc_grace (the same absent-vs-explicit
	// distinction the REST face's pointer field makes).
	grace := cfg.Storage.GCGrace
	graceOverride := false
	if *graceDays > 0 {
		grace = time.Duration(*graceDays) * 24 * time.Hour
		graceOverride = true
	}
	if *graceHours > 0 {
		grace = time.Duration(*graceHours) * time.Hour
		graceOverride = true
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

	// Pass 1 is always the dry pass — the same two-pass shape the REST face
	// runs (T-94): it yields the candidate list and, while every file still
	// exists, their on-disk bytes for the report and the gc.run audit.
	// --apply sweeps for real as pass 2 over a freshly recomputed mark set.
	candidates, err := st.GC(ctx, referenced, grace, false)
	if err != nil {
		return fmt.Errorf("gc: %w", err)
	}
	candidateBytes, statErr := sumBlobFileSizes(cfg.Storage.DataDir, candidates)
	if statErr != nil {
		logger.Warn("gc: candidate sizing incomplete", "error", statErr.Error())
	}

	var deleted []string
	if *apply {
		deleted, err = st.GC(ctx, referenced, grace, true)
		if err != nil {
			return fmt.Errorf("gc: %w", err)
		}
	}

	mode := "dry-run"
	if *apply {
		mode = "apply"
	}
	writeCLIReport(stderr, "gc: mode=%s grace=%s data_dir=%s candidates=%d\n",
		mode, grace, cfg.Storage.DataDir, len(candidates))
	listing := candidates
	if *apply {
		// The apply listing stays the DELETED set (the M2 shape): a serve
		// writer racing the two passes can keep a pre-pass candidate alive.
		listing = deleted
		writeCLIReport(stderr, "gc: deleted=%d\n", len(deleted))
	}
	for _, sha := range listing {
		writeCLIReport(stderr, "%s\n", sha)
	}

	// A real deletion removes the blobs-ledger rows with it: the ledger is
	// the "ever existed" record (ADR-0006), and a row whose physical file
	// is gone must not survive as a phantom.
	if *apply {
		for _, sha := range deleted {
			if err := md.Blobs().Delete(ctx, sha); err != nil && !errors.Is(err, metadata.ErrNotFound) {
				logger.Warn("gc: dropping blobs ledger row failed", "sha256", sha, "error", err.Error())
			}
		}
	}

	// gc.run audit (FR-30-AC5: the CLI leg of the two-path ruling — REST and
	// CLI both record, actor=admin because a CLI carries no authenticated
	// principal to name). The detail mirrors the REST face's shape field for
	// field: graceHours null = the configured grace, candidateBytes the
	// pre-pass physical bytes, deletedCount the sweep's real deletions. The
	// best-effort wiring follows the export.run precedent (T-96).
	var graceHoursAudit *int
	if graceOverride {
		hours := int(grace / time.Hour)
		graceHoursAudit = &hours
	}
	detail, derr := json.Marshal(struct {
		Apply          bool  `json:"apply"`
		GraceHours     *int  `json:"graceHours"`
		CandidateCount int   `json:"candidateCount"`
		CandidateBytes int64 `json:"candidateBytes"`
		DeletedCount   int   `json:"deletedCount"`
	}{*apply, graceHoursAudit, len(candidates), candidateBytes, len(deleted)})
	if derr != nil {
		logger.Warn("gc: audit detail marshal failed", "error", derr.Error())
		detail = []byte("{}")
	}
	audit.BestEffort(audit.New(md, cfg.Audit.Enabled)).Record(ctx, audit.Event{
		Actor:  cliAuditActor,
		Action: audit.ActionGCRun,
		Detail: string(detail),
	})
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
//
// Folder marker rows are skipped (T-124, same exclusion as the snapshot
// manifest boundary): their sha256 is the shared metadata.FolderMarkerSHA
// sentinel with no physical file behind it. Keeping the sentinel out keeps
// the mark set exactly "physical blob shas" — no on-disk file can ever match
// it, so the skip changes no sweep decision, but a future consumer asserting
// mark ⊆ physical blobs must not trip over a value the filestore can never
// carry. Keep this walk in sync with httpapi's liveChecksumSet (same shape,
// same skips).
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
			if n.Sha256 != "" && n.Sha256 != metadata.FolderMarkerSHA {
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

// sumBlobFileSizes totals the on-disk size of the given blobs through the
// exported BlobPath helper (the engine's own path shape) — the CLI twin of
// httpapi's same-named walk: the two GC faces size the gc.run audit's
// candidateBytes independently and identically (physical bytes, never the
// blobs ledger, so crash-residue orphans count too; keep the walks in sync).
// A blob that vanished between sweep and stat contributes zero and surfaces
// through the error. It must run BEFORE the apply pass deletes the files.
func sumBlobFileSizes(dataDir string, shas []string) (int64, error) {
	var total int64
	var firstErr error
	for _, sha := range shas {
		path, err := storage.BlobPath(dataDir, sha)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("blob path %s: %w", sha, err)
			}
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("stat %s: %w", path, err)
			}
			continue
		}
		total += info.Size()
	}
	return total, firstErr
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
