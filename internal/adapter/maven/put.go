package maven

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// handlePut implements the maven deploy chain (ME-01). Ordering follows
// repo-semantics section 2 (validate path/policy before permissions before
// body):
//
//  1. layout (already parsed in ServeHTTP),
//  2. repository resolution + class refusal (remote 405 / virtual 405 via
//     the service),
//  3. handleReleases/handleSnapshots policy (409, ME-08),
//  4. the checksum policy chain: X-Checksum-* headers and sidecar bodies
//     per checksumPolicyType (ME-09), server-computed acceptance when the
//     client declared nothing.
func (h *Handler) handlePut(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, relPath string, l Layout) {
	if v := r.Header.Get(hdrExplodeArchive); v != "" && !strings.EqualFold(v, "false") {
		writeError(w, http.StatusBadRequest, "X-Explode-Archive is not supported in BinFlow")
		return
	}
	// Checksum deploy (zero-transfer, T-73): detected here, SERVED after the
	// policy gates below — it is an artifact deploy like any other, only
	// without the bytes.
	checksumDeploy := false
	if v := r.Header.Get(hdrChecksumDeploy); v != "" && !strings.EqualFold(v, "false") {
		checksumDeploy = true
	}

	row, err := h.svc.GetRepo(ctx, p, repoKey)
	if err != nil {
		h.writeServiceError(w, err, http.MethodPut, repoKey, relPath)
		return
	}
	// Review L014-2 B2: a VIRTUAL repository with a configured write route
	// lands in its local deployment member (repo-semantics 8.2 / T-71) —
	// the maven policy chain and the snapshot arithmetic consult the MEMBER
	// (its behavior config, its storage facts); a virtual key holds no
	// nodes, so consulting it would mint a fresh (ts, N) on every PUT and
	// accumulate same-buildNumber files forever. The PUT itself still
	// addresses the ORIGINAL key (the service routes; the audit and the
	// Location render stay the client's spelling). An unrouted virtual
	// keeps its row here — the rewrite then declines, and the service's
	// own C5 405 answers the doomed write.
	factsKey := repoKey
	if row.Type == repo.TypeVirtual {
		if member := routeTargetOf(row.Config); member != "" {
			if mrow, merr := h.class.Get(ctx, member); merr == nil {
				factsKey, row = member, mrow
			}
		}
	}
	cfg := ParseRepoConfig(row.Config)
	if row.Type != repo.TypeLocal {
		// The rewrite declines for every non-local chain: a remote PUT
		// refuses in the service and an unrouted virtual's C5 405 lands
		// nothing — minting a name against a key with no storage facts is
		// the B2 accumulation bug, so the behavior reads as non-unique here
		// regardless of the (leniently parsed) blob.
		cfg.SnapshotBehavior = BehaviorNonUnique
	}

	// Release/snapshot handling gates (ME-08): they classify the DEPLOY's
	// version type from the version directory, and they bind artifact
	// uploads plus their checksum sidecars — the sidecar of a refused
	// artifact must not land. Metadata documents are bookkeeping, not a
	// deploy of a version: the gate does not apply (ME-06 keeps client
	// metadata PUTs acceptable). A checksum deploy of an artifact is bound
	// by the same gates: zero bytes is still a deploy of that version.
	// The 409 body is the A-form wording family (T-559 / BIN-41, contract
	// maven/handle-policy-reject-409-wording-family): one template for both
	// legs, replacing the pre-T-559 parameter-style message.
	if l.Kind != KindMetadata {
		snapshotDeploy := l.Snapshot || l.Timestamped
		if snapshotDeploy && !cfg.AcceptsSnapshot() {
			writeError(w, http.StatusConflict, handlePolicyConflictMessage(repoKey, relPath))
			return
		}
		if !snapshotDeploy && !cfg.AcceptsRelease() {
			writeError(w, http.StatusConflict, handlePolicyConflictMessage(repoKey, relPath))
			return
		}
	}

	if checksumDeploy {
		h.putChecksumDeploy(ctx, w, r, p, repoKey, relPath, l)
		return
	}

	if l.Kind == KindSidecar {
		// T-587 / BIN-69 (ledger maven/checksum-put-unrouted-virtual-plane,
		// L040 Arm 1a mv-unrouted-*): the sidecar intercept is a WRITE and
		// passes the deploy routing gate BEFORE its svc.Get read probe — a
		// virtual row still standing after the facts resolution above (no
		// defaultDeploymentRepo, or a route the resolution declined) answers
		// the routing refusal's A form, exactly like the plain PUT face: the
		// terminal checksum suffix buys no exemption (A: every write on the
		// unrouted virtual 405s, sidecar included, zero side effects), and
		// the existence probe must never run against a key with no deploy
		// target. A remote row here is the bare-mount defense (the mounted
		// chain answers maven remote PUTs in httpapi's upload engine; a
		// drifted virtual route lands here too): it keeps the plain PUT
		// face's own RE-05 shape and never turns the probe into a remote
		// pull-through on a write verb.
		if row.Type != repo.TypeLocal {
			w.Header().Set("Allow", http.MethodGet)
			if row.Type == repo.TypeVirtual {
				writeError(w, http.StatusMethodNotAllowed, unroutedVirtualWriteMessage(row.RepoKey))
				return
			}
			writeError(w, http.StatusMethodNotAllowed, fmt.Sprintf(
				"Remote repository '%s' is a read-only proxy cache; deployments to remote repositories are not accepted.", row.RepoKey))
			return
		}
		h.putSidecar(ctx, w, r, p, repoKey, factsKey, relPath, l, cfg)
		return
	}
	// The A-form acceptance of the calculator-owned SNAPSHOT version
	// document (L020 wire f1, V6; the L013-4 C1 arm splits the levels):
	// a client PUT of `X/.../<v>-SNAPSHOT/maven-metadata.xml` is
	// 202-accepted and the body DISCARDED — no storage node, no
	// recalculation, no envelope. The module (version-group) document's
	// PUT keeps the store chain (A replays those bytes verbatim there —
	// the R-21 pending face, unadjudicated). Needs the calculator wired
	// (the nil-seam assembly keeps the T-67 verbatim-storage behavior)
	// and fires only where the write would land in a LOCAL repository
	// (row is the routed member under a virtual key): remote and
	// un-routed virtual PUTs fall through and keep the 405 refusals.
	if l.Kind == KindMetadata && l.File == metadataFileName && strings.HasSuffix(l.Module, snapshotSuffix) &&
		h.calc != nil && row.Type == repo.TypeLocal {
		h.acceptDiscardedMetadata(ctx, w, r, p, factsKey, relPath)
		return
	}
	h.putFile(ctx, w, r, p, repoKey, factsKey, relPath, l, cfg)
}

