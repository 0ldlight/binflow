// T-444 acceptance surface (FR-146.1, ADR-0044 K68 / architecture section
// 25.6): the permission-target wire's renewed action vocabulary — the
// five-word canonical echo, the write→deploy-cache receive-only alias —
// and the annotate bit's endpoint gates: the ?properties family's two
// mutating verbs (the 403 probes of a grant without the bit), the content
// plane's unchanged r/w/d/m faces, and the ?permissions view's a letter.

package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// putTargetWire writes one permission target through the real wire and
// returns the status (the verb under test is the body's action words).
func putTargetWire(t *testing.T, h *harness, name, principals string, want int) {
	t.Helper()
	body := `{"name":"` + name + `","repos":["generic-local"],"includePatterns":["sec/**"],"principals":` + principals + `}`
	resp := t215As(t, h, http.MethodPost, "api/v1/permissions", adminUser, adminPass, body)
	raw := readAllT444(t, resp)
	if resp.StatusCode != want {
		t.Fatalf("POST /api/v1/permissions %s = %d, want %d (body: %s)", name, resp.StatusCode, want, raw)
	}
}

func readAllT444(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(raw)
}

// echoOf decodes the admin list view and returns one principal's action
// words (nil when the principal is absent).
func echoOf(t *testing.T, h *harness, target, principal string) []string {
	t.Helper()
	body := t215Admin(t, h, http.MethodGet, "api/v1/permissions", "", 200)
	var targets []struct {
		Name       string `json:"name"`
		Principals struct {
			Users  map[string][]string `json:"users"`
			Groups map[string][]string `json:"groups"`
		} `json:"principals"`
	}
	if err := json.Unmarshal([]byte(body), &targets); err != nil {
		t.Fatalf("decode permission list: %v (%s)", err, body)
	}
	for _, tg := range targets {
		if tg.Name != target {
			continue
		}
		if got, ok := tg.Principals.Users[principal]; ok {
			return got
		}
		return tg.Principals.Groups[principal]
	}
	t.Fatalf("target %s missing from the list view", target)
	return nil
}

// TestPermissionWireActionWords: the T-444 word table — every accepted
// spelling round-trips through the canonical five-word echo, the write
// alias grants deploy-cache ONLY (the split: annotate is its own word),
// and an unknown word is the family's 400 naming the new vocabulary.
func TestPermissionWireActionWords(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"wired", "wired-pw"}})
	seedRepo(t, h, "generic-local")

	tests := []struct {
		name     string
		words    string   // the body's action list
		wantEcho []string // the canonical echo, in order
	}{
		{"canonical five", `"read","deploy-cache","annotate","delete","manage"`,
			[]string{"read", "deploy-cache", "annotate", "delete", "manage"}},
		{"deploy-cache alone", `"deploy-cache"`, []string{"deploy-cache"}},
		{"write alias echoes deploy-cache", `"write"`, []string{"deploy-cache"}},
		{"alias beside canonical", `"read","write","annotate"`,
			[]string{"read", "deploy-cache", "annotate"}},
		{"annotate alone", `"annotate"`, []string{"annotate"}},
		{"legacy full set", `"read","write","delete","manage"`,
			[]string{"read", "deploy-cache", "delete", "manage"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			putTargetWire(t, h, "w-tgt", `{"users":{"wired":[`+tt.words+`]},"groups":{}}`, http.StatusCreated)
			got := echoOf(t, h, "w-tgt", "wired")
			if strings.Join(got, ",") != strings.Join(tt.wantEcho, ",") {
				t.Fatalf("echo = %v, want %v", got, tt.wantEcho)
			}
		})
	}

	// The unknown word: the 400 names the new vocabulary (and the alias).
	putTargetWire(t, h, "w-bad", `{"users":{"wired":["distribute"]},"groups":{}}`, http.StatusBadRequest)
	resp := t215As(t, h, http.MethodPost, "api/v1/permissions", adminUser, adminPass,
		`{"name":"w-bad","repos":["generic-local"],"principals":{"users":{"wired":["nope"]},"groups":{}}}`)
	if body := readAllT444(t, resp); resp.StatusCode != http.StatusBadRequest ||
		!strings.Contains(body, "read, deploy-cache, annotate, delete, manage") {
		t.Fatalf("unknown word = %d (body %s), want the 400 naming the five-word set", resp.StatusCode, body)
	}
}

// grantBitsHTTP seeds one user row with the exact action bits through the
// real store (the wire cannot spell rows the alias would blur).
func grantBitsHTTP(t *testing.T, h *harness, name, user string, read, write, del, manage, annotate bool) {
	t.Helper()
	now := metadata.Now()
	if err := h.md.Permissions().PutTarget(context.Background(), &metadata.PermissionTarget{
		Name: name, Repos: `["generic-local"]`, Includes: `["sec/**"]`, Excludes: `[]`,
		CreatedAt: now, UpdatedAt: now,
	}, []*metadata.PermissionPrincipal{{
		TargetName: name, Principal: user, PrincipalType: "user",
		CanRead: read, CanWrite: write, CanDelete: del, CanManage: manage, CanAnnotate: annotate,
	}}); err != nil {
		t.Fatalf("PutTarget %s: %v", name, err)
	}
}

