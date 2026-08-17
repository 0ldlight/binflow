package storage

import (
	"crypto/md5"  //nolint:gosec // G401: Artifactory-compatibility digest, never a security primitive
	"crypto/sha1" //nolint:gosec // G401: same, ancillary protocol digest
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
)

// digestSums is the triple of ancillary-plus-primary digests every blob
// carries. Sha256 is the addressing key; sha1/md5 serve Maven/PyPI clients.
type digestSums struct {
	sha256 string
	sha1   string
	md5    string
}

// digesters computes all three digests in one streaming pass. Memory use is
// three fixed-size hash states, independent of content size.
type digesters struct {
	sha256 hash.Hash
	sha1   hash.Hash
	md5    hash.Hash
}

func newDigesters() *digesters {
	// sha256 is the only digest used for addressing and integrity decisions;
	// sha1/md5 exist solely because Maven/PyPI/Docker clients verify against
	// them (ADR-0003).
	return &digesters{
		sha256: sha256.New(),
		sha1:   sha1.New(), // compatibility digest only (G401 excluded globally, rationale in .golangci.yml)
		md5:    md5.New(),  // compatibility digest only (G401 excluded globally, rationale in .golangci.yml)
	}
}

// writer fans every byte out to the three hashes; io.Copy drives it with a
// 32 KB stack so RSS stays independent of blob size.
func (d *digesters) writer() io.Writer {
	return io.MultiWriter(d.sha256, d.sha1, d.md5)
}

func (d *digesters) sums() digestSums {
	return digestSums{
		sha256: hex.EncodeToString(d.sha256.Sum(nil)),
		sha1:   hex.EncodeToString(d.sha1.Sum(nil)),
		md5:    hex.EncodeToString(d.md5.Sum(nil)),
	}
}