// acceptDiscardedMetadata renders the 202-discard acceptance. The 401/403
// pair mirrors the store chain's own renderings byte for byte
// (writeServiceError's ErrUnauthorized/ErrForbidden arms): the acceptance
// must not become an authorization bypass on a path that no longer runs
// the service gates — the Authorizer seam (nil = the assembly wired none,
// the npm WithAuth convention) answers the write-grant question against
// the FACTS repository, the one the discarded Put would have gated on.
// The body is drained so the connection stays reusable, then dropped.
func (h *Handler) acceptDiscardedMetadata(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, factsKey, relPath string) {
	if p == nil {
		w.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
		writeError(w, http.StatusUnauthorized, "Authentication is required to deploy artifacts.")
		return
	}
	if h.authz != nil && !h.authz.Can(ctx, p, factsKey, relPath, repo.ActionWrite) {
		writeError(w, http.StatusForbidden,
			fmt.Errorf("write %s/%s: %w", factsKey, relPath, repo.ErrForbidden).Error())
		return
	}
	_, _ = io.Copy(io.Discard, r.Body)
	w.WriteHeader(http.StatusAccepted)
}

// routeTargetOf is the maven-side thin alias of the adapter base's
// single-source write-route probe (T-590 hoist; the semantics and their
// golden live in internal/adapter/deploytarget.go): the first non-empty of
// defaultDeploymentRepo / defaultDeploymentRepoRef / deploymentRepository,
// restating repo's unexported virtualRouteTarget seam — the strict
// agreement rules live at config time (validateVirtualMembers), and a
// drifted target surfaces through the service's own target re-load,
// identically to the seam's contract.
func routeTargetOf(config string) string {
	return adapter.VirtualDeploymentTarget(config)
}

