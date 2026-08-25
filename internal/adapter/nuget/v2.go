package nuget

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The v2 minimal face (PRD 88.1: FindPackagesById() is the carrier —
// nuget.exe-era clients and the curl assertions; Search()/Packages()Id=/
// $count stay unanswered pending the T-280/T-293 adjudication, the K28
// boundary ruling recorded in doc.go). The entity shape is the
// nuget.org V2FeedPackage set (the OData $metadata this file serves is
// the contract the feed's m:properties block must answer to).
//
// The feed answers from the same stored facts as the registrations, so
// the v2 and v3 faces can never disagree about a package's existence.

// v2Namespaces are the feed document's fixed namespace declarations.
const (
	v2AtomNS       = "http://www.w3.org/2005/Atom"
	v2DataNS       = "http://schemas.microsoft.com/ado/2007/08/dataservices"
	v2MetadataNS   = "http://schemas.microsoft.com/ado/2007/08/dataservices/metadata"
	v2SchemaNS     = "http://schemas.microsoft.com/ado/2008/09/edm"
	v2EdmxNS       = "http://schemas.microsoft.com/ado/2007/06/edmx"
	v2EntitySchema = "http://schemas.microsoft.com/ado/2007/08/dataservices/scheme"
)

// serveV2Feed renders GET FindPackagesById()?id='<id>' — the Atom feed of
// every stored version, ascending.
func (h *Handler) serveV2Feed(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, class string) {
	id := v2QueryID(r)
	if id == "" {
		writePlain(w, http.StatusBadRequest, "FindPackagesById requires a non-empty id parameter")
		return
	}

	facts, ok := h.v2Facts(ctx, p, repoKey, class, id)
	if !ok {
		// The empty feed is the protocol's "unknown id" (the OData family
		// answers collections, never 404s).
		facts = nil
	}

	origin := h.baseURLFor(r)
	base := v2Base(origin, repoKey)
	body := renderV2Feed(base, flatBase(origin, repoKey), id, facts)
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body) //nolint:gosec // G705: server-rendered XML over escaped fields
	}
}

// v2Facts collects the feed's fact rows per class (the registration
// collector's twin; the v2 renderer needs the nuspec's dependency list,
// so the sidecar read is not skippable here).
func (h *Handler) v2Facts(ctx context.Context, p *repo.Principal, repoKey, class, id string) ([]versionFacts, bool) {
	switch class {
	case repo.TypeVirtual:
		order, err := h.svc.VirtualMemberOrder(ctx, repoKey)
		if err != nil {
			return nil, false
		}
		var all []versionFacts
		seen := map[string]bool{}
		for _, m := range order {
			if m.Type != repo.TypeLocal {
				continue
			}
			facts, ferr := h.collectMemberFacts(ctx, repoKey, m.Key, id)
			if ferr != nil {
				continue
			}
			for _, f := range facts {
				if !seen[f.ref.version] {
					seen[f.ref.version] = true
					all = append(all, f)
				}
			}
		}
		sort.Slice(all, func(i, j int) bool { return compareNuGetVersions(all[i].ref.version, all[j].ref.version) < 0 })
		return all, true
	default:
		facts, err := h.collectLocalFacts(ctx, p, repoKey, id)
		if err != nil {
			return nil, false
		}
		return facts, true
	}
}

// v2QueryID extracts the OData id parameter ('single-quoted').
func v2QueryID(r *http.Request) string {
	raw := r.URL.Query().Get("id")
	raw = trimSpaceASCII(raw)
	if len(raw) >= 2 {
		if (raw[0] == '\'' && raw[len(raw)-1] == '\'') || (raw[0] == '"' && raw[len(raw)-1] == '"') {
			raw = raw[1 : len(raw)-1]
		}
	}
	raw = strings.TrimSpace(raw) // OData doubled-quote escaping collapses to one
	raw = strings.ReplaceAll(raw, "''", "'")
	if !validPackageID(raw) {
		return ""
	}
	return lowerASCII(raw)
}

// renderV2Feed renders the Atom feed.
func renderV2Feed(base, flat, id string, facts []versionFacts) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString(`<feed xmlns="` + v2AtomNS + `" xmlns:d="` + v2DataNS + `" xmlns:m="` + v2MetadataNS + `" xml:base="`)
	xmlEscape(&b, base)
	b.WriteString(`">` + "\n")
	writeV2Element(&b, "id", base+"/FindPackagesById()?id='"+xmlEscapeString(id)+"'")
	writeV2Element(&b, "title", "Packages")
	writeV2Element(&b, "updated", v2Timestamp(facts))
	b.WriteString(`<link rel="self" href="/FindPackagesById()?id='` + xmlEscapeString(id) + `'"/>` + "\n")

	latestStable, latestAbsolute := v2LatestVersions(facts)
	for _, f := range facts {
		renderV2Entry(&b, base, flat, f, latestStable, latestAbsolute)
	}
	b.WriteString("</feed>")
	return b.Bytes()
}

