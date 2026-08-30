package helmoci

import (
	"net/http"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/docker"
	"github.com/lzwzzy/binflow/internal/repo"
)

// Protocol is the package-type identifier this adapter serves — the repo
// package's own spelling, so the service layer's family check and this
// registration can never drift apart on one string.
const Protocol = repo.PackageHelmOCI

// Handler is the HelmOCI package type's adapter (architecture section 5.1
// dispatch shape). It is deliberately thin: the protocol face is the docker
// adapter's /v2 plane (HL-3 — see the package comment), and this handler
// exists so the content plane's package-type dispatch, the adapter
// registry and the addon-slot assembly guard all see the package type as a
// served, mounted citizen.
type Handler struct {
	// plane is the docker /v2 handler whose ServeHTTP the content-plane
	// requests delegate to. The /v2 root-level route itself dispatches to
	// the docker handler directly (router.go's exception, ADR-0010), so
	// this reference is exercised by the /binflow content face only.
	plane *docker.Handler
}

// Compile-time interface check.
var _ adapter.Handler = (*Handler)(nil)

// New wires the shell over the assembled docker /v2 plane. The plane must
// be the same handler instance cmd mounts at the /v2 route — the
// content-plane delegation then answers with the registry family's uniform
// 404 shape, and any future shared state (upload sessions, token caches)
// stays singular.
func New(plane *docker.Handler) *Handler {
	return &Handler{plane: plane}
}

// Register builds the shell and enters the process-wide handler registry
// under the helmoci key (the pypi/cargo convention; cmd assembly calls it
// exactly once, after the docker plane it hands in is built).
func Register(plane *docker.Handler) *Handler {
	h := New(plane)
	adapter.Register(h)
	return h
}

// Protocol implements adapter.Handler: the repositories.package_type value
// this adapter is dispatched on.
func (h *Handler) Protocol() string { return Protocol }

// RepoTypes implements adapter.Handler: the registry-v2 Helm face serves
// LOCAL repositories in full — the chart push/pull plane — and, since
// T-363, REMOTE repositories (the /v2 pull-through against an upstream OCI
// registry: manifest/blob proxy with Bearer-authenticated egress and the
// checksum-addressed cache). VIRTUAL (the manifest aggregation) is T-365's;
// the Helm/HelmOCI no-mix rule (T-309) already guards the member boundary
// meanwhile.
func (h *Handler) RepoTypes() []string {
	return []string{repo.TypeLocal, repo.TypeRemote}
}

// Layout implements adapter.Handler for the content-plane mount: the first
// path segment is the repository key, the remainder the repo-relative path
// (the generic split — ServeHTTP is the face that says the plane lives
// elsewhere).
func (h *Handler) Layout(r *http.Request) (string, string, error) {
	return adapter.Layout(r)
}

// ServeHTTP implements adapter.Handler. The HelmOCI protocol face is the
// /v2 plane, never /binflow/<repoKey>/...: a content-plane request is
// delegated to the docker handler, which answers the registry family's
// uniform spec-body 404 for any non-/v2 path — byte-identical to what a
// docker repository's content-plane request receives (the two package
// types are indistinguishable on this face, as on the /v2 face).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.plane.ServeHTTP(w, r)
}
