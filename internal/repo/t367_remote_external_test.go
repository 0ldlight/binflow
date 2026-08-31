package repo_test

// M13 T-367: the chartsBaseUrl per-protocol config seat (parse/echo/refusal),
// the registry-v2 same-type member rule (the conductor rider) and the
// RemoteExternalPlane seam (the absolute-URL dependency pull-through with
// its read gates and landing).

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// ---- the chartsBaseUrl config seat ----

func TestT367ChartsBaseURLConfigSeat(t *testing.T) {
	// helm rides the dynamic overlay (the static enum is the pre-M11 five);
	// maven/generic below stay on it.
	unlocked := repo.PackageTypeVerdict{Known: true, Unlocked: true}
	e, _ := gateEnv(t, map[string]repo.PackageTypeVerdict{"helm": unlocked})
	tests := []struct {
		name        string
		packageType string
		config      string
		wantRefus   bool
		wantEcho    string // the canonical chartsBaseUrl spelling on success
		refusalIn   string
	}{
		{
			name:        "helm remote accepts the base",
			packageType: repo.PackageHelm,
			config:      `{"url":"http://up.example/charts","chartsBaseUrl":"http://mirror.example/base/"}`,
			wantEcho:    "http://mirror.example/base",
		},
		{
			name:        "helm remote explicit empty clears",
			packageType: repo.PackageHelm,
			config:      `{"url":"http://up.example/charts","chartsBaseUrl":""}`,
			wantEcho:    "",
		},
		{
			name:        "helm remote absent stays absent",
			packageType: repo.PackageHelm,
			config:      `{"url":"http://up.example/charts"}`,
			wantEcho:    "",
		},
		{
			name:        "maven remote refuses the field by name",
			packageType: repo.PackageMaven,
			config:      `{"url":"http://up.example/m2","chartsBaseUrl":"http://mirror.example/base"}`,
			wantRefus:   true,
			refusalIn:   "chartsBaseUrl",
		},
		{
			name:        "generic remote refuses the field by name",
			packageType: repo.PackageGeneric,
			config:      `{"url":"http://up.example/files","chartsBaseUrl":"http://mirror.example/base"}`,
			wantRefus:   true,
			refusalIn:   "chartsBaseUrl",
		},
		{
			name:        "helm remote refuses a bad scheme",
			packageType: repo.PackageHelm,
			config:      `{"url":"http://up.example/charts","chartsBaseUrl":"ftp://mirror.example/base"}`,
			wantRefus:   true,
			refusalIn:   "scheme must be http or https",
		},
		{
			name:        "helm remote refuses a hostless base",
			packageType: repo.PackageHelm,
			config:      `{"url":"http://up.example/charts","chartsBaseUrl":"http:///base"}`,
			wantRefus:   true,
			refusalIn:   "host is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "t367-cfg-" + strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(tt.name, " ", "-"), "/", "-"))
			stored, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
				RepoKey: key, Type: repo.TypeRemote, PackageType: tt.packageType, Config: tt.config,
			})
			if tt.wantRefus {
				if err == nil || !errors.Is(err, repo.ErrInvalidRepoConfig) || !strings.Contains(err.Error(), tt.refusalIn) {
					t.Fatalf("create err = %v, want ErrInvalidRepoConfig naming %q", err, tt.refusalIn)
				}
				return
			}
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			// The canonical echo carries the trimmed spelling.
			if got := stored.Config; !strings.Contains(got, `"chartsBaseUrl":"`+tt.wantEcho+`"`) && tt.wantEcho != "" {
				t.Fatalf("canonical echo = %s, want chartsBaseUrl %q", got, tt.wantEcho)
			}
			if tt.wantEcho == "" && strings.Contains(stored.Config, "chartsBaseUrl") {
				t.Fatalf("canonical echo = %s, want no chartsBaseUrl key", stored.Config)
			}
			round, rerr := e.svc.GetRepo(context.Background(), admin(), key)
			if rerr != nil || round.Config != stored.Config {
				t.Fatalf("GET echo = (%v, %s), want the canonical form %s", rerr, round.Config, stored.Config)
			}
		})
	}
}

// ---- the registry-v2 same-type member rule (the rider) ----

