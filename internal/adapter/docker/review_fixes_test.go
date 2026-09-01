package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// Review B1/B2/B4 regression suite (T-38 double review, REQUEST_CHANGES).

// ---- B1: concurrent same-UUID operations serialize ----

// TestConcurrentSameUUIDPatchSerialized (review B1): concurrent PATCHes on
// ONE session UUID must serialize — exactly one request wins the anchor at
// each offset, the others observe 416 with the then-authoritative Range,
// and the session's received never regresses. Runs under -race in CI; the
// pre-fix code tripped the race detector here (received/poisoned unlocked).
func TestConcurrentSameUUIDPatchSerialized(t *testing.T) {
	bh := newBlobHarness(t)
	loc, _ := bh.startUpload("team1/app")

	const workers = 6
	const chunk = 4096
	body := strings.Repeat("z", chunk)

	var accepted, rejected atomic.Int64
	var ranges []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPatch, loc, strings.NewReader(body))
			// Anchored at 0: only the winner's anchor matches the server's
			// initial received=0; every later arrival must 416.
			req.Header.Set("Content-Range", fmt.Sprintf("0-%d", chunk-1))
			req = req.WithContext(adapter.WithPrincipal(context.Background(),
				&auth.Principal{Name: "admin", Admin: true}))
			rec := httptest.NewRecorder()
			bh.h.ServeHTTP(rec, req)
			resp := rec.Result()
			drainBody(t, resp)
			switch resp.StatusCode {
			case http.StatusAccepted:
				accepted.Add(1)
				mu.Lock()
				ranges = append(ranges, resp.Header.Get("Range"))
				mu.Unlock()
			case http.StatusRequestedRangeNotSatisfiable:
				rejected.Add(1)
			default:
				t.Errorf("PATCH status = %d, want 202 or 416", resp.StatusCode)
			}
		}()
	}
	wg.Wait()

	if got := accepted.Load(); got != 1 {
		t.Fatalf("accepted PATCHes = %d, want exactly 1 (the anchor owner)", got)
	}
	if got := rejected.Load(); got != workers-1 {
		t.Fatalf("rejected PATCHes = %d, want %d", got, workers-1)
	}
	// The one accepted Range names exactly one chunk's worth of bytes.
	mu.Lock()
	defer mu.Unlock()
	for _, rg := range ranges {
		if rg != fmt.Sprintf("0-%d", chunk-1) {
			t.Fatalf("accepted Range = %q, want 0-%d", rg, chunk-1)
		}
	}

	// The session is intact and correctly anchored for the recovery chunk.
	resp := bh.serve(http.MethodPatch, loc, strings.NewReader("tail"),
		map[string]string{"Content-Range": fmt.Sprintf("%d-%d", chunk, chunk+3)})
	drainBody(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("recovery PATCH status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Range"); got != fmt.Sprintf("0-%d", chunk+3) {
		t.Fatalf("recovery Range = %q, want 0-%d", got, chunk+3)
	}
}

// TestConcurrentMixedVerbsSerialize (review B1): PATCH racing the PUT
// finalize and the offset GET on the same UUID. Whatever the interleaving,
// the invariants are: at most one finalize succeeds, no verb observes a
// torn state (the race detector polices the data access), and the registry
// ends empty.
func TestConcurrentMixedVerbsSerialize(t *testing.T) {
	bh := newBlobHarness(t)
	loc, _ := bh.startUpload("team1/app")

	content := []byte("mixed-verb serialization probe")
	dgst := "sha256:" + sha256Hex(content)
	id := strings.TrimPrefix(loc, "/v2/team1/app/blobs/uploads/")

	var wg sync.WaitGroup
	do := func(method, target string, body io.Reader) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(method, target, body)
			req = req.WithContext(adapter.WithPrincipal(context.Background(),
				&auth.Principal{Name: "admin", Admin: true}))
			rec := httptest.NewRecorder()
			bh.h.ServeHTTP(rec, req)
			resp := rec.Result()
			drainBody(t, resp)
			if resp.StatusCode >= 500 {
				t.Errorf("%s returned %d", method, resp.StatusCode)
			}
		}()
	}
	do(http.MethodGet, loc, nil)
	do(http.MethodGet, loc, nil)
	do(http.MethodPatch, loc, strings.NewReader(string(content)))
	do(http.MethodPut, loc+"?digest="+dgst, nil)
	do(http.MethodPut, loc+"?digest="+dgst, nil)
	wg.Wait()

	// The session left the registry whichever way the race resolved.
	if n := bh.h.sess.count(); n != 0 {
		t.Fatalf("live sessions after the mixed-verb race = %d", n)
	}
	_ = id
}

