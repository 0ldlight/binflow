package remote

// T-442 (FR-143.5) — the remote repository Test probe: the three PRD arms
// (correct credentials succeed / wrong credentials fail inline /
// unreachable presents the transport family), the registry-root 404 pass
// ruling, the draft-override discipline and the ZERO-side-effect contract
// (one read-only GET per probe, nothing written anywhere, and a transport
// fault deliberately does NOT open the assumed-offline window a
// pull-through fault would).

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// probeRepositoryUpstream is the package function under test, unwrapped.
func probeRepositoryUpstream(t *testing.T, e *fetchEnv, repoKey string, ov UpstreamOverride) (TestResult, error) {
	t.Helper()
	res, err := TestRepositoryUpstream(context.Background(), e.md, repoKey, ov)
	if err != nil {
		return res, err
	}
	if res.Message == "" {
		t.Fatalf("probe verdict carries no message: %+v", res)
	}
	return res, nil
}

// TestRepositoryUpstreamThreeArms walks FR-143.5's arms plus the verdict
// families around them.
func TestRepositoryUpstreamThreeArms(t *testing.T) {
	e := newFetchEnv(t, func(_ *metadata.Repo, cfg *metadata.RemoteConfig) {
		cfg.Username, cfg.Password = "u", "p"
	})
	e.state.files["/"] = "upstream root"
	ctx := context.Background()

	// Arm 1 — correct stored credentials: reachable + accepted.
	res, err := probeRepositoryUpstream(t, e, "generic-remote", UpstreamOverride{})
	if err != nil {
		t.Fatalf("stored-config probe: %v", err)
	}
	if !res.OK || res.StatusCode != http.StatusOK || !strings.Contains(res.Message, "tested successfully") {
		t.Fatalf("stored-config probe = %+v, want ok/200/success wording", res)
	}
	if got := e.hits.Load(); got != 1 {
		t.Fatalf("upstream contacts = %d, want 1", got)
	}

	// Wrong credentials (draft override with a bad pair): the inline
	// failure, HTTP-verdict shaped — never an error return.
	e.state.user, e.state.pass = "u", "p"
	e.state.set("auth")
	res, err = probeRepositoryUpstream(t, e, "generic-remote", UpstreamOverride{Username: "u", Password: "wrong"})
	if err != nil {
		t.Fatalf("wrong-credential probe: %v", err)
	}
	if res.OK || res.StatusCode != http.StatusUnauthorized || !strings.Contains(res.Message, "returned error 401") {
		t.Fatalf("wrong-credential probe = %+v, want refused/401/inline reason", res)
	}

	// Unreachable: the transport family names the failed connection.
	res, err = probeRepositoryUpstream(t, e, "generic-remote", UpstreamOverride{URL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("unreachable probe: %v", err)
	}
	if res.OK || !strings.Contains(res.Message, "connection failed") {
		t.Fatalf("unreachable probe = %+v, want the transport family", res)
	}

	// A registry-family root (404 on the bare base) is a PASS: the probe's
	// question is reachability + authentication, not content.
	e.state.set("ok")
	delete(e.state.files, "/")
	res, err = probeRepositoryUpstream(t, e, "generic-remote", UpstreamOverride{})
	if err != nil {
		t.Fatalf("404-root probe: %v", err)
	}
	if !res.OK || res.StatusCode != http.StatusNotFound {
		t.Fatalf("404-root probe = %+v, want ok/404 (the registry-root ruling)", res)
	}

	// A malformed URL is a verdict, not an error.
	res, err = probeRepositoryUpstream(t, e, "generic-remote", UpstreamOverride{URL: "notaurl"})
	if err != nil {
		t.Fatalf("malformed-url probe: %v", err)
	}
	if res.OK || !strings.Contains(res.Message, "must be an absolute http/https URL") {
		t.Fatalf("malformed-url probe = %+v, want the url verdict", res)
	}

	// A partial override (URL only, no password) probes ANONYMOUS: the
	// stored secret never ships to a different candidate host. Against the
	// auth-gated upstream that arm fails exactly like anonymous must.
	e.state.user, e.state.pass = "u", "p"
	e.state.set("auth")
	res, err = probeRepositoryUpstream(t, e, "generic-remote", UpstreamOverride{URL: e.srv.URL})
	if err != nil {
		t.Fatalf("partial-override probe: %v", err)
	}
	if res.OK || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("partial-override probe = %+v, want the anonymous refusal", res)
	}

	// A non-remote repository is a verdict naming the class.
	now := time.Now().UTC().Format(time.RFC3339)
	if err := e.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "local-r", Type: "local", PackageType: "generic",
		Config: "{}", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed local repo: %v", err)
	}
	res, err = probeRepositoryUpstream(t, e, "local-r", UpstreamOverride{})
	if err != nil {
		t.Fatalf("local-repo probe: %v", err)
	}
	if res.OK || !strings.Contains(res.Message, "only remote repositories") {
		t.Fatalf("local-repo probe = %+v, want the class verdict", res)
	}
}

