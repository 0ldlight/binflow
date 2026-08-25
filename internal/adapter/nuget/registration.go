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
// REMOTE repositories proxy the upstream document (cached at the marker
// path — the engine's TTL split rides the provider facet) and REWRITE the
// absolute URLs it embeds onto this repository's own bases: a verbatim
// body would point the client straight at the upstream for every download
// and the pull-through cache would never see a hit. Upstream pagination
// survives: page @ids rewrite onto the registration page route, which
// proxies and rewrites in turn. nuget.org's registration endpoints force
// Content-Encoding: gzip even without negotiation (probed live, August
// 2026); the gunzip-on-magic step below is what makes those bodies
// servable.
//
// VIRTUAL repositories merge the members: the version union in member
// order, each version's leaf taken from the first member that carries it
// (the two-bucket order the resolution machinery fixes), local members'
// leaves citing the MEMBER's own download bases (first-hit resolution
// then serves from the right member).

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

// serveRegistration dispatches GET registration/<id>/index.json.
func (h *Handler) serveRegistration(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, class string, rt route) {
	switch class {
	case repo.TypeRemote:
		h.serveProxiedDocument(ctx, w, r, p, repoKey, repoKey, rt.path)
	case repo.TypeVirtual:
		h.serveVirtualRegistration(ctx, w, r, p, repoKey, rt)
	default:
		body, err := h.buildLocalRegistration(ctx, r, p, repoKey, rt.id)
		if err != nil {
			h.writeError(w, err, repoKey, rt.path)
			return
		}
		writeJSON(w, http.StatusOK, body)
	}
}

