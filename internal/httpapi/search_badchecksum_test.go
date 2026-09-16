package httpapi_test

// GET /api/search/badChecksum (aql.md §16.5-R13 as the L024-4 differential
// closed it, diff L4): the two live-verbatim 400 copies, the admin-only
// gate, and the DB-side client-vs-server comparison — a MISSING declaration
// is bad; a declared digest equal to the registered one is NOT, even when
// the stored bytes were physically corrupted afterwards (the reference
// never rescans bytes — the four-point live proof).

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
	// The corpus: one undeclared artifact (bad by the missing-client arm),
	// one artifact whose md5 the deploy DECLARED (the declaration is
	// persisted on the node row), whose blob is then physically corrupted
	// — the reference's own posture leaves it UNflagged (client == server).
	deposit(t, h, "bad-local", "fine/one.bin", "clean-bytes-one")
	sum1 := md5.Sum([]byte("clean-bytes-two"))
	declaredMD5 := hex.EncodeToString(sum1[:])
	sum1s := sha256.Sum256([]byte("clean-bytes-two"))
	declaredSHA256 := hex.EncodeToString(sum1s[:])
	resp0 := h.do(http.MethodPut, "/binflow/bad-local/fine/two.bin", adminUser, adminPass,
		[]byte("clean-bytes-two"), map[string]string{
			"X-Checksum-Md5":    declaredMD5,
			"X-Checksum-Sha256": declaredSHA256,
		})
	_ = resp0.Body.Close()
	if resp0.StatusCode != http.StatusCreated {
		t.Fatalf("declared deposit: %d", resp0.StatusCode)
	}
	sha := declaredSHA256

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

	t.Run("md5 audit flags the undeclared row only, flat shape + limitReached", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/search/badChecksum?type=md5&repos=bad-local", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d\n%s", resp.StatusCode, mustGet(t, resp))
		}
		var parsed struct {
			Results []struct {
				URI       string `json:"uri"`
				ServerMd5 string `json:"serverMd5"`
				ClientMd5 string `json:"clientMd5"`
			} `json:"results"`
			LimitReached bool `json:"limitReached"`
		}
		page := mustGet(t, resp)
		if err := json.Unmarshal([]byte(page), &parsed); err != nil {
			t.Fatalf("body is not JSON: %v\n%s", err, page)
		}
		if len(parsed.Results) != 1 {
			t.Fatalf("results = %d, want exactly the undeclared row (the declared-but-corrupted one stays clean):\n%s", len(parsed.Results), page)
		}
		row := parsed.Results[0]
		if !strings.HasSuffix(row.URI, "/api/storage/bad-local/fine/one.bin") {
			t.Fatalf("uri = %q", row.URI)
		}
		if row.ServerMd5 == "" || row.ClientMd5 != "" {
			t.Fatalf("flat row = {server:%q, client:%q}, want the server value with an empty client", row.ServerMd5, row.ClientMd5)
		}
		if parsed.LimitReached {
			t.Fatal("limitReached must be false")
		}
	})

	t.Run("sha256 audit mirrors the arm", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/search/badChecksum?type=sha256&repos=bad-local", adminUser, adminPass, nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d\n%s", resp.StatusCode, mustGet(t, resp))
		}
		page := mustGet(t, resp)
		if !strings.Contains(page, "clientSha256") || strings.Contains(page, "two.bin") {
			t.Fatalf("the declared row must stay unflagged:\n%s", page)
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
