package httpapi_test

// T-290 (FR-90.2-AC4 / L25): the smart remote effective-field subset across
// the REST plane — PUT /api/repositories/{key} carries the PRD/LC-12 and
// artifactory.xsd spellings through to repo.Service, GET echoes the
// canonical form, and the M11-ruled field names answer 400 by name while
// the M3 scenario-D tolerance for other unknown fields holds.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestT290SmartRemoteWireRoundTrip: the L25 probe at the REST plane — PUT
// with the three P1 spellings plus the P2 cleanup field, GET echoing the
// canonical values.
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
		"socketTimeoutMs":                   float64(2500),
		"socketTimeoutSecs":                 float64(3), // ceil of 2500ms
		"metadataRetrievalTimeoutSecs":      float64(30),
		"missedRetrievalCachePeriodSecs":    float64(45), // canonical spelling carries the alias value
		"unusedArtifactsCleanupPeriodHours": float64(72),
	} {
		if conf[k] != v {
			t.Fatalf("configuration[%s] = %v, want %v", k, conf[k], v)
		}
	}
}

// TestT290SmartRemoteDefaultsOnWire: a bare remote create materializes the
// product defaults in the echo (socketTimeoutMs 15000 /
// metadataRetrievalTimeoutSecs 60 / cleanup 0 = off).
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
		"socketTimeoutMs":                   float64(15000),
		"socketTimeoutSecs":                 float64(15),
		"metadataRetrievalTimeoutSecs":      float64(60),
		"unusedArtifactsCleanupPeriodHours": float64(0),
	} {
		if conf[k] != v {
			t.Fatalf("configuration[%s] = %v, want %v", k, conf[k], v)
		}
	}
}

// TestT290M11FieldRefusedOnWire: enableTokenAuthentication /
// contentSynchronisation answer 400 naming the field (the no-inert-fields
// rule); the xsd ms spelling and the scenario-D tolerance arm both keep
// working.
func TestT290M11FieldRefusedOnWire(t *testing.T) {
	for _, field := range []string{"contentSynchronisation", "enableTokenAuthentication"} {
		t.Run(field, func(t *testing.T) {
			h := newHarness(t)
			status, body := putRepoStatus(t, h, "m11-remote", fmt.Sprintf(
				`{"rclass":"remote","packageType":"generic","url":"http://127.0.0.1:9099","%s":true}`, field))
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", status, body)
			}
			if !strings.Contains(body, field) {
				t.Fatalf("body %q does not name the refused field", body)
			}
		})
	}
	t.Run("xsd ms spelling accepted", func(t *testing.T) {
		h := newHarness(t)
		status, body := putRepoStatus(t, h, "xsd-remote",
			`{"rclass":"remote","packageType":"generic","url":"http://127.0.0.1:9099","socketTimeoutMillis":800}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", status, body)
		}
		_, cfg := getRepoJSON(t, h, "xsd-remote")
		conf := cfg["configuration"].(map[string]any)
		if conf["socketTimeoutMs"] != float64(800) {
			t.Fatalf("socketTimeoutMs = %v, want 800", conf["socketTimeoutMs"])
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
