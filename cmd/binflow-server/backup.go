package main

// Backup/restore subcommands (FR-32, GE-07/08/09; ADR-0015 decisions 3-5,
// architecture section 7.6):
//
//	export   online export: data lock -> SQLite snapshot FIRST -> blobs copy
//	         (mtime preserved) -> manifest; artifact dir 0700 (NFR-S22)
//	import   offline restore into an EMPTY data dir: verify everything
//	         (manifest + sizes + sha spot/full) before writing a single byte,
//	         then db -> blobs; any failure clears the target back to empty
//
// The blob half of both faces is engine-aware (T-201, T-173 D-2): on
// storage.backend=s3 (without a live dual-write migration) the blobs come
// out of / go back into the storage engine — the data directory carries no
// blobs/ tree there. Dual-write stacks keep the tree copy; the migration's
// restart-safe re-scan reconciles the difference.
//
// There is deliberately no REST face for either (GE-09: /api/export/** and
// /api/import/** stay 404 — the write side is high-risk and belongs to an
// out-of-band CLI, ADR-0015 decision 5).
//
// The consistency order is the one hard rule of the online export (ADR-0015
// decision 3): the DB snapshot is taken while GC is excluded by the
// data-directory lock and BEFORE any blob is copied, so a blob that lands
// during the copy window can only ever be surplus (an unreferenced file the
// restored instance's GC collects), never a dangling reference. The reverse
// order would let the snapshot reference blobs the copy never saw.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

const (
	// exportDBName is the snapshot database's file name inside a backup
	// directory (the PRD W28 layout: <out>/metadata.db).
	exportDBName = "metadata.db"

	// spotVerifyCount is the default sha256 spot-check sample: the first
	// 100 manifest blobs (manifest order is sha-sorted, so the sample is
	// deterministic). --verify full rehashes every blob (FR-32-AC7, P1).
	spotVerifyCount = 100

	// cliAuditActor is the audit actor CLI governance operations record
	// under (mirrors the T-94 gc.run ruling: REST and CLI paths both record
	// actor=admin — a CLI has no authenticated principal to name).
	cliAuditActor = "admin"

	// dataLockResidue is the maintenance lock file's name inside the data
	// directory — aliased to storage's single source of truth (exported for
	// exactly this consumer, T-96 review B1) so the two spellings can never
	// drift. Every "is this directory empty / make it empty" helper here
	// must exempt it: the file is advisory-lock runtime residue, never
	// restored state, and unlinking a HELD lock file is the classic flock
	// lifecycle bug (datalock.go's own comment names it).
	dataLockResidue = storage.MaintenanceLockName
)

// metadataSnapshotter is the snapshot capability the SQLite store carries
// (internal/metadata snapshot.go). Defined here, consumer-side, because only
// this command needs it; a future postgres store that gains an equivalent
// face satisfies it without touching metadata's interface.
type metadataSnapshotter interface {
	VacuumInto(ctx context.Context, dst string) error
}

