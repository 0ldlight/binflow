package generic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// protocol-owned header names (Artifactory compatibility, rest-api.md 1.3).
const (
	hdrChecksumSha256 = "X-Checksum-Sha256"
	hdrChecksumSha1   = "X-Checksum-Sha1"
	hdrChecksumMd5    = "X-Checksum-Md5"
	hdrChecksumDeploy = "X-Checksum-Deploy"
	hdrExplodeArchive = "X-Explode-Archive"
)

// contentTypeFileInfo is the ItemCreated body of a successful upload
// (rest-api.md 1.2): FileInfo JSON with size serialized as a string.
const contentTypeFileInfo = "application/vnd.org.jfrog.artifactory.storage.ItemCreated+json; charset=UTF-8"

// Protocol implements adapter.Handler.
func (h *Handler) Protocol() string { return Protocol }

// RepoTypes implements adapter.Handler: M1 generic served local repos; M3
// (T-66) adds REMOTE — the content plane dispatches remote GET/HEAD to the
// pull-through engine inside repo.Service and refuses writes with RE-05's
// 405, all of it invisible at this layer (architecture section 5.4:
// repository-class differences live in the service). virtual joins with
// T-71's resolver.
func (h *Handler) RepoTypes() []string { return []string{repo.TypeLocal, repo.TypeRemote} }

// Layout implements adapter.Handler via the shared generic layout parser.
func (h *Handler) Layout(r *http.Request) (string, string, error) {
	return adapter.Layout(r)
}

// ServeHTTP dispatches the four content verbs. Middleware (auth, audit
// scaffolding, error envelope injection) is httpapi's; this handler only
// owns protocol semantics and renders every failure as the errors[] JSON
// envelope itself, so a bare mount still never leaks an HTML error page.
//
// Since M10 (T-286) the path resolves through adapter.ResolveContent: a
// paired ";k=v" trailing sequence is peeled off as deploy properties and
// boxed into the request context (adapter.WithDeployProps) — reads use
// the stripped path for addressing only, the PUT arm hands the set to
// repo.PutOptions.Properties.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repoKey, relPath, props, err := adapter.ResolveContent(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := adapter.WithDeployProps(r.Context(), props)
	p := adapter.PrincipalFrom(ctx)

	switch r.Method {
	case http.MethodPut:
		h.handlePut(ctx, w, r, p, repoKey, relPath)
	case http.MethodGet, http.MethodHead:
		h.handleGet(ctx, w, r, p, repoKey, relPath)
	case http.MethodDelete:
		h.handleDelete(ctx, w, p, repoKey, relPath)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT, DELETE")
		writeError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s is not supported on content paths", r.Method))
	}
}

// ---- PUT ----

