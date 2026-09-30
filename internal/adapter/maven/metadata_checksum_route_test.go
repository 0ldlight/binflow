// T-595 / BIN-77 (ledger maven/metadata-checksum-dedicated-route, R12
// three-face ruling, faces 1+2): the filename-keyed metadata checksum
// route — a PUT of maven-metadata.xml.{sha1,md5,sha256} is a dedicated
// 200-empty no-op (never the artifact sidecar interception arm), keyed on
// the terminal base name alone (hierarchy-agnostic, source-agnostic,
// validation-exempt, zero persistence), ordered after the remote refusal
// and the deploy routing gate but ahead of the handle* policy gate and
// the checksum-deploy arm; the GET side computes on demand for every
// algorithm and its miss cites the stripped source. Live anchors: L039
// Arm 5 + addendum, L040 N4 c5-*, and this ticket's probe legs
// wp1-*/wp2-*/cd-*/e-* (A 7.161.26).
package maven

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// seedSnapshotModule lands a pom+jar snapshot pair and waits for the
// calculator, leaving module and version metadata documents in place.
func seedSnapshotModule(t *testing.T, hs *harness, module, version string) {
	t.Helper()
	dir := "com/acme/" + module + "/" + version + "/"
	if resp := hs.serve(http.MethodPut, "/maven-local/"+dir+module+"-"+version+".pom", []byte("<project/>"), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("pom seed: %d %s", resp.StatusCode, string(drain(t, resp)))
	}
	if resp := hs.serve(http.MethodPut, "/maven-local/"+dir+module+"-"+version+".jar", jarBytes, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("jar seed: %d %s", resp.StatusCode, string(drain(t, resp)))
	}
	hs.waitCalc()
}

// TestMetadataChecksumRoutePutNoop walks the route's matrix on the local
// plane: every level (module, version, root), every suffix, source present
// or absent, value right or wrong — one answer, 200 with an empty body, no
// Content-Type, no Location, and zero storage effects.
func TestMetadataChecksumRoutePutNoop(t *testing.T) {
	hs := newHarness(t)
	defer hs.waitCalc()
	seedSnapshotModule(t, hs, "t595mod", "1.0-SNAPSHOT")
	mod := "com/acme/t595mod/maven-metadata.xml"
	ver := "com/acme/t595mod/1.0-SNAPSHOT/maven-metadata.xml"

	legs := []struct {
		name string
		path string
		body string
	}{
		{"module-value-ok", mod + ".sha1", strings.Repeat("1", 40)},
		{"module-md5-wrong", mod + ".md5", strings.Repeat("0", 32)},
		{"module-sha256-wrong", mod + ".sha256", strings.Repeat("3", 64)},
		{"version-value-ok", ver + ".sha1", strings.Repeat("1", 40)},
		{"version-source-miss", "com/acme/t595mod/2.0-SNAPSHOT/maven-metadata.xml.sha1", strings.Repeat("1", 40)},
		{"module-source-miss", "com/acme/never-seeded/maven-metadata.xml.sha1", strings.Repeat("1", 40)},
		{"root", "maven-metadata.xml.sha1", strings.Repeat("1", 40)},
		{"case-SHA1", mod + ".SHA1", strings.Repeat("1", 40)},
		{"case-Sha1-md5", mod + ".Sha1", strings.Repeat("0", 32)},
	}
	for _, lg := range legs {
		resp := hs.serve(http.MethodPut, "/maven-local/"+lg.path, []byte(lg.body), nil, true)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: PUT = %d (%s), want the route's 200 no-op", lg.name, resp.StatusCode, drain(t, resp))
			continue
		}
		if got := drain(t, resp); len(got) != 0 {
			t.Errorf("%s: 200 body = %q, want empty", lg.name, got)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			t.Errorf("%s: Content-Type = %q, want none", lg.name, ct)
		}
		if loc := resp.Header.Get("Location"); loc != "" {
			t.Errorf("%s: Location = %q, want none", lg.name, loc)
		}
	}

	// Zero storage effects: no sidecar node landed anywhere in the family
	// (the addendum's pure no-op), and the module document stands unchanged.
	for _, p := range []string{mod + ".sha1", mod + ".md5", mod + ".sha256", "maven-metadata.xml.sha1", "maven-metadata.xml"} {
		if _, err := hs.md.Nodes().Get(context.Background(), "maven-local", p); err == nil {
			t.Errorf("node landed at %s: the route must write nothing", p)
		}
	}
	resp := hs.serve(http.MethodGet, "/maven-local/"+mod, nil, nil, true)
	doc := drain(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(doc), "<artifactId>t595mod</artifactId>") {
		t.Fatalf("module document after the family = %d %.120s, want the calculator's own body", resp.StatusCode, doc)
	}
	// The metadata node keeps the calculator's own registration untouched:
	// its measured sha256 of the doc it served, no sha1/md5, and none of
	// the family's PUT bodies anywhere near the row.
	node, err := hs.md.Nodes().Get(context.Background(), "maven-local", mod)
	if err != nil {
		t.Fatalf("module node: %v", err)
	}
	if node.ClientSha256 != sha256Hex(doc) || node.ClientSha1 != "" || node.ClientMd5 != "" {
		t.Errorf("module client columns after the family = sha256:%q sha1:%q md5:%q, want the calculator's own (sha256 %q, no side declarations)",
			node.ClientSha256, node.ClientSha1, node.ClientMd5, sha256Hex(doc))
	}
}

