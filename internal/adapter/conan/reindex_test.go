package conan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The management face's legs: the two reindex spellings, the gate
// (authentication + CanManageRepo(write)), the local-only refusal and the
// rebuild itself (a dropped index row comes back).
//
// The handler is exercised through its own httptest mount with the
// principal boxed the way the router's chain boxes it — the dispatchAPI
// cases the assembly owns call this same handler.

// mountManagement builds the management face over a content stack.
func mountManagement(t *testing.T, s *stack) *httptest.Server {
	t.Helper()
	m := NewManagementHandler(s.svc, s.md.Repos(), s.auth)
	mux := http.NewServeMux()
	mux.Handle("/binflow/api/conan/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The principal seam the router's chain fills: Basic through the
		// real authenticator (an invalid credential answers the 401 the
		// chain itself owns).
		if user, pass, ok := r.BasicAuth(); ok {
			// Box a synthetic Basic header the way the router's chain
			// would present it to the authenticator.
			authReq := r.Clone(r.Context())
			authReq.Header = r.Header.Clone()
			authReq.SetBasicAuth(user, pass)
			if p, err := s.auth.Authenticate(r.Context(), authReq); err == nil {
				r = r.WithContext(adapter.WithPrincipal(r.Context(), p))
			}
		}
		m.ServeHTTP(w, r)
	}))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// doMgmt issues one management request with the admin credential.
func doMgmt(t *testing.T, ts *httptest.Server, method, path, body string, auth bool) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		req.SetBasicAuth(adminUser, adminPass)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	return resp.StatusCode, string(buf[:n])
}

// TestReindexGateMatrix: anonymous 401, unauthenticated-shape refusals and
// the local-only 400; the happy arms follow.
func TestReindexGateMatrix(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	s.seedRepo(t, "cn-remote", repo.TypeRemote)
	ts := mountManagement(t, s)

	if code, _ := doMgmt(t, ts, http.MethodPost, "/binflow/api/conan/reindex?repoKey=cn-local", "", false); code != http.StatusUnauthorized {
		t.Errorf("anonymous reindex = %d, want 401", code)
	}
	if code, _ := doMgmt(t, ts, http.MethodPost, "/binflow/api/conan/reindex", "", true); code != http.StatusBadRequest {
		t.Errorf("reindex without repoKey = %d, want 400", code)
	}
	if code, body := doMgmt(t, ts, http.MethodPost, "/binflow/api/conan/reindex?repoKey=no-such-repo", "", true); code != http.StatusNotFound || !strings.Contains(body, "no-such-repo") {
		t.Errorf("unknown repo reindex = (%d, %s), want the named 404", code, body)
	}
	if code, _ := doMgmt(t, ts, http.MethodPost, "/binflow/api/conan/reindex?repoKey=cn-remote", "", true); code != http.StatusBadRequest {
		t.Errorf("remote reindex = %d, want 400 (local-only)", code)
	}
	if code, _ := doMgmt(t, ts, http.MethodGet, "/binflow/api/conan/reindex?repoKey=cn-local", "", true); code != http.StatusMethodNotAllowed {
		t.Errorf("GET reindex = %d, want 405", code)
	}
}

// TestReindexRebuild: a hand-corrupted index (a dropped revision row)
// comes back through both spellings; the response carries the completion
// note and the capability family.
func TestReindexRebuild(t *testing.T) {
	s := newStack(t)
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	rev := fixtureRev(2)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	s.putRecipeFile("cn-local", r, rev, "conanfile.py", []byte("x"))
	pid := fixturePID(3)
	s.putPkgFile("cn-local", r, rev, pid, fixtureRev(5), "conan_package.tgz", []byte("t"))
	ts := mountManagement(t, s)

	// Corrupt: drop the revision row from the stored index.
	if err := s.handlerForTest().removeRecipeRevisionEntry(context.Background(), adminPrincipal(), "cn-local", r, rev); err != nil {
		t.Fatalf("corrupt index: %v", err)
	}
	if code, _, _ := s.get(v2("cn-local", "hello/1.0/myuser/stable/revisions")); code != http.StatusNotFound {
		t.Fatalf("post-corruption revisions = %d, want the empty-chain 404", code)
	}

	code, body := doMgmt(t, ts, http.MethodPost, "/binflow/api/conan/reindex?repoKey=cn-local", "", true)
	if code != http.StatusOK || !strings.Contains(body, "Calculated Conan index") {
		t.Fatalf("query-form reindex = (%d, %s), want 200 + the note", code, body)
	}

	code, wire, _ := s.get(v2("cn-local", "hello/1.0/myuser/stable/revisions"))
	if code != http.StatusOK {
		t.Fatalf("post-reindex revisions = %d", code)
	}
	var doc recipeIndexDoc
	if err := json.Unmarshal([]byte(wire), &doc); err != nil || len(doc.Revisions) != 1 || doc.Revisions[0].Revision != rev {
		t.Fatalf("post-reindex index = %s (%v), want the rebuilt row", wire, err)
	}

	// The path form over the same ground.
	code, body = doMgmt(t, ts, http.MethodPost, "/binflow/api/conan/cn-local/myuser/hello/reindex", "", true)
	if code != http.StatusOK {
		t.Fatalf("path-form reindex = (%d, %s)", code, body)
	}

	// The JSON-body shape of the query form.
	code, body = doMgmt(t, ts, http.MethodPost, "/binflow/api/conan/reindex", `{"repoKey":"cn-local"}`, true)
	if code != http.StatusOK || !strings.Contains(body, "'cn-local'") {
		t.Fatalf("body-form reindex = (%d, %s)", code, body)
	}
}
