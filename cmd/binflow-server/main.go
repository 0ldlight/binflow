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
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/oauth2"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/cargo"
	"github.com/lzwzzy/binflow/internal/adapter/conan"
	"github.com/lzwzzy/binflow/internal/adapter/deb"
	"github.com/lzwzzy/binflow/internal/adapter/docker"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/adapter/goproxy"
	"github.com/lzwzzy/binflow/internal/adapter/helm"
	"github.com/lzwzzy/binflow/internal/adapter/maven"
	"github.com/lzwzzy/binflow/internal/adapter/npm"
	"github.com/lzwzzy/binflow/internal/adapter/nuget"
	"github.com/lzwzzy/binflow/internal/adapter/pypi"
	"github.com/lzwzzy/binflow/internal/adapter/rpm"
	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/console"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/keypair"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
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
	--grace-seconds Override storage.gc_grace with sub-minute precision
	                (positive integer; wins over --grace-hours. An --apply
	                run below 60s is refused while serve holds serve.lock —
	                use the REST gc face or a maintenance window instead)

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
On storage.backend=s3 both faces are engine-aware: export streams the
referenced blobs out of the bucket into the artifact, and import uploads
them back through the engine (a dual-write instance keeps the data-dir copy
and lets the migration sync it).

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

	// The serve heartbeat lock ([M9] ADR-0031 / architecture section 14.2
	// point 4): held for the process lifetime so the gc CLI can tell "a
	// serve owns this data directory" from outside and refuse its
	// cross-process no-window apply. Held before anything else opens the
	// directory; the kernel drops it on exit or crash — that drop IS the
	// heartbeat, no cleanup path exists or needs one. A second serve on the
	// same data directory fails here with a pointed refusal (multi-instance
	// on one data directory is already forbidden, architecture section 9).
	serveLock, err := acquireServeLock(cfg.Storage.DataDir)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	defer func() { _ = serveLock.release() }()

	logger.Info("binflow starting",
		"version", version,
		"revision", revision,
		"config", describeConfigPath(*configPath),
		"listen", cfg.Server.Listen,
		"data_dir", cfg.Storage.DataDir,
		"driver", cfg.Metadata.Driver,
	)

	// T-306 (ADR-0036 decision 7): the storage chain shape in one INFO line
	// — provider order, migration mode, declaring source. Credentials are
	// structurally absent: the secret is env-only and no chain source
	// carries it. The joined provider list IS the storage-type label
	// (BinFlow has no template layer; the binarystore-2 decision recorded
	// in internal/config/binstore.go).
	if chain := cfg.Storage.Chain; len(chain.Providers) > 0 {
		logger.Info("storage chain resolved",
			"providers", strings.Join(chain.Providers, ","),
			"mode", chain.Mode,
			"source", chain.Source)
	}
	// The loader's non-fatal findings (the binstore compatibility-window
	// hints and the ignored chain-scoped env leftovers) drain through the
	// real logger here — the config package itself never logs.
	for _, w := range cfg.StartupWarnings {
		logger.Warn(w)
	}

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

	// Return the boot path's transient heap to the OS before the listener
	// goes up (T-336, PRD milestone-12 §102.3 / D-8R): the admin seed's
	// argon2id hash and the default-password warning's verification each
	// derive at m=64 MiB, so a fresh boot briefly dirties ~128 MiB of heap
	// the process never needs again — measured as 135.8 MiB dirty
	// VM_ALLOCATE against a 100 MB idle budget (the embed FSes are lazy and
	// blameless: gctrace shows ~2 MiB live after the boot GCs).
	// debug.FreeOSMemory forces a full GC and hands every free span back;
	// on darwin that drops the vmmap physical footprint the D-8 gate reads
	// (MADV_FREE_REUSABLE pages leave the footprint at once), on Linux it
	// drops ps RSS (MADV_DONTNEED). It runs after the last boot-time
	// derivation and before any request can arrive, costing single-digit
	// milliseconds against the 2 s cold-start budget. Steady-state password
	// checks stay on the runtime's background scavenger — this is a boot
	// seam, not a memory policy.
	debug.FreeOSMemory()

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

	// The license daily re-evaluation loop (ADR-0032 / D6): expiry is a
	// runtime event, the downgrade needs no restart. Cancellation rides
	// the same signal context as the HTTP drain.
	go stack.licenseMgr.Run(ctx)
	// The unused-cleanup cron (T-324, FR-102.2): one pass per hour, apply
	// mode. Exits with the signal context.
	go stack.cleanupEng.Run(ctx)
	// The trash-can retention cron (T-345, FR-106.3): one pass per hour,
	// purging entries past the retention window. Exits with the signal
	// context (the same no-drain posture as the cleanup cron).
	go stack.trashEng.Run(ctx)

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
	// goproxy (M10/T-285, the Go pilot package type): mounting is the whole
	// wiring — the content plane dispatches on package_type="go", and the
	// provider registration is what teaches the remote pull-through engine
	// the !lower upstream escaping plus the expirable-marker TTL split
	// (docs/reverse/goproxy.md sections 3.1/3.2). The addons.Go() slot in
	// the manifest below carries the pro-tier gating (T-282/T-283).
	goproxyHandler := goproxy.Register(stack.svc, stack.md.Repos(), stack.md.Blobs())
	// nuget (M10/T-287, the NuGet pilot package type): same wiring story
	// as goproxy — the content plane dispatches on package_type="nuget",
	// and the provider registration teaches the remote pull-through
	// engine the flatcontainer/registration upstream prefixes plus the
	// metadata TTL split. The /binflow/api/nuget/{v3,v2} mount is the
	// router's plane-aware api mount (PRD FR-88's spellings); the
	// addons.NuGet() slot carries the pro-tier gating (T-282/T-283).
	nugetHandler := nuget.Register(stack.svc, stack.md.Repos(), stack.md.Blobs(), stack.md.Remote(),
		nuget.Options{BaseURL: cfg.Server.BaseURL})
	// cargo (M11/T-294, the Rust crates package type): same wiring story
	// as goproxy/nuget — the content plane dispatches on
	// package_type="cargo" and the provider registration classifies the
	// sparse-index files as regenerable metadata for the future remote
	// hop. LOCAL repositories only: the remote pull-through and virtual
	// aggregation are their own M11 tickets (spec sections 8/S4/S5). The
	// NodeProps seam carries the protocol's own yank state (the
	// crate.yanked node property IS the yank flag); the addons.Cargo()
	// slot carries the pro-tier gating (T-282/T-283).
	cargoHandler := cargo.Register(stack.svc, stack.md.Repos(), stack.md.Blobs(), stack.md.NodeProps(),
		cargo.Options{BaseURL: cfg.Server.BaseURL, AnonymousAccess: cfg.Security.AnonymousAccess})
	// conan (M11/T-308, the C/C++ package type): same wiring story as
	// cargo — the content plane dispatches on package_type="conan" and the
	// provider registration classifies the index.json/.timestamp nodes as
	// regenerable metadata for the future remote hop (T-312). LOCAL
	// repositories only here; the TokenIssuer seam carries the v1
	// users/authenticate mint (the npm login posture); the addons.Conan()
	// slot carries the pro-tier gating (T-282/T-283). The reindex
	// management face (ADR-0034 dispatchAPI family) is
	// conan.NewManagementHandler — its router cases land with the assembly
	// wire-up this ticket registered (the ticket report carries the exact
	// snippet); the handler itself ships in the adapter package.
	conanHandler := conan.Register(stack.svc, stack.md.Repos(), stack.md.Blobs(), stack.authSvc,
		conan.Options{BaseURL: cfg.Server.BaseURL})
	// The conan reindex management face (the deferred T-308 §5-D9 wire-up,
	// landed with the B4/B5 assembly): a self-gated handler — the router's
	// conan cases only authenticate and delegate; the CanManageRepo(write)
	// walk, the local-only class check and both reindex spellings live in
	// the adapter package (reindex_test.go pinned them direct-mount).
	conanMgmt := conan.NewManagementHandler(stack.svc, stack.md.Repos(), stack.authSvc)
	// helm (M11/T-309 + T-313, the classic Helm chart repository package
	// type): mounting is the whole content-plane wiring — dispatch keys on
	// package_type="helm" — and the provider registration classifies the
	// repo-root index.yaml as regenerable metadata for the remote hop. All
	// three classes serve: LOCAL (T-309), the REMOTE pull-through and the
	// VIRTUAL aggregation (T-313, the RemoteConfigs seam feeds the member
	// upstream URLs the index rewriting recognizes and the _external
	// egress policy); the read-only /binflow/api/helm alias and the
	// reindex management family live in the router (ADR-0034's two
	// clauses); the NodeProps seam carries the chart.* facts; the
	// addons.Helm() slot carries the pro-tier gating (T-282/T-283).
	helmHandler := helm.Register(stack.svc, stack.md.Repos(), stack.md.Blobs(), stack.md.NodeProps(), stack.md.Remote(),
		helm.Options{BaseURL: cfg.Server.BaseURL})
	// rpm (M11/T-311, the RPM/YUM package type): same wiring story as
	// cargo/helm — the content plane dispatches on package_type="rpm" and
	// the provider registration classifies the repodata family as
	// regenerable metadata for the future remote hop. LOCAL repositories
	// only here; the /binflow/api/yum reindex family lives in the router
	// (ADR-0034's dispatchAPI posture); the DataDir seam roots the
	// .rpmcache parse cache; the addons.Rpm() slot carries the pro-tier
	// gating (T-282/T-283). calculateYumMetadata defaults FALSE (RP-2's
	// final ruling — uploads store, repodata recomputes on demand).
	// T-315: the NodeProps seam feeds the remote .rpm rpm.metadata.*
	// backfill; remote and virtual classes serve (handler.go RepoTypes).
	// T-322: the Signer seam signs repomd.xml.asc/.key on every local
	// recompute (stack.signer, the same keypair.SigningService deb rides).
	rpmHandler := rpm.RegisterWithProps(stack.svc, stack.md.Repos(), stack.md.Blobs(), stack.md.NodeProps(),
		rpm.Options{DataDir: cfg.Storage.DataDir, Signer: stack.signer})
	// deb (M11/T-310, the Debian/apt package type): same wiring story as
	// rpm — the content plane dispatches on package_type="debian" and the
	// provider registration classifies the dists/ tree as regenerable
	// metadata for the future remote hop. LOCAL repositories only here;
	// the /binflow/api/deb/reindex family lives in the router (ADR-0034's
	// dispatchAPI posture); the NodeProps seam carries the deb.*/dsc.*
	// coordinate registration (the index engine's source of truth); the
	// addons.Debian() slot carries the pro-tier gating (T-282/T-283).
	// The debPUT chain recomputes automatically (FR-97.1) — no opt-in
	// switch — with the TL-4 forced architecture families on by default.
	debHandler := deb.Register(stack.svc, stack.md.Repos(), stack.md.Blobs(), stack.md.NodeProps(),
		deb.Options{Signer: stack.signer})
	// The addon registry (M10 T-282, ADR-0033 / section 15.2.1): the
	// COMPILE-TIME ASSEMBLY MANIFEST — one literal slice, the
	// META-INF/addon.{xml,properties} behavior pattern in Go form. This is
	// the whole registration ceremony: a new slot is a constructor in
	// internal/addons/slots.go plus one line here, and the gate, the
	// /api/v1/addons view and the repo-create legal set all pick it up with
	// zero further branch edits (FR-86-AC2). No init() self-registration —
	// the explicit-assembly convention internal/adapter established.
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
		Adapters: []adapter.Handler{stack.genericHandler, dockerHandler, mavenHandler, npmHandler, pypiHandler, goproxyHandler, nugetHandler, cargoHandler, conanHandler, helmHandler, rpmHandler, debHandler},
		// The adapters' management mounts (ADR-0034): conan's reindex
		// family today; helm/yum serve their faces through the router's
		// own handlers (the adapter-seam pattern) instead.
		MgmtHandlers: map[string]http.Handler{"conan": conanMgmt},
		// The process metric registry (T-163, ADR-0022): one per serve; the
		// /metrics endpoint and the request-counting middleware ride it.
		Metrics: metrics.NewRegistry(),
		// The addon manifest (M10 T-282): the compile-time assembly literal
		// slice (see addonManifest — the one function to touch when a slot
		// lands). httpapi.New asserts every adapter above carries a slot.
		// The ONE instance openStack built — the repo-create gate seam reads
		// the same registry (T-283).
		Addons:   stack.addonsReg,
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
	// The engine-backed sizing seam (T-201, T-173 D-1): when the assembly's
	// engine is the S3 one (pure s3 or a completed migration), the stats
	// endpoint's physical half, the /metrics storage-byte gauge and the GC
	// candidate sizing read the bucket through the engine instead of walking
	// a data-dir blobs/ tree that does not exist. A dual-write stack keeps
	// the disk walk by omission: its MigrationEngine does not carry the
	// seam, and the data directory still holds every blob while the
	// migration runs.
	if cfg.Storage.Backend == config.StorageBackendS3 {
		if inv, ok := stack.st.(httpapi.BlobInventory); ok {
			deps.BlobInventory = inv
		}
	}
	// The MPU REST seam (M10 T-289, FR-90.1 / architecture section 15.4):
	// /api/v1/uploads rides the engine's multipart-session capability —
	// only the pure-S3 engine carries it. Every other backend (filestore,
	// and dual-write whose MigrationEngine fronts the disk path) leaves
	// the seam nil and the six endpoints answer the honest plain-text 501
	// (FR-90-AC3): MPU-to-S3 through a dual-write stack would bypass its
	// disk half, so the absence is semantics, not a gap.
	if mpu, ok := stack.st.(storage.MultipartUploads); ok {
		deps.Uploads = mpu
	}
	// The push-replication plane (T-180, ADR-0021): the store the REST
	// handlers and the engine share, and the cipher that seals target
	// passwords at create time. openStack owns both; a stack that failed to
	// open them never reaches assembly.
	deps.Replication = stack.replStore
	deps.ReplicationCipher = replicationCipherSeam(stack.replCipher)
	// The OIDC login seam (T-157/T-179) rides the SAME live provider the
	// auth service's Bearer arm verifies against — through the config
	// manager's snapshot (T-305): the seam answers the CURRENT OAuth2
	// config, and a disabled live section yields a nil config (httpapi's
	// activeOIDCConfig keeps both browser routes at the E-26 404,
	// FR-54-AC6/H29, per-configuration).
	deps.OIDC = hotOIDCLoginSeam(stack.authCfg)
	// The auth-config plane (T-305): the nine /api/v1/admin/security/*
	// routes ride the same manager that feeds the arms.
	deps.AuthConfigs = stack.authCfg
	// The instance GPG keypair plane (T-319, ADR-0038): the /api/security/
	// keypair family, the generation endpoint and the v2 association face.
	deps.Keypairs = stack.keypairs
	// The license plane (M10 T-279): /api/system/license rides the manager
	// openStack loaded; its gate facet (AddonEnabled) is consumed by the
	// T-283 weave points, not by these routes.
	deps.License = stack.licenseMgr
	// The unused-cleanup engine (M11 T-324): POST/GET /api/v1/system/cleanup
	// and the cleanup metrics gauges ride it.
	deps.Cleanup = stack.cleanupEng
	// The fail-open replay metrics (M12 T-338): only the dual-write
	// migration engine carries the stats face.
	if rs, ok := stack.st.(httpapi.ReplayStatsSource); ok {
		deps.Replay = rs
	}
	return httpapi.New(deps, logger)
}

