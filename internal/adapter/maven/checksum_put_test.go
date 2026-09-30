// T-574 / BIN-56 landed the三面：源缺 404 文案逐字（A 形冒号分隔）、
// 值错 409 路径去 repo 前缀、终缀路由先于 layout 前置（无 GAV 路径
// .sha1 → 404 checksum 文案而非 400）。T-578 / BIN-60 在 ADR-0052 缝上
// 翻正写穿半面：409 臂照常落值（client 列 + sidecar GET 回显存值）、
// 重 PUT 覆盖、无存值 GET 保持计算值兜底（maven 的 fallback 姿态）。
// 权威口径：reports/compatibility/L037-probe-arms.md Arm 1（A 7.161.26
// 双轮）+ DECISIONS.md ADR-0052。
package maven

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// targetMissingMsg renders the A-form checksum miss wording (L037 Arm 1,
// 7.161.26 verbatim): colon-separated repo:source, no quotes.
func targetMissingMsg(repoKey, src string) string {
	return fmt.Sprintf("Target file to set checksum on doesn't exist: %s:%s", repoKey, src)
}

// TestChecksumPutSourceMissingWording pins the miss arm's exact wording on
// GAV-legitimate paths: {.sha1,.md5,.sha256} answer the checksum family's
// own 404; .sha512 is an ORDINARY file deploy since BIN-66 / T-584 (L039
// Arm 4: the reference accepts it as a plain storage item, missing source
// and all — GAV and non-GAV spellings alike).
func TestChecksumPutSourceMissingWording(t *testing.T) {
	hs := newHarness(t)
	gav := "com/diff/t574/1.0.0/t574-1.0.0.pom"
	for _, sfx := range []string{".sha1", ".md5", ".sha256"} {
		resp := hs.serve(http.MethodPut, "/maven-local/"+gav+sfx, []byte(strings.Repeat("ab", 20)), nil, true)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("PUT %s%s = %d, want 404", gav, sfx, resp.StatusCode)
		}
		if got := string(drain(t, resp)); !strings.Contains(got, targetMissingMsg("maven-local", gav)) {
			t.Fatalf("PUT %s%s 404 body = %s, want wording %q", gav, sfx, got, targetMissingMsg("maven-local", gav))
		}
	}

	// .sha512: an ordinary file deploy even with the source pom missing —
	// the 201 envelope's Location addresses the .sha512 path itself and the
	// GET serves the deployed bytes (no sidecar semantics at all).
	h512 := strings.Repeat("f", 128)
	resp := hs.serve(http.MethodPut, "/maven-local/"+gav+".sha512", []byte(h512), nil, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT %s.sha512 = %d, want 201 ordinary deploy (body=%s)",
			gav, resp.StatusCode, drain(t, resp))
	}
	if loc := resp.Header.Get("Location"); !strings.HasSuffix(loc, "/"+gav+".sha512") {
		t.Fatalf(".sha512 deploy Location = %q, want the file path itself", loc)
	}
	if got := string(drain(t, hs.serve(http.MethodGet, "/maven-local/"+gav+".sha512", nil, nil, true))); got != h512 {
		t.Fatalf("GET %s.sha512 = %q, want the deployed bytes", gav, got)
	}
}

// TestChecksumPutRoutingBeforeLayout pins the routing order: terminal
// {.sha1,.md5,.sha256} outranks the layout gate, so an un-GAV-able tail
// with a missing source answers the checksum family's 404 — never the 400
// layout refusal (L037 Arm 1: A's suffix routing runs first). A terminal
// .sha512 on an un-GAV-able path deploys as an ORDINARY file (BIN-66 /
// T-584, whitelist #8's non-GAV leg); the remaining non-family tails keep
// the layout refusal.
func TestChecksumPutRoutingBeforeLayout(t *testing.T) {
	hs := newHarness(t)
	for _, tc := range []struct {
		path   string
		source string
	}{
		{"foo/bar.txt.sha1", "foo/bar.txt"},
		{"foo/bar.txt.md5", "foo/bar.txt"},
		{"foo/bar.txt.sha256", "foo/bar.txt"},
		// GAV-directory stack but a non-template file name: still the
		// terminal suffix that routes.
		{"com/acme/lib/1.0.0/wrong-name.jar.sha1", "com/acme/lib/1.0.0/wrong-name.jar"},
	} {
		resp := hs.serve(http.MethodPut, "/maven-local/"+tc.path, []byte(strings.Repeat("ab", 20)), nil, true)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("PUT %s = %d, want 404 checksum-family miss", tc.path, resp.StatusCode)
		}
		if got := string(drain(t, resp)); !strings.Contains(got, targetMissingMsg("maven-local", tc.source)) {
			t.Fatalf("PUT %s body = %s, want wording %q", tc.path, got, targetMissingMsg("maven-local", tc.source))
		}
	}

	// Terminal .sha512 on an un-GAV-able path: ordinary file — 201
	// envelope, Location the file path itself, bytes served back, DELETE
	// 204 and the honest miss after (the A corner probe: 201/200/204/404).
	h512 := strings.Repeat("f", 128)
	resp := hs.serve(http.MethodPut, "/maven-local/foo/bar.txt.sha512", []byte(h512), nil, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT foo/bar.txt.sha512 = %d, want 201 ordinary deploy (body=%s)",
			resp.StatusCode, drain(t, resp))
	}
	if loc := resp.Header.Get("Location"); !strings.HasSuffix(loc, "/foo/bar.txt.sha512") {
		t.Fatalf(".sha512 Location = %q, want the file path itself", loc)
	}
	if got := string(drain(t, hs.serve(http.MethodGet, "/maven-local/foo/bar.txt.sha512", nil, nil, true))); got != h512 {
		t.Fatalf("GET foo/bar.txt.sha512 = %q, want the deployed bytes", got)
	}
	if resp := hs.serve(http.MethodDelete, "/maven-local/foo/bar.txt.sha512", nil, nil, true); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE foo/bar.txt.sha512 = %d, want 204", resp.StatusCode)
	}
	if resp := hs.serve(http.MethodGet, "/maven-local/foo/bar.txt.sha512", nil, nil, true); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after DELETE = %d, want 404", resp.StatusCode)
	}

	// Other non-family tails on un-GAV-able paths keep the 400 layout
	// refusal.
	for _, path := range []string{"foo/bar.txt.asc", "foo/bar.txt.sha1.bak"} {
		resp := hs.serve(http.MethodPut, "/maven-local/"+path, []byte("x"), nil, true)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("PUT %s = %d, want 400 layout refusal (outside the family)", path, resp.StatusCode)
		}
	}
}

