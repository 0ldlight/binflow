// Package mimetable is the single source of truth for the extension→
// Content-Type factory table every render/storage face consults (hoisted
// from four lockstep adapter/httpapi copies, R10 dual-review handoff /
// T-581). Dependency direction: this package imports nothing beyond the
// standard library — never an adapter or httpapi package — so any number
// of consumers may depend on it without cycles. Do not grow local copies
// of the table in consumers; the table is unexported on purpose, the only
// entry point is ByPath.
package mimetable

import (
	"path"
	"strings"
)

// extensionMimes is the extension→Content-Type factory table: a
// byte-for-byte transcription of Artifactory's shipped factory table
// (mimetypes.xml v17, docs/reverse/mime-ownership.md section 2). Per the
// BIN-53 / T-571 ownership ruling this table is the ONLY mime authority
// on the storage faces: PUT stores the table value for the deployed path
// (the request's declared Content-Type is ignored), and GET/FileInfo look
// the path up here again at render time. Extensions the table does not
// list answer application/octet-stream; there is no stdlib fallback —
// mime.TypeByExtension consults the OS mime database and answers
// differently per host, a drift family the reference does not have
// (every table-miss is octet-stream there).
//
// Factory quirks kept verbatim on purpose: bare text/* values without a
// charset parameter, .md/.yml/.yaml → text/plain, .gz → application/x-gzip,
// .swift → "text/x-swift " (trailing space — the factory spelling,
// live-confirmed on FileInfo, T-571), .xsl double-registered in the
// factory table under both text/xsl and application/xml (the text/xsl
// registration wins, live-confirmed T-571), and no .csv/.pdf/.svg/.sha512
// entries (all fall to octet-stream).
//
// The factory table also lists multi-segment spellings — tar.gz/tar.bz2 →
// the same value as their final segment, tar.xz/nar.xz → x-xz, and
// jar.pack.gz → application/x-java-pack200 — but the reference parses the
// LAST segment only (live-confirmed: a .jar.pack.gz deploy renders
// application/x-gzip, not pack200; T-571), so under that rule every
// compound spelling collapses onto its final segment's entry and none of
// them is a key here.
//
// Single-source discipline: any change to the table or the lookup rule
// lands here exactly once and every consumer (generic/maven/nuget
// adapters, httpapi FileInfo) inherits it the same instant. The content
// is pinned entry-for-entry by the snapshot test in this package — an
// edit that does not consciously update the golden fails CI.
var extensionMimes = map[string]string{
	".7z":         "application/x-7z-compressed",
	".apk":        "application/vnd.android.package-archive",
	".asc":        "text/plain",
	".box":        "application/x-vagrant-box",
	".bz2":        "application/x-bzip2",
	".c":          "text/x-c",
	".cc":         "text/x-c",
	".conda":      "application/x-conda",
	".cpp":        "text/x-c",
	".cs":         "text/x-csharp.sh",
	".css":        "text/css",
	".ddeb":       "application/x-debian-package",
	".deb":        "application/x-debian-package",
	".dtd":        "application/xml-dtd",
	".ear":        "application/java-archive",
	".ent":        "application/xml-external-parsed-entity",
	".fx":         "text/x-javafx-source",
	".gem":        "application/x-rubygems",
	".gradle":     "text/x-groovy-source",
	".groovy":     "text/x-groovy-source",
	".gz":         "application/x-gzip",
	".h":          "text/x-c",
	".har":        "application/java-archive",
	".hpi":        "application/java-archive",
	".htm":        "text/html",
	".html":       "text/html",
	".info":       "application/json+info",
	".ivy":        "application/x-ivy+xml",
	".jar":        "application/java-archive",
	".java":       "text/x-java-source",
	".jardiff":    "application/x-java-archive-diff",
	".jnlp":       "application/x-java-jnlp-file",
	".jpi":        "application/java-archive",
	".json":       "application/json",
	".log":        "text/plain",
	".md":         "text/plain",
	".md5":        "application/x-checksum",
	".mf":         "text/plain",
	".mod":        "text/plain+mod",
	".nupkg":      "application/x-nupkg",
	".nuspec":     "application/x-nuspec+xml",
	".pom":        "application/x-maven-pom+xml",
	".properties": "text/plain",
	".py":         "text/x-python",
	".rar":        "application/x-rar-compressed",
	".rb":         "text/x-ruby-source",
	".rpm":        "application/x-rpm",
	".rz":         "application/x-ruby-marshal",
	".sar":        "application/java-archive",
	".scala":      "text/x-scala-source",
	".sh":         "text/x-script.sh",
	".sha1":       "application/x-checksum",
	".sha256":     "application/x-checksum",
	".swift":      "text/x-swift ", // trailing space: factory spelling, verbatim
	".tar":        "application/x-tar",
	".tf":         "text/plain",
	".tgz":        "application/x-gzip",
	".txt":        "text/plain",
	".war":        "application/java-archive",
	".xhtml":      "application/xhtml+xml",
	".xml":        "application/xml",
	".xsi":        "application/xml",
	".xsl":        "text/xsl",
	".xslt":       "text/xslt",
	".xsd":        "application/xml-schema",
	".xz":         "application/x-xz",
	".yml":        "text/plain",
	".yaml":       "text/plain",
	".zip":        "application/zip",
}

// ByPath is the mime authority every face shares: the path's (lowercased)
// final extension against the factory table, application/octet-stream for
// every miss — unknown extensions, no extension, folder markers. The
// lookup is case-insensitive (factory keys compare lowercase) and the
// value never depends on the request or the stored row.
func ByPath(relPath string) string {
	ext := strings.ToLower(path.Ext(relPath))
	if m, ok := extensionMimes[ext]; ok {
		return m
	}
	return "application/octet-stream"
}
