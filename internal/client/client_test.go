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
	"sync"
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

// writeRealText copies the real server's plain-text emitter (repositories.go
// writePlainText): text/plain + nosniff. The repository/user mutation planes
// answer successes with it.
func writeRealText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// writeRealOAuthError copies the token plane's error body (security.go
// writeOAuthError): {"error","error_description"} JSON.
func writeRealOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": description})
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
		{
			// Real token-plane error shape (security.go writeOAuthError): the
			// client is not envelope-aware here and falls back to the raw body,
			// which still carries the description text.
			name:           "oauth error body falls back to raw",
			statusCode:     400,
			body:           `{"error":"invalid_request","error_description":"token or token_id is required"}`,
			wantStatusCode: 400,
			wantContains:   "token or token_id is required",
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
// Repository CRUD — fake answers with the REAL server behavior copied from
// internal/httpapi/repositories.go: mutations answer 200 plain text, reads
// answer JSON (repoConfig on GET {key}, repoListItem with "type" on list).
// ---------------------------------------------------------------------------

// fakeRepoStore backs the repository fake with the row subset the real
// handlers project: the typed columns plus the stored canonical config the
// GET body echoes under "configuration" (repoConfigOf).
type fakeRepoStore struct {
	rclass      string
	packageType string
	description string
	members     []string
	includes    string
	excludes    string
}

// realRepoPutBody mirrors the REQUEST struct internal/httpapi decodes
// (repositories.go repoConfig): the wire spellings, never the client's Go
// field names. The fake asserts against these tags — a client regression back
// to the pre-T-191 spellings (members/includes/excludes) decodes as zero
// values here and fails the assertions.
type realRepoPutBody struct {
	Key             string   `json:"key"`
	Rclass          string   `json:"rclass"`
	PackageType     string   `json:"packageType"`
	Description     string   `json:"description"`
	Repositories    []string `json:"repositories"`
	IncludesPattern string   `json:"includesPattern"`
	ExcludesPattern string   `json:"excludesPattern"`
}

// fakeConfigOf renders the stored canonical config the way the real GET does
// (repoConfigOf): nested under "configuration", present only when non-empty.
func fakeConfigOf(row fakeRepoStore) map[string]any {
	m := map[string]any{}
	if len(row.members) > 0 {
		m["repositories"] = row.members
	}
	if row.includes != "" {
		m["includesPattern"] = row.includes
	}
	if row.excludes != "" {
		m["excludesPattern"] = row.excludes
	}
	return m
}

func TestRepoCRUD(t *testing.T) {
	store := make(map[string]fakeRepoStore)
	var putBodies []string // raw mutation bodies, for wire-spelling assertions
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/binflow/api/repositories":
			// GET list: repoListItem shape — "type", not "rclass".
			items := []map[string]any{}
			for key, row := range store {
				items = append(items, map[string]any{
					"key":         key,
					"description": row.description,
					"type":        row.rclass,
					"packageType": row.packageType,
					"url":         "http://" + r.Host + "/" + key,
				})
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(items)

		case (r.Method == http.MethodPut || r.Method == http.MethodPost) && strings.HasPrefix(path, "/binflow/api/repositories/"):
			// PUT create-or-update (handleRepoPut) / POST update spelling
			// (handleRepoPost). The body is decoded with the REAL request
			// struct's tags (T-191 AC 2): repositories / includesPattern /
			// excludesPattern.
			key := strings.TrimPrefix(path, "/binflow/api/repositories/")
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			putBodies = append(putBodies, string(raw))
			mu.Unlock()
			var body realRepoPutBody
			if err := json.Unmarshal(raw, &body); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, errorBody(400, "request body is not valid repository configuration JSON: "+err.Error()))
				return
			}
			if body.Key != "" && body.Key != key {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, errorBody(400, fmt.Sprintf(
					"repository key in body %q does not match the request path %q", body.Key, key)))
				return
			}
			if r.Method == http.MethodPost {
				if _, ok := store[key]; !ok {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNotFound)
					_, _ = io.WriteString(w, errorBody(404, "Repository does not exist: "+key))
					return
				}
			}
			if body.Rclass == "" {
				if row, ok := store[key]; ok {
					body.Rclass = row.rclass // update keeps the stored class
				} else {
					body.Rclass = "local"
				}
			}
			if body.PackageType == "" {
				if row, ok := store[key]; ok {
					body.PackageType = row.packageType // handleRepoPost fallback
				}
			}
			_, exists := store[key]
			store[key] = fakeRepoStore{
				rclass: body.Rclass, packageType: body.PackageType, description: body.Description,
				members: body.Repositories, includes: body.IncludesPattern, excludes: body.ExcludesPattern,
			}
			if exists || r.Method == http.MethodPost {
				writeRealText(w, http.StatusOK, fmt.Sprintf("Repository %s update successfully.\n", key))
				return
			}
			writeRealText(w, http.StatusOK, fmt.Sprintf("Successfully created repository '%s'\n", key))

		case r.Method == http.MethodGet && strings.HasPrefix(path, "/binflow/api/repositories/"):
			// GET single (handleRepoGet): repoConfig shape — "rclass" plus the
			// canonical config nested under "configuration".
			key := strings.TrimPrefix(path, "/binflow/api/repositories/")
			row, ok := store[key]
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, errorBody(404, "Repository does not exist: "+key))
				return
			}
			body := map[string]any{
				"key":         key,
				"rclass":      row.rclass,
				"packageType": row.packageType,
				"description": row.description,
				"url":         "http://" + r.Host + "/" + key,
			}
			if cfg := fakeConfigOf(row); len(cfg) > 0 {
				body["configuration"] = cfg
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(body)

		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/binflow/api/repositories/"):
			key := strings.TrimPrefix(path, "/binflow/api/repositories/")
			if _, ok := store[key]; !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, errorBody(404, "Repository does not exist: "+key))
				return
			}
			delete(store, key)
			writeRealText(w, http.StatusOK, fmt.Sprintf("Repository %s deleted successfully.\n", key))

		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, errorBody(404, "not implemented"))
		}
	}))
	defer srv.Close()

	c := client.New()
	c.BaseURL = srv.URL
	c.RetryMax = 0
	ctx := context.Background()

	t.Run("create answers 200 plain text", func(t *testing.T) {
		repo, err := c.CreateRepo(ctx, client.RepoCreateRequest{
			Key:         "my-repo",
			Rclass:      "local",
			PackageType: "generic",
			Description: "test repo",
		})
		if err != nil {
			t.Fatalf("CreateRepo: %v", err)
		}
		// The real body carries no fields; the client echoes the request.
		if repo.Key != "my-repo" {
			t.Errorf("key = %q, want %q", repo.Key, "my-repo")
		}
		if repo.Rclass != "local" {
			t.Errorf("rclass = %q, want %q (request echo)", repo.Rclass, "local")
		}
		if repo.PackageType != "generic" {
			t.Errorf("packageType = %q, want %q (request echo)", repo.PackageType, "generic")
		}
	})

	t.Run("put on existing key is the update spelling", func(t *testing.T) {
		if _, err := c.CreateRepo(ctx, client.RepoCreateRequest{
			Key: "my-repo", Rclass: "local", PackageType: "generic", Description: "test repo",
		}); err != nil {
			t.Fatalf("CreateRepo on existing key: %v", err)
		}
	})

	t.Run("get decodes the repoConfig body", func(t *testing.T) {
		repo, err := c.GetRepo(ctx, "my-repo")
		if err != nil {
			t.Fatalf("GetRepo: %v", err)
		}
		if repo.Key != "my-repo" || repo.Rclass != "local" || repo.PackageType != "generic" {
			t.Errorf("get = %+v, want key/rclass/packageType decoded from the body", repo)
		}
		if repo.Description != "test repo" {
			t.Errorf("description = %q, want %q", repo.Description, "test repo")
		}
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := c.GetRepo(ctx, "nonexistent")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var se *client.StatusError
		if !errors.As(err, &se) || se.StatusCode != 404 {
			t.Errorf("error = %v, want 404 StatusError", err)
		}
		if !strings.Contains(err.Error(), "Repository does not exist") {
			t.Errorf("error = %q, want the real 404 wording", err)
		}
	})

	t.Run("update via post", func(t *testing.T) {
		if _, err := c.UpdateRepo(ctx, "my-repo", client.RepoCreateRequest{
			PackageType: "docker",
		}); err != nil {
			t.Fatalf("UpdateRepo: %v", err)
		}
		repo, err := c.GetRepo(ctx, "my-repo")
		if err != nil {
			t.Fatalf("GetRepo: %v", err)
		}
		if repo.PackageType != "docker" {
			t.Errorf("packageType = %q, want %q after update", repo.PackageType, "docker")
		}
	})

	t.Run("update unknown key is 404", func(t *testing.T) {
		_, err := c.UpdateRepo(ctx, "nonexistent", client.RepoCreateRequest{PackageType: "generic"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var se *client.StatusError
		if !errors.As(err, &se) || se.StatusCode != 404 {
			t.Errorf("error = %v, want 404 StatusError", err)
		}
	})

	t.Run("list decodes type into rclass", func(t *testing.T) {
		if _, err := c.CreateRepo(ctx, client.RepoCreateRequest{
			Key: "another-repo", Rclass: "local", PackageType: "docker",
		}); err != nil {
			t.Fatalf("CreateRepo: %v", err)
		}
		repos, err := c.ListRepos(ctx)
		if err != nil {
			t.Fatalf("ListRepos: %v", err)
		}
		if len(repos) != 2 {
			t.Fatalf("repo count = %d, want 2", len(repos))
		}
		for _, repo := range repos {
			if repo.Rclass != "local" {
				t.Errorf("list item %+v: rclass = %q, want %q decoded from the wire's \"type\"", repo, repo.Rclass, "local")
			}
		}
	})

	t.Run("create virtual encodes the server member spelling", func(t *testing.T) {
		if _, err := c.CreateRepo(ctx, client.RepoCreateRequest{
			Key:         "agg-repo",
			Rclass:      "virtual",
			PackageType: "generic",
			Members:     []string{"my-repo", "another-repo"},
		}); err != nil {
			t.Fatalf("CreateRepo virtual: %v", err)
		}
		mu.Lock()
		body := putBodies[len(putBodies)-1]
		mu.Unlock()
		var wire map[string]any
		if err := json.Unmarshal([]byte(body), &wire); err != nil {
			t.Fatalf("decode the sent body %q: %v", body, err)
		}
		members, ok := wire["repositories"].([]any)
		if !ok || len(members) != 2 || members[0] != "my-repo" || members[1] != "another-repo" {
			t.Errorf("wire \"repositories\" = %v, want the member keys under the server spelling (fake decodes the httpapi tags)", wire["repositories"])
		}
		if _, ok := wire["members"]; ok {
			t.Errorf("wire body %q still carries the pre-alignment \"members\" spelling", body)
		}
		// Read back: the GET body nests the canonical config under
		// "configuration"; the client folds it into the flat fields.
		got, err := c.GetRepo(ctx, "agg-repo")
		if err != nil {
			t.Fatalf("GetRepo virtual: %v", err)
		}
		if len(got.Members) != 2 || got.Members[0] != "my-repo" || got.Members[1] != "another-repo" {
			t.Errorf("GetRepo members = %v, want the configuration echo folded", got.Members)
		}
	})

	t.Run("local patterns ride includesPattern/excludesPattern", func(t *testing.T) {
		if _, err := c.CreateRepo(ctx, client.RepoCreateRequest{
			Key:         "pattern-repo",
			Rclass:      "local",
			PackageType: "generic",
			Includes:    "**/*.jar",
			Excludes:    "*.tmp",
		}); err != nil {
			t.Fatalf("CreateRepo with patterns: %v", err)
		}
		mu.Lock()
		body := putBodies[len(putBodies)-1]
		mu.Unlock()
		var wire map[string]any
		if err := json.Unmarshal([]byte(body), &wire); err != nil {
			t.Fatalf("decode the sent body %q: %v", body, err)
		}
		if wire["includesPattern"] != "**/*.jar" || wire["excludesPattern"] != "*.tmp" {
			t.Errorf("wire patterns = %v / %v, want the server spellings includesPattern/excludesPattern",
				wire["includesPattern"], wire["excludesPattern"])
		}
		for _, legacy := range []string{"includes", "excludes"} {
			if _, ok := wire[legacy]; ok {
				t.Errorf("wire body %q still carries the pre-alignment %q spelling", body, legacy)
			}
		}
		got, err := c.GetRepo(ctx, "pattern-repo")
		if err != nil {
			t.Fatalf("GetRepo: %v", err)
		}
		if got.Includes != "**/*.jar" || got.Excludes != "*.tmp" {
			t.Errorf("GetRepo patterns = %q / %q, want the configuration echo folded", got.Includes, got.Excludes)
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

// TestRepoMutationJSONCompatibility pins the documented compatibility arm:
// a peer still answering the pre-alignment JSON body decodes over the
// request echo. The real server never sends JSON here (see TestRepoCRUD).
func TestRepoMutationJSONCompatibility(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"key":"legacy","rclass":"remote","packageType":"maven","url":"http://upstream/maven"}`)
	}))
	defer srv.Close()

	c := client.New()
	c.BaseURL = srv.URL
	c.RetryMax = -1

	repo, err := c.CreateRepo(context.Background(), client.RepoCreateRequest{
		Key: "legacy", Rclass: "remote", PackageType: "generic",
	})
	if err != nil {
		t.Fatalf("CreateRepo against JSON-answering peer: %v", err)
	}
	if repo.Rclass != "remote" || repo.URL != "http://upstream/maven" {
		t.Errorf("repo = %+v, want the JSON body to win over the request echo", repo)
	}
}

// ---------------------------------------------------------------------------
// Artifact operations — fake answers with the REAL server behavior copied
// from internal/httpapi/storage.go and internal/adapter/generic: item info
// nests checksums and sizes its size as a string; the ?list body carries
// int64 sizes; uploads answer 201; deletes answer 204.
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
			if r.URL.Query().Has("list") {
				// ?list body (storage.go listResponse/listFile): int64 sizes.
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, `{
  "uri": "http://`+r.Host+`/binflow/api/storage/my-repo/path",
  "created": "2026-08-22T10:00:00.000Z",
  "files": [
    {"uri": "to/file.txt", "size": 11, "lastModified": "2026-08-22T10:00:00.000Z", "folder": false, "sha1": "aa33f1f10e0e18b8", "sha2": "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"},
    {"uri": "subdir", "folder": true}
  ]
}`)
				return
			}
			// Item info (storage.go fileInfoBody): string size, nested checksums.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{
  "uri": "http://`+r.Host+`/binflow/api/storage/my-repo/path/to/file.txt",
  "downloadUri": "http://`+r.Host+`/my-repo/path/to/file.txt",
  "repo": "my-repo",
  "path": "/path/to/file.txt",
  "created": "2026-08-22T10:00:00.000Z",
  "createdBy": "admin",
  "lastModified": "2026-08-22T10:00:00.000Z",
  "modifiedBy": "admin",
  "lastUpdated": "2026-08-22T10:00:00.000Z",
  "size": "11",
  "mimeType": "text/plain",
  "checksums": {
    "sha1": "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed",
    "md5": "5eb63bbbe01eeed093cb22bb8f5acdc3",
    "sha256": "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
  },
  "originalChecksums": {
    "sha1": "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed",
    "md5": "5eb63bbbe01eeed093cb22bb8f5acdc3",
    "sha256": "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
  }
}`)
		case r.Method == http.MethodPut && strings.HasPrefix(path, "/binflow/"):
			// Upload: 201 + ItemCreated body (the client ignores the body).
			body, _ := io.ReadAll(r.Body)
			artifacts[path] = body
			w.Header().Set("Location", "http://"+r.Host+"/my-repo/path/to/file.txt")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"uri":"http://`+r.Host+`/my-repo/path/to/file.txt","downloadUri":"http://`+r.Host+`/my-repo/path/to/file.txt","repo":"my-repo","path":"/path/to/file.txt","created":"2026-08-22T10:00:00.000Z","createdBy":"admin","size":"11","mimeType":"text/plain"}`)

		case r.Method == http.MethodGet && strings.HasPrefix(path, "/binflow/"):
			// Download artifact
			data, ok := artifacts[path]
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, errorBody(404, "file not found"))
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)

		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/binflow/"):
			delete(artifacts, path)
			w.WriteHeader(http.StatusNoContent)

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

	t.Run("get info folds nested checksums", func(t *testing.T) {
		info, err := c.GetArtifactInfo(ctx, "my-repo", "path/to/file.txt")
		if err != nil {
			t.Fatalf("GetArtifactInfo: %v", err)
		}
		if info.Size != "11" {
			t.Errorf("size = %q, want %q (string on the item-info body)", info.Size, "11")
		}
		if info.Sha256 != "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9" {
			t.Errorf("sha256 = %q, want it folded from the nested checksums object", info.Sha256)
		}
		if info.Sha1 != "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed" {
			t.Errorf("sha1 = %q, want it folded from the nested checksums object", info.Sha1)
		}
		if info.MimeType != "text/plain" {
			t.Errorf("mimeType = %q, want %q", info.MimeType, "text/plain")
		}
	})

	t.Run("list artifacts decodes int64 sizes", func(t *testing.T) {
		got, err := c.ListArtifacts(ctx, "my-repo", "path")
		if err != nil {
			t.Fatalf("ListArtifacts: %v", err)
		}
		if len(got.Files) != 2 {
			t.Fatalf("file count = %d, want 2", len(got.Files))
		}
		file := got.Files[0]
		if file.URI != "to/file.txt" || file.Folder || file.Size != 11 {
			t.Errorf("file entry = %+v, want uri to/file.txt, folder false, int64 size 11", file)
		}
		dir := got.Files[1]
		if dir.URI != "subdir" || !dir.Folder {
			t.Errorf("dir entry = %+v, want uri subdir, folder true", dir)
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
// T-231: artifact-path percent-encoding on the wire (the T-228 D-1 defect).
// A node name carrying '%', '#', '?', a space or non-ASCII must reach the
// server percent-escaped on EVERY verb, and the escaped spelling must
// address the same node the literal name denotes.
// ---------------------------------------------------------------------------

// escapingCall is one recorded request of the escaping-matrix fake: the
// ESCAPED wire path (what the request line carried, r.URL.EscapedPath())
// next to the DECODED path the server would route on (r.URL.Path).
type escapingCall struct {
	Method string
	Raw    string
	Path   string
	Query  string
}

// TestArtifactPathEscapingWireForm is the 5-character regression matrix
// against a recording fake. Storage is keyed by the DECODED request path,
// so a verb that escapes wrongly addresses an unknown node and fails —
// correctness is discriminated, not just asserted. Every recorded request's
// escaped spelling is additionally pinned exactly.
func TestArtifactPathEscapingWireForm(t *testing.T) {
	const repo = "esc-repo"
	tests := []struct {
		name     string
		nodePath string // literal node path handed to the client
		wantSegs string // expected escaped repo-relative wire spelling
	}{
		{name: "literal percent", nodePath: "sym'bols$/percent%.txt", wantSegs: "sym%27bols$/percent%25.txt"},
		{name: "fragment marker", nodePath: "frag#ment.txt", wantSegs: "frag%23ment.txt"},
		{name: "query marker", nodePath: "query?name.txt", wantSegs: "query%3Fname.txt"},
		{name: "space", nodePath: "with space.txt", wantSegs: "with%20space.txt"},
		{name: "utf-8 chinese nested", nodePath: "中文/文件 名.txt", wantSegs: "%E4%B8%AD%E6%96%87/%E6%96%87%E4%BB%B6%20%E5%90%8D.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := []byte("t231 payload " + tt.name)
			artifacts := make(map[string][]byte) // keyed by DECODED path
			var mu sync.Mutex
			var calls []escapingCall

			record := func(r *http.Request) escapingCall {
				return escapingCall{Method: r.Method, Raw: r.URL.EscapedPath(), Path: r.URL.Path, Query: r.URL.RawQuery}
			}
			wantCall := func(method string) escapingCall {
				var last escapingCall
				for _, c := range append([]escapingCall(nil), calls...) {
					if c.Method == method {
						last = c
					}
				}
				return last
			}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				c := record(r)
				calls = append(calls, c)
				mu.Unlock()

				switch {
				case strings.HasPrefix(r.URL.Path, "/binflow/api/storage/"):
					// The storage plane must address the same node as the
					// content plane: translate the /api/storage infix onto
					// the content-plane key. A bogus escaping answers 404
					// and fails the client call.
					key := strings.Replace(r.URL.Path, "/binflow/api/storage/", "/binflow/", 1)
					mu.Lock()
					_, ok := artifacts[key]
					mu.Unlock()
					if !ok && r.URL.Query().Get("list") == "" {
						w.WriteHeader(http.StatusNotFound)
						_, _ = io.WriteString(w, errorBody(404, "no such node"))
						return
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprintf(w, `{"uri":"http://%s%s","size":"%d","folder":false,"checksums":{"sha256":"x"}}`,
						r.Host, r.URL.Path, len(content))
				case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/binflow/"):
					b, _ := io.ReadAll(r.Body)
					mu.Lock()
					artifacts[r.URL.Path] = b
					mu.Unlock()
					w.WriteHeader(http.StatusCreated)
				case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/binflow/"):
					mu.Lock()
					b, ok := artifacts[r.URL.Path]
					mu.Unlock()
					if !ok {
						w.WriteHeader(http.StatusNotFound)
						_, _ = io.WriteString(w, errorBody(404, "file not found"))
						return
					}
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(b)
				case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/binflow/"):
					mu.Lock()
					delete(artifacts, r.URL.Path)
					mu.Unlock()
					w.WriteHeader(http.StatusNoContent)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()

			c := client.New()
			c.BaseURL = srv.URL
			c.RetryMax = -1 // fail fast: no 4xx retry masking
			ctx := context.Background()

			// Upload: before T-231 the '%' leg died in url.Parse with
			// `invalid URL escape`; '#'/'?' legs silently addressed the
			// wrong node (fragment/query split).
			if err := c.UploadArtifact(ctx, repo, tt.nodePath, bytes.NewReader(content), int64(len(content)), "text/plain"); err != nil {
				t.Fatalf("UploadArtifact(%q): %v", tt.nodePath, err)
			}
			if got := wantCall(http.MethodPut); got.Raw != "/binflow/"+repo+"/"+tt.wantSegs || got.Path != "/binflow/"+repo+"/"+tt.nodePath {
				t.Errorf("PUT wire = raw %q decoded %q, want raw %q decoded %q",
					got.Raw, got.Path, "/binflow/"+repo+"/"+tt.wantSegs, "/binflow/"+repo+"/"+tt.nodePath)
			}

			// Download must return the exact bytes of the same node.
			rc, err := c.DownloadArtifact(ctx, repo, tt.nodePath)
			if err != nil {
				t.Fatalf("DownloadArtifact(%q): %v", tt.nodePath, err)
			}
			down, readErr := io.ReadAll(rc)
			_ = rc.Close()
			if readErr != nil {
				t.Fatalf("read download: %v", readErr)
			}
			if !bytes.Equal(down, content) {
				t.Errorf("downloaded %q, want %q", down, content)
			}
			if got := wantCall(http.MethodGet); got.Raw != "/binflow/"+repo+"/"+tt.wantSegs {
				t.Errorf("GET wire = raw %q, want %q", got.Raw, "/binflow/"+repo+"/"+tt.wantSegs)
			}

			// Item info on the storage plane addresses the identical node.
			if _, err := c.GetArtifactInfo(ctx, repo, tt.nodePath); err != nil {
				t.Fatalf("GetArtifactInfo(%q): %v", tt.nodePath, err)
			}
			// The ?list spelling keeps the escaped path out of the query.
			if _, err := c.ListArtifacts(ctx, repo, tt.nodePath); err != nil {
				t.Fatalf("ListArtifacts(%q): %v", tt.nodePath, err)
			}
			if got := wantCall(http.MethodGet); got.Raw != "/binflow/api/storage/"+repo+"/"+tt.wantSegs || got.Query != "list" {
				t.Errorf("list wire = raw %q query %q, want raw %q query list",
					got.Raw, got.Query, "/binflow/api/storage/"+repo+"/"+tt.wantSegs)
			}

			// Delete, then the node must be gone.
			if err := c.DeleteArtifact(ctx, repo, tt.nodePath); err != nil {
				t.Fatalf("DeleteArtifact(%q): %v", tt.nodePath, err)
			}
			if _, err := c.DownloadArtifact(ctx, repo, tt.nodePath); err == nil {
				t.Fatal("download after delete must fail, got nil")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// User CRUD — fake answers with the REAL server behavior copied from
// internal/httpapi/security.go: create answers 201 with no body; reads
// answer userDetail/userListItem JSON; the user plane's errors are PLAIN
// TEXT; the change-password body is oldPassword/newPassword.
// ---------------------------------------------------------------------------

func TestUserCRUD(t *testing.T) {
	type fakeUser struct {
		email    string
		admin    bool
		password string
	}
	users := map[string]fakeUser{
		"admin": {email: "admin@example.com", admin: true, password: "admin-pass"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/binflow/api/security/users":
			// handleUserList: name/uri/realm entries.
			items := []map[string]any{}
			for name := range users {
				items = append(items, map[string]any{
					"name":  name,
					"uri":   "http://" + r.Host + "/binflow/api/security/users/" + name,
					"realm": "internal",
				})
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(items)

		case r.Method == http.MethodPut && strings.HasPrefix(path, "/binflow/api/security/users/"):
			// handleUserCreatePut: 201 with no body in both outcomes.
			name := strings.TrimPrefix(path, "/binflow/api/security/users/")
			var body struct {
				Name     string   `json:"name"`
				Email    string   `json:"email"`
				Password string   `json:"password"`
				Admin    bool     `json:"admin"`
				Groups   []string `json:"groups"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeRealText(w, http.StatusBadRequest, "request body is not valid JSON: "+err.Error())
				return
			}
			if strings.TrimSpace(body.Email) == "" {
				writeRealText(w, http.StatusBadRequest, "Please provide a valid user email.")
				return
			}
			if body.Password == "" {
				writeRealText(w, http.StatusBadRequest, "Please provide a valid user password.")
				return
			}
			users[name] = fakeUser{email: strings.TrimSpace(body.Email), admin: body.Admin, password: body.Password}
			w.WriteHeader(http.StatusCreated) // 201, no body

		case r.Method == http.MethodGet && strings.HasPrefix(path, "/binflow/api/security/users/"):
			// handleUserGet: userDetail shape.
			name := strings.TrimPrefix(path, "/binflow/api/security/users/")
			u, ok := users[name]
			if !ok {
				writeRealText(w, http.StatusNotFound, "User not found")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":                     name,
				"email":                    u.email,
				"admin":                    u.admin,
				"groups":                   []string{},
				"realm":                    "internal",
				"profileUpdatable":         true,
				"internalPasswordDisabled": false,
				"disableUIAccess":          false,
			})

		case r.Method == http.MethodPut && path == "/binflow/api/security/password":
			// handleChangePasswordOwn: oldPassword/newPassword body; plain-text
			// answers both ways.
			var body struct {
				OldPassword string `json:"oldPassword"`
				NewPassword string `json:"newPassword"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			admin := users["admin"]
			if body.OldPassword != admin.password {
				writeRealText(w, http.StatusBadRequest, "Incorrect username/password")
				return
			}
			admin.password = body.NewPassword
			users["admin"] = admin
			writeRealText(w, http.StatusOK, "Password has been successfully changed")

		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, errorBody(404, "not implemented"))
		}
	}))
	defer srv.Close()

	c := client.New()
	c.BaseURL = srv.URL
	c.RetryMax = 0
	ctx := context.Background()

	t.Run("create returns 201 empty body", func(t *testing.T) {
		if _, err := c.CreateUser(ctx, client.UserCreateRequest{
			Name:     "alice",
			Password: "secret",
			Email:    "alice@example.com",
			Admin:    true,
		}); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
	})

	t.Run("create without email is a plain-text 400", func(t *testing.T) {
		_, err := c.CreateUser(ctx, client.UserCreateRequest{
			Name: "bob", Password: "secret2",
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "Please provide a valid user email.") {
			t.Errorf("error = %q, want the real plain-text wording", err)
		}
	})

	t.Run("get decodes the userDetail body", func(t *testing.T) {
		u, err := c.GetUser(ctx, "alice")
		if err != nil {
			t.Fatalf("GetUser: %v", err)
		}
		if u.Name != "alice" || u.Email != "alice@example.com" || !u.Admin {
			t.Errorf("user = %+v, want name/email/admin decoded from the body", u)
		}
		if u.Realm != "internal" {
			t.Errorf("realm = %q, want %q", u.Realm, "internal")
		}
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := c.GetUser(ctx, "bob")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "User not found") {
			t.Errorf("error = %q, want the real plain-text wording", err)
		}
	})

	t.Run("list", func(t *testing.T) {
		users, err := c.ListUsers(ctx)
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) != 2 {
			t.Errorf("user count = %d, want 2", len(users))
		}
	})

	t.Run("change password with the wrong old password is rejected", func(t *testing.T) {
		err := c.ChangeSelfPassword(ctx, "wrong-old", "new-pass")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "Incorrect username/password") {
			t.Errorf("error = %q, want the real plain-text wording", err)
		}
	})

	t.Run("change password", func(t *testing.T) {
		if err := c.ChangeSelfPassword(ctx, "admin-pass", "new-pass"); err != nil {
			t.Fatalf("ChangeSelfPassword: %v", err)
		}
		// The rotated password is now the accepted old password.
		if err := c.ChangeSelfPassword(ctx, "new-pass", "admin-pass"); err != nil {
			t.Fatalf("ChangeSelfPassword with the rotated password: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// Token CRUD — fake answers with the REAL server behavior copied from
// internal/httpapi/security.go: the create body is the OAuth-style
// access_token/token_type/expires_in/scope + int64 token_id; revoke parses
// FORM parameters and answers plain text.
// ---------------------------------------------------------------------------

func TestTokenCRUD(t *testing.T) {
	var nextID int64 = 41

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		case r.Method == http.MethodPost && path == "/binflow/api/security/token":
			// handleTokenCreate: accept JSON and form bodies, answer the
			// calibrated create body (tokenCreateResponse).
			scope := ""
			var expiresIn int64
			ct := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
			if ct == "application/json" {
				var body struct {
					Username  string `json:"username"`
					Scope     string `json:"scope"`
					ExpiresIn int64  `json:"expires_in"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					writeRealOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
					return
				}
				scope, expiresIn = body.Scope, body.ExpiresIn
			} else {
				if err := r.ParseForm(); err != nil {
					writeRealOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
					return
				}
				scope = r.PostFormValue("scope")
			}
			if scope == "" {
				scope = "api:*"
			}
			nextID++
			resp := map[string]any{
				"access_token": fmt.Sprintf("issued-token-%d", nextID),
				"token_type":   "Bearer",
				"scope":        scope,
				"token_id":     nextID, // int64 on the wire
			}
			if expiresIn > 0 {
				resp["expires_in"] = expiresIn // omitted when 0 = never
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodPost && path == "/binflow/api/security/token/revoke":
			// handleTokenRevoke: FORM parameters, plain-text answers.
			if err := r.ParseForm(); err != nil {
				writeRealOAuthError(w, http.StatusBadRequest, "invalid_request", "malformed form body: "+err.Error())
				return
			}
			token := r.PostFormValue("token")
			tokenID := strings.TrimSpace(r.PostFormValue("token_id"))
			if token != "" && tokenID != "" {
				writeRealOAuthError(w, http.StatusBadRequest, "invalid_request", "token and token_id are mutually exclusive")
				return
			}
			if token == "" && tokenID == "" {
				writeRealOAuthError(w, http.StatusBadRequest, "invalid_request", "token or token_id is required")
				return
			}
			writeRealText(w, http.StatusOK, "Token revoked")

		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, errorBody(404, "not implemented"))
		}
	}))
	defer srv.Close()

	c := client.New()
	c.BaseURL = srv.URL
	c.RetryMax = 0
	ctx := context.Background()

	t.Run("create json decodes the real body", func(t *testing.T) {
		tok, err := c.CreateToken(ctx, client.TokenCreateRequest{
			Username:  "alice",
			Scope:     "api:*",
			ExpiresIn: 3600,
		})
		if err != nil {
			t.Fatalf("CreateToken: %v", err)
		}
		if tok.Token != "issued-token-42" {
			t.Errorf("token = %q, want the access_token value", tok.Token)
		}
		if tok.TokenID != "42" {
			t.Errorf("token_id = %q, want the int64 wire value rendered as decimal text", tok.TokenID)
		}
		if tok.TokenType != "Bearer" {
			t.Errorf("token_type = %q, want %q", tok.TokenType, "Bearer")
		}
		if tok.ExpiresIn != 3600 {
			t.Errorf("expires_in = %d, want 3600", tok.ExpiresIn)
		}
		if tok.Scope != "api:*" {
			t.Errorf("scope = %q, want %q", tok.Scope, "api:*")
		}
		if tok.Username != "" {
			t.Errorf("username = %q, want empty (the real body does not echo the subject)", tok.Username)
		}
	})

	t.Run("create form decodes the real body", func(t *testing.T) {
		tok, err := c.CreateTokenForm(ctx, client.TokenCreateRequest{
			Username:  "bob",
			Scope:     "api:*",
			ExpiresIn: 3600,
		})
		if err != nil {
			t.Fatalf("CreateTokenForm: %v", err)
		}
		if tok.Token == "" || tok.TokenID != "43" || tok.TokenType != "Bearer" {
			t.Errorf("token = %+v, want access_token/int64-token_id decoding on the form arm too", tok)
		}
	})

	t.Run("never-expiring token omits expires_in", func(t *testing.T) {
		tok, err := c.CreateTokenForm(ctx, client.TokenCreateRequest{Username: "bob"})
		if err != nil {
			t.Fatalf("CreateTokenForm: %v", err)
		}
		if tok.ExpiresIn != 0 {
			t.Errorf("expires_in = %d, want 0 when the wire omits the field", tok.ExpiresIn)
		}
	})

	t.Run("revoke sends form parameters", func(t *testing.T) {
		if err := c.RevokeToken(ctx, client.TokenRevokeRequest{TokenID: "42"}); err != nil {
			t.Fatalf("RevokeToken by id: %v", err)
		}
		if err := c.RevokeToken(ctx, client.TokenRevokeRequest{Token: "issued-token-43"}); err != nil {
			t.Fatalf("RevokeToken by value: %v", err)
		}
	})

	t.Run("revoke without either parameter is rejected", func(t *testing.T) {
		err := c.RevokeToken(ctx, client.TokenRevokeRequest{})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "token or token_id is required") {
			t.Errorf("error = %q, want the real OAuth error description", err)
		}
	})

	t.Run("legacy spelling still decodes", func(t *testing.T) {
		// Pre-alignment arm: kept so stale peers (cmd/bf's fake backend until
		// its refresh) keep decoding. Real shape is asserted above.
		var tok client.TokenCreateResponse
		if err := json.Unmarshal([]byte(`{"token":"legacy-value","token_id":"7","username":"alice","scope":"api:*","expires_in":3600}`), &tok); err != nil {
			t.Fatalf("unmarshal legacy body: %v", err)
		}
		if tok.Token != "legacy-value" || tok.TokenID != "7" || tok.Username != "alice" {
			t.Errorf("token = %+v, want the legacy fields populated", tok)
		}
	})
}

