package search

// Field registry — the closed set of AQL fields BinFlow knows about
// (ADR-0043 pt 2: the registry is the single point of "honest rejection").
//
// Sources of truth:
//   - docs/reverse/aql.md §2.2 (item-domain field table, T-407) — the literal
//     contract authority; §2.3 (property domain), §2.4 (operator set),
//     §2.5 (suffix chain + sort validator copy).
//   - ADR-0043 pt 3 (field→SQL mapping) — names the IR keys T-411 compiles to.
//
// Design rule: a field that Artifactory documents but BinFlow has no storage
// source for is STILL REGISTERED, with Unsupported carrying the reason — so a
// query touching it fails with a 400 naming the field, never a pseudo-empty
// 200 (ADR-0043 pt 2 names modified_by as the canonical case).
//
// This file is pure data: no IO, no database, no dependencies outside stdlib.

// Domain is the AQL entity domain a field belongs to. The M15 query entry is
// the item domain only ("items.find"); property and statistics fields attach
// to items queries via @key / property.* / stat.* paths (aql.md §2.1).
type Domain string

// DomainItem, DomainProperty and DomainStatistics are the AQL entity domains
// (aql.md §2.1: the query entry is items; property/statistics attach via
// @key / stat.* field paths).
const (
	DomainItem       Domain = "item"
	DomainProperty   Domain = "property"
	DomainStatistics Domain = "statistics"
)

// FieldID identifies a registry field independent of how it was written
// (the @key shorthand and the property.key long form resolve to the same ID).
type FieldID string

// FieldRepo through FieldPropertyValue identify registry fields independent
// of how they were written (the @key shorthand and property.key long form
// resolve to the same ID).
const (
	FieldRepo             FieldID = "repo"
	FieldPath             FieldID = "path"
	FieldName             FieldID = "name"
	FieldType             FieldID = "type"
	FieldCreated          FieldID = "created"
	FieldModified         FieldID = "modified"
	FieldUpdated          FieldID = "updated"
	FieldCreatedBy        FieldID = "created_by"
	FieldModifiedBy       FieldID = "modified_by"
	FieldSize             FieldID = "size"
	FieldDepth            FieldID = "depth"
	FieldSha256           FieldID = "sha256"
	FieldActualSHA1       FieldID = "actual_sha1"
	FieldOriginalSHA1     FieldID = "original_sha1"
	FieldActualMD5        FieldID = "actual_md5"
	FieldOriginalMD5      FieldID = "original_md5"
	FieldVirtualRepos     FieldID = "virtual_repos"
	FieldNodeID           FieldID = "id"
	FieldRepoPathChecksum FieldID = "repo_path_checksum"
	FieldPropertyKey      FieldID = "property.key"
	FieldPropertyValue    FieldID = "property.value"
	// The statistics family (aql.md §14.1, M16 T-440): three column-backed
	// fields over the T-438 counting columns, five constant-zero-value
	// stubs for the smart-remote remote_* family BinFlow has no source for
	// (恒 null/0 — registered per the spec's mapping ruling, data never
	// fabricated), and the two internal ids stay unsupported.
	FieldStatDownloaded         FieldID = "stat.downloaded"
	FieldStatDownloads          FieldID = "stat.downloads"
	FieldStatDownloadedBy       FieldID = "stat.downloaded_by"
	FieldStatRemoteDownloaded   FieldID = "stat.remote_downloaded"
	FieldStatRemoteDownloads    FieldID = "stat.remote_downloads"
	FieldStatRemoteDownloadedBy FieldID = "stat.remote_downloaded_by"
	FieldStatRemoteOrigin       FieldID = "stat.remote_origin"
	FieldStatRemotePath         FieldID = "stat.remote_path"
)

// FieldKind is the value kind of a field, mirroring the official field table
// (aql.md §2.2): it decides which operators and which value literals are
// legal on the field.
type FieldKind string

// KindString through KindEnum are the field value kinds of the official field
// table (aql.md §2.2); they gate operators and literal shapes.
const (
	KindString FieldKind = "string"
	KindDate   FieldKind = "date"
	KindInt    FieldKind = "int"
	KindLong   FieldKind = "long"
	KindEnum   FieldKind = "enum"
)

// Operator is a criteria comparator ($eq, $match, $last, ...). $and/$or/$msp
// are structural and live in the parser, not in field Op sets.
type Operator string

