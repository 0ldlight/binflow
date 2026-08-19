package pypi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// Upload form limits. Value fields are capped so a hostile form cannot
// buffer unbounded memory; the cap sits far above every real metadata field
// (twine's description field carries the whole README, which PyPI itself
// allows up to a size no legitimate package approaches here).
const (
	maxFieldValueBytes = 1 << 20 // 1 MiB per non-file field
	maxFormFields      = 128
)

// handleUpload implements the warehouse upload API (PE-02): one
// multipart/form-data POST whose :action must be file_upload, whose
// content part is the distribution file, and whose remaining fields are
// metadata. The response is uniformly 200 (warehouse semantics; twine
// accepts any 2xx).
//
// Byte flow: the content part streams straight into a storage session
// (never buffered in memory); the node lands only after every validation,
// via repo.Service.PutLandedBlob — the T-64 use case built for uploads
// whose storage path is known only after the body has been consumed
// (multipart field order is client-chosen; twine happens to send metadata
// first, but order is not contractual).
func (h *Handler) handleUpload(w http.ResponseWriter, r *http.Request, repoKey string) {
	ctx := r.Context()
	p := adapter.PrincipalFrom(ctx)
	if p == nil {
		// The route gate already challenges anonymous writes; this is the
		// bare-mount defense (NFR-S17).
		w.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
		writeError(w, http.StatusUnauthorized, "Authentication is required to deploy artifacts.")
		return
	}

	// Class gate before any byte is read: remote repositories are read-only
	// proxies (FR-20 RE-04: PUT/POST -> 405 + Allow: GET) and virtual
	// repositories route writes only through an explicitly configured
	// defaultDeploymentRepo (FR-21/C5 wording; the routing engine itself is
	// T-71's surface — the PyPI face refuses early and honestly until
	// then).
	if status, msg, allow := h.rejectNonLocalUpload(ctx, repoKey); status != 0 {
		if allow != "" {
			w.Header().Set("Allow", allow)
		}
		writeError(w, status, msg)
		return
	}

	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		writeError(w, http.StatusBadRequest,
			"the upload endpoint expects a multipart/form-data body with a boundary")
		return
	}

	fields := make(map[string]string, 16)
	var (
		contentFilename string
		contentMime     string
		sess            storage.Session
		sessionStarted  bool
		committed       bool
	)
	defer func() {
		if sessionStarted && !committed {
			_ = sess.Abort(ctx)
		}
	}()

	reader := multipart.NewReader(r.Body, params["boundary"])
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "malformed multipart body: "+err.Error())
			return
		}
		field := part.FormName()
		filename := part.FileName()
		if filename == "" {
			// Value field: bounded read, last occurrence wins.
			if len(fields) >= maxFormFields {
				_ = part.Close()
				writeError(w, http.StatusBadRequest,
					fmt.Sprintf("the upload form exceeds %d fields", maxFormFields))
				return
			}
			value, err := io.ReadAll(io.LimitReader(part, maxFieldValueBytes+1))
			_ = part.Close()
			if err != nil {
				writeError(w, http.StatusBadRequest, "reading form field "+field+": "+err.Error())
				return
			}
			if len(value) > maxFieldValueBytes {
				writeError(w, http.StatusBadRequest,
					fmt.Sprintf("form field %q exceeds the %d byte limit", field, maxFieldValueBytes))
				return
			}
			fields[field] = string(value)
			continue
		}

		// File part: only the content field may carry one. The part is NOT
		// closed here — its bytes stream into the session first (Close
		// would discard the unread remainder).
		if field != "content" {
			_ = part.Close()
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("unexpected file field %q: only 'content' may carry a file", field))
			return
		}
		if contentFilename != "" {
			_ = part.Close()
			writeError(w, http.StatusBadRequest, "duplicate 'content' file field")
			return
		}
		if fields[":action"] != "" && fields[":action"] != "file_upload" {
			// Fail fast when the action is already known to be wrong — no
			// point streaming bytes into a session that cannot land.
			_ = part.Close()
			writeError(w, http.StatusBadRequest, unknownActionMessage(fields[":action"]))
			return
		}
		contentFilename = filename
		contentMime = part.Header.Get("Content-Type")
		if sess, err = h.st.BeginSession(ctx); err != nil {
			_ = part.Close()
			writeError(w, http.StatusInternalServerError, "opening an upload session: "+err.Error())
			return
		}
		sessionStarted = true
		_, appendErr := sess.Append(ctx, part)
		_ = part.Close()
		if appendErr != nil {
			writeError(w, http.StatusBadRequest, "streaming the distribution file: "+appendErr.Error())
			return
		}
	}
	if err := validateUpload(w, fields, contentFilename); err != nil {
		return
	}

	name, version := fields["name"], fields["version"]
	path := name + "/" + version + "/" + contentFilename

	// Duplicate filename: no-overwrite semantics (warehouse "file already
	// exists"; PRD interim ruling R8 — the branch is not in the reverse
	// spec, the 400 is BinFlow's decided posture). The wording must carry
	// "already exists": twine surfaces the response body verbatim and the
	// M33 probe greps for it.
	//
	// The probe is a Get solely because the Service face offers no
	// metadata-only existence check (T-70 review B1 + out-of-scope note 2):
	// the ReadSeekCloser MUST be closed immediately — the local hit path
	// really opens the blob's fd (caller-closes contract). Get also stamps
	// a download audit event per probe — accepted noise until the service
	// face grows a Stat/Head. Known TOCTOU with the finalize below: two
	// concurrent same-filename uploads can both pass the miss and fall
	// into PutLandedBlob's overwrite chain (review N4, no AC covers the
	// concurrent shape; the strict fix needs a "reject-if-exists" Service
	// parameter).
	probeRC, _, err := h.svc.Get(ctx, p, repoKey, path)
	if probeRC != nil {
		_ = probeRC.Close()
	}
	switch {
	case err == nil, errors.Is(err, repo.ErrIsFolder):
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("file '%s' already exists in repository '%s'; overwriting is not allowed (path %s/%s)",
				contentFilename, repoKey, repoKey, path))
		return
	case errors.Is(err, repo.ErrUnauthorized), errors.Is(err, repo.ErrForbidden):
		// A read-denied caller still gets an honest verdict from the WRITE
		// gate inside PutLandedBlob; probing must not preempt it.
	case errors.Is(err, repo.ErrRepoNotFound):
		h.writeServiceError(w, err, r.Method, repoKey, path)
		return
	case errors.Is(err, repo.ErrNodeNotFound):
		// the expected miss
	default:
		h.writeServiceError(w, err, r.Method, repoKey, path)
		return
	}

	// Client-declared digests: md5_digest is OPTIONAL (twine >= 6.2 stopped
	// sending it — the server computes and accepts), sha256_digest is
	// verified the same way when present. Both follow the client-checksums
	// chain: malformed -> 400, well-formed but disagreeing -> 409 at
	// session Commit, before any node exists.
	expect := storage.BlobRef{}
	for _, chk := range []struct {
		field string
		value string
		width int
		algo  string
	}{
		{"md5_digest", fields["md5_digest"], 32, "md5"},
		{"sha256_digest", fields["sha256_digest"], 64, "sha256"},
	} {
		if chk.value == "" {
			continue
		}
		if !isHex(chk.value, chk.width) {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("the %s field must be exactly %d hex characters, got %q",
					chk.field, chk.width, chk.value))
			return
		}
		if chk.algo == "md5" {
			expect.Md5 = strings.ToLower(chk.value)
		} else {
			expect.Sha256 = strings.ToLower(chk.value)
		}
	}

	ref, err := sess.Commit(ctx, expect)
	if err != nil {
		h.writeServiceError(w, err, http.MethodPost, repoKey, path)
		return
	}
	committed = true

	if contentMime == "" {
		contentMime = "application/octet-stream"
	}
	if _, err := h.svc.PutLandedBlob(ctx, p, repoKey, path, ref, contentMime); err != nil {
		h.writeServiceError(w, err, http.MethodPost, repoKey, path)
		return
	}

	// Uniform 200 (warehouse returns 200; Artifactory normalizes the
	// storage layer's 201 to 200 as well — maven-npm-pypi.md section 3.7).
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
}

