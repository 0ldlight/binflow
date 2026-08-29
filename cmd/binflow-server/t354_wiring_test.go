package main

// T-354's assembly wiring: the copyMoveIndexObserver the full assembly
// attaches through repo.AttachCopyMoveObserver. Two levels:
//
//   - the DISPATCH table (fake reindexers recording calls): package-type
//     routing over the four protocol rows, the local-target guard, the
//     copy-only op guard, the generic no-op and the missing-row tolerance;
//   - one REAL end-to-end leg (npm, the T-351 notarget flip): the real
//     maven/npm adapters over a real openStack, the real seam firing after
//     a real copy, the target packument answering the copied tarball's
//     version — the same shape newAssembledServer wires, assembled here
//     with bare constructors so the process-wide adapter registry stays
//     untouched (the ONE full-assembly caller rule).

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter/conan"
	"github.com/lzwzzy/binflow/internal/adapter/deb"
	"github.com/lzwzzy/binflow/internal/adapter/maven"
	"github.com/lzwzzy/binflow/internal/adapter/npm"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// discardLogger is the wiring tests' sink (the observer logs, never panics).
func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// recordingReindexer records one dispatch.
type recordingReindexer struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordingReindexer) ReindexDirs(_ context.Context, _ *repo.Principal, repoKey string, dirs []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, repoKey+"("+strings.Join(dirs, ",")+")")
	return nil
}

