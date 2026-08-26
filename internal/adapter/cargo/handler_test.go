package cargo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The contract tests: every assertion crosses the full middleware chain
// (auth gates, prefix stripping, adapter dispatch) on the real stack.

// fixtureMeta renders one publish metadata frame.
func fixtureMeta(name, vers string) string {
	return fmt.Sprintf(`{"name":%q,"vers":%q,"deps":[{"name":"serde","req":"^1","features":[],"optional":false,"default_features":true,"kind":"normal","registry":"https://github.com/rust-lang/crates.io-index"}],"features":{"default":["std"]},"description":"demo crate","keywords":["demo","test"],"categories":["cli"],"links":"repo"}`, name, vers)
}

// fixtureCrate is a stand-in .crate body (the server never opens the
// tarball — the checksum chain is what is under test).
func fixtureCrate(name, vers string) []byte {
	return []byte("GZIP-PLACEHOLDER:" + name + "-" + vers)
}

// publish issues the publish PUT as admin.
func (s *stack) publish(t *testing.T, repoKey, meta string, crate []byte) (int, string, http.Header) {
	t.Helper()
	return s.put(repoPath(repoKey)+"/api/v1/crates/new", publishBody(meta, crate), nil)
}

// sha256hex of a byte slice.
func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestRootProbe: GET <repo>/ answers 200 with an empty body (spec
// section 2); the probe stays anonymous.
func TestRootProbe(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	status, body, _ := s.get(repoPath("cargo-local") + "/")
	if status != http.StatusOK || body != "" {
		t.Fatalf("root probe = (%d, %q), want (200, \"\")", status, body)
	}
}

// TestConfigJSONTwoStates: the dl/api absolutes take Options.BaseURL
// (server.base_url) when set and the request origin otherwise (TL-1);
// the anonymous-enabled posture omits auth-required.
func TestConfigJSONTwoStates(t *testing.T) {
	t.Run("base url from options", func(t *testing.T) {
		s := newStackOpt(t, stackOptions{baseURL: "https://binflow.example.test/", anonymous: true})
		s.seedRepo(t, "cargo-local", repo.TypeLocal)
		status, body, _ := s.get(repoPath("cargo-local") + "/index/config.json")
		if status != http.StatusOK {
			t.Fatalf("config.json status = %d", status)
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(body), &doc); err != nil {
			t.Fatalf("config.json body = %s (%v)", body, err)
		}
		if got := doc["dl"]; got != "https://binflow.example.test/binflow/cargo-local/v1/crates" {
			t.Errorf("dl = %v", got)
		}
		if got := doc["api"]; got != "https://binflow.example.test/binflow/cargo-local" {
			t.Errorf("api = %v", got)
		}
		if _, present := doc["auth-required"]; present {
			t.Errorf("auth-required must be absent on the anonymous-enabled instance: %s", body)
		}
	})

	t.Run("base url derived from the request", func(t *testing.T) {
		s := newStack(t)
		s.seedRepo(t, "cargo-local", repo.TypeLocal)
		status, body, _ := s.get(repoPath("cargo-local") + "/index/config.json")
		if status != http.StatusOK {
			t.Fatalf("config.json status = %d", status)
		}
		origin := s.srv.URL // the httptest origin the request actually hit
		if !strings.Contains(body, `"dl":"`+origin+`/binflow/cargo-local/v1/crates"`) {
			t.Errorf("dl not request-derived: %s", body)
		}
		if !strings.Contains(body, `"api":"`+origin+`/binflow/cargo-local"`) {
			t.Errorf("api not request-derived: %s", body)
		}
	})
}

// TestConfigJSONAnonymousOff: with the global anonymous read disabled, a
// bare config.json fetch answers 401 (cargo's retry-then-authenticate
// handshake) and an authenticated fetch answers 200 with auth-required.
func TestConfigJSONAnonymousOff(t *testing.T) {
	s := newStackOpt(t, stackOptions{anonymous: false})
	s.seedRepo(t, "cargo-local", repo.TypeLocal)

	status, _, hdr := s.get(repoPath("cargo-local") + "/index/config.json")
	if status != http.StatusUnauthorized {
		t.Fatalf("bare config.json status = %d, want 401", status)
	}
	if hdr.Get("WWW-Authenticate") == "" {
		t.Errorf("401 must carry the Basic challenge")
	}

	status, body, _ := s.do(http.MethodGet, repoPath("cargo-local")+"/index/config.json", adminUser, adminPass, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("authenticated config.json status = %d (body %s)", status, body)
	}
	if !strings.Contains(body, `"auth-required":true`) {
		t.Errorf("config = %s, want auth-required:true", body)
	}
}

// TestPublishChain: the full local chain — publish 200, the index line
// appears with cksum == the storage-measured sha256 == the downloaded
// bytes' sha256 (the AC's reconciliation anchor), the node carries the
// protocol properties, the sidecar holds the verbatim frame.
func TestPublishChain(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)

	crate := fixtureCrate("mycrate", "0.1.0")
	status, body, hdr := s.publish(t, "cargo-local", fixtureMeta("mycrate", "0.1.0"), crate)
	if status != http.StatusOK {
		t.Fatalf("publish status = %d (body %s)", status, body)
	}
	if !strings.Contains(body, `"warnings"`) || !strings.Contains(body, `"invalid_categories":[]`) {
		t.Errorf("publish body = %s, want the official warnings shape", body)
	}
	if strings.Contains(body, `"errors"`) {
		t.Errorf("publish body = %s: a present errors key (even empty) is cargo's failure form — omit it", body)
	}
	measured := sha256hex(crate)
	if got := hdr.Get("X-Checksum-Sha256"); got != measured {
		t.Errorf("publish X-Checksum-Sha256 = %q, want the measured %q", got, measured)
	}

	// The index file appears at the derived path, one NDJSON row.
	status, body, hdr = s.get(repoPath("cargo-local") + "/index/my/cr/mycrate")
	if status != http.StatusOK {
		t.Fatalf("index file status = %d (body %s)", status, body)
	}
	if ct := hdr.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("index Content-Type = %q", ct)
	}
	rows := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	if len(rows) != 1 {
		t.Fatalf("index rows = %d, want 1 (body %q)", len(rows), body)
	}
	var line struct {
		Name     string         `json:"name"`
		Vers     string         `json:"vers"`
		Cksum    string         `json:"cksum"`
		Yanked   bool           `json:"yanked"`
		Deps     []any          `json:"deps"`
		Features map[string]any `json:"features"`
		Links    string         `json:"links"`
	}
	if err := json.Unmarshal([]byte(rows[0]), &line); err != nil {
		t.Fatalf("index row %q: %v", rows[0], err)
	}
	if line.Name != "mycrate" || line.Vers != "0.1.0" || line.Yanked {
		t.Errorf("index row identity = %+v", line)
	}
	if len(line.Deps) != 1 || line.Deps[0].(map[string]any)["name"] != "serde" {
		t.Errorf("deps did not round-trip: %v", line.Deps)
	}
	if len(line.Features) != 1 || line.Links != "repo" {
		t.Errorf("features/links did not round-trip: %v %q", line.Features, line.Links)
	}
	// THE reconciliation anchor: index cksum == storage-measured sha256.
	if line.Cksum != measured {
		t.Fatalf("index cksum = %s, want the measured sha256 %s", line.Cksum, measured)
	}

	// Download: byte-identical, and shasum(download) == cksum.
	status, body, _ = s.get(repoPath("cargo-local") + "/v1/crates/mycrate/0.1.0/download")
	if status != http.StatusOK || body != string(crate) {
		t.Fatalf("download = (%d, %d bytes), want the published bytes", status, len(body))
	}
	if sha256hex([]byte(body)) != line.Cksum {
		t.Fatalf("downloaded sha256 = %s, want the index cksum %s", sha256hex([]byte(body)), line.Cksum)
	}

	// The node properties (spec section 4).
	props, err := s.md.NodeProps().List(t.Context(), "cargo-local", cratePath("mycrate", "0.1.0"))
	if err != nil {
		t.Fatalf("props list: %v", err)
	}
	for key, want := range map[string]string{
		propName: "mycrate", propVersion: "0.1.0",
		propDescription: "demo crate", propKeywords: "demo;test", propCategories: "cli",
	} {
		if got := firstProp(props, key); got != want {
			t.Errorf("property %s = %q, want %q", key, got, want)
		}
	}
	if _, yanked := props[propYanked]; yanked {
		t.Errorf("fresh publish must not carry crate.yanked")
	}
}

