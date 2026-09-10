package docker

import (
	"encoding/json"
	"net/http"

	"github.com/lzwzzy/binflow/internal/repo"
)

// HeaderAPIVersion is the registry advertisement header every /v2 response
// carries ([DIST-API] SHOULD; Artifactory enforces it on every endpoint —
// docker-registry.md section 0, high confidence).
const HeaderAPIVersion = "Docker-Distribution-Api-Version"

// APIVersionValue is the header's constant value.
const APIVersionValue = "registry/2.0"

// Error codes of the registry error schema ([DIST-API] section Errors);
// only the ones the M2 surface can emit are declared here. T-39/T-40 extend
// the table (MANIFEST_*, PAGINATION_*).
const (
	// ErrCodeUnsupported: the operation is not supported by this registry.
	ErrCodeUnsupported = "UNSUPPORTED"
	// ErrCodeUnauthorized: authentication is required (anonymous denied).
	ErrCodeUnauthorized = "UNAUTHORIZED"
	// ErrCodeDenied: the authenticated client does not have the required
	// authorization ([DIST-API] section Errors: "The access controller
	// denied access for the operation on a resource").
	ErrCodeDenied = "DENIED"
	// ErrCodeNameUnknown: the repository name is not known to the registry.
	ErrCodeNameUnknown = "NAME_UNKNOWN"
	// ErrCodeUnknown: an unknown/unexpected server-side error ([DIST-API]
	// reserves UNKNOWN for exactly this).
	ErrCodeUnknown = "UNKNOWN"
	// ErrCodeBlobUnknown: the blob addressed by the digest is not known to
	// the registry (DE-06's 404 body).
	ErrCodeBlobUnknown = "BLOB_UNKNOWN"
	// ErrCodeBlobUploadInvalid: the blob upload encountered an error (the
	// interrupted-session refusal of a poisoned finalize).
	ErrCodeBlobUploadInvalid = "BLOB_UPLOAD_INVALID"
	// ErrCodeBlobUploadUnknown: the upload session addressed by the UUID is
	// not known (unknown or pre-restart sessions).
	ErrCodeBlobUploadUnknown = "BLOB_UPLOAD_UNKNOWN"
	// ErrCodeDigestInvalid: the digest parameter is malformed or does not
	// match the content (D12/DE-05; the official code — Artifactory's
	// BLOB_UPLOAD_INVALID wording for mismatches is NOT adopted).
	ErrCodeDigestInvalid = "DIGEST_INVALID"
	// ErrCodeUnavailable: the registry is temporarily unavailable (storage
	// engine shutting down).
	ErrCodeUnavailable = "UNAVAILABLE"
	// ErrCodeManifestUnknown: the manifest addressed by the tag/digest is
	// not known to the registry (DE-09's 404 body; also the Accept
	// negotiation miss — the requested representation does not exist).
	ErrCodeManifestUnknown = "MANIFEST_UNKNOWN"
	// ErrCodeManifestInvalid: the manifest body/descriptors failed
	// structural parsing (schema1 refusal included, PRD section 6.5).
	ErrCodeManifestInvalid = "MANIFEST_INVALID"
	// ErrCodeManifestBlobUnknown: a manifest references a blob the
	// repository does not serve (the official code — Artifactory folds this
	// into MANIFEST_INVALID's wording, docker-registry.md section 10
	// recommendation 1; D13b).
	ErrCodeManifestBlobUnknown = "MANIFEST_BLOB_UNKNOWN"
	// ErrCodePaginationNumberInvalid: the pagination parameters are invalid
	// (T-40: n=0 or non-numeric — the official code distribution itself
	// registers; Artifactory has none, docker-registry.md section 6).
	ErrCodePaginationNumberInvalid = "PAGINATION_NUMBER_INVALID"
	// ErrCodeNameInvalid: the repository name is invalid (a tags/list name
	// the service layer refuses; the official code NAME_INVALID).
	ErrCodeNameInvalid = "NAME_INVALID"
)

// specError is one entry of the registry error body.
type specError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  any    `json:"detail"`
}

// specErrorBody is the wire shape {"errors":[{code,message,detail}]} that
// every non-2xx on the /v2 plane answers with (DE-17/NFR-S10: the docker
// domain never renders the /binflow errors[] envelope).
type specErrorBody struct {
	Errors []specError `json:"errors"`
}

// statusFormError is one entry of Artifactory's GENERIC error model — the
// {"status":<n>,"message":"..."} entries (no code, no detail) its
// non-registry faults render (L000-B evidence E1-5/E5-1: the token
// endpoint's refused credential and the remote plane's upload refusals
// both speak it, pretty-printed, while the manifest/blob 404s keep the
// spec {code,detail} form above — the two forms coexist on one repository).
type statusFormError struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// statusFormBody is the generic model's envelope (an errors array whose
// entries carry status, not code — the docker CLI renders their message
// under the "unknown:" prefix, E1-5's client-side fingerprint).
type statusFormBody struct {
	Errors []statusFormError `json:"errors"`
}

