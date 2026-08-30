package repo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The registry-v2 VIRTUAL aggregation's service half (M13 T-365, FR-116.2;
// helm.md section 8's family posture — one /v2 stack, the member set kept
// single-family by validateHelmFamilyMix at config time).
//
// A virtual docker/helmoci repository is an AGGREGATED READ plane: the
// four read use cases (ResolveManifest / ResolveTag / ListTags /
// ListImages) walk the member two-bucket order and answer the union or the
// first member that has the fact (the same first-hit-stops rule getVirtual
// serves the generic plane with, FR-21-AC1's registry-v2 spelling), while
// the docker ADAPTER drives the serving walk itself through the
// V2VirtualPlane seam below — a remote member's miss is an upstream
// conversation (the T-363 RemoteV2Plane posture), which is the adapter's
// session to run, so the seam exposes exactly the member-scoped facts and
// landing hooks that walk needs.
//
// The ungated-member posture is getVirtual's (virtual.go): the /v2 route
// gate has already answered the permission question on the VIRTUAL key,
// and members are resolution internals — the membership guard inside every
// member-scoped method is what keeps the ungated reads from ever addressing
// an arbitrary repository.

// Compile-time pin: the service implements the adapter-facing capability.
var _ V2VirtualPlane = (*service)(nil)

// loadV2VirtualRepo resolves repoKey and asserts it is a VIRTUAL
// registry-v2 family repository (the seam's own gate — the read use cases
// admit all three classes through loadV2ReadRepo, the seam's member walk
// must not run against a local or remote key).
func (s *service) loadV2VirtualRepo(ctx context.Context, virtualKey string) (*metadata.Repo, error) {
	r, err := s.loadRepoRow(ctx, virtualKey)
	if err != nil {
		return nil, err
	}
	if r.Type != TypeVirtual {
		return nil, fmt.Errorf("%w: %s repositories have no registry v2 member aggregation", ErrRepoTypeNotSupported, r.Type)
	}
	if !isV2PlaneFamily(r.PackageType) {
		return nil, fmt.Errorf("repo %q: %w: package type is %q, not one of the registry v2 family (%s, %s)",
			virtualKey, ErrRepoTypeNotSupported, r.PackageType, PackageDocker, PackageHelmOCI)
	}
	return r, nil
}

// v2VirtualGuard asserts one member currently sits in the virtual's
// resolution order and returns its walk entry (class included — the caller
// branches local vs remote on it).
func (s *service) v2VirtualGuard(ctx context.Context, virtualKey, member string) (virtualMember, error) {
	order, err := s.virtualMemberOrder(ctx, virtualKey)
	if err != nil {
		return virtualMember{}, err
	}
	for i := range order {
		if order[i].key == member {
			return order[i], nil
		}
	}
	return virtualMember{}, fmt.Errorf("member %s of virtual %s: %w: not a current member",
		member, virtualKey, ErrRepoNotFound)
}

// ---- the adapter seam (V2VirtualPlane) ----

// V2MemberOrder implements V2VirtualPlane: the two-bucket member order of
// one registry-v2 family virtual repository, fresh off the ledger on every
// call (member changes are immediately effective, FR-15-AC6).
func (s *service) V2MemberOrder(ctx context.Context, virtualKey string) ([]VirtualMember, error) {
	if _, err := s.loadV2VirtualRepo(ctx, virtualKey); err != nil {
		return nil, err
	}
	return s.VirtualMemberOrder(ctx, virtualKey)
}

