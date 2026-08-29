package nuget

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The v2 OData face (nuget.md sections 2-6, the 18-endpoint table this file
// and its v2remote/v2virtual/v2batch siblings implement). The entity shape
// is the nuget.org V2FeedPackage set — the OData $metadata below is the
// contract the feed's m:properties block answers to.
//
// Row pipeline: every collection endpoint (Search/Packages/
// FindPackagesById/GetUpdates, ±$count) renders ONE row shape — rows built
// from stored facts (local, virtual local members), from parsed upstream
// entries (the remote proxy), or merged (virtual). Endpoint shaping, the
// OData option face and the semVerLevel ladder run identically over all
// three sources, so the v2 and v3 faces can never disagree about a
// package's existence.

// v2Namespaces are the feed document's fixed namespace declarations.
const (
	v2AtomNS       = "http://www.w3.org/2005/Atom"
	v2AppNS        = "http://www.w3.org/2007/app"
	v2DataNS       = "http://schemas.microsoft.com/ado/2007/08/dataservices"
	v2MetadataNS   = "http://schemas.microsoft.com/ado/2007/08/dataservices/metadata"
	v2SchemaNS     = "http://schemas.microsoft.com/ado/2008/09/edm"
	v2EdmxNS       = "http://schemas.microsoft.com/ado/2007/06/edmx"
	v2EntitySchema = "http://schemas.microsoft.com/ado/2007/08/dataservices/scheme"
	// v2DataServiceVersion is the OData response header every v2 endpoint
	// carries (nuget.md section 2). Live nuget.org serves "3.0" there —
	// the VALUE varies by server, the header's presence is the contract;
	// BinFlow speaks the 2.0 family the spec's Artifactory anchors record.
	v2DataServiceVersion = "2.0"
)

// msgV2LocalOnly is the rclass-matrix refusal (nuget.md sections 5.1/5.2:
// publish/delete on a remote repository, or an unrouted virtual).
const msgV2LocalOnly = "This operation can only be performed on local repositories."

// msgV2UnacceptablePath is the virtual publish's pattern refusal (5.1).
const msgV2UnacceptablePath = "Unacceptable path."

// serveV2ServiceDoc renders GET <v2-base>/ — the OData service document
// (nuget.md section 2 #1; the shape live-captured from nuget.org's own
// face: one workspace, the Packages collection).
func (h *Handler) serveV2ServiceDoc(w http.ResponseWriter, r *http.Request, repoKey string) {
	base := v2Base(h.baseURLFor(r), repoKey)
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	b.WriteString(`<service xml:base="`)
	xmlEscape(&b, base)
	b.WriteString(`" xmlns="` + v2AppNS + `" xmlns:atom="` + v2AtomNS + `">`)
	b.WriteString(`<workspace><atom:title type="text">Default</atom:title>`)
	b.WriteString(`<collection href="Packages"><atom:title type="text">Packages</atom:title></collection>`)
	b.WriteString(`</workspace></service>`)
	writeV2XML(w, r, http.StatusOK, "application/xml; charset=utf-8", b.Bytes())
}

// ---- the collection request model ----

// v2FeedReq is one parsed collection request.
type v2FeedReq struct {
	kind         routeKind
	count        bool
	entryID      string
	entryVersion string
	project      string

	q        *v2Query
	odata    odataOpts
	semver   semverLevel
	id       string // FindPackagesById's id operand (storage-key lower)
	updates  updatesQuery
	rawQuery string
}

// parseV2FeedReq reads the parameter face; a non-nil error is the 400.
func parseV2FeedReq(r *http.Request, rt route) (*v2FeedReq, error) {
	req := &v2FeedReq{
		kind: rt.kind, count: rt.count, entryID: rt.entryID, entryVersion: rt.entryVersion,
		project: rt.project, q: foldV2QueryOf(r), rawQuery: r.URL.RawQuery,
	}
	var err error
	if req.odata, err = parseOData(req.q); err != nil {
		return nil, err
	}
	req.semver = parseSemVerLevel(req.q.get("semVerLevel"))
	if rt.kind == kindV2FindPackages {
		req.id = v2QueryID(r)
	}
	if rt.kind == kindV2GetUpdates {
		req.updates = parseUpdatesQuery(req.q)
	}
	return req, nil
}

// serveV2Collection is the shared dispatcher of the four read faces (#3-#12):
// rows collected per class, endpoint shaping, the OData option face, and the
// count/feed/entry/projection renderers.
func (h *Handler) serveV2Collection(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, class string, rt route) {
	req, err := parseV2FeedReq(r, rt)
	if err != nil {
		writePlain(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.kind == kindV2FindPackages && req.id == "" {
		// nuget.md section 2 #5: the missing id parameter is the 404 (NOT
		// the 400 — the unanswerable address is the not-found family).
		writePlain(w, http.StatusNotFound, "not found")
		return
	}
	if req.count && class == repo.TypeRemote {
		// The $count family's remote arm (7.1-2): the upstream digits, the
		// -1 sentinel on a non-numeric body, the landed count behind them.
		if n, ok := h.v2RemoteCount(ctx, p, repoKey, req); ok {
			writeText(w, http.StatusOK, strconv.Itoa(n))
			return
		}
	}

	rows, err := h.v2CollectRows(ctx, r, p, repoKey, class, req)
	if err != nil {
		h.writeError(w, err, repoKey, "")
		return
	}
	// The level ladder is EVERY v2 endpoint's lens (section 4): the
	// SemVer2-only spellings drop out first, then the latest flags
	// recompute over the visible set — for every source (the upstream's
	// own filtering already ran; the recompute just fixes the flags over
	// the same visible rows).
	rows = filterV2SemVer(rows, req)
	recomputeV2Latest(rows)

	switch req.kind {
	case kindV2FindPackages:
		h.finishV2Collection(w, r, repoKey, req, rows, false, class != repo.TypeLocal)
	case kindV2Search:
		h.finishV2Collection(w, r, repoKey, req, shapeV2Search(rows, req), true, class != repo.TypeLocal)
	case kindV2GetUpdates:
		h.finishV2Collection(w, r, repoKey, req, shapeV2Updates(rows, req), true, class != repo.TypeLocal)
	default: // kindV2Packages
		if req.entryID != "" {
			h.serveV2Entry(w, r, repoKey, req, rows)
			return
		}
		h.finishV2Collection(w, r, repoKey, req, rows, true, class != repo.TypeLocal)
	}
}

// finishV2Collection applies the shared tail: the $filter face, ordering,
// paging, and the count/feed renderers. emptyIs404 carries the endpoint's
// empty-collection semantics (Search/Packages/GetUpdates answer the 404,
// FindPackagesById the 200 empty feed — the collection family's split,
// nuget.md section 2 #3/#5). linksV2 selects the download-link family
// (section 7.1-2/7.2-6: proxied and virtual feeds cite the v2 Download
// face; local facts keep the T-287 flatcontainer shape).
func (h *Handler) finishV2Collection(w http.ResponseWriter, r *http.Request, repoKey string, req *v2FeedReq, rows []v2Row, emptyIs404, linksV2 bool) {
	rows, total := applyV2FilterOrderPage(rows, req)
	if req.count {
		writeText(w, http.StatusOK, strconv.Itoa(total))
		return
	}
	if emptyIs404 && len(rows) == 0 {
		writePlain(w, http.StatusNotFound, "not found")
		return
	}
	origin := h.baseURLFor(r)
	base := v2Base(origin, repoKey)
	flat := flatBase(origin, repoKey)
	body := renderV2Feed(base, flat, v2SelfPath(req), rows, req.odata.inlinecount, total, linksV2)
	writeV2XML(w, r, http.StatusOK, "application/atom+xml; charset=utf-8", body)
}

// v2SelfPath renders the feed's self resource path.
func v2SelfPath(req *v2FeedReq) string {
	switch req.kind {
	case kindV2Search:
		return v2Search + "()"
	case kindV2GetUpdates:
		return v2GetUpdates + "()"
	case kindV2Packages:
		return v2Packages + "()"
	default:
		self := findPackagesByID + "()"
		if req.id != "" {
			self += "?id='" + req.id + "'"
		}
		return self
	}
}

// serveV2Entry renders the single-entry addressings: #8 (the entry) and #9
// (the Id projection — the client self-check face).
func (h *Handler) serveV2Entry(w http.ResponseWriter, r *http.Request, repoKey string, req *v2FeedReq, rows []v2Row) {
	wantVer := ""
	if req.entryVersion != "" {
		wantVer, _ = normalizeNuGetVersion(req.entryVersion)
	}
	var hit *v2Row
	for i := range rows {
		if lowerASCII(rows[i].id) != lowerASCII(req.entryID) {
			continue
		}
		if wantVer != "" && rows[i].version != wantVer {
			continue
		}
		hit = &rows[i]
		break
	}
	if hit == nil {
		writePlain(w, http.StatusNotFound, "not found")
		return
	}
	origin := h.baseURLFor(r)
	base := v2Base(origin, repoKey)
	flat := flatBase(base, repoKey)
	if req.project == v2IDProp {
		var b bytes.Buffer
		b.WriteString(`<d:Id xmlns:d="` + v2DataNS + `" xmlns:m="` + v2MetadataNS + `" m:type="Edm.String">`)
		xmlEscape(&b, hit.id)
		b.WriteString(`</d:Id>`)
		writeV2XML(w, r, http.StatusOK, "application/xml; charset=utf-8", b.Bytes())
		return
	}
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	renderV2Entry(&b, base, flat, *hit, true, true)
	writeV2XML(w, r, http.StatusOK, "application/atom+xml; charset=utf-8", b.Bytes())
}

// applyV2FilterOrderPage runs the OData tail: $filter, the endpoint's
// ordering, $skip/$top — returning the paged rows plus the pre-paging total
// (the $inlinecount value).
func applyV2FilterOrderPage(rows []v2Row, req *v2FeedReq) ([]v2Row, int) {
	rows = applyV2Filter(rows, req)
	sortV2Rows(rows, req)
	total := len(rows)
	lo, hi := req.odata.skip, req.odata.skip+req.odata.top
	if req.odata.top == 0 {
		hi = total // no $top: the whole (bounded) feed
	}
	if lo > total {
		lo = total
	}
	if hi > total {
		hi = total
	}
	return rows[lo:hi], total
}

// filterV2SemVer applies the level ladder to the row set.
func filterV2SemVer(rows []v2Row, req *v2FeedReq) []v2Row {
	if req.semver >= semverLevelInclude {
		return rows
	}
	out := make([]v2Row, 0, len(rows))
	for _, row := range rows {
		if semVerVisible(row.version, req.semver) {
			out = append(out, row)
		}
	}
	return out
}

// applyV2Filter evaluates the parsed $filter family. The IsLatestVersion
// form is DROPPED whole when semVerLevel sits at 2.0.0 or above
// (nuget.ignoreIsLatestVersionFilter defaults true — section 4's
// Artifactory supplement: such a request returns every version).
func applyV2Filter(rows []v2Row, req *v2FeedReq) []v2Row {
	f := req.odata.filter
	if f == "" {
		return rows
	}
	if absolute, wanted, latest := req.odata.filterLatest(); latest {
		if req.semver >= semverLevelInclude {
			return rows // the default-true drop
		}
		out := make([]v2Row, 0, len(rows))
		for _, row := range rows {
			flag := row.isLatest
			if absolute {
				flag = row.isAbsLatest
			}
			if flag == wanted {
				out = append(out, row)
			}
		}
		return out
	}
	if id, ok := req.odata.filterID(); ok {
		idLow := lowerASCII(id)
		out := make([]v2Row, 0, len(rows))
		for _, row := range rows {
			if strings.Contains(lowerASCII(row.id), idLow) {
				out = append(out, row)
			}
		}
		return out
	}
	return rows
}

// sortV2Rows orders the collection: id (lowercase) ascending then version
// ascending — the section 7.2-7 order — with $orderby overriding the FIELD
// (GetUpdates' default field is version: the section 7.2-4 auto-injection).
func sortV2Rows(rows []v2Row, req *v2FeedReq) {
	byVersion := req.odata.orderBy == "version" || (req.odata.orderBy == "" && req.kind == kindV2GetUpdates)
	desc := req.odata.orderDesc
	sort.SliceStable(rows, func(i, j int) bool {
		var c int
		if byVersion {
			c = compareNuGetVersions(rows[i].version, rows[j].version)
		} else {
			c = strings.Compare(lowerASCII(rows[i].id), lowerASCII(rows[j].id))
			if c == 0 {
				c = compareNuGetVersions(rows[i].version, rows[j].version)
			}
		}
		if desc {
			return c > 0
		}
		return c < 0
	})
}

// shapeV2Search narrows the rows to the search face: the term's substring
// family, the prerelease policy, one entry per package id — the newest
// version the policy admits (nuget.md section 2 #3).
func shapeV2Search(rows []v2Row, req *v2FeedReq) []v2Row {
	term := lowerASCII(dequote(req.q.get("searchTerm")))
	includePre := req.q.boolOf("includePrerelease")
	var out []v2Row
	byID := map[string]int{}
	for _, row := range rows {
		if !semVerVisible(row.version, req.semver) {
			continue
		}
		if !includePre && row.isPrerelease {
			continue
		}
		if term != "" && !v2RowMatches(row, term) {
			continue
		}
		idLow := lowerASCII(row.id)
		if idx, seen := byID[idLow]; !seen {
			byID[idLow] = len(out)
			out = append(out, row)
		} else if compareNuGetVersions(row.version, out[idx].version) > 0 {
			out[idx] = row
		}
	}
	return out
}

// v2RowMatches is the term match over the id/title/tags/description family.
func v2RowMatches(row v2Row, termLow string) bool {
	return strings.Contains(lowerASCII(row.id), termLow) ||
		strings.Contains(lowerASCII(row.title), termLow) ||
		strings.Contains(lowerASCII(row.tags), termLow) ||
		strings.Contains(lowerASCII(row.description), termLow)
}

// shapeV2Updates computes the GetUpdates answer (section 2 #11; the
// server-side semantics live-verified against nuget.org, August 2026): per
// (packageIds[i], versions[i], versionConstraints[i]) pair, the versions
// strictly newer than the given one, prerelease/semVerLevel filtered, the
// constraint ranged; includeAllVersions=false keeps only the newest
// qualifying version of each package.
func shapeV2Updates(rows []v2Row, req *v2FeedReq) []v2Row {
	byID := map[string][]v2Row{}
	for _, row := range rows {
		idLow := lowerASCII(row.id)
		byID[idLow] = append(byID[idLow], row)
	}
	var out []v2Row
	for i, rawID := range req.updates.ids {
		idLow := lowerASCII(rawID)
		if idLow == "" {
			continue
		}
		base, baseOK := normalizeNuGetVersion(req.updates.versions[i])
		constraint := parseVersionRange(req.updates.constraints[i])
		candidates := byID[idLow]
		sort.Slice(candidates, func(a, b int) bool {
			return compareNuGetVersions(candidates[a].version, candidates[b].version) < 0
		})
		var qualifying []v2Row
		for _, row := range candidates {
			if baseOK && compareNuGetVersions(row.version, base) <= 0 {
				continue
			}
			if !semVerVisible(row.version, req.semver) {
				continue
			}
			if !req.updates.includePrerelease && row.isPrerelease {
				continue
			}
			if !constraint.contains(row.version) {
				continue
			}
			qualifying = append(qualifying, row)
		}
		if len(qualifying) == 0 {
			continue
		}
		if !req.updates.includeAllVersions {
			qualifying = qualifying[len(qualifying)-1:]
		}
		out = append(out, qualifying...)
	}
	return out
}

// ---- the row model ----

// v2Row is one feed entry's render input, whatever its source.
type v2Row struct {
	id, version string // the wire spellings (gallery casing, verbatim)

	title, description, summary, tags string
	authors, copyright, language      string
	licenseURL, projectURL, iconURL   string
	minClientVersion, releaseNotes    string
	packageHash, hashAlgo             string
	packageSize                       int64
	published, updated, created       string
	isLatest, isAbsLatest             bool
	isPrerelease                      bool
	requireLicenseAcceptance          bool
	dependencies                      string
	galleryDetailsURL                 string
}

// rowFromFacts builds the render row off stored facts (the property set
// mirrors the T-287 renderer — the M10 assertions ride it).
func rowFromFacts(f versionFacts) v2Row {
	id, version := f.ref.id, f.ref.version
	displayID := id
	if f.nuspec != nil && f.nuspec.id != "" {
		displayID = f.nuspec.id
	}
	row := v2Row{
		id: displayID, version: version,
		title:                    v2String(f.nuspec, func(n *nuspecInfo) string { return n.title }),
		description:              v2String(f.nuspec, func(n *nuspecInfo) string { return n.description }),
		summary:                  v2String(f.nuspec, func(n *nuspecInfo) string { return n.summary }),
		tags:                     v2String(f.nuspec, func(n *nuspecInfo) string { return strings.Join(n.tags, " ") }),
		authors:                  v2Author(f),
		language:                 v2String(f.nuspec, func(n *nuspecInfo) string { return n.language }),
		licenseURL:               v2String(f.nuspec, func(n *nuspecInfo) string { return n.licenseURL }),
		projectURL:               v2String(f.nuspec, func(n *nuspecInfo) string { return n.projectURL }),
		iconURL:                  v2String(f.nuspec, func(n *nuspecInfo) string { return n.iconURL }),
		minClientVersion:         v2String(f.nuspec, func(n *nuspecInfo) string { return n.minClientVersion }),
		packageHash:              f.sha512,
		hashAlgo:                 "SHA512",
		packageSize:              f.node.Size,
		updated:                  firstNonEmpty(f.node.UpdatedAt, f.node.CreatedAt, "1970-01-01T00:00:00Z"),
		published:                firstNonEmpty(f.node.CreatedAt, f.node.UpdatedAt, "1970-01-01T00:00:00Z"),
		isPrerelease:             isPrereleaseVersion(version),
		requireLicenseAcceptance: f.nuspec != nil && f.nuspec.requireLicenseAccept,
		dependencies:             v2Dependencies(f.nuspec),
	}
	row.created = row.published
	return row
}

// recomputeV2Latest sets the latest flags over the final row set — the
// legacy aggregation (section 7.2-8, the DEFAULT mode): per id, the LAST
// non-prerelease row carries IsLatestVersion; the LAST row of any class
// carries IsAbsoluteLatestVersion.
func recomputeV2Latest(rows []v2Row) {
	latest := map[string]string{}
	absolute := map[string]string{}
	for _, row := range rows {
		id := lowerASCII(row.id)
		if !row.isPrerelease {
			latest[id] = row.version
		}
		absolute[id] = row.version
	}
	for i := range rows {
		id := lowerASCII(rows[i].id)
		rows[i].isLatest = latest[id] != "" && rows[i].version == latest[id]
		rows[i].isAbsLatest = rows[i].version == absolute[id]
	}
}

// v2String reads one nuspec field with the empty fallback.
func v2String(ns *nuspecInfo, get func(*nuspecInfo) string) string {
	if ns == nil {
		return ""
	}
	return get(ns)
}

// v2Author renders the authors join.
func v2Author(f versionFacts) string {
	if f.nuspec == nil || len(f.nuspec.authors) == 0 {
		if f.nuspec != nil && f.nuspec.id != "" {
			return f.nuspec.id
		}
		return f.ref.id
	}
	return strings.Join(f.nuspec.authors, ", ")
}

// v2Dependencies renders the official v2 dependency string:
// "id:range:tfm|id:range:tfm" (the grammar is positional —
// "Newtonsoft.Json:[13.0.3,):netstandard2.0").
func v2Dependencies(ns *nuspecInfo) string {
	if ns == nil || len(ns.dependencyGroups) == 0 {
		return ""
	}
	var parts []string
	for _, g := range ns.dependencyGroups {
		for _, d := range g.deps {
			parts = append(parts, d.id+":"+d.rangeSpec+":"+g.targetFramework)
		}
	}
	return strings.Join(parts, "|")
}

// v2QueryID extracts the OData id parameter ('single-quoted'); "" for a
// missing or malformed operand.
func v2QueryID(r *http.Request) string {
	raw := dequote(r.URL.Query().Get("id"))
	if !validPackageID(raw) {
		return ""
	}
	return lowerASCII(raw)
}

// ---- the renderers ----

// renderV2Feed renders the Atom feed. linksV2 selects the download-link
// family: the v2 Download face for proxied/virtual feeds (sections 7.1-2/
// 7.2-6), the v3 flatcontainer base for local facts (the T-287 shape the
// M10 assertions pin).
func renderV2Feed(base, flat, self string, rows []v2Row, inlinecount bool, total int, linksV2 bool) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString(`<feed xmlns="` + v2AtomNS + `" xmlns:d="` + v2DataNS + `" xmlns:m="` + v2MetadataNS + `" xml:base="`)
	xmlEscape(&b, base)
	b.WriteString(`">` + "\n")
	if inlinecount {
		b.WriteString(`<m:count>` + strconv.Itoa(total) + `</m:count>` + "\n")
	}
	writeV2Element(&b, "id", base+"/"+self)
	writeV2Element(&b, "title", "Packages")
	writeV2Element(&b, "updated", v2Timestamp(rows))
	b.WriteString(`<link rel="self" href="/` + xmlEscapeString(self) + `"/>` + "\n")
	for i := range rows {
		renderV2Entry(&b, base, flat, rows[i], linksV2, false)
	}
	b.WriteString("</feed>")
	return b.Bytes()
}

// renderV2Entry renders one version's <entry>. standalone selects the
// d/m namespace declarations on the entry element itself — the single-entry
// documents have no <feed> root to inherit them from (T-351 D-2).
func renderV2Entry(b *bytes.Buffer, base, flat string, row v2Row, linksV2, standalone bool) {
	id, version := row.id, row.version
	idLow := lowerASCII(id)
	entryID := fmt.Sprintf("%s/Packages(Id='%s',Version='%s')", base, xmlEscapeString(id), xmlEscapeString(version))
	content := fmt.Sprintf("%s%s/%s/%s.%s%s", flat, idLow, version, idLow, version, suffixNupkg)
	if linksV2 {
		content = fmt.Sprintf("%s/%s/%s/%s", base, v2Download, idLow, version)
	}

	if standalone {
		b.WriteString(`<entry xmlns:d="` + v2DataNS + `" xmlns:m="` + v2MetadataNS + `">` + "\n")
	} else {
		b.WriteString("<entry>\n")
	}
	writeV2Element(b, "id", entryID)
	writeV2Title(b, id)
	writeV2Element(b, "updated", firstNonEmpty(row.updated, "1970-01-01T00:00:00Z"))
	b.WriteString("<author><name>")
	xmlEscape(b, row.authors)
	b.WriteString("</name></author>\n")
	b.WriteString(`<link rel="edit-media" href="Packages(Id='` + xmlEscapeString(idLow) + `',Version='` + xmlEscapeString(version) + `')"/>` + "\n")
	b.WriteString(`<category term="NuGetGallery.V2FeedPackage" scheme="` + v2EntitySchema + `"/>` + "\n")
	b.WriteString(`<content type="application/zip" src="` + xmlEscapeString(content) + `"/>` + "\n")

	b.WriteString("<m:properties>")
	writeV2Property(b, "d:Id", id)
	writeV2Property(b, "d:Version", version)
	writeV2Property(b, "d:Title", row.title)
	writeV2Property(b, "d:Description", row.description)
	writeV2Property(b, "d:Summary", row.summary)
	writeV2Property(b, "d:Tags", row.tags)
	writeV2Property(b, "d:Authors", row.authors)
	writeV2Property(b, "d:Copyright", row.copyright)
	writeV2Property(b, "d:Language", row.language)
	writeV2Property(b, "d:LicenseUrl", row.licenseURL)
	writeV2Property(b, "d:ProjectUrl", row.projectURL)
	writeV2Property(b, "d:IconUrl", row.iconURL)
	writeV2Property(b, "d:MinClientVersion", row.minClientVersion)
	writeV2Property(b, "d:ReleaseNotes", row.releaseNotes)
	writeV2Property(b, "d:PackageHash", row.packageHash)
	writeV2Property(b, "d:PackageHashAlgorithm", row.hashAlgo)
	writeV2IntProperty(b, "d:PackageSize", row.packageSize)
	writeV2IntProperty(b, "d:DownloadCount", 0)
	writeV2IntProperty(b, "d:VersionDownloadCount", 0)
	writeV2Element(b, "d:Published", firstNonEmpty(row.published, "1970-01-01T00:00:00Z"))
	writeV2Element(b, "d:Created", firstNonEmpty(row.created, "1970-01-01T00:00:00Z"))
	writeV2BoolProperty(b, "d:IsLatestVersion", row.isLatest)
	writeV2BoolProperty(b, "d:IsAbsoluteLatestVersion", row.isAbsLatest)
	writeV2BoolProperty(b, "d:IsPrerelease", row.isPrerelease)
	writeV2BoolProperty(b, "d:RequireLicenseAcceptance", row.requireLicenseAcceptance)
	writeV2Property(b, "d:Dependencies", row.dependencies)
	writeV2Property(b, "d:GalleryDetailsUrl", row.galleryDetailsURL)
	b.WriteString("</m:properties>\n")
	b.WriteString("</entry>\n")
}

// v2Timestamp renders the feed's updated stamp (the newest row's).
func v2Timestamp(rows []v2Row) string {
	stamp := ""
	for _, row := range rows {
		if row.updated > stamp {
			stamp = row.updated
		}
	}
	if stamp == "" {
		return "1970-01-01T00:00:00Z"
	}
	return stamp
}

// writeV2XML renders one XML document with the family's headers.
func writeV2XML(w http.ResponseWriter, r *http.Request, status int, ctype string, body []byte) {
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("DataServiceVersion", v2DataServiceVersion)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body) //nolint:gosec // G705: server-rendered XML over escaped fields
	}
}

