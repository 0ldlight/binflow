package generic

import (
	"mime"
	"path"
	"strings"
)

// extensionMimes is the deterministic extension→Content-Type table for
// generic deploys that declare no Content-Type. It takes precedence over the
// standard library's database so the wire contract is identical on every
// host — mime.TypeByExtension consults the OS mime database for extensions
// outside Go's builtin table and can answer those differently on darwin vs a
// bare linux container.
//
// Values are aligned to Artifactory's shipped mimetypes.xml (factory table
// v17) per docs/reverse/mime-ownership.md section 2 — including the
// deliberate deviations from RFC spellings: bare text/* values without a
// charset parameter, .md/.yaml → text/plain, .gz → application/x-gzip, and
// no .csv entry at all (falls through to octet-stream). BIN-52 / T-570.
// .info/.mod are NOT in the table: the goproxy protocol face owns their
// spellings (adjacent divergence, ruled separately).
var extensionMimes = map[string]string{
	".json":       "application/json",
	".xml":        "application/xml",
	".txt":        "text/plain",
	".md":         "text/plain",
	".properties": "text/plain",
	".log":        "text/plain",
	".tf":         "text/plain",
	".asc":        "text/plain",
	".html":       "text/html",
	".htm":        "text/html",
	".yaml":       "text/plain",
	".yml":        "text/plain",
	".gz":         "application/x-gzip",
	".tgz":        "application/x-gzip",
	".zip":        "application/zip",
	".tar":        "application/x-tar",
	".jar":        "application/java-archive",
	".war":        "application/java-archive",
	".ear":        "application/java-archive",
	".sar":        "application/java-archive",
	".har":        "application/java-archive",
	".hpi":        "application/java-archive",
	".jpi":        "application/java-archive",
	".pom":        "application/x-maven-pom+xml",
	".nuspec":     "application/x-nuspec+xml",
	".nupkg":      "application/x-nupkg",
	".deb":        "application/x-debian-package",
	".ddeb":       "application/x-debian-package",
	".rpm":        "application/x-rpm",
	".sha1":       "application/x-checksum",
	".sha256":     "application/x-checksum",
	".md5":        "application/x-checksum",
}

// mimeByExtension maps a dot-prefixed extension (case-insensitive) to a
// Content-Type: BinFlow's table first, then the standard library (builtin
// table + OS database, covering png/pdf/svg/...), "" when unknown.
func mimeByExtension(ext string) string {
	ext = strings.ToLower(ext)
	if m, ok := extensionMimes[ext]; ok {
		return m
	}
	return mime.TypeByExtension(ext)
}

// mimeByPath infers the mime of a deploy from its path: known extensions
// map per extensionMimes/the stdlib database, everything else — no
// extension, unknown extension, folder markers — stays
// application/octet-stream (FR-4-AC13 unchanged).
func mimeByPath(relPath string) string {
	if m := mimeByExtension(path.Ext(relPath)); m != "" {
		return m
	}
	return "application/octet-stream"
}

// mimeForNode resolves the Content-Type a stored node renders with. The
// stored value wins: it is either the client's declared Content-Type or the
// extension inference made at upload time, and keeping it authoritative is
// what makes the download header, the upload FileInfo body and (via the
// same stored column) /api/storage agree without a second mapping. Only a
// theoretically empty stored value — not producible through this adapter,
// belt-and-braces for hand-migrated rows — falls back to extension
// inference, then octet-stream.
func mimeForNode(relPath, stored string) string {
	if strings.TrimSpace(stored) != "" {
		return stored
	}
	return mimeByPath(relPath)
}
