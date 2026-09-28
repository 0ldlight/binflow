package remote

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The <key>-cache projection registry (virtual-four-bucket section 4, the
// four-bucket resolution's remote-side base): every remote repository IMPLIES
// a shadow "<key>-cache" repository that resolution may address as a
// local-bucket participant. It is a DERIVED VIEW, never a stored entity —
// no repositories row, no GET /api/repositories entry, no node/bloom/quota
// storage surface of its own (the cached bytes stay in the remote key's
// namespace, fetcher.go untouched). Behavior spec:
// docs/reverse/remote-cache-projection.md section 1.

// CacheSuffix is the single source of the projection key suffix: a remote
// repository "<K>" projects the repository "<K>-cache" (constant
// concatenation, no configurable form — remote-cache-projection.md 1.1
// "naming rules"). internal/repo consumes it for the create/update guard
// that refuses any user-supplied key ending in the suffix (spec 1.3).
const CacheSuffix = "-cache"

// CacheProjection is the derived <key>-cache repository descriptor of one
// remote repository: a resolution-layer view, NOT a persisted repository row
// and NOT a storage namespace (design virtual-four-bucket section 4).
//
// Field derivation, anchored to remote-cache-projection.md section 1.2:
//
//   - PackageType, RepoLayout, PriorityResolution, HandleReleases,
//     HandleSnapshots, ArchiveBrowsing, BlackedOut: item-by-item mirrors of
//     the remote's own fields (single source = the loaded remote row); a
//     remote config change re-derives on the next call.
//   - Two fields of the reference's projection are FIXED, not inherited, and
//     deliberately carry no struct seat (the consumers treat them as
//     constants; speculative seats would be guessing past the spec):
//     checksum policy is always client-checksums ("the client's declared
//     checksum must match the measured one or the write is refused"), and
//     snapshot version behavior is always unique (cached Maven snapshots
//     land on their unique timestamped paths, no deployer/nonnull
//     transformation).
//   - Key: remoteKey + CacheSuffix.
//   - Timestamps/description mirror the remote (spec 1.1); not carried here
//     — no BinFlow consumer reads them off the projection today.
type CacheProjection struct {
	// Key is remoteKey + CacheSuffix.
	Key string
	// PackageType mirrors the remote's package type.
	PackageType string
	// RepoLayout mirrors the remote's repoLayoutRef.
	RepoLayout string
	// PriorityResolution is inherited (section 1.2): the four-bucket order's
	// cache facet rides its remote's mark.
	PriorityResolution bool
	// HandleReleases is inherited (section 1.2).
	HandleReleases bool
	// HandleSnapshots is inherited (section 1.2).
	HandleSnapshots bool
	// ArchiveBrowsing is inherited (archiveBrowsingEnabled, section 1.2).
	ArchiveBrowsing bool
	// BlackedOut is inherited (section 1.2).
	BlackedOut bool
}

// ProjectionRegistry derives cache projections off the loaded remote rows.
// Implementations rebuild on config reload — the projection is never a
// stored entity (remote-cache-projection.md 1.1 "projection is not a
// persistent entity"; the reference rebuilds its repository cache on every
// config reload). BinFlow's reading face is a fresh store read per call, so
// the engine's implementation is rebuilt-by-construction: a config change is
// visible on the very next call, and nothing outlives the remote row.
type ProjectionRegistry interface {
	CacheProjection(ctx context.Context, remoteKey string) (CacheProjection, bool)
}

// The engine is the default registry implementation.
var _ ProjectionRegistry = (*Engine)(nil)

// defaultProjectionLayoutRef is the repoLayoutRef default the remote
// canonical write plane injects on every create/update (repo.service's
// parseRemoteConfig → maven-2-default, the artifactory.xsd default). The
// import would cycle, so — like repoPolicy's field spellings — this literal
// MUST stay in sync with repo.defaultRepoLayoutRef. It only fires for rows
// that predate the canonical form or were hand-mangled: every canonical row
// carries the key.
const defaultProjectionLayoutRef = "maven-2-default"

// projectionConfig probes the repo-level fields of a remote row's canonical
// config JSON (repositories.config). Field spellings MUST stay in sync with
// repo.remoteConfig's tags — the cycle rule again (see repoPolicy).
//
// Pointer seats keep an explicit false distinct from absent:
//
//   - handleReleases/handleSnapshots: absent = true, the Artifactory default
//     the v1 remote echo renders ({"handleReleases", true}). The remote
//     canonical struct does not persist them today, so current rows read
//     true/true — the projection mirrors whatever the row carries the day
//     the seats land.
//   - storeArtifactsLocally: the projection GATE (spec 1.1 "projection
//     conditions" — false means the remote has NO <key>-cache repository at
//     all). BinFlow wires the knob as a render-only default (always true;
//     the engine has no non-persisting branch), so the gate is a seam that
//     cannot fire off today's storage. Absent = true.
type projectionConfig struct {
	PriorityResolution     bool   `json:"priorityResolution"`
	RepoLayoutRef          string `json:"repoLayoutRef"`
	BlackedOut             bool   `json:"blackedOut"`
	ArchiveBrowsingEnabled bool   `json:"archiveBrowsingEnabled"`
	HandleReleases         *bool  `json:"handleReleases"`
	HandleSnapshots        *bool  `json:"handleSnapshots"`
	StoreArtifactsLocally  *bool  `json:"storeArtifactsLocally"`
}

// CacheProjection derives one remote repository's <key>-cache projection
// off its repositories row. ok=false when the key is not a loaded remote
// repository, when the row is not remote-typed, or when the gate is off
// (storeArtifactsLocally=false — the seam above). Read-only over the store:
// a projection is never written anywhere.
func (e *Engine) CacheProjection(ctx context.Context, remoteKey string) (CacheProjection, bool) {
	row, err := e.md.Repos().Get(ctx, remoteKey)
	if err != nil {
		if !errors.Is(err, metadata.ErrRepoNotFound) {
			// A store fault must not fail resolution: the interface has no
			// error seat, so the member degrades to "no projection" (its
			// remote-self step still runs) and the fault leaves a trace.
			// The message carries the store error, never a credential.
			e.log.WarnContext(ctx, "remote: cache projection lookup failed",
				"repo", remoteKey, "error", err.Error())
		}
		return CacheProjection{}, false
	}
	if row.Type != "remote" {
		return CacheProjection{}, false
	}
	pc := projectionConfig{
		// Value seats default before the probe so a non-decoding row reads
		// the same defaults as an empty one (the memberPriorityResolution
		// posture: hand-mangled rows answer the product default).
		RepoLayoutRef: defaultProjectionLayoutRef,
	}
	if row.Config != "" {
		if err := json.Unmarshal([]byte(row.Config), &pc); err != nil {
			e.log.WarnContext(ctx, "remote: cache projection: config blob not decodable — defaults assumed",
				"repo", remoteKey, "error", err.Error())
			pc = projectionConfig{RepoLayoutRef: defaultProjectionLayoutRef}
		}
	}
	if pc.StoreArtifactsLocally != nil && !*pc.StoreArtifactsLocally {
		// storeArtifactsLocally=false: the remote has no cache projection
		// (spec 1.1). Unreachable off today's always-true wiring — kept as
		// the gate seat for the day the knob becomes stored state.
		return CacheProjection{}, false
	}
	proj := CacheProjection{
		Key:                remoteKey + CacheSuffix,
		PackageType:        row.PackageType,
		RepoLayout:         pc.RepoLayoutRef,
		PriorityResolution: pc.PriorityResolution,
		ArchiveBrowsing:    pc.ArchiveBrowsingEnabled,
		BlackedOut:         pc.BlackedOut,
		HandleReleases:     true, // absent = true (see projectionConfig)
		HandleSnapshots:    true,
	}
	if pc.HandleReleases != nil {
		proj.HandleReleases = *pc.HandleReleases
	}
	if pc.HandleSnapshots != nil {
		proj.HandleSnapshots = *pc.HandleSnapshots
	}
	return proj, true
}
