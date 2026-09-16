package httpapi

// The /api/metadata incremental property face (L024-8 / D01-R08, the spec
// rest-api.md section 3.1, live-calibrated by L024-1 against the reference
// 7.161.15): the CURRENT "Update Item Properties" form. The 6.1.0-era
// migration moved the write off POST /api/storage (that verb is the bare
// 405 now, the storage case in router.go) onto:
//
//	PATCH  /api/metadata/{repoKey}/{path}?recursiveProperties=   body
//	       {"props":{"k":"v"|"k":["v1",…]|"k":null}} — the incremental
//	       write: string and string-array values REPLACE the key's set
//	       (the store's merge law is the spec's delete-then-set), null
//	       DROPS the key (idempotently).
//	DELETE /api/metadata/{repoKey}/{path}?recursive=             drop
//	       EVERY property of the target (204 even when none were there).
//
// The stats leg ({"stats":{…}}) MERGES (L024-11 / diff T4, live-pinned):
// numeric fields set absolutely, the "import" marker lands on
// last_downloaded_by — the import/migration channel the reference's own
// wire showed (downloadCount:1 → count 1, by "import", the rest 0).
//
// The write gate is the properties family's own `a` (annotate) action —
// the same Authorizer split the /api/storage verbs ride (T-444/K68) —
// with the PATCH arm's own 403 wording (item 11) running BEFORE the body
// and target questions, the reference authorization filter's order.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/webhook"
)

// patchPropsParseMsg is section 3.1 item 5's verbatim 400 body cause (the
// trailing period included — live letter for letter): a props value that
// is neither a string nor an array failed the reference's body parse.
const patchPropsParseMsg = "Failed to parse json object while performing patch properties request."

// patchBodyLimit bounds the JSON body read (a trust-boundary cap: the face
// is a metadata write, not an upload plane; an oversized body fails the
// parse and answers the same 400).
const patchBodyLimit = 1 << 20

