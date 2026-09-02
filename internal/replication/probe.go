package replication

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/remote"
)

// The Test-connection probe (M15 T-422, FR-138.2; replication.md §9.2-C/
// §9.3 — the testlocalreplication counterpart, BinFlow's own /api/v1 face
// per §9.3's ruling: the official REST reference carries no test endpoint).
//
// Contract (§9.3's table, semantics immutable):
//
//   - ZERO side effects: one read-only request, no state written anywhere
//     (the probe cannot create, land or enqueue anything), the credentials
//     never reach any log (NFR-S75), and the global block state is NOT
//     consulted (§9.2-C-10: blocking stops execution, never testing).
//   - Pass criteria: the target answers 200 or 302 (§9.2-C-5). The probe
//     address is the target instance's storage-info mount of the target
//     repository — {TargetURL}/binflow/api/storage/{TargetRepo} — the
//     BinFlow mapping of Artifactory's "HEAD the replication target URL"
//     (an Artifactory config URL already carries the repository path;
//     BinFlow splits base/repo, and the storage mount is the one address a
//     BinFlow target answers 200 on for an existing repository with valid
//     credentials — the bare base answers E-26① 404 by design). One GET,
//     redirects not followed, the response body drained bounded and kept
//     only as a short failure reason.
//   - Failure families (§9.2-C): a target answering anything else carries
//     "Connection failed: Target replication URL returned error <status>:
//     <reason>" (401/403 = the credentials were refused); a transport/DNS
//     fault carries the §9.2-C-8 shape; a -cache target is refused
//     verbatim (§9.2-C-4). The REST face renders every OK=false verdict as
//     the SAME result body at HTTP 400 — the inline reason IS the payload
//     (the auth.config.test posture, the word's namesake face).
//
// Error discipline: an error return is reserved for FACE faults the caller
// maps onto 5xx (a stored credential that cannot be unsealed — never
// leaking the sealed text). Every probe verdict, pass or fail, is a
// TestResult with a nil error.

// TargetOverride carries the draft-test arms (§9.3's optional body): every
// non-empty field replaces the stored config's value for THIS probe only —
// nothing is written. TargetPassword is plaintext from the request body,
// never stored and never logged.
type TargetOverride struct {
	TargetURL      string
	TargetRepo     string
	TargetUsername string
	TargetPassword string
}

// TestResult is the probe verdict the REST face serializes (§9.3: ok +
// status_code + message; failures ride the same body at HTTP 400).
type TestResult struct {
	OK         bool
	StatusCode int
	Message    string
}

// ErrTestCredentialUnsealed marks a stored password the cipher cannot open:
// the honest 500 family (§9.3 — never leak the sealed text, never pretend
// the target was contacted).
var ErrTestCredentialUnsealed = errors.New("replication: target credential could not be unsealed for the test probe")

