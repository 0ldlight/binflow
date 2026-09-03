package httpapi

// The remote-repository Test face (M16 T-442, FR-143.5; the remote form's
// Test connectivity button — the replication Test family's sibling, landed
// on the repositories domain per the conductor's wire ruling because its
// consumer surface and semantics differ: it probes a repository's
// CONFIGURED UPSTREAM, not a push target, and the repository form is its
// only consumer):
//
//	POST /binflow/api/repositories/{key}/test    CanManageRepo(write)
//
// The body is OPTIONAL and carries the draft-test arms — {url, username,
// password} — mirroring the replication test faces field-for-field: every
// non-empty field replaces the stored configuration's value for THIS probe
// alone, nothing is written, and a body that carries no password probes
// ANONYMOUS (the stored secret never silently ships to a different
// candidate host). An empty body probes the stored configuration.
//
// The verdict renders as the T-422 family's object body — ok at 200,
// ok:false with the inline reason at 400 (the auth.config.test posture);
// errors[] stays for face faults (404 unknown repository, 400 malformed
// body, 405-class wrong type, 5xx unseal). The probe itself is ZERO side
// effects by construction (remote.TestRepositoryUpstream): one read-only
// upstream GET, no state written anywhere, credentials never in any log
// (the audit row carries the verdict word and the target URL only).

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
)

// auditActionRepositoryRemoteTest records one remote upstream probe (the
// replication.config.test naming precedent, one domain over).
const auditActionRepositoryRemoteTest = "repository.remote.test"

// repositoryTestMaxBodyBytes caps the test body (a URL and a credential
// pair — a few KiB; 64 KiB is the replication face's own generous bound).
const repositoryTestMaxBodyBytes = 1 << 16

// repositoryTestBody is the optional body of the test face. Every field is
// WRITE-ONLY; the password never echoes and never logs.
type repositoryTestBody struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleRepositoryTest serves POST /api/repositories/{key}/test.
func (s *Server) handleRepositoryTest(w http.ResponseWriter, r *http.Request, key string) {
	p := principalFrom(r.Context())
	if p == nil {
		// The route gate demands an authenticated manager; this re-check
		// only keeps a mis-wired assembly honest before the actor reaches
		// the audit trail.
		writeError(w, http.StatusForbidden, "repository test requires an authenticated principal")
		return
	}
	row, err := s.deps.Repos.Get(r.Context(), key)
	if err != nil {
		switch {
		case errors.Is(err, metadata.ErrRepoNotFound):
			writeError(w, http.StatusNotFound, "repository not found: "+key)
		case metadata.IsStoreBusy(err):
			writeError(w, http.StatusServiceUnavailable,
				"metadata store is busy, retry shortly: "+err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "get repository: "+err.Error())
		}
		return
	}
	if row.Type != "remote" {
		writeError(w, http.StatusBadRequest, "repository '"+key+"' is a "+row.Type+
			" repository; only remote repositories have an upstream to test")
		return
	}
	body, ok := decodeRepositoryTestBody(w, r)
	if !ok {
		return
	}
	ov := remote.UpstreamOverride{}
	if body != nil {
		ov = remote.UpstreamOverride{
			URL:      strings.TrimSpace(body.URL),
			Username: body.Username,
			Password: body.Password,
		}
	}
	res, err := remote.TestRepositoryUpstream(r.Context(), s.deps.Metadata, key, ov)
	if err != nil {
		if errors.Is(err, remote.ErrTestCredentialUnsealed) {
			// Never echo the sealed text; the repository is named for the fix.
			writeError(w, http.StatusInternalServerError,
				"stored upstream credential of repository '"+key+"' could not be unsealed for the probe (master key missing or changed)")
			return
		}
		writeError(w, http.StatusInternalServerError, "test repository upstream: "+err.Error())
		return
	}
	target := ov.URL
	if target == "" {
		if rc, cerr := s.deps.Metadata.Remote().GetConfig(r.Context(), key); cerr == nil && rc != nil {
			target = rc.URL
		}
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  p.Name,
		Action: auditActionRepositoryRemoteTest,
		Detail: auditDetail("repo", key, "url", target,
			"status_code", strconv.Itoa(res.StatusCode), "ok", strconv.FormatBool(res.OK)),
	})
	writeRepositoryTestResult(w, res)
}

// decodeRepositoryTestBody drains the bounded body; an EMPTY body answers
// (nil, true) — the stored-configuration probe arm.
func decodeRepositoryTestBody(w http.ResponseWriter, r *http.Request) (*repositoryTestBody, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, repositoryTestMaxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "test body could not be read")
		return nil, false
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, true
	}
	var body repositoryTestBody
	if err := json.Unmarshal(raw, &body); err != nil {
		writeError(w, http.StatusBadRequest, "test body must be a JSON object: "+err.Error())
		return nil, false
	}
	return &body, true
}

// writeRepositoryTestResult renders the probe verdict: the object body at
// 200 (pass) or 400 (fail) — the inline reason IS the payload (the
// replication test family's renderer, verbatim shape).
func writeRepositoryTestResult(w http.ResponseWriter, res remote.TestResult) {
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
