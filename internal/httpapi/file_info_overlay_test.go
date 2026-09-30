// T-578 / BIN-60 — the FileInfo face's originalChecksums overlay
// (ADR-0052 decision 4): the no-upload-context render goes through
// repo.OriginalChecksums, so a stored client declaration wins per
// algorithm (the 409 write-through's WRONG value included, L037 Arm 1)
// and the server triple fills every unregistered slot (T-574's live
// no-declaration finding: the reference fills originalChecksums with the
// computed values there).
package httpapi_test

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestFileInfoOriginalChecksumsOverlay walks the three states of the
// overlay on /api/storage FileInfo: unset (computed triple), partial
// registration (per-algorithm mix), and the 409 write-through (the wrong
// stored value echoes verbatim).
func TestFileInfoOriginalChecksumsOverlay(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	content := []byte("overlay-body")
	sha1S := hex.EncodeToString(sha1Sum(content))
	md5S := hex.EncodeToString(md5Sum(content))
	if resp := h.do(http.MethodPut, "/binflow/generic-local/ov/artifact.bin",
		adminUser, adminPass, content, nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d; body=%s", resp.StatusCode, mustGet(t, resp))
	}
	get := func(t *testing.T) map[string]map[string]string {
		t.Helper()
		resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/ov/artifact.bin",
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
		return map[string]map[string]string{"checksums": doc.Checksums, "original": doc.OriginalChecksums}
	}

	// State 1 — nothing registered: originalChecksums mirrors the computed
	// triple (the no-declaration leg's live shape).
	got := get(t)
	for algo, want := range got["checksums"] {
		if got["original"][algo] != want {
			t.Errorf("unset %s: original = %q, want computed %q", algo, got["original"][algo], want)
		}
	}

	// State 2 — the 409 write-through: a WRONG registered md5 echoes
	// verbatim while sha1/sha256 keep the computed values.
	wrongMd5 := strings.Repeat("0", 32)
	resp := h.do(http.MethodPut, "/binflow/generic-local/ov/artifact.bin.md5",
		adminUser, adminPass, []byte(wrongMd5), nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("wrong-value checksum PUT = %d, want 409; body=%s", resp.StatusCode, mustGet(t, resp))
	}
	got = get(t)
	if got["original"]["md5"] != wrongMd5 {
		t.Errorf("after 409 md5: original = %q, want the written-through %q", got["original"]["md5"], wrongMd5)
	}
	if got["original"]["sha1"] != sha1S {
		t.Errorf("after 409 md5: sha1 original = %q, want computed %q", got["original"]["sha1"], sha1S)
	}

	// State 3 — a correct registration replaces the wrong value.
	resp = h.do(http.MethodPut, "/binflow/generic-local/ov/artifact.bin.md5",
		adminUser, adminPass, []byte(md5S), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("correct-value checksum PUT = %d; body=%s", resp.StatusCode, mustGet(t, resp))
	}
	got = get(t)
	if got["original"]["md5"] != md5S {
		t.Errorf("after re-PUT: md5 original = %q, want %q", got["original"]["md5"], md5S)
	}
}

func sha1Sum(b []byte) []byte { s := sha1.Sum(b); return s[:] }
func md5Sum(b []byte) []byte  { m := md5.Sum(b); return m[:] }
