package httpapi

// Push-replication management REST (T-180, ADR-0021): the /binflow/api/v1
// replications CRUD and the aggregated status face the console panel (T-159)
// polls. The domain lives in internal/replication (configs + the task ledger
// the push engine drains); this file adds only the REST surface:
//
//	GET    /binflow/api/v1/replications          list configs (no secrets)
//	POST   /binflow/api/v1/replications          create a config
//	PUT    /binflow/api/v1/replications/{id}     flip one config's enabled bit
//	DELETE /binflow/api/v1/replications/{name}   drop a config (tasks cascade)
//	GET    /binflow/api/v1/replication/status    panel payload (T-159 shape)
//
// Errors render the errors[] envelope like the sibling governance endpoints
// (/api/v1/storage/migration, /api/v1/system/gc). The status body follows the
// T-159 contract assumptions verbatim (reports/agents/T-159.md, the frontend
// types in web/src/pages/governance/ReplicationPage.tsx): snake_case, int64
// counts, RFC3339 *_at text with '' for "never"/"not finished", targets and
// events always arrays (never null), credentials never serialized, and
// ConfigStatus.ReplicationID not duplicated (it equals the config id).

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/replication"
)

// Audit actions for the configuration plane. String literals, not
// audit-package constants: like the engine's replication.push pair, the M6
// vocabulary has not landed in internal/audit (out of T-180's area); the
// report flags the follow-up.
const (
	auditActionReplicationConfigCreate = "replication.config.create"
	auditActionReplicationConfigUpdate = "replication.config.update"
	auditActionReplicationConfigDelete = "replication.config.delete"
)

// Status-event window (T-159 ruling 5): the panel asks for the most recent
// events merged across configs; 50 is the server default the frontend's
// no-parameter poll relies on, 500 the hard cap so one request cannot ask
// the ledger for an unbounded scan.
const (
	defaultReplicationEventLimit = 50
	maxReplicationEventLimit     = 500
)

// CredentialEncryptor seals a target password into the ADR-0012 enc:v1
// at-rest form at config-create time. *remote.Cipher satisfies it
// structurally; cmd wires the same master-key cipher the replication engine
// decrypts with (BINFLOW_REMOTE_CREDENTIALS_KEY, shared with remote
// repository credentials).
type CredentialEncryptor interface {
	Encrypt(secret string) (string, error)
}

// replicationConfigBody is the create wire shape. target_password is
// WRITE-ONLY: it arrives plaintext, is sealed through Deps.ReplicationCipher
// before the row is stored, and never echoes — every response renders
// replicationConfigResponse, which has no password field at all. An empty
// password (anonymous target) skips the cipher entirely.
type replicationConfigBody struct {
	Name                    string `json:"name"`
	SourceRepo              string `json:"source_repo"`
	TargetURL               string `json:"target_url"`
	TargetRepo              string `json:"target_repo"`
	TargetUsername          string `json:"target_username"`
	TargetPassword          string `json:"target_password"`
	MaxBandwidthBytesPerSec int64  `json:"max_bandwidth_bytes_per_sec"`
	MaxItemsPerPush         int64  `json:"max_items_per_push"`
	// Enabled is a pointer so ABSENT (defaults true, the push engine's
	// useful posture — a config created disabled would silently swallow
	// every enqueue until flipped) stays distinct from an explicit false.
	Enabled *bool `json:"enabled"`
}

// replicationConfigResponse is the read shape: the full config row minus the
// credential pair. The username stays — it identifies the account on the
// TARGET instance, is useless without the password, and the admin-facing
// CRUD plane has always shown it (the T-159 ruling hides credentials on the
// STATUS face only).
type replicationConfigResponse struct {
	ID                      int64  `json:"id"`
	Name                    string `json:"name"`
	SourceRepo              string `json:"source_repo"`
	TargetURL               string `json:"target_url"`
	TargetRepo              string `json:"target_repo"`
	TargetUsername          string `json:"target_username"`
	MaxBandwidthBytesPerSec int64  `json:"max_bandwidth_bytes_per_sec"`
	MaxItemsPerPush         int64  `json:"max_items_per_push"`
	Enabled                 bool   `json:"enabled"`
	CreatedAt               string `json:"created_at"`
	UpdatedAt               string `json:"updated_at"`
}

