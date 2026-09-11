package repo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/remote"
)

// The M3 type-specific repository configuration model (PRD FR-15, T-64).
//
// The wire shape is the Artifactory-compatible field subset (docs/reverse/
// repo-semantics.md sections 7.1/8.1, high confidence); the canonical form
// below is what CreateRepo/UpdateRepo persist in repositories.config and what
// GetRepo echoes. local repositories keep the M1 passthrough contract
// (normalizeConfig): their config is caller-owned JSON — adapters (T-67's
// checksumPolicyType family) read it verbatim — with only the cross-cutting
// shared fields type-checked here.

// Product-level defaults of the remote repository fields (PRD v1.2 C4 per the
// ADR-0012 T-79 errata: missed 1800 / socket 15s / assumed offline 300s; the
// retrieval TTL default is PER PACKAGE TYPE — remote.DefaultContentTTLSecondsFor,
// the single point the fetch engine's loadRepo also reads, ADR-0012 erratum
// three: docker/helmoci 21600, every other type 7200). The remote_configs DDL
// defaults (86400/600) are schema-level fallbacks for rows created outside
// this service — the service always writes the product values (003 migration
// comment, T-62).
const (
	defaultMissedRetrievalCachePeriodSecs int64 = 1800
	defaultSocketTimeoutSecs              int64 = 15
	defaultAssumedOfflinePeriodSecs       int64 = 300
	// defaultMetadataTTLSeconds is the metadata cache TTL written into
	// remote_configs.metadata_ttl_seconds when a config blob carries no
	// explicit metadataRetrievalCachePeriodSecs (T-495 made the TTL a wire
	// knob — the remote-browsing snapshot and the pull-through metadata
	// cache rows both read this column; the DDL's 600 default and this
	// constant stay the same value).
	defaultMetadataTTLSeconds int64 = 600
	// defaultMetadataRetrievalTimeoutSecs is the T-290 (FR-90.2) default of
	// the per-repository metadata singleflight wait cap (repo-semantics 7.1,
	// metadataRetrievalTimeoutSecs 60) — the engine-wide constant becomes a
	// per-repository knob with the same product default.
	defaultMetadataRetrievalTimeoutSecs int64 = 60
)

