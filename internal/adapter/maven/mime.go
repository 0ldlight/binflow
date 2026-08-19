package maven

import (
	"mime"
	"path"
	"strings"
)

// extensionMimes is the deterministic extension→Content-Type table for
// maven deploys that declare no Content-Type — the same determinism rule
// as the generic adapter's table (T-36): the wire contract must be
// identical on every host, so the stdlib OS database is only a fallback
// for extensions outside the table. Values: RFCs where one exists,
// Artifactory's captured spellings otherwise (config-formats.md section 3:
// checksum suffixes → application/x-checksum, high confidence).
var extensionMimes = map[string]string{
	".xml":    "application/xml",
	".pom":    "application/xml",
	".jar":    "application/java-archive",
	".war":    "application/java-archive",
	".ear":    "application/java-archive",
	".json":   "application/json",
	".txt":    "text/plain; charset=utf-8",
	".sha1":   "application/x-checksum",
	".sha256": "application/x-checksum",
	".sha512": "application/x-checksum",
	".md5":    "application/x-checksum",
	".gz":     "application/gzip",
	".tgz":    "application/gzip",
	".zip":    "application/zip",
	".tar":    "application/x-tar",
}

// mimeForPath resolves the Content-Type of a stored node: the stored value
// wins (it is the client's declaration or this table applied at upload),
// extension inference is the fallback for hand-migrated rows, and
// application/octet-stream is the floor.
func mimeForPath(relPath, stored string) string {
	if strings.TrimSpace(stored) != "" {
		return stored
	}
	ext := strings.ToLower(path.Ext(relPath))
	if m, ok := extensionMimes[ext]; ok {
		return m
	}
	if m := mime.TypeByExtension(ext); m != "" {
		return m
	}
	return "application/octet-stream"
}
