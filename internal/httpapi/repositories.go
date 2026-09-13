package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// Artifactory-compatible repository CRUD (rest-api.md section 2; PRD
// E-04..E-08). All routes demand authentication; the gates are the M7
// capability split (ADR-0026): the inventory list and create/delete sit on
// repo:read/repo:write, the single-repo family on CanManageRepo — enforced
// by the route gates in router.go (plus the create-arm split in
// handleRepoPut), not re-checked on the read paths here (the terminal
// handlers run only after the gate passed).
//
// Success bodies for PUT/DELETE are plain text (spec wording); failures use
// the errors[] envelope (the repository plane belongs to the generic error
// layer, PRD section 5.1 three-format split).

// repoListItem is one GET /api/repositories entry (rest-api.md section 2,
// RepoDetails subset): type is lowercase, url is the context URL of the repo.
type repoListItem struct {
	Key           string `json:"key"`
	Description   string `json:"description"`
	Type          string `json:"type"`
	PackageType   string `json:"packageType"`
	URL           string `json:"url"`
	Configuration any    `json:"configuration,omitempty"`
}

// repoConfig is the single-repository configuration body (GET and PUT share
// the shape; rclass echoes the BinFlow row's type). M3 (T-80, FR-15) carries
// the remote/virtual field subset THROUGH to the service layer's typed
// config (internal/repo/config.go): the REST plane is field transport only —
// validation, defaults, canonicalization and credential masking all live in
// repo.Service.
type repoConfig struct {
	Key             string `json:"key"`
	RClass          string `json:"rclass"`
	PackageType     string `json:"packageType"`
	Description     string `json:"description"`
	URL             string `json:"url"`
	Notes           string `json:"notes,omitempty"`
	IncludesPattern string `json:"includesPattern,omitempty"`
	ExcludesPattern string `json:"excludesPattern,omitempty"`

	// ---- T-490 (FR-156.1): the B-1.5 configJSON four-domain family plus
	// the Stage domain, finally FORWARDED (the M16 T-439 decode-only drift
	// closes) ----
	//
	// repoLayoutRef/blackedOut/maxUniqueSnapshots/archiveBrowsingEnabled
	// were decoded here since M1 but silently dropped by every configJSON
	// arm — PUT answered 200 and GET echoed nothing (the T-439 tripwire's
	// pinned drift). They now ride the LOCAL arm verbatim (the maven policy
	// family's passthrough posture): blackedOut gains its write-plane
	// behavior (repo.refuseBlackedOut — the 404 of rest-api.md section 1.2
	// step 6), repoLayoutRef stays presentation-only (K73's pin ruling:
	// layout parsing is protocol-adapter-fixed; the stored value documents,
	// it does not switch — ADR-0003 erratum, PRD K73 backfill), and
	// maxUniqueSnapshots/archiveBrowsingEnabled are round-trip seats (the
	// snapshot cleanup engine and the archive-content gate are future
	// tickets' seams, registered in the T-490 report).
	//
	// maxUniqueSnapshots is a POINTER (K71's wire posture): the product
	// default IS 0 (rest-api.md section 2), so an explicit 0 must survive
	// the round trip instead of collapsing into "absent" like the old
	// omitempty int did.
	//
	// The Stage domain (7.84 Environments → 7.161 Stage) carries TWO wire
	// spellings: environments (canonical, the official REST reference's
	// documented key) and stages (the 7.161-era alias). Both ride verbatim;
	// repo.Service owns the one cross-cutting rule — the two spellings may
	// not disagree (validateLocalConfig).
	RepoLayoutRef          string   `json:"repoLayoutRef,omitempty"`
	BlackedOut             *bool    `json:"blackedOut,omitempty"`
	MaxUniqueSnapshots     *int     `json:"maxUniqueSnapshots,omitempty"`
	ArchiveBrowsingEnabled *bool    `json:"archiveBrowsingEnabled,omitempty"`
	Environments           []string `json:"environments,omitempty"`
	Stages                 []string `json:"stages,omitempty"`

	HandleReleases          *bool  `json:"handleReleases,omitempty"`
	HandleSnapshots         *bool  `json:"handleSnapshots,omitempty"`
	SnapshotVersionBehavior string `json:"snapshotVersionBehavior,omitempty"`
	ChecksumPolicyType      string `json:"checksumPolicyType,omitempty"`

	// ---- M3 remote transport (FR-15; repo-semantics section 7.1 spellings) ----
	//
	// Username/Password ride as RAW JSON (the enableTokenAuthentication
	// posture) since ADR-0050: the credential pair's merge semantics need
	// KEY PRESENCE plus the null-vs-value split to survive this transport —
	// a typed string flattens null onto "" and a pointer flattens null onto
	// nil, both indistinguishable from absent. The raw bytes ride verbatim
	// into the config blob; repo.Service owns the typing gate and the
	// merge/clear decision (never persisted — NFR-S14).
	Username                       json.RawMessage `json:"username,omitempty"`
	Password                       json.RawMessage `json:"password,omitempty"` // transport only; repo.Service never persists it (NFR-S14)
	RetrievalCachePeriodSecs       *int64          `json:"retrievalCachePeriodSecs,omitempty"`
	MissedRetrievalCachePeriodSecs *int64          `json:"missedRetrievalCachePeriodSecs,omitempty"`
	SocketTimeoutSecs              *int64          `json:"socketTimeoutSecs,omitempty"`
	AssumedOfflinePeriodSecs       *int64          `json:"assumedOfflinePeriodSecs,omitempty"`
	HardFail                       *bool           `json:"hardFail,omitempty"`
	AllowPrivateUpstream           *bool           `json:"allowPrivateUpstream,omitempty"`

	// ---- T-495 (FR-158): the metadata TTL wire knob ----
	//
	// metadataRetrievalCachePeriodSecs is the window the remote
	// enumeration snapshot AND the pull-through metadata cache rows are
	// held for (remote-browsing.md §1's "cached per the Metadata Retrieval
	// Cache Period" semantics). It was a 600s hardwired constant until
	// T-495 — T-461's registered leftover ("枚举快照 TTL 非 wire 可调": the
	// degraded e2e had to swap the upstream URL to invalidate the snapshot
	// signature, because no PUT could shrink the window). POINTER per the
	// family posture (absent keeps the stored value, an explicit 0 keeps
	// the 600s product default, a negative value is repo.Service's by-name
	// 400), REMOTE arm only, typing rides the decode.
	MetadataRetrievalCachePeriodSecs *int64 `json:"metadataRetrievalCachePeriodSecs,omitempty"`

	// ---- D-T456-1 remote-browsing optional档 transport (T-448, FR-147.2;
	// remote-browsing.md section 1 / repo-semantics 7.1) ----
	//
	// listRemoteFolderItems is the remote repository's upstream-merge switch:
	// true makes a directory listing merge the enumeration engine's
	// display-only derived rows beside the cache rows (batch-1 types only);
	// false — the product default — keeps the pre-T-448 cached-rows-only
	// posture. The field was missing from this struct since T-448: PUT bodies
	// carrying it were silently swallowed by the decode (an unknown field to
	// json.Decoder) and the mistyped-value 400 was unreachable for the same
	// reason. Same posture as hardFail above: a flat POINTER field (an
	// explicit false must survive the round trip so the flip-off update
	// works), REMOTE arm only (the knob reads the remote canonical config),
	// typing rides the decode (a mistyped value is a 400 naming the field),
	// and the true-outside-batch-1 by-name refusal stays repo.Service's
	// (parseRemoteConfig — one gate, no wire-side mirror to drift).
	ListRemoteFolderItems *bool `json:"listRemoteFolderItems,omitempty"`

	// ---- T-290 smart remote effective subset (FR-90.2; PRD/LC-12 and
	// artifactory.xsd spellings — aliases resolve inside repo.Service, an
	// M11-ruled name is refused there with a 400) ----

	SocketTimeoutMs                   *int64 `json:"socketTimeoutMs,omitempty"`     // ms granularity; wins over socketTimeoutSecs
	SocketTimeoutMillis               *int64 `json:"socketTimeoutMillis,omitempty"` // artifactory.xsd spelling of socketTimeoutMs
	MetadataRetrievalTimeoutSecs      *int64 `json:"metadataRetrievalTimeoutSecs,omitempty"`
	MissRetrievalCachePeriodSecs      *int64 `json:"missRetrievalCachePeriodSecs,omitempty"`      // PRD spelling; alias of missedRetrievalCachePeriodSecs
	UnusedArtifactsCleanupPeriodHours *int64 `json:"unusedArtifactsCleanupPeriodHours,omitempty"` // field-only (engine M11)

	// The smart remote replication pair (T-317, FR-101.1 — the M10-era
	// by-name 400 is retired) stays RawMessage transport: any JSON value
	// shape rides through verbatim so repo.Service's single validation
	// point owns the typing (a non-object contentSynchronisation answers
	// its 400 naming the field there).
	EnableTokenAuthentication *json.RawMessage `json:"enableTokenAuthentication,omitempty"`
	ContentSynchronisation    *json.RawMessage `json:"contentSynchronisation,omitempty"`

	// ChartsBaseURL is the helm remote's divergent charts fetch base
	// (T-367, FR-117; helm.md section 6.1): transport only — repo.Service
	// owns the per-protocol refusal (the field names a helm remote
	// behavior; any other package type's PUT answers a by-name 400 there).
	ChartsBaseURL string `json:"chartsBaseUrl,omitempty"`

	// PriorityResolution is the per-repository virtual-resolution mark
	// (PRD C3's two-bucket order); legal on local and remote members.
	PriorityResolution *bool `json:"priorityResolution,omitempty"`

	// ---- M3 virtual transport (FR-15; repo-semantics section 8.2) ----

	Repositories             []string `json:"repositories,omitempty"`
	DefaultDeploymentRepo    string   `json:"defaultDeploymentRepo,omitempty"`
	DefaultDeploymentRepoRef string   `json:"defaultDeploymentRepoRef,omitempty"`
	DeploymentRepository     string   `json:"deploymentRepository,omitempty"`

	// QuotaBytes is the M4 governance ceiling (FR-31/GE-05, T-95): 0 =
	// unlimited, the default. A POINTER so an explicit 0 round-trips through
	// the stored config (the FE's "0 = 不限" spelling stays stable) — the
	// value rides the LOCAL arm of configJSON only; remote/virtual configs
	// are canonicalized by repo.Service and would drop it anyway, and the
	// write plane it governs is the local one.
	QuotaBytes *int64 `json:"quotaBytes,omitempty"`

	// ---- T-327R deb/rpm policy transport (the D-B unlock) ----
	//
	// The index-engine policy keys the deb/rpm adapters read verbatim off
	// the config blob (their RepoConfig probes; debian.md sections 3-5 /
	// rpm.md sections 2.4, 3.2 and 4). The spellings are the Artifactory
	// flat field names — no sub-object, matching both the adapters' probes
	// and the maven policy family's long-standing posture on this struct.
	// Like that family, the keys ride the LOCAL arm only (the deb/rpm index
	// engines serve local repositories; the reindex family refuses remote,
	// and virtual resolution reads the members' own trees). POINTER fields
	// keep an explicit false/0 distinct from absent — load-bearing for
	// calculateYumMetadata, whose product default IS false (RP-2): the
	// flip-off update must survive the round trip, or the 409 branch could
	// never be turned off again.
	//
	// Validation posture stays the architecture's: this plane transports,
	// the config blob stays caller-owned and repo.Service only type-checks
	// its own cross-cutting fields. Typing rides the decode (a mistyped
	// value fails the body decode with the field named, 400); value
	// semantics (the byHash enum, normalization to spec defaults) belong to
	// the adapters' own normalized() reads.
	ByHash                          string   `json:"byHash,omitempty"`                          // deb: ALL / SHA256 / NONE (default NONE)
	OptionalIndexCompressionFormats []string `json:"optionalIndexCompressionFormats,omitempty"` // deb: xz / lzma spellings
	DebianDefaultArchitectures      string   `json:"debianDefaultArchitectures,omitempty"`      // deb: TL-4 forced families (default i386,amd64; "none" opts out)
	HistoryCycles                   *int64   `json:"historyCycles,omitempty"`                   // deb: by-hash generations to keep (default 3)
	Origin                          string   `json:"origin,omitempty"`                          // deb: Release Origin (default: repo key)
	Label                           string   `json:"label,omitempty"`                           // deb: Release Label (default: repo key)

	CalculateYumMetadata    *bool  `json:"calculateYumMetadata,omitempty"`    // rpm: the RP-2 opt-in (default false)
	YumRootDepth            *int64 `json:"yumRootDepth,omitempty"`            // rpm: repodata root depth (default 0 = repository root)
	EnableFileListsIndexing *bool  `json:"enableFileListsIndexing,omitempty"` // rpm: the filelists index switch (default false)
	YumGroupFileNames       string `json:"yumGroupFileNames,omitempty"`       // rpm: comps group file list (default comps.xml)

	// ---- T-329 D-E helm enforce layout transport (the D-E unlock) ----
	//
	// The helm Enforce Layout switch pair (helm.md section 4.3 / S5; the
	// adapter probe parseEnforcePolicy reads the Artifactory flat spellings
	// verbatim off the config blob). T-327R carried the deb/rpm policy keys
	// but missed this pair, so a PUT answered 200 and silently dropped them
	// — enforce could never be switched on over REST (T-329's L32 arm).
	// Same posture as the deb/rpm family: flat POINTER fields (an explicit
	// false must survive the round trip so the flip-off update works — both
	// product defaults ARE false), LOCAL arm only (the policy judges the
	// local upload hook), package-type-agnostic storage, typing rides the
	// decode (a mistyped value is a 400 naming the field).
	ForceMetadataNameVersion *bool `json:"forceMetadataNameVersion,omitempty"` // helm: Enforce Chart Name and Version (default false)
	ForceNonDuplicateChart   *bool `json:"forceNonDuplicateChart,omitempty"`   // helm: Prevent Duplicate Chart Paths (default false)

	// ---- T-355A conan forced-authentication transport (the D-5 unlock) ----
	//
	// The conan repo-config switch (conan.md section 2's auth gate: when
	// true, an anonymous request to any conan endpoint answers 401 — the
	// client-guiding challenge; default false keeps the ordinary content
	// ACL). Same posture as the deb/rpm/helm families above: a flat POINTER
	// field (an explicit false must survive the round trip so the flip-off
	// update works — the product default IS false), LOCAL arm only (the
	// remote/virtual canonical forms drop the key by design, and the knob's
	// write plane is the local one), typing rides the decode (a mistyped
	// value is a 400 naming the field). T-351's D-5 evidence was exactly
	// the pre-fix shape: PUT 200, GET keyless, anonymous ping 200.
	ForceConanAuthentication *bool `json:"forceConanAuthentication,omitempty"` // conan: anonymous plane demands credentials (default false)

	// Configuration is the GET-only echo of the stored canonical config (the
	// service hands it back already masked, NFR-S14); it is never an input.
	Configuration any `json:"configuration,omitempty"`
}

