package httpapi_test

// T-290 (FR-90.2-AC4 / L25) + T-317 (FR-101.1) inversion: the smart remote
// effective-field subset across the REST plane — PUT /api/repositories/{key}
// carries the PRD/LC-12 and artifactory.xsd spellings through to
// repo.Service, GET echoes the canonical form, and the M11 pair
// (enableTokenAuthentication / contentSynchronisation) is ACCEPTED since
// T-317 while the M3 scenario-D tolerance for other unknown fields holds.
//
// T-346 (FR-113.1 / L28, the T-290-2 carryover): the canonical echo spelling
// is socketTimeoutMillis; the PRD spelling socketTimeoutMs stays accepted as
// an input-only alias and never appears in the echo.

import (
	"net/http"
	"strings"
	"testing"
)

// TestT290SmartRemoteWireRoundTrip: the L25 probe at the REST plane — PUT
// with the three P1 spellings plus the P2 cleanup field (the LEGACY ms
// spelling on input, pinning the alias), GET echoing the canonical values.
func TestT290SmartRemoteWireRoundTrip(t *testing.T) {
	h := newHarness(t)
	status, body := putRepoStatus(t, h, "smart-remote", `{
		"rclass":"remote","packageType":"generic",
		"url":"http://127.0.0.1:9099/up",
		"socketTimeoutMs":2500,
		"metadataRetrievalTimeoutSecs":30,
		"missRetrievalCachePeriodSecs":45,
		"unusedArtifactsCleanupPeriodHours":72
	}`)
	if status != http.StatusOK {
		t.Fatalf("create status = %d; body=%s", status, body)
	}

	code, cfg := getRepoJSON(t, h, "smart-remote")
	if code != http.StatusOK {
		t.Fatalf("GET status = %d", code)
	}
	conf, ok := cfg["configuration"].(map[string]any)
	if !ok {
		t.Fatalf("configuration missing: %v", cfg)
	}
	for k, v := range map[string]any{
		"socketTimeoutMillis":               float64(2500), // the legacy input spelling, canonicalized
		"socketTimeoutSecs":                 float64(3),    // ceil of 2500ms
		"metadataRetrievalTimeoutSecs":      float64(30),
		"missedRetrievalCachePeriodSecs":    float64(45), // canonical spelling carries the alias value
		"unusedArtifactsCleanupPeriodHours": float64(72),
	} {
		if conf[k] != v {
			t.Fatalf("configuration[%s] = %v, want %v", k, conf[k], v)
		}
	}
	// FR-113.1: the input-only alias never rides the echo.
	if _, ok := conf["socketTimeoutMs"]; ok {
		t.Fatalf("echo carries the input-only alias socketTimeoutMs: %v", conf)
	}
}

// TestT290SmartRemoteDefaultsOnWire: a bare remote create materializes the
// product defaults in the echo (socketTimeoutMillis 15000 — the canonical
// xsd spelling since FR-113.1 — / metadataRetrievalTimeoutSecs 60 / cleanup
// 0 = off).
func TestT290SmartRemoteDefaultsOnWire(t *testing.T) {
	h := newHarness(t)
	status, body := putRepoStatus(t, h, "plain-remote",
		`{"rclass":"remote","packageType":"generic","url":"http://127.0.0.1:9099"}`)
	if status != http.StatusOK {
		t.Fatalf("create status = %d; body=%s", status, body)
	}
	_, cfg := getRepoJSON(t, h, "plain-remote")
	conf := cfg["configuration"].(map[string]any)
	for k, v := range map[string]any{
		"socketTimeoutMillis":               float64(15000),
		"socketTimeoutSecs":                 float64(15),
		"metadataRetrievalTimeoutSecs":      float64(60),
		"unusedArtifactsCleanupPeriodHours": float64(0),
	} {
		if conf[k] != v {
			t.Fatalf("configuration[%s] = %v, want %v", k, conf[k], v)
		}
	}
}

// TestT290M11FieldAcceptedOnWire (inverted by T-317 / FR-101.1 — the M10
// L25 by-name 400 is retired): PUT carrying enableTokenAuthentication and
// contentSynchronisation answers 200 and GET echoes the canonical form. A
// mistyped contentSynchronisation still answers 400 naming the field; the
// xsd ms spelling and the scenario-D tolerance arm keep working.
func TestT290M11FieldAcceptedOnWire(t *testing.T) {
	h := newHarness(t)
	status, body := putRepoStatus(t, h, "m11-remote", `{
		"rclass":"remote","packageType":"generic",
		"url":"http://127.0.0.1:9099",
		"enableTokenAuthentication":true,
		"contentSynchronisation":{"enabled":true,"propertiesEnabled":true}
	}`)
	if status != http.StatusOK {
		t.Fatalf("create status = %d, want 200; body=%s", status, body)
	}
	_, cfg := getRepoJSON(t, h, "m11-remote")
	conf, ok := cfg["configuration"].(map[string]any)
	if !ok {
		t.Fatalf("configuration missing: %v", cfg)
	}
	if conf["enableTokenAuthentication"] != true {
		t.Fatalf("enableTokenAuthentication = %v, want true", conf["enableTokenAuthentication"])
	}
	cs, ok := conf["contentSynchronisation"].(map[string]any)
	if !ok {
		t.Fatalf("contentSynchronisation = %v (%T), want an object", conf["contentSynchronisation"], conf["contentSynchronisation"])
	}
	for k, v := range map[string]any{
		"enabled": true, "statisticsEnabled": false, "propertiesEnabled": true, "sourceOrigin": false,
	} {
		if cs[k] != v {
			t.Fatalf("contentSynchronisation[%s] = %v, want %v", k, cs[k], v)
		}
	}
	t.Run("mistyped contentSynchronisation refused by name", func(t *testing.T) {
		h := newHarness(t)
		status, body := putRepoStatus(t, h, "m11-bad", `{
			"rclass":"remote","packageType":"generic",
			"url":"http://127.0.0.1:9099","contentSynchronisation":true}`)
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", status, body)
		}
		if !strings.Contains(body, "contentSynchronisation") {
			t.Fatalf("body %q does not name the field", body)
		}
	})
	t.Run("canonical xsd ms spelling round-trips", func(t *testing.T) {
		h := newHarness(t)
		status, body := putRepoStatus(t, h, "xsd-remote",
			`{"rclass":"remote","packageType":"generic","url":"http://127.0.0.1:9099","socketTimeoutMillis":800}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", status, body)
		}
		_, cfg := getRepoJSON(t, h, "xsd-remote")
		conf := cfg["configuration"].(map[string]any)
		if conf["socketTimeoutMillis"] != float64(800) {
			t.Fatalf("socketTimeoutMillis = %v, want 800", conf["socketTimeoutMillis"])
		}
		if _, ok := conf["socketTimeoutMs"]; ok {
			t.Fatalf("echo carries the input-only alias socketTimeoutMs: %v", conf)
		}
	})
	t.Run("scenario D tolerance holds", func(t *testing.T) {
		h := newHarness(t)
		status, body := putRepoStatus(t, h, "scen-d-remote",
			`{"rclass":"remote","packageType":"generic","url":"http://127.0.0.1:9099","proxyRef":"","shareConfiguration":false}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", status, body)
		}
	})
}
