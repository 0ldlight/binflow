package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The registry-v2 remote pull-through's SERVICE half (M13 T-363, FR-116.1).
//
// The OCI Distribution upstream conversation — Accept negotiation, the
// 401/WWW-Authenticate Bearer token exchange, tag-to-digest resolution —
// cannot be expressed by the generic pull-through engine's path-joined
// fetch (internal/remote answers one plain GET per storage path with the
// repository's static credential). The docker adapter therefore drives the
// upstream session itself, over the engine's EXPORTED outbound client (the
// same NFR-S13 chain: guarded dials, per-hop re-screening, timeouts,
// retries). The CACHE half stays service-owned through the RemoteV2Plane
// seam below, running the same invariants the engine's land() owns —
// blob-first commit protocol, checksum-addressed blob dedup, remote_cache
// TTL rows, the GC hold pairing — so the two cache writers can never
// disagree about what a cached copy is. helm.md section 8.3 (the K51
// increment) is the behavior spec of the whole plane.

// Compile-time pin: the service implements the adapter-facing capability.
var _ RemoteV2Plane = (*service)(nil)

// loadV2ReadRepo resolves repoKey and admits the registry-v2 family's READ
// plane classes: LOCAL (the push plane), REMOTE since T-363 (the
// pull-through — a cached manifest/tag row is resolution state like any
// local row, and the tags/list + catalog faces read the same tables) and
// VIRTUAL since T-365 (the member aggregation — the four read use cases
// walk the member order, and the tags/list + catalog faces read the
// unions).
func (s *service) loadV2ReadRepo(ctx context.Context, repoKey string) (*metadata.Repo, error) {
	r, err := s.loadRepoRow(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	switch r.Type {
	case TypeLocal, TypeRemote, TypeVirtual:
	default:
		return nil, fmt.Errorf("%w: %s repositories are not served by the registry v2 read plane",
			ErrRepoTypeNotSupported, r.Type)
	}
	if !isV2PlaneFamily(r.PackageType) {
		return nil, fmt.Errorf("repo %q: %w: package type is %q, not one of the registry v2 family (%s, %s)",
			repoKey, ErrRepoTypeNotSupported, r.PackageType, PackageDocker, PackageHelmOCI)
	}
	return r, nil
}

// loadRemoteV2Repo is the shared gate of the RemoteV2Plane seams: a REMOTE
// registry-v2 family repository the principal may READ. The /v2 route gate
// has already answered the endpoint's own scope question; this re-check is
// the defense-in-depth posture of service.Get (an unauthorized principal
// must not be able to aim BinFlow at upstream URLs through a narrower
// seam).
func (s *service) loadRemoteV2Repo(ctx context.Context, p *Principal, repoKey, path string) (*metadata.Repo, error) {
	r, err := s.loadRepoRow(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	if r.Type != TypeRemote {
		return nil, fmt.Errorf("%w: %s repositories have no v2 remote pull-through", ErrRepoTypeNotSupported, r.Type)
	}
	if !isV2PlaneFamily(r.PackageType) {
		return nil, fmt.Errorf("repo %q: %w: package type is %q, not one of the registry v2 family (%s, %s)",
			repoKey, ErrRepoTypeNotSupported, r.PackageType, PackageDocker, PackageHelmOCI)
	}
	if !s.allow(ctx, p, repoKey, path, ActionRead) {
		if p == nil {
			return nil, fmt.Errorf("read %s/%s: %w", repoKey, path, ErrUnauthorized)
		}
		return nil, fmt.Errorf("read %s/%s: %w", repoKey, path, ErrForbidden)
	}
	return r, nil
}

// RemoteUpstream implements RemoteV2Plane: the upstream connection facts of
// one registry-v2 remote repository, password DECRYPTED for the adapter's
// session (in-memory only — never logged, never echoed; the at-rest form
// stays the enc:v1 sealed row). The policy fields mirror the engine's own
// resolution order (014 row column, canonical JSON, product default) so the
// adapter's egress and the engine's can never diverge on timeouts.
func (s *service) RemoteUpstream(ctx context.Context, p *Principal, repoKey string) (*RemoteUpstream, error) {
	if _, err := s.loadRemoteV2Repo(ctx, p, repoKey, ""); err != nil {
		return nil, err
	}
	return s.remoteUpstreamCore(ctx, repoKey)
}

// remoteUpstreamCore is the fact assembly without the permission gate (the
// virtual seam's member-scoped twin, T-365 — membership guards it instead).
func (s *service) remoteUpstreamCore(ctx context.Context, repoKey string) (*RemoteUpstream, error) {
	cfg, err := s.md.Remote().GetConfig(ctx, repoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRemoteConfigNotFound) {
			return nil, fmt.Errorf("remote %s: no remote_configs row (the create crash window; update the repository config to heal): %w", repoKey, err)
		}
		return nil, fmt.Errorf("remote %s: load config: %w", repoKey, err)
	}
	row, rerr := s.md.Repos().Get(ctx, repoKey)
	if rerr != nil {
		return nil, fmt.Errorf("remote %s: load repository row: %w", repoKey, rerr)
	}
	// The policy slice the engine reads (repoPolicy's fields, read through
	// this package's own canonical spelling — the same JSON both consume).
	pol := remoteConfig{MissedRetrievalCachePeriodSecs: defaultMissedRetrievalCachePeriodSecs}
	if row.Config != "" {
		if jerr := json.Unmarshal([]byte(row.Config), &pol); jerr != nil {
			return nil, fmt.Errorf("remote %s: config policy: %w", repoKey, jerr)
		}
		if pol.MissedRetrievalCachePeriodSecs == 0 {
			pol.MissedRetrievalCachePeriodSecs = defaultMissedRetrievalCachePeriodSecs
		}
	}
	password := ""
	if cfg.Password != "" && s.cipher != nil {
		plain, _, derr := s.cipher.Decrypt(cfg.Password)
		if derr != nil {
			return nil, fmt.Errorf("remote %s: credentials: %w", repoKey, derr)
		}
		password = plain
	}
	contentTTL := cfg.ContentTTLSeconds
	if contentTTL <= 0 {
		contentTTL = defaultRetrievalCachePeriodSecs
	}
	return &RemoteUpstream{
		URL:                  cfg.URL,
		Username:             cfg.Username,
		Password:             password,
		TokenAuth:            pol.EnableTokenAuthentication,
		AllowPrivateUpstream: cfg.AllowPrivateUpstream,
		SocketTimeoutMs:      effectiveV2SocketTimeoutMs(cfg, pol),
		ContentTTLSeconds:    contentTTL,
		MissedTTLSeconds:     pol.MissedRetrievalCachePeriodSecs,
		BlockedOut:           cfg.BlockedOut,
	}, nil
}

// effectiveV2SocketTimeoutMs mirrors internal/remote's
// effectiveSocketTimeoutMs resolution order (row column, ms JSON field,
// legacy secs JSON field, 15s product default) for the adapter-owned
// session — one order, two consumers, no drift.
func effectiveV2SocketTimeoutMs(cfg *metadata.RemoteConfig, pol remoteConfig) int64 {
	switch {
	case cfg != nil && cfg.SocketTimeoutMs > 0:
		return cfg.SocketTimeoutMs
	case pol.SocketTimeoutMillis > 0:
		return pol.SocketTimeoutMillis
	case pol.SocketTimeoutSecs > 0:
		return pol.SocketTimeoutSecs * 1000
	default:
		return 15000
	}
}

// ProbeRemoteCache implements RemoteV2Plane: the read-only lookup half of
// the pull-through state machine (the engine's steps 3 and 4 — negative
// cache, then the TTL-classed local copy), WITHOUT any upstream contact.
// The adapter drives the upstream leg itself and needs exactly this split
// to decide HIT / STALE / negative / miss before it opens a connection.
// A HIT or STALE outcome carries the download audit row (the serve that
// follows is the event; a MISS audits at landing).
func (s *service) ProbeRemoteCache(ctx context.Context, p *Principal, repoKey, path string) (*RemoteProbe, error) {
	if err := validateNodePath(path); err != nil {
		return nil, err
	}
	if _, err := s.loadRemoteV2Repo(ctx, p, repoKey, path); err != nil {
		return nil, err
	}
	probe, err := s.probeRemoteV2Core(ctx, repoKey, path)
	if err != nil {
		return nil, err
	}
	if probe.Node != nil {
		s.audit(ctx, AuditEvent{Actor: actor(p), Action: AuditActionDownload, Repo: repoKey, Path: path})
	}
	return probe, nil
}

// probeRemoteV2Core is the probe without the permission gate and without
// the audit row: the virtual aggregation seam (T-365) runs the same state
// machine against a MEMBER (membership-guarded, ungated — the walk audits
// one download row addressed to the VIRTUAL key with the resolvedFrom
// detail, getVirtual's posture), so the core is the one place the negative
// window and the TTL classes are decided.
func (s *service) probeRemoteV2Core(ctx context.Context, repoKey, path string) (*RemoteProbe, error) {
	if entry, err := s.md.Remote().GetCache(ctx, repoKey, path); err == nil &&
		entry.Kind == remoteCacheKindNegative() && remoteEntryFresh(entry.ExpiresAt, s.nowFn()) {
		return &RemoteProbe{State: RemoteProbeNegative}, nil
	}
	node, err := s.md.Nodes().Get(ctx, repoKey, path)
	if err != nil && !errors.Is(err, metadata.ErrNodeNotFound) {
		return nil, fmt.Errorf("remote %s: cache node %s: %w", repoKey, path, err)
	}
	if err == nil && node != nil && node.Sha256 != "" {
		state := RemoteProbeStale
		if entry, cerr := s.md.Remote().GetCache(ctx, repoKey, path); cerr == nil && remoteEntryFresh(entry.ExpiresAt, s.nowFn()) {
			state = RemoteProbeHit
		}
		return &RemoteProbe{Node: node, State: state}, nil
	}
	return &RemoteProbe{State: RemoteProbeMiss}, nil
}

// LandRemoteBlob implements RemoteV2Plane: land one upstream-fetched body
// checksum-addressed, mirroring the engine's land() invariant chain —
// storage session commit against the EXPECTED digest (the digest-keyed
// layout paths make the sha256 load-bearing: a mismatched body must never
// be attributed to a digest it is not), then the blob row, then the node
// (first-seen provenance preserved on a same-digest refetch), then the
// content-kind TTL cache row, then the GC-hold release strictly after the
// rows committed ([M9] ADR-0031's W-1 ordering). The caller has already
// verified the fetch (status, content type); this method owns the landing
// only. On success the download audit row records the miss-serve.
func (s *service) LandRemoteBlob(ctx context.Context, p *Principal, repoKey, path, expectHex, mime string, body io.Reader) (*metadata.Node, error) {
	if err := validateNodePath(path); err != nil {
		return nil, err
	}
	if err := validateDigest(expectHex); err != nil {
		return nil, err
	}
	if _, err := s.loadRemoteV2Repo(ctx, p, repoKey, path); err != nil {
		return nil, err
	}
	return s.landRemoteV2Core(ctx, p, repoKey, path, expectHex, mime, body)
}

// landRemoteV2Core is the landing chain without the permission gate (the
// probeRemoteV2Core split, T-365): the virtual aggregation seam lands into
// a MEMBER with the same invariants — membership replaces the member's own
// permission pair as the guard.
func (s *service) landRemoteV2Core(ctx context.Context, p *Principal, repoKey, path, expectHex, mime string, body io.Reader) (*metadata.Node, error) {
	now := s.now()
	committed, err := s.commitBlob(ctx, body, storage.BlobRef{Sha256: expectHex})
	if err != nil {
		return nil, err
	}
	if err := s.md.Blobs().Put(ctx, &metadata.Blob{
		Sha256: committed.Sha256, Sha1: committed.Sha1, Md5: committed.Md5, Size: committed.Size, CreatedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("remote %s: cache blob row %s: %w", repoKey, committed.Sha256, err)
	}
	// First-seen provenance (the idempotent-retransmit rule of
	// repo-semantics section 3, the engine's own landing posture).
	node := &metadata.Node{
		RepoKey: repoKey, Path: path, Sha256: committed.Sha256, Size: committed.Size,
		Mime: mime, CreatedBy: "remote-proxy", CreatedAt: now, UpdatedAt: now,
	}
	if existing, gerr := s.md.Nodes().Get(ctx, repoKey, path); gerr == nil && existing != nil {
		if existing.Sha256 == committed.Sha256 {
			node.CreatedBy = existing.CreatedBy
			node.CreatedAt = existing.CreatedAt
		}
		if node.Mime == "" {
			node.Mime = existing.Mime
		}
	}
	if err := s.md.Nodes().Put(ctx, node); err != nil {
		return nil, fmt.Errorf("remote %s: cache node %s: %w", repoKey, path, err)
	}
	ttl := s.remoteContentTTL(ctx, repoKey)
	if err := s.md.Remote().PutCache(ctx, &metadata.RemoteCacheEntry{
		RepoKey: repoKey, Path: path, Kind: metadata.RemoteCacheKindContent,
		FetchedAt: now, ExpiresAt: remoteExpiry(s.nowFn(), ttl),
	}); err != nil {
		return nil, fmt.Errorf("remote %s: cache state %s: %w", repoKey, path, err)
	}
	s.releaseGCHold(ctx, committed.Sha256)
	s.audit(ctx, AuditEvent{Actor: actor(p), Action: AuditActionDownload, Repo: repoKey, Path: path})
	return node, nil
}

// CacheRemoteMiss implements RemoteV2Plane: the negative-cache write of the
// engine's step 5 (an upstream 404 answers from the miss record inside its
// window, zero upstream packets). Digest-keyed paths only — a TAG miss has
// no storage path to key and simply answers 404 (helm.md 8.3).
func (s *service) CacheRemoteMiss(ctx context.Context, p *Principal, repoKey, path string) error {
	if err := validateNodePath(path); err != nil {
		return err
	}
	if _, err := s.loadRemoteV2Repo(ctx, p, repoKey, path); err != nil {
		return err
	}
	return s.cacheRemoteMissCore(ctx, repoKey, path)
}

// cacheRemoteMissCore is the negative-cache write without the permission
// gate (the virtual seam's member-scoped twin, T-365).
func (s *service) cacheRemoteMissCore(ctx context.Context, repoKey, path string) error {
	if err := s.md.Remote().PutCache(ctx, &metadata.RemoteCacheEntry{
		RepoKey: repoKey, Path: path, Kind: remoteCacheKindNegative(),
		FetchedAt: s.now(), ExpiresAt: remoteExpiry(s.nowFn(), s.remoteMissedTTL(ctx, repoKey)),
	}); err != nil {
		return fmt.Errorf("remote %s: negative cache %s: %w", repoKey, path, err)
	}
	return nil
}

// RecordRemoteManifest implements RemoteV2Plane: the cache-population write
// of the docker index rows after a manifest landed — the manifest row, the
// tag pointer (a tag request records its tag; a digest request records
// none) and the best-effort ref edges. No permission or governance gates
// run here: this is remote cache state the READ gate already admitted (the
// engine's land() writes node rows with the same posture), not a deploy.
func (s *service) RecordRemoteManifest(ctx context.Context, p *Principal, repoKey, image, digest, tag, mediaType string, size int64, refs []*metadata.DockerRef) error {
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
		return fmt.Errorf("manifest %s/%s@%s: %w: media type is empty", repoKey, image, digest, ErrInvalidManifest)
	}
	if size < 0 {
		return fmt.Errorf("manifest %s/%s@%s: %w: negative size", repoKey, image, digest, ErrInvalidManifest)
	}
	for i, r := range refs {
		if r == nil {
			return fmt.Errorf("manifest %s/%s@%s: %w: ref %d is nil", repoKey, image, digest, ErrInvalidManifest, i)
		}
		if err := validateDigest(r.BlobDigest); err != nil {
			return fmt.Errorf("manifest %s/%s@%s ref %d: %w", repoKey, image, digest, i, err)
		}
	}
	if _, err := s.loadRemoteV2Repo(ctx, p, repoKey, dockerPermPath(image)); err != nil {
		return err
	}
	return s.recordRemoteManifestCore(ctx, repoKey, image, digest, tag, mediaType, size, refs)
}

// recordRemoteManifestCore is the index-row write without the permission
// gate (the virtual seam's member-scoped twin, T-365).
func (s *service) recordRemoteManifestCore(ctx context.Context, repoKey, image, digest, tag, mediaType string, size int64, refs []*metadata.DockerRef) error {
	now := s.now()
	if err := s.md.Docker().PutManifest(ctx, &metadata.DockerManifest{
		RepoKey: repoKey, Image: image, Digest: digest, MediaType: mediaType,
		Size: size, CreatedBy: "remote-proxy", CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("remote manifest row %s/%s@%s: %w", repoKey, image, digest, err)
	}
	if tag != "" {
		if err := s.md.Docker().PutTag(ctx, &metadata.DockerTag{
			RepoKey: repoKey, Image: image, Tag: tag, Digest: digest,
			UpdatedBy: "remote-proxy", UpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("remote tag row %s/%s:%s: %w", repoKey, image, tag, err)
		}
	}
	if len(refs) > 0 {
		if err := s.md.Docker().PutRefs(ctx, repoKey, image, digest, refs); err != nil {
			return fmt.Errorf("remote ref rows %s/%s@%s: %w", repoKey, image, digest, err)
		}
	}
	return nil
}

// remoteContentTTL resolves the repository's content TTL off its config row
// (the create-time canonicalization writes the product default; hand-mangled
// rows fall back to it here too).
func (s *service) remoteContentTTL(ctx context.Context, repoKey string) int64 {
	if cfg, err := s.md.Remote().GetConfig(ctx, repoKey); err == nil && cfg != nil && cfg.ContentTTLSeconds > 0 {
		return cfg.ContentTTLSeconds
	}
	return defaultRetrievalCachePeriodSecs
}

// remoteMissedTTL resolves the negative-cache window off the repository
// row's policy JSON (the engine's defaultPolicy posture: 1800s unless the
// canonical config says otherwise).
func (s *service) remoteMissedTTL(ctx context.Context, repoKey string) int64 {
	row, err := s.md.Repos().Get(ctx, repoKey)
	if err != nil || row == nil || row.Config == "" {
		return defaultMissedRetrievalCachePeriodSecs
	}
	pol := remoteConfig{MissedRetrievalCachePeriodSecs: defaultMissedRetrievalCachePeriodSecs}
	if jerr := json.Unmarshal([]byte(row.Config), &pol); jerr != nil {
		return defaultMissedRetrievalCachePeriodSecs
	}
	if pol.MissedRetrievalCachePeriodSecs == 0 {
		return defaultMissedRetrievalCachePeriodSecs
	}
	return pol.MissedRetrievalCachePeriodSecs
}

// remoteCacheKindNegative is this package's spelling of the miss record's
// kind (internal/remote's cacheKindNegative is package-private; the column
// is free text to the store, and the value is pinned equal by test).
func remoteCacheKindNegative() string { return "negative" }

// remoteEntryFresh mirrors internal/remote's entryFresh (RFC3339
// lexicographic-chronological clock).
func remoteEntryFresh(expiresAt string, now time.Time) bool {
	if expiresAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return false
	}
	return now.Before(t)
}

// remoteExpiry mirrors internal/remote's expiry helper.
func remoteExpiry(now time.Time, ttlSeconds int64) string {
	if ttlSeconds <= 0 {
		return now.UTC().Format(time.RFC3339)
	}
	return now.UTC().Add(time.Duration(ttlSeconds) * time.Second).Format(time.RFC3339)
}
