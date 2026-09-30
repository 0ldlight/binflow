package generic

import (
	"context"
	"encoding/json"
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

// contentTypeChecksum is the client-checksum GET echo body's type (L037
// Arm 1: the stored-value face answers a bare digest text, not the
// source artifact's bytes).
const contentTypeChecksum = "application/x-checksum"

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

	// T-574 / BIN-56 routed the terminal-checksum PUT family (L037 Arm 1's
	// A model, 7.161.26); T-578 / BIN-60 completes the storage half on the
	// client-checksum persistence seam (ADR-0052): a PUT whose path
	// TERMINATES in .sha1/.md5/.sha256 (case-insensitively, L039 Arm 2) is
	// a client-checksum write on the stripped source artifact — content,
	// source extension and source spelling are all irrelevant to the
	// routing. Source missing → the family's 404 verbatim, nothing
	// registered; source present → the declared value is registered FIRST
	// (SetClientChecksums, the same call for both outcomes), then the
	// comparison renders 201 (empty body, Location = the source artifact)
	// or 409 with the received/actual wording — the reference writes the
	// client value through on the 409 too (L037 Arm 1, write-through leg),
	// so the SET precedes the rendering.
	//
	// The plane (T-583 / BIN-65, L039 Arm 1b): LOCAL runs verbatim; a
	// VIRTUAL repository runs the whole family against its write route's
	// deployment-target member (existence check, SET and rendering all
	// name the member). Everything else falls through to the ordinary
	// chain — a virtual repository without a defaultDeploymentRepo keeps
	// its already-matching 405, and a remote plane keeps RE-05's read-only
	// refusal: the probe is svc.Get, which on a remote plane PULLS THROUGH
	// on a write verb (R9 reviews A+B blocking, ADR-0052 decision 2).
	if src, algo, ok := checksumPutSource(relPath); ok {
		if plane, pok := h.clientChecksumPutPlane(r.Context(), repoKey); pok {
			if h.putClientChecksum(ctx, w, r, p, plane, relPath, src, algo) {
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
	h.writeCreated(w, r, repoKey, relPath, node)
}

// terminalChecksumSuffixes is the client-checksum PUT interception family
// (T-574 / BIN-56, L037 Arm 1): the path's TERMINAL suffix alone routes —
// .sha512, .asc and compound tails like .sha1.bak are ordinary deploys. The
// match is CASE-INSENSITIVE (T-583 / BIN-65, L039 Arm 2): .SHA1/.Md5/.SHA256
// and every mixed spelling route into the family identically, while the
// excluded faces stay excluded in every spelling — only these three keys
// match, folded.
var terminalChecksumSuffixes = []string{".sha1", ".md5", ".sha256"}

// checksumPutSource strips the terminal checksum suffix off a content path,
// reporting the source artifact path the client-checksum operation addresses
// and the suffix's algorithm ("sha1"/"md5"/"sha256"). The returned source
// keeps the client's ORIGINAL spelling (L039 Arm 2: the 404 wording cites
// the stripped source verbatim, uppercase suffix and all). Both the PUT
// (registration) and the GET (stored-value echo) faces route on the same
// triple.
func checksumPutSource(relPath string) (src, algo string, ok bool) {
	file := relPath[strings.LastIndexByte(relPath, '/')+1:]
	lower := strings.ToLower(file)
	for _, sfx := range terminalChecksumSuffixes {
		if strings.HasSuffix(lower, sfx) && len(lower) > len(sfx) {
			return relPath[:len(relPath)-len(sfx)], strings.TrimPrefix(sfx, "."), true
		}
	}
	return "", "", false
}

// clientChecksumPutPlane resolves the repository the terminal-checksum PUT
// family executes on (T-583 / BIN-65, L039 Arm 1b — the C1 model): the
// addressed LOCAL repository verbatim, or — when the addressed repository
// is VIRTUAL — the write route's deployment-target member, against which
// the existence check, the SET and every rendered reference (the miss 404's
// repo segment, the 201 Location) run. ok=false falls through to the
// ordinary deploy chain: a virtual repository without a
// defaultDeploymentRepo keeps its already-matching 405, and a remote plane
// keeps RE-05's read-only refusal — the family never probes on a write
// verb there.
func (h *Handler) clientChecksumPutPlane(ctx context.Context, repoKey string) (string, bool) {
	row, err := h.class.Get(ctx, repoKey)
	if err != nil {
		return "", false
	}
	switch row.Type {
	case repo.TypeLocal:
		return repoKey, true
	case repo.TypeVirtual:
		target := virtualDeploymentTarget(row.Config)
		if target == "" {
			return "", false
		}
		// The write-verb red line (R9 review): a drifted, non-local target
		// must never turn the family's existence probe into a remote
		// pull-through — fall through and let the ordinary chain answer the
		// drift honestly (config-time validation normally makes this
		// unreachable; raw-seeded rows are exactly the drift case).
		if trow, terr := h.class.Get(ctx, target); terr == nil && trow.Type == repo.TypeLocal {
			return target, true
		}
	}
	return "", false
}

// virtualDeploymentTarget is the generic-side thin alias of the adapter
// base's single-source write-route probe (T-590 hoist; the semantics and
// their golden live in internal/adapter/deploytarget.go).
func virtualDeploymentTarget(config string) string {
	return adapter.VirtualDeploymentTarget(config)
}

// checksumPolicySrvgen reports whether the repository's config blob spells
// the server-generated-checksums policy (BIN-66 / T-584, L039 Arm 6 + the
// generic srvgen probe): under it the reference decouples RECORDING from
// VERIFYING — a terminal-checksum PUT still registers the declared value in
// originalChecksums but skips the mismatch comparison (201), and the GET
// face serves the computed digest whatever was declared. Absent, unparseable
// or unknown spellings read as the client-checksums default (the
// virtualDeploymentTarget tolerance convention).
func (h *Handler) checksumPolicySrvgen(ctx context.Context, repoKey string) bool {
	row, err := h.class.Get(ctx, repoKey)
	if err != nil {
		return false
	}
	var probe struct {
		ChecksumPolicyType string `json:"checksumPolicyType"`
	}
	if err := json.Unmarshal([]byte(row.Config), &probe); err != nil {
		return false
	}
	return strings.TrimSpace(probe.ChecksumPolicyType) == "server-generated-checksums"
}

// clientChecksumSeam resolves the service's client-checksum persistence
// capability; nil when the assembled service predates the seam (a bare test
// double — every real assembly wires the concrete service).
func (h *Handler) clientChecksumSeam() repo.ClientChecksumWriter {
	seam, _ := h.svc.(repo.ClientChecksumWriter)
	return seam
}

// clientChecksumValueOf projects one algorithm's overlay value for the
// client-checksum GET face: the stored client declaration when present,
// nothing otherwise — generic's fallback posture is 404, NOT the computed
// digest (Arm 5's addendum: no on-demand generation for an unset checksum),
// which is exactly what passing an empty server triple to the shared
// overlay helper expresses (ADR-0052 decision 4: the mechanism is the
// single-source helper, the fallback posture stays protocol-owned).
func clientChecksumValueOf(node *metadata.Node, algo string) string {
	o256, o1, o5 := repo.OriginalChecksums(node, "")
	switch algo {
	case "sha256":
		return o256
	case "sha1":
		return o1
	case "md5":
		return o5
	}
	return ""
}

// maxSidecarBytes is the checksum-file body ceiling on the client-checksum
// plane (BIN-70 / T-588, L040 Arm 3's 1024/1025 boundary legs — inclusive:
// 1024 passes into the comparison, 1025 is refused). The maven face keeps
// the same ceiling and wording (put.go maxSidecarBytes).
const maxSidecarBytes = 1024

// suspiciousSidecarMessage renders the refusal wording verbatim (N = the
// exact body length in bytes).
func suspiciousSidecarMessage(n int64) string {
	return fmt.Sprintf("Suspicious checksum file, content length of %d bytes is bigger than allowed.", n)
}

// putClientChecksum serves the terminal-checksum PUT on the resolved
// client-checksum plane (clientChecksumPutPlane — the addressed LOCAL
// repository, or a VIRTUAL repository's deployment-target member, L039
// Arm 1b): every action in it, the existence probe, the SET and each
// rendered reference, addresses the passed repoKey. It reports whether the
// request was answered; a lookup error with its own shape (read denial)
// returns false so the caller continues down the ordinary chain — the same
// posture the T-574 interception kept.
func (h *Handler) putClientChecksum(ctx context.Context, w http.ResponseWriter,
	r *http.Request, p *repo.Principal, repoKey, relPath, src, algo string) bool {
	// Suspicious-size guard first (BIN-70 / T-588, L040 Arm 3 N5: the
	// sz-*/szb-* legs pinned the ceiling at 1024 bytes INCLUSIVE and the
	// verbatim refusal — the maven face runs the same seam and wording,
	// put.go maxSidecarBytes): a declared Content-Length beyond the
	// ceiling answers 409 without reading the body; an oversized chunked
	// body is caught at a bounded read (at most 1025 bytes in memory,
	// never the unbounded ReadAll the ledger's B-side recorded).
	if r.ContentLength > maxSidecarBytes {
		writeError(w, http.StatusConflict, suspiciousSidecarMessage(r.ContentLength))
		return true
	}
	rc, node, err := h.svc.Get(ctx, p, repoKey, src)
	if err != nil {
		if !errors.Is(err, repo.ErrNodeNotFound) && !errors.Is(err, repo.ErrIsFolder) {
			return false
		}
		_, _ = io.Copy(io.Discard, r.Body)
		writeError(w, http.StatusNotFound,
			fmt.Sprintf("Target file to set checksum on doesn't exist: %s:%s", repoKey, src))
		return true
	}
	_ = rc.Close() //nolint:errcheck // read-only probe; only the node facts are needed

	raw, rerr := io.ReadAll(io.LimitReader(r.Body, maxSidecarBytes+1))
	if rerr != nil {
		writeError(w, http.StatusBadRequest, "read checksum body: "+rerr.Error())
		return true
	}
	if int64(len(raw)) > maxSidecarBytes {
		writeError(w, http.StatusConflict, suspiciousSidecarMessage(int64(len(raw))))
		return true
	}
	declared := strings.TrimSpace(string(raw)) // trailing whitespace tolerated, the maven family's rule

	// Registration FIRST (ADR-0052 decision 3.3): the write-through and the
	// successful registration are one call point — the 409 arm does not skip
	// the SET, and a SET failure is an honest 500, never a pretend
	// 201/409 over an unregistered value.
	seam := h.clientChecksumSeam()
	if seam == nil {
		writeError(w, http.StatusInternalServerError,
			fmt.Sprintf("client-checksum persistence is not available on this assembly (%s/%s)", repoKey, src))
		return true
	}
	ref := storage.BlobRef{}
	switch algo {
	case "sha256":
		ref.Sha256 = declared
	case "sha1":
		ref.Sha1 = declared
	case "md5":
		ref.Md5 = declared
	}
	if err := seam.SetClientChecksums(ctx, p, repoKey, src, ref); err != nil {
		h.writeServiceError(w, err, http.MethodPut, repoKey, relPath)
		return true
	}

	// The comparison renders the outcome against the server-measured triple
	// and is the POLICY gate (generic's own domain gate, ADR-0052 decision
	// 6.3; BIN-66 / T-584: under server-generated-checksums the reference
	// skips it — the wrong-value PUT renders the same 201 Location-to-source
	// while the registration above still records the declared value).
	if !h.checksumPolicySrvgen(ctx, repoKey) {
		sums := h.digestsOf(ctx, node)
		var measured string
		switch algo {
		case "sha256":
			measured = sums.sha256
		case "sha1":
			measured = sums.sha1
		case "md5":
			measured = sums.md5
		}
		if measured != "" && declared != measured {
			writeError(w, http.StatusConflict, fmt.Sprintf(
				"Checksum error for '%s': received '%s' but actual is '%s'", relPath, declared, measured))
			return true
		}
	}
	// Success is a metadata write, not a file creation: 201 with an EMPTY
	// body and Location addressing the SOURCE artifact (L037 Arm 1: CL=0,
	// Location never the .sha1 path).
	w.Header().Set("Location", requestBase(r)+productPrefix+"/"+repoKey+"/"+escapePath(src))
	w.WriteHeader(http.StatusCreated)
	return true
}

// serveClientChecksum serves the terminal-checksum GET on a LOCAL
// repository: the STORED client value, or the family's 404 when none was
// registered (Arm 5's addendum — the face never generates on demand). The
// miss wording is TWO-STATE (T-583 / BIN-65, L039 Arm 8's P8 refinement):
// a missing source answers the colon-separated "File not found." family
// form addressing the source, while a PRESENT source with no registered
// value answers the bare `Checksum not found for <src>` — no repo prefix,
// no Path structure. GET and HEAD render the same face.
//
// Under the server-generated-checksums policy the face answers the COMPUTED
// digest, registered or not (BIN-66 / T-584, the A probe: a
// registered-but-wrong .md5 and a never-registered .sha1 both answer the
// computed value — the stored client declaration surfaces only in
// originalChecksums).
func (h *Handler) serveClientChecksum(ctx context.Context, w http.ResponseWriter,
	r *http.Request, p *repo.Principal, repoKey, src, algo string) {
	srvgen := h.checksumPolicySrvgen(ctx, repoKey)
	rc, node, err := h.svc.Get(ctx, p, repoKey, src)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) || errors.Is(err, repo.ErrIsFolder) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("File not found.; Path: '%s:%s'", repoKey, src))
			return
		}
		h.writeServiceError(w, err, r.Method, repoKey, src)
		return
	}
	defer rc.Close() //nolint:errcheck // read-only fd; the value comes from the node row

	if srvgen {
		sums := h.digestsOf(ctx, node)
		var measured string
		switch algo {
		case "sha256":
			measured = sums.sha256
		case "sha1":
			measured = sums.sha1
		case "md5":
			measured = sums.md5
		}
		if measured != "" {
			writeChecksumEcho(w, r, measured)
			return
		}
		// A ledger gap degrades to the two-state miss below — the honest
		// 404 beats a fabricated value.
	}
	value := clientChecksumValueOf(node, algo)
	if value == "" {
		writeError(w, http.StatusNotFound, "Checksum not found for "+src)
		return
	}
	writeChecksumEcho(w, r, value)
}

