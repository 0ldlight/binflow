package maven

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// harness is the real-stack test assembly of the maven plane: a REAL
// storage engine, a REAL metadata store and the REAL repo.Service behind
// the handler, mounted bare (httpapi strips /binflow before the adapter
// sees the path; the principal rides the adapter context seam exactly as
// the mounted chain boxes it).
type harness struct {
	t   *testing.T
	h   *Handler
	md  metadata.Store
	svc repo.Service
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	st, err := storage.OpenEngine(root, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: root + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	seedRepos(ctx, t, md)

	authz := auth.NewFromStore(md, true) // anonymous reads on
	svc := repo.New(st, md, authz, nil)
	h := New(svc, md.Repos(), md.Blobs())
	return &harness{t: t, h: h, md: md, svc: svc}
}

// seedRepos creates the repository rows the matrix needs, config blobs
// included (the REST transport of the checksum-policy family is exercised
// by the curl leg against the assembled binary).
func seedRepos(ctx context.Context, t *testing.T, md metadata.Store) {
	t.Helper()
	rows := []*metadata.Repo{
		{RepoKey: "maven-local", Type: repo.TypeLocal, PackageType: Protocol, Config: "{}"},
		{RepoKey: "maven-lenient", Type: repo.TypeLocal, PackageType: Protocol,
			Config: `{"checksumPolicyType":"server-generated-checksums"}`},
		{RepoKey: "maven-relonly", Type: repo.TypeLocal, PackageType: Protocol,
			Config: `{"handleReleases":false}`},
		{RepoKey: "maven-snaponly", Type: repo.TypeLocal, PackageType: Protocol,
			Config: `{"handleSnapshots":false}`},
		{RepoKey: "maven-remote", Type: repo.TypeRemote, PackageType: Protocol,
			Config: `{"url":"http://127.0.0.1:9/upstream"}`},
		{RepoKey: "maven-virtual", Type: repo.TypeVirtual, PackageType: Protocol, Config: "{}"},
	}
	for _, r := range rows {
		if err := md.Repos().Create(ctx, r); err != nil {
			t.Fatalf("seed repo %s: %v", r.RepoKey, err)
		}
	}
}

// serve runs one request against the bare-mounted handler. admin=true
// boxes an admin principal (the credential the PRD's M-sequences use);
// admin=false sends the request anonymous.
func (hs *harness) serve(method, target string, body []byte, hdr map[string]string, admin bool) *http.Response {
	hs.t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rdr)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if admin {
		p := &auth.Principal{Name: "admin", Admin: true}
		req = req.WithContext(adapter.WithPrincipal(req.Context(), p))
	}
	rec := httptest.NewRecorder()
	hs.h.ServeHTTP(rec, req)
	resp := rec.Result()
	return resp
}

