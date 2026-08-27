package httpapi

// The instance-level GPG keypair plane (M11 T-319, ADR-0038 /
// docs/design/gpg-keypair.md): the Artifactory-compatible
// /api/security/keypair family, the repo association face on
// /api/v2/repositories/{repoKey}/keyPairs, and the BinFlow-native
// generation endpoint under /api/v1/admin/security.
//
//	POST   /binflow/api/security/keypair                          CapSecurityWrite  import (create-or-replace)
//	PUT    /binflow/api/security/keypair                           CapSecurityWrite  update (404 absent)
//	GET    /binflow/api/security/keypair                           CapSecurityRead   list
//	GET    /binflow/api/security/keypair/{pairName}               CapSecurityRead   one (KeyPairSummary)
//	DELETE /binflow/api/security/keypair/{pairName}               CapSecurityWrite  delete (in-use 400 guard)
//	POST   /binflow/api/security/keypair/verify                   CapSecurityWrite  verify material / stored pair
//	GET    /binflow/api/security/keypair/public/repositories/{repoKey} CapSecurityRead  armored public key
//	POST   /binflow/api/v1/admin/security/keypair/generate        CapSecurityWrite  server-side keygen
//	POST   /binflow/api/v2/repositories/{repoKey}/keyPairs        CapSecurityWrite  associate (text/plain body = pair name)
//	DELETE /binflow/api/v2/repositories/{repoKey}/keyPairs/{keyName} CapSecurityWrite disassociate
//
// Wire literals (paths, field names, KeyPairSummary, the 200 "OK" /
// "Key was verified." texts) follow the official Artifactory REST docs
// (spec section 1, level L-A); divergences D-1..D-8 are registered in
// docs/design/gpg-keypair.md section 2.6. The private key and the
// passphrase never appear in any response; audit details carry pair
// names and repository lists only (zero key material, ADR-0038 decision 3).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/keypair"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// Audit actions of the keypair plane (string literals, the
// authconfig/replication precedent — the Actions() picker addition is the
// audit owner's one-liner, registered in the T-319 report).
const (
	auditActionKeypairCreate    = "keypair.create"
	auditActionKeypairUpdate    = "keypair.update"
	auditActionKeypairGenerate  = "keypair.generate"
	auditActionKeypairDelete    = "keypair.delete"
	auditActionKeypairVerify    = "keypair.verify"
	auditActionKeypairAssociate = "keypair.associate"
)

// keypairMaxBodyBytes bounds the import/update payload: an armored
// RSA-4096 pair is ~8 KiB; 1 MiB is generous headroom while staying a
// bound (the authconfig family's cap).
const keypairMaxBodyBytes = 1 << 20

// keypairImportInput mirrors keypair.ImportInput plus the refused-by-name
// vault fields (presence-checked before the typed decode, the
// rejectM11RemoteFields posture: silently storing key material locally
// while the operator believes a vault backs it is the inert-field trap).
type keypairImportInput struct {
	PairName       string          `json:"pairName"`
	PairType       string          `json:"pairType"`
	Alias          string          `json:"alias"`
	PrivateKey     string          `json:"privateKey"`
	PublicKey      string          `json:"publicKey"`
	Passphrase     string          `json:"passphrase"`
	VaultKey       json.RawMessage `json:"vaultKey"`
	VaultPublicKey json.RawMessage `json:"vaultPublicKey"`
}

// refusedKeypairFields are the Enterprise vault spellings of KeyPairInput
// BinFlow does not serve (spec section 2.1).
var refusedKeypairFields = []string{"vaultKey", "vaultPublicKey"}

// KeypairPlane is the consumer-side seam behind the keypair endpoints
// (the keypair.Manager's management facet; nil Deps keeps them at the
// honest 503).
type KeypairPlane interface {
	Import(ctx context.Context, in keypair.ImportInput, actor string) (*keypair.Summary, error)
	Update(ctx context.Context, in keypair.ImportInput, actor string) (*keypair.Summary, error)
	Get(ctx context.Context, pairName string) (*keypair.Summary, error)
	List(ctx context.Context) ([]*keypair.Summary, error)
	Generate(ctx context.Context, in keypair.GenerateInput, actor string) (*keypair.Summary, error)
	VerifyMaterial(in keypair.ImportInput) error
	VerifyStored(ctx context.Context, pairName string) error
	Delete(ctx context.Context, pairName string) error
	PublicKeyOfRepo(ctx context.Context, repoKey string) (string, error)
}

