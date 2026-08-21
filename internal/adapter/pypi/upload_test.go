package pypi

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/repo"
)

// md5Hex is the digest helper for the md5_digest field variants.
func md5Hex(b []byte) string {
	sum := md5.Sum(b) //nolint:gosec // the PyPI upload API's field, not a security choice
	return hex.EncodeToString(sum[:])
}

// TestUploadHappyPath is PE-02's main line (M30): a twine-shaped multipart
// POST answers a uniform 200 with an empty body, stores the file under the
// ORIGINAL name (no normalization — C7), and the index then serves the
// server-computed sha256.
func TestUploadHappyPath(t *testing.T) {
	s := newStack(t)
	content := []byte("the wheel body")

	status, body := s.upload("/binflow/api/pypi/pypi-local", map[string]string{
		":action":          "file_upload",
		"protocol_version": "1",
		"name":             "Demo_Pkg",
		"version":          "0.1.0",
		"filetype":         "bdist_wheel",
		"pyversion":        "py3",
		"metadata_version": "2.1",
		"summary":          "A demo package",
		"description":      strings.Repeat("long readme ", 100),
	}, "Demo_Pkg-0.1.0-py3-none-any.whl", content)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body %s", status, body)
	}
	if body != "" {
		t.Fatalf("response body = %q, want empty (uniform 200)", body)
	}

	// Stored under the original spelling.
	if _, err := s.md.Nodes().Get(context.Background(), "pypi-local", "Demo_Pkg/0.1.0/Demo_Pkg-0.1.0-py3-none-any.whl"); err != nil {
		t.Fatalf("node at the original-name path: %v", err)
	}

	// Index page: normalized URL, sha256 fragment of the exact bytes.
	_, page, _ := s.get("/binflow/api/pypi/pypi-local/simple/demo-pkg/")
	want := "#sha256=" + sha256Hex(content)
	if !strings.Contains(page, want) {
		t.Fatalf("index lacks %q:\n%s", want, page)
	}
}

// TestUploadTrailingSlashAndBareMount: twine posts to the repository root
// without a slash, but the slash form and the bare content mount address
// the same endpoint.
func TestUploadTrailingSlashAndBareMount(t *testing.T) {
	s := newStack(t)
	for _, path := range []string{
		"/binflow/api/pypi/pypi-local/",
		"/binflow/pypi-local",
	} {
		status, body := s.upload(path, map[string]string{
			":action": "file_upload", "name": "p", "version": "1",
		}, fmt.Sprintf("p-1-%d.tar.gz", len(path)), []byte("x"))
		if status != http.StatusOK {
			t.Fatalf("%s: status = %d, body %s", path, status, body)
		}
	}
}

// TestUploadContentFirstOrder: multipart field order is not contractual —
// a content part arriving BEFORE the metadata fields must still land (the
// session buffers the bytes; the path binds at finalize).
func TestUploadContentFirstOrder(t *testing.T) {
	s := newStack(t)
	body, ct := multipartForm(t, map[string]string{
		":action": "file_upload", "name": "late-fields", "version": "1.0",
	}, "late_fields-1.0.tar.gz", []byte("payload"), true)
	resp := s.do(http.MethodPost, "/binflow/api/pypi/pypi-local", adminUser, adminPass, body,
		map[string]string{"Content-Type": ct})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, readAll(t, resp))
	}
	if _, err := s.md.Nodes().Get(context.Background(), "pypi-local", "late-fields/1.0/late_fields-1.0.tar.gz"); err != nil {
		t.Fatalf("node: %v", err)
	}
}

// TestUploadActionVariants pins the M31 fourth detail: :action is strictly
// file_upload; every other value (and the missing field) is a 400 with the
// pinned wording.
func TestUploadActionVariants(t *testing.T) {
	s := newStack(t)
	tests := []struct {
		name   string
		action string
		want   string
	}{
		{"submit", "submit", "unknown action 'submit'"},
		{"submit_form", "submit_form", "unknown action 'submit_form'"},
		{"browse", "browse", "unknown action 'browse'"},
		{"empty-value", "", "missing ':action' field"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]string{"name": "x", "version": "1"}
			if tc.action != "" {
				fields[":action"] = tc.action
			}
			status, body := s.upload("/binflow/api/pypi/pypi-local", fields, "x-1.tar.gz", []byte("x"))
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, body %s", status, body)
			}
			if !strings.Contains(body, tc.want) {
				t.Fatalf("body %q lacks %q", body, tc.want)
			}
		})
	}

	// No upload may have landed from the rejected forms.
	nodes, err := s.svc.List(context.Background(), nil, "pypi-local", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("rejected uploads left %d nodes behind", len(nodes))
	}
}

