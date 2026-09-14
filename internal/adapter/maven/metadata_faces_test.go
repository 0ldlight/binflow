package maven

// L021-3 (maven three small faces, client-blind closure of the L020
// differential): the wire-level replays of the three adjudicated faces
// against the real service stack —
//
//	face 1 (V6): a client PUT of the SNAPSHOT version document is the
//	  reference's 202 + 0B + no Content-Type discard (nothing lands,
//	  nothing recalculates) — the module document's PUT keeps the store
//	  chain (the L013-4 C1 level split; A replays group documents
//	  verbatim, the R-21 pending face) — while the refusal corners the
//	  store chain owns (remote 405, un-routed virtual C5 405, anonymous
//	  401, write-less principal 403) keep their shapes;
//	face 2 (V7): the served XML renders the A form — modelVersion
//	  attribute, `<version>` tail, per-family versioning order — pinned
//	  against the L020/L014-2 wire skeletons;
//	face 3 (V8): the file 404 wording "File not found.; Path:
//	  '<repo>:<path>'" across the wire's three arms (ghost file in an
//	  existing directory, ghost directory tree, ghost metadata document).

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// drainRaw reads and closes a store reader (the response-body drain's
// node-reader twin).
func drainRaw(t *testing.T, rc io.ReadCloser) []byte {
	t.Helper()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read stored node: %v", err)
	}
	_ = rc.Close()
	return b
}

// TestMetadataPutAcceptanceDiscard is face 1: the acceptance code, the
// zero-byte shape, and the no-side-effect contract (no stored client
// bytes, no recalculated document) on the SNAPSHOT version document,
// the module-level contrast, and the preserved refusals.
func TestMetadataPutAcceptanceDiscard(t *testing.T) {
	hs := newHarness(t)
	defer hs.waitCalc()

	// A snapshot pom fact and its settled version document (the pre-PUT
	// baseline, wire f2a).
	snapDir := "com/acme/demo-app/1.0-SNAPSHOT/"
	hs.seedNode("maven-local", snapDir+"demo-app-1.0-20260913.220231-1.pom", []byte("p"))
	hs.recalc("maven-local", "com.acme", "demo-app", "1.0-SNAPSHOT")
	_, baseline := hs.getMeta("maven-local", "com.acme", "demo-app", "1.0-SNAPSHOT")

	target := "/maven-local/" + snapDir + "maven-metadata.xml"
	body := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<metadata modelVersion=\"1.1.0\">\n  <groupId>com.acme</groupId>\n</metadata>\n")
	resp := hs.serve(http.MethodPut, target, body, nil, true)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("snapshot metadata PUT = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	if b := drain(t, resp); len(b) != 0 {
		t.Errorf("202 body = %q, want 0 bytes (wire f1: empty)", b)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		t.Errorf("202 Content-Type = %q, want none (wire f1)", ct)
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		t.Errorf("202 Location = %q, want none", loc)
	}

	// No client bytes landed: the store's node at the path holds the
	// server-computed document (the calculator's own), byte-identical to
	// the pre-PUT baseline — never the uploaded body (wire f2a==f2b, not
	// even a lastUpdated churn).
	rc, node, err := hs.svc.Get(t.Context(), adminP, "maven-local", snapDir+"maven-metadata.xml")
	if err != nil {
		t.Fatalf("stored document gone after the discard: %v", err)
	}
	stored := drainRaw(t, rc)
	if string(stored) != baseline {
		t.Fatalf("stored document is not the server baseline:\nstored:  %s\nbaseline: %s", stored, baseline)
	}
	if node.Sha256 == sha256Hex(body) {
		t.Fatal("the discarded client body's digest is the stored node's")
	}
	if status, after := hs.getMeta("maven-local", "com.acme", "demo-app", "1.0-SNAPSHOT"); status != http.StatusOK || after != baseline {
		t.Fatalf("readback drifted across the discard (status %d)\nbefore: %s\nafter:  %s", status, baseline, after)
	}

	// The module document's PUT keeps the store chain (the C1 level
	// split): 201 with the ItemCreated envelope, stored, recomputed.
	module := hs.serve(http.MethodPut, "/maven-local/com/acme/demo-app/maven-metadata.xml", body, nil, true)
	if module.StatusCode != http.StatusCreated || module.Header.Get("Content-Type") == "" || len(drain(t, module)) == 0 {
		t.Fatalf("module metadata PUT = %d, want the 201 store chain", module.StatusCode)
	}

	// Refusal corners keep their shapes: remote PUT stays the 405 the
	// service's write refusal renders.
	remote := hs.serve(http.MethodPut, "/maven-remote/com/acme/up/1.0-SNAPSHOT/maven-metadata.xml", body, nil, true)
	if remote.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("remote metadata PUT = %d, want 405 (%s)", remote.StatusCode, drain(t, remote))
	}
	// Un-routed virtual stays the C5 405 with its fixed wording.
	virt := hs.serve(http.MethodPut, "/maven-virtual/com/acme/x/1.0-SNAPSHOT/maven-metadata.xml", body, nil, true)
	if virt.StatusCode != http.StatusMethodNotAllowed ||
		!strings.Contains(string(drain(t, virt)), "No local repository was configured as local deployment repository") {
		t.Fatalf("un-routed virtual metadata PUT = %d, want the C5 405", virt.StatusCode)
	}
	// Anonymous stays the 401 challenge (the mounted chain demands a
	// credential for writes; the bare handler mirrors the service shape).
	anon := hs.serve(http.MethodPut, target, body, nil, false)
	if anon.StatusCode != http.StatusUnauthorized || anon.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("anonymous metadata PUT = %d (WWW-Authenticate %q), want 401 + challenge",
			anon.StatusCode, anon.Header.Get("WWW-Authenticate"))
	}
	// An authenticated principal without the write grant stays 403 — the
	// acceptance is not an authorization bypass.
	ctx := t.Context()
	dev := &auth.Principal{Name: "spectator"}
	if err := hs.md.Permissions().PutTarget(ctx, &metadata.PermissionTarget{
		Name: "spec-local", Repos: `["maven-local"]`, Includes: "[]", Excludes: "[]",
	}, []*metadata.PermissionPrincipal{{
		TargetName: "spec-local", Principal: "spectator", PrincipalType: "user",
		CanRead: true, CanWrite: false, CanDelete: false,
	}}); err != nil {
		t.Fatalf("seed permission: %v", err)
	}
	denied := hs.serveAs(http.MethodPut, target, body, nil, dev)
	if denied.StatusCode != http.StatusForbidden ||
		string(drain(t, denied)) != fmt.Sprintf("{\n  \"errors\": [\n    {\n      \"status\": %d,\n      \"message\": \"write maven-local/com/acme/demo-app/1.0-SNAPSHOT/maven-metadata.xml: permission denied\"\n    }\n  ]\n}\n", http.StatusForbidden) {
		t.Fatalf("write-less metadata PUT = %d %s, want the store chain's 403 envelope",
			denied.StatusCode, drain(t, denied))
	}
}