// remoteConfig is the canonical remote repository configuration (FR-15; the
// field names are the Artifactory spellings, repo-semantics section 7.1).
//
// Password is deliberately ABSENT from the persisted form: until the crypto
// chain lands (T-66, ADR-0012 decision 4) accepting a credential here would
// write unprotected plaintext into the metadata store — the window the T-62
// review ordered closed. parseRemoteConfig ACCEPTS the field on input (so
// migration scripts keep working) and drops it; the remote_configs row is
// written with an empty password. NFR-S14 holds by construction: GET echoes
// the canonical form, which never carries a password.
type remoteConfig struct {
	URL                            string `json:"url"`
	Username                       string `json:"username,omitempty"`
	RetrievalCachePeriodSecs       int64  `json:"retrievalCachePeriodSecs"`
	MissedRetrievalCachePeriodSecs int64  `json:"missedRetrievalCachePeriodSecs"`
	SocketTimeoutMillis            int64  `json:"socketTimeoutMillis"`
	SocketTimeoutSecs              int64  `json:"socketTimeoutSecs"`
	MetadataRetrievalTimeoutSecs   int64  `json:"metadataRetrievalTimeoutSecs"`
	UnusedCleanupPeriodHours       int64  `json:"unusedArtifactsCleanupPeriodHours"`
	AssumedOfflinePeriodSecs       int64  `json:"assumedOfflinePeriodSecs"`
	HardFail                       bool   `json:"hardFail"`
	AllowPrivateUpstream           bool   `json:"allowPrivateUpstream"`
	PriorityResolution             bool   `json:"priorityResolution"`
	// MetadataRetrievalCachePeriodSecs is the metadata cache TTL (T-495,
	// FR-158 — the "cached per the Metadata Retrieval Cache Period"
	// semantics remote-browsing.md §1 anchors): the window the remote
	// enumeration snapshot AND the pull-through metadata cache rows are
	// held for, mirrored into remote_configs.metadata_ttl_seconds. A wire
	// knob since T-495 (T-461's "枚举快照 TTL 非 wire 可调" leftover
	// closed); the canonical echo always carries it, like every default
	// the remote config owns.
	MetadataRetrievalCachePeriodSecs int64 `json:"metadataRetrievalCachePeriodSecs"`
	// T-317 (FR-101.1 / K37): the smart remote replication fields, effective
	// since M11 — enableTokenAuthentication switches the fetcher's upstream
	// credential to a Bearer token (repo-semantics 7.1 "切 token 头"), and
	// contentSynchronisation carries the pull-side content-sync policy
	// (sub-field set per artifactory.xsd via inv-4 F5; propertiesEnabled
	// attaches upstream properties to cached nodes). Both echo CANONICALLY
	// (always present, like hardFail — the Artifactory full-config echo
	// posture); consumption lives in internal/remote's repoPolicy, which
	// reads this same JSON (the hardFail precedent — no remote_configs
	// columns: booleans default false = product default, so pre-T-317 rows
	// read as "off" with zero migration surface).
	EnableTokenAuthentication bool                   `json:"enableTokenAuthentication"`
	ContentSynchronisation    ContentSynchronisation `json:"contentSynchronisation"`
	// ChartsBaseURL is the helm remote repository's divergent charts fetch
	// base (helm.md section 6 / S10): the upstream serves its index.yaml at
	// the repository URL but hosts the .tgz/.prov bodies under a DIFFERENT
	// base — a heterogeneous-base upstream. Content-class fetches go to
	// chartsBaseUrl + <repo-relative path>; the repo-root index (metadata
	// class) always comes from the repository URL; unset falls back to the
	// repository URL (the mirror-aligned default, S10's fallback chain).
	//
	// T-367's in-ticket schema ruling (T-342 D-2①, registered for the
	// architect and settled here): the seat rides the canonical config JSON
	// — the enableTokenAuthentication precedent, no remote_configs column —
	// and is REFUSED BY NAME on every non-helm remote package type (the
	// validateLocalKeypairRef posture: silently accepting an inert field
	// would let an admin believe a maven remote fetches charts from the
	// base).
	ChartsBaseURL string `json:"chartsBaseUrl,omitempty"`
	// ListRemoteFolderItems is the remote-browsing optional档 (M16 T-448,
	// FR-147.2, remote-browsing.md section 1 / repo-semantics 7.1): when on,
	// a directory listing of this remote repository merges the upstream's
	// display-only derived rows beside the cache rows (the T-442 enumeration
	// engine); when off — the default, and the whole pre-T-448 posture — the
	// tree shows cached rows only and the upstream is never probed from a
	// browse face (T-406). Canonical echo rides the same JSON seat as
	// hardFail/enableTokenAuthentication (always present); a `true` on a
	// package type outside the batch-1 set is refused by name at config
	// time (the chartsBaseUrl posture — an inert accepted field is the
	// trap). deb/rpm are the official-setting types (remote-browsing.md
	// section 2); helm is BinFlow's L2 superset leg, registered there.
	ListRemoteFolderItems bool `json:"listRemoteFolderItems"`
}

// ContentSynchronisation is the smart remote content-sync policy (T-317,
// FR-101.1 — the K37 sub-field set anchored to artifactory.xsd via
// docs/reverse/inv-4-addons.md F5). Four booleans, all default false:
//
//   - enabled: the master switch — every sub-behavior is gated on it.
//   - propertiesEnabled: the pull-through fetcher attaches the upstream
//     instance's node properties to the cached node (internal/remote).
//   - statisticsEnabled: accepted + echoed; statistics transport is a
//     deliberate no-op — BinFlow has no download-statistics substrate
//     (the ?stats family is an unimplemented E-09 arm) and the inbound
//     reporting protocol has no public specification (replication.md's
//     own low-confidence list). Registered in the T-317 report.
//   - sourceOrigin: accepted + echoed; no behavior until an origin-marking
//     spec lands (same low-confidence registration).
type ContentSynchronisation struct {
	Enabled           bool `json:"enabled"`
	StatisticsEnabled bool `json:"statisticsEnabled"`
	PropertiesEnabled bool `json:"propertiesEnabled"`
	SourceOrigin      bool `json:"sourceOrigin"`
}

// contentSyncInput is the wire shape of contentSynchronisation (pointer
// fields keep absent distinct from an explicit false).
type contentSyncInput struct {
	Enabled           *bool `json:"enabled"`
	StatisticsEnabled *bool `json:"statisticsEnabled"`
	PropertiesEnabled *bool `json:"propertiesEnabled"`
	SourceOrigin      *bool `json:"sourceOrigin"`
}

