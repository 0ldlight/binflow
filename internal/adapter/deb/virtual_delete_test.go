package deb

import (
	"net/http"
	"strings"
	"testing"
)

// T-534: DELETE through a virtual repository, end to end on the deb face.
// The delete touches ONLY the virtual's own aggregation storage
// (virtual-resolution.md section 7.5's errata over repo-semantics
// section 8.2, T-524's ruling; T-530's D-2 removed the repo-layer 405
// refusal and with it this adapter's restated refusal): BinFlow persists
// no aggregate-cache rows, so a member-held path the virtual itself does
// not hold answers 404 (deb's ITEM_NOT_FOUND wording) and no member's
// entity is touched — the pre-T-524 405 wording no longer exists on this
// face (the reverse assertions pin its retirement; maven T-531's
// precedent).
func TestVirtualDeleteOwnStorage(t *testing.T) {
	s := newStack(t)
	seedVirtualMembers(t, s, "deb-va", "deb-vb", false)
	s.seedVirtualRepo(t, "deb-virt", []string{"deb-va", "deb-vb"}, "deb-va")

	const rel = "pool/main/a/alpha/alpha_1.0_amd64.deb"
	status, body, hdr := s.delete("/binflow/deb-virt/" + rel)
	if status != http.StatusNotFound {
		t.Fatalf("virtual DELETE = %d, want 404 ITEM_NOT_FOUND (%s)", status, body)
	}
	if got := hdr.Get("Allow"); got != "" {
		t.Errorf("virtual DELETE Allow = %q, want none (no 405 refusal anymore)", got)
	}
	if !strings.Contains(body, "'deb-virt/"+rel+"' not found") {
		t.Errorf("virtual DELETE body = %s, want the ITEM_NOT_FOUND wording", body)
	}
	if strings.Contains(body, "No local repository was configured") ||
		strings.Contains(body, "Deletes are not propagated") {
		t.Errorf("virtual DELETE body carries a retired 405 wording: %s", body)
	}

	// The member's entity survived: the delete never even looked at
	// members — the member's own face and the virtual's resolution both
	// still serve the bytes (section 7.5).
	if status, _, _ = s.get("/binflow/deb-va/" + rel); status != http.StatusOK {
		t.Fatalf("member GET after the virtual delete = %d, want 200", status)
	}
	if status, _, _ = s.get("/binflow/deb-virt/" + rel); status != http.StatusOK {
		t.Fatalf("virtual GET after the virtual delete = %d, want 200 (member still serves)", status)
	}
}
