package npm

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// stack is the real-collaborator unit harness: sqlite metadata, the real
// storage engine, the real repo.Service and auth.Service behind the real
// handler. Only the HTTP listener is a recorder — the handler is invoked
// directly with the principal injected through the adapter seam, which is
// what httpapi hands it after its own middleware chain.
type stack struct {
	t   *testing.T
	h   *Handler
	svc repo.Service
	md  metadata.Store
}

// admin/devel are the seeded principals. admin passes every grant (the ACL
// model's rule 1); devel holds no permission target, so the service's write
// gate denies — the publish chain's step-3 arm.
var (
	adminPrincipal = &Principal{Name: "admin", Admin: true}
	develPrincipal = &Principal{Name: "devel", Admin: false}
)

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	authSvc := auth.NewFromStore(md, true)
	svc := repo.New(st, md, authSvc, nil)
	if err := md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "npm-local", Type: repo.TypeLocal, PackageType: Protocol,
	}); err != nil {
		t.Fatalf("seed npm repo: %v", err)
	}
	if err := md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "generic-local", Type: repo.TypeLocal, PackageType: "generic",
	}); err != nil {
		t.Fatalf("seed generic repo: %v", err)
	}
	hash, err := auth.HashPassword("devpass")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := md.Users().Create(ctx, &metadata.User{
		Username: "devel", PasswordHash: hash, IsAdmin: false, Enabled: true,
	}); err != nil {
		t.Fatalf("seed devel: %v", err)
	}

	h := New(svc, md.Repos(), Options{BaseURL: "http://registry.test"}).
		WithAuth(authSvc, md.Users(), authSvc).
		WithLedger(md.Blobs())
	h.clock = func() string { return "2026-08-19T00:00:00Z" }
	return &stack{t: t, h: h, svc: svc, md: md}
}

// call drives one request through the handler with p as the principal.
// target carries the REPO KEY as its first segment ("/npm-local/<rest>") —
// the exact shape httpapi hands over after stripping /binflow. Percent
// spellings survive verbatim (httptest.NewRequest keeps RawPath).
func (s *stack) call(method, target string, body string, p *Principal, hdr map[string]string) *httptest.ResponseRecorder {
	s.t.Helper()
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	req := httptest.NewRequest(method, "http://registry.test"+target, rdr)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	req = req.WithContext(withTestPrincipal(req.Context(), p))
	rr := httptest.NewRecorder()
	s.h.ServeHTTP(rr, req)
	return rr
}

// bodyOf returns the recorded body.
func bodyOf(rr *httptest.ResponseRecorder) string { return rr.Body.String() }

// publishDoc builds a publish document for one version, computing honest
// digests of the tarball payload.
func publishDoc(name, version, tarball string, tags map[string]string, distOverrides map[string]any) map[string]any {
	sha1Sum, _, sha512Sum := digestTriple([]byte(tarball))
	integrity := "sha512-" + base64RawURL(sha512Sum)
	dist := map[string]any{
		"integrity": integrity,
		"shasum":    hexEncode(sha1Sum),
		"tarball":   "http://registry.test/binflow/api/npm/npm-local/" + tarballPath(name, version),
	}
	for k, v := range distOverrides {
		dist[k] = v
	}
	if tags == nil {
		tags = map[string]string{"latest": version}
	}
	tagAny := map[string]any{}
	for k, v := range tags {
		tagAny[k] = v
	}
	return map[string]any{
		"_id": name, "name": name,
		"description": "test package",
		"dist-tags":   tagAny,
		"versions": map[string]any{
			version: map[string]any{
				"_id": name + "@" + version, "name": name, "version": version,
				"dist": dist,
			},
		},
		"_attachments": map[string]any{
			name + "-" + version + ".tgz": map[string]any{
				"content_type": "application/octet-stream",
				"data":         base64Std([]byte(tarball)),
				"length":       len(tarball),
			},
		},
	}
}