// remoteConfigInput mirrors remoteConfig with the input-only extras: the
// password (accepted then dropped) and pointer fields so "absent" (default)
// is distinguishable from an explicit zero.
//
// T-290 (FR-90.2) added the ms-granularity timeout spellings and the smart
// remote effective subset; T-346 (FR-113.1, the T-290-2 carryover) flipped
// the canonical spelling:
//
//   - socketTimeoutMillis (the artifactory.xsd / repo-semantics 7.1
//     spelling) is the CANONICAL form — the persisted canonical JSON and
//     the GET echo carry it, never the ms alias. socketTimeoutMs (the
//     PRD/LC-12 spelling) is an INPUT-ONLY alias: accepted on the write
//     plane, canonicalized away. An explicit 0 on either side counts as
//     ABSENT (the create-time "explicit zero keeps the default" rule —
//     resolveRemoteAlias), and two non-zero spellings that disagree refuse
//     (the virtualConfigInput alias-disagreement rule). A non-zero ms
//     value takes precedence over the M3 socketTimeoutSecs field — only
//     the ms spellings can express sub-second timeouts, so the coarser
//     legacy field yields (a zero ms spelling yields right back).
//   - missRetrievalCachePeriodSecs (the inv-4 F5 / PRD spelling) is an
//     input alias of missedRetrievalCachePeriodSecs (the canonical
//     Artifactory spelling this model keeps); the same 0-as-absent /
//     non-zero-disagreement-refuses rule as the ms pair applies.
//   - metadataRetrievalTimeoutSecs and unusedArtifactsCleanupPeriodHours
//     round out the FR-90.2 subset (defaults 60 and 0/off).
type remoteConfigInput struct {
	URL                               *string          `json:"url"`
	Username                          string           `json:"username"`
	Password                          string           `json:"password"` // accepted, never persisted (T-66 owns the encrypted form)
	RetrievalCachePeriodSecs          *int64           `json:"retrievalCachePeriodSecs"`
	MissedRetrievalCachePeriodSecs    *int64           `json:"missedRetrievalCachePeriodSecs"`
	MissRetrievalCachePeriodSecs      *int64           `json:"missRetrievalCachePeriodSecs"` // alias of the field above
	SocketTimeoutSecs                 *int64           `json:"socketTimeoutSecs"`
	SocketTimeoutMs                   *int64           `json:"socketTimeoutMs"`     // input-only alias (FR-113.1); canonicalized to socketTimeoutMillis
	SocketTimeoutMillis               *int64           `json:"socketTimeoutMillis"` // canonical ms spelling (artifactory.xsd)
	MetadataRetrievalTimeoutSecs      *int64           `json:"metadataRetrievalTimeoutSecs"`
	UnusedArtifactsCleanupPeriodHours *int64           `json:"unusedArtifactsCleanupPeriodHours"`
	AssumedOfflinePeriodSecs          *int64           `json:"assumedOfflinePeriodSecs"`
	HardFail                          *bool            `json:"hardFail"`
	AllowPrivateUpstream              *bool            `json:"allowPrivateUpstream"`
	PriorityResolution                *bool            `json:"priorityResolution"`
	EnableTokenAuthentication         *bool            `json:"enableTokenAuthentication"`
	ContentSynchronisation            *json.RawMessage `json:"contentSynchronisation"`
	ChartsBaseURL                     *string          `json:"chartsBaseUrl"`
	ListRemoteFolderItems             *bool            `json:"listRemoteFolderItems"`
	// MetadataRetrievalCachePeriodSecs is the metadata TTL knob (T-495):
	// the same spelling the canonical form echoes, the family's rules —
	// pointer so absent keeps the stored value on update, an explicit 0
	// counts as absent (keeps the 600s product default), a negative value
	// refuses by name with a 400.
	MetadataRetrievalCachePeriodSecs *int64 `json:"metadataRetrievalCachePeriodSecs"`
}

// validateRemoteConfigShape is the strict single-JSON-value gate of the
// remote config blob. T-290 introduced it as the refusal arm of the M11
// field pair; T-317 (FR-101.1) inverted that refusal — the two fields are
// ACCEPTED and effective now — but the strictness the gate added stays:
// the blob must be exactly ONE JSON value, because the typed decode below
// (Decoder.Decode) silently ignores whatever follows the first value.
func validateRemoteConfigShape(config string) error {
	dec := json.NewDecoder(strings.NewReader(config))
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return nil // empty blob: the typed decode below names the missing url
		}
		return fmt.Errorf("%w: remote repository config: %w", ErrInvalidRepoConfig, err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf(
			"%w: remote repository config: trailing data after the JSON object", ErrInvalidRepoConfig)
	}
	return nil
}

