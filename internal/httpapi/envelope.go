package httpapi

import (
	"encoding/json"
	"net/http"
)

// errorEnvelope is the unified non-2xx body (architecture section 7.3,
// calibrated by rest-api.md section 0, high confidence):
//
//	{"errors": [ { "status": 404, "message": "..." } ]}
//
// The array form applies to every endpoint including content paths and the
// /api/v1 plane (it supersedes the original single-object draft, T-22
// write-back R6). Probes (/healthz, /readyz) are exempt only in that they
// have no failure state short of transport errors.
type errorEnvelope struct {
	Errors []errorEntry `json:"errors"`
}

type errorEntry struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// writeError emits the errors[] envelope with the given status. Rendering
// failures are ignored: by the time an error body fails to encode the
// connection is gone anyway, and headers must not be rewritten after the
// status line. HTML escaping is OFF (L009-3): the reference's Jackson
// serializer emits < > & raw, and Go's default < escaping was visibly
// rewriting the Illegal-name character list on the wire.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(errorEnvelope{Errors: []errorEntry{{Status: status, Message: message}}})
}

// notImplemented renders the E-26 "unimplemented endpoint" 404: the message
// must carry a "not implemented" wording (PRD section 5.1: "message 含
// not implemented in BinFlow 类字样") so a client can tell "wrong product"
// apart from "wrong milestone".
func notImplemented(w http.ResponseWriter, what string) {
	writeError(w, http.StatusNotFound, what+" is not implemented in BinFlow")
}

// notFoundPrefixHint renders the E-26 root-path 404 with the /binflow
// prefix hint (PRD C24: "/artifactory/** 的 message 提示 /binflow 前缀").
func notFoundPrefixHint(w http.ResponseWriter, path string) {
	writeError(w, http.StatusNotFound,
		"no root mirror: BinFlow serves every endpoint under the /binflow prefix (requested path "+path+"); the Artifactory /artifactory prefix is not emulated")
}
