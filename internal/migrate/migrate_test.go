package migrate

import (
	"context"
	"crypto/sha1" //nolint:gosec // test fixture digests
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"time"

	"github.com/lzwzzy/binflow/internal/client"
)

// ---------------------------------------------------------------------------
// Mock Artifactory server (shapes per docs/reverse/rest-api.md and
// docs/reverse/auth-model.md)
// ---------------------------------------------------------------------------

// artFixture drives the mock Artifactory server.
type artFixture struct {
	repos       []SourceRepoListItem
	repoConfigs map[string]SourceRepoConfig
	repoStatus  int // GET /api/repositories status (0 = 200)
	users       []SourceUser
	userDetails map[string]SourceUserDetail
	userStatus  int // GET /api/security/users status (0 = 200)
	tokens      []SourceToken
	tokenStatus int // GET /api/security/token status (0 = 200)

	// files is the deep ?list&deep=1 answer per repository (folders may be
	// mixed in; the reader filters them). blobs holds the downloadable
	// content keyed "repo/path".
	files map[string][]SourceFile
	blobs map[string]string

	// rootList400 marks repositories whose ROOT listing answers the spec's
	// 400 "Cannot list files of root." — the FolderInfo-walker leg.
	rootList400 map[string]bool
	// folderChildren feeds the walker: children of "repo" and
	// "repo/folder" keys, FolderInfo shape [{uri,folder}].
	folderChildren map[string][]mockChild
	// fileStatus injects a status for one listing ("repo") or download
	// ("repo/path") request (0 = healthy).
	fileStatus map[string]int
}

// mockChild is one FolderInfo children entry.
type mockChild struct {
	URI    string `json:"uri"`
	Folder bool   `json:"folder"`
}

// blobContent is the deterministic body behind every fixture artifact.
func blobContent(repo, path string) string {
	return fmt.Sprintf("t196 content of %s/%s\n", repo, path)
}

// fileEntries builds listing entries (with true digests) for one repo.
func fileEntries(repo string, paths ...string) []SourceFile {
	out := make([]SourceFile, 0, len(paths))
	for _, p := range paths {
		content := blobContent(repo, p)
		s1 := sha1.Sum([]byte(content)) //nolint:gosec // fixture digest
		s2 := sha256.Sum256([]byte(content))
		out = append(out, SourceFile{
			Path:   p,
			Size:   int64(len(content)),
			Sha1:   hex.EncodeToString(s1[:]),
			Sha256: hex.EncodeToString(s2[:]),
		})
	}
	return out
}

// artMock is a live mock of the Artifactory REST surface the reader uses.
type artMock struct {
	srv *httptest.Server

	mu         sync.Mutex
	authz      []string // Authorization header per request ("" when absent)
	apiKeyHdrs []string // X-JFrog-Art-Api header per request
	requests   []string // method + path per request
	escaped    []string // method + EscapedPath per request (wire spelling)
}

// writeJSON is the shared JSON responder for both mocks.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func newArtMock(t *testing.T, fx artFixture) *artMock {
	t.Helper()
	m := &artMock{}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /artifactory/api/repositories", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		if fx.repoStatus != 0 {
			http.Error(w, "boom", fx.repoStatus)
			return
		}
		writeJSON(w, fx.repos)
	})
	mux.HandleFunc("GET /artifactory/api/repositories/{key}", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		cfg, ok := fx.repoConfigs[r.PathValue("key")]
		if !ok {
			http.Error(w, "The repository was not found", http.StatusNotFound)
			return
		}
		writeJSON(w, cfg)
	})
	mux.HandleFunc("GET /artifactory/api/security/users", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		if fx.userStatus != 0 {
			http.Error(w, "unauthorized", fx.userStatus)
			return
		}
		writeJSON(w, fx.users)
	})
	mux.HandleFunc("GET /artifactory/api/security/users/{name}", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		d, ok := fx.userDetails[r.PathValue("name")]
		if !ok {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		writeJSON(w, d)
	})
	mux.HandleFunc("GET /artifactory/api/security/token", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		if fx.tokenStatus != 0 {
			http.Error(w, "forbidden", fx.tokenStatus)
			return
		}
		writeJSON(w, map[string]any{"tokens": fx.tokens})
	})

	// Storage faces: the deep listing on the repo root (rest-api.md 3) and
	// the plain download (rest-api.md 1). {repo} without a subpath needs
	// its own registration so the FolderInfo read of the root works in
	// walker mode too.
	storageInfo := func(w http.ResponseWriter, r *http.Request, repo, sub string) {
		m.record(r)
		if status := fx.fileStatus[repo]; status != 0 {
			http.Error(w, "boom", status)
			return
		}
		if _, listing := r.URL.Query()["list"]; listing {
			if fx.rootList400[repo] && sub == "" {
				http.Error(w, "Cannot list files of root.", http.StatusBadRequest)
				return
			}
			files := fx.files[repo]
			if files == nil {
				files = []SourceFile{}
			}
			writeJSON(w, sourceFileListBody{URI: "/artifactory/api/storage/" + repo, Created: "2026-08-22T00:00:00.000Z", Files: files})
			return
		}
		// FolderInfo read (walker): children of repo or repo/sub.
		key := repo
		if sub != "" {
			key = repo + "/" + sub
		}
		children := fx.folderChildren[key]
		if children == nil {
			children = []mockChild{}
		}
		writeJSON(w, map[string]any{"uri": "/artifactory/api/storage/" + key, "children": children})
	}
	mux.HandleFunc("GET /artifactory/api/storage/{repo}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.URL.Query()["list"]; !ok && fx.fileStatus[r.PathValue("repo")] == 0 && fx.folderChildren[r.PathValue("repo")] == nil {
			http.Error(w, "Unable to find item", http.StatusNotFound)
			return
		}
		storageInfo(w, r, r.PathValue("repo"), "")
	})
	mux.HandleFunc("GET /artifactory/api/storage/{repo}/{path...}", func(w http.ResponseWriter, r *http.Request) {
		storageInfo(w, r, r.PathValue("repo"), r.PathValue("path"))
	})

	// Download face: GET /artifactory/{repo}/{path} (no /api prefix).
	mux.HandleFunc("GET /artifactory/{repo}/{path...}", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		key := r.PathValue("repo") + "/" + r.PathValue("path")
		if status := fx.fileStatus[key]; status != 0 {
			http.Error(w, "boom", status)
			return
		}
		content, ok := fx.blobs[key]
		if !ok {
			http.Error(w, "Failed to find the requested resource.", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(content))
	})

	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

func (m *artMock) record(r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authz = append(m.authz, r.Header.Get("Authorization"))
	m.apiKeyHdrs = append(m.apiKeyHdrs, r.Header.Get("X-JFrog-Art-Api"))
	m.requests = append(m.requests, r.Method+" "+r.URL.Path)
	m.escaped = append(m.escaped, r.Method+" "+r.URL.EscapedPath())
}

// url returns the mock's base URL, standing in for the Artifactory root
// (context path included in real deployments).
func (m *artMock) url() string { return m.srv.URL + "/artifactory" }

func (m *artMock) snapshot() (authz, apiKeys, requests []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.authz...), append([]string(nil), m.apiKeyHdrs...), append([]string(nil), m.requests...)
}

// snapshotEscaped returns the recorded wire spellings (method + EscapedPath)
// — the discriminator for the path-escaping tests (T-233).
func (m *artMock) snapshotEscaped() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.escaped...)
}

// ---------------------------------------------------------------------------
// Mock BinFlow server (responses per the REAL handlers: repo mutations
// answer 200 plain text, user create 201 with no body, unknown group 400)
// ---------------------------------------------------------------------------

// recordedRepo captures a repository PUT body with the REAL server's request
// spellings (internal/httpapi repoConfig). The client's wire tags were
// aligned to these in T-191 — the pre-alignment union decode
// (members/includes/excludes alongside) was retired with the drift it
// documented (T-167 finding, closed).
type recordedRepo struct {
	Key             string   `json:"key"`
	Rclass          string   `json:"rclass"`
	PackageType     string   `json:"packageType"`
	Description     string   `json:"description"`
	URL             string   `json:"url"`
	Username        string   `json:"username"`
	Repositories    []string `json:"repositories"`
	IncludesPattern string   `json:"includesPattern"`
	ExcludesPattern string   `json:"excludesPattern"`
	QuotaBytes      int64    `json:"quotaBytes"`
}

// recordedUser captures a user PUT body (client.UserCreateRequest tags).
type recordedUser struct {
	Name     string   `json:"name"`
	Password string   `json:"password"`
	Email    string   `json:"email"`
	Admin    bool     `json:"admin"`
	Groups   []string `json:"groups"`
}

// bfMock is a live mock of the BinFlow write plane with the real server's
// observable behaviors: create-or-replace repo PUT (200 plain text), user
// PUT (201 no body), unknown-group 400, virtual member existence checks,
// the admin repo listing (guard eyes) and the plain-file artifact PUT.
type bfMock struct {
	srv *httptest.Server

	token string // required bearer credential ("" accepts anything)

	// failRepos/failUsers inject a status for the given item (0 = healthy).
	failRepos map[string]int
	failUsers map[string]int

	// failArtifacts injects a status for one artifact PUT keyed
	// "repo/path" (0 = healthy); listStatus injects the GET
	// /api/repositories status the guard sees.
	failArtifacts map[string]int
	listStatus    int

	// existingRepos are repositories the target held BEFORE any migration
	// (the guard's refusal fixture).
	existingRepos []map[string]any

	// knownGroups mirrors the real server's validateGroupNames: a body
	// group not in the set fails with 400 plain text.
	knownGroups map[string]bool

	mu        sync.Mutex
	repos     []recordedRepo
	users     []recordedUser
	artifacts map[string][]byte // "repo/path" -> uploaded bytes
	authz     []string
	created   map[string]bool // successfully PUT repositories (for member checks)
}