// TestUploadMissingFields pins PE-02's 400 branch: name, version and
// content are all required.
func TestUploadMissingFields(t *testing.T) {
	s := newStack(t)
	base := map[string]string{":action": "file_upload", "name": "x", "version": "1"}
	tests := []struct {
		name     string
		mutate   func(map[string]string)
		filename string
	}{
		{"no name", func(f map[string]string) { delete(f, "name") }, "x-1.tar.gz"},
		{"no version", func(f map[string]string) { delete(f, "version") }, "x-1.tar.gz"},
		{"no content", func(map[string]string) {}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]string{}
			for k, v := range base {
				fields[k] = v
			}
			tc.mutate(fields)
			status, body := s.upload("/binflow/api/pypi/pypi-local", fields, tc.filename, []byte("x"))
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, body %s", status, body)
			}
		})
	}
}

// TestUploadMd5Digest pins the md5_digest trichotomy (M31/PE-02): absent
// is fine (twine >= 6.2 — the server computes its own and serves it on
// download), present-and-correct is fine (case-insensitive), and
// present-and-wrong walks the client-checksums 409 chain with the
// received/actual wording.
func TestUploadMd5Digest(t *testing.T) {
	s := newStack(t)
	content := []byte("md5 payload")

	// Absent: accepted; the download headers carry the server-computed md5.
	status, body := s.upload("/binflow/api/pypi/pypi-local", map[string]string{
		":action": "file_upload", "name": "nomd5", "version": "1.0",
	}, "nomd5-1.0.tar.gz", content)
	if status != http.StatusOK {
		t.Fatalf("absent md5: status = %d, body %s", status, body)
	}
	_, _, hdr := s.get("/binflow/api/pypi/pypi-local/packages/nomd5/1.0/nomd5-1.0.tar.gz")
	if got := hdr.Get("X-Checksum-Md5"); got != md5Hex(content) {
		t.Fatalf("server-computed md5 = %q, want %q", got, md5Hex(content))
	}

	// Present and correct (uppercase spelling tolerated).
	status, body = s.upload("/binflow/api/pypi/pypi-local", map[string]string{
		":action": "file_upload", "name": "goodmd5", "version": "1.0",
		"md5_digest": strings.ToUpper(md5Hex(content)),
	}, "goodmd5-1.0.tar.gz", content)
	if status != http.StatusOK {
		t.Fatalf("correct md5: status = %d, body %s", status, body)
	}

	// Present and wrong: 409 with received/actual.
	wrong := strings.Repeat("0", 32)
	status, body = s.upload("/binflow/api/pypi/pypi-local", map[string]string{
		":action": "file_upload", "name": "badmd5", "version": "1.0",
		"md5_digest": wrong,
	}, "badmd5-1.0.tar.gz", content)
	if status != http.StatusConflict {
		t.Fatalf("wrong md5: status = %d, body %s", status, body)
	}
	if !strings.Contains(body, "received") || !strings.Contains(body, "actual") {
		t.Fatalf("409 body %q lacks the received/actual wording", body)
	}
	// And nothing landed.
	if _, err := s.md.Nodes().Get(context.Background(), "pypi-local", "badmd5/1.0/badmd5-1.0.tar.gz"); err == nil {
		t.Fatal("the checksum-mismatched upload must not land a node")
	}

	// Malformed width: 400, not 409.
	status, body = s.upload("/binflow/api/pypi/pypi-local", map[string]string{
		":action": "file_upload", "name": "wide", "version": "1.0",
		"md5_digest": "zz",
	}, "wide-1.0.tar.gz", content)
	if status != http.StatusBadRequest {
		t.Fatalf("malformed md5: status = %d, body %s", status, body)
	}
}

// TestUploadSha256Digest: the sha256_digest field (twine sends it) is
// verified the same way when present.
func TestUploadSha256Digest(t *testing.T) {
	s := newStack(t)
	content := []byte("sha payload")
	status, body := s.upload("/binflow/api/pypi/pypi-local", map[string]string{
		":action": "file_upload", "name": "sha", "version": "1.0",
		"sha256_digest": sha256Hex(content),
	}, "sha-1.0.tar.gz", content)
	if status != http.StatusOK {
		t.Fatalf("correct sha256: status = %d, body %s", status, body)
	}

	status, body = s.upload("/binflow/api/pypi/pypi-local", map[string]string{
		":action": "file_upload", "name": "sha", "version": "1.1",
		"sha256_digest": strings.Repeat("a", 64),
	}, "sha-1.1.tar.gz", content)
	if status != http.StatusConflict {
		t.Fatalf("wrong sha256: status = %d, body %s", status, body)
	}
}

