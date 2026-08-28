package httpapi_test

// T-345's REST legs: the license gate's dual form (community D4 403 with
// the header + the capture staying OFF, pro the full chain), the
// delete→trash→restore wire roundtrip (bytes + four-tuple + property
// restoration), empty/clean with the capability door (readonly_admin /
// plain user / anonymous), and the routing family (verb door, E-26 404).

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

// t345Stack is the T-345 assembly: the full management + content plane,
// the manifest WITH the trashcan slot, the feature configured on, and the
// license-plane gate wired like cmd assembly (community = locked).
type t345Stack struct {
	ts   *httptest.Server
	md   metadata.Store
	mgr  *license.Manager
	keys testKeys
}

func (st *t345Stack) do(method, path, user, pass, body string) (int, string, string) {
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

func newT345Stack(t *testing.T) *t345Stack {
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
	seedLicenseUser(t, md, "roat", "roat-pw", "readonly_admin")
	svc := repo.New(st, md, authSvc, audit.New(md, true))
	repo.ConfigureTrash(svc, repo.DefaultTrashConfig())

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
	// The capture-side gate, wired like cmd's trashcanGate: the slot's
	// verdict through the license manager (community = locked — the AC5
	// community form keeps the M11 hard delete).
	repo.AttachTrashGate(svc, t345LicenseGate{mgr: mgr})

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
	return &t345Stack{ts: ts, md: md, mgr: mgr, keys: k}
}

// t345LicenseGate is the test-side trashcanGate (the cmd adapter's shape).
type t345LicenseGate struct{ mgr *license.Manager }

func (g t345LicenseGate) Unlocked(context.Context) bool {
	return g.mgr.State().Tier >= license.TierPro
}

func (st *t345Stack) createRepo(t *testing.T, key string) {
	t.Helper()
	code, body, _ := st.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass,
		`{"rclass":"local","packageType":"generic"}`)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("create repo %s = %d %s", key, code, body)
	}
}

func (st *t345Stack) putContent(t *testing.T, repoKey, path, content string) {
	t.Helper()
	code, body, _ := st.do(http.MethodPut, "/binflow/"+repoKey+"/"+path, adminUser, adminPass, content)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("PUT %s/%s = %d %s", repoKey, path, code, body)
	}
}

// installPro installs a pro license document through the REST plane (the
// t339 posture: the document is signed with the STACK's keys).
func (st *t345Stack) installPro(t *testing.T) {
	t.Helper()
	spec := st.keys.spec(time.Now().UTC())
	spec.tier = "pro"
	signed := spec.sign(t)
	code, body, _ := st.do(http.MethodPost, "/binflow/api/system/license", adminUser, adminPass, signed)
	if code != http.StatusCreated {
		t.Fatalf("install pro = %d %s", code, body)
	}
}

// TestT345GateDualForm: community answers the D4 403 with
// X-Binflow-License-Required: trashcan AND the delete seam stays hard (no
// capture behind the locked gate); one pro install later the SAME request
// runs the full chain.
func TestT345GateDualForm(t *testing.T) {
	st := newT345Stack(t)
	st.createRepo(t, "libs")
	st.putContent(t, "libs", "a.bin", "v")

	const path = "/binflow/api/trash/empty"

	// Community: the 403 + header + tier-naming envelope.
	code, body, _ := st.do(http.MethodPost, path, adminUser, adminPass, "")
	if code != http.StatusForbidden {
		t.Fatalf("community empty = %d %s, want 403", code, body)
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
		"license required: addon 'trashcan' needs tier 'pro' (current: none)") {
		t.Fatalf("envelope message wrong: %s", env.Errors[0].Message)
	}

	// The header leg.
	req, _ := http.NewRequest(http.MethodPost, st.ts.URL+path, nil)
	req.SetBasicAuth(adminUser, adminPass)
	resp, err := st.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	_ = resp.Body.Close() //nolint:errcheck // header-only read
	if got := resp.Header.Get("X-Binflow-License-Required"); got != "trashcan" {
		t.Fatalf("X-Binflow-License-Required = %q, want trashcan", got)
	}

	// The capture side is equally locked: the content-plane DELETE is the
	// M11 hard delete (no auto-trashcan row materializes).
	dcode, dbody, _ := st.do(http.MethodDelete, "/binflow/libs/a.bin", adminUser, adminPass, "")
	if dcode != http.StatusNoContent {
		t.Fatalf("community DELETE = %d %s", dcode, dbody)
	}
	if _, err := st.md.Repos().Get(context.Background(), repo.TrashRepoKey); err == nil {
		t.Fatalf("trash repo materialized behind the locked gate")
	}

	// Pro: the same request empties an (empty) can honestly.
	st.installPro(t)
	code, body, _ = st.do(http.MethodPost, path, adminUser, adminPass, "")
	if code != http.StatusOK {
		t.Fatalf("pro empty = %d %s, want 200", code, body)
	}
}

