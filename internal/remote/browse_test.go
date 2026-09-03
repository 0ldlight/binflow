package remote

// T-442 (FR-147.1) — the remote-browsing batch-1 enumeration: table-driven
// per-type walks against a loopback upstream over the fetcher's own
// fixture stack (the M6 self-referential posture: the upstream documents
// mirror what BinFlow's own deb/rpm/helm index renderers publish), plus
// the degradation/offline semantics, the metadata-TTL snapshot window, the
// ACL seam's zero-enumeration denial and the zero-side-effect probe
// (nothing lands — the optional档's display-only contract).

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"
)

// allowAll is the permissive BrowsePermit (the caller's allow() stand-in).
func allowAll(string) bool { return true }

// denyAll refuses every folder.
func denyAll(string) bool { return false }

// renderEntries flattens one BrowseResult's children for table comparison:
// folders carry a trailing slash.
func renderEntries(res *BrowseResult) []string {
	out := make([]string, 0, len(res.Entries))
	for _, e := range res.Entries {
		if e.IsFolder {
			out = append(out, e.Path+"/")
		} else {
			out = append(out, e.Path)
		}
	}
	return out
}

// browseQuery is one folder walk plus its expected children.
type browseQuery struct {
	folder string
	want   []string
}

// nodeCount counts the repository's node rows (the zero-side-effect
// assertion's before/after reading).
func nodeCount(t *testing.T, e *fetchEnv, repoKey string) int {
	t.Helper()
	nodes, err := e.md.Nodes().ListByPrefix(context.Background(), repoKey, "")
	if err != nil {
		t.Fatalf("count nodes %s: %v", repoKey, err)
	}
	return len(nodes)
}

// ---- AC1: the three batch-1 types, table-driven ----

