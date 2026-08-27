package replication_test

// T-317 (FR-101.1/101.2/101.3) — the two-instance LIVE assertions on real
// in-process BinFlow stacks (the L38/L39 lanes):
//
//	L38 property sync end to end: tag build=77 on the source AFTER the
//	   artifact landed, re-push through the engine's idempotent-retransmit
//	   path (re-upload same bytes → re-enqueue → HEAD hit → property merge),
//	   then read the property back on the TARGET through the real REST
//	   property face (?properties=build).
//
//	L39 replica isolation (M9 Q5 final ruling = ADR-0025 decision 1 held,
//	   status quo maintained): the replica FACE is the un-routed virtual —
//	   PUT/DELETE answer 405 with the spec's exact wording — while the
//	   backing local stays DIRECTLY WRITABLE (the decision's registered,
//	   accepted limitation, asserted as the status quo it is).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/replication"
	"github.com/lzwzzy/binflow/internal/repo"
)

// timeNow keeps the poll helpers' clock spelled in one place.
func timeNow() time.Time { return time.Now() }

// t317Topology stands up the A→B pair with the push engine running on A:
// B = backing local "replica-local" + un-routed virtual "replica"; A = one
// generic local "libs". The engine carries the REAL metadata seam
// (NewStoreMetaSource) so plane selection AND the property carry run
// in-production shapes.
func t317Topology(t *testing.T) (a, b *binflow, store replication.Store, cfgID int64) {
	t.Helper()
	b = newBinFlow(t, "B317", "pw-target", []*metadata.Repo{
		{RepoKey: "replica-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: "{}"},
		{RepoKey: "replica", Type: repo.TypeVirtual, PackageType: repo.PackageGeneric,
			Config: `{"repositories":["replica-local"]}`},
	})
	a = newBinFlow(t, "A317", "pw-source", []*metadata.Repo{
		{RepoKey: "libs", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: "{}"},
	})
	store = a.replicationStore(t)
	cipher, err := remote.NewCipher(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	encPw, err := cipher.Encrypt("pw-target")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	cfgID, err = store.CreateConfig(context.Background(), &replication.ReplicationConfig{
		Name: "t317-props", SourceRepo: "libs",
		TargetURL: b.url, TargetRepo: "replica-local",
		TargetUsername: "admin", TargetPasswordEnc: encPw,
		Enabled: true, CreatedAt: metadata.Now(), UpdatedAt: metadata.Now(),
	})
	if err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}
	engine, err := replication.NewEngine(store, a.st, replication.EngineOptions{
		Cipher: cipher,
		Audit:  audit.New(a.md, true),
		Meta:   replication.NewStoreMetaSource(a.md),
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	repo.AttachReplicator(a.svc, engine)
	runCtx, cancel := context.WithCancel(context.Background())
	go func() { _ = engine.Run(runCtx) }()
	t.Cleanup(func() {
		cancel()
		engine.CloseIdleConnections()
	})
	return a, b, store, cfgID
}

// waitForTaskCount polls the config's task list until at least n rows are
// terminal-success (each row counted once per poll — the per-row predicate
// helper cannot express "a second one arrived").
func waitForTaskCount(t *testing.T, store replication.Store, cfgID int64, n int, what string) {
	t.Helper()
	deadline := timeNow().Add(20 * time.Second)
	for timeNow().Before(deadline) {
		tasks, err := store.ListTasks(context.Background(), cfgID, 10)
		if err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
		successes := 0
		for _, task := range tasks {
			if task.Status == replication.TaskStatusSuccess {
				successes++
			}
		}
		if successes >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestT317PropertySyncEndToEnd is the L38 probe: 源打标 build=77 → 复制 →
// 目标 ?properties=build 读回 — with the tag landing AFTER the artifact's
// first push, converged by the re-upload's idempotent-hit property merge.
func TestT317PropertySyncEndToEnd(t *testing.T) {
	a, b, store, cfgID := t317Topology(t)
	path := "/binflow/libs/org/app/1.0/app-1.0.bin"
	payload := []byte("the t317 property-sync payload")

	// 1. Land the artifact on A through the real REST surface; the engine
	// pushes it (with or without properties — the tag has not happened).
	resp, _ := a.do(http.MethodPut, path, "admin", "pw-source", payload)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload on A: status %d, want 201", resp.StatusCode)
	}
	waitForTask(t, store, cfgID, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusSuccess
	}, "first push success")

	// 2. Tag build=77 on A through the real property face.
	presp, _ := a.do(http.MethodPut, "/binflow/api/storage/libs/org/app/1.0/app-1.0.bin?properties=build=77",
		"admin", "pw-source", nil)
	if presp.StatusCode != http.StatusNoContent {
		t.Fatalf("tag on A: status %d, want 204", presp.StatusCode)
	}

	// 3. Re-upload the SAME bytes: the repo plane's idempotent retransmit
	// re-fires the enqueue hook, the engine's HEAD answers the same sha256
	// (zero transfer) and the property carry merges build=77 onto the
	// target node.
	resp2, _ := a.do(http.MethodPut, path, "admin", "pw-source", payload)
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("re-upload on A: status %d, want 201 (idempotent retransmit)", resp2.StatusCode)
	}
	waitForTaskCount(t, store, cfgID, 2, "two successful pushes (the second carrying properties)")

	// 4. Read the property back on the TARGET, both through the backing
	// repository and through the read-only replica face.
	for _, prefix := range []string{"/binflow/api/storage/replica-local", "/binflow/api/storage/replica"} {
		gresp, gbody := b.do(http.MethodGet, prefix+"/org/app/1.0/app-1.0.bin?properties=build", "admin", "pw-target", nil)
		if gresp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s?properties=build: status %d, want 200 (body %s)", prefix, gresp.StatusCode, gbody)
		}
		var view struct {
			Properties map[string][]string `json:"properties"`
		}
		if err := json.Unmarshal(gbody, &view); err != nil {
			t.Fatalf("property body %q: %v", gbody, err)
		}
		if vs, ok := view.Properties["build"]; !ok || len(vs) != 1 || vs[0] != "77" {
			t.Fatalf("%s properties = %v, want build=77", prefix, view.Properties)
		}
	}
}

// TestT317ReplicaIsolation is the L39 lane: the ADR-0025 decision-1 posture
// asserted live, every clause — the un-routed virtual face refuses writes
// with the spec's exact wording, the backing local stays directly writable
// (the accepted limitation), and both writes are readable through the face.
func TestT317ReplicaIsolation(t *testing.T) {
	a, b, store, cfgID := t317Topology(t)
	path := "/binflow/libs/repl/img-1.0.bin"
	resp, _ := a.do(http.MethodPut, path, "admin", "pw-source", []byte("replica isolation payload"))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload on A: status %d, want 201", resp.StatusCode)
	}
	waitForTask(t, store, cfgID, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusSuccess
	}, "push success")

	// Clause 1 — the replica FACE serves the replicated artifact read-only.
	gresp, _ := b.do(http.MethodGet, "/binflow/replica/repl/img-1.0.bin", "", "", nil)
	if gresp.StatusCode != http.StatusOK {
		t.Fatalf("GET on B replica face: status %d, want 200", gresp.StatusCode)
	}

	// Clause 2 — writes to the face are refused: 405 with the EXACT spec
	// wording (repo-semantics 8.2, high confidence).
	presp, pbody := b.do(http.MethodPut, "/binflow/replica/repl/other.bin", "admin", "pw-target", []byte("x"))
	if presp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("PUT on B replica face: status %d, want 405 (body %s)", presp.StatusCode, pbody)
	}
	wantWording := "No local repository was configured as local deployment repository for the (replica) virtual repository."
	if !bytes.Contains(pbody, []byte(wantWording)) {
		t.Fatalf("405 body %q does not carry the spec wording %q", pbody, wantWording)
	}
	dresp, _ := b.do(http.MethodDelete, "/binflow/replica/repl/img-1.0.bin", "admin", "pw-target", nil)
	if dresp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE on B replica face: status %d, want 405", dresp.StatusCode)
	}

	// Clause 3 — the backing local remains DIRECTLY writable: ADR-0025
	// decision 1's registered, accepted limitation, asserted as the status
	// quo the final ruling (M9 Q5: 现状维持) keeps.
	direct, _ := b.do(http.MethodPut, "/binflow/replica-local/repl/direct.bin", "admin", "pw-target", []byte("direct write"))
	if direct.StatusCode != http.StatusCreated {
		t.Fatalf("direct PUT on B backing local: status %d, want 201 (the accepted limitation)", direct.StatusCode)
	}

	// Clause 4 — the face aggregates both the replicated and the
	// accepted-limitation writes alike.
	for _, rel := range []string{"repl/img-1.0.bin", "repl/direct.bin"} {
		r, _ := b.do(http.MethodGet, "/binflow/replica/"+rel, "", "", nil)
		if r.StatusCode != http.StatusOK {
			t.Fatalf("GET on B replica face %s: status %d, want 200", rel, r.StatusCode)
		}
	}
}