// replicationConfigResponseOf renders one config row in the read shape every
// config-carrying response shares — the GET projection minus the credential
// pair (create, update and the list all answer with exactly these fields,
// so a client cannot tell which verb produced a row).
func replicationConfigResponseOf(c *replication.ReplicationConfig) replicationConfigResponse {
	return replicationConfigResponse{
		ID: c.ID, Name: c.Name, SourceRepo: c.SourceRepo,
		TargetURL: c.TargetURL, TargetRepo: c.TargetRepo,
		TargetUsername:          c.TargetUsername,
		MaxBandwidthBytesPerSec: c.MaxBandwidthBytesPerSec,
		MaxItemsPerPush:         c.MaxItemsPerPush,
		Enabled:                 c.Enabled,
		CreatedAt:               c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

// replicationTargetStatus is one row of the status payload's targets array:
// the config row (credential fields dropped, T-159 ruling 1) flattened with
// the derived ConfigStatus counts (ruling 2: the id IS
// ConfigStatus.ReplicationID, not repeated).
type replicationTargetStatus struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	SourceRepo    string `json:"source_repo"`
	TargetURL     string `json:"target_url"`
	TargetRepo    string `json:"target_repo"`
	Enabled       bool   `json:"enabled"`
	Pending       int64  `json:"pending"`
	InProgress    int64  `json:"in_progress"`
	Succeeded     int64  `json:"succeeded"`
	Failed        int64  `json:"failed"`
	Skipped       int64  `json:"skipped"`
	LastSuccessAt string `json:"last_success_at"`
	UpdatedAt     string `json:"updated_at"`
}

// replicationEvent is one row of the status payload's events array: a
// ReplicationTask verbatim (ruling 3/4: status is the 009 closed set,
// ” means "not terminal yet").
type replicationEvent struct {
	ID            int64  `json:"id"`
	ReplicationID int64  `json:"replication_id"`
	BlobSHA256    string `json:"blob_sha256"`
	NodePath      string `json:"node_path"`
	Status        string `json:"status"`
	Attempts      int64  `json:"attempts"`
	LastError     string `json:"last_error"`
	CreatedAt     string `json:"created_at"`
	CompletedAt   string `json:"completed_at"`
}

// replicationStatusResponse is the GET /api/v1/replication/status body. Both
// arrays are initialized empty (never nil) so the JSON carries [] — the
// panel's "configured face, no targets" empty state (T-159 ruling 6).
type replicationStatusResponse struct {
	Targets []replicationTargetStatus `json:"targets"`
	Events  []replicationEvent        `json:"events"`
}

// validReplicationName reports whether name is a legal config name: 1..64
// runes of [A-Za-z0-9._-] starting alphanumeric. The closed charset keeps a
// name addressable as ONE URL segment on the DELETE route (no '/', no '%',
// no dot-segment spellings) and blocks lookalike separators.
func validReplicationName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// validTargetURL mirrors the engine's target-url gate (engine.targetURL) at
// create time: an absolute http or https URL with a host. Failing the
// malformed config at REST time (400) beats failing every one of its tasks
// as not-retryable after the fact.
func validTargetURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// validateReplicationBody checks the create body's invariants and returns
// the 400 message, or "" when the body is acceptable. Numeric caps are
// non-negative (0 means "default"/"unlimited"); the bandwidth field has no
// upper bound because the engine merely throttles with it.
func validateReplicationBody(b *replicationConfigBody) string {
	switch {
	case !validReplicationName(b.Name):
		return "name must be 1..64 characters of letters, digits, '.', '_' or '-', starting alphanumeric"
	case b.SourceRepo == "":
		return "source_repo is required"
	case b.TargetRepo == "":
		return "target_repo is required"
	case !validTargetURL(b.TargetURL):
		return "target_url must be an absolute http or https URL with a host"
	case b.MaxBandwidthBytesPerSec < 0:
		return "max_bandwidth_bytes_per_sec must not be negative"
	case b.MaxItemsPerPush < 0:
		return "max_items_per_push must not be negative"
	}
	return ""
}

// replicationNotConfigured answers the seam's absence uniformly: 501, the
// panel's "replication not enabled on this instance" degradation (T-159
// ruling 6 — distinct from the 404 an unbridged route would answer).
func replicationNotConfigured(w http.ResponseWriter) {
	writeError(w, http.StatusNotImplemented, "replication is not configured on this instance")
}

// handleReplicationList serves GET /binflow/api/v1/replications: every
// config row, credential fields excluded, id-ordered (the store's order).
func (s *Server) handleReplicationList(w http.ResponseWriter, r *http.Request) {
	if s.deps.Replication == nil {
		replicationNotConfigured(w)
		return
	}
	cfgs, err := s.deps.Replication.ListConfigs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list replications: "+err.Error())
		return
	}
	out := make([]replicationConfigResponse, 0, len(cfgs))
	for _, c := range cfgs {
		out = append(out, replicationConfigResponseOf(c))
	}
	writeJSONBody(w, http.StatusOK, out)
}

