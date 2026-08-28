package storage

// Live-MinIO fail-open leg (T-338 / ADR-0040, env-gated like the T-323
// kill -9 leg): a REAL docker-stop outage window over a REAL MinIO — the
// dial-refused posture the mock's injected 403 only stands in for. Enabled by:
//
//	BINFLOW_T338_MINIO_ENDPOINT=http://127.0.0.1:29000
//	BINFLOW_T338_MINIO_ACCESS_KEY=... BINFLOW_T338_MINIO_SECRET_KEY=...
//	BINFLOW_T338_MINIO_BUCKET=t338
//	BINFLOW_T338_MINIO_CONTAINER=<docker container name the test may stop/start>
//
// The container gate is what creates the window; without it the leg skips.
// Drive: steady dual-write → outage (PUT lands disk-only + queue, GET serves
// all three classes from disk, mid-session degradation keeps the disk blob)
// → recovery (drain + diskList×s3Set reconcile, every disk sha present on
// S3) → restart-survival of the queue.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	t338EndpointEnv = "BINFLOW_T338_MINIO_ENDPOINT"
	t338AccessEnv   = "BINFLOW_T338_MINIO_ACCESS_KEY"
	t338SecretEnv   = "BINFLOW_T338_MINIO_SECRET_KEY"
	t338BucketEnv   = "BINFLOW_T338_MINIO_BUCKET"
	t338ContainEnv  = "BINFLOW_T338_MINIO_CONTAINER"
)

// t338Skip reports the leg's availability and skips with instructions.
func t338Skip(t *testing.T) (endpoint, access, secret, bucket, container string) {
	t.Helper()
	endpoint = os.Getenv(t338EndpointEnv)
	access = os.Getenv(t338AccessEnv)
	secret = os.Getenv(t338SecretEnv)
	bucket = os.Getenv(t338BucketEnv)
	container = os.Getenv(t338ContainEnv)
	if endpoint == "" || container == "" {
		t.Skip("live-MinIO fail-open leg disabled (set BINFLOW_T338_MINIO_ENDPOINT/ACCESS_KEY/SECRET_KEY/BUCKET/CONTAINER to enable)")
	}
	return endpoint, access, secret, bucket, container
}

// t338Client builds the raw minio client.
func t338Client(t *testing.T, endpoint, access, secret string) *minio.Client {
	t.Helper()
	host := strings.TrimPrefix(strings.TrimPrefix(endpoint, "http://"), "https://")
	client, err := minio.New(host, &minio.Options{
		Creds:        credentials.NewStaticV4(access, secret, ""),
		Secure:       strings.HasPrefix(endpoint, "https://"),
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		t.Fatalf("minio client for %s: %v", endpoint, err)
	}
	return client
}

// t338StopMinIO docker-stops the container and waits for the endpoint to
// refuse connections (the outage is real only once the port is gone).
func t338StopMinIO(t *testing.T, endpoint, container string) {
	t.Helper()
	if out, err := exec.Command("docker", "stop", container).CombinedOutput(); err != nil {
		t.Fatalf("docker stop %s: %v\n%s", container, err, out)
	}
	host := strings.TrimPrefix(strings.TrimPrefix(endpoint, "http://"), "https://")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", host, 500*time.Millisecond)
		if err != nil {
			return // refused — the window is open
		}
		_ = conn.Close()
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("endpoint still accepting connections after docker stop")
}

// t338StartMinIO docker-starts the container and waits for MinIO to answer.
func t338StartMinIO(t *testing.T, endpoint, access, secret, bucket, container string) *minio.Client {
	t.Helper()
	if out, err := exec.Command("docker", "start", container).CombinedOutput(); err != nil {
		t.Fatalf("docker start %s: %v\n%s", container, err, out)
	}
	client := t338Client(t, endpoint, access, secret)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		ok, err := client.BucketExists(ctx, bucket)
		cancel()
		if err == nil && ok {
			return client
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("MinIO did not come back after docker start")
	return nil
}

// t338WipeBucket removes every object under blobs/ so runs are hermetic.
func t338WipeBucket(t *testing.T, client *minio.Client, bucket string) {
	t.Helper()
	ctx := context.Background()
	var keys []string
	for object := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: "blobs/", Recursive: true}) {
		if object.Err != nil {
			t.Fatalf("list objects: %v", object.Err)
		}
		keys = append(keys, object.Key)
	}
	for _, key := range keys {
		if err := client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{}); err != nil {
			t.Fatalf("remove %s: %v", key, err)
		}
	}
}

