package httpapi

// L025-3B (D02 batch write family, rest-api.md sections 2.1.5-2.1.7): the
// v2 batch verbs — PUT batch-create (all-or-nothing with rollback, the
// byte-exact 201 text body), POST batch-modify (the single-repo update
// face's ADR-0050 merge dialect applied per item, bare-text 404/403 arms)
// and DELETE batch-delete (the 207 mixed-status state machine: ghost keys
// and concurrent deletes report success:true, all-success batches answer
// 200, true mixes answer 207, an all-failed batch answers the first
// failure's status).
//
// Spec-pending arms implemented literally and registered in the L025-3B
// report: the PUT/POST >100 limit wording (decompile-sourced medium
// confidence), the federated-first creation ordering (low confidence;
// BinFlow refuses the federated rclass, so the partition is structurally
// present and behaviorally a no-op), the DELETE failure-report field
// values (statusMsg / deleteArtifactsFailureCount / the all-fail
// statusMessage were never pinned), the remote repository's success
// wording and the blank-key abort reason.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// writeBareJSONText emits the batch-modify face's bare-string bodies (spec
// 2.1.6): an application/json content type wrapping a PLAIN string — no
// quotes, no errors envelope (the reference's own quirk, live-measured on
// both the 200 success and the 404 missing-keys arms).
func writeBareJSONText(w http.ResponseWriter, status int, body string) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body) //nolint:gosec // G705: plain text under application/json + nosniff; the message interpolates client-supplied keys
}

// addonPackageTypeMessage is checkAddonPackageType's non-writing form: the
// batch faces collect every refusal into one joined message instead of
// writing the first. "" means the package type is legal.
func (s *Server) addonPackageTypeMessage(packageType string) string {
	if s.deps.Addons == nil {
		return ""
	}
	if _, known := s.deps.Addons.ForPackageType(packageType); known {
		return ""
	}
	return unknownPackageTypeMessage(packageType, s.addonPackageTypes())
}

