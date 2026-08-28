package nuget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// blobRefOf declares the measured sha256 of one small in-memory body (the
// sidecar writes' expect argument).
func blobRefOf(b []byte) storage.BlobRef {
	sum := sha256.Sum256(b)
	return storage.BlobRef{Sha256: hex.EncodeToString(sum[:])}
}

// slogWarnContext is slog.WarnContext under one spelling.
func slogWarnContext(ctx context.Context, msg string, args ...any) {
	slog.WarnContext(ctx, msg, args...)
}

// The flatcontainer faces: the versions document, the package-file trio
// (nupkg / .nupkg.sha512 / .nuspec), the PUT push and the DELETE.
//
// Class dispatch per face:
//
//	versions:  local = generated from facts; remote = pull-through (the
//	           identity path); virtual = member union.
//	package:   svc.Get on every class (remote rides the pull-through
//	           engine; virtual rides the first-hit resolver).
//	push:      svc.Put on the LOCAL class (a virtual repository routes
//	           onto its deployment member inside the service); a REMOTE
//	           repository answers the service's read-only refusal.

// packageVersionRow is one version's fact row (the listing input).
type packageVersionRow struct {
	version string
	node    *metadata.Node
}

// listPackageVersions lists a repository's stored versions of one package
// id (the flatcontainer spelling walk). Sorted ascending by the NuGet
// order.
func (h *Handler) listPackageVersions(ctx context.Context, p *repo.Principal, repoKey, id string) ([]packageVersionRow, error) {
	nodes, err := h.svc.List(ctx, p, repoKey, id+"/")
	if err != nil {
		return nil, err
	}
	var rows []packageVersionRow
	for _, n := range nodes {
		if v, ok := versionOfNupkgNode(n.Path, id); ok {
			rows = append(rows, packageVersionRow{version: v, node: n})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		return compareNuGetVersions(rows[i].version, rows[j].version) < 0
	})
	return rows, nil
}

// versionsDocument is the flatcontainer versions body.
type versionsDocument struct {
	Versions []string `json:"versions"`
}

// serveVersions renders GET flatcontainer/<id>/index.json.
func (h *Handler) serveVersions(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, class string, rt route) {
	switch class {
	case repo.TypeRemote:
		// Pull-through at the dynamically resolved upstream path (the
		// section 9.3 .nuGetV3 marker): the engine's cache, TTLs, negative
		// cache and stale downgrade all apply unmodified.
		read := h.v3RepoReader(ctx, p, repoKey)
		flat := v3ResolveUpstreamIndex(read).flatPath()
		rc, node, err := h.svc.Get(ctx, p, repoKey, v3CachePath(flat, versionsPath(rt.id)))
		if err != nil {
			h.writeError(w, err, repoKey, rt.id)
			return
		}
		h.serveNode(ctx, w, r, node, rc, "application/json")
	case repo.TypeVirtual:
		h.serveVirtualVersions(ctx, w, repoKey, rt)
	default:
		rows, err := h.listPackageVersions(ctx, p, repoKey, rt.id)
		if err != nil {
			h.writeError(w, err, repoKey, rt.id)
			return
		}
		versions := make([]string, 0, len(rows))
		for _, row := range rows {
			versions = append(versions, row.version)
		}
		body, merr := json.Marshal(versionsDocument{Versions: versions})
		if merr != nil {
			writePlain(w, http.StatusInternalServerError, merr.Error())
			return
		}
		writeJSON(w, http.StatusOK, body)
	}
}