// setStr/setI64/setBool collect one set transport field into the config map.
func setStr(m map[string]any, key, v string) {
	if v != "" {
		m[key] = v
	}
}

func setI64(m map[string]any, key string, v *int64) {
	if v != nil {
		m[key] = *v
	}
}

func setBool(m map[string]any, key string, v *bool) {
	if v != nil {
		m[key] = *v
	}
}

// setInt collects one int-pointer transport field into the config map
// (T-490's maxUniqueSnapshots posture: an explicit 0 is a value, not an
// absence — the product default round-trips).
func setInt(m map[string]any, key string, v *int) {
	if v != nil {
		m[key] = *v
	}
}

// setRawJSON collects one raw-JSON transport field into the config map (the
// smart remote pair rides verbatim so repo.Service sees the exact shape).
func setRawJSON(m map[string]any, key string, v *json.RawMessage) {
	if v != nil {
		m[key] = *v
	}
}

// setStrSlice collects one string-slice transport field into the config map
// (nil = the field was absent; an explicit empty array still lands, the same
// absent-vs-empty split every pointer field on this struct keeps).
func setStrSlice(m map[string]any, key string, v []string) {
	if v != nil {
		m[key] = v
	}
}

// configJSON renders the request body's type-relevant fields into the config
// blob repo.Service parses. rclass is the EFFECTIVE class (the body's value,
// defaulted from the stored row by the caller); the map keeps only the
// fields the caller actually set, so "" — the "nothing type-relevant was in
// the body" result — reaches the service as the keep-current-config signal
// on update. repo.Service owns everything beyond this point: url required,
// the member rules, the docker-combination matrix, the defaults, the
// credential drop.
func (c repoConfig) configJSON(rclass string) (string, error) {
	m := map[string]any{}
	switch rclass {
	case repo.TypeRemote:
		setStr(m, "url", c.URL)
		// ADR-0050: the credential pair rides VERBATIM (null included) so
		// repo.Service's raw-presence merge can tell omitted (keep) from an
		// explicit null/"" (clear). setRawJSON is not used because these
		// seats are values, not pointers.
		if len(c.Username) > 0 {
			m["username"] = c.Username
		}
		if len(c.Password) > 0 {
			m["password"] = c.Password
		}
		setI64(m, "retrievalCachePeriodSecs", c.RetrievalCachePeriodSecs)
		setI64(m, "missedRetrievalCachePeriodSecs", c.MissedRetrievalCachePeriodSecs)
		setI64(m, "socketTimeoutSecs", c.SocketTimeoutSecs)
		setI64(m, "assumedOfflinePeriodSecs", c.AssumedOfflinePeriodSecs)
		setI64(m, "socketTimeoutMs", c.SocketTimeoutMs)
		setI64(m, "socketTimeoutMillis", c.SocketTimeoutMillis)
		setI64(m, "metadataRetrievalTimeoutSecs", c.MetadataRetrievalTimeoutSecs)
		setI64(m, "metadataRetrievalCachePeriodSecs", c.MetadataRetrievalCachePeriodSecs)
		setI64(m, "missRetrievalCachePeriodSecs", c.MissRetrievalCachePeriodSecs)
		setI64(m, "unusedArtifactsCleanupPeriodHours", c.UnusedArtifactsCleanupPeriodHours)
		setRawJSON(m, "enableTokenAuthentication", c.EnableTokenAuthentication)
		setRawJSON(m, "contentSynchronisation", c.ContentSynchronisation)
		setStr(m, "chartsBaseUrl", c.ChartsBaseURL)
		setBool(m, "hardFail", c.HardFail)
		setBool(m, "allowPrivateUpstream", c.AllowPrivateUpstream)
		setBool(m, "priorityResolution", c.PriorityResolution)
		// D-T456-1: the remote-browsing optional档 finally rides the config
		// blob — repo.Service's parseRemoteConfig owns its typing gate (a
		// `true` outside the batch-1 set refuses by name there).
		setBool(m, "listRemoteFolderItems", c.ListRemoteFolderItems)
		// L006-A (D02-R03/R04): the four round-trip domains ride the REMOTE
		// arm too — the live reference echoes all four on a remote
		// repository (repoLayoutRef defaulting to maven-2-default,
		// blackedOut/maxUniqueSnapshots/archiveBrowsingEnabled verbatim).
		// repo.Service's parseRemoteConfig owns defaults and typing.
		setStr(m, "repoLayoutRef", c.RepoLayoutRef)
		setBool(m, "blackedOut", c.BlackedOut)
		setInt(m, "maxUniqueSnapshots", c.MaxUniqueSnapshots)
		setBool(m, "archiveBrowsingEnabled", c.ArchiveBrowsingEnabled)
	case repo.TypeVirtual:
		if c.Repositories != nil {
			m["repositories"] = c.Repositories
		}
		setStr(m, "defaultDeploymentRepo", c.DefaultDeploymentRepo)
		setStr(m, "defaultDeploymentRepoRef", c.DefaultDeploymentRepoRef)
		setStr(m, "deploymentRepository", c.DeploymentRepository)
		// L006-A (D02-R03/R04): the VIRTUAL arm keeps repoLayoutRef ONLY —
		// the live reference's virtual echo carries it when set, omits it
		// when not, and DROPS blackedOut/maxUniqueSnapshots/
		// archiveBrowsingEnabled sent to a virtual repository. BinFlow
		// copies that scope (those three spellings fall to the unknown-key
		// tolerance inside parseVirtualConfig, same as every field the
		// reference itself ignores).
		setStr(m, "repoLayoutRef", c.RepoLayoutRef)
	default:
		// local: the cross-cutting member mark plus the maven policy
		// family (T-67's consumers read them verbatim out of the config
		// blob — the T-64 passthrough contract; the transport addition is
		// the piece T-80 deferred to this ticket). The M4 governance fields
		// (T-95) ride the same passthrough: includesPattern/excludesPattern
		// (the struct's long-standing transport fields, finally forwarded)
		// and quotaBytes — repo.Service validates and the gates enforce.
		// T-327R (the D-B unlock): the deb/rpm index-engine policy keys ride
		// the same verbatim passthrough — package-type-agnostic like the
		// maven family (a debian key on an rpm repository is stored and
		// ignored, the caller-owned blob posture), read by exactly the
		// adapter whose repository it is.
		setBool(m, "priorityResolution", c.PriorityResolution)
		// T-490 (FR-156.1): the B-1.5 four-domain family plus the Stage
		// domain ride the same verbatim passthrough — the drift T-439 pinned
		// (decode-only, configJSON dropped all four) closes here. blackedOut
		// is consumed by the write plane (repo.refuseBlackedOut);
		// repoLayoutRef is the K73 presentation seat; maxUniqueSnapshots and
		// archiveBrowsingEnabled are stored round-trip seats; environments
		// and stages are the Stage domain's two spellings (the disagreement
		// rule is repo.Service's — validateLocalConfig).
		setStr(m, "repoLayoutRef", c.RepoLayoutRef)
		setBool(m, "blackedOut", c.BlackedOut)
		setInt(m, "maxUniqueSnapshots", c.MaxUniqueSnapshots)
		setBool(m, "archiveBrowsingEnabled", c.ArchiveBrowsingEnabled)
		setStrSlice(m, "environments", c.Environments)
		setStrSlice(m, "stages", c.Stages)
		setBool(m, "handleReleases", c.HandleReleases)
		setBool(m, "handleSnapshots", c.HandleSnapshots)
		setStr(m, "snapshotVersionBehavior", c.SnapshotVersionBehavior)
		setStr(m, "checksumPolicyType", c.ChecksumPolicyType)
		setStr(m, "includesPattern", c.IncludesPattern)
		setStr(m, "excludesPattern", c.ExcludesPattern)
		setI64(m, "quotaBytes", c.QuotaBytes)
		setStr(m, "byHash", c.ByHash)
		setStrSlice(m, "optionalIndexCompressionFormats", c.OptionalIndexCompressionFormats)
		setStr(m, "debianDefaultArchitectures", c.DebianDefaultArchitectures)
		setI64(m, "historyCycles", c.HistoryCycles)
		setStr(m, "origin", c.Origin)
		setStr(m, "label", c.Label)
		setBool(m, "calculateYumMetadata", c.CalculateYumMetadata)
		setI64(m, "yumRootDepth", c.YumRootDepth)
		setBool(m, "enableFileListsIndexing", c.EnableFileListsIndexing)
		setStr(m, "yumGroupFileNames", c.YumGroupFileNames)
		// T-329 D-E: the helm enforce layout pair rides the same verbatim
		// passthrough (the adapter's parseEnforcePolicy reads these exact
		// spellings off the stored blob).
		setBool(m, "forceMetadataNameVersion", c.ForceMetadataNameVersion)
		setBool(m, "forceNonDuplicateChart", c.ForceNonDuplicateChart)
		// T-355A (D-5): the conan forced-authentication switch rides the
		// same verbatim passthrough (the conan adapter's probe reads this
		// exact spelling off the stored blob).
		setBool(m, "forceConanAuthentication", c.ForceConanAuthentication)
	}
	if len(m) == 0 {
		return "", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("render repository config transport: %w", err)
	}
	return string(b), nil
}

