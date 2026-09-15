// The upload/append/query orchestration of the build domain (M17 T-508,
// FR-152.2 / ADR-0045 decision 4 + Errata ①②④): the write faces that weave
// onto the same allow() mirror T-507 installed, the started-format gate that
// keeps the lexicographic ordering projections honest (T-507 leftover 1),
// and the append-merge law whose key is the module id (Errata ④). The wire
// JSON layout is the official public data contract (build-info.md §3.1, a
// clean-room anchor — not translated code); the fields the service does not
// interpret ride the payload archive verbatim.

package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// ErrInvalidBuildInfo marks a malformed build info document (the upload and
// append 400 family's service face). A well-formed document addressing a
// denied build is ErrForbidden; a well-formed, authorized address of a
// missing run wraps metadata.ErrBuildNotFound.
var ErrInvalidBuildInfo = errors.New("build: invalid build info")

// Segment caps: generous CI-scale ceilings whose only job is turning an
// absurd document into an honest 400 instead of an absurd transaction (the
// wire-size cap lives one layer up, at the httpapi body reader).
const (
	maxInfoModules           = 10000
	maxModuleArtifacts       = 100000
	maxModuleDependencies    = 100000
	maxBuildProperties       = 10000
	maxBuildPropertyKeyBytes = 512
)

// startedLayouts are the accepted `started` spellings: the Java canonical
// (yyyy-MM-dd'T'HH:mm:ss.SSSZ — offset WITHOUT the colon, the webhook
// envelope's build_started form), RFC3339-with-fraction (trailing Z or
// +01:00, 0..9 fractional digits) and the seconds-precision RFC822-zone
// form. Everything re-renders through startedCanonical so the stored literal
// is uniform — the lexicographic sort IS the chronological sort (T-507
// leftover 1: mixed-zone literals made MAX(started) lie).
const (
	startedCanonical = "2006-01-02T15:04:05.000-0700" // in: Java form; out: UTC
	startedLayoutISO = time.RFC3339Nano               // Z / +01:00, any fraction
	startedLayoutSec = "2006-01-02T15:04:05-0700"     // no millis, +0000 form
)

// NormalizeStarted parses one accepted `started` spelling and returns the
// canonical UTC rendering (`2006-01-02T15:04:05.000+0000`). Every write face
// stores this form and every read face normalizes its ?started= through the
// same function, so a caller may address a run with the literal it uploaded
// even after canonicalization.
func NormalizeStarted(s string) (string, error) {
	for _, layout := range []string{startedCanonical, startedLayoutISO, startedLayoutSec} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(startedCanonical), nil
		}
	}
	return "", fmt.Errorf("build started %q must be an ISO8601 timestamp (yyyy-MM-dd'T'HH:mm:ss.SSSZ): %w",
		s, ErrInvalidBuildInfo)
}

// Info is the INTERPRETED subset of a build info document: the fields the
// service reads, validates and normalizes into the 024 tables. Every other
// wire field (buildAgent, agent, vcs, licenseControl, issues, url,
// durationMillis, artifactoryPluginVersion, ...) is not interpreted — the
// whole document rides builds.payload verbatim and the GET face echoes it
// from there (build-info.md §3.1's top-level field set).
type Info struct {
	Name       string            `json:"name"`
	Number     string            `json:"number"`
	Type       string            `json:"type"` // MAVEN|GRADLE|ANT|IVY|GENERIC; '' allowed
	Started    string            `json:"started"`
	Modules    []*Module         `json:"modules"`
	Properties map[string]string `json:"properties"`
}

// Module is one wire modules[] entry. ID is the append merge key (same id =
// same module, artifacts and dependencies append — Errata ④) and the
// "<child-name>/<child-number>" reference form of aggregate builds.
// Properties and the artifacts' originalDeploymentRepo are payload-archive
// only in M17: the 024 family has no module_props table (ADR-0045 decision 2
// pins six tables), so the normalized echo does not carry them.
type Module struct {
	ID           string            `json:"id"`
	Type         string            `json:"type"`
	Properties   map[string]string `json:"properties"`
	Artifacts    []*Artifact       `json:"artifacts"`
	Dependencies []*Dependency     `json:"dependencies"`
}