// handleReplicationCreate serves POST /binflow/api/v1/replications. The
// handler re-checks the admin gate (the route already demanded it) because
// it writes the actor into the audit trail.
func (s *Server) handleReplicationCreate(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "replication configuration requires an administrator account")
		return
	}
	if s.deps.Replication == nil {
		replicationNotConfigured(w)
		return
	}
	var body replicationConfigBody
	if err := decodeJSONBodyOf(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.SourceRepo = strings.TrimSpace(body.SourceRepo)
	body.TargetURL = strings.TrimSpace(body.TargetURL)
	body.TargetRepo = strings.TrimSpace(body.TargetRepo)
	if msg := validateReplicationBody(&body); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	// The source repository must exist: the 009 FK would reject the insert,
	// but the pre-check names the offending key (the permissions plane's
	// posture for the same reference shape).
	if _, err := s.deps.Repos.Get(r.Context(), body.SourceRepo); err != nil {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("replication config references an unknown repository %q", body.SourceRepo))
		return
	}

	// Seal the target password before anything is stored (ADR-0012: never
	// store plaintext). No cipher configured + a password offered refuses
	// the create instead of silently storing an unusable secret.
	passwordEnc := ""
	if body.TargetPassword != "" {
		if s.deps.ReplicationCipher == nil {
			writeError(w, http.StatusBadRequest,
				"target_password requires the credential master key (set BINFLOW_REMOTE_CREDENTIALS_KEY and restart)")
			return
		}
		sealed, err := s.deps.ReplicationCipher.Encrypt(body.TargetPassword)
		if err != nil {
			writeError(w, http.StatusBadRequest, "sealing target password: "+err.Error())
			return
		}
		passwordEnc = sealed
	}

	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	maxItems := body.MaxItemsPerPush
	if maxItems == 0 {
		maxItems = 1000 // the 009 DDL default; the store's INSERT always names the column
	}
	now := metadata.Now()
	id, err := s.deps.Replication.CreateConfig(r.Context(), &replication.ReplicationConfig{
		Name: body.Name, SourceRepo: body.SourceRepo,
		TargetURL: body.TargetURL, TargetRepo: body.TargetRepo,
		TargetUsername: body.TargetUsername, TargetPasswordEnc: passwordEnc,
		MaxBandwidthBytesPerSec: body.MaxBandwidthBytesPerSec,
		MaxItemsPerPush:         maxItems,
		Enabled:                 enabled,
		CreatedAt:               now, UpdatedAt: now,
	})
	if err != nil {
		switch {
		case errors.Is(err, replication.ErrDuplicateName):
			writeError(w, http.StatusConflict,
				fmt.Sprintf("replication config name %q is already taken", body.Name))
		case metadata.IsStoreBusy(err):
			writeError(w, http.StatusServiceUnavailable,
				"replication store is busy, retry shortly: "+err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "create replication config: "+err.Error())
		}
		return
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  p.Name,
		Action: auditActionReplicationConfigCreate,
		Repo:   body.SourceRepo,
		Detail: auditDetail("name", body.Name, "target", body.TargetURL, "target_repo", body.TargetRepo),
	})
	created, err := s.deps.Replication.GetConfig(r.Context(), id)
	if err != nil {
		// The row exists (CreateConfig returned its id); a read failure here
		// is a store fault worth surfacing, with the id named for the retry.
		writeError(w, http.StatusInternalServerError,
			fmt.Sprintf("replication config %d created but unreadable: %v", id, err))
		return
	}
	writeJSONBody(w, http.StatusCreated, replicationConfigResponseOf(created))
}

