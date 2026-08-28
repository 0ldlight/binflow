package nuget

import (
	"context"
	"encoding/hex"
	"encoding/xml"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The v2 remote search family's upstream proxy (nuget.md section 7.1): an
// ONLINE remote repository answers Search()/Packages()/GetUpdates()/
// FindPackagesById() (±$count) by proxying the upstream v2 OData face in
// real time — the VERBATIM resource spelling plus the request's full query
// string ride a cache marker (the cargo search-marker posture: the engine's
// TTL/negative/stale machinery applies per query), the landed feed is
// parsed and re-rendered with every entry URL re-anchored onto THIS
// repository's v2 Download face. An upstream fault falls back to the
// repository's landed facts — BinFlow's equivalent of section 7.1-1's
// offline arm — and an empty answer everywhere is the family's 404, never
// a 5xx. The $count family answers the upstream's digits; a body that is
// not digits is the -1 failure sentinel (7.1-2).

// v2CacheDir is the marker directory under a remote repository's cache.
const v2CacheDir = ".nuget-v2"

// v2FeedContextPath is the upstream v2 feed's context path (the xsd
// default feedContextPath; BinFlow exposes no per-repo override — the
// repository URL joins onto it, exactly the spec's default form).
const v2FeedContextPath = "api/v2"

// v2DownloadContextPath is the upstream alternative-download face's
// context path (the xsd default downloadContextPath, section 5.3).
const v2DownloadContextPath = "api/v2/package"

// v2CountSentinel is the count family's failure value (section 7.1-2).
const v2CountSentinel = -1

// v2ResourceOf renders the upstream resource spelling of one collection
// request: "<Resource>[()][/$count]?<verbatim query>".
func v2ResourceOf(req *v2FeedReq) string {
	var b strings.Builder
	switch req.kind {
	case kindV2Search:
		b.WriteString(v2Search + "()")
	case kindV2GetUpdates:
		b.WriteString(v2GetUpdates + "()")
	case kindV2Packages:
		if req.entryID != "" {
			b.WriteString(v2Packages + "(Id='" + req.entryID + "'")
			if req.entryVersion != "" {
				b.WriteString(",Version='" + req.entryVersion + "'")
			}
			b.WriteString(")")
		} else {
			b.WriteString(v2Packages + "()")
		}
	default:
		b.WriteString(findPackagesByID + "()")
	}
	if req.count {
		b.WriteString("/" + v2Count)
	}
	if req.rawQuery != "" {
		b.WriteString("?" + req.rawQuery)
	}
	return b.String()
}

// v2CachePath renders the storage path one upstream v2 response caches
// under: .nuget-v2/<hex of the resource spelling>.xml. The hex keeps the
// key path-legal and collision-free while staying a PURE, reversible
// encoding — the provider's UpstreamPath facet decodes it back into the
// query-carrying upstream endpoint (the cargo search-marker twin).
func v2CachePath(resource string) string {
	return v2CacheDir + "/" + hex.EncodeToString([]byte(resource)) + ".xml"
}

// v2ResourceOfCachePath decodes one search-cache storage path back into its
// resource spelling; ok is false for every other shape.
func v2ResourceOfCachePath(path string) (string, bool) {
	rest, found := strings.CutPrefix(path, v2CacheDir+"/")
	if !found {
		return "", false
	}
	name, ok := strings.CutSuffix(rest, ".xml")
	if !ok || name == "" || strings.Contains(name, "/") {
		return "", false
	}
	raw, err := hex.DecodeString(name)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// v2DownloadCachePath is the alternative-download hop's marker (section
// 5.3): the .nupkg suffix rides the content class (long TTL — a package is
// immutable), the provider's facet maps it onto the upstream
// api/v2/package/<id>/<version> face.
func v2DownloadCachePath(id, version string) string {
	return v2CacheDir + "/dl/" + id + "." + version + suffixNupkg
}

// v2DownloadCacheOf decodes the alternative-download marker.
func v2DownloadCacheOf(path string) (id, version string, ok bool) {
	rest, found := strings.CutPrefix(path, v2CacheDir+"/dl/")
	if !found {
		return "", "", false
	}
	stem, isPkg := strings.CutSuffix(rest, suffixNupkg)
	if !isPkg || strings.Contains(stem, "/") {
		return "", "", false
	}
	gotID, gotVersion, parsed := splitAnyNupkgNode("x/" + stem + suffixNupkg)
	if !parsed {
		return "", "", false
	}
	return gotID, gotVersion, true
}

// v2RemoteRows collects the remote class's rows: the upstream proxy first,
// the landed-facts fallback behind it.
func (h *Handler) v2RemoteRows(ctx context.Context, r *http.Request, p *repo.Principal, repoKey string, req *v2FeedReq) ([]v2Row, error) {
	if rows, ok := h.v2ProxyFeed(ctx, p, repoKey, req); ok {
		return rows, nil
	}
	// The offline arm (7.1-1): answer from the repository's landed facts —
	// the engine serves a stale cached copy inside v2ProxyFeed when one
	// stands; this is the never-cached-anything arm. The fact walk's class
	// refusal (svc.List answers read-only for local repositories) is
	// swallowed: a remote repository with nothing landed simply answers
	// the empty collection, never the 400.
	rows, err := h.v2LocalRows(ctx, r, p, repoKey, req)
	if err != nil {
		return nil, nil
	}
	return rows, nil
}

// v2RemoteCount answers the remote $count family: the upstream digits, the
// -1 sentinel on a non-numeric body (7.1-2), and the landed count when the
// hop gave nothing at all.
func (h *Handler) v2RemoteCount(ctx context.Context, p *repo.Principal, repoKey string, req *v2FeedReq) (int, bool) {
	rc, _, err := h.svc.Get(ctx, p, repoKey, v2CachePath(v2ResourceOf(req)))
	if err != nil {
		return 0, false
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	body, rerr := io.ReadAll(io.LimitReader(rc, 1<<20))
	if rerr != nil {
		return v2CountSentinel, true
	}
	n, perr := strconv.Atoi(strings.TrimSpace(string(body)))
	if perr != nil {
		return v2CountSentinel, true
	}
	return n, true
}

// v2ProxyFeed fetches and parses the upstream feed through the engine's
// marker cache. ok is false whenever the hop gave nothing (the caller falls
// back); the parsed rows keep the upstream's own latest flags — the level
// ladder was the upstream's to apply, its answer is re-rendered as-is.
func (h *Handler) v2ProxyFeed(ctx context.Context, p *repo.Principal, repoKey string, req *v2FeedReq) ([]v2Row, bool) {
	rc, _, err := h.svc.Get(ctx, p, repoKey, v2CachePath(v2ResourceOf(req)))
	if err != nil {
		return nil, false
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	body, rerr := io.ReadAll(io.LimitReader(rc, 64<<20))
	if rerr != nil {
		return nil, false
	}
	rows, perr := parseV2Feed(gunzipIfNeeded(body))
	if perr != nil || len(rows) == 0 {
		return nil, false
	}
	return rows, true
}

// parseV2Feed parses one upstream Atom feed into rows (the property set is
// the same V2FeedPackage family this package renders — pass-through
// rendering with re-anchored URLs).
func parseV2Feed(body []byte) ([]v2Row, error) {
	var feed v2FeedXML
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, err
	}
	rows := make([]v2Row, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		props := e.Properties
		if props.ID == "" || props.Version == "" {
			continue
		}
		rows = append(rows, v2Row{
			id: props.ID, version: props.Version,
			title:                    props.Title,
			description:              props.Description,
			summary:                  props.Summary,
			tags:                     props.Tags,
			authors:                  props.Authors,
			copyright:                props.Copyright,
			language:                 props.Language,
			licenseURL:               props.LicenseURL,
			projectURL:               props.ProjectURL,
			iconURL:                  props.IconURL,
			minClientVersion:         props.MinClientVersion,
			releaseNotes:             props.ReleaseNotes,
			packageHash:              props.PackageHash,
			hashAlgo:                 props.PackageHashAlgorithm,
			packageSize:              props.PackageSize,
			published:                props.Published,
			updated:                  e.Updated,
			created:                  props.Created,
			isLatest:                 props.IsLatest,
			isAbsLatest:              props.IsAbsoluteLatest,
			isPrerelease:             props.IsPrerelease,
			requireLicenseAcceptance: props.RequireLicenseAcceptance,
			dependencies:             props.Dependencies,
			galleryDetailsURL:        props.GalleryDetailsURL,
		})
	}
	return rows, nil
}

// v2FeedXML is the parsed upstream feed.
type v2FeedXML struct {
	Entries []v2EntryXML `xml:"entry"`
}

// v2EntryXML is one parsed upstream entry.
type v2EntryXML struct {
	Updated    string          `xml:"updated"`
	Properties v2PropertiesXML `xml:"properties"`
}

// v2PropertiesXML is the m:properties block.
type v2PropertiesXML struct {
	ID                       string `xml:"Id"`
	Version                  string `xml:"Version"`
	Title                    string `xml:"Title"`
	Description              string `xml:"Description"`
	Summary                  string `xml:"Summary"`
	Tags                     string `xml:"Tags"`
	Authors                  string `xml:"Authors"`
	Copyright                string `xml:"Copyright"`
	Language                 string `xml:"Language"`
	LicenseURL               string `xml:"LicenseUrl"`
	ProjectURL               string `xml:"ProjectUrl"`
	IconURL                  string `xml:"IconUrl"`
	MinClientVersion         string `xml:"MinClientVersion"`
	ReleaseNotes             string `xml:"ReleaseNotes"`
	PackageHash              string `xml:"PackageHash"`
	PackageHashAlgorithm     string `xml:"PackageHashAlgorithm"`
	PackageSize              int64  `xml:"PackageSize"`
	Published                string `xml:"Published"`
	Created                  string `xml:"Created"`
	IsLatest                 bool   `xml:"IsLatestVersion"`
	IsAbsoluteLatest         bool   `xml:"IsAbsoluteLatestVersion"`
	IsPrerelease             bool   `xml:"IsPrerelease"`
	RequireLicenseAcceptance bool   `xml:"RequireLicenseAcceptance"`
	Dependencies             string `xml:"Dependencies"`
	GalleryDetailsURL        string `xml:"GalleryDetailsUrl"`
}

// serveV2VirtualDownload is the virtual Download arm (section 5.3): the
// pattern pre-check degrades to BinFlow's governance (the default
// configuration refuses nothing here); the member walk takes the LOCAL
// members first, then the REMOTE members, each bucket in resolution order —
// the first 2xx wins; all-failed answers the 404
// (nuGetVirtualEnableForbiddenResponse defaults false).
func (h *Handler) serveV2VirtualDownload(ctx context.Context, w http.ResponseWriter, r *http.Request, repoKey string, rt route) {
	order, err := h.svc.VirtualMemberOrder(ctx, repoKey)
	if err != nil {
		h.writeError(w, err, repoKey, rt.path)
		return
	}
	canonical := pkgRef{id: rt.id, version: rt.version}.nupkg()
	serve := func(rc io.ReadSeekCloser, node *metadata.Node) {
		h.serveNode(ctx, w, r, node, rc, "application/octet-stream")
	}
	for _, pass := range []string{repo.TypeLocal, repo.TypeRemote} {
		for _, m := range order {
			if m.Type != pass {
				continue
			}
			if rc, node, gerr := h.svc.ReadVirtualMember(ctx, repoKey, m.Key, canonical); gerr == nil {
				serve(rc, node)
				return
			}
			if pass == repo.TypeLocal {
				// The local member's property-index level: the listing walk
				// (a prefixed publish inside the member).
				nodes, lerr := h.svc.ListVirtualMember(ctx, repoKey, m.Key, "")
				if lerr != nil {
					continue
				}
				for _, n := range nodes {
					if gotID, gotVersion, ok := splitAnyNupkgNode(n.Path); ok && gotID == rt.id && gotVersion == rt.version {
						if rc, node, gerr := h.svc.ReadVirtualMember(ctx, repoKey, m.Key, n.Path); gerr == nil {
							serve(rc, node)
							return
						}
					}
				}
				continue
			}
			// The remote member's dynamic flatcontainer marker (the
			// heterogeneous-upstream hop the service-level canonical
			// resolution cannot express), then the alternative-download
			// hop (5.3).
			read := h.v3MemberReader(ctx, repoKey, m.Key)
			marker := v3CachePath(v3ResolveUpstreamIndex(read).flatPath(), canonical)
			if rc, node, gerr := h.svc.ReadVirtualMember(ctx, repoKey, m.Key, marker); gerr == nil {
				serve(rc, node)
				return
			}
			if rc, node, gerr := h.svc.ReadVirtualMember(ctx, repoKey, m.Key, v2DownloadCachePath(rt.id, rt.version)); gerr == nil {
				serve(rc, node)
				return
			}
		}
	}
	writePlain(w, http.StatusNotFound, v2NotFoundMsg(rt.id, rt.version, repoKey))
}