// Artifact is one wire artifacts[] entry. Path is the association: the
// "<repoKey>/<node path>" whole form whose repo segment splits into the
// build_artifacts nodes FK — the 024 design ("the wire path IS the
// association"). A path that does not resolve to a live node (or whose
// sha256 disagrees with the node's) lands record-only: the row is kept, no
// association is claimed.
type Artifact struct {
	Type   string `json:"type"`
	Sha1   string `json:"sha1"`
	Sha256 string `json:"sha256"`
	Md5    string `json:"md5"`
	Name   string `json:"name"`
	Path   string `json:"path"`
}

// Dependency is one wire dependencies[] entry: what the build CONSUMED.
// Dependencies never resolve to nodes (inv-4 D1); Scopes renders as the
// wire scopes[] array; RequestedBy is payload-archive only (an append body
// has no payload to land in — registered M17 debt).
type Dependency struct {
	Type        string     `json:"type"`
	Sha1        string     `json:"sha1"`
	Sha256      string     `json:"sha256"`
	Md5         string     `json:"md5"`
	ID          string     `json:"id"`
	Scopes      []string   `json:"scopes"`
	RequestedBy [][]string `json:"requestedBy"`
}

// NodeChecker is the consumer-side node-resolution seam behind the artifact
// association: metadata's NodeStore satisfies it structurally (the RepoLookup
// precedent — one method, discovered at assembly).
type NodeChecker interface {
	Get(ctx context.Context, repoKey, path string) (*metadata.Node, error)
}

// DigestSource resolves the blob digest triple behind any one present
// digest — the partial-checksum backfill's seam (build-info.md §11.3
// rewrite 2: a row carrying one or two of sha1/sha256/md5 is completed
// from the binary ledger; all-or-nothing rows are skipped). metadata's
// BlobStore satisfies it structurally.
type DigestSource interface {
	Get(ctx context.Context, sha256 string) (*metadata.Blob, error)
	GetBySha1(ctx context.Context, sha1 string) (*metadata.Blob, error)
	GetByMd5(ctx context.Context, md5 string) (*metadata.Blob, error)
}

// checksumTriple is the backfill's mutable digest view of one
// artifact/dependency row.
type checksumTriple struct{ sha1, sha256, md5 string }

// backfillChecksums completes a row's missing digests from the ledger
// when exactly one or two are present (the reference's rewrite rule — a
// fully-populated or fully-empty row is left untouched; a ledger miss
// leaves the row as the client sent it, never an error: the backfill is
// a best-effort completion, the row's own validity does not hang on it).
func (s *Service) backfillChecksums(ctx context.Context, t *checksumTriple) {
	present := 0
	for _, v := range []string{t.sha1, t.sha256, t.md5} {
		if v != "" {
			present++
		}
	}
	if present == 0 || present == 3 || s.digests == nil {
		return
	}
	var (
		b   *metadata.Blob
		err error
	)
	switch {
	case t.sha256 != "":
		b, err = s.digests.Get(ctx, t.sha256)
	case t.sha1 != "":
		b, err = s.digests.GetBySha1(ctx, t.sha1)
	default:
		b, err = s.digests.GetByMd5(ctx, t.md5)
	}
	if err != nil || b == nil {
		return // no ledger match: the row rides as-sent
	}
	if t.sha1 == "" && b.Sha1 != "" {
		t.sha1 = b.Sha1
	}
	if t.sha256 == "" && b.Sha256 != "" {
		t.sha256 = b.Sha256
	}
	if t.md5 == "" && b.Md5 != "" {
		t.md5 = b.Md5
	}
}

// Option configures the optional seams of New (the WithXxx convention).
type Option func(*Service)

// WithNodes wires the node-resolution seam the artifact association
// resolves through. Without it every artifact lands record-only (the
// metadata-less unit-stack posture — honest, never an error).
func WithNodes(n NodeChecker) Option {
	return func(s *Service) { s.nodes = n }
}

// WithBlobs wires the digest-resolution seam the partial-checksum
// backfill completes through (upload.go's DigestSource; nil = rows ride
// with exactly the digests the client declared).
func WithBlobs(d DigestSource) Option {
	return func(s *Service) { s.digests = d }
}

