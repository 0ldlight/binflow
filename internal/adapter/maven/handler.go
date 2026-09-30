package maven

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// Protocol-owned header names (the M1 content-plane contract, rest-api.md
// section 1.3) — maven inherits the exact set so wagon/mvn see one product.
const (
	hdrChecksumSha256 = "X-Checksum-Sha256"
	hdrChecksumSha1   = "X-Checksum-Sha1"
	hdrChecksumMd5    = "X-Checksum-Md5"
	hdrChecksumDeploy = "X-Checksum-Deploy"
	hdrExplodeArchive = "X-Explode-Archive"
)

// contentTypeItemCreated is the ItemCreated body Content-Type of a
// successful artifact deploy (rest-api.md section 1.2).
const contentTypeItemCreated = "application/vnd.org.jfrog.artifactory.storage.ItemCreated+json; charset=UTF-8"

// sidecarContentType serves the computed checksum body of a sidecar GET
// (.sha1/.md5/.sha256 map to the checksum media type, config-formats.md
// section 3).
const sidecarContentType = "application/x-checksum"

// maxMetadataBuffer bounds the in-memory buffering of client metadata
// uploads (maven-metadata.xml PUTs are a few KB in practice); beyond it the
// body streams without the idempotent-retransmit shortcut.
const maxMetadataBuffer = 1 << 20

// Protocol implements adapter.Handler.
func (h *Handler) Protocol() string { return Protocol }

// RepoTypes implements adapter.Handler: M3's maven serves local
// repositories directly; remote is the pull-through engine's (T-66) and
// virtual the two-bucket resolver's (T-71) — the adapter's transfer plane
// passes those classes through repo.Service and only adds the two
// maven-specific intercepts (sidecar 404 on remote, method gates).
func (h *Handler) RepoTypes() []string {
	return []string{repo.TypeLocal, repo.TypeRemote, repo.TypeVirtual}
}

// Layout implements adapter.Handler via the shared content-path parser;
// the maven-specific structure validation runs inside ServeHTTP so every
// verb sees the same classification (and the same 400s, M20).
func (h *Handler) Layout(r *http.Request) (string, string, error) {
	return adapter.Layout(r)
}