// serveRegistrationPage dispatches GET registration/<id>/page/<file> —
// the remote pagination passthrough (generated documents are single
// inline pages and never address one).
func (h *Handler) serveRegistrationPage(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, class string, rt route) {
	switch class {
	case repo.TypeRemote:
		h.serveProxiedDocument(ctx, w, r, p, repoKey, repoKey, rt.path)
	case repo.TypeVirtual:
		// The first member whose copy answers wins (the tolerance rule).
		order, err := h.svc.VirtualMemberOrder(ctx, repoKey)
		if err != nil {
			h.writeError(w, err, repoKey, rt.path)
			return
		}
		for _, m := range order {
			rc, _, err := h.svc.ReadVirtualMember(ctx, repoKey, m.Key, rt.path)
			if err != nil {
				continue
			}
			body, rerr := io.ReadAll(io.LimitReader(rc, 64<<20))
			_ = rc.Close() //nolint:errcheck // read-only fd
			if rerr != nil {
				continue
			}
			h.writeRewritten(w, r, h.rewriteDocument(body, h.baseURLFor(r), repoKey))
			return
		}
		writePlain(w, http.StatusNotFound, "not found")
	default:
		writePlain(w, http.StatusNotFound, "not found")
	}
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

// ---- the remote proxy arm ----

// serveProxiedDocument pulls one remote document through the engine's
// cache (the marker path), gunzips and rewrites, and serves it.
func (h *Handler) serveProxiedDocument(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, upstreamRepo, path string) {
	rc, _, err := h.svc.Get(ctx, p, upstreamRepo, path)
	if err != nil {
		h.writeError(w, err, repoKey, path)
		return
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	body, err := io.ReadAll(io.LimitReader(rc, 64<<20))
	if err != nil {
		writePlain(w, http.StatusInternalServerError, fmt.Sprintf("read cached document: %v", err))
		return
	}
	h.writeRewritten(w, r, h.rewriteDocument(gunzipIfNeeded(body), h.baseURLFor(r), repoKey))
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

// rewriteDocument rewrites the upstream document's embedded absolute URLs
// onto THIS repository's own bases. The upstream host spellings vary
// (nuget.org's documents cite api.nuget.org even when the bytes arrive
// from a mirror), so the rewrite matches by URL PATH shape, never by
// host: any "<scheme>://<host>/<regPrefix>/<rest>" becomes
// "<origin>/binflow/api/nuget/v3/<repo>/registration/<rest>"; any
// "<scheme>://<host>/v3-flatcontainer/<rest>" becomes the flatcontainer
// equivalent. Other absolute URLs (license/project/icon) pass untouched.
//
// The walk runs over the JSON token stream (string literals only), so
// every non-URL byte stays identical — no decode/re-encode of the whole
// tree, no number reformattings.
func (h *Handler) rewriteDocument(body []byte, origin, repoKey string) []byte {
	if !json.Valid(body) {
		return body
	}
	var b strings.Builder
	b.Grow(len(body))
	i := 0
	for i < len(body) {
		start := indexByteFrom(body, i, '"')
		if start < 0 {
			b.Write(body[i:])
			break
		}
		end := indexByteFrom(body, start+1, '"')
		if end < 0 {
			b.Write(body[i:])
			break
		}
		b.Write(body[i : start+1])
		b.WriteString(rewriteOneURL(string(body[start+1:end]), origin, repoKey))
		b.WriteByte('"')
		i = end + 1
	}
	return []byte(b.String())
}

// rewriteOneURL rewrites one JSON string token when it is an upstream
// registration or flatcontainer URL; every other token returns identical.
func rewriteOneURL(tok, origin, repoKey string) string {
	const (
		schemeMark = "://"
		flatPath   = "v3-flatcontainer/"
	)
	if len(tok) < len(schemeMark)+1 {
		return tok
	}
	i := strings.Index(tok, schemeMark)
	if i <= 0 {
		return tok // not an absolute URL
	}
	rest := tok[i+len(schemeMark):]
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		rest = rest[j+1:]
	} else {
		return tok // scheme://host with no path
	}
	switch {
	case strings.HasPrefix(rest, upstreamRegistrationPrefix+"/"):
		return regBase(origin, repoKey) + rest[len(upstreamRegistrationPrefix)+1:]
	case strings.HasPrefix(rest, flatPath):
		return flatBase(origin, repoKey) + rest[len(flatPath):]
	default:
		return tok
	}
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
// dedup, ascending render).
func (h *Handler) serveVirtualVersions(ctx context.Context, w http.ResponseWriter, repoKey string, rt route) {
	order, err := h.svc.VirtualMemberOrder(ctx, repoKey)
	if err != nil {
		h.writeError(w, err, repoKey, rt.path)
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
			body, ok := h.readMemberDocument(ctx, repoKey, m.Key, versionsPath(rt.id))
			if !ok {
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
// members' leaves arrive through the member seam and are re-anchored onto
// the member's own bases the same way.
func (h *Handler) serveVirtualRegistration(ctx context.Context, w http.ResponseWriter, r *http.Request, _ *repo.Principal, repoKey string, rt route) {
	order, err := h.svc.VirtualMemberOrder(ctx, repoKey)
	if err != nil {
		h.writeError(w, err, repoKey, rt.path)
		return
	}
	origin := h.baseURLFor(r)

	leaves := map[string]*registrationLeaf{}
	var versions []string
	for _, m := range order {
		if m.Type == repo.TypeLocal {
			facts, ferr := h.collectMemberFacts(ctx, repoKey, m.Key, rt.id)
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
		body, ok := h.readMemberDocument(ctx, repoKey, m.Key, regMarker(rt.id))
		if !ok {
			continue
		}
		var doc registrationIndex
		if json.Unmarshal(body, &doc) != nil {
			continue
		}
		for _, page := range doc.Items {
			for _, leaf := range page.Items {
				v, ok := leafVersion(leaf)
				if !ok {
					continue
				}
				if _, seen := leaves[v]; !seen {
					leaves[v] = rewriteLeafOnto(leaf, origin, m.Key)
					versions = append(versions, v)
				}
			}
		}
	}
	if len(leaves) == 0 {
		writePlain(w, http.StatusNotFound, "not found")
		return
	}
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

// rewriteLeafOnto re-anchors a proxied leaf's URLs onto the named
// repository's bases.
func rewriteLeafOnto(leaf *registrationLeaf, origin, repoKey string) *registrationLeaf {
	if leaf == nil {
		return leaf
	}
	leaf.ID = rewriteOneURL(leaf.ID, origin, repoKey)
	if leaf.CatalogEntry != nil {
		leaf.CatalogEntry.PackageContent = rewriteOneURL(leaf.CatalogEntry.PackageContent, origin, repoKey)
	}
	leaf.PackageContent = rewriteOneURL(leaf.PackageContent, origin, repoKey)
	return leaf
}