// serveVirtualClientChecksum serves the stored-value half of the
// terminal-checksum GET face on a VIRTUAL plane (T-583 / BIN-65, L039
// Arm 1b): the source resolves through the virtual read plane and a
// STORED client value echoes byte-identically to the local face.
// Everything else reports false so the ordinary chain keeps rendering —
// the on-demand computation for unset algorithms and the
// unresolvable-source wording are the C2 family's open faces and keep
// today's behavior until their own ruling.
func (h *Handler) serveVirtualClientChecksum(ctx context.Context, w http.ResponseWriter,
	r *http.Request, p *repo.Principal, repoKey, src, algo string) bool {
	rc, node, err := h.svc.Get(ctx, p, repoKey, src)
	if err != nil {
		return false
	}
	_ = rc.Close() //nolint:errcheck // read-only probe; only the node facts are needed
	value := clientChecksumValueOf(node, algo)
	if value == "" {
		return false
	}
	writeChecksumEcho(w, r, value)
	return true
}

// writeChecksumEcho renders the client-checksum stored-value body: the
// bare digest text with the family's content type (L037 Arm 1; the virtual
// face's echo is byte-identical, L039 Arm 1b).
func writeChecksumEcho(w http.ResponseWriter, r *http.Request, value string) {
	hdr := w.Header()
	hdr.Set("Content-Type", contentTypeChecksum)
	hdr.Set("Content-Length", strconv.Itoa(len(value)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, value) //nolint:gosec // G705: the checksum echo IS the stored client value (application/x-checksum body, not HTML — an XSS taint has no sink shape here)
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
	h.writeCreated(w, r, repoKey, relPath, node)
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
//
// A deploy addressed at a VIRTUAL repository renders the LANDED repository
// (T-583 / BIN-65, L039 Arm 1b — the C1 render facet): the service's write
// route already landed the node in the deployment-target member, and the
// reference's envelope repo and self-referential URLs name the member.
// node.RepoKey is that landed key, so LOCAL deploys (RepoKey == the
// addressed key) render byte-identically to before.
func (h *Handler) writeCreated(w http.ResponseWriter, r *http.Request, repoKey, relPath string, node *metadata.Node) {
	if node != nil && node.RepoKey != "" {
		repoKey = node.RepoKey
	}
	sums := h.digestsOf(r.Context(), node)
	w.Header().Set("Location", requestBase(r)+productPrefix+"/"+repoKey+"/"+escapePath(relPath))
	if sums.sha256 != "" && !isFolderNode(node) {
		w.Header().Set(hdrChecksumSha256, sums.sha256)
	}
	w.Header().Set("Content-Type", contentTypeFileInfo)
	w.WriteHeader(http.StatusCreated)
	body := h.itemInfo(requestBase(r), repoKey, relPath, node, sums)
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
	// Client-checksum GET face (T-578 / BIN-60, the read half of the seam):
	// on a LOCAL repository a path TERMINATING in .sha1/.md5/.sha256
	// (case-insensitively) answers the STORED client value — the same
	// triple the PUT arm routes on — and the family's two-state 404 when
	// none was registered (Arm 5's addendum: the face never generates on
	// demand; generic's posture has no computed fallback). On a VIRTUAL
	// plane only the stored-value half serves here (T-583 / BIN-65): the
	// echo resolves through the virtual read plane, and every miss keeps
	// the ordinary chain's rendering (the on-demand face is the C2
	// family's own ruling). Remote planes keep the ordinary chain.
	if src, algo, ok := checksumPutSource(relPath); ok {
		if row, rerr := h.class.Get(r.Context(), repoKey); rerr == nil {
			switch row.Type {
			case repo.TypeLocal:
				h.serveClientChecksum(ctx, w, r, p, repoKey, src, algo)
				return
			case repo.TypeVirtual:
				if h.serveVirtualClientChecksum(ctx, w, r, p, repoKey, src, algo) {
					return
				}
			}
		}
	}
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
