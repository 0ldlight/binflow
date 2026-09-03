package remote

// The remote-repository Test probe (M16 T-442, FR-143.5 — the remote form's
// Test connectivity button), isomorphic to replication's Engine.TestTarget
// (T-422, replication.md §9.2-C/§9.3): the same verdict discipline lifted
// onto a repository's configured UPSTREAM instead of a push target.
//
// Contract (mirrored from the T-422 probe, semantics immutable):
//
//   - ZERO side effects: ONE read-only GET against the configured upstream
//     base URL, no state written anywhere — no node, no cache-state row, no
//     negative cache, and deliberately NO assumed-offline mark (a manual
//     operator probe must never silence the pull path — the engine's fault
//     classification owns that window). Credentials never reach any log or
//     message (NFR-S75).
//   - Pass criteria: the upstream answers and the credentials were not
//     refused — the 2xx/3xx family plus 404. A 404 root is deliberately a
//     PASS: registry-family upstreams (docker, helmoci) answer 404 on the
//     bare base by design, and the probe's question is reachability +
//     authentication, not content (the replication probe's stricter
//     200/302 rule targets a BinFlow storage mount that must answer 200;
//     this probe targets an arbitrary upstream root — the divergence is
//     this file's own ruling, registered in the T-442 report).
//   - Failure families: 401/403 = the credentials were refused; any other
//     non-pass status = "Connection failed: Remote repository URL
//     returned error <status>: <reason>"; a transport/DNS fault carries
//     the §9.2-C-8 shape adapted to the repository word. The REST face
//     renders every OK=false verdict as the SAME result body at HTTP 400 —
//     the inline reason IS the payload (the auth.config.test posture).
//   - Redirects are NOT followed (the first 30x is final): the probe tests
//     the CONFIGURED address, not wherever an upstream might bounce it.
//
// Error discipline: an error return is reserved for FACE faults the caller
// maps onto 5xx (a stored credential that cannot be unsealed — never
// leaking the sealed text; a store read failure). Every probe verdict,
// pass or fail, is a TestResult with a nil error.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// UpstreamOverride carries the draft-test arms (the remote form's Test
// button on an UNSAVED edit): every non-empty field replaces the stored
// config's value for THIS probe alone — nothing is written. Password is
// plaintext from the request body, never stored and never logged.
type UpstreamOverride struct {
	URL      string
	Username string
	Password string
}

// TestResult is the probe verdict the REST face serializes (the T-422
// shape: ok + status_code + message; failures ride the same body at 400).
type TestResult struct {
	OK         bool
	StatusCode int
	Message    string
}

// ErrTestCredentialUnsealed marks a stored password the cipher cannot open
// (no master key configured, or the wrong one): the honest 500 family —
// never leak the sealed text, never pretend the upstream was contacted.
var ErrTestCredentialUnsealed = errors.New("remote: repository credential could not be unsealed for the test probe")

// probeSnippetBytes bounds the failure-reason snippet read from an
// upstream error body (the reason line, not the document).
const probeSnippetBytes = 256