// TestMetadataChecksumRouteOrdering pins the ruling's order model around
// the route: the non-local refusals (unrouted virtual C5 405, the bare
// remote 405 defense) come first, then the route outranks the handle*
// policy gate and the checksum-deploy arm — while the family's non-members
// (the plugin-group variant, a non-.xml spelling) keep the intercept
// family's own miss.
func TestMetadataChecksumRouteOrdering(t *testing.T) {
	hs := newHarness(t)
	if _, err := hs.svc.CreateRepo(context.Background(), adminP, &metadata.Repo{
		RepoKey:     "t595-virt",
		Type:        repo.TypeVirtual,
		PackageType: Protocol,
		Config:      `{"repositories":["maven-local"],"defaultDeploymentRepo":"maven-local"}`,
	}); err != nil {
		t.Fatalf("seed routed virtual: %v", err)
	}
	mod := "com/acme/ordermod/maven-metadata.xml"

	// Unrouted virtual: the C5 405, verbatim, zero side effects.
	resp := hs.serve(http.MethodPut, "/maven-virtual/"+mod+".sha1", []byte(strings.Repeat("1", 40)), nil, true)
	if resp.StatusCode != http.StatusMethodNotAllowed ||
		!strings.Contains(string(drain(t, resp)), unroutedVirtualWriteMessage("maven-virtual")) {
		t.Fatalf("unrouted virtual = %d, want the C5 405 verbatim", resp.StatusCode)
	}
	// Routed virtual: the route fires against the addressed key.
	if resp := hs.serve(http.MethodPut, "/t595-virt/"+mod+".sha1", []byte(strings.Repeat("1", 40)), nil, true); resp.StatusCode != http.StatusOK {
		t.Fatalf("routed virtual = %d (%s), want 200 no-op", resp.StatusCode, drain(t, resp))
	}
	// Remote (bare-mount defense): the read-only 405, no fetch attempt.
	resp = hs.serve(http.MethodPut, "/maven-remote/"+mod+".sha1", []byte(strings.Repeat("1", 40)), nil, true)
	if resp.StatusCode != http.StatusMethodNotAllowed || !strings.Contains(string(drain(t, resp)), "read-only proxy cache") {
		t.Fatalf("remote = %d, want the bare-mount read-only 405", resp.StatusCode)
	}

	// The handle* policy gate: a handleReleases=false repository 409s a
	// plain release deploy (the control) but answers the family's 200 —
	// the route outranks the gate (live wp1-hr pair).
	if resp := hs.serve(http.MethodPut, "/maven-relonly/com/acme/ordermod/1.0.0/ordermod-1.0.0.jar", jarBytes, nil, true); resp.StatusCode != http.StatusConflict {
		t.Fatalf("handleReleases=false control = %d, want 409", resp.StatusCode)
	}
	if resp := hs.serve(http.MethodPut, "/maven-relonly/"+mod+".sha1", []byte(strings.Repeat("1", 40)), nil, true); resp.StatusCode != http.StatusOK {
		t.Fatalf("handleReleases=false metadata sidecar = %d (%s), want the route's 200", resp.StatusCode, drain(t, resp))
	}

	// The checksum-deploy arm: X-Checksum-Deploy on the family answers the
	// route's 200, not putChecksumDeploy's artifacts-only 400 (live
	// cd-meta-path).
	cd := hs.serve(http.MethodPut, "/maven-local/"+mod+".sha1", nil,
		map[string]string{"X-Checksum-Deploy": "true", "X-Checksum-Sha256": strings.Repeat("ab", 32)}, true)
	if cd.StatusCode != http.StatusOK || len(drain(t, cd)) != 0 {
		t.Fatalf("checksum-deploy on the family = %d (%s), want the route's 200 no-op", cd.StatusCode, drain(t, cd))
	}

	// Non-members keep their own families: the plugin-group variant falls
	// to the intercept arm's target-miss 404 (live wp1-plugin-variant), a
	// non-.xml spelling likewise (live wp1-root-nonxml).
	for _, p := range []string{
		"com/acme/ordermod/metadata-maven-metadata.xml.sha1",
		"maven-metadata.md5",
	} {
		resp := hs.serve(http.MethodPut, "/maven-local/"+p, []byte(strings.Repeat("1", 40)), nil, true)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("non-member %s = %d (%s), want the intercept family's 404", p, resp.StatusCode, drain(t, resp))
			continue
		}
		src := strings.TrimSuffix(strings.TrimSuffix(p, ".sha1"), ".md5")
		if got := string(drain(t, resp)); !strings.Contains(got, "Target file to set checksum on doesn't exist: maven-local:"+src) {
			t.Errorf("non-member %s body = %q, want the Target-file miss citing %s", p, got, src)
		}
	}
}

