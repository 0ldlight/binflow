package httpapi_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/config"
)

// T-92: the /api/search domain (FR-26, SR-01/SR-02/SR-04, W14/W15/W16/W36).
// The routes open here are exactly artifact + checksum; every other family
// member stays on the E-26 404 (the routes_test.go matrix keeps those rows,
// with this ticket's flip of the artifact row noted below).

// searchFileInfo is the client-side view of one results[] entry — the E-09
// FileInfo field set (size stays a STRING, checksums the sha1/md5/sha256
// triple).
type searchFileInfo struct {
	URI               string            `json:"uri"`
	DownloadURI       string            `json:"downloadUri"`
	Repo              string            `json:"repo"`
	Path              string            `json:"path"`
	Created           string            `json:"created"`
	CreatedBy         string            `json:"createdBy"`
	LastModified      string            `json:"lastModified"`
	ModifiedBy        string            `json:"modifiedBy"`
	LastUpdated       string            `json:"lastUpdated"`
	Size              string            `json:"size"`
	MimeType          string            `json:"mimeType"`
	Checksums         map[string]string `json:"checksums"`
	OriginalChecksums map[string]string `json:"originalChecksums"`
}

type searchBody struct {
	Results []searchFileInfo `json:"results"`
}

// decodeSearch fetches and decodes one search response body.
func decodeSearch(t *testing.T, resp *http.Response) searchBody {
	t.Helper()
	var b searchBody
	if err := json.Unmarshal([]byte(mustGet(t, resp)), &b); err != nil {
		t.Fatalf("search body is not JSON: %v", err)
	}
	return b
}

// searchHits renders the results as "<repo><path>" keys in response order.
func searchHits(b searchBody) []string {
	out := make([]string, 0, len(b.Results))
	for _, r := range b.Results {
		out = append(out, r.Repo+r.Path)
	}
	return out
}

