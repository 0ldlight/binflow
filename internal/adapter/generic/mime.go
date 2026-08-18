package generic

import (
	"mime"
	"path"
	"strings"
)

// extensionMimes is BinFlow's deterministic extension→Content-Type table
// for generic deploys that declare no Content-Type (PRD milestone-2 section
// 6.4, "Content-Type 扩展名映射", ticket T-36). It takes precedence over the
// standard library's database so the wire contract is identical on every
// host — mime.TypeByExtension consults the OS mime database and would answer
// ".xml" differently on darwin vs a bare linux container.
//
// Values follow the governing RFCs where one exists (gzip: RFC 6713, zip:
// RFC 6713 errata/IANA, yaml: RFC 9512, markdown: RFC 7763) and
// Artifactory's shipped mimetypes.xml where docs/reverse captured it
// (config-formats.md section 3: sha1/sha256/md5 → application/x-checksum,
// high confidence). Artifactory's yaml spelling was not captured by the
// reverse pass, so the RFC 9512 value is BinFlow's own pick; it is a
// one-line change if review prefers a compatibility spelling.
var extensionMimes = map[string]string{
	".json":   "application/json",
	".xml":    "application/xml",
	".txt":    "text/plain; charset=utf-8",
	".csv":    "text/csv; charset=utf-8",
	".md":     "text/markdown; charset=utf-8",
	".html":   "text/html; charset=utf-8",
	".htm":    "text/html; charset=utf-8",
	".yml":    "application/yaml",
	".yaml":   "application/yaml",
	".gz":     "application/gzip",
	".tgz":    "application/gzip",
	".zip":    "application/zip",
	".tar":    "application/x-tar",
	".jar":    "application/java-archive",
	".war":    "application/java-archive",
	".sha1":   "application/x-checksum",
	".sha256": "application/x-checksum",
	".md5":    "application/x-checksum",
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
