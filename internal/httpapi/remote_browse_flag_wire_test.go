package httpapi_test

// D-T456-1 (T-461's BE support leg): the listRemoteFolderItems optional档
// (T-448, FR-147.2) on the repositories REST wire. The field was missing
// from the httpapi transport struct since T-448 landed the service layer:
// a PUT carrying it answered 200 and silently dropped the key (the decode
// swallowed the unknown field), and the mistyped-value 400 was unreachable
// for the same reason. These tests pin the repaired contract: legal
// save-and-echo round trips on the batch-1 types, the explicit-false and
// absent arms (off is the product default — zero regression against the
// pre-T-448 posture), the update plane's flip/keep semantics, the type
// gate's diagnostic 400, and the by-name value gate repo.Service owns.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// browseFlagGate unlocks the batch-1 package types on the service's
// package-type seam — helm/debian/rpm are registry-known slots, the same
// posture internal/repo's t448Env rides (the static enum alone knows the
// five core types). The handler's create-arm registry check passes a
// nil-registry stack through untouched, so no addons.Registry is needed
// here: the seam is the one gate that matters.
type browseFlagGate struct{}

func (browseFlagGate) Verdict(_ context.Context, packageType string) repo.PackageTypeVerdict {
	switch packageType {
	case "helm", "debian", "rpm":
		return repo.PackageTypeVerdict{Known: true, Unlocked: true}
	}
	return repo.PackageTypeVerdict{}
}

// newBrowseFlagHarness assembles the standard stack with the batch-1 slots
// unlocked. The gate attaches to the SAME service instance the handlers
// hold (AttachPackageTypeGate mutates the concrete implementation), so the
// wiring is live for every request the test issues afterwards.
func newBrowseFlagHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	repo.AttachPackageTypeGate(h.svc, browseFlagGate{})
	return h
}

// TestRemoteBrowseFlagRoundTripREST: create carrying the optional档 on a
// batch-1 type saves it and GET echoes it under "configuration" exactly as
// sent (D-T456-1's failing assertion: PUT 200, GET keyless). The explicit
// false arm is deliberate — a POINTER keeps it distinct from absent, which
// is what lets an operator flip the switch back off.
func TestRemoteBrowseFlagRoundTripREST(t *testing.T) {
	tests := []struct {
		name        string
		packageType string
		flag        string
		want        any
	}{
		{"helm accepts true", "helm", `true`, true},
		{"debian accepts true", "debian", `true`, true},
		{"rpm accepts true", "rpm", `true`, true},
		{"helm accepts explicit false", "helm", `false`, false},
		{"generic accepts explicit false", "generic", `false`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newBrowseFlagHarness(t)
			key := "browse-" + tt.packageType
			status, body := putRepoStatus(t, h, key,
				`{"rclass":"remote","packageType":"`+tt.packageType+`",`+
					`"url":"http://127.0.0.1:9099","listRemoteFolderItems":`+tt.flag+`}`)
			if status != http.StatusOK {
				t.Fatalf("create status = %d; body=%s", status, body)
			}
			code, cfg := getRepoJSON(t, h, key)
			if code != http.StatusOK {
				t.Fatalf("GET status = %d", code)
			}
			conf, ok := cfg["configuration"].(map[string]any)
			if !ok {
				t.Fatalf("configuration missing: %v", cfg)
			}
			if conf["listRemoteFolderItems"] != tt.want {
				t.Fatalf("configuration.listRemoteFolderItems = %v, want %v",
					conf["listRemoteFolderItems"], tt.want)
			}
		})
	}

	// The absent arm: a remote created without the flag echoes the canonical
	// default false (the always-present seat, the hardFail posture) — the
	// whole pre-T-448 behavior, zero regression.
	t.Run("absent echoes the false default", func(t *testing.T) {
		h := newBrowseFlagHarness(t)
		status, body := putRepoStatus(t, h, "browse-absent",
			`{"rclass":"remote","packageType":"helm","url":"http://127.0.0.1:9099"}`)
		if status != http.StatusOK {
			t.Fatalf("create status = %d; body=%s", status, body)
		}
		_, cfg := getRepoJSON(t, h, "browse-absent")
		conf := cfg["configuration"].(map[string]any)
		if conf["listRemoteFolderItems"] != false {
			t.Fatalf("configuration.listRemoteFolderItems = %v, want the false default",
				conf["listRemoteFolderItems"])
		}
	})
}

