package httpapi_test

// T-323R tests: the /api/v1/uploads plane's restart visibility. The crash
// model is server replacement — a SECOND full stack over a seam whose
// persisted rows survive, exactly what a kill -9 leaves behind: the engine's
// upload_sessions row and the server-side upload state survive, the plane's
// in-process registry does not. The seam fake carries the caller-blob rows
// the way the real S3Engine does (verbatim both ways) and relays each
// Commit into the CURRENT stack's real engine, so the complete leg's
// PutLandedBlob finds the blob exactly as in production, where the seam and
// the service share one engine instance.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/storage"
)

// fakeMPUContextRow is one persisted session row: the engine session's
// durable bytes plus the caller blob the context begin wrote.
type fakeMPUContextRow struct {
	buf    []byte
	caller []byte
	done   bool
}

// fakeMPUContextSeam stands in for *storage.S3Engine on BOTH stacks: it
// implements MultipartUploads and MultipartUploadContexts over rows that
// outlive any one harness, and its Commit relays into the current stack's
// engine (setEngine re-points it when the "restarted" stack is built).
type fakeMPUContextSeam struct {
	mu      sync.Mutex
	eng     storage.Engine
	rows    map[string]*fakeMPUContextRow
	begins  atomic.Int64
	resumes atomic.Int64
}

func newFakeMPUContextSeam() *fakeMPUContextSeam {
	return &fakeMPUContextSeam{rows: map[string]*fakeMPUContextRow{}}
}

// setEngine re-points the commit relay at the restarted stack's engine.
func (f *fakeMPUContextSeam) setEngine(eng storage.Engine) {
	f.mu.Lock()
	f.eng = eng
	f.mu.Unlock()
}

// lockRow fetches one row under the seam lock; ok=false is ErrSessionNotFound.
func (f *fakeMPUContextSeam) lockRow(id string) (*fakeMPUContextRow, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[id]
	if !ok || row.done {
		return nil, false
	}
	return row, true
}

func (f *fakeMPUContextSeam) BeginMultipartSessionContext(_ context.Context, _ int64, caller []byte) (storage.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(caller) == 0 {
		return nil, fmt.Errorf("fake seam: caller context required")
	}
	f.begins.Add(1)
	id := fmt.Sprintf("ctx-mpu-%d", f.begins.Load())
	f.rows[id] = &fakeMPUContextRow{caller: append([]byte(nil), caller...)}
	return &fakeMPUContextSession{seam: f, id: id}, nil
}

func (f *fakeMPUContextSeam) BeginMultipartSession(_ context.Context, _ int64) (storage.Session, error) {
	// The plain begin writes no caller blob — the pre-T-323R row shape.
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begins.Add(1)
	id := fmt.Sprintf("plain-mpu-%d", f.begins.Load())
	f.rows[id] = &fakeMPUContextRow{}
	return &fakeMPUContextSession{seam: f, id: id}, nil
}

func (f *fakeMPUContextSeam) ResumeSessionContext(_ context.Context, id string) (storage.Session, []byte, error) {
	f.mu.Lock()
	row, ok := f.rows[id]
	var caller []byte
	if ok {
		caller = row.caller
	}
	f.mu.Unlock()
	f.resumes.Add(1)
	if !ok || row.done {
		return nil, nil, fmt.Errorf("fake seam: resume %s: %w", id, storage.ErrSessionNotFound)
	}
	if len(caller) == 0 {
		// The engine contract: the context lane never adopts a caller-less
		// row, and the refusal must leave the row untouched.
		return nil, nil, fmt.Errorf("fake seam: resume %s: %w: no caller context", id, storage.ErrSessionNotFound)
	}
	return &fakeMPUContextSession{seam: f, id: id}, caller, nil
}

// fakeMPUContextSession is the seam's session handle: a thin view over the
// row's durable buffer (what the real engine's Offset reports after a
// resume: the last flushed boundary).
type fakeMPUContextSession struct {
	seam *fakeMPUContextSeam
	id   string
}