// OpEq through OpBefore are the criteria comparators (aql.md §2.4);
// $and/$or/$msp are structural and live in the parser, not in field Op sets.
const (
	OpEq     Operator = "$eq"
	OpNe     Operator = "$ne"
	OpGt     Operator = "$gt"
	OpGte    Operator = "$gte"
	OpLt     Operator = "$lt"
	OpLte    Operator = "$lte"
	OpMatch  Operator = "$match"
	OpNmatch Operator = "$nmatch"
	OpLast   Operator = "$last"
	OpBefore Operator = "$before"
)

// Comparison operator groups (aql.md §2.4): the six order comparators apply
// to string/date/int/long; $match/$nmatch are string-only; $last/$before are
// date-only.
var (
	cmpOps    = []Operator{OpEq, OpNe, OpGt, OpGte, OpLt, OpLte}
	stringOps = []Operator{OpEq, OpNe, OpGt, OpGte, OpLt, OpLte, OpMatch, OpNmatch}
	dateOps   = []Operator{OpEq, OpNe, OpGt, OpGte, OpLt, OpLte, OpLast, OpBefore}
)

// typeValues is the closed value set of the type field (aql.md §2.2).
var typeValues = map[string]bool{"any": true, "file": true, "folder": true}

// Field describes one registry entry.
type Field struct {
	ID     FieldID
	Name   string // canonical wire name (also the registry key)
	Domain Domain
	Kind   FieldKind
	// Ops lists the comparators legal in criteria for this field. An empty
	// Ops set marks an output-only field (virtual_repos): usable in include,
	// rejected in criteria.
	Ops []Operator
	// Sortable reports whether the field may appear in .sort(); Projectable
	// whether it may appear in .include().
	Sortable    bool
	Projectable bool
	// Unsupported is non-empty when the field is known to the AQL language
	// but has no BinFlow storage source (or is internal). Any use — criteria,
	// include or sort — is rejected with a message carrying this reason.
	Unsupported string
}

// lookupField resolves a wire name (including dotted paths like
// "property.key" or "stat.downloads") to its registry entry.
func lookupField(name string) (Field, bool) {
	f, ok := fieldRegistry[name]
	return f, ok
}

// defaultItemOutput is the default output field set of an items query
// without .include() (aql.md §3.3, live-verified). The sort validator treats
// these as "result fields".
var defaultItemOutput = []string{
	"repo", "path", "name", "type", "size", "created", "created_by",
	"modified", "modified_by", "updated",
}

// statFields is the statistics-domain field family (aql.md §2.2/§14.1):
// the three column-backed fields, the five smart-remote stubs and the two
// internal ids. The registry carries one entry each; statStubFields marks
// the constant-zero subset the planner folds (BinFlow has no smart-remote
// topology — the spec's mapping ruling, aql.md §14.1: 勿造数据).
var statFields = []string{
	"stat.downloaded", "stat.downloads", "stat.downloaded_by",
	"stat.remote_downloaded", "stat.remote_downloads",
	"stat.remote_downloaded_by", "stat.remote_origin", "stat.remote_path",
	"stat.id", "stat.remote_id",
}

// statStubFields are the constant zero-value members: queryable and
// projectable against their fixed value (0 for the counter, null for the
// rest), sortable never — a constant has no order to give.
var statStubFields = []FieldID{
	FieldStatRemoteDownloaded,
	FieldStatRemoteDownloads,
	FieldStatRemoteDownloadedBy,
	FieldStatRemoteOrigin,
	FieldStatRemotePath,
}

// isStatStubField reports whether id is one of the constant-zero remote_*
// statistics fields.
func isStatStubField(id FieldID) bool {
	for _, f := range statStubFields {
		if f == id {
			return true
		}
	}
	return false
}

// unsupportedDomains carries the query-entry domains Artifactory accepts that
// BinFlow rejects, each with the reason surfaced in the error message
// (aql.md §2.1 subset table; C-layer enhanced copy sanctioned there).
var unsupportedDomains = map[string]string{
	"builds":            "build-info domains are not implemented",
	"build.properties":  "build-info domains are not implemented",
	"build.promotions":  "build-info domains are not implemented",
	"modules":           "build-info domains are not implemented",
	"module.properties": "build-info domains are not implemented",
	"dependencies":      "build-info domains are not implemented",
	"artifacts":         "build-info domains are not implemented",
	"releases":          "release-bundle domains are not implemented",
	"release_artifacts": "release-bundle domains are not implemented",
	// The statistics ENTRY domain stays closed (the M15/M16 query entry is
	// items); the field family itself is open through stat.* paths.
	"statistics": `query statistics through items.find with {"stat.<field>": value} criteria`,
	"properties": `query properties through items.find with {"@key": value} criteria`,
	"item.infos": "internal domain, not exposed",
}

