package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/webhook"
)

// The ?properties read/write family (M10 T-286, FR-89.2 / architecture
// section 15.3.3 — the E-09 gap-endpoint's redemption). Three verbs hang
// off the EXISTING /api/storage/{repo}/{path} route as a new query arm,
// the ?list/?permissions family's third member; a request without the
// parameter keeps the frozen item-info behavior byte for byte.
//
// Contract (section 15.3.3's table + rest-api.md section 3's high-
// confidence rows; the merge law and empty-hit 200 are BinFlow's own
// pending-calibration rulings — decisions 11.40 and the table's "无命中 =
// {}", flipping the reference 404 "No properties could be found."):
//
//	GET    …?properties=k1,k2*[&atomic=true]  read gate (content plane,
//	                                          anonymous follows the flag)
//	PUT    …?properties=k=v[,v2…][&recursive=1]  required + annotate on path
//	DELETE …?properties=k1,k2*|*[&recursive=1]  required + annotate on path
//
// Multi-value and multi-key share ONE comma grammar on the raw query
// value: segments split on RAW commas; a segment containing '=' opens a
// key, a segment without '=' continues the previous key's value set
// ("qa=passed,owner=team-a" -> two keys; "k=v1,v2" -> one key, two
// values). Percent-encoded commas (%2C) survive as value content because
// the split runs on the RAW query string before decoding — the encoding
// semantics section 15.3.3 pins for tech-writer documentation.
//
// Semicolons are NOT part of this plane's grammar (T-447's contract note,
// pinned as wire behavior by T-493 / FR-157④): the ';' matrix spelling is
// a PATH-plane deploy concern only. An encoded %3B inside a value is plain
// content; a RAW ';' in the raw query value additionally drops the whole
// pair out of net/url's query parsing (Go refuses ';' as a separator), so
// the arm never engages for that spelling — GET answers the plain item
// body, the mutating verbs the family's unknown-spelling 404.
//
// Permissions: reads ride the item-info gate (the service's own read ACL,
// storageNode); writes demand the path's `a` (annotate) through the SAME
// Authorizer the content plane consults — the M16 verb split (T-444 /
// ADR-0044 K68): the property-write face is the annotate action, no longer
// the write action's shadow. Properties are metadata, not content, so the
// overwrite family's `d` demand never applies (section 15.3.3), and
// annotate opens no upload face either.

// propsAuditWrite and propsAuditDelete are the audit actions of the
// property write family (section 15.3.3: "props.write/props.delete").
const (
	propsAuditWrite  = "props.write"
	propsAuditDelete = "props.delete"
)

// propertiesView is the GET body: the uri-addressed node's property map,
// filtered or whole. The wrapper key matches section 15.3.3's table shape
// ({"properties":{…}}); an empty hit answers 200 with an empty map — the
// BinFlow divergence from the reference 404, registered in decision
// 11.40's neighborhood and the T-286 log.
type propertiesView struct {
	Properties map[string][]string `json:"properties"`
}

// handleStoragePropertiesGet serves GET /api/storage/{repo}/{path}?properties
// (FR-89.2): the node's properties, optionally filtered. Filters are a
// comma list on the raw value; a trailing '*' on a filter is a key-prefix
// wildcard. An absent/empty parameter lists every key. atomic=true turns
// any missing LITERAL filter key into the 404 the PRD pins (FR-89.2) —
// wildcard filters never trigger it (they can only match, not miss).
func (s *Server) handleStoragePropertiesGet(w http.ResponseWriter, r *http.Request, repoKey, relPath string) {
	p := principalFrom(r.Context())
	node, err := s.storageNode(r, p, repoKey, relPath)
	if err != nil {
		s.writeStorageError(w, err)
		return
	}
	all, err := s.deps.Metadata.NodeProps().List(r.Context(), node.RepoKey, node.Path)
	if err != nil {
		s.log.ErrorContext(r.Context(), "httpapi: properties list failed",
			"repo", node.RepoKey, "path", node.Path, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "properties query failed")
		return
	}

	raw := rawQueryValue(r, "properties")
	if raw == "" {
		// No filter: the whole key set (the table's "无 filter = 全量键").
		writeJSONBody(w, http.StatusOK, propertiesView{Properties: all})
		return
	}

	out := map[string][]string{}
	for _, f := range splitRawFilter(raw) {
		if prefix, ok := strings.CutSuffix(f, "*"); ok && prefix != "" {
			// Wildcard: prefix-match keys, lexicographic for determinism.
			for k, vs := range all {
				if strings.HasPrefix(k, prefix) {
					out[k] = vs
				}
			}
			continue
		}
		if f == "*" {
			for k, vs := range all {
				out[k] = vs
			}
			continue
		}
		vs, ok := all[f]
		if !ok && isTruthyFlag(rawQueryValue(r, "atomic")) {
			// FR-89.2: atomic=true — any named key missing answers the 404.
			writeError(w, http.StatusNotFound,
				"Property '"+f+"' was not found on '"+repoKey+"/"+node.Path+"'.")
			return
		}
		if ok {
			out[f] = vs
		}
	}
	writeJSONBody(w, http.StatusOK, propertiesView{Properties: out})
}

