package httpapi_test

// T-355A (FR-110.2, the D-5 carryover): the conan forced-authentication
// switch rides the repositories REST plane. T-351's D-5 evidence was the
// pre-fix shape: a PUT carrying forceConanAuthentication answered 200 and
// silently dropped the key (the typed transport knew nothing of it), so the
// conan plane could never be forced over REST — the T-329 D-E hole one
// package later. These tests pin the transport contract (round trip, the
// explicit-false flip-off arm, the typed refusal); the enforcement leg that
// the switch DRIVES (the per-endpoint 401 sweep) lives in the conan
// adapter package's forceauth_test.go.

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// TestConanForceAuthRoundTripREST: create carrying the switch, then GET —
// it must echo back under "configuration" exactly as sent; the explicit
// false arm is the flip-off requirement (the product default IS false, so
// the disable update only works because the pointer survives transport).
func TestConanForceAuthRoundTripREST(t *testing.T) {
	h := newHarness(t)
	status, body := putRepoStatus(t, h, "conan-forced",
		`{"rclass":"local","packageType":"generic","forceConanAuthentication":true}`)
	if status != http.StatusOK {
		t.Fatalf("create status = %d; body=%s", status, body)
	}
	_, cfg := getRepoJSON(t, h, "conan-forced")
	conf, ok := cfg["configuration"].(map[string]any)
	if !ok {
		t.Fatalf("configuration missing: %v", cfg)
	}
	if !reflect.DeepEqual(conf["forceConanAuthentication"], true) {
		t.Errorf("configuration[forceConanAuthentication] = %v, want true", conf["forceConanAuthentication"])
	}

	// The flip-off arm on a second repository: an explicit false echoes
	// false, not absent.
	status, body = putRepoStatus(t, h, "conan-open",
		`{"rclass":"local","packageType":"generic","forceConanAuthentication":false}`)
	if status != http.StatusOK {
		t.Fatalf("create (off arm) status = %d; body=%s", status, body)
	}
	_, cfg = getRepoJSON(t, h, "conan-open")
	conf = cfg["configuration"].(map[string]any)
	if !reflect.DeepEqual(conf["forceConanAuthentication"], false) {
		t.Errorf("off-arm configuration[forceConanAuthentication] = %v, want false", conf["forceConanAuthentication"])
	}
}

// TestConanForceAuthTypeRefusal: the switch is part of the typed transport,
// so a mistyped value fails the body decode with the field named (400)
// instead of the silent drop; repo.validateLocalConfig carries the same
// gate one layer down for hand-shaped blobs (its own test).
func TestConanForceAuthTypeRefusal(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"string for a bool", `{"rclass":"local","packageType":"generic","forceConanAuthentication":"yes"}`, "forceConanAuthentication"},
		{"number for a bool", `{"rclass":"local","packageType":"generic","forceConanAuthentication":1}`, "forceConanAuthentication"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			status, body := putRepoStatus(t, h, "conan-bad", tt.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d; body=%s", status, body)
			}
			if !strings.Contains(body, tt.want) {
				t.Fatalf("body %q does not name %q", body, tt.want)
			}
		})
	}
}