// UploadResult reports what one full upload did.
type UploadResult struct {
	// Created reports the first-publication arm (no prior run at the
	// resolved four-tuple). false = the overwrite arm ran (official note:
	// re-publishing a name+number requires the delete permission too).
	Created bool
	// Started is the canonical started literal actually stored.
	Started string
	// Repo is the resolved build_repo logical key.
	Repo string
	// Checksum is the sha256 of the stored build JSON document (the X-
	// Checksum-Sha256 response header's payload, build-info.md §11.1-E1:
	// the reference computes it over the manifest it persists, not the
	// bytes that arrived — BinFlow re-serializes with its own rewrites,
	// so the checksum follows the STORED document the same way).
	Checksum string
}

// trimLE20 cuts leading code points <= U+0020 — the exact set Java's
// String.trim() removes, which is what the reference's coordinate guard
// strips before its dot-prefix check.
func trimLE20(s string) string {
	return strings.TrimLeftFunc(s, func(r rune) bool { return r <= 0x20 })
}

// wireError carries a spec-verbatim wire message under a domain sentinel
// identity: errors.Is matches the sentinel while Error() stays the exact
// spec sentence (a plain %w wrap would leak the sentinel's own text onto
// the wire after the message — build-info.md's pinned wordings are whole
// messages, not prefixes).
type wireError struct {
	msg      string
	sentinel error
}

func (e *wireError) Error() string { return e.msg }
func (e *wireError) Is(target error) bool {
	return target == e.sentinel // errors.Is compares by identity here
}

// validateHiddenCoords refuses the hidden-coordinate spellings the
// buildinfo layout would bury under a dot-directory (build-info.md §11.3
// rewrite 4, the RTDEV-94003 posture): a name or number whose leading
// whitespace is stripped and then starts with '.' answers the spec's
// verbatim 400 wording.
func validateHiddenCoords(name, number string) error {
	if strings.HasPrefix(trimLE20(name), ".") {
		return &wireError{msg: "Build name must not start with '.'", sentinel: ErrInvalidBuildInfo} //nolint:staticcheck // ST1005: build-info.md §11.3's verbatim wire wording
	}
	if strings.HasPrefix(trimLE20(number), ".") {
		return &wireError{msg: "Build number must not start with '.'", sentinel: ErrInvalidBuildInfo} //nolint:staticcheck // ST1005: build-info.md §11.3's verbatim wire wording
	}
	return nil
}

// rewritePayload applies the server's own document rewrites to the raw
// upload body and returns the stored manifest: artifactoryPrincipal is
// overwritten with the authenticated actor (§11.3 rewrite 1 — the
// reference stores the server-side identity, never the client's claim).
// An actor-less (anonymous) call keeps the field the document carried.
// The re-serialization is the reference's own posture — its stored JSON
// is the re-rendered document, and the X-Checksum-Sha256 header is taken
// over exactly these bytes.
func rewritePayload(raw []byte, actor string, hasActor bool) (string, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("build info is not valid JSON: %w: %s", ErrInvalidBuildInfo, err.Error())
	}
	if hasActor {
		doc["artifactoryPrincipal"] = actor
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("build info could not be re-serialized: %w: %s", ErrInvalidBuildInfo, err.Error())
	}
	return string(out), nil
}

