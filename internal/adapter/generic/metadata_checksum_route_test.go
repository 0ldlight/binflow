package generic_test

// T-613 / BIN-95 (ledger generic/metadata-checksum-route-family, the
// T-595 residual three legs + this ticket's pre-probe faces): the
// filename-keyed metadata checksum route on the generic plane — a PUT of
// maven-metadata.xml.{sha1,md5,sha256} is a dedicated 200-empty no-op
// (never the artifact sidecar interception arm), keyed on the terminal
// base name alone (hierarchy-agnostic, source-agnostic,
// validation-exempt, zero persistence), ranked ahead of the
// checksum-deploy arm and firing on the interception family's own write
// plane (local + routed virtual); the GET side computes on demand for
// every algorithm and its miss cites the stripped source. Live anchors:
// this ticket's A 7.161.26 probe legs gen-* (reports/agents/T-613.md).
// The reference's VIRTUAL read face for this family (synthesized xml +
// computed-of-synthesized + Maven-metadata-not-found miss) is a named
// residual outside this ticket and stays on the ordinary faces.

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

const xmlSeed = "<metadata>generic-seed</metadata>"

// TestMetadataChecksumRoutePutNoop walks the route's PUT matrix on the
// local plane: every level (nested, root), every suffix and case spelling,
// source present or absent, value right or wrong — one answer, 200 with an
// empty body, no Content-Type, no Location, and zero storage effects.
func TestMetadataChecksumRoutePutNoop(t *testing.T) {
	e := newEnv(t)
	mod := "t613/c5/maven-metadata.xml"
	_, sha1S, _ := digestsOf(xmlSeed)
	if resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+mod,
		strings.NewReader(xmlSeed), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed xml = %d (body=%s)", resp.StatusCode, body(t, resp))
	}

	legs := []struct{ name, path, val string }{
		{"nested-sha1-wrong", mod + ".sha1", strings.Repeat("1", 40)},
		{"nested-md5-wrong", mod + ".md5", strings.Repeat("0", 32)},
		{"nested-sha256-wrong", mod + ".sha256", strings.Repeat("3", 64)},
		{"nested-sha1-correct", mod + ".sha1", sha1S},
		{"source-miss", "t613/nomod/maven-metadata.xml.sha1", strings.Repeat("1", 40)},
		{"root", "maven-metadata.xml.sha1", strings.Repeat("1", 40)},
		{"case-SHA1", mod + ".SHA1", strings.Repeat("1", 40)},
		{"case-Sha1-md5", mod + ".Sha1", strings.Repeat("0", 32)},
	}
	for _, lg := range legs {
		resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+lg.path,
			strings.NewReader(lg.val), nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: PUT = %d (body=%s), want the route's 200 no-op", lg.name, resp.StatusCode, body(t, resp))
			continue
		}
		if got := body(t, resp); got != "" {
			t.Errorf("%s: 200 body = %q, want empty", lg.name, got)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			t.Errorf("%s: Content-Type = %q, want none", lg.name, ct)
		}
		if loc := resp.Header.Get("Location"); loc != "" {
			t.Errorf("%s: Location = %q, want none", lg.name, loc)
		}
	}

	// Zero storage effects: no sidecar node landed anywhere in the family,
	// and the source document's row carries none of the PUT bodies.
	for _, p := range []string{mod + ".sha1", mod + ".md5", mod + ".sha256", "maven-metadata.xml.sha1"} {
		if _, err := e.md.Nodes().Get(context.Background(), "generic-local", p); err == nil {
			t.Errorf("node landed at %s: the route must write nothing", p)
		}
	}
	node, err := e.md.Nodes().Get(context.Background(), "generic-local", mod)
	if err != nil {
		t.Fatalf("source node after the family: %v", err)
	}
	// The node row carries no client-declared digest from the family's PUTs.
	for _, v := range []string{strings.Repeat("1", 40), strings.Repeat("0", 32), strings.Repeat("3", 64), sha1S} {
		if node.ClientSha1 == v || node.ClientMd5 == v || node.ClientSha256 == v {
			t.Errorf("source row registered the family's value %q: the route must register nothing", v)
		}
	}
	// The stored bytes stand unchanged.
	resp := e.do(t, http.MethodGet, "/binflow/generic-local/"+mod, nil, nil)
	if got := body(t, resp); resp.StatusCode != http.StatusOK || got != xmlSeed {
		t.Fatalf("xml after the family = %d %q, want the seeded bytes", resp.StatusCode, got)
	}
}

