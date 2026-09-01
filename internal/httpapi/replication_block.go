package httpapi

// The replication global-block and Test-connection REST faces (M15 T-422,
// FR-138.2/FR-138.3; replication.md §9.1-B/§9.2-B/§9.2-C/§9.3 — the
// blocksystemreplication and testlocalreplication counterparts).
//
//	GET  /binflow/api/v1/system/replications             CapSystemRead
//	POST /binflow/api/v1/system/replications/block       CapSystemWrite
//	POST /binflow/api/v1/system/replications/unblock     CapSystemWrite
//	POST /binflow/api/v1/replications/{id}/test          CapSystemWrite
//	POST /binflow/api/v1/replications/test               CapSystemWrite
//
// The block face follows the OFFICIAL REST wire (§9.6's landing ruling:
// A-layer alignment — the three-endpoint form, not the global-config
// simplification): the GET body carries the official camelCase pair
// {blockPullReplications, blockPushReplications} verbatim (§9.1-B, triple
// sourced), the block/unblock verbs take the push/pull query params
// (default "true"; any non-"true" spelling means "leave that direction
// alone this call", §9.2-B-1) and answer text/plain with the message
// variants of §9.2-B-3, "No action taken." on a double no-op included.
// The face is NOT under the replication license gate (BinFlow has none)
// and NOT under the block itself: configuration traffic keeps flowing
// while the brake is on (§9.2-B-6/B-7 posture, t226's UI-API-not-gated
// finding).
//
// The test faces are BinFlow's own C-layer (§9.3's ruling — the official
// reference carries no test endpoint): {id}/test probes a stored config
// (optionally overridden field-by-field by the body, §9.3's draft arm) and
// the id-less /test probes an UNSAVED draft (§9.2-C-9's unsaved-draft
// posture — the create form can test before anything is stored). Both
// render the verdict as the object body — ok at 200, ok:false with the
// inline reason at 400 (the auth.config.test posture, the word's
// namesake); errors[] stays for face faults (404 unknown id, 400 malformed
// body, 5xx unseal).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/replication"
)

// Audit actions of the two faces. String literals per the family's
// precedent (emit sites pin their own words; the §9.4 batch registered the
// vocabulary in internal/audit — T-422's leg).
const (
	// auditActionReplicationBlockUpdate records one global blockPush/
	// blockPull flip (§9.4 #4: the single .update-family word over the
	// block/unblock verb pair).
	auditActionReplicationBlockUpdate = "replication.block.update"
	// auditActionReplicationConfigTest records one connection probe (§9.4
	// #2; auth.config.test is the naming precedent).
	auditActionReplicationConfigTest = "replication.config.test"
)

// replicationTestMaxBodyBytes caps the test bodies (the draft face carries
// a URL, a repo key and a credential pair — a few KiB; 64 KiB is generous).
const replicationTestMaxBodyBytes = 1 << 16

// ReplicationTester is the probe seam (consumer-side, satisfied by
// *replication.Engine — the SAME instance the worker loop drains, so the
// probe rides the engine's guarded client, socket timeout and SSRF
// posture). Nil keeps both test faces at the family's 501 while every
// store-backed face stays up.
type ReplicationTester interface {
	TestTarget(ctx context.Context, cfg *replication.ReplicationConfig, ov replication.TargetOverride) (replication.TestResult, error)
}

// replicationGlobalBlockResponse is the GET body — the official camelCase
// pair verbatim (§9.1-B: {"blockPullReplications":bool,
// "blockPushReplications":bool}; pull first, matching the documented
// example's field order).
type replicationGlobalBlockResponse struct {
	BlockPullReplications bool `json:"blockPullReplications"`
	BlockPushReplications bool `json:"blockPushReplications"`
}

// handleReplicationGlobalBlockGet serves GET /api/v1/system/replications:
// the current brake state. Read gate CapSystemRead at the route — the
// UI-API face's relaxed GET (any-project-admin, §9.2-B-6) maps onto
// BinFlow's readonly_admin-inclusive read capability.
func (s *Server) handleReplicationGlobalBlockGet(w http.ResponseWriter, _ *http.Request) {
	if s.deps.ReplicationBlocks == nil {
		replicationNotConfigured(w)
		return
	}
	f := s.deps.ReplicationBlocks.Flags()
	writeJSONBody(w, http.StatusOK, replicationGlobalBlockResponse{
		BlockPullReplications: f.BlockPull,
		BlockPushReplications: f.BlockPush,
	})
}

// blockDirectionArgs resolves the push/pull query params (§9.2-B-1): ABSENT
// means the direction IS addressed (default "true"); a present value means
// "true" addresses it and ANY other spelling leaves it alone this call.
func blockDirectionArgs(q url.Values) (actPush, actPull bool) {
	actPush = q.Get("push") == "" || q.Get("push") == "true"
	actPull = q.Get("pull") == "" || q.Get("pull") == "true"
	return actPush, actPull
}

