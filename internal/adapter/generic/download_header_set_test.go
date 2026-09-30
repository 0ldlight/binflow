package generic_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// T-608 / BIN-90 — the artifact-download header set against the live A
// model T-601's dual-round probe pinned (7.161.26): the Content-Disposition
// / X-Artifactory-Filename pair on every artifact-body status (ASCII
// dual-parameter form, non-ASCII filename*-only form), the 416's bare set,
// the 304 keeping the pair while CT/CL go, virtual member-header
// inheritance, and the generic sidecar echo's verb-conditional header model
// (R13's two absorbed UNKNOWNs).

const (
	asciiPath = "/binflow/generic-local/t608/plain-ascii.bin"
	utf8Path  = "/binflow/generic-local/t608/文件-ünïcode.bin"
	asciiBody = "T608 plain ascii artifact payload 0123456789 ABCDEFGHIJKLMNOP\n"
	utf8Body  = "T608 utf8-named artifact payload\n"
)

func put608(t *testing.T, e *env, path, content string) {
	t.Helper()
	resp := e.do(t, http.MethodPut, path, strings.NewReader(content), nil)
	defer resp.Body.Close() //nolint:errcheck // probe
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("put %s: %d %s", path, resp.StatusCode, body(t, resp))
	}
}

// assertDispositionPair checks the pair against the live wire forms.
func assertDispositionPair(t *testing.T, h http.Header, wantName, wantCD string) {
	t.Helper()
	if got := h.Get("Content-Disposition"); got != wantCD {
		t.Fatalf("Content-Disposition = %q, want %q", got, wantCD)
	}
	if got := h.Get("X-Artifactory-Filename"); got != wantName {
		t.Fatalf("X-Artifactory-Filename = %q, want %q", got, wantName)
	}
}

func TestDownloadDispositionHeaderForms(t *testing.T) {
	e := newEnv(t)
	put608(t, e, asciiPath, asciiBody)
	put608(t, e, utf8Path, utf8Body)

	tests := []struct {
		name     string
		path     string
		wantName string
		wantCD   string
	}{
		{"ascii GET", asciiPath, "plain-ascii.bin",
			`attachment; filename="plain-ascii.bin"; filename*=UTF-8''plain-ascii.bin`},
		{"ascii HEAD", asciiPath, "plain-ascii.bin",
			`attachment; filename="plain-ascii.bin"; filename*=UTF-8''plain-ascii.bin`},
		{"utf8 GET", utf8Path, "%E6%96%87%E4%BB%B6-%C3%BCn%C3%AFcode.bin",
			"attachment; filename*=UTF-8''%E6%96%87%E4%BB%B6-%C3%BCn%C3%AFcode.bin"},
		{"utf8 HEAD", utf8Path, "%E6%96%87%E4%BB%B6-%C3%BCn%C3%AFcode.bin",
			"attachment; filename*=UTF-8''%E6%96%87%E4%BB%B6-%C3%BCn%C3%AFcode.bin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := http.MethodGet
			if strings.Contains(tt.name, "HEAD") {
				method = http.MethodHead
			}
			resp := e.do(t, method, tt.path, nil, nil)
			defer resp.Body.Close() //nolint:errcheck // probe
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			assertDispositionPair(t, resp.Header, tt.wantName, tt.wantCD)
		})
	}

	// 206 carries the pair alongside Content-Range (gl-range-0-9).
	resp := e.do(t, http.MethodGet, asciiPath, nil, map[string]string{"Range": "bytes=0-9"})
	defer resp.Body.Close() //nolint:errcheck // probe
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("range status = %d, want 206", resp.StatusCode)
	}
	assertDispositionPair(t, resp.Header, "plain-ascii.bin",
		`attachment; filename="plain-ascii.bin"; filename*=UTF-8''plain-ascii.bin`)
}

func TestDownloadRange416BareHeaderSet(t *testing.T) {
	e := newEnv(t)
	put608(t, e, asciiPath, asciiBody)
	resp := e.do(t, http.MethodGet, asciiPath, nil, map[string]string{"Range": "bytes=999999-"})
	defer resp.Body.Close() //nolint:errcheck // probe
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("status = %d, want 416", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Range"); got != fmt.Sprintf("bytes */%d", len(asciiBody)) {
		t.Fatalf("Content-Range = %q, want bytes */%d", got, len(asciiBody))
	}
	if got := resp.Header.Get("Content-Length"); got != "0" {
		t.Fatalf("Content-Length = %q, want 0", got)
	}
	for _, k := range []string{
		"Content-Type", "Etag", "Last-Modified", "Accept-Ranges",
		"Content-Disposition", "X-Artifactory-Filename",
		"X-Checksum-Md5", "X-Checksum-Sha1", "X-Checksum-Sha256",
	} {
		if got := resp.Header.Get(k); got != "" {
			t.Fatalf("416 keeps %s = %q, want the bare set (absent)", k, got)
		}
	}
}

