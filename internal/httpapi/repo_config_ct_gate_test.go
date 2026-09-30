package httpapi_test

// T-607 / BIN-89 (rest/repo-config-put-ct-strictness): the repo config
// write plane's Content-Type family, live-pinned on A 7.161.26 (probe
// /tmp/t607, legs put_noct/put_textplain/put_xml/put_vndapi/put_charset/
// put_charset_remote/post_noct_existing):
//
//   - no Content-Type, or any media type other than application/json
//     (text/plain, application/xml, application/vnd.api+json) answers the
//     bare 415 "Unsupported Media Type" on BOTH the create (PUT) and
//     update (POST) faces, ahead of every body/key question;
//   - a parameterized application/json (charset=UTF-8 — the default of
//     Spring RestTemplate and other Java clients) is NOT the 415: on PUT it
//     takes the type/media refusal (the distribution writer's family, the
//     body's rclass and the raw CT string interpolated), on POST it takes
//     the vendor-negotiation 404s (the notfound-family test file).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// ctWrite is one repo-config write request under a specific Content-Type.
func ctWrite(h *harness, method, key, body, contentType string) (int, []byte) {
	hdr := map[string]string(nil)
	if contentType != "" {
		hdr = map[string]string{"Content-Type": contentType}
	}
	resp := h.do(method, "/binflow/api/repositories/"+key, adminUser, adminPass, []byte(body), hdr)
	defer resp.Body.Close() //nolint:errcheck // drained below
	return cptDrain(h.t, resp)
}

// TestRepoConfigCTGate415: the exact-CT white list — every other spelling
// (absent header included) is the bare 415 on both write faces.
func TestRepoConfigCTGate415(t *testing.T) {
	h := newHarnessCfg(t, nil, nil)

	for _, tt := range []struct {
		name string
		ct   string
	}{
		{"absent header", ""},
		{"text plain", "text/plain"},
		{"application xml", "application/xml"},
		{"vendor api json subtype", "application/vnd.api+json"},
	} {
		for _, method := range []string{http.MethodPut, http.MethodPost} {
			t.Run(fmt.Sprintf("%s/%s", method, tt.name), func(t *testing.T) {
				hh := newHarnessCfg(t, nil, nil)
				status, body := ctWrite(hh, method, "t607-ct-key", `{"rclass":"local","packageType":"generic"}`, tt.ct)
				if status != http.StatusUnsupportedMediaType {
					t.Fatalf("status = %d (%s), want 415", status, body)
				}
				var env cptErrBody
				if err := json.Unmarshal(body, &env); err != nil {
					t.Fatalf("decode envelope (%s): %v", body, err)
				}
				if len(env.Errors) != 1 || env.Errors[0].Status != 415 ||
					env.Errors[0].Message != "Unsupported Media Type" {
					t.Fatalf("envelope = %+v, want the bare 415 literal", env)
				}
			})
		}
	}

	// The 415 rides AHEAD of the key-exists question: an existing key under
	// a bad Content-Type is still the 415, not the create-conflict 400.
	status, body := ctWrite(h, http.MethodPut, "t607-ct-exists", `{"rclass":"local","packageType":"generic"}`, "application/json")
	if status != http.StatusOK {
		t.Fatalf("seed create = %d (%s), want 200", status, body)
	}
	status, body = ctWrite(h, http.MethodPut, "t607-ct-exists", `{"rclass":"local","packageType":"generic"}`, "text/plain")
	if status != http.StatusUnsupportedMediaType {
		t.Fatalf("existing-key bad CT = %d (%s), want 415 (ahead of key-exists)", status, body)
	}
}

// TestRepoConfigPUTCharsetRefusal: the parameterized-CT arm on the create
// face — the distribution writer's message family with the body's rclass
// and the request's raw Content-Type interpolated (NOT the 415).
func TestRepoConfigPUTCharsetRefusal(t *testing.T) {
	h := newHarnessCfg(t, nil, nil)

	for _, tt := range []struct {
		name     string
		body     string
		rclass   string
		mediaStr string
	}{
		{
			"local rclass",
			`{"rclass":"local","packageType":"generic"}`,
			"local", "application/json;charset=UTF-8",
		},
		{
			"remote rclass",
			`{"rclass":"remote","packageType":"generic","url":"http://example.com/up"}`,
			"remote", "application/json;charset=UTF-8",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			key := "t607-cs-" + tt.rclass
			status, body := ctWrite(h, http.MethodPut, key, tt.body, tt.mediaStr)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d (%s), want 400", status, body)
			}
			var env cptErrBody
			if err := json.Unmarshal(body, &env); err != nil {
				t.Fatalf("decode envelope (%s): %v", body, err)
			}
			want := fmt.Sprintf("Unsupported repository type '%s' or media type '%s'", tt.rclass, tt.mediaStr)
			if len(env.Errors) != 1 || env.Errors[0].Status != 400 || env.Errors[0].Message != want {
				t.Fatalf("envelope = %+v, want %q", env, want)
			}
		})
	}

	// Regression guards on the exact-CT arms the refusal family borders:
	// the distribution verbatim keeps its frozen interpolation (the raw CT
	// IS application/json there), and the plain create keeps succeeding.
	status, body := ctWrite(h, http.MethodPut, "t607-cs-dist",
		`{"rclass":"distribution","packageType":"generic"}`, "application/json")
	if status != http.StatusBadRequest ||
		!strings.Contains(string(body), "Unsupported repository type 'distribution' or media type 'application/json'") {
		t.Fatalf("distribution exact-CT = %d (%s), want the p63 verbatim 400", status, body)
	}
	// The unexplored corner, pinned to the A16 order: a rclass-LESS body
	// under a parameterized CT keeps the TYPE refusal (the create path's
	// own wording), not the charset arm and not the 415.
	status, body = ctWrite(h, http.MethodPut, "t607-cs-notype", `{"packageType":"generic"}`,
		"application/json;charset=UTF-8")
	if status != http.StatusBadRequest || !strings.Contains(string(body), "must be one of local, remote, virtual") {
		t.Fatalf("rclass-less charset CT = %d (%s), want the type refusal (A16 order)", status, body)
	}
	status, body = ctWrite(h, http.MethodPut, "t607-cs-ok",
		`{"rclass":"local","packageType":"generic"}`, "application/json")
	if status != http.StatusOK {
		t.Fatalf("exact-CT create = %d (%s), want 200", status, body)
	}
	status, body = ctWrite(h, http.MethodPut, "t607-cs-ok",
		`{"rclass":"local","packageType":"generic"}`, "application/json")
	if status != http.StatusBadRequest || !strings.Contains(string(body), "Repository key already exists") {
		t.Fatalf("exact-CT existing = %d (%s), want the create-conflict 400", status, body)
	}
	// The charset arm PRECEDES the key-exists question: an existing key
	// under a parameterized CT answers the type/media refusal, not the
	// create-conflict (live A, probe /tmp/t607 put_charset_existing).
	status, body = ctWrite(h, http.MethodPut, "t607-cs-ok",
		`{"rclass":"local","packageType":"generic"}`, "application/json;charset=UTF-8")
	if status != http.StatusBadRequest ||
		!strings.Contains(string(body), "Unsupported repository type 'local' or media type 'application/json;charset=UTF-8'") {
		t.Fatalf("charset-CT existing = %d (%s), want the type/media refusal", status, body)
	}
}