// validateUpload applies the field-level checks that need no bytes:
// :action strictly file_upload, name/version/content present, every future
// path segment safe. It writes the error response itself and reports
// whether the request failed.
func validateUpload(w http.ResponseWriter, fields map[string]string, contentFilename string) error {
	action := fields[":action"]
	if action == "" {
		writeError(w, http.StatusBadRequest,
			"missing ':action' field (expected 'file_upload')")
		return errUploadRejected
	}
	if action != "file_upload" {
		writeError(w, http.StatusBadRequest, unknownActionMessage(action))
		return errUploadRejected
	}
	if fields["name"] == "" {
		writeError(w, http.StatusBadRequest, "missing 'name' field")
		return errUploadRejected
	}
	if fields["version"] == "" {
		writeError(w, http.StatusBadRequest, "missing 'version' field")
		return errUploadRejected
	}
	if contentFilename == "" {
		writeError(w, http.StatusBadRequest, "missing 'content' file field")
		return errUploadRejected
	}
	for field, value := range map[string]string{
		"name": fields["name"], "version": fields["version"], "filename": contentFilename,
	} {
		if err := validateUploadSegment(field, value); err != nil {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("rejecting upload: %v (the storage path is <name>/<version>/<filename>)", err))
			return errUploadRejected
		}
	}
	path := fields["name"] + "/" + fields["version"] + "/" + contentFilename
	if err := adapter.NormalizeRelPath(path); err != nil {
		// The belt to the segment checks: the composed path must also
		// satisfy the shared artifact-path rules (double defense, the
		// same validator the content plane runs).
		writeError(w, http.StatusBadRequest, err.Error())
		return errUploadRejected
	}
	return nil
}

