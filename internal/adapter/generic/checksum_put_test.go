// T-574 / BIN-56 landed the routing halves; T-578 / BIN-60 completes the
// model on the ADR-0052 client-checksum persistence seam: 终缀
// .sha1/.md5/.sha256 的 PUT 是源制品的 client-checksum 注册（源缺 404
// 逐字 + 无文件节点 + 阴性对照；源在场 201 CL=0/Location=源 + 值错 409
// 仍写穿 + GET 回显存值），GET 面回显已存 client 值、未存则族内 404。
// 权威口径：reports/compatibility/L037-probe-arms.md Arm 1（A 7.161.26 双轮）
// + DECISIONS.md ADR-0052。
package generic_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestChecksumPutSourceMissing pins the interception family's miss arm:
// any PUT whose path terminates in .sha1/.md5/.sha256 addresses the
// stripped source, and a missing source answers the reference's 404
// wording verbatim with NO sidecar node — content, source extension and
// source spelling are all irrelevant to the routing (L037 Arm 1 legs:
// lone.txt.{sha1,md5,sha256}, valid-hex body, noext).
func TestChecksumPutSourceMissing(t *testing.T) {
	e := newEnv(t)
	hex40 := strings.Repeat("ab", 20)
	cases := []struct {
		name    string
		path    string // under /binflow/generic-local/
		body    string
		wantSrc string
	}{
		{"sha1", "t574/lone.txt.sha1", hex40, "t574/lone.txt"},
		{"md5", "t574/lone.txt.md5", "0123456789abcdef0123456789abcdef", "t574/lone.txt"},
		{"sha256", "t574/lone.txt.sha256", strings.Repeat("cd", 32), "t574/lone.txt"},
		// Content-blind routing: a well-formed 40-hex body changes nothing.
		{"valid-hex body still routed", "t574/valid.bin.sha1", hex40, "t574/valid.bin"},
		// Source-extension-blind routing: an extension-less source name.
		{"extension-less source", "t574/noext.sha1", hex40, "t574/noext"},
		// Arbitrary (non-hex) body is equally irrelevant.
		{"garbage body still routed", "t574/lone.txt.sha1", "not a checksum at all", "t574/lone.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+tc.path,
				strings.NewReader(tc.body), nil)
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("PUT %s = %d, want 404 (body=%s)", tc.path, resp.StatusCode, body(t, resp))
			}
			want := fmt.Sprintf("Target file to set checksum on doesn't exist: generic-local:%s", tc.wantSrc)
			got := body(t, resp)
			// The envelope pretty-prints; the message is one JSON string
			// token, so pinning the quoted value pins it verbatim.
			if !strings.Contains(got, `"`+want+`"`) {
				t.Fatalf("404 body = %s\nwant message  = %q", got, want)
			}
		})
	}

	// No sidecar node materialized anywhere under the namespace (the
	// storage model is a metadata write, never a file deploy).
	nodes, err := e.svc.List(context.Background(), admin(), "generic-local", "t574")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(nodes) != 0 {
		for _, n := range nodes {
			t.Errorf("unexpected node: %s", n.Path)
		}
	}
	// And the GET of the sidecar path stays the honest miss (A: no
	// on-demand generation for an unset checksum) — with the family's own
	// wording, addressing the SOURCE artifact colon-separated.
	get := e.do(t, http.MethodGet, "/binflow/generic-local/t574/lone.txt.sha1", nil, nil)
	if get.StatusCode != http.StatusNotFound {
		t.Fatalf("GET unset sidecar = %d, want 404", get.StatusCode)
	}
	wantGet := "File not found.; Path: 'generic-local:t574/lone.txt'"
	if got := body(t, get); !strings.Contains(got, `"`+wantGet+`"`) {
		t.Errorf("GET unset 404 body = %s\nwant message = %q", got, wantGet)
	}
}

