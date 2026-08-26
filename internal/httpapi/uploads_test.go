package httpapi_test

// T-289 tests: the /api/v1/uploads MPU REST plane. Two stacks:
//
//   - the STANDARD harness (disk engine, no seam wired) is the filestore
//     instance: every endpoint answers the honest plain-text 501
//     (FR-90-AC3), and the auth door still precedes the capability answer.
//   - newHarnessFull with Deps.Uploads wired to fakeMPUSeam is the
//     pure-S3 assembly's shape. The fake relays each session's Commit
//     into the stack's real disk engine — the same engine repo.Service
//     fronts — so PutLandedBlob's physical-blob probe succeeds exactly as
//     it does in production, where the seam and the service share ONE
//     S3Engine instance. The part-size clamp and digest verification ride
//     the real engine code underneath.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// ---------------------------------------------------------------------------
// fake seam (storage.MultipartUploads stand-in)
// ---------------------------------------------------------------------------

// fakeMPUSeam stands in for *storage.S3Engine on the unit stack: sessions
// accumulate in memory per part (the wire behavior the handler owns) and
// Commit streams the whole payload through the stack's real engine, so the
// blob is physically where repo.Service expects it.
type fakeMPUSeam struct {
	eng   storage.Engine
	begun atomic.Int64
}

func (f *fakeMPUSeam) BeginMultipartSession(_ context.Context, partSize int64) (storage.Session, error) {
	f.begun.Add(1)
	return &fakeMPUSession{eng: f.eng, partSize: partSize, id: fmt.Sprintf("mpu-%d", f.begun.Load())}, nil
}

// fakeMPUSession implements storage.Session in the seam's shape.
type fakeMPUSession struct {
	eng      storage.Engine
	id       string
	partSize int64

	mu   sync.Mutex
	buf  []byte
	done bool
}

func (s *fakeMPUSession) ID() string { return s.id }

func (s *fakeMPUSession) Offset() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return int64(len(s.buf))
}

func (s *fakeMPUSession) Append(_ context.Context, r io.Reader) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return 0, fmt.Errorf("session %s already finalized", s.id)
	}
	n, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	s.buf = append(s.buf, n...)
	return int64(len(s.buf)), nil
}

func (s *fakeMPUSession) Commit(ctx context.Context, expect storage.BlobRef) (storage.BlobRef, error) {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return storage.BlobRef{}, fmt.Errorf("session %s already finalized", s.id)
	}
	s.done = true
	payload := s.buf
	s.mu.Unlock()
	inner, err := s.eng.BeginSession(ctx)
	if err != nil {
		return storage.BlobRef{}, err
	}
	if _, err := inner.Append(ctx, bytes.NewReader(payload)); err != nil {
		_ = inner.Abort(ctx)
		return storage.BlobRef{}, err
	}
	return inner.Commit(ctx, expect)
}

func (s *fakeMPUSession) Abort(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done = true
	return nil
}

// newUploadsHarness builds the seam-wired stack (the pure-S3 shape).
func newUploadsHarness(t *testing.T, users ...[2]string) (*harness, *fakeMPUSeam) {
	t.Helper()
	var seam *fakeMPUSeam
	h := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		seam = &fakeMPUSeam{eng: d.GC.(storage.Engine)}
		d.Uploads = seam
	}, users)
	return h, seam
}

// mpuCreate opens a session through the real HTTP plane and returns the
// decoded body.
func mpuCreate(t *testing.T, h *harness, user, pass, body string) (int, map[string]any) {
	t.Helper()
	resp := h.do(http.MethodPost, "/binflow/api/v1/uploads/create", user, pass, []byte(body), nil)
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("create body not JSON (%d): %s", resp.StatusCode, raw)
	}
	return resp.StatusCode, out
}

