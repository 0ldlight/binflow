package httpapi_test

// L001-5 (D21, LOOP 001 tail): the repository GET planes' top-level url
// echo. A REMOTE row's top-level url is the real UPSTREAM (the canonical
// configuration's url — Artifactory's RepoDetails shape, E4 in
// reports/compatibility/L000-docker-remote-diff.md), while local/virtual
// rows keep the self-derived <contextUrl>/<key> (T-493, FR-157①). Both
// faces — the list (GET /api/repositories) and the detail
// (GET /api/repositories/{key}) — and the credential posture: only the
// "url" key lifts out of the configuration; username/password never ride
// the top level.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// repoURLEchoFixture seeds the four rows the table walks: a canonical
// remote (created through the REST plane, so repo.Service's masking ran),
// a raw-seeded remote pair (url present / url absent — the lift and the
// fallback arm), plus the local/virtual controls.
func repoURLEchoFixture(t *testing.T, h *harness, upstream, fallbackURL string) {
	t.Helper()
	remoteBody := `{"rclass":"remote","packageType":"generic","url":"` + upstream +
		`","username":"ci","password":"s3cret","allowPrivateUpstream":true}`
	resp := putRepo(t, h, "echo-remote", remoteBody)
	if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusOK {
		t.Fatalf("seed canonical remote: %d body=%s", code, out)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rows := []*metadata.Repo{
		// Raw rows seeded straight into the store (no canonicalizer pass):
		// the lift must still find a url, and stay on the context URL when
		// there is none (the defensive arm).
		{RepoKey: "echo-remote-raw", Type: "remote", PackageType: "generic",
			Config: `{"url":"` + fallbackURL + `","username":"raw-user"}`, CreatedAt: now, UpdatedAt: now},
		{RepoKey: "echo-remote-urlless", Type: "remote", PackageType: "generic",
			Config: `{}`, CreatedAt: now, UpdatedAt: now},
		{RepoKey: "echo-local", Type: "local", PackageType: "generic",
			Config: `{}`, CreatedAt: now, UpdatedAt: now},
		{RepoKey: "echo-virtual", Type: "virtual", PackageType: "generic",
			Config: `{"repositories":["echo-local"]}`, CreatedAt: now, UpdatedAt: now},
	}
	ctx := t.Context()
	for _, row := range rows {
		if err := h.md.Repos().Create(ctx, row); err != nil {
			t.Fatalf("seed %s: %v", row.RepoKey, err)
		}
	}
}

// wantURLOf is the per-row expectation: remote rows carrying a config url
// echo the upstream, everything else (including the urlless remote) echoes
// the self-derived context URL.
func wantURLOf(h *harness, key, upstream, fallbackURL string) string {
	switch key {
	case "echo-remote":
		return upstream
	case "echo-remote-raw":
		return fallbackURL
	default:
		return h.srv.URL + "/binflow/" + key
	}
}

// TestRepoURLEchoListDetailBothFaces: table-driven over the seeded rows,
// asserting BOTH the list entry and the single-repo body agree, and that
// no credential rides the top level or the list body.
func TestRepoURLEchoListDetailBothFaces(t *testing.T) {
	const (
		upstream    = "http://127.0.0.1:9099"
		fallbackURL = "http://10.0.0.7:1234/up"
	)
	h := newHarness(t)
	repoURLEchoFixture(t, h, upstream, fallbackURL)

	cases := []string{"echo-remote", "echo-remote-raw", "echo-remote-urlless", "echo-local", "echo-virtual"}

	// Face 1: the list — one entry per row, url per the D21 ruling.
	resp := h.do(http.MethodGet, "/binflow/api/repositories", adminUser, adminPass, nil, nil)
	listBody := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d body=%s", resp.StatusCode, listBody)
	}
	var items []struct {
		Key  string `json:"key"`
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal([]byte(listBody), &items); err != nil {
		t.Fatalf("list body %q: %v", listBody, err)
	}
	gotList := map[string]string{}
	for _, it := range items {
		gotList[it.Key] = it.URL
	}
	for _, key := range cases {
		if want := wantURLOf(h, key, upstream, fallbackURL); gotList[key] != want {
			t.Errorf("list url[%s] = %q, want %q", key, gotList[key], want)
		}
	}
	// NFR-S14: the seeded upstream password never crosses the boundary in
	// the list face (repo.Service masked it at write time; the url lift
	// must not widen the surface).
	if strings.Contains(listBody, "s3cret") {
		t.Errorf("list body leaks the upstream password marker: %s", listBody)
	}

	// Face 2: the single-repo body — same ruling, plus the credential
	// posture: username/password never ride the top level.
	for _, key := range cases {
		resp := h.do(http.MethodGet, "/binflow/api/repositories/"+key, adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status %d body=%s", key, resp.StatusCode, body)
		}
		var top struct {
			URL      string `json:"url"`
			Username string `json:"username"`
			Password string `json:"password"`
			Config   struct {
				URL string `json:"url"`
			} `json:"configuration"`
		}
		if err := json.Unmarshal([]byte(body), &top); err != nil {
			t.Fatalf("GET %s body %q: %v", key, body, err)
		}
		if want := wantURLOf(h, key, upstream, fallbackURL); top.URL != want {
			t.Errorf("detail url[%s] = %q, want %q", key, top.URL, want)
		}
		if top.Username != "" || top.Password != "" {
			t.Errorf("detail[%s]: top-level username/password echoed (%q/%q) — credentials must stay inside configuration",
				key, top.Username, top.Password)
		}
		if top.Config.URL != "" && top.Config.URL != top.URL {
			t.Errorf("detail[%s]: top-level url %q disagrees with configuration.url %q", key, top.URL, top.Config.URL)
		}
	}
}