// deposit PUTs one artifact through the content plane and returns its
// sha256.
func deposit(t *testing.T, h *harness, repo, path, content string) string {
	t.Helper()
	resp := h.do(http.MethodPut, "/binflow/"+repo+"/"+path, adminUser, adminPass, []byte(content), nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("deposit %s/%s: status %d", repo, path, resp.StatusCode)
	}
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// seedSearchStack creates the W14 playground: two generic repositories, one
// artifact under acme/, one unrelated jar, and returns the artifact's sha256.
func seedSearchStack(t *testing.T, h *harness) (sha256hex string) {
	t.Helper()
	seedRepo(t, h, "generic-local")
	seedRepo(t, h, "other-local")
	sha256hex = deposit(t, h, "generic-local", "acme/artifact.bin", "artifact-bytes")
	deposit(t, h, "generic-local", "acme/lib.jar", "jar-bytes")
	return sha256hex
}

// TestSearchArtifactW14 is SR-01 (W14): the artifact search returns the
// E-09 FileInfo field set, a missing name answers the E-01 400, an empty
// result is [] not null.
func TestSearchArtifactW14(t *testing.T) {
	h := newHarness(t)
	seedSearchStack(t, h)

	resp := h.do(http.MethodGet, "/binflow/api/search/artifact?name=artifact", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeSearch(t, resp)
	if len(body.Results) != 1 {
		t.Fatalf("results = %v, want exactly /acme/artifact.bin", searchHits(body))
	}
	got := body.Results[0]
	if got.Repo != "generic-local" || got.Path != "/acme/artifact.bin" {
		t.Fatalf("hit = %s%s, want generic-local/acme/artifact.bin", got.Repo, got.Path)
	}
	if got.URI != h.srv.URL+"/binflow/api/storage/generic-local/acme/artifact.bin" || got.DownloadURI != got.URI {
		t.Fatalf("uri/downloadUri = %q / %q", got.URI, got.DownloadURI)
	}
	if got.Size != "14" { // len("artifact-bytes")
		t.Fatalf("size = %q, want the STRING \"14\"", got.Size)
	}
	if got.MimeType != "application/octet-stream" {
		t.Fatalf("mimeType = %q", got.MimeType)
	}
	wantSum := sha256.Sum256([]byte("artifact-bytes"))
	if got.CreatedBy != adminUser || got.ModifiedBy != adminUser {
		t.Fatalf("createdBy/modifiedBy = %q / %q, want %q", got.CreatedBy, got.ModifiedBy, adminUser)
	}
	for _, stamp := range []string{got.Created, got.LastModified, got.LastUpdated} {
		if !strings.Contains(stamp, "T") {
			t.Fatalf("timestamp %q is not ISO8601-shaped", stamp)
		}
	}
	if got.Checksums["sha256"] != hex.EncodeToString(wantSum[:]) {
		t.Fatalf("checksums.sha256 = %v", got.Checksums)
	}
	if got.Checksums["sha1"] == "" || got.Checksums["md5"] == "" {
		t.Fatalf("checksums must carry the ledger triple: %v", got.Checksums)
	}
	if len(got.OriginalChecksums) != len(got.Checksums) {
		t.Fatalf("originalChecksums must mirror the stored triple: %v vs %v", got.OriginalChecksums, got.Checksums)
	}

	// The field set must agree with /api/storage's item info (E-09 reuse).
	item := h.do(http.MethodGet, "/binflow/api/storage/generic-local/acme/artifact.bin", adminUser, adminPass, nil, nil)
	defer func() { _ = item.Body.Close() }()
	var itemInfo searchFileInfo
	if err := json.Unmarshal([]byte(mustGet(t, item)), &itemInfo); err != nil {
		t.Fatalf("item info decode: %v", err)
	}
	if itemInfo.Path != got.Path || itemInfo.Size != got.Size || itemInfo.Checksums["sha256"] != got.Checksums["sha256"] {
		t.Fatalf("search entry and item info disagree: %+v vs %+v", got, itemInfo)
	}

	// Missing / blank name answers the E-01 400 (W14's second leg).
	for _, q := range []string{"", "?name=", "?name=%20%20"} {
		resp := h.do(http.MethodGet, "/binflow/api/search/artifact"+q, adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("query %q status = %d, want 400", q, resp.StatusCode)
		}
		eb := decodeError(t, resp)
		if eb.Errors[0].Status != http.StatusBadRequest {
			t.Fatalf("envelope status = %d, want 400", eb.Errors[0].Status)
		}
	}

	// A fragment nobody holds answers an empty ARRAY.
	resp = h.do(http.MethodGet, "/binflow/api/search/artifact?name=nothing-matches-this", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("empty search status = %d", resp.StatusCode)
	}
	if body := decodeSearch(t, resp); body.Results == nil || len(body.Results) != 0 {
		t.Fatalf("empty search results = %#v, want []", body.Results)
	}
}

// TestSearchArtifactFilterW16 is SR-01's filter leg (W16 first half): the
// repos csv narrows the search; unknown keys match nothing without error.
func TestSearchArtifactFilterW16(t *testing.T) {
	h := newHarness(t)
	seedSearchStack(t, h)
	deposit(t, h, "other-local", "nested/artifact.bin", "other-bytes")

	tests := []struct {
		query string
		want  []string
	}{
		{"?name=artifact", []string{"generic-local/acme/artifact.bin", "other-local/nested/artifact.bin"}},
		{"?name=artifact&repos=generic-local", []string{"generic-local/acme/artifact.bin"}},
		{"?name=artifact&repos=other-local", []string{"other-local/nested/artifact.bin"}},
		{"?name=artifact&repos=generic-local,other-local", []string{"generic-local/acme/artifact.bin", "other-local/nested/artifact.bin"}},
		{"?name=artifact&repos=ghost-local", nil},
		{"?name=artifact&repos=ghost-local,generic-local", []string{"generic-local/acme/artifact.bin"}},
		{"?name=artifact&repos=", []string{"generic-local/acme/artifact.bin", "other-local/nested/artifact.bin"}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			resp := h.do(http.MethodGet, "/binflow/api/search/artifact"+tt.query, adminUser, adminPass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			got := searchHits(decodeSearch(t, resp))
			if len(got) != len(tt.want) {
				t.Fatalf("hits = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("hits = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// TestSearchChecksumW15 is SR-02 (W15): the same blob deployed at two paths
// across two repositories (C07) surfaces as two results — by sha256, sha1
// and md5 alike — and the query-shape failures answer the E-01 400.
func TestSearchChecksumW15(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	seedRepo(t, h, "other-local")
	sha256hex := deposit(t, h, "generic-local", "acme/artifact.bin", "shared-bytes")
	deposit(t, h, "other-local", "mirror/artifact.bin", "shared-bytes")

	// The ledger triple the auxiliary arms address.
	blob, err := h.md.Blobs().Get(context.Background(), sha256hex)
	if err != nil {
		t.Fatalf("ledger row: %v", err)
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"sha256", "?sha256=" + sha256hex, []string{"generic-local/acme/artifact.bin", "other-local/mirror/artifact.bin"}},
		{"sha1", "?sha1=" + blob.Sha1, []string{"generic-local/acme/artifact.bin", "other-local/mirror/artifact.bin"}},
		{"md5", "?md5=" + blob.Md5, []string{"generic-local/acme/artifact.bin", "other-local/mirror/artifact.bin"}},
		{"mixed-case sha256", "?sha256=" + strings.ToUpper(sha256hex), []string{"generic-local/acme/artifact.bin", "other-local/mirror/artifact.bin"}},
		{"repos filter narrows", "?sha256=" + sha256hex + "&repos=other-local", []string{"other-local/mirror/artifact.bin"}},
		{"unknown digest is empty", "?sha256=" + strings.Repeat("0", 64), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.do(http.MethodGet, "/binflow/api/search/checksum"+tt.query, adminUser, adminPass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			got := searchHits(decodeSearch(t, resp))
			if len(got) != len(tt.want) {
				t.Fatalf("hits = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("hits = %v, want %v", got, tt.want)
				}
			}
		})
	}

	// Query-shape failures: all digests absent, malformed values (rest-api
	// section 4's 400 family).
	for _, q := range []string{
		"",
		"?repos=generic-local",
		"?sha256=abc123",
		"?sha256=" + strings.Repeat("z", 64),
		"?sha1=" + strings.Repeat("a", 64),
		"?md5=tooshort",
	} {
		resp := h.do(http.MethodGet, "/binflow/api/search/checksum"+q, adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("query %q status = %d, want 400", q, resp.StatusCode)
		}
		decodeError(t, resp)
	}
}

// TestSearchACLW16 is SR-01/SR-02's zero-leak leg (W16 second half,
// NFR-S24): on an anonymous_access=false instance, ci-bot — read-granted on
// generic-local/ci-out/** only — never sees the other repositories' rows,
// anonymous callers meet the closed-instance 403, and admin sees everything.
func TestSearchACLW16(t *testing.T) {
	h := newHarnessCfg(t, func(c *config.Config) { c.Security.AnonymousAccess = false },
		[][2]string{{"ci-bot", "ci-pw"}})
	seedRepo(t, h, "generic-local")
	seedRepo(t, h, "other-local")
	// ci-bot holds read on the ci-out/** arm of BOTH repositories — the
	// acme/ arm of generic-local stays off-limits, and so does everything
	// the includes pattern does not name even inside a granted repo.
	grant(t, h, "ci-out-r", "generic-local", "ci-out/**", "ci-bot", true, false, false)
	grant(t, h, "other-out-r", "other-local", "ci-out/**", "ci-bot", true, false, false)

	visible := deposit(t, h, "generic-local", "ci-out/artifact.bin", "visible-bytes")
	hidden := deposit(t, h, "generic-local", "acme/artifact.bin", "hidden-bytes")
	deposit(t, h, "other-local", "ci-out/artifact.bin", "hidden-bytes")

	t.Run("ci-bot sees only the granted arms, never acme/", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/search/artifact?name=artifact", "ci-bot", "ci-pw", nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		want := []string{"generic-local/ci-out/artifact.bin", "other-local/ci-out/artifact.bin"}
		if got := searchHits(decodeSearch(t, resp)); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("ci-bot hits = %v, want %v (zero leak of acme/)", got, want)
		}
	})

	t.Run("checksum search filters the cross-repo references the same way", func(t *testing.T) {
		// hidden-bytes sits at two paths (generic-local/acme and
		// other-local/ci-out); the pattern grants ci-bot only the second.
		resp := h.do(http.MethodGet, "/binflow/api/search/checksum?sha256="+hidden, "ci-bot", "ci-pw", nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if got := searchHits(decodeSearch(t, resp)); len(got) != 1 || got[0] != "other-local/ci-out/artifact.bin" {
			t.Fatalf("ci-bot checksum hits = %v, want exactly other-local/ci-out/artifact.bin", got)
		}
	})

	t.Run("admin sees every repository", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/search/artifact?name=artifact", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		got := searchHits(decodeSearch(t, resp))
		want := []string{"generic-local/acme/artifact.bin", "generic-local/ci-out/artifact.bin", "other-local/ci-out/artifact.bin"}
		if len(got) != len(want) {
			t.Fatalf("admin hits = %v, want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("admin hits = %v, want %v", got, want)
			}
		}
	})

	t.Run("anonymous on the closed instance meets 403 (rest-api section 4)", func(t *testing.T) {
		for _, path := range []string{
			"/binflow/api/search/artifact?name=artifact",
			"/binflow/api/search/checksum?sha256=" + visible,
		} {
			resp := h.do(http.MethodGet, path, "", "", nil, nil)
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("%s anonymous status = %d, want 403", path, resp.StatusCode)
			}
			decodeError(t, resp)
		}
	})
}

// TestSearchAnonymousOpenInstance pins the anonymous channel on an OPEN
// instance (O4 boundary maintained): anonymous search passes and sees what
// an anonymous download would see.
func TestSearchAnonymousOpenInstance(t *testing.T) {
	h := newHarness(t)
	seedSearchStack(t, h)

	resp := h.do(http.MethodGet, "/binflow/api/search/artifact?name=artifact", "", "", nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anonymous open-instance status = %d, want 200", resp.StatusCode)
	}
	if got := searchHits(decodeSearch(t, resp)); len(got) != 1 {
		t.Fatalf("anonymous hits = %v", got)
	}
}

// TestSearchUnimplementedFamilyW36 is SR-04 (W36): the search domain opens
// exactly artifact + checksum — every other family member stays on the E-26
// 404 with the not-implemented wording, and so do foreign verbs on the two
// open paths. (The M1 E-26 matrix row for /api/search/artifact flipped in
// this same ticket — routes_test.go carries the note.)
func TestSearchUnimplementedFamilyW36(t *testing.T) {
	h := newHarness(t)

	for _, path := range []string{
		"/binflow/api/search/props?props=license",
		"/binflow/api/search/users?name=admin",
		"/binflow/api/search/artifactory?name=x",
		"/binflow/api/search/artifactory/internal",
		"/binflow/api/search/pattern?pattern=**/*.jar",
		"/binflow/api/search/badge?sha256=" + strings.Repeat("0", 64),
		"/binflow/api/search/gavc?g=com.acme&a=demo-app", // SR-03: P2, still closed
		"/binflow/api/search",
		"/binflow/api/search/",
	} {
		t.Run(path, func(t *testing.T) {
			resp := h.do(http.MethodGet, path, adminUser, adminPass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", resp.StatusCode)
			}
			eb := decodeError(t, resp)
			if !strings.Contains(strings.ToLower(eb.Errors[0].Message), "not implemented") {
				t.Fatalf("message %q lacks the not-implemented wording", eb.Errors[0].Message)
			}
		})
	}

	// Foreign verbs on the two open paths stay 404 (the domain is GET-only).
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/binflow/api/search/artifact?name=x"},
		{http.MethodPut, "/binflow/api/search/checksum?sha256=" + strings.Repeat("a", 64)},
		{http.MethodDelete, "/binflow/api/search/artifact"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			resp := h.do(tc.method, tc.path, adminUser, adminPass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("%s status = %d, want 404", tc.method, resp.StatusCode)
			}
		})
	}
}

// TestSearchPerfSpotCheck is the NFR-P17 spot check: 100k nodes, the P95 of
// an admin name search stays under 1s (the formal gate belongs to T-105;
// this pins that the LIKE path is nowhere near the ceiling). Seeding goes
// straight into SQLite through one transaction — 100k content-plane PUTs
// would measure argon2 and HTTP, not search.
func TestSearchPerfSpotCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("100k-node seeding skipped in -short mode")
	}
	if raceEnabled {
		t.Skip("NFR-P17 budget is a production number; the race detector inflates the measured path ~17x (formal gate: T-105, no-race run)")
	}
	h := newHarness(t)
	const totalNodes = 100_000
	const needles = 100

	for i := 0; i < 5; i++ {
		seedRepo(t, h, fmt.Sprintf("perf-%d", i))
	}

	raw := h.md.(interface {
		RawConn(ctx context.Context, fn func(*sql.Conn) error) error
	})
	ctx := context.Background()
	err := raw.RawConn(ctx, func(conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
			return fmt.Errorf("begin: %w", err)
		}
		commit := false
		defer func() {
			if !commit {
				_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
			}
		}()
		now := "2026-08-19T00:00:00Z"
		// 200 shared blobs satisfy the nodes.sha256 FK.
		blobStmt, err := conn.PrepareContext(ctx,
			`INSERT INTO blobs (sha256, sha1, md5, size, created_at) VALUES (?, ?, ?, 1024, ?)`)
		if err != nil {
			return err
		}
		for i := 0; i < 200; i++ {
			sum := sha256.Sum256([]byte(fmt.Sprintf("perf-blob-%d", i)))
			if _, err := blobStmt.ExecContext(ctx, hex.EncodeToString(sum[:]),
				fmt.Sprintf("%040d", i), fmt.Sprintf("%032d", i), now); err != nil {
				return err
			}
		}
		if err := blobStmt.Close(); err != nil {
			return err
		}
		nodeStmt, err := conn.PrepareContext(ctx,
			`INSERT INTO nodes (repo_key, path, sha256, size, mime, created_by, created_at, updated_at)
			 VALUES (?, ?, ?, 1024, 'application/octet-stream', 'admin', ?, ?)`)
		if err != nil {
			return err
		}
		for i := 0; i < totalNodes; i++ {
			name := fmt.Sprintf("hay-%06d/app.bin", i)
			if i%(totalNodes/needles) == 0 {
				name = fmt.Sprintf("needle-%06d/needle.bin", i)
			}
			sum := sha256.Sum256([]byte(fmt.Sprintf("perf-blob-%d", i%200)))
			if _, err := nodeStmt.ExecContext(ctx,
				fmt.Sprintf("perf-%d", i%5),
				fmt.Sprintf("group-%03d/%s", i%500, name),
				hex.EncodeToString(sum[:]), now, now); err != nil {
				return err
			}
		}
		if err := nodeStmt.Close(); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			return err
		}
		commit = true
		return nil
	})
	if err != nil {
		t.Fatalf("seed 100k nodes: %v", err)
	}

	// Warm one query (page cache, statement plan), then measure.
	warm := h.do(http.MethodGet, "/binflow/api/search/artifact?name=needle", adminUser, adminPass, nil, nil)
	if warm.StatusCode != http.StatusOK {
		t.Fatalf("warm-up status = %d", warm.StatusCode)
	}
	if body := decodeSearch(t, warm); len(body.Results) != needles {
		t.Fatalf("needle hits = %d, want %d", len(body.Results), needles)
	}

	durations := make([]time.Duration, 0, 20)
	for i := 0; i < 20; i++ {
		start := time.Now()
		resp := h.do(http.MethodGet, "/binflow/api/search/artifact?name=needle", adminUser, adminPass, nil, nil)
		elapsed := time.Since(start)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("iteration %d status = %d", i, resp.StatusCode)
		}
		durations = append(durations, elapsed)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[len(durations)*95/100]
	t.Logf("admin name search over %d nodes: p50=%s p95=%s (n=%d)",
		totalNodes, durations[len(durations)/2], p95, len(durations))
	if p95 >= time.Second {
		t.Fatalf("p95 = %s, want < 1s (NFR-P17)", p95)
	}
}