// parseContentSynchronisation validates the contentSynchronisation input
// (T-317, FR-101.1 / K37). A nil or JSON-null value counts as ABSENT (all
// four booleans keep the false default — the same explicit-null-is-absent
// read every other nullable config field gets); an OBJECT decodes the four
// anchored sub-fields (unknown sub-fields drop, the scenario-D tolerance at
// one level down); any other JSON shape refuses naming the field, so a
// boolean or string cannot silently masquerade as the policy object.
func parseContentSynchronisation(raw *json.RawMessage) (ContentSynchronisation, error) {
	if raw == nil || string(*raw) == "null" {
		return ContentSynchronisation{}, nil
	}
	var in contentSyncInput
	if err := json.Unmarshal(*raw, &in); err != nil {
		return ContentSynchronisation{}, fmt.Errorf(
			"%w: remote repository config: contentSynchronisation must be an object of booleans (enabled, statisticsEnabled, propertiesEnabled, sourceOrigin): %w",
			ErrInvalidRepoConfig, err)
	}
	var out ContentSynchronisation
	if in.Enabled != nil {
		out.Enabled = *in.Enabled
	}
	if in.StatisticsEnabled != nil {
		out.StatisticsEnabled = *in.StatisticsEnabled
	}
	if in.PropertiesEnabled != nil {
		out.PropertiesEnabled = *in.PropertiesEnabled
	}
	if in.SourceOrigin != nil {
		out.SourceOrigin = *in.SourceOrigin
	}
	return out, nil
}

