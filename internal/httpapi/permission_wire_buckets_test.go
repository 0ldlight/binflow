// T-491 wire legs (M17 W2, FR-156.2 / M16 B-2.16): the permission-target
// plane's acceptance of the three preset wildcard buckets — repos[] carries
// the literals, the echo round-trips them — and the content-plane coverage
// probes over real HTTP: an ANY LOCAL read grant opens a local repository
// (including one created after the grant) for the granted user while the
// ungranted user keeps the 403 and the read-only grant keeps being unable
// to write (AC1's 403/不可见 probe + AC2's zero-expansion face).

package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// putBucketTargetWire creates one permission target through the real wire
// with the given repos[] and action list for one user.
func putBucketTargetWire(t *testing.T, h *harness, name string, repos []string, user, actions string, want int) {
	t.Helper()
	reposJSON, err := json.Marshal(repos)
	if err != nil {
		t.Fatalf("marshal repos: %v", err)
	}
	body := `{"name":"` + name + `","repos":` + string(reposJSON) + `,"principals":{"users":{"` + user + `":[` + actions + `]},"groups":{}}}`
	resp := t215As(t, h, http.MethodPost, "api/v1/permissions", adminUser, adminPass, body)
	raw := readAllT444(t, resp)
	if resp.StatusCode != want {
		t.Fatalf("POST /api/v1/permissions %s = %d, want %d (body: %s)", name, resp.StatusCode, want, raw)
	}
}

// TestPermissionWirePresetBuckets: the three preset bucket literals are
// storable repos[] entries (pre-T-491 the family answered the
// unknown-repository 400 on each of them — M16 B-2.16's drift), the echo
// round-trips them verbatim, and the closed set holds: mixing in an
// unknown repository still 400s on that entry, and the reference's fourth
// family member ("ANY") stays refused.
func TestPermissionWirePresetBuckets(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"reader", "reader-pw"}})
	seedRepo(t, h, "generic-local")

	tests := []struct {
		name  string
		repos []string
		want  int
	}{
		{"any local alone", []string{"ANY LOCAL"}, http.StatusCreated},
		{"any remote alone", []string{"ANY REMOTE"}, http.StatusCreated},
		{"any distribution alone", []string{"ANY DISTRIBUTION"}, http.StatusCreated},
		{"bucket beside a real repository", []string{"generic-local", "ANY REMOTE"}, http.StatusCreated},
		{"unknown entry beside a bucket", []string{"ANY LOCAL", "no-such-repo"}, http.StatusBadRequest},
		{"the fourth family member stays refused", []string{"ANY"}, http.StatusBadRequest},
		{"lowercased spelling is not a bucket", []string{"any local"}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			putBucketTargetWire(t, h, "bucket-tgt", tt.repos, "reader", `"read"`, tt.want)
		})
	}

	// The stored literal survives the create-or-replace of the last row and
	// echoes verbatim in the admin list view.
	putBucketTargetWire(t, h, "echo-tgt", []string{"ANY LOCAL"}, "reader", `"read"`, http.StatusCreated)
	if got := echoOf(t, h, "echo-tgt", "reader"); len(got) != 1 || got[0] != "read" {
		t.Fatalf("echo actions = %v, want [read]", got)
	}
	listBody := t215Admin(t, h, http.MethodGet, "api/v1/permissions", "", 200)
	var targets []struct {
		Name  string   `json:"name"`
		Repos []string `json:"repos"`
	}
	if err := json.Unmarshal([]byte(listBody), &targets); err != nil {
		t.Fatalf("decode permission list: %v", err)
	}
	for _, tg := range targets {
		if tg.Name != "echo-tgt" {
			continue
		}
		if len(tg.Repos) != 1 || tg.Repos[0] != "ANY LOCAL" {
			t.Fatalf("echo repos = %v, want [ANY LOCAL] (verbatim round-trip)", tg.Repos)
		}
		return
	}
	t.Fatal("echo-tgt missing from the list view")
}

// TestWildcardBucketContentPlaneProbes: the AC1/AC2 HTTP probes. One ANY
// LOCAL read grant, one granted user, one stranger: the granted user reads
// a local repository (and a local repository created AFTER the grant — the
// wildcard-effectiveness assertion), cannot write into it (read-only, the
// verb columns do not expand), and the stranger keeps the 403 on every
// face.
func TestWildcardBucketContentPlaneProbes(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"reader", "reader-pw"}, {"stranger", "stranger-pw"}})
	seedRepo(t, h, "generic-local")
	putBucketTargetWire(t, h, "any-local-read", []string{"ANY LOCAL"}, "reader", `"read"`, http.StatusCreated)

	// Admin deposits one artifact per repository.
	for _, repo := range []string{"generic-local", "late-local"} {
		if repo == "late-local" {
			// Created AFTER the grant — the auto-inclusion probe.
			seedRepo(t, h, repo)
		}
		resp := h.do(http.MethodPut, "/binflow/"+repo+"/acme/v1.bin", adminUser, adminPass, []byte("x"), nil)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("admin PUT %s = %d, want 201", repo, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	tests := []struct {
		name       string
		method     string
		user, pass string
		path       string
		want       int
	}{
		{"granted user reads the pre-grant local repository", http.MethodGet, "reader", "reader-pw", "/binflow/generic-local/acme/v1.bin", http.StatusOK},
		{"granted user reads the post-grant local repository", http.MethodGet, "reader", "reader-pw", "/binflow/late-local/acme/v1.bin", http.StatusOK},
		{"granted user gets a read-only 403 on write", http.MethodPut, "reader", "reader-pw", "/binflow/generic-local/acme/v2.bin", http.StatusForbidden},
		{"stranger keeps the 403 on the pre-grant repository", http.MethodGet, "stranger", "stranger-pw", "/binflow/generic-local/acme/v1.bin", http.StatusForbidden},
		{"stranger keeps the 403 on the post-grant repository", http.MethodGet, "stranger", "stranger-pw", "/binflow/late-local/acme/v1.bin", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			if tt.method == http.MethodPut {
				body = []byte("x")
			}
			resp := h.do(tt.method, tt.path, tt.user, tt.pass, body, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.want {
				t.Fatalf("%s %s as %s = %d, want %d", tt.method, tt.path, tt.user, resp.StatusCode, tt.want)
			}
		})
	}

	// The stranger's write is 403 too (the content plane's verb face).
	resp := h.do(http.MethodPut, "/binflow/generic-local/x.bin", "stranger", "stranger-pw", []byte("x"), nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("stranger PUT = %d, want 403", resp.StatusCode)
	}
}