// handleReplicationGlobalBlockSet serves the two POST verbs (block=true
// sets, unblock clears). The handler re-checks the admin gate because it
// writes the actor into the audit trail. Idempotent (§9.2-B-5): the same
// values re-write the same row and answer the same message.
func (s *Server) handleReplicationGlobalBlockSet(blocking bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := principalFrom(r.Context())
		if p == nil || !p.Admin {
			writeError(w, http.StatusForbidden, "replication configuration requires an administrator account")
			return
		}
		if s.deps.ReplicationBlocks == nil {
			replicationNotConfigured(w)
			return
		}
		actPush, actPull := blockDirectionArgs(r.URL.Query())
		if !actPush && !actPull {
			// §9.2-B-3's fourth variant: still 200, state untouched, no
			// audit row (nothing happened).
			writePlainText(w, http.StatusOK, "No action taken.")
			return
		}
		cur := s.deps.ReplicationBlocks.Flags()
		nextPush, nextPull := cur.BlockPush, cur.BlockPull
		if actPush {
			nextPush = blocking
		}
		if actPull {
			nextPull = blocking
		}
		next, err := s.deps.ReplicationBlocks.Update(r.Context(), nextPush, nextPull, p.Name)
		if err != nil {
			if metadata.IsStoreBusy(err) {
				writeError(w, http.StatusServiceUnavailable,
					"replication store is busy, retry shortly: "+err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "update replication block state: "+err.Error())
			return
		}
		s.audit.Record(r.Context(), audit.Event{
			Actor:  p.Name,
			Action: auditActionReplicationBlockUpdate,
			Detail: auditDetail("blockPush", fmt.Sprint(next.BlockPush), "blockPull", fmt.Sprint(next.BlockPull)),
		})
		s.log.InfoContext(r.Context(), "httpapi: replication global block updated",
			"blocking", blocking, "block_push", next.BlockPush, "block_pull", next.BlockPull, "actor", p.Name)
		writePlainText(w, http.StatusOK, blockMessage(blocking, actPush, actPull))
	}
}

// blockMessage renders the §9.2-B-3 variants verbatim (confidence
// medium-high: official example + decompiled wording, §9.5 #2).
func blockMessage(blocking bool, actPush, actPull bool) string {
	switch {
	case actPush && actPull:
		if blocking {
			return "Successfully blocked all replications, no replication will be triggered."
		}
		return "Successfully unblocked all replications."
	case actPull:
		if blocking {
			return "Successfully blocked all pull replications, no pull replication will be triggered."
		}
		return "Successfully unblocked all pull replications."
	default: // actPush alone
		if blocking {
			return "Successfully blocked all push replications, no push replication will be triggered."
		}
		return "Successfully unblocked all push replications."
	}
}

// replicationTestBody is the optional/required body of the two test faces.
// Every field is WRITE-ONLY; the password never echoes and never logs.
type replicationTestBody struct {
	Name           string `json:"name"`
	TargetURL      string `json:"target_url"`
	TargetRepo     string `json:"target_repo"`
	TargetUsername string `json:"target_username"`
	TargetPassword string `json:"target_password"`
}

// writeReplicationTestResult renders the probe verdict: the object body at
// 200 (pass) or 400 (fail) — the inline reason IS the payload.
func writeReplicationTestResult(w http.ResponseWriter, res replication.TestResult) {
	status := http.StatusOK
	if !res.OK {
		status = http.StatusBadRequest
	}
	writeJSONBody(w, status, struct {
		OK         bool   `json:"ok"`
		StatusCode int    `json:"status_code"`
		Message    string `json:"message"`
	}{OK: res.OK, StatusCode: res.StatusCode, Message: res.Message})
}

// decodeReplicationTestBody drains the bounded body; an EMPTY body answers
// (nil, true) — the {id}/test face's no-override arm.
func decodeReplicationTestBody(w http.ResponseWriter, r *http.Request) (*replicationTestBody, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, replicationTestMaxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "test body could not be read")
		return nil, false
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, true
	}
	var body replicationTestBody
	if err := json.Unmarshal(raw, &body); err != nil {
		writeError(w, http.StatusBadRequest, "test body must be a JSON object: "+err.Error())
		return nil, false
	}
	return &body, true
}

