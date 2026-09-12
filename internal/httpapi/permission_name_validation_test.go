// L009-3 (ledger rest/permissions-v1-edge-validation-family arms ①②,
// authority reports/compatibility/L006-a-b-diff.md §2.1 + live :8082
// probes 2026-09-12): the classic keyed permissions face's name semantics —
// the double decode of the path key (JAX-RS percent decode, then the
// reference's own URLDecoder pass with '+' as a space), the two name
// validators (NameValidator on a body-carried name, XSSValidator on a
// nameless body's path key), and the transport's bare 400 for a malformed
// escape on the wire.

package httpapi_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
)

// keyedPut issues a keyed-face PUT with the caller's path segment and body.
func keyedPut(t *testing.T, h *harness, seg, body string) (*http.Response, string) {
	t.Helper()
	resp := t215As(t, h, http.MethodPut, "api/security/permissions/"+seg, adminUser, adminPass, body)
	return resp, readAllT444(t, resp)
}

// keyedGet issues a keyed-face GET and returns the status and body.
func keyedGet(t *testing.T, h *harness, seg string) (*http.Response, string) {
	t.Helper()
	resp := t215As(t, h, http.MethodGet, "api/security/permissions/"+seg, adminUser, adminPass, "")
	return resp, readAllT444(t, resp)
}

