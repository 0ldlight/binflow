// T-626 / BIN-110 (ledger maven/sidecar-head-cd-filename, ruled BUG by
// T-625 family 2 on live A 7.161.26 dual-round legs sc-head-{sha1,md5,
// sha256}): the sidecar face's HEAD arm carries the disposition pair the
// GET face keeps bare — Content-Disposition in the source-basename
// dual-parameter form and X-Artifactory-Filename echoing the source base
// name — the same verb-conditional sub-arm the generic plane's
// writeChecksumEcho renders (T-608 / BIN-90). These tests pin the three
// sidecar algorithms on HEAD and the GET arm's continued bareness.
package maven

import (
	"net/http"
	"strconv"
	"testing"
)

// TestSidecarHeadDispositionPair walks HEAD .sha1/.md5/.sha256 of one
// seeded artifact (registered sha1, registered md5, computed sha256) and
// asserts the disposition pair addresses the SOURCE basename on every
// arm, while the GET half of each pair stays bare.
func TestSidecarHeadDispositionPair(t *testing.T) {
	hs := newHarness(t)
	jar := "com/diff/t626/1.0.0/t626-1.0.0.jar"
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar, jarBytes, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	s1, m5, s256 := digests(jarBytes)
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar+".sha1", []byte(s1), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("sha1 registration = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar+".md5", []byte(m5), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("md5 registration = %d (%s)", resp.StatusCode, drain(t, resp))
	}

	// The pair's expected rendering for an ASCII source name (the live A
	// form, T-625 §二): dual-parameter Content-Disposition plus the
	// URL-encoded base name echo — the SOURCE's, never the sidecar's.
	const base = "t626-1.0.0.jar"
	wantCD := `attachment; filename="` + base + `"; filename*=UTF-8''` + base

	cases := []struct {
		algo string
		body string // the sidecar body each verb renders
	}{
		{"sha1", s1},
		{"md5", m5},
		{"sha256", s256},
	}
	for _, tt := range cases {
		h := hs.serve(http.MethodHead, "/maven-local/"+jar+"."+tt.algo, nil, nil, true)
		if h.StatusCode != http.StatusOK {
			t.Fatalf("HEAD .%s = %d (%s)", tt.algo, h.StatusCode, drain(t, h))
		}
		if got := h.Header.Get("Content-Disposition"); got != wantCD {
			t.Errorf("HEAD .%s Content-Disposition = %q, want the source-basename dual form %q", tt.algo, got, wantCD)
		}
		if got := h.Header.Get("X-Artifactory-Filename"); got != base {
			t.Errorf("HEAD .%s X-Artifactory-Filename = %q, want the source base name %q (never the .%s spelling)", tt.algo, got, base, tt.algo)
		}
		if got := h.Header.Get("Content-Length"); got != strconv.Itoa(len(tt.body)) {
			t.Errorf("HEAD .%s Content-Length = %q, want the sidecar body's %d", tt.algo, got, len(tt.body))
		}

		// GET arm unchanged (A GET face is aligned; BIN-110 touches the
		// HEAD arm only): the bare infra set carries no disposition pair.
		g := hs.serve(http.MethodGet, "/maven-local/"+jar+"."+tt.algo, nil, nil, true)
		assertBareSidecarGet(t, g, tt.body, len(tt.body))
		for _, hdrName := range []string{"Content-Disposition", "X-Artifactory-Filename"} {
			if v := g.Header.Get(hdrName); v != "" {
				t.Errorf("GET .%s carries %s: %q, want none (the GET face stays bare)", tt.algo, hdrName, v)
			}
		}
	}
}
