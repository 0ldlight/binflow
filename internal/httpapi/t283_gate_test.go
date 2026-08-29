package httpapi_test

// T-283's D1~D7 legs over a REAL stack (sqlite metadata, real auth.Service,
// real repo.Service WITH the PackageTypeGate seam wired exactly like cmd
// assembly, real license.Manager on per-test keys — the ADR-mandated
// constructor injection, since a stock binary can verify no document at
// all). The mounted "go" adapter is a pull-through miniature: its GET arm
// LANDS the node through repo.Service on a miss — the architect's risk-1
// seam (a server-internal write behind an HTTP read verb) proven live
// under a DENIED gate.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// t283GoAdapter is the go pilot slot's stand-in until T-285: writes land
// through repo.Service, reads serve — and a read of a MISSING node first
// lands it internally (the pull-through shape: the internal write must
// never meet the HTTP verb face's gate).
type t283GoAdapter struct {
	svc repo.Service
}

func (h *t283GoAdapter) Protocol() string    { return "go" }
func (h *t283GoAdapter) RepoTypes() []string { return []string{repo.TypeLocal} }
func (h *t283GoAdapter) Layout(r *http.Request) (string, string, error) {
	return adapter.Layout(r)
}

func (h *t283GoAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repoKey, path, err := adapter.Layout(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p := adapter.PrincipalFrom(r.Context())
	switch r.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		if _, err := h.svc.Put(r.Context(), p, repoKey, path, strings.NewReader(string(body)),
			storage.BlobRef{}, "application/octet-stream"); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		rc, _, err := h.svc.Get(r.Context(), p, repoKey, path)
		if errors.Is(err, repo.ErrNodeNotFound) {
			// The pull-through landing: a server-internal write behind the
			// READ verb (risk 1). The gate must never see this.
			if _, perr := h.svc.Put(r.Context(), p, repoKey, path,
				strings.NewReader("pulled-through"), storage.BlobRef{},
				"application/octet-stream"); perr != nil {
				http.Error(w, perr.Error(), http.StatusInternalServerError)
				return
			}
			rc, _, err = h.svc.Get(r.Context(), p, repoKey, path)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		defer rc.Close() //nolint:errcheck // test read
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, rc)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// t283Stack is the T-283 assembly: the full management + content plane with
// the cmd-shaped package-type gate wired and the metrics registry mounted.
type t283Stack struct {
	ts   *httptest.Server
	srv  *httpapi.Server
	md   metadata.Store
	keys testKeys
	mgr  *license.Manager
	clk  *t283Clock
	goAd *t283GoAdapter
	reg  *metrics.Registry
}

// t283Clock is the injectable clock (the D6 expiry leg).
type t283Clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *t283Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *t283Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// newT283Stack assembles the stack. disabledCSV feeds the Manager (the
// breaker form); withClock swaps the Manager onto the controllable clock.
func newT283Stack(t *testing.T, disabledCSV string, withClock bool, adapters ...adapter.Handler) *t283Stack {
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
	clk := &t283Clock{now: time.Now().UTC()}
	sink := &logSink{}
	logger := newSlogTo(sink)
	opts := license.Options{
		Store:       md.Licenses(),
		VerifyKeys:  k.keys,
		DisabledCSV: disabledCSV,
		Audit:       audit.BestEffort(audit.New(md, true)),
		Log:         logger,
	}
	if withClock {
		opts.Now = clk.Now
	}
	mgr, err := license.New(opts)
	if err != nil {
		t.Fatalf("license.New: %v", err)
	}
	if err := mgr.Load(ctx); err != nil {
		t.Fatalf("license Load: %v", err)
	}

	// The cmd-shaped D3 weave: one manifest instance, one gate adapter —
	// productionManifest mirrors cmd's addonManifest (TestT282CmdManifest
	// parity pins the cmd side).
	reg := productionManifest()
	repo.AttachPackageTypeGate(svc, t283Gate{reg: reg, ev: mgr})

	goAd := &t283GoAdapter{svc: svc}
	mounted := append([]adapter.Handler{goAd, generic.New(svc, md.Blobs())}, adapters...)
	mreg := metrics.NewRegistry()
	s := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Auth:     authSvc,
		Authz:    authSvc,
		Metadata: md,
		Repos:    md.Repos(),
		ReposSvc: svc,
		License:  mgr,
		Addons:   reg,
		Adapters: mounted,
		Metrics:  mreg,
	}, logger)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &t283Stack{ts: ts, srv: s, md: md, keys: k, mgr: mgr, clk: clk, goAd: goAd, reg: mreg}
}