// drain reads and closes a response body.
func drain(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	if resp.Body == nil {
		return nil
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	_ = resp.Body.Close()
	return b
}

const (
	jarPath = "/maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar"
	pomPath = "/maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0.pom"
)

var jarBytes = []byte("fake-jar-bytes-0123456789")

func digests(b []byte) (sha1Hex, md5Hex, sha256Hex string) {
	s1, m5, s256 := sha1.Sum(b), md5.Sum(b), sha256.Sum256(b)
	return hex.EncodeToString(s1[:]), hex.EncodeToString(m5[:]), hex.EncodeToString(s256[:])
}

// deployJar lands the standard release trio (jar + correct header
// checksums) into repoKey at the given GAV path.
func (hs *harness) deployJar(repoKey, gavPath string, body []byte) *http.Response {
	s1, _, s256 := digests(body)
	return hs.serve(http.MethodPut, "/"+repoKey+"/"+gavPath, body, map[string]string{
		"X-Checksum-Sha1":   s1,
		"X-Checksum-Sha256": s256,
	}, true)
}

// TestDeployResolveRoundtrip is the wire-level M11/M12/M13 leg: the exact
// request sequence maven-deploy-plugin and the resolver issue (PUT artifact
// + PUT sidecars + GET artifact + GET sidecars), against the real service
// stack.
func TestDeployResolveRoundtrip(t *testing.T) {
	hs := newHarness(t)
	s1, m5, s256 := digests(jarBytes)

	// deploy: jar with correct header checksums
	resp := hs.serve(http.MethodPut, jarPath, jarBytes, map[string]string{
		"X-Checksum-Sha1":   s1,
		"X-Checksum-Sha256": s256,
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("jar PUT = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, jarPath) {
		t.Errorf("Location = %q", loc)
	}
	if got := resp.Header.Get("X-Checksum-Sha256"); got != s256 {
		t.Errorf("X-Checksum-Sha256 = %q, want measured %q", got, s256)
	}
	body := drain(t, resp)
	if !bytes.Contains(body, []byte(`"checksums"`)) || !bytes.Contains(body, []byte(s1)) {
		t.Errorf("ItemCreated body missing checksums: %s", body)
	}

	// pom
	pom := []byte("<project><modelVersion>4.0.0</modelVersion></project>")
	if resp := hs.serve(http.MethodPut, pomPath, pom, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("pom PUT = %d", resp.StatusCode)
	}

	// sidecars (correct values; mvn wagon PUTs them after the artifacts)
	for _, tc := range []struct{ suffix, value string }{
		{".sha1", s1}, {".md5", m5},
	} {
		resp := hs.serve(http.MethodPut, jarPath+tc.suffix, []byte(tc.value+"\n"), nil, true)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("sidecar PUT %s = %d (%s)", tc.suffix, resp.StatusCode, drain(t, resp))
		}
		if b := drain(t, resp); len(b) != 0 {
			t.Errorf("sidecar PUT body = %q, want empty (201 no body)", b)
		}
	}

	// client metadata PUT is accepted (ME-06; the calculator is T-68's)
	metaXML := []byte("<metadata><groupId>com.acme</groupId><artifactId>demo-app</artifactId></metadata>")
	if resp := hs.serve(http.MethodPut, "/maven-local/com/acme/demo-app/maven-metadata.xml", metaXML, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("metadata PUT = %d (%s)", resp.StatusCode, drain(t, resp))
	}

	// resolve: GET artifact with the M1 header set (M12)
	resp = hs.serve(http.MethodGet, jarPath, nil, nil, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("jar GET = %d", resp.StatusCode)
	}
	if got := drain(t, resp); !bytes.Equal(got, jarBytes) {
		t.Errorf("jar body mismatch")
	}
	if got := resp.Header.Get("X-Checksum-Sha1"); got != s1 {
		t.Errorf("X-Checksum-Sha1 = %q, want %q", got, s1)
	}
	if got := resp.Header.Get("X-Checksum-Md5"); got != m5 {
		t.Errorf("X-Checksum-Md5 = %q, want %q", got, m5)
	}
	if got := resp.Header.Get("ETag"); got != s1 {
		t.Errorf("ETag = %q, want sha1", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/java-archive" {
		t.Errorf("Content-Type = %q", got)
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Error("Accept-Ranges missing")
	}

	// sidecar GETs answer the COMPUTED bare hex, not the stored bytes
	// (the PUT above sent value+"\n"; the GET must come back without it)
	for _, tc := range []struct{ suffix, want string }{
		{".sha1", s1}, {".md5", m5}, {".sha256", s256},
	} {
		resp := hs.serve(http.MethodGet, jarPath+tc.suffix, nil, nil, true)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("sidecar GET %s = %d", tc.suffix, resp.StatusCode)
		}
		got := string(drain(t, resp))
		if got != tc.want {
			t.Errorf("sidecar GET %s = %q, want %q (bare hex, no newline)", tc.suffix, got, tc.want)
		}
	}

	// stored metadata serves as-is until T-68's calculator lands
	resp = hs.serve(http.MethodGet, "/maven-local/com/acme/demo-app/maven-metadata.xml", nil, nil, true)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(drain(t, resp), metaXML) {
		t.Fatalf("metadata GET = %d", resp.StatusCode)
	}

	// HEAD answers the same headers minus the body
	resp = hs.serve(http.MethodHead, jarPath, nil, nil, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD = %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Checksum-Sha1") != s1 {
		t.Error("HEAD missing checksum header")
	}
	if b := drain(t, resp); len(b) != 0 {
		t.Errorf("HEAD body = %q, want empty", b)
	}
}

// TestChecksumPolicyTwoStates is M16c/M17: a wrong X-Checksum-Sha1 is a
// 409 on a client-checksums repository and a silent 201 (measured values
// authoritative) on a server-generated one.
func TestChecksumPolicyTwoStates(t *testing.T) {
	hs := newHarness(t)
	zeros := strings.Repeat("0", 40)
	hdr := map[string]string{"X-Checksum-Sha1": zeros}

	strict := hs.serve(http.MethodPut, jarPath, jarBytes, hdr, true)
	if strict.StatusCode != http.StatusConflict {
		t.Fatalf("strict PUT = %d, want 409", strict.StatusCode)
	}
	msg := string(drain(t, strict))
	if !strings.Contains(msg, "received") || !strings.Contains(msg, "actual") {
		t.Errorf("409 message lacks received/actual: %s", msg)
	}

	lenient := hs.serve(http.MethodPut, "/maven-lenient/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", jarBytes, hdr, true)
	if lenient.StatusCode != http.StatusCreated {
		t.Fatalf("lenient PUT = %d, want 201", lenient.StatusCode)
	}
	_ = drain(t, lenient)

	s1, _, _ := digests(jarBytes)
	resp := hs.serve(http.MethodGet, "/maven-lenient/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", nil, nil, true)
	if got := resp.Header.Get("X-Checksum-Sha1"); got != s1 {
		t.Errorf("lenient GET X-Checksum-Sha1 = %q, want measured %q (claim was %q)", got, s1, zeros)
	}
}

// TestSidecarStates is M18 plus the size guard and missing-target rules of
// rest-api.md section 1.5.
func TestSidecarStates(t *testing.T) {
	hs := newHarness(t)
	if resp := hs.deployJar("maven-local", "com/acme/demo-app/1.1.0/demo-app-1.1.0.jar", jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d", resp.StatusCode)
	}
	s1, _, _ := digests(jarBytes)
	side := "/maven-local/com/acme/demo-app/1.1.0/demo-app-1.1.0.jar.sha1"

	// good value -> 201 and the GET answers the measured digest
	if resp := hs.serve(http.MethodPut, side, []byte(s1), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("good sidecar = %d (%s)", resp.StatusCode, drain(t, resp))
	}

	// wrong value on a client-checksums repo -> 409
	resp := hs.serve(http.MethodPut, side, []byte("deadbeef"), nil, true)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("bad sidecar = %d, want 409", resp.StatusCode)
	}
	if msg := string(drain(t, resp)); !strings.Contains(msg, "deadbeef") || !strings.Contains(msg, s1) {
		t.Errorf("409 message = %s", msg)
	}

	// wrong value on a server-generated repo -> accepted, measured lands
	lenientSide := "/maven-lenient/com/acme/demo-app/1.1.0/demo-app-1.1.0.jar.sha1"
	if resp := hs.deployJar("maven-lenient", "com/acme/demo-app/1.1.0/demo-app-1.1.0.jar", jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("lenient seed = %d", resp.StatusCode)
	}
	if resp := hs.serve(http.MethodPut, lenientSide, []byte("deadbeef"), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("lenient sidecar = %d, want 201", resp.StatusCode)
	}
	if got := string(drain(t, hs.serve(http.MethodGet, lenientSide, nil, nil, true))); got != s1 {
		t.Errorf("lenient sidecar GET = %q, want measured %q", got, s1)
	}

	// >1024 bytes -> the fixed suspicious-size 409
	big := bytes.Repeat([]byte("a"), 1025)
	resp = hs.serve(http.MethodPut, side, big, nil, true)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("oversized sidecar = %d, want 409", resp.StatusCode)
	}
	if msg := string(drain(t, resp)); !strings.Contains(msg, "Suspicious checksum file") {
		t.Errorf("oversized message = %s", msg)
	}

	// sidecar of a missing target -> 404
	resp = hs.serve(http.MethodPut, "/maven-local/com/acme/demo-app/9.9.9/demo-app-9.9.9.jar.sha1", []byte(s1), nil, true)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("sidecar of missing target = %d, want 404", resp.StatusCode)
	}

	// sha512: layout-recognized, PUT accepted unverified, GET 404
	p512 := "/maven-local/com/acme/demo-app/1.1.0/demo-app-1.1.0.jar.sha512"
	if resp := hs.serve(http.MethodPut, p512, []byte(strings.Repeat("f", 128)), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("sha512 sidecar PUT = %d", resp.StatusCode)
	}
	if resp := hs.serve(http.MethodGet, p512, nil, nil, true); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("sha512 sidecar GET = %d, want 404", resp.StatusCode)
	}
}

