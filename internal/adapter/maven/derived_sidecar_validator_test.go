package maven

// T-551 (BIN-33, L032 Arm 6): the DERIVED sidecar's validator family, as
// the live A face (7.161.26) ruled it and the B side violated — three
// dimensions that flip here, plus the regression red lines the same fix
// must not disturb:
//
//   - divergence 1: the derived .sha1/.md5/.sha256 carries its OWN
//     Last-Modified (the derivation moment);
//   - divergence 2: If-Modified-Since at or after that stamp is a 304,
//     an older value a 200;
//   - divergence 3: NO ETag on the sidecar — the body face answers
//     ETag/If-None-Match, so an INM aimed at the sidecar never
//     short-circuits (no false 304);
//   - red line: sidecar CONTENT stays the digest of the SERVED derived
//     body (stripped for a java-agent, whole for a capable client);
//   - red line: the BODY face keeps ETag + Last-Modified and answers
//     both conditional forms (INM and IMS-newer → 304, IMS-older → 200);
//   - red line: .sha512 stays the honest 404.
//
// writeDerivedSidecar serves three legs: the virtual merge under a
// java-agent UA (stripped), the virtual merge under a Maven 3 UA (whole)
// and the member document under a java-agent UA (stripped) — the member
// capable leg is serveSidecar's stored-node face, pinned content-wise by
// TestSnapshotVersionsUAStripping.

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestDerivedSidecarValidatorFamily walks the three derived-sidecar legs
// (table rows) and pins every dimension above on each.
func TestDerivedSidecarValidatorFamily(t *testing.T) {
	const (
		capableUA = "Apache-Maven/3.9.16 (Java 17.0.11; Mac OS X 15.0)"
		agentUA   = "Java/1.8.0_391"
	)
	f := newVirtualFixture(t, `{"repositories":["mv-a","mv-b"]}`)
	f.deploySnapshotPom(t, "mv-a", "20240101.120000", 1)
	f.deploySnapshotPom(t, "mv-b", "20240102.130000", 2)

	vmeta := "/mv-virt/com/acme/lib/1.0-SNAPSHOT/maven-metadata.xml"
	mmeta := "/mv-a/com/acme/lib/1.0-SNAPSHOT/maven-metadata.xml"
	legs := []struct {
		name string
		path string
		ua   string
	}{
		{"virtual java-agent stripped merge", vmeta, agentUA},
		{"virtual Maven 3 whole merge", vmeta, capableUA},
		{"member java-agent stripped document", mmeta, agentUA},
	}
	for _, tc := range legs {
		t.Run(tc.name, func(t *testing.T) {
			ua := map[string]string{"User-Agent": tc.ua}

			// The served body and its validators — the BODY-face red line
			// (unchanged by T-551).
			body := f.hs.serve(http.MethodGet, tc.path, nil, ua, true)
			if body.StatusCode != http.StatusOK {
				t.Fatalf("body GET = %d (%s)", body.StatusCode, drain(t, body))
			}
			etag := body.Header.Get("ETag")
			bodyLM := body.Header.Get("Last-Modified")
			bodyBytes := drain(t, body)
			if etag == "" || bodyLM == "" {
				t.Fatalf("body face lost its validators: ETag=%q Last-Modified=%q", etag, bodyLM)
			}
			if got := f.hs.serve(http.MethodGet, tc.path, nil,
				withHeader(ua, "If-None-Match", etag), true); got.StatusCode != http.StatusNotModified {
				t.Errorf("body If-None-Match = %d, want 304", got.StatusCode)
			}
			blm, err := time.Parse(http.TimeFormat, bodyLM)
			if err != nil {
				t.Fatalf("body Last-Modified unparseable: %v", err)
			}
			if got := f.hs.serve(http.MethodGet, tc.path, nil,
				withHeader(ua, "If-Modified-Since", blm.Add(90*time.Second).Format(http.TimeFormat)), true); got.StatusCode != http.StatusNotModified {
				t.Errorf("body If-Modified-Since (newer) = %d, want 304", got.StatusCode)
			}
			if got := f.hs.serve(http.MethodGet, tc.path, nil,
				withHeader(ua, "If-Modified-Since", blm.Add(-90*time.Second).Format(http.TimeFormat)), true); got.StatusCode != http.StatusOK {
				t.Errorf("body If-Modified-Since (older) = %d, want 200", got.StatusCode)
			}

			// Every sidecar algo carries the same validator family and the
			// derived-content contract.
			sums := digestsOfBody(bodyBytes)
			for algo, want := range map[string]string{"sha1": sums.sha1, "md5": sums.md5, "sha256": sums.sha256} {
				side := f.hs.serve(http.MethodGet, tc.path+"."+algo, nil, ua, true)
				if side.StatusCode != http.StatusOK {
					t.Fatalf("%s sidecar = %d (%s)", algo, side.StatusCode, drain(t, side))
				}
				if got := strings.TrimSpace(string(drain(t, side))); got != want {
					t.Errorf("%s sidecar content = %s, want the digest of the served body %s", algo, got, want)
				}
				lm := side.Header.Get("Last-Modified")
				stamp, err := time.Parse(http.TimeFormat, lm)
				if err != nil {
					t.Fatalf("%s sidecar Last-Modified = %q (%v), want present and parseable", algo, lm, err)
				}
				if side.Header.Get("ETag") != "" {
					t.Errorf("%s sidecar carries ETag %q, want none", algo, side.Header.Get("ETag"))
				}
				if got := f.hs.serve(http.MethodGet, tc.path+"."+algo, nil,
					withHeader(ua, "If-Modified-Since", stamp.Add(90*time.Second).Format(http.TimeFormat)), true); got.StatusCode != http.StatusNotModified {
					t.Errorf("%s sidecar If-Modified-Since (newer) = %d, want 304", algo, got.StatusCode)
				}
				if got := f.hs.serve(http.MethodGet, tc.path+"."+algo, nil,
					withHeader(ua, "If-Modified-Since", stamp.Add(-90*time.Second).Format(http.TimeFormat)), true); got.StatusCode != http.StatusOK {
					t.Errorf("%s sidecar If-Modified-Since (older) = %d, want 200", algo, got.StatusCode)
				}
				// No served ETag means no INM match — not even the digest
				// itself, not even "*" (the pre-fix bug answered 304 here).
				for _, inm := range []string{want, `"` + want + `"`, "*"} {
					if got := f.hs.serve(http.MethodGet, tc.path+"."+algo, nil,
						withHeader(ua, "If-None-Match", inm), true); got.StatusCode != http.StatusOK {
						t.Errorf("%s sidecar If-None-Match %q = %d, want 200 (no ETag to match)", algo, inm, got.StatusCode)
					}
				}
			}

			// .sha512 keeps its honest 404 on the derived face too.
			if got := f.hs.serve(http.MethodGet, tc.path+".sha512", nil, ua, true); got.StatusCode != http.StatusNotFound {
				t.Errorf(".sha512 = %d, want 404", got.StatusCode)
			}
		})
	}
}

// withHeader copies hdr and sets one more entry.
func withHeader(hdr map[string]string, k, v string) map[string]string {
	out := make(map[string]string, len(hdr)+1)
	for kk, vv := range hdr {
		out[kk] = vv
	}
	out[k] = v
	return out
}
