package httpapi_test

// L024-3A: GET /api/search/badChecksum (aql.md §16.5-R13, D03-R13): the two
// live-verbatim 400 copies, the admin-only gate, and the corruption hit row
// — seeded by overwriting a blob's bytes under the storage engine itself, so
// the audit compares a registered ledger digest against genuinely different
// content.

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/storage"
)

func TestSearchBadChecksum(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"carol", "carol-pass"}})
	seedRepo(t, h, "bad-local")
	// The clean corpus: two intact artifacts.
	deposit(t, h, "bad-local", "fine/one.bin", "clean-bytes-one")
	sha := deposit(t, h, "bad-local", "fine/two.bin", "clean-bytes-two")

	// The corruption: replace the second blob's content on disk with
	// different bytes — the registered ledger digests now describe content
	// that no longer exists at that address.
	corrupted := []byte("mutated-content-bytes")
	blobPath, err := storage.BlobPath(h.dataDir, sha)
	if err != nil {
		t.Fatalf("blob path: %v", err)
	}
	if err := os.WriteFile(blobPath, corrupted, 0o600); err != nil {
		t.Fatalf("corrupt blob: %v", err)
	}
	sum := md5.Sum(corrupted)
	corruptMD5 := hex.EncodeToString(sum[:])
	sum256 := sha256.Sum256(corrupted)
	corruptSHA256 := hex.EncodeToString(sum256[:])

	t.Run("missing type answers the verbatim 400", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/search/badChecksum", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d\n%s", resp.StatusCode, mustGet(t, resp))
		}
		if body := mustGet(t, resp); !strings.Contains(body, "No checksum type defined") {
			t.Fatalf("body must carry the verbatim copy:\n%s", body)
		}
	})

	t.Run("unknown type answers the formatted 400", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/search/badChecksum?type=sha512", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d\n%s", resp.StatusCode, mustGet(t, resp))
		}
		if body := mustGet(t, resp); !strings.Contains(body, "Checksum type: sha512 is not defined") {
			t.Fatalf("body must carry the verbatim copy:\n%s", body)
		}
	})

	t.Run("non-admin callers are refused", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/search/badChecksum?type=md5", "carol", "carol-pass", nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403\n%s", resp.StatusCode, mustGet(t, resp))
		}
	})

	t.Run("anonymous callers are challenged", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/search/badChecksum?type=md5", "", "", nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("md5 audit reports the corrupted blob with both digests", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/search/badChecksum?type=md5&repos=bad-local", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d\n%s", resp.StatusCode, mustGet(t, resp))
		}
		var parsed struct {
			Results []struct {
				URI       string `json:"uri"`
				Checksums map[string]struct {
					Expected string `json:"expected"`
					Actual   string `json:"actual"`
				} `json:"checksums"`
			} `json:"results"`
		}
		page := mustGet(t, resp)
		if err := json.Unmarshal([]byte(page), &parsed); err != nil {
			t.Fatalf("body is not JSON: %v\n%s", err, page)
		}
		if len(parsed.Results) != 1 {
			t.Fatalf("results = %d, want the one corrupted row:\n%s", len(parsed.Results), page)
		}
		row := parsed.Results[0]
		if !strings.HasSuffix(row.URI, "/api/storage/bad-local/fine/two.bin") {
			t.Fatalf("uri = %q", row.URI)
		}
		pair, ok := row.Checksums["md5"]
		if !ok {
			t.Fatalf("md5 pair missing:\n%s", page)
		}
		if pair.Actual != corruptMD5 {
			t.Fatalf("actual md5 = %q, want the mutated content's %q", pair.Actual, corruptMD5)
		}
		if pair.Expected == "" || pair.Expected == pair.Actual {
			t.Fatalf("expected md5 = %q, want the registered ledger value", pair.Expected)
		}
	})

	t.Run("sha256 audit hits the same corruption", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/search/badChecksum?type=sha256&repos=bad-local", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d\n%s", resp.StatusCode, mustGet(t, resp))
		}
		page := mustGet(t, resp)
		if !strings.Contains(page, corruptSHA256) {
			t.Fatalf("body must carry the mutated content's sha256:\n%s", page)
		}
	})

	t.Run("a clean repository answers an empty results page", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/search/badChecksum?type=md5&repos=ghost-local", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d\n%s", resp.StatusCode, mustGet(t, resp))
		}
		var parsed struct {
			Results []json.RawMessage `json:"results"`
		}
		if err := json.Unmarshal([]byte(mustGet(t, resp)), &parsed); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}
		if len(parsed.Results) != 0 {
			t.Fatalf("results = %d, want 0", len(parsed.Results))
		}
	})
}