// TestUploadDuplicateFilename pins M33/PE-02's no-overwrite branch: the
// same filename again answers 400 with an "already exists" wording twine
// surfaces verbatim, and the stored original is untouched.
func TestUploadDuplicateFilename(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0.tar.gz", []byte("original"))

	status, body := s.upload("/binflow/api/pypi/pypi-local", map[string]string{
		":action": "file_upload", "name": "demo-pkg", "version": "1.0.0",
	}, "demo_pkg-1.0.0.tar.gz", []byte("different bytes"))
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (no overwrite)", status)
	}
	if !strings.Contains(strings.ToLower(body), "already exists") {
		t.Fatalf("body %q lacks the already-exists wording", body)
	}

	// Same content retransmit: still 400 — the no-overwrite branch fires
	// before the service's idempotent-retransmit semantics.
	status, _ = s.upload("/binflow/api/pypi/pypi-local", map[string]string{
		":action": "file_upload", "name": "demo-pkg", "version": "1.0.0",
	}, "demo_pkg-1.0.0.tar.gz", []byte("original"))
	if status != http.StatusBadRequest {
		t.Fatalf("identical retransmit: status = %d, want 400 (strict no-overwrite)", status)
	}

	// The original bytes survive.
	_, got, _ := s.get("/binflow/api/pypi/pypi-local/packages/demo-pkg/1.0.0/demo_pkg-1.0.0.tar.gz")
	if got != "original" {
		t.Fatalf("stored content = %q, want the original", got)
	}
}

// TestUploadFieldTraversalRejected pins NFR-S18 for the name/version form
// fields: they are future path segments and are judged as such — no ../,
// separators, dot segments or control bytes may reach the node namespace.
// Every variant must 400 and leave the repository empty.
func TestUploadFieldTraversalRejected(t *testing.T) {
	s := newStack(t)
	tests := []struct {
		name     string
		fields   map[string]string
		filename string
	}{
		{"dotdot name", map[string]string{":action": "file_upload", "name": "../evil", "version": "1"}, "evil-1.tar.gz"},
		{"dotted name", map[string]string{":action": "file_upload", "name": "..", "version": "1"}, "x-1.tar.gz"},
		{"name with slash", map[string]string{":action": "file_upload", "name": "a/b", "version": "1"}, "ab-1.tar.gz"},
		{"dotdot version", map[string]string{":action": "file_upload", "name": "ok", "version": "../../etc"}, "ok-1.tar.gz"},
		{"dot version", map[string]string{":action": "file_upload", "name": "ok", "version": "."}, "ok-1.tar.gz"},
		{"name control byte", map[string]string{":action": "file_upload", "name": "ok\x00", "version": "1"}, "ok-1.tar.gz"},
		{"name empty", map[string]string{":action": "file_upload", "name": "", "version": "1"}, "x-1.tar.gz"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, body := s.upload("/binflow/api/pypi/pypi-local", tc.fields, tc.filename, []byte("x"))
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, body %s", status, body)
			}
		})
	}
	nodes, err := s.svc.List(context.Background(), nil, "pypi-local", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(nodes) != 0 {
		names := make([]string, 0, len(nodes))
		for _, n := range nodes {
			names = append(names, n.Path)
		}
		t.Fatalf("traversal variants left nodes behind: %v", names)
	}
}

