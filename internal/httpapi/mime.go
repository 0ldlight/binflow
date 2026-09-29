package httpapi

import (
	"path"
	"strings"
)

// extensionMimes is the extension→Content-Type table the /api/storage
// FileInfo mimeType renders by: a byte-for-byte transcription of
// Artifactory's shipped factory table (mimetypes.xml v17,
// docs/reverse/mime-ownership.md section 2) — the same table the generic
// and maven adapters hold local copies of (the requestBase copy policy;
// keep the three in lockstep when the rule changes again).
//
// Per the BIN-53 / T-571 ownership ruling the table is the ONLY mime
// authority on the FileInfo face: the value renders from the node's path
// at request time, the stored mime column takes no part, and table misses
// answer application/octet-stream (no stdlib fallback — the OS mime
// database answers per host, a drift family the reference does not have).
//
// Factory quirks kept verbatim: .swift → "text/x-swift " (trailing space,
// the factory spelling, live-confirmed on FileInfo, T-571), .xsl →
// text/xsl (the factory table's earlier registration beats its
// application/xml one, live-confirmed T-571), and the compound spellings
// the factory table lists (tar.gz/tar.bz2/tar.xz/nar.xz/jar.pack.gz)
// collapse onto their final segment under the reference's last-segment
// parse — .jar.pack.gz renders x-gzip, not pack200 (live-confirmed
// T-571) — so none of them is a key here.
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

// ociMediaTreePrefixes names the OCI content-negotiation vendor trees the
// docker and helmoci adapters store verbatim on manifest nodes: that
// mediaType is protocol payload (T-32 R3's pass-through ruling), excluded
// from the path-table ownership flip (BIN-53), so it keeps rendering from
// the stored column. The extension table never produces these spellings,
// so the prefix match cannot shadow a table value.
var ociMediaTreePrefixes = [...]string{
	"application/vnd.docker.",
	"application/vnd.oci.",
	"application/vnd.cncf.",
}

// mimeByPath is the table lookup: the path's (lowercased) final extension
// against extensionMimes, application/octet-stream for every miss.
func mimeByPath(relPath string) string {
	ext := strings.ToLower(path.Ext(relPath))
	if m, ok := extensionMimes[ext]; ok {
		return m
	}
	return "application/octet-stream"
}

// mimeForNode renders one node's FileInfo mimeType: the OCI mediaType
// carve-out first (see ociMediaTreePrefixes), the extension table for the
// path everywhere else — including rows whose stored column disagrees
// with the table (old-model declared values, other adapters' storage
// constants): those re-render per the table exactly like fresh deploys
// (BIN-53 / T-571 render-time ownership).
func mimeForNode(relPath, stored string) string {
	for _, p := range ociMediaTreePrefixes {
		if strings.HasPrefix(stored, p) {
			return stored
		}
	}
	return mimeByPath(relPath)
}