// TestPublishDuplicate409: the same name+version refuses 409 — both the
// exact retransmit and the build-metadata variant (CG-3).
func TestPublishDuplicate409(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)

	if status, body, _ := s.publish(t, "cargo-local", fixtureMeta("dup", "0.1.0"), fixtureCrate("dup", "0.1.0")); status != http.StatusOK {
		t.Fatalf("first publish = %d (%s)", status, body)
	}
	status, body, _ := s.publish(t, "cargo-local", fixtureMeta("dup", "0.1.0"), fixtureCrate("dup", "0.1.0"))
	if status != http.StatusConflict {
		t.Fatalf("exact duplicate = %d (%s), want 409", status, body)
	}
	if !strings.Contains(body, `"errors":[{"detail":`) {
		t.Errorf("409 body = %s, want the errors envelope", body)
	}
	status, body, _ = s.publish(t, "cargo-local", fixtureMeta("dup", "0.1.0+build.7"), fixtureCrate("dup", "0.1.0+build.7"))
	if status != http.StatusConflict {
		t.Fatalf("build-metadata duplicate = %d (%s), want 409 (build metadata does not distinguish versions)", status, body)
	}
}

// TestPublishFailureFamily: malformed framing and metadata answer the
// 400 + envelope (CG-2's client arm); nothing lands.
func TestPublishFailureFamily(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)

	cases := []struct {
		name string
		body []byte
	}{
		{"truncated frame", publishBody(fixtureMeta("bad", "0.1.0"), fixtureCrate("bad", "0.1.0"))[:12]},
		{"trailing byte", append(publishBody(fixtureMeta("bad", "0.1.0"), fixtureCrate("bad", "0.1.0")), 'x')},
		{"not json", publishBody("nonsense", fixtureCrate("bad", "0.1.0"))},
		{"bad name", publishBody(`{"name":"1bad","vers":"0.1.0"}`, fixtureCrate("bad", "0.1.0"))},
		{"bad version", publishBody(`{"name":"bad","vers":"0.1"}`, fixtureCrate("bad", "0.1.0"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body, _ := s.put(repoPath("cargo-local")+"/api/v1/crates/new", tc.body, nil)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d (body %s), want 400", status, body)
			}
			if !strings.Contains(body, `"errors":[{"detail":`) {
				t.Errorf("body = %s, want the errors envelope", body)
			}
		})
	}
	nodes, err := s.md.Nodes().ListByPrefix(t.Context(), "cargo-local", "crates")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, n := range nodes {
		if n.Path != "crates/" {
			t.Errorf("refused publishes must land nothing; node %q survived", n.Path)
		}
	}
}

