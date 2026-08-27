package replication_test

// T-317 (FR-101.2) — the push engine's property carry: after a generic-plane
// push succeeds (fresh transfer OR idempotent hit), the source node's
// properties — read at PUSH time — merge onto the target node through the
// target instance's property face ({target}/binflow/api/storage/{repo}/{path}
// ?properties=…, the M10 property plane's comma grammar). A property the
// operator tagged after the first transfer converges on the next push of the
// same content (the idempotent-retransmit → re-enqueue → idempotent-hit →
// property-merge loop the two-instance test drives end to end).

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/replication"
)

// propsTarget is a path-aware scripted BinFlow target: the content mount
// answers HEAD/PUT like scriptTarget, and the /binflow/api/storage property
// arm records every property write (path, RAW query, auth) and answers
// propsStatus (204 by default — the property plane's success).
type propsTarget struct {
	mu sync.Mutex
	// headStatus/headSum script the existence probe (0 = 404; 200 with
	// headSum scripts the idempotent hit).
	headStatus int
	headSum    string
	// propsStatus answers the property PUT (0 = 204).
	propsStatus int
	// contentStatus answers the content PUT (0 = 201).
	contentStatus int

	reqs []string // "METHOD <path>?<rawquery>" in arrival order
}

func (p *propsTarget) handler(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	q := r.URL.RawQuery
	if q != "" {
		q = "?" + q
	}
	p.reqs = append(p.reqs, r.Method+" "+r.URL.Path+q)
	switch {
	case strings.HasPrefix(r.URL.Path, "/binflow/api/storage/"):
		status := p.propsStatus
		if status == 0 {
			status = http.StatusNoContent
		}
		w.WriteHeader(status)
	case r.Method == http.MethodHead:
		status := p.headStatus
		if status == 0 {
			status = http.StatusNotFound
		}
		if status == http.StatusOK {
			w.Header().Set("X-Checksum-Sha256", p.headSum)
		}
		w.WriteHeader(status)
	case r.Method == http.MethodPut:
		_, _ = io.Copy(io.Discard, r.Body)
		status := p.contentStatus
		if status == 0 {
			status = http.StatusCreated
		}
		w.Header().Set("X-Checksum-Sha256", r.Header.Get("X-Checksum-Sha256"))
		w.WriteHeader(status)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (p *propsTarget) requests() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.reqs...)
}

// newPropsFixture wires one engine over the real 009 store, a propsTarget
// and a fakeMeta carrying props for the source path.
func newPropsFixture(t *testing.T, target *propsTarget, props map[string][]string) (*engineFixture, *propsTarget) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(target.handler))
	t.Cleanup(server.Close)
	_, store := openStore(t)
	meta := &fakeMeta{
		pkg:   map[string]string{"libs-local": "generic"},
		props: map[string]map[string][]string{"libs-local/org/app-1.0.bin": props},
	}
	sleep := &sleepRecorder{}
	eng, err := replication.NewEngine(store, &fakeBlobs{content: map[string]string{
		testSHA256(testPayload): testPayload,
	}}, replication.EngineOptions{Sleep: sleep.sleep, Meta: meta})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	cfg := fixtureConfig("dr-props")
	cfg.TargetURL = server.URL
	cfg.SourceRepo = "libs-local"
	cfg.TargetRepo = "mirror"
	cfg.TargetPasswordEnc = "" // anonymous target: no cipher in this fixture
	if _, err := store.CreateConfig(t.Context(), cfg); err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}
	runCtx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = eng.Run(runCtx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("engine Run did not return after cancel")
		}
		eng.CloseIdleConnections()
	})
	f := &engineFixture{ctx: t.Context(), cancel: cancel, store: store, blobs: nil, target: nil,
		server: server, sleep: sleep, audit: nil, engine: eng}
	return f, target
}

// enqueueAndWait pushes one (path, sha) through the engine and waits for a
// terminal task, returning it.
func enqueueAndWait(t *testing.T, f *engineFixture, path string) *replication.ReplicationTask {
	t.Helper()
	sum := testSHA256(testPayload)
	f.engine.Enqueue(f.ctx, "libs-local", path, sum)
	return f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusSuccess ||
			(task.Status == replication.TaskStatusFailed && task.Attempts >= 6)
	}, "terminal")
}

// TestT317PropertyCarryFreshPush: a fresh push (HEAD 404 → PUT 201) carries
// the source properties — the third request on the wire is the property
// merge, with the exact RAW query the M10 grammar demands.
func TestT317PropertyCarryFreshPush(t *testing.T) {
	f, target := newPropsFixture(t, &propsTarget{}, map[string][]string{"build": {"77"}})
	task := enqueueAndWait(t, f, "org/app-1.0.bin")
	if task.Status != replication.TaskStatusSuccess {
		t.Fatalf("task = %s (%s), want success", task.Status, task.LastError)
	}
	reqs := target.requests()
	want := []string{
		"HEAD /binflow/mirror/org/app-1.0.bin",
		"PUT /binflow/mirror/org/app-1.0.bin",
		"PUT /binflow/api/storage/mirror/org/app-1.0.bin?properties=build=77",
	}
	if fmt.Sprint(reqs) != fmt.Sprint(want) {
		t.Fatalf("target requests =\n%v\nwant\n%v", reqs, want)
	}
}

