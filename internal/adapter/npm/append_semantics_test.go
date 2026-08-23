package npm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// T-249 — packument append semantics. The packument PUT that adds a version
// is npm's STANDARD publish path ([NPM-API] publish) and must ride the write
// grant alone; rewriting already-published version data (a dist digest swap,
// a field overwrite, a version removal) keeps repo-semantics section 3's
// overwrite pair — the delete-permission demand. Version immutability itself
// lives at the tarball layer (spec section 2.3 step 4), which stays
// unconditional.

// seedGrant wires one non-admin principal with read+write on npm-local, with
// or without delete (the maven plane's TestMetadataOverwriteFreedom shape).
func seedGrant(t *testing.T, s *stack, name string, canDelete bool) *Principal {
	t.Helper()
	target := "t249-" + name
	if err := s.md.Permissions().PutTarget(t.Context(), &metadata.PermissionTarget{
		Name: target, Repos: `["npm-local"]`, Includes: "[]", Excludes: "[]",
	}, []*metadata.PermissionPrincipal{{
		TargetName: target, Principal: name, PrincipalType: "user",
		CanRead: true, CanWrite: true, CanDelete: canDelete,
	}}); err != nil {
		t.Fatalf("seed permission for %s: %v", name, err)
	}
	return &Principal{Name: name}
}

// docOf decodes one JSON document with the production decoder (numbers stay
// literal — the classifier's byte-identity rule keys on that).
func docOf(t *testing.T, js string) map[string]any {
	t.Helper()
	d, err := decodeDoc([]byte(js))
	if err != nil {
		t.Fatalf("decode %s: %v", js, err)
	}
	return d
}