// drainBody reads and closes a response body (test helper).
func drainBody(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// ---- B2: idle TTL eviction ----

// TestIdleSessionEviction (review B2): a session idle beyond the registry's
// TTL is aborted (fd released, storage directory removed) and dropped from
// the registry; further verbs answer 404 BLOB_UPLOAD_UNKNOWN. A fresh
// session inside the TTL survives the same sweep.
func TestIdleSessionEviction(t *testing.T) {
	bh := newBlobHarness(t)

	// Shrink the TTL so the test does not wait a day; the sweep function is
	// driven directly (the production loop's interval is the TTL/48).
	bh.h.sess.mu.Lock()
	bh.h.sess.ttl = 50 * time.Millisecond
	bh.h.sess.mu.Unlock()

	// Deterministic clock: evictIdle takes the sweep time as a parameter,
	// so the boundary is driven by synthetic timestamps — no sleeps (a
	// wall-clock sleep raced the scheduler under parallel -race load: a
	// stall between the assertions flipped the fresh-session arm). t0 is
	// sampled BEFORE the session exists, so the session's own started is
	// t0+delta with delta = the two in-memory round trips (<< margins).
	t0 := time.Now()
	loc, _ := bh.startUpload("team1/app")
	resp := bh.serve(http.MethodPatch, loc, strings.NewReader("half a blob"), nil)
	drainBody(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("PATCH status = %d", resp.StatusCode)
	}

	if evicted := bh.h.sess.evictIdle(t0.Add(10 * time.Millisecond)); len(evicted) != 0 {
		t.Fatalf("session evicted well INSIDE the 50ms TTL: %v", evicted)
	}
	evicted := bh.h.sess.evictIdle(t0.Add(200 * time.Millisecond))
	if len(evicted) != 1 {
		t.Fatalf("evicted = %v, want the one idle session (sweep far past the TTL)", evicted)
	}
	if bh.h.sess.count() != 0 {
		t.Fatalf("registry still holds the idle session")
	}

	// The expired session answers 404 BLOB_UPLOAD_UNKNOWN on every verb.
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodPut, http.MethodDelete} {
		resp := bh.serve(method, loc, nil, nil)
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s expired session status = %d", method, resp.StatusCode)
		}
		if !strings.Contains(string(body), "BLOB_UPLOAD_UNKNOWN") {
			t.Fatalf("%s expired session body = %s", method, body)
		}
	}

	// The storage-side session directory is gone too (Abort ran).
	id := strings.TrimPrefix(loc, "/v2/team1/app/blobs/uploads/")
	if _, err := os.Stat(bh.root + "/sessions/" + id); !os.IsNotExist(err) {
		t.Fatalf("session directory survived eviction: %v", err)
	}

	// A fresh session survives a sweep at its own birth time.
	loc2, _ := bh.startUpload("team1/app")
	if evicted := bh.h.sess.evictIdle(time.Now()); len(evicted) != 0 {
		t.Fatalf("fresh session evicted: %v", evicted)
	}
	if bh.h.sess.count() != 1 {
		t.Fatalf("fresh session missing after sweep")
	}
	resp = bh.serve(http.MethodGet, loc2, nil, nil)
	drainBody(t, resp)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("fresh session offset query status = %d", resp.StatusCode)
	}
}

// ---- B3: mount probe releases the reader ----

