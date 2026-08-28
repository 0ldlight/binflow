package httpapi_test

// T-339's REST legs: the license gate's dual form (Q4 ruling — community
// 403 with the header, pro 200), the copy/move chain over the real content
// plane (sha256/properties 对账), dryRun's zero side effects, the 401
// override special case on the wire, and the routing family (auth door,
// verb door, the flat endpoints' deliberate absence).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// t339Stack is the T-339 assembly: the full management + content plane, the
// manifest WITH the repo-operations slot, and a real license Manager so the
// gate's two forms are one install apart.
type t339Stack struct {
	ts   *httptest.Server
	md   metadata.Store
	mgr  *license.Manager
	keys testKeys
}

func (st *t339Stack) do(method, path, user, pass, body string) (int, string, string) {
	rdr := strings.NewReader(body)
	req, err := http.NewRequest(method, st.ts.URL+path, rdr)
	if err != nil {
		panic(err) // unreachable: fixed-shape test paths
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := st.ts.Client().Do(req)
	if err != nil {
		panic(err) // unreachable: the live listener serves the test's lifetime
	}
	defer resp.Body.Close() //nolint:errcheck // test read
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header.Get("Content-Type")
}

func newT339Stack(t *testing.T) *t339Stack {
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

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	cfg.Security.AnonymousAccess = false
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	seedLicenseUser(t, md, "plain", "plain-pw", "user")
	svc := repo.New(st, md, authSvc, audit.New(md, true))

	k := newTestKeys(t)
	mgr, err := license.New(license.Options{
		Store:      md.Licenses(),
		VerifyKeys: k.keys,
		Audit:      audit.BestEffort(audit.New(md, true)),
	})
	if err != nil {
		t.Fatalf("license.New: %v", err)
	}
	if err := mgr.Load(ctx); err != nil {
		t.Fatalf("license Load: %v", err)
	}

	s := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Auth:     authSvc,
		Authz:    authSvc,
		Metadata: md,
		Repos:    md.Repos(),
		ReposSvc: svc,
		License:  mgr,
		Addons:   productionManifest(),
		Adapters: []adapter.Handler{generic.New(svc, md.Blobs())},
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &t339Stack{ts: ts, md: md, mgr: mgr, keys: k}
}

// t339Msg is one decoded messages[] entry.
type t339Msg struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// t339CopyMessages decodes the CopyOrMoveResult body.
func t339CopyMessages(t *testing.T, body string) []t339Msg {
	t.Helper()
	var parsed struct {
		Messages []t339Msg `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("body is not messages[] JSON: %v (%s)", err, body)
	}
	return parsed.Messages
}

// putContent uploads through the content plane (the generic adapter).
func (st *t339Stack) putContent(t *testing.T, repoKey, path, content string) {
	t.Helper()
	code, body, _ := st.do(http.MethodPut, "/binflow/"+repoKey+"/"+path, adminUser, adminPass, content)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("PUT %s/%s = %d %s", repoKey, path, code, body)
	}
}

// getWithCreds fetches bytes through the content plane.
func (st *t339Stack) getContent(t *testing.T, repoKey, path string) (int, string) {
	t.Helper()
	code, body, _ := st.do(http.MethodGet, "/binflow/"+repoKey+"/"+path, adminUser, adminPass, "")
	return code, body
}

// createRepo creates a local generic repository through the REST plane.
func (st *t339Stack) createRepo(t *testing.T, key string) {
	t.Helper()
	code, body, _ := st.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass,
		`{"rclass":"local","packageType":"generic"}`)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("create repo %s = %d %s", key, code, body)
	}
}

// installPro installs a pro license document through the REST plane. The
// document is signed with the STACK's keys (the Manager verifies against
// them), not a fresh set.
func (st *t339Stack) installPro(t *testing.T) {
	t.Helper()
	spec := st.keys.spec(time.Now().UTC())
	spec.tier = "pro"
	signed := spec.sign(t)
	code, body, _ := st.do(http.MethodPost, "/binflow/api/system/license", adminUser, adminPass, signed)
	if code != http.StatusCreated {
		t.Fatalf("install pro = %d %s", code, body)
	}
}

// TestT339GateDualForm: the Q4 ruling's two shapes — the unlicensed stack
// answers the D4 403 with X-Binflow-License-Required: repo-operations; one
// pro install later the SAME request runs the full chain to 200.
func TestT339GateDualForm(t *testing.T) {
	st := newT339Stack(t)
	st.createRepo(t, "src")
	st.createRepo(t, "dst")
	st.putContent(t, "src", "a.bin", "v")

	const path = "/binflow/api/copy/src/a.bin?to=/dst/a.bin"

	// Community: the 403 + header + tier-naming envelope.
	code, body, _ := st.do(http.MethodPost, path, adminUser, adminPass, "")
	if code != http.StatusForbidden {
		t.Fatalf("community copy = %d %s, want 403", code, body)
	}
	var env struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil || len(env.Errors) == 0 {
		t.Fatalf("community body is not the errors[] envelope: %s", body)
	}
	if !strings.Contains(env.Errors[0].Message,
		"license required: addon 'repo-operations' needs tier 'pro' (current: none)") {
		t.Fatalf("envelope message wrong: %s", env.Errors[0].Message)
	}

	// The header leg needs the raw response: re-issue and inspect.
	hdr := st.doHeaders(t, http.MethodPost, path, adminUser, adminPass)
	if got := hdr.Get("X-Binflow-License-Required"); got != "repo-operations" {
		t.Fatalf("X-Binflow-License-Required = %q, want repo-operations", got)
	}

	// Pro: the same request runs the chain.
	st.installPro(t)
	code, body, _ = st.do(http.MethodPost, path, adminUser, adminPass, "")
	if code != http.StatusOK {
		t.Fatalf("pro copy = %d %s, want 200", code, body)
	}
	msgs := t339CopyMessages(t, body)
	if len(msgs) != 1 || msgs[0].Level != "INFO" ||
		!strings.Contains(msgs[0].Message, "1 artifacts and 0 folders were copied") {
		t.Fatalf("pro summary wrong: %+v", msgs)
	}
}

// TestT339FullChainCopyMove: the wire chain — copy lands the target with
// identical sha256 and properties (the M10 对账), the source survives; move
// removes it. The vendor Content-Type rides both responses.
func TestT339FullChainCopyMove(t *testing.T) {
	st := newT339Stack(t)
	st.installPro(t)
	st.createRepo(t, "src")
	st.createRepo(t, "dst")
	st.putContent(t, "src", "com/acme/lib/1.0/acme.jar", "jar-bytes")

	// Seed properties through the M10 REST arm, then verify they ride.
	code, body, _ := st.do(http.MethodPut,
		"/binflow/api/storage/src/com/acme/lib/1.0/acme.jar?properties=license=apache-2.0,team=registry",
		adminUser, adminPass, "")
	if code != http.StatusNoContent && code != http.StatusOK {
		t.Fatalf("seed properties = %d %s", code, body)
	}

	code, body, ctype := st.do(http.MethodPost,
		"/binflow/api/copy/src/com/acme/lib/1.0/acme.jar?to=/dst/com/acme/lib/1.0/acme.jar",
		adminUser, adminPass, "")
	if code != http.StatusOK {
		t.Fatalf("copy = %d %s", code, body)
	}
	if !strings.Contains(ctype, "application/vnd.org.jfrog.artifactory.storage.CopyOrMoveResult+json") {
		t.Fatalf("Content-Type = %q", ctype)
	}
	if gcode, gbody := st.getContent(t, "dst", "com/acme/lib/1.0/acme.jar"); gcode != 200 || gbody != "jar-bytes" {
		t.Fatalf("target read = %d %q", gcode, gbody)
	}
	if gcode, _ := st.getContent(t, "src", "com/acme/lib/1.0/acme.jar"); gcode != 200 {
		t.Fatalf("copy removed the source: %d", gcode)
	}
	// Properties 对账: the target carries the same set.
	pcode, pbody, _ := st.do(http.MethodGet,
		"/binflow/api/storage/dst/com/acme/lib/1.0/acme.jar?properties", adminUser, adminPass, "")
	if pcode != 200 || !strings.Contains(pbody, "apache-2.0") || !strings.Contains(pbody, "registry") {
		t.Fatalf("target properties = %d %s", pcode, pbody)
	}

	// Move: source disappears, target serves.
	code, body, _ = st.do(http.MethodPost,
		"/binflow/api/move/src/com/acme/lib/1.0/acme.jar?to=/dst/moved/acme.jar",
		adminUser, adminPass, "")
	if code != http.StatusOK {
		t.Fatalf("move = %d %s", code, body)
	}
	if gcode, _ := st.getContent(t, "src", "com/acme/lib/1.0/acme.jar"); gcode != 404 {
		t.Fatalf("move left the source: %d", gcode)
	}
	if gcode, gbody := st.getContent(t, "dst", "moved/acme.jar"); gcode != 200 || gbody != "jar-bytes" {
		t.Fatalf("moved read = %d %q", gcode, gbody)
	}
}

// TestT339DryRunREST: dry=1 keeps the target empty while reporting the
// would-copy summary.
func TestT339DryRunREST(t *testing.T) {
	st := newT339Stack(t)
	st.installPro(t)
	st.createRepo(t, "src")
	st.createRepo(t, "dst")
	st.putContent(t, "src", "d/f.bin", "v")

	code, body, _ := st.do(http.MethodPost,
		"/binflow/api/copy/src/d/f.bin?to=/dst/d/f.bin&dry=1", adminUser, adminPass, "")
	if code != http.StatusOK {
		t.Fatalf("dry copy = %d %s", code, body)
	}
	msgs := t339CopyMessages(t, body)
	if !strings.HasPrefix(msgs[len(msgs)-1].Message, "Dry run for copying") {
		t.Fatalf("no Dry-run prefix: %+v", msgs)
	}
	if gcode, _ := st.getContent(t, "dst", "d/f.bin"); gcode != 404 {
		t.Fatalf("dry run wrote the target: %d", gcode)
	}
}

// TestT339Override401Wire: the §1.3 #5 special case over REST — the response
// status is the last error's 401 and the body carries the override message;
// no WWW-Authenticate challenge (it is a permission answer, not an auth
// prompt).
func TestT339Override401Wire(t *testing.T) {
	st := newT339Stack(t)
	st.installPro(t)
	st.createRepo(t, "src")
	st.createRepo(t, "dst")
	st.putContent(t, "src", "a.bin", "new")
	st.putContent(t, "dst", "a.bin", "old")

	// plain: read src + write dst via a permission target, no delete.
	target := `{"name":"t339","repos":["src","dst"],"includePatterns":["**"],"principals":{"users":{"plain":["read","write"]}}}}`
	code, body, _ := st.do(http.MethodPost, "/binflow/api/v1/permissions", adminUser, adminPass, target)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("create permission = %d %s", code, body)
	}

	code, body, _ = st.do(http.MethodPost, "/binflow/api/copy/src/a.bin?to=/dst/a.bin", "plain", "plain-pw", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("override copy = %d %s, want 401", code, body)
	}
	msgs := t339CopyMessages(t, body)
	if len(msgs) == 0 || !strings.Contains(msgs[0].Message,
		"User doesn't have permissions to override 'dst/a.bin'. Needs delete permissions.") {
		t.Fatalf("override message wrong: %+v", msgs)
	}
	// The old target survives.
	if _, gbody := st.getContent(t, "dst", "a.bin"); gbody != "old" {
		t.Fatalf("target modified despite the 401: %q", gbody)
	}
}

// TestT339RoutingFamily: the route doors — anonymous 401 at the auth gate
// (BEFORE the license question), non-POST 404, the missing-to 400, and the
// flat endpoints' deliberate absence (Artifactory's own default-404 posture,
// spec section 1.6).
func TestT339RoutingFamily(t *testing.T) {
	st := newT339Stack(t)
	st.installPro(t)
	st.createRepo(t, "src")
	st.createRepo(t, "dst")

	if code, body, _ := st.do(http.MethodPost, "/binflow/api/copy/src/x?to=/dst/x", "", "", ""); code != 401 {
		t.Fatalf("anonymous copy = %d %s, want 401", code, body)
	}
	if code, _, _ := st.do(http.MethodGet, "/binflow/api/copy/src/x?to=/dst/x", adminUser, adminPass, ""); code != 404 {
		t.Fatalf("GET copy = %d, want the E-26 404", code)
	}
	if code, body, _ := st.do(http.MethodPost, "/binflow/api/copy/src/x", adminUser, adminPass, ""); code != 400 ||
		!strings.Contains(body, "Target repository key is empty") {
		t.Fatalf("missing to = %d %s, want 400", code, body)
	}
	if code, _, _ := st.do(http.MethodPost, "/binflow/api/flat/copy/src/x?to=/dst/x", adminUser, adminPass, ""); code != 404 {
		t.Fatalf("flat copy = %d, want 404 (the default-off posture)", code)
	}
	// The bare spelling reaches the handler's parameter 400, not a route 404.
	if code, body, _ := st.do(http.MethodPost, "/binflow/api/copy", adminUser, adminPass, ""); code != 400 ||
		!strings.Contains(body, "Source repository key is empty") {
		t.Fatalf("bare copy = %d %s, want the spec 400", code, body)
	}
}

// doHeaders returns the raw response headers for one request.
func (st *t339Stack) doHeaders(t *testing.T, method, path, user, pass string) http.Header {
	t.Helper()
	req, err := http.NewRequest(method, st.ts.URL+path, nil)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := st.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer resp.Body.Close() //nolint:errcheck // header-only read
	return resp.Header
}
