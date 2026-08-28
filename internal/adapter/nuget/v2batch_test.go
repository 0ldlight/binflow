package nuget

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The OData $batch face (nuget.md section 2 #13; the wire shape
// live-captured against nuget.org's own $batch, August 2026): 202 Accepted
// under the batchresponse_<guid> boundary, one application/http response
// part per sub-request, the query override, and the whole-batch 400 on an
// unsupported entry.

// writeBatchPart renders one batch part (the OData spelling carries
// Content-Type: application/http and the binary transfer encoding).
func writeBatchPart(w *multipart.Writer, requestLine string) {
	h := make(map[string][]string)
	h["Content-Type"] = []string{"application/http"}
	h["Content-Transfer-Encoding"] = []string{"binary"}
	part, err := w.CreatePart(h)
	if err != nil {
		panic(err)
	}
	_, _ = fmt.Fprintf(part, "GET %s HTTP/1.1\r\nHost: binflow\r\n\r\n", requestLine) //nolint:errcheck // test fixture writer into bytes.Buffer
}

func buildBatch(t *testing.T, lines ...string) (string, string) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for _, line := range lines {
		writeBatchPart(mw, line)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("batch close: %v", err)
	}
	return b.String(), "multipart/mixed; boundary=" + mw.Boundary()
}

func TestV2Batch(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	mustPushV2(t, s, "ng-local", buildNupkg(t, "Bat.Pkg", "1.0.0", flatDeps("none")))
	mustPushV2(t, s, "ng-local", buildNupkg(t, "Bat.Pkg", "1.1.0", flatDeps("none")))
	mustPushV2(t, s, "ng-local", buildNupkg(t, "Bat.Two", "2.0.0", flatDeps("none")))

	// A two-part batch: a $count and a Search — the parts carry their own
	// query strings (the override), and the response comes back as 202
	// multipart under the batchresponse_ boundary with per-part statuses.
	body, ctype := buildBatch(t,
		"/api/v2/ng-local/FindPackagesById()/$count?id='Bat.Pkg'",
		"/api/v2/ng-local/Search()?searchTerm='bat'",
	)
	status, respBody, hdr := s.do(http.MethodPost, apiV2Path("ng-local")+"/$batch", adminUser, adminPass, strings.NewReader(body), map[string]string{"Content-Type": ctype})
	if status != http.StatusAccepted {
		t.Fatalf("$batch = %d %s", status, respBody)
	}
	if ct := hdr.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/mixed; boundary=batchresponse_") {
		t.Errorf("$batch content-type = %q, want the batchresponse_ boundary", ct)
	}
	if v := hdr.Get("DataServiceVersion"); v == "" {
		t.Errorf("$batch carries no DataServiceVersion")
	}
	if !strings.Contains(respBody, "HTTP/1.1 200 OK") {
		t.Errorf("$batch body carries no part status:\n%.200s", respBody)
	}
	if !strings.Contains(respBody, "2") || !strings.Contains(respBody, "bat.pkg") {
		t.Errorf("$batch body misses the parts' answers:\n%.300s", respBody)
	}
	// Per-part failure isolation: a Search() with no hits answers its part
	// 404 without failing the batch.
	body, ctype = buildBatch(t, "/api/v2/ng-local/Search()?searchTerm='zzz'")
	status, respBody, _ = s.do(http.MethodPost, apiV2Path("ng-local")+"/$batch", adminUser, adminPass, strings.NewReader(body), map[string]string{"Content-Type": ctype})
	if status != http.StatusAccepted || !strings.Contains(respBody, "HTTP/1.1 404") {
		t.Errorf("part-level 404 batch = (%d, %.200s)", status, respBody)
	}

	// An unsupported entry fails the WHOLE batch with the exact wording.
	body, ctype = buildBatch(t, "/api/v2/ng-local/$metadata")
	status, respBody, _ = s.do(http.MethodPost, apiV2Path("ng-local")+"/$batch", adminUser, adminPass, strings.NewReader(body), map[string]string{"Content-Type": ctype})
	if status != http.StatusBadRequest || strings.TrimSpace(respBody) != "Unsupported batch entry." {
		t.Errorf("unsupported batch = (%d, %q)", status, firstLine(respBody))
	}

	// A non-multipart body is refused the same way.
	if status, _, _ := s.do(http.MethodPost, apiV2Path("ng-local")+"/$batch", adminUser, adminPass, strings.NewReader("junk"), map[string]string{"Content-Type": "text/plain"}); status != http.StatusBadRequest {
		t.Errorf("junk batch = %d, want 400", status)
	}

	// GET on $batch is not a v2 verb.
	if status, _, hdr := s.get(apiV2Path("ng-local") + "/$batch"); status != http.StatusMethodNotAllowed || !strings.Contains(hdr.Get("Allow"), "POST") {
		t.Errorf("GET $batch = %d", status)
	}
}