// TestBrowseRemoteBatch1Types walks helm/deb/rpm repositories against a
// loopback upstream serving each type's index documents, asserting every
// folder's children AND the upstream-contact count (one snapshot per
// repository — the metadata-TTL cache's single-fetch window).
func TestBrowseRemoteBatch1Types(t *testing.T) {
	cases := []struct {
		name        string
		packageType string
		seed        func(t *testing.T, e *fetchEnv, repoKey string)
		queries     []browseQuery
		wantHits    int64 // upstream GETs the whole case costs
	}{
		{
			name:        "helm classic index.yaml whole tree",
			packageType: "helm",
			seed: func(_ *testing.T, e *fetchEnv, _ string) {
				e.state.files["/index.yaml"] = `apiVersion: v1
entries:
  acs-engine:
  - name: acs-engine
    version: "1.0.0"
    urls:
    - charts/acs-engine-1.0.0.tgz
  - name: acs-engine
    version: "1.1.0"
    urls:
    - charts/acs-engine-1.1.0.tgz
  solo:
  - name: solo
    version: "0.2.0"
    urls:
    - solo-0.2.0.tgz
  away:
  - name: away
    version: "3.0.0"
    urls:
    - https://charts.example/away/away-3.0.0.tgz?sig=1
  evil:
  - name: evil
    version: "9.9.9"
    urls:
    - ../../evil-9.9.9.tgz
`
			},
			queries: []browseQuery{
				{folder: "", want: []string{"away/", "charts/", "index.yaml", "solo-0.2.0.tgz"}},
				{folder: "charts", want: []string{"charts/acs-engine-1.0.0.tgz", "charts/acs-engine-1.1.0.tgz"}},
				{folder: "away", want: []string{"away/away-3.0.0.tgz"}},
				{folder: "no-such-folder", want: []string{}},
			},
			wantHits: 1, // index.yaml once; every folder walk reads the snapshot
		},
		{
			name:        "debian suite metadata enumeration",
			packageType: "debian",
			seed: func(t *testing.T, e *fetchEnv, repoKey string) {
				pkgs := `Package: foo
Version: 1.0
Filename: pool/main/f/foo/foo_1.0_amd64.deb
Size: 10

Package: foo
Version: 2.0
Filename: pool/main/f/foo/foo_2.0_amd64.deb
Size: 10

Package: bar
Version: 1.1
Filename: pool/main/b/bar/bar_1.1_all.deb
Size: 10
`
				var gz bytes.Buffer
				zw := gzip.NewWriter(&gz)
				if _, err := zw.Write([]byte(pkgs)); err != nil {
					t.Fatalf("gzip packages: %v", err)
				}
				if err := zw.Close(); err != nil {
					t.Fatalf("close gzip: %v", err)
				}
				e.state.files["/dists/stable/Release"] = `Origin: BinFlow
Label: BinFlow
Suite: stable
Codename: stable
Components: main
Architectures: amd64
Description: test suite
SHA256:
 1111111111111111111111111111111111111111111111111111111111111111       83 main/binary-amd64/Packages
 2222222222222222222222222222222222222222222222222222222222222222       45 main/binary-amd64/Packages.gz
 3333333333333333333333333333333333333333333333333333333333333333       20 source/main/Sources
`
				e.state.files["/dists/stable/main/binary-amd64/Packages"] = pkgs
				e.state.files["/dists/stable/main/binary-amd64/Packages.gz"] = gz.String()
				e.state.files["/dists/stable/source/main/Sources"] = "Format: 3.0 (native)\n"
				// Prime the suite discovery: an apt client's Release pull
				// through the ordinary engine (this lands the cached row
				// the suite set is derived from — the self-referential
				// learning path).
				if _, err := e.eng.Fetch(context.Background(), repoKey, "dists/stable/Release"); err != nil {
					t.Fatalf("prime suite: %v", err)
				}
			},
			queries: []browseQuery{
				{folder: "", want: []string{"dists/", "pool/"}},
				{folder: "dists", want: []string{"dists/stable/"}},
				{folder: "dists/stable", want: []string{"dists/stable/Release", "dists/stable/main/", "dists/stable/source/"}},
				{folder: "dists/stable/main", want: []string{"dists/stable/main/binary-amd64/"}},
				{folder: "dists/stable/main/binary-amd64", want: []string{"dists/stable/main/binary-amd64/Packages", "dists/stable/main/binary-amd64/Packages.gz"}},
				{folder: "dists/stable/source/main", want: []string{"dists/stable/source/main/Sources"}},
				{folder: "pool/main/f/foo", want: []string{"pool/main/f/foo/foo_1.0_amd64.deb", "pool/main/f/foo/foo_2.0_amd64.deb"}},
				{folder: "pool/main/b/bar", want: []string{"pool/main/b/bar/bar_1.1_all.deb"}},
			},
			// 1 Release + 1 chosen Packages index (the uncompressed
			// variant wins the per-directory pick; the priming contact is
			// subtracted out below).
			wantHits: 2,
		},
		{
			name:        "rpm repomd primary enumeration",
			packageType: "rpm",
			seed: func(_ *testing.T, e *fetchEnv, _ string) {
				primary := `<?xml version="1.0" encoding="UTF-8"?>
<metadata xmlns="http://linux.duke.edu/metadata/common" packages="2">
  <package type="rpm">
    <name>foo</name>
    <arch>x86_64</arch>
    <location href="Packages/x86_64/foo-1.0-1.x86_64.rpm"/>
  </package>
  <package type="rpm">
    <name>bar</name>
    <arch>noarch</arch>
    <location href="Packages/noarch/bar-2.0-1.noarch.rpm"/>
  </package>
</metadata>
`
				var gz bytes.Buffer
				zw := gzip.NewWriter(&gz)
				if _, err := zw.Write([]byte(primary)); err != nil {
					t.Fatalf("gzip primary: %v", err)
				}
				if err := zw.Close(); err != nil {
					t.Fatalf("close gzip: %v", err)
				}
				e.state.files["/repodata/repomd.xml"] = `<?xml version="1.0" encoding="UTF-8"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo">
  <revision></revision>
  <data type="primary">
    <location href="repodata/2b7e-primary.xml.gz"/>
    <checksum type="sha256">aaaa</checksum>
  </data>
  <data type="filelists">
    <location href="repodata/3c8f-filelists.xml.gz"/>
    <checksum type="sha256">bbbb</checksum>
  </data>
</repomd>
`
				e.state.files["/repodata/2b7e-primary.xml.gz"] = gz.String()
				e.state.files["/repodata/3c8f-filelists.xml.gz"] = ""
			},
			queries: []browseQuery{
				{folder: "", want: []string{"Packages/", "repodata/"}},
				{folder: "repodata", want: []string{"repodata/2b7e-primary.xml.gz", "repodata/3c8f-filelists.xml.gz", "repodata/repomd.xml"}},
				{folder: "Packages", want: []string{"Packages/noarch/", "Packages/x86_64/"}},
				{folder: "Packages/x86_64", want: []string{"Packages/x86_64/foo-1.0-1.x86_64.rpm"}},
				{folder: "Packages/noarch", want: []string{"Packages/noarch/bar-2.0-1.noarch.rpm"}},
			},
			wantHits: 2, // repomd.xml + primary
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newFetchEnv(t, nil)
			repoKey := "browse-" + tc.packageType
			e.createRemote(t, repoKey, tc.packageType, nil)
			hitsBefore := e.hits.Load()
			tc.seed(t, e, repoKey)
			primed := e.hits.Load() - hitsBefore
			nodesBefore := nodeCount(t, e, repoKey)

			ctx := context.Background()
			for _, q := range tc.queries {
				res, err := e.eng.BrowseRemote(ctx, allowAll, repoKey, q.folder)
				if err != nil {
					t.Fatalf("BrowseRemote(%q): %v", q.folder, err)
				}
				if res.Degraded != "" {
					t.Fatalf("BrowseRemote(%q) degraded: %s", q.folder, res.Degraded)
				}
				if got := renderEntries(res); !equalStrings(got, q.want) {
					t.Errorf("BrowseRemote(%q) children = %v, want %v", q.folder, got, q.want)
				}
			}
			if got := e.hits.Load() - hitsBefore - primed; got != tc.wantHits {
				t.Errorf("upstream contacts = %d, want %d (one snapshot per repository)", got, tc.wantHits)
			}
			// The display-only contract (§4-3): enumeration lands nothing.
			if after := nodeCount(t, e, repoKey); after != nodesBefore {
				t.Errorf("node rows = %d after browsing, want %d (zero side effects)", after, nodesBefore)
			}
		})
	}
}

