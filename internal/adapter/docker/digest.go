package docker

import (
	"crypto/md5"  //nolint:gosec // G401: md5 is a protocol-compatibility digest only (X-Checksum-Md5); addressing and integrity decisions are sha256-only (ADR-0003), same ruling as internal/storage/digest.go
	"crypto/sha1" //nolint:gosec // G401: sha1 is a protocol-compatibility digest only (X-Checksum-Sha1/ETag); addressing and integrity decisions are sha256-only (ADR-0003)
	"encoding/hex"
)

// sumSha1 and sumMd5 compute the ancillary protocol digests of the
// synthesized empty layer. They are used for exactly one fixed artifact at
// init time (never on client-supplied content), which keeps the ADR-0003
// sha256-only addressing rule untouched.

// sumSha1 yields the lowercase hex sha1 of b.
func sumSha1(b []byte) string {
	s := sha1.Sum(b) //nolint:gosec // G401: protocol-compatibility digest of the fixed empty-layer artifact, see file header
	return hex.EncodeToString(s[:])
}

// sumMd5 yields the lowercase hex md5 of b.
func sumMd5(b []byte) string {
	m := md5.Sum(b) //nolint:gosec // G401: protocol-compatibility digest of the fixed empty-layer artifact, see file header
	return hex.EncodeToString(m[:])
}

// mustHex decodes a fixed hex literal; it panics only on a programmer typo
// in a constant (caught the moment any test loads the package).
func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("docker: bad constant hex literal: " + err.Error())
	}
	return b
}