func (s *fakeMPUContextSession) ID() string { return s.id }

func (s *fakeMPUContextSession) Offset() int64 {
	row, ok := s.seam.lockRow(s.id)
	if !ok {
		return 0
	}
	return int64(len(row.buf))
}

func (s *fakeMPUContextSession) Append(_ context.Context, r io.Reader) (int64, error) {
	n, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	s.seam.mu.Lock()
	defer s.seam.mu.Unlock()
	row, ok := s.seam.rows[s.id]
	if !ok || row.done {
		return 0, fmt.Errorf("session %s already finalized", s.id)
	}
	row.buf = append(row.buf, n...)
	return int64(len(row.buf)), nil
}

func (s *fakeMPUContextSession) Commit(ctx context.Context, expect storage.BlobRef) (storage.BlobRef, error) {
	s.seam.mu.Lock()
	row, ok := s.seam.rows[s.id]
	if !ok || row.done {
		s.seam.mu.Unlock()
		return storage.BlobRef{}, fmt.Errorf("session %s already finalized", s.id)
	}
	row.done = true
	payload := row.buf
	delete(s.seam.rows, s.id)
	eng := s.seam.eng
	s.seam.mu.Unlock()

	inner, err := eng.BeginSession(ctx)
	if err != nil {
		return storage.BlobRef{}, err
	}
	if _, err := inner.Append(ctx, bytes.NewReader(payload)); err != nil {
		_ = inner.Abort(ctx)
		return storage.BlobRef{}, err
	}
	return inner.Commit(ctx, expect)
}

func (s *fakeMPUContextSession) Abort(_ context.Context) error {
	s.seam.mu.Lock()
	defer s.seam.mu.Unlock()
	if row, ok := s.seam.rows[s.id]; ok {
		row.done = true
		delete(s.seam.rows, s.id)
	}
	return nil
}

// newRestartStacks builds TWO full stacks sharing one context seam: the
// first plays the pre-crash process, the second the restart (a fresh Server
// means a fresh in-process registry, exactly the kill -9 shape).
func newRestartStacks(t *testing.T) (a, b *harness, seam *fakeMPUContextSeam) {
	t.Helper()
	seam = newFakeMPUContextSeam()
	wire := func(d *httpapi.Deps) {
		eng, ok := d.GC.(storage.Engine)
		if !ok {
			t.Fatal("stack GC seam is not a storage.Engine")
		}
		seam.setEngine(eng)
		d.Uploads = seam
	}
	a = newHarnessFull(t, nil, nil, nil, wire, nil)
	b = newHarnessFull(t, nil, nil, nil, wire, nil)
	seedRepo(t, a, "generic-local")
	seedRepo(t, b, "generic-local")
	return a, b, seam
}