// putChecksumDeploy implements X-Checksum-Deploy on the maven plane (T-73,
// PRD §6.4-1): a zero-transfer artifact deploy addressed by sha256 — or by
// sha1 alone, the maven ecosystem's dominant algorithm (the ledger's sha1
// index resolves it inside the service). The header shapes and the miss
// rendering follow the generic plane's M1 C15 family: no checksum header →
// 400, malformed value → 404, miss → 404 "no content found". Artifacts only:
// sidecars are registration data with their own chain, metadata documents
// are regenerated bookkeeping — checksum-deploying either is refused rather
// than landing a node those chains would never have written.
func (h *Handler) putChecksumDeploy(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, relPath string, l Layout) {
	if l.Kind != KindArtifact {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("X-Checksum-Deploy supports artifact paths only on maven repositories; %q is a %s path",
				relPath, l.Kind))
		return
	}
	sha256 := strings.ToLower(strings.TrimSpace(r.Header.Get(hdrChecksumSha256)))
	sha1v := strings.ToLower(strings.TrimSpace(r.Header.Get(hdrChecksumSha1)))
	md5v := strings.ToLower(strings.TrimSpace(r.Header.Get(hdrChecksumMd5)))
	if sha256 == "" && sha1v == "" {
		writeError(w, http.StatusBadRequest,
			"Checksum deploy failed. no checksum header 'X-Checksum-Sha1/X-Checksum-Sha256' was found.")
		return
	}
	if sha256 != "" && !isHex(sha256, 64) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Checksum deploy failed: malformed sha256 value %q.", sha256))
		return
	}
	if sha1v != "" && !isHex(sha1v, 40) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Checksum deploy failed: malformed sha1 value %q.", sha1v))
		return
	}
	if md5v != "" && !isHex(md5v, 32) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Checksum deploy failed: malformed md5 value %q.", md5v))
		return
	}

	// The path's extension owns the stored mime (BIN-53 / T-571): the
	// factory-table lookup runs for every deploy and the declared
	// Content-Type header takes no part in it (the 7.161.26 18-leg matrix
	// answered every explicit declaration from the table).
	mime := mimeForPath(relPath)
	ref := storage.BlobRef{Sha256: sha256, Sha1: sha1v, Md5: md5v}
	node, err := h.svc.PutFromBlob(ctx, p, repoKey, relPath, ref, mime)
	if err != nil {
		if errors.Is(err, repo.ErrOrphanBlob) || errors.Is(err, repo.ErrNodeNotFound) {
			writeError(w, http.StatusNotFound, "Checksum deploy failed: no content found for the given checksum.")
			return
		}
		h.writeServiceError(w, err, http.MethodPut, repoKey, relPath)
		return
	}
	// A landed artifact feeds FR-17's metadata calculator exactly like a
	// byte-carrying deploy (the facts and the metadata belong to where the
	// bytes — here the referenced blob — live).
	h.calc.afterArtifactDeploy(ctx, p, node.RepoKey, l)
	h.writeCreated(w, r, repoKey, relPath, node, declaredSetOf(ref))
}