// TestMetadataRenderAForm is face 2: the served documents render the A
// form — `<metadata modelVersion="1.1.0">`, the versioning order per
// family (module: latest, release, versions, lastUpdated; snapshot dir:
// lastUpdated, snapshot, snapshotVersions), and the `<version>` tail —
// verified element-by-element in exact linear order.
func TestMetadataRenderAForm(t *testing.T) {
	hs := newHarness(t)
	defer hs.waitCalc()

	// The wire f2a fixture shape: one unique snapshot pom (the
	// timestamp/buildNumber source), pinned facts.
	dir := "com/acme/demo-app/1.0-SNAPSHOT/"
	hs.seedNode("maven-local", dir+"demo-app-1.0-20260913.220231-1.pom", []byte("p"))
	hs.recalc("maven-local", "com.acme", "demo-app", "1.0-SNAPSHOT")
	_, snap := hs.getMeta("maven-local", "com.acme", "demo-app", "1.0-SNAPSHOT")
	// The A-form skeleton (wire f2a/f2b element order; the lastUpdated
	// stamp is the recalculation's, so only the ORDER is pinned here).
	wantOrder := []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<metadata modelVersion="1.1.0">`,
		`  <groupId>com.acme</groupId>`,
		`  <artifactId>demo-app</artifactId>`,
		`  <versioning>`,
		`    <lastUpdated>`,
		`    <snapshot>`,
		`      <timestamp>20260913.220231</timestamp>`,
		`      <buildNumber>1</buildNumber>`,
		`    </snapshot>`,
		`    <snapshotVersions>`,
		`      <snapshotVersion>`,
		`        <extension>pom</extension>`,
		`        <value>1.0-20260913.220231-1</value>`,
		`        <updated>`,
		`      </snapshotVersion>`,
		`    </snapshotVersions>`,
		`  </versioning>`,
		`  <version>1.0-SNAPSHOT</version>`,
		`</metadata>`,
	}
	assertOrderedLines(t, "snapshot document", snap, wantOrder)

	// Module document (L014-2 a1/a3 wire): versioning order latest,
	// release, versions, lastUpdated and the `<version>` = latest tail.
	hs.seedNode("maven-local", "com/acme/demo-app/1.0.0/demo-app-1.0.0.pom", []byte("p"))
	hs.seedNode("maven-local", "com/acme/demo-app/2.0.0-SNAPSHOT/demo-app-2.0.0-SNAPSHOT.pom", []byte("p"))
	hs.recalc("maven-local", "com.acme", "demo-app", "")
	_, mod := hs.getMeta("maven-local", "com.acme", "demo-app", "")
	wantMod := []string{
		`<metadata modelVersion="1.1.0">`,
		`  <groupId>com.acme</groupId>`,
		`  <artifactId>demo-app</artifactId>`,
		`  <versioning>`,
		`    <latest>2.0.0-SNAPSHOT</latest>`,
		`    <release>1.0.0</release>`,
		`    <versions>`,
		`      <version>1.0.0</version>`,
		`      <version>2.0.0-SNAPSHOT</version>`,
		`    </versions>`,
		`    <lastUpdated>`,
		`  </versioning>`,
		`  <version>2.0.0-SNAPSHOT</version>`,
		`</metadata>`,
	}
	assertOrderedLines(t, "module document", mod, wantMod)
}

// assertOrderedLines walks want as a subsequence of body's lines, prefix
// matched, in order — a wire-form pin that tolerates the timestamp fields
// (any value) while fixing every element's position.
func assertOrderedLines(t *testing.T, what, body string, want []string) {
	t.Helper()
	lines := strings.Split(body, "\n")
	i := 0
	for _, w := range want {
		found := false
		for ; i < len(lines); i++ {
			if strings.HasPrefix(lines[i], w) {
				found = true
				i++
				break
			}
		}
		if !found {
			t.Fatalf("%s: missing/out-of-order %q in\n%s", what, w, body)
		}
	}
}

// TestFileNotFoundWordingAForm is face 3: the wire's three ghost arms all
// answer the A-form message "File not found.; Path: '<repo>:<path>'".
func TestFileNotFoundWordingAForm(t *testing.T) {
	hs := newHarness(t)
	defer hs.waitCalc()

	hs.seedNode("maven-local", "com/acme/demo-app/1.0-SNAPSHOT/demo-app-1.0-20260913.220231-1.pom", []byte("p"))

	for _, tc := range []struct{ label, path, wantPath string }{
		// f3a: ghost file inside an existing directory.
		{"ghost jar", "com/acme/demo-app/1.0-SNAPSHOT/demo-app-1.0-SNAPSHOT.jar",
			"com/acme/demo-app/1.0-SNAPSHOT/demo-app-1.0-SNAPSHOT.jar"},
		// f3b: the whole directory tree is a ghost.
		{"ghost dir", "com/acme/nosuch/1.0-SNAPSHOT/nosuch-1.0-SNAPSHOT.jar",
			"com/acme/nosuch/1.0-SNAPSHOT/nosuch-1.0-SNAPSHOT.jar"},
		// f3c: a metadata document the calculator never generated.
		{"ghost metadata", "com/acme/demo-app/2.0-SNAPSHOT/maven-metadata.xml",
			"com/acme/demo-app/2.0-SNAPSHOT/maven-metadata.xml"},
	} {
		resp := hs.serve(http.MethodGet, "/maven-local/"+tc.path, nil, nil, true)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s GET = %d, want 404", tc.label, resp.StatusCode)
		}
		wantMsg := "File not found.; Path: 'maven-local:" + tc.wantPath + "'"
		wantBody := fmt.Sprintf("{\n  \"errors\": [\n    {\n      \"status\": %d,\n      \"message\": %q\n    }\n  ]\n}\n",
			http.StatusNotFound, wantMsg)
		if got := string(drain(t, resp)); got != wantBody {
			t.Errorf("%s 404 body = %s, want\n%s", tc.label, got, wantBody)
		}
	}
}
