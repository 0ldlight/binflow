package httpapi

// L025-3A (D02 read family, rest-api.md sections 2.1.1-2.1.4): the
// repository-configuration read faces — the grouped all-configurations
// inventory, the project x type existence probe, the v2 single-repository
// configuration read (type-for-rclass dialect, Content-Type negotiation
// quirk, non-admin partial view) and the v2 batch read. The batch WRITE
// family (PUT/POST/DELETE /api/v2/repositories/batch) is ticket B.
//
// Field presence follows the spec's null-omission posture: BinFlow emits
// the keys it actually stores and omits the ones it does not model
// (notes/signedUrlTtl/propertySets/downloadRedirect/cdnRedirect/xrayIndex/
// xrayDataTtl/projectKey) — the reference's full 17/18-key sets ride on
// fields BinFlow's data model will grow later.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// repoBatchItemsLimit is repo.config.rest.create.items.limit's product
// default — the v2 batch family's per-request name cap (spec 2.1.4,
// medium confidence: decompile-sourced, pinned here so the 400 arm is
// observable).
const repoBatchItemsLimit = 100

// repoConfigurationEntry is one GET /api/repositories/configurations row:
// the COMMON field set (spec 2.1.1) — every rclass renders the same keys,
// no url, rclass lowercase. Modeled subset only; null fields are omitted.
type repoConfigurationEntry struct {
	Key                    string `json:"key"`
	PackageType            string `json:"packageType"`
	Description            string `json:"description"`
	IncludesPattern        string `json:"includesPattern,omitempty"`
	ExcludesPattern        string `json:"excludesPattern,omitempty"`
	RepoLayoutRef          string `json:"repoLayoutRef,omitempty"`
	PriorityResolution     *bool  `json:"priorityResolution,omitempty"`
	Environments           []any  `json:"environments,omitempty"`
	BlackedOut             *bool  `json:"blackedOut,omitempty"`
	ArchiveBrowsingEnabled *bool  `json:"archiveBrowsingEnabled,omitempty"`
	RClass                 string `json:"rclass"`
}

// repoConfigurationsBody is the grouped top level: uppercase class keys in
// the spec's fixed order, each present only when a repository of that class
// exists (omitempty), so the no-match filter arm renders {}. The Go field
// names stay CamelCase (revive) — the WIRE keys are the uppercase tags.
type repoConfigurationsBody struct {
	Local         []repoConfigurationEntry `json:"LOCAL,omitempty"`
	Remote        []repoConfigurationEntry `json:"REMOTE,omitempty"`
	Virtual       []repoConfigurationEntry `json:"VIRTUAL,omitempty"`
	Federated     []repoConfigurationEntry `json:"FEDERATED,omitempty"`
	ReleaseBundle []repoConfigurationEntry `json:"RELEASE_BUNDLE,omitempty"`
}

// repoClassUpper maps a stored rclass onto the response's uppercase class
// spelling (also the existence face's matchingRepoTypes vocabulary).
func repoClassUpper(rclass string) string {
	switch rclass {
	case repo.TypeLocal:
		return "LOCAL"
	case repo.TypeRemote:
		return "REMOTE"
	case repo.TypeVirtual:
		return "VIRTUAL"
	case "federated":
		return "FEDERATED"
	case "release_bundle":
		return "RELEASE_BUNDLE"
	}
	return ""
}

// commaFilterValues splits one comma-separated filter value into its OR set
// (nil = the axis is not filtered; empty parts are dropped).
func commaFilterValues(v string) map[string]bool {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	set := make(map[string]bool)
	for _, part := range strings.Split(v, ",") {
		if part != "" {
			set[part] = true
		}
	}
	return set
}

// repoRowBlob decodes a row's stored config blob; unparseable or empty
// blobs read as no keys (the caller-owned local posture).
func repoRowBlob(row *metadata.Repo) map[string]any {
	if row.Config == "" || row.Config == "{}" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(row.Config), &m); err != nil {
		return nil
	}
	return m
}