// handleRepoBatchPut serves PUT /api/v2/repositories/batch (spec 2.1.5):
// batch CREATE, whole-batch semantics — every name is validated before any
// repository is created (the live probe's all-or-nothing evidence), and a
// creation-time failure (config validation lives inside repo.Service)
// unwinds the created prefix before answering. Success is 201 text/plain
// with the byte-exact per-repository blocks: "Successfully created
// repository '<key>' \n\n" each (trailing space, blank line between and
// after — the two-repo live capture, joined per block).
//
// The route gate is required-only so the face's own NON-ADMIN 403 — the
// BARE "Forbidden" errors envelope (L025-5 / diff G4, live: the same
// wording the configurations read face answers) — is what reaches the
// wire, never the BinFlow standard manage-gate rendering.
func (s *Server) handleRepoBatchPut(w http.ResponseWriter, r *http.Request) {
	if !s.canManage(r.Context(), principalFrom(r.Context()), auth.CapRepoWrite) {
		writeError(w, http.StatusForbidden, "Forbidden")
		return
	}
	var items []repoConfig
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	if err := dec.Decode(&items); err != nil {
		writeError(w, http.StatusBadRequest,
			"request body is not valid repository configuration JSON: "+err.Error())
		return
	}
	if len(items) > repoBatchItemsLimit {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"Repository item limit exceeded: %d. Limit: %d", len(items), repoBatchItemsLimit))
		return
	}
	p := principalFrom(r.Context())

	// Validation pass: every problem is collected, then joined with "\n"
	// into one errors-envelope message (the reference's multi-error shape).
	var problems []string
	seen := make(map[string]bool, len(items))
	for i := range items {
		key := items[i].Key
		if key == "" {
			problems = append(problems, "Repository key are missing in configuration")
			continue
		}
		if seen[key] {
			// A within-batch duplicate is the same conflict the second
			// create would hit after the first landed — answered up front so
			// the batch stays all-or-nothing without a rollback round-trip.
			problems = append(problems, fmt.Sprintf(
				"error when validating repository name: %s : Repository key already exists", key))
			continue
		}
		seen[key] = true
		_, err := s.deps.ReposSvc.GetRepo(r.Context(), p, key)
		switch {
		case err == nil:
			problems = append(problems, fmt.Sprintf(
				"error when validating repository name: %s : Repository key already exists", key))
		case !errors.Is(err, repo.ErrRepoNotFound):
			s.writeRepoSvcError(w, err)
			return
		}
		if msg := s.addonPackageTypeMessage(items[i].PackageType); msg != "" {
			problems = append(problems, msg)
		}
	}
	if len(problems) > 0 {
		writeError(w, http.StatusBadRequest, strings.Join(problems, "\n"))
		return
	}

	// Creation pass, federated items first (spec 2.1.5's decompile-sourced
	// sortByPriorities ordering, LOW confidence, registered as awaiting a
	// differential arm; BinFlow refuses the federated rclass outright, so
	// the partition is behaviorally a no-op — kept literal per the ticket).
	order := make([]int, 0, len(items))
	for i := range items {
		if items[i].RClass == "federated" {
			order = append(order, i)
		}
	}
	for i := range items {
		if items[i].RClass != "federated" {
			order = append(order, i)
		}
	}
	var created []string
	for _, i := range order {
		stored := &metadata.Repo{
			RepoKey: items[i].Key, Type: items[i].RClass, PackageType: items[i].PackageType,
			Description: items[i].Description,
		}
		config, err := items[i].configJSON(stored.Type)
		if err != nil {
			s.log.ErrorContext(r.Context(), "httpapi: batch create config render failed",
				"repo", stored.RepoKey, "error", err.Error())
			writeError(w, http.StatusInternalServerError, "repository operation failed")
			return
		}
		stored.Config = config
		if _, err := s.deps.ReposSvc.CreateRepo(r.Context(), p, stored); err != nil {
			// All-or-nothing: unwind the created prefix (newest first), then
			// let the sentinel mapping render the refusal.
			for j := len(created) - 1; j >= 0; j-- {
				if derr := s.deps.ReposSvc.DeleteRepo(r.Context(), p, created[j], true); derr != nil {
					s.log.WarnContext(r.Context(), "httpapi: batch create rollback failed",
						"repo", created[j], "error", derr.Error())
				}
			}
			s.writeRepoSvcError(w, err)
			return
		}
		created = append(created, items[i].Key)
	}
	var body strings.Builder
	for _, key := range created {
		fmt.Fprintf(&body, "Successfully created repository '%s' \n\n", key)
	}
	writeText(w, http.StatusCreated, body.String())
}

// repoBatchUpdate is one POST-batch item resolved against its stored row.
type repoBatchUpdate struct {
	item repoConfig
	raw  json.RawMessage
	row  *metadata.Repo
}

// canManageRepoWrite asks the family-7 single-repo configuration gate — the
// same decision the v1 update route's repoManage write gate walks (admin
// passes, a manage holder passes inside their coverage). The batch-modify
// face aggregates it per key for its unauthorized-list 403.
func (s *Server) canManageRepoWrite(ctx context.Context, p *auth.Principal, repoKey string) bool {
	return managementAllowed(s.deps.Authz, func(m auth.ManagementAuthorizer) bool {
		return m.CanManageRepo(ctx, p, repoKey, true)
	})
}

