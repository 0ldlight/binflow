package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// T-63: the /binflow/api/{npm,pypi} dispatch seam. These tests own the
// SEAM only — routing, gating, spelling preservation — never protocol
// behavior (T-69/T-70). The handlers below are fakes that record what the
// seam handed them.

// t63Handler is a fake protocol handler: it records the method and the
// ESCAPED request spelling it received and answers with its protocol name
// in a header, so tests can tell routing from anything else.
type t63Handler struct {
	proto string

	mu   sync.Mutex
	hits []string // "METHOD ESCAPED#RAWPATH"
}

func (h *t63Handler) Protocol() string    { return h.proto }
func (h *t63Handler) RepoTypes() []string { return []string{"local"} }
func (h *t63Handler) Layout(r *http.Request) (string, string, error) {
	return adapter.Layout(r)
}

func (h *t63Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.hits = append(h.hits, r.Method+" "+r.URL.EscapedPath()+"#"+r.URL.RawPath)
	h.mu.Unlock()
	w.Header().Set("X-T63-Proto", h.proto)
	w.WriteHeader(http.StatusOK)
}

// recorded returns a copy of the hits so far.
func (h *t63Handler) recorded() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.hits))
	copy(out, h.hits)
	return out
}

// seedProtocolRepo writes a repository row whose package_type is an M3
// protocol directly through the metadata store: repo.Service's package-type
// matrix is T-64's surface, and the seam must not depend on it (the router
// only reads the row).
func seedProtocolRepo(t *testing.T, h *harness, key, packageType string) {
	t.Helper()
	if err := h.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeLocal, PackageType: packageType,
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
}

// TestT63UnmountedProtocolStaysE01 pins the seam's OFF state (T-61 risk
// R5): on an assembly without npm/pypi handlers — the default stack, even
// after T-69/T-70 land, since the harness never mounts them — every
// /binflow/api protocol-shaped path keeps answering the E-01/E-26 envelope
// 404 with the not-implemented wording. The E-26 matrix rows flip inside
// the protocol tickets, not here.
func TestT63UnmountedProtocolStaysE01(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{
		"/binflow/api/npm/npm-local/@scope%2fpkg",
		"/binflow/api/pypi/pypi-local/simple/demo-pkg/",
		"/binflow/api/pypi-ui/packages",
		"/binflow/api/maven/npm-local/x",
		"/binflow/api/npm",
	} {
		t.Run(path, func(t *testing.T) {
			resp := h.do(http.MethodGet, path, "", "", nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", resp.StatusCode)
			}
			eb := decodeError(t, resp)
			if !strings.Contains(strings.ToLower(eb.Errors[0].Message), "not implemented") {
				t.Fatalf("message %q is not the E-26 not-implemented wording", eb.Errors[0].Message)
			}
		})
	}
}