// parseRemoteConfig validates one remote repository config blob and returns
// its canonical form plus the input password (the encryption chain of T-66:
// the password never persists in the canonical JSON, but the service stores
// its AES-256-GCM sealed form in the remote_configs row — ADR-0012 decision
// 4). With no master key configured the password is dropped with a WARN
// (fetching then goes anonymous), which keeps the T-64 no-plaintext-window
// contract exactly: nothing unprotected ever reaches the store.
// packageType carries the repository row's package type — the one
// per-protocol seat, chartsBaseUrl (T-367), keys on it.
//
// Validation is scheme/format only (FR-15-AC3): a private-address URL is
// LEGAL at create time — the SSRF chain runs per request because DNS and
// networks change (NFR-S13, ADR-0012 errata two point five). The URL's
// trailing slashes are trimmed so the fetcher's {url}/{path} concatenation
// can never produce a double slash.
// Unknown fields are DROPPED, not rejected: the M3 field set is a subset of
// Artifactory's (PRD scenario D — migration scripts keep their full config
// bodies and only swap the URL prefix), so tolerance is the compatible
// posture while the canonical form stays exactly what M3 serves. The
// T-290-era refusal of the smart remote pair (enableTokenAuthentication /
// contentSynchronisation) is retired by T-317 (FR-101.1): both are ACCEPTED
// and effective — the no-inert-fields rule is satisfied by their consumers
// in internal/remote (Bearer token auth, pull-side property attach). The
// same rule governs chartsBaseUrl (T-367): accepted and effective on helm
// remotes, refused by name everywhere else (never a stored inert field).
func parseRemoteConfig(config, packageType string) (remoteConfig, string, error) {
	if err := validateRemoteConfigShape(config); err != nil {
		return remoteConfig{}, "", err
	}
	var in remoteConfigInput
	dec := json.NewDecoder(strings.NewReader(config))
	if err := dec.Decode(&in); err != nil {
		return remoteConfig{}, "", fmt.Errorf("%w: remote repository config: %w", ErrInvalidRepoConfig, err)
	}
	if in.URL == nil || strings.TrimSpace(*in.URL) == "" {
		return remoteConfig{}, "", fmt.Errorf(
			"%w: remote repository config: url is required (http/https upstream base URL)", ErrInvalidRepoConfig)
	}
	rawURL := strings.TrimSpace(*in.URL)
	u, err := url.Parse(rawURL)
	if err != nil {
		return remoteConfig{}, "", fmt.Errorf("%w: remote repository config: url %q: %w", ErrInvalidRepoConfig, rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return remoteConfig{}, "", fmt.Errorf(
			"%w: remote repository config: url %q: scheme must be http or https", ErrInvalidRepoConfig, rawURL)
	}
	if u.Host == "" {
		return remoteConfig{}, "", fmt.Errorf(
			"%w: remote repository config: url %q: host is required", ErrInvalidRepoConfig, rawURL)
	}

	// chartsBaseUrl (T-367, the per-protocol seat): a non-empty value is a
	// helm remote's field ONLY — any other package type gets the by-name
	// refusal (the validateLocalKeypairRef posture: an inert accepted field
	// is the trap; a silently dropped one is a lie). Absent or an explicit
	// "" (clearing the base back to the URL fallback) passes everywhere.
	chartsBase := ""
	if in.ChartsBaseURL != nil {
		if trimmed := strings.TrimSpace(*in.ChartsBaseURL); trimmed != "" {
			if packageType != PackageHelm {
				return remoteConfig{}, "", fmt.Errorf(
					"%w: remote %s repository config: chartsBaseUrl %q is not accepted (the divergent charts fetch base is a helm remote-repository behavior)",
					ErrInvalidRepoConfig, packageType, trimmed)
			}
			cb, err := url.Parse(trimmed)
			if err != nil {
				return remoteConfig{}, "", fmt.Errorf(
					"%w: remote repository config: chartsBaseUrl %q: %w", ErrInvalidRepoConfig, trimmed, err)
			}
			if cb.Scheme != "http" && cb.Scheme != "https" {
				return remoteConfig{}, "", fmt.Errorf(
					"%w: remote repository config: chartsBaseUrl %q: scheme must be http or https", ErrInvalidRepoConfig, trimmed)
			}
			if cb.Host == "" {
				return remoteConfig{}, "", fmt.Errorf(
					"%w: remote repository config: chartsBaseUrl %q: host is required", ErrInvalidRepoConfig, trimmed)
			}
			chartsBase = strings.TrimRight(cb.String(), "/")
		}
	}

	out := remoteConfig{
		URL:      strings.TrimRight(u.String(), "/"),
		Username: in.Username,
		// The create-time content-TTL default resolves per package type
		// (remote.DefaultContentTTLSecondsFor — ADR-0012 erratum three's
		// one-resolver rule: this write and the engine's fetch TTL are the
		// two consumers of one source; docker/helmoci 21600, else 7200).
		RetrievalCachePeriodSecs:         remote.DefaultContentTTLSecondsFor(packageType),
		MissedRetrievalCachePeriodSecs:   defaultMissedRetrievalCachePeriodSecs,
		SocketTimeoutMillis:              defaultSocketTimeoutSecs * 1000,
		SocketTimeoutSecs:                defaultSocketTimeoutSecs,
		MetadataRetrievalTimeoutSecs:     defaultMetadataRetrievalTimeoutSecs,
		UnusedCleanupPeriodHours:         0, // off (repo-semantics 7.1)
		AssumedOfflinePeriodSecs:         defaultAssumedOfflinePeriodSecs,
		HardFail:                         false,
		AllowPrivateUpstream:             false,
		PriorityResolution:               false,
		MetadataRetrievalCachePeriodSecs: defaultMetadataTTLSeconds,
	}

	// missRetrievalCachePeriodSecs alias (T-290): one knob, two spellings;
	// an explicit 0 counts as ABSENT on either side (the create-time
	// "explicit zero keeps the default" rule — 0+value resolves to the
	// value, matching the fetcher's 0=unset consumption), and two non-zero
	// spellings that differ refuse like the virtual aliases do.
	missed, ok := resolveRemoteAlias(in.MissedRetrievalCachePeriodSecs, in.MissRetrievalCachePeriodSecs)
	if !ok {
		return remoteConfig{}, "", fmt.Errorf(
			"%w: remote repository config: missedRetrievalCachePeriodSecs and missRetrievalCachePeriodSecs disagree (%d vs %d)",
			ErrInvalidRepoConfig, derefInt64(in.MissedRetrievalCachePeriodSecs), derefInt64(in.MissRetrievalCachePeriodSecs))
	}

	// socketTimeoutMillis / socketTimeoutMs alias pair (T-290; canonical
	// flipped to the xsd spelling by T-346 / FR-113.1): same knob, either
	// spelling accepted on input, the same 0-as-absent rule applies.
	socketMs, ok := resolveRemoteAlias(in.SocketTimeoutMs, in.SocketTimeoutMillis)
	if !ok {
		return remoteConfig{}, "", fmt.Errorf(
			"%w: remote repository config: socketTimeoutMs and socketTimeoutMillis disagree (%d vs %d)",
			ErrInvalidRepoConfig, derefInt64(in.SocketTimeoutMs), derefInt64(in.SocketTimeoutMillis))
	}
	// A resolved zero is "unset": the legacy socketTimeoutSecs field
	// applies then, exactly like the fetcher's fallback chain
	// (effectiveSocketTimeoutMs) treats a zero column.
	if socketMs != nil && *socketMs == 0 {
		socketMs = nil
	}

	for _, f := range []struct {
		name  string
		given bool
		value int64
	}{
		{"retrievalCachePeriodSecs", in.RetrievalCachePeriodSecs != nil, derefInt64(in.RetrievalCachePeriodSecs)},
		{"missedRetrievalCachePeriodSecs", missed != nil, derefInt64(missed)},
		{"socketTimeoutMillis", socketMs != nil, derefInt64(socketMs)},
		{"socketTimeoutSecs", socketMs == nil && in.SocketTimeoutSecs != nil, derefInt64(in.SocketTimeoutSecs)},
		{"metadataRetrievalTimeoutSecs", in.MetadataRetrievalTimeoutSecs != nil, derefInt64(in.MetadataRetrievalTimeoutSecs)},
		{"metadataRetrievalCachePeriodSecs", in.MetadataRetrievalCachePeriodSecs != nil, derefInt64(in.MetadataRetrievalCachePeriodSecs)},
		{"unusedArtifactsCleanupPeriodHours", in.UnusedArtifactsCleanupPeriodHours != nil, derefInt64(in.UnusedArtifactsCleanupPeriodHours)},
		{"assumedOfflinePeriodSecs", in.AssumedOfflinePeriodSecs != nil, derefInt64(in.AssumedOfflinePeriodSecs)},
	} {
		if !f.given || f.value == 0 {
			continue // absent or explicit zero: keep the product default
		}
		if f.value < 0 {
			return remoteConfig{}, "", fmt.Errorf(
				"%w: remote repository config: %s must not be negative (got %d)", ErrInvalidRepoConfig, f.name, f.value)
		}
		switch f.name {
		case "retrievalCachePeriodSecs":
			out.RetrievalCachePeriodSecs = f.value
		case "missedRetrievalCachePeriodSecs":
			out.MissedRetrievalCachePeriodSecs = f.value
		case "socketTimeoutMillis":
			out.SocketTimeoutMillis = f.value
		case "socketTimeoutSecs":
			out.SocketTimeoutMillis = f.value * 1000
		case "metadataRetrievalTimeoutSecs":
			out.MetadataRetrievalTimeoutSecs = f.value
		case "metadataRetrievalCachePeriodSecs":
			out.MetadataRetrievalCachePeriodSecs = f.value
		case "unusedArtifactsCleanupPeriodHours":
			out.UnusedCleanupPeriodHours = f.value
		case "assumedOfflinePeriodSecs":
			out.AssumedOfflinePeriodSecs = f.value
		}
	}
	// The legacy echo field is derived from the effective ms value (ceil, so
	// the seconds spelling never over-reports the timeout a client gets).
	out.SocketTimeoutSecs = (out.SocketTimeoutMillis + 999) / 1000
	if in.HardFail != nil {
		out.HardFail = *in.HardFail
	}
	// listRemoteFolderItems (T-448, FR-147.2): a `true` outside the
	// enumeration engine's batch-1 set is refused BY NAME (the chartsBaseUrl
	// posture — the admin would believe the tree merges upstream rows on a
	// type whose upstream has no root-level enumeration); absent or an
	// explicit false passes on every type (false IS the product default).
	// The set question is the engine's own (remote.BrowseSupported — one
	// source, no repo-side spelling mirror to drift).
	if in.ListRemoteFolderItems != nil {
		if *in.ListRemoteFolderItems && !remote.BrowseSupported(packageType) {
			return remoteConfig{}, "", fmt.Errorf(
				"%w: remote %s repository config: listRemoteFolderItems true is not accepted (remote folder enumeration exists for the batch-1 types: helm, debian, rpm)",
				ErrInvalidRepoConfig, packageType)
		}
		out.ListRemoteFolderItems = *in.ListRemoteFolderItems
	}
	if in.AllowPrivateUpstream != nil {
		out.AllowPrivateUpstream = *in.AllowPrivateUpstream
	}
	if in.PriorityResolution != nil {
		out.PriorityResolution = *in.PriorityResolution
	}
	// T-317 (FR-101.1): the smart remote pair — a mistyped
	// enableTokenAuthentication fails the typed decode above with the field
	// named in the json error; contentSynchronisation gets its own shape
	// gate so a non-object value names the field precisely.
	if in.EnableTokenAuthentication != nil {
		out.EnableTokenAuthentication = *in.EnableTokenAuthentication
	}
	cs, csErr := parseContentSynchronisation(in.ContentSynchronisation)
	if csErr != nil {
		return remoteConfig{}, "", csErr
	}
	out.ContentSynchronisation = cs
	out.ChartsBaseURL = chartsBase
	return out, in.Password, nil
}

// resolveRemoteAlias merges one alias pair of the T-290 remote fields: an
// absent OR EXPLICIT-ZERO spelling yields to the other side's value (the
// create-time "explicit zero keeps the default" rule — the fetcher's
// 0=unset consumption reads the same way), and ok=false marks the one
// refusal left: two non-zero spellings that disagree.
func resolveRemoteAlias(a, b *int64) (v *int64, ok bool) {
	av, bv := derefInt64(a), derefInt64(b)
	switch {
	case b == nil || bv == 0:
		return a, true
	case a == nil || av == 0:
		return b, true
	case av != bv:
		return nil, false
	default:
		return a, true
	}
}

// derefInt64 returns *v or 0 for nil.
func derefInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// marshalConfig renders one canonical config struct into the JSON stored in
// repositories.config.
func marshalConfig(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("render repository config: %w", err)
	}
	return string(b), nil
}

