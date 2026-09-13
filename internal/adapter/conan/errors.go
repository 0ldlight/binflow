package conan

import (
	"encoding/json"
	"net/http"
)

// The LOCAL data plane's 404 body is the errors[] envelope (the L019 D2
// ruling): the reference answers the conan ghost family with Artifactory's
// global JSON fallback — {"errors":[{"status":404,"message":"Not Found"}]}
// — never the conan-native bare strings the as-built plane carried (L018
// wire v2-05d/v1-22b/v2-11c; npm's D4 "Not found" converged on the same
// construct, and normalize R2 covers the pretty-print indent style). The
// local writer duplicates npm/generic/docker's deliberately: adapter
// packages never import httpapi (architecture section 2 dependency
// direction). The remote/virtual faces keep their as-built rendering —
// they are the deferred differential ticket's scope, not this ruling's.

// msgNotFound is the reference's generic 404 message (L018 wire, verbatim).
const msgNotFound = "Not Found"

// conanError is the errors[] envelope body.
type conanError struct {
	Errors []conanErrorEntry `json:"errors"`
}

type conanErrorEntry struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// writeEnvelope emits the errors[] envelope. Encoding failures are
// swallowed: by then the connection is gone and headers cannot be
// rewritten (the npm writer's contract).
func writeEnvelope(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(conanError{Errors: []conanErrorEntry{{Status: status, Message: message}}})
}
