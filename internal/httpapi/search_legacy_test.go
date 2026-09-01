package httpapi_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter/maven"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/search"
)

// The FR-134 legacy trio (T-417): gavc / prop / pattern through the real
// HTTP stack — the SR-03/SR-04 flip's positive half (the closed-404 rows
// they replace live in t92_search_test.go). Every test pins the shared
// conventions once and its own semantics after: the {"results":[FileInfo]}
// envelope, the 200-empty-array family (aql.md section 0-4), the E-01 400s,
// the same allow() ACL, and the K63 ceiling with its truncation header.

// seedTypedRepo creates a local repository of an explicit package type
// (seedRepo's typed sibling — the gavc leg needs a real maven repository).
func seedTypedRepo(t *testing.T, h *harness, key string, pkgType string) {
	t.Helper()
	admin := &repo.Principal{Name: adminUser, Admin: true}
	if _, err := h.svc.CreateRepo(context.Background(), admin, &metadata.Repo{
		RepoKey: key, Type: repo.TypeLocal, PackageType: pkgType,
	}); err != nil {
		t.Fatalf("CreateRepo %s: %v", key, err)
	}
}

// newMavenSearchStack mounts the REAL maven adapter beside the default two
// (cmd's assembly spelling): the gavc leg deploys through the layout-
// validating content plane, so the coordinates under test are the ones a
// real mvn deploy lands.
func newMavenSearchStack(t *testing.T) *harness {
	t.Helper()
	return newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		d.Adapters = append(d.Adapters,
			maven.New(d.ReposSvc, d.Metadata.Repos(), d.Metadata.Blobs(), d.Metadata.Nodes()))
	}, nil)
}