// Upload is PUT /api/build (the BODY-carried form — ADR-0045 Errata ①: the
// path-segment skeleton is voided): a full-document save of one run.
//
// Gate ladder (rejection precedes existence — no oracle for the denied):
// w(buildRepo, name) always; d(buildRepo, name) additionally when a run
// already sits at the resolved four-tuple (the official "overwriting an
// existing name+number requires delete permission" note — a caller holding
// w but not d can therefore learn whether a run exists, which is inherent
// in the official semantics, not a BinFlow invention).
//
// Replace law: header, modules and properties are replaced whole (PUT is
// the full-save verb — the append face is the incremental one); the
// promotion history is NOT — append-only history survives re-publication
// (spec §2.4's immutability law outranks the reference's delete-and-recreate
// file-path behavior, which BinFlow does not carry).
func (s *Service) Upload(ctx context.Context, p *Principal, raw []byte, buildRepo string) (*UploadResult, error) {
	var info Info
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("build info is not valid JSON: %w: %s", ErrInvalidBuildInfo, err.Error())
	}

	c := Coordinate{Name: info.Name, Number: info.Number, Started: info.Started, Repo: buildRepo}
	c = c.Resolve()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := validateHiddenCoords(c.Name, c.Number); err != nil {
		return nil, err
	}
	started, err := NormalizeStarted(info.Started)
	if err != nil {
		return nil, err
	}
	c.Started = started

	if !s.allow(ctx, p, c.Repo, c.Name, auth.ActionWrite) {
		return nil, fmt.Errorf("build %s#%s: %w", c.Name, c.Number, ErrForbidden)
	}
	created := true
	if _, err := s.store.GetBuild(ctx, c.Name, c.Number, c.Started, c.Repo); err == nil {
		// Overwrite arm: the official delete-permission note (Errata ①).
		if !s.allow(ctx, p, c.Repo, c.Name, auth.ActionDelete) {
			return nil, fmt.Errorf("build %s#%s overwrite: %w", c.Name, c.Number, ErrForbidden)
		}
		created = false
	} else if !errors.Is(err, metadata.ErrBuildNotFound) {
		return nil, fmt.Errorf("build %s#%s lookup: %w", c.Name, c.Number, err)
	}

	modules, err := s.toStoreModules(ctx, info.Modules)
	if err != nil {
		return nil, err
	}
	props, err := toStoreProperties(info.Properties)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	actor := ""
	if p != nil {
		actor = p.Name
	}
	// The stored manifest: the client document with the server's own
	// rewrites applied (artifactoryPrincipal, §11.3 rewrite 1). The
	// response's X-Checksum-Sha256 is taken over exactly these bytes.
	payload, err := rewritePayload(raw, actor, p != nil)
	if err != nil {
		return nil, err
	}
	// PutBuild first (the parent row the segment FKs hang from), then the
	// two child segments — each replace-whole write is one transaction in
	// the store; the run header is the anchor.
	header := &metadata.Build{
		Name: c.Name, Number: c.Number, Started: c.Started, Repo: c.Repo,
		Type: info.Type, Payload: payload,
		CreatedBy: actor, CreatedAt: now, UpdatedBy: actor, UpdatedAt: now,
	}
	if err := s.store.PutBuild(ctx, header); err != nil {
		return nil, fmt.Errorf("build %s#%s save: %w", c.Name, c.Number, err)
	}
	if err := s.store.PutModules(ctx, c.Name, c.Number, c.Started, c.Repo, modules); err != nil {
		return nil, fmt.Errorf("build %s#%s modules: %w", c.Name, c.Number, err)
	}
	if err := s.store.PutProperties(ctx, c.Name, c.Number, c.Started, c.Repo, props); err != nil {
		return nil, fmt.Errorf("build %s#%s properties: %w", c.Name, c.Number, err)
	}
	// The upload's audit row (ADR-0045 decision 10's build.upload — the
	// T-509 landing of the +5 words; same-address best-effort as the
	// promote/retention faces), then the webhook uploaded event at the
	// same tail (T-510, ADR-0045 decision 7: every successful publication
	// — first upload and overwrite alike — fires build/uploaded).
	s.recordAudit(ctx, audit.Event{
		Actor: actor, Action: audit.ActionBuildUpload,
		Repo: c.Repo, Path: c.Name,
		Detail: fmt.Sprintf(`{"number":%q,"created":%t,"modules":%d}`,
			c.Number, created, len(modules)),
	})
	s.emitWebhook(ctx, WebhookEvent{
		Type: EventUploaded, Name: c.Name, Number: c.Number,
		Started: c.Started, Repo: c.Repo, Principal: p,
	})
	sum := sha256.Sum256([]byte(payload))
	return &UploadResult{
		Created: created, Started: c.Started, Repo: c.Repo,
		Checksum: hex.EncodeToString(sum[:]),
	}, nil
}