// replicationUpdateBody is the PUT /{id} wire shape (T-405). The update face
// is deliberately the enable/disable bit ALONE: the mini-PUT ruling scopes
// full-row editing to a later ticket (the wider Artifactory field family —
// cron, path prefixes, the sync flags — is a medium-confidence reverse-spec
// area, replication.md 2.1/2.2, and no BinFlow model carries it yet). The
// decode still reuses the whole create shape so a client round-tripping a
// row it read from the list (the natural console form) is accepted instead
// of refused: every field besides enabled is parsed and IGNORED, and the
// response echoes the stored row, so the caller sees exactly what applied.
type replicationUpdateBody struct {
	replicationConfigBody
}

// handleReplicationUpdate serves PUT /binflow/api/v1/replications/{id} (the
// console's start/stop switch, T-405). The flip is a STORE bit, not an
// engine signal: the push engine re-reads the config rows on every event
// (Enqueue) and every drain pass (wake + the 1m sweep), so disabling stops
// new task rows immediately and stops new task claims within one pass — an
// attempt already in flight runs to its own conclusion — and enabling
// resumes both, no restart anywhere. The id is the numeric row id the list
// projection carries (the immutable key; the name stays renamable).
func (s *Server) handleReplicationUpdate(w http.ResponseWriter, r *http.Request, idRaw string) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "replication configuration requires an administrator account")
		return
	}
	if s.deps.Replication == nil {
		replicationNotConfigured(w)
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(idRaw), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return
	}
	var body replicationUpdateBody
	if err := decodeJSONBodyOf(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required (true or false); no other field is editable on this face yet")
		return
	}
	cfg, err := s.deps.Replication.GetConfig(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, replication.ErrConfigNotFound):
			writeError(w, http.StatusNotFound, "replication config not found: "+idRaw)
		case metadata.IsStoreBusy(err):
			writeError(w, http.StatusServiceUnavailable,
				"replication store is busy, retry shortly: "+err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "get replication config: "+err.Error())
		}
		return
	}
	// Read-modify-write of the full row (the store has no single-column
	// flip): safe today because this face is the only config-column writer,
	// and it is what preserves the sealed credential and every throttle cap
	// through the flip.
	cfg.Enabled = *body.Enabled
	cfg.UpdatedAt = metadata.Now()
	if err := s.deps.Replication.UpdateConfig(r.Context(), cfg); err != nil {
		switch {
		case errors.Is(err, replication.ErrConfigNotFound):
			writeError(w, http.StatusNotFound, "replication config not found: "+idRaw)
		case errors.Is(err, replication.ErrDuplicateName):
			// Unreachable through this face (the name is never rewritten
			// here); mapped anyway so a racing future writer cannot leak a
			// 500 where the family answers 409.
			writeError(w, http.StatusConflict,
				fmt.Sprintf("replication config name %q is already taken", cfg.Name))
		case metadata.IsStoreBusy(err):
			writeError(w, http.StatusServiceUnavailable,
				"replication store is busy, retry shortly: "+err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "update replication config: "+err.Error())
		}
		return
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  p.Name,
		Action: auditActionReplicationConfigUpdate,
		Repo:   cfg.SourceRepo,
		Detail: auditDetail("name", cfg.Name, "enabled", fmt.Sprint(cfg.Enabled)),
	})
	updated, err := s.deps.Replication.GetConfig(r.Context(), id)
	if err != nil {
		// Same posture as the create's echo read: the flip already landed,
		// surface the store fault with the id named for the retry.
		writeError(w, http.StatusInternalServerError,
			fmt.Sprintf("replication config %d updated but unreadable: %v", id, err))
		return
	}
	writeJSONBody(w, http.StatusOK, replicationConfigResponseOf(updated))
}