// writeV2Element writes one text element (content escaped).
func writeV2Element(b *bytes.Buffer, name, content string) {
	b.WriteString("<" + name + ">")
	xmlEscape(b, content)
	b.WriteString("</" + name + ">\n")
}

// writeV2Title writes the atom title element with its type attribute.
func writeV2Title(b *bytes.Buffer, content string) {
	b.WriteString(`<title type="text">`)
	xmlEscape(b, content)
	b.WriteString("</title>\n")
}

// writeV2Property writes one m:properties child (d: namespace).
func writeV2Property(b *bytes.Buffer, name, content string) {
	b.WriteString("<" + name + ">")
	xmlEscape(b, content)
	b.WriteString("</" + name + ">")
}

// writeV2IntProperty writes one numeric property.
func writeV2IntProperty(b *bytes.Buffer, name string, v int64) {
	b.WriteString("<" + name + ">")
	b.WriteString(strconv.FormatInt(v, 10))
	b.WriteString("</" + name + ">")
}

// writeV2BoolProperty writes one boolean property.
func writeV2BoolProperty(b *bytes.Buffer, name string, v bool) {
	writeV2Property(b, name, strconv.FormatBool(v))
}

// xmlEscape writes escaped text.
func xmlEscape(b *bytes.Buffer, s string) {
	_ = xml.EscapeText(b, []byte(s)) //nolint:errcheck // bytes.Buffer writes never fail
}