// Append is POST /api/build/append/{name}/{number} (Errata ①: POST, the
// module-ARRAY body, 204 on success): the incremental merge face.
//
// Gate: Deploy ∧ Delete per the official permission note — BinFlow
// w(buildRepo, name) ∧ d(buildRepo, name), evaluated BEFORE the parent
// lookup (a denied caller gets 403 with no existence oracle). The parent
// must already exist: a missing run answers metadata.ErrBuildNotFound (the
// caller renders the spec's verbatim "Build-Info not found"; T-507 leftover
// 3 — the FK's generic error is never the mapper's input).
//
// Merge law (Errata ④): modules merge BY ID — an incoming module whose id
// already exists APPENDS its artifacts and dependencies to that module
// (never overwrites); a new id lands as a new module. Nothing is dropped.
func (s *Service) Append(ctx context.Context, p *Principal, c Coordinate, raw []byte) (*metadata.Build, error) {
	c = c.Resolve()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.Started != "" {
		started, err := NormalizeStarted(c.Started)
		if err != nil {
			return nil, err
		}
		c.Started = started
	}

	if !s.allow(ctx, p, c.Repo, c.Name, auth.ActionWrite) ||
		!s.allow(ctx, p, c.Repo, c.Name, auth.ActionDelete) {
		return nil, fmt.Errorf("build %s#%s append: %w", c.Name, c.Number, ErrForbidden)
	}

	// Parent first: the 404 verdict on a missing run precedes any body
	// interpretation (addressing errors before payload errors — the repo
	// PUT ladder's order). started = '' resolves the LATEST run, the merge
	// then lands on the run GetBuild resolved.
	parent, err := s.store.GetBuild(ctx, c.Name, c.Number, c.Started, c.Repo)
	if err != nil {
		return nil, fmt.Errorf("build %s#%s append: %w", c.Name, c.Number, err)
	}

	wire, err := decodeModuleArray(raw)
	if err != nil {
		return nil, err
	}
	incoming, err := s.toStoreModules(ctx, wire)
	if err != nil {
		return nil, err
	}

	// Read-modify-write under one lock: two concurrent appends to the same
	// run must both land (last-writer-wins on a stale base would silently
	// drop a whole merge — CI-frequency traffic makes the global lock free).
	s.mergeMu.Lock()
	defer s.mergeMu.Unlock()

	existing, err := s.store.ListModules(ctx, c.Name, c.Number, parent.Started, c.Repo)
	if err != nil {
		return nil, fmt.Errorf("build %s#%s append read: %w", c.Name, c.Number, err)
	}
	merged := mergeModules(existing, incoming)
	if err := s.store.PutModules(ctx, c.Name, c.Number, parent.Started, c.Repo, merged); err != nil {
		return nil, fmt.Errorf("build %s#%s append write: %w", c.Name, c.Number, err)
	}
	// The append's audit row (decision 10's build.append — one row per
	// successful merge, the modules merged as the detail), then the
	// webhook uploaded event on the SAME tail (T-510: an append is a
	// publication of the run it merged into — the resolved parent's
	// coordinates, not the caller's looser addressing).
	s.recordAudit(ctx, audit.Event{
		Actor: actorOf(p), Action: audit.ActionBuildAppend,
		Repo: c.Repo, Path: c.Name,
		Detail: fmt.Sprintf(`{"number":%q,"modules_in":%d,"modules_total":%d}`,
			c.Number, len(incoming), len(merged)),
	})
	s.emitWebhook(ctx, WebhookEvent{
		Type: EventUploaded, Name: parent.Name, Number: parent.Number,
		Started: parent.Started, Repo: parent.Repo, Principal: p,
	})
	return parent, nil
}

// actorOf renders the audit actor of a principal ("" = anonymous; the
// recorder stamps its own default).
func actorOf(p *Principal) string {
	if p == nil {
		return ""
	}
	return p.Name
}

