package npm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/lzwzzy/binflow/internal/repo"
)

// Dist-tags: the npm >= 8 family /-/package/<name>/dist-tags[/<tag>] and the
// legacy PUT /<name>/<tag> spelling (spec endpoint table, high confidence;
// PRD NE-04 pinned wording: PUT -> 201 {"ok":"created new tag"}, DELETE ->
// 200 empty, missing -> 404 "npm package not found with name:<n>, and
// tag:<t>").

// distTagsMaxBody bounds the tag payloads (a JSON string or a small map).
const distTagsMaxBody = 1 << 20

// serveDistTags handles the collection: GET lists, PUT/POST bulk-merge
// (npm's own dist-tag ls/add flows address the single-tag routes; the bulk
// shape is the pre-8 registry contract kept for old clients).
func (h *Handler) serveDistTags(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *Principal, repoKey, name string) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		// The read face sees the MERGED tag union (T-72); only the mutating
		// branches below read the deployment target's own document.
		doc, _, _, err := h.loadPackument(ctx, p, repoKey, name)
		if err != nil {
			h.writeTagsLookupError(w, err, name)
			return
		}
		writeJSONBody(w, http.StatusOK, distTagsOf(doc))
	case http.MethodPut, http.MethodPost:
		var tags map[string]string
		dec := json.NewDecoder(io.LimitReader(r.Body, distTagsMaxBody))
		if err := dec.Decode(&tags); err != nil {
			writeError(w, http.StatusBadRequest, "invalid dist-tags body: "+err.Error())
			return
		}
		if len(tags) == 0 {
			writeError(w, http.StatusBadRequest, "dist-tags body must be a non-empty tag map")
			return
		}
		h.setDistTags(ctx, w, p, repoKey, name, tags)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT, POST")
		writeError(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on dist-tags")
	}
}

// serveDistTag handles one tag: PUT body is a JSON string naming the
// version; DELETE removes.
func (h *Handler) serveDistTag(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *Principal, repoKey, name, tag string) {
	switch r.Method {
	case http.MethodPut, http.MethodPost:
		var version string
		dec := json.NewDecoder(io.LimitReader(r.Body, distTagsMaxBody))
		if err := dec.Decode(&version); err != nil {
			writeError(w, http.StatusBadRequest, "invalid dist-tag body: "+err.Error())
			return
		}
		h.setDistTags(ctx, w, p, repoKey, name, map[string]string{tag: version})
	case http.MethodDelete:
		h.deleteDistTag(ctx, w, p, repoKey, name, tag)
	default:
		w.Header().Set("Allow", "PUT, POST, DELETE")
		writeError(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on a dist-tag")
	}
}

// serveLegacyTagPut is the pre-npm-8 spelling: PUT /<name>/<tag> with a
// JSON-string version body (identical semantics to the modern route).
func (h *Handler) serveLegacyTagPut(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *Principal, repoKey, name, tag string) {
	var version string
	dec := json.NewDecoder(io.LimitReader(r.Body, distTagsMaxBody))
	if err := dec.Decode(&version); err != nil {
		writeError(w, http.StatusBadRequest, "invalid dist-tag body: "+err.Error())
		return
	}
	h.setDistTags(ctx, w, p, repoKey, name, map[string]string{tag: version})
}

// setDistTags validates and merges tags into the packument: every referenced
// version must exist (404 pinned wording), the caller needs write on the
// package (the service's Put gate), success answers the pinned 201 body.
func (h *Handler) setDistTags(ctx context.Context, w http.ResponseWriter, p *Principal,
	repoKey, name string, tags map[string]string) {
	h.docMu.Lock()
	defer h.docMu.Unlock()
	doc, _, _, err := h.loadPackumentForWrite(ctx, p, repoKey, name)
	if err != nil {
		h.writeTagsLookupError(w, err, name)
		return
	}
	versions := versionsOf(doc)
	for tag, version := range tags {
		if version == "" {
			writeError(w, http.StatusBadRequest, "dist-tag "+tag+" has an empty version")
			return
		}
		if versions[version] == nil {
			writeError(w, http.StatusNotFound, fmt.Sprintf(msgTagNotFound, name, tag))
			return
		}
	}
	tagsM := mapOf(doc["dist-tags"])
	if tagsM == nil {
		tagsM = map[string]any{}
	}
	for tag, version := range tags {
		tagsM[tag] = version
	}
	doc["dist-tags"] = tagsM
	bumpRev(doc)
	if err := h.savePackument(ctx, p, repoKey, name, doc); err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSONBody(w, http.StatusCreated, map[string]any{"ok": "created new tag"})
}

// deleteDistTag removes one tag; missing package or missing tag is the same
// pinned 404; success is 200 with an empty body.
func (h *Handler) deleteDistTag(ctx context.Context, w http.ResponseWriter, p *Principal,
	repoKey, name, tag string) {
	h.docMu.Lock()
	defer h.docMu.Unlock()
	doc, _, _, err := h.loadPackumentForWrite(ctx, p, repoKey, name)
	if err != nil {
		h.writeTagsLookupError(w, err, name)
		return
	}
	tags := mapOf(doc["dist-tags"])
	if tags == nil || tags[tag] == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf(msgTagNotFound, name, tag))
		return
	}
	delete(tags, tag)
	doc["dist-tags"] = tags
	bumpRev(doc)
	if err := h.savePackument(ctx, p, repoKey, name, doc); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK) // pinned: 200, empty body
}

// writeTagsLookupError renders the dist-tag lookups' failures (missing
// package = the pinned not-found wording).
func (h *Handler) writeTagsLookupError(w http.ResponseWriter, err error, name string) {
	if errors.Is(err, repo.ErrNodeNotFound) {
		writeError(w, http.StatusNotFound, fmt.Sprintf(msgPackNotFound, name))
		return
	}
	h.writeServiceError(w, err)
}