// TestSearchGavcMavenCoordinates is FR-134.1: real maven-layout deploys are
// found by coordinate (g+a, the v narrowing arm, the c narrowing arm, the
// repos filter), a miss answers the 200 empty array, and illegal
// coordinates answer the E-01 400.
func TestSearchGavcMavenCoordinates(t *testing.T) {
	h := newMavenSearchStack(t)
	seedTypedRepo(t, h, "maven-local", repo.PackageMaven)
	seedTypedRepo(t, h, "maven-other", repo.PackageMaven)

	deploy := []struct{ repo, path, body string }{
		{"maven-local", "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", "gavc-jar"},
		{"maven-local", "com/acme/demo-app/1.0.0/demo-app-1.0.0.pom", "gavc-pom"},
		{"maven-local", "com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar", "gavc-src"},
		{"maven-local", "com/acme/demo-app/2.0.0/demo-app-2.0.0.jar", "gavc-jar2"},
		{"maven-local", "com/acme/other-lib/1.0.0/other-lib-1.0.0.jar", "gavc-other"},
		{"maven-other", "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", "gavc-mirror"},
	}
	for _, d := range deploy {
		deposit(t, h, d.repo, d.path, d.body)
	}

	tests := []struct {
		query string
		want  []string
	}{
		{"?g=com.acme&a=demo-app", []string{
			"maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar",
			"maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar",
			"maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0.pom",
			"maven-local/com/acme/demo-app/2.0.0/demo-app-2.0.0.jar",
			"maven-other/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar",
		}},
		{"?g=com.acme&a=demo-app&v=1.0.0", []string{
			"maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar",
			"maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar",
			"maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0.pom",
			"maven-other/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar",
		}},
		{"?g=com.acme&a=demo-app&v=1.0.0&c=sources", []string{
			"maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar",
		}},
		{"?g=com.acme&a=demo-app&v=1.0.0&repos=maven-local", []string{
			"maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar",
			"maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar",
			"maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0.pom",
		}},
		{"?g=com.acme&a=other-lib", []string{
			"maven-local/com/acme/other-lib/1.0.0/other-lib-1.0.0.jar",
		}},
		{"?g=org.nobody&a=nowhere", nil},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			resp := h.do(http.MethodGet, "/binflow/api/search/gavc"+tt.query, adminUser, adminPass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
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

	// The E-09 envelope rides the family's shared shape: uri/downloadUri
	// address the storage plane, the checksums carry the ledger triple.
	resp := h.do(http.MethodGet, "/binflow/api/search/gavc?g=com.acme&a=demo-app&v=1.0.0&c=sources",
		adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	body := decodeSearch(t, resp)
	if len(body.Results) != 1 {
		t.Fatalf("classifier arm results = %d, want 1", len(body.Results))
	}
	hit := body.Results[0]
	if hit.URI != h.srv.URL+"/binflow/api/storage/maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar" {
		t.Fatalf("uri = %q", hit.URI)
	}
	if hit.DownloadURI != hit.URI || hit.Repo != "maven-local" || hit.MimeType == "" || hit.Size == "" {
		t.Fatalf("FileInfo shape degraded: %+v", hit)
	}

	// Illegal coordinates: the E-01 400 family (the layout-model subset).
	for _, q := range []string{
		"",              // no coordinate at all
		"?g=%20%20",     // blank-only coordinates
		"?g=com..acme",  // empty group segment
		"?g=.com",       // leading empty segment
		"?g=com/acme",   // slash inside a group
		"?a=demo/app",   // slash inside an artifactId
		"?v=1.0/2",      // slash inside a version
		"?c=sources/x",  // slash inside a classifier
		"?a=demo%01app", // control byte
	} {
		resp := h.do(http.MethodGet, "/binflow/api/search/gavc"+q, adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("query %q status = %d, want 400", q, resp.StatusCode)
		}
		decodeError(t, resp)
	}
}

// TestSearchPropIndex is FR-134.2: the M10 property index answers the prop
// search — the documented props= form, the official any-parameter form, the
// key-without-value arm, the repos filter, and the honest empty page for
// unknown keys.
func TestSearchPropIndex(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	seedRepo(t, h, "other-local")
	deposit(t, h, "generic-local", "acme/artifact.bin", "prop-a")
	deposit(t, h, "generic-local", "acme/other.bin", "prop-b")
	deposit(t, h, "generic-local", "acme/bare.bin", "prop-c")
	deposit(t, h, "other-local", "acme/mirror.bin", "prop-d")

	propPut := func(repoKey, path, props string) {
		t.Helper()
		resp := h.do(http.MethodPut, "/binflow/api/storage/"+repoKey+"/"+path+"?properties="+props,
			adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("properties PUT %s %s: status %d", repoKey, path, resp.StatusCode)
		}
	}
	propPut("generic-local", "acme/artifact.bin", "license=Apache-2.0")
	propPut("generic-local", "acme/artifact.bin", "build.name=demo")
	propPut("generic-local", "acme/other.bin", "license=MIT")
	propPut("generic-local", "acme/bare.bin", "stage=gold")
	propPut("other-local", "acme/mirror.bin", "license=Apache-2.0")

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"documented props=key=value form", "?props=license=Apache-2.0", []string{
			"generic-local/acme/artifact.bin", "other-local/acme/mirror.bin",
		}},
		{"key without value matches any value", "?props=license", []string{
			"generic-local/acme/artifact.bin", "generic-local/acme/other.bin",
			"other-local/acme/mirror.bin",
		}},
		{"official any-parameter form", "?build.name=demo", []string{
			"generic-local/acme/artifact.bin",
		}},
		{"official form without a value", "?stage", []string{
			"generic-local/acme/bare.bin",
		}},
		{"constraints AND across spellings", "?props=license=Apache-2.0&build.name=demo", []string{
			"generic-local/acme/artifact.bin",
		}},
		{"repos filter narrows", "?props=license=Apache-2.0&repos=other-local", []string{
			"other-local/acme/mirror.bin",
		}},
		{"unknown key is the honest empty page", "?props=ghost", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.do(http.MethodGet, "/binflow/api/search/prop"+tt.query, adminUser, adminPass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
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

	// No constraint at all, and a key the M10 grammar could never store:
	// both are the E-01 400, never a silently-widened match.
	for _, q := range []string{"", "?repos=generic-local", "?props=1bad", "?props=&stage=x-y"} {
		resp := h.do(http.MethodGet, "/binflow/api/search/prop"+q, adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("query %q status = %d, want 400", q, resp.StatusCode)
		}
		decodeError(t, resp)
	}
}

// TestSearchPatternGlob is FR-134.3: the pattern arm through the shared
// wildcard kernel — the literal head, the ** tree arm (with its shared-
// kernel depth floor pinned: `**` is SQL %, one star, not Ant's
// zero-or-more directories), the cross-repository repo glob, and the E-01
// 400s for a value that is not <repo>:<path>.
func TestSearchPatternGlob(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	seedRepo(t, h, "other-local")
	for _, d := range []struct{ repo, path string }{
		{"generic-local", "com/acme/lib/a.jar"},
		{"generic-local", "com/acme/lib/deep/b.jar"},
		{"generic-local", "com/acme/root.jar"},
		{"generic-local", "com/acme/lib/c.pom"},
		{"other-local", "com/acme/lib/a.jar"},
	} {
		deposit(t, h, d.repo, d.path, "pattern-bytes")
	}

	tests := []struct {
		pattern string
		want    []string
	}{
		{"generic-local:com/acme/root.jar", []string{"generic-local/com/acme/root.jar"}},
		// The ticket's own example shape: the tree under a directory (every
		// .jar at depth below com/acme/).
		{"generic-local:com/acme/**/*.jar", []string{
			"generic-local/com/acme/lib/a.jar",
			"generic-local/com/acme/lib/deep/b.jar",
		}},
		// Shared-kernel depth floor pinned: `**` is SQL % (one star), NOT
		// Ant's zero-or-more directories — lib/**/*.jar demands one more
		// slash below lib/, so the directly-contained a.jar falls outside.
		{"generic-local:com/acme/lib/**/*.jar", []string{
			"generic-local/com/acme/lib/deep/b.jar",
		}},
		// Shared-kernel semantics pinned: one '*' already crosses segments
		// (SQL %, the AQL $match reading) — the registered divergence from
		// Ant globbing (aql.md section 8.2's alignment note).
		{"generic-local:com/acme/*.jar", []string{
			"generic-local/com/acme/lib/a.jar",
			"generic-local/com/acme/lib/deep/b.jar",
			"generic-local/com/acme/root.jar",
		}},
		{"generic-local:com/acme/lib/*.pom", []string{"generic-local/com/acme/lib/c.pom"}},
		// The cross-repository wildcard arm.
		{"*-local:com/acme/lib/a.jar", []string{
			"generic-local/com/acme/lib/a.jar",
			"other-local/com/acme/lib/a.jar",
		}},
		{"generic-local:org/nowhere/**", nil},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			resp := h.do(http.MethodGet, "/binflow/api/search/pattern?pattern="+tt.pattern,
				adminUser, adminPass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
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

	for _, q := range []string{
		"",                         // missing
		"?pattern=",                // empty
		"?pattern=com/acme/**.jar", // no <repo>: separator
		"?pattern=:com/acme/**",    // empty repo half
		"?pattern=generic-local:",  // empty path half
	} {
		resp := h.do(http.MethodGet, "/binflow/api/search/pattern"+q, adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("query %q status = %d, want 400", q, resp.StatusCode)
		}
		decodeError(t, resp)
	}
}

// TestSearchLegacyACLZeroLeak is FR-134.4's zero-leak leg (the t92 probe's
// shape over the three new entrances): on a closed instance ci-bot — read-
// granted on the com/acme/ci-out/** arm of both repositories only — never
// sees a row outside the grant on any of the three, anonymous callers meet
// the 403, and admin sees everything.
func TestSearchLegacyACLZeroLeak(t *testing.T) {
	h := newHarnessCfg(t, func(c *config.Config) { c.Security.AnonymousAccess = false },
		[][2]string{{"ci-bot", "ci-pw"}})
	seedRepo(t, h, "generic-local")
	seedRepo(t, h, "other-local")
	grant(t, h, "g1", "generic-local", "com/acme/ci-out/**", "ci-bot", true, false, false)
	grant(t, h, "g2", "other-local", "com/acme/ci-out/**", "ci-bot", true, false, false)

	deposit(t, h, "generic-local", "com/acme/ci-out/app/1.0/app-1.0.jar", "vis-pub")
	deposit(t, h, "generic-local", "com/acme/hidden/app/1.0/app-1.0.jar", "vis-secret")
	deposit(t, h, "other-local", "com/acme/ci-out/app/1.0/app-1.0.jar", "vis-pub2")
	for _, p := range []struct{ repo, path string }{
		{"generic-local", "com/acme/ci-out/app/1.0/app-1.0.jar"},
		{"generic-local", "com/acme/hidden/app/1.0/app-1.0.jar"},
		{"other-local", "com/acme/ci-out/app/1.0/app-1.0.jar"},
	} {
		resp := h.do(http.MethodPut, "/binflow/api/storage/"+p.repo+"/"+p.path+"?properties=license=Apache-2.0",
			adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("properties PUT %s: status %d", p.path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	want := []string{"generic-local/com/acme/ci-out/app/1.0/app-1.0.jar",
		"other-local/com/acme/ci-out/app/1.0/app-1.0.jar"}
	for _, tt := range []struct {
		name   string
		target string
	}{
		// The gavc probe rides the module directory at any depth (the
		// fixture's module sits under the granted com/acme/ci-out arm, so a
		// group-anchored probe could never see both arms).
		{"gavc", "/binflow/api/search/gavc?a=app"},
		{"prop", "/binflow/api/search/prop?props=license"},
		{"pattern", "/binflow/api/search/pattern?pattern=*-local:com/acme/**"},
	} {
		t.Run("ci-bot sees only the granted arm on "+tt.name, func(t *testing.T) {
			resp := h.do(http.MethodGet, tt.target, "ci-bot", "ci-pw", nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			got := searchHits(decodeSearch(t, resp))
			if len(got) != len(want) {
				t.Fatalf("ci-bot %s hits = %v, want %v (zero leak of the hidden arm)", tt.name, got, want)
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("ci-bot %s hits = %v, want %v", tt.name, got, want)
				}
			}
		})
	}

	t.Run("admin sees every arm", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/search/gavc?a=app", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if got := searchHits(decodeSearch(t, resp)); len(got) != 3 {
			t.Fatalf("admin hits = %v, want all three", got)
		}
	})

	t.Run("anonymous on the closed instance meets 403", func(t *testing.T) {
		for _, target := range []string{
			"/binflow/api/search/gavc?g=com.acme",
			"/binflow/api/search/prop?props=license",
			"/binflow/api/search/pattern?pattern=generic-local:com/acme/**",
		} {
			resp := h.do(http.MethodGet, target, "", "", nil, nil)
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("%s anonymous status = %d, want 403", target, resp.StatusCode)
			}
			decodeError(t, resp)
		}
	})
}

// TestSearchLegacyTruncationCap is FR-134.4's K63 leg: the legacy trio
// shares AQL's 1,000-row ceiling and truncation header — a matching set
// beyond the cap answers exactly the cap plus the header, a set under it
// answers everything with no header.
func TestSearchLegacyTruncationCap(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "cap-local")

	raw := h.md.(interface {
		RawConn(ctx context.Context, fn func(*sql.Conn) error) error
	})
	ctx := context.Background()
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
		now := "2026-09-01T00:00:00Z"
		sum := sha256.Sum256([]byte("cap-blob"))
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO blobs (sha256, sha1, md5, size, created_at) VALUES (?, ?, ?, 9, ?)`,
			hex.EncodeToString(sum[:]), strings.Repeat("1", 40), strings.Repeat("2", 32), now); err != nil {
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
				hex.EncodeToString(sum[:]), now, now); err != nil {
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

	resp := h.do(http.MethodGet, "/binflow/api/search/pattern?pattern=cap-local:bulk/*",
		adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get(search.TruncatedHeader); got != "true" {
		t.Fatalf("truncation header = %q, want \"true\"", got)
	}
	if body := decodeSearch(t, resp); len(body.Results) != search.ResultCap {
		t.Fatalf("results = %d, want exactly the %d cap", len(body.Results), search.ResultCap)
	}

	// Under the cap: everything, and no header.
	resp = h.do(http.MethodGet, "/binflow/api/search/pattern?pattern=cap-local:bulk/item-0000*.bin",
		adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("narrow status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get(search.TruncatedHeader); got != "" {
		t.Fatalf("truncation header = %q under the cap, want unset", got)
	}
	if body := decodeSearch(t, resp); len(body.Results) != 10 {
		t.Fatalf("narrow results = %d, want the 10 item-0000* rows", len(body.Results))
	}
}

// TestSearchArtifactNameCaseInsensitiveK64 pins the artifact entrance's K64
// calibration (aql.md section 0-5, executed by this ticket): the name
// fragment matches case-insensitively in both directions, while the
// fragment's wildcard bytes stay literal.
func TestSearchArtifactNameCaseInsensitiveK64(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	deposit(t, h, "generic-local", "acme/artifact.bin", "k64-bytes")
	deposit(t, h, "generic-local", "acme/ARTIFACT.bin", "k64-twin")

	for _, frag := range []string{"ARTIFACT", "artifact.BIN", "Artifact"} {
		resp := h.do(http.MethodGet, "/binflow/api/search/artifact?name="+frag, adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("name=%q status = %d, want 200", frag, resp.StatusCode)
		}
		if got := searchHits(decodeSearch(t, resp)); len(got) != 2 {
			t.Fatalf("name=%q hits = %v, want both case twins", frag, got)
		}
		_ = resp.Body.Close()
	}

	// A fragment nobody holds case-folds onto nothing: the empty array.
	resp := h.do(http.MethodGet, "/binflow/api/search/artifact?name=NOTHING-MATCHES", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if body := decodeSearch(t, resp); body.Results == nil || len(body.Results) != 0 {
		t.Fatalf("miss results = %#v, want []", body.Results)
	}
}

// TestSearchLegacyMetrics pins the plane label's expansion (T-415's
// preseed note): the legacy trio counts on the family's query counter and
// duration histogram beside the aql plane.
func TestSearchLegacyMetrics(t *testing.T) {
	h := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		d.Metrics = metrics.NewRegistry()
	}, nil)
	seedRepo(t, h, "generic-local")
	deposit(t, h, "generic-local", "acme/artifact.bin", "metrics-bytes")

	resp := h.do(http.MethodGet, "/binflow/api/search/gavc?g=com.acme", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("gavc status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	m := h.do(http.MethodGet, "/metrics", "", "", nil, nil)
	defer func() { _ = m.Body.Close() }()
	if m.StatusCode != http.StatusOK {
		t.Fatalf("metrics status = %d", m.StatusCode)
	}
	body := mustGet(t, m)
	for _, want := range []string{
		`binflow_search_queries_total{plane="legacy"} 1`,
		`binflow_search_queries_total{plane="aql"} 0`,
		"binflow_search_query_duration_seconds_count 1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q\nbody:\n%s", want, body)
		}
	}
}
