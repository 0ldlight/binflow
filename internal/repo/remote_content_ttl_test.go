package repo_test

// LOOP 003 pre-close fix (T-L003-1 handoff patch, ADR-0012 erratum three):
// the content-TTL three-way contract — the create-time default is PER
// PACKAGE TYPE (docker/helmoci 21600, every other type 7200), an explicit
// wire value passes through untouched, and an unset or hand-mangled
// remote_configs row resolves through the SAME single point the fetch
// engine's loadRepo reads (remote.ResolveContentTTLSeconds), so the repo
// service's writes and reads and the engine's fetch window can never
// disagree about one repository's cache period.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestRemoteCreateContentTTLDefaultPerPackageType: the create-time write
// (CreateRepo -> parseRemoteConfig -> remote_configs.content_ttl_seconds).
// The absent knob lands on the package type's default; an explicit value
// (including a legacy 7200 on a docker remote) passes through verbatim; and
// the stored value is a fixed point of the engine's resolution — what
// loadRepo would read equals what the create wrote.
func TestRemoteCreateContentTTLDefaultPerPackageType(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	cases := []struct {
		name        string
		packageType string
		explicit    string // extra JSON body; "" leaves the knob absent
		want        int64
	}{
		// helmoci has no row here: the service-level create legal set
		// (validate.go's static five) does not admit it — its resolution
		// legs run in the two suites below over seeded rows, and the
		// create-path dispatch is the same DefaultContentTTLSecondsFor
		// branch the docker rows prove.
		{"docker absent takes the 21600 package default", repo.PackageDocker, "", remote.DockerRemoteContentTTLSeconds},
		{"maven absent keeps the generic 7200", repo.PackageMaven, "", remote.GenericContentTTLSeconds},
		{"npm absent keeps the generic 7200", repo.PackageNpm, "", remote.GenericContentTTLSeconds},
		{"generic absent keeps the generic 7200", repo.PackageGeneric, "", remote.GenericContentTTLSeconds},
		{"docker explicit passes through", repo.PackageDocker, `,"retrievalCachePeriodSecs":3600`, 3600},
		{"docker explicit legacy 7200 is never rewritten", repo.PackageDocker, `,"retrievalCachePeriodSecs":7200`, 7200},
		{"maven explicit passes through", repo.PackageMaven, `,"retrievalCachePeriodSecs":21600`, 21600},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := "ttl-create-" + tc.packageType + "-" + string(rune('a'+i))
			if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
				RepoKey: key, Type: repo.TypeRemote, PackageType: tc.packageType,
				Config: `{"url":"http://upstream.example/base"` + tc.explicit + "}",
			}); err != nil {
				t.Fatalf("CreateRepo(%s): %v", key, err)
			}
			row, err := e.md.Remote().GetConfig(ctx, key)
			if err != nil {
				t.Fatalf("GetConfig(%s): %v", key, err)
			}
			if row.ContentTTLSeconds != tc.want {
				t.Errorf("content_ttl_seconds = %d, want %d (package %s)", row.ContentTTLSeconds, tc.want, tc.packageType)
			}
			// Engine parity: the fetcher's loadRepo computes
			// ResolveContentTTLSeconds(stored, packageType) — the create
			// write and the engine read must agree on one value.
			if got := remote.ResolveContentTTLSeconds(row.ContentTTLSeconds, tc.packageType); got != tc.want {
				t.Errorf("engine resolution of the stored row = %d, want %d: the create write and the engine read disagree", got, tc.want)
			}
		})
	}
}

// seedV2RemoteTTL seeds one registry-v2 family remote with an explicit
// content TTL on its remote_configs row (0 = the unset/hand-mangled shape).
func seedV2RemoteTTL(t *testing.T, e *env, key, packageType string, contentTTL int64) {
	t.Helper()
	if err := e.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeRemote, PackageType: packageType, Config: "{}",
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
	if err := e.md.Remote().CreateConfig(context.Background(), &metadata.RemoteConfig{
		RepoKey: key, URL: "http://127.0.0.1:1/v2/up", ContentTTLSeconds: contentTTL, MetadataTTLSeconds: 600,
	}); err != nil {
		t.Fatalf("seed remote config %s: %v", key, err)
	}
}