// handleStoragePropertiesPut serves PUT /api/storage/{repo}/{path}?properties=k=v…
// (FR-89.2): the merge write — same-key value-set replace, other keys kept
// (decision 11.40's own semantics, isolated in this handler as the flip
// point). The node must exist (404); folder targets are legal property
// carriers (section 15.3.2); recursive=1 applies the merge to the folder
// row AND every node under it. Success is 204 with no body.
func (s *Server) handleStoragePropertiesPut(w http.ResponseWriter, r *http.Request, repoKey, relPath string) {
	p := principalFrom(r.Context())
	node, err := s.storageNode(r, p, repoKey, relPath)
	if err != nil {
		s.writeStorageError(w, err)
		return
	}
	if !s.allowPropsWrite(r, p, node.RepoKey, node.Path) {
		writePropsForbidden(w)
		return
	}

	props, err := parsePropertiesQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(props) == 0 {
		// The reference plane's refusal for an empty write set (rest-api.md
		// section 3, high confidence): PUT with no parseable pair writes
		// nothing and says so.
		writeError(w, http.StatusBadRequest, "Unspecified properties to set.")
		return
	}

	targets, err := s.propsTargets(r, node, isTruthyFlag(rawQueryValue(r, "recursive")))
	if err != nil {
		s.writePropsStoreError(w, err)
		return
	}
	for _, t := range targets {
		// Merge-view caps: the WRITE set is validated first, then the
		// post-merge cardinality (the merge law replaces same-key sets, so
		// the only cross-check the store cannot do is the key COUNT):
		// every written key plus every surviving existing key the write
		// does not name. (T-297 finding: this loop used to walk props
		// counting fresh keys on top of len(props) — double-counting the
		// write set, so a fresh node refused anything over 32 keys in one
		// PUT while claiming "more than 64".)
		merged := len(props)
		existing, err := s.deps.Metadata.NodeProps().List(r.Context(), t.repo, t.path)
		if err != nil {
			s.writePropsStoreError(w, err)
			return
		}
		for k := range existing {
			if _, ok := props[k]; !ok {
				merged++
			}
		}
		if merged > metadata.MaxNodePropKeys {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"more than %d property keys on one node", metadata.MaxNodePropKeys))
			return
		}
		if err := s.deps.Metadata.NodeProps().Merge(r.Context(), t.repo, t.path, props); err != nil {
			s.writePropsStoreError(w, err)
			return
		}
	}
	s.recordPropsAudit(r, propsAuditWrite, node.RepoKey, node.Path, propsQuerySummary(props), len(targets) > 1)
	// Webhook seam: artifact_property/added per written key on the
	// ADDRESSED node (webhook.md 3.2 — the envelope's node facts come from
	// the addressed row; recursive fan-out targets fire nothing here, the
	// addressed node's event is the operation's observable).
	for k, vs := range props {
		s.emitWebhook(r, webhook.Event{
			Domain: webhook.DomainArtifactProperty, Type: webhook.TypePropAdded,
			Repo: node.RepoKey, Path: node.Path, Sha256: node.Sha256, Size: node.Size,
			PropertyKey: k, PropertyValues: vs,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleStoragePropertiesDelete serves DELETE
// /api/storage/{repo}/{path}?properties=k1,k2 (FR-89.2): drops the named
// keys — `properties=*` drops every key — idempotently (204 whether or
// not the node carried them). recursive mirrors PUT. An empty parameter is
// the reference plane's explicit 400 (rest-api.md section 3, high
// confidence: "Unspecified properties to delete.").
func (s *Server) handleStoragePropertiesDelete(w http.ResponseWriter, r *http.Request, repoKey, relPath string) {
	p := principalFrom(r.Context())
	node, err := s.storageNode(r, p, repoKey, relPath)
	if err != nil {
		s.writeStorageError(w, err)
		return
	}
	if !s.allowPropsWrite(r, p, node.RepoKey, node.Path) {
		writePropsForbidden(w)
		return
	}

	raw := rawQueryValue(r, "properties")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "Unspecified properties to delete.")
		return
	}
	var keys []string
	all := false
	for _, f := range splitRawFilter(raw) {
		if f == "*" {
			all = true
			continue
		}
		if prefix, ok := strings.CutSuffix(f, "*"); ok && prefix != "" {
			// Wildcard delete resolves against the node's live key set —
			// the store's Delete is name-exact.
			live, err := s.deps.Metadata.NodeProps().List(r.Context(), node.RepoKey, node.Path)
			if err != nil {
				s.writePropsStoreError(w, err)
				return
			}
			for k := range live {
				if strings.HasPrefix(k, prefix) {
					keys = append(keys, k)
				}
			}
			continue
		}
		keys = append(keys, f)
	}
	for _, k := range keys {
		if err := metadata.ValidatePropKey(k); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	targets, err := s.propsTargets(r, node, isTruthyFlag(rawQueryValue(r, "recursive")))
	if err != nil {
		s.writePropsStoreError(w, err)
		return
	}
	for _, t := range targets {
		drop := keys
		if all {
			drop = nil // nil = every key (the store's properties=* form)
		}
		if err := s.deps.Metadata.NodeProps().Delete(r.Context(), t.repo, t.path, drop); err != nil {
			s.writePropsStoreError(w, err)
			return
		}
	}
	s.recordPropsAudit(r, propsAuditDelete, node.RepoKey, node.Path, raw, len(targets) > 1)
	// Webhook seam: artifact_property/deleted per named key (the wildcard
	// arm fires nothing — its key set was never materialized; property_values
	// echoes empty: the values are gone by definition of the event).
	for _, k := range keys {
		s.emitWebhook(r, webhook.Event{
			Domain: webhook.DomainArtifactProperty, Type: webhook.TypePropDeleted,
			Repo: node.RepoKey, Path: node.Path, Sha256: node.Sha256, Size: node.Size,
			PropertyKey: k,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- shared plumbing ----

// propsTarget is one node a recursive property write applies to.
type propsTarget struct {
	repo string
	path string
}

// propsTargets resolves the write's target set: the addressed node alone,
// or — folder target plus recursive=1 — the folder row and every node
// under its prefix (section 15.3.3: "folder + recursive=1 递归应用"; the
// folder row itself carries properties per section 15.3.2). The node walk
// rides the metadata prefix listing directly: the caller already passed
// the write gate on the addressed path, and the path-matcher semantics of
// `a` make the subtree the same grant's domain.
func (s *Server) propsTargets(r *http.Request, node *metadata.Node, recursive bool) ([]propsTarget, error) {
	if !recursive || !isFolderPath(node.Path) {
		return []propsTarget{{repo: node.RepoKey, path: node.Path}}, nil
	}
	dir := strings.TrimSuffix(node.Path, "/")
	nodes, err := s.deps.Metadata.Nodes().ListByPrefix(r.Context(), node.RepoKey, dir)
	if err != nil {
		return nil, fmt.Errorf("list %s/%s: %w", node.RepoKey, dir, err)
	}
	out := make([]propsTarget, 0, len(nodes)+1)
	out = append(out, propsTarget{repo: node.RepoKey, path: node.Path})
	for _, n := range nodes {
		if n.Path == node.Path {
			continue // the folder row itself is already first
		}
		out = append(out, propsTarget{repo: n.RepoKey, path: n.Path})
	}
	return out, nil
}

// allowPropsWrite is the write gate of both mutating verbs: the `a`
// (annotate) action on the addressed path, through the same Authorizer
// every other content decision consults. M16 (T-444, ADR-0044 K68 /
// architecture section 25.6) flipped this single point from `w` to `a` —
// the property-write face is its own permission now, backfilled onto every
// pre-split write grant by migration 023 (zero-privilege equivalence).
// Still no `d` demand — properties are metadata, and the section 15.3.3
// table deliberately decouples the property write from the overwrite-check
// family — and no content-byte face either: uploads keep gating on `w`
// elsewhere.
func (s *Server) allowPropsWrite(r *http.Request, p *auth.Principal, repoKey, path string) bool {
	return s.deps.Authz.Can(r.Context(), p, repoKey, path, auth.ActionAnnotate)
}

// writePropsForbidden renders the properties family's 403 (an
// authenticated caller without `a`; the route already answered the
// anonymous 401 challenge).
func writePropsForbidden(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "permission denied: writing properties requires annotate access on the item")
}

// writePropsStoreError maps a property store failure: busy-class answers
// the retryable 503, everything else an honest 500.
func (s *Server) writePropsStoreError(w http.ResponseWriter, err error) {
	if metadata.IsStoreBusy(err) {
		writeError(w, http.StatusServiceUnavailable, "storage is busy, retry shortly")
		return
	}
	s.log.Error("httpapi: properties store failure", "error", err.Error())
	writeError(w, http.StatusInternalServerError, "properties operation failed")
}

// recordPropsAudit appends the family's audit row (best-effort, the
// platform rule): repo/path of the addressed node, the raw key list in the
// detail, and the recursive mark when the write fanned out.
func (s *Server) recordPropsAudit(r *http.Request, action, repoKey, path, keys string, recursive bool) {
	detail, err := json.Marshal(map[string]any{"keys": keys, "recursive": recursive})
	if err != nil {
		detail = []byte("{}") // unreachable: flat scalar map
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actorName(r),
		Action: action,
		Repo:   repoKey,
		Path:   path,
		Detail: string(detail),
	})
}

// propsQuerySummary renders a write set as the deterministic "k=v,k2=v2"
// digest the audit detail carries (sorted keys, sorted values).
func propsQuerySummary(props map[string][]string) string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		vs := append([]string(nil), props[k]...)
		sort.Strings(vs)
		parts = append(parts, k+"="+strings.Join(vs, ","))
	}
	return strings.Join(parts, ",")
}

// parsePropertiesQuery parses the PUT arm's properties value with the
// comma grammar (see the file comment): RAW commas separate segments; a
// segment with '=' opens a key (first '=' wins — later ones are value
// content), a segment without '=' appends to the previous key's value
// set. Values are query-unescaped AFTER the split, so %2C survives as
// content. A continuation with no preceding key, an empty key, and every
// closed-rule violation (metadata.ValidateProp*) answer 400s.
func parsePropertiesQuery(r *http.Request) (map[string][]string, error) {
	raw := rawQueryValue(r, "properties")
	if raw == "" {
		return nil, nil
	}
	props := map[string][]string{}
	last := ""
	for _, seg := range splitRawFilter(raw) {
		decoded, derr := url.QueryUnescape(seg)
		if derr != nil {
			return nil, fmt.Errorf("malformed percent-encoding in properties segment %q: %w", seg, derr)
		}
		if key, value, found := strings.Cut(decoded, "="); found {
			if key == "" {
				return nil, errors.New("properties segment has an empty key")
			}
			if err := metadata.ValidatePropKey(key); err != nil {
				return nil, err
			}
			if err := metadata.ValidatePropValue(key, value); err != nil {
				return nil, err
			}
			props[key] = append(props[key], value)
			last = key
			continue
		}
		if last == "" {
			return nil, fmt.Errorf("properties segment %q continues no key (no '=' seen yet)", decoded)
		}
		if err := metadata.ValidatePropValue(last, decoded); err != nil {
			return nil, err
		}
		props[last] = append(props[last], decoded)
	}
	if err := metadata.ValidatePropSet(props); err != nil {
		return nil, err
	}
	return props, nil
}

// splitRawFilter splits a raw query VALUE on unencoded commas: the
// separator grammar runs before decoding so %2C stays value content (the
// encode-then-split law section 15.3.3 pins).
func splitRawFilter(raw string) []string {
	return strings.Split(raw, ",")
}

// rawQueryValue returns one parameter's RAW (still-encoded) value off the
// query string; url.Values would have decoded it, erasing the raw/encoded
// comma distinction the grammar needs.
func rawQueryValue(r *http.Request, name string) string {
	for _, part := range strings.Split(r.URL.RawQuery, "&") {
		k, v, _ := strings.Cut(part, "=")
		if k == name {
			return v
		}
	}
	return ""
}

// isTruthyFlag reads a boolean-ish flag the family accepts ("1", "true")
// — the PRD's recursive=1 spelling and the atomic=true spelling both land
// here; absent or any other value means false.
func isTruthyFlag(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true":
		return true
	}
	return false
}