// keypairsWired answers whether the keypair plane is assembled (the honest
// 503 posture of every optional Deps seam; unit stacks and degraded
// assemblies only).
func (s *Server) keypairsWired(w http.ResponseWriter, _ *http.Request) bool {
	if s.deps.Keypairs == nil {
		writeError(w, http.StatusServiceUnavailable, "key pair plane is not available on this instance")
		return false
	}
	return true
}

// handleKeypairImport serves POST /api/security/keypair (create or replace
// the named pair; 201 with the summary).
func (s *Server) handleKeypairImport(w http.ResponseWriter, r *http.Request) {
	if !s.keypairsWired(w, r) {
		return
	}
	in, ok := s.readKeypairInput(w, r)
	if !ok {
		return
	}
	summary, err := s.deps.Keypairs.Import(r.Context(), *in, actorName(r))
	if err != nil {
		s.writeKeypairError(w, r, "importing", err)
		return
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actorName(r),
		Action: auditActionKeypairCreate,
		Detail: keypairAuditDetail(in.PairName, in.Alias, nil),
	})
	s.log.InfoContext(r.Context(), "httpapi: key pair imported",
		"pairName", in.PairName, "actor", actorName(r))
	writeJSONBody(w, http.StatusCreated, summary)
}

// handleKeypairUpdate serves PUT /api/security/keypair (replace the named
// pair's material; 404 when absent, 200 with the summary).
func (s *Server) handleKeypairUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.keypairsWired(w, r) {
		return
	}
	in, ok := s.readKeypairInput(w, r)
	if !ok {
		return
	}
	summary, err := s.deps.Keypairs.Update(r.Context(), *in, actorName(r))
	if err != nil {
		s.writeKeypairError(w, r, "updating", err)
		return
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actorName(r),
		Action: auditActionKeypairUpdate,
		Detail: keypairAuditDetail(in.PairName, in.Alias, nil),
	})
	s.log.InfoContext(r.Context(), "httpapi: key pair updated",
		"pairName", in.PairName, "actor", actorName(r))
	writeJSONBody(w, http.StatusOK, summary)
}

// handleKeypairList serves GET /api/security/keypair.
func (s *Server) handleKeypairList(w http.ResponseWriter, r *http.Request) {
	if !s.keypairsWired(w, r) {
		return
	}
	summaries, err := s.deps.Keypairs.List(r.Context())
	if err != nil {
		s.writeKeypairError(w, r, "listing", err)
		return
	}
	if summaries == nil {
		summaries = []*keypair.Summary{}
	}
	writeJSONBody(w, http.StatusOK, summaries)
}

// handleKeypairGet serves GET /api/security/keypair/{pairName}.
func (s *Server) handleKeypairGet(w http.ResponseWriter, r *http.Request, pairName string) {
	if !s.keypairsWired(w, r) {
		return
	}
	summary, err := s.deps.Keypairs.Get(r.Context(), pairName)
	if err != nil {
		s.writeKeypairError(w, r, "reading", err)
		return
	}
	writeJSONBody(w, http.StatusOK, summary)
}

// handleKeypairDelete serves DELETE /api/security/keypair/{pairName}: the
// Artifactory 200 with the plain-text "OK" body, or the in-use guard's
// 400 naming the referencing repositories.
func (s *Server) handleKeypairDelete(w http.ResponseWriter, r *http.Request, pairName string) {
	if !s.keypairsWired(w, r) {
		return
	}
	if err := s.deps.Keypairs.Delete(r.Context(), pairName); err != nil {
		s.writeKeypairError(w, r, "deleting", err)
		return
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actorName(r),
		Action: auditActionKeypairDelete,
		Detail: keypairAuditDetail(pairName, "", nil),
	})
	s.log.InfoContext(r.Context(), "httpapi: key pair deleted",
		"pairName", pairName, "actor", actorName(r))
	writeText(w, http.StatusOK, "OK")
}

