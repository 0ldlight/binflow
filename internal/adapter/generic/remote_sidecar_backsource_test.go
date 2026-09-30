package generic_test

// T-597 / BIN-79 (ledger generic/remote-deploy-refusal-form, arm d): the
// GENERIC face of the remote sidecar retraction. The 200/computed-digest
// arm lives in remote_render_test.go (TestRemoteOutcomesRenderThroughHandler,
// source-HIT pinned); this file pins the fault arms the live A probe
// anchored on the generic face (/tmp/t597 legs g-side-sha1-first /
// g-side-md5-inwin / g-side-head-sha1-inwin): the first upstream fault
// 404-externalizes the retrieval error citing the source path and the
// source's upstream URL; the requests inside the assumed-offline window
// answer the offline form; and the NFR-S13 screening stays on the chain in
// front of all of it (an unexempted private upstream answers the guard's
// 400, never a sidecar-specific refusal).

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// TestRemoteSidecarFaultArms walks one terminal-checksum spelling through
// the fault arms of the retracted sidecar face.
func TestRemoteSidecarFaultArms(t *testing.T) {
	ctx := context.Background()
	dataDir, dbDir := t.TempDir(), t.TempDir()
	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	defer st.Close() //nolint:errcheck // teardown of the harness engine
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dbDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer md.Close() //nolint:errcheck // teardown of the harness store
	clk := &rclock{now: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	svc := repo.NewWithClock(st, md, &allowAll{}, nil, clk.Now)
	h := genericHandler(t, svc, md)

	// The unreachable upstream: port 1 with the NFR-S13 exemption ON, so
	// the chain itself — not the screening — answers.
	if _, err := svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "gen-dead", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
		Config: `{"url":"http://127.0.0.1:1/dead","allowPrivateUpstream":true}`,
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	const src = "t597/a.bin"
	const upURL = "http://127.0.0.1:1/dead/" + src

	// First contact: the retrieval-error form (live-A probe
	// g-side-sha1-first — the generic face's verbatim anchor).
	res := h(t, http.MethodGet, "/binflow/gen-dead/"+src+".sha1")
	if res.Code != http.StatusNotFound {
		t.Fatalf("first sidecar GET = %d, want 404", res.Code)
	}
	first := res.Body.String()
	for _, want := range []string{
		"Error in getting information for '" + src + "'",
		"Failed retrieving resource from " + upURL,
		"Path: 'gen-dead:" + src + "'",
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("first-fault message misses %q: %s", want, first)
		}
	}
	if strings.Contains(first, ".sha1") || strings.Contains(first, "not downloadable") {
		t.Fatalf("first-fault message cites the sidecar face instead of the source: %s", first)
	}

	// Inside the assumed-offline window: the offline form (probe
	// g-side-md5-inwin — a DIFFERENT terminal spelling stays on the same
	// read plane).
	res = h(t, http.MethodGet, "/binflow/gen-dead/"+src+".md5")
	if res.Code != http.StatusNotFound {
		t.Fatalf("in-window sidecar GET = %d, want 404", res.Code)
	}
	inwin := res.Body.String()
	for _, want := range []string{
		"is assumed offline, 'gen-dead:" + src + "' is not found at '" + src + "'",
		"Path: 'gen-dead:" + src + "'",
	} {
		if !strings.Contains(inwin, want) {
			t.Fatalf("in-window message misses %q: %s", want, inwin)
		}
	}
	if strings.Contains(inwin, "Failed retrieving resource from") {
		t.Fatalf("in-window message re-externalizes the retrieval error: %s", inwin)
	}

	// HEAD renders the same face (probe g-side-head-sha1-inwin): same
	// status. The empty-body half of the anchor is wire-level (Go's http
	// server suppresses HEAD bodies after the handler writes them; the
	// ResponseRecorder does not) — it is pinned on the real wire by the
	// T-597 live GATE instead of here.
	res = h(t, http.MethodHead, "/binflow/gen-dead/"+src+".sha1")
	if res.Code != http.StatusNotFound {
		t.Fatalf("in-window sidecar HEAD = %d, want 404", res.Code)
	}
}

// TestRemoteSidecarScreeningStaysInFront pins the NFR-S13 negative: an
// UNEXEMPTED private upstream answers the screening 400 citing the source
// — the retraction removed the sidecar's own refusal, not the chain's
// guard.
func TestRemoteSidecarScreeningStaysInFront(t *testing.T) {
	ctx := context.Background()
	dataDir, dbDir := t.TempDir(), t.TempDir()
	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	defer st.Close() //nolint:errcheck // teardown of the harness engine
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dbDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer md.Close() //nolint:errcheck // teardown of the harness store
	svc := repo.NewWithClock(st, md, &allowAll{}, nil, time.Now)
	h := genericHandler(t, svc, md)

	if _, err := svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "gen-screened", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
		Config: `{"url":"http://127.0.0.1:9/upstream"}`, // loopback, NO exemption
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	const src = "t597/guard.bin"
	res := h(t, http.MethodGet, "/binflow/gen-screened/"+src+".sha1")
	if res.Code != http.StatusBadRequest {
		t.Fatalf("screened sidecar GET = %d, want 400", res.Code)
	}
	body := res.Body.String()
	if !strings.Contains(body, "Cannot fetch 'gen-screened/"+src+"'") {
		t.Fatalf("screening message must cite the SOURCE path: %s", body)
	}
	if strings.Contains(body, ".sha1") {
		t.Fatalf("screening message cites the sidecar spelling: %s", body)
	}
}