// servePackageFile renders the package-file trio. The remote class joins
// the dynamically resolved upstream flatcontainer base (the .nuGetV3
// marker); every other class keeps the identity storage mapping.
func (h *Handler) servePackageFile(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, class string, rt route) {
	ref := pkgRef{id: rt.id, version: rt.version}
	var path string
	switch rt.file {
	case "nupkg":
		path = ref.nupkg()
	case "sha512":
		path = ref.sha512()
	case "nuspec":
		path = ref.nuspec()
	default:
		writePlain(w, http.StatusNotFound, "not found")
		return
	}

	if class == repo.TypeRemote {
		// The remote arm addresses the upstream document family directly —
		// a missing sha512 is the upstream's honest 404 (no synthesis on
		// the remote class, the T-287 posture).
		read := h.v3RepoReader(ctx, p, repoKey)
		flat := v3ResolveUpstreamIndex(read).flatPath()
		marker := v3CachePath(flat, path)
		rc, node, err := h.svc.Get(ctx, p, repoKey, marker)
		if err != nil {
			h.writeError(w, err, repoKey, marker)
			return
		}
		h.serveNode(ctx, w, r, node, rc, contentTypeOfPackageFile(rt.file))
		return
	}

	// The sha512 sidecar has a synthesis fallback: a package whose sidecar
	// is missing (a bare-content upload that skipped push, or a remote
	// upstream without the file) still answers — the digest is computed
	// off the stored blob, never trusted from the client.
	if rt.file == "sha512" {
		if ok := h.serveSha512Sidecar(ctx, w, r, p, repoKey, class, ref); ok {
			return
		}
	}

	rc, node, err := h.svc.Get(ctx, p, repoKey, path)
	if err == nil {
		h.serveNode(ctx, w, r, node, rc, contentTypeOfPackageFile(rt.file))
		return
	}
	if class == repo.TypeVirtual {
		// The service's first-hit resolution serves local members and
		// nuget.org-family remote members at the canonical path; behind its
		// miss, the REMOTE members' dynamically resolved markers answer
		// (the heterogeneous-upstream hop the service-level walk cannot
		// express — the adapter owns the resolution).
		if rc, node, ok := h.virtualRemotePackageFile(ctx, repoKey, path); ok {
			h.serveNode(ctx, w, r, node, rc, contentTypeOfPackageFile(rt.file))
			return
		}
	}
	h.writeError(w, err, repoKey, path)
}

// virtualRemotePackageFile walks one package-file canonical path through
// the VIRTUAL's remote members under their dynamically resolved
// flatcontainer markers (first hit wins; authorization rides the member
// seam).
func (h *Handler) virtualRemotePackageFile(ctx context.Context, repoKey, path string) (io.ReadSeekCloser, *metadata.Node, bool) {
	order, err := h.svc.VirtualMemberOrder(ctx, repoKey)
	if err != nil {
		return nil, nil, false
	}
	for _, m := range order {
		if m.Type != repo.TypeRemote {
			continue
		}
		read := h.v3MemberReader(ctx, repoKey, m.Key)
		marker := v3CachePath(v3ResolveUpstreamIndex(read).flatPath(), path)
		if rc, node, gerr := h.svc.ReadVirtualMember(ctx, repoKey, m.Key, marker); gerr == nil {
			return rc, node, true
		}
	}
	return nil, nil, false
}

// serveSha512Sidecar serves the stored sidecar when it exists, and the
// synthesized digest when it does not (local class only — on remote the
// miss is the upstream's honest 404). It reports whether it wrote a
// response.
func (h *Handler) serveSha512Sidecar(ctx context.Context, w http.ResponseWriter, _ *http.Request, p *repo.Principal, repoKey, class string, ref pkgRef) bool {
	rc, _, err := h.svc.Get(ctx, p, repoKey, ref.sha512())
	if err == nil {
		defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
		body, rerr := io.ReadAll(io.LimitReader(rc, 1<<20))
		if rerr == nil {
			digest := trimSpaceASCII(string(body))
			if digest != "" {
				writeText(w, http.StatusOK, digest)
				return true
			}
		}
		return false
	}
	if !errors.Is(err, repo.ErrNodeNotFound) || class == repo.TypeRemote {
		return false
	}
	// Synthesize: hash the stored package blob.
	rc2, _, err := h.svc.Get(ctx, p, repoKey, ref.nupkg())
	if err != nil {
		return false
	}
	defer func() { _ = rc2.Close() }() //nolint:errcheck // read-only fd
	sum := sha512.New()
	if _, cerr := io.Copy(sum, rc2); cerr != nil {
		return false
	}
	writeText(w, http.StatusOK, base64.StdEncoding.EncodeToString(sum.Sum(nil)))
	return true
}