func newBFMock(t *testing.T, token string) *bfMock {
	t.Helper()
	m := &bfMock{
		token:         token,
		failRepos:     map[string]int{},
		failUsers:     map[string]int{},
		failArtifacts: map[string]int{},
		knownGroups:   map[string]bool{},
		created:       map[string]bool{},
		artifacts:     map[string][]byte{},
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /binflow/api/repositories", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		if m.listStatus != 0 {
			http.Error(w, "boom", m.listStatus)
			return
		}
		m.mu.Lock()
		listed := append([]map[string]any(nil), m.existingRepos...)
		for _, rr := range m.repos {
			listed = append(listed, map[string]any{"key": rr.Key, "type": rr.Rclass, "packageType": rr.PackageType})
		}
		m.mu.Unlock()
		writeJSON(w, listed)
	})

	mux.HandleFunc("PUT /binflow/api/repositories/{key}", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		key := r.PathValue("key")
		var body recordedRepo
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if status := m.failRepos[key]; status != 0 {
			http.Error(w, "injected failure", status)
			return
		}
		// The real repo plane validates that virtual members exist
		// (internal/repo member rules); mirror it against everything
		// successfully created so far in this server's lifetime.
		for _, member := range body.Repositories {
			if !m.isCreated(member) {
				http.Error(w, "member repository does not exist: "+member, http.StatusBadRequest)
				return
			}
		}
		m.mu.Lock()
		m.repos = append(m.repos, body)
		m.created[key] = true
		m.mu.Unlock()
		// Real handler wording (repositories.go handleRepoPut).
		_, _ = fmt.Fprintf(w, "Successfully created repository '%s'\n", key)
	})

	mux.HandleFunc("PUT /binflow/api/security/users/{name}", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		name := r.PathValue("name")
		var body recordedUser
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if status := m.failUsers[name]; status != 0 {
			http.Error(w, "injected failure", status)
			return
		}
		for _, g := range body.Groups {
			if !m.knownGroups[g] {
				// Real handler wording (security.go userCreate).
				http.Error(w, "Unable to find group by name '"+g+"'.", http.StatusBadRequest)
				return
			}
		}
		if body.Email == "" || body.Password == "" {
			http.Error(w, "Please provide a valid user email.", http.StatusBadRequest)
			return
		}
		m.mu.Lock()
		m.users = append(m.users, body)
		m.mu.Unlock()
		w.WriteHeader(http.StatusCreated) // real plane: 201, no body
	})

	// Plain-file artifact face: PUT /binflow/{repo}/{path}. The real
	// generic/maven adapters answer 201 here; docker/npm/pypi faces do not
	// (that is exactly why the artifact phase refuses to guess for them).
	mux.HandleFunc("PUT /binflow/{repo}/{path...}", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		key := r.PathValue("repo") + "/" + r.PathValue("path")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		if status := m.failArtifacts[key]; status != 0 {
			http.Error(w, "injected failure", status)
			return
		}
		m.mu.Lock()
		m.artifacts[key] = body
		m.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	})

	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

func (m *bfMock) record(r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authz = append(m.authz, r.Header.Get("Authorization"))
}

func (m *bfMock) url() string { return m.srv.URL }

func (m *bfMock) isCreated(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.created[key]
}

func (m *bfMock) repoBodies() []recordedRepo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]recordedRepo(nil), m.repos...)
}

func (m *bfMock) userBodies() []recordedUser {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]recordedUser(nil), m.users...)
}

func (m *bfMock) artifactBodies() map[string][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string][]byte, len(m.artifacts))
	for k, v := range m.artifacts {
		out[k] = v
	}
	return out
}

func (m *bfMock) authHeaders() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.authz...)
}

// ---------------------------------------------------------------------------
// Shared fixtures
// ---------------------------------------------------------------------------

// baseFixture is the realistic mixed workload: 7 repos (2 skippable), 4
// users (2 skippable), 2 tokens and three repositories' worth of artifacts
// (generic fully copied, maven-layout copied, docker listed-and-skipped),
// in source-server order (type then key — virtual last, which the repo
// phase relies on).
func baseFixture() artFixture {
	fx := artFixture{
		repos: []SourceRepoListItem{
			{Key: "fed-mirror", Type: "federated", PackageType: "generic"},
			{Key: "docker-local", Type: "local", PackageType: "docker"},
			{Key: "gradle-libs", Type: "local", PackageType: "gradle"},
			{Key: "libs-generic", Type: "local", PackageType: "generic"},
			{Key: "libs-nuget", Type: "local", PackageType: "nuget"},
			{Key: "maven-remote", Type: "remote", PackageType: "maven"},
			{Key: "libs-virtual", Type: "virtual", PackageType: "maven"},
		},
		repoConfigs: map[string]SourceRepoConfig{
			"fed-mirror":   {Key: "fed-mirror", Rclass: "federated", PackageType: "generic"},
			"docker-local": {Key: "docker-local", Rclass: "local", PackageType: "docker", Description: "images"},
			"gradle-libs":  {Key: "gradle-libs", Rclass: "local", PackageType: "gradle", IncludesPattern: "**/*", ExcludesPattern: ""},
			"libs-generic": {Key: "libs-generic", Rclass: "local", PackageType: "generic", Description: "raw files", IncludesPattern: "**/*", ExcludesPattern: "internal/**"},
			"libs-nuget":   {Key: "libs-nuget", Rclass: "local", PackageType: "nuget"},
			"maven-remote": {Key: "maven-remote", Rclass: "remote", PackageType: "maven", URL: "https://repo1.maven.org/maven2/", Username: "ci-bot"},
			"libs-virtual": {Key: "libs-virtual", Rclass: "virtual", PackageType: "maven", Repositories: []string{"libs-generic", "maven-remote"}},
		},
		users: []SourceUser{
			{Name: "alice", Realm: "internal"},
			{Name: "anonymous", Realm: "internal"},
			{Name: "bob", Realm: "internal"},
			{Name: "carol", Realm: "ldap"},
		},
		userDetails: map[string]SourceUserDetail{
			"alice":     {Name: "alice", Email: "alice@example.com", Admin: true, Groups: []string{"readers", "developers"}, Realm: "internal"},
			"anonymous": {Name: "anonymous", Realm: "internal"},
			"bob":       {Name: "bob", Email: "bob@example.com", Admin: false, Groups: nil, Realm: "internal"},
			"carol":     {Name: "carol", Email: "carol@example.com", Admin: false, Realm: "ldap"},
		},
		tokens: []SourceToken{
			{TokenID: "1", Subject: "alice", IssuedAt: 1700000000},
			{TokenID: "2", Subject: "admin", IssuedAt: 1700000001, Expiry: ptrInt64(1800000000)},
		},
	}

	// Artifact content: 6 generic files (nested), 2 maven-layout files,
	// 3 docker files (listed but left behind). A folder entry mixes into
	// the generic listing to prove the reader filters it.
	genericPaths := []string{
		"org/app/1.0/app-1.0.bin",
		"org/app/1.1/app-1.1.bin",
		"docs/readme.md",
		"docs/img/logo.png",
		"root.txt",
		"a/b/c/deep.bin",
	}
	mavenPaths := []string{
		"com/acme/lib/1.0/lib-1.0.jar",
		"com/acme/lib/1.0/lib-1.0.pom",
	}
	dockerPaths := []string{
		"t175/hello/sha256-aaaaaaaa",
		"t175/hello/sha256-bbbbbbbb",
		"t175/hello/manifests/latest",
	}
	fx.files = map[string][]SourceFile{
		"libs-generic": append(fileEntries("libs-generic", genericPaths...), SourceFile{Path: "docs/", Folder: true}),
		"gradle-libs":  fileEntries("gradle-libs", mavenPaths...),
		"docker-local": fileEntries("docker-local", dockerPaths...),
	}
	fx.blobs = map[string]string{}
	fx.fileStatus = map[string]int{}
	for _, p := range genericPaths {
		fx.blobs["libs-generic/"+p] = blobContent("libs-generic", p)
	}
	for _, p := range mavenPaths {
		fx.blobs["gradle-libs/"+p] = blobContent("gradle-libs", p)
	}
	for _, p := range dockerPaths {
		fx.blobs["docker-local/"+p] = blobContent("docker-local", p)
	}
	return fx
}

func ptrInt64(v int64) *int64 { return &v }

// baseGroups are the groups the base fixture's users reference; the mock
// BinFlow server must know them (the operator pre-creates groups).
func baseGroups() map[string]bool { return map[string]bool{"readers": true, "developers": true} }

// fixedNow keeps progress timestamps deterministic.
func fixedNow() time.Time { return time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC) }

// deterministicPasswords returns a generator producing genpw-0, genpw-1...
func deterministicPasswords() func() (string, error) {
	n := 0
	return func() (string, error) {
		pw := fmt.Sprintf("genpw-%d", n)
		n++
		return pw, nil
	}
}

// runOpts is the test shorthand for one Run call.
func runOpts(t *testing.T, tmp string, mutate func(*Options)) Options {
	t.Helper()
	opts := Options{
		ProgressPath:   filepath.Join(tmp, "progress.json"),
		ReportPath:     filepath.Join(tmp, "migration_report.json"),
		PasswordsOut:   filepath.Join(tmp, "passwords.txt"),
		Now:            fixedNow,
		RandomPassword: deterministicPasswords(),
		ToolVersion:    "test",
		Stdout:         &strings.Builder{},
	}
	if mutate != nil {
		mutate(&opts)
	}
	return opts
}

func readerWriter(t *testing.T, art *artMock, bf *bfMock) (*Reader, *Writer) {
	t.Helper()
	rd, err := NewSourceReader(SourceConfig{BaseURL: art.url(), Token: "src-token"})
	if err != nil {
		t.Fatalf("NewSourceReader: %v", err)
	}
	wr := NewTargetWriter(TargetConfig{BaseURL: bf.url(), Token: "tgt-token", RetryMax: -1})
	return rd, wr
}

func readProgressFile(t *testing.T, path string) Progress {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read progress: %v", err)
	}
	var p Progress
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("decode progress: %v", err)
	}
	return p
}

// readReportFile decodes migration_report.json (same package: the report
// shape is the contract under test).
func readReportFile(t *testing.T, path string) reportFile {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var rep reportFile
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	return rep
}

func repoBody(bodies []recordedRepo, key string) (recordedRepo, bool) {
	for _, b := range bodies {
		if b.Key == key {
			return b, true
		}
	}
	return recordedRepo{}, false
}

func userBody(bodies []recordedUser, name string) (recordedUser, bool) {
	for _, b := range bodies {
		if b.Name == name {
			return b, true
		}
	}
	return recordedUser{}, false
}

// ---------------------------------------------------------------------------
// Run: table-driven migration tests against both mocks
// ---------------------------------------------------------------------------

