package generic_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// BIN-78/T-596 + BIN-94/T-612: the DELETE-miss wording family on the
// generic wire. Both the REMOTE and the LOCAL face speak the deletion
// engine's miss wording verbatim (live A 7.161.26 evidence: T-596's probe
// and contrast leg, T-612's maven/pypi/generic double-round): 404 "Artifact
// deletion error: Item <repo>/<path> does not exist" — no trailing period,
// trailing slash kept verbatim on folder spellings — rendered through the
// service layer's deleteMissError StatusError so the adapter stays
// class-agnostic. The VIRTUAL miss keeps the adapter's Could-not-locate
// wording: that face is undecided (no A probe; deleteVirtualOwnStorage
// still returns the plain sentinel).

func TestRemoteDeleteMissWording(t *testing.T) {
	ctx := context.Background()
	st, err := storage.OpenEngine(t.TempDir(), storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	defer st.Close() //nolint:errcheck // teardown of the harness engine
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer md.Close() //nolint:errcheck // teardown of the harness store
	svc := repo.New(st, md, &allowAll{}, nil)

	// The upstream is never contacted (a DELETE-miss short of the engine's
	// invalidation); it exists only to make the remote row honest.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	t.Cleanup(upstream.Close)
	for _, r := range []*metadata.Repo{
		{RepoKey: "generic-remote", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
			Config: `{"url":"` + upstream.URL + `","allowPrivateUpstream":true}`},
		{RepoKey: "generic-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric},
	} {
		if _, err := svc.CreateRepo(ctx, admin(), r); err != nil {
			t.Fatalf("CreateRepo(%s): %v", r.RepoKey, err)
		}
	}

	h := genericHandler(t, svc, md)
	tests := []struct {
		name     string
		target   string
		wantCode int
		wantMsg  string
	}{
		{"remote miss: the deletion engine's wording, no trailing period",
			"/binflow/generic-remote/t596/never.txt", http.StatusNotFound,
			"Artifact deletion error: Item generic-remote/t596/never.txt does not exist"},
		{"local file miss: the same engine family (T-612)",
			"/binflow/generic-local/t596/never.txt", http.StatusNotFound,
			"Artifact deletion error: Item generic-local/t596/never.txt does not exist"},
		{"local folder miss: trailing slash kept verbatim",
			"/binflow/generic-local/t596/never-dir/", http.StatusNotFound,
			"Artifact deletion error: Item generic-local/t596/never-dir/ does not exist"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := h(t, http.MethodDelete, tc.target)
			if res.Code != tc.wantCode {
				t.Fatalf("DELETE %s = %d (body %s), want %d", tc.target, res.Code, res.Body.String(), tc.wantCode)
			}
			if !strings.Contains(res.Body.String(), `"message": "`+tc.wantMsg+`"`) {
				t.Fatalf("DELETE %s body = %s, want the message %q", tc.target, res.Body.String(), tc.wantMsg)
			}
		})
	}
}