// handleRepoBatchPost serves POST /api/v2/repositories/batch (spec 2.1.6):
// batch MODIFY over an array — resolve every key first (a missing key, or
// one filtered out by a vendor Content-Type naming another rclass, fails
// the whole batch with the bare-text 404), then aggregate the per-key
// update authorization into the bare-text 403, then apply each item with
// the single-repo update face's ADR-0050 merge semantics (omitted
// description keeps the stored value; the remote arm's config merges off
// the stored canonical baseline). Success is the bare string
// "Repositories updated successfully." under application/json.
//
// A mid-batch update failure adopts the official page's entire-batch-fails
// posture: the touched rows' pre-batch snapshots are restored (best
// effort) before the refusal is rendered.
func (s *Server) handleRepoBatchPost(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest,
			"request body is not valid repository configuration JSON: "+err.Error())
		return
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(raw, &raws); err != nil {
		writeError(w, http.StatusBadRequest,
			"request body is not valid repository configuration JSON: "+err.Error())
		return
	}
	if len(raws) > repoBatchItemsLimit {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"Repository item limit exceeded: %d. Limit: %d", len(raws), repoBatchItemsLimit))
		return
	}
	items := make([]repoConfig, len(raws))
	for i, rr := range raws {
		if err := json.Unmarshal(rr, &items[i]); err != nil {
			writeError(w, http.StatusBadRequest,
				"request body is not valid repository configuration JSON: "+err.Error())
			return
		}
	}
	for i := range items {
		if items[i].Key == "" {
			writeError(w, http.StatusBadRequest, "Repository key are missing in configuration")
			return
		}
	}
	p := principalFrom(r.Context())

	// The vendor Content-Type filter (spec 2.1.6, medium confidence): a
	// request typed as one rclass's vendor media resolves only repositories
	// of that class — the rest fall into the 404 missing-keys list.
	vendorRclass := ""
	if ct := requestMediaType(r.Header.Get("Content-Type")); ct != "" {
		if want, ok := v2ConfigVendorType[ct]; ok {
			vendorRclass = want
		}
	}
	updates := make([]repoBatchUpdate, 0, len(items))
	resolved := make(map[string]bool, len(items))
	var missing []string
	for i := range items {
		key := items[i].Key
		if resolved[key] {
			continue
		}
		resolved[key] = true
		row, gerr := s.deps.ReposSvc.GetRepo(r.Context(), p, key)
		switch {
		case gerr == nil && vendorRclass != "" && row.Type != vendorRclass:
			missing = append(missing, key)
		case gerr == nil:
			updates = append(updates, repoBatchUpdate{item: items[i], raw: raws[i], row: row})
		case errors.Is(gerr, repo.ErrRepoNotFound):
			missing = append(missing, key)
		default:
			s.writeRepoSvcError(w, gerr)
			return
		}
	}
	if len(missing) > 0 {
		writeBareJSONText(w, http.StatusNotFound,
			"No repositories found for the following keys: "+strings.Join(missing, ", "))
		return
	}
	var unauthorized []string
	for i := range updates {
		if !s.canManageRepoWrite(r.Context(), p, updates[i].row.RepoKey) {
			unauthorized = append(unauthorized, updates[i].row.RepoKey)
		}
	}
	if len(unauthorized) > 0 {
		writeBareJSONText(w, http.StatusForbidden,
			"User is not authorized to update the following repositories: "+strings.Join(unauthorized, ", "))
		return
	}
	touched := make([]int, 0, len(updates))
	for i := range updates {
		if err := s.batchUpdateOne(r, p, updates[i]); err != nil {
			// Entire batch fails: restore every touched snapshot plus the
			// failed item itself (its update may have partially applied
			// before the service refused).
			for _, j := range touched {
				s.restoreRepoRow(r.Context(), p, updates[j].row)
			}
			s.restoreRepoRow(r.Context(), p, updates[i].row)
			s.writeRepoSvcError(w, err)
			return
		}
		touched = append(touched, i)
	}
	writeBareJSONText(w, http.StatusOK, "Repositories updated successfully.")
}

// batchUpdateOne applies one POST-batch item with the single-repo update
// face's exact semantics (handleRepoPost's per-item twin): the description
// merges on raw key presence (ADR-0050 decision 5), rclass and package type
// default from the stored row, and the body's type-relevant fields render
// into the config blob the service parses ("" = keep-current-config).
func (s *Server) batchUpdateOne(r *http.Request, p *auth.Principal, u repoBatchUpdate) error {
	description := u.item.Description
	var presence map[string]json.RawMessage
	if json.Unmarshal(u.raw, &presence) == nil {
		if _, ok := presence["description"]; !ok {
			description = u.row.Description
		}
	}
	rclass := u.item.RClass
	if rclass == "" {
		rclass = u.row.Type
	}
	packageType := u.item.PackageType
	if packageType == "" {
		packageType = u.row.PackageType
	}
	config, err := u.item.configJSON(rclass)
	if err != nil {
		return err
	}
	_, err = s.deps.ReposSvc.UpdateRepo(r.Context(), p, &metadata.Repo{
		RepoKey: u.row.RepoKey, Type: rclass, PackageType: packageType,
		Description: description, Config: config,
	})
	return err
}