// t338S3Shas lists every committed blob object sha in the bucket (the mc-side
// audit: object key blobs/<xx>/<sha>).
func t338S3Shas(t *testing.T, client *minio.Client, bucket string) map[string]struct{} {
	t.Helper()
	out := make(map[string]struct{})
	for object := range client.ListObjects(context.Background(), bucket, minio.ListObjectsOptions{Prefix: "blobs/", Recursive: true}) {
		if object.Err != nil {
			t.Fatalf("list objects: %v", object.Err)
		}
		rest, ok := strings.CutPrefix(object.Key, "blobs/")
		if !ok {
			continue
		}
		parts := strings.Split(rest, "/")
		if len(parts) == 2 && len(parts[1]) == 64 {
			out[parts[1]] = struct{}{}
		}
	}
	return out
}

// t338LiveHarness builds a dual-write MigrationEngine over a temp data dir
// and the live MinIO.
func t338LiveHarness(t *testing.T, client *minio.Client, bucket string) (*MigrationEngine, string, *foEventRecorder) {
	t.Helper()
	root := t.TempDir()
	disk, err := OpenEngine(root, Options{SessionTTL: 24 * time.Hour})
	if err != nil {
		t.Fatalf("open disk engine: %v", err)
	}
	s3e, err := OpenS3EngineWithClient(client, bucket, &S3EngineOptions{Sessions: newMemUploadSessions()})
	if err != nil {
		t.Fatalf("open s3 engine: %v", err)
	}
	me := NewMigrationEngine(disk, s3e, MigrationConfig{Enabled: true, Concurrency: 5}).(*MigrationEngine)
	rec := &foEventRecorder{}
	me.SetReplayEvents(rec.record)
	return me, root, rec
}