// repoTypeOrder ranks types for the list ordering: type ascending, then key
// ascending (rest-api.md section 2). M1 has local only; the full ordering
// table keeps the contract stable the day remote/virtual land.
var repoTypeOrder = map[string]int{
	repo.TypeLocal:   0,
	repo.TypeRemote:  1,
	repo.TypeVirtual: 2,
}

// writeText emits a plain-text success body (repo-management plane).
func writeText(w http.ResponseWriter, status int, body string) {
	writePlainText(w, status, body)
}

// writePlainText is the single plain-text emitter: the nosniff guard keeps
// browsers from content-sniffing these bodies as HTML (gosec G705 posture —
// several of these messages interpolate client-supplied identifiers).
func writePlainText(w http.ResponseWriter, status int, body string) {
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body)) //nolint:gosec // G705: body is operator-controlled plain text under nosniff + text/plain
}

// writeJSONBody emits an indented JSON success body.
func writeJSONBody(w http.ResponseWriter, status int, v any) {
	writeJSONBodyCT(w, status, "application/json", v)
}

// writeJSONBodyCT is writeJSONBody with an explicit content type — the faces
// whose wire contract pins a vendor media type (?list's
// application/vnd.org.jfrog.artifactory.storage.FileList+json, the user
// detail's …security.User+json) share the same rendering path instead of
// forking it. HTML escaping is OFF (L010-2, the escape-asymmetry residual):
// the reference's Jackson serializer emits & < > raw on success bodies too —
// writeError's own L009-3 posture, now shared by the success path so an
// ampersand filename survives the wire byte-for-byte. Encoder.Encode appends
// the one trailing newline MarshalIndent never wrote — trimmed, keeping the
// already-aligned bodies byte-stable.
func writeJSONBodyCT(w http.ResponseWriter, status int, contentType string, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		writeError(w, http.StatusInternalServerError, "render response: "+err.Error())
		return
	}
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
}