// keypairVerifyInput is the verify body: the full material form (the
// Artifactory KeyPairInput shape) or the pairName-only stored-pair form
// (BinFlow extension, spec divergence D-5).
type keypairVerifyInput struct {
	PairName   string `json:"pairName"`
	PairType   string `json:"pairType"`
	Alias      string `json:"alias"`
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
	Passphrase string `json:"passphrase"`
}

// handleKeypairVerify serves POST /api/security/keypair/verify: full
// material in the body → validate it; pairName only → validate the stored
// pair end to end. Success answers the Artifactory text "Key was
// verified."; refusal answers the errors[] envelope.
func (s *Server) handleKeypairVerify(w http.ResponseWriter, r *http.Request) {
	if !s.keypairsWired(w, r) {
		return
	}
	body, ok := s.readKeypairBody(w, r)
	if !ok {
		return
	}
	var in keypairVerifyInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "key pair verify: malformed JSON body: "+err.Error())
		return
	}
	var err error
	if strings.TrimSpace(in.PrivateKey) == "" && strings.TrimSpace(in.PublicKey) == "" {
		if strings.TrimSpace(in.PairName) == "" {
			writeError(w, http.StatusBadRequest,
				"key pair verify: provide the key material (privateKey/publicKey) or a pairName to verify the stored pair")
			return
		}
		err = s.deps.Keypairs.VerifyStored(r.Context(), in.PairName)
	} else {
		err = s.deps.Keypairs.VerifyMaterial(keypair.ImportInput{
			PairName:   in.PairName,
			PairType:   in.PairType,
			Alias:      in.Alias,
			PrivateKey: in.PrivateKey,
			PublicKey:  in.PublicKey,
			Passphrase: in.Passphrase,
		})
	}
	if err != nil {
		s.writeKeypairError(w, r, "verifying", err)
		return
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actorName(r),
		Action: auditActionKeypairVerify,
		Detail: keypairAuditDetail(in.PairName, in.Alias, nil),
	})
	writeText(w, http.StatusOK, "Key was verified.")
}

// handleKeypairPublicByRepo serves GET /api/security/keypair/public/
// repositories/{repoKey}: the repository's associated pair's armored
// public key as text (Artifactory's own Content-Type posture).
func (s *Server) handleKeypairPublicByRepo(w http.ResponseWriter, r *http.Request, repoKey string) {
	if !s.keypairsWired(w, r) {
		return
	}
	publicKey, err := s.deps.Keypairs.PublicKeyOfRepo(r.Context(), repoKey)
	if err != nil {
		s.writeKeypairError(w, r, "reading the repository key of", err)
		return
	}
	writeText(w, http.StatusOK, publicKey)
}

// handleKeypairGenerate serves POST /api/v1/admin/security/keypair/
// generate, the BinFlow-native server-side keygen (spec section 2.2):
// 201 with the summary; a taken name answers 409.
func (s *Server) handleKeypairGenerate(w http.ResponseWriter, r *http.Request) {
	if !s.keypairsWired(w, r) {
		return
	}
	body, ok := s.readKeypairBody(w, r)
	if !ok {
		return
	}
	var in keypair.GenerateInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "key pair generate: malformed JSON body: "+err.Error())
		return
	}
	summary, err := s.deps.Keypairs.Generate(r.Context(), in, actorName(r))
	if err != nil {
		s.writeKeypairError(w, r, "generating", err)
		return
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actorName(r),
		Action: auditActionKeypairGenerate,
		Detail: keypairAuditDetail(in.PairName, in.Alias, nil),
	})
	s.log.InfoContext(r.Context(), "httpapi: key pair generated",
		"pairName", in.PairName, "actor", actorName(r))
	writeJSONBody(w, http.StatusCreated, summary)
}