// TestFailOpenLiveMinIOOutageWindow is the full-chain leg: the AC-A1/A2/A3,
// C and F anchors against a real stopped MinIO.
func TestFailOpenLiveMinIOOutageWindow(t *testing.T) {
	endpoint, access, secret, bucket, container := t338Skip(t)
	if access == "" || secret == "" || bucket == "" {
		t.Fatal("live leg needs ACCESS_KEY/SECRET_KEY/BUCKET as well")
	}
	client := t338Client(t, endpoint, access, secret)
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil || !exists {
		t.Fatalf("bucket %s not reachable at %s (err=%v)", bucket, endpoint, err)
	}
	t338WipeBucket(t, client, bucket)

	me, root, _ := t338LiveHarness(t, client, bucket)
	defer func() { _ = me.Close() }()

	put := func(content string) BlobRef {
		t.Helper()
		sess, err := me.BeginSession(ctx)
		if err != nil {
			t.Fatalf("begin session: %v", err)
		}
		if _, err := sess.Append(ctx, strings.NewReader(content)); err != nil {
			t.Fatalf("append: %v", err)
		}
		ref, err := sess.Commit(ctx, BlobRef{})
		if err != nil {
			t.Fatalf("commit: %v", err)
		}
		if err := me.ReleaseGCHold(ref.Sha256); err != nil {
			t.Fatalf("release hold: %v", err)
		}
		return ref
	}

	// Class-2 blob: dual-written while healthy (steady state is synchronous
	// dual-write — the anchor AC-A2's second class).
	steadyRef := put("live-steady-dual")
	if _, err := os.Stat(filepath.Join(root, "blobs", steadyRef.Sha256[:2], steadyRef.Sha256)); err != nil {
		t.Fatalf("steady blob not on disk: %v", err)
	}
	if _, err := client.StatObject(ctx, bucket, "blobs/"+steadyRef.Sha256[:2]+"/"+steadyRef.Sha256, minio.StatObjectOptions{}); err != nil {
		t.Fatalf("steady blob not on MinIO: %v", err)
	}
	if st := me.ReplayStats(); st.QueueDepth != 0 || st.WindowOpen {
		t.Fatalf("steady state stats = %+v, want closed window + empty queue", st)
	}

	// Class-1 blob: pre-migration disk-only (planted before the window, S3
	// never saw it — only the reconcile diff can catch it).
	plant := make([]byte, 1024)
	if _, err := rand.Read(plant); err != nil {
		t.Fatal(err)
	}
	plantSum := sha256.Sum256(plant)
	plantSha := hex.EncodeToString(plantSum[:])
	{
		sess, err := me.disk.BeginSession(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sess.Append(ctx, strings.NewReader(string(plant))); err != nil {
			t.Fatal(err)
		}
		ref, err := sess.Commit(ctx, BlobRef{})
		if err != nil {
			t.Fatal(err)
		}
		if ref.Sha256 != plantSha {
			t.Fatalf("plant sha drifted: %s", ref.Sha256)
		}
		_ = me.disk.ReleaseGCHold(ref.Sha256)
	}

	// ------------------------- the outage window -------------------------
	t338StopMinIO(t, endpoint, container)

	// AC-A1: PUTs inside the window all succeed and land on disk; the queue
	// grows once per distinct content.
	windowPayload := func(i int) string {
		return strings.Repeat(fmt.Sprintf("t338-window-%d-", i), 16*1024) // 256 KiB deterministic
	}
	var windowRefs []BlobRef
	for i := 0; i < 3; i++ {
		start := time.Now()
		ref := put(windowPayload(i))
		if d := time.Since(start); d > 30*time.Second {
			t.Errorf("in-window PUT took %v — the first dial pays minio-go retries, later ones must take the fast path", d)
		}
		if _, err := os.Stat(filepath.Join(root, "blobs", ref.Sha256[:2], ref.Sha256)); err != nil {
			t.Fatalf("window blob not on disk (layout blobs/<xx>/<sha>): %v", err)
		}
		windowRefs = append(windowRefs, ref)
	}
	st := me.ReplayStats()
	if st.QueueDepth != 3 {
		t.Errorf("queue depth = %d, want 3", st.QueueDepth)
	}
	if !st.WindowOpen {
		t.Error("window should be open during the outage")
	}
	// Idempotent enqueue: a repeat PUT of the SAME content must not grow
	// the queue (per-sha entry names — the checksum dedup source).
	dup := put(windowPayload(0))
	if dup.Sha256 != windowRefs[0].Sha256 {
		t.Fatalf("same content landed a different sha")
	}
	if got := me.ReplayStats().QueueDepth; got != 3 {
		t.Errorf("queue depth after duplicate in-window PUT = %d, want 3", got)
	}

	// AC-A2: all three classes GET + Stat from disk while MinIO is down.
	for name, sha := range map[string]string{
		"pre-migration disk-only": plantSha,
		"dual-written":            steadyRef.Sha256,
		"in-window #0":            windowRefs[0].Sha256,
	} {
		rc, got, err := me.Open(ctx, sha)
		if err != nil {
			t.Fatalf("GET %s during outage failed (want 200-equivalent): %v", name, err)
		}
		body, rerr := io.ReadAll(rc)
		_ = rc.Close()
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != sha {
			t.Errorf("GET %s returned wrong bytes", name)
		}
		_ = got
		if _, err := me.Stat(ctx, sha); err != nil {
			t.Errorf("Stat %s during outage failed (checksum-deploy path): %v", name, err)
		}
	}

	// AC-A3: a session whose S3 arm dies mid-flight — begin + first append
	// happen while... the outage already covers every arm; the direct
	// mid-session shape is: degrade + commit keeps the disk blob.
	sess, err := me.BeginSession(ctx)
	if err != nil {
		t.Fatalf("begin during outage: %v", err)
	}
	if _, err := sess.Append(ctx, strings.NewReader("mid-session-part-1 ")); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if _, err := sess.Append(ctx, strings.NewReader("part-2")); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	midRef, err := sess.Commit(ctx, BlobRef{})
	if err != nil {
		t.Fatalf("commit during outage (site-3): %v", err)
	}
	_ = me.ReleaseGCHold(midRef.Sha256)
	if _, err := os.Stat(filepath.Join(root, "blobs", midRef.Sha256[:2], midRef.Sha256)); err != nil {
		t.Fatal("mid-session disk blob was destroyed after S3 failure (site-3 regression)")
	}

	// The restart leg (AC-B): the queue must survive an engine teardown with
	// MinIO still down; recovery drains under a fresh engine.
	ac3Depth := me.ReplayStats().QueueDepth
	if err := me.Close(); err != nil {
		t.Fatalf("close during outage: %v", err)
	}

	// --------------------------- recovery --------------------------------
	client = t338StartMinIO(t, endpoint, access, secret, bucket, container)

	disk2, err := OpenEngine(root, Options{SessionTTL: 24 * time.Hour})
	if err != nil {
		t.Fatalf("reopen disk engine: %v", err)
	}
	s3b, err := OpenS3EngineWithClient(client, bucket, &S3EngineOptions{Sessions: newMemUploadSessions()})
	if err != nil {
		t.Fatalf("reopen s3 engine: %v", err)
	}
	me2 := NewMigrationEngine(disk2, s3b, MigrationConfig{Enabled: true, Concurrency: 5}).(*MigrationEngine)
	defer func() { _ = me2.Close() }()
	rec2 := &foEventRecorder{}
	me2.SetReplayEvents(rec2.record)

	if st := me2.ReplayStats(); st.QueueDepth != ac3Depth {
		t.Fatalf("restart watermark = %d, want %d (queue must survive)", st.QueueDepth, ac3Depth)
	}

	// AC-C: drain + reconcile — the drained EVENT is the divergence-closing
	// point (depth 0 alone does not imply convergence: the reconcile diff
	// may still be re-enqueueing blobs the queue never saw).
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if len(rec2.drainedEvents()) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if st := me2.ReplayStats(); st.QueueDepth != 0 || st.WindowOpen {
		t.Fatalf("post-recovery stats = %+v, want drained + closed", st)
	}
	if len(rec2.drainedEvents()) == 0 {
		t.Fatal("drain did not converge within the deadline (no drained event)")
	}

	s3Shas := t338S3Shas(t, client, bucket)
	var diskShas []string
	{
		list, err := listAllBlobs(ctx, disk2)
		if err != nil {
			t.Fatalf("disk list: %v", err)
		}
		diskShas = list
	}
	for _, sha := range diskShas {
		if _, ok := s3Shas[sha]; !ok {
			t.Errorf("disk blob %s missing from MinIO after drain+reconcile (mc-side audit: %d objects vs %d disk blobs)", sha[:12], len(s3Shas), len(diskShas))
		}
	}
	if len(s3Shas) != len(diskShas) {
		t.Errorf("MinIO holds %d blob objects, disk holds %d — the superset invariant broke", len(s3Shas), len(diskShas))
	}

	drained := rec2.drainedEvents()
	if len(drained) == 0 {
		t.Fatal("no drained event after live recovery")
	}
	if last := drained[len(drained)-1]; last.ReconcileMissing != 0 {
		t.Errorf("drained event reconcile_missing = %d, want 0 (mc-side zero-gap)", last.ReconcileMissing)
	}
	// NOTE: no window{close} event is expected HERE — the breaker is
	// process-internal state (ADR-0040 point 1), me died with its window
	// open during the outage, and me2 was born closed. The
	// open→close-within-one-process arc is asserted by the mock leg
	// (TestFailOpenRecoveryDrainAndReconcile).

	// Steady state resumed: a fresh PUT dual-writes synchronously again.
	after := func() BlobRef {
		sess, err := me2.BeginSession(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sess.Append(ctx, strings.NewReader("live-post-recovery")); err != nil {
			t.Fatal(err)
		}
		ref, err := sess.Commit(ctx, BlobRef{})
		if err != nil {
			t.Fatal(err)
		}
		_ = me2.ReleaseGCHold(ref.Sha256)
		return ref
	}()
	if _, err := client.StatObject(ctx, bucket, "blobs/"+after.Sha256[:2]+"/"+after.Sha256, minio.StatObjectOptions{}); err != nil {
		t.Errorf("post-recovery PUT did not reach MinIO synchronously: %v", err)
	}
	if st := me2.ReplayStats(); st.QueueDepth != 0 {
		t.Errorf("post-recovery queue depth = %d, want 0", st.QueueDepth)
	}

	// The completed gate reads clean on an emptied queue.
	if err := CheckCompletedReplayQueue(root); err != nil {
		t.Errorf("completed gate on an emptied queue = %v, want nil", err)
	}
}