// addonManifest is THE assembly list (M10 T-282, ADR-0033 / section 15.2.1):
// the compile-time addon manifest as one literal slice — the
// META-INF/addon.{xml,properties} behavior pattern in Go form. Adding a
// slot is one constructor in internal/addons/slots.go plus one line here;
// the gate, the /api/v1/addons view and the repo-create legal set pick it
// up with zero further branch edits (FR-86-AC2). No init()
// self-registration — the explicit-assembly convention internal/adapter
// established (no package-level singletons, everything injectable).
func addonManifest() *addons.Registry {
	return addons.New(
		// Five-core retro-fit (§15.2.4): community floor, zero behavior
		// change — MinTier=TierCommunity passes every gate on every
		// instance, licensed or not.
		addons.Generic(), addons.Docker(), addons.Maven(), addons.Npm(), addons.Pypi(),
		// Gated pilot package-type slots (pro; the adapters land with their
		// own tickets — the slots exist so gate/view/legal-set are complete).
		addons.Go(), addons.NuGet(), addons.Cargo(), addons.Conan(), addons.Helm(), addons.Rpm(), addons.Debian(),
		// Feature slots: properties on the floor, repo-operations at pro
		// (the Q4 final ruling — the copy/move/archive family mirrors
		// Artifactory's entitlement posture), trashcan at pro (the Q3
		// INTERIM — T-345's evidence brief recommends community; the flip
		// is this manifest's one tier value), the enterprise placeholders
		// visible with their M11+ reservation notes.
		addons.Properties(), addons.RepoOperations(), addons.Trashcan(), addons.HA(), addons.XrayIntegration(),
	)
}

