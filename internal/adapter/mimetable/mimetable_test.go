package mimetable

// Single-source guard for the hoisted factory table (T-581): the golden
// below pins every entry of extensionMimes exactly (content verified as
// byte-identical across the four pre-hoist lockstep copies, R10 dual
// review; hash proof in reports/agents/T-581.md). Any edit to the table —
// or a re-added local copy drifting from it — surfaces here as a failure
// that cannot be silenced without consciously updating the golden. The
// rule legs (case-insensitive lookup, octet-stream floor, final-segment
// compound parse) re-pin the BIN-53 / T-571 ownership behavior at the
// resolver level.
import (
	"reflect"
	"testing"
)

// golden is the R10-verified v17 factory table, entry for entry.
var golden = map[string]string{
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

// TestTableSnapshot pins the table entry-for-entry: same length, no
// missing keys, no extra keys, no value drift. This is the single-source
// guard — the one place a table edit must consciously land.
func TestTableSnapshot(t *testing.T) {
	if len(extensionMimes) != len(golden) {
		t.Fatalf("table length = %d, golden = %d (an entry was added or removed; update the golden deliberately)", len(extensionMimes), len(golden))
	}
	if !reflect.DeepEqual(extensionMimes, golden) {
		for k, want := range golden {
			if got, ok := extensionMimes[k]; !ok {
				t.Errorf("table lost key %q", k)
			} else if got != want {
				t.Errorf("table[%q] = %q, want %q", k, got, want)
			}
		}
		for k := range extensionMimes {
			if _, ok := golden[k]; !ok {
				t.Errorf("table gained key %q not in golden", k)
			}
		}
		t.Fatal("table drift from golden (see per-entry errors above)")
	}
}

// TestByPathTableRule pins the resolver rule shared by every consumer:
// case-insensitive extension lookup, octet-stream floor for unknown
// extensions / no extension / table misses (no stdlib consultation,
// BIN-53 3a), and the factory's last-segment compound parse.
func TestByPathTableRule(t *testing.T) {
	cases := map[string]string{
		// table hits (incl. the trailing-space factory spelling)
		"a/b/c.nupkg": "application/x-nupkg",
		"a/b/c.pom":   "application/x-maven-pom+xml",
		"a/b/c.swift": "text/x-swift ", // header transport trims, FileInfo keeps
		"a/b/c.xsl":   "text/xsl",
		// case-insensitive lookup
		"a/b/DATA.JSON":     "application/json",
		"a/b/Bundle.TAR.GZ": "application/x-gzip",
		// compound spellings parse the FINAL segment (never pack200)
		"a/b/c.jar.pack.gz": "application/x-gzip",
		"a/b/c.tar.bz2":     "application/x-bzip2",
		"a/b/c.nar.xz":      "application/x-xz",
		// floor: unknown extension, no extension, table misses
		"a/b/c.zzz":      "application/octet-stream",
		"a/b/Dockerfile": "application/octet-stream",
		"a/b/c.csv":      "application/octet-stream",
		"a/b/c.pdf":      "application/octet-stream",
		"a/b/c.svg":      "application/octet-stream",
		"a/b/c.sha512":   "application/octet-stream",
	}
	for p, want := range cases {
		if got := ByPath(p); got != want {
			t.Errorf("ByPath(%q) = %q, want %q", p, got, want)
		}
	}
}
