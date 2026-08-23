package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// T-148 skeleton behavior (kept): help matrix, --version, unknown command
// ---------------------------------------------------------------------------

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
	if got := stdout.String(); !strings.Contains(got, "bf "+version) {
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

func TestRunUnknownNounVerb(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown repo verb", args: []string{"repo", "delete", "x"}, want: "unknown repo verb"},
		{name: "unknown artifact verb", args: []string{"artifact", "download", "x"}, want: "unknown artifact verb"},
		{name: "unknown user verb", args: []string{"user", "rm", "x"}, want: "unknown user verb"},
		{name: "unknown token verb", args: []string{"token", "revoke"}, want: "unknown token verb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tt.args, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("run(%v) error = %v, want %q", tt.args, err, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T-166 AC 1: --help lists the four subcommands (FR-62-AC1 / H56) and each
// subcommand exposes its own help face
// ---------------------------------------------------------------------------

func TestUsageListsSubcommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(--help) error = %v, want nil", err)
	}
	got := stdout.String()
	for _, want := range []string{
		"repo create",
		"artifact upload",
		"user create",
		"token create",
		"--profile",
		"BF_CONFIG",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("usage missing %q", want)
		}
	}
}

func TestSubcommandHelpFaces(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "repo noun help", args: []string{"repo", "--help"}, want: "bf repo create"},
		{name: "repo noun bare", args: []string{"repo"}, want: "bf repo create"},
		{name: "repo create help long", args: []string{"repo", "create", "--help"}, want: "--package-type"},
		{name: "repo create help short", args: []string{"repo", "create", "-h"}, want: "--description"},
		{name: "artifact noun help", args: []string{"artifact", "--help"}, want: "bf artifact upload"},
		{name: "artifact upload help", args: []string{"artifact", "upload", "--help"}, want: "--content-type"},
		{name: "user noun help", args: []string{"user", "--help"}, want: "bf user create"},
		{name: "user create help", args: []string{"user", "create", "--help"}, want: "--password-env"},
		{name: "token noun help", args: []string{"token", "--help"}, want: "bf token create"},
		{name: "token create help", args: []string{"token", "create", "--help"}, want: "--expires-in"},
		{name: "token create help positional mix", args: []string{"token", "create", "stray", "--help"}, want: "--scope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run(tt.args, &stdout, &stderr); err != nil {
				t.Fatalf("run(%v) error = %v, want nil (help exits zero)", tt.args, err)
			}
			if !strings.Contains(stdout.String(), tt.want) {
				t.Errorf("stdout missing %q, got:\n%s", tt.want, stdout.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Fake BinFlow backend (AC 3: httptest-driven call-chain verification)
// ---------------------------------------------------------------------------

// fakeCall is one recorded incoming request.
type fakeCall struct {
	Method      string
	Path        string // decoded request path (r.URL.Path)
	Raw         string // escaped wire spelling (r.URL.EscapedPath())
	Body        string
	Auth        string
	ContentType string
}

// fakeBackend is an httptest server that records every request and answers
// with the response shapes internal/client expects to decode (T-165
// contract). The real server currently answers repo create with a plain
// text body and token create with access_token/int token_id — recorded as
// T-166 leftover debt against internal/client (see reports/agents/T-166.md).
type fakeBackend struct {
	*httptest.Server
	mu    sync.Mutex
	calls []fakeCall
}

func (f *fakeBackend) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{
		Method:      r.Method,
		Path:        r.URL.Path,
		Raw:         r.URL.EscapedPath(),
		Body:        string(body),
		Auth:        r.Header.Get("Authorization"),
		ContentType: r.Header.Get("Content-Type"),
	})
	f.mu.Unlock()
}

func (f *fakeBackend) recorded() []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *fakeBackend) reset() {
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
}

