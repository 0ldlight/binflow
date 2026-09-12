package httpapi_test

// T-329 D-E (the transport half): the helm Enforce Layout switch pair rides
// the repositories REST plane. T-327R carried the deb/rpm policy keys but
// missed forceMetadataNameVersion / forceNonDuplicateChart, so a PUT
// carrying them answered 200 and silently dropped them (the scenario-D
// tolerance swallowing two keys the helm adapter's probe reads) — enforce
// could never be switched on over REST, and an explicit false could never
// turn it back off. These tests pin the transport contract on a generic
// local repository (the local arm is package-type-agnostic by design); the
// licensed helm leg proving the switches DRIVE the 403 upload hook lives in
// the adapter package.

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// TestHelmEnforceKeysRoundTripREST: create carrying both switches, then GET
// — both must echo back under "configuration" exactly as sent (D-E's
// failing assertion: PUT 200, GET keyless). The explicit false arm is the
// flip-off requirement: an operator turning enforce back off needs the
// pointer to survive transport, not collapse to absent.
func TestHelmEnforceKeysRoundTripREST(t *testing.T) {
	h := newHarness(t)
	status, body := putRepoStatus(t, h, "enforce-local",
		`{"rclass":"local","packageType":"generic",`+
			`"forceMetadataNameVersion":true,"forceNonDuplicateChart":true}`)
	if status != http.StatusOK {
		t.Fatalf("create status = %d; body=%s", status, body)
	}

	_, cfg := getRepoJSON(t, h, "enforce-local")
	conf, ok := cfg["configuration"].(map[string]any)
	if !ok {
		t.Fatalf("configuration missing: %v", cfg)
	}
	for k, v := range map[string]any{
		"forceMetadataNameVersion": true,
		"forceNonDuplicateChart":   true,
	} {
		if !reflect.DeepEqual(conf[k], v) {
			t.Errorf("configuration[%s] = %v, want %v", k, conf[k], v)
		}
	}

	// The flip-off arm: a second repository carrying explicit false must
	// echo false, not drop the keys (both product defaults are false — the
	// disable update only works because the pointer survives transport).
	status, body = putRepoStatus(t, h, "enforce-off",
		`{"rclass":"local","packageType":"generic",`+
			`"forceMetadataNameVersion":false,"forceNonDuplicateChart":false}`)
	if status != http.StatusOK {
		t.Fatalf("create (off arm) status = %d; body=%s", status, body)
	}
	_, cfg = getRepoJSON(t, h, "enforce-off")
	conf = cfg["configuration"].(map[string]any)
	for k, v := range map[string]any{
		"forceMetadataNameVersion": false,
		"forceNonDuplicateChart":   false,
	} {
		if !reflect.DeepEqual(conf[k], v) {
			t.Errorf("off-arm configuration[%s] = %v, want %v", k, conf[k], v)
		}
	}
}

// TestHelmEnforceKeysUpdateReplacesConfig: the switches obey the same
// Artifactory full-replace PUT semantics as every other local key — a
// partial update drops the keys it does not carry, and an update without
// any type-relevant field keeps the stored configuration.
func TestHelmEnforceKeysUpdateReplacesConfig(t *testing.T) {
	h := newHarness(t)
	if status, body := putRepoStatus(t, h, "enforce-up",
		`{"rclass":"local","packageType":"generic",`+
			`"forceMetadataNameVersion":true,"forceNonDuplicateChart":true}`); status != http.StatusOK {
		t.Fatalf("create status = %d; body=%s", status, body)
	}
	// Partial update: one switch flips, the other (absent) is replaced away.
	if status, body := postRepoStatus(t, h, "enforce-up", `{"forceNonDuplicateChart":false}`); status != http.StatusOK {
		t.Fatalf("update status = %d; body=%s", status, body)
	}
	_, cfg := getRepoJSON(t, h, "enforce-up")
	conf := cfg["configuration"].(map[string]any)
	if conf["forceNonDuplicateChart"] != false {
		t.Errorf("forceNonDuplicateChart after update = %v, want false", conf["forceNonDuplicateChart"])
	}
	if _, still := conf["forceMetadataNameVersion"]; still {
		t.Errorf("full-replace dropped nothing — forceMetadataNameVersion = %v survived", conf["forceMetadataNameVersion"])
	}
	// Description-only update: no type-relevant field, configuration kept.
	if status, body := postRepoStatus(t, h, "enforce-up", `{"description":"words only"}`); status != http.StatusOK {
		t.Fatalf("description-only status = %d; body=%s", status, body)
	}
	_, cfg = getRepoJSON(t, h, "enforce-up")
	conf = cfg["configuration"].(map[string]any)
	if conf["forceNonDuplicateChart"] != false {
		t.Errorf("forceNonDuplicateChart after description-only update = %v, want the kept false", conf["forceNonDuplicateChart"])
	}
}

// TestHelmEnforceKeysTypeRefusal: the switches are part of the typed
// transport now, so a mistyped value fails the body decode with the field
// named (400) instead of the pre-D-E silent drop.
func TestHelmEnforceKeysTypeRefusal(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"forceMetadataNameVersion not a bool", `{"rclass":"local","packageType":"generic","forceMetadataNameVersion":"yes"}`, "forceMetadataNameVersion"},
		{"forceNonDuplicateChart not a bool", `{"rclass":"local","packageType":"generic","forceNonDuplicateChart":1}`, "forceNonDuplicateChart"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			status, body := putRepoStatus(t, h, "enforce-bad", tt.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d; body=%s", status, body)
			}
			if !strings.Contains(body, tt.want) {
				t.Fatalf("body %q does not name %q", body, tt.want)
			}
		})
	}
}