// ServeHTTP dispatches the four content verbs. Middleware (auth, error
// envelope) is httpapi's; this handler owns the maven semantics and
// renders every failure as the errors[] JSON envelope itself (the three
// protocol adapters share that body shape, maven-npm-pypi.md section 0).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repoKey, relPath, props, err := adapter.ResolveContent(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The Maven indexer namespace is a permanent non-goal (PRD section
	// 2.2): answer the honest E-01 404 before the layout parser would
	// reject the same path with a 400 (ME-10's status is the contract).
	if seg, _, _ := strings.Cut(relPath, "/"); seg == ".index" {
		writeError(w, http.StatusNotFound,
			".index is not implemented in BinFlow (the Maven repository index is a documented non-goal)")
		return
	}
	// The filename-keyed metadata checksum route (T-595 / BIN-77, WP1)
	// dispatches BEFORE the layout gate: the family is keyed on the
	// terminal base name alone, so the repository ROOT spelling — which the
	// maven layout refuses — routes exactly like the module/version ones
	// (live leg wp1-root: A answers the family's 200 no-op at root level
	// too). Put through the dedicated no-op arm; the GET miss of the same
	// family stays a read-plane 404 citing the stripped SOURCE (the
	// parse-gate arm below).
	if r.Method == http.MethodPut {
		if _, ok := metadataChecksumRouteKey(relPath); ok {
			ctx := adapter.WithDeployProps(r.Context(), props)
			h.putMetadataChecksumFile(ctx, w, r, adapter.PrincipalFrom(ctx), repoKey, relPath)
			return
		}
	}
	l, err := Parse(relPath)
	if err != nil {
		// BIN-66 / T-584 (L039 Arm 4 + the whitelist #8 and non-GAV corner
		// probes): a terminal .sha512 path is an ORDINARY file the layout
		// gate never adjudicates — the GAV spelling parses as a plain
		// artifact below (its extension is sha512); this arm carries the
		// un-GAV-able spelling through the same verbs any stored file
		// answers (PUT deploys it, GET/HEAD/DELETE address the stored
		// item — A: 201/200-bytes/204/404-after-delete). The suffix match
		// is the family's own lowercase-exact convention (case folding is
		// the open maven successor face, T-583 Risks).
		if file, ok := terminalSha512File(relPath); ok {
			switch r.Method {
			case http.MethodPut:
				ctx := adapter.WithDeployProps(r.Context(), props)
				h.putSha512ChecksumFile(ctx, w, r, adapter.PrincipalFrom(ctx), repoKey, relPath)
				return
			case http.MethodGet, http.MethodHead, http.MethodDelete:
				l, err = Layout{Kind: KindArtifact, File: file}, nil
			}
		}
	}
	if err != nil {
		// T-562 (BIN-44, contract maven/non-snapshot-spelling-get-gate-404,
		// the L035 t8 live finding): a GET/HEAD of a file name the layout
		// cannot parse is a routing-layer honest miss on the reference —
		// 404, never a 400 layout-parse refusal; the read plane serves
		// paths, it does not adjudicate spellings. The parse gate stays a
		// WRITE-side refusal (A's t8-shaped PUT face is unobserved — kept
		// as-is, not guessed).
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			// T-595 (live leg wp2-root-sha1): the metadata checksum
			// family's miss cites the stripped SOURCE even at a depth the
			// layout cannot parse (`File not found.; Path: '<repo>:
			// maven-metadata.xml'` on A) — the family is filename-keyed on
			// both planes, so the miss never cites the suffix spelling.
			if src, ok := metadataChecksumRouteKey(relPath); ok {
				writeError(w, http.StatusNotFound, notFoundMessage(repoKey, src))
				return
			}
			writeError(w, http.StatusNotFound, notFoundMessage(repoKey, relPath))
			return
		}
		// T-574 / BIN-56 (maven/checksum-put-404-wording, routing order):
		// terminal-checksum routing OUTRANKS the layout gate — a PUT
		// ending in .sha1/.md5/.sha256 is a client-checksum write on the
		// stripped source regardless of GAV shape, so an un-GAV-able tail
		// whose source is missing answers the checksum family's own 404,
		// never the 400 layout refusal (L037 Arm 1: A's suffix routing
		// runs before any layout adjudication). Scoped to LOCAL
		// repositories: the lookup must not send the remote engine
		// fetching on a write path, and non-local checksum PUTs keep
		// their pre-T-574 answer. A live source at a non-layout path is
		// unreachable through this plane's own PUTs — it keeps the layout
		// refusal.
		if r.Method == http.MethodPut {
			if src, cok := checksumPutTarget(relPath); cok {
				if row, rerr := h.class.Get(r.Context(), repoKey); rerr == nil && row.Type == repo.TypeLocal {
					rc, _, gerr := h.svc.Get(r.Context(), adapter.PrincipalFrom(r.Context()), repoKey, src)
					if gerr == nil {
						_ = rc.Close() //nolint:errcheck // read-only existence probe
					} else if errors.Is(gerr, repo.ErrNodeNotFound) || errors.Is(gerr, repo.ErrIsFolder) {
						_, _ = io.Copy(io.Discard, r.Body)
						writeError(w, http.StatusNotFound,
							fmt.Sprintf("Target file to set checksum on doesn't exist: %s:%s", repoKey, src))
						return
					}
				}
			}
		}
		// C2 interim: the layout model is settled (high confidence), the
		// refusal code is not in the spec — 400 is BinFlow's ruling.
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// The matrix-parameter peel lands here like on the generic plane
	// (T-286, architecture section 15.3.1): the ";k=v" set boxed into the
	// context reaches putFile through repo.PutOptions.Properties; reads
	// and deletes address the stripped path only.
	ctx := adapter.WithDeployProps(r.Context(), props)
	p := adapter.PrincipalFrom(ctx)
	switch r.Method {
	case http.MethodPut:
		h.handlePut(ctx, w, r, p, repoKey, relPath, l)
	case http.MethodGet, http.MethodHead:
		h.handleGet(ctx, w, r, p, repoKey, relPath, l)
	case http.MethodDelete:
		h.handleDelete(ctx, w, p, repoKey, relPath, l)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT, DELETE")
		writeError(w, http.StatusMethodNotAllowed,
			fmt.Sprintf("method %s is not supported on maven content paths", r.Method))
	}
}

// ---- GET / HEAD ----

