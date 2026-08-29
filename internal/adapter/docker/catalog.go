package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The catalog domain (T-40, FR-10/DE-11/DE-12): GET /v2/_catalog and
// GET /v2/<name>/tags/list over the single official pagination contract —
// n as the page size (default 100; the Artifactory full-list deviation is
// NOT adopted, docker-registry.md section 6), last as the EXCLUSIVE cursor,
// and a Link rel="next" header exactly while a further page exists.

const (
	// defaultPageSize is n's default when the parameter is absent (official
	// distribution default; PRD v1.1 section 6.5 calibration).
	defaultPageSize = 100

	// tagsListTail is the route tail of the tag listing endpoint
	// ("/v2/<name>/tags/list" — parseV2Name yields tail "tags/list").
	tagsListTail = "tags/list"

	// linkNext is the Link header's relation suffix for the next page
	// ([DIST-API] Pagination; the angle brackets belong to the value).
	linkNext = `; rel="next"`
)

// catalogBody is the _catalog response shape (DE-11).
type catalogBody struct {
	Repositories []string `json:"repositories"`
}

// tagsBody is the tags/list response shape (DE-12). Tags stays nil for an
// image that exists but carries zero tags: the wire form is "tags":null —
// the PRD v1.1 ruling (R4), aligned with the distribution reference
// implementation and deliberately NOT the empty array.
type tagsBody struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// serveCatalog implements GET /v2/_catalog (DE-11, FR-10-AC1). The listing
// aggregates repo.Service.ListImages over every docker repository the
// caller may see — the source of truth is "images with at least one
// manifest" (docker_manifests DISTINCT), so an empty docker repository
// never appears and a deleted repository vanishes with its cascade
// (FR-7-AC5). Visibility is the Q5 interim matrix: admin sees everything,
// an authenticated non-admin sees the repositories its read ACL covers,
// anonymous sees everything while anonymous_access is on — and no
// credential on a closed instance gets the catalog-scoped challenge (AC4:
// registry:catalog:*, the scope T-37's token flow already narrows).
func (h *Handler) serveCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeSpecError(w, http.StatusMethodNotAllowed, ErrCodeUnsupported,
			fmt.Sprintf("method %s is not supported on the catalog", r.Method), nil)
		return
	}
	p := principalOf(r)
	if p == nil && !h.opts.AnonymousAccess {
		// The catalog is a management-flavored listing: without a credential
		// on a closed instance it challenges with the registry-level catalog
		// scope (the client then negotiates a token carrying exactly that
		// grant, T-37's narrowing table).
		h.challenge(w, r, scopeRegistryCatalog)
		return
	}
	n, ok := parsePageSize(w, r)
	if !ok {
		return // parsePageSize rendered the 400
	}
	last := r.URL.Query().Get("last")
	if h.svc == nil {
		writeSpecError(w, http.StatusServiceUnavailable, ErrCodeUnavailable,
			"registry catalog is not available", nil)
		return
	}

	rows, err := h.repos.List(r.Context())
	if err != nil {
		// A repository listing fault is not "empty catalog" (the B2 posture:
		// an outage must not masquerade as a fact about the data).
		h.log.ErrorContext(r.Context(), "docker: catalog repository listing failed", "error", err.Error())
		writeSpecError(w, http.StatusInternalServerError, ErrCodeUnknown,
			"catalog repository listing failed", nil)
		return
	}

	// Aggregate per-repository listings into one globally sorted name set.
	// The merge cannot be a plain concatenation in repository-key order:
	// keys are only a name prefix, and "team-x/app" sorts BEFORE "team/app"
	// ('-' < '/'), so a global sort after the union is the only correct
	// ordering. The per-repository fetch is unbounded (n=0): the official
	// page window is applied once, over the merged set, so a page may span
	// repositories — the price is one full listing per visible repository,
	// right-sized for the M2 single-node registry.
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		// The registry-v2 family (HL-3): a helmoci repository's images are
		// registry names like any docker repository's — the family shares
		// this plane, so it shares the catalog.
		if !servesV2Plane(row.PackageType()) {
			continue
		}
		if !h.catalogVisible(r.Context(), p, row.Key()) {
			continue
		}
		images, err := h.svc.ListImages(r.Context(), p, row.Key(), 0, "")
		switch {
		case err == nil:
			names = append(names, images...)
		case errors.Is(err, repo.ErrRepoNotFound):
			// The repository vanished between the listing and the query —
			// its rows went with it, so it simply leaves the catalog.
			continue
		case errors.Is(err, repo.ErrUnauthorized):
			// The service's own read gate refused a principal the Q5 filter
			// admitted (a nil-authorizer assembly, or a target that covers
			// the repository root only partially): fail closed honestly
			// rather than 500-ing a permission answer.
			h.challenge(w, r, scopeRegistryCatalog)
			return
		default:
			h.log.ErrorContext(r.Context(), "docker: catalog image listing failed",
				"repo", row.Key(), "error", err.Error())
			writeSpecError(w, http.StatusInternalServerError, ErrCodeUnknown,
				fmt.Sprintf("catalog image listing failed for %q", row.Key()), nil)
			return
		}
	}
	sort.Strings(names)

	page, more := slicePage(names, last, n)
	if page == nil {
		page = []string{} // an empty catalog renders as [], never null
	}
	if more {
		w.Header().Set("Link", nextPageLink(catalogPath, page[len(page)-1], n))
	}
	writeAPIVersion(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(catalogBody{Repositories: page})
}

