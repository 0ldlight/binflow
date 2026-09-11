package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The registry-v2 VIRTUAL plane (M13 T-365, FR-116.2): a virtual
// docker/helmoci repository aggregates its members on the READ side —
//
//   - manifest GET/HEAD by tag or digest: walk the two-bucket member
//     order; a LOCAL member serves its standing copy, a REMOTE member runs
//     the T-363 pull-through conversation in its own right (probe, the
//     upstream session, landing into the MEMBER's cache) — the first
//     member that can produce the body wins (first-seen semantics: member
//     order, not content freshness, decides);
//   - blob GET/HEAD: the same walk over the digest-keyed blob path;
//   - tags/list and _catalog: the service's union listings (ListTags and
//     ListImages walk the members themselves since T-365);
//   - every write verb: the service-rendered 405 + Allow: GET (the C5
//     spelling un-routed; the honest not-on-this-plane wording when a
//     defaultDeploymentRepo IS configured — registry-v2 push-through
//     routing is registered follow-up, not claimed).
//
// The degradation posture is getVirtual's R10 rule in the T-363 dialect:
// a member's RESULT stops the walk (even a STALE one — the expired copy
// with the X-Binflow-Upstream-Error marker), a member's UNFOUND miss
// continues it (negative cache, no copy, upstream 404/401/403 or a
// transport fault without a copy), and a classified NON-unfound failure —
// the SSRF chain's 400, the body ceiling's 502 — propagates verbatim
// (masking a security refusal as a virtual-wide 404 would hide a
// configured behavior). A walk no member answers ends in the unfound
// family with the last upstream summary attached — zero naked 5xx.

// virtualPlane resolves the service's v2 virtual aggregation capability;
// nil when the assembled service predates the seam (a bare test double).
func (h *Handler) virtualPlane() repo.V2VirtualPlane {
	plane, _ := h.svc.(repo.V2VirtualPlane)
	return plane
}

// serveVirtualRoute answers every request the /v2 plane receives against a
// VIRTUAL repository row (T-365): reads walk the member aggregation,
// writes answer the service-rendered 405 with Allow: GET (the route's
// scope gate has already run — an unauthorized writer keeps its 401/403).
func (h *Handler) serveVirtualRoute(w http.ResponseWriter, r *http.Request, ref nameRef) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if plane := h.virtualPlane(); plane != nil {
			if se := plane.V2WriteRefusal(r.Context(), ref.repoKey); se != nil {
				writeVerbatimStatusError(w, se)
				return
			}
		}
		w.Header().Set("Allow", http.MethodGet)
		writeSpecError(w, http.StatusMethodNotAllowed, ErrCodeUnsupported,
			fmt.Sprintf("Virtual repository '%s' is an aggregated read plane; deployments through it are not accepted.", ref.repoKey), nil)
		return
	}
	switch {
	case strings.HasPrefix(ref.tail, blobsTail):
		h.serveVirtualBlob(w, r, ref, ref.tail)
	case strings.HasPrefix(ref.tail, manifestsTail):
		h.serveVirtualManifest(w, r, ref, ref.tail)
	case ref.tail == tagsListTail:
		// The service's union listing (a member's cached tag rows for a
		// remote member — T-363 D-2, no upstream tags/list proxying).
		h.serveTagsList(w, r, ref)
	default:
		writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported,
			"registry route /v2/"+ref.repoKey+"/"+ref.image+"/"+ref.tail+" is not implemented in BinFlow M2 yet", nil)
	}
}