func TestDownloadConditional304KeepsDispositionPair(t *testing.T) {
	e := newEnv(t)
	put608(t, e, asciiPath, asciiBody)
	head := e.do(t, http.MethodHead, asciiPath, nil, nil)
	etag := head.Header.Get("ETag")
	defer head.Body.Close() //nolint:errcheck // probe
	if etag == "" {
		t.Fatal("fixture ETag missing")
	}

	resp := e.do(t, http.MethodGet, asciiPath, nil, map[string]string{"If-None-Match": etag})
	defer resp.Body.Close() //nolint:errcheck // probe
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", resp.StatusCode)
	}
	// The near-full set (T-601 §二): the pair, ETag, validators and
	// Accept-Ranges stay; Content-Type and Content-Length go.
	assertDispositionPair(t, resp.Header, "plain-ascii.bin",
		`attachment; filename="plain-ascii.bin"; filename*=UTF-8''plain-ascii.bin`)
	if got := resp.Header.Get("ETag"); got != etag {
		t.Fatalf("304 ETag = %q, want %q", got, etag)
	}
	if got := resp.Header.Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("304 Accept-Ranges = %q, want bytes", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "" {
		t.Fatalf("304 Content-Type = %q, want suppressed", got)
	}
	if got := resp.Header.Get("Content-Length"); got != "" {
		t.Fatalf("304 Content-Length = %q, want suppressed", got)
	}
}

func TestDownloadVirtualInheritsDispositionPair(t *testing.T) {
	e := newVirtualEnv(t)
	put608(t, e, asciiPath, asciiBody)
	resp := e.do(t, http.MethodGet, "/binflow/gvirt/t608/plain-ascii.bin", nil, nil)
	defer resp.Body.Close() //nolint:errcheck // probe
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	// Member headers inherit byte-identically (T-601 §二's virtual model)
	// and B's own resolution hint stays (F4 INTENTIONAL-keep).
	assertDispositionPair(t, resp.Header, "plain-ascii.bin",
		`attachment; filename="plain-ascii.bin"; filename*=UTF-8''plain-ascii.bin`)
	if got := resp.Header.Get("X-BinFlow-Resolved-From"); got != "generic-local" {
		t.Fatalf("X-BinFlow-Resolved-From = %q, want generic-local", got)
	}
}

func TestSidecarEchoVerbConditionalHeaderModel(t *testing.T) {
	e := newEnv(t)
	put608(t, e, asciiPath, asciiBody)
	s256, s1, m5 := digestsOf(asciiBody)

	// Register the client sha1 (a correct declaration lands 201).
	reg := e.do(t, http.MethodPut, asciiPath+".sha1", strings.NewReader(s1), nil)
	defer reg.Body.Close() //nolint:errcheck // probe
	if reg.StatusCode != http.StatusCreated {
		t.Fatalf("register .sha1: %d %s", reg.StatusCode, body(t, reg))
	}

	// GET: the infra set ONLY — CT, CL, Last-Modified; nothing a client
	// could validate the echoed bytes against (T-598 leg g1-get-sha1-set).
	get := e.do(t, http.MethodGet, asciiPath+".sha1", nil, nil)
	defer get.Body.Close() //nolint:errcheck // probe
	if get.StatusCode != http.StatusOK {
		t.Fatalf("sidecar GET status = %d, want 200", get.StatusCode)
	}
	if got := body(t, get); got != s1 {
		t.Fatalf("sidecar GET body = %q, want %q", got, s1)
	}
	if got := get.Header.Get("Content-Type"); got != "application/x-checksum" {
		t.Fatalf("sidecar GET Content-Type = %q, want application/x-checksum", got)
	}
	if got := get.Header.Get("Content-Length"); got != fmt.Sprint(len(s1)) {
		t.Fatalf("sidecar GET Content-Length = %q, want %d", got, len(s1))
	}
	if get.Header.Get("Last-Modified") == "" {
		t.Fatal("sidecar GET Last-Modified missing, want present (sidecar-get-last-modified)")
	}
	for _, k := range []string{
		"Etag", "Accept-Ranges", "Content-Disposition", "X-Artifactory-Filename",
		"X-Checksum-Md5", "X-Checksum-Sha1", "X-Checksum-Sha256",
	} {
		if got := get.Header.Get(k); got != "" {
			t.Fatalf("sidecar GET keeps %s = %q, want the infra-only set", k, got)
		}
	}

	// HEAD: the full validator set addressing the SOURCE, plus the
	// disposition pair under the SOURCE's base name (T-598 leg
	// g1-head-sha1-set).
	head := e.do(t, http.MethodHead, asciiPath+".sha1", nil, nil)
	defer head.Body.Close() //nolint:errcheck // probe
	if head.StatusCode != http.StatusOK {
		t.Fatalf("sidecar HEAD status = %d, want 200", head.StatusCode)
	}
	if got := head.Header.Get("Content-Type"); got != "application/x-checksum" {
		t.Fatalf("sidecar HEAD Content-Type = %q, want application/x-checksum", got)
	}
	if got := head.Header.Get("Content-Length"); got != fmt.Sprint(len(s1)) {
		t.Fatalf("sidecar HEAD Content-Length = %q, want %d", got, len(s1))
	}
	if head.Header.Get("Last-Modified") == "" {
		t.Fatal("sidecar HEAD Last-Modified missing, want present")
	}
	if got := head.Header.Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("sidecar HEAD Accept-Ranges = %q, want bytes", got)
	}
	if got := head.Header.Get("ETag"); got != s1 {
		t.Fatalf("sidecar HEAD ETag = %q, want the unquoted source sha1 %q", got, s1)
	}
	for k, want := range map[string]string{
		"X-Checksum-Sha256": s256,
		"X-Checksum-Sha1":   s1,
		"X-Checksum-Md5":    m5,
	} {
		if got := head.Header.Get(k); got != want {
			t.Fatalf("sidecar HEAD %s = %q, want the source's %q", k, got, want)
		}
	}
	assertDispositionPair(t, head.Header, "plain-ascii.bin",
		`attachment; filename="plain-ascii.bin"; filename*=UTF-8''plain-ascii.bin`)
}