// catalogVisible answers the Q5 interim matrix for one repository key:
// admin sees everything; an authenticated non-admin sees what its read ACL
// covers (the repository root — the same question ListImages asks on the
// service side); anonymous sees everything while the flag is on. A nil
// Authorizer fails closed for authenticated principals (authorizeRoute's
// convention) and stays open for the anonymous-read case.
func (h *Handler) catalogVisible(ctx context.Context, p *Principal, repoKey string) bool {
	if p == nil {
		return h.opts.AnonymousAccess
	}
	if p.Admin {
		return true
	}
	if h.authz == nil {
		return false
	}
	return h.authz.Can(ctx, p, repoKey, "", auth.ActionRead)
}

// serveTagsList implements GET /v2/<name>/tags/list (DE-12, FR-10-AC2/AC3)
// over svc.ListTags — lexicographic tag order, the exclusive last cursor,
// and the empty-image distinction: an image without any manifest row is
// NAME_UNKNOWN, an image whose tags were all deleted renders "tags":null.
func (h *Handler) serveTagsList(w http.ResponseWriter, r *http.Request, ref nameRef) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeSpecError(w, http.StatusMethodNotAllowed, ErrCodeUnsupported,
			fmt.Sprintf("method %s is not supported on tags/list", r.Method), nil)
		return
	}
	n, ok := parsePageSize(w, r)
	if !ok {
		return // parsePageSize rendered the 400
	}
	last := r.URL.Query().Get("last")
	if h.svc == nil {
		writeSpecError(w, http.StatusServiceUnavailable, ErrCodeUnavailable,
			"registry catalog is not available", nil)
		return
	}

	// Probe one entry past the page: svc.ListTags caps at its n, so asking
	// for n+1 tells the Link condition (a full page with nothing after it
	// carries no next link) without a second query.
	got, err := h.svc.ListTags(r.Context(), principalOf(r), ref.repoKey, ref.image, n+1, last)
	if err != nil {
		if errors.Is(err, repo.ErrImageNotFound) && h.imageListed(r.Context(), principalOf(r), ref) {
			// T-35's ListTags folds "existing image, zero tags" into
			// ErrImageNotFound, contradicting its own contract (repo/api.go:
			// "an existing image with zero tags returns an empty slice" —
			// repo/service.go checks the manifests query's error but never
			// its rows). The distinction is DE-12's whole point: unknown
			// image -> NAME_UNKNOWN, tagless image -> "tags":null. Rather
			// than reaching into the repo area, the adapter separates the
			// two states against the same source of truth the catalog reads
			// (docker_manifests' distinct images, via ListImages). The probe
			// rides only this rare error path; once the service honors its
			// contract, the branch simply stops firing.
			//
			// Update (T-52, 2026-08-19): the service contract was fixed in
			// repo/service.go — zero-tag images now return an empty slice,
			// not ErrImageNotFound. This branch is therefore dormant defense
			// in depth; keep it (it costs one ListImages probe on a rare
			// error path) but expect it not to fire.
			hdr := w.Header()
			writeAPIVersionHdr(hdr)
			hdr.Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(tagsBody{Name: ref.repoKey + "/" + ref.image})
			return
		}
		h.writeTagsListError(w, r, err, ref)
		return
	}
	more := len(got) > n
	if more {
		got = got[:n]
	}
	var tags []string // nil on an empty image -> "tags":null (PRD R4)
	if len(got) > 0 {
		tags = make([]string, len(got))
		for i, t := range got {
			tags[i] = t.Tag
		}
	}
	if more {
		w.Header().Set("Link", nextPageLink(tagsListPath(ref), got[len(got)-1].Tag, n))
	}
	writeAPIVersion(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tagsBody{Name: ref.repoKey + "/" + ref.image, Tags: tags})
}