// TestMountProbeClosesReader (review B3): blobPresent must close the reader
// the service Get opens on the probe path. The handler is wrapped so every
// Get's reader is counted open/closed; a successful mount must leave every
// probe reader closed.
func TestMountProbeClosesReader(t *testing.T) {
	bh := newBlobHarness(t)
	content := []byte("mount probe fd content")
	dgst := "sha256:" + sha256Hex(content)

	// Wrap the service: every successful Get hands out a counting reader.
	var open, closed atomic.Int64
	inner := bh.h.svc
	bh.h.svc = &countingGetService{inner: inner,
		onOpen:  func(int64) { open.Add(1) },
		onClose: func(int64) { closed.Add(1) }}

	resp := bh.serve(http.MethodPost,
		"/v2/team1/app/blobs/uploads/?digest="+dgst, strings.NewReader(string(content)), nil)
	drainBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed status = %d", resp.StatusCode)
	}

	// A successful mount probes the source (one open) and must close it.
	resp = bh.serve(http.MethodPost,
		"/v2/team2/copy/blobs/uploads/?mount="+dgst+"&from=team1/app", nil, nil)
	drainBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("mount status = %d", resp.StatusCode)
	}
	if open.Load() == 0 {
		t.Fatal("the mount never probed the source (test wiring broken)")
	}
	if open.Load() != closed.Load() {
		t.Fatalf("probe readers leaked: opened %d, closed %d", open.Load(), closed.Load())
	}
}

// countingReadSeeker counts Close calls (the B3 leak probe).
type countingReadSeeker struct {
	rc        io.ReadSeekCloser
	onClose   func()
	closeOnce sync.Once
}

func (c *countingReadSeeker) Read(p []byte) (int, error)         { return c.rc.Read(p) }
func (c *countingReadSeeker) Seek(o int64, w int) (int64, error) { return c.rc.Seek(o, w) }
func (c *countingReadSeeker) Close() error {
	c.closeOnce.Do(c.onClose)
	return c.rc.Close()
}

// countingGetService wraps a repo.Service counting the readers its Get opens
// versus the ones actually closed — the B3 leak observable.
type countingGetService struct {
	inner           repo.Service
	onOpen, onClose func(int64)
}

func (s *countingGetService) Get(ctx context.Context, p *Principal, repoKey, path string) (io.ReadSeekCloser, *metadata.Node, error) {
	rc, node, err := s.inner.Get(ctx, p, repoKey, path)
	if err != nil {
		return rc, node, err
	}
	s.onOpen(1)
	return &countingReadSeeker{rc: rc, onClose: func() { s.onClose(1) }}, node, nil
}

// The remaining Service methods delegate verbatim (the blob domain only
// calls Get/Put/PutFromBlob/PutLandedBlob; the rest keep the wrapped
// behavior).
func (s *countingGetService) Put(ctx context.Context, p *Principal, rk, path string, body io.Reader, expect storage.BlobRef, mime string) (*metadata.Node, error) {
	return s.inner.Put(ctx, p, rk, path, body, expect, mime)
}
func (s *countingGetService) PutFromBlob(ctx context.Context, p *Principal, rk, path string, ref storage.BlobRef, mime string) (*metadata.Node, error) {
	return s.inner.PutFromBlob(ctx, p, rk, path, ref, mime)
}
func (s *countingGetService) PutWithOptions(ctx context.Context, p *Principal, rk, path string, body io.Reader, expect storage.BlobRef, mime string, opts repo.PutOptions) (*metadata.Node, error) {
	return s.inner.PutWithOptions(ctx, p, rk, path, body, expect, mime, opts)
}
func (s *countingGetService) PutLandedBlob(ctx context.Context, p *Principal, rk, path string, ref storage.BlobRef, mime string) (*metadata.Node, error) {
	return s.inner.PutLandedBlob(ctx, p, rk, path, ref, mime)
}
func (s *countingGetService) Delete(ctx context.Context, p *Principal, rk, path string) error {
	return s.inner.Delete(ctx, p, rk, path)
}
func (s *countingGetService) List(ctx context.Context, p *Principal, rk, prefix string) ([]*metadata.Node, error) {
	return s.inner.List(ctx, p, rk, prefix)
}

// RewriteSubtreePrefix delegates verbatim (the T-371 interface widening's
// test-only ripple; the blob domain never re-homes subtrees).
func (s *countingGetService) RewriteSubtreePrefix(ctx context.Context, rk, src, dst string) (*repo.SubtreeRewrite, error) {
	return s.inner.RewriteSubtreePrefix(ctx, rk, src, dst)
}

