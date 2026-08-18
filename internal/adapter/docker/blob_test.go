package docker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// readBody drains and closes resp, returning its bytes.
func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	if resp == nil || resp.Body == nil {
		return nil
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	_ = resp.Body.Close()
	return b
}

// blobHarness is the unit-test stack of the blob domain: the real handler
// over a real storage engine plus stub repo/service collaborators. The
// service seam (blob reads, mount puts) is a recording fake; the repo gate
// reads a static table.
type blobHarness struct {
	t         *testing.T
	h         *Handler
	svc       *fakeService
	store     storage.Engine
	root      string
	adminName string
	admin     bool
}

// fakeService is a table-backed repo.Service for the blob read/mount paths
// and (T-39) the manifest plane: Get serves in-memory blobs registered by
// path; Put/PutFromBlob record calls; the manifest index is a slice pair
// seeded through PutManifest itself (so the adapter's own writes drive the
// reads back).
type fakeService struct {
	mu     sync.Mutex
	blobs  map[string][]byte // "<repoKey>/<path>" -> content
	mimes  map[string]string // "<repoKey>/<path>" -> mime of the last Put
	puts   []mountCall
	getErr map[string]error
	putErr error
	engine storage.Engine // committed-blob source for PutFromBlob

	// manifest-plane tables (T-39).
	manifests      []*metadata.DockerManifest
	tags           []*metadata.DockerTag
	refs           []*metadata.DockerRef
	putManifestErr error // injected publish failure
	resolveErr     error // injected ResolveManifest/ResolveTag failure
	deleteErr      error // injected DeleteManifest failure
}

type mountCall struct {
	repoKey, path, hex string
}

func newFakeService() *fakeService {
	return &fakeService{blobs: map[string][]byte{}, mimes: map[string]string{}, getErr: map[string]error{}}
}

func (f *fakeService) put(repoKey, path string, content []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blobs[repoKey+"/"+path] = content
}

func (f *fakeService) Get(_ context.Context, _ *Principal, repoKey, path string) (io.ReadSeekCloser, *metadata.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.getErr[repoKey+"/"+path]; err != nil {
		return nil, nil, err
	}
	content, ok := f.blobs[repoKey+"/"+path]
	if !ok {
		return nil, nil, errNodeNotFoundFake
	}
	sum := sha256.Sum256(content)
	return readSeekCloser{r: bytes.NewReader(content)}, &metadata.Node{
		RepoKey: repoKey, Path: path,
		Sha256: hex.EncodeToString(sum[:]),
		Size:   int64(len(content)),
	}, nil
}

// readSeekCloser adapts a *bytes.Reader onto the io.ReadSeekCloser the
// service returns (NopCloser loses Seek, which the range path needs).
type readSeekCloser struct{ r *bytes.Reader }

func (c readSeekCloser) Read(p []byte) (int, error)         { return c.r.Read(p) }
func (c readSeekCloser) Seek(o int64, w int) (int64, error) { return c.r.Seek(o, w) }
func (c readSeekCloser) Close() error                       { return nil }

func (f *fakeService) PutFromBlob(ctx context.Context, p *Principal, repoKey, path string, ref storage.BlobRef, mime string) (*metadata.Node, error) {
	f.mu.Lock()
	if f.putErr != nil {
		f.mu.Unlock()
		return nil, f.putErr
	}
	f.puts = append(f.puts, mountCall{repoKey: repoKey, path: path, hex: ref.Sha256})
	f.mu.Unlock()
	// Mirror the real service: the node materializes over the committed
	// blob, so subsequent Gets serve the store's content.
	content, err := f.readBlob(ctx, ref.Sha256)
	if err != nil {
		return nil, err
	}
	f.put(repoKey, path, content)
	_ = p
	_ = mime
	return &metadata.Node{RepoKey: repoKey, Path: path, Sha256: ref.Sha256}, nil
}

// readBlob streams the committed blob from the real storage engine (the
// fake's Gets serve from this, keeping the read path honest end to end).
func (f *fakeService) readBlob(ctx context.Context, hex256 string) ([]byte, error) {
	if f.engine == nil {
		return nil, errUnimplementedFake
	}
	rc, _, err := f.engine.Open(ctx, hex256)
	if err != nil {
		return nil, err
	}
	defer rc.Close() //nolint:errcheck // read-only fd
	return io.ReadAll(rc)
}

// errNodeNotFoundFake wraps the repo sentinel the same way the real service
// does so errors.Is keeps working across the stub boundary.
var errNodeNotFoundFake = fmt.Errorf("node: %w", repo.ErrNodeNotFound)

// Put mirrors the real service's landing path for the registration flow:
// the body is stored at the path (the adapter re-feeds the committed blob;
// the engine's Commit dedups, so no second copy appears).
func (f *fakeService) Put(_ context.Context, _ *Principal, repoKey, path string, body io.Reader, _ storage.BlobRef, mime string) (*metadata.Node, error) {
	f.mu.Lock()
	putErr := f.putErr
	f.mu.Unlock()
	if putErr != nil {
		return nil, putErr
	}
	content, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.puts = append(f.puts, mountCall{repoKey: repoKey, path: path, hex: sha256Hex(content)})
	f.mimes[repoKey+"/"+path] = mime
	f.mu.Unlock()
	f.put(repoKey, path, content)
	return &metadata.Node{RepoKey: repoKey, Path: path, Sha256: sha256Hex(content), Size: int64(len(content))}, nil
}

