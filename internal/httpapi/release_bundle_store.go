// L026-6 (D08-R03): PUT /api/release/store — the Distribution push's
// receiving endpoint, wire-frozen by release-bundle.md §10.4 (p44-p49,
// p11, p48b). The arm chain is ORDERED and first-failure-short-circuiting:
// admin door → projectKey → body parse → the null-JWS 500 leak → the
// INVALID_RB_REPO double-key envelope → the default system-repo
// provisioning side effect → the bare JWS-parse 500 → the signature
// validation 400. The 202/200/409 tri-state behind it (§3.1) needs a valid
// signature chain BinFlow does not carry — unreachable, registered UNKNOWN
// in the ticket report (the probe instance could not reach it either,
// §8 #5).

package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lzwzzy/binflow/internal/bundle"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// The store face's frozen copy (release-bundle.md §10.4 — do not reword).
const (
	msgInvalidRBRepoReason = "INVALID_RB_REPO"
	msgInvalidRBRepo       = "Invalid release bundle repository"
	// msgRBProjectNotFound is the Access-collaborator passthrough (p49):
	// BinFlow has no projects domain, so EVERY projectKey names a missing
	// project — the copy embeds the caller's key in backticks verbatim.
	msgRBProjectNotFound = "HTTP response status 404:Failed to execute add project resource with error Could not find project `%s`"
	// msgRBNullJWS is the reference's null-annotation leak (p45): a body
	// without signedJwsBundle 500s, it does not 400.
	msgRBNullJWS = "jwsString is marked non-null but is null"
)

// invalidRBRepoBody is the double-key envelope (p44) — "reason" at the top
// level beside the standard errors array, the only such shape in the
// family.
type invalidRBRepoBody struct {
	Reason string       `json:"reason"`
	Errors []errorEntry `json:"errors"`
}

// handleBundleStore serves PUT /api/release/store (§10.4's arm chain).
func (s *Server) handleBundleStore(w http.ResponseWriter, r *http.Request) {
	if !bundleAdminGate(w, r) {
		return // p52: the bare "Forbidden" envelope
	}
	if !s.requireBundleAddon(w, r) {
		return
	}
	if pk := strings.TrimSpace(r.URL.Query().Get("projectKey")); pk != "" {
		writeError(w, http.StatusNotFound, fmt.Sprintf(msgRBProjectNotFound, pk)) // p49
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bundleMaxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "release bundle body could not be read: "+err.Error())
		return
	}
	var wire struct {
		SignedJwsBundle *string `json:"signedJwsBundle"`
		StoringRepo     string  `json:"storingRepo"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		if msg, ok := jacksonUnrecognizedToken(raw); ok {
			writeError(w, http.StatusBadRequest, msg) // p46
			return
		}
		writeError(w, http.StatusBadRequest, "release bundle body is not valid JSON: "+err.Error())
		return
	}
	if wire.SignedJwsBundle == nil {
		writeError(w, http.StatusInternalServerError, msgRBNullJWS) // p45
		return
	}
	if wire.StoringRepo != "" {
		// p44: an explicit storingRepo must already BE a release-bundle
		// repository. A lookup miss answers the same envelope (an unknown
		// key is not a release-bundle repository — the sub-arm is unprobed,
		// family-consistent).
		row, gerr := s.deps.Repos.Get(r.Context(), wire.StoringRepo)
		if gerr != nil || row == nil || row.PackageType != bundle.PackageTypeReleaseBundles {
			// The double-key envelope: "reason" at the top level beside
			// the errors array — the only such shape in the family, on the
			// platform's shared pretty renderer.
			writeJSONBody(w, http.StatusBadRequest, invalidRBRepoBody{
				Reason: msgInvalidRBRepoReason,
				Errors: []errorEntry{{Status: http.StatusBadRequest, Message: msgInvalidRBRepo}},
			})
			return
		}
	} else if s.bundles != nil {
		// p48: the default project's storing repository resolves to
		// release-bundles AND the provisioning side effect fires BEFORE the
		// JWS parse arm (the probe's 500 answer coexisted with the created
		// repository).
		if err := s.bundles.EnsureSystemRepo(r.Context()); err != nil {
			s.log.ErrorContext(r.Context(), "httpapi: release-bundles system repo provisioning failed", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "release bundle operation failed")
			return
		}
	}
	if msg := jwsFormError(strings.TrimSpace(*wire.SignedJwsBundle)); msg != "" {
		// p48: the bare copy — NOT the transaction family's prefixed form
		// (same cause, different port, the §10.4 asymmetry).
		writeError(w, http.StatusInternalServerError, msg)
		return
	}
	// Signature validation: BinFlow holds no Distribution signing keys, so
	// every well-formed JWS honestly fails here. The 202/200/409 tri-state
	// (§3.1, {"bundle_path": "<storingRepo>/<name>/<version>"}) is
	// unreachable — UNKNOWN, not guessed.
	writeError(w, http.StatusBadRequest, msgRBSignatureInvalid)
}

// handleBundleStoreOptions serves OPTIONS /api/release/store (p11): the
// CORS preflight — 200 text/plain "OPTIONS, PUT" plus the Allow header,
// unauthenticated by preflight semantics.
func (s *Server) handleBundleStoreOptions(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Allow", "OPTIONS,PUT")
	writePlainText(w, http.StatusOK, "OPTIONS, PUT")
}

// bundleRepoIsHidden reports whether a repository row is the release-bundle
// system population the repositories LIST faces never show (p48b: the
// auto-created release-bundles repository answers its single GET but stays
// out of GET /api/repositories).
func bundleRepoIsHidden(row *metadata.Repo) bool {
	return row.PackageType == bundle.PackageTypeReleaseBundles
}