// V2MemberManifest implements V2VirtualPlane: one member's local fact base
// for a manifest reference, with no upstream contact. A member whose rows
// do not answer returns the zero-ish miss shape (Digest "") — the walk
// continues; a REMOTE member's RemoteProbeMiss is the caller's signal to
// run that member's upstream conversation (the T-363 posture: no local
// rows still asks upstream, the tag may simply never have been cached).
//
// A local member whose rows answer but whose manifest NODE is missing (the
// crash window) reads as a miss — probeLocalMember's posture: a member
// that cannot produce the body does not stop the walk.
func (s *service) V2MemberManifest(ctx context.Context, p *Principal, virtualKey, member, image, reference string) (*V2MemberManifest, error) {
	if err := validateDockerImage(image); err != nil {
		return nil, err
	}
	isDigestRef := strings.HasPrefix(reference, "sha256:")
	if isDigestRef {
		if _, err := parseV2ReferenceDigest(reference); err != nil {
			return nil, err
		}
	} else if err := validateTag(reference); err != nil {
		return nil, fmt.Errorf("manifest reference %q: %w", reference, err)
	}
	m, err := s.v2VirtualGuard(ctx, virtualKey, member)
	if err != nil {
		return nil, err
	}
	miss := &V2MemberManifest{Cache: remoteMemberCacheOf(m.typ)}
	if m.typ == TypeRemote {
		// The remote member's rows ARE cache state; the probe classifies
		// the standing copy without touching the network.
		dgst, row, ok, rerr := s.v2MemberRows(ctx, m.key, image, reference, isDigestRef)
		if rerr != nil {
			return nil, rerr
		}
		if !ok {
			return miss, nil // no rows: the upstream conversation decides
		}
		probe, perr := s.probeRemoteV2Core(ctx, m.key, dockerImageManifestPath(image, dgst))
		if perr != nil {
			return nil, perr
		}
		return &V2MemberManifest{
			Digest: dgst, MediaType: row.MediaType, Size: row.Size,
			Node: probe.Node, Cache: probe.State,
		}, nil
	}
	dgst, row, ok, rerr := s.v2MemberRows(ctx, m.key, image, reference, isDigestRef)
	if rerr != nil {
		return nil, rerr
	}
	if !ok {
		return miss, nil
	}
	node, nerr := s.md.Nodes().Get(ctx, m.key, dockerImageManifestPath(image, dgst))
	if nerr != nil {
		if errors.Is(nerr, metadata.ErrNodeNotFound) {
			return miss, nil // rows without a node: the crash window, walk on
		}
		return nil, fmt.Errorf("node %s/%s: %w", m.key, dockerImageManifestPath(image, dgst), nerr)
	}
	s.auditVirtualResolve(ctx, p, virtualKey, m.key, dockerImageManifestPath(image, dgst))
	return &V2MemberManifest{Digest: dgst, MediaType: row.MediaType, Size: row.Size, Node: node}, nil
}

// V2MemberBlob implements V2VirtualPlane: one member's standing copy at a
// digest-keyed blob path — a local member answers its node row, a remote
// member the cache probe's states verbatim.
func (s *service) V2MemberBlob(ctx context.Context, p *Principal, virtualKey, member, image, hex string) (*V2MemberBlob, error) {
	_ = p // the walk audits at serve time; this probe is read-only state
	if err := validateDockerImage(image); err != nil {
		return nil, err
	}
	if err := validateDigest(hex); err != nil {
		return nil, err
	}
	m, err := s.v2VirtualGuard(ctx, virtualKey, member)
	if err != nil {
		return nil, err
	}
	path := image + "/blobs/" + hex
	if m.typ == TypeRemote {
		probe, perr := s.probeRemoteV2Core(ctx, m.key, path)
		if perr != nil {
			return nil, perr
		}
		return &V2MemberBlob{Node: probe.Node, Cache: probe.State}, nil
	}
	node, nerr := s.md.Nodes().Get(ctx, m.key, path)
	if nerr != nil {
		if errors.Is(nerr, metadata.ErrNodeNotFound) {
			return &V2MemberBlob{}, nil
		}
		return nil, fmt.Errorf("node %s/%s: %w", m.key, path, nerr)
	}
	return &V2MemberBlob{Node: node}, nil
}

// V2MemberUpstream implements V2VirtualPlane: one REMOTE member's upstream
// connection facts, membership-guarded (the direct plane's RemoteUpstream
// re-checks the member's own read grant; the walk has already been gated
// on the VIRTUAL key).
func (s *service) V2MemberUpstream(ctx context.Context, p *Principal, virtualKey, member string) (*RemoteUpstream, error) {
	_ = p // membership-guarded by contract; the principal only feeds logs
	m, err := s.v2VirtualGuard(ctx, virtualKey, member)
	if err != nil {
		return nil, err
	}
	if m.typ != TypeRemote {
		return nil, fmt.Errorf("%w: member %s of virtual %s is %s, not a remote pull-through",
			ErrRepoTypeNotSupported, member, virtualKey, m.typ)
	}
	return s.remoteUpstreamCore(ctx, m.key)
}

