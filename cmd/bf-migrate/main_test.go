package main

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // fixture digests
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestRunHelpMatrix(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no args prints usage", args: nil},
		{name: "long help flag", args: []string{"--help"}},
		{name: "short help flag", args: []string{"-h"}},
		{name: "help command", args: []string{"help"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run(tt.args, &stdout, &stderr); err != nil {
				t.Fatalf("run(%v) error = %v, want nil", tt.args, err)
			}
			if got := stdout.String(); !strings.Contains(got, "Usage:") {
				t.Errorf("stdout = %q, want it to contain usage", got)
			}
		})
	}
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(--version) error = %v, want nil", err)
	}
	if got := stdout.String(); !strings.Contains(got, "bf-migrate "+version) {
		t.Errorf("stdout = %q, want it to contain the version banner", got)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"frobnicate"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("run(frobnicate) error = %v, want unknown command", err)
	}
}

func TestMigrateHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"migrate", "--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(migrate --help) error = %v, want nil (help exits zero)", err)
	}
	for _, want := range []string{"--dry-run", "--resume", "--passwords-out", "--allow-non-empty", "--concurrency", "--report-file", "--skip-users", "artifacts repository content", "target must be EMPTY"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("migrate usage missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestMigrateFlagErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{
			name: "missing artifactory url",
			args: []string{"migrate", "--dry-run"},
			want: "--artifactory-url is required",
		},
		{
			name: "relative artifactory url",
			args: []string{"migrate", "--artifactory-url", "host:8081/artifactory", "--dry-run"},
			want: "must be absolute",
		},
		{
			name: "positional args rejected",
			args: []string{"migrate", "--artifactory-url", "http://x", "--dry-run", "stray"},
			want: "no positional arguments",
		},
		{
			name: "missing target token fails fast without dry-run",
			args: []string{"migrate", "--artifactory-url", "http://x"},
			want: "no BinFlow credentials",
		},
		{
			name: "empty named token env",
			args: []string{"migrate", "--artifactory-url", "http://x", "--token-env", "NOPE"},
			want: "--token-env NOPE is not set or empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			t.Setenv("BINFLOW_TOKEN", "")
			t.Setenv("BF_TOKEN", "")
			var stdout, stderr bytes.Buffer
			err := run(tt.args, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

// cliArtMock / cliBFMock are self-contained mock servers for the CLI-level
// tests (cmd cannot reach internal/migrate's test mocks). The shapes mirror
// docs/reverse/rest-api.md and the real BinFlow write plane. userStatus
// injects the GET /api/security/users answer (0 = 200) for the --skip-users
// legs (B-1: 403 = non-admin credentials, 400 = the OSS license gate).
func cliArtMock(t *testing.T, userStatus int) *httptest.Server {
	t.Helper()
	repos := []map[string]any{
		{"key": "libs-generic", "type": "local", "packageType": "generic"},
		{"key": "libs-nuget", "type": "local", "packageType": "nuget"},
		{"key": "libs-virtual", "type": "virtual", "packageType": "generic", "repositories": []string{"libs-generic"}},
	}
	configs := map[string]map[string]any{
		"libs-generic": {"key": "libs-generic", "rclass": "local", "packageType": "generic", "description": "raw"},
		"libs-nuget":   {"key": "libs-nuget", "rclass": "local", "packageType": "nuget"},
		"libs-virtual": {"key": "libs-virtual", "rclass": "virtual", "packageType": "generic", "repositories": []string{"libs-generic"}},
	}
	users := []map[string]any{
		{"name": "alice", "uri": "x", "realm": "internal"},
		{"name": "anonymous", "uri": "x", "realm": "internal"},
	}
	details := map[string]map[string]any{
		"alice":     {"name": "alice", "email": "alice@example.com", "admin": true, "groups": []string{"readers"}, "realm": "internal"},
		"anonymous": {"name": "anonymous", "realm": "internal"},
	}

	// Artifact content: 3 generic files with true digests (rest-api.md 3
	// listing shape; download face serves the bytes).
	paths := []string{"org/app/1.0/app-1.0.bin", "docs/readme.md", "root.txt"}
	blobs := map[string]string{}
	files := make([]map[string]any, 0, len(paths))
	for _, p := range paths {
		content := fmt.Sprintf("cli content of libs-generic/%s\n", p)
		s1 := sha1.Sum([]byte(content)) //nolint:gosec // fixture digest
		s2 := sha256.Sum256([]byte(content))
		blobs[p] = content
		files = append(files, map[string]any{
			"uri": p, "size": len(content), "folder": false,
			"sha1": hex.EncodeToString(s1[:]), "sha2": hex.EncodeToString(s2[:]),
		})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /artifactory/api/repositories", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, repos)
	})
	mux.HandleFunc("GET /artifactory/api/repositories/{key}", func(w http.ResponseWriter, r *http.Request) {
		cfg, ok := configs[r.PathValue("key")]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, cfg)
	})
	mux.HandleFunc("GET /artifactory/api/security/users", func(w http.ResponseWriter, _ *http.Request) {
		if userStatus != 0 {
			http.Error(w, "available only in Artifactory Pro", userStatus)
			return
		}
		writeJSON(w, users)
	})
	mux.HandleFunc("GET /artifactory/api/security/users/{name}", func(w http.ResponseWriter, r *http.Request) {
		d, ok := details[r.PathValue("name")]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, d)
	})
	mux.HandleFunc("GET /artifactory/api/security/token", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"tokens": []map[string]any{{"token_id": "1", "subject": "alice"}}})
	})
	mux.HandleFunc("GET /artifactory/api/storage/libs-generic", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"uri": "/artifactory/api/storage/libs-generic", "files": files})
	})
	mux.HandleFunc("GET /artifactory/libs-generic/{path...}", func(w http.ResponseWriter, r *http.Request) {
		content, ok := blobs[r.PathValue("path")]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(content))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// cliBFMock records writes against one stable target URL; setFail
// injects (or clears, with "") a per-repository failure — the crash
// simulation lever for the --resume legs. The mock also serves the guard's
// repo listing (preExisting seeds it) and the plain-file artifact PUT.
func cliBFMock(t *testing.T) (*httptest.Server, *cliBFState) {
	t.Helper()
	st := &cliBFState{created: map[string]bool{}, artifacts: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /binflow/api/repositories", func(w http.ResponseWriter, _ *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		listed := append([]map[string]any(nil), st.preExisting...)
		for _, key := range st.repos {
			listed = append(listed, map[string]any{"key": key, "type": "local", "packageType": "generic"})
		}
		writeJSON(w, listed)
	})
	mux.HandleFunc("PUT /binflow/api/repositories/{key}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		key := r.PathValue("key")
		st.recordRepo(key, body, r.Header.Get("Authorization"))
		if st.failing(key) {
			http.Error(w, "injected failure", http.StatusInternalServerError)
			return
		}
		// The real repo plane validates virtual member existence; the wire
		// key is the server's "repositories" (client tags aligned in T-191).
		if members, ok := body["repositories"].([]any); ok {
			for _, m := range members {
				if !st.isCreated(fmt.Sprint(m)) {
					http.Error(w, "member repository does not exist: "+fmt.Sprint(m), http.StatusBadRequest)
					return
				}
			}
		}
		st.markCreated(key)
		_, _ = fmt.Fprintf(w, "Successfully created repository '%s'\n", key)
	})
	mux.HandleFunc("PUT /binflow/api/security/users/{name}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		st.recordUser(r.PathValue("name"), body, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("PUT /binflow/{repo}/{path...}", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		st.mu.Lock()
		st.artifacts[r.PathValue("repo")+"/"+r.PathValue("path")] = string(body)
		st.authz = append(st.authz, r.Header.Get("Authorization"))
		st.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, st
}