// t283Gate is the test-side copy of cmd's packageTypeGate adapter (same
// derivation order; the cmd side is pinned by its own unit test).
type t283Gate struct {
	reg *addons.Registry
	ev  addons.Evaluator
}

func (g t283Gate) Verdict(ctx context.Context, packageType string) repo.PackageTypeVerdict {
	st, ok := g.reg.StatusOf(ctx, g.ev, packageType)
	if !ok {
		return repo.PackageTypeVerdict{}
	}
	v := repo.PackageTypeVerdict{Known: true, Unlocked: st.Enable}
	if !st.Enable {
		switch {
		case !g.ev.AddonEnabled(ctx, st.Addon.ID, license.TierCommunity):
			v.Refusal = "disabled by configuration (addons.disabled) — remove the entry and restart to restore"
		case g.ev.State().Tier < st.Addon.MinTier:
			v.Refusal = fmt.Sprintf("license tier '%s' < '%s'", g.ev.State().Tier, st.Addon.MinTier)
		default:
			v.Refusal = "not named in the license addon allowlist"
		}
	}
	return v
}

// do issues one request with optional Basic auth, returning status, body
// and the response headers.
func (st *t283Stack) do(t *testing.T, method, path, user, pass, body string) (int, string, http.Header) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, st.ts.URL+path, rdr)
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
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

// installPro POSTs a fresh pro document through the license REST plane.
func (st *t283Stack) installPro(t *testing.T) {
	t.Helper()
	spec := st.keys.spec(time.Now().UTC())
	spec.tier = "pro"
	if code, body, _ := st.do(t, http.MethodPost, "/binflow/api/system/license", adminUser, adminPass, spec.sign(t)); code != http.StatusCreated {
		t.Fatalf("install pro = %d %s", code, body)
	}
}

func (st *t283Stack) uninstall(t *testing.T) {
	t.Helper()
	if code, body, _ := st.do(t, http.MethodDelete, "/binflow/api/system/license", adminUser, adminPass, ""); code != http.StatusOK {
		t.Fatalf("uninstall = %d %s", code, body)
	}
}

// seedGoRepo writes a go repository row straight through the store (the
// expiry/uninstall posture: a row created while licensed, observed after).
func (st *t283Stack) seedGoRepo(t *testing.T, key string) {
	t.Helper()
	if err := st.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeLocal, PackageType: "go", Config: "{}",
	}); err != nil {
		t.Fatalf("seed go repo: %v", err)
	}
}

// envelopeMessage decodes the errors[] envelope's first message (the JSON
// layer HTML-escapes '<', so raw-body Contains checks on tier clauses would
// lie).
func envelopeMessage(t *testing.T, body string) string {
	t.Helper()
	var env struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("body is not the errors[] envelope: %v (%s)", err, body)
	}
	if len(env.Errors) == 0 {
		t.Fatalf("envelope carries no errors[]: %s", body)
	}
	return env.Errors[0].Message
}

func (st *t283Stack) putRepo(t *testing.T, key, packageType string) (int, string) {
	t.Helper()
	code, body, _ := st.do(t, http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass,
		`{"rclass":"local","packageType":"`+packageType+`"}`)
	return code, body
}