// TestMetadataChecksumRouteGetMissCitesSource pins the read-side miss of
// the un-parseable family spellings: the root-level GET cites the stripped
// SOURCE (the T-562 gate's one family exception), not the suffix spelling.
func TestMetadataChecksumRouteGetMissCitesSource(t *testing.T) {
	hs := newHarness(t)
	for _, p := range []string{
		"maven-metadata.xml.sha1",
		"maven-metadata.xml.md5",
		"maven-metadata.xml.sha256",
		"maven-metadata.xml.SHA1",
	} {
		algo := p[strings.LastIndexByte(p, '.')+1:]
		resp := hs.serve(http.MethodGet, "/maven-local/"+p, nil, nil, true)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, resp.StatusCode)
			continue
		}
		got := string(drain(t, resp))
		if want := "File not found.; Path: 'maven-local:maven-metadata.xml'"; !strings.Contains(got, want) {
			t.Errorf("GET %s body = %q, want the source-cited miss %q", p, got, want)
		}
		if strings.Contains(got, "."+algo+"'") {
			t.Errorf("GET %s cites the suffix spelling: %s", p, got)
		}
	}
}

// TestSidecarGetSha256PrimaryDigestWins pins the overlay flip (live
// e-get-sha256-after): a wrong sha256 registered through the 409
// write-through still renders the COMPUTED primary digest on the
// overlay-armed faces — the sha1/md5 write-through echo (L038 whitelist
// #5) is untouched.
func TestSidecarGetSha256PrimaryDigestWins(t *testing.T) {
	hs := newHarness(t)
	if _, err := hs.svc.CreateRepo(context.Background(), adminP, &metadata.Repo{
		RepoKey:     "t595-read",
		Type:        repo.TypeVirtual,
		PackageType: Protocol,
		Config:      `{"repositories":["maven-local"]}`,
	}); err != nil {
		t.Fatalf("seed read virtual: %v", err)
	}
	jar := "com/acme/t595e/1.0.0/t595e-1.0.0.jar"
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar, jarBytes, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	_, m5, s256 := digests(jarBytes)
	w64, w32 := strings.Repeat("3", 64), strings.Repeat("0", 32)

	// The 409 write-through registers both wrong values (L037 Arm 1).
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar+".sha256", []byte(w64), nil, true); resp.StatusCode != http.StatusConflict {
		t.Fatalf("sha256 wrong-value registration = %d, want 409 write-through", resp.StatusCode)
	}
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar+".md5", []byte(w32), nil, true); resp.StatusCode != http.StatusConflict {
		t.Fatalf("md5 wrong-value registration = %d, want 409 write-through", resp.StatusCode)
	}
	for _, face := range []string{"maven-local", "t595-read"} {
		if got := string(drain(t, hs.serve(http.MethodGet, "/"+face+"/"+jar+".sha256", nil, nil, true))); got != s256 {
			t.Errorf("%s GET .sha256 (registered wrong) = %q, want the computed %q", face, got, s256)
		}
		if got := string(drain(t, hs.serve(http.MethodGet, "/"+face+"/"+jar+".md5", nil, nil, true))); got != w32 {
			t.Errorf("%s GET .md5 (registered wrong) = %q, want the write-through echo %q", face, got, w32)
		}
	}
	if node, err := hs.md.Nodes().Get(context.Background(), "maven-local", jar); err != nil {
		t.Fatalf("node: %v", err)
	} else if node.ClientSha256 != w64 {
		t.Errorf("registration kept = %q, want the write-through value (only the GET rendering flips)", node.ClientSha256)
	}
	_ = m5
}

