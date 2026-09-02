package replication_test

// T-422 (FR-138.2) — the connection probe's unit contract (§9.2-C/§9.3):
// the verdict matrix (200/302 pass, everything else carries the anchored
// Connection-failed wording with the target's reason inline), the probe
// ADDRESS ({TargetURL}/binflow/api/storage/{TargetRepo}, one GET — the
// zero-side-effect leg), the -cache/malformed refusals that never touch the
// wire, the unreachable arm, the credential ride (right pair passes, wrong
// pair refuses), and NFR-S75's credentials-never-logged.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/replication"
	"github.com/lzwzzy/binflow/internal/repo"
)

// probeEnv is an engine over the real 009 store with a scripted target.
type probeEnv struct {
	eng    *replication.Engine
	store  replication.Store
	server *httptest.Server
	// hits counts every request the target saw.
	hits *int
	// logBuf collects the engine's log output (NFR-S75 assertions).
	logBuf *bytes.Buffer
}

func newProbeEnv(t *testing.T, target http.HandlerFunc, cipher *remote.Cipher) *probeEnv {
	t.Helper()
	server := httptest.NewServer(target)
	t.Cleanup(server.Close)
	_, store := openStore(t)
	hits := 0
	logBuf := &bytes.Buffer{}
	mu := &sync.Mutex{}
	eng, err := replication.NewEngine(store, &fakeBlobs{}, replication.EngineOptions{
		Cipher: cipher,
		Logger: slog.New(slog.NewTextHandler(&syncWriter{b: logBuf, mu: mu}, nil)),
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return &probeEnv{eng: eng, store: store, server: server, hits: &hits, logBuf: logBuf}
}

// syncWriter serializes the slog handler's writes for the buffer capture.
type syncWriter struct {
	b  *bytes.Buffer
	mu *sync.Mutex
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

// cfgFor builds the candidate config the probe addresses.
func cfgFor(url, repo, user, passEnc string) *replication.ReplicationConfig {
	c := fixtureConfig("dr-test")
	c.TargetURL = url
	c.TargetRepo = repo
	c.TargetUsername = user
	c.TargetPasswordEnc = passEnc
	return c
}

// statusTarget answers one fixed status with an optional reason body.
func statusTarget(status int, reason string, hits *int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if hits != nil {
			*hits++
		}
		w.WriteHeader(status)
		if reason != "" {
			_, _ = w.Write([]byte(reason))
		}
	}
}

func TestProbeVerdictMatrix(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		reason     string
		wantOK     bool
		wantSubstr string
	}{
		{"200 passes", http.StatusOK, "", true, "tested successfully"},
		{"302 passes", http.StatusFound, "", true, "tested successfully"},
		{"401 refuses the credentials", http.StatusUnauthorized, "Bad Credentials",
			false, "Connection failed: Target replication URL returned error 401: Bad Credentials"},
		{"404 carries the reason", http.StatusNotFound, "Failed to find the repository 'mirror'.",
			false, "Connection failed: Target replication URL returned error 404: Failed to find the repository 'mirror'."},
		{"502 carries the reason", http.StatusBadGateway, "upstream boom",
			false, "returned error 502: upstream boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hits := 0
			e := newProbeEnv(t, statusTarget(tc.status, tc.reason, &hits), nil)
			res, err := e.eng.TestTarget(context.Background(),
				cfgFor(e.server.URL, "mirror", "", ""), replication.TargetOverride{})
			if err != nil {
				t.Fatalf("TestTarget: %v", err)
			}
			if res.OK != tc.wantOK {
				t.Fatalf("ok = %t (message %q), want %t", res.OK, res.Message, tc.wantOK)
			}
			if !strings.Contains(res.Message, tc.wantSubstr) {
				t.Fatalf("message = %q, want it to contain %q", res.Message, tc.wantSubstr)
			}
			if res.StatusCode != tc.status {
				t.Fatalf("status_code = %d, want %d", res.StatusCode, tc.status)
			}
			if hits != 1 {
				t.Fatalf("target hits = %d, want exactly one probe request", hits)
			}
		})
	}
}

