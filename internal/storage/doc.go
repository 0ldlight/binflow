// Package storage is the checksum-addressed blob engine: upload sessions
// with streaming sha256+sha1+md5 digests, atomic content-addressed commits
// with deduplication, streaming reads, startup sweeping of stale sessions
// and mark-sweep garbage collection (architecture sections 3.1 and 4,
// ADR-0006; implemented by T-9; sessions DB-backed by T-209).
//
// Disk layout (ADR-0006):
//
//	<data>/blobs/<sha256[0:2]>/<sha256>   immutable, globally deduplicated
//	<data>/uploads/<uuid>/data           in-progress upload bytes (temp files)
//
// Upload-session state lives in the metadata store's upload_sessions table
// (migration 010, T-209); the uploads/ directory holds only the transient
// data bytes, and the startup sweep deletes expired rows and their files.
//
// Commit protocol, order fixed by ADR-0006: write -> fsync(data) -> rename
// into the blob store (unless the target already exists) -> fsync(shard
// dir). A crash at any point leaves at worst a session directory (swept at
// startup) or a complete-but-unreferenced blob (collected by GC after the
// grace period); a half-written blob is never visible to readers.
//
// This package imports internal/metadata only for the UploadSessionStore
// seam (a one-way storage -> metadata dependency). The reverse direction does
// not exist: reference facts live in the metadata store and reach GC through
// the caller-supplied referenced-set callback (architecture section 2).
//
// Usage constraint: a data directory may be served by at most one Engine
// instance at a time. The startup sweep assumes the uploads/ directory
// belongs to this process — a second engine opening the same root would see
// the first engine's live sessions as abandoned residue (their in-memory
// liveness is invisible across processes) and could delete them mid-upload.
// The single-process deployment model (one binary per data dir, ADR-0004)
// satisfies this by construction; enforcing it cross-process needs a
// lockfile, which is deliberately out of scope for M1.
package storage
