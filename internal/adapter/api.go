package adapter

import (
	"errors"
	"net/http"
)

// Handler is the complete HTTP behavior of one package protocol (generic,
// docker, maven, ...) mounted behind the /binflow prefix (architecture
// section 5.1). httpapi strips /binflow, resolves the first path segment to
// a repository row, and dispatches to the handler registered for that
// repository's package type.
type Handler interface {
	// Protocol names the protocol for routing and documentation, e.g.
	// "generic" or "docker".
	Protocol() string
	// RepoTypes declares which repository classes this protocol can serve
	// ("local", "remote", "virtual"); httpapi uses it for up-front 404/400
	// decisions. M1: {"local"} only.
	RepoTypes() []string
	// Layout splits the request path (with the /binflow prefix already
	// stripped) into (repoKey, relPath). generic: first segment is the
	// repository key, the remainder is the artifact path. Parse failures
	// return an error wrapping ErrBadRequestPath.
	Layout(r *http.Request) (repoKey, relPath string, err error)
	// ServeHTTP is the business body. httpapi's middleware chain has already
	// authenticated and authorized the request; the principal reaches the
	// handler via PrincipalFrom. The handler talks to repo.Service only.
	http.Handler
}

// ErrBadRequestPath wraps every Layout rejection: dot-segment escape,
// invalid percent-encoding, empty segment, reserved segment, oversized
// path. The HTTP layer maps it to 400 (FR-4-AC10/AC11).
var ErrBadRequestPath = errors.New("bad request path")

// ErrInvalidChecksum wraps a malformed client-declared X-Checksum-* header
// value (wrong width or non-hex). Callers map it to 400 — distinct from a
// well-formed digest that simply disagrees with the content, which is a
// 409 (repo-semantics section 5, client-checksums policy).
var ErrInvalidChecksum = errors.New("invalid checksum header")

// Path limits (FR-4-AC11): the repository segment is bounded by the repo
// package's own charset rule ([a-z][a-z0-9-]{1,62}, PRD FR-3-AC4), artifact
// paths are bounded to 512 characters — the same ceiling the metadata layer
// enforces, checked here first so a bad path dies at the front door.
const (
	// MaxRepoKeyLen is the longest repository key segment Layout accepts.
	MaxRepoKeyLen = 63
	// MaxRelPathLen is the longest repo-relative artifact path Layout
	// accepts (FR-4-AC11).
	MaxRelPathLen = 512
)

// IsReservedSegment reports whether seg collides with a /binflow routing
// segment (ADR-0008). Repository keys may never use them, so a content path
// starting with one is never routed to an adapter in the first place;
// Layout still rejects them defensively for direct mounts.
func IsReservedSegment(seg string) bool { return seg == "api" || seg == "v2" }