// handleKeypairAssociate serves POST /api/v2/repositories/{repoKey}/
// keyPairs (the Artifactory 7.19 association face): the request body is
// the pair name as plain text. The write rides the repository update path
// (the same validation chain as a config PUT — class/package-type rules
// and reference existence), so the association can never drift from what
// CreateRepo/UpdateRepo accept.
func (s *Server) handleKeypairAssociate(w http.ResponseWriter, r *http.Request, repoKey string) {
	if s.deps.ReposSvc == nil {
		writeError(w, http.StatusServiceUnavailable, "repository service is not available on this instance")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 256))
	if err != nil {
		writeError(w, http.StatusBadRequest, "key pair association: reading the pair name: "+err.Error())
		return
	}
	pairName := strings.TrimSpace(string(body))
	if pairName == "" {
		writeError(w, http.StatusBadRequest, "key pair association: the request body must carry the key pair name as plain text")
		return
	}
	p := principalFrom(r.Context())
	current, err := s.deps.ReposSvc.GetRepo(r.Context(), p, repoKey)
	if err != nil {
		s.writeKeypairError(w, r, "associating", err)
		return
	}
	if err := s.updateRepoKeypairRef(r, current, pairName); err != nil {
		s.writeKeypairError(w, r, "associating", err)
		return
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actorName(r),
		Action: auditActionKeypairAssociate,
		Detail: keypairAuditDetail(pairName, "", []string{repoKey}),
	})
	s.log.InfoContext(r.Context(), "httpapi: repository key pair associated",
		"repoKey", repoKey, "pairName", pairName, "actor", actorName(r))
	writeText(w, http.StatusOK, "OK")
}

// handleKeypairDisassociate serves DELETE /api/v2/repositories/{repoKey}/
// keyPairs/{keyName}: clears the association when it carries keyName
// (Artifactory removes the named key's assignment; BinFlow's single slot
// means the name is the guard against clearing the wrong pair — a repo
// associated with another name answers 404 for THIS name).
func (s *Server) handleKeypairDisassociate(w http.ResponseWriter, r *http.Request, repoKey, keyName string) {
	if s.deps.ReposSvc == nil {
		writeError(w, http.StatusServiceUnavailable, "repository service is not available on this instance")
		return
	}
	if keyName == "" {
		writeError(w, http.StatusBadRequest, "key pair disassociation: the key pair name is required")
		return
	}
	p := principalFrom(r.Context())
	current, err := s.deps.ReposSvc.GetRepo(r.Context(), p, repoKey)
	if err != nil {
		s.writeKeypairError(w, r, "disassociating", err)
		return
	}
	associated, _, refErr := keypair.RepoConfigReference(current.Config)
	if refErr != nil {
		s.writeKeypairError(w, r, "disassociating", refErr)
		return
	}
	if associated != keyName {
		writeError(w, http.StatusNotFound,
			fmt.Sprintf("key pair disassociation: repository %q is not associated with %q", repoKey, keyName))
		return
	}
	if err := s.updateRepoKeypairRef(r, current, ""); err != nil {
		s.writeKeypairError(w, r, "disassociating", err)
		return
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actorName(r),
		Action: auditActionKeypairAssociate,
		Detail: keypairAuditDetail(keyName, "", []string{repoKey}),
	})
	s.log.InfoContext(r.Context(), "httpapi: repository key pair disassociated",
		"repoKey", repoKey, "pairName", keyName, "actor", actorName(r))
	writeText(w, http.StatusOK, "OK")
}

// updateRepoKeypairRef merges the keyPairName field into the repository's
// config and rides UpdateRepo (validation + persistence in one).
func (s *Server) updateRepoKeypairRef(r *http.Request, current *metadata.Repo, pairName string) error {
	merged, err := mergeKeypairRef(current.Config, pairName)
	if err != nil {
		return err
	}
	update := *current
	update.Config = merged
	p := principalFrom(r.Context())
	_, err = s.deps.ReposSvc.UpdateRepo(r.Context(), p, &update)
	return err
}

// mergeKeypairRef sets (or clears, on "") the keyPairName field of a
// config blob, preserving every other field.
func mergeKeypairRef(config, pairName string) (string, error) {
	blob := config
	if strings.TrimSpace(blob) == "" {
		blob = "{}"
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(blob), &raw); err != nil {
		return "", fmt.Errorf("repository config: %w", err)
	}
	if pairName == "" {
		delete(raw, keypair.RepoConfigField)
	} else {
		raw[keypair.RepoConfigField] = pairName
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return "", fmt.Errorf("repository config: %w", err)
	}
	return string(out), nil
}

