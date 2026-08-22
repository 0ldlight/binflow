// Login-failure classification (T-187 / T-174 D8 + O-2): the HTTP plane
// answers every rejected login with one uniform 401 whose wording reveals
// nothing (FR-23-AC5), but the audit trail and the operator's log need to
// know WHICH arm failed and WHY. This file carries that vocabulary:
//
//   - Failure is an error type that wraps the rejection cause while carrying
//     the classification (Method + Reason). It always satisfies
//     errors.Is(err, ErrInvalidCredentials), so the HTTP plane's uniform-401
//     mapping is untouched — classification is additive diagnostics, never a
//     behavior change on the wire.
//   - The provider layer signals infrastructure problems through the
//     ErrProviderUnreachable / ErrTLSHandshake sentinels; the login paths
//     fold them into Failure values (O-2: a failed StartTLS upgrade or an
//     unreachable directory is distinguishable from a wrong password in the
//     audit log, while both stay the same 401 to the caller).
//
// Vocabulary (PRD FR-56, snake_case; extensions marked):
//
//	method:  local | oidc | ldap — the authentication arm that rejected,
//	         the same taxonomy as Principal.Source and users.provider.
//	reason:  bad_credentials     (PRD; dispatch "bad-credentials")
//	         user_not_found      (PRD)
//	         provider_error      (PRD; dispatch "provider-unreachable")
//	         tls_handshake       (T-187/O-2; dispatch "tls-handshake")
//	         user_disabled       (T-187 extension: the row exists but is off)
//	         bad_request         (T-187 extension: malformed OIDC callback)

package auth

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
)

// Reason values of a login failure (see the file comment for provenance).
const (
	ReasonBadCredentials = "bad_credentials"
	ReasonUserNotFound   = "user_not_found"
	ReasonUserDisabled   = "user_disabled"
	ReasonProviderError  = "provider_error"
	ReasonTLSHandshake   = "tls_handshake"
	ReasonBadRequest     = "bad_request"
)

// Sentinel errors the provider layer uses to signal infrastructure problems.
// They ride the error chain (never the HTTP response) so the login paths can
// classify without string matching.
var (
	// ErrProviderUnreachable marks a failure to reach the identity provider
	// (dial refused, timeout, pool closed) — the provider could not be asked.
	ErrProviderUnreachable = errors.New("auth: identity provider unreachable")

	// ErrTLSHandshake marks a TLS handshake failure against the identity
	// provider (StartTLS upgrade refused, certificate rejected) — O-2's
	// "StartTLS failed" class, distinct from both bad credentials and plain
	// unreachability.
	ErrTLSHandshake = errors.New("auth: identity provider TLS handshake failed")
)

// Failure is a classified login rejection. Method is the arm that rejected
// (local/oidc/ldap), Reason the minimal classification (see the constants).
// The wrapped cause always reaches ErrInvalidCredentials, so callers that
// only ask errors.Is keep answering the uniform 401.
type Failure struct {
	Method Provider
	Reason string
	cause  error
}

// Error renders the classification plus the cause text. The cause never
// carries a credential (the providers build messages from usernames and DNs
// only, NFR-S3).
func (f *Failure) Error() string {
	return fmt.Sprintf("auth: login via %s failed (%s): %v", f.Method, f.Reason, f.cause)
}

// Unwrap exposes the cause chain (reaching ErrInvalidCredentials).
func (f *Failure) Unwrap() error { return f.cause }

// newFailure classifies cause as a (method, reason) rejection. When the
// cause chain does not already satisfy ErrInvalidCredentials it is wrapped
// so the HTTP plane's uniform-401 mapping holds for every classification.
func newFailure(method Provider, reason string, cause error) *Failure {
	switch {
	case cause == nil:
		cause = ErrInvalidCredentials
	case !errors.Is(cause, ErrInvalidCredentials):
		cause = fmt.Errorf("%w: %w", cause, ErrInvalidCredentials)
	}
	return &Failure{Method: method, Reason: reason, cause: cause}
}

// failureOf returns err's outermost Failure classification, nil when absent.
func failureOf(err error) *Failure {
	var f *Failure
	if errors.As(err, &f) {
		return f
	}
	return nil
}

// FailureClass extracts the login-failure classification of err: which arm
// rejected (local/oidc/ldap) and the minimal reason. ok is false when err
// carries no classification — callers apply their own defaults then.
func FailureClass(err error) (method, reason string, ok bool) {
	if f := failureOf(err); f != nil {
		return string(f.Method), f.Reason, true
	}
	return "", "", false
}

// InfraFailure reports whether err classifies as a provider-side
// infrastructure failure (provider_error / tls_handshake): the O-2 signal
// the HTTP plane turns into one operator-visible WARN — the response stays
// the uniform 401 either way.
func InfraFailure(err error) bool {
	if f := failureOf(err); f != nil {
		return infraReason(f.Reason)
	}
	return false
}

// infraReason reports whether a failure reason is provider-side
// infrastructure (the O-2 classes that outrank the row-owner verdict in
// classifyLoginFailure).
func infraReason(reason string) bool {
	return reason == ReasonProviderError || reason == ReasonTLSHandshake
}

// ProviderFailureReason classifies a provider-plane transport error (OAuth
// code exchange, JWKS fetch) into a reason value: tls_handshake when the
// failure is a TLS problem, provider_error otherwise. nil classifies as a
// provider error too (the caller would not ask otherwise).
func ProviderFailureReason(err error) string {
	if err != nil && isTLSFailure(err) {
		return ReasonTLSHandshake
	}
	return ReasonProviderError
}

// isTLSFailure reports whether err's chain carries a TLS handshake failure
// (certificate verification, unknown authority, hostname mismatch, record
// header). It is best-effort on third-party wrapping: a miss degrades the
// classification to provider_error, which still separates the failure from
// bad credentials — the O-2 requirement.
func isTLSFailure(err error) bool {
	var (
		certVerify *tls.CertificateVerificationError
		unknownCA  x509.UnknownAuthorityError
		hostname   x509.HostnameError
		record     tls.RecordHeaderError
	)
	return errors.As(err, &certVerify) ||
		errors.As(err, &unknownCA) ||
		errors.As(err, &hostname) ||
		errors.As(err, &record)
}

// classifyProviderConnErr folds a connection-acquisition error (dial,
// StartTLS upgrade, pool) into the LDAP arm's failure classification.
func classifyProviderConnErr(err error) error {
	switch {
	case errors.Is(err, ErrTLSHandshake):
		return newFailure(ProviderLDAP, ReasonTLSHandshake, err)
	case errors.Is(err, ErrProviderUnreachable):
		return newFailure(ProviderLDAP, ReasonProviderError, err)
	default:
		// Unknown infrastructure failure (closed pool, ...): the directory
		// could not be asked — provider_error, never bad_credentials.
		return newFailure(ProviderLDAP, ReasonProviderError, err)
	}
}
