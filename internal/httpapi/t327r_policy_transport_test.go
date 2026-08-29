package httpapi_test

// T-327R (the D-B unlock, transport half): the deb/rpm index-engine policy
// keys ride the repositories REST plane. Before this ticket the configJSON
// local arm only forwarded the generic/maven/governance families, so a PUT
// carrying byHash / calculateYumMetadata & co. answered 200 and silently
// dropped them (the scenario-D tolerance swallowing keys the adapters
// actually read). These tests pin the contract at the transport level on a
// generic local repository — the local arm is package-type-agnostic by
// design (the maven family's posture), and the licensed deb/rpm legs that
// prove the keys DRIVE their engines live in the adapter packages.

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// TestDebRpmPolicyKeysRoundTripREST: create carrying the full deb + rpm
// policy key set, then GET — every key must echo back under
// "configuration" exactly as sent (D-B's failing assertion: PUT 200, GET
// keyless). The explicit false / empty-array / zero arms are deliberate:
// pointer fields keep them distinct from absent, which is what lets an
// operator flip calculateYumMetadata back off.
func TestDebRpmPolicyKeysRoundTripREST(t *testing.T) {
	h := newHarness(t)
	status, body := putRepoStatus(t, h, "policy-local",
		`{"rclass":"local","packageType":"generic",`+
			`"byHash":"SHA256","optionalIndexCompressionFormats":["xz","lzma"],`+
			`"debianDefaultArchitectures":"i386,amd64","historyCycles":5,`+
			`"origin":"ops.example.org","label":"production",`+
			`"calculateYumMetadata":true,"yumRootDepth":4,`+
			`"enableFileListsIndexing":true,"yumGroupFileNames":"comps.xml,groups.xml"}`)
	if status != http.StatusOK {
		t.Fatalf("create status = %d; body=%s", status, body)
	}

	code, cfg := getRepoJSON(t, h, "policy-local")
	if code != http.StatusOK {
		t.Fatalf("GET status = %d", code)
	}
	conf, ok := cfg["configuration"].(map[string]any)
	if !ok {
		t.Fatalf("configuration missing: %v", cfg)
	}
	want := map[string]any{
		"byHash":                          "SHA256",
		"optionalIndexCompressionFormats": []any{"xz", "lzma"},
		"debianDefaultArchitectures":      "i386,amd64",
		"historyCycles":                   float64(5),
		"origin":                          "ops.example.org",
		"label":                           "production",
		"calculateYumMetadata":            true,
		"yumRootDepth":                    float64(4),
		"enableFileListsIndexing":         true,
		"yumGroupFileNames":               "comps.xml,groups.xml",
	}
	for k, v := range want {
		if !reflect.DeepEqual(conf[k], v) {
			t.Errorf("configuration[%s] = %v, want %v", k, conf[k], v)
		}
	}

	// The explicit-zero/family off arm: a second repository carrying false,
	// 0 and the empty compression array must echo those EXACT values, not
	// drop them (the product default of calculateYumMetadata is false — the
	// flip-off update only works because the pointer survives transport).
	status, body = putRepoStatus(t, h, "policy-off",
		`{"rclass":"local","packageType":"generic","calculateYumMetadata":false,`+
			`"yumRootDepth":0,"enableFileListsIndexing":false,`+
			`"optionalIndexCompressionFormats":[],"byHash":"NONE"}`)
	if status != http.StatusOK {
		t.Fatalf("create (off arm) status = %d; body=%s", status, body)
	}
	_, cfg = getRepoJSON(t, h, "policy-off")
	conf = cfg["configuration"].(map[string]any)
	for k, v := range map[string]any{
		"calculateYumMetadata":            false,
		"yumRootDepth":                    float64(0),
		"enableFileListsIndexing":         false,
		"optionalIndexCompressionFormats": []any{},
		"byHash":                          "NONE",
	} {
		if !reflect.DeepEqual(conf[k], v) {
			t.Errorf("off-arm configuration[%s] = %v, want %v", k, conf[k], v)
		}
	}
}

// TestDebRpmPolicyKeysUpdateReplacesConfig: a policy-carrying update is the
// Artifactory full-replace PUT — the echoed configuration is exactly the
// new body's keys, and an update WITHOUT any type-relevant field keeps the
// stored one (the keep-current signal).
func TestDebRpmPolicyKeysUpdateReplacesConfig(t *testing.T) {
	h := newHarness(t)
	if status, body := putRepoStatus(t, h, "policy-up",
		`{"rclass":"local","packageType":"generic","byHash":"ALL","calculateYumMetadata":true}`); status != http.StatusOK {
		t.Fatalf("create status = %d; body=%s", status, body)
	}
	if status, body := putRepoStatus(t, h, "policy-up", `{"byHash":"NONE"}`); status != http.StatusOK {
		t.Fatalf("update status = %d; body=%s", status, body)
	}
	_, cfg := getRepoJSON(t, h, "policy-up")
	conf := cfg["configuration"].(map[string]any)
	if conf["byHash"] != "NONE" {
		t.Errorf("byHash after update = %v, want NONE", conf["byHash"])
	}
	if _, still := conf["calculateYumMetadata"]; still {
		t.Errorf("full-replace dropped nothing — calculateYumMetadata = %v survived", conf["calculateYumMetadata"])
	}
	// The POST update spelling rides the same transport.
	resp := h.do(http.MethodPost, "/binflow/api/repositories/policy-up", adminUser, adminPass,
		[]byte(`{"byHash":"SHA256"}`), map[string]string{"Content-Type": "application/json"})
	if postBody := mustGet(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("POST update status = %d; body=%s", resp.StatusCode, postBody)
	}
	// Description-only PUT: no type-relevant field, configuration kept.
	if status, body := putRepoStatus(t, h, "policy-up", `{"description":"words only"}`); status != http.StatusOK {
		t.Fatalf("description-only status = %d; body=%s", status, body)
	}
	_, cfg = getRepoJSON(t, h, "policy-up")
	conf = cfg["configuration"].(map[string]any)
	if conf["byHash"] != "SHA256" {
		t.Errorf("byHash after description-only update = %v, want the kept SHA256", conf["byHash"])
	}
}