// handleGet serves downloads with the inherited M1 header set (X-Checksum-*,
// ETag=sha1, Last-Modified, Accept-Ranges, Content-Type; Range 206/416 and
// conditional 304 — ME-02), plus the maven-specific reads: the computed
// checksum sidecar body (on a remote plane, back-sourced through the
// ordinary pull-through since T-597) and the virtual-repository metadata
// merge (T-72: maven-metadata.xml and its sidecars answer from the
// in-memory merge of the members' documents, never a single member's
// first-hit copy).
func (h *Handler) handleGet(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, relPath string, l Layout) {
	var rowType string
	var rowCfg RepoConfig
	if row, err := h.class.Get(ctx, repoKey); err == nil {
		rowType = row.Type
		rowCfg = ParseRepoConfig(row.Config)
	}
	if rowType == repo.TypeVirtual {
		if h.serveVirtualMetadata(ctx, w, r, repoKey, relPath, l) {
			return
		}
	}
	// The member GET class gate (T-559 / BIN-41, contract
	// maven/handle-policy-member-get-class-gate — the L033 live finding's
	// "路径类×策略是成员面读写双门"): a LOCAL repository's READ path runs the
	// same path-class × handle* check the deploy gate runs, and it runs
	// BEFORE the existence lookup — an unlanded conflicting-class path
	// direct-read answers 409 with the wording family's GET form, never a
	// plain 404. Scope mirrors the write gate's taxonomy: artifacts and
	// their checksum sidecars (a refused artifact's companions refuse too),
	// metadata documents exempt (ME-06); the VIRTUAL face keeps its
	// walk-layer skip semantics (§3.6 — skip, never a 409 pass-through)
	// and remote rows carry no handle* seats.
	if rowType == repo.TypeLocal && l.Kind != KindMetadata {
		snapshotPath := l.Snapshot || l.Timestamped
		if (snapshotPath && !rowCfg.AcceptsSnapshot()) || (!snapshotPath && !rowCfg.AcceptsRelease()) {
			writeError(w, http.StatusConflict, handlePolicyConflictGETMessage(repoKey, relPath))
			return
		}
	}
	// T-562 (BIN-44, contract maven/plain-snapshot-unique-walk-resolve):
	// the plain-SNAPSHOT walk resolve — a plain-spelling artifact
	// GET/HEAD (or its checksum sidecar) that misses storage answers the
	// selected timestamped candidate of the same (artifact, baseRev,
	// classifier, extension) family; the virtual face picks across members
	// by storage mtime (contract maven/virtual-plain-walk-cross-member-
	// selection). Runs AFTER the class gate (a policy 409 outranks any
	// resolution) and BEFORE the sidecar/file planes, which render the
	// no-candidate miss as the ordinary 404 (t9: no cross-extension
	// fallback).
	if h.servePlainWalk(ctx, w, r, p, repoKey, relPath, l, rowType) {
		return
	}
	// T-542 (BIN-16, L030 case 1's member control leg): a LOCAL repository
	// serves its own SNAPSHOT version document with <snapshotVersions>
	// stripped to a client the M3 capability predicate rejects — the member
	// plane of the same §5.1 rider the virtual merge applies above.
	if rowType == repo.TypeLocal && l.Kind == KindMetadata && l.File == metadataFileName &&
		isSnapshotLevelMetadata(l) && !clientSupportsM3SnapshotVersions(r.UserAgent()) &&
		h.serveSnapshotMetadataStripped(ctx, w, r, p, repoKey, relPath) {
		return
	}
	if l.Kind == KindSidecar {
		// T-597 / BIN-79 (ledger generic/remote-deploy-refusal-form arm d,
		// R12 ruling): a REMOTE repository's sidecar GET is NO LONGER the
		// a-priori 404 "Checksums are not downloadable." — the live
		// reference (7.161.26, /tmp/t597 probe, generic and maven faces
		// alike) back-sources the SOURCE through the ordinary remote chain
		// and externalizes the upstream fault against the suffix-stripped
		// source path; the request falls through to serveSidecar below,
		// whose svc.Get(l.Target) is exactly that pull-through (the landed
		// copy answers its computed digest; maven-npm-pypi.md §1.5 Erratum
		// E1). The 200 form (reachable upstream) is live-unprobed — NOT_RUN,
		// extrapolated via the error form's source resolution.
		// T-542 review follow-up: the member plane's strip reaches the
		// sidecar face too — a capability-rejected client's .sha1/.md5
		// answers the digest of the STRIPPED document (the derived-body
		// contract writeDerivedMetadata upholds), or the same GET pair
		// contradicts itself. sha512 stays on serveSidecar's 404 (no
		// honest computed body under the three-digest model); a nil body
		// falls back to the verbatim plane below.
		if rowType == repo.TypeLocal && l.TargetKind == KindMetadata && l.Algo != "sha512" &&
			!clientSupportsM3SnapshotVersions(r.UserAgent()) {
			if tl, perr := Parse(l.Target); perr == nil && tl.Kind == KindMetadata &&
				tl.File == metadataFileName && isSnapshotLevelMetadata(tl) {
				if body, lastMod := h.strippedSnapshotMetadata(ctx, p, repoKey, l.Target); body != nil {
					h.writeDerivedSidecar(w, r, body, l.Algo, lastMod)
					return
				}
			}
		}
		// The client-value overlay (T-578 / BIN-60, ADR-0052 decision 4):
		// a LOCAL repository's ARTIFACT sidecar echoes the stored client
		// declaration first — the PUT face registered the value above,
		// wrong values included (L037 Arm 1's 409 write-through leg). Under
		// the server-generated-checksums policy the overlay stays OFF (BIN-66 /
		// T-584, L039 Arm 6): the registration happens there too, but the
		// reference's GET face keeps serving the COMPUTED digest whatever
		// was declared — the stored client value surfaces only in
		// originalChecksums. The VIRTUAL face overlays too (T-587 / BIN-69,
		// L040 Arm 1b mv2-get-md5: A echoes the member's registered client
		// value through the virtual read plane — the generic plane's
		// serveVirtualClientChecksum mirror). Since BIN-76 / T-594 (ledger
		// maven/sidecar-get-ondemand-matrix, L041 Arm 1 + T-587's
		// mvu-get-*-unset legs) the unset-value fallback on the
		// overlay-armed face is sha256-ONLY: an unset md5/sha1 answers the
		// checksum family's own 404 citing the source (`Checksum not found
		// for <src>`, serveSidecarOfPath) — A computes no md5/sha1 on
		// demand. Metadata targets (the derived-document contract owns
		// their digests) and the remote plane keep the computed answer.
		h.serveSidecar(ctx, w, r, p, repoKey, rowType, l,
			l.TargetKind == KindArtifact && (rowType == repo.TypeVirtual ||
				(rowType == repo.TypeLocal && rowCfg.ChecksumPolicy != ChecksumPolicyServerGenerated)))
		return
	}
	h.serveFile(ctx, w, r, p, repoKey, relPath)
}

