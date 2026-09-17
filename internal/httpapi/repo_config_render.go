package httpapi

// L025-6 (D02 config four-layer key face, rest-api.md // 2.1.9): the
// reference-measured full-key renderers for the repository configuration
// read family. Four distinct projections, each pinned to a live-wire key
// set (reports/compatibility/l025q-wire + the L025-6 reference probes):
//
//	configurations face   local 18 / remote 46 / virtual 12 keys (rclass)
//	v1 GET + v2 batch     local 61 / remote 102 / virtual 50 keys (rclass)
//	v2 single GET         local 18 / remote 46 / virtual 12 keys (type)
//	non-admin partial     {key,packageType,description} + rclass/type,
//	                      plus url (remote) / repositories (virtual)
//
// Every seat carries the reference's MEASURED default for repositories
// whose stored config lacks the field (BinFlow's caller-owned local blob
// keeps only what a PUT carried). Unmodeled keys (xrayIndex,
// enableComposerSupport, hexPublicKey ...) render their wire default
// shapes verbatim -- key-NAME sets are the compat contract; value
// divergences on instance-specific keys (hexPublicKey's PEM) are
// registered in the L025-6 report. The stored contentSynchronisation uses
// BinFlow's flat spellings and is reshaped to the reference's nested form
// on the way out.