// renderV2Entry renders one version's <entry>.
func renderV2Entry(b *bytes.Buffer, base, flat string, f versionFacts, latestStable, latestAbsolute string) {
	id, version := f.ref.id, f.ref.version
	// The wire identity keeps the nuspec's ORIGINAL id casing (the
	// gallery spelling); the storage key is the lowercase fold.
	displayID := id
	if f.nuspec != nil && f.nuspec.id != "" {
		displayID = f.nuspec.id
	}
	entryID := fmt.Sprintf("%s/Packages(Id='%s',Version='%s')", base, xmlEscapeString(displayID), xmlEscapeString(version))
	content := fmt.Sprintf("%s%s/%s.%s%s", flat, f.ref.dir(), id, version, suffixNupkg)

	b.WriteString("<entry>\n")
	writeV2Element(b, "id", entryID)
	writeV2Title(b, displayID)
	writeV2Element(b, "updated", firstNonEmpty(f.node.UpdatedAt, f.node.CreatedAt, "1970-01-01T00:00:00Z"))
	b.WriteString("<author><name>")
	xmlEscape(b, v2Author(f))
	b.WriteString("</name></author>\n")
	b.WriteString(`<link rel="edit-media" href="Packages(Id='` + xmlEscapeString(id) + `',Version='` + xmlEscapeString(version) + `')"/>` + "\n")
	b.WriteString(`<category term="NuGetGallery.V2FeedPackage" scheme="` + v2EntitySchema + `"/>` + "\n")
	b.WriteString(`<content type="application/zip" src="` + xmlEscapeString(content) + `"/>` + "\n")

	b.WriteString("<m:properties>")
	writeV2Property(b, "d:Id", displayID)
	writeV2Property(b, "d:Version", version)
	writeV2Property(b, "d:Title", v2String(f.nuspec, func(n *nuspecInfo) string { return n.title }))
	writeV2Property(b, "d:Description", v2String(f.nuspec, func(n *nuspecInfo) string { return n.description }))
	writeV2Property(b, "d:Summary", v2String(f.nuspec, func(n *nuspecInfo) string { return n.summary }))
	writeV2Property(b, "d:Tags", v2String(f.nuspec, func(n *nuspecInfo) string { return strings.Join(n.tags, " ") }))
	writeV2Property(b, "d:Authors", v2Author(f))
	writeV2Property(b, "d:Copyright", "")
	writeV2Property(b, "d:Language", v2String(f.nuspec, func(n *nuspecInfo) string { return n.language }))
	writeV2Property(b, "d:LicenseUrl", v2String(f.nuspec, func(n *nuspecInfo) string { return n.licenseURL }))
	writeV2Property(b, "d:ProjectUrl", v2String(f.nuspec, func(n *nuspecInfo) string { return n.projectURL }))
	writeV2Property(b, "d:IconUrl", v2String(f.nuspec, func(n *nuspecInfo) string { return n.iconURL }))
	writeV2Property(b, "d:MinClientVersion", v2String(f.nuspec, func(n *nuspecInfo) string { return n.minClientVersion }))
	writeV2Property(b, "d:ReleaseNotes", "")
	writeV2Property(b, "d:PackageHash", f.sha512)
	writeV2Property(b, "d:PackageHashAlgorithm", "SHA512")
	writeV2IntProperty(b, "d:PackageSize", f.node.Size)
	writeV2IntProperty(b, "d:DownloadCount", 0)
	writeV2IntProperty(b, "d:VersionDownloadCount", 0)
	writeV2Element(b, "d:Published", firstNonEmpty(f.node.CreatedAt, f.node.UpdatedAt, "1970-01-01T00:00:00Z"))
	writeV2Element(b, "d:Created", firstNonEmpty(f.node.CreatedAt, f.node.UpdatedAt, "1970-01-01T00:00:00Z"))
	writeV2BoolProperty(b, "d:IsLatestVersion", version == latestStable)
	writeV2BoolProperty(b, "d:IsAbsoluteLatestVersion", version == latestAbsolute)
	writeV2BoolProperty(b, "d:IsPrerelease", isPrereleaseVersion(version))
	writeV2BoolProperty(b, "d:RequireLicenseAcceptance", f.nuspec != nil && f.nuspec.requireLicenseAccept)
	writeV2Property(b, "d:Dependencies", v2Dependencies(f.nuspec))
	writeV2Property(b, "d:GalleryDetailsUrl", "")
	b.WriteString("</m:properties>\n")
	b.WriteString("</entry>\n")
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
// "id:range:tfm|id:range:tfm" (empty parts omitted-kept: the grammar is
// positional — "Newtonsoft.Json:[13.0.3,):netstandard2.0").
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

// v2LatestVersions picks the feed's latest anchors — the newest STABLE
// version (IsLatestVersion) and the newest overall (IsAbsoluteLatestVersion,
// prereleases included — the v2 gallery's own semantics). Either may be
// "" when the feed holds no version of that class.
func v2LatestVersions(facts []versionFacts) (stable, absolute string) {
	for _, f := range facts {
		if !isPrereleaseVersion(f.ref.version) {
			stable = f.ref.version
		}
		absolute = f.ref.version
	}
	return stable, absolute
}

// v2Timestamp renders the feed's updated stamp (the newest row's).
func v2Timestamp(facts []versionFacts) string {
	if len(facts) == 0 {
		return "1970-01-01T00:00:00Z"
	}
	last := facts[len(facts)-1]
	return firstNonEmpty(last.node.UpdatedAt, last.node.CreatedAt, "1970-01-01T00:00:00Z")
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

// serveV2Metadata renders the static EDMX document (the entity set the
// feed answers to; old clients fetch it before their first query).
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
      </EntityContainer>
    </Schema>
  </edmx:DataServices>
</edmx:Edmx>
`)
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body) //nolint:gosec // G705: static server-rendered document
	}
}