// serveSidecar answers a checksum sidecar GET/HEAD with the STORED CLIENT
// digest of the TARGET when the overlay flag holds and one was registered,
// else the on-demand matrix of serveSidecarOfPath (sha256 computed, md5/sha1
// the checksum family's 404) — never a passthrough of stored sidecar bytes
// (the stored bytes only register the client's original claim, ME-03).
func (h *Handler) serveSidecar(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, rowType string, l Layout, overlayClient bool) {
	h.serveSidecarOfPath(ctx, w, r, p, repoKey, l.Target, l.Algo, rowType, overlayClient)
}

// writeSidecarDigest renders the computed sidecar of ONE node (digest
// lookup, headers, conditional, body) — shared by the ordinary sidecar
// face and the walk resolve's sidecar leg (t5/t6: the digest addresses the
// RESOLVED entity). The R7 sha512 gate that used to live here is DELETED
// (Review B NB-③, T-587): since BIN-66 / T-584 checksumSuffixes carries
// only {.sha256,.sha1,.md5}, so Parse — the sole Algo producer — can never
// hand this exit a sha512, and every path there (a terminal .sha512 GET)
// now parses as an ordinary artifact file whose miss the transfer plane
// 404s on its own; the sha512 leg's observable contract (404, never a 500
// ledger gap) stays pinned by TestPlainSnapshotWalkSha512SidecarGate.
func (h *Handler) writeSidecarDigest(ctx context.Context, w http.ResponseWriter, r *http.Request,
	node *metadata.Node, algo, repoKey, path string) {
	digest, ok := h.digestOf(ctx, node, algo)
	if !ok {
		writeError(w, http.StatusInternalServerError,
			fmt.Sprintf("digest %s of '%s/%s' is not available (ledger gap)", algo, repoKey, path))
		return
	}
	h.writeSidecarBody(w, r, digest, node) // bare hex, no trailing newline (ME-03/FR-16)
}