// SearchScope/CanRead delegate verbatim (T-413's AQL read-only seams —
// same test-only ripple family as RewriteSubtreePrefix above).
func (s *countingGetService) SearchScope(ctx context.Context, p *Principal) ([]repo.ReadScope, error) {
	return s.inner.SearchScope(ctx, p)
}

func (s *countingGetService) CanRead(ctx context.Context, p *Principal, rk, path string) bool {
	return s.inner.CanRead(ctx, p, rk, path)
}
func (s *countingGetService) CreateRepo(ctx context.Context, p *Principal, r *metadata.Repo) (*metadata.Repo, error) {
	return s.inner.CreateRepo(ctx, p, r)
}
func (s *countingGetService) GetRepo(ctx context.Context, p *Principal, rk string) (*metadata.Repo, error) {
	return s.inner.GetRepo(ctx, p, rk)
}
func (s *countingGetService) ListRepos(ctx context.Context, p *Principal) ([]*metadata.Repo, error) {
	return s.inner.ListRepos(ctx, p)
}
func (s *countingGetService) ListReposFiltered(ctx context.Context, p *Principal, repoType, packageType string) ([]*metadata.Repo, error) {
	return s.inner.ListReposFiltered(ctx, p, repoType, packageType)
}
func (s *countingGetService) UpdateRepo(ctx context.Context, p *Principal, r *metadata.Repo) (*metadata.Repo, error) {
	return s.inner.UpdateRepo(ctx, p, r)
}
func (s *countingGetService) DeleteRepo(ctx context.Context, p *Principal, rk string, del bool) error {
	return s.inner.DeleteRepo(ctx, p, rk, del)
}
func (s *countingGetService) PutManifest(ctx context.Context, p *Principal, rk, image, digest, tag, mediaType string, size int64, refs []*metadata.DockerRef) (*repo.PutManifestResult, error) {
	return s.inner.PutManifest(ctx, p, rk, image, digest, tag, mediaType, size, refs)
}
func (s *countingGetService) ResolveManifest(ctx context.Context, p *Principal, rk, image, digest string) (*metadata.DockerManifest, error) {
	return s.inner.ResolveManifest(ctx, p, rk, image, digest)
}
func (s *countingGetService) ResolveTag(ctx context.Context, p *Principal, rk, image, tag string) (*metadata.DockerTag, error) {
	return s.inner.ResolveTag(ctx, p, rk, image, tag)
}
func (s *countingGetService) ListTags(ctx context.Context, p *Principal, rk, image string, n int, last string) ([]*metadata.DockerTag, error) {
	return s.inner.ListTags(ctx, p, rk, image, n, last)
}
func (s *countingGetService) ListImages(ctx context.Context, p *Principal, rk string, n int, last string) ([]string, error) {
	return s.inner.ListImages(ctx, p, rk, n, last)
}
func (s *countingGetService) DeleteManifest(ctx context.Context, p *Principal, rk, image, digest string) error {
	return s.inner.DeleteManifest(ctx, p, rk, image, digest)
}
func (s *countingGetService) DeleteRepoDocker(ctx context.Context, rk string) (int64, error) {
	return s.inner.DeleteRepoDocker(ctx, rk)
}
func (s *countingGetService) VirtualMemberOrder(ctx context.Context, virtualKey string) ([]repo.VirtualMember, error) {
	return s.inner.VirtualMemberOrder(ctx, virtualKey)
}
func (s *countingGetService) ReadVirtualMember(ctx context.Context, virtualKey, member, path string) (io.ReadSeekCloser, *metadata.Node, error) {
	return s.inner.ReadVirtualMember(ctx, virtualKey, member, path)
}
func (s *countingGetService) ListVirtualMember(ctx context.Context, virtualKey, member, prefix string) ([]*metadata.Node, error) {
	return s.inner.ListVirtualMember(ctx, virtualKey, member, prefix)
}

// SearchArtifacts/SearchChecksum delegate the T-92 search face through the
// wrapper (the counted call sites are the Get family; search is pass-through
// boilerplate the interface demands).
func (s *countingGetService) SearchArtifacts(ctx context.Context, p *Principal, name string, repos []string) ([]*metadata.Node, error) {
	return s.inner.SearchArtifacts(ctx, p, name, repos)
}
func (s *countingGetService) SearchChecksum(ctx context.Context, p *Principal, q repo.ChecksumQuery, repos []string) ([]*metadata.Node, error) {
	return s.inner.SearchChecksum(ctx, p, q, repos)
}

