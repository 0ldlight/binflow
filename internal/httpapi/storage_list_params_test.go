// LOOP 009 L009-2: the ?list parameter family against the L008-3
// differential evidence (reports/compatibility/L008-list-params-diff.md,
// 32-arm matrix) — recursion semantics (deep/depth), the listFolders and
// includeRootPath listing-shape arms, integer validation, and the wire form
// (leading-slash uris, request-time created, vendor content type).
package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// l009Entry/l009Body decode one ?list response.
type l009Entry struct {
	URI          string `json:"uri"`
	Size         int64  `json:"size"`
	LastModified string `json:"lastModified"`
	Folder       bool   `json:"folder"`
	SHA1         string `json:"sha1"`
	SHA2         string `json:"sha2"`
}

type l009Body struct {
	URI     string      `json:"uri"`
	Created string      `json:"created"`
	Files   []l009Entry `json:"files"`
}

// seedL009Tree builds the differential's fixture tree under repo l009-loc:
// f0.txt / d1/{f1,f2}.txt / d1/d2/f3.txt / d1/d2/d3/f4.txt / d1/d2/d3/d4/f5.txt
// (ancestor folder rows materialize on upload, T-128).
func seedL009Tree(t *testing.T, h *harness) {
	t.Helper()
	seedRepo(t, h, "l009-loc")
	for _, p := range []string{
		"f0.txt",
		"d1/f1.txt", "d1/f2.txt",
		"d1/d2/f3.txt",
		"d1/d2/d3/f4.txt",
		"d1/d2/d3/d4/f5.txt",
	} {
		resp := h.do(http.MethodPut, "/binflow/l009-loc/"+p, adminUser, adminPass, []byte(p), nil)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("PUT %s: %d; body=%s", p, resp.StatusCode, mustGet(t, resp))
		}
		_ = resp.Body.Close()
	}
}

// getL009List fetches and decodes one ?list response.
func getL009List(t *testing.T, h *harness, pathAndQuery string) (*http.Response, l009Body) {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/storage/"+pathAndQuery, adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status = %d; body=%s", pathAndQuery, resp.StatusCode, body)
	}
	var lb l009Body
	if err := json.Unmarshal([]byte(body), &lb); err != nil {
		t.Fatalf("GET %s: body %q: %v", pathAndQuery, body, err)
	}
	return resp, lb
}

// urisOf projects the listing onto its entry uris, in wire order.
func urisOf(lb l009Body) []string {
	out := make([]string, 0, len(lb.Files))
	for _, f := range lb.Files {
		out = append(out, f.URI)
	}
	return out
}