// ---------------------------------------------------------------------------
// Real-response snapshots: byte-for-byte bodies as the real emitters render
// them (writeJSONBody marshals with two-space indent), decoded into the
// client DTOs. These pin the wire contract independent of any fake.
// ---------------------------------------------------------------------------

func TestRealResponseSnapshots(t *testing.T) {
	t.Run("token create body", func(t *testing.T) {
		body := `{
  "access_token": "2fba5c9d0e7a44b1b3f4b2b6c1a4b0a7",
  "token_type": "Bearer",
  "expires_in": 3600,
  "scope": "api:*",
  "token_id": 42
}`
		var tok client.TokenCreateResponse
		if err := json.Unmarshal([]byte(body), &tok); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		want := client.TokenCreateResponse{
			Token: "2fba5c9d0e7a44b1b3f4b2b6c1a4b0a7", TokenID: "42",
			TokenType: "Bearer", ExpiresIn: 3600, Scope: "api:*",
		}
		if tok != want {
			t.Errorf("decoded = %+v, want %+v", tok, want)
		}
	})

	t.Run("token create body without expiry", func(t *testing.T) {
		body := `{
  "access_token": "abc",
  "token_type": "Bearer",
  "scope": "api:*",
  "token_id": 7
}`
		var tok client.TokenCreateResponse
		if err := json.Unmarshal([]byte(body), &tok); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if tok.ExpiresIn != 0 || tok.TokenID != "7" || tok.Token != "abc" {
			t.Errorf("decoded = %+v, want expires_in 0 (field omitted = never)", tok)
		}
	})

	t.Run("repo list body uses type", func(t *testing.T) {
		body := `[
  {
    "key": "libs-release",
    "description": "",
    "type": "local",
    "packageType": "generic",
    "url": "http://127.0.0.1:8080/libs-release"
  }
]`
		var repos []client.RepoInfo
		if err := json.Unmarshal([]byte(body), &repos); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(repos) != 1 || repos[0].Rclass != "local" || repos[0].Key != "libs-release" {
			t.Errorf("decoded = %+v, want type decoded into rclass", repos)
		}
	})

	t.Run("repo config body uses rclass", func(t *testing.T) {
		body := `{
  "key": "libs-release",
  "rclass": "local",
  "packageType": "generic",
  "description": "main releases",
  "url": "http://127.0.0.1:8080/libs-release"
}`
		var repo client.RepoInfo
		if err := json.Unmarshal([]byte(body), &repo); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if repo.Rclass != "local" || repo.PackageType != "generic" || repo.Description != "main releases" {
			t.Errorf("decoded = %+v, want the rclass spelling to win", repo)
		}
	})

	t.Run("repo config body nests the virtual members", func(t *testing.T) {
		// The real GET echoes the stored canonical config under
		// "configuration" (repoConfigOf + virtualConfig); the client folds
		// "repositories" into the flat Members field.
		body := `{
  "key": "libs-virtual",
  "rclass": "virtual",
  "packageType": "generic",
  "description": "aggregate",
  "url": "http://127.0.0.1:8080/libs-virtual",
  "configuration": {
    "repositories": [
      "libs-release",
      "maven-remote"
    ]
  }
}`
		var repo client.RepoInfo
		if err := json.Unmarshal([]byte(body), &repo); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(repo.Members) != 2 || repo.Members[0] != "libs-release" || repo.Members[1] != "maven-remote" {
			t.Errorf("decoded = %+v, want the configuration echo's repositories folded into Members", repo)
		}
	})

	t.Run("repo config body nests local patterns", func(t *testing.T) {
		body := `{
  "key": "libs-release",
  "rclass": "local",
  "packageType": "generic",
  "description": "",
  "url": "http://127.0.0.1:8080/libs-release",
  "configuration": {
    "excludesPattern": "internal/**",
    "includesPattern": "**/*"
  }
}`
		var repo client.RepoInfo
		if err := json.Unmarshal([]byte(body), &repo); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if repo.Includes != "**/*" || repo.Excludes != "internal/**" {
			t.Errorf("decoded = %+v, want the configuration echo's patterns folded into Includes/Excludes", repo)
		}
	})

	t.Run("repo create request encodes the server spellings", func(t *testing.T) {
		b, err := json.Marshal(client.RepoCreateRequest{
			Key: "libs-virtual", Rclass: "virtual", PackageType: "generic",
			Members: []string{"libs-release", "maven-remote"},
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		want := `{"key":"libs-virtual","rclass":"virtual","packageType":"generic","repositories":["libs-release","maven-remote"]}`
		if string(b) != want {
			t.Errorf("encoded = %s, want %s (byte-exact pin of the T-191 wire tags)", b, want)
		}
	})

	t.Run("user detail body", func(t *testing.T) {
		body := `{
  "name": "alice",
  "email": "alice@example.com",
  "admin": true,
  "groups": [],
  "realm": "internal",
  "profileUpdatable": true,
  "internalPasswordDisabled": false,
  "disableUIAccess": false
}`
		var u client.UserInfo
		if err := json.Unmarshal([]byte(body), &u); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if u.Name != "alice" || u.Email != "alice@example.com" || !u.Admin || u.Realm != "internal" {
			t.Errorf("decoded = %+v, want the userDetail fields", u)
		}
	})

	t.Run("file info body nests checksums", func(t *testing.T) {
		body := `{
  "uri": "http://127.0.0.1:8080/binflow/api/storage/libs-release/doc/hello.txt",
  "downloadUri": "http://127.0.0.1:8080/libs-release/doc/hello.txt",
  "repo": "libs-release",
  "path": "/doc/hello.txt",
  "created": "2026-08-22T10:00:00.000Z",
  "createdBy": "admin",
  "lastModified": "2026-08-22T10:00:00.000Z",
  "modifiedBy": "admin",
  "lastUpdated": "2026-08-22T10:00:00.000Z",
  "size": "11",
  "mimeType": "text/plain",
  "checksums": {
    "sha1": "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed",
    "md5": "5eb63bbbe01eeed093cb22bb8f5acdc3",
    "sha256": "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
  },
  "originalChecksums": {
    "sha1": "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed",
    "md5": "5eb63bbbe01eeed093cb22bb8f5acdc3",
    "sha256": "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
  }
}`
		var info client.ArtifactInfo
		if err := json.Unmarshal([]byte(body), &info); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if info.Size != "11" || info.MimeType != "text/plain" {
			t.Errorf("decoded = %+v, want string size and mimeType", info)
		}
		if info.Sha256 != "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9" ||
			info.Sha1 != "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed" ||
			info.MD5 != "5eb63bbbe01eeed093cb22bb8f5acdc3" {
			t.Errorf("decoded = %+v, want the nested checksums folded into the flat digests", info)
		}
	})

	t.Run("list body carries int64 sizes", func(t *testing.T) {
		body := `{
  "uri": "http://127.0.0.1:8080/binflow/api/storage/libs-release/doc",
  "created": "2026-08-22T10:00:00.000Z",
  "files": [
    {
      "uri": "hello.txt",
      "size": 11,
      "lastModified": "2026-08-22T10:00:00.000Z",
      "folder": false,
      "sha1": "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed",
      "sha2": "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
    },
    {
      "uri": "nested",
      "folder": true
    }
  ]
}`
		var list client.ArtifactListResponse
		if err := json.Unmarshal([]byte(body), &list); err != nil {
			t.Fatalf("unmarshal: %v (int64 size into a string field is exactly the T-166 class of bug)", err)
		}
		if len(list.Files) != 2 || list.Files[0].Size != 11 || list.Files[0].Folder {
			t.Errorf("decoded = %+v, want int64 size 11, folder false", list)
		}
		if !list.Files[1].Folder {
			t.Errorf("decoded = %+v, want the folder entry flagged", list)
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
