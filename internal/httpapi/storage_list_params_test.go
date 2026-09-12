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
	URI           string            `json:"uri"`
	Size          int64             `json:"size"`
	LastModified  string            `json:"lastModified"`
	Folder        bool              `json:"folder"`
	SHA1          string            `json:"sha1"`
	SHA2          string            `json:"sha2"`
	MDTimestamps  map[string]string `json:"mdTimestamps"`
	PropertiesMd5 string            `json:"propertiesMd5"`
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

	t.Run("ampersand filename survives the wire unescaped (L010-2 escape asymmetry)", func(t *testing.T) {
		resp := h.do(http.MethodPut, "/binflow/l009-loc/a&b.txt", adminUser, adminPass, []byte("amp"), nil)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("PUT a&b.txt: %d; body=%s", resp.StatusCode, mustGet(t, resp))
		}
		_ = resp.Body.Close()
		list := h.do(http.MethodGet, "/binflow/api/storage/l009-loc?list", adminUser, adminPass, nil, nil)
		raw := mustGet(t, list)
		if list.StatusCode != http.StatusOK {
			t.Fatalf("GET ?list: %d; body=%s", list.StatusCode, raw)
		}
		// The reference's Jackson serializer emits & raw; Go's default HTML
		// escaping would rewrite it to the & escape sequence — the exact
		// asymmetry retired by the SetEscapeHTML(false) encoder.
		if !strings.Contains(raw, `"/a&b.txt"`) {
			t.Fatalf("listing lacks the raw ampersand uri: %s", raw)
		}
		if strings.Contains(raw, "\\u0026") {
			t.Fatalf("listing carries an escaped ampersand: %s", raw)
		}
	})
}

// TestStorageNoSegmentListArm (L010-2): the no-repo-segment storage request
// (?list or bare, trailing slash or not) answers the reference's generic 404
// errors envelope "Not Found" — the 400 "Cannot list files of root." ticket
// premise did NOT reproduce on the live reference (four wire-probed
// variants, :8082 7.161.20, 2026-09-12).
func TestStorageNoSegmentListArm(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		name string
		path string
	}{
		{"bare storage with list", "/binflow/api/storage?list"},
		{"trailing slash with list", "/binflow/api/storage/?list"},
		{"bare storage without list", "/binflow/api/storage"},
	} {
		resp := h.do(http.MethodGet, tc.path, adminUser, adminPass, nil, nil)
		raw := mustGet(t, resp)
		var env struct {
			Errors []struct {
				Status  int    `json:"status"`
				Message string `json:"message"`
			} `json:"errors"`
		}
		if resp.StatusCode != http.StatusNotFound || json.Unmarshal([]byte(raw), &env) != nil ||
			len(env.Errors) != 1 || env.Errors[0].Status != 404 || env.Errors[0].Message != "Not Found" {
			t.Errorf("%s: got %d body=%s, want the generic 404 errors envelope \"Not Found\"", tc.name, resp.StatusCode, raw)
		}
	}
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

// ---- LOOP 010 L010-1: the P2 metadata parameters (mdTimestamps /
// statsTimestamps / includePropertiesMd5), field forms pinned to live
// reference evidence taken on :8082 (reports/compatibility/L010-list-p2-diff.md
// section 1). ----