// TestProbeAddressAndVerb: the probe addresses the storage-info mount of the
// target repository with ONE GET (the BinFlow mapping of §9.2-C-5's HEAD —
// read-only by construction, zero side effects upstream and at home).
func TestProbeAddressAndVerb(t *testing.T) {
	var method, path string
	hits := 0
	e := newProbeEnv(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusOK)
	}, nil)
	res, err := e.eng.TestTarget(context.Background(),
		cfgFor(e.server.URL, "mirror", "", ""), replication.TargetOverride{})
	if err != nil || !res.OK {
		t.Fatalf("probe = (%+v, %v), want a pass", res, err)
	}
	if method != http.MethodGet {
		t.Fatalf("probe verb = %s, want GET (the read-only storage-info mount)", method)
	}
	if path != "/binflow/api/storage/mirror" {
		t.Fatalf("probe path = %q, want /binflow/api/storage/mirror", path)
	}
	if hits != 1 {
		t.Fatalf("hits = %d, want 1", hits)
	}
	// Zero side effects at home: no ledger row, no config change.
	if tasks, terr := e.store.ListTasks(context.Background(), 1, 100); terr != nil || len(tasks) != 0 {
		t.Fatalf("probe wrote task rows: %d (%v)", len(tasks), terr)
	}
}

// TestProbeCredentialRide: a password-bearing draft override ships the body's
// pair (the stored secret untouched); the target's verdict follows the pair.
func TestProbeCredentialRide(t *testing.T) {
	const user, pass = "repl-user", "s3cret-value"
	e := newProbeEnv(t, func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != user || p != pass {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("invalid credentials"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}, nil)
	ctx := context.Background()

	// The correct pair passes; the wrong pair fails with the 401 inline —
	// and the sealed-store arm stays anonymous-free (no stored secret here).
	res, err := e.eng.TestTarget(ctx, cfgFor(e.server.URL, "mirror", "", ""),
		replication.TargetOverride{TargetUsername: user, TargetPassword: pass})
	if err != nil || !res.OK {
		t.Fatalf("correct-credential probe = (%+v, %v), want a pass", res, err)
	}
	res, err = e.eng.TestTarget(ctx, cfgFor(e.server.URL, "mirror", "", ""),
		replication.TargetOverride{TargetUsername: user, TargetPassword: "wrong"})
	if err != nil {
		t.Fatalf("wrong-credential probe err: %v", err)
	}
	if res.OK || !strings.Contains(res.Message, "error 401") {
		t.Fatalf("wrong-credential verdict = %+v, want the inline 401", res)
	}

	// NFR-S75: the password never reaches the log stream.
	if strings.Contains(e.logBuf.String(), pass) {
		t.Fatalf("the probe password leaked into the engine log: %q", e.logBuf.String())
	}
}

// TestProbeStoredCredentialUnsealed: a stored enc:v1 secret rides the probe
// when the cipher is wired; without one the honest unseal error surfaces.
func TestProbeStoredCredentialUnsealed(t *testing.T) {
	const user, pass = "repl-user", "sealed-secret"
	cipher, err := remote.NewCipher(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	enc, err := cipher.Encrypt(pass)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	e := newProbeEnv(t, func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != user || p != pass {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}, cipher)
	res, err := e.eng.TestTarget(context.Background(),
		cfgFor(e.server.URL, "mirror", user, enc), replication.TargetOverride{})
	if err != nil || !res.OK {
		t.Fatalf("sealed-credential probe = (%+v, %v), want a pass", res, err)
	}

	// Same row, no cipher: the 500 family, sealed text never echoed.
	bare := newProbeEnv(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}, nil)
	_, err = bare.eng.TestTarget(context.Background(),
		cfgFor(bare.server.URL, "mirror", user, enc), replication.TargetOverride{})
	if err == nil || !strings.Contains(err.Error(), "could not be unsealed") {
		t.Fatalf("no-cipher probe err = %v, want the unseal refusal", err)
	}
	if strings.Contains(err.Error(), enc) {
		t.Fatalf("the unseal refusal echoed the sealed text: %v", err)
	}
}

// TestProbeRefusalsBeforeWire: the §9.2-C-4 -cache refusal and the URL/形状
// refusals never touch the network.
func TestProbeRefusalsBeforeWire(t *testing.T) {
	hits := 0
	e := newProbeEnv(t, statusTarget(http.StatusOK, "", &hits), nil)
	ctx := context.Background()
	cases := []struct {
		name string
		cfg  *replication.ReplicationConfig
		ov   replication.TargetOverride
		want string
	}{
		{"cache target refused", cfgFor(e.server.URL, "mirror-cache", "", ""),
			replication.TargetOverride{}, "Replication to remote cache repositories is not allowed."},
		{"cache override refused", cfgFor(e.server.URL, "mirror", "", ""),
			replication.TargetOverride{TargetRepo: "dr-cache"}, "not allowed"},
		{"malformed url refused", cfgFor("not-a-url", "mirror", "", ""),
			replication.TargetOverride{}, "must be an absolute http/https URL"},
		{"empty repo refused", cfgFor(e.server.URL, "", "", ""),
			replication.TargetOverride{}, "target repository key is empty"},
		{"dot segment refused", cfgFor(e.server.URL, "mirror/../x", "", ""),
			replication.TargetOverride{}, "illegal path segment"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := e.eng.TestTarget(ctx, tc.cfg, tc.ov)
			if err != nil {
				t.Fatalf("TestTarget: %v", err)
			}
			if res.OK || !strings.Contains(res.Message, tc.want) {
				t.Fatalf("verdict = %+v, want refusal containing %q", res, tc.want)
			}
		})
	}
	if hits != 0 {
		t.Fatalf("refused probes contacted the target %d times, want 0", hits)
	}
}