func (f *fakeService) Delete(context.Context, *Principal, string, string) error {
	return errUnimplementedFake
}

func (f *fakeService) List(context.Context, *Principal, string, string) ([]*metadata.Node, error) {
	return nil, errUnimplementedFake
}

func (f *fakeService) CreateRepo(context.Context, *Principal, *metadata.Repo) (*metadata.Repo, error) {
	return nil, errUnimplementedFake
}

func (f *fakeService) GetRepo(context.Context, *Principal, string) (*metadata.Repo, error) {
	return nil, errUnimplementedFake
}

func (f *fakeService) ListRepos(context.Context, *Principal) ([]*metadata.Repo, error) {
	return nil, errUnimplementedFake
}

func (f *fakeService) UpdateRepo(context.Context, *Principal, *metadata.Repo) (*metadata.Repo, error) {
	return nil, errUnimplementedFake
}

func (f *fakeService) DeleteRepo(context.Context, *Principal, string, bool) error {
	return errUnimplementedFake
}

// PutManifest (fake): records the manifest row, the tag pointer and the
// ref ledger exactly once per digest (idempotent republish refreshes tags
// and refs only, mirroring the real service), and writes the manifest node
// at the layout path with the media type as its mime (the real service's
// putNode step — the adapter's read path streams from that node).
func (f *fakeService) PutManifest(_ context.Context, _ *Principal, repoKey, image, digest, tag, mediaType string, size int64, refs []*metadata.DockerRef) (*repo.PutManifestResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putManifestErr != nil {
		return nil, f.putManifestErr
	}
	var existing *metadata.DockerManifest
	for _, m := range f.manifests {
		if m.RepoKey == repoKey && m.Image == image && m.Digest == digest {
			existing = m
			break
		}
	}
	reported := existing
	if reported == nil {
		reported = &metadata.DockerManifest{
			RepoKey: repoKey, Image: image, Digest: digest, MediaType: mediaType, Size: size}
		f.manifests = append(f.manifests, reported)
	}
	// The manifest node (mirrors the real service's putNode at
	// <image>/manifests/<hex>): the body was already stored by the adapter
	// through the blob plane, so the node streams the same bytes with the
	// manifest's media type.
	nodePath := manifestNodePath(image, digest)
	if _, ok := f.blobs[repoKey+"/"+nodePath]; !ok {
		if body, ok := f.blobs[repoKey+"/"+blobNodePath(image, digest)]; ok {
			f.blobs[repoKey+"/"+nodePath] = body
			f.mimes[repoKey+"/"+nodePath] = mediaType
		}
	}
	if tag != "" {
		found := false
		for _, t := range f.tags {
			if t.RepoKey == repoKey && t.Image == image && t.Tag == tag {
				t.Digest = digest
				found = true
				break
			}
		}
		if !found {
			f.tags = append(f.tags, &metadata.DockerTag{RepoKey: repoKey, Image: image, Tag: tag, Digest: digest})
		}
	}
	// The ref ledger mirrors the real store's PK semantics (T-39 review
	// B1): one row per (repo, image, manifest, blob) — duplicate blob
	// digests in the caller's set collapse onto the first, they never
	// multiply. The fake was previously weaker than the store here, which
	// is exactly how B1 escaped the unit suite.
	kept := refs[:0:0]
	seenRef := make(map[string]struct{}, len(refs))
	for _, r := range refs {
		if r == nil {
			continue
		}
		if _, dup := seenRef[r.BlobDigest]; dup {
			continue
		}
		seenRef[r.BlobDigest] = struct{}{}
		kept = append(kept, &metadata.DockerRef{
			RepoKey: repoKey, Image: image, ManifestDigest: digest,
			BlobDigest: r.BlobDigest, ChildMediaType: r.ChildMediaType})
	}
	f.refs = kept
	return &repo.PutManifestResult{Manifest: reported}, nil
}

// ResolveManifest (fake): the index row or ErrManifestNotFound.
func (f *fakeService) ResolveManifest(_ context.Context, _ *Principal, repoKey, image, digest string) (*metadata.DockerManifest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	for _, m := range f.manifests {
		if m.RepoKey == repoKey && m.Image == image && m.Digest == digest {
			return m, nil
		}
	}
	return nil, fmt.Errorf("manifest: %w", repo.ErrManifestNotFound)
}

// ResolveTag (fake): the tag's current digest or ErrTagNotFound.
func (f *fakeService) ResolveTag(_ context.Context, _ *Principal, repoKey, image, tag string) (*metadata.DockerTag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	for _, t := range f.tags {
		if t.RepoKey == repoKey && t.Image == image && t.Tag == tag {
			return t, nil
		}
	}
	return nil, fmt.Errorf("tag: %w", repo.ErrTagNotFound)
}

