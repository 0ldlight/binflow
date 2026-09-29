// T-574 / BIN-56 — maven 域裸 checksum PUT 的可落地三面：源缺 404 文案
// 逐字（A 形冒号分隔）、值错 409 路径去 repo 前缀、终缀路由先于 layout
// 前置（无 GAV 路径 .sha1 → 404 checksum 文案而非 400）。409 写穿
// （originalChecksums 收客户端宣称值）依赖 client-checksum 持久化缝
// （repo.Service 缺口，票内登记移交）。权威口径：
// reports/compatibility/L037-probe-arms.md Arm 1（A 7.161.26 双轮）。
package maven

import (
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
// own 404; .sha512 keeps the pre-T-574 shape (outside the family, its A
// form unprobed — not guessed).
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

	// .sha512: outside the interception family — legacy miss shape stays.
	resp := hs.serve(http.MethodPut, "/maven-local/"+gav+".sha512", []byte(strings.Repeat("f", 128)), nil, true)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("PUT %s.sha512 = %d, want 404", gav, resp.StatusCode)
	}
	if got := string(drain(t, resp)); !strings.Contains(got,
		"Could not locate artifact. Path: 'maven-local/"+gav+"'.") {
		t.Fatalf(".sha512 miss body = %s, want the pre-T-574 wording", got)
	}
}

// TestChecksumPutRoutingBeforeLayout pins the routing order: terminal
// {.sha1,.md5,.sha256} outranks the layout gate, so an un-GAV-able tail
// with a missing source answers the checksum family's 404 — never the 400
// layout refusal (L037 Arm 1: A's suffix routing runs first). Non-family
// tails (.sha512/.asc/.sha1.bak) keep the layout refusal.
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

	// Non-family tails on un-GAV-able paths keep the 400 layout refusal.
	for _, path := range []string{"foo/bar.txt.sha512", "foo/bar.txt.asc", "foo/bar.txt.sha1.bak"} {
		resp := hs.serve(http.MethodPut, "/maven-local/"+path, []byte("x"), nil, true)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("PUT %s = %d, want 400 layout refusal (outside the family)", path, resp.StatusCode)
		}
	}
}

// TestChecksumPutMismatchPath pins the 409 wording's path form: the
// reference quotes the PUT TARGET (checksum suffix included) WITHOUT the
// repository prefix (L037 Arm 1's live 409). The value write-through of
// this leg is the blocked persistence seam (ledger
// maven/checksum-put-409-write-through) — nothing lands here.
func TestChecksumPutMismatchPath(t *testing.T) {
	hs := newHarness(t)
	jar := "com/acme/t574c/2.0.0/t574c-2.0.0.jar"
	if resp := hs.deployJar("maven-local", jar, jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d", resp.StatusCode)
	}
	s1, _, _ := digests(jarBytes)
	side := "/maven-local/" + jar + ".sha1"
	resp := hs.serve(http.MethodPut, side, []byte(strings.Repeat("0", 40)), nil, true)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("wrong-value sidecar = %d, want 409", resp.StatusCode)
	}
	want := fmt.Sprintf("Checksum error for '%s': received '%s' but actual is '%s'",
		jar+".sha1", strings.Repeat("0", 40), s1)
	if got := string(drain(t, resp)); !strings.Contains(got, want) {
		t.Fatalf("409 body = %s\nwant fragment = %s", got, want)
	}
	// Nothing landed and nothing was written through (pre-seam): the GET
	// keeps serving the computed digest — the write-through flip is the
	// blocked half.
	if got := string(drain(t, hs.serve(http.MethodGet, side, nil, nil, true))); got != s1 {
		t.Fatalf("sidecar GET after 409 = %q, want computed %q (pre-seam)", got, s1)
	}
}