// writeText renders one small text body.
func writeText(w http.ResponseWriter, status int, body string) {
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Length", fmt.Sprint(len(body)))
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body) //nolint:gosec // G705: server-computed digest text
}

// servePush implements the PUT push chain: spool + hash, zip/nuspec
// validation (400), the package landing, the two server-generated
// sidecars, 201 with the canonical Location.
//
// Two URL shapes reach here: the addressed form
// (flatcontainer/<id>/<version>, the PRD's carrier and the curl surface)
// and the DIRECT form (the publish base itself — the modern dotnet
// client's shape, the identity taken from the embedded nuspec; 8.x
// verified live, T-287). The nuspec identity is authoritative in both:
// an addressed push whose URL disagrees with the nuspec is the 400.
//
// The ADDON GATE already ran at dispatchContent (write verbs); the
// adapter never re-asks. A push onto a remote repository dies at the
// service's read-only door (RE-05's family).
func (h *Handler) servePush(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, _ string, rt route) {
	if rt.kind != kindPush && rt.kind != kindV2Push && rt.kind != kindPushDirect {
		writePlain(w, http.StatusNotFound, "not found")
		return
	}
	body, closeBody, err := packageBodyReader(r)
	if err != nil {
		writePlain(w, http.StatusBadRequest, err.Error())
		return
	}
	defer closeBody()
	sp, err := spoolNupkg(body)
	if err != nil {
		h.writePushError(w, err)
		return
	}
	defer sp.close()

	var target pkgRef
	var nuspecBody []byte
	if rt.kind == kindPushDirect {
		// The identity comes from the package itself.
		nuspecBody, target, err = sp.identityFromNuspec()
		if err != nil {
			h.writePushError(w, err)
			return
		}
	} else {
		target = pkgRef{id: rt.id, version: rt.version}
		nuspecBody, _, err = sp.validatePush(target)
		if err != nil {
			h.writePushError(w, err)
			return
		}
	}

	// Declared client digests (the shared contract; NuGet clients send
	// none, curl deployments may).
	expect, err := declaredDigests(r.Header)
	if err != nil {
		writePlain(w, http.StatusBadRequest, err.Error())
		return
	}

	// Land the package. The spooled file re-reads as the body — the
	// package is never held in memory whole.
	if _, err := sp.file.Seek(0, io.SeekStart); err != nil {
		writePlain(w, http.StatusInternalServerError, fmt.Sprintf("rewind spool: %v", err))
		return
	}
	node, err := h.svc.Put(ctx, p, repoKey, target.nupkg(), sp.file, expect, "application/octet-stream")
	if err != nil {
		h.writeError(w, err, repoKey, target.nupkg())
		return
	}

	// The sidecars: the extracted nuspec (verbatim) and the measured
	// SHA-512 (base64). Regenerable-content writes — a later push of the
	// same version rewrites them without a delete-permission demand.
	if _, err := h.svc.PutWithOptions(ctx, p, repoKey, target.nuspec(),
		bytes.NewReader(nuspecBody), blobRefOf(nuspecBody), "application/xml",
		repo.PutOptions{SkipOverwriteCheck: true}); err != nil {
		h.warnSidecar(ctx, repoKey, target.nuspec(), err)
	}
	shaBody := sp.sha512Base64()
	if _, err := h.svc.PutWithOptions(ctx, p, repoKey, target.sha512(),
		strings.NewReader(shaBody), blobRefOf([]byte(shaBody)), "text/plain; charset=utf-8",
		repo.PutOptions{SkipOverwriteCheck: true}); err != nil {
		h.warnSidecar(ctx, repoKey, target.sha512(), err)
	}

	w.Header().Set("Location", target.nupkg())
	if node != nil && node.Sha256 != "" {
		w.Header().Set(hdrChecksumSha256, node.Sha256)
	}
	w.WriteHeader(http.StatusCreated)
}