func TestT367V2MemberTypes(t *testing.T) {
	unlocked := repo.PackageTypeVerdict{Known: true, Unlocked: true}
	e, _ := gateEnv(t, map[string]repo.PackageTypeVerdict{
		"helm":    unlocked,
		"helmoci": unlocked,
		"docker":  unlocked,
	})
	seed := func(key, packageType string) {
		t.Helper()
		if err := e.md.Repos().Create(context.Background(), &metadata.Repo{
			RepoKey: key, Type: repo.TypeLocal, PackageType: packageType, Config: "{}",
		}); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}
	seed("t367-docker-local", repo.PackageDocker)
	seed("t367-helmoci-local", repo.PackageHelmOCI)
	seed("t367-helm-local", repo.PackageHelm)

	tests := []struct {
		name      string
		key       string
		own       string
		members   string
		wantRefus bool
		refusalIn string
	}{
		{
			name:    "helmoci virtual with helmoci members",
			key:     "t367-virt-ok",
			own:     repo.PackageHelmOCI,
			members: `{"repositories":["t367-helmoci-local"]}`,
		},
		{
			name:      "helmoci virtual with a docker member",
			key:       "t367-virt-hoci-docker",
			own:       repo.PackageHelmOCI,
			members:   `{"repositories":["t367-helmoci-local","t367-docker-local"]}`,
			wantRefus: true,
			refusalIn: "cannot mix the helmoci and docker package types",
		},
		{
			// docker virtuals are still refused by the class matrix BEFORE
			// member validation (remote docker opened in T-392; the virtual
			// half stays refused); the refusal below names the matrix, and
			// the member rule's docker-owning arm is then live for a future
			// flip.
			name:      "docker virtual with a helmoci member",
			key:       "t367-virt-docker-hoci",
			own:       repo.PackageDocker,
			members:   `{"repositories":["t367-docker-local","t367-helmoci-local"]}`,
			wantRefus: true,
			refusalIn: "are not supported",
		},
		{
			// The rider's scope is the v2 family: a non-v2 virtual keeps
			// its earlier rules only (registered in the ticket report).
			name:    "helm virtual with a docker member stays outside the rider",
			key:     "t367-virt-helm-docker",
			own:     repo.PackageHelm,
			members: `{"repositories":["t367-helm-local","t367-docker-local"]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
				RepoKey: tt.key, Type: repo.TypeVirtual, PackageType: tt.own, Config: tt.members,
			})
			if tt.wantRefus {
				if err == nil || !strings.Contains(err.Error(), tt.refusalIn) {
					t.Fatalf("create err = %v, want the refusal containing %q", err, tt.refusalIn)
				}
				return
			}
			if err != nil {
				t.Fatalf("create: %v", err)
			}
		})
	}
}

// ---- the RemoteExternalPlane seam ----

// seedT367Remote writes one REMOTE helm repository row plus its
// remote_configs row (the loopback upstream needs the admin-set exemption).
func seedT367Remote(t *testing.T, e *env, key, upstream string) {
	t.Helper()
	if err := e.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeRemote, PackageType: repo.PackageHelm, Config: "{}",
	}); err != nil {
		t.Fatalf("seed remote %s: %v", key, err)
	}
	if err := e.md.Remote().CreateConfig(context.Background(), &metadata.RemoteConfig{
		RepoKey: key, URL: strings.TrimRight(upstream, "/"),
		ContentTTLSeconds:    7200,
		MetadataTTLSeconds:   600,
		AllowPrivateUpstream: true,
	}); err != nil {
		t.Fatalf("seed remote config %s: %v", key, err)
	}
}

func TestT367FetchExternalSeam(t *testing.T) {
	e := newEnv(t)
	var hits atomic.Int64
	dep := "dependency-bytes"
	third := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/deps/dep-1.0.0.tgz" {
			_, _ = w.Write([]byte(dep))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(third.Close)
	seedT367Remote(t, e, "t367-remote", third.URL)

	plane, ok := e.svc.(repo.RemoteExternalPlane)
	if !ok {
		t.Fatal("the concrete service must satisfy RemoteExternalPlane")
	}
	folded := "_external/http/" + strings.TrimPrefix(third.URL, "http://") + "/deps/dep-1.0.0.tgz"

	// The anonymous read is refused before any egress.
	if _, _, err := plane.FetchExternal(context.Background(), nil, "t367-remote", folded, third.URL+"/deps/dep-1.0.0.tgz"); !errors.Is(err, repo.ErrUnauthorized) {
		t.Fatalf("anonymous fetch err = %v, want ErrUnauthorized", err)
	}
	// A non-remote repository has no face.
	if err := e.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: "t367-local", Type: repo.TypeLocal, PackageType: repo.PackageHelm, Config: "{}",
	}); err != nil {
		t.Fatalf("seed local: %v", err)
	}
	if _, _, err := plane.FetchExternal(context.Background(), admin(), "t367-local", folded, third.URL+"/deps/dep-1.0.0.tgz"); !errors.Is(err, repo.ErrRepoTypeNotSupported) {
		t.Fatalf("local fetch err = %v, want ErrRepoTypeNotSupported", err)
	}

	rc, node, err := plane.FetchExternal(context.Background(), admin(), "t367-remote", folded, third.URL+"/deps/dep-1.0.0.tgz")
	if err != nil {
		t.Fatalf("FetchExternal: %v", err)
	}
	body, rerr := io.ReadAll(rc)
	_ = rc.Close()
	if rerr != nil || string(body) != dep {
		t.Fatalf("body = (%q, %v), want the dependency bytes", body, rerr)
	}
	if node == nil || node.RepoKey != "t367-remote" || node.Path != folded {
		t.Fatalf("node = %+v, want the folded-path landing", node)
	}
	// The second pull is a member-local HIT with zero egress; the hints ride
	// the reader structurally.
	rc2, _, err := plane.FetchExternal(context.Background(), admin(), "t367-remote", folded, third.URL+"/deps/dep-1.0.0.tgz")
	if err != nil {
		t.Fatalf("second FetchExternal: %v", err)
	}
	if hx, ok := rc2.(interface{ ExtraHeaders() http.Header }); !ok || hx.ExtraHeaders().Get("X-BinFlow-Cache") != "HIT" {
		t.Fatalf("second pull hints = %v, want X-BinFlow-Cache HIT", rc2)
	}
	_ = rc2.Close()
	if got := hits.Load(); got != 1 {
		t.Fatalf("third-party hits = %d, want 1 (cached)", got)
	}

	// The unfound family wraps ErrNodeNotFound for the face's own wording.
	if _, _, err = plane.FetchExternal(context.Background(), admin(), "t367-remote", "_external/http/third.example/absent.tgz", third.URL+"/absent.tgz"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("absent dependency err = %v, want ErrNodeNotFound", err)
	}
}

func TestT367FetchVirtualExternalMemberGuard(t *testing.T) {
	e := newEnv(t)
	dep := "dependency-bytes"
	third := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/deps/dep-1.0.0.tgz" {
			_, _ = w.Write([]byte(dep))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(third.Close)
	seedT367Remote(t, e, "t367-vm-remote", third.URL)
	if err := e.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: "t367-vm-local", Type: repo.TypeLocal, PackageType: repo.PackageHelm, Config: "{}",
	}); err != nil {
		t.Fatalf("seed local member: %v", err)
	}
	if err := e.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: "t367-vm-virt", Type: repo.TypeVirtual, PackageType: repo.PackageHelm,
		Config: `{"repositories":["t367-vm-local","t367-vm-remote"]}`,
	}); err != nil {
		t.Fatalf("seed virtual: %v", err)
	}
	if err := e.md.Virtual().SetMembers(context.Background(), "t367-vm-virt", []string{"t367-vm-local", "t367-vm-remote"}); err != nil {
		t.Fatalf("set members: %v", err)
	}

	plane := e.svc.(repo.RemoteExternalPlane)
	folded := "_external/http/" + strings.TrimPrefix(third.URL, "http://") + "/deps/dep-1.0.0.tgz"
	target := third.URL + "/deps/dep-1.0.0.tgz"

	// The membership guard: a key outside the order never reaches the engine.
	if _, _, err := plane.FetchVirtualExternal(context.Background(), admin(), "t367-vm-virt", "t367-remote", folded, target); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("non-member err = %v, want ErrRepoNotFound", err)
	}
	// "t367-remote" IS a real repository — just not a member; a LOCAL member
	// is in the order but has no egress face.
	if _, _, err := plane.FetchVirtualExternal(context.Background(), admin(), "t367-vm-virt", "t367-vm-local", folded, target); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("local member err = %v, want ErrRepoNotFound (no egress face)", err)
	}
	// The admitting member's hop lands in ITS namespace.
	rc, node, err := plane.FetchVirtualExternal(context.Background(), admin(), "t367-vm-virt", "t367-vm-remote", folded, target)
	if err != nil {
		t.Fatalf("FetchVirtualExternal: %v", err)
	}
	_ = rc.Close()
	if node == nil || node.RepoKey != "t367-vm-remote" || node.Path != folded {
		t.Fatalf("node = %+v, want the member-namespaced landing", node)
	}
	if _, _, err = plane.FetchVirtualExternal(context.Background(), admin(), "t367-vm-virt", "t367-vm-remote", "_external/http/third.example/absent.tgz", third.URL+"/absent.tgz"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("absent member dependency err = %v, want ErrNodeNotFound", err)
	}
}
