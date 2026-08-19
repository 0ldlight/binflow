package npm

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The npm domain's failure body is the shared E-01 envelope
// {"errors":[{"status":N,"message":"..."}]} (spec section 0, PRD Q7 ruling;
// npm clients do not parse the structure, but BinFlow's planes stay uniform).
// The duplicate of httpapi's unexported writer is deliberate: adapter packages
// never import httpapi (architecture section 2 dependency direction), and the
// generic/docker adapters carry the same local writer.

// npmError is the E-01 envelope body.
type npmError struct {
	Errors []npmErrorEntry `json:"errors"`
}

type npmErrorEntry struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// writeError emits the errors[] envelope. Encoding failures are swallowed:
// by then the connection is gone and headers cannot be rewritten.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(npmError{Errors: []npmErrorEntry{{Status: status, Message: message}}})
}

// writeNotImplemented renders the E-26 wording 404 for npm addresses BinFlow
// deliberately does not serve (NE-08: search, audits, attestations...; M58
// probes assert the status only, the wording follows E-26).
func writeNotImplemented(w http.ResponseWriter, what string) {
	writeError(w, http.StatusNotFound, "npm endpoint "+what+" is not implemented in BinFlow")
}

// Spec-pinned npm messages (maven-npm-pypi.md section 2.1/2.3, high
// confidence; PRD v1.1 Q7 403 ruling on the duplicate publish). Wording is
// load-bearing — npm surfaces the body verbatim in CLI errors.
const (
	msgCannotDeploy = "Cannot deploy to '%s'"
	msgCannotModify = "Cannot modify pre-existing version '%s', aborting upload for: '%s'"
	msgMissingVers  = "Missing versions in npm package '%s'"
	msgMissingAtt   = "Missing attachments with tarball data in npm package '%s'"
	msgIntegrity    = "Conflict between integrity from metadata and tarball"
	msgSha1Conflict = "Conflict between sha1 from metadata and tarball"
	msgInvalidVer   = "Invalid Version: '%s'"
	msgTagNotFound  = "npm package not found with name:%s, and tag:%s"
	msgPackNotFound = "Package '%s' not found"
)

// writeServiceError maps repo.Service sentinels onto the npm plane. The
// checksum-mismatch arm is the publish chain's step 9 (storage's
// client-checksums 409 surfaces as npm's 400, spec section 2.3 step 9).
func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrChecksumMismatch):
		writeError(w, http.StatusBadRequest, msgSha1Conflict)
	case errors.Is(err, repo.ErrNodeNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, repo.ErrRepoNotFound):
		writeError(w, http.StatusNotFound, "repository not found")
	case errors.Is(err, repo.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
		writeError(w, http.StatusUnauthorized, "authentication required")
	case errors.Is(err, repo.ErrForbidden):
		writeError(w, http.StatusForbidden, "permission denied")
	case errors.Is(err, repo.ErrIsFolder), errors.Is(err, repo.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// repoRowError maps the N4 guard's ClassReader failure: only a missing row is
// a 404; a broken store is an honest 500.
func repoRowError(w http.ResponseWriter, repoKey string, err error) {
	if errors.Is(err, metadata.ErrRepoNotFound) || errors.Is(err, repo.ErrRepoNotFound) {
		writeError(w, http.StatusNotFound,
			"Failed to find the repository '"+repoKey+"' specified in the request.")
		return
	}
	writeError(w, http.StatusInternalServerError, "repository lookup failed")
}