// ListTags (fake, T-40): mirrors the real service's listing contract — tag
// order, the exclusive last cursor over the tag charset (ErrInvalidCursor),
// ErrImageNotFound for an image without manifest rows, and the n<=0=all cap.
func (f *fakeService) ListTags(_ context.Context, _ *Principal, repoKey, image string, n int, last string) ([]*metadata.DockerTag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if last != "" {
		if err := validateManifestTag(last); err != nil {
			return nil, fmt.Errorf("tags/list cursor %q: %w", last, repo.ErrInvalidCursor)
		}
	}
	var tags []*metadata.DockerTag
	for _, t := range f.tags {
		if t.RepoKey == repoKey && t.Image == image {
			tags = append(tags, t)
		}
	}
	if len(tags) == 0 {
		for _, m := range f.manifests {
			if m.RepoKey == repoKey && m.Image == image {
				return nil, nil // image exists, zero tags
			}
		}
		return nil, fmt.Errorf("image %s/%s: %w", repoKey, image, repo.ErrImageNotFound)
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].Tag < tags[j].Tag })
	if last != "" {
		idx := sort.Search(len(tags), func(i int) bool { return tags[i].Tag > last })
		tags = tags[idx:]
	}
	if n > 0 && len(tags) > n {
		tags = tags[:n]
	}
	return tags, nil
}

// ListImages (fake, T-40): distinct "<repoKey>/<image>" names carrying at
// least one manifest row, lexicographic, exclusive cursor, n<=0=all.
func (f *fakeService) ListImages(_ context.Context, _ *Principal, repoKey string, n int, last string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := map[string]bool{}
	var names []string
	for _, m := range f.manifests {
		if m.RepoKey != repoKey || seen[m.Image] {
			continue
		}
		seen[m.Image] = true
		names = append(names, repoKey+"/"+m.Image)
	}
	sort.Strings(names)
	if last != "" {
		if !strings.HasPrefix(last, repoKey+"/") {
			return nil, fmt.Errorf("catalog cursor %q: %w", last, repo.ErrInvalidCursor)
		}
		idx := sort.SearchStrings(names, last)
		if idx < len(names) && names[idx] == last {
			idx++
		}
		names = names[idx:]
	}
	if n > 0 && len(names) > n {
		names = names[:n]
	}
	return names, nil
}

// DeleteManifest (fake): drops the manifest row and cascades its tags and
// refs (the real store's same-transaction contract, mirrored for the
// adapter's by-tag/by-digest assertions).
func (f *fakeService) DeleteManifest(_ context.Context, _ *Principal, repoKey, image, digest string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	found := false
	kept := f.manifests[:0]
	for _, m := range f.manifests {
		if m.RepoKey == repoKey && m.Image == image && m.Digest == digest {
			found = true
			continue
		}
		kept = append(kept, m)
	}
	f.manifests = kept
	if !found {
		return fmt.Errorf("manifest: %w", repo.ErrManifestNotFound)
	}
	tags := f.tags[:0]
	for _, t := range f.tags {
		if t.RepoKey == repoKey && t.Image == image && t.Digest == digest {
			continue
		}
		tags = append(tags, t)
	}
	f.tags = tags
	refs := f.refs[:0]
	for _, r := range f.refs {
		if r.RepoKey == repoKey && r.Image == image && r.ManifestDigest == digest {
			continue
		}
		refs = append(refs, r)
	}
	f.refs = refs
	return nil
}

func (f *fakeService) DeleteRepoDocker(context.Context, string) (int64, error) {
	return 0, errUnimplementedFake
}

var errUnimplementedFake = fmt.Errorf("unimplemented in the blob-domain fake")

