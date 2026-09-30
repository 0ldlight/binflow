package remote

// T-617 (BIN-99) — remote URL userinfo residue faces, negative-test hard
// gate (Security ticket: no negative test != done). A remote repo URL may
// legally embed userinfo (config validation checks scheme/host only,
// internal/repo/config.go:402-413), so every face that renders the URL —
// server log lines, the browse degraded note, the probe's verdict
// messages, the SSRF guard's audit line — must strip it. Each test drives
// a userinfo-bearing URL through a real site and asserts the REDACTED form
// with the credential absent; the fake credentials are placeholders, never
// real secrets. The auth-semantics test pins WHY the ruling is render-side
// redaction (option c) and not parse-layer stripping: URL userinfo is a
// live Basic-auth source when no username/password fields are configured.

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// syncBuffer is a mutex-guarded log sink (slog may write from any
// goroutine the engine dispatches on).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newBufLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, nil))
}

// fake credential placeholders — never real secrets.
const (
	uiUser = "ulogin-t617"
	uiPass = "FAKECRED-T617"
)

// userinfoURL splices the fake userinfo pair into any scheme URL.
func userinfoURL(bare string) string {
	i := strings.Index(bare, "://")
	if i < 0 {
		return bare
	}
	return bare[:i+3] + uiUser + ":" + uiPass + "@" + bare[i+3:]
}

// assertNoUserinfo fails when the rendered text leaks the placeholder pair
// (either the full user:pass form or the Go client's masked user:*** form —
// the username alone is a credential residue too).
func assertNoUserinfo(t *testing.T, rendered, site string) {
	t.Helper()
	if strings.Contains(rendered, uiUser) {
		t.Errorf("%s leaks the userinfo username: %q", site, rendered)
	}
	if strings.Contains(rendered, uiPass) {
		t.Errorf("%s leaks the userinfo password: %q", site, rendered)
	}
}

// newLoggedEnv opens a real engine over a fresh stack whose logger drains
// into the returned buffer — the log-face assertions read it back.
func newLoggedEnv(t *testing.T) (*Engine, *syncBuffer, metadata.Store) {
	t.Helper()
	ctx := context.Background()
	st, err := storage.OpenEngine(t.TempDir(), storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })
	buf := &syncBuffer{}
	eng, err := NewEngine(st, md, EngineOptions{Logger: newBufLogger(buf)})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return eng, buf, md
}

