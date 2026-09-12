package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// L011-1 (rest/metadata-plane-download-count-pollution): the /api/storage
// metadata faces resolve nodes through the non-counting List channel, so
// they leave downloadCount/lastDownloaded and the download audit trail
// untouched — the reference's metadata plane never counts (differential
// evidence: reference item-info and property reads/writes answer 0→0 on
// the member row, including through the virtual resolution surface, and an
// uncached remote item-info is a plain 404 with no fetch; the pre-fix
// BinFlow answered +1 per face through storageNode→ReposSvc.Get).

// statDownloadCount reads one node's downloadCount off the ?stats face.
func statDownloadCount(t *testing.T, h *harness, repo, path string) int64 {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/storage/"+repo+"/"+path+"?stats", adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("?stats %s/%s: status %d body=%s", repo, path, resp.StatusCode, body)
	}
	var got statsBody
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("?stats body %q: %v", body, err)
	}
	return got.DownloadCount
}

// TestMetadataFacesDoNotCountDownloads is the isolation probe as a test:
// every metadata face fires against a never-downloaded artifact and the
// counter must hold at zero throughout, then ONE real content GET moves it
// to 1 — the fix de-probed the faces, not the counter.
func TestMetadataFacesDoNotCountDownloads(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/d1/f1.bin", "f1")

	const item = "/binflow/api/storage/generic-local/d1/f1.bin"
	faces := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"item-info file GET", http.MethodGet, item, http.StatusOK},
		{"item-info trailing-slash file", http.MethodGet, item + "/", http.StatusNotFound},
		{"properties PUT", http.MethodPut, item + "?properties=pk=pv", http.StatusNoContent},
		{"properties GET", http.MethodGet, item + "?properties", http.StatusOK},
		{"properties DELETE", http.MethodDelete, item + "?properties=pk", http.StatusNoContent},
		{"permissions view", http.MethodGet, item + "?permissions", http.StatusOK},
		{"list over the parent folder", http.MethodGet, "/binflow/api/storage/generic-local/d1?list", http.StatusOK},
		{"list with the file as target answers 400", http.MethodGet, item + "?list", http.StatusBadRequest},
	}
	for _, f := range faces {
		t.Run(f.name, func(t *testing.T) {
			resp := h.do(f.method, f.path, adminUser, adminPass, nil, nil)
			_ = mustGet(t, resp)
			if resp.StatusCode != f.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, f.want)
			}
			if n := statDownloadCount(t, h, "generic-local", "d1/f1.bin"); n != 0 {
				t.Fatalf("downloadCount = %d after %s, want 0 (metadata faces never count)", n, f.name)
			}
		})
	}

	// The counter itself still works: one real download = one count, and a
	// second one keeps accruing.
	for want := int64(1); want <= 2; want++ {
		resp := h.do(http.MethodGet, "/binflow/generic-local/d1/f1.bin", adminUser, adminPass, nil, nil)
		_ = mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("content GET: status %d", resp.StatusCode)
		}
		if n := statDownloadCount(t, h, "generic-local", "d1/f1.bin"); n != want {
			t.Fatalf("downloadCount = %d after %d content GET(s), want %d", n, want, want)
		}
	}
}

