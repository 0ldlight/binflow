package audit_test

// NFR-S21 (PRD M4): the audit log is append-only — no API and no store
// code path may modify or delete an audit row. The API half is enforced by
// the router routing GET as the only /api/v1/audit verb (W39, asserted in
// internal/httpapi); this file pins the storage half by scanning the
// metadata package's PRODUCTION sources for any UPDATE/DELETE statement
// against audit_events. Test files are skipped: assertions quoting the
// banned shapes belong to tests, not to code paths.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetadataHasNoAuditMutationPath(t *testing.T) {
	dir := filepath.Join("..", "metadata")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read metadata sources: %v", err)
	}
	mutations := 0
	scanned := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		scanned++
		// Collapse whitespace so multi-line SQL literals are seen as one
		// statement; compare lowercase.
		src := strings.ToLower(strings.Join(strings.Fields(string(raw)), " "))
		for _, banned := range []string{"update audit_events", "delete from audit_events"} {
			if strings.Contains(src, banned) {
				t.Errorf("%s contains %q — the audit log must stay append-only (NFR-S21)", e.Name(), banned)
				mutations++
			}
		}
	}
	if scanned == 0 {
		t.Fatalf("no metadata production sources scanned — the scan would pass vacuously")
	}
	if mutations != 0 {
		t.Fatalf("%d audit mutation code path(s) found", mutations)
	}
}
