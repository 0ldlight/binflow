package npm

import (
	"net/http"
	"strings"
	"testing"
)

// seedPackage publishes one package with two versions through the adapter's
// own publish path (the reads are then driven by what real writes stored).
func seedPackage(t *testing.T, s *stack, name string, versions ...string) {
	t.Helper()
	for _, v := range versions {
		doc := publishDoc(name, v, "TARBALL-"+name+"-"+v, nil, nil)
		target := "/npm-local/" + name
		if strings.HasPrefix(name, "@") {
			target = "/npm-local/" + strings.Replace(name, "/", "%2F", 1)
		}
		if rr := s.call(http.MethodPut, target, mustJSON(doc), adminPrincipal, nil); rr.Code != http.StatusCreated {
			t.Fatalf("seed publish %s@%s: status %d; body=%s", name, v, rr.Code, bodyOf(rr))
		}
	}
}

// TestDistTagsModernFamily exercises the npm >= 8 routes: ls (GET the
// collection), add (PUT one tag with a JSON-string body), rm (DELETE).
func TestDistTagsModernFamily(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "demo-pkg", "1.0.0", "1.1.0")

	// ls
	rr := s.call(http.MethodGet, "/npm-local/-/package/demo-pkg/dist-tags", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("ls status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if !strings.Contains(bodyOf(rr), `"latest":"1.1.0"`) {
		t.Fatalf("ls body = %s", bodyOf(rr))
	}

	// add beta -> 1.0.0
	rr = s.call(http.MethodPut, "/npm-local/-/package/demo-pkg/dist-tags/beta", `"1.0.0"`, adminPrincipal, nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("add status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if !strings.Contains(bodyOf(rr), `"ok":"created new tag"`) {
		t.Fatalf("add body = %s, want the pinned 201 wording", bodyOf(rr))
	}
	rr = s.call(http.MethodGet, "/npm-local/demo-pkg", "", adminPrincipal, nil)
	if !strings.Contains(bodyOf(rr), `"beta":"1.0.0"`) {
		t.Fatalf("beta not in packument: %s", bodyOf(rr))
	}

	// add pointing at a missing version is the pinned 404
	rr = s.call(http.MethodPut, "/npm-local/-/package/demo-pkg/dist-tags/next", `"9.9.9"`, adminPrincipal, nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("bad version status = %d; body=%s", rr.Code, bodyOf(rr))
	}

	// rm
	rr = s.call(http.MethodDelete, "/npm-local/-/package/demo-pkg/dist-tags/beta", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("rm status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if bodyOf(rr) != "" {
		t.Fatalf("rm body = %q, want empty", bodyOf(rr))
	}

	// rm of a missing tag is the pinned 404 wording
	rr = s.call(http.MethodDelete, "/npm-local/-/package/demo-pkg/dist-tags/beta", "", adminPrincipal, nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("rm missing status = %d", rr.Code)
	}
	if !strings.Contains(bodyOf(rr), "npm package not found with name:demo-pkg, and tag:beta") {
		t.Fatalf("rm missing wording = %s", bodyOf(rr))
	}

	// ls of an unknown package is 404
	rr = s.call(http.MethodGet, "/npm-local/-/package/no-such/dist-tags", "", adminPrincipal, nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("ls unknown status = %d", rr.Code)
	}
}

// TestDistTagsScopedName: the /-/package family addresses scoped names with
// the percent-encoded separator; npm spells escapedName exactly this way.
func TestDistTagsScopedName(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "@acme/util", "1.0.0")

	rr := s.call(http.MethodPut, "/npm-local/-/package/@acme%2Futil/dist-tags/edge", `"1.0.0"`, adminPrincipal, nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("scoped add status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	rr = s.call(http.MethodGet, "/npm-local/-/package/@acme%2futil/dist-tags", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("scoped ls status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if !strings.Contains(bodyOf(rr), `"edge":"1.0.0"`) {
		t.Fatalf("scoped ls body = %s", bodyOf(rr))
	}
}

// TestDistTagsLegacyPut: the pre-npm-8 spelling PUT /<name>/<tag> shares the
// semantics and the 201 wording.
func TestDistTagsLegacyPut(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "demo-pkg", "1.0.0")

	rr := s.call(http.MethodPut, "/npm-local/demo-pkg/legacy", `"1.0.0"`, adminPrincipal, nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("legacy put status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if !strings.Contains(bodyOf(rr), `"ok":"created new tag"`) {
		t.Fatalf("legacy put body = %s", bodyOf(rr))
	}
	rr = s.call(http.MethodGet, "/npm-local/demo-pkg", "", adminPrincipal, nil)
	if !strings.Contains(bodyOf(rr), `"legacy":"1.0.0"`) {
		t.Fatalf("legacy tag missing from packument: %s", bodyOf(rr))
	}
}

// TestDistTagsBulkPut: the collection PUT merges a whole tag map (the old
// registry contract some tooling still uses).
func TestDistTagsBulkPut(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "demo-pkg", "1.0.0", "1.1.0")

	body := `{"latest":"1.1.0","canary":"1.0.0"}`
	rr := s.call(http.MethodPut, "/npm-local/-/package/demo-pkg/dist-tags", body, adminPrincipal, nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("bulk status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	rr = s.call(http.MethodGet, "/npm-local/-/package/demo-pkg/dist-tags", "", adminPrincipal, nil)
	for _, want := range []string{`"latest":"1.1.0"`, `"canary":"1.0.0"`} {
		if !strings.Contains(bodyOf(rr), want) {
			t.Fatalf("bulk result %s missing %s", bodyOf(rr), want)
		}
	}
}
