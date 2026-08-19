package npm

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // G401: npm dist.shasum protocol digest, never a security primitive
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// PUT /<name> — the publish/deprecate document ([NPM-API] publish; spec
// section 2.3). The validation order IS the error order: ten steps, each
// failure answering before the next runs. Wording and codes are pinned by
// the PRD v1.1/v1.2 rulings (duplicate publish 403, integrity 400 by the
// BinFlow strictness decision R8).

// publishBody is the parsed publish document.
type publishBody struct {
	raw          map[string]any
	name         string
	versions     map[string]map[string]any // version -> manifest
	distTags     map[string]string
	attachments  map[string]attachment
	deprecated   map[string]string // version -> deprecated message ("" = clear)
	hasDeprecate bool
}

// attachment is one _attachments entry: the tarball bytes, base64 in JSON.
type attachment struct {
	contentType string
	data        []byte
}

// maxPublishBody bounds the publish document: the tarball rides inline as
// base64. 512MB of JSON covers every real-world package with room to spare
// while still bounding a hostile body.
const maxPublishBody = 512 << 20

// parsePublishBody decodes the request body (step 1).
func parsePublishBody(r *http.Request) (*publishBody, error) {
	var doc map[string]any
	dec := json.NewDecoder(io.LimitReader(r.Body, maxPublishBody))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	b := &publishBody{raw: doc, distTags: map[string]string{}, deprecated: map[string]string{}}
	b.name = stringOf(doc["name"])
	if b.name == "" {
		b.name = stringOf(doc["_id"])
	}
	for v, mv := range mapOf(doc["versions"]) {
		if m := mapOf(mv); m != nil {
			if b.versions == nil {
				b.versions = map[string]map[string]any{}
			}
			b.versions[v] = m
		}
	}
	for tag, v := range mapOf(doc["dist-tags"]) {
		if s := stringOf(v); s != "" {
			b.distTags[tag] = s
		}
	}
	for name, av := range mapOf(doc["_attachments"]) {
		a := mapOf(av)
		data, err := decodeAttachmentData(stringOf(a["data"]))
		if err != nil {
			return nil, fmt.Errorf("attachment %s: %w", name, err)
		}
		if b.attachments == nil {
			b.attachments = map[string]attachment{}
		}
		b.attachments[name] = attachment{contentType: stringOf(a["content_type"]), data: data}
	}
	for v, m := range b.versions {
		if dv, ok := m["deprecated"]; ok {
			b.hasDeprecate = true
			b.deprecated[v] = stringOf(dv)
		}
	}
	return b, nil
}