// handlePut implements the upload semantics of rest-api.md sections 1.2/1.3
// and PRD E-11: client checksums are verified per algorithm (409 on
// disagreement, message carrying received/actual), X-Checksum-Deploy
// performs a zero-transfer deploy, a trailing slash creates a folder node.
func (h *Handler) handlePut(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, relPath string) {
	if v := r.Header.Get(hdrExplodeArchive); v != "" && !strings.EqualFold(v, "false") {
		writeError(w, http.StatusBadRequest, "X-Explode-Archive is not supported in BinFlow M1")
		return
	}
	// Old metadata notation is rejected before anything else touches the
	// path (rest-api.md 1.2 step 3, high confidence).
	if suffix := pathSuffix(relPath); suffix == ":properties" || suffix == ":statistics" {
		writeError(w, http.StatusConflict, "Old metadata notation is not supported anymore")
		return
	}

	// The path's extension owns the stored mime (BIN-53 / T-571): the
	// factory-table lookup runs for every PUT and the request's declared
	// Content-Type takes no part in it — the 7.161.26 18-leg matrix shows
	// every explicit declaration answered from the table (the earlier
	// "Artifactory honors the header too" note here was wrong, corrected
	// with the flip). curl -T's absent header and a declared one land
	// identically: PUT x.json stores application/json either way.
	mime := mimeByPath(relPath)

	// X-Checksum-Deploy: zero-transfer deploy against an existing blob.
	if deploy, ok := headerBool(r.Header, hdrChecksumDeploy); ok && deploy {
		h.handleChecksumDeploy(ctx, w, r, p, repoKey, relPath, mime)
		return
	}

	// T-574 / BIN-56 (generic/checksum-terminal-suffix-put-routing; L037
	// Arm 1's A model, 7.161.26 dual-round): a PUT whose path TERMINATES
	// in .sha1/.md5/.sha256 is a client-checksum write on the stripped
	// source artifact, never a file deploy — content, source extension
	// and source spelling are all irrelevant to the routing. The model's
	// storage half (originalChecksums write-through plus the GET echo of
	// the stored client value) needs a client-checksum persistence seam
	// repo.Service does not expose yet — that half is the ticket's
	// registered handoff, so today only the SOURCE-MISSING arm
	// intercepts: a checksum for nothing registers nothing (404, the
	// reference's wording verbatim, no sidecar node). A live source keeps
	// the ordinary deploy chain below as an explicitly interim state.
	if src, ok := checksumPutSource(relPath); ok {
		// LOCAL-only (R9 reviews A+B blocking): the probe is svc.Get, and
		// on a remote plane that PULLS THROUGH — a write verb warming the
		// cache — while an upstream miss would answer the checksum 404 in
		// place of the plane's own 405 read-only refusal (RE-05). Virtual
		// and remote planes keep their ordinary chain; same posture as the
		// maven arm's local gate.
		if row, rerr := h.class.Get(r.Context(), repoKey); rerr == nil && row.Type == repo.TypeLocal {
			if h.interceptMissingChecksumPut(ctx, w, r, p, repoKey, src) {
				return
			}
		}
	}

	expect, err := declaredDigests(r.Header)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Deploy properties ride the options seam (T-286, architecture section
	// 15.3.1): the matrix set ServeHTTP boxed off the path lands with the
	// node. A path without matrix parameters passes the zero options — the
	// plain-Put behavior, byte for byte.
	var opts repo.PutOptions
	if props := adapter.DeployPropsFrom(ctx); len(props) > 0 {
		opts.Properties = map[string][]string(props)
	}
	node, err := h.svc.PutWithOptions(ctx, p, repoKey, relPath, r.Body, expect, mime, opts)
	if err != nil {
		h.writeServiceError(w, err, r.Method, repoKey, relPath)
		return
	}
	h.writeCreated(w, r, repoKey, relPath, node, uploadContext{declared: declaredSet(expect)})
}

// terminalChecksumSuffixes is the client-checksum PUT interception family
// (T-574 / BIN-56, L037 Arm 1): the path's TERMINAL suffix alone routes —
// .sha512, .asc and compound tails like .sha1.bak are ordinary deploys.
var terminalChecksumSuffixes = []string{".sha1", ".md5", ".sha256"}

// checksumPutSource strips the terminal checksum suffix off a PUT path,
// reporting the source artifact path the client-checksum write addresses.
func checksumPutSource(relPath string) (src string, ok bool) {
	file := relPath[strings.LastIndexByte(relPath, '/')+1:]
	for _, sfx := range terminalChecksumSuffixes {
		if strings.HasSuffix(file, sfx) && len(file) > len(sfx) {
			return strings.TrimSuffix(relPath, sfx), true
		}
	}
	return "", false
}

// interceptMissingChecksumPut answers the terminal-checksum PUT whose
// SOURCE artifact does not exist: 404 "Target file to set checksum on
// doesn't exist: <repo>:<src>" (7.161.26 verbatim, L037 Arm 1 — rendered
// for every body shape, the routing is content-blind; a folder source
// lumps into the same miss). It reports whether the request was answered;
// a live source — or any lookup error with its own shape, e.g. the
// remote-plane refusals — returns false so the caller continues down the
// ordinary chain unchanged.
func (h *Handler) interceptMissingChecksumPut(ctx context.Context, w http.ResponseWriter,
	r *http.Request, p *repo.Principal, repoKey, src string) bool {
	rc, _, err := h.svc.Get(ctx, p, repoKey, src)
	if err == nil {
		_ = rc.Close() //nolint:errcheck // read-only probe; only existence is needed
		return false
	}
	if !errors.Is(err, repo.ErrNodeNotFound) && !errors.Is(err, repo.ErrIsFolder) {
		return false
	}
	_, _ = io.Copy(io.Discard, r.Body)
	writeError(w, http.StatusNotFound,
		fmt.Sprintf("Target file to set checksum on doesn't exist: %s:%s", repoKey, src))
	return true
}

// handleChecksumDeploy implements X-Checksum-Deploy (rest-api.md 1.3):
// no body transfer; the declared sha256 (or sha1) must name a blob the
// filestore already holds. Missing dedicated headers -> 400; malformed
// digest or unknown blob -> 404; hit -> 201 with zero bytes transmitted.
func (h *Handler) handleChecksumDeploy(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, relPath, mime string) {
	sha256 := strings.ToLower(strings.TrimSpace(r.Header.Get(hdrChecksumSha256)))
	sha1 := strings.ToLower(strings.TrimSpace(r.Header.Get(hdrChecksumSha1)))
	md5 := strings.ToLower(strings.TrimSpace(r.Header.Get(hdrChecksumMd5)))
	if sha256 == "" && sha1 == "" {
		writeError(w, http.StatusBadRequest,
			"Checksum deploy failed. no checksum header 'X-Checksum-Sha1/X-Checksum-Sha256' was found.")
		return
	}
	if sha256 != "" && !isHex(sha256, 64) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Checksum deploy failed: malformed sha256 value %q.", sha256))
		return
	}
	if sha1 != "" && !isHex(sha1, 40) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Checksum deploy failed: malformed sha1 value %q.", sha1))
		return
	}
	if md5 != "" && !isHex(md5, 32) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Checksum deploy failed: malformed md5 value %q.", md5))
		return
	}

	// The declared checksum addresses the blob: sha256 directly, or — since
	// T-73 (PRD §6.4-1) — a sha1-only declaration, which the service
	// resolves through the ledger's sha1 index. A miss (either key) answers
	// the same 404 below: the client cannot distinguish "never uploaded"
	// from "wrong checksum", which is the spec's posture.
	//
	// Zero-transfer deploy goes through repo.Service.PutFromBlob (T-13
	// review B1: the section 5.1 exception clause is "extend the Service",
	// not "open blobs from the adapter"). The ledger row — not the client's
	// header claims — is the sha1/md5 source of truth inside the service.
	ref := storage.BlobRef{Sha256: sha256, Sha1: sha1, Md5: md5}
	node, err := h.svc.PutFromBlob(ctx, p, repoKey, relPath, ref, mime)
	if err != nil {
		if errors.Is(err, repo.ErrOrphanBlob) || errors.Is(err, repo.ErrNodeNotFound) {
			writeError(w, http.StatusNotFound, "Checksum deploy failed: no content found for the given checksum.")
			return
		}
		h.writeServiceError(w, err, r.Method, repoKey, relPath)
		return
	}
	h.writeCreated(w, r, repoKey, relPath, node, uploadContext{declared: declaredSet(ref)})
}

