package goproxy

// T-576 / BIN-58: the go-repository domain's .info/.mod Content-Type
// spellings, aligned to A 7.161.26 (L037 Arm 4's three-face probe: storage
// GET, GOPROXY GET and FileInfo mimeType all agree).

import (
	"net/http"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// TestContentTypeOfSpellings pins the wire table verbatim: the +info/+mod
// suffix spellings carry NO charset parameter (A renders them bare), .zip
// stays the plain IANA type, and every other extension keeps the
// octet-stream default.
func TestContentTypeOfSpellings(t *testing.T) {
	cases := []struct {
		ext  string
		want string
	}{
		{ext: "info", want: "application/json+info"},
		{ext: "mod", want: "text/plain+mod"},
		{ext: "zip", want: "application/zip"},
		{ext: "mod.gz", want: "application/octet-stream"}, // not a routable ext on this plane; the storage FileInfo face owns .gz
		{ext: "list", want: "application/octet-stream"},
		{ext: "", want: "application/octet-stream"},
	}
	for _, c := range cases {
		if got := contentTypeOf(c.ext); got != c.want {
			t.Errorf("contentTypeOf(%q) = %q, want %q", c.ext, got, c.want)
		}
	}
}

// TestVersionFileContentTypesOnTheWire walks the full stack: PUT then GET of
// the trio renders the A spellings byte-for-byte (exact equality, so a
// charset parameter on .mod or a bare application/json on .info fails), on
// the plane that in BinFlow serves both the GOPROXY role and the go
// repository's storage-download role.
func TestVersionFileContentTypesOnTheWire(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "go-ct", repo.TypeLocal)
	base := "/binflow/go-ct/example.com/ctmod/@v/v1.0.0"
	mod := "module example.com/ctmod\n"
	if status, body, _ := s.put(base+".mod", []byte(mod), nil); status != http.StatusCreated {
		t.Fatalf("PUT .mod: %d %s", status, body)
	}
	if status, body, _ := s.put(base+".info", []byte(`{"Version":"v1.0.0"}`), nil); status != http.StatusCreated {
		t.Fatalf("PUT .info: %d %s", status, body)
	}
	if status, body, _ := s.put(base+".zip", []byte("PK"), nil); status != http.StatusCreated {
		t.Fatalf("PUT .zip: %d %s", status, body)
	}
	for ext, want := range map[string]string{
		"mod":  "text/plain+mod",
		"info": "application/json+info",
		"zip":  "application/zip",
	} {
		status, _, hdr := s.get(base + "." + ext)
		if status != http.StatusOK {
			t.Fatalf("GET .%s: status %d", ext, status)
		}
		if got := hdr.Get("Content-Type"); got != want {
			t.Errorf("GET .%s Content-Type = %q, want exactly %q (no charset, no parameters)", ext, got, want)
		}
	}
}

// TestModGzStaysUnroutable pins the route set: .mod.gz is NOT one of the
// @v extensions (layout.go's {zip, mod, info}), so GET answers the plain 404
// and PUT never becomes a storage entrance — matching A's GOPROXY face. The
// A storage face's application/x-gzip for .mod.gz renders on the FileInfo
// face (the httpapi extension table's final-segment .gz rule), which is
// outside this package.
func TestModGzStaysUnroutable(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "go-ct", repo.TypeLocal)
	base := "/binflow/go-ct/example.com/ctmod/@v/"
	if status, body, _ := s.put(base+"v1.0.0.zip", []byte("PK"), nil); status != http.StatusCreated {
		t.Fatalf("PUT .zip: %d %s", status, body)
	}
	if status, _, _ := s.get(base + "v1.0.0.mod.gz"); status != http.StatusNotFound {
		t.Errorf("GET .mod.gz = %d, want the unrouted 404", status)
	}
	if status, body, _ := s.put(base+"v1.0.0.mod.gz", []byte("gz"), nil); status != http.StatusNotFound {
		t.Errorf("PUT .mod.gz = %d (%s), want the unrouted 404 — no second storage entrance", status, body)
	}
}
