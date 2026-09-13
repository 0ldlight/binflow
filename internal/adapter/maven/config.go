package maven

import (
	"encoding/json"
	"strings"
)

// Checksum policy values of a LOCAL maven repository (repo-semantics.md
// section 5, high confidence — two legal values; the remote four-value
// family is T-66's, and a local row carrying one of those spellings is
// treated as the strict default, the safe side).
const (
	// ChecksumPolicyClient is the default (and the value an unconfigured
	// repository behaves as): client-declared digests that disagree with
	// the measured content are a 409.
	ChecksumPolicyClient = "client-checksums"
	// ChecksumPolicyServerGenerated silently accepts disagreeing client
	// digests; the measured values are the ones that land and echo.
	ChecksumPolicyServerGenerated = "server-generated-checksums"
)

// Snapshot version behaviors (maven-npm-pypi.md section 1.3, high
// confidence; the field-absent default pinned by the L013-4 A6 wire: the
// reference rewrites -SNAPSHOT PUTs in a repository created without the
// field). L014-2 implements the unique server-side rewrite.
const (
	BehaviorDeployer  = "deployer"
	BehaviorNonUnique = "non-unique"
	BehaviorUnique    = "unique"
)

// RepoConfig is the maven-relevant slice of a local repository's config
// blob. Local configs are caller-owned passthrough JSON (T-64 ruling); this
// reader is deliberately lenient — unknown values fall back to the safe
// default rather than failing the request, because a hand-migrated config
// blob must not take the content plane down.
type RepoConfig struct {
	// ChecksumPolicy is ChecksumPolicyClient unless the row spells the
	// server-generated value.
	ChecksumPolicy string
	// SnapshotBehavior records the configured spelling. The field-absent
	// default is unique (the L013-4 A6 wire: a repository created without
	// the field rewrites -SNAPSHOT PUTs to ts-N); non-unique and deployer
	// store the uploaded name.
	SnapshotBehavior string
	// HandleReleases/HandleSnapshots are true unless explicitly false; a
	// false value refuses the matching deploy with 409 (ME-08, v1.1
	// errata: SnapshotPolicyException carries 409).
	HandleReleases  bool
	HandleSnapshots bool
}

// ParseRepoConfig reads the config blob of a repository row. An empty,
// non-JSON or field-wise unknown blob yields the defaults — never an
// error.
func ParseRepoConfig(config string) RepoConfig {
	rc := RepoConfig{
		ChecksumPolicy:   ChecksumPolicyClient,
		SnapshotBehavior: BehaviorUnique,
		HandleReleases:   true,
		HandleSnapshots:  true,
	}
	var raw struct {
		ChecksumPolicyType      string `json:"checksumPolicyType"`
		SnapshotVersionBehavior string `json:"snapshotVersionBehavior"`
		HandleReleases          *bool  `json:"handleReleases"`
		HandleSnapshots         *bool  `json:"handleSnapshots"`
	}
	if err := json.Unmarshal([]byte(config), &raw); err != nil {
		return rc // not our shape (remote/virtual canonical forms, "{}", garbage): defaults
	}
	if strings.TrimSpace(raw.ChecksumPolicyType) == ChecksumPolicyServerGenerated {
		rc.ChecksumPolicy = ChecksumPolicyServerGenerated
	}
	switch strings.TrimSpace(raw.SnapshotVersionBehavior) {
	case BehaviorNonUnique:
		rc.SnapshotBehavior = BehaviorNonUnique
	case BehaviorDeployer:
		rc.SnapshotBehavior = BehaviorDeployer
	case BehaviorUnique:
		rc.SnapshotBehavior = BehaviorUnique
	} // absent (and any unknown spelling) stays unique — the lenient default
	if raw.HandleReleases != nil {
		rc.HandleReleases = *raw.HandleReleases
	}
	if raw.HandleSnapshots != nil {
		rc.HandleSnapshots = *raw.HandleSnapshots
	}
	return rc
}

// AcceptsSnapshot reports whether a deploy into a -SNAPSHOT version
// directory (or with a timestamped snapshot file name) is allowed.
func (rc RepoConfig) AcceptsSnapshot() bool { return rc.HandleSnapshots }

// AcceptsRelease reports whether a release-directory deploy is allowed.
func (rc RepoConfig) AcceptsRelease() bool { return rc.HandleReleases }