// xmlEscapeString renders escaped text into a string.
func xmlEscapeString(s string) string {
	var b bytes.Buffer
	xmlEscape(&b, s)
	return b.String()
}

// serveV2Metadata renders the EDMX document (the entity set + the three
// function imports the collection faces answer to; old clients fetch it
// before their first query).
func (h *Handler) serveV2Metadata(w http.ResponseWriter, r *http.Request, _ string) {
	body := []byte(`<?xml version="1.0" encoding="utf-8"?>
<edmx:Edmx xmlns:edmx="` + v2EdmxNS + `" Version="1.0">
  <edmx:DataServices xmlns:m="` + v2MetadataNS + `" m:DataServiceVersion="2.0">
    <Schema xmlns="` + v2SchemaNS + `" Namespace="NuGetGallery">
      <EntityType Name="V2FeedPackage">
        <Key>
          <PropertyRef Name="Id"/>
          <PropertyRef Name="Version"/>
        </Key>
        <Property Name="Id" Type="Edm.String" Nullable="false"/>
        <Property Name="Version" Type="Edm.String" Nullable="false"/>
        <Property Name="Title" Type="Edm.String"/>
        <Property Name="Description" Type="Edm.String"/>
        <Property Name="Summary" Type="Edm.String"/>
        <Property Name="Tags" Type="Edm.String"/>
        <Property Name="Authors" Type="Edm.String"/>
        <Property Name="Copyright" Type="Edm.String"/>
        <Property Name="Language" Type="Edm.String"/>
        <Property Name="LicenseUrl" Type="Edm.String"/>
        <Property Name="ProjectUrl" Type="Edm.String"/>
        <Property Name="IconUrl" Type="Edm.String"/>
        <Property Name="MinClientVersion" Type="Edm.String"/>
        <Property Name="ReleaseNotes" Type="Edm.String"/>
        <Property Name="PackageHash" Type="Edm.String"/>
        <Property Name="PackageHashAlgorithm" Type="Edm.String"/>
        <Property Name="PackageSize" Type="Edm.Int64"/>
        <Property Name="DownloadCount" Type="Edm.Int64"/>
        <Property Name="VersionDownloadCount" Type="Edm.Int64"/>
        <Property Name="Published" Type="Edm.String"/>
        <Property Name="Created" Type="Edm.String"/>
        <Property Name="IsLatestVersion" Type="Edm.Boolean"/>
        <Property Name="IsAbsoluteLatestVersion" Type="Edm.Boolean"/>
        <Property Name="IsPrerelease" Type="Edm.Boolean"/>
        <Property Name="RequireLicenseAcceptance" Type="Edm.Boolean"/>
        <Property Name="Dependencies" Type="Edm.String"/>
        <Property Name="GalleryDetailsUrl" Type="Edm.String"/>
      </EntityType>
      <EntityContainer Name="Container" m:IsDefaultEntityContainer="true">
        <EntitySet Name="Packages" EntityType="NuGetGallery.V2FeedPackage"/>
        <FunctionImport Name="FindPackagesById" ReturnType="Collection(NuGetGallery.V2FeedPackage)" EntitySet="Packages" m:HttpMethod="GET">
          <Parameter Name="id" Type="Edm.String" Mode="In"/>
        </FunctionImport>
        <FunctionImport Name="Search" ReturnType="Collection(NuGetGallery.V2FeedPackage)" EntitySet="Packages" m:HttpMethod="GET">
          <Parameter Name="searchTerm" Type="Edm.String" Mode="In"/>
          <Parameter Name="targetFramework" Type="Edm.String" Mode="In"/>
          <Parameter Name="includePrerelease" Type="Edm.Boolean" Mode="In"/>
        </FunctionImport>
        <FunctionImport Name="GetUpdates" ReturnType="Collection(NuGetGallery.V2FeedPackage)" EntitySet="Packages" m:HttpMethod="GET">
          <Parameter Name="packageIds" Type="Edm.String" Mode="In"/>
          <Parameter Name="versions" Type="Edm.String" Mode="In"/>
          <Parameter Name="includePrerelease" Type="Edm.Boolean" Mode="In"/>
          <Parameter Name="includeAllVersions" Type="Edm.Boolean" Mode="In"/>
          <Parameter Name="targetFrameworks" Type="Edm.String" Mode="In"/>
          <Parameter Name="versionConstraints" Type="Edm.String" Mode="In"/>
        </FunctionImport>
      </EntityContainer>
    </Schema>
  </edmx:DataServices>
</edmx:Edmx>
`)
	writeV2XML(w, r, http.StatusOK, "application/xml; charset=utf-8", body)
}

