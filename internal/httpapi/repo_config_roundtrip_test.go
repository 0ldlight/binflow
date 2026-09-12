package httpapi_test

// T-490 (FR-156.1): the configJSON four-domain round-trip over the real
// REST plane — the wire face of the M16 T-439 drift closure. The decode-only
// posture (PUT 200, GET keyless) is inverted: every B-1.5 domain plus the
// Stage domain's two spellings rides PUT -> stored -> GET configuration
// verbatim (the web-side tripwire of t439-form-stepper.spec.ts is the FE
// promotion signal this inversion trips, deliberately left for T-519 to
// flip green on the spec side), and blackedOut=true refuses the write plane
// — the generic content PUT and the docker push (blob upload + manifest
// publish) answer the spec's 404 with its exact message.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// getConfiguration fetches one repository's echoed configuration map.
func getConfiguration(t *testing.T, h *harness, key string) map[string]any {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/repositories/"+key, adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d body=%s", key, resp.StatusCode, body)
	}
	var m struct {
		Configuration map[string]any `json:"configuration"`
	}
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("GET %s body %q: %v", key, body, err)
	}
	return m.Configuration
}

// TestRepoConfigFourDomainRoundTripWire: the AC's table — one row per
// domain, create arm and update arm both, per-key echo equality.
func TestRepoConfigFourDomainRoundTripWire(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value any // the PUT body's JSON value
		want  any // the expected echoed value
	}{
		{"repoLayoutRef", "repoLayoutRef", "maven-2-default", "maven-2-default"},
		{"blackedOut true", "blackedOut", true, true},
		{"blackedOut explicit false", "blackedOut", false, false},
		{"maxUniqueSnapshots", "maxUniqueSnapshots", 7, float64(7)},
		{"maxUniqueSnapshots explicit 0", "maxUniqueSnapshots", 0, float64(0)},
		{"archiveBrowsingEnabled", "archiveBrowsingEnabled", true, true},
		{"environments spelling", "environments", []string{"DEV", "PROD"}, []any{"DEV", "PROD"}},
		{"stages spelling (7.161-era alias)", "stages", []string{"BOX"}, []any{"BOX"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			valueJSON, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("marshal value: %v", err)
			}
			body := fmt.Sprintf(`{"rclass":"local","packageType":"generic","%s":%s}`,
				tt.field, valueJSON)
			resp := putRepo(t, h, "lib", body)
			if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusOK {
				t.Fatalf("create status %d body=%s", code, out)
			}
			cfg := getConfiguration(t, h, "lib")
			got, ok := cfg[tt.field]
			if !ok {
				t.Fatalf("create echo: configuration %v lacks %q (the T-439 decode-only drift)", cfg, tt.field)
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("create echo: %s = %#v, want %#v", tt.field, got, tt.want)
			}

			// Update arm: a different value on the same row.
			updated := map[string]any{
				"repoLayoutRef":          "simple-default",
				"blackedOut":             false,
				"maxUniqueSnapshots":     3,
				"archiveBrowsingEnabled": true,
				"environments":           []string{"PROD"},
				"stages":                 []string{"PROD"},
			}[tt.field]
			updatedJSON, _ := json.Marshal(updated)
			body = fmt.Sprintf(`{"rclass":"local","%s":%s}`, tt.field, updatedJSON)
			resp = postRepo(t, h, "lib", body)
			if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusOK {
				t.Fatalf("update status %d body=%s", code, out)
			}
			cfg = getConfiguration(t, h, "lib")
			var wantUpdated any
			switch v := updated.(type) {
			case int:
				wantUpdated = float64(v)
			case string:
				wantUpdated = v
			case bool:
				wantUpdated = v
			case []string:
				wantUpdated = toAnySlice(v)
			}
			if fmt.Sprint(cfg[tt.field]) != fmt.Sprint(wantUpdated) {
				t.Errorf("update echo: %s = %#v, want %#v", tt.field, cfg[tt.field], wantUpdated)
			}
		})
	}
}

// toAnySlice widens one string slice for the generic comparison.
func toAnySlice(v []string) []any {
	out := make([]any, len(v))
	for i, s := range v {
		out[i] = s
	}
	return out
}

