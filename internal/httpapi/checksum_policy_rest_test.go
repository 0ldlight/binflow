package httpapi_test

// T-585 (BIN-67 / L039 C6): the REST config plane's checksumPolicyType enum
// refusal and echo. The create (PUT) and update (POST) faces carry the
// reference's literal 400 wording verbatim in the errors[] envelope
// (live-pinned on 7.161.26, L039 Arm 6), a refused create leaves no
// repository behind, and the legal spellings round-trip through the
// config-read face — the set value echoes verbatim, the unset seat renders
// the product default client-checksums.

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// cptErrBody is the errors[] envelope slice the refusal asserts on.
type cptErrBody struct {
	Errors []struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
	} `json:"errors"`
}

// cptPutCreate runs one admin create PUT and returns status + body bytes.
func cptPutCreate(t *testing.T, h *harness, key, body string) (int, []byte) {
	t.Helper()
	resp := h.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass, []byte(body), repoConfigCT())
	defer resp.Body.Close() //nolint:errcheck // drained below
	return cptDrain(t, resp)
}

// cptPostUpdate runs one admin update POST and returns status + body bytes.
func cptPostUpdate(t *testing.T, h *harness, key, body string) (int, []byte) {
	t.Helper()
	resp := h.do(http.MethodPost, "/binflow/api/repositories/"+key, adminUser, adminPass, []byte(body), repoConfigCT())
	defer resp.Body.Close() //nolint:errcheck // drained below
	return cptDrain(t, resp)
}

func cptDrain(t *testing.T, resp *http.Response) (int, []byte) {
	t.Helper()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, buf
}

// TestChecksumPolicyRestEnumRefusal: the illegal-enum 400 family — message
// verbatim, errors[] envelope shape, and zero side effects (the key stays
// creatable afterwards).
func TestChecksumPolicyRestEnumRefusal(t *testing.T) {
	h := newHarnessCfg(t, nil, nil)

	status, body := cptPutCreate(t, h, "cpt-none", `{"rclass":"local","packageType":"maven","checksumPolicyType":"none"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("create with none = %d (%s), want 400", status, body)
	}
	var env cptErrBody
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope (%s): %v", body, err)
	}
	if len(env.Errors) != 1 || env.Errors[0].Status != 400 ||
		env.Errors[0].Message != "No checksum policy type found for type: none" {
		t.Fatalf("envelope = %+v, want the reference literal at status 400", env)
	}

	// Nothing landed: the same key is still creatable.
	status, body = cptPutCreate(t, h, "cpt-none", `{"rclass":"local","packageType":"maven"}`)
	if status != http.StatusOK {
		t.Fatalf("create after refusal = %d (%s), want 200 (no side effects)", status, body)
	}

	// The update face shares the gate and the wording.
	status, body = cptPostUpdate(t, h, "cpt-none", `{"checksumPolicyType":"none"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("update with none = %d (%s), want 400", status, body)
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope (%s): %v", body, err)
	}
	if len(env.Errors) != 1 || env.Errors[0].Message != "No checksum policy type found for type: none" {
		t.Fatalf("update envelope = %+v, want the reference literal", env)
	}
}

// TestChecksumPolicyRestEchoRoundTrip: the cfg-read echo legs — both legal
// spellings round-trip verbatim; the unset seat renders the product default.
func TestChecksumPolicyRestEchoRoundTrip(t *testing.T) {
	h := newHarnessCfg(t, nil, nil)

	for _, tt := range []struct{ key, body, want string }{
		{"cpt-client", `{"rclass":"local","packageType":"maven","checksumPolicyType":"client-checksums"}`, "client-checksums"},
		{"cpt-srvgen", `{"rclass":"local","packageType":"maven","checksumPolicyType":"server-generated-checksums"}`, "server-generated-checksums"},
		{"cpt-unset", `{"rclass":"local","packageType":"maven"}`, "client-checksums"},
	} {
		status, body := cptPutCreate(t, h, tt.key, tt.body)
		if status != http.StatusOK {
			t.Fatalf("create %s = %d (%s), want 200", tt.key, status, body)
		}
		resp := h.do(http.MethodGet, "/binflow/api/repositories/"+tt.key, adminUser, adminPass, nil, nil)
		status, raw := cptDrain(t, resp)
		resp.Body.Close() //nolint:errcheck // drained above
		if status != http.StatusOK {
			t.Fatalf("GET %s = %d (%s), want 200", tt.key, status, raw)
		}
		var cfg map[string]any
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatalf("decode config (%s): %v", raw, err)
		}
		if got := cfg["checksumPolicyType"]; got != tt.want {
			t.Fatalf("GET %s checksumPolicyType = %v, want %q", tt.key, got, tt.want)
		}
	}
}