// TestDownload404Body: the pinned refusal body (spec section 2).
func TestDownload404Body(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	status, body, _ := s.get(repoPath("cargo-local") + "/v1/crates/nosuch/9.9.9/download")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
	if body != `{"errors":[{"detail":"unable to download crate"}]}` {
		t.Fatalf("body = %s, want the pinned unable-to-download-crate envelope", body)
	}
}

// TestIndexConditionalRequests: ETag = the file's sha256; If-None-Match
// answers 304; the config document carries its own validator too.
func TestIndexConditionalRequests(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	if status, body, _ := s.publish(t, "cargo-local", fixtureMeta("etag", "0.1.0"), fixtureCrate("etag", "0.1.0")); status != http.StatusOK {
		t.Fatalf("publish = %d (%s)", status, body)
	}

	_, _, hdr := s.get(repoPath("cargo-local") + "/index/et/ag/etag")
	etag := hdr.Get("ETag")
	if etag == "" || !strings.HasPrefix(etag, `"`) {
		t.Fatalf("ETag = %q, want the quoted sha256", etag)
	}
	// The quoted validator IS the file's sha256 (the raw-store listing is
	// trailing-slash sensitive — svc.List is the normalizing wrapper).
	nodes, err := s.md.Nodes().ListByPrefix(t.Context(), "cargo-local", "index")
	if err != nil {
		t.Fatalf("list index nodes: %v", err)
	}
	var fileSHA string
	for _, n := range nodes {
		if n.Path == "index/et/ag/etag" {
			fileSHA = n.Sha256
		}
	}
	if fileSHA == "" {
		t.Fatalf("index file node missing; nodes = %+v", nodes)
	}
	if got := `"` + fileSHA + `"`; got != etag {
		t.Errorf("ETag = %s, want the node sha256 %s", etag, got)
	}

	rstatus, _, _ := s.do(http.MethodGet, repoPath("cargo-local")+"/index/et/ag/etag", "", "", nil,
		map[string]string{"If-None-Match": etag})
	if rstatus != http.StatusNotModified {
		t.Errorf("If-None-Match hit = %d, want 304", rstatus)
	}
	rstatus, _, _ = s.do(http.MethodGet, repoPath("cargo-local")+"/index/et/ag/etag", "", "", nil,
		map[string]string{"If-None-Match": `"deadbeef"`})
	if rstatus != http.StatusOK {
		t.Errorf("If-None-Match miss = %d, want 200", rstatus)
	}

	// config.json validator.
	_, _, chdr := s.get(repoPath("cargo-local") + "/index/config.json")
	if chdr.Get("ETag") == "" {
		t.Errorf("config.json must carry an ETag")
	}
	cstatus, _, _ := s.do(http.MethodGet, repoPath("cargo-local")+"/index/config.json", "", "", nil,
		map[string]string{"If-None-Match": chdr.Get("ETag")})
	if cstatus != http.StatusNotModified {
		t.Errorf("config.json If-None-Match = %d, want 304", cstatus)
	}
}

