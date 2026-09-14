package npm

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Ghost-publish guard (L016-2; evidence L015 N4/N4' g2): the duplicate
// predicate is the TARBALL PATH derived from the _attachments name, not the
// declared version. A document whose versions/attachment names disagree is
// the ghost taxonomy; every real npm publish agrees and reduces to the
// canonical layout path.

// ghostDoc builds a publish document whose _attachments key is attName —
// version and attachment name deliberately decoupled. dist carries honest
// digests of the tarball so the integrity/shasum steps stay out of the way.
func ghostDoc(name, version, attName, tarball string, extraVersions map[string]string) map[string]any {
	sha1Sum, _, sha512Sum := digestTriple([]byte(tarball))
	versionDoc := func(v string) map[string]any {
		return map[string]any{
			"_id": name + "@" + v, "name": name, "version": v,
			"dist": map[string]any{
				"integrity": "sha512-" + base64RawURL(sha512Sum),
				"shasum":    hexEncode(sha1Sum),
				"tarball":   "http://registry.test/binflow/api/npm/npm-local/" + tarballPath(name, v),
			},
		}
	}
	versions := map[string]any{version: versionDoc(version)}
	for v := range extraVersions {
		versions[v] = versionDoc(v)
	}
	return map[string]any{
		"_id": name, "name": name,
		"dist-tags": map[string]any{"latest": version},
		"versions":  versions,
		"_attachments": map[string]any{
			attName: map[string]any{
				"content_type": "application/octet-stream",
				"data":         base64Std([]byte(tarball)),
				"length":       len(tarball),
			},
		},
	}
}

// packumentVersions fetches the served packument (nil when not 200).
func packumentVersions(t *testing.T, s *stack, pkg string) (map[string]any, map[string]any) {
	t.Helper()
	rr := s.call(http.MethodGet, "/npm-local/"+pkg, "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		return nil, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("packument not JSON: %v", err)
	}
	return mapOf(doc["versions"]), mapOf(doc["dist-tags"])
}

// The N4 arm: versions declare 9.9.9, the attachment carries the already
// stored 1.0.0 filename — the pinned 403 cites 9.9.9, the ghost version never
// enters the index, latest is not hijacked, no ghost-path node appears.
func TestPublishGhostGuardAttachmentPathConflict(t *testing.T) {
	s := newStack(t)
	base := publishDoc("demo-pkg", "1.0.0", "TARBALL-1.0.0", nil, nil)
	if rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(base), adminPrincipal, nil); rr.Code != http.StatusCreated {
		t.Fatalf("setup publish status = %d; body=%s", rr.Code, bodyOf(rr))
	}

	ghost := ghostDoc("demo-pkg", "9.9.9", "demo-pkg-1.0.0.tgz", "TARBALL-1.0.0", nil)
	rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(ghost), adminPrincipal, nil)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("ghost publish status = %d, want 403; body=%s", rr.Code, bodyOf(rr))
	}
	want := "Cannot modify pre-existing version '9.9.9', aborting upload for: 'demo-pkg'"
	if !strings.Contains(bodyOf(rr), want) {
		t.Fatalf("body %q missing the pinned wording %q", bodyOf(rr), want)
	}

	versions, tags := packumentVersions(t, s, "demo-pkg")
	if _, ok := versions["9.9.9"]; ok {
		t.Fatalf("ghost version entered the index: %v", versions)
	}
	if len(versions) != 1 || versions["1.0.0"] == nil {
		t.Fatalf("versions = %v, want only 1.0.0", versions)
	}
	if tags["latest"] != "1.0.0" {
		t.Fatalf("latest = %q, want 1.0.0 (not hijacked)", tags["latest"])
	}
	if _, _, err := s.svc.Get(t.Context(), adminPrincipal, "npm-local", "demo-pkg/-/demo-pkg-9.9.9.tgz"); err == nil {
		t.Fatalf("ghost-path tarball node exists; the attachment bytes must not be stored")
	}
}

