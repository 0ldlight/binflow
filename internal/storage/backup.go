package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// BackupFormatVersion is the manifest format this build writes and accepts.
// A manifest with a higher formatVersion is refused (the reader may be
// missing fields it cannot default); a lower one is fine — fields have only
// ever been additive.
const BackupFormatVersion = 1

// Manifest is the export manifest (PRD FR-32 / ADR-0015 decision 3): the
// self-description a backup carries so import can verify it end to end.
//
// The manifest is the export's consistency boundary: its Blobs list is the
// live set of the DATABASE SNAPSHOT (nodes ∪ docker_refs), never "whatever
// was on disk at copy time" — blobs that landed during the export window are
// copied but not referenced, and become ordinary GC candidates after a
// restore. Manifest entries carry no secrets (NFR-S22): sha256, size and
// mtime only.
type Manifest struct {
	FormatVersion  int              `json:"formatVersion"`
	CreatedAt      string           `json:"createdAt"`      // RFC3339 UTC
	BinflowVersion string           `json:"binflowVersion"` // writing binary's version
	BlobCount      int              `json:"blobCount"`      // == len(Blobs), checked by Validate
	TotalBytes     int64            `json:"totalBytes"`     // == sum of Blobs sizes
	Metadata       ManifestMetadata `json:"metadata"`       // the snapshot database file
	GraceNote      string           `json:"graceNote"`      // operator-facing mtime warning, ManifestGraceNote
	Blobs          []ManifestBlob   `json:"blobs"`          // sorted by sha256; the reference set
}

// ManifestMetadata names the snapshot database file inside the backup and
// pins its sha256: import refuses to restore a database whose bytes drift
// from the manifest even by one bit.
type ManifestMetadata struct {
	File   string `json:"file"`   // bare file name inside the backup directory
	Sha256 string `json:"sha256"` // hex sha256 of the file's bytes
}

// ManifestBlob is one referenced blob. Size backs the import-side full size
// verification; MTime (RFC3339Nano of the source file) documents the grace
// clock the restore must reproduce (ADR-0006 erratum 2 / W32).
type ManifestBlob struct {
	Sha256 string `json:"sha256"`
	Size   int64  `json:"size"`
	MTime  string `json:"mtime"`
}

// ManifestGraceNote is carried verbatim in every manifest so the mtime rule
// travels with the artifact itself (NFR-S22-adjacent operator warning).
const ManifestGraceNote = "Blob file mtimes are preserved end to end (ADR-0006 erratum 2): " +
	"the GC grace clock does not reset after a restore. Blobs present in the export window but " +
	"not referenced here are restored as ordinary GC candidates. The backup contains password " +
	"hashes and encrypted remote credentials — store it as carefully as a secret."

// manifestName is the manifest file name inside a backup directory.
const manifestName = "manifest.json"

// ManifestPath returns the manifest location inside a backup directory.
func ManifestPath(backupDir string) string { return filepath.Join(backupDir, manifestName) }

// ErrManifestInvalid marks every structural manifest refusal (bad version,
// count/size arithmetic, malformed digests). The wrapped chain carries the
// concrete reason; callers fail fast on it.
var ErrManifestInvalid = errors.New("backup manifest is invalid")