// TestRepositoryUpstreamZeroSideEffects pins the probe's state contract:
// every probe is exactly one read-only GET (no probe-side caching), a
// transport fault does NOT open the assumed-offline window, and nothing
// lands anywhere.
func TestRepositoryUpstreamZeroSideEffects(t *testing.T) {
	e := newFetchEnv(t, nil)
	e.state.files["/"] = "root"
	ctx := context.Background()

	nodesBefore := nodeCount(t, e, "generic-remote")
	for i := 0; i < 3; i++ {
		res, err := probeRepositoryUpstream(t, e, "generic-remote", UpstreamOverride{})
		if err != nil || !res.OK {
			t.Fatalf("probe %d = (%v, %+v)", i, err, res)
		}
	}
	if got := e.hits.Load(); got != 3 {
		t.Fatalf("upstream contacts for 3 probes = %d, want 3 (one GET each, no probe cache)", got)
	}

	// A transport-fault probe must not silence the pull path: the very
	// next engine fetch still contacts the upstream.
	if _, err := probeRepositoryUpstream(t, e, "generic-remote", UpstreamOverride{URL: "http://127.0.0.1:1"}); err != nil {
		t.Fatalf("transport-fault probe: %v", err)
	}
	e.state.files["/afile"] = "body"
	res, err := e.eng.Fetch(ctx, "generic-remote", "afile")
	if err != nil {
		t.Fatalf("fetch after transport-fault probe: %v (the probe must not open the offline window)", err)
	}
	defer res.Body.Close() //nolint:errcheck // read-only assertion stream
	// 4 loopback contacts: 3 probes + the fetch (the faulted probe aimed
	// at a different host and never reached this counter).
	if got := e.hits.Load(); got != 4 {
		t.Fatalf("upstream contacts = %d, want 4 (3 probes + 1 fetch)", got)
	}
	if after := nodeCount(t, e, "generic-remote"); after != nodesBefore+1 {
		t.Errorf("node rows = %d after probes + one fetch, want %d (the fetch's landing only)", after, nodesBefore+1)
	}
}

// TestRepositoryUpstreamSealedCredentialUnsealRefused: a stored credential
// that cannot be unsealed is the honest 500 family — the probe never
// pretends the upstream was contacted and never echoes sealed material.
func TestRepositoryUpstreamSealedCredentialUnsealRefused(t *testing.T) {
	e := newFetchEnv(t, func(_ *metadata.Repo, cfg *metadata.RemoteConfig) {
		cfg.Password = "enc:v1:not-actually-sealed"
	})
	_, err := probeRepositoryUpstream(t, e, "generic-remote", UpstreamOverride{})
	if !errors.Is(err, ErrTestCredentialUnsealed) {
		t.Fatalf("sealed-credential probe = %v, want ErrTestCredentialUnsealed", err)
	}
	if got := e.hits.Load(); got != 0 {
		t.Errorf("upstream contacts = %d, want 0 (never contacted)", got)
	}
	// A draft pair bypasses the sealed store entirely and reaches the
	// upstream (the create-form's test-before-save arm).
	e.state.user, e.state.pass = "u", "p"
	e.state.set("auth")
	res, err := probeRepositoryUpstream(t, e, "generic-remote", UpstreamOverride{Username: "u", Password: "p"})
	if err != nil || !res.OK {
		t.Fatalf("draft-pair probe = (%v, %+v), want ok", err, res)
	}
}