// runExport implements the export subcommand (GE-07, W28/W28b). Online by
// design: the serving process keeps running; only GC is excluded (data
// lock), because a concurrent sweep would delete blobs mid-copy.
func runExport(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "", "path to binflow.yaml (same resolution as serve)")
	output := fs.String("output", "", "directory to write the backup into (must not exist, or exist empty; created 0700)")
	tar := fs.Bool("tar", false, "write a single-file tar artifact instead of a directory (not implemented in M4; P2 backlog)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing export flags: %w", err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q, see --help for usage", fs.Arg(0))
	}
	if *tar {
		return errors.New("export: --tar is not implemented in M4 (P2 backlog); the directory artifact is the M4 form")
	}
	if *output == "" {
		return errors.New("export: --output is required")
	}

	ctx := context.Background()
	cfg, err := loadServeConfig(*configPath)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	out, err := filepath.Abs(*output)
	if err != nil {
		return fmt.Errorf("export: resolving --output %s: %w", *output, err)
	}
	if err := prepareBackupDir(out); err != nil {
		return fmt.Errorf("export: %w", err)
	}
	// A failure after this point must not leave a directory that looks like
	// a backup: drop the artifacts this run created before returning.
	completed := false
	defer func() {
		if !completed {
			cleanupBackupDir(out)
		}
	}()

	// Mutual exclusion with GC (ADR-0015 erratum 3; the primitive is
	// storage.AcquireDataLock, landed here ahead of T-94's REST face —
	// both faces must hold the same lock).
	lock, err := storage.AcquireDataLock(cfg.Storage.DataDir, storage.DataLockOpExport)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	if err := checkDirDisjoint(cfg.Storage.DataDir, out); err != nil {
		return fmt.Errorf("export: %w", err)
	}

	md, err := metadata.Open(ctx, metadata.Options{
		Driver:        cfg.Metadata.Driver,
		DSN:           sqlitePath(cfg),
		AdminPassword: cfg.AdminPassword,
	})
	if err != nil {
		return fmt.Errorf("export: opening metadata: %w", err)
	}
	defer func() { _ = md.Close() }()

	snap, ok := md.(metadataSnapshotter)
	if !ok {
		return fmt.Errorf("export: metadata store %T has no snapshot face (SQLite only in M4)", md)
	}

	started := time.Now()

	// Step 1 — DB snapshot FIRST (the ADR-0015 hard order).
	snapPath := filepath.Join(out, exportDBName)
	if err := snap.VacuumInto(ctx, snapPath); err != nil {
		return fmt.Errorf("export: %w", err)
	}
	// VACUUM INTO creates the file under the umask (0644 typically); the
	// artifact is secret-grade (NFR-S22), so pin it to 0600 like the
	// manifest regardless of how the operator's umask falls.
	if err := os.Chmod(snapPath, 0o600); err != nil {
		return fmt.Errorf("export: chmod %s 0600: %w", snapPath, err)
	}
	// web_sessions never ride a backup (architecture 11.19); purge them from
	// the artifact before it is hashed.
	if err := metadata.PurgeTransientFromSnapshot(ctx, snapPath); err != nil {
		return fmt.Errorf("export: %w", err)
	}
	// The manifest boundary is the SNAPSHOT's own live set — not the live
	// store's, which keeps moving.
	live, err := metadata.SnapshotChecksums(ctx, snapPath)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	schemaVersion, err := metadata.SnapshotSchemaVersion(ctx, snapPath)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}

	// Step 2 — copy blobs (mtime preserved; surplus window blobs allowed).
	//
	// Engine-aware since T-201 (T-173 D-2): an S3-backed instance streams
	// each referenced blob OUT of the storage engine — the data directory
	// carries no blobs/ tree to walk, so the previous unconditional
	// CopyBlobsTree made every S3 export die at the manifest boundary
	// ("dangling reference") after copying nothing. Dual-write stacks keep
	// the tree copy: while a migration runs, the disk half still carries
	// every blob.
	var files int
	var copied int64
	if engineBackedBlobStore(cfg) {
		st, oerr := openStorageEngine(ctx, cfg, logger)
		if oerr != nil {
			return fmt.Errorf("export: %w", oerr)
		}
		defer func() { _ = st.Close() }()
		files, copied, err = exportBlobsFromEngine(ctx, st, live, out)
	} else {
		files, copied, err = storage.CopyBlobsTree(cfg.Storage.DataDir, out)
	}
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}

	// Step 3 — manifest. Every referenced blob must be present in the copy
	// (W28b: a reference the artifact cannot satisfy means the SOURCE has a
	// dangling reference — refuse rather than ship a broken backup).
	manifest := &storage.Manifest{
		FormatVersion:  storage.BackupFormatVersion,
		CreatedAt:      metadata.Now(),
		BinflowVersion: version,
		Metadata:       storage.ManifestMetadata{File: exportDBName},
		GraceNote:      storage.ManifestGraceNote,
	}
	manifest.Blobs = make([]storage.ManifestBlob, 0, len(live))
	for sha := range live {
		p, err := storage.BlobPath(out, sha)
		if err != nil {
			return fmt.Errorf("export: %w", err)
		}
		info, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("export: snapshot references blob %s but the filestore under %s does not carry it (dangling reference in the source instance): %w", sha, cfg.Storage.DataDir, err)
		}
		manifest.Blobs = append(manifest.Blobs, storage.ManifestBlob{
			Sha256: sha,
			Size:   info.Size(),
			MTime:  info.ModTime().UTC().Format(time.RFC3339Nano),
		})
	}
	storage.SortManifestBlobs(manifest)
	manifest.BlobCount = len(manifest.Blobs)
	metaSum, metaSize, err := storage.HashFile(snapPath)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	manifest.Metadata.Sha256 = metaSum
	for _, b := range manifest.Blobs {
		manifest.TotalBytes += b.Size
	}
	if err := storage.WriteManifest(manifest, storage.ManifestPath(out)); err != nil {
		return fmt.Errorf("export: %w", err)
	}

	// NFR-S22: the artifact carries password hashes and enc:v1: credential
	// ciphertext — it is secret-grade and stored as such.
	if err := os.Chmod(out, 0o700); err != nil { //nolint:gosec // G302: 0700 on a directory is the NFR-S22 artifact posture, not a slip
		return fmt.Errorf("export: chmod %s 0700: %w", out, err)
	}

	// audit export.run on the LIVE store (the snapshot is already sealed;
	// the event belongs to the source instance's own history).
	elapsed := time.Since(started)
	detail, _ := json.Marshal(struct {
		Output     string `json:"output"`
		BlobCount  int    `json:"blobCount"`
		TotalBytes int64  `json:"totalBytes"`
		DurationMs int64  `json:"durationMs"`
	}{out, manifest.BlobCount, manifest.TotalBytes, elapsed.Milliseconds()})
	audit.BestEffort(audit.New(md, cfg.Audit.Enabled)).Record(ctx, audit.Event{
		Actor: cliAuditActor, Action: audit.ActionExportRun, Detail: string(detail),
	})

	logger.Info("export complete",
		"output", out,
		"blob_count", manifest.BlobCount,
		"referenced_bytes", manifest.TotalBytes,
		"files_copied", files,
		"bytes_copied", copied,
		"snapshot_bytes", metaSize,
		"schema_version", schemaVersion,
		"throughput", throughput(copied, elapsed),
		"duration", elapsed.String(),
	)
	writeCLIReport(stderr, "export: mode=online output=%s blobs=%d bytes=%d throughput=%s\n",
		out, manifest.BlobCount, manifest.TotalBytes, throughput(copied, elapsed))

	completed = true
	return nil
}

