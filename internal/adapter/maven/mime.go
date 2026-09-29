package maven

import (
	"path"
	"strings"
)

// extensionMimes is the extension→Content-Type table for the maven
// storage plane: a byte-for-byte transcription of Artifactory's shipped
// factory table (mimetypes.xml v17, docs/reverse/mime-ownership.md
// section 2) — the same table the generic adapter and internal/httpapi
// hold local copies of (the requestBase copy policy; keep the three in
// lockstep when the rule changes again). Convergence trigger (R9
// dual-review B NB): the next rule change that forces an edit here AND in
// both siblings is the signal to hoist the table into the shared adapter
// package — do not grow a fourth copy.
//
// Per the BIN-53 / T-571 ownership ruling this table is the ONLY mime
// authority on the storage faces: PUT stores the table value for the
// deployed path (the request's declared Content-Type is ignored), and
// GET/FileInfo look the path up here again at render time. Table misses
// answer application/octet-stream; there is no stdlib fallback (the OS
// mime database answers per host — a drift family the reference does not
// have).
//
// Factory quirks kept verbatim: .pom → application/x-maven-pom+xml (not
// the plain xml spelling), NO .sha512 entry (an undeclared .sha512 path
// falls to octet-stream; the sidecar GET face keeps its own x-checksum
// protocol constant, ruled separately in L032), .swift → "text/x-swift "
// (trailing space, factory spelling), .xsl → text/xsl (the factory
// table's earlier registration beats its application/xml one,
// live-confirmed T-571), and the compound spellings the factory table
// lists (tar.gz/tar.bz2/tar.xz/nar.xz/jar.pack.gz) collapse onto their
// final segment under the reference's last-segment parse — .jar.pack.gz
// deploys render x-gzip, not pack200 (live-confirmed T-571) — so none of
// them is a key here.
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

// mimeForPath is the storage-plane mime authority: the path's (lowercased)
// final extension against extensionMimes, application/octet-stream for
// every miss — unknown extensions, no extension, folder markers. The
// value never depends on the request headers or the stored row.
func mimeForPath(relPath string) string {
	ext := strings.ToLower(path.Ext(relPath))
	if m, ok := extensionMimes[ext]; ok {
		return m
	}
	return "application/octet-stream"
}