// TestUploadsRestartResumeVisibilityAndComplete is T-323R's acceptance leg:
// create + part 1 on the first process, kill -9 (stack swap), then the
// restarted process must SEE the session (200, persisted coordinates,
// derived accounting), accept the remaining parts, complete against the
// whole-content sha256 and land a byte-identical node.
func TestUploadsRestartResumeVisibilityAndComplete(t *testing.T) {
	h1, h2, seam := newRestartStacks(t)

	code, created := mpuCreate(t, h1, adminUser, adminPass,
		`{"repoKey":"generic-local","path":"big/restart.bin","partSizeMB":5,"mimeType":"application/x-test"}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d: %v", code, created)
	}
	sid := created["sessionId"].(string)

	part1 := bytes.Repeat([]byte{0x11}, 5<<20)
	part2 := bytes.Repeat([]byte{0x22}, 5<<20)
	part3 := bytes.Repeat([]byte{0x33}, 1<<20)
	whole := append(append([]byte{}, part1...), append(part2, part3...)...)

	resp := h1.do(http.MethodPut, "/binflow/api/v1/uploads/part/"+sid+"/1", adminUser, adminPass, part1, nil)
	body, _ := io.ReadAll(resp.Body)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("part 1 (pre-crash) = %d: %s", resp.StatusCode, body)
	}

	// The restart: from here every request rides the SECOND stack — a fresh
	// process whose registry has never seen the id. The seam's row (the
	// engine's upload_sessions row + caller blob in production) survived.
	code, raw := mpuStatus(t, h2, adminUser, adminPass, sid)
	if code != http.StatusOK {
		t.Fatalf("post-restart status = %d, want 200 (restart resume): %s", code, raw)
	}
	var st map[string]any
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("status body not JSON: %s", raw)
	}
	for k, want := range map[string]any{
		"repoKey":        "generic-local",
		"path":           "big/restart.bin",
		"state":          "active",
		"receivedBytes":  float64(5 << 20),
		"partsReceived":  float64(1),
		"partSizeBytes":  float64(5 << 20),
		"nextPartNumber": float64(2),
		"createdBy":      adminUser,
	} {
		if st[k] != want {
			t.Fatalf("post-restart status %s = %v, want %v (body: %s)", k, st[k], want, raw)
		}
	}
	if seam.resumes.Load() != 1 {
		t.Fatalf("seam resumes = %d, want exactly 1 (the lazy rebuild)", seam.resumes.Load())
	}

	// The remaining parts ride the resumed session and the complete gate
	// verifies the WHOLE content (the engine's rebuilt-commit readback in
	// production; the seam relays the full buffer here).
	for i, part := range [][]byte{part2, part3} {
		resp := h2.do(http.MethodPut, "/binflow/api/v1/uploads/part/"+sid+"/"+fmt.Sprint(i+2),
			adminUser, adminPass, part, nil)
		body, _ := io.ReadAll(resp.Body)
		func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("post-restart part %d PUT = %d: %s", i+2, resp.StatusCode, body)
		}
	}
	code, raw = mpuStatus(t, h2, adminUser, adminPass, sid)
	if code != http.StatusOK {
		t.Fatalf("status before complete = %d", code)
	}
	_ = json.Unmarshal(raw, &st)
	if st["state"] != "awaiting-complete" {
		t.Fatalf("state after the short final part = %v, want awaiting-complete: %s", st["state"], raw)
	}

	sum := sha256.Sum256(whole)
	resp = h2.do(http.MethodPost, "/binflow/api/v1/uploads/complete/"+sid, adminUser, adminPass,
		[]byte(`{"sha256":"`+hex.EncodeToString(sum[:])+`"}`), nil)
	body, _ = io.ReadAll(resp.Body)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("post-restart complete = %d: %s", resp.StatusCode, body)
	}
	var done map[string]any
	_ = json.Unmarshal(body, &done)
	if int64(done["size"].(float64)) != int64(len(whole)) {
		t.Fatalf("complete size = %v, want %d", done["size"], len(whole))
	}
	if !strings.Contains(done["downloadUri"].(string), "/binflow/generic-local/big/restart.bin") {
		t.Fatalf("downloadUri = %v (the PERSISTED coordinates must drive the landing)", done["downloadUri"])
	}

	// Terminal posture + byte-for-byte readback through the content plane.
	if code, _ := mpuStatus(t, h2, adminUser, adminPass, sid); code != http.StatusNotFound {
		t.Fatalf("status after complete = %d, want 404", code)
	}
	resp = h2.do(http.MethodGet, "/binflow/generic-local/big/restart.bin", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("artifact GET = %d", resp.StatusCode)
	}
	if !bytes.Equal(got, whole) {
		t.Fatalf("artifact bytes differ (%d vs %d)", len(got), len(whole))
	}
}

// TestUploadsRestartResumeSingleFlight: concurrent first requests for the
// same restarted id must funnel into ONE engine resume — the engine
// resolves same-id concurrent resumes last-writer-wins, so two flyers would
// leak that verdict as a 5xx.
func TestUploadsRestartResumeSingleFlight(t *testing.T) {
	h1, h2, seam := newRestartStacks(t)

	code, created := mpuCreate(t, h1, adminUser, adminPass,
		`{"repoKey":"generic-local","path":"big/race.bin","partSizeMB":5}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d: %v", code, created)
	}
	sid := created["sessionId"].(string)
	resp := h1.do(http.MethodPut, "/binflow/api/v1/uploads/part/"+sid+"/1", adminUser, adminPass,
		bytes.Repeat([]byte{0x44}, 5<<20), nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("part 1 = %d", resp.StatusCode)
	}

	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code, raw := mpuStatus(t, h2, adminUser, adminPass, sid)
			if code != http.StatusOK {
				t.Errorf("concurrent status %d = %d: %s", i, code, raw)
			}
		}(i)
	}
	wg.Wait()
	if got := seam.resumes.Load(); got != 1 {
		t.Fatalf("engine resumes = %d, want exactly 1 (the per-id funnel)", got)
	}
}