// maskRemoteConfig is the read-boundary defense of NFR-S14: the canonical
// form never carries a password, but a row written by any other means (a
// future migration, direct DB surgery) must not leak one through GetRepo
// either. A password key of any value is dropped from the echoed config.
func maskRemoteConfig(config string) string {
	if !strings.Contains(config, "password") {
		return config // fast path: nothing that even looks like a credential
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(config), &raw); err != nil {
		return config // not our canonical shape; echo verbatim (masking is best-effort)
	}
	if _, ok := raw["password"]; !ok {
		return config
	}
	delete(raw, "password")
	out, err := marshalConfig(raw)
	if err != nil {
		return config
	}
	return out
}

// ---- virtual repository config (FR-15; repo-semantics section 8) ----

// virtualConfig is the canonical virtual repository configuration.
type virtualConfig struct {
	Repositories          []string `json:"repositories"`
	DefaultDeploymentRepo string   `json:"defaultDeploymentRepo,omitempty"`
}

// virtualConfigInput adds the write-routing aliases Artifactory's REST body
// carries (repo-semantics section 8.2 spells defaultDeploymentRepoRef; the
// console-era deploymentRepository spelling is accepted alongside). All
// aliases are collected ("并收"); conflicting non-empty values refuse.
type virtualConfigInput struct {
	Repositories             []string `json:"repositories"`
	DefaultDeploymentRepo    string   `json:"defaultDeploymentRepo"`
	DefaultDeploymentRepoRef string   `json:"defaultDeploymentRepoRef"`
	DeploymentRepository     string   `json:"deploymentRepository"`
}