// TestT283ContentGateD1D2 is the seam-1 core: D2's 403+header+envelope on
// write verbs, D1's read pass, and the internal-write exemption (the
// pull-through miniature landing behind a denied gate).
func TestT283ContentGateD1D2(t *testing.T) {
	st := newT283Stack(t, "", false)
	st.seedGoRepo(t, "go-local")
	const node = "/binflow/go-local/example.com/mod/@v/v1.0.0.zip"

	// Unlicensed floor: the write verb refuses with D2's exact form.
	code, body, hdr := st.do(t, http.MethodPut, node, adminUser, adminPass, "bytes")
	if code != http.StatusForbidden {
		t.Fatalf("floor go PUT = %d %s, want 403", code, body)
	}
	if got := hdr.Get("X-Binflow-License-Required"); got != "go" {
		t.Fatalf("D2 header = %q, want go", got)
	}
	if !strings.Contains(body, `license required: addon 'go' needs tier 'pro' (current: none)`) {
		t.Fatalf("D2 message wrong: %s", body)
	}
	if !strings.Contains(body, "errors") {
		t.Fatalf("D2 body is not the errors[] envelope: %s", body)
	}

	// D1 + risk 1: the READ passes AND lands the node through the service
	// (the internal write never met the gate).
	code, body, _ = st.do(t, http.MethodGet, node, adminUser, adminPass, "")
	if code != http.StatusOK || body != "pulled-through" {
		t.Fatalf("floor go GET = %d %q, want 200 pulled-through", code, body)
	}
	// The landed node is durable: a second GET serves it (still unlicensed).
	if code, body, _ = st.do(t, http.MethodGet, node, adminUser, adminPass, ""); code != http.StatusOK || body != "pulled-through" {
		t.Fatalf("second GET = %d %q", code, body)
	}
	// The write verb still refuses after content exists (data present, gate
	// unchanged — degradation never holds data hostage, never unlocks on it).
	if code, _, _ = st.do(t, http.MethodPut, node, adminUser, adminPass, "more"); code != http.StatusForbidden {
		t.Fatalf("PUT after landing = %d, want 403", code)
	}

	// Pro unlocks the write; the read keeps working (nothing flapped).
	st.installPro(t)
	if code, body, _ = st.do(t, http.MethodPut, node, adminUser, adminPass, "real-push"); code != http.StatusOK {
		t.Fatalf("licensed go PUT = %d %s, want 200", code, body)
	}
	if code, body, _ = st.do(t, http.MethodGet, node, adminUser, adminPass, ""); code != http.StatusOK || body != "real-push" {
		t.Fatalf("licensed GET = %d %q", code, body)
	}

	// Uninstall: the write closes again, the read stays open (D1/D2 both
	// directions).
	st.uninstall(t)
	if code, _, hdr = st.do(t, http.MethodPut, node, adminUser, adminPass, "x"); code != http.StatusForbidden || hdr.Get("X-Binflow-License-Required") != "go" {
		t.Fatalf("post-uninstall PUT = %d, want 403 + header", code)
	}
	if code, body, _ = st.do(t, http.MethodGet, node, adminUser, adminPass, ""); code != http.StatusOK || body != "real-push" {
		t.Fatalf("post-uninstall GET = %d %q, want the licensed bytes", code, body)
	}
}

// TestT283CreateGateD3: the configuration plane — community refuses the
// gated slot with the pointed 400, pro unlocks it (the matrix T05/T06 flip
// premise, proven with constructor-injected keys since a stock binary can
// verify no document), uninstall closes it again. The five-core floor never
// moves.
func TestT283CreateGateD3(t *testing.T) {
	st := newT283Stack(t, "", false)

	code, body := st.putRepo(t, "d3-go", "go")
	if code != http.StatusBadRequest {
		t.Fatalf("community go create = %d %s, want 400", code, body)
	}
	if msg := envelopeMessage(t, body); !strings.Contains(msg, "not available on this instance") ||
		!strings.Contains(msg, "license tier 'community' < 'pro'") {
		t.Fatalf("D3 message wrong: %s", msg)
	}
	if code, _, hdr := st.do(t, http.MethodPut, "/binflow/api/repositories/d3-go", adminUser, adminPass,
		`{"rclass":"local","packageType":"go"}`); hdr.Get("X-Binflow-License-Required") != "" {
		t.Fatalf("D3 carries the D2 header (it must not): %d", code)
	}
	// nuget row of the same matrix (T05's twin).
	if code, body = st.putRepo(t, "d3-nuget", "nuget"); code != http.StatusBadRequest || !strings.Contains(body, "nuget") {
		t.Fatalf("community nuget create = %d %s, want the pointed 400", code, body)
	}
	// The five-core floor is untouched by the gate.
	if code, body = st.putRepo(t, "d3-generic", "generic"); code != http.StatusOK {
		t.Fatalf("generic create = %d %s, want 200", code, body)
	}

	st.installPro(t)
	if code, body = st.putRepo(t, "d3-go", "go"); code != http.StatusOK {
		t.Fatalf("licensed go create = %d %s, want 200 (T05/T06 flip premise)", code, body)
	}
	// The row really landed with the gated package type.
	row, err := st.md.Repos().Get(context.Background(), "d3-go")
	if err != nil || row.PackageType != "go" {
		t.Fatalf("stored row = %+v err=%v, want packageType go", row, err)
	}

	// Uninstall: the CONFIGURATION plane closes (update AND create; the
	// delete too — the ticket's 删仓同 ruling) while the row survives.
	st.uninstall(t)
	if code, body = st.putRepo(t, "d3-nuget", "nuget"); code != http.StatusBadRequest {
		t.Fatalf("post-uninstall nuget create = %d %s, want 400", code, body)
	}
	if code, body, _ = st.do(t, http.MethodPost, "/binflow/api/repositories/d3-go", adminUser, adminPass, `{"description":"x"}`); code != http.StatusBadRequest || !strings.Contains(body, "not available") {
		t.Fatalf("locked go update = %d %s, want the D3 400", code, body)
	}
	if code, body, _ = st.do(t, http.MethodDelete, "/binflow/api/repositories/d3-go", adminUser, adminPass, ""); code != http.StatusBadRequest || !strings.Contains(body, "not available") {
		t.Fatalf("locked go delete = %d %s, want the D3 400", code, body)
	}
	if _, err := st.md.Repos().Get(context.Background(), "d3-go"); err != nil {
		t.Fatalf("row vanished behind a refused delete: %v", err)
	}
}