// writePlainError emits the plain-text error body of the user/permission
// management plane (PRD section 5.1 three-format split, layer 2).
func writePlainError(w http.ResponseWriter, status int, message string) {
	writePlainText(w, status, message)
}

// handleRepoList serves GET /api/repositories (E-04): every repository the
// caller can see (M1: all of them — the read filter needs per-repo ACL data
// M1 does not carry; the route itself already requires authentication),
// ordered type-then-key, Cache-Control: no-store (rest-api.md section 2).
// M3 (M04, FR-15-AC5) turns the ?type= and ?packageType= filters on: exact
// column matches through ListReposFiltered, invalid values matching nothing
// with an empty array rather than an error (rest-api.md section 2).
// L013 (R-15c/d, matrix D02-R01) adds the ?project= axis as a filter, not a
// tolerated-and-ignored parameter: the live reference answers the empty set
// for an unknown project even with the Projects addon inactive (probe arms
// c1/c3, reports/compatibility/L013-r15-packument-probes.md §1) — the
// ?type=<invalid> family posture on the project axis (c4). BinFlow has no
// projects domain, so no repository can carry a project affiliation and
// "the repositories of project v" is truthfully empty for every v
// (pending-rulings §2 R-15c 案乙) — the [] answer, never the full list a
// project-scoped cleanup script would misread as the silent superset. The
// empty string keeps no-parameter semantics (arm c2: both sides ignore
// it). The match arm (a project's own repositories) waits on a projects
// domain in the repo model — the L013 model gap, not a field fabricated
// here.
func (s *Server) handleRepoList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if strings.TrimSpace(q.Get("project")) != "" {
		w.Header().Set("Cache-Control", "no-store") // the same wire headers as the full path
		writeJSONBody(w, http.StatusOK, []repoListItem{})
		return
	}
	repos, err := s.deps.ReposSvc.ListReposFiltered(r.Context(), principalFrom(r.Context()),
		strings.TrimSpace(q.Get("type")), strings.TrimSpace(q.Get("packageType")))
	if err != nil {
		s.writeRepoSvcError(w, err)
		return
	}
	items := make([]repoListItem, 0, len(repos))
	for _, row := range repos {
		items = append(items, s.repoListItemOf(r, row))
	}
	sort.SliceStable(items, func(i, j int) bool {
		ti, tj := repoTypeOrder[items[i].Type], repoTypeOrder[items[j].Type]
		if ti != tj {
			return ti < tj
		}
		return items[i].Key < items[j].Key
	})
	w.Header().Set("Cache-Control", "no-store")
	writeJSONBody(w, http.StatusOK, items)
}