// ---- the local fact walk (section 6's three-level resolution, BinFlow's
// equivalent chain: the canonical flatcontainer directory first — the
// "default flat path" level; then the whole-repository walk matching
// <id>.<version>.nupkg base names — the property-index level, which is
// what finds packages published under a path prefix) ----

// storedRef is one located nupkg node (the fact builder's input).
type storedRef struct {
	path    string
	version string
	node    *metadata.Node
}

// v2StoredFacts collects the full fact rows of the named package ids (nil =
// every package), keyed by storage-key id, versions ascending.
func (h *Handler) v2StoredFacts(ctx context.Context, p *repo.Principal, repoKey string, ids []string) (map[string][]versionFacts, error) {
	out := map[string][]versionFacts{}
	if ids != nil {
		for _, raw := range ids {
			id := lowerASCII(raw)
			if id == "" || !validPackageID(id) {
				continue
			}
			facts, err := h.collectLocalFactsDeep(ctx, p, repoKey, id)
			if err != nil {
				return nil, err
			}
			if len(facts) > 0 {
				out[id] = facts
			}
		}
		return out, nil
	}
	nodes, err := h.svc.List(ctx, p, repoKey, "")
	if err != nil {
		return nil, err
	}
	byID := map[string][]storedRef{}
	for _, n := range nodes {
		if id, version, ok := splitAnyNupkgNode(n.Path); ok {
			byID[id] = append(byID[id], storedRef{path: n.Path, version: version, node: n})
		}
	}
	read := func(path string) ([]byte, bool) { return h.readSidecar(ctx, p, repoKey, path) }
	for id, refs := range byID {
		out[id] = factsOfRefs(read, id, refs)
	}
	return out, nil
}