// TestT63APIProtocolMountMatrix is the seam's routing matrix on a real
// stack (sqlite metadata, real auth chain, real dispatch) with fake npm and
// pypi handlers mounted.
func TestT63APIProtocolMountMatrix(t *testing.T) {
	npm := &t63Handler{proto: "npm"}
	pypi := &t63Handler{proto: "pypi"}
	h := newHarnessCfg(t, nil, nil, npm, pypi)
	seedProtocolRepo(t, h, "npm-local", "npm")
	seedProtocolRepo(t, h, "pypi-local", "pypi")

	tests := []struct {
		name string

		method string
		path   string
		user   string // "" = anonymous
		// wantStatus is the HTTP status the CLIENT sees.
		wantStatus int
		// wantProto is the fake handler that must have answered ("" = no
		// protocol handler may be hit).
		wantProto string
		// wantHit is the exact "METHOD ESCAPED#RAWPATH" record the handler
		// must hold afterwards ("" = not checked).
		wantHit string
	}{
		{
			name:   "npm mount routes to the npm handler, prefix stripped",
			method: http.MethodGet, path: "/binflow/api/npm/npm-local/@scope%2fpkg",
			wantStatus: http.StatusOK, wantProto: "npm",
			wantHit: "GET /npm-local/@scope%2fpkg#/npm-local/@scope%2fpkg",
		},
		{
			name:   "pypi mount routes to the pypi handler",
			method: http.MethodGet, path: "/binflow/api/pypi/pypi-local/simple/demo-pkg/",
			wantStatus: http.StatusOK, wantProto: "pypi",
			wantHit: "GET /pypi-local/simple/demo-pkg/#/pypi-local/simple/demo-pkg/",
		},
		{
			name:   "npm anonymous write is gated before the handler",
			method: http.MethodPut, path: "/binflow/api/npm/npm-local/@scope%2fpkg",
			wantStatus: http.StatusUnauthorized, wantProto: "",
		},
		{
			name:   "npm authenticated write reaches the handler",
			method: http.MethodPut, path: "/binflow/api/npm/npm-local/@scope%2fpkg",
			user:       adminUser,
			wantStatus: http.StatusOK, wantProto: "npm",
			wantHit: "PUT /npm-local/@scope%2fpkg#/npm-local/@scope%2fpkg",
		},
		{
			name:   "pypi anonymous upload (POST) is gated",
			method: http.MethodPost, path: "/binflow/api/pypi/pypi-local/",
			wantStatus: http.StatusUnauthorized, wantProto: "",
		},
		{
			name:   "look-alike pypi-ui prefix never mounts",
			method: http.MethodGet, path: "/binflow/api/pypi-ui/packages",
			wantStatus: http.StatusNotFound, wantProto: "",
		},
		{
			name:   "unmounted maven keeps the envelope 404",
			method: http.MethodGet, path: "/binflow/api/maven/npm-local/x",
			wantStatus: http.StatusNotFound, wantProto: "",
		},
		{
			name:   "bare /binflow/api/npm keeps the envelope 404",
			method: http.MethodGet, path: "/binflow/api/npm",
			wantStatus: http.StatusNotFound, wantProto: "",
		},
		{
			name:   "unknown repo under a live mount answers the content-plane 404",
			method: http.MethodGet, path: "/binflow/api/npm/no-such-repo/x",
			wantStatus: http.StatusNotFound, wantProto: "",
		},
		{
			// The rewrite preserves dispatch semantics EXACTLY: routing
			// follows the repo row's package type, not the URL's protocol
			// spelling — here a pypi-spelled mount onto an npm repository
			// lands on the npm handler, same as /binflow/npm-local/x.
			name:   "dispatch follows the repository's package type",
			method: http.MethodGet, path: "/binflow/api/pypi/npm-local/x",
			wantStatus: http.StatusOK, wantProto: "npm",
			wantHit: "GET /npm-local/x#/npm-local/x",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			beforeNpm := len(npm.recorded())
			beforePypi := len(pypi.recorded())
			resp := h.do(tc.method, tc.path, tc.user, adminPass, nil, nil)
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusUnauthorized &&
				resp.Header.Get("WWW-Authenticate") != `Basic realm="BinFlow Realm"` {
				t.Fatalf("WWW-Authenticate = %q, want the Basic challenge",
					resp.Header.Get("WWW-Authenticate"))
			}

			var fresh []string
			for _, pair := range []struct {
				h      *t63Handler
				before int
			}{{npm, beforeNpm}, {pypi, beforePypi}} {
				fresh = append(fresh, pair.h.recorded()[pair.before:]...)
			}
			switch {
			case tc.wantProto == "" && len(fresh) != 0:
				t.Fatalf("no protocol handler may be hit; recorded %q", fresh)
			case tc.wantProto != "" && len(fresh) != 1:
				t.Fatalf("exactly one handler hit expected, got %q", fresh)
			}
			if tc.wantProto != "" {
				if got := resp.Header.Get("X-T63-Proto"); got != tc.wantProto {
					t.Fatalf("answered by %q, want %q", got, tc.wantProto)
				}
				if tc.wantHit != "" && fresh[0] != tc.wantHit {
					t.Fatalf("handler record = %q, want %q", fresh[0], tc.wantHit)
				}
			}
			if tc.wantProto == "" && tc.wantStatus == http.StatusNotFound {
				eb := decodeError(t, resp)
				if eb.Errors[0].Status != http.StatusNotFound {
					t.Fatalf("envelope status = %d, want 404", eb.Errors[0].Status)
				}
			}
		})
	}
}

// TestT63ScopedSlashEncodingEquivalence pins the scoped-name spelling rule
// for the npm mount (T-61 AC③): @scope%2f and @scope%2F are EQUIVALENT
// addresses because the seam preserves the escaped spelling VERBATIM — the
// decode to "@scope/name" is the adapter's job (the T-14 EscapedPath chain
// reused as-is). The seam must neither collapse the two spellings into one
// byte form nor re-encode them: each must arrive exactly as the client
// spelled it.
func TestT63ScopedSlashEncodingEquivalence(t *testing.T) {
	npm := &t63Handler{proto: "npm"}
	h := newHarnessCfg(t, nil, nil, npm)
	seedProtocolRepo(t, h, "npm-local", "npm")

	for _, tc := range []struct {
		enc     string // the encoded separator the client spells
		wantRaw string // the escaped spelling the handler must receive
	}{
		{"%2f", "/npm-local/@scope%2fname"},
		{"%2F", "/npm-local/@scope%2Fname"},
	} {
		t.Run(tc.enc, func(t *testing.T) {
			path := "/binflow/api/npm/npm-local/@scope" + tc.enc + "name"
			resp := h.do(http.MethodGet, path, "", "", nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			recs := npm.recorded()
			if len(recs) == 0 {
				t.Fatal("npm handler was not reached")
			}
			got := recs[len(recs)-1]
			want := "GET " + tc.wantRaw + "#" + tc.wantRaw
			if got != want {
				t.Fatalf("handler record = %q, want %q (escaped spelling not preserved verbatim)", got, want)
			}
		})
	}
}

// TestT63SeamEqualsContentPlane asserts the seam's core equivalence: after
// the rewrite, /binflow/api/npm/<repo>/<rest> and /binflow/<repo>/<rest>
// hand the adapter byte-identical requests — one node namespace, two
// entrances (architecture section 5.4.2).
func TestT63SeamEqualsContentPlane(t *testing.T) {
	npm := &t63Handler{proto: "npm"}
	h := newHarnessCfg(t, nil, nil, npm)
	seedProtocolRepo(t, h, "npm-local", "npm")

	for _, path := range []string{
		"/binflow/api/npm/npm-local/@scope%2fpkg/-rev/3",
		"/binflow/npm-local/@scope%2fpkg/-rev/3",
	} {
		resp := h.do(http.MethodGet, path, "", "", nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", path, resp.StatusCode)
		}
	}
	recs := npm.recorded()
	if len(recs) != 2 {
		t.Fatalf("handler recorded %d hits, want 2", len(recs))
	}
	if recs[0] != recs[1] {
		t.Fatalf("mounts disagree on the adapter's request: %q vs %q", recs[0], recs[1])
	}
}