// mergeModules folds incoming into existing by module id: same id appends
// artifacts and dependencies (seq reassigned after the module's current
// tail), a new id appends a whole module. existing is reused in place —
// callers hand over a private slice.
func mergeModules(existing, incoming []*metadata.BuildModule) []*metadata.BuildModule {
	out := append([]*metadata.BuildModule(nil), existing...)
	byID := make(map[string]*metadata.BuildModule, len(out))
	for _, m := range out {
		byID[m.ID] = m
	}
	for _, m := range incoming {
		dst, ok := byID[m.ID]
		if !ok {
			byID[m.ID] = m
			out = append(out, m)
			continue
		}
		base := int64(len(dst.Artifacts))
		for i, a := range m.Artifacts {
			a.Seq = base + int64(i)
			dst.Artifacts = append(dst.Artifacts, a)
		}
		base = int64(len(dst.Dependencies))
		for i, d := range m.Dependencies {
			d.Seq = base + int64(i)
			dst.Dependencies = append(dst.Dependencies, d)
		}
	}
	return out
}

// decodeModuleArray parses the append body: a JSON ARRAY of modules (the
// official + first-party-OpenAPI form; the historic single-object shape is
// superseded — build-info.md §5's divergence note). An empty array is a
// valid no-op merge.
func decodeModuleArray(raw []byte) ([]*Module, error) {
	var modules []*Module
	if err := json.Unmarshal(raw, &modules); err != nil {
		return nil, fmt.Errorf("append body is not a JSON array of modules: %w: %s",
			ErrInvalidBuildInfo, err.Error())
	}
	return modules, nil
}

// toStoreModules converts the wire modules into store rows: validation
// (non-empty ids, no duplicate ids inside one document, segment caps) and
// the per-artifact node association.
func (s *Service) toStoreModules(ctx context.Context, wire []*Module) ([]*metadata.BuildModule, error) {
	if len(wire) > maxInfoModules {
		return nil, fmt.Errorf("build info carries %d modules (max %d): %w", len(wire), maxInfoModules, ErrInvalidBuildInfo)
	}
	out := make([]*metadata.BuildModule, 0, len(wire))
	seen := make(map[string]bool, len(wire))
	for _, m := range wire {
		if m == nil {
			continue
		}
		if m.ID == "" {
			return nil, fmt.Errorf("module id is empty: %w", ErrInvalidBuildInfo)
		}
		if seen[m.ID] {
			return nil, fmt.Errorf("duplicate module id %q: %w", m.ID, ErrInvalidBuildInfo)
		}
		seen[m.ID] = true
		if len(m.Artifacts) > maxModuleArtifacts {
			return nil, fmt.Errorf("module %q carries %d artifacts (max %d): %w",
				m.ID, len(m.Artifacts), maxModuleArtifacts, ErrInvalidBuildInfo)
		}
		if len(m.Dependencies) > maxModuleDependencies {
			return nil, fmt.Errorf("module %q carries %d dependencies (max %d): %w",
				m.ID, len(m.Dependencies), maxModuleDependencies, ErrInvalidBuildInfo)
		}
		row := &metadata.BuildModule{ID: m.ID, Type: m.Type}
		for i, a := range m.Artifacts {
			repoKey, nodePath, err := s.resolveArtifactNode(ctx, a)
			if err != nil {
				return nil, fmt.Errorf("module %q artifact association: %w", m.ID, err)
			}
			digests := checksumTriple{sha1: a.Sha1, sha256: a.Sha256, md5: a.Md5}
			s.backfillChecksums(ctx, &digests)
			row.Artifacts = append(row.Artifacts, &metadata.BuildArtifact{
				Seq: int64(i), Name: a.Name, Type: a.Type,
				Sha1: digests.sha1, Sha256: digests.sha256, Md5: digests.md5,
				RepoKey: repoKey, Path: nodePath,
			})
		}
		for i, d := range m.Dependencies {
			if d.ID == "" {
				return nil, fmt.Errorf("module %q has a dependency with an empty id: %w", m.ID, ErrInvalidBuildInfo)
			}
			digests := checksumTriple{sha1: d.Sha1, sha256: d.Sha256, md5: d.Md5}
			s.backfillChecksums(ctx, &digests)
			row.Dependencies = append(row.Dependencies, &metadata.BuildDependency{
				Seq: int64(i), ID: d.ID, Type: d.Type,
				Scopes: strings.Join(d.Scopes, ","),
				Sha1:   digests.sha1, Sha256: digests.sha256, Md5: digests.md5,
			})
		}
		out = append(out, row)
	}
	return out, nil
}