// TestRepositoryUpstream probes one remote repository's configured
// upstream (plus optional draft overrides) with the same egress posture a
// pull-through fetch takes — the repository's guarded client construction
// (SSRF chain, socket timeout, credential spelling) — minus every write.
// It is a package function, not an Engine method, because the engine is
// assembled INSIDE repo.New (architecture section 5.4): the REST face
// holds the metadata store and nothing else, and a second engine instance
// would double-claim the cache plane.
func TestRepositoryUpstream(ctx context.Context, md metadata.Store, repoKey string, ov UpstreamOverride) (TestResult, error) {
	if md == nil {
		return TestResult{}, fmt.Errorf("remote test %s: metadata store is required", repoKey)
	}
	row, err := md.Repos().Get(ctx, repoKey)
	if err != nil {
		return TestResult{}, fmt.Errorf("remote test %s: load repository: %w", repoKey, err)
	}
	if row.Type != "remote" {
		return TestResult{Message: fmt.Sprintf(
			"Repository '%s' is a %s repository; only remote repositories have an upstream to test.", repoKey, row.Type)}, nil
	}
	cfg, err := md.Remote().GetConfig(ctx, repoKey)
	if err != nil {
		return TestResult{}, fmt.Errorf("remote test %s: load config: %w", repoKey, err)
	}
	pol := defaultPolicy
	if row.Config != "" {
		if jerr := unmarshalPolicy(row.Config, &pol); jerr != nil {
			return TestResult{}, fmt.Errorf("remote test %s: config policy: %w", repoKey, jerr)
		}
	}

	targetURL := strings.TrimSpace(ov.URL)
	if targetURL == "" {
		targetURL = strings.TrimSpace(cfg.URL)
	}
	username, password := cfg.Username, ""
	switch {
	case ov.Password != "":
		// Draft override with a password: the body's plaintext pair (the
		// stored secret is never consulted).
		username, password = ov.Username, ov.Password
	case ov.Username != "" || ov.URL != "":
		// A partial override (an edited URL or username, no password): the
		// probe runs ANONYMOUS — the stored secret must never silently
		// ship to a different candidate host than the one it was sealed
		// for (the T-422 override discipline, verbatim).
		username, password = ov.Username, ""
	default:
		// Stored-config probe: unseal the stored credential.
		secret, uerr := unsealForProbe(cfg.Password)
		if uerr != nil {
			return TestResult{}, fmt.Errorf("%w: repository %s", ErrTestCredentialUnsealed, repoKey)
		}
		password = secret
	}

	base, err := url.Parse(targetURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return TestResult{Message: fmt.Sprintf(
			"Remote repository url %q must be an absolute http/https URL with a host", targetURL)}, nil
	}

	// The one-shot egress client: the repository's own posture (base URL,
	// credentials, token-auth spelling, private-upstream exemption, socket
	// timeout) with redirects pinned off. A one-shot client — never pooled
	// — keeps the probe's zero-footprint promise.
	client, cerr := NewClient(Options{
		RepoKey:              repoKey,
		BaseURL:              base.String(),
		Username:             username,
		Password:             password,
		TokenAuth:            pol.EnableTokenAuthentication,
		AllowPrivateUpstream: cfg.AllowPrivateUpstream,
		SocketTimeout:        time.Duration(effectiveSocketTimeoutMs(cfg, pol)) * time.Millisecond,
		MaxRedirects:         -1,
	})
	if cerr != nil {
		return TestResult{}, fmt.Errorf("remote test %s: outbound client: %w", repoKey, cerr)
	}
	defer client.CloseIdleConnections()

	res, perr := client.Fetch(ctx, Request{Path: ""})
	if perr != nil {
		return TestResult{Message: probeTransportMessage(repoKey, base, perr)}, nil
	}
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 400, res.StatusCode == http.StatusNotFound:
		return TestResult{OK: true, StatusCode: res.StatusCode, Message: fmt.Sprintf(
			"Remote repository '%s' url '%s' tested successfully", repoKey, base.String())}, nil
	default:
		return TestResult{StatusCode: res.StatusCode, Message: fmt.Sprintf(
			"Connection failed: Remote repository URL returned error %d: %s",
			res.StatusCode, probeSnippet(res.Body))}, nil
	}
}

// unsealForProbe opens one stored credential for a stored-config probe.
// Empty means anonymous; plaintext passthrough is the pre-T-66 legacy
// spelling Decrypt itself tolerates; the error is deliberately generic
// (the cipher's own wording could echo sealed material and must not
// travel).
func unsealForProbe(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if !IsEncrypted(stored) {
		return stored, nil
	}
	key, err := LoadKey()
	if err != nil {
		return "", err
	}
	if key == nil {
		return "", fmt.Errorf("credential value is encrypted but %s is not set: %w", CredentialsEnvVar, ErrNoCredentialsKey)
	}
	cipher, err := NewCipher(key)
	if err != nil {
		return "", err
	}
	secret, _, err := cipher.Decrypt(stored)
	if err != nil {
		return "", err
	}
	return secret, nil
}

// unmarshalPolicy decodes one repositories.config JSON over a seeded
// policy (the loadRepo parse, extracted for the probe's one-shot read).
func unmarshalPolicy(raw string, pol *repoPolicy) error {
	if err := json.Unmarshal([]byte(raw), pol); err != nil {
		return err
	}
	if pol.MissedRetrievalCachePeriodSecs == 0 {
		pol.MissedRetrievalCachePeriodSecs = defaultPolicy.MissedRetrievalCachePeriodSecs
	}
	if pol.SocketTimeoutSecs == 0 {
		pol.SocketTimeoutSecs = defaultPolicy.SocketTimeoutSecs
	}
	if pol.AssumedOfflinePeriodSecs == 0 {
		pol.AssumedOfflinePeriodSecs = defaultPolicy.AssumedOfflinePeriodSecs
	}
	return nil
}

// probeSnippet reads a short, flattened failure reason out of an upstream
// error body (empty when nothing readable).
func probeSnippet(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > probeSnippetBytes {
		s = s[:probeSnippetBytes]
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// probeTransportMessage maps a transport fault onto the §9.2-C-8 shape
// with the repository word: DNS failures name the host; an SSRF-guard
// refusal carries its own operator-facing message; everything else names
// the redacted URL (credentials never appear — the URL never carried them
// and the Authorization header is not part of any transport text).
func probeTransportMessage(repoKey string, u *url.URL, err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return fmt.Sprintf("Error testing remote repository '%s': unknown host '%s'", repoKey, u.Hostname())
	}
	if IsRejection(err) {
		return fmt.Sprintf("Error testing remote repository '%s': %s", repoKey, err.Error())
	}
	return fmt.Sprintf("Error testing remote repository '%s': %s %s: connection failed",
		repoKey, http.MethodGet, u.Redacted())
}
