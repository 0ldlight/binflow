package httpapi

// The archive family's three wire faces (M12 T-343, FR-105.3; the behavior
// spec is docs/reverse/repo-operations.md sections 0/2/3/4):
//
//	GET  /binflow/api/archive/download/{repo}[/{path}]?archiveType=…
//	     the folder download — the route family (GET only, the anonymous
//	     decision inside the handler so the §2.2 template's own 401
//	     wording survives), gated through the SAME repo-operations slot as
//	     copy/move (the Q4 final ruling: one entitlement for the whole
//	     operations family, the D4 403 + X-Binflow-License-Required form).
//
//	GET  /binflow/{repo}/{archive}!/{entry}
//	     the archive member read — intercepted in dispatchContent BEFORE
//	     the adapter dispatch (the "!/" addressing is cross-protocol, not a
//	     package-type concern), split on the DECODED path's FIRST "!/"
//	     (section 3.1; strict dot-slash by construction — this router never
//	     normalizes paths).
//
//	PUT  /binflow/{repo}/{path} + X-Explode-Archive[: true|-Atomic: true]
//	     the exploded upload — intercepted in dispatchContent's inner
//	     handler AFTER the RBAC write gate and the package-type gate, the
//	     M10 E-25 "explicit 400 refusal" REVERSED per PRD 105.3 (the
//	     refusal survives only as the bare-mounted adapters' own defense
//	     arm, unreachable through this router).
//
// All three handlers answer through the service capability face asserted
// off Deps.ReposSvc (the copyMoveRunner precedent — the big Service
// interface stays untouched).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The explode trigger headers (section 4.1: either one enters the branch;
// the Atomic spelling additionally selects the all-or-nothing path).
const (
	hdrExplodeArchive       = "X-Explode-Archive"
	hdrExplodeArchiveAtomic = "X-Explode-Archive-Atomic"
)

// archiveFamilyRunner is the service capability face (consumer-side, the
// copyMoveRunner precedent: asserted off Deps.ReposSvc so the hand-written
// adapter fakes that implement Service method by method never break).
type archiveFamilyRunner interface {
	ArchiveDownload(ctx context.Context, p *repo.Principal, req repo.ArchiveDownloadRequest) (*repo.ArchiveDownloadResult, error)
	ArchiveMember(ctx context.Context, p *repo.Principal, req repo.ArchiveMemberRequest) (*repo.ArchiveMemberResult, error)
	ExplodeArchive(ctx context.Context, p *repo.Principal, req repo.ExplodeRequest, body io.Reader) (*repo.ExplodeResult, error)
}

// archiveRunner resolves the capability face or answers the honest 501
// (the deb/helm management-face posture for a missing collaborator).
func (s *Server) archiveRunner(w http.ResponseWriter) (archiveFamilyRunner, bool) {
	svc, ok := s.deps.ReposSvc.(archiveFamilyRunner)
	if !ok {
		writeError(w, http.StatusNotImplemented, "the archive operations family is not available on this instance")
		return nil, false
	}
	return svc, true
}

// archiveAddonGate is the family's uniform first line: the repo-operations
// entitlement (weave 3), AFTER the route's own RBAC door wherever one
// exists (the T-283 order).
func (s *Server) archiveAddonGate(w http.ResponseWriter, r *http.Request) bool {
	return s.RequireAddon(w, r, addonRepoOperations.ID, addonRepoOperations.MinTier)
}

// ---- GET /api/archive/download ----

