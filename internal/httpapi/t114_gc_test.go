package httpapi_test

// T-114 (T-94 review N1): the apply leg must survive a client disconnect.
// The sweep, the blobs-ledger teardown and the gc.run audit all run on a
// context detached from the connection's lifetime — a hang-up mid-apply
// would otherwise cancel the sweep between deletions and leave the already
// swept blobs behind as permanent phantom ledger rows, with no audit row
// for the partially-effective deletion.
//
// The test parks the apply pass inside a wrapping GC seam (the consumer-side
// GarbageCollector interface), cancels the request while it is parked, and
// asserts the context the engine received stayed live and the run completed:
// file gone, ledger row gone, gc.run audited.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/console"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// t114BlockingGC delegates to the real engine but parks the APPLY pass
// until the test releases it, handing the caller the context the handler
// passed down — the evidence the detachment assertion runs on.
type t114BlockingGC struct {
	inner    httpapi.GarbageCollector
	applyCtx chan context.Context
	proceed  chan struct{}
}

func (g *t114BlockingGC) GC(ctx context.Context, referenced func() (map[string]struct{}, error),
	grace time.Duration, apply bool) ([]string, error) {
	if apply {
		g.applyCtx <- ctx
		<-g.proceed
	}
	return g.inner.GC(ctx, referenced, grace, apply)
}

// TestT114ApplySurvivesClientDisconnect drives an apply whose client hangs
// up while the sweep is parked: the sweep context must not follow the
// disconnect, and the run must still complete (blob deleted, ledger row
// dropped — no phantom — and gc.run recorded).
func TestT114ApplySurvivesClientDisconnect(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()

	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	// One aged orphan: on disk, ledgered, referenced by nothing.
	body := "t114 disconnect orphan payload"
	sum := sha256.Sum256([]byte(body))
	orphan := hex.EncodeToString(sum[:])
	blobPath := filepath.Join(dataDir, "blobs", orphan[:2], orphan)
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o700); err != nil {
		t.Fatalf("mkdir blob shard: %v", err)
	}
	if err := os.WriteFile(blobPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write blob: %v", err)
	}
	past := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(blobPath, past, past); err != nil {
		t.Fatalf("backdate blob mtime: %v", err)
	}
	if err := md.Blobs().Put(ctx, &metadata.Blob{
		Sha256: orphan, Size: int64(len(body)), CreatedAt: metadata.Now(),
	}); err != nil {
		t.Fatalf("blobs put: %v", err)
	}

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	wrap := &t114BlockingGC{
		inner:    st,
		applyCtx: make(chan context.Context, 1),
		proceed:  make(chan struct{}),
	}
	s := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Auth:     authSvc,
		Authz:    authSvc,
		Metadata: md,
		Repos:    md.Repos(),
		GC:       wrap,
		DataDir:  dataDir,
		Console:  console.Handler(),
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		ts.URL+"/binflow/api/v1/system/gc", strings.NewReader(`{"apply":true,"graceHours":0}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.SetBasicAuth(adminUser, adminPass)

	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		resp, err := ts.Client().Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body) //nolint:errcheck // the response is discarded by design
			_ = resp.Body.Close()
		}
		// err != nil is the expected branch: cancel() aborts the exchange.
	}()

	// Any assertion failure from here on must not strand the parked handler
	// (t.Cleanup's ts.Close waits for outstanding requests): the release is
	// idempotent and deferred as well as explicit.
	release := sync.OnceFunc(func() { close(wrap.proceed) })
	defer release()

	// The dry pass ran unblocked; the apply pass is now parked inside the
	// seam holding the context the handler gave it.
	sweepCtx := <-wrap.applyCtx
	cancel()
	// Give the hang-up a beat to propagate into the server's
	// r.Context() — the assertion is precisely that the sweep context
	// did NOT inherit that cancellation.
	time.Sleep(100 * time.Millisecond)
	if err := sweepCtx.Err(); err != nil {
		t.Fatalf("apply sweep context followed the client disconnect: %v", err)
	}
	release()
	<-clientDone

	// The handler may still be finishing (ledger teardown, audit, the
	// doomed response write): poll for the completed end state and only
	// fail when the deadline passes without it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		fileGone := false
		if _, err := os.Stat(blobPath); err == nil {
			fileGone = false
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat orphan: %v", err)
		} else {
			fileGone = true
		}

		rowGone := false
		if _, err := md.Blobs().Get(ctx, orphan); err == nil {
			// Not yet torn down — or the phantom the fix exists to prevent
			// (file gone, row alive); the deadline tells them apart.
			rowGone = false
		} else if !errors.Is(err, metadata.ErrNotFound) {
			t.Fatalf("ledger lookup: %v", err)
		} else {
			rowGone = true
		}

		page, err := audit.New(md, true).Query(ctx, audit.Filter{Action: audit.ActionGCRun, Limit: 10})
		if err != nil {
			t.Fatalf("audit query: %v", err)
		}
		audited := len(page.Events) == 1
		if audited {
			ev := page.Events[0]
			if ev.Actor != adminUser {
				t.Fatalf("gc.run actor = %q, want admin", ev.Actor)
			}
			var detail struct {
				Apply          bool `json:"apply"`
				CandidateCount int  `json:"candidateCount"`
				DeletedCount   int  `json:"deletedCount"`
			}
			if err := json.Unmarshal([]byte(ev.Detail), &detail); err != nil {
				t.Fatalf("gc.run detail: %v\n%s", err, ev.Detail)
			}
			if !detail.Apply || detail.CandidateCount != 1 || detail.DeletedCount != 1 {
				t.Fatalf("gc.run detail = %s, want apply with 1 candidate / 1 deleted", ev.Detail)
			}
		}

		if fileGone && rowGone && audited {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the disconnected apply never completed (fileGone=%v rowGone=%v audited=%v)",
				fileGone, rowGone, audited)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