// wantEnvelope asserts the response is the errors envelope with exactly the
// given status and message.
func wantEnvelope(t *testing.T, resp *http.Response, raw string, status int, message string) {
	t.Helper()
	var env struct {
		Errors []struct {
			Status  int    `json:"status"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if resp.StatusCode != status || json.Unmarshal([]byte(raw), &env) != nil ||
		len(env.Errors) != 1 || env.Errors[0].Status != status || env.Errors[0].Message != message {
		t.Fatalf("status=%d body=%s, want envelope {status:%d, message:%q}",
			resp.StatusCode, raw, status, message)
	}
}

// TestPermissionsV1KeySecondDecode: the reference decodes the keyed name a
// second time with URLDecoder semantics — '+' becomes a space and a further
// %XX pass runs — and a malformed escape in that pass answers the
// URLDecoder's own envelope wording (:8082, wire-verified 2026-09-12).
func TestPermissionsV1KeySecondDecode(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	v1body := `{"includesPattern":"**","excludesPattern":"","repositories":["generic-local"],"principals":{"users":{}}}`

	// The malformed-escape 400s (verbatim reference wording, envelopes).
	cases := []struct {
		seg     string
		message string
	}{
		{"n1%25zz", `URLDecoder: Illegal hex characters in escape (%) pattern - not a hexadecimal digit: "z" = 122`},
		{"n1%252g", `URLDecoder: Illegal hex characters in escape (%) pattern - not a hexadecimal digit: "g" = 103`},
		{"n1%25g2", `URLDecoder: Illegal hex characters in escape (%) pattern - not a hexadecimal digit: "g" = 103`},
		{"n1%25", `URLDecoder: Incomplete trailing escape (%) pattern`},
		{"n1%25%20b%25zz", `URLDecoder: Illegal hex characters in escape (%) pattern - not a hexadecimal digit: " " = 32`},
	}
	for _, tc := range cases {
		for _, verb := range []string{http.MethodGet, http.MethodDelete} {
			var resp *http.Response
			var raw string
			if verb == http.MethodGet {
				resp, raw = keyedGet(t, h, tc.seg)
			} else {
				resp = t215As(t, h, verb, "api/security/permissions/"+tc.seg, adminUser, adminPass, "")
				raw = readAllT444(t, resp)
			}
			wantEnvelope(t, resp, raw, http.StatusBadRequest, tc.message)
		}
	}

	// The second decode is load-bearing for the lookup: the space-named
	// target answers through every spelling the reference admits.
	resp, raw := keyedPut(t, h, "pl%20name%20x", v1body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create pl-name-x = %d body=%s", resp.StatusCode, raw)
	}
	for _, seg := range []string{"pl%20name%20x", "pl+name+x", "pl%2Bname%2Bx"} {
		resp, raw = keyedGet(t, h, seg)
		if resp.StatusCode != http.StatusOK || !strings.Contains(raw, "pl name x") {
			t.Fatalf("GET %s = %d body=%s, want the space-named target", seg, resp.StatusCode, raw)
		}
	}
	// And for the create: a '+' in the path key stores as a space.
	resp, raw = keyedPut(t, h, "plus%2Bprobe", v1body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create plus-probe = %d body=%s", resp.StatusCode, raw)
	}
	resp, raw = keyedGet(t, h, "plus%20probe")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET plus%%20probe = %d body=%s, want the '+'-created target under its space name", resp.StatusCode, raw)
	}
	// A double-escape resolves through both passes: %2570d2 -> %70d2 -> pd2.
	resp, raw = keyedPut(t, h, "pd2", v1body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create pd2 = %d body=%s", resp.StatusCode, raw)
	}
	resp, raw = keyedGet(t, h, "%2570d2")
	if resp.StatusCode != http.StatusOK || !strings.Contains(raw, `"pd2"`) {
		t.Fatalf("GET %%2570d2 = %d body=%s, want the double-decoded pd2 target", resp.StatusCode, raw)
	}
}

// TestPermissionsV1KeyNameTransport400: a malformed escape on the wire never
// reaches the app — the HTTP transport rejects the request line with the
// same bare 400 Bad Request the reference's connector does (the L006
// observation of a BinFlow 404 was a client that re-quoted the % before
// sending). Pinned through a raw TCP write so no client normalizes it.
func TestPermissionsV1KeyNameTransport400(t *testing.T) {
	h := newHarness(t)
	conn, err := net.Dial("tcp", h.srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = fmt.Fprintf(conn, "GET /binflow/api/security/permissions/%%zz HTTP/1.1\r\nHost: t\r\nConnection: close\r\n\r\n")
	var sb strings.Builder
	_, _ = bufio.NewReader(conn).WriteTo(&sb)
	got := sb.String()
	if !strings.Contains(got, "400 Bad Request") || !strings.Contains(got, "text/plain") {
		t.Fatalf("raw %%zz answer = %q, want the transport's bare 400 Bad Request plain text", got)
	}
}

// TestPermissionsV1KeyXSSValidator: the nameless body's path key must clear
// the reference's XSSValidator — its own decompiled pattern
// (.*)<(|/|[^/>][^>]+|/[^/>][^>]+)>(.*) under matches() semantics: the
// bracketed content empty, a bare slash, or two-plus characters not
// starting with a slash (both tag and closing-tag shapes). The corner arms
// were live-verified on :8082 after the port (Review A rework): "</x>" 201
// (one-letter closing tag passes), "</>" 400 (bare slash refuses).
func TestPermissionsV1KeyXSSValidator(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	v1body := `{"includesPattern":"**","excludesPattern":"","repositories":["generic-local"],"principals":{"users":{}}}`
	const xssMsg = "Name may contains a Cross-Site Scripting expression"

	reject := []string{
		"n1%3Cem%3En2",           // <em> — two-plus char content
		"n1%3Cscript%3En2",       // <script>
		"n1%3CScRiPt%3En2",       // any case (the pattern has no letters)
		"n1%3Ca%20href%3Dx%3En2", // <a href=x> — attrs ride [^>]+
		"n1%3Cfoo%3En2",          // unknown tag
		"n1%3Cfoo%20bar%3En2",    // tag with attributes
		"n1%3Cfoo%2F%3En2",       // <foo/> — '/' inside rides [^>]+
		"n1%3C%3En2",             // <> — the empty alternative
		"n1%3C%2F%3En2",          // </> — the bare-slash alternative
		"n1%3C%2Fxy%3En2",        // </xy> — closing tag, two-plus name
		"n1%3Cb%20%3En2",         // one letter + a space = two chars
		// Newline INSIDE the brackets: the negated class [^>]+ crosses
		// newlines even though the pattern's (.*) edges do not (RE2 and
		// Java agree on both defaults). Reference-side this arm is
		// unroutable (routing 404 for a %-decoded newline in the segment,
		// probed :8082) — the pin here is the port's Java fidelity.
		"n1%3Ce%0Am%3En2",
	}
	for _, seg := range reject {
		resp, raw := keyedPut(t, h, seg, v1body)
		wantEnvelope(t, resp, raw, http.StatusBadRequest, xssMsg)
	}

	accept := []string{
		"n1%3Cb%3En2",    // one character between brackets
		"n1%3C1%3En2",    // ... even a digit
		"n1%3C%2Fx%3En2", // </x> — one-letter CLOSING tag needs /name of two-plus
		"x%20%3C%20y",    // no closing bracket at all
		"n1%3Cfoo",       // '<' with no closing '>'
		"a%0A%3Cem%3En2", // newline BEFORE the bracket: the leading .* cannot cross it (same unroutable-on-reference note)
	}
	for _, seg := range accept {
		resp, raw := keyedPut(t, h, seg, v1body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("PUT %s = %d body=%s, want 201 (passes the validator)", seg, resp.StatusCode, raw)
		}
	}

	// The check runs before the repositories validation and on every keyed
	// verb's name path: an XSS-shaped name with a repositories-less body
	// answers the XSS message, not the missing-repositories one.
	resp, raw := keyedPut(t, h, "n1%3Cem%3En2", `{"principals":{"users":{}}}`)
	wantEnvelope(t, resp, raw, http.StatusBadRequest, xssMsg)

	// The rich face keeps its frozen posture: no validator there.
	resp2 := t215As(t, h, http.MethodPost, "api/v1/permissions", adminUser, adminPass,
		`{"name":"rich<em>name","repos":["generic-local"],"principals":{"users":{}}}`)
	if raw2 := readAllT444(t, resp2); resp2.StatusCode != http.StatusCreated {
		t.Fatalf("rich-face create = %d body=%s, want 201 (frozen, no validator)", resp2.StatusCode, raw2)
	}
}

// TestPermissionsV1KeyNameValidator: a name the BODY carries must clear
// NameValidator's illegal-character set (the reference's verbatim wording,
// an envelope) plus its three literal rejections (".", "..", "&" answer
// the "empty link" 400, wire-verified :8082 2026-09-12 — exact match only:
// "a&b" and "ok.name" pass), and the checks fire BEFORE the 409 path/body
// mismatch. The nameless body's path key skips it entirely (an entity-key
// "n1/n2" is accepted, probed).
func TestPermissionsV1KeyNameValidator(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	v1body := `{"includesPattern":"**","excludesPattern":"","repositories":["generic-local"],"principals":{"users":{}}}`
	const illegalMsg = `Illegal name : '/,\,:,|,?,<,>,*,"' is not allowed`

	for _, name := range []string{"bad/name", "star*name", `q\"uote`, "colon:name", "pipe|name", `back\\slash`, "q?mark", "lt<gt"} {
		resp, raw := keyedPut(t, h, "benign-l009", `{"name":"`+name+`","includesPattern":"**","repositories":["generic-local"],"principals":{"users":{}}}`)
		wantEnvelope(t, resp, raw, http.StatusBadRequest, illegalMsg)
	}

	// The three literal arms, each with its own wording (the probed
	// reference message interpolates the name).
	for _, name := range []string{".", "..", "&"} {
		resp, raw := keyedPut(t, h, "benign-l009", `{"name":"`+name+`","includesPattern":"**","repositories":["generic-local"],"principals":{"users":{}}}`)
		wantEnvelope(t, resp, raw, http.StatusBadRequest, "Name cannot be empty link: '"+name+"'")
	}

	// Before the 409: a malicious body name against a benign path key
	// answers the Illegal name 400, not the mismatch 409...
	resp, raw := keyedPut(t, h, "benign-l009", `{"name":"bad/name","includesPattern":"**","repositories":["generic-local"],"principals":{"users":{}}}`)
	wantEnvelope(t, resp, raw, http.StatusBadRequest, illegalMsg)
	// The &-arm is EXACT match: "a&b" passes the literals and falls to
	// the 409 (probed on the reference).
	resp, raw = keyedPut(t, h, "benign-l009", `{"name":"a&b","includesPattern":"**","repositories":["generic-local"],"principals":{"users":{}}}`)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("a&b = %d body=%s, want the 409 (only a bare & is literal)", resp.StatusCode, raw)
	}
	// ...while a benign body name against an XSS-shaped path key answers
	// the 409 (the body's name governs; the path key is never validated).
	resp, raw = keyedPut(t, h, "n1%3Cem%3En2", `{"name":"benign-l009","includesPattern":"**","repositories":["generic-local"],"principals":{"users":{}}}`)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("mismatch with XSS path = %d body=%s, want the 409", resp.StatusCode, raw)
	}

	// The entity-key arm skips NameValidator: a slash in the path key is
	// accepted (a nameless body creating "n1/n2", probed on :8082).
	resp, raw = keyedPut(t, h, "n1%2Fn2", v1body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("entity-key n1/n2 = %d body=%s, want 201 (NameValidator is body-name-only)", resp.StatusCode, raw)
	}
	resp, raw = keyedGet(t, h, "n1%2Fn2")
	if resp.StatusCode != http.StatusOK || !strings.Contains(raw, `"n1/n2"`) {
		t.Fatalf("GET n1%%2Fn2 = %d body=%s, want the slash-named target", resp.StatusCode, raw)
	}
}