// Validate checks the manifest's structural invariants: a format version
// this build understands, self-consistent count/total arithmetic, a bare
// (path-traversal-free) metadata file name and well-formed digests. It does
// NOT touch the filesystem — presence and byte-level checks are the
// importer's job, layered on top of this.
func (m *Manifest) Validate() error {
	if m == nil {
		return fmt.Errorf("manifest: %w: nil manifest", ErrManifestInvalid)
	}
	if m.FormatVersion <= 0 {
		return fmt.Errorf("manifest: %w: formatVersion %d is not a positive version", ErrManifestInvalid, m.FormatVersion)
	}
	if m.FormatVersion > BackupFormatVersion {
		return fmt.Errorf("manifest: %w: formatVersion %d is newer than this build understands (%d)", ErrManifestInvalid, m.FormatVersion, BackupFormatVersion)
	}
	if m.Metadata.File == "" {
		return fmt.Errorf("manifest: %w: metadata.file is empty", ErrManifestInvalid)
	}
	// A bare name only: "../x" or "a/b" would steer the restore outside the
	// backup directory.
	if filepath.Base(m.Metadata.File) != m.Metadata.File || m.Metadata.File == "." || m.Metadata.File == ".." {
		return fmt.Errorf("manifest: %w: metadata.file %q must be a bare file name", ErrManifestInvalid, m.Metadata.File)
	}
	if !validSha256(m.Metadata.Sha256) {
		return fmt.Errorf("manifest: %w: metadata.sha256 %q is not a sha256", ErrManifestInvalid, m.Metadata.Sha256)
	}
	if m.BlobCount != len(m.Blobs) {
		return fmt.Errorf("manifest: %w: blobCount %d does not match the %d blob entries", ErrManifestInvalid, m.BlobCount, len(m.Blobs))
	}
	var total int64
	seen := make(map[string]struct{}, len(m.Blobs))
	for i, b := range m.Blobs {
		if !validSha256(b.Sha256) {
			return fmt.Errorf("manifest: %w: blob %d has malformed sha256 %q", ErrManifestInvalid, i, b.Sha256)
		}
		if _, dup := seen[b.Sha256]; dup {
			return fmt.Errorf("manifest: %w: blob %s listed twice", ErrManifestInvalid, b.Sha256)
		}
		seen[b.Sha256] = struct{}{}
		if b.Size < 0 {
			return fmt.Errorf("manifest: %w: blob %s has negative size %d", ErrManifestInvalid, b.Sha256, b.Size)
		}
		total += b.Size
	}
	if total != m.TotalBytes {
		return fmt.Errorf("manifest: %w: totalBytes %d does not match the sum of the blob sizes (%d)", ErrManifestInvalid, m.TotalBytes, total)
	}
	return nil
}

// WriteManifest validates and writes m to path as indented JSON (0600: the
// manifest describes a secret-grade artifact, NFR-S22).
func WriteManifest(m *Manifest, path string) error {
	if err := m.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("storage: manifest: marshal: %w", err)
	}
	b = append(b, '\n')
	if err := writeFileSync(path, b, 0o600); err != nil {
		return fmt.Errorf("storage: manifest: write %s: %w", path, err)
	}
	return nil
}