// runImport implements the import subcommand (GE-08, W30/W30b/W31/W32).
// Offline by contract: the target instance must be stopped and its data
// directory empty. Everything the backup claims is verified BEFORE the first
// byte is written, and a write-phase failure clears the target back to
// empty — a half-restored data directory must never exist.
//
// The whole run holds the data-directory maintenance lock (correctness
// review B1): import is as much a data-directory-wide maintenance operation
// as gc or export, and a concurrent `gc --apply` against the (lock-residue
// only, hence "empty") target would build a binflow.db under a restore in
// progress. requireEmptyDataDir and the failure cleanup both treat the lock
// file as runtime residue, so the lock composes with every existing check.
func runImport(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "", "path to binflow.yaml (same resolution as serve)")
	input := fs.String("input", "", "backup directory produced by 'binflow-server export'")
	verify := fs.String("verify", "spot", "integrity mode: spot (sizes for every blob, sha256 for the first 100) or full (rehash every blob)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing import flags: %w", err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q, see --help for usage", fs.Arg(0))
	}
	if *input == "" {
		return errors.New("import: --input is required")
	}
	switch *verify {
	case "spot", "full":
	default:
		return fmt.Errorf("import: --verify must be spot or full, got %q", *verify)
	}

	ctx := context.Background()
	cfg, err := loadServeConfig(*configPath)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// B2: import restores a file-level SQLite snapshot; any other driver
	// has no file to restore (and a postgres URL would otherwise be treated
	// as a path and littered with a garbage tree outside the data dir).
	if cfg.Metadata.Driver != config.DriverSQLite {
		return fmt.Errorf("import: metadata.driver %q is not supported: import restores a SQLite snapshot file (set metadata.driver: sqlite)", cfg.Metadata.Driver)
	}

	in, err := filepath.Abs(*input)
	if err != nil {
		return fmt.Errorf("import: resolving --input %s: %w", *input, err)
	}

	// B1: hold the maintenance lock for the whole run — the empty check
	// below and the write phase must be one indivisible interval against
	// gc (which takes the same lock on the same directory).
	lock, err := storage.AcquireDataLock(cfg.Storage.DataDir, storage.DataLockOpImport)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	defer func() { _ = lock.Release() }()

	// Fail fast on a non-empty target BEFORE reading the backup: no matter
	// how broken the backup is, the target directory is left exactly as the
	// operator left it (and import never merges — ADR-0015 decision 4).
	if err := requireEmptyDataDir(cfg.Storage.DataDir); err != nil {
		return fmt.Errorf("import: %w", err)
	}

	// ---- verification phase (read-only; no target byte is written) ----

	manifest, err := storage.LoadManifest(storage.ManifestPath(in))
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	metaSrc := filepath.Join(in, manifest.Metadata.File)
	gotSum, _, err := storage.HashFile(metaSrc)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	if gotSum != manifest.Metadata.Sha256 {
		return fmt.Errorf("import: metadata file %s hashes to %s, manifest says %s — the backup is corrupted", metaSrc, gotSum, manifest.Metadata.Sha256)
	}
	// Migration chain reachability (ADR-0015 decision 4): a snapshot from a
	// NEWER build cannot be downgraded; one from an older build migrates up
	// on the first open after restore.
	schemaVersion, err := metadata.SnapshotSchemaVersion(ctx, metaSrc)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	if schemaVersion > metadata.LatestSchemaVersion() {
		return fmt.Errorf("import: backup schema version %d is newer than this build carries (%d) — the migration chain is not reachable, refusing", schemaVersion, metadata.LatestSchemaVersion())
	}
	hashed, err := verifyBackupBlobs(in, manifest, *verify)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}

	// ---- write phase (db first, then blobs — ADR-0015 decision 4) ----

	// B2: an explicit metadata.dsn may point outside the data directory.
	// The empty-target guarantee only covers the data directory, so the
	// out-of-bounds landing point gets its own guards: it must not already
	// exist (import restores into an empty instance only — no silent
	// O_TRUNC of some other database), and it joins the failure cleanup
	// below so a mid-write failure cannot leave residue outside the data
	// directory either.
	started := time.Now()
	dbDst := sqlitePath(cfg)
	dbAbs, err := filepath.Abs(dbDst)
	if err != nil {
		return fmt.Errorf("import: resolving metadata target %s: %w", dbDst, err)
	}
	dataAbs, err := filepath.Abs(cfg.Storage.DataDir)
	if err != nil {
		return fmt.Errorf("import: resolving data dir %s: %w", cfg.Storage.DataDir, err)
	}
	dbOutsideDataDir, err := pathOutside(dataAbs, dbAbs)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	if dbOutsideDataDir {
		if _, err := os.Stat(dbAbs); err == nil {
			return fmt.Errorf("import: metadata target %s already exists — import restores into an empty instance only and refuses to overwrite a database outside the data directory", dbAbs)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("import: checking metadata target %s: %w", dbAbs, err)
		}
	}

	restored := false
	defer func() {
		if !restored {
			cleanupFailedImport(cfg.Storage.DataDir, dbAbs, dbOutsideDataDir)
			writeCLIReport(stderr, "import: target %s cleared back to empty (no half-restore)\n", cfg.Storage.DataDir)
		}
	}()

	if dir := filepath.Dir(dbAbs); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("import: creating %s: %w", dir, err)
		}
	}
	if err := copyFile(metaSrc, dbAbs, 0o600); err != nil {
		return fmt.Errorf("import: %w", err)
	}
	// Blob restore is engine-aware (T-201, T-173 D-2 — the symmetric half
	// of the export fix): an S3-backed instance uploads the artifact's
	// blobs INTO the bucket through the engine's session path (the same
	// drive the migration engine uses), because a data-dir blobs/ tree is
	// not where that instance reads blobs from. A write-phase failure rolls
	// the uploads back through the engine — the no-half-restore guarantee
	// extends to the bucket for blobs THIS run created (pre-existing
	// objects are never touched: a blob the bucket already carries is
	// skipped as idempotent, not re-uploaded, and must not be deleted).
	// Dual-write stacks keep the tree copy; the migration's idempotent
	// re-scan syncs the restored disk blobs to S3 on its next run.
	if engineBackedBlobStore(cfg) {
		st, oerr := openStorageEngine(ctx, cfg, logger)
		if oerr != nil {
			return fmt.Errorf("import: %w", oerr)
		}
		defer func() { _ = st.Close() }()
		if uerr := importBlobsIntoEngine(ctx, st, in, manifest, logger); uerr != nil {
			return fmt.Errorf("import: %w", uerr)
		}
	} else if _, _, cerr := storage.CopyBlobsTree(in, cfg.Storage.DataDir); cerr != nil {
		return fmt.Errorf("import: %w", cerr)
	}
	// The engine's posture for a data directory: secret-grade contents.
	if err := os.Chmod(cfg.Storage.DataDir, 0o700); err != nil { //nolint:gosec // G302: matches the engine's 0700 data-dir posture (OpenEngine)
		return fmt.Errorf("import: chmod %s 0700: %w", cfg.Storage.DataDir, err)
	}

	// Record import.run in the RESTORED instance's audit trail. Opening
	// through metadata.Open also migrates an older snapshot up (the
	// reachable direction checked above) — a failure here is a write-phase
	// failure and clears the target like any other.
	md, err := metadata.Open(ctx, metadata.Options{
		Driver:        cfg.Metadata.Driver,
		DSN:           dbAbs,
		AdminPassword: cfg.AdminPassword,
	})
	if err != nil {
		return fmt.Errorf("import: opening restored metadata: %w", err)
	}
	defer func() { _ = md.Close() }()
	elapsed := time.Since(started)
	detail, _ := json.Marshal(struct {
		Input      string `json:"input"`
		BlobCount  int    `json:"blobCount"`
		TotalBytes int64  `json:"totalBytes"`
		Verify     string `json:"verify"`
		SchemaFrom int    `json:"schemaFrom"`
		DurationMs int64  `json:"durationMs"`
	}{in, manifest.BlobCount, manifest.TotalBytes, *verify, schemaVersion, elapsed.Milliseconds()})
	audit.BestEffort(audit.New(md, cfg.Audit.Enabled)).Record(ctx, audit.Event{
		Actor: cliAuditActor, Action: audit.ActionImportRun, Detail: string(detail),
	})

	logger.Info("import complete",
		"input", in,
		"blob_count", manifest.BlobCount,
		"total_bytes", manifest.TotalBytes,
		"blobs_rehashed", hashed,
		"schema_from", schemaVersion,
		"schema_now", metadata.LatestSchemaVersion(),
		"duration", elapsed.String(),
	)
	writeCLIReport(stderr, "import: input=%s blobs=%d bytes=%d verify=%s rehashed=%d\n",
		in, manifest.BlobCount, manifest.TotalBytes, *verify, hashed)

	restored = true
	return nil
}

