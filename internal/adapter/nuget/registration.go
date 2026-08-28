package nuget

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The registration face (the official Registration Blob spec).
//
// LOCAL repositories generate the document from stored facts: one inline
// page over every version, each leaf's catalogEntry rendered from the
// nuspec sidecar, packageHash from the sha512 sidecar (synthesized off
// the blob when a sidecar is missing), dependencyGroups from the nuspec.
//
// REMOTE repositories resolve the upstream's service INDEX first (nuget.md
// section 9.2's type ladder — the T-304 L4 ruling: upstream resources are
// located by TYPE through the live index, never by a path-prefix constant),
// fetch the document through the engine under the .nuGetV3/<upstream-path>/
// marker (section 9.3's cache layout; the engine's TTL split rides the
// provider facet), and REWRITE the absolute URLs it embeds onto this
// repository's own faces per section 9.4: the id/registration family onto
// the v3 registration base, packageContent onto the v2 Download face (a
// verbatim body would point the client straight at the upstream for every
// download and the pull-through cache would never see a hit). Upstream
// pagination survives: page @ids rewrite onto the registration page
// route, which proxies and rewrites in turn. nuget.org's registration
// endpoints force Content-Encoding: gzip even without negotiation (probed
// live, August 2026); the gunzip-on-magic step below is what makes those
// bodies servable.
//
// VIRTUAL repositories merge the members: the version union in member
// order, each version's leaf taken from the first member that carries it
// (the two-bucket order the resolution machinery fixes), local members'
// leaves citing the MEMBER's own download bases (first-hit resolution
// then serves from the right member); remote members' leaves arrive
// through the member seam under the same dynamic markers and re-anchor
// onto the member's bases the same way.

// ---- the JSON shapes (field set = what the official clients read) ----

// registrationIndex is the {base}/{id}/index.json body.
type registrationIndex struct {
	Count int                 `json:"count"`
	Items []*registrationPage `json:"items"`
}

// registrationPage is one catalog page (inline items on BinFlow's
// generated documents; count-only with itemsUrl on proxied pages).
type registrationPage struct {
	ID       string              `json:"@id"`
	Type     string              `json:"@type,omitempty"`
	Commit   string              `json:"commitId,omitempty"`
	When     string              `json:"commitTimeStamp,omitempty"`
	Count    int                 `json:"count"`
	Lower    string              `json:"lower"`
	Upper    string              `json:"upper"`
	Items    []*registrationLeaf `json:"items,omitempty"`
	ItemsURL string              `json:"itemsUrl,omitempty"`
}

// registrationLeaf is one package-version leaf.
type registrationLeaf struct {
	ID             string        `json:"@id"`
	Type           string        `json:"@type,omitempty"`
	Commit         string        `json:"commitId,omitempty"`
	When           string        `json:"commitTimeStamp,omitempty"`
	CatalogEntry   *catalogEntry `json:"catalogEntry"`
	PackageContent string        `json:"packageContent"`
}

// catalogEntry is the leaf's metadata block.
type catalogEntry struct {
	ID                    string                 `json:"id"`
	Version               string                 `json:"version"`
	Description           string                 `json:"description"`
	Summary               string                 `json:"summary"`
	Title                 string                 `json:"title"`
	Language              string                 `json:"language"`
	LicenseURL            string                 `json:"licenseUrl"`
	ProjectURL            string                 `json:"projectUrl"`
	IconURL               string                 `json:"iconUrl"`
	Authors               string                 `json:"authors"`
	Owners                string                 `json:"owners"`
	Tags                  string                 `json:"tags"`
	RequireLicenseAccept  bool                   `json:"requireLicenseAcceptance"`
	IsPrerelease          bool                   `json:"isPrerelease"`
	Listed                bool                   `json:"listed"`
	Hidden                bool                   `json:"hidden"`
	DevelopmentDependency bool                   `json:"developmentDependency"`
	Published             string                 `json:"published"`
	Created               string                 `json:"created,omitempty"`
	LastEdited            string                 `json:"lastEdited,omitempty"`
	Downloads             int64                  `json:"downloads"`
	Verified              bool                   `json:"verified"`
	PackageContent        string                 `json:"packageContent"`
	PackageHash           string                 `json:"packageHash"`
	PackageHashAlgorithm  string                 `json:"packageHashAlgorithm"`
	PackageSize           int64                  `json:"packageSize"`
	DependencyGroups      []*dependencyGroupJSON `json:"dependencyGroups,omitempty"`
	MinClientVersion      string                 `json:"minClientVersion,omitempty"`
}