// TestRemoteUpstreamContentTTLResolution: the adapter-facing upstream facts
// (RemoteV2Plane.RemoteUpstream) resolve the content TTL through the single
// point too — an unset row takes the docker/helmoci 21600 default, an
// explicit stored value wins.
func TestRemoteUpstreamContentTTLResolution(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	plane, ok := e.svc.(repo.RemoteV2Plane)
	if !ok {
		t.Fatal("the service does not implement repo.RemoteV2Plane")
	}
	cases := []struct {
		name        string
		packageType string
		storedTTL   int64
		want        int64
	}{
		{"docker unset row resolves 21600", repo.PackageDocker, 0, remote.DockerRemoteContentTTLSeconds},
		{"helmoci unset row resolves 21600", repo.PackageHelmOCI, 0, remote.DockerRemoteContentTTLSeconds},
		{"docker explicit 7200 wins", repo.PackageDocker, 7200, 7200},
		{"helmoci explicit 3600 wins", repo.PackageHelmOCI, 3600, 3600},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := "ttl-up-" + tc.packageType + "-" + string(rune('a'+i))
			seedV2RemoteTTL(t, e, key, tc.packageType, tc.storedTTL)
			up, err := plane.RemoteUpstream(ctx, admin(), key)
			if err != nil {
				t.Fatalf("RemoteUpstream(%s): %v", key, err)
			}
			if up.ContentTTLSeconds != tc.want {
				t.Errorf("RemoteUpstream.ContentTTLSeconds = %d, want %d (stored %d, package %s)",
					up.ContentTTLSeconds, tc.want, tc.storedTTL, tc.packageType)
			}
			if got := remote.ResolveContentTTLSeconds(tc.storedTTL, tc.packageType); got != tc.want {
				t.Errorf("single-point resolution(%d, %s) = %d, want %d: the adapter facts and the engine read disagree",
					tc.storedTTL, tc.packageType, got, tc.want)
			}
		})
	}
}

// TestRemoteContentCacheWindowMatchesEngineResolution: the landed cache
// row's freshness window (the remoteContentTTL read behind
// LandRemoteBlob's cache-state write) equals the single-point resolution of
// the repository's row — the unset docker row's window is the 21600 the
// engine's own fetch loop would use, and an explicit row keeps its value.
func TestRemoteContentCacheWindowMatchesEngineResolution(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	plane, ok := e.svc.(repo.RemoteV2Plane)
	if !ok {
		t.Fatal("the service does not implement repo.RemoteV2Plane")
	}
	cases := []struct {
		name      string
		storedTTL int64
		want      time.Duration
	}{
		{"unset docker row caches for the 21600 package default", 0, 21600 * time.Second},
		{"explicit 7200 row caches for 7200", 7200, 7200 * time.Second},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := "ttl-cache-" + string(rune('a'+i))
			seedV2RemoteTTL(t, e, key, repo.PackageDocker, tc.storedTTL)
			body := "the cached blob body"
			digest := sha256HexOf(body)
			path := "myapp/manifests/" + digest
			if _, err := plane.LandRemoteBlob(ctx, admin(), key, path, digest,
				"application/octet-stream", strings.NewReader(body)); err != nil {
				t.Fatalf("LandRemoteBlob(%s): %v", key, err)
			}
			entry, err := e.md.Remote().GetCache(ctx, key, path)
			if err != nil {
				t.Fatalf("GetCache(%s, %s): %v", key, path, err)
			}
			fetched, err := time.Parse(time.RFC3339, entry.FetchedAt)
			if err != nil {
				t.Fatalf("FetchedAt %q: %v", entry.FetchedAt, err)
			}
			expires, err := time.Parse(time.RFC3339, entry.ExpiresAt)
			if err != nil {
				t.Fatalf("ExpiresAt %q: %v", entry.ExpiresAt, err)
			}
			window := expires.Sub(fetched)
			if window != tc.want {
				t.Errorf("cache window = %v, want %v (stored content TTL %d)", window, tc.want, tc.storedTTL)
			}
			if want := time.Duration(remote.ResolveContentTTLSeconds(tc.storedTTL, repo.PackageDocker)) * time.Second; window != want {
				t.Errorf("cache window %v != engine resolution %v: the service write and the engine fetch disagree", window, want)
			}
		})
	}
}
