// BIN-71 / T-589 — the originalChecksums KEYSET model on the /api/storage
// FileInfo face (the ledger's httpapi/original-checksums-key-model, closed
// by the single-source repo.OriginalChecksums): keyset = the
// client-REGISTERED algorithms ∪ {sha256}. T-584's five-leg live pinned
// the reference (only-md5-registered → no sha1 key; nothing registered →
// {sha256} alone), L041's m1/wsrv legs cross-confirmed, and the 409
// write-through leg (L037 Arm 1) keeps a registered WRONG value verbatim.
package httpapi_test

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestFileInfoOriginalChecksumsKeyset walks the declared-subset table on
// the FileInfo GET face: PUT with a correct declared subset registers it,
// and the FileInfo render answers EXACTLY client∪{sha256} — no full-triple
// fallback (the B-side bug this closes), never an empty set.
func TestFileInfoOriginalChecksumsKeyset(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	sha256HexOf := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	sha1HexOf := func(b []byte) string { s := sha1.Sum(b); return hex.EncodeToString(s[:]) }
	md5HexOf := func(b []byte) string { m := md5.Sum(b); return hex.EncodeToString(m[:]) } //nolint:gosec // wire-compat digest fixture

	cases := []struct {
		name     string
		hdr      map[string]string // the declared X-Checksum-* headers (correct values)
		wantKeys []string          // the exact keyset FileInfo must answer
	}{
		{"no declaration = sha256 alone", nil, []string{"sha256"}},
		{"md5 only", map[string]string{"X-Checksum-Md5": "MD5"}, []string{"md5", "sha256"}},
		{"sha1 only", map[string]string{"X-Checksum-Sha1": "SHA1"}, []string{"sha1", "sha256"}},
		{"sha256 declared", map[string]string{"X-Checksum-Sha256": "SHA256"}, []string{"sha256"}},
		{"md5+sha1", map[string]string{"X-Checksum-Md5": "MD5", "X-Checksum-Sha1": "SHA1"}, []string{"md5", "sha1", "sha256"}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := []byte("keyset-leg-" + tc.name)
			for k, v := range tc.hdr {
				switch v {
				case "MD5":
					tc.hdr[k] = md5HexOf(content)
				case "SHA1":
					tc.hdr[k] = sha1HexOf(content)
				case "SHA256":
					tc.hdr[k] = sha256HexOf(content)
				}
			}
			path := "/binflow/generic-local/ks/leg" + string(rune('a'+i)) + ".bin"
			if resp := h.do(http.MethodPut, path, adminUser, adminPass, content, tc.hdr); resp.StatusCode != http.StatusCreated {
				t.Fatalf("PUT = %d; body=%s", resp.StatusCode, mustGet(t, resp))
			}
			resp := h.do(http.MethodGet, "/binflow/api/storage"+strings.TrimPrefix(path, "/binflow"),
				adminUser, adminPass, nil, nil)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("FileInfo = %d; body=%s", resp.StatusCode, mustGet(t, resp))
			}
			var doc struct {
				Checksums         map[string]string `json:"checksums"`
				OriginalChecksums map[string]string `json:"originalChecksums"`
			}
			if err := json.Unmarshal([]byte(mustGet(t, resp)), &doc); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(doc.OriginalChecksums) != len(tc.wantKeys) {
				t.Fatalf("oc keyset = %v (len %d), want exactly %v", doc.OriginalChecksums, len(doc.OriginalChecksums), tc.wantKeys)
			}
			for _, k := range tc.wantKeys {
				v, ok := doc.OriginalChecksums[k]
				if !ok {
					t.Fatalf("oc key %q missing; got %v", k, doc.OriginalChecksums)
				}
				if want := doc.Checksums[k]; v != want {
					// correct declared == computed on these legs; the point
					// is the KEYSET, values ride the single source.
					t.Errorf("oc[%q] = %q, want %q", k, v, want)
				}
			}
		})
	}

	// Zero-placeholder write-through: a WRONG (all-zero) registered md5
	// survives verbatim (L041's srvgen evidence — registration, not
	// correctness, decides), and sha256 alone fills the rest — NO sha1
	// backfill key (L041 #8a's residual, the face this ticket closes).
	content := []byte("zero-placeholder-body")
	if resp := h.do(http.MethodPut, "/binflow/generic-local/ks/src.bin",
		adminUser, adminPass, content, nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d; body=%s", resp.StatusCode, mustGet(t, resp))
	}
	zeroMd5 := strings.Repeat("0", 32)
	if resp := h.do(http.MethodPut, "/binflow/generic-local/ks/src.bin.md5",
		adminUser, adminPass, []byte(zeroMd5), nil); resp.StatusCode != http.StatusConflict {
		t.Fatalf("zero-value checksum PUT = %d, want 409; body=%s", resp.StatusCode, mustGet(t, resp))
	}
	resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/ks/src.bin", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("FileInfo = %d; body=%s", resp.StatusCode, mustGet(t, resp))
	}
	// Fresh decode target per leg: unmarshaling into an existing map MERGES,
	// a stale md5 key from the previous leg would survive the omitempty.
	var wt struct {
		Checksums         map[string]string `json:"checksums"`
		OriginalChecksums map[string]string `json:"originalChecksums"`
	}
	if err := json.Unmarshal([]byte(mustGet(t, resp)), &wt); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(wt.OriginalChecksums) != 2 {
		t.Fatalf("write-through oc keyset = %v, want exactly {md5, sha256}", wt.OriginalChecksums)
	}
	if wt.OriginalChecksums["md5"] != zeroMd5 {
		t.Errorf("write-through oc[md5] = %q, want the zero placeholder verbatim", wt.OriginalChecksums["md5"])
	}
	if wt.OriginalChecksums["sha256"] != wt.Checksums["sha256"] {
		t.Errorf("write-through oc[sha256] = %q, want computed %q", wt.OriginalChecksums["sha256"], wt.Checksums["sha256"])
	}

	// Re-deploy with NO declaration clears the registration (the service's
	// wholesale column replace): the FileInfo face must return to
	// {sha256} — the empty-set regression leg in reverse.
	if resp := h.do(http.MethodPut, "/binflow/generic-local/ks/src.bin",
		adminUser, adminPass, []byte("re-deployed-body"), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("re-deploy = %d; body=%s", resp.StatusCode, mustGet(t, resp))
	}
	resp = h.do(http.MethodGet, "/binflow/api/storage/generic-local/ks/src.bin", adminUser, adminPass, nil, nil)
	var after struct {
		Checksums         map[string]string `json:"checksums"`
		OriginalChecksums map[string]string `json:"originalChecksums"`
	}
	if err := json.Unmarshal([]byte(mustGet(t, resp)), &after); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(after.OriginalChecksums) != 1 || after.OriginalChecksums["sha256"] == "" {
		t.Fatalf("after re-deploy oc = %v, want {sha256: computed} alone", after.OriginalChecksums)
	}
	if after.OriginalChecksums["sha256"] != sha256HexOf([]byte("re-deployed-body")) {
		t.Errorf("after re-deploy oc[sha256] = %q, want the new computed value", after.OriginalChecksums["sha256"])
	}
}
