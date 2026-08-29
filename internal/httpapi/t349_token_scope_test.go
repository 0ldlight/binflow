package httpapi_test

// T-349 (FR-113.3 / NFR-S63): the checksum-deploy token's permission domain
// is narrowed to the session's own landing. These legs drive the REAL chain
// — the mint in the finish task, the middleware's deployScopeGuard before
// routing — and assert the lateral-use matrix: the one admitted PUT, and
// 403 for everything else the same credential could otherwise reach.

import (
	"net/http"
	"testing"
)

// t349DriveToFinished opens a session on repo/path, uploads one short part,
// completes and polls to FINISHED, returning the checksum-deploy token and
// the corpus sha1.
func t349DriveToFinished(t *testing.T, h *harness, repo, path string) (depTok, wholeSHA1 string) {
	t.Helper()
	tok := mpuCreateToken(t, h, adminUser, adminPass, repo, path, 5)
	_, _, p3, _ := mpuPayload()
	if code, _ := mpuPutPart(t, h, tok, 1, p3); code != http.StatusOK {
		t.Fatalf("part PUT = %d, want 200", code)
	}
	wholeSHA1 = sha1Hex(t, p3)
	if code, _ := mpuPostToken(t, h, "complete", tok, "?sha1="+wholeSHA1); code != http.StatusAccepted {
		t.Fatalf("complete = %d, want 202", code)
	}
	done := mpuStatusPoll(t, h, tok, "FINISHED")
	depTok, ok := done["checksumToken"].(string)
	if !ok || depTok == "" {
		t.Fatalf("Finished status carries no checksum-deploy token: %v", done)
	}
	return depTok, wholeSHA1
}

// t349ScopedPut issues a PUT through the scoped token. checksumDeploy
// controls the X-Checksum-Deploy header ("" = absent); the sha1 rides along
// whenever the deploy header is present, so refusals come from the SCOPE,
// never from a missing checksum the plane would 400 on.
func t349ScopedPut(t *testing.T, h *harness, depTok, path, checksumDeploy, sha1 string) int {
	t.Helper()
	hdr := map[string]string{"Authorization": "Bearer " + depTok}
	if checksumDeploy != "" {
		hdr["X-Checksum-Deploy"] = checksumDeploy
		hdr["X-Checksum-Sha1"] = sha1
	}
	resp := h.do(http.MethodPut, path, "", "", nil, hdr)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// TestT349ChecksumDeployTokenNarrowMatrix: the minted token lands its own
// artifact and refuses every other use — other paths, other repos, reads,
// the management API, the MPU plane itself, and a plain (non-checksum)
// PUT on its own path.
func TestT349ChecksumDeployTokenNarrowMatrix(t *testing.T) {
	h, _ := newUploadsHarness(t)
	// A second repo to prove cross-repo refusal.
	resp := h.do(http.MethodPut, "/binflow/api/repositories/other-local", adminUser, adminPass,
		[]byte(`{"rclass":"local","packageType":"generic"}`), nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create other-local = %d", resp.StatusCode)
	}

	depTok, wholeSHA1 := t349DriveToFinished(t, h, "generic-local", "scoped/one.bin")

	// THE one admitted request: the scoped checksum-deploy PUT at the
	// session's own coordinates.
	if code := t349ScopedPut(t, h, depTok, "/binflow/generic-local/scoped/one.bin", "true", wholeSHA1); code != http.StatusCreated {
		t.Fatalf("own-path checksum-deploy PUT = %d, want 201", code)
	}

	// Same token, same checksum headers: a DIFFERENT path in the SAME repo.
	if code := t349ScopedPut(t, h, depTok, "/binflow/generic-local/scoped/elsewhere.bin", "true", wholeSHA1); code != http.StatusForbidden {
		t.Fatalf("other-path PUT = %d, want 403", code)
	}
	// A DIFFERENT repository.
	if code := t349ScopedPut(t, h, depTok, "/binflow/other-local/scoped/one.bin", "true", wholeSHA1); code != http.StatusForbidden {
		t.Fatalf("other-repo PUT = %d, want 403", code)
	}
	// Its own path WITHOUT the checksum-deploy header: not the scoped
	// operation, refused before the content plane sees it.
	if code := t349ScopedPut(t, h, depTok, "/binflow/generic-local/scoped/one.bin", "", wholeSHA1); code != http.StatusForbidden {
		t.Fatalf("own-path plain PUT = %d, want 403", code)
	}
	// A read of the artifact it just landed.
	resp = h.do(http.MethodGet, "/binflow/generic-local/scoped/one.bin", "", "", nil,
		map[string]string{"Authorization": "Bearer " + depTok})
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("scoped-token GET = %d, want 403", resp.StatusCode)
	}
	// The management API (an admin-power surface the OWNER could reach).
	resp = h.do(http.MethodGet, "/binflow/api/v1/users", "", "", nil,
		map[string]string{"Authorization": "Bearer " + depTok})
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("scoped-token management GET = %d, want 403", resp.StatusCode)
	}
	// The MPU plane's own status verb: the checksum token is NOT the
	// capability token, and it is not the scoped operation either.
	resp = h.do(http.MethodPost, "/binflow/api/v1/uploads/status", "", "", nil,
		map[string]string{"Authorization": "Bearer " + depTok})
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("scoped-token MPU status = %d, want 403", resp.StatusCode)
	}
	// The landed artifact is readable with ordinary credentials (the scope
	// narrowed the token, never the artifact).
	resp = h.do(http.MethodGet, "/binflow/generic-local/scoped/one.bin", adminUser, adminPass, nil, nil)
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("artifact GET after scoped deploy = %d, want 200", resp.StatusCode)
	}
}