// putFile lands an artifact or a client maven-metadata.xml document
// (ME-06: metadata PUTs ride the generic upload chain and are accepted).
// repoKey is the ADDRESSED key (PUT, Location, audit); factsKey is the
// repository the maven chain consults (the routed member under a virtual
// key — equal to repoKey everywhere else).
func (h *Handler) putFile(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, factsKey, relPath string, l Layout, cfg RepoConfig) {
	var body io.Reader = r.Body
	// T-543 (D-4, L030 case 2): the pom-coordinates-vs-path consistency
	// gate — a .pom deploy whose content GAV disagrees with the deployment
	// path is 409-refused BEFORE anything lands (the reference's message
	// verbatim, captured live on 7.161.26), unless the landing repository
	// sets suppressPomConsistencyChecks (default false). The pom is
	// buffered for the parse and replayed into the store chain below; a
	// body past the ceiling streams on unchecked (pom documents are KBs —
	// a >4MiB "pom" is not a parse anyone trusts) and an unparseable or
	// coordinate-incomplete one deploys unchecked (the evidence pins only
	// the mismatch refusal — refusing anything else would be guessing).
	if l.Kind == KindArtifact && strings.HasSuffix(l.File, ".pom") && !cfg.SuppressPomConsistencyChecks {
		buf, rerr := io.ReadAll(io.LimitReader(r.Body, maxPomConsistencyBytes+1))
		if rerr != nil {
			writeError(w, http.StatusBadRequest, "read pom body: "+rerr.Error())
			return
		}
		if int64(len(buf)) > maxPomConsistencyBytes {
			slog.WarnContext(ctx, "maven: pom body past the consistency-check ceiling — deploying unchecked",
				slog.String("repo", repoKey), slog.String("path", relPath),
				slog.Int64("bytes", int64(len(buf))))
			body = io.MultiReader(bytes.NewReader(buf), r.Body)
		} else {
			if msg, refuse := pomPathMismatch(relPath, l, buf); refuse {
				writeError(w, http.StatusConflict, msg)
				return
			}
			body = bytes.NewReader(buf)
		}
	}
	// L014-2 BUG 1: under snapshotVersionBehavior=unique a -SNAPSHOT file
	// name is rewritten to the timestamped spelling BEFORE the bytes land
	// (the 201 Location, the storage node and the calculator trigger all
	// address the rewritten name; the version directory keeps -SNAPSHOT).
	if l.Kind == KindArtifact && cfg.SnapshotBehavior == BehaviorUnique && l.Snapshot && !l.Timestamped {
		if name := h.calc.adjustUniqueSnapshot(ctx, factsKey, l); name != l.File {
			relPath = relPath[:strings.LastIndexByte(relPath, '/')+1] + name
			l.File, l.Timestamped = name, true
		}
	}
	// Same ownership rule as the checksum-deploy chain above: the extension
	// table answers, the declared header has no vote — the relPath here is
	// the post-unique-rewrite spelling, the same node the GET face serves.
	mime := mimeForPath(relPath)

	declared, err := declaredDigests(r.Header)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// server-generated-checksums: the client's disagreeing claims are
	// accepted silently and the measured values are authoritative
	// (repo-semantics section 5) — dropping them here means storage
	// computes and accepts, and the response echoes the measured set.
	if cfg.ChecksumPolicy == ChecksumPolicyServerGenerated {
		declared = storage.BlobRef{}
	}

	// Client metadata re-PUTs are routine (every mvn deploy refreshes the
	// artifact-level document), and a metadata re-send with IDENTICAL
	// bytes must never trip anything: buffering the (small) body to pass
	// its own sha256 makes the service treat that case as the idempotent
	// retransmit its overwrite chain already exempts. Bodies beyond the
	// buffer ceiling stream unbuffered (no shortcut, still stored).
	if l.Kind == KindMetadata {
		buf, rerr := io.ReadAll(io.LimitReader(r.Body, maxMetadataBuffer+1))
		if rerr != nil {
			writeError(w, http.StatusBadRequest, "read metadata body: "+rerr.Error())
			return
		}
		if int64(len(buf)) > maxMetadataBuffer {
			body = io.MultiReader(bytes.NewReader(buf), r.Body)
		} else {
			body = bytes.NewReader(buf)
			if declared.Sha256 == "" && len(buf) > 0 {
				declared.Sha256 = sha256Hex(buf)
			}
		}
	}

	// The metadata family never triggers the overwrite check
	// (repo-semantics section 3, high confidence): sidecar and metadata
	// documents are freely rewritable — every mvn deploy re-PUTs them with
	// changed bytes, which must not demand DELETE on the old node (the
	// T-67 leftover this closes). The write grant still applies.
	//
	// Deploy matrix properties (T-286) ride the same options either way:
	// the ";k=v" set ServeHTTP boxed off the path lands with the node.
	opts := repo.PutOptions{Properties: deployPropsOf(ctx)}
	if l.Kind == KindMetadata {
		opts.SkipOverwriteCheck = true
	}
	node, err := h.svc.PutWithOptions(ctx, p, repoKey, relPath, body, declared, mime, opts)
	if err != nil {
		h.writeServiceError(w, err, http.MethodPut, repoKey, relPath)
		return
	}

	// FR-17's trigger taxonomy fires on the LANDED node's repository (a
	// routed virtual write lands in the deployment member — the facts and
	// the metadata belong to where the bytes are). The metadata arm is the
	// module (version-group) document, the plugin-group variant and the
	// nil-seam assembly: the SNAPSHOT version document never reaches here
	// (the 202-discard acceptance above).
	if l.Kind == KindArtifact {
		h.calc.afterArtifactDeploy(ctx, p, node.RepoKey, l)
	} else {
		h.calc.afterMetadataDeploy(ctx, p, node.RepoKey, l)
	}

	declaredSet := map[string]bool{}
	if cfg.ChecksumPolicy != ChecksumPolicyServerGenerated {
		declaredSet = declaredSetOf(declared)
	}
	h.writeCreated(w, r, repoKey, relPath, node, declaredSet)
}