// TestPackumentAppendOnlyClassifier is the pure-function table for the
// transition classifier: added versions and document-level bookkeeping
// (dist-tags, time, root metadata) never gate the decision; a removed
// version or a changed existing manifest does.
func TestPackumentAppendOnlyClassifier(t *testing.T) {
	const v100 = `{"name":"demo-pkg","version":"1.0.0","dist":{"shasum":"aa","tarball":"demo-pkg/-/demo-pkg-1.0.0.tgz"}}`
	tests := []struct {
		name string
		old  string // "" = nil (fresh package)
		new  string
		want bool
	}{
		{"fresh package", "", `{"versions":{"1.0.0":` + v100 + `}}`, true},
		{"identical documents", `{"versions":{"1.0.0":` + v100 + `}}`, `{"versions":{"1.0.0":` + v100 + `}}`, true},
		{"version appended", `{"versions":{"1.0.0":` + v100 + `}}`,
			`{"versions":{"1.0.0":` + v100 + `,"1.0.1":{"name":"demo-pkg","version":"1.0.1"}}}`, true},
		{"dist-tags time and root metadata may change",
			`{"dist-tags":{"latest":"1.0.0"},"time":{"created":"a"},"readme":"old","versions":{"1.0.0":` + v100 + `}}`,
			`{"dist-tags":{"latest":"1.0.1"},"time":{"created":"a","modified":"b"},"readme":"new","versions":{"1.0.0":` + v100 + `}}`, true},
		{"version removed", `{"versions":{"1.0.0":` + v100 + `,"1.0.1":{"name":"demo-pkg"}}}`,
			`{"versions":{"1.0.0":` + v100 + `}}`, false},
		{"existing dist digest swapped",
			`{"versions":{"1.0.0":` + v100 + `}}`,
			`{"versions":{"1.0.0":{"name":"demo-pkg","version":"1.0.0","dist":{"shasum":"bb","tarball":"demo-pkg/-/demo-pkg-1.0.0.tgz"}}}}`, false},
		{"existing manifest field overwritten (deprecated)",
			`{"versions":{"1.0.0":` + v100 + `}}`,
			`{"versions":{"1.0.0":{"name":"demo-pkg","version":"1.0.0","deprecated":"gone","dist":{"shasum":"aa","tarball":"demo-pkg/-/demo-pkg-1.0.0.tgz"}}}}`, false},
		{"number spelling drift is a rewrite (byte-identity rule)",
			`{"versions":{"1.0.0":{"x":1.0}}}`, `{"versions":{"1.0.0":{"x":1.00}}}`, false},
		{"versions map dropped", `{"versions":{"1.0.0":` + v100 + `}}`, `{"versions":{}}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var old map[string]any
			if tt.old != "" {
				old = docOf(t, tt.old)
			}
			if got := packumentAppendOnly(old, docOf(t, tt.new)); got != tt.want {
				t.Fatalf("packumentAppendOnly = %t, want %t", got, tt.want)
			}
		})
	}
}

// TestPublishAppendRidesWriteGrant is the T-247 regression: a CI principal
// with read+write and NO delete publishes two versions of one package back
// to back — both must be 201. Pre-T-249 the second publish died E403 on the
// packument save's overwrite arm (T-247 product finding P-1).
func TestPublishAppendRidesWriteGrant(t *testing.T) {
	s := newStack(t)
	ci := seedGrant(t, s, "ciwrite", false)

	for _, v := range []string{"1.0.0", "1.0.1"} {
		doc := publishDoc("binflow-ci-hello", v, "TARBALL-"+v, nil, nil)
		rr := s.call(http.MethodPut, "/npm-local/binflow-ci-hello", mustJSON(doc), ci, nil)
		if rr.Code != http.StatusCreated {
			t.Fatalf("publish %s as write-only CI = %d, want 201; body=%s", v, rr.Code, bodyOf(rr))
		}
	}

	rr := s.call(http.MethodGet, "/npm-local/binflow-ci-hello", "", ci, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("packument status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("packument not JSON: %v", err)
	}
	if versions := doc["versions"].(map[string]any); len(versions) != 2 {
		t.Fatalf("versions = %v, want both 1.0.0 and 1.0.1", versions)
	}
	if tags := doc["dist-tags"].(map[string]any); tags["latest"] != "1.0.1" {
		t.Fatalf("latest = %v, want 1.0.1", tags["latest"])
	}
	// The pre-existing version's dist reference survived the append
	// byte-stably.
	if !strings.Contains(bodyOf(rr), `"shasum"`) {
		t.Fatalf("dist metadata missing after append: %s", bodyOf(rr))
	}
}

// TestRewriteExistingVersionNeedsDelete walks the rewrite family: every
// transition that touches EXISTING version data keeps the overwrite pair's
// delete demand — 403 for the write-only principal, success for the one
// holding delete. The append face of the same -rev PUT route stays on the
// write grant (last case).
func TestRewriteExistingVersionNeedsDelete(t *testing.T) {
	// servedDoc fetches the rendered packument as a mutable map.
	servedDoc := func(t *testing.T, s *stack, name string) map[string]any {
		t.Helper()
		rr := s.call(http.MethodGet, "/npm-local/"+name, "", adminPrincipal, nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET packument = %d; body=%s", rr.Code, bodyOf(rr))
		}
		var m map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
			t.Fatalf("packument not JSON: %v", err)
		}
		return m
	}
	revPut := func(s *stack, name string, doc map[string]any, p *Principal) *httptest.ResponseRecorder {
		return s.call(http.MethodPut, "/npm-local/"+name+"/-rev/1-abc", mustJSON(doc), p, nil)
	}

	t.Run("deprecate marker without delete is 403, with delete 201", func(t *testing.T) {
		for _, tt := range []struct {
			principal string
			canDelete bool
			want      int
		}{{"ciwrite", false, http.StatusForbidden}, {"cidev", true, http.StatusCreated}} {
			s := newStack(t)
			seedPackage(t, s, "demo-pkg", "1.0.0")
			p := seedGrant(t, s, tt.principal, tt.canDelete)
			dep := map[string]any{
				"name": "demo-pkg", "_id": "demo-pkg",
				"versions": map[string]any{
					"1.0.0": map[string]any{"name": "demo-pkg", "version": "1.0.0", "deprecated": "gone"},
				},
			}
			rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(dep), p, nil)
			if rr.Code != tt.want {
				t.Fatalf("%s deprecate = %d, want %d; body=%s", tt.principal, rr.Code, tt.want, bodyOf(rr))
			}
		}
	})

	t.Run("existing dist digest swap via -rev PUT", func(t *testing.T) {
		for _, tt := range []struct {
			principal string
			canDelete bool
			want      int
		}{{"ciwrite", false, http.StatusForbidden}, {"cidev", true, http.StatusOK}} {
			s := newStack(t)
			seedPackage(t, s, "demo-pkg", "1.0.0")
			p := seedGrant(t, s, tt.principal, tt.canDelete)
			doc := servedDoc(t, s, "demo-pkg")
			dist := doc["versions"].(map[string]any)["1.0.0"].(map[string]any)["dist"].(map[string]any)
			dist["shasum"] = strings.Repeat("cd", 20)
			rr := revPut(s, "demo-pkg", doc, p)
			if rr.Code != tt.want {
				t.Fatalf("%s digest swap = %d, want %d; body=%s", tt.principal, rr.Code, tt.want, bodyOf(rr))
			}
		}
	})

	t.Run("mixed new version plus old version rewrite is still 403", func(t *testing.T) {
		for _, tt := range []struct {
			principal string
			canDelete bool
			want      int
		}{{"ciwrite", false, http.StatusForbidden}, {"cidev", true, http.StatusOK}} {
			s := newStack(t)
			seedPackage(t, s, "demo-pkg", "1.0.0")
			p := seedGrant(t, s, tt.principal, tt.canDelete)
			doc := servedDoc(t, s, "demo-pkg")
			m := doc["versions"].(map[string]any)["1.0.0"].(map[string]any)
			m["description"] = "rewritten"
			doc["versions"].(map[string]any)["1.0.2"] = map[string]any{
				"name": "demo-pkg", "version": "1.0.2",
			}
			rr := revPut(s, "demo-pkg", doc, p)
			if rr.Code != tt.want {
				t.Fatalf("%s mixed rewrite = %d, want %d; body=%s", tt.principal, rr.Code, tt.want, bodyOf(rr))
			}
		}
	})

	t.Run("version removal via -rev PUT", func(t *testing.T) {
		for _, tt := range []struct {
			principal string
			canDelete bool
			want      int
		}{{"ciwrite", false, http.StatusForbidden}, {"cidev", true, http.StatusOK}} {
			s := newStack(t)
			seedPackage(t, s, "demo-pkg", "1.0.0", "1.0.1")
			p := seedGrant(t, s, tt.principal, tt.canDelete)
			doc := servedDoc(t, s, "demo-pkg")
			delete(doc["versions"].(map[string]any), "1.0.1")
			doc["dist-tags"].(map[string]any)["latest"] = "1.0.0"
			rr := revPut(s, "demo-pkg", doc, p)
			if rr.Code != tt.want {
				t.Fatalf("%s version removal = %d, want %d; body=%s", tt.principal, rr.Code, tt.want, bodyOf(rr))
			}
		}
	})

	t.Run("tarball republish stays the unconditional step-4 403", func(t *testing.T) {
		// Re-publishing an existing version with CHANGED tarball bytes dies
		// at the tarball-immutability arm (spec 2.3 step 4) for principals
		// WITH delete too — the rewrite protection of version data does not
		// live at the packument layer.
		for _, tt := range []struct {
			name      string
			admin     bool
			canDelete bool
		}{{"admin", true, false}, {"ciwrite", false, false}, {"cidev", false, true}} {
			s := newStack(t)
			seedPackage(t, s, "demo-pkg", "1.0.0")
			var p *Principal
			if tt.admin {
				p = adminPrincipal
			} else {
				p = seedGrant(t, s, tt.name, tt.canDelete)
			}
			doc := publishDoc("demo-pkg", "1.0.0", "TAMPERED-TARBALL", nil, nil)
			rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(doc), p, nil)
			if rr.Code != http.StatusForbidden {
				t.Fatalf("republish as %s = %d, want 403; body=%s", tt.name, rr.Code, bodyOf(rr))
			}
			if !strings.Contains(bodyOf(rr), "Cannot modify pre-existing version '1.0.0'") {
				t.Fatalf("body %q missing the step-4 wording", bodyOf(rr))
			}
		}
	})

	t.Run("rev PUT appending a version rides the write grant", func(t *testing.T) {
		s := newStack(t)
		seedPackage(t, s, "demo-pkg", "1.0.0")
		ci := seedGrant(t, s, "ciwrite", false)
		doc := servedDoc(t, s, "demo-pkg")
		doc["versions"].(map[string]any)["1.0.2"] = map[string]any{
			"name": "demo-pkg", "version": "1.0.2",
			"dist": map[string]any{"tarball": "http://registry.test/binflow/api/npm/npm-local/demo-pkg/-/demo-pkg-1.0.2.tgz"},
		}
		rr := revPut(s, "demo-pkg", doc, ci)
		if rr.Code != http.StatusOK {
			t.Fatalf("append via -rev PUT as write-only CI = %d, want 200; body=%s", rr.Code, bodyOf(rr))
		}
		if !strings.Contains(bodyOf(rr), "updated package") {
			t.Fatalf("body = %s", bodyOf(rr))
		}
		got := s.call(http.MethodGet, "/npm-local/demo-pkg", "", ci, nil)
		if !strings.Contains(bodyOf(got), `"1.0.2"`) {
			t.Fatalf("appended version missing: %s", bodyOf(got))
		}
	})
}

// TestDistTagMoveRidesWriteGrant: a dist-tag move rewrites no version
// manifest, so the write grant carries it (spec section 2.1's error column
// for the route is "403 无 write" — no delete demand). Same classifier, same
// fix family as the publish append.
func TestDistTagMoveRidesWriteGrant(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "demo-pkg", "1.0.0", "1.0.1")
	ci := seedGrant(t, s, "ciwrite", false)

	rr := s.call(http.MethodPut, "/npm-local/-/package/demo-pkg/dist-tags/beta", `"1.0.0"`, ci, nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("tag add as write-only CI = %d, want 201; body=%s", rr.Code, bodyOf(rr))
	}
	rr = s.call(http.MethodDelete, "/npm-local/-/package/demo-pkg/dist-tags/beta", "", ci, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("tag rm as write-only CI = %d, want 200; body=%s", rr.Code, bodyOf(rr))
	}
}