// decodeAttachmentData parses the base64 payload (standard spelling; the
// unpadded variant tolerated).
func decodeAttachmentData(s string) ([]byte, error) {
	if s == "" {
		return nil, errors.New("empty attachment data")
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

// firstAttachment returns the attachment npm's flow addresses (spec: "the
// first attachment"). Key order is the _attachments map's sorted keys for
// determinism; real clients send exactly one.
func (b *publishBody) firstAttachment() (attachment, bool) {
	keys := make([]string, 0, len(b.attachments))
	for k := range b.attachments {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return attachment{}, false
	}
	return b.attachments[keys[0]], true
}

// firstVersion returns the version this publish carries: the one a dist-tag
// points at when it exists in versions, else the greatest by semver (both
// reduce to "the only version" for real npm clients, which send one).
func (b *publishBody) firstVersion() (string, map[string]any, bool) {
	if len(b.versions) == 0 {
		return "", nil, false
	}
	target := ""
	tags := make([]string, 0, len(b.distTags))
	for _, v := range b.distTags {
		tags = append(tags, v)
	}
	sort.Strings(tags)
	for _, v := range tags {
		if _, ok := b.versions[v]; ok {
			target = v
			break
		}
	}
	if target == "" {
		for v := range b.versions {
			if target == "" || compareSemver(v, target) > 0 {
				target = v
			}
		}
	}
	m, ok := b.versions[target]
	return target, m, ok
}

// servePublish runs the ten-step chain of spec section 2.3.
func (h *Handler) servePublish(ctx context.Context, w http.ResponseWriter, r *http.Request, p *Principal,
	repoKey, name string) {
	// Step 1: body JSON parse.
	body, err := parsePublishBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid npm publish document: "+err.Error())
		return
	}

	// Step 2: attachments without versions.
	if len(body.attachments) > 0 && len(body.versions) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(msgMissingVers, name))
		return
	}

	att, hasAttachment := body.firstAttachment()
	version, manifest, hasVersion := body.firstVersion()

	if hasAttachment && hasVersion {
		h.publishWithTarball(ctx, w, p, repoKey, name, body, version, manifest, att)
		return
	}

	// Step 6: no attachments but a deprecated marker -> deprecate flow.
	if !hasAttachment && hasVersion && body.hasDeprecate {
		h.deprecate(ctx, w, p, repoKey, name, body)
		return
	}

	// M26 ruling (PRD FR-18-AC6): replaying a SERVED packument back at the
	// publish endpoint — attachments stripped by the GET face, every version
	// already stored — is the duplicate-publish conflict (403), not the
	// missing-attachment 400 of a genuinely new package. Evaluated after the
	// deprecate arm so `npm deprecate` (same shape plus deprecated markers)
	// keeps its 201.
	if !hasAttachment && h.publishConflict(ctx, w, p, repoKey, name, body) {
		return
	}

	// Step 7: no attachments and nothing to deprecate.
	writeError(w, http.StatusBadRequest, fmt.Sprintf(msgMissingAtt, name))
}

// publishConflict reports whether the attachment-less body re-declares an
// already-stored version, answering the pinned 403 when it does.
func (h *Handler) publishConflict(ctx context.Context, w http.ResponseWriter, p *Principal,
	repoKey, name string, body *publishBody) bool {
	doc, _, err := h.loadPackument(ctx, p, repoKey, name)
	if err != nil {
		return false // no stored packument: nothing can conflict
	}
	stored := versionsOf(doc)
	for v := range body.versions {
		if stored[v] != nil {
			writeError(w, http.StatusForbidden, fmt.Sprintf(msgCannotModify, v, name))
			return true
		}
	}
	return false
}

// publishWithTarball is steps 3..5 and 8..10 (the attachment path).
func (h *Handler) publishWithTarball(ctx context.Context, w http.ResponseWriter, p *Principal, repoKey, name string,
	body *publishBody, version string, manifest map[string]any, att attachment) {
	tb := tarballPath(name, version)

	// Step 3: write permission on the tarball path (the early authorizer;
	// repo.Service re-checks the same grant inside Put — defense in depth).
	if h.authz != nil && p != nil && !h.authz.Can(ctx, p, repoKey, tb, repo.ActionWrite) {
		writeError(w, http.StatusForbidden, fmt.Sprintf(msgCannotDeploy, tb))
		return
	}

	// Step 4: the version must be new (403 ruling, PRD v1.1 Q7).
	if h.tarballExists(ctx, p, repoKey, tb) {
		writeError(w, http.StatusForbidden, fmt.Sprintf(msgCannotModify, version, name))
		return
	}

	// Step 5: name and version spelling (semver, no leading zeros).
	if err := validatePackageName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := parseSemver(version); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(msgInvalidVer, version))
		return
	}

	// Step 8: integrity (sha512, base64 in the ssri spelling) vs the actual
	// tarball — enforced (BinFlow strictness ruling R8; Artifactory only
	// records it). The comparison is over DECODED bytes: ssri emits padded
	// standard base64, other tooling base64url, and either names the same
	// digest.
	_, sha256Sum, sha512Sum := digestTriple(att.data)
	if want := stringOf(distOf(manifest)["integrity"]); want != "" {
		if !integrityMatches(want, sha512Sum) {
			writeError(w, http.StatusBadRequest, msgIntegrity)
			return
		}
	}

	// Step 9: dist.shasum — handed to the storage client-checksums chain as
	// the declared sha1; a mismatch surfaces as 400 below.
	declared := storage.BlobRef{Sha256: hex.EncodeToString(sha256Sum)}
	if want := stringOf(distOf(manifest)["shasum"]); want != "" {
		declared.Sha1 = strings.ToLower(want)
	}

	if _, err := h.svc.Put(ctx, p, repoKey, tb, bytes.NewReader(att.data), declared, "application/octet-stream"); err != nil {
		if errors.Is(err, storage.ErrChecksumMismatch) {
			writeError(w, http.StatusBadRequest, msgSha1Conflict)
			return
		}
		if errors.Is(err, repo.ErrForbidden) {
			// The service's own write gate (the step-3 arm when no early
			// authorizer is wired).
			writeError(w, http.StatusForbidden, fmt.Sprintf(msgCannotDeploy, tb))
			return
		}
		h.writeServiceError(w, err)
		return
	}

	// Step 10: merge the packument and answer 201. The read-modify-write is
	// serialized (docMu) so concurrent publishes of different versions merge
	// instead of racing one away.
	h.docMu.Lock()
	defer h.docMu.Unlock()
	doc, _, err := h.loadPackument(ctx, p, repoKey, name)
	if err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
		h.writeServiceError(w, err)
		return
	}
	if doc == nil {
		doc = newPackument(name)
	}
	merged := mergePublish(doc, name, version, manifest, body.raw, body.distTags, h.clock)
	bumpRev(merged)
	if err := h.savePackument(ctx, p, repoKey, name, merged); err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSONBody(w, http.StatusCreated, map[string]any{"success": true})
}