// parseVirtualConfig validates the virtual repository config blob's shape.
// Member EXISTENCE and the no-nesting rule need the store and are checked in
// the service (validateVirtualMembers); this function owns the JSON-level
// contract only: repositories present and non-empty, write-routing aliases
// agreeing.
// Unknown fields are dropped (the same migration-script tolerance as
// parseRemoteConfig).
func parseVirtualConfig(config string) (virtualConfig, error) {
	var in virtualConfigInput
	dec := json.NewDecoder(strings.NewReader(config))
	if err := dec.Decode(&in); err != nil {
		return virtualConfig{}, fmt.Errorf("%w: virtual repository config: %w", ErrInvalidRepoConfig, err)
	}
	if len(in.Repositories) == 0 {
		return virtualConfig{}, fmt.Errorf(
			"%w: virtual repository config: repositories is required and must list at least one member", ErrInvalidRepoConfig)
	}
	def := in.DefaultDeploymentRepo
	for _, alias := range []string{in.DefaultDeploymentRepoRef, in.DeploymentRepository} {
		if alias == "" {
			continue
		}
		if def == "" {
			def = alias
			continue
		}
		if def != alias {
			return virtualConfig{}, fmt.Errorf(
				"%w: virtual repository config: defaultDeploymentRepo aliases disagree (%q vs %q)",
				ErrInvalidRepoConfig, def, alias)
		}
	}
	return virtualConfig{Repositories: in.Repositories, DefaultDeploymentRepo: def}, nil
}

// byHashPolicies is the closed value domain of the deb by-hash policy key
// (debian.md section 5, high confidence — the spec's code enum plus the inv-3
// double confirmation). K45's in-ticket ruling (FR-113.2): this is THE enum;
// rpm.md carries no byHash knob of its own (its policy keys are boolean/integer
// typed and ride the decode-time typing), so the gate is spelled once, here,
// for the one key that has a value domain. The check is package-type-agnostic
// like every other validateLocalConfig rule: the key is the deb policy knob,
// but a mistyped value never sits stored whatever the repository's type (the
// adapter's normalized() reader keeps its unknown-reads-as-NONE arm as defense
// in depth for hand-mangled blobs — T-327R registered exactly this split).
var byHashPolicies = map[string]bool{
	"ALL": true, "SHA256": true, "NONE": true,
}