// TestUploadFilenameNeutralized pins the filename half of NFR-S18 through
// the two defense layers: the multipart layer strips directory components
// from Content-Disposition filenames (RFC 7578 section 4.2 — Go's
// mime/multipart enforces it), so slash-carrying filenames land as their
// BASE NAME inside <name>/<version>/; a backslash-carrying filename that
// survives the parse is rejected outright by the adapter's own segment
// validation. Nothing ever addresses a node outside the repository.
// A literal percent in a NAME field is raw form data, not a URL escape —
// the layer boundary is what this test documents.
func TestUploadFilenameNeutralized(t *testing.T) {
	s := newStack(t)
	tests := []struct {
		name      string
		filename  string
		wantSolid bool // true: 200 with the basename; false: 400
		stored    string
	}{
		{"relative traversal", "../pwned1.tar.gz", true, "pwned1.tar.gz"},
		{"absolute path", "/etc/pwned2.tar.gz", true, "pwned2.tar.gz"},
		{"nested path", "sub/dir/x.tar.gz", true, "x.tar.gz"},
		{"backslash", `..\pwned4.tar.gz`, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, body := s.upload("/binflow/api/pypi/pypi-local", map[string]string{
				":action": "file_upload", "name": "ok", "version": "1",
			}, tc.filename, []byte("x"))
			if tc.wantSolid {
				if status != http.StatusOK {
					t.Fatalf("status = %d, body %s (the basename must land)", status, body)
				}
				if _, err := s.md.Nodes().Get(context.Background(), "pypi-local", "ok/1/"+tc.stored); err != nil {
					t.Fatalf("node at ok/1/%s: %v", tc.stored, err)
				}
				return
			}
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, body %s, want the segment-validation 400", status, body)
			}
		})
	}

	// A literal "%2f" inside the name field is form DATA, not a URL: it
	// names a project with percent characters and stays one segment.
	status, body := s.upload("/binflow/api/pypi/pypi-local", map[string]string{
		":action": "file_upload", "name": "a%2fb", "version": "1",
	}, "ab-1.tar.gz", []byte("x"))
	if status != http.StatusOK {
		t.Fatalf("literal-percent name: status = %d, body %s", status, body)
	}
	if _, err := s.md.Nodes().Get(context.Background(), "pypi-local", "a%2fb/1/ab-1.tar.gz"); err != nil {
		t.Fatalf("node under the literal name: %v", err)
	}

	// Nothing outside <name>/<version>/ ever landed: every FILE node has
	// exactly three segments. Folder rows are excluded — since T-128
	// (ADR-0016) every upload materializes its ancestor directories as
	// trailing-slash rows, which is the invariant working as designed.
	nodes, err := s.svc.List(context.Background(), nil, "pypi-local", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, n := range nodes {
		if strings.HasSuffix(n.Path, "/") {
			continue
		}
		if strings.Count(n.Path, "/") != 2 {
			t.Fatalf("node %q escaped the <name>/<version>/<filename> shape", n.Path)
		}
	}
}

