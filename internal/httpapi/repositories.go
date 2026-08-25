package httpapi

import (
	"context"
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
	Key                     string `json:"key"`
	RClass                  string `json:"rclass"`
	PackageType             string `json:"packageType"`
	Description             string `json:"description"`
	URL                     string `json:"url"`
	Notes                   string `json:"notes,omitempty"`
	IncludesPattern         string `json:"includesPattern,omitempty"`
	ExcludesPattern         string `json:"excludesPattern,omitempty"`
	RepoLayoutRef           string `json:"repoLayoutRef,omitempty"`
	BlackedOut              *bool  `json:"blackedOut,omitempty"`
	HandleReleases          *bool  `json:"handleReleases,omitempty"`
	HandleSnapshots         *bool  `json:"handleSnapshots,omitempty"`
	SnapshotVersionBehavior string `json:"snapshotVersionBehavior,omitempty"`
	MaxUniqueSnapshots      int    `json:"maxUniqueSnapshots,omitempty"`
	ChecksumPolicyType      string `json:"checksumPolicyType,omitempty"`
	ArchiveBrowsingEnabled  *bool  `json:"archiveBrowsingEnabled,omitempty"`

	// ---- M3 remote transport (FR-15; repo-semantics section 7.1 spellings) ----

	Username                       string `json:"username,omitempty"`
	Password                       string `json:"password,omitempty"` // transport only; repo.Service never persists it (NFR-S14)
	RetrievalCachePeriodSecs       *int64 `json:"retrievalCachePeriodSecs,omitempty"`
	MissedRetrievalCachePeriodSecs *int64 `json:"missedRetrievalCachePeriodSecs,omitempty"`
	SocketTimeoutSecs              *int64 `json:"socketTimeoutSecs,omitempty"`
	AssumedOfflinePeriodSecs       *int64 `json:"assumedOfflinePeriodSecs,omitempty"`
	HardFail                       *bool  `json:"hardFail,omitempty"`
	AllowPrivateUpstream           *bool  `json:"allowPrivateUpstream,omitempty"`
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
		setStr(m, "username", c.Username)
		setStr(m, "password", c.Password)
		setI64(m, "retrievalCachePeriodSecs", c.RetrievalCachePeriodSecs)
		setI64(m, "missedRetrievalCachePeriodSecs", c.MissedRetrievalCachePeriodSecs)
		setI64(m, "socketTimeoutSecs", c.SocketTimeoutSecs)
		setI64(m, "assumedOfflinePeriodSecs", c.AssumedOfflinePeriodSecs)
		setBool(m, "hardFail", c.HardFail)
		setBool(m, "allowPrivateUpstream", c.AllowPrivateUpstream)
		setBool(m, "priorityResolution", c.PriorityResolution)
	case repo.TypeVirtual:
		if c.Repositories != nil {
			m["repositories"] = c.Repositories
		}
		setStr(m, "defaultDeploymentRepo", c.DefaultDeploymentRepo)
		setStr(m, "defaultDeploymentRepoRef", c.DefaultDeploymentRepoRef)
		setStr(m, "deploymentRepository", c.DeploymentRepository)
	default:
		// local: the cross-cutting member mark plus the maven policy
		// family (T-67's consumers read them verbatim out of the config
		// blob — the T-64 passthrough contract; the transport addition is
		// the piece T-80 deferred to this ticket). The M4 governance fields
		// (T-95) ride the same passthrough: includesPattern/excludesPattern
		// (the struct's long-standing transport fields, finally forwarded)
		// and quotaBytes — repo.Service validates and the gates enforce.
		setBool(m, "priorityResolution", c.PriorityResolution)
		setBool(m, "handleReleases", c.HandleReleases)
		setBool(m, "handleSnapshots", c.HandleSnapshots)
		setStr(m, "snapshotVersionBehavior", c.SnapshotVersionBehavior)
		setStr(m, "checksumPolicyType", c.ChecksumPolicyType)
		setStr(m, "includesPattern", c.IncludesPattern)
		setStr(m, "excludesPattern", c.ExcludesPattern)
		setI64(m, "quotaBytes", c.QuotaBytes)
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
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render response: "+err.Error())
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(body)
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
func (s *Server) handleRepoList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
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
func (s *Server) repoListItemOf(r *http.Request, row *metadata.Repo) repoListItem {
	item := repoListItem{
		Key:         row.RepoKey,
		Description: row.Description,
		Type:        row.Type,
		PackageType: row.PackageType,
		URL:         requestBase(r) + "/" + row.RepoKey,
	}
	if row.Config != "" && row.Config != "{}" {
		var m map[string]any
		if err := json.Unmarshal([]byte(row.Config), &m); err == nil && len(m) > 0 {
			item.Configuration = m
		}
	}
	return item
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
func (s *Server) repoConfigOf(r *http.Request, row *metadata.Repo) repoConfig {
	cfg := repoConfig{
		Key:         row.RepoKey,
		RClass:      row.Type,
		PackageType: row.PackageType,
		Description: row.Description,
		URL:         requestBase(r) + "/" + row.RepoKey,
	}
	if row.Config != "" && row.Config != "{}" {
		var m map[string]any
		if err := json.Unmarshal([]byte(row.Config), &m); err == nil && len(m) > 0 {
			cfg.Configuration = m
		}
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

// handleRepoPut serves PUT /api/repositories/{key} (E-06/E-07): create, or
// update when the key exists — both answer 200 plain text (PRD v1.3
// calibration R2; the create-vs-update wording distinction is kept so
// scripts can log accurately). M3 (T-80) opens the remote/virtual classes:
// the type-relevant body fields ride through to repo.Service's typed config
// (E-07's M1 refusal is inverted per PRD section 5.6; the docker
// combinations stay refused — that rule is the service's).
//
// M7 (ADR-0026, inventory family 6): the route gate is the family-7
// repoManage write gate (the replace arm of an EXISTING repository); the
// CREATE arm — the key does not exist yet — splits here onto the global
// repo:write capability, which is deliberately NOT delegated to manage
// holders (FR-65: a repo admin cannot create or delete repositories).
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
	stored := &metadata.Repo{
		RepoKey: key, Type: body.RClass, PackageType: body.PackageType,
		Description: body.Description,
	}

	// Update path first: an existing key makes this an update regardless of
	// the body's completeness (PUT is the documented M1 update spelling too).
	current, getErr := s.deps.ReposSvc.GetRepo(r.Context(), p, key)
	switch {
	case getErr == nil:
		if stored.Type == "" {
			stored.Type = current.Type
		}
		if stored.PackageType == "" {
			stored.PackageType = current.PackageType
		}
		config, err := body.configJSON(stored.Type)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		stored.Config = config
		if _, err := s.deps.ReposSvc.UpdateRepo(r.Context(), p, stored); err != nil {
			s.writeRepoSvcError(w, err)
			return
		}
		writeText(w, http.StatusOK, fmt.Sprintf("Repository %s update successfully.\n", key))
		return
	case !errors.Is(getErr, repo.ErrRepoNotFound):
		s.writeRepoSvcError(w, getErr)
		return
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
// rest-api.md section 2): 404 when the key is unknown.
func (s *Server) handleRepoPost(w http.ResponseWriter, r *http.Request, key string) {
	var body repoConfig
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "request body is not valid repository configuration JSON: "+err.Error())
		return
	}
	p := principalFrom(r.Context())
	current, err := s.deps.ReposSvc.GetRepo(r.Context(), p, key)
	if err != nil {
		s.writeRepoSvcError(w, err)
		return
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
		Description: body.Description, Config: config,
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