// serveVirtualManifest implements the manifest arm of the virtual walk.
func (h *Handler) serveVirtualManifest(w http.ResponseWriter, r *http.Request, ref nameRef, tail string) {
	reference, ok := strings.CutPrefix(tail, manifestsTail)
	if !ok || reference == "" {
		writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported, "unknown manifest route "+tail, nil)
		return
	}
	unfound := manifestUnfound(ref.image)
	// The reference shape runs BEFORE anything else (the local plane's
	// rule): a sha256: spelling must parse, anything else a legal tag.
	isDigestRef := strings.HasPrefix(reference, digestPrefix)
	var wantHex string
	if isDigestRef {
		hexPart, err := parseDigestParam(reference)
		if err != nil {
			writeSpecError(w, http.StatusBadRequest, ErrCodeDigestInvalid,
				fmt.Sprintf("manifest reference %q is not a valid sha256 digest", reference),
				map[string]string{"digest": reference})
			return
		}
		wantHex = hexPart
	} else if err := validateManifestTag(reference); err != nil {
		writeSpecError(w, http.StatusBadRequest, ErrCodeManifestInvalid,
			fmt.Sprintf("manifest reference %q is neither a digest nor a legal tag: %v", reference, err), nil)
		return
	}

	ctx := r.Context()
	p := principalOf(r)
	plane := h.virtualPlane()
	if plane == nil {
		h.log.ErrorContext(ctx, "docker: virtual manifest without the v2 plane seam", "repo", ref.repoKey)
		writeSpecError(w, http.StatusServiceUnavailable, ErrCodeUnavailable,
			"the registry's service carries no v2 virtual plane", nil)
		return
	}
	order, err := plane.V2MemberOrder(ctx, ref.repoKey)
	if err != nil {
		h.writeManifestReadError(w, r, err, ref, reference)
		return
	}

	summary := "" // the last upstream summary, for the final unfound body
	for _, m := range order {
		facts, ferr := plane.V2MemberManifest(ctx, p, ref.repoKey, m.Key, ref.image, reference)
		if ferr != nil {
			h.writeManifestReadError(w, r, ferr, ref, reference)
			return
		}
		switch m.Type {
		case repo.TypeLocal:
			if facts.Digest == "" || facts.Node == nil {
				continue // member miss (no rows, or the crash-window node gap)
			}
			mediaType := facts.MediaType
			if mediaType == "" {
				mediaType = facts.Node.Mime
			}
			if !acceptAllows(r.Header.Values("Accept"), mediaType) {
				writeSpecError(w, http.StatusNotFound, ErrCodeManifestUnknown,
					fmt.Sprintf("manifest %s is not available in an accepted media type (stored: %s)", reference, mediaType),
					map[string]string{"mediaType": mediaType})
				return
			}
			h.serveVirtualManifestCopy(w, r, m.Key, "", facts.Node, mediaType, facts.Digest, facts.Size, "", "")
			return
		case repo.TypeRemote:
			var standing *remoteStanding
			if facts.Digest != "" && facts.Node != nil {
				standing = &remoteStanding{node: facts.Node, mediaType: facts.MediaType, dgst: facts.Digest, size: facts.Size}
			}
			switch facts.Cache {
			case repo.RemoteProbeHit:
				mediaType := facts.MediaType
				if mediaType == "" {
					mediaType = facts.Node.Mime
				}
				if !acceptAllows(r.Header.Values("Accept"), mediaType) {
					writeSpecError(w, http.StatusNotFound, ErrCodeManifestUnknown,
						fmt.Sprintf("manifest %s is not available in an accepted media type (stored: %s)", reference, mediaType),
						map[string]string{"mediaType": mediaType})
					return
				}
				h.serveVirtualManifestCopy(w, r, m.Key, "", facts.Node, mediaType, facts.Digest, facts.Size, remote.CacheHit, "")
				return
			case repo.RemoteProbeNegative:
				continue // the member's fresh miss record: walk on
			}
			// STALE (an expired copy stands) or MISS (nothing local): the
			// member's upstream conversation decides.
			outcome, why := h.serveVirtualRemoteManifest(w, r, plane, ref, m.Key, reference, isDigestRef, wantHex, standing)
			switch outcome {
			case virtualMemberServed, virtualMemberFault:
				return
			}
			summary = why
		}
	}
	// No member answered: the unfound family with the last upstream
	// summary — never a naked 5xx (FR-116.5 through the aggregation).
	unfound.write(w, summary)
}

// virtualMemberOutcome classifies one remote member's walk step.
type virtualMemberOutcome int

const (
	// virtualMemberServed: the response is written (a copy served).
	virtualMemberServed virtualMemberOutcome = iota
	// virtualMemberUnfound: the member has no answer — the walk continues.
	virtualMemberUnfound
	// virtualMemberFault: the response is written (a classified failure
	// propagated verbatim, the SSRF/body-ceiling family).
	virtualMemberFault
)

