package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The T-111 suite: the /v2 plane's StatusError-verbatim rendering arm — the
// docker half of the seam generic (T-66) and maven/npm/pypi (T-82) already
// carry, closing T-95's leftover 1 (a quota 413 / pattern 409 from the repo
// governance gates answered 500 UNKNOWN on /v2 while the message rode
// along). Two layers:
//
//   - the injected-fake layer pins the RENDERING contract exactly (status
//     verbatim, registry envelope shape, code mapping, carried headers);
//   - the real-stack layer (real repo.Service with governance-configured
//     repositories) pins the W26c end-to-end verdicts: a push over the
//     ceiling answers 413 with zero residue, pattern-rejected pushes answer
//     409, and unconfigured repositories keep the M1~M3 behavior.

// ---- the rendering contract (injected *repo.StatusError) ----

// TestVerbatimRenderBlobRegistration: a StatusError from the registration
// call after a successful Commit (the blob-upload finalize and monolithic
// POST both land here) renders with its own status in the registry
// envelope — not the 500 UNKNOWN the branch answered before T-111.
func TestVerbatimRenderBlobRegistration(t *testing.T) {
	tests := []struct {
		name     string
		se       *repo.StatusError
		wantCode string
	}{
		{
			name: "quota 413",
			se: repo.NewStatusError(http.StatusRequestEntityTooLarge,
				"Repository 'tiny' quota exceeded: used 800 of 1024 bytes; the write to 'app/blobs/ab' needs 512 more bytes.",
				nil, fmt.Errorf("%w: repository tiny holds 800 of 1024 bytes", repo.ErrQuotaExceeded)),
			wantCode: ErrCodeDenied,
		},
		{
			name: "pattern 409",
			se: repo.NewStatusError(http.StatusConflict,
				"Repository 'picky' rejected deployment of 'app/blobs/ab': the path matches excludesPattern '**/blobs/**'.",
				nil, fmt.Errorf("%w: picky/app/blobs/ab hit excludesPattern", repo.ErrPatternRejected)),
			wantCode: ErrCodeDenied,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bh := newBlobHarness(t)
			content := []byte("verbatim-registration-probe")
			dgst := "sha256:" + sha256Hex(content)
			bh.svc.putErr = tt.se
			resp := bh.serve(http.MethodPost, "/v2/team1/app/blobs/uploads/?digest="+dgst,
				strings.NewReader(string(content)), nil)
			body := readBody(t, resp)
			if resp.StatusCode != tt.se.Code {
				t.Fatalf("monolithic push status = %d, want the verbatim %d (body %s)",
					resp.StatusCode, tt.se.Code, body)
			}
			assertSpecEnvelope(t, resp, body, tt.wantCode, tt.se.Message)
		})
	}
}

// TestVerbatimRenderManifestPut: both landing steps of the manifest publish
// render a StatusError verbatim — step 1 (svc.Put of the body through the
// blob path) and step 2 (svc.PutManifest) — including the Allow header a
// 405 StatusError carries.
func TestVerbatimRenderManifestPut(t *testing.T) {
	t.Run("step 1 body put", func(t *testing.T) {
		bh := newBlobHarness(t)
		fx := newManifestFixture(t, bh)
		se := repo.NewStatusError(http.StatusRequestEntityTooLarge,
			"Repository 'tiny' quota exceeded: used 900 of 1024 bytes.", nil,
			fmt.Errorf("%w: repository tiny holds 900 of 1024 bytes", repo.ErrQuotaExceeded))
		bh.svc.putErr = se
		resp := bh.putManifest(t, "v2", fx.manifest, mediaTypeDockerManifest)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("manifest PUT status = %d, want 413 (body %s)", resp.StatusCode, body)
		}
		assertSpecEnvelope(t, resp, body, ErrCodeDenied, se.Message)
	})

	t.Run("step 2 manifest index", func(t *testing.T) {
		bh := newBlobHarness(t)
		fx := newManifestFixture(t, bh)
		se := repo.NewStatusError(http.StatusConflict,
			"Repository 'pickier' rejected deployment of 'app/manifests/ab': the path matches excludesPattern '**/manifests/**'.",
			nil, fmt.Errorf("%w: pickier/app/manifests/ab hit excludesPattern", repo.ErrPatternRejected))
		bh.svc.putManifestErr = se
		resp := bh.putManifest(t, "v1", fx.manifest, mediaTypeDockerManifest)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("manifest PUT status = %d, want 409 (body %s)", resp.StatusCode, body)
		}
		assertSpecEnvelope(t, resp, body, ErrCodeDenied, se.Message)
	})

	t.Run("405 carries its Allow header", func(t *testing.T) {
		bh := newBlobHarness(t)
		fx := newManifestFixture(t, bh)
		se := repo.NewStatusError(http.StatusMethodNotAllowed,
			"Remote repository 'mirror' is a read-only proxy cache.", nil,
			fmt.Errorf("%w: remote repositories are read-only", repo.ErrRepoTypeNotSupported))
		se.Header = http.Header{"Allow": []string{http.MethodGet}}
		bh.svc.putManifestErr = se
		resp := bh.putManifest(t, "v1", fx.manifest, mediaTypeDockerManifest)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("manifest PUT status = %d, want 405 (body %s)", resp.StatusCode, body)
		}
		if got := resp.Header.Get("Allow"); got != http.MethodGet {
			t.Fatalf("Allow header = %q, want the StatusError's own %q", got, http.MethodGet)
		}
		assertSpecEnvelope(t, resp, body, ErrCodeUnsupported, se.Message)
	})
}

