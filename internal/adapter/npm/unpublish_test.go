package npm

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestRevPutPlaceholderIsFakeSuccess (M27-AC11): PUT /<name>/-rev/<arbitrary
// rev> with a placeholder body answers the pinned 200 body and leaves the
// package byte-identical.
func TestRevPutPlaceholderIsFakeSuccess(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "demo-pkg", "1.0.0")

	before := s.call(http.MethodGet, "/npm-local/demo-pkg", "", adminPrincipal, nil).Body.String()
	rr := s.call(http.MethodPut, "/npm-local/demo-pkg/-rev/00000000000000000000000000000000",
		`"0-0000000000000000000000000000000"`, adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, bodyOf(rr))
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if body["ok"] != "updated package" {
		t.Fatalf("ok = %v, want 'updated package'", body["ok"])
	}
	after := s.call(http.MethodGet, "/npm-local/demo-pkg", "", adminPrincipal, nil).Body.String()
	if before != after {
		t.Fatalf("placeholder PUT changed the package:\nbefore=%s\nafter=%s", before, after)
	}
}

// TestRevPutPackumentBodyApplies: the npm 10 single-version unpublish PUTs
// the full modified packument (verified against libnpmpublish 10.9.8); the
// server applies it while keeping the pinned 200 body.
func TestRevPutPackumentBodyApplies(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "demo-pkg", "1.0.0", "1.0.1")

	// The client shape: the packument it GETs, minus the version, with the
	// dist-tags cleaned.
	doc := s.call(http.MethodGet, "/npm-local/demo-pkg", "", adminPrincipal, nil).Body.String()
	var m map[string]any
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatalf("packument not JSON: %v", err)
	}
	versions := m["versions"].(map[string]any)
	delete(versions, "1.0.1")
	tags := m["dist-tags"].(map[string]any)
	delete(tags, "latest")
	tags["latest"] = "1.0.0"
	delete(m, "_attachments")

	rr := s.call(http.MethodPut, "/npm-local/demo-pkg/-rev/whatever", mustJSON(m), adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if !strings.Contains(bodyOf(rr), "updated package") {
		t.Fatalf("body = %s", bodyOf(rr))
	}
	got := s.call(http.MethodGet, "/npm-local/demo-pkg", "", adminPrincipal, nil).Body.String()
	var after map[string]any
	if err := json.Unmarshal([]byte(got), &after); err != nil {
		t.Fatalf("packument not JSON after apply: %v", err)
	}
	if versions := after["versions"].(map[string]any); len(versions) != 1 {
		t.Fatalf("versions after apply = %v, want only 1.0.0: %s", versions, got)
	}
	if tags := after["dist-tags"].(map[string]any); tags["latest"] != "1.0.0" {
		t.Fatalf("latest after apply = %v, want 1.0.0: %s", tags["latest"], got)
	}
}

// TestUnpublishSingleVersion: the tarball DELETE removes the version entry,
// its dist-tags and the tarball node; the sibling version stays installable
// (M27).
func TestUnpublishSingleVersion(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "demo-pkg", "1.0.0", "1.0.1")
	if rr := s.call(http.MethodPut, "/npm-local/-/package/demo-pkg/dist-tags/beta", `"1.0.1"`, adminPrincipal, nil); rr.Code != http.StatusCreated {
		t.Fatalf("tag beta: %d %s", rr.Code, bodyOf(rr))
	}

	target := "/npm-local/demo-pkg/-/demo-pkg-1.0.1.tgz/-rev/17"
	rr := s.call(http.MethodDelete, target, "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, bodyOf(rr))
	}

	got := s.call(http.MethodGet, "/npm-local/demo-pkg", "", adminPrincipal, nil).Body.String()
	if strings.Contains(got, `"1.0.1"`) {
		t.Fatalf("version entry not removed: %s", got)
	}
	if strings.Contains(got, `"beta"`) {
		t.Fatalf("beta tag not cleaned: %s", got)
	}
	// The tarball node is gone; the sibling tarball survives.
	if rr := s.call(http.MethodGet, "/npm-local/demo-pkg/-/demo-pkg-1.0.1.tgz", "", adminPrincipal, nil); rr.Code != http.StatusNotFound {
		t.Fatalf("unpublished tarball status = %d, want 404", rr.Code)
	}
	if rr := s.call(http.MethodGet, "/npm-local/demo-pkg/-/demo-pkg-1.0.0.tgz", "", adminPrincipal, nil); rr.Code != http.StatusOK {
		t.Fatalf("sibling tarball status = %d, want 200", rr.Code)
	}
}

// TestUnpublishWholePackage: the package DELETE drops every tarball and the
// packument; a second DELETE is 404.
func TestUnpublishWholePackage(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "demo-pkg", "1.0.0", "1.1.0")

	rr := s.call(http.MethodDelete, "/npm-local/demo-pkg/-rev/2-abc", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	for _, p := range []string{
		"/npm-local/demo-pkg",
		"/npm-local/demo-pkg/-/demo-pkg-1.0.0.tgz",
		"/npm-local/demo-pkg/-/demo-pkg-1.1.0.tgz",
	} {
		if rr := s.call(http.MethodGet, p, "", adminPrincipal, nil); rr.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", p, rr.Code)
		}
	}
	if rr := s.call(http.MethodDelete, "/npm-local/demo-pkg/-rev/3-abc", "", adminPrincipal, nil); rr.Code != http.StatusNotFound {
		t.Fatalf("repeat delete status = %d, want 404", rr.Code)
	}
}

// TestUnpublishScoped: the scoped tarball-path spelling npm derives from the
// rewritten dist.tarball URL removes the right version.
func TestUnpublishScoped(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "@acme/util", "1.0.0", "2.0.0")

	target := "/npm-local/@acme/util/-/@acme/util-2.0.0.tgz/-rev/1"
	rr := s.call(http.MethodDelete, target, "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	got := s.call(http.MethodGet, "/npm-local/@acme%2Futil", "", adminPrincipal, nil).Body.String()
	if strings.Contains(got, `"2.0.0"`) {
		t.Fatalf("scoped version not removed: %s", got)
	}
}