// TestBrowseRemoteOffDefaultAndUnsupportedTypes pins the off posture:
// the other package types (generic among them) are refused, and a folder
// walk on an unsupported or malformed spelling never reaches the upstream.
func TestBrowseRemoteOffDefaultAndUnsupportedTypes(t *testing.T) {
	e := newFetchEnv(t, nil)
	e.state.files["/index.yaml"] = "apiVersion: v1\nentries: {}\n"
	ctx := context.Background()

	if _, err := e.eng.BrowseRemote(ctx, allowAll, "generic-remote", ""); !errors.Is(err, ErrBrowseUnsupportedType) {
		t.Errorf("browse generic = %v, want ErrBrowseUnsupportedType", err)
	}
	if _, err := e.eng.BrowseRemote(ctx, allowAll, "generic-remote", "../etc"); !errors.Is(err, ErrBrowseInvalidFolder) {
		t.Errorf("browse dot-dot folder = %v, want ErrBrowseInvalidFolder", err)
	}
	if _, err := e.eng.BrowseRemote(ctx, allowAll, "generic-remote", "a//b"); !errors.Is(err, ErrBrowseInvalidFolder) {
		t.Errorf("browse empty-segment folder = %v, want ErrBrowseInvalidFolder", err)
	}
	if got := e.hits.Load(); got != 0 {
		t.Errorf("upstream contacts = %d, want 0", got)
	}
	if !BrowseSupported("helm") || !BrowseSupported("debian") || !BrowseSupported("rpm") {
		t.Error("BrowseSupported(batch 1) = false for a batch-1 type")
	}
	for _, pt := range []string{"generic", "docker", "maven", "npm", "pypi", "nuget", "go", "conan", "cargo", "helmoci"} {
		if BrowseSupported(pt) {
			t.Errorf("BrowseSupported(%q) = true, want false", pt)
		}
	}
}

// ---- AC2: degradation, offline silence, ACL, zero side effects ----