// putSidecar implements the checksum-file upload chain (rest-api.md
// section 1.5, high confidence): the sidecar body is the client's declared
// digest of the TARGET artifact; the >1024B guard, the target-must-exist
// 404 and the two-policy comparison all precede the registration. L014-2
// BUG 2: a checksum-file PUT is REGISTRATION ONLY — no sidecar storage
// item materializes (the reference's post-deploy listing shows pom, jar
// and maven-metadata.xml only; BinFlow's phantom .sha1/.md5 nodes were the
// E2 divergence). The 201 carries Location = the TARGET artifact and no
// body. Since the ADR-0052 seam (T-578 / BIN-60) the registration is the
// client-checksum WRITE itself on an artifact target — SET first, 201/409
// rendered after, and the 409 arm writes through too (L037 Arm 1); GET of
// the sidecar path then echoes the stored client value before the
// computed one (overlay in serveSidecarOfPath).
//
// factsKey is the DEPLOY PLANE the whole family executes on (T-587 /
// BIN-69, L040 Arm 1b mv2-*): the addressed local repository verbatim, or
// a routed virtual's deployment-target member — the existence probe, the
// SET and every rendered reference (the miss 404's repo segment, the 201
// Location) name that member, A's intercept-penetrates-virtual model.
func (h *Handler) putSidecar(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, factsKey, relPath string, l Layout, cfg RepoConfig) {
	// Suspicious-size guard first: a checksum file is a digest plus
	// whitespace; Content-Length beyond the ceiling answers without
	// reading the body, an oversized chunked body at the read.
	if r.ContentLength > maxSidecarBytes {
		writeError(w, http.StatusConflict, suspiciousSidecarMessage(r.ContentLength))
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxSidecarBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read checksum file: "+err.Error())
		return
	}
	if int64(len(raw)) > maxSidecarBytes {
		writeError(w, http.StatusConflict, suspiciousSidecarMessage(int64(len(raw))))
		return
	}
	declared := strings.TrimSpace(string(raw)) // trailing newline tolerated (FR-16)

	// Under unique behavior the target of a -SNAPSHOT sidecar is the
	// REWRITTEN artifact name (the checksum registers against the file
	// that actually landed, spec section 1.3's companion-follows rule).
	if cfg.SnapshotBehavior == BehaviorUnique && l.TargetKind == KindArtifact &&
		l.Snapshot && !l.Timestamped {
		if tl, terr := Parse(l.Target); terr == nil {
			if name := h.calc.adjustCompanionTarget(ctx, factsKey, tl); name != tl.File {
				l.Target = l.Target[:strings.LastIndexByte(l.Target, '/')+1] + name
			}
		}
	}

	// The target must exist: a checksum for nothing registers nothing.
	// The lookup consults the FACTS repository — under a virtual key a
	// content GET would read the aggregate face, not the write target.
	rc, node, err := h.svc.Get(ctx, p, factsKey, l.Target)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) || errors.Is(err, repo.ErrIsFolder) {
			// T-574 / BIN-56 (maven/checksum-put-404-wording): the
			// {.sha1,.md5,.sha256} family answers the checksum family's
			// own miss wording — 7.161.26 verbatim, colon-separated
			// repo:target, domain-agnostic (L037 Arm 1 pinned it on the
			// generic and maven legs alike). The sidecar plane only
			// carries that family since BIN-66 / T-584 (.sha512 is an
			// ordinary file face routed before the layout parse). Since
			// T-587 the repo segment names the DEPLOY PLANE (factsKey):
			// a routed virtual's miss cites the member key — the
			// intercept penetrates the virtual (L040 Arm 1b mv2-sidecar-
			// miss, A verbatim).
			writeError(w, http.StatusNotFound,
				fmt.Sprintf("Target file to set checksum on doesn't exist: %s:%s", factsKey, l.Target))
			return
		}
		h.writeServiceError(w, err, http.MethodPut, repoKey, relPath)
		return
	}
	_ = rc.Close() //nolint:errcheck // read-only fd; only the node metadata is needed

	// The policy comparison stays the artifact-plane gate: a disagreeing
	// claim on a client-checksums repository is a 409. The metadata family
	// keeps its tolerance — its target is server-authoritative and
	// regenerated at any deploy (FR-17), so a client value checksummed
	// against pre-recalculation bytes may legitimately disagree already.
	measured, measuredOK := h.digestOf(ctx, node, l.Algo)
	mismatch := measuredOK && measured != declared
	refuse := mismatch && cfg.ChecksumPolicy == ChecksumPolicyClient && l.TargetKind != KindMetadata

	// Registration FIRST (ADR-0052 decisions 3.3/6.1, T-578 / BIN-60): on
	// the deploy plane the ARTIFACT sidecar registers through the
	// persistence seam before any outcome renders — the correct value and
	// the refused one write through alike (L037 Arm 1's 409 leg:
	// originalChecksums keeps the client's value, wrong or not), a re-PUT
	// overwrites per algorithm, and a seam failure is an honest 500 rather
	// than a pretend 201/409 over an unregistered value. Since BIN-66 /
	// T-584 (L039 Arm 6, ledger maven/checksum-oc-write-under-srvgen-policy)
	// the registration is POLICY-INDEPENDENT: the reference decouples
	// recording from verification — the declared value lands in
	// originalChecksums under server-generated-checksums too (the client
	// column), while only the COMPARISON above stays the policy gate (its
	// srvgen leg skips the 409 and renders 201). Metadata targets keep the
	// tolerance no-op (their sidecar family is the C5 open face); the
	// non-local planes never reach here since T-587 (the deploy routing
	// gate in handlePut refuses them before the read probe, L040 Arm 1).
	if l.TargetKind == KindArtifact {
		var ref storage.BlobRef
		switch l.Algo {
		case "sha256":
			ref.Sha256 = declared
		case "sha1":
			ref.Sha1 = declared
		case "md5":
			ref.Md5 = declared
		}
		if ref != (storage.BlobRef{}) {
			seam := h.clientChecksumSeam()
			if seam == nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf(
					"client-checksum persistence is not available on this assembly (%s/%s)", factsKey, l.Target))
				return
			}
			if err := seam.SetClientChecksums(ctx, p, factsKey, l.Target, ref); err != nil {
				h.writeServiceError(w, err, http.MethodPut, repoKey, relPath)
				return
			}
		}
	}

	if refuse {
		// T-574 / BIN-56 (maven/checksum-put-404-wording, the 409 half):
		// the reference's path quotes the PUT TARGET (suffix included)
		// WITHOUT the repository prefix — L037 Arm 1's live wording. The
		// value itself already wrote through above (ledger
		// maven/checksum-put-409-write-through, closed by T-578).
		writeError(w, http.StatusConflict, fmt.Sprintf(
			"Checksum error for '%s': received '%s' but actual is '%s'",
			relPath, declared, measured))
		return
	}

	// Registration only (L014-2 BUG 2): the 201 carries the TARGET's
	// Location and no body (rest-api.md section 1.1's dedicated column for
	// the checksum-file PUT). L034-R6 Arm 8's cksum leg pins the A face
	// rendering the Location THROUGH the contextPath. Since T-587 the
	// Location names the DEPLOY PLANE (factsKey) — a routed virtual's 201
	// addresses the member's source artifact, A's Location-to-member face
	// (L040 Arm 1b mv2-sidecar-ok; the generic plane's C1 model, T-583).
	w.Header().Set("Location", requestBase(r)+productPrefix+"/"+factsKey+"/"+escapePath(l.Target))
	w.WriteHeader(http.StatusCreated)
}