// handleArchiveDownload serves the folder download. rest is the raw route
// tail after /binflow/api/ ("archive/download[/…]"). The §2.2 execution
// order lives in the service; this face owns the license gate, the
// anonymous gate's rendering, the query parse and the streaming response.
func (s *Server) handleArchiveDownload(w http.ResponseWriter, r *http.Request, rest string) {
	if !s.archiveAddonGate(w, r) {
		return
	}
	svc, ok := s.archiveRunner(w)
	if !ok {
		return
	}

	// The anonymous gate is the handler's own (§2.2 step 1): the route
	// carries no required gate precisely so this spec-worded 401 — not the
	// generic challenge — is what an anonymous caller on a closed-for-
	// anonymous instance meets.
	p := principalFrom(r.Context())

	seg := strings.TrimPrefix(rest, "archive/download")
	seg = strings.TrimPrefix(seg, "/")
	keyRaw, tailRaw, _ := strings.Cut(seg, "/")
	if keyRaw == "" {
		// The whole-repository form still names its repository; the bare
		// spelling is not a route (the E-26 family).
		notImplemented(w, "/binflow/api/"+rest)
		return
	}
	repoKey, err := url.PathUnescape(keyRaw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository key escaping")
		return
	}
	relPath, err := url.PathUnescape(tailRaw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid path escaping")
		return
	}

	q := r.URL.Query()
	req := repo.ArchiveDownloadRequest{
		RepoKey:              repoKey,
		Path:                 relPath,
		Type:                 q.Get("archiveType"),
		IncludeChecksumFiles: isTruthyFlag(q.Get("includeChecksumFiles")),
	}
	res, err := svc.ArchiveDownload(r.Context(), p, req)
	if err != nil {
		s.writeArchiveError(w, r, err)
		return
	}
	defer res.Body.Close() //nolint:errcheck // releases the concurrency slot

	h := w.Header()
	h.Set("Content-Type", res.ContentType)
	h.Set("Content-Disposition", `attachment; filename="`+res.Filename+`"`)
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, res.Body) // a mid-stream fault truncates: §2.2's no-staging cost
}

// ---- GET /{repo}/{archive}!/{entry} ----

// archiveMemberTail reports whether the request's decoded content path
// carries the "!/" member-addressing marker, returning the decoded tail
// (the /binflow prefix stripped). The split is judged on the DECODED form —
// this router's stated rule (the package comment): percent-encoded
// spellings are assessed after decoding, never rewritten.
func archiveMemberTail(r *http.Request) (string, bool) {
	decoded, err := url.PathUnescape(r.URL.EscapedPath())
	if err != nil {
		return "", false // the adapter planes answer the malformed escape
	}
	tail := strings.TrimPrefix(decoded, prefix+"/")
	if !strings.Contains(tail, archiveMemberSep) {
		return "", false
	}
	return tail, true
}

// archiveMemberSep is the member-addressing marker (§3.1: the "!" must be
// immediately followed by "/").
const archiveMemberSep = "!/"

// handleArchiveMember serves one member read off the content plane. The
// read gate, the remote pull-through and the virtual resolution all ride
// the service's Get on the ARCHIVE path — the spec's rule that member
// reads carry only the archive's read permission (V-6's negative
// assertion: no archiveBrowsingEnabled-style toggle exists on this path).
func (s *Server) handleArchiveMember(w http.ResponseWriter, r *http.Request, tail string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed,
			fmt.Sprintf("method %s is not supported on archive member paths", r.Method))
		return
	}
	if !s.archiveAddonGate(w, r) {
		return
	}
	svc, ok := s.archiveRunner(w)
	if !ok {
		return
	}

	// The matrix-parameter peel (§3.1: matrix addressing applies on the
	// archive path as on any content path): peel the paired ";k=v" tail
	// off the whole decoded spelling, validating exactly like the adapter
	// planes do, THEN split at the first "!/".
	clean, matrix := adapter.SplitMatrixParams(tail)
	if _, err := adapter.ParseMatrixProps(matrix); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	archivePart, entry, found := strings.Cut(clean, archiveMemberSep)
	if !found {
		writeError(w, http.StatusBadRequest, "archive member addressing requires '!/' after the archive path")
		return
	}
	repoKey, archivePath, _ := strings.Cut(archivePart, "/")
	if repoKey == "" || archivePath == "" || entry == "" {
		writeError(w, http.StatusBadRequest, "archive member addressing requires a repository, an archive path and a member path")
		return
	}

	res, err := svc.ArchiveMember(r.Context(), principalFrom(r.Context()), repo.ArchiveMemberRequest{
		RepoKey: repoKey, ArchivePath: archivePath, Entry: entry,
	})
	if err != nil {
		s.writeArchiveError(w, r, err)
		return
	}
	defer res.Body.Close() //nolint:errcheck // read-only stream

	h := w.Header()
	h.Set("Content-Type", res.ContentType)
	h.Set("X-Content-Type-Options", "nosniff")
	if res.Size >= 0 {
		h.Set("Content-Length", strconv.FormatInt(res.Size, 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, res.Body)
}

// ---- PUT + X-Explode-Archive ----

// explodeIntent inspects a content PUT for the explode trigger headers
// (§4.1: either spelling enters the branch; the Atomic spelling selects
// all-or-nothing). ok=false leaves the request on the normal deploy plane.
// A header value that is neither a truthy nor a falsy spelling is refused
// explicitly (400) rather than silently ignored — an unrecognized value
// must not half-select the branch. That refusal is returned as an error.
func explodeIntent(r *http.Request) (req repo.ExplodeRequest, ok bool, err error) {
	if r.Method != http.MethodPut {
		return repo.ExplodeRequest{}, false, nil
	}
	truthy := func(v string) bool {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes":
			return true
		}
		return false
	}
	falsy := func(v string) bool {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "", "false", "0", "no":
			return true
		}
		return false
	}
	plain, atomic := r.Header.Get(hdrExplodeArchive), r.Header.Get(hdrExplodeArchiveAtomic)
	for _, v := range []string{plain, atomic} {
		if !truthy(v) && !falsy(v) {
			return repo.ExplodeRequest{}, false, fmt.Errorf("invalid %s value: %q (use true or false)", hdrExplodeArchive, v)
		}
	}
	if !truthy(plain) && !truthy(atomic) {
		return repo.ExplodeRequest{}, false, nil
	}
	return repo.ExplodeRequest{Atomic: truthy(atomic)}, true, nil
}