// repoListItemOf projects one metadata row onto the wire shape. Remote and
// virtual entries carry their (masked, canonical) configuration like the
// single-repo GET does; local rows keep the bare M1 shape.
//
// The url field is <contextUrl>/<key> (rest-api.md section 2, high
// confidence): the context URL carries the product prefix, the same base
// storageURI/downloadURI build on. M1 as-built omitted the /binflow segment
// (the T-445-registered drift ①); the M17 errata (T-493, FR-157①) restores
// the prefixed form family-wide — except a REMOTE row, whose top-level url
// is the upstream (remoteUpstreamURL, D21/L001-5).
func (s *Server) repoListItemOf(r *http.Request, row *metadata.Repo) repoListItem {
	item := repoListItem{
		Key:         row.RepoKey,
		Description: row.Description,
		Type:        row.Type,
		PackageType: row.PackageType,
		URL:         contextURL(r) + "/" + row.RepoKey,
	}
	if row.Config != "" && row.Config != "{}" {
		var m map[string]any
		if err := json.Unmarshal([]byte(row.Config), &m); err == nil && len(m) > 0 {
			item.Configuration = m
		}
	}
	if u, ok := remoteUpstreamURL(row, item.Configuration); ok {
		item.URL = u
	}
	return item
}

// remoteUpstreamURL is the D21 ruling (L001-5; L000-docker-remote-diff E4):
// Artifactory's RepoDetails carries the UPSTREAM url as a remote row's
// top-level url — the self-derived context URL is the local/virtual shape.
// Reads only the "url" key out of the already-masked canonical config
// (NFR-S14: no credential field is consulted); anything but a non-empty
// string falls back to the caller's context URL (the defensive arm for
// rows seeded outside repo.Service's canonicalizer).
func remoteUpstreamURL(row *metadata.Repo, cfg any) (string, bool) {
	if row.Type != repo.TypeRemote {
		return "", false
	}
	m, ok := cfg.(map[string]any)
	if !ok {
		return "", false
	}
	u, _ := m["url"].(string)
	if u == "" {
		return "", false
	}
	return u, true
}