// createRemoteRow writes one remote repo row plus its remote_configs row
// with the given URL (the createRemote shape, URL overridable).
func createRemoteRow(t *testing.T, md metadata.Store, key, packageType, rawURL string) {
	t.Helper()
	now := "2026-09-30T00:00:00Z"
	row := &metadata.Repo{
		RepoKey: key, Type: "remote", PackageType: packageType,
		Config:    `{"url":"` + rawURL + `","username":"","allowPrivateUpstream":true}`,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := md.Repos().Create(context.Background(), row); err != nil {
		t.Fatalf("create repo %s: %v", key, err)
	}
	cfg := &metadata.RemoteConfig{RepoKey: key, URL: rawURL, AllowPrivateUpstream: true}
	if err := md.Remote().CreateConfig(context.Background(), cfg); err != nil {
		t.Fatalf("create remote config %s: %v", key, err)
	}
}

// TestSyncPropsNoSegmentWarnRedactsUserinfo: the content-synchronisation
// WARN on a repo-segment-less URL is a server LOG face — it must carry the
// redacted URL, never the embedded pair (fetcher.go, the T-617 named site).
func TestSyncPropsNoSegmentWarnRedactsUserinfo(t *testing.T) {
	eng, buf, md := newLoggedEnv(t)
	bare := "http://127.0.0.1:1" // no repo segment: the WARN arm fires
	createRemoteRow(t, md, "generic-remote", "generic", userinfoURL(bare))

	cfg, err := md.Remote().GetConfig(context.Background(), "generic-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	eng.syncUpstreamProperties(context.Background(), cfg, defaultPolicy, "generic-remote", "a/b.bin")

	logged := buf.String()
	if !strings.Contains(logged, "no repository segment") {
		t.Fatalf("WARN did not fire; log = %q", logged)
	}
	assertNoUserinfo(t, logged, "sync-props WARN")
	if !strings.Contains(logged, "url="+bare) {
		t.Errorf("WARN url field = %q, want the redacted form %q", logged, bare)
	}
}

// TestBrowseDegradedRedactsUserinfo: a transport fault while enumerating a
// helm remote quotes the upstream URL (username intact, password masked)
// into BOTH the degraded-note face and the WARN log line — both must carry
// the redacted form.
func TestBrowseDegradedRedactsUserinfo(t *testing.T) {
	eng, buf, md := newLoggedEnv(t)
	createRemoteRow(t, md, "helm-r", "helm", userinfoURL("http://127.0.0.1:1"))

	res, err := eng.BrowseRemote(context.Background(), allowAll, "helm-r", "")
	if err != nil {
		t.Fatalf("BrowseRemote: %v", err)
	}
	if res.Degraded == "" {
		t.Fatalf("browse = %+v, want the degraded face", res)
	}
	assertNoUserinfo(t, res.Degraded, "browse degraded note")
	assertNoUserinfo(t, buf.String(), "browse degraded WARN")
}

// TestProbeFacesRedactUserinfo: the repository Test probe's verdict
// messages (PASS wording and transport-fault wording) name the probed URL —
// both must strip userinfo before the REST face renders them.
func TestProbeFacesRedactUserinfo(t *testing.T) {
	e := newFetchEnv(t, nil)
	e.state.files["/"] = "upstream root"

	// PASS face: override URL with embedded userinfo against the live fake.
	ui := userinfoURL(e.srv.URL)
	res, err := TestRepositoryUpstream(context.Background(), e.md, "generic-remote", UpstreamOverride{URL: ui})
	if err != nil {
		t.Fatalf("probe (pass arm): %v", err)
	}
	if !res.OK {
		t.Fatalf("probe (pass arm) = %+v, want ok", res)
	}
	assertNoUserinfo(t, res.Message, "probe pass message")
	if !strings.Contains(res.Message, e.srv.URL) {
		t.Errorf("probe pass message = %q, want the redacted URL form", res.Message)
	}

	// Transport-fault face: dead URL with embedded userinfo.
	res, err = TestRepositoryUpstream(context.Background(), e.md, "generic-remote",
		UpstreamOverride{URL: userinfoURL("http://127.0.0.1:1")})
	if err != nil {
		t.Fatalf("probe (transport arm): %v", err)
	}
	if res.OK || !strings.Contains(res.Message, "connection failed") {
		t.Fatalf("probe (transport arm) = %+v, want the transport family", res)
	}
	assertNoUserinfo(t, res.Message, "probe transport message")
}

// TestGuardRejectRedactsUserinfo: the SSRF guard's WARN audit line and
// rejection error text name the denied target — the arms that render the
// FULL URL (scheme rejection, parse failure) must arrive redacted on both
// faces (shared guard: this also covers the webhook/replication CheckURL
// callers); the loopback arm renders host only and needs no redaction.
func TestGuardRejectRedactsUserinfo(t *testing.T) {
	// Scheme rejection: target rendered as the full URL string.
	buf := &syncBuffer{}
	g := NewGuard(GuardOptions{RepoKey: "r", Logger: newBufLogger(buf)})
	err := g.CheckURL(context.Background(), userinfoURL("ftp://127.0.0.1:9/x"))
	if err == nil || !IsRejection(err) {
		t.Fatalf("CheckURL (scheme arm) = %v, want a rejection", err)
	}
	assertNoUserinfo(t, err.Error(), "guard rejection error")
	assertNoUserinfo(t, buf.String(), "guard WARN audit line")

	// Parse failure: the quoted raw URL must be redacted.
	if err := g.CheckURL(context.Background(), userinfoURL("http://127.0.0.1:9/%zz")); err == nil || IsRejection(err) {
		t.Fatalf("CheckURL (parse arm) = %v, want a plain parse error", err)
	} else {
		assertNoUserinfo(t, err.Error(), "guard parse-error text")
	}
}

// TestURLUserinfoAuthSemantics pins the ruling's auth dependency LIVE (local
// fake upstream, no guessing): Go's transport turns URL userinfo into a
// Basic Authorization header ONLY when none is set, and the client's
// username/password fields set one — so (1) a userinfo-only URL is a live
// credential a parse-layer strip would silently break, and (2) when both
// are configured the FIELDS win and the userinfo is inert.
func TestURLUserinfoAuthSemantics(t *testing.T) {
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	basic := func(user, pass string) string {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
	}
	fetch := func(opts Options) string {
		t.Helper()
		c, err := NewClient(opts)
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		defer c.CloseIdleConnections()
		if _, err := c.Fetch(context.Background(), Request{Path: ""}); err != nil {
			t.Fatalf("Fetch: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(got) == 0 {
			t.Fatal("upstream saw no request")
		}
		return got[len(got)-1]
	}

	// Userinfo only (no fields): the transport sends it as Basic auth.
	if auth := fetch(Options{BaseURL: userinfoURL(srv.URL), AllowPrivateUpstream: true}); auth != basic(uiUser, uiPass) {
		t.Errorf("userinfo-only authorization = %q, want the URL pair's Basic", auth)
	}
	// Fields + userinfo: the explicit header wins; the URL pair is inert.
	if auth := fetch(Options{
		BaseURL: userinfoURL(srv.URL), Username: "fields-t617", Password: "fieldspass-t617",
		AllowPrivateUpstream: true,
	}); auth != basic("fields-t617", "fieldspass-t617") {
		t.Errorf("fields+userinfo authorization = %q, want the fields' Basic (header wins)", auth)
	}
}
