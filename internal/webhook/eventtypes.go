package webhook

import "fmt"

// The event-type closed set — ADR-0041 decision 7's single source of
// truth, as amended by its own "翻转路径" clause through the M17-Q5
// ruling (裁①: register only sourced domains — see the ADR's decision 7
// errata). The registration face is CLOSED-DOMAIN: a domain enters the
// set only when BinFlow carries a trigger source for at least one of its
// types, and it leaves the set the moment that ceases to be true.
//
// As of the ruling (build wired in T-510), four domains qualify —
// artifact (5 wired), artifact_property (2), docker (2 wired + promoted
// dormant), build (3) — 13 registered types, 12 of them wired. The nine
// sourceless domains the M13 interim posture carried as dormant
// (release_bundle, release_bundle_v2, release_bundle_v2_promotion,
// distribution, destination, curation, user, xray_scan_status, app_trust)
// are DEREGISTERED: unknown domain 400s on create/update, their stored
// legacy subscriptions stay readable and never fire, and no trigger is
// ever fabricated for them. Re-registering one is a new ruling plus its
// wired trigger seam (the dormant→wired path, schema-zero as ever).
//
// Every type still carries its Source: wired types have BinFlow trigger
// sources (the Emit seams in internal/repo, internal/build and httpapi's
// property family); docker's promoted stays dormant — subscribable,
// validation-green, never fired (the promotion REST has no BinFlow body;
// flipping it is adding an Emit line, never a schema change).
//
// Spellings are the official event_type literals verbatim (webhook.md 3,
// high confidence).

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

// Event domains — the closed-domain set (M17-Q5 裁①): only domains with
// BinFlow trigger sources. The deregistered nine live in
// deregisteredDomains for the honest migration posture (readable legacy
// rows, refused writes), not as subscribable choices.
const (
	DomainArtifact         = "artifact"
	DomainArtifactProperty = "artifact_property"
	DomainDocker           = "docker"
	DomainBuild            = "build"
)

// deregisteredDomains lists the nine sourceless domains the M13 interim
// posture registered as dormant and the M17-Q5 ruling (裁①) removed. The
// list is documentation plus the legacy-row posture's vocabulary — it is
// deliberately NOT part of Domains(), so validation, matching and the
// REST error messages all answer "unknown domain" for these names.
var deregisteredDomains = []string{
	"release_bundle", "release_bundle_v2", "release_bundle_v2_promotion",
	"distribution", "destination", "curation", "user",
	"xray_scan_status", "app_trust",
}

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

// eventTypes is the closed-domain registry. The count is asserted in
// tests (TestT362EventClosedSet, TestBuildDomainRegistryAudit,
// closed_domain_registry_test.go) so a typo'd edit cannot silently
// reshape the subscribable surface.
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

// domains is the closed-domain set in registry order.
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
// 400 gate — the sourced four, M17-Q5 裁①).
func Domains() []string { return append([]string(nil), domains...) }

// DeregisteredDomains lists the sourceless domains the M13 interim
// posture registered as dormant and the M17-Q5 ruling (裁①) removed: the
// honest migration posture's vocabulary (legacy rows stay readable,
// writes 400) and the console/docs face — never a subscribable choice.
func DeregisteredDomains() []string { return append([]string(nil), deregisteredDomains...) }

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