// engineBackedBlobStore reports whether the configured instance keeps its
// blobs in the storage engine rather than the data directory's blobs/ tree:
// backend=s3 without a live dual-write migration (T-201, T-173 D-2). A
// dual-write stack's disk half still carries every blob, so both backup
// faces keep the tree copy there — and the migration's restart-safe re-scan
// reconciles anything a restore landed on disk only.
func engineBackedBlobStore(cfg *config.Config) bool {
	if cfg.Storage.Backend != config.StorageBackendS3 {
		return false
	}
	return !cfg.Storage.Migration.Enabled || cfg.Storage.Migration.Completed
}

// blobInventory is the consumer-side spelling of the storage engine's
// read-only listing seam (storage.S3Engine.BlobStats): the export uses the
// per-object LastModified as the artifact's mtime — the bucket's stand-in
// for the disk mtime the tree copy preserves (W28/W32: the GC grace clock
// must not reset on restore). An engine without the seam falls back to the
// write time, the same value a fresh upload would carry.
type blobInventory interface {
	BlobStats(ctx context.Context) (map[string]storage.BlobStat, error)
}

// exportBlobsFromEngine streams every snapshot-referenced blob out of the
// engine into the artifact's blobs/ tree (the engine-backed replacement for
// storage.CopyBlobsTree, T-201). Unlike the tree copy this ships EXACTLY
// the live set — the bucket holds no per-run surplus to speak of, and an
// unreferenced object the operator keeps in the bucket is not the backup's
// business. A blob the engine cannot produce is a dangling reference in the
// SOURCE instance: refuse rather than ship a broken artifact (W28b).
func exportBlobsFromEngine(ctx context.Context, eng storage.Engine, live map[string]struct{}, out string) (files int, bytes int64, err error) {
	mtimes := map[string]time.Time{}
	if inv, ok := eng.(blobInventory); ok {
		stats, serr := inv.BlobStats(ctx)
		if serr != nil {
			return 0, 0, fmt.Errorf("sizing blobs through the storage engine: %w", serr)
		}
		for sha, stat := range stats {
			mtimes[sha] = stat.LastModified
		}
	}
	shas := make([]string, 0, len(live))
	for sha := range live {
		shas = append(shas, sha)
	}
	sort.Strings(shas)
	for _, sha := range shas {
		rc, _, oerr := eng.Open(ctx, sha)
		if oerr != nil {
			return files, bytes, fmt.Errorf("snapshot references blob %s but the storage engine does not carry it (dangling reference in the source instance): %w", sha, oerr)
		}
		dst, perr := storage.BlobPath(out, sha)
		if perr != nil {
			_ = rc.Close()
			return files, bytes, perr
		}
		n, werr := writeBlobFromReader(dst, rc, mtimes[sha])
		if cerr := rc.Close(); werr == nil && cerr != nil {
			werr = fmt.Errorf("close blob %s stream: %w", sha, cerr)
		}
		if werr != nil {
			return files, bytes, werr
		}
		files++
		bytes += n
	}
	return files, bytes, nil
}