// requestBase is scheme://host as the request presented it (URLs inside
// bodies are derived from the request, never from a configured base in M1;
// config.Server.BaseURL wiring lands with the console milestone).
func requestBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// contextURL is requestBase plus the product prefix: <scheme://host>/binflow,
// Artifactory's <contextUrl> equivalent (rest-api.md sections 0/1.2 — the
// context path is part of every addressed URL the product hands out). The
// single definition the repo-url family (T-493, FR-157①) and the
// storageURI/downloadURI pair share.
func contextURL(r *http.Request) string {
	return requestBase(r) + prefix
}

// handleRepoGet serves GET /api/repositories/{key} (E-05): the full config
// body; unknown key -> 404 with the spec's plain wording wrapped in the
// envelope (BinFlow keeps the envelope for the repository plane, E-01).
func (s *Server) handleRepoGet(w http.ResponseWriter, r *http.Request, key string) {
	row, err := s.deps.ReposSvc.GetRepo(r.Context(), principalFrom(r.Context()), key)
	if err != nil {
		s.writeRepoSvcError(w, err)
		return
	}
	writeJSONBody(w, http.StatusOK, s.repoConfigOf(r, row))
}

// repoConfigOf projects one row onto the configuration body. M3 (T-80):
// remote/virtual rows echo their canonical config under "configuration" —
// the service already handed back the masked form (NFR-S14: no password
// ever crosses this boundary); local rows keep the M1 shape ({} is omitted).
// The url field rides contextURL like the list entry's (T-493, FR-157① —
// the baseUrl family aligns on the prefixed context URL) except a REMOTE
// row, whose top-level url is the upstream (remoteUpstreamURL, D21/L001-5).
func (s *Server) repoConfigOf(r *http.Request, row *metadata.Repo) repoConfig {
	cfg := repoConfig{
		Key:         row.RepoKey,
		RClass:      row.Type,
		PackageType: row.PackageType,
		Description: row.Description,
		URL:         contextURL(r) + "/" + row.RepoKey,
	}
	if row.Config != "" && row.Config != "{}" {
		var m map[string]any
		if err := json.Unmarshal([]byte(row.Config), &m); err == nil && len(m) > 0 {
			cfg.Configuration = m
		}
	}
	if u, ok := remoteUpstreamURL(row, cfg.Configuration); ok {
		cfg.URL = u
	}
	return cfg
}

