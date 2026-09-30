package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// The unified non-2xx body (architecture section 7.3, calibrated by
// rest-api.md section 0, high confidence; wire layout fixed by the T-615
// live leg set against Artifactory 7.161.26):
//
//	{
//	  "errors" : [ {
//	    "status" : 404,
//	    "message" : "..."
//	  } ]
//	}
//
// The layout is the reference's Jackson pretty printer, byte-exact: a
// space before every colon, the single entry's braces hugging the array
// brackets, and no trailing newline (raw capture ar1-m-jar-get.body, 151
// bytes, /tmp/t615/raw). The array form applies to every endpoint
// including content paths and the /api/v1 plane (it supersedes the
// original single-object draft, T-22 write-back R6). Probes (/healthz,
// /readyz) are exempt only in that they have no failure state short of
// transport errors.

const (
	ctJSONPlain   = "application/json"
	ctJSONCharset = "application/json;charset=ISO-8859-1"
)

// errorEntry is the envelope's single entry shape, shared with the one
// face that embeds errors[] inside a wider body (the release-bundle
// store's double-key "reason"+errors 400, release_bundle_store.go).
type errorEntry struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// writeError emits the errors[] envelope with the given status. The wire
// face follows the reference's PLANE-level matrix (T-615, 32 legs on
// 7.161.26): the repo-path content plane and every 401 arm — across all
// planes — answer with the charset parameter; the protocol/management
// API planes (/api/**) keep the bare media type. The registry spec plane
// (/v2) renders its own bodies and never passes here.
//
// Rendering failures are ignored: by the time an error body fails to
// encode the connection is gone anyway, and headers must not be rewritten
// after the status line. HTML escaping is OFF (L009-3): the reference's
// Jackson serializer emits < > & raw, and Go's default < escaping was
// visibly rewriting the Illegal-name character list on the wire.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", errorFaceContentType(w, status))
	w.WriteHeader(status)
	_, _ = w.Write([]byte(renderErrorEnvelope(status, message)))
}

// errorFaceContentType picks the error face's media type per the plane
// matrix: a content-plane writer or a 401 anywhere takes the reference's
// charset spelling (no space, d-ping-anon byte form); every other plane
// stays bare.
func errorFaceContentType(w http.ResponseWriter, status int) string {
	if status == http.StatusUnauthorized || isContentPlane(w) {
		return ctJSONCharset
	}
	return ctJSONPlain
}

// renderErrorEnvelope lays the single-entry errors[] body out exactly as
// the reference's Jackson pretty printer does (T-615 verbatim legs):
// `{\n  "errors" : [ {\n    "status" : N,\n    "message" : "..."\n  } ]\n}`
// with no trailing newline.
func renderErrorEnvelope(status int, message string) string {
	var b strings.Builder
	b.WriteString("{\n  \"errors\" : [ {\n    \"status\" : ")
	b.WriteString(strconv.Itoa(status))
	b.WriteString(",\n    \"message\" : ")
	b.WriteString(jsonStringRaw(message))
	b.WriteString("\n  } ]\n}")
	return b.String()
}

// jsonStringRaw quotes s per JSON with HTML escaping off (L009-3): the
// reference emits < > & raw.
func jsonStringRaw(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		// Unreachable for a string payload; the fallback keeps the
		// writer contract even on an impossible encoder error.
		return `""`
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// contentPlaneMark tags the response writer of the repo-path content
// plane (applied once at the dispatchContent funnel). Pure pass-through:
// nothing observes it except the envelope seam's face decision, so the
// adapters and handlers downstream keep rendering exactly as before.
type contentPlaneMark struct{ http.ResponseWriter }

// Flush forwards streaming — the embed hides the wrapped writer's own
// Flush, and downloads must not be buffered by the mark.
func (c contentPlaneMark) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap keeps http.NewResponseController working through the mark.
func (c contentPlaneMark) Unwrap() http.ResponseWriter { return c.ResponseWriter }

// markContentPlane is the single point that decides "this response rides
// the repo-path content plane" for the envelope's charset face rule.
func markContentPlane(w http.ResponseWriter) http.ResponseWriter {
	return contentPlaneMark{ResponseWriter: w}
}

func isContentPlane(w http.ResponseWriter) bool {
	_, ok := w.(contentPlaneMark)
	return ok
}

// notImplemented renders the E-26 "unimplemented endpoint" 404: the message
// must carry a "not implemented" wording (PRD section 5.1: "message 含
// not implemented in BinFlow 类字样") so a client can tell "wrong product"
// apart from "wrong milestone".
func notImplemented(w http.ResponseWriter, what string) {
	writeError(w, http.StatusNotFound, what+" is not implemented in BinFlow")
}

// notFoundPrefixHint renders the E-26 root-path 404 with the /binflow
// prefix hint (PRD C24: legacy root requests receive a /binflow prefix hint).
func notFoundPrefixHint(w http.ResponseWriter, path string) {
	writeError(w, http.StatusNotFound,
		"no root mirror: BinFlow serves every endpoint under the /binflow prefix (requested path "+path+"); the legacy repository context prefix is not emulated")
}