// collectLocalFactsDeep resolves one package id through the chain as a
// UNION: the canonical directory listing (the "default flat path" level)
// plus the whole-repository walk's matches (the property-index level —
// path-prefix publishes surface there, and a package with BOTH shapes
// answers with every version it has).
func (h *Handler) collectLocalFactsDeep(ctx context.Context, p *repo.Principal, repoKey, id string) ([]versionFacts, error) {
	facts, err := h.collectLocalFacts(ctx, p, repoKey, id)
	if err != nil {
		return nil, err
	}
	nodes, err := h.svc.List(ctx, p, repoKey, "")
	if err != nil {
		if len(facts) > 0 {
			return facts, nil // the deep level is additive, never fatal
		}
		return nil, err
	}
	seen := map[string]bool{}
	for _, f := range facts {
		seen[f.ref.version] = true
	}
	merged := facts
	for _, n := range nodes {
		if gotID, version, ok := splitAnyNupkgNode(n.Path); ok && gotID == id && !seen[version] {
			seen[version] = true
			merged = append(merged, versionFacts{ref: pkgRef{id: id, version: version}, node: n})
		}
	}
	if len(merged) > len(facts) {
		// The deep level found versions the canonical listing lacks: rebuild
		// the WHOLE set from every located node (canonical ones included —
		// their base names match the same identity rule, and the builder
		// reads the same sibling sidecars).
		var refs []storedRef
		for _, n := range nodes {
			if gotID, version, ok := splitAnyNupkgNode(n.Path); ok && gotID == id {
				refs = append(refs, storedRef{path: n.Path, version: version, node: n})
			}
		}
		read := func(path string) ([]byte, bool) { return h.readSidecar(ctx, p, repoKey, path) }
		merged = factsOfRefs(read, id, refs)
	}
	return merged, nil
}