// canManage asks one management-plane capability of the injected authorizer
// (handler-side arm splits, family 6: the PUT route's create arm knows it
// is a create only after resolving the key). Fails closed without the facet.
func (s *Server) canManage(ctx context.Context, p *auth.Principal, capability auth.ManagementCapability) bool {
	return managementAllowed(s.deps.Authz, func(m auth.ManagementAuthorizer) bool {
		return m.CanManage(ctx, p, capability)
	})
}

// handleRepoPut serves PUT /api/repositories/{key} (E-06/E-07): CREATE only
// since ADR-0050 — the reference's update spelling is POST, and a PUT onto
// an EXISTING key answers the create-only 400 (the reference's literal
// wording, frozen by the L007-3/L008-1b evidence; zero side effects). M3
// (T-80) opens the remote/virtual classes: the type-relevant body fields
// ride through to repo.Service's typed config (E-07's M1 refusal is
// inverted per PRD section 5.6; the docker combinations stay refused —
// that rule is the service's).
//
// The type refusal precedes the key-exists question (live probe A16: a
// rclass-less PUT onto an existing key hits "Missing repository type"
// first): such a body falls into the create path below, which refuses the
// type before the key question can arise.
//
// M7 (ADR-0026, inventory family 6): the route gate is the family-7
// repoManage write gate; the CREATE arm — the key does not exist yet —
// splits here onto the global repo:write capability, which is deliberately
// NOT delegated to manage holders (FR-65: a repo admin cannot create or
// delete repositories).
func (s *Server) handleRepoPut(w http.ResponseWriter, r *http.Request, key string) {
	var body repoConfig
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "request body is not valid repository configuration JSON: "+err.Error())
		return
	}
	// PRD v1.3: BinFlow does not distinguish the body/path key mismatch
	// cases (400 create / 409 update) — one uniform 400.
	if body.Key != "" && body.Key != key {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"repository key in body %q does not match the request path %q", body.Key, key))
		return
	}

	p := principalFrom(r.Context())

	// ADR-0050 decision 2 (PUT = create-only): an existing key refuses with
	// the reference's create-conflict literal, regardless of body
	// completeness, and touches nothing — except a body WITHOUT rclass,
	// which hits the type refusal first (the A16 order) by falling into
	// the create path below.
	_, getErr := s.deps.ReposSvc.GetRepo(r.Context(), p, key)
	switch {
	case getErr == nil && body.RClass != "":
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"error when validating repository name: %s : Repository key already exists", key))
		return
	case getErr != nil && !errors.Is(getErr, repo.ErrRepoNotFound):
		s.writeRepoSvcError(w, getErr)
		return
	}

	stored := &metadata.Repo{
		RepoKey: key, Type: body.RClass, PackageType: body.PackageType,
		Description: body.Description,
	}

	config, err := body.configJSON(stored.Type)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	stored.Config = config
	// Family 6 create arm: repository creation stays on the global
	// repo:write capability (admin-only by invariant). The route already
	// answered the family-7 question; this second door is what keeps a
	// manage holder from minting repositories (FR-65 V08's boundary).
	if !s.canManage(r.Context(), p, auth.CapRepoWrite) {
		writeError(w, http.StatusForbidden, "administrator privileges required")
		return
	}
	// M10 T-282 (FR-86.5's enum half): with the addon registry mounted, the
	// package-type legality question reads the assembly's DYNAMIC slot set —
	// a newly registered package-type addon is a creatable type with zero
	// further branch edits. The 400/403 order is unchanged for every caller:
	// this runs after the capability door, exactly where repo.Service's own
	// enum rejection used to be the only check. The unlock half (a
	// known-but-gated slot's refusal) is T-283's weave.
	if !s.checkAddonPackageType(w, stored.PackageType) {
		return
	}
	created, err := s.deps.ReposSvc.CreateRepo(r.Context(), p, stored)
	if err != nil {
		s.writeRepoSvcError(w, err)
		return
	}
	writeText(w, http.StatusOK, fmt.Sprintf("Successfully created repository '%s'\n", created.RepoKey))
}