// resolveArtifactNode splits the wire path "<repoKey>/<node path>" and
// resolves the association against the live nodes table: an existing node
// carries the FK pair, everything else (no seam wired, no '/' in the path,
// missing node, or a sha256 disagreement) lands record-only ("", ""). A
// store failure short of not-found propagates — a flaky lookup must not
// silently degrade the association.
func (s *Service) resolveArtifactNode(ctx context.Context, a *Artifact) (repoKey, path string, err error) {
	if a.Path == "" || s.nodes == nil {
		return "", "", nil
	}
	repoKey, path, ok := strings.Cut(a.Path, "/")
	if !ok || repoKey == "" || path == "" {
		// No repo segment to hang the FK on: record-only.
		return "", "", nil
	}
	node, err := s.nodes.Get(ctx, repoKey, path)
	if err != nil {
		if errors.Is(err, metadata.ErrNodeNotFound) {
			return "", "", nil
		}
		return "", "", err
	}
	if a.Sha256 != "" && node.Sha256 != "" &&
		!strings.EqualFold(a.Sha256, node.Sha256) {
		// The document and the node disagree: keeping the association would
		// forge a link the checksums refute — record-only.
		return "", "", nil
	}
	return repoKey, path, nil
}

// toStoreProperties converts the wire properties map into the sorted row
// set (deterministic order — the store's set semantics don't care, tests
// and echoes do).
func toStoreProperties(wire map[string]string) ([]*metadata.BuildProperty, error) {
	if len(wire) == 0 {
		return nil, nil
	}
	if len(wire) > maxBuildProperties {
		return nil, fmt.Errorf("build info carries %d properties (max %d): %w",
			len(wire), maxBuildProperties, ErrInvalidBuildInfo)
	}
	keys := make([]string, 0, len(wire))
	for k := range wire {
		if k == "" {
			return nil, fmt.Errorf("build property name is empty: %w", ErrInvalidBuildInfo)
		}
		if len(k) > maxBuildPropertyKeyBytes {
			return nil, fmt.Errorf("build property name exceeds %d bytes: %w",
				maxBuildPropertyKeyBytes, ErrInvalidBuildInfo)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*metadata.BuildProperty, 0, len(keys))
	for _, k := range keys {
		out = append(out, &metadata.BuildProperty{Name: k, Value: wire[k]})
	}
	return out, nil
}

// Detail is the single-run read assembly behind GET /api/build/{name}/
// {number}: the gated header plus the three child segments the echo
// overlays onto the archived payload.
type Detail struct {
	Build      *metadata.Build
	Modules    []*metadata.BuildModule
	Properties []*metadata.BuildProperty
	Promotions []*metadata.BuildPromotion
}

// GetBuildDetail is the query face's orchestration: the same r(buildRepo,
// name) gate GetBuild runs (denial first — no existence oracle), started
// normalized when the caller disambiguates a run, then the child segments
// of the RESOLVED run (started = ” resolves the latest, which the segments
// must follow).
func (s *Service) GetBuildDetail(ctx context.Context, p *Principal, c Coordinate) (*Detail, error) {
	c = c.Resolve()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.Started != "" {
		started, err := NormalizeStarted(c.Started)
		if err != nil {
			return nil, err
		}
		c.Started = started
	}
	b, err := s.GetBuild(ctx, p, c)
	if err != nil {
		return nil, err
	}
	d := &Detail{Build: b}
	if d.Modules, err = s.store.ListModules(ctx, b.Name, b.Number, b.Started, b.Repo); err != nil {
		return nil, fmt.Errorf("build %s#%s modules read: %w", b.Name, b.Number, err)
	}
	if d.Properties, err = s.store.ListProperties(ctx, b.Name, b.Number, b.Started, b.Repo); err != nil {
		return nil, fmt.Errorf("build %s#%s properties read: %w", b.Name, b.Number, err)
	}
	if d.Promotions, err = s.store.ListPromotions(ctx, b.Name, b.Number, b.Started, b.Repo); err != nil {
		return nil, fmt.Errorf("build %s#%s promotions read: %w", b.Name, b.Number, err)
	}
	return d, nil
}
