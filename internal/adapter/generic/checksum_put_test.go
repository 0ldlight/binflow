// T-574 / BIN-56 — generic 存储面裸 checksum PUT（终缀 .sha1/.md5/.sha256）
// 拦截族的可落地半面：源缺 404 文案逐字 + 无文件节点 + 阴性对照
// （.sha512/.asc/非终缀普通部署不变）。值对 201 空体/值错 409 写穿/GET
// 回显依赖 client-checksum 持久化缝（repo.Service 缺口，票内登记移交），
// 源在场臂在此钉住过渡态（普通部署链），缝落地后由后续票翻正。
// 权威口径：reports/compatibility/L037-probe-arms.md Arm 1（A 7.161.26 双轮）。
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
	// on-demand generation for an unset checksum).
	if resp := e.do(t, http.MethodGet, "/binflow/generic-local/t574/lone.txt.sha1", nil, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET unset sidecar = %d, want 404", resp.StatusCode)
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

// TestChecksumPutSourcePresentInterim pins the explicitly INTERIM state
// of the source-present arms until the client-checksum persistence seam
// lands (repo.Service gap, T-574 handoff): the ordinary deploy chain
// still runs, so the sidecar lands as a plain file node with the full
// envelope. The A model (201 CL=0 + Location=source + originalChecksums
// write-through, wrong value 409-with-write-through, GET echo of the
// stored client value) flips these arms once the seam exists.
func TestChecksumPutSourcePresentInterim(t *testing.T) {
	e := newEnv(t)
	src := "t574i/src.bin"
	if resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+src,
		strings.NewReader("source-bytes"), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("source seed = %d (%s)", resp.StatusCode, body(t, resp))
	}
	resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+src+".sha1",
		strings.NewReader(strings.Repeat("00", 20)), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("interim sidecar PUT = %d, want 201 ordinary deploy (body=%s)",
			resp.StatusCode, body(t, resp))
	}
	node, err := e.md.Nodes().Get(context.Background(), "generic-local", src+".sha1")
	if err != nil {
		t.Fatalf("interim sidecar node: %v (expected the pre-seam file-node landing)", err)
	}
	if node.Size == 0 {
		t.Error("interim sidecar node size = 0")
	}
}