// notSupportedOps are operator spellings that parse like operators but are
// outside the BinFlow subset (aql.md §2.4 "unsupported" rows + the
// decompiled-only case-insensitive variants V-f): each gets a rejection that
// names it instead of a generic syntax error.
var notSupportedOps = map[string]string{
	"$not":      "$not is not part of the AQL language; use $ne or $nmatch",
	"$contains": "not an AQL operator; use $match with wildcards",
	"$eqic":     "case-insensitive operator variants are not part of the BinFlow subset",
	"$eqvic":    "case-insensitive operator variants are not part of the BinFlow subset",
	"$matchic":  "case-insensitive operator variants are not part of the BinFlow subset",
}

// notSupportedSuffixes are method-chain suffixes that are part of the AQL
// language but outside the M15 read-only query subset (aql.md §2.5).
var notSupportedSuffixes = map[string]string{
	"distinct":   "the .distinct() modifier is not part of the BinFlow subset",
	"transitive": "the .transitive modifier is not implemented (Smart Remote deep links)",
	"delete":     "AQL write actions are not supported; BinFlow AQL is read-only",
	"update":     "AQL write actions are not supported; BinFlow AQL is read-only",
	"dryRun":     "AQL write actions are not supported; BinFlow AQL is read-only",
}

// fieldRegistry is the closed set. Keep sorted by name for reviewability.
var fieldRegistry = map[string]Field{
	"actual_md5": {
		ID: FieldActualMD5, Name: "actual_md5", Domain: DomainItem,
		Kind: KindString, Ops: stringOps, Sortable: true, Projectable: true,
	},
	"actual_sha1": {
		ID: FieldActualSHA1, Name: "actual_sha1", Domain: DomainItem,
		Kind: KindString, Ops: stringOps, Sortable: true, Projectable: true,
	},
	"created": {
		ID: FieldCreated, Name: "created", Domain: DomainItem,
		Kind: KindDate, Ops: dateOps, Sortable: true, Projectable: true,
	},
	"created_by": {
		ID: FieldCreatedBy, Name: "created_by", Domain: DomainItem,
		Kind: KindString, Ops: stringOps, Sortable: true, Projectable: true,
	},
	"depth": {
		ID: FieldDepth, Name: "depth", Domain: DomainItem,
		Kind: KindInt, Ops: cmpOps, Sortable: true, Projectable: true,
	},
	"id": {
		ID: FieldNodeID, Name: "id", Domain: DomainItem,
		Kind: KindLong, Unsupported: "internal field, not exposed",
	},
	"modified": {
		ID: FieldModified, Name: "modified", Domain: DomainItem,
		Kind: KindDate, Ops: dateOps, Sortable: true, Projectable: true,
	},
	"modified_by": {
		ID: FieldModifiedBy, Name: "modified_by", Domain: DomainItem,
		Kind: KindString,
		// nodes has no updated_by column: registered for honest rejection,
		// not silently mapped onto created_by (ADR-0043 pt 2).
		Unsupported: "no storage source in BinFlow yet",
	},
	"name": {
		ID: FieldName, Name: "name", Domain: DomainItem,
		Kind: KindString, Ops: stringOps, Sortable: true, Projectable: true,
	},
	"original_md5": {
		ID: FieldOriginalMD5, Name: "original_md5", Domain: DomainItem,
		Kind: KindString,
		// BinFlow stores one checksum per content, not the Artifactory
		// original/actual pair (aql.md §10).
		Unsupported: "BinFlow does not store separate original checksums",
	},
	"original_sha1": {
		ID: FieldOriginalSHA1, Name: "original_sha1", Domain: DomainItem,
		Kind:        KindString,
		Unsupported: "BinFlow does not store separate original checksums",
	},
	"path": {
		ID: FieldPath, Name: "path", Domain: DomainItem,
		Kind: KindString, Ops: stringOps, Sortable: true, Projectable: true,
	},
	"property.key": {
		ID: FieldPropertyKey, Name: "property.key", Domain: DomainProperty,
		Kind: KindString, Ops: stringOps, Sortable: false, Projectable: true,
	},
	"property.value": {
		ID: FieldPropertyValue, Name: "property.value", Domain: DomainProperty,
		Kind: KindString, Ops: stringOps, Sortable: false, Projectable: true,
	},
	"repo": {
		ID: FieldRepo, Name: "repo", Domain: DomainItem,
		Kind: KindString, Ops: stringOps, Sortable: true, Projectable: true,
	},
	"repo_path_checksum": {
		ID: FieldRepoPathChecksum, Name: "repo_path_checksum", Domain: DomainItem,
		Kind: KindString, Unsupported: "internal field, not exposed",
	},
	"sha256": {
		ID: FieldSha256, Name: "sha256", Domain: DomainItem,
		Kind: KindString, Ops: stringOps, Sortable: true, Projectable: true,
	},
	"size": {
		ID: FieldSize, Name: "size", Domain: DomainItem,
		Kind: KindLong, Ops: cmpOps, Sortable: true, Projectable: true,
	},
	"type": {
		ID: FieldType, Name: "type", Domain: DomainItem,
		Kind: KindEnum, Ops: []Operator{OpEq, OpNe}, Sortable: true, Projectable: true,
	},
	"updated": {
		ID: FieldUpdated, Name: "updated", Domain: DomainItem,
		Kind: KindDate, Ops: dateOps, Sortable: true, Projectable: true,
	},
	"virtual_repos": {
		ID: FieldVirtualRepos, Name: "virtual_repos", Domain: DomainItem,
		// Logical output field resolved from repo aggregation at runtime
		// (aql.md §2.2/§7-3): include-only, never a criteria or sort key.
		Kind: KindString, Projectable: true,
	},
}