// validateLocalConfig type-checks the cross-cutting fields of a LOCAL
// repository config (M1's passthrough contract keeps the blob caller-owned;
// only the fields this package's own consumers read are validated).
// priorityResolution is the virtual-resolution member mark (PRD C3 two-bucket
// order); it must be a boolean when present so T-71's reader never trips over
// a hand-mangled blob. The M4 governance fields (T-95/W12a/W26) validate the
// same way: quotaBytes must be a non-negative integer (0 = unlimited, the
// default); a type error anywhere is refused at CONFIG time (the decode error
// names the offending field), never discovered mid-upload.
//
// T-346 (FR-113.2, the T-327R leftover ②): byHash gains its value-domain gate
// — an arbitrary string no longer stores verbatim to be read as NONE later;
// the PUT refuses with the enum named (Artifactory's configure-time posture).
// An absent or empty value stays legal (absent = the adapter default NONE).
func validateLocalConfig(config string) error {
	var probe struct {
		PriorityResolution *bool   `json:"priorityResolution"`
		QuotaBytes         *int64  `json:"quotaBytes"`
		IncludesPattern    *string `json:"includesPattern"`
		ExcludesPattern    *string `json:"excludesPattern"`
		ByHash             *string `json:"byHash"`
		// T-355A (FR-110.2, the D-5 carryover): forceConanAuthentication is
		// the conan repo-config switch (conan.md section 2's auth gate,
		// consumed by the conan adapter). A boolean when present — typing
		// rides the decode like every other probe field, so a mistyped value
		// is refused at CONFIG time with the field named, never discovered
		// when the adapter's tolerant probe reads it as false.
		ForceConanAuthentication *bool `json:"forceConanAuthentication"`
		// T-490 (FR-156.1): blackedOut is the blackout mark the WRITE plane
		// reads (refuseBlackedOut, rest-api.md section 1.2 step 6). A
		// boolean when present — the same decode-time typing so a mistyped
		// mark is refused at CONFIG time with the field named, never
		// discovered as a silently-false read inside the gate.
		BlackedOut *bool `json:"blackedOut"`
		// T-490 (FR-156.1): the Stage domain — Artifactory's repository
		// environment tags (7.84 audit name Environments, 7.161 UI name
		// Stage). WIRE KEY RULING: `environments` is the canonical spelling
		// (the official JFrog REST reference's documented field); `stages`
		// is the 7.161-era alias (docs/reverse/webhook.md carries a
		// same-era `stages` wire key on the app-trust face, while
		// release-bundle v2 promotion keeps `selectedEnvironments` — both
		// spellings live in the 7.161 product). The local blob stores
		// whichever spelling(s) arrived (the passthrough posture — the echo
		// is verbatim, so round-trip is consistent per key); the ONE
		// cross-cutting rule is the alias-disagreement refusal, the same
		// resolveRemoteAlias posture every other dual spelling gets.
		Environments []string `json:"environments"`
		Stages       []string `json:"stages"`
	}
	if err := json.Unmarshal([]byte(config), &probe); err != nil {
		return fmt.Errorf("%w: local repository config: %w", ErrInvalidRepoConfig, err)
	}
	if probe.QuotaBytes != nil && *probe.QuotaBytes < 0 {
		return fmt.Errorf("%w: quotaBytes must not be negative (got %d; 0 means unlimited)",
			ErrInvalidRepoConfig, *probe.QuotaBytes)
	}
	if probe.ByHash != nil && *probe.ByHash != "" && !byHashPolicies[*probe.ByHash] {
		return fmt.Errorf("%w: byHash %q is not a legal by-hash policy: must be one of ALL, SHA256, NONE (debian.md section 5)",
			ErrInvalidRepoConfig, *probe.ByHash)
	}
	if err := validateStageNames(probe.Environments, probe.Stages); err != nil {
		return err
	}
	return nil
}

// validateStageNames holds the Stage domain's cross-cutting rules (T-490,
// FR-156.1): two spellings of one knob never disagree, and a stage name is
// a non-empty token (an empty entry is garbage no UI can produce and no
// echo should preserve). The nil-vs-empty split follows the passthrough
// posture: nil = the key was absent, an explicit empty array is a legal
// clear.
func validateStageNames(environments, stages []string) error {
	if len(environments) > 0 && len(stages) > 0 && !strSlicesEqual(environments, stages) {
		return fmt.Errorf(
			"%w: local repository config: environments and stages are two spellings of one knob and disagree (%v vs %v)",
			ErrInvalidRepoConfig, environments, stages)
	}
	for _, name := range environments {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("%w: local repository config: environments/stages entries must be non-empty stage names",
				ErrInvalidRepoConfig)
		}
	}
	for _, name := range stages {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("%w: local repository config: environments/stages entries must be non-empty stage names",
				ErrInvalidRepoConfig)
		}
	}
	return nil
}

// strSlicesEqual compares two string slices for element-wise equality.
func strSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
