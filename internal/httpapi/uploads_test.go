package httpapi_test

// T-332 tests: the /api/v1/uploads MPU REST plane on the flipped
// (Artifactory-shaped) wire — six endpoints, POST + QueryParam, the session
// token as the per-session credential, complete?sha1= 202 + the async task
// model, GET /config the capability probe (ADR-0039).
//
//   - the STANDARD harness (disk engine, no seam wired) is the filestore
//     instance: the five data endpoints answer the honest plain-text 501
//     (FR-90-AC3) while config — the probe — answers 200 supported:false.
//   - newUploadsHarness wires Deps.Uploads to fakeMPUContextSeam (the
//     T-323R context seam, which the token lane requires): rows persist
//     across harnesses and each Commit relays into the CURRENT stack's real
//     engine, so the assembled blob is physically where the checksum-deploy
//     PUT (repo.Service.PutFromBlob) looks for it, exactly as in production
//     where the seam and the service share one engine instance.

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// mpuCreate opens a session through the real HTTP plane (POST + QueryParam,
// the flipped create) and returns the decoded body.
func mpuCreate(t *testing.T, h *harness, user, pass, repoKey, path string, partSizeMB int64) (int, map[string]any) {
	t.Helper()
	q := "?repoKey=" + repoKey + "&repoPath=" + path
	if partSizeMB != 0 {
		q += fmt.Sprintf("&partSizeMB=%d", partSizeMB)
	}
	resp := h.do(http.MethodPost, "/binflow/api/v1/uploads/create"+q, user, pass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("create body not JSON (%d): %s", resp.StatusCode, raw)
	}
	return resp.StatusCode, out
}

// mpuCreateToken opens a session and returns its capability token.
func mpuCreateToken(t *testing.T, h *harness, user, pass, repoKey, path string, partSizeMB int64) string {
	t.Helper()
	code, body := mpuCreate(t, h, user, pass, repoKey, path, partSizeMB)
	if code != http.StatusOK {
		t.Fatalf("create = %d: %v", code, body)
	}
	tok, _ := body["token"].(string)
	if tok == "" {
		t.Fatalf("create body carries no token: %v", body)
	}
	return tok
}

// bearer is the header set every token-authenticated verb rides.
func bearer(tok string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + tok}
}

// mpuTokenSessionID extracts the public half of a token (test convenience).
func mpuTokenSessionID(t *testing.T, tok string) string {
	t.Helper()
	_, sid, found := strings.Cut(tok, ".")
	if !found || sid == "" {
		t.Fatalf("token %q carries no session id", tok)
	}
	return sid
}

// mpuPostToken issues one POST on the token lane and decodes the JSON body.
func mpuPostToken(t *testing.T, h *harness, verb, tok, query string) (int, map[string]any) {
	t.Helper()
	resp := h.do(http.MethodPost, "/binflow/api/v1/uploads/"+verb+query, "", "", nil, bearer(tok))
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("%s body not JSON (%d): %s", verb, resp.StatusCode, raw)
		}
	}
	return resp.StatusCode, out
}