func init() {
	// Statistics family (M16 T-440, aql.md §14.1): the three column-backed
	// fields are full registry citizens — criteria, include, sort, the null
	// literal — over the T-438 counting columns (the single counting
	// channel). The remote_* stubs are registered against their constant
	// zero values so parity queries (the usage template's remote arm, §14.2)
	// parse and fold instead of being rejected; the two internal ids keep
	// the honest unsupported refusal.
	fieldRegistry["stat.downloaded"] = Field{
		ID: FieldStatDownloaded, Name: "stat.downloaded", Domain: DomainStatistics,
		Kind: KindDate, Ops: dateOps, Sortable: true, Projectable: true,
	}
	fieldRegistry["stat.downloads"] = Field{
		ID: FieldStatDownloads, Name: "stat.downloads", Domain: DomainStatistics,
		Kind: KindInt, Ops: cmpOps, Sortable: true, Projectable: true,
	}
	fieldRegistry["stat.downloaded_by"] = Field{
		ID: FieldStatDownloadedBy, Name: "stat.downloaded_by", Domain: DomainStatistics,
		Kind: KindString, Ops: stringOps, Sortable: true, Projectable: true,
	}
	fieldRegistry["stat.remote_downloaded"] = Field{
		ID: FieldStatRemoteDownloaded, Name: "stat.remote_downloaded", Domain: DomainStatistics,
		Kind: KindDate, Ops: dateOps, Projectable: true,
	}
	fieldRegistry["stat.remote_downloads"] = Field{
		ID: FieldStatRemoteDownloads, Name: "stat.remote_downloads", Domain: DomainStatistics,
		Kind: KindInt, Ops: cmpOps, Projectable: true,
	}
	fieldRegistry["stat.remote_downloaded_by"] = Field{
		ID: FieldStatRemoteDownloadedBy, Name: "stat.remote_downloaded_by", Domain: DomainStatistics,
		Kind: KindString, Ops: stringOps, Projectable: true,
	}
	fieldRegistry["stat.remote_origin"] = Field{
		ID: FieldStatRemoteOrigin, Name: "stat.remote_origin", Domain: DomainStatistics,
		Kind: KindString, Ops: stringOps, Projectable: true,
	}
	fieldRegistry["stat.remote_path"] = Field{
		ID: FieldStatRemotePath, Name: "stat.remote_path", Domain: DomainStatistics,
		Kind: KindString, Ops: stringOps, Projectable: true,
	}
	for _, name := range []string{"stat.id", "stat.remote_id"} {
		fieldRegistry[name] = Field{
			ID: FieldID(name), Name: name, Domain: DomainStatistics,
			Kind: KindLong, Unsupported: "internal field, not exposed",
		}
	}
}