// writeSidecarBody renders one sidecar BODY value (stored client
// declaration or computed digest — the byte rendering is identical) under
// the sidecar face's VERB-CONDITIONAL header model (T-598 / BIN-80, ledger
// maven/sidecar-get-x-checksum-sha256-echo, live A 7.161.26 legs
// m1-get-sha1-set / m1-head-sha1-set, L041 Arm 1 — the rendering path is
// the VERB, not the repository state or the algorithm):
//
//   - GET renders the bare infra set ONLY — Content-Type, Content-Length,
//     Last-Modified. No ETag, no X-Checksum-*, no Accept-Ranges: A answers
//     nothing a client could validate the sidecar bytes against. An
//     If-None-Match therefore never short-circuits (no served ETag to
//     match — writeDerivedSidecar's inert-etag habit); If-Modified-Since
//     still earns its 304 off the rendered stamp.
//   - HEAD renders the full validator set — Accept-Ranges, ETag and
//     X-Checksum-{Md5,Sha1,Sha256} — and every validator addresses the
//     SOURCE artifact (the node), never the sidecar's own digest: ETag is
//     the unquoted source sha1 and the triple is the source's computed
//     triple (serveNode's digestTriple), a ledger gap degrading per key.
//
// The derived-metadata sidecar (writeDerivedSidecar) is a different
// contract family (L032 Arm 6 / BIN-41) and keeps its own face.
func (h *Handler) writeSidecarBody(w http.ResponseWriter, r *http.Request, body string, node *metadata.Node) {
	hdr := w.Header()
	hdr.Set("Content-Type", sidecarContentType)
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	lastMod := nodeTime(node)
	hdr.Set("Last-Modified", lastMod.UTC().Format(http.TimeFormat))
	etag := ""
	if r.Method == http.MethodHead {
		sums := h.digestTriple(r.Context(), node)
		hdr.Set("Accept-Ranges", "bytes")
		if sums.sha256 != "" {
			hdr.Set(hdrChecksumSha256, sums.sha256)
		}
		if sums.sha1 != "" {
			hdr.Set(hdrChecksumSha1, sums.sha1)
			hdr.Set("ETag", sums.sha1) // unquoted source sha1, m1-head-sha1-set
			etag = sums.sha1
		}
		if sums.md5 != "" {
			hdr.Set(hdrChecksumMd5, sums.md5)
		}
	}
	if evalConditional(r, etag, lastMod) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, body) // bare hex, no trailing newline (ME-03/FR-16)
}

// serveFile streams an artifact or stored metadata node with the full M1
// download contract.
func (h *Handler) serveFile(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, relPath string) {
	rc, node, err := h.svc.Get(ctx, p, repoKey, relPath)
	if err != nil {
		if errors.Is(err, repo.ErrIsFolder) {
			writeError(w, http.StatusNotFound, notFoundMessage(repoKey, relPath))
			return
		}
		h.writeServiceError(w, err, r.Method, repoKey, relPath)
		return
	}
	defer rc.Close() //nolint:errcheck // read-only fd
	h.serveNode(w, r, rc, node, relPath)
}