func (r *recordingReindexer) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// openTestStack boots a real openStack on a scratch data directory.
func openTestStack(t *testing.T) *stack {
	t.Helper()
	cfg := configDefaults()
	cfg.Storage.DataDir = t.TempDir()
	st, err := openStack(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	t.Cleanup(func() { st.close(discardLogger()) })
	return st
}

// TestT354DispatchMatrix: the four protocol rows dispatch with the
// directory set; generic has no entry; move and the empty set never
// dispatch; remote rows (structurally refused by the pipeline's precheck)
// and unknown keys only log.
func TestT354DispatchMatrix(t *testing.T) {
	st := openTestStack(t)
	ctx := context.Background()

	fakes := map[string]*recordingReindexer{
		maven.Protocol: {}, npm.Protocol: {}, deb.Protocol: {}, conan.Protocol: {},
	}
	rx := make(map[string]dirReindexer, len(fakes))
	for k, v := range fakes {
		rx[k] = v
	}
	obs := copyMoveIndexObserver{repos: st.md.Repos(), reindexers: rx, log: discardLogger()}

	rows := map[string]string{
		"maven-repo": maven.Protocol, "npm-repo": npm.Protocol,
		"deb-repo": deb.Protocol, "conan-repo": conan.Protocol, "gen-repo": "generic",
		"rem-repo": npm.Protocol,
	}
	for key, pt := range rows {
		cls := repo.TypeLocal
		if key == "rem-repo" {
			cls = repo.TypeRemote
		}
		if err := st.md.Repos().Create(ctx, &metadata.Repo{RepoKey: key, Type: cls, PackageType: pt}); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}

	for _, key := range []string{"maven-repo", "npm-repo", "deb-repo", "conan-repo"} {
		obs.AfterCopyMove(ctx, repo.OpCopy, key, []string{"a/b/"})
	}
	protoOf := map[string]string{"maven-repo": maven.Protocol, "npm-repo": npm.Protocol,
		"deb-repo": deb.Protocol, "conan-repo": conan.Protocol}
	for key, proto := range protoOf {
		f := fakes[proto]
		if got := f.snapshot(); len(got) != 1 || got[0] != key+"(a/b/)" {
			t.Errorf("%s dispatch = %v, want exactly [\"%s(a/b/)\"]", proto, got, key)
		}
	}

	// Generic (no entry), move, the empty set, a remote row and an
	// unknown key: no dispatch, no panic.
	obs.AfterCopyMove(ctx, repo.OpCopy, "gen-repo", []string{"x/"})
	obs.AfterCopyMove(ctx, repo.OpMove, "npm-repo", []string{"x/"})
	obs.AfterCopyMove(ctx, repo.OpCopy, "npm-repo", nil)
	obs.AfterCopyMove(ctx, repo.OpCopy, "rem-repo", []string{"x/"})
	obs.AfterCopyMove(ctx, repo.OpCopy, "no-such-repo", []string{"x/"})
	for key, f := range fakes {
		if got := f.snapshot(); len(got) != 1 {
			t.Errorf("%s dispatch after the guard legs = %v, want still one call", key, got)
		}
	}
}

// TestT354NpmCopyFlipRealAssembly: the real adapters over a real stack —
// a tarball copied through the real pipeline with the observer attached
// the way newAssembledServer attaches it, and the target's packument
// answering the copied tarball's version (the T-351 L16 flip at the
// assembly level).
func TestT354NpmCopyFlipRealAssembly(t *testing.T) {
	st := openTestStack(t)
	ctx := context.Background()

	// Bare constructors: no adapter.Register, so the process-wide registry
	// (one full assembly per process) stays untouched.
	mavenHandler := maven.New(st.svc, st.md.Repos(), st.md.Blobs(), st.md.Nodes())
	npmHandler := npm.New(st.svc, st.md.Repos(), npm.Options{})
	repo.AttachCopyMoveObserver(st.svc, copyMoveIndexObserver{
		repos: st.md.Repos(),
		reindexers: map[string]dirReindexer{
			maven.Protocol: mavenHandler,
			npm.Protocol:   npmHandler,
		},
		log: discardLogger(),
	})

	for _, key := range []string{"npm-src", "npm-dst"} {
		if err := st.md.Repos().Create(ctx, &metadata.Repo{RepoKey: key, Type: repo.TypeLocal, PackageType: npm.Protocol}); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}

	// A real tarball (manifest inside) landed in the source repository.
	tb := wiredTarball(t, "wired-pkg", "1.0.0")
	admin := &repo.Principal{Name: "admin", Admin: true}
	tbPath := "wired-pkg/-/wired-pkg-1.0.0.tgz"
	if _, err := st.svc.Put(ctx, admin, "npm-src", tbPath,
		bytes.NewReader(tb), wiredRef(t, tb), "application/octet-stream"); err != nil {
		t.Fatalf("seed tarball: %v", err)
	}

	cms, ok := st.svc.(repo.CopyMoveService)
	if !ok {
		t.Fatalf("the stack's service lacks the CopyMoveService face")
	}
	res, err := cms.CopyOrMove(ctx, admin, repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "npm-src", SrcPath: tbPath,
		TargetRepo: "npm-dst", TargetPath: tbPath,
	})
	if err != nil || res.HTTPStatus != http.StatusOK {
		t.Fatalf("copy = (err %v, status %d, messages %d)", err, res.HTTPStatus, len(res.Messages))
	}

	// The observer fires off the request path: poll the target's packument
	// until it answers the copied version with a live latest tag.
	deadline := time.Now().Add(5 * time.Second)
	for {
		rc, _, err := st.svc.Get(ctx, admin, "npm-dst", "wired-pkg/packument.json")
		if err == nil {
			raw, rerr := io.ReadAll(rc)
			_ = rc.Close()
			if rerr != nil {
				t.Fatalf("read packument: %v", rerr)
			}
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatalf("packument not JSON: %v (%s)", err, raw)
			}
			if len(doc["versions"].(map[string]any)) == 0 {
				t.Fatalf("packument landed without the version: %s", raw)
			}
			tags, _ := doc["dist-tags"].(map[string]any)
			if tags["latest"] != "1.0.0" {
				t.Fatalf("latest = %v, want 1.0.0: %s", tags["latest"], raw)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the wired observer never rebuilt the target packument: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ---- local helpers ----

// wiredTarball packs one real npm tarball (package/package.json inside).
func wiredTarball(t *testing.T, name, version string) []byte {
	t.Helper()
	manifest, err := json.Marshal(map[string]any{"name": name, "version": version})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "package/package.json", Mode: 0o644, Size: int64(len(manifest))}); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write(manifest); err != nil {
		t.Fatalf("tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gz close: %v", err)
	}
	return buf.Bytes()
}

// wiredRef declares the seed node's honest digest.
func wiredRef(t *testing.T, b []byte) storage.BlobRef {
	t.Helper()
	s := sha256.Sum256(b)
	return storage.BlobRef{Sha256: hex.EncodeToString(s[:])}
}