// declaredDigests parses the X-Checksum-* headers into a BlobRef. Malformed
// values (wrong width, non-hex) are 400-shaped errors, distinct from a
// checksum *mismatch* (409): a client that cannot even spell its digest
// never reached the comparison.
func declaredDigests(hdr http.Header) (storage.BlobRef, error) {
	parse := func(name string, width int) (string, error) {
		v := strings.ToLower(strings.TrimSpace(hdr.Get(name)))
		if v == "" {
			return "", nil
		}
		if !isHex(v, width) {
			return "", fmt.Errorf("%w: %s must be exactly %d hex characters, got %q",
				adapter.ErrInvalidChecksum, name, width, v)
		}
		return v, nil
	}
	sha256, err := parse(hdrChecksumSha256, 64)
	if err != nil {
		return storage.BlobRef{}, err
	}
	sha1, err := parse(hdrChecksumSha1, 40)
	if err != nil {
		return storage.BlobRef{}, err
	}
	md5, err := parse(hdrChecksumMd5, 32)
	if err != nil {
		return storage.BlobRef{}, err
	}
	return storage.BlobRef{Sha256: sha256, Sha1: sha1, Md5: md5}, nil
}

// declaredSet remembers which algorithms the client actually declared, so
// originalChecksums echoes exactly those (repo-semantics section 5: the
// policy only ever inspects algorithms the client supplied).
func declaredSet(expect storage.BlobRef) map[string]bool {
	m := map[string]bool{}
	if expect.Sha256 != "" {
		m["sha256"] = true
	}
	if expect.Sha1 != "" {
		m["sha1"] = true
	}
	if expect.Md5 != "" {
		m["md5"] = true
	}
	return m
}