// TestT349ChecksumDeployTokenVirtualDualSpelling: a session created through
// a virtual (defaultDeploymentRepo resolved at create) mints a token that
// admits BOTH spellings — the client's virtual AND the resolved member —
// and still refuses everything else.
func TestT349ChecksumDeployTokenVirtualDualSpelling(t *testing.T) {
	h, _ := newUploadsHarness(t)
	for _, leg := range []struct{ repo, body string }{
		{"gen-local", `{"rclass":"local","packageType":"generic"}`},
		{"gen-other", `{"rclass":"local","packageType":"generic"}`},
		{"gen-virtual", `{"rclass":"virtual","packageType":"generic","repositories":["gen-local","gen-other"],"defaultDeploymentRepo":"gen-local"}`},
	} {
		resp := h.do(http.MethodPut, "/binflow/api/repositories/"+leg.repo, adminUser, adminPass, []byte(leg.body), nil)
		func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create %s = %d", leg.repo, resp.StatusCode)
		}
	}

	depTok, sha1 := t349DriveToFinished(t, h, "gen-virtual", "dual.bin")

	// The client's spelling (the only one it knows): admitted.
	if code := t349ScopedPut(t, h, depTok, "/binflow/gen-virtual/dual.bin", "true", sha1); code != http.StatusCreated {
		t.Fatalf("virtual-spelling PUT = %d, want 201", code)
	}
	// The resolved member is a DIFFERENT session coordinate-wise but the
	// same landing: a second session proves the member spelling is
	// admitted for ITS OWN token too.
	depTok2, sha1b := t349DriveToFinished(t, h, "gen-virtual", "dual2.bin")
	if code := t349ScopedPut(t, h, depTok2, "/binflow/gen-local/dual2.bin", "true", sha1b); code != http.StatusCreated {
		t.Fatalf("resolved-member PUT = %d, want 201", code)
	}
	// The unrelated member repo is NOT admitted.
	if code := t349ScopedPut(t, h, depTok2, "/binflow/gen-other/dual2.bin", "true", sha1b); code != http.StatusForbidden {
		t.Fatalf("unrelated-member PUT = %d, want 403", code)
	}
	// And the first token cannot reach the second session's path.
	if code := t349ScopedPut(t, h, depTok, "/binflow/gen-virtual/dual2.bin", "true", sha1); code != http.StatusForbidden {
		t.Fatalf("cross-session same-repo PUT = %d, want 403", code)
	}
}