func newFakeBackend(t *testing.T) *fakeBackend {
	t.Helper()
	f := &fakeBackend{}
	mux := http.NewServeMux()
	mux.HandleFunc("/binflow/api/repositories/", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if r.Method != http.MethodPut {
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/binflow/api/repositories/")
		_, _ = fmt.Fprintf(w, `{"key":%q,"rclass":"local","packageType":"generic"}`, key)
	})
	mux.HandleFunc("/binflow/api/security/users/", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if r.Method != http.MethodPut {
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusCreated) // real server: 201, empty body
	})
	mux.HandleFunc("/binflow/api/security/token", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		_, _ = fmt.Fprint(w, `{"token":"bf-test-token-value","token_id":"7","username":"admin","scope":"api:*","expires_in":3600}`)
	})
	mux.HandleFunc("/binflow/", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if r.Method != http.MethodPut {
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusCreated) // upload response body is ignored by the client
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

// isolateCLIEnv neutralizes ambient BF_* variables and pins BF_CONFIG to a
// nonexistent temp path so the developer's real ~/.bf/config.yaml can never
// leak into a test.
func isolateCLIEnv(t *testing.T) string {
	t.Helper()
	for _, k := range []string{"BF_BASE_URL", "BINFLOW_SERVER_URL", "BF_TOKEN", "BF_USERNAME", "BF_PASSWORD"} {
		t.Setenv(k, "")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("BF_CONFIG", path)
	return path
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ---------------------------------------------------------------------------
// AC 3: the four subcommand HTTP call chains, table-driven
// ---------------------------------------------------------------------------

func TestSubcommandCallChain(t *testing.T) {
	isolateCLIEnv(t)
	f := newFakeBackend(t)

	content := "hello binflow\n"
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	file := filepath.Join(t.TempDir(), "artifact.bin")
	writeFile(t, file, content)

	// config with one default profile: server URL + token_env credential
	// reference (no secret on disk).
	profileConfig := fmt.Sprintf("profiles:\n  default:\n    base_url: %s\n    token_env: BF_TEST_TOKEN\n", f.URL)

	tests := []struct {
		name        string
		config      string // YAML written to BF_CONFIG; "" keeps it nonexistent
		env         map[string]string
		args        []string
		wantCalls   int
		wantMethod  string
		wantPath    string
		wantBodyHas []string // substrings the request body must contain
		wantBodyEq  string   // exact request body (overrides wantBodyHas)
		wantAuth    string   // expected Authorization header ("" = absent)
		wantCT      string   // expected Content-Type ("" = unchecked)
		wantStdout  []string
		wantErr     string // error substring; "" = expect success
	}{
		{
			name:        "repo create, positional first",
			config:      profileConfig,
			env:         map[string]string{"BF_TEST_TOKEN": "sekret"},
			args:        []string{"repo", "create", "test-repo", "--type", "local", "--package-type", "generic", "--description", "CI repo"},
			wantCalls:   1,
			wantMethod:  "PUT",
			wantPath:    "/binflow/api/repositories/test-repo",
			wantBodyHas: []string{`"key":"test-repo"`, `"rclass":"local"`, `"packageType":"generic"`, `"description":"CI repo"`},
			wantAuth:    "Bearer sekret",
			wantCT:      "application/json",
			wantStdout:  []string{"Repository 'test-repo' created.\n"},
		},
		{
			name:        "repo create, flags first",
			config:      profileConfig,
			env:         map[string]string{"BF_TEST_TOKEN": "sekret"},
			args:        []string{"repo", "create", "--type", "local", "--package-type", "generic", "test-repo"},
			wantCalls:   1,
			wantMethod:  "PUT",
			wantPath:    "/binflow/api/repositories/test-repo",
			wantBodyHas: []string{`"key":"test-repo"`},
			wantAuth:    "Bearer sekret",
			wantStdout:  []string{"Repository 'test-repo' created.\n"},
		},
		{
			name:       "repo create zero-config via BF_BASE_URL",
			config:     "",
			env:        map[string]string{"BF_BASE_URL": f.URL},
			args:       []string{"repo", "create", "env-repo"},
			wantCalls:  1,
			wantMethod: "PUT",
			wantPath:   "/binflow/api/repositories/env-repo",
			wantAuth:   "",
			wantStdout: []string{"Repository 'env-repo' created.\n"},
		},
		{
			name:    "repo create rejects unknown rclass before any request",
			config:  profileConfig,
			args:    []string{"repo", "create", "x", "--type", "federated"},
			wantErr: `invalid --type "federated"`,
		},
		{
			name:    "repo create rejects unknown package type",
			config:  profileConfig,
			args:    []string{"repo", "create", "x", "--package-type", "gem"},
			wantErr: `invalid --package-type "gem"`,
		},
		{
			name:    "repo create missing key",
			config:  profileConfig,
			args:    []string{"repo", "create", "--type", "local"},
			wantErr: "requires exactly one <key> argument",
		},
		{
			name:       "artifact upload, positional first",
			config:     profileConfig,
			env:        map[string]string{"BF_TEST_TOKEN": "sekret"},
			args:       []string{"artifact", "upload", file, "--repo", "libs-release", "--path", "doc/readme.md"},
			wantCalls:  1,
			wantMethod: "PUT",
			wantPath:   "/binflow/libs-release/doc/readme.md",
			wantBodyEq: content,
			wantAuth:   "Bearer sekret",
			wantCT:     "application/octet-stream",
			wantStdout: []string{"sha256: " + sum + "\n", "uri: " + f.URL + "/binflow/libs-release/doc/readme.md\n"},
		},
		{
			name:       "artifact upload with explicit content type",
			config:     profileConfig,
			args:       []string{"artifact", "upload", "--repo", "libs-release", "--path", "doc/readme.md", "--content-type", "text/x-custom", file},
			wantCalls:  1,
			wantMethod: "PUT",
			wantPath:   "/binflow/libs-release/doc/readme.md",
			wantBodyEq: content,
			wantCT:     "text/x-custom",
			wantStdout: []string{"sha256: " + sum + "\n"},
		},
		{
			name:    "artifact upload requires --repo",
			config:  profileConfig,
			args:    []string{"artifact", "upload", file, "--path", "doc/readme.md"},
			wantErr: "requires --repo",
		},
		{
			name:    "artifact upload requires --path",
			config:  profileConfig,
			args:    []string{"artifact", "upload", file, "--repo", "libs-release"},
			wantErr: "requires --path",
		},
		{
			name:    "artifact upload nonexistent file",
			config:  profileConfig,
			args:    []string{"artifact", "upload", "/nonexistent/artifact.bin", "--repo", "r", "--path", "p"},
			wantErr: "open /nonexistent/artifact.bin",
		},
		{
			name:        "user create",
			config:      profileConfig,
			env:         map[string]string{"BF_TEST_TOKEN": "sekret"},
			args:        []string{"user", "create", "ci-user", "--password", "pw123", "--email", "ci@example.com"},
			wantCalls:   1,
			wantMethod:  "PUT",
			wantPath:    "/binflow/api/security/users/ci-user",
			wantBodyHas: []string{`"name":"ci-user"`, `"password":"pw123"`, `"email":"ci@example.com"`, `"admin":false`},
			wantAuth:    "Bearer sekret",
			wantStdout:  []string{"User 'ci-user' created.\n"},
		},
		{
			name:        "user create admin via password env",
			config:      profileConfig,
			env:         map[string]string{"CI_PW": "env-secret"},
			args:        []string{"user", "create", "ci-admin", "--password-env", "CI_PW", "--email", "a@example.com", "--admin"},
			wantCalls:   1,
			wantMethod:  "PUT",
			wantPath:    "/binflow/api/security/users/ci-admin",
			wantBodyHas: []string{`"password":"env-secret"`, `"admin":true`},
			wantStdout:  []string{"User 'ci-admin' created.\n"},
		},
		{
			name:    "user create requires email",
			config:  profileConfig,
			args:    []string{"user", "create", "x", "--password", "pw"},
			wantErr: "requires --email",
		},
		{
			name:    "user create requires a password source",
			config:  profileConfig,
			args:    []string{"user", "create", "x", "--email", "x@example.com"},
			wantErr: "requires --password or --password-env",
		},
		{
			name:        "token create with all flags",
			config:      profileConfig,
			env:         map[string]string{"BF_TEST_TOKEN": "sekret"},
			args:        []string{"token", "create", "--username", "admin", "--expires-in", "3600", "--scope", "api:*", "--description", "ci token"},
			wantCalls:   1,
			wantMethod:  "POST",
			wantPath:    "/binflow/api/security/token",
			wantBodyHas: []string{`"username":"admin"`, `"expires_in":3600`, `"scope":"api:*"`, `"description":"ci token"`},
			wantAuth:    "Bearer sekret",
			wantStdout:  []string{"bf-test-token-value\n", "token_id: 7\n", "expires_in: 3600\n"},
		},
		{
			name:        "token create defaults subject to profile username",
			config:      fmt.Sprintf("profiles:\n  default:\n    base_url: %s\n    username: deployer\n", f.URL),
			args:        []string{"token", "create"},
			wantCalls:   1,
			wantMethod:  "POST",
			wantPath:    "/binflow/api/security/token",
			wantBodyHas: []string{`"username":"deployer"`},
			wantStdout:  []string{"bf-test-token-value\n"},
		},
		{
			name:    "token create rejects positional arguments",
			config:  profileConfig,
			args:    []string{"token", "create", "stray"},
			wantErr: "takes no positional arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f.reset()
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if tt.config != "" {
				path := filepath.Join(t.TempDir(), "config.yaml")
				writeFile(t, path, tt.config)
				t.Setenv("BF_CONFIG", path)
			}

			var stdout, stderr bytes.Buffer
			err := run(tt.args, &stdout, &stderr)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("run(%v) error = %v, want it to contain %q", tt.args, err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("run(%v) error = %v, want nil", tt.args, err)
			}

			calls := f.recorded()
			if len(calls) != tt.wantCalls {
				t.Fatalf("recorded %d calls, want %d (calls: %+v)", len(calls), tt.wantCalls, calls)
			}
			if tt.wantErr != "" {
				return
			}

			c := calls[0]
			if c.Method != tt.wantMethod {
				t.Errorf("method = %s, want %s", c.Method, tt.wantMethod)
			}
			if c.Path != tt.wantPath {
				t.Errorf("path = %s, want %s", c.Path, tt.wantPath)
			}
			if c.Auth != tt.wantAuth {
				t.Errorf("Authorization = %q, want %q", c.Auth, tt.wantAuth)
			}
			if tt.wantCT != "" && c.ContentType != tt.wantCT {
				t.Errorf("Content-Type = %q, want %q", c.ContentType, tt.wantCT)
			}
			if tt.wantBodyEq != "" && c.Body != tt.wantBodyEq {
				t.Errorf("body = %q, want exactly %q", c.Body, tt.wantBodyEq)
			}
			for _, want := range tt.wantBodyHas {
				if !strings.Contains(c.Body, want) {
					t.Errorf("body %q missing %q", c.Body, want)
				}
			}

			out := stdout.String()
			for _, want := range tt.wantStdout {
				if !strings.Contains(out, want) {
					t.Errorf("stdout %q missing %q", out, want)
				}
			}
			if tt.wantErr == "" && stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty on success", stderr.String())
			}
		})
	}
}

// token create must print the raw token value as the first stdout line so
// `bf token create | grep ...` scripting works (FR-62 / H60).
func TestTokenCreateFirstLineIsToken(t *testing.T) {
	isolateCLIEnv(t)
	f := newFakeBackend(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, path, fmt.Sprintf("profiles:\n  default:\n    base_url: %s\n", f.URL))
	t.Setenv("BF_CONFIG", path)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"token", "create"}, &stdout, &stderr); err != nil {
		t.Fatalf("run error = %v, want nil", err)
	}
	if first := strings.SplitN(stdout.String(), "\n", 2)[0]; first != "bf-test-token-value" {
		t.Errorf("first stdout line = %q, want the raw token value", first)
	}
}

