package httpapi_test

// T-530 (D-2): the virtual DELETE wire face (virtual-resolution.md section
// 7.5's errata + the L028 live-measured reference: 404 ITEM_NOT_FOUND,
// member artifacts alive). The service branch is deleteVirtualOwnStorage;
// this file pins the adapter plane's rendering and the full chain.

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestVirtualDeleteWire404Chain: DELETE through a virtual answers the
// adapter's not-found wording with the VIRTUAL path in it, the member
// artifact survives, the member's own delete still works, and the virtual
// re-resolve then misses.
func TestVirtualDeleteWire404Chain(t *testing.T) {
	h := newHarness(t)
	if status, body := putRepoStatus(t, h, "member-local",
		`{"rclass":"local","packageType":"generic"}`); status != http.StatusOK {
		t.Fatalf("create local: %d %s", status, body)
	}
	if status, body := putRepoStatus(t, h, "virt",
		`{"rclass":"virtual","packageType":"generic","repositories":["member-local"]}`); status != http.StatusOK {
		t.Fatalf("create virtual: %d %s", status, body)
	}

	// Deploy into the member directly.
	resp := h.do(http.MethodPut, "/binflow/member-local/a/b/app.bin", adminUser, adminPass,
		[]byte("payload"), nil)
	if resp.StatusCode >= 300 {
		t.Fatalf("member deploy = %d", resp.StatusCode)
	}
	drain(resp)

	// DELETE through the virtual: the own-storage 404, ITEM_NOT_FOUND
	// family wording, the VIRTUAL spelling in the path.
	resp = h.do(http.MethodDelete, "/binflow/virt/a/b/app.bin", adminUser, adminPass, nil, nil)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("virtual DELETE = %d %s, want 404", resp.StatusCode, string(body))
	}
	if !strings.Contains(string(body), "Could not locate artifact. Path: 'virt/a/b/app.bin'.") {
		t.Fatalf("virtual DELETE body = %s, want the ITEM_NOT_FOUND wording", string(body))
	}

	// The member artifact survives.
	resp = h.do(http.MethodGet, "/binflow/member-local/a/b/app.bin", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member GET after virtual delete = %d, want 200 (members untouched)", resp.StatusCode)
	}
	drain(resp)

	// The member's own delete keeps the 204 semantics...
	resp = h.do(http.MethodDelete, "/binflow/member-local/a/b/app.bin", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("member DELETE = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()
	// ...and the virtual re-resolve now misses.
	resp = h.do(http.MethodGet, "/binflow/virt/a/b/app.bin", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("virtual GET after member delete = %d, want 404", resp.StatusCode)
	}
	drain(resp)
}