type cliBFState struct {
	mu          sync.Mutex
	repos       []string
	users       []string
	bodies      []map[string]any
	authz       []string
	created     map[string]bool
	fail        string
	preExisting []map[string]any
	artifacts   map[string]string
}

// setFail injects a failing repository key ("" clears the injection).
func (s *cliBFState) setFail(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = key
}

func (s *cliBFState) failing(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fail != "" && key == s.fail
}

func (s *cliBFState) isCreated(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.created[key]
}

// counts returns the number of repo/user writes so far (for delta
// assertions across resume legs on one shared server).
func (s *cliBFState) counts() (repos, users int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.repos), len(s.users)
}

func (s *cliBFState) recordRepo(key string, body map[string]any, authz string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos = append(s.repos, key)
	s.bodies = append(s.bodies, body)
	s.authz = append(s.authz, authz)
}

func (s *cliBFState) recordUser(name string, body map[string]any, authz string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users = append(s.users, name)
	s.bodies = append(s.bodies, body)
	s.authz = append(s.authz, authz)
}

func (s *cliBFState) markCreated(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.created == nil {
		s.created = map[string]bool{}
	}
	s.created[key] = true
}

func (s *cliBFState) snapshot() (repos, users []string, authz []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.repos...), append([]string(nil), s.users...), append([]string(nil), s.authz...)
}