// warnSidecar logs a failed sidecar landing (the package itself is stored;
// the documents degrade to synthesis — the read path must not fail).
func (h *Handler) warnSidecar(ctx context.Context, repoKey, path string, err error) {
	slogWarnContext(ctx, "nuget: sidecar write failed (documents will synthesize)",
		"repo", repoKey, "path", path, "error", err.Error())
}

// writePushError renders the validation-family refusal.
func (h *Handler) writePushError(w http.ResponseWriter, err error) {
	if errors.Is(err, errMissingPackageField) {
		writePlain(w, http.StatusBadRequest, msgMissingPackageField)
		return
	}
	if errors.Is(err, errInvalidPackage) {
		writePlain(w, http.StatusBadRequest, err.Error())
		return
	}
	writePlain(w, http.StatusInternalServerError, err.Error())
}

// msgMissingPackageField is the publish refusal's exact wording (nuget.md
// section 2 #17: the form-data body without the package field).
const msgMissingPackageField = "Unable to find 'package' field in request form data."

// errMissingPackageField is the sentinel behind the exact-wording refusal.
var errMissingPackageField = errors.New(msgMissingPackageField) //nolint:staticcheck // ST1005: the protocol's exact wording (nuget.md section 2 #17) ends with a period

// packageBodyReader unwraps the push body: the dotnet client (8.x
// verified live, T-287 — and Artifactory's FormDataMultiParam publish
// route before it) sends the package as a ONE-PART multipart/form-data
// body (name=package, filename=package.nupkg), while the curl surface
// and nuget.exe-era clients PUT the raw octet stream. A form-data body
// whose parts carry no field NAMED "package" is the exact-wording 400;
// the raw spelling passes through untouched.
func packageBodyReader(r *http.Request) (io.Reader, func(), error) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return r.Body, func() {}, nil
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, nil, fmt.Errorf("%w: multipart push body carries no boundary", errInvalidPackage)
	}
	mr := multipart.NewReader(r.Body, boundary)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%w: multipart push body unreadable: %w", errInvalidPackage, err)
		}
		if part.FormName() != "package" {
			_ = part.Close() //nolint:errcheck // read-only part fd
			continue
		}
		return part, func() { _ = part.Close() }, nil //nolint:errcheck // read-only part fd
	}
	return nil, nil, fmt.Errorf("%w: %w", errInvalidPackage, errMissingPackageField)
}

// serveDelete implements the unlist verb as a hard delete of the version
// directory (T-287 ruling: no listed bit in the M10 storage model — the
// package, its sidecars and the folder rows go; the wire contract — 204
// on success, 404 when absent — is the official one).
func (h *Handler) serveDelete(ctx context.Context, w http.ResponseWriter, _ *http.Request, p *repo.Principal, repoKey, _ string, rt route) {
	dir := pkgRef{id: rt.id, version: rt.version}.dir() + "/"
	if err := h.svc.Delete(ctx, p, repoKey, dir); err != nil {
		h.writeError(w, err, repoKey, dir)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// serveBareContent is the raw storage face (no plane segment): GET/HEAD
// stream the node, PUT lands bytes verbatim, DELETE removes — the curl
// and debug reachability the other adapters' content planes give.
func (h *Handler) serveBareContent(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, rel string) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		rc, node, err := h.svc.Get(ctx, p, repoKey, rel)
		if err != nil {
			h.writeError(w, err, repoKey, rel)
			return
		}
		ctype := node.Mime
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		h.serveNode(ctx, w, r, node, rc, ctype)
	case http.MethodPut:
		expect, err := declaredDigests(r.Header)
		if err != nil {
			writePlain(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, err := h.svc.Put(ctx, p, repoKey, rel, r.Body, expect, "application/octet-stream"); err != nil {
			h.writeError(w, err, repoKey, rel)
			return
		}
		w.Header().Set("Location", rel)
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		if err := h.svc.Delete(ctx, p, repoKey, rel); err != nil {
			h.writeError(w, err, repoKey, rel)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT, DELETE")
		writePlain(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on content paths")
	}
}
