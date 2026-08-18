package docker

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// apiVersion is the literal first segment of every registry route.
const apiVersion = "v2"

// routeTail segments that end an image name and begin the endpoint route
// ([DIST-API] URL layout). The name parser scans for them from the LEFT but
// only accepts a match when at least one name segment precedes the segment
// — the official distribution reference implementation resolves names the
// same greedy way ("/v2/foo/manifests/latest" has no legal two-segment
// reading, "foo" alone is not a repository reference).
var routeTails = []string{"manifests", "blobs", "tags", "referrers", "uploads"}

// nameRef is a parsed /v2 request: the BinFlow repository key, the image's
// repository-relative name and the endpoint tail that follows it.
type nameRef struct {
	repoKey string
	image   string
	tail    string
}

// parseV2Name splits a /v2 request path per ADR-0010 clause 3:
//
//	/v2/<name>/(manifests|blobs|tags|referrers|uploads)/...
//	 name = repoKey "/" image ("/" image-segment)*
//
// The whole path is percent-decoded BEFORE the split, so a %2e%2e traversal
// is judged on its decoded form and can never ride the escape layer past
// the dot-segment defense (NFR-S11, the docker twin of the generic layer's
// FR-4-AC10 rule).
//
// Error mapping is the caller's job and stays shape-based, mirroring the
// generic layer's split: dot-segment/bad-escape rejections (ErrBadRequestPath
// with a "dot segment" or "malformed" wording) are 400; single-segment
// names and missing routes are 404 (a client cannot address anything with
// them, and the spec has no error for "not a name at all").
func parseV2Name(path string) (nameRef, error) {
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return nameRef{}, fmt.Errorf("%w: malformed percent-encoding in %q: %w",
			adapter.ErrBadRequestPath, path, err)
	}
	decoded = strings.TrimPrefix(decoded, "/"+apiVersion)
	if decoded == "" {
		return nameRef{}, errNotAName("no repository name after /v2")
	}
	segments := strings.Split(strings.TrimPrefix(decoded, "/"), "/")
	if len(segments) < 2 {
		// Single-segment name ("/v2/<one>"): no repository-key split
		// exists. Artifactory also requires the repo-prefix form; M2 does
		// not serve top-level bare names (ADR-0010 clause 3).
		return nameRef{}, errNotAName(fmt.Sprintf("name %q has no repository prefix", decoded))
	}
	// Dot-segment / empty-segment defense runs BEFORE any shape analysis,
	// and covers EVERY segment including the repository key (T-33 review
	// B2): a traversal spelling like /v2/team1/../etc/passwd happens to
	// carry no registry route, and judging shape first would answer a
	// benign 404 for what is an attack probe — the 400 must win
	// (NFR-S11). Judging only the image half left /v2/../x reachable as
	// a 404 with ".." sitting in the repo-key slot.
	if err := adapter.NormalizeRelPath(strings.Join(segments, "/")); err != nil {
		return nameRef{}, err
	}
	repoKey := segments[0]
	if repoKey == "" {
		return nameRef{}, errNotAName("empty repository key")
	}
	if len(repoKey) > adapter.MaxRepoKeyLen {
		return nameRef{}, fmt.Errorf("%w: repository key longer than %d characters",
			adapter.ErrBadRequestPath, adapter.MaxRepoKeyLen)
	}
	// Locate the route tail: the first tail segment that has at least one
	// name segment before it INSIDE the remainder (repoKey already
	// consumed one segment). "acme/app/manifests/latest" -> tail at
	// "manifests", image "acme/app"; "acme/manifests/latest" -> image
	// "acme" — a single image segment is legal (repoKey/image).
	rest := segments[1:]
	tailIdx := -1
	for i, seg := range rest {
		if i == 0 {
			continue // the image needs at least one segment before the route
		}
		if isRouteTail(seg) {
			tailIdx = i
			break
		}
	}
	if tailIdx < 1 {
		return nameRef{}, errNotAName(fmt.Sprintf("path %q carries no registry route", decoded))
	}
	image := strings.Join(rest[:tailIdx], "/")
	tail := strings.Join(rest[tailIdx:], "/")
	return nameRef{repoKey: repoKey, image: image, tail: tail}, nil
}

// isRouteTail reports whether the segment opens the endpoint route.
func isRouteTail(seg string) bool {
	for _, t := range routeTails {
		if seg == t {
			return true
		}
	}
	return false
}

// errNotAName marks shape misses that upstream answers with 404.
func errNotAName(msg string) error {
	return &notANameError{msg: msg}
}

// notANameError is the 404-shaped parse rejection.
type notANameError struct{ msg string }

func (e *notANameError) Error() string { return e.msg }

// isNotAName reports whether err is a 404-shaped name-parse rejection.
func isNotAName(err error) bool {
	var target *notANameError
	return errors.As(err, &target)
}

// Layout implements adapter.Handler for the /v2 mount. httpapi hands the
// request over with NO prefix stripped (the root-level exception,
// ADR-0010 clause 1); relPath is the image name (repo-key-relative).
func (h *Handler) Layout(r *http.Request) (string, string, error) {
	if r == nil || r.URL == nil {
		return "", "", fmt.Errorf("%w: empty request URL", adapter.ErrBadRequestPath)
	}
	ref, err := parseV2Name(r.URL.EscapedPath())
	if err != nil {
		return "", "", err
	}
	return ref.repoKey, ref.image, nil
}