import (
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// cfgSeat is one wire key with its reference-measured default.
type cfgSeat struct {
	key string
	def any
}

var v1LocalSeats = []cfgSeat{
	{"archiveBrowsingEnabled", false},
	{"blackedOut", false},
	{"blockPushingSchema1", true},
	{"calculateYumMetadata", false},
	{"cargoAnonymousAccess", false},
	{"cargoInternalIndex", false},
	{"cdnRedirect", false},
	{"checksumPolicyType", "client-checksums"},
	{"ddebSupported", false},
	{"debianTrivialLayout", false},
	{"dockerApiVersion", "V2"},
	{"dockerProjectId", ""},
	{"dockerTagRetention", 1},
	{"downloadRedirect", false},
	{"enableBowerSupport", false},
	{"enableChefSupport", false},
	{"enableCocoaPodsSupport", false},
	{"enableComposerSupport", false},
	{"enableComposerV1Indexing", false},
	{"enableConanSupport", false},
	{"enableDebianSupport", false},
	{"enableDistRepoSupport", false},
	{"enableDockerSupport", false},
	{"enableFileListsIndexing", false},
	{"enableGemsSupport", false},
	{"enableGitLfsSupport", false},
	{"enableNormalizedVersion", false},
	{"enableNpmSupport", false},
	{"enableNuGetSupport", false},
	{"enablePuppetSupport", false},
	{"enablePypiSupport", false},
	{"enableVagrantSupport", false},
	{"encryptStates", true},
	{"excludesPattern", ""},
	{"forceConanAuthentication", false},
	{"forceMetadataNameVersion", false},
	{"forceNonDuplicateChart", false},
	{"forceNugetAuthentication", false},
	{"forceP2Authentication", false},
	{"handleReleases", true},
	{"handleSnapshots", true},
	{"includesPattern", "**/*"},
	{"maxUniqueSnapshots", 0},
	{"maxUniqueTags", 0},
	{"mlRepoLayout", false},
	{"notes", ""},
	{"priorityResolution", false},
	{"propertySets", []any{}},
	{"repoLayoutRef", "maven-2-default"},
	{"signedUrlTtl", 90},
	{"snapshotVersionBehavior", "unique"},
	{"suppressPomConsistencyChecks", false},
	{"terraformType", "MODULE"},
	{"xrayDataTtl", 90},
	{"xrayIndex", false},
	{"yumRootDepth", 0},
}

var v1RemoteSeats = []cfgSeat{
	{"allowAnyHostAuth", false},
	{"archiveBrowsingEnabled", false},
	{"assumedOfflinePeriodSecs", 300},
	{"authUrl", ""},
	{"blackedOut", false},
	{"blockMismatchingMimeTypes", false},
	{"blockPushingSchema1", true},
	{"bowerRegistryUrl", "https://registry.bower.io"},
	{"bypassHeadRequests", false},
	{"cargoAnonymousAccess", false},
	{"cargoInternalIndex", true},
	{"cdnRedirect", false},
	{"composerRegistryUrl", "https://packagist.org"},
	{"curated", false},
	{"customHttpHeaders", []any{}},
	{"ddebSupported", false},
	{"debianTrivialLayout", false},
	{"disableProxy", false},
	{"disableUrlNormalization", false},
	{"dockerApiVersion", "V2"},
	{"dockerProjectId", ""},
	{"downloadRedirect", false},
	{"enableBowerSupport", false},
	{"enableChefSupport", false},
	{"enableCocoaPodsSupport", false},
	{"enableComposerSupport", false},
	{"enableConanSupport", false},
	{"enableCookieManagement", false},
	{"enableDebianSupport", false},
	{"enableDistRepoSupport", false},
	{"enableDockerSupport", false},
	{"enableGemsSupport", false},
	{"enableGitLfsSupport", false},
	{"enableNormalizedVersion", false},
	{"enableNpmSupport", false},
	{"enableNuGetSupport", false},
	{"enablePuppetSupport", false},
	{"enablePypiSupport", false},
	{"enableTokenAuthentication", false},
	{"enableVagrantSupport", false},
	{"excludesPattern", ""},
	{"externalDependenciesEnabled", false},
	{"fetchJarsEagerly", false},
	{"fetchSourcesEagerly", false},
	{"forceConanAuthentication", false},
	{"forceMetadataNameVersion", false},
	{"forceNonDuplicateChart", false},
	{"forceNugetAuthentication", false},
	{"forceP2Authentication", false},
	{"gitLabResolveSubgroups", false},
	{"gitRegistryUrl", "https://index.crates.io"},
	{"handleReleases", true},
	{"handleSnapshots", true},
	{"hardFail", false},
	{"hexPublicKey", ""},
	{"includesPattern", "**/*"},
	{"listRemoteFolderItems", false},
	{"localAddress", ""},
	{"maxUniqueSnapshots", 0},
	{"maxUniqueTags", 0},
	{"metadataRetrievalTimeoutSecs", 60},
	{"missedRetrievalCachePeriodSecs", 1800},
	{"mlRepoLayout", false},
	{"notes", ""},
	{"offline", false},
	{"passThrough", false},
	{"podsCdnUrl", "https://cdn.cocoapods.org"},
	{"podsForceModifyIndex", false},
	{"podsSpecsRepoUrl", "https://github.com/CocoaPods/Specs"},
	{"priorityResolution", false},
	{"propagateQueryParams", false},
	{"propertySets", []any{}},
	{"pyPIRegistryUrl", "https://pypi.org"},
	{"pyPIRepositorySuffix", "simple"},
	{"rejectInvalidJars", false},
	{"remoteRepoChecksumPolicyType", "generate-if-absent"},
	{"repoLayoutRef", "maven-2-default"},
	{"retrievalCachePeriodSecs", 7200},
	{"retrieveSha256FromServer", false},
	{"sendContext", false},
	{"shareConfiguration", false},
	{"signedUrlTtl", 90},
	{"socketTimeoutMillis", 15000},
	{"storeArtifactsLocally", true},
	{"suppressPomConsistencyChecks", false},
	{"synchronizeProperties", false},
	{"terraformProvidersUrl", "https://releases.hashicorp.com"},
	{"terraformRegistryUrl", "https://registry.terraform.io"},
	{"unusedArtifactsCleanupPeriodHours", 0},
	{"url", ""},
	{"username", ""},
	{"vcsGitProvider", "GITHUB"},
	{"vcsType", "GIT"},
	{"xrayDataTtl", 90},
	{"xrayIndex", false},
}

var v1VirtualSeats = []cfgSeat{
	{"artifactoryRequestsCanRetrieveRemoteArtifacts", false},
	{"blockPushingSchema1", true},
	{"cachingLocalForeignLayersEnabled", false},
	{"cargoAnonymousAccess", false},
	{"cargoInternalIndex", true},
	{"ddebSupported", false},
	{"debianTrivialLayout", false},
	{"dockerApiVersion", "V2"},
	{"dockerProjectId", ""},
	{"enableBowerSupport", false},
	{"enableChefSupport", false},
	{"enableCocoaPodsSupport", false},
	{"enableComposerSupport", false},
	{"enableConanSupport", false},
	{"enableDebianSupport", false},
	{"enableDistRepoSupport", false},
	{"enableDockerSupport", false},
	{"enableGemsSupport", false},
	{"enableGitLfsSupport", false},
	{"enableNormalizedVersion", false},
	{"enableNpmSupport", false},
	{"enableNuGetSupport", false},
	{"enablePuppetSupport", false},
	{"enablePypiSupport", false},
	{"enableVagrantSupport", false},
	{"excludesPattern", ""},
	{"externalDependenciesEnabled", false},
	{"forceConanAuthentication", false},
	{"forceMavenAuthentication", false},
	{"forceMetadataNameVersion", false},
	{"forceNonDuplicateChart", false},
	{"forceNugetAuthentication", false},
	{"forceP2Authentication", false},
	{"hideUnauthorizedResources", false},
	{"includesPattern", "**/*"},
	{"keyPair", ""},
	{"mlRepoLayout", false},
	{"notes", ""},
	{"pomRepositoryReferencesCleanupPolicy", "discard_active_reference"},
	{"priorityResolution", false},
	{"repositories", []any{}},
	{"resolveDockerTagsByTimestamp", false},
	{"signedUrlTtl", 90},
	{"useNamespaces", false},
	{"virtualRetrievalCachePeriodSecs", 600},
}

var v2LocalSeats = []cfgSeat{
	{"archiveBrowsingEnabled", false},
	{"blackedOut", false},
	{"cdnRedirect", false},
	{"downloadRedirect", false},
	{"excludesPattern", ""},
	{"includesPattern", "**/*"},
	{"notes", ""},
	{"priorityResolution", false},
	{"propertySets", []any{}},
	{"repoLayoutRef", "maven-2-default"},
	{"signedUrlTtl", 90},
	{"xrayDataTtl", 90},
	{"xrayIndex", false},
}

var v2RemoteSeats = []cfgSeat{
	{"allowAnyHostAuth", false},
	{"archiveBrowsingEnabled", false},
	{"assumedOfflinePeriodSecs", 300},
	{"blackedOut", false},
	{"blockMismatchingMimeTypes", false},
	{"bypassHeadRequests", false},
	{"curated", false},
	{"customHttpHeaders", []any{}},
	{"disableProxy", false},
	{"disableUrlNormalization", false},
	{"downloadRedirect", false},
	{"enableCookieManagement", false},
	{"excludesPattern", ""},
	{"hardFail", false},
	{"includesPattern", "**/*"},
	{"listRemoteFolderItems", false},
	{"localAddress", ""},
	{"metadataRetrievalTimeoutSecs", 60},
	{"missedRetrievalCachePeriodSecs", 1800},
	{"notes", ""},
	{"offline", false},
	{"passThrough", false},
	{"priorityResolution", false},
	{"propagateQueryParams", false},
	{"propertySets", []any{}},
	{"repoLayoutRef", "maven-2-default"},
	{"retrievalCachePeriodSecs", 7200},
	{"retrieveSha256FromServer", false},
	{"sendContext", false},
	{"shareConfiguration", false},
	{"signedUrlTtl", 90},
	{"socketTimeoutMillis", 15000},
	{"storeArtifactsLocally", true},
	{"synchronizeProperties", false},
	{"unusedArtifactsCleanupPeriodHours", 0},
	{"url", ""},
	{"username", ""},
	{"xrayDataTtl", 90},
	{"xrayIndex", false},
}

var v2VirtualSeats = []cfgSeat{
	{"artifactoryRequestsCanRetrieveRemoteArtifacts", false},
	{"excludesPattern", ""},
	{"hideUnauthorizedResources", false},
	{"includesPattern", "**/*"},
	{"notes", ""},
	{"repositories", []any{}},
	{"signedUrlTtl", 90},
}

// renderConfigSeats walks one face's seat list: the row's own columns
// answer key/packageType/description, the stored blob wins for every
// seat it carries, and the reference-measured default fills the rest.
// Password is never blob-sourced (NFR-S14); environments falls back to
// the stored stages alias spelling. The dialect key (rclass or type)
// rides the row's class. Defaults are shared but read-only (json marshal
// only) -- renders never mutate them.
func renderConfigSeats(row *metadata.Repo, blob map[string]any, seats []cfgSeat, dialect string) map[string]any {
	m := make(map[string]any, len(seats)+4)
	m["key"] = row.RepoKey
	m["packageType"] = row.PackageType
	m["description"] = row.Description
	m[dialect] = row.Type
	for _, s := range seats {
		switch s.key {
		case "password":
			m[s.key] = "" // credential never crosses the read plane
		case "environments":
			if v, ok := blob["environments"]; ok && v != nil {
				m[s.key] = v
			} else if v, ok := blob["stages"]; ok && v != nil {
				m[s.key] = v
			} else {
				m[s.key] = s.def
			}
		case "contentSynchronisation":
			if v, ok := blob["contentSynchronisation"]; ok && v != nil {
				m[s.key] = contentSyncWire(v)
			} else {
				m[s.key] = contentSyncWire(nil)
			}
		default:
			if v, ok := blob[s.key]; ok && v != nil {
				m[s.key] = v
			} else {
				m[s.key] = s.def
			}
		}
	}
	// A virtual row's repoLayoutRef is the one conditional key: rendered
	// when set (L006-A live evidence), omitted when not -- the wire's
	// 50/12-key virtual samples are the unset arm.
	if row.Type == repo.TypeVirtual {
		if v, ok := blob["repoLayoutRef"].(string); ok && v != "" {
			m["repoLayoutRef"] = v
		}
	}
	return m
}

// configFaceSeats picks the seat list for one face and rclass. The
// configurations face shares the v2 set (same serializer family, the
// spec's // 2.1.1/2.1.3 note); federated/release_bundle rows cannot
// exist in BinFlow (CreateRepo refuses the rclass), so they fall to the
// local list and stay unreachable.
func configFaceSeats(face, rclass string) []cfgSeat {
	// withEnvironments copies (never appends in place: the backing arrays
	// of the package-level lists are shared across requests).
	withEnvironments := func(seats []cfgSeat, extra ...cfgSeat) []cfgSeat {
		out := make([]cfgSeat, 0, len(seats)+1+len(extra))
		out = append(out, seats...)
		out = append(out, environmentsSeat)
		return append(out, extra...)
	}
	seats := func(v1, v2 []cfgSeat, extra ...cfgSeat) []cfgSeat {
		if face == "v1" {
			return withEnvironments(v1, extra...)
		}
		return withEnvironments(v2, extra...)
	}
	switch rclass {
	case repo.TypeRemote:
		return seats(v1RemoteSeats, v2RemoteSeats, remoteOnlySeats...)
	case repo.TypeVirtual:
		return seats(v1VirtualSeats, v2VirtualSeats)
	default:
		return seats(v1LocalSeats, v2LocalSeats)
	}
}

// environmentsSeat is appended by configFaceSeats (every measured face
// carries the Stage domain's environments key; the renderer's alias
// fallback to the stored stages spelling rides the special case).
var environmentsSeat = cfgSeat{"environments", []any{}}

// remoteOnlySeats: password (never blob-sourced, NFR-S14) and
// contentSynchronisation (the nested reshape) ride both remote faces —
// their seats live here because the generator's transcription special-
// cases them with the row-sourced keys.
var remoteOnlySeats = []cfgSeat{
	{"password", ""},
	{"contentSynchronisation", nil},
}

// contentSyncWire reshapes the stored contentSynchronisation onto the
// reference's nested wire form {enabled, statistics.enabled,
// properties.enabled, source.originAbsenceDetection}. BinFlow's
// canonical blob spells the sub-knobs flat (statisticsEnabled ...); a
// hand-seeded blob may already carry the nested spellings -- both are
// read, the unmentioned sub-knobs render the measured false defaults.
func contentSyncWire(v any) any {
	bools := map[string]bool{}
	if m, ok := v.(map[string]any); ok {
		for _, k := range []string{"enabled", "statisticsEnabled", "propertiesEnabled", "sourceOrigin"} {
			if b, ok := m[k].(bool); ok {
				bools[k] = b
			}
		}
		nested := func(outer, inner string) (bool, bool) {
			sub, ok := m[outer].(map[string]any)
			if !ok {
				return false, false
			}
			b, ok := sub[inner].(bool)
			return b, ok
		}
		if b, ok := nested("statistics", "enabled"); ok {
			bools["statisticsEnabled"] = b
		}
		if b, ok := nested("properties", "enabled"); ok {
			bools["propertiesEnabled"] = b
		}
		if b, ok := nested("source", "originAbsenceDetection"); ok {
			bools["sourceOrigin"] = b
		}
	}
	return map[string]any{
		"enabled":    bools["enabled"],
		"statistics": map[string]any{"enabled": bools["statisticsEnabled"]},
		"properties": map[string]any{"enabled": bools["propertiesEnabled"]},
		"source":     map[string]any{"originAbsenceDetection": bools["sourceOrigin"]},
	}
}

// partialConfigSeats is the non-admin projection (the L025-6 reference
// probes): exactly key/packageType/description plus the dialect key, and
// the one class-natural identifier per rclass -- the remote upstream url
// and the virtual member list. A local row carries NO url (the L025-4
// report's flagged drift: BinFlow fabricated a context URL there).
func partialConfigMap(row *metadata.Repo, blob map[string]any, dialect string) map[string]any {
	m := map[string]any{
		"key":         row.RepoKey,
		"packageType": row.PackageType,
		"description": row.Description,
		dialect:       row.Type,
	}
	switch row.Type {
	case repo.TypeRemote:
		m["url"] = blobString(blob, "url")
	case repo.TypeVirtual:
		if v, ok := blob["repositories"].([]any); ok {
			m["repositories"] = v
		} else {
			m["repositories"] = []any{}
		}
	}
	return m
}