// TestRemoteBrowseFlagUpdateFlipsAndKeepsREST: the update plane. A
// config-carrying PUT is the Artifactory full-replace — the flag flips with
// it; a body without any type-relevant field keeps the stored config (the
// keep-current signal). A flag-only update still answers the url-required
// 400 (the remote arm's full-replace precondition, the FE contract for
// T-461: send the whole form, not the single knob).
func TestRemoteBrowseFlagUpdateFlipsAndKeepsREST(t *testing.T) {
	h := newBrowseFlagHarness(t)
	if status, body := putRepoStatus(t, h, "browse-up",
		`{"rclass":"remote","packageType":"helm","url":"http://127.0.0.1:9099",`+
			`"listRemoteFolderItems":true}`); status != http.StatusOK {
		t.Fatalf("create status = %d; body=%s", status, body)
	}

	// Flip off: the full body carrying an explicit false.
	if status, body := putRepoStatus(t, h, "browse-up",
		`{"url":"http://127.0.0.1:9099","listRemoteFolderItems":false}`); status != http.StatusOK {
		t.Fatalf("flip-off status = %d; body=%s", status, body)
	}
	_, cfg := getRepoJSON(t, h, "browse-up")
	conf := cfg["configuration"].(map[string]any)
	if conf["listRemoteFolderItems"] != false {
		t.Fatalf("after flip-off = %v, want false", conf["listRemoteFolderItems"])
	}

	// Description-only update: no type-relevant field, the stored config
	// (flag included) is kept untouched.
	if status, body := putRepoStatus(t, h, "browse-up", `{"description":"words only"}`); status != http.StatusOK {
		t.Fatalf("description-only status = %d; body=%s", status, body)
	}
	_, cfg = getRepoJSON(t, h, "browse-up")
	conf = cfg["configuration"].(map[string]any)
	if conf["listRemoteFolderItems"] != false {
		t.Fatalf("after description-only update = %v, want the kept false", conf["listRemoteFolderItems"])
	}

	// The POST update spelling rides the same transport and flips back on.
	resp := h.do(http.MethodPost, "/binflow/api/repositories/browse-up", adminUser, adminPass,
		[]byte(`{"url":"http://127.0.0.1:9099","listRemoteFolderItems":true}`),
		map[string]string{"Content-Type": "application/json"})
	if postBody := mustGet(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("POST update status = %d; body=%s", resp.StatusCode, postBody)
	}
	_, cfg = getRepoJSON(t, h, "browse-up")
	conf = cfg["configuration"].(map[string]any)
	if conf["listRemoteFolderItems"] != true {
		t.Fatalf("after POST flip-on = %v, want true", conf["listRemoteFolderItems"])
	}

	// A flag-only update is NOT a partial update: the remote arm's
	// full-replace demands the url, so the knob alone answers the
	// url-required 400 (same refusal every other remote field gets).
	status, body := putRepoStatus(t, h, "browse-up", `{"listRemoteFolderItems":false}`)
	if status != http.StatusBadRequest || !strings.Contains(body, "url is required") {
		t.Fatalf("flag-only update = (%d, %q), want 400 naming the url requirement", status, body)
	}
	_, cfg = getRepoJSON(t, h, "browse-up")
	conf = cfg["configuration"].(map[string]any)
	if conf["listRemoteFolderItems"] != true {
		t.Fatalf("after refused flag-only update = %v, want the kept true", conf["listRemoteFolderItems"])
	}
}

// TestRemoteBrowseFlagTypeRefusalREST: the field is part of the typed
// transport now, so a mistyped value fails the body decode with the field
// named (400) instead of the pre-D-T456-1 silent drop. A JSON null counts
// as absent (the explicit-null-is-absent read every nullable config field
// gets), and genuinely unknown fields keep the scenario-D tolerance.
func TestRemoteBrowseFlagTypeRefusalREST(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"string for a bool", `{"rclass":"remote","packageType":"helm","url":"http://127.0.0.1:9099","listRemoteFolderItems":"yes"}`},
		{"number for a bool", `{"rclass":"remote","packageType":"helm","url":"http://127.0.0.1:9099","listRemoteFolderItems":1}`},
		{"array for a bool", `{"rclass":"remote","packageType":"helm","url":"http://127.0.0.1:9099","listRemoteFolderItems":[]}`},
		{"object for a bool", `{"rclass":"remote","packageType":"helm","url":"http://127.0.0.1:9099","listRemoteFolderItems":{"enabled":true}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newBrowseFlagHarness(t)
			status, body := putRepoStatus(t, h, "browse-bad", tt.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d; body=%s", status, body)
			}
			if !strings.Contains(body, "listRemoteFolderItems") {
				t.Fatalf("body %q does not name listRemoteFolderItems", body)
			}
		})
	}

	t.Run("null counts as absent", func(t *testing.T) {
		h := newBrowseFlagHarness(t)
		status, body := putRepoStatus(t, h, "browse-null",
			`{"rclass":"remote","packageType":"helm","url":"http://127.0.0.1:9099","listRemoteFolderItems":null}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d; body=%s", status, body)
		}
		_, cfg := getRepoJSON(t, h, "browse-null")
		conf := cfg["configuration"].(map[string]any)
		if conf["listRemoteFolderItems"] != false {
			t.Fatalf("configuration.listRemoteFolderItems = %v, want the false default",
				conf["listRemoteFolderItems"])
		}
	})
}

// TestRemoteBrowseFlagBatch1GateREST: a `true` outside the enumeration
// engine's batch-1 set (helm, debian, rpm) answers the by-name 400
// repo.Service's parseRemoteConfig owns (the chartsBaseUrl posture — the
// admin would believe the tree merges upstream rows on a type whose
// upstream has no root-level enumeration). The wire leg of the gate
// internal/repo's t448_browse_test.go pins at the config face.
func TestRemoteBrowseFlagBatch1GateREST(t *testing.T) {
	for _, packageType := range []string{"generic", "docker", "npm"} {
		t.Run(packageType+" refuses true", func(t *testing.T) {
			h := newBrowseFlagHarness(t)
			status, body := putRepoStatus(t, h, "browse-gate-"+packageType,
				`{"rclass":"remote","packageType":"`+packageType+`",`+
					`"url":"http://127.0.0.1:9099","listRemoteFolderItems":true}`)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d; body=%s", status, body)
			}
			if !strings.Contains(body, "listRemoteFolderItems") {
				t.Fatalf("body %q does not name listRemoteFolderItems", body)
			}
		})
	}
}