// errUploadRejected marks a validation failure whose response has already
// been written (the caller only needs the non-nil signal).
var errUploadRejected = errors.New("pypi upload rejected")

// unknownActionMessage is the pinned wrong-action wording (PRD v1.2 M31
// detail four, maven-npm-pypi.md section 3.3: 400 `unknown action '<a>'`).
func unknownActionMessage(action string) string {
	return fmt.Sprintf("unknown action '%s'", action)
}

// rejectNonLocalUpload answers the class gate for write operations: remote
// -> 405 read-only (RE-04), virtual -> 405 with the C5 no-deployment-target
// wording (T-71 owns the routing engine; until it lands the PyPI face
// refuses with the same message the router will give). status 0 means the
// repository is local and the upload may proceed.
func (h *Handler) rejectNonLocalUpload(ctx context.Context, repoKey string) (status int, msg string, allow string) {
	row, err := h.repos.Get(ctx, repoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return http.StatusNotFound,
				fmt.Sprintf("Failed to find the repository '%s' specified in the request.", repoKey), ""
		}
		return http.StatusInternalServerError, "repository lookup failed: " + err.Error(), ""
	}
	switch row.Type {
	case repo.TypeLocal:
		return 0, "", ""
	case repo.TypeRemote:
		return http.StatusMethodNotAllowed,
			fmt.Sprintf("repository '%s' is a remote repository and does not accept uploads; publish to a local repository instead", repoKey),
			"GET"
	default: // virtual (and any future class): C5 wording
		return http.StatusMethodNotAllowed,
			fmt.Sprintf("No local repository was configured as local deployment repository for the (%s) virtual repository.", repoKey),
			"GET"
	}
}

// isHex reports whether s is exactly n lowercase-or-uppercase hex chars.
func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}