// V2LandMemberBlob implements V2VirtualPlane: the RemoteV2Plane landing
// chain against one REMOTE member (membership replaces the member's own
// permission pair as the guard; the invariants are the engine's land()
// verbatim).
func (s *service) V2LandMemberBlob(ctx context.Context, p *Principal, virtualKey, member, path, expectHex, mime string, body io.Reader) (*metadata.Node, error) {
	m, err := s.v2VirtualGuard(ctx, virtualKey, member)
	if err != nil {
		return nil, err
	}
	if m.typ != TypeRemote {
		return nil, fmt.Errorf("%w: member %s of virtual %s is %s, not a remote pull-through",
			ErrRepoTypeNotSupported, member, virtualKey, m.typ)
	}
	if err := validateNodePath(path); err != nil {
		return nil, err
	}
	if err := validateDigest(expectHex); err != nil {
		return nil, err
	}
	node, err := s.landRemoteV2Core(ctx, p, m.key, path, expectHex, mime, body)
	if err != nil {
		return nil, err
	}
	s.auditVirtualResolve(ctx, p, virtualKey, m.key, path)
	return node, nil
}

// V2RecordMemberManifest implements V2VirtualPlane: the cache-population
// index rows against one REMOTE member.
func (s *service) V2RecordMemberManifest(ctx context.Context, p *Principal, virtualKey, member, image, digest, tag, mediaType string, size int64, refs []*metadata.DockerRef) error {
	_ = p // cache population runs no permission gates (RecordRemoteManifest's posture)
	m, err := s.v2VirtualGuard(ctx, virtualKey, member)
	if err != nil {
		return err
	}
	if m.typ != TypeRemote {
		return fmt.Errorf("%w: member %s of virtual %s is %s, not a remote pull-through",
			ErrRepoTypeNotSupported, member, virtualKey, m.typ)
	}
	if err := validateDockerImage(image); err != nil {
		return err
	}
	if err := validateDigest(digest); err != nil {
		return err
	}
	if tag != "" {
		if err := validateTag(tag); err != nil {
			return err
		}
	}
	if mediaType == "" {
		return fmt.Errorf("manifest %s/%s@%s: %w: media type is empty", member, image, digest, ErrInvalidManifest)
	}
	if size < 0 {
		return fmt.Errorf("manifest %s/%s@%s: %w: negative size", member, image, digest, ErrInvalidManifest)
	}
	return s.recordRemoteManifestCore(ctx, m.key, image, digest, tag, mediaType, size, refs)
}

// V2CacheMemberMiss implements V2VirtualPlane: the negative-cache write
// against one REMOTE member.
func (s *service) V2CacheMemberMiss(ctx context.Context, p *Principal, virtualKey, member, path string) error {
	_ = p // bookkeeping only; the miss window needs no permission question
	m, err := s.v2VirtualGuard(ctx, virtualKey, member)
	if err != nil {
		return err
	}
	if m.typ != TypeRemote {
		return fmt.Errorf("%w: member %s of virtual %s is %s, not a remote pull-through",
			ErrRepoTypeNotSupported, member, virtualKey, m.typ)
	}
	if err := validateNodePath(path); err != nil {
		return err
	}
	return s.cacheRemoteMissCore(ctx, m.key, path)
}

// V2WriteRefusal implements V2VirtualPlane: the /v2 write refusal's exact
// rendering. Un-routed virtuals keep the C5 405 verbatim (refuseVirtualWrite
// — the one spelling the generic plane's RE-08 answer carries); a virtual
// WITH a defaultDeploymentRepo gets the honest wording instead, because
// registry-v2 push-through routing is not implemented (T-365's scope is the
// aggregated READ plane; claiming "no local repository was configured"
// about one that IS would be a lie).
func (s *service) V2WriteRefusal(ctx context.Context, virtualKey string) *StatusError {
	row, err := s.loadV2VirtualRepo(ctx, virtualKey)
	if err != nil {
		return &StatusError{
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
			cause:   err,
		}
	}
	if target := virtualRouteTarget(row.Config); target != "" {
		return &StatusError{
			Code: http.StatusMethodNotAllowed,
			Message: fmt.Sprintf(
				"The virtual repository '%s' does not accept pushes on the registry v2 plane; push to its local deployment repository '%s' directly.",
				virtualKey, target),
			Header: http.Header{"Allow": []string{http.MethodGet}},
			cause:  fmt.Errorf("%w: registry v2 virtual push-through routing is not implemented", ErrRepoTypeNotSupported),
		}
	}
	var se *StatusError
	if errors.As(refuseVirtualWrite(virtualKey), &se) {
		return se
	}
	// Unreachable (refuseVirtualWrite always builds a *StatusError); the
	// fallback keeps the method total without depending on that fact.
	return &StatusError{
		Code:    http.StatusMethodNotAllowed,
		Message: msgNoDeploymentRepo(virtualKey),
		Header:  http.Header{"Allow": []string{http.MethodGet}},
		cause:   fmt.Errorf("%w: virtual repository %q has no defaultDeploymentRepo", ErrRepoTypeNotSupported, virtualKey),
	}
}