// factsOfRefs builds the fact rows off located nodes: versions ascending,
// the nuspec/sha512 sidecars read next to each node (the sibling names the
// flatcontainer trio defines — canonical publishes spell them at the
// canonical directory by construction).
func factsOfRefs(read func(string) ([]byte, bool), id string, refs []storedRef) []versionFacts {
	sort.Slice(refs, func(i, j int) bool {
		return compareNuGetVersions(refs[i].version, refs[j].version) < 0
	})
	facts := make([]versionFacts, 0, len(refs))
	for _, ref := range refs {
		f := versionFacts{ref: pkgRef{id: id, version: ref.version}, node: ref.node}
		dir := ""
		if i := strings.LastIndexByte(ref.path, '/'); i >= 0 {
			dir = ref.path[:i+1]
		}
		stem := strings.TrimSuffix(lastSegment(ref.path), suffixNupkg)
		if body, ok := read(dir + stem + suffixNuspec); ok {
			if info, perr := parseNuspec(body); perr == nil {
				f.nuspec = info
			}
		}
		if body, ok := read(dir + stem + suffixSha512); ok {
			f.sha512 = trimSpaceASCII(string(body))
		}
		facts = append(facts, f)
	}
	return facts
}

// splitAnyNupkgNode splits ANY stored nupkg node path into (id, version):
// the LAST segment must spell <id>.<version>.nupkg. The split point scans
// LEFT to RIGHT — the LONGEST valid version tail wins, because a version's
// core carries 2-4 dot-separated numeric fields while ids are free-form
// ("pfx.pkg.2.0.0" is (pfx.pkg, 2.0.0), never (pfx.pkg.2.0, 0)). This is
// the property-index level's match: wherever the package lives, its base
// name carries its identity.
func splitAnyNupkgNode(path string) (string, string, bool) {
	base := lastSegment(path)
	if base == "" || !strings.HasSuffix(base, suffixNupkg) {
		return "", "", false
	}
	stem := base[:len(base)-len(suffixNupkg)]
	for i := strings.IndexByte(stem, '.'); i > 0; i = strings.IndexByte(stem[i+1:], '.') + i + 1 {
		version, ok := normalizeNuGetVersion(stem[i+1:])
		if !ok {
			continue
		}
		id := stem[:i]
		if !validPackageID(id) {
			continue
		}
		return lowerASCII(id), version, true
	}
	return "", "", false
}

// v2LocalRows renders the local class's rows for one collection request.
func (h *Handler) v2LocalRows(ctx context.Context, _ *http.Request, p *repo.Principal, repoKey string, req *v2FeedReq) ([]v2Row, error) {
	var ids []string
	switch req.kind {
	case kindV2FindPackages:
		ids = []string{req.id}
	case kindV2GetUpdates:
		ids = req.updates.ids
	}
	grouped, err := h.v2StoredFacts(ctx, p, repoKey, ids)
	if err != nil {
		return nil, err
	}
	var rows []v2Row
	for _, facts := range grouped {
		for _, f := range facts {
			rows = append(rows, rowFromFacts(f))
		}
	}
	return rows, nil
}

// v2CollectRows dispatches the row source per repository class.
func (h *Handler) v2CollectRows(ctx context.Context, r *http.Request, p *repo.Principal, repoKey, class string, req *v2FeedReq) ([]v2Row, error) {
	switch class {
	case repo.TypeRemote:
		return h.v2RemoteRows(ctx, r, p, repoKey, req)
	case repo.TypeVirtual:
		return h.v2VirtualRows(ctx, r, repoKey, req)
	default:
		return h.v2LocalRows(ctx, r, p, repoKey, req)
	}
}