// newBlobHarness assembles the handler with the real storage engine (the
// upload state machine is the ticket's core — a stubbed engine would test
// nothing) and the fake service.
func newBlobHarness(t *testing.T) *blobHarness {
	t.Helper()
	root := t.TempDir()
	st, err := storage.OpenEngine(root, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := newFakeService()
	h := New(nil, NewStaticRepoLookup(map[string]string{"team1": "docker", "team2": "docker", "plain": "generic"}),
		adminPassAuthorizer{}, nil, nil, Options{AnonymousAccess: true}, nil).
		WithStorage(st, nil)
	h.svc = svc
	svc.engine = st
	return &blobHarness{t: t, h: h, svc: svc, store: st, root: root, adminName: "admin", admin: true}
}

// serve runs one request through the handler with an admin principal in the
// context (the repo/permission gates sit ahead of the blob domain and are
// T-33/T-37's, tested there; this harness asserts the blob behavior with
// the gates passed).
func (bh *blobHarness) serve(method, path string, body io.Reader, hdr map[string]string) *http.Response {
	bh.t.Helper()
	req := httptest.NewRequest(method, path, body)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	req = req.WithContext(adapter.WithPrincipal(req.Context(), &auth.Principal{Name: bh.adminName, Admin: bh.admin}))
	rec := httptest.NewRecorder()
	bh.h.ServeHTTP(rec, req)
	return rec.Result()
}

// doUpload drives the canonical three-step push and returns the upload URL.
func (bh *blobHarness) startUpload(name string) (loc, uuid string) {
	bh.t.Helper()
	resp := bh.serve(http.MethodPost, "/v2/"+name+"/blobs/uploads/", nil, nil)
	body := readBody(bh.t, resp)
	if resp.StatusCode != http.StatusAccepted {
		bh.t.Fatalf("start upload status = %d body=%s", resp.StatusCode, body)
	}
	return resp.Header.Get("Location"), resp.Header.Get("Docker-Upload-UUID")
}

// sha256Hex is the test digest helper.
func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// ---- the upload three styles ----

// TestUploadThreeStyles (DE-02/DE-04/DE-05, table-driven): each style pushes
// the same content and must land the identical blob, addressable through the
// read plane with the exact wire shapes pinned.
func TestUploadThreeStyles(t *testing.T) {
	content := bytes.Repeat([]byte("binflow-blob-"), 512) // ~6.5KB

	tests := []struct {
		name string
		push func(t *testing.T, bh *blobHarness) string // returns digest
	}{
		{
			name: "monolithic PUT finalize (D06 shape)",
			push: func(t *testing.T, bh *blobHarness) string {
				dgst := "sha256:" + sha256Hex(content)
				loc, _ := bh.startUpload("team1/app")
				resp := bh.serve(http.MethodPut, loc+"?digest="+dgst, bytes.NewReader(content), nil)
				body := readBody(t, resp)
				if resp.StatusCode != http.StatusCreated {
					t.Fatalf("finalize status = %d body=%s", resp.StatusCode, body)
				}
				if got := resp.Header.Get("Location"); got != "/v2/team1/app/blobs/"+dgst {
					t.Fatalf("Location = %q", got)
				}
				if got := resp.Header.Get("Docker-Content-Digest"); got != dgst {
					t.Fatalf("Docker-Content-Digest = %q", got)
				}
				return dgst
			},
		},
		{
			name: "single-request POST (D07)",
			push: func(t *testing.T, bh *blobHarness) string {
				dgst := "sha256:" + sha256Hex(content)
				resp := bh.serve(http.MethodPost, "/v2/team1/app/blobs/uploads/?digest="+dgst,
					bytes.NewReader(content), nil)
				body := readBody(t, resp)
				if resp.StatusCode != http.StatusCreated {
					t.Fatalf("monolithic POST status = %d body=%s", resp.StatusCode, body)
				}
				return dgst
			},
		},
		{
			name: "chunked PATCH x3 + empty-body PUT (D10 shape)",
			push: func(t *testing.T, bh *blobHarness) string {
				loc, _ := bh.startUpload("team1/app")
				chunks := [][]byte{content[:1000], content[1000:3000], content[3000:]}
				offset := int64(0)
				for i, ch := range chunks {
					resp := bh.serve(http.MethodPatch, loc, bytes.NewReader(ch), map[string]string{
						"Content-Range":  fmt.Sprintf("%d-%d", offset, offset+int64(len(ch))-1),
						"Content-Length": fmt.Sprint(len(ch)),
					})
					body := readBody(t, resp)
					if resp.StatusCode != http.StatusAccepted {
						t.Fatalf("chunk %d status = %d body=%s", i, resp.StatusCode, body)
					}
					want := fmt.Sprintf("0-%d", offset+int64(len(ch))-1)
					if got := resp.Header.Get("Range"); got != want {
						t.Fatalf("chunk %d Range = %q want %q", i, got, want)
					}
					offset += int64(len(ch))
				}
				dgst := "sha256:" + sha256Hex(content)
				resp := bh.serve(http.MethodPut, loc+"?digest="+dgst, nil, nil)
				body := readBody(t, resp)
				if resp.StatusCode != http.StatusCreated {
					t.Fatalf("finalize status = %d body=%s", resp.StatusCode, body)
				}
				return dgst
			},
		},
		{
			name: "streamed PATCH without Content-Range",
			push: func(t *testing.T, bh *blobHarness) string {
				loc, _ := bh.startUpload("team1/app")
				resp := bh.serve(http.MethodPatch, loc, bytes.NewReader(content), nil)
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusAccepted {
					t.Fatalf("stream PATCH status = %d", resp.StatusCode)
				}
				dgst := "sha256:" + sha256Hex(content)
				resp = bh.serve(http.MethodPut, loc+"?digest="+dgst, nil, nil)
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusCreated {
					t.Fatalf("finalize status = %d", resp.StatusCode)
				}
				return dgst
			},
		},
		{
			name: "PUT carries the final chunk",
			push: func(t *testing.T, bh *blobHarness) string {
				loc, _ := bh.startUpload("team1/app")
				head, tail := content[:2000], content[2000:]
				resp := bh.serve(http.MethodPatch, loc, bytes.NewReader(head), nil)
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusAccepted {
					t.Fatalf("PATCH status = %d", resp.StatusCode)
				}
				dgst := "sha256:" + sha256Hex(content)
				resp = bh.serve(http.MethodPut, loc+"?digest="+dgst, bytes.NewReader(tail), nil)
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusCreated {
					t.Fatalf("finalize-with-tail status = %d", resp.StatusCode)
				}
				return dgst
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bh := newBlobHarness(t)
			dgst := tc.push(t, bh)

			// The blob is GETtable and byte-identical.
			resp := bh.serve(http.MethodGet, "/v2/team1/app/blobs/"+dgst, nil, nil)
			got, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET status = %d", resp.StatusCode)
			}
			if !bytes.Equal(got, content) {
				t.Fatalf("GET body differs: %d bytes vs %d", len(got), len(content))
			}
			if resp.Header.Get("Docker-Content-Digest") != dgst {
				t.Fatalf("GET Docker-Content-Digest = %q", resp.Header.Get("Docker-Content-Digest"))
			}

			// HEAD carries length + digest, no body.
			resp = bh.serve(http.MethodHead, "/v2/team1/app/blobs/"+dgst, nil, nil)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("HEAD status = %d", resp.StatusCode)
			}
			if resp.Header.Get("Content-Length") != fmt.Sprint(len(content)) {
				t.Fatalf("HEAD Content-Length = %q", resp.Header.Get("Content-Length"))
			}

			// The node landed at the docker layout path.
			bh.svc.mu.Lock()
			_, ok := bh.svc.blobs["team1/app/blobs/"+strings.TrimPrefix(dgst, "sha256:")]
			bh.svc.mu.Unlock()
			if !ok {
				t.Fatalf("no node at the docker layout path")
			}

			// The session is gone: every further verb is BLOB_UPLOAD_UNKNOWN.
			if n := bh.h.sess.count(); n != 0 {
				t.Fatalf("live sessions after finalize = %d", n)
			}
		})
	}
}

