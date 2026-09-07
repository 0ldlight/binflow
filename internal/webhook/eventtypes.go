package webhook

import "fmt"

// The event-type closed set (webhook.md section 3: 13 domains, 66 event
// types; ADR-0041 decision 7's single source of truth). Every type carries
// its domain and its Source: wired types have BinFlow trigger sources (the
// nine M13织入点 — the Emit seams in internal/repo and httpapi's property
// family), dormant types are subscribable, validate green and NEVER fire
// (their domains have no BinFlow body yet; flipping one to wired is adding
// an Emit line, never a schema change — event_type is a string column).
//
// Spellings are the official event_type literals verbatim (webhook.md 3,
// high confidence). Two documented quirks are honored exactly as the spec
// pins them: distribution uses delete_* (the payload spelling, not the
// page's deletion_* section titles — webhook.md 10.2), and curation's four
// types are the page's title spellings because that is the only form the
// official doc registers (webhook.md 3.10, mid confidence; dormant either
// way, so the wire never carries them).

// Source is one event type's BinFlow trigger-source coverage.
type Source string

const (
	// SourceWired: BinFlow has a body domain and an Emit seam for the type;
	// subscriptions fire.
	SourceWired Source = "wired"
	// SourceDormant: subscribable, validation-green, zero triggers (the
	// honest no-trigger state — never faked, presented as such).
	SourceDormant Source = "dormant"
)

// Event domains (webhook.md section 3's thirteen).
const (
	DomainArtifact                 = "artifact"
	DomainArtifactProperty         = "artifact_property"
	DomainDocker                   = "docker"
	DomainBuild                    = "build"
	DomainReleaseBundle            = "release_bundle"
	DomainReleaseBundleV2          = "release_bundle_v2"
	DomainReleaseBundleV2Promotion = "release_bundle_v2_promotion"
	DomainDistribution             = "distribution"
	DomainDestination              = "destination"
	DomainCuration                 = "curation"
	DomainUser                     = "user"
	DomainXrayScanStatus           = "xray_scan_status"
	DomainAppTrust                 = "app_trust"
)

// The twelve wired event types: webhook.md 0's "9 个有本体触发源" M13
// ruling (artifact 5 + artifact_property 2 + docker 2) plus the build
// domain's three (M17 T-510, ADR-0045 decision 7 — the build body landed).
const (
	TypeArtifactDeployed = "deployed"
	TypeArtifactDeleted  = "deleted"
	TypeArtifactMoved    = "moved"
	TypeArtifactCopied   = "copied"
	TypeArtifactCached   = "cached"
	TypePropAdded        = "added"
	TypePropDeleted      = "deleted"
	TypeDockerPushed     = "pushed"
	TypeDockerDeleted    = "deleted"
	TypeBuildUploaded    = "uploaded"
	TypeBuildDeleted     = "deleted"
	TypeBuildPromoted    = "promoted"
)

// eventType is one closed-set entry.
type eventType struct {
	Name   string
	Domain string
	Source Source
}