// Usage delegates like the rest (the T-95 interface addition's test-only
// ripple on this wrapper).
func (s *countingGetService) Usage(ctx context.Context, p *Principal, repoKey string) (*repo.UsageReport, error) {
	return s.inner.Usage(ctx, p, repoKey)
}

// UsageBatch delegates like the rest (the T-253/E1 interface addition's
// test-only ripple on this wrapper — the same shape as Usage above).
func (s *countingGetService) UsageBatch(ctx context.Context, p *Principal, q repo.UsageBatchQuery) ([]*repo.UsageBatchReport, error) {
	return s.inner.UsageBatch(ctx, p, q)
}

// ---- B4: registration failure renders 5xx, retry heals ----

// TestRegistrationFailureIs5xx (review B4): when Commit succeeds but the
// node registration (svc.Put) fails, the response is 500 UNKNOWN — never a
// 201 over a blob no node points at. Clearing the fault and re-pushing
// lands the full chain (retry-safety: Commit dedups, Put retransmits).
func TestRegistrationFailureIs5xx(t *testing.T) {
	bh := newBlobHarness(t)
	content := []byte("b4 registration failure probe")
	dgst := "sha256:" + sha256Hex(content)

	// Inject the registration failure (transient).
	bh.svc.mu.Lock()
	bh.svc.putErr = errors.New("simulated metadata outage")
	bh.svc.mu.Unlock()

	resp := bh.serve(http.MethodPost,
		"/v2/team1/app/blobs/uploads/?digest="+dgst, strings.NewReader(string(content)), nil)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("registration-failure status = %d body=%s", resp.StatusCode, body)
	}
	assertSpecCode(t, body, "UNKNOWN")

	// No node row exists: the blob is unreadable through the plane.
	resp = bh.serve(http.MethodGet, "/v2/team1/app/blobs/"+dgst, nil, nil)
	drainBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unregistered blob GET status = %d, want 404", resp.StatusCode)
	}

	// The retry heals: the same content re-pushes to 201 (Commit dedups
	// onto the physical blob, Put completes the ledger+node rows).
	bh.svc.mu.Lock()
	bh.svc.putErr = nil
	bh.svc.mu.Unlock()
	resp = bh.serve(http.MethodPost,
		"/v2/team1/app/blobs/uploads/?digest="+dgst, strings.NewReader(string(content)), nil)
	drainBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("retry status = %d", resp.StatusCode)
	}
	resp = bh.serve(http.MethodGet, "/v2/team1/app/blobs/"+dgst, nil, nil)
	got, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(got) != string(content) {
		t.Fatalf("post-retry GET status = %d", resp.StatusCode)
	}
}

// TestRegistrationDeniedIs403 (review B4's denied arm): a permission-shaped
// registration refusal stays 403 DENIED, not the 5xx retry signal.
func TestRegistrationDeniedIs403(t *testing.T) {
	bh := newBlobHarness(t)
	content := []byte("b4 denied probe")
	dgst := "sha256:" + sha256Hex(content)

	bh.svc.mu.Lock()
	bh.svc.putErr = fmt.Errorf("write team1/app/blobs/x: %w", repo.ErrForbidden)
	bh.svc.mu.Unlock()

	resp := bh.serve(http.MethodPost,
		"/v2/team1/app/blobs/uploads/?digest="+dgst, strings.NewReader(string(content)), nil)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("denied registration status = %d body=%s", resp.StatusCode, body)
	}
	assertSpecCode(t, body, "DENIED")
}

// ---- fd-equivalent evidence on the real stack ----