// TestUploadStartResponseShape (DE-02): the initiating POST's exact wire
// shape — relative Location, Docker-Upload-UUID, Range 0-0, zero length.
func TestUploadStartResponseShape(t *testing.T) {
	bh := newBlobHarness(t)
	loc, uuid := bh.startUpload("team1/acme/app")
	if !strings.HasPrefix(loc, "/v2/team1/acme/app/blobs/uploads/") {
		t.Fatalf("Location = %q, want a RELATIVE /v2 path", loc)
	}
	if uuid == "" || !strings.HasSuffix(loc, uuid) {
		t.Fatalf("Docker-Upload-UUID %q must be the Location tail %q", uuid, loc)
	}

	// The offset query (official GET endpoint) answers 204 + Range 0-0.
	resp := bh.serve(http.MethodGet, loc, nil, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("GET upload status = %d", resp.StatusCode)
	}
	if resp.Header.Get("Range") != "0-0" {
		t.Fatalf("GET upload Range = %q", resp.Header.Get("Range"))
	}
	if resp.Header.Get("Docker-Upload-UUID") != uuid {
		t.Fatalf("GET upload UUID = %q", resp.Header.Get("Docker-Upload-UUID"))
	}

	// DELETE cancels: 204, and the session is unknown afterwards.
	resp = bh.serve(http.MethodDelete, loc, nil, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE upload status = %d", resp.StatusCode)
	}
	if bh.h.sess.count() != 0 {
		t.Fatalf("session survived cancel")
	}
	resp = bh.serve(http.MethodGet, loc, nil, nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("post-cancel GET status = %d", resp.StatusCode)
	}
	assertSpecCode(t, body, "BLOB_UPLOAD_UNKNOWN")
}

// TestContentRangeMismatchMatrix (DE-04, table-driven): every misalignment
// of the Content-Range anchor answers 416 with the server's authoritative
// Range and an empty body — and the session survives, still appendable at
// the true offset.
func TestContentRangeMismatchMatrix(t *testing.T) {
	first := bytes.Repeat([]byte("a"), 100)
	second := bytes.Repeat([]byte("b"), 50)

	tests := []struct {
		name   string
		header string
	}{
		{name: "start behind the received offset", header: "0-99"},
		{name: "start ahead of the received offset", header: "200-249"},
		{name: "far ahead", header: "999999-1000048"},
		{name: "malformed: no dash", header: "100"},
		{name: "malformed: non-numeric", header: "onehundred-onefifty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bh := newBlobHarness(t)
			loc, _ := bh.startUpload("team1/app")
			resp := bh.serve(http.MethodPatch, loc, bytes.NewReader(first), nil)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusAccepted {
				t.Fatalf("first PATCH status = %d", resp.StatusCode)
			}

			resp = bh.serve(http.MethodPatch, loc, bytes.NewReader(second),
				map[string]string{"Content-Range": tc.header})
			body := readBody(t, resp)
			if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
				t.Fatalf("mismatch status = %d body=%s", resp.StatusCode, body)
			}
			if len(body) != 0 {
				t.Fatalf("416 must carry an empty body, got %d bytes", len(body))
			}
			if got := resp.Header.Get("Range"); got != "0-99" {
				t.Fatalf("416 Range = %q, want 0-99 (the server's offset)", got)
			}

			// The session is intact: the correctly-anchored chunk continues.
			resp = bh.serve(http.MethodPatch, loc, bytes.NewReader(second),
				map[string]string{"Content-Range": "100-149"})
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusAccepted {
				t.Fatalf("recovery PATCH status = %d", resp.StatusCode)
			}
			if got := resp.Header.Get("Range"); got != "0-149" {
				t.Fatalf("recovery Range = %q", got)
			}
		})
	}
}

// TestDigestMismatchAbortsSession (D12/FR-8-AC5): a wrong digest on the
// finalize is 400 DIGEST_INVALID with the session consumed and NO blob —
// neither readable nor present in the store directory.
func TestDigestMismatchAbortsSession(t *testing.T) {
	bh := newBlobHarness(t)
	content := []byte("binflow digest-mismatch probe")
	loc, _ := bh.startUpload("team1/app")
	resp := bh.serve(http.MethodPatch, loc, bytes.NewReader(content), nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("PATCH status = %d", resp.StatusCode)
	}

	wrong := "sha256:" + strings.Repeat("0", 64)
	resp = bh.serve(http.MethodPut, loc+"?digest="+wrong, nil, nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("finalize status = %d", resp.StatusCode)
	}
	assertSpecCode(t, body, "DIGEST_INVALID")

	// Zero residue: session gone, blob unreadable.
	if n := bh.h.sess.count(); n != 0 {
		t.Fatalf("session survived a digest mismatch")
	}
	resp = bh.serve(http.MethodGet, "/v2/team1/app/blobs/"+wrong, nil, nil)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET wrong digest status = %d", resp.StatusCode)
	}
	assertSpecCode(t, body, "BLOB_UNKNOWN")

	// The same for the single-request style.
	resp = bh.serve(http.MethodPost, "/v2/team1/app/blobs/uploads/?digest="+wrong,
		bytes.NewReader(content), nil)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("monolithic mismatch status = %d", resp.StatusCode)
	}
	assertSpecCode(t, body, "DIGEST_INVALID")
}

