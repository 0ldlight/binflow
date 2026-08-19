package repo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
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
// ADR-0012 T-79 errata: retrieval 7200 / missed 1800 / socket 15s / assumed
// offline 300s). The remote_configs DDL defaults (86400/600) are schema-level
// fallbacks for rows created outside this service — the service always writes
// the product values (003 migration comment, T-62).
const (
	defaultRetrievalCachePeriodSecs       int64 = 7200
	defaultMissedRetrievalCachePeriodSecs int64 = 1800
	defaultSocketTimeoutSecs              int64 = 15
	defaultAssumedOfflinePeriodSecs       int64 = 300
	// defaultMetadataTTLSeconds is the metadata cache TTL written into
	// remote_configs.metadata_ttl_seconds. It has no create-API field in M3
	// (ADR-0012's dual-TTL split is fetcher bookkeeping, not a user knob), so
	// the product value is a constant, not an input.
	defaultMetadataTTLSeconds int64 = 600
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
	SocketTimeoutSecs              int64  `json:"socketTimeoutSecs"`
	AssumedOfflinePeriodSecs       int64  `json:"assumedOfflinePeriodSecs"`
	HardFail                       bool   `json:"hardFail"`
	AllowPrivateUpstream           bool   `json:"allowPrivateUpstream"`
	PriorityResolution             bool   `json:"priorityResolution"`
}

// remoteConfigInput mirrors remoteConfig with the input-only extras: the
// password (accepted then dropped) and pointer fields so "absent" (default)
// is distinguishable from an explicit zero.
type remoteConfigInput struct {
	URL                            *string `json:"url"`
	Username                       string  `json:"username"`
	Password                       string  `json:"password"` // accepted, never persisted (T-66 owns the encrypted form)
	RetrievalCachePeriodSecs       *int64  `json:"retrievalCachePeriodSecs"`
	MissedRetrievalCachePeriodSecs *int64  `json:"missedRetrievalCachePeriodSecs"`
	SocketTimeoutSecs              *int64  `json:"socketTimeoutSecs"`
	AssumedOfflinePeriodSecs       *int64  `json:"assumedOfflinePeriodSecs"`
	HardFail                       *bool   `json:"hardFail"`
	AllowPrivateUpstream           *bool   `json:"allowPrivateUpstream"`
	PriorityResolution             *bool   `json:"priorityResolution"`
}

// parseRemoteConfig validates one remote repository config blob and returns
// its canonical form plus the parsed input (CreateConfig needs fields the
// canonical JSON no longer carries — the dropped password shape is identical,
// but keeping both makes the "input accepted, output canonical" contract
// explicit at the call site).
//
// Validation is scheme/format only (FR-15-AC3): a private-address URL is
// LEGAL at create time — the SSRF chain runs per request because DNS and
// networks change (NFR-S13, ADR-0012 errata two point five). The URL's
// trailing slashes are trimmed so the fetcher's {url}/{path} concatenation
// can never produce a double slash.
// Unknown fields are DROPPED, not rejected: the M3 field set is a subset of
// Artifactory's (PRD scenario D — migration scripts keep their full config
// bodies and only swap the URL prefix), so tolerance is the compatible
// posture while the canonical form stays exactly what M3 serves.
func parseRemoteConfig(config string) (remoteConfig, error) {
	var in remoteConfigInput
	dec := json.NewDecoder(strings.NewReader(config))
	if err := dec.Decode(&in); err != nil {
		return remoteConfig{}, fmt.Errorf("%w: remote repository config: %w", ErrInvalidRepoConfig, err)
	}
	if in.URL == nil || strings.TrimSpace(*in.URL) == "" {
		return remoteConfig{}, fmt.Errorf(
			"%w: remote repository config: url is required (http/https upstream base URL)", ErrInvalidRepoConfig)
	}
	rawURL := strings.TrimSpace(*in.URL)
	u, err := url.Parse(rawURL)
	if err != nil {
		return remoteConfig{}, fmt.Errorf("%w: remote repository config: url %q: %w", ErrInvalidRepoConfig, rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return remoteConfig{}, fmt.Errorf(
			"%w: remote repository config: url %q: scheme must be http or https", ErrInvalidRepoConfig, rawURL)
	}
	if u.Host == "" {
		return remoteConfig{}, fmt.Errorf(
			"%w: remote repository config: url %q: host is required", ErrInvalidRepoConfig, rawURL)
	}

	out := remoteConfig{
		URL:                            strings.TrimRight(u.String(), "/"),
		Username:                       in.Username,
		RetrievalCachePeriodSecs:       defaultRetrievalCachePeriodSecs,
		MissedRetrievalCachePeriodSecs: defaultMissedRetrievalCachePeriodSecs,
		SocketTimeoutSecs:              defaultSocketTimeoutSecs,
		AssumedOfflinePeriodSecs:       defaultAssumedOfflinePeriodSecs,
		HardFail:                       false,
		AllowPrivateUpstream:           false,
		PriorityResolution:             false,
	}
	for _, f := range []struct {
		name  string
		given bool
		value int64
	}{
		{"retrievalCachePeriodSecs", in.RetrievalCachePeriodSecs != nil, derefInt64(in.RetrievalCachePeriodSecs)},
		{"missedRetrievalCachePeriodSecs", in.MissedRetrievalCachePeriodSecs != nil, derefInt64(in.MissedRetrievalCachePeriodSecs)},
		{"socketTimeoutSecs", in.SocketTimeoutSecs != nil, derefInt64(in.SocketTimeoutSecs)},
		{"assumedOfflinePeriodSecs", in.AssumedOfflinePeriodSecs != nil, derefInt64(in.AssumedOfflinePeriodSecs)},
	} {
		if !f.given || f.value == 0 {
			continue // absent or explicit zero: keep the product default
		}
		if f.value < 0 {
			return remoteConfig{}, fmt.Errorf(
				"%w: remote repository config: %s must not be negative (got %d)", ErrInvalidRepoConfig, f.name, f.value)
		}
		switch f.name {
		case "retrievalCachePeriodSecs":
			out.RetrievalCachePeriodSecs = f.value
		case "missedRetrievalCachePeriodSecs":
			out.MissedRetrievalCachePeriodSecs = f.value
		case "socketTimeoutSecs":
			out.SocketTimeoutSecs = f.value
		case "assumedOfflinePeriodSecs":
			out.AssumedOfflinePeriodSecs = f.value
		}
	}
	if in.HardFail != nil {
		out.HardFail = *in.HardFail
	}
	if in.AllowPrivateUpstream != nil {
		out.AllowPrivateUpstream = *in.AllowPrivateUpstream
	}
	if in.PriorityResolution != nil {
		out.PriorityResolution = *in.PriorityResolution
	}
	return out, nil
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

// validateLocalConfig type-checks the cross-cutting fields of a LOCAL
// repository config (M1's passthrough contract keeps the blob caller-owned;
// only the fields this package's own consumers read are validated).
// priorityResolution is the virtual-resolution member mark (PRD C3 two-bucket
// order); it must be a boolean when present so T-71's reader never trips over
// a hand-mangled blob.
func validateLocalConfig(config string) error {
	if !strings.Contains(config, "priorityResolution") {
		return nil
	}
	var probe struct {
		PriorityResolution *bool `json:"priorityResolution"`
	}
	if err := json.Unmarshal([]byte(config), &probe); err != nil {
		return fmt.Errorf("%w: priorityResolution must be a boolean: %w", ErrInvalidRepoConfig, err)
	}
	return nil
}