// handleReplicationDelete serves DELETE /binflow/api/v1/replications/{name}:
// the config row goes and its task rows cascade with it (the 009 FK). The
// store keys deletes by id, so the name resolves through the listing — a
// config set is operator-scale, and the alternative (a GetConfigByName
// store method) is not worth the surface for one route.
func (s *Server) handleReplicationDelete(w http.ResponseWriter, r *http.Request, name string) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "replication configuration requires an administrator account")
		return
	}
	if s.deps.Replication == nil {
		replicationNotConfigured(w)
		return
	}
	cfgs, err := s.deps.Replication.ListConfigs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list replications: "+err.Error())
		return
	}
	var id int64 = -1
	for _, c := range cfgs {
		if c.Name == name {
			id = c.ID
			break
		}
	}
	if id < 0 {
		writeError(w, http.StatusNotFound, "replication config not found: "+name)
		return
	}
	if err := s.deps.Replication.DeleteConfig(r.Context(), id); err != nil {
		if metadata.IsStoreBusy(err) {
			writeError(w, http.StatusServiceUnavailable,
				"replication store is busy, retry shortly: "+err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError,
			fmt.Sprintf("delete replication config %s: %v", name, err))
		return
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  p.Name,
		Action: auditActionReplicationConfigDelete,
		Repo:   "", // the source repo may be gone; the detail names the config
		Detail: auditDetail("name", name),
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleReplicationStatus serves GET /binflow/api/v1/replication/status: the
// T-159 panel payload. Every config renders one target row flattened with
// its derived ConfigStatus; events are the newest tasks merged across ALL
// configs (the ledger has no cross-config listing — per-config windows are
// merged and re-sorted here), bounded by ?limit= (default 50, max 500).
func (s *Server) handleReplicationStatus(w http.ResponseWriter, r *http.Request) {
	if s.deps.Replication == nil {
		replicationNotConfigured(w)
		return
	}
	limit := defaultReplicationEventLimit
	if raw, ok := r.URL.Query()["limit"]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(raw[0]))
		if err != nil || n < 1 || n > maxReplicationEventLimit {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"limit must be an integer between 1 and %d", maxReplicationEventLimit))
			return
		}
		limit = n
	}

	cfgs, err := s.deps.Replication.ListConfigs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list replications: "+err.Error())
		return
	}
	resp := replicationStatusResponse{
		Targets: make([]replicationTargetStatus, 0, len(cfgs)),
		Events:  make([]replicationEvent, 0, limit),
	}
	for _, c := range cfgs {
		st, err := s.deps.Replication.Status(r.Context(), c.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError,
				fmt.Sprintf("replication status %s: %v", c.Name, err))
			return
		}
		resp.Targets = append(resp.Targets, replicationTargetStatus{
			ID: c.ID, Name: c.Name, SourceRepo: c.SourceRepo,
			TargetURL: c.TargetURL, TargetRepo: c.TargetRepo,
			Enabled: c.Enabled,
			Pending: st.Pending, InProgress: st.InProgress,
			Succeeded: st.Succeeded, Failed: st.Failed, Skipped: st.Skipped,
			LastSuccessAt: st.LastSuccessAt, UpdatedAt: c.UpdatedAt,
		})
		tasks, err := s.deps.Replication.ListTasks(r.Context(), c.ID, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError,
				fmt.Sprintf("replication events %s: %v", c.Name, err))
			return
		}
		for _, t := range tasks {
			resp.Events = append(resp.Events, replicationEvent{
				ID: t.ID, ReplicationID: t.ReplicationID,
				BlobSHA256: t.BlobSHA256, NodePath: t.NodePath,
				Status: t.Status, Attempts: t.Attempts, LastError: t.LastError,
				CreatedAt: t.CreatedAt, CompletedAt: t.CompletedAt,
			})
		}
	}
	// The per-config windows are newest-first already; the merge re-sorts
	// globally by (created_at desc, id desc) — ids share one autoincrement
	// keyspace, so the tiebreak stays chronological across configs.
	sort.Slice(resp.Events, func(i, j int) bool {
		if resp.Events[i].CreatedAt != resp.Events[j].CreatedAt {
			return resp.Events[i].CreatedAt > resp.Events[j].CreatedAt
		}
		return resp.Events[i].ID > resp.Events[j].ID
	})
	if len(resp.Events) > limit {
		resp.Events = resp.Events[:limit]
	}
	writeJSONBody(w, http.StatusOK, resp)
}