// putProps writes properties through the FR-89.2 face (the audit row it
// records is mdTimestamps.properties' source).
func putProps(t *testing.T, h *harness, path, query string) {
	t.Helper()
	resp := h.do(http.MethodPut, "/binflow/api/storage/"+path+"?properties="+query,
		adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT props %s?properties=%s: %d; body=%s", path, query,
			resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()
}

// mdOf reads one entry's decoded mdTimestamps off a listing.
func entryOf(t *testing.T, lb l009Body, uri string) *l009Entry {
	t.Helper()
	for i := range lb.Files {
		if lb.Files[i].URI == uri {
			return &lb.Files[i]
		}
	}
	t.Fatalf("entry %s not in listing (have %v)", uri, urisOf(lb))
	return nil
}

// TestStorageListP2MDTimestampsProperties: mdTimestamps=1 adds
// mdTimestamps.properties on property-carrying entries (files AND folders,
// and the includeRootPath "/" row of a property-carrying folder);
// property-less entries omit the whole key.
func TestStorageListP2MDTimestampsProperties(t *testing.T) {
	h := newHarness(t)
	seedL009Tree(t, h)
	putProps(t, h, "l009-loc/f0.txt", "p1=v1")
	putProps(t, h, "l009-loc/d1", "fk=fv")

	_, lb := getL009List(t, h, "l009-loc?list&mdTimestamps=1")
	f0 := entryOf(t, lb, "/f0.txt")
	if f0.MDTimestamps["properties"] == "" {
		t.Errorf("/f0.txt (props p1=v1): mdTimestamps.properties missing: %+v", f0.MDTimestamps)
	}
	if ts := f0.MDTimestamps["properties"]; ts != "" && !strings.Contains(ts, "Z") {
		t.Errorf("/f0.txt mdTimestamps.properties %q: not an ISO8601 UTC stamp", ts)
	}
	// Property-less files omit the whole key (direct children of d1 carry
	// no properties in this fixture).
	_, lb = getL009List(t, h, "l009-loc/d1?list&mdTimestamps=1")
	if got := entryOf(t, lb, "/f1.txt").MDTimestamps; len(got) != 0 {
		t.Errorf("/f1.txt (no props): mdTimestamps present: %+v", got)
	}

	// Folder rows: /d1 (queried from the root with listFolders) carries the
	// key; the nested property-less /d2 (child of d1) does not.
	_, lb = getL009List(t, h, "l009-loc?list&listFolders=1&mdTimestamps=1")
	if ts := entryOf(t, lb, "/d1").MDTimestamps["properties"]; ts == "" {
		t.Errorf("/d1 (props fk=fv): mdTimestamps.properties missing")
	}
	_, lb = getL009List(t, h, "l009-loc/d1?list&listFolders=1&mdTimestamps=1")
	if got := entryOf(t, lb, "/d2").MDTimestamps; len(got) != 0 {
		t.Errorf("/d2 (no props): mdTimestamps present: %+v", got)
	}

	// The includeRootPath "/" row of the queried folder is enriched the same
	// way when the folder carries properties.
	_, lb = getL009List(t, h, "l009-loc/d1?list&includeRootPath=1&mdTimestamps=1")
	if ts := entryOf(t, lb, "/").MDTimestamps["properties"]; ts == "" {
		t.Errorf("/ row of property-carrying d1: mdTimestamps.properties missing")
	}
}

// TestStorageListP2StatsTimestamps: statsTimestamps=1 adds
// mdTimestamps.artifactory.stats on downloaded file entries only; combined
// with mdTimestamps=1 a property-carrying downloaded file carries both keys.
func TestStorageListP2StatsTimestamps(t *testing.T) {
	h := newHarness(t)
	seedL009Tree(t, h)
	putProps(t, h, "l009-loc/d1/f1.txt", "p1=v1")
	resp := h.do(http.MethodGet, "/binflow/l009-loc/d1/f1.txt", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download f1.txt: %d; body=%s", resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()

	_, lb := getL009List(t, h, "l009-loc/d1?list&statsTimestamps=1")
	f1 := entryOf(t, lb, "/f1.txt")
	if ts := f1.MDTimestamps["artifactory.stats"]; ts == "" {
		t.Errorf("/f1.txt (downloaded): mdTimestamps.artifactory.stats missing: %+v", f1.MDTimestamps)
	}
	if got := entryOf(t, lb, "/f2.txt").MDTimestamps; len(got) != 0 {
		t.Errorf("/f2.txt (never downloaded): mdTimestamps present: %+v", got)
	}

	_, lb = getL009List(t, h, "l009-loc/d1?list&mdTimestamps=1&statsTimestamps=1")
	f1 = entryOf(t, lb, "/f1.txt")
	if f1.MDTimestamps["properties"] == "" || f1.MDTimestamps["artifactory.stats"] == "" {
		t.Errorf("/f1.txt combined params: want both keys, got %+v", f1.MDTimestamps)
	}
	// Folder rows never carry the stats key.
	_, lb = getL009List(t, h, "l009-loc?list&listFolders=1&statsTimestamps=1")
	if got := entryOf(t, lb, "/d1").MDTimestamps["artifactory.stats"]; got != "" {
		t.Errorf("/d1 folder row: artifactory.stats present: %q", got)
	}
}

// TestStorageListP2PropertiesMd5 pins the canonical property-set digest to
// the digests the live reference answered for the SAME property sets
// (cross-system equality, L010-1 evidence section 1): {p1=[v1]} ->
// c03a74d41225e1c1f65df743e0e49da3, {p1=[v1,v2]} ->
// e215d43d0a83274a703cca40aa24ce25, {pa=[y],pb=[x]} (merged in the
// insertion order pb-then-pa) -> e2dc06da45abc3107844d9baa22accf0.
func TestStorageListP2PropertiesMd5(t *testing.T) {
	h := newHarness(t)
	seedL009Tree(t, h)
	putProps(t, h, "l009-loc/f0.txt", "p1=v1")
	putProps(t, h, "l009-loc/d1/f1.txt", "p1=v1,p1=v2")
	putProps(t, h, "l009-loc/d1/f2.txt", "pb=x")
	putProps(t, h, "l009-loc/d1/f2.txt", "pa=y")

	_, lb := getL009List(t, h, "l009-loc?list&deep=1&includePropertiesMd5=1")
	for uri, want := range map[string]string{
		"/f0.txt":    "c03a74d41225e1c1f65df743e0e49da3",
		"/d1/f1.txt": "e215d43d0a83274a703cca40aa24ce25",
		"/d1/f2.txt": "e2dc06da45abc3107844d9baa22accf0",
	} {
		if got := entryOf(t, lb, uri).PropertiesMd5; got != want {
			t.Errorf("%s propertiesMd5 = %q, want reference digest %q", uri, got, want)
		}
	}
	if got := entryOf(t, lb, "/d1/d2/f3.txt").PropertiesMd5; got != "" {
		t.Errorf("property-less /d1/d2/f3.txt carries propertiesMd5 %q", got)
	}

	// Wire order on a fully enriched entry: sha2, then mdTimestamps, then
	// propertiesMd5 (the reference's raw entry order).
	resp := h.do(http.MethodGet, "/binflow/api/storage/l009-loc?list&mdTimestamps=1&includePropertiesMd5=1",
		adminUser, adminPass, nil, nil)
	raw := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wire-order list: %d; body=%s", resp.StatusCode, raw)
	}
	i, j, k := strings.Index(raw, `"sha2"`), strings.Index(raw, `"mdTimestamps"`), strings.Index(raw, `"propertiesMd5"`)
	if i < 0 || j <= i || k <= j {
		t.Errorf("field order sha2(%d) < mdTimestamps(%d) < propertiesMd5(%d) broken in %s", i, j, k, raw)
	}
}

// TestStorageListRootEntryStampIsReal: the includeRootPath "/" row at the
// REPOSITORY ROOT carries the repo's real creation stamp, not the 1970
// epoch zero (reference evidence: a real root-folder mtime).
func TestStorageListRootEntryStampIsReal(t *testing.T) {
	h := newHarness(t)
	seedL009Tree(t, h)
	_, lb := getL009List(t, h, "l009-loc?list&includeRootPath=1")
	got := entryOf(t, lb, "/").LastModified
	if got == "" || strings.HasPrefix(got, "1970-01-01") {
		t.Errorf("repo-root \"/\" lastModified = %q, want a real (non-epoch-zero) stamp", got)
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z07:00", got); err != nil {
		t.Errorf("repo-root \"/\" lastModified %q not an ISO8601 millis stamp: %v", got, err)
	}
}