// mpuStatus polls status until the task reaches one of want (fail: msg).
func mpuStatusPoll(t *testing.T, h *harness, tok string, want ...string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		code, body := mpuPostToken(t, h, "status", tok, "")
		if code != http.StatusOK {
			t.Fatalf("status = %d: %v", code, body)
		}
		for _, w := range want {
			if body["status"] == w {
				return body
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("status never reached %v (last: %v)", want, body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// mpuPutPart uploads one part through the token lane.
func mpuPutPart(t *testing.T, h *harness, tok string, part int, body []byte) (int, map[string]any) {
	t.Helper()
	sid := mpuTokenSessionID(t, tok)
	resp := h.do(http.MethodPut,
		fmt.Sprintf("/binflow/api/v1/uploads/part/%s/%d", sid, part), "", "", body, bearer(tok))
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out
}

// newUploadsHarness builds the context-seam-wired stack (the S3 shape).
func newUploadsHarness(t *testing.T, users ...[2]string) (*harness, *fakeMPUContextSeam) {
	t.Helper()
	seam := newFakeMPUContextSeam()
	wire := func(d *httpapi.Deps) {
		eng, ok := d.GC.(storage.Engine)
		if !ok {
			t.Fatal("stack GC seam is not a storage.Engine")
		}
		seam.setEngine(eng)
		d.Uploads = seam
	}
	h := newHarnessFull(t, nil, nil, nil, wire, users)
	seedRepo(t, h, "generic-local")
	return h, seam
}

// mpuPayload builds the standard 3-part corpus (5 MiB + 5 MiB + 1 MiB).
func mpuPayload() (p1, p2, p3, whole []byte) {
	p1 = bytes.Repeat([]byte{0x11}, 5<<20)
	p2 = bytes.Repeat([]byte{0x22}, 5<<20)
	p3 = bytes.Repeat([]byte{0x33}, 1<<20)
	whole = append(append([]byte{}, p1...), append(p2, p3...)...)
	return
}

// ---------------------------------------------------------------------------
// filestore honesty (FR-90-AC3 + the config probe's deliberate exception)
// ---------------------------------------------------------------------------

// TestUploadsFilestoreHonesty pins the backend matrix on a filestore
// instance: the five data endpoints answer 501 text/plain "not supported on
// this backend" — never a 404 masquerading as an absent route — while GET
// /config answers 200 {"supported": false}: a probe that could not say "no"
// would be no probe (ADR-0039). Authentication still comes first.
func TestUploadsFilestoreHonesty(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	legs := []struct {
		name   string
		method string
		path   string
		query  string
	}{
		{"create", http.MethodPost, "/binflow/api/v1/uploads/create", "?repoKey=generic-local&repoPath=a.bin"},
		{"urlPart", http.MethodPost, "/binflow/api/v1/uploads/urlPart", "?partNumber=1"},
		{"status", http.MethodPost, "/binflow/api/v1/uploads/status", ""},
		{"complete", http.MethodPost, "/binflow/api/v1/uploads/complete", "?sha1=" + strings.Repeat("0", 40)},
		{"abort", http.MethodPost, "/binflow/api/v1/uploads/abort", ""},
		{"part", http.MethodPut, "/binflow/api/v1/uploads/part/some-id/1", ""},
	}
	for _, leg := range legs {
		t.Run(leg.name, func(t *testing.T) {
			resp := h.do(leg.method, leg.path+leg.query, adminUser, adminPass, []byte("x"), nil)
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusNotImplemented {
				t.Fatalf("status = %d, want 501 (body: %s)", resp.StatusCode, body)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
				t.Fatalf("Content-Type = %q, want text/plain", ct)
			}
			if !strings.Contains(string(body), "not supported on this backend") || !strings.Contains(string(body), "S3") {
				t.Fatalf("body lacks the honest refusal: %s", body)
			}
		})
	}

	// The probe: 200 + supported:false.
	resp := h.do(http.MethodGet, "/binflow/api/v1/uploads/config", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	var cfg map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&cfg)
	if resp.StatusCode != http.StatusOK || cfg["supported"] != false {
		t.Fatalf("filestore config = %d %v, want 200 supported:false", resp.StatusCode, cfg)
	}

	// Anonymous meets the route's 401 first — the plane exists, it is the
	// backend that lacks the capability.
	resp = h.do(http.MethodPost, "/binflow/api/v1/uploads/create?repoKey=generic-local&repoPath=a.bin", "", "", nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous create = %d, want 401 (auth precedes the capability answer)", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// config: the capability probe and the jfrog-cli version gate
// ---------------------------------------------------------------------------

// TestUploadsConfigVersionGate walks the reverse-engineered version
// comparison's observable arms: below the floor -> false; at/above -> the
// backend's verdict; a SHORTER version than the floor is below it (2.62 <
// 2.62.2); a non-numeric segment aborts the comparison as NOT old (the
// catch arm); a non-jfrog UA skips the gate entirely.
func TestUploadsConfigVersionGate(t *testing.T) {
	h, _ := newUploadsHarness(t)
	probe := func(ua string) any {
		t.Helper()
		resp := h.do(http.MethodGet, "/binflow/api/v1/uploads/config", adminUser, adminPass, nil,
			map[string]string{"User-Agent": ua})
		defer func() { _ = resp.Body.Close() }()
		var cfg map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&cfg)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("config (%s) = %d, want 200", ua, resp.StatusCode)
		}
		return cfg["supported"]
	}
	for ua, want := range map[string]bool{
		"curl/8.0":             true,  // no gate for foreign agents
		"jfrog-cli-go/2.62.2":  true,  // exactly the floor
		"jfrog-cli-go/2.63.0":  true,  // above
		"jfrog-cli-go/2.63":    true,  // above at the second segment
		"jfrog-cli-go/2.62":    false, // shorter than the floor is below it
		"jfrog-cli-go/2.50.1":  false, // below
		"jfrog-cli-go/1.99.99": false, // far below
		"jfrog-cli-go/dev":     true,  // non-numeric segment: the catch arm
		"jfrog-cli-go/x.y.z":   true,  // ditto
	} {
		if got := probe(ua); got != want {
			t.Errorf("config supported for UA %q = %v, want %v", ua, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// the full chain on the flipped wire
// ---------------------------------------------------------------------------

// TestUploadsFullChainNewWire is the jfrog-cli flow end to end: create
// (query params -> token) -> urlPart (POST + partNumber query param) ->
// 3 part PUTs on the token -> complete?sha1= 202 -> status poll to
// Finished(100) carrying the checksum-deploy token -> the CLIENT's
// zero-transfer X-Checksum-Deploy PUT lands the node -> byte-identical GET.
func TestUploadsFullChainNewWire(t *testing.T) {
	h, seam := newUploadsHarness(t)

	tok := mpuCreateToken(t, h, adminUser, adminPass, "generic-local", "big/pkg.bin", 5)
	sid := mpuTokenSessionID(t, tok)
	if seam.begins.Load() != 1 {
		t.Fatalf("seam began %d sessions, want 1", seam.begins.Load())
	}

	// urlPart: POST + the partNumber query param, the token the credential.
	code, body := mpuPostToken(t, h, "urlPart", tok, "?partNumber=2")
	if code != http.StatusOK {
		t.Fatalf("urlPart = %d: %v", code, body)
	}
	url, _ := body["url"].(string)
	if !strings.Contains(url, "/api/v1/uploads/part/"+sid+"/2") {
		t.Fatalf("urlPart url = %q", url)
	}
	if _, has := body["offsetBytes"]; has {
		t.Fatalf("urlPart body carries the retired offset echo: %v", body)
	}

	// Status mid-upload: the task model, Uploading.
	code, body = mpuPostToken(t, h, "status", tok, "")
	if code != http.StatusOK || body["status"] != "PARTS" || body["progress"] != float64(0) {
		t.Fatalf("mid-upload status = %d %v", code, body)
	}

	// Three parts on the token lane: 5 MiB + 5 MiB + 1 MiB (short final).
	p1, p2, p3, whole := mpuPayload()
	for i, part := range [][]byte{p1, p2, p3} {
		code, echo := mpuPutPart(t, h, tok, i+1, part)
		if code != http.StatusOK {
			t.Fatalf("part %d PUT = %d: %v", i+1, code, echo)
		}
		want := int64(len(part)) * int64(i+1)
		if i == 2 {
			want = int64(len(whole))
		}
		if int64(echo["receivedBytes"].(float64)) != want {
			t.Fatalf("part %d echo receivedBytes = %v, want %d", i+1, echo["receivedBytes"], want)
		}
	}
	// A fourth part after the short final: refused.
	if code, _ := mpuPutPart(t, h, tok, 4, []byte("x")); code != http.StatusConflict {
		t.Fatalf("part after the short final = %d, want 409", code)
	}

	// complete: malformed sha1 -> 400; the right one -> 202 (async).
	if code, _ := mpuPostToken(t, h, "complete", tok, "?sha1=not-a-sha1"); code != http.StatusBadRequest {
		t.Fatalf("malformed sha1 complete = %d, want 400", code)
	}
	wholeSHA1 := sha1Hex(t, whole)
	code, _ = mpuPostToken(t, h, "complete", tok, "?sha1="+wholeSHA1)
	if code != http.StatusAccepted {
		t.Fatalf("complete = %d, want 202", code)
	}

	// Poll to Finished: progress 100 and the checksum-deploy token.
	done := mpuStatusPoll(t, h, tok, "FINISHED")
	if done["progress"] != float64(100) {
		t.Fatalf("Finished progress = %v, want 100", done["progress"])
	}
	depTok, _ := done["checksumToken"].(string)
	if depTok == "" {
		t.Fatalf("Finished status carries no checksum-deploy token: %v", done)
	}

	// The node does NOT exist yet — the client lands it (Artifactory's
	// flow: the checksumToken exists precisely for this PUT).
	resp := h.do(http.MethodGet, "/binflow/generic-local/big/pkg.bin", adminUser, adminPass, nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("artifact before checksum-deploy = %d, want 404 (the node is the client's landing)", resp.StatusCode)
	}

	// The zero-transfer landing: X-Checksum-Deploy with the 5-minute token.
	resp = h.do(http.MethodPut, "/binflow/generic-local/big/pkg.bin", "", "", nil,
		map[string]string{
			"Authorization":     "Bearer " + depTok,
			"X-Checksum-Deploy": "true",
			"X-Checksum-Sha1":   wholeSHA1,
		})
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("checksum-deploy PUT = %d, want 201", resp.StatusCode)
	}

	// Byte-for-byte readback through the content plane.
	resp = h.do(http.MethodGet, "/binflow/generic-local/big/pkg.bin", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, whole) {
		t.Fatalf("artifact GET = %d (%d bytes), want the 11MiB corpus", resp.StatusCode, len(got))
	}

	// Terminal postures: the upload is consumed (urlPart/complete 404) but
	// the task record stays observable (status still Finished).
	if code, _ := mpuPostToken(t, h, "urlPart", tok, "?partNumber=9"); code != http.StatusNotFound {
		t.Fatalf("urlPart after Finished = %d, want 404", code)
	}
	if code, _ := mpuPostToken(t, h, "complete", tok, "?sha1="+wholeSHA1); code != http.StatusNotFound {
		t.Fatalf("complete after Finished = %d, want 404", code)
	}
	code, body = mpuPostToken(t, h, "status", tok, "")
	if code != http.StatusOK || body["status"] != "FINISHED" {
		t.Fatalf("status after Finished = %d %v, want the observable task record", code, body)
	}
	// Abort cannot discard an assembled artifact.
	if code, _ := mpuPostToken(t, h, "abort", tok, ""); code != http.StatusConflict {
		t.Fatalf("abort after Finished = %d, want 409", code)
	}
}

// TestUploadsWrongSha1FailsTheTask: a well-formed but WRONG sha1 accepts
// (202 — the gate is asynchronous now) and fails the task; the error is
// observable through status, nothing lands, and the session is consumed.
func TestUploadsWrongSha1FailsTheTask(t *testing.T) {
	h, _ := newUploadsHarness(t)

	tok := mpuCreateToken(t, h, adminUser, adminPass, "generic-local", "big/bad.bin", 5)
	_, _, p3, _ := mpuPayload()
	if code, _ := mpuPutPart(t, h, tok, 1, p3); code != http.StatusOK {
		t.Fatalf("short final part = %d", code)
	}

	if code, _ := mpuPostToken(t, h, "complete", tok, "?sha1="+strings.Repeat("0", 40)); code != http.StatusAccepted {
		t.Fatalf("wrong-sha1 complete = %d, want 202 (the async gate)", code)
	}
	failed := mpuStatusPoll(t, h, tok, "NON_RETRYABLE_ERROR")
	msg, _ := failed["error"].(string)
	if !strings.Contains(msg, "checksum") {
		t.Fatalf("Failed error = %q, want the checksum-mismatch wording", msg)
	}

	// Nothing landed; the session is consumed; abort reclaims the record.
	resp := h.do(http.MethodGet, "/binflow/generic-local/big/bad.bin", adminUser, adminPass, nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("mismatched artifact GET = %d, want 404", resp.StatusCode)
	}
	if code, _ := mpuPostToken(t, h, "complete", tok, "?sha1="+strings.Repeat("1", 40)); code != http.StatusNotFound {
		t.Fatalf("complete after Failed = %d, want 404 (session consumed)", code)
	}
	if code, _ := mpuPostToken(t, h, "abort", tok, ""); code != http.StatusNoContent {
		t.Fatalf("abort after Failed = %d, want 204 (record cleanup)", code)
	}
	if code, _ := mpuPostToken(t, h, "status", tok, ""); code != http.StatusNotFound {
		t.Fatalf("status after abort = %d, want 404", code)
	}
}

// ---------------------------------------------------------------------------
// the capability-token verdict ladder
// ---------------------------------------------------------------------------

// TestUploadsTokenVerdicts walks the credential arms of the token lane:
// anonymous 401, valid shared credentials 403 (the scope refusal — only the
// session token drives these verbs), a garbage Bearer the middleware
// rejected 401 (its own verdict, rendered by the handler), a well-shaped
// token with the wrong secret 404, and one session's token on another
// session's part URL 404.
func TestUploadsTokenVerdicts(t *testing.T) {
	h, _ := newUploadsHarness(t)
	tokA := mpuCreateToken(t, h, adminUser, adminPass, "generic-local", "big/a.bin", 5)
	tokB := mpuCreateToken(t, h, adminUser, adminPass, "generic-local", "big/b.bin", 5)
	sidA := mpuTokenSessionID(t, tokA)
	sidB := mpuTokenSessionID(t, tokB)

	// Anonymous: the 401 challenge.
	resp := h.do(http.MethodPost, "/binflow/api/v1/uploads/urlPart?partNumber=1", "", "", nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous urlPart = %d, want 401", resp.StatusCode)
	}
	// Valid shared credentials: 403, the scope refusal.
	resp = h.do(http.MethodPost, "/binflow/api/v1/uploads/urlPart?partNumber=1", adminUser, adminPass, nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Basic-auth urlPart = %d, want 403 (the session token is the only credential)", resp.StatusCode)
	}
	// A rejected garbage Bearer: the middleware's own verdict.
	resp = h.do(http.MethodPost, "/binflow/api/v1/uploads/status", "", "", nil,
		bearer("garbage-not-a-token"))
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("garbage Bearer status = %d, want 401", resp.StatusCode)
	}
	// Well-shaped, wrong secret: unknown capability, 404.
	forged := strings.Repeat("ab", 32) + "." + sidA
	if code, _ := mpuPostToken(t, h, "status", forged, ""); code != http.StatusNotFound {
		t.Fatalf("forged secret status = %d, want 404", code)
	}
	// Session A's token on session B's part URL: the URL and the
	// credential disagree.
	resp = h.do(http.MethodPut, "/binflow/api/v1/uploads/part/"+sidB+"/1", "", "", []byte("x"), bearer(tokA))
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-session part PUT = %d, want 404", resp.StatusCode)
	}
	// A regular API token (valid to the middleware) is not the capability.
	resp = h.do(http.MethodPost, "/binflow/api/v1/uploads/status", "", "", nil,
		bearer("2f9b7d1c0e8a4536b7c1d09e4f2a7b31c5d8e0f64a2b71c9d0e3f58a1b6c4d72"))
	func() { _ = resp.Body.Close() }()
	// (unknown token -> rejected by the middleware -> handler: not
	// MPU-shaped -> 401; an authenticated non-MPU Bearer would be the 403
	// above, which the Basic leg already pins)
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("regular-Bearer status = %d, want 401/403", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// create's guards on the flipped wire
// ---------------------------------------------------------------------------

// TestUploadsCreateGuards: query-param presence (Artifactory's exact 400),
// the one-403 refusal family (unknown repo / no `w` / remote), part size
// bounds — and the FLIP itself: a protocol (docker) repository and a
// virtual repository now create sessions instead of the retired 400.
func TestUploadsCreateGuards(t *testing.T) {
	h, _ := newUploadsHarness(t, [2]string{"alice", "alice-pw"})
	seedRepo(t, h, "repo-a")

	// Param presence: the Artifactory wording verbatim.
	resp := h.do(http.MethodPost, "/binflow/api/v1/uploads/create?repoKey=repo-a", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "Query param repoKey or repoPath is null") {
		t.Fatalf("repoPath-less create = %d %s, want the Artifactory 400 wording", resp.StatusCode, body)
	}

	// The one-403 family: unknown repo and no `w` share Artifactory's
	// ForbiddenException wording.
	if code, _ := mpuCreate(t, h, "alice", "alice-pw", "no-such-repo", "a.bin", 0); code != http.StatusForbidden {
		t.Fatalf("unknown repo create = %d, want 403", code)
	}
	resp = h.do(http.MethodPost, "/binflow/api/v1/uploads/create?repoKey=repo-a&repoPath=x.bin", "alice", "alice-pw", nil, nil)
	defer func() { _ = resp.Body.Close() }()
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "The user is not allowed to deploy to this location") {
		t.Fatalf("no-w create = %d %s, want the Artifactory 403 wording", resp.StatusCode, body)
	}

	// The grant opens the same caller.
	grant(t, h, "mpu-grant", "repo-a", "**", "alice", false, true, false)
	if code, _ := mpuCreate(t, h, "alice", "alice-pw", "repo-a", "x.bin", 5); code != http.StatusOK {
		t.Fatalf("create with w = %d, want 200", code)
	}

	if err := createTypedRepo(t, h, "docker-local", "docker"); err == nil {
		// THE FLIP: a protocol repository now accepts MPU create (the
		// package-type gate retired with the wire, T-304 section 1.2-B).
		if code, _ := mpuCreate(t, h, adminUser, adminPass, "docker-local", "raw/big.bin", 5); code != http.StatusOK {
			t.Fatalf("create on docker repo = %d, want 200 (the type gate is retired)", code)
		}
	}

	// A remote repository is a cache, not a deploy target: the same 403.
	resp = h.do(http.MethodPut, "/binflow/api/repositories/gen-remote", adminUser, adminPass,
		[]byte(`{"rclass":"remote","packageType":"generic","url":"https://upstream.invalid"}`), nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		if code, _ := mpuCreate(t, h, adminUser, adminPass, "gen-remote", "a.bin", 0); code != http.StatusForbidden {
			t.Fatalf("remote repo create = %d, want 403", code)
		}
	}

	// Path validation family (BinFlow's own defense, kept).
	for path, want := range map[string]int{
		"":                                http.StatusBadRequest,
		"/abs/path.bin":                   http.StatusBadRequest,
		"dir/":                            http.StatusBadRequest,
		"../escape.bin":                   http.StatusBadRequest,
		"file.bin;k=v":                    http.StatusBadRequest,
		strings.Repeat("a", 513) + ".bin": http.StatusBadRequest,
	} {
		if code, _ := mpuCreate(t, h, adminUser, adminPass, "repo-a", path, 0); code != want {
			t.Fatalf("create path %q = %d, want %d", path, code, want)
		}
	}

	// partSizeMB bounds (B3 kept: the megabyte-domain overflow gate).
	for _, mb := range []string{"junk", "-1", "8796093022209", "5121"} {
		resp := h.do(http.MethodPost,
			"/binflow/api/v1/uploads/create?repoKey=repo-a&repoPath=big/y.bin&partSizeMB="+mb,
			adminUser, adminPass, nil, nil)
		func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("partSizeMB=%s = %d, want 400", mb, resp.StatusCode)
		}
	}
	if code, _ := mpuCreate(t, h, adminUser, adminPass, "repo-a", "big/z.bin", 5120); code != http.StatusOK {
		t.Fatalf("partSizeMB=5120 = %d, want 200 (the boundary is legal)", code)
	}
}

// TestUploadsVirtualDefault: create on a virtual repository resolves to the
// defaultDeploymentRepo — the write door and the session address the MEMBER,
// and the client's checksum-deploy through the VIRTUAL lands there
// (routeVirtualWrite), the getRepoPath behavior (T-304 section 1.2-B).
func TestUploadsVirtualDefault(t *testing.T) {
	h, _ := newUploadsHarness(t)

	// members: gen-local (default deployment target) + gen-other.
	resp := h.do(http.MethodPut, "/binflow/api/repositories/gen-local", adminUser, adminPass,
		[]byte(`{"rclass":"local","packageType":"generic"}`), nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create gen-local = %d", resp.StatusCode)
	}
	resp = h.do(http.MethodPut, "/binflow/api/repositories/gen-other", adminUser, adminPass,
		[]byte(`{"rclass":"local","packageType":"generic"}`), nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create gen-other = %d", resp.StatusCode)
	}
	resp = h.do(http.MethodPut, "/binflow/api/repositories/gen-virtual", adminUser, adminPass,
		[]byte(`{"rclass":"virtual","packageType":"generic","repositories":["gen-local","gen-other"],"defaultDeploymentRepo":"gen-local"}`), nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create gen-virtual = %d: %s", resp.StatusCode, body)
	}

	tok := mpuCreateToken(t, h, adminUser, adminPass, "gen-virtual", "via-virtual.bin", 5)
	_, _, p3, _ := mpuPayload()
	if code, _ := mpuPutPart(t, h, tok, 1, p3); code != http.StatusOK {
		t.Fatalf("short part = %d", code)
	}
	wholeSHA1 := sha1Hex(t, p3)
	if code, _ := mpuPostToken(t, h, "complete", tok, "?sha1="+wholeSHA1); code != http.StatusAccepted {
		t.Fatalf("complete = %d", code)
	}
	done := mpuStatusPoll(t, h, tok, "FINISHED")
	depTok := done["checksumToken"].(string)

	// The client checksum-deploys through the VIRTUAL (it only knows the
	// repo it addressed): routeVirtualWrite lands the node on gen-local.
	resp = h.do(http.MethodPut, "/binflow/gen-virtual/via-virtual.bin", "", "", nil,
		map[string]string{
			"Authorization":     "Bearer " + depTok,
			"X-Checksum-Deploy": "true",
			"X-Checksum-Sha1":   wholeSHA1,
		})
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("checksum-deploy through the virtual = %d, want 201", resp.StatusCode)
	}
	resp = h.do(http.MethodGet, "/binflow/gen-virtual/via-virtual.bin", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, p3) {
		t.Fatalf("virtual GET = %d (%d bytes), want the corpus", resp.StatusCode, len(got))
	}
}

// TestUploadsAbortNewWire: abort discards mid-upload — 204, then the token
// addresses nothing (404), second abort 404, no node ever lands.
func TestUploadsAbortNewWire(t *testing.T) {
	h, _ := newUploadsHarness(t)
	tok := mpuCreateToken(t, h, adminUser, adminPass, "generic-local", "big/gone.bin", 5)
	p1, _, _, _ := mpuPayload()
	if code, _ := mpuPutPart(t, h, tok, 1, p1); code != http.StatusOK {
		t.Fatalf("part 1 = %d", code)
	}
	if code, _ := mpuPostToken(t, h, "abort", tok, ""); code != http.StatusNoContent {
		t.Fatalf("abort = %d, want 204", code)
	}
	if code, _ := mpuPostToken(t, h, "status", tok, ""); code != http.StatusNotFound {
		t.Fatalf("status after abort = %d, want 404", code)
	}
	if code, _ := mpuPostToken(t, h, "abort", tok, ""); code != http.StatusNotFound {
		t.Fatalf("second abort = %d, want 404", code)
	}
	resp := h.do(http.MethodGet, "/binflow/generic-local/big/gone.bin", adminUser, adminPass, nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("aborted artifact GET = %d, want 404", resp.StatusCode)
	}
}

// TestUploadsOldWireRetired: every retired spelling falls to the E-26 404 —
// no compatibility shims on a flipped plane (ADR-0039 retirement clause).
func TestUploadsOldWireRetired(t *testing.T) {
	h, _ := newUploadsHarness(t)
	for _, leg := range []struct{ method, path string }{
		{http.MethodGet, "/binflow/api/v1/uploads/status"},
		{http.MethodGet, "/binflow/api/v1/uploads/status/some-id"},
		{http.MethodGet, "/binflow/api/v1/uploads/urlPart/some-id/1"},
		{http.MethodPost, "/binflow/api/v1/uploads/config"},
		{http.MethodPost, "/binflow/api/v1/uploads/complete/some-id"},
		{http.MethodPost, "/binflow/api/v1/uploads/abort/some-id"},
		{http.MethodPost, "/binflow/api/v1/uploads/urlPart/some-id/1"},
		{http.MethodPost, "/binflow/api/v1/uploads/nosuch"},
		{http.MethodDelete, "/binflow/api/v1/uploads/abort"},
	} {
		resp := h.do(leg.method, leg.path, adminUser, adminPass, nil, nil)
		func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s = %d, want the E-26 404 (retired wire)", leg.method, leg.path, resp.StatusCode)
		}
	}
}

// ---------------------------------------------------------------------------
// part-stream defense (kept from the T-289 review rounds)
// ---------------------------------------------------------------------------

// TestUploadsPartLengthRequiredKeepsBody (B2, kept): a part PUT without
// Content-Length answers 411 AND the errors[] envelope reaches the wire.
// The request rides the token lane now — the client's one credential.
func TestUploadsPartLengthRequiredKeepsBody(t *testing.T) {
	h, _ := newUploadsHarness(t)
	tok := mpuCreateToken(t, h, adminUser, adminPass, "generic-local", "big/nolen.bin", 5)
	sid := mpuTokenSessionID(t, tok)

	req, err := http.NewRequest(http.MethodPut,
		h.srv.URL+"/binflow/api/v1/uploads/part/"+sid+"/1",
		io.NopCloser(bytes.NewReader(bytes.Repeat([]byte{1}, 64))))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusLengthRequired {
		t.Fatalf("chunked part PUT = %d, want 411", resp.StatusCode)
	}
	if !strings.Contains(string(body), "errors") || !strings.Contains(string(body), "Content-Length") {
		t.Fatalf("411 body must be the errors[] envelope naming Content-Length, got: %q", body)
	}
}

// TestUploadsTornPartFailsSession (review B1 test debt, kept): a part that
// DECLARES more Content-Length than it delivers must never answer 2xx — the
// session ends observably-failed (status carries the failure now) and
// nothing lands. Raw TCP because the Go client refuses to lie itself.
func TestUploadsTornPartFailsSession(t *testing.T) {
	h, _ := newUploadsHarness(t)
	tok := mpuCreateToken(t, h, adminUser, adminPass, "generic-local", "big/torn.bin", 5)
	sid := mpuTokenSessionID(t, tok)

	conn, err := net.Dial("tcp", h.srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	req := "PUT /binflow/api/v1/uploads/part/" + sid + "/1 HTTP/1.1\r\n" +
		"Host: " + h.srv.Listener.Addr().String() + "\r\n" +
		"Authorization: Bearer " + tok + "\r\n" +
		"Content-Type: application/octet-stream\r\n" +
		"Content-Length: 1048576\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write request head: %v", err)
	}
	if _, err := conn.Write(bytes.Repeat([]byte{9}, 4096)); err != nil {
		t.Fatalf("write torn body: %v", err)
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
	}
	raw := make([]byte, 8192)
	var respBytes []byte
	for {
		n, rerr := conn.Read(raw)
		respBytes = append(respBytes, raw[:n]...)
		if rerr != nil {
			break
		}
		if len(respBytes) > 0 && n == 0 {
			break
		}
	}
	head := string(respBytes)
	if !strings.Contains(head, "HTTP/1.1 5") && !strings.Contains(head, "HTTP/1.1 4") {
		t.Fatalf("torn part response head = %.120s, want a 4xx/5xx (never 2xx)", head)
	}

	// The failure is observable through the task model now.
	code, st := mpuPostToken(t, h, "status", tok, "")
	if code != http.StatusOK || st["status"] != "NON_RETRYABLE_ERROR" {
		t.Fatalf("status after torn part = %d %v, want NON_RETRYABLE_ERROR", code, st)
	}
	resp := h.do(http.MethodGet, "/binflow/generic-local/big/torn.bin", adminUser, adminPass, nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("torn artifact GET = %d, want 404", resp.StatusCode)
	}
}

// TestUploadsManualDriverBasicLane: the part PUT still accepts the shared
// credentials with `w` (the manual-driver arm of the relay URL) while the
// four token verbs do not — the urlPart answer's URL contract.
func TestUploadsManualDriverBasicLane(t *testing.T) {
	h, _ := newUploadsHarness(t, [2]string{"alice", "alice-pw"})
	grant(t, h, "mpu-manual", "generic-local", "**", "alice", false, true, false)

	// alice cannot create (no grant) — admin opens the session.
	tok := mpuCreateToken(t, h, adminUser, adminPass, "generic-local", "big/manual.bin", 5)
	sid := mpuTokenSessionID(t, tok)
	grant(t, h, "mpu-manual2", "generic-local", "big/**", "alice", false, true, false)

	resp := h.do(http.MethodPut, "/binflow/api/v1/uploads/part/"+sid+"/1",
		"alice", "alice-pw", []byte("short final part"), nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Basic-lane part PUT = %d, want 200", resp.StatusCode)
	}
	// The shared credentials never drive the four token verbs.
	resp = h.do(http.MethodPost, "/binflow/api/v1/uploads/status", "alice", "alice-pw", nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Basic-lane status = %d, want 403", resp.StatusCode)
	}
}

// TestUploadsPresignedPartURLNoAuth (the T-332 real-client finding): the
// urlPart answer carries the capability in its query string and the part
// PUT goes through with NO Authorization header at all — exactly how
// jfrog-cli drives what it takes for a presigned URL.
func TestUploadsPresignedPartURLNoAuth(t *testing.T) {
	h, _ := newUploadsHarness(t)
	tok := mpuCreateToken(t, h, adminUser, adminPass, "generic-local", "big/presigned.bin", 5)

	_, body := mpuPostToken(t, h, "urlPart", tok, "?partNumber=1")
	rawURL, _ := body["url"].(string)
	if !strings.Contains(rawURL, "token=") {
		t.Fatalf("urlPart URL carries no capability: %v", body)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("urlPart URL unparseable: %v", err)
	}
	u.Host = h.srv.Listener.Addr().String() // the test server's dialable host
	if strings.EqualFold(u.Scheme, "https") {
		u.Scheme = "http"
	}

	// The raw PUT: no Authorization header, the capability is the URL's.
	// A FULL 5MiB part first (a short one would close the stream).
	full := bytes.Repeat([]byte{7}, 5<<20)
	req, err := http.NewRequest(http.MethodPut, u.String(), bytes.NewReader(full))
	if err != nil {
		t.Fatalf("build PUT: %v", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("presigned PUT: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("presigned part PUT = %d: %s", resp.StatusCode, raw)
	}
	// A short final part with the URL capability too, then the full chain.
	_, body = mpuPostToken(t, h, "urlPart", tok, "?partNumber=2")
	rawURL2, _ := body["url"].(string)
	u2, _ := url.Parse(rawURL2)
	u2.Host = h.srv.Listener.Addr().String()
	if strings.EqualFold(u2.Scheme, "https") {
		u2.Scheme = "http"
	}
	req, err = http.NewRequest(http.MethodPut, u2.String(), bytes.NewReader([]byte("tail")))
	if err != nil {
		t.Fatalf("build PUT 2: %v", err)
	}
	resp, err = h.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("presigned PUT 2: %v", err)
	}
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("presigned short final PUT = %d", resp.StatusCode)
	}
	whole := append(append([]byte{}, full...), []byte("tail")...)
	if code, _ := mpuPostToken(t, h, "complete", tok, "?sha1="+sha1Hex(t, whole)); code != http.StatusAccepted {
		t.Fatalf("complete = %d", code)
	}
	done := mpuStatusPoll(t, h, tok, "FINISHED")
	depTok := done["checksumToken"].(string)
	resp = h.do(http.MethodPut, "/binflow/generic-local/big/presigned.bin", "", "", nil,
		map[string]string{
			"Authorization":     "Bearer " + depTok,
			"X-Checksum-Deploy": "true",
			"X-Checksum-Sha1":   sha1Hex(t, whole),
		})
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("checksum-deploy = %d", resp.StatusCode)
	}
	resp = h.do(http.MethodGet, "/binflow/generic-local/big/presigned.bin", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, whole) {
		t.Fatalf("presigned corpus mismatch (%d vs %d bytes)", len(got), len(whole))
	}
}

// TestUploadsOutOfOrderParts: S3 part-arrival semantics — a worker-pool
// client's leapfrog parts land in the bounded staging set and flush in
// order; the duplicate and budget gates stay honest.
func TestUploadsOutOfOrderParts(t *testing.T) {
	h, _ := newUploadsHarness(t)
	tok := mpuCreateToken(t, h, adminUser, adminPass, "generic-local", "big/ooo.bin", 5)

	p1, p2, p3, whole := mpuPayload()

	// Part 3 first (a leapfrog): staged, 200, nothing in the engine yet.
	if code, echo := mpuPutPart(t, h, tok, 3, p3); code != http.StatusOK {
		t.Fatalf("leapfrog part 3 = %d: %v", code, echo)
	}
	code, st := mpuPostToken(t, h, "status", tok, "")
	if code != http.StatusOK || st["status"] != "PARTS" {
		t.Fatalf("status after staged part = %d %v", code, st)
	}
	// A duplicate of a staged part is a clean 409... part 1 is not even
	// staged; duplicate means already RECEIVED — exercise that after the
	// flush instead. Part 2: still ahead of part 1 → staged too.
	if code, _ := mpuPutPart(t, h, tok, 2, p2); code != http.StatusOK {
		t.Fatalf("leapfrog part 2 = %d", code)
	}
	// Part 1 closes the gap: everything flushes in order, the short tail
	// closes the stream.
	resp := h.do(http.MethodPut,
		fmt.Sprintf("/binflow/api/v1/uploads/part/%s/1?token=%s", mpuTokenSessionID(t, tok), url.QueryEscape(tok)),
		"", "", p1, nil)
	body2, _ := io.ReadAll(resp.Body)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("gap-closing part 1 = %d: %s", resp.StatusCode, body2)
	}
	var echo map[string]any
	_ = json.Unmarshal(body2, &echo)
	if int64(echo["receivedBytes"].(float64)) != int64(len(whole)) || int64(echo["partsReceived"].(float64)) != 3 {
		t.Fatalf("post-flush echo = %v, want the whole corpus accounted", echo)
	}
	// A duplicate of a received part: refused.
	if code, _ := mpuPutPart(t, h, tok, 2, p2); code != http.StatusConflict {
		t.Fatalf("duplicate part = %d, want 409", code)
	}
	// Complete and reconcile byte for byte through the client landing.
	if code, _ := mpuPostToken(t, h, "complete", tok, "?sha1="+sha1Hex(t, whole)); code != http.StatusAccepted {
		t.Fatalf("complete = %d", code)
	}
	done := mpuStatusPoll(t, h, tok, "FINISHED")
	depTok := done["checksumToken"].(string)
	resp = h.do(http.MethodPut, "/binflow/generic-local/big/ooo.bin", "", "", nil,
		map[string]string{
			"Authorization":     "Bearer " + depTok,
			"X-Checksum-Deploy": "true",
			"X-Checksum-Sha1":   sha1Hex(t, whole),
		})
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("checksum-deploy = %d", resp.StatusCode)
	}
	resp = h.do(http.MethodGet, "/binflow/generic-local/big/ooo.bin", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, whole) {
		t.Fatalf("out-of-order corpus mismatch (%d vs %d bytes)", len(got), len(whole))
	}
}

