// Package storage is the checksum-addressed blob engine: upload sessions
// with streaming sha256+sha1+md5 digests, atomic content-addressed commits
// with deduplication, streaming reads, startup sweeping of stale sessions
// and mark-sweep garbage collection (architecture sections 3.1 and 4,
// ADR-0006; implemented by T-9).
//
// Disk layout (a compatibility contract, ADR-0006):
//
//	<data>/blobs/<sha256[0:2]>/<sha256>   immutable, globally deduplicated
//	<data>/sessions/<uuid>/{data,state.json}
//
// Commit protocol, order fixed by ADR-0006: write -> fsync(data) -> rename
// into the blob store (unless the target already exists) -> fsync(shard
// dir). A crash at any point leaves at worst a session directory (swept at
// startup) or a complete-but-unreferenced blob (collected by GC after the
// grace period); a half-written blob is never visible to readers.
//
// This package never imports internal/metadata: reference facts live in the
// metadata store and reach GC through the caller-supplied referenced-set
// callback (architecture section 2, one-way dependency).
package storage