// TestSnapshotPolicy is M19 (v1.1 errata: 409): the handle* switches
// refuse the matching deploy class, sidecars inherit the refusal, metadata
// stays exempt and reads stay unaffected.
func TestSnapshotPolicy(t *testing.T) {
	hs := newHarness(t)

	snapJar := "/maven-snaponly/com/acme/x/2.0-SNAPSHOT/x-2.0-SNAPSHOT.jar"
	resp := hs.serve(http.MethodPut, snapJar, jarBytes, nil, true)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("snapshot deploy on handleSnapshots=false = %d, want 409", resp.StatusCode)
	}

	// timestamped spelling inherits the refusal
	tsJar := "/maven-snaponly/com/acme/x/2.0-SNAPSHOT/x-2.0-20240819.101500-1.jar"
	if resp := hs.serve(http.MethodPut, tsJar, jarBytes, nil, true); resp.StatusCode != http.StatusConflict {
		t.Fatalf("timestamped deploy on handleSnapshots=false = %d, want 409", resp.StatusCode)
	}

	// a release deploys fine into the snapshot-only repo, and reads work
	rel := "/maven-snaponly/com/acme/x/2.0/x-2.0.jar"
	if resp := hs.deployJar("maven-snaponly", "com/acme/x/2.0/x-2.0.jar", jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("release deploy on handleSnapshots=false = %d", resp.StatusCode)
	}
	if resp := hs.serve(http.MethodGet, rel, nil, nil, true); resp.StatusCode != http.StatusOK {
		t.Fatalf("GET on policy-off repo = %d, want 200", resp.StatusCode)
	}

	// the sidecar of a refused snapshot inherits the refusal
	s1, _, _ := digests(jarBytes)
	if resp := hs.serve(http.MethodPut, snapJar+".sha1", []byte(s1), nil, true); resp.StatusCode != http.StatusConflict {
		t.Fatalf("snapshot sidecar on handleSnapshots=false = %d, want 409", resp.StatusCode)
	}

	// release switch
	if resp := hs.serve(http.MethodPut, "/maven-relonly/com/acme/x/2.0/x-2.0.jar", jarBytes, nil, true); resp.StatusCode != http.StatusConflict {
		t.Fatalf("release deploy on handleReleases=false = %d, want 409", resp.StatusCode)
	}

	// metadata is exempt from the version-class gates
	if resp := hs.serve(http.MethodPut, "/maven-snaponly/com/acme/x/maven-metadata.xml", []byte("<metadata/>"), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("metadata PUT on policy-off repo = %d, want 201", resp.StatusCode)
	}
}