// TestT283VirtualMemberGate: FR-85.1④ — a locked member refuses the virtual
// create on the community floor; pro re-opens it.
func TestT283VirtualMemberGate(t *testing.T) {
	st := newT283Stack(t, "", false)
	if code, body := st.putRepo(t, "vm-gen", "generic"); code != http.StatusOK {
		t.Fatalf("generic member create = %d %s", code, body)
	}
	st.installPro(t)
	if code, body := st.putRepo(t, "vm-go", "go"); code != http.StatusOK {
		t.Fatalf("go member create = %d %s", code, body)
	}
	st.uninstall(t)

	virt := `{"rclass":"virtual","packageType":"generic","repositories":["vm-go","vm-gen"]}`
	code, body, _ := st.do(t, http.MethodPut, "/binflow/api/repositories/vm-virt", adminUser, adminPass, virt)
	if code != http.StatusBadRequest || !strings.Contains(envelopeMessage(t, body), `member 'vm-go'`) {
		t.Fatalf("virtual with locked member = %d %s, want the member-pointed 400", code, body)
	}

	st.installPro(t)
	if code, body, _ = st.do(t, http.MethodPut, "/binflow/api/repositories/vm-virt", adminUser, adminPass, virt); code != http.StatusOK {
		t.Fatalf("licensed virtual create = %d %s, want 200", code, body)
	}
}

// TestT283DisabledBreaker: addons.disabled through the whole Manager (the
// config key's consumer) — the breaker's create/content faces name the knob
// and the recovery path, never carry the license header, and leave reads
// plus every untouched slot alone.
func TestT283DisabledBreaker(t *testing.T) {
	st := newT283Stack(t, "go,npm", false)
	st.seedGoRepo(t, "br-go")
	const node = "/binflow/br-go/x.zip"

	code, body := st.putRepo(t, "br-npm", "npm")
	if code != http.StatusBadRequest || !strings.Contains(body, "disabled by configuration") || !strings.Contains(body, "addons.disabled") {
		t.Fatalf("breaker npm create = %d %s", code, body)
	}
	// Even a pro license does not clear the breaker (the knob outranks the
	// document — the Manager evaluates the disabled set first).
	st.installPro(t)
	if code, body = st.putRepo(t, "br-go2", "go"); code != http.StatusBadRequest || !strings.Contains(body, "disabled by configuration") {
		t.Fatalf("breaker under pro = %d %s, want the disabled 400", code, body)
	}
	code, body, hdr := st.do(t, http.MethodPut, node, adminUser, adminPass, "x")
	if code != http.StatusForbidden || !strings.Contains(body, "disabled by configuration") {
		t.Fatalf("breaker content PUT = %d %s", code, body)
	}
	if got := hdr.Get("X-Binflow-License-Required"); got != "" {
		t.Fatalf("breaker shape carries the license header (it must not): %q", got)
	}
	// D1 holds for the breaker too: the read passes and lands internally.
	if code, body, _ = st.do(t, http.MethodGet, node, adminUser, adminPass, ""); code != http.StatusOK || body != "pulled-through" {
		t.Fatalf("breaker GET = %d %q, want 200", code, body)
	}
	// Untouched slots: generic create and content work normally.
	if code, body = st.putRepo(t, "br-generic", "generic"); code != http.StatusOK {
		t.Fatalf("generic under breaker = %d %s", code, body)
	}
	if code, body, _ = st.do(t, http.MethodPut, "/binflow/br-generic/a.txt", adminUser, adminPass, "x"); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("generic content under breaker = %d %s", code, body)
	}
}