// TestT317PropertyCarryIdempotentHit: when the target already holds the
// bytes (HEAD 200 same sha), the transfer is skipped but the property merge
// STILL runs — the late-tag convergence contract.
func TestT317PropertyCarryIdempotentHit(t *testing.T) {
	f, target := newPropsFixture(t, &propsTarget{
		headStatus: http.StatusOK, headSum: testSHA256(testPayload),
	}, map[string][]string{"build": {"77"}})
	task := enqueueAndWait(t, f, "org/app-1.0.bin")
	if task.Status != replication.TaskStatusSuccess {
		t.Fatalf("task = %s (%s), want success", task.Status, task.LastError)
	}
	reqs := target.requests()
	if len(reqs) != 2 || reqs[0] != "HEAD /binflow/mirror/org/app-1.0.bin" ||
		reqs[1] != "PUT /binflow/api/storage/mirror/org/app-1.0.bin?properties=build=77" {
		t.Fatalf("target requests = %v, want HEAD + property merge only", reqs)
	}
}

// TestT317PropertyCarrySkippedWhenEmpty: a source node without properties
// issues NO property request (the plain T-162 wire shape is unchanged).
func TestT317PropertyCarrySkippedWhenEmpty(t *testing.T) {
	f, target := newPropsFixture(t, &propsTarget{}, nil)
	task := enqueueAndWait(t, f, "org/app-1.0.bin")
	if task.Status != replication.TaskStatusSuccess {
		t.Fatalf("task = %s (%s), want success", task.Status, task.LastError)
	}
	reqs := target.requests()
	if len(reqs) != 2 {
		t.Fatalf("target requests = %v, want HEAD + PUT only", reqs)
	}
}

// TestT317PropertyQueryGrammar: the RAW query renders the comma grammar —
// multi-value as continuation segments, sorted keys and values for
// determinism, and reserved characters percent-encoded so they survive as
// content (%2C stays a value comma, not a separator).
func TestT317PropertyQueryGrammar(t *testing.T) {
	tests := []struct {
		name  string
		props map[string][]string
		query string
	}{
		{"single", map[string][]string{"build": {"77"}}, "properties=build=77"},
		{"multi value", map[string][]string{"build": {"78", "77"}}, "properties=build=77,78"},
		{"multi key sorted", map[string][]string{"zeta": {"1"}, "alpha": {"2"}}, "properties=alpha=2,zeta=1"},
		{"comma in value", map[string][]string{"note": {"a,b"}}, "properties=note=a%2Cb"},
		{"equals in value", map[string][]string{"note": {"x=y"}}, "properties=note=x%3Dy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, target := newPropsFixture(t, &propsTarget{headStatus: http.StatusOK, headSum: testSHA256(testPayload)}, tt.props)
			task := enqueueAndWait(t, f, "org/app-1.0.bin")
			if task.Status != replication.TaskStatusSuccess {
				t.Fatalf("task = %s (%s), want success", task.Status, task.LastError)
			}
			reqs := target.requests()
			if len(reqs) != 2 || !strings.HasSuffix(reqs[1], "?"+tt.query) {
				t.Fatalf("target requests = %v, want the property merge ending ?%s", reqs, tt.query)
			}
		})
	}
}

// TestT317PropertyCarryFailures: the property arm owns the failure classes
// — a 400 is deterministic (terminal, not-retryable marker, no backoff
// burn), a 500 is transient (the full attempt schedule, retryable wording).
func TestT317PropertyCarryFailures(t *testing.T) {
	tests := []struct {
		name        string
		propsStatus int
		wantClass   string
		wantSubstr  string
	}{
		{"400 terminal", http.StatusBadRequest, "terminal", "not retryable"},
		{"409 terminal", http.StatusConflict, "terminal", "not retryable"},
		{"500 retryable", http.StatusInternalServerError, "retryable", "property carry"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := newPropsFixture(t, &propsTarget{propsStatus: tt.propsStatus},
				map[string][]string{"build": {"77"}})
			sum := testSHA256(testPayload)
			f.engine.Enqueue(f.ctx, "libs-local", "org/app-1.0.bin", sum)
			task := f.waitTask(t, func(task *replication.ReplicationTask) bool {
				return task.Status == replication.TaskStatusFailed && task.Attempts >= 6
			}, "terminal failure")
			if !strings.Contains(task.LastError, tt.wantSubstr) {
				t.Fatalf("LastError %q does not contain %q", task.LastError, tt.wantSubstr)
			}
			if tt.wantClass == "terminal" && !strings.Contains(task.LastError, "not retryable") {
				t.Fatalf("LastError %q lacks the not-retryable marker", task.LastError)
			}
		})
	}
}