// serveVirtualRemoteManifest runs one REMOTE member's pull-through
// conversation as a step of the virtual walk: the upstream session, the
// fetch, the landing into the MEMBER's cache (through the V2 seam), the
// degradation matrix. The outcome tells the walk whether the response is
// written; the returned summary is the member's why for the final unfound
// answer when nothing served.
func (h *Handler) serveVirtualRemoteManifest(w http.ResponseWriter, r *http.Request, plane repo.V2VirtualPlane, ref nameRef, member, reference string, isDigestRef bool, wantHex string, standing *remoteStanding) (virtualMemberOutcome, string) {
	ctx := r.Context()
	p := principalOf(r)
	entry, facts, serr := h.virtualMemberSession(ctx, p, plane, ref.repoKey, member)
	if serr != nil {
		// The member's own unfound-shaped refusals (the blackout's 404
		// among them) continue the walk; honest failures surface.
		var se *repo.StatusError
		if errors.As(serr, &se) && se.Code == http.StatusNotFound {
			return virtualMemberUnfound, se.Message
		}
		h.writeRemoteServeError(w, r, serr, ref)
		return virtualMemberFault, serr.Error()
	}
	origin := originRemoteOf(facts, v2WireManifestPath(ref.image, reference))
	fetched, ferr := entry.fetchManifest(ctx, ref.image, reference, r.Header.Values("Accept"),
		revalidationValidator(standing, isDigestRef))
	if ferr != nil {
		var serveStale func(string)
		if standing != nil {
			mediaType, size := standing.mediaType, standing.size
			if mediaType == "" {
				mediaType = standing.node.Mime
			}
			if size == 0 {
				size = standing.node.Size
			}
			serveStale = func(summary string) {
				h.serveVirtualManifestCopy(w, r, member, origin, standing.node, mediaType, standing.dgst, size, remote.CacheStale, summary)
			}
		}
		if h.writeVirtualFetchFault(w, r, ferr, ref, serveStale) {
			return virtualMemberFault, ""
		}
		return virtualMemberUnfound, upstreamFaultSummary(ferr)
	}
	switch fetched.status {
	case http.StatusOK:
		h.landFetchedManifest(w, r, ref, reference, isDigestRef, wantHex, fetched, remoteManifestSink{
			land: func(path, expectHex, mime string, body io.Reader) (*metadata.Node, error) {
				return plane.V2LandMemberBlob(ctx, p, ref.repoKey, member, path, expectHex, mime, body)
			},
			record: func(dgst, tag, mediaType string, size int64, refs []*metadata.DockerRef) error {
				return plane.V2RecordMemberManifest(ctx, p, ref.repoKey, member, ref.image, dgst, tag, mediaType, size, refs)
			},
		}, func(w http.ResponseWriter, r *http.Request, node *metadata.Node, mediaType, dgst string, size int64, cacheState, upstreamError string) {
			h.serveVirtualManifestCopy(w, r, member, origin, node, mediaType, dgst, size, cacheState, upstreamError)
		})
		return virtualMemberServed, ""
	case http.StatusNotModified:
		// The revalidation arm (§5.3), member-scoped: slide the member's
		// window through the V2 seam and serve the confirmed copy.
		if standing != nil && standing.node != nil {
			h.serveRevalidatedManifest(w, r, ref, standing, func(mediaType string, body io.Reader) error {
				_, lerr := plane.V2LandMemberBlob(ctx, p, ref.repoKey, member, manifestNodePath(ref.image, standing.dgst), standing.dgst, mediaType, body)
				return lerr
			}, func(w http.ResponseWriter, r *http.Request, node *metadata.Node, mediaType, dgst string, size int64, cacheState, upstreamError string) {
				h.serveVirtualManifestCopy(w, r, member, origin, node, mediaType, dgst, size, cacheState, upstreamError)
			})
			return virtualMemberServed, ""
		}
		return virtualMemberUnfound, fmt.Sprintf("upstream answered 304 %s without a validator being offered", fetched.statusT)
	case http.StatusNotFound:
		// The engine's step: record the miss (the standing arm keys the
		// digest's manifest node path; the cold-miss arm below keys the
		// reference shape — digest the same node path, tag the image's
		// tags/ row), member-scoped, then an expired copy still serves
		// (STALE).
		if standing != nil {
			_ = plane.V2CacheMemberMiss(ctx, p, ref.repoKey, member, manifestNodePath(ref.image, standing.dgst)) //nolint:errcheck // best-effort bookkeeping; the serve stands
			h.serveVirtualManifestCopy(w, r, member, origin, standing.node, standing.mediaType, standing.dgst, standing.size,
				remote.CacheStale, "upstream 404 (expired copy served)")
			return virtualMemberServed, ""
		}
		// The cold miss records the reference-keyed miss row in the MEMBER's
		// cache (C14 — both reference shapes: the digest at the manifest
		// node path, the tag under the image's tags/ row). The member-facts
		// seam probes the row on the next ask (L006-2), so the walk reuses
		// the member's miss record inside its window — the same memory the
		// member's own direct face consults, cross-face consistent.
		_ = plane.V2CacheMemberMiss(ctx, p, ref.repoKey, member, manifestMissNodePath(ref.image, reference, isDigestRef, wantHex)) //nolint:errcheck // best-effort bookkeeping; the serve stands
		return virtualMemberUnfound, fmt.Sprintf("upstream answered 404 %s", fetched.statusT)
	case http.StatusUnauthorized, http.StatusForbidden:
		// Credentials refused upstream (the engine's 401/403 posture — no
		// negative cache, the credential state is correctable).
		if standing != nil {
			h.serveVirtualManifestCopy(w, r, member, origin, standing.node, standing.mediaType, standing.dgst, standing.size,
				remote.CacheStale, fmt.Sprintf("upstream %d %s", fetched.status, fetched.statusT))
			return virtualMemberServed, ""
		}
		return virtualMemberUnfound, fmt.Sprintf("upstream answered %d %s; credentials refused or insufficient", fetched.status, fetched.statusT)
	default:
		if standing != nil {
			h.serveVirtualManifestCopy(w, r, member, origin, standing.node, standing.mediaType, standing.dgst, standing.size,
				remote.CacheStale, fmt.Sprintf("upstream %d %s", fetched.status, fetched.statusT))
			return virtualMemberServed, ""
		}
		return virtualMemberUnfound, fmt.Sprintf("upstream answered %d %s", fetched.status, fetched.statusT)
	}
}