// TestPropertiesAnnotateGateMatrix is the endpoint-gate table of the split
// (AC2): verb × grant shape × endpoint. The load-bearing rows — a
// deploy-cache grant WITHOUT annotate meets the properties family's 403
// (the pre-T-444 write shadow is gone), and an annotate grant without
// deploy-cache writes properties but not artifacts (annotate opens no
// content-byte face). The legacy verbs' rows are the zero-regression
// proof: r reads, w deploys, d deletes, m holds the permission view.
func TestPropertiesAnnotateGateMatrix(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{
		{"reader", "reader-pw"}, {"deployer", "deployer-pw"},
		{"annotator", "annotator-pw"}, {"manager", "manager-pw"},
		{"deleter", "deleter-pw"},
	})
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/sec/app.bin;env=prod", "app")

	grantBitsHTTP(t, h, "g-reader", "reader", true, false, false, false, false)
	grantBitsHTTP(t, h, "g-deployer", "deployer", true, true, false, false, false) // w, NO a: the split probe
	grantBitsHTTP(t, h, "g-annotator", "annotator", true, false, false, false, true)
	// r+m (the t215 seam shape: the view's path probe needs the read bit the
	// same row carries) — the annotate probe is the discriminator: m ↛ a.
	grantBitsHTTP(t, h, "g-manager", "manager", true, false, false, true, false)
	grantBitsHTTP(t, h, "g-deleter", "deleter", true, false, true, false, false)

	const props = "/binflow/api/storage/generic-local/sec/app.bin?properties="
	const content = "/binflow/generic-local/sec/"

	matrix := []struct {
		persona, pass string
		method, path  string
		want          int
		note          string
	}{
		{"reader", "reader-pw", http.MethodGet, props + "env", 200, "r gate unchanged on the read verb"},
		{"reader", "reader-pw", http.MethodPut, props + "qa=1", 403, "no annotate bit -> 403 probe"},
		{"reader", "reader-pw", http.MethodDelete, props + "env", 403, "no annotate bit -> 403 probe"},

		{"deployer", "deployer-pw", http.MethodPut, content + "dep.bin", 201, "deploy-cache face unchanged"},
		{"deployer", "deployer-pw", http.MethodPut, props + "qa=1", 403, "w WITHOUT a (new shape): the split probe"},
		{"deployer", "deployer-pw", http.MethodDelete, props + "env", 403, "w WITHOUT a (new shape): the split probe"},

		{"annotator", "annotator-pw", http.MethodPut, props + "qa=passed", 204, "a writes properties"},
		{"annotator", "annotator-pw", http.MethodDelete, props + "qa", 204, "a deletes properties"},
		{"annotator", "annotator-pw", http.MethodPut, content + "nope.bin", 403, "annotate opens no content-byte face"},
		{"annotator", "annotator-pw", http.MethodDelete, content + "app.bin", 403, "annotate implies no d"},

		{"deleter", "deleter-pw", http.MethodDelete, content + "dep.bin", 204, "d face unchanged"},
		{"manager", "manager-pw", http.MethodGet, "/binflow/api/storage/generic-local/sec/app.bin?permissions", 200, "m face unchanged (view route)"},
		{"manager", "manager-pw", http.MethodPut, props + "qa=1", 403, "m does not imply a (no privilege chain)"},
	}
	for _, tc := range matrix {
		t.Run(tc.note, func(t *testing.T) {
			resp := h.do(tc.method, tc.path, tc.persona, tc.pass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.want {
				t.Fatalf("%s %s as %s = %d, want %d (%s)",
					tc.method, tc.path, tc.persona, resp.StatusCode, tc.want, tc.note)
			}
		})
	}
}

// TestPermissionsViewAnnotateLetter: the ?permissions view appends the a
// letter (after m, the M7 append order preserved) — BinFlow's internal
// compact code per K68 point 2.
func TestPermissionsViewAnnotateLetter(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"full", "full-pw"}, {"anno", "anno-pw"}})
	seedRepo(t, h, "generic-local")
	putContent(t, h, "/binflow/generic-local/sec/app.bin", "app")
	grantBitsHTTP(t, h, "g-full", "full", true, true, true, true, true)
	grantBitsHTTP(t, h, "g-anno", "anno", false, false, false, false, true)

	body := t215Admin(t, h, http.MethodGet, "api/storage/generic-local/sec/app.bin?permissions", "", 200)
	var view struct {
		Principals struct {
			Users map[string][]string `json:"users"`
		} `json:"principals"`
	}
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("decode permissions view: %v (%s)", err, body)
	}
	if got := view.Principals.Users["full"]; strings.Join(got, "") != "rwdma" {
		t.Errorf("full-bit letters = %v, want [r w d m a]", got)
	}
	if got := view.Principals.Users["anno"]; strings.Join(got, "") != "a" {
		t.Errorf("annotate-only letters = %v, want [a]", got)
	}
}