// The G2 arm: versions carry a pre-existing version plus a new one, the
// attachment names the NEW version — no tarball path conflicts, the publish
// proceeds and appends only the new version.
func TestPublishGhostGuardMixedVersionsAppend(t *testing.T) {
	s := newStack(t)
	base := publishDoc("demo-pkg", "1.0.0", "TARBALL-1.0.0", nil, nil)
	if rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(base), adminPrincipal, nil); rr.Code != http.StatusCreated {
		t.Fatalf("setup publish status = %d; body=%s", rr.Code, bodyOf(rr))
	}

	doc := ghostDoc("demo-pkg", "2.0.0", "demo-pkg-2.0.0.tgz", "TARBALL-2.0.0", map[string]string{"1.0.0": ""})
	rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(doc), adminPrincipal, nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("mixed publish status = %d, want 201; body=%s", rr.Code, bodyOf(rr))
	}
	versions, tags := packumentVersions(t, s, "demo-pkg")
	if len(versions) != 2 || versions["1.0.0"] == nil || versions["2.0.0"] == nil {
		t.Fatalf("versions = %v, want 1.0.0 and 2.0.0", versions)
	}
	if tags["latest"] != "2.0.0" {
		t.Fatalf("latest = %q, want 2.0.0", tags["latest"])
	}
}

// The G3 arm: attachment names a version that is neither declared nor stored
// — no conflict, the bytes land at the ATTACHMENT path (the wire filename the
// download face serves) and dist.tarball points there.
func TestPublishGhostGuardMismatchedNameStoresAtAttachmentPath(t *testing.T) {
	s := newStack(t)
	base := publishDoc("demo-pkg", "1.0.0", "TARBALL-1.0.0", nil, nil)
	if rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(base), adminPrincipal, nil); rr.Code != http.StatusCreated {
		t.Fatalf("setup publish status = %d; body=%s", rr.Code, bodyOf(rr))
	}

	doc := ghostDoc("demo-pkg", "9.9.9", "demo-pkg-7.7.7.tgz", "TARBALL-7.7.7", nil)
	rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(doc), adminPrincipal, nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("mismatched publish status = %d, want 201; body=%s", rr.Code, bodyOf(rr))
	}

	// The served dist.tarball references the attachment path, and that path
	// serves the bytes; the version-canonical path does not exist.
	rr = s.call(http.MethodGet, "/npm-local/demo-pkg/9.9.9", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK || !strings.Contains(bodyOf(rr), "demo-pkg-7.7.7.tgz") {
		t.Fatalf("version manifest dist.tarball = %s, want the attachment path", bodyOf(rr))
	}
	rr = s.call(http.MethodGet, "/npm-local/demo-pkg/-/demo-pkg-7.7.7.tgz", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("attachment-path tarball status = %d, want 200", rr.Code)
	}
	if !strings.Contains(bodyOf(rr), "TARBALL-7.7.7") {
		t.Fatalf("attachment-path tarball bytes = %q", bodyOf(rr))
	}
	if rr := s.call(http.MethodGet, "/npm-local/demo-pkg/-/demo-pkg-9.9.9.tgz", "", adminPrincipal, nil); rr.Code != http.StatusNotFound {
		t.Fatalf("version-canonical path status = %d, want 404", rr.Code)
	}
}

// The attachment name is client-controlled body data becoming a storage path:
// dot segments die at 400, nothing is stored.
func TestPublishAttachmentNameTraversal(t *testing.T) {
	cases := []string{"../evil.tgz", "a/../../evil.tgz", "..", ".", "a//b.tgz", "/abs.tgz"}
	for _, att := range cases {
		s := newStack(t)
		doc := ghostDoc("demo-pkg", "1.0.0", att, "TARBALL", nil)
		rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(doc), adminPrincipal, nil)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("attachment %q: status = %d, want 400; body=%s", att, rr.Code, bodyOf(rr))
		}
		if !strings.Contains(bodyOf(rr), "invalid attachment name") {
			t.Fatalf("attachment %q: body %q missing the wording", att, bodyOf(rr))
		}
		if _, _, err := s.svc.Get(t.Context(), adminPrincipal, "npm-local", "demo-pkg/packument.json"); err == nil {
			t.Fatalf("attachment %q: packument stored despite rejection", att)
		}
	}
}