// TestIndex404: an absent index file answers 404 + envelope.
func TestIndex404(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	status, body, _ := s.get(repoPath("cargo-local") + "/index/my/cr/nosuch")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
	if !strings.Contains(body, `"errors":[{"detail":`) {
		t.Errorf("body = %s, want the errors envelope", body)
	}
}

// TestYankUnyank: the flag flips the index row only; unyank flips it
// back; an unknown crate/version answers 404 + envelope (TL-6); the
// blob keeps serving throughout (yank is not delete).
func TestYankUnyank(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	crate := fixtureCrate("yankme", "0.1.0")
	if status, body, _ := s.publish(t, "cargo-local", fixtureMeta("yankme", "0.1.0"), crate); status != http.StatusOK {
		t.Fatalf("publish = %d (%s)", status, body)
	}

	// Unknown crate and unknown version: 404 + envelope.
	status, body, _ := s.delete(repoPath("cargo-local") + "/api/v1/crates/nosuch/0.1.0/yank")
	if status != http.StatusNotFound || !strings.Contains(body, `"errors":`) {
		t.Fatalf("yank unknown crate = (%d, %s), want 404 + envelope", status, body)
	}
	status, _, _ = s.delete(repoPath("cargo-local") + "/api/v1/crates/yankme/9.9.9/yank")
	if status != http.StatusNotFound {
		t.Fatalf("yank unknown version = %d, want 404", status)
	}

	// Yank: {"ok":true}, the index row flips, the property lands.
	status, body, _ = s.delete(repoPath("cargo-local") + "/api/v1/crates/yankme/0.1.0/yank")
	if status != http.StatusOK || body != `{"ok":true}` {
		t.Fatalf("yank = (%d, %s), want (200, {\"ok\":true})", status, body)
	}
	status, body, _ = s.get(repoPath("cargo-local") + "/index/ya/nk/yankme")
	if status != http.StatusOK || !strings.Contains(body, `"yanked":true`) {
		t.Fatalf("post-yank index = (%d, %s)", status, body)
	}
	props, err := s.md.NodeProps().List(t.Context(), "cargo-local", cratePath("yankme", "0.1.0"))
	if err != nil || firstProp(props, propYanked) != "true" {
		t.Errorf("yanked property = %v (%v)", props, err)
	}

	// The blob keeps serving (a locked Cargo.lock build must not break).
	status, body, _ = s.get(repoPath("cargo-local") + "/v1/crates/yankme/0.1.0/download")
	if status != http.StatusOK || body != string(crate) {
		t.Fatalf("post-yank download = (%d, %d bytes), want the blob", status, len(body))
	}

	// Unyank: PUT, the row flips back, the property is gone.
	status, body, _ = s.put(repoPath("cargo-local")+"/api/v1/crates/yankme/0.1.0/unyank", nil, nil)
	if status != http.StatusOK || body != `{"ok":true}` {
		t.Fatalf("unyank = (%d, %s)", status, body)
	}
	status, body, _ = s.get(repoPath("cargo-local") + "/index/ya/nk/yankme")
	if status != http.StatusOK || !strings.Contains(body, `"yanked":false`) {
		t.Fatalf("post-unyank index = (%d, %s)", status, body)
	}
	props, err = s.md.NodeProps().List(t.Context(), "cargo-local", cratePath("yankme", "0.1.0"))
	if err != nil {
		t.Fatalf("props list: %v", err)
	}
	if _, still := props[propYanked]; still {
		t.Errorf("unyank must remove the crate.yanked property: %v", props)
	}
}