// maxSidecarBytes is the checksum-file size ceiling (rest-api.md 1.5).
const maxSidecarBytes = 1024

// putSha512ChecksumFile deploys a terminal-.sha512 PUT on an un-GAV-able
// path as an ORDINARY file (BIN-66 / T-584, L039 Arm 4 + whitelist #8, the
// C4 ledger model): the reference keeps .sha512 outside the client-checksum
// family AND outside the layout gate — GAV and non-GAV spellings deploy as
// plain storage items (201 ItemCreated envelope, octet-stream mime, GET
// serving the bytes verbatim). The GAV spelling reaches putFile through the
// ordinary KindArtifact parse (its pom gate never fires — not a .pom — and
// the metadata calculator treats it like any file in a version directory);
// this arm carries the spelling the parser refuses, so no pom-consistency
// gate, no unique-snapshot rewrite and no calculator trigger apply (no GAV
// to key any of them on). Class refusals stay the service's own, exactly
// like any ordinary deploy (remote 405 / unrouted virtual 405).
func (h *Handler) putSha512ChecksumFile(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, relPath string) {
	// No .sha512 table entry: the extension falls through to octet-stream,
	// the reference envelope's own mime (L039 Arm 4).
	mime := mimeForPath(relPath)
	declared, err := declaredDigests(r.Header)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	node, err := h.svc.PutWithOptions(ctx, p, repoKey, relPath, r.Body, declared, mime,
		repo.PutOptions{Properties: deployPropsOf(ctx)})
	if err != nil {
		h.writeServiceError(w, err, http.MethodPut, repoKey, relPath)
		return
	}
	h.writeCreated(w, r, repoKey, relPath, node, declaredSetOf(declared))
}

