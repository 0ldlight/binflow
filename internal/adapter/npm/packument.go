package npm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The packument is the npm package document ([NPM-API] package-metadata):
// name / dist-tags / versions / time / _id / _rev, versions[v].dist carrying
// {tarball, shasum, integrity}. It is stored as ONE node at
// <pkg>/packument.json (architecture section 5.4.2) and every derived view
// (tarball URL rewrite, SLIM negotiation, _attachments stripping) is rendered
// at SERVE time — the stored bytes stay protocol-neutral.
//
// Representation: map[string]any decoded with UseNumber so unknown fields and
// large numeric literals round-trip byte-stably (architecture section 5.4.2:
// BinFlow does not validate the whole document, unknown fields are preserved
// for client compatibility). Go's map marshal emits sorted keys, which makes
// the stored bytes deterministic for a given state.

// nowClock is the RFC3339 UTC timestamp source (injectable in tests).
type nowClock func() string

// loadPackument reads the stored document node. Missing node maps to
// repo.ErrNodeNotFound; the caller decides 404-vs-fresh.
func (h *Handler) loadPackument(ctx context.Context, p *Principal, repoKey, name string) (map[string]any, *metadata.Node, error) {
	rc, node, err := h.svc.Get(ctx, p, repoKey, packumentPath(name))
	if err != nil {
		return nil, nil, err
	}
	defer rc.Close() //nolint:errcheck // read-only fd
	raw, err := io.ReadAll(rc)
	if err != nil {
		return nil, nil, fmt.Errorf("read packument %s/%s: %w", repoKey, name, err)
	}
	doc, err := decodeDoc(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("parse packument %s/%s: %w", repoKey, name, err)
	}
	return doc, node, nil
}

// savePackument marshals and stores the document node.
func (h *Handler) savePackument(ctx context.Context, p *Principal, repoKey, name string, doc map[string]any) error {
	body, err := encodeDoc(doc)
	if err != nil {
		return fmt.Errorf("encode packument %s/%s: %w", repoKey, name, err)
	}
	if _, err := h.svc.Put(ctx, p, repoKey, packumentPath(name), bytes.NewReader(body),
		storage.BlobRef{}, "application/json"); err != nil {
		return err
	}
	return nil
}