// restoreRepoRow is the POST batch's all-or-nothing unwind: it re-applies
// one resolved row's pre-batch snapshot (description + config). The
// snapshot's remote config is the masked read form — safe to re-parse and
// credential-neutral (the password seat merges on key presence inside the
// service, so a restore never touches the stored sealed credential,
// NFR-S14). Best effort: restore failures are logged, never masking the
// original batch error.
func (s *Server) restoreRepoRow(ctx context.Context, p *auth.Principal, snapshot *metadata.Repo) {
	if _, err := s.deps.ReposSvc.UpdateRepo(ctx, p, &metadata.Repo{
		RepoKey: snapshot.RepoKey, Type: snapshot.Type, PackageType: snapshot.PackageType,
		Description: snapshot.Description, Config: snapshot.Config,
	}); err != nil {
		s.log.WarnContext(ctx, "httpapi: batch update rollback failed",
			"repo", snapshot.RepoKey, "error", err.Error())
	}
}

// ---- the DELETE batch's 207 state machine (spec 2.1.7) ----

// repoDeleteTryLock/repoDeleteUnlock are the batch-delete plane's per-key
// in-flight registry: a delete already running for a key makes a second
// batch entry report the "deletion is already in progress" SUCCESS form
// instead of blocking or failing (the reference's delete-lock semantics —
// the single-delete 202 degraded to a success report inside a batch).
var (
	repoDeleteMu       sync.Mutex
	repoDeleteInFlight = make(map[string]bool)
)

func repoDeleteTryLock(key string) bool {
	repoDeleteMu.Lock()
	defer repoDeleteMu.Unlock()
	if repoDeleteInFlight[key] {
		return false
	}
	repoDeleteInFlight[key] = true
	return true
}

func repoDeleteUnlock(key string) {
	repoDeleteMu.Lock()
	defer repoDeleteMu.Unlock()
	delete(repoDeleteInFlight, key)
}

// repoBatchDeleteReport is one per-key report of the aggregate body. The
// success form carries exactly repoKey/statusMsg/deletedArtifactsCount/
// success; a real failure additionally carries deleteArtifactsFailureCount
// and errors[] (the pointer keeps a legitimate 0 on the wire while success
// entries omit the key).
type repoBatchDeleteReport struct {
	RepoKey                     string                    `json:"repoKey"`
	StatusMsg                   string                    `json:"statusMsg"`
	DeletedArtifactsCount       int                       `json:"deletedArtifactsCount"`
	Success                     bool                      `json:"success"`
	DeleteArtifactsFailureCount *int                      `json:"deleteArtifactsFailureCount,omitempty"`
	Errors                      []repoBatchDeleteErrEntry `json:"errors,omitempty"`
	// failStatus carries the mapped HTTP status of the first error for the
	// all-failed aggregation ("the first failure's status code"); it never
	// renders.
	failStatus int `json:"-"`
}

// repoBatchDeleteErrEntry is one errors[] entry of a failure report (the
// official single-delete FailureReport shape, capped at 50 examples there —
// BinFlow's whole-repository delete produces at most one).
type repoBatchDeleteErrEntry struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// repoBatchDeleteStatusOnly is the pre-validation abort body: a bare
// statusMessage object with NO reports key (spec 2.1.7 items 1-2).
type repoBatchDeleteStatusOnly struct {
	StatusMessage string `json:"statusMessage"`
}

// repoBatchDeleteBody is the aggregate body of the per-repository segment.
type repoBatchDeleteBody struct {
	Reports       []repoBatchDeleteReport `json:"reports"`
	StatusMessage string                  `json:"statusMessage"`
}