// routeKeypairAssociate adapts "v2/repositories/{repoKey}/keyPairs" to the
// associate handler (the router's one-string extraction does not fit the
// two-segment v2 shape).
func (s *Server) routeKeypairAssociate(rest string) http.HandlerFunc {
	repoKey := strings.TrimSuffix(strings.TrimPrefix(rest, "v2/repositories/"), "/keyPairs")
	return func(w http.ResponseWriter, r *http.Request) {
		if repoKey == "" || strings.Contains(repoKey, "/") {
			writeError(w, http.StatusBadRequest, "key pair association: malformed repository path")
			return
		}
		s.handleKeypairAssociate(w, r, repoKey)
	}
}

// routeKeypairDisassociate adapts "v2/repositories/{repoKey}/keyPairs/
// {keyName}" to the disassociate handler.
func (s *Server) routeKeypairDisassociate(rest string) http.HandlerFunc {
	tail := strings.TrimPrefix(rest, "v2/repositories/")
	repoKey, keyTail, ok := strings.Cut(tail, "/keyPairs/")
	return func(w http.ResponseWriter, r *http.Request) {
		if !ok || repoKey == "" || strings.Contains(repoKey, "/") || keyTail == "" || strings.Contains(keyTail, "/") {
			writeError(w, http.StatusBadRequest, "key pair disassociation: malformed repository key pair path")
			return
		}
		s.handleKeypairDisassociate(w, r, repoKey, keyTail)
	}
}

// ---------------------------------------------------------------------------
// shared plumbing

// readKeypairBody reads and bounds the JSON body.
func (s *Server) readKeypairBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, keypairMaxBodyBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "key pair: reading the request body: "+err.Error())
		return nil, false
	}
	if len(body) > keypairMaxBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("key pair: the request body exceeds the %d-byte limit", keypairMaxBodyBytes))
		return nil, false
	}
	return body, true
}

// readKeypairInput decodes the KeyPairInput shape after the by-name
// refusal of the vault fields.
func (s *Server) readKeypairInput(w http.ResponseWriter, r *http.Request) (*keypair.ImportInput, bool) {
	body, ok := s.readKeypairBody(w, r)
	if !ok {
		return nil, false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		writeError(w, http.StatusBadRequest, "key pair: malformed JSON body: "+err.Error())
		return nil, false
	}
	for _, field := range refusedKeypairFields {
		if _, ok := raw[field]; ok {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("key pair: %q is not supported (vault-backed key storage is not available in BinFlow); remove it and supply the key material directly", field))
			return nil, false
		}
	}
	var in keypairImportInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "key pair: malformed JSON body: "+err.Error())
		return nil, false
	}
	return &keypair.ImportInput{
		PairName:   in.PairName,
		PairType:   in.PairType,
		Alias:      in.Alias,
		PrivateKey: in.PrivateKey,
		PublicKey:  in.PublicKey,
		Passphrase: in.Passphrase,
	}, true
}

// writeKeypairError maps the domain taxonomy to the errors[] envelope
// (the Artifactory REST status posture of the family: 400 bad
// input/material/in-use, 404 unknown pair or repository, 409 generate
// name collision, 503 unwired plane).
func (s *Server) writeKeypairError(w http.ResponseWriter, r *http.Request, verb string, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, keypair.ErrInvalidPairName),
		errors.Is(err, keypair.ErrBadMaterial),
		errors.Is(err, keypair.ErrInUse),
		errors.Is(err, keypair.ErrNoMasterKey),
		errors.Is(err, metadata.ErrRepoNotFound),
		errors.Is(err, repo.ErrInvalidRepoConfig),
		errors.Is(err, repo.ErrRepoNotFound),
		errors.Is(err, repo.ErrInvalidRepoType):
		status = http.StatusBadRequest
	case errors.Is(err, keypair.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, keypair.ErrPairExists):
		status = http.StatusConflict
	}
	s.log.WarnContext(r.Context(), "httpapi: key pair request refused",
		"verb", verb, "error", err.Error())
	writeError(w, status, "key pair "+verb+": "+err.Error())
}

// keypairAuditDetail renders the audit detail (names only — zero key
// material, ADR-0038 decision 3).
func keypairAuditDetail(pairName, alias string, repos []string) string {
	type detail struct {
		PairName     string   `json:"pairName"`
		Alias        string   `json:"alias,omitempty"`
		Repositories []string `json:"repositories,omitempty"`
	}
	body, err := json.Marshal(detail{PairName: pairName, Alias: alias, Repositories: repos})
	if err != nil {
		return "{}"
	}
	return string(body)
}
