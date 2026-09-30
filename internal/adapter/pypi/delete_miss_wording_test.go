package pypi

// BIN-94/T-612: the DELETE face on the PyPI wire. Live A (7.161.26,
// double-round) answers DELETE from the deletion engine's storage plane on
// every non-root spelling — bare content, packages/ and simple/ prefixes
// carried verbatim into the Item path — so the adapter forwards the
// verbatim rel path into the unified delete instead of shaping a
// protocol-specific 405. The repository root keeps its method gate (the
// root spelling is a whole-repository wipe corner, unruled), and the
// virtual face keeps its 405 (undecided; T-531's pin).

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

func TestDeleteMissWordAndRouting(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0-py3-none-any.whl", []byte("wheel-bytes"))
	s.seedRepo(t, "pyv-undecided", repo.TypeVirtual, repo.PackagePypi)

	tests := []struct {
		name      string
		path      string
		wantCode  int
		wantAllow string
		wantMsg   string // substring of the errors[] envelope
	}{
		{"bare miss: the deletion engine's family",
			"/binflow/api/pypi/pypi-local/never-pkg/1.0.0/never_pkg-1.0.0-py3-none-any.whl",
			http.StatusNotFound, "",
			"Artifact deletion error: Item pypi-local/never-pkg/1.0.0/never_pkg-1.0.0-py3-none-any.whl does not exist"},
		{"packages miss: the mount prefix rides the Item path verbatim",
			"/binflow/api/pypi/pypi-local/packages/never-pkg/1.0.0/never_pkg-1.0.0-py3-none-any.whl",
			http.StatusNotFound, "",
			"Artifact deletion error: Item pypi-local/packages/never-pkg/1.0.0/never_pkg-1.0.0-py3-none-any.whl does not exist"},
		{"simple miss: the index spelling rides the Item path verbatim, slash kept",
			"/binflow/api/pypi/pypi-local/simple/never-pkg/",
			http.StatusNotFound, "",
			"Artifact deletion error: Item pypi-local/simple/never-pkg/ does not exist"},
		{"bare content mount miss: same family on the second entrance",
			"/binflow/pypi-local/never-pkg/1.0.0/never_pkg-1.0.0-py3-none-any.whl",
			http.StatusNotFound, "",
			"Artifact deletion error: Item pypi-local/never-pkg/1.0.0/never_pkg-1.0.0-py3-none-any.whl does not exist"},
		{"repository root keeps the method gate",
			"/binflow/api/pypi/pypi-local/", http.StatusMethodNotAllowed, "GET, HEAD, POST", ""},
		{"virtual face keeps the undecided 405",
			"/binflow/api/pypi/pyv-undecided/packages/x/1.0.0/x-1.0.0-py3-none-any.whl",
			http.StatusMethodNotAllowed, "GET, HEAD", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := s.do(http.MethodDelete, tc.path, adminUser, adminPass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if resp.StatusCode != tc.wantCode {
				t.Fatalf("DELETE %s = %d (body %s), want %d", tc.path, resp.StatusCode, body, tc.wantCode)
			}
			if tc.wantAllow != "" {
				if got := resp.Header.Get("Allow"); got != tc.wantAllow {
					t.Fatalf("DELETE %s Allow = %q, want %q", tc.path, got, tc.wantAllow)
				}
			}
			if tc.wantMsg != "" && !strings.Contains(string(body), `"message": "`+tc.wantMsg+`"`) {
				t.Fatalf("DELETE %s body = %s, want the message %q", tc.path, body, tc.wantMsg)
			}
		})
	}

	// The existing delete really deletes: 204, the node is gone, and the
	// repeat delete misses with the same engine family (idempotent 404).
	path := "/binflow/api/pypi/pypi-local/demo-pkg/1.0.0/demo_pkg-1.0.0-py3-none-any.whl"
	resp := s.do(http.MethodDelete, path, adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("existing DELETE = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()
	if status, _, _ := s.get(path); status != http.StatusNotFound {
		t.Fatalf("GET after delete = %d, want 404", status)
	}
	resp = s.do(http.MethodDelete, path, adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNotFound ||
		!strings.Contains(string(body), "Artifact deletion error: Item pypi-local/demo-pkg/1.0.0/demo_pkg-1.0.0-py3-none-any.whl does not exist") {
		t.Fatalf("repeat DELETE = %d body %s, want the engine miss family", resp.StatusCode, body)
	}
}
