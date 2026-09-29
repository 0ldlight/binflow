package generic_test

// T-564 / BIN-46: the deploy 201's Location header and envelope
// uri/downloadUri carry the /binflow context prefix (A-face shape on
// Artifactory 7.161.26 behind its /artifactory context root: all three
// render through the context path, Location byte-equal to the uri), and
// the rendered value resolves back to the deployed bytes through the
// /binflow routing — the bare-root form 404s when followed. Same probe
// matrix as the A-side curl evidence: fresh byte deploy, checksum deploy,
// overwrite deploy. Companion fix to maven's T-561/T-563 render rule.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// assertPrefixed201 checks the three self-referential forms of a 201 PUT
// response against scheme://host/binflow/<repo>/<path> and returns the
// Location for the resolvability anchor.
func assertPrefixed201(t *testing.T, e *env, resp *http.Response, path string) string {
	t.Helper()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT status = %d, body=%s", resp.StatusCode, body(t, resp))
	}
	want := e.srv.URL + "/binflow" + path
	loc := resp.Header.Get("Location")
	if loc != want {
		t.Errorf("Location = %q, want %q", loc, want)
	}
	var fi fileInfoJSON
	if err := json.Unmarshal([]byte(body(t, resp)), &fi); err != nil {
		t.Fatalf("envelope decode: %v", err)
	}
	if fi.URI != want {
		t.Errorf("uri = %q, want %q", fi.URI, want)
	}
	if fi.DownloadURI != want {
		t.Errorf("downloadUri = %q, want %q", fi.DownloadURI, want)
	}
	if loc != fi.URI {
		t.Errorf("Location = %q, envelope uri = %q, want byte-equal", loc, fi.URI)
	}
	return loc
}

// getBack fetches loc through the harness's /binflow routing and asserts
// 200 with the deployed bytes.
func getBack(t *testing.T, e *env, loc, wantBody string) {
	t.Helper()
	resp := e.do(t, http.MethodGet, strings.TrimPrefix(loc, e.srv.URL), nil, nil)
	got := body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Location = %d, body=%s", resp.StatusCode, got)
	}
	if !bytes.Equal([]byte(got), []byte(wantBody)) {
		t.Errorf("GET body = %q, want the deployed %q", got, wantBody)
	}
}

// TestDeployCreatedEnvelopeURIContextPrefix: byte deploy and overwrite
// deploy (A answers 201 on both, same prefixed forms) render Location,
// uri and downloadUri through /binflow, Location byte-equal to the uri,
// and the value GETs back the deployed bytes.
func TestDeployCreatedEnvelopeURIContextPrefix(t *testing.T) {
	e := newEnv(t)
	for _, tc := range []struct {
		name    string
		path    string
		first   string
		second  string // second non-empty exercises the overwrite leg
		literal string // bytes the GET must return after the last PUT
	}{
		{name: "fresh deploy", path: "/generic-local/t564/probe.bin", first: "t564-probe-bytes"},
		{name: "overwrite deploy", path: "/generic-local/t564/probe.bin", first: "t564-probe-bytes", second: "t564-overwrite-bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := e.do(t, http.MethodPut, "/binflow"+tc.path, strings.NewReader(tc.first), nil)
			loc := assertPrefixed201(t, e, resp, tc.path)
			want := tc.first
			if tc.second != "" {
				resp2 := e.do(t, http.MethodPut, "/binflow"+tc.path, strings.NewReader(tc.second), nil)
				loc = assertPrefixed201(t, e, resp2, tc.path)
				want = tc.second
			}
			getBack(t, e, loc, want)
		})
	}
}

// TestChecksumDeployCreatedEnvelopeURIContextPrefix: the zero-transfer
// X-Checksum-Deploy chain (writeCreated's second caller) renders the same
// prefixed forms, resolving to the referenced blob's bytes.
func TestChecksumDeployCreatedEnvelopeURIContextPrefix(t *testing.T) {
	e := newEnv(t)
	seed := "t564-checksum-seed"
	sha, _, _ := digestsOf(seed)
	resp := e.do(t, http.MethodPut, "/generic-local/t564/seed.bin", strings.NewReader(seed), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed deploy: %d (%s)", resp.StatusCode, body(t, resp))
	}

	path := "/generic-local/t564/zero-transfer.bin"
	resp = e.do(t, http.MethodPut, "/binflow"+path, nil, map[string]string{
		"X-Checksum-Deploy": "true",
		"X-Checksum-Sha256": sha,
	})
	loc := assertPrefixed201(t, e, resp, path)
	getBack(t, e, loc, seed)
}