// handleMetadataPatch serves PATCH /api/metadata/{repo}/{path}: the
// incremental property write. Item 9's execution order (deletes, then
// modifies, then adds) rides one store pass per target — the drop keys go
// out through Delete first, the written keys through Merge second (its
// same-key replace IS the delete-then-set of item 2).
func (s *Server) handleMetadataPatch(w http.ResponseWriter, r *http.Request, repoKey, relPath string) {
	p := principalFrom(r.Context())
	// Item 11: the annotate gate precedes the body and the target (the
	// reference's authorization filter order), carrying the face's own 403
	// wording — not the generic family 403.
	if !s.allowPropsWrite(r, p, repoKey, relPath) {
		writeError(w, http.StatusForbidden, fmt.Sprintf(
			"Request for '%s:%s' is forbidden for user: '%s', You must have annotate permission on this path",
			repoKey, relPath, p.Name))
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, patchBodyLimit))
	if err != nil {
		writeError(w, http.StatusBadRequest, metadataSetWrap(repoKey, relPath, patchPropsParseMsg))
		return
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		writeError(w, http.StatusBadRequest, metadataSetWrap(repoKey, relPath, patchPropsParseMsg))
		return
	}
	propsRaw, hasProps := top["props"]
	statsRaw, hasStats := top["stats"]
	nullish := func(raw json.RawMessage) bool { return strings.TrimSpace(string(raw)) == "null" }
	propsThere, statsThere := hasProps && !nullish(propsRaw), hasStats && !nullish(statsRaw)
	if !propsThere && !statsThere {
		// Item 4, verbatim: neither field (or a null one) carries a request.
		writeError(w, http.StatusBadRequest, "props or stats fields required")
		return
	}

	var set map[string][]string // non-null props: key -> replacement value set
	var drop []string           // null props: keys to remove
	if propsThere {
		set, drop, err = parsePatchProps(repoKey, relPath, propsRaw)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	var statsMerge metadata.StatsMerge
	if statsThere {
		// L024-11 / diff T4: the leg MERGES (the low-confidence no-op is
		// voided by the differential): numeric fields set absolutely, and
		// the "import" marker lands on last_downloaded_by when the body did
		// not name one (the wire's downloadCount:1 → lastDownloadedBy:
		// "import").
		var statsObj map[string]json.RawMessage
		if err := json.Unmarshal(statsRaw, &statsObj); err != nil {
			writeError(w, http.StatusBadRequest, patchPropsParseMsg)
			return
		}
		num := func(key string) *int64 {
			raw, ok := statsObj[key]
			if !ok {
				return nil
			}
			var n int64
			if json.Unmarshal(raw, &n) != nil {
				return nil // non-numeric spellings skip silently (item 7's law)
			}
			return &n
		}
		statsMerge = metadata.StatsMerge{
			DownloadCount:       num("downloadCount"),
			RemoteDownloadCount: num("remoteDownloadCount"),
		}
		if raw, ok := statsObj["lastDownloadedBy"]; ok {
			var by string
			if json.Unmarshal(raw, &by) == nil && by != "" {
				statsMerge.By = by
			}
		}
		if statsMerge.By == "" && (statsMerge.DownloadCount != nil || statsMerge.RemoteDownloadCount != nil) {
			statsMerge.By = "import"
		}
	}

	if set != nil || drop != nil || statsMerge != (metadata.StatsMerge{}) {
		node, ok := s.metadataPatchTarget(w, r, repoKey, relPath)
		if !ok {
			return
		}
		if (statsMerge != metadata.StatsMerge{}) {
			if err := s.deps.Metadata.Nodes().MergeStats(r.Context(), node.RepoKey, node.Path, statsMerge); err != nil {
				s.writePropsStoreError(w, err)
				return
			}
		}
		recursive := metadataRecursiveFlag(r, node, "recursiveProperties")
		targets, err := s.propsTargets(r, node, recursive)
		if err != nil {
			s.writePropsStoreError(w, err)
			return
		}
		summary := propsQuerySummary(set)
		for _, t := range targets {
			existing, err := s.deps.Metadata.NodeProps().List(r.Context(), t.repo, t.path)
			if err != nil {
				s.writePropsStoreError(w, err)
				return
			}
			// The post-write key count (the cap the store cannot check):
			// every written key plus every surviving key the write neither
			// replaces nor drops — the T-297 arithmetic, one count.
			merged := len(set)
			for k := range existing {
				if _, written := set[k]; !written && !slices.Contains(drop, k) {
					merged++
				}
			}
			if merged > metadata.MaxNodePropKeys {
				writeError(w, http.StatusBadRequest, fmt.Sprintf(
					"more than %d property keys on one node", metadata.MaxNodePropKeys))
				return
			}
			// Item 9: the deletes go first, the writes second.
			if len(drop) > 0 {
				if err := s.deps.Metadata.NodeProps().Delete(r.Context(), t.repo, t.path, drop); err != nil {
					s.writePropsStoreError(w, err)
					return
				}
				if deleteChanges(existing, drop) {
					s.recordPropsAudit(r, propsAuditDelete, t.repo, t.path, strings.Join(drop, ","), len(targets) > 1)
				}
			}
			if len(set) > 0 {
				if err := s.deps.Metadata.NodeProps().Merge(r.Context(), t.repo, t.path, set); err != nil {
					s.writePropsStoreError(w, err)
					return
				}
				if mergeChanges(existing, set) {
					s.recordPropsAudit(r, propsAuditWrite, t.repo, t.path, summary, len(targets) > 1)
				}
			}
		}
		// The webhook seam rides the ADDRESSED node like the storage verbs
		// (the envelope's node facts come from the addressed row).
		for k, vs := range set {
			s.emitWebhook(r, webhook.Event{
				Domain: webhook.DomainArtifactProperty, Type: webhook.TypePropAdded,
				Repo: node.RepoKey, Path: node.Path, Sha256: node.Sha256, Size: node.Size,
				PropertyKey: k, PropertyValues: vs,
			})
		}
		for _, k := range drop {
			s.emitWebhook(r, webhook.Event{
				Domain: webhook.DomainArtifactProperty, Type: webhook.TypePropDeleted,
				Repo: node.RepoKey, Path: node.Path, Sha256: node.Sha256, Size: node.Size,
				PropertyKey: k,
			})
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMetadataDelete serves DELETE /api/metadata/{repo}/{path}: drop
// EVERY property of the target. The face is GUARD-LESS on the target
// (L024-11 / diff T1: a missing item and a non-local repository alike
// answer the silent 204 — the reference has no failure arm on this verb,
// the PATCH family's 400 wordings never ride DELETE). The 403 is the
// family's bare wording (item 11 reserves the verbose one for PATCH).
func (s *Server) handleMetadataDelete(w http.ResponseWriter, r *http.Request, repoKey, relPath string) {
	p := principalFrom(r.Context())
	if !s.allowPropsWrite(r, p, repoKey, relPath) {
		writePropsForbidden(w)
		return
	}
	node, ok := s.metadataDeleteTarget(w, r, repoKey, relPath)
	if !ok {
		return
	}
	targets, err := s.propsTargets(r, node, metadataRecursiveFlag(r, node, "recursive"))
	if err != nil {
		s.writePropsStoreError(w, err)
		return
	}
	for _, t := range targets {
		live, err := s.deps.Metadata.NodeProps().List(r.Context(), t.repo, t.path)
		if err != nil {
			s.writePropsStoreError(w, err)
			return
		}
		// nil keys = the store's every-key form (the properties=* law).
		if err := s.deps.Metadata.NodeProps().Delete(r.Context(), t.repo, t.path, nil); err != nil {
			s.writePropsStoreError(w, err)
			return
		}
		if deleteChanges(live, nil) {
			s.recordPropsAudit(r, propsAuditDelete, t.repo, t.path, "*", len(targets) > 1)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// metadataDeleteTarget resolves the DELETE face's target WITHOUT the
// PATCH guards: every unresolvable spelling (unknown repository,
// non-local repository, missing item) is the silent 204 — ok=false with
// the response already written.
func (s *Server) metadataDeleteTarget(w http.ResponseWriter, r *http.Request, repoKey, relPath string) (*metadata.Node, bool) {
	row, err := s.deps.Repos.Get(r.Context(), repoKey)
	if err != nil && !errors.Is(err, repo.ErrRepoNotFound) && !errors.Is(err, metadata.ErrRepoNotFound) {
		s.log.ErrorContext(r.Context(), "httpapi: metadata delete repository lookup failed",
			"repo", repoKey, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "metadata operation failed")
		return nil, false
	}
	if row == nil || row.Type != repo.TypeLocal {
		w.WriteHeader(http.StatusNoContent) // diff T1: no guard, no-op
		return nil, false
	}
	node, err := s.storageNode(r, principalFrom(r.Context()), repoKey, relPath)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) {
			w.WriteHeader(http.StatusNoContent) // diff T1: missing item is the idempotent no-op
			return nil, false
		}
		s.writeStorageError(w, err)
		return nil, false
	}
	return node, true
}

// metadataPatchTarget resolves the shared target guards of both verbs: the
// repository must be a LOCAL one (item 12 — virtual/remote and unknown
// keys alike answer the not-a-local-repository 400; the single guard is
// the root-cause fix, an unknown key fails the same local test) and the
// node must exist (item 6: a 400 in the set-failure wrap, NOT a 404 — the
// face's registered quirk).
func (s *Server) metadataPatchTarget(w http.ResponseWriter, r *http.Request, repoKey, relPath string) (*metadata.Node, bool) {
	row, err := s.deps.Repos.Get(r.Context(), repoKey)
	if err != nil && !errors.Is(err, repo.ErrRepoNotFound) && !errors.Is(err, metadata.ErrRepoNotFound) {
		s.log.ErrorContext(r.Context(), "httpapi: metadata face repository lookup failed",
			"repo", repoKey, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "metadata operation failed")
		return nil, false
	}
	if row == nil || row.Type != repo.TypeLocal {
		writeError(w, http.StatusBadRequest, metadataSetWrap(repoKey, relPath,
			fmt.Sprintf("Repository '%s' is not a local repository", repoKey)))
		return nil, false
	}
	node, err := s.storageNode(r, principalFrom(r.Context()), repoKey, relPath)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) {
			writeError(w, http.StatusBadRequest, metadataSetWrap(repoKey, relPath,
				fmt.Sprintf("Item %s:%s does not exist", repoKey, relPath)))
			return nil, false
		}
		s.writeStorageError(w, err)
		return nil, false
	}
	return node, true
}

// metadataSetWrap is the face's 400 wrap (items 5/6/12/13): the plain-text
// cause rides the standard errors envelope.
func metadataSetWrap(repoKey, path, cause string) string {
	return fmt.Sprintf("Failed to set properties on %s:%s: %s", repoKey, path, cause)
}

// parsePatchProps decodes the props object: string values become one-value
// sets, arrays keep their TEXT elements only (item 7: non-string elements
// are silently skipped — a number never fails the request), null drops the
// key, anything else is item 5's verbatim parse 400 (inside the set-failure
// wrap, item 13). Keys and values run the same closed validation the
// /api/storage verbs enforce.
func parsePatchProps(repoKey, relPath string, raw json.RawMessage) (map[string][]string, []string, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, nil, errors.New(metadataSetWrap(repoKey, relPath, patchPropsParseMsg))
	}
	set := map[string][]string{}
	var drop []string
	for k, v := range obj {
		if err := metadata.ValidatePropKey(k); err != nil {
			return nil, nil, err
		}
		trimmed := strings.TrimSpace(string(v))
		if trimmed == "null" {
			drop = append(drop, k)
			continue
		}
		var one string
		if err := json.Unmarshal(v, &one); err == nil {
			if err := metadata.ValidatePropValue(k, one); err != nil {
				return nil, nil, err
			}
			set[k] = append(set[k], one)
			continue
		}
		var arr []json.RawMessage
		if err := json.Unmarshal(v, &arr); err != nil {
			// Neither a string nor an array (a bare number, object, bool):
			// the reference's own parse failure, verbatim in the wrap.
			return nil, nil, errors.New(metadataSetWrap(repoKey, relPath, patchPropsParseMsg))
		}
		vals := make([]string, 0, len(arr))
		for _, el := range arr {
			var sv string
			if err := json.Unmarshal(el, &sv); err != nil {
				continue // item 7: non-text elements skip silently
			}
			if err := metadata.ValidatePropValue(k, sv); err != nil {
				return nil, nil, err
			}
			vals = append(vals, sv)
		}
		set[k] = vals
	}
	if err := metadata.ValidatePropSet(set); err != nil {
		return nil, nil, err
	}
	return set, drop, nil
}

// metadataRecursiveFlag reads the face's recursion flag (item 8: the PUT
// family's own law, rest-api.md section 3's footer): absent/blank lets
// the TARGET decide — a folder defaults recursive, a file non-recursive —
// while an explicit value is the 0/1 boolean. atomicProperties is
// deliberately unread (item 8's registered divergence: the reference
// ignores it too).
func metadataRecursiveFlag(r *http.Request, node *metadata.Node, param string) bool {
	v := rawQueryValue(r, param)
	if strings.TrimSpace(v) == "" {
		return isFolderPath(node.Path)
	}
	return isTruthyFlag(v)
}