// handleRepoPost serves POST /api/repositories/{key} (update spelling,
// rest-api.md section 2): 404 when the key is unknown. Since ADR-0050 the
// description seat MERGES like every other field: an omitted key keeps the
// stored value, an explicit null/""/value overwrites (null decodes as "").
func (s *Server) handleRepoPost(w http.ResponseWriter, r *http.Request, key string) {
	rawBody, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "request body is not valid repository configuration JSON: "+err.Error())
		return
	}
	var body repoConfig
	if err := json.Unmarshal(rawBody, &body); err != nil {
		writeError(w, http.StatusBadRequest, "request body is not valid repository configuration JSON: "+err.Error())
		return
	}
	p := principalFrom(r.Context())
	current, err := s.deps.ReposSvc.GetRepo(r.Context(), p, key)
	if err != nil {
		s.writeRepoSvcError(w, err)
		return
	}
	// ADR-0050 decision 5: description merges — the raw body's key presence
	// decides keep-vs-overwrite (the typed seat cannot: null and absent
	// both decode to "").
	description := body.Description
	var presence map[string]json.RawMessage
	if json.Unmarshal(rawBody, &presence) == nil {
		if _, ok := presence["description"]; !ok {
			description = current.Description
		}
	}
	rclass := body.RClass
	if rclass == "" {
		rclass = current.Type
	}
	packageType := body.PackageType
	if packageType == "" {
		packageType = current.PackageType
	}
	config, err := body.configJSON(rclass)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.deps.ReposSvc.UpdateRepo(r.Context(), p, &metadata.Repo{
		RepoKey: key, Type: rclass, PackageType: packageType,
		Description: description, Config: config,
	}); err != nil {
		s.writeRepoSvcError(w, err)
		return
	}
	writeText(w, http.StatusOK, fmt.Sprintf("Repository %s update successfully.\n", key))
}

// handleRepoDelete serves DELETE /api/repositories/{key} (E-08): empty
// repositories delete directly; non-empty ones demand ?deleteContent=true
// (the 400 message names the flag, FR-3-AC5). Success is a 200 plain-text
// report (rest-api.md section 2).
func (s *Server) handleRepoDelete(w http.ResponseWriter, r *http.Request, key string) {
	deleteContent := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("deleteContent")), "true")
	if err := s.deps.ReposSvc.DeleteRepo(r.Context(), principalFrom(r.Context()), key, deleteContent); err != nil {
		s.writeRepoSvcError(w, err)
		return
	}
	writeText(w, http.StatusOK, fmt.Sprintf("Repository %s deleted successfully.\n", key))
}

// unknownPackageTypeMessage renders the addon-registry plane's unknown-type
// 400: the legal set is the registry's dynamic slot list (M10 T-282), so the
// message derives from the same source — a newly assembled slot appears in
// the guidance without a wording edit.
func unknownPackageTypeMessage(packageType string, known []string) string {
	return fmt.Sprintf("package type %q is not a registered addon slot on this instance; must be one of %s",
		packageType, strings.Join(known, ", "))
}

// writeRepoSvcError maps repo.Service sentinels onto envelope statuses.
func (s *Server) writeRepoSvcError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repo.ErrRepoNotFound):
		writeError(w, http.StatusNotFound, "Repository does not exist: "+err.Error())
	case errors.Is(err, repo.ErrInvalidRepoKey), errors.Is(err, repo.ErrReservedRepoKey),
		errors.Is(err, repo.ErrInvalidRepoConfig), errors.Is(err, repo.ErrInvalidRepoType):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrRepoTypeNotSupported):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrPackageTypeNotAvailable):
		// M10 T-283 (ADR-0032 D3): the addon-plane refusal is a
		// CONFIGURATION validation 400 — the repo validation family's
		// shape, deliberately not a licensing 403 (the PRD↔ADR divergence
		// registered for T-293's K25 ruling). No X-Binflow-License-Required
		// header here: that marker is the data-plane 403's (D2).
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrRepoExists):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrRepoNotEmpty):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", basicChallenge)
		writeError(w, http.StatusUnauthorized, "authentication required")
	case errors.Is(err, repo.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		s.log.Error("httpapi: repository service failure", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "repository operation failed")
	}
}