// TestUploadsStagingBudgetRefusal: a part too far ahead of the stream is
// refused honestly (409) instead of buffering without bound.
func TestUploadsStagingBudgetRefusal(t *testing.T) {
	h, _ := newUploadsHarness(t)
	// partSizeMB 5 (clamped): the byte budget admits a handful of 5MiB
	// leapfrogs; the PART-count budget is the easy one to hit exactly —
	// stage mpuMaxStagedParts distinct far-ahead parts of 1 byte each.
	tok := mpuCreateToken(t, h, adminUser, adminPass, "generic-local", "big/budget.bin", 5)
	one := []byte("x") // 1-byte parts: within partSize, absurd but legal
	for i := 2; i < 2+64; i++ {
		if code, _ := mpuPutPart(t, h, tok, i, one); code != http.StatusOK {
			t.Fatalf("staging part %d = %d, want 200", i, code)
		}
	}
	// The 65th staged part crosses the part-count budget: 409.
	if code, _ := mpuPutPart(t, h, tok, 2+64, one); code != http.StatusConflict {
		t.Fatalf("budget-crossing part = %d, want 409", code)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// sha1Hex computes a test corpus's sha1.
func sha1Hex(t *testing.T, b []byte) string {
	t.Helper()
	sum := sha1.Sum(b)
	return hex.EncodeToString(sum[:])
}

// createTypedRepo seeds a non-generic local repo for the typing guard.
func createTypedRepo(t *testing.T, h *harness, key, packageType string) error {
	t.Helper()
	admin := &auth.Principal{Name: adminUser, Admin: true}
	_, err := h.svc.CreateRepo(context.Background(), admin, &metadata.Repo{
		RepoKey: key, Type: repo.TypeLocal, PackageType: packageType,
	})
	return err
}