// imageListed reports whether the image carries at least one manifest row
// (the catalog's source of truth). It backs the ErrImageNotFound
// disambiguation on tags/list; a lookup failure answers false — the caller
// then renders the service's own error, never a fabricated verdict.
func (h *Handler) imageListed(ctx context.Context, p *Principal, ref nameRef) bool {
	if h.svc == nil {
		return false
	}
	images, err := h.svc.ListImages(ctx, p, ref.repoKey, 0, "")
	if err != nil {
		// DEBUG, not ERROR (T-43 QA D2): ListImages needs repo-ROOT read,
		// so a prefix-scoped principal (granted on the image but not the
		// repo) makes this probe fail with permission denied on the
		// legitimate "unknown image" path — an expected non-answer, not a
		// fault. ERROR here polluted the O1 "ERROR count = real failures"
		// signal (QA reproduced: unknown image + prefix grant -> 404 +
		// ERROR line). The caller falls back to the service's own error
		// either way; the log only records why the probe could not run.
		h.log.DebugContext(ctx, "docker: tagless-image probe unavailable",
			"repo", ref.repoKey, "image", ref.image, "error", err.Error())
		return false
	}
	want := ref.repoKey + "/" + ref.image
	for _, name := range images {
		if name == want {
			return true
		}
	}
	return false
}

// writeTagsListError maps the ListTags failures onto the spec body.
func (h *Handler) writeTagsListError(w http.ResponseWriter, r *http.Request, err error, ref nameRef) {
	switch {
	case errors.Is(err, repo.ErrImageNotFound), errors.Is(err, repo.ErrRepoNotFound):
		// FR-10-AC3: the name (or its repository) is not known to the
		// registry — NAME_UNKNOWN, not an empty list.
		writeSpecError(w, http.StatusNotFound, ErrCodeNameUnknown,
			fmt.Sprintf("repository name not known to registry: %q", ref.repoKey+"/"+ref.image),
			map[string]string{"name": ref.repoKey + "/" + ref.image})
	case errors.Is(err, repo.ErrInvalidCursor):
		// The cursor failed the tag charset (the only spelling a tags/list
		// cursor may take). PAGINATION_NUMBER_INVALID is the official
		// family's sole pagination code; the message carries the cause.
		writeSpecError(w, http.StatusBadRequest, ErrCodePaginationNumberInvalid,
			fmt.Sprintf("invalid last cursor %q: must be a tag the listing could have returned", r.URL.Query().Get("last")),
			map[string]string{"last": r.URL.Query().Get("last")})
	case errors.Is(err, repo.ErrUnauthorized):
		h.challenge(w, r, deriveChallengeScope(r.Method, ref))
	case errors.Is(err, repo.ErrForbidden):
		writeSpecError(w, http.StatusForbidden, ErrCodeDenied,
			"requested access to the resource is denied", nil)
	case errors.Is(err, repo.ErrInvalidImage):
		writeSpecError(w, http.StatusBadRequest, ErrCodeNameInvalid, err.Error(), nil)
	default:
		h.log.ErrorContext(r.Context(), "docker: tags/list failed",
			"repo", ref.repoKey, "image", ref.image, "error", err.Error())
		writeSpecError(w, http.StatusInternalServerError, ErrCodeUnknown,
			"tags/list failed: "+err.Error(), nil)
	}
}

// parsePageSize resolves the n parameter: absent -> defaultPageSize;
// present but not a positive decimal integer (0, negatives, non-numeric,
// overflow) -> 400 PAGINATION_NUMBER_INVALID (FR-10-AC4, docker-registry.md
// section 6). It returns ok=false when it already rendered the error.
func parsePageSize(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("n")
	if raw == "" {
		return defaultPageSize, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		writeSpecError(w, http.StatusBadRequest, ErrCodePaginationNumberInvalid,
			fmt.Sprintf("invalid page size %q: n must be a positive integer", raw),
			map[string]string{"n": raw})
		return 0, false
	}
	return n, true
}

// slicePage applies the official pagination slice to a lexicographically
// sorted name set: drop everything through the exclusive last cursor (a
// cursor beyond the tail leaves nothing — the official semantics leave a
// moved page edge undefined and the empty tail is the least surprising
// answer), then cap at n. more reports whether at least one entry follows
// the page, which is exactly the Link condition. n must be positive.
func slicePage(sorted []string, last string, n int) (page []string, more bool) {
	if last != "" {
		i := sort.SearchStrings(sorted, last)
		if i < len(sorted) && sorted[i] == last {
			i++ // exclusive: the cursor's own entry is skipped
		}
		sorted = sorted[i:]
	}
	if len(sorted) > n {
		return sorted[:n], true
	}
	return sorted, false
}

// nextPageLink builds the Link header value pointing at the next page
// (docker-registry.md section 6: <path?last=<last>&n=<n>>; rel="next" —
// path-absolute, the form both Artifactory and distribution emit and every
// client follows verbatim). The values ride through url.Values so a cursor
// carrying reserved characters ("%2F" inside a nested image name) can never
// corrupt the query.
func nextPageLink(path, last string, n int) string {
	q := url.Values{}
	q.Set("last", last)
	q.Set("n", strconv.Itoa(n))
	return "<" + path + "?" + q.Encode() + ">" + linkNext
}

// tagsListPath is the canonical tags/list URL path of one image.
func tagsListPath(ref nameRef) string {
	return "/v2/" + ref.repoKey + "/" + ref.image + "/" + tagsListTail
}