// TestUploadMultipartEdges pins the malformed-body family: a non-multipart
// media type and a multipart content type wrapping garbage both answer 400.
func TestUploadMultipartEdges(t *testing.T) {
	s := newStack(t)

	// Not multipart at all.
	resp := s.do(http.MethodPost, "/binflow/api/pypi/pypi-local", adminUser, adminPass,
		strings.NewReader("plain body"), map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-multipart: status = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Multipart content type but a garbage body.
	resp = s.do(http.MethodPost, "/binflow/api/pypi/pypi-local", adminUser, adminPass,
		strings.NewReader("not a multipart body at all"),
		map[string]string{"Content-Type": "multipart/form-data; boundary=xyz"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("garbage multipart: status = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestUploadFileFieldGuard covers the remaining multipart edges through
// hand-built forms (fields the shared helper cannot express).
func TestUploadFileFieldGuard(t *testing.T) {
	s := newStack(t)

	build := func(parts []formPart) (io.Reader, string) {
		return rawMultipart(t, "guard", parts)
	}

	// A file under the wrong field name.
	body, ct := build([]formPart{
		textPart(":action", "file_upload"),
		textPart("name", "x"),
		textPart("version", "1"),
		filePart("gpg_signature", "x-1.tar.gz.asc", []byte("sig")),
	})
	resp := s.do(http.MethodPost, "/binflow/api/pypi/pypi-local", adminUser, adminPass, body,
		map[string]string{"Content-Type": ct})
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(readAll(t, resp), "only 'content'") {
		t.Fatalf("wrong file field: status = %d, want 400 naming the content field", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Duplicate content parts.
	body, ct = build([]formPart{
		textPart(":action", "file_upload"),
		textPart("name", "x"),
		textPart("version", "1"),
		filePart("content", "x-1.tar.gz", []byte("one")),
		filePart("content", "x-1.tar.gz", []byte("two")),
	})
	resp = s.do(http.MethodPost, "/binflow/api/pypi/pypi-local", adminUser, adminPass, body,
		map[string]string{"Content-Type": ct})
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(readAll(t, resp), "duplicate 'content'") {
		t.Fatalf("duplicate content: status = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// A field over the size cap.
	big := strings.Repeat("a", maxFieldValueBytes+1)
	body, ct = build([]formPart{
		textPart(":action", "file_upload"),
		textPart("name", "x"),
		textPart("version", "1"),
		textPart("description", big),
		filePart("content", "x-1.tar.gz", []byte("ok")),
	})
	resp = s.do(http.MethodPost, "/binflow/api/pypi/pypi-local", adminUser, adminPass, body,
		map[string]string{"Content-Type": ct})
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(readAll(t, resp), "byte limit") {
		t.Fatalf("oversized field: status = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// formPart is one hand-built multipart part (for forms the shared helper
// cannot express).
type formPart struct {
	field    string
	filename string // "" = value part
	value    string
}

func textPart(field, value string) formPart { return formPart{field: field, value: value} }
func filePart(field, name string, b []byte) formPart {
	return formPart{field: field, filename: name, value: string(b)}
}

// rawMultipart renders hand-built parts under one boundary.
func rawMultipart(t *testing.T, boundary string, parts []formPart) (io.Reader, string) {
	t.Helper()
	var sb strings.Builder
	for _, p := range parts {
		fmt.Fprintf(&sb, "--%s\r\n", boundary)
		if p.filename == "" {
			fmt.Fprintf(&sb, "Content-Disposition: form-data; name=%q\r\n\r\n", p.field)
			sb.WriteString(p.value)
			sb.WriteString("\r\n")
			continue
		}
		fmt.Fprintf(&sb, "Content-Disposition: form-data; name=%q; filename=%q\r\n\r\n", p.field, p.filename)
		sb.WriteString(p.value)
		sb.WriteString("\r\n")
	}
	fmt.Fprintf(&sb, "--%s--\r\n", boundary)
	return strings.NewReader(sb.String()), "multipart/form-data; boundary=" + boundary
}

// TestUploadClassGates pins the write gates by repository class (RE-04 for
// remote, C5 for virtual): both answer 405 + Allow: GET before any byte of
// the body is consumed, with the PRD-fixed wordings.
func TestUploadClassGates(t *testing.T) {
	s := newStackCfg(t, nil)
	s.seedRepo(t, "pypi-remote", repo.TypeRemote, repo.PackagePypi)
	s.seedRepo(t, "pypi-virtual", repo.TypeVirtual, repo.PackagePypi)

	tests := []struct {
		repo string
		want string
	}{
		{"pypi-remote", "does not accept uploads"},
		{"pypi-virtual", "No local repository was configured as local deployment repository for the (pypi-virtual) virtual repository."},
	}
	for _, tc := range tests {
		t.Run(tc.repo, func(t *testing.T) {
			body, ct := multipartForm(t, map[string]string{
				":action": "file_upload", "name": "x", "version": "1",
			}, "x-1.tar.gz", []byte("x"), false)
			resp := s.do(http.MethodPost, "/binflow/api/pypi/"+tc.repo, adminUser, adminPass, body,
				map[string]string{"Content-Type": ct})
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, body %s", resp.StatusCode, readAll(t, resp))
			}
			if got := resp.Header.Get("Allow"); got != "GET" {
				t.Fatalf("Allow = %q, want GET", got)
			}
			if respBody := readAll(t, resp); !strings.Contains(respBody, tc.want) {
				t.Fatalf("body %q lacks %q", respBody, tc.want)
			}
		})
	}
}

// TestAnonymousBoundary pins M35's equivalent (AC③): anonymous reads of the
// index and of files pass (anonymous_access default on), anonymous upload
// is challenged with 401 before the adapter runs (NFR-S17).
func TestAnonymousBoundary(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0.tar.gz", []byte("anon"))

	if status, _, _ := s.get("/binflow/api/pypi/pypi-local/simple/demo-pkg/"); status != http.StatusOK {
		t.Fatalf("anonymous simple GET: status = %d, want 200", status)
	}
	if status, body, _ := s.get("/binflow/api/pypi/pypi-local/packages/demo-pkg/1.0.0/demo_pkg-1.0.0.tar.gz"); status != http.StatusOK || body != "anon" {
		t.Fatalf("anonymous file GET: status = %d, want 200", status)
	}

	body, ct := multipartForm(t, map[string]string{
		":action": "file_upload", "name": "x", "version": "1",
	}, "x-1.tar.gz", []byte("x"), false)
	resp := s.do(http.MethodPost, "/binflow/api/pypi/pypi-local", "", "", body,
		map[string]string{"Content-Type": ct})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous POST: status = %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); !strings.HasPrefix(got, `Basic realm="`) {
		t.Fatalf("WWW-Authenticate = %q, want the Basic challenge", got)
	}
}

// TestAnonymousAccessOffRead pins the closed-instance posture: with
// anonymous_access off, even the index answers 401.
func TestAnonymousAccessOffRead(t *testing.T) {
	s := newStackCfg(t, func(c *config.Config) { c.Security.AnonymousAccess = false })
	s.seedRepo(t, "pypi-local", repo.TypeLocal, repo.PackagePypi)
	resp := s.do(http.MethodGet, "/binflow/api/pypi/pypi-local/simple/demo-pkg/", "", "", nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
