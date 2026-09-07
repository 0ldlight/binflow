package httpapi_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/search"
)

// T-452 AC3: the M15 PRD §5.7 endpoint-panorama reconciliation, M16 row
// by row. The M16-assigned members are now IMPLEMENTED (usage landed in
// T-440; dates/creation, the UI search family and the QRL REST face in
// this ticket); the M16+/远期 rows keep the E-26 404 — "维持 404 与表一
// 致" is asserted by distinguishing the E-26 not-implemented wording from
// every implemented door's own answer (the 404-empty family's verbatim
// copy, the QRL disabled copy, the stash off-copy).
//
// Implemented rows asserted: aql (T-415), artifact+checksum (T-92),
// gavc/prop/pattern (T-417), usage (T-440), creation+dates (T-452),
// UI 族 artifactsearch/stashResults/packagesSearch/syntax-search (T-452),
// v1/system/query_rate_limiter (T-452).
func TestSearchPanoramaM16Rows(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	deposit(t, h, "generic-local", "acme/artifact.bin", "panorama-bytes")

	// Implemented: each door answers its OWN contract, never the E-26
	// not-implemented 404.
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		notBody    string // the copy that must NOT appear
	}{
		{"aql", http.MethodPost, "search/aql", http.StatusOK, "not implemented"},
		{"artifact", http.MethodGet, "search/artifact?name=artifact", http.StatusOK, "not implemented"},
		{"checksum", http.MethodGet, "search/checksum?sha256=" + strings.Repeat("0", 64), http.StatusOK, "not implemented"},
		{"gavc", http.MethodGet, "search/gavc?a=app", http.StatusOK, "not implemented"},
		{"prop", http.MethodGet, "search/prop?props=license", http.StatusOK, "not implemented"},
		{"pattern", http.MethodGet, "search/pattern?pattern=generic-local:acme/**", http.StatusOK, "not implemented"},
		{"usage (T-440)", http.MethodGet, "search/usage?notUsedSince=1000", http.StatusNotFound, "not implemented"},
		{"creation (T-452)", http.MethodGet, "search/creation?from=1000&to=2000", http.StatusNotFound, "not implemented"},
		{"dates (T-452)", http.MethodGet, "search/dates?from=1000&to=2000", http.StatusNotFound, "not implemented"},
		{"UI quick (T-452)", http.MethodPost, "artifactsearch/quick", http.StatusOK, "not implemented"},
		{"UI stash off-copy (T-452)", http.MethodPost, "stashResults", http.StatusNotFound, "not implemented"},
		{"UI packagesSearch (T-452)", http.MethodPost, "packagesSearch/leadFile", http.StatusNotFound, "not implemented"},
		{"UI syntax-search (T-452)", http.MethodPost, "syntax-search", http.StatusOK, "not implemented"},
		{"QRL REST (T-452)", http.MethodGet, "v1/system/query_rate_limiter/config", http.StatusBadRequest, "not implemented"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			if tt.method == http.MethodPost {
				if strings.HasPrefix(tt.path, "search/aql") || tt.path == "syntax-search" {
					body = []byte(`items.find({"repo":"generic-local"}).include("name")`)
				} else {
					body = []byte(`{"searchTerm":"artifact","repoKey":"generic-local","path":"acme/nothing.bin","a":"app"}`)
				}
			}
			resp := h.do(tt.method, "/binflow/api/"+tt.path, adminUser, adminPass, body, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("%s %s = %d, want %d", tt.method, tt.path, resp.StatusCode, tt.wantStatus)
			}
			page := mustGet(t, resp)
			if strings.Contains(strings.ToLower(page), tt.notBody) {
				t.Fatalf("%s answered the E-26 not-implemented copy — the panorama says IMPLEMENTED:\n%s", tt.path, page)
			}
		})
	}

	// The implemented 404-family doors carry their own verbatim copies.
	for _, path := range []string{"search/usage?notUsedSince=1000", "search/creation?from=1000&to=2000", "search/dates?from=1000&to=2000"} {
		resp := h.do(http.MethodGet, "/binflow/api/"+path, adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		_ = resp.Body.Close()
		if !strings.Contains(body, "No results found.") {
			t.Fatalf("%s must carry the 404 family's verbatim copy: %s", path, body)
		}
	}
	qrl := h.do(http.MethodGet, "/binflow/api/v1/system/query_rate_limiter/config", adminUser, adminPass, nil, nil)
	if body := mustGet(t, qrl); !strings.Contains(body, "Query rate limiter is disabled") {
		t.Fatalf("QRL factory arm must carry the verbatim copy: %s", body)
	}
	_ = qrl.Body.Close()
}