// TestPoisonedSessionRefusesOperations (the ErrSessionPoisoned docking): a
// session whose Append failed mid-stream refuses further PATCH and finalize
// with an observable failure, and the abort clears it. The poisoning is
// forced by cancelling the request context mid-append.
func TestPoisonedSessionRefusesOperations(t *testing.T) {
	bh := newBlobHarness(t)
	loc, _ := bh.startUpload("team1/app")

	// A cancelled context mid-append poisons the session (the storage
	// contract: no further Append or Commit is trustworthy).
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPatch, loc, infiniteReader{})
	req = req.WithContext(adapter.WithPrincipal(ctx, &auth.Principal{Name: "admin", Admin: true}))
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		bh.h.ServeHTTP(rec, req)
	}()
	cancel()
	<-done

	up, ok := bh.h.sess.lookup(strings.TrimPrefix(loc, "/v2/team1/app/blobs/uploads/"))
	if !ok {
		t.Fatal("the interrupted session left the registry entirely")
	}
	if !up.poisoned {
		t.Fatal("the interrupted session is not marked poisoned")
	}

	// A poisoned PATCH answers 416-with-offset (the client's restart cue).
	resp := bh.serve(http.MethodPatch, loc, strings.NewReader("more"), nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("poisoned PATCH status = %d", resp.StatusCode)
	}

	// A poisoned finalize is refused and consumed (never committed).
	dgst := "sha256:" + sha256Hex([]byte("anything"))
	resp = bh.serve(http.MethodPut, loc+"?digest="+dgst, nil, nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("poisoned finalize status = %d body=%s", resp.StatusCode, body)
	}
	assertSpecCode(t, body, "BLOB_UPLOAD_INVALID")
	if bh.h.sess.count() != 0 {
		t.Fatalf("poisoned session was not consumed by the refused finalize")
	}
}

// infiniteReader yields bytes forever; used to force a mid-append cancel.
type infiniteReader struct{}

func (infiniteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

// TestConcurrentSessionsConverge: several simultaneous uploads to the same
// and different repos finish independently, land distinct blobs and leave
// no live sessions.
func TestConcurrentSessionsConverge(t *testing.T) {
	bh := newBlobHarness(t)
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("team1/app%d", i%3)
			content := bytes.Repeat([]byte{byte('a' + i)}, 4096)
			req := httptest.NewRequest(http.MethodPost,
				"/v2/"+name+"/blobs/uploads/", nil)
			req = req.WithContext(adapter.WithPrincipal(context.Background(), &auth.Principal{Name: "admin", Admin: true}))
			rec := httptest.NewRecorder()
			bh.h.ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				errs <- fmt.Errorf("worker %d: start status %d", i, rec.Code)
				return
			}
			loc := rec.Header().Get("Location")
			patch := httptest.NewRequest(http.MethodPatch, loc, bytes.NewReader(content))
			patch = patch.WithContext(adapter.WithPrincipal(context.Background(), &auth.Principal{Name: "admin", Admin: true}))
			rec2 := httptest.NewRecorder()
			bh.h.ServeHTTP(rec2, patch)
			if rec2.Code != http.StatusAccepted {
				errs <- fmt.Errorf("worker %d: patch status %d", i, rec2.Code)
				return
			}
			fin := httptest.NewRequest(http.MethodPut,
				loc+"?digest=sha256:"+sha256Hex(content), nil)
			fin = fin.WithContext(adapter.WithPrincipal(context.Background(), &auth.Principal{Name: "admin", Admin: true}))
			rec3 := httptest.NewRecorder()
			bh.h.ServeHTTP(rec3, fin)
			if rec3.Code != http.StatusCreated {
				errs <- fmt.Errorf("worker %d: finalize status %d", i, rec3.Code)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if n := bh.h.sess.count(); n != 0 {
		t.Fatalf("live sessions after convergence = %d", n)
	}
}

// TestBlobUnknownAndDelete (DE-06 404 / DE-14): a missing blob answers the
// spec BLOB_UNKNOWN body; DELETE on a blob is the deliberate 405
// UNSUPPORTED.
func TestBlobUnknownAndDelete(t *testing.T) {
	bh := newBlobHarness(t)
	dgst := "sha256:" + strings.Repeat("ab", 32)

	resp := bh.serve(http.MethodGet, "/v2/team1/app/blobs/"+dgst, nil, nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET unknown blob status = %d", resp.StatusCode)
	}
	assertSpecCode(t, body, "BLOB_UNKNOWN")

	resp = bh.serve(http.MethodDelete, "/v2/team1/app/blobs/"+dgst, nil, nil)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE blob status = %d", resp.StatusCode)
	}
	assertSpecCode(t, body, "UNSUPPORTED")
	if allow := resp.Header.Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow = %q", allow)
	}
}

