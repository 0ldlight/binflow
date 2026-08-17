package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/lzwzzy/binflow/internal/config"
)

// TestStorageItemInfoAnonymousModes: the /api/storage read pair follows the
// content plane's anonymous switch (PRD E-09 note): with
// anonymous_access=false the item-info GET answers 401 for anonymous and 200
// for an authenticated reader, while ?list keeps its own stricter rule
// (authenticated only, 403) regardless of the switch.
func TestStorageItemInfoAnonymousModes(t *testing.T) {
	h := newHarnessCfg(t, func(c *config.Config) { c.Security.AnonymousAccess = false }, nil)
	seedRepo(t, h, "generic-local")

	if resp := h.do(http.MethodPut, "/binflow/generic-local/acme/", adminUser, adminPass, nil, nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("mkdir: %d", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}
	if resp := h.do(http.MethodPut, "/binflow/generic-local/acme/x.bin", adminUser, adminPass, []byte("x"), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed: %d", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}

	tests := []struct {
		name string
		path string
		user string
		want int
	}{
		{"anonymous item info is 401 when the switch is off", "/binflow/api/storage/generic-local/acme/x.bin", "", http.StatusUnauthorized},
		{"admin item info is 200", "/binflow/api/storage/generic-local/acme/x.bin", adminUser, http.StatusOK},
		{"anonymous list stays 403", "/binflow/api/storage/generic-local/acme?list", "", http.StatusForbidden},
		{"admin list is 200", "/binflow/api/storage/generic-local/acme?list", adminUser, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pass := adminPass
			resp := h.do(http.MethodGet, tc.path, tc.user, pass, nil, nil)
			_ = mustGet(t, resp)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}