// TestSearchPanoramaDeferredRows pins the table's M16+/远期 rows: every
// member there keeps the E-26 404 with the not-implemented wording (the
// audit's "维持 404" half — badChecksum / versions / latestVersion /
// license / buildArtifacts plus the two out-of-SearchResource extras
// archive / latestVersionByProperties).
//
// T-511 assertion inversion ⑥ (FR-152.3 / aql.md §15.4): GET /api/search/
// dependency LEFT this table — the member ROUTES now (its 400 family and
// positive halves live in search_build_test.go). buildArtifacts stays in
// its GET arm only: the official member is POST (the OSS live matrix's
// GET-405 arm), which search_build_test.go owns.
func TestSearchPanoramaDeferredRows(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{
		"search/badChecksum?md5=" + strings.Repeat("0", 32),
		"search/versions?g=com.acme&a=app",
		"search/latestVersion?g=com.acme&a=app",
		"search/license?name=apache",
		"search/buildArtifacts",
		"search/archive?name=lib",
		"search/latestVersionByProperties?os=linux",
	} {
		t.Run(path, func(t *testing.T) {
			var body []byte
			if strings.HasPrefix(path, "search/buildArtifacts") {
				body = []byte(`{"buildName":"x","buildNumber":"1"}`)
			}
			resp := h.do(http.MethodGet, "/binflow/api/"+path, adminUser, adminPass, body, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("%s = %d, want the 404", path, resp.StatusCode)
			}
			page := mustGet(t, resp)
			if !strings.Contains(strings.ToLower(page), "not implemented") {
				t.Fatalf("%s must keep the E-26 not-implemented copy (the panorama's deferred row):\n%s", path, page)
			}
		})
	}
}

// TestSearchPanoramaCapFamilyShared pins the shared K63 ceiling across the
// NEW doors (the truncation header is the family's single surface):
// a dates-family page beyond the cap answers exactly the cap plus the
// header — the same leg T-417 ran for the legacy trio.
func TestSearchPanoramaCapFamilyShared(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "cap-local")
	raw := h.md.(interface {
		RawConn(ctx context.Context, fn func(*sql.Conn) error) error
	})
	ctx := context.Background()
	sum := sha256.Sum256([]byte("cap-blob"))
	nowTS := "2026-09-01T00:00:00Z"
	err := raw.RawConn(ctx, func(conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
			return err
		}
		commit := false
		defer func() {
			if !commit {
				_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
			}
		}()
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO blobs (sha256, sha1, md5, size, created_at) VALUES (?, ?, ?, 9, ?)`,
			hex.EncodeToString(sum[:]), strings.Repeat("1", 40), strings.Repeat("2", 32), nowTS); err != nil {
			return err
		}
		stmt, err := conn.PrepareContext(ctx,
			`INSERT INTO nodes (repo_key, path, sha256, size, mime, created_by, created_at, updated_at)
			 VALUES (?, ?, ?, 9, 'application/octet-stream', 'admin', ?, ?)`)
		if err != nil {
			return err
		}
		for i := 0; i < search.ResultCap+2; i++ {
			if _, err := stmt.ExecContext(ctx, "cap-local", fmt.Sprintf("bulk/item-%05d.bin", i),
				hex.EncodeToString(sum[:]), nowTS, nowTS); err != nil {
				return err
			}
		}
		if err := stmt.Close(); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			return err
		}
		commit = true
		return nil
	})
	if err != nil {
		t.Fatalf("seed cap corpus: %v", err)
	}

	for _, door := range []string{"creation", "dates"} {
		resp := h.do(http.MethodGet, "/binflow/api/search/"+door+"?from=1000", adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s cap status = %d", door, resp.StatusCode)
		}
		if got := resp.Header.Get(search.TruncatedHeader); got != "true" {
			t.Fatalf("%s truncation header = %q, want \"true\"", door, got)
		}
		var body struct {
			Results []struct {
				URI string `json:"uri"`
			} `json:"results"`
		}
		page := mustGet(t, resp)
		_ = resp.Body.Close()
		if err := json.Unmarshal([]byte(page), &body); err != nil {
			t.Fatalf("%s body %q: %v", door, page, err)
		}
		if len(body.Results) != search.ResultCap {
			t.Fatalf("%s results = %d, want exactly the %d cap", door, len(body.Results), search.ResultCap)
		}
	}
}
