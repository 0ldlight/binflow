package npm

import (
	"net/http"
	"strings"
	"testing"
)

// L012-1 D2: "latest" is immortal on the dist-tags read face — deleting it
// can never leave the package tagless, because GET recomputes an ABSENT
// latest at read time from the greatest stored version (evidence section
// 3-D2: set latest=1.0.0, DELETE, GET answers 1.1.0). A present latest is
// never overwritten (a client's rollback pointer is legitimate).

// TestDistTagReadFaceRecomputeLatest pins the read-time recompute semantics
// of the collection GET.
func TestDistTagReadFaceRecomputeLatest(t *testing.T) {
	s := newStack(t)

	cases := []struct {
		name  string
		pkg   string
		setup func(t *testing.T, tagsURL string)
	}{
		{
			name: "latest alone deleted",
			pkg:  "recompute-one-pkg",
			setup: func(t *testing.T, tagsURL string) {
				if rr := s.call(http.MethodDelete, tagsURL+"/latest", "", adminPrincipal, nil); rr.Code != http.StatusOK {
					t.Fatalf("delete latest: status %d; body=%s", rr.Code, bodyOf(rr))
				}
			},
		},
		{
			name: "every tag deleted (npm ls must not say No dist-tags found)",
			pkg:  "recompute-all-pkg",
			setup: func(t *testing.T, tagsURL string) {
				if rr := s.call(http.MethodPut, tagsURL+"/beta", `"1.0.0"`, adminPrincipal, nil); rr.Code != http.StatusCreated {
					t.Fatalf("add beta: status %d; body=%s", rr.Code, bodyOf(rr))
				}
				for _, tag := range []string{"beta", "latest"} {
					if rr := s.call(http.MethodDelete, tagsURL+"/"+tag, "", adminPrincipal, nil); rr.Code != http.StatusOK {
						t.Fatalf("delete %s: status %d; body=%s", tag, rr.Code, bodyOf(rr))
					}
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seedPackage(t, s, tc.pkg, "1.0.0", "1.1.0")
			tagsURL := "/npm-local/-/package/" + tc.pkg + "/dist-tags"
			tc.setup(t, tagsURL)
			rr := s.call(http.MethodGet, tagsURL, "", adminPrincipal, nil)
			if rr.Code != http.StatusOK {
				t.Fatalf("ls status = %d; body=%s", rr.Code, bodyOf(rr))
			}
			if !strings.Contains(bodyOf(rr), `"latest":"1.1.0"`) {
				t.Fatalf("ls body = %s, want the read-time recompute crowning 1.1.0", bodyOf(rr))
			}
		})
	}
}

// TestDistTagReadFaceKeepsExplicitLatest: a latest the client pointed at an
// OLDER version survives — the recompute only fires when the tag is absent.
func TestDistTagReadFaceKeepsExplicitLatest(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "demo-pkg", "1.0.0", "1.1.0")

	rr := s.call(http.MethodPut, "/npm-local/-/package/demo-pkg/dist-tags/latest", `"1.0.0"`, adminPrincipal, nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("repin latest: status %d; body=%s", rr.Code, bodyOf(rr))
	}
	rr = s.call(http.MethodGet, "/npm-local/-/package/demo-pkg/dist-tags", "", adminPrincipal, nil)
	if !strings.Contains(bodyOf(rr), `"latest":"1.0.0"`) {
		t.Fatalf("ls body = %s, want the explicit older latest untouched", bodyOf(rr))
	}
	// The packument face honors the same never-overwrite rule (crownLatest
	// only fires on an ABSENT latest).
	rr = s.call(http.MethodGet, "/npm-local/demo-pkg", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK || !strings.Contains(bodyOf(rr), `"latest":"1.0.0"`) {
		t.Fatalf("packument = %d %s, want the explicit older latest untouched", rr.Code, bodyOf(rr))
	}
}

// TestDistTagRecomputeIsReadTimeOnly: after DELETE latest, BOTH read faces
// re-crown latest at read time (L013 R-15 n4: the reference answers
// latest=1.1.0 on the packument AND the dist-tags endpoint) while the STORED
// document keeps the deletion — the crown is a GET-face projection, proven
// by a second DELETE answering the tag-not-found 404. The packument face
// matters because "npm install <pkg>" resolves latest through it.
func TestDistTagRecomputeIsReadTimeOnly(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "demo-pkg", "1.0.0", "1.1.0")

	if rr := s.call(http.MethodDelete, "/npm-local/-/package/demo-pkg/dist-tags/latest", "", adminPrincipal, nil); rr.Code != http.StatusOK {
		t.Fatalf("delete latest: status %d; body=%s", rr.Code, bodyOf(rr))
	}
	rr := s.call(http.MethodGet, "/npm-local/demo-pkg", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("packument status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if !strings.Contains(bodyOf(rr), `"latest":"1.1.0"`) {
		t.Fatalf("packument dist-tags = %s, want the same read-time recompute as the dist-tags face crowning 1.1.0", bodyOf(rr))
	}
	rr = s.call(http.MethodGet, "/npm-local/-/package/demo-pkg/dist-tags", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("dist-tags status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if !strings.Contains(bodyOf(rr), `"latest":"1.1.0"`) {
		t.Fatalf("dist-tags = %s, want the read-time recompute crowning 1.1.0", bodyOf(rr))
	}
	// Read-time only: the stored document keeps the deletion, so the tag is
	// still deletable-not-found — a persisted crown would answer 200 here.
	if rr := s.call(http.MethodDelete, "/npm-local/-/package/demo-pkg/dist-tags/latest", "", adminPrincipal, nil); rr.Code != http.StatusNotFound {
		t.Fatalf("second delete latest: status %d; body=%s — the crown leaked into the store", rr.Code, bodyOf(rr))
	}
}