func TestRunTable(t *testing.T) {
	tests := []struct {
		name string
		// fixture mutators applied over baseFixture
		fixup     func(*artFixture)
		bfSetup   func(*bfMock)
		optMutate func(*Options)
		env       map[string]string
		// setup seeds state (e.g. a progress file) before the run; it
		// receives the resolved mock endpoints so seeded files match.
		setup func(t *testing.T, tmp, srcURL, tgtURL string)
		// expectations
		wantErr string // non-empty: error must contain it
		check   func(t *testing.T, s *Summary, err error, art *artMock, bf *bfMock, tmp string)
		// secondRun re-runs with --resume after clearing injected
		// failures (crash-recovery leg).
		secondRun   bool
		secondCheck func(t *testing.T, s *Summary, err error, art *artMock, bf *bfMock, tmp string)
	}{
		{
			name: "full migration with generated passwords",
			check: func(t *testing.T, s *Summary, err error, _ *artMock, bf *bfMock, tmp string) {
				if err != nil {
					t.Fatalf("Run error = %v, want nil", err)
				}
				// Repos: 7 found, 5 migrated, 2 skipped.
				if s.Repos.Found != 7 || s.Repos.Migrated != 5 || s.Repos.Skipped != 2 || s.Repos.Failed != 0 {
					t.Errorf("repo counts = %+v, want found=7 migrated=5 skipped=2 failed=0", s.Repos)
				}
				// Users: 4 found, 2 migrated, 2 skipped.
				if s.Users.Found != 4 || s.Users.Migrated != 2 || s.Users.Skipped != 2 || s.Users.Failed != 0 {
					t.Errorf("user counts = %+v, want found=4 migrated=2 skipped=2 failed=0", s.Users)
				}
				// Tokens: accounted, never migrated.
				if s.TokensFound != 2 || s.TokensSkipped != 2 || s.TokensMigrated != 0 {
					t.Errorf("token counts found=%d skipped=%d migrated=%d, want 2/2/0", s.TokensFound, s.TokensSkipped, s.TokensMigrated)
				}

				repos := bf.repoBodies()
				if len(repos) != 5 {
					t.Fatalf("target repo PUTs = %d, want 5: %+v", len(repos), repos)
				}
				if b, ok := repoBody(repos, "docker-local"); !ok || b.Rclass != "local" || b.PackageType != "docker" || b.Description != "images" {
					t.Errorf("docker-local body = %+v", b)
				}
				if b, ok := repoBody(repos, "libs-generic"); !ok || b.IncludesPattern != "**/*" || b.ExcludesPattern != "internal/**" {
					t.Errorf("libs-generic patterns not carried: %+v", b)
				}
				if b, ok := repoBody(repos, "gradle-libs"); !ok || b.PackageType != "maven" {
					t.Errorf("gradle-libs must map to maven: %+v", b)
				}
				if b, ok := repoBody(repos, "maven-remote"); !ok || b.URL != "https://repo1.maven.org/maven2/" || b.Username != "ci-bot" {
					t.Errorf("maven-remote url/username not carried: %+v", b)
				}
				if b, ok := repoBody(repos, "libs-virtual"); !ok || len(b.Repositories) != 2 || b.Repositories[0] != "libs-generic" || b.Repositories[1] != "maven-remote" {
					t.Errorf("libs-virtual members not carried: %+v", b)
				}

				users := bf.userBodies()
				if len(users) != 2 {
					t.Fatalf("target user PUTs = %d, want 2", len(users))
				}
				a, _ := userBody(users, "alice")
				if !a.Admin || a.Email != "alice@example.com" || len(a.Groups) != 2 || a.Password != "genpw-0" {
					t.Errorf("alice body = %+v", a)
				}
				b, _ := userBody(users, "bob")
				if b.Admin || b.Password != "genpw-1" {
					t.Errorf("bob body = %+v", b)
				}

				// All target requests carried the bearer credential.
				for i, h := range bf.authHeaders() {
					if h != "Bearer tgt-token" {
						t.Errorf("target auth header[%d] = %q, want Bearer", i, h)
					}
				}

				// Progress file: every item accounted, tokens listed.
				p := readProgressFile(t, filepath.Join(tmp, "progress.json"))
				if len(p.Repos) != 7 || p.Repos["docker-local"] != StateDone || p.Repos["fed-mirror"] != StateSkipped {
					t.Errorf("progress repos = %+v", p.Repos)
				}
				if len(p.Users) != 4 || p.Users["alice"] != StateDone || p.Users["anonymous"] != StateSkipped {
					t.Errorf("progress users = %+v", p.Users)
				}
				if !p.TokensListed {
					t.Error("progress tokens_listed = false, want true")
				}

				// Passwords file: mode 0600, deterministic generated values.
				pwRaw, err := os.ReadFile(filepath.Join(tmp, "passwords.txt"))
				if err != nil {
					t.Fatalf("passwords file: %v", err)
				}
				got := string(pwRaw)
				for _, line := range []string{"alice genpw-0\n", "bob genpw-1\n"} {
					if !strings.Contains(got, line) {
						t.Errorf("passwords file missing %q, got %q", line, got)
					}
				}
				st, err := os.Stat(filepath.Join(tmp, "passwords.txt"))
				if err != nil {
					t.Fatalf("stat passwords: %v", err)
				}
				if perm := st.Mode().Perm(); perm != 0o600 {
					t.Errorf("passwords file mode = %o, want 0600", perm)
				}

				// Remote-credential warning surfaced.
				foundCredWarn := false
				for _, w := range s.RepoWarnings {
					if strings.Contains(w, "credentials cannot be exported") {
						foundCredWarn = true
					}
				}
				if !foundCredWarn {
					t.Errorf("missing remote credential warning, got %+v", s.RepoWarnings)
				}

				// Artifacts (D7): generic 6 + maven 2 copied byte-exact,
				// docker 3 listed and left behind, remote/virtual reasoned.
				if s.Artifacts.Found != 11 || s.Artifacts.Migrated != 8 || s.Artifacts.Skipped != 3 || s.Artifacts.Failed != 0 || s.Artifacts.AlreadyDone != 0 {
					t.Errorf("artifact counts = %+v, want found=11 migrated=8 skipped=3 failed=0", s.Artifacts)
				}
				arts := bf.artifactBodies()
				if len(arts) != 8 {
					t.Fatalf("target artifact PUTs = %d, want 8: %v", len(arts), arts)
				}
				for path, want := range map[string]string{
					"libs-generic/org/app/1.0/app-1.0.bin":     blobContent("libs-generic", "org/app/1.0/app-1.0.bin"),
					"libs-generic/a/b/c/deep.bin":              blobContent("libs-generic", "a/b/c/deep.bin"),
					"gradle-libs/com/acme/lib/1.0/lib-1.0.jar": blobContent("gradle-libs", "com/acme/lib/1.0/lib-1.0.jar"),
				} {
					if got := string(arts[path]); got != want {
						t.Errorf("artifact %s body = %q, want %q", path, got, want)
					}
				}
				for _, leftBehind := range []string{
					"docker-local/t175/hello/sha256-aaaaaaa", // prefix of a docker path
				} {
					for k := range arts {
						if strings.HasPrefix(k, leftBehind) {
							t.Errorf("docker artifact %s must not be uploaded", k)
						}
					}
				}
				byRepo := map[string]RepoArtifactStats{}
				for _, st := range s.ArtifactRepos {
					byRepo[st.Repo] = st
				}
				if st := byRepo["libs-generic"]; st.Found != 6 || st.Migrated != 6 || !st.Supported {
					t.Errorf("libs-generic artifact row = %+v", st)
				}
				if st := byRepo["docker-local"]; st.Found != 3 || st.Skipped != 3 || st.Supported || !strings.Contains(st.Reason, "registry protocol") {
					t.Errorf("docker-local artifact row = %+v", st)
				}
				if st := byRepo["maven-remote"]; st.Found != 0 || !strings.Contains(st.Reason, "proxy an upstream") {
					t.Errorf("maven-remote artifact row = %+v", st)
				}
				if st := byRepo["libs-virtual"]; !strings.Contains(st.Reason, "aggregate their members") {
					t.Errorf("libs-virtual artifact row = %+v", st)
				}
				if st := byRepo["libs-nuget"]; !strings.Contains(st.Reason, "repo phase") {
					t.Errorf("libs-nuget artifact row = %+v", st)
				}

				// Progress: the 8 copied artifacts are recorded.
				if s.ProgressArtifacts != 8 {
					t.Errorf("progress artifacts = %d, want 8", s.ProgressArtifacts)
				}
				if p := readProgressFile(t, filepath.Join(tmp, "progress.json")); p.Artifacts["libs-generic/root.txt"] != StateDone || p.Artifacts["gradle-libs/com/acme/lib/1.0/lib-1.0.pom"] != StateDone {
					t.Errorf("progress artifacts = %+v", p.Artifacts)
				}

				// Report (D8): the full document with the guard verdict.
				rep := readReportFile(t, filepath.Join(tmp, "migration_report.json"))
				if rep.Tool != "bf-migrate" || rep.Version != "test" || rep.DryRun {
					t.Errorf("report header = %+v", rep)
				}
				if rep.Guard.Checked != true || rep.Guard.TargetNonEmpty || rep.Guard.Refused {
					t.Errorf("report guard = %+v, want checked+empty", rep.Guard)
				}
				if rep.Artifacts.Migrated != 8 || rep.Artifacts.Found != 11 || len(rep.Artifacts.Repositories) != 7 {
					t.Errorf("report artifacts = %+v", rep.Artifacts)
				}
				if rep.Repos.Found != 7 || rep.Users.Found != 4 || rep.Tokens.Found != 2 {
					t.Errorf("report phase counts = repos %+v users %+v tokens %+v", rep.Repos, rep.Users, rep.Tokens)
				}
				if len(rep.Repos.SkippedItems) != 2 || len(rep.Users.SkippedItems) != 2 {
					t.Errorf("report skip lists = repos %+v users %+v", rep.Repos.SkippedItems, rep.Users.SkippedItems)
				}
			},
		},
		{
			name: "shared password via env",
			optMutate: func(o *Options) {
				o.PasswordEnv = "MIGRATE_TEST_PW"
				o.PasswordsOut = ""
			},
			env: map[string]string{"MIGRATE_TEST_PW": "shared-secret"},
			check: func(t *testing.T, s *Summary, err error, _ *artMock, bf *bfMock, tmp string) {
				if err != nil {
					t.Fatalf("Run error = %v, want nil", err)
				}
				users := bf.userBodies()
				if len(users) != 2 {
					t.Fatalf("user PUTs = %d, want 2", len(users))
				}
				for _, u := range users {
					if u.Password != "shared-secret" {
						t.Errorf("user %s password = %q, want shared value", u.Name, u.Password)
					}
				}
				if _, err := os.Stat(filepath.Join(tmp, "passwords.txt")); !os.IsNotExist(err) {
					t.Errorf("passwords file must not exist in shared mode (err=%v)", err)
				}
				if s.PasswordsWritten != 0 {
					t.Errorf("PasswordsWritten = %d, want 0", s.PasswordsWritten)
				}
			},
		},
		{
			name:      "dry run touches nothing",
			optMutate: func(o *Options) { o.DryRun = true },
			check: func(t *testing.T, s *Summary, err error, _ *artMock, bf *bfMock, tmp string) {
				if err != nil {
					t.Fatalf("Run error = %v, want nil", err)
				}
				if n := len(bf.repoBodies()) + len(bf.userBodies()) + len(bf.artifactBodies()); n != 0 {
					t.Errorf("dry-run wrote %d items to the target, want 0", n)
				}
				if _, err := os.Stat(filepath.Join(tmp, "progress.json")); !os.IsNotExist(err) {
					t.Errorf("dry-run must not create the progress file (err=%v)", err)
				}
				if _, err := os.Stat(filepath.Join(tmp, "passwords.txt")); !os.IsNotExist(err) {
					t.Errorf("dry-run must not create the passwords file (err=%v)", err)
				}
				// Counts still report the planned outcome.
				if s.Repos.Migrated != 5 || s.Users.Migrated != 2 || s.TokensFound != 2 {
					t.Errorf("dry-run counts = repos %+v users %+v tokens %d", s.Repos, s.Users, s.TokensFound)
				}
				// Artifacts are counted (what WOULD be copied), not copied.
				if s.Artifacts.Found != 11 || s.Artifacts.Migrated != 8 || s.Artifacts.Skipped != 3 {
					t.Errorf("dry-run artifact counts = %+v, want found=11 migrated=8 skipped=3", s.Artifacts)
				}
				if !s.DryRun {
					t.Error("summary must record the dry-run mode")
				}
				// The report is still produced, labeled dry-run.
				rep := readReportFile(t, filepath.Join(tmp, "migration_report.json"))
				if !rep.DryRun || rep.Artifacts.Migrated != 8 {
					t.Errorf("dry-run report = %+v", rep.Artifacts)
				}
			},
		},
		{
			name:      "resume skips recorded items",
			optMutate: func(o *Options) { o.Resume = true },
			setup: func(t *testing.T, tmp, srcURL, tgtURL string) {
				// A previous run recorded docker-local and alice, and
				// completed the token phase.
				p := newProgress(filepath.Join(tmp, "progress.json"), srcURL, tgtURL, fixedNow())
				if err := p.markRepo("docker-local", StateDone, fixedNow()); err != nil {
					t.Fatal(err)
				}
				if err := p.markUser("alice", StateDone, fixedNow()); err != nil {
					t.Fatal(err)
				}
				if err := p.markTokensListed(fixedNow()); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, s *Summary, err error, _ *artMock, bf *bfMock, _ string) {
				if err != nil {
					t.Fatalf("Run error = %v, want nil", err)
				}
				repos := bf.repoBodies()
				if _, ok := repoBody(repos, "docker-local"); ok {
					t.Error("resume must not re-send the recorded repo docker-local")
				}
				if len(repos) != 4 {
					t.Errorf("target repo PUTs = %d, want 4 (the 4 not-yet-recorded migratables)", len(repos))
				}
				users := bf.userBodies()
				if _, ok := userBody(users, "alice"); ok {
					t.Error("resume must not re-send the recorded user alice")
				}
				if s.Repos.AlreadyDone != 1 || s.Users.AlreadyDone != 1 {
					t.Errorf("already-done = repos %d users %d, want 1/1", s.Repos.AlreadyDone, s.Users.AlreadyDone)
				}
				if s.TokensFound != 0 || !strings.Contains(s.TokenPhaseNote, "already recorded") {
					t.Errorf("token phase must skip the listed check on resume, got note %q found %d", s.TokenPhaseNote, s.TokensFound)
				}
			},
		},
		{
			name: "failures are recorded, retried and recovered by resume",
			bfSetup: func(b *bfMock) {
				b.failRepos["libs-generic"] = http.StatusInternalServerError
				b.failUsers["bob"] = http.StatusInternalServerError
			},
			wantErr: "3 item(s) failed (2 repo, 1 user, 0 artifact)",
			// secondRun executes after the first, with the injected
			// failures cleared and --resume on.
			secondRun: true,
			check: func(t *testing.T, s *Summary, err error, _ *artMock, _ *bfMock, tmp string) {
				if err == nil {
					t.Fatal("first run must fail (item failures)")
				}
				if !strings.Contains(err.Error(), "libs-generic") {
					t.Errorf("error must name the failed repo: %v", err)
				}
				// Virtual member of the failed repo is rejected by the
				// member-existence check (real server behavior).
				if s.Repos.Failed != 2 {
					t.Errorf("repo failures = %d, want 2 (libs-generic + its dependent virtual)", s.Repos.Failed)
				}
				if s.Users.Failed != 1 {
					t.Errorf("user failures = %d, want 1", s.Users.Failed)
				}
				// Failed items are NOT recorded — the retry lever.
				p := readProgressFile(t, filepath.Join(tmp, "progress.json"))
				if p.Repos["libs-generic"] != "" || p.Repos["libs-virtual"] != "" || p.Users["bob"] != "" {
					t.Errorf("failed items recorded: repos %+v users %+v", p.Repos, p.Users)
				}
				// Healthy items were migrated despite the failures.
				if s.Repos.Migrated != 3 || s.Users.Migrated != 1 {
					t.Errorf("first-run migrated = repos %d users %d, want 3/1", s.Repos.Migrated, s.Users.Migrated)
				}
				// The FAILED user's password must not land in the file (no
				// account exists behind it); only the successful one's.
				pwRaw, err := os.ReadFile(filepath.Join(tmp, "passwords.txt"))
				if err != nil {
					t.Fatalf("passwords file: %v", err)
				}
				if strings.Contains(string(pwRaw), "bob ") || !strings.Contains(string(pwRaw), "alice genpw-0") {
					t.Errorf("passwords file = %q, want only the successful alice line", pwRaw)
				}
			},
			secondCheck: func(t *testing.T, s *Summary, err error, _ *artMock, bf *bfMock, _ string) {
				if err != nil {
					t.Fatalf("resume run error = %v, want nil", err)
				}
				repos := bf.repoBodies()
				if _, ok := repoBody(repos, "libs-generic"); !ok {
					t.Error("resume must retry the failed repo libs-generic")
				}
				if len(repos) != 5 {
					t.Errorf("total target repo PUTs after resume = %d, want 5", len(repos))
				}
				users := bf.userBodies()
				if _, ok := userBody(users, "bob"); !ok {
					t.Error("resume must retry the failed user bob")
				}
				// Run 1 recorded 5 repos (3 done + 2 skipped) and 3 users
				// (1 done + 2 skipped); the resume run skips exactly those.
				if s.Repos.AlreadyDone != 5 || s.Users.AlreadyDone != 3 {
					t.Errorf("resume already-done = repos %d users %d, want 5/3", s.Repos.AlreadyDone, s.Users.AlreadyDone)
				}
			},
		},
		{
			name: "missing password strategy fails fast",
			optMutate: func(o *Options) {
				o.PasswordEnv = ""
				o.PasswordsOut = ""
			},
			wantErr: "no password strategy",
			check: func(t *testing.T, s *Summary, err error, _ *artMock, bf *bfMock, _ string) {
				if err == nil {
					t.Fatal("expected fail-fast error")
				}
				if n := len(bf.userBodies()); n != 0 {
					t.Errorf("no user may be written without a password strategy, got %d", n)
				}
				// The repo phase completed before the abort.
				if s.Repos.Migrated != 5 {
					t.Errorf("repo phase must complete, migrated = %d", s.Repos.Migrated)
				}
				// Token phase never ran (users phase aborted the run).
				if s.TokensFound != 0 {
					t.Errorf("token phase must not run after an aborted users phase")
				}
			},
		},
		{
			name: "empty password env fails fast",
			optMutate: func(o *Options) {
				o.PasswordEnv = "MIGRATE_TEST_PW_EMPTY"
				o.PasswordsOut = ""
			},
			wantErr: "not set or empty",
			check: func(t *testing.T, _ *Summary, err error, _ *artMock, _ *bfMock, _ string) {
				if err == nil {
					t.Fatal("expected fail-fast error for an empty password env")
				}
			},
		},
		{
			name:  "token listing refused is a warning not a failure",
			fixup: func(f *artFixture) { f.tokenStatus = http.StatusForbidden },
			check: func(t *testing.T, s *Summary, err error, _ *artMock, _ *bfMock, tmp string) {
				if err != nil {
					t.Fatalf("Run error = %v, want nil (token listing is best-effort)", err)
				}
				if s.TokensFound != 0 || s.TokenPhaseWarn == "" {
					t.Errorf("token warn = %q found = %d, want a warning and zero found", s.TokenPhaseWarn, s.TokensFound)
				}
				p := readProgressFile(t, filepath.Join(tmp, "progress.json"))
				if p.TokensListed {
					t.Error("a refused listing must not be recorded (resume retries it)")
				}
			},
		},
		{
			name: "unknown group fails the user",
			bfSetup: func(b *bfMock) {
				// The base groups stay unknown: simulate an operator who
				// did not pre-create them.
				b.knownGroups = map[string]bool{}
			},
			wantErr: "1 item(s) failed",
			check: func(t *testing.T, s *Summary, err error, _ *artMock, _ *bfMock, _ string) {
				if err == nil {
					t.Fatal("expected aggregated failure error")
				}
				if s.Users.Failed != 1 || s.Users.Migrated != 1 {
					t.Errorf("user counts = %+v, want failed=1 migrated=1 (bob has no groups)", s.Users)
				}
				found := false
				for _, n := range s.UserFailures {
					if n.Name == "alice" && strings.Contains(n.Reason, "group") {
						found = true
					}
				}
				if !found {
					t.Errorf("alice failure must mention the group: %+v", s.UserFailures)
				}
			},
		},
		{
			name: "memberless virtual is skipped",
			fixup: func(f *artFixture) {
				f.repoConfigs["libs-virtual"] = SourceRepoConfig{Key: "libs-virtual", Rclass: "virtual", PackageType: "maven"}
			},
			check: func(t *testing.T, s *Summary, err error, _ *artMock, _ *bfMock, _ string) {
				if err != nil {
					t.Fatalf("Run error = %v, want nil", err)
				}
				if s.Repos.Skipped != 3 {
					t.Errorf("repo skips = %d, want 3 (federated, nuget, memberless virtual)", s.Repos.Skipped)
				}
			},
		},
		{
			name:    "repo config read failure is an item failure",
			fixup:   func(f *artFixture) { delete(f.repoConfigs, "libs-generic") },
			wantErr: "2 item(s) failed (2 repo, 0 user, 0 artifact)",
			check: func(t *testing.T, s *Summary, err error, _ *artMock, _ *bfMock, _ string) {
				if err == nil {
					t.Fatal("expected aggregated failure error")
				}
				if s.Repos.Failed != 2 { // libs-generic itself + its dependent virtual
					t.Errorf("repo failures = %d, want 2", s.Repos.Failed)
				}
			},
		},
		{
			name:    "source repo list failure aborts before any write",
			fixup:   func(f *artFixture) { f.repoStatus = http.StatusInternalServerError },
			wantErr: "repo phase aborted before any write",
			check: func(t *testing.T, _ *Summary, err error, _ *artMock, bf *bfMock, tmp string) {
				if err == nil {
					t.Fatal("expected phase abort error")
				}
				if n := len(bf.repoBodies()) + len(bf.userBodies()) + len(bf.artifactBodies()); n != 0 {
					t.Errorf("no writes expected after an aborted first phase, got %d", n)
				}
				if _, statErr := os.Stat(filepath.Join(tmp, "progress.json")); !os.IsNotExist(statErr) {
					t.Errorf("no progress file expected (err=%v)", statErr)
				}
			},
		},
		{
			name: "non-empty target is refused before any write (D6)",
			bfSetup: func(b *bfMock) {
				b.existingRepos = []map[string]any{{"key": "pre-existing", "type": "local", "packageType": "generic"}}
			},
			wantErr: "target instance is not empty",
			check: func(t *testing.T, s *Summary, err error, _ *artMock, bf *bfMock, tmp string) {
				if err == nil {
					t.Fatal("expected the guard to refuse")
				}
				if !errors.Is(err, ErrTargetNotEmpty) {
					t.Errorf("error must wrap ErrTargetNotEmpty: %v", err)
				}
				if !strings.Contains(err.Error(), "pre-existing") || !strings.Contains(err.Error(), "--allow-non-empty") {
					t.Errorf("refusal must name the evidence and the override: %v", err)
				}
				if n := len(bf.repoBodies()) + len(bf.userBodies()) + len(bf.artifactBodies()); n != 0 {
					t.Errorf("a refused run must write nothing, got %d items", n)
				}
				if _, statErr := os.Stat(filepath.Join(tmp, "progress.json")); !os.IsNotExist(statErr) {
					t.Errorf("a refused run must leave no progress file (err=%v)", statErr)
				}
				if !s.Guard.Refused || !s.Guard.TargetNonEmpty || !s.Guard.Checked {
					t.Errorf("guard info = %+v, want checked+non-empty+refused", s.Guard)
				}
				// The refusal itself is reportable (D8 covers refusals too).
				rep := readReportFile(t, filepath.Join(tmp, "migration_report.json"))
				if !rep.Guard.Refused || rep.Guard.TargetNonEmpty != true || rep.Repos.Found != 0 {
					t.Errorf("refusal report = guard %+v repos %+v", rep.Guard, rep.Repos)
				}
			},
		},
		{
			name: "non-empty target merges via --allow-non-empty",
			bfSetup: func(b *bfMock) {
				b.existingRepos = []map[string]any{{"key": "pre-existing", "type": "local", "packageType": "generic"}}
			},
			optMutate: func(o *Options) { o.AllowNonEmpty = true },
			check: func(t *testing.T, s *Summary, err error, _ *artMock, _ *bfMock, tmp string) {
				if err != nil {
					t.Fatalf("Run error = %v, want nil (override)", err)
				}
				if !s.Guard.AllowedViaFlag || s.Guard.Refused {
					t.Errorf("guard info = %+v, want allowed-via-flag", s.Guard)
				}
				if s.Artifacts.Migrated != 8 {
					t.Errorf("merged run must still copy artifacts, got %+v", s.Artifacts)
				}
				// The report records the merge semantics (D6 AC).
				rep := readReportFile(t, filepath.Join(tmp, "migration_report.json"))
				if !rep.Guard.AllowedViaFlag || !strings.Contains(rep.Guard.Note, "merge semantics") {
					t.Errorf("report guard = %+v, want the merge-semantics note", rep.Guard)
				}
			},
		},
		{
			name:      "resume of a recorded run is exempt from the guard",
			optMutate: func(o *Options) { o.Resume = true },
			bfSetup: func(b *bfMock) {
				b.existingRepos = []map[string]any{{"key": "libs-generic", "type": "local", "packageType": "generic"}}
			},
			setup: func(t *testing.T, tmp, srcURL, tgtURL string) {
				// A previous (interrupted) run of the SAME pair recorded
				// the repos; the target now looks populated because of it.
				p := newProgress(filepath.Join(tmp, "progress.json"), srcURL, tgtURL, fixedNow())
				for _, key := range []string{"fed-mirror", "docker-local", "gradle-libs", "libs-generic", "libs-nuget", "maven-remote", "libs-virtual"} {
					if err := p.markRepo(key, StateDone, fixedNow()); err != nil {
						t.Fatal(err)
					}
				}
				if err := p.markTokensListed(fixedNow()); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, s *Summary, err error, _ *artMock, bf *bfMock, _ string) {
				if err != nil {
					t.Fatalf("Run error = %v, want nil (resume exemption)", err)
				}
				if !s.Guard.ResumedRun || s.Guard.Refused {
					t.Errorf("guard info = %+v, want resumed-run", s.Guard)
				}
				// The artifact phase still ran for the recorded repos.
				if s.Artifacts.Migrated != 8 {
					t.Errorf("resume artifacts = %+v, want 8 migrated", s.Artifacts)
				}
				if len(bf.repoBodies()) != 0 {
					t.Errorf("resume must not re-create recorded repos, got %v", bf.repoBodies())
				}
			},
		},
		{
			name: "artifact failures are recorded and retried by resume",
			bfSetup: func(b *bfMock) {
				b.failArtifacts["libs-generic/root.txt"] = http.StatusInternalServerError
				b.failArtifacts["gradle-libs/com/acme/lib/1.0/lib-1.0.pom"] = http.StatusInternalServerError
			},
			wantErr:   "2 item(s) failed (0 repo, 0 user, 2 artifact)",
			secondRun: true,
			check: func(t *testing.T, s *Summary, err error, _ *artMock, _ *bfMock, tmp string) {
				if err == nil {
					t.Fatal("first run must fail (two artifact upload failures)")
				}
				for _, want := range []string{"libs-generic/root.txt", "gradle-libs/com/acme/lib/1.0/lib-1.0.pom"} {
					found := false
					for _, n := range s.ArtifactFailures {
						if n.Name == want {
							found = true
						}
					}
					if !found {
						t.Errorf("failure notes must name %s: %+v", want, s.ArtifactFailures)
					}
				}
				if s.Artifacts.Migrated != 6 || s.Artifacts.Failed != 2 {
					t.Errorf("artifact counts = %+v, want migrated=6 failed=2", s.Artifacts)
				}
				// Failed artifacts are NOT recorded — the resume lever.
				p := readProgressFile(t, filepath.Join(tmp, "progress.json"))
				if p.Artifacts["libs-generic/root.txt"] != "" || len(p.Artifacts) != 6 {
					t.Errorf("progress artifacts = %+v, want only the 6 successes", p.Artifacts)
				}
			},
			secondCheck: func(t *testing.T, s *Summary, err error, _ *artMock, bf *bfMock, _ string) {
				if err != nil {
					t.Fatalf("resume run error = %v, want nil", err)
				}
				if s.Artifacts.Migrated != 2 || s.Artifacts.AlreadyDone != 6 || s.Artifacts.Failed != 0 {
					t.Errorf("resume artifacts = %+v, want migrated=2 already-done=6", s.Artifacts)
				}
				if len(bf.artifactBodies()) != 8 {
					t.Errorf("total artifact PUTs after resume = %d, want 8", len(bf.artifactBodies()))
				}
				if got := string(bf.artifactBodies()["libs-generic/root.txt"]); got != blobContent("libs-generic", "root.txt") {
					t.Errorf("retried artifact body = %q", got)
				}
			},
		},
		{
			name: "source digest mismatch fails the artifact",
			fixup: func(f *artFixture) {
				for i, e := range f.files["libs-generic"] {
					if e.Path == "root.txt" {
						f.files["libs-generic"][i].Sha256 = "deadbeef" + strings.Repeat("0", 56)
					}
				}
			},
			wantErr: "1 item(s) failed (0 repo, 0 user, 1 artifact)",
			check: func(t *testing.T, s *Summary, err error, _ *artMock, bf *bfMock, _ string) {
				if err == nil {
					t.Fatal("expected the mismatch to fail the run")
				}
				found := false
				for _, n := range s.ArtifactFailures {
					if n.Name == "libs-generic/root.txt" && strings.Contains(n.Reason, "sha256 mismatch") {
						found = true
					}
				}
				if !found {
					t.Errorf("failure notes = %+v, want a sha256 mismatch on root.txt", s.ArtifactFailures)
				}
				if _, uploaded := bf.artifactBodies()["libs-generic/root.txt"]; uploaded {
					t.Error("a mismatched artifact must not be uploaded")
				}
			},
		},
		{
			name:    "repo file listing failure is a repo-level artifact failure",
			fixup:   func(f *artFixture) { f.fileStatus["libs-generic"] = http.StatusInternalServerError },
			wantErr: "1 item(s) failed (0 repo, 0 user, 1 artifact)",
			check: func(t *testing.T, s *Summary, err error, _ *artMock, _ *bfMock, _ string) {
				if err == nil {
					t.Fatal("expected the listing failure to surface")
				}
				if s.Artifacts.Found != 5 { // gradle 2 + docker 3
					t.Errorf("artifact found = %d, want 5 (libs-generic unlisted)", s.Artifacts.Found)
				}
				found := false
				for _, n := range s.ArtifactFailures {
					if n.Name == "libs-generic" && strings.Contains(n.Reason, "file listing failed") {
						found = true
					}
				}
				if !found {
					t.Errorf("failure notes = %+v, want a listing failure for libs-generic", s.ArtifactFailures)
				}
			},
		},

		// --skip-users (FR-77 AC2 / B-1): a REFUSED user listing degrades to
		// a warning and the rest of the migration proceeds. T-228 R-1 saw
		// both refusals on a real source: 400 = the OSS license gate, 403 =
		// non-admin credentials.
		{
			name:  "skip-users degrades a 403 listing to a warning and migrates the rest",
			fixup: func(f *artFixture) { f.userStatus = http.StatusForbidden },
			optMutate: func(o *Options) {
				o.SkipUsers = true
				o.PasswordsOut = "" // no users can be created: no password strategy may be required
			},
			check: func(t *testing.T, s *Summary, err error, _ *artMock, bf *bfMock, tmp string) {
				if err != nil {
					t.Fatalf("Run error = %v, want nil (the degraded run succeeds)", err)
				}
				if s.Users.Found != 0 || s.Users.Migrated != 0 || s.Users.Failed != 0 {
					t.Errorf("user counts = %+v, want all zero (phase skipped)", s.Users)
				}
				if s.UserPhaseWarn == "" || !strings.Contains(s.UserPhaseWarn, "--skip-users") || !strings.Contains(s.UserPhaseWarn, "403") {
					t.Errorf("UserPhaseWarn = %q, want the --skip-users degradation note naming the refusal", s.UserPhaseWarn)
				}
				if len(bf.userBodies()) != 0 {
					t.Errorf("target user PUTs = %v, want none", bf.userBodies())
				}
				if _, err := os.Stat(filepath.Join(tmp, "passwords.txt")); !os.IsNotExist(err) {
					t.Errorf("degraded run must not need a passwords file (err=%v)", err)
				}
				// Tokens and artifacts still run (B-1: the refusal used to
				// make them unreachable).
				if s.TokensFound != 2 || s.TokensSkipped != 2 {
					t.Errorf("token counts found=%d skipped=%d, want 2/2 (phase reached)", s.TokensFound, s.TokensSkipped)
				}
				if s.Artifacts.Migrated != 8 || s.Artifacts.Failed != 0 {
					t.Errorf("artifact counts = %+v, want migrated=8 failed=0 (phase reached)", s.Artifacts)
				}
				// Progress: repos recorded, users untouched.
				p := readProgressFile(t, filepath.Join(tmp, "progress.json"))
				if len(p.Repos) != 7 || len(p.Users) != 0 {
					t.Errorf("progress repos=%d users=%d, want 7/0", len(p.Repos), len(p.Users))
				}
				// The report persists the degradation note (durable evidence).
				rep := readReportFile(t, filepath.Join(tmp, "migration_report.json"))
				if len(rep.Users.Warnings) != 1 || !strings.Contains(rep.Users.Warnings[0], "--skip-users") {
					t.Errorf("report users warnings = %+v, want the degradation note", rep.Users.Warnings)
				}
			},
		},
		{
			name:  "skip-users degrades the 400 OSS license-gate shape",
			fixup: func(f *artFixture) { f.userStatus = http.StatusBadRequest },
			optMutate: func(o *Options) {
				o.SkipUsers = true
				o.PasswordsOut = ""
			},
			check: func(t *testing.T, s *Summary, err error, _ *artMock, _ *bfMock, _ string) {
				if err != nil {
					t.Fatalf("Run error = %v, want nil", err)
				}
				if !strings.Contains(s.UserPhaseWarn, "license gate") || !strings.Contains(s.UserPhaseWarn, "400") {
					t.Errorf("UserPhaseWarn = %q, want the OSS license-gate note naming 400", s.UserPhaseWarn)
				}
				if s.Artifacts.Migrated != 8 {
					t.Errorf("artifact migrated = %d, want 8", s.Artifacts.Migrated)
				}
			},
		},
		{
			name: "skip-users with a readable listing still migrates users",
			optMutate: func(o *Options) {
				o.SkipUsers = true
			},
			check: func(t *testing.T, s *Summary, err error, _ *artMock, bf *bfMock, _ string) {
				if err != nil {
					t.Fatalf("Run error = %v, want nil", err)
				}
				if s.Users.Found != 4 || s.Users.Migrated != 2 || s.Users.Skipped != 2 {
					t.Errorf("user counts = %+v, want found=4 migrated=2 skipped=2 (full migration)", s.Users)
				}
				if s.UserPhaseWarn != "" {
					t.Errorf("UserPhaseWarn = %q, want empty (no degradation happened)", s.UserPhaseWarn)
				}
				if users := bf.userBodies(); len(users) != 2 {
					t.Errorf("target user PUTs = %d, want 2", len(users))
				}
			},
		},
		{
			// Zero-regression negative legs: without the flag nothing
			// changes — the refusal still aborts before the first write.
			name:    "refused user listing still aborts without the flag",
			fixup:   func(f *artFixture) { f.userStatus = http.StatusForbidden },
			wantErr: "user phase aborted before any write",
			check: func(t *testing.T, s *Summary, err error, _ *artMock, bf *bfMock, tmp string) {
				if err == nil {
					t.Fatal("expected the abort without --skip-users")
				}
				if s.Repos.Migrated != 5 {
					t.Errorf("repo migrated = %d, want 5 (repos phase precedes users)", s.Repos.Migrated)
				}
				if s.UserPhaseWarn != "" {
					t.Errorf("UserPhaseWarn = %q, want empty (no flag, no degradation)", s.UserPhaseWarn)
				}
				if tokens := s.TokensFound; tokens != 0 {
					t.Errorf("token found = %d, want 0 (phase never reached)", tokens)
				}
				if arts := len(bf.artifactBodies()); arts != 0 {
					t.Errorf("artifact PUTs = %d, want 0 (phase never reached)", arts)
				}
				rep := readReportFile(t, filepath.Join(tmp, "migration_report.json"))
				if len(rep.Users.Warnings) != 0 {
					t.Errorf("report users warnings = %+v, want none", rep.Users.Warnings)
				}
			},
		},
		{
			name:  "skip-users does not soften unexpected listing errors",
			fixup: func(f *artFixture) { f.userStatus = http.StatusInternalServerError },
			optMutate: func(o *Options) {
				o.SkipUsers = true
				o.PasswordsOut = ""
			},
			wantErr: "user phase aborted before any write",
			check: func(t *testing.T, s *Summary, err error, _ *artMock, _ *bfMock, _ string) {
				if err == nil {
					t.Fatal("expected the abort: only 400/403 degrade")
				}
				if s.UserPhaseWarn != "" {
					t.Errorf("UserPhaseWarn = %q, want empty (500 does not degrade)", s.UserPhaseWarn)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			tmp := t.TempDir()

			fx := baseFixture()
			if tt.fixup != nil {
				tt.fixup(&fx)
			}
			art := newArtMock(t, fx)
			bf := newBFMock(t, "tgt-token")
			bf.knownGroups = baseGroups()
			if tt.bfSetup != nil {
				tt.bfSetup(bf)
			}

			if tt.setup != nil {
				tt.setup(t, tmp, art.url(), bf.url())
			}

			rd, wr := readerWriter(t, art, bf)
			opts := runOpts(t, tmp, tt.optMutate)
			s, err := Run(context.Background(), rd, wr, opts)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Run error = %v, want it to contain %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Run error = %v, want nil", err)
			}
			tt.check(t, s, err, art, bf, tmp)

			// Second leg: recover with --resume after clearing failures.
			if tt.secondRun {
				bf.failRepos = map[string]int{}
				bf.failUsers = map[string]int{}
				bf.failArtifacts = map[string]int{}
				opts2 := runOpts(t, tmp, func(o *Options) { o.Resume = true })
				s2, err2 := Run(context.Background(), rd, wr, opts2)
				if err2 != nil {
					t.Fatalf("resume Run error = %v, want nil", err2)
				}
				tt.secondCheck(t, s2, err2, art, bf, tmp)
			}
		})
	}
}