// selfBaseURL resolves this instance's own origin: the configured base URL
// when set, else the request's scheme://host (§9.3's self-instance check —
// validateTargetIsDifferentInstance's BinFlow spelling).
func (s *Server) selfBaseURL(r *http.Request) string {
	if s.deps.Config != nil && strings.TrimSpace(s.deps.Config.Server.BaseURL) != "" {
		return strings.TrimSpace(s.deps.Config.Server.BaseURL)
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// normalizeOrigin reduces a URL to host + normalized port ("" when the
// scheme default). ok=false for anything unparsable.
func normalizeOrigin(raw string) (host, port string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", "", false
	}
	scheme := strings.ToLower(u.Scheme)
	host = strings.ToLower(u.Hostname())
	port = u.Port()
	if port == "" && scheme == "https" {
		port = "443"
	}
	if port == "" && scheme == "http" {
		port = "80"
	}
	// The scheme's default port normalizes away (http://h == http://h:80).
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	return host, port, true
}

// replicationTargetIsSelf reports whether targetURL names THIS instance:
// same host and effective port (scheme-insensitive — an http/https mix on
// one host:port still names the same deployment front door; a path
// difference does not clear the check, the base URL is the front door).
func replicationTargetIsSelf(targetURL, selfBase string) bool {
	th, tp, ok1 := normalizeOrigin(targetURL)
	sh, sp, ok2 := normalizeOrigin(selfBase)
	return ok1 && ok2 && th != "" && th == sh && tp == sp
}

// handleReplicationTest serves POST /api/v1/replications/{id}/test: probe
// the STORED config (§9.3's primary addressing — credentials and URL come
// from the row), with every non-empty body field overriding for this probe
// alone (§9.3's draft arm: an edited URL is testable before saving).
func (s *Server) handleReplicationTest(w http.ResponseWriter, r *http.Request, idRaw string) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "replication configuration requires an administrator account")
		return
	}
	if s.deps.Replication == nil {
		replicationNotConfigured(w)
		return
	}
	if s.deps.ReplicationTester == nil {
		writeError(w, http.StatusNotImplemented,
			"replication test is not wired on this instance (the push engine is absent)")
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(idRaw), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
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
	body, ok := decodeReplicationTestBody(w, r)
	if !ok {
		return
	}
	ov := replication.TargetOverride{}
	if body != nil {
		ov = replication.TargetOverride{
			TargetURL:      strings.TrimSpace(body.TargetURL),
			TargetRepo:     strings.TrimSpace(body.TargetRepo),
			TargetUsername: body.TargetUsername,
			TargetPassword: body.TargetPassword,
		}
	}
	s.runReplicationTest(w, r, cfg, ov, cfg.Name)
}

// handleReplicationTestDraft serves POST /api/v1/replications/test — the
// id-less draft face (§9.2-C-9: an UNSAVED form can test). The body is
// REQUIRED and carries the candidate; nothing is read from or written to
// the store.
func (s *Server) handleReplicationTestDraft(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "replication configuration requires an administrator account")
		return
	}
	if s.deps.ReplicationTester == nil {
		if s.deps.Replication == nil {
			replicationNotConfigured(w)
			return
		}
		writeError(w, http.StatusNotImplemented,
			"replication test is not wired on this instance (the push engine is absent)")
		return
	}
	body, ok := decodeReplicationTestBody(w, r)
	if !ok {
		return
	}
	if body == nil {
		writeError(w, http.StatusBadRequest,
			"a draft test requires a body: {target_url, target_repo, target_username?, target_password?}")
		return
	}
	// A candidate synthesized for the probe alone — the name rides for the
	// audit trail, nothing else is consumed.
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "(draft)"
	}
	cfg := &replication.ReplicationConfig{Name: name}
	ov := replication.TargetOverride{
		TargetURL:      strings.TrimSpace(body.TargetURL),
		TargetRepo:     strings.TrimSpace(body.TargetRepo),
		TargetUsername: body.TargetUsername,
		TargetPassword: body.TargetPassword,
	}
	s.runReplicationTest(w, r, cfg, ov, name)
}

// runReplicationTest performs the shared tail: the self-instance check
// (§9.3's semantic row — pull-face wording named, BinFlow spelling: the
// target must not be this instance's own front door), the probe, the audit
// row and the verdict rendering. The probe result carries no credential
// material by construction (NFR-S75).
func (s *Server) runReplicationTest(w http.ResponseWriter, r *http.Request, cfg *replication.ReplicationConfig, ov replication.TargetOverride, name string) {
	target := ov.TargetURL
	if target == "" {
		target = cfg.TargetURL
	}
	if replicationTargetIsSelf(target, s.selfBaseURL(r)) {
		res := replication.TestResult{Message: fmt.Sprintf(
			"Cannot replicate to the same instance: target url '%s' is this BinFlow instance", target)}
		s.auditReplicationTest(r, name, target, res)
		writeReplicationTestResult(w, res)
		return
	}
	res, err := s.deps.ReplicationTester.TestTarget(r.Context(), cfg, ov)
	if err != nil {
		if errors.Is(err, replication.ErrTestCredentialUnsealed) {
			// Never echo the sealed text; the config is named for the fix.
			writeError(w, http.StatusInternalServerError,
				fmt.Sprintf("target credential of config %q could not be unsealed for the probe (master key missing or changed)", name))
			return
		}
		writeError(w, http.StatusInternalServerError, "test replication target: "+err.Error())
		return
	}
	s.auditReplicationTest(r, name, target, res)
	writeReplicationTestResult(w, res)
}

// auditReplicationTest records one probe (§9.4 #2's payload suggestion:
// name, target_url, status_code). No credentials, no reason snippet that
// could echo target material — the verdict word travels instead.
func (s *Server) auditReplicationTest(r *http.Request, name, target string, res replication.TestResult) {
	p := principalFrom(r.Context())
	actor := ""
	if p != nil {
		actor = p.Name
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actor,
		Action: auditActionReplicationConfigTest,
		Detail: auditDetail("name", name, "target_url", target,
			"status_code", strconv.Itoa(res.StatusCode), "ok", fmt.Sprint(res.OK)),
	})
}
