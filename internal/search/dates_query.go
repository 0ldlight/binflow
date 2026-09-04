package search

import (
	"context"
	"time"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The dates/creation fixed templates (M16 T-452, FR-148.3 / aql.md §14.3):
// the two epoch-milliseconds date-range doors of the old-search 404-empty
// family. Both ride runAST — the very same gate/deadline/cap/ACL plane a
// hand-written AQL query takes (the RunUsage posture: one query plane, zero
// exemptions).
//
// Shared semantics (§14.3's skeleton, both doors):
//
//   - the interval is `> from AND <= to` (from exclusive, to inclusive);
//   - `to` absent defaults to now() — the OFFICIAL ruling BinFlow adopts
//     from the two conflicting sources (V-j: the decompile has no upper
//     bound, the official docs say now(); the difference is observable only
//     for future-dated items, and the official reading is the safer one);
//   - maven-metadata.xml is always excluded (both doors, §14.3);
//   - a miss is the 404 "No results found." family — the transport's arm,
//     not the engine's.

// CreationQuery is GET /api/search/creation's fixed template (§14.3): an
// item hits when created OR lastModified falls inside the range (the OR is
// the decompile's supplement — the official docs describe created only).
type CreationQuery struct {
	From  int64    // epoch milliseconds, REQUIRED (> from)
	To    int64    // epoch milliseconds; 0 → now() (V-j official ruling)
	Repos []string // optional repository narrowing (CSV on the wire)
}

// RunCreation executes the creation template.
func (e *Engine) RunCreation(ctx context.Context, p *repo.Principal, cq CreationQuery) (*Result, error) {
	return e.runAST(ctx, p, creationTemplate(cq, e.nowFn()))
}

// DatesQuery is GET /api/search/dates' fixed template: any of the named
// date fields falling inside the range hits (an OR group over the fields,
// §14.3). Fields carries the validated wire spellings (created /
// lastModified / lastDownloaded / remote_last_downloaded); an empty set is
// the default pair {created, lastModified} (V-k: the decompile registers no
// explicit default — the creation door's fixed pair is the family's own
// reading, registered as pending verification).
type DatesQuery struct {
	From   int64
	To     int64 // 0 → now()
	Fields []string
	Repos  []string
}

// DatesFieldNames is the closed dateFields set, in wire echo order (the 400
// unknown-field copy renders the same order, §14.3).
var DatesFieldNames = []string{"created", "lastModified", "lastDownloaded", "remote_last_downloaded"}

// ValidDatesField reports whether name is a legal dateFields member.
func ValidDatesField(name string) bool {
	for _, f := range DatesFieldNames {
		if f == name {
			return true
		}
	}
	return false
}

// RunDates executes the dates template.
func (e *Engine) RunDates(ctx context.Context, p *repo.Principal, dq DatesQuery) (*Result, error) {
	return e.runAST(ctx, p, datesTemplate(dq, e.nowFn()))
}

// creationTemplate builds the creation AST: the metadata exclusion, the
// created-or-modified range disjunction, then the optional repo narrowing.
// The projection carries the identity triple plus both date fields (the
// echoed `created` value needs modified as the V-o fallback source); the
// sort is created ascending (the spec registers no order for these doors —
// BinFlow pins the date-ascending order for determinism).
func creationTemplate(cq CreationQuery, now time.Time) *Query {
	from := time.UnixMilli(cq.From).UTC()
	to := time.UnixMilli(cq.To).UTC()
	if cq.To == 0 {
		to = now
	}
	children := []Criteria{
		&Compare{Field: itemRef(FieldName), Op: OpNe, Value: Value{Kind: LitString, Str: datesMetadataExclusion}},
		&Or{Children: []Criteria{
			dateInRange(itemRef(FieldCreated), from, to),
			dateInRange(itemRef(FieldModified), from, to),
		}},
	}
	children = append(children, datesRepoArms(cq.Repos)...)
	return &Query{
		Domain:   "items",
		Criteria: &And{Children: children},
		Include: []IncludeField{
			{Raw: "repo", Field: itemRef(FieldRepo)},
			{Raw: "path", Field: itemRef(FieldPath)},
			{Raw: "name", Field: itemRef(FieldName)},
			{Raw: "created", Field: itemRef(FieldCreated)},
			{Raw: "modified", Field: itemRef(FieldModified)},
		},
		Sort: []SortKey{{Field: itemRef(FieldCreated), Asc: true}},
	}
}

// datesTemplate builds the dates AST over the named fields. The field map
// is the wire-name → registry-ref table the door validates against.
func datesTemplate(dq DatesQuery, now time.Time) *Query {
	from := time.UnixMilli(dq.From).UTC()
	to := time.UnixMilli(dq.To).UTC()
	if dq.To == 0 {
		to = now
	}
	fields := dq.Fields
	if len(fields) == 0 {
		fields = []string{"created", "lastModified"}
	}
	arms := make([]Criteria, 0, len(fields))
	for _, name := range fields {
		ref, ok := datesFieldRef(name)
		if !ok {
			continue // unreachable: the transport validates the closed set
		}
		arms = append(arms, dateInRange(ref, from, to))
	}
	children := []Criteria{
		&Compare{Field: itemRef(FieldName), Op: OpNe, Value: Value{Kind: LitString, Str: datesMetadataExclusion}},
		&Or{Children: arms},
	}
	children = append(children, datesRepoArms(dq.Repos)...)
	return &Query{
		Domain:   "items",
		Criteria: &And{Children: children},
		Include: []IncludeField{
			{Raw: "repo", Field: itemRef(FieldRepo)},
			{Raw: "path", Field: itemRef(FieldPath)},
			{Raw: "name", Field: itemRef(FieldName)},
			{Raw: "created", Field: itemRef(FieldCreated)},
			{Raw: "modified", Field: itemRef(FieldModified)},
		},
		Sort: []SortKey{{Field: itemRef(FieldCreated), Asc: true}},
	}
}

// datesMetadataExclusion is the always-excluded descriptor filename
// (§14.3: "maven-metadata.xml 恒排除（两端口径）").
const datesMetadataExclusion = "maven-metadata.xml"

// dateInRange builds one field's `> from AND <= to` conjunction.
func dateInRange(ref FieldRef, from, to time.Time) Criteria {
	return &And{Children: []Criteria{
		&Compare{Field: ref, Op: OpGt, Value: Value{Kind: LitString, Str: epochMillisLiteral(from)}},
		&Compare{Field: ref, Op: OpLte, Value: Value{Kind: LitString, Str: epochMillisLiteral(to)}},
	}}
}

// datesRepoArms builds the optional repo narrowing (an OR over the keys).
func datesRepoArms(repos []string) []Criteria {
	if len(repos) == 0 {
		return nil
	}
	arms := make([]Criteria, 0, len(repos))
	for _, key := range repos {
		arms = append(arms, &Compare{
			Field: itemRef(FieldRepo), Op: OpEq, Value: Value{Kind: LitString, Str: key},
		})
	}
	return []Criteria{&Or{Children: arms}}
}

// datesFieldRef maps a validated wire dateFields name onto its registry
// ref: the two item-domain dates plus the two statistics-domain download
// stamps (remote_last_downloaded is the smart-remote stub — constant null,
// so an arm over it matches nothing, the honest empty set rather than
// fabricated data).
func datesFieldRef(name string) (FieldRef, bool) {
	switch name {
	case "created":
		return itemRef(FieldCreated), true
	case "lastModified":
		return itemRef(FieldModified), true
	case "lastDownloaded":
		return statRef(FieldStatDownloaded), true
	case "remote_last_downloaded":
		return statRef(FieldStatRemoteDownloaded), true
	}
	return FieldRef{}, false
}

// itemRef resolves an item-domain FieldID through the registry (statRef's
// sibling — the single spelling source for hand-built ASTs).
func itemRef(id FieldID) FieldRef {
	f, _ := lookupField(string(id))
	return FieldRef{ID: f.ID, Name: f.Name, Domain: f.Domain}
}