// uploadContext marks that an ItemCreated body is being rendered for an
// upload that just happened, carrying which algorithms the client declared.
// A non-nil zero-algorithm context means "upload with no declared digests"
// (originalChecksums renders empty, T-13 review m1); a nil context means
// "no upload context at all" (downloads, storage-info renders), where the
// stored triple is the best echo available.
type uploadContext struct{ declared map[string]bool }

// productPrefix is the instance context path every self-referential URL
// carries: ADR-0008's single product namespace /binflow, the same wire
// constant httpapi routes the content plane on (the adapter itself sees the
// path stripped, so the prefix lives here as a render-time fact). T-564's
// A-face probe (Artifactory 7.161.26 behind the /artifactory context root)
// pinned the 201 Location header and the envelope uri/downloadUri all
// rendered THROUGH the context path, Location byte-equal to the uri; the
// bare-root form 404s when followed on B's own routing. Same render rule
// as adapter/maven's T-561/T-563 productPrefix — kept as this package's
// own constant, no cross-package import.
const productPrefix = "/binflow"

// writeCreated renders the 201 response: Location, X-Checksum-Sha256 header
// and the FileInfo/FolderInfo-shaped ItemCreated body (rest-api.md 1.2).
// Both the Location header and the body's uri/downloadUri carry the
// /binflow prefix, Location byte-equal to the uri (T-564 A-face probe).
func (h *Handler) writeCreated(w http.ResponseWriter, r *http.Request, repoKey, relPath string, node *metadata.Node, up uploadContext) {
	sums := h.digestsOf(r.Context(), node)
	w.Header().Set("Location", requestBase(r)+productPrefix+"/"+repoKey+"/"+escapePath(relPath))
	if sums.sha256 != "" && !isFolderNode(node) {
		w.Header().Set(hdrChecksumSha256, sums.sha256)
	}
	w.Header().Set("Content-Type", contentTypeFileInfo)
	w.WriteHeader(http.StatusCreated)
	body := h.itemInfo(requestBase(r), repoKey, relPath, node, sums, up)
	writeJSON(w, body)
}

// isFolderNode reports whether the node is a folder marker: the storage
// layer's shared empty-content sentinel must never leak onto the protocol
// surface (T-13 review m4) — Artifactory FolderInfo carries no checksums.
func isFolderNode(n *metadata.Node) bool {
	return n != nil && strings.HasSuffix(n.Path, "/")
}

// ---- GET / HEAD ----