// repoBatchDeleteErrEntry maps one service error onto the errors[] entry,
// mirroring writeRepoSvcError's sentinel table (a *repo.StatusError carries
// its own exact status and message; unknown failures render the generic
// wording, never internals).
func repoBatchDeleteErrOf(err error) repoBatchDeleteErrEntry {
	var se *repo.StatusError
	if errors.As(err, &se) {
		return repoBatchDeleteErrEntry{Status: se.Code, Message: se.Message}
	}
	switch {
	case errors.Is(err, repo.ErrRepoNotFound):
		return repoBatchDeleteErrEntry{Status: http.StatusNotFound, Message: err.Error()}
	case errors.Is(err, repo.ErrRepoExists), errors.Is(err, repo.ErrRepoNotEmpty),
		errors.Is(err, repo.ErrInvalidRepoKey), errors.Is(err, repo.ErrReservedRepoKey),
		errors.Is(err, repo.ErrInvalidRepoConfig), errors.Is(err, repo.ErrInvalidRepoType),
		errors.Is(err, repo.ErrRepoTypeNotSupported), errors.Is(err, repo.ErrPackageTypeNotAvailable):
		return repoBatchDeleteErrEntry{Status: http.StatusBadRequest, Message: err.Error()}
	case errors.Is(err, repo.ErrUnauthorized):
		return repoBatchDeleteErrEntry{Status: http.StatusUnauthorized, Message: err.Error()}
	case errors.Is(err, repo.ErrForbidden):
		return repoBatchDeleteErrEntry{Status: http.StatusForbidden, Message: err.Error()}
	default:
		return repoBatchDeleteErrEntry{Status: http.StatusInternalServerError, Message: "repository operation failed"}
	}
}

func repoBatchDeleteSuccessReport(key, statusMsg string) repoBatchDeleteReport {
	return repoBatchDeleteReport{RepoKey: key, StatusMsg: statusMsg, Success: true}
}

func repoBatchDeleteFailureReport(key string, err error) repoBatchDeleteReport {
	zero := 0
	entry := repoBatchDeleteErrOf(err)
	return repoBatchDeleteReport{
		RepoKey: key, StatusMsg: err.Error(), Success: false,
		DeleteArtifactsFailureCount: &zero,
		Errors:                      []repoBatchDeleteErrEntry{entry},
		failStatus:                  entry.Status,
	}
}

// repoBatchDeleteAggregate is the state machine's aggregation rule (spec
// 2.1.7 item 4): every report success (ghost and in-progress included) →
// 200 "All repositories were removed successfully"; failures and successes
// together → 207 "Some repositories failed to be removed"; every entry a
// real failure → the FIRST failure's status code (the body keeps the
// reports form; its statusMessage wording was never pinned and reuses the
// 207 aggregate — registered in the L025-3B report).
func repoBatchDeleteAggregate(reports []repoBatchDeleteReport) (int, string) {
	firstFail := -1
	for i := range reports {
		if !reports[i].Success {
			firstFail = i
			break
		}
	}
	if firstFail < 0 {
		return http.StatusOK, "All repositories were removed successfully"
	}
	for i := range reports {
		if reports[i].Success {
			return http.StatusMultiStatus, "Some repositories failed to be removed"
		}
	}
	return reports[firstFail].failStatus, "Some repositories failed to be removed"
}