// ---- v2 Download (#14), the bare .nupkg face (#15), DELETE (#16) and the
// publish pair (#17/#18) ----

// v2NotFoundMsg is #14's miss wording.
func v2NotFoundMsg(id, version, repoKey string) string {
	return fmt.Sprintf("Unable to find NuPkg '%s-%s' in '%s'", id, version, repoKey)
}

// serveV2Download renders GET Download/{id}/{version}: the three-level
// resolution per class (sections 5.3/6). The remote class walks the
// dynamically resolved flatcontainer marker first (the section 9.4
// packageContent rewrite cites THIS face — the v3-cached copy must serve
// here), then the canonical probe, then the alternative-download hop —
// section 5.3's upstream URL <base>/<downloadContextPath>/<id>/<version>
// (default api/v2/package, the xsd value) — through the engine's marker
// cache.
func (h *Handler) serveV2Download(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, class string, rt route) {
	switch class {
	case repo.TypeRemote:
		ref := pkgRef{id: rt.id, version: rt.version}
		for _, path := range []string{h.v3FlatMarkerPath(ctx, p, repoKey, ref.nupkg()), ref.nupkg(), v2DownloadCachePath(rt.id, rt.version)} {
			if rc, node, err := h.svc.Get(ctx, p, repoKey, path); err == nil {
				h.serveNode(ctx, w, r, node, rc, "application/octet-stream")
				return
			}
		}
		writePlain(w, http.StatusNotFound, v2NotFoundMsg(rt.id, rt.version, repoKey))
	case repo.TypeVirtual:
		h.serveV2VirtualDownload(ctx, w, r, repoKey, rt)
	default:
		rc, node, err := h.openLocalNupkg(ctx, p, repoKey, rt.id, rt.version)
		if err != nil {
			if errors.Is(err, repo.ErrForbidden) || errors.Is(err, repo.ErrUnauthorized) {
				h.writeError(w, err, repoKey, rt.id)
				return
			}
			// The unfound family carries #14's wording — the three-level
			// chain having nothing is the miss message, not the bare 404.
			writePlain(w, http.StatusNotFound, v2NotFoundMsg(rt.id, rt.version, repoKey))
			return
		}
		h.serveNode(ctx, w, r, node, rc, "application/octet-stream")
	}
}

// openLocalNupkg resolves one version through the chain and opens the
// package stream.
func (h *Handler) openLocalNupkg(ctx context.Context, p *repo.Principal, repoKey, id, version string) (io.ReadSeekCloser, *metadata.Node, error) {
	ref := pkgRef{id: id, version: version}
	rc, node, err := h.svc.Get(ctx, p, repoKey, ref.nupkg())
	if err == nil {
		return rc, node, nil
	}
	if !errors.Is(err, repo.ErrNodeNotFound) {
		return nil, nil, err
	}
	// The property-index level: the whole-repo walk (prefix publishes).
	path, ok := h.locateNupkgByFacts(ctx, p, repoKey, id, version)
	if !ok {
		return nil, nil, fmt.Errorf("node %s/%s: %w", repoKey, ref.nupkg(), repo.ErrNodeNotFound)
	}
	return h.svc.Get(ctx, p, repoKey, path)
}

// locateNupkgByFacts finds one version's stored path through the listing
// walk.
func (h *Handler) locateNupkgByFacts(ctx context.Context, p *repo.Principal, repoKey, id, version string) (string, bool) {
	nodes, err := h.svc.List(ctx, p, repoKey, "")
	if err != nil {
		return "", false
	}
	for _, n := range nodes {
		if gotID, gotVersion, ok := splitAnyNupkgNode(n.Path); ok && gotID == id && gotVersion == version {
			return n.Path, true
		}
	}
	return "", false
}

// serveV2BareNupkg renders GET {file}.nupkg (#15): no v2 door of its own —
// the request straight-lines into storage (section 3's exception row), the
// identity mapping onto the node path.
func (h *Handler) serveV2BareNupkg(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, rel string) {
	rc, node, err := h.svc.Get(ctx, p, repoKey, rel)
	if err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}
	h.serveNode(ctx, w, r, node, rc, "application/octet-stream")
}

// serveV2Delete renders DELETE /{path} (#16): the first two segments are
// the id/version, the rest ignored; the resolution is the three-level
// chain; the wire is the 200/404/403 text family.
func (h *Handler) serveV2Delete(ctx context.Context, w http.ResponseWriter, p *repo.Principal, repoKey, class string, rt route) {
	idRaw, rest, found := strings.Cut(rt.path, "/")
	if !found {
		writePlain(w, http.StatusNotFound, "not found")
		return
	}
	versionRaw, _, _ := strings.Cut(rest, "/")
	id := lowerASCII(idRaw)
	version, ok := normalizeNuGetVersion(versionRaw)
	if !validPackageID(id) || !ok {
		writePlain(w, http.StatusNotFound, "not found")
		return
	}
	switch class {
	case repo.TypeRemote:
		writePlain(w, http.StatusBadRequest, msgV2LocalOnly)
		return
	case repo.TypeVirtual:
		row, err := h.repos.Get(ctx, repoKey)
		if err != nil {
			h.writeError(w, err, repoKey, rt.path)
			return
		}
		target := virtualDeployTarget(row.Config)
		if target == "" {
			writePlain(w, http.StatusBadRequest, msgV2LocalOnly)
			return
		}
		h.deleteV2Version(ctx, w, p, target, id, version, rt.path)
		return
	}
	h.deleteV2Version(ctx, w, p, repoKey, id, version, rt.path)
}

// deleteV2Version runs the resolved delete on one LOCAL repository: the
// canonical directory when it stands, else the located node file and its
// sibling sidecars.
func (h *Handler) deleteV2Version(ctx context.Context, w http.ResponseWriter, p *repo.Principal, repoKey, id, version, wirePath string) {
	ref := pkgRef{id: id, version: version}
	if _, _, err := h.svc.Get(ctx, p, repoKey, ref.nupkg()); err == nil {
		if derr := h.svc.Delete(ctx, p, repoKey, ref.dir()+"/"); derr != nil {
			writeV2DeleteError(w, derr, wirePath)
			return
		}
		writeText(w, http.StatusOK, fmt.Sprintf("Successfully removed '%s'", wirePath))
		return
	} else if !errors.Is(err, repo.ErrNodeNotFound) {
		writeV2DeleteError(w, err, wirePath)
		return
	}
	path, ok := h.locateNupkgByFacts(ctx, p, repoKey, id, version)
	if !ok {
		writePlain(w, http.StatusNotFound, "not found")
		return
	}
	if derr := h.svc.Delete(ctx, p, repoKey, path); derr != nil {
		writeV2DeleteError(w, derr, wirePath)
		return
	}
	// The sibling sidecars of a prefixed publish go with the package.
	dir := path[:strings.LastIndexByte(path, '/')+1]
	stem := strings.TrimSuffix(lastSegment(path), suffixNupkg)
	for _, sibling := range []string{stem + suffixNuspec, stem + suffixSha512} {
		_ = h.svc.Delete(ctx, p, repoKey, dir+sibling) //nolint:errcheck // best-effort cleanup; the package itself is gone
	}
	writeText(w, http.StatusOK, fmt.Sprintf("Successfully removed '%s'", wirePath))
}