// handleGet streams the artifact body (GET) or just the metadata headers
// (HEAD) per rest-api.md section 1.4: checksum headers, ETag = sha1
// (unquoted), Last-Modified, Accept-Ranges, Content-Type. Range and
// conditional requests (FR-4-AC14/AC15) are honored on both verbs: a HEAD
// answers 206/304/416 exactly like a GET, minus the body.
func (h *Handler) handleGet(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, relPath string) {
	rc, node, err := h.svc.Get(ctx, p, repoKey, relPath)
	if err != nil {
		if errors.Is(err, repo.ErrIsFolder) {
			// Folder nodes have no body; Artifactory's content path answers
			// folder GETs through /api/storage, so on the raw path a plain
			// 404-shaped "not a file" is the honest answer.
			writeError(w, http.StatusNotFound, notFoundMessage(repoKey, relPath))
			return
		}
		h.writeServiceError(w, err, r.Method, repoKey, relPath)
		return
	}
	defer rc.Close() //nolint:errcheck // read-only fd

	// Service-level engines may attach response hints to the body stream —
	// the remote proxy's X-BinFlow-Cache / X-Binflow-Upstream-Error (T-66).
	// The probe is structural: this handler never imports the engine or
	// learns the repository class (architecture section 5.4).
	if extra, ok := rc.(interface{ ExtraHeaders() http.Header }); ok {
		hdrHint := w.Header()
		for k, vv := range extra.ExtraHeaders() {
			for _, v := range vv {
				hdrHint.Add(k, v)
			}
		}
	}

	sums := h.digestsOf(ctx, node)
	lastMod := parseRFC3339(node.UpdatedAt)
	if lastMod.IsZero() {
		lastMod = parseRFC3339(node.CreatedAt)
	}

	hdr := w.Header()
	if sums.sha256 != "" {
		hdr.Set(hdrChecksumSha256, sums.sha256)
	}
	if sums.sha1 != "" {
		hdr.Set(hdrChecksumSha1, sums.sha1)
	}
	if sums.md5 != "" {
		hdr.Set(hdrChecksumMd5, sums.md5)
	}
	if sums.sha1 != "" {
		hdr.Set("ETag", sums.sha1) // unquoted sha1, rest-api.md 1.4
	}
	if !lastMod.IsZero() {
		hdr.Set("Last-Modified", lastMod.UTC().Format(http.TimeFormat))
	}
	hdr.Set("Accept-Ranges", "bytes")
	// Render-time ownership (BIN-53 / T-571): the table lookup answers for
	// the path; the stored mime column takes no part, so rows stored under
	// the old model (or by another adapter's constants) re-render per the
	// table exactly like fresh deploys.
	hdr.Set("Content-Type", mimeByPath(relPath))

	// Conditional requests first: a fresh store answers 304 with no body.
	if evalConditional(r, sums.sha1, lastMod) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// Then Range: one satisfiable byte range slices the stream (206); an
	// unsatisfiable/malformed spec is a 416; anything BinFlow does not
	// implement (multi-range, other units) is ignored and serves the full
	// 200 body (FR-4-AC14: never 5xx).
	parser := httpRangeParser{total: node.Size}
	rng, malformed, ignore := parser.parseRange(r.Header.Get("Range"))
	switch {
	case malformed:
		hdr.Set("Content-Range", "bytes */"+strconv.FormatInt(node.Size, 10))
		hdr.Del("Content-Length") // an unsatisfiable range has no body length
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	case !ignore && rng.length() > 0:
		if _, err := rc.Seek(rng.start, io.SeekStart); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("seek blob for range: %v", err))
			return
		}
		hdr.Set("Content-Range", rng.contentRange(node.Size))
		hdr.Set("Content-Length", strconv.FormatInt(rng.length(), 10))
		w.WriteHeader(http.StatusPartialContent)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.CopyN(w, rc, rng.length())
		return
	}

	// Full 200 body.
	hdr.Set("Content-Length", strconv.FormatInt(node.Size, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, rc) // client aborts surface as short writes, not errors
}

// ---- DELETE ----