// clientChecksumPutSuffixes is the client-checksum PUT interception family
// (T-574 / BIN-56, L037 Arm 1): a terminal .sha1/.md5/.sha256 routes the
// PUT as a client-checksum write on the stripped source. .sha512 is NOT
// family (BIN-66 / T-584, L039 Arm 4): it deploys as an ORDINARY file —
// the GAV spelling parses as a plain artifact, the un-GAV-able spelling
// rides the dedicated arm in ServeHTTP.
var clientChecksumPutSuffixes = []string{".sha1", ".md5", ".sha256"}

// clientChecksumSeam resolves the service's client-checksum persistence
// capability (ADR-0052 decision 1); nil when the assembled service predates
// the seam (a bare test double — every real assembly wires the concrete
// service).
func (h *Handler) clientChecksumSeam() repo.ClientChecksumWriter {
	seam, _ := h.svc.(repo.ClientChecksumWriter)
	return seam
}

// clientChecksumValueOf projects one algorithm's stored client declaration
// off a node row — the overlay half of repo.OriginalChecksums with the
// computed triple left empty (the fallback posture is the CALLER's: the
// direct sidecar face falls through to the computed digest when this
// returns "", per ADR-0052 decision 4's protocol-owned fallbacks).
func clientChecksumValueOf(node *metadata.Node, algo string) string {
	o256, o1, o5 := repo.OriginalChecksums(node, "", "", "")
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

// checksumPutTarget strips the family suffix off a PUT path, reporting
// the source artifact path the client-checksum write addresses.
func checksumPutTarget(relPath string) (src string, ok bool) {
	file := relPath[strings.LastIndexByte(relPath, '/')+1:]
	for _, sfx := range clientChecksumPutSuffixes {
		if strings.HasSuffix(file, sfx) && len(file) > len(sfx) {
			return strings.TrimSuffix(relPath, sfx), true
		}
	}
	return "", false
}

// terminalSha512File reports the final path segment when relPath TERMINATES
// in the .sha512 suffix (lowercase-exact, the interception family's
// convention; BIN-66 / T-584): such a path is an ORDINARY file the maven
// families never claim — outside the client-checksum interception family,
// outside the sidecar plane and outside the layout gate.
func terminalSha512File(relPath string) (string, bool) {
	file := relPath[strings.LastIndexByte(relPath, '/')+1:]
	if strings.HasSuffix(file, ".sha512") && len(file) > len(".sha512") {
		return file, true
	}
	return "", false
}

// maxPomConsistencyBytes bounds the pom buffering of the T-543 consistency
// gate (pom documents are KBs; past the ceiling the deploy streams on
// unchecked rather than buffering an unbounded "pom" in memory).
const maxPomConsistencyBytes = 4 << 20 // 4 MiB

// pomPathMismatch parses the pom body's coordinates and compares them with
// the deployment path's GAV (Maven model semantics: groupId/version may be
// inherited from <parent>; artifactId is always the project's own). It
// returns the reference's refusal message (captured on 7.161.26, virtual
// and local legs identical) when the pom's expected path prefix disagrees
// with the addressed one; an unparseable or coordinate-incomplete pom
// returns no refusal — the evidence pins only the mismatch shape.
func pomPathMismatch(relPath string, l Layout, pom []byte) (string, bool) {
	var p struct {
		GroupID    string `xml:"groupId"`
		ArtifactID string `xml:"artifactId"`
		Version    string `xml:"version"`
		Parent     *struct {
			GroupID string `xml:"groupId"`
			Version string `xml:"version"`
		} `xml:"parent"`
	}
	if err := xml.Unmarshal(pom, &p); err != nil {
		return "", false
	}
	if p.GroupID == "" && p.Parent != nil {
		p.GroupID = p.Parent.GroupID
	}
	if p.Version == "" && p.Parent != nil {
		p.Version = p.Parent.Version
	}
	if p.GroupID == "" || p.ArtifactID == "" || p.Version == "" {
		return "", false
	}
	want := strings.ReplaceAll(p.GroupID, ".", "/") + "/" + p.ArtifactID + "/" + p.Version
	got := strings.ReplaceAll(l.OrgPath, ".", "/") + "/" + l.Module + "/" + l.VersionDir
	if want == got {
		return "", false
	}
	return fmt.Sprintf(
		"The target deployment path '%s' does not match the POM's expected path prefix '%s'. "+
			"Please verify your POM content for correctness and make sure the source path is a valid Maven repository root path.",
		relPath, want), true
}

// deployPropsOf lifts the request's matrix-parameter set into the option
// shape (nil for a path without any — the plain-deploy options).
func deployPropsOf(ctx context.Context) map[string][]string {
	if props := adapter.DeployPropsFrom(ctx); len(props) > 0 {
		return map[string][]string(props)
	}
	return nil
}

// suspiciousSidecarMessage renders the fixed refusal wording.
func suspiciousSidecarMessage(n int64) string {
	return fmt.Sprintf("Suspicious checksum file, content length of %d bytes is bigger than allowed.", n)
}

// declaredDigests parses the X-Checksum-* headers into a BlobRef
// (malformed values are 400-shaped, the shared adapter contract).
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

// declaredSetOf remembers which algorithms the client declared.
func declaredSetOf(expect storage.BlobRef) map[string]bool {
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

// isHex reports whether s is exactly n hex characters.
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

// productPrefix is the instance context path every self-referential URL
// carries: ADR-0008's single product namespace /binflow, the same wire
// constant httpapi routes the content plane on (the adapter itself sees the
// path stripped, so the prefix lives here as a render-time fact). The
// deploy envelope's uri/downloadUri (T-561/BIN-42) and the 201 Location
// header (T-563/BIN-45, L034-R6 Arm 8: A renders Location THROUGH its
// contextPath and byte-equal to the envelope uri — the bare-root form 404s
// when followed) address the artifact THROUGH the prefix, the way
// Artifactory's response URLs carry its contextPath.
const productPrefix = "/binflow"

// writeCreated renders the 201 of an artifact/metadata deploy: Location,
// X-Checksum-Sha256 and the FileInfo ItemCreated body (rest-api.md 1.2).
// Both the Location header and the body's uri/downloadUri carry the
// /binflow prefix, Location byte-equal to the uri (L034-R6 Arm 8). The
// addressed key renders (the L014-2 ruling: the service routes a virtual
// write onto its member, the audit and the Location keep the client's
// spelling) — the A wire names the member instead (L040 Arm 1b
// mv2-seed-plain); that plain-deploy envelope face is a standing
// divergence outside the intercept arm's scope (T-587 keeps it and flags
// it), unlike the SIDECAR 201's Location, which the same A wire pins to
// the member (putSidecar below).
func (h *Handler) writeCreated(w http.ResponseWriter, r *http.Request, repoKey, relPath string,
	node *metadata.Node, declared map[string]bool) {
	sums := h.digestTriple(r.Context(), node)
	w.Header().Set("Location", requestBase(r)+productPrefix+"/"+repoKey+"/"+escapePath(relPath))
	if sums.sha256 != "" && !isFolder(node) {
		w.Header().Set(hdrChecksumSha256, sums.sha256)
	}
	w.Header().Set("Content-Type", contentTypeItemCreated)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, h.itemInfo(requestBase(r), repoKey, relPath, node, sums, declared))
}

// isFolder reports a folder-marker node (no checksums exist for one).
func isFolder(n *metadata.Node) bool {
	return n != nil && strings.HasSuffix(n.Path, "/")
}

// itemInfo renders the FileInfo shape (rest-api.md 1.2): size is a string,
// checksums the measured triple, originalChecksums the client-declared
// subset of the upload.
func (h *Handler) itemInfo(base, repoKey, relPath string, node *metadata.Node,
	sums digestTriple, declared map[string]bool) fileInfo {
	self := base + productPrefix + "/" + repoKey + "/" + escapePath(relPath)
	info := fileInfo{
		URI:         self,
		DownloadURI: self,
		Repo:        repoKey,
		Path:        "/" + relPath,
		Created:     node.CreatedAt,
		CreatedBy:   node.CreatedBy,
		Size:        strconv.FormatInt(node.Size, 10),
		MimeType:    mimeForPath(relPath),
	}
	if !isFolder(node) {
		info.Checksums = &checksums{Sha1: sums.sha1, Md5: sums.md5, Sha256: sums.sha256}
		orig := &checksums{}
		if declared["sha1"] {
			orig.Sha1 = sums.sha1
		}
		if declared["md5"] {
			orig.Md5 = sums.md5
		}
		if declared["sha256"] {
			orig.Sha256 = sums.sha256
		}
		info.OriginalChecksums = orig
	}
	if node.UpdatedAt != "" && node.UpdatedAt != node.CreatedAt {
		info.LastModified = node.UpdatedAt
		info.LastUpdated = node.UpdatedAt
		info.ModifiedBy = node.CreatedBy
	}
	return info
}