// TestYankWrongMethod: the verbs are pinned (DELETE yank, PUT unyank).
func TestYankWrongMethod(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	status, _, hdr := s.put(repoPath("cargo-local")+"/api/v1/crates/x/0.1.0/yank", nil, nil)
	if status != http.StatusMethodNotAllowed || hdr.Get("Allow") != http.MethodDelete {
		t.Fatalf("PUT yank = %d (Allow %q), want 405/DELETE", status, hdr.Get("Allow"))
	}
}

// TestSearchContract: the official shape, per_page bounds, yanked
// exclusion, the name/description wildcard, per-crate dedup at the
// highest non-yanked version.
func TestSearchContract(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)

	pub := func(name, vers, desc string) {
		t.Helper()
		meta := fmt.Sprintf(`{"name":%q,"vers":%q,"description":%q}`, name, vers, desc)
		if status, body, _ := s.publish(t, "cargo-local", meta, fixtureCrate(name, vers)); status != http.StatusOK {
			t.Fatalf("publish %s %s = %d (%s)", name, vers, status, body)
		}
	}
	pub("alpha", "0.1.0", "the alpha demo crate")
	pub("alpha", "0.2.0", "the alpha demo crate")
	pub("beta", "1.0.0", "unrelated thing")
	pub("gamma", "0.1.0", "a matched description only")

	status, body, _ := s.get(repoPath("cargo-local") + "/api/v1/crates?q=alpha&per_page=10")
	if status != http.StatusOK {
		t.Fatalf("search = %d (%s)", status, body)
	}
	var resp struct {
		Crates []struct {
			Name        string `json:"name"`
			MaxVersion  string `json:"max_version"`
			Description string `json:"description"`
		} `json:"crates"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("search body = %s (%v)", body, err)
	}
	if resp.Meta.Total != 1 || len(resp.Crates) != 1 {
		t.Fatalf("search total/hits = %d/%d, want 1/1 (body %s)", resp.Meta.Total, len(resp.Crates), body)
	}
	if resp.Crates[0].Name != "alpha" || resp.Crates[0].MaxVersion != "0.2.0" || resp.Crates[0].Description != "the alpha demo crate" {
		t.Errorf("hit = %+v", resp.Crates[0])
	}

	// Description-side wildcard.
	status, body, _ = s.get(repoPath("cargo-local") + "/api/v1/crates?q=matched+description")
	if status != http.StatusOK || !strings.Contains(body, `"name":"gamma"`) {
		t.Fatalf("description match = (%d, %s)", status, body)
	}

	// Yanked versions never surface; the max version falls back to the
	// highest non-yanked one.
	if status, body, _ := s.delete(repoPath("cargo-local") + "/api/v1/crates/alpha/0.2.0/yank"); status != http.StatusOK {
		t.Fatalf("yank = %d (%s)", status, body)
	}
	status, body, _ = s.get(repoPath("cargo-local") + "/api/v1/crates?q=alpha")
	if status != http.StatusOK {
		t.Fatalf("post-yank search = %d (%s)", status, body)
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("post-yank body = %s (%v)", body, err)
	}
	if resp.Crates[0].MaxVersion != "0.1.0" {
		t.Errorf("post-yank hit = %s, want max_version 0.1.0", body)
	}

	// per_page: default 10, ceiling 100, garbage falls back.
	status, body, _ = s.get(repoPath("cargo-local") + "/api/v1/crates?per_page=banana")
	if status != http.StatusOK || !strings.Contains(body, `"meta":{"total":`) {
		t.Fatalf("garbage per_page = (%d, %s)", status, body)
	}
}

// TestUnknownPaths: owners (four endpoints), init and the rest of the
// unimplemented family answer the unknown-path 404 + envelope.
func TestUnknownPaths(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	for _, path := range []string{
		"/api/v1/crates/mycrate/owners",
		"/api/v1/crates/mycrate/owners/user/admin",
		"/api/v1/crates/me",
		"/api/v1/crates/init",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPost} {
			status, body, _ := s.do(method, repoPath("cargo-local")+path, adminUser, adminPass, nil, nil)
			if status != http.StatusNotFound || !strings.Contains(body, `"errors":`) {
				t.Fatalf("%s %s = (%d, %s), want 404 + envelope", method, path, status, body)
			}
		}
	}
}

// TestGitIndexFace: the deprecated git protocol shapes answer the
// spec's deprecation wording.
func TestGitIndexFace(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	for _, path := range []string{"/info/refs", "/git-upload-pack"} {
		status, body, _ := s.do(http.MethodGet, repoPath("cargo-local")+path+"?service=git-upload-pack", adminUser, adminPass, nil, nil)
		if status != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", path, status)
		}
		if !strings.Contains(body, "Git index is deprecated") || !strings.Contains(body, "sparse") {
			t.Errorf("git face body = %s, want the deprecation wording", body)
		}
	}
}

// TestRemoteVirtualRefused: the non-local classes answer the honest 404
// until their own M11 tickets land (remote = S4, virtual = S5).
func TestRemoteVirtualRefused(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	s.seedRepo(t, "cargo-remote", repo.TypeRemote)
	s.seedRepo(t, "cargo-virtual", repo.TypeVirtual)
	s.seedVirtualMembers(t, "cargo-virtual", "cargo-local")
	status, body, _ := s.get(repoPath("cargo-remote") + "/index/config.json")
	if status != http.StatusNotFound || !strings.Contains(body, "remote") {
		t.Fatalf("remote config.json = (%d, %s), want the class 404", status, body)
	}
	status, _, _ = s.get(repoPath("cargo-virtual") + "/index/config.json")
	if status != http.StatusNotFound {
		t.Fatalf("virtual config.json = %d, want 404", status)
	}
}

// TestBareContentFace: the debug face serves the stored nodes, refuses
// direct writes onto the server-owned prefixes, and 201s elsewhere.
func TestBareContentFace(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	if status, body, _ := s.publish(t, "cargo-local", fixtureMeta("bare", "0.1.0"), fixtureCrate("bare", "0.1.0")); status != http.StatusOK {
		t.Fatalf("publish = %d (%s)", status, body)
	}

	status, body, _ := s.get(repoPath("cargo-local") + "/crates/bare/bare-0.1.0.crate")
	if status != http.StatusOK || !strings.HasPrefix(body, "GZIP-PLACEHOLDER") {
		t.Fatalf("bare GET = (%d, %s)", status, body)
	}

	status, body, _ = s.put(repoPath("cargo-local")+"/index/my/cr/handwritten", []byte("forged row\n"), nil)
	if status != http.StatusForbidden || !strings.Contains(body, "server-generated") {
		t.Fatalf("index PUT = (%d, %s), want 403 + the server-generated wording", status, body)
	}
	status, body, _ = s.put(repoPath("cargo-local")+"/.cargo/crates/bare/bare-0.1.0.json", []byte("{}"), nil)
	if status != http.StatusForbidden {
		t.Fatalf("sidecar PUT = (%d, %s), want 403", status, body)
	}

	status, _, hdr := s.put(repoPath("cargo-local")+"/docs/readme.txt", []byte("hello"), nil)
	if status != http.StatusCreated || hdr.Get("Location") != "docs/readme.txt" {
		t.Fatalf("plain bare PUT = (%d, %s), want 201 + Location", status, hdr.Get("Location"))
	}
}

// TestSidecarBackfillIndexRewrite: a second publish of the SAME crate
// rewrites the whole file — two rows, semver-ordered, each with its own
// measured cksum (the whole-file rewrite, spec section 3.3).
func TestSidecarBackfillIndexRewrite(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)

	pub := func(name, vers string) []byte {
		t.Helper()
		crate := fixtureCrate(name, vers)
		if status, body, _ := s.publish(t, "cargo-local", fixtureMeta(name, vers), crate); status != http.StatusOK {
			t.Fatalf("publish %s = %d (%s)", vers, status, body)
		}
		return crate
	}
	c1 := pub("multi", "0.2.0")
	c2 := pub("multi", "0.1.0")

	_, body, _ := s.get(repoPath("cargo-local") + "/index/mu/lt/multi")
	rows := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	if len(rows) != 2 {
		t.Fatalf("rows = %d (body %q), want 2", len(rows), body)
	}
	var first, second struct {
		Vers  string `json:"vers"`
		Cksum string `json:"cksum"`
	}
	if err := json.Unmarshal([]byte(rows[0]), &first); err != nil {
		t.Fatalf("row 0: %v", err)
	}
	if err := json.Unmarshal([]byte(rows[1]), &second); err != nil {
		t.Fatalf("row 1: %v", err)
	}
	if first.Vers != "0.1.0" || second.Vers != "0.2.0" {
		t.Errorf("row order = %s, %s; want semver ascending", first.Vers, second.Vers)
	}
	if first.Cksum != sha256hex(c2) || second.Cksum != sha256hex(c1) {
		t.Errorf("cksums = %s/%s, want the per-version measured values", first.Cksum, second.Cksum)
	}
}

// TestIndexLineImportBackfill: a bare-imported crate (no sidecar — the
// external-import shape) still gets a row on the next rewrite, with an
// empty dep set and feature map (spec section 4's backfill note).
func TestIndexLineImportBackfill(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	// Import via the bare face: blob only, no sidecar, no properties.
	crate := fixtureCrate("imported", "1.0.0")
	status, _, _ := s.put(repoPath("cargo-local")+"/"+cratePath("imported", "1.0.0"), crate, nil)
	if status != http.StatusCreated {
		t.Fatalf("import = %d, want 201", status)
	}
	// Trigger the rewrite through a yank/unyank round-trip.
	status, yankBody, _ := s.delete(repoPath("cargo-local") + "/api/v1/crates/imported/1.0.0/yank")
	if status != http.StatusOK {
		t.Fatalf("yank = %d (%s)", status, yankBody)
	}
	status, unyankBody, _ := s.put(repoPath("cargo-local")+"/api/v1/crates/imported/1.0.0/unyank", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("unyank = %d (%s)", status, unyankBody)
	}
	_, indexBody, _ := s.get(repoPath("cargo-local") + "/index/im/po/imported")
	if !strings.Contains(indexBody, `"vers":"1.0.0"`) || !strings.Contains(indexBody, `"deps":[]`) || !strings.Contains(indexBody, `"yanked":false`) {
		t.Fatalf("backfilled row = %s, want vers + empty deps + unyanked", indexBody)
	}
	if !strings.Contains(indexBody, `"cksum":"`+sha256hex(crate)+`"`) {
		t.Fatalf("backfilled cksum = %s, want the imported blob's sha256", indexBody)
	}
}

// TestAnonymousWriteRefused: publish without a credential answers 401
// (the middleware's write door — cargo then authenticates).
func TestAnonymousWriteRefused(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	body := publishBody(fixtureMeta("anon", "0.1.0"), fixtureCrate("anon", "0.1.0"))
	status, respBody, hdr := s.do(http.MethodPut, repoPath("cargo-local")+"/api/v1/crates/new", "", "", bytes.NewReader(body), nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous publish = %d (%s), want 401", status, respBody)
	}
	if hdr.Get("WWW-Authenticate") == "" {
		t.Errorf("401 must carry the Basic challenge")
	}
}

// indexRowsOf fetches and parses the crate's index file; every row's
// (vers, cksum, yanked) is returned. The concurrency legs' oracle.
func indexRowsOf(t *testing.T, s *stack, repoKey, name string) map[string]struct {
	cksum  string
	yanked bool
} {
	t.Helper()
	status, body, _ := s.get(repoPath(repoKey) + "/index/" + indexPath(name))
	if status != http.StatusOK {
		t.Fatalf("index file of %s = %d (body %s)", name, status, body)
	}
	rows := map[string]struct {
		cksum  string
		yanked bool
	}{}
	for _, row := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
		if row == "" {
			continue
		}
		var line struct {
			Vers   string `json:"vers"`
			Cksum  string `json:"cksum"`
			Yanked bool   `json:"yanked"`
		}
		if err := json.Unmarshal([]byte(row), &line); err != nil {
			t.Fatalf("index row %q: %v", row, err)
		}
		if _, dup := rows[line.Vers]; dup {
			t.Fatalf("version %s appears twice in the index (body %s)", line.Vers, body)
		}
		rows[line.Vers] = struct {
			cksum  string
			yanked bool
		}{line.Cksum, line.Yanked}
	}
	return rows
}

// TestConcurrentPublishIndexIntegrity: B1's regression leg — N concurrent
// publishes of one crate's N versions must leave N rows with the right
// per-version cksums. The pre-fix lost-update race silently dropped rows
// (review probe: 8 publishes → 2 rows); the whole critical section is
// serialized per (repoKey, crate), and every publish lands its .crate
// node BEFORE the rewrite, so the last writer's snapshot is complete.
func TestConcurrentPublishIndexIntegrity(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)

	const versions = 8
	for round := 0; round < 3; round++ { // raise the catch odds for a reintroduced race
		crate := fmt.Sprintf("racer%d", round)
		var wg sync.WaitGroup
		errs := make(chan string, versions)
		for i := 0; i < versions; i++ {
			vers := fmt.Sprintf("0.%d.0", i+1)
			wg.Add(1)
			go func() {
				defer wg.Done()
				status, body, _ := s.publish(t, "cargo-local", fixtureMeta(crate, vers), fixtureCrate(crate, vers))
				if status != http.StatusOK {
					errs <- fmt.Sprintf("publish %s %s = %d (%s)", crate, vers, status, body)
				}
			}()
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			t.Error(e)
		}
		if t.Failed() {
			t.FailNow()
		}

		rows := indexRowsOf(t, s, "cargo-local", crate)
		if len(rows) != versions {
			t.Fatalf("round %d: index rows = %d, want %d (the B1 lost-update signature)", round, len(rows), versions)
		}
		for i := 0; i < versions; i++ {
			vers := fmt.Sprintf("0.%d.0", i+1)
			row, ok := rows[vers]
			if !ok {
				t.Fatalf("round %d: version %s missing from the index", round, vers)
			}
			if want := sha256hex(fixtureCrate(crate, vers)); row.cksum != want {
				t.Errorf("round %d: %s cksum = %s, want the measured %s", round, vers, row.cksum, want)
			}
			if row.yanked {
				t.Errorf("round %d: %s must not be yanked", round, vers)
			}
		}
	}
}

// TestConcurrentYankPublishMix: the mixed-mutation leg — concurrent
// publish + yank + unyank on one crate. Determinism argument: every
// request lands its own state change (node or property) BEFORE entering
// the serialized rewrite, so whichever rewrite runs last observes ALL
// completed mutations — the final file must carry every version with the
// yank flags of the completed operations.
func TestConcurrentYankPublishMix(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)

	// Seed two versions sequentially (the mixed phase's baseline).
	for _, vers := range []string{"0.1.0", "0.2.0"} {
		if status, body, _ := s.publish(t, "cargo-local", fixtureMeta("mixed", vers), fixtureCrate("mixed", vers)); status != http.StatusOK {
			t.Fatalf("seed publish %s = %d (%s)", vers, status, body)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan string, 3)
	must := func(what string, status int, body string) {
		if status != http.StatusOK {
			errs <- fmt.Sprintf("%s = %d (%s)", what, status, body)
		}
	}
	wg.Add(3)
	go func() { // publish a third version
		defer wg.Done()
		status, body, _ := s.publish(t, "cargo-local", fixtureMeta("mixed", "0.3.0"), fixtureCrate("mixed", "0.3.0"))
		must("concurrent publish 0.3.0", status, body)
	}()
	go func() { // yank the first version
		defer wg.Done()
		status, body, _ := s.delete(repoPath("cargo-local") + "/api/v1/crates/mixed/0.1.0/yank")
		must("concurrent yank 0.1.0", status, body)
	}()
	go func() { // unyank the second (already unyanked — idempotent flip)
		defer wg.Done()
		status, body, _ := s.put(repoPath("cargo-local")+"/api/v1/crates/mixed/0.2.0/unyank", nil, nil)
		must("concurrent unyank 0.2.0", status, body)
	}()
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if t.Failed() {
		t.FailNow()
	}

	rows := indexRowsOf(t, s, "cargo-local", "mixed")
	if len(rows) != 3 {
		t.Fatalf("mixed index rows = %d (%v), want 3", len(rows), rows)
	}
	if !rows["0.1.0"].yanked {
		t.Errorf("0.1.0 yanked = false, want true (the yank completed before the last rewrite)")
	}
	if rows["0.2.0"].yanked || rows["0.3.0"].yanked {
		t.Errorf("unyanked versions flipped: %+v", rows)
	}
	for _, vers := range []string{"0.1.0", "0.2.0", "0.3.0"} {
		if want := sha256hex(fixtureCrate("mixed", vers)); rows[vers].cksum != want {
			t.Errorf("%s cksum = %s, want the measured %s", vers, rows[vers].cksum, want)
		}
	}
}