// TestRepoConfigTripwireBodyInvertedWire: the EXACT body of the T-439
// tripwire leg (local maven, all four B-1.5 domains, includesPattern as the
// forwarded-control) now echoes every key — this is the Go-side green the
// web spec's drift pin flips to when T-519 promotes the reserved slots.
func TestRepoConfigTripwireBodyInvertedWire(t *testing.T) {
	h := newHarness(t)
	resp := putRepo(t, h, "t439d", `{
		"rclass":"local","packageType":"maven",
		"repoLayoutRef":"maven-2-default","blackedOut":true,
		"maxUniqueSnapshots":7,"archiveBrowsingEnabled":true,
		"includesPattern":"**/*.jar"
	}`)
	if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusOK {
		t.Fatalf("put status %d body=%s", code, out)
	}
	cfg := getConfiguration(t, h, "t439d")
	for key, want := range map[string]any{
		"includesPattern":        "**/*.jar",
		"repoLayoutRef":          "maven-2-default",
		"blackedOut":             true,
		"maxUniqueSnapshots":     float64(7),
		"archiveBrowsingEnabled": true,
	} {
		got, ok := cfg[key]
		if !ok {
			t.Errorf("configuration lacks %q (drift not closed): %v", key, cfg)
			continue
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s = %#v, want %#v", key, got, want)
		}
	}
}