// TestRealStackMountNoFdAccumulation (B3's real-stack arm): repeated mounts
// over the full httpapi stack must not grow the process's open-file count.
// Lsof on macOS from inside `go test` is unreliable; the open-fd table of
// the test process itself is the equivalent, directly observable signal.
// The mount probes run against the REAL repo.Service (real *os.File
// readers), which is exactly the leak surface the review described.
func TestRealStackMountNoFdAccumulation(t *testing.T) {
	if testing.Short() {
		t.Skip("fd-count probe in short mode")
	}
	bh := blobHarnessRealStack(t)
	content := []byte("fd accumulation probe")
	dgst := "sha256:" + sha256Hex(content)

	// Seed the source through a real monolithic push.
	resp := bh.serve(http.MethodPost,
		"/v2/team1/app/blobs/uploads/?digest="+dgst, strings.NewReader(string(content)), nil)
	drainBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed status = %d", resp.StatusCode)
	}

	openFDs := func() int {
		// ReadDir stats every entry and macOS's /dev/fd symlinks fail
		// fstatat; Readdirnames only lists, which is exactly the count.
		d, err := os.Open("/dev/fd") //nolint:gosec // constant path
		if err != nil {
			t.Skipf("/dev/fd unavailable: %v", err)
		}
		names, err := d.Readdirnames(-1)
		_ = d.Close()
		if err != nil {
			t.Skipf("readdir /dev/fd: %v", err)
		}
		return len(names) - 1 // minus the directory handle just opened
	}

	// Warm up, then measure across a batch of mounts.
	for i := 0; i < 5; i++ {
		resp := bh.serve(http.MethodPost,
			fmt.Sprintf("/v2/team2/copy%d/blobs/uploads/?mount=%s&from=team1/app", i, dgst), nil, nil)
		drainBody(t, resp)
	}
	before := openFDs()
	for i := 5; i < 55; i++ {
		resp := bh.serve(http.MethodPost,
			fmt.Sprintf("/v2/copy%d/blobs/uploads/?mount=%s&from=team1/app", i, dgst), nil, nil)
		drainBody(t, resp)
	}
	after := openFDs()
	if after > before+5 { // small slack for unrelated runtime churn
		t.Fatalf("open fds grew across 50 mounts: %d -> %d", before, after)
	}
}

// ---- T-43 QA D1: non-admin mount honors a granted read ----

// TestNonAdminMountSucceeds (T-43 QA D1): a NON-admin principal with read
// on the source image must get the zero-copy 201, not the degraded 202.
// canMountFrom used to pass the scope spelling ("pull") into
// Authorizer.Can, whose action domain is "r"/"w"/"d" — rowAllows answers
// default:false to unknown actions, so every non-admin mount silently
// degraded (admins were masked by Can's p.Admin short-circuit). Both the
// unit fake (exact-prefix read grant) and the real auth.Service stack are
// covered; the real one is the regression that matters (it is the exact
// surface QA reproduced against).
func TestNonAdminMountSucceeds(t *testing.T) {
	t.Run("unit fake: granted reader mounts 201", func(t *testing.T) {
		bh := newBlobHarness(t)
		content := []byte("d1 non-admin mount")
		dgst := "sha256:" + sha256Hex(content)

		resp := bh.serve(http.MethodPost,
			"/v2/team1/app/blobs/uploads/?digest="+dgst, strings.NewReader(string(content)), nil)
		drainBody(t, resp)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("seed status = %d", resp.StatusCode)
		}

		// A non-admin with read+write on team2 AND read on the source
		// image's repo — precisely QA's reproduction shape.
		bh.h.authz = allowListAuthorizer{"mover": {"team1": true, "team2": true}}
		bh.adminName = "mover"
		bh.admin = false

		resp = bh.serve(http.MethodPost,
			"/v2/team2/copy/blobs/uploads/?mount="+dgst+"&from=team1/app", nil, nil)
		drainBody(t, resp)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("non-admin mount status = %d, want 201 (got the degraded 202 — D1 regression)", resp.StatusCode)
		}
		resp = bh.serve(http.MethodGet, "/v2/team2/copy/blobs/"+dgst, nil, nil)
		got, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(got) != string(content) {
			t.Fatalf("mounted GET status = %d", resp.StatusCode)
		}

		// The negative holds: a non-admin WITHOUT the source read still
		// degrades (fail-closed direction unchanged by the fix).
		bh.h.authz = allowListAuthorizer{"blind": {"team2": true}}
		bh.adminName = "blind"
		resp = bh.serve(http.MethodPost,
			"/v2/team2/copy2/blobs/uploads/?mount="+dgst+"&from=team1/app", nil, nil)
		drainBody(t, resp)
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("unreadable-source mount status = %d, want the degraded 202", resp.StatusCode)
		}
	})

	t.Run("real authorizer: granted non-admin mounts 201", func(t *testing.T) {
		bh := blobHarnessRealStack(t)
		ctx := context.Background()
		md := bh.realMeta(t)
		content := []byte("d1 real-stack non-admin mount")
		dgst := "sha256:" + sha256Hex(content)

		// Seed a non-admin user with read on the source repo's image path
		// and read+write on the destination repo — through the REAL
		// permission store, so the exact rowAllows path runs.
		if err := md.Users().Create(ctx, &metadata.User{
			Username: "mover", PasswordHash: hashForTest("mover-pw"), Enabled: true,
		}); err != nil {
			t.Fatalf("seed user: %v", err)
		}
		if err := md.Permissions().PutTarget(ctx, &metadata.PermissionTarget{
			Name: "d1-mover", Repos: `["team1","team2"]`,
			Includes: `["app/**","copy/**"]`, Excludes: "[]",
		}, []*metadata.PermissionPrincipal{{
			TargetName: "d1-mover", Principal: "mover", PrincipalType: "user",
			CanRead: true, CanWrite: true, CanDelete: false,
		}}); err != nil {
			t.Fatalf("seed permission: %v", err)
		}

		resp := bh.serve(http.MethodPost,
			"/v2/team1/app/blobs/uploads/?digest="+dgst, strings.NewReader(string(content)), nil)
		drainBody(t, resp)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("seed status = %d", resp.StatusCode)
		}

		bh.adminName = "mover"
		bh.admin = false
		resp = bh.serve(http.MethodPost,
			"/v2/team2/copy/blobs/uploads/?mount="+dgst+"&from=team1/app", nil, nil)
		drainBody(t, resp)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("non-admin mount status = %d, want 201 (D1 regression on the real authorizer)", resp.StatusCode)
		}
		resp = bh.serve(http.MethodGet, "/v2/team2/copy/blobs/"+dgst, nil, nil)
		got, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(got) != string(content) {
			t.Fatalf("mounted GET status = %d", resp.StatusCode)
		}
	})
}