// dependencyGroupJSON is one dependency group.
type dependencyGroupJSON struct {
	Type            string            `json:"@type,omitempty"`
	TargetFramework string            `json:"targetFramework,omitempty"`
	Dependencies    []*dependencyJSON `json:"dependencies,omitempty"`
}

// dependencyJSON is one dependency row.
type dependencyJSON struct {
	Type  string `json:"@type,omitempty"`
	ID    string `json:"id"`
	Range string `json:"range,omitempty"`
}

// ---- dispatch ----

// serveRegistration dispatches GET registration[-semver2]/<id>/index.json.
func (h *Handler) serveRegistration(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, class string, rt route) {
	switch class {
	case repo.TypeRemote:
		h.serveUpstreamRegistration(ctx, w, r, p, repoKey, rt.semver2, rt.id+"/"+fileIndex)
	case repo.TypeVirtual:
		h.serveVirtualRegistration(ctx, w, r, p, repoKey, rt)
	default:
		body, err := h.buildLocalRegistration(ctx, r, p, repoKey, rt.id)
		if err != nil {
			h.writeError(w, err, repoKey, rt.id)
			return
		}
		writeJSON(w, http.StatusOK, body)
	}
}

// serveRegistrationPage dispatches GET registration[-semver2]/<id>/
// page/<file> — the remote pagination passthrough (generated documents
// are single inline pages and never address one).
func (h *Handler) serveRegistrationPage(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, class string, rt route) {
	tail := rt.id + "/" + segPage + "/" + rt.file
	switch class {
	case repo.TypeRemote:
		h.serveUpstreamRegistration(ctx, w, r, p, repoKey, rt.semver2, tail)
	case repo.TypeVirtual:
		// The first member whose copy answers wins (the tolerance rule).
		order, err := h.svc.VirtualMemberOrder(ctx, repoKey)
		if err != nil {
			h.writeError(w, err, repoKey, tail)
			return
		}
		for _, m := range order {
			read := h.v3MemberReader(ctx, repoKey, m.Key)
			body, rerr := read(v3CachePath(v3ResolveUpstreamIndex(read).registrationPath(rt.semver2), tail))
			if rerr != nil {
				continue
			}
			h.writeRewritten(w, r, v3RewriteJSON(body, h.memberRewriteTable(ctx, r, repoKey, m.Key)))
			return
		}
		writePlain(w, http.StatusNotFound, "not found")
	default:
		writePlain(w, http.StatusNotFound, "not found")
	}
}

// serveRegistrationLeaf dispatches GET registration[-semver2]/<id>/
// <version>.json — the single-version display family (nuget.md section
// 9.1's PackageVersionDisplayMetadataUriTemplate shape).
func (h *Handler) serveRegistrationLeaf(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, class string, rt route) {
	tail := rt.id + "/" + rt.version + ".json"
	switch class {
	case repo.TypeRemote:
		h.serveUpstreamRegistration(ctx, w, r, p, repoKey, rt.semver2, tail)
	case repo.TypeVirtual:
		leaves, _, err := h.virtualRegistrationLeaves(ctx, repoKey, rt.id, h.baseURLFor(r))
		if err != nil {
			h.writeError(w, err, repoKey, tail)
			return
		}
		leaf, ok := leaves[rt.version]
		if !ok {
			writePlain(w, http.StatusNotFound, "not found")
			return
		}
		h.writeSingleLeaf(w, leaf)
	default:
		facts, err := h.collectLocalFacts(ctx, p, repoKey, rt.id)
		if err != nil {
			h.writeError(w, err, repoKey, tail)
			return
		}
		for _, f := range facts {
			if f.ref.version == rt.version {
				origin := h.baseURLFor(r)
				h.writeSingleLeaf(w, renderLeaf(f, flatBase(origin, repoKey), regBase(origin, repoKey)))
				return
			}
		}
		writePlain(w, http.StatusNotFound, "not found")
	}
}

