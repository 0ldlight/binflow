package conan

import (
	"net/http"
	"strings"
	"testing"
)

// T-534: DELETE through a virtual repository, end to end on the conan v2
// face. The delete touches ONLY the virtual's own aggregation storage
// (virtual-resolution.md section 7.5's errata over repo-semantics
// section 8.2, T-524's ruling; T-530's D-2 removed the repo-layer 405
// refusal and with it this adapter's restated refusal): BinFlow persists
// no rows under the virtual key, so a member-held coordinate the virtual
// itself does not hold answers 404 (conan's errors[] envelope) and no
// member's entity is touched — the pre-T-524 405 wording no longer
// exists on this face (the reverse assertions pin its retirement; maven
// T-531's precedent).
func TestVirtualDeleteOwnStorage(t *testing.T) {
	f := newVirtualFixture(t)

	// The recipe DELETE and the revision DELETE, both through the virtual.
	for _, path := range []string{
		"hello/1.0/myuser/stable",
		"hello/1.0/myuser/stable/revisions/" + f.rrevA,
	} {
		code, body, hdr := f.delete(v2("vv", path))
		if code != http.StatusNotFound {
			t.Fatalf("virtual DELETE %s = (%d, %q), want 404 ITEM_NOT_FOUND", path, code, body)
		}
		if got := hdr.Get("Allow"); got != "" {
			t.Errorf("virtual DELETE %s Allow = %q, want none (no 405 refusal anymore)", path, got)
		}
		if !strings.Contains(body, `"status": 404`) || !strings.Contains(body, msgNotFound) {
			t.Errorf("virtual DELETE %s body = %s, want the 404 envelope", path, body)
		}
		if strings.Contains(body, "No local repository was configured") ||
			strings.Contains(body, "Deletes are not propagated") {
			t.Errorf("virtual DELETE %s body carries a retired 405 wording: %s", path, body)
		}
	}

	// The members survived both deletes: member A's index document and
	// member B's file are untouched (section 7.5 — the delete never even
	// looked at members).
	if doc := f.memberIndexDoc(t, "va"); len(doc.Revisions) == 0 {
		t.Error("member va's recipe index vanished after the virtual deletes")
	}
	if _, _, err := f.svc.Get(t.Context(), adminPrincipal(), "vb",
		recipeFile(f.rf.coordinateRoot(), f.rrevB, "local-b.py")); err != nil {
		t.Errorf("member vb's file did not survive the virtual deletes: %v", err)
	}
}