// writeBlobFromReader persists one streamed blob at dst: 0600, fsynced (a
// crash mid-backup must not leave a short file under a correct name), and
// stamped with mtime when the source carries one (zero time = keep the
// write time). The temp+rename dance is unnecessary here: the artifact
// directory is this run's private output, cleaned wholesale on failure.
func writeBlobFromReader(dst string, r io.Reader, mtime time.Time) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return 0, fmt.Errorf("create shard dir %s: %w", filepath.Dir(dst), err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // G304: dst is built from the manifest-validated 64-hex digest
	if err != nil {
		return 0, fmt.Errorf("create %s: %w", dst, err)
	}
	n, err := io.Copy(out, r)
	if err != nil {
		_ = out.Close()
		return n, fmt.Errorf("write %s: %w", dst, err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return n, fmt.Errorf("fsync %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return n, fmt.Errorf("close %s: %w", dst, err)
	}
	if !mtime.IsZero() {
		if err := os.Chtimes(dst, mtime, mtime); err != nil {
			return n, fmt.Errorf("restore mtime on %s: %w", dst, err)
		}
	}
	return n, nil
}

// importBlobsIntoEngine uploads the artifact's blobs into the storage
// engine's blob store (the S3-backed restore path, T-201). Blobs are
// streamed through the engine's own session protocol — the same drive the
// migration engine uses — with the manifest's sha256 as the expected
// digest, so the engine's commit-time checksum gate is the integrity check
// on the wire. Idempotent per blob: one the store already carries is
// skipped. A failure mid-run deletes the blobs THIS run created (best
// effort, on a context detached from the failing one) so a failed import
// leaves no half-restored bucket either.
func importBlobsIntoEngine(ctx context.Context, eng storage.Engine, in string, manifest *storage.Manifest, logger *slog.Logger) (err error) {
	var created []string
	defer func() {
		if err == nil {
			return
		}
		rollbackCtx := context.WithoutCancel(ctx)
		for _, sha := range created {
			if derr := eng.Delete(rollbackCtx, sha); derr != nil && !errors.Is(derr, storage.ErrBlobNotFound) {
				logger.Warn("import: rolling back an uploaded blob failed", "sha256", sha, "error", derr.Error())
			}
		}
	}()
	for _, b := range manifest.Blobs {
		if rc, _, perr := eng.Open(ctx, b.Sha256); perr == nil {
			_ = rc.Close()
			continue // the store already carries it: idempotent skip
		} else if !errors.Is(perr, storage.ErrBlobNotFound) {
			return fmt.Errorf("probing blob %s in the target store: %w", b.Sha256, perr)
		}
		src, perr := storage.BlobPath(in, b.Sha256)
		if perr != nil {
			return perr
		}
		f, oerr := os.Open(src) //nolint:gosec // G304: path built from the manifest's verified 64-hex digest
		if oerr != nil {
			return fmt.Errorf("open backup blob %s: %w", src, oerr)
		}
		sess, serr := eng.BeginSession(ctx)
		if serr != nil {
			_ = f.Close()
			return fmt.Errorf("begin upload session for %s: %w", b.Sha256, serr)
		}
		if _, aerr := sess.Append(ctx, f); aerr != nil {
			_ = f.Close()
			_ = sess.Abort(ctx)
			return fmt.Errorf("upload blob %s: %w", b.Sha256, aerr)
		}
		ref, cerr := sess.Commit(ctx, storage.BlobRef{Sha256: b.Sha256, Size: b.Size})
		if cerr != nil {
			_ = f.Close()
			return fmt.Errorf("commit blob %s: %w", b.Sha256, cerr)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("close backup blob %s: %w", src, err)
		}
		if ref.Size != b.Size {
			return fmt.Errorf("blob %s restored as %d bytes, manifest says %d — the backup is corrupted", b.Sha256, ref.Size, b.Size)
		}
		created = append(created, b.Sha256)
	}
	return nil
}

// verifyBackupBlobs checks the artifact's blob claims against the backup
// directory: size for EVERY referenced blob, plus sha256 for the first
// spotVerifyCount (sorted manifest order) — or every blob under
// --verify full. Read-only; the importer runs it before writing anything.
func verifyBackupBlobs(in string, manifest *storage.Manifest, mode string) (hashed int, err error) {
	limit := len(manifest.Blobs)
	if mode == "spot" && limit > spotVerifyCount {
		limit = spotVerifyCount
	}
	for i, b := range manifest.Blobs {
		p, err := storage.BlobPath(in, b.Sha256)
		if err != nil {
			return hashed, err
		}
		info, err := os.Stat(p) //nolint:gosec // G304: path built from the manifest's validated 64-hex digest
		if err != nil {
			return hashed, fmt.Errorf("referenced blob %s is missing from the backup (expected at %s): %w", b.Sha256, p, err)
		}
		if info.Size() != b.Size {
			return hashed, fmt.Errorf("referenced blob %s has size %d, manifest says %d — the backup is corrupted", b.Sha256, info.Size(), b.Size)
		}
		if i < limit {
			sum, _, err := storage.HashFile(p)
			if err != nil {
				return hashed, err
			}
			if sum != b.Sha256 {
				return hashed, fmt.Errorf("referenced blob %s content hashes to %s — the backup is corrupted", b.Sha256, sum)
			}
			hashed++
		}
	}
	return hashed, nil
}

// prepareBackupDir creates the export target (0700, umask-defeated) and
// refuses to reuse a directory that already holds anything — mixing runs
// would blur which manifest owns which blobs.
func prepareBackupDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err == nil {
		if len(entries) > 0 {
			return fmt.Errorf("output directory %s exists and is not empty — export refuses to mix artifacts", dir)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat output directory %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create output directory %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // G302: 0700 on a directory is the NFR-S22 artifact posture, not a slip
		return fmt.Errorf("chmod %s 0700: %w", dir, err)
	}
	return nil
}

// cleanupBackupDir removes the artifacts an aborted export may have created
// (snapshot db, blob tree, manifest), leaving the directory itself in place.
func cleanupBackupDir(dir string) {
	for _, name := range []string{exportDBName, "blobs", "manifest.json"} {
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			// Nothing left to do but be loud: an operator MUST not mistake
			// the residue for a usable backup.
			fmt.Fprintf(os.Stderr, "export: cleanup: remove %s: %v\n", filepath.Join(dir, name), err) //nolint:errcheck // last-resort diagnostics on the failure path
		}
	}
}

// checkDirDisjoint refuses a backup directory that overlaps the data
// directory in either direction: reading and writing the same tree (or a
// subtree of each other) would let the blob copy observe its own output.
func checkDirDisjoint(dataDir, backupDir string) error {
	dataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("resolving data dir %s: %w", dataDir, err)
	}
	rel, err := filepath.Rel(dataDir, backupDir)
	if err != nil {
		return fmt.Errorf("relating %s to %s: %w", backupDir, dataDir, err)
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return fmt.Errorf("output directory %s is inside the data directory %s", backupDir, dataDir)
	}
	relBack, err := filepath.Rel(backupDir, dataDir)
	if err != nil {
		return fmt.Errorf("relating %s to %s: %w", dataDir, backupDir, err)
	}
	if relBack == "." || (relBack != ".." && !strings.HasPrefix(relBack, ".."+string(filepath.Separator))) {
		return fmt.Errorf("data directory %s is inside the output directory %s", dataDir, backupDir)
	}
	return nil
}