// TestTarget probes one config's target (plus optional overrides) with the
// engine's own guarded client, socket timeout and SSRF posture — the same
// wire path a push would take, minus the write. cfg carries the sealed
// credential for a stored-config probe; a draft probe passes a synthesized
// candidate plus the body's plaintext override and the stored secret is
// never consulted.
func (e *Engine) TestTarget(ctx context.Context, cfg *ReplicationConfig, ov TargetOverride) (TestResult, error) {
	if cfg == nil {
		return TestResult{Message: "No replication configuration given to test."}, nil
	}
	targetURL := strings.TrimSpace(ov.TargetURL)
	if targetURL == "" {
		targetURL = strings.TrimSpace(cfg.TargetURL)
	}
	targetRepo := strings.TrimSpace(ov.TargetRepo)
	if targetRepo == "" {
		targetRepo = strings.TrimSpace(cfg.TargetRepo)
	}
	username, password := cfg.TargetUsername, ""
	overridePresent := ov.TargetURL != "" || ov.TargetRepo != "" ||
		ov.TargetUsername != "" || ov.TargetPassword != ""
	switch {
	case !overridePresent:
		// Stored-config probe: unseal the stored credential.
		secret, err := e.resolveTestPassword(cfg)
		if err != nil {
			return TestResult{}, err
		}
		password = secret
	case ov.TargetPassword != "":
		// Draft override with a password: the body's plaintext pair (the
		// stored secret is never consulted).
		username, password = ov.TargetUsername, ov.TargetPassword
	default:
		// A partial override (an edited URL or username, no password): the
		// probe runs ANONYMOUS — the stored secret must never silently ship
		// to a different candidate host than the one it was sealed for.
		username = ov.TargetUsername
	}

	// §9.2-C-4 verbatim: a remote cache repository is not a legal target.
	if strings.HasSuffix(targetRepo, "-cache") {
		return TestResult{Message: "Replication to remote cache repositories is not allowed."}, nil
	}

	base, err := url.Parse(targetURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return TestResult{Message: fmt.Sprintf(
			"target url %q must be an absolute http/https URL with a host", targetURL)}, nil
	}
	if targetRepo == "" {
		return TestResult{Message: "target repository key is empty"}, nil
	}
	segments := strings.Split(targetRepo, "/")
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return TestResult{Message: fmt.Sprintf(
				"target repository %q: illegal path segment", targetRepo)}, nil
		}
	}
	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + "/binflow/api/storage/" + strings.Join(segments, "/")
	u.RawPath = "" // re-derived from Path by String()
	u.RawQuery = ""
	u.Fragment = ""

	resp, perr := e.probeDo(ctx, &u, username, password)
	if perr != nil {
		return TestResult{Message: probeTransportMessage(&u, perr)}, nil
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusFound, http.StatusMovedPermanently:
		_ = resp.Body.Close()
		// §9.1-C push face, anchored wording (url = the probed address).
		return TestResult{OK: true, StatusCode: resp.StatusCode,
			Message: fmt.Sprintf("Push replication target url '%s' tested successfully", u.String())}, nil
	default:
		return TestResult{StatusCode: resp.StatusCode, Message: fmt.Sprintf(
			"Connection failed: Target replication URL returned error %d: %s",
			resp.StatusCode, strings.TrimSpace(readSnippet(resp)))}, nil
	}
}

// resolveTestPassword unseals the stored target credential for the probe.
// The error is deliberately generic: the cipher's own wording could echo
// sealed material and must not travel.
func (e *Engine) resolveTestPassword(cfg *ReplicationConfig) (string, error) {
	if cfg.TargetPasswordEnc == "" {
		return "", nil
	}
	secret, _, err := e.cfg.cipher.Decrypt(cfg.TargetPasswordEnc)
	if err != nil {
		return "", fmt.Errorf("%w: config %s", ErrTestCredentialUnsealed, cfg.Name)
	}
	return secret, nil
}

// probeDo issues the one guarded probe request. Rejections from the SSRF
// chain surface as probe refusals (the target address is operator input on
// the draft face); every other fault returns for transport classification.
func (e *Engine) probeDo(ctx context.Context, u *url.URL, username, password string) (*http.Response, error) {
	if err := e.guard.CheckURL(ctx, u.String()); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", pushUserAgent)
	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}
	return e.client.Do(req)
}

// probeTransportMessage maps a transport fault onto §9.2-C-8's shape: DNS
// failures name the host; an SSRF-guard refusal carries its own message
// (already operator-facing); everything else names the redacted URL (the
// credentials never appear — the URL never carried them, and the request's
// Authorization header is not part of any transport error text).
func probeTransportMessage(u *url.URL, err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return fmt.Sprintf("Error testing push replication config: unknown host '%s'", u.Hostname())
	}
	if remote.IsRejection(err) {
		return fmt.Sprintf("Error testing push replication config: %s", err.Error())
	}
	return fmt.Sprintf("Error testing push replication config: %s %s: connection failed",
		http.MethodGet, u.Redacted())
}