// LoadManifest reads and validates the manifest at path. Unknown JSON fields
// are tolerated (forward compatibility, see BackupFormatVersion); every
// field this build relies on is checked by Validate.
func LoadManifest(path string) (*Manifest, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: operator-provided backup directory + constant file name — this read IS the feature
	if err != nil {
		return nil, fmt.Errorf("storage: manifest: read %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("storage: manifest: parse %s: %w", path, err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// BlobPath maps a sha256 to its content-addressed path under root:
// <root>/blobs/<sha256[0:2]>/<sha256>. The digest check is the same
// path-traversal guard the engine applies (blobPath); it is exported because
// the backup paths (export reference check, import verification) address
// blobs outside any engine instance.
func BlobPath(root, sha256 string) (string, error) {
	if !validSha256(sha256) {
		return "", fmt.Errorf("storage: invalid sha256 %q", sha256)
	}
	return filepath.Join(root, blobsDirName, sha256[:2], sha256), nil
}

// CopyBlobsTree copies the blob tree <srcRoot>/blobs/<xx>/<sha256> into
// dstRoot, preserving each file's permission bits and mtime — the ADR-0006
// erratum 2 hard constraint: a restore whose mtimes reset would zero every
// grace clock and make the whole history instantly collectable.
//
// Only well-formed blob paths are copied (two-hex shard directory, 64-hex
// file name matching its shard): foreign files in blobs/ are skipped, the
// same filter the GC sweep applies, so a backup never launders junk into a
// fresh instance. A missing source blobs/ directory is an empty store and
// copies nothing.
func CopyBlobsTree(srcRoot, dstRoot string) (files int, bytes int64, err error) {
	srcBlobs := filepath.Join(srcRoot, blobsDirName)
	shards, err := os.ReadDir(srcBlobs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, 0, nil // empty store: no blob tree yet
		}
		return 0, 0, fmt.Errorf("storage: copy blobs: scan %s: %w", srcBlobs, err)
	}
	dstBlobs := filepath.Join(dstRoot, blobsDirName)
	for _, shard := range shards {
		if !shard.IsDir() || !validShardName(shard.Name()) {
			continue
		}
		if err := os.MkdirAll(filepath.Join(dstBlobs, shard.Name()), 0o700); err != nil {
			return files, bytes, fmt.Errorf("storage: copy blobs: create shard %s: %w", shard.Name(), err)
		}
		entries, err := os.ReadDir(filepath.Join(srcBlobs, shard.Name()))
		if err != nil {
			return files, bytes, fmt.Errorf("storage: copy blobs: scan shard %s: %w", shard.Name(), err)
		}
		for _, ent := range entries {
			sum := ent.Name()
			if ent.IsDir() || !validSha256(sum) || sum[:2] != shard.Name() {
				continue
			}
			src := filepath.Join(srcBlobs, shard.Name(), sum)
			dst := filepath.Join(dstBlobs, shard.Name(), sum)
			n, err := copyFilePreserving(src, dst)
			if err != nil {
				return files, bytes, err
			}
			files++
			bytes += n
		}
	}
	return files, bytes, nil
}

// copyFilePreserving copies one regular file and restores its permission
// bits and mtime (atime is set to mtime — tar's default posture; only mtime
// is load-bearing, as the GC grace basis). The destination is fsynced so a
// crash mid-backup cannot leave a short file behind under a correct name.
func copyFilePreserving(src, dst string) (int64, error) {
	in, err := os.Open(src) //nolint:gosec // G304: path walked from the engine-validated blob tree (2-hex dir, 64-hex name)
	if err != nil {
		return 0, fmt.Errorf("storage: copy %s: %w", src, err)
	}
	defer in.Close() //nolint:errcheck // read-only fd
	info, err := in.Stat()
	if err != nil {
		return 0, fmt.Errorf("storage: copy %s: %w", src, err)
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("storage: copy %s: not a regular file", src)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm()) //nolint:gosec // G304: dst is the walked blob-tree mirror of the validated src name
	if err != nil {
		return 0, fmt.Errorf("storage: copy to %s: %w", dst, err)
	}
	n, err := io.Copy(out, in)
	if err != nil {
		_ = out.Close()
		return n, fmt.Errorf("storage: copy %s to %s: %w", src, dst, err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return n, fmt.Errorf("storage: copy %s to %s: fsync: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return n, fmt.Errorf("storage: copy %s to %s: %w", src, dst, err)
	}
	mtime := info.ModTime()
	if err := os.Chtimes(dst, mtime, mtime); err != nil {
		return n, fmt.Errorf("storage: copy %s to %s: restore mtime: %w", src, dst, err)
	}
	return n, nil
}

// HashFile streams path once and returns its sha256 (hex) and size — the
// manifest metadata hash and the import verification primitive.
func HashFile(path string) (sha string, size int64, err error) {
	f, err := os.Open(path) //nolint:gosec // G304: caller-resolved backup/engine path; hashing arbitrary files is the feature
	if err != nil {
		return "", 0, fmt.Errorf("storage: hash %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // read-only fd
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, fmt.Errorf("storage: hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// SortManifestBlobs orders the manifest's blob list by sha256 so "the first
// 100" of the spot verification is a stable, reproducible sample.
func SortManifestBlobs(m *Manifest) {
	sort.Slice(m.Blobs, func(i, j int) bool { return m.Blobs[i].Sha256 < m.Blobs[j].Sha256 })
}
