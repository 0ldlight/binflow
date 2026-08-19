package npm

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestPublishTenStepChain walks the spec's validation order (section 2.3):
// every failure branch answers its pinned status and wording before the
// next step runs. Each case publishes from a fresh stack unless the step
// needs prior state (duplicate, deprecate).
func TestPublishTenStepChain(t *testing.T) {
	tarball := "TARBALL-BYTES-1.0.0"

	t.Run("step1 body not JSON", func(t *testing.T) {
		s := newStack(t)
		rr := s.call(http.MethodPut, "/npm-local/demo-pkg", "{not json", adminPrincipal, nil)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rr.Code, bodyOf(rr))
		}
		if !strings.Contains(bodyOf(rr), "invalid npm publish document") {
			t.Fatalf("body %q missing the parse wording", bodyOf(rr))
		}
	})

	t.Run("step2 attachments without versions", func(t *testing.T) {
		s := newStack(t)
		doc := publishDoc("demo-pkg", "1.0.0", tarball, nil, nil)
		delete(doc, "versions")
		rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(doc), adminPrincipal, nil)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rr.Code, bodyOf(rr))
		}
		if !strings.Contains(bodyOf(rr), "Missing versions in npm package") {
			t.Fatalf("body %q missing the pinned wording", bodyOf(rr))
		}
	})

	t.Run("step3 no write permission on the tarball path", func(t *testing.T) {
		s := newStack(t)
		doc := publishDoc("demo-pkg", "1.0.0", tarball, nil, nil)
		rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(doc), develPrincipal, nil)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body=%s", rr.Code, bodyOf(rr))
		}
		want := "Cannot deploy to 'demo-pkg/-/demo-pkg-1.0.0.tgz'"
		if !strings.Contains(bodyOf(rr), want) {
			t.Fatalf("body %q missing %q", bodyOf(rr), want)
		}
	})

	t.Run("step4 duplicate version is 403 not 409", func(t *testing.T) {
		s := newStack(t)
		doc := publishDoc("demo-pkg", "1.0.0", tarball, nil, nil)
		if rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(doc), adminPrincipal, nil); rr.Code != http.StatusCreated {
			t.Fatalf("first publish status = %d; body=%s", rr.Code, bodyOf(rr))
		}
		rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(doc), adminPrincipal, nil)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("republish status = %d, want 403 (v1.1 Q7 ruling); body=%s", rr.Code, bodyOf(rr))
		}
		want := "Cannot modify pre-existing version '1.0.0', aborting upload for: 'demo-pkg'"
		if !strings.Contains(bodyOf(rr), want) {
			t.Fatalf("body %q missing %q", bodyOf(rr), want)
		}
	})

	t.Run("step5 invalid version spelling", func(t *testing.T) {
		s := newStack(t)
		doc := publishDoc("demo-pkg", "1.02.3", tarball, nil, nil)
		rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(doc), adminPrincipal, nil)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rr.Code, bodyOf(rr))
		}
		if !strings.Contains(bodyOf(rr), "Invalid Version: '1.02.3'") {
			t.Fatalf("body %q missing the Invalid Version wording", bodyOf(rr))
		}
	})

	t.Run("step5 invalid package name charset", func(t *testing.T) {
		s := newStack(t)
		doc := publishDoc("demo pkg", "1.0.0", tarball, nil, nil)
		rr := s.call(http.MethodPut, "/npm-local/demo%20pkg", mustJSON(doc), adminPrincipal, nil)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rr.Code, bodyOf(rr))
		}
		if !strings.Contains(bodyOf(rr), "illegal character") {
			t.Fatalf("body %q missing the charset wording", bodyOf(rr))
		}
	})

	t.Run("step6 deprecate flow answers 201 updated package", func(t *testing.T) {
		s := newStack(t)
		pub := publishDoc("demo-pkg", "1.0.0", tarball, nil, nil)
		if rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(pub), adminPrincipal, nil); rr.Code != http.StatusCreated {
			t.Fatalf("publish status = %d; body=%s", rr.Code, bodyOf(rr))
		}
		dep := map[string]any{
			"name": "demo-pkg", "_id": "demo-pkg",
			"versions": map[string]any{
				"1.0.0": map[string]any{"name": "demo-pkg", "version": "1.0.0", "deprecated": "gone"},
			},
		}
		rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(dep), adminPrincipal, nil)
		if rr.Code != http.StatusCreated {
			t.Fatalf("deprecate status = %d, want 201; body=%s", rr.Code, bodyOf(rr))
		}
		var body map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("body not JSON: %v", err)
		}
		if body["ok"] != "updated package" {
			t.Fatalf("ok = %v, want 'updated package'", body["ok"])
		}
		// The marker stuck.
		got := s.call(http.MethodGet, "/npm-local/demo-pkg/1.0.0", "", adminPrincipal, nil)
		if !strings.Contains(bodyOf(got), `"deprecated":"gone"`) {
			t.Fatalf("deprecated marker not applied: %s", bodyOf(got))
		}
	})

	t.Run("step7 no attachments and no deprecate", func(t *testing.T) {
		s := newStack(t)
		doc := map[string]any{
			"name": "demo-pkg", "_id": "demo-pkg",
			"versions": map[string]any{
				"1.0.0": map[string]any{"name": "demo-pkg", "version": "1.0.0"},
			},
		}
		rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(doc), adminPrincipal, nil)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rr.Code, bodyOf(rr))
		}
		if !strings.Contains(bodyOf(rr), "Missing attachments with tarball data") {
			t.Fatalf("body %q missing the pinned wording", bodyOf(rr))
		}
	})

	t.Run("M26 packument replay is the 403 conflict", func(t *testing.T) {
		s := newStack(t)
		pub := publishDoc("demo-pkg", "1.0.0", tarball, nil, nil)
		if rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(pub), adminPrincipal, nil); rr.Code != http.StatusCreated {
			t.Fatalf("publish status = %d; body=%s", rr.Code, bodyOf(rr))
		}
		// The GET face strips _attachments; PUTting the served document
		// back is the duplicate-publish probe of PRD M26.
		served := s.call(http.MethodGet, "/npm-local/demo-pkg", "", adminPrincipal, nil)
		rr := s.call(http.MethodPut, "/npm-local/demo-pkg", bodyOf(served), adminPrincipal,
			map[string]string{"Content-Type": "application/json"})
		if rr.Code != http.StatusForbidden {
			t.Fatalf("replay status = %d, want 403; body=%s", rr.Code, bodyOf(rr))
		}
		if !strings.Contains(bodyOf(rr), "Cannot modify pre-existing version '1.0.0'") {
			t.Fatalf("body %q missing the pinned wording", bodyOf(rr))
		}
	})

	t.Run("step8 integrity mismatch", func(t *testing.T) {
		s := newStack(t)
		doc := publishDoc("demo-pkg", "1.0.0", tarball, nil, map[string]any{
			"integrity": "sha512-AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		})
		rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(doc), adminPrincipal, nil)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rr.Code, bodyOf(rr))
		}
		if !strings.Contains(bodyOf(rr), "Conflict between integrity from metadata and tarball") {
			t.Fatalf("body %q missing the integrity wording", bodyOf(rr))
		}
	})

	t.Run("step9 shasum mismatch", func(t *testing.T) {
		s := newStack(t)
		doc := publishDoc("demo-pkg", "1.0.0", tarball, nil, map[string]any{
			"shasum": strings.Repeat("ab", 20),
		})
		rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(doc), adminPrincipal, nil)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rr.Code, bodyOf(rr))
		}
		if !strings.Contains(bodyOf(rr), "Conflict between sha1 from metadata and tarball") {
			t.Fatalf("body %q missing the sha1 wording", bodyOf(rr))
		}
	})

	t.Run("step10 success writes tarball and packument nodes", func(t *testing.T) {
		s := newStack(t)
		doc := publishDoc("demo-pkg", "1.0.0", tarball, nil, nil)
		rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(doc), adminPrincipal, nil)
		if rr.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body=%s", rr.Code, bodyOf(rr))
		}
		var body map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("body not JSON: %v", err)
		}
		if body["success"] != true {
			t.Fatalf("body = %v, want {\"success\":true}", body)
		}
		for _, p := range []string{"demo-pkg/-/demo-pkg-1.0.0.tgz", "demo-pkg/packument.json"} {
			if _, _, err := s.svc.Get(t.Context(), adminPrincipal, "npm-local", p); err != nil {
				t.Fatalf("node %s missing after publish: %v", p, err)
			}
		}
	})
}