// handleRepoBatchDelete serves DELETE /api/v2/repositories/batch (spec
// 2.1.7): body = a JSON string array (duplicates collapse, order kept).
// The pre-validation segment aborts the WHOLE batch with a single-report
// body for its refusal arms (permission; the trash system key); every
// OTHER spelling — ghost keys, blank keys, keys whose deletion is already
// in progress — rides the per-repository segment with success:true, and
// the aggregate lands on 200 / 207 / the first failure's status.
func (s *Server) handleRepoBatchDelete(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	var keys []string
	if err != nil || json.Unmarshal(body, &keys) != nil || len(keys) == 0 {
		writeJSONBody(w, http.StatusBadRequest, repoBatchDeleteStatusOnly{
			StatusMessage: "No repository keys were provided for deletion"})
		return
	}
	uniq := make([]string, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if !seen[key] {
			seen[key] = true
			uniq = append(uniq, key)
		}
	}
	keys = uniq
	p := principalFrom(r.Context())

	// Pre-validation segment (whole-batch abort arms). The permission arm's
	// wording is live-probed verbatim; the route stays at required-only so
	// THIS form — not the BinFlow standard 403 envelope — reaches the wire.
	if !s.canManage(r.Context(), p, auth.CapRepoWrite) {
		first := keys[0]
		writeJSONBody(w, http.StatusForbidden, repoBatchDeleteStatusOnly{StatusMessage: fmt.Sprintf(
			"Cannot delete repository: '%s', Reason: User: ('%s') has insufficient permission to delete repositories: %s",
			first, p.Name, first)})
		return
	}
	for _, key := range keys {
		// L025-5 / diff G2: a BLANK key rides the per-repository ghost arm
		// (spec §2.1.7-2's whole-batch abort is voided by the live probe:
		// ["k",""] answers 200 with the '' row's
		// "repository config does not exist" success report) — no
		// pre-validation refusal here.
		if key == repo.TrashRepoKey {
			// Abort-class system key. The refusal text is harvested by
			// invoking the service, which refuses at its FIRST guard —
			// before any mutation — so the call is side-effect-free by
			// construction and the wording can never drift from the
			// service's own.
			gerr := s.deps.ReposSvc.DeleteRepo(r.Context(), p, key, true)
			entry := repoBatchDeleteErrOf(gerr)
			writeJSONBody(w, entry.Status, repoBatchDeleteStatusOnly{StatusMessage: entry.Message})
			return
		}
	}

	reports := make([]repoBatchDeleteReport, 0, len(keys))
	for _, key := range keys {
		if !repoDeleteTryLock(key) {
			reports = append(reports, repoBatchDeleteSuccessReport(key, fmt.Sprintf(
				"Cannot delete repository: '%s', repository deletion is already in progress", key)))
			continue
		}
		rep := s.batchDeleteOne(r.Context(), p, key)
		repoDeleteUnlock(key)
		reports = append(reports, rep)
	}
	status, message := repoBatchDeleteAggregate(reports)
	writeJSONBody(w, status, repoBatchDeleteBody{Reports: reports, StatusMessage: message})
}

// batchDeleteOne deletes one key for the batch segment and renders its
// report: the ghost arm (missing key → success:true with the
// config-does-not-exist statusMsg), the real delete (content always goes —
// the v2 flow's "and all its content" semantics; deletedArtifactsCount is
// the file-node count), or the failure report carrying the mapped
// errors[] entry.
func (s *Server) batchDeleteOne(ctx context.Context, p *auth.Principal, key string) repoBatchDeleteReport {
	row, err := s.deps.ReposSvc.GetRepo(ctx, p, key)
	if err != nil {
		if errors.Is(err, repo.ErrRepoNotFound) {
			return repoBatchDeleteSuccessReport(key, fmt.Sprintf(
				"Cannot delete repository: '%s', repository config does not exist", key))
		}
		return repoBatchDeleteFailureReport(key, err)
	}
	count := s.countRepoArtifacts(ctx, key)
	if err := s.deps.ReposSvc.DeleteRepo(ctx, p, key, true); err != nil {
		return repoBatchDeleteFailureReport(key, err)
	}
	// The success wording is pinned for local/federated ("and all its
	// content") and virtual; remote rides the content-bearing form too —
	// live-verified against the reference (L025-3B probe, 2026-09-17: a
	// remote repo deleted in a mixed batch answered exactly the local
	// wording).
	statusMsg := fmt.Sprintf("Repository '%s' and all its content have been removed successfully.", key)
	if row.Type == repo.TypeVirtual {
		statusMsg = fmt.Sprintf("Repository '%s' has been removed successfully.", key)
	}
	rep := repoBatchDeleteSuccessReport(key, statusMsg)
	rep.DeletedArtifactsCount = count
	return rep
}

// countRepoArtifacts counts the file nodes under one repository (folder
// sentinel rows excluded) — the deletedArtifactsCount the v2 report
// carries. A counting failure logs and answers 0: the report must never
// fail the delete itself.
func (s *Server) countRepoArtifacts(ctx context.Context, repoKey string) int {
	nodes, err := s.deps.Metadata.Nodes().ListByPrefix(ctx, repoKey, "")
	if err != nil {
		s.log.WarnContext(ctx, "httpapi: batch delete artifact count failed",
			"repo", repoKey, "error", err.Error())
		return 0
	}
	n := 0
	for _, node := range nodes {
		if node.Sha256 != metadata.FolderMarkerSHA {
			n++
		}
	}
	return n
}