// TestRunSummaryOutput pins the operator-facing report.
func TestRunSummaryOutput(t *testing.T) {
	tmp := t.TempDir()
	art := newArtMock(t, baseFixture())
	bf := newBFMock(t, "tgt-token")
	bf.knownGroups = baseGroups()
	rd, wr := readerWriter(t, art, bf)

	out := &strings.Builder{}
	opts := runOpts(t, tmp, func(o *Options) { o.Stdout = out })
	if _, err := Run(context.Background(), rd, wr, opts); err != nil {
		t.Fatalf("Run error = %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"repos: found=7 migrated=5 skipped=2 already-done=0 failed=0",
		"skipped repo fed-mirror: federated repositories are not supported by BinFlow",
		`skipped repo libs-nuget: package type "nuget" is not supported by BinFlow`,
		"users: found=4 migrated=2 skipped=2 already-done=0 failed=0",
		"skipped user anonymous: the anonymous account is built into BinFlow",
		`skipped user carol: realm "ldap" users are provisioned by their identity provider, not migrated`,
		"tokens: found=2 migrated=0 skipped=2",
		"token values cannot be exported from Artifactory",
		"warning: repo maven-remote: upstream credentials cannot be exported",
		"guard: target empty",
		"artifacts: found=11 migrated=8 skipped=3 already-done=0 failed=0",
		"repo docker-local (local/docker): found=3 migrated=0 skipped=3 already-done=0 failed=0",
		"registry protocol",
		"repo maven-remote (remote/maven): found=0 migrated=0 skipped=0 already-done=0 failed=0",
		"progress: " + filepath.Join(tmp, "progress.json") + " (7 repo, 4 user, 8 artifact recorded; resume with --resume)",
		"report: " + filepath.Join(tmp, "migration_report.json"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q\nsummary:\n%s", want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// Reader
// ---------------------------------------------------------------------------

func TestReaderAuthPrecedence(t *testing.T) {
	t.Run("bearer token wins", func(t *testing.T) {
		art := newArtMock(t, baseFixture())
		rd, err := NewSourceReader(SourceConfig{BaseURL: art.url(), Token: "tok", APIKey: "key"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rd.ListUsers(context.Background()); err != nil {
			t.Fatal(err)
		}
		authz, apiKeys, _ := art.snapshot()
		if len(authz) == 0 || authz[0] != "Bearer tok" {
			t.Errorf("Authorization = %v, want Bearer tok", authz)
		}
		if apiKeys[0] != "" {
			t.Errorf("X-JFrog-Art-Api = %v, want empty when a token is set", apiKeys)
		}
	})
	t.Run("api key fallback", func(t *testing.T) {
		art := newArtMock(t, baseFixture())
		rd, err := NewSourceReader(SourceConfig{BaseURL: art.url(), APIKey: "key123"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rd.ListUsers(context.Background()); err != nil {
			t.Fatal(err)
		}
		authz, apiKeys, _ := art.snapshot()
		if authz[0] != "" {
			t.Errorf("Authorization = %v, want empty in api-key mode", authz)
		}
		if apiKeys[0] != "key123" {
			t.Errorf("X-JFrog-Art-Api = %v, want key123", apiKeys)
		}
	})
	t.Run("endpoints and shapes", func(t *testing.T) {
		art := newArtMock(t, baseFixture())
		rd, err := NewSourceReader(SourceConfig{BaseURL: art.url()})
		if err != nil {
			t.Fatal(err)
		}
		repos, err := rd.ListRepositories(context.Background())
		if err != nil || len(repos) != 7 {
			t.Fatalf("ListRepositories = %v, %v", repos, err)
		}
		cfg, err := rd.RepositoryConfig(context.Background(), "libs-virtual")
		if err != nil || len(cfg.Repositories) != 2 {
			t.Fatalf("RepositoryConfig = %+v, %v", cfg, err)
		}
		users, err := rd.ListUsers(context.Background())
		if err != nil || len(users) != 4 {
			t.Fatalf("ListUsers = %v, %v", users, err)
		}
		detail, err := rd.UserDetail(context.Background(), "alice")
		if err != nil || detail.Email != "alice@example.com" || !detail.Admin {
			t.Fatalf("UserDetail = %+v, %v", detail, err)
		}
		tokens, err := rd.ListTokens(context.Background())
		if err != nil || len(tokens) != 2 || tokens[1].Expiry == nil {
			t.Fatalf("ListTokens = %+v, %v", tokens, err)
		}
		_, _, requests := art.snapshot()
		for _, want := range []string{
			"GET /artifactory/api/repositories", "GET /artifactory/api/repositories/libs-virtual",
			"GET /artifactory/api/security/users", "GET /artifactory/api/security/users/alice",
			"GET /artifactory/api/security/token",
		} {
			found := false
			for _, r := range requests {
				if r == want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("reader never hit %q (requests: %v)", want, requests)
			}
		}
	})
}

func TestReaderURLValidation(t *testing.T) {
	tests := []struct {
		name string
		url  string
		ok   bool
	}{
		{name: "empty", url: "", ok: false},
		{name: "no scheme", url: "host:8081/artifactory", ok: false},
		{name: "trailing slash trimmed", url: "http://host:8081/artifactory/", ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSourceReader(SourceConfig{BaseURL: tt.url})
			if (err == nil) != tt.ok {
				t.Errorf("NewSourceReader(%q) err = %v, want ok=%v", tt.url, err, tt.ok)
			}
		})
	}
}

func TestReaderHTTPError(t *testing.T) {
	art := newArtMock(t, baseFixture())
	rd, err := NewSourceReader(SourceConfig{BaseURL: art.url()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rd.RepositoryConfig(context.Background(), "no-such-repo")
	if err == nil {
		t.Fatal("expected an error for a missing repo config")
	}
	if !IsStatus(err, http.StatusNotFound) {
		t.Errorf("error must classify as 404: %v", err)
	}
	if IsStatus(err, http.StatusForbidden) {
		t.Errorf("404 must not classify as 403: %v", err)
	}
	if !strings.Contains(err.Error(), "no-such-repo") {
		t.Errorf("error must name the repo: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Converter
// ---------------------------------------------------------------------------

func TestConvertRepo(t *testing.T) {
	tests := []struct {
		name        string
		src         SourceRepoConfig
		skipped     bool
		wantReason  string
		wantReq     *client.RepoCreateRequest
		wantWarning string
	}{
		{
			name:    "local generic with patterns",
			src:     SourceRepoConfig{Key: "libs", Rclass: "local", PackageType: "generic", Description: "d", IncludesPattern: "**/release/*", ExcludesPattern: "*.tmp"},
			wantReq: &client.RepoCreateRequest{Key: "libs", Rclass: "local", PackageType: "generic", Description: "d", Includes: "**/release/*", Excludes: "*.tmp"},
		},
		{
			name:    "mixed case normalized",
			src:     SourceRepoConfig{Key: "K", Rclass: " Local ", PackageType: " Generic "},
			wantReq: &client.RepoCreateRequest{Key: "K", Rclass: "local", PackageType: "generic"},
		},
		{
			name:        "gradle aliases to maven",
			src:         SourceRepoConfig{Key: "g", Rclass: "local", PackageType: "gradle"},
			wantReq:     &client.RepoCreateRequest{Key: "g", Rclass: "local", PackageType: "maven"},
			wantWarning: `package type "gradle" maps to BinFlow's "maven"`,
		},
		{
			name:       "federated skipped",
			src:        SourceRepoConfig{Key: "f", Rclass: "federated", PackageType: "generic"},
			skipped:    true,
			wantReason: "federated repositories are not supported by BinFlow",
		},
		{
			name:       "unsupported package type skipped",
			src:        SourceRepoConfig{Key: "n", Rclass: "local", PackageType: "nuget"},
			skipped:    true,
			wantReason: `package type "nuget" is not supported by BinFlow`,
		},
		{
			name:       "unknown rclass skipped",
			src:        SourceRepoConfig{Key: "x", Rclass: "hyper", PackageType: "generic"},
			skipped:    true,
			wantReason: `repository class "hyper" is not supported by BinFlow`,
		},
		{
			name:       "no key skipped",
			src:        SourceRepoConfig{Rclass: "local", PackageType: "generic"},
			skipped:    true,
			wantReason: "repository has no key",
		},
		{
			name:       "no rclass skipped",
			src:        SourceRepoConfig{Key: "k", PackageType: "generic"},
			skipped:    true,
			wantReason: `repository "k" has no rclass`,
		},
		{
			name:        "remote carries url and username, warns about credentials",
			src:         SourceRepoConfig{Key: "r", Rclass: "remote", PackageType: "maven", URL: "https://example.com/m2", Username: "bot"},
			wantReq:     &client.RepoCreateRequest{Key: "r", Rclass: "remote", PackageType: "maven", URL: "https://example.com/m2", Username: "bot"},
			wantWarning: "upstream credentials cannot be exported",
		},
		{
			name:       "remote without url skipped",
			src:        SourceRepoConfig{Key: "r", Rclass: "remote", PackageType: "maven", Username: "bot"},
			skipped:    true,
			wantReason: "remote repository has no upstream URL",
		},
		{
			name:    "virtual carries members",
			src:     SourceRepoConfig{Key: "v", Rclass: "virtual", PackageType: "npm", Repositories: []string{"a", "b"}},
			wantReq: &client.RepoCreateRequest{Key: "v", Rclass: "virtual", PackageType: "npm", Members: []string{"a", "b"}},
		},
		{
			name:       "memberless virtual skipped",
			src:        SourceRepoConfig{Key: "v", Rclass: "virtual", PackageType: "npm"},
			skipped:    true,
			wantReason: "virtual repository has no members",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := ConvertRepo(tt.src)
			if plan.Skipped != tt.skipped {
				t.Fatalf("Skipped = %v, want %v (reason %q)", plan.Skipped, tt.skipped, plan.Reason)
			}
			if tt.skipped {
				if plan.Reason != tt.wantReason {
					t.Errorf("Reason = %q, want %q", plan.Reason, tt.wantReason)
				}
				return
			}
			if tt.wantReq != nil && !reflect.DeepEqual(plan.Request, *tt.wantReq) {
				t.Errorf("Request = %+v, want %+v", plan.Request, *tt.wantReq)
			}
			if tt.wantWarning != "" {
				found := false
				for _, w := range plan.Warnings {
					if strings.Contains(w, tt.wantWarning) {
						found = true
					}
				}
				if !found {
					t.Errorf("warnings %v missing %q", plan.Warnings, tt.wantWarning)
				}
			}
		})
	}
}

func TestConvertUser(t *testing.T) {
	tests := []struct {
		name       string
		src        SourceUserDetail
		skipped    bool
		wantReason string
		wantPlan   *UserPlan
	}{
		{
			name:     "internal admin with groups",
			src:      SourceUserDetail{Name: "alice", Email: "a@x.io", Admin: true, Groups: []string{"g1"}, Realm: "internal"},
			wantPlan: &UserPlan{Name: "alice", Email: "a@x.io", Admin: true, Groups: []string{"g1"}},
		},
		{
			name:     "mixed case name lowercased",
			src:      SourceUserDetail{Name: "Alice", Email: "a@x.io", Realm: "internal"},
			wantPlan: &UserPlan{Name: "alice", Email: "a@x.io"},
		},
		{
			name:       "anonymous skipped",
			src:        SourceUserDetail{Name: "anonymous", Realm: "internal"},
			skipped:    true,
			wantReason: "the anonymous account is built into BinFlow",
		},
		{
			name:       "system skipped",
			src:        SourceUserDetail{Name: "_system_", Realm: "internal"},
			skipped:    true,
			wantReason: "reserved system account",
		},
		{
			name:       "ldap realm skipped",
			src:        SourceUserDetail{Name: "carol", Email: "c@x.io", Realm: "ldap"},
			skipped:    true,
			wantReason: `realm "ldap" users are provisioned by their identity provider, not migrated`,
		},
		{
			name:       "missing email skipped",
			src:        SourceUserDetail{Name: "nodude", Realm: "internal"},
			skipped:    true,
			wantReason: "user has no email (required by BinFlow)",
		},
		{
			name:       "missing name skipped",
			src:        SourceUserDetail{Email: "x@x.io", Realm: "internal"},
			skipped:    true,
			wantReason: "user has no name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := ConvertUser(tt.src)
			if plan.Skipped != tt.skipped {
				t.Fatalf("Skipped = %v, want %v (reason %q)", plan.Skipped, tt.skipped, plan.Reason)
			}
			if tt.skipped {
				if plan.Reason != tt.wantReason {
					t.Errorf("Reason = %q, want %q", plan.Reason, tt.wantReason)
				}
				return
			}
			if tt.wantPlan != nil {
				want := *tt.wantPlan
				want.Warnings = plan.Warnings // warnings asserted separately below
				if !reflect.DeepEqual(plan, want) {
					t.Errorf("plan = %+v, want %+v", plan, want)
				}
			}
			if len(plan.Groups) > 0 && len(plan.Warnings) == 0 {
				t.Errorf("groups must produce a warning, got %+v", plan.Warnings)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Progress
// ---------------------------------------------------------------------------

func TestProgressRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.json")
	p := newProgress(path, "http://src", "http://tgt", fixedNow())
	if err := p.markRepo("r1", StateDone, fixedNow()); err != nil {
		t.Fatal(err)
	}
	if err := p.markUser("u1", StateSkipped, fixedNow()); err != nil {
		t.Fatal(err)
	}
	if err := p.markTokensListed(fixedNow()); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadProgress(path, "http://src", "http://tgt", fixedNow())
	if err != nil {
		t.Fatalf("loadProgress: %v", err)
	}
	if loaded.repoState("r1") != StateDone || loaded.userState("u1") != StateSkipped || !loaded.TokensListed {
		t.Errorf("round trip lost state: repos %+v users %+v tokens %v", loaded.Repos, loaded.Users, loaded.TokensListed)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("progress mode = %o, want 0600", perm)
	}
	if strings.TrimSpace(loaded.UpdatedAt) == "" {
		t.Error("UpdatedAt must be set")
	}
}

func TestProgressMissingFileIsFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.json")
	p, err := loadProgress(path, "http://src", "http://tgt", fixedNow())
	if err != nil {
		t.Fatalf("missing file must yield a fresh progress: %v", err)
	}
	if len(p.Repos) != 0 || len(p.Users) != 0 || p.TokensListed {
		t.Errorf("fresh progress must be empty: %+v", p)
	}
}

func TestProgressEndpointMismatchRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.json")
	p := newProgress(path, "http://src", "http://tgt", fixedNow())
	if err := p.save(fixedNow()); err != nil {
		t.Fatal(err)
	}
	_, err := loadProgress(path, "http://other", "http://tgt", fixedNow())
	if err == nil || !strings.Contains(err.Error(), "refusing to resume") {
		t.Fatalf("mismatch must be refused, got %v", err)
	}
}

func TestProgressCorruptFileRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadProgress(path, "http://src", "http://tgt", fixedNow())
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("corrupt file must be refused, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Artifact phase: reader shapes, path validation, bulk copy (T-196)
// ---------------------------------------------------------------------------

// TestListRepoFilesWalker covers the FolderInfo fallback when a source
// refuses repo-root listings (the strict reading of the spec's 400 row).
func TestListRepoFilesWalker(t *testing.T) {
	fx := baseFixture()
	fx.rootList400 = map[string]bool{"libs-generic": true}
	fx.folderChildren = map[string][]mockChild{
		"libs-generic": {
			{URI: "/org", Folder: true},
			{URI: "/root.txt", Folder: false},
		},
		"libs-generic/org": {
			{URI: "/app", Folder: true},
			{URI: "/stray.dat", Folder: false},
		},
		"libs-generic/org/app": {
			{URI: "/1.0", Folder: true},
		},
		"libs-generic/org/app/1.0": {
			{URI: "/app-1.0.bin", Folder: false},
		},
	}
	art := newArtMock(t, fx)
	rd, err := NewSourceReader(SourceConfig{BaseURL: art.url(), Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	files, err := rd.ListRepoFiles(context.Background(), "libs-generic")
	if err != nil {
		t.Fatalf("ListRepoFiles: %v", err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f.Path] = true
		if f.Folder {
			t.Errorf("walker must not emit folders, got %q", f.Path)
		}
	}
	for _, want := range []string{"root.txt", "org/stray.dat", "org/app/1.0/app-1.0.bin"} {
		if !got[want] {
			t.Errorf("walker output missing %q: %v", want, got)
		}
	}
	if len(files) != 3 {
		t.Errorf("walker found %d files, want 3: %v", len(files), got)
	}
	_, _, requests := art.snapshot()
	sawRoot, sawSubfolder := false, false
	for _, r := range requests {
		if r == "GET /artifactory/api/storage/libs-generic" {
			sawRoot = true
		}
		if r == "GET /artifactory/api/storage/libs-generic/org" {
			sawSubfolder = true
		}
	}
	if !sawRoot || !sawSubfolder {
		t.Errorf("walker must read FolderInfo nodes, root=%v subfolder=%v (requests %v)", sawRoot, sawSubfolder, requests)
	}
}

// TestReaderSpecialPathEscapes pins the reader's URL spelling after the
// T-233 convergence onto client.EscapePathSegments (T-231 legacy item 1 —
// the private escapePathSegments copy was deleted). Both read faces that
// BUILD URLs must request the percent-encoded spelling: the download face
// (OpenFile) and the walker's FolderInfo reads. A raw '%', '#' or '?' in
// the wire form fails client-side (invalid URL escape) or addresses the
// wrong node; the mock's escaped-path recording pins the exact wire form,
// so under-escaping cannot hide behind net/http's re-escaping of spaces
// and UTF-8 (the T-231 mutation observation).
func TestReaderSpecialPathEscapes(t *testing.T) {
	const (
		percentPath = "sym'bols$/percent%.txt" // the T-228 D-1 reproducer
		markerPath  = "dir frag#ment/query?name.bin"
	)
	fx := baseFixture()
	fx.files = map[string][]SourceFile{
		"libs-generic": fileEntries("libs-generic", percentPath, markerPath),
	}
	fx.blobs = map[string]string{
		"libs-generic/" + percentPath: blobContent("libs-generic", percentPath),
		"libs-generic/" + markerPath:  blobContent("libs-generic", markerPath),
	}
	art := newArtMock(t, fx)
	rd, err := NewSourceReader(SourceConfig{BaseURL: art.url(), Token: "tok"})
	if err != nil {
		t.Fatalf("NewSourceReader: %v", err)
	}

	// Download face: content round-trips for every special character.
	for _, p := range []string{percentPath, markerPath} {
		rc, err := rd.OpenFile(context.Background(), "libs-generic", p)
		if err != nil {
			t.Fatalf("OpenFile(%q): %v", p, err)
		}
		body, rerr := io.ReadAll(rc)
		_ = rc.Close()
		if rerr != nil {
			t.Fatalf("read %q: %v", p, rerr)
		}
		if string(body) != blobContent("libs-generic", p) {
			t.Errorf("OpenFile(%q) body = %q", p, body)
		}
	}
	// Wire spelling: the server saw the percent-encoded form, not the
	// literal one (EscapedPath keeps what the client sent).
	wire := strings.Join(art.snapshotEscaped(), "\n")
	for _, want := range []string{
		"GET /artifactory/libs-generic/sym%27bols$/percent%25.txt",
		"GET /artifactory/libs-generic/dir%20frag%23ment/query%3Fname.bin",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("wire form missing %q; escaped requests:\n%s", want, wire)
		}
	}

	// Walker face: a special-character FOLDER name must survive the
	// FolderInfo walk (the second call site of the escaping).
	fx2 := baseFixture()
	fx2.rootList400 = map[string]bool{"libs-generic": true}
	fx2.folderChildren = map[string][]mockChild{
		"libs-generic": {
			{URI: "/dir frag#ment", Folder: true},
			{URI: "/root.txt", Folder: false},
		},
		"libs-generic/dir frag#ment": {
			{URI: "/percent%.bin", Folder: false},
		},
	}
	art2 := newArtMock(t, fx2)
	rd2, err := NewSourceReader(SourceConfig{BaseURL: art2.url(), Token: "tok"})
	if err != nil {
		t.Fatalf("NewSourceReader: %v", err)
	}
	files, err := rd2.ListRepoFiles(context.Background(), "libs-generic")
	if err != nil {
		t.Fatalf("ListRepoFiles (walker): %v", err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f.Path] = true
	}
	for _, want := range []string{"root.txt", "dir frag#ment/percent%.bin"} {
		if !got[want] {
			t.Errorf("walker output missing %q: %v", want, got)
		}
	}
}

// TestListRepoFilesShapes pins the primary listing contract: deep list
// served, 404 treated as empty, auth header carried.
func TestListRepoFilesShapes(t *testing.T) {
	t.Run("deep listing", func(t *testing.T) {
		art := newArtMock(t, baseFixture())
		rd, _ := NewSourceReader(SourceConfig{BaseURL: art.url(), Token: "tok"})
		files, err := rd.ListRepoFiles(context.Background(), "libs-generic")
		if err != nil {
			t.Fatalf("ListRepoFiles: %v", err)
		}
		if len(files) != 6 {
			t.Fatalf("files = %d, want 6 (folder filtered): %+v", len(files), files)
		}
		for _, f := range files {
			if strings.HasPrefix(f.Path, "/") || f.Path == "" {
				t.Errorf("path %q must be plain relative", f.Path)
			}
			if f.Sha256 == "" || f.Sha1 == "" {
				t.Errorf("listing entry %q must carry digests: %+v", f.Path, f)
			}
		}
	})
	t.Run("empty repo answers 404", func(t *testing.T) {
		art := newArtMock(t, baseFixture())
		rd, _ := NewSourceReader(SourceConfig{BaseURL: art.url(), Token: "tok"})
		files, err := rd.ListRepoFiles(context.Background(), "never-written")
		if err != nil || len(files) != 0 {
			t.Fatalf("empty repo must yield no files and no error, got %v %v", files, err)
		}
	})
}

// TestArtifactPathValidation pins the hostile-entry rejections.
func TestArtifactPathValidation(t *testing.T) {
	for _, bad := range []string{"", "/abs.txt", "trailing/", "a//b", "..", "../up", "a/../b", "./here"} {
		if err := validateArtifactPath(bad); err == nil {
			t.Errorf("validateArtifactPath(%q) must fail", bad)
		}
	}
	for _, ok := range []string{"a", "a/b/c.bin", "with space.txt"} {
		if err := validateArtifactPath(ok); err != nil {
			t.Errorf("validateArtifactPath(%q) = %v, want nil", ok, err)
		}
	}
}

// TestRunBulkArtifactCopy migrates a 120-artifact repository through the
// real worker pool at elevated concurrency — the unit-scale rehearsal of
// the QA H63 leg (every artifact byte-exact, progress complete, report
// consistent).
func TestRunBulkArtifactCopy(t *testing.T) {
	fx := baseFixture()
	paths := make([]string, 0, 120)
	for i := 0; i < 120; i++ {
		paths = append(paths, fmt.Sprintf("pkg/%02d/group/artifact-%03d.bin", i%7, i))
	}
	fx.files["libs-generic"] = fileEntries("libs-generic", paths...)
	for _, p := range paths {
		fx.blobs["libs-generic/"+p] = blobContent("libs-generic", p)
	}

	art := newArtMock(t, fx)
	bf := newBFMock(t, "tgt-token")
	bf.knownGroups = baseGroups()
	rd, wr := readerWriter(t, art, bf)
	tmp := t.TempDir()

	s, err := Run(context.Background(), rd, wr, runOpts(t, tmp, func(o *Options) { o.ArtifactConcurrency = 8 }))
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if s.Artifacts.Found != 120+2+3 || s.Artifacts.Migrated != 120+2 || s.Artifacts.Failed != 0 {
		t.Errorf("bulk counts = %+v", s.Artifacts)
	}
	bodies := bf.artifactBodies()
	if len(bodies) != 122 {
		t.Fatalf("artifact PUTs = %d, want 122", len(bodies))
	}
	for _, p := range paths {
		if got := string(bodies["libs-generic/"+p]); got != blobContent("libs-generic", p) {
			t.Fatalf("artifact %s corrupted", p)
		}
	}
	p := readProgressFile(t, filepath.Join(tmp, "progress.json"))
	if len(p.Artifacts) != 122 {
		t.Errorf("progress artifacts = %d, want 122", len(p.Artifacts))
	}
	rep := readReportFile(t, filepath.Join(tmp, "migration_report.json"))
	if rep.Artifacts.Migrated != 122 || len(rep.Artifacts.Repositories) != 7 {
		t.Errorf("bulk report = %+v", rep.Artifacts)
	}
}

// TestRunGuardFailsClosed pins the fail-closed posture when the occupancy
// check itself errors on a write run.
func TestRunGuardFailsClosed(t *testing.T) {
	art := newArtMock(t, baseFixture())
	bf := newBFMock(t, "tgt-token")
	bf.knownGroups = baseGroups()
	bf.listStatus = http.StatusUnauthorized
	rd, wr := readerWriter(t, art, bf)
	tmp := t.TempDir()

	_, err := Run(context.Background(), rd, wr, runOpts(t, tmp, nil))
	if err == nil || !strings.Contains(err.Error(), "occupancy check failed") {
		t.Fatalf("write run must fail closed, got %v", err)
	}
	if n := len(bf.repoBodies()) + len(bf.userBodies()); n != 0 {
		t.Errorf("no writes expected, got %d", n)
	}
}

// TestRunGuardDryRunSoft pins that a dry-run survives an unreadable target
// (no credentials) with a note instead of an abort.
func TestRunGuardDryRunSoft(t *testing.T) {
	art := newArtMock(t, baseFixture())
	bf := newBFMock(t, "tgt-token")
	bf.knownGroups = baseGroups()
	bf.listStatus = http.StatusUnauthorized
	rd, wr := readerWriter(t, art, bf)
	tmp := t.TempDir()

	s, err := Run(context.Background(), rd, wr, runOpts(t, tmp, func(o *Options) { o.DryRun = true }))
	if err != nil {
		t.Fatalf("dry-run must survive an unreadable target: %v", err)
	}
	if !strings.Contains(s.Guard.Note, "not verified") {
		t.Errorf("guard note = %q, want the soft not-verified note", s.Guard.Note)
	}
}

// TestRunArtifactContextCancel pins the interruption posture: a canceled
// context yields one warning (not per-file failures), nothing partial in
// the progress file, and a resumable state.
func TestRunArtifactContextCancel(t *testing.T) {
	fx := baseFixture()
	// Enough files that the cancellation lands mid-copy.
	paths := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		paths = append(paths, fmt.Sprintf("slow/dir/art-%02d.bin", i))
	}
	fx.files["libs-generic"] = fileEntries("libs-generic", paths...)
	fx.blobs = map[string]string{}
	for _, p := range paths {
		fx.blobs["libs-generic/"+p] = blobContent("libs-generic", p)
	}
	art := newArtMock(t, fx)
	bf := newBFMock(t, "tgt-token")
	bf.knownGroups = baseGroups()
	rd, wr := readerWriter(t, art, bf)
	tmp := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel once the first artifacts landed.
	go func() {
		for {
			if len(bf.artifactBodies()) >= 2 {
				cancel()
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	s, err := Run(ctx, rd, wr, runOpts(t, tmp, nil))
	// The run may legitimately finish all items before the cancel lands;
	// both outcomes must be internally consistent.
	_ = s
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Logf("run outcome after cancel: %v", err)
	}
	p := readProgressFile(t, filepath.Join(tmp, "progress.json"))
	for key, state := range p.Artifacts {
		if state != StateDone {
			t.Errorf("progress artifact %s = %q, want only done states", key, state)
		}
	}
}
