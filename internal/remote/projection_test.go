// T-529: <K>-cache projection derivation registry — the behavior under test
// is the field-by-field derivation of the shadow cache repository descriptor
// off a remote row (remote-cache-projection.md section 1), its suffix
// constant, the storeArtifactsLocally gate seam, and the reload semantics
// (fresh derivation per call, nothing persisted).
package remote

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// newProjectionEnv opens one engine over a fresh stack. No upstream server:
// the projection is a pure store read, the fetch paths are not exercised.
func newProjectionEnv(t *testing.T) *Engine {
	t.Helper()
	ctx := context.Background()
	st, err := storage.OpenEngine(t.TempDir(), storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })
	eng, err := NewEngine(st, md, EngineOptions{Now: func() time.Time { return time.Now().UTC() }, Logger: silentLogger})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return eng
}

// seedRepo writes one repositories row of the given type; rc == "" seeds an
// empty config blob.
func seedRepo(t *testing.T, eng *Engine, key, typ, pkg, config string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := eng.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: typ, PackageType: pkg,
		Config: config, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create repo %s: %v", key, err)
	}
}

// canonical is a minimal remote canonical JSON carrying a url, so the row
// shape matches what repo.Service persists.
const canonical = `{"url":"https://up.example/dl","username":"","retrievalCachePeriodSecs":7200,` +
	`"missedRetrievalCachePeriodSecs":1800,"socketTimeoutSecs":15,` +
	`"assumedOfflinePeriodSecs":300,"hardFail":false,` +
	`"allowPrivateUpstream":false,"priorityResolution":false}`