// TestBlobRangeRequests (FR-8-AC8): the read plane honors single ranges
// (206 + the exact slice), answers 416 on unsatisfiable specs and ignores
// multi-range/other units (full 200 body).
func TestBlobRangeRequests(t *testing.T) {
	bh := newBlobHarness(t)
	content := []byte("0123456789abcdefghij") // 20 bytes
	dgst := "sha256:" + sha256Hex(content)
	resp := bh.serve(http.MethodPost, "/v2/team1/app/blobs/uploads/?digest="+dgst,
		bytes.NewReader(content), nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed status = %d", resp.StatusCode)
	}

	tests := []struct {
		name   string
		header string
		status int
		want   string
		crange string
	}{
		{name: "slice", header: "bytes=2-5", status: 206, want: "2345", crange: "bytes 2-5/20"},
		{name: "open end", header: "bytes=15-", status: 206, want: "fghij", crange: "bytes 15-19/20"},
		{name: "suffix", header: "bytes=-4", status: 206, want: "ghij", crange: "bytes 16-19/20"},
		{name: "clamped end", header: "bytes=0-999", status: 206, want: "0123456789abcdefghij", crange: "bytes 0-19/20"},
		{name: "start beyond total", header: "bytes=20-25", status: 416, crange: "bytes */20"},
		{name: "inverted", header: "bytes=15-3", status: 416, crange: "bytes */20"},
		{name: "empty suffix", header: "bytes=-0", status: 416, crange: "bytes */20"},
		{name: "multi-range ignored", header: "bytes=0-3,5-8", status: 200, want: string(content)},
		{name: "other unit ignored", header: "items=0-3", status: 200, want: string(content)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := bh.serve(http.MethodGet, "/v2/team1/app/blobs/"+dgst, nil,
				map[string]string{"Range": tc.header})
			body := readBody(t, resp)
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d want %d", resp.StatusCode, tc.status)
			}
			if tc.want != "" && string(body) != tc.want {
				t.Fatalf("body = %q want %q", body, tc.want)
			}
			if tc.crange != "" {
				if got := resp.Header.Get("Content-Range"); got != tc.crange {
					t.Fatalf("Content-Range = %q want %q", got, tc.crange)
				}
			}
		})
	}
}

// TestEmptyLayerSynthesis (规格照抄 4): the canonical empty layer answers
// GET/HEAD from the fixed bytes — on a repo where nothing was ever pushed —
// and the digest constants are self-consistent.
func TestEmptyLayerSynthesis(t *testing.T) {
	bh := newBlobHarness(t)

	// The byte table must hash to the documented digest (guards a typo in
	// the constant artifact).
	sum := sha256.Sum256(emptyLayerBytes)
	if hex.EncodeToString(sum[:]) != emptyLayerDigestHex {
		t.Fatalf("emptyLayerBytes sha256 = %x, want %s", sum, emptyLayerDigestHex)
	}
	if len(emptyLayerBytes) != 32 {
		t.Fatalf("empty layer is %d bytes, want 32", len(emptyLayerBytes))
	}

	url := "/v2/team1/app/blobs/sha256:" + emptyLayerDigestHex
	resp := bh.serve(http.MethodGet, url, nil, nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET empty layer status = %d", resp.StatusCode)
	}
	if !bytes.Equal(body, emptyLayerBytes) {
		t.Fatalf("GET empty layer body differs (%d bytes)", len(body))
	}
	if resp.Header.Get("Content-Length") != "32" {
		t.Fatalf("Content-Length = %q", resp.Header.Get("Content-Length"))
	}
	if resp.Header.Get(hdrChecksumSha256) != emptyLayerDigestHex {
		t.Fatalf("X-Checksum-Sha256 = %q", resp.Header.Get(hdrChecksumSha256))
	}

	// HEAD carries the same headers with no body.
	resp = bh.serve(http.MethodHead, url, nil, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD empty layer status = %d", resp.StatusCode)
	}
	if resp.Header.Get(hdrChecksumSha1) != emptyLayerAncillary.sha1 {
		t.Fatalf("X-Checksum-Sha1 = %q", resp.Header.Get(hdrChecksumSha1))
	}
}