// TestLayoutRefusals is M20: non-layout paths are a 400 on PUT and GET
// alike (strict maven parsing, C2 interim).
func TestLayoutRefusals(t *testing.T) {
	hs := newHarness(t)
	for _, p := range []string{
		"/maven-local/foo.jar",
		"/maven-local/com/acme/demo-app/1.0.0/zzz-1.0.0.jar",
		"/maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar/../evil.jar",
		"/maven-local/demo-app/1.0.0/demo-app-1.0.0.jar",
	} {
		resp := hs.serve(http.MethodPut, p, jarBytes, nil, true)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("PUT %s = %d, want 400 (%s)", p, resp.StatusCode, drain(t, resp))
		}
	}
	// reads are parsed with the same strictness
	if resp := hs.serve(http.MethodGet, "/maven-local/foo.jar", nil, nil, true); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("GET bad layout = %d, want 400", resp.StatusCode)
	}
}

// TestRangeAndConditional is M21: the M1 download semantics inherited on
// maven paths.
func TestRangeAndConditional(t *testing.T) {
	hs := newHarness(t)
	if resp := hs.deployJar("maven-local", "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d", resp.StatusCode)
	}

	resp := hs.serve(http.MethodGet, jarPath, nil, map[string]string{"Range": "bytes=0-3"}, true)
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("range GET = %d, want 206", resp.StatusCode)
	}
	if got := drain(t, resp); string(got) != string(jarBytes[:4]) {
		t.Errorf("range body = %q", got)
	}
	if cr := resp.Header.Get("Content-Range"); cr != fmt.Sprintf("bytes 0-3/%d", len(jarBytes)) {
		t.Errorf("Content-Range = %q", cr)
	}

	resp = hs.serve(http.MethodGet, jarPath, nil, map[string]string{"Range": "bytes=99999-"}, true)
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("unsatisfiable range = %d, want 416", resp.StatusCode)
	}

	s1, _, _ := digests(jarBytes)
	resp = hs.serve(http.MethodGet, jarPath, nil, map[string]string{"If-None-Match": s1}, true)
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("conditional GET = %d, want 304", resp.StatusCode)
	}

	resp = hs.serve(http.MethodGet, jarPath, nil, map[string]string{"If-None-Match": `"` + s1 + `"`}, true)
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("quoted ETag GET = %d, want 304", resp.StatusCode)
	}
}