// writeV2DeleteError maps the delete failure onto #16's 403 wording.
func writeV2DeleteError(w http.ResponseWriter, err error, wirePath string) {
	if errors.Is(err, repo.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
		writePlain(w, http.StatusUnauthorized, "authentication required")
		return
	}
	writePlain(w, http.StatusForbidden, fmt.Sprintf("Unable to delete NuGet package '%s'", wirePath))
}

// virtualDeployTarget reads a virtual repository's defaultDeploymentRepo
// (the repo package's alias set, mirrored here because that seam is
// unexported — the ClassReader hands the Config blob verbatim).
func virtualDeployTarget(config string) string {
	var probe struct {
		DefaultDeploymentRepo    string `json:"defaultDeploymentRepo"`
		DefaultDeploymentRepoRef string `json:"defaultDeploymentRepoRef"`
		DeploymentRepository     string `json:"deploymentRepository"`
	}
	if err := json.Unmarshal([]byte(config), &probe); err != nil {
		return ""
	}
	for _, alias := range []string{probe.DefaultDeploymentRepo, probe.DefaultDeploymentRepoRef, probe.DeploymentRepository} {
		if alias != "" {
			return alias
		}
	}
	return ""
}

// serveV2Publish renders the PUT pair (#17 root / #18 path prefix): the
// identity ALWAYS comes from the package's own nuspec; the URL contributes
// only the deployment prefix (deployPath = <prefix>/<id>.<version>.nupkg,
// section 5.1's derivation). The duplicate arm follows section 5.1: an
// existing package plus a principal without the overwrite right answers
// the 409 with the exact wording.
func (h *Handler) serveV2Publish(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, class string, rt route) {
	_ = ctx
	switch class {
	case repo.TypeRemote:
		// Before the body drains (section 5.1's rclass matrix).
		writePlain(w, http.StatusBadRequest, msgV2LocalOnly)
		return
	case repo.TypeVirtual:
		row, err := h.repos.Get(r.Context(), repoKey)
		if err != nil {
			h.writeError(w, err, repoKey, rt.path)
			return
		}
		if virtualDeployTarget(row.Config) == "" {
			writePlain(w, http.StatusBadRequest, msgV2LocalOnly)
			return
		}
	}

	body, closeBody, err := packageBodyReader(r)
	if err != nil {
		h.writePushError(w, err)
		return
	}
	defer closeBody()
	sp, err := spoolNupkg(body)
	if err != nil {
		h.writePushError(w, err)
		return
	}
	defer sp.close()

	nuspecBody, target, err := sp.identityFromNuspec()
	if err != nil {
		h.writePushError(w, err)
		return
	}

	// The deployment path (section 5.1's derivation, BinFlow's layout):
	// the ROOT form lands at the canonical flatcontainer layout — the v2
	// and v3 faces share one storage, the T-287 contract — and the
	// path-prefix form honors the prefix literally (<prefix>/<id>.<version>
	// .nupkg; the T-287-era <id>/<version> spelling therefore lands
	// canonically BY CONSTRUCTION). Prefix publishes stay addressable
	// through the deep chain (section 6's property-index level).
	deployPath := target.nupkg()
	if rt.kind == kindV2Push && rt.path != "" {
		deployPath = rt.path + "/" + target.id + "." + target.version + suffixNupkg
	}

	// The duplicate arm's probe: an existing package is what turns the
	// service's overwrite refusal into the 409 below (section 5.1's
	// isPackageAlreadyExistAndCantBeDeletedByCurrentUser).
	existed := false
	if _, _, gerr := h.svc.Get(r.Context(), p, repoKey, deployPath); gerr == nil {
		existed = true
	}

	// The measured sha256 rides as the declared digest: a same-bytes
	// retransmit lands idempotent, a different package demands the
	// overwrite right — the service's own permission pair (repo-semantics
	// section 3), which is exactly the 409/overwrite split of 5.1.
	if _, err := sp.file.Seek(0, io.SeekStart); err != nil {
		writePlain(w, http.StatusInternalServerError, fmt.Sprintf("rewind spool: %v", err))
		return
	}
	expect := storage.BlobRef{Sha256: sp.sha256Hex()}
	if _, perr := h.svc.Put(r.Context(), p, repoKey, deployPath, sp.file, expect, "application/octet-stream"); perr != nil {
		switch {
		case existed && errors.Is(perr, repo.ErrForbidden):
			writeText(w, http.StatusConflict, "Package already exist: "+deployPath)
		case errors.Is(perr, repo.ErrPatternRejected):
			writeText(w, http.StatusConflict, msgV2UnacceptablePath)
		default:
			h.writeError(w, perr, repoKey, deployPath)
		}
		return
	}

	// The sidecars land next to the package (the sibling names the
	// flatcontainer trio defines — canonical publishes spell the trio at
	// the canonical directory by construction).
	dir := ""
	if i := strings.LastIndexByte(deployPath, '/'); i >= 0 {
		dir = deployPath[:i+1]
	}
	h.putSidecar(r.Context(), p, repoKey, dir+target.id+"."+target.version+suffixNuspec, nuspecBody, "application/xml")
	h.putSidecar(r.Context(), p, repoKey, dir+target.id+"."+target.version+suffixSha512, []byte(sp.sha512Base64()), "text/plain; charset=utf-8")

	writeText(w, http.StatusCreated, "Successfully published NuPkg to: "+deployPath)
}

// putSidecar lands one regenerable sidecar (warn-only, the push posture).
func (h *Handler) putSidecar(ctx context.Context, p *repo.Principal, repoKey, path string, body []byte, mime string) {
	if _, err := h.svc.PutWithOptions(ctx, p, repoKey, path,
		bytes.NewReader(body), blobRefOf(body), mime,
		repo.PutOptions{SkipOverwriteCheck: true}); err != nil {
		h.warnSidecar(ctx, repoKey, path, err)
	}
}
