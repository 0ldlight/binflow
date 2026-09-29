package maven

import (
	"mime"
	"path"
	"strings"
)

// extensionMimes is the deterministic extension→Content-Type table for
// maven deploys that declare no Content-Type — the same determinism rule
// as the generic adapter's table: the wire contract must be identical on
// every host, so the stdlib OS database is only a fallback for extensions
// outside the table. Values are aligned to Artifactory's shipped
// mimetypes.xml (factory table v17) per docs/reverse/mime-ownership.md
// section 2 — notably .pom → application/x-maven-pom+xml (not the plain
// xml spelling) and NO .sha512 entry (the factory table lacks it, so an
// undeclared .sha512 deploy falls to octet-stream; the sidecar GET face
// keeps its own x-checksum protocol constant, unaffected). BIN-52 / T-570.
var extensionMimes = map[string]string{
	".xml":        "application/xml",
	".pom":        "application/x-maven-pom+xml",
	".jar":        "application/java-archive",
	".war":        "application/java-archive",
	".ear":        "application/java-archive",
	".sar":        "application/java-archive",
	".har":        "application/java-archive",
	".hpi":        "application/java-archive",
	".jpi":        "application/java-archive",
	".json":       "application/json",
	".txt":        "text/plain",
	".properties": "text/plain",
	".log":        "text/plain",
	".tf":         "text/plain",
	".asc":        "text/plain",
	".sha1":       "application/x-checksum",
	".sha256":     "application/x-checksum",
	".md5":        "application/x-checksum",
	".gz":         "application/x-gzip",
	".tgz":        "application/x-gzip",
	".zip":        "application/zip",
	".tar":        "application/x-tar",
	".nuspec":     "application/x-nuspec+xml",
	".nupkg":      "application/x-nupkg",
	".deb":        "application/x-debian-package",
	".ddeb":       "application/x-debian-package",
	".rpm":        "application/x-rpm",
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