// requireEmptyDataDir enforces the import precondition (ADR-0015 decision
// 4): restore targets a fresh instance only. The maintenance lock file is
// the one ignored artifact — it is runtime residue of a lock, never state,
// and refusing on it would block the very normal "gc ran once on the new
// machine" flow without protecting anything.
func requireEmptyDataDir(dataDir string) error {
	entries, err := os.ReadDir(dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil // fresh directory: import creates it
	}
	if err != nil {
		return fmt.Errorf("reading data dir %s: %w", dataDir, err)
	}
	for _, e := range entries {
		if e.Name() == dataLockResidue {
			continue
		}
		return fmt.Errorf("data dir %s is not empty (found %q) — import restores into an empty directory only; move the existing data away first", dataDir, e.Name())
	}
	return nil
}

// clearDirContents removes every entry under dir except the maintenance
// lock file (the no-half-restore guarantee for a failed import write phase),
// leaving the directory itself in place. Skipping the lock file is not
// cosmetic: this runs while import HOLDS the lock (B1), and unlinking a
// held lock file lets the next acquirer lock a fresh inode — the classic
// unlink-while-held race the datalock type comment forbids.
func clearDirContents(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // nothing created or already gone
	}
	for _, e := range entries {
		if e.Name() == dataLockResidue {
			continue
		}
		_ = os.RemoveAll(filepath.Join(dir, e.Name()))
	}
}