// TestSpecCodeOfVerbatim pins the envelope-code decision (T-111): the
// governance refusals carry DENIED (the official policy-refusal code —
// TOOMANYREQUESTS was rejected as 429-bound), the statuses with canonical
// codes keep them, the rest stay UNKNOWN.
func TestSpecCodeOfVerbatim(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{http.StatusUnauthorized, ErrCodeUnauthorized},
		{http.StatusForbidden, ErrCodeDenied},
		{http.StatusConflict, ErrCodeDenied},              // pattern rejection
		{http.StatusRequestEntityTooLarge, ErrCodeDenied}, // quota exceeded
		{http.StatusMethodNotAllowed, ErrCodeUnsupported}, // virtual/remote write
		{http.StatusInternalServerError, ErrCodeUnknown},  // unmapped
		{http.StatusBadGateway, ErrCodeUnknown},           // unmapped
		{http.StatusInsufficientStorage, ErrCodeUnknown},  // unmapped
	}
	for _, tt := range tests {
		if got := specCodeOfVerbatim(tt.status); got != tt.want {
			t.Errorf("specCodeOfVerbatim(%d) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

// assertSpecEnvelope checks the verbatim render's wire shape: exactly one
// registry error entry, the expected envelope code, the StatusError's own
// message, and the mandatory api-version header.
func assertSpecEnvelope(t *testing.T, resp *http.Response, body []byte, wantCode, wantMessage string) {
	t.Helper()
	if got := resp.Header.Get(HeaderAPIVersion); got != APIVersionValue {
		t.Fatalf("%s header = %q, want %q", HeaderAPIVersion, got, APIVersionValue)
	}
	var env struct {
		Errors []specError `json:"errors"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("error body is not the registry envelope: %v (body %s)", err, body)
	}
	if len(env.Errors) != 1 {
		t.Fatalf("errors entries = %d, want 1 (body %s)", len(env.Errors), body)
	}
	e := env.Errors[0]
	if e.Code != wantCode {
		t.Fatalf("envelope code = %q, want %q (body %s)", e.Code, wantCode, body)
	}
	if e.Message != wantMessage {
		t.Fatalf("envelope message = %q, want the verbatim %q", e.Message, wantMessage)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}

// ---- the end-to-end verdicts (real repo.Service + governance configs) ----

// putManifestOn issues one manifest PUT against a named repository (the
// shared putManifest helper is pinned to team1).
func (bh *blobHarness) putManifestOn(t *testing.T, repoKey, reference string, body []byte, contentType string) *http.Response {
	t.Helper()
	return bh.serve(http.MethodPut, "/v2/"+repoKey+"/app/manifests/"+reference,
		strings.NewReader(string(body)), map[string]string{"Content-Type": contentType})
}

// getManifestOn issues one manifest GET/HEAD against a named repository.
func (bh *blobHarness) getManifestOn(t *testing.T, repoKey, method, reference string) *http.Response {
	t.Helper()
	return bh.serve(method, "/v2/"+repoKey+"/app/manifests/"+reference, nil, nil)
}

// newGovernanceHarness assembles the real stack — real storage engine, real
// metadata store, real repo.Service — over docker repositories seeded with
// governance config blobs (the T-95 gates, driven through the public face
// exactly as the /v2 plane drives them in production).
func newGovernanceHarness(t *testing.T, configs map[string]string) *blobHarness {
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

	table := make(map[string]string, len(configs))
	for key := range configs {
		table[key] = "docker"
	}
	authz := auth.NewFromStore(md, true)
	svc := repo.New(st, md, authz, nil)
	h := New(nil, NewStaticRepoLookup(table), authz, nil, nil,
		Options{AnonymousAccess: true}, nil).
		WithStorage(st, md.Blobs())
	h.svc = svc

	admin := &auth.Principal{Name: "admin", Admin: true}
	for key, cfg := range configs {
		if _, err := svc.CreateRepo(ctx, admin, &metadata.Repo{
			RepoKey: key, Type: repo.TypeLocal, PackageType: "docker", Config: cfg,
		}); err != nil {
			t.Fatalf("CreateRepo(%s, %s): %v", key, cfg, err)
		}
	}
	return &blobHarness{t: t, h: h, svc: nil, store: st, root: root,
		adminName: "admin", admin: true, realMD: md}
}

// govConfigFixture is one coherent small image (config + a config-only
// manifest — the reference gate must pass before the landing steps the
// quota gate sits in).
type govConfigFixture struct {
	cfgBytes  []byte
	cfgDigest string
	m1        []byte
	m1Digest  string
}

func newGovConfigFixture(t *testing.T) *govConfigFixture {
	t.Helper()
	f := &govConfigFixture{
		cfgBytes: []byte(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]},"pad":"` +
			strings.Repeat("c", 30) + `"}`),
	}
	f.cfgDigest = "sha256:" + sha256Hex(f.cfgBytes)
	f.m1 = []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json",`+
			`"config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"%s","size":%d},`+
			`"layers":[],"pad":"%s"}`,
		f.cfgDigest, len(f.cfgBytes), strings.Repeat("m", 60)))
	f.m1Digest = "sha256:" + sha256Hex(f.m1)
	return f
}