// TestProbeUnreachable: a closed port answers the transport-refusal wording
// (§9.2-C-8's family).
func TestProbeUnreachable(t *testing.T) {
	e := newProbeEnv(t, func(_ http.ResponseWriter, _ *http.Request) {}, nil)
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	res, err := e.eng.TestTarget(context.Background(),
		cfgFor(deadURL, "mirror", "", ""), replication.TargetOverride{})
	if err != nil {
		t.Fatalf("TestTarget: %v", err)
	}
	if res.OK || !strings.Contains(res.Message, "Error testing push replication config") {
		t.Fatalf("unreachable verdict = %+v, want the transport-refusal wording", res)
	}
}

// TestProbeNilConfig: the §9.2-C-1 anchored refusal (the draft face always
// synthesizes, so this arm is the engine's own guard).
func TestProbeNilConfig(t *testing.T) {
	e := newProbeEnv(t, func(_ http.ResponseWriter, _ *http.Request) {}, nil)
	res, err := e.eng.TestTarget(context.Background(), nil, replication.TargetOverride{})
	if err != nil {
		t.Fatalf("TestTarget(nil): %v", err)
	}
	if res.OK || res.Message == "" {
		t.Fatalf("nil-config verdict = %+v, want an inline refusal", res)
	}
}

// ---- L28: the two REST test faces against a REAL second instance ----