// AC 2: profiles select the server; --profile wins, default_profile is the
// fallback, unknown profiles fail fast (FR-62-AC6 / H61).
func TestProfileSelection(t *testing.T) {
	isolateCLIEnv(t)
	prod := newFakeBackend(t)
	staging := newFakeBackend(t)

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, path, fmt.Sprintf("profiles:\n  default:\n    base_url: %s\n  staging:\n    base_url: %s\n", prod.URL, staging.URL))
	t.Setenv("BF_CONFIG", path)

	// Explicit --profile staging routes to the staging server only.
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--profile", "staging", "repo", "create", "staged-repo"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(--profile staging) error = %v, want nil", err)
	}
	if got := staging.recorded(); len(got) != 1 || got[0].Path != "/binflow/api/repositories/staged-repo" {
		t.Errorf("staging calls = %+v, want one staged-repo create", got)
	}
	if got := prod.recorded(); len(got) != 0 {
		t.Errorf("prod calls = %+v, want none", got)
	}
	if !strings.Contains(stdout.String(), "Repository 'staged-repo' created.\n") {
		t.Errorf("stdout = %q, want creation notice", stdout.String())
	}

	// No --profile: the default profile wins.
	if err := run([]string{"repo", "create", "prod-repo"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(default) error = %v, want nil", err)
	}
	if got := prod.recorded(); len(got) != 1 || got[0].Path != "/binflow/api/repositories/prod-repo" {
		t.Errorf("prod calls = %+v, want one prod-repo create", got)
	}

	// Unknown profile fails fast with the available names listed.
	err := run([]string{"--profile", "nosuch", "repo", "create", "x"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), `profile "nosuch" not found`) {
		t.Fatalf("run(--profile nosuch) error = %v, want profile not found", err)
	}
	if !strings.Contains(err.Error(), "staging") {
		t.Errorf("error %q should list available profiles", err.Error())
	}

	// default_profile re-points the implicit selection.
	writeFile(t, path, fmt.Sprintf("default_profile: staging\nprofiles:\n  default:\n    base_url: %s\n  staging:\n    base_url: %s\n", prod.URL, staging.URL))
	if err := run([]string{"repo", "create", "staged-2"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(default_profile) error = %v, want nil", err)
	}
	if got := staging.recorded(); len(got) != 2 {
		t.Errorf("staging calls = %d, want 2", len(got))
	}
}

// Server 4xx answers surface the errors[].message through the CLI as a
// non-zero exit with the message on stderr (FR-62 error handling).
func TestServerErrorSurfacing(t *testing.T) {
	isolateCLIEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"errors":[{"status":400,"message":"bad repo key: invalid characters"}]}`)
	}))
	t.Cleanup(srv.Close)

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, path, fmt.Sprintf("profiles:\n  default:\n    base_url: %s\n", srv.URL))
	t.Setenv("BF_CONFIG", path)

	var stdout, stderr bytes.Buffer
	err := run([]string{"repo", "create", "BAD KEY"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run error = nil, want the server error")
	}
	for _, want := range []string{"bad repo key: invalid characters", "400"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty on failure", stdout.String())
	}
}

// ---------------------------------------------------------------------------
// Config file handling (AC 2)
// ---------------------------------------------------------------------------

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		missing bool // true = do not create the file at all
		content string
		want    *Config // expected config when wantErr == ""
		wantErr string
	}{
		{
			name:    "missing file yields empty config",
			missing: true,
			want:    &Config{},
		},
		{
			name:    "empty file yields empty config",
			content: "",
			want:    &Config{},
		},
		{
			name: "full profile",
			content: `profiles:
  default:
    base_url: http://localhost:8080
    username: admin
    password_env: BINFLOW_PASSWORD
    skip_tls_verify: true
`,
			want: &Config{Profiles: map[string]Profile{
				"default": {
					BaseURL:       "http://localhost:8080",
					Username:      "admin",
					PasswordEnv:   "BINFLOW_PASSWORD",
					SkipTLSVerify: true,
				},
			}},
		},
		{
			name: "default_profile",
			content: `default_profile: staging
profiles:
  staging:
    base_url: https://staging.example.com
`,
			want: &Config{
				DefaultProfile: "staging",
				Profiles:       map[string]Profile{"staging": {BaseURL: "https://staging.example.com"}},
			},
		},
		{
			name:    "broken YAML",
			content: "profiles: [oops\n",
			wantErr: "parse config YAML",
		},
		{
			name:    "multiple documents rejected",
			content: "profiles: {}\n---\nprofiles: {}\n",
			wantErr: "exactly one document",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if !tt.missing {
				writeFile(t, path, tt.content)
			}
			cfg, err := LoadConfig(path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadConfig error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadConfig error = %v, want nil", err)
			}
			if tt.want.DefaultProfile != cfg.DefaultProfile {
				t.Errorf("DefaultProfile = %q, want %q", cfg.DefaultProfile, tt.want.DefaultProfile)
			}
			if len(cfg.Profiles) != len(tt.want.Profiles) {
				t.Fatalf("profiles = %+v, want %+v", cfg.Profiles, tt.want.Profiles)
			}
			for name, want := range tt.want.Profiles {
				if got := cfg.Profiles[name]; got != want {
					t.Errorf("profile %q = %+v, want %+v", name, got, want)
				}
			}
		})
	}
}

// Credential resolution precedence (AC 2: env references, no disk secrets).
func TestBuildClientCredentials(t *testing.T) {
	t.Run("token env reference resolves at runtime", func(t *testing.T) {
		t.Setenv("BF_TEST_TOKEN", "from-profile-env")
		rt, err := buildClient(Profile{BaseURL: "http://localhost:1", TokenEnv: "BF_TEST_TOKEN"}, "")
		if err != nil {
			t.Fatalf("buildClient error = %v", err)
		}
		if rt.Client.Token != "from-profile-env" {
			t.Errorf("token = %q, want from-profile-env", rt.Client.Token)
		}
	})

	t.Run("BF_TOKEN overrides the profile token_env", func(t *testing.T) {
		t.Setenv("BF_TEST_TOKEN", "from-profile-env")
		t.Setenv("BF_TOKEN", "direct")
		rt, err := buildClient(Profile{BaseURL: "http://localhost:1", TokenEnv: "BF_TEST_TOKEN"}, "")
		if err != nil {
			t.Fatalf("buildClient error = %v", err)
		}
		if rt.Client.Token != "direct" {
			t.Errorf("token = %q, want direct", rt.Client.Token)
		}
	})

	t.Run("basic auth wraps the transport and keeps it out of the token", func(t *testing.T) {
		t.Setenv("BF_TEST_PW", "pw")
		rt, err := buildClient(Profile{BaseURL: "http://localhost:1", Username: "admin", PasswordEnv: "BF_TEST_PW"}, "")
		if err != nil {
			t.Fatalf("buildClient error = %v", err)
		}
		hc := rt.Client.HTTP
		bt, ok := hc.Transport.(*basicAuthTransport)
		if !ok {
			t.Fatalf("transport = %T, want *basicAuthTransport", hc.Transport)
		}
		if got := basicCredentials(bt.user, bt.pass); got != basicCredentials("admin", "pw") {
			t.Errorf("basic credentials = %q, want admin:pw", got)
		}
		if rt.Client.Token != "" {
			t.Errorf("token = %q, want empty (basic auth path)", rt.Client.Token)
		}
		if rt.Username != "admin" {
			t.Errorf("username = %q, want admin", rt.Username)
		}
	})

	t.Run("password without username is an error", func(t *testing.T) {
		t.Setenv("BF_TEST_PW", "pw")
		if _, err := buildClient(Profile{BaseURL: "http://localhost:1", PasswordEnv: "BF_TEST_PW"}, ""); err == nil ||
			!strings.Contains(err.Error(), "no username") {
			t.Fatalf("buildClient error = %v, want no-username error", err)
		}
	})

	t.Run("skip_tls_verify configures an insecure transport", func(t *testing.T) {
		rt, err := buildClient(Profile{BaseURL: "https://localhost:1", SkipTLSVerify: true}, "")
		if err != nil {
			t.Fatalf("buildClient error = %v", err)
		}
		hc := rt.Client.HTTP
		tr, ok := hc.Transport.(*http.Transport)
		if !ok {
			t.Fatalf("transport = %T, want *http.Transport", hc.Transport)
		}
		if tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
			t.Error("TLSClientConfig.InsecureSkipVerify not set")
		}
	})

	t.Run("base URL precedence flag > BF_BASE_URL > alias > profile > default", func(t *testing.T) {
		cases := []struct {
			override string
			env      string
			alias    string
			profile  string
			want     string
		}{
			{override: "http://flag:1", env: "http://env:1", alias: "http://alias:1", profile: "http://profile:1", want: "http://flag:1"},
			{override: "", env: "http://env:1", alias: "http://alias:1", profile: "http://profile:1", want: "http://env:1"},
			{override: "", env: "", alias: "http://alias:1", profile: "http://profile:1", want: "http://alias:1"},
			{override: "", env: "", alias: "", profile: "http://profile:1", want: "http://profile:1"},
			{override: "", env: "", alias: "", profile: "", want: "http://localhost:8080"},
		}
		for _, c := range cases {
			t.Setenv("BF_BASE_URL", c.env)
			t.Setenv("BINFLOW_SERVER_URL", c.alias)
			rt, err := buildClient(Profile{BaseURL: c.profile}, c.override)
			if err != nil {
				t.Fatalf("buildClient error = %v", err)
			}
			if rt.BaseURL != c.want {
				t.Errorf("baseURL = %q, want %q", rt.BaseURL, c.want)
			}
		}
	})

	t.Run("non-http scheme rejected", func(t *testing.T) {
		if _, err := buildClient(Profile{BaseURL: "ftp://localhost:1"}, ""); err == nil ||
			!strings.Contains(err.Error(), "invalid server URL") {
			t.Fatalf("buildClient error = %v, want invalid server URL", err)
		}
	})
}

// ---------------------------------------------------------------------------
// T-231: artifact upload percent-escapes special characters in --path
// (the T-228 D-1 defect class — a literal '%' made the PUT URL unparseable)
// ---------------------------------------------------------------------------

func TestArtifactUploadEscapesSpecialPaths(t *testing.T) {
	isolateCLIEnv(t)
	f := newFakeBackend(t)

	content := "t231 cli payload\n"
	file := filepath.Join(t.TempDir(), "artifact.bin")
	writeFile(t, file, content)
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))

	tests := []struct {
		name     string
		nodePath string
		wantSegs string // escaped repo-relative wire spelling
	}{
		{name: "literal percent (D-1 reproducer)", nodePath: "sym'bols$/percent%.txt", wantSegs: "sym%27bols$/percent%25.txt"},
		{name: "fragment marker", nodePath: "frag#ment.txt", wantSegs: "frag%23ment.txt"},
		{name: "query marker", nodePath: "query?name.txt", wantSegs: "query%3Fname.txt"},
		{name: "space", nodePath: "with space.txt", wantSegs: "with%20space.txt"},
		{name: "utf-8 chinese nested", nodePath: "中文/文件 名.txt", wantSegs: "%E4%B8%AD%E6%96%87/%E6%96%87%E4%BB%B6%20%E5%90%8D.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f.reset()
			path := filepath.Join(t.TempDir(), "config.yaml")
			writeFile(t, path, fmt.Sprintf("profiles:\n  default:\n    base_url: %s\n    token_env: BF_TEST_TOKEN\n", f.URL))
			t.Setenv("BF_CONFIG", path)
			t.Setenv("BF_TEST_TOKEN", "sekret")

			var stdout, stderr bytes.Buffer
			if err := run([]string{"artifact", "upload", file, "--repo", "esc-cli", "--path", tt.nodePath}, &stdout, &stderr); err != nil {
				t.Fatalf("run artifact upload --path %q: %v (before T-231 the '%%' leg died in url.Parse)", tt.nodePath, err)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty on success", stderr.String())
			}

			calls := f.recorded()
			if len(calls) != 1 || calls[0].Method != "PUT" {
				t.Fatalf("calls = %+v, want exactly one PUT", calls)
			}
			// Decoded: the server routes the literal node path; escaped: the
			// wire spelling is percent-encoded per segment.
			if calls[0].Path != "/binflow/esc-cli/"+tt.nodePath {
				t.Errorf("decoded path = %q, want %q", calls[0].Path, "/binflow/esc-cli/"+tt.nodePath)
			}
			if calls[0].Raw != "/binflow/esc-cli/"+tt.wantSegs {
				t.Errorf("wire path = %q, want %q", calls[0].Raw, "/binflow/esc-cli/"+tt.wantSegs)
			}
			if calls[0].Body != content {
				t.Errorf("body = %q, want the file content", calls[0].Body)
			}

			// The printed uri line carries the escaped spelling so the
			// operator's next curl hop is copy-pasteable.
			out := stdout.String()
			if !strings.Contains(out, "sha256: "+sum+"\n") {
				t.Errorf("stdout %q missing sha256 line", out)
			}
			if !strings.Contains(out, "uri: "+f.URL+"/binflow/esc-cli/"+tt.wantSegs+"\n") {
				t.Errorf("stdout %q missing the escaped uri line %q", out, f.URL+"/binflow/esc-cli/"+tt.wantSegs)
			}
		})
	}
}