// TestUploadsRestartWithoutContextSeamKeeps404: a seam without the context
// pair (the pre-T-323R shape) keeps the process-only posture — a restart
// forgets the session and status answers the plain 404.
func TestUploadsRestartWithoutContextSeamKeeps404(t *testing.T) {
	var seam *fakeMPUSeam
	h1 := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		seam = &fakeMPUSeam{eng: d.GC.(storage.Engine)}
		d.Uploads = seam
	}, nil)
	seedRepo(t, h1, "generic-local")
	code, created := mpuCreate(t, h1, adminUser, adminPass,
		`{"repoKey":"generic-local","path":"big/old.bin","partSizeMB":5}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d: %v", code, created)
	}
	sid := created["sessionId"].(string)

	h2 := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		d.Uploads = &fakeMPUSeam{eng: d.GC.(storage.Engine)}
	}, nil)
	if code, _ := mpuStatus(t, h2, adminUser, adminPass, sid); code != http.StatusNotFound {
		t.Fatalf("post-restart status = %d, want the process-only 404", code)
	}
}

// TestUploadsResumeWriteGateUsesPersistedCoordinates: the write door on a
// resumed session judges against the PERSISTED repo/path — a caller without
// the grant collects the 403 (the same verdict the in-process posture
// renders), and a privileged caller reaching the same id afterwards still
// succeeds: the refused verb left the re-materialized session usable.
func TestUploadsResumeWriteGateUsesPersistedCoordinates(t *testing.T) {
	h1, _, seam := newRestartStacks(t)
	code, created := mpuCreate(t, h1, adminUser, adminPass,
		`{"repoKey":"generic-local","path":"big/gated.bin","partSizeMB":5}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d: %v", code, created)
	}
	sid := created["sessionId"].(string)

	// The restarted stack carries an unprivileged user; the seam and its
	// rows are the ones the crash left behind.
	h2 := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		seam.setEngine(d.GC.(storage.Engine))
		d.Uploads = seam
	}, [][2]string{{"mallory", "mallory-pass"}})
	seedRepo(t, h2, "generic-local")

	resp := h2.do(http.MethodGet, "/binflow/api/v1/uploads/status/"+sid, "mallory", "mallory-pass", nil, nil)
	raw, _ := io.ReadAll(resp.Body)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unprivileged post-restart status = %d, want 403 (the persisted coordinates drive the door): %s",
			resp.StatusCode, raw)
	}
	// The refused verb published nothing harmful: the privileged caller's
	// next request resolves the same session and reads the durable offset.
	if code, raw := mpuStatus(t, h2, adminUser, adminPass, sid); code != http.StatusOK {
		t.Fatalf("admin post-refusal status = %d: %s", code, raw)
	}
}
