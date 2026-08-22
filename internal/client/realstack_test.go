package client_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/client"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// Cross-validation against the REAL server stack (T-189 AC 4): the client
// package's own fakes can drift from the handlers (that drift is exactly
// what T-166 found), so this file boots the same collaborators cmd wires
// (config -> sqlite metadata -> storage engine -> auth/audit -> repo.Service
// -> generic adapter -> httpapi, the light Deps shape of the cmd test
// precedent) and drives the repo/token/user/artifact chains through
// internal/client over a real httptest listener.
//
// The import set is the sanctioned test-only exception to the package's
// stdlib-only policy (see doc.go); the library itself stays a pure REST
// consumer.

// The evaluation admin seed (metadata.Open with BINFLOW_ADMIN_PASSWORD
// unset) must be deterministic regardless of the developer shell.
const (
	realStackAdminUser = "admin"
	realStackAdminPass = "password"
)

func init() { _ = os.Unsetenv("BINFLOW_ADMIN_PASSWORD") }

// newRealStack assembles the light real Deps stack. Only the HTTP listener
// is httptest; every collaborator behind it is production code.
func newRealStack(t *testing.T) *httptest.Server {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()

	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: filepath.Join(dataDir, "binflow.db")})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir

	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	svc := repo.New(st, md, authSvc, audit.New(md, true))
	genericHandler := generic.New(svc, md.Blobs())

	s := httpapi.New(httpapi.Deps{
		Config:    cfg,
		Auth:      authSvc,
		Authz:     authSvc,
		Metadata:  md,
		Repos:     md.Repos(),
		ReposSvc:  svc,
		Passwords: authSvc,
		Tokens:    authSvc,
		GC:        st,
		DataDir:   dataDir,
		Adapters:  []adapter.Handler{genericHandler},
		Version:   "1.0.0-test",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// basicAuthTransport injects Basic credentials when the request carries no
// Authorization header — the same composition cmd/bf uses for its basic
// arm. internal/client only injects Bearer, so this bootstraps the first
// token over the wire before the client can authenticate itself.
type basicAuthTransport struct{ user, pass string }

func (b *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Authorization") == "" {
		req = req.Clone(req.Context())
		req.SetBasicAuth(b.user, b.pass)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// newBasicClient returns a client that authenticates with Basic credentials.
func newBasicClient(ts *httptest.Server, user, pass string) *client.Client {
	c := client.New()
	c.BaseURL = ts.URL
	c.RetryMax = -1 // no retries: assertions must fail fast, not retry 4xx
	c.HTTP = &http.Client{Transport: &basicAuthTransport{user: user, pass: pass}}
	return c
}

// newTokenClient returns a Bearer-authenticated client.
func newTokenClient(ts *httptest.Server, token string) *client.Client {
	c := client.New()
	c.BaseURL = ts.URL
	c.RetryMax = -1
	c.Token = token
	return c
}

// bootstrapAdminToken mints the evaluation admin's token over the wire with
// Basic auth (the same composition every real-stack test starts from) and
// pins the calibrated create body's decode faces on the way.
func bootstrapAdminToken(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	boot := newBasicClient(ts, realStackAdminUser, realStackAdminPass)
	adminTok, err := boot.CreateToken(context.Background(), client.TokenCreateRequest{
		Username:  realStackAdminUser,
		ExpiresIn: 3600,
	})
	if err != nil {
		t.Fatalf("bootstrap CreateToken: %v", err)
	}
	if adminTok.Token == "" {
		t.Fatal("bootstrap token is empty: access_token did not decode")
	}
	if adminTok.TokenType != "Bearer" || adminTok.Scope != "api:*" {
		t.Errorf("token = %+v, want token_type Bearer and scope api:*", adminTok)
	}
	if adminTok.ExpiresIn != 3600 {
		t.Errorf("expires_in = %d, want 3600", adminTok.ExpiresIn)
	}
	if n, err := strconv.ParseInt(adminTok.TokenID, 10, 64); err != nil || n <= 0 {
		t.Errorf("token_id = %q, want the int64 wire id as positive decimal text", adminTok.TokenID)
	}
	return adminTok.Token
}

// TestClientAgainstRealServerStack drives the four chains (repo, token,
// user, artifact) against the real httpapi assembly. Every assertion below
// is a decode/encode face the T-166 class of drift previously broke.
func TestClientAgainstRealServerStack(t *testing.T) {
	ts := newRealStack(t)
	ctx := context.Background()

	c := newTokenClient(ts, bootstrapAdminToken(t, ts))

	// ---- repo chain: create (200 plain text), read back, list, update ----
	created, err := c.CreateRepo(ctx, client.RepoCreateRequest{
		Key:         "smoke-repo",
		Rclass:      "local",
		PackageType: "generic",
		Description: "T-189 real-stack smoke",
	})
	if err != nil {
		t.Fatalf("CreateRepo against real stack: %v", err)
	}
	if created.Key != "smoke-repo" || created.Rclass != "local" {
		t.Errorf("created = %+v, want the request echo (real body is plain text)", created)
	}

	got, err := c.GetRepo(ctx, "smoke-repo")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.Key != "smoke-repo" || got.Rclass != "local" || got.PackageType != "generic" {
		t.Errorf("GetRepo = %+v, want the repoConfig fields decoded", got)
	}
	if got.Description != "T-189 real-stack smoke" {
		t.Errorf("description = %q, want it round-tripped", got.Description)
	}

	if _, err := c.UpdateRepo(ctx, "smoke-repo", client.RepoCreateRequest{
		PackageType: "generic",
		Description: "T-189 updated",
	}); err != nil {
		t.Fatalf("UpdateRepo (POST plain-text arm): %v", err)
	}
	got, err = c.GetRepo(ctx, "smoke-repo")
	if err != nil {
		t.Fatalf("GetRepo after update: %v", err)
	}
	if got.Description != "T-189 updated" {
		t.Errorf("description after update = %q, want %q", got.Description, "T-189 updated")
	}

	repos, err := c.ListRepos(ctx)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 1 || repos[0].Key != "smoke-repo" || repos[0].Rclass != "local" {
		t.Errorf("ListRepos = %+v, want the list body's \"type\" decoded into rclass", repos)
	}

	// ---- artifact chain: upload, download, item info, list ----
	content := []byte("hello binflow from T-189\n")
	sum := sha256.Sum256(content)
	wantSha := hex.EncodeToString(sum[:])

	if err := c.UploadArtifact(ctx, "smoke-repo", "docs/hello.txt",
		bytes.NewReader(content), int64(len(content)), "text/plain"); err != nil {
		t.Fatalf("UploadArtifact: %v", err)
	}

	rc, err := c.DownloadArtifact(ctx, "smoke-repo", "docs/hello.txt")
	if err != nil {
		t.Fatalf("DownloadArtifact: %v", err)
	}
	down, readErr := io.ReadAll(rc)
	_ = rc.Close()
	if readErr != nil {
		t.Fatalf("read download: %v", readErr)
	}
	if !bytes.Equal(down, content) {
		t.Errorf("downloaded %q, want %q", down, content)
	}

	info, err := c.GetArtifactInfo(ctx, "smoke-repo", "docs/hello.txt")
	if err != nil {
		t.Fatalf("GetArtifactInfo: %v", err)
	}
	if info.Size != strconv.FormatInt(int64(len(content)), 10) {
		t.Errorf("size = %q, want %q", info.Size, strconv.FormatInt(int64(len(content)), 10))
	}
	if info.Sha256 != wantSha {
		t.Errorf("sha256 = %q, want the server-computed %q folded from the nested checksums", info.Sha256, wantSha)
	}
	if info.MimeType != "text/plain" {
		t.Errorf("mimeType = %q, want %q", info.MimeType, "text/plain")
	}

	listing, err := c.ListArtifacts(ctx, "smoke-repo", "docs")
	if err != nil {
		t.Fatalf("ListArtifacts: %v (int64 size into a string field is the T-166 bug class)", err)
	}
	if len(listing.Files) != 1 {
		t.Fatalf("files = %+v, want the single uploaded file", listing.Files)
	}
	if listing.Files[0].URI != "hello.txt" || listing.Files[0].Folder || listing.Files[0].Size != int64(len(content)) {
		t.Errorf("file entry = %+v, want uri hello.txt, folder false, int64 size %d", listing.Files[0], len(content))
	}

	// ---- user chain: create (201 empty), read back, list, rotate password ----
	if _, err := c.CreateUser(ctx, client.UserCreateRequest{
		Name:     "ci-user",
		Password: "ci-pass-123",
		Email:    "ci@example.com",
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u, err := c.GetUser(ctx, "ci-user")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.Name != "ci-user" || u.Email != "ci@example.com" || u.Admin {
		t.Errorf("GetUser = %+v, want the userDetail fields decoded", u)
	}
	if u.Realm != "internal" {
		t.Errorf("realm = %q, want %q", u.Realm, "internal")
	}

	users, err := c.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("user count = %d (%v), want admin + ci-user", len(users), users)
	}

	if err := newBasicClient(ts, "ci-user", "ci-pass-123").
		ChangeSelfPassword(ctx, "ci-pass-123", "ci-pass-456"); err != nil {
		t.Fatalf("ChangeSelfPassword with the oldPassword spelling: %v", err)
	}
	if err := newBasicClient(ts, "ci-user", "ci-pass-456").
		ChangeSelfPassword(ctx, "wrong-old", "ci-pass-789"); err == nil {
		t.Fatal("ChangeSelfPassword with a wrong old password must fail")
	} else if !strings.Contains(err.Error(), "Incorrect username/password") {
		t.Errorf("error = %q, want the real plain-text wording", err)
	}

	// ---- token chain: admin mints for a subject, non-admin guardrails,
	// revoke, revoked-token 401 ----
	ciTok, err := c.CreateToken(ctx, client.TokenCreateRequest{
		Username:  "ci-user",
		ExpiresIn: 3600,
	})
	if err != nil {
		t.Fatalf("CreateToken for ci-user as admin: %v", err)
	}
	if ciTok.Token == "" || ciTok.TokenID == "" {
		t.Errorf("token = %+v, want access_token and token_id decoded", ciTok)
	}

	ci := newTokenClient(ts, ciTok.Token)
	if _, err := ci.ListRepos(ctx); err == nil {
		t.Fatal("non-admin ListRepos must be denied")
	} else {
		var se *client.StatusError
		if !errors.As(err, &se) || se.StatusCode != http.StatusForbidden {
			t.Errorf("non-admin ListRepos error = %v, want 403 StatusError", err)
		}
	}

	// T-190 model: a non-admin may mint for ITSELF with a finite TTL.
	selfTok, err := ci.CreateTokenForm(ctx, client.TokenCreateRequest{
		Username:  "ci-user",
		ExpiresIn: 60,
	})
	if err != nil {
		t.Fatalf("non-admin self CreateTokenForm: %v", err)
	}
	if selfTok.Token == "" || selfTok.ExpiresIn != 60 {
		t.Errorf("self token = %+v, want access_token and expires_in 60", selfTok)
	}

	// Revoke by id (admin), then prove the revoked token no longer
	// authenticates: the 401 round-trips as a StatusError.
	if err := c.RevokeToken(ctx, client.TokenRevokeRequest{TokenID: ciTok.TokenID}); err != nil {
		t.Fatalf("RevokeToken by id: %v", err)
	}
	if _, err := ci.ListRepos(ctx); err == nil {
		t.Fatal("revoked token must no longer authenticate")
	} else {
		var se *client.StatusError
		if !errors.As(err, &se) || se.StatusCode != http.StatusUnauthorized {
			t.Errorf("revoked-token error = %v, want 401 StatusError", err)
		}
	}
	// Revoking the value form works too (the second, self-minted token).
	if err := c.RevokeToken(ctx, client.TokenRevokeRequest{Token: selfTok.Token}); err != nil {
		t.Fatalf("RevokeToken by value: %v", err)
	}

	// ---- cleanup: delete artifact then the now-empty repository ----
	if err := c.DeleteArtifact(ctx, "smoke-repo", "docs/hello.txt"); err != nil {
		t.Fatalf("DeleteArtifact: %v", err)
	}
	if err := c.DeleteRepo(ctx, "smoke-repo"); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}
	if _, err := c.GetRepo(ctx, "smoke-repo"); err == nil {
		t.Fatal("expected 404 after delete, got nil")
	}
}

// TestRepoConfigEncodeRoundTripRealStack is the T-191 AC 3 cross-validation:
// virtual members and local path patterns written through the client must
// survive the REAL server's create → store → GET path value-for-value. The
// pre-alignment spellings (members/includes/excludes) are additionally proven
// illegible on the real wire: the server drops them, and a member-less
// virtual create is refused outright (repo.Service demands at least one
// member) — the loud face of the T-167 finding; the patterns variant is the
// silent one (a local create simply loses them).
func TestRepoConfigEncodeRoundTripRealStack(t *testing.T) {
	ts := newRealStack(t)
	ctx := context.Background()
	c := newTokenClient(ts, bootstrapAdminToken(t, ts))

	// Two local members first (the real member rules demand existence and
	// forbid nested virtuals); one carries path patterns.
	if _, err := c.CreateRepo(ctx, client.RepoCreateRequest{
		Key:         "rs-libs-local",
		Rclass:      "local",
		PackageType: "generic",
		Description: "member one",
		Includes:    "**/*.jar;**/*.war",
		Excludes:    "internal/**",
	}); err != nil {
		t.Fatalf("CreateRepo local with patterns: %v", err)
	}
	if _, err := c.CreateRepo(ctx, client.RepoCreateRequest{
		Key: "rs-libs-rel", Rclass: "local", PackageType: "generic",
	}); err != nil {
		t.Fatalf("CreateRepo second member: %v", err)
	}

	if _, err := c.CreateRepo(ctx, client.RepoCreateRequest{
		Key:         "rs-libs-virtual",
		Rclass:      "virtual",
		PackageType: "generic",
		Members:     []string{"rs-libs-local", "rs-libs-rel"},
	}); err != nil {
		t.Fatalf("CreateRepo virtual with members: %v", err)
	}

	// Virtual readback: the canonical config's repositories, order included.
	got, err := c.GetRepo(ctx, "rs-libs-virtual")
	if err != nil {
		t.Fatalf("GetRepo virtual: %v", err)
	}
	if got.Rclass != "virtual" {
		t.Errorf("rclass = %q, want virtual", got.Rclass)
	}
	if want := []string{"rs-libs-local", "rs-libs-rel"}; len(got.Members) != 2 ||
		got.Members[0] != want[0] || got.Members[1] != want[1] {
		t.Errorf("members = %v, want %v round-tripped through the stored canonical config", got.Members, want)
	}

	// Local readback: the passthrough patterns survive verbatim.
	local, err := c.GetRepo(ctx, "rs-libs-local")
	if err != nil {
		t.Fatalf("GetRepo local: %v", err)
	}
	if local.Includes != "**/*.jar;**/*.war" || local.Excludes != "internal/**" {
		t.Errorf("patterns = %q / %q, want them round-tripped through the passthrough config",
			local.Includes, local.Excludes)
	}

	// Negative evidence: the pre-alignment spelling is illegible to the real
	// server. A virtual create whose body says "members" transports no
	// repositories into the config blob, and repo.Service refuses a
	// member-less virtual with 400.
	legacyBody := `{"key":"rs-legacy-spelling","rclass":"virtual","packageType":"generic","members":["rs-libs-local"]}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		ts.URL+"/binflow/api/repositories/rs-legacy-spelling", strings.NewReader(legacyBody))
	if err != nil {
		t.Fatalf("build legacy-spelling request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("legacy-spelling PUT: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("legacy-spelling virtual create status = %d (body %s), want 400: the server never read %q",
			resp.StatusCode, raw, "members")
	}

	// Cleanup: the virtual first (it references the members).
	if err := c.DeleteRepo(ctx, "rs-libs-virtual"); err != nil {
		t.Fatalf("DeleteRepo virtual: %v", err)
	}
	for _, key := range []string{"rs-libs-local", "rs-libs-rel"} {
		if err := c.DeleteRepo(ctx, key); err != nil {
			t.Fatalf("DeleteRepo %s: %v", key, err)
		}
	}
}