// serveNode renders one OPENED node with the full M1 download contract
// (digest headers, ETag=sha1, conditional 304, Range 206/416). The walk
// resolve serves its target through this same exit the direct spelling
// uses — the W3 same-face ruling: one serving path, and every validator,
// slice and byte addresses the SERVED entity (t2/t4), so the plain and
// timestamped spellings of one artifact can never disagree.
func (h *Handler) serveNode(w http.ResponseWriter, r *http.Request,
	rc io.ReadSeekCloser, node *metadata.Node, relPath string) {
	// Service-level engines may attach response hints to the body stream —
	// a remote member's X-BinFlow-Cache / X-Binflow-Upstream-Error (T-66), a
	// virtual resolution's X-BinFlow-Resolved-From on top of those (T-71).
	// The probe is structural: this handler never imports the engine or
	// learns the repository class (architecture section 5.4).
	applyReaderHints(w, rc)

	sums := h.digestTriple(r.Context(), node)
	lastMod := nodeTime(node)

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
	// Render-time ownership (BIN-53 / T-571): the extension table answers
	// for the served path; the stored mime column takes no part.
	hdr.Set("Content-Type", mimeForPath(relPath))

	if evalConditional(r, sums.sha1, lastMod) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	parser := httpRangeParser{total: node.Size}
	rng, malformed, ignore := parser.parseRange(r.Header.Get("Range"))
	switch {
	case malformed:
		hdr.Set("Content-Range", "bytes */"+strconv.FormatInt(node.Size, 10))
		hdr.Del("Content-Length")
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
	hdr.Set("Content-Length", strconv.FormatInt(node.Size, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, rc) // client aborts surface as short writes, not errors
}

// ---- DELETE ----

// handleDelete is the M1 idempotent delete (204; repeat delete the same
// 404 as any unknown path) plus ME-07's asynchronous recalculation of the
// affected directory tree: an artifact delete touches its version
// directory and the module version list, a metadata delete the document's
// own directory (which the recalculation regenerates while the facts
// warrant one).
func (h *Handler) handleDelete(ctx context.Context, w http.ResponseWriter,
	p *repo.Principal, repoKey, relPath string, l Layout) {
	if err := h.svc.Delete(ctx, p, repoKey, relPath); err != nil {
		h.writeServiceError(w, err, http.MethodDelete, repoKey, relPath)
		return
	}
	// Only a LOCAL repository's facts drive recalculation: a remote
	// delete dropped a cache copy (its metadata is upstream's business)
	// and a virtual delete never reached here (RE-08's 405).
	if row, err := h.class.Get(ctx, repoKey); err == nil && row.Type == repo.TypeLocal {
		h.calc.afterDelete(p, repoKey, l)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- error mapping ----

// writeServiceError maps repo.Service sentinels onto the maven protocol
// statuses. It mirrors the generic adapter's table (the transfer plane is
// shared) and adds the class refusals: PUT on a remote repository is the
// RE-05 405, PUT on an un-routed virtual repository the Q2/C5 405 with the
// errata's fixed wording.
//
// A *repo.StatusError renders VERBATIM first (T-82, the generic adapter's
// T-66 seam): the repository-class engines — the remote pull-through's RE-04
// fault matrix and RE-05 read-only 405, the virtual resolver's RE-08 delete
// refusal and C5 write refusal — own their exact client rendering in the
// service layer while this handler stays class-agnostic (architecture
// section 5.4). Before this branch the DELETE-on-virtual refusal fell into
// the ErrRepoTypeNotSupported arm's non-PUT 400 shape instead of its 405.
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
	case errors.Is(err, repo.ErrIsFolder):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, repo.ErrRepoTypeNotSupported):
		// A non-local repository on a WRITE path: the class refusals the
		// remote/virtual engines own (RE-05/Q2). Since T-66/T-71 those
		// refusals arrive as *repo.StatusError and render verbatim in the
		// branch above; this arm is the fallback for plain sentinel wraps
		// (the engines being unwired, docker-class refusals leaking through)
		// and keeps the 405 shape rather than pre-empting svc.Put, so a
		// service-side write ROUTE (T-71's defaultDeploymentRepo) passes
		// through untouched — the adapter only shapes the refusal.
		if method != http.MethodPut && method != http.MethodPost {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.Header().Set("Allow", "GET, HEAD")
		if h.repoClassIsVirtual(repoKey) {
			writeError(w, http.StatusMethodNotAllowed, unroutedVirtualWriteMessage(repoKey))
			return
		}
		writeError(w, http.StatusMethodNotAllowed, fmt.Sprintf(
			"Cannot deploy '%s/%s': remote repositories are read-only pull-through caches.", repoKey, relPath))
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// unroutedVirtualWriteMessage is the deploy-routing refusal's A form (the
// C5 405, byte-identical to the service's refuseVirtualWrite — repo/
// virtual.go msgNoDeploymentRepo — and to the 7.161.26 wire, L040 Arm 1a):
// the sidecar intercept's routing gate (T-587 / BIN-69) and the fallback
// StatusError arm above render the one spelling.
func unroutedVirtualWriteMessage(repoKey string) string {
	return fmt.Sprintf(
		"No local repository was configured as local deployment repository for the (%s) virtual repository.", repoKey)
}

// repoClassIsVirtual resolves the repository class via the anonymous seam
// (the PUT already carries an authenticated principal, but the refusal
// wording choice is routing data, which is exactly what ClassReader is
// for; a lookup failure falls back to the remote wording).
func (h *Handler) repoClassIsVirtual(repoKey string) bool {
	row, err := h.class.Get(context.Background(), repoKey)
	return err == nil && row.Type == repo.TypeVirtual
}

// checksumMismatchMessage reshapes the storage error ("sha256 received X,
// actual Y") into the client-checksums wording (repo-semantics section 5),
// the same reshape the generic adapter performs so both adapters answer a
// mismatch byte-identically.
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

// notFoundMessage is the download-side 404 wording (L020 wire f3a-c, the
// A-form spelling verbatim): "File not found.; " with the reference's own
// double punctuation, and the repository and path colon-separated inside
// ONE quoted segment. Breadth is the wire's finding — the same shape for
// a ghost file in an existing directory, a ghost directory tree and a
// ghost metadata document.
func notFoundMessage(repoKey, relPath string) string {
	return fmt.Sprintf("File not found.; Path: '%s:%s'", repoKey, relPath)
}

// handlePolicyConflictMessage renders the handle* policy refusal's 409
// wording, A-form verbatim (contract
// maven/handle-policy-reject-409-wording-family; live 7.161.26 wire,
// run/l033-r5-r{3,4} put_rel_to_hr_body / put_snap_to_hs_body): one
// template for BOTH legs — release-into-handleReleases=false and
// snapshot-into-handleSnapshots=false answer byte-identically, the policy
// domain always reads "snapshot release handling policy" (no per-leg
// phrase), and "resolution" is the A-side's own word even on the PUT
// deploy leg.
func handlePolicyConflictMessage(repoKey, relPath string) string {
	return fmt.Sprintf(
		"The repository '%s' rejected the resolution of an artifact '%s:%s' due to conflict in the snapshot release handling policy.",
		repoKey, repoKey, relPath)
}

// handlePolicyConflictGETMessage renders the read-leg form: the same 409
// message with the "; Path: '…'" segment appended (wire anchor:
// run/l033-r5-r{3,4} direct_get_{relpath_hr,snappath_hs}_body — the fill
// is the same '<repoKey>:<path>' string as the message's inner artifact
// reference, confirmed byte-identical up to the harness's 300-char
// capture ceiling; the leg's status face is the companion contract
// maven/handle-policy-member-get-class-gate).
func handlePolicyConflictGETMessage(repoKey, relPath string) string {
	return handlePolicyConflictMessage(repoKey, relPath) +
		fmt.Sprintf("; Path: '%s:%s'", repoKey, relPath)
}

// applyReaderHints copies a body stream's structural response hints onto the
// response (the generic adapter's T-66 seam, restated locally because adapter
// packages share no unexported code — the area rule). A plain local blob
// carries none, so the probe is a no-op for the M1 paths.
func applyReaderHints(w http.ResponseWriter, body io.ReadSeekCloser) {
	if extra, ok := body.(interface{ ExtraHeaders() http.Header }); ok {
		for k, vv := range extra.ExtraHeaders() {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
	}
}

// ---- digest helpers ----

type digestTriple struct{ sha256, sha1, md5 string }

// digestTriple resolves a node's three digests; a ledger miss degrades to
// sha256-only (the download keeps serving, the same posture as generic).
func (h *Handler) digestTriple(ctx context.Context, node *metadata.Node) digestTriple {
	t := digestTriple{sha256: node.Sha256}
	if h.ledger == nil || node.Sha256 == "" {
		return t
	}
	b, err := h.ledger.Get(ctx, node.Sha256)
	if err != nil || b == nil {
		return t
	}
	t.sha1, t.md5 = b.Sha1, b.Md5
	return t
}

// digestOf resolves one named digest ("sha256"/"sha1"/"md5") of a node.
func (h *Handler) digestOf(ctx context.Context, node *metadata.Node, algo string) (string, bool) {
	switch algo {
	case "sha256":
		return node.Sha256, node.Sha256 != ""
	case "sha1", "md5":
		t := h.digestTriple(ctx, node)
		if algo == "sha1" {
			return t.sha1, t.sha1 != ""
		}
		return t.md5, t.md5 != ""
	}
	return "", false
}

// nodeTime parses a node timestamp, zero on failure.
func nodeTime(n *metadata.Node) time.Time {
	for _, s := range []string{n.UpdatedAt, n.CreatedAt} {
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