// contentAddressing decodes and matrix-peels a content request's path into
// (repoKey, relPath) — the same single-point normalization the adapter
// planes run (adapter.ResolveContent), kept here because the intercepts
// sit BEFORE the adapter dispatch and must not address a different node
// than the adapter grammar would.
func contentAddressing(r *http.Request) (repoKey, relPath string, err error) {
	decoded, derr := url.PathUnescape(r.URL.EscapedPath())
	if derr != nil {
		return "", "", fmt.Errorf("malformed percent-encoding in %q: %w", r.URL.EscapedPath(), derr)
	}
	clean, matrix := adapter.SplitMatrixParams(decoded)
	if _, perr := adapter.ParseMatrixProps(matrix); perr != nil {
		return "", "", perr
	}
	tail := strings.TrimPrefix(clean, prefix+"/")
	repoKey, relPath, _ = strings.Cut(tail, "/")
	return repoKey, relPath, nil
}

// handleExplode serves the exploded upload. The RBAC write gate and the
// package-type license gate have already run (dispatchContent's chain);
// this face owns the repo-operations gate, the header parse, the service
// call and the V-1 success rendering: 201 with an EMPTY body (the
// documented status over the decompiled implicit 200 — the T-343 ruling,
// no live instance was reachable to break the tie).
func (s *Server) handleExplode(w http.ResponseWriter, r *http.Request, repoKey string, intent repo.ExplodeRequest) {
	if !s.archiveAddonGate(w, r) {
		return
	}
	svc, ok := s.archiveRunner(w)
	if !ok {
		return
	}
	_, relPath, err := contentAddressing(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	intent.RepoKey, intent.Path = repoKey, relPath
	res, err := svc.ExplodeArchive(r.Context(), principalFrom(r.Context()), intent, r.Body)
	if err != nil {
		s.writeArchiveError(w, r, err)
		return
	}
	w.Header().Set("X-Binflow-Exploded-Files", strconv.Itoa(res.Files))
	w.WriteHeader(http.StatusCreated)
}

// writeArchiveError maps the family's service failures: the *StatusError
// renders verbatim (the spec's own wordings), the shared sentinels keep
// the /api/storage-family mapping, everything else is an honest 500.
func (s *Server) writeArchiveError(w http.ResponseWriter, r *http.Request, err error) {
	var se *repo.StatusError
	if errors.As(err, &se) {
		for k, vv := range se.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		if se.Code == http.StatusUnauthorized {
			// Every 401 on this plane carries the challenge (rest-api
			// section 1.4), the family's spec-worded anonymous gate
			// included.
			w.Header().Set("WWW-Authenticate", basicChallenge)
		}
		writeError(w, se.Code, se.Message)
		return
	}
	switch {
	case errors.Is(err, repo.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", basicChallenge)
		writeError(w, http.StatusUnauthorized, "Authentication is required.")
	case errors.Is(err, repo.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, repo.ErrRepoNotFound):
		writeError(w, http.StatusNotFound,
			fmt.Sprintf("Failed to find the repository specified in the request (%s).", err.Error()))
	case errors.Is(err, repo.ErrNodeNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, repo.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrRepoTypeNotSupported):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.log.ErrorContext(r.Context(), "httpapi: archive family service failure",
			"path", r.URL.EscapedPath(), "error", err.Error())
		writeError(w, http.StatusInternalServerError, "archive operation failed")
	}
}