// TestBrowseRemoteUpstreamFaultsDegradeNotCollapse walks §4-1/§4-2: an
// upstream fault degrades the remote layer (no entries, an inline reason,
// no error) instead of collapsing the listing; transport/5xx faults open
// the assumed-offline window (a second browse inside the silence contacts
// nothing); a credentials refusal degrades WITHOUT opening the window.
func TestBrowseRemoteUpstreamFaultsDegradeNotCollapse(t *testing.T) {
	e := newFetchEnv(t, nil)
	e.createRemote(t, "helm-r", "helm", nil)
	e.state.files["/index.yaml"] = "apiVersion: v1\nentries:\n  acs:\n  - name: acs\n    version: \"1.0\"\n    urls:\n    - acs-1.0.tgz\n"
	ctx := context.Background()

	// Healthy baseline: the snapshot caches under the metadata TTL.
	res, err := e.eng.BrowseRemote(ctx, allowAll, "helm-r", "")
	if err != nil || res.Degraded != "" {
		t.Fatalf("baseline browse = (%v, %q), want healthy", err, res.Degraded)
	}
	if got := renderEntries(res); !equalStrings(got, []string{"acs-1.0.tgz", "index.yaml"}) {
		t.Fatalf("baseline children = %v", got)
	}

	// 5xx: the layer degrades — empty entries, an inline reason, nil
	// error (the caller keeps serving the cached rows beside the note).
	e.clk.Advance(601 * time.Second) // past the metadata TTL: refresh forced
	e.state.set("500")
	hitsAtFault := e.hits.Load()
	res, err = e.eng.BrowseRemote(ctx, allowAll, "helm-r", "")
	if err != nil {
		t.Fatalf("faulted browse: %v", err)
	}
	if res.Degraded == "" || res.Entries != nil {
		t.Fatalf("faulted browse = (%q entries, %d rows), want degraded with zero entries", res.Degraded, len(res.Entries))
	}

	// The assumed-offline window opened: the next browse inside the
	// silence contacts nothing and still presents the error state.
	e.state.set("ok")
	res, err = e.eng.BrowseRemote(ctx, allowAll, "helm-r", "")
	if err != nil || res.Degraded == "" {
		t.Fatalf("silent-window browse = (%v, %q), want degraded without contact", err, res.Degraded)
	}
	if got := e.hits.Load() - hitsAtFault; got != 1 {
		t.Errorf("upstream contacts across fault + silence = %d, want 1 (the faulting probe only)", got)
	}

	// Past the silence the enumeration recovers.
	e.clk.Advance(301 * time.Second)
	res, err = e.eng.BrowseRemote(ctx, allowAll, "helm-r", "")
	if err != nil || res.Degraded != "" {
		t.Fatalf("recovered browse = (%v, %q)", err, res.Degraded)
	}
	if got := renderEntries(res); !equalStrings(got, []string{"acs-1.0.tgz", "index.yaml"}) {
		t.Fatalf("recovered children = %v", got)
	}

	// A credentials refusal degrades but does NOT open the window: the
	// very next browse (clock untouched) contacts the upstream again.
	e.clk.Advance(601 * time.Second)
	e.state.user, e.state.pass = "u", "p"
	e.state.set("auth")
	res, err = e.eng.BrowseRemote(ctx, allowAll, "helm-r", "")
	if err != nil || res.Degraded == "" || res.Entries != nil {
		t.Fatalf("refused browse = (%v, %q, %d rows), want degraded", err, res.Degraded, len(res.Entries))
	}
	if !strings.Contains(res.Degraded, "refused") {
		t.Errorf("refused degradation = %q, want the refusal wording", res.Degraded)
	}
	e.state.set("ok")
	res, err = e.eng.BrowseRemote(ctx, allowAll, "helm-r", "")
	if err != nil || res.Degraded != "" {
		t.Fatalf("post-refusal browse = (%v, %q), want immediate recovery (no offline window)", err, res.Degraded)
	}
}

// TestBrowseRemoteSnapshotMetadataTTL pins the cache window: one upstream
// index fetch per metadata TTL (the official Metadata Retrieval Cache
// Period mapping), and a config change invalidates the snapshot early.
func TestBrowseRemoteSnapshotMetadataTTL(t *testing.T) {
	e := newFetchEnv(t, nil)
	e.createRemote(t, "helm-r", "helm", nil)
	e.state.files["/index.yaml"] = "apiVersion: v1\nentries:\n  acs:\n  - name: acs\n    version: \"1.0\"\n    urls:\n    - acs-1.0.tgz\n"
	ctx := context.Background()

	if _, err := e.eng.BrowseRemote(ctx, allowAll, "helm-r", ""); err != nil {
		t.Fatalf("first browse: %v", err)
	}
	if _, err := e.eng.BrowseRemote(ctx, allowAll, "helm-r", "charts"); err != nil {
		t.Fatalf("second browse: %v", err)
	}
	if got := e.hits.Load(); got != 1 {
		t.Fatalf("upstream contacts inside the TTL = %d, want 1", got)
	}

	// Past the TTL the snapshot refreshes (one more contact) and serves
	// an upstream that changed underneath it.
	e.clk.Advance(601 * time.Second)
	e.state.files["/index.yaml"] = "apiVersion: v1\nentries:\n  acs:\n  - name: acs\n    version: \"2.0\"\n    urls:\n    - acs-2.0.tgz\n"
	res, err := e.eng.BrowseRemote(ctx, allowAll, "helm-r", "")
	if err != nil {
		t.Fatalf("post-TTL browse: %v", err)
	}
	if got := renderEntries(res); !equalStrings(got, []string{"acs-2.0.tgz", "index.yaml"}) {
		t.Fatalf("post-TTL children = %v, want the refreshed tree", got)
	}
	if got := e.hits.Load(); got != 2 {
		t.Fatalf("upstream contacts after refresh = %d, want 2", got)
	}
}

