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
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
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
	lock, err := storage.AcquireDataLock(cfg.Storage.DataDir, "export")
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
	files, copied, err := storage.CopyBlobsTree(cfg.Storage.DataDir, out)
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

	in, err := filepath.Abs(*input)
	if err != nil {
		return fmt.Errorf("import: resolving --input %s: %w", *input, err)
	}

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

	restored := false
	defer func() {
		if !restored {
			clearDirContents(cfg.Storage.DataDir)
			writeCLIReport(stderr, "import: target %s cleared back to empty (no half-restore)\n", cfg.Storage.DataDir)
		}
	}()

	started := time.Now()
	dbDst := sqlitePath(cfg)
	if dir := filepath.Dir(dbDst); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("import: creating %s: %w", dir, err)
		}
	}
	if err := copyFile(metaSrc, dbDst, 0o600); err != nil {
		return fmt.Errorf("import: %w", err)
	}
	if _, _, err := storage.CopyBlobsTree(in, cfg.Storage.DataDir); err != nil {
		return fmt.Errorf("import: %w", err)
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
		DSN:           dbDst,
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
		if e.Name() == ".maintenance.lock" {
			continue
		}
		return fmt.Errorf("data dir %s is not empty (found %q) — import restores into an empty directory only; move the existing data away first", dataDir, e.Name())
	}
	return nil
}

// clearDirContents removes every entry under dir (the no-half-restore
// guarantee for a failed import write phase), leaving the directory itself
// in place.
func clearDirContents(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // nothing created or already gone
	}
	for _, e := range entries {
		_ = os.RemoveAll(filepath.Join(dir, e.Name()))
	}
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
