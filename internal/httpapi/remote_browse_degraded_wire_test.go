package httpapi_test

// D-T461-1 (T-461's contract-drift close-out, wire leg over T-448's §5-2
// seam): the remote-browse layer's degradation note on the storage wire.
// T-448 built the note into RemoteBrowseListing and left Service.List
// dropping it — every storage face rendered the cached rows with no field
// saying the remote layer went quiet, so the console tree (which consumes an
// assumed remoteDegraded, absent = no rendering) could never light up. These
// tests pin the repaired contract end to end through the real HTTP stack:
//
//   - an ENGAGED layer over a HEALTHY upstream stays field-absent on all
//     three faces (FolderInfo root, FolderInfo folder, ?list) while the
//     derived rows ride the wire;
//   - an upstream fault keeps the CACHED rows and lands the note on all
//     three faces — first the 500-probe spelling, then the assumed-offline
//     silence spelling (remote-browsing.md §4-1/§4-2);
//   - a flag-off remote over the same dead upstream stays field-absent with
//     zero upstream contact (the off posture is structural, diff zero).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
)

// degradedUpstream is a flippable helm classic upstream: index.yaml at the
// root, a deterministic .tgz body, and a broken switch answering 500 to
// everything — the browseFetch offline family, which opens the
// assumed-offline window exactly like a dead upstream would.
type degradedUpstream struct {
	srv    *httptest.Server
	broken *atomic.Bool
	hits   *atomic.Int64
}

func newDegradedUpstream(t *testing.T) *degradedUpstream {
	t.Helper()
	u := &degradedUpstream{broken: &atomic.Bool{}, hits: &atomic.Int64{}}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.hits.Add(1)
		if u.broken.Load() {
			http.Error(w, "upstream maintenance", http.StatusInternalServerError)
			return
		}
		switch {
		case r.URL.Path == "/index.yaml":
			_, _ = w.Write([]byte(`apiVersion: v1
entries:
  solo:
  - name: solo
    version: "0.2.0"
    urls:
    - solo-0.2.0.tgz
  deep:
  - name: deep
    version: "1.0.0"
    urls:
    - deep/nested/deep-1.0.0.tgz
`))
		case strings.HasSuffix(r.URL.Path, ".tgz"):
			_, _ = w.Write([]byte("body:" + strings.TrimPrefix(r.URL.Path, "/")))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(u.srv.Close)
	return u
}

// putDegradedRepo creates one remote helm repository on the upstream through
// the REST plane (the loopback egress exemption rides along, ADR-0012).
func putDegradedRepo(t *testing.T, h *harness, key, url string, flagOn bool) {
	t.Helper()
	cfg := `{"rclass":"remote","packageType":"helm","url":"` + url + `","allowPrivateUpstream":true`
	if flagOn {
		cfg += `,"listRemoteFolderItems":true`
	}
	if status, body := putRepoStatus(t, h, key, cfg+`}`); status != http.StatusOK {
		t.Fatalf("create %s: status=%d body=%s", key, status, body)
	}
}

// pullThrough lands one cached row through the service's content plane (a
// plain Get is the whole pull-through chain — the folder rows a deep path
// needs land with it). The harness mounts no helm adapter, so the seeding
// rides the service seam exactly like internal/repo's T-448 fixture; the
// assertions under test stay on the HTTP wire.
func pullThrough(t *testing.T, h *harness, repoKey, path string) {
	t.Helper()
	rc, _, err := h.svc.Get(context.Background(), &auth.Principal{Name: adminUser, Admin: true}, repoKey, path)
	if err != nil {
		t.Fatalf("pull %s/%s: %v", repoKey, path, err)
	}
	_, _ = io.Copy(io.Discard, rc) //nolint:errcheck // the pull's body is discarded by design
	_ = rc.Close()
}

// storageJSON fetches one storage-face body as decoded JSON.
func storageJSON(t *testing.T, h *harness, pathAndQuery string) map[string]any {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/storage/"+pathAndQuery, adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status=%d body=%s", pathAndQuery, resp.StatusCode, body)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("GET %s body %q: %v", pathAndQuery, body, err)
	}
	return m
}

// childNames flattens a FolderInfo children array into its bare names.
func childNames(m map[string]any) map[string]bool {
	out := map[string]bool{}
	kids, _ := m["children"].([]any)
	for _, k := range kids {
		if c, ok := k.(map[string]any); ok {
			if uri, _ := c["uri"].(string); uri != "" {
				out[strings.TrimPrefix(uri, "/")] = true
			}
		}
	}
	return out
}

// notePrefix is the note's fixed head — the engine's one spelling for every
// degradation cause (internal/remote browse.go).
const notePrefix = "remote enumeration unavailable"

