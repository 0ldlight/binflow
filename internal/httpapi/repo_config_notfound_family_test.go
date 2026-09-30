package httpapi_test

// T-607 / BIN-89 (rest/repo-update-missing-key-404-wording): the repo
// config face's missing-key 404 method family, live-pinned on A 7.161.26
// (probe /tmp/t607, legs post_exact_missing/post_charset_existing/
// post_charset_missing/get_missing/delete_missing/delete_existing):
//
//   - POST missing key: 404 errors[] envelope, the QUOTED literal —
//     Content-Type independent;
//   - POST existing key under a PARAMETERIZED Content-Type: the
//     vendor-negotiation 404 with the NO-QUOTE literal, and the update
//     does not happen;
//   - GET missing: the bare 400 "Bad Request" quirk (already aligned,
//     re-anchored here so the family stays pinned together);
//   - DELETE missing: 404 with the statusMsg REPORT body (repoKey/
//     statusMsg/deletedArtifactsCount/success), the success path's shape
//     with the refusal wording and zeroed counts.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestRepoPostMissingKeyQuoted404: the update spelling's unknown-key answer.
func TestRepoPostMissingKeyQuoted404(t *testing.T) {
	h := newHarnessCfg(t, nil, nil)

	for _, tt := range []struct {
		name string
		ct   string
	}{
		{"exact content type", "application/json"},
		{"parameterized content type", "application/json;charset=UTF-8"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status, body := ctWrite(h, http.MethodPost, "t607-no-such-repo",
				`{"description":"x"}`, tt.ct)
			if status != http.StatusNotFound {
				t.Fatalf("status = %d (%s), want 404", status, body)
			}
			var env cptErrBody
			if err := json.Unmarshal(body, &env); err != nil {
				t.Fatalf("decode envelope (%s): %v", body, err)
			}
			want := "No repository 't607-no-such-repo' was found."
			if len(env.Errors) != 1 || env.Errors[0].Status != 404 || env.Errors[0].Message != want {
				t.Fatalf("envelope = %+v, want the quoted literal %q", env, want)
			}
		})
	}
}

// TestRepoPostCharsetExistingNoQuote404: an EXISTING key under a
// parameterized Content-Type answers the no-quote negotiation 404 and the
// stored configuration is untouched.
func TestRepoPostCharsetExistingNoQuote404(t *testing.T) {
	h := newHarnessCfg(t, nil, nil)

	status, body := ctWrite(h, http.MethodPut, "t607-nq-exists",
		`{"rclass":"local","packageType":"generic","description":"before"}`, "application/json")
	if status != http.StatusOK {
		t.Fatalf("seed create = %d (%s), want 200", status, body)
	}
	status, body = ctWrite(h, http.MethodPost, "t607-nq-exists",
		`{"description":"after"}`, "application/json;charset=UTF-8")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d (%s), want 404", status, body)
	}
	var env cptErrBody
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope (%s): %v", body, err)
	}
	want := "No repository t607-nq-exists was found."
	if len(env.Errors) != 1 || env.Errors[0].Status != 404 || env.Errors[0].Message != want {
		t.Fatalf("envelope = %+v, want the NO-quote literal %q", env, want)
	}

	// The update did not happen: the description survives verbatim.
	resp := h.do(http.MethodGet, "/binflow/api/repositories/t607-nq-exists", adminUser, adminPass, nil, nil)
	status, raw := cptDrain(t, resp)
	resp.Body.Close() //nolint:errcheck // drained above
	if status != http.StatusOK {
		t.Fatalf("GET = %d (%s), want 200", status, raw)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode config (%s): %v", raw, err)
	}
	if got := cfg["description"]; got != "before" {
		t.Fatalf("description = %v, want the untouched %q", got, "before")
	}

	// The same update under the exact Content-Type still succeeds.
	status, body = ctWrite(h, http.MethodPost, "t607-nq-exists",
		`{"description":"after"}`, "application/json")
	if status != http.StatusOK {
		t.Fatalf("exact-CT update = %d (%s), want 200", status, body)
	}
}

// TestRepoGetMissingKeyBad400: the read face's own quirk — the bare 400,
// re-anchored beside its write-face siblings.
func TestRepoGetMissingKeyBad400(t *testing.T) {
	h := newHarnessCfg(t, nil, nil)

	resp := h.do(http.MethodGet, "/binflow/api/repositories/t607-get-miss", adminUser, adminPass, nil, nil)
	status, raw := cptDrain(t, resp)
	resp.Body.Close() //nolint:errcheck // drained above
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d (%s), want 400", status, raw)
	}
	var env cptErrBody
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope (%s): %v", raw, err)
	}
	if len(env.Errors) != 1 || env.Errors[0].Status != 400 || env.Errors[0].Message != "Bad Request" {
		t.Fatalf("envelope = %+v, want the bare Bad Request", env)
	}
}

// TestRepoDeleteMissingKeyStatusMsgReport: the delete face's missing-key
// answer is the report body, not the errors envelope.
func TestRepoDeleteMissingKeyStatusMsgReport(t *testing.T) {
	h := newHarnessCfg(t, nil, nil)

	resp := h.do(http.MethodDelete, "/binflow/api/repositories/t607-del-miss", adminUser, adminPass, nil, nil)
	status, raw := cptDrain(t, resp)
	resp.Body.Close() //nolint:errcheck // drained above
	if status != http.StatusNotFound {
		t.Fatalf("status = %d (%s), want 404", status, raw)
	}
	var report struct {
		RepoKey               string `json:"repoKey"`
		StatusMsg             string `json:"statusMsg"`
		DeletedArtifactsCount int    `json:"deletedArtifactsCount"`
		Success               bool   `json:"success"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decode report (%s): %v", raw, err)
	}
	wantMsg := "Cannot delete repository: 't607-del-miss', repository config does not exist"
	if report.RepoKey != "t607-del-miss" || report.StatusMsg != wantMsg ||
		report.DeletedArtifactsCount != 0 || !report.Success {
		t.Fatalf("report = %+v, want the refusal report shape with %q", report, wantMsg)
	}
	if strings.Contains(string(raw), "errors") {
		t.Fatalf("body (%s) must not carry the errors envelope", raw)
	}

	// The success path keeps its own report (regression guard).
	status, body := ctWrite(h, http.MethodPut, "t607-del-ok",
		`{"rclass":"local","packageType":"generic"}`, "application/json")
	if status != http.StatusOK {
		t.Fatalf("seed create = %d (%s), want 200", status, body)
	}
	resp = h.do(http.MethodDelete, "/binflow/api/repositories/t607-del-ok", adminUser, adminPass, nil, nil)
	status, raw = cptDrain(t, resp)
	resp.Body.Close() //nolint:errcheck // drained above
	if status != http.StatusOK || !strings.Contains(string(raw), "have been removed successfully") {
		t.Fatalf("existing delete = %d (%s), want the 200 removal report", status, raw)
	}
}