func blobString(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func blobBool(m map[string]any, key string) *bool {
	v, ok := m[key].(bool)
	if !ok {
		return nil
	}
	return &v
}

// handleRepoConfigurations serves GET /api/repositories/configurations
// (spec 2.1.1): admin-only grouped inventory, comma-OR filters on both
// axes (stackable), no-match filters answering 200 {} rather than an
// error, vendor list content type, Cache-Control: no-store.
//
// The admin gate renders the spec's verbatim 403 "Forbidden" envelope, so
// it lives in the handler (the route-level manage gate would answer the
// BinFlow "administrator privileges required" wording instead); the probe
// is CapRepoWrite — the capability only the system admin holds, BinFlow's
// equivalent of the reference's @RolesAllowed(admin).
func (s *Server) handleRepoConfigurations(w http.ResponseWriter, r *http.Request) {
	if !s.canManage(r.Context(), principalFrom(r.Context()), auth.CapRepoWrite) {
		writeError(w, http.StatusForbidden, "Forbidden")
		return
	}
	rows, err := s.deps.ReposSvc.ListReposFiltered(r.Context(), principalFrom(r.Context()), "", "")
	if err != nil {
		s.writeRepoSvcError(w, err)
		return
	}
	pkgSet := commaFilterValues(r.URL.Query().Get("packageType"))
	typeSet := commaFilterValues(r.URL.Query().Get("repoType"))

	var body repoConfigurationsBody
	for _, row := range rows {
		if pkgSet != nil && !pkgSet[row.PackageType] {
			continue
		}
		if typeSet != nil && !typeSet[row.Type] {
			continue
		}
		blob := repoRowBlob(row)
		entry := repoConfigurationEntry{
			Key:                    row.RepoKey,
			PackageType:            row.PackageType,
			Description:            row.Description,
			IncludesPattern:        blobString(blob, "includesPattern"),
			ExcludesPattern:        blobString(blob, "excludesPattern"),
			RepoLayoutRef:          blobString(blob, "repoLayoutRef"),
			PriorityResolution:     blobBool(blob, "priorityResolution"),
			BlackedOut:             blobBool(blob, "blackedOut"),
			ArchiveBrowsingEnabled: blobBool(blob, "archiveBrowsingEnabled"),
			RClass:                 row.Type,
		}
		if env, ok := blob["environments"].([]any); ok {
			entry.Environments = env
		}
		switch repoClassUpper(row.Type) {
		case "LOCAL":
			body.Local = append(body.Local, entry)
		case "REMOTE":
			body.Remote = append(body.Remote, entry)
		case "VIRTUAL":
			body.Virtual = append(body.Virtual, entry)
		case "FEDERATED":
			body.Federated = append(body.Federated, entry)
		case "RELEASE_BUNDLE":
			body.ReleaseBundle = append(body.ReleaseBundle, entry)
		}
	}
	byKey := func(g []repoConfigurationEntry) {
		sort.Slice(g, func(i, j int) bool { return g[i].Key < g[j].Key })
	}
	byKey(body.Local)
	byKey(body.Remote)
	byKey(body.Virtual)
	byKey(body.Federated)
	byKey(body.ReleaseBundle)

	w.Header().Set("Cache-Control", "no-store")
	writeJSONBodyCT(w, http.StatusOK,
		"application/vnd.org.jfrog.artifactory.repositories.RepositoryConfigurationsList+json", body)
}

// repoExistenceTypes is the existence face's type vocabulary: query spelling
// onto the uppercase class echo. The federated/release_bundle spellings are
// spec-pending details (the reference probe only exercised local/remote/
// virtual); they validate rather than 400 so a class BinFlow cannot host
// reads as exists:false instead of a hard error.
var repoExistenceTypes = map[string]string{
	repo.TypeLocal:   "LOCAL",
	repo.TypeRemote:  "REMOTE",
	repo.TypeVirtual: "VIRTUAL",
	"federated":      "FEDERATED",
	"release_bundle": "RELEASE_BUNDLE",
}

// repoClassOrder is matchingRepoTypes' canonical order (also the no-type
// echo of all five classes; the reference itself is order-unstable there).
var repoClassOrder = []string{"LOCAL", "REMOTE", "VIRTUAL", "FEDERATED", "RELEASE_BUNDLE"}

// repoExistenceBody is the existence response (spec 2.1.2): field order
// exists, matchingRepoTypes, projectKey.
type repoExistenceBody struct {
	Exists            bool     `json:"exists"`
	MatchingRepoTypes []string `json:"matchingRepoTypes"`
	ProjectKey        string   `json:"projectKey"`
}

// handleRepoExistence serves GET /api/repositories/existence (spec 2.1.2):
// the project x type probe. BinFlow has no projects domain, so every
// repository belongs to the default project — a non-default projectKey
// truthfully answers exists:false (the admin-passthrough echo behavior the
// spec pins for unknown project names). The gate is the target project's
// admin; with no project memberships in BinFlow that set is the system
// admin alone, and the refusal is the verbatim 403 "Forbidden" envelope.
// project= is silently ignored — the parameter's real name is projectKey.
func (s *Server) handleRepoExistence(w http.ResponseWriter, r *http.Request) {
	if !s.canManage(r.Context(), principalFrom(r.Context()), auth.CapRepoWrite) {
		writeError(w, http.StatusForbidden, "Forbidden")
		return
	}
	q := r.URL.Query()
	requested := make(map[string]bool)
	for _, v := range q["type"] {
		if v == "" {
			continue
		}
		class, ok := repoExistenceTypes[v]
		if !ok {
			writeError(w, http.StatusBadRequest, "Invalid repository type: "+v)
			return
		}
		requested[class] = true
	}
	projectKey := q.Get("projectKey")
	if projectKey == "" {
		projectKey = "default"
	}
	rows, err := s.deps.ReposSvc.ListRepos(r.Context(), principalFrom(r.Context()))
	if err != nil {
		s.writeRepoSvcError(w, err)
		return
	}
	exists := false
	if projectKey == "default" {
		for _, row := range rows {
			if len(requested) == 0 || requested[repoClassUpper(row.Type)] {
				exists = true
				break
			}
		}
	}
	matching := make([]string, 0, len(repoClassOrder))
	for _, class := range repoClassOrder {
		if len(requested) == 0 || requested[class] {
			matching = append(matching, class)
		}
	}
	writeJSONBody(w, http.StatusOK, repoExistenceBody{
		Exists: exists, MatchingRepoTypes: matching, ProjectKey: projectKey,
	})
}

// v2VendorConfigCTs are the per-rclass vendor media types of the v2
// configuration face — both the response Content-Type and (the quirk) the
// request Content-Type that drives type negotiation.
const (
	v2LocalConfigCT   = "application/vnd.org.jfrog.artifactory.repositories.localrepositoryconfiguration+json"
	v2RemoteConfigCT  = "application/vnd.org.jfrog.artifactory.repositories.remoterepositoryconfiguration+json"
	v2VirtualConfigCT = "application/vnd.org.jfrog.artifactory.repositories.virtualrepositoryconfiguration+json"
)

// v2ConfigVendorType maps a request's vendor Content-Type onto the rclass
// it names (keys lowercased; media types compare case-insensitively).
var v2ConfigVendorType = map[string]string{
	v2LocalConfigCT:   repo.TypeLocal,
	v2RemoteConfigCT:  repo.TypeRemote,
	v2VirtualConfigCT: repo.TypeVirtual,
}

// requestMediaType normalizes one Content-Type header onto its comparable
// media-type token (lowercased, parameters stripped).
func requestMediaType(ct string) string {
	media, _, _ := strings.Cut(ct, ";")
	return strings.ToLower(strings.TrimSpace(media))
}

// v2ResponseCT is the response Content-Type for one rclass (spec 2.1.3:
// always the vendor type of the row's class, never plain application/json).
func v2ResponseCT(rclass string) string {
	switch rclass {
	case repo.TypeLocal:
		return "application/vnd.org.jfrog.artifactory.repositories.LocalRepositoryConfiguration+json"
	case repo.TypeVirtual:
		return "application/vnd.org.jfrog.artifactory.repositories.VirtualRepositoryConfiguration+json"
	default:
		return "application/vnd.org.jfrog.artifactory.repositories.RemoteRepositoryConfiguration+json"
	}
}

// v2ConfigMap renders the admin-full v2 body: type replaces rclass, the
// common modeled subset rides every class, and the remote/virtual arms add
// their class fields off the canonical blob (already credential-masked by
// repo.Service, NFR-S14). Map rendering: the reference's per-class Jackson
// declaration order is not spec-pinned and the differential normalizes key
// order, so encoding/json's sorted-map output is the stable choice.
func v2ConfigMap(row *metadata.Repo, blob map[string]any) map[string]any {
	m := map[string]any{
		"key":         row.RepoKey,
		"type":        row.Type,
		"packageType": row.PackageType,
		"description": row.Description,
	}
	for _, k := range []string{
		"includesPattern", "excludesPattern", "repoLayoutRef", "priorityResolution",
		"environments", "blackedOut", "archiveBrowsingEnabled",
	} {
		if v, ok := blob[k]; ok {
			m[k] = v
		}
	}
	switch row.Type {
	case repo.TypeRemote:
		for _, k := range []string{
			"url", "username",
			"retrievalCachePeriodSecs", "missedRetrievalCachePeriodSecs",
			"socketTimeoutMillis", "metadataRetrievalTimeoutSecs",
			"unusedArtifactsCleanupPeriodHours", "assumedOfflinePeriodSecs",
			"hardFail", "contentSynchronisation", "listRemoteFolderItems",
		} {
			if v, ok := blob[k]; ok {
				m[k] = v
			}
		}
	case repo.TypeVirtual:
		if v, ok := blob["repositories"]; ok {
			m["repositories"] = v
		}
	}
	return m
}

// v2PartialConfigMap is the non-admin view (spec 2.1.3): the five-key
// projection measured on a remote repository {key,type,packageType,
// description,url}. The local/virtual arms of the projection were not
// probed — the same five-key set with the context URL is implemented here
// and registered as spec-pending in the L025-3A report.
func v2PartialConfigMap(r *http.Request, row *metadata.Repo, blob map[string]any) map[string]any {
	url := contextURL(r) + "/" + row.RepoKey
	if u, ok := remoteUpstreamURL(row, blob); ok {
		url = u
	}
	return map[string]any{
		"key": row.RepoKey, "type": row.Type, "packageType": row.PackageType,
		"description": row.Description, "url": url,
	}
}

// handleRepoGetV2 serves GET /api/v2/repositories/{key} (spec 2.1.3).
// Dialect faces versus the v1 read: type replaces rclass, package-specific
// fields are stripped, the Content-Type is the row class's vendor type,
// the unknown-key 404 carries the v2 wording, and NON-admin callers get
// the five-key partial view instead of a refusal. The negotiation quirk:
// the request's Content-Type — not Accept — selects the representation, so
// a mismatched vendor Content-Type answers 406 while any Accept passes.
func (s *Server) handleRepoGetV2(w http.ResponseWriter, r *http.Request, key string) {
	p := principalFrom(r.Context())
	row, err := s.deps.ReposSvc.GetRepo(r.Context(), p, key)
	if err != nil {
		if errors.Is(err, repo.ErrRepoNotFound) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("The repository %s was not found", key))
			return
		}
		s.writeRepoSvcError(w, err)
		return
	}
	if ct := requestMediaType(r.Header.Get("Content-Type")); ct != "" {
		if want, ok := v2ConfigVendorType[ct]; ok && want != row.Type {
			writeError(w, http.StatusNotAcceptable, http.StatusText(http.StatusNotAcceptable))
			return
		}
	}
	blob := repoRowBlob(row)
	w.Header().Set("Cache-Control", "no-store")
	if !s.canManage(r.Context(), p, auth.CapRepoWrite) {
		writeJSONBodyCT(w, http.StatusOK, v2ResponseCT(row.Type), v2PartialConfigMap(r, row, blob))
		return
	}
	writeJSONBodyCT(w, http.StatusOK, v2ResponseCT(row.Type), v2ConfigMap(row, blob))
}