// decodeDoc parses one packument document, keeping numbers literal.
func decodeDoc(raw []byte) (map[string]any, error) {
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// encodeDoc renders one packument deterministically (sorted map keys).
func encodeDoc(doc map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---- map accessors (nil-safe; wrong-typed fields read as absent) ----

func mapOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

// distTagsOf returns the doc's dist-tags as string map (never nil).
func distTagsOf(doc map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range mapOf(doc["dist-tags"]) {
		if s := stringOf(v); s != "" {
			out[k] = s
		}
	}
	return out
}

// versionsOf returns the doc's versions (never nil).
func versionsOf(doc map[string]any) map[string]any {
	if m := mapOf(doc["versions"]); m != nil {
		return m
	}
	return map[string]any{}
}

// distOf returns the dist object of one version manifest.
func distOf(version map[string]any) map[string]any { return mapOf(version["dist"]) }

// versionDistTarball returns the stored tarball reference of one version:
// the path encoded in dist.tarball when it points inside this registry, else
// the canonical layout path (spec section 2.4 resolution order, simplified —
// BinFlow always stores the canonical relative path at publish, so the
// fallback is the rule).
func versionDistTarball(name, version string, dist map[string]any) string {
	if u := stringOf(dist["tarball"]); u != "" {
		if rel, ok := relativeTarballPath(u); ok {
			return rel
		}
	}
	return tarballPath(name, version)
}

// relativeTarballPath reduces an absolute tarball URL to the repo-relative
// path when it points at any BinFlow mount of this shape:
// .../binflow/api/npm/<repo>/<rel> or .../binflow/<repo>/<rel>.
func relativeTarballPath(u string) (string, bool) {
	pu, err := url.Parse(u)
	if err != nil {
		return "", false
	}
	p := pu.Path
	for _, head := range []string{"/binflow/api/npm/", "/binflow/"} {
		if i := strings.Index(p, head); i >= 0 {
			rest := p[i+len(head):]
			if _, tail, found := strings.Cut(rest, "/"); found && tail != "" {
				if rel, err := url.PathUnescape(tail); err == nil {
					return rel, true
				}
			}
		}
	}
	return "", false
}

// ---- document transitions ----

// newPackument is the empty document shell of a package.
func newPackument(name string) map[string]any {
	return map[string]any{
		"_id": name, "name": name,
		"dist-tags": map[string]any{},
		"versions":  map[string]any{},
		"time":      map[string]any{},
	}
}

// topLevelKeep is the publish document's root-level passthrough: fields the
// npm client sends once per publish that the packument must keep (README and
// friends — the SLIM negotiation's whole point is dropping them at serve
// time, not at store time).
var topLevelKeep = []string{
	"readme", "description", "homepage", "repository", "bugs", "license",
	"keywords", "maintainers", "author", "contributors", "access",
}

// mergePublish folds one published version into the document: the manifest
// (dist normalized to the canonical tarball path), the request's dist-tags,
// the time bookkeeping and the root-level passthrough. Returns the merged
// document.
func mergePublish(doc map[string]any, name, version string, manifest, root map[string]any,
	tags map[string]string, now nowClock) map[string]any {
	if doc == nil {
		doc = newPackument(name)
	}
	for _, k := range topLevelKeep {
		if v, ok := root[k]; ok && v != nil {
			doc[k] = v
		}
	}
	normalized := copyManifest(manifest)
	normalized["_id"] = name + "@" + version
	dist := distOf(normalized)
	if dist == nil {
		dist = map[string]any{}
	}
	dist["tarball"] = tarballPath(name, version)
	normalized["dist"] = dist

	versions := mapOf(doc["versions"])
	if versions == nil {
		versions = map[string]any{}
	}
	versions[version] = normalized
	doc["versions"] = versions

	tm := mapOf(doc["time"])
	if tm == nil {
		tm = map[string]any{}
	}
	if _, ok := tm["created"]; !ok {
		tm["created"] = now()
	}
	tm[version] = now()
	tm["modified"] = now()
	doc["time"] = tm

	tagsM := mapOf(doc["dist-tags"])
	if tagsM == nil {
		tagsM = map[string]any{}
	}
	for tag, v := range tags {
		tagsM[tag] = v
	}
	doc["dist-tags"] = tagsM
	return doc
}

// applyRevDocument replaces the document state from a client-supplied full
// packument (the npm 10 unpublish/deprecate PUT spelling): versions and
// dist-tags come from the client, the service-owned identity fields stay,
// and the rev bumps. npm's own client already removed the unpublished
// version and its dist-tags before sending, so the replacement IS the merge.
// Version dist.tarball references normalize to BinFlow-relative paths — the
// client echoes the RENDERED URLs, and the stored document stays
// protocol-neutral.
func applyRevDocument(doc map[string]any, body map[string]any, name string, now nowClock) map[string]any {
	if doc == nil {
		return nil
	}
	out := copyDoc(doc)
	if versions := mapOf(body["versions"]); versions != nil {
		clean := map[string]any{}
		for v, mv := range versions {
			m := mapOf(mv)
			if m == nil {
				clean[v] = mv
				continue
			}
			cm := copyManifest(m)
			if dist := distOf(cm); dist != nil {
				dist["tarball"] = versionDistTarball(name, v, dist)
			}
			clean[v] = cm
		}
		out["versions"] = clean
	}
	if tags := mapOf(body["dist-tags"]); tags != nil {
		out["dist-tags"] = tags
	}
	if tm := mapOf(body["time"]); tm != nil {
		out["time"] = tm
	}
	for _, k := range topLevelKeep {
		if v, ok := body[k]; ok && body[k] != nil {
			out[k] = v
		}
	}
	out["time"] = withModified(mapOf(out["time"]), now())
	return out
}

// withModified stamps time.modified.
func withModified(tm map[string]any, now string) map[string]any {
	if tm == nil {
		tm = map[string]any{}
	}
	tm["modified"] = now
	return tm
}

// removeVersion deletes one version, drops dist-tags pointing at it, and
// repoints "latest" at the greatest remaining version (the npm 10 client
// computes the same result before its own PUT — the DELETE path arriving
// here did NOT pass through that PUT reproduces it server-side).
func removeVersion(doc map[string]any, version string, now nowClock) bool {
	versions := mapOf(doc["versions"])
	if versions == nil || versions[version] == nil {
		return false
	}
	delete(versions, version)
	tags := mapOf(doc["dist-tags"])
	var latestWas string
	if tags != nil {
		latestWas = stringOf(tags["latest"])
		for tag, v := range tags {
			if stringOf(v) == version {
				delete(tags, tag)
			}
		}
		if latestWas == version {
			if next := latestVersion(versions); next != "" {
				tags["latest"] = next
			}
		}
	}
	tm := mapOf(doc["time"])
	if tm != nil {
		delete(tm, version)
		tm["modified"] = now()
	}
	return true
}

// bumpRev advances the couch-style revision counter ("<n>-<hex>"). npm treats
// the value as an opaque token it echoes back; a monotonic counter keeps
// sequential PUTs observable in tests.
func bumpRev(doc map[string]any) {
	n := int64(1)
	if prev := stringOf(doc["_rev"]); prev != "" {
		if i := strings.IndexByte(prev, '-'); i > 0 {
			if v, err := strconv.ParseInt(prev[:i], 10, 64); err == nil {
				n = v + 1
			}
		}
	}
	var b [8]byte
	_, _ = rand.Read(b[:]) //nolint:gosec // rev nonce, not a secret
	doc["_rev"] = fmt.Sprintf("%d-%s", n, hex.EncodeToString(b[:]))
}

// copyDoc deep-copies via encode/decode (documents are small; the JSON
// round-trip preserves json.Number fidelity).
func copyDoc(doc map[string]any) map[string]any {
	raw, err := encodeDoc(doc)
	if err != nil {
		return doc
	}
	out, err := decodeDoc(raw)
	if err != nil {
		return doc
	}
	return out
}

// copyManifest copies one version manifest (same round-trip strategy).
func copyManifest(m map[string]any) map[string]any { return copyDoc(m) }

// ---- serve-time rendering ----

// renderPackument builds the wire body: _attachments stripped (never stored
// anyway — belt and braces for documents relayed by remote proxies), every
// dist.tarball rewritten to this registry's /api/npm mount (spec section
// 2.4: unconditionally rewritten — a stored or relayed absolute URL reduces
// to its BinFlow-relative path first, the canonical layout path when it
// cannot), and — under the SLIM Accept — reduced to the installer-only
// whitelist with the negotiated content type.
func renderPackument(doc map[string]any, name, scheme, host, baseURL, repoKey string, slim bool) ([]byte, string, error) {
	out := copyDoc(doc)
	delete(out, "_attachments")
	prefix := packumentURLPrefix(scheme, host, baseURL, repoKey)
	for v, mv := range versionsOf(out) {
		m := mapOf(mv)
		if m == nil {
			continue
		}
		dist := distOf(m)
		if dist == nil {
			continue
		}
		rel := versionDistTarball(name, v, dist)
		dist["tarball"] = prefix + escapePathSegments(rel)
	}
	ct := "application/json"
	if slim {
		out = slimPackument(out)
		ct = contentTypeSLIM
	}
	body, err := encodeDoc(out)
	return body, ct, err
}

// packumentURLPrefix is "<base>/binflow/api/npm/<repoKey>/" with base from
// the configured BaseURL or the request (X-Forwarded-Proto honored — the
// docker realm precedent).
func packumentURLPrefix(scheme, host, baseURL, repoKey string) string {
	base := baseURL
	if base == "" {
		base = scheme + "://" + host
	}
	return strings.TrimSuffix(base, "/") + "/binflow/api/npm/" + repoKey + "/"
}

// escapePathSegments percent-encodes each path segment of rel, keeping the
// "/" separators: scoped tarball paths carry their scope as literal segments
// (registry.npmjs.org spells them the same way).
func escapePathSegments(rel string) string {
	segs := strings.Split(rel, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// slimTopKeep/slimVersionKeep are the SLIM ("corgi") whitelists: everything
// an installer resolves dependencies with; README and human-facing metadata
// dropped (npm ci never reads them).
var (
	slimTopKeep = map[string]bool{
		"_id": true, "name": true, "dist-tags": true, "versions": true,
		"access": true, "modified": true,
	}
	slimVersionKeep = map[string]bool{
		"_id": true, "name": true, "version": true,
		"dependencies": true, "devDependencies": true, "optionalDependencies": true,
		"peerDependencies": true, "peerDependenciesMeta": true,
		"bundleDependencies": true, "bundledDependencies": true,
		"bin": true, "directories": true, "engines": true,
		"os": true, "cpu": true, "libc": true, "funding": true,
		"deprecated": true, "dist": true, "_hasShrinkwrap": true,
		"workspaces": true,
	}
)

// slimPackument reduces a rendered document to the SLIM shape.
func slimPackument(doc map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range doc {
		if slimTopKeep[k] {
			out[k] = v
		}
	}
	versions := map[string]any{}
	for ver, v := range versionsOf(doc) {
		m := mapOf(v)
		if m == nil {
			continue
		}
		red := map[string]any{}
		for k, fv := range m {
			if slimVersionKeep[k] {
				red[k] = fv
			}
		}
		versions[ver] = red
	}
	out["versions"] = versions
	return out
}

// clockUTC is the default RFC3339 UTC timestamp source.
func clockUTC() string { return time.Now().UTC().Format(time.RFC3339) }