// TestChecksumPutFamilyNegatives pins what the interception family is
// NOT: .sha512, .asc and a compound .sha1.bak tail deploy as ordinary
// files (201 ItemCreated envelope, node lands, GET serves the bytes).
func TestChecksumPutFamilyNegatives(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{
		"t574n/lone.txt.sha512",
		"t574n/lone.txt.asc",
		"t574n/lone.txt.sha1.bak",
	} {
		resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+path,
			strings.NewReader("payload-for-"+path), nil)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("PUT %s = %d, want 201 ordinary deploy (body=%s)", path, resp.StatusCode, body(t, resp))
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "ItemCreated+json") {
			t.Errorf("PUT %s Content-Type = %q, want the ItemCreated envelope", path, ct)
		}
		get := e.do(t, http.MethodGet, "/binflow/generic-local/"+path, nil, nil)
		if get.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200 (ordinary file)", path, get.StatusCode)
		}
		if got := body(t, get); got != "payload-for-"+path {
			t.Errorf("GET %s body = %q", path, got)
		}
	}
}

// TestChecksumPutSourcePresent pins the source-present arms of the
// client-checksum registration (L037 Arm 1's A model, complete since the
// ADR-0052 seam): a correct value registers and answers 201 with an EMPTY
// body and Location addressing the SOURCE artifact (never the .sha1 path),
// no sidecar file node materializes (a registration is a metadata write);
// a wrong value answers 409 with the received/actual wording AND still
// writes through (the GET face echoes the wrong stored value); a re-PUT
// overwrites; the .md5/.sha256 arms behave identically per algorithm.
func TestChecksumPutSourcePresent(t *testing.T) {
	e := newEnv(t)
	src := "t574i/src.bin"
	if resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+src,
		strings.NewReader("source-bytes"), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("source seed = %d (%s)", resp.StatusCode, body(t, resp))
	}
	_, sha1S, md5S := digestsOf("source-bytes")
	sha256S := sha256Of(t, e, src)

	// --- correct value: 201, CL=0, Location = the SOURCE artifact ---
	loc := e.srv.URL + "/binflow/generic-local/" + src
	resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+src+".sha1",
		strings.NewReader(sha1S), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("correct-value PUT = %d (body=%s)", resp.StatusCode, body(t, resp))
	}
	if got := resp.Header.Get("Location"); got != loc {
		t.Errorf("Location = %q, want the source artifact %q", got, loc)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "0" {
		t.Errorf("Content-Length = %q, want 0 (empty body)", cl)
	}
	if got := body(t, resp); got != "" {
		t.Errorf("201 body = %q, want empty", got)
	}
	// No sidecar node materialized: the namespace holds the source alone.
	nodes, err := e.svc.List(context.Background(), admin(), "generic-local", "t574i")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// No sidecar node materialized: the namespace holds only the auto
	// folder marker and the source (del3's convergence — the count A shows).
	for _, n := range nodes {
		if strings.HasSuffix(n.Path, ".sha1") || strings.HasSuffix(n.Path, ".md5") || strings.HasSuffix(n.Path, ".sha256") {
			t.Errorf("unexpected sidecar node: %s", n.Path)
		}
	}
	if len(nodes) != 2 { // "t574i/" marker + src
		var paths []string
		for _, n := range nodes {
			paths = append(paths, n.Path)
		}
		t.Fatalf("nodes under t574i = %v, want exactly [t574i/ %s]", paths, src)
	}
	// GET echo: the stored client value, not a generated digest file.
	get := e.do(t, http.MethodGet, "/binflow/generic-local/"+src+".sha1", nil, nil)
	if get.StatusCode != http.StatusOK {
		t.Fatalf("GET echo = %d (body=%s)", get.StatusCode, body(t, get))
	}
	if ct := get.Header.Get("Content-Type"); ct != "application/x-checksum" {
		t.Errorf("GET echo Content-Type = %q, want application/x-checksum", ct)
	}
	if got := body(t, get); got != sha1S {
		t.Errorf("GET echo body = %q, want the stored client sha1 %q", got, sha1S)
	}
	// HEAD answers the same headers minus the body.
	head := e.do(t, http.MethodHead, "/binflow/generic-local/"+src+".sha1", nil, nil)
	if head.StatusCode != http.StatusOK || head.Header.Get("Content-Type") != "application/x-checksum" {
		t.Errorf("HEAD echo = %d CT=%q, want 200 application/x-checksum", head.StatusCode, head.Header.Get("Content-Type"))
	}

	// --- wrong value: 409 AND the value still written through ---
	wrong := strings.Repeat("00", 20)
	resp = e.do(t, http.MethodPut, "/binflow/generic-local/"+src+".sha1",
		strings.NewReader(wrong), nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("wrong-value PUT = %d, want 409 (body=%s)", resp.StatusCode, body(t, resp))
	}
	want := fmt.Sprintf("Checksum error for '%s': received '%s' but actual is '%s'", src+".sha1", wrong, sha1S)
	if got := body(t, resp); !strings.Contains(got, `"`+want+`"`) {
		t.Errorf("409 body = %s\nwant message = %q", got, want)
	}
	// Write-through: the client column and the GET echo carry the WRONG value.
	node, err := e.md.Nodes().Get(context.Background(), "generic-local", src)
	if err != nil {
		t.Fatalf("node after 409: %v", err)
	}
	if node.ClientSha1 != wrong {
		t.Errorf("node.ClientSha1 after 409 = %q, want the written-through %q", node.ClientSha1, wrong)
	}
	get = e.do(t, http.MethodGet, "/binflow/generic-local/"+src+".sha1", nil, nil)
	if got := body(t, get); got != wrong {
		t.Errorf("GET echo after 409 = %q, want the written-through %q", got, wrong)
	}

	// --- re-PUT overwrites: back to the correct value, 201 again ---
	resp = e.do(t, http.MethodPut, "/binflow/generic-local/"+src+".sha1",
		strings.NewReader(sha1S), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("re-PUT = %d (body=%s)", resp.StatusCode, body(t, resp))
	}
	get = e.do(t, http.MethodGet, "/binflow/generic-local/"+src+".sha1", nil, nil)
	if got := body(t, get); got != sha1S {
		t.Errorf("GET echo after re-PUT = %q, want %q", got, sha1S)
	}

	// --- per-algorithm independence: md5 and sha256 arms on their columns ---
	for _, tc := range []struct{ algo, value string }{
		{"md5", md5S},
		{"sha256", sha256S},
	} {
		resp = e.do(t, http.MethodPut, "/binflow/generic-local/"+src+"."+tc.algo,
			strings.NewReader(tc.value), nil)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("%s PUT = %d (body=%s)", tc.algo, resp.StatusCode, body(t, resp))
		}
		if got := resp.Header.Get("Location"); got != loc {
			t.Errorf("%s Location = %q, want %q", tc.algo, got, loc)
		}
		get = e.do(t, http.MethodGet, "/binflow/generic-local/"+src+"."+tc.algo, nil, nil)
		if got := body(t, get); got != tc.value {
			t.Errorf("%s GET echo = %q, want %q", tc.algo, got, tc.value)
		}
	}
	// The sha1 column survived the sibling registrations (per-algo overwrite).
	node, err = e.md.Nodes().Get(context.Background(), "generic-local", src)
	if err != nil {
		t.Fatalf("node after sibling arms: %v", err)
	}
	if node.ClientSha1 != sha1S || node.ClientMd5 != md5S || node.ClientSha256 != sha256S {
		t.Errorf("client columns = (%q,%q,%q), want all three correct",
			node.ClientSha1, node.ClientMd5, node.ClientSha256)
	}
}

// sha256Of reads the stored node sha256 of a seeded artifact (the harness's
// digestsOf covers sha1/md5; the sha256 here is the node's own column).
func sha256Of(t *testing.T, e *env, path string) string {
	t.Helper()
	node, err := e.md.Nodes().Get(context.Background(), "generic-local", path)
	if err != nil {
		t.Fatalf("node %s: %v", path, err)
	}
	if node.Sha256 == "" {
		t.Fatalf("node %s has no sha256 column", path)
	}
	return node.Sha256
}