// blobHarnessRealStack builds the blob harness against a REAL repo.Service
// (real storage engine + real metadata store), the leak surface B3 named.
func blobHarnessRealStack(t *testing.T) *blobHarness {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	st, err := storage.OpenEngine(root, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: root + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	// Seed the repository rows the mount destinations need (copy0..copy54).
	table := map[string]string{"team1": "docker", "team2": "docker"}
	for i := 0; i < 60; i++ {
		table[fmt.Sprintf("copy%d", i)] = "docker"
		if err := md.Repos().Create(ctx, &metadata.Repo{
			RepoKey: fmt.Sprintf("copy%d", i), Type: repo.TypeLocal, PackageType: "docker",
		}); err != nil {
			t.Fatalf("seed copy repo: %v", err)
		}
	}
	for _, key := range []string{"team1", "team2"} {
		if err := md.Repos().Create(ctx, &metadata.Repo{
			RepoKey: key, Type: repo.TypeLocal, PackageType: "docker",
		}); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}

	authz := auth.NewFromStore(md, true)
	svc := repo.New(st, md, authz, nil)
	h := New(nil, NewStaticRepoLookup(table), authz, nil, nil,
		Options{AnonymousAccess: true}, nil).
		WithStorage(st, md.Blobs())
	h.svc = svc
	bh := &blobHarness{t: t, h: h, svc: nil, store: st, root: root, adminName: "admin", admin: true}
	bh.realMD = md
	return bh
}

// realMeta returns the real metadata store behind a real-stack harness (the
// D1 test seeds its user/permission rows through it).
func (bh *blobHarness) realMeta(t *testing.T) metadata.Store {
	t.Helper()
	if bh.realMD == nil {
		t.Fatal("harness was not built on a real metadata store")
	}
	return bh.realMD
}

// hashForTest hashes a password the way the real auth service does.
func hashForTest(pw string) string {
	h, err := auth.HashPassword(pw)
	if err != nil {
		panic("hashForTest: " + err.Error())
	}
	return h
}