// TestDebRpmPolicyKeysTypeRefusal: the keys are part of the typed transport
// now, so a mistyped value fails the body decode with the field named
// (400) instead of the pre-T-327R silent drop. Genuinely unknown fields
// keep the scenario-D tolerance — that posture is untouched.
func TestDebRpmPolicyKeysTypeRefusal(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"byHash not a string", `{"rclass":"local","packageType":"generic","byHash":123}`, "byHash"},
		{"calculateYumMetadata not a bool", `{"rclass":"local","packageType":"generic","calculateYumMetadata":"yes"}`, "calculateYumMetadata"},
		{"yumRootDepth not a number", `{"rclass":"local","packageType":"generic","yumRootDepth":"deep"}`, "yumRootDepth"},
		{"optionalIndexCompressionFormats not an array", `{"rclass":"local","packageType":"generic","optionalIndexCompressionFormats":"xz"}`, "optionalIndexCompressionFormats"},
		{"historyCycles not a number", `{"rclass":"local","packageType":"generic","historyCycles":true}`, "historyCycles"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			status, body := putRepoStatus(t, h, "policy-bad", tt.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d; body=%s", status, body)
			}
			if !strings.Contains(body, tt.want) {
				t.Fatalf("body %q does not name %q", body, tt.want)
			}
		})
	}
	// The tolerance boundary: a key outside every family still drops
	// silently (200, absent from the echo) — scenario-D is for the
	// unknown, not for the contracted.
	t.Run("unknown key still tolerated", func(t *testing.T) {
		h := newHarness(t)
		status, body := putRepoStatus(t, h, "policy-tol",
			`{"rclass":"local","packageType":"generic","someFutureField":true}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d; body=%s", status, body)
		}
		_, cfg := getRepoJSON(t, h, "policy-tol")
		conf, ok := cfg["configuration"].(map[string]any)
		if ok {
			if _, there := conf["someFutureField"]; there {
				raw, _ := json.Marshal(conf)
				t.Fatalf("unknown field leaked into the config: %s", raw)
			}
		}
	})
}

// TestByHashEnumValueDomainREST (T-346 / FR-113.2 BE, the T-327R leftover ②
// / K45 ruling): byHash values outside ALL/SHA256/NONE (debian.md section 5)
// answer 400 naming the enum at both the create and the replace arm — the
// pre-T-346 posture (store verbatim, behave as NONE) is retired. The
// gate itself lives in repo.Service's validateLocalConfig; this is the REST
// leg of L29-BE.
func TestByHashEnumValueDomainREST(t *testing.T) {
	t.Run("illegal value on create refuses with the enum", func(t *testing.T) {
		h := newHarness(t)
		status, body := putRepoStatus(t, h, "policy-enum",
			`{"rclass":"local","packageType":"generic","byHash":"strong"}`)
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d; body=%s", status, body)
		}
		if !strings.Contains(body, "byHash") || !strings.Contains(body, "ALL, SHA256, NONE") {
			t.Fatalf("body %q does not name the field and the enum", body)
		}
	})
	t.Run("illegal value on update refuses", func(t *testing.T) {
		h := newHarness(t)
		if status, body := putRepoStatus(t, h, "policy-enum-up",
			`{"rclass":"local","packageType":"generic","byHash":"ALL"}`); status != http.StatusOK {
			t.Fatalf("create status = %d; body=%s", status, body)
		}
		status, body := putRepoStatus(t, h, "policy-enum-up", `{"byHash":"all"}`)
		if status != http.StatusBadRequest {
			t.Fatalf("update status = %d; body=%s", status, body)
		}
		if !strings.Contains(body, "ALL, SHA256, NONE") {
			t.Fatalf("body %q does not name the enum", body)
		}
		// The refused update left the stored value untouched.
		_, cfg := getRepoJSON(t, h, "policy-enum-up")
		conf := cfg["configuration"].(map[string]any)
		if conf["byHash"] != "ALL" {
			t.Fatalf("byHash after refused update = %v, want the kept ALL", conf["byHash"])
		}
	})
}