// deprecate applies versions[v].deprecated markers (spec step 6; the npm 10
// deprecate command PUTs the full packument with no attachments).
func (h *Handler) deprecate(ctx context.Context, w http.ResponseWriter, p *Principal, repoKey, name string,
	body *publishBody) {
	h.docMu.Lock()
	defer h.docMu.Unlock()
	doc, _, err := h.loadPackument(ctx, p, repoKey, name)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	versions := mapOf(doc["versions"])
	for v, msg := range body.deprecated {
		m := mapOf(versions[v])
		if m == nil {
			continue
		}
		if msg == "" {
			delete(m, "deprecated")
		} else {
			m["deprecated"] = msg
		}
	}
	tm := mapOf(doc["time"])
	if tm != nil {
		tm["modified"] = h.clock()
	}
	bumpRev(doc)
	if err := h.savePackument(ctx, p, repoKey, name, doc); err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSONBody(w, http.StatusCreated, map[string]any{"ok": "updated package"})
}

// tarballExists probes the node namespace without the download audit a Get
// would emit: one prefix listing, exact-path match.
func (h *Handler) tarballExists(ctx context.Context, p *Principal, repoKey, path string) bool {
	nodes, err := h.svc.List(ctx, p, repoKey, path)
	if err != nil {
		return false
	}
	for _, n := range nodes {
		if n.Path == path {
			return true
		}
	}
	return false
}

// digestTriple computes the three digests of one buffer (publish bodies are
// already in memory; there is nothing to stream).
func digestTriple(b []byte) (sha1Sum, sha256Sum, sha512Sum []byte) {
	s1 := sha1.Sum(b) //nolint:gosec // G401: dist.shasum is the npm protocol checksum
	s256 := sha256.Sum256(b)
	s512 := sha512.Sum512(b)
	return s1[:], s256[:], s512[:]
}

// integrityMatches compares an "sha512-<base64>" integrity reference against
// the actual digest bytes. ssri (the npm client) emits PADDED STANDARD
// base64; base64url spellings decode to the same bytes and compare equal —
// the algorithm prefix must be sha512 (a mismatched algorithm is a mismatch,
// not an error to re-derive).
func integrityMatches(ref string, sha512Sum []byte) bool {
	algo, encoded, found := strings.Cut(ref, "-")
	if !found || algo != "sha512" || encoded == "" {
		return false
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		if decoded, err := enc.DecodeString(encoded); err == nil {
			return bytes.Equal(decoded, sha512Sum)
		}
	}
	return false
}