// TestIndexNamespace is ME-10: the Maven indexer namespace answers the
// E-01 404 on every verb, never a layout 400.
func TestIndexNamespace(t *testing.T) {
	hs := newHarness(t)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		resp := hs.serve(method, "/maven-local/.index/nexus-maven-repository-index.gz", jarBytes, nil, true)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s .index = %d, want 404", method, resp.StatusCode)
		}
		if msg := string(drain(t, resp)); !strings.Contains(msg, "not implemented") {
			t.Errorf("message = %s", msg)
		}
	}
}

// TestNonLocalRepositories pins the two class intercepts: the remote
// sidecar pass-through 404 (spec-fixed message) and the write refusals
// (RE-05 405 for remote, Q2/C5 405 for an un-routed virtual).
func TestNonLocalRepositories(t *testing.T) {
	hs := newHarness(t)

	// remote: checksum suffixes never proxy — 404 before any engine
	for _, suffix := range []string{".sha1", ".md5", ".sha256"} {
		resp := hs.serve(http.MethodGet, "/maven-remote/junit/junit/4.13.2/junit-4.13.2.jar"+suffix, nil, nil, true)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("remote sidecar GET %s = %d, want 404", suffix, resp.StatusCode)
		}
		if msg := string(drain(t, resp)); msg != "Checksums are not downloadable." && !strings.Contains(msg, "Checksums are not downloadable.") {
			t.Errorf("remote sidecar message = %s", msg)
		}
	}
	// anonymous remote sidecar GET hits the same wall (class seam needs no
	// principal)
	resp := hs.serve(http.MethodGet, "/maven-remote/junit/junit/4.13.2/junit-4.13.2.jar.sha1", nil, nil, false)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("anonymous remote sidecar = %d, want 404", resp.StatusCode)
	}

	// remote: PUT is the 405 of a read-only cache
	resp = hs.serve(http.MethodPut, "/maven-remote/com/acme/x/1.0.0/x-1.0.0.jar", jarBytes, nil, true)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("remote PUT = %d, want 405", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); !strings.Contains(allow, "GET") {
		t.Errorf("Allow = %q", allow)
	}

	// virtual: un-routed write refusal with the errata's fixed wording
	resp = hs.serve(http.MethodPut, "/maven-virtual/com/acme/x/1.0.0/x-1.0.0.jar", jarBytes, nil, true)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("virtual PUT = %d, want 405", resp.StatusCode)
	}
	if msg := string(drain(t, resp)); !strings.Contains(msg,
		"No local repository was configured as local deployment repository for the (maven-virtual) virtual repository.") {
		t.Errorf("virtual 405 message = %s", msg)
	}
}