// handleRepoBatchGet serves GET /api/v2/repositories/batch?names=a&names=b
// (spec 2.1.4): a map of repository key onto the v1 single-get schema
// (byte-for-byte the same projection GET /api/repositories/{key} serves,
// which is the invariant the spec pins). Unknown names are silently
// omitted; a comma inside one value is a single (usually nonexistent) key,
// not a list; no names answers the verbatim 400.
func (s *Server) handleRepoBatchGet(w http.ResponseWriter, r *http.Request) {
	var names []string
	for _, v := range r.URL.Query()["names"] {
		if v != "" {
			names = append(names, v)
		}
	}
	if len(names) == 0 {
		writeError(w, http.StatusBadRequest, "Repository keys are missing.")
		return
	}
	if len(names) > repoBatchItemsLimit {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"Repository item limit exceeded: %d. Limit: %d", len(names), repoBatchItemsLimit))
		return
	}
	p := principalFrom(r.Context())
	out := make(map[string]repoConfig, len(names))
	for _, name := range names {
		row, err := s.deps.ReposSvc.GetRepo(r.Context(), p, name)
		if err != nil {
			if errors.Is(err, repo.ErrRepoNotFound) {
				continue
			}
			s.writeRepoSvcError(w, err)
			return
		}
		out[name] = s.repoConfigOf(r, row)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSONBody(w, http.StatusOK, out)
}