// setPreExisting seeds a repository the target already holds (the guard's
// refusal fixture).
func (s *cliBFState) setPreExisting(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.preExisting = append(s.preExisting, map[string]any{"key": key, "type": "local", "packageType": "generic"})
}

// artifactCount returns the number of artifact PUTs recorded so far.
func (s *cliBFState) artifactCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.artifacts)
}

// artifactBody returns the uploaded bytes of one artifact ("repo/path").
func (s *cliBFState) artifactBody(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.artifacts[key]
}

func TestMigrateEndToEnd(t *testing.T) {
	t.Setenv("ARTIFACTORY_TOKEN", "src-cred")
	t.Setenv("BINFLOW_TOKEN", "tgt-cred")
	t.Setenv("ARTIFACTORY_URL", "")
	t.Setenv("BINFLOW_SERVER_URL", "")

	art := cliArtMock(t, 0)
	tmp := t.TempDir()
	progress := filepath.Join(tmp, "progress.json")
	passwords := filepath.Join(tmp, "passwords.txt")
	report := filepath.Join(tmp, "migration_report.json")

	t.Run("dry run writes nothing", func(t *testing.T) {
		bf, st := cliBFMock(t)
		var stdout, stderr bytes.Buffer
		err := run([]string{
			"migrate",
			"--artifactory-url", art.URL + "/artifactory",
			"--server", bf.URL,
			"--progress-file", progress,
			"--report-file", report,
			"--dry-run",
		}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("run error = %v\nstderr: %s", err, stderr.String())
		}
		repos, users, _ := st.snapshot()
		if len(repos)+len(users) != 0 {
			t.Errorf("dry-run wrote %d items, want 0", len(repos)+len(users))
		}
		if n := st.artifactCount(); n != 0 {
			t.Errorf("dry-run uploaded %d artifacts, want 0", n)
		}
		if _, err := os.Stat(progress); !os.IsNotExist(err) {
			t.Errorf("dry-run must not create the progress file (err=%v)", err)
		}
		if !strings.Contains(stdout.String(), "(dry-run)") {
			t.Errorf("summary must be labeled dry-run:\n%s", stdout.String())
		}
		if !strings.Contains(stdout.String(), "artifacts: found=3 migrated=3") {
			t.Errorf("dry-run must count the planned artifacts:\n%s", stdout.String())
		}
		// The dry-run report exists, labeled as such.
		raw, err := os.ReadFile(report)
		if err != nil {
			t.Fatalf("dry-run report: %v", err)
		}
		if !strings.Contains(string(raw), `"dry_run": true`) {
			t.Errorf("dry-run report must carry dry_run=true:\n%s", raw)
		}
	})

	t.Run("non-empty target is refused then merges via flag", func(t *testing.T) {
		bf, st := cliBFMock(t)
		st.setPreExisting("someone-elses-repo")
		refuseProgress := filepath.Join(tmp, "progress-refuse.json")
		refuseReport := filepath.Join(tmp, "report-refuse.json")
		var stdout, stderr bytes.Buffer
		err := run([]string{
			"migrate",
			"--artifactory-url", art.URL + "/artifactory",
			"--server", bf.URL,
			"--progress-file", refuseProgress,
			"--report-file", refuseReport,
			"--retry-max", "-1",
		}, &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "not empty") {
			t.Fatalf("run error = %v, want the guard refusal", err)
		}
		if !strings.Contains(err.Error(), "someone-elses-repo") || !strings.Contains(err.Error(), "--allow-non-empty") {
			t.Errorf("refusal must name the evidence and the override: %v", err)
		}
		if repos, users, _ := st.snapshot(); len(repos)+len(users) != 0 {
			t.Errorf("a refused run must write nothing, got repos %v users %v", repos, users)
		}
		if _, err := os.Stat(refuseProgress); !os.IsNotExist(err) {
			t.Errorf("a refused run must leave no progress file (err=%v)", err)
		}
		raw, err := os.ReadFile(refuseReport)
		if err != nil {
			t.Fatalf("refusal report: %v", err)
		}
		if !strings.Contains(string(raw), `"refused": true`) {
			t.Errorf("refusal report must record the refusal:\n%s", raw)
		}

		// The same populated target merges with --allow-non-empty.
		var stdout2, stderr2 bytes.Buffer
		err = run([]string{
			"migrate",
			"--artifactory-url", art.URL + "/artifactory",
			"--server", bf.URL,
			"--progress-file", refuseProgress,
			"--report-file", refuseReport,
			"--passwords-out", filepath.Join(tmp, "merge-passwords.txt"),
			"--allow-non-empty",
			"--retry-max", "-1",
		}, &stdout2, &stderr2)
		if err != nil {
			t.Fatalf("allow-non-empty run error = %v\nstderr: %s", err, stderr2.String())
		}
		if !strings.Contains(stdout2.String(), "--allow-non-empty") {
			t.Errorf("summary must state the merge semantics:\n%s", stdout2.String())
		}
		raw2, err := os.ReadFile(refuseReport)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw2), `"allowed_via_flag": true`) || !strings.Contains(string(raw2), "merge semantics") {
			t.Errorf("merge report must record the override and semantics:\n%s", raw2)
		}
	})

	// One shared target server across the full-run and resume legs: the
	// progress file is bound to the endpoint pair, so --resume must hit
	// the same target URL (as it would in a real recovery).
	bf, st := cliBFMock(t)

	t.Run("full run with generated passwords", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := run([]string{
			"migrate",
			"--artifactory-url", art.URL + "/artifactory",
			"--server", bf.URL,
			"--progress-file", progress,
			"--report-file", report,
			"--passwords-out", passwords,
			"--concurrency", "2",
			"--retry-max", "-1",
		}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("run error = %v\nstderr: %s\nstdout: %s", err, stderr.String(), stdout.String())
		}
		repos, users, authz := st.snapshot()
		if len(repos) != 2 || len(users) != 1 {
			t.Fatalf("target writes = repos %v users %v, want 2 repos and alice", repos, users)
		}
		if users[0] != "alice" {
			t.Errorf("migrated user = %q, want alice", users[0])
		}
		for i, h := range authz {
			if h != "Bearer tgt-cred" {
				t.Errorf("target auth[%d] = %q, want Bearer tgt-cred", i, h)
			}
		}
		// Artifacts: the 3 generic files, byte-exact (D7).
		if n := st.artifactCount(); n != 3 {
			t.Fatalf("uploaded artifacts = %d, want 3", n)
		}
		for _, p := range []string{"org/app/1.0/app-1.0.bin", "docs/readme.md", "root.txt"} {
			if got := st.artifactBody("libs-generic/" + p); got != fmt.Sprintf("cli content of libs-generic/%s\n", p) {
				t.Errorf("artifact %s body = %q", p, got)
			}
		}
		if _, err := os.Stat(progress); err != nil {
			t.Fatalf("progress file: %v", err)
		}
		// The report records the full outcome (D8).
		raw, err := os.ReadFile(report)
		if err != nil {
			t.Fatalf("report file: %v", err)
		}
		var rep map[string]any
		if err := json.Unmarshal(raw, &rep); err != nil {
			t.Fatalf("report is not JSON: %v\n%s", err, raw)
		}
		if rep["tool"] != "bf-migrate" || rep["version"] != version {
			t.Errorf("report header = %v (version %q)", rep["tool"], version)
		}
		arts, _ := rep["artifacts"].(map[string]any)
		if arts == nil || arts["migrated"] != float64(3) || arts["found"] != float64(3) {
			t.Errorf("report artifacts = %v", arts)
		}
		guard, _ := rep["guard"].(map[string]any)
		if guard == nil || guard["target_non_empty"] != false {
			t.Errorf("report guard = %v", guard)
		}
		pw, err := os.ReadFile(passwords)
		if err != nil {
			t.Fatalf("passwords file: %v", err)
		}
		if !strings.HasPrefix(string(pw), "alice ") {
			t.Errorf("passwords file = %q, want an alice line", string(pw))
		}
		if !strings.Contains(stdout.String(), "tokens: found=1 migrated=0 skipped=1") {
			t.Errorf("summary must account tokens:\n%s", stdout.String())
		}
		if !strings.Contains(stdout.String(), "artifacts: found=3 migrated=3") {
			t.Errorf("summary must account artifacts:\n%s", stdout.String())
		}
	})

	t.Run("resume run is a no-op when everything is recorded", func(t *testing.T) {
		reposBefore, usersBefore := st.counts()
		artsBefore := st.artifactCount()
		var stdout, stderr bytes.Buffer
		err := run([]string{
			"migrate",
			"--artifactory-url", art.URL + "/artifactory",
			"--server", bf.URL,
			"--progress-file", progress,
			"--report-file", report,
			"--passwords-out", passwords,
			"--resume",
			"--retry-max", "-1",
		}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("run error = %v\nstderr: %s", err, stderr.String())
		}
		reposAfter, usersAfter := st.counts()
		if reposAfter != reposBefore || usersAfter != usersBefore {
			t.Errorf("resume after completion re-wrote items: repos %d->%d users %d->%d, want no change",
				reposBefore, reposAfter, usersBefore, usersAfter)
		}
		if got := st.artifactCount(); got != artsBefore {
			t.Errorf("resume after completion re-uploaded artifacts: %d -> %d, want no change", artsBefore, got)
		}
		if !strings.Contains(stdout.String(), "already-done=2") {
			t.Errorf("summary must report the recorded repos:\n%s", stdout.String())
		}
	})

	t.Run("failed item is retried by a fresh resume", func(t *testing.T) {
		failProgress := filepath.Join(tmp, "progress-fail.json")
		failPasswords := filepath.Join(tmp, "passwords-fail.txt")
		failReport := filepath.Join(tmp, "report-fail.json")

		// First run: libs-generic fails server-side; the virtual repo
		// depending on it fails with it.
		bf, st := cliBFMock(t)
		st.setFail("libs-generic")
		var stdout, stderr bytes.Buffer
		err := run([]string{
			"migrate",
			"--artifactory-url", art.URL + "/artifactory",
			"--server", bf.URL,
			"--progress-file", failProgress,
			"--report-file", failReport,
			"--passwords-out", failPasswords,
			"--retry-max", "-1",
		}, &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "libs-generic") {
			t.Fatalf("run error = %v, want libs-generic failure", err)
		}
		// The failed repo's artifacts were never attempted.
		if n := st.artifactCount(); n != 0 {
			t.Errorf("artifacts of a failed repo must not be attempted, got %d", n)
		}

		// Second run against the SAME target (healthy again) with --resume.
		st.setFail("")
		var stdout2, stderr2 bytes.Buffer
		err = run([]string{
			"migrate",
			"--artifactory-url", art.URL + "/artifactory",
			"--server", bf.URL,
			"--progress-file", failProgress,
			"--report-file", failReport,
			"--passwords-out", failPasswords,
			"--resume",
			"--retry-max", "-1",
		}, &stdout2, &stderr2)
		if err != nil {
			t.Fatalf("resume error = %v\nstderr: %s", err, stderr2.String())
		}
		repos, users, _ := st.snapshot()
		if len(users) != 1 || users[0] != "alice" {
			t.Errorf("resume must not re-create the recorded user, got %v", users)
		}
		// Run 1 recorded the failed libs-generic PUT and its dependent
		// virtual (member check); the resume run retried exactly those
		// two, in order — and then copied the retried repo's artifacts.
		wantRepos := []string{"libs-generic", "libs-virtual", "libs-generic", "libs-virtual"}
		if !reflect.DeepEqual(repos, wantRepos) {
			t.Errorf("repo PUTs = %v, want %v", repos, wantRepos)
		}
		if n := st.artifactCount(); n != 3 {
			t.Errorf("resume must copy the retried repo's artifacts, got %d", n)
		}
	})
}