// TestT345FullChain: AC1+AC2 on the wire — DELETE captures (original 404,
// the can visible with the four-tuple through the standing ?properties
// arm), restore roundtrips the bytes and the original properties, and the
// transaction-size knob is accepted.
func TestT345FullChain(t *testing.T) {
	st := newT345Stack(t)
	st.installPro(t)
	st.createRepo(t, "libs")
	st.putContent(t, "libs", "com/acme/lib.jar", "jar-bytes")

	// An original property rides the roundtrip.
	pcode, pbody, _ := st.do(http.MethodPut,
		"/binflow/api/storage/libs/com/acme/lib.jar?properties=license=apache-2.0",
		adminUser, adminPass, "")
	if pcode != http.StatusNoContent && pcode != http.StatusOK {
		t.Fatalf("seed properties = %d %s", pcode, pbody)
	}

	// Delete: 204 on the wire, 404 afterwards, visible in the can.
	if code, body, _ := st.do(http.MethodDelete, "/binflow/libs/com/acme/lib.jar", adminUser, adminPass, ""); code != http.StatusNoContent {
		t.Fatalf("DELETE = %d %s", code, body)
	}
	if code, _, _ := st.do(http.MethodGet, "/binflow/libs/com/acme/lib.jar", adminUser, adminPass, ""); code != http.StatusNotFound {
		t.Fatalf("original GET after delete = %d, want 404", code)
	}
	pcode, pbody, _ = st.do(http.MethodGet,
		"/binflow/api/storage/auto-trashcan/libs/com/acme/lib.jar?properties",
		adminUser, adminPass, "")
	if pcode != http.StatusOK {
		t.Fatalf("trash properties = %d %s", pcode, pbody)
	}
	for _, frag := range []string{"trash.time", "trash.deletedBy", "trash.originalRepository", "trash.originalPath", "libs", "com/acme/lib.jar"} {
		if !strings.Contains(pbody, frag) {
			t.Fatalf("trash four-tuple missing %q: %s", frag, pbody)
		}
	}

	// Restore: 200 with the copy/move messages[] shape, original path
	// serves identical bytes, properties restored without the markers.
	code, body, ctype := st.do(http.MethodPost,
		"/binflow/api/trash/restore/libs/com/acme/lib.jar?transaction-size=100",
		adminUser, adminPass, "")
	if code != http.StatusOK {
		t.Fatalf("restore = %d %s", code, body)
	}
	if !strings.Contains(ctype, "CopyOrMoveResult") {
		t.Fatalf("restore Content-Type = %q", ctype)
	}
	gcode, gbody, _ := st.do(http.MethodGet, "/binflow/libs/com/acme/lib.jar", adminUser, adminPass, "")
	if gcode != 200 || gbody != "jar-bytes" {
		t.Fatalf("restored read = %d %q", gcode, gbody)
	}
	pcode, pbody, _ = st.do(http.MethodGet,
		"/binflow/api/storage/libs/com/acme/lib.jar?properties", adminUser, adminPass, "")
	if pcode != 200 || !strings.Contains(pbody, "apache-2.0") || strings.Contains(pbody, "trash.") {
		t.Fatalf("restored properties = %d %s", pcode, pbody)
	}
	// The can no longer holds the entry.
	if code, _, _ := st.do(http.MethodGet,
		"/binflow/api/storage/auto-trashcan/libs/com/acme/lib.jar", adminUser, adminPass, ""); code != http.StatusNotFound {
		t.Fatalf("trash entry survived the restore: %d", code)
	}

	// A restore of a missing entry answers 404; transaction-size 0 is a 400.
	if code, body, _ := st.do(http.MethodPost, "/binflow/api/trash/restore/libs/missing.bin", adminUser, adminPass, ""); code != http.StatusNotFound {
		t.Fatalf("restore miss = %d %s", code, body)
	}
	if code, body, _ := st.do(http.MethodPost, "/binflow/api/trash/restore/libs/x?transaction-size=0", adminUser, adminPass, ""); code != http.StatusBadRequest {
		t.Fatalf("transaction-size 0 = %d %s, want 400", code, body)
	}
}

