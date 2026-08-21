package httpapi_test

// T-180 AC ①/⑤: the /binflow/api/v1 replications CRUD and the aggregated
// /api/v1/replication/status face, pinned to the T-159 contract assumptions
// (reports/agents/T-159.md): shape names, credential exclusion, '' time
// sentinels, non-null arrays, the 403/404/409/501 degradation ladder and the
// delete cascade over the 009 FK. The stack is the real one (storage engine,
// sqlite metadata with the 009 migration, real repo.Service, real auth) —
// only the listener is httptest, and the replication store rides its OWN
// second connection, the posture cmd assembly uses.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/replication"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"

	_ "modernc.org/sqlite" // driver for the replication store's own connection
)

// replHarness is the T-180 stack: the standard real assembly plus the
// replication deps (store over a second connection, optional cipher).
type replHarness struct {
	t         *testing.T
	srv       *httptest.Server
	md        metadata.Store
	replStore replication.Store
	cipher    *remote.Cipher
	dataDir   string
}

// newReplHarness assembles the stack. withCipher decides whether the
// credential master key is wired (both postures are product behavior).
func newReplHarness(t *testing.T, withCipher bool) *replHarness {
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
	svc := repo.New(st, md, authSvc, nil)
	// The source repository every valid config references.
	if err := md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "libs-release", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: "{}", CreatedAt: "2026-08-22T00:00:00Z", UpdatedAt: "2026-08-22T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	// A non-admin account for the 403 rows.
	hash, err := auth.HashPassword("dev-pw")
	if err != nil {
		t.Fatalf("hash dev password: %v", err)
	}
	if err := md.Users().Create(ctx, &metadata.User{
		Username: "dev", PasswordHash: hash, IsAdmin: false, Enabled: true,
	}); err != nil {
		t.Fatalf("seed dev user: %v", err)
	}

	// The replication store on its own connection — the cmd posture
	// (openReplicationDB): same file, foreign_keys + the shared busy budget.
	dsn := "file:" + url.PathEscape(filepath.Join(dataDir, "binflow.db")) +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(" +
		fmt.Sprintf("%d", metadata.BusyTimeoutMs) + ")"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sql.Open replication: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	replStore := replication.NewSQLiteStore(db)

	var cipher *remote.Cipher
	if withCipher {
		key := make([]byte, 32)
		for i := range key {
			key[i] = byte(i)
		}
		cipher, err = remote.NewCipher(key)
		if err != nil {
			t.Fatalf("NewCipher: %v", err)
		}
	}

	deps := httpapi.Deps{
		Config:      cfg,
		Auth:        authSvc,
		Authz:       authSvc,
		Metadata:    md,
		Repos:       md.Repos(),
		ReposSvc:    svc,
		DataDir:     dataDir,
		Replication: replStore,
	}
	if cipher != nil {
		deps.ReplicationCipher = cipher
	}
	s := httpapi.New(deps, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &replHarness{t: t, srv: ts, md: md, replStore: replStore, cipher: cipher, dataDir: dataDir}
}

// do issues one request against the harness; user != "" adds Basic auth.
func (h *replHarness) do(method, path, user, pass string, body string) (*http.Response, string) {
	h.t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, h.srv.URL+path, rdr)
	if err != nil {
		h.t.Fatalf("build %s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		h.t.Fatalf("do %s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(raw)
}

// admin posts/gets as the seeded administrator.
func (h *replHarness) admin(method, path, body string) (*http.Response, string) {
	return h.do(method, path, "admin", "password", body)
}

// createConfig posts one valid config body and returns the decoded response
// map (fails the test on any non-2xx).
func (h *replHarness) createConfig(t *testing.T, name string) map[string]any {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"source_repo":"libs-release","target_url":"https://dr.example.com","target_repo":"libs-dr"}`, name)
	resp, raw := h.admin(http.MethodPost, "/binflow/api/v1/replications", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create config %s: status %d, body %s", name, resp.StatusCode, raw)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("create config %s: decode: %v", name, err)
	}
	return out
}

// addTask inserts one ledger row straight through the store (the engine is
// not under test here; the REST face is).
func (h *replHarness) addTask(t *testing.T, cfgID int64, status, createdAt, completedAt string) {
	t.Helper()
	if _, err := h.replStore.CreateTask(context.Background(), &replication.ReplicationTask{
		ReplicationID: cfgID, BlobSHA256: fmt.Sprintf("%064d", cfgID), NodePath: "a/b.bin",
		Status: status, Attempts: 1, CreatedAt: createdAt, CompletedAt: completedAt,
	}); err != nil {
		t.Fatalf("CreateTask(%s): %v", status, err)
	}
}

// ---- AC ①: create validation (table-driven) ----

func TestReplicationsCreateValidation(t *testing.T) {
	h := newReplHarness(t, false)
	cases := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{"missing name", `{"source_repo":"libs-release","target_url":"https://x.example.com","target_repo":"r"}`, "name must be"},
		{"name with slash", `{"name":"a/b","source_repo":"libs-release","target_url":"https://x.example.com","target_repo":"r"}`, "name must be"},
		{"name starting with dash", `{"name":"-lead","source_repo":"libs-release","target_url":"https://x.example.com","target_repo":"r"}`, "name must be"},
		{"empty source_repo", `{"name":"dr","source_repo":"","target_url":"https://x.example.com","target_repo":"r"}`, "source_repo is required"},
		{"unknown source_repo", `{"name":"dr","source_repo":"nope","target_url":"https://x.example.com","target_repo":"r"}`, "unknown repository"},
		{"empty target_repo", `{"name":"dr","source_repo":"libs-release","target_url":"https://x.example.com","target_repo":""}`, "target_repo is required"},
		{"target url without scheme", `{"name":"dr","source_repo":"libs-release","target_url":"dr.example.com","target_repo":"r"}`, "absolute http or https"},
		{"target url wrong scheme", `{"name":"dr","source_repo":"libs-release","target_url":"ftp://dr.example.com","target_repo":"r"}`, "absolute http or https"},
		{"negative bandwidth", `{"name":"dr","source_repo":"libs-release","target_url":"https://x.example.com","target_repo":"r","max_bandwidth_bytes_per_sec":-1}`, "must not be negative"},
		{"negative max items", `{"name":"dr","source_repo":"libs-release","target_url":"https://x.example.com","target_repo":"r","max_items_per_push":-5}`, "must not be negative"},
		{"not json", `{{{`, "not valid JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, raw := h.admin(http.MethodPost, "/binflow/api/v1/replications", tc.body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", resp.StatusCode, raw)
			}
			if !strings.Contains(raw, tc.wantMsg) {
				t.Fatalf("message %q does not contain %q", raw, tc.wantMsg)
			}
		})
	}
	// Nothing was created: the list stays empty.
	resp, raw := h.admin(http.MethodGet, "/binflow/api/v1/replications", "")
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(raw) != "[]" {
		t.Fatalf("list after rejects = %d %q, want 200 []", resp.StatusCode, raw)
	}
}

// ---- AC ①: create + list happy paths, defaults, credential handling ----

func TestReplicationsCreateAndList(t *testing.T) {
	h := newReplHarness(t, true)

	created := h.createConfig(t, "dr-site")
	// Response shape: full row minus the credential pair; absent enabled
	// defaults true and absent max_items_per_push takes the 009 default.
	want := map[string]any{
		"id": created["id"], "name": "dr-site", "source_repo": "libs-release",
		"target_url": "https://dr.example.com", "target_repo": "libs-dr",
		"target_username": "", "enabled": true,
		"max_bandwidth_bytes_per_sec": float64(0), "max_items_per_push": float64(1000),
	}
	if len(created) != len(want)+2 { // + created_at, updated_at
		t.Fatalf("created keys = %v (%d), want exactly %v plus the two timestamps", created, len(created), want)
	}
	for k, v := range want {
		got, ok := created[k]
		if !ok || fmt.Sprint(got) != fmt.Sprint(v) {
			t.Errorf("created[%s] = %v (%T), want %v", k, got, got, v)
		}
	}
	if _, leaked := created["target_password"]; leaked {
		t.Error("create response leaks target_password")
	}
	if _, leaked := created["target_password_enc"]; leaked {
		t.Error("create response leaks target_password_enc")
	}

	// Explicit enabled=false survives.
	resp, raw := h.admin(http.MethodPost, "/binflow/api/v1/replications",
		`{"name":"paused","source_repo":"libs-release","target_url":"http://dr2.example.com","target_repo":"libs-dr","enabled":false}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create paused: %d %s", resp.StatusCode, raw)
	}
	var paused map[string]any
	_ = json.Unmarshal([]byte(raw), &paused)
	if paused["enabled"] != false {
		t.Errorf("paused config enabled = %v, want explicit false", paused["enabled"])
	}

	// List: bare JSON array, both rows, secrets absent from the wire form.
	resp, raw = h.admin(http.MethodGet, "/binflow/api/v1/replications", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", resp.StatusCode, raw)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		t.Fatalf("list decode: %v (%s)", err, raw)
	}
	if len(list) != 2 || list[0]["name"] != "dr-site" || list[1]["name"] != "paused" {
		t.Fatalf("list = %v, want dr-site then paused (store id order)", list)
	}
	if strings.Contains(raw, "password") {
		t.Fatalf("list body names a password field: %s", raw)
	}
}

// A password-carrying create without a master key is refused (never stored
// plaintext); with the cipher wired it lands as enc:v1 ciphertext and never
// crosses the wire again.
func TestReplicationsPasswordCiphering(t *testing.T) {
	h := newReplHarness(t, false)
	body := `{"name":"dr-secret","source_repo":"libs-release","target_url":"https://dr.example.com","target_repo":"libs-dr","target_username":"repl","target_password":"hunter2"}`
	resp, raw := h.admin(http.MethodPost, "/binflow/api/v1/replications", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("no-cipher create: status %d, want 400 (body %s)", resp.StatusCode, raw)
	}
	if !strings.Contains(raw, "BINFLOW_REMOTE_CREDENTIALS_KEY") {
		t.Fatalf("no-cipher refusal does not name the env var: %s", raw)
	}

	hc := newReplHarness(t, true)
	resp, raw = hc.admin(http.MethodPost, "/binflow/api/v1/replications", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("cipher create: status %d, want 201 (body %s)", resp.StatusCode, raw)
	}
	var created map[string]any
	_ = json.Unmarshal([]byte(raw), &created)
	id := int64(created["id"].(float64))
	cfg, err := hc.replStore.GetConfig(context.Background(), id)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if !strings.HasPrefix(cfg.TargetPasswordEnc, "enc:v1:") {
		t.Fatalf("stored credential = %q, want the enc:v1 at-rest form", cfg.TargetPasswordEnc)
	}
	if strings.Contains(cfg.TargetPasswordEnc, "hunter2") {
		t.Fatal("stored credential contains the plaintext")
	}
	// The sealed value must not leak through either read face.
	_, listRaw := hc.admin(http.MethodGet, "/binflow/api/v1/replications", "")
	if strings.Contains(listRaw, cfg.TargetPasswordEnc) || strings.Contains(listRaw, "hunter2") {
		t.Fatalf("list leaks the credential: %s", listRaw)
	}
	_, statusRaw := hc.admin(http.MethodGet, "/binflow/api/v1/replication/status", "")
	if strings.Contains(statusRaw, cfg.TargetPasswordEnc) || strings.Contains(statusRaw, "hunter2") {
		t.Fatalf("status leaks the credential: %s", statusRaw)
	}
	// A round-trip through the engine's decryptor opens it (same key wired
	// on both faces — the ADR-0021 reuse).
	secret, _, err := hc.cipher.Decrypt(cfg.TargetPasswordEnc)
	if err != nil || secret != "hunter2" {
		t.Fatalf("decrypt round-trip = %q, %v; want the plaintext back", secret, err)
	}
}

// ---- AC ①/⑤: permissions (403/401), duplicate name (409), delete 404 ----

func TestReplicationsPermissionsAndDuplicate(t *testing.T) {
	h := newReplHarness(t, false)
	h.createConfig(t, "dr-site")

	cases := []struct {
		name   string
		method string
		path   string
		user   string
		pass   string
		want   int
	}{
		{"anonymous list", http.MethodGet, "/binflow/api/v1/replications", "", "", http.StatusUnauthorized},
		{"non-admin list", http.MethodGet, "/binflow/api/v1/replications", "dev", "dev-pw", http.StatusForbidden},
		{"anonymous create", http.MethodPost, "/binflow/api/v1/replications", "", "", http.StatusUnauthorized},
		{"non-admin create", http.MethodPost, "/binflow/api/v1/replications", "dev", "dev-pw", http.StatusForbidden},
		{"non-admin delete", http.MethodDelete, "/binflow/api/v1/replications/dr-site", "dev", "dev-pw", http.StatusForbidden},
		{"anonymous status", http.MethodGet, "/binflow/api/v1/replication/status", "", "", http.StatusUnauthorized},
		{"non-admin status", http.MethodGet, "/binflow/api/v1/replication/status", "dev", "dev-pw", http.StatusForbidden},
		// Duplicate names are a conflict, not a bad request: the body is
		// valid, the NAME is taken.
		{"duplicate name", http.MethodPost, "/binflow/api/v1/replications", "admin", "password", http.StatusConflict},
		{"delete unknown name", http.MethodDelete, "/binflow/api/v1/replications/ghost", "admin", "password", http.StatusNotFound},
		{"delete is one segment only", http.MethodDelete, "/binflow/api/v1/replications/a/b", "admin", "password", http.StatusNotFound},
		// No update surface yet: every other verb falls to the E-26 404.
		{"put not implemented", http.MethodPut, "/binflow/api/v1/replications", "admin", "password", http.StatusNotFound},
		{"bare replication prefix not routed", http.MethodGet, "/binflow/api/v1/replication", "admin", "password", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := ""
			if tc.method == http.MethodPost {
				body = `{"name":"dr-site","source_repo":"libs-release","target_url":"https://x.example.com","target_repo":"r"}`
			}
			resp, raw := h.do(tc.method, tc.path, tc.user, tc.pass, body)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", resp.StatusCode, tc.want, raw)
			}
			if tc.want >= 400 && !strings.Contains(raw, "\"errors\"") {
				t.Fatalf("body %q lacks the errors[] envelope", raw)
			}
		})
	}
}

// ---- AC ①/⑤: delete happy path + the 009 task cascade ----

func TestReplicationsDeleteCascadesTasks(t *testing.T) {
	h := newReplHarness(t, false)
	created := h.createConfig(t, "dr-site")
	id := int64(created["id"].(float64))
	h.addTask(t, id, replication.TaskStatusPending, "2026-08-22T10:00:00Z", "")
	h.addTask(t, id, replication.TaskStatusSuccess, "2026-08-22T09:00:00Z", "2026-08-22T09:00:05Z")

	resp, raw := h.admin(http.MethodDelete, "/binflow/api/v1/replications/dr-site", "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d, want 204 (body %s)", resp.StatusCode, raw)
	}
	// The config is gone...
	if cfgs, err := h.replStore.ListConfigs(context.Background()); err != nil || len(cfgs) != 0 {
		t.Fatalf("configs after delete = %v (%v), want empty", cfgs, err)
	}
	// ...and its task rows cascaded with it (the 009 FK).
	tasks, err := h.replStore.ListTasks(context.Background(), id, 10)
	if err != nil {
		t.Fatalf("ListTasks after cascade: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("tasks after config delete = %d rows, want the FK cascade to empty them", len(tasks))
	}
	// Second delete: honest 404.
	resp, raw = h.admin(http.MethodDelete, "/binflow/api/v1/replications/dr-site", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("re-delete: status %d, want 404 (body %s)", resp.StatusCode, raw)
	}
	// The governance trail kept the deletion.
	events, err := h.md.Audits().Query(context.Background(), metadata.AuditQuery{Action: "replication.config.delete", Limit: 10})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(events) != 1 || events[0].Actor != "admin" {
		t.Fatalf("audit rows = %+v, want one admin replication.config.delete", events)
	}
	created2 := h.createConfig(t, "dr-again")
	events, err = h.md.Audits().Query(context.Background(), metadata.AuditQuery{Action: "replication.config.create", Limit: 10})
	if err != nil || len(events) != 2 {
		t.Fatalf("create audit rows = %d (%v), want both creates recorded", len(events), err)
	}
	_ = created2
}

// ---- AC ①/⑤: the aggregated status face, pinned to the T-159 shape ----

func TestReplicationStatus(t *testing.T) {
	h := newReplHarness(t, false)
	first := h.createConfig(t, "dr-site")
	idA := int64(first["id"].(float64))
	second := h.createConfig(t, "dr-backup")
	idB := int64(second["id"].(float64))

	// Config A: the counts the panel renders. RFC3339 text sorts
	// chronologically, so MAX(completed_at) is the newest success.
	h.addTask(t, idA, replication.TaskStatusPending, "2026-08-22T10:00:00Z", "")
	h.addTask(t, idA, replication.TaskStatusInProgress, "2026-08-22T10:00:01Z", "")
	h.addTask(t, idA, replication.TaskStatusSuccess, "2026-08-22T09:00:00Z", "2026-08-22T09:00:05Z")
	h.addTask(t, idA, replication.TaskStatusSuccess, "2026-08-22T09:30:00Z", "2026-08-22T11:59:01Z")
	h.addTask(t, idA, replication.TaskStatusFailed, "2026-08-22T08:00:00Z", "2026-08-22T08:00:09Z")
	h.addTask(t, idA, replication.TaskStatusSkipped, "2026-08-22T07:00:00Z", "2026-08-22T07:00:02Z")
	// Config B: older events exercise the cross-config merge order.
	h.addTask(t, idB, replication.TaskStatusSuccess, "2026-08-21T00:00:00Z", "2026-08-21T00:00:01Z")

	resp, raw := h.admin(http.MethodGet, "/binflow/api/v1/replication/status", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d %s", resp.StatusCode, raw)
	}
	var body struct {
		Targets []map[string]any `json:"targets"`
		Events  []map[string]any `json:"events"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("status decode: %v (%s)", err, raw)
	}

	// Targets: id-ordered, counts flattened, no duplicated replication id
	// column, no credential fields, '' = never succeeded.
	if len(body.Targets) != 2 {
		t.Fatalf("targets = %d rows, want 2", len(body.Targets))
	}
	a := body.Targets[0]
	wantCounts := map[string]float64{
		"pending": 1, "in_progress": 1, "succeeded": 2, "failed": 1, "skipped": 1,
	}
	for k, v := range wantCounts {
		if got := a[k]; got != v {
			t.Errorf("target A %s = %v, want %v", k, got, v)
		}
	}
	if a["last_success_at"] != "2026-08-22T11:59:01Z" {
		t.Errorf("target A last_success_at = %v, want the newest success completion", a["last_success_at"])
	}
	if a["updated_at"] != first["updated_at"] {
		t.Errorf("target A updated_at = %v, want the config row's %v", a["updated_at"], first["updated_at"])
	}
	for _, forbidden := range []string{"target_username", "target_password", "target_password_enc", "replication_id"} {
		if _, ok := a[forbidden]; ok {
			t.Errorf("target row carries %q (T-159 rulings 1/2)", forbidden)
		}
	}
	if b := body.Targets[1]; b["last_success_at"] != "2026-08-21T00:00:01Z" {
		t.Errorf("target B last_success_at = %v, want its only success", b["last_success_at"])
	}

	// Events: merged newest-first ACROSS configs, every ledger column
	// present, '' completed_at for non-terminal rows.
	if len(body.Events) != 7 {
		t.Fatalf("events = %d rows, want all 7 (default limit 50)", len(body.Events))
	}
	if body.Events[0]["status"] != replication.TaskStatusInProgress {
		t.Errorf("newest event = %v, want config A's in_progress row", body.Events[0]["status"])
	}
	if body.Events[6]["replication_id"].(float64) != float64(idB) {
		t.Errorf("oldest event replication_id = %v, want config B's row", body.Events[6]["replication_id"])
	}
	wantKeys := []string{"id", "replication_id", "blob_sha256", "node_path",
		"status", "attempts", "last_error", "created_at", "completed_at"}
	for i, ev := range body.Events {
		if len(ev) != len(wantKeys) {
			t.Fatalf("event %d keys = %v, want exactly %v", i, ev, wantKeys)
		}
		for _, k := range wantKeys {
			if _, ok := ev[k]; !ok {
				t.Fatalf("event %d lacks %q", i, k)
			}
		}
	}
	if v := body.Events[0]["completed_at"]; v != "" {
		t.Errorf("in_progress event completed_at = %q, want '' (not terminal)", v)
	}

	// ?limit= truncates the merged window; bounds and garbage refuse.
	resp, raw = h.admin(http.MethodGet, "/binflow/api/v1/replication/status?limit=2", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status?limit=2: %d %s", resp.StatusCode, raw)
	}
	var limited struct {
		Events []map[string]any `json:"events"`
	}
	_ = json.Unmarshal([]byte(raw), &limited)
	if len(limited.Events) != 2 || limited.Events[0]["status"] != replication.TaskStatusInProgress {
		t.Fatalf("limit=2 events = %v, want the two newest", limited.Events)
	}
	for _, q := range []string{"limit=0", "limit=501", "limit=abc", "limit=-1"} {
		resp, raw = h.admin(http.MethodGet, "/binflow/api/v1/replication/status?"+q, "")
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status?%s: status %d, want 400 (body %s)", q, resp.StatusCode, raw)
		}
	}

	// The fresh-instance face: configured plane, nothing yet — arrays are []
	// on the wire, never null (the panel's empty state).
	h2 := newReplHarness(t, false)
	resp, raw = h2.admin(http.MethodGet, "/binflow/api/v1/replication/status", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("empty status: %d %s", resp.StatusCode, raw)
	}
	if strings.Contains(raw, "null") {
		t.Fatalf("empty status body renders null: %s", raw)
	}
	if !strings.Contains(raw, `"targets": []`) || !strings.Contains(raw, `"events": []`) {
		t.Fatalf("empty status body must carry empty arrays: %s", raw)
	}
}

// ---- AC ① ruling 6: unwired instances degrade to 501, not 404 ----

func TestReplicationNotConfigured(t *testing.T) {
	h := newReplHarness(t, false)
	// Rebuild the same stack WITHOUT Deps.Replication — the assembly that
	// predates T-180 (and the httpapi unit stacks).
	cfg := config.Defaults()
	authSvc := auth.NewFromStore(h.md, cfg.Security.AnonymousAccess)
	s := httpapi.New(httpapi.Deps{
		Config: cfg, Auth: authSvc, Authz: authSvc, Metadata: h.md, Repos: h.md.Repos(),
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/binflow/api/v1/replications"},
		{http.MethodPost, "/binflow/api/v1/replications"},
		{http.MethodDelete, "/binflow/api/v1/replications/dr"},
		{http.MethodGet, "/binflow/api/v1/replication/status"},
	} {
		req, err := http.NewRequest(tc.method, ts.URL+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth("admin", "password")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotImplemented {
			t.Fatalf("%s %s = %d, want 501 (body %s)", tc.method, tc.path, resp.StatusCode, raw)
		}
	}
}

// Compile-time pin: the adapter set this file's stacks mount is the generic
// one only — no process-wide registry is touched, keeping the package's
// parallel tests decoupled (the harness_test.go contract).
var _ adapter.Handler = (*generic.Handler)(nil)