// TestVirtualMetadataMergeMissWording pins the merge face's miss family on
// both the body and the sidecar branches: `Maven metadata not found for
// '<src>'.; Path: '<virt>:<src>'` — the SOURCE, never the suffix spelling
// (live wp2-virt-*; virtual-resolution section 5.1's miss row).
func TestVirtualMetadataMergeMissWording(t *testing.T) {
	hs := newHarness(t)
	src := "com/acme/ghostmod/maven-metadata.xml"
	want := "Maven metadata not found for '" + src + "'.; Path: 'maven-virtual:" + src + "'"

	body := hs.serve(http.MethodGet, "/maven-virtual/"+src, nil, nil, true)
	if body.StatusCode != http.StatusNotFound || !strings.Contains(string(drain(t, body)), want) {
		t.Fatalf("virtual body miss = %d, want the merge family's 404 %q", body.StatusCode, want)
	}
	for _, algo := range []string{"sha1", "md5", "sha256"} {
		resp := hs.serve(http.MethodGet, "/maven-virtual/"+src+"."+algo, nil, nil, true)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("virtual .%s miss = %d, want 404", algo, resp.StatusCode)
			continue
		}
		if got := string(drain(t, resp)); !strings.Contains(got, want) {
			t.Errorf("virtual .%s miss body = %q, want %q", algo, got, want)
		}
	}
}

// TestChecksumDeployEnvelopeDropsAncillaryClaims pins the checksum-deploy
// envelope face (live cd-wrong-md5): a declared ancillary digest (a wrong
// X-Checksum-Md5 beside the sha256 address) neither registers nor
// surfaces — the envelope's originalChecksums render the computed triple —
// while the sha1-only addressing form keeps resolving.
func TestChecksumDeployEnvelopeDropsAncillaryClaims(t *testing.T) {
	hs := newHarness(t)
	if resp := hs.deployJar("maven-local", "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed deploy: %d", resp.StatusCode)
	}
	s1, m5, s256 := digests(jarBytes)

	resp := hs.serve(http.MethodPut, "/maven-local/com/acme/demo-app/2.0.0/demo-app-2.0.0.jar", nil,
		map[string]string{"X-Checksum-Deploy": "true", "X-Checksum-Sha256": s256,
			"X-Checksum-Md5": strings.Repeat("0", 32)}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("checksum deploy with wrong md5 = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	var env struct {
		Checksums         *checksums `json:"checksums"`
		OriginalChecksums *checksums `json:"originalChecksums"`
	}
	if err := json.Unmarshal(drain(t, resp), &env); err != nil {
		t.Fatalf("envelope parse: %v", err)
	}
	for name, got := range map[string]*checksums{"checksums": env.Checksums, "originalChecksums": env.OriginalChecksums} {
		if got == nil {
			t.Fatalf("%s block missing", name)
		}
		if got.Md5 != m5 {
			t.Errorf("%s.md5 = %q, want the computed %q (the wrong claim must not surface)", name, got.Md5, m5)
		}
		if got.Sha256 != s256 {
			t.Errorf("%s.sha256 = %q, want %q", name, got.Sha256, s256)
		}
	}

	// The sha1-only addressing form still resolves (its ref rides the
	// ledger index; nothing else registers).
	sha1Only := hs.serve(http.MethodPut, "/maven-local/com/acme/demo-app/3.0.0/demo-app-3.0.0.jar", nil,
		map[string]string{"X-Checksum-Deploy": "true", "X-Checksum-Sha1": s1}, true)
	if sha1Only.StatusCode != http.StatusCreated {
		t.Fatalf("sha1-only deploy = %d (%s)", sha1Only.StatusCode, drain(t, sha1Only))
	}
}