// TestT345EmptyCleanAndGates: the purge pair plus the capability doors.
func TestT345EmptyCleanAndGates(t *testing.T) {
	st := newT345Stack(t)
	st.installPro(t)
	st.createRepo(t, "libs")
	st.putContent(t, "libs", "a.bin", "aaa")
	st.putContent(t, "libs", "d/b.bin", "bbb")
	for _, p := range []string{"a.bin", "d/"} {
		if code, body, _ := st.do(http.MethodDelete, "/binflow/libs/"+p, adminUser, adminPass, ""); code != http.StatusNoContent {
			t.Fatalf("DELETE %s = %d %s", p, code, body)
		}
	}

	// clean one entry: the addressed subtree only.
	code, body, _ := st.do(http.MethodDelete, "/binflow/api/trash/clean/libs/d/", adminUser, adminPass, "")
	if code != http.StatusOK {
		t.Fatalf("clean = %d %s", code, body)
	}
	var sum struct {
		Files int64 `json:"files"`
	}
	if err := json.Unmarshal([]byte(body), &sum); err != nil || sum.Files != 1 {
		t.Fatalf("clean summary = %s (%v)", body, err)
	}
	if code, _, _ := st.do(http.MethodGet, "/binflow/api/storage/auto-trashcan/libs/a.bin", adminUser, adminPass, ""); code != http.StatusOK {
		t.Fatalf("clean purged the sibling: %d", code)
	}

	// The capability doors: readonly_admin and plain user 403, anonymous
	// the 401 challenge (AC4's permission gate).
	for _, who := range []struct{ user, pass string }{
		{"roat", "roat-pw"}, {"plain", "plain-pw"},
	} {
		if code, body, _ := st.do(http.MethodPost, "/binflow/api/trash/empty", who.user, who.pass, ""); code != http.StatusForbidden {
			t.Fatalf("empty by %s = %d %s, want 403", who.user, code, body)
		}
	}
	if code, _, _ := st.do(http.MethodPost, "/binflow/api/trash/empty", "", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("anonymous empty = %d, want 401", code)
	}

	// empty: the zero-residue form.
	code, body, _ = st.do(http.MethodPost, "/binflow/api/trash/empty", adminUser, adminPass, "")
	if code != http.StatusOK {
		t.Fatalf("empty = %d %s", code, body)
	}
	if code, _, _ := st.do(http.MethodGet, "/binflow/api/storage/auto-trashcan/libs/a.bin", adminUser, adminPass, ""); code != http.StatusNotFound {
		t.Fatalf("empty left rows behind: %d", code)
	}

	// The verb door: every other spelling is the E-26 404.
	for _, verb := range []string{http.MethodGet, http.MethodPut} {
		if code, _, _ := st.do(verb, "/binflow/api/trash/empty", adminUser, adminPass, ""); code != http.StatusNotFound {
			t.Fatalf("%s trash/empty = %d, want 404", verb, code)
		}
	}
	if code, _, _ := st.do(http.MethodPost, "/binflow/api/trash/clean/libs/x", adminUser, adminPass, ""); code != http.StatusNotFound {
		t.Fatalf("POST trash/clean = %d, want 404", code)
	}
	if code, _, _ := st.do(http.MethodDelete, "/binflow/api/trash/restore/libs/x", adminUser, adminPass, ""); code != http.StatusNotFound {
		t.Fatalf("DELETE trash/restore = %d, want 404", code)
	}
}