// writeStatusFormError renders the Artifactory generic error model
// (pretty-printed, the evidence bodies' shape) with the mandatory
// api-version header every /v2 response carries (DE-17).
func writeStatusFormError(w http.ResponseWriter, status int, message string) {
	hdr := w.Header()
	hdr.Set("Content-Type", "application/json")
	hdr.Set(HeaderAPIVersion, APIVersionValue)
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(statusFormBody{Errors: []statusFormError{{Status: status, Message: message}}})
}

// writeSpecError renders the registry error envelope with the mandatory
// api-version header. detail may be nil (serialized as null).
func writeSpecError(w http.ResponseWriter, status int, code, message string, detail any) {
	hdr := w.Header()
	hdr.Set("Content-Type", "application/json")
	hdr.Set(HeaderAPIVersion, APIVersionValue)
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	_ = enc.Encode(specErrorBody{Errors: []specError{{
		Code: code, Message: message, Detail: detail,
	}}})
}

// writeVerbatimStatusError renders a *repo.StatusError on the /v2 plane with
// the error's OWN status code and message (T-111 — the docker arm of the
// verbatim seam the generic adapter opened in T-66 and maven/npm/pypi joined
// in T-82): a repository-class verdict that already knows its exact client
// rendering — the quota 413 and pattern 409 of T-95's governance gates, the
// virtual/remote write 405s — reaches the wire verbatim instead of
// collapsing into the 500 UNKNOWN the default error arms used to answer
// with. The envelope's code field is re-derived from the status
// (specCodeOfVerbatim below); any header the StatusError itself carries
// (the 405 Allow) rides along.
func writeVerbatimStatusError(w http.ResponseWriter, se *repo.StatusError) {
	for k, vv := range se.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	writeSpecError(w, se.Code, specCodeOfVerbatim(se.Code), se.Message, nil)
}

// specCodeOfVerbatim maps a verbatim StatusError's HTTP status onto the
// registry envelope's code field. Decision (T-111, documented for review):
//
//   - the governance refusals — quota 413 (ErrQuotaExceeded), pattern 409
//     (ErrPatternRejected) — carry DENIED, the official [DIST-API] code
//     whose wording ("the access controller denied access for the operation
//     on a resource") is the closest official fit for a policy refusal of
//     an operation on a resource. TOOMANYREQUESTS was considered and
//     rejected: the spec binds it to 429 rate limiting, so pairing it with
//     413 is a status/code mismatch. A custom code (QUOTA_EXCEEDED et al.)
//     would add non-standard surface no client consumes — the reverse spec
//     already shows registries diverging freely here (docker-registry.md
//     section 1 row 3: Artifactory's SIZE_INVALID / NO_TAGS_FOUND) — so the
//     official code wins on adapter-internal consistency: DENIED is this
//     adapter's refusal code everywhere else.
//   - the other statuses keep their canonical envelope codes where one
//     exists (405 UNSUPPORTED, 401 UNAUTHORIZED, 403 DENIED); anything
//     unmapped stays UNKNOWN. The 401 arm is dead-path defense held by a
//     service-layer constraint, not a type: every repo.StatusError
//     constructor speaks a governance/plane verdict (409/413/405/...) —
//     auth refusals are plain wrapped sentinels, so the challenge arms
//     keep the docker token flow. Should a StatusError ever carry 401,
//     this arm's verbatim render would lack the WWW-Authenticate
//     challenge and the arm must route through the challenge instead
//     (review non-blocking 1).
//
// The verbatim contract itself (status + message) is untouched by this
// mapping: a 413 renders as 413 with the service layer's exact quota
// wording whichever code the envelope carries.
func specCodeOfVerbatim(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return ErrCodeUnauthorized
	case http.StatusForbidden, http.StatusConflict, http.StatusRequestEntityTooLarge:
		return ErrCodeDenied
	case http.StatusMethodNotAllowed:
		return ErrCodeUnsupported
	default:
		return ErrCodeUnknown
	}
}

// writeAPIVersion sets the advertisement header on a response the handler
// is about to render itself (success and no-content cases; error paths go
// through writeSpecError).
func writeAPIVersion(w http.ResponseWriter) {
	w.Header().Set(HeaderAPIVersion, APIVersionValue)
}

// writeAPIVersionHdr is the Header-map form for streaming paths that
// already hold hdr.
func writeAPIVersionHdr(hdr http.Header) {
	hdr.Set(HeaderAPIVersion, APIVersionValue)
}