// TestT283V2BreakerArm: the /v2 root exception's write arm — the registry
// plane must not stand outside the gate (POST upload-initiate and PUT
// finalize refuse under the breaker; the ping and the token endpoint pass).
// Since T-342 the arm resolves the repository ROW's package type (the
// plane serves the docker+helmoci family), so the gated write targets a
// SEEDED docker repository; a row the lookup cannot resolve passes through
// to the adapter's own repo gate — the 404 a missing repository answers
// with is not an entitlement verdict (the license question never leaks row
// existence).
func TestT283V2BreakerArm(t *testing.T) {
	st := newT283Stack(t, "docker", false, &t63Handler{proto: "docker"})
	if err := st.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: "myimg", Type: repo.TypeLocal, PackageType: repo.PackageDocker, Config: "{}",
	}); err != nil {
		t.Fatalf("seed docker repo: %v", err)
	}

	code, body, hdr := st.do(t, http.MethodPost, "/v2/myimg/blobs/uploads/", adminUser, adminPass, "")
	if code != http.StatusForbidden || !strings.Contains(body, "disabled by configuration") {
		t.Fatalf("breaker /v2 upload initiate = %d %s, want the disabled 403", code, body)
	}
	if code, _, _ = st.do(t, http.MethodPut, "/v2/myimg/manifests/latest", adminUser, adminPass, "{}"); code != http.StatusForbidden {
		t.Fatalf("breaker /v2 manifest PUT = %d, want 403", code)
	}
	// A write naming a repository the lookup cannot resolve is the
	// adapter's question, not the gate's: the fake answers 200 (the real
	// adapter answers its spec 404).
	if code, _, _ = st.do(t, http.MethodPost, "/v2/missing-repo/blobs/uploads/", adminUser, adminPass, ""); code != http.StatusOK {
		t.Fatalf("breaker /v2 write on a missing row = %d, want the adapter's own answer (200 on the fake)", code)
	}
	// Reads pass (D1) — the fake adapter answers 200.
	if code, _, _ = st.do(t, http.MethodGet, "/v2/myimg/manifests/latest", adminUser, adminPass, ""); code != http.StatusOK {
		t.Fatalf("breaker /v2 GET = %d, want 200", code)
	}
	// The token endpoint is an auth plane: a POST there is not a content
	// write and must reach the adapter (the fake answers 200), never the
	// addon gate.
	if code, _, _ = st.do(t, http.MethodPost, "/v2/token", adminUser, adminPass, ""); code != http.StatusOK {
		t.Fatalf("breaker /v2/token POST = %d, want 200 (auth plane, ungated)", code)
	}
	if hdr.Get("X-T63-Proto") != "" {
		t.Fatal("unexpected header leak")
	}
}

// TestT283RbacPrecedesLicense: the invariant-2 leg — a principal without
// the content write grant meets the RBAC 403, NOT the license 403, on a
// gated slot (the gate sits strictly after the authorization door).
func TestT283RbacPrecedesLicense(t *testing.T) {
	st := newT283Stack(t, "", false)
	st.seedGoRepo(t, "rb-go")
	const node = "/binflow/rb-go/x.zip"

	// Unlicensed + no grant: RBAC answers (the message is the ACL denial,
	// not the license clause).
	if code, body, hdr := st.do(t, http.MethodPut, node, "plain", "plain-pw", "x"); code != http.StatusForbidden || strings.Contains(body, "license required") {
		t.Fatalf("plain-user PUT = %d %s, want the RBAC 403", code, body)
	} else if hdr.Get("X-Binflow-License-Required") != "" {
		t.Fatal("the license header leaked through the RBAC door")
	}
	// Anonymous on a closed instance: the 401 challenge, before any gate.
	if code, _, hdr := st.do(t, http.MethodPut, node, "", "", "x"); code != http.StatusUnauthorized || hdr.Get("X-Binflow-License-Required") != "" {
		t.Fatalf("anonymous PUT = %d, want the 401 challenge", code)
	}
	// Same order on the management plane: the plain user's repo create is
	// the RBAC 403 even for a gated type (the license question never runs).
	mcode, mbody, _ := st.do(t, http.MethodPut, "/binflow/api/repositories/rb-go2", "plain", "plain-pw",
		`{"rclass":"local","packageType":"go"}`)
	if mcode != http.StatusForbidden || strings.Contains(mbody, "license") {
		t.Fatalf("plain-user gated create = %d %s, want RBAC 403", mcode, mbody)
	}
}