// TestGovernanceQuotaDockerPush413 (W26c, the ticket's core): on a quota'd
// docker repository a push that crosses the ceiling answers 413 on the /v2
// plane — both at blob registration (finalize/monolithic) and at manifest
// publish (step 1) — with the quota wording and zero residue; a push that
// fits is untouched (regression).
func TestGovernanceQuotaDockerPush413(t *testing.T) {
	fx := newGovConfigFixture(t)
	// base is the usage after the pair that FITS has landed: the config
	// node plus the manifest body's two nodes (blobs/<hex> and
	// manifests/<hex> — T-95 leftover 2's double count). The ceiling sits
	// 50 bytes above it: any further real content crosses, the landed pair
	// never did.
	base := len(fx.cfgBytes) + 2*len(fx.m1)
	quota := base + 50
	bh := newGovernanceHarness(t, map[string]string{
		"tiny": fmt.Sprintf(`{"quotaBytes":%d}`, quota),
	})

	// Fits: config blob + manifest v1 land normally (the regression leg —
	// the governance plane never disturbs an under-ceiling push).
	resp := bh.serve(http.MethodPost, "/v2/tiny/app/blobs/uploads/?digest="+fx.cfgDigest,
		strings.NewReader(string(fx.cfgBytes)), nil)
	if body := readBody(t, resp); resp.StatusCode != http.StatusCreated {
		t.Fatalf("config blob push status = %d, want 201 (body %s)", resp.StatusCode, body)
	}
	resp = bh.putManifestOn(t, "tiny", "v1", fx.m1, mediaTypeDockerManifest)
	if body := readBody(t, resp); resp.StatusCode != http.StatusCreated {
		t.Fatalf("manifest v1 PUT status = %d, want 201 (body %s)", resp.StatusCode, body)
	}

	// Over the ceiling: a 300-byte layer. Registration refuses and the /v2
	// plane answers the verbatim 413 (the 500 UNKNOWN of T-95 leftover 1).
	layer := []byte(strings.Repeat("L", 300))
	layerDgst := "sha256:" + sha256Hex(layer)
	resp = bh.serve(http.MethodPost, "/v2/tiny/app/blobs/uploads/?digest="+layerDgst,
		strings.NewReader(string(layer)), nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("layer push status = %d, want 413 (body %s)", resp.StatusCode, body)
	}
	assertSpecEnvelope(t, resp, body, ErrCodeDenied, fmt.Sprintf(
		"Repository 'tiny' quota exceeded: used %d of %d bytes; the write to 'app/blobs/%s' needs 300 more bytes.",
		base, quota, strings.TrimPrefix(layerDgst, "sha256:")))

	// Zero residue: the refused blob is not readable on any plane.
	resp = bh.serve(http.MethodGet, "/v2/tiny/app/blobs/"+layerDgst, nil, nil)
	readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("refused layer GET status = %d, want 404", resp.StatusCode)
	}

	// Over the ceiling at manifest publish: a second manifest body (same,
	// present references so the reference gate passes) whose step-1 landing
	// crosses the ceiling → 413 from the publish path.
	m2 := append(append([]byte{}, fx.m1...), ' ', ' ') // same refs, distinct bytes
	if sha256Hex(m2) == sha256Hex(fx.m1) {
		t.Fatal("m2 must differ from m1")
	}
	resp = bh.putManifestOn(t, "tiny", "v2", m2, mediaTypeDockerManifest)
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("manifest v2 PUT status = %d, want 413 (body %s)", resp.StatusCode, body)
	}
	assertSpecEnvelope(t, resp, body, ErrCodeDenied, fmt.Sprintf(
		"Repository 'tiny' quota exceeded: used %d of %d bytes; the write to 'app/blobs/%s' needs %d more bytes.",
		base, quota, strings.TrimPrefix("sha256:"+sha256Hex(m2), "sha256:"), len(m2)))

	// W26c tail: the tag list carries only v1 and the refused manifest is
	// not resolvable — neither by its tag nor by its digest.
	resp = bh.serve(http.MethodGet, "/v2/tiny/app/tags/list", nil, nil)
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"v1"`) ||
		strings.Contains(string(body), `"v2"`) {
		t.Fatalf("tags/list after refusals = %d %s, want v1 only", resp.StatusCode, body)
	}
	resp = bh.getManifestOn(t, "tiny", http.MethodGet, "v2")
	readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("refused manifest by tag status = %d, want 404", resp.StatusCode)
	}
	resp = bh.getManifestOn(t, "tiny", http.MethodGet, "sha256:"+sha256Hex(m2))
	readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("refused manifest by digest status = %d, want 404", resp.StatusCode)
	}
}

// TestGovernancePatternDockerPush409: the includes/excludes gates render
// their 409 verbatim on /v2 — on the blob plane when the layout path is
// excluded, and on the manifest publish when the MANIFEST layout path is
// excluded (the PutManifest gate T-95 placed on the layout path).
func TestGovernancePatternDockerPush409(t *testing.T) {
	t.Run("excluded blob path refuses the upload", func(t *testing.T) {
		bh := newGovernanceHarness(t, map[string]string{"picky": `{"excludesPattern":"**/blobs/**"}`})
		content := []byte("pattern-rejected-layer")
		dgst := "sha256:" + sha256Hex(content)
		resp := bh.serve(http.MethodPost, "/v2/picky/app/blobs/uploads/?digest="+dgst,
			strings.NewReader(string(content)), nil)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("blob push status = %d, want 409 (body %s)", resp.StatusCode, body)
		}
		assertSpecEnvelope(t, resp, body, ErrCodeDenied, fmt.Sprintf(
			"Repository 'picky' rejected deployment of 'app/blobs/%s': the path matches excludesPattern '**/blobs/**' (includesPattern '**/*').",
			strings.TrimPrefix(dgst, "sha256:")))
		resp = bh.serve(http.MethodGet, "/v2/picky/app/blobs/"+dgst, nil, nil)
		readBody(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("refused blob GET status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("excluded manifest path refuses the publish", func(t *testing.T) {
		bh := newGovernanceHarness(t, map[string]string{"pickier": `{"excludesPattern":"**/manifests/**"}`})
		fx := newGovConfigFixture(t)
		// The config blob's path is NOT excluded: it lands through the very
		// same registration the first case just proved refuses.
		resp := bh.serve(http.MethodPost, "/v2/pickier/app/blobs/uploads/?digest="+fx.cfgDigest,
			strings.NewReader(string(fx.cfgBytes)), nil)
		if body := readBody(t, resp); resp.StatusCode != http.StatusCreated {
			t.Fatalf("config blob push status = %d, want 201 (body %s)", resp.StatusCode, body)
		}
		resp = bh.putManifestOn(t, "pickier", "v1", fx.m1, mediaTypeDockerManifest)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("manifest PUT status = %d, want 409 (body %s)", resp.StatusCode, body)
		}
		if !strings.Contains(string(body), "excludesPattern '**/manifests/**'") {
			t.Fatalf("409 body lacks the pattern wording: %s", body)
		}
		// Zero residue on the manifest plane: the image never gained a
		// manifest, so the name is not known to the registry at all (the
		// catalog's no-manifests judgment — NAME_UNKNOWN, not an empty
		// list); and the manifest itself is not resolvable by digest.
		resp = bh.serve(http.MethodGet, "/v2/pickier/app/tags/list", nil, nil)
		body = readBody(t, resp)
		if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), ErrCodeNameUnknown) {
			t.Fatalf("tags/list after refusal = %d %s, want 404 NAME_UNKNOWN (no manifest ever landed)",
				resp.StatusCode, body)
		}
		resp = bh.getManifestOn(t, "pickier", http.MethodGet, fx.m1Digest)
		readBody(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("refused manifest GET status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("unconfigured repository is untouched (regression)", func(t *testing.T) {
		bh := newGovernanceHarness(t, map[string]string{"free": `{}`})
		fx := newGovConfigFixture(t)
		resp := bh.serve(http.MethodPost, "/v2/free/app/blobs/uploads/?digest="+fx.cfgDigest,
			strings.NewReader(string(fx.cfgBytes)), nil)
		if body := readBody(t, resp); resp.StatusCode != http.StatusCreated {
			t.Fatalf("config blob push status = %d, want 201 (body %s)", resp.StatusCode, body)
		}
		resp = bh.putManifestOn(t, "free", "v1", fx.m1, mediaTypeDockerManifest)
		if body := readBody(t, resp); resp.StatusCode != http.StatusCreated {
			t.Fatalf("manifest PUT status = %d, want 201 (body %s)", resp.StatusCode, body)
		}
	})
}

// TestGovernanceRefusedReadsServe404: the download arm of the pattern gate
// stays indistinguishable from a miss on /v2 (the converged dual-value
// ruling) — a governance repository never leaks "exists but excluded".
func TestGovernanceRefusedReadsServe404(t *testing.T) {
	bh := newGovernanceHarness(t, map[string]string{"picky": `{"includesPattern":"**/manifests/**"}`})
	// Only the manifest layout path is includable: the blob plane is
	// refused on WRITE with 409...
	content := []byte("read-plane-probe")
	dgst := "sha256:" + sha256Hex(content)
	resp := bh.serve(http.MethodPost, "/v2/picky/app/blobs/uploads/?digest="+dgst,
		strings.NewReader(string(content)), nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("blob push status = %d, want 409 (body %s)", resp.StatusCode, body)
	}
	// ...and the READ plane answers plain 404 BLOB_UNKNOWN, byte-identical
	// to a repository without governance.
	resp = bh.serve(http.MethodGet, "/v2/picky/app/blobs/"+dgst, nil, nil)
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("refused blob GET status = %d, want 404", resp.StatusCode)
	}
	if !strings.Contains(string(body), ErrCodeBlobUnknown) {
		t.Fatalf("read-plane 404 body = %s, want the plain BLOB_UNKNOWN miss shape", body)
	}
}