// TestBrowseRemotePermitIsTheAllowSource pins the ACL seam: a nil or
// refusing permit answers ErrBrowseDenied with ZERO upstream contact and
// ZERO rows revealed — not even a snapshot a permitted call just cached.
func TestBrowseRemotePermitIsTheAllowSource(t *testing.T) {
	e := newFetchEnv(t, nil)
	e.createRemote(t, "helm-r", "helm", nil)
	e.state.files["/index.yaml"] = "apiVersion: v1\nentries:\n  acs:\n  - name: acs\n    version: \"1.0\"\n    urls:\n    - acs-1.0.tgz\n"
	ctx := context.Background()

	if _, err := e.eng.BrowseRemote(ctx, denyAll, "helm-r", ""); !errors.Is(err, ErrBrowseDenied) {
		t.Errorf("refused permit = %v, want ErrBrowseDenied", err)
	}
	if _, err := e.eng.BrowseRemote(ctx, nil, "helm-r", ""); !errors.Is(err, ErrBrowseDenied) {
		t.Errorf("nil permit = %v, want ErrBrowseDenied (fail closed)", err)
	}
	if got := e.hits.Load(); got != 0 {
		t.Fatalf("upstream contacts after denials = %d, want 0", got)
	}

	// The folder the permit is asked about is the folder being browsed
	// (the caller's allow() source decides, folder by folder).
	var asked []string
	permit := func(folder string) bool {
		asked = append(asked, folder)
		return folder == "charts"
	}
	if _, err := e.eng.BrowseRemote(ctx, permit, "helm-r", "dists"); !errors.Is(err, ErrBrowseDenied) {
		t.Errorf("permit(dists)=false browse = %v, want ErrBrowseDenied", err)
	}
	if _, err := e.eng.BrowseRemote(ctx, permit, "helm-r", "charts"); err != nil {
		t.Errorf("permit(charts)=true browse: %v", err)
	}
	sort.Strings(asked)
	if !equalStrings(asked, []string{"charts", "dists"}) {
		t.Errorf("permit asked about %v, want the browsed folders", asked)
	}

	// A cached snapshot must not leak past a later refusal.
	if _, err := e.eng.BrowseRemote(ctx, denyAll, "helm-r", ""); !errors.Is(err, ErrBrowseDenied) {
		t.Errorf("post-cache refused permit = %v, want ErrBrowseDenied", err)
	}
	if got := e.hits.Load(); got != 1 {
		t.Errorf("upstream contacts = %d, want 1 (the permitted refresh only)", got)
	}
}

// TestBrowseRemoteUnfoundIndexIsEmptyAndUncached: an upstream with no
// index at the expected address is healthy-but-empty (no degradation
// note), and the emptiness is not cached — the first index to appear
// shows up on the next browse.
func TestBrowseRemoteUnfoundIndexIsEmptyAndUncached(t *testing.T) {
	e := newFetchEnv(t, nil)
	e.createRemote(t, "helm-r", "helm", nil)
	ctx := context.Background()

	res, err := e.eng.BrowseRemote(ctx, allowAll, "helm-r", "")
	if err != nil || res.Degraded != "" || len(res.Entries) != 0 {
		t.Fatalf("empty-upstream browse = (%v, %q, %d rows), want healthy empty", err, res.Degraded, len(res.Entries))
	}
	e.state.files["/index.yaml"] = "apiVersion: v1\nentries:\n  acs:\n  - name: acs\n    version: \"1.0\"\n    urls:\n    - acs-1.0.tgz\n"
	res, err = e.eng.BrowseRemote(ctx, allowAll, "helm-r", "")
	if err != nil || res.Degraded != "" {
		t.Fatalf("post-index browse = (%v, %q)", err, res.Degraded)
	}
	if got := renderEntries(res); !equalStrings(got, []string{"acs-1.0.tgz", "index.yaml"}) {
		t.Fatalf("post-index children = %v, want the new tree immediately", got)
	}
}

// equalStrings compares two string slices in order.
func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