// packageTypeGate joins the assembled addon manifest with the license
// Manager into repo.Service's consumer-side verdict seam (M10 T-283,
// architecture section 15.1.5 weave 2: "消费方经小接口注入，repo 不 import
// license 包" — this adapter is the one place both collaborators meet).
//
// The refusal clause mirrors addons view.statusOf's derivation ORDER
// exactly (the disabled config first, then the tier floor, then the
// allowlist), so the D3 message always names the DECIDING clause, never a
// plausible one — the same single-source rule the /api/v1/addons view and
// the content-plane gate follow.
type packageTypeGate struct {
	reg *addons.Registry
	ev  addons.Evaluator // *license.Manager
}

// Verdict implements repo.PackageTypeGate.
func (g packageTypeGate) Verdict(ctx context.Context, packageType string) repo.PackageTypeVerdict {
	st, ok := g.reg.StatusOf(ctx, g.ev, packageType)
	if !ok {
		return repo.PackageTypeVerdict{}
	}
	v := repo.PackageTypeVerdict{Known: true, Unlocked: st.Enable}
	if !st.Enable {
		v.Refusal = g.refusal(ctx, st)
	}
	return v
}

// refusal renders the pointed clause for a known-but-locked slot.
func (g packageTypeGate) refusal(ctx context.Context, st addons.Status) string {
	if !g.ev.AddonEnabled(ctx, st.Addon.ID, license.TierCommunity) {
		return "disabled by configuration (addons.disabled) — remove the entry and restart to restore"
	}
	if state := g.ev.State(); state.Tier < st.Addon.MinTier {
		return fmt.Sprintf("license tier '%s' < '%s'", state.Tier, st.Addon.MinTier)
	}
	return "not named in the license addon allowlist"
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

// hotOIDCLoginSeam adapts the auth ConfigManager onto the login-flow seam
// (T-305): nil manager → nil interface; otherwise the OAuth2 config of the
// CURRENT provider (nil when the live section is disabled — httpapi's
// activeOIDCConfig turns that into the E-26 404 posture).
func hotOIDCLoginSeam(m *auth.ConfigManager) httpapi.OIDCLoginFlow {
	if m == nil {
		return nil
	}
	return hotOIDCLogin{m: m}
}

type hotOIDCLogin struct{ m *auth.ConfigManager }

func (h hotOIDCLogin) OAuth2Config() *oauth2.Config {
	if p := h.m.CurrentOIDC(); p != nil {
		return p.OAuth2Config()
	}
	return nil
}

// oidcLoginSeam's typed-nil guard (T-179) moved into hotOIDCLoginSeam
// (T-305): the seam now delegates to the config manager's live provider,
// with the same nil-interface contract.

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
	// T-306 (ADR-0036 decision 1): even a boot without any binflow.yaml
	// discovers binstore.yaml through the same resolution order —
	// ./binstore.yaml, then $BINFLOW_HOME/binstore.yaml — so the default
	// form keeps "binstore.yaml next to where binflow.yaml would be". The
	// temp-file roundtrip above may have recorded branch-① hints against a
	// temp directory that never had a binstore.yaml; the real discovery
	// below re-derives the whole warning set, so start it clean.
	cfg.StartupWarnings = nil
	if err := config.ApplyBinstoreForDefaultBoot(cfg, home); err != nil {
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

// writeStartupWarnings drains the config loader's non-fatal findings onto a
// CLI subcommand's stderr (T-306, ADR-0036). serve does not use this — it
// drains the same list through its real structured logger right after
// construction, so each surface has exactly one destination.
func writeStartupWarnings(w io.Writer, cfg *config.Config) {
	for _, msg := range cfg.StartupWarnings {
		writeCLIReport(w, "%s\n", msg)
	}
}

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
	// signer is the GPG signing seam (T-319's SigningService, fed to the
	// deb/rpm adapters' release-signing Options by the adapter phase).
	signer *keypair.SigningService
	// oidcProv/ldapProv were the config-driven identity providers (T-179,
	// ADR-0020). T-305 (ADR-0035) replaced them with authCfg, the
	// ConfigManager: the three protocol sections live in auth_configs
	// (migration 015), the file sections are first-boot seeds only, and the
	// providers are rebuilt per config PUT — the auth service's external
	// arms resolve their provider per request through the manager's
	// snapshot (change-effective-immediately, no restart).
	authCfg *auth.ConfigManager
	// keypairs is the instance GPG keypair plane (T-319, ADR-0038): the
	// /api/security/keypair family plus generation and the repo association
	// face. Built over the 016 gpg_keypairs rows, the same enc:v1 cipher
	// replication seals with (nil cipher = writes refuse; BootCheck fails
	// the boot when rows exist without the key), and the repositories'
	// configs for the in-use guard. The signing seam (the deb/rpm legs of
	// T-321/T-322) assembles from the same collaborators.
	keypairs *keypair.Manager
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

	// licenseMgr is the entitlement manager (M10 T-279, ADR-0032): built
	// and loaded in openStack; its daily re-evaluation ticker starts with
	// the signal context in runServe and simply exits with it (no drain
	// semantics — a mid-tick cancellation leaves the snapshot consistent,
	// the next boot's Load re-derives it from the stored row).
	licenseMgr *license.Manager

	// cleanupEng is the unused-cleanup engine (M11 T-324, FR-102.2): built
	// in openStack over the same store/engine/audit collaborators; its
	// hourly Run loop is a LIFECYCLE concern runServe starts with the
	// signal context (the licenseMgr precedent — no drain semantics: a run
	// already holding the maintenance lock completes detached from the
	// cancellation, see RunOnce's WithoutCancel).
	cleanupEng *repo.CleanupEngine

	// trashEng is the trash-can retention engine (M12 T-345, FR-106.3):
	// built in openStack over the same store/audit collaborators plus the
	// trashcan slot's license verdict; its hourly Run loop starts with the
	// signal context like the cleanup cron (node-row purges only — no
	// maintenance lock to coordinate, blob reclamation is the standing
	// GC's).
	trashEng *repo.TrashEngine

	// addonsReg is the assembled addon manifest (M10 T-282/T-283): ONE
	// registry per process — the repo-create gate seam (openStack, weave 2)
	// and the HTTP surface's Deps.Addons (newAssembledServer) read the same
	// instance, so the two consumers can never drift apart on the slot set.
	addonsReg *addons.Registry

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

	// The auth-configuration plane (T-305, ADR-0035 / FR-92) constructs
	// between metadata and storage: it needs md's user store for the
	// provider resolve seams, and an early failure (a stored secret without
	// a master key, an unreachable seeded OIDC issuer) tears down only
	// metadata. Load resolves the dual-source rules (DB row authoritative,
	// file section first-boot seed, WARN on override) and replays the
	// stored rows into the opening snapshot.
	authCfgMgr, err := wireAuthConfigManager(ctx, cfg, md, logger)
	if err != nil {
		_ = md.Close()
		return nil, err
	}

	// The instance GPG keypair plane (T-319, ADR-0038): the 016
	// gpg_keypairs rows sealed under the same enc:v1 master key. BootCheck
	// fails the boot when rows exist without the key — an instance that
	// once sealed keypairs cannot silently degrade to serving them
	// unsealable (the static-secret family posture of auth_configs and
	// replication).
	keypairMgr, keypairSigner, err := wireKeypairManager(ctx, md, logger)
	if err != nil {
		_ = md.Close()
		return nil, err
	}

	st, err := openStorageEngine(ctx, cfg, logger, md)
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
	// auth.hash_concurrency (T-204 / T-192 leftover 1): 0 keeps the service's
	// derived argon2 gate limit (GOMAXPROCS clamped to [1,16]); a positive
	// override resizes the one gate every password entrance shares (Basic
	// arm, web login, docker /v2/token and npm couch login).
	if cfg.Auth.HashConcurrency > 0 {
		authSvc = authSvc.WithHashConcurrency(cfg.Auth.HashConcurrency)
	}
	// Arm the external provider arms through the live config snapshot
	// (T-305, ADR-0035): the OIDC Bearer arm and the LDAP login fallback
	// resolve their provider per request from the manager — a config PUT is
	// effective on the next authentication, no restart. The auto-create
	// seam stays whatever NewFromStore wired (the store-backed creator);
	// the section flags (auto_create_users / autoCreateUser, default true)
	// gate it live. The pre-T-305 static wiring (WithOIDC/WithLDAP over
	// construction-time providers) is gone — the file sections are seeds.
	authSvc = authSvc.WithAuthConfig(authCfgMgr)
	auditLog := audit.New(md, cfg.Audit.Enabled)
	// ADR-0040's audit facet (T-338 wiring 2/3): the dual-write engine
	// emits raw ReplayEvents synchronously; the assembly maps them to the
	// audit vocabulary (the cleanup.run precedent — storage imports no
	// audit by layering discipline). Best-effort by the audit contract.
	replayAudit := audit.BestEffort(auditLog)
	if re, ok := st.(interface {
		SetReplayEvents(func(storage.ReplayEvent))
	}); ok {
		re.SetReplayEvents(func(ev storage.ReplayEvent) {
			action, detail := "storage.replay.window", map[string]any{"phase": ev.Phase}
			switch ev.Kind {
			case storage.ReplayEventDrained:
				action = "storage.replay.drained"
				detail = map[string]any{
					"drained": ev.Drained, "source_gone": ev.SourceGone,
					"permanent_failed":  ev.PermanentFailed,
					"reconcile_missing": ev.ReconcileMissing, "rounds": ev.Rounds,
				}
			default:
				if ev.FirstError != "" {
					detail["first_error"] = ev.FirstError
				}
			}
			if b, merr := json.Marshal(detail); merr == nil {
				replayAudit.Record(context.Background(), audit.Event{
					Actor: "system-replay", Action: action, Detail: string(b),
				})
			}
		})
	}
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
		// replication.allow_private_target (T-210 / ADR-0025 决策 4) bridges
		// into the engine's SSRF screen: enabling the key (default) leaves
		// DenyPrivateTargets false (private targets allowed); setting it
		// false flips DenyPrivateTargets true and rejects private targets.
		// scheme/host validation, per-hop re-check, and DNS-rebinding pinning
		// are unaffected by this switch.
		DenyPrivateTargets: !cfg.Replication.AllowPrivateTarget,
		// The source-side metadata seam (T-195): selects the protocol-aware
		// push planes (docker /v2, npm publish/dist-tag, pypi multipart) by
		// the source repository's package type. Without it every task takes
		// the generic REST plane — correct for generic/maven only.
		Meta: replication.NewStoreMetaSource(md),
	})
	if err != nil {
		_ = replDB.Close()
		_ = st.Close()
		_ = md.Close()
		return nil, fmt.Errorf("wiring replication engine: %w", err)
	}
	repo.AttachReplicator(svc, replEngine)

	// The entitlement manager (M10 T-279, ADR-0032): embedded verify keys,
	// the 012 licenses row as fact source, the community floor until a
	// document verifies. Load re-runs the chain synchronously — a stored
	// row that stopped verifying (rotation, tampering) degrades to the
	// floor with a WARN and never blocks the boot (NFR-S53). The daily
	// re-evaluation ticker is a LIFECYCLE concern: runServe launches it
	// with the signal context, startLicenseTicker below.
	licenseKeys, err := license.EmbeddedVerifyKeys()
	if err != nil {
		_ = replDB.Close()
		_ = st.Close()
		_ = md.Close()
		return nil, fmt.Errorf("license verify keys: %w", err)
	}
	licenseMgr, err := license.New(license.Options{
		Store:       md.Licenses(),
		VerifyKeys:  licenseKeys,
		DisabledCSV: cfg.Addons.Disabled, // the addons.disabled breaker (M10 T-283, ADR-0032 / section 15.5): restart-effective, deletes nothing
		Audit:       audit.BestEffort(auditLog),
		Log:         logger,
	})
	if err != nil {
		_ = replDB.Close()
		_ = st.Close()
		_ = md.Close()
		return nil, fmt.Errorf("license manager: %w", err)
	}
	if err := licenseMgr.Load(ctx); err != nil {
		_ = replDB.Close()
		_ = st.Close()
		_ = md.Close()
		return nil, fmt.Errorf("loading stored license: %w", err)
	}

	// The unused-cleanup engine (M11 T-324, FR-102.2): the remote-cache
	// policy pass over the same engine + audit the service uses. Built
	// here so the first scheduled tick already sees the opened stack.
	cleanupEng, err := repo.NewCleanupEngine(repo.CleanupOptions{
		Store:        md,
		Engine:       st,
		Audit:        auditLog,
		AuditEnabled: cfg.Audit.Enabled,
		DataDir:      cfg.Storage.DataDir,
		Grace:        cfg.Storage.GCGrace,
	})
	if err != nil {
		_ = replDB.Close()
		_ = st.Close()
		_ = md.Close()
		return nil, fmt.Errorf("cleanup engine: %w", err)
	}

	// The D3 weave (M10 T-283, architecture section 15.1.5 weave 2):
	// repo.Service's package-type legality question now rides the addon
	// manifest joined with the license Manager — the packageTypeGate adapter
	// is the one place both collaborators meet (repo imports neither
	// package; it consumes the consumer-side PackageTypeGate seam).
	// Attached after the Manager loaded so the very first request already
	// sees the stored document's verdict.
	addonsReg := addonManifest()
	repo.AttachPackageTypeGate(svc, packageTypeGate{reg: addonsReg, ev: licenseMgr})

	// The trash can (M12 T-345, FR-106): the feature configuration (spec
	// defaults — enabled, 14-day retention; the config.yaml field is a
	// registered follow-up) plus the license-plane gate over the trashcan
	// slot (community keeps the M11 hard delete; pro captures), and the
	// retention cron over the same store/audit collaborators.
	repo.ConfigureTrash(svc, repo.DefaultTrashConfig())
	repo.AttachTrashGate(svc, trashcanGate{reg: addonsReg, ev: licenseMgr})
	trashEng, err := repo.NewTrashEngine(repo.TrashEngineOptions{
		Store:         md,
		Audit:         auditLog,
		Gate:          trashcanGate{reg: addonsReg, ev: licenseMgr},
		RetentionDays: repo.TrashDefaultRetentionDays,
	})
	if err != nil {
		_ = replDB.Close()
		_ = st.Close()
		_ = md.Close()
		return nil, fmt.Errorf("trash retention engine: %w", err)
	}

	return &stack{
		md:             md,
		st:             st,
		authSvc:        authSvc,
		auditLog:       auditLog,
		svc:            svc,
		genericHandler: generic.New(svc, md.Blobs()),
		authCfg:        authCfgMgr,
		keypairs:       keypairMgr,
		signer:         keypairSigner,
		replStore:      replStore,
		replDB:         replDB,
		replEngine:     replEngine,
		replCipher:     replCipher,
		licenseMgr:     licenseMgr,
		cleanupEng:     cleanupEng,
		trashEng:       trashEng,
		addonsReg:      addonsReg,
		dataDir:        cfg.Storage.DataDir,
	}, nil
}