// eventTypes is the full 66-entry registry. The count is asserted at init
// (TestT362EventClosedSet pins it too) so a typo'd edit cannot silently
// shrink the subscribable surface.
var eventTypes = []eventType{
	// artifact (5) — all wired (webhook.md 3.1).
	{TypeArtifactDeployed, DomainArtifact, SourceWired},
	{TypeArtifactDeleted, DomainArtifact, SourceWired},
	{TypeArtifactMoved, DomainArtifact, SourceWired},
	{TypeArtifactCopied, DomainArtifact, SourceWired},
	{TypeArtifactCached, DomainArtifact, SourceWired},
	// artifact_property (2) — all wired (webhook.md 3.2).
	{TypePropAdded, DomainArtifactProperty, SourceWired},
	{TypePropDeleted, DomainArtifactProperty, SourceWired},
	// docker (3) — pushed/deleted wired; promoted dormant (the promotion
	// REST is Build-info adjacent, PRD M14+ — no faked trigger; webhook.md 3.3).
	{TypeDockerPushed, DomainDocker, SourceWired},
	{TypeDockerDeleted, DomainDocker, SourceWired},
	{"promoted", DomainDocker, SourceDormant},
	// build (3) — all wired (M17 T-510, ADR-0045 decision 7: upload/append
	// → uploaded, retention's discard arm → deleted, promote → promoted;
	// webhook.md 3.4).
	{TypeBuildUploaded, DomainBuild, SourceWired},
	{TypeBuildDeleted, DomainBuild, SourceWired},
	{TypeBuildPromoted, DomainBuild, SourceWired},
	// release_bundle (3) — dormant, RBv1 not built (webhook.md 3.5).
	{"created", DomainReleaseBundle, SourceDormant},
	{"signed", DomainReleaseBundle, SourceDormant},
	{"deleted", DomainReleaseBundle, SourceDormant},
	// release_bundle_v2 (3) — dormant (webhook.md 3.6).
	{"release_bundle_v2_started", DomainReleaseBundleV2, SourceDormant},
	{"release_bundle_v2_failed", DomainReleaseBundleV2, SourceDormant},
	{"release_bundle_v2_completed", DomainReleaseBundleV2, SourceDormant},
	// release_bundle_v2_promotion (3) — dormant (webhook.md 3.7).
	{"release_bundle_v2_promotion_started", DomainReleaseBundleV2Promotion, SourceDormant},
	{"release_bundle_v2_promotion_failed", DomainReleaseBundleV2Promotion, SourceDormant},
	{"release_bundle_v2_promotion_completed", DomainReleaseBundleV2Promotion, SourceDormant},
	// distribution (7) — dormant; delete_* is the payload spelling
	// (webhook.md 3.8 / 10.2).
	{"distribute_started", DomainDistribution, SourceDormant},
	{"distribute_completed", DomainDistribution, SourceDormant},
	{"distribute_aborted", DomainDistribution, SourceDormant},
	{"distribute_failed", DomainDistribution, SourceDormant},
	{"delete_started", DomainDistribution, SourceDormant},
	{"delete_completed", DomainDistribution, SourceDormant},
	{"delete_failed", DomainDistribution, SourceDormant},
	// destination (4) — dormant, Edge nodes not built (webhook.md 3.9).
	{"received", DomainDestination, SourceDormant},
	{"delete_started", DomainDestination, SourceDormant},
	{"delete_completed", DomainDestination, SourceDormant},
	{"delete_failed", DomainDestination, SourceDormant},
	// curation (4) — dormant; the page's title spellings are the only
	// registered form (webhook.md 3.10, mid confidence).
	{"Package was blocked by Curation", DomainCuration, SourceDormant},
	{"Curation Waiver Request Created", DomainCuration, SourceDormant},
	{"Curation Waiver Request Updated", DomainCuration, SourceDormant},
	{"Curation Policy Changed", DomainCuration, SourceDormant},
	// user (1) — dormant, no failed-login lockout body (webhook.md 3.11).
	{"locked", DomainUser, SourceDormant},
	// xray_scan_status (4) — dormant, Xray is a product non-goal
	// (webhook.md 3.12).
	{"done", DomainXrayScanStatus, SourceDormant},
	{"failed", DomainXrayScanStatus, SourceDormant},
	{"partial", DomainXrayScanStatus, SourceDormant},
	{"not_supported", DomainXrayScanStatus, SourceDormant},
	// app_trust (24) — dormant, external product (webhook.md 3.13).
	{"entry_gate_evaluation_started", DomainAppTrust, SourceDormant},
	{"entry_gate_evaluation_validation_passed", DomainAppTrust, SourceDormant},
	{"entry_gate_evaluation_validation_failed", DomainAppTrust, SourceDormant},
	{"exit_gate_evaluation_started", DomainAppTrust, SourceDormant},
	{"exit_gate_evaluation_validation_passed", DomainAppTrust, SourceDormant},
	{"exit_gate_evaluation_validation_failed", DomainAppTrust, SourceDormant},
	{"application_creation_started", DomainAppTrust, SourceDormant},
	{"application_creation_completed", DomainAppTrust, SourceDormant},
	{"application_creation_failed", DomainAppTrust, SourceDormant},
	{"application_update_started", DomainAppTrust, SourceDormant},
	{"application_update_completed", DomainAppTrust, SourceDormant},
	{"application_update_failed", DomainAppTrust, SourceDormant},
	{"application_deletion_started", DomainAppTrust, SourceDormant},
	{"application_deletion_completed", DomainAppTrust, SourceDormant},
	{"application_deletion_failed", DomainAppTrust, SourceDormant},
	{"version_creation_started", DomainAppTrust, SourceDormant},
	{"version_creation_completed", DomainAppTrust, SourceDormant},
	{"version_creation_failed", DomainAppTrust, SourceDormant},
	{"version_promotion_started", DomainAppTrust, SourceDormant},
	{"version_promotion_completed", DomainAppTrust, SourceDormant},
	{"version_promotion_failed", DomainAppTrust, SourceDormant},
	{"release_started", DomainAppTrust, SourceDormant},
	{"release_completed", DomainAppTrust, SourceDormant},
	{"release_failed", DomainAppTrust, SourceDormant},
}

// lookup indexes the registry by the (domain, event_type) pair — names
// collide across domains ("deleted" in artifact/docker/build,
// "delete_failed" in distribution/destination), so every validation and
// match goes through the pair, never the bare name.
var lookup = func() map[[2]string]eventType {
	m := make(map[[2]string]eventType, len(eventTypes))
	for _, et := range eventTypes {
		m[[2]string{et.Domain, et.Name}] = et
	}
	return m
}()

// domains is the thirteen-domain closed set in registry order.
var domains = func() []string {
	seen := map[string]bool{}
	var out []string
	for _, et := range eventTypes {
		if !seen[et.Domain] {
			seen[et.Domain] = true
			out = append(out, et.Domain)
		}
	}
	return out
}()

// Domains returns the domain closed set (subscription validation's first
// 400 gate).
func Domains() []string { return append([]string(nil), domains...) }

// Lookup resolves one (domain, event_type) pair. ok is false for an
// unknown pair — the caller's 400.
func Lookup(domain, eventType string) (et eventType, ok bool) {
	et, ok = lookup[[2]string{domain, eventType}]
	return et, ok
}

// Wired reports whether the pair has a BinFlow trigger source (Emit's
// defensive gate: a dormant type arriving at the bus is a programming
// error, logged and dropped — never an invented trigger).
func Wired(domain, eventType string) bool {
	et, ok := lookup[[2]string{domain, eventType}]
	return ok && et.Source == SourceWired
}

// EventTypesOfDomain lists a domain's registered types (validation error
// messages name the legal set).
func EventTypesOfDomain(domain string) []string {
	var out []string
	for _, et := range eventTypes {
		if et.Domain == domain {
			out = append(out, et.Name)
		}
	}
	return out
}

// ValidDomain reports whether domain is in the closed set.
func ValidDomain(domain string) bool {
	for _, d := range domains {
		if d == domain {
			return true
		}
	}
	return false
}

// String renders one registry entry for logs.
func (e eventType) String() string {
	return fmt.Sprintf("%s/%s (%s)", e.Domain, e.Name, e.Source)
}
