package httpapi_test

// L024-3A: GET /api/versions/{repoKey}/{path} — latest version by properties
// at its REAL mount (aql.md §16.4, D03-R12): the _any wildcards, the
// lowercase version property as the version source, the always-present
// artifacts array, the listFiles trailing-comma quirk (live bytes: the row
// ends `,\n  }` — not strict JSON) and the bare "Not Found" miss copy.

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// seedVersionsByProps lands the property corpus: three competing version
// values, two artifacts sharing the latest, one os=linux-tagged subset.
func seedVersionsByProps(t *testing.T, h *harness) {
	t.Helper()
	seedRepo(t, h, "props-local")
	for _, p := range []string{"a/old.jar", "a/two.jar", "a/two-b.jar", "b/nine.jar", "c/linux.jar"} {
		deposit(t, h, "props-local", p, "bytes-"+p)
	}
	merge := func(path string, props map[string][]string) {
		t.Helper()
		if err := h.md.NodeProps().Merge(context.Background(), "props-local", path, props); err != nil {
			t.Fatalf("merge props %s: %v", path, err)
		}
	}
	merge("a/old.jar", map[string][]string{"version": {"1.0"}})
	merge("a/two.jar", map[string][]string{"version": {"1.1"}})
	merge("a/two-b.jar", map[string][]string{"version": {"1.1"}})
	merge("b/nine.jar", map[string][]string{"version": {"0.9"}})
	merge("c/linux.jar", map[string][]string{"version": {"0.5"}, "os": {"linux"}})
}

func TestVersionsByProperties(t *testing.T) {
	h := newHarness(t)
	seedVersionsByProps(t, h)

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string // exact body when 200
	}{
		{
			name:       "_any/_any: latest across the repository, empty artifacts array",
			path:       "versions/_any/_any",
			wantStatus: http.StatusOK,
			wantBody:   "{\n  \"version\" : \"1.1\",\n  \"artifacts\" : [ ]\n}",
		},
		{
			name:       "repo-scoped with path _any",
			path:       "versions/props-local/_any",
			wantStatus: http.StatusOK,
			wantBody:   "{\n  \"version\" : \"1.1\",\n  \"artifacts\" : [ ]\n}",
		},
		{
			name:       "path prefix narrows to the b folder",
			path:       "versions/props-local/b",
			wantStatus: http.StatusOK,
			wantBody:   "{\n  \"version\" : \"0.9\",\n  \"artifacts\" : [ ]\n}",
		},
		{
			name:       "a property filter narrows to the os=linux subset",
			path:       "versions/_any/_any?os=linux",
			wantStatus: http.StatusOK,
			wantBody:   "{\n  \"version\" : \"0.5\",\n  \"artifacts\" : [ ]\n}",
		},
		{
			name:       "no hit answers the bare Not Found",
			path:       "versions/props-local/nope",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "an unknown repository answers the bare Not Found",
			path:       "versions/ghost-repo/_any",
			wantStatus: http.StatusNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.do(http.MethodGet, "/binflow/api/"+tt.path, adminUser, adminPass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d\n%s", resp.StatusCode, tt.wantStatus, mustGet(t, resp))
			}
			body := mustGet(t, resp)
			if tt.wantStatus != http.StatusOK {
				if !strings.Contains(body, "\"message\"") || !strings.Contains(body, "Not Found") {
					t.Fatalf("miss body must be the errors envelope's bare Not Found:\n%s", body)
				}
				return
			}
			if body != tt.wantBody {
				t.Fatalf("body = %q\nwant    %q", body, tt.wantBody)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
				t.Fatalf("content type = %q", ct)
			}
		})
	}

	t.Run("listFiles=1 renders the two-key rows with the trailing comma", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/versions/props-local/_any?listFiles=1", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d\n%s", resp.StatusCode, mustGet(t, resp))
		}
		body := mustGet(t, resp)
		// The live shape (§16.4/V-aa): repo+path two-key rows, each ending
		// with a trailing comma before the close brace — non-strict JSON by
		// design. The two 1.1 artifacts ride in (repo_key, path) order.
		want := "{\n  \"version\" : \"1.1\",\n  \"artifacts\" : [ " +
			"{\n    \"repo\" : \"props-local\",\n    \"path\" : \"a/two-b.jar\",\n  }," +
			" " +
			"{\n    \"repo\" : \"props-local\",\n    \"path\" : \"a/two.jar\",\n  } ]\n}"
		if body != want {
			t.Fatalf("body = %q\nwant    %q", body, want)
		}
	})

	t.Run("anonymous callers are challenged", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/versions/_any/_any", "", "", nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401\n%s", resp.StatusCode, mustGet(t, resp))
		}
	})
}