// remoteMemberCacheOf spells a walk miss per member class: a remote member
// that cannot answer from local facts is a RemoteProbeMiss (the upstream
// conversation decides); a local member carries no cache semantics at all.
func remoteMemberCacheOf(memberType string) string {
	if memberType == TypeRemote {
		return RemoteProbeMiss
	}
	return ""
}

// v2MemberRows resolves one reference against a member's docker index
// rows: a digest reference reads the manifest row directly; a tag
// reference reads the tag row first and follows its CURRENT digest (a tag
// row whose manifest row is gone — crash residue — reads as no answer).
func (s *service) v2MemberRows(ctx context.Context, member, image, reference string, isDigestRef bool) (string, *metadata.DockerManifest, bool, error) {
	if isDigestRef {
		hexPart, err := parseV2ReferenceDigest(reference)
		if err != nil {
			return "", nil, false, err
		}
		row, gerr := s.md.Docker().GetManifest(ctx, member, image, hexPart)
		if gerr != nil {
			if errors.Is(gerr, metadata.ErrManifestNotFound) {
				return "", nil, false, nil
			}
			return "", nil, false, fmt.Errorf("manifest %s/%s@%s: %w", member, image, hexPart, gerr)
		}
		return hexPart, row, true, nil
	}
	t, terr := s.md.Docker().GetTag(ctx, member, image, reference)
	if terr != nil {
		if errors.Is(terr, metadata.ErrTagNotFound) {
			return "", nil, false, nil
		}
		return "", nil, false, fmt.Errorf("tag %s/%s:%s: %w", member, image, reference, terr)
	}
	if t.Digest == "" {
		return "", nil, false, nil
	}
	row, gerr := s.md.Docker().GetManifest(ctx, member, image, t.Digest)
	if gerr != nil {
		if errors.Is(gerr, metadata.ErrManifestNotFound) {
			return "", nil, false, nil
		}
		return "", nil, false, fmt.Errorf("manifest %s/%s@%s: %w", member, image, t.Digest, gerr)
	}
	return t.Digest, row, true, nil
}

// parseV2ReferenceDigest validates and strips a "sha256:"-prefixed
// reference into bare hex (the walk receives the wire spelling).
func parseV2ReferenceDigest(reference string) (string, error) {
	hexPart := strings.TrimPrefix(reference, "sha256:")
	if err := validateDigest(hexPart); err != nil {
		return "", fmt.Errorf("manifest reference %q: %w", reference, err)
	}
	return hexPart, nil
}

// auditVirtualResolve records the walk's download row addressed to the
// VIRTUAL (getVirtual's shape): the member that served rides the detail so
// "which copy did I get" is answerable from the audit trail alone.
func (s *service) auditVirtualResolve(ctx context.Context, p *Principal, virtualKey, member, path string) {
	s.audit(ctx, AuditEvent{
		Actor: actor(p), Action: AuditActionDownload, Repo: virtualKey, Path: path,
		Detail: fmt.Sprintf(`{"resolvedFrom":%q}`, member),
	})
}

// ---- the read use cases' virtual arms ----

// resolveV2VirtualManifest is ResolveManifest's virtual arm: walk the
// two-bucket order, the first member whose manifest row answers wins
// (first-seen semantics — member order, not content freshness, decides).
func (s *service) resolveV2VirtualManifest(ctx context.Context, p *Principal, virtualKey, image, digest string) (*metadata.DockerManifest, error) {
	order, err := s.v2VirtualWalkOrder(ctx, virtualKey)
	if err != nil {
		return nil, err
	}
	reference := "sha256:" + digest
	for _, m := range order {
		_, row, ok, rerr := s.v2MemberRows(ctx, m.key, image, reference, true)
		if rerr != nil {
			return nil, rerr
		}
		if !ok {
			continue
		}
		s.auditVirtualResolve(ctx, p, virtualKey, m.key, dockerImageManifestPath(image, digest))
		return row, nil
	}
	return nil, fmt.Errorf("manifest %s/%s@%s: %w", virtualKey, image, digest, ErrManifestNotFound)
}