// serveVirtualBlob implements the blob arm of the virtual walk.
func (h *Handler) serveVirtualBlob(w http.ResponseWriter, r *http.Request, ref nameRef, tail string) {
	digestParam, ok := strings.CutPrefix(tail, blobsTail)
	if !ok || digestParam == "" {
		writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported, "unknown blob route "+tail, nil)
		return
	}
	unfound := blobUnfound(digestParam)
	hex, err := parseDigestParam(digestParam)
	if err != nil {
		writeSpecError(w, http.StatusBadRequest, ErrCodeDigestInvalid,
			fmt.Sprintf("digest %q is not a valid sha256 digest", digestParam),
			map[string]string{"digest": digestParam})
		return
	}
	if hex == emptyLayerDigestHex {
		h.serveEmptyLayer(w, r) // the canonical bytes are member-independent
		return
	}

	ctx := r.Context()
	p := principalOf(r)
	plane := h.virtualPlane()
	if plane == nil {
		h.log.ErrorContext(ctx, "docker: virtual blob without the v2 plane seam", "repo", ref.repoKey)
		writeSpecError(w, http.StatusServiceUnavailable, ErrCodeUnavailable,
			"the registry's service carries no v2 virtual plane", nil)
		return
	}
	order, oerr := plane.V2MemberOrder(ctx, ref.repoKey)
	if oerr != nil {
		h.writeBlobReadError(w, r, oerr, ref, hex)
		return
	}

	summary := ""
	for _, m := range order {
		blob, berr := plane.V2MemberBlob(ctx, p, ref.repoKey, m.Key, ref.image, hex)
		if berr != nil {
			h.writeBlobReadError(w, r, berr, ref, hex)
			return
		}
		switch m.Type {
		case repo.TypeLocal:
			if blob.Node == nil {
				continue
			}
			h.serveVirtualBlobCopy(w, r, m.Key, "", blob.Node, "", "")
			return
		case repo.TypeRemote:
			switch blob.Cache {
			case repo.RemoteProbeHit:
				h.serveVirtualBlobCopy(w, r, m.Key, "", blob.Node, remote.CacheHit, "")
				return
			case repo.RemoteProbeNegative:
				continue
			}
			outcome, why := h.serveVirtualRemoteBlob(w, r, plane, ref, m.Key, hex, blob.Node)
			switch outcome {
			case virtualMemberServed, virtualMemberFault:
				return
			}
			summary = why
		}
	}
	unfound.write(w, summary)
}