// handleDelete implements the idempotent delete: success is 204 with no
// body, a repeat delete is the same 404 as any unknown path (rest-api.md
// 1.1, repo-semantics section 4).
func (h *Handler) handleDelete(ctx context.Context, w http.ResponseWriter, p *repo.Principal, repoKey, relPath string) {
	if suffix := pathSuffix(relPath); suffix == ":properties" || suffix == ":statistics" {
		writeError(w, http.StatusConflict, "Old metadata notation is not supported anymore")
		return
	}
	if err := h.svc.Delete(ctx, p, repoKey, relPath); err != nil {
		h.writeServiceError(w, err, http.MethodDelete, repoKey, relPath)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// notFoundMessage is the download-side 404 wording (rest-api.md section
// 1.4, high confidence) shared by the missing-file and folder-GET branches
// so every GET/HEAD 404 reads the same. A path carrying the archive
// member marker '!' (the no-slash browsing spelling) answers the archive
// family's miss instead (L024-11 / diff T3: File-not-found + the colon
// Path tail, live on generic and maven alike).
func notFoundMessage(repoKey, relPath string) string {
	if strings.Contains(relPath, "!") {
		return fmt.Sprintf("File not found.; Path: '%s:%s'", repoKey, relPath)
	}
	return fmt.Sprintf("Failed to find the requested resource '%s/%s'.", repoKey, relPath)
}

// ---- shared helpers ----

// writeServiceError maps repo.Service sentinels onto protocol statuses with
// the errors[] envelope. Checksum mismatches carry the spec's received/
// actual wording (repo-semantics section 5, client-checksums policy). The
// not-found wording is verb-specific (T-13 review M1): GET/HEAD use the
// download-side message of rest-api.md section 1.4, DELETE keeps the
// undeploy wording of repo-semantics section 4.
//
// A *repo.StatusError renders VERBATIM first (T-66): repository-class
// semantics — the remote engine's RE-04 fault matrix, RE-05's read-only
// 405 — stay entirely in the service layer while their exact client
// rendering still reaches the wire. The case is class-agnostic: any service
// arm may speak it (T-71's virtual 405 will reuse it), so this handler
// never learns what a "remote" repository is (architecture section 5.4).
func (h *Handler) writeServiceError(w http.ResponseWriter, err error, method, repoKey, relPath string) {
	var se *repo.StatusError
	if errors.As(err, &se) {
		for k, vv := range se.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		writeError(w, se.Code, se.Message)
		return
	}
	switch {
	case errors.Is(err, storage.ErrChecksumMismatch):
		writeError(w, http.StatusConflict, checksumMismatchMessage(err, repoKey, relPath))
	case errors.Is(err, repo.ErrNodeNotFound):
		if method == http.MethodGet || method == http.MethodHead {
			writeError(w, http.StatusNotFound, notFoundMessage(repoKey, relPath))
			return
		}
		writeError(w, http.StatusNotFound, fmt.Sprintf("Could not locate artifact. Path: '%s/%s'.", repoKey, relPath))
	case errors.Is(err, repo.ErrRepoNotFound):
		writeError(w, http.StatusNotFound, fmt.Sprintf("Failed to find the repository '%s' specified in the request.", repoKey))
	case errors.Is(err, repo.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrInvalidProperties):
		// Deploy properties rejected by the closed rules (defensive: the
		// matrix parse refuses them before the service — the service-side
		// guard is the no-bypass backstop, T-286).
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
		writeError(w, http.StatusUnauthorized, "Authentication is required to deploy artifacts.")
	case errors.Is(err, repo.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, repo.ErrRepoTypeNotSupported):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrIsFolder):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// checksumMismatchMessage reshapes the storage error ("sha256 received X,
// actual Y") into the client-checksums policy wording. Storage's message
// carries both values; parse them out rather than string-building a second
// source of truth.
func checksumMismatchMessage(err error, repoKey, relPath string) string {
	msg := err.Error()
	received, actual := "", ""
	for _, algo := range []string{"sha256", "sha1", "md5"} {
		marker := algo + " received "
		if i := strings.Index(msg, marker); i >= 0 {
			rest := msg[i+len(marker):]
			if j := strings.Index(rest, ","); j >= 0 {
				received, actual = rest[:j], strings.TrimPrefix(rest[j+1:], " actual ")
				break
			}
		}
	}
	if received == "" || actual == "" {
		return fmt.Sprintf("Checksum error for '%s/%s': %v", repoKey, relPath, err)
	}
	return fmt.Sprintf("Checksum error for '%s/%s': received '%s' but actual is '%s'",
		repoKey, relPath, received, actual)
}

// digestsOf resolves the node's three digests. sha256 lives on the node;
// sha1/md5 are ledger facts keyed by the blob (ADR-0006: no sidecar). A
// ledger miss degrades to sha256-only — the download keeps serving while
// the consistency tooling notices the gap.
func (h *Handler) digestsOf(ctx context.Context, node *metadata.Node) digestTriple {
	t := digestTriple{sha256: node.Sha256}
	if h.md == nil || node.Sha256 == "" {
		return t
	}
	b, err := h.md.Get(ctx, node.Sha256)
	if err != nil || b == nil {
		return t
	}
	t.sha1, t.md5 = b.Sha1, b.Md5
	return t
}

type digestTriple struct{ sha256, sha1, md5 string }

// pathSuffix returns the ":name" suffix of the final path segment, if any.
func pathSuffix(rel string) string {
	if i := strings.LastIndexByte(rel, ':'); i >= 0 {
		return rel[i:]
	}
	return ""
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

// mimeByPath (see mime.go) is the storage-plane mime authority: the
// extension table answers for the path at render time, unknown extensions
// still fall through to application/octet-stream (FR-4-AC13 unchanged),
// and neither the declared Content-Type nor the stored mime column has a
// vote (BIN-53 / T-571).

// headerBool parses an optional boolean-ish header; ok=false means absent.
func headerBool(hdr http.Header, name string) (val, ok bool) {
	v := strings.TrimSpace(hdr.Get(name))
	if v == "" {
		return false, false
	}
	switch strings.ToLower(v) {
	case "true", "1", "yes":
		return true, true
	case "false", "0", "no":
		return false, true
	}
	return false, false
}

// requestBase is scheme://host from the request (Location header base).
func requestBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// escapePath percent-encodes the path for the Location header.
func escapePath(rel string) string {
	segs := strings.Split(rel, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// parseRFC3339 parses an RFC3339 timestamp, returning zero on failure.
func parseRFC3339(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