// TestChecksumPutMismatchPath pins the 409 wording's path form: the
// reference quotes the PUT TARGET (checksum suffix included) WITHOUT the
// repository prefix (L037 Arm 1's live 409) — and, since the ADR-0052 seam
// (T-578 / BIN-60), the refused value WRITES THROUGH: the node's client
// column and the sidecar GET echo keep the wrong value (ledger
// maven/checksum-put-409-write-through, closed).
func TestChecksumPutMismatchPath(t *testing.T) {
	hs := newHarness(t)
	jar := "com/acme/t574c/2.0.0/t574c-2.0.0.jar"
	if resp := hs.deployJar("maven-local", jar, jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d", resp.StatusCode)
	}
	s1, _, _ := digests(jarBytes)
	side := "/maven-local/" + jar + ".sha1"
	wrong := strings.Repeat("0", 40)
	resp := hs.serve(http.MethodPut, side, []byte(wrong), nil, true)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("wrong-value sidecar = %d, want 409", resp.StatusCode)
	}
	want := fmt.Sprintf("Checksum error for '%s': received '%s' but actual is '%s'",
		jar+".sha1", wrong, s1)
	if got := string(drain(t, resp)); !strings.Contains(got, want) {
		t.Fatalf("409 body = %s\nwant fragment = %s", got, want)
	}
	// Write-through: the client column and the GET echo keep the WRONG
	// value (the 409 arm does not skip the registration).
	node, err := hs.md.Nodes().Get(context.Background(), "maven-local", jar)
	if err != nil {
		t.Fatalf("node after 409: %v", err)
	}
	if node.ClientSha1 != wrong {
		t.Errorf("node.ClientSha1 after 409 = %q, want the written-through %q", node.ClientSha1, wrong)
	}
	if got := string(drain(t, hs.serve(http.MethodGet, side, nil, nil, true))); got != wrong {
		t.Fatalf("sidecar GET after 409 = %q, want the written-through %q", got, wrong)
	}
	// A re-PUT with the correct value overwrites and renders 201 with the
	// TARGET's Location (the plain registration shape, L014-2 BUG 2).
	resp = hs.serve(http.MethodPut, side, []byte(s1), nil, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("correct-value sidecar = %d, want 201", resp.StatusCode)
	}
	if got := string(drain(t, hs.serve(http.MethodGet, side, nil, nil, true))); got != s1 {
		t.Fatalf("sidecar GET after re-PUT = %q, want %q", got, s1)
	}
}

// TestChecksumPutClientOverlayFallback pins the overlay's fallback posture
// (ADR-0052 decision 4): with NO stored client value the artifact sidecar
// GET answers the computed digest — maven's posture, unlike generic's 404.
func TestChecksumPutClientOverlayFallback(t *testing.T) {
	hs := newHarness(t)
	// Seed WITHOUT checksum headers: no client column is ever written.
	jar := "com/acme/t578f/1.0/t578f-1.0.jar"
	resp := hs.serve(http.MethodPut, "/maven-local/"+jar, jarBytes, nil, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d", resp.StatusCode)
	}
	s1, _, _ := digests(jarBytes)
	for _, algo := range []string{"sha1", "md5"} {
		if resp := hs.serve(http.MethodGet, "/maven-local/"+jar+"."+algo, nil, nil, true); resp.StatusCode != http.StatusOK {
			t.Fatalf("GET .%s = %d, want 200", algo, resp.StatusCode)
		} else if got := string(drain(t, resp)); got != map[string]string{"sha1": s1, "md5": digests2(jarBytes)}[algo] {
			t.Errorf("GET .%s = %q, want computed", algo, got)
		}
	}
	// Register one algorithm (correct value): the .sha1 face flips to the
	// stored client value while .md5 stays computed (per-algorithm overlay).
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar+".sha1", []byte(s1), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("sha1 registration = %d", resp.StatusCode)
	}
	if got := string(drain(t, hs.serve(http.MethodGet, "/maven-local/"+jar+".sha1", nil, nil, true))); got != s1 {
		t.Errorf("GET .sha1 after registration = %q, want %q", got, s1)
	}
	if got := string(drain(t, hs.serve(http.MethodGet, "/maven-local/"+jar+".md5", nil, nil, true))); got != digests2(jarBytes) {
		t.Errorf("GET .md5 after sibling registration = %q, want computed", got)
	}
}

// digests2 is digests' md5 arm alone (the overlay test spells both algos).
func digests2(b []byte) string {
	_, md5Hex, _ := digests(b)
	return md5Hex
}