// TestCrossRepoMount (DE-03/D13): mount lands the node zero-copy (the
// content is never re-uploaded), honors the source-read ACL, and degrades
// to a 202 session when the source lacks the blob or the read.
func TestCrossRepoMount(t *testing.T) {
	content := []byte("mountable layer content")
	hex256 := sha256Hex(content)
	dgst := "sha256:" + hex256

	seedSource := func(t *testing.T, bh *blobHarness) {
		t.Helper()
		resp := bh.serve(http.MethodPost, "/v2/team1/app/blobs/uploads/?digest="+dgst,
			bytes.NewReader(content), nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("seed source status = %d", resp.StatusCode)
		}
	}

	t.Run("successful mount is a zero-copy 201", func(t *testing.T) {
		bh := newBlobHarness(t)
		seedSource(t, bh)
		resp := bh.serve(http.MethodPost,
			"/v2/team2/copy/blobs/uploads/?mount="+dgst+"&from=team1/app", nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("mount status = %d", resp.StatusCode)
		}
		if got := resp.Header.Get("Location"); got != "/v2/team2/copy/blobs/"+dgst {
			t.Fatalf("mount Location = %q", got)
		}
		// The mount recorded exactly ONE additional zero-copy put at the
		// destination's layout path (the first put is the seed upload's own
		// finalize, the second is the mount — and the mount must be the
		// last word).
		bh.svc.mu.Lock()
		puts := append([]mountCall(nil), bh.svc.puts...)
		bh.svc.mu.Unlock()
		if len(puts) != 2 {
			t.Fatalf("mount puts = %+v, want the seed's own registration plus exactly one mount", puts)
		}
		if puts[1].repoKey != "team2" || puts[1].path != "copy/blobs/"+hex256 || puts[1].hex != hex256 {
			t.Fatalf("mount put = %+v", puts[1])
		}
		if puts[0].repoKey != "team1" {
			t.Fatalf("the earlier put must be the source seed: %+v", puts[0])
		}
		// The blob is readable through the destination.
		resp = bh.serve(http.MethodGet, "/v2/team2/copy/blobs/"+dgst, nil, nil)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK || !bytes.Equal(body, content) {
			t.Fatalf("mounted GET status = %d", resp.StatusCode)
		}
	})

	t.Run("missing source blob degrades to a 202 session", func(t *testing.T) {
		bh := newBlobHarness(t)
		resp := bh.serve(http.MethodPost,
			"/v2/team2/copy/blobs/uploads/?mount="+dgst+"&from=team1/app", nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("degraded mount status = %d", resp.StatusCode)
		}
		if resp.Header.Get("Docker-Upload-UUID") == "" {
			t.Fatal("degraded mount granted no upload session")
		}
	})

	t.Run("no read on the source degrades (NFR-S12)", func(t *testing.T) {
		bh := newBlobHarness(t)
		seedSource(t, bh)
		// The default harness principal is admin (reads everything); swap
		// in a reader that only sees team2.
		bh.h.authz = allowListAuthorizer{"reader": {"team2": true}}
		bh.adminName = "reader"
		bh.admin = false
		resp := bh.serve(http.MethodPost,
			"/v2/team2/copy/blobs/uploads/?mount="+dgst+"&from=team1/app", nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("unreadable-source mount status = %d", resp.StatusCode)
		}
	})

	t.Run("malformed mount digest degrades", func(t *testing.T) {
		bh := newBlobHarness(t)
		resp := bh.serve(http.MethodPost,
			"/v2/team2/copy/blobs/uploads/?mount=notadigest&from=team1/app", nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("malformed mount status = %d", resp.StatusCode)
		}
	})
}

// adminPassAuthorizer passes every admin principal (the route gate's own
// production behavior for admins, minus the store): the blob harness runs
// with the authorization gates passed so the tests assert the blob domain.
type adminPassAuthorizer struct{}

func (adminPassAuthorizer) Can(_ context.Context, p *auth.Principal, _, _, _ string) bool {
	return p != nil && p.Admin
}

// allowListAuthorizer grants read on the listed repos to each named
// principal; everything else denies.
type allowListAuthorizer map[string]map[string]bool

func (a allowListAuthorizer) Can(_ context.Context, p *auth.Principal, repoKey, _, _ string) bool {
	if p == nil {
		return false
	}
	return a[p.Name][repoKey]
}

// TestUploadMethodMatrix: wrong verbs on the two route shapes answer 405
// with an Allow header.
func TestUploadMethodMatrix(t *testing.T) {
	bh := newBlobHarness(t)
	tests := []struct {
		method, path string
		allow        string
	}{
		{http.MethodGet, "/v2/team1/app/blobs/uploads/", "POST"},
		{http.MethodPut, "/v2/team1/app/blobs/uploads/", "POST"},
		{http.MethodPost, "/v2/team1/app/blobs/sha256:" + strings.Repeat("0", 64), "GET, HEAD"},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			resp := bh.serve(tc.method, tc.path, nil, nil)
			body := readBody(t, resp)
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d body=%s", resp.StatusCode, body)
			}
			assertSpecCode(t, body, "UNSUPPORTED")
			if got := resp.Header.Get("Allow"); got != tc.allow {
				t.Fatalf("Allow = %q want %q", got, tc.allow)
			}
		})
	}
}

// TestDigestParamShapes (table-driven): the digest grammar of the blob
// plane — only sha256 with 64 hex characters is addressable.
func TestDigestParamShapes(t *testing.T) {
	tests := []struct {
		param string
		ok    bool
	}{
		{"sha256:" + strings.Repeat("ab", 32), true},
		{"sha256:" + strings.Repeat("AB", 32), true}, // folded
		{strings.Repeat("ab", 32), false},            // bare hex: no algorithm
		{"sha512:" + strings.Repeat("ab", 64), false},
		{"sha256:short", false},
		{"sha256:" + strings.Repeat("g", 64), false},
		{"sha256:", false},
		{"", false},
	}
	for _, tc := range tests {
		_, err := parseDigestParam(tc.param)
		if tc.ok && err != nil {
			t.Errorf("parseDigestParam(%q) = %v, want accepted", tc.param, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("parseDigestParam(%q) accepted, want rejected", tc.param)
		}
	}
}

// assertSpecCode fails unless body is the registry error schema with code.
func assertSpecCode(t *testing.T, body []byte, code string) {
	t.Helper()
	var eb struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &eb); err != nil {
		t.Fatalf("body %q is not the spec schema: %v", body, err)
	}
	if len(eb.Errors) != 1 || eb.Errors[0].Code != code {
		t.Fatalf("body %q: want exactly one %q entry", body, code)
	}
}