// TestPublishSecondVersionMerges: publishing 1.1.0 after 1.0.0 keeps both
// versions, moves latest and stamps time (the merge semantics of storage
// face AC storage ②).
func TestPublishSecondVersionMerges(t *testing.T) {
	s := newStack(t)
	for _, v := range []string{"1.0.0", "1.1.0"} {
		doc := publishDoc("demo-pkg", v, "TARBALL-"+v, nil, nil)
		if rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(doc), adminPrincipal, nil); rr.Code != http.StatusCreated {
			t.Fatalf("publish %s status = %d; body=%s", v, rr.Code, bodyOf(rr))
		}
	}
	rr := s.call(http.MethodGet, "/npm-local/demo-pkg", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("packument status = %d", rr.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("packument not JSON: %v", err)
	}
	tags := doc["dist-tags"].(map[string]any)
	if tags["latest"] != "1.1.0" {
		t.Fatalf("latest = %v, want 1.1.0", tags["latest"])
	}
	versions := doc["versions"].(map[string]any)
	if len(versions) != 2 {
		t.Fatalf("versions = %v, want both 1.0.0 and 1.1.0", versions)
	}
}

// TestPublishScopedLayout: the scoped package lands at the C8 layout path
// and both scope-separator spellings address the same package.
func TestPublishScopedLayout(t *testing.T) {
	s := newStack(t)
	doc := publishDoc("@acme/util", "1.0.0", "SCOPED-TARBALL", nil, nil)
	rr := s.call(http.MethodPut, "/npm-local/@acme%2Futil", mustJSON(doc), adminPrincipal, nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("publish status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	const node = "@acme/util/-/@acme/util-1.0.0.tgz"
	if _, _, err := s.svc.Get(t.Context(), adminPrincipal, "npm-local", node); err != nil {
		t.Fatalf("scoped tarball missing at %s: %v", node, err)
	}
	for _, enc := range []string{"%2f", "%2F"} {
		rr := s.call(http.MethodGet, "/npm-local/@acme"+enc+"util", "", adminPrincipal, nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET with %s: status = %d; body=%s", enc, rr.Code, bodyOf(rr))
		}
		if !strings.Contains(bodyOf(rr), `"name":"@acme/util"`) {
			t.Fatalf("GET with %s served the wrong package: %s", enc, bodyOf(rr))
		}
	}
}

// TestPublishTraversalVariants (NFR-S18): dot segments — raw, percent-encoded
// and inside a decoded scope separator — die at the layout defense with 400,
// never reaching storage.
func TestPublishTraversalVariants(t *testing.T) {
	s := newStack(t)
	for _, target := range []string{
		"/npm-local/../etc/passwd",
		"/npm-local/%2e%2e/evil",
		"/npm-local/demo-pkg/../../evil",
		"/npm-local/@scope%2F..",
		"/npm-local/@scope%2f..%2fother",
	} {
		rr := s.call(http.MethodPut, target, "{}", adminPrincipal, nil)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400; body=%s", target, rr.Code, bodyOf(rr))
		}
	}
}

// TestPublishNameCharset: names outside the finite charset are 400 even when
// the path itself is legal (the semantic layer above the layout defense).
func TestPublishNameCharset(t *testing.T) {
	s := newStack(t)
	for _, name := range []string{
		"demo;pkg", "demo(p)", "demo=pkg", "demo|pkg", "demo'pkg",
	} {
		doc := publishDoc(name, "1.0.0", "X", nil, nil)
		rr := s.call(http.MethodPut, "/npm-local/"+name, mustJSON(doc), adminPrincipal, nil)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("name %q: status = %d, want 400; body=%s", name, rr.Code, bodyOf(rr))
		}
	}
}
