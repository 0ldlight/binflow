package docker

import (
	"encoding/json"
	"net/http"
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
