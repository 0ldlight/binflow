// The package boundary as a test (M17 T-507, ADR-0045 decision 1): the
// import graph IS the audit evidence — the weaving faces (webhook Emit
// facet, search BuildSearcher) must arrive through injected seams at the
// assembly layer, never through a direct import. This test parses the
// package's own production sources and asserts the sanctioned-dependency
// whitelist; a banned import fails the build here, at the desk, instead
// of in review.

package build_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// sanctionedImports is ADR-0045 decision 1's positive list: the ONLY
// internal packages this domain may depend on.
var sanctionedImports = map[string]string{
	"github.com/lzwzzy/binflow/internal/metadata": "the BuildStore sub-store seam",
	"github.com/lzwzzy/binflow/internal/auth":     "Authorizer + Principal",
	"github.com/lzwzzy/binflow/internal/repo":     "CopyOrMove carrier (T-509)",
	"github.com/lzwzzy/binflow/internal/audit":    "audit facet",
	"github.com/lzwzzy/binflow/internal/config":   "read-only config",
}

// bannedImports is the ban list verbatim (the whitelist already excludes
// them; the pair exists so a failure names the actual rule broken).
var bannedImports = []string{
	"github.com/lzwzzy/binflow/internal/webhook",
	"github.com/lzwzzy/binflow/internal/search",
	"github.com/lzwzzy/binflow/internal/httpapi",
	"github.com/lzwzzy/binflow/internal/scheduler",
	"github.com/lzwzzy/binflow/internal/bundle",
	"github.com/lzwzzy/binflow/internal/insights",
	"github.com/lzwzzy/binflow/internal/license",
	"github.com/lzwzzy/binflow/internal/replication",
}

// TestPackageImportBoundary walks every production file of this package
// and asserts each internal import is on the sanctioned list (and none is
// on the ban list).
func TestPackageImportBoundary(t *testing.T) {
	dir := "." // go test runs in the package's source directory
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	var internal []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(path, "github.com/lzwzzy/binflow/internal/") {
				internal = append(internal, path)
			}
		}
	}
	if len(internal) == 0 {
		t.Fatal("no internal imports found — the test is not looking at the package sources")
	}
	sort.Strings(internal)

	for _, path := range internal {
		if reason, ok := sanctionedImports[path]; !ok {
			t.Errorf("unsanctioned internal import %q (ADR-0045 decision 1 whitelist: metadata/auth/repo/audit/config only)", path)
		} else {
			t.Logf("sanctioned: %s (%s)", path, reason)
		}
		for _, banned := range bannedImports {
			if path == banned {
				t.Errorf("BANNED import %q — weaving goes through injected facets at the assembly layer, never a direct import", banned)
			}
		}
	}
}