// TestMigrateSkipUsers pins the B-1 fix on the CLI seam (FR-77 AC2): a
// source that refuses the user listing (the T-228 real-OSS shapes: 400
// license gate, 403 non-admin) aborts without the flag, degrades to an
// explicit warning with it, and a readable listing still migrates users.
func TestMigrateSkipUsers(t *testing.T) {
	t.Setenv("ARTIFACTORY_TOKEN", "src-cred")
	t.Setenv("BINFLOW_TOKEN", "tgt-cred")
	t.Setenv("ARTIFACTORY_URL", "")
	t.Setenv("BINFLOW_SERVER_URL", "")

	runMigrate := func(t *testing.T, artURL, bfURL string, args ...string) (string, error) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		args = append([]string{
			"migrate",
			"--artifactory-url", artURL + "/artifactory",
			"--server", bfURL,
			"--retry-max", "-1",
		}, args...)
		err := run(args, &stdout, &stderr)
		return stdout.String(), err
	}

	t.Run("refused listing aborts without the flag", func(t *testing.T) {
		art := cliArtMock(t, http.StatusForbidden)
		bf, st := cliBFMock(t)
		tmp := t.TempDir()
		_, err := runMigrate(t, art.URL, bf.URL,
			"--progress-file", filepath.Join(tmp, "p1.json"),
			"--report-file", filepath.Join(tmp, "r1.json"))
		if err == nil || !strings.Contains(err.Error(), "user phase aborted before any write") {
			t.Fatalf("error = %v, want the user-phase abort (zero regression without the flag)", err)
		}
		if repos, users, _ := st.snapshot(); len(users) != 0 || len(repos) == 0 {
			t.Errorf("repos phase precedes users (repos written), users not: repos=%v users=%v", repos, users)
		}
		if n := st.artifactCount(); n != 0 {
			t.Errorf("artifacts reached the target (%d), want never attempted", n)
		}
	})

	t.Run("license-gated source degrades to a warning and finishes", func(t *testing.T) {
		art := cliArtMock(t, http.StatusBadRequest) // the OSS "available only in Artifactory Pro" shape
		bf, st := cliBFMock(t)
		tmp := t.TempDir()
		progress := filepath.Join(tmp, "p2.json")
		report := filepath.Join(tmp, "r2.json")
		stdout, err := runMigrate(t, art.URL, bf.URL,
			"--progress-file", progress,
			"--report-file", report,
			"--skip-users") // no --passwords-out: nothing to create
		if err != nil {
			t.Fatalf("run error = %v (the degraded run must succeed)", err)
		}
		for _, want := range []string{
			"users: found=0 migrated=0",
			"warning: users phase skipped via --skip-users",
			"license gate",
			"artifacts: found=3 migrated=3",
		} {
			if !strings.Contains(stdout, want) {
				t.Errorf("summary missing %q:\n%s", want, stdout)
			}
		}
		if repos, users, _ := st.snapshot(); len(users) != 0 || len(repos) != 2 {
			t.Errorf("target writes = repos %v users %v, want 2 repos and no users", repos, users)
		}
		if _, err := os.Stat(filepath.Join(tmp, "passwords.txt")); !os.IsNotExist(err) {
			t.Errorf("no passwords file may be required for a users-free run (err=%v)", err)
		}
		raw, err := os.ReadFile(report)
		if err != nil {
			t.Fatalf("report file: %v", err)
		}
		if !strings.Contains(string(raw), "--skip-users") {
			t.Errorf("report must persist the degradation note:\n%s", raw)
		}
	})

	t.Run("readable listing still migrates users with the flag", func(t *testing.T) {
		art := cliArtMock(t, 0)
		bf, st := cliBFMock(t)
		tmp := t.TempDir()
		stdout, err := runMigrate(t, art.URL, bf.URL,
			"--progress-file", filepath.Join(tmp, "p3.json"),
			"--report-file", filepath.Join(tmp, "r3.json"),
			"--passwords-out", filepath.Join(tmp, "p3-passwords.txt"),
			"--skip-users")
		if err != nil {
			t.Fatalf("run error = %v", err)
		}
		if strings.Contains(stdout, "users phase skipped") {
			t.Errorf("summary must not claim a skip when users migrated:\n%s", stdout)
		}
		if !strings.Contains(stdout, "users: found=2 migrated=1") {
			t.Errorf("summary must account the migrated user:\n%s", stdout)
		}
		if _, users, _ := st.snapshot(); len(users) != 1 || users[0] != "alice" {
			t.Errorf("target users = %v, want [alice]", users)
		}
	})
}