// serveVirtualRemoteBlob runs one REMOTE member's blob pull-through as a
// step of the virtual walk (the streaming landing with the digest
// ENFORCED at commit, the degradation matrix on faults).
func (h *Handler) serveVirtualRemoteBlob(w http.ResponseWriter, r *http.Request, plane repo.V2VirtualPlane, ref nameRef, member, hex string, standingNode *metadata.Node) (virtualMemberOutcome, string) {
	ctx := r.Context()
	p := principalOf(r)
	entry, facts, serr := h.virtualMemberSession(ctx, p, plane, ref.repoKey, member)
	if serr != nil {
		var se *repo.StatusError
		if errors.As(serr, &se) && se.Code == http.StatusNotFound {
			return virtualMemberUnfound, se.Message
		}
		h.writeRemoteServeError(w, r, serr, ref)
		return virtualMemberFault, serr.Error()
	}
	origin := originRemoteOf(facts, v2WireBlobPath(ref.image, hex))
	var serveStale func(string)
	if standingNode != nil {
		serveStale = func(summary string) {
			h.serveVirtualBlobCopy(w, r, member, origin, standingNode, remote.CacheStale, summary)
		}
	}
	// The marker gate, member-scoped (ADR-0047 §3): a cold miss the MEMBER's
	// chains never named is unfound for this member — walk on, no upstream
	// contact, no negative-cache row, no summary (the deterministic answer
	// keeps the terminal body plain, the E4-2 shape).
	if standingNode == nil && !h.memberChainAdmits(ctx, p, ref, member, hex) {
		return virtualMemberUnfound, ""
	}
	path := blobNodePath(ref.image, hex)
	stream, ferr := entry.fetchBlobStream(ctx, ref.image, hex)
	if ferr != nil {
		if h.writeVirtualFetchFault(w, r, ferr, ref, serveStale) {
			return virtualMemberFault, ""
		}
		return virtualMemberUnfound, upstreamFaultSummary(ferr)
	}
	switch stream.StatusCode {
	case http.StatusOK:
		node, lerr := plane.V2LandMemberBlob(ctx, p, ref.repoKey, member, path, hex,
			mediaTypeOfHeader(stream.Header.Get("Content-Type")), stream.Body)
		drainUpstream(stream.Body)
		if lerr != nil {
			h.writeRemoteServeError(w, r, lerr, ref)
			return virtualMemberFault, lerr.Error()
		}
		h.serveVirtualBlobCopy(w, r, member, origin, node, remote.CacheMiss, "")
		return virtualMemberServed, ""
	case http.StatusNotFound:
		drainUpstream(stream.Body)
		_ = plane.V2CacheMemberMiss(ctx, p, ref.repoKey, member, path) //nolint:errcheck // best-effort bookkeeping; the serve stands
		if standingNode != nil {
			h.serveVirtualBlobCopy(w, r, member, origin, standingNode, remote.CacheStale, "upstream 404 (expired copy served)")
			return virtualMemberServed, ""
		}
		return virtualMemberUnfound, fmt.Sprintf("upstream answered 404 %s", stream.Status)
	default:
		drainUpstream(stream.Body)
		if standingNode != nil {
			h.serveVirtualBlobCopy(w, r, member, origin, standingNode, remote.CacheStale,
				fmt.Sprintf("upstream %d %s", stream.StatusCode, stream.Status))
			return virtualMemberServed, ""
		}
		return virtualMemberUnfound, fmt.Sprintf("upstream answered %d %s", stream.StatusCode, stream.Status)
	}
}

// memberChainAdmits is blobChainAdmits' membership-guarded twin for the
// virtual walk: the gate query addresses the REMOTE member's
// (repoKey, image), never the virtual key (ADR-0047 §3). The same
// fault-admits posture as the direct arm.
func (h *Handler) memberChainAdmits(ctx context.Context, p *Principal, ref nameRef, member, hex string) bool {
	gate := h.chainGate()
	if gate == nil {
		return true
	}
	in, err := gate.V2MemberBlobInChain(ctx, p, ref.repoKey, member, ref.image, hex)
	if err != nil {
		h.log.WarnContext(ctx, "docker virtual: member chain gate unavailable (fetching)",
			"repo", ref.repoKey, "member", member, "image", ref.image, "path", blobNodePath(ref.image, hex),
			"cache_result", "chain-gate-error", "error", err.Error())
		return true
	}
	return in
}