// TestT283RequireAddonSeam: weave 3's D4 form — the floor refuses, the
// enterprise tier unlocks, the breaker names the knob. Same 403 shape as
// the data plane.
func TestT283RequireAddonSeam(t *testing.T) {
	st := newT283Stack(t, "", false)
	r := httptest.NewRequest(http.MethodGet, "/binflow/api/v1/cluster/dump", nil)

	w := httptest.NewRecorder()
	if st.srv.RequireAddon(w, r, "ha", license.TierEnterprise) {
		t.Fatal("floor granted the enterprise slot")
	}
	if w.Code != http.StatusForbidden || w.Header().Get("X-Binflow-License-Required") != "ha" {
		t.Fatalf("D4 form wrong: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `addon 'ha' needs tier 'enterprise' (current: none)`) {
		t.Fatalf("D4 message wrong: %s", w.Body.String())
	}

	// Enterprise unlocks; pro does not (the closed tier order).
	spec := st.keys.spec(time.Now().UTC())
	spec.tier = "enterprise"
	if code, body, _ := st.do(t, http.MethodPost, "/binflow/api/system/license", adminUser, adminPass, spec.sign(t)); code != http.StatusCreated {
		t.Fatalf("install enterprise = %d %s", code, body)
	}
	w = httptest.NewRecorder()
	if !st.srv.RequireAddon(w, r, "ha", license.TierEnterprise) || w.Body.Len() != 0 {
		t.Fatalf("enterprise still refused: %d %s", w.Code, w.Body.String())
	}
	st.uninstall(t)
	spec = st.keys.spec(time.Now().UTC())
	spec.tier = "pro"
	if code, body, _ := st.do(t, http.MethodPost, "/binflow/api/system/license", adminUser, adminPass, spec.sign(t)); code != http.StatusCreated {
		t.Fatalf("install pro = %d %s", code, body)
	}
	w = httptest.NewRecorder()
	if st.srv.RequireAddon(w, r, "ha", license.TierEnterprise) || w.Code != http.StatusForbidden {
		t.Fatalf("pro unlocked the enterprise slot: %d", w.Code)
	}
}

// TestT283D6Expiry: the license's window closes under the instance's feet
// (the injected clock crossed expiresAt) — the gates follow on the very
// next request, no restart, no grace.
func TestT283D6Expiry(t *testing.T) {
	st := newT283Stack(t, "", true)
	st.seedGoRepo(t, "d6-go")
	const node = "/binflow/d6-go/x.zip"

	now := time.Now().UTC()
	st.clk.Set(now)
	spec := st.keys.spec(now)
	spec.tier = "pro"
	exp := now.Add(2 * time.Hour).Format(time.RFC3339)
	spec.expiresAt = &exp
	if code, body, _ := st.do(t, http.MethodPost, "/binflow/api/system/license", adminUser, adminPass, spec.sign(t)); code != http.StatusCreated {
		t.Fatalf("install short pro = %d %s", code, body)
	}
	if code, _, _ := st.do(t, http.MethodPut, node, adminUser, adminPass, "x"); code != http.StatusOK {
		t.Fatalf("in-window PUT = %d, want 200", code)
	}

	// Cross the window (leeway is 1h; land comfortably past it).
	st.clk.Set(now.Add(4 * time.Hour))
	if code, body, hdr := st.do(t, http.MethodPut, node, adminUser, adminPass, "x"); code != http.StatusForbidden || hdr.Get("X-Binflow-License-Required") != "go" {
		t.Fatalf("post-expiry PUT = %d %s, want the gated 403", code, body)
	} else if !strings.Contains(body, `(current: community`) && !strings.Contains(body, `(current: none`) {
		t.Fatalf("post-expiry message lost the current clause: %s", body)
	}
	if gcode, _, _ := st.do(t, http.MethodGet, node, adminUser, adminPass, ""); gcode != http.StatusOK {
		t.Fatalf("post-expiry GET = %d, want 200 (D1)", gcode)
	}
	if gcode, gbody := st.putRepo(t, "d6-go2", "go"); gcode != http.StatusBadRequest {
		t.Fatalf("post-expiry create = %d %s, want 400 (D3)", gcode, gbody)
	}
}

// TestT283D7FailedInstallKeepsState: a tampered document answers 400 and
// the in-force pro license keeps gating exactly as before.
func TestT283D7FailedInstallKeepsState(t *testing.T) {
	st := newT283Stack(t, "", false)
	st.seedGoRepo(t, "d7-go")
	const node = "/binflow/d7-go/x.zip"
	st.installPro(t)

	if code, _, _ := st.do(t, http.MethodPut, node, adminUser, adminPass, "x"); code != http.StatusOK {
		t.Fatal("precondition: licensed PUT works")
	}
	bad := tamperPayloadContent(t, st.keys.spec(time.Now().UTC()).sign(t))
	if code, _, _ := st.do(t, http.MethodPost, "/binflow/api/system/license", adminUser, adminPass, bad); code != http.StatusBadRequest {
		t.Fatalf("tampered install = %d, want 400", code)
	}
	if code, _, _ := st.do(t, http.MethodPut, node, adminUser, adminPass, "x"); code != http.StatusOK {
		t.Fatalf("post-refusal PUT = %d, want 200 (D7: state unchanged)", code)
	}
}

// TestT283MetricsAndAudit: the observability legs — the three metric
// families flip with the license, and every refusal leaves a
// license.addon.denied audit row with actor and addon.
func TestT283MetricsAndAudit(t *testing.T) {
	st := newT283Stack(t, "", false)
	st.seedGoRepo(t, "obs-go")

	scrape := func() string {
		t.Helper()
		code, body, _ := st.do(t, http.MethodGet, "/metrics", "", "", "")
		if code != http.StatusOK {
			t.Fatalf("scrape = %d", code)
		}
		return body
	}
	floor := scrape()
	if !strings.Contains(floor, "binflow_license_tier 0\n") {
		t.Fatalf("floor tier gauge missing:\n%s", floor)
	}
	if !strings.Contains(floor, `binflow_addons_enabled{addon="go"} 0`) || !strings.Contains(floor, `binflow_addons_enabled{addon="generic"} 1`) {
		t.Fatalf("floor unlock gauge wrong:\n%s", floor)
	}

	// Two denials (a content write and a repo create).
	if code, _, _ := st.do(t, http.MethodPut, "/binflow/obs-go/x.zip", adminUser, adminPass, "x"); code != http.StatusForbidden {
		t.Fatal("content denial missing")
	}
	if code, _ := st.putRepo(t, "obs-go2", "go"); code != http.StatusBadRequest {
		t.Fatal("config denial missing")
	}
	b := scrape()
	if !strings.Contains(b, `binflow_addon_gate_requests_total{addon="go",decision="deny"}`) {
		t.Fatalf("deny counter missing the content-plane hit:\n%s", b)
	}

	st.installPro(t)
	b = scrape()
	if !strings.Contains(b, "binflow_license_tier 1\n") {
		t.Fatalf("pro tier gauge missing:\n%s", b)
	}
	if !strings.Contains(b, `binflow_addons_enabled{addon="go"} 1`) {
		t.Fatalf("pro unlock gauge wrong:\n%s", b)
	}

	// The audit trail: one row per refusal, actor + addon + tier + path.
	events, err := st.md.Audits().Query(context.Background(), metadata.AuditQuery{})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	denied := 0
	for _, ev := range events {
		if ev.Action != repo.AuditActionAddonDenied {
			continue
		}
		denied++
		if ev.Actor != "admin" {
			t.Fatalf("denied actor = %q", ev.Actor)
		}
		var d map[string]any
		if err := json.Unmarshal([]byte(ev.Detail), &d); err != nil {
			t.Fatalf("denied detail not JSON: %v", err)
		}
		if d["addon"] != "go" {
			t.Fatalf("denied detail wrong: %s", ev.Detail)
		}
		// The content-plane row carries the tier clauses; the config-plane
		// row (repo.Service's) carries the refusal clause.
		if _, ok := d["tier"]; ok {
			if d["tier"] != "none" || d["need"] != "pro" {
				t.Fatalf("content-plane detail wrong: %s", ev.Detail)
			}
			if ev.Path == "" {
				t.Fatalf("content-plane denial lost the node path: %+v", ev)
			}
		} else if _, ok := d["refusal"]; !ok {
			t.Fatalf("config-plane detail lacks the refusal: %s", ev.Detail)
		}
		if ev.RepoKey != "obs-go" && ev.RepoKey != "obs-go2" {
			t.Fatalf("denied repo wrong: %+v", ev)
		}
	}
	if denied != 2 {
		t.Fatalf("license.addon.denied rows = %d, want 2", denied)
	}
}

// TestT283NoTearingUnderFlips: FR-85.2/AC3 — concurrent gated writes while
// the license installs/uninstalls in a loop: every go-content answer is
// exactly 200 or 403 (never a 5xx, never a torn shape), every five-core
// operation succeeds throughout, and the flip is visible on the FIRST
// request after the REST call returns (< 1s, NFR-P44's leg).
func TestT283NoTearingUnderFlips(t *testing.T) {
	st := newT283Stack(t, "", false)
	st.seedGoRepo(t, "tear-go")
	if code, body := st.putRepo(t, "tear-generic", "generic"); code != http.StatusOK {
		t.Fatalf("generic create = %d %s", code, body)
	}
	const goNode = "/binflow/tear-go/x.zip"

	stop := make(chan struct{})
	var wg sync.WaitGroup
	counts := map[int]int{}
	var mu sync.Mutex
	record := func(code int) {
		mu.Lock()
		counts[code]++
		mu.Unlock()
	}
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				req, _ := http.NewRequest(http.MethodPut, st.ts.URL+goNode, strings.NewReader("x"))
				req.SetBasicAuth(adminUser, adminPass)
				resp, err := st.ts.Client().Do(req)
				if err != nil {
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				record(resp.StatusCode)
			}
		}()
	}

	for i := 0; i < 10; i++ {
		st.installPro(t)
		st.uninstall(t)
	}
	close(stop)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(counts) == 0 {
		t.Fatal("no go-content answers recorded")
	}
	for code := range counts {
		if code != http.StatusOK && code != http.StatusForbidden {
			t.Fatalf("torn answer %d observed (only 200/403 are legal): %v", code, counts)
		}
	}
	t.Logf("go-content answers under flips: %v", counts)

	// The five-core plane never failed a single call (implicit: any failure
	// above would have aborted). And the flip is IMMEDIATE:
	st.installPro(t)
	start := time.Now()
	code, _, _ := st.do(t, http.MethodPut, goNode, adminUser, adminPass, "x")
	if code != http.StatusOK {
		t.Fatalf("post-install PUT = %d, want the immediate 200", code)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("flip took %s, want < 1s (NFR-P44)", elapsed)
	}
	st.uninstall(t)
	start = time.Now()
	if code, _, _ = st.do(t, http.MethodPut, goNode, adminUser, adminPass, "x"); code != http.StatusForbidden {
		t.Fatalf("post-uninstall PUT = %d, want the immediate 403", code)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("close took %s, want < 1s", elapsed)
	}
}

