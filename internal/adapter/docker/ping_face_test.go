package docker

// L003-2 (L002-contract-replay.md #1/#2, captures a_ping.h/a_pingbad.h):
// the /v2 ping face's two 401 arms are their own Content-Type dialect —
// both spell application/json;charset=ISO-8859-1, unique on the /v2 plane
// (every other JSON face answers the bare application/json) — and the
// arms differ in challenge shape: anonymous keeps the Bearer challenge +
// compact spec body, the refused credential answers the Basic realm +
// pretty "Bad Credentials" (RenderAuthFailure's ping branch, pinned in
// token_test.go's split test).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// TestPingFaceAnonymousCharset: the anonymous ping 401 keeps the exact
// compact UNAUTHORIZED body and Bearer challenge of the closed-instance
// challenge, with the ping face's charset Content-Type; the authenticated
// ping 200 keeps the plane's bare Content-Type (the charset spelling
// belongs to the 401 arms alone).
func TestPingFaceAnonymousCharset(t *testing.T) {
	rs := &remotePullStack{newCatalogStack(t, true)}

	code, body, hdr := rs.get("/v2/", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous ping = %d, want 401", code)
	}
	if got := hdr.Get("Content-Type"); got != "application/json;charset=ISO-8859-1" {
		t.Fatalf("ping Content-Type = %q, want the charset spelling", got)
	}
	if want := `{"errors":[{"code":"UNAUTHORIZED","message":"authentication required","detail":null}]}`; strings.TrimSpace(body) != want {
		t.Fatalf("ping body = %q, want the compact spec form", body)
	}
	if !strings.HasPrefix(hdr.Get("WWW-Authenticate"), `Bearer realm="`) {
		t.Fatalf("ping challenge = %q, want the Bearer form", hdr.Get("WWW-Authenticate"))
	}

	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	req = req.WithContext(adapter.WithPrincipal(req.Context(), rs.admin))
	rec := httptest.NewRecorder()
	rs.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated ping = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("authenticated ping Content-Type = %q, want the bare form", got)
	}
}