// TestMetadataChecksumRouteGetComputed walks the GET matrix on the local
// plane: every algorithm (and case spelling) answers the COMPUTED digest
// of the stored source after wrong AND correct no-op PUTs — never a
// registered echo — HEAD renders the same face, the root level computes
// too, and a missing source answers the colon-form miss addressing the
// SOURCE.
func TestMetadataChecksumRouteGetComputed(t *testing.T) {
	e := newEnv(t)
	mod := "t613/c5/maven-metadata.xml"
	sha256S, sha1S, md5S := digestsOf(xmlSeed)
	if resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+mod,
		strings.NewReader(xmlSeed), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed xml = %d (body=%s)", resp.StatusCode, body(t, resp))
	}
	if resp := e.do(t, http.MethodPut, "/binflow/generic-local/maven-metadata.xml",
		strings.NewReader(xmlSeed), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed root xml = %d (body=%s)", resp.StatusCode, body(t, resp))
	}
	// The family's no-ops, wrong and correct alike, must not surface.
	for _, p := range []struct{ path, val string }{
		{mod + ".sha1", strings.Repeat("1", 40)},
		{mod + ".md5", strings.Repeat("0", 32)},
		{mod + ".sha256", strings.Repeat("3", 64)},
	} {
		if resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+p.path,
			strings.NewReader(p.val), nil); resp.StatusCode != http.StatusOK {
			t.Fatalf("no-op PUT %s = %d", p.path, resp.StatusCode)
		}
	}

	tests := []struct {
		name    string
		path    string
		head    bool
		want    int
		wantOut string
		wantMsg string
	}{
		{"sha1 computed after wrong no-op", mod + ".sha1", false, http.StatusOK, sha1S, ""},
		{"md5 computed after wrong no-op", mod + ".md5", false, http.StatusOK, md5S, ""},
		{"sha256 computed after wrong no-op", mod + ".sha256", false, http.StatusOK, sha256S, ""},
		{"case .SHA1 computes", mod + ".SHA1", false, http.StatusOK, sha1S, ""},
		{"case .Md5 computes", mod + ".Md5", false, http.StatusOK, md5S, ""},
		{"root computes", "maven-metadata.xml.sha1", false, http.StatusOK, sha1S, ""},
		{"HEAD renders the same face", mod + ".sha1", true, http.StatusOK, "", ""},
		{"source absent keeps colon-form miss at the source", "t613/nomod/maven-metadata.xml.sha1", false,
			http.StatusNotFound, "", `"File not found.; Path: 'generic-local:t613/nomod/maven-metadata.xml'"`},
		{"source absent md5 same miss family", "t613/nomod/maven-metadata.xml.md5", false,
			http.StatusNotFound, "", `"File not found.; Path: 'generic-local:t613/nomod/maven-metadata.xml'"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := http.MethodGet
			if tt.head {
				method = http.MethodHead
			}
			resp := e.do(t, method, "/binflow/generic-local/"+tt.path, nil, nil)
			got := body(t, resp)
			if resp.StatusCode != tt.want {
				t.Fatalf("%s = %d (body=%s), want %d", method, resp.StatusCode, got, tt.want)
			}
			if tt.wantMsg != "" && !strings.Contains(got, tt.wantMsg) {
				t.Fatalf("body = %s, want message %q", got, tt.wantMsg)
			}
			if tt.want == http.StatusOK {
				if ct := resp.Header.Get("Content-Type"); ct != "application/x-checksum" {
					t.Fatalf("Content-Type = %q, want application/x-checksum", ct)
				}
				if !tt.head {
					if got != tt.wantOut {
						t.Fatalf("body = %q, want computed %q", got, tt.wantOut)
					}
					if cl := resp.Header.Get("Content-Length"); cl != strconv.Itoa(len(tt.wantOut)) {
						t.Fatalf("Content-Length = %q, want %d", cl, len(tt.wantOut))
					}
				} else if got != "" {
					t.Fatalf("HEAD body = %q, want empty", got)
				}
			}
		})
	}
}

// TestMetadataChecksumRouteOrdering pins the route's rank and plane model:
// ahead of the checksum-deploy arm (the header never reaches it), on the
// interception family's write plane (routed virtual no-ops, un-routed
// virtual keeps the C5 405, remote keeps the read-only refusal), and the
// anonymous write still earns the store chain's 401 challenge.
func TestMetadataChecksumRouteOrdering(t *testing.T) {
	e := newVirtualEnv(t)
	mod := "t613/c5/maven-metadata.xml"
	if resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+mod,
		strings.NewReader(xmlSeed), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed xml = %d (body=%s)", resp.StatusCode, body(t, resp))
	}
	sha256S, _, _ := digestsOf(xmlSeed)

	// X-Checksum-Deploy on the family path: the route's 200 no-op, never
	// the deploy arm's own answers.
	resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+mod+".sha1", nil,
		map[string]string{"X-Checksum-Deploy": "true", "X-Checksum-Sha256": sha256S})
	if resp.StatusCode != http.StatusOK || body(t, resp) != "" {
		t.Fatalf("cd-header family PUT = %d %q, want the 200 empty no-op", resp.StatusCode, body(t, resp))
	}

	// Routed virtual: the same no-op through the write route's plane.
	if resp := e.do(t, http.MethodPut, "/binflow/gvirt/"+mod+".sha1",
		strings.NewReader(strings.Repeat("1", 40)), nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("routed virtual family PUT = %d (body=%s), want 200 no-op", resp.StatusCode, body(t, resp))
	}

	// Un-routed virtual: falls through to the ordinary chain's C5 405.
	resp = e.do(t, http.MethodPut, "/binflow/gvirt-noroute/"+mod+".sha1",
		strings.NewReader(strings.Repeat("1", 40)), nil)
	if resp.StatusCode != http.StatusMethodNotAllowed ||
		!strings.Contains(body(t, resp), "No local repository was configured as local deployment repository for the (gvirt-noroute) virtual repository.") {
		t.Fatalf("un-routed virtual family PUT = %d (body=%s), want the C5 405", resp.StatusCode, body(t, resp))
	}

	// Remote: falls through to the read-only refusal (RE-05), never a
	// pull-through on the write verb.
	ctx := context.Background()
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "t613-remote", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
		Config: `{"url":"http://127.0.0.1:9/","allowPrivateUpstream":true}`,
	}); err != nil {
		t.Fatalf("CreateRepo remote: %v", err)
	}
	resp = e.do(t, http.MethodPut, "/binflow/t613-remote/"+mod+".sha1",
		strings.NewReader(strings.Repeat("1", 40)), nil)
	if resp.StatusCode != http.StatusMethodNotAllowed ||
		!strings.Contains(body(t, resp), "read-only proxy cache") {
		t.Fatalf("remote family PUT = %d (body=%s), want the RE-05 405", resp.StatusCode, body(t, resp))
	}

	// Anonymous write: the store chain's 401 challenge, not a silent no-op.
	e.anon = true
	resp = e.do(t, http.MethodPut, "/binflow/generic-local/"+mod+".sha1",
		strings.NewReader(strings.Repeat("1", 40)), nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous family PUT = %d, want 401", resp.StatusCode)
	}
	if ch := resp.Header.Get("WWW-Authenticate"); ch != `Basic realm="BinFlow Realm"` {
		t.Fatalf("WWW-Authenticate = %q, want the store chain's Basic challenge", ch)
	}
}

// TestMetadataChecksumRouteNonMembers pins the family's boundary: the
// plugin-group variant and non-.xml spellings stay on the interception
// family's miss, .sha512 is an ordinary file, and a plain artifact's
// sidecar keeps the 409 write-through + registered echo untouched.
func TestMetadataChecksumRouteNonMembers(t *testing.T) {
	e := newEnv(t)
	if resp := e.do(t, http.MethodPut, "/binflow/generic-local/t613/c5/maven-metadata.xml",
		strings.NewReader(xmlSeed), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed xml = %d (body=%s)", resp.StatusCode, body(t, resp))
	}
	other := "t613/c5/other.bin"
	if resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+other,
		strings.NewReader("t613-other"), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed other = %d (body=%s)", resp.StatusCode, body(t, resp))
	}

	// Plugin-group variant and non-.xml spelling: the interception
	// family's source-miss 404 (their stripped sources were never seeded).
	for _, p := range []string{"t613/c5/metadata-maven-metadata.xml.sha1", "t613/c5/maven-metadata.md5"} {
		resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+p,
			strings.NewReader(strings.Repeat("1", 40)), nil)
		if resp.StatusCode != http.StatusNotFound ||
			!strings.Contains(body(t, resp), "Target file to set checksum on doesn't exist") {
			t.Fatalf("PUT %s = %d (body=%s), want the interception family's miss", p, resp.StatusCode, body(t, resp))
		}
	}

	// .sha512: the ordinary-file arm (201 + stored bytes).
	if resp := e.do(t, http.MethodPut, "/binflow/generic-local/t613/c5/maven-metadata.xml.sha512",
		strings.NewReader("ffffffff"), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("sha512 PUT = %d (body=%s), want the ordinary 201", resp.StatusCode, body(t, resp))
	}
	resp := e.do(t, http.MethodGet, "/binflow/generic-local/t613/c5/maven-metadata.xml.sha512", nil, nil)
	if resp.StatusCode != http.StatusOK || body(t, resp) != "ffffffff" {
		t.Fatalf("sha512 GET = %d %q, want the stored bytes", resp.StatusCode, body(t, resp))
	}

	// Plain artifact sidecar: the 409 write-through and the registered
	// echo ride the interception family unchanged.
	wrong := strings.Repeat("0", 32)
	if resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+other+".md5",
		strings.NewReader(wrong), nil); resp.StatusCode != http.StatusConflict {
		t.Fatalf("other.bin.md5 wrong PUT = %d, want the interception family's 409", resp.StatusCode)
	}
	resp = e.do(t, http.MethodGet, "/binflow/generic-local/"+other+".md5", nil, nil)
	if resp.StatusCode != http.StatusOK || body(t, resp) != wrong {
		t.Fatalf("other.bin.md5 GET after = %d %q, want the registered echo", resp.StatusCode, body(t, resp))
	}
}