// mpuStatus fetches one session's status (or the bare list when id == "").
func mpuStatus(t *testing.T, h *harness, user, pass, id string) (int, []byte) {
	t.Helper()
	path := "/binflow/api/v1/uploads/status"
	if id != "" {
		path += "/" + id
	}
	resp := h.do(http.MethodGet, path, user, pass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// ---------------------------------------------------------------------------
// filestore honesty (FR-90-AC3)
// ---------------------------------------------------------------------------

// TestUploadsFilestoreHonest501 pins the whole six-endpoint set on a
// filestore instance: 501, text/plain, "not supported on this backend" —
// never a 404 masquerading as an absent route. Authentication still comes
// first (the route gate), so anonymous callers meet the 401 challenge.
func TestUploadsFilestoreHonest501(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	legs := []struct {
		name   string
		method string
		path   string
	}{
		{"create", http.MethodPost, "/binflow/api/v1/uploads/create"},
		{"config", http.MethodPost, "/binflow/api/v1/uploads/config"},
		{"urlPart", http.MethodGet, "/binflow/api/v1/uploads/urlPart/some-id/1"},
		{"status one", http.MethodGet, "/binflow/api/v1/uploads/status/some-id"},
		{"status list", http.MethodGet, "/binflow/api/v1/uploads/status"},
		{"complete", http.MethodPost, "/binflow/api/v1/uploads/complete/some-id"},
		{"abort", http.MethodPost, "/binflow/api/v1/uploads/abort/some-id"},
		{"part", http.MethodPut, "/binflow/api/v1/uploads/part/some-id/1"},
	}
	for _, leg := range legs {
		t.Run(leg.name, func(t *testing.T) {
			resp := h.do(leg.method, leg.path, adminUser, adminPass, []byte(`{"repoKey":"generic-local","path":"a.bin"}`), nil)
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusNotImplemented {
				t.Fatalf("status = %d, want 501 (body: %s)", resp.StatusCode, body)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
				t.Fatalf("Content-Type = %q, want text/plain", ct)
			}
			if !strings.Contains(string(body), "not supported on this backend") {
				t.Fatalf("body lacks the honest refusal: %s", body)
			}
			if !strings.Contains(string(body), "S3") {
				t.Fatalf("body does not name the S3-only fact: %s", body)
			}
		})
	}

	// Anonymous meets the route's 401 first — the plane exists, it is the
	// backend that lacks the capability.
	resp := h.do(http.MethodPost, "/binflow/api/v1/uploads/create", "", "", []byte(`{}`), nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous create = %d, want 401 (auth precedes the capability answer)", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// the S3-shaped stack
// ---------------------------------------------------------------------------

// TestUploadsFullChain is FR-90-AC1's wire shape: create (clamped part
// size, URL family) -> urlPart -> 3 part PUTs (two full 5 MiB + one short
// final) with progress assertions -> complete (sha256 gate) -> the landed
// node reads back byte-identical through the content plane.
func TestUploadsFullChain(t *testing.T) {
	h, seam := newUploadsHarness(t)
	seedRepo(t, h, "generic-local")

	// partSizeMB 2 clamps to the S3 floor (5 MiB) — echoed, never guessed.
	code, created := mpuCreate(t, h, adminUser, adminPass,
		`{"repoKey":"generic-local","path":"big/pkg.bin","partSizeMB":2}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d, want 201: %v", code, created)
	}
	if got := created["partSizeBytes"].(float64); int64(got) != 5<<20 {
		t.Fatalf("partSizeBytes = %v, want the 5MiB clamp", got)
	}
	sid := created["sessionId"].(string)
	if sid == "" {
		t.Fatal("empty sessionId")
	}
	for _, k := range []string{"urlPartUri", "partUploadUri", "statusUri", "completeUri", "abortUri"} {
		if s, _ := created[k].(string); s == "" || !strings.Contains(s, sid) {
			t.Fatalf("create body missing a usable %s: %v", k, created)
		}
	}
	if got := seam.begun.Load(); got != 1 {
		t.Fatalf("seam began %d sessions, want 1", got)
	}

	// The list form shows the live session before any byte flows.
	code, raw := mpuStatus(t, h, adminUser, adminPass, "")
	if code != http.StatusOK {
		t.Fatalf("bare status = %d, want 200", code)
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("bare status body not a JSON array: %s", raw)
	}
	if len(list) != 1 || list[0]["sessionId"] != sid {
		t.Fatalf("list form = %s, want exactly the fresh session", raw)
	}

	// urlPart hands out the PUT target with the expected offset.
	resp := h.do(http.MethodGet, "/binflow/api/v1/uploads/urlPart/"+sid+"/2", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("urlPart = %d", resp.StatusCode)
	}
	var pu map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&pu)
	func() { _ = resp.Body.Close() }()
	if int64(pu["offsetBytes"].(float64)) != 5<<20 {
		t.Fatalf("urlPart(2).offsetBytes = %v, want 5MiB", pu["offsetBytes"])
	}
	if u, _ := pu["url"].(string); !strings.Contains(u, "/api/v1/uploads/part/"+sid+"/2") {
		t.Fatalf("urlPart url = %v", pu["url"])
	}

	// Three parts: 5 MiB + 5 MiB + 1 MiB (the short final).
	part1 := bytes.Repeat([]byte{0x11}, 5<<20)
	part2 := bytes.Repeat([]byte{0x22}, 5<<20)
	part3 := bytes.Repeat([]byte{0x33}, 1<<20)
	whole := append(append([]byte{}, part1...), append(part2, part3...)...)
	for i, part := range [][]byte{part1, part2, part3} {
		resp := h.do(http.MethodPut, "/binflow/api/v1/uploads/part/"+sid+"/"+fmt.Sprint(i+1),
			adminUser, adminPass, part, nil)
		body, _ := io.ReadAll(resp.Body)
		func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("part %d PUT = %d: %s", i+1, resp.StatusCode, body)
		}
		var echo map[string]any
		_ = json.Unmarshal(body, &echo)
		wantReceived := int64(len(part1)) * int64(i+1)
		if i == 2 {
			wantReceived = int64(len(whole))
		}
		if int64(echo["receivedBytes"].(float64)) != wantReceived {
			t.Fatalf("part %d echo receivedBytes = %v, want %d", i+1, echo["receivedBytes"], wantReceived)
		}
		// Progress through the status leg after every part (AC1).
		code, raw := mpuStatus(t, h, adminUser, adminPass, sid)
		if code != http.StatusOK {
			t.Fatalf("status after part %d = %d", i+1, code)
		}
		var st map[string]any
		_ = json.Unmarshal(raw, &st)
		if int64(st["receivedBytes"].(float64)) != wantReceived {
			t.Fatalf("status after part %d: receivedBytes = %v, want %d", i+1, st["receivedBytes"], wantReceived)
		}
	}
	// The short part closed the stream: state advanced, no further part.
	_, raw = mpuStatus(t, h, adminUser, adminPass, sid)
	var st map[string]any
	_ = json.Unmarshal(raw, &st)
	if st["state"] != "awaiting-complete" {
		t.Fatalf("state after the short final part = %v, want awaiting-complete", st["state"])
	}
	resp = h.do(http.MethodPut, "/binflow/api/v1/uploads/part/"+sid+"/4", adminUser, adminPass, []byte("x"), nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("part after the short final = %d, want 409", resp.StatusCode)
	}

	// Complete: sha256 gate, node landing, honest echoes.
	sum := sha256.Sum256(whole)
	resp = h.do(http.MethodPost, "/binflow/api/v1/uploads/complete/"+sid, adminUser, adminPass,
		[]byte(`{"sha256":"`+hex.EncodeToString(sum[:])+`"}`), nil)
	body, _ := io.ReadAll(resp.Body)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("complete = %d: %s", resp.StatusCode, body)
	}
	var done map[string]any
	if err := json.Unmarshal(body, &done); err != nil {
		t.Fatalf("complete body not JSON: %s", body)
	}
	if int64(done["size"].(float64)) != int64(len(whole)) {
		t.Fatalf("complete size = %v, want %d", done["size"], len(whole))
	}
	if !strings.Contains(done["downloadUri"].(string), "/binflow/generic-local/big/pkg.bin") {
		t.Fatalf("downloadUri = %v", done["downloadUri"])
	}

	// The session is gone: status 404 (the terminal-verb posture).
	if code, _ := mpuStatus(t, h, adminUser, adminPass, sid); code != http.StatusNotFound {
		t.Fatalf("status after complete = %d, want 404", code)
	}

	// The landed node reads back byte-identical through the content plane.
	resp = h.do(http.MethodGet, "/binflow/generic-local/big/pkg.bin", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("artifact GET = %d", resp.StatusCode)
	}
	if !bytes.Equal(got, whole) {
		t.Fatalf("artifact bytes differ (%d vs %d)", len(got), len(whole))
	}
}

// TestUploadsAbortDiscards: abort mid-upload removes the session (status
// 404, second abort 404) and no node ever lands at the target path.
func TestUploadsAbortDiscards(t *testing.T) {
	h, _ := newUploadsHarness(t)
	seedRepo(t, h, "generic-local")

	code, created := mpuCreate(t, h, adminUser, adminPass,
		`{"repoKey":"generic-local","path":"big/gone.bin","partSizeMB":5}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d", code)
	}
	sid := created["sessionId"].(string)

	resp := h.do(http.MethodPut, "/binflow/api/v1/uploads/part/"+sid+"/1", adminUser, adminPass,
		bytes.Repeat([]byte{9}, 5<<20), nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("part 1 = %d", resp.StatusCode)
	}

	resp = h.do(http.MethodPost, "/binflow/api/v1/uploads/abort/"+sid, adminUser, adminPass, nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("abort = %d, want 204", resp.StatusCode)
	}
	if code, _ := mpuStatus(t, h, adminUser, adminPass, sid); code != http.StatusNotFound {
		t.Fatalf("status after abort = %d, want 404", code)
	}
	resp = h.do(http.MethodPost, "/binflow/api/v1/uploads/abort/"+sid, adminUser, adminPass, nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("second abort = %d, want 404", resp.StatusCode)
	}
	resp = h.do(http.MethodGet, "/binflow/generic-local/big/gone.bin", adminUser, adminPass, nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("aborted artifact GET = %d, want 404 (blob invisible)", resp.StatusCode)
	}
}

// TestUploadsChecksumMismatch: a well-formed but wrong sha256 answers 409
// (repo-semantics section 5's client-checksum posture) and consumes the
// session.
func TestUploadsChecksumMismatch(t *testing.T) {
	h, _ := newUploadsHarness(t)
	seedRepo(t, h, "generic-local")

	_, created := mpuCreate(t, h, adminUser, adminPass,
		`{"repoKey":"generic-local","path":"big/bad.bin","partSizeMB":5}`)
	sid := created["sessionId"].(string)
	resp := h.do(http.MethodPut, "/binflow/api/v1/uploads/part/"+sid+"/1", adminUser, adminPass,
		bytes.Repeat([]byte{7}, 1<<20), nil) // short part closes the stream
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("short final part = %d", resp.StatusCode)
	}

	wrong := strings.Repeat("0", 64)
	resp = h.do(http.MethodPost, "/binflow/api/v1/uploads/complete/"+sid, adminUser, adminPass,
		[]byte(`{"sha256":"`+wrong+`"}`), nil)
	body, _ := io.ReadAll(resp.Body)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("complete with wrong sha256 = %d: %s", resp.StatusCode, body)
	}
	if code, _ := mpuStatus(t, h, adminUser, adminPass, sid); code != http.StatusNotFound {
		t.Fatalf("status after failed complete = %d, want 404 (session consumed)", code)
	}
	resp = h.do(http.MethodGet, "/binflow/generic-local/big/bad.bin", adminUser, adminPass, nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("mismatched artifact GET = %d, want 404 (nothing landed)", resp.StatusCode)
	}
}

// TestUploadsGuards walks the refusal ladder: ordering, oversize, write
// door, path validation, repo typing, checksum shape.
func TestUploadsGuards(t *testing.T) {
	h, _ := newUploadsHarness(t, [2]string{"alice", "alice-pw"})
	seedRepo(t, h, "generic-local")

	// A plain user without `w` is refused at the door.
	code, body := mpuCreate(t, h, "alice", "alice-pw",
		`{"repoKey":"generic-local","path":"big/x.bin"}`)
	if code != http.StatusForbidden {
		t.Fatalf("create without w = %d (%v), want 403", code, body)
	}
	// The grant opens it (the same Authorizer the content plane consults).
	grant(t, h, "mpu-grant", "generic-local", "big/**", "alice", false, true, false)
	code, body = mpuCreate(t, h, "alice", "alice-pw",
		`{"repoKey":"generic-local","path":"big/x.bin","partSizeMB":5}`)
	if code != http.StatusCreated {
		t.Fatalf("create with w = %d (%v), want 201", code, body)
	}
	sid := body["sessionId"].(string)

	// Out-of-order part.
	resp := h.do(http.MethodPut, "/binflow/api/v1/uploads/part/"+sid+"/2", "alice", "alice-pw",
		bytes.Repeat([]byte{1}, 5<<20), nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("out-of-order part = %d, want 409", resp.StatusCode)
	}
	// Oversize part (pre-stream gate).
	resp = h.do(http.MethodPut, "/binflow/api/v1/uploads/part/"+sid+"/1", "alice", "alice-pw",
		bytes.Repeat([]byte{1}, (5<<20)+1), nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversize part = %d, want 400", resp.StatusCode)
	}
	// Unknown session.
	resp = h.do(http.MethodPut, "/binflow/api/v1/uploads/part/nope/1", "alice", "alice-pw",
		[]byte("x"), nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown session part = %d, want 404", resp.StatusCode)
	}
	// A session created by alice is not drivable by another authenticated
	// principal without the write door (the id is a capability, not an
	// authorization) — carol exists (seeded below via the users seam is
	// overkill; the anonymous arm suffices): anonymous meets the route's
	// 401, and alice — the holder — still reads it.
	resp = h.do(http.MethodGet, "/binflow/api/v1/uploads/status/"+sid, "", "", nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", resp.StatusCode)
	}
	resp = h.do(http.MethodGet, "/binflow/api/v1/uploads/status/"+sid, "alice", "alice-pw", nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder status = %d, want 200", resp.StatusCode)
	}

	// Path validation family.
	for path, want := range map[string]int{
		"":                                http.StatusBadRequest,
		"/abs/path.bin":                   http.StatusBadRequest,
		"dir/":                            http.StatusBadRequest,
		"../escape.bin":                   http.StatusBadRequest,
		"dir/../escape.bin":               http.StatusBadRequest,
		"file.bin;k=v":                    http.StatusBadRequest,
		strings.Repeat("a", 513) + ".bin": http.StatusBadRequest,
	} {
		code, body := mpuCreate(t, h, adminUser, adminPass,
			`{"repoKey":"generic-local","path":`+mustJSON(path)+`}`)
		if code != want {
			t.Fatalf("create path %q = %d (%v), want %d", path, code, body, want)
		}
	}

	// Unknown repo / wrong typing.
	code, _ = mpuCreate(t, h, adminUser, adminPass, `{"repoKey":"no-such-repo","path":"a.bin"}`)
	if code != http.StatusNotFound {
		t.Fatalf("create on unknown repo = %d, want 404", code)
	}
	code, _ = mpuCreate(t, h, adminUser, adminPass, `{"repoKey":"generic-local"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("create without path = %d, want 400", code)
	}
	if err := createTypedRepo(t, h, "docker-local", "docker"); err == nil {
		code, _ = mpuCreate(t, h, adminUser, adminPass,
			`{"repoKey":"docker-local","path":"a.bin"}`)
		if code != http.StatusBadRequest {
			t.Fatalf("create on docker repo = %d, want 400 (protocol layouts take their own uploads)", code)
		}
	}

	// partSizeMB bounds.
	code, _ = mpuCreate(t, h, adminUser, adminPass,
		`{"repoKey":"generic-local","path":"big/y.bin","partSizeMB":-1}`)
	if code != http.StatusBadRequest {
		t.Fatalf("negative partSizeMB = %d, want 400", code)
	}
	code, _ = mpuCreate(t, h, adminUser, adminPass,
		`{"repoKey":"generic-local","path":"big/y.bin","partSizeMB":99999}`)
	if code != http.StatusBadRequest {
		t.Fatalf("oversized partSizeMB = %d, want 400", code)
	}

	// Complete's checksum shape: missing sha256, wrong width.
	resp = h.do(http.MethodPost, "/binflow/api/v1/uploads/complete/"+sid, "alice", "alice-pw",
		[]byte(`{"sha1":"`+strings.Repeat("a", 40)+`"}`), nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("complete without sha256 = %d, want 400", resp.StatusCode)
	}
}

// TestUploadsConfigRePart: config re-parts a byte-less session (the id —
// the capability every URL carries — survives the swap) and refuses once
// bytes have flowed.
func TestUploadsConfigRePart(t *testing.T) {
	h, seam := newUploadsHarness(t)
	seedRepo(t, h, "generic-local")

	_, created := mpuCreate(t, h, adminUser, adminPass,
		`{"repoKey":"generic-local","path":"big/repart.bin","partSizeMB":5}`)
	sid := created["sessionId"].(string)
	before := seam.begun.Load()

	resp := h.do(http.MethodPost, "/binflow/api/v1/uploads/config", adminUser, adminPass,
		[]byte(`{"sessionId":"`+sid+`","partSizeMB":16}`), nil)
	body, _ := io.ReadAll(resp.Body)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("config = %d: %s", resp.StatusCode, body)
	}
	var echo map[string]any
	_ = json.Unmarshal(body, &echo)
	if int64(echo["partSizeBytes"].(float64)) != 16<<20 {
		t.Fatalf("config echo partSizeBytes = %v, want 16MiB", echo["partSizeBytes"])
	}
	if echo["sessionId"] != sid {
		t.Fatalf("config changed the session id: %v", echo["sessionId"])
	}
	if got := seam.begun.Load(); got != before+1 {
		t.Fatalf("config began %d sessions total, want %d (the swap)", got, before+1)
	}

	// After a part: fixed.
	resp = h.do(http.MethodPut, "/binflow/api/v1/uploads/part/"+sid+"/1", adminUser, adminPass,
		bytes.Repeat([]byte{3}, 1<<20), nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("short part after config = %d", resp.StatusCode)
	}
	resp = h.do(http.MethodPost, "/binflow/api/v1/uploads/config", adminUser, adminPass,
		[]byte(`{"sessionId":"`+sid+`","partSizeMB":32}`), nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("config after bytes = %d, want 409", resp.StatusCode)
	}
}

// TestUploadsURLPartAndStatusUnknown: unknown ids and grammar misses.
func TestUploadsURLPartAndStatusUnknown(t *testing.T) {
	h, _ := newUploadsHarness(t)
	seedRepo(t, h, "generic-local")

	resp := h.do(http.MethodGet, "/binflow/api/v1/uploads/urlPart/nope/1", adminUser, adminPass, nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("urlPart unknown session = %d, want 404", resp.StatusCode)
	}
	resp = h.do(http.MethodGet, "/binflow/api/v1/uploads/urlPart/nope/0", adminUser, adminPass, nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("urlPart part 0 = %d, want 400", resp.StatusCode)
	}
	// Grammar misses fall to the E-26 404.
	for _, p := range []string{
		"/binflow/api/v1/uploads/part/nope/1/2",
		"/binflow/api/v1/uploads/part/nope/abc",
		"/binflow/api/v1/uploads/status/a/b",
		"/binflow/api/v1/uploads/nosuch",
	} {
		resp := h.do(http.MethodGet, p, adminUser, adminPass, nil, nil)
		func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want the E-26 404", p, resp.StatusCode)
		}
	}
	// Verbs the plane does not define.
	resp = h.do(http.MethodDelete, "/binflow/api/v1/uploads/abort/x", adminUser, adminPass, nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE abort = %d, want 404 (the plane defines POST)", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// review-round tests (T-289 review B2/B3/B4 + torn part)
// ---------------------------------------------------------------------------

// TestUploadsPartLengthRequiredKeepsBody (B2): a part PUT without
// Content-Length (chunked transfer) answers 411 AND the errors[] envelope
// actually reaches the wire — the old arm declared Content-Length: 0 and
// the server dropped the body it had just written.
func TestUploadsPartLengthRequiredKeepsBody(t *testing.T) {
	h, _ := newUploadsHarness(t)
	seedRepo(t, h, "generic-local")
	_, created := mpuCreate(t, h, adminUser, adminPass,
		`{"repoKey":"generic-local","path":"big/nolen.bin","partSizeMB":5}`)
	sid := created["sessionId"].(string)

	// io.NopCloser hides the *bytes.Reader from http.NewRequest, so no
	// Content-Length is derived and the request goes out chunked.
	req, err := http.NewRequest(http.MethodPut,
		h.srv.URL+"/binflow/api/v1/uploads/part/"+sid+"/1",
		io.NopCloser(bytes.NewReader(bytes.Repeat([]byte{1}, 64))))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.SetBasicAuth(adminUser, adminPass)
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
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("411 Content-Type = %q, want application/json", resp.Header.Get("Content-Type"))
	}
}

// TestUploadsPartSizeMBBounds (B3): the megabyte-domain bound closes the
// shift-overflow hole — partSizeMB >= 2^43 used to wrap int64 negative
// under << 20 and sail through the below-floor clamp as a 201.
func TestUploadsPartSizeMBBounds(t *testing.T) {
	h, _ := newUploadsHarness(t)
	seedRepo(t, h, "generic-local")

	leg := func(mb int64) (int, map[string]any) {
		return mpuCreate(t, h, adminUser, adminPass,
			fmt.Sprintf(`{"repoKey":"generic-local","path":"big/psz-%d.bin","partSizeMB":%d}`, mb, mb))
	}
	for _, mb := range []int64{1 << 43, 1 << 44, 5120 + 1} {
		code, body := leg(mb)
		if code != http.StatusBadRequest {
			t.Fatalf("partSizeMB=%d = %d (%v), want 400 (the overflow/bound gate)", mb, code, body)
		}
	}
	// The boundary itself is legal: 5120 MB = exactly 5 GiB.
	code, body := leg(5120)
	if code != http.StatusCreated {
		t.Fatalf("partSizeMB=5120 = %d (%v), want 201", code, body)
	}
	if got := int64(body["partSizeBytes"].(float64)); got != int64(5120)<<20 {
		t.Fatalf("partSizeBytes = %d, want %d", got, int64(5120)<<20)
	}
}

// TestUploadsStatusListFilteredByWriteGate (B4): the bare status arm
// filters per session by the same write door as the single-session arm —
// a plain user sees only sessions targeting repositories it holds `w` on,
// never the instance's whole in-flight set.
func TestUploadsStatusListFilteredByWriteGate(t *testing.T) {
	h, _ := newUploadsHarness(t, [2]string{"alice", "alice-pw"})
	seedRepo(t, h, "repo-a")
	seedRepo(t, h, "repo-b")
	grant(t, h, "mpu-a", "repo-a", "**", "alice", false, true, false)

	// alice's session on repo-a, admin's on repo-b.
	code, mine := mpuCreate(t, h, "alice", "alice-pw", `{"repoKey":"repo-a","path":"x.bin"}`)
	if code != http.StatusCreated {
		t.Fatalf("alice create on repo-a = %d (%v)", code, mine)
	}
	code, other := mpuCreate(t, h, adminUser, adminPass, `{"repoKey":"repo-b","path":"y.bin"}`)
	if code != http.StatusCreated {
		t.Fatalf("admin create on repo-b = %d", code)
	}

	listFor := func(user, pass string) []string {
		t.Helper()
		code, raw := mpuStatus(t, h, user, pass, "")
		if code != http.StatusOK {
			t.Fatalf("bare status for %s = %d", user, code)
		}
		var list []map[string]any
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatalf("bare status body: %s", raw)
		}
		ids := make([]string, 0, len(list))
		for _, s := range list {
			ids = append(ids, s["sessionId"].(string))
		}
		return ids
	}
	aliceIDs := listFor("alice", "alice-pw")
	if len(aliceIDs) != 1 || aliceIDs[0] != mine["sessionId"] {
		t.Fatalf("alice's list = %v, want exactly her own session %v", aliceIDs, mine["sessionId"])
	}
	adminIDs := listFor(adminUser, adminPass)
	if len(adminIDs) != 2 {
		t.Fatalf("admin's list = %v, want both sessions", adminIDs)
	}
	// The single-session arm stays consistent: alice cannot address the
	// admin's session id directly.
	resp := h.do(http.MethodGet, "/binflow/api/v1/uploads/status/"+other["sessionId"].(string),
		"alice", "alice-pw", nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("alice status on repo-b session = %d, want 403", resp.StatusCode)
	}
}

// TestUploadsTornPartFailsSession (review B1 test debt): a part that
// DECLARES more Content-Length than it delivers must never answer 2xx —
// the session ends observable-failed with its honest offset, and nothing
// lands. Raw TCP because the Go client refuses to send a lying
// Content-Length itself.
func TestUploadsTornPartFailsSession(t *testing.T) {
	h, _ := newUploadsHarness(t)
	seedRepo(t, h, "generic-local")
	_, created := mpuCreate(t, h, adminUser, adminPass,
		`{"repoKey":"generic-local","path":"big/torn.bin","partSizeMB":5}`)
	sid := created["sessionId"].(string)

	conn, err := net.Dial("tcp", h.srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	auth := base64.StdEncoding.EncodeToString([]byte(adminUser + ":" + adminPass))
	req := "PUT /binflow/api/v1/uploads/part/" + sid + "/1 HTTP/1.1\r\n" +
		"Host: " + h.srv.Listener.Addr().String() + "\r\n" +
		"Authorization: Basic " + auth + "\r\n" +
		"Content-Type: application/octet-stream\r\n" +
		"Content-Length: 1048576\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write request head: %v", err)
	}
	if _, err := conn.Write(bytes.Repeat([]byte{9}, 4096)); err != nil {
		t.Fatalf("write torn body: %v", err)
	}
	// Half-close: the server now sees 4096 of the declared 1048576 bytes,
	// then EOF. The response (or the connection's end) must follow.
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

	// The session is observably failed with its honest offset; nothing lands.
	code, raw2 := mpuStatus(t, h, adminUser, adminPass, sid)
	if code != http.StatusOK {
		t.Fatalf("status after torn part = %d, want 200 (failure observable)", code)
	}
	var st map[string]any
	_ = json.Unmarshal(raw2, &st)
	if st["state"] != "failed" {
		t.Fatalf("state after torn part = %v, want %q", st["state"], "failed")
	}
	if got := int64(st["receivedBytes"].(float64)); got > 4096 {
		t.Fatalf("receivedBytes after torn part = %d, want the honest partial count (<= 4096)", got)
	}
	resp := h.do(http.MethodGet, "/binflow/generic-local/big/torn.bin", adminUser, adminPass, nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("torn artifact GET = %d, want 404", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// mustJSON marshals v (test-only convenience for building bodies).
func mustJSON(v string) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
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