// TestT283RealStackCurl: the literal curl leg — the ticket's "真实栈 curl:
// pro license（构造注入）下 go 仓 PUT 通 / community 403 带 gated 头 + GET
// 200", run as actual curl(1) processes against the real TCP server. Skips
// when curl is not on PATH (the Go-client tests above carry the same
// assertions unconditionally).
func TestT283RealStackCurl(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not on PATH")
	}
	st := newT283Stack(t, "", false)
	st.seedGoRepo(t, "curl-go")
	const node = "/binflow/curl-go/x.zip"

	curl := func(args ...string) (string, string) {
		t.Helper()
		full := append([]string{"-sS", "-u", adminUser + ":" + adminPass}, args...)
		cmd := exec.Command("curl", full...)
		var out, errb strings.Builder
		cmd.Stdout = &out
		cmd.Stderr = &errb
		if err := cmd.Run(); err != nil {
			t.Fatalf("curl %v: %v %s", args, err, errb.String())
		}
		return out.String(), errb.String()
	}

	// community: PUT -> 403 with the gated header (and no body consumed).
	body, _ := curl("-o", "/dev/null", "-w", "%{http_code} %{header_json}", "-X", "PUT", "--data", "bytes", st.ts.URL+node)
	if !strings.Contains(strings.ToLower(body), `"x-binflow-license-required":["go"]`) || !strings.Contains(body, "403") {
		t.Fatalf("community curl PUT wrong: %s", body)
	}
	// community: GET -> 200 (the pull-through landing served it).
	body, _ = curl("-X", "GET", st.ts.URL+node)
	if body != "pulled-through" {
		t.Fatalf("community curl GET = %q, want pulled-through", body)
	}
	// pro: PUT -> 200.
	st.installPro(t)
	body, _ = curl("-o", "/dev/null", "-w", "%{http_code}", "-X", "PUT", "--data", "real", st.ts.URL+node)
	if strings.TrimSpace(body) != "200" {
		t.Fatalf("pro curl PUT = %q, want 200", body)
	}
}