// TestDeleteFlow pins the M1 delete semantics on maven paths (204, then
// the idempotent 404s; the metadata recalculation cascade is T-68's).
func TestDeleteFlow(t *testing.T) {
	hs := newHarness(t)
	if resp := hs.deployJar("maven-local", "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d", resp.StatusCode)
	}

	if resp := hs.serve(http.MethodDelete, jarPath, nil, nil, true); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204", resp.StatusCode)
	}
	if resp := hs.serve(http.MethodDelete, jarPath, nil, nil, true); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("repeat DELETE = %d, want 404", resp.StatusCode)
	}
	if resp := hs.serve(http.MethodGet, jarPath, nil, nil, true); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after delete = %d, want 404", resp.StatusCode)
	}
}

// TestTraversalDefense is NFR-S18's maven slice: encoded and raw dot
// segments die at the shared layout defense with 400, never reaching the
// node layer.
func TestTraversalDefense(t *testing.T) {
	hs := newHarness(t)
	targets := []string{
		"/maven-local/com/acme/../../etc/passwd.jar",
		"/maven-local/com/%2e%2e/x.jar",
		"/maven-local/com/%2E%2E%2Fx.jar",
		"/maven-local//com/acme/app/1.0.0/app-1.0.0.jar",
	}
	for _, target := range targets {
		resp := hs.serve(http.MethodPut, target, jarBytes, nil, true)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("PUT %s = %d, want 400 (%s)", target, resp.StatusCode, drain(t, resp))
		}
	}
}

// TestAnonymousAndMethodGates: anonymous reads ride the anonymous-access
// flag, anonymous writes are challenged, unknown verbs answer 405+Allow.
func TestAnonymousAndMethodGates(t *testing.T) {
	hs := newHarness(t)
	if resp := hs.deployJar("maven-local", "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d", resp.StatusCode)
	}

	if resp := hs.serve(http.MethodGet, jarPath, nil, nil, false); resp.StatusCode != http.StatusOK {
		t.Fatalf("anonymous GET = %d, want 200", resp.StatusCode)
	}

	resp := hs.serve(http.MethodPut, "/maven-local/com/acme/demo-app/2.0.0/demo-app-2.0.0.jar", jarBytes, nil, false)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous PUT = %d, want 401", resp.StatusCode)
	}
	if resp.Header.Get("WWW-Authenticate") == "" {
		t.Error("401 without Basic challenge")
	}

	resp = hs.serve(http.MethodPost, jarPath, jarBytes, nil, true)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); !strings.Contains(allow, "PUT") {
		t.Errorf("Allow = %q", allow)
	}
}

// TestMetadataOverwriteFreedom pins the exemption's practical half: an
// identical metadata re-send is the idempotent retransmit (no gates), and
// a CHANGED re-send succeeds for the credential the M-sequences use
// (admin). The write-without-delete divergence for delete-less principals
// is the documented repo.Service SPI gap (see the ticket log).
func TestMetadataOverwriteFreedom(t *testing.T) {
	hs := newHarness(t)
	target := "/maven-local/com/acme/demo-app/maven-metadata.xml"
	v1 := []byte("<metadata><versioning><versions><version>1.0.0</version></versions></versioning></metadata>")
	v2 := []byte("<metadata><versioning><versions><version>1.0.0</version><version>1.1.0</version></versions></versioning></metadata>")

	if resp := hs.serve(http.MethodPut, target, v1, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("metadata v1 = %d", resp.StatusCode)
	}
	if resp := hs.serve(http.MethodPut, target, v1, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("metadata v1 re-send = %d, want idempotent 201", resp.StatusCode)
	}
	if resp := hs.serve(http.MethodPut, target, v2, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("metadata v2 = %d, want 201 (admin overwrite)", resp.StatusCode)
	}
	if got := drain(t, hs.serve(http.MethodGet, target, nil, nil, true)); !bytes.Equal(got, v2) {
		t.Errorf("metadata after overwrite = %s", got)
	}
}