func equalUris(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestStorageListIntegerValidation: every PRESENT value of the seven integer
// params must parse — non-numeric answers 400 with Java's parseInt wording
// (L008-3 §1 item 1; arms A09/A29/A32).
func TestStorageListIntegerValidation(t *testing.T) {
	h := newHarness(t)
	seedL009Tree(t, h)

	for _, name := range []string{
		"deep", "depth", "listFolders", "mdTimestamps", "statsTimestamps", "includeRootPath", "includePropertiesMd5",
	} {
		t.Run(name+"=abc is 400 with the parseInt wording", func(t *testing.T) {
			resp := h.do(http.MethodGet, "/binflow/api/storage/l009-loc/d1?list&"+name+"=abc", adminUser, adminPass, nil, nil)
			eb := decodeError(t, resp)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			if want := `For input string: "abc"`; eb.Errors[0].Message != want {
				t.Fatalf("message = %q, want %q", eb.Errors[0].Message, want)
			}
		})
	}

	t.Run("listFolders=true is 400 (A29)", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/storage/l009-loc/d1?list&listFolders=true", adminUser, adminPass, nil, nil)
		eb := decodeError(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		if want := `For input string: "true"`; eb.Errors[0].Message != want {
			t.Fatalf("message = %q, want %q", eb.Errors[0].Message, want)
		}
	})

	t.Run("int32 boundary overflow answers the same wording (Review A)", func(t *testing.T) {
		// Java's parseInt is int32-bounded: values beyond MaxInt32 /
		// below MinInt32 throw NumberFormatException with the value echoed.
		// Go's Atoi accepts up to int64 — the window must be refused
		// explicitly.
		for _, v := range []string{"2147483648", "-2147483649"} {
			resp := h.do(http.MethodGet, "/binflow/api/storage/l009-loc/d1?list&depth="+v, adminUser, adminPass, nil, nil)
			eb := decodeError(t, resp)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("depth=%s: status = %d, want 400", v, resp.StatusCode)
			}
			if want := `For input string: "` + v + `"`; eb.Errors[0].Message != want {
				t.Fatalf("depth=%s: message = %q, want %q", v, eb.Errors[0].Message, want)
			}
		}
		// The boundary values themselves stay in range and valid.
		for _, v := range []string{"2147483647", "-2147483648"} {
			resp, _ := getL009List(t, h, "l009-loc/d1?list&depth="+v)
			_ = resp.Body.Close()
		}
	})

	t.Run("numeric values of all seven params pass validation", func(t *testing.T) {
		q := "list&deep=0&depth=0&listFolders=0&mdTimestamps=0&statsTimestamps=0&includeRootPath=0&includePropertiesMd5=0"
		resp, _ := getL009List(t, h, "l009-loc/d1?"+q)
		_ = resp.Body.Close()
	})

	t.Run("empty and blank values are absent, not 400 (Review B)", func(t *testing.T) {
		// getQueryParameterAsInt short-circuits on isNotBlank BEFORE
		// parseInt (ArtifactResource.java:376-382): `?list&deep` and
		// `depth= ` never reach the parse and count as 0 — the listing
		// stays the plain direct-children one.
		for _, q := range []string{
			"list&deep", "list&deep=", "list&depth=", "list&listFolders=",
			"list&includeRootPath=%20", "list&depth=%20%20",
		} {
			_, lb := getL009List(t, h, "l009-loc/d1?"+q)
			if got := urisOf(lb); !equalUris(got, "/f1.txt", "/f2.txt") {
				t.Fatalf("%s: uris = %v, want the plain direct-children listing", q, got)
			}
		}
	})

	t.Run("param validation precedes target resolution", func(t *testing.T) {
		// A non-numeric param on a FILE target answers the parse 400, not
		// the file-target 400 — the reference parses params at the resource
		// layer before any node resolution, and the handler preserves that
		// order.
		resp := h.do(http.MethodGet, "/binflow/api/storage/l009-loc/d1/f1.txt?list&depth=abc", adminUser, adminPass, nil, nil)
		eb := decodeError(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		if want := `For input string: "abc"`; eb.Errors[0].Message != want {
			t.Fatalf("message = %q, want %q (parse error must win)", eb.Errors[0].Message, want)
		}
	})
}

// TestStorageListRecursionSemantics: deep=1 is the ONLY recursion trigger;
// depth merely clamps it; without listFolders no folder row appears at any
// depth (L008-3 §1 items 2-3; arms A02/A03/A05/A10/A11/A31).
func TestStorageListRecursionSemantics(t *testing.T) {
	h := newHarness(t)
	seedL009Tree(t, h)

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"deep=1 recurses with zero folder-row leak (A02)", "list&deep=1", []string{
			"/d2/d3/d4/f5.txt", "/d2/d3/f4.txt", "/d2/f3.txt", "/f1.txt", "/f2.txt",
		}},
		{"deep=0 stays flat (A03)", "list&deep=0", []string{"/f1.txt", "/f2.txt"}},
		{"deep=2 is a non-1 value: flat (A31)", "list&deep=2", []string{"/f1.txt", "/f2.txt"}},
		{"depth alone never recurses (A05)", "list&depth=2", []string{"/f1.txt", "/f2.txt"}},
		{"depth=99 alone never recurses (A10)", "list&depth=99", []string{"/f1.txt", "/f2.txt"}},
		{"depth=0 is an ignored value (A08)", "list&depth=0", []string{"/f1.txt", "/f2.txt"}},
		{"deep=1 depth=2 clamps to two levels (A11)", "list&deep=1&depth=2", []string{
			"/d2/f3.txt", "/f1.txt", "/f2.txt",
		}},
		{"deep=1 depth=1 clamps to direct children", "list&deep=1&depth=1", []string{
			"/f1.txt", "/f2.txt",
		}},
		{"deep=1 depth=4 clamps above the deepest level", "list&deep=1&depth=4", []string{
			"/d2/d3/d4/f5.txt", "/d2/d3/f4.txt", "/d2/f3.txt", "/f1.txt", "/f2.txt",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, lb := getL009List(t, h, "l009-loc/d1?"+tc.query)
			if got := urisOf(lb); !equalUris(got, tc.want...) {
				t.Fatalf("uris = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestStorageListFolderRows: listFolders=1 adds folder rows in the `/d2` form
// — size -1, folder true, no digests — mixed with file rows in the one
// alphabetical order; listFolders=0 keeps them out everywhere
// (L008-3 §1 item 4; arms A12/A13/A14).
func TestStorageListFolderRows(t *testing.T) {
	h := newHarness(t)
	seedL009Tree(t, h)

	t.Run("listFolders=1 lists the direct folder row (A12)", func(t *testing.T) {
		_, lb := getL009List(t, h, "l009-loc/d1?list&listFolders=1")
		if got := urisOf(lb); !equalUris(got, "/d2", "/f1.txt", "/f2.txt") {
			t.Fatalf("uris = %v, want [/d2 /f1.txt /f2.txt]", got)
		}
		for _, f := range lb.Files {
			if f.URI != "/d2" {
				continue
			}
			if !f.Folder || f.Size != -1 || f.SHA1 != "" || f.SHA2 != "" {
				t.Fatalf("folder row /d2 = %+v, want folder=true size=-1 no sha", f)
			}
		}
	})

	t.Run("listFolders=0 keeps folder rows out", func(t *testing.T) {
		_, lb := getL009List(t, h, "l009-loc/d1?list&listFolders=0")
		if got := urisOf(lb); !equalUris(got, "/f1.txt", "/f2.txt") {
			t.Fatalf("uris = %v, want [/f1.txt /f2.txt]", got)
		}
	})

	t.Run("deep=1 listFolders=1 mixes every folder row alphabetically (A14)", func(t *testing.T) {
		_, lb := getL009List(t, h, "l009-loc/d1?list&deep=1&listFolders=1")
		want := []string{
			"/d2", "/d2/d3", "/d2/d3/d4", "/d2/d3/d4/f5.txt", "/d2/d3/f4.txt",
			"/d2/f3.txt", "/f1.txt", "/f2.txt",
		}
		if got := urisOf(lb); !equalUris(got, want...) {
			t.Fatalf("uris = %v, want %v", got, want)
		}
	})

	t.Run("file rows keep their digests and sizes under listFolders", func(t *testing.T) {
		_, lb := getL009List(t, h, "l009-loc/d1?list&listFolders=1")
		for _, f := range lb.Files {
			if f.Folder {
				continue
			}
			if f.SHA2 == "" || f.Size != int64(len("d1/f1.txt")) {
				t.Fatalf("file row %s = %+v, want sha2 and real size", f.URI, f)
			}
		}
	})
}

// TestStorageListRootPathEntry: includeRootPath=1 leads files[] with the
// queried folder itself as uri "/" (L008-3 §1 item 8; arms A18/A19/A20).
func TestStorageListRootPathEntry(t *testing.T) {
	h := newHarness(t)
	seedL009Tree(t, h)

	t.Run("includeRootPath=1 leads with / (A18)", func(t *testing.T) {
		_, lb := getL009List(t, h, "l009-loc/d1?list&includeRootPath=1")
		if got := urisOf(lb); !equalUris(got, "/", "/f1.txt", "/f2.txt") {
			t.Fatalf("uris = %v, want [/ /f1.txt /f2.txt]", got)
		}
		if lb.Files[0].Size != -1 || !lb.Files[0].Folder {
			t.Fatalf("root entry = %+v, want folder=true size=-1", lb.Files[0])
		}
	})

	t.Run("listFolders+includeRootPath composes (A19)", func(t *testing.T) {
		_, lb := getL009List(t, h, "l009-loc/d1?list&listFolders=1&includeRootPath=1")
		if got := urisOf(lb); !equalUris(got, "/", "/d2", "/f1.txt", "/f2.txt") {
			t.Fatalf("uris = %v, want [/ /d2 /f1.txt /f2.txt]", got)
		}
	})

	t.Run("deep=1 includeRootPath=1 composes (A20)", func(t *testing.T) {
		_, lb := getL009List(t, h, "l009-loc/d1?list&deep=1&includeRootPath=1")
		want := []string{"/",
			"/d2/d3/d4/f5.txt", "/d2/d3/f4.txt", "/d2/f3.txt", "/f1.txt", "/f2.txt"}
		if got := urisOf(lb); !equalUris(got, want...) {
			t.Fatalf("uris = %v, want %v", got, want)
		}
	})
}

// TestStorageListRepoRootArms: the repository root lists — plain, and under
// the deep+listFolders+includeRootPath combination (arms A26/A27).
func TestStorageListRepoRootArms(t *testing.T) {
	h := newHarness(t)
	seedL009Tree(t, h)

	t.Run("plain root listing answers the direct child file (A26)", func(t *testing.T) {
		_, lb := getL009List(t, h, "l009-loc?list")
		if got := urisOf(lb); !equalUris(got, "/f0.txt") {
			t.Fatalf("uris = %v, want [/f0.txt]", got)
		}
	})

	t.Run("deep root combination composes (A27)", func(t *testing.T) {
		_, lb := getL009List(t, h, "l009-loc?list&deep=1&listFolders=1&includeRootPath=1")
		want := []string{"/",
			"/d1", "/d1/d2", "/d1/d2/d3", "/d1/d2/d3/d4", "/d1/d2/d3/d4/f5.txt",
			"/d1/d2/d3/f4.txt", "/d1/d2/f3.txt", "/d1/f1.txt", "/d1/f2.txt", "/f0.txt"}
		if got := urisOf(lb); !equalUris(got, want...) {
			t.Fatalf("uris = %v, want %v", got, want)
		}
	})

	t.Run("root uri is the bare repo key (no trailing slash)", func(t *testing.T) {
		_, lb := getL009List(t, h, "l009-loc?list")
		if !strings.HasSuffix(lb.URI, "/l009-loc") || strings.HasSuffix(lb.URI, "/l009-loc/") {
			t.Fatalf("uri = %q, want .../l009-loc without trailing slash", lb.URI)
		}
	})
}

// TestStorageListWireForm: the listing's wire form — vendor content type,
// no-trailing-slash top uri, request-time created (L008-3 §1 items 9-11,
// §4; the +74ms drift evidence).
func TestStorageListWireForm(t *testing.T) {
	h := newHarness(t)
	seedL009Tree(t, h)

	t.Run("content type is the FileList vendor media type", func(t *testing.T) {
		resp, _ := getL009List(t, h, "l009-loc/d1?list")
		defer func() { _ = resp.Body.Close() }()
		if got := resp.Header.Get("Content-Type"); got != "application/vnd.org.jfrog.artifactory.storage.FileList+json" {
			t.Fatalf("Content-Type = %q", got)
		}
	})

	t.Run("folder uri carries no trailing slash", func(t *testing.T) {
		_, lb := getL009List(t, h, "l009-loc/d1?list")
		if !strings.HasSuffix(lb.URI, "/d1") || strings.HasSuffix(lb.URI, "/d1/") {
			t.Fatalf("uri = %q, want .../d1 without trailing slash", lb.URI)
		}
	})

	t.Run("created is the request wall clock, not the folder's own timestamp", func(t *testing.T) {
		// The folder was created by the seed PUTs; the listing's created must
		// be strictly later (the +74ms drift evidence — created tracks the
		// request, not the folder).
		time.Sleep(20 * time.Millisecond)
		_, lb1 := getL009List(t, h, "l009-loc/d1?list")
		folderCreated := folderCreatedOf(t, h, "l009-loc/d1")
		c1, err := time.Parse(time.RFC3339, lb1.Created)
		if err != nil {
			t.Fatalf("created %q: %v", lb1.Created, err)
		}
		if !c1.After(folderCreated) {
			t.Fatalf("created %v not after folder creation %v", c1, folderCreated)
		}
		// Two consecutive requests carry their own clocks: stamps never move
		// backwards.
		time.Sleep(20 * time.Millisecond)
		_, lb2 := getL009List(t, h, "l009-loc/d1?list")
		c2, err := time.Parse(time.RFC3339, lb2.Created)
		if err != nil {
			t.Fatalf("created %q: %v", lb2.Created, err)
		}
		if c2.Before(c1) {
			t.Fatalf("created went backwards: %v then %v", c1, c2)
		}
	})
}

// folderCreatedOf reads a folder's FolderInfo created stamp for comparison.
func folderCreatedOf(t *testing.T, h *harness, path string) time.Time {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/storage/"+path, adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("FolderInfo %s: %d; body=%s", path, resp.StatusCode, body)
	}
	var fi struct {
		Created string `json:"created"`
	}
	if err := json.Unmarshal([]byte(body), &fi); err != nil {
		t.Fatalf("FolderInfo %s: body %q: %v", path, body, err)
	}
	created, err := time.Parse(time.RFC3339, fi.Created)
	if err != nil {
		t.Fatalf("FolderInfo created %q: %v", fi.Created, err)
	}
	return created
}
