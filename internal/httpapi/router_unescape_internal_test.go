// L009-3: the withNameUnescaped seam's defensive arm. A malformed escape in
// the wire segment cannot arrive through the listener (the HTTP transport
// rejects the request line first — see TestPermissionsV1KeyNameTransport400
// for the wire proof), but the seam must not smuggle a raw %zz into a
// handler as a lookup key either: the branch answers the transport's own
// bare 400.

package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithNameUnescapedBadEscapeDefensive(t *testing.T) {
	s := &Server{}
	called := false
	wrapped := s.withNameUnescaped("security/permissions/%zz", "security/permissions/",
		func(http.ResponseWriter, *http.Request, string) { called = true })
	w := httptest.NewRecorder()
	wrapped(w, httptest.NewRequest(http.MethodGet, "/binflow/api/security/permissions/x", nil))
	if called {
		t.Fatal("handler ran for a malformed escape")
	}
	if w.Code != http.StatusBadRequest || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" ||
		w.Body.String() != "400 Bad Request" {
		t.Fatalf("defensive arm = %d %q, want the bare 400 Bad Request plain text", w.Code, w.Body.String())
	}
}

// The second (URLDecoder) decode and its Java-verbatim error wording.
func TestURLDecoderDecode(t *testing.T) {
	ok := []struct{ in, want string }{
		{"plain", "plain"},
		{"a+b", "a b"},
		{"a%20b", "a b"},
		{"%70d2", "pd2"},
		{"n1%2Fn2", "n1/n2"},
		{"%3Cem%3E", "<em>"},
	}
	for _, tc := range ok {
		got, errMsg := urlDecoderDecode(tc.in)
		if errMsg != "" || got != tc.want {
			t.Errorf("decode(%q) = %q, %q; want %q, no error", tc.in, got, errMsg, tc.want)
		}
	}
	bad := []struct{ in, want string }{
		{"%zz", `URLDecoder: Illegal hex characters in escape (%) pattern - not a hexadecimal digit: "z" = 122`},
		{"%G2", `URLDecoder: Illegal hex characters in escape (%) pattern - not a hexadecimal digit: "G" = 71`},
		{"n1%", "URLDecoder: Incomplete trailing escape (%) pattern"},
		{"n1%2", "URLDecoder: Incomplete trailing escape (%) pattern"},
	}
	for _, tc := range bad {
		got, errMsg := urlDecoderDecode(tc.in)
		if errMsg != tc.want || got != "" {
			t.Errorf("decode(%q) error = %q; want %q", tc.in, errMsg, tc.want)
		}
	}
	if got, errMsg := urlDecoderDecode(""); errMsg != "" || got != "" {
		t.Errorf("decode(\"\") = %q, %q; want empty, no error", got, errMsg)
	}
}
