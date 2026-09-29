package maven

// T-559 / BIN-41: the derived sidecar's Last-Modified is a materialized
// STABLE stamp (contract maven/derived-sidecar-lm-materialized-stamp,
// confidence medium — A-side cross-second 304 is mechanism-derived): the
// stamp rides the derivation INPUT's own timestamp, so the same sidecar
// answers the same Last-Modified across requests and a cross-second
// revisit holding the previously observed stamp earns a 304 — never the
// per-request clock's 200 + forwarded stamp (the L033 residual:
// java-agent strip face, LM advancing 00:44:09→00:44:10 between GETs).

import (
	"net/http"
	"testing"
	"time"
)

// TestDerivedSidecarStampStableMemberFace is the divergence's own face:
// a java-agent client's strip-derivation sidecar on a LOCAL member. Two
// GETs a cross-second apart must answer the identical Last-Modified
// (equal to the stored document node's stamp — the same value the body
// face serves), and the second GET with If-Modified-Since = the observed
// stamp must 304.
func TestDerivedSidecarStampStableMemberFace(t *testing.T) {
	const agentUA = "Java/1.8.0_391"
	f := newVirtualFixture(t, `{"repositories":["mv-a"]}`)
	f.deploySnapshotPom(t, "mv-a", "20240101.120000", 1)

	ua := map[string]string{"User-Agent": agentUA}
	const meta = "/mv-a/com/acme/lib/1.0-SNAPSHOT/maven-metadata.xml"

	// The derivation inputs' stamps, pinned through the BODY face: the
	// stripped body's Last-Modified IS the stored node's timestamp (the
	// strip passes nodeTime(node) through), and the sidecar must carry
	// the same stamp — the input's materialization moment, not now.
	body := f.hs.serve(http.MethodGet, meta, nil, ua, true)
	if body.StatusCode != http.StatusOK {
		t.Fatalf("body GET = %d (%s)", body.StatusCode, drain(t, body))
	}
	drain(t, body)
	bodyLM := body.Header.Get("Last-Modified")

	side1 := f.hs.serve(http.MethodGet, meta+".sha1", nil, ua, true)
	if side1.StatusCode != http.StatusOK {
		t.Fatalf("sidecar GET 1 = %d (%s)", side1.StatusCode, drain(t, side1))
	}
	drain(t, side1)
	lm1 := side1.Header.Get("Last-Modified")
	if lm1 == "" {
		t.Fatalf("sidecar Last-Modified absent")
	}
	if lm1 != bodyLM {
		t.Errorf("sidecar Last-Modified = %q, want the input stamp %q (the body face's)", lm1, bodyLM)
	}

	// The cross-second revisit: past the second boundary, the same
	// sidecar keeps its stamp and a conditional holding the old value
	// answers 304. (The pre-fix per-request clock answered 200 with the
	// stamp forwarded a second — this is exactly that leg.)
	time.Sleep(1100 * time.Millisecond)

	if got := f.hs.serve(http.MethodGet, meta+".sha1", nil, ua, true); got.Header.Get("Last-Modified") != lm1 {
		t.Errorf("sidecar Last-Modified after cross-second gap = %q, want the stable %q (not per-request)",
			got.Header.Get("Last-Modified"), lm1)
	}
	cond := f.hs.serve(http.MethodGet, meta+".sha1", nil,
		withHeader(ua, "If-Modified-Since", lm1), true)
	if cond.StatusCode != http.StatusNotModified {
		t.Errorf("cross-second If-Modified-Since = %d, want 304 (materialized stable stamp)", cond.StatusCode)
	}
}

// TestDerivedSidecarStampStableVirtualFace pins the same posture on the
// virtual merge face (unobserved on A; BinFlow keeps it the same
// posture as the member face per the validator-family contract's note):
// the merged sidecar's stamp equals the merged BODY face's stamp — the
// newest contributor's node timestamp — with no clock in the loop.
func TestDerivedSidecarStampStableVirtualFace(t *testing.T) {
	f := newVirtualFixture(t, `{"repositories":["mv-a","mv-b"]}`)
	f.deploySnapshotPom(t, "mv-a", "20240101.120000", 1)
	f.deploySnapshotPom(t, "mv-b", "20240102.130000", 2)

	const meta = "/mv-virt/com/acme/lib/1.0-SNAPSHOT/maven-metadata.xml"
	body := f.hs.serve(http.MethodGet, meta, nil, nil, true)
	if body.StatusCode != http.StatusOK {
		t.Fatalf("body GET = %d (%s)", body.StatusCode, drain(t, body))
	}
	drain(t, body)
	bodyLM := body.Header.Get("Last-Modified")

	for _, algo := range []string{"sha1", "md5", "sha256"} {
		side := f.hs.serve(http.MethodGet, meta+"."+algo, nil, nil, true)
		if side.StatusCode != http.StatusOK {
			t.Fatalf("%s sidecar = %d (%s)", algo, side.StatusCode, drain(t, side))
		}
		drain(t, side)
		if got := side.Header.Get("Last-Modified"); got != bodyLM {
			t.Errorf("%s sidecar Last-Modified = %q, want the body face's input stamp %q", algo, got, bodyLM)
		}
	}
}
