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
// only the ones the M2 foundation can emit are declared here. T-38/T-39
// extend the table (BLOB_UNKNOWN, DIGEST_INVALID, ...).
const (
	// ErrCodeUnsupported: the operation is not supported by this registry.
	ErrCodeUnsupported = "UNSUPPORTED"
	// ErrCodeUnauthorized: authentication is required (anonymous denied).
	ErrCodeUnauthorized = "UNAUTHORIZED"
	// ErrCodeNameUnknown: the repository name is not known to the registry.
	ErrCodeNameUnknown = "NAME_UNKNOWN"
	// ErrCodeUnknown: an unknown/unexpected server-side error ([DIST-API]
	// reserves UNKNOWN for exactly this).
	ErrCodeUnknown = "UNKNOWN"
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