// resolveV2VirtualTag is ResolveTag's virtual arm: the first member whose
// tag row AND its manifest row answer wins.
func (s *service) resolveV2VirtualTag(ctx context.Context, p *Principal, virtualKey, image, tag string) (*metadata.DockerTag, error) {
	order, err := s.v2VirtualWalkOrder(ctx, virtualKey)
	if err != nil {
		return nil, err
	}
	for _, m := range order {
		t, terr := s.md.Docker().GetTag(ctx, m.key, image, tag)
		if terr != nil {
			if errors.Is(terr, metadata.ErrTagNotFound) {
				continue
			}
			return nil, fmt.Errorf("tag %s/%s:%s: %w", m.key, image, tag, terr)
		}
		if t.Digest == "" {
			continue
		}
		_, _, ok, rerr := s.v2MemberRows(ctx, m.key, image, "sha256:"+t.Digest, true)
		if rerr != nil {
			return nil, rerr
		}
		if !ok {
			continue
		}
		s.auditVirtualResolve(ctx, p, virtualKey, m.key, dockerImageManifestPath(image, t.Digest))
		return t, nil
	}
	return nil, fmt.Errorf("tag %s/%s:%s: %w", virtualKey, image, tag, ErrTagNotFound)
}

// listV2VirtualTags is ListTags' virtual arm: the member tag UNION —
// first-seen wins on a tag name both members carry (the two-bucket order,
// S13's first-wins semantics) — sorted globally and sliced by the official
// pagination contract. The unknown-image / tagless-image distinction keeps
// the local plane's rule: no member knowing the image is NAME_UNKNOWN, an
// image whose members hold manifest rows but no matching tags the empty
// page. Remote members contribute their CACHED rows only (T-363 D-2 — no
// upstream tags/list proxying).
func (s *service) listV2VirtualTags(ctx context.Context, virtualKey, image string, n int, last string) ([]*metadata.DockerTag, error) {
	order, err := s.v2VirtualWalkOrder(ctx, virtualKey)
	if err != nil {
		return nil, err
	}
	var merged []*metadata.DockerTag
	seen := make(map[string]bool)
	sawImage := false
	for _, m := range order {
		tags, terr := s.md.Docker().ListTagsByImage(ctx, m.key, image)
		if terr != nil {
			return nil, fmt.Errorf("tags of %s/%s (member %s): %w", virtualKey, image, m.key, terr)
		}
		for _, t := range tags {
			if seen[t.Tag] {
				continue
			}
			seen[t.Tag] = true
			merged = append(merged, t)
		}
		if len(tags) > 0 {
			sawImage = true
			continue
		}
		rows, merr := s.md.Docker().ListManifestsByImage(ctx, m.key, image)
		if merr != nil {
			return nil, fmt.Errorf("manifests of %s/%s (member %s): %w", virtualKey, image, m.key, merr)
		}
		if len(rows) > 0 {
			sawImage = true
		}
	}
	if len(merged) == 0 && !sawImage {
		return nil, fmt.Errorf("image %s/%s: %w", virtualKey, image, ErrImageNotFound)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Tag < merged[j].Tag })
	return sliceAfterCursor(merged, n, last, func(t *metadata.DockerTag) string { return t.Tag }), nil
}

// listV2VirtualImages is ListImages' virtual arm: the union of the
// members' images, rendered under the VIRTUAL key (the catalog names the
// addressed surface, not the members), sorted and sliced by the official
// pagination contract.
func (s *service) listV2VirtualImages(ctx context.Context, virtualKey string, n int, after string) ([]string, error) {
	order, err := s.v2VirtualWalkOrder(ctx, virtualKey)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool)
	for _, m := range order {
		images, ierr := s.md.Docker().ListImages(ctx, m.key, "", 0)
		if ierr != nil {
			return nil, fmt.Errorf("catalog of member %s of %s: %w", m.key, virtualKey, ierr)
		}
		for _, img := range images {
			set[img] = true
		}
	}
	names := make([]string, 0, len(set))
	for img := range set {
		names = append(names, img)
	}
	sort.Strings(names)
	if after != "" {
		i := sort.SearchStrings(names, after)
		if i < len(names) && names[i] == after {
			i++ // exclusive cursor
		}
		names = names[i:]
	}
	if n > 0 && len(names) > n {
		names = names[:n]
	}
	out := make([]string, len(names))
	for i, img := range names {
		out[i] = virtualKey + "/" + img
	}
	return out, nil
}

// v2VirtualWalkOrder is the virtual arms' shared preamble: the family gate
// plus the fresh member order.
func (s *service) v2VirtualWalkOrder(ctx context.Context, virtualKey string) ([]virtualMember, error) {
	if _, err := s.loadV2VirtualRepo(ctx, virtualKey); err != nil {
		return nil, err
	}
	return s.virtualMemberOrder(ctx, virtualKey)
}