// writeSingleLeaf renders the one-leaf page document (the official
// single-version display shape: a count-1 catalog page).
func (h *Handler) writeSingleLeaf(w http.ResponseWriter, leaf *registrationLeaf) {
	body, err := json.Marshal(registrationIndex{
		Count: 1,
		Items: []*registrationPage{{
			ID: leaf.ID, Type: "catalog:CatalogPage", Count: 1,
			Lower: leafVersionOr(leaf), Upper: leafVersionOr(leaf),
			Items: []*registrationLeaf{leaf},
		}},
	})
	if err != nil {
		writePlain(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// leafVersionOr extracts a leaf's version for the page bounds ("" on the
// degenerate shape).
func leafVersionOr(leaf *registrationLeaf) string {
	if leaf != nil && leaf.CatalogEntry != nil {
		return leaf.CatalogEntry.Version
	}
	return ""
}

// ---- the local generated arm ----

// versionFacts is one version's render input set.
type versionFacts struct {
	ref    pkgRef
	node   *metadata.Node
	nuspec *nuspecInfo
	sha512 string
}

// buildLocalRegistration renders the local class's registration index.
func (h *Handler) buildLocalRegistration(ctx context.Context, r *http.Request, p *repo.Principal, repoKey, id string) ([]byte, error) {
	facts, err := h.collectLocalFacts(ctx, p, repoKey, id)
	if err != nil {
		return nil, err
	}
	if len(facts) == 0 {
		return nil, repo.ErrNodeNotFound
	}
	origin := h.baseURLFor(r)
	flat, reg := flatBase(origin, repoKey), regBase(origin, repoKey)

	items := make([]*registrationLeaf, 0, len(facts))
	for _, f := range facts {
		items = append(items, renderLeaf(f, flat, reg))
	}
	doc := registrationIndex{
		Count: 1,
		Items: []*registrationPage{{
			ID:    reg + id + "/" + fileIndex + "#page",
			Type:  "catalog:CatalogPage",
			Count: len(items),
			Lower: facts[0].ref.version,
			Upper: facts[len(facts)-1].ref.version,
			Items: items,
		}},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("render registration: %w", err)
	}
	return body, nil
}

// collectLocalFacts gathers one package's render inputs (versions sorted
// ascending; sidecars read best-effort, hashes synthesized when absent).
func (h *Handler) collectLocalFacts(ctx context.Context, p *repo.Principal, repoKey, id string) ([]versionFacts, error) {
	rows, err := h.listPackageVersions(ctx, p, repoKey, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	facts := make([]versionFacts, 0, len(rows))
	for _, row := range rows {
		ref := pkgRef{id: id, version: row.version}
		f := versionFacts{ref: ref, node: row.node}
		if body, ok := h.readSidecar(ctx, p, repoKey, ref.nuspec()); ok {
			if info, perr := parseNuspec(body); perr == nil {
				f.nuspec = info
			}
		}
		if body, ok := h.readSidecar(ctx, p, repoKey, ref.sha512()); ok {
			f.sha512 = trimSpaceASCII(string(body))
		}
		if f.sha512 == "" {
			f.sha512 = h.hashBlobBase64(ctx, p, repoKey, ref.nupkg())
		}
		facts = append(facts, f)
	}
	return facts, nil
}

// readSidecar reads one small sidecar body (false = absent/unreadable).
func (h *Handler) readSidecar(ctx context.Context, p *repo.Principal, repoKey, path string) ([]byte, bool) {
	rc, _, err := h.svc.Get(ctx, p, repoKey, path)
	if err != nil {
		return nil, false
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	body, err := io.ReadAll(io.LimitReader(rc, 8<<20))
	if err != nil {
		return nil, false
	}
	return body, true
}

// hashBlobBase64 streams one stored blob through SHA-512 (the synthesis
// fallback; "" when the blob cannot be read).
func (h *Handler) hashBlobBase64(ctx context.Context, p *repo.Principal, repoKey, path string) string {
	rc, _, err := h.svc.Get(ctx, p, repoKey, path)
	if err != nil {
		return ""
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	sum := sha512.New()
	if _, err := io.Copy(sum, rc); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(sum.Sum(nil))
}

// renderLeaf renders one version's leaf (nuspec facts when present, the
// path facts otherwise — a bare-content upload still lists correctly).
func renderLeaf(f versionFacts, flat, reg string) *registrationLeaf {
	id, version := f.ref.id, f.ref.version
	leafID := fmt.Sprintf("%s%s/%s.json", reg, id, version)
	content := fmt.Sprintf("%s%s/%s.%s%s", flat, f.ref.dir(), id, version, suffixNupkg)

	entry := &catalogEntry{
		ID:                   id,
		Version:              version,
		Listed:               true,
		Published:            firstNonEmpty(f.node.UpdatedAt, f.node.CreatedAt, "1970-01-01T00:00:00Z"),
		PackageContent:       content,
		PackageHashAlgorithm: "SHA512",
		PackageSize:          f.node.Size,
		PackageHash:          f.sha512,
	}
	if ns := f.nuspec; ns != nil {
		entry.ID = ns.id
		entry.Version = ns.version
		entry.Title = ns.title
		entry.Summary = ns.summary
		entry.Description = ns.description
		entry.Language = ns.language
		entry.LicenseURL = ns.licenseURL
		entry.ProjectURL = ns.projectURL
		entry.IconURL = ns.iconURL
		entry.Authors = strings.Join(ns.authors, ", ")
		entry.Owners = strings.Join(ns.owners, ", ")
		entry.Tags = strings.Join(ns.tags, " ")
		entry.RequireLicenseAccept = ns.requireLicenseAccept
		entry.IsPrerelease = isPrereleaseVersion(ns.version)
		entry.DevelopmentDependency = ns.developmentDependency
		entry.MinClientVersion = ns.minClientVersion
		entry.DependencyGroups = renderDependencyGroups(ns.dependencyGroups)
	} else {
		entry.IsPrerelease = isPrereleaseVersion(version)
	}
	return &registrationLeaf{
		ID:             leafID,
		Type:           "Package",
		When:           entry.Published,
		CatalogEntry:   entry,
		PackageContent: content,
	}
}

// renderDependencyGroups maps nuspec groups onto the wire shape.
func renderDependencyGroups(groups []dependencyGroup) []*dependencyGroupJSON {
	if len(groups) == 0 {
		return nil
	}
	out := make([]*dependencyGroupJSON, 0, len(groups))
	for _, g := range groups {
		gj := &dependencyGroupJSON{
			Type:            "PackageDependencyGroup",
			TargetFramework: g.targetFramework,
		}
		for _, d := range g.deps {
			gj.Dependencies = append(gj.Dependencies, &dependencyJSON{
				Type:  "PackageDependency",
				ID:    d.id,
				Range: d.rangeSpec,
			})
		}
		out = append(out, gj)
	}
	return out
}

// firstNonEmpty is the strings fallback helper.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- the remote proxy arm (dynamic: nuget.md sections 9.2–9.4) ----

// serveUpstreamRegistration resolves the upstream registration base for
// the requested shape (the section 9.2 ladder), pulls the document
// through the engine's .nuGetV3 marker cache, rewrites it per section 9.4
// and serves it.
func (h *Handler) serveUpstreamRegistration(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey string, semver2 bool, tail string) {
	read := h.v3RepoReader(ctx, p, repoKey)
	idx := v3ResolveUpstreamIndex(read)
	marker := v3CachePath(idx.registrationPath(semver2), tail)
	body, err := read(marker)
	if err != nil {
		h.writeError(w, err, repoKey, marker)
		return
	}
	tbl := v3BuildRewriteTable(h.baseURLFor(r), repoKey, idx, true, false)
	h.writeRewritten(w, r, v3RewriteJSON(body, tbl))
}

// memberRewriteTable builds the section 9.4 table anchored on one MEMBER's
// own faces (the virtual merge's remote leaves: the download stays inside
// the member — first-hit resolution then serves from there — so the v2
// arm of the single-repository rewrite does not apply; the deviation
// register in the ticket log carries the nuance).
func (h *Handler) memberRewriteTable(ctx context.Context, r *http.Request, virtualKey, member string) *v3rewriteTable {
	read := h.v3MemberReader(ctx, virtualKey, member)
	idx := v3ResolveUpstreamIndex(read)
	return v3BuildRewriteTable(h.baseURLFor(r), member, idx, false, false)
}

// writeRewritten renders one rewritten document.
func (h *Handler) writeRewritten(w http.ResponseWriter, r *http.Request, body []byte) {
	hdr := w.Header()
	hdr.Set("Content-Type", "application/json")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Length", fmt.Sprint(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body) //nolint:gosec // G705: rewritten upstream document, served as JSON
	}
}

// gunzipIfNeeded decodes a forced-gzip body (nuget.org's registration
// endpoints send Content-Encoding: gzip regardless of negotiation —
// probed live, August 2026). Plain bodies pass through untouched.
func gunzipIfNeeded(body []byte) []byte {
	if len(body) < 2 || body[0] != 0x1f || body[1] != 0x8b {
		return body
	}
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return body
	}
	decoded, err := io.ReadAll(io.LimitReader(zr, 64<<20))
	if err != nil {
		return body
	}
	_ = zr.Close() //nolint:errcheck // read-only decoder
	return decoded
}

// indexByteFrom is bytes.IndexByte from an offset.
func indexByteFrom(b []byte, from int, c byte) int {
	for i := from; i < len(b); i++ {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// ---- the virtual arm ----

// serveVirtualVersions merges the members' version sets (member-order
// dedup, ascending render). Remote members' documents arrive through the
// dynamic .nuGetV3 markers.
func (h *Handler) serveVirtualVersions(ctx context.Context, w http.ResponseWriter, repoKey string, rt route) {
	order, err := h.svc.VirtualMemberOrder(ctx, repoKey)
	if err != nil {
		h.writeError(w, err, repoKey, rt.id)
		return
	}
	seen := map[string]bool{}
	var versions []string
	for _, m := range order {
		var memberVersions []string
		if m.Type == repo.TypeLocal {
			rows, lerr := h.svc.ListVirtualMember(ctx, repoKey, m.Key, rt.id+"/")
			if lerr != nil {
				continue
			}
			for _, n := range rows {
				if v, ok := versionOfNupkgNode(n.Path, rt.id); ok {
					memberVersions = append(memberVersions, v)
				}
			}
		} else {
			read := h.v3MemberReader(ctx, repoKey, m.Key)
			flat := v3ResolveUpstreamIndex(read).flatPath()
			body, rerr := read(v3CachePath(flat, versionsPath(rt.id)))
			if rerr != nil {
				continue
			}
			var doc versionsDocument
			if json.Unmarshal(body, &doc) != nil {
				continue
			}
			memberVersions = doc.Versions
		}
		for _, v := range memberVersions {
			if !seen[v] {
				seen[v] = true
				versions = append(versions, v)
			}
		}
	}
	sort.Slice(versions, func(i, j int) bool { return compareNuGetVersions(versions[i], versions[j]) < 0 })
	body, merr := json.Marshal(versionsDocument{Versions: versions})
	if merr != nil {
		writePlain(w, http.StatusInternalServerError, merr.Error())
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// readMemberDocument reads one member's copy of a metadata document
// (remote members through the full pull-through chain).
func (h *Handler) readMemberDocument(ctx context.Context, virtualKey, member, path string) ([]byte, bool) {
	rc, _, err := h.svc.ReadVirtualMember(ctx, virtualKey, member, path)
	if err != nil {
		return nil, false
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	body, err := io.ReadAll(io.LimitReader(rc, 64<<20))
	if err != nil {
		return nil, false
	}
	return gunzipIfNeeded(body), true
}

// serveVirtualRegistration merges the members' registration indexes: the
// version union in member order, each version's leaf taken from the first
// member that carries it. Local members' leaves are rendered from facts
// citing the MEMBER's bases (the download stays inside the member — the
// virtual's first-hit resolution then serves it from there); remote
// members' leaves arrive through the member seam under the dynamic
// .nuGetV3 markers and are re-anchored onto the member's own bases.
func (h *Handler) serveVirtualRegistration(ctx context.Context, w http.ResponseWriter, r *http.Request, _ *repo.Principal, repoKey string, rt route) {
	leaves, versions, err := h.virtualRegistrationLeaves(ctx, repoKey, rt.id, h.baseURLFor(r))
	if err != nil {
		h.writeError(w, err, repoKey, rt.id)
		return
	}
	if len(leaves) == 0 {
		writePlain(w, http.StatusNotFound, "not found")
		return
	}
	origin := h.baseURLFor(r)
	sort.Slice(versions, func(i, j int) bool { return compareNuGetVersions(versions[i], versions[j]) < 0 })
	items := make([]*registrationLeaf, 0, len(versions))
	for _, v := range versions {
		items = append(items, leaves[v])
	}
	doc := registrationIndex{
		Count: 1,
		Items: []*registrationPage{{
			ID:    regBase(origin, repoKey) + rt.id + "/" + fileIndex + "#page",
			Type:  "catalog:CatalogPage",
			Count: len(items),
			Lower: versions[0],
			Upper: versions[len(versions)-1],
			Items: items,
		}},
	}
	body, merr := json.Marshal(doc)
	if merr != nil {
		writePlain(w, http.StatusInternalServerError, merr.Error())
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// virtualRegistrationLeaves collects one package id's merged leaf set:
// per member, local facts render onto the member's bases and remote
// proxies re-anchor onto the member's bases; first-seen version wins.
func (h *Handler) virtualRegistrationLeaves(ctx context.Context, repoKey, id, origin string) (map[string]*registrationLeaf, []string, error) {
	order, err := h.svc.VirtualMemberOrder(ctx, repoKey)
	if err != nil {
		return nil, nil, err
	}
	leaves := map[string]*registrationLeaf{}
	var versions []string
	for _, m := range order {
		if m.Type == repo.TypeLocal {
			facts, ferr := h.collectMemberFacts(ctx, repoKey, m.Key, id)
			if ferr != nil {
				continue
			}
			memberFlat, memberReg := flatBase(origin, m.Key), regBase(origin, m.Key)
			for _, f := range facts {
				if _, seen := leaves[f.ref.version]; !seen {
					leaves[f.ref.version] = renderLeaf(f, memberFlat, memberReg)
					versions = append(versions, f.ref.version)
				}
			}
			continue
		}
		read := h.v3MemberReader(ctx, repoKey, m.Key)
		idx := v3ResolveUpstreamIndex(read)
		body, rerr := read(v3CachePath(idx.registrationPath(false), id+"/"+fileIndex))
		if rerr != nil {
			continue
		}
		var doc registrationIndex
		if json.Unmarshal(body, &doc) != nil {
			continue
		}
		tbl := v3BuildRewriteTable(origin, m.Key, idx, false, false)
		for _, page := range doc.Items {
			for _, leaf := range page.Items {
				v, ok := leafVersion(leaf)
				if !ok {
					continue
				}
				if _, seen := leaves[v]; !seen {
					leaves[v] = rewriteLeafOnto(leaf, tbl)
					versions = append(versions, v)
				}
			}
		}
	}
	return leaves, versions, nil
}

// collectMemberFacts is collectLocalFacts through the member seam.
func (h *Handler) collectMemberFacts(ctx context.Context, virtualKey, member, id string) ([]versionFacts, error) {
	nodes, err := h.svc.ListVirtualMember(ctx, virtualKey, member, id+"/")
	if err != nil {
		return nil, err
	}
	var rows []packageVersionRow
	for _, n := range nodes {
		if v, ok := versionOfNupkgNode(n.Path, id); ok {
			rows = append(rows, packageVersionRow{version: v, node: n})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return compareNuGetVersions(rows[i].version, rows[j].version) < 0 })
	facts := make([]versionFacts, 0, len(rows))
	for _, row := range rows {
		ref := pkgRef{id: id, version: row.version}
		f := versionFacts{ref: ref, node: row.node}
		if body, ok := h.readMemberSidecar(ctx, virtualKey, member, ref.nuspec()); ok {
			if info, perr := parseNuspec(body); perr == nil {
				f.nuspec = info
			}
		}
		if body, ok := h.readMemberSidecar(ctx, virtualKey, member, ref.sha512()); ok {
			f.sha512 = trimSpaceASCII(string(body))
		}
		facts = append(facts, f)
	}
	return facts, nil
}

// readMemberSidecar reads one sidecar through the member seam.
func (h *Handler) readMemberSidecar(ctx context.Context, virtualKey, member, path string) ([]byte, bool) {
	rc, _, err := h.svc.ReadVirtualMember(ctx, virtualKey, member, path)
	if err != nil {
		return nil, false
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	body, err := io.ReadAll(io.LimitReader(rc, 8<<20))
	if err != nil {
		return nil, false
	}
	return body, true
}

// leafVersion extracts a proxied leaf's version key (normalized).
func leafVersion(leaf *registrationLeaf) (string, bool) {
	if leaf == nil || leaf.CatalogEntry == nil || leaf.CatalogEntry.Version == "" {
		return "", false
	}
	return normalizeNuGetVersion(leaf.CatalogEntry.Version)
}

// rewriteLeafOnto re-anchors a proxied leaf's URLs onto the table's
// repository bases (the member-anchored virtual posture).
func rewriteLeafOnto(leaf *registrationLeaf, tbl *v3rewriteTable) *registrationLeaf {
	if leaf == nil || tbl == nil {
		return leaf
	}
	leaf.ID = tbl.rewriteToken(leaf.ID)
	if leaf.CatalogEntry != nil {
		leaf.CatalogEntry.PackageContent = tbl.rewriteToken(leaf.CatalogEntry.PackageContent)
	}
	leaf.PackageContent = tbl.rewriteToken(leaf.PackageContent)
	return leaf
}
