// Package errface renders the unified non-2xx error face the httpapi
// envelope seam and the protocol adapters share: the single-entry errors[]
// body in the reference's Jackson pretty layout (wire layout fixed by the
// T-615 live leg set against Artifactory 7.161.26) plus the plane matrix
// that picks the media type spelling.
//
// It is a LEAF package (standard library only — the internal/redact
// precedent): httpapi delegates here instead of owning the bytes, adapters
// import it directly instead of keeping per-package copies, and no domain
// dependency exists, so the import graph stays cycle-free by construction.
package errface

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// Media type spellings of the error face, per the reference's PLANE-level
// matrix (T-615, 32 legs on 7.161.26): the repo-path content plane and
// every 401 arm answer the charset parameter (no space, the d-ping-anon
// byte form); the protocol/management API planes (/api/**) keep the bare
// media type.
const (
	Plain   = "application/json"
	Charset = "application/json;charset=ISO-8859-1"
)

// ContentType picks the error face's media type per the plane matrix: a
// content-plane response or a 401 anywhere takes the charset spelling;
// every other plane stays bare.
func ContentType(contentPlane bool, status int) string {
	if status == http.StatusUnauthorized || contentPlane {
		return Charset
	}
	return Plain
}

// Render lays out the single-entry errors[] body exactly as the reference's
// Jackson pretty printer does (T-615 verbatim legs, e.g. the 132-byte
// ar1-h-tgz-get capture):
//
//	{\n  "errors" : [ {\n    "status" : N,\n    "message" : "..."\n  } ]\n}
//
// with no trailing newline. HTML escaping is OFF (L009-3): the reference's
// Jackson serializer emits < > & raw, and Go's default < escaping was
// visibly rewriting the Illegal-name character list on the wire.
func Render(status int, message string) string {
	var b strings.Builder
	b.WriteString("{\n  \"errors\" : [ {\n    \"status\" : ")
	b.WriteString(strconv.Itoa(status))
	b.WriteString(",\n    \"message\" : ")
	b.WriteString(jsonStringRaw(message))
	b.WriteString("\n  } ]\n}")
	return b.String()
}

// Write emits the envelope with the plane-appropriate media type. Rendering
// failures are ignored: by the time an error body fails to encode the
// connection is gone anyway, and headers must not be rewritten after the
// status line.
func Write(w http.ResponseWriter, status int, message string, contentPlane bool) {
	w.Header().Set("Content-Type", ContentType(contentPlane, status))
	w.WriteHeader(status)
	_, _ = w.Write([]byte(Render(status, message)))
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
