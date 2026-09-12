package remote

// The per-package-type content-TTL defaults (remote-cache-v2 §5.1 / the
// cache-v2 ADR decision ③, revising ADR-0012 erratum two ④'s flat 7200):
// docker and helmoci remote repositories default their content-class
// retrieval window to 21600s — Artifactory's
// DEFAULT_DOCKER_REMOTE_RETRIEVAL_CACHE_PERIOD (E3-4) — every other
// package type keeps the generic 7200s. The three consumption clauses the
// ADR pins:
//
//   - explicit wins: a remote_configs row with content_ttl_seconds > 0 is
//     a legal explicit value and is never rewritten (存量不回改 — existing
//     rows keep their 7200);
//   - unset resolves per type: a row with 0 takes its package-type default
//     HERE — the single resolution point;
//   - one resolver, two consumers: the repo service's create-time default
//     write and the engine's fetch TTL both route through this file (the
//     effectiveV2SocketTimeoutMs "one order, two consumers" precedent).
//
// The package-type spellings mirror the repo package's constants (the
// pkgTypeHelm precedent — importing internal/repo would be a cycle).

const (
	// pkgTypeDocker mirrors repo.PackageDocker.
	pkgTypeDocker = "docker"
	// pkgTypeHelmOCI mirrors repo.PackageHelmOCI.
	pkgTypeHelmOCI = "helmoci"
)

const (
	// GenericContentTTLSeconds is the content-class default of every
	// package type without a specific one (ADR-0012 erratum two ④).
	GenericContentTTLSeconds int64 = 7200
	// DockerRemoteContentTTLSeconds is the docker/helmoci content-class
	// default (remote-cache-v2 §5.1; Artifactory E3-4).
	DockerRemoteContentTTLSeconds int64 = 21600
)

// DefaultContentTTLSecondsFor returns the package type's content-class
// TTL default — the create-time write value and the unset-row resolution
// value, one source for both.
func DefaultContentTTLSecondsFor(packageType string) int64 {
	switch packageType {
	case pkgTypeDocker, pkgTypeHelmOCI:
		return DockerRemoteContentTTLSeconds
	default:
		return GenericContentTTLSeconds
	}
}

// ResolveContentTTLSeconds is the single resolution point: an explicit
// value (> 0) wins unchanged; an unset row (<= 0) takes the package
// type's default.
func ResolveContentTTLSeconds(explicit int64, packageType string) int64 {
	if explicit > 0 {
		return explicit
	}
	return DefaultContentTTLSecondsFor(packageType)
}