// TestRemoteDegradedNoteOnDegradedStorageWire: the fault leg. An engaged
// layer over a dead upstream keeps the cached rows on every face and carries
// the note — first the 500 answer, then the assumed-offline silence (the
// second and third faces ride the window the first probe opened; §4-2's
// "at most one probe per silence period").
func TestRemoteDegradedNoteOnDegradedStorageWire(t *testing.T) {
	h := newBrowseFlagHarness(t)
	up := newDegradedUpstream(t)
	putDegradedRepo(t, h, "browse-degraded", up.srv.URL, true)
	// Cached rows while the upstream is still alive: a root file and a deep
	// one (the deep pull also lands the deep/ and deep/nested/ folder rows).
	// No listing happens here — the browse snapshot must stay cold so the
	// degraded leg measures a real probe, not a warm TTL window.
	pullThrough(t, h, "browse-degraded", "solo-0.2.0.tgz")
	pullThrough(t, h, "browse-degraded", "deep/nested/deep-1.0.0.tgz")
	hitsAfterPulls := up.hits.Load()

	up.broken.Store(true)

	// Face 1 — FolderInfo root: cached children stay, the note names the 500.
	root := storageJSON(t, h, "browse-degraded")
	note, _ := root["remoteDegraded"].(string)
	if !strings.HasPrefix(note, notePrefix) || !strings.Contains(note, "answered 500") {
		t.Fatalf("root note = %q, want %q head naming the 500 answer", note, notePrefix)
	}
	kids := childNames(root)
	if !kids["solo-0.2.0.tgz"] || !kids["deep"] {
		t.Fatalf("degraded root children = %v, want the cached rows kept", kids)
	}
	if kids["index.yaml"] {
		t.Fatalf("degraded root carried the derived-only row index.yaml: %v", kids)
	}

	// Face 2 — FolderInfo folder: the cached folder row resolves, the note
	// now spells the assumed-offline silence, and the window means the broken
	// upstream was probed exactly once more (never again on face 3).
	folder := storageJSON(t, h, "browse-degraded/deep")
	note, _ = folder["remoteDegraded"].(string)
	if !strings.HasPrefix(note, notePrefix) || !strings.Contains(note, "assumed offline") {
		t.Fatalf("folder note = %q, want %q head naming the assumed-offline silence", note, notePrefix)
	}

	// Face 3 — ?list: the flat listing carries the same note (one channel:
	// the two faces cannot disagree about a tree).
	listed := storageJSON(t, h, "browse-degraded/deep?list&deep=1")
	note, _ = listed["remoteDegraded"].(string)
	if !strings.HasPrefix(note, notePrefix) {
		t.Fatalf("?list note = %q, want the %q head", note, notePrefix)
	}
	files, _ := listed["files"].([]any)
	if len(files) == 0 {
		t.Fatalf("?list files empty; the cached deep row must survive the degradation: %v", listed)
	}

	if got := up.hits.Load() - hitsAfterPulls; got != 1 {
		t.Fatalf("upstream hits after the break = %d, want exactly 1 (the root probe; the silence window absorbs the rest)", got)
	}
}

// TestRemoteDegradedNoteAbsentOnHealthyAndFlagOffWire: the healthy and the
// off postures. An engaged layer over a healthy upstream renders the derived
// rows on every face with the note field ABSENT (omitempty — never a blank
// or false placeholder), and a flag-off remote over the SAME dead upstream
// stays field-absent with zero upstream contact: the degradation note is an
// engaged-layer fact, not a global one.
func TestRemoteDegradedNoteAbsentOnHealthyAndFlagOffWire(t *testing.T) {
	h := newBrowseFlagHarness(t)
	up := newDegradedUpstream(t)
	putDegradedRepo(t, h, "browse-healthy", up.srv.URL, true)
	putDegradedRepo(t, h, "browse-off", up.srv.URL, false)

	// The engaged, healthy tree: derived rows ride all three faces and the
	// note key never appears.
	root := storageJSON(t, h, "browse-healthy")
	if _, ok := root["remoteDegraded"]; ok {
		t.Fatalf("healthy root carries remoteDegraded: %v", root["remoteDegraded"])
	}
	if kids := childNames(root); !kids["index.yaml"] || !kids["solo-0.2.0.tgz"] || !kids["deep"] {
		t.Fatalf("healthy root children = %v, want the derived rows on the wire", kids)
	}

	folder := storageJSON(t, h, "browse-healthy/deep")
	if _, ok := folder["remoteDegraded"]; ok {
		t.Fatalf("healthy folder carries remoteDegraded: %v", folder["remoteDegraded"])
	}
	if kids := childNames(folder); !kids["nested"] {
		t.Fatalf("healthy folder children = %v, want the derived nested row", kids)
	}

	listed := storageJSON(t, h, "browse-healthy/deep?list&deep=1")
	if _, ok := listed["remoteDegraded"]; ok {
		t.Fatalf("healthy ?list carries remoteDegraded: %v", listed["remoteDegraded"])
	}

	// The off posture: the cached face over a DEAD upstream — field absent,
	// and the broken upstream is never even probed (off is structural).
	pullThrough(t, h, "browse-off", "solo-0.2.0.tgz")
	hitsAfterPull := up.hits.Load()
	up.broken.Store(true)
	off := storageJSON(t, h, "browse-off")
	if _, ok := off["remoteDegraded"]; ok {
		t.Fatalf("flag-off root carries remoteDegraded: %v", off["remoteDegraded"])
	}
	if kids := childNames(off); !kids["solo-0.2.0.tgz"] {
		t.Fatalf("flag-off root children = %v, want the cached row", kids)
	}
	if got := up.hits.Load() - hitsAfterPull; got != 0 {
		t.Fatalf("flag-off listing cost %d upstream hits; the off posture must never probe", got)
	}
}
