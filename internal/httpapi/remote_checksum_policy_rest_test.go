package httpapi_test

// T-607 / BIN-89 (rest/remote-domain-policy-enum-gate): the remote rclass's
// remoteRepoChecksumPolicyType — the closed four-value domain, the
// verbatim-refusal 400, and the verbatim echo on both write faces,
// live-pinned on A 7.161.26 (probe /tmp/t607, legs rrcp_*). The wording is
// the LOCAL family's variant: "found for:" without the "type" word —
// two literals, deliberately distinct (the local face's own legs live in
// checksum_policy_rest_test.go).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// rrcpCreate PUTs one remote repository with a remoteRepoChecksumPolicyType.
func rrcpCreate(t *testing.T, h *harness, key, policy string) (int, []byte) {
	t.Helper()
	body := fmt.Sprintf(
		`{"rclass":"remote","packageType":"generic","url":"http://example.com/up","remoteRepoChecksumPolicyType":%q}`,
		policy)
	return ctWrite(h, http.MethodPut, key, body, "application/json")
}

// rrcpReadback GETs the config and returns the echoed policy field.
func rrcpReadback(t *testing.T, h *harness, key string) (int, string) {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/repositories/"+key, adminUser, adminPass, nil, nil)
	defer resp.Body.Close() //nolint:errcheck // drained below
	status, raw := cptDrain(t, resp)
	if status != http.StatusOK {
		return status, string(raw)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode config (%s): %v", raw, err)
	}
	v, ok := cfg["remoteRepoChecksumPolicyType"]
	if !ok {
		return status, "<absent>"
	}
	s, ok := v.(string)
	if !ok {
		return status, fmt.Sprintf("<non-string %v>", v)
	}
	return status, s
}

// TestRemoteChecksumPolicyGoodEnumRoundTrip: the four legal spellings create
// and echo verbatim; the unset seat renders the measured default.
func TestRemoteChecksumPolicyGoodEnumRoundTrip(t *testing.T) {
	h := newHarnessCfg(t, nil, nil)

	for _, policy := range []string{
		"generate-if-absent", "fail", "ignore-and-generate", "pass-thru",
	} {
		key := "t607-rrcp-" + policy
		status, body := rrcpCreate(t, h, key, policy)
		if status != http.StatusOK {
			t.Fatalf("create %s = %d (%s), want 200", key, status, body)
		}
		if st, got := rrcpReadback(t, h, key); st != http.StatusOK || got != policy {
			t.Fatalf("readback %s = (%d, %q), want the verbatim %q", key, st, got, policy)
		}
	}

	// The unset posture — and an EXPLICIT empty value (live A: 200 create,
	// readback renders the default; the empty spelling is legal like the
	// local family's) — renders the reference's measured default.
	for _, tt := range []struct{ key, body string }{
		{"t607-rrcp-unset", `{"rclass":"remote","packageType":"generic","url":"http://example.com/up"}`},
		{"t607-rrcp-empty", `{"rclass":"remote","packageType":"generic","url":"http://example.com/up","remoteRepoChecksumPolicyType":""}`},
	} {
		status, rbody := ctWrite(h, http.MethodPut, tt.key, tt.body, "application/json")
		if status != http.StatusOK {
			t.Fatalf("create %s = %d (%s), want 200", tt.key, status, rbody)
		}
		if st, got := rrcpReadback(t, h, tt.key); st != http.StatusOK || got != "generate-if-absent" {
			t.Fatalf("readback %s = (%d, %q), want the default generate-if-absent", tt.key, st, got)
		}
	}
}

// TestRemoteChecksumPolicyBadEnumRefusal: everything outside the four-value
// domain (strict, none, case variants) refuses with the reference's literal
// wording, verbatim-interpolated, and leaves no repository behind.
func TestRemoteChecksumPolicyBadEnumRefusal(t *testing.T) {
	h := newHarnessCfg(t, nil, nil)

	for _, policy := range []string{"strict", "none", "FAIL", "pass-through"} {
		key := "t607-rrcp-bad"
		status, body := rrcpCreate(t, h, key, policy)
		if status != http.StatusBadRequest {
			t.Fatalf("create with %q = %d (%s), want 400", policy, status, body)
		}
		var env cptErrBody
		if err := json.Unmarshal(body, &env); err != nil {
			t.Fatalf("decode envelope (%s): %v", body, err)
		}
		want := fmt.Sprintf("No checksum policy type found for: %s", policy)
		if len(env.Errors) != 1 || env.Errors[0].Status != 400 || env.Errors[0].Message != want {
			t.Fatalf("envelope for %q = %+v, want the verbatim %q", policy, env, want)
		}
	}

	// Zero side effects: the refused key never landed.
	status, body := rrcpCreate(t, h, "t607-rrcp-bad", "fail")
	if status != http.StatusOK {
		t.Fatalf("create after refusal = %d (%s), want 200 (no side effects)", status, body)
	}
}

// TestRemoteChecksumPolicyUpdateFace: the update face shares the gate and
// the persistence — a flip stores and echoes the new value verbatim, a
// bad enum refuses, and an omitted key keeps the stored value.
func TestRemoteChecksumPolicyUpdateFace(t *testing.T) {
	h := newHarnessCfg(t, nil, nil)

	status, body := rrcpCreate(t, h, "t607-rrcp-upd", "fail")
	if status != http.StatusOK {
		t.Fatalf("seed create = %d (%s), want 200", status, body)
	}

	// Flip: persists and echoes verbatim.
	status, body = ctWrite(h, http.MethodPost, "t607-rrcp-upd",
		`{"remoteRepoChecksumPolicyType":"pass-thru"}`, "application/json")
	if status != http.StatusOK {
		t.Fatalf("update flip = %d (%s), want 200", status, body)
	}
	if st, got := rrcpReadback(t, h, "t607-rrcp-upd"); st != http.StatusOK || got != "pass-thru" {
		t.Fatalf("readback after flip = (%d, %q), want pass-thru", st, got)
	}

	// Bad enum on the update face: the same 400 literal, no mutation.
	status, body = ctWrite(h, http.MethodPost, "t607-rrcp-upd",
		`{"remoteRepoChecksumPolicyType":"strict"}`, "application/json")
	if status != http.StatusBadRequest {
		t.Fatalf("update badenum = %d (%s), want 400", status, body)
	}
	var env cptErrBody
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope (%s): %v", body, err)
	}
	if len(env.Errors) != 1 || env.Errors[0].Message != "No checksum policy type found for: strict" {
		t.Fatalf("envelope = %+v, want the strict literal", env)
	}
	if st, got := rrcpReadback(t, h, "t607-rrcp-upd"); st != http.StatusOK || got != "pass-thru" {
		t.Fatalf("readback after refused update = (%d, %q), want the untouched pass-thru", st, got)
	}

	// Omitted key keeps the stored value (merge-on-omit).
	status, body = ctWrite(h, http.MethodPost, "t607-rrcp-upd",
		`{"description":"kept"}`, "application/json")
	if status != http.StatusOK {
		t.Fatalf("update unrelated = %d (%s), want 200", status, body)
	}
	if st, got := rrcpReadback(t, h, "t607-rrcp-upd"); st != http.StatusOK || got != "pass-thru" {
		t.Fatalf("readback after omit = (%d, %q), want the kept pass-thru", st, got)
	}
}
