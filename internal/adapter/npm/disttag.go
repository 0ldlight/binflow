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
// 200 empty, missing tag -> 404 "npm package not found with name:<n>, and
// tag:<t>").
//
// L012-1 wire alignment (reports/compatibility/L012-dist-tags-evidence.md,
// conductor rulings D2-D7): the collection is READ-ONLY (the reference
// registry rejects the bulk PUT/POST face with 405 — m13/m14/m15), GET
// recomputes an absent "latest" at read time (the tag is immortal — D2) and
// carries Cache-Control: max-age=60 (D7), a PUT naming a missing version
// answers the version-position 404 wording (D3), a malformed body answers
// 400 with a neutral message that never leaks the decoder's internals (D5),
// and a ghost package answers 404 "Not found" (D4).

// distTagsMaxBody bounds the tag payloads (a JSON string or a small map).
const distTagsMaxBody = 1 << 20

// serveDistTags handles the collection: GET/HEAD lists the tags with the
// read-time latest recompute; every mutating method is 405 (npm's own
// dist-tag ls/add/rm flows only ever GET the collection and address the
// single-tag routes — the bulk shape the pre-8 registry contract allowed is
// gone from the reference wire, L012-1 m13/m14).
func (h *Handler) serveDistTags(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *Principal, repoKey, name string) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		// The read face sees the MERGED tag union (T-72); only the mutating
		// branches below read the deployment target's own document.
		doc, _, _, err := h.loadPackument(ctx, p, repoKey, name)
		if err != nil {
			h.writeTagsLookupError(w, err)
			return
		}
		crownLatest(doc)
		tags := distTagsOf(doc)
		// D7: the reference pins a one-minute client freshness window on the
		// dist-tags read (npm's own fetchTags caching rides it).
		w.Header().Set("Cache-Control", "max-age=60")
		writeJSONBody(w, http.StatusOK, tags)
	default:
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, msgMethodNotAllowed)
	}
}

// crownLatest recomputes an ABSENT "latest" at read time from the greatest
// stored version — the D2 recompute, defined ONCE here and shared by every
// read face: the dist-tags collection above and the packument projection
// (renderPackument) crown from this same source, so the two faces can never
// disagree (L013 R-15 evidence n4: after DELETE latest the reference still
// answers latest=1.1.0 on BOTH GET /-/package/<n>/dist-tags and GET /<name>).
// Deleting latest can never leave the package tagless; the recompute crowns
// the greatest version. A PRESENT latest is never overwritten: pointing
// latest at an older version is a legitimate client rollback the reference
// registry honors. The STORED document is never touched — callers pass a
// per-request decode or a render copy, so the crown stays a read-time
// projection (a second DELETE still answers the tag-not-found 404).
func crownLatest(doc map[string]any) {
	if _, ok := mapOf(doc["dist-tags"])["latest"]; ok {
		return
	}
	best := latestVersion(versionsOf(doc))
	if best == "" {
		return
	}
	tags := mapOf(doc["dist-tags"])
	if tags == nil {
		tags = map[string]any{}
		doc["dist-tags"] = tags
	}
	tags["latest"] = best
}

// serveDistTag handles one tag: PUT body is a JSON string naming the
// version; DELETE removes; POST is not on the reference wire (m15).
func (h *Handler) serveDistTag(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *Principal, repoKey, name, tag string) {
	switch r.Method {
	case http.MethodPut:
		var version string
		dec := json.NewDecoder(io.LimitReader(r.Body, distTagsMaxBody))
		if err := dec.Decode(&version); err != nil {
			writeError(w, http.StatusBadRequest, msgInvalidTagBody)
			return
		}
		h.setDistTags(ctx, w, p, repoKey, name, map[string]string{tag: version})
	case http.MethodDelete:
		h.deleteDistTag(ctx, w, p, repoKey, name, tag)
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		writeError(w, http.StatusMethodNotAllowed, msgMethodNotAllowed)
	}
}

// serveLegacyTagPut is the pre-npm-8 spelling: PUT /<name>/<tag> with a
// JSON-string version body (identical semantics to the modern route).
func (h *Handler) serveLegacyTagPut(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *Principal, repoKey, name, tag string) {
	var version string
	dec := json.NewDecoder(io.LimitReader(r.Body, distTagsMaxBody))
	if err := dec.Decode(&version); err != nil {
		writeError(w, http.StatusBadRequest, msgInvalidTagBody)
		return
	}
	h.setDistTags(ctx, w, p, repoKey, name, map[string]string{tag: version})
}

// setDistTags validates and merges tags into the packument: every referenced
// version must exist (the version-position 404 wording, D3), the caller needs
// write on the package (the service's Put gate), success answers the pinned
// 201 body.
func (h *Handler) setDistTags(ctx context.Context, w http.ResponseWriter, p *Principal,
	repoKey, name string, tags map[string]string) {
	h.docMu.Lock()
	defer h.docMu.Unlock()
	doc, _, _, err := h.loadPackumentForWrite(ctx, p, repoKey, name)
	if err != nil {
		h.writeTagsLookupError(w, err)
		return
	}
	oldDoc := copyDoc(doc)
	versions := versionsOf(doc)
	for tag, version := range tags {
		if version == "" {
			writeError(w, http.StatusBadRequest, "dist-tag "+tag+" has an empty version")
			return
		}
		if versions[version] == nil {
			writeError(w, http.StatusNotFound, fmt.Sprintf(msgTagVersionNotFound, name, version))
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
	// A tag move rewrites no version manifest, so the save classifies
	// append-only and rides the write grant (T-249's classifier; spec
	// section 2.1's own error column for this route is "403 无 write" — no
	// delete demand).
	if err := h.savePackument(ctx, p, repoKey, name, oldDoc, doc); err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSONBody(w, http.StatusCreated, map[string]any{"ok": "created new tag"})
}

// deleteDistTag removes one tag; missing package or missing tag is the same
// pinned 404; success is 200 with an empty body. Deleting "latest" succeeds
// — the read face recomputes it (D2 above).
func (h *Handler) deleteDistTag(ctx context.Context, w http.ResponseWriter, p *Principal,
	repoKey, name, tag string) {
	h.docMu.Lock()
	defer h.docMu.Unlock()
	doc, _, _, err := h.loadPackumentForWrite(ctx, p, repoKey, name)
	if err != nil {
		h.writeTagsLookupError(w, err)
		return
	}
	oldDoc := copyDoc(doc)
	tags := mapOf(doc["dist-tags"])
	if tags == nil || tags[tag] == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf(msgTagNotFound, name, tag))
		return
	}
	delete(tags, tag)
	doc["dist-tags"] = tags
	bumpRev(doc)
	// Same as setDistTags: no version manifest is rewritten, the write grant
	// carries the save (T-249).
	if err := h.savePackument(ctx, p, repoKey, name, oldDoc, doc); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK) // pinned: 200, empty body
}

// writeTagsLookupError renders the dist-tag lookups' failures (missing
// package = the reference's bare "Not found", D4/m05).
func (h *Handler) writeTagsLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, repo.ErrNodeNotFound) {
		writeError(w, http.StatusNotFound, msgGhostNotFound)
		return
	}
	h.writeServiceError(w, err)
}