// cleanupFailedImport is the no-half-restore rollback of a failed import
// write phase: the data directory goes back to empty (the held maintenance
// lock file excepted) and the out-of-bounds metadata target is removed —
// it was verified absent before the write phase began, so whatever sits
// there now is this run's partial database, never an operator file.
func cleanupFailedImport(dataDir, externalDB string, hadExternal bool) {
	clearDirContents(dataDir)
	if hadExternal {
		_ = os.Remove(externalDB) //nolint:errcheck // best-effort residue rollback on an already-failing path
	}
}

// pathOutside reports whether target is NOT inside base (base itself
// counts as inside). Both must be absolute, cleaned paths.
func pathOutside(base, target string) (bool, error) {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false, fmt.Errorf("relating %s to %s: %w", target, base, err)
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

// copyFile copies src to dst with a fixed mode (plain byte copy — the db
// artifact carries no meaningful mtime).
func copyFile(src, dst string, mode os.FileMode) (retErr error) {
	in, err := os.Open(src) //nolint:gosec // G304: src is the verified backup's snapshot file
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()                                                       //nolint:errcheck // read-only fd
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode) //nolint:gosec // G304: dst is the config-resolved database path
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer func() {
		if cerr := out.Close(); retErr == nil && cerr != nil {
			retErr = fmt.Errorf("close %s: %w", dst, cerr)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	return out.Sync()
}

// throughput renders a copy rate for the NFR-P19 record (logged, never
// gated).
func throughput(bytes int64, d time.Duration) string {
	if bytes <= 0 || d <= 0 {
		return "n/a"
	}
	bps := float64(bytes) / d.Seconds()
	return fmt.Sprintf("%.1f MiB/s", bps/(1024*1024))
}
