package httpapi_test

// L024-3A: the versions family wire (aql.md §16.2/§16.3, the L024-1 frozen
// contract): the two-key thin row new→old, the unique-snapshot expansion,
// the verbatim 404 copy, the filter-after-verdict execution order, and
// latestVersion's three v arms over text/plain.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// seedVersionStack lands the maven corpus: demo-app carries two releases,
// one unique and one non-unique snapshot line; snap-only is integration-only.
func seedVersionStack(t *testing.T, h *harness) {
	t.Helper()
	seedRepo(t, h, "mvn-local")
	for _, p := range []string{
		"com/acme/demo-app/1.0/demo-app-1.0.jar",
		"com/acme/demo-app/1.1/demo-app-1.1.jar",
		"com/acme/demo-app/2.0-SNAPSHOT/demo-app-2.0-20260915.175736-1.jar",
		"com/acme/demo-app/3.0-SNAPSHOT/demo-app-3.0-SNAPSHOT.jar",
		"com/acme/snap-only/1.0-SNAPSHOT/snap-only-1.0-SNAPSHOT.jar",
	} {
		deposit(t, h, "mvn-local", p, "bytes-of-"+p)
	}
}

// versionRows decodes a versions body.
func versionRows(t *testing.T, body string) []struct {
	Version     string `json:"version"`
	Integration bool   `json:"integration"`
} {
	t.Helper()
	var parsed struct {
		Results []struct {
			Version     string `json:"version"`
			Integration bool   `json:"integration"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("versions body is not JSON: %v\n%s", err, body)
	}
	return parsed.Results
}

func TestSearchVersions(t *testing.T) {
	h := newHarness(t)
	seedVersionStack(t, h)

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantBody   string // substring when 200/404 copy check applies
		wantRows   []string
	}{
		{
			name:       "hit: newest first, segments literal (diff L1)",
			query:      "g=com.acme&a=demo-app",
			wantStatus: http.StatusOK,
			wantRows:   []string{"3.0-SNAPSHOT", "2.0-SNAPSHOT", "1.1", "1.0"},
		},
		{
			name:       "v wildcard filters the full set",
			query:      "g=com.acme&a=demo-app&v=1.*",
			wantStatus: http.StatusOK,
			wantRows:   []string{"1.1", "1.0"},
		},
		{
			name:       "v filter miss stays 200 empty (filter runs after the verdict)",
			query:      "g=com.acme&a=demo-app&v=9.*",
			wantStatus: http.StatusOK,
			wantRows:   []string{},
		},
		{
			name:       "no version at all answers the verbatim 404",
			query:      "g=com.nope&a=nothing",
			wantStatus: http.StatusNotFound,
			wantBody:   "Unable to find artifact versions",
		},
		{
			name:       "missing g answers 400",
			query:      "a=demo-app",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing a answers 400",
			query:      "g=com.acme",
			wantStatus: http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.do(http.MethodGet, "/binflow/api/search/versions?"+tt.query, adminUser, adminPass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d\n%s", resp.StatusCode, tt.wantStatus, mustGet(t, resp))
			}
			body := mustGet(t, resp)
			if tt.wantBody != "" && !strings.Contains(body, tt.wantBody) {
				t.Fatalf("body must carry %q:\n%s", tt.wantBody, body)
			}
			if tt.wantRows == nil {
				return
			}
			rows := versionRows(t, body)
			if len(rows) != len(tt.wantRows) {
				t.Fatalf("rows = %v, want %v", rows, tt.wantRows)
			}
			for i, want := range tt.wantRows {
				if rows[i].Version != want {
					t.Fatalf("row %d = %+v, want version %q", i, rows[i], want)
				}
			}
		})
	}

	t.Run("the integration flag rides the snapshot lines", func(t *testing.T) {
		rows := versionRows(t, getVersionBody(t, h, "g=com.acme&a=demo-app"))
		if !rows[0].Integration || !rows[1].Integration {
			t.Fatalf("snapshot rows must carry integration=true: %+v", rows[:2])
		}
		if rows[2].Integration || rows[3].Integration {
			t.Fatalf("release rows must carry integration=false: %+v", rows[2:])
		}
	})
}

// getVersionBody fetches one versions page for the flag assertions.
func getVersionBody(t *testing.T, h *harness, query string) string {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/search/versions?"+query, adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("versions status = %d", resp.StatusCode)
	}
	return mustGet(t, resp)
}

func TestSearchLatestVersion(t *testing.T) {
	h := newHarness(t)
	seedVersionStack(t, h)

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantBody   string
		wantText   bool
	}{
		{
			name:       "v absent skips integrations to the latest release",
			query:      "g=com.acme&a=demo-app",
			wantStatus: http.StatusOK,
			wantBody:   "1.1",
			wantText:   true,
		},
		{
			name:       "v wildcard takes the first pattern hit",
			query:      "g=com.acme&a=demo-app&v=1.*",
			wantStatus: http.StatusOK,
			wantBody:   "1.1",
			wantText:   true,
		},
		{
			name:       "v wildcard miss answers the integration 404",
			query:      "g=com.acme&a=demo-app&v=9.*",
			wantStatus: http.StatusNotFound,
			wantBody:   "Latest integration version not found",
		},
		{
			// Diff L2 (V-ab closed): v + the line's snapshot timestamp + "-N",
			// glued with NO separator between v and the timestamp.
			name:       "v non-wildcard concatenates the line's snapshot parts",
			query:      "g=com.acme&a=demo-app&v=2.0",
			wantStatus: http.StatusOK,
			wantBody:   "2.0" + "20260915.175736" + "-1",
			wantText:   true,
		},
		{
			// The full-literal spelling is not the line form — the
			// differential pinned only the release-line arm (l03/l04);
			// this BinFlow-made arm follows the same rule honestly.
			name:       "v non-wildcard on the full literal spelling answers the empty-set 404",
			query:      "g=com.acme&a=demo-app&v=3.0-SNAPSHOT",
			wantStatus: http.StatusNotFound,
			wantBody:   "Unable to find artifact versions",
		},
		{
			name:       "v non-wildcard on a release-only line answers the empty-set 404 (live arm)",
			query:      "g=com.acme&a=demo-app&v=1.1",
			wantStatus: http.StatusNotFound,
			wantBody:   "Unable to find artifact versions",
		},
		{
			name:       "no release at all answers the release 404",
			query:      "g=com.acme&a=snap-only",
			wantStatus: http.StatusNotFound,
			wantBody:   "Latest release version not found",
		},
		{
			name:       "no version at all answers the empty-set 404",
			query:      "g=com.nope&a=nothing",
			wantStatus: http.StatusNotFound,
			wantBody:   "Unable to find artifact versions",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.do(http.MethodGet, "/binflow/api/search/latestVersion?"+tt.query, adminUser, adminPass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d\n%s", resp.StatusCode, tt.wantStatus, mustGet(t, resp))
			}
			body := mustGet(t, resp)
			if !strings.Contains(body, tt.wantBody) {
				t.Fatalf("body must carry %q:\n%s", tt.wantBody, body)
			}
			if tt.wantText {
				if ct := resp.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
					t.Fatalf("content type = %q, want text/plain", ct)
				}
				if body != tt.wantBody {
					t.Fatalf("body = %q, want the bare version string %q", body, tt.wantBody)
				}
			}
		})
	}
}