// virtualMemberSession resolves one remote MEMBER's upstream session
// during a virtual walk: the facts come through the V2 seam
// (membership-guarded — the walk was already gated on the VIRTUAL key),
// the pooled clients are the same per-repository pool the direct plane
// uses, keyed on the member.
func (h *Handler) virtualMemberSession(ctx context.Context, p *Principal, plane repo.V2VirtualPlane, virtualKey, member string) (*remoteSessionEntry, *repo.RemoteUpstream, error) {
	facts, err := plane.V2MemberUpstream(ctx, p, virtualKey, member)
	if err != nil {
		return nil, nil, err
	}
	if facts.BlockedOut {
		return nil, facts, &repo.StatusError{
			Code: http.StatusNotFound,
			Message: fmt.Sprintf("The repository '%s' is blacked out and cannot serve content.",
				member),
		}
	}
	entry, err := h.remotes.forRepo(member, facts)
	if err != nil {
		return nil, facts, err
	}
	return entry, facts, nil
}

// writeVirtualFetchFault is the walk's variant of the direct plane's
// fetch-fault mapper: the CLASSIFIED failures (the SSRF chain's 400, the
// body ceiling's 502) and a standing copy's STALE serve write the response
// and answer true; a fault with no copy answers false — the member is
// unfound and the WALK continues (the direct plane would answer the
// unfound family itself; here a later member may still hold the body).
func (h *Handler) writeVirtualFetchFault(w http.ResponseWriter, r *http.Request, err error, ref nameRef, serveStale func(summary string)) bool {
	var rej *remote.RejectionError
	switch {
	case errors.As(err, &rej):
		writeSpecError(w, http.StatusBadRequest, ErrCodeUnsupported,
			fmt.Sprintf("Cannot fetch '%s/%s': upstream target refused — private or suppressed upstream (%v)",
				ref.repoKey, ref.image, err), nil)
		return true
	case errors.Is(err, remote.ErrBodyTooLarge):
		writeSpecError(w, http.StatusBadGateway, ErrCodeUnknown,
			fmt.Sprintf("Failed to proxy '%s': %v", ref.repoKey, err), nil)
		return true
	default:
		summary := upstreamFaultSummary(err)
		if serveStale != nil {
			serveStale(summary)
			return true
		}
		h.log.WarnContext(r.Context(), "docker virtual: member "+summary,
			"repo", ref.repoKey, "error", err.Error())
		return false
	}
}

// upstreamFaultSummary renders one transport/token fault's summary line
// (the marker and the unfound body share it).
func upstreamFaultSummary(err error) string {
	if errors.Is(err, errUpstreamAuth) {
		return "upstream token exchange failed"
	}
	return "upstream unreachable"
}

// serveVirtualManifestCopy serves one member's manifest copy through the
// remote plane's copy server with the resolution hint on top (the walk's
// winning member — the diagnostic the generic virtual face carries too).
//
// Review B #4 semantic note: on the VIRTUAL plane the copy face's registry
// key — what a manifest HEAD's X-Artifactory-Docker-Registry spells — is
// the WINNING MEMBER's key, not the repository the client addressed. That
// is a different answer than the DIRECT remote face's (remote_face.go: the
// client-addressed repoKey), and it is deliberate: the virtual walk's
// whole face is resolution-relative (X-Resolved-From carries the same
// member), the member key being the resolution's own registry. The face
// here has no live-capture backing for the member-key spelling (L003-2
// noted the virtual evidence gap) — the note pins the semantics so the
// contract review does not read the two spellings as an accident to
// "fix".
func (h *Handler) serveVirtualManifestCopy(w http.ResponseWriter, r *http.Request, member, origin string, node *metadata.Node, mediaType, dgst string, size int64, cacheState, upstreamError string) {
	w.Header().Set(repo.HdrResolvedFrom, member)
	h.serveRemoteManifestCopy(w, r, remoteFace{registry: member, origin: origin}, node, mediaType, dgst, size, cacheState, upstreamError)
}

// serveVirtualBlobCopy serves one member's blob copy: local members ride
// the same body server the remote copies do (Range, the checksum family,
// Docker-Content-Digest) — a cache-less local copy simply carries no
// cache markers.
func (h *Handler) serveVirtualBlobCopy(w http.ResponseWriter, r *http.Request, member, origin string, node *metadata.Node, cacheState, upstreamError string) {
	w.Header().Set(repo.HdrResolvedFrom, member)
	h.serveRemoteBlobCopy(w, r, remoteFace{registry: member, origin: origin}, node, cacheState, upstreamError)
}