// TestRepoConfigStageRefusalsWire: the two Stage spellings are one knob —
// a disagreement answers 400 naming both spellings.
func TestRepoConfigStageRefusalsWire(t *testing.T) {
	h := newHarness(t)
	resp := putRepo(t, h, "lib",
		`{"rclass":"local","packageType":"generic","environments":["DEV"],"stages":["PROD"]}`)
	eb := decodeError(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	msg := eb.Errors[0].Message
	if !strings.Contains(msg, "environments") || !strings.Contains(msg, "stages") {
		t.Errorf("message %q does not name both spellings", msg)
	}
}

// TestRepoConfigMistypedDomainWire: a mistyped blackedOut fails the body
// decode with the field named (typing rides the decode, the transport
// family's posture).
func TestRepoConfigMistypedDomainWire(t *testing.T) {
	h := newHarness(t)
	resp := putRepo(t, h, "lib", `{"rclass":"local","blackedOut":"yes"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if msg := decodeError(t, resp).Errors[0].Message; !strings.Contains(msg, "blackedOut") {
		t.Errorf("message %q does not name the field", msg)
	}
}

// TestRepoConfigRemoteArmScopeWire (L006-A, D02-R03/R04): the live
// reference (:8082, 7.161.20) round-trips ALL FOUR domains on a remote
// repository — the earlier pin ("the family is the LOCAL arm's") was the
// T-439 drift's own boundary, overturned by evidence. The virtual arm is
// the reference's narrower scope: repoLayoutRef echoes when set, the other
// three drop.
func TestRepoConfigRemoteArmScopeWire(t *testing.T) {
	h := newHarness(t)
	resp := putRepo(t, h, "rem", `{
		"rclass":"remote","packageType":"generic","url":"https://upstream.example/px",
		"blackedOut":true,"repoLayoutRef":"simple-default",
		"maxUniqueSnapshots":7,"archiveBrowsingEnabled":true
	}`)
	if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusOK {
		t.Fatalf("create status %d body=%s", code, out)
	}
	cfg := getConfiguration(t, h, "rem")
	for key, want := range map[string]any{
		"blackedOut": true, "repoLayoutRef": "simple-default",
		"maxUniqueSnapshots": float64(7), "archiveBrowsingEnabled": true,
	} {
		if cfg[key] != want {
			t.Errorf("remote echo %q = %v (%T), want %v", key, cfg[key], cfg[key], want)
		}
	}
}

// TestRepoConfigVirtualArmScopeWire (L006-A): the reference's virtual arm
// keeps repoLayoutRef (set → echo, absent → key omitted: no xsd default on
// this arm) and DROPS the other three domains sent to it.
func TestRepoConfigVirtualArmScopeWire(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "lib")
	resp := putRepo(t, h, "vscope", `{
		"rclass":"virtual","packageType":"generic","repositories":["lib"],
		"repoLayoutRef":"simple-default","blackedOut":true,
		"maxUniqueSnapshots":9,"archiveBrowsingEnabled":true
	}`)
	if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusOK {
		t.Fatalf("create status %d body=%s", code, out)
	}
	cfg := getConfiguration(t, h, "vscope")
	if cfg["repoLayoutRef"] != "simple-default" {
		t.Errorf("virtual echo repoLayoutRef = %v, want simple-default (full %v)", cfg["repoLayoutRef"], cfg)
	}
	for _, key := range []string{"blackedOut", "maxUniqueSnapshots", "archiveBrowsingEnabled"} {
		if _, ok := cfg[key]; ok {
			t.Errorf("virtual echo unexpectedly carries %q — the reference drops it on the virtual arm: %v", key, cfg)
		}
	}

	// The bare virtual body echoes WITHOUT repoLayoutRef (the reference's
	// virtual arm has no default).
	resp = putRepo(t, h, "vbare", `{"rclass":"virtual","packageType":"generic","repositories":["lib"]}`)
	if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusOK {
		t.Fatalf("bare create status %d body=%s", code, out)
	}
	if cfg := getConfiguration(t, h, "vbare"); cfg != nil {
		if _, ok := cfg["repoLayoutRef"]; ok {
			t.Errorf("bare virtual echo carries repoLayoutRef %v — the reference omits it when unset", cfg)
		}
	}
}

// TestBlackedOutWriteRefusalWire: the behavior linkage — blackedOut=true
// refuses the generic content PUT with the spec's 404 and message, docker
// push refuses on both faces (blob upload, manifest publish), and the
// flip-off update reopens the plane.
func TestBlackedOutWriteRefusalWire(t *testing.T) {
	const blackoutMsg = "is blacked out and cannot serve artifact"
	h := newHarness(t)
	seedRepo(t, h, "lib")
	seedDockerRepo(t, h, "dock")

	// Black the generic repository out through the config plane.
	resp := postRepo(t, h, "lib", `{"rclass":"local","blackedOut":true}`)
	if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusOK {
		t.Fatalf("blackout update status %d body=%s", code, out)
	}

	// Content PUT refuses: 404, the errors[] envelope, the spec message.
	resp = h.do(http.MethodPut, "/binflow/lib/a/b.txt", adminUser, adminPass,
		[]byte("x"), nil)
	eb := decodeError(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("content PUT status = %d body=%s", resp.StatusCode, eb.Errors[0].Message)
	}
	if !strings.Contains(eb.Errors[0].Message, blackoutMsg) ||
		!strings.Contains(eb.Errors[0].Message, "'lib'") {
		t.Errorf("content PUT message = %q, want the blackout wording naming lib", eb.Errors[0].Message)
	}

	// Flip off: the write plane reopens.
	resp = postRepo(t, h, "lib", `{"rclass":"local","blackedOut":false}`)
	if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusOK {
		t.Fatalf("flip-off status %d body=%s", code, out)
	}
	resp = h.do(http.MethodPut, "/binflow/lib/a/b.txt", adminUser, adminPass,
		[]byte("x"), nil)
	if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusCreated {
		t.Fatalf("reopened PUT status %d body=%s", code, out)
	}

	// The docker push: monolithic blob upload and manifest publish both
	// refuse while the repository is blacked out, then succeed after the
	// flip-off. The layer lands BEFORE the blackout so the manifest leg's
	// refusal is the blackout alone (its referenced blob exists).
	layer := []byte("layer-bytes")
	dgst := "sha256:" + sha256Of(layer)
	resp = h.do(http.MethodPost, "/v2/dock/app/blobs/uploads/?digest="+dgst,
		adminUser, adminPass, layer, nil)
	if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusCreated {
		t.Fatalf("baseline blob upload status %d body=%s", code, out)
	}

	resp = postRepo(t, h, "dock", `{"rclass":"local","blackedOut":true}`)
	if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusOK {
		t.Fatalf("docker blackout status %d body=%s", code, out)
	}
	fresh := []byte("config-bytes")
	freshDgst := "sha256:" + sha256Of(fresh)
	resp = h.do(http.MethodPost, "/v2/dock/app/blobs/uploads/?digest="+freshDgst,
		adminUser, adminPass, fresh, nil)
	if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusNotFound {
		t.Fatalf("blob upload status %d body=%s, want 404", code, out)
	} else if !strings.Contains(out, blackoutMsg) {
		t.Fatalf("blob upload body %q lacks the blackout wording", out)
	}

	manifest := []byte(fmt.Sprintf(
		`{"schemaVersion":2,"config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":%q,"size":35}}`, dgst))
	resp = h.do(http.MethodPut, "/v2/dock/app/manifests/v1", adminUser, adminPass,
		manifest, map[string]string{"Content-Type": "application/vnd.docker.distribution.manifest.v2+json"})
	if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusNotFound {
		t.Fatalf("manifest publish status %d body=%s, want 404", code, out)
	} else if !strings.Contains(out, blackoutMsg) {
		t.Fatalf("manifest publish body %q lacks the blackout wording", out)
	}

	// Flip the registry back on: the same push lands (the mark is a live
	// switch, not a sticky refusal).
	resp = postRepo(t, h, "dock", `{"rclass":"local","blackedOut":false}`)
	if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusOK {
		t.Fatalf("docker flip-off status %d body=%s", code, out)
	}
	resp = h.do(http.MethodPost, "/v2/dock/app/blobs/uploads/?digest="+freshDgst,
		adminUser, adminPass, fresh, nil)
	if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusCreated {
		t.Fatalf("reopened blob upload status %d body=%s", code, out)
	}
}
