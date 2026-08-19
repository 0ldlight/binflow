package repo_test

// T-64's PutLandedBlob suite (architecture section 11.13's debt closure):
// the use case docker's upload finalize and the future maven/npm chunked
// uploads land their metadata through — blobs-ledger row plus node from an
// already-committed BlobRef, no O(size) re-read of the bytes.

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// landRawBlob commits content through a BARE storage session: the physical
// blob exists with the session's full digest triple, but NO service-written
// rows do — exactly the state a finished docker upload session leaves
// between Commit and the registration call.
func landRawBlob(t TB, e *env, content string) storage.BlobRef {
	t.Helper()
	ctx := context.Background()
	sess, err := e.st.BeginSession(ctx)
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if _, err := sess.Append(ctx, strings.NewReader(content)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	ref, err := sess.Commit(ctx, storage.BlobRef{})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return ref
}

// sha1Of and md5Of compute the ancillary digests assertions compare against.
func sha1Of(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func md5Of(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// TestPutLandedBlobFreshLand: the happy path — a committed session ref lands
// its ledger row (with the session's sha1/md5) and its node, the download
// path serves the bytes, and the audit records the landedBlob marker.
func TestPutLandedBlobFreshLand(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateTypedRepo(t, e, "docker-local", repo.PackageDocker)

	body := "landed layer bytes"
	ref := landRawBlob(t, e, body)
	if ref.Sha1 == "" || ref.Md5 == "" {
		t.Fatalf("session ref lacks ancillary digests: %+v", ref)
	}

	n, err := e.svc.PutLandedBlob(ctx, admin(), "docker-local",
		"acme/app/blobs/"+ref.Sha256, ref, "application/octet-stream")
	if err != nil {
		t.Fatalf("PutLandedBlob: %v", err)
	}
	if n.Sha256 != ref.Sha256 || n.Size != int64(len(body)) || n.Mime != "application/octet-stream" {
		t.Fatalf("node = %+v", n)
	}
	if n.CreatedBy != "admin" {
		t.Fatalf("createdBy = %q", n.CreatedBy)
	}

	// The ledger row THIS method created carries the session's digests.
	row, err := e.md.Blobs().Get(ctx, ref.Sha256)
	if err != nil {
		t.Fatalf("ledger row: %v", err)
	}
	if row.Sha1 != ref.Sha1 || row.Md5 != ref.Md5 || row.Size != int64(len(body)) {
		t.Fatalf("ledger row = %+v, want the session triple %+v", row, ref)
	}
	if row.Sha1 != sha1Of(body) || row.Md5 != md5Of(body) {
		t.Fatal("session digests do not match the content (harness sanity)")
	}

	// The node serves the committed bytes end to end.
	rc, got, err := e.svc.Get(ctx, admin(), "docker-local", "acme/app/blobs/"+ref.Sha256)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close() //nolint:errcheck // read-side close error is irrelevant
	b, _ := io.ReadAll(rc)
	if string(b) != body || got.Sha256 != ref.Sha256 {
		t.Fatalf("served %q (%s), want the landed bytes", b, got.Sha256)
	}

	found := false
	for _, ev := range eventsOf(e.au) {
		if ev.Action == repo.AuditActionDeploy && ev.Repo == "docker-local" &&
			strings.Contains(ev.Detail, `"landedBlob":true`) {
			found = true
		}
	}
	if !found {
		t.Fatal("no deploy audit event with the landedBlob marker")
	}
}

// TestPutLandedBlobVsPutFromBlobOrphan: THE contract delta of section 11.13.
// The same no-ledger state PutFromBlob must refuse (ErrOrphanBlob — it
// refuses to freeze an incomplete digest record) is exactly the state
// PutLandedBlob exists to serve: the caller owns a completed session and its
// digest triple, so the row is CREATED, not scavenged.
func TestPutLandedBlobVsPutFromBlobOrphan(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateTypedRepo(t, e, "docker-local", repo.PackageDocker)

	ref := landRawBlob(t, e, "fresh push bytes")
	if _, err := e.svc.PutFromBlob(ctx, admin(), "docker-local",
		"a/blobs/"+ref.Sha256, ref, ""); !errors.Is(err, repo.ErrOrphanBlob) {
		t.Fatalf("PutFromBlob(fresh session) error = %v, want ErrOrphanBlob", err)
	}
	if _, err := e.svc.PutLandedBlob(ctx, admin(), "docker-local",
		"a/blobs/"+ref.Sha256, ref, ""); err != nil {
		t.Fatalf("PutLandedBlob(fresh session): %v", err)
	}
	if _, err := e.md.Blobs().Get(ctx, ref.Sha256); err != nil {
		t.Fatalf("ledger row after landed put: %v", err)
	}
}

// TestPutLandedBlobExistingLedgerKept: a blob the generic plane already
// recorded keeps ITS ledger row (Blobs.Put is DO-NOTHING on conflict — the
// row is the digest authority); the new node lands beside it with the
// file's own size.
func TestPutLandedBlobExistingLedgerKept(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateTypedRepo(t, e, "docker-local", repo.PackageDocker)

	src := put(t, e, admin(), "docker-local", "seed/generic.bin", "shared content bytes")
	seedRow, err := e.md.Blobs().Get(ctx, src.Sha256)
	if err != nil {
		t.Fatalf("seed ledger row: %v", err)
	}
	ref := landRawBlob(t, e, "shared content bytes") // dedup: same physical blob
	if ref.Sha256 != src.Sha256 {
		t.Fatalf("dedup sanity: %s != %s", ref.Sha256, src.Sha256)
	}

	n, err := e.svc.PutLandedBlob(ctx, admin(), "docker-local",
		"app/blobs/"+ref.Sha256, ref, "")
	if err != nil {
		t.Fatalf("PutLandedBlob: %v", err)
	}
	row, err := e.md.Blobs().Get(ctx, ref.Sha256)
	if err != nil {
		t.Fatalf("ledger row: %v", err)
	}
	if row.Sha1 != seedRow.Sha1 || row.Md5 != seedRow.Md5 {
		t.Fatalf("ledger row rewritten: %+v (generic seed had %s/%s)", row, seedRow.Sha1, seedRow.Md5)
	}
	if n.Size != src.Size {
		t.Fatalf("node size = %d, want the file's %d", n.Size, src.Size)
	}
}

// TestPutLandedBlobValidation: the refusal ladder, table-driven. Every
// branch must leave no node behind.
func TestPutLandedBlobValidation(t *testing.T) {
	seed := func(t *testing.T) (*env, storage.BlobRef) {
		t.Helper()
		e := newEnv(t)
		mustCreateTypedRepo(t, e, "docker-local", repo.PackageDocker)
		mustCreateRepo(t, e, "generic-local")
		if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
			RepoKey: "generic-remote", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
			Config: `{"url":"http://127.0.0.1:9099"}`,
		}); err != nil {
			t.Fatalf("seed remote: %v", err)
		}
		return e, landRawBlob(t, e, "validation body")
	}
	tests := []struct {
		name    string
		p       *repo.Principal
		repoKey string
		path    string
		ref     func(ref storage.BlobRef) storage.BlobRef
		want    error
		// plain marks the "any non-sentinel failure" expectation (the
		// internal-inconsistency branch: a plain error, NOT ErrNodeNotFound,
		// which would mask a store fault as a client-addressable 404).
		plain bool
	}{
		{"anonymous is refused", nil, "docker-local", "a/blobs/x", func(r storage.BlobRef) storage.BlobRef { return r }, repo.ErrUnauthorized, false},
		{"folder path", admin(), "docker-local", "a/blobs/", func(r storage.BlobRef) storage.BlobRef { return r }, repo.ErrInvalidPath, false},
		{"dot segment", admin(), "docker-local", "a/../blobs/x", func(r storage.BlobRef) storage.BlobRef { return r }, repo.ErrInvalidPath, false},
		{"empty path", admin(), "docker-local", "", func(r storage.BlobRef) storage.BlobRef { return r }, repo.ErrInvalidPath, false},
		{"missing sha256", admin(), "docker-local", "a/blobs/x", func(r storage.BlobRef) storage.BlobRef { r.Sha256 = ""; return r }, repo.ErrInvalidPath, false},
		{"unknown repository", admin(), "no-such", "a/blobs/x", func(r storage.BlobRef) storage.BlobRef { return r }, repo.ErrRepoNotFound, false},
		{"remote repository", admin(), "generic-remote", "a/blobs/x", func(r storage.BlobRef) storage.BlobRef { return r }, repo.ErrRepoTypeNotSupported, false},
		{"generic local is fine (any local serves)", admin(), "generic-local", "a/blobs/x", func(r storage.BlobRef) storage.BlobRef { return r }, nil, false},
		{"physical blob missing", admin(), "docker-local", "a/blobs/x", func(r storage.BlobRef) storage.BlobRef { r.Sha256 = shaOf("never committed"); return r }, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, ref := seed(t)
			r := tt.ref(ref)
			_, err := e.svc.PutLandedBlob(context.Background(), tt.p, tt.repoKey, tt.path, r, "")
			if tt.plain {
				if err == nil || errors.Is(err, repo.ErrNodeNotFound) {
					t.Fatalf("error = %v, want a plain non-404 failure", err)
				}
				return
			}
			if tt.want == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestPutLandedBlobPermissionPair: the same pair as Put — idempotent
// re-land skips the gates, overwrite needs delete, fresh land needs write.
func TestPutLandedBlobPermissionPair(t *testing.T) {
	ctx := context.Background()

	t.Run("fresh land needs write", func(t *testing.T) {
		e := newEnv(t)
		mustCreateTypedRepo(t, e, "docker-local", repo.PackageDocker)
		ref := landRawBlob(t, e, "perm body")
		if _, err := e.svc.PutLandedBlob(ctx, alice(), "docker-local",
			"app/blobs/"+ref.Sha256, ref, ""); !errors.Is(err, repo.ErrForbidden) {
			t.Fatalf("error = %v, want ErrForbidden", err)
		}
	})

	t.Run("granted write lands", func(t *testing.T) {
		e := newEnv(t)
		mustCreateTypedRepo(t, e, "docker-local", repo.PackageDocker)
		e.az.add("alice", repo.ActionWrite, "app/")
		ref := landRawBlob(t, e, "perm body")
		if _, err := e.svc.PutLandedBlob(ctx, alice(), "docker-local",
			"app/blobs/"+ref.Sha256, ref, ""); err != nil {
			t.Fatalf("PutLandedBlob: %v", err)
		}
	})

	t.Run("idempotent re-land skips the gates", func(t *testing.T) {
		e := newEnv(t)
		mustCreateTypedRepo(t, e, "docker-local", repo.PackageDocker)
		ref := landRawBlob(t, e, "perm body")
		if _, err := e.svc.PutLandedBlob(ctx, admin(), "docker-local",
			"app/blobs/"+ref.Sha256, ref, ""); err != nil {
			t.Fatalf("seed land: %v", err)
		}
		// alice holds NOTHING — the same-digest re-land still succeeds.
		if _, err := e.svc.PutLandedBlob(ctx, alice(), "docker-local",
			"app/blobs/"+ref.Sha256, ref, ""); err != nil {
			t.Fatalf("idempotent re-land: %v", err)
		}
	})

	t.Run("overwrite of a different digest needs delete", func(t *testing.T) {
		e := newEnv(t)
		mustCreateTypedRepo(t, e, "docker-local", repo.PackageDocker)
		old := landRawBlob(t, e, "old bytes")
		newer := landRawBlob(t, e, "new bytes")
		path := "app/blobs/x"
		if _, err := e.svc.PutLandedBlob(ctx, admin(), "docker-local", path, old, ""); err != nil {
			t.Fatalf("seed land: %v", err)
		}
		e.az.add("alice", repo.ActionWrite, "app/")
		if _, err := e.svc.PutLandedBlob(ctx, alice(), "docker-local", path, newer, ""); !errors.Is(err, repo.ErrForbidden) {
			t.Fatalf("overwrite without delete error = %v, want ErrForbidden", err)
		}
		e.az.add("alice", repo.ActionDelete, "app/")
		if _, err := e.svc.PutLandedBlob(ctx, alice(), "docker-local", path, newer, ""); err != nil {
			t.Fatalf("overwrite with delete: %v", err)
		}
	})
}

// TestPutLandedBlobNeverReadsTheBytes: the section 11.13 debt closure pinned
// mechanically. failReadEngine makes every committed blob UNREADABLE while
// keeping Open's ref (size) honest: PutLandedBlob — which takes only the
// O(1) open for presence/size — succeeds, while the pre-T-64 workaround it
// deletes (streaming the opened blob back through Put) had to READ the bytes
// and fails on the same engine.
func TestPutLandedBlobNeverReadsTheBytes(t *testing.T) {
	ctx := context.Background()
	dir, dbDir := t.TempDir(), t.TempDir()
	eng, err := storage.OpenEngine(dir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dbDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })
	svc := repo.NewWithClock(failReadEngine{eng}, md, nil, nil,
		func() time.Time { return time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC) })

	mustCreateTypedRepo(t, &env{svc: svc, st: eng, md: md}, "docker-local", repo.PackageDocker)

	// The finalize state: a session committed against the RAW engine.
	sess, err := eng.BeginSession(ctx)
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if _, err := sess.Append(ctx, strings.NewReader("never to be re-read")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	ref, err := sess.Commit(ctx, storage.BlobRef{})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// The landed registration succeeds although the blob cannot be read.
	n, err := svc.PutLandedBlob(ctx, admin(), "docker-local",
		"acme/app/blobs/"+ref.Sha256, ref, "")
	if err != nil {
		t.Fatalf("PutLandedBlob on an unreadable blob: %v", err)
	}
	if n.Size != int64(len("never to be re-read")) {
		t.Fatalf("node size = %d", n.Size)
	}

	// Contrast pin: the read-back workaround this use case deletes. The old
	// flow opened the landed blob THROUGH THE SAME ENGINE the service sees
	// and fed the reader to Put — which must read it, and fails here.
	wrapped := failReadEngine{eng}
	rc, rref, err := wrapped.Open(ctx, ref.Sha256)
	if err != nil {
		t.Fatalf("wrapped Open: %v", err)
	}
	defer rc.Close() //nolint:errcheck // read-side close error is irrelevant
	if _, err := svc.Put(ctx, admin(), "docker-local",
		"other/img/blobs/"+ref.Sha256, rc, rref, ""); err == nil {
		t.Fatal("the streaming workaround should have needed the bytes (and failed)")
	}
}

// failReadEngine hands out readers whose Read always fails while Open's
// ref (size) stays honest — the probe that separates "opens the file" from
// "reads the file".
type failReadEngine struct {
	storage.Engine
}

type failReadCloser struct{ io.ReadSeekCloser }

func (failReadCloser) Read([]byte) (int, error) { return 0, errors.New("blob read attempted") }

func (e failReadEngine) Open(ctx context.Context, sha string) (io.ReadSeekCloser, storage.BlobRef, error) {
	rc, ref, err := e.Engine.Open(ctx, sha)
	if err != nil {
		return nil, ref, err
	}
	return failReadCloser{rc}, ref, nil
}

// TestPutLandedBlobConcurrentSameDigest: parallel finalize-style lands of
// the same digest (the retry-after-503 pattern) stay race-clean and converge
// on one node.
func TestPutLandedBlobConcurrentSameDigest(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateTypedRepo(t, e, "docker-local", repo.PackageDocker)
	ref := landRawBlob(t, e, "raced layer bytes")
	path := "acme/app/blobs/" + ref.Sha256

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.svc.PutLandedBlob(ctx, admin(), "docker-local", path, ref, ""); err != nil {
				t.Errorf("concurrent PutLandedBlob: %v", err)
			}
		}()
	}
	wg.Wait()

	nodes, err := e.md.Nodes().ListByPrefix(ctx, "docker-local", "acme/app")
	if err != nil {
		t.Fatalf("ListByPrefix: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Sha256 != ref.Sha256 {
		t.Fatalf("nodes after race = %+v, want exactly one at the digest", nodes)
	}
}