func TestCacheProjectionDerivation(t *testing.T) {
	tests := []struct {
		name string
		pkg  string
		conf string
		want CacheProjection
	}{
		{
			name: "canonical defaults on a bare remote row",
			conf: canonical,
			want: CacheProjection{
				Key: "maven-remote" + CacheSuffix, PackageType: "generic",
				RepoLayout:     "maven-2-default",
				HandleReleases: true, HandleSnapshots: true,
			},
		},
		{
			name: "every inherited field mirrors the remote row",
			pkg:  "npm",
			conf: `{"url":"https://up.example/npm","priorityResolution":true,` +
				`"repoLayoutRef":"npm-default","blackedOut":true,` +
				`"archiveBrowsingEnabled":true,"handleReleases":false,"handleSnapshots":false}`,
			want: CacheProjection{
				Key: "maven-remote" + CacheSuffix, PackageType: "npm",
				RepoLayout: "npm-default", PriorityResolution: true,
				HandleReleases: false, HandleSnapshots: false,
				ArchiveBrowsing: true, BlackedOut: true,
			},
		},
		{
			name: "empty config blob falls back to the product defaults",
			conf: "",
			want: CacheProjection{
				Key: "maven-remote" + CacheSuffix, PackageType: "generic",
				RepoLayout:     "maven-2-default",
				HandleReleases: true, HandleSnapshots: true,
			},
		},
		{
			name: "non-decoding config blob falls back to the product defaults",
			conf: "{not json",
			want: CacheProjection{
				Key: "maven-remote" + CacheSuffix, PackageType: "generic",
				RepoLayout:     "maven-2-default",
				HandleReleases: true, HandleSnapshots: true,
			},
		},
		{
			name: "explicit handleReleases=false alone inherits the rest",
			conf: `{"url":"https://up.example/dl","handleReleases":false}`,
			want: CacheProjection{
				Key: "maven-remote" + CacheSuffix, PackageType: "generic",
				RepoLayout:     "maven-2-default",
				HandleReleases: false, HandleSnapshots: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng := newProjectionEnv(t)
			pkg := tt.pkg
			if pkg == "" {
				pkg = "generic"
			}
			seedRepo(t, eng, "maven-remote", "remote", pkg, tt.conf)
			got, ok := eng.CacheProjection(context.Background(), "maven-remote")
			if !ok {
				t.Fatalf("CacheProjection ok=false, want true")
			}
			if got != tt.want {
				t.Errorf("CacheProjection = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCacheProjectionKeySuffixConstant(t *testing.T) {
	// The constant is the single source internal/repo's -cache create/update
	// guard consumes (remote-cache-projection.md 1.1/1.3): pin its spelling.
	if CacheSuffix != "-cache" {
		t.Fatalf("CacheSuffix = %q, want %q", CacheSuffix, "-cache")
	}
	eng := newProjectionEnv(t)
	seedRepo(t, eng, "k", "remote", "generic", canonical)
	got, ok := eng.CacheProjection(context.Background(), "k")
	if !ok || got.Key != "k"+CacheSuffix {
		t.Fatalf("projection key = %q (ok=%t), want %q", got.Key, ok, "k"+CacheSuffix)
	}
}

func TestCacheProjectionGateStoreArtifactsLocally(t *testing.T) {
	tests := []struct {
		name string
		conf string
		want bool
	}{
		{"absent knob projects (today's always-true wiring)", canonical, true},
		{"storeArtifactsLocally=true projects", `{"url":"https://u/","storeArtifactsLocally":true}`, true},
		{"storeArtifactsLocally=false has no projection", `{"url":"https://u/","storeArtifactsLocally":false}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng := newProjectionEnv(t)
			seedRepo(t, eng, "gated", "remote", "generic", tt.conf)
			_, ok := eng.CacheProjection(context.Background(), "gated")
			if ok != tt.want {
				t.Errorf("ok = %t, want %t", ok, tt.want)
			}
		})
	}
}

func TestCacheProjectionUnknownOrNonRemoteKey(t *testing.T) {
	eng := newProjectionEnv(t)
	seedRepo(t, eng, "a-local", "local", "generic", `{}`)
	seedRepo(t, eng, "a-virtual", "virtual", "generic", `{}`)
	tests := []struct{ key, why string }{
		{"no-such-remote", "unknown key"},
		{"a-local", "local row"},
		{"a-virtual", "virtual row"},
		{"", "empty key"},
	}
	for _, tt := range tests {
		t.Run(tt.why, func(t *testing.T) {
			if _, ok := eng.CacheProjection(context.Background(), tt.key); ok {
				t.Errorf("CacheProjection(%q) ok=true, want false (%s)", tt.key, tt.why)
			}
		})
	}
}

// TestCacheProjectionReloadSemantics pins the derived-not-persisted contract
// (spec 1.1: every config reload re-projects): a config change is visible on
// the very next call, and deleting the remote row ends the projection — no
// stored entity outlives the remote.
func TestCacheProjectionReloadSemantics(t *testing.T) {
	ctx := context.Background()
	eng := newProjectionEnv(t)
	seedRepo(t, eng, "flip", "remote", "generic", canonical)

	if got, ok := eng.CacheProjection(ctx, "flip"); !ok || got.PriorityResolution || got.BlackedOut {
		t.Fatalf("initial derivation = %+v (ok=%t), want priority/blackedOut false", got, ok)
	}

	// The config-update equivalent of a reload: priority flips on, blackout
	// marks on, layout changes.
	row, err := eng.md.Repos().Get(ctx, "flip")
	if err != nil {
		t.Fatalf("get row: %v", err)
	}
	row.Config = `{"url":"https://up.example/dl","priorityResolution":true,` +
		`"repoLayoutRef":"gradle-default","blackedOut":true}`
	if err := eng.md.Repos().Update(ctx, row); err != nil {
		t.Fatalf("update row: %v", err)
	}
	got, ok := eng.CacheProjection(ctx, "flip")
	if !ok {
		t.Fatalf("post-update derivation ok=false, want true")
	}
	if !got.PriorityResolution || !got.BlackedOut || got.RepoLayout != "gradle-default" {
		t.Errorf("post-update derivation = %+v, want priority+blackedOut true, layout gradle-default", got)
	}

	// The projection dies with the remote row.
	if err := eng.md.Repos().Delete(ctx, "flip"); err != nil {
		t.Fatalf("delete row: %v", err)
	}
	if _, ok := eng.CacheProjection(ctx, "flip"); ok {
		t.Errorf("post-delete derivation ok=true, want false")
	}
}

// TestCacheProjectionConcurrentDerivation exercises the registry under
// concurrent readers racing a config writer (the four-bucket order derives
// per request; the engine must stay safe for that shape).
func TestCacheProjectionConcurrentDerivation(t *testing.T) {
	ctx := context.Background()
	eng := newProjectionEnv(t)
	seedRepo(t, eng, "racy", "remote", "generic", canonical)

	row, err := eng.md.Repos().Get(ctx, "racy")
	if err != nil {
		t.Fatalf("get row: %v", err)
	}
	flipped := `{"url":"https://up.example/dl","priorityResolution":true}`
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			row.Config = canonical
			if i%2 == 1 {
				row.Config = flipped
			}
			if err := eng.md.Repos().Update(ctx, row); err != nil {
				t.Errorf("update row: %v", err)
				return
			}
		}
	}()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 500 {
				proj, ok := eng.CacheProjection(ctx, "racy")
				if !ok {
					t.Error("concurrent derivation ok=false unexpectedly")
					return
				}
				// Whatever config instant the read lands on, the projection
				// must be internally consistent: the key always carries the
				// suffix, and priority rides the row's mark verbatim.
				if proj.Key != "racy"+CacheSuffix {
					t.Errorf("concurrent derivation key = %q", proj.Key)
					return
				}
			}
		}()
	}
	wg.Wait()
	<-done
}