// TestVirtualMetadataReadsDoNotCountOnMember pins the virtual arm of the
// fix: the virtual face resolves onto the member's row for rendering, and
// a metadata read through the virtual surface leaves the MEMBER's counter
// alone (the reference: 0→0; pre-fix BinFlow: +1 per face). A content GET
// through the virtual still counts on the member — the K69 via-virtual
// arm is untouched.
func TestVirtualMetadataReadsDoNotCountOnMember(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "member-local")
	if resp := putRepo(t, h, "agg-virt",
		`{"rclass":"virtual","packageType":"generic","repositories":["member-local"]}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("virtual create: status %d body=%s", resp.StatusCode, mustGet(t, resp))
	}
	putContent(t, h, "/binflow/member-local/via/f2.bin", "f2")

	const virtItem = "/binflow/api/storage/agg-virt/via/f2.bin"
	for _, face := range []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"virtual item-info", http.MethodGet, virtItem, http.StatusOK},
		{"virtual properties PUT", http.MethodPut, virtItem + "?properties=vk=vv", http.StatusNoContent},
		{"virtual properties GET", http.MethodGet, virtItem + "?properties", http.StatusOK},
	} {
		t.Run(face.name, func(t *testing.T) {
			resp := h.do(face.method, face.path, adminUser, adminPass, nil, nil)
			_ = mustGet(t, resp)
			if resp.StatusCode != face.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, face.want)
			}
			if n := statDownloadCount(t, h, "member-local", "via/f2.bin"); n != 0 {
				t.Fatalf("member downloadCount = %d after %s, want 0", n, face.name)
			}
		})
	}

	// The download arm survives: a content GET through the VIRTUAL counts
	// on the member's row (K69 arm 2), not on the virtual key.
	resp := h.do(http.MethodGet, "/binflow/agg-virt/via/f2.bin", adminUser, adminPass, nil, nil)
	_ = mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("virtual content GET: status %d", resp.StatusCode)
	}
	if n := statDownloadCount(t, h, "member-local", "via/f2.bin"); n != 1 {
		t.Fatalf("member downloadCount = %d after the via-virtual download, want 1", n)
	}
	statsResp := h.do(http.MethodGet, "/binflow/api/storage/agg-virt/via/f2.bin?stats", adminUser, adminPass, nil, nil)
	body := mustGet(t, statsResp)
	if statsResp.StatusCode != http.StatusOK {
		t.Fatalf("virtual ?stats: status %d body=%s", statsResp.StatusCode, body)
	}
	var via statsBody
	if err := json.Unmarshal([]byte(body), &via); err != nil {
		t.Fatalf("virtual ?stats body %q: %v", body, err)
	}
	if via.DownloadCount != 1 {
		t.Fatalf("virtual ?stats downloadCount = %d, want 1 (the member's row)", via.DownloadCount)
	}
}

// TestMetadataFaceResolutionArms pins the resolution semantics the
// List-based storageNode must keep from its Get-based predecessor: the
// folder spelling fallback, the frozen root postures, and the 404 wording
// family.
func TestMetadataFaceResolutionArms(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/d1/inner.bin", "x")

	tests := []struct {
		name    string
		method  string
		path    string
		want    int
		wantMsg string
	}{
		{"folder item-info without the slash still answers FolderInfo",
			http.MethodGet, "/binflow/api/storage/generic-local/d1", http.StatusOK, ""},
		{"folder item-info with the slash",
			http.MethodGet, "/binflow/api/storage/generic-local/d1/", http.StatusOK, ""},
		{"missing item keeps the 404 wording",
			http.MethodGet, "/binflow/api/storage/generic-local/nope.bin", http.StatusNotFound,
			"Unable to find item"},
		{"properties on the repository root keeps its 400",
			http.MethodGet, "/binflow/api/storage/generic-local?properties", http.StatusBadRequest,
			"invalid artifact path"},
		{"dot-segment path keeps its 400",
			http.MethodGet, "/binflow/api/storage/generic-local/a/../b.bin", http.StatusBadRequest,
			"dot segment"},
		{"N1: the degenerate slash-only spelling is the front 400 (statsNode posture)",
			http.MethodGet, "/binflow/api/storage/generic-local///", http.StatusBadRequest,
			"the repository root is not a node"},
		{"N1: empty middle segment keeps its 400",
			http.MethodGet, "/binflow/api/storage/generic-local/d1//inner.bin", http.StatusBadRequest,
			"empty path segment"},
		{"?stats on the repository root keeps its 404",
			http.MethodGet, "/binflow/api/storage/generic-local?stats", http.StatusNotFound,
			"Unable to find item"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.do(tc.method, tc.path, adminUser, adminPass, nil, nil)
			body := mustGet(t, resp)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d body=%s", resp.StatusCode, tc.want, body)
			}
			if tc.wantMsg != "" && !strings.Contains(body, tc.wantMsg) {
				t.Fatalf("body %q must contain %q", body, tc.wantMsg)
			}
		})
	}

	// FolderInfo children render (the fallback arm resolved the folder row,
	// not a phantom): exactly one child, "inner.bin", spelled as a file.
	resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/d1", adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	var folder struct {
		Children []struct {
			URI    string `json:"uri"`
			Folder bool   `json:"folder"`
		} `json:"children"`
	}
	if err := json.Unmarshal([]byte(body), &folder); err != nil {
		t.Fatalf("FolderInfo body %q: %v", body, err)
	}
	if len(folder.Children) != 1 || folder.Children[0].URI != "/inner.bin" || folder.Children[0].Folder {
		t.Fatalf("children = %+v, want exactly [/inner.bin file]", folder.Children)
	}
}

// TestMetadataFaceGovernanceRefusal pins review A B1: the local governance
// pattern gate rides the metadata faces exactly as it rode the old
// Get-based resolution (T-95/W12a dual-value ruling — a pattern-refused
// path is indistinguishable from a missing one). The leak fixture is the
// realistic one: the artifact landed under a permissive pattern, the
// pattern then tightened; the row still exists, but no metadata face may
// admit it exists (GET would leak size/checksums/timestamps/properties,
// PUT/DELETE would write a refused path).
func TestMetadataFaceGovernanceRefusal(t *testing.T) {
	h := newHarness(t)
	t95CreateRepo(t, h, "gov-local",
		`{"rclass":"local","packageType":"generic","includesPattern":"**/*"}`)
	putContent(t, h, "/binflow/gov-local/secret/leak.bin", "leaky")
	putContent(t, h, "/binflow/gov-local/keep.jar", "kept")

	// Tighten: everything under secret/ is now refused for reads, keep.jar
	// stays inside the includes.
	resp := postRepo(t, h, "gov-local",
		`{"rclass":"local","packageType":"generic","includesPattern":"**/*.jar","excludesPattern":"secret/**"}`)
	if body := mustGet(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("tighten pattern: status %d body=%s", resp.StatusCode, body)
	}

	const refused = "/binflow/api/storage/gov-local/secret/leak.bin"
	faces := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"item-info GET on the refused path is the missing-item 404",
			http.MethodGet, refused, http.StatusNotFound},
		{"properties GET on the refused path is the missing-item 404",
			http.MethodGet, refused + "?properties", http.StatusNotFound},
		{"properties PUT on the refused path is the missing-item 404",
			http.MethodPut, refused + "?properties=pk=pv", http.StatusNotFound},
		{"properties DELETE on the refused path is the missing-item 404",
			http.MethodDelete, refused + "?properties=pk", http.StatusNotFound},
		{"?permissions on the refused path is the missing-item 404",
			http.MethodGet, refused + "?permissions", http.StatusNotFound},
		{"allowed path still answers 200",
			http.MethodGet, "/binflow/api/storage/gov-local/keep.jar", http.StatusOK},
	}
	for _, f := range faces {
		t.Run(f.name, func(t *testing.T) {
			resp := h.do(f.method, f.path, adminUser, adminPass, nil, nil)
			body := mustGet(t, resp)
			if resp.StatusCode != f.want {
				t.Fatalf("status = %d, want %d body=%s", resp.StatusCode, f.want, body)
			}
			if f.want == http.StatusNotFound && !strings.Contains(body, "Unable to find item") {
				t.Fatalf("refusal body %q must be the missing-item 404 family", body)
			}
		})
	}
}

// TestMetadataFaceACLMatchStartArm pins review A B2: the resolution's read
// gate runs on the ADDRESSED spelling, so a bare-directory permission
// pattern (no wildcard) admits an explicit-slash subfolder address through
// the pathMatcher's directory-prefix rule — the matchStart arm the trimmed
// List resolution had lost. Demonstrator is the properties family: its
// handlers resolve and read props with no children listing. The slash-less
// spelling of the same address stays denied (matchStart requires the
// folder form — documented ACL semantics), and the denial message carries
// the spelling the client sent, slash included.
func TestMetadataFaceACLMatchStartArm(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"scoped", "scoped-pw"}})
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/acme/team/inner.bin", "x")
	// Read on the BARE directory name only: "acme" matches "acme/team/"
	// (folder-spelled, matchStart) but not the file-spelled "acme/team".
	grant(t, h, "bare-dir", "generic-local", "acme", "scoped", true, false, false)

	tests := []struct {
		name    string
		method  string
		path    string
		want    int
		wantMsg string
	}{
		{"properties GET on the explicit-slash subfolder passes the matchStart arm",
			http.MethodGet, "/binflow/api/storage/generic-local/acme/team/?properties", http.StatusOK, ""},
		{"slash-less spelling of the same address stays denied",
			http.MethodGet, "/binflow/api/storage/generic-local/acme/team?properties", http.StatusForbidden,
			"read generic-local/acme/team:"},
		{"denial on a folder-spelled address keeps the slash in the message",
			http.MethodGet, "/binflow/api/storage/generic-local/other/box/?properties", http.StatusForbidden,
			"read generic-local/other/box/:"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.do(tc.method, tc.path, "scoped", "scoped-pw", nil, nil)
			body := mustGet(t, resp)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d body=%s", resp.StatusCode, tc.want, body)
			}
			if tc.wantMsg != "" && !strings.Contains(body, tc.wantMsg) {
				t.Fatalf("body %q must contain %q", body, tc.wantMsg)
			}
		})
	}
}