// TestT422TestFacesTwoInstance pins the wire: the {id} face probes the
// stored sealed credential (correct pair passes against a real BinFlow
// target, wrong pair refuses with the inline 401), the draft face carries
// the form's candidate, the self-instance and error ladder answer their
// families, and the audit row lands.
func TestT422TestFacesTwoInstance(t *testing.T) {
	ctx := context.Background()
	b := newBinFlow(t, "B422T", "pw-target", []*metadata.Repo{
		{RepoKey: "replica-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: "{}"},
	})
	a := newT420Source(t, []*metadata.Repo{
		{RepoKey: "libs", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: "{}"},
	})

	createCfg := func(name, targetURL string) int64 {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"name": name, "source_repo": "libs", "target_url": targetURL,
			"target_repo": "replica-local", "target_username": "admin",
			"target_password": "pw-target", "enabled": true,
		})
		resp, raw := a.do(http.MethodPost, "/binflow/api/v1/replications", "admin", "pw-source", body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create %s: %d (%s)", name, resp.StatusCode, raw)
		}
		var out struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("create %s decode: %v", name, err)
		}
		return out.ID
	}
	test := func(path string, body string) (int, map[string]any) {
		t.Helper()
		resp, raw := a.do(http.MethodPost, path, "admin", "pw-source", []byte(body))
		var out map[string]any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return resp.StatusCode, out
	}

	id := createCfg("t422-probe", b.url)

	// The stored sealed credential against the real target: a pass.
	status, body := test(fmt.Sprintf("/binflow/api/v1/replications/%d/test", id), "")
	if status != http.StatusOK {
		t.Fatalf("id-face test: %d (%v), want 200", status, body)
	}
	if body["ok"] != true || body["status_code"] != float64(http.StatusOK) {
		t.Fatalf("id-face verdict = %v, want ok:true status 200", body)
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "tested successfully") {
		t.Fatalf("id-face message = %v, want the anchored wording", body["message"])
	}
	for k := range body {
		if strings.Contains(k, "password") {
			t.Fatalf("verdict body carries %q — credentials never echo", k)
		}
	}

	// A wrong credential override: the inline 401 (AC1's 错误凭据 arm).
	status, body = test(fmt.Sprintf("/binflow/api/v1/replications/%d/test", id),
		`{"target_password":"nope"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("wrong-credential test: %d (%v), want 400", status, body)
	}
	if body["ok"] != false {
		t.Fatalf("wrong-credential verdict = %v, want ok:false", body)
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "error 401") {
		t.Fatalf("wrong-credential message = %v, want the inline 401", body["message"])
	}

	// The draft face: the form's candidate against the real target, then the
	// unreachable arm (AC1's 不可达).
	draft, _ := json.Marshal(map[string]any{
		"name": "draft-candidate", "target_url": b.url, "target_repo": "replica-local",
		"target_username": "admin", "target_password": "pw-target",
	})
	if status, body = test("/binflow/api/v1/replications/test", string(draft)); status != http.StatusOK || body["ok"] != true {
		t.Fatalf("draft test = (%d, %v), want 200 ok:true", status, body)
	}
	if status, body = test("/binflow/api/v1/replications/test",
		`{"target_url":"http://127.0.0.1:1","target_repo":"replica-local"}`); status != http.StatusBadRequest || body["ok"] != false {
		t.Fatalf("unreachable draft = (%d, %v), want 400 ok:false", status, body)
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "Error testing push replication config") {
		t.Fatalf("unreachable message = %v, want the transport wording", body["message"])
	}

	// The self-instance refusal (§9.3's semantic row).
	selfID := createCfg("t422-self", a.url)
	if status, body = test(fmt.Sprintf("/binflow/api/v1/replications/%d/test", selfID), ""); status != http.StatusBadRequest {
		t.Fatalf("self test: %d (%v), want 400", status, body)
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "same instance") {
		t.Fatalf("self message = %v, want the same-instance refusal", body["message"])
	}

	// The ladder: unknown id 404, malformed id 400, empty draft body 400.
	if status, _ = test("/binflow/api/v1/replications/9999/test", ""); status != http.StatusNotFound {
		t.Fatalf("unknown id: %d, want 404", status)
	}
	if status, _ = test("/binflow/api/v1/replications/abc/test", ""); status != http.StatusBadRequest {
		t.Fatalf("malformed id: %d, want 400", status)
	}
	if status, _ = test("/binflow/api/v1/replications/test", ""); status != http.StatusBadRequest {
		t.Fatalf("empty draft body: %d, want 400", status)
	}

	// The audit trail: one replication.config.test row per probe, the admin
	// on every row, no credential material anywhere.
	events, err := a.md.Audits().Query(ctx, metadata.AuditQuery{Action: "replication.config.test", Limit: 20})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(events) < 5 {
		t.Fatalf("replication.config.test rows = %d, want at least 5", len(events))
	}
	for _, ev := range events {
		if ev.Actor != "admin" {
			t.Errorf("test row actor = %q, want admin", ev.Actor)
		}
		if strings.Contains(ev.Detail, "pw-target") || strings.Contains(ev.Detail, "nope") {
			t.Errorf("test row detail leaked a credential: %s", ev.Detail)
		}
	}
}