// trashcanGate joins the assembled addon manifest with the license Manager
// onto repo.TrashGate (the packageTypeGate posture: the one adapter both
// collaborators meet at; repo imports neither package).
type trashcanGate struct {
	reg *addons.Registry
	ev  addons.Evaluator // *license.Manager
}

// Unlocked implements repo.TrashGate.
func (g trashcanGate) Unlocked(ctx context.Context) bool {
	st, ok := g.reg.StatusOf(ctx, g.ev, addons.Trashcan().ID)
	return ok && st.Enable
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

// wireKeypairManager builds the instance GPG keypair plane (T-319,
// ADR-0038 / docs/design/gpg-keypair.md): the Manager over the 016
// gpg_keypairs rows, the enc:v1 cipher from the same instance master key
// the auth-config and replication planes seal with, and the repositories'
// configs (the delete guard's reference scan and the repo-keyed public-key
// lookup). BootCheck enforces the static-secret posture: sealed rows
// without the master key fail the boot. The signing seam T-321/T-322
// consume assembles from the same store + cipher + repo source
// (keypair.NewSigningService) inside their adapter wiring.
func wireKeypairManager(ctx context.Context, md metadata.Store, logger *slog.Logger) (*keypair.Manager, *keypair.SigningService, error) {
	cipher, err := replicationCipher()
	if err != nil {
		return nil, nil, fmt.Errorf("keypair master key: %w", err)
	}
	mgr, err := keypair.NewManager(keypair.Options{
		Store:  md.GpgKeypairs(),
		Cipher: cipherSeam(cipher),
		Repos:  md.Repos(),
		Log:    logger,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("keypair manager: %w", err)
	}
	if err := mgr.BootCheck(ctx); err != nil {
		return nil, nil, fmt.Errorf("wiring keypair plane: %w", err)
	}
	// The signing seam (T-321/T-322): assembled from the same store +
	// cipher + repo source so the adapters' signatures and the REST plane
	// resolve one identical key universe.
	svc, err := keypair.NewSigningService(md.GpgKeypairs(), cipherSeam(cipher), md.Repos())
	if err != nil {
		return nil, nil, fmt.Errorf("keypair signing service: %w", err)
	}
	return mgr, svc, nil
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

// wireAuthConfigManager builds and loads the auth-configuration plane
// (T-305, ADR-0035 / FR-92): the ConfigManager over the 015 auth_configs
// rows, the enc:v1 cipher from the instance master key
// (BINFLOW_REMOTE_CREDENTIALS_KEY — the same key replication seals with),
// and the M3-Guard-backed outbound seams every probe and every OIDC
// discovery rides (zero new SSRF surface; the private-address posture is
// the ADR-0025 decision-4 default: IdPs and directories realistically sit
// on internal networks, non-private categories — Teredo, malformed — still
// refuse).
//
// Load resolves the dual-source rules (K31): stored rows win (the leftover
// file sections WARN), missing rows plus configured file sections seed the
// DB once (secrets sealed from the env values), then the snapshot replays
// with provider rebuilds — an enabled OIDC section whose issuer does not
// answer discovery fails the boot, exactly the M6 fail-fast posture.
func wireAuthConfigManager(ctx context.Context, cfg *config.Config, md metadata.Store, logger *slog.Logger) (*auth.ConfigManager, error) {
	cipher, err := replicationCipher()
	if err != nil {
		return nil, fmt.Errorf("auth config master key: %w", err)
	}
	guard := remote.NewGuard(remote.GuardOptions{
		RepoKey:              "auth-config",
		AllowPrivateUpstream: true,
		Logger:               logger,
	})
	dial := guard.Dialer(10 * time.Second)
	client := &http.Client{Transport: &http.Transport{DialContext: dial}} //nolint:gosec // probe posture: short-lived, body-capped reads
	mgr, err := auth.NewAuthConfigManager(auth.ConfigOptions{
		Store:        auth.NewConfigStoreAdapter(md.AuthConfigs()),
		Cipher:       cipherSeam(cipher),
		LDAPResolver: auth.NewLDAPResolver(md.Users()),
		OIDCResolver: auth.NewOIDCResolver(md.Users()),
		HTTPClient:   client,
		Screen:       guard.CheckURL,
		Dial:         dial,
		Log:          logger,
	})
	if err != nil {
		return nil, fmt.Errorf("auth config manager: %w", err)
	}
	if err := mgr.Load(ctx, authConfigSeeds(cfg)); err != nil {
		return nil, fmt.Errorf("wiring auth config: %w", err)
	}
	return mgr, nil
}

// cipherSeam hands the *remote.Cipher to the manager as auth's consumer-side
// SecretCipher — a nil *remote.Cipher must produce a NIL interface (the
// manager's nil check decides the no-master-key posture).
func cipherSeam(c *remote.Cipher) auth.SecretCipher {
	if c == nil {
		return nil
	}
	return c
}

// authConfigSeeds derives the first-boot seed docs from the binflow.yaml
// auth sections (K31 rule ②). Secrets ride in plaintext from the env reads
// (config.Load already resolved the env names into the fields; the direct
// os.Getenv covers hand-built configs — the same belt-and-braces posture
// the static M6 wiring kept); Load seals them into the row. Every doc is
// CANONICALIZED through the section decoder (defaults applied) so an
// all-default section equals the "unset" shape — Load's configured test
// then correctly leaves it unseeded.
func authConfigSeeds(cfg *config.Config) map[string][]byte {
	seeds := map[string][]byte{}

	oc := cfg.Auth.OIDC
	secret := oc.ClientSecret
	if secret == "" {
		secret = os.Getenv(config.OIDCClientSecretEnvVar)
	}
	seeds[auth.SectionOIDC] = canonicalSeed(auth.SectionOIDC, &auth.OIDCSection{
		Enabled:         oc.Enabled,
		IssuerURL:       oc.IssuerURL,
		ClientID:        oc.ClientID,
		ClientSecret:    secret,
		RedirectURL:     oc.RedirectURL,
		Scopes:          oc.Scopes,
		UserClaim:       oc.UserClaim,
		GroupClaim:      oc.GroupClaim,
		AdminGroup:      oc.AdminGroup,
		ReadOnlyGroup:   oc.ReadOnlyGroup,
		AutoCreateUsers: true,
	})

	lc := cfg.Auth.LDAP
	bindPassword := lc.BindPassword
	if bindPassword == "" {
		bindPassword = os.Getenv(config.LDAPBindPasswordEnvVar)
	}
	seeds[auth.SectionLDAP] = canonicalSeed(auth.SectionLDAP, &auth.LDAPSection{
		Key:     auth.SectionLDAP,
		Enabled: lc.Enabled,
		LDAPURL: ldapSeedURL(lc.URL, lc.BaseDN),
		Search: auth.LDAPSearchSection{
			SearchFilter:    seedFilter(lc.UserFilter),
			SearchSubTree:   true,
			ManagerDN:       lc.BindDN,
			ManagerPassword: bindPassword,
		},
		AutoCreateUser:          true,
		EmailAttribute:          "mail",
		PagingSupportEnabled:    true,
		LDAPPoisoningProtection: true,
		GroupFilter:             seedFilter(lc.GroupFilter),
		GroupBaseDN:             lc.GroupBaseDN,
		GroupNameAttribute:      lc.GroupNameAttr,
		AdminGroup:              lc.AdminGroup,
		ReadOnlyGroup:           lc.ReadOnlyGroup,
		StartTLS:                lc.StartTLS,
		SkipTLSVerify:           lc.SkipTLSVerify,
		PoolSize:                lc.PoolSize,
	})
	return seeds
}

// canonicalSeed marshals one section struct and round-trips it through the
// strict decoder so the stored/compared form carries the full defaults.
func canonicalSeed(section string, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	if dec, err := auth.DecodeAuthSection(section, b); err == nil {
		if b2, err := json.Marshal(dec); err == nil {
			return b2
		}
	}
	return b
}

// ldapSeedURL folds the search base DN into the ldapUrl path (the §1.1 #3
// wire shape the section plane speaks).
func ldapSeedURL(urlStr, baseDN string) string {
	if urlStr == "" {
		return ""
	}
	baseDN = strings.Trim(baseDN, "/ ,")
	if baseDN == "" {
		return urlStr
	}
	return strings.TrimSuffix(urlStr, "/") + "/" + baseDN
}

// seedFilter converts the runtime's %s placeholder spelling into the wire's
// {0} (the boundary conversion, §7's naming-trap note).
func seedFilter(f string) string {
	return strings.ReplaceAll(f, "%s", "{0}")
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
func openStorageEngine(ctx context.Context, cfg *config.Config, logger *slog.Logger, md metadata.Store) (storage.Engine, error) {
	if cfg.Storage.Backend != config.StorageBackendS3 {
		if cfg.Storage.Migration.Enabled && !cfg.Storage.Migration.Completed {
			return nil, fmt.Errorf("opening storage engine: storage.migration.enabled requires storage.backend=s3 (dual-write targets the S3 backend)")
		}
		st, err := storage.OpenEngine(cfg.Storage.DataDir, storage.Options{
			SessionTTL: cfg.Storage.SessionTTL,
			GCHoldTTL:  cfg.Storage.GCHoldTTL,
			Sessions:   md.UploadSessions(),
		})
		if err != nil {
			return nil, fmt.Errorf("opening storage engine at %s: %w", cfg.Storage.DataDir, err)
		}
		return st, nil
	}

	s3Engine, err := openS3Engine(ctx, cfg, md)
	if err != nil {
		return nil, err
	}

	mig := cfg.Storage.Migration
	if mig.Enabled && !mig.Completed {
		diskEngine, derr := storage.OpenEngine(cfg.Storage.DataDir, storage.Options{
			SessionTTL: cfg.Storage.SessionTTL,
			GCHoldTTL:  cfg.Storage.GCHoldTTL,
			Sessions:   md.UploadSessions(),
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
		// ADR-0040's boot gate (T-338 wiring 3/3): a completed migration
		// with a non-empty replay queue means blobs exist that S3 never
		// received — serving s3-only would hide them. Refuse to boot
		// (the ADR-0036 divergence posture: divergence is a data-
		// visibility incident, not a degraded mode).
		if err := storage.CheckCompletedReplayQueue(cfg.Storage.DataDir); err != nil {
			return nil, fmt.Errorf("opening storage engine: %w", err)
		}
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
func openS3Engine(ctx context.Context, cfg *config.Config, md metadata.Store) (storage.Engine, error) {
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

	eng, err := storage.OpenS3EngineWithClient(client, sc.Bucket, &storage.S3EngineOptions{
		BucketPrefix: sc.BucketPrefix,
		PartSize:     sc.UploadPartSize,
		GCHoldTTL:    cfg.Storage.GCHoldTTL,
		// T-323: the upload_sessions rows carry the MPU ids for
		// kill-9 resume (ListParts rebuild); the TTL clock is shared
		// with the orphan-MPU sweep — the disk arm's own posture.
		Sessions:   md.UploadSessions(),
		SessionTTL: cfg.Storage.SessionTTL,
	})
	if err != nil {
		return nil, fmt.Errorf("opening s3 engine: sweep orphan uploads in %s: %w", sc.Bucket, err)
	}
	return eng, nil
}

// close tears the stack down in the section 7.4 order. It is safe to call
// twice (every closer is idempotent).
func (s *stack) close(logger *slog.Logger) {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	// The auth-configuration manager owns the live LDAP pool (T-305); it
	// dials outside both engines, so it drains first (the auth plane closes
	// before the data plane).
	if s.authCfg != nil {
		s.authCfg.Close()
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
// there is no ambiguity to reject. --grace-seconds ([M9] ADR-0031) is the
// sub-minute spelling of the same ladder and wins over --grace-hours; it is
// what makes an explicit no-window CLI run expressible, which is exactly the
// run the serve.lock gate below guards.
func runGC(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "", "path to binflow.yaml (same resolution as serve)")
	apply := fs.Bool("apply", false, "delete blobs instead of dry-run listing")
	graceDays := fs.Int("grace-days", 0, "override storage.gc_grace for this run in days (positive integer, 0 = use config)")
	graceHours := fs.Int("grace-hours", 0, "override storage.gc_grace for this run in hours, sub-day precision (positive integer, 0 = use config; wins over --grace-days)")
	graceSeconds := fs.Int("grace-seconds", 0, "override storage.gc_grace for this run in seconds, sub-minute precision (positive integer, 0 = use config; wins over --grace-hours)")
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
	if *graceSeconds < 0 {
		return fmt.Errorf("gc: --grace-seconds must be a positive integer, got %d", *graceSeconds)
	}

	ctx := context.Background()

	cfg, err := loadServeConfig(*configPath)
	if err != nil {
		return err
	}
	// T-306: the loader's non-fatal findings (binstore coexistence hints)
	// reach the CLI surface here — gc's logger only exists deeper in.
	writeStartupWarnings(stderr, cfg)

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
	if *graceSeconds > 0 {
		grace = time.Duration(*graceSeconds) * time.Second
		graceOverride = true
	}

	// The serve.lock cross-process gate ([M9] ADR-0031 / architecture
	// section 14.2 point 4): an APPLY run with an explicit sub-minute grace
	// while a serve process holds the data directory's heartbeat lock is
	// refused before anything is opened. The CLI process cannot see serve's
	// in-flight hold set (it is in-process state by design, section 9's
	// single-instance precondition), so a no-window sweep here could delete
	// a blob whose reference is still being written — the T-232 race, W-1
	// half. Default-grace runs are unrestricted (the mtime window covers the
	// millisecond-scale pre-reference gap); dry runs are unrestricted (no
	// destructive face) and get the K22 annotation below instead.
	if *apply && grace < storage.MinGCHoldTTL && serveRunning(cfg.Storage.DataDir) {
		return fmt.Errorf(
			"gc: refusing --apply with grace %s while serve is running on %s: a cross-process sweep cannot see serve's in-flight uploads (serve.lock held); use the REST gc endpoint (POST /binflow/api/v1/system/gc — it shares serve's in-flight hold set) or stop serve and rerun in a maintenance window",
			grace, cfg.Storage.DataDir)
	}
	// K22 wording: while serve runs, this process's candidate list can
	// over-report (in-flight uploads are invisible cross-process — their
	// holds live in serve). The dry-run report says so up front instead of
	// pretending precision it cannot have.
	serveLive := serveRunning(cfg.Storage.DataDir)

	// Data-directory maintenance lock: held for the whole run (mark, sweep,
	// ledger cleanup) — export is refused while gc works and vice versa.
	lock, err := storage.AcquireDataLock(cfg.Storage.DataDir, storage.DataLockOpGC)
	if err != nil {
		return fmt.Errorf("gc: %w", err)
	}
	defer func() { _ = lock.Release() }()

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	// Metadata opens first so the storage engine can hand its upload_sessions
	// store to the startup sweep (T-209: sessions are DB-backed).
	md, err := metadata.Open(ctx, metadata.Options{
		Driver:        cfg.Metadata.Driver,
		DSN:           sqlitePath(cfg),
		AdminPassword: cfg.AdminPassword,
	})
	if err != nil {
		return fmt.Errorf("gc: opening metadata: %w", err)
	}
	defer func() { _ = md.Close() }()

	st, err := storage.OpenEngine(cfg.Storage.DataDir, storage.Options{
		SessionTTL: cfg.Storage.SessionTTL,
		GCHoldTTL:  cfg.Storage.GCHoldTTL,
		Sessions:   md.UploadSessions(),
	})
	if err != nil {
		return fmt.Errorf("gc: opening storage engine at %s: %w", cfg.Storage.DataDir, err)
	}
	defer func() { _ = st.Close() }()

	marker := cliGCMarker{ctx: ctx, md: md}

	// Pass 1 is always the dry pass — the same two-pass shape the REST face
	// runs (T-94): it yields the candidate list and, while every file still
	// exists, their on-disk bytes for the report and the gc.run audit.
	// --apply sweeps for real as pass 2 over a freshly recomputed mark set.
	// Since T-256 both passes ride GCSweep with the real GCMarker — the
	// per-candidate Live recheck (ADR-0031 mechanism A) works cross-process
	// too, because it reads the shared metadata store.
	if serveLive {
		writeCLIReport(stderr, "gc: note: serve is running on %s; this cross-process run cannot see in-flight uploads — candidates may over-report\n",
			cfg.Storage.DataDir)
	}
	candidates, err := st.GCSweep(ctx, marker, grace, false)
	if err != nil {
		return fmt.Errorf("gc: %w", err)
	}
	candidateBytes, statErr := sumBlobFileSizes(cfg.Storage.DataDir, candidates)
	if statErr != nil {
		logger.Warn("gc: candidate sizing incomplete", "error", statErr.Error())
	}

	var deleted []string
	if *apply {
		deleted, err = st.GCSweep(ctx, marker, grace, true)
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

// cliGCMarker is the CLI gc's storage.GCMarker ([M9] ADR-0031 decision 3):
// Mark is the liveChecksumSet walk below, Live is the metadata store's
// single-point IsReferenced probe — the per-candidate pre-delete recheck
// that closes the stale-snapshot window (W-2). The engine calls both
// synchronously inside one GCSweep, so the run's context rides along here.
type cliGCMarker struct {
	ctx context.Context
	md  metadata.Store
}

// Mark implements storage.GCMarker.
func (m cliGCMarker) Mark() (map[string]struct{}, error) {
	set, err := liveChecksumSet(m.ctx, m.md)
	if err != nil {
		return nil, fmt.Errorf("gc: referenced set: %w", err)
	}
	return set, nil
}

// Live implements storage.GCMarker.
func (m cliGCMarker) Live(sha256 string) (bool, error) {
	live, err := m.md.IsReferenced(m.ctx, sha256)
	if err != nil {
		return false, fmt.Errorf("gc: reference recheck %s: %w", sha256, err)
	}
	return live, nil
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
