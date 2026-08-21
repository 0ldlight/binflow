package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/client"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// errorBody returns the JSON errors[] envelope for the given status and message.
func errorBody(status int, message string) string {
	b, _ := json.Marshal(map[string]interface{}{
		"errors": []map[string]interface{}{
			{"status": status, "message": message},
		},
	})
	return string(b)
}

// ---------------------------------------------------------------------------
// Client construction
// ---------------------------------------------------------------------------

func TestClientDefaults(t *testing.T) {
	c := client.New()
	if c.BaseURL != client.DefaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, client.DefaultBaseURL)
	}
	if c.RetryMax != client.DefaultRetryMax {
		t.Errorf("RetryMax = %d, want %d", c.RetryMax, client.DefaultRetryMax)
	}
	if c.Token != "" {
		t.Errorf("Token = %q, want empty", c.Token)
	}
}

// TestClientAbsURL verifies base URL joining end-to-end: the mock server
// records the absolute URL it received and the test compares it against the
// expected join result. Trailing-slash variants prove the client trims them
// before appending the path. The zero-value default and non-routable custom
// base URLs are covered by TestAbsURLJoining in client_internal_test.go.
func TestClientAbsURL(t *testing.T) {
	tests := []struct {
		name   string
		suffix string // appended to the server URL to form the client BaseURL
	}{
		{name: "base URL without trailing slash", suffix: ""},
		{name: "base URL with one trailing slash", suffix: "/"},
		{name: "base URL with two trailing slashes", suffix: "//"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotURL string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotURL = "http://" + r.Host + r.URL.String()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			}))
			defer srv.Close()

			c := client.New()
			c.BaseURL = srv.URL + tt.suffix
			c.RetryMax = -1 // no retries

			if _, err := c.ListRepos(context.Background()); err != nil {
				t.Fatalf("ListRepos: %v", err)
			}

			want := srv.URL + "/binflow/api/repositories"
			if gotURL != want {
				t.Errorf("request URL = %q, want %q", gotURL, want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Auth header injection
// ---------------------------------------------------------------------------

func TestAuthHeaderInjection(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{name: "no token", token: "", want: ""},
		{name: "bearer token", token: "abc123", want: "Bearer abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got := r.Header.Get("Authorization")
				if got != tt.want {
					t.Errorf("Authorization = %q, want %q", got, tt.want)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			}))
			defer srv.Close()

			c := client.New()
			c.BaseURL = srv.URL
			c.Token = tt.token
			c.RetryMax = -1 // no retries

			_, err := c.ListRepos(context.Background())
			if err != nil {
				t.Errorf("ListRepos: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Error envelope parsing
// ---------------------------------------------------------------------------

func TestErrorEnvelopeParsing(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		body           string
		wantStatusCode int
		wantContains   string
	}{
		{
			name:           "404 not found",
			statusCode:     404,
			body:           errorBody(404, "Failed to find the repository"),
			wantStatusCode: 404,
			wantContains:   "Failed to find the repository",
		},
		{
			name:           "401 unauthorized",
			statusCode:     401,
			body:           errorBody(401, "authentication required"),
			wantStatusCode: 401,
			wantContains:   "authentication required",
		},
		{
			name:           "403 forbidden",
			statusCode:     403,
			body:           errorBody(403, "permission denied"),
			wantStatusCode: 403,
			wantContains:   "permission denied",
		},
		{
			name:           "500 internal server error",
			statusCode:     500,
			body:           errorBody(500, "internal server error"),
			wantStatusCode: 500,
			wantContains:   "internal server error",
		},
		{
			name:           "non-JSON body falls back to raw",
			statusCode:     502,
			body:           "Bad Gateway",
			wantStatusCode: 502,
			wantContains:   "Bad Gateway",
		},
		{
			name:           "empty errors array",
			statusCode:     500,
			body:           `{"errors":[]}`,
			wantStatusCode: 500,
			wantContains:   `{"errors":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			c := client.New()
			c.BaseURL = srv.URL
			c.RetryMax = -1 // no retries

			_, err := c.ListRepos(context.Background())
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantContains) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantContains)
			}
			var se *client.StatusError
			if !errors.As(err, &se) {
				t.Fatalf("error is %T, want *client.StatusError", err)
			}
			if se.StatusCode != tt.wantStatusCode {
				t.Errorf("StatusError.StatusCode = %d, want %d", se.StatusCode, tt.wantStatusCode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Retry logic
// ---------------------------------------------------------------------------

func TestRetryOnServerError(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n <= 2 {
			// First two attempts: 503 Service Unavailable
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(errorBody(503, "service unavailable")))
			return
		}
		// Third attempt: success
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := client.New()
	c.BaseURL = srv.URL
	c.RetryMax = 3

	_, err := c.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestNoRetryOnClientError(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(errorBody(404, "not found")))
	}))
	defer srv.Close()

	c := client.New()
	c.BaseURL = srv.URL
	c.RetryMax = 3

	_, err := c.ListRepos(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("attempts = %d, want 1 (4xx is not retryable)", attempts)
	}
}

func TestRetryExhausted(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(errorBody(502, "bad gateway")))
	}))
	defer srv.Close()

	c := client.New()
	c.BaseURL = srv.URL
	c.RetryMax = 2

	_, err := c.ListRepos(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "retries exhausted") {
		t.Errorf("error = %q, want it to contain 'retries exhausted'", err)
	}
	if atomic.LoadInt32(&attempts) != 3 { // 1 initial + 2 retries
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

// ---------------------------------------------------------------------------
// Repository CRUD
// ---------------------------------------------------------------------------

func TestRepoCRUD(t *testing.T) {
	store := make(map[string]client.RepoInfo)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/binflow/api/repositories":
			// List repos
			var list []client.RepoInfo
			for _, v := range store {
				list = append(list, v)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(list)

		case r.Method == http.MethodPut && strings.HasPrefix(path, "/binflow/api/repositories/"):
			// Create repo
			key := strings.TrimPrefix(path, "/binflow/api/repositories/")
			var req client.RepoCreateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			info := client.RepoInfo{
				Key:         key,
				Rclass:      req.Rclass,
				PackageType: req.PackageType,
				Description: req.Description,
				URL:         req.URL,
				Members:     req.Members,
				QuotaBytes:  req.QuotaBytes,
				Includes:    req.Includes,
				Excludes:    req.Excludes,
			}
			store[key] = info
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(info)

		case r.Method == http.MethodGet && strings.HasPrefix(path, "/binflow/api/repositories/"):
			key := strings.TrimPrefix(path, "/binflow/api/repositories/")
			info, ok := store[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(errorBody(404, "Failed to find the repository '"+key+"' specified in the request.")))
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(info)

		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/binflow/api/repositories/"):
			key := strings.TrimPrefix(path, "/binflow/api/repositories/")
			delete(store, key)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))

		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(errorBody(404, "not implemented")))
		}
	}))
	defer srv.Close()

	c := client.New()
	c.BaseURL = srv.URL
	c.RetryMax = 0
	ctx := context.Background()

	t.Run("create", func(t *testing.T) {
		repo, err := c.CreateRepo(ctx, client.RepoCreateRequest{
			Key:         "my-repo",
			Rclass:      "local",
			PackageType: "generic",
			Description: "test repo",
		})
		if err != nil {
			t.Fatalf("CreateRepo: %v", err)
		}
		if repo.Key != "my-repo" {
			t.Errorf("key = %q, want %q", repo.Key, "my-repo")
		}
		if repo.Rclass != "local" {
			t.Errorf("rclass = %q, want %q", repo.Rclass, "local")
		}
	})

	t.Run("get", func(t *testing.T) {
		repo, err := c.GetRepo(ctx, "my-repo")
		if err != nil {
			t.Fatalf("GetRepo: %v", err)
		}
		if repo.Key != "my-repo" {
			t.Errorf("key = %q, want %q", repo.Key, "my-repo")
		}
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := c.GetRepo(ctx, "nonexistent")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "404") {
			t.Errorf("error = %q, want 404", err)
		}
	})

	t.Run("list", func(t *testing.T) {
		// Create another repo
		_, err := c.CreateRepo(ctx, client.RepoCreateRequest{
			Key:         "another-repo",
			Rclass:      "local",
			PackageType: "docker",
		})
		if err != nil {
			t.Fatalf("CreateRepo: %v", err)
		}

		repos, err := c.ListRepos(ctx)
		if err != nil {
			t.Fatalf("ListRepos: %v", err)
		}
		if len(repos) != 2 {
			t.Errorf("repo count = %d, want 2", len(repos))
		}
	})

	t.Run("delete", func(t *testing.T) {
		if err := c.DeleteRepo(ctx, "my-repo"); err != nil {
			t.Fatalf("DeleteRepo: %v", err)
		}
		// Verify deleted
		_, err := c.GetRepo(ctx, "my-repo")
		if err == nil {
			t.Fatal("expected 404 after delete, got nil")
		}
	})
}

// ---------------------------------------------------------------------------
// Artifact operations
// ---------------------------------------------------------------------------

func TestArtifactOperations(t *testing.T) {
	content := []byte("hello world")
	artifacts := make(map[string][]byte)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		// Storage API check BEFORE the content plane (/binflow/ can also
		// match /binflow/api/storage/ on the prefix).
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/binflow/api/storage/"):
			// Artifact info — return a fixed response
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(client.ArtifactInfo{
				URI:    path,
				Size:   "11",
				Folder: false,
				Sha256: "abc123",
			})

		case r.Method == http.MethodPut && strings.HasPrefix(path, "/binflow/"):
			// Upload artifact
			body, _ := io.ReadAll(r.Body)
			artifacts[path] = body
			w.WriteHeader(http.StatusCreated)

		case r.Method == http.MethodGet && strings.HasPrefix(path, "/binflow/"):
			// Download artifact
			data, ok := artifacts[path]
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(errorBody(404, "file not found")))
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)

		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/binflow/"):
			delete(artifacts, path)
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := client.New()
	c.BaseURL = srv.URL
	c.RetryMax = 0
	ctx := context.Background()

	t.Run("upload", func(t *testing.T) {
		err := c.UploadArtifact(ctx, "my-repo", "path/to/file.txt", bytes.NewReader(content), int64(len(content)), "text/plain")
		if err != nil {
			t.Fatalf("UploadArtifact: %v", err)
		}
	})

	t.Run("download", func(t *testing.T) {
		rc, err := c.DownloadArtifact(ctx, "my-repo", "path/to/file.txt")
		if err != nil {
			t.Fatalf("DownloadArtifact: %v", err)
		}
		defer func() { _ = rc.Close() }()
		got, _ := io.ReadAll(rc)
		if !bytes.Equal(got, content) {
			t.Errorf("downloaded = %q, want %q", got, content)
		}
	})

	t.Run("download not found", func(t *testing.T) {
		_, err := c.DownloadArtifact(ctx, "my-repo", "path/to/nonexistent.txt")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("get info", func(t *testing.T) {
		info, err := c.GetArtifactInfo(ctx, "my-repo", "path/to/file.txt")
		if err != nil {
			t.Fatalf("GetArtifactInfo: %v", err)
		}
		if info.Sha256 != "abc123" {
			t.Errorf("sha256 = %q, want %q", info.Sha256, "abc123")
		}
	})

	t.Run("list artifacts", func(t *testing.T) {
		// Override server for this subtest to return a list
		listSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/binflow/api/storage/") && r.URL.Query().Has("list") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(client.ArtifactListResponse{
					URI:     r.URL.Path,
					Created: "2024-01-01T00:00:00Z",
					Files: []client.ArtifactListEntry{
						{URI: "/file1.txt", Folder: false, Size: "100"},
						{URI: "/subdir", Folder: true},
					},
				})
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer listSrv.Close()

		lc := client.New()
		lc.BaseURL = listSrv.URL
		lc.RetryMax = -1 // no retries

		got, err := lc.ListArtifacts(context.Background(), "my-repo", "")
		if err != nil {
			t.Fatalf("ListArtifacts: %v", err)
		}
		if len(got.Files) != 2 {
			t.Errorf("file count = %d, want 2", len(got.Files))
		}
	})

	t.Run("delete", func(t *testing.T) {
		err := c.DeleteArtifact(ctx, "my-repo", "path/to/file.txt")
		if err != nil {
			t.Fatalf("DeleteArtifact: %v", err)
		}
		// Verify deleted
		_, err = c.DownloadArtifact(ctx, "my-repo", "path/to/file.txt")
		if err == nil {
			t.Fatal("expected error after delete, got nil")
		}
	})
}

// ---------------------------------------------------------------------------
// User CRUD
// ---------------------------------------------------------------------------

func TestUserCRUD(t *testing.T) {
	users := make(map[string]client.UserInfo)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/binflow/api/security/users":
			var list []client.UserInfo
			for _, v := range users {
				list = append(list, v)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(list)

		case r.Method == http.MethodPut && strings.HasPrefix(path, "/binflow/api/security/users/"):
			name := strings.TrimPrefix(path, "/binflow/api/security/users/")
			var req client.UserCreateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			info := client.UserInfo{
				Name:   name,
				Email:  req.Email,
				Admin:  req.Admin,
				Groups: req.Groups,
				Realm:  "local",
			}
			users[name] = info
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(info)

		case r.Method == http.MethodGet && strings.HasPrefix(path, "/binflow/api/security/users/"):
			name := strings.TrimPrefix(path, "/binflow/api/security/users/")
			info, ok := users[name]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(errorBody(404, "user not found")))
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(info)

		case r.Method == http.MethodPut && path == "/binflow/api/security/password":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))

		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(errorBody(404, "not implemented")))
		}
	}))
	defer srv.Close()

	c := client.New()
	c.BaseURL = srv.URL
	c.RetryMax = 0
	ctx := context.Background()

	t.Run("create", func(t *testing.T) {
		u, err := c.CreateUser(ctx, client.UserCreateRequest{
			Name:     "alice",
			Password: "secret",
			Email:    "alice@example.com",
			Admin:    true,
		})
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		if u.Name != "alice" {
			t.Errorf("name = %q, want %q", u.Name, "alice")
		}
		if !u.Admin {
			t.Error("admin = false, want true")
		}
	})

	t.Run("get", func(t *testing.T) {
		u, err := c.GetUser(ctx, "alice")
		if err != nil {
			t.Fatalf("GetUser: %v", err)
		}
		if u.Name != "alice" {
			t.Errorf("name = %q, want %q", u.Name, "alice")
		}
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := c.GetUser(ctx, "bob")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("list", func(t *testing.T) {
		// Create another user
		_, _ = c.CreateUser(ctx, client.UserCreateRequest{
			Name:     "bob",
			Password: "secret2",
			Admin:    false,
		})

		users, err := c.ListUsers(ctx)
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) != 2 {
			t.Errorf("user count = %d, want 2", len(users))
		}
	})

	t.Run("change password", func(t *testing.T) {
		err := c.ChangeSelfPassword(ctx, "old", "new")
		if err != nil {
			t.Fatalf("ChangeSelfPassword: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// Token CRUD
// ---------------------------------------------------------------------------

func TestTokenCRUD(t *testing.T) {
	tokens := make(map[string]client.TokenCreateResponse)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodPost && path == "/binflow/api/security/token":
			if r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
				// Form-encoded token creation
				_ = r.ParseForm()
				username := r.Form.Get("username")
				token := client.TokenCreateResponse{
					TokenID:  fmt.Sprintf("tok-%s", username),
					Token:    fmt.Sprintf("secret-%s", username),
					Username: username,
					Scope:    r.Form.Get("scope"),
				}
				tokens[username] = token
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(token)
				return
			}
			// JSON token creation
			var req client.TokenCreateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			token := client.TokenCreateResponse{
				TokenID:  fmt.Sprintf("tok-%s", req.Username),
				Token:    fmt.Sprintf("secret-%s", req.Username),
				Username: req.Username,
				Scope:    req.Scope,
			}
			tokens[req.Username] = token
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(token)

		case r.Method == http.MethodPost && path == "/binflow/api/security/token/revoke":
			// Revoke is idempotent — always returns 200
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))

		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(errorBody(404, "not implemented")))
		}
	}))
	defer srv.Close()

	c := client.New()
	c.BaseURL = srv.URL
	c.RetryMax = 0
	ctx := context.Background()

	t.Run("create json", func(t *testing.T) {
		tok, err := c.CreateToken(ctx, client.TokenCreateRequest{
			Username: "alice",
			Scope:    "api:*",
		})
		if err != nil {
			t.Fatalf("CreateToken: %v", err)
		}
		if tok.Username != "alice" {
			t.Errorf("username = %q, want %q", tok.Username, "alice")
		}
		if tok.Token == "" {
			t.Error("token is empty")
		}
	})

	t.Run("create form", func(t *testing.T) {
		tok, err := c.CreateTokenForm(ctx, client.TokenCreateRequest{
			Username:    "bob",
			Scope:       "api:*",
			ExpiresIn:   3600,
			Description: "my token",
		})
		if err != nil {
			t.Fatalf("CreateTokenForm: %v", err)
		}
		if tok.Username != "bob" {
			t.Errorf("username = %q, want %q", tok.Username, "bob")
		}
	})

	t.Run("revoke by token id", func(t *testing.T) {
		err := c.RevokeToken(ctx, client.TokenRevokeRequest{
			TokenID: "tok-alice",
		})
		if err != nil {
			t.Fatalf("RevokeToken: %v", err)
		}
	})

	t.Run("revoke by token value", func(t *testing.T) {
		err := c.RevokeToken(ctx, client.TokenRevokeRequest{
			Token: "secret-bob",
		})
		if err != nil {
			t.Fatalf("RevokeToken: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// Progress callback
// ---------------------------------------------------------------------------

func TestProgressCallback(t *testing.T) {
	var progressCalls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, "received %d bytes", len(body))
	}))
	defer srv.Close()

	c := client.New()
	c.BaseURL = srv.URL
	c.RetryMax = 0
	c.Progress = func(total, sent int64) {
		progressCalls = append(progressCalls, fmt.Sprintf("%d/%d", sent, total))
	}

	ctx := context.Background()
	data := []byte(strings.Repeat("x", 100))
	err := c.UploadArtifact(ctx, "repo", "file.dat", bytes.NewReader(data), int64(len(data)), "")
	if err != nil {
		t.Fatalf("UploadArtifact: %v", err)
	}

	if len(progressCalls) == 0 {
		t.Error("progress callback was not invoked")
	}
	t.Logf("progress calls: %v", progressCalls)
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

func TestContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := client.New()
	c.BaseURL = srv.URL
	c.RetryMax = 0

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.ListRepos(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), "canceled") {
		t.Errorf("error = %q, want context deadline exceeded", err)
	}
}

// ---------------------------------------------------------------------------
// Round-trip: retry with eventual success
// ---------------------------------------------------------------------------

func TestRetryEventualSuccess(t *testing.T) {
	var attempts int32
	var goodResponse atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			// First attempt: 503
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(errorBody(503, "overloaded")))
			return
		}
		// Subsequent attempts: success
		goodResponse.Store(true)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := client.New()
	c.BaseURL = srv.URL
	c.RetryMax = 2

	_, err := c.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if !goodResponse.Load() {
		t.Error("did not receive a good response")
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}
